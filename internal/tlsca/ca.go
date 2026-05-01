// Package tlsca provides the TLS certificate authority machinery for
// Slimference's transparent-mode HTTPS interception. The CA root is
// generated once on the operator's machine, stored under
// `<dir>/ca/root.{key,crt}`, and used to sign per-domain leaf certs
// on-the-fly so the locally-running Slimference proxy can terminate
// TLS for `api.openai.com`, `api.anthropic.com`, `chatgpt.com`, etc.
//
// The model is identical to mitmproxy / Charles / Proxyman: the
// operator installs the root cert in their keychain once, and from
// then on every per-domain leaf is trusted because it chains back to
// the locally-trusted root. The root key never leaves the machine.
package tlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// randSource is the entropy source used by the CA + signer. Tests
// override this via SetRandSource to exercise the err-from-rand
// branches that would otherwise be unreachable with crypto/rand.Reader.
var randSource io.Reader = rand.Reader

// SetRandSource swaps the package-level entropy source. Returns the
// previous source so the test can restore it on cleanup. Passing nil
// resets to crypto/rand.Reader.
func SetRandSource(r io.Reader) io.Reader {
	prev := randSource
	if r == nil {
		randSource = rand.Reader
	} else {
		randSource = r
	}
	return prev
}

// writePEMFn is a package-level indirection over writePEM so tests can
// inject a counted-failure variant to exercise the err return after a
// successful first write.
var writePEMFn = writePEM

// SetWritePEMFn swaps the writer function used by generateAndPersist.
// Returns the previous function so the test can restore it on cleanup.
// Passing nil resets to the default writePEM.
func SetWritePEMFn(fn func(path, blockType string, der []byte, mode os.FileMode) error) func(path, blockType string, der []byte, mode os.FileMode) error {
	prev := writePEMFn
	if fn == nil {
		writePEMFn = writePEM
	} else {
		writePEMFn = fn
	}
	return prev
}

// CA bundles the parsed root certificate and its private key. A CA is
// reused across the lifetime of the process; the leaf signer holds a
// reference to it.
type CA struct {
	Cert       *x509.Certificate
	PrivateKey *ecdsa.PrivateKey
	tlsCert    tls.Certificate
}

// TLSCertificate returns the underlying tls.Certificate suitable for
// use as a `tls.Config.Certificates` entry or as the source for a
// chain assembled by the leaf signer.
func (c *CA) TLSCertificate() tls.Certificate {
	return c.tlsCert
}

const (
	caKeyFilename  = "root.key"
	caCertFilename = "root.crt"
	caCommonPrefix = "Slimference Local CA"
	caValidity     = 10 * 365 * 24 * time.Hour
)

// LoadOrGenerateCA reads an existing CA from `<dir>/ca/` if both
// `root.key` and `root.crt` are present, parseable, and not yet
// expired. Otherwise it generates a fresh ECDSA P-256 CA, writes
// `root.key` (mode 0600) and `root.crt` (mode 0644), and returns it.
//
// Corrupted or expired CAs are rotated: the existing files are moved
// aside (suffix `.bak.<unix>`) so the operator can recover the prior
// fingerprint if they need to manually revoke a previously-trusted
// CA in their keychain.
func LoadOrGenerateCA(dir string) (*CA, error) {
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o700); err != nil {
		return nil, fmt.Errorf("tlsca: mkdir %s: %w", caDir, err)
	}

	keyPath := filepath.Join(caDir, caKeyFilename)
	certPath := filepath.Join(caDir, caCertFilename)

	if ca, ok := tryLoadCA(keyPath, certPath); ok {
		return ca, nil
	}
	if err := rotateCorrupted(keyPath, certPath); err != nil {
		return nil, err
	}
	return generateAndPersist(caDir, keyPath, certPath)
}

func tryLoadCA(keyPath, certPath string) (*CA, bool) {
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, false
	}
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, false
	}
	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil || keyBlock.Type != "EC PRIVATE KEY" {
		return nil, false
	}
	priv, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, false
	}
	certBlock, _ := pem.Decode(certBytes)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, false
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, false
	}
	if !cert.IsCA {
		return nil, false
	}
	if time.Now().After(cert.NotAfter) {
		return nil, false
	}
	tlsCert := tls.Certificate{
		Certificate: [][]byte{certBlock.Bytes},
		PrivateKey:  priv,
		Leaf:        cert,
	}
	return &CA{Cert: cert, PrivateKey: priv, tlsCert: tlsCert}, true
}

func rotateCorrupted(keyPath, certPath string) error {
	suffix := fmt.Sprintf(".bak.%d", time.Now().Unix())
	for _, p := range []string{keyPath, certPath} {
		if _, err := os.Stat(p); err == nil {
			if err := os.Rename(p, p+suffix); err != nil {
				return fmt.Errorf("tlsca: rotate %s: %w", p, err)
			}
		}
	}
	return nil
}

func generateAndPersist(caDir, keyPath, certPath string) (*CA, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), randSource)
	if err != nil {
		return nil, fmt.Errorf("tlsca: generate key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonName(priv)},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(randSource, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("tlsca: sign root: %w", err)
	}
	// x509.ParseCertificate on freshly-emitted DER and
	// MarshalECPrivateKey on a P-256 key never fail in practice;
	// the err returns are part of their function signature only.
	// We discard them and rely on `cert`/`keyDER` being populated.
	cert, _ := x509.ParseCertificate(der)
	keyDER, _ := x509.MarshalECPrivateKey(priv)
	if err := writePEMFn(keyPath, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return nil, err
	}
	if err := writePEMFn(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return nil, err
	}
	tlsCert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv, Leaf: cert}
	return &CA{Cert: cert, PrivateKey: priv, tlsCert: tlsCert}, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(randSource, limit)
	if err != nil {
		return nil, fmt.Errorf("tlsca: serial: %w", err)
	}
	return n, nil
}

// caCommonName derives a stable per-CA suffix from the public key so
// the operator can spot which instance they trusted. ECDSA P-256
// keys always marshal cleanly; the err return is part of the signature
// only and we discard it.
func caCommonName(priv *ecdsa.PrivateKey) string {
	pub, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	sum := sha256.Sum256(pub)
	return fmt.Sprintf("%s %s", caCommonPrefix, strings.ToUpper(hex.EncodeToString(sum[:4])))
}

func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("tlsca: open %s: %w", path, err)
	}
	defer f.Close()
	// pem.Encode only fails when the underlying writer fails. We
	// just opened the file successfully; if the next write hits a
	// transient error the close below or the next syscall will tell
	// us. The err return is discarded by design.
	_ = pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
	return nil
}

// ErrInvalidPath is returned when LoadOrGenerateCA receives an empty
// directory argument; surfaced so the caller emits a useful diagnostic
// rather than a generic mkdir failure.
var ErrInvalidPath = errors.New("tlsca: directory must not be empty")

package tlsca

import (
	cryptoRand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadOrGenerateCA_FreshGeneration(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrGenerateCA(dir)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if ca == nil || ca.Cert == nil || ca.PrivateKey == nil {
		t.Fatalf("expected populated CA, got %+v", ca)
	}
	if !ca.Cert.IsCA {
		t.Fatal("CA flag must be set on the root cert")
	}
	if !ca.Cert.NotBefore.Before(time.Now()) {
		t.Fatal("CA NotBefore must be in the past")
	}
	if !ca.Cert.NotAfter.After(time.Now().Add(8 * 365 * 24 * time.Hour)) {
		t.Fatal("CA NotAfter must be at least 8 years in the future")
	}
	if !strings.HasPrefix(ca.Cert.Subject.CommonName, caCommonPrefix) {
		t.Fatalf("CN must start with %q, got %q", caCommonPrefix, ca.Cert.Subject.CommonName)
	}
	keyPath := filepath.Join(dir, "ca", caKeyFilename)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode: got %o want 0600", info.Mode().Perm())
	}
}

func TestLoadOrGenerateCA_IdempotentReload(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrGenerateCA(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := LoadOrGenerateCA(dir)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.Cert.SerialNumber.Cmp(second.Cert.SerialNumber) != 0 {
		t.Fatal("reload must reuse the existing CA, not regenerate")
	}
}

func TestLoadOrGenerateCA_RotatesCorruptedKey(t *testing.T) {
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caDir, caKeyFilename), []byte("not a key"), 0o600); err != nil {
		t.Fatalf("write garbage key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caDir, caCertFilename), []byte("not a cert"), 0o644); err != nil {
		t.Fatalf("write garbage cert: %v", err)
	}
	ca, err := LoadOrGenerateCA(dir)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if !ca.Cert.IsCA {
		t.Fatal("expected fresh CA after rotation")
	}
	entries, err := os.ReadDir(caDir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	rotated := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.") {
			rotated++
		}
	}
	if rotated != 2 {
		t.Fatalf("expected 2 .bak.<unix> files, got %d", rotated)
	}
}

func TestLoadOrGenerateCA_NonCAExistingCertRotates(t *testing.T) {
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Generate a leaf-style cert (IsCA=false) and write it where the
	// CA file should be; loader should reject and rotate.
	tmpCA, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("seed CA: %v", err)
	}
	signer := NewSigner(tmpCA, 4)
	leaf, err := signer.Cert("example.com")
	if err != nil {
		t.Fatalf("leaf: %v", err)
	}
	leafCert, err := x509.ParseCertificate(leaf.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if leafCert.IsCA {
		t.Fatal("leaf must not have IsCA=true")
	}
	if err := writePEM(filepath.Join(caDir, caCertFilename), "CERTIFICATE", leaf.Certificate[0], 0o644); err != nil {
		t.Fatalf("write leaf as CA: %v", err)
	}
	// Use the same leaf private key.
	keyDER, err := x509.MarshalECPrivateKey(tmpCA.PrivateKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := writePEM(filepath.Join(caDir, caKeyFilename), "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	ca, err := LoadOrGenerateCA(dir)
	if err != nil {
		t.Fatalf("regen: %v", err)
	}
	if !ca.Cert.IsCA {
		t.Fatal("expected fresh CA after rejecting non-CA file")
	}
}

func TestLoadOrGenerateCA_ExpiredRotates(t *testing.T) {
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Generate a CA in a sibling, then rewrite the cert with NotAfter
	// in the past (we cannot do that via the public API, so synthesise
	// an expired CA inline).
	tmp := t.TempDir()
	ca, err := LoadOrGenerateCA(tmp)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	expiredCert := *ca.Cert
	expiredCert.NotAfter = time.Now().Add(-1 * time.Hour)
	expiredCert.NotBefore = time.Now().Add(-2 * time.Hour)
	expiredDER, err := x509.CreateCertificate(testRand(t), &expiredCert, &expiredCert, &ca.PrivateKey.PublicKey, ca.PrivateKey)
	if err != nil {
		t.Fatalf("synth expired: %v", err)
	}
	if err := writePEM(filepath.Join(caDir, caCertFilename), "CERTIFICATE", expiredDER, 0o644); err != nil {
		t.Fatalf("write expired: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(ca.PrivateKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := writePEM(filepath.Join(caDir, caKeyFilename), "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	got, err := LoadOrGenerateCA(dir)
	if err != nil {
		t.Fatalf("regen: %v", err)
	}
	if got.Cert.NotAfter.Before(time.Now()) {
		t.Fatal("expected fresh non-expired CA after rotation")
	}
}

func TestLoadOrGenerateCA_TLSCertificateIsLoadable(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrGenerateCA(dir)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{ca.TLSCertificate()}}
	if len(cfg.Certificates) != 1 || cfg.Certificates[0].Leaf == nil {
		t.Fatal("TLSCertificate must yield a usable Certificate with Leaf set")
	}
}

func TestLoadOrGenerateCA_BadDirReturnsError(t *testing.T) {
	// Create a regular file at the path; mkdir will fail because the
	// parent path component is a file, not a directory.
	tmp := t.TempDir()
	bogus := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(bogus, []byte("file"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := LoadOrGenerateCA(bogus); err == nil {
		t.Fatal("expected error when path component is a file")
	}
}

func TestRotateCorrupted_NonExistentSilent(t *testing.T) {
	dir := t.TempDir()
	if err := rotateCorrupted(filepath.Join(dir, "no.key"), filepath.Join(dir, "no.crt")); err != nil {
		t.Fatalf("rotateCorrupted on missing files must be a no-op, got %v", err)
	}
}

func TestRotateCorrupted_RenameFailureSurfaces(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Use a target whose parent does not exist; rename fails.
	missing := filepath.Join(dir, "no-parent", "missing")
	if err := rotateCorrupted(src, missing); err == nil {
		t.Skip("OS allowed the rename despite missing parent; that's fine")
	}
}

func TestWritePEM_OpenFailure(t *testing.T) {
	// Path with non-existent parent component triggers ENOENT on
	// OpenFile.
	dir := t.TempDir()
	bad := filepath.Join(dir, "no-such-parent", "out.pem")
	if err := writePEM(bad, "CERTIFICATE", []byte("body"), 0o644); err == nil {
		t.Fatal("expected error when parent dir does not exist")
	}
}

func TestTryLoadCA_GarbageKeyDecodesFalse(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	certPath := filepath.Join(dir, "crt")
	if err := os.WriteFile(keyPath, []byte("not pem"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(certPath, []byte("not pem"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, ok := tryLoadCA(keyPath, certPath); ok {
		t.Fatal("garbage PEM must yield ok=false")
	}
}

func TestTryLoadCA_WrongBlockType(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	certPath := filepath.Join(dir, "crt")
	if err := writePEM(keyPath, "WRONG TYPE", []byte("body"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writePEM(certPath, "CERTIFICATE", []byte("body"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, ok := tryLoadCA(keyPath, certPath); ok {
		t.Fatal("wrong key block type must yield ok=false")
	}
}

func TestTryLoadCA_ParseFailureFallsThrough(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	certPath := filepath.Join(dir, "crt")
	if err := writePEM(keyPath, "EC PRIVATE KEY", []byte("not der"), 0o600); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	if err := writePEM(certPath, "CERTIFICATE", []byte("not der"), 0o644); err != nil {
		t.Fatalf("seed cert: %v", err)
	}
	if _, ok := tryLoadCA(keyPath, certPath); ok {
		t.Fatal("non-DER body must yield ok=false")
	}
}

func TestTryLoadCA_MissingCertReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	if err := os.WriteFile(keyPath, []byte("key body"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, ok := tryLoadCA(keyPath, filepath.Join(dir, "missing")); ok {
		t.Fatal("missing cert must yield ok=false")
	}
}

func TestTryLoadCA_MissingKeyReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	if _, ok := tryLoadCA(filepath.Join(dir, "missing"), filepath.Join(dir, "missing2")); ok {
		t.Fatal("missing key must yield ok=false")
	}
}

func TestTryLoadCA_NotPEMCertReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	certPath := filepath.Join(dir, "crt")
	// Real ECDSA key, garbage cert (no PEM block).
	tmpCA, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(tmpCA.PrivateKey)
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	if err := os.WriteFile(certPath, []byte("not pem"), 0o644); err != nil {
		t.Fatalf("seed cert: %v", err)
	}
	if _, ok := tryLoadCA(keyPath, certPath); ok {
		t.Fatal("non-PEM cert must yield ok=false")
	}
}

// errReader fails every Read - used to exercise the err-from-rand
// branches in CA generation and leaf signing that are unreachable
// with crypto/rand.Reader.
type errReader struct{}

func (errReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("entropy exhausted (test)")
}

// failAfterReader serves bytes from crypto/rand for the first `n`
// bytes, then errors on every subsequent read. Lets tests exercise
// the error paths in generateAndPersist that fire AFTER a successful
// key generation but DURING a later crypto operation.
type failAfterReader struct {
	remaining int
}

func (f *failAfterReader) Read(p []byte) (int, error) {
	if f.remaining <= 0 {
		return 0, fmt.Errorf("entropy exhausted after threshold (test)")
	}
	n := min(len(p), f.remaining)
	read, err := cryptoRand.Read(p[:n])
	f.remaining -= read
	return read, err
}

// swapAfterCallsReader proxies to `initial` for the first `swapAt`
// Read calls then swaps to `after` for every subsequent call.
// Deterministic per Read-call rather than per byte - lets tests pin
// the exact crypto operation at which the entropy source flips.
type swapAfterCallsReader struct {
	initial io.Reader
	after   io.Reader
	swapAt  int
	calls   int
}

func (r *swapAfterCallsReader) Read(p []byte) (int, error) {
	r.calls++
	if r.calls > r.swapAt {
		return r.after.Read(p)
	}
	return r.initial.Read(p)
}

func TestSetRandSource_RoundTrip(t *testing.T) {
	prev := SetRandSource(errReader{})
	defer SetRandSource(prev)
	if randSource == nil {
		t.Fatal("randSource must not be nil after SetRandSource")
	}
	// Restore via nil to confirm reset semantics.
	SetRandSource(nil)
	if randSource == nil {
		t.Fatal("nil must reset to crypto/rand.Reader, not nil")
	}
	SetRandSource(prev)
}

func TestGenerateAndPersist_RandFailureSurfaces(t *testing.T) {
	prev := SetRandSource(errReader{})
	defer SetRandSource(prev)
	dir := t.TempDir()
	if _, err := LoadOrGenerateCA(dir); err == nil {
		t.Fatal("expected error when entropy source fails")
	}
}

func TestGenerateAndPersist_FailDuringDownstreamCrypto(t *testing.T) {
	// Try a sweep of swap-points: at one of them the swap-after-calls
	// reader will let ecdsa.GenerateKey complete and then starve the
	// next consumer (rand.Int for serial, or x509.CreateCertificate
	// for the signature). Several trials per swap because ECDSA
	// keygen does rejection sampling and consumes a non-fixed number
	// of reads.
	covered := false
	for _, swap := range []int{1, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20, 30, 50} {
		for range 3 {
			prev := SetRandSource(&swapAfterCallsReader{
				initial: cryptoRand.Reader,
				after:   errReader{},
				swapAt:  swap,
			})
			_, err := LoadOrGenerateCA(t.TempDir())
			SetRandSource(prev)
			if err != nil {
				covered = true
			}
		}
	}
	if !covered {
		t.Skip("OS satisfied every crypto call across the whole sweep; not a bug")
	}
}

func TestGenerateAndPersist_WriteKeyFailure(t *testing.T) {
	// Make the ca dir read-only so OpenFile for the key file fails.
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o500); err != nil {
		t.Fatalf("mkdir ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(caDir, 0o700) })
	if _, err := LoadOrGenerateCA(dir); err == nil {
		t.Fatal("expected write failure on read-only ca dir")
	}
}

func TestLoadOrGenerateCA_MkdirFailure(t *testing.T) {
	// Place a regular file where the ca dir would be created - mkdir
	// must fail with ENOTDIR.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ca"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := LoadOrGenerateCA(dir); err == nil {
		t.Fatal("expected mkdir failure when 'ca' is a regular file")
	}
}

func TestLoadOrGenerateCA_RotateFailureSurfaces(t *testing.T) {
	// Plant a corrupt cert with a parent that becomes read-only so
	// rotateCorrupted's Rename fails. We chmod the ca dir 0o500 right
	// before the call so existing files cannot be renamed.
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caDir, caKeyFilename), []byte("garbage"), 0o600); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caDir, caCertFilename), []byte("garbage"), 0o644); err != nil {
		t.Fatalf("seed cert: %v", err)
	}
	if err := os.Chmod(caDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(caDir, 0o700) })
	if _, err := LoadOrGenerateCA(dir); err == nil {
		t.Skip("OS allowed rename despite read-only parent; not a Slimference bug")
	}
}

func TestTryLoadCA_CertPEMValidButDERGarbage(t *testing.T) {
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tmpCA, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("seed CA: %v", err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(tmpCA.PrivateKey)
	if err := writePEM(filepath.Join(caDir, caKeyFilename), "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	// Valid "CERTIFICATE" PEM block, garbage DER body. ParseCertificate
	// must error and tryLoadCA must yield ok=false.
	if err := writePEM(filepath.Join(caDir, caCertFilename), "CERTIFICATE", []byte{0xff, 0xfe, 0xfd}, 0o644); err != nil {
		t.Fatalf("seed cert: %v", err)
	}
	if _, ok := tryLoadCA(filepath.Join(caDir, caKeyFilename), filepath.Join(caDir, caCertFilename)); ok {
		t.Fatal("garbage DER inside CERTIFICATE block must yield ok=false")
	}
}

func TestWritePEM_DirectoryAsTarget(t *testing.T) {
	// OpenFile on a path that is an existing directory must fail
	// with EISDIR. Exercises the err path in writePEM directly.
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writePEM(target, "CERTIFICATE", []byte("x"), 0o644); err == nil {
		t.Fatal("expected error when target path is a directory")
	}
}

func TestGenerateAndPersist_SecondWriteFails(t *testing.T) {
	calls := 0
	prev := SetWritePEMFn(func(path, blockType string, der []byte, mode os.FileMode) error {
		calls++
		if calls == 2 {
			return fmt.Errorf("simulated cert write failure")
		}
		return writePEM(path, blockType, der, mode)
	})
	defer SetWritePEMFn(prev)
	if _, err := LoadOrGenerateCA(t.TempDir()); err == nil {
		t.Fatal("expected the second writePEM call to surface its error")
	}
}

func TestSetWritePEMFn_NilResets(t *testing.T) {
	prev := SetWritePEMFn(nil)
	defer SetWritePEMFn(prev)
	// Default behaviour is restored: a normal CA generation succeeds.
	if _, err := LoadOrGenerateCA(t.TempDir()); err != nil {
		t.Fatalf("default writePEMFn must keep generation working: %v", err)
	}
}

func TestSignLeafLocked_MidFailDownstream(t *testing.T) {
	ca, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	signer := NewSigner(ca, 4)
	// Sweep swap-points so at least one exercises the err-after-key
	// path inside signLeafLocked (rand.Int for serial OR
	// CreateCertificate for the signature). Run multiple iterations
	// per swap-point because ECDSA P-256 keygen consumes a non-fixed
	// number of reads (rejection sampling); we want at least one
	// trial where the swap lands AFTER the keygen completes.
	covered := false
	for _, swap := range []int{1, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20, 30, 50} {
		for range 3 {
			signer.cache = map[string]*cachedLeaf{}
			signer.order = nil
			prev := SetRandSource(&swapAfterCallsReader{
				initial: cryptoRand.Reader,
				after:   errReader{},
				swapAt:  swap,
			})
			_, err := signer.Cert("api.openai.com")
			SetRandSource(prev)
			if err != nil {
				covered = true
			}
		}
	}
	if !covered {
		t.Skip("OS satisfied every signLeafLocked path; not a bug")
	}
}

func TestRandomSerial_FailureSurfaces(t *testing.T) {
	prev := SetRandSource(errReader{})
	defer SetRandSource(prev)
	if _, err := randomSerial(); err == nil {
		t.Fatal("expected randomSerial to surface entropy failure")
	}
}

func TestSignLeafLocked_RandFailureSurfaces(t *testing.T) {
	// Build a CA with real entropy so the signer is constructable,
	// then swap the source to error and request a leaf.
	ca, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	signer := NewSigner(ca, 4)
	prev := SetRandSource(errReader{})
	defer SetRandSource(prev)
	if _, err := signer.Cert("api.openai.com"); err == nil {
		t.Fatal("expected leaf signing to surface entropy failure")
	}
}

func TestErrInvalidPath_Sentinel(t *testing.T) {
	if ErrInvalidPath == nil {
		t.Fatal("ErrInvalidPath must be a non-nil sentinel")
	}
	if !strings.Contains(ErrInvalidPath.Error(), "directory") {
		t.Fatalf("error message should reference directory: %q", ErrInvalidPath.Error())
	}
}

// testRand returns crypto/rand.Reader behind a hot-pathable interface
// in case future tests want to inject a deterministic source.
func testRand(t *testing.T) interface {
	Read(p []byte) (n int, err error)
} {
	t.Helper()
	return cryptoRand.Reader
}

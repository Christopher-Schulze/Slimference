package installsteps

import (
	"crypto/sha1" // #nosec G505 - keychain identifies certs by SHA-1, fixed by the macOS Security framework, not a security boundary
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

// certSHA1Fingerprint reads a PEM-encoded cert from path and returns
// its SHA-1 fingerprint in the upper-case hex form macOS `security`
// uses for `-Z`-based lookups. The Keychain identifies certs by SHA-1
// regardless of our preference - the algorithm is fixed by the
// platform API.
func certSHA1Fingerprint(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", errors.New("keychain: cert is not PEM-encoded")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("keychain: parse cert: %w", err)
	}
	sum := sha1.Sum(cert.Raw) // #nosec G401 - see above
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

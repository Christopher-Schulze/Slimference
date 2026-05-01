package tlsca

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"strings"
)

// Fingerprint returns the SHA-256 fingerprint of the CA root in the
// canonical hex-colon format that `security` (macOS) emits, for
// example `AB:CD:EF:...`. `slimference proxy status` prints this so
// the operator can cross-check Keychain Access against the CA we
// generated.
func Fingerprint(ca *CA) string {
	if ca == nil || ca.Cert == nil {
		return ""
	}
	sum := sha256.Sum256(ca.Cert.Raw)
	return formatColons(hex.EncodeToString(sum[:]))
}

// SHA1Fingerprint returns the SHA-1 fingerprint in colon-separated
// upper-case hex. macOS `security delete-certificate -Z <sha1>`
// expects this format. SHA-1 is weak as a hash but is the canonical
// keychain identifier on macOS, so we accept the legacy here.
func SHA1Fingerprint(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	sum := sha1.Sum(cert.Raw)
	return formatColons(hex.EncodeToString(sum[:]))
}

func formatColons(hexStr string) string {
	hexStr = strings.ToUpper(hexStr)
	if len(hexStr)%2 != 0 {
		return hexStr
	}
	var sb strings.Builder
	sb.Grow(len(hexStr) + len(hexStr)/2)
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(hexStr[i : i+2])
	}
	return sb.String()
}

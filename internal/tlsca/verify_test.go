package tlsca

import (
	"crypto/x509"
	"strings"
	"testing"
)

func TestFingerprint_Format(t *testing.T) {
	ca, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	got := Fingerprint(ca)
	if got == "" {
		t.Fatal("empty fingerprint")
	}
	parts := strings.Split(got, ":")
	if len(parts) != 32 { // SHA-256 = 32 bytes
		t.Fatalf("expected 32 hex pairs, got %d (%q)", len(parts), got)
	}
	for _, p := range parts {
		if len(p) != 2 {
			t.Fatalf("each pair must be 2 hex chars: %q", p)
		}
		if strings.ToUpper(p) != p {
			t.Fatalf("fingerprint must be upper-case: %q", p)
		}
	}
}

func TestFingerprint_NilCA(t *testing.T) {
	if got := Fingerprint(nil); got != "" {
		t.Fatalf("nil CA must yield empty string, got %q", got)
	}
}

func TestFingerprint_NilCert(t *testing.T) {
	if got := Fingerprint(&CA{}); got != "" {
		t.Fatalf("CA with nil Cert must yield empty string, got %q", got)
	}
}

func TestSHA1Fingerprint_Format(t *testing.T) {
	ca, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	got := SHA1Fingerprint(ca.Cert)
	parts := strings.Split(got, ":")
	if len(parts) != 20 { // SHA-1 = 20 bytes
		t.Fatalf("expected 20 hex pairs, got %d (%q)", len(parts), got)
	}
}

func TestSHA1Fingerprint_NilCert(t *testing.T) {
	var c *x509.Certificate
	if got := SHA1Fingerprint(c); got != "" {
		t.Fatalf("nil cert must yield empty string, got %q", got)
	}
}

func TestFormatColons_OddLengthFallsThrough(t *testing.T) {
	got := formatColons("ABC")
	if got != "ABC" {
		t.Fatalf("odd-length input must round-trip unchanged, got %q", got)
	}
}

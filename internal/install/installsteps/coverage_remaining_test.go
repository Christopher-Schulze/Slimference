package installsteps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
)

func TestCertSHA1FingerprintRejectsInvalidDER(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.crt")
	if err := os.WriteFile(path, []byte("-----BEGIN CERTIFICATE-----\nAQID\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := certSHA1Fingerprint(path); err == nil || !strings.Contains(err.Error(), "parse cert") {
		t.Fatalf("fingerprint err=%v, want parse cert", err)
	}
}

func TestKeychainInspectUntrustedRunnerReturnsAbsent(t *testing.T) {
	dir := t.TempDir()
	cert := writeTestCA(t, dir)
	step := &KeychainTrust{CertPath: cert, Runner: (&fakeRunner{err: os.ErrPermission, out: []byte("CSSMERR_TP_NOT_TRUSTED")}).run}
	if got := step.Inspect(context.Background()); got != reversibility.StateAbsent {
		t.Fatalf("inspect=%v want absent for known untrusted cert", got)
	}
}

func TestNoticeReverseRemoveError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can remove from read-only directories")
	}
	n, dir := newNotice(t)
	if err := n.Apply(context.Background()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := n.Reverse(context.Background()); err == nil {
		t.Fatal("expected remove permission error")
	}
}

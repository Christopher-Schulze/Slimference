package installsteps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/control/reversibility"
	"github.com/slimference/slimference/internal/tlsca"
)

// writeTestCA generates a fresh CA + writes root.crt to dir. Returns
// the cert path.
func writeTestCA(t *testing.T, dir string) string {
	t.Helper()
	if _, err := tlsca.LoadOrGenerateCA(dir); err != nil {
		t.Fatalf("LoadOrGenerateCA: %v", err)
	}
	return filepath.Join(dir, "ca", "root.crt")
}

// fakeRunner records every call and returns canned output.
type fakeRunner struct {
	calls [][]string
	err   error
	out   []byte
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	return f.out, f.err
}

func TestKeychainApplyHappyPath(t *testing.T) {
	dir := t.TempDir()
	cert := writeTestCA(t, dir)
	fr := &fakeRunner{}
	step := &KeychainTrust{CertPath: cert, Scope: ScopeUser, Runner: fr.run}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 call, got %d (%v)", len(fr.calls), fr.calls)
	}
	if fr.calls[0][0] != "security" || fr.calls[0][1] != "add-trusted-cert" {
		t.Errorf("first call wrong: %v", fr.calls[0])
	}
	if step.fingerprintSHA1 == "" {
		t.Error("fingerprint not cached after Apply")
	}
	if !strings.HasPrefix(step.fingerprintSHA1, strings.ToUpper(step.fingerprintSHA1[:1])) {
		t.Errorf("fingerprint not upper-case: %q", step.fingerprintSHA1)
	}
}

func TestKeychainApplyContextCancelled(t *testing.T) {
	step := &KeychainTrust{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := step.Apply(ctx); err == nil {
		t.Fatal("expected ctx.Err()")
	}
}

func TestKeychainApplyMissingCertRejected(t *testing.T) {
	step := &KeychainTrust{CertPath: "/nonexistent/cert.crt", Runner: (&fakeRunner{}).run}
	if err := step.Apply(context.Background()); err == nil {
		t.Fatal("expected error on missing cert")
	}
}

func TestKeychainApplyEmptyPathRejected(t *testing.T) {
	step := &KeychainTrust{Runner: (&fakeRunner{}).run}
	if err := step.Apply(context.Background()); err == nil {
		t.Fatal("expected error on empty CertPath")
	}
}

func TestKeychainApplyRunnerErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	cert := writeTestCA(t, dir)
	fr := &fakeRunner{err: errors.New("security failed")}
	step := &KeychainTrust{CertPath: cert, Scope: ScopeUser, Runner: fr.run}
	if err := step.Apply(context.Background()); err == nil {
		t.Fatal("expected runner error to propagate")
	}
}

func TestKeychainApplyBadCertFingerprintErrorAfterInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.crt")
	if err := os.WriteFile(path, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	step := &KeychainTrust{CertPath: path, Scope: ScopeUser, Runner: (&fakeRunner{}).run}
	if err := step.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("Apply err=%v, want fingerprint error", err)
	}
}

func TestKeychainReverseHappyPath(t *testing.T) {
	dir := t.TempDir()
	cert := writeTestCA(t, dir)
	fr := &fakeRunner{}
	step := &KeychainTrust{CertPath: cert, Scope: ScopeUser, Runner: fr.run}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(fr.calls))
	}
	if fr.calls[1][1] != "delete-certificate" {
		t.Errorf("second call wrong: %v", fr.calls[1])
	}
}

func TestKeychainReverseWithoutApplyComputesFingerprint(t *testing.T) {
	dir := t.TempDir()
	cert := writeTestCA(t, dir)
	fr := &fakeRunner{}
	step := &KeychainTrust{CertPath: cert, Scope: ScopeUser, Runner: fr.run}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.calls))
	}
}

func TestKeychainReverseCertMissingIsNoOp(t *testing.T) {
	step := &KeychainTrust{CertPath: "/nonexistent/cert.crt", Runner: (&fakeRunner{}).run}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatalf("Reverse on missing cert: %v", err)
	}
}

func TestKeychainReverseContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	step := &KeychainTrust{CertPath: "x"}
	if err := step.Reverse(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reverse canceled err=%v", err)
	}
}

func TestKeychainReverseBadCertFingerprintError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.crt")
	if err := os.WriteFile(path, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	step := &KeychainTrust{CertPath: path, Runner: (&fakeRunner{}).run}
	if err := step.Reverse(context.Background()); err == nil {
		t.Fatal("expected fingerprint parse error")
	}
}

func TestKeychainReverseRunnerErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	cert := writeTestCA(t, dir)
	fr := &fakeRunner{err: errors.New("delete failed")}
	step := &KeychainTrust{CertPath: cert, Scope: ScopeUser, Runner: fr.run}
	if err := step.Reverse(context.Background()); err == nil || !strings.Contains(err.Error(), "uninstall") {
		t.Fatalf("Reverse err=%v, want uninstall error", err)
	}
}

func TestKeychainReverseRunnerNotFoundIsNoOp(t *testing.T) {
	dir := t.TempDir()
	cert := writeTestCA(t, dir)
	fr := &fakeRunner{err: errors.New("not found"), out: []byte("could not be found")}
	step := &KeychainTrust{CertPath: cert, Scope: ScopeUser, Runner: fr.run}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatalf("Reverse not-found should be nil, got %v", err)
	}
}

func TestKeychainReverseEmptyPathErrors(t *testing.T) {
	step := &KeychainTrust{}
	if err := step.Reverse(context.Background()); err == nil {
		t.Fatal("expected error on empty CertPath")
	}
}

func TestKeychainInspectAbsentWhenMissing(t *testing.T) {
	step := &KeychainTrust{CertPath: "/nonexistent/cert.crt"}
	if got := step.Inspect(context.Background()); got != reversibility.StateAbsent {
		t.Errorf("got %v want StateAbsent", got)
	}
}

func TestKeychainInspectUnknownEmptyPath(t *testing.T) {
	step := &KeychainTrust{}
	if got := step.Inspect(context.Background()); got != reversibility.StateUnknown {
		t.Errorf("got %v want StateUnknown", got)
	}
}

func TestKeychainInspectTrustedAndUnknownVerify(t *testing.T) {
	dir := t.TempDir()
	cert := writeTestCA(t, dir)
	present := &KeychainTrust{CertPath: cert, Runner: (&fakeRunner{}).run}
	if got := present.Inspect(context.Background()); got != reversibility.StatePresent {
		t.Fatalf("trusted inspect=%v want present", got)
	}
	unknown := &KeychainTrust{CertPath: cert, Runner: (&fakeRunner{err: errors.New("verify failed"), out: []byte("bad")}).run}
	if got := unknown.Inspect(context.Background()); got != reversibility.StateUnknown {
		t.Fatalf("verify-error inspect=%v want unknown", got)
	}
}

func TestKeychainNameStable(t *testing.T) {
	step := &KeychainTrust{}
	if step.Name() != "ca.keychain" {
		t.Errorf("Name=%q", step.Name())
	}
}

func TestKeychainScopeSelection(t *testing.T) {
	dir := t.TempDir()
	cert := writeTestCA(t, dir)
	fr := &fakeRunner{}
	step := &KeychainTrust{CertPath: cert, Scope: ScopeSystem, Runner: fr.run}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	hasSystemKeychain := false
	for _, a := range fr.calls[0] {
		if strings.Contains(a, "/Library/Keychains/System.keychain") {
			hasSystemKeychain = true
		}
	}
	if !hasSystemKeychain {
		t.Errorf("ScopeSystem did not target System.keychain (%v)", fr.calls[0])
	}
}

func TestCertSHA1FingerprintMissingFile(t *testing.T) {
	if _, err := certSHA1Fingerprint("/nonexistent.crt"); err == nil {
		t.Fatal("expected error on missing file")
	}
}

func TestCertSHA1FingerprintBadPEM(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bogus.crt")
	if err := os.WriteFile(tmp, []byte("not a cert"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := certSHA1Fingerprint(tmp); err == nil {
		t.Fatal("expected error on non-PEM input")
	}
}

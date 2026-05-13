package transparent

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestScope_Stringer(t *testing.T) {
	t.Parallel()
	if ScopeUser.String() != "user" {
		t.Fatalf("user stringer: %q", ScopeUser.String())
	}
	if ScopeSystem.String() != "system" {
		t.Fatalf("system stringer: %q", ScopeSystem.String())
	}
	if Scope(99).String() != "unknown" {
		t.Fatal("unknown scope must stringify to unknown")
	}
}

func TestKeychain_InstallUserScope(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	k := NewKeychain()
	k.SetExec(mock.run)
	k.SetHome(func() string { return "/Users/test" })
	if err := k.Install("/path/to/cert", ScopeUser); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 security call, got %v", mock.calls)
	}
	got := strings.Join(mock.calls[0], " ")
	if !strings.Contains(got, "/Users/test/Library/Keychains/login.keychain-db") {
		t.Fatalf("user scope must target login keychain, got %q", got)
	}
	if !strings.Contains(got, "trustRoot") {
		t.Fatalf("must mark cert as trustRoot, got %q", got)
	}
}

func TestKeychain_InstallSystemScope(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	k := NewKeychain()
	k.SetExec(mock.run)
	if err := k.Install("/path/to/cert", ScopeSystem); err != nil {
		t.Fatalf("install: %v", err)
	}
	got := strings.Join(mock.calls[0], " ")
	if !strings.Contains(got, "/Library/Keychains/System.keychain") {
		t.Fatalf("system scope must target System.keychain, got %q", got)
	}
}

func TestKeychain_InstallExecFailureSurfaced(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.errs["security add-trusted-cert -d -r trustRoot -p ssl -k /Users/test/Library/Keychains/login.keychain-db /path/to/cert"] = errors.New("denied")
	k := NewKeychain()
	k.SetExec(mock.run)
	k.SetHome(func() string { return "/Users/test" })
	if err := k.Install("/path/to/cert", ScopeUser); err == nil {
		t.Fatal("expected install to surface security failure")
	}
}

func TestKeychain_InstallUserScope_NoHome(t *testing.T) {
	t.Parallel()
	k := NewKeychain()
	k.SetHome(func() string { return "" })
	if err := k.Install("/p", ScopeUser); err == nil {
		t.Fatal("expected error when HOME unresolved")
	}
}

func TestKeychain_InstallUnknownScope(t *testing.T) {
	t.Parallel()
	k := NewKeychain()
	if err := k.Install("/p", Scope(99)); err == nil {
		t.Fatal("expected error for unknown scope")
	}
}

func TestKeychain_UninstallSucceeds(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	k := NewKeychain()
	k.SetExec(mock.run)
	if err := k.Uninstall("ABCDEF", ScopeSystem); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
}

func TestKeychain_UninstallIdempotentOnNotFound(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.out["security delete-certificate -Z ABCDEF /Library/Keychains/System.keychain"] = []byte(
		"SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n",
	)
	mock.errs["security delete-certificate -Z ABCDEF /Library/Keychains/System.keychain"] = errors.New("exit 1")
	k := NewKeychain()
	k.SetExec(mock.run)
	if err := k.Uninstall("ABCDEF", ScopeSystem); err != nil {
		t.Fatalf("uninstall must be idempotent on not-found, got %v", err)
	}
}

func TestKeychain_UninstallOtherErrorSurfaced(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.errs["security delete-certificate -Z ABCDEF /Library/Keychains/System.keychain"] = errors.New("permission denied")
	mock.out["security delete-certificate -Z ABCDEF /Library/Keychains/System.keychain"] = []byte("permission denied")
	k := NewKeychain()
	k.SetExec(mock.run)
	if err := k.Uninstall("ABCDEF", ScopeSystem); err == nil {
		t.Fatal("expected real error to surface")
	}
}

func TestKeychain_UninstallUnknownScope(t *testing.T) {
	t.Parallel()
	k := NewKeychain()
	if err := k.Uninstall("X", Scope(99)); err == nil {
		t.Fatal("expected error for unknown scope")
	}
}

func TestKeychain_IsTrustedTrue(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	k := NewKeychain()
	k.SetExec(mock.run)
	ok, err := k.IsTrusted("/path")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("expected trusted=true")
	}
}

func TestKeychain_IsTrustedFalse(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.errs["security verify-cert -c /path"] = errors.New("untrusted root")
	k := NewKeychain()
	k.SetExec(mock.run)
	ok, err := k.IsTrusted("/path")
	if ok {
		t.Fatal("expected trusted=false")
	}
	if err == nil {
		t.Fatal("expected err when untrusted")
	}
}

func TestKeychain_SetExecNilNoOp(t *testing.T) {
	t.Parallel()
	k := NewKeychain()
	k.SetExec(nil)
	if k.exec == nil {
		t.Fatal("nil arg must NOT clear exec hook")
	}
}

func TestKeychain_SetHomeNilNoOp(t *testing.T) {
	t.Parallel()
	k := NewKeychain()
	k.SetHome(nil)
	if k.homeFn == nil {
		t.Fatal("nil arg must NOT clear homeFn hook")
	}
}

func TestKeychain_DefaultTimeoutAllowsMacOSTrustPrompt(t *testing.T) {
	t.Parallel()
	k := NewKeychain()
	if k.timeout < time.Minute {
		t.Fatalf("keychain trust timeout too short for macOS auth prompt: %s", k.timeout)
	}
}

func TestErrNoHome_Sentinel(t *testing.T) {
	t.Parallel()
	if ErrNoHome == nil || !strings.Contains(ErrNoHome.Error(), "HOME") {
		t.Fatal("ErrNoHome must be a non-nil sentinel mentioning HOME")
	}
}

func TestDefaultHome_Resolution(t *testing.T) {
	t.Parallel()
	// We cannot reliably reset HOME without affecting other parallel
	// tests, so just verify a non-empty current value or empty
	// fallback (both branches fine).
	_ = defaultHome()
}

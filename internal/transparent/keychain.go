package transparent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Scope decides which macOS keychain the CA certificate is added to.
// User scope (login.keychain-db) is the default - it does NOT need
// sudo and only affects the current user. System scope (System.keychain)
// requires sudo and trusts the cert for every user on the machine.
type Scope int

const (
	// ScopeUser puts the trusted cert in ~/Library/Keychains/login.keychain-db.
	ScopeUser Scope = iota
	// ScopeSystem puts the trusted cert in /Library/Keychains/System.keychain.
	ScopeSystem
)

func (s Scope) String() string {
	switch s {
	case ScopeUser:
		return "user"
	case ScopeSystem:
		return "system"
	default:
		return "unknown"
	}
}

// Keychain wraps macOS `security` invocations needed to install /
// uninstall / verify the Slimference CA cert.
type Keychain struct {
	exec    func(ctx context.Context, name string, args ...string) ([]byte, error)
	timeout time.Duration
	homeFn  func() string
}

// NewKeychain returns a Keychain wired to the production `security`
// binary with a 10-second per-command timeout.
func NewKeychain() *Keychain {
	return &Keychain{
		exec:    runCommand,
		timeout: 10 * time.Second,
		homeFn:  defaultHome,
	}
}

// SetExec overrides the command runner; tests pin this so no real
// `security` binary runs during go test.
func (k *Keychain) SetExec(fn func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	if fn != nil {
		k.exec = fn
	}
}

// SetHome overrides the resolution of the current user's home dir.
func (k *Keychain) SetHome(fn func() string) {
	if fn != nil {
		k.homeFn = fn
	}
}

// Install adds the cert at certPath to the chosen keychain as a
// trusted root for SSL. Idempotent: re-installing a cert with the
// same SHA-1 succeeds without error.
func (k *Keychain) Install(certPath string, scope Scope) error {
	keychain := k.keychainPath(scope)
	if keychain == "" {
		return fmt.Errorf("transparent: cannot resolve keychain for scope %s", scope)
	}
	args := []string{"add-trusted-cert", "-d", "-r", "trustRoot", "-k", keychain, certPath}
	if _, err := k.run(args); err != nil {
		return fmt.Errorf("transparent: add-trusted-cert: %w", err)
	}
	return nil
}

// Uninstall removes the cert (identified by SHA-1) from the chosen
// keychain. Returns nil if the cert is not present.
func (k *Keychain) Uninstall(certSHA1 string, scope Scope) error {
	keychain := k.keychainPath(scope)
	if keychain == "" {
		return fmt.Errorf("transparent: cannot resolve keychain for scope %s", scope)
	}
	args := []string{"delete-certificate", "-Z", certSHA1, keychain}
	out, err := k.run(args)
	if err != nil {
		// `security` returns non-zero when the cert is not found.
		// Treat that as success so uninstall is idempotent.
		if strings.Contains(string(out), "could not be found") {
			return nil
		}
		return fmt.Errorf("transparent: delete-certificate: %w (output: %s)", err, string(out))
	}
	return nil
}

// IsTrusted reports whether the cert at certPath chains back to a
// trusted root in the system. macOS `security verify-cert` returns 0
// when trusted.
func (k *Keychain) IsTrusted(certPath string) (bool, error) {
	out, err := k.run([]string{"verify-cert", "-c", certPath})
	if err == nil {
		return true, nil
	}
	// When the cert is untrusted, `security` returns a non-zero exit
	// code; we still surface stdout/stderr so the caller can render
	// it for the operator.
	return false, fmt.Errorf("transparent: verify-cert: %w (output: %s)", err, string(out))
}

func (k *Keychain) keychainPath(scope Scope) string {
	switch scope {
	case ScopeUser:
		home := k.homeFn()
		if home == "" {
			return ""
		}
		return home + "/Library/Keychains/login.keychain-db"
	case ScopeSystem:
		return "/Library/Keychains/System.keychain"
	default:
		return ""
	}
}

func (k *Keychain) run(args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), k.timeout)
	defer cancel()
	return k.exec(ctx, "security", args...)
}

// ErrNoHome is the sentinel returned when the user's home directory
// could not be determined.
var ErrNoHome = errors.New("transparent: HOME unresolved")

// Package installsteps contains reversibility.Step implementations
// that wrap pre-existing install primitives (Keychain, hooks) so they
// can run inside an install.Plan with atomic Apply/Reverse semantics.
package installsteps

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/slimference/slimference/internal/control/reversibility"
	"github.com/slimference/slimference/internal/transparent"
)

// KeychainScope mirrors transparent.Scope so callers don't have to
// import the transparent package directly.
type KeychainScope int

const (
	// ScopeUser is the per-user login.keychain-db. No admin needed.
	ScopeUser KeychainScope = iota
	// ScopeSystem is the system-wide keychain. Requires admin.
	ScopeSystem
)

// scope() converts the public KeychainScope to the internal
// transparent.Scope.
func (s KeychainScope) scope() transparent.Scope {
	if s == ScopeSystem {
		return transparent.ScopeSystem
	}
	return transparent.ScopeUser
}

// KeychainRunner is the exec hook used to drive the macOS `security`
// binary. Tests inject a stub that records or fakes invocations.
type KeychainRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// KeychainTrust adds (Apply) / removes (Reverse) a CA root cert from
// the macOS Keychain. Backed by the existing transparent.Keychain
// helper.
//
// Idempotency: Apply re-installing the same cert succeeds (`security
// add-trusted-cert` returns 0 for the dup). Reverse with a missing
// cert is treated as success.
type KeychainTrust struct {
	// CertPath is the path to the root.crt to trust.
	CertPath string
	// Scope picks user vs system keychain.
	Scope KeychainScope
	// Runner injects the `security` exec for tests. Nil = production.
	Runner KeychainRunner

	// fingerprint cached after Apply for Reverse.
	fingerprintSHA1 string
}

const keychainStepName = "ca.keychain"

// Name implements reversibility.Step.
func (s *KeychainTrust) Name() string { return keychainStepName }

// Apply adds CertPath to the Keychain as a trusted SSL root.
func (s *KeychainTrust) Apply(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.validate(); err != nil {
		return err
	}
	k := s.keychain()
	if err := k.Install(s.CertPath, s.Scope.scope()); err != nil {
		return fmt.Errorf("keychain install: %w", err)
	}
	fp, err := certSHA1Fingerprint(s.CertPath)
	if err != nil {
		return fmt.Errorf("keychain fingerprint: %w", err)
	}
	s.fingerprintSHA1 = fp
	return nil
}

// Reverse removes the CertPath from the Keychain. Idempotent.
func (s *KeychainTrust) Reverse(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.CertPath == "" {
		return errors.New("keychain: CertPath empty")
	}
	fp := s.fingerprintSHA1
	if fp == "" {
		// Allow Reverse to compute the fingerprint from disk so we
		// don't need Apply to have run in this process.
		got, err := certSHA1Fingerprint(s.CertPath)
		if err != nil {
			// Cert file gone: nothing to remove (Reverse is idempotent).
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		fp = got
	}
	k := s.keychain()
	if err := k.Uninstall(fp, s.Scope.scope()); err != nil {
		return fmt.Errorf("keychain uninstall: %w", err)
	}
	return nil
}

// Inspect reports whether the cert is currently trusted.
func (s *KeychainTrust) Inspect(ctx context.Context) reversibility.StepState {
	if s.CertPath == "" {
		return reversibility.StateUnknown
	}
	if _, err := os.Stat(s.CertPath); err != nil {
		return reversibility.StateAbsent
	}
	k := s.keychain()
	trusted, err := k.IsTrusted(s.CertPath)
	if err != nil {
		return reversibility.StateUnknown
	}
	if trusted {
		return reversibility.StatePresent
	}
	return reversibility.StateAbsent
}

func (s *KeychainTrust) validate() error {
	if s.CertPath == "" {
		return errors.New("keychain: CertPath empty")
	}
	if _, err := os.Stat(s.CertPath); err != nil {
		return fmt.Errorf("keychain: cert path: %w", err)
	}
	return nil
}

func (s *KeychainTrust) keychain() *transparent.Keychain {
	k := transparent.NewKeychain()
	if s.Runner != nil {
		k.SetExec(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return s.Runner(ctx, name, args...)
		})
	}
	return k
}

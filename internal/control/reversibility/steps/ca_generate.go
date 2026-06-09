// Package steps contains concrete `reversibility.Step` implementations
// for the Slimference install/uninstall plan.
//
// Each Step is idempotent (Apply twice is a no-op the second time;
// Reverse on a not-applied Step is a no-op success) and inspectable
// (Inspect reports the current state without modifying anything).
package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
	"github.com/Christopher-Schulze/Slimference/internal/tlsca"
)

// CAGenerate is the install step that materialises the local
// Slimference Root CA. It uses the existing internal/tlsca generation
// path: a fresh ECDSA P-256 CA written to `<Dir>/ca/root.{key,crt}`.
// Reverse moves the CA aside (does NOT delete) so the operator can
// recover the previously-trusted fingerprint if they need to.
type CAGenerate struct {
	// Dir is the Slimference data directory (typically
	// `~/.slimference/`). CA materializes under `<Dir>/ca/`.
	Dir string
	// Clock overrides time.Now for tests.
	Clock func() time.Time
}

const caGenerateStepName = "ca.generate"

// Name implements reversibility.Step.
func (s *CAGenerate) Name() string { return caGenerateStepName }

// Apply generates (or re-uses an already-valid) CA under
// `<Dir>/ca/`. Idempotent: a fresh-and-valid CA on disk is left
// alone; an expired one is rotated automatically by
// tlsca.LoadOrGenerateCA.
func (s *CAGenerate) Apply(ctx context.Context) error {
	if s.Dir == "" {
		return errors.New("steps: CAGenerate Dir empty")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := tlsca.LoadOrGenerateCA(s.Dir); err != nil {
		return fmt.Errorf("steps: CA generate: %w", err)
	}
	return nil
}

// Reverse moves the CA files aside with a timestamp suffix so an
// operator can re-trust them later if needed. Does not delete the
// private key (deletion is the operator's call via
// `slimference ca purge`).
func (s *CAGenerate) Reverse(ctx context.Context) error {
	if s.Dir == "" {
		return errors.New("steps: CAGenerate Dir empty")
	}
	caDir := filepath.Join(s.Dir, "ca")
	keyPath := filepath.Join(caDir, "root.key")
	certPath := filepath.Join(caDir, "root.crt")
	now := time.Now
	if s.Clock != nil {
		now = s.Clock
	}
	suffix := fmt.Sprintf(".bak.%d", now().Unix())

	// Move each file aside if it exists. Idempotent: missing file =
	// nothing to do.
	for _, p := range []string{keyPath, certPath} {
		if _, err := os.Stat(p); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("steps: stat %s: %w", p, err)
		}
		if err := os.Rename(p, p+suffix); err != nil {
			return fmt.Errorf("steps: move %s aside: %w", p, err)
		}
	}
	return nil
}

// Inspect reports whether a valid CA exists in the expected location.
func (s *CAGenerate) Inspect(ctx context.Context) reversibility.StepState {
	if s.Dir == "" {
		return reversibility.StateUnknown
	}
	keyPath := filepath.Join(s.Dir, "ca", "root.key")
	certPath := filepath.Join(s.Dir, "ca", "root.crt")
	keyOK, certOK := false, false
	if info, err := os.Stat(keyPath); err == nil && info.Mode().IsRegular() {
		keyOK = true
	}
	if info, err := os.Stat(certPath); err == nil && info.Mode().IsRegular() {
		certOK = true
	}
	switch {
	case keyOK && certOK:
		return reversibility.StatePresent
	case keyOK || certOK:
		return reversibility.StatePartial
	default:
		return reversibility.StateAbsent
	}
}

// Ensure CAGenerate satisfies the Step interface at compile time.
var _ reversibility.Step = (*CAGenerate)(nil)

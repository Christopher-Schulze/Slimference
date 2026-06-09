package steps

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
)

// hostsBeginMarker / hostsEndMarker fence the Slimference-managed
// block in /etc/hosts. Reverse strips the fenced block; Apply replaces
// or inserts it. Lines outside the fence are never touched.
const (
	hostsBeginMarker = "# >>> slimference managed: do not edit between markers >>>"
	hostsEndMarker   = "# <<< slimference managed <<<"
)

// HostsPatch is the install step that points the configured hosts at
// our transparent listener. With Slimference armed, `chatgpt.com` and
// friends resolve to `127.0.0.1` so our :443 listener intercepts the
// TLS connection.
//
// Apply is idempotent and Reverse leaves /etc/hosts byte-equal to the
// pre-install state (modulo the time-stamped fence comment line we
// add for forensics).
type HostsPatch struct {
	// Path is the file to patch. Defaults to /etc/hosts when empty.
	// Tests pass a temp path.
	Path string
	// Targets is the list of domains to redirect, e.g.
	// ["chatgpt.com", "api.openai.com"].
	Targets []string
	// Address is the loopback to redirect to (typically "127.0.0.1").
	Address string
	// Clock overrides time.Now for tests.
	Clock func() time.Time
	// BackupDir is where the pre-patch snapshot lands. Defaults to
	// "<Path>.slimference.bak" alongside the original.
	BackupDir string

	mu sync.Mutex
}

const hostsPatchStepName = "hosts.patch"

// Name implements reversibility.Step.
func (s *HostsPatch) Name() string { return hostsPatchStepName }

// Apply rewrites the hosts file so the fenced Slimference block is
// present and lists every target in s.Targets pointing at s.Address.
// Idempotent: re-running keeps a single block, refreshes timestamps,
// and does not duplicate entries.
func (s *HostsPatch) Apply(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.validate(); err != nil {
		return err
	}
	original, err := os.ReadFile(s.path())
	if err != nil {
		return fmt.Errorf("steps: read %s: %w", s.path(), err)
	}
	if err := s.writeBackup(original); err != nil {
		return err
	}
	// stripManagedBlock always terminates lines with `\n` or returns
	// an empty string, so the concatenation below is well-formed
	// without a separate newline-fix-up step.
	stripped := stripManagedBlock(string(original))
	block := s.renderBlock()
	updated := stripped + block
	if err := writeAtomic(s.path(), []byte(updated), 0o644); err != nil {
		return fmt.Errorf("steps: write hosts: %w", err)
	}
	return nil
}

// Reverse strips the Slimference-managed block from the hosts file.
// If we have a recent backup, we restore from it (byte-equal restore).
// Otherwise we fall back to stripping the marker fence.
func (s *HostsPatch) Reverse(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validate(); err != nil {
		return err
	}
	if backup := s.backupPath(); backup != "" {
		if data, err := os.ReadFile(backup); err == nil {
			if err := writeAtomic(s.path(), data, 0o644); err != nil {
				return fmt.Errorf("steps: restore hosts from backup: %w", err)
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("steps: read backup %s: %w", backup, err)
		}
	}
	current, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("steps: read hosts: %w", err)
	}
	cleaned := stripManagedBlock(string(current))
	if err := writeAtomic(s.path(), []byte(cleaned), 0o644); err != nil {
		return fmt.Errorf("steps: write hosts: %w", err)
	}
	return nil
}

// Inspect reports whether the managed block is present in the file.
// `s.path()` always resolves (defaults to /etc/hosts when Path is
// empty) so we don't need a separate emptiness branch here.
func (s *HostsPatch) Inspect(ctx context.Context) reversibility.StepState {
	data, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return reversibility.StateAbsent
		}
		return reversibility.StateUnknown
	}
	if !strings.Contains(string(data), hostsBeginMarker) {
		return reversibility.StateAbsent
	}
	want := s.renderBlock()
	if strings.Contains(string(data), strings.TrimSpace(want)) {
		return reversibility.StatePresent
	}
	return reversibility.StatePartial
}

func (s *HostsPatch) validate() error {
	// path() always resolves (defaults to /etc/hosts when Path is
	// empty) so no empty-path branch.
	if len(s.Targets) == 0 {
		return errors.New("steps: HostsPatch targets empty")
	}
	if s.Address == "" {
		return errors.New("steps: HostsPatch address empty")
	}
	return nil
}

func (s *HostsPatch) path() string {
	if s.Path != "" {
		return s.Path
	}
	return "/etc/hosts"
}

func (s *HostsPatch) backupPath() string {
	if s.BackupDir != "" {
		return filepath.Join(s.BackupDir, "hosts.slimference.bak")
	}
	return s.path() + ".slimference.bak"
}

func (s *HostsPatch) writeBackup(original []byte) error {
	backup := s.backupPath()
	if _, err := os.Stat(backup); err == nil {
		// Backup already exists - keep the oldest one (the
		// genuinely-pristine pre-Slimference state).
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return fmt.Errorf("steps: backup mkdir: %w", err)
	}
	if err := os.WriteFile(backup, original, 0o644); err != nil {
		return fmt.Errorf("steps: backup write: %w", err)
	}
	return nil
}

func (s *HostsPatch) renderBlock() string {
	now := time.Now
	if s.Clock != nil {
		now = s.Clock
	}
	var b strings.Builder
	b.WriteString(hostsBeginMarker)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("# Generated %s\n", now().UTC().Format(time.RFC3339)))
	for _, t := range s.Targets {
		b.WriteString(fmt.Sprintf("%s\t%s\n", s.Address, t))
	}
	b.WriteString(hostsEndMarker)
	b.WriteString("\n")
	return b.String()
}

// stripManagedBlock removes every fenced Slimference block from the
// input. Lines outside the fence and any non-Slimference content are
// preserved byte-equal.
func stripManagedBlock(in string) string {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(in))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inside := false
	for scanner.Scan() {
		line := scanner.Text()
		if !inside && strings.HasPrefix(strings.TrimSpace(line), hostsBeginMarker) {
			inside = true
			continue
		}
		if inside && strings.HasPrefix(strings.TrimSpace(line), hostsEndMarker) {
			inside = false
			continue
		}
		if inside {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

// writeAtomic writes content to path via a tmp-rename to avoid
// half-written files on power loss / kill.
func writeAtomic(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := createAtomicTempFileFn(dir, ".slimference.tmp.*")
	if err != nil {
		return err
	}
	defer removeAtomicFileFn(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return renameAtomicFileFn(tmp.Name(), path)
}

type atomicTempFile interface {
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Close() error
	Name() string
}

var (
	createAtomicTempFileFn = func(dir, pattern string) (atomicTempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	removeAtomicFileFn = os.Remove
	renameAtomicFileFn = os.Rename
)

var _ reversibility.Step = (*HostsPatch)(nil)

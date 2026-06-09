package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
)

// FileBackup is a generic install step that:
//
//  1. snapshots a file's pre-patch content to `<BackupDir>/<basename>.
//     slimference.<unix>.bak` on first Apply (oldest backup wins on
//     repeat applies, preserving the genuinely-pristine state);
//  2. patches the file by applying `Patch` to the content (Patch must
//     return the new content);
//  3. on Reverse, restores from the oldest backup if present.
//
// It is the workhorse for Codex `config.toml` and `hooks.json` patches.
// Both files have a header comment block we own (marker-fenced) so the
// patch is idempotent.
type FileBackup struct {
	// Path is the file to manage.
	Path string
	// BackupDir overrides the location of the snapshot. Defaults to
	// `~/.slimference/backups/`.
	BackupDir string
	// Patch transforms the on-disk content. Called on Apply with the
	// current bytes; should return the new bytes. Must be idempotent
	// (Patch(Patch(x)) == Patch(x)).
	Patch func([]byte) []byte
	// InspectMarker is a substring that should be present after Apply
	// and absent before Apply. Drives Inspect's state report.
	InspectMarker string
	// Name overrides the step's user-visible name.
	StepName string
	// Clock overrides time.Now for the backup filename suffix.
	Clock func() time.Time

	// Required indicates the file must exist on Apply. When false,
	// missing input is treated as a no-op (the file isn't there, so
	// there's nothing for us to patch).
	Required bool
}

// Name implements reversibility.Step.
func (s *FileBackup) Name() string {
	if s.StepName != "" {
		return s.StepName
	}
	return "file.backup." + filepath.Base(s.Path)
}

// Apply snapshots + patches.
func (s *FileBackup) Apply(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	original, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if s.Required {
				return fmt.Errorf("steps: required file missing: %s", s.Path)
			}
			return nil
		}
		return fmt.Errorf("steps: read %s: %w", s.Path, err)
	}
	if err := s.maybeSnapshot(original); err != nil {
		return err
	}
	patched := s.Patch(original)
	if len(patched) == 0 {
		patched = original
	}
	if string(patched) == string(original) {
		return nil // patch was a no-op
	}
	if err := writeAtomic(s.Path, patched, 0o644); err != nil {
		return fmt.Errorf("steps: write %s: %w", s.Path, err)
	}
	return nil
}

// Reverse restores from the oldest snapshot we know about, or removes
// the file if the snapshot indicates the file didn't exist
// pre-install (zero-byte sentinel).
func (s *FileBackup) Reverse(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	backup, err := s.oldestBackup()
	if err != nil {
		return err
	}
	if backup == "" {
		return nil
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("steps: read backup %s: %w", backup, err)
	}
	if err := writeAtomic(s.Path, data, 0o644); err != nil {
		return fmt.Errorf("steps: restore %s: %w", s.Path, err)
	}
	return nil
}

// Inspect reports whether the patched marker is present.
func (s *FileBackup) Inspect(ctx context.Context) reversibility.StepState {
	if s.Path == "" {
		return reversibility.StateUnknown
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return reversibility.StateAbsent
		}
		return reversibility.StateUnknown
	}
	if s.InspectMarker == "" {
		return reversibility.StatePresent // can't tell, assume present
	}
	if strings.Contains(string(data), s.InspectMarker) {
		return reversibility.StatePresent
	}
	return reversibility.StateAbsent
}

func (s *FileBackup) validate() error {
	if s.Path == "" {
		return errors.New("steps: FileBackup Path empty")
	}
	if s.Patch == nil {
		return errors.New("steps: FileBackup Patch nil")
	}
	return nil
}

func (s *FileBackup) backupDir() (string, error) {
	if s.BackupDir != "" {
		return s.BackupDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".slimference", "backups"), nil
}

func (s *FileBackup) clockNow() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

func (s *FileBackup) maybeSnapshot(original []byte) error {
	dir, err := s.backupDir()
	if err != nil {
		return fmt.Errorf("steps: backup dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("steps: mkdir backup: %w", err)
	}
	// Skip if any backup for this file already exists - we want the
	// OLDEST (i.e. truly pristine) snapshot.
	existing, err := s.findBackups(dir)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	name := fmt.Sprintf("%s.slimference.%d.bak", filepath.Base(s.Path),
		s.clockNow().UnixNano())
	return os.WriteFile(filepath.Join(dir, name), original, 0o644)
}

func (s *FileBackup) oldestBackup() (string, error) {
	dir, err := s.backupDir()
	if err != nil {
		return "", err
	}
	backups, err := s.findBackups(dir)
	if err != nil {
		return "", err
	}
	if len(backups) == 0 {
		return "", nil
	}
	// findBackups returns sorted by name (timestamp) ascending.
	return filepath.Join(dir, backups[0]), nil
}

func (s *FileBackup) findBackups(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("steps: read backup dir: %w", err)
	}
	base := filepath.Base(s.Path)
	prefix := base + ".slimference."
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".bak") {
			out = append(out, n)
		}
	}
	// os.ReadDir returns entries sorted by filename, so backup names are
	// already oldest-first because the timestamp sits in the sortable
	// filename stem.
	return out, nil
}

var _ reversibility.Step = (*FileBackup)(nil)

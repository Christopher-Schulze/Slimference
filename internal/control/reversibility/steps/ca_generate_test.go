package steps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
)

func TestCAGenerateApplyMaterialisesCA(t *testing.T) {
	dir := t.TempDir()
	step := &CAGenerate{Dir: dir}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, fn := range []string{"root.key", "root.crt"} {
		p := filepath.Join(dir, "ca", fn)
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected %s: %v", p, err)
		}
		if !info.Mode().IsRegular() {
			t.Errorf("%s not regular file", p)
		}
	}
	if step.Inspect(context.Background()) != reversibility.StatePresent {
		t.Errorf("Inspect after Apply should be present")
	}
}

func TestCAGenerateApplyIdempotent(t *testing.T) {
	dir := t.TempDir()
	step := &CAGenerate{Dir: dir}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	certBefore, _ := os.ReadFile(filepath.Join(dir, "ca", "root.crt"))
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	certAfter, _ := os.ReadFile(filepath.Join(dir, "ca", "root.crt"))
	if string(certBefore) != string(certAfter) {
		t.Errorf("second Apply rotated the CA unexpectedly")
	}
}

func TestCAGenerateReverseMovesFilesAside(t *testing.T) {
	dir := t.TempDir()
	step := &CAGenerate{
		Dir:   dir,
		Clock: func() time.Time { return time.Unix(1717000000, 0) },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatalf("reverse: %v", err)
	}
	for _, fn := range []string{"root.key", "root.crt"} {
		current := filepath.Join(dir, "ca", fn)
		if _, err := os.Stat(current); !os.IsNotExist(err) {
			t.Errorf("%s still present after reverse: err=%v", current, err)
		}
		moved := current + ".bak.1717000000"
		if _, err := os.Stat(moved); err != nil {
			t.Errorf("moved file %s missing: %v", moved, err)
		}
	}
}

func TestCAGenerateReverseIdempotent(t *testing.T) {
	dir := t.TempDir()
	step := &CAGenerate{Dir: dir}
	// Reverse on non-existent CA must succeed.
	if err := step.Reverse(context.Background()); err != nil {
		t.Errorf("reverse on empty dir: %v", err)
	}
	// Apply + Reverse twice.
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Errorf("second reverse: %v", err)
	}
}

func TestCAGenerateInspectStates(t *testing.T) {
	dir := t.TempDir()
	step := &CAGenerate{Dir: dir}
	if s := step.Inspect(context.Background()); s != reversibility.StateAbsent {
		t.Errorf("fresh dir: got %s want absent", s)
	}
	// Partial state: only key present.
	if err := os.MkdirAll(filepath.Join(dir, "ca"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca", "root.key"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := step.Inspect(context.Background()); s != reversibility.StatePartial {
		t.Errorf("only key: got %s want partial", s)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca", "root.crt"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := step.Inspect(context.Background()); s != reversibility.StatePresent {
		t.Errorf("both files: got %s want present", s)
	}
}

func TestCAGenerateEmptyDirRejected(t *testing.T) {
	step := &CAGenerate{}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("Apply: expected error on empty Dir")
	}
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("Reverse: expected error on empty Dir")
	}
	if s := step.Inspect(context.Background()); s != reversibility.StateUnknown {
		t.Errorf("Inspect: got %s want unknown", s)
	}
}

func TestCAGenerateApplyContextCancelled(t *testing.T) {
	step := &CAGenerate{Dir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := step.Apply(ctx); err == nil {
		t.Errorf("expected context error")
	}
}

func TestCAGenerateReverseStatErrorPropagates(t *testing.T) {
	// Make a parent directory that exists but is unreadable. Stat
	// fails with a non-ENOENT error → Reverse returns it.
	if os.Getuid() == 0 {
		t.Skip("running as root - chmod-based test won't work")
	}
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caDir, "root.key"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(caDir, 0o000); err != nil {
		t.Skipf("chmod 000 unsupported: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(caDir, 0o700) })
	step := &CAGenerate{Dir: dir}
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected stat error to surface")
	}
}

func TestCAGenerateApplyMkdirError(t *testing.T) {
	// Dir's parent is a regular file → mkdir fails inside LoadOrGenerateCA.
	dir := t.TempDir()
	bogus := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(bogus, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	step := &CAGenerate{Dir: bogus}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected mkdir error")
	}
}

func TestCAGenerateReverseSkipsAfterRotation(t *testing.T) {
	// Reverse twice when both files were moved aside: the .bak files
	// remain, the current files are gone, Reverse is a no-op success.
	dir := t.TempDir()
	step := &CAGenerate{Dir: dir, Clock: func() time.Time { return time.Unix(100, 0) }}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Errorf("idempotent reverse: %v", err)
	}
	// Confirm the .bak files survive (we don't delete them on second Reverse).
	if matches, _ := filepath.Glob(filepath.Join(dir, "ca", "*.bak.100")); len(matches) != 2 {
		t.Errorf("expected 2 bak files, got %d (%v)", len(matches), matches)
	}
}

func TestCAGenerateReverseRenameError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root - chmod-based test won't work")
	}
	dir := t.TempDir()
	step := &CAGenerate{Dir: dir, Clock: func() time.Time { return time.Unix(1, 0) }}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	caDir := filepath.Join(dir, "ca")
	if err := os.Chmod(caDir, 0o500); err != nil {
		t.Skipf("chmod unsupported: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(caDir, 0o700) })
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected rename error on read-only dir")
	}
}

func TestCAGenerateNameStable(t *testing.T) {
	step := &CAGenerate{Dir: t.TempDir()}
	if !strings.Contains(step.Name(), "ca") {
		t.Errorf("step name should contain 'ca': %q", step.Name())
	}
}

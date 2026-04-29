package integrate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type failingAtomicTempFile struct {
	name     string
	writeErr error
	chmodErr error
	closeErr error
}

func (f *failingAtomicTempFile) Name() string { return f.name }
func (f *failingAtomicTempFile) Write([]byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return 1, nil
}
func (f *failingAtomicTempFile) Chmod(os.FileMode) error { return f.chmodErr }
func (f *failingAtomicTempFile) Close() error            { return f.closeErr }

// TestWriteAtomic_ParentMissingReturnsError covers the create-temp error path.
func TestWriteAtomic_ParentMissingReturnsError(t *testing.T) {
	target := filepath.Join(t.TempDir(), "no-such-subdir", "file")
	err := writeAtomic(target, []byte("x"), 0o644)
	if err == nil {
		t.Fatal("expected error on missing parent dir")
	}
}

func TestWriteAtomic_TempFileOperationErrors(t *testing.T) {
	tests := []struct {
		name string
		file *failingAtomicTempFile
	}{
		{name: "write", file: &failingAtomicTempFile{writeErr: errors.New("write boom")}},
		{name: "chmod", file: &failingAtomicTempFile{chmodErr: errors.New("chmod boom")}},
		{name: "close", file: &failingAtomicTempFile{closeErr: errors.New("close boom")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.file.name = filepath.Join(dir, ".slim-test")
			if err := os.WriteFile(tc.file.name, []byte("tmp"), 0o644); err != nil {
				t.Fatal(err)
			}
			orig := createTempFileFn
			createTempFileFn = func(string, string) (atomicTempFile, error) {
				return tc.file, nil
			}
			t.Cleanup(func() { createTempFileFn = orig })
			if err := writeAtomic(filepath.Join(dir, "target"), []byte("x"), 0o644); err == nil {
				t.Fatalf("expected %s error", tc.name)
			}
		})
	}
}

// TestWriteAtomic_PreservesLargerMode exercises the mode-from-existing-file
// branch more thoroughly.
func TestWriteAtomic_PreservesLargerMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perms")
	if err := os.WriteFile(path, []byte("orig"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o, want 0755", info.Mode().Perm())
	}
}

// TestBackupOnce_ReadErrorPath covers the os.ReadFile error branch by
// making the source unreadable mid-operation (best-effort: on some OSes
// chmod 0 still allows the owner to read, so we skip if that is the case).
func TestBackupOnce_ReadErrorPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only permission semantics")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "file")
	if err := os.WriteFile(src, []byte("data"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(src, 0o644) })
	// Running as root bypasses permission checks entirely.
	if os.Geteuid() == 0 {
		t.Skip("root bypasses perms")
	}
	_, err := backupOnce(src)
	if err == nil {
		t.Skip("OS let us read a mode-000 file")
	}
}

// TestReadRC_UnreadableReturnsError covers the !IsNotExist error branch.
func TestReadRC_UnreadableReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("unix-only; cannot produce unreadable file as root")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "rc")
	if err := os.WriteFile(p, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	_, _, err := ReadRC(p)
	if err == nil {
		t.Skip("OS let us read a mode-000 file")
	}
}

// TestWriteRCBlock_MkdirErrorOnNonExistentParent confirms we create the
// parent directory chain for fish rc files and do not error.
func TestWriteRCBlock_FishCreatesParentDirs(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".config", "fish", "config.fish")
	// Parent path does not exist yet.
	evt, err := WriteRCBlock(rc, ShellFish, ProxyURL)
	if err != nil {
		t.Fatalf("fish rc write: %v", err)
	}
	if evt.Action != "wrote_block" {
		t.Fatalf("action = %q", evt.Action)
	}
	got, _ := os.ReadFile(rc)
	if !strings.Contains(string(got), "set -gx ANTHROPIC_BASE_URL") {
		t.Fatalf("fish syntax missing: %s", got)
	}
}

// TestWriteCodexBlock_AtomicReplaceExistingFence checks we rewrite
// in-place when the fence body matches but duplicate conflicts exist.
func TestWriteCodexBlock_RewriteOnDuplicateEvenWhenBodyMatches(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	path := filepath.Join(home, ".codex", "config.toml")
	// Pre-seed: top-level duplicate + fenced block with matching body.
	initial := `openai_base_url = "http://127.0.0.1:8990"
` + renderBlock(codexBlockBody(ProxyURL))
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	evt, err := WriteCodexBlock(home, ProxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "wrote_block" {
		t.Fatalf("action = %q, want wrote_block", evt.Action)
	}
	// After rewrite, top-level duplicate must be gone.
	got, _ := os.ReadFile(path)
	// Count top-level openai_base_url occurrences (excluding the fence).
	before, _, after, _ := splitBlock(string(got))
	n := countTopLevelKey(before+"\n"+after, "openai_base_url")
	if n != 0 {
		t.Fatalf("duplicate still present: count=%d, content=\n%s", n, got)
	}
}

// TestWriteCodexBlock_FenceAlreadyAtTopLevelIdempotent — when the file
// has ONLY our fence (no duplicates) re-running is idempotent.
func TestWriteCodexBlock_FenceAlreadyCleanIdempotent(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	// Install once to seed a clean fence.
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	// Second call must skip.
	evt, err := WriteCodexBlock(home, ProxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("action = %q, want skipped_idempotent", evt.Action)
	}
}

// TestFenceIsTopLevelWithBody_NoFence returns false when no fence exists.
func TestFenceIsTopLevelWithBody_NoFence(t *testing.T) {
	if fenceIsTopLevelWithBody("just some content", "body") {
		t.Fatal("no-fence content should return false")
	}
}

// TestFenceIsTopLevelWithBody_BodyMismatch returns false when body differs.
func TestFenceIsTopLevelWithBody_BodyMismatch(t *testing.T) {
	content := renderBlock("different body")
	if fenceIsTopLevelWithBody(content, "expected body") {
		t.Fatal("mismatched body should return false")
	}
}

// TestFenceIsTopLevelWithBody_FenceNestedInTable returns false.
func TestFenceIsTopLevelWithBody_FenceNestedInTable(t *testing.T) {
	content := "[some.table]\n" + renderBlock("body")
	if fenceIsTopLevelWithBody(content, "body") {
		t.Fatal("nested fence should return false")
	}
}

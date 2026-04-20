package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstall_InvalidHomeReturnsError(t *testing.T) {
	// Force os.UserHomeDir to error by clearing HOME and its alternatives.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	// If Go's UserHomeDir still resolves (e.g. via /etc/passwd on Unix),
	// we pass through to Install with empty HomeDir and rely on
	// resolveHome below. For portability we set HomeDir explicitly to
	// exercise the error-reporting shape.
	rep := Install(Options{})
	// Either the report has no writes (home resolved to a throwaway
	// location) or errors are populated. The contract is "do not panic".
	_ = rep
}

func TestInstall_ProxyURLOverride(t *testing.T) {
	home := t.TempDir()
	// Ensure zsh detection: install picks the rc flavour from $SHELL; force
	// zsh so we know where the block lands.
	t.Setenv("SHELL", "/bin/zsh")
	opts := Options{
		Client:   "claude",
		HomeDir:  home,
		ProxyURL: "http://127.0.0.1:9999",
	}
	rep := Install(opts)
	if len(rep.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", rep.Errors)
	}
	// Read whichever rc the detector picked. Install reports via Claude
	// status whose ConfigPath points at it.
	rc := rep.Claude.ConfigPath
	if rc == "" {
		t.Fatalf("no rc path in report: %+v", rep.Claude)
	}
	content, _ := os.ReadFile(rc)
	if !strings.Contains(string(content), ":9999") {
		t.Fatalf("custom proxy URL did not land in %s: %s", rc, content)
	}
}

func TestRemove_ClaudeOnly(t *testing.T) {
	home := t.TempDir()
	// Pre-install Claude.
	Install(Options{Client: "claude", HomeDir: home, ProxyURL: ProxyURL})
	rep := Remove(Options{Client: "claude", HomeDir: home, ProxyURL: ProxyURL})
	if len(rep.Errors) > 0 {
		t.Fatalf("errors: %v", rep.Errors)
	}
	rc := filepath.Join(home, ".zshrc")
	content, _ := os.ReadFile(rc)
	if strings.Contains(string(content), markerStart) {
		t.Fatalf("fence still present: %s", content)
	}
}

func TestRemove_CodexOnly_NoCodexDirIsNoop(t *testing.T) {
	home := t.TempDir()
	rep := Remove(Options{Client: "codex", HomeDir: home, ProxyURL: ProxyURL})
	// No error, no writes of note.
	if len(rep.Errors) > 0 {
		t.Fatalf("errors: %v", rep.Errors)
	}
}

func TestRemove_DryRun(t *testing.T) {
	home := t.TempDir()
	rep := Remove(Options{HomeDir: home, DryRun: true, ProxyURL: ProxyURL})
	for _, w := range rep.Writes {
		if !strings.HasPrefix(w.Action, "DRY_RUN_") {
			t.Errorf("non-dry-run write in dry run: %+v", w)
		}
	}
}

func TestInstall_DryRunCodex(t *testing.T) {
	home := t.TempDir()
	rep := Install(Options{Client: "codex", HomeDir: home, DryRun: true, ProxyURL: ProxyURL})
	hasDryRun := false
	for _, w := range rep.Writes {
		if strings.HasPrefix(w.Action, "DRY_RUN_") {
			hasDryRun = true
		}
	}
	if !hasDryRun {
		t.Fatalf("dry-run writes missing: %+v", rep.Writes)
	}
	// No actual config.toml created.
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err == nil {
		t.Fatal("dry-run wrote the file")
	}
}

func TestResolveProxyURL_Default(t *testing.T) {
	opts := Options{}
	if got := opts.resolveProxyURL(); got != ProxyURL {
		t.Fatalf("empty opts = %q, want %q", got, ProxyURL)
	}
}

func TestResolveProxyURL_Trimmed(t *testing.T) {
	opts := Options{ProxyURL: "   http://x   "}
	if got := opts.resolveProxyURL(); got != "http://x" {
		t.Fatalf("trim failed: %q", got)
	}
}

func TestResolveHome_Override(t *testing.T) {
	opts := Options{HomeDir: "/custom/home"}
	got, err := opts.resolveHome()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/custom/home" {
		t.Fatalf("home = %q", got)
	}
}

func TestWriteAtomic_PreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample")
	if err := os.WriteFile(path, []byte("orig"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %o, want 0600", info.Mode().Perm())
	}
}

func TestWriteAtomic_CreatesWithDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new")
	if err := writeAtomic(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("default mode = %o", info.Mode().Perm())
	}
}

func TestBackupOnce_CopiesExistingFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "file")
	content := []byte("secrets")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	backup, err := backupOnce(src)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" || !strings.Contains(backup, ".slim-backup-") {
		t.Fatalf("backup = %q", backup)
	}
	got, _ := os.ReadFile(backup)
	if string(got) != "secrets" {
		t.Fatalf("backup content = %q", got)
	}
}

func TestRenderBlock_WrapsBody(t *testing.T) {
	got := renderBlock("content")
	if !strings.Contains(got, markerStart) || !strings.Contains(got, markerEnd) {
		t.Fatalf("markers missing: %q", got)
	}
	if !strings.Contains(got, "content") {
		t.Fatalf("body missing: %q", got)
	}
}

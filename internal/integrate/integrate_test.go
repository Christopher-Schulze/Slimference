package integrate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReplaceOrAppendBlock_AppendsWhenMissing(t *testing.T) {
	base := "# existing content\nexport FOO=bar\n"
	out := replaceOrAppendBlock(base, "export BAZ=qux")
	if !strings.Contains(out, markerStart) || !strings.Contains(out, markerEnd) {
		t.Fatalf("markers missing: %s", out)
	}
	if !strings.Contains(out, "export FOO=bar") {
		t.Fatal("existing content dropped")
	}
	if !strings.Contains(out, "export BAZ=qux") {
		t.Fatal("new body missing")
	}
}

func TestReplaceOrAppendBlock_ReplacesExisting(t *testing.T) {
	base := "# pre\n" + markerStart + "\nold body\n" + markerEnd + "\n# post\n"
	out := replaceOrAppendBlock(base, "new body")
	if !strings.Contains(out, "new body") {
		t.Fatal("new body missing")
	}
	if strings.Contains(out, "old body") {
		t.Fatal("old body leaked")
	}
	if !strings.Contains(out, "# pre") || !strings.Contains(out, "# post") {
		t.Fatal("surrounding content dropped")
	}
}

func TestReplaceOrAppendBlock_EmptyBodyRemoves(t *testing.T) {
	base := "keep me\n" + markerStart + "\ngo away\n" + markerEnd + "\nalso keep\n"
	out := replaceOrAppendBlock(base, "")
	if strings.Contains(out, "go away") || strings.Contains(out, markerStart) {
		t.Fatalf("removal incomplete: %q", out)
	}
	if !strings.Contains(out, "keep me") || !strings.Contains(out, "also keep") {
		t.Fatal("surrounding content lost")
	}
}

func TestReplaceOrAppendBlock_IdempotentRoundTrip(t *testing.T) {
	base := "header\n"
	body := "set FOO=bar"
	once := replaceOrAppendBlock(base, body)
	twice := replaceOrAppendBlock(once, body)
	if once != twice {
		t.Fatalf("not idempotent:\nonce:\n%s\ntwice:\n%s", once, twice)
	}
}

func TestSplitBlock_UnterminatedHandled(t *testing.T) {
	content := "pre\n" + markerStart + "\nleaked body no end\n"
	before, block, after, exists := splitBlock(content)
	if !exists {
		t.Fatal("unterminated block should be marked exists")
	}
	if !strings.Contains(block, "leaked body no end") {
		t.Fatal("block missing captured content")
	}
	if before != "pre\n" {
		t.Fatalf("before = %q", before)
	}
	_ = after
}

func TestShellFromEnv(t *testing.T) {
	cases := map[string]ShellFlavor{
		"/bin/zsh":               ShellZsh,
		"/opt/homebrew/bin/fish": ShellFish,
		"/usr/bin/bash":          ShellBash,
		"/bin/sh":                ShellBash,
		"/weird/custom-shell":    ShellUnknown,
		"":                       ShellUnknown,
	}
	for in, want := range cases {
		if got := shellFromEnv(in); got != want {
			t.Errorf("%s: got %v, want %v", in, got, want)
		}
	}
}

func TestShellFlavorString(t *testing.T) {
	cases := map[ShellFlavor]string{
		ShellZsh:        "zsh",
		ShellBash:       "bash",
		ShellFish:       "fish",
		ShellUnknown:    "unknown",
		ShellFlavor(99): "unknown",
	}
	for flavor, want := range cases {
		if got := flavor.String(); got != want {
			t.Errorf("flavor %d: got %q, want %q", int(flavor), got, want)
		}
	}
}

func TestDetectRCFile_PrefersExistingShellMatch(t *testing.T) {
	home := t.TempDir()
	// Create both zshrc and bashrc; $SHELL says zsh → zshrc wins.
	for _, n := range []string{".zshrc", ".bashrc"} {
		_ = os.WriteFile(filepath.Join(home, n), []byte("# empty\n"), 0o644)
	}
	rc := DetectRCFile(home, "/bin/zsh")
	if rc.Flavor != ShellZsh || !strings.HasSuffix(rc.Path, ".zshrc") {
		t.Fatalf("expected zshrc, got %+v", rc)
	}
}

func TestDetectRCFile_PrefersAnyExistingWhenShellUnknown(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte("# bash profile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := DetectRCFile(home, "/usr/local/bin/custom-shell")
	if rc.Flavor != ShellBash || !rc.Exists || !strings.HasSuffix(rc.Path, ".bash_profile") {
		t.Fatalf("expected existing bash_profile, got %+v", rc)
	}
}

func TestDetectRCFile_UsesShellMatchWhenMissing(t *testing.T) {
	home := t.TempDir()
	rc := DetectRCFile(home, "/usr/local/bin/fish")
	if rc.Flavor != ShellFish || rc.Exists || !strings.HasSuffix(rc.Path, filepath.Join(".config", "fish", "config.fish")) {
		t.Fatalf("expected missing fish target, got %+v", rc)
	}
}

func TestDetectRCFile_FallsBackToZshOnEmptyHome(t *testing.T) {
	home := t.TempDir()
	rc := DetectRCFile(home, "")
	if !strings.HasSuffix(rc.Path, ".zshrc") {
		t.Fatalf("expected fallback zshrc, got %+v", rc)
	}
}

func TestClaudeEnvBlockBody_FishSyntax(t *testing.T) {
	body := claudeEnvBlockBody(ShellFish, ProxyURL)
	if !strings.HasPrefix(body, "set -gx") {
		t.Fatalf("fish body wrong: %q", body)
	}
}

func TestClaudeEnvBlockBody_DefaultExport(t *testing.T) {
	body := claudeEnvBlockBody(ShellBash, ProxyURL)
	if !strings.HasPrefix(body, "export ") {
		t.Fatalf("bash body wrong: %q", body)
	}
}

func TestClaudeEnvBlockBody_BlankProxyUsesDefault(t *testing.T) {
	body := claudeEnvBlockBody(ShellZsh, "  ")
	if !strings.Contains(body, ProxyURL) {
		t.Fatalf("default proxy missing: %q", body)
	}
}

func TestWriteRCBlock_CreatesFile(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	evt, err := WriteRCBlock(rc, ShellZsh, ProxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "wrote_block" {
		t.Fatalf("action = %q", evt.Action)
	}
	content, _ := os.ReadFile(rc)
	if !strings.Contains(string(content), "ANTHROPIC_BASE_URL") {
		t.Fatalf("content: %s", content)
	}
}

func TestWriteRCBlock_ReadErrorOnDirectory(t *testing.T) {
	dirAsFile := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.Mkdir(dirAsFile, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteRCBlock(dirAsFile, ShellZsh, ProxyURL); err == nil {
		t.Fatal("expected read error for directory path")
	}
}

func TestWriteRCBlock_MkdirParentBlockedByFile(t *testing.T) {
	home := t.TempDir()
	blocker := filepath.Join(home, ".config")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(blocker, "fish", "config.fish")
	if _, err := WriteRCBlock(rc, ShellFish, ProxyURL); err == nil {
		t.Fatal("expected mkdir error when parent segment is a file")
	}
}

func TestWriteRCBlock_AtomicWriteError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix directory permissions")
	}
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if err := os.Chmod(home, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })
	if _, err := WriteRCBlock(rc, ShellZsh, ProxyURL); err == nil {
		t.Fatal("expected atomic write error")
	}
}

func TestWriteRCBlock_IdempotentSecondRun(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	_, _ = WriteRCBlock(rc, ShellZsh, ProxyURL)
	evt, err := WriteRCBlock(rc, ShellZsh, ProxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("second run not idempotent: %q", evt.Action)
	}
}

func TestWriteRCBlock_BacksUpExistingFile(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("# user content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteRCBlock(rc, ShellZsh, ProxyURL); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(home)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".zshrc.slim-backup-") {
			found = true
		}
	}
	if !found {
		t.Fatal("no backup file created")
	}
}

func TestWriteRCBlock_RewritesChangedBodyAndBacksUp(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if _, err := WriteRCBlock(rc, ShellZsh, "http://127.0.0.1:8991"); err != nil {
		t.Fatal(err)
	}
	evt, err := WriteRCBlock(rc, ShellZsh, ProxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "wrote_block" {
		t.Fatalf("action = %q", evt.Action)
	}
	content, _ := os.ReadFile(rc)
	if strings.Contains(string(content), "8991") || !strings.Contains(string(content), ProxyURL) {
		t.Fatalf("rewrite did not replace proxy URL: %s", content)
	}
	entries, _ := os.ReadDir(home)
	foundBackup := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".zshrc.slim-backup-") {
			foundBackup = true
		}
	}
	if !foundBackup {
		t.Fatal("rewrite did not create backup")
	}
}

func TestWriteRCBlock_BackupWriteError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix directory permissions")
	}
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("export OLD=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })
	if _, err := WriteRCBlock(rc, ShellZsh, ProxyURL); err == nil {
		t.Fatal("expected backup write error")
	}
}

func TestRemoveRCBlock_RoundTrip(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	_, _ = WriteRCBlock(rc, ShellZsh, ProxyURL)
	evt, err := RemoveRCBlock(rc)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "removed_block" {
		t.Fatalf("action = %q", evt.Action)
	}
	content, _ := os.ReadFile(rc)
	if strings.Contains(string(content), markerStart) {
		t.Fatalf("block still present: %s", content)
	}
}

func TestRemoveRCBlock_ExistingFileWithoutBlockSkips(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rc, []byte("export FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evt, err := RemoveRCBlock(rc)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("action = %q", evt.Action)
	}
	content, _ := os.ReadFile(rc)
	if string(content) != "export FOO=bar\n" {
		t.Fatalf("content changed: %q", string(content))
	}
}

func TestRemoveRCBlock_ReadErrorOnDirectory(t *testing.T) {
	dirAsFile := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.Mkdir(dirAsFile, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveRCBlock(dirAsFile); err == nil {
		t.Fatal("expected read error for directory path")
	}
}

func TestRemoveRCBlock_WriteErrorOnDirectoryTarget(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if _, err := WriteRCBlock(rc, ShellZsh, ProxyURL); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(rc); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(rc, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveRCBlock(rc); err == nil {
		t.Fatal("expected read error when rc path became a directory")
	}
}

func TestRemoveRCBlock_BackupWriteError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix directory permissions")
	}
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if _, err := WriteRCBlock(rc, ShellZsh, ProxyURL); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })
	if _, err := RemoveRCBlock(rc); err == nil {
		t.Fatal("expected backup write error")
	}
}

func TestRemoveRCBlock_AtomicWriteError(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if _, err := WriteRCBlock(rc, ShellZsh, ProxyURL); err != nil {
		t.Fatal(err)
	}
	orig := createTempFileFn
	createTempFileFn = func(string, string) (atomicTempFile, error) {
		return &failingAtomicTempFile{
			name:     filepath.Join(home, ".slim-test"),
			closeErr: os.ErrClosed,
		}, nil
	}
	t.Cleanup(func() { createTempFileFn = orig })
	if _, err := RemoveRCBlock(rc); err == nil {
		t.Fatal("expected atomic write error")
	}
}

func TestRemoveRCBlock_MissingFileIsIdempotent(t *testing.T) {
	evt, err := RemoveRCBlock(filepath.Join(t.TempDir(), ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("action = %q", evt.Action)
	}
}

func TestWriteCodexBlock_DirMissingSkips(t *testing.T) {
	home := t.TempDir()
	evt, err := WriteCodexBlock(home, ProxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "skipped_client_absent" {
		t.Fatalf("action = %q", evt.Action)
	}
}

func TestWriteCodexBlock_ReadErrorOnConfigDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(CodexConfigPath(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexBlock(home, ProxyURL); err == nil {
		t.Fatal("expected read error for config directory")
	}
}

func TestWriteCodexBlock_CodexDirStatError(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".codex"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexBlock(home, ProxyURL); err == nil {
		t.Fatal("expected stat/read error when .codex is a file")
	}
}

func TestWriteCodexBlock_BackupWriteError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix directory permissions")
	}
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := CodexConfigPath(home)
	if err := os.WriteFile(cfg, []byte("model = \"gpt-5.4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(codexDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(codexDir, 0o755) })
	if _, err := WriteCodexBlock(home, ProxyURL); err == nil {
		t.Fatal("expected backup write error")
	}
}

func TestWriteCodexBlock_AtomicWriteError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix directory permissions")
	}
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(codexDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(codexDir, 0o755) })
	if _, err := WriteCodexBlock(home, ProxyURL); err == nil {
		t.Fatal("expected atomic write error")
	}
}

func TestWriteCodexBlock_BlankProxyUsesDefault(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	evt, err := WriteCodexBlock(home, "  ")
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "wrote_block" {
		t.Fatalf("action = %q", evt.Action)
	}
	content, _ := os.ReadFile(CodexConfigPath(home))
	if !strings.Contains(string(content), `openai_base_url = "`+ProxyURL+`"`) {
		t.Fatalf("default openai_base_url missing: %s", content)
	}
	if !strings.Contains(string(content), `chatgpt_base_url = "`+ProxyURL+`"`) {
		t.Fatalf("default chatgpt_base_url missing: %s", content)
	}
}

func TestWriteCodexBlock_CreatesBlock(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed existing config to verify preservation.
	cfg := filepath.Join(home, ".codex", "config.toml")
	preExisting := "model = \"gpt-5.4\"\napproval_policy = \"never\"\n"
	_ = os.WriteFile(cfg, []byte(preExisting), 0o644)

	evt, err := WriteCodexBlock(home, ProxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "wrote_block" {
		t.Fatalf("action = %q", evt.Action)
	}
	content, _ := os.ReadFile(cfg)
	if !strings.Contains(string(content), "model = \"gpt-5.4\"") {
		t.Fatal("pre-existing content dropped")
	}
	if !strings.Contains(string(content), "openai_base_url") {
		t.Fatal("openai_base_url missing")
	}
	if !strings.Contains(string(content), "chatgpt_base_url") {
		t.Fatal("chatgpt_base_url missing")
	}
}

func TestWriteCodexBlock_InsertsBeforeFirstTable(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := CodexConfigPath(home)
	preExisting := "model = \"gpt-5.4\"\n\n[features]\ncodex_hooks = true\n"
	if err := os.WriteFile(cfg, []byte(preExisting), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(cfg)
	fenceIdx := strings.Index(string(content), markerStart)
	tableIdx := strings.Index(string(content), "[features]")
	if fenceIdx < 0 || tableIdx < 0 || fenceIdx > tableIdx {
		t.Fatalf("fence not before table:\n%s", content)
	}
}

func TestRemoveCodexBlock_MissingAndNoBlockAreIdempotent(t *testing.T) {
	home := t.TempDir()
	evt, err := RemoveCodexBlock(home)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("missing action = %q", evt.Action)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexConfigPath(home), []byte("model = \"gpt-5.4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evt, err = RemoveCodexBlock(home)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("no-block action = %q", evt.Action)
	}
}

func TestRemoveCodexBlock_ReadErrorOnConfigDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(CodexConfigPath(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveCodexBlock(home); err == nil {
		t.Fatal("expected read error for config directory")
	}
}

func TestRemoveCodexBlock_WriteErrorOnConfigDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	cfg := CodexConfigPath(home)
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveCodexBlock(home); err == nil {
		t.Fatal("expected read error when config path became a directory")
	}
}

func TestRemoveCodexBlock_BackupWriteError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix directory permissions")
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	codexDir := filepath.Join(home, ".codex")
	if err := os.Chmod(codexDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(codexDir, 0o755) })
	if _, err := RemoveCodexBlock(home); err == nil {
		t.Fatal("expected backup write error")
	}
}

func TestRemoveCodexBlock_AtomicWriteError(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	orig := createTempFileFn
	createTempFileFn = func(string, string) (atomicTempFile, error) {
		return &failingAtomicTempFile{
			name:     filepath.Join(home, ".codex", ".slim-test"),
			closeErr: os.ErrClosed,
		}, nil
	}
	t.Cleanup(func() { createTempFileFn = orig })
	if _, err := RemoveCodexBlock(home); err == nil {
		t.Fatal("expected atomic write error")
	}
}

func TestInstall_DryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	opts := Options{
		DryRun:   true,
		HomeDir:  home,
		ProxyURL: ProxyURL,
	}
	rep := Install(opts)
	if len(rep.Writes) == 0 {
		t.Fatal("dry-run should still report intended writes")
	}
	for _, w := range rep.Writes {
		if !strings.HasPrefix(w.Action, "DRY_RUN_") {
			t.Errorf("non-dry-run write slipped through: %+v", w)
		}
	}
	// No actual files touched.
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Fatalf("dry-run created %d entries under home", len(entries))
	}
}

func TestRemove_DryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	opts := Options{
		DryRun:   true,
		HomeDir:  home,
		ProxyURL: ProxyURL,
	}
	rep := Remove(opts)
	if len(rep.Writes) == 0 {
		t.Fatal("dry-run remove should report intended writes")
	}
	for _, w := range rep.Writes {
		if !strings.HasPrefix(w.Action, "DRY_RUN_") {
			t.Errorf("non-dry-run write slipped through: %+v", w)
		}
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Fatalf("dry-run created %d entries under home", len(entries))
	}
}

func TestStatus_HomeResolutionError(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	rep := Status(Options{})
	if len(rep.Errors) == 0 {
		t.Fatal("expected home resolution error")
	}
}

func TestRemove_HomeResolutionError(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	rep := Remove(Options{})
	if len(rep.Errors) == 0 {
		t.Fatal("expected home resolution error")
	}
}

func TestInstall_ErrorReportsForBlockedTargets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SHELL", "/usr/local/bin/fish")
	if err := os.WriteFile(filepath.Join(home, ".config"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := Install(Options{HomeDir: home, Client: "all", ProxyURL: ProxyURL})
	if len(rep.Errors) < 2 {
		t.Fatalf("expected claude and codex errors, got %+v", rep.Errors)
	}
	if len(rep.Writes) != 0 {
		t.Fatalf("expected no successful writes, got %+v", rep.Writes)
	}
}

func TestRemove_ErrorReportsForDirectoryTargets(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".zshrc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(CodexConfigPath(home), 0o755); err != nil {
		t.Fatal(err)
	}
	rep := Remove(Options{HomeDir: home, Client: "all", ProxyURL: ProxyURL})
	if len(rep.Errors) < 2 {
		t.Fatalf("expected claude and codex remove errors, got %+v", rep.Errors)
	}
}

func TestInstallRemoveRoundTrip_Codex(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := Options{HomeDir: home, Client: "codex", ProxyURL: ProxyURL}
	if rep := Install(opts); len(rep.Errors) > 0 {
		t.Fatalf("install errors: %v", rep.Errors)
	}
	content, _ := os.ReadFile(CodexConfigPath(home))
	if !strings.Contains(string(content), "chatgpt_base_url") {
		t.Fatalf("codex install did not write config block: %s", content)
	}
	if rep := Remove(opts); len(rep.Errors) > 0 {
		t.Fatalf("remove errors: %v", rep.Errors)
	}
	content, _ = os.ReadFile(CodexConfigPath(home))
	if strings.Contains(string(content), markerStart) {
		t.Fatalf("codex remove left fence: %s", content)
	}
}

func TestInstallRemoveRoundTrip_Claude(t *testing.T) {
	home := t.TempDir()
	opts := Options{HomeDir: home, Client: "claude", ProxyURL: ProxyURL}

	if rep := Install(opts); len(rep.Errors) > 0 {
		t.Fatalf("install errors: %v", rep.Errors)
	}
	rc := DetectRCFile(home, "")
	content, _ := os.ReadFile(rc.Path)
	if !strings.Contains(string(content), "ANTHROPIC_BASE_URL") {
		t.Fatal("install did not write env block")
	}

	if rep := Remove(opts); len(rep.Errors) > 0 {
		t.Fatalf("remove errors: %v", rep.Errors)
	}
	content, _ = os.ReadFile(rc.Path)
	if strings.Contains(string(content), markerStart) {
		t.Fatalf("remove left fence: %s", content)
	}
}

func TestInstallRemove_AllClients(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := Options{HomeDir: home, Client: "", ProxyURL: ProxyURL}
	install := Install(opts)
	if len(install.Errors) > 0 {
		t.Fatalf("install errors: %v", install.Errors)
	}
	if len(install.Writes) < 2 {
		t.Fatalf("expected claude and codex writes, got %+v", install.Writes)
	}
	remove := Remove(opts)
	if len(remove.Errors) > 0 {
		t.Fatalf("remove errors: %v", remove.Errors)
	}
	if len(remove.Writes) < 2 {
		t.Fatalf("expected claude and codex removals, got %+v", remove.Writes)
	}
}

func TestHasCodexBlock_DetectsPresence(t *testing.T) {
	home := t.TempDir()
	if HasCodexBlock(home) {
		t.Fatal("empty home reported as wired")
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	if !HasCodexBlock(home) {
		t.Fatal("wired block not detected")
	}
}

func TestClientStateString(t *testing.T) {
	cases := map[ClientState]string{
		ClientNotInstalled:   "not_installed",
		ClientInstalled:      "installed",
		ClientPartiallyWired: "partially_wired",
		ClientFullyWired:     "fully_wired",
		ClientState(99):      "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("state %d: got %q, want %q", int(s), got, want)
		}
	}
}

func TestDiffPreview_EmptyReport(t *testing.T) {
	out := DiffPreview(Report{})
	if !strings.Contains(out, "no changes") {
		t.Fatalf("empty preview wrong: %q", out)
	}
}

func TestDiffPreview_FormatsWrites(t *testing.T) {
	rep := Report{
		Writes: []WriteEvent{{Path: "/a", Action: "wrote_block"}},
		Errors: []string{"boom"},
	}
	out := DiffPreview(rep)
	if !strings.Contains(out, "wrote_block") || !strings.Contains(out, "boom") {
		t.Fatalf("preview missing entries: %q", out)
	}
}

func TestBackupOnce_MissingSourceReturnsNoError(t *testing.T) {
	name, err := backupOnce(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if name != "" {
		t.Fatalf("name = %q, want empty", name)
	}
}

func TestBackupOnce_WriteError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unix directory permissions")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "config")
	if err := os.WriteFile(src, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := backupOnce(src); err == nil {
		t.Fatal("expected backup write error")
	}
}

func TestOptionsResolveDefaultsAndOverrides(t *testing.T) {
	if got := (Options{}).resolveProxyURL(); got != ProxyURL {
		t.Fatalf("default proxy = %q", got)
	}
	if got := (Options{ProxyURL: "  http://127.0.0.1:7777  "}).resolveProxyURL(); got != "http://127.0.0.1:7777" {
		t.Fatalf("override proxy = %q", got)
	}
	home := t.TempDir()
	gotHome, err := (Options{HomeDir: home}).resolveHome()
	if err != nil {
		t.Fatal(err)
	}
	if gotHome != home {
		t.Fatalf("home = %q, want %q", gotHome, home)
	}
}

func TestInsertBeforeFirstTable_AddsMissingBlockNewline(t *testing.T) {
	out := insertBeforeFirstTable("[features]\ncodex_hooks = true\n", markerStart+"\nbody\n"+markerEnd)
	if !strings.Contains(out, markerEnd+"\n\n[features]") {
		t.Fatalf("missing separator after block: %q", out)
	}
}

func TestFenceIsTopLevelWithBody_InternalInconsistentNoMarker(t *testing.T) {
	if fenceIsTopLevelWithBody("body only", "body") {
		t.Fatal("content without marker cannot be top-level fence")
	}
}

func TestCountTopLevelKey_IgnoresComments(t *testing.T) {
	if got := countTopLevelKey("# openai_base_url = \"x\"\nopenai_base_url = \"y\"\n", "openai_base_url"); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
}

func TestDetectDaemon_UnreachableIsClean(t *testing.T) {
	s := DetectDaemon("http://127.0.0.1:1")
	if s.Running {
		t.Fatal("unreachable reported as running")
	}
	if s.Health == "" {
		t.Fatal("health string empty")
	}
}

func TestStatusDetectsWiredFakeClients(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex"} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("ANTHROPIC_BASE_URL", ProxyURL)
	if err := os.MkdirAll(filepath.Join(home, ".slimference", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(home, ".slimference", "hooks", "claude-prompt.sh"),
		filepath.Join(home, ".slimference", "hooks", "codex-post-tool.sh"),
	} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := WriteRCBlock(filepath.Join(home, ".zshrc"), ShellZsh, ProxyURL); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}

	rep := Status(Options{HomeDir: home, ProxyURL: "http://127.0.0.1:1"})
	if len(rep.Errors) != 0 {
		t.Fatalf("status errors: %v", rep.Errors)
	}
	if rep.Claude.State != ClientFullyWired {
		t.Fatalf("claude state = %s details=%v", rep.Claude.State, rep.Claude.Details)
	}
	if rep.Codex.State != ClientFullyWired {
		t.Fatalf("codex state = %s details=%v", rep.Codex.State, rep.Codex.Details)
	}
	if rep.Daemon.Running {
		t.Fatal("unreachable daemon reported running")
	}
}

package integrate

import (
	"os"
	"path/filepath"
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
		"/bin/zsh":                 ShellZsh,
		"/opt/homebrew/bin/fish":   ShellFish,
		"/usr/bin/bash":            ShellBash,
		"/bin/sh":                  ShellBash,
		"/weird/custom-shell":      ShellUnknown,
		"":                         ShellUnknown,
	}
	for in, want := range cases {
		if got := shellFromEnv(in); got != want {
			t.Errorf("%s: got %v, want %v", in, got, want)
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

func TestDetectDaemon_UnreachableIsClean(t *testing.T) {
	s := DetectDaemon("http://127.0.0.1:1")
	if s.Running {
		t.Fatal("unreachable reported as running")
	}
	if s.Health == "" {
		t.Fatal("health string empty")
	}
}

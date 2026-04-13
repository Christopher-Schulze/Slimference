package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledStatus_noneInstalled(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	claude, codex := InstalledStatus(home)
	if claude || codex {
		t.Fatalf("want both false when nothing installed, got claude=%v codex=%v", claude, codex)
	}
}

func TestInstalledStatus_claudeOnly(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	scriptDir := filepath.Join(home, ".claude", "hooks")
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "slimference-rewrite.sh"), []byte("#!/bin/sh"), 0644); err != nil {
		t.Fatal(err)
	}
	claude, codex := InstalledStatus(home)
	if !claude {
		t.Fatal("want claude=true after script created")
	}
	if codex {
		t.Fatal("want codex=false when AGENTS.md absent")
	}
}

func TestInstalledStatus_codexOnly(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	claude, codex := InstalledStatus(home)
	if claude {
		t.Fatal("want claude=false when script absent")
	}
	if !codex {
		t.Fatal("want codex=true after InstallCodex")
	}
}

func TestInstalledStatus_both(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Install claude script.
	scriptDir := filepath.Join(home, ".claude", "hooks")
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "slimference-rewrite.sh"), []byte("#!/bin/sh"), 0644); err != nil {
		t.Fatal(err)
	}
	// Install codex block.
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	claude, codex := InstalledStatus(home)
	if !claude || !codex {
		t.Fatalf("want both true, got claude=%v codex=%v", claude, codex)
	}
}

func TestVerifyReport_missingClaudeScript(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	lines, ok := VerifyReport(home)
	if ok {
		t.Fatal("want ok=false when claude hook missing")
	}
	var sawMissing bool
	for _, ln := range lines {
		if strings.Contains(ln, "MISSING") && strings.Contains(ln, "slimference-rewrite.sh") {
			sawMissing = true
		}
	}
	if !sawMissing {
		t.Fatalf("lines: %#v", lines)
	}
}

func TestVerifyReport_codexFileWithoutMarker(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	agents := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(agents), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agents, []byte("# hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyReport(home)
	if ok {
		t.Fatal("claude script missing => ok=false")
	}
	var sawNoBlock bool
	for _, ln := range lines {
		if strings.Contains(ln, "codex") && strings.Contains(ln, "no slimference block") {
			sawNoBlock = true
		}
	}
	if !sawNoBlock {
		t.Fatalf("lines: %#v", lines)
	}
}

func TestStripClaudePreToolUse(t *testing.T) {
	t.Parallel()
	t.Run("missing_file", func(t *testing.T) {
		t.Parallel()
		if err := stripClaudePreToolUse(filepath.Join(t.TempDir(), "nope.json")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("drops_pre_tool_use_only_hooks", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(p, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash"}]}}`), 0644); err != nil {
			t.Fatal(err)
		}
		if err := stripClaudePreToolUse(p); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "PreToolUse") {
			t.Fatalf("still has PreToolUse: %s", b)
		}
		if strings.Contains(string(b), `"hooks"`) {
			t.Fatalf("empty hooks object should remove hooks key: %s", b)
		}
	})
	t.Run("keeps_other_hook_keys", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "settings.json")
		raw := `{"hooks":{"PreToolUse":[{"matcher":"Bash"}],"Other":true}}`
		if err := os.WriteFile(p, []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
		if err := stripClaudePreToolUse(p); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if strings.Contains(s, "PreToolUse") || !strings.Contains(s, "Other") {
			t.Fatalf("bad rewrite: %s", s)
		}
	})
	t.Run("invalid_json", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(p, []byte(`{`), 0644); err != nil {
			t.Fatal(err)
		}
		if err := stripClaudePreToolUse(p); err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestInstallClaude_invalidExistingSettings(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`not json`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(home, ""); err == nil {
		t.Fatal("expected merge error on corrupt settings.json")
	}
}

func TestInstallCodex_idempotent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := InstallCodex(home, "/bin/slimference"); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, "/bin/slimference"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Count(string(b), codexMarkerBegin); c != 1 {
		t.Fatalf("marker count: %d", c)
	}
}

func TestRemoveCodex_unclosedMarker(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	p := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("before "+codexMarkerBegin+" trash\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCodex(home); err == nil {
		t.Fatal("expected unclosed marker error")
	}
}

func TestClaudeHookScript_customCommand(t *testing.T) {
	t.Parallel()
	s := ClaudeHookScript("/opt/bin/slimference")
	if !strings.Contains(s, "exec '/opt/bin/slimference' rewrite") {
		t.Fatalf("script:\n%s", s)
	}
	s2 := ClaudeHookScript("")
	if !strings.Contains(s2, "exec 'slimference' rewrite") && !strings.Contains(s2, "exec slimference rewrite") {
		t.Fatalf("default script:\n%s", s2)
	}
}

func TestCodexAgentsBlock_custom(t *testing.T) {
	t.Parallel()
	b := CodexAgentsBlock("/x/slimference")
	if !strings.Contains(b, "`/x/slimference filter`") {
		t.Fatal(b)
	}
}

func TestInstallRemoveClaude(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := InstallClaude(home, ""); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyReport(home)
	if !ok {
		t.Fatalf("verify should see claude: ok=%v %#v", ok, lines)
	}
	if err := RemoveClaude(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("script should be removed")
	}
}

func TestInstallRemoveCodex(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := InstallCodex(home, ""); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), codexMarkerBegin) {
		t.Fatal("marker missing")
	}
	if err := RemoveCodex(home); err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b2), codexMarkerBegin) {
		t.Fatal("marker should be removed")
	}
}

func TestInstallClaude_mkdirError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	home := t.TempDir()
	// Put a file where .claude/ dir should be created - MkdirAll will fail.
	claudePath := filepath.Join(home, ".claude")
	if err := os.WriteFile(claudePath, []byte("blocker"), 0444); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaude(home, ""); err == nil {
		t.Fatal("expected error when .claude is a file, not a dir")
	}
}

func TestInstallCodex_mkdirError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	home := t.TempDir()
	// Put a file where .codex/ should be created - MkdirAll will fail.
	codexPath := filepath.Join(home, ".codex")
	if err := os.WriteFile(codexPath, []byte("blocker"), 0444); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, ""); err == nil {
		t.Fatal("expected error when .codex is a file, not a dir")
	}
}

func TestInstallCodex_prevNoNewline(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	agentsDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(agentsDir, "AGENTS.md")
	// Write content without trailing newline.
	if err := os.WriteFile(agentsPath, []byte("# existing content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, codexMarkerBegin) {
		t.Fatal("marker should be present after install")
	}
	// A newline must have been inserted between the old content and the block.
	if !strings.Contains(content, "# existing content\n") {
		t.Errorf("expected newline inserted after content without trailing newline: %q", content)
	}
}

func TestRemoveCodex_noMarker(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	agentsDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(agentsDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# some content without marker\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// RemoveCodex with no marker must return nil.
	if err := RemoveCodex(home); err != nil {
		t.Fatalf("expected nil error when no marker, got: %v", err)
	}
}

func TestStripClaudePreToolUse_hooksNotMap(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "settings.json")
	// "hooks" is not a map (it's a bool) - root["hooks"].(map[string]interface{}) fails - return nil.
	if err := os.WriteFile(p, []byte(`{"hooks":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := stripClaudePreToolUse(p); err != nil {
		t.Fatalf("expected nil when hooks is not a map, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Additional coverage tests
// ---------------------------------------------------------------------------

// TestInstallClaude_writeFileError triggers the WriteFile error on line 42-44
// by making the hooks directory read-only after creation.
func TestInstallClaude_writeFileError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	home := t.TempDir()
	hookDir := filepath.Join(home, ".claude", "hooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Make the hooks dir read-only so WriteFile fails.
	if err := os.Chmod(hookDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hookDir, 0755) })
	if err := InstallClaude(home, "slimference"); err == nil {
		t.Fatal("expected WriteFile error when hooks dir is read-only")
	}
}

// TestMergeClaudeSettings_readFileError triggers the non-ENOENT ReadFile error
// on lines 60-62 by passing a directory path as the settings file path.
func TestMergeClaudeSettings_readFileError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	// Use a directory as settingsPath - reading a dir is a non-ENOENT error.
	dirPath := t.TempDir()
	if err := mergeClaudeSettings(dirPath, "/some/script.sh"); err == nil {
		t.Fatal("expected ReadFile error when settingsPath is a directory")
	}
}

// TestStripClaudePreToolUse_readFileError triggers the non-ENOENT ReadFile error
// on line 103 by passing a directory path as the settings file path.
func TestStripClaudePreToolUse_readFileError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	// Use a directory as settingsPath - reading a dir is a non-ENOENT error.
	dirPath := t.TempDir()
	if err := stripClaudePreToolUse(dirPath); err == nil {
		t.Fatal("expected ReadFile error when settingsPath is a directory")
	}
}

// TestInstallCodex_openFileError triggers the OpenFile error on lines 45-47
// by creating AGENTS.md as a directory so OpenFile(O_WRONLY) fails.
func TestInstallCodex_openFileError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create AGENTS.md as a directory - ReadFile on dir returns error (swallowed),
	// prev stays empty, no marker found, then OpenFile on a dir with O_WRONLY fails.
	agentsDir := filepath.Join(codexDir, "AGENTS.md")
	if err := os.Mkdir(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, "slimference"); err == nil {
		t.Fatal("expected OpenFile error when AGENTS.md is a directory")
	}
}

// TestRemoveCodex_missingFile tests lines 63-66 (ENOENT path): file doesn't exist,
// RemoveCodex must return nil.
func TestRemoveCodex_missingFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// .codex/AGENTS.md does not exist at all.
	if err := RemoveCodex(home); err != nil {
		t.Fatalf("expected nil when AGENTS.md missing, got: %v", err)
	}
}

// TestRemoveCodex_readFileError triggers the non-ENOENT error on line 67
// by making AGENTS.md a directory so ReadFile returns a non-ENOENT error.
func TestRemoveCodex_readFileError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create AGENTS.md as a directory - ReadFile on a dir gives non-ENOENT error.
	agentsDir := filepath.Join(codexDir, "AGENTS.md")
	if err := os.Mkdir(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCodex(home); err == nil {
		t.Fatal("expected non-ENOENT ReadFile error when AGENTS.md is a directory")
	}
}

// TestMergeClaudeSettings_mkdirError covers the os.MkdirAll error path (claude.go:91-93)
// by creating a read-only parent directory so MkdirAll cannot create the subdirectory.
func TestMergeClaudeSettings_mkdirError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	tmp := t.TempDir()
	// Make a read-only directory so MkdirAll cannot create a subdir inside it.
	blocked := filepath.Join(tmp, "blocked")
	if err := os.MkdirAll(blocked, 0555); err != nil {
		t.Fatal(err)
	}
	// settingsPath points inside the blocked dir; its parent can't be created.
	settingsPath := filepath.Join(blocked, "sub", "settings.json")
	// ReadFile returns IsNotExist (path component "sub" doesn't exist) -> proceeds.
	// MkdirAll("blocked/sub", 0755) -> fails (blocked is 0555 = no write).
	err := mergeClaudeSettings(settingsPath, "script.sh")
	if err == nil {
		t.Fatal("expected error when MkdirAll fails due to read-only parent")
	}
}

func TestVerifyReport_codexWithMarker(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Install both claude and codex so verify sees the codex marker present line.
	if err := InstallClaude(home, ""); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, ""); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyReport(home)
	if !ok {
		t.Fatalf("expected ok=true, lines: %#v", lines)
	}
	var sawBlockPresent bool
	for _, ln := range lines {
		if strings.Contains(ln, "codex") && strings.Contains(ln, "instruction block present") {
			sawBlockPresent = true
		}
	}
	if !sawBlockPresent {
		t.Fatalf("expected 'instruction block present' in lines: %#v", lines)
	}
}

// TestMergeClaudeSettings_readError covers the non-IsNotExist ReadFile error branch
// in mergeClaudeSettings (err != nil && !os.IsNotExist(err) -> return err).
func TestMergeClaudeSettings_readError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(`{}`), 0000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(p, 0644) }()
	if err := mergeClaudeSettings(p, "/bin/tp"); err == nil {
		t.Fatal("expected error reading permission-denied file")
	}
}

// TestStripClaudePreToolUse_readError covers the non-IsNotExist ReadFile error branch
// in stripClaudePreToolUse (err != nil, !os.IsNotExist(err) -> return err).
func TestStripClaudePreToolUse_readError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(`{}`), 0000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(p, 0644) }()
	if err := stripClaudePreToolUse(p); err == nil {
		t.Fatal("expected error reading permission-denied file")
	}
}

// TestInstallCodex_openFilePermissionError covers the os.OpenFile error branch in InstallCodex
// when the file exists with 000 permissions (MkdirAll succeeds, ReadFile swallows error,
// no marker found, then OpenFile with O_WRONLY fails on a 000-permission file).
func TestInstallCodex_openFilePermissionError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	home := t.TempDir()
	agentsDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(agentsDir, "AGENTS.md")
	// Create file without codex marker but with no read/write permission.
	if err := os.WriteFile(agentsPath, []byte("other content"), 0000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(agentsPath, 0644) }()
	if err := InstallCodex(home, "/bin/tp"); err == nil {
		t.Fatal("expected error opening permission-denied file")
	}
}

package hooks

import (
	"encoding/json"
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
	if err := InstallClaude(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	claude, codex := InstalledStatus(home)
	if !claude {
		t.Fatal("want claude=true after InstallClaude")
	}
	if codex {
		t.Fatal("want codex=false when AGENTS.md absent")
	}
}

func TestInstalledStatusClaudeMissingPostToolScript(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := InstallClaude(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, ".claude", "hooks", "slimference-posttool.sh")); err != nil {
		t.Fatal(err)
	}
	claude, _ := InstalledStatus(home)
	if claude {
		t.Fatal("claude install must be incomplete without posttool script")
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
	if err := InstallClaude(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	claude, codex := InstalledStatus(home)
	if !claude || !codex {
		t.Fatalf("want both true, got claude=%v codex=%v", claude, codex)
	}
}

func TestVerifyReport_missingClaudeScriptDoesNotFail(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyReport(home)
	if !ok {
		t.Fatal("want ok=true when only Claude hooks are missing in Codex-only mode")
	}
	var sawParked bool
	for _, ln := range lines {
		if strings.Contains(ln, "claude") && strings.Contains(ln, "parked") {
			sawParked = true
		}
	}
	if !sawParked {
		t.Fatalf("lines: %#v", lines)
	}
}

func TestVerifyReport_codexFileWithoutMarker(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Create a hooks.json without slimference entry.
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(codexDir, "hooks.json")
	if err := os.WriteFile(hooksPath, []byte(`{"hooks":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyReport(home)
	if ok {
		t.Fatal("codex hooks.json without slimference entries => ok=false")
	}
	var sawNoHook bool
	for _, ln := range lines {
		if strings.Contains(ln, "codex") && (strings.Contains(ln, "no slimference hook") || strings.Contains(ln, "not installed")) {
			sawNoHook = true
		}
	}
	if !sawNoHook {
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
		raw := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/slimference-rewrite.sh"}]}]}}`
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
		raw := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/slimference-rewrite.sh"}]}],"Other":true}}`
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
	if _, err := os.Stat(filepath.Join(home, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("InstallCodex must not create AGENTS.md, stat err=%v", err)
	}
}

func TestRemoveCodex_ignoresAgentsMD(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	p := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("before "+codexMarkerBegin+" trash\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCodex(home); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "before "+codexMarkerBegin+" trash\n" {
		t.Fatalf("RemoveCodex must not modify AGENTS.md, got %q", string(data))
	}
}

func TestClaudeHookScript_customCommand(t *testing.T) {
	t.Parallel()
	s := ClaudeHookScript("/opt/bin/slimference")
	if !strings.Contains(s, "/opt/bin/slimference") || !strings.Contains(s, "rewrite -- \"$CMD\"") {
		t.Fatalf("script:\n%s", s)
	}
	if !strings.Contains(s, "updatedInput") {
		t.Fatalf("script:\n%s", s)
	}
	s2 := ClaudeHookScript("")
	if !strings.Contains(s2, "slimference") || !strings.Contains(s2, "rewrite -- \"$CMD\"") {
		t.Fatalf("default script:\n%s", s2)
	}
}

func TestClaudePostToolHookScript_DefaultOffMaxOptIn(t *testing.T) {
	t.Parallel()
	s := ClaudePostToolHookScript("/opt/bin/slimference")
	modeGate := strings.Index(s, `SLIMFERENCE_CLAUDE_HOOK_MODE:-off`)
	postToolCall := strings.Index(s, `claudeposttool`)
	inputRead := strings.Index(s, `INPUT=$(cat)`)
	if modeGate == -1 || postToolCall == -1 || inputRead == -1 || modeGate > inputRead || modeGate > postToolCall {
		t.Fatalf("expected off-by-default mode gate before claudeposttool call:\n%s", s)
	}
	for _, want := range []string{"max", "compact", "aggressive", "auto"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q mode in script:\n%s", want, s)
		}
	}
}

func TestClaudePostToolHookScript_DefaultCommand(t *testing.T) {
	t.Parallel()
	s := ClaudePostToolHookScript("")
	if !strings.Contains(s, "slimference") || !strings.Contains(s, "claudeposttool") {
		t.Fatalf("default posttool script:\n%s", s)
	}
}

func TestCodexPreToolHookScript_DefaultSilentAggressiveOptIn(t *testing.T) {
	t.Parallel()
	s := codexPreToolHookScript("/opt/bin/slimference")
	modeGate := strings.Index(s, `if [[ "$MODE" != "aggressive" ]]`)
	rewriteCall := strings.Index(s, `rewrite -- "$CMD"`)
	inputRead := strings.Index(s, `INPUT=$(cat)`)
	if modeGate == -1 || rewriteCall == -1 || inputRead == -1 || modeGate > inputRead || modeGate > rewriteCall {
		t.Fatalf("expected aggressive mode gate before rewrite call:\n%s", s)
	}
	if !strings.Contains(s, `MODE="${SLIMFERENCE_CODEX_HOOK_MODE:-silent}"`) {
		t.Fatalf("expected silent default mode:\n%s", s)
	}
	if !strings.Contains(s, "Rerun compacted command:") {
		t.Fatalf("expected legacy aggressive retry path to remain opt-in:\n%s", s)
	}
}

func TestCodexPostToolHookScript_DefaultAutoSilentOptOut(t *testing.T) {
	t.Parallel()
	s := codexPostToolHookScript("/opt/bin/slimference")
	modeGate := strings.Index(s, `compact|aggressive|auto`)
	postToolCall := strings.Index(s, `posttool >"$TMP_OUT"`)
	inputRead := strings.Index(s, `INPUT=$(cat)`)
	if modeGate == -1 || postToolCall == -1 || inputRead == -1 || modeGate > inputRead || modeGate > postToolCall {
		t.Fatalf("expected mode gate before posttool call:\n%s", s)
	}
	if !strings.Contains(s, `MODE="${SLIMFERENCE_CODEX_HOOK_MODE:-auto}"`) {
		t.Fatalf("expected auto default mode:\n%s", s)
	}
}

func TestCodexReadAndLifecycleHookScripts_DefaultAutoSilentOptOut(t *testing.T) {
	t.Parallel()
	readScript := codexReadToolHookScript("/opt/bin/slimference")
	readGate := strings.Index(readScript, `compact|aggressive|auto|debug`)
	readCall := strings.Index(readScript, `readhook codex`)
	readInput := strings.Index(readScript, `INPUT=$(cat)`)
	if readGate == -1 || readCall == -1 || readInput == -1 || readGate > readInput || readGate > readCall {
		t.Fatalf("expected mode gate before readhook call:\n%s", readScript)
	}
	if !strings.Contains(readScript, `MODE="${SLIMFERENCE_CODEX_HOOK_MODE:-auto}"`) {
		t.Fatalf("expected auto default mode:\n%s", readScript)
	}
	lifecycleScript := codexLifecycleHookScript("/opt/bin/slimference", "session-start")
	lifecycleGate := strings.Index(lifecycleScript, `compact|aggressive|auto|debug`)
	lifecycleCall := strings.Index(lifecycleScript, `codexhook session-start`)
	lifecycleInput := strings.Index(lifecycleScript, `INPUT=$(cat)`)
	if lifecycleGate == -1 || lifecycleCall == -1 || lifecycleInput == -1 || lifecycleGate > lifecycleInput || lifecycleGate > lifecycleCall {
		t.Fatalf("expected mode gate before lifecycle call:\n%s", lifecycleScript)
	}
	if !strings.Contains(lifecycleScript, `MODE="${SLIMFERENCE_CODEX_HOOK_MODE:-auto}"`) {
		t.Fatalf("expected auto default mode:\n%s", lifecycleScript)
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
	readPath := filepath.Join(home, ".claude", "hooks", "slimference-read-cache.sh")
	if _, err := os.Stat(readPath); err != nil {
		t.Fatal(err)
	}
	postPath := filepath.Join(home, ".claude", "hooks", "slimference-posttool.sh")
	if _, err := os.Stat(postPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	claude, _ := InstalledStatus(home)
	if !claude {
		t.Fatal("InstalledStatus should see Claude hook while legacy code remains available")
	}
	lines, _ := VerifyReport(home)
	var sawParked bool
	for _, ln := range lines {
		if strings.Contains(ln, "claude") && strings.Contains(ln, "parked") {
			sawParked = true
		}
	}
	if !sawParked {
		t.Fatalf("VerifyReport should mark Claude parked: %#v", lines)
	}
	if err := RemoveClaude(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("script should be removed")
	}
	if _, err := os.Stat(readPath); !os.IsNotExist(err) {
		t.Fatal("read script should be removed")
	}
	if _, err := os.Stat(postPath); !os.IsNotExist(err) {
		t.Fatal("posttool script should be removed")
	}
}

func TestInstallRemoveCodex(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := InstallCodex(home, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("InstallCodex must not create AGENTS.md, stat err=%v", err)
	}
	if !CodexHookInstalled(home) {
		t.Fatal("codex hooks should be installed")
	}
	if err := RemoveCodex(home); err != nil {
		t.Fatal(err)
	}
	if CodexHookInstalled(home) {
		t.Fatal("codex hooks should be removed")
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

func TestInstallClaude_readAndPostToolWriteErrors(t *testing.T) {
	t.Parallel()
	t.Run("read_script_path_is_directory", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		hookDir := filepath.Join(home, ".claude", "hooks")
		if err := os.MkdirAll(filepath.Join(hookDir, "slimference-read-cache.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := InstallClaude(home, "slimference"); err == nil {
			t.Fatal("expected write error when read hook path is a directory")
		}
	})
	t.Run("posttool_script_path_is_directory", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		hookDir := filepath.Join(home, ".claude", "hooks")
		if err := os.MkdirAll(filepath.Join(hookDir, "slimference-posttool.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := InstallClaude(home, "slimference"); err == nil {
			t.Fatal("expected write error when posttool hook path is a directory")
		}
	})
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

func TestInstallCodex_preservesExistingAgentsMD(t *testing.T) {
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
	if content != "# existing content" {
		t.Fatalf("InstallCodex must preserve AGENTS.md byte-for-byte, got %q", content)
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
	if err := mergeClaudeSettings(dirPath, "/some/script.sh", "/some/read.sh", "/some/post.sh"); err == nil {
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
	// Create hooks.json as a directory - writing to it should fail.
	hooksDir := filepath.Join(codexDir, "hooks.json")
	if err := os.Mkdir(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, "slimference"); err == nil {
		t.Fatal("expected error when hooks.json is a directory")
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
func TestRemoveCodex_ignoresAgentsDirectory(t *testing.T) {
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
	if err := RemoveCodex(home); err != nil {
		t.Fatalf("RemoveCodex must ignore AGENTS.md directory, got %v", err)
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
	err := mergeClaudeSettings(settingsPath, "script.sh", "read.sh", "post.sh")
	if err == nil {
		t.Fatal("expected error when MkdirAll fails due to read-only parent")
	}
}

func TestVerifyReport_codexWithMarker(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Install both claude and codex so verify sees both hooks present.
	if err := InstallClaude(home, ""); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, ""); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.WriteFile(configPath, []byte("openai_base_url = \"http://127.0.0.1:8990\"\nchatgpt_base_url = \"http://127.0.0.1:8990\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyReport(home)
	if !ok {
		t.Fatalf("expected ok=true, lines: %#v", lines)
	}
	var sawCodexHook bool
	for _, ln := range lines {
		if strings.Contains(ln, "codex") && (strings.Contains(ln, "sha256=") || strings.Contains(ln, "instruction block")) {
			sawCodexHook = true
		}
	}
	if !sawCodexHook {
		t.Fatalf("expected codex hook in lines: %#v", lines)
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
	if err := mergeClaudeSettings(p, "/bin/tp", "/bin/read", "/bin/post"); err == nil {
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
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create hooks.json without write permission to trigger a write error.
	hooksPath := filepath.Join(codexDir, "hooks.json")
	if err := os.WriteFile(hooksPath, []byte("{}"), 0000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(hooksPath, 0644) }()
	if err := InstallCodex(home, "/bin/tp"); err == nil {
		t.Fatal("expected error opening permission-denied file")
	}
}

// --- New Codex hooks.json integration tests ---

func TestInstallCodexHooksJSON_createsNew(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	scriptPath := filepath.Join(home, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installCodexHooksJSON(home, scriptPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Slimference") && !strings.Contains(string(data), "slimference") {
		t.Fatalf("hooks.json should contain slimference/Slimference: %s", data)
	}
	if !strings.Contains(string(data), scriptPath) {
		t.Fatalf("hooks.json should reference script path: %s", data)
	}
}

func TestInstallCodexHooksJSON_idempotent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	scriptPath := filepath.Join(home, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installCodexHooksJSON(home, scriptPath); err != nil {
		t.Fatal(err)
	}
	// Second install should not duplicate entries.
	if err := installCodexHooksJSON(home, scriptPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	count := strings.Count(string(data), "Output compactor")
	if count != 1 {
		t.Fatalf("expected 1 output compactor entry, got %d", count)
	}
}

func TestInstallCodexHooksJSON_invalidExistingJSONFailsWithoutOverwrite(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(codexDir, "hooks.json")
	if err := os.WriteFile(hooksPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(home, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := installCodexHooksJSON(home, scriptPath)
	if err == nil || !strings.Contains(err.Error(), "parse hooks.json") {
		t.Fatalf("expected parse hooks.json error, got %v", err)
	}
	data, readErr := os.ReadFile(hooksPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "{" {
		t.Fatalf("invalid hooks.json must be preserved, got %q", string(data))
	}
}

func TestInstallCodexHooksJSONWithScripts_MkdirError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".codex"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := installCodexHooksJSONWithScripts(home, "/tmp/pre.sh", "/tmp/post.sh", "/tmp/read.sh")
	if err == nil {
		t.Fatal("expected mkdir error for .codex blocker file")
	}
}

func TestRemoveCodexHooksJSON_removesEntry(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	scriptPath := filepath.Join(home, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installCodexHooksJSON(home, scriptPath); err != nil {
		t.Fatal(err)
	}
	if err := removeCodexHooksJSON(home); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "slimference") {
		t.Fatalf("hooks.json should not contain slimference after removal: %s", data)
	}
}

func TestInstallAndRemoveCodexPreservesReconcHooks(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reconcHooks := `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear",
        "hooks": [
          {
            "type": "command",
            "command": "sh -lc 'repo=\".\"; hook=\"$repo/tools/reconc/bin/hook\"; if [ -x \"$hook\" ]; then exec \"$hook\" codex-session-start \"$repo\"; fi; RECONC_HOOK_REPO_RESOLVED=1 exec \"$repo/tools/reconc/bin/hook\" codex-session-start \"$repo\"'",
            "statusMessage": "reconc: initializing policy session"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit|Bash|apply_patch",
        "hooks": [
          {
            "type": "command",
            "command": "sh -lc 'repo=\".\"; hook=\"$repo/tools/reconc/bin/hook\"; if [ -x \"$hook\" ]; then exec \"$hook\" codex-pre-tool-use \"$repo\"; fi; RECONC_HOOK_REPO_RESOLVED=1 exec \"$repo/tools/reconc/bin/hook\" codex-pre-tool-use \"$repo\"'"
          }
        ]
      }
    ],
    "PostToolUseFailure": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "sh -lc 'repo=\".\"; hook=\"$repo/tools/reconc/bin/hook\"; if [ -x \"$hook\" ]; then exec \"$hook\" codex-post-tool-use-failure \"$repo\"; fi; RECONC_HOOK_REPO_RESOLVED=1 exec \"$repo/tools/reconc/bin/hook\" codex-post-tool-use-failure \"$repo\"'"
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "sh -lc 'repo=\".\"; hook=\"$repo/tools/reconc/bin/hook\"; if [ -x \"$hook\" ]; then exec \"$hook\" codex-session-end \"$repo\"; fi; RECONC_HOOK_REPO_RESOLVED=1 exec \"$repo/tools/reconc/bin/hook\" codex-session-end \"$repo\"'"
          }
        ]
      }
    ]
  }
}`
	hooksPath := filepath.Join(codexDir, "hooks.json")
	if err := os.WriteFile(hooksPath, []byte(reconcHooks), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, "/bin/slimference"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	afterInstall := string(data)
	for _, want := range []string{
		"tools/reconc/bin/hook",
		"codex-session-start",
		"codex-pre-tool-use",
		"codex-post-tool-use-failure",
		"codex-session-end",
		"codex-post-tool.sh",
		"codex-read-tool.sh",
	} {
		if !strings.Contains(afterInstall, want) {
			t.Fatalf("merged hooks.json lost %q:\n%s", want, afterInstall)
		}
	}
	if err := RemoveCodex(home); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	afterRemove := string(data)
	for _, want := range []string{
		"tools/reconc/bin/hook",
		"codex-session-start",
		"codex-pre-tool-use",
		"codex-post-tool-use-failure",
		"codex-session-end",
	} {
		if !strings.Contains(afterRemove, want) {
			t.Fatalf("remove hooks.json lost Reconc hook %q:\n%s", want, afterRemove)
		}
	}
	for _, forbidden := range []string{
		"codex-pre-tool.sh",
		"codex-post-tool.sh",
		"codex-read-tool.sh",
		"codex-session-start.sh",
		"Output compactor",
	} {
		if strings.Contains(afterRemove, forbidden) {
			t.Fatalf("remove hooks.json kept Slimference hook %q:\n%s", forbidden, afterRemove)
		}
	}
}

func TestRemoveCodexHooksJSON_noFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := removeCodexHooksJSON(home); err != nil {
		t.Fatalf("should not error on missing file: %v", err)
	}
}

func TestPatchCodexConfig_addsKeys(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := patchCodexConfig(home); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "openai_base_url") {
		t.Fatal("config should contain openai_base_url")
	}
	if !strings.Contains(content, "hooks = true") {
		t.Fatal("config should contain hooks = true")
	}
}

func TestPatchCodexConfig_conflictingBaseURLReturnsError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	existing := "model = \"gpt-5\"\nopenai_base_url = \"http://custom:9999\"\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	err := patchCodexConfig(home)
	if err == nil || !strings.Contains(err.Error(), "conflicting openai_base_url") {
		t.Fatalf("expected conflicting openai_base_url error, got %v", err)
	}
}

func TestPatchCodexConfig_conflictingDisabledHooksReturnsError(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	existing := "openai_base_url = \"http://127.0.0.1:8990\"\ncodex_hooks = false\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	err := patchCodexConfig(home)
	if err == nil || !strings.Contains(err.Error(), "conflicting codex_hooks=false") {
		t.Fatalf("expected conflicting codex_hooks=false error, got %v", err)
	}
}

func TestInstallCodex_preflightConflictDoesNotWriteScripts(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("openai_base_url = \"http://example.com\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := InstallCodex(home, "slimference")
	if err != nil {
		t.Fatalf("hook install should not validate config.toml anymore, got %v", err)
	}
	if _, statErr := os.Stat(CodexPreHookScriptPath(home)); statErr != nil {
		t.Fatalf("pre-hook script should be created independently of config.toml, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(CodexHookScriptPath(home)); statErr != nil {
		t.Fatalf("post-hook script should be created independently of config.toml, stat err=%v", statErr)
	}
}

func TestInstallCodex_preflightInvalidHooksJSONDoesNotWriteScripts(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(codexDir, "hooks.json")
	if err := os.WriteFile(hooksPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := InstallCodex(home, "slimference")
	if err == nil || !strings.Contains(err.Error(), "parse hooks.json") {
		t.Fatalf("expected parse hooks.json error, got %v", err)
	}
	if _, statErr := os.Stat(CodexPreHookScriptPath(home)); !os.IsNotExist(statErr) {
		t.Fatalf("pre-hook script should not be created on invalid hooks.json, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(CodexHookScriptPath(home)); !os.IsNotExist(statErr) {
		t.Fatalf("post-hook script should not be created on invalid hooks.json, stat err=%v", statErr)
	}
	data, readErr := os.ReadFile(hooksPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "{" {
		t.Fatalf("invalid hooks.json must be preserved, got %q", string(data))
	}
}

func TestInstallCodex_agentsMDIsIgnored(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	agentsPath := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(agentsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatalf("modern Codex install should succeed while ignoring AGENTS.md: %v", err)
	}
	if !CodexHookInstalled(home) {
		t.Fatal("modern hooks.json install should still succeed")
	}
}

func TestPatchCodexConfig_existingSlimferenceConfigRemainsValid(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	existing := "openai_base_url = \"http://localhost:8990/v1\"\n[features]\ncodex_hooks = true\n"
	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := patchCodexConfig(home); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Count(content, "openai_base_url") != 1 {
		t.Fatalf("openai_base_url should not be duplicated: %s", content)
	}
	if strings.Count(content, "codex_hooks") != 1 {
		t.Fatalf("codex_hooks should not be duplicated: %s", content)
	}
}

func TestReadExistingCodexHooksJSON_emptyFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readExistingCodexHooksJSON(hooksPath)
	if err == nil || !strings.Contains(err.Error(), "empty file") {
		t.Fatalf("expected empty file parse error, got %v", err)
	}
}

func TestReadExistingCodexHooksJSON_readError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(hooksPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := readExistingCodexHooksJSON(hooksPath)
	if err == nil {
		t.Fatal("expected read error when hooks.json path is a directory")
	}
}

func TestUnpatchCodexConfig_removesAdditions(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "model = \"gpt-5\"\n# Slimference proxy endpoint\nopenai_base_url = \"http://127.0.0.1:8990\"\ncodex_hooks = true  # Slimference: enable lifecycle hooks\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := unpatchCodexConfig(home); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cleaned := string(data)
	if strings.Contains(cleaned, "openai_base_url") {
		t.Fatalf("should remove openai_base_url: %s", cleaned)
	}
	if strings.Contains(cleaned, "codex_hooks") {
		t.Fatalf("should remove codex_hooks: %s", cleaned)
	}
	if !strings.Contains(cleaned, "model = \"gpt-5\"") {
		t.Fatal("should preserve existing settings")
	}
}

func TestCodexHookInstalled(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if CodexHookInstalled(home) {
		t.Fatal("should not be installed in empty home")
	}
	scriptPath := filepath.Join(home, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installCodexHooksJSON(home, scriptPath); err != nil {
		t.Fatal(err)
	}
	if !CodexHookInstalled(home) {
		t.Fatal("should be installed after installCodexHooksJSON")
	}
}

func TestInstallCodexToolHooksAreBashOnly(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := installCodexHooksJSONWithScripts(home, "/tmp/codex-pre-tool.sh", "/tmp/codex-post-tool.sh", "/tmp/codex-read-tool.sh"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]map[string][]map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	post := root["hooks"]["PostToolUse"]
	if len(post) != 1 {
		t.Fatalf("posttool entries=%d data=%s", len(post), data)
	}
	if got, _ := post[0]["matcher"].(string); got != "Bash" {
		t.Fatalf("PostToolUse matcher=%q data=%s", got, data)
	}
	pre := root["hooks"]["PreToolUse"]
	if len(pre) != 2 {
		t.Fatalf("pretool entries=%d data=%s", len(pre), data)
	}
	if got, _ := pre[0]["matcher"].(string); got != "Bash" {
		t.Fatalf("PreToolUse matcher=%q data=%s", got, data)
	}
	permission := root["hooks"]["PermissionRequest"]
	if len(permission) != 1 {
		t.Fatalf("permission entries=%d data=%s", len(permission), data)
	}
	if got, _ := permission[0]["matcher"].(string); got != "Bash" {
		t.Fatalf("PermissionRequest matcher=%q data=%s", got, data)
	}
}

func TestCodexPostToolHookScriptHasFailOpenTimeout(t *testing.T) {
	t.Parallel()
	script := codexPostToolHookScript("slimference")
	for _, want := range []string{
		"SLIMFERENCE_CODEX_POSTTOOL_TIMEOUT_SECONDS",
		"posttool-timeout",
		"exit 0",
		"2>/dev/null",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("posttool script missing %q:\n%s", want, script)
		}
	}
}

func TestCodexHookScriptPath(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	p := CodexHookScriptPath(home)
	if !strings.HasSuffix(p, filepath.Join(".slimference", "hooks", "codex-post-tool.sh")) {
		t.Fatalf("unexpected path: %s", p)
	}
}

func TestInstallCodex_hooksDirMkdirError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Make .slimference/hooks a file so MkdirAll fails
	hooksDir := filepath.Join(home, ".slimference", "hooks")
	if err := os.MkdirAll(filepath.Dir(hooksDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksDir, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	err := InstallCodex(home, "slimference")
	if err == nil {
		t.Fatal("expected error when hooks dir is a file")
	}
}

func TestRemoveCodex_fullCleanupWithoutAgentsMD(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Install first
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("AGENTS.md must not exist after install, stat err=%v", err)
	}
	// Remove
	if err := RemoveCodex(home); err != nil {
		t.Fatal(err)
	}
	// Verify hooks.json has no slimference
	hooksData, _ := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if strings.Contains(string(hooksData), "slimference") {
		t.Fatal("hooks.json should not contain slimference after removal")
	}
	// Verify config.toml has no slimference additions
	configData, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(string(configData), "openai_base_url") {
		t.Fatal("config.toml should not have openai_base_url after removal")
	}
	// Verify script removed
	scriptPath := CodexHookScriptPath(home)
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Fatal("hook script should be removed")
	}
}

func TestRemoveCodex_noFilesIsFine(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Remove on clean home should not error
	if err := RemoveCodex(home); err != nil {
		t.Fatalf("remove on empty home should succeed: %v", err)
	}
}

func TestUnpatchCodexConfig_preservesOtherContent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `model = "gpt-5"
# Slimference proxy endpoint
openai_base_url = "http://127.0.0.1:8990"

[features]
codex_hooks = true  # Slimference: enable lifecycle hooks
`
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := unpatchCodexConfig(home); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	result := string(data)
	if strings.Contains(result, "openai_base_url") {
		t.Fatalf("openai_base_url should be removed: %s", result)
	}
	if strings.Contains(result, "codex_hooks") {
		t.Fatalf("codex_hooks should be removed: %s", result)
	}
	if !strings.Contains(result, `model = "gpt-5"`) {
		t.Fatalf("model should be preserved: %s", result)
	}
}

func TestUnpatchCodexConfig_preservesUserManagedFeatures(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `model = "gpt-5"
# Slimference proxy endpoint
openai_base_url = "http://127.0.0.1:8990"

[features]
codex_hooks = true
other = true
`
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := unpatchCodexConfig(home); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	result := string(data)
	if strings.Contains(result, "openai_base_url") {
		t.Fatalf("openai_base_url should be removed: %s", result)
	}
	if !strings.Contains(result, "[features]") {
		t.Fatalf("[features] should be preserved when user-managed entries remain: %s", result)
	}
	if !strings.Contains(result, "codex_hooks = true") {
		t.Fatalf("user-managed codex_hooks should be preserved: %s", result)
	}
	if !strings.Contains(result, "other = true") {
		t.Fatalf("other feature should be preserved: %s", result)
	}
}

func TestUnpatchCodexConfig_removesLegacySingleHookFeaturesSection(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `model = "gpt-5"
[features]
codex_hooks = true
`
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := unpatchCodexConfig(home); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	result := string(data)
	if strings.Contains(result, "[features]") {
		t.Fatalf("empty legacy features section should be removed: %s", result)
	}
	if strings.Contains(result, "codex_hooks = true") {
		t.Fatalf("legacy codex_hooks line should be removed when it is the only feature: %s", result)
	}
	if !strings.Contains(result, `model = "gpt-5"`) {
		t.Fatalf("model should be preserved: %s", result)
	}
}

func TestRemoveCodexHooksJSON_invalidJSON(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte("not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	// Invalid JSON should return nil (graceful)
	if err := removeCodexHooksJSON(home); err != nil {
		t.Fatalf("invalid JSON should be handled gracefully: %v", err)
	}
}

func TestRemoveCodexHooksJSON_preservesNonSlimferenceNestedHooks(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `{
  "other": "keep",
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "/tmp/codex-pre-tool.sh"
          }
        ]
      },
      {
        "matcher": "Read",
        "hooks": [
          {
            "type": "command",
            "command": "echo keep"
          }
        ]
      }
    ]
  }
}`
	hooksPath := filepath.Join(codexDir, "hooks.json")
	if err := os.WriteFile(hooksPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := removeCodexHooksJSON(home); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	result := string(data)
	if strings.Contains(result, "codex-pre-tool.sh") {
		t.Fatalf("Slimference hook was not removed: %s", result)
	}
	if !strings.Contains(result, "echo keep") || !strings.Contains(result, `"other": "keep"`) {
		t.Fatalf("non-Slimference data was not preserved: %s", result)
	}
}

func TestCollectFeaturesSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lines    []string
		wantNext int
		wantSkip bool
	}{
		{
			name:     "legacy_only_until_next_section",
			lines:    []string{"[features]", "codex_hooks = true", "[other]"},
			wantNext: 2,
			wantSkip: true,
		},
		{
			name:     "user_entries_keep_section",
			lines:    []string{"[features]", "other = true", "[other]"},
			wantNext: 2,
			wantSkip: false,
		},
		{
			name:     "comments_and_blank_are_ignored",
			lines:    []string{"[features]", "# note", "", "codex_hooks = true"},
			wantNext: 4,
			wantSkip: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotNext, gotSkip := collectFeaturesSection(tc.lines, 0)
			if gotNext != tc.wantNext || gotSkip != tc.wantSkip {
				t.Fatalf("collectFeaturesSection(%q) = (%d, %v), want (%d, %v)", tc.lines, gotNext, gotSkip, tc.wantNext, tc.wantSkip)
			}
		})
	}
}

func TestParseCodexConfigState(t *testing.T) {
	t.Parallel()

	state := parseCodexConfigState("openai_base_url = \"http://127.0.0.1:8990\" # keep\nchatgpt_base_url = \"http://127.0.0.1:8990\"\ncodex_hooks = true\n")
	if !state.HasOpenAIBaseURL || state.OpenAIBaseURL != "http://127.0.0.1:8990" {
		t.Fatalf("unexpected base url state: %#v", state)
	}
	if !state.HasChatGPTBaseURL || state.ChatGPTBaseURL != "http://127.0.0.1:8990" {
		t.Fatalf("unexpected chatgpt base url state: %#v", state)
	}
	if state.CodexHooks == nil || !*state.CodexHooks {
		t.Fatalf("expected codex_hooks=true, got %#v", state)
	}

	state = parseCodexConfigState("openai_base_url = broken\nchatgpt_base_url = broken\ncodex_hooks = false\n")
	if !state.HasOpenAIBaseURL || state.OpenAIBaseURL != "" {
		t.Fatalf("broken quoted base url should still mark presence without parsed value: %#v", state)
	}
	if !state.HasChatGPTBaseURL || state.ChatGPTBaseURL != "" {
		t.Fatalf("broken quoted chatgpt url should still mark presence without parsed value: %#v", state)
	}
	if state.CodexHooks == nil || *state.CodexHooks {
		t.Fatalf("expected codex_hooks=false, got %#v", state)
	}

	state = parseCodexConfigState("note without equals\ncodex_hooks = maybe\n")
	if state.CodexHooks != nil {
		t.Fatalf("invalid codex_hooks value should not parse: %#v", state)
	}
}

func TestStripCodexConfigInlineComment(t *testing.T) {
	t.Parallel()

	if got := stripCodexConfigInlineComment(`openai_base_url = "http://127.0.0.1:8990#frag" # trailing`); got != `openai_base_url = "http://127.0.0.1:8990#frag"` {
		t.Fatalf("unexpected inline comment stripping: %q", got)
	}
	if got := stripCodexConfigInlineComment(`codex_hooks = true # trailing`); got != `codex_hooks = true` {
		t.Fatalf("unexpected boolean comment stripping: %q", got)
	}
	if got := stripCodexConfigInlineComment(`openai_base_url = "http://127.0.0.1:8990\"quoted" # trailing`); got != `openai_base_url = "http://127.0.0.1:8990\"quoted"` {
		t.Fatalf("unexpected escaped quote handling: %q", got)
	}
	if got := stripCodexConfigInlineComment(`openai_base_url = "http://127.0.0.1:8990"`); got != `openai_base_url = "http://127.0.0.1:8990"` {
		t.Fatalf("unexpected no-comment handling: %q", got)
	}
}

func TestIsSlimferenceCodexBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want bool
	}{
		{raw: "http://127.0.0.1:8990", want: true},
		{raw: "http://localhost:8990/v1", want: true},
		{raw: "http://127.0.0.1:8990/other", want: false},
		{raw: "http://example.com:8990", want: false},
		{raw: "http://127.0.0.1:9000", want: false},
		{raw: "://bad-url", want: false},
	}

	for _, tc := range tests {
		if got := isSlimferenceCodexBaseURL(tc.raw); got != tc.want {
			t.Fatalf("isSlimferenceCodexBaseURL(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestCodexHookInstalled_noDir(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if CodexHookInstalled(home) {
		t.Fatal("should be false when no .codex dir exists")
	}
}

func TestCodexHookInstalled_emptyJSON(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if CodexHookInstalled(home) {
		t.Fatal("empty hooks.json should not report installed")
	}
}

func TestRemoveCodex_fullCleanup(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Install full codex hooks.
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}
	if !CodexHookInstalled(home) {
		t.Fatal("should be installed")
	}
	// Remove everything.
	if err := RemoveCodex(home); err != nil {
		t.Fatal(err)
	}
	if CodexHookInstalled(home) {
		t.Fatal("should not be installed after remove")
	}
	scriptPath := CodexHookScriptPath(home)
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Fatal("hook script should be removed")
	}
}

func TestVerifyReport_codexLegacyUpgrade(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Install Claude hook to prove it is still reported, but it no
	// longer controls VerifyReport's ok bit in Codex-only mode.
	if err := InstallClaude(home, ""); err != nil {
		t.Fatal(err)
	}
	// Create legacy AGENTS.md block (no hooks.json).
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(codexDir, "AGENTS.md")
	if err := os.WriteFile(agents, []byte(codexMarkerBegin+"\nstuff\n"+codexMarkerEnd+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyReport(home)
	if ok {
		t.Fatalf("legacy Codex block should still require hook upgrade: %v", lines)
	}
	var sawLegacy bool
	for _, ln := range lines {
		if strings.Contains(ln, "codex") && strings.Contains(ln, "legacy") {
			sawLegacy = true
		}
	}
	if !sawLegacy {
		t.Fatalf("expected legacy indicator: %v", lines)
	}
}

func TestVerifyReport_codexConfigConflictIgnored(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(CodexPreHookScriptPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexPreHookScriptPath(home), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexHookScriptPath(home), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexReadHookScriptPath(home), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	hooksJSON := `{
  "PreToolUse": [
    {"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-pre-tool.sh","statusMessage":"Slimference rewrite guard"}]},
    {"matcher":"Read","hooks":[{"type":"command","command":"bash /tmp/codex-read-tool.sh","statusMessage":"Slimference read cache"}]}
  ],
  "PostToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-post-tool.sh","statusMessage":"Slimference filter"}]}]
}`
	if err := os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(hooksJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("openai_base_url = \"http://example.com\"\ncodex_hooks = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, ok := VerifyReport(home)
	if ok {
		t.Fatal("verify report should still fail because Claude hooks are absent")
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "config conflict") {
		t.Fatalf("hook verify must not report config conflict, got %v", lines)
	}
	if !strings.Contains(joined, "config-patch status not checked") {
		t.Fatalf("expected config-patch owner hint, got %v", lines)
	}
}

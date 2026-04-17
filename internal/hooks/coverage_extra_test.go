package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripClaudePreToolUse_PreservesOtherHookSections(t *testing.T) {
	home := t.TempDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	data := `{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo keep"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := stripClaudePreToolUse(settingsPath); err != nil {
		t.Fatalf("stripClaudePreToolUse: %v", err)
	}
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(after), "PostToolUse") {
		t.Fatalf("unexpected stripped settings: %q", string(after))
	}
}

func TestStripClaudePreToolUse_PreservesRemainingPreToolEntries(t *testing.T) {
	home := t.TempDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	data := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo keep"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := stripClaudePreToolUse(settingsPath); err != nil {
		t.Fatalf("stripClaudePreToolUse: %v", err)
	}
	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(after), "PreToolUse") {
		t.Fatalf("expected remaining PreToolUse entry, got %q", string(after))
	}
}

func TestInstallCodex_PreAndPostScriptWriteErrors(t *testing.T) {
	t.Run("pre", func(t *testing.T) {
		home := t.TempDir()
		prePath := CodexPreHookScriptPath(home)
		if err := os.MkdirAll(prePath, 0o755); err != nil {
			t.Fatalf("mkdir pre path: %v", err)
		}
		err := InstallCodex(home, "slimference")
		if err == nil || !strings.Contains(err.Error(), "write pre-tool hook script") {
			t.Fatalf("expected pre script write error, got %v", err)
		}
	})

	t.Run("post", func(t *testing.T) {
		home := t.TempDir()
		postPath := CodexHookScriptPath(home)
		if err := os.MkdirAll(postPath, 0o755); err != nil {
			t.Fatalf("mkdir post path: %v", err)
		}
		err := InstallCodex(home, "slimference")
		if err == nil || !strings.Contains(err.Error(), "write hook script") {
			t.Fatalf("expected post script write error, got %v", err)
		}
	})
}

func TestInstallCodex_InstallHooksJSONError(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex")
	if err := os.WriteFile(codexPath, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	err := InstallCodex(home, "slimference")
	if err == nil || !strings.Contains(err.Error(), "write hooks.json") {
		t.Fatalf("expected hooks.json error, got %v", err)
	}
}

func TestInstallCodex_PatchConfigError(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(codexDir, "config.toml"), 0o755); err != nil {
		t.Fatalf("mkdir config path: %v", err)
	}
	err := InstallCodex(home, "slimference")
	if err == nil || !strings.Contains(err.Error(), "patch config.toml") {
		t.Fatalf("expected patch config error, got %v", err)
	}
}

func TestPatchCodexConfig_MkdirError(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".codex"), []byte("blocker"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	err := patchCodexConfig(home)
	if err == nil {
		t.Fatal("expected patchCodexConfig mkdir error")
	}
}

func TestInstallCodexAgentsMD_OpenError(t *testing.T) {
	home := t.TempDir()
	agentsPath := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(agentsPath, 0o755); err != nil {
		t.Fatalf("mkdir agents path: %v", err)
	}
	err := installCodexAgentsMD(home, "slimference")
	if err == nil {
		t.Fatal("expected AGENTS.md open error")
	}
}

func TestInstallCodexAgentsMD_MkdirError(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".codex"), []byte("blocker"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	err := installCodexAgentsMD(home, "slimference")
	if err == nil {
		t.Fatal("expected AGENTS.md mkdir error")
	}
}

func TestRemoveCodex_PropagatesHooksJSONError(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".codex"), []byte("blocker"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	err := RemoveCodex(home)
	if err == nil {
		t.Fatal("expected removeCodex hooks.json error")
	}
}

func TestRemoveCodex_PropagatesConfigError(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write hooks.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex", "config.toml"), 0o755); err != nil {
		t.Fatalf("mkdir config path: %v", err)
	}
	err := RemoveCodex(home)
	if err == nil {
		t.Fatal("expected removeCodex config error")
	}
}

func TestUnpatchCodexConfig_ReadError(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex", "config.toml"), 0o755); err != nil {
		t.Fatalf("mkdir config path: %v", err)
	}
	err := unpatchCodexConfig(home)
	if err == nil {
		t.Fatal("expected unpatchCodexConfig read error")
	}
}

func TestVerifyReport_CodexHooksFileWithoutSlimference(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(`{"PreToolUse":[]}`), 0o644); err != nil {
		t.Fatalf("write hooks.json: %v", err)
	}
	lines, ok := VerifyReport(home)
	if ok {
		t.Fatal("verify report should be false when claude is missing")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "file exists (no slimference hook)") {
		t.Fatalf("unexpected verify output: %s", joined)
	}
}

func TestVerifyReport_CodexIncompleteAndAgentsFallbackWithoutMarker(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(`{"PreToolUse":[{"hooks":[{"command":"bash codex-pre-tool.sh"}]}]}`), 0o644); err != nil {
		t.Fatalf("write hooks.json: %v", err)
	}
	lines, ok := VerifyReport(home)
	if ok {
		t.Fatal("expected incomplete codex install to fail verify")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "config MISSING") {
		t.Fatalf("unexpected verify output: %v", lines)
	}

	if err := os.Remove(filepath.Join(home, ".codex", "hooks.json")); err != nil {
		t.Fatalf("remove hooks.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "AGENTS.md"), []byte("plain text"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	lines, _ = VerifyReport(home)
	if !strings.Contains(strings.Join(lines, "\n"), "not installed") {
		t.Fatalf("unexpected fallback verify output: %v", lines)
	}
}

func TestInstallCodexHooksJSONWithScripts_MarshalError(t *testing.T) {
	orig := jsonMarshalIndentFn
	jsonMarshalIndentFn = func(any, string, string) ([]byte, error) {
		return nil, errors.New("marshal boom")
	}
	defer func() {
		jsonMarshalIndentFn = orig
	}()

	err := installCodexHooksJSONWithScripts(t.TempDir(), "/tmp/pre.sh", "/tmp/post.sh")
	if err == nil || !strings.Contains(err.Error(), "marshal boom") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestRemoveCodexHooksJSON_MarshalError(t *testing.T) {
	home := t.TempDir()
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"PreToolUse":[],"PostToolUse":[]}`), 0o644); err != nil {
		t.Fatalf("write hooks.json: %v", err)
	}

	orig := jsonMarshalIndentFn
	jsonMarshalIndentFn = func(any, string, string) ([]byte, error) {
		return nil, errors.New("marshal boom")
	}
	defer func() {
		jsonMarshalIndentFn = orig
	}()

	err := removeCodexHooksJSON(home)
	if err == nil || !strings.Contains(err.Error(), "marshal boom") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestVerifyReport_CodexConfigIncomplete(t *testing.T) {
	home := t.TempDir()
	prePath := CodexPreHookScriptPath(home)
	postPath := CodexHookScriptPath(home)
	if err := os.MkdirAll(filepath.Dir(prePath), 0o755); err != nil {
		t.Fatalf("mkdir hook dir: %v", err)
	}
	if err := os.WriteFile(prePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write pre hook: %v", err)
	}
	if err := os.WriteFile(postPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write post hook: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	hooksJSON := `{"PreToolUse":[{"hooks":[{"command":"bash ` + prePath + `"}]}],"PostToolUse":[{"hooks":[{"command":"bash ` + postPath + `"}]}]}`
	if err := os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(hooksJSON), 0o644); err != nil {
		t.Fatalf("write hooks.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[features]\ncodex_hooks = true\n"), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	lines, ok := VerifyReport(home)
	if ok {
		t.Fatal("expected incomplete config verification failure")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "config incomplete") {
		t.Fatalf("unexpected verify output: %v", lines)
	}
}

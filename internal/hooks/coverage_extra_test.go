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

func TestInstallCodex_LifecycleScriptWriteErrors(t *testing.T) {
	tests := []struct {
		name string
		path func(string) string
		want string
	}{
		{"read", CodexReadHookScriptPath, "write read-tool hook script"},
		{"session", CodexSessionStartHookScriptPath, "write session-start hook script"},
		{"permission", CodexPermissionHookScriptPath, "write permission-request hook script"},
		{"user_prompt", CodexUserPromptHookScriptPath, "write user-prompt-submit hook script"},
		{"stop", CodexStopHookScriptPath, "write stop hook script"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.MkdirAll(tc.path(home), 0o755); err != nil {
				t.Fatalf("mkdir blocker: %v", err)
			}
			err := InstallCodex(home, "slimference")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestEnsureCodexHooksFeatureBranches(t *testing.T) {
	t.Run("mkdir_error", func(t *testing.T) {
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, ".codex"), []byte("blocker"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureCodexHooksFeature(home); err == nil {
			t.Fatal("expected mkdir error")
		}
	})
	t.Run("conflicting_false", func(t *testing.T) {
		home := t.TempDir()
		configPath := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte("[features]\ncodex_hooks = false\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureCodexHooksFeature(home); err == nil || !strings.Contains(err.Error(), "codex_hooks=false") {
			t.Fatalf("expected conflict, got %v", err)
		}
	})
	t.Run("conflicting_current_hooks_false", func(t *testing.T) {
		home := t.TempDir()
		configPath := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte("[features]\nhooks = false\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureCodexHooksFeature(home); err == nil || !strings.Contains(err.Error(), "hooks=false") {
			t.Fatalf("expected hooks conflict, got %v", err)
		}
	})
	t.Run("already_true", func(t *testing.T) {
		home := t.TempDir()
		configPath := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatal(err)
		}
		before := "[features]\ncodex_hooks = true\n"
		if err := os.WriteFile(configPath, []byte(before), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureCodexHooksFeature(home); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		after, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != before {
			t.Fatalf("already true should not rewrite: %q", string(after))
		}
	})
	t.Run("insert_existing_features", func(t *testing.T) {
		home := t.TempDir()
		configPath := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte("[features]\nother = true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureCodexHooksFeature(home); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		after, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(after), slimferenceCodexHooksLine) || !strings.Contains(string(after), "other = true") {
			t.Fatalf("unexpected config: %q", string(after))
		}
	})
	t.Run("append_new_features_to_nonempty_file", func(t *testing.T) {
		home := t.TempDir()
		configPath := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte(`model = "gpt-5"`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureCodexHooksFeature(home); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		after, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(after), "model = \"gpt-5\"\n\n[features]\n"+slimferenceCodexHooksLine) {
			t.Fatalf("unexpected config: %q", string(after))
		}
	})
	t.Run("config_path_directory_error", func(t *testing.T) {
		home := t.TempDir()
		configPath := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(configPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := ensureCodexHooksFeature(home); err == nil {
			t.Fatal("expected config read error")
		}
	})
}

func TestCodexHookEventAndManagedLineBranches(t *testing.T) {
	t.Parallel()

	if isCodexHookEvent("NotARealHook") {
		t.Fatal("unknown hook event should not be accepted")
	}
	for _, event := range []string{"PreCompact", "PostCompact", "SubagentStart", "SubagentStop"} {
		if !isCodexHookEvent(event) {
			t.Fatalf("%s should be accepted as a Codex hook event", event)
		}
	}
	content := "model = \"gpt-5\"\n" + slimferenceCodexHooksLine + "\n"
	cleaned := removeManagedCodexHooksLine(content, slimferenceCodexHooksLine)
	if strings.Contains(cleaned, slimferenceCodexHooksLine) {
		t.Fatalf("managed hooks line was not removed: %s", cleaned)
	}
	if !strings.Contains(cleaned, "model = \"gpt-5\"") {
		t.Fatalf("unrelated config was removed: %s", cleaned)
	}
}

func TestValidateCodexConfig_currentHooksFalseConflict(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[features]\nhooks = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := validateCodexConfig(configPath)
	if err == nil || !strings.Contains(err.Error(), "hooks=false") {
		t.Fatalf("expected hooks=false conflict, got %v", err)
	}
}

func TestInstallCodex_InstallHooksJSONError(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex")
	if err := os.WriteFile(codexPath, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	err := InstallCodex(home, "slimference")
	if err == nil || !strings.Contains(err.Error(), "hooks.json") {
		t.Fatalf("expected hooks.json path error, got %v", err)
	}
}

func TestInstallCodex_ConfigPathDirectoryFailsFeatureFlag(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(codexDir, "config.toml"), 0o755); err != nil {
		t.Fatalf("mkdir config path: %v", err)
	}
	err := InstallCodex(home, "slimference")
	if err == nil || !strings.Contains(err.Error(), "enable codex hooks feature") {
		t.Fatalf("expected config feature flag error, got %v", err)
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

func TestRemoveCodex_IgnoresConfigPath(t *testing.T) {
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
	if err != nil {
		t.Fatalf("hook remove should not touch config.toml, got %v", err)
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
	if !strings.Contains(strings.Join(lines, "\n"), "script MISSING") {
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

	err := installCodexHooksJSONWithScripts(t.TempDir(), "/tmp/pre.sh", "/tmp/post.sh", "/tmp/read.sh")
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

func TestVerifyReport_CodexConfigPatchIgnored(t *testing.T) {
	home := t.TempDir()
	if err := InstallCodex(home, "slimference"); err != nil {
		t.Fatalf("install codex: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[features]\ncodex_hooks = true\n"), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	lines, ok := VerifyCodexReport(home)
	if !ok {
		t.Fatalf("expected config-patch to be ignored by hook verify, got lines=%v", lines)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "config-patch status not checked") {
		t.Fatalf("unexpected verify output: %v", lines)
	}
}

func TestVerifyCodexReport_FileExistsWithoutSlimferenceHook(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(`{"PreToolUse":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyCodexReport(home)
	if ok {
		t.Fatal("codex target verify should fail when hooks.json lacks Slimference hooks")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "file exists (no slimference hook)") {
		t.Fatalf("unexpected verify output: %v", lines)
	}
}

func TestVerifyCodexReport_NotExecutableScripts(t *testing.T) {
	home := t.TempDir()
	prePath := CodexPreHookScriptPath(home)
	postPath := CodexHookScriptPath(home)
	readPath := CodexReadHookScriptPath(home)
	if err := os.MkdirAll(filepath.Dir(prePath), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{prePath, postPath, readPath} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hooksJSON := `{"PreToolUse":[{"hooks":[{"command":"bash ` + prePath + `"}]},{"hooks":[{"command":"bash ` + readPath + `"}]}],"PostToolUse":[{"hooks":[{"command":"bash ` + postPath + `"}]}]}`
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(hooksJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("openai_base_url = \"http://127.0.0.1:8990\"\nchatgpt_base_url = \"http://127.0.0.1:8990\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyCodexReport(home)
	if ok {
		t.Fatal("codex target verify should fail when scripts are not executable")
	}
	if strings.Count(strings.Join(lines, "\n"), "script NOT_EXECUTABLE") != 3 {
		t.Fatalf("expected all scripts not executable, got %v", lines)
	}
}

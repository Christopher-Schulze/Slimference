package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledStatus_ClaudeSettingsMissingHookIsFalse(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	scriptDir := filepath.Join(home, ".claude", "hooks")
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "slimference-rewrite.sh"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"hooks":{"PreToolUse":[]}}`), 0644); err != nil {
		t.Fatal(err)
	}

	claude, codex := InstalledStatus(home)
	if claude {
		t.Fatal("want claude=false when script exists but settings.json is not wired")
	}
	if codex {
		t.Fatal("want codex=false in claude-only test")
	}
}

func TestInstalledStatus_CodexIncompleteHooksJSONIsFalse(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0755); err != nil {
		t.Fatal(err)
	}
	raw := `{
  "PreToolUse": [
    {"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-pre-tool.sh","statusMessage":"Slimference rewrite guard"}]},
    {"matcher":"Read","hooks":[{"type":"command","command":"bash /tmp/codex-read-tool.sh","statusMessage":"Slimference read cache"}]}
  ],
  "PostToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-post-tool.sh","statusMessage":"Slimference filter"}]}]
}`
	if err := os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	claude, codex := InstalledStatus(home)
	if claude {
		t.Fatal("want claude=false in codex-only test")
	}
	if codex {
		t.Fatal("want codex=false when hooks.json exists but scripts/config are missing")
	}
}

func TestInstalledStatus_CodexLegacyFallbackStillTrue(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	agentsPath := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(agentsPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentsPath, []byte("before\n"+codexMarkerBegin+"\nlegacy\n"+codexMarkerEnd+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	claude, codex := InstalledStatus(home)
	if claude {
		t.Fatal("want claude=false in codex legacy test")
	}
	if !codex {
		t.Fatal("want codex=true for legacy AGENTS fallback")
	}
}

func TestInstalledStatus_CodexMissingPostScriptIsFalse(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	raw := `{
  "PreToolUse": [
    {"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-pre-tool.sh","statusMessage":"Slimference rewrite guard"}]},
    {"matcher":"Read","hooks":[{"type":"command","command":"bash /tmp/codex-read-tool.sh","statusMessage":"Slimference read cache"}]}
  ],
  "PostToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-post-tool.sh","statusMessage":"Slimference filter"}]}]
}`
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(CodexPreHookScriptPath(home)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexPreHookScriptPath(home), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexReadHookScriptPath(home), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	config := "openai_base_url = \"http://127.0.0.1:8787/v1\"\ncodex_hooks = true\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	_, codex := InstalledStatus(home)
	if codex {
		t.Fatal("want codex=false when post hook script is missing")
	}
}

func TestInstalledStatus_CodexIncompleteConfigIsFalse(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	raw := `{
  "PreToolUse": [
    {"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-pre-tool.sh","statusMessage":"Slimference rewrite guard"}]},
    {"matcher":"Read","hooks":[{"type":"command","command":"bash /tmp/codex-read-tool.sh","statusMessage":"Slimference read cache"}]}
  ],
  "PostToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-post-tool.sh","statusMessage":"Slimference filter"}]}]
}`
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(CodexPreHookScriptPath(home)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexPreHookScriptPath(home), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexReadHookScriptPath(home), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexHookScriptPath(home), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	config := "openai_base_url = \"http://127.0.0.1:8787/v1\"\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	_, codex := InstalledStatus(home)
	if codex {
		t.Fatal("want codex=false when config.toml is incomplete")
	}
}

func TestInstalledStatus_CodexMissingConfigIsFalse(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	raw := `{
  "PreToolUse": [
    {"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-pre-tool.sh","statusMessage":"Slimference rewrite guard"}]},
    {"matcher":"Read","hooks":[{"type":"command","command":"bash /tmp/codex-read-tool.sh","statusMessage":"Slimference read cache"}]}
  ],
  "PostToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"bash /tmp/codex-post-tool.sh","statusMessage":"Slimference filter"}]}]
}`
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(CodexPreHookScriptPath(home)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexPreHookScriptPath(home), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexReadHookScriptPath(home), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CodexHookScriptPath(home), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}

	_, codex := InstalledStatus(home)
	if codex {
		t.Fatal("want codex=false when config.toml is missing")
	}
}

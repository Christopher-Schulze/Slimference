package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveClaudeSlimferenceHooks(t *testing.T) {
	t.Parallel()

	scriptPath := "/tmp/slimference-rewrite.sh"
	entries := []interface{}{
		"raw",
		map[string]interface{}{"matcher": "Bash"},
		map[string]interface{}{
			"matcher": "Bash",
			"hooks": []interface{}{
				map[string]interface{}{"command": "bash /tmp/other.sh"},
				map[string]interface{}{"command": "bash " + scriptPath},
			},
		},
		map[string]interface{}{
			"matcher": "Edit",
			"hooks": []interface{}{
				map[string]interface{}{"command": "bash /tmp/slimference-rewrite.sh"},
			},
		},
	}

	filtered := removeClaudeSlimferenceHooks(entries, scriptPath)
	if len(filtered) != 3 {
		t.Fatalf("unexpected filtered length: %d (%#v)", len(filtered), filtered)
	}
	last, ok := filtered[2].(map[string]interface{})
	if !ok {
		t.Fatalf("expected third entry map, got %#v", filtered[2])
	}
	hooksSlice := last["hooks"].([]interface{})
	if len(hooksSlice) != 1 {
		t.Fatalf("expected slimference hook removal, got %#v", hooksSlice)
	}
}

func TestIsClaudeSlimferenceHook(t *testing.T) {
	t.Parallel()

	if isClaudeSlimferenceHook("not-a-map", "/tmp/slimference-rewrite.sh") {
		t.Fatal("non-map hook should be false")
	}
	if isClaudeSlimferenceHook(map[string]interface{}{"command": ""}, "/tmp/slimference-rewrite.sh") {
		t.Fatal("empty command should be false")
	}
	if isClaudeSlimferenceHook(map[string]interface{}{"command": "bash /tmp/other.sh"}, "/tmp/slimference-rewrite.sh") {
		t.Fatal("non-slimference command should be false")
	}
	if !isClaudeSlimferenceHook(map[string]interface{}{"command": "bash /tmp/slimference-rewrite.sh"}, "/tmp/custom/slimference-rewrite.sh") {
		t.Fatal("basename match should be true")
	}
}

func TestInstallCodexHooksJSONWithScripts_ReplacesExistingSlimferenceEntries(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0755); err != nil {
		t.Fatal(err)
	}
	raw := map[string]interface{}{
		"PreToolUse": []interface{}{
			map[string]interface{}{
				"matcher": "Bash",
				"hooks": []interface{}{
					map[string]interface{}{"command": "bash /old/codex-pre-tool.sh"},
				},
			},
			map[string]interface{}{
				"matcher": "Keep",
				"hooks": []interface{}{
					map[string]interface{}{"command": "bash /keep.sh"},
				},
			},
		},
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := installCodexHooksJSONWithScripts(home, "/new/codex-pre-tool.sh", "/new/codex-post-tool.sh", "/new/codex-read-tool.sh"); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if strings.Contains(text, "/old/codex-pre-tool.sh") {
		t.Fatalf("old slimference entry should be replaced: %s", text)
	}
	if !strings.Contains(text, "/new/codex-pre-tool.sh") || !strings.Contains(text, "/new/codex-post-tool.sh") || !strings.Contains(text, "/new/codex-read-tool.sh") {
		t.Fatalf("new scripts missing: %s", text)
	}
	if !strings.Contains(text, "/keep.sh") {
		t.Fatalf("non-slimference entry should remain: %s", text)
	}
}

func TestPatchAndUnpatchCodexConfig(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[features]\nother = true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := patchCodexConfig(home); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "openai_base_url") || !strings.Contains(text, "hooks = true") {
		t.Fatalf("patchCodexConfig missing fields: %s", text)
	}

	if err := unpatchCodexConfig(home); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text = string(data)
	if strings.Contains(text, "openai_base_url") || strings.Contains(text, "hooks = true") {
		t.Fatalf("unpatchCodexConfig should remove slimference additions: %s", text)
	}
	if !strings.Contains(text, "other = true") {
		t.Fatalf("unpatchCodexConfig should preserve unrelated config: %s", text)
	}
}

func TestPatchCodexConfig_ConflictingOpenAIBaseURLReturnsError(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	raw := "openai_base_url = \"http://example.com\"\n"
	if err := os.WriteFile(configPath, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	err := patchCodexConfig(home)
	if err == nil || !strings.Contains(err.Error(), "conflicting openai_base_url") {
		t.Fatalf("expected conflicting openai_base_url error, got %v", err)
	}
}

func TestInstallCodexAndRemoveCodex_ErrorPaths(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	blocker := filepath.Join(home, ".slimference", "hooks")
	if err := os.MkdirAll(filepath.Dir(blocker), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(home, "slimference"); err == nil {
		t.Fatal("expected InstallCodex to fail when hooks dir cannot be created")
	}

	home2 := t.TempDir()
	codexDir := filepath.Join(home2, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("config"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCodex(home2); err != nil {
		t.Fatalf("RemoveCodex must ignore AGENTS.md entirely and clean hooks/config only: %v", err)
	}
}

func TestRemoveCodexHooksJSON_NotExistAndStripClaudeNoHooks(t *testing.T) {
	t.Parallel()

	if err := removeCodexHooksJSON(t.TempDir()); err != nil {
		t.Fatalf("removeCodexHooksJSON missing file: %v", err)
	}

	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"hooks":"bad"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := stripClaudePreToolUse(settings); err != nil {
		t.Fatalf("stripClaudePreToolUse with non-map hooks should be ignored: %v", err)
	}
}

func TestRemoveCodexHooksJSON_InvalidJSONIsIgnored(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeCodexHooksJSON(home); err != nil {
		t.Fatalf("invalid hooks.json should be ignored, got %v", err)
	}
}

func TestRemoveCodexHookEventAndMergeHelpers(t *testing.T) {
	t.Parallel()

	existing := map[string]interface{}{
		"PreToolUse": []interface{}{
			map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{"command": "bash /tmp/codex-pre-tool.sh"},
				},
			},
			map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{"command": "bash /tmp/keep.sh"},
				},
			},
		},
	}
	removeCodexHookEvent(existing, "PreToolUse")
	entries := existing["PreToolUse"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected only non-slimference entry to remain, got %#v", entries)
	}

	merged := mergeCodexHookEntries([]interface{}{
		map[string]interface{}{"hooks": []interface{}{map[string]interface{}{"command": "bash /tmp/codex-post-tool.sh"}}},
		map[string]interface{}{"hooks": []interface{}{map[string]interface{}{"command": "bash /tmp/keep.sh"}}},
	}, map[string]interface{}{"matcher": "Bash"})
	if len(merged) != 2 {
		t.Fatalf("unexpected merged entries: %#v", merged)
	}
}

func TestCodexEntryHasSlimferenceHook(t *testing.T) {
	t.Parallel()

	if codexEntryHasSlimferenceHook("bad") {
		t.Fatal("non-map entry should be false")
	}
	if codexEntryHasSlimferenceHook(map[string]interface{}{"hooks": []interface{}{"bad"}}) {
		t.Fatal("non-map hook should be false")
	}
	if !codexEntryHasSlimferenceHook(map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{"statusMessage": "Slimference rewrite guard"},
		},
	}) {
		t.Fatal("status message should identify slimference hook")
	}
}

func TestVerifyReport_CodexIncompleteAndLegacy(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"PreToolUse":[{"hooks":[{"command":"bash /tmp/codex-pre-tool.sh"}]}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	lines, ok := VerifyReport(home)
	if ok {
		t.Fatal("incomplete codex install should not verify cleanly")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "script MISSING") {
		t.Fatalf("expected missing script report, got %v", lines)
	}

	legacyHome := t.TempDir()
	agentsPath := filepath.Join(legacyHome, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(agentsPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentsPath, []byte(codexMarkerBegin+"\nlegacy\n"+codexMarkerEnd), 0644); err != nil {
		t.Fatal(err)
	}
	lines, ok = VerifyReport(legacyHome)
	if ok {
		t.Fatal("missing claude hook should still fail verification")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "legacy instruction block") {
		t.Fatalf("expected legacy codex report, got %v", lines)
	}
}

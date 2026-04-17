package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// codexMarkerBegin/End kept for backwards compatibility with old AGENTS.md installs.
const codexMarkerBegin = "<!-- slimference:begin -->"
const codexMarkerEnd = "<!-- slimference:end -->"

// codexHookScript returns the shell script content for the Codex PostToolUse hook.
// The hook receives JSON on stdin with tool_response containing the Bash output.
// It pipes the output through slimference filter and returns a filtered version.
func codexHookScript(slimferenceCmd string) string {
	cmd := strings.TrimSpace(slimferenceCmd)
	if cmd == "" {
		cmd = "slimference"
	}
	return "#!/bin/bash\n# Slimference PostToolUse hook for Codex CLI\nset -e\nINPUT=$(cat)\nRESPONSE=$(echo \"$INPUT\" | python3 -c \"import sys,json; d=json.load(sys.stdin); print(d.get('tool_response',''))\" 2>/dev/null || echo \"\")\nif [ -z \"$RESPONSE\" ]; then exit 0; fi\nFILTERED=$(" + cmd + " filter -- \"$RESPONSE\" 2>/dev/null || echo \"$RESPONSE\")\nif [ \"$FILTERED\" = \"$RESPONSE\" ]; then exit 0; fi\nprintf '%s' \"{\\\"decision\\\":\\\"block\\\",\\\"reason\\\":\\\"\" \"$(echo \"$FILTERED\" | sed 's/\"/\\\\\"/g')\" \"\\\",\\\"hookSpecificOutput\\\":{\\\"hookEventName\\\":\\\"PostToolUse\\\",\\\"additionalContext\\\":\\\"Output filtered by Slimference\\\"}}\"\n"
}

// InstallCodex installs the Slimference hooks for Codex CLI.
//
// Writes:
//  1. ~/.slimference/hooks/codex-post-tool.sh - PostToolUse Bash hook script
//  2. ~/.codex/hooks.json - PostToolUse Bash hook entry
//  3. ~/.codex/config.toml - adds openai_base_url + codex_hooks feature flag
//
// If a hooks.json already exists with Slimference entries, it is not modified.
// If config.toml already has openai_base_url set, the value is not overwritten.
func InstallCodex(home string, slimferenceCmd string) error {
	// Step 1: Write hook script to ~/.slimference/hooks/
	hooksDir := filepath.Join(home, ".slimference", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}
	scriptPath := filepath.Join(hooksDir, "codex-post-tool.sh")
	scriptContent := codexHookScript(slimferenceCmd)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("write hook script: %w", err)
	}

	// Step 2: Write/update ~/.codex/hooks.json
	if err := installCodexHooksJSON(home, scriptPath); err != nil {
		return fmt.Errorf("write hooks.json: %w", err)
	}

	// Step 3: Patch ~/.codex/config.toml
	if err := patchCodexConfig(home); err != nil {
		return fmt.Errorf("patch config.toml: %w", err)
	}

	// Step 4: Also keep AGENTS.md block for backwards compat with older Codex versions
	_ = installCodexAgentsMD(home, slimferenceCmd)

	return nil
}

// installCodexHooksJSON writes or merges Slimference hooks into ~/.codex/hooks.json.
func installCodexHooksJSON(home string, scriptPath string) error {
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return err
	}

	hooksPath := filepath.Join(codexDir, "hooks.json")

	// Read existing hooks.json if present.
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(hooksPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	// Check if Slimference hook already exists in PostToolUse.
	postToolUse, _ := existing["PostToolUse"].([]interface{})
	for _, entry := range postToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			hooks, _ := m["hooks"].([]interface{})
			for _, h := range hooks {
				if fn, ok := h.(map[string]interface{}); ok {
					// Check both command path and statusMessage for slimference.
					cmd, _ := fn["command"].(string)
					msg, _ := fn["statusMessage"].(string)
					if strings.Contains(strings.ToLower(cmd), "slimference") ||
						strings.Contains(strings.ToLower(msg), "slimference") {
						return nil // already installed
					}
				}
			}
		}
	}

	// Build the Slimference PostToolUse hook entry.
	entry := map[string]interface{}{
		"matcher": "Bash",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":          "command",
				"command":       fmt.Sprintf("bash %s", scriptPath),
				"statusMessage": "Slimference filter",
			},
		},
	}

	// Append to existing PostToolUse array or create new one.
	postToolUse = append(postToolUse, entry)
	existing["PostToolUse"] = postToolUse

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(hooksPath, append(data, '\n'), 0644)
}

// patchCodexConfig merges Slimference settings into ~/.codex/config.toml.
// Only adds keys that don't already exist.
func patchCodexConfig(home string) error {
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(codexDir, "config.toml")
	data, _ := os.ReadFile(configPath)
	content := string(data)

	var additions []string

	// Add openai_base_url only if not already present.
	if !strings.Contains(content, "openai_base_url") {
		additions = append(additions, "\n# Slimference proxy endpoint\nopenai_base_url = \"http://127.0.0.1:8990\"\n")
	}

	// Add codex_hooks feature flag only if not already present.
	if !strings.Contains(content, "codex_hooks") {
		if strings.Contains(content, "[features]") {
			// Insert after [features] line
			content = strings.Replace(content, "[features]", "[features]\ncodex_hooks = true  # Slimference: enable lifecycle hooks", 1)
		} else {
			additions = append(additions, "\n[features]\ncodex_hooks = true  # Slimference: enable lifecycle hooks\n")
		}
	}

	if len(additions) > 0 {
		content += strings.Join(additions, "")
	}

	return os.WriteFile(configPath, []byte(content), 0644)
}

// installCodexAgentsMD appends a Slimference block to ~/.codex/AGENTS.md (legacy fallback).
func installCodexAgentsMD(home string, slimferenceCmd string) error {
	p := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	prev, _ := os.ReadFile(p)
	if strings.Contains(string(prev), codexMarkerBegin) {
		return nil
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(prev) > 0 && !strings.HasSuffix(string(prev), "\n") {
		_, _ = f.WriteString("\n")
	}
	block := CodexAgentsBlock(slimferenceCmd)
	_, err = f.WriteString(strings.TrimPrefix(block, "\n"))
	return err
}

// RemoveCodex removes all Slimference artifacts from the Codex configuration.
func RemoveCodex(home string) error {
	// Remove hooks.json Slimference entry.
	if err := removeCodexHooksJSON(home); err != nil {
		return err
	}

	// Remove config.toml Slimference additions.
	if err := unpatchCodexConfig(home); err != nil {
		return err
	}

	// Remove hook script.
	scriptPath := filepath.Join(home, ".slimference", "hooks", "codex-post-tool.sh")
	_ = os.Remove(scriptPath)

	// Remove AGENTS.md block (legacy).
	return removeCodexAgentsMD(home)
}

// removeCodexHooksJSON removes the Slimference entry from ~/.codex/hooks.json.
func removeCodexHooksJSON(home string) error {
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	existing := make(map[string]interface{})
	if err := json.Unmarshal(data, &existing); err != nil {
		return nil // not valid JSON, nothing to remove
	}

	postToolUse, _ := existing["PostToolUse"].([]interface{})
	filtered := make([]interface{}, 0, len(postToolUse))
	for _, entry := range postToolUse {
		m, ok := entry.(map[string]interface{})
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		hooks, _ := m["hooks"].([]interface{})
		hasSlimference := false
		for _, h := range hooks {
			if fn, ok := h.(map[string]interface{}); ok {
				cmd, _ := fn["command"].(string)
				if strings.Contains(cmd, "slimference") {
					hasSlimference = true
					break
				}
			}
		}
		if !hasSlimference {
			filtered = append(filtered, entry)
		}
	}

	if len(filtered) == 0 {
		delete(existing, "PostToolUse")
	} else {
		existing["PostToolUse"] = filtered
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(hooksPath, append(out, '\n'), 0644)
}

// unpatchCodexConfig removes Slimference additions from ~/.codex/config.toml.
func unpatchCodexConfig(home string) error {
	configPath := filepath.Join(home, ".codex", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content := string(data)

	// Remove Slimference comment lines and openai_base_url line.
	lines := strings.Split(content, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# Slimference") {
			continue
		}
		if strings.HasPrefix(trimmed, "openai_base_url") && strings.Contains(trimmed, "8990") {
			continue
		}
		if trimmed == "codex_hooks = true" || strings.HasPrefix(trimmed, "codex_hooks = true") {
			continue
		}
		cleaned = append(cleaned, line)
	}

	result := strings.Join(cleaned, "\n")
	// Clean up empty [features] section.
	result = strings.Replace(result, "[features]\n\n", "", 1)
	result = strings.Replace(result, "[features]\n", "", 1)

	return os.WriteFile(configPath, []byte(result), 0644)
}

// removeCodexAgentsMD removes the Slimference block from ~/.codex/AGENTS.md.
func removeCodexAgentsMD(home string) error {
	p := filepath.Join(home, ".codex", "AGENTS.md")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s := string(data)
	i := strings.Index(s, codexMarkerBegin)
	if i < 0 {
		return nil
	}
	j := strings.Index(s[i:], codexMarkerEnd)
	if j < 0 {
		return fmt.Errorf("slimference: unclosed marker in AGENTS.md")
	}
	j += i + len(codexMarkerEnd)
	out := strings.TrimSpace(s[:i] + s[j:])
	return os.WriteFile(p, []byte(out+"\n"), 0644)
}

// CodexAgentsBlock returns the markdown block for AGENTS.md (legacy fallback).
func CodexAgentsBlock(slimferenceCmd string) string {
	cmd := strings.TrimSpace(slimferenceCmd)
	if cmd == "" {
		cmd = "slimference"
	}
	return `

` + codexMarkerBegin + `
## Slimference (shell output)

When running shell commands, prefer wrapping them with:

` + fmt.Sprintf("`%s filter`", cmd) + ` so that command output is compacted before it enters the context.

Example: ` + fmt.Sprintf("`%s filter git status`", cmd) + ` instead of ` + "`git status`" + `.

` + codexMarkerEnd + `
`
}

// CodexHookInstalled checks whether the Codex hooks.json has a Slimference entry.
func CodexHookInstalled(home string) bool {
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, "slimference") || strings.Contains(content, "Slimference")
}

// CodexHookScriptPath returns the path to the installed Codex hook script.
func CodexHookScriptPath(home string) string {
	return filepath.Join(home, ".slimference", "hooks", "codex-post-tool.sh")
}

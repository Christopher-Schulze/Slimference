package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClaudeHookScript returns the bash hook body; slimferenceCmd is the executable for `rewrite` (default "slimference").
func ClaudeHookScript(slimferenceCmd string) string {
	cmd := strings.TrimSpace(slimferenceCmd)
	if cmd == "" {
		cmd = "slimference"
	}
	q := bashSingleQuoted(cmd)
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
INPUT=$(cat)
CMD=$(printf '%%s' "$INPUT" | jq -r '.command // .tool_input.command // empty' 2>/dev/null || true)
if [[ -z "${CMD:-}" ]]; then
  echo "slimference: could not read .command from hook JSON" >&2
  exit 1
fi
exec %s rewrite -- $CMD
`, q)
}

func bashSingleQuoted(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'"'"'`) + `'`
}

// InstallClaude writes ~/.claude/hooks/slimference-rewrite.sh and merges ~/.claude/settings.json.
// slimferenceCmd is embedded in the script (empty = "slimference").
func InstallClaude(home string, slimferenceCmd string) error {
	hookDir := filepath.Join(home, ".claude", "hooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		return err
	}
	scriptPath := filepath.Join(hookDir, "slimference-rewrite.sh")
	if err := os.WriteFile(scriptPath, []byte(ClaudeHookScript(slimferenceCmd)), 0755); err != nil {
		return err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	return mergeClaudeSettings(settingsPath, scriptPath)
}

// RemoveClaude removes the hook script and drops PreToolUse from settings when present.
func RemoveClaude(home string) error {
	scriptPath := filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh")
	_ = os.Remove(scriptPath)
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	return stripClaudePreToolUse(settingsPath)
}

func mergeClaudeSettings(settingsPath, scriptPath string) error {
	var root map[string]interface{}
	data, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse settings.json: %w", err)
		}
	}
	if root == nil {
		root = make(map[string]interface{})
	}
	hooksObj, _ := root["hooks"].(map[string]interface{})
	if hooksObj == nil {
		hooksObj = make(map[string]interface{})
	}
	hooksObj["PreToolUse"] = []interface{}{
		map[string]interface{}{
			"matcher": "Bash",
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": "bash " + scriptPath,
				},
			},
		},
	}
	root["hooks"] = hooksObj
	out, _ := json.MarshalIndent(root, "", "  ")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, out, 0644)
}

func stripClaudePreToolUse(settingsPath string) error {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	hooksObj, ok := root["hooks"].(map[string]interface{})
	if !ok {
		return nil
	}
	delete(hooksObj, "PreToolUse")
	if len(hooksObj) == 0 {
		delete(root, "hooks")
	}
	out, _ := json.MarshalIndent(root, "", "  ")
	return os.WriteFile(settingsPath, out, 0644)
}

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
  exit 0
fi
TMP_ERR=$(mktemp)
cleanup() {
  rm -f "$TMP_ERR"
}
trap cleanup EXIT

set +e
REWRITTEN=$(%s rewrite -- "$CMD" 2>"$TMP_ERR")
STATUS=$?
set -e
ERR_MSG=$(cat "$TMP_ERR")

case "$STATUS" in
  0)
    if [[ -z "${REWRITTEN:-}" || "$REWRITTEN" == "$CMD" ]]; then
      exit 0
    fi
    printf '%%s' "$INPUT" | jq -c --arg cmd "$REWRITTEN" '{hookSpecificOutput:{hookEventName:"PreToolUse",updatedInput:(.tool_input + {command:$cmd})}}'
    ;;
  1)
    exit 0
    ;;
  2)
    if [[ -z "${ERR_MSG:-}" ]]; then
      ERR_MSG="Slimference blocked this command."
    fi
    jq -nc --arg reason "$ERR_MSG" '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$reason}}'
    ;;
  3)
    if [[ -z "${ERR_MSG:-}" ]]; then
      ERR_MSG="Slimference requires confirmation before running this command."
    fi
    jq -nc --arg reason "$ERR_MSG" '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"ask",permissionDecisionReason:$reason}}'
    ;;
  *)
    if [[ -n "${ERR_MSG:-}" ]]; then
      echo "$ERR_MSG" >&2
    fi
    exit 1
    ;;
esac
`, q)
}

// ClaudeReadHookScript returns the bash hook body for Claude Read PreToolUse hooks.
func ClaudeReadHookScript(slimferenceCmd string) string {
	cmd := strings.TrimSpace(slimferenceCmd)
	if cmd == "" {
		cmd = "slimference"
	}
	q := bashSingleQuoted(cmd)
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
INPUT=$(cat)
printf '%%s' "$INPUT" | %s readhook
`, q)
}

// ClaudePostToolHookScript returns the bash hook body for Claude PostToolUse
// output replacement. It is intentionally default-off: installing Claude hooks
// prepares the max hook path without mutating Claude sessions until
// SLIMFERENCE_CLAUDE_HOOK_MODE is set to max/compact/aggressive/auto.
func ClaudePostToolHookScript(slimferenceCmd string) string {
	cmd := strings.TrimSpace(slimferenceCmd)
	if cmd == "" {
		cmd = "slimference"
	}
	q := bashSingleQuoted(cmd)
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
MODE="${SLIMFERENCE_CLAUDE_HOOK_MODE:-off}"
case "$MODE" in
  max|compact|aggressive|auto) ;;
  *) exit 0 ;;
esac
INPUT=$(cat)
printf '%%s' "$INPUT" | %s claudeposttool
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
	readScriptPath := filepath.Join(hookDir, "slimference-read-cache.sh")
	if err := os.WriteFile(readScriptPath, []byte(ClaudeReadHookScript(slimferenceCmd)), 0755); err != nil {
		return err
	}
	postToolScriptPath := filepath.Join(hookDir, "slimference-posttool.sh")
	if err := os.WriteFile(postToolScriptPath, []byte(ClaudePostToolHookScript(slimferenceCmd)), 0755); err != nil {
		return err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	return mergeClaudeSettings(settingsPath, scriptPath, readScriptPath, postToolScriptPath)
}

// RemoveClaude removes the hook script and drops PreToolUse from settings when present.
func RemoveClaude(home string) error {
	scriptPath := filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh")
	_ = os.Remove(scriptPath)
	readScriptPath := filepath.Join(home, ".claude", "hooks", "slimference-read-cache.sh")
	_ = os.Remove(readScriptPath)
	postToolScriptPath := filepath.Join(home, ".claude", "hooks", "slimference-posttool.sh")
	_ = os.Remove(postToolScriptPath)
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	return stripClaudeHooks(settingsPath)
}

func mergeClaudeSettings(settingsPath, scriptPath, readScriptPath, postToolScriptPath string) error {
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
	entries, _ := hooksObj["PreToolUse"].([]interface{})
	entries = removeClaudeSlimferenceHooks(entries, scriptPath)
	entries = removeClaudeSlimferenceHooks(entries, readScriptPath)
	entries = append(entries, map[string]interface{}{
		"matcher": "Bash",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "bash " + scriptPath,
			},
		},
	})
	entries = append(entries, map[string]interface{}{
		"matcher": "Read",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "bash " + readScriptPath,
			},
		},
	})
	hooksObj["PreToolUse"] = entries

	postEntries, _ := hooksObj["PostToolUse"].([]interface{})
	postEntries = removeClaudeSlimferenceHooks(postEntries, postToolScriptPath)
	postEntries = append(postEntries, map[string]interface{}{
		"matcher": "Bash",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "bash " + postToolScriptPath,
			},
		},
	})
	hooksObj["PostToolUse"] = postEntries
	root["hooks"] = hooksObj
	out, _ := json.MarshalIndent(root, "", "  ")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, out, 0644)
}

func stripClaudePreToolUse(settingsPath string) error {
	return stripClaudeHooks(settingsPath)
}

func stripClaudeHooks(settingsPath string) error {
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
	entries, _ := hooksObj["PreToolUse"].([]interface{})
	entries = removeClaudeSlimferenceHooks(entries, filepath.Join(filepath.Dir(settingsPath), "hooks", "slimference-rewrite.sh"))
	entries = removeClaudeSlimferenceHooks(entries, filepath.Join(filepath.Dir(settingsPath), "hooks", "slimference-read-cache.sh"))
	if len(entries) == 0 {
		delete(hooksObj, "PreToolUse")
	} else {
		hooksObj["PreToolUse"] = entries
	}
	postEntries, _ := hooksObj["PostToolUse"].([]interface{})
	postEntries = removeClaudeSlimferenceHooks(postEntries, filepath.Join(filepath.Dir(settingsPath), "hooks", "slimference-posttool.sh"))
	if len(postEntries) == 0 {
		delete(hooksObj, "PostToolUse")
	} else {
		hooksObj["PostToolUse"] = postEntries
	}
	if len(hooksObj) == 0 {
		delete(root, "hooks")
	}
	out, _ := json.MarshalIndent(root, "", "  ")
	return os.WriteFile(settingsPath, out, 0644)
}

func removeClaudeSlimferenceHooks(entries []interface{}, scriptPath string) []interface{} {
	out := make([]interface{}, 0, len(entries))
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			out = append(out, entry)
			continue
		}
		hooksSlice, ok := entryMap["hooks"].([]interface{})
		if !ok {
			out = append(out, entry)
			continue
		}
		filteredHooks := make([]interface{}, 0, len(hooksSlice))
		for _, hook := range hooksSlice {
			if isClaudeSlimferenceHook(hook, scriptPath) {
				continue
			}
			filteredHooks = append(filteredHooks, hook)
		}
		if len(filteredHooks) == 0 {
			continue
		}
		cloned := make(map[string]interface{}, len(entryMap))
		for key, value := range entryMap {
			cloned[key] = value
		}
		cloned["hooks"] = filteredHooks
		out = append(out, cloned)
	}
	return out
}

func isClaudeSlimferenceHook(hook interface{}, scriptPath string) bool {
	hookMap, ok := hook.(map[string]interface{})
	if !ok {
		return false
	}
	command, _ := hookMap["command"].(string)
	if command == "" {
		return false
	}
	base := filepath.Base(scriptPath)
	return strings.Contains(command, base) ||
		strings.Contains(command, "slimference-rewrite.sh") ||
		strings.Contains(command, "slimference-read-cache.sh") ||
		strings.Contains(command, "slimference-posttool.sh")
}

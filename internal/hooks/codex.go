package hooks

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var jsonMarshalIndentFn = json.MarshalIndent

const slimferenceCodexHooksLine = "codex_hooks = true  # Slimference: enable lifecycle hooks"
const slimferenceCodexBaseURL = "http://127.0.0.1:8990"

// codexMarkerBegin/End kept for backwards compatibility with old AGENTS.md installs.
const codexMarkerBegin = "<!-- slimference:begin -->"
const codexMarkerEnd = "<!-- slimference:end -->"

// codexPreToolHookScript returns the shell script content for the Codex PreToolUse hook.
// Codex 0.121.0 does not support updatedInput, so when a rewrite would apply we block
// the tool call and instruct the model to rerun the command via slimference filter.
func codexPreToolHookScript(slimferenceCmd string) string {
	cmd := strings.TrimSpace(slimferenceCmd)
	if cmd == "" {
		cmd = "slimference"
	}
	q := bashSingleQuoted(cmd)
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
INPUT=$(cat)
CMD=$(printf '%%s' "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null || true)
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
    jq -nc --arg reason "Rerun this command through the local output filter: ${REWRITTEN}" '{decision:"block",reason:$reason}'
    ;;
  1)
    exit 0
    ;;
  2|3)
    if [[ -z "${ERR_MSG:-}" ]]; then
      ERR_MSG="Local policy blocked this Bash command."
    fi
    jq -nc --arg reason "$ERR_MSG" '{decision:"block",reason:$reason}'
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

// codexPostToolHookScript returns the shell script content for the Codex PostToolUse hook.
// It compacts captured tool output and returns additionalContext when that adds signal.
func codexPostToolHookScript(slimferenceCmd string) string {
	cmd := strings.TrimSpace(slimferenceCmd)
	if cmd == "" {
		cmd = "slimference"
	}
	q := bashSingleQuoted(cmd)
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
INPUT=$(cat)
printf '%%s' "$INPUT" | %s posttool
`, q)
}

func codexReadToolHookScript(slimferenceCmd string) string {
	cmd := strings.TrimSpace(slimferenceCmd)
	if cmd == "" {
		cmd = "slimference"
	}
	q := bashSingleQuoted(cmd)
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
INPUT=$(cat)
printf '%%s' "$INPUT" | %s readhook codex
`, q)
}

func codexLifecycleHookScript(slimferenceCmd string, event string) string {
	cmd := strings.TrimSpace(slimferenceCmd)
	if cmd == "" {
		cmd = "slimference"
	}
	q := bashSingleQuoted(cmd)
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
INPUT=$(cat)
printf '%%s' "$INPUT" | %s codexhook %s
`, q, event)
}

// InstallCodex installs the Slimference hooks for Codex CLI.
//
// Writes:
//  1. ~/.slimference/hooks/codex-pre-tool.sh - PreToolUse Bash hook script
//  2. ~/.slimference/hooks/codex-post-tool.sh - PostToolUse Bash hook script
//  3. ~/.slimference/hooks/codex-read-tool.sh - PreToolUse Read hook script
//  4. ~/.slimference/hooks/codex-session-start.sh - SessionStart hook script
//  5. ~/.slimference/hooks/codex-permission-request.sh - PermissionRequest hook script
//  6. ~/.slimference/hooks/codex-user-prompt-submit.sh - UserPromptSubmit hook script
//  7. ~/.slimference/hooks/codex-stop.sh - Stop hook script
//  8. ~/.codex/hooks.json - lifecycle hook entries
//
// If a hooks.json already exists with Slimference entries, it is not modified.
func InstallCodex(home string, slimferenceCmd string) error {
	if err := validateCodexInstallPreconditions(home); err != nil {
		return err
	}

	// Step 1: Write hook scripts to ~/.slimference/hooks/
	hooksDir := filepath.Join(home, ".slimference", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}
	preScriptPath := CodexPreHookScriptPath(home)
	if err := os.WriteFile(preScriptPath, []byte(codexPreToolHookScript(slimferenceCmd)), 0755); err != nil {
		return fmt.Errorf("write pre-tool hook script: %w", err)
	}
	postScriptPath := CodexHookScriptPath(home)
	if err := os.WriteFile(postScriptPath, []byte(codexPostToolHookScript(slimferenceCmd)), 0755); err != nil {
		return fmt.Errorf("write hook script: %w", err)
	}
	readScriptPath := CodexReadHookScriptPath(home)
	if err := os.WriteFile(readScriptPath, []byte(codexReadToolHookScript(slimferenceCmd)), 0755); err != nil {
		return fmt.Errorf("write read-tool hook script: %w", err)
	}
	sessionScriptPath := CodexSessionStartHookScriptPath(home)
	if err := os.WriteFile(sessionScriptPath, []byte(codexLifecycleHookScript(slimferenceCmd, "session-start")), 0755); err != nil {
		return fmt.Errorf("write session-start hook script: %w", err)
	}
	permissionScriptPath := CodexPermissionHookScriptPath(home)
	if err := os.WriteFile(permissionScriptPath, []byte(codexLifecycleHookScript(slimferenceCmd, "permission-request")), 0755); err != nil {
		return fmt.Errorf("write permission-request hook script: %w", err)
	}
	userPromptScriptPath := CodexUserPromptHookScriptPath(home)
	if err := os.WriteFile(userPromptScriptPath, []byte(codexLifecycleHookScript(slimferenceCmd, "user-prompt-submit")), 0755); err != nil {
		return fmt.Errorf("write user-prompt-submit hook script: %w", err)
	}
	stopScriptPath := CodexStopHookScriptPath(home)
	if err := os.WriteFile(stopScriptPath, []byte(codexLifecycleHookScript(slimferenceCmd, "stop")), 0755); err != nil {
		return fmt.Errorf("write stop hook script: %w", err)
	}

	// Step 2: Write/update ~/.codex/hooks.json
	if err := installCodexHooksJSONWithScripts(home, preScriptPath, postScriptPath, readScriptPath); err != nil {
		return fmt.Errorf("write hooks.json: %w", err)
	}
	if err := ensureCodexHooksFeature(home); err != nil {
		return fmt.Errorf("enable codex hooks feature: %w", err)
	}

	// Step 3: Also keep AGENTS.md block for backwards compat with older Codex versions
	_ = installCodexAgentsMD(home, slimferenceCmd)

	return nil
}

// installCodexHooksJSON writes or merges Slimference hooks into ~/.codex/hooks.json.
// The single scriptPath argument is kept for test compatibility and is treated as the
// PostToolUse path; the PreToolUse path is derived from the home directory.
func installCodexHooksJSON(home string, scriptPath string) error {
	return installCodexHooksJSONWithScripts(home, CodexPreHookScriptPath(home), scriptPath, CodexReadHookScriptPath(home))
}

func installCodexHooksJSONWithScripts(home string, preScriptPath string, postScriptPath string, readScriptPath string) error {
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return err
	}

	hooksPath := filepath.Join(codexDir, "hooks.json")

	existing, err := readExistingCodexHooksJSON(hooksPath)
	if err != nil {
		return err
	}

	existing["PreToolUse"] = mergeCodexHookEntries(existing["PreToolUse"],
		map[string]interface{}{
			"matcher": "Bash|apply_patch|Edit|Write|mcp__.*",
			"hooks": []interface{}{
				map[string]interface{}{
					"type":          "command",
					"command":       fmt.Sprintf("bash %s", preScriptPath),
					"statusMessage": "Local rewrite guard",
				},
			},
		},
		map[string]interface{}{
			"matcher": "Read",
			"hooks": []interface{}{
				map[string]interface{}{
					"type":          "command",
					"command":       fmt.Sprintf("bash %s", readScriptPath),
					"statusMessage": "Local read cache",
				},
			},
		},
	)
	existing["SessionStart"] = mergeCodexHookEntries(existing["SessionStart"], map[string]interface{}{
		"matcher": "startup|resume|clear",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":          "command",
				"command":       fmt.Sprintf("bash %s", CodexSessionStartHookScriptPath(home)),
				"statusMessage": "Local session boundary",
			},
		},
	})
	existing["PermissionRequest"] = mergeCodexHookEntries(existing["PermissionRequest"], map[string]interface{}{
		"matcher": "Bash|apply_patch|Edit|Write|mcp__.*",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":          "command",
				"command":       fmt.Sprintf("bash %s", CodexPermissionHookScriptPath(home)),
				"statusMessage": "Local approval guard",
			},
		},
	})
	existing["PostToolUse"] = mergeCodexHookEntries(existing["PostToolUse"], map[string]interface{}{
		"matcher": "Bash|apply_patch|Edit|Write|mcp__.*",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":          "command",
				"command":       fmt.Sprintf("bash %s", postScriptPath),
				"statusMessage": "Local output filter",
			},
		},
	})
	existing["UserPromptSubmit"] = mergeCodexHookEntries(existing["UserPromptSubmit"], map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": fmt.Sprintf("bash %s", CodexUserPromptHookScriptPath(home)),
			},
		},
	})
	existing["Stop"] = mergeCodexHookEntries(existing["Stop"], map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": fmt.Sprintf("bash %s", CodexStopHookScriptPath(home)),
				"timeout": 30,
			},
		},
	})

	data, err := jsonMarshalIndentFn(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(hooksPath, append(data, '\n'), 0644)
}

func validateCodexInstallPreconditions(home string) error {
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	if _, err := readExistingCodexHooksJSON(hooksPath); err != nil {
		return err
	}
	return nil
}

func ensureCodexHooksFeature(home string) error {
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return err
	}
	configPath := filepath.Join(codexDir, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	state := parseCodexConfigState(content)
	if state.CodexHooks != nil {
		if !*state.CodexHooks {
			return fmt.Errorf("conflicting codex_hooks=false in config.toml")
		}
		return nil
	}
	if strings.Contains(content, "[features]") {
		content = strings.Replace(content, "[features]", "[features]\n"+slimferenceCodexHooksLine, 1)
	} else {
		if strings.TrimSpace(content) != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n[features]\n" + slimferenceCodexHooksLine + "\n"
	}
	return os.WriteFile(configPath, []byte(content), 0644)
}

func removeCodexHooksFeature(home string) error {
	configPath := filepath.Join(home, ".codex", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == slimferenceCodexHooksLine {
			continue
		}
		filtered = append(filtered, line)
	}
	return os.WriteFile(configPath, []byte(strings.Join(filtered, "\n")), 0644)
}

func readExistingCodexHooksJSON(hooksPath string) (map[string]interface{}, error) {
	existing := make(map[string]interface{})
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return existing, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("parse hooks.json: empty file")
	}
	if err := json.Unmarshal(data, &existing); err != nil {
		return nil, fmt.Errorf("parse hooks.json: %w", err)
	}
	return existing, nil
}

func validateCodexConfig(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	state := parseCodexConfigState(string(data))
	if state.HasOpenAIBaseURL && !isSlimferenceCodexBaseURL(state.OpenAIBaseURL) {
		return fmt.Errorf("conflicting openai_base_url in config.toml: %q", state.OpenAIBaseURL)
	}
	if state.CodexHooks != nil && !*state.CodexHooks {
		return fmt.Errorf("conflicting codex_hooks=false in config.toml")
	}
	return nil
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
	if err := validateCodexConfig(configPath); err != nil {
		return err
	}
	state := parseCodexConfigState(content)

	var additions []string

	// Add openai_base_url only if not already present.
	if !state.HasOpenAIBaseURL {
		additions = append(additions, "\n# Slimference proxy endpoint\nopenai_base_url = "+strconv.Quote(slimferenceCodexBaseURL)+"\n")
	}

	// Add codex_hooks feature flag only if not already present.
	if state.CodexHooks == nil {
		if strings.Contains(content, "[features]") {
			// Insert after [features] line
			content = strings.Replace(content, "[features]", "[features]\n"+slimferenceCodexHooksLine, 1)
		} else {
			additions = append(additions, "\n[features]\n"+slimferenceCodexHooksLine+"\n")
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

	// Remove hook script.
	_ = os.Remove(CodexPreHookScriptPath(home))
	_ = os.Remove(CodexHookScriptPath(home))
	_ = os.Remove(CodexReadHookScriptPath(home))
	_ = os.Remove(CodexSessionStartHookScriptPath(home))
	_ = os.Remove(CodexPermissionHookScriptPath(home))
	_ = os.Remove(CodexUserPromptHookScriptPath(home))
	_ = os.Remove(CodexStopHookScriptPath(home))
	_ = removeCodexHooksFeature(home)

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

	removeCodexHookEvent(existing, "PreToolUse")
	removeCodexHookEvent(existing, "SessionStart")
	removeCodexHookEvent(existing, "PermissionRequest")
	removeCodexHookEvent(existing, "PostToolUse")
	removeCodexHookEvent(existing, "UserPromptSubmit")
	removeCodexHookEvent(existing, "Stop")

	out, err := jsonMarshalIndentFn(existing, "", "  ")
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
	result := cleanCodexConfigAfterSlimference(string(data))
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
	hasPre := strings.Contains(content, "codex-pre-tool.sh") || strings.Contains(content, "Slimference rewrite guard") || strings.Contains(content, "Local rewrite guard")
	hasPost := strings.Contains(content, "codex-post-tool.sh") || strings.Contains(content, "Slimference filter") || strings.Contains(content, "Local output filter")
	hasRead := strings.Contains(content, "codex-read-tool.sh") || strings.Contains(content, "Slimference read cache") || strings.Contains(content, "Local read cache")
	hasSession := strings.Contains(content, "codex-session-start.sh") || strings.Contains(content, "Local session boundary")
	hasPermission := strings.Contains(content, "codex-permission-request.sh") || strings.Contains(content, "Local approval guard")
	hasUserPrompt := strings.Contains(content, "codex-user-prompt-submit.sh")
	hasStop := strings.Contains(content, "codex-stop.sh")
	return hasPre && hasPost && hasRead && hasSession && hasPermission && hasUserPrompt && hasStop
}

// CodexPreHookScriptPath returns the path to the installed Codex PreToolUse hook script.
func CodexPreHookScriptPath(home string) string {
	return filepath.Join(home, ".slimference", "hooks", "codex-pre-tool.sh")
}

// CodexHookScriptPath returns the path to the installed Codex hook script.
func CodexHookScriptPath(home string) string {
	return filepath.Join(home, ".slimference", "hooks", "codex-post-tool.sh")
}

func CodexReadHookScriptPath(home string) string {
	return filepath.Join(home, ".slimference", "hooks", "codex-read-tool.sh")
}

func CodexSessionStartHookScriptPath(home string) string {
	return filepath.Join(home, ".slimference", "hooks", "codex-session-start.sh")
}

func CodexPermissionHookScriptPath(home string) string {
	return filepath.Join(home, ".slimference", "hooks", "codex-permission-request.sh")
}

func CodexUserPromptHookScriptPath(home string) string {
	return filepath.Join(home, ".slimference", "hooks", "codex-user-prompt-submit.sh")
}

func CodexStopHookScriptPath(home string) string {
	return filepath.Join(home, ".slimference", "hooks", "codex-stop.sh")
}

func mergeCodexHookEntries(existing interface{}, slimferenceEntries ...map[string]interface{}) []interface{} {
	entries, _ := existing.([]interface{})
	filtered := make([]interface{}, 0, len(entries)+len(slimferenceEntries))
	for _, entry := range entries {
		if codexEntryHasSlimferenceHook(entry) {
			continue
		}
		filtered = append(filtered, entry)
	}
	for _, entry := range slimferenceEntries {
		filtered = append(filtered, entry)
	}
	return filtered
}

func removeCodexHookEvent(existing map[string]interface{}, eventName string) {
	entries, _ := existing[eventName].([]interface{})
	filtered := make([]interface{}, 0, len(entries))
	for _, entry := range entries {
		if codexEntryHasSlimferenceHook(entry) {
			continue
		}
		filtered = append(filtered, entry)
	}
	if len(filtered) == 0 {
		delete(existing, eventName)
		return
	}
	existing[eventName] = filtered
}

func codexEntryHasSlimferenceHook(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	hooksSlice, _ := entryMap["hooks"].([]interface{})
	for _, hook := range hooksSlice {
		hookMap, ok := hook.(map[string]interface{})
		if !ok {
			continue
		}
		command, _ := hookMap["command"].(string)
		statusMessage, _ := hookMap["statusMessage"].(string)
		if strings.Contains(command, "codex-pre-tool.sh") ||
			strings.Contains(command, "codex-post-tool.sh") ||
			strings.Contains(command, "codex-read-tool.sh") ||
			strings.Contains(command, "codex-session-start.sh") ||
			strings.Contains(command, "codex-permission-request.sh") ||
			strings.Contains(command, "codex-user-prompt-submit.sh") ||
			strings.Contains(command, "codex-stop.sh") {
			return true
		}
		if strings.Contains(statusMessage, "Slimference rewrite guard") || strings.Contains(statusMessage, "Slimference filter") || strings.Contains(statusMessage, "Slimference read cache") ||
			strings.Contains(statusMessage, "Local rewrite guard") || strings.Contains(statusMessage, "Local output filter") || strings.Contains(statusMessage, "Local read cache") ||
			strings.Contains(statusMessage, "Local session boundary") || strings.Contains(statusMessage, "Local approval guard") {
			return true
		}
	}
	return false
}

func cleanCodexConfigAfterSlimference(content string) string {
	lines := strings.Split(content, "\n")
	cleaned := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# Slimference") {
			continue
		}
		if strings.HasPrefix(trimmed, "openai_base_url") && strings.Contains(trimmed, "8990") {
			continue
		}
		if trimmed == slimferenceCodexHooksLine {
			continue
		}
		if trimmed == "[features]" {
			next, skip := collectFeaturesSection(lines, i)
			if skip {
				i = next - 1
				continue
			}
		}
		cleaned = append(cleaned, line)
	}
	return strings.Join(cleaned, "\n")
}

func collectFeaturesSection(lines []string, headerIndex int) (nextIndex int, skip bool) {
	nextIndex = len(lines)
	entries := 0
	for i := headerIndex + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			nextIndex = i
			break
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "codex_hooks = true" {
			continue
		}
		entries++
	}
	return nextIndex, entries == 0
}

type codexConfigState struct {
	HasOpenAIBaseURL  bool
	OpenAIBaseURL     string
	HasChatGPTBaseURL bool
	ChatGPTBaseURL    string
	CodexHooks        *bool
}

func parseCodexConfigState(content string) codexConfigState {
	lines := strings.Split(content, "\n")
	state := codexConfigState{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		withoutComment := stripCodexConfigInlineComment(trimmed)
		key, value, ok := strings.Cut(withoutComment, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "openai_base_url":
			state.HasOpenAIBaseURL = true
			if unquoted, err := strconv.Unquote(value); err == nil {
				state.OpenAIBaseURL = unquoted
			}
		case "chatgpt_base_url":
			state.HasChatGPTBaseURL = true
			if unquoted, err := strconv.Unquote(value); err == nil {
				state.ChatGPTBaseURL = unquoted
			}
		case "codex_hooks":
			if parsed, err := strconv.ParseBool(value); err == nil {
				parsedCopy := parsed
				state.CodexHooks = &parsedCopy
			}
		}
	}
	return state
}

func stripCodexConfigInlineComment(s string) string {
	inQuote := false
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		switch r {
		case '\\':
			if inQuote {
				escaped = true
			}
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return strings.TrimSpace(s)
}

func isSlimferenceCodexBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" {
		return false
	}
	if u.Port() != "8990" {
		return false
	}
	switch strings.TrimRight(u.EscapedPath(), "/") {
	case "", "/v1":
		return true
	default:
		return false
	}
}

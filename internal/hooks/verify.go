package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CodexHookState describes every Slimference-owned Codex hook artifact.
type CodexHookState struct {
	HooksJSONExists bool
	PreEntry        bool
	PostEntry       bool
	ReadEntry       bool
	PreScript       bool
	PostScript      bool
	ReadScript      bool
	PreExecutable   bool
	PostExecutable  bool
	ReadExecutable  bool
}

// InstalledStatus returns whether the Claude hook script and Codex hooks are present.
func InstalledStatus(home string) (claude, codex bool) {
	claude = claudeHookInstalled(home)
	codex = codexStatusInstalled(home)
	return claude, codex
}

func claudeHookInstalled(home string) bool {
	claudeScript := filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh")
	if _, err := os.Stat(claudeScript); err != nil {
		return false
	}
	readScript := filepath.Join(home, ".claude", "hooks", "slimference-read-cache.sh")
	if _, err := os.Stat(readScript); err != nil {
		return false
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return os.IsNotExist(err)
	}
	text := string(data)
	return strings.Contains(text, "slimference-rewrite.sh") && strings.Contains(text, "slimference-read-cache.sh")
}

func codexStatusInstalled(home string) bool {
	if codexCoherentInstall(home) {
		return true
	}
	agents := filepath.Join(home, ".codex", "AGENTS.md")
	data, err := os.ReadFile(agents)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), codexMarkerBegin)
}

func codexCoherentInstall(home string) bool {
	hookState := InspectCodexHooks(home)
	if !hookState.Complete() {
		return false
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	configState := parseCodexConfigState(string(configData))
	return codexConfigOperational(configState)
}

// InspectCodexHooks checks hooks.json plus the three expected executable
// script files. It does not inspect config.toml; config ownership lives in
// internal/integrate.
func InspectCodexHooks(home string) CodexHookState {
	state := CodexHookState{}
	codexHooksPath := filepath.Join(home, ".codex", "hooks.json")
	if data, err := os.ReadFile(codexHooksPath); err == nil {
		text := string(data)
		state.HooksJSONExists = true
		state.PreEntry = strings.Contains(text, "codex-pre-tool.sh") ||
			strings.Contains(text, "Slimference rewrite guard")
		state.PostEntry = strings.Contains(text, "codex-post-tool.sh") ||
			strings.Contains(text, "Slimference filter")
		state.ReadEntry = strings.Contains(text, "codex-read-tool.sh") ||
			strings.Contains(text, "Slimference read cache")
	}
	state.PreScript, state.PreExecutable = executableFileExists(CodexPreHookScriptPath(home))
	state.PostScript, state.PostExecutable = executableFileExists(CodexHookScriptPath(home))
	state.ReadScript, state.ReadExecutable = executableFileExists(CodexReadHookScriptPath(home))
	return state
}

// Complete reports whether every expected Codex hook entry and script exists.
func (s CodexHookState) Complete() bool {
	return s.HooksJSONExists &&
		s.PreEntry && s.PostEntry && s.ReadEntry &&
		s.PreScript && s.PostScript && s.ReadScript &&
		s.PreExecutable && s.PostExecutable && s.ReadExecutable
}

func executableFileExists(path string) (exists bool, executable bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false, false
	}
	return true, info.Mode().Perm()&0o111 != 0
}

// VerifyReport lists hook files and SHA-256 hashes. ok is false when a hook installation
// is missing or internally inconsistent for any target that appears to be installed.
func VerifyReport(home string) (lines []string, ok bool) {
	ok = true
	claudeScript := filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh")
	if b, err := os.ReadFile(claudeScript); err == nil {
		sum := sha256.Sum256(b)
		lines = append(lines, fmt.Sprintf("claude  %s  sha256=%s", claudeScript, hex.EncodeToString(sum[:])))
	} else {
		lines = append(lines, fmt.Sprintf("claude  %s  MISSING", claudeScript))
		ok = false
	}
	readScript := filepath.Join(home, ".claude", "hooks", "slimference-read-cache.sh")
	if b, err := os.ReadFile(readScript); err == nil {
		sum := sha256.Sum256(b)
		lines = append(lines, fmt.Sprintf("claude  %s  sha256=%s", readScript, hex.EncodeToString(sum[:])))
	} else {
		lines = append(lines, fmt.Sprintf("claude  %s  MISSING", readScript))
		ok = false
	}

	codexLines, codexOK := VerifyCodexReport(home)
	lines = append(lines, codexLines...)
	codexState := InspectCodexHooks(home)
	if codexState.PreEntry || codexState.PostEntry || codexState.ReadEntry {
		ok = ok && codexOK
	}
	return lines, ok
}

// VerifyCodexReport lists only Codex hook artifacts and config status.
func VerifyCodexReport(home string) (lines []string, ok bool) {
	ok = true
	codexHooksPath := filepath.Join(home, ".codex", "hooks.json")
	if _, err := os.ReadFile(codexHooksPath); err == nil {
		state := InspectCodexHooks(home)
		hasPre := state.PreEntry
		hasPost := state.PostEntry
		hasRead := state.ReadEntry
		if hasPre || hasPost || hasRead {
			prePath := CodexPreHookScriptPath(home)
			postPath := CodexHookScriptPath(home)
			readPath := CodexReadHookScriptPath(home)
			configPath := filepath.Join(home, ".codex", "config.toml")

			if sb, serr := os.ReadFile(prePath); serr == nil && state.PreExecutable {
				sum := sha256.Sum256(sb)
				lines = append(lines, fmt.Sprintf("codex   %s  sha256=%s", prePath, hex.EncodeToString(sum[:])))
			} else if state.PreScript {
				lines = append(lines, fmt.Sprintf("codex   %s  script NOT_EXECUTABLE", prePath))
				ok = false
			} else {
				lines = append(lines, fmt.Sprintf("codex   %s  hooks.json OK, script MISSING", prePath))
				ok = false
			}
			if sb, serr := os.ReadFile(postPath); serr == nil && state.PostExecutable {
				sum := sha256.Sum256(sb)
				lines = append(lines, fmt.Sprintf("codex   %s  sha256=%s", postPath, hex.EncodeToString(sum[:])))
			} else if state.PostScript {
				lines = append(lines, fmt.Sprintf("codex   %s  script NOT_EXECUTABLE", postPath))
				ok = false
			} else {
				lines = append(lines, fmt.Sprintf("codex   %s  hooks.json OK, script MISSING", postPath))
				ok = false
			}
			if sb, serr := os.ReadFile(readPath); serr == nil && state.ReadExecutable {
				sum := sha256.Sum256(sb)
				lines = append(lines, fmt.Sprintf("codex   %s  sha256=%s", readPath, hex.EncodeToString(sum[:])))
			} else if state.ReadScript {
				lines = append(lines, fmt.Sprintf("codex   %s  script NOT_EXECUTABLE", readPath))
				ok = false
			} else {
				lines = append(lines, fmt.Sprintf("codex   %s  hooks.json OK, script MISSING", readPath))
				ok = false
			}
			if configData, cerr := os.ReadFile(configPath); cerr == nil {
				state := parseCodexConfigState(string(configData))
				switch codexConfigStatus(state) {
				case "ok":
					lines = append(lines, fmt.Sprintf("codex   %s  config OK", configPath))
				case "conflict":
					lines = append(lines, fmt.Sprintf("codex   %s  config conflict", configPath))
					ok = false
				default:
					lines = append(lines, fmt.Sprintf("codex   %s  config incomplete", configPath))
					ok = false
				}
			} else {
				lines = append(lines, fmt.Sprintf("codex   %s  config MISSING", configPath))
				ok = false
			}
			if !hasPre || !hasPost || !hasRead {
				ok = false
			}
			return lines, ok
		}
		lines = append(lines, fmt.Sprintf("codex   %s  file exists (no slimference hook)", codexHooksPath))
		return lines, false
	}

	agents := filepath.Join(home, ".codex", "AGENTS.md")
	if data, err := os.ReadFile(agents); err == nil && strings.Contains(string(data), codexMarkerBegin) {
		lines = append(lines, fmt.Sprintf("codex   %s  legacy instruction block (upgrade: hook install codex)", agents))
		return lines, false
	}
	lines = append(lines, fmt.Sprintf("codex   %s  not installed", codexHooksPath))
	return lines, false
}

func codexConfigOperational(state codexConfigState) bool {
	return codexConfigStatus(state) == "ok"
}

func codexConfigStatus(state codexConfigState) string {
	if (state.HasOpenAIBaseURL && !isSlimferenceCodexBaseURL(state.OpenAIBaseURL)) ||
		(state.HasChatGPTBaseURL && !isSlimferenceCodexBaseURL(state.ChatGPTBaseURL)) ||
		(state.CodexHooks != nil && !*state.CodexHooks) {
		return "conflict"
	}
	if !state.HasOpenAIBaseURL || !state.HasChatGPTBaseURL {
		return "incomplete"
	}
	return "ok"
}

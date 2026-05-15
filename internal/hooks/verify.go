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
	HooksJSONExists      bool
	PreEntry             bool
	PostEntry            bool
	ReadEntry            bool
	SessionEntry         bool
	PermissionEntry      bool
	UserPromptEntry      bool
	StopEntry            bool
	PreScript            bool
	PostScript           bool
	ReadScript           bool
	SessionScript        bool
	PermissionScript     bool
	UserPromptScript     bool
	StopScript           bool
	PreExecutable        bool
	PostExecutable       bool
	ReadExecutable       bool
	SessionExecutable    bool
	PermissionExecutable bool
	UserPromptExecutable bool
	StopExecutable       bool
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
	if InspectCodexHooks(home).Complete() {
		return true
	}
	agents := filepath.Join(home, ".codex", "AGENTS.md")
	data, err := os.ReadFile(agents)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), codexMarkerBegin)
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
			strings.Contains(text, "Slimference rewrite guard") ||
			strings.Contains(text, "Local rewrite guard") ||
			strings.Contains(text, "Output guard")
		state.PostEntry = strings.Contains(text, "codex-post-tool.sh") ||
			strings.Contains(text, "Slimference filter") ||
			strings.Contains(text, "Local output filter") ||
			strings.Contains(text, "Output compactor")
		state.ReadEntry = strings.Contains(text, "codex-read-tool.sh") ||
			strings.Contains(text, "Slimference read cache") ||
			strings.Contains(text, "Local read cache") ||
			strings.Contains(text, "Read cache")
		state.SessionEntry = strings.Contains(text, "codex-session-start.sh") ||
			strings.Contains(text, "Local session boundary") ||
			strings.Contains(text, "Session boundary")
		state.PermissionEntry = strings.Contains(text, "codex-permission-request.sh") ||
			strings.Contains(text, "Local approval guard") ||
			strings.Contains(text, "Approval guard")
		state.UserPromptEntry = strings.Contains(text, "codex-user-prompt-submit.sh")
		state.StopEntry = strings.Contains(text, "codex-stop.sh")
	}
	state.PreScript, state.PreExecutable = executableFileExists(CodexPreHookScriptPath(home))
	state.PostScript, state.PostExecutable = executableFileExists(CodexHookScriptPath(home))
	state.ReadScript, state.ReadExecutable = executableFileExists(CodexReadHookScriptPath(home))
	state.SessionScript, state.SessionExecutable = executableFileExists(CodexSessionStartHookScriptPath(home))
	state.PermissionScript, state.PermissionExecutable = executableFileExists(CodexPermissionHookScriptPath(home))
	state.UserPromptScript, state.UserPromptExecutable = executableFileExists(CodexUserPromptHookScriptPath(home))
	state.StopScript, state.StopExecutable = executableFileExists(CodexStopHookScriptPath(home))
	return state
}

// Complete reports whether every expected Codex hook entry and script exists.
func (s CodexHookState) Complete() bool {
	return s.HooksJSONExists &&
		s.PreEntry && s.PostEntry && s.ReadEntry &&
		s.SessionEntry && s.PermissionEntry && s.UserPromptEntry && s.StopEntry &&
		s.PreScript && s.PostScript && s.ReadScript &&
		s.SessionScript && s.PermissionScript && s.UserPromptScript && s.StopScript &&
		s.PreExecutable && s.PostExecutable && s.ReadExecutable &&
		s.SessionExecutable && s.PermissionExecutable && s.UserPromptExecutable && s.StopExecutable
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

// VerifyCodexReport lists only Codex hook artifacts. Codex config-patch status
// belongs to internal/integrate and is reported by `slimference integrate status`.
func VerifyCodexReport(home string) (lines []string, ok bool) {
	ok = true
	codexHooksPath := filepath.Join(home, ".codex", "hooks.json")
	if _, err := os.ReadFile(codexHooksPath); err == nil {
		state := InspectCodexHooks(home)
		if codexStateHasAnyEntry(state) {
			lines, ok = appendCodexArtifactLine(lines, ok, CodexPreHookScriptPath(home), state.PreScript, state.PreExecutable, state.PreEntry)
			lines, ok = appendCodexArtifactLine(lines, ok, CodexHookScriptPath(home), state.PostScript, state.PostExecutable, state.PostEntry)
			lines, ok = appendCodexArtifactLine(lines, ok, CodexReadHookScriptPath(home), state.ReadScript, state.ReadExecutable, state.ReadEntry)
			lines, ok = appendCodexArtifactLine(lines, ok, CodexSessionStartHookScriptPath(home), state.SessionScript, state.SessionExecutable, state.SessionEntry)
			lines, ok = appendCodexArtifactLine(lines, ok, CodexPermissionHookScriptPath(home), state.PermissionScript, state.PermissionExecutable, state.PermissionEntry)
			lines, ok = appendCodexArtifactLine(lines, ok, CodexUserPromptHookScriptPath(home), state.UserPromptScript, state.UserPromptExecutable, state.UserPromptEntry)
			lines, ok = appendCodexArtifactLine(lines, ok, CodexStopHookScriptPath(home), state.StopScript, state.StopExecutable, state.StopEntry)
			if !state.Complete() {
				ok = false
			}
			lines = append(lines, "codex   review note: if Codex reports hooks need review, open /hooks and approve the installed hook entries")
			lines = append(lines, "codex   config-patch status not checked here (use `slimference integrate status --client codex`)")
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

func codexStateHasAnyEntry(state CodexHookState) bool {
	return state.PreEntry || state.PostEntry || state.ReadEntry ||
		state.SessionEntry || state.PermissionEntry || state.UserPromptEntry || state.StopEntry
}

func appendCodexArtifactLine(lines []string, ok bool, path string, exists bool, executable bool, entry bool) ([]string, bool) {
	if !entry {
		lines = append(lines, fmt.Sprintf("codex   %s  hooks.json entry MISSING", path))
		return lines, false
	}
	if sb, serr := os.ReadFile(path); serr == nil && executable {
		sum := sha256.Sum256(sb)
		lines = append(lines, fmt.Sprintf("codex   %s  sha256=%s", path, hex.EncodeToString(sum[:])))
		return lines, ok
	}
	if exists {
		lines = append(lines, fmt.Sprintf("codex   %s  script NOT_EXECUTABLE", path))
		return lines, false
	}
	lines = append(lines, fmt.Sprintf("codex   %s  hooks.json OK, script MISSING", path))
	return lines, false
}

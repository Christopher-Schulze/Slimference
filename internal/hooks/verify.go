package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstalledStatus returns whether the Claude hook script and Codex hooks are present.
func InstalledStatus(home string) (claude, codex bool) {
	claudeScript := filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh")
	if _, err := os.Stat(claudeScript); err == nil {
		claude = true
	}
	codex = CodexHookInstalled(home)
	return claude, codex
}

// VerifyReport lists hook files and SHA-256 hashes. ok is false only if the Claude hook script is missing.
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

	// Codex: check hooks.json for Slimference entry.
	codexHooksPath := filepath.Join(home, ".codex", "hooks.json")
	if data, err := os.ReadFile(codexHooksPath); err == nil {
		if strings.Contains(string(data), "slimference") {
			// Also verify the hook script exists.
			scriptPath := CodexHookScriptPath(home)
			if sb, serr := os.ReadFile(scriptPath); serr == nil {
				sum := sha256.Sum256(sb)
				lines = append(lines, fmt.Sprintf("codex   %s  sha256=%s", scriptPath, hex.EncodeToString(sum[:])))
			} else {
				lines = append(lines, fmt.Sprintf("codex   %s  hooks.json OK, script MISSING", codexHooksPath))
			}
		} else {
			lines = append(lines, fmt.Sprintf("codex   %s  file exists (no slimference hook)", codexHooksPath))
		}
	} else {
		// Fallback: check legacy AGENTS.md marker.
		agents := filepath.Join(home, ".codex", "AGENTS.md")
		if data, err := os.ReadFile(agents); err == nil {
			if strings.Contains(string(data), codexMarkerBegin) {
				lines = append(lines, fmt.Sprintf("codex   %s  legacy instruction block (upgrade: hook install codex)", agents))
			} else {
				lines = append(lines, fmt.Sprintf("codex   %s  not installed", codexHooksPath))
			}
		} else {
			lines = append(lines, fmt.Sprintf("codex   %s  not installed", codexHooksPath))
		}
	}
	return lines, ok
}

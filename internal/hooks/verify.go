package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstalledStatus returns whether the Claude hook script and Codex AGENTS.md block are present.
func InstalledStatus(home string) (claude, codex bool) {
	claudeScript := filepath.Join(home, ".claude", "hooks", "tokenproxy-rewrite.sh")
	if _, err := os.Stat(claudeScript); err == nil {
		claude = true
	}
	agents := filepath.Join(home, ".codex", "AGENTS.md")
	if data, err := os.ReadFile(agents); err == nil {
		codex = strings.Contains(string(data), codexMarkerBegin)
	}
	return claude, codex
}

// VerifyReport lists hook files and SHA-256 hashes. ok is false only if the Claude hook script is missing.
func VerifyReport(home string) (lines []string, ok bool) {
	ok = true
	claudeScript := filepath.Join(home, ".claude", "hooks", "tokenproxy-rewrite.sh")
	if b, err := os.ReadFile(claudeScript); err == nil {
		sum := sha256.Sum256(b)
		lines = append(lines, fmt.Sprintf("claude  %s  sha256=%s", claudeScript, hex.EncodeToString(sum[:])))
	} else {
		lines = append(lines, fmt.Sprintf("claude  %s  MISSING", claudeScript))
		ok = false
	}
	agents := filepath.Join(home, ".codex", "AGENTS.md")
	if data, err := os.ReadFile(agents); err == nil {
		if strings.Contains(string(data), codexMarkerBegin) {
			lines = append(lines, fmt.Sprintf("codex   %s  instruction block present", agents))
		} else {
			lines = append(lines, fmt.Sprintf("codex   %s  file exists (no tokenproxy block)", agents))
		}
	} else {
		lines = append(lines, fmt.Sprintf("codex   %s  not installed (optional)", agents))
	}
	return lines, ok
}

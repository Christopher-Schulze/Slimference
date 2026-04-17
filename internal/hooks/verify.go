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

	// Codex: check hooks.json, scripts, and config for a coherent install.
	codexHooksPath := filepath.Join(home, ".codex", "hooks.json")
	if data, err := os.ReadFile(codexHooksPath); err == nil {
		hasPre := strings.Contains(string(data), "codex-pre-tool.sh")
		hasPost := strings.Contains(string(data), "codex-post-tool.sh")
		if hasPre || hasPost {
			prePath := CodexPreHookScriptPath(home)
			postPath := CodexHookScriptPath(home)
			configPath := filepath.Join(home, ".codex", "config.toml")

			if sb, serr := os.ReadFile(prePath); serr == nil {
				sum := sha256.Sum256(sb)
				lines = append(lines, fmt.Sprintf("codex   %s  sha256=%s", prePath, hex.EncodeToString(sum[:])))
			} else {
				lines = append(lines, fmt.Sprintf("codex   %s  hooks.json OK, script MISSING", prePath))
				ok = false
			}
			if sb, serr := os.ReadFile(postPath); serr == nil {
				sum := sha256.Sum256(sb)
				lines = append(lines, fmt.Sprintf("codex   %s  sha256=%s", postPath, hex.EncodeToString(sum[:])))
			} else {
				lines = append(lines, fmt.Sprintf("codex   %s  hooks.json OK, script MISSING", postPath))
				ok = false
			}
			if configData, cerr := os.ReadFile(configPath); cerr == nil {
				content := string(configData)
				hasBaseURL := strings.Contains(content, "openai_base_url")
				hasHooksFlag := strings.Contains(content, "codex_hooks = true")
				if hasBaseURL && hasHooksFlag {
					lines = append(lines, fmt.Sprintf("codex   %s  config OK", configPath))
				} else {
					lines = append(lines, fmt.Sprintf("codex   %s  config incomplete", configPath))
					ok = false
				}
			} else {
				lines = append(lines, fmt.Sprintf("codex   %s  config MISSING", configPath))
				ok = false
			}
			if !hasPre || !hasPost {
				ok = false
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

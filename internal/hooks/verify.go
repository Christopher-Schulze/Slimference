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
	if !CodexHookInstalled(home) {
		return false
	}
	if _, err := os.Stat(CodexPreHookScriptPath(home)); err != nil {
		return false
	}
	if _, err := os.Stat(CodexHookScriptPath(home)); err != nil {
		return false
	}
	if _, err := os.Stat(CodexReadHookScriptPath(home)); err != nil {
		return false
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	state := parseCodexConfigState(string(configData))
	return codexConfigOperational(state)
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

	// Codex: check hooks.json, scripts, and config for a coherent install.
	codexHooksPath := filepath.Join(home, ".codex", "hooks.json")
	if data, err := os.ReadFile(codexHooksPath); err == nil {
		hasPre := strings.Contains(string(data), "codex-pre-tool.sh")
		hasPost := strings.Contains(string(data), "codex-post-tool.sh")
		hasRead := strings.Contains(string(data), "codex-read-tool.sh")
		if hasPre || hasPost || hasRead {
			prePath := CodexPreHookScriptPath(home)
			postPath := CodexHookScriptPath(home)
			readPath := CodexReadHookScriptPath(home)
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
			if sb, serr := os.ReadFile(readPath); serr == nil {
				sum := sha256.Sum256(sb)
				lines = append(lines, fmt.Sprintf("codex   %s  sha256=%s", readPath, hex.EncodeToString(sum[:])))
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

func codexConfigOperational(state codexConfigState) bool {
	return codexConfigStatus(state) == "ok"
}

func codexConfigStatus(state codexConfigState) string {
	if !state.HasOpenAIBaseURL || state.CodexHooks == nil {
		return "incomplete"
	}
	if !isSlimferenceCodexBaseURL(state.OpenAIBaseURL) || !*state.CodexHooks {
		return "conflict"
	}
	return "ok"
}

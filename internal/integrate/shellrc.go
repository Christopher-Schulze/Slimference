package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ShellFlavor identifies the shell syntax to emit into an rc file.
type ShellFlavor int

const (
	ShellUnknown ShellFlavor = iota
	ShellZsh
	ShellBash
	ShellFish
)

func (f ShellFlavor) String() string {
	switch f {
	case ShellZsh:
		return "zsh"
	case ShellBash:
		return "bash"
	case ShellFish:
		return "fish"
	}
	return "unknown"
}

// RCFile describes an rc file target picked by the detector.
type RCFile struct {
	Path    string
	Flavor  ShellFlavor
	Exists  bool
}

// DetectRCFile picks the best-match rc file for the current user. Priority:
// 1. `$SHELL` matches → prefer its canonical rc.
// 2. Files that already exist on disk.
// 3. Fall back to `~/.zshrc` because macOS defaults to zsh since Catalina.
//
// home must be the user's home directory (caller resolves this from
// Options.HomeDir or os.UserHomeDir).
func DetectRCFile(home, shellEnv string) RCFile {
	candidates := []RCFile{
		{Path: filepath.Join(home, ".zshrc"), Flavor: ShellZsh},
		{Path: filepath.Join(home, ".bashrc"), Flavor: ShellBash},
		{Path: filepath.Join(home, ".bash_profile"), Flavor: ShellBash},
		{Path: filepath.Join(home, ".config", "fish", "config.fish"), Flavor: ShellFish},
	}
	for i := range candidates {
		if _, err := os.Stat(candidates[i].Path); err == nil {
			candidates[i].Exists = true
		}
	}

	preferred := shellFromEnv(shellEnv)
	// First pass: shell-matched + exists.
	for _, c := range candidates {
		if c.Flavor == preferred && c.Exists {
			return c
		}
	}
	// Second pass: any rc that exists.
	for _, c := range candidates {
		if c.Exists {
			return c
		}
	}
	// Third: shell-matched even if missing (we will create it).
	for _, c := range candidates {
		if c.Flavor == preferred {
			return c
		}
	}
	// Last resort: zsh.
	return candidates[0]
}

// shellFromEnv inspects $SHELL. Empty or unrecognised returns ShellUnknown.
func shellFromEnv(shellEnv string) ShellFlavor {
	base := filepath.Base(shellEnv)
	switch base {
	case "zsh":
		return ShellZsh
	case "bash", "sh":
		return ShellBash
	case "fish":
		return ShellFish
	}
	return ShellUnknown
}

// claudeEnvBlockBody returns the shell-specific body that exports
// ANTHROPIC_BASE_URL. Each flavor has its own export syntax.
func claudeEnvBlockBody(flavor ShellFlavor, proxyURL string) string {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		proxyURL = ProxyURL
	}
	switch flavor {
	case ShellFish:
		return fmt.Sprintf("set -gx ANTHROPIC_BASE_URL %s", proxyURL)
	default:
		return fmt.Sprintf("export ANTHROPIC_BASE_URL=%s", proxyURL)
	}
}

// ReadRC reads an rc file, returning the content and whether it existed.
// Non-existent files return empty content + exists=false + nil error.
func ReadRC(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(data), true, nil
}

// WriteRCBlock upserts the Slimference block in the rc file. Always creates
// the directory chain if missing (relevant for fish at ~/.config/fish/).
func WriteRCBlock(path string, flavor ShellFlavor, proxyURL string) (WriteEvent, error) {
	content, exists, err := ReadRC(path)
	if err != nil {
		return WriteEvent{}, err
	}
	body := claudeEnvBlockBody(flavor, proxyURL)

	// Check idempotency: already wired with exact body?
	before, existingBlock, _, hasBlock := splitBlock(content)
	_ = before
	if hasBlock && strings.Contains(existingBlock, body) {
		return WriteEvent{Path: path, Action: "skipped_idempotent"}, nil
	}

	if exists {
		if _, err := backupOnce(path); err != nil {
			return WriteEvent{}, err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return WriteEvent{}, err
		}
	}

	newContent := replaceOrAppendBlock(content, body)
	if err := writeAtomic(path, []byte(newContent), 0o644); err != nil {
		return WriteEvent{}, err
	}
	return WriteEvent{Path: path, Action: "wrote_block"}, nil
}

// RemoveRCBlock strips the Slimference block if present. Idempotent.
func RemoveRCBlock(path string) (WriteEvent, error) {
	content, exists, err := ReadRC(path)
	if err != nil {
		return WriteEvent{}, err
	}
	if !exists {
		return WriteEvent{Path: path, Action: "skipped_idempotent"}, nil
	}
	_, _, _, hasBlock := splitBlock(content)
	if !hasBlock {
		return WriteEvent{Path: path, Action: "skipped_idempotent"}, nil
	}
	if _, err := backupOnce(path); err != nil {
		return WriteEvent{}, err
	}
	newContent := replaceOrAppendBlock(content, "")
	if err := writeAtomic(path, []byte(newContent), 0o644); err != nil {
		return WriteEvent{}, err
	}
	return WriteEvent{Path: path, Action: "removed_block"}, nil
}

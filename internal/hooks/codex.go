package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const codexMarkerBegin = "<!-- slimference:begin -->"
const codexMarkerEnd = "<!-- slimference:end -->"

// CodexAgentsBlock returns the markdown block for AGENTS.md (cmd is usually "slimference" or a full path).
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

// InstallCodex appends a Slimference block to ~/.codex/AGENTS.md (or creates it).
func InstallCodex(home string, slimferenceCmd string) error {
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

// RemoveCodex removes the Slimference block from ~/.codex/AGENTS.md if present.
func RemoveCodex(home string) error {
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

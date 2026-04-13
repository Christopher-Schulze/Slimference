package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const codexMarkerBegin = "<!-- tokenproxy:begin -->"
const codexMarkerEnd = "<!-- tokenproxy:end -->"

// CodexAgentsBlock returns the markdown block for AGENTS.md (cmd is usually "tokenproxy" or a full path).
func CodexAgentsBlock(tokenproxyCmd string) string {
	cmd := strings.TrimSpace(tokenproxyCmd)
	if cmd == "" {
		cmd = "tokenproxy"
	}
	return `

` + codexMarkerBegin + `
## TokenProxy (shell output)

When running shell commands, prefer wrapping them with:

` + fmt.Sprintf("`%s filter`", cmd) + ` so that command output is compacted before it enters the context.

Example: ` + fmt.Sprintf("`%s filter git status`", cmd) + ` instead of ` + "`git status`" + `.

` + codexMarkerEnd + `
`
}

// InstallCodex appends a TokenProxy block to ~/.codex/AGENTS.md (or creates it).
func InstallCodex(home string, tokenproxyCmd string) error {
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
	block := CodexAgentsBlock(tokenproxyCmd)
	_, err = f.WriteString(strings.TrimPrefix(block, "\n"))
	return err
}

// RemoveCodex removes the TokenProxy block from ~/.codex/AGENTS.md if present.
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
		return fmt.Errorf("tokenproxy: unclosed marker in AGENTS.md")
	}
	j += i + len(codexMarkerEnd)
	out := strings.TrimSpace(s[:i] + s[j:])
	return os.WriteFile(p, []byte(out+"\n"), 0644)
}

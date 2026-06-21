package main

import (
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	scopedCodexCLIActiveCountFn     = scopedCodexCLIActiveCount
	codexDesktopAppServerCountFn    = codexDesktopAppServerCount
	codexDesktopProcessArgsOutputFn = func() ([]byte, error) { return exec.Command("ps", "-axo", "args=").Output() }
)

func scopedCodexCLIActiveCount() int {
	out, err := codexDesktopProcessArgsOutputFn()
	if err != nil {
		return 0
	}
	return countScopedCodexCLILines(string(out))
}

func codexDesktopAppServerCount() int {
	out, err := codexDesktopProcessArgsOutputFn()
	if err != nil {
		return 0
	}
	return countCodexDesktopAppServerLines(string(out))
}

func countScopedCodexCLILines(text string) int {
	count := 0
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 {
			continue
		}
		if filepath.Base(fields[0]) == "slimference" && fields[1] == "codex" && fields[2] == "run" {
			count++
		}
	}
	return count
}

func countCodexDesktopAppServerLines(text string) int {
	count := 0
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		if filepath.Base(fields[0]) == "slimference" && fields[1] == "app-server" {
			count++
		}
	}
	return count
}

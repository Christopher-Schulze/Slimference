package main

import (
	"fmt"
	"os"
)

const (
	codexTerminalTitleActive = "[SF] Codex CLI"
	codexTerminalTitleReset  = "Codex CLI"
)

var terminalTitleWriteFn = func(title string) {
	if !termIsTerminalFn(int(os.Stderr.Fd())) {
		return
	}
	fmt.Fprintf(os.Stderr, "\033]0;%s\007", title)
}

func setScopedCodexTerminalTitle() func() {
	terminalTitleWriteFn(codexTerminalTitleActive)
	return func() {
		terminalTitleWriteFn(codexTerminalTitleReset)
	}
}

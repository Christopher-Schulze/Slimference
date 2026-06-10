package main

import "testing"

func TestCountScopedCodexCLILines(t *testing.T) {
	text := `
/Users/me/.local/bin/slimference codex run --transport=auto --
/Users/me/.local/bin/slimference daemon
/usr/local/bin/codex run --transport=auto --
/Users/me/.local/bin/slimference codex status
/Users/me/.local/bin/slimference codex run -- foo
`
	if got := countScopedCodexCLILines(text); got != 2 {
		t.Fatalf("countScopedCodexCLILines=%d want 2", got)
	}
}

func TestCountCodexDesktopAppServerLines(t *testing.T) {
	text := `
/Users/me/.local/bin/slimference app-server -c model_provider=slimference-codex
/Users/me/.local/bin/slimference codex run --transport=auto --
/Applications/Codex.app/Contents/MacOS/Codex
/Users/me/.local/bin/slimference app-server
`
	if got := countCodexDesktopAppServerLines(text); got != 2 {
		t.Fatalf("countCodexDesktopAppServerLines=%d want 2", got)
	}
}

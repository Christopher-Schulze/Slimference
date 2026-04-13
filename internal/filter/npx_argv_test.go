package filter

import "testing"

func TestNpxCommandIndex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv []string
		want int
	}{
		{[]string{"npx", "eslint", "."}, 1},
		{[]string{"npx", "-y", "terraform", "fmt"}, 2},
		{[]string{"npx", "--yes", "deno", "lint"}, 2},
		{[]string{"npx", "-p", "pkg", "cmd"}, 3},
		{[]string{"npx", "--", "weird", "x"}, 2},
	}
	for _, tc := range cases {
		if got := npxCommandIndex(tc.argv); got != tc.want {
			t.Errorf("npxCommandIndex(%q) = %d, want %d", tc.argv, got, tc.want)
		}
	}
}

func TestNpxCommandIndex_edgeCases(t *testing.T) {
	t.Parallel()
	// "--" at very end — no command follows, returns len(argv).
	if got := npxCommandIndex([]string{"npx", "--"}); got != 2 {
		t.Errorf("npx --: got %d, want 2", got)
	}
	// Unknown flag like -x: skipped, then eslint is the command.
	if got := npxCommandIndex([]string{"npx", "-x", "eslint"}); got != 2 {
		t.Errorf("npx -x eslint: got %d, want 2", got)
	}
	// --package with value followed by command.
	if got := npxCommandIndex([]string{"npx", "--package", "pkg", "cmd"}); got != 3 {
		t.Errorf("npx --package pkg cmd: got %d, want 3", got)
	}
	// --call with value (no command follows, exhausted).
	if got := npxCommandIndex([]string{"npx", "--call", "sh"}); got != 3 {
		t.Errorf("npx --call sh: got %d, want 3", got)
	}
	// non-npx binary: returns len(argv) immediately.
	if got := npxCommandIndex([]string{"yarn", "eslint"}); got != 2 {
		t.Errorf("yarn not npx: got %d, want 2", got)
	}
	// too short.
	if got := npxCommandIndex([]string{"npx"}); got != 1 {
		t.Errorf("npx only: got %d, want 1", got)
	}
}

func TestNpxArgvSuffix_edgeCases(t *testing.T) {
	t.Parallel()
	// empty argv.
	if rest, ok := npxArgvSuffix([]string{}); rest != nil || ok {
		t.Errorf("empty argv: rest=%v ok=%v", rest, ok)
	}
	// npx with no command after flags (exhausted).
	if rest, ok := npxArgvSuffix([]string{"npx", "-y"}); rest != nil || !ok {
		t.Errorf("npx -y exhausted: rest=%v ok=%v", rest, ok)
	}
	// non-npx binary.
	if rest, ok := npxArgvSuffix([]string{"yarn", "eslint"}); rest != nil || ok {
		t.Errorf("yarn not npx: rest=%v ok=%v", rest, ok)
	}
}

func TestNpxMatches(t *testing.T) {
	t.Parallel()
	if !npxMatches([]string{"npx", "-y", "buf", "lint"}, "buf", "lint") {
		t.Fatal("npx -y buf lint")
	}
	if npxMatches([]string{"npx", "buf", "format"}, "buf", "lint") {
		t.Fatal("should not match buf format")
	}
}

func TestExecArgvSubcommand_branches(t *testing.T) {
	t.Parallel()
	// npx where rest has < 2 args (tool only, no subcommand)
	if execArgvSubcommand([]string{"npx", "-y", "dprint"}, "dprint", "fmt") {
		t.Fatal("npx: rest<2 should return false")
	}
	// yarn branch — should match
	if !execArgvSubcommand([]string{"yarn", "dprint", "fmt"}, "dprint", "fmt") {
		t.Fatal("yarn dprint fmt should match")
	}
}

func TestIsPyPkgToolSubcommandPythonMArgv_branches(t *testing.T) {
	t.Parallel()
	// npx with len(argv)>=4 but rest<4 after stripping flags — return false at L108-110
	// ["npx", "-y", "a", "b"]: len=4>=4, rest=["a","b"] (len=2<4) → return false
	if isPyPkgToolSubcommandPythonMArgv([]string{"npx", "-y", "a", "b"}, "sqlfluff", "lint") {
		t.Fatal("npx rest<4 should return false")
	}
	// npx with full python -m tool sub — recursive match
	if !isPyPkgToolSubcommandPythonMArgv([]string{"npx", "python3", "-m", "sqlfluff", "lint"}, "sqlfluff", "lint") {
		t.Fatal("npx python3 -m sqlfluff lint should match")
	}
	// yarn branch — match
	if !isPyPkgToolSubcommandPythonMArgv([]string{"yarn", "python3", "-m", "sqlfluff", "lint"}, "sqlfluff", "lint") {
		t.Fatal("yarn python3 -m sqlfluff lint should match")
	}
}

func TestIsRuffArgv_branches(t *testing.T) {
	t.Parallel()
	// len<1 guard (direct call with empty argv)
	if isRuffArgv([]string{}) {
		t.Fatal("empty argv: should return false")
	}
	// npx with rest<1 (only -y consumed) — return false
	if isRuffArgv([]string{"npx", "-y"}) {
		t.Fatal("npx -y: rest<1 should return false")
	}
	// npx with ruff — recursive call succeeds
	if !isRuffArgv([]string{"npx", "ruff", "check"}) {
		t.Fatal("npx ruff check should match")
	}
}

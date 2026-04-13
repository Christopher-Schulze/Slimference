package filter

import (
	"path/filepath"
	"strings"
)

// npxCommandIndex returns the argv index of the first token after npx (argv[0]) that starts
// the command to run, skipping common npx flags (-y, --yes, -p/--package with value, -c/--call
// with value, --, and other single-token flags beginning with "-").
func npxCommandIndex(argv []string) int {
	if len(argv) < 2 {
		return len(argv)
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "npx" && b != "npx.cmd" {
		return len(argv)
	}
	i := 1
	for i < len(argv) {
		a := argv[i]
		if a == "--" {
			if i+1 < len(argv) {
				return i + 1
			}
			return len(argv)
		}
		switch a {
		case "-y", "--yes":
			i++
		case "-p", "--package", "-c", "--call":
			i++
			if i < len(argv) {
				i++
			}
		default:
			if strings.HasPrefix(a, "-") {
				i++
				continue
			}
			return i
		}
	}
	return len(argv)
}

// npxArgvSuffix returns argv[commandIndex:] when argv[0] is npx, or (nil, false) otherwise.
func npxArgvSuffix(argv []string) ([]string, bool) {
	if len(argv) < 1 {
		return nil, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "npx" && b != "npx.cmd" {
		return nil, false
	}
	i := npxCommandIndex(argv)
	if i >= len(argv) {
		return nil, true
	}
	return argv[i:], true
}

// npxMatches reports whether npx argv resolves to the given leading command tokens (exact match).
func npxMatches(argv []string, parts ...string) bool {
	rest, ok := npxArgvSuffix(argv)
	if !ok || len(rest) < len(parts) {
		return false
	}
	for i, p := range parts {
		if rest[i] != p {
			return false
		}
	}
	return true
}

// execArgvSubcommand matches `tool sub` / `npx|pnpm exec|yarn … tool sub` for non-Python CLIs (F09/F24).
func execArgvSubcommand(argv []string, tool, sub string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return execArgvSubcommand(rest, tool, sub)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return execArgvSubcommand(argv[2:], tool, sub)
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return execArgvSubcommand(argv[1:], tool, sub)
	}
	b := filepath.Base(argv[0])
	return (strings.EqualFold(b, tool) || strings.EqualFold(b, tool+".exe")) && argv[1] == sub
}

// isPyPkgToolSubcommandPythonMArgv matches `python -m tool sub` after unwrapping npx/pnpm/yarn.
func isPyPkgToolSubcommandPythonMArgv(argv []string, tool, sub string) bool {
	if len(argv) < 4 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 4 {
			return false
		}
		return isPyPkgToolSubcommandPythonMArgv(rest, tool, sub)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isPyPkgToolSubcommandPythonMArgv(argv[2:], tool, sub)
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isPyPkgToolSubcommandPythonMArgv(argv[1:], tool, sub)
	}
	b := b0
	if b != "python" && b != "python3" && b != "python.exe" && b != "python3.exe" {
		return false
	}
	return argv[1] == "-m" && argv[2] == tool && argv[3] == sub
}

// isPyPkgToolSubcommandArgv matches `tool sub` / `python -m tool sub` / `npx|pnpm exec|yarn …` thereof (e.g. sqlfluff lint).
func isPyPkgToolSubcommandArgv(argv []string, tool, sub string) bool {
	return execArgvSubcommand(argv, tool, sub) || isPyPkgToolSubcommandPythonMArgv(argv, tool, sub)
}

// isRuffArgv reports `ruff …` / `python -m ruff …` / `npx|pnpm exec|yarn …` thereof (Layer 0 compacts).
func isRuffArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 1 {
			return false
		}
		return isRuffArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isRuffArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isRuffArgv(argv[1:])
	}
	b := b0
	if b == "ruff" || b == "ruff.exe" {
		return true
	}
	if b != "python" && b != "python3" && b != "python.exe" && b != "python3.exe" {
		return false
	}
	return len(argv) >= 3 && argv[1] == "-m" && argv[2] == "ruff"
}

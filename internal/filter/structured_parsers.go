package filter

import (
	"path/filepath"
	"strings"
)

type structuredParser struct {
	name    string
	matches func(argv []string) bool
	parse   func(argv []string, stdout string) (compact string, hadFailures bool, ok bool)
}

var structuredParsers = []structuredParser{
	{"go_build", isGoBuildOrVetArgv, parseStructuredWithoutArgv(parseGoErrors)},
	{"cargo_build", isCargoBuildOrCheckArgv, parseCargoErrorsForArgv},
	{"gcc_clang", isGccClangArgv, parseStructuredWithoutArgv(parseGccClangErrors)},
	{"focused_lint", isFocusedLintDiagnosticArgv, parseFocusedLintDiagnosticsForArgv},
	{"typescript", isTypeScriptDiagnosticArgv, parseStructuredWithoutArgv(parseTypeScriptDiagnostics)},
	{"svelte", isSvelteDiagnosticArgv, parseStructuredWithoutArgv(parseSvelteDiagnostics)},
	{"frontend", isFrontendDiagnosticArgv, parseStructuredWithoutArgv(parseFrontendDiagnostics)},
	{"python", isPythonDiagnosticArgv, parseStructuredWithoutArgv(parsePythonDiagnostics)},
	{"zig", isZigDiagnosticArgv, parseStructuredWithoutArgv(parseZigDiagnostics)},
	{"sql", isSQLDiagnosticArgv, parseStructuredWithoutArgv(parseSQLDiagnostics)},
	{"markdown", isMarkdownDiagnosticArgv, parseStructuredWithoutArgv(parseMarkdownDiagnostics)},
	{"ecosystem", isPracticalEcosystemDiagnosticArgv, parseStructuredWithoutArgv(parsePracticalEcosystemDiagnostics)},
}

func ParseFailures(argv []string, stdout string) (string, bool) {
	for _, p := range structuredParsers {
		if p.matches(argv) {
			compact, hadFailures, ok := p.parse(argv, stdout)
			if !ok {
				continue
			}
			if hadFailures {
				return compact, true
			}
			return compact, true
		}
	}
	return "", false
}

func parseStructuredWithoutArgv(fn func(string) (string, bool, bool)) func([]string, string) (string, bool, bool) {
	return func(_ []string, stdout string) (string, bool, bool) {
		return fn(stdout)
	}
}

func isGoBuildOrVetArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	if !isGoBinary(argv[0]) {
		return false
	}
	sub := argv[1]
	return sub == "build" || sub == "vet" || sub == "test"
}

func isCargoBuildOrCheckArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	if !isCargoBin(argv[0]) {
		return false
	}
	return argv[1] == "build" || argv[1] == "check" || argv[1] == "clippy"
}

func isGccClangArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	return b == "gcc" || b == "g++" || b == "cc" || b == "c++" ||
		b == "clang" || b == "clang++" || b == "gcc.exe" || b == "g++.exe" ||
		b == "clang.exe" || b == "clang++.exe"
}

const failureContextLines = 2
const maxFailureBlockLines = 30

package filter

import (
	"path/filepath"
	"strings"
)

type structuredParser struct {
	name    string
	matches func(argv []string) bool
	parse   func(stdout string) (compact string, hadFailures bool, ok bool)
}

var structuredParsers = []structuredParser{
	{"go_build", isGoBuildOrVetArgv, parseGoErrors},
	{"cargo_build", isCargoBuildOrCheckArgv, parseCargoErrors},
	{"gcc_clang", isGccClangArgv, parseGccClangErrors},
}

func ParseFailures(argv []string, stdout string) (string, bool) {
	for _, p := range structuredParsers {
		if p.matches(argv) {
			compact, hadFailures, ok := p.parse(stdout)
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

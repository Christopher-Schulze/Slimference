package filter

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reColonDiagnostic = regexp.MustCompile(`^[^\s:][^:\n]*:\d+(:\d+)?:\s*(error|warning|fatal error|note|hint|info|E\d+|W\d+|TS\d+|[A-Z]+-\d+)\b`)
	reParenDiagnostic = regexp.MustCompile(`^[^( \t][^\n]*\(\d+,\d+\):\s*(error|warning)\s+[A-Z]+\d+:`)
	rePipeDiagnostic  = regexp.MustCompile(`^\s*\d+\s+[^\s].*\s+(error|warning|style|convention|refactor)\s+[\w./-]+`)
	reSQLFluffLine    = regexp.MustCompile(`^L:\s*\d+\s*\|\s*P:\s*\d+\s*\|\s*[A-Z]{1,4}\d{2}\s*\|`)
	reMarkdownLine    = regexp.MustCompile(`^[^\s:][^:\n]*\.md:\d+(:\d+)?:\s*MD\d{3}\b`)
	reSummaryLine     = regexp.MustCompile(`(?i)(\b(error|errors|warning|warnings|failed|failures|violations|problems|issues|diagnostics)\b|✖|✗)`)
)

func parseDiagnosticRows(label string, stdout string) (string, bool, bool) {
	if strings.TrimSpace(stdout) == "" {
		return "", false, false
	}
	if detectBuildSuccess(stdout) {
		result := "[" + label + "] ok\n"
		if len(result) < len(stdout) {
			return result, false, true
		}
		return "", false, false
	}
	lines := strings.Split(stdout, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isDiagnosticLine(trimmed) || isDiagnosticSummary(trimmed) {
			kept = append(kept, trimmed)
		}
	}
	if len(kept) == 0 {
		return "", false, false
	}
	result := "[" + label + "] FAILED\n" + strings.Join(dedupeAdjacent(kept), "\n") + "\n"
	if len(result) >= len(stdout) {
		return "", false, false
	}
	return result, true, true
}

func isDiagnosticLine(line string) bool {
	return reColonDiagnostic.MatchString(line) ||
		reParenDiagnostic.MatchString(line) ||
		rePipeDiagnostic.MatchString(line) ||
		reSQLFluffLine.MatchString(line) ||
		reMarkdownLine.MatchString(line)
}

func isDiagnosticSummary(line string) bool {
	if len(line) > 220 {
		return false
	}
	return reSummaryLine.MatchString(line)
}

func dedupeAdjacent(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(out) > 0 && out[len(out)-1] == line {
			continue
		}
		out = append(out, line)
	}
	return out
}

func parseTypeScriptDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("typescript", stdout)
}

func parseZigDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("zig", stdout)
}

func parseSvelteDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("svelte", stdout)
}

func parseSQLDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("sql", stdout)
}

func parseMarkdownDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("markdown", stdout)
}

func parsePracticalEcosystemDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("ecosystem", stdout)
}

func isTypeScriptDiagnosticArgv(argv []string) bool {
	return commandMatchesAny(argv, "tsc", "vue-tsc", "tsserver")
}

func isSvelteDiagnosticArgv(argv []string) bool {
	return commandMatchesAny(argv, "svelte-check")
}

func isZigDiagnosticArgv(argv []string) bool {
	return execArgvSubcommand(argv, "zig", "build") ||
		execArgvSubcommand(argv, "zig", "test") ||
		commandMatchesAny(argv, "zig")
}

func isSQLDiagnosticArgv(argv []string) bool {
	return isPyPkgToolSubcommandArgv(argv, "sqlfluff", "lint") ||
		commandMatchesAny(argv, "sqlfluff", "sqruff", "psql")
}

func isMarkdownDiagnosticArgv(argv []string) bool {
	return commandMatchesAny(argv, "markdownlint", "markdownlint-cli2", "mdformat")
}

func isPracticalEcosystemDiagnosticArgv(argv []string) bool {
	return commandMatchesAny(argv,
		"swift", "xcodebuild", "swiftlint", "swift-format",
		"kotlinc", "gradle", "mvn", "sbt", "scalac",
		"php", "phpstan", "psalm", "phpunit", "composer",
		"dart", "flutter",
		"lua", "luacheck",
		"protoc", "buf", "graphql-codegen",
		"hadolint", "docker", "make", "ninja", "cmake",
		"terraform", "tofu",
		"pwsh", "powershell",
		"perl", "ocamlc", "dune", "cabal", "stack", "ghc",
		"erlc", "rebar3", "mix",
		"solc", "forge",
		"jsonnet", "jsonnetfmt",
	)
}

func commandMatchesAny(argv []string, names ...string) bool {
	if len(argv) == 0 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		return ok && commandMatchesAny(rest, names...)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return commandMatchesAny(argv[2:], names...)
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return commandMatchesAny(argv[1:], names...)
	}
	if len(argv) >= 3 && (b0 == "bun" || b0 == "bun.exe") && (argv[1] == "x" || argv[1] == "run") {
		return commandMatchesAny(argv[2:], names...)
	}
	for _, name := range names {
		if strings.EqualFold(b0, name) || strings.EqualFold(b0, name+".exe") {
			return true
		}
	}
	return false
}

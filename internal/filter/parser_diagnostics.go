package filter

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	reColonDiagnostic  = regexp.MustCompile(`^[^\s:][^:\n]*:\d+(:\d+)?:\s*(error|warning|fatal error|note|hint|info|E\d+|W\d+|TS\d+|[A-Z]\d{3,4}|[A-Z]+-\d+)\b`)
	reDashDiagnostic   = regexp.MustCompile(`^[^\s:][^:\n]*:\d+(:\d+)?\s+-\s+(error|warning|information|hint)\b`)
	reParenDiagnostic  = regexp.MustCompile(`^[^( \t][^\n]*\(\d+,\d+\):\s*(error|warning)\s+[A-Z]+\d+:`)
	rePipeDiagnostic   = regexp.MustCompile(`^\s*\d+\s+[^\s].*\s+(error|warning|style|convention|refactor)\s+[\w./-]+`)
	reSQLFluffLine     = regexp.MustCompile(`^L:\s*\d+\s*\|\s*P:\s*\d+\s*\|\s*[A-Z]{1,4}\d{2}\s*\|`)
	reMarkdownLine     = regexp.MustCompile(`^[^\s:][^:\n]*\.md:\d+(:\d+)?:\s*MD\d{3}\b`)
	rePytestFailedLine = regexp.MustCompile(`^(FAILED|ERROR)\s+[^ \t]+\.py(::|\s+-\s+)`)
	reKotlinLine       = regexp.MustCompile(`^[ew]:\s+[^:\n]+\.kts?:\s+\(\d+,\s*\d+\):\s+`)
	reSummaryLine      = regexp.MustCompile(`(?i)(\b(error|errors|warning|warnings|failed|failures|violations|problems|issues|diagnostics)\b|✖|✗)`)

	reFocusedLintColonLine    = regexp.MustCompile(`^[^\s:][^:\n]*:\d+(:\d+)?:\s+\S`)
	reFocusedLintGocycloLine  = regexp.MustCompile(`^\d+\s+[^\s:][^:\n]*:\d+:\d+:\s+\S`)
	reFocusedLintMisspellLine = regexp.MustCompile(`^[^\s:][^:\n]*:\d+:\d+\s+found\s+"[^"]+"\s+a\s+misspelling\s+of\s+"[^"]+"$`)
	reFocusedLintCSVRow       = regexp.MustCompile(`^"[^"\n]+",\d+,\d+,[^,\n]+,[^,\n]+$`)
)

func parseDiagnosticRows(label string, stdout string) (string, bool, bool) {
	if strings.TrimSpace(stdout) == "" {
		return "", false, false
	}
	if detectBuildSuccess(stdout) {
		if buildOutputHasNonZeroWarning(stdout) {
			return "", false, false
		}
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
		reDashDiagnostic.MatchString(line) ||
		reParenDiagnostic.MatchString(line) ||
		rePipeDiagnostic.MatchString(line) ||
		reSQLFluffLine.MatchString(line) ||
		reMarkdownLine.MatchString(line) ||
		rePytestFailedLine.MatchString(line) ||
		reKotlinLine.MatchString(line)
}

func isDiagnosticSummary(line string) bool {
	if len(line) > 220 {
		return false
	}
	if isSuccessfulDiagnosticSummary(line) {
		return false
	}
	return reSummaryLine.MatchString(line)
}

func isSuccessfulDiagnosticSummary(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "no issues") ||
		strings.Contains(lower, "no errors") ||
		strings.Contains(lower, "found 0 issues") ||
		strings.Contains(lower, "found 0 errors")
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
	compact, hadFailures, ok := parseDiagnosticRows("typescript", stdout)
	if !ok {
		return "", false, false
	}
	if hadFailures && (!typeScriptDiagnosticsHaveConcreteDetail(compact) || typeScriptDiagnosticPayloadHasSourceContext(stdout)) {
		return "", false, false
	}
	return compact, hadFailures, true
}

func typeScriptDiagnosticsHaveConcreteDetail(compact string) bool {
	for raw := range strings.SplitSeq(compact, "\n") {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		if containsTypeScriptDiagnosticCode(line) &&
			(strings.Contains(lower, "error") || strings.Contains(lower, "warning")) {
			return true
		}
	}
	return false
}

func typeScriptDiagnosticPayloadHasSourceContext(stdout string) bool {
	for raw := range strings.SplitSeq(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || isDiagnosticLine(line) || isDiagnosticSummary(line) {
			continue
		}
		if typeScriptLineLooksLikeSource(line) {
			return true
		}
	}
	return false
}

func typeScriptLineLooksLikeSource(line string) bool {
	for _, prefix := range []string{
		"import ",
		"export ",
		"function ",
		"class ",
		"const ",
		"let ",
		"var ",
		"interface ",
		"type ",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
		if strings.Contains(line, "| "+prefix) {
			return true
		}
	}
	return false
}

func containsTypeScriptDiagnosticCode(line string) bool {
	for i := 0; i+2 < len(line); i++ {
		if line[i] != 'T' || line[i+1] != 'S' {
			continue
		}
		if line[i+2] >= '0' && line[i+2] <= '9' {
			return true
		}
	}
	return false
}

func parseZigDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("zig", stdout)
}

func parseSvelteDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("svelte", stdout)
}

func parseFrontendDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("frontend", stdout)
}

func parsePythonDiagnostics(stdout string) (string, bool, bool) {
	return parseDiagnosticRows("python", stdout)
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

func parseFocusedLintDiagnosticsForArgv(argv []string, stdout string) (string, bool, bool) {
	label, ok := focusedLintDiagnosticLabel(argv)
	if !ok {
		return "", false, false
	}
	return parseFocusedLintDiagnostics(label, stdout)
}

func parseFocusedLintDiagnostics(label string, stdout string) (string, bool, bool) {
	if strings.TrimSpace(stdout) == "" {
		return "", false, false
	}
	lines := strings.Split(stdout, "\n")
	diagnostics := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if isFocusedLintDiagnosticLine(line) {
			diagnostics = append(diagnostics, line)
			continue
		}
		if isFocusedLintNeutralLine(line, label) {
			continue
		}
		return "", false, false
	}
	if len(diagnostics) == 0 {
		return "", false, false
	}
	compactedDiagnostics := compactAdjacentFocusedLintDiagnostics(diagnostics)
	result := "[" + label + "] FAILED (" + diagnosticCountText(len(diagnostics)) + ")\n" +
		strings.Join(compactedDiagnostics, "\n") + "\n"
	if len(result) >= len(stdout) {
		return "", false, false
	}
	return result, true, true
}

func isFocusedLintDiagnosticLine(line string) bool {
	if line == "file,line,column,typo,corrected" {
		return true
	}
	return reFocusedLintColonLine.MatchString(line) ||
		reFocusedLintGocycloLine.MatchString(line) ||
		reFocusedLintMisspellLine.MatchString(line) ||
		reFocusedLintCSVRow.MatchString(line)
}

func isFocusedLintNeutralLine(line string, label string) bool {
	lower := strings.ToLower(line)
	if strings.HasPrefix(line, "> ") || strings.HasPrefix(line, "$ ") {
		return true
	}
	if strings.HasPrefix(lower, "running "+label) ||
		strings.HasPrefix(lower, label+" ") ||
		strings.HasPrefix(lower, "checking ") ||
		strings.HasPrefix(lower, "analyzing ") {
		return true
	}
	return false
}

func compactAdjacentFocusedLintDiagnostics(lines []string) []string {
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		line := lines[i]
		j := i + 1
		for j < len(lines) && lines[j] == line {
			j++
		}
		count := j - i
		if count == 1 {
			out = append(out, line)
		} else {
			out = append(out, line+" (repeated "+strconv.Itoa(count)+" times)")
		}
		i = j
	}
	return out
}

func diagnosticCountText(count int) string {
	if count == 1 {
		return "1 diagnostic"
	}
	return strconv.Itoa(count) + " diagnostics"
}

func isFocusedLintDiagnosticArgv(argv []string) bool {
	_, ok := focusedLintDiagnosticLabel(argv)
	return ok
}

func focusedLintDiagnosticLabel(argv []string) (string, bool) {
	if !commandMatchesAny(argv,
		"golangci-lint", "staticcheck", "revive",
		"errcheck", "ineffassign", "nilaway", "unparam",
		"misspell", "gocyclo", "forbidigo", "prealloc",
	) {
		return "", false
	}
	label := lintToolLabel(argv)
	if label == "" {
		return "", false
	}
	return label, true
}

func isTypeScriptDiagnosticArgv(argv []string) bool {
	return commandMatchesAny(argv, "tsc", "vue-tsc", "tsserver")
}

func isSvelteDiagnosticArgv(argv []string) bool {
	return commandMatchesAny(argv, "svelte-check")
}

func isFrontendDiagnosticArgv(argv []string) bool {
	if commandMatchesAny(argv,
		"next", "vite", "vitest", "jest", "playwright",
		"eslint", "biome", "oxlint", "turbo", "nx", "lerna",
	) {
		return true
	}
	return isBunDiagnosticArgv(argv)
}

func isPythonDiagnosticArgv(argv []string) bool {
	if isRuffArgv(argv) ||
		isPylintArgv(argv) ||
		isFlake8Argv(argv) ||
		isMypyArgv(argv) ||
		isUvRunPytestArgv(argv) ||
		isPoetryRunPytestArgv(argv) ||
		isToxExplicitTestEnvArgv(argv) ||
		isPythonUnittestArgv(argv) {
		return true
	}
	if isPytestArgv(argv) {
		return true
	}
	return commandMatchesAny(argv, "pyright", "basedpyright", "pytest", "py.test")
}

func isPytestArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		return ok && isPytestArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isPytestArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isPytestArgv(argv[1:])
	}
	if b0 == "pytest" || b0 == "py.test" || b0 == "pytest.exe" {
		return true
	}
	if b0 != "python" && b0 != "python3" && b0 != "python.exe" && b0 != "python3.exe" {
		return false
	}
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-m" && (argv[i+1] == "pytest" || argv[i+1] == "py.test") {
			return true
		}
	}
	return false
}

func isBunDiagnosticArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "bun" && b != "bun.exe" {
		return false
	}
	switch argv[1] {
	case "test", "build":
		return true
	default:
		return false
	}
}

func isZigDiagnosticArgv(argv []string) bool {
	return execArgvSubcommand(argv, "zig", "build") ||
		execArgvSubcommand(argv, "zig", "test") ||
		commandMatchesAny(argv, "zig")
}

func isSQLDiagnosticArgv(argv []string) bool {
	return isPyPkgToolSubcommandArgv(argv, "sqlfluff", "lint") ||
		commandMatchesAny(argv,
			"sqlfluff", "sqruff",
			"psql", "sqlite3", "sqlite", "mysql", "mariadb",
			"prisma", "drizzle-kit",
		)
}

func isMarkdownDiagnosticArgv(argv []string) bool {
	return commandMatchesAny(argv, "markdownlint", "markdownlint-cli2", "mdformat")
}

func isPracticalEcosystemDiagnosticArgv(argv []string) bool {
	return commandMatchesAny(argv,
		"swift", "xcodebuild", "swiftlint", "swift-format",
		"swiftc", "kotlinc", "gradle", "gradlew", "mvn", "mvnw", "javac", "sbt", "scalac",
		"php", "phpstan", "psalm", "phpunit", "composer",
		"dart", "flutter",
		"lua", "luacheck",
		"protoc", "buf", "graphql-codegen",
		"hadolint", "docker", "docker-compose", "podman", "nerdctl",
		"kubectl", "oc", "helm",
		"make", "ninja", "cmake",
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
	if len(argv) >= 3 && (b0 == "npm" || b0 == "npm.cmd") && (argv[1] == "exec" || argv[1] == "x") {
		rest := argv[2:]
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		return commandMatchesAny(rest, names...)
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

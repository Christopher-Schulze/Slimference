package filter

import (
	"path/filepath"
	"strings"
)

type packageManagerScriptParser func([]string, []byte) ([]byte, bool)

type packageManagerScriptCandidate struct {
	argv    []string
	payload []byte
}

func compactPackageManagerBuildScriptOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := compactPackageManagerScriptOutput(argv, stdout, packageManagerBuildScriptParsers()); ok {
		return out, true
	}
	if out, ok := compactPackageManagerTypeScriptFailureScriptOutput(argv, stdout); ok {
		return out, true
	}
	return compactPackageManagerMypyScriptOutput(argv, stdout)
}

func compactPackageManagerLintScriptOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := compactPackageManagerScriptOutput(argv, stdout, packageManagerLintScriptParsers()); ok {
		return out, true
	}
	if out, ok := compactPackageManagerEslintStylishScriptOutput(argv, stdout); ok {
		return out, true
	}
	return compactPackageManagerScriptFailureOutput(argv, stdout)
}

func compactPackageManagerFormatScriptOutput(argv []string, stdout []byte) ([]byte, bool) {
	return compactPackageManagerScriptOutput(argv, stdout, packageManagerFormatScriptParsers())
}

func compactPackageManagerScriptOutput(argv []string, stdout []byte, parsers []packageManagerScriptParser) ([]byte, bool) {
	if !isSafePackageManagerScriptArgv(argv) || strings.TrimSpace(string(stdout)) == "" {
		return stdout, false
	}
	for _, candidate := range packageManagerScriptTranscriptCandidates(stdout) {
		for _, parse := range parsers {
			if out, ok := parse(candidate.argv, candidate.payload); ok && packageManagerScriptOKSummary(out) && len(out) < len(stdout) {
				return out, true
			}
		}
	}
	return stdout, false
}

func compactPackageManagerScriptFailureOutput(argv []string, stdout []byte) ([]byte, bool) {
	if !isSafePackageManagerScriptArgv(argv) || strings.TrimSpace(string(stdout)) == "" {
		return stdout, false
	}
	for _, candidate := range packageManagerScriptTranscriptCandidates(stdout) {
		compact, ok := ParseFailures(candidate.argv, string(candidate.payload))
		if !ok || !packageManagerScriptFailureSummary([]byte(compact)) || len(compact) >= len(stdout) {
			continue
		}
		return []byte(compact), true
	}
	return stdout, false
}

func compactPackageManagerTypeScriptFailureScriptOutput(argv []string, stdout []byte) ([]byte, bool) {
	if !isSafePackageManagerScriptArgv(argv) || strings.TrimSpace(string(stdout)) == "" {
		return stdout, false
	}
	for _, candidate := range packageManagerScriptTranscriptCandidates(stdout) {
		compact, ok := TryCompactTscDiagnostics(candidate.argv, candidate.payload)
		if !ok || !packageManagerTypeScriptFailureSummary(compact) || len(compact) >= len(stdout) {
			continue
		}
		return compact, true
	}
	return stdout, false
}

func compactPackageManagerMypyScriptOutput(argv []string, stdout []byte) ([]byte, bool) {
	if !isSafePackageManagerScriptArgv(argv) || strings.TrimSpace(string(stdout)) == "" {
		return stdout, false
	}
	for _, candidate := range packageManagerScriptTranscriptCandidates(stdout) {
		if compact, ok := TryCompactMypyDiagnostics(candidate.argv, candidate.payload); ok && len(compact) < len(stdout) {
			return compact, true
		}
		if compact, ok := TryCompactMypy(candidate.argv, candidate.payload); ok &&
			packageManagerScriptOKSummary(compact) &&
			len(compact) < len(stdout) {
			return compact, true
		}
	}
	return stdout, false
}

func compactPackageManagerEslintStylishScriptOutput(argv []string, stdout []byte) ([]byte, bool) {
	if !isSafePackageManagerScriptArgv(argv) || strings.TrimSpace(string(stdout)) == "" {
		return stdout, false
	}
	for _, candidate := range packageManagerScriptTranscriptCandidates(stdout) {
		compact, ok := TryCompactEslintStylish(candidate.argv, candidate.payload)
		if !ok || !packageManagerEslintStylishFindingsSummary(compact) || len(compact) >= len(stdout) {
			continue
		}
		return compact, true
	}
	return stdout, false
}

func packageManagerScriptFailureSummary(out []byte) bool {
	text := strings.TrimSpace(string(out))
	if !strings.HasPrefix(text, "[") {
		return false
	}
	closeBracket := strings.IndexByte(text, ']')
	if closeBracket <= 0 {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(text[closeBracket+1:]), "FAILED")
}

func packageManagerEslintStylishFindingsSummary(out []byte) bool {
	return strings.HasPrefix(strings.TrimSpace(string(out)), "[eslint] FINDINGS (")
}

func packageManagerTypeScriptFailureSummary(out []byte) bool {
	return strings.HasPrefix(strings.TrimSpace(string(out)), "[typescript] FAILED")
}

func packageManagerBuildScriptParsers() []packageManagerScriptParser {
	return []packageManagerScriptParser{
		TryCompactGoBuild,
		TryCompactCargoBuild,
		TryCompactCargoCheck,
		TryCompactCargoDoc,
		TryCompactMake,
		TryCompactNinja,
		TryCompactCmakeBuild,
		TryCompactTsc,
		TryCompactNextBuild,
		TryCompactViteBuild,
		TryCompactTsupBuild,
		TryCompactWebpack,
		TryCompactRspackBuild,
		TryCompactParcelBuild,
		TryCompactRollupConfig,
		TryCompactEsbuildBundle,
		TryCompactNxBuild,
		TryCompactMoonRunBuild,
		TryCompactTurboBuild,
		TryCompactNpmRunBuild,
		TryCompactPnpmRunBuild,
		TryCompactYarnRunBuild,
		TryCompactMvn,
		TryCompactGradle,
		TryCompactZigBuild,
		TryCompactWasmPackBuild,
		TryCompactBazelBuild,
		TryCompactSwiftBuild,
		TryCompactBufBuild,
		TryCompactKoBuild,
		TryCompactMesonCompile,
		TryCompactPackBuild,
		TryCompactJust,
	}
}

func packageManagerLintScriptParsers() []packageManagerScriptParser {
	return []packageManagerScriptParser{
		TryCompactCargoClippy,
		TryCompactCargoAudit,
		TryCompactGolangciLint,
		TryCompactStaticcheck,
		TryCompactGocritic,
		TryCompactGosec,
		TryCompactBufLint,
		TryCompactProtolint,
		TryCompactSemgrep,
		TryCompactJscpd,
		TryCompactDjlint,
		TryCompactTyCheck,
		TryCompactGofumpt,
		TryCompactRevive,
		TryCompactErrcheck,
		TryCompactIneffassign,
		TryCompactNilaway,
		TryCompactGoVet,
		TryCompactUnparam,
		TryCompactMisspell,
		TryCompactGocyclo,
		TryCompactForbidigo,
		TryCompactPrealloc,
		TryCompactPreCommit,
		TryCompactRuffCheck,
		TryCompactPylint,
		TryCompactFlake8,
		TryCompactBandit,
		TryCompactBiomeCheck,
		TryCompactSqlfluffLint,
		TryCompactTaploCheck,
		TryCompactCueVet,
		TryCompactSpectralLint,
		TryCompactOxlint,
		TryCompactDenoLint,
		TryCompactDartAnalyze,
		TryCompactFlutterAnalyze,
		TryCompactSwiftlint,
		TryCompactKtlint,
		TryCompactDetekt,
		TryCompactShellcheck,
		TryCompactAnsibleLint,
		TryCompactHadolint,
		TryCompactMarkdownlint,
		TryCompactYamllint,
		TryCompactDotenvLinter,
		TryCompactKubeLinter,
		TryCompactTflint,
		TryCompactCfnLint,
		TryCompactActionlint,
		TryCompactZizmor,
		TryCompactVale,
		TryCompactRubocop,
		TryCompactPint,
		TryCompactPhpcs,
		TryCompactPhpstan,
		TryCompactPsalm,
		TryCompactPhan,
		TryCompactMypy,
		TryCompactPyright,
		TryCompactEslint,
		TryCompactStylelint,
	}
}

func packageManagerFormatScriptParsers() []packageManagerScriptParser {
	return []packageManagerScriptParser{
		TryCompactPrettier,
		TryCompactDprint,
		TryCompactBiomeFormat,
		TryCompactBufFormat,
		TryCompactTerraformFmt,
		TryCompactBlack,
		TryCompactRuffFormat,
		TryCompactTaploFormat,
		TryCompactShfmt,
		TryCompactSqlfmt,
		TryCompactIsort,
		TryCompactAutopep8,
		TryCompactGofmt,
		TryCompactRustfmt,
		TryCompactClangFormat,
		TryCompactZigFmt,
	}
}

func isSafePackageManagerScriptArgv(argv []string) bool {
	script, ok := packageManagerScriptName(argv)
	return ok && safePackageManagerScriptName(script)
}

func packageManagerScriptName(argv []string) (string, bool) {
	if len(argv) < 2 {
		return "", false
	}
	b0 := packageManagerBinaryName(argv[0])
	switch b0 {
	case "npm":
		if argv[1] == "run" || argv[1] == "run-script" {
			return packageManagerScriptNameFromArgs(argv[2:])
		}
	case "pnpm":
		if argv[1] == "run" {
			return packageManagerScriptNameFromArgs(argv[2:])
		}
		if packageManagerScriptShorthandOK(argv[1]) {
			return argv[1], true
		}
	case "yarn", "yarnpkg":
		if argv[1] == "run" {
			return packageManagerScriptNameFromArgs(argv[2:])
		}
		if packageManagerScriptShorthandOK(argv[1]) {
			return argv[1], true
		}
	case "bun":
		if argv[1] == "run" {
			return packageManagerScriptNameFromArgs(argv[2:])
		}
		if packageManagerScriptShorthandOK(argv[1]) {
			return argv[1], true
		}
	}
	return "", false
}

func packageManagerScriptNameFromArgs(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if packageManagerRunOptionHasValue(arg) {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg, true
	}
	return "", false
}

func packageManagerRunOptionHasValue(arg string) bool {
	switch arg {
	case "-C", "--dir", "--cwd", "-w", "--workspace", "--filter":
		return true
	}
	return false
}

func packageManagerScriptShorthandOK(arg string) bool {
	if arg == "" || strings.HasPrefix(arg, "-") || packageManagerBuiltinCommand(arg) {
		return false
	}
	return true
}

func packageManagerBuiltinCommand(arg string) bool {
	switch strings.ToLower(arg) {
	case "add", "audit", "bin", "cache", "config", "create", "dedupe", "deploy", "dlx",
		"env", "exec", "explain", "fetch", "help", "i", "import", "init", "install",
		"link", "list", "login", "logout", "ls", "outdated", "owner", "pack", "patch",
		"prune", "publish", "rebuild", "remove", "root", "run", "run-script", "search",
		"setup", "start", "store", "team", "test", "unlink", "unpublish", "update",
		"upgrade", "up", "version", "view", "why", "workspace", "workspaces":
		return true
	default:
		return false
	}
}

func packageManagerBinaryName(name string) string {
	base := strings.ToLower(filepath.Base(name))
	base = strings.TrimSuffix(base, ".cmd")
	base = strings.TrimSuffix(base, ".exe")
	return base
}

func safePackageManagerScriptName(script string) bool {
	s := strings.ToLower(strings.TrimSpace(script))
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	for _, term := range []string{"deploy", "publish", "release", "start", "serve", "dev", "watch", "preview", "fix", "write", "update", "migrate"} {
		if packageManagerScriptNameHasTerm(s, term) {
			return false
		}
	}
	switch s {
	case "lint", "typecheck", "type-check", "type:check", "check-types", "check:types",
		"types", "tsc", "build", "compile", "format:check", "fmt:check", "format-check",
		"fmt-check", "check:format", "check:fmt", "prettier:check", "check:prettier":
		return true
	}
	if strings.HasPrefix(s, "lint:") || strings.HasSuffix(s, ":lint") {
		return true
	}
	if strings.HasPrefix(s, "build:") || strings.HasSuffix(s, ":build") {
		return true
	}
	if strings.HasPrefix(s, "typecheck:") || strings.HasSuffix(s, ":typecheck") ||
		strings.HasPrefix(s, "type-check:") || strings.HasSuffix(s, ":type-check") {
		return true
	}
	return false
}

func packageManagerScriptNameHasTerm(script, term string) bool {
	if script == term {
		return true
	}
	for _, part := range strings.FieldsFunc(script, func(r rune) bool {
		return r == ':' || r == '-' || r == '_' || r == '.'
	}) {
		if part == term {
			return true
		}
	}
	return false
}

func packageManagerScriptTranscriptCandidates(stdout []byte) []packageManagerScriptCandidate {
	lines := strings.SplitAfter(string(stdout), "\n")
	out := make([]packageManagerScriptCandidate, 0, 2)
	for i, line := range lines {
		command, ok := packageManagerScriptTranscriptCommand(line)
		if !ok {
			continue
		}
		argv := ArgvForCapturedOutput(command)
		if len(argv) == 0 || packageManagerScriptNestedRunArgv(argv) {
			continue
		}
		payload := packageManagerTrimScriptPayload(strings.Join(lines[i+1:], ""))
		out = append(out, packageManagerScriptCandidate{argv: argv, payload: payload})
	}
	return out
}

func packageManagerScriptTranscriptCommand(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"> ", "$ "} {
		if strings.HasPrefix(trimmed, prefix) {
			command := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			return command, command != ""
		}
	}
	return "", false
}

func packageManagerScriptNestedRunArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	switch packageManagerBinaryName(argv[0]) {
	case "npm":
		return argv[1] == "run" || argv[1] == "run-script" || argv[1] == "test"
	case "pnpm", "yarn", "yarnpkg", "bun":
		return argv[1] == "run" || argv[1] == "test"
	default:
		return false
	}
}

func packageManagerTrimScriptPayload(payload string) []byte {
	lines := strings.SplitAfter(payload, "\n")
	for len(lines) > 0 {
		line := lines[len(lines)-1]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || packageManagerSuccessFooter(trimmed) {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}
	return []byte(strings.Join(lines, ""))
}

func packageManagerSuccessFooter(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, "done in ") && strings.HasSuffix(lower, ".")
}

func packageManagerScriptOKSummary(out []byte) bool {
	text := strings.TrimSpace(string(out))
	if !strings.HasPrefix(text, "[") {
		return false
	}
	closeBracket := strings.IndexByte(text, ']')
	return closeBracket > 0 && strings.HasPrefix(text[closeBracket+1:], " ok")
}

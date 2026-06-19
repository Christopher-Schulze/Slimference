package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

const (
	commandOutputFirstDisableEnv = "SLIMFERENCE_COMMAND_OUTPUT_FIRST_DISABLE"
	commandOutputFirstActiveEnv  = "SLIMFERENCE_COMMAND_OUTPUT_FIRST"
)

func maybeApplyCommandOutputFirstEnv(mode string, command []string) ([]string, func()) {
	if !commandOutputFirstModeEnabled(mode) || os.Getenv(commandOutputFirstDisableEnv) == "1" {
		return command, func() {}
	}
	env, cleanup, ok := prepareCommandOutputFirstEnv()
	if !ok {
		return command, cleanup
	}
	return insertEnvAssignmentsBeforeUtility(command, env...), cleanup
}

func commandOutputFirstModeEnabled(mode string) bool {
	switch mode {
	case "proxied", "proxied-wss", "proxied-wss-bridge":
		return true
	default:
		return false
	}
}

func prepareCommandOutputFirstEnv() ([]string, func(), bool) {
	self, err := osExecutable()
	if err != nil || strings.TrimSpace(self) == "" {
		return nil, func() {}, false
	}
	dir, err := os.MkdirTemp("", "slimference-codex-cof-*")
	if err != nil {
		return nil, func() {}, false
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	shims := 0
	for _, command := range []string{
		"git", "rg", "go", "npm", "pnpm", "yarn", "bun", "cargo",
		"pytest", "py.test", "python", "python3", "uv", "poetry",
		"fd", "fdfind", "find", "wc",
		"make", "gmake", "cmake", "ninja", "npx", "tsc", "next", "vite",
		"webpack", "webpack-cli", "pre-commit", "ruff", "pyright",
		"basedpyright", "stylelint", "prettier", "mypy",
		"vitest", "jest", "mocha", "ava", "karma", "playwright", "cypress",
		"wdio", "nx", "turbo", "deno", "phpunit", "ctest", "ginkgo",
		"nox", "tox", "hatch", "rspec", "rake", "rails", "dart", "flutter",
		"gradle", "sbt", "mill",
		"tsup", "rspack", "parcel", "rollup", "esbuild", "mvn", "mvnw",
		"gradlew", "meson", "zig", "wasm-pack", "bazel", "bazelisk",
		"swift", "buf", "ko", "moon", "pack",
	} {
		realBin, err := exec.LookPath(command)
		if err != nil || strings.TrimSpace(realBin) == "" {
			continue
		}
		if err := writeCommandOutputFirstShim(filepath.Join(dir, command), self, realBin, command); err != nil {
			cleanup()
			return nil, func() {}, false
		}
		shims++
	}
	if shims == 0 {
		cleanup()
		return nil, func() {}, false
	}
	bashEnv := filepath.Join(dir, "bash_env")
	bashEnvScript := "export " + commandOutputFirstActiveEnv + "=1\n" +
		"export PATH=" + shellQuote(dir) + "${PATH:+:$PATH}\n"
	if err := os.WriteFile(bashEnv, []byte(bashEnvScript), 0644); err != nil {
		cleanup()
		return nil, func() {}, false
	}
	path := dir
	if oldPath := os.Getenv("PATH"); oldPath != "" {
		path += string(os.PathListSeparator) + oldPath
	}
	return []string{
		"PATH=" + path,
		"BASH_ENV=" + bashEnv,
		commandOutputFirstActiveEnv + "=1",
	}, cleanup, true
}

func writeCommandOutputFirstShim(path, slimferenceBin, realBin, command string) error {
	script := "#!/bin/sh\n" +
		"exec " + shellQuote(slimferenceBin) + " __command-output-first-shim --command=" + shellQuote(command) +
		" --real-bin=" + shellQuote(realBin) + " -- \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		return err
	}
	return nil
}

func insertEnvAssignmentsBeforeUtility(command []string, assignments ...string) []string {
	if len(command) == 0 || len(assignments) == 0 {
		return append([]string(nil), command...)
	}
	idx := 1
	for idx < len(command) {
		arg := command[idx]
		if arg == "-u" {
			idx += 2
			continue
		}
		if strings.HasPrefix(arg, "-") {
			idx++
			continue
		}
		if isEnvAssignment(arg) {
			idx++
			continue
		}
		break
	}
	out := make([]string, 0, len(command)+len(assignments))
	out = append(out, command[:idx]...)
	out = append(out, assignments...)
	out = append(out, command[idx:]...)
	return out
}

func isEnvAssignment(arg string) bool {
	idx := strings.Index(arg, "=")
	if idx <= 0 {
		return false
	}
	key := arg[:idx]
	return !strings.ContainsAny(key, " /.-")
}

func handleCommandOutputFirstShim(args []string) {
	code := runCommandOutputFirstShim(args, os.Stdin, os.Stdout, os.Stderr)
	exitFn(code)
}

func runCommandOutputFirstShim(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, childArgs, err := parseCommandOutputFirstShimArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "__command-output-first-shim: %v\n", err)
		return 2
	}
	if !commandOutputFirstAllowCapture(cfg.command, childArgs) {
		return execCommandOutputFirstPassthrough(cfg.realBin, childArgs)
	}
	cmd := exec.Command(cfg.realBin, childArgs...)
	cmd.Stdin = stdin
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	code := commandExitCode(runErr)
	rawOut := outBuf.Bytes()
	rawErr := errBuf.Bytes()
	compacted, ok := compactCommandOutputFirst(cfg.command, cfg.realBin, childArgs, rawOut, rawErr, code)
	if ok {
		recoverable, ok := archiveCommandOutputFirstCompaction(cfg.command, childArgs, rawOut, compacted)
		if !ok {
			_, _ = stdout.Write(rawOut)
			_, _ = stderr.Write(rawErr)
			return code
		}
		recordCommandOutputFirstRun(cfg.command, childArgs, rawOut, recoverable)
		_, _ = stdout.Write(recoverable)
		return code
	}
	_, _ = stdout.Write(rawOut)
	_, _ = stderr.Write(rawErr)
	return code
}

type commandOutputFirstShimConfig struct {
	command string
	realBin string
}

func parseCommandOutputFirstShimArgs(args []string) (commandOutputFirstShimConfig, []string, error) {
	var cfg commandOutputFirstShimConfig
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if cfg.command == "" || cfg.realBin == "" {
				return cfg, nil, errors.New("missing --command or --real-bin")
			}
			return cfg, append([]string(nil), args[i+1:]...), nil
		}
		switch {
		case strings.HasPrefix(arg, "--command="):
			cfg.command = strings.TrimPrefix(arg, "--command=")
		case strings.HasPrefix(arg, "--real-bin="):
			cfg.realBin = strings.TrimPrefix(arg, "--real-bin=")
		default:
			return cfg, nil, fmt.Errorf("unknown arg %q", arg)
		}
	}
	return cfg, nil, errors.New("missing -- child separator")
}

func commandOutputFirstAllowCapture(command string, args []string) bool {
	switch command {
	case "git":
		sub := commandOutputFirstGitSubcommand(args)
		switch sub {
		case "status", "ls-files":
			return true
		case "grep":
			return true
		case "diff":
			return commandOutputFirstGitDiffMetadataOnly(args)
		default:
			return false
		}
	case "rg":
		return true
	case "fd", "fdfind", "find":
		return commandOutputFirstPathListAllowed(command, args)
	case "wc":
		return commandOutputFirstWcAllowed(args)
	case "go":
		switch commandOutputFirstGoSubcommand(args) {
		case "test", "build":
			return true
		default:
			return false
		}
	case "npm", "pnpm", "yarn", "bun":
		return commandOutputFirstPackageScriptAllowed(command, args)
	case "npx":
		return commandOutputFirstNpxAllowed(args)
	case "cargo":
		return commandOutputFirstCargoAllowed(args)
	case "pytest", "py.test", "python", "python3", "uv", "poetry":
		return commandOutputFirstPythonTestAllowed(command, args)
	default:
		return commandOutputFirstDirectBuildAllowed(command, args) ||
			commandOutputFirstDirectTestAllowed(command, args) ||
			commandOutputFirstDirectLintAllowed(command, args) ||
			commandOutputFirstDirectFormatAllowed(command, args)
	}
}

func commandOutputFirstDirectBuildAllowed(command string, args []string) bool {
	switch command {
	case "make", "gmake":
		return !commandOutputFirstArgsContain(args, "-n", "--just-print", "--dry-run") &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "ninja":
		return !commandOutputFirstArgsContain(args, "-n", "--just-print", "--dry-run", "-t") &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "tsc":
		return !commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "next", "vite":
		return commandOutputFirstFirstNonOption(args) == "build" &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "webpack", "webpack-cli":
		return !commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "cmake":
		return len(args) > 0 && args[0] == "--build" &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "tsup":
		return !commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "rspack", "parcel":
		return commandOutputFirstFirstNonOption(args) == "build" &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "rollup":
		return commandOutputFirstArgsContain(args, "-c", "--config") &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "esbuild":
		return commandOutputFirstEsbuildBundleAllowed(args) &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "nx":
		return commandOutputFirstFirstNonOption(args) == "build" &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "turbo":
		return commandOutputFirstTurboBuildAllowed(args) &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "mvn", "mvnw":
		return commandOutputFirstMavenBuildAllowed(args)
	case "gradle", "gradlew":
		return commandOutputFirstArgsContain(args, "build") &&
			!commandOutputFirstBuildArgsUnsafeLongRunning(args)
	case "meson":
		return commandOutputFirstFirstNonOption(args) == "compile"
	case "zig", "wasm-pack", "bazel", "bazelisk", "swift", "buf", "ko", "pack":
		return commandOutputFirstFirstNonOption(args) == "build"
	case "moon":
		return commandOutputFirstMoonBuildAllowed(args)
	default:
		return false
	}
}

func commandOutputFirstDirectTestAllowed(command string, args []string) bool {
	switch command {
	case "vitest", "jest", "mocha", "ava", "phpunit", "ctest", "ginkgo", "rspec":
		return true
	case "karma":
		return commandOutputFirstFirstNonOption(args) == "start"
	case "playwright":
		return commandOutputFirstFirstNonOption(args) == "test"
	case "cypress", "wdio":
		return commandOutputFirstFirstNonOption(args) == "run"
	case "nx":
		return commandOutputFirstFirstNonOption(args) == "test"
	case "turbo":
		return commandOutputFirstTurboTestAllowed(args)
	case "deno", "dart", "flutter", "hatch":
		return commandOutputFirstFirstNonOption(args) == "test"
	case "nox":
		return commandOutputFirstNoxTestAllowed(args)
	case "tox":
		return commandOutputFirstToxTestAllowed(args)
	case "rake":
		sub := commandOutputFirstFirstNonOption(args)
		return sub == "test" || sub == "spec"
	case "rails":
		return commandOutputFirstFirstNonOption(args) == "test"
	case "gradle", "sbt":
		return commandOutputFirstArgsContain(args, "test")
	case "mill":
		return commandOutputFirstMillTestAllowed(args)
	default:
		return false
	}
}

func commandOutputFirstDirectLintAllowed(command string, args []string) bool {
	switch command {
	case "pre-commit":
		return len(args) > 0 && args[0] == "run"
	case "ruff":
		return commandOutputFirstArgsContain(args, "check")
	case "pyright", "basedpyright", "stylelint", "mypy":
		return true
	default:
		return false
	}
}

func commandOutputFirstDirectFormatAllowed(command string, args []string) bool {
	switch command {
	case "prettier":
		return commandOutputFirstArgsContain(args, "--check", "-c", "--list-different")
	case "ruff":
		return commandOutputFirstArgsContain(args, "format") &&
			commandOutputFirstArgsContain(args, "--check")
	default:
		return false
	}
}

func commandOutputFirstNpxAllowed(args []string) bool {
	tool, toolArgs, ok := commandOutputFirstNpxTool(args)
	if !ok {
		return false
	}
	return commandOutputFirstDirectBuildAllowed(tool, toolArgs) ||
		commandOutputFirstDirectTestAllowed(tool, toolArgs) ||
		commandOutputFirstDirectLintAllowed(tool, toolArgs) ||
		commandOutputFirstDirectFormatAllowed(tool, toolArgs)
}

func commandOutputFirstNpxTool(args []string) (string, []string, bool) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return "", nil, false
		}
		if arg == "--" {
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", nil, false
			}
			return strings.TrimSpace(args[i+1]), append([]string(nil), args[i+2:]...), true
		}
		if commandOutputFirstNpxOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return "", nil, false
			}
			continue
		}
		switch {
		case arg == "-y", arg == "--yes", arg == "--no-install", arg == "--ignore-existing",
			arg == "--quiet", arg == "--npm":
			continue
		case strings.HasPrefix(arg, "--package="), strings.HasPrefix(arg, "-p="):
			continue
		case strings.HasPrefix(arg, "-"):
			return "", nil, false
		default:
			return arg, append([]string(nil), args[i+1:]...), true
		}
	}
	return "", nil, false
}

func commandOutputFirstNpxOptionConsumesValue(arg string) bool {
	switch arg {
	case "-p", "--package", "--userconfig", "--cache":
		return true
	default:
		return false
	}
}

func commandOutputFirstArgsContain(args []string, wants ...string) bool {
	for _, arg := range args {
		for _, want := range wants {
			if arg == want {
				return true
			}
		}
	}
	return false
}

func commandOutputFirstFirstNonOption(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return ""
		}
		if arg == "--" {
			if i+1 < len(args) {
				return strings.TrimSpace(args[i+1])
			}
			return ""
		}
		if commandOutputFirstGenericOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return ""
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func commandOutputFirstGenericOptionConsumesValue(arg string) bool {
	switch arg {
	case "-c", "--config", "-C", "--cwd", "--project", "-p", "--project-name",
		"--grep", "-g", "--filter", "--browser", "--reporter":
		return true
	default:
		return false
	}
}

func commandOutputFirstTurboTestAllowed(args []string) bool {
	first := commandOutputFirstFirstNonOption(args)
	if first == "test" {
		return true
	}
	if first != "run" {
		return false
	}
	return commandOutputFirstArgsContain(args, "test")
}

func commandOutputFirstBuildArgsUnsafeLongRunning(args []string) bool {
	return commandOutputFirstArgsContain(args, "--watch", "-w", "--continuous", "watch", "dev", "serve", "start")
}

func commandOutputFirstEsbuildBundleAllowed(args []string) bool {
	for _, arg := range args {
		if arg == "--bundle" || strings.HasPrefix(arg, "--bundle=") {
			return true
		}
	}
	return false
}

func commandOutputFirstTurboBuildAllowed(args []string) bool {
	first := commandOutputFirstFirstNonOption(args)
	if first == "build" {
		return true
	}
	if first != "run" {
		return false
	}
	return commandOutputFirstArgsContain(args, "build")
}

func commandOutputFirstMavenBuildAllowed(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return false
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		switch arg {
		case "compile", "test", "package", "verify", "install":
			return true
		case "deploy", "release", "site":
			return false
		}
	}
	return false
}

func commandOutputFirstMoonBuildAllowed(args []string) bool {
	if commandOutputFirstFirstNonOption(args) != "run" {
		return false
	}
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "build" || strings.HasSuffix(arg, ":build") {
			return true
		}
	}
	return false
}

func commandOutputFirstNoxTestAllowed(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "-s" || arg == "--session":
			return i+1 < len(args) && strings.TrimSpace(args[i+1]) == "test"
		case strings.HasPrefix(arg, "--session="):
			return strings.TrimPrefix(arg, "--session=") == "test"
		}
	}
	return false
}

func commandOutputFirstToxTestAllowed(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "-e" || arg == "--env":
			return i+1 < len(args) && strings.TrimSpace(args[i+1]) == "test"
		case strings.HasPrefix(arg, "-e="):
			return strings.TrimPrefix(arg, "-e=") == "test"
		case strings.HasPrefix(arg, "--env="):
			return strings.TrimPrefix(arg, "--env=") == "test"
		}
	}
	return false
}

func commandOutputFirstMillTestAllowed(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "test" || strings.HasSuffix(arg, ".test") {
			return true
		}
	}
	return false
}

func compactCommandOutputFirst(command, realBin string, args []string, stdout, stderr []byte, code int) ([]byte, bool) {
	if code != 0 || len(stderr) != 0 {
		return nil, false
	}
	argv := append([]string{realBin}, args...)
	switch command {
	case "git":
		switch commandOutputFirstGitSubcommand(args) {
		case "status":
			if strings.TrimSpace(string(stdout)) != "" {
				return nil, false
			}
			compacted, ok := filter.TryCompactGitStatus(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "diff":
			if !commandOutputFirstGitDiffMetadataOnly(args) {
				return nil, false
			}
			compacted, ok := filter.TryCompactGitDiff(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "ls-files":
			compacted, ok := filter.TryCompactGitLsFiles(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "grep":
			compacted, ok := filter.TryCompactSearchOutputWithOptions(argv, stdout, filter.SearchCompactOptions{
				MinRetainedPct: 100,
			})
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		default:
			return nil, false
		}
	case "rg":
		compacted, ok := filter.TryCompactPathListOutput(argv, stdout)
		if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
			return out, true
		}
		compacted, ok = filter.TryCompactSearchOutputWithOptions(argv, stdout, filter.SearchCompactOptions{
			MinRetainedPct: 100,
		})
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "fd", "fdfind", "find":
		compacted, ok := filter.TryCompactPathListOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "wc":
		compacted, ok := filter.TryCompactWc(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "go":
		switch commandOutputFirstGoSubcommand(args) {
		case "test":
			compacted, ok := filter.TryCompactTestOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "build":
			compacted, ok := filter.TryCompactBuildOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		default:
			return nil, false
		}
	case "npm", "pnpm", "yarn", "bun":
		packageArgs := commandOutputFirstPackageScriptFilterArgs(command, args)
		packageArgv := append([]string{realBin}, packageArgs...)
		if commandOutputFirstPackageScriptIsTest(command, args) {
			compacted, ok := filter.TryCompactTestOutput(packageArgv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstPackageScriptIsBuild(command, args) {
			compacted, ok := filter.TryCompactBuildOutput(packageArgv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstPackageScriptIsLint(command, args) {
			compacted, ok := filter.TryCompactLintOutput(packageArgv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstPackageScriptIsFormat(command, args) {
			compacted, ok := filter.TryCompactFormatOutput(packageArgv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		return nil, false
	case "npx":
		tool, toolArgs, ok := commandOutputFirstNpxTool(args)
		if !ok {
			return nil, false
		}
		if commandOutputFirstDirectTestAllowed(tool, toolArgs) {
			compacted, ok := filter.TryCompactTestOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstDirectBuildAllowed(tool, toolArgs) {
			compacted, ok := filter.TryCompactBuildOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstDirectLintAllowed(tool, toolArgs) {
			compacted, ok := filter.TryCompactLintOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstDirectFormatAllowed(tool, toolArgs) {
			compacted, ok := filter.TryCompactFormatOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		return nil, false
	case "cargo":
		switch commandOutputFirstCargoSubcommand(args) {
		case "test", "nextest", "llvm-cov":
			compacted, ok := filter.TryCompactTestOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "build", "check", "doc":
			compacted, ok := filter.TryCompactBuildOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "clippy", "audit":
			compacted, ok := filter.TryCompactLintOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		default:
			return nil, false
		}
	case "pytest", "py.test", "python", "python3", "uv", "poetry":
		compacted, ok := filter.TryCompactTestOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	default:
		if commandOutputFirstDirectTestAllowed(command, args) {
			compacted, ok := filter.TryCompactTestOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstDirectBuildAllowed(command, args) {
			compacted, ok := filter.TryCompactBuildOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstDirectLintAllowed(command, args) {
			compacted, ok := filter.TryCompactLintOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstDirectFormatAllowed(command, args) {
			compacted, ok := filter.TryCompactFormatOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		return nil, false
	}
}

func commandOutputFirstPositiveCompaction(compacted []byte, ok bool, raw []byte) ([]byte, bool) {
	if !ok {
		return nil, false
	}
	inputTokens := filter.EstimateTokensFromBytes(len(raw))
	outputTokens := filter.EstimateTokensFromBytes(len(compacted))
	if inputTokens <= outputTokens {
		return nil, false
	}
	return compacted, true
}

func archiveCommandOutputFirstCompaction(command string, args []string, rawOut, compacted []byte) ([]byte, bool) {
	if len(rawOut) == 0 || len(compacted) == 0 {
		return nil, false
	}
	home, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, false
	}
	label := commandOutputFirstLabel(command, args)
	entry, err := contentarchive.Put(contentarchive.DefaultDir(home), contentarchive.Input{
		SubLayer: "command_output_first",
		Original: string(rawOut),
		Preview:  label,
	}, contentarchive.Limits{})
	if err != nil || entry == nil || strings.TrimSpace(entry.URI) == "" {
		return nil, false
	}
	marker := "\n[context-archive kind=tool-output uri=" + entry.URI + " recover=\"slimference expand " + entry.URI + "\"]\n"
	recoverable := []byte(strings.TrimRight(string(compacted), "\n") + marker)
	return commandOutputFirstPositiveCompaction(recoverable, true, rawOut)
}

func recordCommandOutputFirstRun(command string, args []string, rawOut, compacted []byte) {
	dbPath, err := resolveFilterDBPathFn()
	if err != nil || strings.TrimSpace(dbPath) == "" {
		return
	}
	if err := osMkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		return
	}
	defer db.Close()
	wd, err := osGetwd()
	if err != nil {
		wd = ""
	}
	inputTokens := filter.EstimateTokensFromBytes(len(rawOut))
	outputTokens := filter.EstimateTokensFromBytes(len(compacted))
	if inputTokens <= outputTokens {
		return
	}
	savingsPct := float64(inputTokens-outputTokens) * 100 / float64(inputTokens)
	_ = filter.RecordFilterRun(db, commandOutputFirstLabel(command, args), wd, inputTokens, outputTokens, savingsPct, time.Now())
}

func commandOutputFirstLabel(command string, args []string) string {
	label := "[command-output-first:" + command + "] " + command
	if len(args) > 0 {
		label += " " + strings.Join(args, " ")
	}
	return label
}

func commandOutputFirstGitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return ""
		}
		switch {
		case arg == "-C" || arg == "-c" || arg == "--git-dir" || arg == "--work-tree" ||
			arg == "--namespace" || arg == "--exec-path":
			i++
			if i >= len(args) {
				return ""
			}
		case strings.HasPrefix(arg, "--git-dir="), strings.HasPrefix(arg, "--work-tree="),
			strings.HasPrefix(arg, "--namespace="), strings.HasPrefix(arg, "--exec-path="),
			strings.HasPrefix(arg, "-c"):
			continue
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return arg
		}
	}
	return ""
}

func commandOutputFirstGitDiffMetadataOnly(args []string) bool {
	for _, arg := range args {
		switch {
		case arg == "--stat", strings.HasPrefix(arg, "--stat="), arg == "--name-only", arg == "--name-status":
			return true
		}
	}
	return false
}

func commandOutputFirstGoSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return ""
		}
		switch {
		case arg == "-C":
			i++
			if i >= len(args) {
				return ""
			}
		case strings.HasPrefix(arg, "-C="), strings.HasPrefix(arg, "-"):
			continue
		default:
			return arg
		}
	}
	return ""
}

func commandOutputFirstPathListAllowed(command string, args []string) bool {
	argv := append([]string{command}, args...)
	return filter.PathListOutputReducerEligibleArgv(argv)
}

func commandOutputFirstWcAllowed(args []string) bool {
	hasExplicitInput := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return false
		}
		if arg == "--" {
			for j := i + 1; j < len(args); j++ {
				if strings.TrimSpace(args[j]) == "" || args[j] == "-" {
					return false
				}
				hasExplicitInput = true
			}
			return hasExplicitInput
		}
		if !strings.HasPrefix(arg, "-") {
			hasExplicitInput = true
			continue
		}
		if arg == "-" {
			return false
		}
		if strings.HasPrefix(arg, "--") {
			switch arg {
			case "--lines", "--words", "--chars", "--bytes", "--max-line-length":
				continue
			default:
				return false
			}
		}
		for _, ch := range arg[1:] {
			switch ch {
			case 'l', 'w', 'm', 'c', 'L':
			default:
				return false
			}
		}
	}
	return hasExplicitInput
}

func commandOutputFirstCargoAllowed(args []string) bool {
	sub, idx := commandOutputFirstCargoCommand(args)
	switch sub {
	case "test", "llvm-cov", "build", "check", "doc", "clippy", "audit":
		return true
	case "nextest":
		return idx+1 < len(args) && strings.TrimSpace(args[idx+1]) == "run"
	default:
		return false
	}
}

func commandOutputFirstCargoSubcommand(args []string) string {
	sub, _ := commandOutputFirstCargoCommand(args)
	return sub
}

func commandOutputFirstCargoCommand(args []string) (string, int) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return "", -1
		}
		if strings.HasPrefix(arg, "+") && len(arg) > 1 {
			continue
		}
		switch {
		case arg == "-C" || arg == "--config" || arg == "-Z":
			i++
			if i >= len(args) {
				return "", -1
			}
		case strings.HasPrefix(arg, "--config="), strings.HasPrefix(arg, "-Z"):
			continue
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return arg, i
		}
	}
	return "", -1
}

func commandOutputFirstPythonTestAllowed(command string, args []string) bool {
	switch command {
	case "pytest", "py.test":
		return true
	case "python", "python3":
		module := commandOutputFirstPythonModule(args)
		return module == "pytest" || module == "unittest"
	case "uv":
		return commandOutputFirstUvRunPytest(args)
	case "poetry":
		return commandOutputFirstPoetryRunPytest(args)
	default:
		return false
	}
}

func commandOutputFirstPythonModule(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return ""
		}
		if arg == "-m" {
			if i+1 >= len(args) {
				return ""
			}
			return strings.TrimSpace(args[i+1])
		}
		if commandOutputFirstPythonOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return ""
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return ""
	}
	return ""
}

func commandOutputFirstPythonOptionConsumesValue(arg string) bool {
	switch arg {
	case "-W", "-X":
		return true
	default:
		return false
	}
}

func commandOutputFirstUvRunPytest(args []string) bool {
	verb, idx := packageScriptFirstCommand(args)
	if verb != "run" {
		return false
	}
	if idx+1 >= len(args) {
		return false
	}
	next := strings.TrimSpace(args[idx+1])
	if next == "pytest" {
		return true
	}
	if (next == "python" || next == "python3") && idx+3 < len(args) && args[idx+2] == "-m" && args[idx+3] == "pytest" {
		return true
	}
	return false
}

func commandOutputFirstPoetryRunPytest(args []string) bool {
	if len(args) < 2 || strings.TrimSpace(args[0]) != "run" {
		return false
	}
	next := strings.TrimSpace(args[1])
	if next == "pytest" {
		return true
	}
	if (next == "python" || next == "python3") && len(args) >= 4 && args[2] == "-m" && args[3] == "pytest" {
		return true
	}
	return false
}

func commandOutputFirstPackageScriptAllowed(command string, args []string) bool {
	return commandOutputFirstPackageScriptIsTest(command, args) ||
		commandOutputFirstPackageScriptIsBuild(command, args) ||
		commandOutputFirstPackageScriptIsLint(command, args) ||
		commandOutputFirstPackageScriptIsFormat(command, args)
}

func commandOutputFirstPackageScriptIsTest(command string, args []string) bool {
	switch command {
	case "npm", "pnpm", "yarn":
		return packageScriptVerb(args) == "test" || packageRunScriptName(args) == "test"
	case "bun":
		return packageScriptVerb(args) == "test"
	default:
		return false
	}
}

func commandOutputFirstPackageScriptIsBuild(command string, args []string) bool {
	script := packageRunScriptName(args)
	switch command {
	case "npm", "pnpm", "yarn":
		return script == "build" || script == "typecheck"
	case "bun":
		return script == "build" || script == "typecheck"
	default:
		return false
	}
}

func commandOutputFirstPackageScriptIsLint(command string, args []string) bool {
	switch command {
	case "npm", "pnpm", "yarn", "bun":
		return packageRunScriptName(args) == "lint"
	default:
		return false
	}
}

func commandOutputFirstPackageScriptIsFormat(command string, args []string) bool {
	switch command {
	case "npm", "pnpm", "yarn", "bun":
		return commandOutputFirstFormatScriptName(packageRunScriptName(args))
	default:
		return false
	}
}

func commandOutputFirstFormatScriptName(script string) bool {
	switch script {
	case "format:check", "check:format", "fmt:check", "check:fmt", "prettier:check":
		return true
	default:
		return false
	}
}

func commandOutputFirstPackageScriptFilterArgs(command string, args []string) []string {
	verb, idx := packageScriptFirstCommand(args)
	if idx < 0 {
		return append([]string(nil), args...)
	}
	switch command {
	case "npm", "pnpm", "yarn":
		if verb == "run" {
			if script := packageRunScriptName(args); script != "" {
				return []string{"run", script}
			}
		}
	case "bun":
	default:
		return append([]string(nil), args...)
	}
	out := make([]string, 0, len(args)-idx)
	out = append(out, verb)
	out = append(out, args[idx+1:]...)
	return out
}

func packageScriptVerb(args []string) string {
	verb, _ := packageScriptFirstCommand(args)
	return verb
}

func packageRunScriptName(args []string) string {
	verb, idx := packageScriptFirstCommand(args)
	if verb != "run" {
		return ""
	}
	for j := idx + 1; j < len(args); j++ {
		name := strings.TrimSpace(args[j])
		if name == "" {
			return ""
		}
		if strings.HasPrefix(name, "-") {
			continue
		}
		return name
	}
	return ""
}

func packageScriptFirstCommand(args []string) (string, int) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return "", -1
		}
		if arg == "--" {
			if i+1 < len(args) {
				next := strings.TrimSpace(args[i+1])
				if next != "" {
					return next, i + 1
				}
			}
			return "", -1
		}
		if packageScriptOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return "", -1
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg, i
	}
	return "", -1
}

func packageScriptOptionConsumesValue(arg string) bool {
	switch arg {
	case "-C", "--prefix", "--workspace", "--dir", "--cwd", "--filter", "--project", "-F":
		return true
	default:
		return false
	}
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 127
}

func execCommandOutputFirstPassthrough(realBin string, args []string) int {
	argv := append([]string{realBin}, args...)
	if err := syscall.Exec(realBin, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "__command-output-first-shim passthrough: %v\n", err)
		return 127
	}
	return 127
}

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
		"git", "rg", "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift",
		"go", "npm", "pnpm", "yarn", "bun", "cargo",
		"pytest", "py.test", "python", "python3", "uv", "poetry",
		"pip", "pip3",
		"fd", "fdfind", "find", "plocate", "locate", "wc",
		"make", "gmake", "cmake", "ninja", "npx", "tsc", "next", "vite",
		"webpack", "webpack-cli", "pre-commit", "ruff", "pyright",
		"basedpyright", "stylelint", "eslint", "prettier", "mypy",
		"golangci-lint", "staticcheck", "revive", "errcheck",
		"ineffassign", "nilaway", "unparam", "misspell", "gocyclo",
		"forbidigo", "prealloc", "gocritic", "gosec", "protolint",
		"semgrep", "jscpd", "djlint", "ty", "biome", "sqlfluff",
		"taplo", "cue", "spectral", "oxlint", "shellcheck",
		"ansible-lint", "hadolint", "markdownlint", "yamllint",
		"dotenv-linter", "kube-linter", "tflint", "cfn-lint",
		"actionlint", "zizmor", "vale", "rubocop", "pint", "phpcs",
		"phpstan", "psalm", "phan", "bandit", "pylint", "flake8",
		"swiftlint", "ktlint", "detekt",
		"vitest", "jest", "mocha", "ava", "karma", "playwright", "cypress",
		"wdio", "nx", "turbo", "deno", "phpunit", "ctest", "ginkgo",
		"nox", "tox", "hatch", "rspec", "rake", "rails", "dart", "flutter",
		"gradle", "sbt", "mill",
		"tsup", "rspack", "parcel", "rollup", "esbuild", "mvn", "mvnw",
		"dotnet", "dotnet.exe",
		"gradlew", "meson", "zig", "wasm-pack", "bazel", "bazelisk",
		"swift", "buf", "ko", "moon", "pack",
		"docker", "podman", "nerdctl", "docker-compose", "kubectl", "oc", "helm",
		"terraform", "tofu", "tf", "gh", "glab", "aws", "jq",
		"curl", "wget", "http", "https",
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
	compacted, ok := compactCommandOutputFirstStreams(cfg.command, cfg.realBin, childArgs, rawOut, rawErr, code)
	if ok {
		recoverable, ok := archiveCommandOutputFirstCompaction(cfg.command, childArgs, compacted.stream, compacted.raw, compacted.compacted)
		if !ok {
			_, _ = stdout.Write(rawOut)
			_, _ = stderr.Write(rawErr)
			return code
		}
		accountingRaw := compacted.raw
		if len(compacted.accountingRaw) != 0 {
			accountingRaw = compacted.accountingRaw
		}
		if compacted.stream == "stderr" {
			accountingCompacted := append(append([]byte(nil), compacted.passthroughStdout...), recoverable...)
			recordCommandOutputFirstRun(cfg.command, childArgs, accountingRaw, accountingCompacted)
			_, _ = stdout.Write(compacted.passthroughStdout)
			_, _ = stderr.Write(recoverable)
		} else {
			accountingCompacted := append(append([]byte(nil), recoverable...), compacted.passthroughStderr...)
			recordCommandOutputFirstRun(cfg.command, childArgs, accountingRaw, accountingCompacted)
			_, _ = stdout.Write(recoverable)
			_, _ = stderr.Write(compacted.passthroughStderr)
		}
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

type commandOutputFirstCompaction struct {
	stream            string
	raw               []byte
	compacted         []byte
	passthroughStdout []byte
	passthroughStderr []byte
	accountingRaw     []byte
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
		case "show":
			return commandOutputFirstGitShowMetadataOnly(args)
		default:
			return false
		}
	case "rg":
		return true
	case "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift":
		return true
	case "fd", "fdfind", "find":
		return commandOutputFirstPathListAllowed(command, args)
	case "plocate", "locate":
		return commandOutputFirstLocateAllowed(args)
	case "wc":
		return commandOutputFirstWcAllowed(args)
	case "go":
		if commandOutputFirstKnownJSONOutputAllowed(command, args) {
			return true
		}
		switch commandOutputFirstGoSubcommand(args) {
		case "test", "build":
			return true
		default:
			return false
		}
	case "npm", "pnpm", "yarn", "bun":
		if commandOutputFirstKnownJSONOutputAllowed(command, args) {
			return true
		}
		return commandOutputFirstPackageScriptAllowed(command, args) ||
			commandOutputFirstPackageOutputAllowed(command, args)
	case "npx":
		return commandOutputFirstNpxAllowed(args)
	case "cargo":
		return commandOutputFirstCargoAllowed(args) ||
			commandOutputFirstKnownJSONOutputAllowed(command, args)
	case "pytest", "py.test", "python", "python3", "uv", "poetry":
		return commandOutputFirstPythonTestAllowed(command, args) ||
			commandOutputFirstPythonMypyAllowed(command, args) ||
			commandOutputFirstPythonModuleLintAllowed(command, args) ||
			commandOutputFirstPackageOutputAllowed(command, args)
	case "pip", "pip3":
		return commandOutputFirstPackageOutputAllowed(command, args)
	case "docker", "podman", "nerdctl", "docker-compose", "kubectl", "oc", "helm":
		return commandOutputFirstContainerStatusAllowed(command, args) ||
			commandOutputFirstKnownJSONOutputAllowed(command, args)
	case "terraform", "tofu", "tf":
		return commandOutputFirstTerraformAllowed(args) ||
			commandOutputFirstKnownJSONOutputAllowed(command, args)
	case "gh", "glab":
		return commandOutputFirstVCSHostAllowed(args)
	case "aws":
		return commandOutputFirstAwsJSONAllowed(args)
	case "jq":
		return true
	case "curl", "wget", "http", "https":
		return commandOutputFirstNetworkResponseAllowed(command, args)
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
	case "dotnet", "dotnet.exe":
		return commandOutputFirstDotnetBuildAllowed(args)
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
	case "dotnet", "dotnet.exe":
		return commandOutputFirstDotnetTestAllowed(args)
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
	case "golangci-lint", "staticcheck", "revive", "errcheck", "ineffassign",
		"nilaway", "unparam", "misspell", "gocyclo", "forbidigo", "prealloc":
		return true
	case "ruff":
		return commandOutputFirstArgsContain(args, "check")
	case "gocritic":
		return commandOutputFirstFirstNonOption(args) == "check"
	case "buf":
		return commandOutputFirstFirstNonOption(args) == "lint"
	case "ty", "biome", "taplo":
		return commandOutputFirstFirstNonOption(args) == "check"
	case "sqlfluff", "spectral", "deno":
		return commandOutputFirstFirstNonOption(args) == "lint"
	case "cue":
		return commandOutputFirstFirstNonOption(args) == "vet"
	case "pyright", "basedpyright", "stylelint", "eslint", "mypy",
		"gosec", "protolint", "semgrep", "jscpd", "djlint",
		"oxlint", "shellcheck", "ansible-lint", "hadolint",
		"markdownlint", "yamllint", "dotenv-linter", "kube-linter",
		"tflint", "cfn-lint", "actionlint", "zizmor", "vale",
		"rubocop", "pint", "phpcs", "phpstan", "psalm", "phan",
		"bandit", "pylint", "flake8", "swiftlint", "ktlint", "detekt":
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

func commandOutputFirstDotnetBuildAllowed(args []string) bool {
	switch commandOutputFirstFirstNonOption(args) {
	case "build", "publish", "pack":
		return !commandOutputFirstBuildArgsUnsafeLongRunning(args)
	default:
		return false
	}
}

func commandOutputFirstDotnetTestAllowed(args []string) bool {
	return commandOutputFirstFirstNonOption(args) == "test" &&
		!commandOutputFirstBuildArgsUnsafeLongRunning(args)
}

func commandOutputFirstContainerStatusAllowed(command string, args []string) bool {
	if len(args) == 0 || commandOutputFirstArgsContain(args, "-w", "--watch", "--watch-only") {
		return false
	}
	switch command {
	case "docker", "podman", "nerdctl":
		switch args[0] {
		case "ps", "images":
			return true
		case "compose":
			return len(args) >= 2 && (args[1] == "ps" || args[1] == "ls")
		default:
			return false
		}
	case "docker-compose":
		return args[0] == "ps"
	case "kubectl", "oc":
		return args[0] == "get"
	case "helm":
		return args[0] == "list" || args[0] == "search"
	default:
		return false
	}
}

func commandOutputFirstTerraformAllowed(args []string) bool {
	switch commandOutputFirstTerraformSubcommand(args) {
	case "plan", "init", "validate", "show", "fmt":
		return true
	default:
		return false
	}
}

func commandOutputFirstTerraformSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return ""
		}
		if commandOutputFirstTerraformOptionConsumesValue(arg) {
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

func commandOutputFirstTerraformOptionConsumesValue(arg string) bool {
	switch arg {
	case "-chdir", "-var", "-var-file", "-state", "-state-out", "-backup", "-config", "-plugin-dir":
		return true
	default:
		return false
	}
}

func commandOutputFirstKnownJSONOutputAllowed(command string, args []string) bool {
	switch command {
	case "go":
		sub := commandOutputFirstGoSubcommand(args)
		return (sub == "env" || sub == "list" || sub == "version") && commandOutputFirstHasJSONMode(args)
	case "cargo":
		return commandOutputFirstCargoSubcommand(args) == "metadata"
	case "kubectl", "oc":
		return commandOutputFirstFirstNonOption(args) == "get" && commandOutputFirstHasJSONMode(args)
	case "docker", "podman", "nerdctl":
		return commandOutputFirstContainerJSONAllowed(command, args)
	case "docker-compose":
		return len(args) > 0 && args[0] == "config" && commandOutputFirstHasJSONMode(args)
	case "terraform", "tofu", "tf":
		sub := commandOutputFirstTerraformSubcommand(args)
		return (sub == "show" || sub == "output") && commandOutputFirstHasJSONMode(args)
	case "npm", "pnpm", "yarn", "bun":
		return commandOutputFirstPackageJSONAllowed(command, args)
	default:
		return false
	}
}

func commandOutputFirstContainerJSONAllowed(command string, args []string) bool {
	if len(args) < 1 {
		return false
	}
	switch args[0] {
	case "inspect":
		return true
	case "compose":
		return len(args) >= 2 && args[1] == "config" && commandOutputFirstHasJSONMode(args)
	default:
		return len(args) >= 2 && args[1] == "inspect"
	}
}

func commandOutputFirstPackageJSONAllowed(command string, args []string) bool {
	if !commandOutputFirstHasJSONMode(args) {
		return false
	}
	verb, idx := packageScriptFirstCommand(args)
	if idx < 0 {
		return false
	}
	switch command {
	case "npm", "pnpm":
		return commandOutputFirstPackageJSONSubcommand(verb)
	case "yarn":
		if commandOutputFirstPackageJSONSubcommand(verb) {
			return true
		}
		return verb == "npm" && idx+1 < len(args) && commandOutputFirstPackageJSONSubcommand(args[idx+1])
	case "bun":
		return verb == "pm" && idx+1 < len(args) && commandOutputFirstPackageJSONSubcommand(args[idx+1])
	default:
		return false
	}
}

func commandOutputFirstPackageJSONSubcommand(sub string) bool {
	switch sub {
	case "fund", "info", "list", "ls", "outdated", "query", "view", "why":
		return true
	default:
		return false
	}
}

func commandOutputFirstHasJSONMode(args []string) bool {
	for i, raw := range args {
		arg := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case arg == "-json" || arg == "--json":
			return true
		case strings.HasPrefix(arg, "--json="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--json="))
			return value != "" && value != "false" && value != "0"
		case arg == "-o=json" || arg == "--output=json" || arg == "--format=json":
			return true
		case arg == "-o" || arg == "--output" || arg == "--format":
			return i+1 < len(args) && strings.EqualFold(strings.TrimSpace(args[i+1]), "json")
		}
	}
	return false
}

func commandOutputFirstVCSHostAllowed(args []string) bool {
	if len(args) == 0 {
		return false
	}
	return commandOutputFirstVCSHostJSONAllowed(args) || commandOutputFirstVCSHostListAllowed(args)
}

func commandOutputFirstVCSHostJSONAllowed(args []string) bool {
	return commandOutputFirstFirstNonOption(args) == "api" || commandOutputFirstHasJSONMode(args)
}

func commandOutputFirstVCSHostListAllowed(args []string) bool {
	nonOptions := commandOutputFirstNonOptionArgs(args)
	if len(nonOptions) < 2 {
		return false
	}
	return nonOptions[len(nonOptions)-1] == "list"
}

func commandOutputFirstNonOptionArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return nil
		}
		if commandOutputFirstGenericOptionConsumesValue(arg) || commandOutputFirstVCSOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return nil
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func commandOutputFirstVCSOptionConsumesValue(arg string) bool {
	switch arg {
	case "-R", "--repo", "--hostname", "--owner", "--org", "--limit", "-L", "--jq", "-q", "--template":
		return true
	default:
		return false
	}
}

func commandOutputFirstAwsJSONAllowed(args []string) bool {
	if len(args) == 0 {
		return false
	}
	nonOptions := commandOutputFirstAwsNonOptionArgs(args)
	if len(nonOptions) == 0 {
		return false
	}
	if nonOptions[0] == "configure" {
		return false
	}
	if len(nonOptions) >= 2 && nonOptions[0] == "sso" && nonOptions[1] == "login" {
		return false
	}
	if len(nonOptions) >= 2 && nonOptions[0] == "ecr" && nonOptions[1] == "get-login-password" {
		return false
	}
	for i, raw := range args {
		arg := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case arg == "--output":
			if i+1 >= len(args) {
				return false
			}
			value := strings.ToLower(strings.TrimSpace(args[i+1]))
			return value == "json"
		case strings.HasPrefix(arg, "--output="):
			return strings.TrimPrefix(arg, "--output=") == "json"
		}
	}
	return true
}

func commandOutputFirstAwsNonOptionArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return nil
		}
		if commandOutputFirstGenericOptionConsumesValue(arg) || commandOutputFirstAwsOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return nil
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func commandOutputFirstAwsOptionConsumesValue(arg string) bool {
	switch arg {
	case "--profile", "--region", "--endpoint-url", "--ca-bundle", "--cli-input-json", "--cli-input-yaml", "--query":
		return true
	default:
		return false
	}
}

func commandOutputFirstNetworkResponseAllowed(command string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch command {
	case "curl":
		return commandOutputFirstCurlResponseAllowed(args)
	case "wget":
		return commandOutputFirstWgetResponseAllowed(args)
	case "http", "https":
		return commandOutputFirstHTTPieResponseAllowed(args)
	default:
		return false
	}
}

func commandOutputFirstCurlResponseAllowed(args []string) bool {
	hasURL := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return false
		}
		if commandOutputFirstCurlDenyCaptureFlag(arg) {
			return false
		}
		if commandOutputFirstCurlOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return false
			}
			if arg == "--url" && commandOutputFirstNetworkURLLike(strings.TrimSpace(args[i])) {
				hasURL = true
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if commandOutputFirstNetworkURLLike(arg) {
			hasURL = true
		}
	}
	return hasURL
}

func commandOutputFirstCurlDenyCaptureFlag(arg string) bool {
	switch arg {
	case "-N", "--no-buffer", "-I", "--head", "-i", "--include", "-O", "--remote-name",
		"-J", "--remote-header-name", "--raw":
		return true
	default:
		return strings.HasPrefix(arg, "--output=") ||
			strings.HasPrefix(arg, "--upload-file=") ||
			strings.HasPrefix(arg, "-o")
	}
}

func commandOutputFirstCurlOptionConsumesValue(arg string) bool {
	switch arg {
	case "-o", "--output", "-D", "--dump-header", "-H", "--header", "-A", "--user-agent",
		"-X", "--request", "-d", "--data", "--data-raw", "--data-binary", "--data-urlencode",
		"-F", "--form", "--form-string", "-u", "--user", "--url", "--connect-to", "--resolve",
		"--cacert", "--cert", "--key", "--trace", "--trace-ascii", "--upload-file":
		return true
	default:
		return false
	}
}

func commandOutputFirstWgetResponseAllowed(args []string) bool {
	hasURL := false
	writesStdout := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return false
		}
		if commandOutputFirstWgetDenyCaptureFlag(arg) {
			return false
		}
		if commandOutputFirstWgetOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return false
			}
			if arg == "-O" || arg == "--output-document" {
				writesStdout = strings.TrimSpace(args[i]) == "-"
			}
			continue
		}
		if strings.HasSuffix(arg, "O-") && strings.HasPrefix(arg, "-") {
			writesStdout = true
			continue
		}
		if strings.HasPrefix(arg, "-O") && len(arg) > 2 {
			writesStdout = arg == "-O-"
			continue
		}
		if strings.HasPrefix(arg, "--output-document=") {
			writesStdout = strings.TrimPrefix(arg, "--output-document=") == "-"
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if commandOutputFirstNetworkURLLike(arg) {
			hasURL = true
		}
	}
	return hasURL && writesStdout
}

func commandOutputFirstWgetDenyCaptureFlag(arg string) bool {
	switch arg {
	case "-r", "--recursive", "-m", "--mirror", "--spider", "-S", "--server-response":
		return true
	default:
		return false
	}
}

func commandOutputFirstWgetOptionConsumesValue(arg string) bool {
	switch arg {
	case "-O", "--output-document", "-o", "--output-file", "-P", "--directory-prefix",
		"--header", "--user", "--password", "--post-data", "--post-file", "--method",
		"--body-data", "--body-file":
		return true
	default:
		return false
	}
}

func commandOutputFirstHTTPieResponseAllowed(args []string) bool {
	hasTarget := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return false
		}
		if commandOutputFirstHTTPieDenyCaptureFlag(arg) {
			return false
		}
		if commandOutputFirstHTTPieOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return false
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if commandOutputFirstHTTPMethod(arg) {
			continue
		}
		if commandOutputFirstNetworkURLLike(arg) || commandOutputFirstHTTPieTargetLike(arg) {
			hasTarget = true
		}
	}
	return hasTarget
}

func commandOutputFirstHTTPieDenyCaptureFlag(arg string) bool {
	switch arg {
	case "--download", "--stream", "--headers", "-h", "--print=H", "--print=h", "-pH", "-ph":
		return true
	default:
		return false
	}
}

func commandOutputFirstHTTPieOptionConsumesValue(arg string) bool {
	switch arg {
	case "--session", "--session-read-only", "--auth", "-a", "--proxy", "--verify",
		"--cert", "--cert-key", "--timeout", "--style", "--pretty", "--print", "-p",
		"--format-options":
		return true
	default:
		return false
	}
}

func commandOutputFirstHTTPMethod(arg string) bool {
	switch strings.ToUpper(arg) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

func commandOutputFirstNetworkURLLike(arg string) bool {
	return strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
}

func commandOutputFirstHTTPieTargetLike(arg string) bool {
	return strings.HasPrefix(arg, ":") ||
		strings.HasPrefix(arg, "localhost/") ||
		strings.HasPrefix(arg, "localhost:") ||
		strings.HasPrefix(arg, "127.0.0.1") ||
		strings.Contains(arg, ".")
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
	if len(stderr) != 0 {
		return nil, false
	}
	return compactCommandOutputFirstStdout(command, realBin, args, stdout, code)
}

func compactCommandOutputFirstStdout(command, realBin string, args []string, stdout []byte, code int) ([]byte, bool) {
	argv := append([]string{realBin}, args...)
	if code != 0 {
		return compactCommandOutputFirstNonzeroDiagnostic(command, args, argv, stdout)
	}
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
		case "show":
			if !commandOutputFirstGitShowMetadataOnly(args) {
				return nil, false
			}
			compacted, ok := filter.TryCompactGitShow(argv, stdout)
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
	case "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift":
		compacted, ok := filter.TryCompactSearchOutputWithOptions(argv, stdout, filter.SearchCompactOptions{
			MinRetainedPct: 100,
		})
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "fd", "fdfind", "find":
		compacted, ok := filter.TryCompactPathListOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "plocate", "locate":
		compacted, ok := filter.TryCompactSearchOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "wc":
		compacted, ok := filter.TryCompactWc(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "go":
		if commandOutputFirstKnownJSONOutputAllowed(command, args) {
			compacted, ok := filter.TryCompactKnownCLIJSONExact(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
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
		if commandOutputFirstKnownJSONOutputAllowed(command, args) {
			compacted, ok := filter.TryCompactKnownCLIJSONExact(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstPackageOutputAllowed(command, args) {
			compacted, ok := filter.TryCompactPackageOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
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
		if commandOutputFirstKnownJSONOutputAllowed(command, args) {
			compacted, ok := filter.TryCompactCargoMetadataJSON(argv, stdout)
			if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
				return out, true
			}
			compacted, ok = filter.TryCompactKnownCLIJSONExact(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
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
		case "fetch", "update":
			compacted, ok := filter.TryCompactPackageOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		default:
			return nil, false
		}
	case "pytest", "py.test", "python", "python3", "uv", "poetry":
		if commandOutputFirstPackageOutputAllowed(command, args) {
			compacted, ok := filter.TryCompactPackageOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		compacted, ok := filter.TryCompactTestOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "pip", "pip3":
		compacted, ok := filter.TryCompactPackageOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "dotnet", "dotnet.exe":
		compacted, ok := filter.TryCompactDotnet(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "docker", "podman", "nerdctl", "docker-compose", "kubectl", "oc", "helm":
		if commandOutputFirstKnownJSONOutputAllowed(command, args) {
			compacted, ok := filter.TryCompactKubectlJSON(argv, stdout)
			if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
				return out, true
			}
			compacted, ok = filter.TryCompactKnownCLIJSONExact(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		compacted, ok := filter.TryCompactContainerOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "terraform", "tofu", "tf":
		if commandOutputFirstKnownJSONOutputAllowed(command, args) {
			compacted, ok := filter.TryCompactTerraformShowJSON(argv, stdout)
			if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
				return out, true
			}
			compacted, ok = filter.TryCompactKnownCLIJSONExact(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		switch commandOutputFirstTerraformSubcommand(args) {
		case "plan":
			compacted, ok := filter.TryCompactTerraformPlan(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "init":
			compacted, ok := filter.TryCompactTerraformInit(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "validate":
			compacted, ok := filter.TryCompactTerraformValidate(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "show":
			compacted, ok := filter.TryCompactTerraformShow(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "fmt":
			compacted, ok := filter.TryCompactFormatOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		default:
			return nil, false
		}
	case "gh", "glab":
		compacted, ok := filter.TryCompactVCSHostJSONExact(argv, stdout)
		if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
			return out, true
		}
		if commandOutputFirstVCSHostListAllowed(args) {
			if command == "gh" {
				compacted, ok = filter.TryCompactGhList(argv, stdout)
			} else {
				compacted, ok = filter.TryCompactGlabList(argv, stdout)
			}
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		return nil, false
	case "aws":
		compacted, ok := filter.TryCompactAwsJSONExact(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "jq":
		compacted, ok := filter.TryCompactJQJSONExact(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "curl", "wget", "http", "https":
		compacted, ok := filter.TryCompactNetworkResponse(argv, stdout)
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

func compactCommandOutputFirstStreams(command, realBin string, args []string, stdout, stderr []byte, code int) (commandOutputFirstCompaction, bool) {
	if code != 0 && len(stdout) == 0 && len(stderr) != 0 {
		argv := append([]string{realBin}, args...)
		compacted, ok := compactCommandOutputFirstNonzeroDiagnostic(command, args, argv, stderr)
		if ok {
			return commandOutputFirstCompaction{stream: "stderr", raw: stderr, compacted: compacted}, true
		}
	}
	if len(stdout) != 0 && len(stderr) != 0 {
		if compacted, ok := compactCommandOutputFirstStdout(command, realBin, args, stdout, code); ok {
			return commandOutputFirstMixedCompaction("stdout", stdout, stderr, compacted)
		}
		if code != 0 {
			argv := append([]string{realBin}, args...)
			compacted, ok := compactCommandOutputFirstNonzeroDiagnostic(command, args, argv, stderr)
			if ok {
				return commandOutputFirstMixedCompaction("stderr", stdout, stderr, compacted)
			}
		}
	}
	compacted, ok := compactCommandOutputFirst(command, realBin, args, stdout, stderr, code)
	if !ok {
		return commandOutputFirstCompaction{}, false
	}
	return commandOutputFirstCompaction{stream: "stdout", raw: stdout, compacted: compacted}, true
}

func commandOutputFirstMixedCompaction(stream string, stdout, stderr, compacted []byte) (commandOutputFirstCompaction, bool) {
	accountingRaw := append(append([]byte(nil), stdout...), stderr...)
	switch stream {
	case "stdout":
		accountingCompacted := append(append([]byte(nil), compacted...), stderr...)
		if _, ok := commandOutputFirstPositiveCompaction(accountingCompacted, true, accountingRaw); !ok {
			return commandOutputFirstCompaction{}, false
		}
		return commandOutputFirstCompaction{
			stream:            "stdout",
			raw:               stdout,
			compacted:         compacted,
			passthroughStderr: stderr,
			accountingRaw:     accountingRaw,
		}, true
	case "stderr":
		accountingCompacted := append(append([]byte(nil), stdout...), compacted...)
		if _, ok := commandOutputFirstPositiveCompaction(accountingCompacted, true, accountingRaw); !ok {
			return commandOutputFirstCompaction{}, false
		}
		return commandOutputFirstCompaction{
			stream:            "stderr",
			raw:               stderr,
			compacted:         compacted,
			passthroughStdout: stdout,
			accountingRaw:     accountingRaw,
		}, true
	default:
		return commandOutputFirstCompaction{}, false
	}
}

func compactCommandOutputFirstNonzeroDiagnostic(command string, args, argv []string, stdout []byte) ([]byte, bool) {
	if commandOutputFirstFocusedLintDiagnosticAllowed(command, args) {
		compacted, ok := filter.TryCompactLintOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	}
	if commandOutputFirstEslintStylishDiagnosticAllowed(command, args) {
		compacted, ok := filter.TryCompactEslintStylish(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	}
	if commandOutputFirstMypyDiagnosticAllowed(command, args) {
		compacted, ok := filter.TryCompactMypyDiagnostics(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	}
	if commandOutputFirstStructuredDiagnosticAllowed(command, args) {
		compacted, ok := filter.ParseFailures(argv, string(stdout))
		return commandOutputFirstPositiveCompaction([]byte(compacted), ok, stdout)
	}
	return nil, false
}

func commandOutputFirstStructuredDiagnosticAllowed(command string, args []string) bool {
	switch command {
	case "git", "rg", "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift",
		"fd", "fdfind", "find", "plocate", "locate", "wc",
		"docker", "podman", "nerdctl", "docker-compose", "kubectl", "oc", "helm",
		"terraform", "tofu", "tf", "gh", "glab", "aws", "jq", "curl", "wget", "http", "https":
		return false
	case "go":
		switch commandOutputFirstGoSubcommand(args) {
		case "test", "build":
			return true
		default:
			return false
		}
	case "cargo":
		switch commandOutputFirstCargoSubcommand(args) {
		case "build", "check", "clippy":
			return true
		default:
			return false
		}
	case "pytest", "py.test", "python", "python3", "uv", "poetry":
		return commandOutputFirstPythonTestAllowed(command, args) ||
			commandOutputFirstPythonMypyAllowed(command, args) ||
			commandOutputFirstPythonModuleLintAllowed(command, args)
	case "npm", "pnpm", "yarn", "bun":
		return commandOutputFirstPackageScriptAllowed(command, args)
	case "npx":
		tool, toolArgs, ok := commandOutputFirstNpxTool(args)
		return ok && commandOutputFirstStructuredDiagnosticAllowed(tool, toolArgs)
	default:
		return commandOutputFirstDirectBuildAllowed(command, args) ||
			commandOutputFirstDirectTestAllowed(command, args) ||
			commandOutputFirstDirectLintAllowed(command, args) ||
			commandOutputFirstDirectFormatAllowed(command, args)
	}
}

func commandOutputFirstFocusedLintDiagnosticAllowed(command string, args []string) bool {
	if commandOutputFirstFocusedLintCommand(command) {
		return true
	}
	if command != "npx" {
		return false
	}
	tool, _, ok := commandOutputFirstNpxTool(args)
	return ok && commandOutputFirstFocusedLintCommand(tool)
}

func commandOutputFirstFocusedLintCommand(command string) bool {
	switch command {
	case "golangci-lint", "staticcheck", "revive", "errcheck", "ineffassign",
		"nilaway", "unparam", "misspell", "gocyclo", "forbidigo", "prealloc":
		return true
	default:
		return false
	}
}

func commandOutputFirstEslintStylishDiagnosticAllowed(command string, args []string) bool {
	if command == "eslint" {
		return true
	}
	if command != "npx" {
		return false
	}
	tool, _, ok := commandOutputFirstNpxTool(args)
	return ok && tool == "eslint"
}

func commandOutputFirstMypyDiagnosticAllowed(command string, args []string) bool {
	if command == "mypy" {
		return true
	}
	if commandOutputFirstPythonMypyAllowed(command, args) {
		return true
	}
	if command != "npx" {
		return false
	}
	tool, _, ok := commandOutputFirstNpxTool(args)
	return ok && tool == "mypy"
}

func commandOutputFirstPythonMypyAllowed(command string, args []string) bool {
	switch command {
	case "python", "python3":
		return commandOutputFirstPythonModule(args) == "mypy"
	default:
		return false
	}
}

func commandOutputFirstPythonModuleLintAllowed(command string, args []string) bool {
	switch command {
	case "python", "python3":
	default:
		return false
	}
	switch commandOutputFirstPythonModule(args) {
	case "pylint", "flake8", "bandit", "semgrep", "djlint", "yamllint":
		return true
	case "sqlfluff":
		return commandOutputFirstArgsContain(args, "lint")
	default:
		return false
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

func archiveCommandOutputFirstCompaction(command string, args []string, stream string, raw, compacted []byte) ([]byte, bool) {
	if len(raw) == 0 || len(compacted) == 0 {
		return nil, false
	}
	home, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, false
	}
	label := commandOutputFirstLabel(command, args)
	entry, err := contentarchive.Put(contentarchive.DefaultDir(home), contentarchive.Input{
		SubLayer: "command_output_first",
		Original: string(raw),
		Preview:  label,
	}, contentarchive.Limits{})
	if err != nil || entry == nil || strings.TrimSpace(entry.URI) == "" {
		return nil, false
	}
	recoverable := []byte(strings.TrimRight(string(compacted), "\n") + commandOutputFirstArchiveMarker(entry.URI, stream))
	return commandOutputFirstPositiveCompaction(recoverable, true, raw)
}

func commandOutputFirstArchiveMarker(uri string, stream string) string {
	uri = strings.TrimSpace(uri)
	if stream == "stderr" {
		return "\n[archive " + uri + "; stream=stderr; recover: slimference expand URI]\n"
	}
	return "\n[archive " + uri + "; recover: slimference expand URI]\n"
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

func commandOutputFirstGitShowMetadataOnly(args []string) bool {
	modes := 0
	for _, arg := range args {
		switch {
		case arg == "--stat", strings.HasPrefix(arg, "--stat="), arg == "--name-only", arg == "--name-status":
			modes++
		case arg == "-p", arg == "--patch", arg == "--patch-with-stat", arg == "--raw",
			arg == "--numstat", arg == "--shortstat", arg == "-z", arg == "--word-diff",
			strings.HasPrefix(arg, "--word-diff="), strings.HasPrefix(arg, "-U"),
			strings.HasPrefix(arg, "--unified="):
			return false
		}
	}
	return modes == 1
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

func commandOutputFirstLocateAllowed(args []string) bool {
	hasPattern := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return false
		}
		if arg == "--" {
			for _, rest := range args[i+1:] {
				if strings.TrimSpace(rest) == "" {
					return false
				}
				hasPattern = true
			}
			return hasPattern
		}
		if commandOutputFirstLocateDenyFlag(arg) {
			return false
		}
		if commandOutputFirstLocateValueFlag(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return false
			}
			continue
		}
		if commandOutputFirstLocateInlineValueFlag(arg) || commandOutputFirstLocateBoolFlag(arg) {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return false
		}
		hasPattern = true
	}
	return hasPattern
}

func commandOutputFirstLocateBoolFlag(arg string) bool {
	switch arg {
	case "-i", "--ignore-case", "-b", "--basename", "-e", "--existing",
		"-L", "--follow", "-P", "--nofollow", "-r", "--regex", "--regexp",
		"-w", "--wholename", "-A", "--all":
		return true
	default:
		return false
	}
}

func commandOutputFirstLocateValueFlag(arg string) bool {
	switch arg {
	case "-d", "--database", "-l", "--limit":
		return true
	default:
		return false
	}
}

func commandOutputFirstLocateInlineValueFlag(arg string) bool {
	switch {
	case strings.HasPrefix(arg, "--database="), strings.HasPrefix(arg, "--limit="):
		_, value, _ := strings.Cut(arg, "=")
		return strings.TrimSpace(value) != ""
	case strings.HasPrefix(arg, "-d"), strings.HasPrefix(arg, "-l"):
		return len(arg) > 2 && strings.TrimSpace(arg[2:]) != ""
	default:
		return false
	}
}

func commandOutputFirstLocateDenyFlag(arg string) bool {
	switch arg {
	case "-0", "--null", "-c", "--count", "-S", "--statistics", "-h", "--help", "-V", "--version":
		return true
	default:
		return false
	}
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
	case "test", "llvm-cov", "build", "check", "doc", "clippy", "audit", "fetch", "update":
		return true
	case "nextest":
		return idx+1 < len(args) && strings.TrimSpace(args[idx+1]) == "run"
	default:
		return false
	}
}

func commandOutputFirstPackageOutputAllowed(command string, args []string) bool {
	switch command {
	case "npm":
		verb, idx := packageScriptFirstCommand(args)
		switch verb {
		case "install", "ci", "update":
			return idx >= 0 && commandOutputFirstNpmInstallArgsAllowed(args[idx+1:])
		case "audit":
			return idx >= 0 && commandOutputFirstPackageAuditJSONArgsAllowed(args[idx+1:])
		default:
			return false
		}
	case "pnpm":
		verb, idx := packageScriptFirstCommand(args)
		switch verb {
		case "install", "ci", "update":
			return idx >= 0 && commandOutputFirstPnpmInstallArgsAllowed(args[idx+1:])
		case "audit":
			return idx >= 0 && commandOutputFirstPackageAuditJSONArgsAllowed(args[idx+1:])
		default:
			return false
		}
	case "yarn":
		verb, idx := packageScriptFirstCommand(args)
		switch verb {
		case "install", "upgrade":
			return idx >= 0 && commandOutputFirstYarnInstallArgsAllowed(args[idx+1:])
		default:
			return false
		}
	case "bun":
		verb, idx := packageScriptFirstCommand(args)
		if verb != "install" || idx < 0 {
			return false
		}
		return commandOutputFirstBunInstallArgsAllowed(args[idx+1:])
	case "poetry":
		verb, idx := packageScriptFirstCommand(args)
		return verb == "install" && idx >= 0
	case "uv":
		verb, idx := packageScriptFirstCommand(args)
		if verb == "sync" {
			return idx >= 0
		}
		return verb == "pip" && idx+2 < len(args) && args[idx+1] == "install"
	case "pip", "pip3":
		verb, idx := packageScriptFirstCommand(args)
		return verb == "install" && idx >= 0
	default:
		return false
	}
}

func commandOutputFirstNpmInstallArgsAllowed(args []string) bool {
	for i := 0; i < len(args); i++ {
		lower := strings.ToLower(strings.TrimSpace(args[i]))
		switch lower {
		case "--dry-run", "--package-lock-only", "--json", "--parseable", "--porcelain", "--verbose", "-d", "-dd", "-ddd":
			return false
		case "--loglevel":
			if i+1 >= len(args) || commandOutputFirstNpmLogLevelUnsafe(args[i+1]) {
				return false
			}
			i++
		default:
			if strings.HasPrefix(lower, "--loglevel=") && commandOutputFirstNpmLogLevelUnsafe(strings.TrimPrefix(lower, "--loglevel=")) {
				return false
			}
		}
	}
	return true
}

func commandOutputFirstNpmLogLevelUnsafe(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "verbose", "silly":
		return true
	default:
		return false
	}
}

func commandOutputFirstPnpmInstallArgsAllowed(args []string) bool {
	for i := 0; i < len(args); i++ {
		lower := strings.ToLower(strings.TrimSpace(args[i]))
		switch lower {
		case "--ignore-scripts", "--frozen-lockfile", "--prefer-frozen-lockfile",
			"--prod", "--production", "-p", "--dev", "-d", "--no-optional",
			"--offline", "--prefer-offline", "--ignore-workspace", "--no-color",
			"--color=false", "--reporter=append-only", "--reporter=default":
			continue
		case "--reporter":
			if i+1 >= len(args) {
				return false
			}
			next := strings.ToLower(strings.TrimSpace(args[i+1]))
			if next != "append-only" && next != "default" {
				return false
			}
			i++
		default:
			return false
		}
	}
	return true
}

func commandOutputFirstYarnInstallArgsAllowed(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--non-interactive", "--no-progress", "--frozen-lockfile",
			"--pure-lockfile", "--prefer-offline", "--offline", "--production",
			"--prod", "--ignore-optional", "--ignore-engines", "--no-bin-links",
			"--check-files", "--no-default-rc", "--no-node-version-check":
			continue
		default:
			return false
		}
	}
	return true
}

func commandOutputFirstBunInstallArgsAllowed(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--ignore-scripts", "--no-progress", "--production", "-p", "--frozen-lockfile", "--yarn", "-y":
			continue
		default:
			return false
		}
	}
	return true
}

func commandOutputFirstPackageAuditJSONArgsAllowed(args []string) bool {
	hasJSON := false
	for _, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		switch lower {
		case "--json", "--json=true", "--json=1":
			hasJSON = true
		case "--json=false", "--json=0":
			return false
		}
	}
	return hasJSON
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

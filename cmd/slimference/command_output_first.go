package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/readcache"
)

const (
	commandOutputFirstDisableEnv             = "SLIMFERENCE_COMMAND_OUTPUT_FIRST_DISABLE"
	commandOutputFirstActiveEnv              = "SLIMFERENCE_COMMAND_OUTPUT_FIRST"
	commandOutputFirstSessionEnv             = "SLIMFERENCE_COMMAND_OUTPUT_FIRST_SESSION"
	commandOutputFirstObservationScope       = "command_output_first"
	commandOutputFirstObservationMinTokens   = 50
	commandOutputFirstObservationFullPass    = "full_pass"
	commandOutputFirstObservationArchiveFail = "archive_unavailable"
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

// applyCommandOutputFirstEnvToList merges the command-output-first shim
// environment into an existing env list for the long-running Codex Desktop
// app-server mediated path (codex_desktop_app_server_shim.go). The CLI helper
// maybeApplyCommandOutputFirstEnv wraps a single synchronous command; the
// Desktop app-server instead spawns its own `bash -lc` tool children, which
// inherit the shim transparently via the prepended PATH and BASH_ENV — the same
// archive-backed, byte-equal-fail-open output compaction (§10.2), now on the
// Desktop transport. The returned cleanup func removes the temp shim dir and
// must run only after the app-server process exits. Honors the same disable
// escape hatch as the CLI path. Fail-open: on any setup failure the env is
// returned unchanged with a no-op cleanup.
func applyCommandOutputFirstEnvToList(env []string) ([]string, func()) {
	if os.Getenv(commandOutputFirstDisableEnv) == "1" {
		return env, func() {}
	}
	cofEnv, cleanup, ok := prepareCommandOutputFirstEnv()
	if !ok {
		return env, cleanup
	}
	return upsertEnvAssignments(env, cofEnv), cleanup
}

// upsertEnvAssignments overrides any existing KEY=value entries in env with the
// matching entries from overrides, appending keys not already present. Existing
// duplicate keys keep exec's last-wins semantics (the last matching index is
// overridden). The input slice is not mutated.
func upsertEnvAssignments(env []string, overrides []string) []string {
	out := append([]string(nil), env...)
	idx := make(map[string]int, len(out))
	for i, kv := range out {
		if k, _, ok := strings.Cut(kv, "="); ok {
			idx[k] = i
		}
	}
	for _, kv := range overrides {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			out = append(out, kv)
			continue
		}
		if i, hit := idx[k]; hit {
			out[i] = kv
			continue
		}
		idx[k] = len(out)
		out = append(out, kv)
	}
	return out
}

func commandOutputFirstModeEnabled(mode string) bool {
	switch mode {
	case "proxied", "proxied-wss", "proxied-wss-bridge", "transparent-proxied":
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
	sessionID := "cof-" + filepath.Base(dir)
	shims := 0
	for _, command := range []string{
		"cat", "head", "sed", "awk",
		"git", "rg", "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift",
		"go", "npm", "pnpm", "yarn", "bun", "cargo",
		"pytest", "py.test", "python", "python3", "uv", "poetry",
		"pip", "pip3", "pipenv", "composer", "mix", "gem",
		"fd", "fdfind", "find", "plocate", "locate", "wc", "ls", "tree",
		"make", "gmake", "cmake", "ninja", "npx", "tsc", "next", "vite",
		"webpack", "webpack-cli", "pre-commit", "ruff", "pyright",
		"basedpyright", "stylelint", "eslint", "prettier", "mypy",
		"gofmt", "rustfmt", "black", "isort", "clang-format",
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
		"nox", "tox", "hatch", "ruby", "bundle", "rspec", "rake", "rails", "dart", "flutter",
		"gradle", "sbt", "mill",
		"tsup", "rspack", "parcel", "rollup", "esbuild", "mvn", "mvnw",
		"dotnet", "dotnet.exe",
		"gradlew", "meson", "zig", "wasm-pack", "bazel", "bazelisk",
		"swift", "buf", "ko", "moon", "pack",
		"docker", "podman", "nerdctl", "docker-compose", "kubectl", "oc", "helm",
		"terraform", "tofu", "tf", "gh", "glab", "aws", "jq",
		"curl", "wget", "http", "https",
		"journalctl", "tail",
		"psql", "mysql", "mariadb", "sqlite", "sqlite3", "duckdb",
	} {
		realBin, err := exec.LookPath(command)
		if err != nil || strings.TrimSpace(realBin) == "" {
			continue
		}
		if err := writeCommandOutputFirstShim(filepath.Join(dir, command), self, realBin, command, sessionID); err != nil {
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
		"export " + commandOutputFirstSessionEnv + "=" + shellQuote(sessionID) + "\n" +
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
		commandOutputFirstSessionEnv + "=" + sessionID,
	}, cleanup, true
}

func writeCommandOutputFirstShim(path, slimferenceBin, realBin, command, sessionID string) error {
	// Embed the sessionID directly in the shim script so it survives
	// Codex's shell_environment_policy.include_only filtering. Without this,
	// SLIMFERENCE_COMMAND_OUTPUT_FIRST_SESSION is stripped from the shell
	// environment and recordCommandOutputFirstSidecar silently writes nothing.
	script := "#!/bin/sh\n" +
		"export " + commandOutputFirstActiveEnv + "=1\n" +
		"export " + commandOutputFirstSessionEnv + "=" + shellQuote(sessionID) + "\n" +
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
			recordCommandOutputFirstObservation(cfg.command, childArgs, rawOut, rawErr, commandOutputFirstObservationArchiveFail)
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
	recordCommandOutputFirstObservation(cfg.command, childArgs, rawOut, rawErr, commandOutputFirstObservationFullPass)
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
	for i := range args {
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
	case "cat", "head", "sed", "awk":
		return commandOutputFirstReadAllowed(command, args)
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
		case "log":
			return commandOutputFirstGitLogMetadataOnly(args)
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
	case "ls":
		return commandOutputFirstLsLongAllowed(args)
	case "tree":
		return commandOutputFirstTreeAllowed(args)
	case "go":
		if commandOutputFirstKnownJSONOutputAllowed(command, args) {
			return true
		}
		switch commandOutputFirstGoSubcommand(args) {
		case "test", "build", "fmt", "vet":
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
	case "pipenv", "composer", "mix", "gem":
		return commandOutputFirstPackageOutputAllowed(command, args)
	case "bundle":
		return commandOutputFirstPackageOutputAllowed(command, args) ||
			commandOutputFirstBundleExecRubyTestAllowed(args)
	case "docker", "podman", "nerdctl", "docker-compose", "kubectl", "oc", "helm":
		if commandOutputFirstLogDuplicateAllowed(command, args) {
			return true
		}
		return commandOutputFirstContainerStatusAllowed(command, args) ||
			commandOutputFirstKnownJSONOutputAllowed(command, args)
	case "journalctl", "tail":
		return commandOutputFirstLogDuplicateAllowed(command, args)
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
	case "psql", "mysql", "mariadb", "sqlite", "sqlite3", "duckdb":
		return commandOutputFirstSQLShellAllowed(command, args)
	case "history", "fc", "dmesg", "mount",
		"base64", "base32",
		"md5sum", "sha256sum", "sha1sum", "sha512sum", "shasum", "b2sum", "cksum",
		"objdump", "readelf", "nm", "strings",
		"strace", "ltrace",
		"vmstat", "iostat", "mpstat", "sar",
		"ip", "ifconfig",
		"cloc", "scc", "tokei", "loc",
		"systemctl",
		"rustc",
		"tcpdump", "tshark",
		"perf":
		return true
	default:
		return commandOutputFirstDirectBuildAllowed(command, args) ||
			commandOutputFirstDirectTestAllowed(command, args) ||
			commandOutputFirstDirectLintAllowed(command, args) ||
			commandOutputFirstDirectFormatAllowed(command, args)
	}
}

func commandOutputFirstReadAllowed(command string, args []string) bool {
	argv := append([]string{command}, args...)
	req, ok := filter.ReadRequestFromArgv(argv)
	return ok && commandOutputFirstReadPathAllowed(req.Path)
}

func commandOutputFirstReadPathAllowed(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".log":
		return false
	default:
		return true
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
	case "gcc", "g++", "clang", "clang++", "cc", "c++":
		return !commandOutputFirstCompilerArgsUnsafeLongRunning(args)
	case "javac":
		return !commandOutputFirstBuildArgsUnsafeLongRunning(args)
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
	case "ruby":
		return commandOutputFirstRubyMinitestAllowed(args)
	case "bundle":
		return commandOutputFirstBundleExecRubyTestAllowed(args)
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

func commandOutputFirstRubyMinitestAllowed(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return false
		}
		switch arg {
		case "-e", "--eval", "-":
			return false
		case "--":
			if i+1 >= len(args) {
				return false
			}
			return commandOutputFirstRubyTestFile(args[i+1])
		}
		if commandOutputFirstRubyOptionConsumesValue(arg) {
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return false
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return commandOutputFirstRubyTestFile(arg)
	}
	return false
}

func commandOutputFirstRubyOptionConsumesValue(arg string) bool {
	if strings.HasPrefix(arg, "-I") && len(arg) > 2 {
		return false
	}
	if strings.HasPrefix(arg, "-r") && len(arg) > 2 {
		return false
	}
	switch arg {
	case "-I", "-r", "-C", "-E", "--encoding":
		return true
	default:
		return false
	}
}

func commandOutputFirstRubyTestFile(arg string) bool {
	path := filepath.ToSlash(strings.TrimSpace(arg))
	if !strings.HasSuffix(path, "_test.rb") {
		return false
	}
	return strings.HasPrefix(path, "test/") ||
		strings.HasPrefix(path, "./test/") ||
		strings.Contains(path, "/test/")
}

func commandOutputFirstBundleExecRubyTestAllowed(args []string) bool {
	if len(args) < 2 || strings.TrimSpace(args[0]) != "exec" {
		return false
	}
	tool := strings.TrimSpace(args[1])
	toolArgs := args[2:]
	switch tool {
	case "rspec":
		return true
	case "rake":
		sub := commandOutputFirstFirstNonOption(toolArgs)
		return sub == "test" || sub == "spec"
	case "rails":
		return commandOutputFirstFirstNonOption(toolArgs) == "test"
	case "ruby":
		return commandOutputFirstRubyMinitestAllowed(toolArgs)
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
	case "gofmt":
		return commandOutputFirstArgsContain(args, "-l") &&
			!commandOutputFirstArgsContain(args, "-w")
	case "rustfmt":
		return commandOutputFirstArgsContain(args, "--check")
	case "black":
		return commandOutputFirstArgsContain(args, "--check")
	case "isort":
		return commandOutputFirstArgsContain(args, "--check", "--check-only", "-c")
	case "clang-format":
		return commandOutputFirstArgsContain(args, "--dry-run", "-n")
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
		if slices.Contains(wants, arg) {
			return true
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

// commandOutputFirstCompilerArgsUnsafeLongRunning checks for flags that make
// C/C++ compilers (gcc, clang, etc.) unsafe for command-output-first compaction.
// Unlike the generic build helper, this does NOT match "-w" (which means
// "suppress warnings" for C/C++ compilers, not "watch mode").
func commandOutputFirstCompilerArgsUnsafeLongRunning(args []string) bool {
	return commandOutputFirstArgsContain(args, "--watch", "--continuous", "watch", "dev", "serve", "start")
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

func commandOutputFirstLogDuplicateAllowed(command string, args []string) bool {
	if len(args) == 0 || commandOutputFirstArgsContain(args, "-f", "--follow") {
		return false
	}
	switch command {
	case "docker", "podman", "nerdctl":
		return args[0] == "logs" && commandOutputFirstLogArgsFinite(args[1:])
	case "kubectl", "oc":
		return args[0] == "logs" && commandOutputFirstLogArgsFinite(args[1:])
	case "journalctl":
		return commandOutputFirstLogArgsFinite(args)
	case "tail":
		return commandOutputFirstTailLogAllowed(args)
	default:
		return false
	}
}

func commandOutputFirstLogArgsFinite(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return false
		}
		switch {
		case arg == "--":
			continue
		case arg == "-f" || arg == "--follow":
			return false
		case strings.HasPrefix(arg, "--follow="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--follow="))
			if value != "" && value != "false" && value != "0" {
				return false
			}
		}
	}
	return true
}

func commandOutputFirstTailLogAllowed(args []string) bool {
	if !commandOutputFirstLogArgsFinite(args) || len(args) == 0 {
		return false
	}
	target := strings.TrimSpace(args[len(args)-1])
	if target == "" || strings.HasPrefix(target, "-") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(target))
	return ext == ".log" || ext == ".out" || ext == ".err"
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

func commandOutputFirstSQLShellAllowed(command string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch command {
	case "psql":
		return commandOutputFirstOptionWithValuePresent(args, "-c", "--command", "-f", "--file")
	case "mysql", "mariadb":
		return commandOutputFirstOptionWithValuePresent(args, "-e", "--execute")
	case "sqlite", "sqlite3":
		return commandOutputFirstSQLPositionalQueryPresent(args, "-cmd", "-init", "-separator", "-nullvalue")
	case "duckdb":
		return commandOutputFirstOptionWithValuePresent(args, "-c", "--command") ||
			commandOutputFirstSQLPositionalQueryPresent(args, "-c", "--command")
	default:
		return false
	}
}

func commandOutputFirstSQLPositionalQueryPresent(args []string, optionValueNames ...string) bool {
	nonOptions := 0
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			return false
		}
		consumesValue := false
		for _, name := range optionValueNames {
			if arg == name {
				consumesValue = true
				break
			}
			if strings.HasPrefix(arg, name+"=") {
				consumesValue = false
				break
			}
		}
		if consumesValue {
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return false
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		nonOptions++
	}
	return nonOptions >= 2
}

func commandOutputFirstOptionWithValuePresent(args []string, names ...string) bool {
	for i, raw := range args {
		arg := strings.TrimSpace(raw)
		if arg == "" {
			return false
		}
		for _, name := range names {
			if arg == name {
				return i+1 < len(args) && strings.TrimSpace(args[i+1]) != ""
			}
			if after, ok := strings.CutPrefix(arg, name+"="); ok {
				return strings.TrimSpace(after) != ""
			}
		}
	}
	return false
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
		if after, ok := strings.CutPrefix(arg, "--output-document="); ok {
			writesStdout = after == "-"
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
	for i := range args {
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
	for i := range args {
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
	case "cat", "head", "sed", "awk", "bat", "batcat":
		// Try JSON minification first — cat of JSON files (package.json,
		// tsconfig.json, etc.) is common and minification + schema
		// extraction can produce large savings on pretty-printed JSON.
		if compacted, ok := filter.TryCompactJSONMinify(stdout); ok {
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		compacted, ok := compactCommandOutputFirstReadDelta(command, args, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "git":
		switch commandOutputFirstGitSubcommand(args) {
		case "status":
			compacted, ok := filter.TryCompactGitStatus(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "diff":
			// Try compacting all git diff output, not just --stat/--name-only.
			// compactGitDiff strips context lines from unified diffs, keeping
			// +/- lines and hunk headers. For --stat/--name-only/--name-status,
			// the dedicated path-list/stat compactors are used.
			compacted, ok := filter.TryCompactGitDiff(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "show":
			// Try compacting all git show output. compactGitShow extracts
			// the commit header + stat and calls compactGitDiff on the diff
			// section, stripping context lines.
			compacted, ok := filter.TryCompactGitShow(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "log":
			// Try compacting all git log output. compactGitLog extracts
			// commit hash + subject + stat per entry, stripping full
			// commit messages and diffs.
			compacted, ok := filter.TryCompactGitLog(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "ls-files":
			compacted, ok := filter.TryCompactGitLsFiles(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "grep":
			compacted, ok := filter.TryCompactSearchOutputWithOptions(argv, stdout, filter.SearchCompactOptions{
				MinRetainedPct: 100,
			})
			if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
				return out, true
			}
			// Archive-backed search compaction for git grep.
			compacted, ok = filter.TryCompactSearchOutputArchived(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		default:
			return nil, false
		}
	case "rg":
		// rg --json produces NDJSON events that are not handled by the
		// plain-text search compaction path. Try the JSON archived compactor
		// first — it strips the JSON envelope and produces the same archived
		// summary format as the plain-text path, with archive recovery.
		compacted, ok := filter.TryCompactRipgrepJSONArchived(argv, stdout)
		if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
			return out, true
		}
		compacted, ok = filter.TryCompactPathListOutput(argv, stdout)
		if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
			return out, true
		}
		compacted, ok = filter.TryCompactSearchOutputWithOptions(argv, stdout, filter.SearchCompactOptions{
			MinRetainedPct: 100,
		})
		if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
			return out, true
		}
		// Archive-backed search compaction: truncate match content and cap
		// matches per file. The omitted bytes are recoverable via the
		// command-output-first archive marker appended by the shim.
		compacted, ok = filter.TryCompactSearchOutputArchived(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift":
		compacted, ok := filter.TryCompactSearchOutputWithOptions(argv, stdout, filter.SearchCompactOptions{
			MinRetainedPct: 100,
		})
		if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
			return out, true
		}
		// Archive-backed search compaction: truncate match content and cap
		// matches per file. The omitted bytes are recoverable via the
		// command-output-first archive marker appended by the shim.
		compacted, ok = filter.TryCompactSearchOutputArchived(argv, stdout)
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
	case "ls":
		compacted, ok := filter.TryCompactLsLong(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "tree":
		compacted, ok := filter.TryCompactTree(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "du":
		compacted, ok := filter.TryCompactDu(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "df":
		compacted, ok := filter.TryCompactDf(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "ps":
		compacted, ok := filter.TryCompactPs(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "env", "printenv":
		compacted, ok := filter.TryCompactEnv(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "xxd", "hexdump", "od":
		compacted, ok := filter.TryCompactHexDump(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "diff", "diff3":
		compacted, ok := filter.TryCompactDiff(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "lsof":
		compacted, ok := filter.TryCompactLsof(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "ss", "netstat":
		compacted, ok := filter.TryCompactNetstat(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "history", "fc":
		compacted, ok := filter.TryCompactHistory(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "dmesg":
		compacted, ok := filter.TryCompactDmesg(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "mount":
		compacted, ok := filter.TryCompactMount(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "base64", "base32":
		compacted, ok := filter.TryCompactBase64(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "md5sum", "sha256sum", "sha1sum", "sha512sum", "shasum", "b2sum", "cksum":
		compacted, ok := filter.TryCompactHashSum(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "objdump", "readelf", "nm", "strings":
		compacted, ok := filter.TryCompactObjdump(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "strace", "ltrace":
		compacted, ok := filter.TryCompactStrace(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "vmstat", "iostat", "mpstat", "sar":
		compacted, ok := filter.TryCompactVmstat(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "ip", "ifconfig":
		compacted, ok := filter.TryCompactIpAddr(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "cloc", "scc", "tokei", "loc":
		compacted, ok := filter.TryCompactCloc(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "systemctl":
		compacted, ok := filter.TryCompactSystemctl(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "rustc":
		compacted, ok := filter.TryCompactRustc(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "tcpdump", "tshark":
		compacted, ok := filter.TryCompactTcpdump(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "perf":
		compacted, ok := filter.TryCompactPerf(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "go":
		// go test -json produces verbose NDJSON events. Try the JSON
		// compactor first — it replaces all-pass output with one line
		// and extracts only failed test events + summary on failure.
		if compacted, ok := filter.TryCompactGoTestJSON(argv, stdout); ok {
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		// go list -json produces NDJSON module objects that
		// TryCompactKnownCLIJSONExact cannot handle (it expects a single
		// JSON document). Try the NDJSON compactor before the generic
		// JSON path.
		if compacted, ok := filter.TryCompactGoListJSON(argv, stdout); ok {
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
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
		case "fmt":
			compacted, ok := filter.TryCompactFormatOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		case "vet":
			compacted, ok := filter.TryCompactLintOutput(argv, stdout)
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
			compacted, ok := filter.TryCompactCargo(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
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
	case "pipenv", "composer", "mix", "gem":
		compacted, ok := filter.TryCompactPackageOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "apt", "apt-get", "yum", "dnf", "brew", "pacman":
		compacted, ok := filter.TryCompactPackageOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "bundle":
		if commandOutputFirstPackageOutputAllowed(command, args) {
			compacted, ok := filter.TryCompactPackageOutput(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		compacted, ok := filter.TryCompactRubyOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "dotnet", "dotnet.exe":
		compacted, ok := filter.TryCompactDotnet(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "docker", "podman", "nerdctl", "docker-compose", "kubectl", "oc", "helm":
		if commandOutputFirstLogDuplicateAllowed(command, args) {
			compacted, ok := filter.TryCompactLogDuplicateRuns(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		if commandOutputFirstKnownJSONOutputAllowed(command, args) {
			compacted, ok := filter.TryCompactKubectlJSON(argv, stdout)
			if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
				return out, true
			}
			compacted, ok = filter.TryCompactKnownCLIJSONExact(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		compacted, ok := filter.TryCompactContainerOutput(argv, stdout)
		if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
			return out, true
		}
		// Fallback: generic line-capping compactor
		switch command {
		case "kubectl", "oc":
			compacted, ok = filter.TryCompactKubectl(argv, stdout)
		case "helm":
			compacted, ok = filter.TryCompactHelm(argv, stdout)
		default:
			compacted, ok = filter.TryCompactDocker(argv, stdout)
		}
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
	case "journalctl", "tail":
		compacted, ok := filter.TryCompactLogDuplicateRuns(argv, stdout)
		if out, accepted := commandOutputFirstPositiveCompaction(compacted, ok, stdout); accepted {
			return out, true
		}
		if command == "journalctl" {
			compacted, ok = filter.TryCompactJournalctl(argv, stdout)
			return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
		}
		return nil, false
	case "sort", "uniq", "cut", "tr", "column", "paste", "join", "comm", "tsort":
		compacted, ok := filter.TryCompactTextUtility(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "psql", "mysql", "mariadb", "sqlite", "sqlite3", "duckdb":
		compacted, ok := filter.TryCompactPsql(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	case "ruby", "rspec", "rake":
		compacted, ok := filter.TryCompactRubyOutput(argv, stdout)
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
	if code == 0 && len(stdout) != 0 && len(stderr) == 0 {
		if compacted, ok := compactCommandOutputFirstRepeatedOutput(command, args, stdout); ok {
			return commandOutputFirstCompaction{stream: "stdout", raw: stdout, compacted: compacted}, true
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
	if compacted, ok := filter.TryCompactSARIF(argv, stdout); ok {
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	}
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
	if commandOutputFirstRubyDiagnosticAllowed(command, args) {
		compacted, ok := filter.TryCompactRubyOutput(argv, stdout)
		return commandOutputFirstPositiveCompaction(compacted, ok, stdout)
	}
	if commandOutputFirstStructuredDiagnosticAllowed(command, args) {
		compacted, ok := filter.ParseFailures(argv, string(stdout))
		return commandOutputFirstPositiveCompaction([]byte(compacted), ok, stdout)
	}
	return nil, false
}

func commandOutputFirstRubyDiagnosticAllowed(command string, args []string) bool {
	switch command {
	case "rspec":
		return true
	case "rake":
		sub := commandOutputFirstFirstNonOption(args)
		return sub == "test" || sub == "spec"
	case "ruby":
		return commandOutputFirstRubyMinitestAllowed(args)
	case "bundle":
		return commandOutputFirstBundleExecRubyTestAllowed(args)
	default:
		return false
	}
}

func commandOutputFirstStructuredDiagnosticAllowed(command string, args []string) bool {
	switch command {
	case "git", "rg", "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift",
		"fd", "fdfind", "find", "plocate", "locate", "wc", "ls",
		"docker", "podman", "nerdctl", "docker-compose", "kubectl", "oc", "helm",
		"terraform", "tofu", "tf", "gh", "glab", "aws", "jq", "curl", "wget", "http", "https":
		return false
	case "go":
		switch commandOutputFirstGoSubcommand(args) {
		case "test", "build", "vet":
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

func compactCommandOutputFirstReadDelta(command string, args []string, stdout []byte) ([]byte, bool) {
	sessionID := strings.TrimSpace(os.Getenv(commandOutputFirstSessionEnv))
	if sessionID == "" {
		return nil, false
	}
	argv := append([]string{command}, args...)
	req, ok := filter.ReadRequestFromArgv(argv)
	if !ok || !commandOutputFirstReadPathAllowed(req.Path) {
		return nil, false
	}
	home, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, false
	}
	cacheDir := readcache.DefaultDir(home)
	decision, err := readcache.EvaluateObserved(cacheDir, readcache.Request{
		SessionID: sessionID,
		TurnID:    fmt.Sprintf("cof-turn-%d", time.Now().UnixNano()),
		FilePath:  req.Path,
		Offset:    req.Offset,
		Limit:     req.Limit,
	}, string(stdout), contentarchive.DefaultDir(home), false)
	if err != nil {
		return nil, false
	}
	if err := readcache.FlushSession(cacheDir, sessionID); err != nil {
		return nil, false
	}
	if decision.Type != readcache.DecisionBlock || strings.TrimSpace(decision.Reason) == "" {
		return nil, false
	}
	return []byte(strings.TrimRight(decision.Reason, "\n") + "\n"), true
}

func compactCommandOutputFirstRepeatedOutput(command string, args []string, stdout []byte) ([]byte, bool) {
	if commandOutputFirstReadCommand(command) {
		return nil, false
	}
	sessionID := strings.TrimSpace(os.Getenv(commandOutputFirstSessionEnv))
	if sessionID == "" {
		return nil, false
	}
	home, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil, false
	}
	commandLine := commandOutputFirstCommandLine(command, args)
	cacheDir := readcache.DefaultDir(home)
	decision, err := readcache.EvaluateObservedOutput(cacheDir, readcache.OutputRequest{
		SessionID:   sessionID,
		TurnID:      fmt.Sprintf("cof-output-turn-%d", time.Now().UnixNano()),
		Key:         commandOutputFirstOutputKey(command, args),
		CommandLine: commandLine,
	}, string(stdout), contentarchive.DefaultDir(home))
	if err != nil {
		return nil, false
	}
	if err := readcache.FlushSession(cacheDir, sessionID); err != nil {
		return nil, false
	}
	if decision.Type != readcache.DecisionBlock || strings.TrimSpace(decision.Reason) == "" {
		return nil, false
	}
	return []byte(strings.TrimRight(decision.Reason, "\n") + "\n"), true
}

func commandOutputFirstReadCommand(command string) bool {
	switch command {
	case "cat", "head", "sed", "awk", "bat", "batcat":
		return true
	default:
		return false
	}
}

func commandOutputFirstOutputKey(command string, args []string) string {
	if commandOutputFirstSearchCommand(command, args) {
		return "search:" + command + "\t" + strings.Join(args, "\t")
	}
	return "command:" + commandOutputFirstCommandLine(command, args)
}

func commandOutputFirstSearchCommand(command string, args []string) bool {
	switch command {
	case "rg", "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift":
		return true
	case "git":
		return commandOutputFirstGitSubcommand(args) == "grep"
	default:
		return false
	}
}

func commandOutputFirstCommandLine(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
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
	savedTokens := inputTokens - outputTokens
	savingsPct := float64(savedTokens) * 100 / float64(inputTokens)
	_ = filter.RecordFilterRun(db, commandOutputFirstLabel(command, args), wd, inputTokens, outputTokens, savingsPct, time.Now())
	recordCommandOutputFirstSidecar(command, args, int64(inputTokens), int64(outputTokens), int64(savedTokens), rawOut, compacted)
}

// commandOutputFirstProvenanceMaxRawBytes caps how large a raw payload may be
// before its bytes are embedded in the sidecar for independent gate
// recompute. Larger payloads (e.g. huge `go test -json` dumps) only carry the
// content hash and therefore stay UNVERIFIED in the gate — they are not counted
// toward the trusted S_local number (AGENTS.md §3.8.1: no trust-without-recompute).
const commandOutputFirstProvenanceMaxRawBytes = 256 * 1024

// commandOutputFirstProvenance returns the sha256 of the raw input plus
// gzip+base64 of the raw input and compacted output, so the corpus gate can
// independently re-derive input/output/saved token counts from real bytes.
// The gzip fields are omitted when the raw payload exceeds the cap.
func commandOutputFirstProvenance(raw, compacted []byte) (sha256hex, rawGzipB64, compGzipB64 string) {
	sum := sha256.Sum256(raw)
	sha256hex = hex.EncodeToString(sum[:])
	if len(raw) == 0 || len(raw) > commandOutputFirstProvenanceMaxRawBytes {
		return sha256hex, "", ""
	}
	rawGzipB64 = gzipBase64(raw)
	compGzipB64 = gzipBase64(compacted)
	if rawGzipB64 == "" || compGzipB64 == "" {
		return sha256hex, "", ""
	}
	return sha256hex, rawGzipB64, compGzipB64
}

func gzipBase64(b []byte) string {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		_ = zw.Close()
		return ""
	}
	if err := zw.Close(); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// recordCommandOutputFirstSidecar appends one JSON line to a per-session
// command_output_first_<session>.jsonl file in ~/.slimference/analytics/.
// This sidecar is read by the corpus evaluator to count T418 savings in
// the real_current_local_savings_ratio gate. Failures are silent (fail-open).
func recordCommandOutputFirstSidecar(command string, args []string, inputTokens, outputTokens, savedTokens int64, rawBytes, compactedBytes []byte) {
	sessionID := strings.TrimSpace(os.Getenv(commandOutputFirstSessionEnv))
	if sessionID == "" {
		return
	}
	homeDir, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return
	}
	dir := filepath.Join(homeDir, ".slimference", "analytics")
	if err := osMkdirAll(dir, 0755); err != nil {
		return
	}
	path := filepath.Join(dir, "command_output_first_"+sessionID+".jsonl")
	sha256hex, rawGzipB64, compGzipB64 := commandOutputFirstProvenance(rawBytes, compactedBytes)
	row := struct {
		Timestamp        string `json:"ts"`
		Command          string `json:"command"`
		InputTokens      int64  `json:"input_tokens"`
		OutputTokens     int64  `json:"output_tokens"`
		SavedTokens      int64  `json:"saved_tokens"`
		InputSHA256      string `json:"input_sha256,omitempty"`
		RawGzipB64       string `json:"raw_gzip_b64,omitempty"`
		CompactedGzipB64 string `json:"compacted_gzip_b64,omitempty"`
	}{
		Timestamp:        time.Now().UTC().Format(time.RFC3339Nano),
		Command:          commandOutputFirstLabel(command, args),
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		SavedTokens:      savedTokens,
		InputSHA256:      sha256hex,
		RawGzipB64:       rawGzipB64,
		CompactedGzipB64: compGzipB64,
	}
	data, err := json.Marshal(row)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_ = osAppendToFile(path, data, 0644)
}

func recordCommandOutputFirstObservation(command string, args []string, rawOut, rawErr []byte, outcome string) {
	raw := append(append([]byte(nil), rawOut...), rawErr...)
	inputTokens := filter.EstimateTokensFromBytes(len(raw))
	if inputTokens < commandOutputFirstObservationMinTokens {
		return
	}
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
	_ = filter.RecordFilterObservation(db, commandOutputFirstObservationScope, commandOutputFirstLabel(command, args), wd, inputTokens, inputTokens, outcome, time.Now())
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

func commandOutputFirstGitLogMetadataOnly(args []string) bool {
	modes := 0
	for _, arg := range args {
		switch {
		case arg == "--stat", strings.HasPrefix(arg, "--stat="), arg == "--name-only", arg == "--name-status":
			modes++
		case arg == "-p", arg == "--patch", arg == "--patch-with-stat", arg == "--raw",
			arg == "--numstat", arg == "--shortstat", arg == "--oneline", arg == "--word-diff",
			strings.HasPrefix(arg, "--word-diff="), strings.HasPrefix(arg, "-U"),
			strings.HasPrefix(arg, "--unified="), strings.HasPrefix(arg, "--format="),
			strings.HasPrefix(arg, "--pretty="), arg == "--format", arg == "--pretty":
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
	for i := range args {
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

func commandOutputFirstLsLongAllowed(args []string) bool {
	argv := append([]string{"ls"}, args...)
	return filter.LsLongOutputEligibleArgv(argv)
}

func commandOutputFirstTreeAllowed(args []string) bool {
	argv := append([]string{"tree"}, args...)
	_, ok := filter.TryCompactTree(argv, []byte(treeEligibilityProbeOutput))
	return ok
}

const treeEligibilityProbeOutput = ".\n" +
	"├── src\n" +
	"│   ├── app.go\n" +
	"│   ├── app_test.go\n" +
	"│   ├── config.go\n" +
	"│   ├── router.go\n" +
	"│   └── service.go\n" +
	"└── docs\n" +
	"    └── README.md\n\n" +
	"2 directories, 6 files\n"

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
	case "pipenv":
		verb, idx := packageScriptFirstCommand(args)
		return verb == "install" && idx >= 0 && commandOutputFirstSimplePackageInstallArgsAllowed(args[idx+1:])
	case "composer":
		verb, idx := packageScriptFirstCommand(args)
		return verb == "install" && idx >= 0 && commandOutputFirstSimplePackageInstallArgsAllowed(args[idx+1:])
	case "mix":
		verb, idx := packageScriptFirstCommand(args)
		return verb == "deps.get" && idx >= 0 && commandOutputFirstSimplePackageInstallArgsAllowed(args[idx+1:])
	case "gem":
		verb, idx := packageScriptFirstCommand(args)
		return verb == "install" && idx >= 0 && commandOutputFirstSimplePackageInstallArgsAllowed(args[idx+1:])
	case "bundle":
		verb, idx := packageScriptFirstCommand(args)
		switch verb {
		case "install", "update":
			return idx >= 0 && commandOutputFirstBundleInstallArgsAllowed(args[idx+1:])
		default:
			return false
		}
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

func commandOutputFirstBundleInstallArgsAllowed(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := strings.ToLower(strings.TrimSpace(args[i]))
		if arg == "" {
			return false
		}
		switch arg {
		case "--verbose", "-v":
			return false
		case "--jobs", "-j", "--retry", "--path", "--with", "--without", "--gemfile":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return false
			}
		default:
			if strings.HasPrefix(arg, "--jobs=") ||
				strings.HasPrefix(arg, "--retry=") ||
				strings.HasPrefix(arg, "--path=") ||
				strings.HasPrefix(arg, "--with=") ||
				strings.HasPrefix(arg, "--without=") ||
				strings.HasPrefix(arg, "--gemfile=") {
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return false
			}
		}
	}
	return true
}

func commandOutputFirstSimplePackageInstallArgsAllowed(args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		if lower == "" {
			return false
		}
		switch lower {
		case "--verbose", "-v", "-vv", "-vvv", "--debug":
			return false
		default:
			if strings.HasPrefix(lower, "--verbose=") || strings.HasPrefix(lower, "--debug=") {
				return false
			}
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
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
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

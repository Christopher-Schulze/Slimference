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
	for _, command := range []string{"git", "rg", "go"} {
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
		recordCommandOutputFirstRun(cfg.command, childArgs, rawOut, compacted)
		_, _ = stdout.Write(compacted)
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
		case "diff":
			return commandOutputFirstGitDiffMetadataOnly(args)
		default:
			return false
		}
	case "rg":
		return true
	case "go":
		switch commandOutputFirstGoSubcommand(args) {
		case "test", "build":
			return true
		default:
			return false
		}
	default:
		return false
	}
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
		default:
			return nil, false
		}
	case "rg":
		compacted, ok := filter.TryCompactSearchOutputWithOptions(argv, stdout, filter.SearchCompactOptions{
			MinRetainedPct: 100,
		})
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
	default:
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
	label := "[command-output-first:" + command + "] " + command
	if len(args) > 0 {
		label += " " + strings.Join(args, " ")
	}
	_ = filter.RecordFilterRun(db, label, wd, inputTokens, outputTokens, savingsPct, time.Now())
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

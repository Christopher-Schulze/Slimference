package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const (
	codexDesktopIndicatorDisableEnv = "SLIMFERENCE_CODEX_DESKTOP_INDICATOR"
	codexDesktopIndicatorLabel      = "SLIMFERENCE ACTIVE"
)

var (
	codexDesktopIndicatorStartFn      = startCodexDesktopIndicator
	codexDesktopIndicatorExecutableFn = osExecutable
	codexDesktopIndicatorBaseEnvFn    = os.Environ
	codexDesktopIndicatorStartCmdFn   = startCodexDesktopIndicatorCommand
	codexDesktopIndicatorSupportedFn  = codexDesktopIndicatorSupported
	codexDesktopIndicatorRunWindowFn  = runCodexDesktopIndicatorWindow
	codexDesktopIndicatorCleanLabelFn = cleanCodexDesktopIndicatorLabel
)

type codexDesktopIndicatorFlags struct {
	label    string
	watchPID int
	quiet    bool
	help     bool
}

func handleCodexDesktopIndicatorCmd(args []string) {
	exitFn(runCodexDesktopIndicatorCmd(args, defaultInstallPrinter()))
}

func runCodexDesktopIndicatorCmd(args []string, p installPrinter) int {
	flags, err := parseCodexDesktopIndicatorFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "desktop-indicator: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexDesktopIndicatorHelpText)
		return 0
	}
	if !codexDesktopIndicatorSupportedFn() {
		if !flags.quiet {
			fmt.Fprintln(p.Err, "desktop-indicator: unsupported on this platform")
		}
		return 0
	}
	label := codexDesktopIndicatorCleanLabelFn(flags.label)
	if label == "" {
		label = codexDesktopIndicatorLabel
	}
	if err := codexDesktopIndicatorRunWindowFn(label, flags.watchPID); err != nil {
		fmt.Fprintf(p.Err, "desktop-indicator: %v\n", err)
		return 1
	}
	return 0
}

func parseCodexDesktopIndicatorFlags(args []string) (codexDesktopIndicatorFlags, error) {
	f := codexDesktopIndicatorFlags{label: codexDesktopIndicatorLabel}
	for _, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			f.help = true
		case arg == "--quiet":
			f.quiet = true
		case strings.HasPrefix(arg, "--label="):
			f.label = strings.TrimPrefix(arg, "--label=")
		case strings.HasPrefix(arg, "--watch-pid="):
			raw := strings.TrimPrefix(arg, "--watch-pid=")
			pid, err := strconv.Atoi(raw)
			if err != nil || pid < 0 {
				return f, fmt.Errorf("invalid --watch-pid %q", raw)
			}
			f.watchPID = pid
		default:
			return f, fmt.Errorf("unknown flag %q", arg)
		}
	}
	return f, nil
}

func maybeStartCodexDesktopIndicator(pid int, env []string) error {
	if !codexDesktopIndicatorShouldStart(env) {
		return nil
	}
	return codexDesktopIndicatorStartFn(pid, env)
}

func codexDesktopIndicatorShouldStart(env []string) bool {
	if envValue(env, codexDesktopShimActiveEnv) != "1" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(envValue(env, codexDesktopIndicatorDisableEnv))) {
	case "0", "false", "off", "no":
		return false
	}
	return codexDesktopIndicatorSupportedFn()
}

func startCodexDesktopIndicator(pid int, _ []string) error {
	if pid <= 0 {
		return fmt.Errorf("invalid Codex.app PID %d", pid)
	}
	binary, err := codexDesktopIndicatorExecutableFn()
	if err != nil {
		return fmt.Errorf("resolve slimference executable: %w", err)
	}
	args := []string{
		"desktop-indicator",
		"--quiet",
		"--watch-pid=" + strconv.Itoa(pid),
		"--label=" + codexDesktopIndicatorLabel,
	}
	cmd := newCodexDesktopIndicatorCommand(binary, args, codexDesktopIndicatorBaseEnvFn())
	return codexDesktopIndicatorStartCmdFn(cmd)
}

func newCodexDesktopIndicatorCommand(binary string, args []string, baseEnv []string) *exec.Cmd {
	cmd := exec.Command(binary, args...)
	cmd.Env = sanitizeCodexDesktopBaseEnv(baseEnv)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

func startCodexDesktopIndicatorCommand(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func cleanCodexDesktopIndicatorLabel(label string) string {
	label = strings.ToUpper(strings.TrimSpace(label))
	if len(label) > 32 {
		label = label[:32]
	}
	return label
}

const codexDesktopIndicatorHelpText = `usage: slimference desktop-indicator [--watch-pid=<pid>] [--label=<text>] [--quiet]

Hidden helper used by Slimference's scoped Codex Desktop launcher. It shows a
small patch-free macOS route indicator and exits automatically when --watch-pid
is gone. Normal Codex launches never start it.
`

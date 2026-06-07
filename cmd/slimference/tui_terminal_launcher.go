package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type tuiTerminalApp string

const (
	tuiTerminalAppleTerminal tuiTerminalApp = "Terminal"
	tuiTerminalGhostty       tuiTerminalApp = "Ghostty"
)

var tuiTerminalEnvFn = os.Getenv

func launchCodexCLIInCurrentTerminal(binary string, dir string) (string, error) {
	command := scopedCodexCLICommand(binary, dir)
	app := detectTUITerminalApp()
	switch app {
	case tuiTerminalGhostty:
		if err := launchGhosttyTab(command); err != nil {
			return "", err
		}
	case tuiTerminalAppleTerminal:
		if err := launchAppleTerminalTab(command); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported terminal app for same-app launch: TERM_PROGRAM=%q", tuiTerminalEnvFn("TERM_PROGRAM"))
	}
	return fmt.Sprintf("Codex CLI launched via Slimference transport=auto in %s (%s)", dir, app), nil
}

func scopedCodexCLICommand(binary string, dir string) string {
	return "for k in ${!CODEX_@}; do unset \"$k\"; done; cd " + shellQuote(dir) + " && " + shellQuote(binary) + " codex run --transport=auto --"
}

func detectTUITerminalApp() tuiTerminalApp {
	termProgram := strings.ToLower(strings.TrimSpace(tuiTerminalEnvFn("TERM_PROGRAM")))
	switch termProgram {
	case "ghostty":
		return tuiTerminalGhostty
	case "apple_terminal":
		return tuiTerminalAppleTerminal
	}
	if strings.Contains(termProgram, "ghostty") {
		return tuiTerminalGhostty
	}
	if strings.Contains(termProgram, "terminal") {
		return tuiTerminalAppleTerminal
	}
	return ""
}

func launchAppleTerminalTab(command string) error {
	cmdLine := "/bin/bash -lc " + shellQuote(command)
	script := "tell application \"Terminal\"\n" +
		"activate\n" +
		"if (count of windows) is 0 then\n" +
		"do script " + strconv.Quote(cmdLine) + "\n" +
		"else\n" +
		"do script " + strconv.Quote(cmdLine) + " in front window\n" +
		"end if\n" +
		"end tell"
	if err := tuiLaunchCommandFn("osascript", "-e", script); err != nil {
		return fmt.Errorf("open Terminal tab: %w", err)
	}
	return nil
}

func launchGhosttyTab(command string) error {
	cmdLine := "/bin/bash -lc " + shellQuote(command)
	script := "tell application \"Ghostty\" to activate\n" +
		"delay 0.1\n" +
		"tell application \"System Events\"\n" +
		"tell process \"Ghostty\"\n" +
		"keystroke \"t\" using command down\n" +
		"delay 0.15\n" +
		"keystroke " + strconv.Quote(cmdLine) + "\n" +
		"key code 36\n" +
		"end tell\n" +
		"end tell"
	if err := tuiLaunchCommandFn("osascript", "-e", script); err != nil {
		return fmt.Errorf("open Ghostty tab: %w", err)
	}
	return nil
}

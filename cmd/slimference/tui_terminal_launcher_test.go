package main

import (
	"errors"
	"strings"
	"testing"
)

func TestDetectTUITerminalApp(t *testing.T) {
	oldEnv := tuiTerminalEnvFn
	t.Cleanup(func() { tuiTerminalEnvFn = oldEnv })
	for _, tc := range []struct {
		value string
		want  tuiTerminalApp
	}{
		{value: "ghostty", want: tuiTerminalGhostty},
		{value: "Ghostty", want: tuiTerminalGhostty},
		{value: "Apple_Terminal", want: tuiTerminalAppleTerminal},
		{value: "Apple Terminal", want: tuiTerminalAppleTerminal},
		{value: "unknown", want: ""},
	} {
		t.Run(tc.value, func(t *testing.T) {
			tuiTerminalEnvFn = func(key string) string {
				if key != "TERM_PROGRAM" {
					t.Fatalf("unexpected env key %q", key)
				}
				return tc.value
			}
			if got := detectTUITerminalApp(); got != tc.want {
				t.Fatalf("detect=%q want %q", got, tc.want)
			}
		})
	}
}

func TestLaunchCodexCLIInCurrentTerminalUsesGhosttyTab(t *testing.T) {
	oldEnv := tuiTerminalEnvFn
	oldLaunch := tuiLaunchCommandFn
	t.Cleanup(func() {
		tuiTerminalEnvFn = oldEnv
		tuiLaunchCommandFn = oldLaunch
	})
	tuiTerminalEnvFn = func(key string) string { return "ghostty" }
	var gotName string
	var gotArgs []string
	tuiLaunchCommandFn = func(name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}

	msg, err := launchCodexCLIInCurrentTerminal("/tmp/slimference", "/tmp/slim repo")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if gotName != "osascript" || len(gotArgs) != 2 {
		t.Fatalf("command=%q args=%v", gotName, gotArgs)
	}
	script := gotArgs[1]
	for _, want := range []string{
		"tell application \"Ghostty\" to activate",
		"tell process \"Ghostty\"",
		"keystroke \"t\" using command down",
		"/bin/bash -lc",
		"/tmp/slim repo",
		"[2J",
		"[H",
		"[SF] Codex CLI started with Slimference",
		"exec /tmp/slimference codex run --transport=auto --",
		"/tmp/slimference codex run --transport=auto --",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Ghostty script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "tell application \"Terminal\"") {
		t.Fatalf("Ghostty launch must not target Terminal.app:\n%s", script)
	}
	if !strings.Contains(msg, "(Ghostty)") {
		t.Fatalf("message=%q", msg)
	}
}

func TestLaunchCodexCLIInCurrentTerminalUsesTerminalTab(t *testing.T) {
	oldEnv := tuiTerminalEnvFn
	oldLaunch := tuiLaunchCommandFn
	t.Cleanup(func() {
		tuiTerminalEnvFn = oldEnv
		tuiLaunchCommandFn = oldLaunch
	})
	tuiTerminalEnvFn = func(key string) string { return "Apple_Terminal" }
	var gotName string
	var gotArgs []string
	tuiLaunchCommandFn = func(name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}

	msg, err := launchCodexCLIInCurrentTerminal("/tmp/slimference", "/repo")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if gotName != "osascript" || len(gotArgs) != 2 {
		t.Fatalf("command=%q args=%v", gotName, gotArgs)
	}
	script := gotArgs[1]
	for _, want := range []string{
		"tell application \"Terminal\"",
		"do script",
		"in front window",
		"cd /repo",
		"[2J",
		"[H",
		"[SF] Codex CLI started with Slimference",
		"exec /tmp/slimference codex run --transport=auto --",
		"/tmp/slimference codex run --transport=auto --",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Terminal script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "tell process \"Ghostty\"") {
		t.Fatalf("Terminal launch must not target Ghostty:\n%s", script)
	}
	if !strings.Contains(msg, "(Terminal)") {
		t.Fatalf("message=%q", msg)
	}
}

func TestLaunchCodexCLIInCurrentTerminalPropagatesLauncherError(t *testing.T) {
	oldEnv := tuiTerminalEnvFn
	oldLaunch := tuiLaunchCommandFn
	t.Cleanup(func() {
		tuiTerminalEnvFn = oldEnv
		tuiLaunchCommandFn = oldLaunch
	})
	tuiTerminalEnvFn = func(key string) string { return "ghostty" }
	tuiLaunchCommandFn = func(name string, args ...string) error {
		return errors.New("automation denied")
	}
	_, err := launchCodexCLIInCurrentTerminal("/tmp/slimference", "/repo")
	if err == nil || !strings.Contains(err.Error(), "open Ghostty tab") || !strings.Contains(err.Error(), "automation denied") {
		t.Fatalf("error=%v", err)
	}
}

func TestLaunchCodexCLIInCurrentTerminalRejectsUnknownTerminal(t *testing.T) {
	oldEnv := tuiTerminalEnvFn
	oldLaunch := tuiLaunchCommandFn
	t.Cleanup(func() {
		tuiTerminalEnvFn = oldEnv
		tuiLaunchCommandFn = oldLaunch
	})
	tuiTerminalEnvFn = func(key string) string { return "WeirdTerm" }
	tuiLaunchCommandFn = func(name string, args ...string) error {
		t.Fatalf("unknown terminal must not launch %s %v", name, args)
		return nil
	}
	_, err := launchCodexCLIInCurrentTerminal("/tmp/slimference", "/repo")
	if err == nil || !strings.Contains(err.Error(), "unsupported terminal app") {
		t.Fatalf("error=%v", err)
	}
}

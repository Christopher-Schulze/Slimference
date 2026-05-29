package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/daemon"
)

func TestServiceControlAdapter_StartDaemon(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origExecutable := osExecutable
	origStartDetached := startDetachedDaemonFn
	defer func() {
		daemonIsRunningFn = origIsRunning
		osExecutable = origExecutable
		startDetachedDaemonFn = origStartDetached
	}()

	started := false
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		if started {
			return true, &daemon.PIDFile{PID: 77, Port: 8990}, nil
		}
		return false, nil, nil
	}
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startDetachedDaemonFn = func(binary string) error {
		started = true
		if binary != "/tmp/slimference" {
			t.Fatalf("unexpected binary: %q", binary)
		}
		return nil
	}

	if err := (&serviceControlAdapter{}).StartDaemon(); err != nil {
		t.Fatalf("StartDaemon: %v", err)
	}
	if !started {
		t.Fatal("expected osStartProcess to be called")
	}
}

func TestServiceControlAdapter_StartDaemonErrors(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origExecutable := osExecutable
	origStartDetached := startDetachedDaemonFn
	defer func() {
		daemonIsRunningFn = origIsRunning
		osExecutable = origExecutable
		startDetachedDaemonFn = origStartDetached
	}()

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return false, nil, errors.New("state boom")
	}
	if err := (&serviceControlAdapter{}).StartDaemon(); err == nil || !strings.Contains(err.Error(), "check daemon") {
		t.Fatalf("expected daemon check error, got %v", err)
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 42, Port: 8990}, nil
	}
	if err := (&serviceControlAdapter{}).StartDaemon(); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected already running error, got %v", err)
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	if err := (&serviceControlAdapter{}).StartDaemon(); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("expected executable error, got %v", err)
	}

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startDetachedDaemonFn = func(string) error {
		return errors.New("boom")
	}
	if err := (&serviceControlAdapter{}).StartDaemon(); err == nil || !strings.Contains(err.Error(), "start daemon") {
		t.Fatalf("expected start daemon error, got %v", err)
	}
}

func TestResolveDaemonLifecycleBinaryRejectsTemporaryGoBuild(t *testing.T) {
	origExecutable := osExecutable
	t.Cleanup(func() { osExecutable = origExecutable })

	osExecutable = func() (string, error) {
		return filepath.Join(os.TempDir(), "go-build123456", "b001", "exe", "slimference"), nil
	}
	_, err := resolveDaemonLifecycleBinary("start")
	if err == nil || !strings.Contains(err.Error(), "temporary Go build artifact") {
		t.Fatalf("expected temp executable rejection, got %v", err)
	}
}

func TestServiceControlAdapter_StopRestartInstallUninstallAndStatus(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origStop := daemonStopFn
	origInstall := daemonInstallLaunchdFn
	origUninstall := daemonUninstallFn
	origExecutable := osExecutable
	origStartDetached := startDetachedDaemonFn
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonStopFn = origStop
		daemonInstallLaunchdFn = origInstall
		daemonUninstallFn = origUninstall
		osExecutable = origExecutable
		startDetachedDaemonFn = origStartDetached
	}()

	stopCalls := 0
	daemonStopFn = func() error {
		stopCalls++
		return nil
	}
	if err := (&serviceControlAdapter{}).StopDaemon(); err != nil {
		t.Fatalf("StopDaemon: %v", err)
	}

	isRunningChecks := 0
	startCalls := 0
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		isRunningChecks++
		if isRunningChecks == 1 {
			return true, &daemon.PIDFile{PID: 42, Port: 8990}, nil
		}
		if startCalls > 0 {
			return true, &daemon.PIDFile{PID: 43, Port: 8990}, nil
		}
		return false, nil, nil
	}
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startDetachedDaemonFn = func(string) error {
		startCalls++
		return nil
	}
	if err := (&serviceControlAdapter{}).RestartDaemon(); err != nil {
		t.Fatalf("RestartDaemon: %v", err)
	}
	if stopCalls != 2 || startCalls != 1 {
		t.Fatalf("unexpected stop/start calls: stop=%d start=%d", stopCalls, startCalls)
	}

	daemonInstallLaunchdFn = func(binary string) error {
		if binary != "/tmp/slimference" {
			t.Fatalf("unexpected binary: %q", binary)
		}
		return nil
	}
	if err := (&serviceControlAdapter{}).InstallService(); err != nil {
		t.Fatalf("InstallService: %v", err)
	}

	daemonUninstallFn = func() error { return nil }
	if err := (&serviceControlAdapter{}).UninstallService(); err != nil {
		t.Fatalf("UninstallService: %v", err)
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 7, Port: 8123}, nil
	}
	running, pid, port := (&serviceControlAdapter{}).DaemonStatus()
	if !running || pid != 7 || port != 8123 {
		t.Fatalf("DaemonStatus: running=%v pid=%d port=%d", running, pid, port)
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	running, pid, port = (&serviceControlAdapter{}).DaemonStatus()
	if running || pid != 0 || port != 0 {
		t.Fatalf("DaemonStatus false case: running=%v pid=%d port=%d", running, pid, port)
	}
}

func TestServiceControlAdapter_RestartAndInstallServiceErrors(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origStop := daemonStopFn
	origExecutable := osExecutable
	origInstall := daemonInstallLaunchdFn
	origStartDetached := startDetachedDaemonFn
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonStopFn = origStop
		osExecutable = origExecutable
		daemonInstallLaunchdFn = origInstall
		startDetachedDaemonFn = origStartDetached
	}()

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 42, Port: 8990}, nil
	}
	daemonStopFn = func() error { return errors.New("stop failed") }
	if err := (&serviceControlAdapter{}).RestartDaemon(); err == nil || !strings.Contains(err.Error(), "stop failed") {
		t.Fatalf("expected restart stop error, got %v", err)
	}

	startCalls := 0
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		if startCalls > 0 {
			return true, &daemon.PIDFile{PID: 43, Port: 8990}, nil
		}
		return false, nil, nil
	}
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startDetachedDaemonFn = func(string) error {
		startCalls++
		return nil
	}
	if err := (&serviceControlAdapter{}).RestartDaemon(); err != nil {
		t.Fatalf("RestartDaemon no-running path: %v", err)
	}
	if startCalls != 1 {
		t.Fatalf("expected one start call, got %d", startCalls)
	}

	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	if err := (&serviceControlAdapter{}).InstallService(); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("expected install service executable error, got %v", err)
	}

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	daemonInstallLaunchdFn = func(string) error { return errors.New("install failed") }
	if err := (&serviceControlAdapter{}).InstallService(); err == nil || !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("expected install launchd error, got %v", err)
	}
}

func TestServiceControlAdapter_HookOperations(t *testing.T) {
	origHome := osUserHomeDir
	origConfigLoad := configLoadFn
	origInstallClaude := installClaudeHookFn
	origInstallCodex := installCodexHookFn
	origRemoveClaude := removeClaudeHookFn
	origRemoveCodex := removeCodexHookFn
	defer func() {
		osUserHomeDir = origHome
		configLoadFn = origConfigLoad
		installClaudeHookFn = origInstallClaude
		installCodexHookFn = origInstallCodex
		removeClaudeHookFn = origRemoveClaude
		removeCodexHookFn = origRemoveCodex
	}()

	osUserHomeDir = func() (string, error) { return "/tmp/home", nil }
	cfg := config.Defaults()
	cfg.Hooks.SlimferenceCommand = "/custom/slimference"
	configLoadFn = func() (*config.Config, error) { return cfg, nil }

	installClaudeHookFn = func(home, cmd string) error {
		t.Fatalf("Claude installer must not be called while parked: home=%q cmd=%q", home, cmd)
		return nil
	}
	installCodexHookFn = func(home, cmd string) error {
		if home != "/tmp/home" || cmd != "/custom/slimference" {
			t.Fatalf("unexpected codex args: home=%q cmd=%q", home, cmd)
		}
		return nil
	}
	if err := (&serviceControlAdapter{}).InstallHook("claude"); err == nil || !strings.Contains(err.Error(), "parked") {
		t.Fatalf("expected parked Claude install error, got %v", err)
	}
	if err := (&serviceControlAdapter{}).InstallHook("codex"); err != nil {
		t.Fatalf("InstallHook codex: %v", err)
	}
	if err := (&serviceControlAdapter{}).InstallHook("nope"); err == nil {
		t.Fatal("expected unknown hook target error")
	}

	removeClaudeHookFn = func(home string) error {
		t.Fatalf("Claude remover must not be called while parked: home=%q", home)
		return nil
	}
	removeCodexHookFn = func(home string) error {
		if home != "/tmp/home" {
			t.Fatalf("unexpected remove codex home: %q", home)
		}
		return nil
	}
	if err := (&serviceControlAdapter{}).RemoveHook("claude"); err == nil || !strings.Contains(err.Error(), "parked") {
		t.Fatalf("expected parked Claude remove error, got %v", err)
	}
	if err := (&serviceControlAdapter{}).RemoveHook("codex"); err != nil {
		t.Fatalf("RemoveHook codex: %v", err)
	}
	if err := (&serviceControlAdapter{}).RemoveHook("nope"); err == nil {
		t.Fatal("expected unknown remove hook target error")
	}

	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	if err := (&serviceControlAdapter{}).InstallHook("codex"); err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("expected home error, got %v", err)
	}
	if err := (&serviceControlAdapter{}).RemoveHook("codex"); err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("expected home error, got %v", err)
	}
}

// TestParseDaemonLogsFlags covers every parser branch of `daemon logs` args.
func TestParseDaemonLogsFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(t *testing.T, f daemonLogsFlags)
	}{
		{
			name: "defaults",
			args: nil,
			check: func(t *testing.T, f daemonLogsFlags) {
				if f.stream != "both" || f.lines != 200 || f.showPath || f.since != 0 {
					t.Fatalf("unexpected defaults: %+v", f)
				}
			},
		},
		{
			name: "empty string skipped",
			args: []string{""},
			check: func(t *testing.T, f daemonLogsFlags) {
				if f.stream != "both" {
					t.Fatalf("empty arg changed state: %+v", f)
				}
			},
		},
		{
			name: "show path",
			args: []string{"--path"},
			check: func(t *testing.T, f daemonLogsFlags) {
				if !f.showPath {
					t.Fatal("--path must set showPath")
				}
			},
		},
		{
			name: "stream stdout",
			args: []string{"--stream=stdout"},
			check: func(t *testing.T, f daemonLogsFlags) {
				if f.stream != "stdout" {
					t.Fatalf("stream: %s", f.stream)
				}
			},
		},
		{
			name: "stream stderr",
			args: []string{"--stream=stderr"},
			check: func(t *testing.T, f daemonLogsFlags) {
				if f.stream != "stderr" {
					t.Fatal("stream must be stderr")
				}
			},
		},
		{
			name: "lines override",
			args: []string{"--lines=50"},
			check: func(t *testing.T, f daemonLogsFlags) {
				if f.lines != 50 {
					t.Fatalf("lines: %d", f.lines)
				}
			},
		},
		{
			name: "since override",
			args: []string{"--since=10m"},
			check: func(t *testing.T, f daemonLogsFlags) {
				if f.since <= 0 {
					t.Fatal("since must be positive")
				}
			},
		},
		{name: "bad stream", args: []string{"--stream=other"}, wantErr: true},
		{name: "bad lines", args: []string{"--lines=zero"}, wantErr: true},
		{name: "negative lines", args: []string{"--lines=-3"}, wantErr: true},
		{name: "bad since", args: []string{"--since=never"}, wantErr: true},
		{name: "zero since", args: []string{"--since=0s"}, wantErr: true},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := parseDaemonLogsFlags(tc.args)
			if tc.wantErr {
				if f.err == nil {
					t.Fatalf("expected error, got flags=%+v", f)
				}
				return
			}
			if f.err != nil {
				t.Fatalf("unexpected error: %v", f.err)
			}
			if tc.check != nil {
				tc.check(t, f)
			}
		})
	}
}

// TestHandleDaemonLogsCmd_paths prints stdout+stderr paths with --path.
func TestHandleDaemonLogsCmd_paths(t *testing.T) {
	origStdout := daemonStdoutLogPathFn
	origStderr := daemonStderrLogPathFn
	t.Cleanup(func() {
		daemonStdoutLogPathFn = origStdout
		daemonStderrLogPathFn = origStderr
	})
	daemonStdoutLogPathFn = func() string { return "/tmp/test-stdout.log" }
	daemonStderrLogPathFn = func() string { return "/tmp/test-stderr.log" }

	origOut := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	handleDaemonLogsCmd([]string{"--path"})
	_ = wp.Close()
	os.Stdout = origOut

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	out := buf.String()
	if !strings.Contains(out, "/tmp/test-stdout.log") || !strings.Contains(out, "/tmp/test-stderr.log") {
		t.Fatalf("missing paths: %q", out)
	}
}

// TestHandleDaemonLogsCmd_badFlag exits non-zero when parse fails.
func TestHandleDaemonLogsCmd_badFlag(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleDaemonLogsCmd([]string{"--nope"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "unknown flag") {
		t.Fatalf("expected exit 1 with unknown flag, got code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

// TestHandleDaemonLogsCmd_printsLines prints stdout + stderr log content.
func TestHandleDaemonLogsCmd_printsLines(t *testing.T) {
	origStdout := daemonStdoutLogPathFn
	origStderr := daemonStderrLogPathFn
	origRead := daemonReadRecentLogLinesFn
	t.Cleanup(func() {
		daemonStdoutLogPathFn = origStdout
		daemonStderrLogPathFn = origStderr
		daemonReadRecentLogLinesFn = origRead
	})
	daemonStdoutLogPathFn = func() string { return "/tmp/a.log" }
	daemonStderrLogPathFn = func() string { return "/tmp/b.log" }
	daemonReadRecentLogLinesFn = func(path string, n int, since time.Time) ([]string, error) {
		if path == "/tmp/a.log" {
			return []string{"stdout line"}, nil
		}
		return []string{"stderr line"}, nil
	}

	origOut := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	handleDaemonLogsCmd(nil)
	_ = wp.Close()
	os.Stdout = origOut

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	out := buf.String()
	if !strings.Contains(out, "stdout line") || !strings.Contains(out, "stderr line") {
		t.Fatalf("missing expected lines: %q", out)
	}
}

// TestHandleDaemonLogsCmd_streamFilter routes to exactly one stream.
func TestHandleDaemonLogsCmd_streamFilter(t *testing.T) {
	origStdout := daemonStdoutLogPathFn
	origStderr := daemonStderrLogPathFn
	origRead := daemonReadRecentLogLinesFn
	t.Cleanup(func() {
		daemonStdoutLogPathFn = origStdout
		daemonStderrLogPathFn = origStderr
		daemonReadRecentLogLinesFn = origRead
	})
	daemonStdoutLogPathFn = func() string { return "/tmp/a.log" }
	daemonStderrLogPathFn = func() string { return "/tmp/b.log" }
	daemonReadRecentLogLinesFn = func(path string, n int, since time.Time) ([]string, error) {
		return []string{path}, nil
	}

	origOut := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	handleDaemonLogsCmd([]string{"--stream=stderr"})
	_ = wp.Close()
	os.Stdout = origOut

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	out := buf.String()
	if strings.Contains(out, "/tmp/a.log") {
		t.Fatalf("stderr stream must not print stdout path: %q", out)
	}
	if !strings.Contains(out, "/tmp/b.log") {
		t.Fatalf("stderr stream missing: %q", out)
	}
}

// TestHandleDaemonLogsCmd_sinceFilter passes the cutoff into the reader.
func TestHandleDaemonLogsCmd_sinceFilter(t *testing.T) {
	origRead := daemonReadRecentLogLinesFn
	t.Cleanup(func() { daemonReadRecentLogLinesFn = origRead })
	var gotSince time.Time
	daemonReadRecentLogLinesFn = func(path string, n int, since time.Time) ([]string, error) {
		gotSince = since
		return []string{"ok"}, nil
	}
	origOut := os.Stdout
	_, wp, _ := os.Pipe()
	os.Stdout = wp
	handleDaemonLogsCmd([]string{"--since=1h", "--stream=stdout"})
	_ = wp.Close()
	os.Stdout = origOut
	if gotSince.IsZero() {
		t.Fatal("--since must propagate a cutoff into the reader")
	}
}

// TestHandleDaemonLogsCmd_readError prints a warning but does not exit.
func TestHandleDaemonLogsCmd_readError(t *testing.T) {
	origRead := daemonReadRecentLogLinesFn
	t.Cleanup(func() { daemonReadRecentLogLinesFn = origRead })
	daemonReadRecentLogLinesFn = func(path string, n int, since time.Time) ([]string, error) {
		return nil, errors.New("boom")
	}

	rp, cleanup := redirectStderr()
	origOut := os.Stdout
	_, wp, _ := os.Pipe()
	os.Stdout = wp
	handleDaemonLogsCmd([]string{"--stream=stdout"})
	_ = wp.Close()
	os.Stdout = origOut
	cleanup()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("expected boom error surfaced to stderr, got %q", buf.String())
	}
}

// TestHandleDaemonLogsCmd_emptyLog surfaces the no-lines hint on stderr.
func TestHandleDaemonLogsCmd_emptyLog(t *testing.T) {
	origRead := daemonReadRecentLogLinesFn
	t.Cleanup(func() { daemonReadRecentLogLinesFn = origRead })
	daemonReadRecentLogLinesFn = func(path string, n int, since time.Time) ([]string, error) {
		return nil, nil
	}

	rp, cleanup := redirectStderr()
	origOut := os.Stdout
	_, wp, _ := os.Pipe()
	os.Stdout = wp
	handleDaemonLogsCmd([]string{"--stream=stdout"})
	_ = wp.Close()
	os.Stdout = origOut
	cleanup()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !strings.Contains(buf.String(), "no stdout lines") {
		t.Fatalf("expected 'no stdout lines' hint, got %q", buf.String())
	}
}

// TestHandleDaemonCmd_logsDispatch covers the dispatch into
// handleDaemonLogsCmd from the outer daemon handler.
func TestHandleDaemonCmd_logsDispatch(t *testing.T) {
	origStdout := daemonStdoutLogPathFn
	origStderr := daemonStderrLogPathFn
	t.Cleanup(func() {
		daemonStdoutLogPathFn = origStdout
		daemonStderrLogPathFn = origStderr
	})
	daemonStdoutLogPathFn = func() string { return "/tmp/x.log" }
	daemonStderrLogPathFn = func() string { return "/tmp/y.log" }

	origOut := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	handleDaemonCmd([]string{"logs", "--path"})
	_ = wp.Close()
	os.Stdout = origOut

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !strings.Contains(buf.String(), "/tmp/x.log") {
		t.Fatalf("dispatched handler output missing: %q", buf.String())
	}
}

// TestHandleDaemonCmd_noArgsSuccess covers the zero-args happy path.
func TestHandleDaemonCmd_noArgsSuccess(t *testing.T) {
	origRun := daemonRunFn
	t.Cleanup(func() { daemonRunFn = origRun })
	daemonRunFn = func(start func() (int, func(context.Context) error, error)) error {
		return nil
	}

	done := make(chan struct{})
	go func() {
		handleDaemonCmd(nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleDaemonCmd did not return on success")
	}
}

// TestHandleDaemonCmd_unknownSub fails cleanly on an unknown subcommand.
func TestHandleDaemonCmd_unknownSub(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleDaemonCmd([]string{"nope"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "unknown daemon subcommand") {
		t.Fatalf("expected exit 1 with unknown subcommand, got code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

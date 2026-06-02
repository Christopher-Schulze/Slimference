package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type codexCaptureRunFlags struct {
	binary              string
	capturePath         string
	host                string
	port                string
	healthTimeout       time.Duration
	matrixPath          string
	id                  string
	client              string
	workloadClass       string
	codexVersion        string
	slimferenceCommit   string
	repo                string
	model               string
	exitMarker          string
	exitMarkerCount     int
	expectedReducers    []string
	expectedZeroSavings bool
	help                bool
	codexArgs           []string
}

type codexCaptureRunDeps struct {
	now            func() time.Time
	ensureNoDaemon func(context.Context, codexCaptureRunFlags) error
	startDaemon    func(context.Context, codexCaptureRunFlags, io.Writer) (*codexCaptureDaemon, error)
	waitHealth     func(context.Context, codexCaptureRunFlags, <-chan error) error
	runCodex       func(context.Context, codexCaptureRunFlags, io.Writer, io.Writer) error
	stopDaemon     func(context.Context, *codexCaptureDaemon) error
	replay         func(wssABReplayFlags) (wssABReplayReport, error)
}

type codexCaptureDaemon struct {
	cmd  *exec.Cmd
	done <-chan error
}

type codexCaptureRunResult struct {
	CapturePath string            `json:"capture_path"`
	MatrixPath  string            `json:"matrix_path,omitempty"`
	Replay      wssABReplayReport `json:"replay"`
	StartedAt   string            `json:"started_at"`
	EndedAt     string            `json:"ended_at"`
}

const codexCaptureRunHelpText = `codex-capture-run: run a scoped Codex CLI capture with a managed foreground daemon

Usage:
  go run ./scripts/utils codex-capture-run [flags] -- <codex run args...>

Flags:
  --binary PATH              Slimference binary to run (default: slimference)
  --capture PATH             WSS frame capture path (default: ~/.slimference/captures/codex-capture-<timestamp>.jsonl)
  --host HOST                Daemon host (default: 127.0.0.1)
  --port PORT                Daemon port (default: 8990)
  --health-timeout DURATION  Time to wait for daemon /health (default: 10s)
  --matrix-row PATH          Append a wss-proof-matrix JSONL row after replay
  --id ID                    Matrix row id
  --client cli|desktop       Matrix row client (default: cli)
  --workload-class CLASS     Matrix row workload class, required with --matrix-row
  --expected-reducer NAME    Matrix row expected reducer, repeatable
  --expected-zero            Matrix row expected_zero_savings=true
  --codex-version VALUE      Matrix row Codex version
  --slimference-commit VALUE Matrix row Slimference commit
  --repo VALUE               Matrix row repository label
  --model VALUE              Matrix row model label
  --exit-marker TEXT         Interrupt Codex automatically once TEXT appears in output.
                             On macOS this uses script(1) so Codex still sees a TTY.
  --exit-marker-count N      Required marker occurrences before interrupt (default: 1)

The tool starts the daemon as its own child process with SLIMFERENCE_WSS_AB_CAPTURE
set, waits for /health, runs "slimference codex run --transport=auto -- ...",
stops the daemon, then replays the capture with --fail-on-lost semantics. It
does not use a detached background daemon, because detached shell starts are too
fragile for unattended release captures.`

func runCodexCaptureRun(args []string, stdout, stderr io.Writer) int {
	deps := codexCaptureRunDeps{
		now:            time.Now,
		ensureNoDaemon: ensureNoCodexCaptureDaemon,
		startDaemon:    startCodexCaptureDaemon,
		waitHealth:     waitCodexCaptureHealth,
		runCodex:       runCodexCaptureCLI,
		stopDaemon:     stopCodexCaptureDaemon,
		replay:         loadWSSABReplayReport,
	}
	return runCodexCaptureRunWithDeps(args, stdout, stderr, deps)
}

func runCodexCaptureRunWithDeps(args []string, stdout, stderr io.Writer, deps codexCaptureRunDeps) int {
	flags, err := parseCodexCaptureRunFlags(args, deps.now())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, codexCaptureRunHelpText)
		return 0
	}
	if len(flags.codexArgs) == 0 {
		fmt.Fprintln(stderr, "codex-capture-run requires codex run arguments after --")
		return 2
	}
	if flags.matrixPath != "" && flags.workloadClass == "" {
		fmt.Fprintln(stderr, "--workload-class is required with --matrix-row")
		return 2
	}
	if err := ensureCodexCaptureDir(flags.capturePath); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	ctx := context.Background()
	if err := deps.ensureNoDaemon(ctx, flags); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	startedAt := deps.now().UTC()
	daemon, err := deps.startDaemon(ctx, flags, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	stopDaemon := true
	defer func() {
		if stopDaemon {
			_ = deps.stopDaemon(context.Background(), daemon)
		}
	}()
	if err := deps.waitHealth(ctx, flags, daemon.done); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := deps.runCodex(ctx, flags, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := deps.stopDaemon(ctx, daemon); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	stopDaemon = false
	endedAt := deps.now().UTC()

	replay, err := deps.replay(wssABReplayFlags{path: flags.capturePath, failOnLost: true})
	if err != nil {
		fmt.Fprintf(stderr, "replay capture: %v\n", err)
		return 1
	}
	result := codexCaptureRunResult{
		CapturePath: flags.capturePath,
		MatrixPath:  flags.matrixPath,
		Replay:      replay,
		StartedAt:   startedAt.Format(time.RFC3339),
		EndedAt:     endedAt.Format(time.RFC3339),
	}
	if flags.matrixPath != "" {
		if err := appendCodexCaptureMatrixRow(flags, result); err != nil {
			fmt.Fprintf(stderr, "append matrix row: %v\n", err)
			return 1
		}
	}
	writeCodexCaptureRunSummary(stdout, result)
	if !replay.GatePassed {
		return 3
	}
	return 0
}

func parseCodexCaptureRunFlags(args []string, now time.Time) (codexCaptureRunFlags, error) {
	flags := codexCaptureRunFlags{
		binary:          "slimference",
		host:            "127.0.0.1",
		port:            "8990",
		healthTimeout:   10 * time.Second,
		client:          "cli",
		exitMarkerCount: 1,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			flags.codexArgs = append(flags.codexArgs, args[i+1:]...)
			break
		}
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--expected-zero":
			flags.expectedZeroSavings = true
		case arg == "--binary", arg == "--capture", arg == "--host", arg == "--port",
			arg == "--health-timeout", arg == "--matrix-row", arg == "--id",
			arg == "--client", arg == "--workload-class", arg == "--expected-reducer",
			arg == "--codex-version", arg == "--slimference-commit", arg == "--repo",
			arg == "--model", arg == "--exit-marker":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("%s requires a value", arg)
			}
			i++
			if err := setCodexCaptureRunFlag(&flags, arg, args[i]); err != nil {
				return flags, err
			}
		case arg == "--exit-marker-count":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("%s requires a value", arg)
			}
			i++
			n, err := parseNonNegativeIntFlag("--exit-marker-count", args[i])
			if err != nil {
				return flags, err
			}
			if n == 0 {
				return flags, fmt.Errorf("--exit-marker-count must be > 0")
			}
			flags.exitMarkerCount = n
		case strings.HasPrefix(arg, "--exit-marker-count="):
			n, err := parseNonNegativeIntFlag("--exit-marker-count", strings.TrimPrefix(arg, "--exit-marker-count="))
			if err != nil {
				return flags, err
			}
			if n == 0 {
				return flags, fmt.Errorf("--exit-marker-count must be > 0")
			}
			flags.exitMarkerCount = n
		case strings.HasPrefix(arg, "--"):
			name, value, ok := strings.Cut(arg, "=")
			if !ok {
				return flags, fmt.Errorf("unknown flag: %s", arg)
			}
			if err := setCodexCaptureRunFlag(&flags, name, value); err != nil {
				return flags, err
			}
		default:
			return flags, fmt.Errorf("unexpected argument before --: %s", arg)
		}
	}
	if flags.capturePath == "" {
		flags.capturePath = filepath.Join("~", ".slimference", "captures", "codex-capture-"+now.UTC().Format("20060102T150405Z")+".jsonl")
	}
	var err error
	flags.capturePath, err = expandCodexCapturePath(flags.capturePath)
	if err != nil {
		return flags, err
	}
	flags.matrixPath, err = expandCodexCapturePath(flags.matrixPath)
	if err != nil {
		return flags, err
	}
	flags.client = strings.ToLower(strings.TrimSpace(flags.client))
	if flags.client != "cli" && flags.client != "desktop" {
		return flags, fmt.Errorf("--client must be cli or desktop")
	}
	return flags, nil
}

func setCodexCaptureRunFlag(flags *codexCaptureRunFlags, name, value string) error {
	value = strings.TrimSpace(value)
	switch name {
	case "--binary":
		flags.binary = value
	case "--capture":
		flags.capturePath = value
	case "--host":
		flags.host = value
	case "--port":
		flags.port = value
	case "--health-timeout":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("--health-timeout must be a positive duration")
		}
		flags.healthTimeout = d
	case "--matrix-row":
		flags.matrixPath = value
	case "--id":
		flags.id = value
	case "--client":
		flags.client = value
	case "--workload-class":
		flags.workloadClass = value
	case "--expected-reducer":
		if value != "" {
			flags.expectedReducers = append(flags.expectedReducers, value)
		}
	case "--codex-version":
		flags.codexVersion = value
	case "--slimference-commit":
		flags.slimferenceCommit = value
	case "--repo":
		flags.repo = value
	case "--model":
		flags.model = value
	case "--exit-marker":
		flags.exitMarker = value
	default:
		return fmt.Errorf("unknown flag: %s", name)
	}
	return nil
}

func expandCodexCapturePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("resolve home for %s: %w", path, err)
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func ensureCodexCaptureDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create capture dir %s: %w", dir, err)
	}
	return nil
}

func ensureNoCodexCaptureDaemon(ctx context.Context, flags codexCaptureRunFlags) error {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	url := "http://" + flags.host + ":" + flags.port + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build capture daemon preflight request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return fmt.Errorf("capture daemon preflight found an existing healthy daemon at %s; stop it first so SLIMFERENCE_WSS_AB_CAPTURE applies to the managed daemon", url)
	}
	return nil
}

func startCodexCaptureDaemon(ctx context.Context, flags codexCaptureRunFlags, stderr io.Writer) (*codexCaptureDaemon, error) {
	cmd := exec.CommandContext(ctx, flags.binary, "daemon")
	cmd.Env = append(os.Environ(), "SLIMFERENCE_WSS_AB_CAPTURE="+flags.capturePath)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start capture daemon: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	return &codexCaptureDaemon{cmd: cmd, done: done}, nil
}

func waitCodexCaptureHealth(ctx context.Context, flags codexCaptureRunFlags, daemonDone <-chan error) error {
	deadline := time.NewTimer(flags.healthTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	url := "http://" + flags.host + ":" + flags.port + "/health"
	client := http.Client{Timeout: 500 * time.Millisecond}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-daemonDone:
			if err == nil {
				return errors.New("capture daemon exited before health check passed")
			}
			return fmt.Errorf("capture daemon exited before health check passed: %w", err)
		case <-deadline.C:
			return fmt.Errorf("capture daemon did not become healthy at %s within %s", url, flags.healthTimeout)
		case <-ticker.C:
			resp, err := client.Get(url)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return nil
				}
			}
		}
	}
}

func runCodexCaptureCLI(ctx context.Context, flags codexCaptureRunFlags, stdout, stderr io.Writer) error {
	args := []string{"codex", "run", "--transport=auto", "--"}
	args = append(args, flags.codexArgs...)
	cmd := exec.CommandContext(ctx, flags.binary, args...)
	cmd.Stdin = os.Stdin
	if flags.exitMarker != "" {
		return runCodexCaptureCLIUntilMarker(cmd, flags.exitMarker, flags.exitMarkerCount, stdout, stderr)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run scoped Codex capture: %w", err)
	}
	return nil
}

func runCodexCaptureCLIUntilMarker(cmd *exec.Cmd, marker string, markerCount int, stdout, stderr io.Writer) error {
	if runtime.GOOS != "darwin" {
		return errors.New("--exit-marker requires macOS script(1) PTY support; rerun without --exit-marker and interrupt Codex manually after the marker")
	}
	logFile, err := os.CreateTemp("", "slimference-codex-capture-*.typescript")
	if err != nil {
		return fmt.Errorf("create Codex PTY log: %w", err)
	}
	logPath := logFile.Name()
	_ = logFile.Close()
	defer func() {
		_ = os.Remove(logPath)
	}()

	args := append([]string{"-q", logPath}, cmd.Args...)
	scriptCmd := exec.Command("script", args...)
	scriptCmd.Stdin = cmd.Stdin
	scriptCmd.Stdout = stdout
	scriptCmd.Stderr = stderr
	if err := scriptCmd.Start(); err != nil {
		return fmt.Errorf("start Codex PTY capture: %w", err)
	}
	markerHit := make(chan struct{})
	stopWatch := make(chan struct{})
	go watchCodexCaptureMarker(logPath, marker, markerCount, markerHit, stopWatch)
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- scriptCmd.Wait()
	}()
	var errWait error
	select {
	case <-markerHit:
		if scriptCmd.Process != nil {
			_ = scriptCmd.Process.Signal(os.Interrupt)
		}
		errWait = waitCodexCapturePTYAfterMarker(scriptCmd, waitErr)
	case errWait = <-waitErr:
	}
	close(stopWatch)
	if errWait != nil {
		select {
		case <-markerHit:
			return nil
		default:
		}
		return fmt.Errorf("run scoped Codex capture: %w", errWait)
	}
	return nil
}

func waitCodexCapturePTYAfterMarker(scriptCmd *exec.Cmd, waitErr <-chan error) error {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case err := <-waitErr:
		return err
	case <-timer.C:
		if scriptCmd.Process != nil {
			_ = scriptCmd.Process.Kill()
		}
		return <-waitErr
	}
}

func watchCodexCaptureMarker(path, marker string, markerCount int, hit chan<- struct{}, stop <-chan struct{}) {
	if marker == "" {
		return
	}
	if markerCount <= 0 {
		markerCount = 1
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	signaled := false
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if !signaled && strings.Count(string(data), marker) >= markerCount {
				signaled = true
				close(hit)
			}
		}
	}
}

func stopCodexCaptureDaemon(ctx context.Context, daemon *codexCaptureDaemon) error {
	if daemon == nil || daemon.cmd == nil || daemon.cmd.Process == nil {
		return nil
	}
	if daemon.done == nil {
		return daemon.cmd.Process.Kill()
	}
	select {
	case err := <-daemon.done:
		return ignoreExpectedProcessExit(err)
	default:
	}
	_ = daemon.cmd.Process.Signal(os.Interrupt)
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	select {
	case err := <-daemon.done:
		return ignoreExpectedProcessExit(err)
	case <-ctx.Done():
		_ = daemon.cmd.Process.Kill()
		return ctx.Err()
	case <-timeout.C:
		if err := daemon.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill capture daemon: %w", err)
		}
		err := <-daemon.done
		return ignoreExpectedProcessExit(err)
	}
}

func ignoreExpectedProcessExit(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return err
}

func appendCodexCaptureMatrixRow(flags codexCaptureRunFlags, result codexCaptureRunResult) error {
	if err := ensureCodexCaptureDir(flags.matrixPath); err != nil {
		return err
	}
	record := wssProofMatrixRecord{
		ID:                  flags.id,
		Client:              flags.client,
		WorkloadClass:       flags.workloadClass,
		FramesPath:          result.CapturePath,
		CodexVersion:        flags.codexVersion,
		SlimferenceCommit:   flags.slimferenceCommit,
		Repo:                flags.repo,
		Model:               flags.model,
		StartedAt:           result.StartedAt,
		EndedAt:             result.EndedAt,
		ExpectedReducers:    append([]string(nil), flags.expectedReducers...),
		ExpectedZeroSavings: flags.expectedZeroSavings,
	}
	f, err := os.OpenFile(flags.matrixPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open matrix row file %s: %w", flags.matrixPath, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if err := enc.Encode(record); err != nil {
		return fmt.Errorf("write matrix row %s: %w", flags.matrixPath, err)
	}
	return nil
}

func writeCodexCaptureRunSummary(w io.Writer, result codexCaptureRunResult) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Codex capture run complete")
	fmt.Fprintf(w, "  capture:       %s\n", result.CapturePath)
	if result.MatrixPath != "" {
		fmt.Fprintf(w, "  matrix_row:    %s\n", result.MatrixPath)
	}
	fmt.Fprintf(w, "  frames:        %d\n", result.Replay.Frames)
	fmt.Fprintf(w, "  request_turns: %d\n", result.Replay.RequestTurns)
	fmt.Fprintf(w, "  mutated:       %d\n", result.Replay.MutatedRequests)
	fmt.Fprintf(w, "  bytes_saved:   %d\n", result.Replay.BytesSaved)
	fmt.Fprintf(w, "  lost:          %d\n", result.Replay.Lost)
	fmt.Fprintf(w, "  gate:          %s\n", passFail(result.Replay.GatePassed))
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCodexCaptureRunFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	flags, err := parseCodexCaptureRunFlags([]string{
		"--binary", "/tmp/slimference",
		"--capture=~/captures/out.jsonl",
		"--host=127.0.0.2",
		"--port", "8991",
		"--health-timeout=2s",
		"--codex-timeout=3s",
		"--matrix-row", "~/matrix.jsonl",
		"--id", "cli-git",
		"--client", "cli",
		"--workload-class", "git_status_diff",
		"--expected-reducer", "captured_output",
		"--expected-reducer=codex_exec_envelope",
		"--expected-zero",
		"--codex-version", "0.136.0",
		"--slimference-commit", "abc123",
		"--repo", "Slimference",
		"--model", "gpt-5.5",
		"--exit-marker", "DONE",
		"--exit-marker-count=2",
		"--quiet-codex-output",
		"--", "Run", "git status",
	}, now)
	if err != nil {
		t.Fatalf("parseCodexCaptureRunFlags: %v", err)
	}
	if flags.binary != "/tmp/slimference" || flags.host != "127.0.0.2" || flags.port != "8991" {
		t.Fatalf("bad route flags: %+v", flags)
	}
	if !strings.HasSuffix(flags.capturePath, filepath.Join("captures", "out.jsonl")) {
		t.Fatalf("capturePath = %q", flags.capturePath)
	}
	if !strings.HasSuffix(flags.matrixPath, "matrix.jsonl") {
		t.Fatalf("matrixPath = %q", flags.matrixPath)
	}
	if flags.healthTimeout != 2*time.Second {
		t.Fatalf("healthTimeout = %s", flags.healthTimeout)
	}
	if flags.codexTimeout != 3*time.Second {
		t.Fatalf("codexTimeout = %s", flags.codexTimeout)
	}
	if !flags.expectedZeroSavings || len(flags.expectedReducers) != 2 {
		t.Fatalf("bad expected reducer flags: %+v", flags)
	}
	if strings.Join(flags.codexArgs, " ") != "Run git status" {
		t.Fatalf("codexArgs = %#v", flags.codexArgs)
	}
	if flags.exitMarker != "DONE" {
		t.Fatalf("exitMarker = %q", flags.exitMarker)
	}
	if flags.exitMarkerCount != 2 {
		t.Fatalf("exitMarkerCount = %d", flags.exitMarkerCount)
	}
	if !flags.quietCodexOutput {
		t.Fatal("quietCodexOutput = false")
	}

	defaults, err := parseCodexCaptureRunFlags([]string{"--", "hello"}, now)
	if err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if defaults.binary != "slimference" || defaults.host != "127.0.0.1" || defaults.port != "8990" {
		t.Fatalf("bad defaults: %+v", defaults)
	}
	if !strings.Contains(defaults.capturePath, "codex-capture-20260602T120000Z.jsonl") {
		t.Fatalf("default capturePath = %q", defaults.capturePath)
	}
}

func TestRunCodexCaptureRunWithDepsLifecycleAndMatrix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	var calls []string
	done := make(chan error)
	deps := codexCaptureRunDeps{
		now: func() time.Time {
			return time.Date(2026, 6, 2, 12, len(calls), 0, 0, time.UTC)
		},
		ensureNoDaemon: func(ctx context.Context, flags codexCaptureRunFlags) error {
			calls = append(calls, "preflight:"+flags.host+":"+flags.port)
			return nil
		},
		startDaemon: func(ctx context.Context, flags codexCaptureRunFlags, stderr io.Writer) (*codexCaptureDaemon, error) {
			calls = append(calls, "start:"+flags.capturePath)
			return &codexCaptureDaemon{done: done}, nil
		},
		waitHealth: func(ctx context.Context, flags codexCaptureRunFlags, daemonDone <-chan error) error {
			calls = append(calls, "health:"+flags.host+":"+flags.port)
			return nil
		},
		adminSnapshot: func(ctx context.Context, flags codexCaptureRunFlags) (codexCaptureAdminSnapshot, error) {
			calls = append(calls, "admin")
			if strings.Count(strings.Join(calls, ","), "admin") == 1 {
				return codexCaptureAdminSnapshot{}, nil
			}
			return codexCaptureAdminSnapshot{
				BillableInputTokensSaved:  321,
				InputTokensSaved:          321,
				PhasefBridged:             1,
				CompressedMessagesMutated: 1,
				FramesReencoded:           1,
				PhasefMutations:           1,
				ProxyLayer0ReadDelta:      1,
			}, nil
		},
		runCodex: func(ctx context.Context, flags codexCaptureRunFlags, stdout, stderr io.Writer) error {
			calls = append(calls, "codex:"+strings.Join(flags.codexArgs, " "))
			return nil
		},
		stopDaemon: func(ctx context.Context, daemon *codexCaptureDaemon) error {
			calls = append(calls, "stop")
			return nil
		},
		replay: func(flags wssABReplayFlags) (wssABReplayReport, error) {
			calls = append(calls, "replay:"+flags.path)
			if !flags.failOnLost {
				t.Fatalf("replay should run with failOnLost")
			}
			return wssABReplayReport{
				Path:            flags.path,
				Frames:          8,
				RequestTurns:    2,
				MutatedRequests: 1,
				BytesSaved:      1234,
				GatePassed:      true,
			}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runCodexCaptureRunWithDeps([]string{
		"--binary", "/tmp/slimference",
		"--capture", capturePath,
		"--matrix-row", matrixPath,
		"--id", "cli-repeat",
		"--workload-class", "repeat_full_read",
		"--expected-reducer", "read_delta",
		"--", "Read AGENTS.md twice",
	}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	wantCalls := []string{
		"preflight:127.0.0.1:8990",
		"start:" + capturePath,
		"health:127.0.0.1:8990",
		"admin",
		"codex:Read AGENTS.md twice",
		"admin",
		"stop",
		"replay:" + capturePath,
	}
	if strings.Join(calls, "\n") != strings.Join(wantCalls, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(wantCalls, "\n"))
	}
	if !strings.Contains(stdout.String(), "billable_input_tokens_saved: 321") ||
		!strings.Contains(stdout.String(), "replay_bytes_saved: 1234") ||
		!strings.Contains(stdout.String(), "gate:          PASS") {
		t.Fatalf("summary missing replay fields:\n%s", stdout.String())
	}
	records, err := readWSSProofMatrixRecords(matrixPath)
	if err != nil {
		t.Fatalf("read matrix row: %v", err)
	}
	if len(records) != 1 || records[0].ID != "cli-repeat" || records[0].FramesPath != capturePath {
		t.Fatalf("bad matrix record: %+v", records)
	}
	if got := strings.Join(records[0].ExpectedReducers, ","); got != "read_delta" {
		t.Fatalf("ExpectedReducers = %q", got)
	}
	if records[0].LiveDelta == nil || records[0].LiveDelta.BillableInputTokensSaved != 321 || records[0].LiveDelta.ProxyLayer0ReadDelta != 1 {
		t.Fatalf("matrix row missing live token delta: %+v", records[0].LiveDelta)
	}
}

func TestRunCodexCaptureRunStopsDaemonOnCodexTimeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	capturePath := filepath.Join(t.TempDir(), "capture.jsonl")
	var calls []string
	deps := codexCaptureRunDeps{
		now: func() time.Time { return time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC) },
		ensureNoDaemon: func(context.Context, codexCaptureRunFlags) error {
			calls = append(calls, "preflight")
			return nil
		},
		startDaemon: func(context.Context, codexCaptureRunFlags, io.Writer) (*codexCaptureDaemon, error) {
			calls = append(calls, "start")
			return &codexCaptureDaemon{done: make(chan error)}, nil
		},
		waitHealth: func(context.Context, codexCaptureRunFlags, <-chan error) error {
			calls = append(calls, "health")
			return nil
		},
		adminSnapshot: func(context.Context, codexCaptureRunFlags) (codexCaptureAdminSnapshot, error) {
			calls = append(calls, "admin")
			return codexCaptureAdminSnapshot{}, nil
		},
		runCodex: func(ctx context.Context, flags codexCaptureRunFlags, stdout, stderr io.Writer) error {
			calls = append(calls, "codex")
			<-ctx.Done()
			return ctx.Err()
		},
		stopDaemon: func(context.Context, *codexCaptureDaemon) error {
			calls = append(calls, "stop")
			return nil
		},
		replay: func(wssABReplayFlags) (wssABReplayReport, error) {
			t.Fatal("replay should not run after Codex timeout")
			return wssABReplayReport{}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runCodexCaptureRunWithDeps([]string{
		"--capture", capturePath,
		"--codex-timeout=1ns",
		"--", "prompt",
	}, &stdout, &stderr, deps)
	if code != 1 || !strings.Contains(stderr.String(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected timeout failure, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if got := strings.Join(calls, ","); got != "preflight,start,health,admin,codex,stop" {
		t.Fatalf("calls = %s", got)
	}
}

func TestRunCodexCaptureRunValidationAndReplayFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	deps := codexCaptureRunDeps{
		now: func() time.Time { return time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC) },
		ensureNoDaemon: func(context.Context, codexCaptureRunFlags) error {
			return nil
		},
		startDaemon: func(context.Context, codexCaptureRunFlags, io.Writer) (*codexCaptureDaemon, error) {
			return &codexCaptureDaemon{done: make(chan error)}, nil
		},
		waitHealth: func(context.Context, codexCaptureRunFlags, <-chan error) error { return nil },
		adminSnapshot: func(context.Context, codexCaptureRunFlags) (codexCaptureAdminSnapshot, error) {
			return codexCaptureAdminSnapshot{}, nil
		},
		runCodex:   func(context.Context, codexCaptureRunFlags, io.Writer, io.Writer) error { return nil },
		stopDaemon: func(context.Context, *codexCaptureDaemon) error { return nil },
		replay: func(wssABReplayFlags) (wssABReplayReport, error) {
			return wssABReplayReport{}, errors.New("bad replay")
		},
	}
	code := runCodexCaptureRunWithDeps([]string{"--matrix-row", filepath.Join(t.TempDir(), "m.jsonl"), "--", "prompt"}, &stdout, &stderr, deps)
	if code != 2 || !strings.Contains(stderr.String(), "--workload-class is required") {
		t.Fatalf("expected workload-class validation, code=%d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runCodexCaptureRunWithDeps([]string{"--capture", filepath.Join(t.TempDir(), "c.jsonl"), "--", "prompt"}, &stdout, &stderr, deps)
	if code != 1 || !strings.Contains(stderr.String(), "replay capture: bad replay") {
		t.Fatalf("expected replay failure, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runCodexCaptureRunWithDeps([]string{"--help"}, &stdout, &stderr, deps)
	if code != 0 || !strings.Contains(stdout.String(), "codex-capture-run") {
		t.Fatalf("help failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestAppendCodexCaptureMatrixRowWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "nested", "matrix.jsonl")
	result := codexCaptureRunResult{
		CapturePath: "/tmp/capture.jsonl",
		StartedAt:   "2026-06-02T12:00:00Z",
		EndedAt:     "2026-06-02T12:01:00Z",
	}
	flags := codexCaptureRunFlags{
		matrixPath:          matrixPath,
		id:                  "desktop-control",
		client:              "desktop",
		workloadClass:       "no_savings_control",
		codexVersion:        "0.136.0",
		slimferenceCommit:   "abc123",
		repo:                "Slimference",
		model:               "gpt-5.5",
		expectedReducers:    []string{"none"},
		expectedZeroSavings: true,
	}
	if err := appendCodexCaptureMatrixRow(flags, result); err != nil {
		t.Fatalf("appendCodexCaptureMatrixRow: %v", err)
	}
	rows, err := readWSSProofMatrixRecords(matrixPath)
	if err != nil {
		t.Fatalf("read matrix: %v", err)
	}
	data, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"expected_zero_savings":true`) {
		t.Fatalf("matrix row missing expected_zero_savings: %s", string(data))
	}
}

func TestWatchCodexCaptureMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "typescript.log")
	if err := os.WriteFile(path, []byte("not yet"), 0o600); err != nil {
		t.Fatal(err)
	}
	hit := make(chan struct{})
	stop := make(chan struct{})
	go watchCodexCaptureMarker(path, "CAPTURE_DONE", 2, hit, stop)
	select {
	case <-hit:
		t.Fatal("marker fired before content was present")
	case <-time.After(150 * time.Millisecond):
	}
	if err := os.WriteFile(path, []byte("prefix CAPTURE_DONE suffix"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hit:
		t.Fatal("marker fired before second occurrence")
	case <-time.After(150 * time.Millisecond):
	}
	if err := os.WriteFile(path, []byte("prefix CAPTURE_DONE suffix CAPTURE_DONE"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hit:
	case <-time.After(time.Second):
		t.Fatal("marker did not fire")
	}
	close(stop)
}

func TestWatchCodexCaptureMarkerFindsANSISeparatedMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "typescript.log")
	rendered := "\x1b[;mC\x1b[0m\n\x1b[;mL\x1b[0m\n\x1b[;mI\x1b[0m\n" +
		"\x1b[;m_\x1b[0m\n\x1b[;mM\x1b[0m\n\x1b[;mA\x1b[0m\n\x1b[;mT\x1b[0m\n" +
		"\x1b[;mR\x1b[0m\n\x1b[;mI\x1b[0m\n\x1b[;mX\x1b[0m\n"
	if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
		t.Fatal(err)
	}
	hit := make(chan struct{})
	stop := make(chan struct{})
	go watchCodexCaptureMarker(path, "CLI_MATRIX", 1, hit, stop)
	select {
	case <-hit:
	case <-time.After(time.Second):
		t.Fatal("ANSI-separated marker did not fire")
	}
	close(stop)
}

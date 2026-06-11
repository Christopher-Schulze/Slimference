package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control"
)

func TestParseCodexCaptureRunFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	flags, err := parseCodexCaptureRunFlags([]string{
		"--binary", "/tmp/slimference",
		"--capture=~/captures/out.jsonl",
		"--host=127.0.0.2",
		"--port", "8991",
		"--transport=wss",
		"--health-timeout=2s",
		"--codex-timeout=3s",
		"--matrix-row", "~/matrix.jsonl",
		"--resource-profile-proof", "~/resource-proof",
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
		"--restart-after-mutated-completion=1",
		"--quiet-codex-output",
		"--", "Run", "git status",
	}, now)
	if err != nil {
		t.Fatalf("parseCodexCaptureRunFlags: %v", err)
	}
	if flags.binary != "/tmp/slimference" || flags.host != "127.0.0.2" || flags.port != "8991" || flags.transport != "wss" {
		t.Fatalf("bad route flags: %+v", flags)
	}
	if !strings.HasSuffix(flags.capturePath, filepath.Join("captures", "out.jsonl")) {
		t.Fatalf("capturePath = %q", flags.capturePath)
	}
	if !strings.HasSuffix(flags.matrixPath, "matrix.jsonl") {
		t.Fatalf("matrixPath = %q", flags.matrixPath)
	}
	if !strings.HasSuffix(flags.resourceProfileProof, "resource-proof") {
		t.Fatalf("resourceProfileProof = %q", flags.resourceProfileProof)
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
	if flags.restartAfterMutatedCompletion != 1 {
		t.Fatalf("restartAfterMutatedCompletion = %d", flags.restartAfterMutatedCompletion)
	}
	if !flags.quietCodexOutput {
		t.Fatal("quietCodexOutput = false")
	}

	defaults, err := parseCodexCaptureRunFlags([]string{"--", "hello"}, now)
	if err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if defaults.binary != "slimference" || defaults.host != "127.0.0.1" || defaults.port != "8990" || defaults.transport != "auto" {
		t.Fatalf("bad defaults: %+v", defaults)
	}
	if !strings.Contains(defaults.capturePath, "codex-capture-20260602T120000Z.jsonl") {
		t.Fatalf("default capturePath = %q", defaults.capturePath)
	}

	resourceDir := filepath.Join(t.TempDir(), "bundle")
	resourceDefaults, err := parseCodexCaptureRunFlags([]string{
		"--resource-profile-proof", resourceDir,
		"--workload-class", "host_resource_long_workday",
		"--", "Run host resource workload",
	}, now)
	if err != nil {
		t.Fatalf("parse resource defaults: %v", err)
	}
	if resourceDefaults.capturePath != filepath.Join(resourceDir, "frames.jsonl") {
		t.Fatalf("resource capturePath = %q", resourceDefaults.capturePath)
	}
	if resourceDefaults.matrixPath != filepath.Join(resourceDir, "matrix.jsonl") {
		t.Fatalf("resource matrixPath = %q", resourceDefaults.matrixPath)
	}
}

func TestRunCodexCaptureRunWithDepsLifecycleAndMatrix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	resourceDir := filepath.Join(dir, "resource")
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
				ProviderCacheReadTokens:   1000,
				ProviderCacheCreateTokens: 200,
				PhasefBridged:             1,
				CompressedMessagesMutated: 1,
				FramesReencoded:           1,
				PhasefMutations:           1,
				ProxyLayer0ReadDelta:      1,
				ProxyLayer0ChunkRefs:      2,
				ProxyLayer0ChunkDedup:     1,
				ProxyLayer0Policy: []control.ProxyLayer0PolicyEntry{
					{Route: "wss_phasef", Mechanism: "chunk_dedup", Action: "allow", Reason: "recoverable_chunk_dedup", Count: 1},
				},
				ProxyLayer0Cache: []control.ProxyLayer0CacheEntry{
					{Route: "wss_phasef", Mechanism: "read_delta", Action: "hit", Reason: "unchanged", Count: 1},
				},
				ToolPrunePruned:         3,
				OutputReduceInjected:    4,
				StopSeqRequestsModified: 5,
				HostBudgetStatus:        "ok",
				HostBudgetCPUWindowPct:  2.5,
				HostBudgetCPUWindowSec:  1.5,
				HostBudgetRSSBytes:      1000,
				HostBudgetStateBytes:    2000,
				HostBudgetCompressionOK: true,
				HostBudgetDegradationOK: true,
			}, nil
		},
		runCodex: func(ctx context.Context, flags codexCaptureRunFlags, stdout, stderr io.Writer) error {
			calls = append(calls, "codex:"+flags.transport+":"+strings.Join(flags.codexArgs, " "))
			return nil
		},
		stopDaemon: func(ctx context.Context, daemon *codexCaptureDaemon) error {
			calls = append(calls, "stop")
			return nil
		},
		replay: func(flags wssABReplayFlags) (wssABReplayReport, error) {
			calls = append(calls, "replay:"+flags.path)
			if !flags.failOnLost || !flags.failOnUpstreamError {
				t.Fatalf("replay should run with failOnLost and failOnUpstreamError: %+v", flags)
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
		resourceBefore: func(ctx context.Context, flags codexCaptureRunFlags, daemon *codexCaptureDaemon) (*codexCaptureResourceProof, error) {
			calls = append(calls, "resource-before:"+flags.resourceProfileProof)
			return &codexCaptureResourceProof{dir: flags.resourceProfileProof}, nil
		},
		resourceAfter: func(ctx context.Context, flags codexCaptureRunFlags, daemon *codexCaptureDaemon, proof *codexCaptureResourceProof) error {
			calls = append(calls, "resource-after:"+proof.dir)
			return nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runCodexCaptureRunWithDeps([]string{
		"--binary", "/tmp/slimference",
		"--capture", capturePath,
		"--transport", "wss",
		"--matrix-row", matrixPath,
		"--resource-profile-proof", resourceDir,
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
		"resource-before:" + resourceDir,
		"codex:wss:Read AGENTS.md twice",
		"admin",
		"resource-after:" + resourceDir,
		"stop",
		"replay:" + capturePath,
	}
	if strings.Join(calls, "\n") != strings.Join(wantCalls, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(wantCalls, "\n"))
	}
	if !strings.Contains(stdout.String(), "billable_input_tokens_saved: 321") ||
		!strings.Contains(stdout.String(), "replay_bytes_saved: 1234") ||
		!strings.Contains(stdout.String(), "provider_cache_read/create:  1000 / 200") ||
		!strings.Contains(stdout.String(), "layer0_live read/repeated/chunk/refs: 1 / 0 / 1 / 2") ||
		!strings.Contains(stdout.String(), "host_budget: ok exceeded=false") ||
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
	if records[0].LiveDelta.ProviderCacheReadTokens != 1000 || records[0].LiveDelta.ProviderCacheCreateTokens != 200 {
		t.Fatalf("matrix row missing provider-cache delta: %+v", records[0].LiveDelta)
	}
	if records[0].LiveDelta.ProxyLayer0ChunkRefs != 2 ||
		records[0].LiveDelta.ToolPrunePruned != 3 ||
		records[0].LiveDelta.OutputReduceInjected != 4 ||
		records[0].LiveDelta.StopSeqRequestsModified != 5 ||
		records[0].LiveDelta.HostBudgetStatus != "ok" ||
		!records[0].LiveDelta.HostBudgetCompressionOK ||
		!records[0].LiveDelta.HostBudgetDegradationOK {
		t.Fatalf("matrix row missing extended live delta: %+v", records[0].LiveDelta)
	}
	if len(records[0].LiveDelta.ProxyLayer0Policy) != 1 ||
		records[0].LiveDelta.ProxyLayer0Policy[0].Mechanism != "chunk_dedup" ||
		records[0].LiveDelta.ProxyLayer0Policy[0].Count != 1 {
		t.Fatalf("matrix row missing policy delta: %+v", records[0].LiveDelta.ProxyLayer0Policy)
	}
	if len(records[0].LiveDelta.ProxyLayer0Cache) != 1 ||
		records[0].LiveDelta.ProxyLayer0Cache[0].Mechanism != "read_delta" ||
		records[0].LiveDelta.ProxyLayer0Cache[0].Count != 1 {
		t.Fatalf("matrix row missing cache delta: %+v", records[0].LiveDelta.ProxyLayer0Cache)
	}
}

func TestRunCodexCaptureRunRestartsAfterMutatedCompletion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")
	var mu sync.Mutex
	var calls []string
	restarted := make(chan struct{})
	closeRestarted := sync.Once{}
	starts := 0
	deps := codexCaptureRunDeps{
		now: func() time.Time {
			return time.Date(2026, 6, 11, 12, 0, starts, 0, time.UTC)
		},
		ensureNoDaemon: func(context.Context, codexCaptureRunFlags) error {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "preflight")
			return nil
		},
		startDaemon: func(ctx context.Context, flags codexCaptureRunFlags, stderr io.Writer) (*codexCaptureDaemon, error) {
			mu.Lock()
			defer mu.Unlock()
			starts++
			calls = append(calls, "start")
			return &codexCaptureDaemon{done: make(chan error)}, nil
		},
		waitHealth: func(ctx context.Context, flags codexCaptureRunFlags, daemonDone <-chan error) error {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "health")
			if starts == 2 {
				closeRestarted.Do(func() { close(restarted) })
			}
			return nil
		},
		adminSnapshot: func(context.Context, codexCaptureRunFlags) (codexCaptureAdminSnapshot, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "admin")
			return codexCaptureAdminSnapshot{}, nil
		},
		runCodex: func(ctx context.Context, flags codexCaptureRunFlags, stdout, stderr io.Writer) error {
			mu.Lock()
			calls = append(calls, "codex-start")
			mu.Unlock()
			writeJSONLFile(t, capturePath,
				map[string]any{
					"direction": "client_to_server",
					"mutated":   true,
					"payload": map[string]any{
						"type": "response.create",
						"input": []map[string]any{{
							"type":    "function_call_output",
							"call_id": "call_mutated",
							"output":  "mutated output",
						}},
					},
				},
				map[string]any{
					"direction": "server_to_client",
					"payload": map[string]any{
						"type": "response.completed",
					},
				},
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-restarted:
			case <-time.After(2 * time.Second):
				t.Fatal("daemon was not restarted after mutated completion")
			}
			mu.Lock()
			calls = append(calls, "codex-end")
			mu.Unlock()
			return nil
		},
		stopDaemon: func(context.Context, *codexCaptureDaemon) error {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "stop")
			return nil
		},
		replay: func(flags wssABReplayFlags) (wssABReplayReport, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "replay")
			return wssABReplayReport{Path: flags.path, Frames: 2, RequestTurns: 1, GatePassed: true}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runCodexCaptureRunWithDeps([]string{
		"--capture", capturePath,
		"--restart-after-mutated-completion", "1",
		"--", "Run E5 harness",
	}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	mu.Lock()
	got := strings.Join(calls, ",")
	mu.Unlock()
	for _, want := range []string{"preflight", "start", "health", "admin", "codex-start", "stop", "start", "health", "codex-end", "admin", "stop", "replay"} {
		if !strings.Contains(got, want) {
			t.Fatalf("calls missing %s: %s", want, got)
		}
	}
	startCount := 0
	stopCount := 0
	for _, call := range strings.Split(got, ",") {
		if call == "start" {
			startCount++
		}
		if call == "stop" {
			stopCount++
		}
	}
	if startCount != 2 || stopCount != 2 {
		t.Fatalf("restart lifecycle should have exactly two starts and stops: %s", got)
	}
	if !strings.Contains(stderr.String(), "capture daemon restarted after mutated completion 1") {
		t.Fatalf("restart note missing from stderr: %s", stderr.String())
	}
}

func TestCodexCaptureHasMutatedCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	writeJSONLFile(t, path,
		map[string]any{
			"direction": "server_to_client",
			"payload":   map[string]any{"type": "response.completed"},
		},
		map[string]any{
			"direction": "client_to_server",
			"mutated":   true,
			"payload":   map[string]any{"type": "response.create"},
		},
	)
	if codexCaptureHasMutatedCompletion(path, 1) {
		t.Fatal("completion before the target mutation must not satisfy the restart trigger")
	}
	appendJSONLFile(t, path,
		map[string]any{
			"direction": "server_to_client",
			"payload":   map[string]any{"type": "response.in_progress"},
		},
		map[string]any{
			"direction": "server_to_client",
			"payload":   map[string]any{"type": "response.completed"},
		},
	)
	if !codexCaptureHasMutatedCompletion(path, 1) {
		t.Fatal("completion after the target mutation should satisfy the restart trigger")
	}
	if codexCaptureHasMutatedCompletion(path, 2) {
		t.Fatal("target 2 should not fire with only one mutated client request")
	}
}

func TestPrepareCodexCaptureDaemonCommandDetachesSession(t *testing.T) {
	cmd := exec.Command("slimference", "daemon")
	prepareCodexCaptureDaemonCommand(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Fatalf("capture daemon must run in its own session, SysProcAttr=%+v", cmd.SysProcAttr)
	}
}

func TestRunCodexCaptureRunRejectsRestartWithResourceProof(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := runCodexCaptureRunWithDeps([]string{
		"--resource-profile-proof", filepath.Join(t.TempDir(), "resource"),
		"--restart-after-mutated-completion", "1",
		"--workload-class", "host_resource_long_workday",
		"--", "prompt",
	}, &stdout, &stderr, codexCaptureRunDeps{
		now: func() time.Time { return time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC) },
	})
	if code != 2 || !strings.Contains(stderr.String(), "cannot be combined with --resource-profile-proof") {
		t.Fatalf("expected restart/resource validation failure, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRunCodexCaptureRunAllowsFinalAdminFailureForRestartProof(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	capturePath := filepath.Join(t.TempDir(), "capture.jsonl")
	adminCalls := 0
	deps := codexCaptureRunDeps{
		now: func() time.Time { return time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC) },
		ensureNoDaemon: func(context.Context, codexCaptureRunFlags) error {
			return nil
		},
		startDaemon: func(context.Context, codexCaptureRunFlags, io.Writer) (*codexCaptureDaemon, error) {
			return &codexCaptureDaemon{done: make(chan error)}, nil
		},
		waitHealth: func(context.Context, codexCaptureRunFlags, <-chan error) error { return nil },
		adminSnapshot: func(context.Context, codexCaptureRunFlags) (codexCaptureAdminSnapshot, error) {
			adminCalls++
			if adminCalls == 1 {
				return codexCaptureAdminSnapshot{BillableInputTokensSaved: 7}, nil
			}
			return codexCaptureAdminSnapshot{}, errors.New("daemon reset after proof restart")
		},
		runCodex: func(context.Context, codexCaptureRunFlags, io.Writer, io.Writer) error {
			return nil
		},
		stopDaemon: func(context.Context, *codexCaptureDaemon) error { return nil },
		replay: func(flags wssABReplayFlags) (wssABReplayReport, error) {
			return wssABReplayReport{
				Path:            flags.path,
				Frames:          2,
				RequestTurns:    1,
				MutatedRequests: 1,
				GatePassed:      true,
			}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runCodexCaptureRunWithDeps([]string{
		"--capture", capturePath,
		"--restart-after-mutated-completion", "1",
		"--", "prompt",
	}, &stdout, &stderr, deps)
	if code != 0 || !strings.Contains(stderr.String(), "continuing with replay-only live delta") {
		t.Fatalf("expected replay-only success for restart proof, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "gate:          PASS") {
		t.Fatalf("summary missing replay gate after final admin failure:\n%s", stdout.String())
	}
}

func TestRunCodexCaptureRunWritesMatrixBeforeExpectedReducerFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	deps := codexCaptureRunDeps{
		now: func() time.Time {
			return time.Date(2026, 6, 4, 20, 30, 0, 0, time.UTC)
		},
		ensureNoDaemon: func(context.Context, codexCaptureRunFlags) error { return nil },
		startDaemon: func(context.Context, codexCaptureRunFlags, io.Writer) (*codexCaptureDaemon, error) {
			return &codexCaptureDaemon{done: make(chan error)}, nil
		},
		waitHealth: func(context.Context, codexCaptureRunFlags, <-chan error) error { return nil },
		adminSnapshot: func(ctx context.Context, flags codexCaptureRunFlags) (codexCaptureAdminSnapshot, error) {
			return codexCaptureAdminSnapshot{
				HostBudgetStatus:        "attention",
				HostBudgetExceeded:      true,
				HostBudgetReasons:       []string{"rss_budget_exceeded"},
				HostBudgetRSSBytes:      227590144,
				HostBudgetCompressionOK: true,
				HostBudgetDegradationOK: true,
			}, nil
		},
		runCodex:   func(context.Context, codexCaptureRunFlags, io.Writer, io.Writer) error { return nil },
		stopDaemon: func(context.Context, *codexCaptureDaemon) error { return nil },
		replay: func(flags wssABReplayFlags) (wssABReplayReport, error) {
			return wssABReplayReport{Path: flags.path, Frames: 3, RequestTurns: 1, GatePassed: true}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runCodexCaptureRunWithDeps([]string{
		"--capture", capturePath,
		"--matrix-row", matrixPath,
		"--id", "host-resource-negative",
		"--workload-class", "host_resource_long_workday",
		"--expected-reducer", "host_budget_ok",
		"--", "Run host resource workload",
	}, &stdout, &stderr, deps)
	if code != 3 || !strings.Contains(stderr.String(), "expected reducer host_budget_ok did not fire") {
		t.Fatalf("expected reducer failure after matrix write, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	records, err := readWSSProofMatrixRecords(matrixPath)
	if err != nil {
		t.Fatalf("read matrix row: %v", err)
	}
	if len(records) != 1 || records[0].ID != "host-resource-negative" {
		t.Fatalf("matrix row missing after expected reducer failure: %+v", records)
	}
	if records[0].LiveDelta == nil || !records[0].LiveDelta.HostBudgetExceeded ||
		records[0].LiveDelta.HostBudgetReasons[0] != "rss_budget_exceeded" {
		t.Fatalf("negative host-budget evidence not persisted: %+v", records[0].LiveDelta)
	}
}

func TestCodexCaptureAdminSnapshotParsesExtendedAdminState(t *testing.T) {
	state, err := parseCodexCaptureAdminStateJSON([]byte(`{
	  "savings": {
	    "billable_input_tokens_saved": 10,
	    "provider_cache_read_tokens": 700,
	    "provider_cache_create_tokens": 120,
	    "proxy_layer0_chunk_dedup_blocks": 1,
	    "proxy_layer0_chunk_dedup_references": 4,
	    "proxy_layer0_chunk_dedup_referenced_bytes": 8192,
	    "proxy_layer0_chunk_dedup_input_bytes": 16384,
	    "proxy_layer0_policy": [
	      {
	        "route": "wss_phasef",
	        "mechanism": "chunk_dedup",
	        "action": "allow",
	        "reason": "recoverable_chunk_dedup",
	        "count": 3
	      }
	    ],
	    "proxy_layer0_cache": [
	      {
	        "route": "wss_phasef",
	        "mechanism": "read_delta",
	        "action": "hit",
	        "reason": "unchanged",
	        "count": 2
	      }
	    ]
	  },
	  "wss": {
	    "phasef_bridged": 1,
	    "parse_failures": 0,
	    "degraded_sessions": 0,
	    "compression_errors": 0
	  },
	  "tool_prune": {
	    "pruned_total": 7,
	    "reattach_total": 2,
	    "miss_total": 1,
	    "retry_total": 1,
	    "always_keep_total": 5,
	    "disabled_sessions": 1,
	    "tokens_saved_sum": 120
	  },
	  "output_reduce": {
	    "injected_turns": 3,
	    "skipped_turns": 4,
	    "input_overhead_tokens": 9,
	    "output_tokens_observed": 200,
	    "downgrades": [{"bucket":"x"}]
	  },
	  "output_reduce_counters": {
	    "stop_seq_requests_modified": 1,
	    "streamcut_fired": 2,
	    "repdet_responses_rewritten": 3,
	    "stale_read_blocks_replaced": 4,
	    "obsolete_read_blocks_pruned": 5,
	    "beterse_injections": 6
	  },
	  "host_budget": {
	    "status": "ok",
	    "exceeded": false,
	    "rss_bytes": 123,
	    "cpu_window_percent": 1.5,
	    "cpu_window_seconds": 2.5,
	    "disk_write_ops_delta": 8,
	    "state_bytes": 456,
	    "compression_ok": true,
	    "degradation_ok": true
	  }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := codexCaptureAdminSnapshotFromState(state)
	if snapshot.BillableInputTokensSaved != 10 || snapshot.ProviderCacheReadTokens != 700 || snapshot.ProviderCacheCreateTokens != 120 {
		t.Fatalf("savings fields missing: %+v", snapshot)
	}
	if snapshot.ProxyLayer0ChunkRefs != 4 || snapshot.ProxyLayer0ChunkRefB != 8192 || snapshot.ProxyLayer0ChunkInB != 16384 {
		t.Fatalf("chunk fields missing: %+v", snapshot)
	}
	if len(snapshot.ProxyLayer0Policy) != 1 || snapshot.ProxyLayer0Policy[0].Count != 3 {
		t.Fatalf("policy fields missing: %+v", snapshot.ProxyLayer0Policy)
	}
	if len(snapshot.ProxyLayer0Cache) != 1 || snapshot.ProxyLayer0Cache[0].Count != 2 {
		t.Fatalf("cache fields missing: %+v", snapshot.ProxyLayer0Cache)
	}
	if snapshot.ToolPrunePruned != 7 || snapshot.ToolPruneReattach != 2 || snapshot.ToolPruneTokensSaved != 120 {
		t.Fatalf("tool prune fields missing: %+v", snapshot)
	}
	if snapshot.OutputReduceInjected != 3 || snapshot.OutputReduceDowngrades != 1 || snapshot.BeterseInjections != 6 {
		t.Fatalf("output reduce fields missing: %+v", snapshot)
	}
	if snapshot.HostBudgetStatus != "ok" || snapshot.HostBudgetRSSBytes != 123 || snapshot.HostBudgetCPUWindowSec != 2.5 || !snapshot.HostBudgetCompressionOK || !snapshot.HostBudgetDegradationOK {
		t.Fatalf("host budget fields missing: %+v", snapshot)
	}
}

func TestMergeCodexCaptureAdminStatusAddsToolPrune(t *testing.T) {
	state, err := parseCodexCaptureAdminStateJSON([]byte(`{
	  "savings": {"billable_input_tokens_saved": 99},
	  "host_budget": {"status": "ok", "compression_ok": true, "degradation_ok": true}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	status, err := parseCodexCaptureAdminStatusJSON([]byte(`{
	  "tool_prune": {
	    "pruned_total": 2,
	    "reattach_total": 1,
	    "miss_total": 0,
	    "retry_total": 0,
	    "always_keep_total": 17,
	    "disabled_sessions": 0,
	    "tokens_saved_sum": 42
	  },
	  "output_reduce": {
	    "injected_turns": 1,
	    "input_overhead_tokens": 5
	  },
	  "output_reduce_counters": {
	    "stop_seq_requests_modified": 3
	  }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	mergeCodexCaptureAdminStatus(&state, status)
	snapshot := codexCaptureAdminSnapshotFromState(state)
	if snapshot.BillableInputTokensSaved != 99 {
		t.Fatalf("state savings lost during merge: %+v", snapshot)
	}
	if snapshot.ToolPrunePruned != 2 || snapshot.ToolPruneReattach != 1 || snapshot.ToolPruneAlwaysKeep != 17 || snapshot.ToolPruneTokensSaved != 42 {
		t.Fatalf("status tool-prune fields missing after merge: %+v", snapshot)
	}
	if snapshot.OutputReduceInjected != 1 || snapshot.OutputReduceInputOverheadTokens != 5 || snapshot.StopSeqRequestsModified != 3 {
		t.Fatalf("status output-reduce fields missing after merge: %+v", snapshot)
	}
	if snapshot.HostBudgetStatus != "ok" || !snapshot.HostBudgetCompressionOK || !snapshot.HostBudgetDegradationOK {
		t.Fatalf("state host budget lost during merge: %+v", snapshot)
	}
}

func TestValidateCodexCaptureExpectedReducers(t *testing.T) {
	failures := validateCodexCaptureExpectedReducers([]string{"read_delta", "none", "read_delta"}, &codexCaptureLiveDelta{
		ProxyLayer0ReadDelta: 1,
	})
	if len(failures) != 0 {
		t.Fatalf("unexpected failures: %v", failures)
	}

	failures = validateCodexCaptureExpectedReducers([]string{"output_reduce_injected"}, &codexCaptureLiveDelta{})
	if len(failures) != 1 || !strings.Contains(failures[0], "output_reduce_injected did not fire") {
		t.Fatalf("expected missing reducer failure, got %v", failures)
	}

	failures = validateCodexCaptureExpectedReducers([]string{"does_not_exist"}, &codexCaptureLiveDelta{})
	if len(failures) != 1 || !strings.Contains(failures[0], "unknown expected reducer") {
		t.Fatalf("expected unknown reducer failure, got %v", failures)
	}
}

func TestAugmentCodexCaptureLiveDeltaFromWireOutputReduce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	data := strings.Join([]string{
		`{"direction":"c2s","payload":{"type":"response.create","instructions":"base"}}`,
		`{"direction":"s2c","payload":{"type":"response.completed","response":{"usage":{"output_tokens":42}}}}`,
		`{"direction":"server_to_client","payload":"{\"usage\":{\"completion_tokens\":7}}"}`,
		`{"direction":"s2c","payload":{"type":"response.created","response":{"instructions":"base\n\n#slimference-output-rules\nAnswer directly."}}}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	live := augmentCodexCaptureLiveDeltaFromWire(path, &codexCaptureLiveDelta{})
	if live.OutputReduceInjected != 1 {
		t.Fatalf("OutputReduceInjected = %d, want wire evidence hit", live.OutputReduceInjected)
	}
	if live.ProviderOutputTokens != 49 {
		t.Fatalf("ProviderOutputTokens = %d, want wire usage total", live.ProviderOutputTokens)
	}
	if failures := validateCodexCaptureExpectedReducers([]string{"output_reduce_injected"}, live); len(failures) != 0 {
		t.Fatalf("wire evidence should satisfy output reduce reducer: %v", failures)
	}

	live = augmentCodexCaptureLiveDeltaFromWire(path, &codexCaptureLiveDelta{OutputReduceInjected: 3, ProviderOutputTokens: 11})
	if live.OutputReduceInjected != 3 || live.ProviderOutputTokens != 11 {
		t.Fatalf("existing live counters overwritten: %+v", live)
	}
}

func TestWaitCodexCaptureAggregateReportWithHostWindow(t *testing.T) {
	t.Parallel()
	calls := 0
	got, err := waitCodexCaptureAggregateReportWithHostWindow(context.Background(), codexCaptureRunFlags{}, time.Second, func(codexCaptureRunFlags) (aggregateSavingsReport, string, error) {
		calls++
		report := aggregateSavingsReport{Generated: time.Date(2026, 6, 4, 21, 0, calls, 0, time.UTC)}
		if calls >= 2 {
			report.HostBudget.CPUWindowSeconds = 1.25
			report.HostBudget.Status = "ok"
		}
		return report, "", nil
	})
	if err != nil {
		t.Fatalf("waitCodexCaptureAggregateReportWithHostWindow: %v", err)
	}
	if calls < 2 || got.HostBudget.CPUWindowSeconds != 1.25 {
		t.Fatalf("did not wait for measured host window: calls=%d report=%+v", calls, got.HostBudget)
	}
}

func TestWaitCodexCaptureAggregateReportWithHostWindowFailsClosed(t *testing.T) {
	t.Parallel()
	_, err := waitCodexCaptureAggregateReportWithHostWindow(context.Background(), codexCaptureRunFlags{}, time.Nanosecond, func(codexCaptureRunFlags) (aggregateSavingsReport, string, error) {
		return aggregateSavingsReport{HostBudget: aggregateHostBudgetBlock{Status: "ok"}}, "", nil
	})
	if err == nil || !strings.Contains(err.Error(), "host_budget cpu_window_seconds stayed") {
		t.Fatalf("expected fail-closed missing host window error, got %v", err)
	}
	_, err = waitCodexCaptureAggregateReportWithHostWindow(context.Background(), codexCaptureRunFlags{}, time.Second, nil)
	if err == nil || !strings.Contains(err.Error(), "loader is nil") {
		t.Fatalf("expected nil loader error, got %v", err)
	}
}

func TestDeltaCodexCaptureAdminSnapshotIncludesPolicyAndCacheDeltas(t *testing.T) {
	base := codexCaptureAdminSnapshot{
		ProxyLayer0Policy: []control.ProxyLayer0PolicyEntry{
			{Route: "wss_phasef", Mechanism: "chunk_dedup", Action: "block", Reason: "below_min_bytes", BlockReason: "below_min_bytes", Count: 2},
			{Route: "wss_phasef", Mechanism: "read_delta", Action: "allow", Reason: "lossless_or_exact_reducer", Count: 5},
		},
		ProxyLayer0Cache: []control.ProxyLayer0CacheEntry{
			{Route: "wss_phasef", Mechanism: "read_delta", Action: "miss", Reason: "first_observation_seeded", Count: 1},
		},
	}
	current := codexCaptureAdminSnapshot{
		ProxyLayer0Policy: []control.ProxyLayer0PolicyEntry{
			{Route: "wss_phasef", Mechanism: "chunk_dedup", Action: "block", Reason: "below_min_bytes", BlockReason: "below_min_bytes", Count: 2},
			{Route: "wss_phasef", Mechanism: "chunk_dedup", Action: "allow", Reason: "recoverable_chunk_dedup", Count: 3},
			{Route: "wss_phasef", Mechanism: "read_delta", Action: "allow", Reason: "lossless_or_exact_reducer", Count: 7},
		},
		ProxyLayer0Cache: []control.ProxyLayer0CacheEntry{
			{Route: "wss_phasef", Mechanism: "read_delta", Action: "miss", Reason: "first_observation_seeded", Count: 2},
			{Route: "wss_phasef", Mechanism: "read_delta", Action: "hit", Reason: "unchanged", Count: 1},
		},
	}
	delta := deltaCodexCaptureAdminSnapshot(base, current)
	if len(delta.ProxyLayer0Policy) != 2 {
		t.Fatalf("policy delta count = %d: %+v", len(delta.ProxyLayer0Policy), delta.ProxyLayer0Policy)
	}
	policyCounts := map[string]int64{}
	for _, entry := range delta.ProxyLayer0Policy {
		policyCounts[entry.Mechanism+"/"+entry.Action+"/"+entry.Reason] = entry.Count
	}
	if policyCounts["chunk_dedup/allow/recoverable_chunk_dedup"] != 3 ||
		policyCounts["read_delta/allow/lossless_or_exact_reducer"] != 2 {
		t.Fatalf("policy delta mismatch: %+v", delta.ProxyLayer0Policy)
	}
	if len(delta.ProxyLayer0Cache) != 2 {
		t.Fatalf("cache delta count = %d: %+v", len(delta.ProxyLayer0Cache), delta.ProxyLayer0Cache)
	}
	cacheCounts := map[string]int64{}
	for _, entry := range delta.ProxyLayer0Cache {
		cacheCounts[entry.Mechanism+"/"+entry.Action+"/"+entry.Reason] = entry.Count
	}
	if cacheCounts["read_delta/miss/first_observation_seeded"] != 1 ||
		cacheCounts["read_delta/hit/unchanged"] != 1 {
		t.Fatalf("cache delta mismatch: %+v", delta.ProxyLayer0Cache)
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
		abPairID:            "output-ab-1",
		abVariant:           "directive",
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
	if !strings.Contains(string(data), `"ab_pair_id":"output-ab-1"`) ||
		!strings.Contains(string(data), `"ab_variant":"directive"`) {
		t.Fatalf("matrix row missing A/B metadata: %s", string(data))
	}
}

func TestValidateABProofFlags(t *testing.T) {
	t.Parallel()
	if err := validateABProofFlags("pair", "baseline"); err != nil {
		t.Fatalf("baseline should pass: %v", err)
	}
	if err := validateABProofFlags("pair", "directive"); err != nil {
		t.Fatalf("directive should pass: %v", err)
	}
	for _, tc := range []struct {
		pair    string
		variant string
	}{
		{pair: "pair"},
		{variant: "baseline"},
		{pair: "pair", variant: "other"},
	} {
		if err := validateABProofFlags(tc.pair, tc.variant); err == nil {
			t.Fatalf("expected invalid A/B flags to fail: %+v", tc)
		}
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

func TestWatchCodexCaptureFunctionOutputMarkerIgnoresPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	promptOnly := `{"payload":{"type":"response.create","input":[{"type":"message","content":"CAPTURE_DONE in prompt"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(promptOnly), 0o600); err != nil {
		t.Fatal(err)
	}
	hit := make(chan struct{})
	stop := make(chan struct{})
	go watchCodexCaptureFunctionOutputMarker(path, "CAPTURE_DONE", 1, func() { close(hit) }, stop)
	select {
	case <-hit:
		t.Fatal("marker fired from prompt content")
	case <-time.After(150 * time.Millisecond):
	}
	withOutput := promptOnly + `{"payload":{"type":"response.create","input":[{"type":"function_call_output","output":"tool says CAPTURE_DONE"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(withOutput), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hit:
	case <-time.After(time.Second):
		t.Fatal("marker did not fire from function_call_output")
	}
	close(stop)
}

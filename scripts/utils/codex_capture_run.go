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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/slimference/slimference/internal/compression"
	"github.com/slimference/slimference/internal/control"
)

type codexCaptureRunFlags struct {
	binary              string
	capturePath         string
	host                string
	port                string
	transport           string
	healthTimeout       time.Duration
	codexTimeout        time.Duration
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
	quietCodexOutput    bool
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
	adminSnapshot  func(context.Context, codexCaptureRunFlags) (codexCaptureAdminSnapshot, error)
	runCodex       func(context.Context, codexCaptureRunFlags, io.Writer, io.Writer) error
	stopDaemon     func(context.Context, *codexCaptureDaemon) error
	replay         func(wssABReplayFlags) (wssABReplayReport, error)
}

type codexCaptureDaemon struct {
	cmd  *exec.Cmd
	done <-chan error
}

type codexCaptureRunResult struct {
	CapturePath string                 `json:"capture_path"`
	MatrixPath  string                 `json:"matrix_path,omitempty"`
	Replay      wssABReplayReport      `json:"replay"`
	LiveDelta   *codexCaptureLiveDelta `json:"live_delta,omitempty"`
	StartedAt   string                 `json:"started_at"`
	EndedAt     string                 `json:"ended_at"`
}

type codexCaptureAdminSnapshot struct {
	BillableInputTokensSaved int64 `json:"billable_input_tokens_saved"`
	InputTokensSaved         int64 `json:"input_tokens_saved"`
	OutputWireBytesSaved     int64 `json:"output_wire_bytes_saved"`
	RequestSideBytesReduced  int64 `json:"request_side_bytes_reduced"`

	PhasefBridged             int64 `json:"phasef_bridged"`
	CompressedMessagesMutated int64 `json:"compressed_messages_mutated"`
	FramesReencoded           int64 `json:"frames_reencoded"`
	PhasefMutations           int64 `json:"phasef_mutations"`

	ProxyLayer0ReadDelta  int64                            `json:"proxy_layer0_read_delta_blocks"`
	ProxyLayer0Captured   int64                            `json:"proxy_layer0_captured_output_blocks"`
	ProxyLayer0Envelope   int64                            `json:"proxy_layer0_codex_exec_envelope_blocks"`
	ProxyLayer0Repeated   int64                            `json:"proxy_layer0_repeated_output_blocks"`
	ProxyLayer0ChunkDedup int64                            `json:"proxy_layer0_chunk_dedup_blocks"`
	ProxyLayer0ChunkRefs  int64                            `json:"proxy_layer0_chunk_dedup_references"`
	ProxyLayer0ChunkRefB  int64                            `json:"proxy_layer0_chunk_dedup_referenced_bytes"`
	ProxyLayer0ChunkInB   int64                            `json:"proxy_layer0_chunk_dedup_input_bytes"`
	ProxyLayer0Policy     []control.ProxyLayer0PolicyEntry `json:"proxy_layer0_policy,omitempty"`
	ProxyLayer0Cache      []control.ProxyLayer0CacheEntry  `json:"proxy_layer0_cache,omitempty"`

	ToolPrunePruned      int64 `json:"tool_prune_pruned_total"`
	ToolPruneReattach    int64 `json:"tool_prune_reattach_total"`
	ToolPruneMiss        int64 `json:"tool_prune_miss_total"`
	ToolPruneRetry       int64 `json:"tool_prune_retry_total"`
	ToolPruneAlwaysKeep  int64 `json:"tool_prune_always_keep_total"`
	ToolPruneDisabled    int64 `json:"tool_prune_disabled_sessions"`
	ToolPruneTokensSaved int64 `json:"tool_prune_tokens_saved_sum"`

	OutputReduceInjected             int64 `json:"output_reduce_injected_turns"`
	OutputReduceSkipped              int64 `json:"output_reduce_skipped_turns"`
	OutputReduceInputOverheadTokens  int64 `json:"output_reduce_input_overhead_tokens"`
	OutputReduceOutputTokensObserved int64 `json:"output_reduce_output_tokens_observed"`
	OutputReduceDowngrades           int64 `json:"output_reduce_downgrades"`
	StopSeqRequestsModified          int64 `json:"stop_seq_requests_modified"`
	StreamcutFired                   int64 `json:"streamcut_fired"`
	RepdetResponsesRewritten         int64 `json:"repdet_responses_rewritten"`
	StaleReadBlocksReplaced          int64 `json:"stale_read_blocks_replaced"`
	ObsoleteReadBlocksPruned         int64 `json:"obsolete_read_blocks_pruned"`
	BeterseInjections                int64 `json:"beterse_injections"`

	ParseFailures     int64 `json:"parse_failures"`
	DegradedSessions  int64 `json:"degraded_sessions"`
	CompressionErrors int64 `json:"compression_errors"`

	HostBudgetStatus        string   `json:"host_budget_status,omitempty"`
	HostBudgetExceeded      bool     `json:"host_budget_exceeded,omitempty"`
	HostBudgetReasons       []string `json:"host_budget_reasons,omitempty"`
	HostBudgetRSSBytes      int64    `json:"host_budget_rss_bytes,omitempty"`
	HostBudgetCPUWindowPct  float64  `json:"host_budget_cpu_window_percent,omitempty"`
	HostBudgetCPUWindowSec  float64  `json:"host_budget_cpu_window_seconds,omitempty"`
	HostBudgetDiskWriteOps  int64    `json:"host_budget_disk_write_ops_delta,omitempty"`
	HostBudgetStateBytes    int64    `json:"host_budget_state_bytes,omitempty"`
	HostBudgetCompressionOK bool     `json:"host_budget_compression_ok,omitempty"`
	HostBudgetDegradationOK bool     `json:"host_budget_degradation_ok,omitempty"`
}

type codexCaptureLiveDelta struct {
	BillableInputTokensSaved int64 `json:"billable_input_tokens_saved"`
	InputTokensSaved         int64 `json:"input_tokens_saved"`
	OutputWireBytesSaved     int64 `json:"output_wire_bytes_saved"`
	RequestSideBytesReduced  int64 `json:"request_side_bytes_reduced"`

	PhasefBridged             int64 `json:"phasef_bridged"`
	CompressedMessagesMutated int64 `json:"compressed_messages_mutated"`
	FramesReencoded           int64 `json:"frames_reencoded"`
	PhasefMutations           int64 `json:"phasef_mutations"`

	ProxyLayer0ReadDelta  int64                            `json:"proxy_layer0_read_delta_blocks"`
	ProxyLayer0Captured   int64                            `json:"proxy_layer0_captured_output_blocks"`
	ProxyLayer0Envelope   int64                            `json:"proxy_layer0_codex_exec_envelope_blocks"`
	ProxyLayer0Repeated   int64                            `json:"proxy_layer0_repeated_output_blocks"`
	ProxyLayer0ChunkDedup int64                            `json:"proxy_layer0_chunk_dedup_blocks"`
	ProxyLayer0ChunkRefs  int64                            `json:"proxy_layer0_chunk_dedup_references"`
	ProxyLayer0ChunkRefB  int64                            `json:"proxy_layer0_chunk_dedup_referenced_bytes"`
	ProxyLayer0ChunkInB   int64                            `json:"proxy_layer0_chunk_dedup_input_bytes"`
	ProxyLayer0Policy     []control.ProxyLayer0PolicyEntry `json:"proxy_layer0_policy,omitempty"`
	ProxyLayer0Cache      []control.ProxyLayer0CacheEntry  `json:"proxy_layer0_cache,omitempty"`

	ToolPrunePruned      int64 `json:"tool_prune_pruned_total"`
	ToolPruneReattach    int64 `json:"tool_prune_reattach_total"`
	ToolPruneMiss        int64 `json:"tool_prune_miss_total"`
	ToolPruneRetry       int64 `json:"tool_prune_retry_total"`
	ToolPruneAlwaysKeep  int64 `json:"tool_prune_always_keep_total"`
	ToolPruneDisabled    int64 `json:"tool_prune_disabled_sessions"`
	ToolPruneTokensSaved int64 `json:"tool_prune_tokens_saved_sum"`

	OutputReduceInjected             int64 `json:"output_reduce_injected_turns"`
	OutputReduceSkipped              int64 `json:"output_reduce_skipped_turns"`
	OutputReduceInputOverheadTokens  int64 `json:"output_reduce_input_overhead_tokens"`
	OutputReduceOutputTokensObserved int64 `json:"output_reduce_output_tokens_observed"`
	OutputReduceDowngrades           int64 `json:"output_reduce_downgrades"`
	StopSeqRequestsModified          int64 `json:"stop_seq_requests_modified"`
	StreamcutFired                   int64 `json:"streamcut_fired"`
	RepdetResponsesRewritten         int64 `json:"repdet_responses_rewritten"`
	StaleReadBlocksReplaced          int64 `json:"stale_read_blocks_replaced"`
	ObsoleteReadBlocksPruned         int64 `json:"obsolete_read_blocks_pruned"`
	BeterseInjections                int64 `json:"beterse_injections"`

	ParseFailures     int64 `json:"parse_failures"`
	DegradedSessions  int64 `json:"degraded_sessions"`
	CompressionErrors int64 `json:"compression_errors"`

	HostBudgetStatus        string   `json:"host_budget_status,omitempty"`
	HostBudgetExceeded      bool     `json:"host_budget_exceeded,omitempty"`
	HostBudgetReasons       []string `json:"host_budget_reasons,omitempty"`
	HostBudgetRSSBytes      int64    `json:"host_budget_rss_bytes,omitempty"`
	HostBudgetCPUWindowPct  float64  `json:"host_budget_cpu_window_percent,omitempty"`
	HostBudgetCPUWindowSec  float64  `json:"host_budget_cpu_window_seconds,omitempty"`
	HostBudgetDiskWriteOps  int64    `json:"host_budget_disk_write_ops_delta,omitempty"`
	HostBudgetStateBytes    int64    `json:"host_budget_state_bytes,omitempty"`
	HostBudgetCompressionOK bool     `json:"host_budget_compression_ok,omitempty"`
	HostBudgetDegradationOK bool     `json:"host_budget_degradation_ok,omitempty"`
}

type codexCaptureAdminState struct {
	control.SetupState
	ToolPrune            codexCaptureToolPruneSnapshot            `json:"tool_prune"`
	OutputReduce         codexCaptureOutputReduceSnapshot         `json:"output_reduce"`
	OutputReduceCounters codexCaptureOutputReduceCountersSnapshot `json:"output_reduce_counters"`
}

type codexCaptureToolPruneSnapshot struct {
	PrunedTotal      int64 `json:"pruned_total"`
	ReattachTotal    int64 `json:"reattach_total"`
	MissTotal        int64 `json:"miss_total"`
	RetryTotal       int64 `json:"retry_total"`
	AlwaysKeepTotal  int64 `json:"always_keep_total"`
	DisabledSessions int   `json:"disabled_sessions"`
	TokensSavedSum   int64 `json:"tokens_saved_sum"`
}

type codexCaptureOutputReduceSnapshot struct {
	InjectedTurns        int64             `json:"injected_turns"`
	SkippedTurns         int64             `json:"skipped_turns"`
	InputOverheadTokens  int64             `json:"input_overhead_tokens"`
	OutputTokensObserved int64             `json:"output_tokens_observed"`
	Downgrades           []json.RawMessage `json:"downgrades,omitempty"`
}

type codexCaptureOutputReduceCountersSnapshot struct {
	StopSeqRequestsModified  uint64 `json:"stop_seq_requests_modified"`
	StreamcutFired           uint64 `json:"streamcut_fired"`
	RepdetResponsesRewritten uint64 `json:"repdet_responses_rewritten"`
	StaleReadBlocksReplaced  uint64 `json:"stale_read_blocks_replaced"`
	ObsoleteReadBlocksPruned uint64 `json:"obsolete_read_blocks_pruned"`
	BeterseInjections        uint64 `json:"beterse_injections"`
}

const codexCaptureRunHelpText = `codex-capture-run: run a scoped Codex CLI capture with a managed foreground daemon

Usage:
  go run ./scripts/utils codex-capture-run [flags] -- <codex run args...>

Flags:
  --binary PATH              Slimference binary to run (default: slimference)
  --capture PATH             WSS frame capture path (default: ~/.slimference/captures/codex-capture-<timestamp>.jsonl)
  --host HOST                Daemon host (default: 127.0.0.1)
  --port PORT                Daemon port (default: 8990)
  --transport VALUE          Scoped Codex transport: auto, http, wss, wss-bridge, or direct (default: auto)
  --health-timeout DURATION  Time to wait for daemon /health (default: 10s)
  --codex-timeout DURATION   Max runtime for the scoped Codex command (default: 5m)
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
                             The marker is also watched in captured function_call_output
                             frames, so quiet TUI output cannot hide it.
  --exit-marker-count N      Required marker occurrences before interrupt (default: 1)
  --quiet-codex-output       Hide Codex TUI output and print only the final summary

The tool starts the daemon as its own child process with SLIMFERENCE_WSS_AB_CAPTURE
set, waits for /health, runs "slimference codex run --transport=<value> -- ...",
records live admin-state token deltas, stops the daemon, then replays the
capture with --fail-on-lost semantics. Live billable input-token savings are the
product savings signal; replay bytes are only the model-facing regression/safety
proxy. It does not use a detached background daemon, because detached shell
starts are too fragile for unattended release captures.`

func runCodexCaptureRun(args []string, stdout, stderr io.Writer) int {
	deps := codexCaptureRunDeps{
		now:            time.Now,
		ensureNoDaemon: ensureNoCodexCaptureDaemon,
		startDaemon:    startCodexCaptureDaemon,
		waitHealth:     waitCodexCaptureHealth,
		adminSnapshot:  loadCodexCaptureAdminSnapshot,
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
	before, err := deps.adminSnapshot(ctx, flags)
	if err != nil {
		fmt.Fprintf(stderr, "read initial admin state: %v\n", err)
		return 1
	}
	runStdout := stdout
	runStderr := stderr
	if flags.quietCodexOutput {
		runStdout = io.Discard
		runStderr = io.Discard
	}
	runCtx, cancelRun := context.WithTimeout(ctx, flags.codexTimeout)
	err = deps.runCodex(runCtx, flags, runStdout, runStderr)
	cancelRun()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	after, err := deps.adminSnapshot(ctx, flags)
	if err != nil {
		fmt.Fprintf(stderr, "read final admin state: %v\n", err)
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
		LiveDelta:   deltaCodexCaptureAdminSnapshot(before, after),
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
		transport:       "auto",
		healthTimeout:   10 * time.Second,
		codexTimeout:    5 * time.Minute,
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
		case arg == "--quiet-codex-output":
			flags.quietCodexOutput = true
		case arg == "--binary", arg == "--capture", arg == "--host", arg == "--port", arg == "--transport",
			arg == "--health-timeout", arg == "--codex-timeout", arg == "--matrix-row", arg == "--id",
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
	flags.transport = strings.ToLower(strings.TrimSpace(flags.transport))
	if !validCodexCaptureTransport(flags.transport) {
		return flags, fmt.Errorf("--transport must be auto, http, wss, wss-bridge, or direct")
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
	case "--transport":
		flags.transport = value
	case "--health-timeout":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("--health-timeout must be a positive duration")
		}
		flags.healthTimeout = d
	case "--codex-timeout":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("--codex-timeout must be a positive duration")
		}
		flags.codexTimeout = d
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

func validCodexCaptureTransport(transport string) bool {
	switch transport {
	case "auto", "http", "wss", "wss-bridge", "direct":
		return true
	default:
		return false
	}
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

func loadCodexCaptureAdminSnapshot(ctx context.Context, flags codexCaptureRunFlags) (codexCaptureAdminSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := "http://" + flags.host + ":" + flags.port + "/_slimference/admin/state"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return codexCaptureAdminSnapshot{}, fmt.Errorf("build admin state request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return codexCaptureAdminSnapshot{}, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return codexCaptureAdminSnapshot{}, fmt.Errorf("admin state returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return codexCaptureAdminSnapshot{}, fmt.Errorf("read admin state body: %w", err)
	}
	state, err := parseCodexCaptureAdminStateJSON(data)
	if err != nil {
		return codexCaptureAdminSnapshot{}, err
	}
	return codexCaptureAdminSnapshotFromState(state), nil
}

func parseCodexCaptureAdminStateJSON(data []byte) (codexCaptureAdminState, error) {
	var state codexCaptureAdminState
	if err := json.Unmarshal(data, &state); err != nil {
		return codexCaptureAdminState{}, fmt.Errorf("parse admin state JSON: %w", err)
	}
	return state, nil
}

func codexCaptureAdminSnapshotFromState(setup codexCaptureAdminState) codexCaptureAdminSnapshot {
	return codexCaptureAdminSnapshot{
		BillableInputTokensSaved: setup.Savings.BillableInputTokensSaved,
		InputTokensSaved:         setup.Savings.InputTokensSaved,
		OutputWireBytesSaved:     setup.Savings.OutputWireBytesSaved,
		RequestSideBytesReduced:  setup.Savings.RequestSideBytesReduced,

		PhasefBridged:             setup.WSS.PhasefBridged,
		CompressedMessagesMutated: setup.WSS.CompressedMessagesMutated,
		FramesReencoded:           setup.WSS.FramesReencoded,
		PhasefMutations:           setup.WSS.PhaseFMutations,

		ProxyLayer0ReadDelta:  setup.Savings.ProxyLayer0ReadDelta,
		ProxyLayer0Captured:   setup.Savings.ProxyLayer0Captured,
		ProxyLayer0Envelope:   setup.Savings.ProxyLayer0Envelope,
		ProxyLayer0Repeated:   setup.Savings.ProxyLayer0Repeated,
		ProxyLayer0ChunkDedup: setup.Savings.ProxyLayer0ChunkDedup,
		ProxyLayer0ChunkRefs:  setup.Savings.ProxyLayer0ChunkRefs,
		ProxyLayer0ChunkRefB:  setup.Savings.ProxyLayer0ChunkRefBytes,
		ProxyLayer0ChunkInB:   setup.Savings.ProxyLayer0ChunkInBytes,
		ProxyLayer0Policy:     append([]control.ProxyLayer0PolicyEntry(nil), setup.Savings.ProxyLayer0Policy...),
		ProxyLayer0Cache:      append([]control.ProxyLayer0CacheEntry(nil), setup.Savings.ProxyLayer0Cache...),

		ToolPrunePruned:      setup.ToolPrune.PrunedTotal,
		ToolPruneReattach:    setup.ToolPrune.ReattachTotal,
		ToolPruneMiss:        setup.ToolPrune.MissTotal,
		ToolPruneRetry:       setup.ToolPrune.RetryTotal,
		ToolPruneAlwaysKeep:  setup.ToolPrune.AlwaysKeepTotal,
		ToolPruneDisabled:    int64(setup.ToolPrune.DisabledSessions),
		ToolPruneTokensSaved: setup.ToolPrune.TokensSavedSum,

		OutputReduceInjected:             setup.OutputReduce.InjectedTurns,
		OutputReduceSkipped:              setup.OutputReduce.SkippedTurns,
		OutputReduceInputOverheadTokens:  setup.OutputReduce.InputOverheadTokens,
		OutputReduceOutputTokensObserved: setup.OutputReduce.OutputTokensObserved,
		OutputReduceDowngrades:           int64(len(setup.OutputReduce.Downgrades)),
		StopSeqRequestsModified:          int64(setup.OutputReduceCounters.StopSeqRequestsModified),
		StreamcutFired:                   int64(setup.OutputReduceCounters.StreamcutFired),
		RepdetResponsesRewritten:         int64(setup.OutputReduceCounters.RepdetResponsesRewritten),
		StaleReadBlocksReplaced:          int64(setup.OutputReduceCounters.StaleReadBlocksReplaced),
		ObsoleteReadBlocksPruned:         int64(setup.OutputReduceCounters.ObsoleteReadBlocksPruned),
		BeterseInjections:                int64(setup.OutputReduceCounters.BeterseInjections),

		ParseFailures:     setup.WSS.ParseFailures,
		DegradedSessions:  setup.WSS.DegradedSessions,
		CompressionErrors: setup.WSS.CompressionErrors,

		HostBudgetStatus:        setup.HostBudget.Status,
		HostBudgetExceeded:      setup.HostBudget.Exceeded,
		HostBudgetReasons:       append([]string(nil), setup.HostBudget.Reasons...),
		HostBudgetRSSBytes:      setup.HostBudget.RSSBytes,
		HostBudgetCPUWindowPct:  setup.HostBudget.CPUWindowPercent,
		HostBudgetCPUWindowSec:  setup.HostBudget.CPUWindowSeconds,
		HostBudgetDiskWriteOps:  setup.HostBudget.DiskWriteOpsDelta,
		HostBudgetStateBytes:    setup.HostBudget.StateBytes,
		HostBudgetCompressionOK: setup.HostBudget.CompressionOK,
		HostBudgetDegradationOK: setup.HostBudget.DegradationOK,
	}
}

func deltaCodexCaptureAdminSnapshot(base, current codexCaptureAdminSnapshot) *codexCaptureLiveDelta {
	return &codexCaptureLiveDelta{
		BillableInputTokensSaved: nonNegativeDelta(current.BillableInputTokensSaved, base.BillableInputTokensSaved),
		InputTokensSaved:         nonNegativeDelta(current.InputTokensSaved, base.InputTokensSaved),
		OutputWireBytesSaved:     nonNegativeDelta(current.OutputWireBytesSaved, base.OutputWireBytesSaved),
		RequestSideBytesReduced:  nonNegativeDelta(current.RequestSideBytesReduced, base.RequestSideBytesReduced),

		PhasefBridged:             nonNegativeDelta(current.PhasefBridged, base.PhasefBridged),
		CompressedMessagesMutated: nonNegativeDelta(current.CompressedMessagesMutated, base.CompressedMessagesMutated),
		FramesReencoded:           nonNegativeDelta(current.FramesReencoded, base.FramesReencoded),
		PhasefMutations:           nonNegativeDelta(current.PhasefMutations, base.PhasefMutations),

		ProxyLayer0ReadDelta:  nonNegativeDelta(current.ProxyLayer0ReadDelta, base.ProxyLayer0ReadDelta),
		ProxyLayer0Captured:   nonNegativeDelta(current.ProxyLayer0Captured, base.ProxyLayer0Captured),
		ProxyLayer0Envelope:   nonNegativeDelta(current.ProxyLayer0Envelope, base.ProxyLayer0Envelope),
		ProxyLayer0Repeated:   nonNegativeDelta(current.ProxyLayer0Repeated, base.ProxyLayer0Repeated),
		ProxyLayer0ChunkDedup: nonNegativeDelta(current.ProxyLayer0ChunkDedup, base.ProxyLayer0ChunkDedup),
		ProxyLayer0ChunkRefs:  nonNegativeDelta(current.ProxyLayer0ChunkRefs, base.ProxyLayer0ChunkRefs),
		ProxyLayer0ChunkRefB:  nonNegativeDelta(current.ProxyLayer0ChunkRefB, base.ProxyLayer0ChunkRefB),
		ProxyLayer0ChunkInB:   nonNegativeDelta(current.ProxyLayer0ChunkInB, base.ProxyLayer0ChunkInB),
		ProxyLayer0Policy:     deltaCodexCapturePolicyEntries(base.ProxyLayer0Policy, current.ProxyLayer0Policy),
		ProxyLayer0Cache:      deltaCodexCaptureCacheEntries(base.ProxyLayer0Cache, current.ProxyLayer0Cache),

		ToolPrunePruned:      nonNegativeDelta(current.ToolPrunePruned, base.ToolPrunePruned),
		ToolPruneReattach:    nonNegativeDelta(current.ToolPruneReattach, base.ToolPruneReattach),
		ToolPruneMiss:        nonNegativeDelta(current.ToolPruneMiss, base.ToolPruneMiss),
		ToolPruneRetry:       nonNegativeDelta(current.ToolPruneRetry, base.ToolPruneRetry),
		ToolPruneAlwaysKeep:  nonNegativeDelta(current.ToolPruneAlwaysKeep, base.ToolPruneAlwaysKeep),
		ToolPruneDisabled:    nonNegativeDelta(current.ToolPruneDisabled, base.ToolPruneDisabled),
		ToolPruneTokensSaved: nonNegativeDelta(current.ToolPruneTokensSaved, base.ToolPruneTokensSaved),

		OutputReduceInjected:             nonNegativeDelta(current.OutputReduceInjected, base.OutputReduceInjected),
		OutputReduceSkipped:              nonNegativeDelta(current.OutputReduceSkipped, base.OutputReduceSkipped),
		OutputReduceInputOverheadTokens:  nonNegativeDelta(current.OutputReduceInputOverheadTokens, base.OutputReduceInputOverheadTokens),
		OutputReduceOutputTokensObserved: nonNegativeDelta(current.OutputReduceOutputTokensObserved, base.OutputReduceOutputTokensObserved),
		OutputReduceDowngrades:           nonNegativeDelta(current.OutputReduceDowngrades, base.OutputReduceDowngrades),
		StopSeqRequestsModified:          nonNegativeDelta(current.StopSeqRequestsModified, base.StopSeqRequestsModified),
		StreamcutFired:                   nonNegativeDelta(current.StreamcutFired, base.StreamcutFired),
		RepdetResponsesRewritten:         nonNegativeDelta(current.RepdetResponsesRewritten, base.RepdetResponsesRewritten),
		StaleReadBlocksReplaced:          nonNegativeDelta(current.StaleReadBlocksReplaced, base.StaleReadBlocksReplaced),
		ObsoleteReadBlocksPruned:         nonNegativeDelta(current.ObsoleteReadBlocksPruned, base.ObsoleteReadBlocksPruned),
		BeterseInjections:                nonNegativeDelta(current.BeterseInjections, base.BeterseInjections),

		ParseFailures:     nonNegativeDelta(current.ParseFailures, base.ParseFailures),
		DegradedSessions:  nonNegativeDelta(current.DegradedSessions, base.DegradedSessions),
		CompressionErrors: nonNegativeDelta(current.CompressionErrors, base.CompressionErrors),

		HostBudgetStatus:        current.HostBudgetStatus,
		HostBudgetExceeded:      current.HostBudgetExceeded,
		HostBudgetReasons:       append([]string(nil), current.HostBudgetReasons...),
		HostBudgetRSSBytes:      current.HostBudgetRSSBytes,
		HostBudgetCPUWindowPct:  current.HostBudgetCPUWindowPct,
		HostBudgetCPUWindowSec:  current.HostBudgetCPUWindowSec,
		HostBudgetDiskWriteOps:  current.HostBudgetDiskWriteOps,
		HostBudgetStateBytes:    current.HostBudgetStateBytes,
		HostBudgetCompressionOK: current.HostBudgetCompressionOK,
		HostBudgetDegradationOK: current.HostBudgetDegradationOK,
	}
}

func deltaCodexCapturePolicyEntries(base, current []control.ProxyLayer0PolicyEntry) []control.ProxyLayer0PolicyEntry {
	baseCounts := make(map[string]int64, len(base))
	for _, entry := range base {
		baseCounts[codexCapturePolicyEntryKey(entry)] += entry.Count
	}
	out := make([]control.ProxyLayer0PolicyEntry, 0, len(current))
	for _, entry := range current {
		count := nonNegativeDelta(entry.Count, baseCounts[codexCapturePolicyEntryKey(entry)])
		if count == 0 {
			continue
		}
		entry.Count = count
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return codexCapturePolicyEntryKey(out[i]) < codexCapturePolicyEntryKey(out[j])
	})
	return out
}

func codexCapturePolicyEntryKey(entry control.ProxyLayer0PolicyEntry) string {
	return entry.Route + "\x00" + entry.Mechanism + "\x00" + entry.Action + "\x00" + entry.Reason + "\x00" + entry.BlockReason
}

func deltaCodexCaptureCacheEntries(base, current []control.ProxyLayer0CacheEntry) []control.ProxyLayer0CacheEntry {
	baseCounts := make(map[string]int64, len(base))
	for _, entry := range base {
		baseCounts[codexCaptureCacheEntryKey(entry)] += entry.Count
	}
	out := make([]control.ProxyLayer0CacheEntry, 0, len(current))
	for _, entry := range current {
		count := nonNegativeDelta(entry.Count, baseCounts[codexCaptureCacheEntryKey(entry)])
		if count == 0 {
			continue
		}
		entry.Count = count
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return codexCaptureCacheEntryKey(out[i]) < codexCaptureCacheEntryKey(out[j])
	})
	return out
}

func codexCaptureCacheEntryKey(entry control.ProxyLayer0CacheEntry) string {
	return entry.Route + "\x00" + entry.Mechanism + "\x00" + entry.Action + "\x00" + entry.Reason
}

func runCodexCaptureCLI(ctx context.Context, flags codexCaptureRunFlags, stdout, stderr io.Writer) error {
	args := []string{"codex", "run", "--transport=" + flags.transport, "--"}
	args = append(args, flags.codexArgs...)
	cmd := exec.CommandContext(ctx, flags.binary, args...)
	cmd.Stdin = os.Stdin
	if flags.exitMarker != "" {
		return runCodexCaptureCLIUntilMarker(ctx, cmd, flags.capturePath, flags.exitMarker, flags.exitMarkerCount, stdout, stderr)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run scoped Codex capture: %w", err)
	}
	return nil
}

func runCodexCaptureCLIUntilMarker(ctx context.Context, cmd *exec.Cmd, capturePath, marker string, markerCount int, stdout, stderr io.Writer) error {
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
	scriptCmd := exec.CommandContext(ctx, "script", args...)
	scriptCmd.Stdin = cmd.Stdin
	scriptCmd.Stdout = stdout
	scriptCmd.Stderr = stderr
	if err := scriptCmd.Start(); err != nil {
		return fmt.Errorf("start Codex PTY capture: %w", err)
	}
	markerHit := make(chan struct{})
	var markerOnce sync.Once
	signalMarker := func() {
		markerOnce.Do(func() {
			close(markerHit)
		})
	}
	stopWatch := make(chan struct{})
	go watchCodexCaptureMarkerFunc(logPath, marker, markerCount, signalMarker, stopWatch)
	go watchCodexCaptureFunctionOutputMarker(capturePath, marker, markerCount, signalMarker, stopWatch)
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
	case <-ctx.Done():
		errWait = stopCodexCapturePTY(scriptCmd, waitErr, 2*time.Second)
		if errWait == nil {
			errWait = ctx.Err()
		}
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
	return stopCodexCapturePTY(scriptCmd, waitErr, 10*time.Second)
}

func stopCodexCapturePTY(scriptCmd *exec.Cmd, waitErr <-chan error, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
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
	watchCodexCaptureMarkerFunc(path, marker, markerCount, func() { close(hit) }, stop)
}

func watchCodexCaptureMarkerFunc(path, marker string, markerCount int, signal func(), stop <-chan struct{}) {
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
			if !signaled && strings.Count(normalizeCodexCaptureMarkerText(string(data)), normalizeCodexCaptureMarkerText(marker)) >= markerCount {
				signaled = true
				signal()
			}
		}
	}
}

func watchCodexCaptureFunctionOutputMarker(path, marker string, markerCount int, signal func(), stop <-chan struct{}) {
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
			if !signaled && countCodexCaptureFunctionOutputMarker(data, marker) >= markerCount {
				signaled = true
				signal()
			}
		}
	}
}

func countCodexCaptureFunctionOutputMarker(data []byte, marker string) int {
	needle := normalizeCodexCaptureMarkerText(marker)
	if needle == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var frame struct {
			Payload struct {
				Input []struct {
					Type   string `json:"type"`
					Output string `json:"output"`
				} `json:"input"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		for _, item := range frame.Payload.Input {
			if item.Type != "function_call_output" {
				continue
			}
			count += strings.Count(normalizeCodexCaptureMarkerText(item.Output), needle)
		}
	}
	return count
}

func normalizeCodexCaptureMarkerText(s string) string {
	s = compression.StripANSICodes(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
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
		LiveDelta:           result.LiveDelta,
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
	if result.LiveDelta != nil {
		fmt.Fprintf(w, "  billable_input_tokens_saved: %d\n", result.LiveDelta.BillableInputTokensSaved)
		fmt.Fprintf(w, "  input_tokens_saved:          %d\n", result.LiveDelta.InputTokensSaved)
		fmt.Fprintf(w, "  output_wire_bytes_saved:     %d\n", result.LiveDelta.OutputWireBytesSaved)
		fmt.Fprintf(w, "  layer0_live read/repeated/chunk/refs: %d / %d / %d / %d\n",
			result.LiveDelta.ProxyLayer0ReadDelta, result.LiveDelta.ProxyLayer0Repeated,
			result.LiveDelta.ProxyLayer0ChunkDedup, result.LiveDelta.ProxyLayer0ChunkRefs)
		fmt.Fprintf(w, "  safety_parse/degraded/compression: %d / %d / %d\n",
			result.LiveDelta.ParseFailures, result.LiveDelta.DegradedSessions, result.LiveDelta.CompressionErrors)
		writeCodexCaptureHostBudgetSummary(w, result.LiveDelta)
		writeCodexCapturePolicySummary(w, result.LiveDelta.ProxyLayer0Policy)
		writeCodexCaptureCacheSummary(w, result.LiveDelta.ProxyLayer0Cache)
	}
	fmt.Fprintf(w, "  replay_bytes_saved: %d\n", result.Replay.BytesSaved)
	fmt.Fprintf(w, "  lost:          %d\n", result.Replay.Lost)
	fmt.Fprintf(w, "  gate:          %s\n", passFail(result.Replay.GatePassed))
}

func writeCodexCaptureHostBudgetSummary(w io.Writer, delta *codexCaptureLiveDelta) {
	if delta == nil || delta.HostBudgetStatus == "" {
		return
	}
	reasons := "-"
	if len(delta.HostBudgetReasons) > 0 {
		reasons = strings.Join(delta.HostBudgetReasons, ",")
	}
	fmt.Fprintf(w, "  host_budget: %s exceeded=%v reasons=%s cpu_window=%.2f%%/%.2fs rss=%d state=%d\n",
		delta.HostBudgetStatus, delta.HostBudgetExceeded, reasons, delta.HostBudgetCPUWindowPct,
		delta.HostBudgetCPUWindowSec, delta.HostBudgetRSSBytes, delta.HostBudgetStateBytes)
}

func writeCodexCapturePolicySummary(w io.Writer, entries []control.ProxyLayer0PolicyEntry) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintln(w, "  policy_delta:")
	for _, entry := range entries {
		reason := entry.Reason
		if entry.BlockReason != "" {
			reason += " block=" + entry.BlockReason
		}
		fmt.Fprintf(w, "    %s/%s/%s %s: %d\n", valueOrDash(entry.Route), entry.Mechanism, entry.Action, reason, entry.Count)
	}
}

func writeCodexCaptureCacheSummary(w io.Writer, entries []control.ProxyLayer0CacheEntry) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintln(w, "  cache_delta:")
	for _, entry := range entries {
		fmt.Fprintf(w, "    %s/%s/%s %s: %d\n", valueOrDash(entry.Route), entry.Mechanism, entry.Action, entry.Reason, entry.Count)
	}
}

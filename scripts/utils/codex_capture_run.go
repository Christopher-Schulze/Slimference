package main

import (
	"bytes"
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/compression"
	"github.com/Christopher-Schulze/Slimference/internal/control"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
)

type codexCaptureRunFlags struct {
	binary                        string
	capturePath                   string
	host                          string
	port                          string
	transport                     string
	healthTimeout                 time.Duration
	codexTimeout                  time.Duration
	matrixPath                    string
	resourceProfileProof          string
	id                            string
	client                        string
	workloadClass                 string
	codexVersion                  string
	slimferenceCommit             string
	repo                          string
	model                         string
	abPairID                      string
	abVariant                     string
	exitMarker                    string
	exitMarkerCount               int
	quietCodexOutput              bool
	restartAfterCompletion        int
	restartAfterMutatedCompletion int
	expectedReducers              []string
	expectedZeroSavings           bool
	captureExplicit               bool
	help                          bool
	codexArgs                     []string
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
	resourceBefore func(context.Context, codexCaptureRunFlags, *codexCaptureDaemon) (*codexCaptureResourceProof, error)
	resourceAfter  func(context.Context, codexCaptureRunFlags, *codexCaptureDaemon, *codexCaptureResourceProof) error
}

type codexCaptureDaemon struct {
	cmd      *exec.Cmd
	done     <-chan error
	stateDir string
}

type codexCaptureResourceProof struct {
	dir          string
	baselineFile string
	baseline     workdaySavingsBaseline
	sampleDone   <-chan error
}

const (
	codexCaptureResourceHostWindowWait     = 5 * time.Second
	codexCaptureResourceHostWindowInterval = 250 * time.Millisecond
)

type codexCaptureRunResult struct {
	CapturePath string                 `json:"capture_path"`
	MatrixPath  string                 `json:"matrix_path,omitempty"`
	Replay      wssABReplayReport      `json:"replay"`
	LiveDelta   *codexCaptureLiveDelta `json:"live_delta,omitempty"`
	StartedAt   string                 `json:"started_at"`
	EndedAt     string                 `json:"ended_at"`
}

type codexCaptureRunExecution struct {
	daemon             *codexCaptureDaemon
	preRestartSnapshot *codexCaptureAdminSnapshot
}

type codexCaptureAdminSnapshot struct {
	BillableInputTokensSaved  int64 `json:"billable_input_tokens_saved"`
	InputTokensSaved          int64 `json:"input_tokens_saved"`
	OutputWireBytesSaved      int64 `json:"output_wire_bytes_saved"`
	RequestSideBytesReduced   int64 `json:"request_side_bytes_reduced"`
	ProviderCacheReadTokens   int64 `json:"provider_cache_read_tokens"`
	ProviderCacheCreateTokens int64 `json:"provider_cache_create_tokens"`

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

	OutputReduceInjected              int64 `json:"output_reduce_injected_turns"`
	OutputReduceSkipped               int64 `json:"output_reduce_skipped_turns"`
	OutputReduceInputOverheadTokens   int64 `json:"output_reduce_input_overhead_tokens"`
	OutputReduceOutputTokensObserved  int64 `json:"output_reduce_output_tokens_observed"`
	OutputReduceDowngrades            int64 `json:"output_reduce_downgrades"`
	StopSeqRequestsModified           int64 `json:"stop_seq_requests_modified"`
	StreamcutFired                    int64 `json:"streamcut_fired"`
	RepdetResponsesRewritten          int64 `json:"repdet_responses_rewritten"`
	StaleReadBlocksReplaced           int64 `json:"stale_read_blocks_replaced"`
	ObsoleteReadBlocksPruned          int64 `json:"obsolete_read_blocks_pruned"`
	BeterseInjections                 int64 `json:"beterse_injections"`
	WSSStatefulPrefixElisionRequests  int64 `json:"wss_stateful_prefix_elision_requests"`
	WSSStatefulPrefixElisionTools     int64 `json:"wss_stateful_prefix_elision_tool_requests"`
	WSSStatefulPrefixElisionBytes     int64 `json:"wss_stateful_prefix_elision_bytes_saved"`
	WSSStatefulPrefixElisionTokens    int64 `json:"wss_stateful_prefix_elision_tokens_saved"`
	WSSStatefulPrefixInstructionsKept int64 `json:"wss_stateful_prefix_elision_instructions_kept"`

	ParseFailures                     int64 `json:"parse_failures"`
	DegradedSessions                  int64 `json:"degraded_sessions"`
	CompressionErrors                 int64 `json:"compression_errors"`
	AnalyticsProofEventsDropped       int64 `json:"analytics_proof_events_dropped"`
	AnalyticsLowPriorityEventsDropped int64 `json:"analytics_low_priority_events_dropped"`

	HostBudgetStatus        string   `json:"host_budget_status,omitempty"`
	HostBudgetExceeded      bool     `json:"host_budget_exceeded,omitempty"`
	HostBudgetReasons       []string `json:"host_budget_reasons,omitempty"`
	HostBudgetRSSBytes      int64    `json:"host_budget_rss_bytes,omitempty"`
	HostBudgetGoRetainedB   int64    `json:"host_budget_go_retained_bytes,omitempty"`
	HostBudgetEffectiveRSSB int64    `json:"host_budget_effective_rss_bytes,omitempty"`
	HostBudgetCPUWindowPct  float64  `json:"host_budget_cpu_window_percent,omitempty"`
	HostBudgetCPUWindowSec  float64  `json:"host_budget_cpu_window_seconds,omitempty"`
	HostBudgetDiskWriteOps  int64    `json:"host_budget_disk_write_ops_delta,omitempty"`
	HostBudgetStateBytes    int64    `json:"host_budget_state_bytes,omitempty"`
	HostBudgetCompressionOK bool     `json:"host_budget_compression_ok,omitempty"`
	HostBudgetDegradationOK bool     `json:"host_budget_degradation_ok,omitempty"`
}

type codexCaptureLiveDelta struct {
	BillableInputTokensSaved   int64            `json:"billable_input_tokens_saved"`
	InputTokensSaved           int64            `json:"input_tokens_saved"`
	OutputWireBytesSaved       int64            `json:"output_wire_bytes_saved"`
	RequestSideBytesReduced    int64            `json:"request_side_bytes_reduced"`
	ProviderCacheReadTokens    int64            `json:"provider_cache_read_tokens"`
	ProviderCacheCreateTokens  int64            `json:"provider_cache_create_tokens"`
	ProviderInputTokens        int64            `json:"provider_input_tokens_observed,omitempty"`
	ProviderOutputTokens       int64            `json:"provider_output_tokens_observed,omitempty"`
	WireSurfaceFrames          int64            `json:"wire_surface_frames_observed"`
	WireClientResponseCreates  int64            `json:"wire_client_response_create_requests"`
	WireClientDeclaredTools    int64            `json:"wire_client_declared_tools_total"`
	WireClientDeclaredToolsMax int64            `json:"wire_client_declared_tools_max"`
	WireClientInputItems       int64            `json:"wire_client_input_items"`
	WireFunctionCallOutputs    int64            `json:"wire_function_call_output_items"`
	WireServerOutputItems      int64            `json:"wire_server_output_items"`
	WireServerFunctionCalls    int64            `json:"wire_server_function_call_items"`
	WireClientInputItemTypes   map[string]int64 `json:"wire_client_input_item_types,omitempty"`
	WireServerOutputItemTypes  map[string]int64 `json:"wire_server_output_item_types,omitempty"`

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

	OutputReduceInjected              int64 `json:"output_reduce_injected_turns"`
	OutputReduceSkipped               int64 `json:"output_reduce_skipped_turns"`
	OutputReduceInputOverheadTokens   int64 `json:"output_reduce_input_overhead_tokens"`
	OutputReduceOutputTokensObserved  int64 `json:"output_reduce_output_tokens_observed"`
	OutputReduceDowngrades            int64 `json:"output_reduce_downgrades"`
	StopSeqRequestsModified           int64 `json:"stop_seq_requests_modified"`
	StreamcutFired                    int64 `json:"streamcut_fired"`
	RepdetResponsesRewritten          int64 `json:"repdet_responses_rewritten"`
	StaleReadBlocksReplaced           int64 `json:"stale_read_blocks_replaced"`
	ObsoleteReadBlocksPruned          int64 `json:"obsolete_read_blocks_pruned"`
	BeterseInjections                 int64 `json:"beterse_injections"`
	WSSStatefulPrefixElisionRequests  int64 `json:"wss_stateful_prefix_elision_requests"`
	WSSStatefulPrefixElisionTools     int64 `json:"wss_stateful_prefix_elision_tool_requests"`
	WSSStatefulPrefixElisionBytes     int64 `json:"wss_stateful_prefix_elision_bytes_saved"`
	WSSStatefulPrefixElisionTokens    int64 `json:"wss_stateful_prefix_elision_tokens_saved"`
	WSSStatefulPrefixInstructionsKept int64 `json:"wss_stateful_prefix_elision_instructions_kept"`

	ParseFailures                     int64 `json:"parse_failures"`
	DegradedSessions                  int64 `json:"degraded_sessions"`
	CompressionErrors                 int64 `json:"compression_errors"`
	AnalyticsProofEventsDropped       int64 `json:"analytics_proof_events_dropped"`
	AnalyticsLowPriorityEventsDropped int64 `json:"analytics_low_priority_events_dropped"`

	HostBudgetStatus        string   `json:"host_budget_status,omitempty"`
	HostBudgetExceeded      bool     `json:"host_budget_exceeded,omitempty"`
	HostBudgetReasons       []string `json:"host_budget_reasons,omitempty"`
	HostBudgetRSSBytes      int64    `json:"host_budget_rss_bytes,omitempty"`
	HostBudgetGoRetainedB   int64    `json:"host_budget_go_retained_bytes,omitempty"`
	HostBudgetEffectiveRSSB int64    `json:"host_budget_effective_rss_bytes,omitempty"`
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
	StopSeqRequestsModified           uint64 `json:"stop_seq_requests_modified"`
	StreamcutFired                    uint64 `json:"streamcut_fired"`
	RepdetResponsesRewritten          uint64 `json:"repdet_responses_rewritten"`
	StaleReadBlocksReplaced           uint64 `json:"stale_read_blocks_replaced"`
	ObsoleteReadBlocksPruned          uint64 `json:"obsolete_read_blocks_pruned"`
	BeterseInjections                 uint64 `json:"beterse_injections"`
	WSSStatefulPrefixElisionRequests  uint64 `json:"wss_stateful_prefix_elision_requests"`
	WSSStatefulPrefixElisionTools     uint64 `json:"wss_stateful_prefix_elision_tool_requests"`
	WSSStatefulPrefixElisionBytes     uint64 `json:"wss_stateful_prefix_elision_bytes_saved"`
	WSSStatefulPrefixElisionTokens    uint64 `json:"wss_stateful_prefix_elision_tokens_saved"`
	WSSStatefulPrefixInstructionsKept uint64 `json:"wss_stateful_prefix_elision_instructions_kept"`
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
  --resource-profile-proof DIR
                             Write a release resource bundle for this managed
                             daemon run. Defaults --capture to DIR/frames.jsonl
                             and --matrix-row to DIR/matrix.jsonl when omitted.
  --id ID                    Matrix row id
  --client cli|desktop       Matrix row client (default: cli)
  --workload-class CLASS     Matrix row workload class, required with --matrix-row
  --expected-reducer NAME    Matrix row expected reducer, repeatable
  --expected-zero            Matrix row expected_zero_savings=true
  --codex-version VALUE      Matrix row Codex version
  --slimference-commit VALUE Matrix row Slimference commit
  --repo VALUE               Matrix row repository label
  --model VALUE              Matrix row model label
  --ab-pair-id VALUE         Optional A/B pair id for output-reduce proofs
  --ab-variant VALUE         Optional A/B variant: baseline or directive
  --exit-marker TEXT         Interrupt Codex automatically once TEXT appears in output.
                             On macOS this uses script(1) so Codex still sees a TTY.
                             The marker is also watched in captured function_call_output
                             frames, so quiet TUI output cannot hide it.
  --exit-marker-count N      Required marker occurrences before interrupt (default: 1)
  --quiet-codex-output       Hide Codex TUI output and print only the final summary
  --restart-after-completion N
                             Lab/proof only: restart the managed daemon after
                             the Nth server response.completed frame, even if
                             no request mutated. This forces a Codex reconnect
                             under product-default delta guards so WSS
                             full-history resend tolerance can be proven.
  --restart-after-mutated-completion N
                             Lab/proof only: restart the managed daemon after the
                             Nth mutated client request is followed by a server
                             response.completed frame. This forces Codex to
                             reconnect after an accepted mutation so WSS
                             full-history resend tolerance can be proven.

The tool starts the daemon as its own child process with SLIMFERENCE_WSS_AB_CAPTURE
set, waits for /health, runs "slimference codex run --transport=<value> -- ...",
records live admin-state token deltas, stops the daemon, then replays the
capture with --fail-on-lost and --fail-on-upstream-error semantics. Live billable
input-token savings are the product savings signal; replay bytes are only the
model-facing regression/safety proxy. It does not use a detached background
daemon, because detached shell starts are too fragile for unattended release
captures.`

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
		resourceBefore: startCodexCaptureResourceProof,
		resourceAfter:  finishCodexCaptureResourceProof,
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
	if flags.restartAfterCompletion > 0 && flags.restartAfterMutatedCompletion > 0 {
		fmt.Fprintln(stderr, "--restart-after-completion cannot be combined with --restart-after-mutated-completion")
		return 2
	}
	if (flags.restartAfterCompletion > 0 || flags.restartAfterMutatedCompletion > 0) && flags.resourceProfileProof != "" {
		fmt.Fprintln(stderr, "restart proof flags cannot be combined with --resource-profile-proof because the daemon PID intentionally changes mid-run")
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
	var resourceProof *codexCaptureResourceProof
	if flags.resourceProfileProof != "" {
		if deps.resourceBefore == nil || deps.resourceAfter == nil {
			fmt.Fprintln(stderr, "resource proof dependencies are not configured")
			return 1
		}
		resourceProof, err = deps.resourceBefore(ctx, flags, daemon)
		if err != nil {
			fmt.Fprintf(stderr, "start resource proof: %v\n", err)
			return 1
		}
	}
	runStdout := stdout
	runStderr := stderr
	if flags.quietCodexOutput {
		runStdout = io.Discard
		runStderr = io.Discard
	}
	runCtx, cancelRun := context.WithTimeout(ctx, flags.codexTimeout)
	execution, err := runCodexCaptureWithOptionalRestart(runCtx, flags, daemon, deps, runStdout, runStderr, stderr)
	cancelRun()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	daemon = execution.daemon
	after, err := deps.adminSnapshot(ctx, flags)
	if err != nil {
		if flags.restartAfterCompletion <= 0 && flags.restartAfterMutatedCompletion <= 0 {
			fmt.Fprintf(stderr, "read final admin state: %v\n", err)
			return 1
		}
		if execution.preRestartSnapshot != nil {
			fmt.Fprintf(stderr, "read final admin state after restart proof failed; continuing with pre-restart live delta: %v\n", err)
		} else {
			fmt.Fprintf(stderr, "read final admin state after restart proof failed; continuing with replay-only live delta: %v\n", err)
		}
		after = before
	}
	if resourceProof != nil {
		if err := deps.resourceAfter(ctx, flags, daemon, resourceProof); err != nil {
			fmt.Fprintf(stderr, "finish resource proof: %v\n", err)
			return 1
		}
	}
	if err := deps.stopDaemon(ctx, daemon); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	stopDaemon = false
	endedAt := deps.now().UTC()

	replay, err := deps.replay(wssABReplayFlags{path: flags.capturePath, failOnLost: true, failOnUpstreamError: true})
	if err != nil {
		fmt.Fprintf(stderr, "replay capture: %v\n", err)
		return 1
	}
	liveDelta := deltaCodexCaptureAdminSnapshot(before, after)
	if execution.preRestartSnapshot != nil {
		liveDelta = deltaCodexCaptureAdminSnapshot(before, *execution.preRestartSnapshot)
	}
	result := codexCaptureRunResult{
		CapturePath: flags.capturePath,
		MatrixPath:  flags.matrixPath,
		Replay:      replay,
		LiveDelta:   liveDelta,
		StartedAt:   startedAt.Format(time.RFC3339),
		EndedAt:     endedAt.Format(time.RFC3339),
	}
	result.LiveDelta = augmentCodexCaptureLiveDeltaFromWire(flags.capturePath, result.LiveDelta)
	if flags.matrixPath != "" {
		if err := appendCodexCaptureMatrixRow(flags, result); err != nil {
			fmt.Fprintf(stderr, "append matrix row: %v\n", err)
			return 1
		}
	}
	if failures := validateCodexCaptureExpectedReducers(flags.expectedReducers, result.LiveDelta); len(failures) > 0 {
		fmt.Fprintf(stderr, "validate expected reducers: %s\n", strings.Join(failures, "; "))
		writeCodexCaptureRunSummary(stdout, result)
		return 3
	}
	writeCodexCaptureRunSummary(stdout, result)
	if !replay.GatePassed {
		return 3
	}
	return 0
}

func validateCodexCaptureExpectedReducers(expected []string, live *codexCaptureLiveDelta) []string {
	expected = normalizeExpectedReducers(expected)
	if len(expected) == 0 {
		return nil
	}
	_, failures := validateExpectedReducers(expected, live)
	return failures
}

func augmentCodexCaptureLiveDeltaFromWire(path string, live *codexCaptureLiveDelta) *codexCaptureLiveDelta {
	if live == nil {
		return live
	}
	usage := codexCaptureWireTokenUsageObserved(path)
	if live.ProviderInputTokens == 0 {
		live.ProviderInputTokens = usage.InputTokens
	}
	if live.ProviderOutputTokens == 0 {
		live.ProviderOutputTokens = usage.OutputTokens
	}
	if live.OutputReduceInjected == 0 && codexCaptureWireHasOutputReduceMarker(path, outputreduce.DefaultMarker) {
		live.OutputReduceInjected = 1
	}
	surface := codexCaptureWireSurfaceObserved(path)
	live.WireSurfaceFrames = surface.Frames
	live.WireClientResponseCreates = surface.ClientResponseCreates
	live.WireClientDeclaredTools = surface.ClientDeclaredTools
	live.WireClientDeclaredToolsMax = surface.ClientDeclaredToolsMax
	live.WireClientInputItems = surface.ClientInputItems
	live.WireFunctionCallOutputs = surface.FunctionCallOutputs
	live.WireServerOutputItems = surface.ServerOutputItems
	live.WireServerFunctionCalls = surface.ServerFunctionCalls
	live.WireClientInputItemTypes = surface.ClientInputItemTypes
	live.WireServerOutputItemTypes = surface.ServerOutputItemTypes
	return live
}

func codexCaptureWireOutputTokensObserved(path string) int64 {
	return codexCaptureWireTokenUsageObserved(path).OutputTokens
}

type codexCaptureWireUsage struct {
	InputTokens  int64
	OutputTokens int64
}

type codexCaptureWireSurface struct {
	Frames                 int64
	ClientResponseCreates  int64
	ClientDeclaredTools    int64
	ClientDeclaredToolsMax int64
	ClientInputItems       int64
	FunctionCallOutputs    int64
	ServerOutputItems      int64
	ServerFunctionCalls    int64
	ClientInputItemTypes   map[string]int64
	ServerOutputItemTypes  map[string]int64
}

func codexCaptureWireSurfaceObserved(path string) codexCaptureWireSurface {
	surface := codexCaptureWireSurface{
		ClientInputItemTypes:  make(map[string]int64),
		ServerOutputItemTypes: make(map[string]int64),
	}
	if strings.TrimSpace(path) == "" {
		return surface
	}
	f, err := os.Open(path)
	if err != nil {
		return surface
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var frame struct {
			Direction string          `json:"direction"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := dec.Decode(&frame); err != nil {
			if errors.Is(err, io.EOF) {
				return surface
			}
			return surface
		}
		payload := codexCaptureDecodedPayload(frame.Payload)
		if len(payload) == 0 {
			continue
		}
		surface.Frames++
		switch {
		case codexCaptureFrameFromClient(frame.Direction):
			codexCaptureWireSurfaceObserveClient(payload, &surface)
		case codexCaptureFrameFromServer(frame.Direction):
			codexCaptureWireSurfaceObserveServer(payload, &surface)
		}
	}
}

func codexCaptureWireSurfaceObserveClient(payload json.RawMessage, surface *codexCaptureWireSurface) {
	if surface == nil {
		return
	}
	var env struct {
		Type  string `json:"type"`
		Tools []struct {
			Type string `json:"type"`
		} `json:"tools"`
		Input []struct {
			Type string `json:"type"`
		} `json:"input"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}
	if strings.TrimSpace(env.Type) == "response.create" {
		surface.ClientResponseCreates++
	}
	toolCount := int64(len(env.Tools))
	surface.ClientDeclaredTools += toolCount
	if toolCount > surface.ClientDeclaredToolsMax {
		surface.ClientDeclaredToolsMax = toolCount
	}
	for _, item := range env.Input {
		itemType := strings.TrimSpace(item.Type)
		if itemType == "" {
			itemType = "unknown"
		}
		surface.ClientInputItems++
		surface.ClientInputItemTypes[itemType]++
		if itemType == "function_call_output" {
			surface.FunctionCallOutputs++
		}
	}
}

func codexCaptureWireSurfaceObserveServer(payload json.RawMessage, surface *codexCaptureWireSurface) {
	if surface == nil {
		return
	}
	var env struct {
		Item *struct {
			Type string `json:"type"`
		} `json:"item"`
		Output []struct {
			Type string `json:"type"`
		} `json:"output"`
		Response *struct {
			Output []struct {
				Type string `json:"type"`
			} `json:"output"`
		} `json:"response"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}
	if env.Item != nil {
		codexCaptureWireSurfaceCountServerItem(env.Item.Type, surface)
	}
	for _, item := range env.Output {
		codexCaptureWireSurfaceCountServerItem(item.Type, surface)
	}
	if env.Response != nil {
		for _, item := range env.Response.Output {
			codexCaptureWireSurfaceCountServerItem(item.Type, surface)
		}
	}
}

func codexCaptureWireSurfaceCountServerItem(itemType string, surface *codexCaptureWireSurface) {
	itemType = strings.TrimSpace(itemType)
	if itemType == "" {
		itemType = "unknown"
	}
	surface.ServerOutputItems++
	surface.ServerOutputItemTypes[itemType]++
	if itemType == "function_call" {
		surface.ServerFunctionCalls++
	}
}

func codexCaptureDecodedPayload(payload json.RawMessage) json.RawMessage {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return nil
	}
	if payload[0] != '"' {
		return payload
	}
	var encoded string
	if err := json.Unmarshal(payload, &encoded); err != nil {
		return nil
	}
	return bytes.TrimSpace([]byte(encoded))
}

func codexCaptureWireTokenUsageObserved(path string) codexCaptureWireUsage {
	if strings.TrimSpace(path) == "" {
		return codexCaptureWireUsage{}
	}
	f, err := os.Open(path)
	if err != nil {
		return codexCaptureWireUsage{}
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var total codexCaptureWireUsage
	for {
		var frame struct {
			Direction string          `json:"direction"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := dec.Decode(&frame); err != nil {
			if errors.Is(err, io.EOF) {
				return total
			}
			return total
		}
		if !codexCaptureFrameFromServer(frame.Direction) {
			continue
		}
		usage := codexCapturePayloadTokenUsage(frame.Payload)
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
	}
}

func codexCaptureFrameFromServer(direction string) bool {
	direction = strings.ToLower(strings.TrimSpace(direction))
	return direction == "s2c" || direction == "server_to_client"
}

func codexCapturePayloadOutputTokens(payload json.RawMessage) int64 {
	return codexCapturePayloadTokenUsage(payload).OutputTokens
}

func codexCapturePayloadTokenUsage(payload json.RawMessage) codexCaptureWireUsage {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return codexCaptureWireUsage{}
	}
	if payload[0] == '"' {
		var encoded string
		if err := json.Unmarshal(payload, &encoded); err != nil {
			return codexCaptureWireUsage{}
		}
		payload = []byte(encoded)
	}
	var env struct {
		Usage    *codexCaptureUsageFields `json:"usage"`
		Response *struct {
			Usage *codexCaptureUsageFields `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return codexCaptureWireUsage{}
	}
	var usage codexCaptureWireUsage
	if env.Usage != nil {
		usage.InputTokens = maxInt64(usage.InputTokens, env.Usage.inputTokens())
		usage.OutputTokens = maxInt64(usage.OutputTokens, env.Usage.outputTokens())
	}
	if env.Response != nil && env.Response.Usage != nil {
		usage.InputTokens = maxInt64(usage.InputTokens, env.Response.Usage.inputTokens())
		usage.OutputTokens = maxInt64(usage.OutputTokens, env.Response.Usage.outputTokens())
	}
	return usage
}

type codexCaptureUsageFields struct {
	InputTokens      int64 `json:"input_tokens"`
	PromptTokens     int64 `json:"prompt_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

func (u codexCaptureUsageFields) inputTokens() int64 {
	return maxInt64(u.InputTokens, u.PromptTokens)
}

func (u codexCaptureUsageFields) outputTokens() int64 {
	return maxInt64(u.OutputTokens, u.CompletionTokens)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func codexCaptureWireHasOutputReduceMarker(path, marker string) bool {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(marker) == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var frame struct {
			Direction string          `json:"direction"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := dec.Decode(&frame); err != nil {
			if errors.Is(err, io.EOF) {
				return false
			}
			return false
		}
		if codexCaptureFrameFromServer(frame.Direction) && strings.Contains(codexCapturePayloadInstructions(frame.Payload), marker) {
			return true
		}
	}
}

func codexCapturePayloadInstructions(payload json.RawMessage) string {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return ""
	}
	if payload[0] == '"' {
		var encoded string
		if err := json.Unmarshal(payload, &encoded); err != nil {
			return ""
		}
		payload = []byte(encoded)
	}
	var env struct {
		Instructions string `json:"instructions"`
		Response     struct {
			Instructions string `json:"instructions"`
		} `json:"response"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return ""
	}
	if env.Response.Instructions != "" {
		return env.Response.Instructions
	}
	return env.Instructions
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
			arg == "--model", arg == "--ab-pair-id", arg == "--ab-variant",
			arg == "--exit-marker", arg == "--resource-profile-proof":
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
		case arg == "--restart-after-mutated-completion":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("%s requires a value", arg)
			}
			i++
			n, err := parseNonNegativeIntFlag("--restart-after-mutated-completion", args[i])
			if err != nil {
				return flags, err
			}
			if n == 0 {
				return flags, fmt.Errorf("--restart-after-mutated-completion must be > 0")
			}
			flags.restartAfterMutatedCompletion = n
		case arg == "--restart-after-completion":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("%s requires a value", arg)
			}
			i++
			n, err := parseNonNegativeIntFlag("--restart-after-completion", args[i])
			if err != nil {
				return flags, err
			}
			if n == 0 {
				return flags, fmt.Errorf("--restart-after-completion must be > 0")
			}
			flags.restartAfterCompletion = n
		case strings.HasPrefix(arg, "--exit-marker-count="):
			n, err := parseNonNegativeIntFlag("--exit-marker-count", strings.TrimPrefix(arg, "--exit-marker-count="))
			if err != nil {
				return flags, err
			}
			if n == 0 {
				return flags, fmt.Errorf("--exit-marker-count must be > 0")
			}
			flags.exitMarkerCount = n
		case strings.HasPrefix(arg, "--restart-after-mutated-completion="):
			n, err := parseNonNegativeIntFlag("--restart-after-mutated-completion", strings.TrimPrefix(arg, "--restart-after-mutated-completion="))
			if err != nil {
				return flags, err
			}
			if n == 0 {
				return flags, fmt.Errorf("--restart-after-mutated-completion must be > 0")
			}
			flags.restartAfterMutatedCompletion = n
		case strings.HasPrefix(arg, "--restart-after-completion="):
			n, err := parseNonNegativeIntFlag("--restart-after-completion", strings.TrimPrefix(arg, "--restart-after-completion="))
			if err != nil {
				return flags, err
			}
			if n == 0 {
				return flags, fmt.Errorf("--restart-after-completion must be > 0")
			}
			flags.restartAfterCompletion = n
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
	var err error
	flags.resourceProfileProof, err = expandCodexCapturePath(flags.resourceProfileProof)
	if err != nil {
		return flags, err
	}
	if flags.capturePath == "" {
		if flags.resourceProfileProof != "" {
			flags.capturePath = filepath.Join(flags.resourceProfileProof, "frames.jsonl")
		} else {
			flags.capturePath = filepath.Join("~", ".slimference", "captures", "codex-capture-"+now.UTC().Format("20060102T150405Z")+".jsonl")
		}
	}
	if flags.matrixPath == "" && flags.resourceProfileProof != "" {
		flags.matrixPath = filepath.Join(flags.resourceProfileProof, "matrix.jsonl")
	}
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
	if err := validateABProofFlags(flags.abPairID, flags.abVariant); err != nil {
		return flags, err
	}
	flags.transport = strings.ToLower(strings.TrimSpace(flags.transport))
	if !validCodexCaptureTransport(flags.transport) {
		return flags, fmt.Errorf("--transport must be auto, http, wss, wss-bridge, or direct")
	}
	flags.host = strings.TrimSpace(flags.host)
	if flags.host == "" {
		return flags, fmt.Errorf("--host is required")
	}
	port, err := strconv.Atoi(strings.TrimSpace(flags.port))
	if err != nil || port < 1 || port > 65535 {
		return flags, fmt.Errorf("--port must be 1-65535")
	}
	flags.port = strconv.Itoa(port)
	return flags, nil
}

func setCodexCaptureRunFlag(flags *codexCaptureRunFlags, name, value string) error {
	value = strings.TrimSpace(value)
	switch name {
	case "--binary":
		flags.binary = value
	case "--capture":
		flags.captureExplicit = true
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
	case "--resource-profile-proof":
		flags.resourceProfileProof = value
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
	case "--ab-pair-id":
		flags.abPairID = value
	case "--ab-variant":
		flags.abVariant = strings.ToLower(value)
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

func startCodexCaptureResourceProof(ctx context.Context, flags codexCaptureRunFlags, daemon *codexCaptureDaemon) (*codexCaptureResourceProof, error) {
	if daemon == nil || daemon.cmd == nil || daemon.cmd.Process == nil {
		return nil, errors.New("managed daemon PID is unavailable")
	}
	dir := flags.resourceProfileProof
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("resource proof directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create resource proof dir %s: %w", dir, err)
	}
	report, err := waitCodexCaptureAggregateReportWithHostWindow(ctx, flags, codexCaptureResourceHostWindowWait, loadCodexCaptureAggregateReport)
	if err != nil {
		return nil, fmt.Errorf("load admin-before aggregate report: %w", err)
	}
	if err := writeCodexCaptureJSONFile(filepath.Join(dir, "admin-before.json"), report); err != nil {
		return nil, err
	}
	baselineFile := filepath.Join(dir, "workday-baseline.json")
	baseline := workdaySavingsBaseline{
		SchemaVersion: 1,
		StartedAt:     report.Generated,
		Source:        report.Source,
		Report:        report,
	}
	if err := writeWorkdayBaseline(baselineFile, baseline); err != nil {
		return nil, err
	}
	pid := daemon.cmd.Process.Pid
	if err := writeCodexCapturePSSnapshot(ctx, pid, filepath.Join(dir, "ps-before.txt")); err != nil {
		return nil, err
	}
	sampleDone, err := startCodexCaptureSample(ctx, pid, filepath.Join(dir, "slimference.sample.txt"))
	if err != nil {
		return nil, err
	}
	return &codexCaptureResourceProof{
		dir:          dir,
		baselineFile: baselineFile,
		baseline:     baseline,
		sampleDone:   sampleDone,
	}, nil
}

func finishCodexCaptureResourceProof(ctx context.Context, flags codexCaptureRunFlags, daemon *codexCaptureDaemon, proof *codexCaptureResourceProof) error {
	if proof == nil {
		return nil
	}
	if proof.sampleDone != nil {
		if err := <-proof.sampleDone; err != nil {
			return err
		}
	}
	if daemon == nil || daemon.cmd == nil || daemon.cmd.Process == nil {
		return errors.New("managed daemon PID is unavailable")
	}
	report, err := waitCodexCaptureAggregateReportWithHostWindow(ctx, flags, codexCaptureResourceHostWindowWait, loadCodexCaptureAggregateReport)
	if err != nil {
		return fmt.Errorf("load admin-after aggregate report: %w", err)
	}
	if err := writeCodexCaptureJSONFile(filepath.Join(proof.dir, "admin-after.json"), report); err != nil {
		return err
	}
	if err := writeCodexCapturePSSnapshot(ctx, daemon.cmd.Process.Pid, filepath.Join(proof.dir, "ps-after.txt")); err != nil {
		return err
	}
	result := workdaySavingsResult{
		SchemaVersion: 1,
		BaselineFile:  proof.baselineFile,
		StartedAt:     proof.baseline.StartedAt,
		FinishedAt:    report.Generated,
		Duration:      report.Generated.Sub(proof.baseline.StartedAt).Round(time.Second).String(),
		Baseline:      proof.baseline.Report,
		Current:       report,
		Delta:         diffAggregateSavingsReports(proof.baseline.Report, report),
	}
	return writeCodexCaptureJSONFile(filepath.Join(proof.dir, "workday-finish.json"), result)
}

func loadCodexCaptureAggregateReport(flags codexCaptureRunFlags) (aggregateSavingsReport, string, error) {
	return loadWorkdayAggregateReport(aggregateSavingsFlags{
		adminStateURL: "http://" + flags.host + ":" + flags.port + "/_slimference/admin/state",
		period:        "all",
		outputFormat:  outputJSON,
	})
}

func waitCodexCaptureAggregateReportWithHostWindow(ctx context.Context, flags codexCaptureRunFlags, timeout time.Duration, load func(codexCaptureRunFlags) (aggregateSavingsReport, string, error)) (aggregateSavingsReport, error) {
	if timeout <= 0 {
		timeout = codexCaptureResourceHostWindowWait
	}
	if load == nil {
		return aggregateSavingsReport{}, errors.New("aggregate report loader is nil")
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(codexCaptureResourceHostWindowInterval)
	defer ticker.Stop()

	var last aggregateSavingsReport
	var lastErr error
	for {
		report, _, err := load(flags)
		if err == nil {
			last = report
			if report.HostBudget.CPUWindowSeconds > 0 {
				return report, nil
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return aggregateSavingsReport{}, fmt.Errorf("%w after last report error: %v", ctx.Err(), lastErr)
			}
			return aggregateSavingsReport{}, ctx.Err()
		case <-deadline.C:
			if lastErr != nil {
				return aggregateSavingsReport{}, lastErr
			}
			return aggregateSavingsReport{}, fmt.Errorf("host_budget cpu_window_seconds stayed %.2f for %s", last.HostBudget.CPUWindowSeconds, timeout)
		case <-ticker.C:
		}
	}
}

func writeCodexCaptureJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeCodexCapturePSSnapshot(ctx context.Context, pid int, path string) error {
	cmd := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "pid,ppid,rss,vsz,pcpu,etime,command")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("capture ps snapshot for pid %d: %w", pid, err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return fmt.Errorf("capture ps snapshot for pid %d: empty output", pid)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func startCodexCaptureSample(ctx context.Context, pid int, path string) (<-chan error, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("resource proof sampling requires macOS sample(1)")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create dir for %s: %w", path, err)
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sample", strconv.Itoa(pid), "10", "1", "-file", path)
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start sample for pid %d: %w", pid, err)
	}
	go func() {
		err := cmd.Wait()
		if err == nil {
			if info, statErr := os.Stat(path); statErr != nil {
				err = fmt.Errorf("sample output %s: %w", path, statErr)
			} else if info.Size() == 0 {
				err = fmt.Errorf("sample output %s is empty", path)
			}
		}
		if err != nil {
			err = fmt.Errorf("capture sample for pid %d: %w", pid, err)
		}
		done <- err
		close(done)
	}()
	return done, nil
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
	stateDir, err := os.MkdirTemp(codexCaptureDaemonTempRoot(), "slmcd-*")
	if err != nil {
		return nil, fmt.Errorf("create capture daemon state dir: %w", err)
	}
	cmd := exec.CommandContext(ctx, flags.binary, "daemon")
	cmd.Env = codexCaptureDaemonEnv(os.Environ(), flags, stateDir)
	prepareCodexCaptureDaemonCommand(cmd)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(stateDir)
		return nil, fmt.Errorf("start capture daemon: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	return &codexCaptureDaemon{cmd: cmd, done: done, stateDir: stateDir}, nil
}

func codexCaptureDaemonTempRoot() string {
	if info, err := os.Stat("/tmp"); err == nil && info.IsDir() {
		return "/tmp"
	}
	return ""
}

func codexCaptureDaemonEnv(base []string, flags codexCaptureRunFlags, stateDir string) []string {
	env := make([]string, 0, len(base)+4)
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			env = append(env, entry)
			continue
		}
		switch key {
		case "SLIMFERENCE_WSS_AB_CAPTURE", "SLIMFERENCE_LISTEN_ADDRESS", "SLIMFERENCE_LISTEN_PORT", "SLIMFERENCE_DAEMON_STATE_DIR":
			continue
		default:
			env = append(env, entry)
		}
	}
	env = append(env,
		"SLIMFERENCE_WSS_AB_CAPTURE="+flags.capturePath,
		"SLIMFERENCE_LISTEN_ADDRESS="+flags.host,
		"SLIMFERENCE_LISTEN_PORT="+flags.port,
		"SLIMFERENCE_DAEMON_STATE_DIR="+stateDir,
	)
	return env
}

func prepareCodexCaptureDaemonCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
	baseURL := "http://" + flags.host + ":" + flags.port
	stateData, err := fetchCodexCaptureAdminJSON(ctx, baseURL+"/_slimference/admin/state")
	if err != nil {
		return codexCaptureAdminSnapshot{}, err
	}
	state, err := parseCodexCaptureAdminStateJSON(stateData)
	if err != nil {
		return codexCaptureAdminSnapshot{}, err
	}
	if statusData, err := fetchCodexCaptureAdminJSON(ctx, baseURL+"/_slimference/admin/status"); err == nil {
		if status, err := parseCodexCaptureAdminStatusJSON(statusData); err == nil {
			mergeCodexCaptureAdminStatus(&state, status)
		}
	}
	return codexCaptureAdminSnapshotFromState(state), nil
}

func fetchCodexCaptureAdminJSON(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build admin request %s: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("admin endpoint %s returned HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read admin endpoint %s body: %w", url, err)
	}
	return data, nil
}

func parseCodexCaptureAdminStateJSON(data []byte) (codexCaptureAdminState, error) {
	var state codexCaptureAdminState
	if err := json.Unmarshal(data, &state); err != nil {
		return codexCaptureAdminState{}, fmt.Errorf("parse admin state JSON: %w", err)
	}
	return state, nil
}

func parseCodexCaptureAdminStatusJSON(data []byte) (codexCaptureAdminState, error) {
	var status codexCaptureAdminState
	if err := json.Unmarshal(data, &status); err != nil {
		return codexCaptureAdminState{}, fmt.Errorf("parse admin status JSON: %w", err)
	}
	return status, nil
}

func mergeCodexCaptureAdminStatus(state *codexCaptureAdminState, status codexCaptureAdminState) {
	if state == nil {
		return
	}
	state.ToolPrune = status.ToolPrune
	if status.OutputReduce.InjectedTurns != 0 ||
		status.OutputReduce.SkippedTurns != 0 ||
		status.OutputReduce.InputOverheadTokens != 0 ||
		status.OutputReduce.OutputTokensObserved != 0 ||
		len(status.OutputReduce.Downgrades) != 0 {
		state.OutputReduce = status.OutputReduce
	}
	if status.OutputReduceCounters != (codexCaptureOutputReduceCountersSnapshot{}) {
		state.OutputReduceCounters = status.OutputReduceCounters
	}
}

func codexCaptureAdminSnapshotFromState(setup codexCaptureAdminState) codexCaptureAdminSnapshot {
	return codexCaptureAdminSnapshot{
		BillableInputTokensSaved:  setup.Savings.BillableInputTokensSaved,
		InputTokensSaved:          setup.Savings.InputTokensSaved,
		OutputWireBytesSaved:      setup.Savings.OutputWireBytesSaved,
		RequestSideBytesReduced:   setup.Savings.RequestSideBytesReduced,
		ProviderCacheReadTokens:   setup.Savings.ProviderCacheReadTokens,
		ProviderCacheCreateTokens: setup.Savings.ProviderCacheCreateTokens,

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

		OutputReduceInjected:              setup.OutputReduce.InjectedTurns,
		OutputReduceSkipped:               setup.OutputReduce.SkippedTurns,
		OutputReduceInputOverheadTokens:   setup.OutputReduce.InputOverheadTokens,
		OutputReduceOutputTokensObserved:  setup.OutputReduce.OutputTokensObserved,
		OutputReduceDowngrades:            int64(len(setup.OutputReduce.Downgrades)),
		StopSeqRequestsModified:           int64(setup.OutputReduceCounters.StopSeqRequestsModified),
		StreamcutFired:                    int64(setup.OutputReduceCounters.StreamcutFired),
		RepdetResponsesRewritten:          int64(setup.OutputReduceCounters.RepdetResponsesRewritten),
		StaleReadBlocksReplaced:           int64(setup.OutputReduceCounters.StaleReadBlocksReplaced),
		ObsoleteReadBlocksPruned:          int64(setup.OutputReduceCounters.ObsoleteReadBlocksPruned),
		BeterseInjections:                 int64(setup.OutputReduceCounters.BeterseInjections),
		WSSStatefulPrefixElisionRequests:  int64(setup.OutputReduceCounters.WSSStatefulPrefixElisionRequests),
		WSSStatefulPrefixElisionTools:     int64(setup.OutputReduceCounters.WSSStatefulPrefixElisionTools),
		WSSStatefulPrefixElisionBytes:     int64(setup.OutputReduceCounters.WSSStatefulPrefixElisionBytes),
		WSSStatefulPrefixElisionTokens:    int64(setup.OutputReduceCounters.WSSStatefulPrefixElisionTokens),
		WSSStatefulPrefixInstructionsKept: int64(setup.OutputReduceCounters.WSSStatefulPrefixInstructionsKept),

		ParseFailures:                     setup.WSS.ParseFailures,
		DegradedSessions:                  setup.WSS.DegradedSessions,
		CompressionErrors:                 setup.WSS.CompressionErrors,
		AnalyticsProofEventsDropped:       setup.Savings.AnalyticsProofEventsDropped,
		AnalyticsLowPriorityEventsDropped: setup.Savings.AnalyticsLowPriorityEventsDropped,

		HostBudgetStatus:        setup.HostBudget.Status,
		HostBudgetExceeded:      setup.HostBudget.Exceeded,
		HostBudgetReasons:       append([]string(nil), setup.HostBudget.Reasons...),
		HostBudgetRSSBytes:      setup.HostBudget.RSSBytes,
		HostBudgetGoRetainedB:   setup.HostBudget.GoRetainedBytes,
		HostBudgetEffectiveRSSB: setup.HostBudget.EffectiveRSSBytes,
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
		BillableInputTokensSaved:  nonNegativeDelta(current.BillableInputTokensSaved, base.BillableInputTokensSaved),
		InputTokensSaved:          nonNegativeDelta(current.InputTokensSaved, base.InputTokensSaved),
		OutputWireBytesSaved:      nonNegativeDelta(current.OutputWireBytesSaved, base.OutputWireBytesSaved),
		RequestSideBytesReduced:   nonNegativeDelta(current.RequestSideBytesReduced, base.RequestSideBytesReduced),
		ProviderCacheReadTokens:   nonNegativeDelta(current.ProviderCacheReadTokens, base.ProviderCacheReadTokens),
		ProviderCacheCreateTokens: nonNegativeDelta(current.ProviderCacheCreateTokens, base.ProviderCacheCreateTokens),

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

		OutputReduceInjected:              nonNegativeDelta(current.OutputReduceInjected, base.OutputReduceInjected),
		OutputReduceSkipped:               nonNegativeDelta(current.OutputReduceSkipped, base.OutputReduceSkipped),
		OutputReduceInputOverheadTokens:   nonNegativeDelta(current.OutputReduceInputOverheadTokens, base.OutputReduceInputOverheadTokens),
		OutputReduceOutputTokensObserved:  nonNegativeDelta(current.OutputReduceOutputTokensObserved, base.OutputReduceOutputTokensObserved),
		OutputReduceDowngrades:            nonNegativeDelta(current.OutputReduceDowngrades, base.OutputReduceDowngrades),
		StopSeqRequestsModified:           nonNegativeDelta(current.StopSeqRequestsModified, base.StopSeqRequestsModified),
		StreamcutFired:                    nonNegativeDelta(current.StreamcutFired, base.StreamcutFired),
		RepdetResponsesRewritten:          nonNegativeDelta(current.RepdetResponsesRewritten, base.RepdetResponsesRewritten),
		StaleReadBlocksReplaced:           nonNegativeDelta(current.StaleReadBlocksReplaced, base.StaleReadBlocksReplaced),
		ObsoleteReadBlocksPruned:          nonNegativeDelta(current.ObsoleteReadBlocksPruned, base.ObsoleteReadBlocksPruned),
		BeterseInjections:                 nonNegativeDelta(current.BeterseInjections, base.BeterseInjections),
		WSSStatefulPrefixElisionRequests:  nonNegativeDelta(current.WSSStatefulPrefixElisionRequests, base.WSSStatefulPrefixElisionRequests),
		WSSStatefulPrefixElisionTools:     nonNegativeDelta(current.WSSStatefulPrefixElisionTools, base.WSSStatefulPrefixElisionTools),
		WSSStatefulPrefixElisionBytes:     nonNegativeDelta(current.WSSStatefulPrefixElisionBytes, base.WSSStatefulPrefixElisionBytes),
		WSSStatefulPrefixElisionTokens:    nonNegativeDelta(current.WSSStatefulPrefixElisionTokens, base.WSSStatefulPrefixElisionTokens),
		WSSStatefulPrefixInstructionsKept: nonNegativeDelta(current.WSSStatefulPrefixInstructionsKept, base.WSSStatefulPrefixInstructionsKept),

		ParseFailures:                     nonNegativeDelta(current.ParseFailures, base.ParseFailures),
		DegradedSessions:                  nonNegativeDelta(current.DegradedSessions, base.DegradedSessions),
		CompressionErrors:                 nonNegativeDelta(current.CompressionErrors, base.CompressionErrors),
		AnalyticsProofEventsDropped:       nonNegativeDelta(current.AnalyticsProofEventsDropped, base.AnalyticsProofEventsDropped),
		AnalyticsLowPriorityEventsDropped: nonNegativeDelta(current.AnalyticsLowPriorityEventsDropped, base.AnalyticsLowPriorityEventsDropped),

		HostBudgetStatus:        current.HostBudgetStatus,
		HostBudgetExceeded:      current.HostBudgetExceeded,
		HostBudgetReasons:       append([]string(nil), current.HostBudgetReasons...),
		HostBudgetRSSBytes:      current.HostBudgetRSSBytes,
		HostBudgetGoRetainedB:   current.HostBudgetGoRetainedB,
		HostBudgetEffectiveRSSB: current.HostBudgetEffectiveRSSB,
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

func runCodexCaptureWithOptionalRestart(ctx context.Context, flags codexCaptureRunFlags, daemon *codexCaptureDaemon, deps codexCaptureRunDeps, stdout, stderr, log io.Writer) (codexCaptureRunExecution, error) {
	if flags.restartAfterCompletion <= 0 && flags.restartAfterMutatedCompletion <= 0 {
		err := deps.runCodex(ctx, flags, stdout, stderr)
		return codexCaptureRunExecution{daemon: daemon}, err
	}
	if deps.runCodex == nil || deps.stopDaemon == nil || deps.startDaemon == nil || deps.waitHealth == nil || deps.adminSnapshot == nil {
		return codexCaptureRunExecution{daemon: daemon}, errors.New("restart capture dependencies are not configured")
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	runDone := make(chan error, 1)
	go func() {
		runDone <- deps.runCodex(runCtx, flags, stdout, stderr)
	}()

	restartDone := make(chan codexCaptureRestartResult, 1)
	go func() {
		restartDone <- restartCodexCaptureDaemonAfterConfiguredCompletion(runCtx, flags, daemon, deps, log)
	}()

	current := daemon
	var preRestartSnapshot *codexCaptureAdminSnapshot
	for {
		select {
		case err := <-runDone:
			cancelRun()
			return codexCaptureRunExecution{daemon: current, preRestartSnapshot: preRestartSnapshot}, err
		case result := <-restartDone:
			if result.err != nil {
				cancelRun()
				runErr := <-runDone
				if runErr != nil && !errors.Is(runErr, context.Canceled) {
					return codexCaptureRunExecution{daemon: current, preRestartSnapshot: preRestartSnapshot}, fmt.Errorf("%w; codex run also failed: %v", result.err, runErr)
				}
				return codexCaptureRunExecution{daemon: current, preRestartSnapshot: preRestartSnapshot}, result.err
			}
			current = result.daemon
			preRestartSnapshot = result.preRestartSnapshot
			// One forced reconnect is the proof shape. More would blur the
			// capture, hide root cause, and make E5 harder to interpret.
			err := <-runDone
			cancelRun()
			return codexCaptureRunExecution{daemon: current, preRestartSnapshot: preRestartSnapshot}, err
		}
	}
}

type codexCaptureRestartResult struct {
	daemon             *codexCaptureDaemon
	preRestartSnapshot *codexCaptureAdminSnapshot
	err                error
}

func restartCodexCaptureDaemonAfterConfiguredCompletion(ctx context.Context, flags codexCaptureRunFlags, daemon *codexCaptureDaemon, deps codexCaptureRunDeps, log io.Writer) codexCaptureRestartResult {
	if flags.restartAfterCompletion > 0 {
		return restartCodexCaptureDaemonAfterCompletion(ctx, flags, daemon, deps, log)
	}
	return restartCodexCaptureDaemonAfterMutatedCompletion(ctx, flags, daemon, deps, log)
}

func restartCodexCaptureDaemonAfterCompletion(ctx context.Context, flags codexCaptureRunFlags, daemon *codexCaptureDaemon, deps codexCaptureRunDeps, log io.Writer) codexCaptureRestartResult {
	if err := waitCodexCaptureCompletion(ctx, flags.capturePath, flags.restartAfterCompletion); err != nil {
		return codexCaptureRestartResult{daemon: daemon, err: err}
	}
	preRestart, err := deps.adminSnapshot(ctx, flags)
	if err != nil {
		return codexCaptureRestartResult{daemon: daemon, err: fmt.Errorf("restart capture daemon after completion: read pre-restart admin state: %w", err)}
	}
	if err := deps.stopDaemon(ctx, daemon); err != nil {
		return codexCaptureRestartResult{daemon: daemon, preRestartSnapshot: &preRestart, err: fmt.Errorf("restart capture daemon after completion: stop old daemon: %w", err)}
	}
	next, err := deps.startDaemon(ctx, flags, log)
	if err != nil {
		return codexCaptureRestartResult{daemon: daemon, preRestartSnapshot: &preRestart, err: fmt.Errorf("restart capture daemon after completion: start new daemon: %w", err)}
	}
	if err := deps.waitHealth(ctx, flags, next.done); err != nil {
		_ = deps.stopDaemon(context.Background(), next)
		return codexCaptureRestartResult{daemon: daemon, preRestartSnapshot: &preRestart, err: fmt.Errorf("restart capture daemon after completion: wait health: %w", err)}
	}
	if log != nil {
		fmt.Fprintf(log, "capture daemon restarted after completion %d\n", flags.restartAfterCompletion)
	}
	return codexCaptureRestartResult{daemon: next, preRestartSnapshot: &preRestart}
}

func restartCodexCaptureDaemonAfterMutatedCompletion(ctx context.Context, flags codexCaptureRunFlags, daemon *codexCaptureDaemon, deps codexCaptureRunDeps, log io.Writer) codexCaptureRestartResult {
	if err := waitCodexCaptureMutatedCompletion(ctx, flags.capturePath, flags.restartAfterMutatedCompletion); err != nil {
		return codexCaptureRestartResult{daemon: daemon, err: err}
	}
	preRestart, err := deps.adminSnapshot(ctx, flags)
	if err != nil {
		return codexCaptureRestartResult{daemon: daemon, err: fmt.Errorf("restart capture daemon after mutated completion: read pre-restart admin state: %w", err)}
	}
	if err := deps.stopDaemon(ctx, daemon); err != nil {
		return codexCaptureRestartResult{daemon: daemon, preRestartSnapshot: &preRestart, err: fmt.Errorf("restart capture daemon after mutated completion: stop old daemon: %w", err)}
	}
	next, err := deps.startDaemon(ctx, flags, log)
	if err != nil {
		return codexCaptureRestartResult{daemon: daemon, preRestartSnapshot: &preRestart, err: fmt.Errorf("restart capture daemon after mutated completion: start new daemon: %w", err)}
	}
	if err := deps.waitHealth(ctx, flags, next.done); err != nil {
		_ = deps.stopDaemon(context.Background(), next)
		return codexCaptureRestartResult{daemon: daemon, preRestartSnapshot: &preRestart, err: fmt.Errorf("restart capture daemon after mutated completion: wait health: %w", err)}
	}
	if log != nil {
		fmt.Fprintf(log, "capture daemon restarted after mutated completion %d\n", flags.restartAfterMutatedCompletion)
	}
	return codexCaptureRestartResult{daemon: next, preRestartSnapshot: &preRestart}
}

func waitCodexCaptureCompletion(ctx context.Context, path string, target int) error {
	if target <= 0 {
		return nil
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if codexCaptureHasCompletion(path, target) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitCodexCaptureMutatedCompletion(ctx context.Context, path string, target int) error {
	if target <= 0 {
		return nil
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if codexCaptureHasMutatedCompletion(path, target) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func codexCaptureHasCompletion(path string, target int) bool {
	if strings.TrimSpace(path) == "" || target <= 0 {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	completed := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var frame struct {
			Direction string          `json:"direction"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		if codexCaptureFrameFromServer(frame.Direction) && codexCapturePayloadType(frame.Payload) == "response.completed" {
			completed++
			if completed >= target {
				return true
			}
		}
	}
	return false
}

func codexCaptureHasMutatedCompletion(path string, target int) bool {
	if strings.TrimSpace(path) == "" || target <= 0 {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	mutated := 0
	armed := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var frame struct {
			Direction string          `json:"direction"`
			Mutated   bool            `json:"mutated"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		if codexCaptureFrameFromClient(frame.Direction) && frame.Mutated {
			mutated++
			if mutated >= target {
				armed = true
			}
			continue
		}
		if armed && codexCaptureFrameFromServer(frame.Direction) && codexCapturePayloadType(frame.Payload) == "response.completed" {
			return true
		}
	}
	return false
}

func codexCaptureFrameFromClient(direction string) bool {
	direction = strings.ToLower(strings.TrimSpace(direction))
	return direction == "c2s" || direction == "client_to_server" || direction == "client" || direction == "request"
}

func codexCapturePayloadType(payload json.RawMessage) string {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return ""
	}
	if payload[0] == '"' {
		var encoded string
		if err := json.Unmarshal(payload, &encoded); err != nil {
			return ""
		}
		payload = []byte(encoded)
	}
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return ""
	}
	return env.Type
}

func runCodexCaptureCLI(ctx context.Context, flags codexCaptureRunFlags, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, flags.binary, codexCaptureCLIArgs(flags)...)
	cmd.Stdin = os.Stdin
	if flags.exitMarker != "" {
		return runCodexCaptureCLIUntilMarker(ctx, cmd, flags, stdout, stderr)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run scoped Codex capture: %w", err)
	}
	return nil
}

func codexCaptureCLIArgs(flags codexCaptureRunFlags) []string {
	args := []string{"codex", "run", "--transport=" + flags.transport, "--host=" + flags.host, "--port=" + flags.port, "--"}
	args = append(args, flags.codexArgs...)
	return args
}

func runCodexCaptureCLIUntilMarker(ctx context.Context, cmd *exec.Cmd, flags codexCaptureRunFlags, stdout, stderr io.Writer) error {
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
	go watchCodexCaptureMarkerFunc(logPath, flags.exitMarker, flags.exitMarkerCount, signalMarker, stopWatch)
	go watchCodexCaptureServerMarker(flags.capturePath, flags.exitMarker, flags.exitMarkerCount, signalMarker, stopWatch)
	go watchCodexCaptureFunctionOutputMarker(flags.capturePath, flags.exitMarker, flags.exitMarkerCount, signalMarker, stopWatch)
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

func watchCodexCaptureServerMarker(path, marker string, markerCount int, signal func(), stop <-chan struct{}) {
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
			if !signaled && countCodexCaptureServerMarker(data, marker) >= markerCount {
				signaled = true
				signal()
			}
		}
	}
}

func countCodexCaptureServerMarker(data []byte, marker string) int {
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
			Direction string          `json:"direction"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		if !codexCaptureFrameFromServer(frame.Direction) {
			continue
		}
		payload := codexCaptureDecodedPayload(frame.Payload)
		count += strings.Count(normalizeCodexCaptureMarkerText(string(payload)), needle)
	}
	return count
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
		if daemon != nil && daemon.stateDir != "" {
			_ = os.RemoveAll(daemon.stateDir)
		}
		return nil
	}
	defer func() {
		if daemon.stateDir != "" {
			_ = os.RemoveAll(daemon.stateDir)
		}
	}()
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
		ABPairID:            flags.abPairID,
		ABVariant:           flags.abVariant,
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

func validateABProofFlags(pairID, variant string) error {
	pairID = strings.TrimSpace(pairID)
	variant = strings.TrimSpace(strings.ToLower(variant))
	if pairID == "" && variant == "" {
		return nil
	}
	if pairID == "" || variant == "" {
		return fmt.Errorf("--ab-pair-id and --ab-variant must be set together")
	}
	switch variant {
	case "baseline", "directive":
		return nil
	default:
		return fmt.Errorf("--ab-variant must be baseline or directive")
	}
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
	fmt.Fprintf(w, "  shapes:        root=%d delta=%d full_history=%d\n",
		result.Replay.RequestShapes.Root, result.Replay.RequestShapes.Delta, result.Replay.RequestShapes.FullHistory)
	fmt.Fprintf(w, "  mutated_shapes root=%d delta=%d full_history=%d\n",
		result.Replay.MutatedShapes.Root, result.Replay.MutatedShapes.Delta, result.Replay.MutatedShapes.FullHistory)
	if result.Replay.CapturedMutatedRequests > 0 {
		fmt.Fprintf(w, "  captured:      %d\n", result.Replay.CapturedMutatedRequests)
		fmt.Fprintf(w, "  captured_shapes root=%d delta=%d full_history=%d\n",
			result.Replay.CapturedMutatedShapes.Root, result.Replay.CapturedMutatedShapes.Delta, result.Replay.CapturedMutatedShapes.FullHistory)
	}
	if result.LiveDelta != nil {
		fmt.Fprintf(w, "  billable_input_tokens_saved: %d\n", result.LiveDelta.BillableInputTokensSaved)
		fmt.Fprintf(w, "  input_tokens_saved:          %d\n", result.LiveDelta.InputTokensSaved)
		fmt.Fprintf(w, "  output_wire_bytes_saved:     %d\n", result.LiveDelta.OutputWireBytesSaved)
		fmt.Fprintf(w, "  provider_cache_read/create:  %d / %d\n",
			result.LiveDelta.ProviderCacheReadTokens, result.LiveDelta.ProviderCacheCreateTokens)
		fmt.Fprintf(w, "  provider_input_tokens:       %d\n", result.LiveDelta.ProviderInputTokens)
		fmt.Fprintf(w, "  provider_output_tokens:      %d\n", result.LiveDelta.ProviderOutputTokens)
		writeCodexCaptureWireSurfaceSummary(w, result.LiveDelta)
		fmt.Fprintf(w, "  layer0_live read/captured/envelope/repeated/chunk/refs: %d / %d / %d / %d / %d / %d\n",
			result.LiveDelta.ProxyLayer0ReadDelta, result.LiveDelta.ProxyLayer0Captured,
			result.LiveDelta.ProxyLayer0Envelope, result.LiveDelta.ProxyLayer0Repeated,
			result.LiveDelta.ProxyLayer0ChunkDedup, result.LiveDelta.ProxyLayer0ChunkRefs)
		if result.LiveDelta.WSSStatefulPrefixElisionRequests > 0 {
			fmt.Fprintf(w, "  wss_prefix_elision req/tools/tokens/bytes: %d / %d / %d / %d\n",
				result.LiveDelta.WSSStatefulPrefixElisionRequests,
				result.LiveDelta.WSSStatefulPrefixElisionTools,
				result.LiveDelta.WSSStatefulPrefixElisionTokens,
				result.LiveDelta.WSSStatefulPrefixElisionBytes)
		}
		fmt.Fprintf(w, "  safety_parse/degraded/compression: %d / %d / %d\n",
			result.LiveDelta.ParseFailures, result.LiveDelta.DegradedSessions, result.LiveDelta.CompressionErrors)
		fmt.Fprintf(w, "  analytics_proof/low_dropped: %d / %d\n",
			result.LiveDelta.AnalyticsProofEventsDropped, result.LiveDelta.AnalyticsLowPriorityEventsDropped)
		writeCodexCaptureHostBudgetSummary(w, result.LiveDelta)
		writeCodexCapturePolicySummary(w, result.LiveDelta.ProxyLayer0Policy)
		writeCodexCaptureCacheSummary(w, result.LiveDelta.ProxyLayer0Cache)
	}
	fmt.Fprintf(w, "  replay_bytes_saved: %d\n", result.Replay.BytesSaved)
	fmt.Fprintf(w, "  upstream_errors: frames=%d invalid_request=%d http_400=%d response_failed=%d\n",
		result.Replay.UpstreamErrorFrames,
		result.Replay.UpstreamInvalidRequests,
		result.Replay.UpstreamHTTP400Errors,
		result.Replay.UpstreamResponseFailures)
	fmt.Fprintf(w, "  lost:          %d\n", result.Replay.Lost)
	fmt.Fprintf(w, "  gate:          %s\n", passFail(result.Replay.GatePassed))
}

func writeCodexCaptureWireSurfaceSummary(w io.Writer, delta *codexCaptureLiveDelta) {
	if delta == nil || delta.WireSurfaceFrames == 0 {
		return
	}
	fmt.Fprintf(w, "  wire_surface frames/client_creates/tools_max/input_items/function_outputs/server_items/function_calls: %d / %d / %d / %d / %d / %d / %d\n",
		delta.WireSurfaceFrames,
		delta.WireClientResponseCreates,
		delta.WireClientDeclaredToolsMax,
		delta.WireClientInputItems,
		delta.WireFunctionCallOutputs,
		delta.WireServerOutputItems,
		delta.WireServerFunctionCalls)
	if len(delta.WireClientInputItemTypes) > 0 {
		fmt.Fprintf(w, "  wire_client_input_types: %s\n", formatCodexCaptureInt64Map(delta.WireClientInputItemTypes))
	}
	if len(delta.WireServerOutputItemTypes) > 0 {
		fmt.Fprintf(w, "  wire_server_output_types: %s\n", formatCodexCaptureInt64Map(delta.WireServerOutputItemTypes))
	}
}

func formatCodexCaptureInt64Map(values map[string]int64) string {
	if len(values) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return strings.Join(parts, ",")
}

func writeCodexCaptureHostBudgetSummary(w io.Writer, delta *codexCaptureLiveDelta) {
	if delta == nil || delta.HostBudgetStatus == "" {
		return
	}
	reasons := "-"
	if len(delta.HostBudgetReasons) > 0 {
		reasons = strings.Join(delta.HostBudgetReasons, ",")
	}
	fmt.Fprintf(w, "  host_budget: %s exceeded=%v reasons=%s cpu_window=%.2f%%/%.2fs rss=%d effective_rss=%d go_retained=%d state=%d\n",
		delta.HostBudgetStatus, delta.HostBudgetExceeded, reasons, delta.HostBudgetCPUWindowPct,
		delta.HostBudgetCPUWindowSec, delta.HostBudgetRSSBytes, delta.HostBudgetEffectiveRSSB,
		delta.HostBudgetGoRetainedB, delta.HostBudgetStateBytes)
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

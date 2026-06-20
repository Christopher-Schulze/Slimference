package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
)

type wssT354ShapeProofFlags struct {
	path                    string
	t420HandoffPath         string
	t408OpenSlicePath       string
	socketSeq               uint64
	outputFormat            string
	requireRecoveryContract bool
	help                    bool
}

type wssT354ShapeProofReport struct {
	Path                 string                        `json:"path"`
	T420HandoffPath      string                        `json:"t420_handoff_path,omitempty"`
	T408OpenSlicePath    string                        `json:"t408_open_slice_path,omitempty"`
	SocketSeq            uint64                        `json:"socket_seq,omitempty"`
	FrameFiles           int                           `json:"frame_files"`
	SkippedFiles         int                           `json:"skipped_files"`
	Totals               wssT354ShapeProofTotal        `json:"totals"`
	Rows                 []wssT354ShapeProofRow        `json:"rows"`
	T420ReconnectHandoff []wssT354T420ReconnectHandoff `json:"t420_reconnect_handoff,omitempty"`
	T408OpenSlices       []wssT354T408OpenSlice        `json:"t408_open_slices,omitempty"`
	T419RecoveryContract *wssT354RecoveryContractGate  `json:"t419_recovery_contract,omitempty"`
	Skips                []wssT354ShapeProofSkip       `json:"skips,omitempty"`
	Findings             []string                      `json:"findings,omitempty"`
	GatePassed           bool                          `json:"gate_passed"`
	GateFailures         []string                      `json:"gate_failures,omitempty"`
}

type wssT354RecoveryContractGate struct {
	Rows                       int      `json:"rows"`
	ProductReady               int      `json:"product_ready"`
	ProductGaps                int      `json:"product_gaps"`
	BlockedRows                int      `json:"blocked_rows"`
	ArchiveBackedRows          int      `json:"archive_backed_rows"`
	RehydrateBeforeUpstream    int      `json:"rehydrate_before_upstream_rows"`
	T417ServerStateRowReady    bool     `json:"t417_server_state_row_recovery_ready"`
	T417ServerStateRowBlockers []string `json:"t417_server_state_row_blockers,omitempty"`
	GatePassed                 bool     `json:"gate_passed"`
	GateFailures               []string `json:"gate_failures,omitempty"`
}

type wssT354ShapeProofTotal struct {
	Frames                         int                   `json:"frames"`
	RequestTurns                   int                   `json:"request_turns"`
	RequestShapes                  replayShapeCounts     `json:"request_shapes"`
	MutatedRequests                int                   `json:"mutated_requests"`
	MutatedToolOutputCandidates    int                   `json:"mutated_tool_output_candidates"`
	MutatedDeltaCandidates         int                   `json:"mutated_delta_candidates"`
	MutatedFullHistoryCandidates   int                   `json:"mutated_full_history_candidates"`
	CandidatesWithCleanCurrentTurn int                   `json:"candidates_with_clean_current_turn"`
	CandidatesWithFollowingTurn    int                   `json:"candidates_with_following_turn"`
	CandidatesWithCleanFollowing   int                   `json:"candidates_with_clean_following"`
	CandidatesPassing              int                   `json:"candidates_passing"`
	UpstreamErrorFrames            int                   `json:"upstream_error_frames"`
	InvalidRequestErrors           int                   `json:"invalid_request_errors"`
	HTTP400Errors                  int                   `json:"http_400_errors"`
	ResponseFailedFrames           int                   `json:"response_failed_frames"`
	Lost                           int                   `json:"lost"`
	ReplayLocalSavedTokens         int                   `json:"replay_local_saved_tokens"`
	CapturedLocalSavedTokens       int                   `json:"captured_local_saved_tokens_estimate"`
	RetryOrResendExtraTokens       int                   `json:"retry_or_resend_extra_tokens_estimate"`
	NetCapturedLocalSavedTokens    int                   `json:"net_captured_local_saved_tokens_estimate"`
	ProviderUsage                  wssT354ProviderUsage  `json:"provider_usage"`
	MissingFollowingTurnCandidates int                   `json:"missing_following_turn_candidates"`
	UnsafeCandidates               int                   `json:"unsafe_candidates"`
	UnprovenCandidates             int                   `json:"unproven_candidates,omitempty"`
	MetadataComparisons            int                   `json:"metadata_comparisons,omitempty"`
	MetadataMismatches             int                   `json:"metadata_mismatches,omitempty"`
	CandidatesWithServerOutputItem int                   `json:"candidates_with_server_output_item,omitempty"`
	CandidatesWithServerOutputID   int                   `json:"candidates_with_server_output_id,omitempty"`
	T420ReconnectHandoffRows       int                   `json:"t420_reconnect_handoff_rows,omitempty"`
	T420ReconnectInputTokens       int                   `json:"t420_reconnect_input_tokens,omitempty"`
	T420RetryResendCostTokens      int                   `json:"t420_retry_resend_cost_tokens,omitempty"`
	T420ProviderInputTokens        int                   `json:"t420_provider_input_tokens,omitempty"`
	T420ProviderCachedTokens       int                   `json:"t420_provider_cached_tokens,omitempty"`
	T420LocalSavedTokens           int                   `json:"t420_local_saved_tokens,omitempty"`
	T417ReconnectRerouteCandidates int                   `json:"t417_reconnect_reroute_candidates,omitempty"`
	T420TransportFixCandidates     int                   `json:"t420_transport_fix_candidates,omitempty"`
	T408OpenSliceRows              int                   `json:"t408_open_slice_rows,omitempty"`
	T408OpenSliceRequests          int                   `json:"t408_open_slice_requests,omitempty"`
	T408OpenSliceCandidateTokens   int                   `json:"t408_open_slice_candidate_tokens_estimate,omitempty"`
	T408OpenSliceLocalSavedTokens  int                   `json:"t408_open_slice_local_saved_tokens,omitempty"`
	T408OpenSliceHeadroomTokens    int                   `json:"t408_open_slice_headroom_tokens,omitempty"`
	TopT408OpenSlice               *wssT354T408OpenSlice `json:"top_t408_open_slice,omitempty"`
	NetPositiveCandidates          int                   `json:"net_positive_candidates,omitempty"`
	NetPositiveFullHistory         int                   `json:"net_positive_full_history_candidates,omitempty"`
	NetPositiveCapturedSavedTokens int                   `json:"net_positive_captured_local_saved_tokens,omitempty"`
	NetPositiveRetryExtraTokens    int                   `json:"net_positive_retry_or_resend_extra_tokens,omitempty"`
	NetPositiveNetSavedTokens      int                   `json:"net_positive_net_captured_local_saved_tokens,omitempty"`
	TopNetCandidate                *wssT354TopCandidate  `json:"top_net_candidate,omitempty"`
}

type wssT354ShapeProofRow struct {
	Path                           string                  `json:"path"`
	SocketSeq                      uint64                  `json:"socket_seq,omitempty"`
	Frames                         int                     `json:"frames"`
	RequestTurns                   int                     `json:"request_turns"`
	RequestShapes                  replayShapeCounts       `json:"request_shapes"`
	Candidates                     []wssT354CandidateProof `json:"candidates,omitempty"`
	Upstream                       wssT354UpstreamProof    `json:"upstream"`
	Lost                           int                     `json:"lost"`
	ReplayLocalSavedTokens         int                     `json:"replay_local_saved_tokens"`
	CapturedLocalSavedTokens       int                     `json:"captured_local_saved_tokens_estimate"`
	RetryOrResendExtraTokens       int                     `json:"retry_or_resend_extra_tokens_estimate"`
	NetCapturedLocalSavedTokens    int                     `json:"net_captured_local_saved_tokens_estimate"`
	ProviderUsage                  wssT354ProviderUsage    `json:"provider_usage"`
	MetadataComparisons            int                     `json:"metadata_comparisons,omitempty"`
	MetadataMismatches             int                     `json:"metadata_mismatches,omitempty"`
	CandidatesWithServerOutputItem int                     `json:"candidates_with_server_output_item,omitempty"`
	CandidatesWithServerOutputID   int                     `json:"candidates_with_server_output_id,omitempty"`
	NetPositiveCandidates          int                     `json:"net_positive_candidates,omitempty"`
	NetPositiveFullHistory         int                     `json:"net_positive_full_history_candidates,omitempty"`
	NetPositiveCapturedSavedTokens int                     `json:"net_positive_captured_local_saved_tokens,omitempty"`
	NetPositiveRetryExtraTokens    int                     `json:"net_positive_retry_or_resend_extra_tokens,omitempty"`
	NetPositiveNetSavedTokens      int                     `json:"net_positive_net_captured_local_saved_tokens,omitempty"`
	TopNetCandidate                *wssT354TopCandidate    `json:"top_net_candidate,omitempty"`
	GatePassed                     bool                    `json:"gate_passed"`
	GateFailures                   []string                `json:"gate_failures,omitempty"`
}

type wssT354CandidateProof struct {
	TurnIndex                      int                       `json:"turn_index"`
	Shape                          string                    `json:"shape"`
	PreviousResponseID             bool                      `json:"previous_response_id"`
	ToolOutputs                    int                       `json:"tool_outputs"`
	CustomToolOutputs              int                       `json:"custom_tool_outputs"`
	RequestTokensEstimate          int                       `json:"request_tokens_estimate"`
	CapturedOriginalRequestTokens  int                       `json:"captured_original_request_tokens_estimate,omitempty"`
	CapturedLocalSavedTokens       int                       `json:"captured_local_saved_tokens_estimate,omitempty"`
	CurrentTurnClean               bool                      `json:"current_turn_clean"`
	CurrentTurnHealth              wssT354TurnHealth         `json:"current_turn_health"`
	FollowingTurnPresent           bool                      `json:"following_turn_present"`
	FollowingTurnShape             string                    `json:"following_turn_shape,omitempty"`
	FollowingRequestTokensEstimate int                       `json:"following_request_tokens_estimate,omitempty"`
	RetryOrResendExtraTokens       int                       `json:"retry_or_resend_extra_tokens_estimate,omitempty"`
	NetCapturedLocalSavedTokens    int                       `json:"net_captured_local_saved_tokens_estimate,omitempty"`
	PromotionEligible              bool                      `json:"promotion_eligible"`
	EconomicsVerdict               string                    `json:"economics_verdict,omitempty"`
	ContinuationCandidate          string                    `json:"continuation_candidate,omitempty"`
	FollowingTurnClean             bool                      `json:"following_turn_clean"`
	FollowingTurnHealth            *wssT354TurnHealth        `json:"following_turn_health,omitempty"`
	UnlockProofPassing             bool                      `json:"unlock_proof_passing"`
	MetadataConsistency            string                    `json:"metadata_consistency,omitempty"`
	OriginalMetadataFootprint      *wssT354MetadataFootprint `json:"original_metadata_footprint,omitempty"`
	MutatedMetadataFootprint       *wssT354MetadataFootprint `json:"mutated_metadata_footprint,omitempty"`
	CurrentTurnServerOutputItems   int                       `json:"current_turn_server_output_items,omitempty"`
	CurrentTurnServerOutputIDs     int                       `json:"current_turn_server_output_ids,omitempty"`
	BlockReasons                   []string                  `json:"block_reasons,omitempty"`
}

type wssT354TopCandidate struct {
	Path                        string   `json:"path,omitempty"`
	TurnIndex                   int      `json:"turn_index"`
	Shape                       string   `json:"shape"`
	PreviousResponseID          bool     `json:"previous_response_id"`
	FollowingTurnShape          string   `json:"following_turn_shape,omitempty"`
	CapturedLocalSavedTokens    int      `json:"captured_local_saved_tokens_estimate"`
	RetryOrResendExtraTokens    int      `json:"retry_or_resend_extra_tokens_estimate"`
	NetCapturedLocalSavedTokens int      `json:"net_captured_local_saved_tokens_estimate"`
	EconomicsVerdict            string   `json:"economics_verdict"`
	ContinuationCandidate       string   `json:"continuation_candidate"`
	BlockReasons                []string `json:"block_reasons,omitempty"`
}

type wssT354MetadataFootprint struct {
	ReferenceFields int `json:"reference_fields"`
	MetadataFields  int `json:"metadata_fields"`
	ShapeFields     int `json:"shape_fields"`
	fingerprint     string
}

type wssT354ProviderUsage struct {
	InputTokens      int     `json:"input_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CachedPct        float64 `json:"cached_pct,omitempty"`
	CompletionFrames int     `json:"completion_frames,omitempty"`
}

type wssT354TurnHealth struct {
	Terminal             bool `json:"terminal"`
	ErrorFrames          int  `json:"error_frames"`
	HTTP400Errors        int  `json:"http_400_errors"`
	InvalidRequestErrors int  `json:"invalid_request_errors"`
	ResponseFailedFrames int  `json:"response_failed_frames"`
}

type wssT354UpstreamProof struct {
	ErrorFrames          int `json:"error_frames"`
	HTTP400Errors        int `json:"http_400_errors"`
	InvalidRequestErrors int `json:"invalid_request_errors"`
	ResponseFailedFrames int `json:"response_failed_frames"`
}

type wssT354ShapeProofSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type wssT354T420ReconnectHandoff struct {
	SocketKey                     string         `json:"socket_key"`
	SessionID                     string         `json:"session_id,omitempty"`
	Cause                         string         `json:"cause"`
	RequestShapes                 map[string]int `json:"request_shapes,omitempty"`
	Requests                      int            `json:"requests"`
	FullHistoryRequests           int            `json:"full_history_requests"`
	ProviderInputTokens           int            `json:"provider_input_tokens"`
	ProviderCachedTokens          int            `json:"provider_cached_tokens"`
	LocalSavedTokens              int            `json:"local_saved_tokens"`
	ReconnectInputTokens          int            `json:"reconnect_input_tokens"`
	RetryResendCostTokens         int            `json:"retry_resend_cost_tokens"`
	PreviousSocketKey             string         `json:"previous_socket_key,omitempty"`
	PreviousClose                 string         `json:"previous_close_initiator,omitempty"`
	ReconnectGapMillis            int64          `json:"reconnect_gap_ms,omitempty"`
	Attribution                   string         `json:"attribution,omitempty"`
	ContinuationCandidate         string         `json:"continuation_candidate"`
	RecommendedAction             string         `json:"recommended_action,omitempty"`
	CandidatePotentialLocalTokens int            `json:"candidate_potential_local_tokens,omitempty"`
	EconomicsVerdict              string         `json:"economics_verdict,omitempty"`
}

type wssT354T408OpenSlice struct {
	RequestShape                 string   `json:"request_shape"`
	Kind                         string   `json:"kind"`
	OpenRequests                 int      `json:"open_requests"`
	OpenCandidateTokens          int      `json:"open_candidate_tokens_estimate"`
	OpenLocalSavedTokens         int      `json:"open_local_saved_tokens"`
	OpenHeadroomTokens           int      `json:"open_headroom_tokens"`
	ProviderInputTokens          int      `json:"provider_input_tokens,omitempty"`
	ProviderCachedTokens         int      `json:"provider_cached_tokens,omitempty"`
	AggregateHeadroomTokens      int      `json:"aggregate_headroom_tokens,omitempty"`
	AggregateBlockers            []string `json:"aggregate_blockers,omitempty"`
	TopSessionID                 string   `json:"top_session_id,omitempty"`
	TopSessionOpenRequests       int      `json:"top_session_open_requests,omitempty"`
	TopSessionOpenHeadroomTokens int      `json:"top_session_open_headroom_tokens,omitempty"`
	TopSessionAggregateHeadroom  int      `json:"top_session_aggregate_headroom_tokens,omitempty"`
	TopSessionAggregateBlockers  []string `json:"top_session_aggregate_blockers,omitempty"`
	PromotionOpenStage           string   `json:"promotion_open_stage"`
	ContinuationCandidate        string   `json:"continuation_candidate"`
	EconomicsVerdict             string   `json:"economics_verdict"`
	RecommendedAction            string   `json:"recommended_action,omitempty"`
}

type wssT354Turn struct {
	shape                         string
	previousResponseID            bool
	toolOutputs                   int
	customToolOutputs             int
	requestTokensEstimate         int
	capturedOriginalRequestTokens int
	capturedLocalSavedTokens      int
	metadataFootprint             wssT354MetadataFootprint
	capturedOriginalMetadata      wssT354MetadataFootprint
	metadataComparisonAvailable   bool
	metadataConsistent            bool
	serverOutputItems             int
	serverOutputItemIDs           int
	sequence                      int64
	socketSeq                     uint64
	mutated                       bool
	capturedOriginalShadow        bool
	terminal                      bool
	errorFrames                   int
	http400Errors                 int
	invalidRequests               int
	responseFailures              int
}

const wssT354ShapeProofHelpText = `wss-t354-shape-proof: classify WSS T354 downstream-state proof readiness

Usage:
  go run ./scripts/utils wss-t354-shape-proof <frames.jsonl-or-dir> [--json] [--socket-seq=N] [--t420-handoff-json=debug-wss-sockets.json] [--t408-open-slice-json=wss-audit.json] [--require-recovery-contract]

The report is content-free. It reads WSS frame captures and emits only request
shape, mutation, downstream-turn, 400/invalid_request, and lost-comprehension
counters. A passing report proves the capture contains at least one mutated
delta/full-history tool-output candidate whose current turn and following turn
are both clean. It does not by itself enable any runtime guard.`

func runWSST354ShapeProof(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSST354ShapeProofFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssT354ShapeProofHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-t354-shape-proof <frames.jsonl-or-dir> [--json]")
		return 2
	}
	report, err := loadWSST354ShapeProofReport(flags)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if flags.outputFormat == outputJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		if !report.GatePassed {
			return 3
		}
		return 0
	}
	writeWSST354ShapeProofText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseWSST354ShapeProofFlags(args []string) (wssT354ShapeProofFlags, error) {
	flags := wssT354ShapeProofFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--require-recovery-contract":
			flags.requireRecoveryContract = true
		case arg == "--t420-handoff-json":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("--t420-handoff-json requires a value")
			}
			i++
			flags.t420HandoffPath = strings.TrimSpace(args[i])
			if flags.t420HandoffPath == "" {
				return flags, fmt.Errorf("--t420-handoff-json requires a value")
			}
		case strings.HasPrefix(arg, "--t420-handoff-json="):
			flags.t420HandoffPath = strings.TrimSpace(strings.TrimPrefix(arg, "--t420-handoff-json="))
			if flags.t420HandoffPath == "" {
				return flags, fmt.Errorf("--t420-handoff-json requires a value")
			}
		case arg == "--t408-open-slice-json":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("--t408-open-slice-json requires a value")
			}
			i++
			flags.t408OpenSlicePath = strings.TrimSpace(args[i])
			if flags.t408OpenSlicePath == "" {
				return flags, fmt.Errorf("--t408-open-slice-json requires a value")
			}
		case strings.HasPrefix(arg, "--t408-open-slice-json="):
			flags.t408OpenSlicePath = strings.TrimSpace(strings.TrimPrefix(arg, "--t408-open-slice-json="))
			if flags.t408OpenSlicePath == "" {
				return flags, fmt.Errorf("--t408-open-slice-json requires a value")
			}
		case arg == "--socket-seq":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("--socket-seq requires a value")
			}
			i++
			n, err := parseSocketSeqFlag("--socket-seq", args[i])
			if err != nil {
				return flags, err
			}
			flags.socketSeq = n
		case strings.HasPrefix(arg, "--socket-seq="):
			n, err := parseSocketSeqFlag("--socket-seq", strings.TrimPrefix(arg, "--socket-seq="))
			if err != nil {
				return flags, err
			}
			flags.socketSeq = n
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple proof paths provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func loadWSST354ShapeProofReport(flags wssT354ShapeProofFlags) (wssT354ShapeProofReport, error) {
	files, singleFile, err := wssSavingsBaselineFiles(flags.path)
	if err != nil {
		return wssT354ShapeProofReport{}, err
	}
	restoreLogger := silenceWSSSavingsBaselineReplayLogs()
	defer restoreLogger()
	report := wssT354ShapeProofReport{Path: flags.path, T420HandoffPath: flags.t420HandoffPath, T408OpenSlicePath: flags.t408OpenSlicePath, SocketSeq: flags.socketSeq, GatePassed: true}
	for _, path := range files {
		row, err := loadWSST354ShapeProofRow(path, flags.socketSeq)
		if err != nil {
			if singleFile {
				return wssT354ShapeProofReport{}, err
			}
			report.Skips = append(report.Skips, wssT354ShapeProofSkip{Path: path, Reason: err.Error()})
			report.SkippedFiles++
			continue
		}
		report.Rows = append(report.Rows, row)
		report.FrameFiles++
		applyWSST354ShapeProofRow(&report.Totals, row)
		if !row.GatePassed {
			report.GatePassed = false
			report.GateFailures = append(report.GateFailures, fmt.Sprintf("%s: %s", path, strings.Join(row.GateFailures, "; ")))
		}
	}
	if report.FrameFiles == 0 {
		return wssT354ShapeProofReport{}, fmt.Errorf("no WSS replay frame files found under %s", flags.path)
	}
	if report.Totals.Lost > 0 {
		report.GatePassed = false
		report.GateFailures = append(report.GateFailures, fmt.Sprintf("lost=%d > 0", report.Totals.Lost))
	}
	if flags.t420HandoffPath != "" {
		handoff, err := loadWSST354T420ReconnectHandoff(flags.t420HandoffPath)
		if err != nil {
			return wssT354ShapeProofReport{}, err
		}
		report.T420ReconnectHandoff = handoff
		applyWSST354T420ReconnectHandoff(&report.Totals, handoff)
	}
	if flags.t408OpenSlicePath != "" {
		openSlices, err := loadWSST354T408OpenSlices(flags.t408OpenSlicePath)
		if err != nil {
			return wssT354ShapeProofReport{}, err
		}
		report.T408OpenSlices = openSlices
		applyWSST354T408OpenSlices(&report.Totals, openSlices)
		if len(openSlices) == 0 {
			report.GatePassed = false
			report.GateFailures = append(report.GateFailures, "t408_open_slice_candidates=0")
		}
	}
	if flags.requireRecoveryContract {
		report.T419RecoveryContract = wssT354RecoveryContractGateFromMatrix(buildRecoveryContractMatrixReport())
		if !report.T419RecoveryContract.GatePassed {
			report.GatePassed = false
			for _, failure := range report.T419RecoveryContract.GateFailures {
				report.GateFailures = append(report.GateFailures, "t419_recovery_contract: "+failure)
			}
		}
	}
	report.GateFailures = compactStringList(report.GateFailures)
	report.Findings = wssT354ShapeProofFindings(report)
	return report, nil
}

func wssT354RecoveryContractGateFromMatrix(matrix recoveryContractMatrixReport) *wssT354RecoveryContractGate {
	gate := &wssT354RecoveryContractGate{
		Rows:                    matrix.Summary.Rows,
		ProductReady:            matrix.Summary.ProductReady,
		ProductGaps:             matrix.Summary.ProductGaps,
		BlockedRows:             matrix.Summary.BlockedRows,
		ArchiveBackedRows:       matrix.Summary.ArchiveBackedRows,
		RehydrateBeforeUpstream: matrix.Summary.RehydrateBeforeRows,
		GatePassed:              true,
	}
	for _, row := range matrix.Rows {
		if row.ID != "t417_class_b_server_state_recovery_gate" {
			continue
		}
		gate.T417ServerStateRowReady = row.ArchiveExact && row.MechanicalRecovery && row.NegativeAccounting && row.FailOpen
		gate.T417ServerStateRowBlockers = recoveryContractBlockersExcept(row.Blockers, "not_default_eligible")
		break
	}
	if matrix.Summary.ProductGaps > 0 {
		gate.GatePassed = false
		gate.GateFailures = append(gate.GateFailures, fmt.Sprintf("product_gaps=%d > 0", matrix.Summary.ProductGaps))
	}
	if !gate.T417ServerStateRowReady {
		gate.GatePassed = false
		gate.GateFailures = append(gate.GateFailures, "t417_server_state_recovery_contract_missing")
	}
	if len(gate.T417ServerStateRowBlockers) > 0 {
		gate.GatePassed = false
		gate.GateFailures = append(gate.GateFailures, "t417_server_state_recovery_blockers="+strings.Join(gate.T417ServerStateRowBlockers, ","))
	}
	return gate
}

func recoveryContractBlockersExcept(blockers []string, ignored ...string) []string {
	if len(blockers) == 0 {
		return nil
	}
	ignore := make(map[string]struct{}, len(ignored))
	for _, blocker := range ignored {
		ignore[blocker] = struct{}{}
	}
	out := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		if _, ok := ignore[blocker]; ok {
			continue
		}
		out = append(out, blocker)
	}
	return out
}

func loadWSST354ShapeProofRow(path string, socketSeq uint64) (wssT354ShapeProofRow, error) {
	frames, err := readWSSABReplayFrames(path)
	if err != nil {
		return wssT354ShapeProofRow{}, err
	}
	frames = filterWSSABReplayFramesBySocketSeq(frames, socketSeq)
	if len(frames) == 0 {
		if socketSeq > 0 {
			return wssT354ShapeProofRow{}, fmt.Errorf("no replay frames for socket_seq=%d in %s", socketSeq, path)
		}
		return wssT354ShapeProofRow{}, fmt.Errorf("replay %s contained no frames", path)
	}
	replay, err := loadWSSABReplayReport(wssABReplayFlags{path: path, socketSeq: socketSeq})
	if err != nil {
		return wssT354ShapeProofRow{}, err
	}
	upstream := wssABReplayUpstreamDiagnostics(frames)
	turnFrames := wssT354CanonicalTurnFrames(frames)
	turns := wssT354TurnsFromFrames(turnFrames)
	row := wssT354ShapeProofRow{
		Path:                   path,
		SocketSeq:              socketSeq,
		Frames:                 len(frames),
		RequestTurns:           len(turns),
		ReplayLocalSavedTokens: replay.ReducerTokensSaved,
		ProviderUsage:          wssT354ProviderUsageFromFrames(frames),
		Upstream: wssT354UpstreamProof{
			ErrorFrames:          upstream.ErrorFrames,
			HTTP400Errors:        upstream.HTTP400Errors,
			InvalidRequestErrors: upstream.InvalidRequestErrors,
			ResponseFailedFrames: upstream.ResponseFailedFrames,
		},
		Lost:       replay.Lost,
		GatePassed: true,
	}
	for _, turn := range turns {
		addWSST354ShapeCount(&row.RequestShapes, turn.shape)
	}
	for i, turn := range turns {
		if !wssT354CandidateTurn(turn) {
			continue
		}
		candidate := wssT354CandidateProof{
			TurnIndex:                     i,
			Shape:                         turn.shape,
			PreviousResponseID:            turn.previousResponseID,
			ToolOutputs:                   turn.toolOutputs,
			CustomToolOutputs:             turn.customToolOutputs,
			RequestTokensEstimate:         turn.requestTokensEstimate,
			CapturedOriginalRequestTokens: turn.capturedOriginalRequestTokens,
			CapturedLocalSavedTokens:      turn.capturedLocalSavedTokens,
			CurrentTurnClean:              wssT354TurnClean(turn),
			CurrentTurnHealth:             wssT354TurnHealthFromTurn(turn),
			CurrentTurnServerOutputItems:  turn.serverOutputItems,
			CurrentTurnServerOutputIDs:    turn.serverOutputItemIDs,
		}
		if turn.metadataComparisonAvailable {
			candidate.MetadataConsistency = "preserved"
			if !turn.metadataConsistent {
				candidate.MetadataConsistency = "mismatch"
			}
			candidate.OriginalMetadataFootprint = turn.capturedOriginalMetadata.publicCopy()
			candidate.MutatedMetadataFootprint = turn.metadataFootprint.publicCopy()
		}
		following, hasFollowing := wssT354NextLogicalTurn(turns, i)
		if hasFollowing {
			followingHealth := wssT354TurnHealthFromTurn(following)
			candidate.FollowingTurnPresent = true
			candidate.FollowingTurnShape = following.shape
			candidate.FollowingRequestTokensEstimate = following.requestTokensEstimate
			candidate.FollowingTurnClean = wssT354TurnClean(following)
			candidate.FollowingTurnHealth = &followingHealth
		}
		candidate.RetryOrResendExtraTokens = wssT354RetryOrResendExtraTokens(turn, following, hasFollowing)
		candidate.BlockReasons = wssT354CandidateBlockReasons(candidate)
		candidate.UnlockProofPassing = len(candidate.BlockReasons) == 0
		finalizeWSST354CandidateEconomics(&candidate)
		row.CapturedLocalSavedTokens += candidate.CapturedLocalSavedTokens
		row.RetryOrResendExtraTokens += candidate.RetryOrResendExtraTokens
		if candidate.MetadataConsistency != "" {
			row.MetadataComparisons++
			if candidate.MetadataConsistency == "mismatch" {
				row.MetadataMismatches++
			}
		}
		if candidate.CurrentTurnServerOutputItems > 0 {
			row.CandidatesWithServerOutputItem++
		}
		if candidate.CurrentTurnServerOutputIDs > 0 {
			row.CandidatesWithServerOutputID++
		}
		if candidate.PromotionEligible {
			row.NetPositiveCandidates++
			if candidate.Shape == "full_history" {
				row.NetPositiveFullHistory++
			}
			row.NetPositiveCapturedSavedTokens += candidate.CapturedLocalSavedTokens
			row.NetPositiveRetryExtraTokens += candidate.RetryOrResendExtraTokens
			row.NetPositiveNetSavedTokens += candidate.NetCapturedLocalSavedTokens
			row.TopNetCandidate = wssT354BetterTopCandidate(row.TopNetCandidate, wssT354CandidateTopSummary(path, candidate))
		}
		row.Candidates = append(row.Candidates, candidate)
	}
	row.NetCapturedLocalSavedTokens = row.CapturedLocalSavedTokens - row.RetryOrResendExtraTokens
	row.GateFailures = wssT354RowGateFailures(row)
	row.GatePassed = len(row.GateFailures) == 0
	return row, nil
}

func loadWSST354T420ReconnectHandoff(path string) ([]wssT354T420ReconnectHandoff, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read T420 handoff JSON %s: %w", path, err)
	}
	var wrapped struct {
		T417ReconnectHandoff []wssT354T420ReconnectHandoff `json:"t417_reconnect_handoff"`
		T420ReconnectHandoff []wssT354T420ReconnectHandoff `json:"t420_reconnect_handoff"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil {
		var root map[string]json.RawMessage
		if err := json.Unmarshal(data, &root); err == nil {
			_, hasT417 := root["t417_reconnect_handoff"]
			_, hasT420 := root["t420_reconnect_handoff"]
			if hasT417 || hasT420 {
				rows := append([]wssT354T420ReconnectHandoff(nil), wrapped.T417ReconnectHandoff...)
				rows = append(rows, wrapped.T420ReconnectHandoff...)
				return finalizeWSST354T420ReconnectHandoff(rows), nil
			}
			if _, looksLikeSocketReport := root["sockets"]; looksLikeSocketReport {
				reconnectRequests := rawJSONInt(root["reconnect_full_history_requests"])
				if reconnectRequests > 0 {
					return nil, fmt.Errorf("T420 handoff JSON %s has reconnect_full_history_requests=%d but no t417_reconnect_handoff rows", path, reconnectRequests)
				}
				return nil, nil
			}
		}
	}
	var rows []wssT354T420ReconnectHandoff
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("decode T420 handoff JSON %s: %w", path, err)
	}
	return finalizeWSST354T420ReconnectHandoff(rows), nil
}

func finalizeWSST354T420ReconnectHandoff(rows []wssT354T420ReconnectHandoff) []wssT354T420ReconnectHandoff {
	out := make([]wssT354T420ReconnectHandoff, 0, len(rows))
	for _, row := range rows {
		row.SocketKey = strings.TrimSpace(row.SocketKey)
		row.Cause = strings.TrimSpace(row.Cause)
		row.ContinuationCandidate = strings.TrimSpace(row.ContinuationCandidate)
		if row.SocketKey == "" || row.Cause == "" {
			continue
		}
		row.RequestShapes = cloneIntMap(row.RequestShapes)
		if row.CandidatePotentialLocalTokens == 0 && row.ReconnectInputTokens > 0 {
			row.CandidatePotentialLocalTokens = row.ReconnectInputTokens
		}
		row.EconomicsVerdict = wssT354T420ReconnectEconomicsVerdict(row)
		out = append(out, row)
	}
	return out
}

func wssT354T420ReconnectEconomicsVerdict(row wssT354T420ReconnectHandoff) string {
	if row.ReconnectInputTokens <= 0 || row.FullHistoryRequests <= 0 {
		return "not_actionable"
	}
	switch row.ContinuationCandidate {
	case "t417_stateless_or_lineage_reroute":
		return "t417_reconnect_reroute_input"
	case "t420_local_lifecycle_fix", "t420_upstream_keepalive_or_recovery":
		return "t420_transport_fix_input"
	case "classify_before_reroute", "":
		return "classify_before_reroute"
	default:
		if strings.HasPrefix(row.ContinuationCandidate, "t417_") {
			return "t417_reconnect_reroute_input"
		}
		if strings.HasPrefix(row.ContinuationCandidate, "t420_") {
			return "t420_transport_fix_input"
		}
		return "classify_before_reroute"
	}
}

func applyWSST354T420ReconnectHandoff(total *wssT354ShapeProofTotal, rows []wssT354T420ReconnectHandoff) {
	for _, row := range rows {
		total.T420ReconnectHandoffRows++
		total.T420ReconnectInputTokens += row.ReconnectInputTokens
		total.T420RetryResendCostTokens += row.RetryResendCostTokens
		total.T420ProviderInputTokens += row.ProviderInputTokens
		total.T420ProviderCachedTokens += row.ProviderCachedTokens
		total.T420LocalSavedTokens += row.LocalSavedTokens
		switch row.EconomicsVerdict {
		case "t417_reconnect_reroute_input":
			total.T417ReconnectRerouteCandidates++
		case "t420_transport_fix_input":
			total.T420TransportFixCandidates++
		}
	}
}

func loadWSST354T408OpenSlices(path string) ([]wssT354T408OpenSlice, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read T408 open-slice JSON %s: %w", path, err)
	}
	var report wssAuditReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode T408 open-slice JSON %s: %w", path, err)
	}
	rows := make([]wssT354T408OpenSlice, 0, len(report.ShadowMirrorCandidates))
	for _, candidate := range report.ShadowMirrorCandidates {
		row, ok := wssT354T408OpenSliceFromCandidate(candidate)
		if ok {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].OpenHeadroomTokens != rows[j].OpenHeadroomTokens {
			return rows[i].OpenHeadroomTokens > rows[j].OpenHeadroomTokens
		}
		if rows[i].OpenCandidateTokens != rows[j].OpenCandidateTokens {
			return rows[i].OpenCandidateTokens > rows[j].OpenCandidateTokens
		}
		if rows[i].RequestShape != rows[j].RequestShape {
			return rows[i].RequestShape < rows[j].RequestShape
		}
		return rows[i].Kind < rows[j].Kind
	})
	return rows, nil
}

func wssT354T408OpenSliceFromCandidate(candidate wssShadowMirrorCandidate) (wssT354T408OpenSlice, bool) {
	if candidate.CandidateLane != "t417_class_b_server_state" ||
		!candidate.PromotionOpenReady ||
		candidate.PromotionOpenHeadroom <= 0 ||
		len(candidate.PromotionOpenBlockers) > 0 {
		return wssT354T408OpenSlice{}, false
	}
	row := wssT354T408OpenSlice{
		RequestShape:            strings.TrimSpace(candidate.RequestShape),
		Kind:                    strings.TrimSpace(candidate.Kind),
		OpenRequests:            candidate.PromotionOpenRequests,
		OpenCandidateTokens:     candidate.PromotionOpenCandidateTokens,
		OpenLocalSavedTokens:    candidate.PromotionOpenLocalSavedTokens,
		OpenHeadroomTokens:      candidate.PromotionOpenHeadroom,
		ProviderInputTokens:     candidate.ProviderInputTokens,
		ProviderCachedTokens:    candidate.ProviderCachedTokens,
		AggregateHeadroomTokens: candidate.IncrementalLocalTokensHeadroom,
		AggregateBlockers:       compactStringList(candidate.PromotionBlockers),
		PromotionOpenStage:      strings.TrimSpace(candidate.PromotionOpenStage),
		ContinuationCandidate:   "t417_exact_scope_open_slice",
		EconomicsVerdict:        "t417_open_slice_net_positive",
		RecommendedAction:       "promote only exact open-scope rows; keep aggregate blockers guarded",
	}
	for _, session := range candidate.TopSessions {
		if !session.PromotionOpenReady || session.PromotionOpenHeadroom <= 0 || len(session.PromotionOpenBlockers) > 0 {
			continue
		}
		row.TopSessionID = strings.TrimSpace(session.SessionID)
		row.TopSessionOpenRequests = session.PromotionOpenRequests
		row.TopSessionOpenHeadroomTokens = session.PromotionOpenHeadroom
		row.TopSessionAggregateHeadroom = session.IncrementalLocalTokensHeadroom
		row.TopSessionAggregateBlockers = compactStringList(session.PromotionBlockers)
		break
	}
	if row.RequestShape == "" || row.Kind == "" || row.OpenRequests <= 0 || row.OpenCandidateTokens <= 0 {
		return wssT354T408OpenSlice{}, false
	}
	return row, true
}

func applyWSST354T408OpenSlices(total *wssT354ShapeProofTotal, rows []wssT354T408OpenSlice) {
	for _, row := range rows {
		total.T408OpenSliceRows++
		total.T408OpenSliceRequests += row.OpenRequests
		total.T408OpenSliceCandidateTokens += row.OpenCandidateTokens
		total.T408OpenSliceLocalSavedTokens += row.OpenLocalSavedTokens
		total.T408OpenSliceHeadroomTokens += row.OpenHeadroomTokens
		total.TopT408OpenSlice = wssT354BetterT408OpenSlice(total.TopT408OpenSlice, row)
	}
}

func wssT354BetterT408OpenSlice(current *wssT354T408OpenSlice, candidate wssT354T408OpenSlice) *wssT354T408OpenSlice {
	if candidate.OpenHeadroomTokens <= 0 {
		return current
	}
	if current == nil || candidate.OpenHeadroomTokens > current.OpenHeadroomTokens ||
		(candidate.OpenHeadroomTokens == current.OpenHeadroomTokens && candidate.OpenCandidateTokens > current.OpenCandidateTokens) {
		copy := candidate
		copy.AggregateBlockers = append([]string(nil), candidate.AggregateBlockers...)
		copy.TopSessionAggregateBlockers = append([]string(nil), candidate.TopSessionAggregateBlockers...)
		return &copy
	}
	return current
}

func cloneIntMap(values map[string]int) map[string]int {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]int, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func wssT354TurnsFromFrames(frames []proxy.WSSABReplayFrame) []wssT354Turn {
	var turns []wssT354Turn
	current := -1
	var lastOriginal *wssT354Turn
	lastOriginalIndex := -1
	for _, frame := range frames {
		if frame.Direction == wsmitm.DirClientToServer {
			body, root, ok := wssT354RequestBody(frame.Payload)
			if !ok || !wssT354LooksLikeRequestBody(root) {
				lastOriginal = nil
				lastOriginalIndex = -1
				continue
			}
			info := wssT354RequestInfo(root)
			info.metadataFootprint = wssT354MetadataFootprintFromRoot(root)
			info.requestTokensEstimate = tokens.Estimate(len(body))
			info.sequence = frame.Sequence
			info.socketSeq = frame.SocketSeq
			turns = append(turns, info)
			current = len(turns) - 1
			if frame.Mutated {
				turns[current].mutated = true
				if wssT354SameCapturedSequence(lastOriginal, info) {
					turns[current].capturedOriginalRequestTokens = lastOriginal.requestTokensEstimate
					turns[current].capturedLocalSavedTokens = positiveDelta(lastOriginal.requestTokensEstimate, info.requestTokensEstimate)
					if lastOriginal.shape == info.shape && lastOriginal.previousResponseID == info.previousResponseID {
						turns[current].capturedOriginalMetadata = lastOriginal.metadataFootprint
						turns[current].metadataComparisonAvailable = true
						turns[current].metadataConsistent = lastOriginal.metadataFootprint.fingerprint == info.metadataFootprint.fingerprint
					}
					if lastOriginalIndex >= 0 && lastOriginalIndex < current {
						turns[lastOriginalIndex].capturedOriginalShadow = true
					}
				}
				lastOriginal = nil
				lastOriginalIndex = -1
			} else {
				copyInfo := info
				lastOriginal = &copyInfo
				lastOriginalIndex = current
			}
			continue
		}
		if current < 0 || frame.Direction != wsmitm.DirServerToClient {
			continue
		}
		lastOriginal = nil
		lastOriginalIndex = -1
		env, err := wsmitm.Parse(frame.Payload)
		if err != nil {
			continue
		}
		switch env.Kind {
		case wsmitm.FrameKindResponseOutputItemAdded, wsmitm.FrameKindResponseOutputItemDone:
			turns[current].serverOutputItems++
			turns[current].serverOutputItemIDs += wssT354ServerOutputItemIDFields(env)
		case wsmitm.FrameKindResponseCompleted:
			turns[current].terminal = true
		case wsmitm.FrameKindError:
			turns[current].terminal = true
			turns[current].errorFrames++
			status, errorType := wssABReplayErrorStatusAndType(frame.Payload)
			if status == "400" {
				turns[current].http400Errors++
			}
			if errorType == "invalid_request_error" {
				turns[current].invalidRequests++
			}
		case wsmitm.FrameKindResponseFailed, wsmitm.FrameKindResponseIncomplete:
			turns[current].terminal = true
			turns[current].errorFrames++
			if env.Kind == wsmitm.FrameKindResponseFailed {
				turns[current].responseFailures++
			}
			status, errorType := wssABReplayErrorStatusAndType(frame.Payload)
			if status == "400" {
				turns[current].http400Errors++
			}
			if errorType == "invalid_request_error" {
				turns[current].invalidRequests++
			}
		}
	}
	return turns
}

func wssT354CanonicalTurnFrames(frames []proxy.WSSABReplayFrame) []proxy.WSSABReplayFrame {
	if len(frames) < 2 {
		return frames
	}
	out := make([]proxy.WSSABReplayFrame, 0, len(frames))
	for _, frame := range frames {
		if len(out) > 0 && wssT354DuplicateAdjacentRequestFrame(out[len(out)-1], frame) {
			continue
		}
		out = append(out, frame)
	}
	return out
}

func wssT354DuplicateAdjacentRequestFrame(previous, current proxy.WSSABReplayFrame) bool {
	if previous.Direction != wsmitm.DirClientToServer ||
		current.Direction != wsmitm.DirClientToServer ||
		previous.Mutated != current.Mutated ||
		previous.SocketSeq != current.SocketSeq ||
		previous.Sequence != current.Sequence ||
		!bytes.Equal(previous.Payload, current.Payload) {
		return false
	}
	_, root, ok := wssT354RequestBody(current.Payload)
	return ok && wssT354LooksLikeRequestBody(root)
}

func wssT354NextLogicalTurn(turns []wssT354Turn, index int) (wssT354Turn, bool) {
	for i := index + 1; i < len(turns); i++ {
		if turns[i].capturedOriginalShadow {
			continue
		}
		return turns[i], true
	}
	return wssT354Turn{}, false
}

func wssT354RetryOrResendExtraTokens(current, following wssT354Turn, hasFollowing bool) int {
	if current.shape == "full_history" && current.capturedOriginalRequestTokens > 0 {
		return positiveDelta(current.requestTokensEstimate, current.capturedOriginalRequestTokens)
	}
	if !hasFollowing {
		return 0
	}
	if following.shape != "full_history" {
		return 0
	}
	if wssT354CandidateTurn(following) && following.capturedOriginalRequestTokens > 0 {
		return 0
	}
	return following.requestTokensEstimate
}

func wssT354SameCapturedSequence(previous *wssT354Turn, current wssT354Turn) bool {
	if previous == nil {
		return false
	}
	if previous.sequence == 0 || current.sequence == 0 {
		return previous.sequence == 0 && current.sequence == 0
	}
	return previous.sequence == current.sequence
}

func wssT354FrameObject(payload []byte) (map[string]json.RawMessage, bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, false
	}
	if body, ok := root["body"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(body, &nested); err == nil && len(nested) > 0 {
			return nested, true
		}
	}
	return root, true
}

func wssT354RequestBody(payload []byte) ([]byte, map[string]json.RawMessage, bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, nil, false
	}
	for _, key := range []string{"body", "request"} {
		raw := root[key]
		if !jsonRawObject(raw) {
			continue
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err == nil && len(nested) > 0 {
			return append([]byte(nil), raw...), nested, true
		}
	}
	if wssT354LooksLikeRequestBody(root) {
		return append([]byte(nil), payload...), root, true
	}
	return nil, nil, false
}

func jsonRawObject(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")
}

func wssT354LooksLikeRequestBody(root map[string]json.RawMessage) bool {
	if len(root["input"]) > 0 {
		return true
	}
	if len(root["previous_response_id"]) > 0 && len(root["model"]) > 0 {
		return true
	}
	return false
}

func wssT354RequestInfo(root map[string]json.RawMessage) wssT354Turn {
	previous := strings.TrimSpace(rawJSONScalarString(root["previous_response_id"])) != ""
	toolOutputs, customToolOutputs, history := wssT354InputFacts(root["input"])
	shape := "root"
	if history {
		shape = "full_history"
	} else if previous {
		shape = "delta"
	}
	return wssT354Turn{
		shape:              shape,
		previousResponseID: previous,
		toolOutputs:        toolOutputs,
		customToolOutputs:  customToolOutputs,
	}
}

func wssT354MetadataFootprintFromRoot(root map[string]json.RawMessage) wssT354MetadataFootprint {
	var footprint wssT354MetadataFootprint
	var signatureParts []string
	for key, raw := range root {
		wssT354WalkMetadataFootprint(strings.ToLower(strings.TrimSpace(key)), raw, &footprint, &signatureParts)
	}
	sort.Strings(signatureParts)
	sum := sha256.Sum256([]byte(strings.Join(signatureParts, "\n")))
	footprint.fingerprint = fmt.Sprintf("%x", sum[:])
	return footprint
}

func wssT354WalkMetadataFootprint(key string, raw json.RawMessage, footprint *wssT354MetadataFootprint, signatureParts *[]string) {
	if category := wssT354MetadataKeyCategory(key); category != "" {
		switch category {
		case "reference":
			footprint.ReferenceFields++
		case "metadata":
			footprint.MetadataFields++
		case "shape":
			footprint.ShapeFields++
		}
		*signatureParts = append(*signatureParts, "key:"+category+":"+key)
		if scalar, ok := wssT354ScalarSignature(raw); ok {
			*signatureParts = append(*signatureParts, "value:"+category+":"+key+"="+scalar)
		}
	}
	if wssT354ContentBearingKey(key) {
		return
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil && len(object) > 0 {
		for childKey, childRaw := range object {
			wssT354WalkMetadataFootprint(strings.ToLower(strings.TrimSpace(childKey)), childRaw, footprint, signatureParts)
		}
		return
	}
	var array []json.RawMessage
	if err := json.Unmarshal(raw, &array); err == nil {
		for _, childRaw := range array {
			wssT354WalkMetadataFootprint("", childRaw, footprint, signatureParts)
		}
	}
}

func wssT354ContentBearingKey(key string) bool {
	switch key {
	case "arguments", "body", "content", "delta", "output", "text":
		return true
	default:
		return false
	}
}

func wssT354MetadataKeyCategory(key string) string {
	switch key {
	case "previous_response_id", "response_id", "item_id", "call_id", "output_id", "tool_call_id", "id":
		return "reference"
	case "metadata", "client_metadata", "x-codex-turn-metadata", "conversation_id", "thread_id", "session_id", "prompt_cache_key":
		return "metadata"
	case "model", "type", "role", "name":
		return "shape"
	default:
		return ""
	}
}

func wssT354ScalarSignature(raw json.RawMessage) (string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return "s:" + typed, true
	case float64:
		return fmt.Sprintf("n:%g", typed), true
	case bool:
		if typed {
			return "b:true", true
		}
		return "b:false", true
	case nil:
		return "null", true
	default:
		return "", false
	}
}

func (f wssT354MetadataFootprint) publicCopy() *wssT354MetadataFootprint {
	out := f
	out.fingerprint = ""
	return &out
}

func wssT354ServerOutputItemIDFields(env wsmitm.Envelope) int {
	count := 0
	if strings.TrimSpace(env.ItemID) != "" {
		count++
	}
	if len(env.Item) == 0 {
		return count
	}
	var item map[string]json.RawMessage
	if err := json.Unmarshal(env.Item, &item); err != nil {
		return count
	}
	return count + wssT354CountReferenceFields(item)
}

func wssT354CountReferenceFields(root map[string]json.RawMessage) int {
	count := 0
	for key, raw := range root {
		if wssT354MetadataKeyCategory(strings.ToLower(strings.TrimSpace(key))) == "reference" {
			count++
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err == nil && len(object) > 0 {
			count += wssT354CountReferenceFields(object)
			continue
		}
		var array []json.RawMessage
		if err := json.Unmarshal(raw, &array); err == nil {
			for _, childRaw := range array {
				var child map[string]json.RawMessage
				if err := json.Unmarshal(childRaw, &child); err == nil {
					count += wssT354CountReferenceFields(child)
				}
			}
		}
	}
	return count
}

func wssT354InputFacts(raw json.RawMessage) (toolOutputs int, customToolOutputs int, history bool) {
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0, 0, false
	}
	for _, item := range items {
		itemType := rawJSONScalarString(item["type"])
		role := rawJSONScalarString(item["role"])
		if itemType == "response_item" {
			var nested map[string]json.RawMessage
			if err := json.Unmarshal(item["payload"], &nested); err == nil {
				itemType = rawJSONScalarString(nested["type"])
				role = rawJSONScalarString(nested["role"])
			}
		}
		switch itemType {
		case "function_call_output":
			toolOutputs++
		case "custom_tool_call_output":
			toolOutputs++
			customToolOutputs++
		case "function_call", "custom_tool_call", "reasoning":
			history = true
		case "message":
			if role == "assistant" {
				history = true
			}
		}
	}
	return toolOutputs, customToolOutputs, history
}

func rawJSONScalarString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func rawJSONInt(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var value int
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return int(number)
	}
	return 0
}

type wssT354OpenAIUsage struct {
	InputTokens        int `json:"input_tokens"`
	PromptTokens       int `json:"prompt_tokens"`
	OutputTokens       int `json:"output_tokens"`
	CompletionTokens   int `json:"completion_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details,omitempty"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

func wssT354ProviderUsageFromFrames(frames []proxy.WSSABReplayFrame) wssT354ProviderUsage {
	var out wssT354ProviderUsage
	for _, frame := range frames {
		if frame.Direction != wsmitm.DirServerToClient {
			continue
		}
		usage, ok := wssT354ProviderUsageFromPayload(frame.Payload)
		if !ok {
			continue
		}
		out.InputTokens += usage.inputTokens()
		out.CachedTokens += usage.cachedTokens()
		out.OutputTokens += usage.outputTokens()
		out.CompletionFrames++
	}
	if out.InputTokens > 0 {
		out.CachedPct = float64(out.CachedTokens) / float64(out.InputTokens) * 100
	}
	return out
}

func wssT354ProviderUsageFromPayload(payload []byte) (wssT354OpenAIUsage, bool) {
	var envelope struct {
		Usage    *wssT354OpenAIUsage `json:"usage,omitempty"`
		Response *struct {
			Usage *wssT354OpenAIUsage `json:"usage,omitempty"`
		} `json:"response,omitempty"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return wssT354OpenAIUsage{}, false
	}
	var merged wssT354OpenAIUsage
	found := false
	if envelope.Usage != nil {
		merged = wssT354MergeUsage(merged, *envelope.Usage)
		found = true
	}
	if envelope.Response != nil && envelope.Response.Usage != nil {
		merged = wssT354MergeUsage(merged, *envelope.Response.Usage)
		found = true
	}
	return merged, found
}

func wssT354MergeUsage(a, b wssT354OpenAIUsage) wssT354OpenAIUsage {
	if b.InputTokens > a.InputTokens {
		a.InputTokens = b.InputTokens
	}
	if b.PromptTokens > a.PromptTokens {
		a.PromptTokens = b.PromptTokens
	}
	if b.OutputTokens > a.OutputTokens {
		a.OutputTokens = b.OutputTokens
	}
	if b.CompletionTokens > a.CompletionTokens {
		a.CompletionTokens = b.CompletionTokens
	}
	if b.InputTokensDetails != nil {
		if a.InputTokensDetails == nil {
			a.InputTokensDetails = b.InputTokensDetails
		} else if b.InputTokensDetails.CachedTokens > a.InputTokensDetails.CachedTokens {
			a.InputTokensDetails.CachedTokens = b.InputTokensDetails.CachedTokens
		}
	}
	if b.PromptTokensDetails != nil {
		if a.PromptTokensDetails == nil {
			a.PromptTokensDetails = b.PromptTokensDetails
		} else if b.PromptTokensDetails.CachedTokens > a.PromptTokensDetails.CachedTokens {
			a.PromptTokensDetails.CachedTokens = b.PromptTokensDetails.CachedTokens
		}
	}
	return a
}

func (u wssT354OpenAIUsage) inputTokens() int {
	if u.InputTokens > 0 {
		return u.InputTokens
	}
	return u.PromptTokens
}

func (u wssT354OpenAIUsage) outputTokens() int {
	if u.OutputTokens > 0 {
		return u.OutputTokens
	}
	return u.CompletionTokens
}

func (u wssT354OpenAIUsage) cachedTokens() int {
	cached := 0
	if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens > cached {
		cached = u.InputTokensDetails.CachedTokens
	}
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > cached {
		cached = u.PromptTokensDetails.CachedTokens
	}
	return cached
}

func wssT354CandidateTurn(turn wssT354Turn) bool {
	if !turn.mutated || turn.toolOutputs == 0 {
		return false
	}
	return turn.shape == "delta" || turn.shape == "full_history"
}

func finalizeWSST354CandidateEconomics(candidate *wssT354CandidateProof) {
	if candidate == nil {
		return
	}
	candidate.NetCapturedLocalSavedTokens = candidate.CapturedLocalSavedTokens - candidate.RetryOrResendExtraTokens
	candidate.PromotionEligible = candidate.UnlockProofPassing && candidate.NetCapturedLocalSavedTokens > 0
	candidate.EconomicsVerdict = wssT354CandidateEconomicsVerdict(*candidate)
	candidate.ContinuationCandidate = wssT354CandidateContinuationCandidate(*candidate)
}

func wssT354CandidateEconomicsVerdict(candidate wssT354CandidateProof) string {
	switch {
	case wssT354CandidateHasSafetyFailure(candidate):
		return "unsafe"
	case !candidate.UnlockProofPassing:
		return "unproven"
	case candidate.NetCapturedLocalSavedTokens <= 0:
		return "negative_net"
	case candidate.Shape == "full_history":
		return "class_b_net_positive"
	case candidate.Shape == "delta":
		return "delta_net_positive"
	default:
		return "net_positive"
	}
}

func wssT354CandidateContinuationCandidate(candidate wssT354CandidateProof) string {
	switch candidate.EconomicsVerdict {
	case "unsafe":
		return "keep_guarded_safety_failure"
	case "unproven":
		return "collect_following_turn_or_t420_handoff"
	case "negative_net":
		return "keep_guarded_retry_resend_negative"
	}
	switch {
	case candidate.Shape == "full_history" && candidate.PreviousResponseID:
		return "lineage_scoped_class_b_candidate"
	case candidate.Shape == "full_history" && candidate.RetryOrResendExtraTokens > 0:
		return "stateless_detach_net_positive"
	case candidate.Shape == "full_history":
		return "stateful_preserved_class_b"
	case candidate.Shape == "delta" && candidate.PreviousResponseID:
		return "stateful_delta_proven_slice"
	default:
		return "rank_before_promotion"
	}
}

func wssT354CandidateTopSummary(path string, candidate wssT354CandidateProof) *wssT354TopCandidate {
	return &wssT354TopCandidate{
		Path:                        path,
		TurnIndex:                   candidate.TurnIndex,
		Shape:                       candidate.Shape,
		PreviousResponseID:          candidate.PreviousResponseID,
		FollowingTurnShape:          candidate.FollowingTurnShape,
		CapturedLocalSavedTokens:    candidate.CapturedLocalSavedTokens,
		RetryOrResendExtraTokens:    candidate.RetryOrResendExtraTokens,
		NetCapturedLocalSavedTokens: candidate.NetCapturedLocalSavedTokens,
		EconomicsVerdict:            candidate.EconomicsVerdict,
		ContinuationCandidate:       candidate.ContinuationCandidate,
		BlockReasons:                append([]string(nil), candidate.BlockReasons...),
	}
}

func wssT354BetterTopCandidate(current, candidate *wssT354TopCandidate) *wssT354TopCandidate {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.NetCapturedLocalSavedTokens > current.NetCapturedLocalSavedTokens {
		return candidate
	}
	if candidate.NetCapturedLocalSavedTokens == current.NetCapturedLocalSavedTokens &&
		candidate.Shape == "full_history" && current.Shape != "full_history" {
		return candidate
	}
	return current
}

func wssT354TurnClean(turn wssT354Turn) bool {
	return turn.terminal && turn.errorFrames == 0 && turn.http400Errors == 0 &&
		turn.invalidRequests == 0 && turn.responseFailures == 0
}

func wssT354TurnHealthFromTurn(turn wssT354Turn) wssT354TurnHealth {
	return wssT354TurnHealth{
		Terminal:             turn.terminal,
		ErrorFrames:          turn.errorFrames,
		HTTP400Errors:        turn.http400Errors,
		InvalidRequestErrors: turn.invalidRequests,
		ResponseFailedFrames: turn.responseFailures,
	}
}

func wssT354CandidateBlockReasons(candidate wssT354CandidateProof) []string {
	var out []string
	if candidate.MetadataConsistency == "mismatch" {
		out = append(out, "metadata_reference_mismatch")
	}
	if !candidate.CurrentTurnClean {
		out = append(out, wssT354TurnHealthBlockReason("current_turn", candidate.CurrentTurnHealth))
	}
	if !candidate.FollowingTurnPresent {
		out = append(out, "missing_following_turn")
	} else if !candidate.FollowingTurnClean {
		out = append(out, wssT354TurnHealthBlockReason("following_turn", *candidate.FollowingTurnHealth))
	}
	return out
}

func wssT354TurnHealthBlockReason(prefix string, health wssT354TurnHealth) string {
	switch {
	case !health.Terminal:
		return prefix + "_not_terminal"
	case health.InvalidRequestErrors > 0:
		return fmt.Sprintf("%s_invalid_request=%d", prefix, health.InvalidRequestErrors)
	case health.HTTP400Errors > 0:
		return fmt.Sprintf("%s_http_400=%d", prefix, health.HTTP400Errors)
	case health.ResponseFailedFrames > 0:
		return fmt.Sprintf("%s_response_failed=%d", prefix, health.ResponseFailedFrames)
	case health.ErrorFrames > 0:
		return fmt.Sprintf("%s_error_frames=%d", prefix, health.ErrorFrames)
	default:
		return prefix + "_not_clean"
	}
}

func wssT354RowGateFailures(row wssT354ShapeProofRow) []string {
	var failures []string
	if len(row.Candidates) == 0 {
		failures = append(failures, "no mutated delta/full-history tool-output candidate observed")
	}
	if row.Upstream.ErrorFrames > 0 {
		failures = append(failures, fmt.Sprintf("upstream_error_frames=%d", row.Upstream.ErrorFrames))
	}
	if row.Upstream.InvalidRequestErrors > 0 {
		failures = append(failures, fmt.Sprintf("invalid_request=%d", row.Upstream.InvalidRequestErrors))
	}
	if row.Upstream.HTTP400Errors > 0 {
		failures = append(failures, fmt.Sprintf("http_400=%d", row.Upstream.HTTP400Errors))
	}
	if row.Lost > 0 {
		failures = append(failures, fmt.Sprintf("lost=%d", row.Lost))
	}
	passing := 0
	for _, candidate := range row.Candidates {
		if candidate.UnlockProofPassing {
			passing++
		}
	}
	if len(row.Candidates) > 0 && passing == 0 {
		failures = append(failures, "no candidate has clean current and following turn")
	}
	for _, candidate := range row.Candidates {
		for _, reason := range candidate.BlockReasons {
			if !wssT354CandidateReasonIsSafetyFailure(candidate, reason, passing) {
				continue
			}
			failures = append(failures, fmt.Sprintf("candidate_%d:%s", candidate.TurnIndex, reason))
		}
	}
	return compactStringList(failures)
}

func wssT354CandidateReasonIsSafetyFailure(candidate wssT354CandidateProof, reason string, passingCandidates int) bool {
	if passingCandidates <= 0 {
		return true
	}
	return wssT354CandidateReasonHasSafetyFailure(candidate, reason)
}

func wssT354CandidateHasSafetyFailure(candidate wssT354CandidateProof) bool {
	for _, reason := range candidate.BlockReasons {
		if wssT354CandidateReasonHasSafetyFailure(candidate, reason) {
			return true
		}
	}
	return false
}

func wssT354CandidateReasonHasSafetyFailure(candidate wssT354CandidateProof, reason string) bool {
	if !candidate.FollowingTurnPresent && reason == "missing_following_turn" {
		return false
	}
	if reason == "metadata_reference_mismatch" {
		return true
	}
	if strings.Contains(reason, "invalid_request") ||
		strings.Contains(reason, "http_400") ||
		strings.Contains(reason, "response_failed") ||
		strings.Contains(reason, "error_frames") {
		return true
	}
	return false
}

func applyWSST354ShapeProofRow(total *wssT354ShapeProofTotal, row wssT354ShapeProofRow) {
	total.Frames += row.Frames
	total.RequestTurns += row.RequestTurns
	addReplayShapeCounts(&total.RequestShapes, row.RequestShapes)
	total.UpstreamErrorFrames += row.Upstream.ErrorFrames
	total.InvalidRequestErrors += row.Upstream.InvalidRequestErrors
	total.HTTP400Errors += row.Upstream.HTTP400Errors
	total.ResponseFailedFrames += row.Upstream.ResponseFailedFrames
	total.Lost += row.Lost
	total.ReplayLocalSavedTokens += row.ReplayLocalSavedTokens
	total.CapturedLocalSavedTokens += row.CapturedLocalSavedTokens
	total.RetryOrResendExtraTokens += row.RetryOrResendExtraTokens
	total.NetCapturedLocalSavedTokens += row.NetCapturedLocalSavedTokens
	total.ProviderUsage.add(row.ProviderUsage)
	total.MetadataComparisons += row.MetadataComparisons
	total.MetadataMismatches += row.MetadataMismatches
	total.CandidatesWithServerOutputItem += row.CandidatesWithServerOutputItem
	total.CandidatesWithServerOutputID += row.CandidatesWithServerOutputID
	total.NetPositiveCandidates += row.NetPositiveCandidates
	total.NetPositiveFullHistory += row.NetPositiveFullHistory
	total.NetPositiveCapturedSavedTokens += row.NetPositiveCapturedSavedTokens
	total.NetPositiveRetryExtraTokens += row.NetPositiveRetryExtraTokens
	total.NetPositiveNetSavedTokens += row.NetPositiveNetSavedTokens
	total.TopNetCandidate = wssT354BetterTopCandidate(total.TopNetCandidate, row.TopNetCandidate)
	for _, candidate := range row.Candidates {
		total.MutatedToolOutputCandidates++
		switch candidate.Shape {
		case "delta":
			total.MutatedDeltaCandidates++
		case "full_history":
			total.MutatedFullHistoryCandidates++
		}
		if candidate.CurrentTurnClean {
			total.CandidatesWithCleanCurrentTurn++
		}
		if candidate.FollowingTurnPresent {
			total.CandidatesWithFollowingTurn++
		} else {
			total.MissingFollowingTurnCandidates++
		}
		if candidate.FollowingTurnClean {
			total.CandidatesWithCleanFollowing++
		}
		if candidate.UnlockProofPassing {
			total.CandidatesPassing++
		} else if wssT354CandidateHasSafetyFailure(candidate) {
			total.UnsafeCandidates++
		} else {
			total.UnprovenCandidates++
		}
	}
	total.MutatedRequests += len(row.Candidates)
}

func addReplayShapeCounts(dst *replayShapeCounts, src replayShapeCounts) {
	dst.Root += src.Root
	dst.Delta += src.Delta
	dst.FullHistory += src.FullHistory
}

func addWSST354ShapeCount(counts *replayShapeCounts, shape string) {
	switch shape {
	case "root":
		counts.Root++
	case "delta":
		counts.Delta++
	case "full_history":
		counts.FullHistory++
	}
}

func wssT354ShapeProofFindings(report wssT354ShapeProofReport) []string {
	var findings []string
	if report.Totals.CandidatesPassing > 0 {
		findings = append(findings, fmt.Sprintf("t354_clean_candidate_count=%d", report.Totals.CandidatesPassing))
	}
	if report.Totals.MutatedDeltaCandidates > 0 {
		findings = append(findings, fmt.Sprintf("mutated_delta_candidates=%d", report.Totals.MutatedDeltaCandidates))
	}
	if report.Totals.MutatedFullHistoryCandidates > 0 {
		findings = append(findings, fmt.Sprintf("mutated_full_history_candidates=%d", report.Totals.MutatedFullHistoryCandidates))
	}
	if report.Totals.MissingFollowingTurnCandidates > 0 {
		findings = append(findings, fmt.Sprintf("missing_following_turn_candidates=%d", report.Totals.MissingFollowingTurnCandidates))
	}
	if report.Totals.UnprovenCandidates > 0 {
		findings = append(findings, fmt.Sprintf("unproven_candidates=%d", report.Totals.UnprovenCandidates))
	}
	if report.Totals.CapturedLocalSavedTokens > 0 {
		findings = append(findings, fmt.Sprintf("captured_local_saved_tokens_estimate=%d", report.Totals.CapturedLocalSavedTokens))
	}
	if report.Totals.RetryOrResendExtraTokens > 0 {
		findings = append(findings, fmt.Sprintf("retry_or_resend_extra_tokens_estimate=%d", report.Totals.RetryOrResendExtraTokens))
	}
	if report.Totals.ProviderUsage.CachedTokens > 0 {
		findings = append(findings, fmt.Sprintf("provider_cached_tokens=%d", report.Totals.ProviderUsage.CachedTokens))
	}
	if report.Totals.MetadataComparisons > 0 {
		findings = append(findings, fmt.Sprintf("metadata_comparisons=%d", report.Totals.MetadataComparisons))
	}
	if report.Totals.MetadataMismatches > 0 {
		findings = append(findings, fmt.Sprintf("metadata_mismatches=%d", report.Totals.MetadataMismatches))
	}
	if report.Totals.CandidatesWithServerOutputID > 0 {
		findings = append(findings, fmt.Sprintf("server_output_item_id_candidates=%d", report.Totals.CandidatesWithServerOutputID))
	}
	if report.Totals.T420ReconnectHandoffRows > 0 {
		findings = append(findings, fmt.Sprintf("t420_reconnect_handoff_rows=%d", report.Totals.T420ReconnectHandoffRows))
		findings = append(findings, fmt.Sprintf("t420_reconnect_input_tokens=%d", report.Totals.T420ReconnectInputTokens))
		findings = append(findings, fmt.Sprintf("t420_retry_resend_cost_tokens=%d", report.Totals.T420RetryResendCostTokens))
	}
	if report.Totals.T417ReconnectRerouteCandidates > 0 {
		findings = append(findings, fmt.Sprintf("t417_reconnect_reroute_candidates=%d", report.Totals.T417ReconnectRerouteCandidates))
	}
	if report.Totals.T420TransportFixCandidates > 0 {
		findings = append(findings, fmt.Sprintf("t420_transport_fix_candidates=%d", report.Totals.T420TransportFixCandidates))
	}
	if report.Totals.T408OpenSliceRows > 0 {
		findings = append(findings, fmt.Sprintf("t408_open_slice_rows=%d", report.Totals.T408OpenSliceRows))
		findings = append(findings, fmt.Sprintf("t408_open_slice_headroom_tokens=%d", report.Totals.T408OpenSliceHeadroomTokens))
	}
	if report.Totals.TopT408OpenSlice != nil {
		findings = append(findings, fmt.Sprintf("top_t408_open_slice=%s/%s open_headroom=%d candidate=%s",
			report.Totals.TopT408OpenSlice.RequestShape,
			report.Totals.TopT408OpenSlice.Kind,
			report.Totals.TopT408OpenSlice.OpenHeadroomTokens,
			report.Totals.TopT408OpenSlice.ContinuationCandidate))
	}
	if report.Totals.NetPositiveCandidates > 0 {
		findings = append(findings, fmt.Sprintf("net_positive_candidates=%d", report.Totals.NetPositiveCandidates))
	}
	if report.Totals.NetPositiveFullHistory > 0 {
		findings = append(findings, fmt.Sprintf("net_positive_full_history_candidates=%d", report.Totals.NetPositiveFullHistory))
	}
	if report.Totals.NetPositiveNetSavedTokens > 0 {
		findings = append(findings, fmt.Sprintf("net_positive_net_captured_local_saved_tokens=%d", report.Totals.NetPositiveNetSavedTokens))
	}
	if report.Totals.TopNetCandidate != nil {
		findings = append(findings, fmt.Sprintf("top_net_candidate=%s turn=%d net=%d candidate=%s",
			report.Totals.TopNetCandidate.Shape,
			report.Totals.TopNetCandidate.TurnIndex,
			report.Totals.TopNetCandidate.NetCapturedLocalSavedTokens,
			report.Totals.TopNetCandidate.ContinuationCandidate))
	}
	if report.T419RecoveryContract != nil {
		findings = append(findings, fmt.Sprintf("t419_recovery_contract_product_gaps=%d", report.T419RecoveryContract.ProductGaps))
		if report.T419RecoveryContract.T417ServerStateRowReady {
			findings = append(findings, "t419_t417_server_state_recovery_ready")
		}
	}
	if report.Totals.UpstreamErrorFrames == 0 && report.Totals.Lost == 0 {
		findings = append(findings, "upstream_and_lost_clean")
	}
	return findings
}

func writeWSST354ShapeProofText(w io.Writer, report wssT354ShapeProofReport) {
	fmt.Fprintf(w, "WSS T354 shape proof: %s\n", report.Path)
	fmt.Fprintf(w, "  frame_files:       %d\n", report.FrameFiles)
	fmt.Fprintf(w, "  skipped_files:     %d\n", report.SkippedFiles)
	fmt.Fprintf(w, "  frames:            %d\n", report.Totals.Frames)
	fmt.Fprintf(w, "  request_turns:     %d\n", report.Totals.RequestTurns)
	fmt.Fprintf(w, "  shapes:            root=%d delta=%d full_history=%d\n",
		report.Totals.RequestShapes.Root,
		report.Totals.RequestShapes.Delta,
		report.Totals.RequestShapes.FullHistory)
	fmt.Fprintf(w, "  candidates:        total=%d delta=%d full_history=%d passing=%d unsafe=%d unproven=%d missing_following=%d\n",
		report.Totals.MutatedToolOutputCandidates,
		report.Totals.MutatedDeltaCandidates,
		report.Totals.MutatedFullHistoryCandidates,
		report.Totals.CandidatesPassing,
		report.Totals.UnsafeCandidates,
		report.Totals.UnprovenCandidates,
		report.Totals.MissingFollowingTurnCandidates)
	fmt.Fprintf(w, "  upstream:          errors=%d invalid_request=%d http_400=%d response_failed=%d lost=%d\n",
		report.Totals.UpstreamErrorFrames,
		report.Totals.InvalidRequestErrors,
		report.Totals.HTTP400Errors,
		report.Totals.ResponseFailedFrames,
		report.Totals.Lost)
	fmt.Fprintf(w, "  economics:         replay_local_saved=%d captured_local_saved_est=%d retry_or_resend_extra_est=%d net_captured_local_saved_est=%d\n",
		report.Totals.ReplayLocalSavedTokens,
		report.Totals.CapturedLocalSavedTokens,
		report.Totals.RetryOrResendExtraTokens,
		report.Totals.NetCapturedLocalSavedTokens)
	fmt.Fprintf(w, "  provider_usage:    input=%d cached=%d cached_pct=%.2f%% output=%d frames=%d\n",
		report.Totals.ProviderUsage.InputTokens,
		report.Totals.ProviderUsage.CachedTokens,
		report.Totals.ProviderUsage.CachedPct,
		report.Totals.ProviderUsage.OutputTokens,
		report.Totals.ProviderUsage.CompletionFrames)
	fmt.Fprintf(w, "  metadata:          comparisons=%d mismatches=%d server_output_items=%d server_output_ids=%d\n",
		report.Totals.MetadataComparisons,
		report.Totals.MetadataMismatches,
		report.Totals.CandidatesWithServerOutputItem,
		report.Totals.CandidatesWithServerOutputID)
	fmt.Fprintf(w, "  t420_handoff:      rows=%d reconnect_input=%d retry_resend_cost=%d provider_input=%d cached=%d local_saved=%d t417_reroute=%d t420_transport=%d\n",
		report.Totals.T420ReconnectHandoffRows,
		report.Totals.T420ReconnectInputTokens,
		report.Totals.T420RetryResendCostTokens,
		report.Totals.T420ProviderInputTokens,
		report.Totals.T420ProviderCachedTokens,
		report.Totals.T420LocalSavedTokens,
		report.Totals.T417ReconnectRerouteCandidates,
		report.Totals.T420TransportFixCandidates)
	fmt.Fprintf(w, "  t408_open_slice:   rows=%d requests=%d candidate_tokens=%d local_saved=%d headroom=%d\n",
		report.Totals.T408OpenSliceRows,
		report.Totals.T408OpenSliceRequests,
		report.Totals.T408OpenSliceCandidateTokens,
		report.Totals.T408OpenSliceLocalSavedTokens,
		report.Totals.T408OpenSliceHeadroomTokens)
	if report.Totals.TopT408OpenSlice != nil {
		fmt.Fprintf(w, "  top_t408_slice:    shape=%s kind=%s open_headroom=%d top_session=%s top_session_open_headroom=%d verdict=%s candidate=%s\n",
			report.Totals.TopT408OpenSlice.RequestShape,
			report.Totals.TopT408OpenSlice.Kind,
			report.Totals.TopT408OpenSlice.OpenHeadroomTokens,
			report.Totals.TopT408OpenSlice.TopSessionID,
			report.Totals.TopT408OpenSlice.TopSessionOpenHeadroomTokens,
			report.Totals.TopT408OpenSlice.EconomicsVerdict,
			report.Totals.TopT408OpenSlice.ContinuationCandidate)
	}
	fmt.Fprintf(w, "  net_positive:      candidates=%d full_history=%d captured_saved=%d retry_extra=%d net=%d\n",
		report.Totals.NetPositiveCandidates,
		report.Totals.NetPositiveFullHistory,
		report.Totals.NetPositiveCapturedSavedTokens,
		report.Totals.NetPositiveRetryExtraTokens,
		report.Totals.NetPositiveNetSavedTokens)
	if report.Totals.TopNetCandidate != nil {
		fmt.Fprintf(w, "  top_candidate:     shape=%s turn=%d net=%d verdict=%s candidate=%s\n",
			report.Totals.TopNetCandidate.Shape,
			report.Totals.TopNetCandidate.TurnIndex,
			report.Totals.TopNetCandidate.NetCapturedLocalSavedTokens,
			report.Totals.TopNetCandidate.EconomicsVerdict,
			report.Totals.TopNetCandidate.ContinuationCandidate)
	}
	if report.T419RecoveryContract != nil {
		fmt.Fprintf(w, "  t419_contract:     rows=%d product_ready=%d product_gaps=%d blocked=%d archive_backed=%d rehydrate_before=%d t417_ready=%v gate=%s\n",
			report.T419RecoveryContract.Rows,
			report.T419RecoveryContract.ProductReady,
			report.T419RecoveryContract.ProductGaps,
			report.T419RecoveryContract.BlockedRows,
			report.T419RecoveryContract.ArchiveBackedRows,
			report.T419RecoveryContract.RehydrateBeforeUpstream,
			report.T419RecoveryContract.T417ServerStateRowReady,
			passFail(report.T419RecoveryContract.GatePassed))
	}
	for _, row := range report.T420ReconnectHandoff {
		fmt.Fprintf(w, "  t420_handoff_row:  socket=%s cause=%s reconnect_input=%d retry_resend_cost=%d cached=%d local_saved=%d verdict=%s candidate=%s\n",
			row.SocketKey,
			row.Cause,
			row.ReconnectInputTokens,
			row.RetryResendCostTokens,
			row.ProviderCachedTokens,
			row.LocalSavedTokens,
			row.EconomicsVerdict,
			row.ContinuationCandidate)
	}
	for _, row := range report.T408OpenSlices {
		fmt.Fprintf(w, "  t408_open_row:     shape=%s kind=%s requests=%d candidate_tokens=%d local_saved=%d headroom=%d top_session=%s top_session_headroom=%d blockers=%s verdict=%s candidate=%s\n",
			row.RequestShape,
			row.Kind,
			row.OpenRequests,
			row.OpenCandidateTokens,
			row.OpenLocalSavedTokens,
			row.OpenHeadroomTokens,
			row.TopSessionID,
			row.TopSessionOpenHeadroomTokens,
			strings.Join(row.AggregateBlockers, "|"),
			row.EconomicsVerdict,
			row.ContinuationCandidate)
	}
	if len(report.Findings) > 0 {
		fmt.Fprintln(w, "  findings:")
		for _, finding := range report.Findings {
			fmt.Fprintf(w, "    - %s\n", finding)
		}
	}
	fmt.Fprintf(w, "  gate:             %s\n", passFail(report.GatePassed))
	if len(report.GateFailures) > 0 {
		fmt.Fprintln(w, "  gate_failures:")
		for _, failure := range report.GateFailures {
			fmt.Fprintf(w, "    - %s\n", failure)
		}
	}
}

func (u *wssT354ProviderUsage) add(other wssT354ProviderUsage) {
	u.InputTokens += other.InputTokens
	u.CachedTokens += other.CachedTokens
	u.OutputTokens += other.OutputTokens
	u.CompletionFrames += other.CompletionFrames
	if u.InputTokens > 0 {
		u.CachedPct = float64(u.CachedTokens) / float64(u.InputTokens) * 100
	}
}

func compactStringList(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

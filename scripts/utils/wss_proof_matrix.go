package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type wssProofMatrixFlags struct {
	path                                string
	outputFormat                        string
	requireLiveTokenDelta               bool
	requiredWorkloads                   []string
	expectedReducers                    []string
	searchCapCandidates                 []searchCapProfileCandidate
	searchCapMinCandidateRetainedPct    float64
	searchCapMinSearchOutputs           int
	searchCapMinExtraReducerTokens      int
	searchCapMinExtraReducerTokensIsSet bool
	minCaptures                         int
	minCLI                              int
	minDesktop                          int
	minPositive                         int
	help                                bool
}

type wssProofMatrixOptions struct {
	requireLiveTokenDelta               bool
	requiredWorkloads                   []string
	expectedReducers                    []string
	searchCapCandidates                 []searchCapProfileCandidate
	searchCapMinCandidateRetainedPct    float64
	searchCapMinSearchOutputs           int
	searchCapMinExtraReducerTokens      int
	searchCapMinExtraReducerTokensIsSet bool
	minCaptures                         int
	minCLI                              int
	minDesktop                          int
	minPositive                         int
}

type wssProofMatrixRequirements struct {
	requiredWorkloads      []string
	expectedReducers       []string
	searchCapProofRequired bool
	minCaptures            int
	minCLI                 int
	minDesktop             int
	minPositive            int
}

type wssProofMatrixCapture struct {
	ID                  string                 `json:"id"`
	Client              string                 `json:"client"`
	WorkloadClass       string                 `json:"workload_class"`
	FramesPath          string                 `json:"frames_path"`
	DecisionsPath       string                 `json:"decisions_path,omitempty"`
	CodexVersion        string                 `json:"codex_version,omitempty"`
	SlimferenceCommit   string                 `json:"slimference_commit,omitempty"`
	Repo                string                 `json:"repo,omitempty"`
	Model               string                 `json:"model,omitempty"`
	SocketSeq           uint64                 `json:"socket_seq,omitempty"`
	ABPairID            string                 `json:"ab_pair_id,omitempty"`
	ABVariant           string                 `json:"ab_variant,omitempty"`
	StartedAt           string                 `json:"started_at,omitempty"`
	EndedAt             string                 `json:"ended_at,omitempty"`
	ExpectedReducers    []string               `json:"expected_reducers,omitempty"`
	ExpectedZeroSavings bool                   `json:"expected_zero_savings,omitempty"`
	MinFunctionCalls    int                    `json:"min_function_calls,omitempty"`
	MinFunctionOutputs  int                    `json:"min_function_call_outputs,omitempty"`
	MinFullHistory      int                    `json:"min_full_history_requests,omitempty"`
	ExpectedReducerHits map[string]int64       `json:"expected_reducer_hits,omitempty"`
	LiveDelta           *codexCaptureLiveDelta `json:"live_delta,omitempty"`
	Replay              wssABReplayReport      `json:"replay"`
	SearchCapProof      *searchCapProofReport  `json:"search_cap_proof,omitempty"`
	Audit               *wssAuditReport        `json:"audit,omitempty"`
	GatePassed          bool                   `json:"gate_passed"`
	GateFailures        []string               `json:"gate_failures,omitempty"`
}

type wssProofMatrixReport struct {
	Path                      string                  `json:"path"`
	Captures                  int                     `json:"captures"`
	CLI                       int                     `json:"cli"`
	Desktop                   int                     `json:"desktop"`
	PositiveSavings           int                     `json:"positive_savings_captures"`
	PositiveTokenSavings      int                     `json:"positive_token_savings_captures"`
	PositiveReplayByteSavings int                     `json:"positive_replay_byte_savings_captures"`
	ExpectedZero              int                     `json:"expected_zero_captures"`
	WorkloadClasses           map[string]int          `json:"workload_classes"`
	MissingWorkloads          []string                `json:"missing_workloads,omitempty"`
	RequiredReducerHits       map[string]int64        `json:"required_reducer_hits,omitempty"`
	MissingRequiredReducers   []string                `json:"missing_required_reducers,omitempty"`
	CapturesWithIssues        int                     `json:"captures_with_issues"`
	GatePassed                bool                    `json:"gate_passed"`
	GateFailures              []string                `json:"gate_failures,omitempty"`
	CaptureReports            []wssProofMatrixCapture `json:"capture_reports"`
}

type wssProofMatrixRecord struct {
	ID                  string                 `json:"id"`
	Client              string                 `json:"client"`
	WorkloadClass       string                 `json:"workload_class"`
	FramesPath          string                 `json:"frames_path"`
	DecisionsPath       string                 `json:"decisions_path"`
	CodexVersion        string                 `json:"codex_version"`
	SlimferenceCommit   string                 `json:"slimference_commit"`
	Repo                string                 `json:"repo"`
	Model               string                 `json:"model"`
	SocketSeq           uint64                 `json:"socket_seq,omitempty"`
	ABPairID            string                 `json:"ab_pair_id,omitempty"`
	ABVariant           string                 `json:"ab_variant,omitempty"`
	StartedAt           string                 `json:"started_at"`
	EndedAt             string                 `json:"ended_at"`
	ExpectedReducers    []string               `json:"expected_reducers"`
	ExpectedZeroSavings bool                   `json:"expected_zero_savings"`
	MinFunctionCalls    int                    `json:"min_function_calls,omitempty"`
	MinFunctionOutputs  int                    `json:"min_function_call_outputs,omitempty"`
	MinFullHistory      int                    `json:"min_full_history_requests,omitempty"`
	LiveDelta           *codexCaptureLiveDelta `json:"live_delta,omitempty"`
	SearchCapProof      *searchCapProofReport  `json:"search_cap_proof,omitempty"`
	GatePassed          bool                   `json:"gate_passed,omitempty"`
	GateFailures        []string               `json:"gate_failures,omitempty"`
}

var requiredWSSProofWorkloads = []string{
	"repeat_full_read",
	"similar_files",
	"changed_file",
	"ranged_read",
	"search_loop",
	"git_status_diff",
	"build_test_lint_failure",
	"apply_patch_then_read",
	"long_mixed_workday",
	"no_savings_control",
}

const wssProofMatrixHelpText = `wss-proof-matrix: verify the Codex WSS real-workload proof matrix

Usage:
  go run ./scripts/utils wss-proof-matrix <captures.jsonl> [--json] [--require-live-token-delta]

Input JSONL rows:
  {"id":"cli-repeat-1","client":"cli","workload_class":"repeat_full_read","frames_path":"/tmp/frames.jsonl","decisions_path":"~/.slimference/debug/decisions.jsonl","expected_reducers":["read_delta"]}

The tool replays each frames file with wss-ab-replay lab/proof tool-output
mutation enabled, optionally audits the matching decisions log, and emits a
content-free PASS/FAIL matrix. Use --require-live-token-delta for release proofs
where replay bytes are not allowed to stand in for real billable token deltas.
Raw frame payloads stay local and are not copied into the report.

Optional focused-proof gates:
  --required-workload=<class>     Require one workload class; repeatable.
  --required-workloads=a,b        Require comma-separated workload classes.
  --min-captures=N                Minimum capture rows.
  --min-cli=N                     Minimum CLI capture rows.
  --min-desktop=N                 Minimum Desktop capture rows.
  --min-positive=N                Minimum positive-token or expected-zero rows.
  --expected-reducer=NAME         Require one live signal across the matrix; repeatable.
  --expected-reducer NAME         Same as above.
  --search-cap-candidate=F:M      For search_loop rows, run search-cap-proof with a candidate cap; repeatable.
  --search-cap-min-retained-pct=N Require candidate match-retention percentage (default 40).
  --search-cap-min-search-outputs=N Require resolved search-output breadth (default 2).
  --search-cap-min-extra-tokens=N Require extra reducer tokens vs default replay (default 1).

When focused workload flags are present, only matching workload rows are replayed,
counted, and gate-checked. If focused mode also passes --expected-reducer, those
command-line reducer expectations override row-local expected_reducers for the
focused proof. Unfocused release mode still validates every row as recorded.

Expected signal names include:
  read_delta, captured_output, codex_exec_envelope, repeated_output,
  chunk_dedup, chunk_dedup_refs, tool_prune, tool_prune_reattach,
	  tool_prune_retry, tool_prune_tokens_saved, output_reduce_injected,
	  output_reduce_output_tokens, output_reduce_skipped,
	  output_reduce_downgraded, stop_seq, streamcut, repdet, stale_read,
	  obsolete_prune, beterse, wss_stateful_prefix_elision,
	  wss_stateful_prefix_elision_tools, wss_stateful_prefix_elision_bytes,
	  wss_stateful_prefix_elision_tokens, provider_cache_read,
	  provider_cache_create, function_call_output_surface, tool_output_surface,
	  host_budget_ok.
Stateful prefix-elision proof rows must also declare min_function_calls and
min_function_call_outputs, so tool-schema savings cannot pass on token evidence
while suppressing the live tool-use surface.

Without focused-proof flags, the tool enforces the full release matrix:
10 captures, 5 CLI, 5 Desktop, all release workload classes, and 7 positive/zero
rows.`

func runWSSProofMatrix(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSProofMatrixFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssProofMatrixHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-proof-matrix <captures.jsonl> [--json] [--require-live-token-delta]")
		return 2
	}
	report, err := loadWSSProofMatrixReportWithOptions(flags.path, wssProofMatrixOptions{
		requireLiveTokenDelta:               flags.requireLiveTokenDelta,
		requiredWorkloads:                   flags.requiredWorkloads,
		expectedReducers:                    flags.expectedReducers,
		searchCapCandidates:                 flags.searchCapCandidates,
		searchCapMinCandidateRetainedPct:    flags.searchCapMinCandidateRetainedPct,
		searchCapMinSearchOutputs:           flags.searchCapMinSearchOutputs,
		searchCapMinExtraReducerTokens:      flags.searchCapMinExtraReducerTokens,
		searchCapMinExtraReducerTokensIsSet: flags.searchCapMinExtraReducerTokensIsSet,
		minCaptures:                         flags.minCaptures,
		minCLI:                              flags.minCLI,
		minDesktop:                          flags.minDesktop,
		minPositive:                         flags.minPositive,
	})
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
	writeWSSProofMatrixText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseWSSProofMatrixFlags(args []string) (wssProofMatrixFlags, error) {
	flags := wssProofMatrixFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--require-live-token-delta":
			flags.requireLiveTokenDelta = true
		case arg == "--expected-reducer":
			i++
			if i >= len(args) {
				return flags, fmt.Errorf("--expected-reducer requires a non-empty value")
			}
			value := strings.TrimSpace(args[i])
			if value == "" {
				return flags, fmt.Errorf("--expected-reducer requires a non-empty value")
			}
			flags.expectedReducers = append(flags.expectedReducers, value)
		case strings.HasPrefix(arg, "--expected-reducer="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--expected-reducer="))
			if value == "" {
				return flags, fmt.Errorf("--expected-reducer requires a non-empty value")
			}
			flags.expectedReducers = append(flags.expectedReducers, value)
		case arg == "--search-cap-candidate":
			i++
			if i >= len(args) {
				return flags, fmt.Errorf("--search-cap-candidate requires a files:matches value")
			}
			if err := appendWSSProofSearchCapCandidate(&flags, args[i]); err != nil {
				return flags, err
			}
		case strings.HasPrefix(arg, "--search-cap-candidate="):
			if err := appendWSSProofSearchCapCandidate(&flags, strings.TrimPrefix(arg, "--search-cap-candidate=")); err != nil {
				return flags, err
			}
		case strings.HasPrefix(arg, "--search-cap-min-retained-pct="):
			value, err := parseNonNegativeProofFloat(arg, "--search-cap-min-retained-pct=")
			if err != nil {
				return flags, err
			}
			flags.searchCapMinCandidateRetainedPct = value
		case strings.HasPrefix(arg, "--search-cap-min-search-outputs="):
			value, err := parseNonNegativeProofInt(arg, "--search-cap-min-search-outputs=")
			if err != nil {
				return flags, err
			}
			flags.searchCapMinSearchOutputs = value
		case strings.HasPrefix(arg, "--search-cap-min-extra-tokens="):
			value, err := parseNonNegativeProofInt(arg, "--search-cap-min-extra-tokens=")
			if err != nil {
				return flags, err
			}
			flags.searchCapMinExtraReducerTokens = value
			flags.searchCapMinExtraReducerTokensIsSet = true
		case strings.HasPrefix(arg, "--required-workload="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--required-workload="))
			if value == "" {
				return flags, fmt.Errorf("--required-workload requires a non-empty value")
			}
			flags.requiredWorkloads = append(flags.requiredWorkloads, value)
		case strings.HasPrefix(arg, "--required-workloads="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--required-workloads="))
			if value == "" {
				return flags, fmt.Errorf("--required-workloads requires a non-empty value")
			}
			for _, part := range strings.Split(value, ",") {
				workload := strings.TrimSpace(part)
				if workload != "" {
					flags.requiredWorkloads = append(flags.requiredWorkloads, workload)
				}
			}
			if len(flags.requiredWorkloads) == 0 {
				return flags, fmt.Errorf("--required-workloads requires at least one non-empty value")
			}
		case strings.HasPrefix(arg, "--min-captures="):
			value, err := parseNonNegativeProofInt(arg, "--min-captures=")
			if err != nil {
				return flags, err
			}
			flags.minCaptures = value
		case strings.HasPrefix(arg, "--min-cli="):
			value, err := parseNonNegativeProofInt(arg, "--min-cli=")
			if err != nil {
				return flags, err
			}
			flags.minCLI = value
		case strings.HasPrefix(arg, "--min-desktop="):
			value, err := parseNonNegativeProofInt(arg, "--min-desktop=")
			if err != nil {
				return flags, err
			}
			flags.minDesktop = value
		case strings.HasPrefix(arg, "--min-positive="):
			value, err := parseNonNegativeProofInt(arg, "--min-positive=")
			if err != nil {
				return flags, err
			}
			flags.minPositive = value
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple proof matrix files provided")
			}
			flags.path = arg
		}
	}
	if len(flags.searchCapCandidates) == 0 && (flags.searchCapMinCandidateRetainedPct > 0 || flags.searchCapMinSearchOutputs > 0 || flags.searchCapMinExtraReducerTokensIsSet) {
		return flags, fmt.Errorf("search-cap proof thresholds require --search-cap-candidate")
	}
	return flags, nil
}

func appendWSSProofSearchCapCandidate(flags *wssProofMatrixFlags, raw string) error {
	var candidates searchCapProfileCandidateFlags
	if err := candidates.Set(raw); err != nil {
		return fmt.Errorf("--search-cap-candidate %w", err)
	}
	flags.searchCapCandidates = append(flags.searchCapCandidates, candidates...)
	return nil
}

func parseNonNegativeProofInt(arg, prefix string) (int, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(arg, prefix))
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s requires a non-negative integer", strings.TrimSuffix(prefix, "="))
	}
	return value, nil
}

func parseNonNegativeProofFloat(arg, prefix string) (float64, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(arg, prefix))
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s requires a non-negative number", strings.TrimSuffix(prefix, "="))
	}
	return value, nil
}

func loadWSSProofMatrixReport(path string) (wssProofMatrixReport, error) {
	return loadWSSProofMatrixReportWithOptions(path, wssProofMatrixOptions{})
}

func loadWSSProofMatrixReportWithOptions(path string, options wssProofMatrixOptions) (wssProofMatrixReport, error) {
	records, err := readWSSProofMatrixRecords(path)
	if err != nil {
		return wssProofMatrixReport{}, err
	}
	report := wssProofMatrixReport{
		Path:                path,
		WorkloadClasses:     make(map[string]int),
		RequiredReducerHits: make(map[string]int64),
		GatePassed:          true,
	}
	baseDir := filepath.Dir(path)
	focusedWorkloads := focusedWSSProofWorkloads(options.requiredWorkloads)
	for _, record := range records {
		capture := wssProofMatrixCapture{
			ID:                  record.ID,
			Client:              normalizeProofClient(record.Client),
			WorkloadClass:       strings.TrimSpace(record.WorkloadClass),
			FramesPath:          resolveProofPath(baseDir, record.FramesPath),
			DecisionsPath:       resolveProofPath(baseDir, record.DecisionsPath),
			CodexVersion:        record.CodexVersion,
			SlimferenceCommit:   record.SlimferenceCommit,
			Repo:                record.Repo,
			Model:               record.Model,
			SocketSeq:           record.SocketSeq,
			ABPairID:            record.ABPairID,
			ABVariant:           record.ABVariant,
			StartedAt:           record.StartedAt,
			EndedAt:             record.EndedAt,
			ExpectedReducers:    append([]string(nil), record.ExpectedReducers...),
			ExpectedZeroSavings: record.ExpectedZeroSavings,
			MinFunctionCalls:    record.MinFunctionCalls,
			MinFunctionOutputs:  record.MinFunctionOutputs,
			MinFullHistory:      record.MinFullHistory,
			LiveDelta:           record.LiveDelta,
			GatePassed:          true,
		}
		if !wssProofRecordInScope(capture.WorkloadClass, focusedWorkloads) {
			continue
		}
		if capture.ID == "" {
			capture.ID = fmt.Sprintf("capture-%02d", len(report.CaptureReports)+1)
		}
		expectedReducers := proofCaptureExpectedReducers(capture.ExpectedReducers, options)
		capture.GateFailures = validateWSSProofMetadata(capture)

		replay, err := loadWSSABReplayReport(wssABReplayFlags{
			path:                capture.FramesPath,
			socketSeq:           capture.SocketSeq,
			failOnLost:          true,
			failOnUpstreamError: true,
			toolOutputMutation:  true,
		})
		if err != nil {
			capture.GateFailures = append(capture.GateFailures, fmt.Sprintf("replay failed: %v", err))
		} else {
			capture.Replay = sanitizeWSSProofReplayReport(replay)
			if !replay.GatePassed {
				capture.GateFailures = append(capture.GateFailures, replay.GateFailures...)
			}
			capture.GateFailures = append(capture.GateFailures, validateWSSProofShapeMinima(capture)...)
			if capture.WorkloadClass == "search_loop" {
				searchCapMutationProof := false
				if len(options.searchCapCandidates) > 0 {
					searchCapProof, err := loadSearchCapProofReport(wssProofSearchCapFlags(capture.FramesPath, capture.SocketSeq, options))
					if err != nil {
						capture.GateFailures = append(capture.GateFailures, fmt.Sprintf("search-cap proof failed: %v", err))
					} else {
						capture.SearchCapProof = &searchCapProof
						if !searchCapProof.GatePassed {
							capture.GateFailures = append(capture.GateFailures, prefixedSearchCapProofFailures("search_cap_proof", searchCapProof.GateFailures)...)
						} else if searchCapProofShowsSearchMutation(searchCapProof) {
							searchCapMutationProof = true
						}
					}
				}
				if replay.SearchRequestTurns+replay.SearchCapturedMutated == 0 && !searchCapMutationProof {
					capture.GateFailures = append(capture.GateFailures, "search_loop proof has no named search-output request")
				} else if replay.SearchMutatedRequests+replay.SearchCapturedMutated == 0 && !searchCapMutationProof {
					capture.GateFailures = append(capture.GateFailures, "search_loop proof has no named search-output mutation")
				}
			}
		}
		capture.GateFailures = append(capture.GateFailures, validateWSSProofPrefixElisionOracle(capture, expectedReducers)...)
		var requiredReducerHits map[string]int64
		if capture.LiveDelta != nil {
			for _, reducer := range options.expectedReducers {
				name := strings.TrimSpace(reducer)
				if name == "" {
					continue
				}
				if count, ok := liveReducerCount(name, capture.LiveDelta); ok && count > 0 {
					if requiredReducerHits == nil {
						requiredReducerHits = make(map[string]int64)
					}
					requiredReducerHits[name] += count
				}
			}
			tokenPositive := wssProofLiveEconomicSignal(capture)
			if capture.ExpectedZeroSavings && wssProofLiveLocalSavingsSignal(capture.LiveDelta) {
				capture.GateFailures = append(capture.GateFailures, "expected zero local savings, got positive local savings signal")
			}
			if !capture.ExpectedZeroSavings && !tokenPositive {
				capture.GateFailures = append(capture.GateFailures, "expected positive live economic signal, got none")
			}
			if safety := capture.LiveDelta.ParseFailures + capture.LiveDelta.DegradedSessions + capture.LiveDelta.CompressionErrors + capture.LiveDelta.AnalyticsProofEventsDropped; safety > 0 {
				capture.GateFailures = append(capture.GateFailures,
					fmt.Sprintf("live safety counters non-zero: parse=%d degraded=%d compression_errors=%d proof_events_dropped=%d",
						capture.LiveDelta.ParseFailures, capture.LiveDelta.DegradedSessions, capture.LiveDelta.CompressionErrors, capture.LiveDelta.AnalyticsProofEventsDropped))
			}
			capture.GateFailures = append(capture.GateFailures, validateWSSProofFunctionCallMinima(capture)...)
			if capture.LiveDelta.HostBudgetStatus != "" {
				if capture.LiveDelta.HostBudgetStatus != "ok" || capture.LiveDelta.HostBudgetExceeded || !capture.LiveDelta.HostBudgetCompressionOK || !capture.LiveDelta.HostBudgetDegradationOK {
					capture.GateFailures = append(capture.GateFailures,
						fmt.Sprintf("host budget not ok: status=%s exceeded=%t compression_ok=%t degradation_ok=%t reasons=%s",
							capture.LiveDelta.HostBudgetStatus,
							capture.LiveDelta.HostBudgetExceeded,
							capture.LiveDelta.HostBudgetCompressionOK,
							capture.LiveDelta.HostBudgetDegradationOK,
							strings.Join(capture.LiveDelta.HostBudgetReasons, ",")))
				}
			}
			hits, failures := validateExpectedReducers(expectedReducers, capture.LiveDelta)
			capture.ExpectedReducerHits = hits
			capture.GateFailures = append(capture.GateFailures, failures...)
		} else if options.requireLiveTokenDelta {
			capture.GateFailures = append(capture.GateFailures, "live_delta is required in --require-live-token-delta mode")
		} else if capture.MinFunctionCalls > 0 || capture.MinFunctionOutputs > 0 {
			capture.GateFailures = append(capture.GateFailures, "live_delta is required for function-call minima")
		} else if capture.Replay.Path != "" {
			if !capture.ExpectedZeroSavings && capture.Replay.BytesSaved <= 0 {
				capture.GateFailures = append(capture.GateFailures, "expected positive savings, no live token delta and replay bytes_saved<=0")
			}
		}
		if capture.DecisionsPath != "" {
			audit, err := loadWSSAuditReport(wssAuditFlags{path: capture.DecisionsPath, minPhaseF: 1})
			if err != nil {
				capture.GateFailures = append(capture.GateFailures, fmt.Sprintf("audit failed: %v", err))
			} else {
				capture.Audit = &audit
				if !audit.GatePassed {
					capture.GateFailures = append(capture.GateFailures, audit.GateFailures...)
				}
			}
		}
		capture.GatePassed = len(capture.GateFailures) == 0
		if !capture.GatePassed {
			report.CapturesWithIssues++
		} else {
			if capture.LiveDelta != nil {
				for name, count := range requiredReducerHits {
					report.RequiredReducerHits[name] += count
				}
				if wssProofLiveEconomicSignal(capture) {
					report.PositiveTokenSavings++
					report.PositiveSavings++
				}
			} else if capture.Replay.BytesSaved > 0 {
				report.PositiveSavings++
			}
			if capture.Replay.BytesSaved > 0 {
				report.PositiveReplayByteSavings++
			}
			switch capture.Client {
			case "cli":
				report.CLI++
			case "desktop":
				report.Desktop++
			}
			if capture.ExpectedZeroSavings {
				report.ExpectedZero++
			}
			if capture.WorkloadClass != "" {
				report.WorkloadClasses[capture.WorkloadClass]++
			}
		}
		report.Captures++
		report.CaptureReports = append(report.CaptureReports, capture)
	}
	requirements := wssProofRequirements(options)
	report.MissingWorkloads = missingWSSProofWorkloads(report.WorkloadClasses, requirements.requiredWorkloads)
	report.MissingRequiredReducers = missingWSSRequiredReducers(report.RequiredReducerHits, requirements.expectedReducers)
	if len(report.RequiredReducerHits) == 0 {
		report.RequiredReducerHits = nil
	}
	report.GateFailures = wssProofMatrixGateFailures(report, requirements)
	report.GatePassed = len(report.GateFailures) == 0
	return report, nil
}

func searchCapProofShowsSearchMutation(proof searchCapProofReport) bool {
	return proof.GatePassed &&
		proof.SelectedCandidate != nil &&
		proof.DefaultReplay.SearchRequestTurns > 0 &&
		proof.DefaultReplay.SearchMutatedRequests+proof.DefaultReplay.SearchCapturedMutated > 0 &&
		proof.DefaultReplay.SearchCapProofLatch &&
		!proof.DefaultReplay.ToolOutputMutation &&
		!proof.DefaultReplay.DeltaToolOutputMutation
}

func sanitizeWSSProofReplayReport(report wssABReplayReport) wssABReplayReport {
	report.Elisions = nil
	return report
}

func focusedWSSProofWorkloads(required []string) map[string]bool {
	if len(required) == 0 {
		return nil
	}
	out := make(map[string]bool, len(required))
	for _, raw := range required {
		workload := strings.TrimSpace(raw)
		if workload != "" {
			out[workload] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func proofCaptureExpectedReducers(rowExpected []string, options wssProofMatrixOptions) []string {
	if len(options.requiredWorkloads) > 0 && len(options.expectedReducers) > 0 {
		return options.expectedReducers
	}
	return rowExpected
}

func wssProofRecordInScope(workload string, focused map[string]bool) bool {
	if len(focused) == 0 {
		return true
	}
	return focused[strings.TrimSpace(workload)]
}

func wssProofLiveEconomicSignal(capture wssProofMatrixCapture) bool {
	return wssProofLiveEconomicTokens(capture.WorkloadClass, capture.LiveDelta) > 0
}

func wssProofLiveLocalSavingsSignal(live *codexCaptureLiveDelta) bool {
	if live == nil {
		return false
	}
	return live.BillableInputTokensSaved > 0 ||
		live.InputTokensSaved > 0 ||
		live.OutputWireBytesSaved > 0 ||
		live.RequestSideBytesReduced > 0 ||
		live.ToolPruneTokensSaved > 0 ||
		live.ProxyLayer0ReadDelta > 0 ||
		live.ProxyLayer0Captured > 0 ||
		live.ProxyLayer0Envelope > 0 ||
		live.ProxyLayer0Repeated > 0 ||
		live.ProxyLayer0ChunkDedup > 0 ||
		live.ProxyLayer0ChunkRefs > 0
}

func wssProofLiveEconomicTokens(workloadClass string, live *codexCaptureLiveDelta) int64 {
	if live == nil {
		return 0
	}
	if live.BillableInputTokensSaved > 0 {
		return live.BillableInputTokensSaved
	}
	switch strings.TrimSpace(workloadClass) {
	case "provider_cache_long_session", "host_resource_long_workday":
		return live.ProviderCacheReadTokens
	case "tool_heavy":
		return live.ToolPruneTokensSaved
	case "output_reduce_aggressive":
		if live.OutputReduceInjected <= 0 {
			return 0
		}
		return live.OutputReduceOutputTokensObserved
	default:
		return 0
	}
}

func wssProofRequirements(options wssProofMatrixOptions) wssProofMatrixRequirements {
	requirements := wssProofMatrixRequirements{
		requiredWorkloads:      requiredWSSProofWorkloads,
		expectedReducers:       normalizeExpectedReducers(options.expectedReducers),
		searchCapProofRequired: len(options.searchCapCandidates) > 0,
		minCaptures:            10,
		minCLI:                 5,
		minDesktop:             5,
		minPositive:            7,
	}
	if len(options.requiredWorkloads) > 0 {
		requirements.requiredWorkloads = append([]string(nil), options.requiredWorkloads...)
		requirements.minCaptures = len(requirements.requiredWorkloads)
		requirements.minCLI = 0
		requirements.minDesktop = 0
		requirements.minPositive = len(requirements.requiredWorkloads)
	}
	if options.minCaptures > 0 {
		requirements.minCaptures = options.minCaptures
	}
	if options.minCLI > 0 {
		requirements.minCLI = options.minCLI
	}
	if options.minDesktop > 0 {
		requirements.minDesktop = options.minDesktop
	}
	if options.minPositive > 0 {
		requirements.minPositive = options.minPositive
	}
	return requirements
}

func wssProofSearchCapFlags(framesPath string, socketSeq uint64, options wssProofMatrixOptions) searchCapProofFlags {
	minRetention := options.searchCapMinCandidateRetainedPct
	if minRetention <= 0 {
		minRetention = releaseSearchCapMinRetainedPct
	}
	minSearchOutputs := options.searchCapMinSearchOutputs
	if minSearchOutputs <= 0 {
		minSearchOutputs = releaseSearchCapMinSearchOutputs
	}
	minExtra := options.searchCapMinExtraReducerTokens
	if !options.searchCapMinExtraReducerTokensIsSet {
		minExtra = releaseSearchCapMinExtraReducerTokens
	}
	return searchCapProofFlags{
		framesPath:              framesPath,
		socketSeq:               socketSeq,
		candidates:              options.searchCapCandidates,
		minCandidateRetainedPct: minRetention,
		minSearchOutputs:        minSearchOutputs,
		minExtraReducerTokens:   minExtra,
	}
}

func normalizeExpectedReducers(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func readWSSProofMatrixRecords(path string) ([]wssProofMatrixRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open proof matrix %s: %w", path, err)
	}
	defer f.Close()
	var records []wssProofMatrixRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record wssProofMatrixRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("%s:%d: decode proof row: %w", path, lineNo, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan proof matrix %s: %w", path, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("proof matrix %s contained no captures", path)
	}
	return records, nil
}

func validateWSSProofMetadata(capture wssProofMatrixCapture) []string {
	var failures []string
	if capture.Client != "cli" && capture.Client != "desktop" {
		failures = append(failures, "client must be cli or desktop")
	}
	if capture.WorkloadClass == "" {
		failures = append(failures, "workload_class is required")
	}
	if capture.FramesPath == "" {
		failures = append(failures, "frames_path is required")
	}
	return failures
}

func validateWSSProofFunctionCallMinima(capture wssProofMatrixCapture) []string {
	if capture.MinFunctionCalls <= 0 && capture.MinFunctionOutputs <= 0 {
		return nil
	}
	if capture.LiveDelta == nil {
		return []string{"live_delta is required for function-call minima"}
	}
	var failures []string
	if capture.MinFunctionCalls > 0 && capture.LiveDelta.WireServerFunctionCalls < int64(capture.MinFunctionCalls) {
		failures = append(failures, fmt.Sprintf("wire_server_function_call_items=%d below required minimum %d", capture.LiveDelta.WireServerFunctionCalls, capture.MinFunctionCalls))
	}
	if capture.MinFunctionOutputs > 0 && capture.LiveDelta.WireFunctionCallOutputs < int64(capture.MinFunctionOutputs) {
		failures = append(failures, fmt.Sprintf("wire_function_call_output_items=%d below required minimum %d", capture.LiveDelta.WireFunctionCallOutputs, capture.MinFunctionOutputs))
	}
	return failures
}

func validateWSSProofShapeMinima(capture wssProofMatrixCapture) []string {
	if capture.MinFullHistory <= 0 {
		return nil
	}
	if capture.Replay.RequestShapes.FullHistory < capture.MinFullHistory {
		return []string{fmt.Sprintf("full_history_request_shapes=%d below required minimum %d", capture.Replay.RequestShapes.FullHistory, capture.MinFullHistory)}
	}
	return nil
}

func validateWSSProofPrefixElisionOracle(capture wssProofMatrixCapture, expected []string) []string {
	if !wssProofPrefixElisionProofSurface(capture, expected) {
		return nil
	}
	var failures []string
	if capture.LiveDelta == nil {
		failures = append(failures, "wss_stateful_prefix_elision proof requires live_delta tool-use oracle")
	}
	if capture.MinFunctionCalls <= 0 || capture.MinFunctionOutputs <= 0 {
		failures = append(failures, "wss_stateful_prefix_elision proof requires min_function_calls and min_function_call_outputs")
	}
	return failures
}

func wssProofPrefixElisionProofSurface(capture wssProofMatrixCapture, expected []string) bool {
	for _, raw := range expected {
		if wssProofPrefixElisionSignal(strings.TrimSpace(raw)) {
			return true
		}
	}
	if capture.LiveDelta == nil {
		return false
	}
	return capture.LiveDelta.WSSStatefulPrefixElisionRequests > 0 ||
		capture.LiveDelta.WSSStatefulPrefixElisionTools > 0 ||
		capture.LiveDelta.WSSStatefulPrefixElisionBytes > 0 ||
		capture.LiveDelta.WSSStatefulPrefixElisionTokens > 0
}

func wssProofPrefixElisionSignal(name string) bool {
	switch name {
	case "wss_stateful_prefix_elision",
		"wss_stateful_prefix_elision_tools",
		"wss_stateful_prefix_elision_bytes",
		"wss_stateful_prefix_elision_tokens":
		return true
	default:
		return false
	}
}

func validateExpectedReducers(expected []string, live *codexCaptureLiveDelta) (map[string]int64, []string) {
	if len(expected) == 0 {
		return nil, nil
	}
	hits := make(map[string]int64, len(expected))
	var failures []string
	for _, raw := range expected {
		name := strings.TrimSpace(raw)
		if name == "" || name == "none" {
			continue
		}
		count, ok := liveReducerCount(name, live)
		if !ok {
			failures = append(failures, "unknown expected reducer: "+name)
			continue
		}
		hits[name] = count
		if count <= 0 {
			failures = append(failures, expectedReducerMissingFailure(name, live))
		}
	}
	if len(hits) == 0 {
		return nil, failures
	}
	return hits, failures
}

func expectedReducerMissingFailure(name string, live *codexCaptureLiveDelta) string {
	if name == "captured_output" &&
		live != nil &&
		live.WireSurfaceFrames > 0 &&
		live.WireFunctionCallOutputs == 0 {
		return "expected reducer captured_output did not fire in live delta; no function_call_output input items were observed in the WSS capture"
	}
	return fmt.Sprintf("expected reducer %s did not fire in live delta", name)
}

func liveReducerCount(name string, live *codexCaptureLiveDelta) (int64, bool) {
	if live == nil {
		return 0, false
	}
	switch name {
	case "read_delta":
		return live.ProxyLayer0ReadDelta, true
	case "captured_output":
		return live.ProxyLayer0Captured, true
	case "codex_exec_envelope":
		return live.ProxyLayer0Envelope, true
	case "repeated_output":
		return live.ProxyLayer0Repeated, true
	case "chunk_dedup":
		return live.ProxyLayer0ChunkDedup, true
	case "chunk_dedup_refs":
		return live.ProxyLayer0ChunkRefs, true
	case "tool_prune":
		return live.ToolPrunePruned, true
	case "tool_prune_reattach":
		return live.ToolPruneReattach, true
	case "tool_prune_retry":
		return live.ToolPruneRetry, true
	case "tool_prune_tokens_saved":
		return live.ToolPruneTokensSaved, true
	case "output_reduce_injected":
		return live.OutputReduceInjected, true
	case "output_reduce_output_tokens":
		return live.OutputReduceOutputTokensObserved, true
	case "output_reduce_skipped":
		return live.OutputReduceSkipped, true
	case "output_reduce_downgraded":
		return live.OutputReduceDowngrades, true
	case "stop_seq":
		return live.StopSeqRequestsModified, true
	case "streamcut":
		return live.StreamcutFired, true
	case "repdet":
		return live.RepdetResponsesRewritten, true
	case "stale_read":
		return live.StaleReadBlocksReplaced, true
	case "obsolete_prune":
		return live.ObsoleteReadBlocksPruned, true
	case "beterse":
		return live.BeterseInjections, true
	case "wss_stateful_prefix_elision":
		return live.WSSStatefulPrefixElisionRequests, true
	case "wss_stateful_prefix_elision_tools":
		return live.WSSStatefulPrefixElisionTools, true
	case "wss_stateful_prefix_elision_bytes":
		return live.WSSStatefulPrefixElisionBytes, true
	case "wss_stateful_prefix_elision_tokens":
		return live.WSSStatefulPrefixElisionTokens, true
	case "provider_cache_read":
		return live.ProviderCacheReadTokens, true
	case "provider_cache_create":
		return live.ProviderCacheCreateTokens, true
	case "function_call_output_surface", "tool_output_surface":
		return live.WireFunctionCallOutputs, true
	case "host_budget_ok":
		if live.HostBudgetStatus == "ok" && !live.HostBudgetExceeded && live.HostBudgetCompressionOK && live.HostBudgetDegradationOK {
			return 1, true
		}
		return 0, true
	case "proof_events_ok":
		if live.AnalyticsProofEventsDropped == 0 {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func missingWSSRequiredReducers(hits map[string]int64, required []string) []string {
	var missing []string
	for _, name := range required {
		if _, ok := liveReducerCount(name, &codexCaptureLiveDelta{
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		}); !ok {
			missing = append(missing, "unknown:"+name)
			continue
		}
		if hits[name] <= 0 {
			missing = append(missing, name)
		}
	}
	return missing
}

func wssProofMatrixGateFailures(report wssProofMatrixReport, requirements wssProofMatrixRequirements) []string {
	var failures []string
	validCaptures := report.Captures - report.CapturesWithIssues
	if validCaptures < requirements.minCaptures {
		failures = append(failures, fmt.Sprintf("expected at least %d valid captures, got %d", requirements.minCaptures, validCaptures))
	}
	if report.CLI < requirements.minCLI {
		failures = append(failures, fmt.Sprintf("expected at least %d CLI captures, got %d", requirements.minCLI, report.CLI))
	}
	if report.Desktop < requirements.minDesktop {
		failures = append(failures, fmt.Sprintf("expected at least %d Desktop captures, got %d", requirements.minDesktop, report.Desktop))
	}
	if len(report.MissingWorkloads) > 0 {
		failures = append(failures, "missing workload classes: "+strings.Join(report.MissingWorkloads, ", "))
	}
	if len(report.MissingRequiredReducers) > 0 {
		failures = append(failures, "missing expected reducer signals: "+strings.Join(report.MissingRequiredReducers, ", "))
	}
	if requirements.searchCapProofRequired && report.WorkloadClasses["search_loop"] == 0 {
		failures = append(failures, "search-cap proof requires at least one search_loop capture")
	}
	if report.PositiveSavings+report.ExpectedZero < requirements.minPositive {
		failures = append(failures, fmt.Sprintf("expected at least %d positive-token-savings or expected-zero captures, got %d", requirements.minPositive, report.PositiveSavings+report.ExpectedZero))
	}
	if report.CapturesWithIssues > 0 {
		failures = append(failures, fmt.Sprintf("%d capture(s) failed per-capture gates", report.CapturesWithIssues))
	}
	return failures
}

func missingWSSProofWorkloads(classes map[string]int, required []string) []string {
	var missing []string
	for _, class := range required {
		if classes[class] == 0 {
			missing = append(missing, class)
		}
	}
	return missing
}

func normalizeProofClient(client string) string {
	return strings.ToLower(strings.TrimSpace(client))
}

func resolveProofPath(baseDir, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func writeWSSProofMatrixText(w io.Writer, report wssProofMatrixReport) {
	fmt.Fprintf(w, "WSS proof matrix: %s\n", report.Path)
	fmt.Fprintf(w, "  captures:          %d\n", report.Captures)
	fmt.Fprintf(w, "  cli/desktop:       %d / %d\n", report.CLI, report.Desktop)
	fmt.Fprintf(w, "  positive/zero:     %d / %d\n", report.PositiveSavings, report.ExpectedZero)
	fmt.Fprintf(w, "  token/replay+:     %d / %d\n", report.PositiveTokenSavings, report.PositiveReplayByteSavings)
	fmt.Fprintf(w, "  capture issues:    %d\n", report.CapturesWithIssues)
	fmt.Fprintf(w, "  gate:              %s\n", passFail(report.GatePassed))
	if len(report.WorkloadClasses) > 0 {
		fmt.Fprintln(w, "\nWorkloads:")
		for _, key := range sortedStringKeys(report.WorkloadClasses) {
			fmt.Fprintf(w, "  %-28s %d\n", key, report.WorkloadClasses[key])
		}
	}
	if len(report.CaptureReports) > 0 {
		fmt.Fprintln(w, "\nCaptures:")
		for _, capture := range report.CaptureReports {
			status := passFail(capture.GatePassed)
			tokens := int64(0)
			economicTokens := int64(0)
			if capture.LiveDelta != nil {
				tokens = capture.LiveDelta.BillableInputTokensSaved
				economicTokens = wssProofLiveEconomicTokens(capture.WorkloadClass, capture.LiveDelta)
			}
			fmt.Fprintf(w, "  %-24s %-7s %-24s billable_tokens=%d economic_tokens=%d replay_bytes=%d mutated=%d gate=%s\n",
				capture.ID, capture.Client, capture.WorkloadClass,
				tokens, economicTokens, capture.Replay.BytesSaved, capture.Replay.MutatedRequests, status)
			if capture.SearchCapProof != nil {
				if capture.SearchCapProof.SelectedCandidate != nil {
					selected := capture.SearchCapProof.SelectedCandidate
					fmt.Fprintf(w, "    search_cap: %s %d/%d extra_tokens=%+d retained=%.2f%% gate=%s\n",
						selected.Name,
						selected.MaxFilesShown,
						selected.MaxMatchesPerFile,
						selected.ExtraReducerTokens,
						selected.MatchRetentionPct,
						passFail(capture.SearchCapProof.GatePassed))
				} else {
					fmt.Fprintf(w, "    search_cap: no selected candidate gate=%s\n", passFail(capture.SearchCapProof.GatePassed))
				}
			}
			for _, failure := range capture.GateFailures {
				fmt.Fprintf(w, "    - %s\n", failure)
			}
		}
	}
	if len(report.GateFailures) > 0 {
		fmt.Fprintln(w, "\nGate failures:")
		for _, failure := range report.GateFailures {
			fmt.Fprintf(w, "  - %s\n", failure)
		}
	}
}

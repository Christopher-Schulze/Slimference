package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type wssSavingsBaselineFlags struct {
	path                  string
	outputFormat          string
	searchCapFiles        int
	searchCapMatches      int
	includeUnsafeDeltaLab bool
	help                  bool
}

type wssSavingsBaselineReport struct {
	Path                  string                   `json:"path"`
	FrameFiles            int                      `json:"frame_files"`
	SkippedFiles          int                      `json:"skipped_files"`
	SearchCapFiles        int                      `json:"search_cap_files"`
	SearchCapMatches      int                      `json:"search_cap_matches"`
	IncludeUnsafeDeltaLab bool                     `json:"include_unsafe_delta_lab,omitempty"`
	Totals                wssSavingsBaselineTotal  `json:"totals"`
	Rows                  []wssSavingsBaselineRow  `json:"rows"`
	Skips                 []wssSavingsBaselineSkip `json:"skips,omitempty"`
	Findings              []string                 `json:"findings,omitempty"`
	GatePassed            bool                     `json:"gate_passed"`
	GateFailures          []string                 `json:"gate_failures,omitempty"`
}

type wssSavingsBaselineRow struct {
	Path                         string                           `json:"path"`
	Frames                       int                              `json:"frames"`
	RequestTurns                 int                              `json:"request_turns"`
	SearchRequestTurns           int                              `json:"search_request_turns"`
	RequestShapes                replayShapeCounts                `json:"request_shapes"`
	Product                      wssSavingsBaselineReplaySummary  `json:"product"`
	SearchCapProofLatch          wssSavingsBaselineReplaySummary  `json:"search_cap_proof_latch"`
	BroadToolOutputNonDelta      wssSavingsBaselineReplaySummary  `json:"broad_tool_output_non_delta"`
	UnsafeDeltaLab               *wssSavingsBaselineReplaySummary `json:"unsafe_delta_lab,omitempty"`
	SearchCapExtraTokens         int                              `json:"search_cap_extra_tokens"`
	SearchCapExtraBytes          int                              `json:"search_cap_extra_bytes"`
	BroadToolOutputExtraTokens   int                              `json:"broad_tool_output_extra_tokens"`
	BroadToolOutputExtraBytes    int                              `json:"broad_tool_output_extra_bytes"`
	UnsafeDeltaLabExtraTokens    int                              `json:"unsafe_delta_lab_extra_tokens,omitempty"`
	UnsafeDeltaLabExtraBytes     int                              `json:"unsafe_delta_lab_extra_bytes,omitempty"`
	SearchDeltaGuarded           bool                             `json:"search_delta_guarded,omitempty"`
	FullHistorySearchCapPositive bool                             `json:"full_history_search_cap_positive,omitempty"`
	ProductPositive              bool                             `json:"product_positive,omitempty"`
	GatePassed                   bool                             `json:"gate_passed"`
	GateFailures                 []string                         `json:"gate_failures,omitempty"`
}

type wssSavingsBaselineReplaySummary struct {
	MutatedRequests               int               `json:"mutated_requests"`
	MutatedDelta                  int               `json:"mutated_delta"`
	MutatedFullHistory            int               `json:"mutated_full_history"`
	BytesSaved                    int               `json:"bytes_saved"`
	ReducerTokensSaved            int               `json:"reducer_tokens_saved"`
	ReducerBlocksModified         int               `json:"reducer_blocks_modified"`
	ReducerReadDeltaBlocks        int               `json:"reducer_read_delta_blocks"`
	ReducerRepeatedBlocks         int               `json:"reducer_repeated_output_blocks"`
	ReducerChunkBlocks            int               `json:"reducer_chunk_dedup_blocks"`
	ReducerCapturedBlocks         int               `json:"reducer_captured_output_blocks"`
	ReducerEnvelopeBlocks         int               `json:"reducer_codex_envelope_blocks"`
	ReducerChunkRefs              int               `json:"reducer_chunk_dedup_references"`
	CompoundedEstimateTokens      int               `json:"compounded_estimate_tokens"`
	HighFootprintAppliedDecisions int               `json:"high_footprint_applied_decisions"`
	GuardedDeltaReadDeltaHits     int               `json:"guarded_delta_read_delta_hits,omitempty"`
	GuardedDeltaReadDeltaMisses   int               `json:"guarded_delta_read_delta_misses,omitempty"`
	GuardedDeltaRepeatedHits      int               `json:"guarded_delta_repeated_output_hits,omitempty"`
	GuardedDeltaRepeatedMisses    int               `json:"guarded_delta_repeated_output_misses,omitempty"`
	SearchMutatedRequests         int               `json:"search_mutated_requests"`
	SearchCapturedMutated         int               `json:"search_captured_mutated_requests,omitempty"`
	UpstreamErrorFrames           int               `json:"upstream_error_frames"`
	UpstreamInvalidRequests       int               `json:"upstream_invalid_request_errors"`
	UpstreamHTTP400Errors         int               `json:"upstream_http_400_errors"`
	UpstreamResponseFailures      int               `json:"upstream_response_failed_frames"`
	Lost                          int               `json:"lost"`
	GatePassed                    bool              `json:"gate_passed"`
	GateFailures                  []string          `json:"gate_failures,omitempty"`
	MutatedShapes                 replayShapeCounts `json:"mutated_shapes"`
}

type wssSavingsBaselineTotal struct {
	RequestTurns                      int `json:"request_turns"`
	SearchRequestTurns                int `json:"search_request_turns"`
	ProductPositiveFiles              int `json:"product_positive_files"`
	ProductReducerTokensSaved         int `json:"product_reducer_tokens_saved"`
	ProductBytesSaved                 int `json:"product_bytes_saved"`
	ProductGuardedDeltaReadHits       int `json:"product_guarded_delta_read_delta_hits"`
	ProductGuardedDeltaReadMisses     int `json:"product_guarded_delta_read_delta_misses"`
	ProductGuardedDeltaRepeatedHits   int `json:"product_guarded_delta_repeated_output_hits"`
	ProductGuardedDeltaRepeatedMisses int `json:"product_guarded_delta_repeated_output_misses"`
	SearchCapPositiveExtraFiles       int `json:"search_cap_positive_extra_files"`
	SearchCapExtraTokens              int `json:"search_cap_extra_tokens"`
	SearchCapExtraBytes               int `json:"search_cap_extra_bytes"`
	SearchDeltaGuardedFiles           int `json:"search_delta_guarded_files"`
	FullHistorySearchCapFiles         int `json:"full_history_search_cap_files"`
	BroadToolOutputPositiveFiles      int `json:"broad_tool_output_positive_files"`
	BroadToolOutputExtraTokens        int `json:"broad_tool_output_extra_tokens"`
	BroadToolOutputExtraBytes         int `json:"broad_tool_output_extra_bytes"`
	UnsafeDeltaLabPositiveFiles       int `json:"unsafe_delta_lab_positive_files,omitempty"`
	UnsafeDeltaLabExtraTokens         int `json:"unsafe_delta_lab_extra_tokens,omitempty"`
	UnsafeDeltaLabExtraBytes          int `json:"unsafe_delta_lab_extra_bytes,omitempty"`
	ProductSafetyIssueFiles           int `json:"product_safety_issue_files"`
	SearchCapSafetyIssueFiles         int `json:"search_cap_safety_issue_files"`
	BroadToolOutputSafetyIssueFiles   int `json:"broad_tool_output_safety_issue_files"`
	UnsafeDeltaLabSafetyIssueFiles    int `json:"unsafe_delta_lab_safety_issue_files,omitempty"`
}

type wssSavingsBaselineSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

const wssSavingsBaselineHelpText = `wss-savings-baseline: replay WSS captures through product and proof-latch modes

Usage:
  go run ./scripts/utils wss-savings-baseline <frames.jsonl-or-dir> [--json]

Flags:
  --json                      Output JSON
  --search-cap-files N         Candidate search-cap files shown (default 25)
  --search-cap-matches N       Candidate search-cap matches per file (default 15)
  --include-unsafe-delta-lab   Also replay the known-unsafe delta lab override

The report is content-free: it emits replay counters, safety issues, and guard
gap classes, never raw prompt or tool payload text. Product completion gates must
use product/default and proof-latch rows; unsafe delta-lab numbers are diagnostic
only and must not be promoted without fresh live zero-drawdown proof.`

func runWSSSavingsBaseline(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSSavingsBaselineFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssSavingsBaselineHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-savings-baseline <frames.jsonl-or-dir> [--json]")
		return 2
	}
	report, err := loadWSSSavingsBaselineReport(flags)
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
	writeWSSSavingsBaselineText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseWSSSavingsBaselineFlags(args []string) (wssSavingsBaselineFlags, error) {
	flags := wssSavingsBaselineFlags{
		outputFormat:     outputText,
		searchCapFiles:   25,
		searchCapMatches: 15,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--include-unsafe-delta-lab":
			flags.includeUnsafeDeltaLab = true
		case arg == "--search-cap-files":
			i++
			if i >= len(args) {
				return flags, fmt.Errorf("--search-cap-files requires a value")
			}
			n, err := parseNonNegativeIntFlag("--search-cap-files", args[i])
			if err != nil {
				return flags, err
			}
			flags.searchCapFiles = n
		case strings.HasPrefix(arg, "--search-cap-files="):
			n, err := parseNonNegativeIntFlag("--search-cap-files", strings.TrimPrefix(arg, "--search-cap-files="))
			if err != nil {
				return flags, err
			}
			flags.searchCapFiles = n
		case arg == "--search-cap-matches":
			i++
			if i >= len(args) {
				return flags, fmt.Errorf("--search-cap-matches requires a value")
			}
			n, err := parseNonNegativeIntFlag("--search-cap-matches", args[i])
			if err != nil {
				return flags, err
			}
			flags.searchCapMatches = n
		case strings.HasPrefix(arg, "--search-cap-matches="):
			n, err := parseNonNegativeIntFlag("--search-cap-matches", strings.TrimPrefix(arg, "--search-cap-matches="))
			if err != nil {
				return flags, err
			}
			flags.searchCapMatches = n
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple baseline paths provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func loadWSSSavingsBaselineReport(flags wssSavingsBaselineFlags) (wssSavingsBaselineReport, error) {
	files, singleFile, err := wssSavingsBaselineFiles(flags.path)
	if err != nil {
		return wssSavingsBaselineReport{}, err
	}
	restoreLogger := silenceWSSSavingsBaselineReplayLogs()
	defer restoreLogger()
	report := wssSavingsBaselineReport{
		Path:                  flags.path,
		SearchCapFiles:        flags.searchCapFiles,
		SearchCapMatches:      flags.searchCapMatches,
		IncludeUnsafeDeltaLab: flags.includeUnsafeDeltaLab,
		GatePassed:            true,
	}
	for _, path := range files {
		row, err := loadWSSSavingsBaselineRow(path, flags)
		if err != nil {
			if singleFile {
				return wssSavingsBaselineReport{}, err
			}
			report.Skips = append(report.Skips, wssSavingsBaselineSkip{Path: path, Reason: err.Error()})
			report.SkippedFiles++
			continue
		}
		report.Rows = append(report.Rows, row)
		report.FrameFiles++
		applyWSSSavingsBaselineRow(&report.Totals, row)
		if !row.GatePassed {
			report.GatePassed = false
			report.GateFailures = append(report.GateFailures, fmt.Sprintf("%s: %s", path, strings.Join(row.GateFailures, "; ")))
		}
	}
	if report.FrameFiles == 0 {
		return wssSavingsBaselineReport{}, fmt.Errorf("no WSS replay frame files found under %s", flags.path)
	}
	report.Findings = wssSavingsBaselineFindings(report)
	return report, nil
}

func silenceWSSSavingsBaselineReplayLogs() func() {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return func() {
		slog.SetDefault(previous)
	}
}

func loadWSSSavingsBaselineRow(path string, flags wssSavingsBaselineFlags) (wssSavingsBaselineRow, error) {
	product, err := loadWSSABReplayReport(wssABReplayFlags{
		path:                path,
		failOnLost:          true,
		failOnUpstreamError: true,
	})
	if err != nil {
		return wssSavingsBaselineRow{}, err
	}
	searchCap, err := loadWSSABReplayReport(wssABReplayFlags{
		path:                path,
		failOnLost:          true,
		failOnUpstreamError: true,
		searchCapProofLatch: true,
		searchCapFiles:      flags.searchCapFiles,
		searchCapMatches:    flags.searchCapMatches,
	})
	if err != nil {
		return wssSavingsBaselineRow{}, err
	}
	broad, err := loadWSSABReplayReport(wssABReplayFlags{
		path:                path,
		failOnLost:          true,
		failOnUpstreamError: true,
		toolOutputMutation:  true,
	})
	if err != nil {
		return wssSavingsBaselineRow{}, err
	}
	row := wssSavingsBaselineRow{
		Path:                       path,
		Frames:                     product.Frames,
		RequestTurns:               product.RequestTurns,
		SearchRequestTurns:         product.SearchRequestTurns,
		RequestShapes:              product.RequestShapes,
		Product:                    wssSavingsBaselineReplaySummaryFrom(product),
		SearchCapProofLatch:        wssSavingsBaselineReplaySummaryFrom(searchCap),
		BroadToolOutputNonDelta:    wssSavingsBaselineReplaySummaryFrom(broad),
		SearchCapExtraTokens:       positiveDelta(searchCap.ReducerTokensSaved, product.ReducerTokensSaved),
		SearchCapExtraBytes:        positiveDelta(searchCap.BytesSaved, product.BytesSaved),
		BroadToolOutputExtraTokens: positiveDelta(broad.ReducerTokensSaved, product.ReducerTokensSaved),
		BroadToolOutputExtraBytes:  positiveDelta(broad.BytesSaved, product.BytesSaved),
		ProductPositive:            product.ReducerTokensSaved > 0 || product.BytesSaved > 0,
		GatePassed:                 true,
	}
	row.SearchDeltaGuarded = row.SearchRequestTurns > 0 &&
		row.SearchCapProofLatch.SearchMutatedRequests == 0 &&
		row.RequestShapes.Delta > 0 &&
		row.SearchCapExtraTokens == 0 &&
		row.SearchCapExtraBytes == 0
	row.FullHistorySearchCapPositive = row.SearchCapProofLatch.SearchMutatedRequests > 0 &&
		row.SearchCapProofLatch.MutatedDelta == 0 &&
		row.SearchCapExtraTokens > 0
	row.GateFailures = append(row.GateFailures, wssSavingsBaselineModeFailures("product", product)...)
	row.GateFailures = append(row.GateFailures, wssSavingsBaselineModeFailures("search_cap", searchCap)...)
	row.GateFailures = append(row.GateFailures, wssSavingsBaselineModeFailures("broad_tool_output", broad)...)
	if flags.includeUnsafeDeltaLab {
		unsafeDelta, err := loadWSSABReplayReport(wssABReplayFlags{
			path:                       path,
			failOnLost:                 true,
			failOnUpstreamError:        true,
			toolOutputMutation:         true,
			deltaToolOutputMutationLab: true,
		})
		if err != nil {
			return wssSavingsBaselineRow{}, err
		}
		summary := wssSavingsBaselineReplaySummaryFrom(unsafeDelta)
		row.UnsafeDeltaLab = &summary
		row.UnsafeDeltaLabExtraTokens = positiveDelta(unsafeDelta.ReducerTokensSaved, product.ReducerTokensSaved)
		row.UnsafeDeltaLabExtraBytes = positiveDelta(unsafeDelta.BytesSaved, product.BytesSaved)
		row.GateFailures = append(row.GateFailures, wssSavingsBaselineModeFailures("unsafe_delta_lab", unsafeDelta)...)
	}
	row.GatePassed = len(row.GateFailures) == 0
	return row, nil
}

func wssSavingsBaselineReplaySummaryFrom(report wssABReplayReport) wssSavingsBaselineReplaySummary {
	return wssSavingsBaselineReplaySummary{
		MutatedRequests:               report.MutatedRequests,
		MutatedDelta:                  report.MutatedShapes.Delta,
		MutatedFullHistory:            report.MutatedShapes.FullHistory,
		BytesSaved:                    report.BytesSaved,
		ReducerTokensSaved:            report.ReducerTokensSaved,
		ReducerBlocksModified:         report.ReducerBlocksModified,
		ReducerReadDeltaBlocks:        report.ReducerReadDeltaBlocks,
		ReducerRepeatedBlocks:         report.ReducerRepeatedBlocks,
		ReducerChunkBlocks:            report.ReducerChunkBlocks,
		ReducerCapturedBlocks:         report.ReducerCapturedBlocks,
		ReducerEnvelopeBlocks:         report.ReducerEnvelopeBlocks,
		ReducerChunkRefs:              report.ReducerChunkRefs,
		CompoundedEstimateTokens:      report.CompoundedEstimateTokens,
		HighFootprintAppliedDecisions: report.HighFootprintAppliedDecisions,
		GuardedDeltaReadDeltaHits:     report.GuardedDeltaReadDeltaHits,
		GuardedDeltaReadDeltaMisses:   report.GuardedDeltaReadDeltaMisses,
		GuardedDeltaRepeatedHits:      report.GuardedDeltaRepeatedHits,
		GuardedDeltaRepeatedMisses:    report.GuardedDeltaRepeatedMisses,
		SearchMutatedRequests:         report.SearchMutatedRequests,
		SearchCapturedMutated:         report.SearchCapturedMutated,
		UpstreamErrorFrames:           report.UpstreamErrorFrames,
		UpstreamInvalidRequests:       report.UpstreamInvalidRequests,
		UpstreamHTTP400Errors:         report.UpstreamHTTP400Errors,
		UpstreamResponseFailures:      report.UpstreamResponseFailures,
		Lost:                          report.Lost,
		GatePassed:                    report.GatePassed,
		GateFailures:                  append([]string(nil), report.GateFailures...),
		MutatedShapes:                 report.MutatedShapes,
	}
}

func wssSavingsBaselineFiles(path string) ([]string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, true, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("scan %s: %w", path, err)
	}
	sort.Strings(files)
	return files, false, nil
}

func applyWSSSavingsBaselineRow(total *wssSavingsBaselineTotal, row wssSavingsBaselineRow) {
	total.RequestTurns += row.RequestTurns
	total.SearchRequestTurns += row.SearchRequestTurns
	total.ProductReducerTokensSaved += row.Product.ReducerTokensSaved
	total.ProductBytesSaved += row.Product.BytesSaved
	total.ProductGuardedDeltaReadHits += row.Product.GuardedDeltaReadDeltaHits
	total.ProductGuardedDeltaReadMisses += row.Product.GuardedDeltaReadDeltaMisses
	total.ProductGuardedDeltaRepeatedHits += row.Product.GuardedDeltaRepeatedHits
	total.ProductGuardedDeltaRepeatedMisses += row.Product.GuardedDeltaRepeatedMisses
	if row.ProductPositive {
		total.ProductPositiveFiles++
	}
	if row.Product.UpstreamErrorFrames > 0 || row.Product.Lost > 0 || !row.Product.GatePassed {
		total.ProductSafetyIssueFiles++
	}
	total.SearchCapExtraTokens += row.SearchCapExtraTokens
	total.SearchCapExtraBytes += row.SearchCapExtraBytes
	if row.SearchCapExtraTokens > 0 || row.SearchCapExtraBytes > 0 {
		total.SearchCapPositiveExtraFiles++
	}
	if row.SearchCapProofLatch.UpstreamErrorFrames > 0 || row.SearchCapProofLatch.Lost > 0 || !row.SearchCapProofLatch.GatePassed {
		total.SearchCapSafetyIssueFiles++
	}
	if row.SearchDeltaGuarded {
		total.SearchDeltaGuardedFiles++
	}
	if row.FullHistorySearchCapPositive {
		total.FullHistorySearchCapFiles++
	}
	total.BroadToolOutputExtraTokens += row.BroadToolOutputExtraTokens
	total.BroadToolOutputExtraBytes += row.BroadToolOutputExtraBytes
	if row.BroadToolOutputExtraTokens > 0 || row.BroadToolOutputExtraBytes > 0 {
		total.BroadToolOutputPositiveFiles++
	}
	if row.BroadToolOutputNonDelta.UpstreamErrorFrames > 0 || row.BroadToolOutputNonDelta.Lost > 0 || !row.BroadToolOutputNonDelta.GatePassed {
		total.BroadToolOutputSafetyIssueFiles++
	}
	if row.UnsafeDeltaLab != nil {
		total.UnsafeDeltaLabExtraTokens += row.UnsafeDeltaLabExtraTokens
		total.UnsafeDeltaLabExtraBytes += row.UnsafeDeltaLabExtraBytes
		if row.UnsafeDeltaLabExtraTokens > 0 || row.UnsafeDeltaLabExtraBytes > 0 {
			total.UnsafeDeltaLabPositiveFiles++
		}
		if row.UnsafeDeltaLab.UpstreamErrorFrames > 0 || row.UnsafeDeltaLab.Lost > 0 || !row.UnsafeDeltaLab.GatePassed {
			total.UnsafeDeltaLabSafetyIssueFiles++
		}
	}
}

func wssSavingsBaselineFindings(report wssSavingsBaselineReport) []string {
	var findings []string
	if report.Totals.ProductSafetyIssueFiles == 0 {
		findings = append(findings, "product_default_safety_clean")
	}
	if report.Totals.ProductPositiveFiles > 0 {
		findings = append(findings, fmt.Sprintf("product_default_positive_files=%d", report.Totals.ProductPositiveFiles))
	}
	if hits := report.Totals.ProductGuardedDeltaReadHits + report.Totals.ProductGuardedDeltaRepeatedHits; hits > 0 {
		findings = append(findings, fmt.Sprintf("product_guarded_delta_observe_hits=%d", hits))
	}
	if misses := report.Totals.ProductGuardedDeltaReadMisses + report.Totals.ProductGuardedDeltaRepeatedMisses; misses > 0 {
		findings = append(findings, fmt.Sprintf("product_guarded_delta_observe_misses=%d", misses))
	}
	if report.Totals.SearchDeltaGuardedFiles > 0 {
		findings = append(findings, fmt.Sprintf("search_delta_guarded_files=%d", report.Totals.SearchDeltaGuardedFiles))
	}
	if report.Totals.FullHistorySearchCapFiles > 0 {
		findings = append(findings, fmt.Sprintf("full_history_search_cap_positive_files=%d", report.Totals.FullHistorySearchCapFiles))
	}
	if report.Totals.SearchCapExtraTokens > 0 {
		findings = append(findings, fmt.Sprintf("search_cap_extra_tokens=%d", report.Totals.SearchCapExtraTokens))
	}
	if report.Totals.BroadToolOutputExtraTokens > 0 {
		findings = append(findings, fmt.Sprintf("broad_tool_output_non_delta_extra_tokens=%d", report.Totals.BroadToolOutputExtraTokens))
	}
	if report.IncludeUnsafeDeltaLab && report.Totals.UnsafeDeltaLabExtraTokens > 0 {
		findings = append(findings, fmt.Sprintf("unsafe_delta_lab_extra_tokens=%d", report.Totals.UnsafeDeltaLabExtraTokens))
	}
	return findings
}

func wssSavingsBaselineModeFailures(label string, report wssABReplayReport) []string {
	if report.GatePassed {
		return nil
	}
	out := make([]string, 0, len(report.GateFailures))
	for _, failure := range report.GateFailures {
		out = append(out, label+": "+failure)
	}
	return out
}

func writeWSSSavingsBaselineText(w io.Writer, report wssSavingsBaselineReport) {
	fmt.Fprintf(w, "WSS savings baseline: %s\n", report.Path)
	fmt.Fprintf(w, "  frame_files:       %d\n", report.FrameFiles)
	fmt.Fprintf(w, "  skipped_files:     %d\n", report.SkippedFiles)
	fmt.Fprintf(w, "  request_turns:     %d\n", report.Totals.RequestTurns)
	fmt.Fprintf(w, "  search_turns:      %d\n", report.Totals.SearchRequestTurns)
	fmt.Fprintf(w, "  product_default:   files=%d tokens=%d bytes=%d safety_issues=%d\n",
		report.Totals.ProductPositiveFiles,
		report.Totals.ProductReducerTokensSaved,
		report.Totals.ProductBytesSaved,
		report.Totals.ProductSafetyIssueFiles)
	if report.Totals.ProductGuardedDeltaReadHits > 0 || report.Totals.ProductGuardedDeltaReadMisses > 0 ||
		report.Totals.ProductGuardedDeltaRepeatedHits > 0 || report.Totals.ProductGuardedDeltaRepeatedMisses > 0 {
		fmt.Fprintf(w, "  guarded_delta_obs: read_hit=%d read_miss=%d repeated_hit=%d repeated_miss=%d\n",
			report.Totals.ProductGuardedDeltaReadHits,
			report.Totals.ProductGuardedDeltaReadMisses,
			report.Totals.ProductGuardedDeltaRepeatedHits,
			report.Totals.ProductGuardedDeltaRepeatedMisses)
	}
	fmt.Fprintf(w, "  search_cap_latch:  files=%d extra_tokens=%d extra_bytes=%d guarded_delta_files=%d full_history_files=%d safety_issues=%d\n",
		report.Totals.SearchCapPositiveExtraFiles,
		report.Totals.SearchCapExtraTokens,
		report.Totals.SearchCapExtraBytes,
		report.Totals.SearchDeltaGuardedFiles,
		report.Totals.FullHistorySearchCapFiles,
		report.Totals.SearchCapSafetyIssueFiles)
	fmt.Fprintf(w, "  broad_non_delta:   files=%d extra_tokens=%d extra_bytes=%d safety_issues=%d\n",
		report.Totals.BroadToolOutputPositiveFiles,
		report.Totals.BroadToolOutputExtraTokens,
		report.Totals.BroadToolOutputExtraBytes,
		report.Totals.BroadToolOutputSafetyIssueFiles)
	if report.IncludeUnsafeDeltaLab {
		fmt.Fprintf(w, "  unsafe_delta_lab:  files=%d extra_tokens=%d extra_bytes=%d safety_issues=%d\n",
			report.Totals.UnsafeDeltaLabPositiveFiles,
			report.Totals.UnsafeDeltaLabExtraTokens,
			report.Totals.UnsafeDeltaLabExtraBytes,
			report.Totals.UnsafeDeltaLabSafetyIssueFiles)
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

func positiveDelta(candidate, baseline int) int {
	if candidate <= baseline {
		return 0
	}
	return candidate - baseline
}

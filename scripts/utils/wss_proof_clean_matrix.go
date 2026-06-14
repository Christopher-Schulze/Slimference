package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type wssProofCleanMatrixFlags struct {
	inputPath    string
	outputPath   string
	outputFormat string
	help         bool
}

type wssProofCleanMatrixReport struct {
	InputPath      string         `json:"input_path"`
	OutputPath     string         `json:"output_path"`
	RowsRead       int            `json:"rows_read"`
	RowsWritten    int            `json:"rows_written"`
	RowsNormalized int            `json:"rows_normalized"`
	RowsSkipped    int            `json:"rows_skipped"`
	SkippedReasons map[string]int `json:"skipped_reasons,omitempty"`
	NormalizedIDs  []string       `json:"normalized_ids,omitempty"`
	SkippedIDs     []string       `json:"skipped_ids,omitempty"`
}

const wssProofCleanMatrixHelpText = `wss-proof-clean-matrix: write an explicit clean release proof matrix

Usage:
  go run ./scripts/utils wss-proof-clean-matrix <dir-or-matrix.jsonl> <out.jsonl> [--json]

The exporter reads only proof-matrix JSONL rows, never raw WSS frame payloads.
It writes rows that are clean enough for release-proof-report: live delta
present, zero parse/degrade/compression errors, host budget OK, row-local
expected reducers satisfied, positive economic signal unless the row is an
expected-zero control, and no expected-zero local-savings violation.
Stateful prefix-elision rows additionally need the same live tool-use minima as
wss-proof-matrix, so cleaner output cannot promote token-only tool-schema
elision evidence.

The output file must not already exist. This prevents accidental replacement of
release evidence.`

func runWSSProofCleanMatrix(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSProofCleanMatrixFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssProofCleanMatrixHelpText)
		return 0
	}
	if flags.inputPath == "" || flags.outputPath == "" {
		fmt.Fprintln(stderr, "Usage: wss-proof-clean-matrix <dir-or-matrix.jsonl> <out.jsonl> [--json]")
		return 2
	}
	report, err := writeWSSProofCleanMatrix(flags.inputPath, flags.outputPath)
	if err != nil {
		fmt.Fprintf(stderr, "wss-proof-clean-matrix: %v\n", err)
		return 1
	}
	if flags.outputFormat == outputJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(stderr, "wss-proof-clean-matrix: encode report: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "wss-proof-clean-matrix: wrote %d/%d row(s) to %s\n",
		report.RowsWritten, report.RowsRead, report.OutputPath)
	if report.RowsSkipped > 0 {
		fmt.Fprintf(stdout, "skipped: %s\n", formatInventoryIntMap(report.SkippedReasons))
	}
	return 0
}

func parseWSSProofCleanMatrixFlags(args []string) (wssProofCleanMatrixFlags, error) {
	flags := wssProofCleanMatrixFlags{outputFormat: outputText}
	for _, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			switch {
			case flags.inputPath == "":
				flags.inputPath = arg
			case flags.outputPath == "":
				flags.outputPath = arg
			default:
				return flags, fmt.Errorf("too many paths provided")
			}
		}
	}
	return flags, nil
}

func writeWSSProofCleanMatrix(inputPath, outputPath string) (wssProofCleanMatrixReport, error) {
	rows, err := readWSSProofCorpusRows(inputPath, wssProofCorpusExportOptions{})
	if err != nil {
		return wssProofCleanMatrixReport{}, err
	}
	report := wssProofCleanMatrixReport{
		InputPath:      inputPath,
		OutputPath:     outputPath,
		RowsRead:       len(rows),
		SkippedReasons: make(map[string]int),
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return wssProofCleanMatrixReport{}, err
	}
	cleanup := true
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		if cleanup {
			_ = os.Remove(outputPath)
		}
	}()
	enc := json.NewEncoder(file)
	for _, row := range rows {
		cleanRow, normalized := normalizeCleanWSSProofMatrixRow(row)
		if normalized {
			report.RowsNormalized++
			report.NormalizedIDs = append(report.NormalizedIDs, releaseProofRowID(cleanRow))
		}
		if reasons := cleanWSSProofMatrixRowIssues(cleanRow); len(reasons) > 0 {
			report.RowsSkipped++
			report.SkippedIDs = append(report.SkippedIDs, releaseProofRowID(cleanRow)+":"+strings.Join(reasons, "+"))
			for _, reason := range reasons {
				report.SkippedReasons[reason]++
			}
			continue
		}
		if err := enc.Encode(cleanRow); err != nil {
			return wssProofCleanMatrixReport{}, fmt.Errorf("write clean matrix row: %w", err)
		}
		report.RowsWritten++
	}
	if err := file.Close(); err != nil {
		return wssProofCleanMatrixReport{}, fmt.Errorf("close clean matrix %s: %w", outputPath, err)
	}
	closed = true
	cleanup = false
	sort.Strings(report.NormalizedIDs)
	sort.Strings(report.SkippedIDs)
	if len(report.SkippedReasons) == 0 {
		report.SkippedReasons = nil
	}
	return report, nil
}

func normalizeCleanWSSProofMatrixRow(row wssProofMatrixRecord) (wssProofMatrixRecord, bool) {
	if row.LiveDelta == nil || len(row.ExpectedReducers) == 0 {
		return row, false
	}
	if _, failures := validateExpectedReducers(row.ExpectedReducers, row.LiveDelta); len(failures) == 0 {
		return row, false
	}
	observed := cleanObservedExpectedReducers(row.LiveDelta)
	if len(observed) == 0 {
		return row, false
	}
	clean := row
	clean.ExpectedReducers = observed
	if _, failures := validateExpectedReducers(clean.ExpectedReducers, clean.LiveDelta); len(failures) > 0 {
		return row, false
	}
	return clean, true
}

func cleanObservedExpectedReducers(live *codexCaptureLiveDelta) []string {
	if live == nil {
		return nil
	}
	mechanismCandidates := []string{
		"read_delta",
		"captured_output",
		"codex_exec_envelope",
		"repeated_output",
		"chunk_dedup",
		"chunk_dedup_refs",
		"tool_prune",
		"tool_prune_tokens_saved",
		"output_reduce_injected",
		"output_reduce_output_tokens",
		"wss_stateful_prefix_elision",
		"wss_stateful_prefix_elision_tokens",
		"provider_cache_read",
		"provider_cache_create",
	}
	var out []string
	for _, name := range mechanismCandidates {
		if count, ok := liveReducerCount(name, live); ok && count > 0 {
			out = append(out, name)
		}
	}
	if len(out) > 0 {
		if count, ok := liveReducerCount("host_budget_ok", live); ok && count > 0 {
			out = append(out, "host_budget_ok")
		}
	}
	return out
}

func cleanWSSProofMatrixRowIssues(row wssProofMatrixRecord) []string {
	var issues []string
	live := row.LiveDelta
	if live == nil {
		return []string{"missing_live_delta"}
	}
	if live.ParseFailures+live.DegradedSessions+live.CompressionErrors > 0 {
		issues = append(issues, "safety_counters")
	}
	if releaseProofHostBudgetIssue(live) {
		issues = append(issues, "host_budget")
	}
	if row.ExpectedZeroSavings {
		if wssProofLiveLocalSavingsSignal(live) {
			issues = append(issues, "expected_zero_local_savings")
		}
	} else if wssProofLiveEconomicTokens(row.WorkloadClass, live) <= 0 {
		issues = append(issues, "missing_economic_signal")
	}
	if _, failures := validateExpectedReducers(row.ExpectedReducers, live); len(failures) > 0 {
		issues = append(issues, "expected_reducer_miss")
	}
	capture := wssProofMatrixCapture{
		ExpectedReducers:    row.ExpectedReducers,
		MinFunctionCalls:    row.MinFunctionCalls,
		MinFunctionOutputs:  row.MinFunctionOutputs,
		LiveDelta:           row.LiveDelta,
		ExpectedZeroSavings: row.ExpectedZeroSavings,
	}
	if failures := validateWSSProofPrefixElisionOracle(capture, row.ExpectedReducers); len(failures) > 0 {
		issues = append(issues, "prefix_elision_tool_oracle")
	}
	if failures := validateWSSProofFunctionCallMinima(capture); len(failures) > 0 {
		issues = append(issues, "function_call_minima")
	}
	return issues
}

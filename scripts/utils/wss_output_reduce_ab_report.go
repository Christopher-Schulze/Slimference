package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

type outputReduceABFlags struct {
	path          string
	outputFormat  string
	minNetTokens  int64
	minSavingsPct float64
	help          bool
}

type outputReduceABReport struct {
	Path         string               `json:"path"`
	Pairs        []outputReduceABPair `json:"pairs"`
	PairCount    int                  `json:"pair_count"`
	GatePassed   bool                 `json:"gate_passed"`
	GateFailures []string             `json:"gate_failures,omitempty"`
}

type outputReduceABPair struct {
	PairID                 string   `json:"pair_id"`
	Client                 string   `json:"client"`
	WorkloadClass          string   `json:"workload_class"`
	BaselineRowID          string   `json:"baseline_row_id"`
	DirectiveRowID         string   `json:"directive_row_id"`
	BaselineOutputTokens   int64    `json:"baseline_output_tokens"`
	DirectiveOutputTokens  int64    `json:"directive_output_tokens"`
	DirectiveInputOverhead int64    `json:"directive_input_overhead_tokens"`
	OutputTokensSaved      int64    `json:"output_tokens_saved"`
	NetTokensSaved         int64    `json:"net_tokens_saved"`
	OutputSavingsPct       float64  `json:"output_savings_pct"`
	GatePassed             bool     `json:"gate_passed"`
	GateFailures           []string `json:"gate_failures,omitempty"`
}

const outputReduceABHelpText = `wss-output-reduce-ab-report: verify paired output-reduce A/B proof rows

Usage:
  go run ./scripts/utils wss-output-reduce-ab-report <matrix.jsonl> [--json] [--min-net-tokens=N] [--min-output-savings-pct=PCT]

Rows are paired by optional proof-matrix fields ab_pair_id and ab_variant
(baseline|directive). The report reads only content-free matrix counters. It
does not read prompts, frames, command output, or model text. A passing pair
requires baseline and directive provider output-token observations, directive
output-reduce injection, zero safety errors, host budget OK when reported, and
positive net tokens after subtracting directive input overhead.`

func runOutputReduceABReport(args []string, stdout, stderr io.Writer) int {
	flags, err := parseOutputReduceABFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, outputReduceABHelpText)
		return 0
	}
	report, err := loadOutputReduceABReport(flags)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if flags.outputFormat == outputJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if !report.GatePassed {
			return 3
		}
		return 0
	}
	writeOutputReduceABText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseOutputReduceABFlags(args []string) (outputReduceABFlags, error) {
	flags := outputReduceABFlags{outputFormat: outputText, minNetTokens: 1}
	for _, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case strings.HasPrefix(arg, "--min-net-tokens="):
			value, err := parseProofInt64Flag(arg, "--min-net-tokens=")
			if err != nil {
				return flags, err
			}
			flags.minNetTokens = value
		case strings.HasPrefix(arg, "--min-output-savings-pct="):
			value, err := parseProofFloatFlag(arg, "--min-output-savings-pct=")
			if err != nil {
				return flags, err
			}
			flags.minSavingsPct = value
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple proof matrix files provided")
			}
			flags.path = arg
		}
	}
	if !flags.help && strings.TrimSpace(flags.path) == "" {
		return flags, fmt.Errorf("Usage: wss-output-reduce-ab-report <matrix.jsonl> [--json]")
	}
	return flags, nil
}

func parseProofInt64Flag(arg, prefix string) (int64, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(arg, prefix))
	if raw == "" {
		return 0, fmt.Errorf("%s requires a value", strings.TrimSuffix(prefix, "="))
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s requires a non-negative integer", strings.TrimSuffix(prefix, "="))
	}
	return value, nil
}

func parseProofFloatFlag(arg, prefix string) (float64, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(arg, prefix))
	if raw == "" {
		return 0, fmt.Errorf("%s requires a value", strings.TrimSuffix(prefix, "="))
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s requires a non-negative finite number", strings.TrimSuffix(prefix, "="))
	}
	return value, nil
}

func loadOutputReduceABReport(flags outputReduceABFlags) (outputReduceABReport, error) {
	rows, err := readWSSProofMatrixRecords(flags.path)
	if err != nil {
		return outputReduceABReport{}, err
	}
	groups := map[string]map[string]wssProofMatrixRecord{}
	for _, row := range rows {
		pairID := strings.TrimSpace(row.ABPairID)
		variant := strings.ToLower(strings.TrimSpace(row.ABVariant))
		if pairID == "" && variant == "" {
			continue
		}
		if groups[pairID] == nil {
			groups[pairID] = map[string]wssProofMatrixRecord{}
		}
		groups[pairID][variant] = row
	}
	report := outputReduceABReport{Path: flags.path, GatePassed: true}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		pair := evaluateOutputReduceABPair(id, groups[id], flags)
		report.Pairs = append(report.Pairs, pair)
		if !pair.GatePassed {
			for _, failure := range pair.GateFailures {
				report.GateFailures = append(report.GateFailures, fmt.Sprintf("%s: %s", id, failure))
			}
		}
	}
	report.PairCount = len(report.Pairs)
	if report.PairCount == 0 {
		report.GateFailures = append(report.GateFailures, "no output-reduce A/B pairs found")
	}
	report.GatePassed = len(report.GateFailures) == 0
	return report, nil
}

func evaluateOutputReduceABPair(id string, rows map[string]wssProofMatrixRecord, flags outputReduceABFlags) outputReduceABPair {
	baseline, hasBaseline := rows["baseline"]
	directive, hasDirective := rows["directive"]
	pair := outputReduceABPair{PairID: id, GatePassed: true}
	if hasBaseline {
		pair.Client = normalizeProofClient(baseline.Client)
		pair.WorkloadClass = strings.TrimSpace(baseline.WorkloadClass)
		pair.BaselineRowID = releaseProofRowID(baseline)
		if baseline.LiveDelta != nil {
			pair.BaselineOutputTokens = outputReduceABObservedOutputTokens(baseline.LiveDelta)
		}
	}
	if hasDirective {
		if pair.Client == "" {
			pair.Client = normalizeProofClient(directive.Client)
		}
		if pair.WorkloadClass == "" {
			pair.WorkloadClass = strings.TrimSpace(directive.WorkloadClass)
		}
		pair.DirectiveRowID = releaseProofRowID(directive)
		if directive.LiveDelta != nil {
			pair.DirectiveOutputTokens = outputReduceABObservedOutputTokens(directive.LiveDelta)
			pair.DirectiveInputOverhead = directive.LiveDelta.OutputReduceInputOverheadTokens
		}
	}
	if !hasBaseline {
		pair.GateFailures = append(pair.GateFailures, "missing baseline row")
	}
	if !hasDirective {
		pair.GateFailures = append(pair.GateFailures, "missing directive row")
	}
	if hasBaseline && hasDirective {
		pair.GateFailures = append(pair.GateFailures, validateOutputReduceABRows(baseline, directive)...)
	}
	pair.OutputTokensSaved = pair.BaselineOutputTokens - pair.DirectiveOutputTokens
	pair.NetTokensSaved = pair.OutputTokensSaved - pair.DirectiveInputOverhead
	if pair.BaselineOutputTokens > 0 {
		pair.OutputSavingsPct = float64(pair.OutputTokensSaved) / float64(pair.BaselineOutputTokens) * 100
	}
	if pair.OutputTokensSaved <= 0 {
		pair.GateFailures = append(pair.GateFailures, fmt.Sprintf("output_tokens_saved=%d <= 0", pair.OutputTokensSaved))
	}
	if pair.NetTokensSaved < flags.minNetTokens {
		pair.GateFailures = append(pair.GateFailures, fmt.Sprintf("net_tokens_saved=%d < min=%d", pair.NetTokensSaved, flags.minNetTokens))
	}
	if flags.minSavingsPct > 0 && pair.OutputSavingsPct+1e-9 < flags.minSavingsPct {
		pair.GateFailures = append(pair.GateFailures, fmt.Sprintf("output_savings_pct=%.2f < min=%.2f", pair.OutputSavingsPct, flags.minSavingsPct))
	}
	pair.GatePassed = len(pair.GateFailures) == 0
	return pair
}

func validateOutputReduceABRows(baseline, directive wssProofMatrixRecord) []string {
	var failures []string
	if strings.TrimSpace(baseline.WorkloadClass) != strings.TrimSpace(directive.WorkloadClass) {
		failures = append(failures, "baseline/directive workload_class mismatch")
	}
	if normalizeProofClient(baseline.Client) != normalizeProofClient(directive.Client) {
		failures = append(failures, "baseline/directive client mismatch")
	}
	failures = append(failures, validateOutputReduceABLive("baseline", baseline.LiveDelta, false)...)
	failures = append(failures, validateOutputReduceABLive("directive", directive.LiveDelta, true)...)
	return failures
}

func validateOutputReduceABLive(label string, live *codexCaptureLiveDelta, wantInjected bool) []string {
	var failures []string
	if live == nil {
		return []string{label + " missing live_delta"}
	}
	if outputReduceABObservedOutputTokens(live) <= 0 {
		failures = append(failures, fmt.Sprintf("%s missing observed output tokens", label))
	}
	if wantInjected && live.OutputReduceInjected <= 0 {
		failures = append(failures, "directive missing output_reduce_injected")
	}
	if wantInjected && live.OutputReduceInjected > 0 && live.OutputReduceInputOverheadTokens <= 0 {
		failures = append(failures, "directive missing positive output_reduce_input_overhead_tokens")
	}
	if !wantInjected && live.OutputReduceInjected > 0 {
		failures = append(failures, "baseline unexpectedly has output_reduce_injected")
	}
	if live.OutputReduceDowngrades > 0 {
		failures = append(failures, fmt.Sprintf("%s has output-reduce downgrade/repair signal", label))
	}
	if safety := live.ParseFailures + live.DegradedSessions + live.CompressionErrors; safety > 0 {
		failures = append(failures, fmt.Sprintf("%s safety counters non-zero: parse=%d degraded=%d compression_errors=%d",
			label, live.ParseFailures, live.DegradedSessions, live.CompressionErrors))
	}
	if live.HostBudgetStatus != "" && (live.HostBudgetStatus != "ok" || live.HostBudgetExceeded || !live.HostBudgetCompressionOK || !live.HostBudgetDegradationOK) {
		failures = append(failures, fmt.Sprintf("%s host budget not ok: status=%s exceeded=%t", label, live.HostBudgetStatus, live.HostBudgetExceeded))
	}
	return failures
}

func outputReduceABObservedOutputTokens(live *codexCaptureLiveDelta) int64 {
	if live == nil {
		return 0
	}
	if live.ProviderOutputTokens > 0 {
		return live.ProviderOutputTokens
	}
	return live.OutputReduceOutputTokensObserved
}

func writeOutputReduceABText(w io.Writer, report outputReduceABReport) {
	fmt.Fprintf(w, "Output-reduce A/B report: %s\n", report.Path)
	fmt.Fprintf(w, "  pairs: %d\n", report.PairCount)
	for _, pair := range report.Pairs {
		fmt.Fprintf(w, "  - %s %s/%s baseline=%d directive=%d overhead=%d saved=%d net=%d savings=%.2f%% gate=%s\n",
			pair.PairID,
			pair.Client,
			pair.WorkloadClass,
			pair.BaselineOutputTokens,
			pair.DirectiveOutputTokens,
			pair.DirectiveInputOverhead,
			pair.OutputTokensSaved,
			pair.NetTokensSaved,
			pair.OutputSavingsPct,
			passFail(pair.GatePassed))
		for _, failure := range pair.GateFailures {
			fmt.Fprintf(w, "      ! %s\n", failure)
		}
	}
	fmt.Fprintf(w, "  gate: %s\n", passFail(report.GatePassed))
	for _, failure := range report.GateFailures {
		fmt.Fprintf(w, "    ! %s\n", failure)
	}
}

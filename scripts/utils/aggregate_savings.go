package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/control"
)

type aggregateSavingsFlags struct {
	adminStateURL  string
	adminStateFile string
	filterDB       string
	period         string
	usdPerMTokens  float64
	outputFormat   string
	help           bool
}

type aggregateWSSBlock struct {
	PhasefBridged             int64 `json:"phasef_bridged"`
	CompressedMessagesMutated int64 `json:"compressed_messages_mutated"`
	FramesReencoded           int64 `json:"frames_reencoded"`
	PhasefMutations           int64 `json:"phasef_mutations"`
	InputTokensSaved          int64 `json:"input_tokens_saved"`
	ParseFailures             int64 `json:"parse_failures"`
	DegradedSessions          int64 `json:"degraded_sessions"`
	CompressionErrors         int64 `json:"compression_errors"`
	MutationActive            bool  `json:"mutation_active"`
	ByteBridgeOnly            bool  `json:"byte_bridge_only"`
}

type aggregateOutputReduceBlock struct {
	RepdetRewrites      int64 `json:"repdet_rewrites"`
	RepdetBytesSaved    int64 `json:"repdet_bytes_saved"`
	StaleReadBlocks     int64 `json:"stale_read_blocks"`
	ObsoletePruneBlocks int64 `json:"obsolete_prune_blocks"`
	StopSeqInjections   int64 `json:"stop_seq_injections"`
	BeterseInjections   int64 `json:"beterse_injections"`
	StreamcutFires      int64 `json:"streamcut_fires"`
}

type aggregateTotalsBlock struct {
	WSSInputTokensSaved     int64   `json:"wss_input_tokens_saved"`
	Layer0FilterTokensSaved int64   `json:"layer0_filter_tokens_saved"`
	TotalTokensSaved        int64   `json:"total_tokens_saved"`
	EstUSDSaved             float64 `json:"estimated_usd_saved,omitempty"`
}

type aggregateSavingsReport struct {
	Source       string                       `json:"source"`
	Generated    time.Time                    `json:"generated"`
	WSS          aggregateWSSBlock            `json:"wss"`
	OutputReduce aggregateOutputReduceBlock   `json:"output_reduce"`
	FilterLayer0 *analytics.FilterGainReport  `json:"filter_layer0,omitempty"`
	Aggregate    aggregateTotalsBlock         `json:"aggregate"`
	Notes        []string                     `json:"notes"`
}

const aggregateSavingsHelpText = `aggregate-savings: live + offline Slimference savings honest aggregate report

Usage:
  go run ./scripts/utils aggregate-savings [flags]

Flags:
  --admin-url=<url>            Admin state endpoint (default http://127.0.0.1:8990/_slimference/admin/state)
  --admin-state-file=<path>    Read admin state from JSON file instead (offline mode, takes precedence)
  --filter-db=<path>           Filter SQLite DB (e.g. ~/.slimference/analytics/filter.db)
  --period=<all|today|week|month>  Filter DB period (default all)
  --usd-per-million=<float>    USD cost per million tokens for aggregate estimate
  --json                       Output as JSON
  --help                       Show this help

This tool gives an honest, single-glance picture of every measurable Slimference
savings source for one daemon, without conflating route-ready with savings-proven.
WSS counters are live (daemon admin/state); filter Layer-0 savings come from the
SQLite analytics DB if a path is provided.`

func runAggregateSavings(args []string, stdout, stderr io.Writer) int {
	flags, err := parseAggregateSavingsFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, aggregateSavingsHelpText)
		return 0
	}
	state, src, err := loadAdminState(flags)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	report := buildAggregateSavingsReport(state, src, flags, time.Now().UTC())
	if flags.outputFormat == outputJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	writeAggregateSavingsText(stdout, report)
	return 0
}

func parseAggregateSavingsFlags(args []string) (aggregateSavingsFlags, error) {
	f := aggregateSavingsFlags{
		adminStateURL: "http://127.0.0.1:8990/_slimference/admin/state",
		period:        "all",
		outputFormat:  outputText,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--json":
			f.outputFormat = outputJSON
		case strings.HasPrefix(a, "--admin-url="):
			f.adminStateURL = strings.TrimPrefix(a, "--admin-url=")
		case strings.HasPrefix(a, "--admin-state-file="):
			f.adminStateFile = strings.TrimPrefix(a, "--admin-state-file=")
		case strings.HasPrefix(a, "--filter-db="):
			f.filterDB = strings.TrimPrefix(a, "--filter-db=")
		case strings.HasPrefix(a, "--period="):
			f.period = strings.TrimPrefix(a, "--period=")
		case strings.HasPrefix(a, "--usd-per-million="):
			v := strings.TrimPrefix(a, "--usd-per-million=")
			n, err := strconv.ParseFloat(v, 64)
			if err != nil || n < 0 {
				return f, fmt.Errorf("--usd-per-million must be a non-negative number")
			}
			f.usdPerMTokens = n
		default:
			return f, fmt.Errorf("unknown flag %q", a)
		}
	}
	if !validAggregatePeriod(f.period) {
		return f, fmt.Errorf("--period must be one of: all, today, week, month")
	}
	return f, nil
}

func validAggregatePeriod(p string) bool {
	switch p {
	case "all", "today", "week", "month":
		return true
	default:
		return false
	}
}

var aggregateHTTPGet = http.Get

func loadAdminState(flags aggregateSavingsFlags) (control.SetupState, string, error) {
	if flags.adminStateFile != "" {
		data, err := os.ReadFile(flags.adminStateFile)
		if err != nil {
			return control.SetupState{}, "", fmt.Errorf("read admin state file %s: %w", flags.adminStateFile, err)
		}
		state, err := parseAdminStateJSON(data)
		if err != nil {
			return control.SetupState{}, "", err
		}
		return state, "file:" + flags.adminStateFile, nil
	}
	url := flags.adminStateURL
	if url == "" {
		return control.SetupState{}, "", fmt.Errorf("--admin-url or --admin-state-file required")
	}
	resp, err := aggregateHTTPGet(url)
	if err != nil {
		return control.SetupState{}, "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return control.SetupState{}, "", fmt.Errorf("admin state returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return control.SetupState{}, "", fmt.Errorf("read admin state body: %w", err)
	}
	state, err := parseAdminStateJSON(data)
	if err != nil {
		return control.SetupState{}, "", err
	}
	return state, "url:" + url, nil
}

func parseAdminStateJSON(data []byte) (control.SetupState, error) {
	var state control.SetupState
	if err := json.Unmarshal(data, &state); err != nil {
		return control.SetupState{}, fmt.Errorf("parse admin state JSON: %w", err)
	}
	return state, nil
}

func buildAggregateSavingsReport(state control.SetupState, source string, flags aggregateSavingsFlags, now time.Time) aggregateSavingsReport {
	report := aggregateSavingsReport{
		Source:    source,
		Generated: now,
		WSS: aggregateWSSBlock{
			PhasefBridged:             state.WSS.PhasefBridged,
			CompressedMessagesMutated: state.WSS.CompressedMessagesMutated,
			FramesReencoded:           state.WSS.FramesReencoded,
			PhasefMutations:           state.WSS.PhaseFMutations,
			InputTokensSaved:          state.Savings.InputTokensSaved,
			ParseFailures:             state.WSS.ParseFailures,
			DegradedSessions:          state.WSS.DegradedSessions,
			CompressionErrors:         state.WSS.CompressionErrors,
			MutationActive:            state.WSS.MutationActive,
			ByteBridgeOnly:            state.WSS.ByteBridgeOnly,
		},
		OutputReduce: aggregateOutputReduceBlock{
			RepdetRewrites:      state.Savings.RepdetRewrites,
			RepdetBytesSaved:    state.Savings.RepdetBytesSaved,
			StaleReadBlocks:     state.Savings.StaleReadBlocks,
			ObsoletePruneBlocks: state.Savings.ObsoletePruneBlocks,
			StopSeqInjections:   state.Savings.StopSeqInjections,
			BeterseInjections:   state.Savings.BeterseInjections,
			StreamcutFires:      state.Savings.StreamcutFires,
		},
	}
	if flags.filterDB != "" {
		fr, ferr := analytics.QueryFilterGainReport(flags.filterDB, flags.period, now, false, "", flags.usdPerMTokens)
		if ferr == nil {
			report.FilterLayer0 = &fr
		} else {
			report.Notes = append(report.Notes, fmt.Sprintf("filter db unavailable: %v", ferr))
		}
	}
	report.Aggregate.WSSInputTokensSaved = report.WSS.InputTokensSaved
	if report.FilterLayer0 != nil {
		report.Aggregate.Layer0FilterTokensSaved = report.FilterLayer0.TokensSavedEst
	}
	report.Aggregate.TotalTokensSaved = report.Aggregate.WSSInputTokensSaved + report.Aggregate.Layer0FilterTokensSaved
	if flags.usdPerMTokens > 0 {
		report.Aggregate.EstUSDSaved = float64(report.Aggregate.TotalTokensSaved) * flags.usdPerMTokens / 1_000_000
	}
	report.Notes = append(report.Notes,
		"WSS input_tokens_saved is from the live RecordProxyLayer0 path (read-delta + L0 filter chain).",
		"WSS savings are workload-dependent: low without repeat-read sessions, large with them.",
		"Filter Layer-0 savings cover non-WSS HTTP-path Codex hook traffic (offline SQLite).",
		"byte_bridge_only=true means the current daemon has only bridged byte-equal so far (no mutation observed yet).",
	)
	return report
}

func writeAggregateSavingsText(w io.Writer, report aggregateSavingsReport) {
	fmt.Fprintln(w, "=== Slimference Aggregate Savings ===")
	fmt.Fprintf(w, "Source:    %s\n", report.Source)
	fmt.Fprintf(w, "Generated: %s\n", report.Generated.Format(time.RFC3339))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "WSS Phase-F (live counters):")
	fmt.Fprintf(w, "  phasef_bridged sessions:      %d\n", report.WSS.PhasefBridged)
	fmt.Fprintf(w, "  frames_reencoded:             %d\n", report.WSS.FramesReencoded)
	fmt.Fprintf(w, "  compressed_messages_mutated:  %d\n", report.WSS.CompressedMessagesMutated)
	fmt.Fprintf(w, "  phasef_mutations:             %d\n", report.WSS.PhasefMutations)
	fmt.Fprintf(w, "  mutation_active:              %v\n", report.WSS.MutationActive)
	fmt.Fprintf(w, "  byte_bridge_only:             %v\n", report.WSS.ByteBridgeOnly)
	fmt.Fprintf(w, "  input_tokens_saved:           %d\n", report.WSS.InputTokensSaved)
	if report.WSS.ParseFailures+report.WSS.DegradedSessions+report.WSS.CompressionErrors > 0 {
		fmt.Fprintf(w, "  HEALTH WARN parse=%d degraded=%d compression_errors=%d\n",
			report.WSS.ParseFailures, report.WSS.DegradedSessions, report.WSS.CompressionErrors)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Output-Reduce sub-layers (live counters):")
	fmt.Fprintf(w, "  repdet_rewrites:       %d (bytes saved: %d)\n", report.OutputReduce.RepdetRewrites, report.OutputReduce.RepdetBytesSaved)
	fmt.Fprintf(w, "  stale_read_blocks:     %d\n", report.OutputReduce.StaleReadBlocks)
	fmt.Fprintf(w, "  obsolete_prune_blocks: %d\n", report.OutputReduce.ObsoletePruneBlocks)
	fmt.Fprintf(w, "  stop_seq_injections:   %d\n", report.OutputReduce.StopSeqInjections)
	fmt.Fprintf(w, "  beterse_injections:    %d\n", report.OutputReduce.BeterseInjections)
	fmt.Fprintf(w, "  streamcut_fires:       %d\n", report.OutputReduce.StreamcutFires)
	fmt.Fprintln(w)

	if report.FilterLayer0 != nil {
		fmt.Fprintf(w, "HTTP-path Layer-0 filter (period=%s):\n", report.FilterLayer0.Period)
		fmt.Fprintf(w, "  runs:               %d\n", report.FilterLayer0.Runs)
		fmt.Fprintf(w, "  input_tokens:       %d\n", report.FilterLayer0.InputTokens)
		fmt.Fprintf(w, "  output_tokens:      %d\n", report.FilterLayer0.OutputTokens)
		fmt.Fprintf(w, "  tokens_saved_est:   %d\n", report.FilterLayer0.TokensSavedEst)
		if report.FilterLayer0.SavingsUsdEst > 0 {
			fmt.Fprintf(w, "  estimated USD:      %.4f\n", report.FilterLayer0.SavingsUsdEst)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "HTTP-path Layer-0 filter: not loaded (pass --filter-db=<path>)")
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "Aggregate:")
	fmt.Fprintf(w, "  WSS input tokens saved:        %d\n", report.Aggregate.WSSInputTokensSaved)
	fmt.Fprintf(w, "  Filter Layer-0 tokens saved:   %d\n", report.Aggregate.Layer0FilterTokensSaved)
	fmt.Fprintf(w, "  TOTAL tokens saved:            %d\n", report.Aggregate.TotalTokensSaved)
	if report.Aggregate.EstUSDSaved > 0 {
		fmt.Fprintf(w, "  Estimated USD saved:           %.4f\n", report.Aggregate.EstUSDSaved)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Notes:")
	for _, note := range report.Notes {
		fmt.Fprintf(w, "  - %s\n", note)
	}
}

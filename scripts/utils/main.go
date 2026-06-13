// Package main implements the scripts/utils tool for offline session analysis.
//
// Usage:
//
//	go run ./scripts/utils session-report <file.jsonl> [--json|--csv]
//	go run ./scripts/utils decision-report <file.jsonl> [--json|--csv]
//	go run ./scripts/utils filter-report <filter.db> [--json|--csv]
//	go run ./scripts/utils combined-report <analytics.jsonl> <decisions.jsonl> <filter.db> [--json|--csv]
//	go run ./scripts/utils aggregate-savings [--admin-url=... | --admin-state-file=...] [--filter-db=...] [--json]
//	go run ./scripts/utils workday-savings <start|finish> [--baseline-file=...] [--json]
//	go run ./scripts/utils codex-capture-run [flags] -- <codex run args...>
//	go run ./scripts/utils wss-audit <decisions.jsonl> [--json]
//	go run ./scripts/utils wss-local-gap <decisions.jsonl> [--json] [--since=<rfc3339>] [--min-local-ratio=<ratio>] [--min-local-saved=<tokens>]
//	go run ./scripts/utils wss-ab-replay <frames.jsonl> [--json|--fail-on-lost|--fail-on-upstream-error|--archive-recovery-note|--tool-output-mutation|--delta-tool-output-mutation-lab|--codex-chunk-dedup]
//	go run ./scripts/utils wss-proof-matrix <captures.jsonl> [--json] [--require-live-token-delta]
//	go run ./scripts/utils wss-proof-inventory <dir-or-matrix.jsonl> [--json]
//	go run ./scripts/utils wss-proof-export-corpus <matrix.jsonl> <live-corpus-root>
//	go run ./scripts/utils wss-proof-clean-matrix <dir-or-matrix.jsonl> <out.jsonl> [--json]
//	go run ./scripts/utils wss-proof-live-row --matrix-row PATH --frames PATH --workload-class CLASS
//	go run ./scripts/utils wss-output-reduce-ab-report <matrix.jsonl> [--json]
//	go run ./scripts/utils search-cap-profile (--command CMD --input stdout.txt | --frames frames.jsonl) [--candidate files:matches...] [--json]
//	go run ./scripts/utils search-cap-proof --frames frames.jsonl --candidate files:matches... [--json]
//	go run ./scripts/utils release-proof-report <clean-release-matrix.jsonl> [--json] --resource-profile-proof DIR --resource-profile-proof DIR
//	go run ./scripts/utils local-artifact-hygiene [--json|--clean]
//	go run ./scripts/utils tls-probe [--profile=<name>] [--json]
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./scripts/utils <subcommand> <path>")
		fmt.Fprintln(os.Stderr, "Subcommands: session-report, decision-report, filter-report, combined-report, aggregate-savings, workday-savings, codex-capture-run, wss-audit, wss-local-gap, wss-ab-replay, wss-proof-matrix, wss-proof-inventory, wss-proof-export-corpus, wss-proof-clean-matrix, wss-proof-live-row, wss-output-reduce-ab-report, search-cap-profile, search-cap-proof, release-proof-report, local-artifact-hygiene, tls-probe")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "session-report":
		outputFormat, rest, err := parseOutputFlag(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "Usage: session-report <analytics.jsonl> [--json|--csv]")
			os.Exit(1)
		}
		if err := sessionReport(rest[0], outputFormat); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "decision-report":
		outputFormat, rest, err := parseOutputFlag(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "Usage: decision-report <decisions.jsonl> [--json|--csv]")
			os.Exit(1)
		}
		if err := decisionReport(rest[0], outputFormat); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "filter-report":
		outputFormat, rest, err := parseOutputFlag(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "Usage: filter-report <filter.db> [--json|--csv]")
			os.Exit(1)
		}
		if err := filterReport(rest[0], outputFormat); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "combined-report":
		outputFormat, rest, err := parseOutputFlag(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(rest) != 3 {
			fmt.Fprintln(os.Stderr, "Usage: combined-report <analytics.jsonl> <decisions.jsonl> <filter.db> [--json|--csv]")
			os.Exit(1)
		}
		if err := combinedReport(rest[0], rest[1], rest[2], outputFormat); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "structure-accuracy":
		os.Exit(runStructureAccuracy(os.Args[2:], os.Stdout, os.Stderr))
	case "leaf-audit":
		os.Exit(runLeafAudit(os.Args[2:], os.Stdout, os.Stderr))
	case "aggregate-savings":
		os.Exit(runAggregateSavings(os.Args[2:], os.Stdout, os.Stderr))
	case "workday-savings":
		os.Exit(runWorkdaySavings(os.Args[2:], os.Stdout, os.Stderr))
	case "codex-capture-run":
		os.Exit(runCodexCaptureRun(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-audit":
		os.Exit(runWSSAudit(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-local-gap":
		os.Exit(runWSSLocalGap(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-ab-replay":
		os.Exit(runWSSABReplay(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-proof-matrix":
		os.Exit(runWSSProofMatrix(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-proof-inventory":
		os.Exit(runWSSProofInventory(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-proof-export-corpus":
		os.Exit(runWSSProofExportCorpus(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-proof-clean-matrix":
		os.Exit(runWSSProofCleanMatrix(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-proof-live-row":
		os.Exit(runWSSProofLiveRow(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-output-reduce-ab-report":
		os.Exit(runOutputReduceABReport(os.Args[2:], os.Stdout, os.Stderr))
	case "search-cap-profile":
		os.Exit(runSearchCapProfile(os.Args[2:], os.Stdout, os.Stderr))
	case "search-cap-proof":
		os.Exit(runSearchCapProof(os.Args[2:], os.Stdout, os.Stderr))
	case "release-proof-report":
		os.Exit(runReleaseProofReport(os.Args[2:], os.Stdout, os.Stderr))
	case "local-artifact-hygiene":
		os.Exit(runLocalArtifactHygiene(os.Args[2:], os.Stdout, os.Stderr))
	case "tls-probe":
		os.Exit(runTLSProbe(os.Args[2:], os.Stdout, os.Stderr))
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n", os.Args[1])
		os.Exit(1)
	}
}

const (
	outputText = "text"
	outputJSON = "json"
	outputCSV  = "csv"
)

func parseOutputFlag(args []string) (outputFormat string, rest []string, err error) {
	outputFormat = outputText
	for _, arg := range args {
		switch arg {
		case "--json":
			if outputFormat != outputText {
				return "", nil, fmt.Errorf("multiple output flags provided")
			}
			outputFormat = outputJSON
		case "--csv":
			if outputFormat != outputText {
				return "", nil, fmt.Errorf("multiple output flags provided")
			}
			outputFormat = outputCSV
		case "":
			continue
		default:
			if strings.HasPrefix(arg, "-") {
				return "", nil, fmt.Errorf("unknown flag: %s", arg)
			}
			rest = append(rest, arg)
		}
	}
	return outputFormat, rest, nil
}

// --- session-report: parses analytics JSONL files ---

type sessionStats struct {
	firstTS, lastTS time.Time
	totalRequests   int
	origTokens      int
	compTokens      int
	outputTokens    int
	layer1Savings   int
	layer2Savings   int
	secretsFound    int
	errors          int
	retries         int
	byProvider      map[string]*providerStats
}

type providerStats struct {
	requests   int
	origTokens int
	compTokens int
	saved      int
}

type sessionReportOutput struct {
	Path           string                       `json:"path"`
	Source         string                       `json:"source"`
	FirstTimestamp time.Time                    `json:"first_timestamp,omitempty"`
	LastTimestamp  time.Time                    `json:"last_timestamp,omitempty"`
	TotalRequests  int                          `json:"total_requests"`
	OrigTokens     int                          `json:"orig_tokens"`
	CompTokens     int                          `json:"comp_tokens"`
	OutputTokens   int                          `json:"output_tokens"`
	Layer1Savings  int                          `json:"layer1_savings"`
	Layer2Savings  int                          `json:"layer2_savings"`
	SecretsFound   int                          `json:"secrets_found"`
	Errors         int                          `json:"errors"`
	Retries        int                          `json:"retries"`
	ByProvider     map[string]providerStatsView `json:"by_provider,omitempty"`
}

type providerStatsView struct {
	Requests   int     `json:"requests"`
	OrigTokens int     `json:"orig_tokens"`
	CompTokens int     `json:"comp_tokens"`
	Saved      int     `json:"saved"`
	RatioPct   float64 `json:"ratio_pct"`
}

func sessionReport(path string, outputFormat string) error {
	report, err := loadSessionReport(path)
	if err != nil {
		return err
	}
	if report.TotalRequests == 0 {
		fmt.Println("No request_processed events found in file.")
		return nil
	}

	switch outputFormat {
	case outputJSON:
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	case outputCSV:
		return writeSessionCSV(os.Stdout, report)
	}

	totalSaved := report.OrigTokens - report.CompTokens
	fmt.Printf("=== Session Report: %s ===\n", filepath.Base(path))
	if report.Source != "" {
		fmt.Printf("Source: %s\n", report.Source)
	}
	if !report.FirstTimestamp.IsZero() {
		fmt.Printf("Time range: %s to %s\n",
			report.FirstTimestamp.Format("15:04:05"), report.LastTimestamp.Format("15:04:05"))
	}
	fmt.Printf("Total Requests:     %d\n", report.TotalRequests)
	fmt.Printf("Input Tokens (orig): %s\n", formatNum(report.OrigTokens))
	fmt.Printf("Input Tokens (comp): %s\n", formatNum(report.CompTokens))
	fmt.Printf("Savings:            %s (%.1f%%)\n", formatNum(totalSaved), ratioPct(report.OrigTokens, totalSaved))
	fmt.Printf("Output Tokens:      %s\n", formatNum(report.OutputTokens))
	fmt.Printf("Secrets Detected:   %d\n", report.SecretsFound)
	fmt.Printf("Errors:             %d\n", report.Errors)
	fmt.Printf("Retries:            %d\n", report.Retries)

	fmt.Println("\nPer Provider:")
	for _, prov := range sortedProviderViewKeys(report.ByProvider) {
		ps := report.ByProvider[prov]
		fmt.Printf("  %-12s %d requests, %s saved (%.1f%%)\n",
			prov, ps.Requests, formatNum(ps.Saved), ps.RatioPct)
	}

	return nil
}

func loadSessionReport(path string) (sessionReportOutput, error) {
	f, err := os.Open(path)
	if err != nil {
		return sessionReportOutput{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	stats := &sessionStats{byProvider: make(map[string]*providerStats)}
	dec := json.NewDecoder(f)
	source := ""
	var latestSnapshot analytics.AnalyticsSnapshot
	var haveSnapshot bool

	for dec.More() {
		var envelope struct {
			Type      string          `json:"type"`
			Timestamp time.Time       `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := dec.Decode(&envelope); err != nil {
			continue
		}

		switch envelope.Type {
		case "request_processed":
			var ev types.AnalyticsEvent
			if err := json.Unmarshal(envelope.Payload, &ev); err != nil {
				continue
			}
			applyRequestProcessed(stats, envelope.Timestamp, ev)
			source = "request_processed"
		case "analytics_event":
			var ev types.AnalyticsEvent
			if err := json.Unmarshal(envelope.Payload, &ev); err != nil {
				continue
			}
			if ev.Type == types.EventRequestProcessed {
				applyRequestProcessed(stats, envelope.Timestamp, ev)
				source = "analytics_event"
				continue
			}
			switch ev.Type {
			case types.EventSecretDetected:
				stats.secretsFound += ev.SecretsFound
			case types.EventErrorOccurred:
				stats.errors++
			case types.EventRateLimitRetry, types.EventOverflowRetry:
				stats.retries++
			}
		case "session_snapshot":
			var snap analytics.AnalyticsSnapshot
			if err := json.Unmarshal(envelope.Payload, &snap); err != nil {
				continue
			}
			latestSnapshot = snap
			haveSnapshot = true
			if stats.firstTS.IsZero() || envelope.Timestamp.Before(stats.firstTS) {
				stats.firstTS = envelope.Timestamp
			}
			if envelope.Timestamp.After(stats.lastTS) {
				stats.lastTS = envelope.Timestamp
			}
		}
	}

	if stats.totalRequests == 0 && haveSnapshot {
		applySnapshot(stats, latestSnapshot)
		source = "session_snapshot"
	}

	return buildSessionReport(path, source, stats), nil
}

func applyRequestProcessed(stats *sessionStats, timestamp time.Time, ev types.AnalyticsEvent) {
	stats.totalRequests++
	stats.origTokens += ev.InputTokensOrig
	stats.compTokens += ev.InputTokensComp
	stats.outputTokens += ev.OutputTokens
	for _, l := range ev.Layers {
		switch l {
		case 1:
			stats.layer1Savings += ev.TokensSaved
		case 2, 3:
			stats.layer2Savings += ev.TokensSaved
		}
	}
	prov := ev.Provider.String()
	ps, ok := stats.byProvider[prov]
	if !ok {
		ps = &providerStats{}
		stats.byProvider[prov] = ps
	}
	ps.requests++
	ps.origTokens += ev.InputTokensOrig
	ps.compTokens += ev.InputTokensComp
	ps.saved += ev.InputTokensOrig - ev.InputTokensComp

	if stats.firstTS.IsZero() || timestamp.Before(stats.firstTS) {
		stats.firstTS = timestamp
	}
	if timestamp.After(stats.lastTS) {
		stats.lastTS = timestamp
	}
}

func applySnapshot(stats *sessionStats, snap analytics.AnalyticsSnapshot) {
	stats.totalRequests = snap.TotalRequests
	stats.origTokens = snap.TotalInputTokens
	stats.compTokens = snap.TotalInputTokens - snap.SavedInputTokens
	stats.outputTokens = snap.TotalOutputTokens
	stats.layer1Savings = snap.Layer1Savings
	stats.layer2Savings = snap.Layer2Savings
	stats.secretsFound = snap.SecretsRedacted
	stats.errors = snap.Errors
	stats.retries = snap.AutoRetries
	for provider, ps := range snap.PerProvider {
		copy := ps
		stats.byProvider[provider.String()] = &providerStats{
			requests:   copy.Messages,
			origTokens: copy.InputTokensOrig,
			compTokens: copy.InputTokensOrig - copy.InputTokensSaved,
			saved:      copy.InputTokensSaved,
		}
	}
}

func buildSessionReport(path, source string, stats *sessionStats) sessionReportOutput {
	out := sessionReportOutput{
		Path:           path,
		Source:         source,
		FirstTimestamp: stats.firstTS,
		LastTimestamp:  stats.lastTS,
		TotalRequests:  stats.totalRequests,
		OrigTokens:     stats.origTokens,
		CompTokens:     stats.compTokens,
		OutputTokens:   stats.outputTokens,
		Layer1Savings:  stats.layer1Savings,
		Layer2Savings:  stats.layer2Savings,
		SecretsFound:   stats.secretsFound,
		Errors:         stats.errors,
		Retries:        stats.retries,
		ByProvider:     make(map[string]providerStatsView, len(stats.byProvider)),
	}
	for name, ps := range stats.byProvider {
		ratioPct := 0.0
		if ps.origTokens > 0 {
			ratioPct = float64(ps.saved) / float64(ps.origTokens) * 100
		}
		out.ByProvider[name] = providerStatsView{
			Requests:   ps.requests,
			OrigTokens: ps.origTokens,
			CompTokens: ps.compTokens,
			Saved:      ps.saved,
			RatioPct:   ratioPct,
		}
	}
	return out
}

// --- decision-report: parses debug decision JSONL ---

type decisionReportOutput struct {
	Path             string         `json:"path"`
	Requests         int            `json:"requests"`
	InputTokensOrig  int            `json:"input_tokens_orig"`
	InputTokensFinal int            `json:"input_tokens_final"`
	TotalSaved       int            `json:"total_saved"`
	RatioPct         float64        `json:"ratio_pct"`
	SubLayerTotals   map[string]int `json:"sub_layer_totals,omitempty"`
}

func decisionReport(path string, outputFormat string) error {
	report, err := loadDecisionReport(path)
	if err != nil {
		return err
	}
	if report.Requests == 0 {
		fmt.Println("No request summaries found in file.")
		return nil
	}

	switch outputFormat {
	case outputJSON:
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	case outputCSV:
		return writeDecisionCSV(os.Stdout, report)
	}

	fmt.Printf("=== Decision Report: %s ===\n", filepath.Base(path))
	fmt.Printf("Requests analyzed:  %d\n", report.Requests)
	fmt.Printf("Input Tokens (orig): %s\n", formatNum(report.InputTokensOrig))
	fmt.Printf("Input Tokens (final): %s\n", formatNum(report.InputTokensFinal))
	fmt.Printf("Total Savings:      %s (%.1f%%)\n", formatNum(report.TotalSaved), report.RatioPct)

	if len(report.SubLayerTotals) > 0 {
		fmt.Println("\nPer Sub-Layer Breakdown:")
		for _, name := range sortedStringKeys(report.SubLayerTotals) {
			saved := report.SubLayerTotals[name]
			fmt.Printf("  %-25s %s tokens (%.1f%%)\n", name, formatNum(saved), ratioPct(report.TotalSaved, saved))
		}
	}

	return nil
}

func loadDecisionReport(path string) (decisionReportOutput, error) {
	f, err := os.Open(path)
	if err != nil {
		return decisionReportOutput{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var summaries []dbg.RequestSummary
	dec := json.NewDecoder(f)
	for dec.More() {
		var s dbg.RequestSummary
		if err := dec.Decode(&s); err != nil {
			continue
		}
		summaries = append(summaries, s)
	}

	totalOrig, totalFinal := 0, 0
	subLayerTotals := make(map[string]int) // sub-layer name -> total saved tokens

	for _, s := range summaries {
		totalOrig += s.Tokens.Original
		totalFinal += s.Tokens.Final
		for name, bd := range s.Layer1Breakdown {
			subLayerTotals[name] += bd.Saved
		}
	}

	totalSaved := totalOrig - totalFinal
	var ratio float64
	if totalOrig > 0 {
		ratio = float64(totalSaved) / float64(totalOrig) * 100
	}

	return decisionReportOutput{
		Path:             path,
		Requests:         len(summaries),
		InputTokensOrig:  totalOrig,
		InputTokensFinal: totalFinal,
		TotalSaved:       totalSaved,
		RatioPct:         ratio,
		SubLayerTotals:   subLayerTotals,
	}, nil
}

// --- filter-report: uses slimference gain infrastructure (requires filter.db) ---

func filterReport(path string, outputFormat string) error {
	report, err := loadFilterReport(path)
	if err != nil {
		return err
	}

	switch outputFormat {
	case outputJSON:
		data, err := analytics.FormatFilterGainReportJSON(report)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	case outputCSV:
		return writeFilterCSV(os.Stdout, report)
	}

	summary := report.FilterGainSummary
	fmt.Printf("=== Filter Report: %s ===\n", filepath.Base(path))
	fmt.Printf("Runs:                %d\n", summary.Runs)
	fmt.Printf("Input Tokens (est):  %s\n", formatNum(int(summary.InputTokens)))
	fmt.Printf("Output Tokens (est): %s\n", formatNum(int(summary.OutputTokens)))
	fmt.Printf("Tokens Saved (est):  %s\n", formatNum(int(summary.TokensSavedEst)))
	if len(report.ByCommand) > 0 {
		fmt.Println("\nBy Command:")
		for _, row := range report.ByCommand {
			fmt.Printf("  %-36s runs=%d saved=%s\n", row.Command, row.Runs, formatNum(int(row.TokensSavedEst)))
		}
	}
	return nil
}

func loadFilterReport(path string) (analytics.FilterGainReport, error) {
	report, err := analytics.QueryFilterGainReport(path, "all", time.Now(), true, "", 0)
	if err != nil {
		return analytics.FilterGainReport{}, fmt.Errorf("filter report: %w", err)
	}
	return report, nil
}

type combinedReportOutput struct {
	Analytics              sessionReportOutput        `json:"analytics"`
	Decisions              decisionReportOutput       `json:"decisions"`
	Filter                 analytics.FilterGainReport `json:"filter"`
	ProxySavedTokens       int                        `json:"proxy_saved_tokens"`
	Layer0SavedTokensEst   int                        `json:"layer0_saved_tokens_est"`
	CombinedInputTokensEst int                        `json:"combined_input_tokens_est"`
	CombinedSavedTokensEst int                        `json:"combined_saved_tokens_est"`
	CombinedRatioPct       float64                    `json:"combined_ratio_pct"`
}

func combinedReport(analyticsPath, decisionsPath, filterPath, outputFormat string) error {
	report, err := loadCombinedReport(analyticsPath, decisionsPath, filterPath)
	if err != nil {
		return err
	}

	switch outputFormat {
	case outputJSON:
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	case outputCSV:
		return writeCombinedCSV(os.Stdout, report)
	}

	fmt.Printf("=== Combined Report ===\n")
	fmt.Printf("Analytics log:       %s\n", filepath.Base(analyticsPath))
	fmt.Printf("Decisions log:       %s\n", filepath.Base(decisionsPath))
	fmt.Printf("Filter DB:           %s\n", filepath.Base(filterPath))
	fmt.Printf("Proxy requests:      %d\n", report.Analytics.TotalRequests)
	fmt.Printf("Proxy savings:       %s (%.1f%%)\n", formatNum(report.ProxySavedTokens), ratioPct(report.Analytics.OrigTokens, report.ProxySavedTokens))
	fmt.Printf("Layer 0 savings est: %s\n", formatNum(report.Layer0SavedTokensEst))
	fmt.Printf("Combined savings est:%s (%.1f%%)\n", formatNum(report.CombinedSavedTokensEst), report.CombinedRatioPct)
	if report.Decisions.Requests > 0 {
		fmt.Printf("Decision requests:   %d\n", report.Decisions.Requests)
	}

	return nil
}

func loadCombinedReport(analyticsPath, decisionsPath, filterPath string) (combinedReportOutput, error) {
	session, err := loadSessionReport(analyticsPath)
	if err != nil {
		return combinedReportOutput{}, err
	}
	if session.TotalRequests == 0 {
		return combinedReportOutput{}, fmt.Errorf("combined report: analytics file has no request_processed data")
	}

	decisions, err := loadDecisionReport(decisionsPath)
	if err != nil {
		return combinedReportOutput{}, err
	}
	if decisions.Requests == 0 {
		return combinedReportOutput{}, fmt.Errorf("combined report: decisions file has no request summaries")
	}

	filterReport, err := loadFilterReport(filterPath)
	if err != nil {
		return combinedReportOutput{}, err
	}

	proxySaved := session.OrigTokens - session.CompTokens
	layer0Saved := int(filterReport.TokensSavedEst)
	combinedInput := session.OrigTokens + int(filterReport.InputTokens)
	combinedSaved := proxySaved + layer0Saved

	return combinedReportOutput{
		Analytics:              session,
		Decisions:              decisions,
		Filter:                 filterReport,
		ProxySavedTokens:       proxySaved,
		Layer0SavedTokensEst:   layer0Saved,
		CombinedInputTokensEst: combinedInput,
		CombinedSavedTokensEst: combinedSaved,
		CombinedRatioPct:       ratioPct(combinedInput, combinedSaved),
	}, nil
}

// --- helpers ---

func writeSessionCSV(w *os.File, report sessionReportOutput) error {
	rows := [][]string{
		{"metric", "value"},
		{"path", report.Path},
		{"source", report.Source},
		{"total_requests", fmt.Sprintf("%d", report.TotalRequests)},
		{"orig_tokens", fmt.Sprintf("%d", report.OrigTokens)},
		{"comp_tokens", fmt.Sprintf("%d", report.CompTokens)},
		{"output_tokens", fmt.Sprintf("%d", report.OutputTokens)},
		{"saved_tokens", fmt.Sprintf("%d", report.OrigTokens-report.CompTokens)},
		{"saved_ratio_pct", fmt.Sprintf("%.2f", ratioPct(report.OrigTokens, report.OrigTokens-report.CompTokens))},
		{"layer1_savings", fmt.Sprintf("%d", report.Layer1Savings)},
		{"layer2_savings", fmt.Sprintf("%d", report.Layer2Savings)},
		{"secrets_found", fmt.Sprintf("%d", report.SecretsFound)},
		{"errors", fmt.Sprintf("%d", report.Errors)},
		{"retries", fmt.Sprintf("%d", report.Retries)},
	}
	for _, prov := range sortedProviderViewKeys(report.ByProvider) {
		ps := report.ByProvider[prov]
		rows = append(rows,
			[]string{fmt.Sprintf("provider.%s.requests", prov), fmt.Sprintf("%d", ps.Requests)},
			[]string{fmt.Sprintf("provider.%s.orig_tokens", prov), fmt.Sprintf("%d", ps.OrigTokens)},
			[]string{fmt.Sprintf("provider.%s.comp_tokens", prov), fmt.Sprintf("%d", ps.CompTokens)},
			[]string{fmt.Sprintf("provider.%s.saved", prov), fmt.Sprintf("%d", ps.Saved)},
			[]string{fmt.Sprintf("provider.%s.ratio_pct", prov), fmt.Sprintf("%.2f", ps.RatioPct)},
		)
	}
	return writeMetricCSV(w, rows)
}

func writeDecisionCSV(w *os.File, report decisionReportOutput) error {
	rows := [][]string{
		{"metric", "value"},
		{"path", report.Path},
		{"requests", fmt.Sprintf("%d", report.Requests)},
		{"input_tokens_orig", fmt.Sprintf("%d", report.InputTokensOrig)},
		{"input_tokens_final", fmt.Sprintf("%d", report.InputTokensFinal)},
		{"total_saved", fmt.Sprintf("%d", report.TotalSaved)},
		{"ratio_pct", fmt.Sprintf("%.2f", report.RatioPct)},
	}
	for _, name := range sortedStringKeys(report.SubLayerTotals) {
		rows = append(rows, []string{fmt.Sprintf("sub_layer.%s.saved", name), fmt.Sprintf("%d", report.SubLayerTotals[name])})
	}
	return writeMetricCSV(w, rows)
}

func writeFilterCSV(w *os.File, report analytics.FilterGainReport) error {
	rows := [][]string{
		{"metric", "value"},
		{"period", report.Period},
		{"runs", fmt.Sprintf("%d", report.Runs)},
		{"input_tokens", fmt.Sprintf("%d", report.InputTokens)},
		{"output_tokens", fmt.Sprintf("%d", report.OutputTokens)},
		{"tokens_saved_est", fmt.Sprintf("%d", report.TokensSavedEst)},
	}
	for _, row := range report.ByCommand {
		rows = append(rows,
			[]string{fmt.Sprintf("command.%s.runs", row.Command), fmt.Sprintf("%d", row.Runs)},
			[]string{fmt.Sprintf("command.%s.tokens_saved_est", row.Command), fmt.Sprintf("%d", row.TokensSavedEst)},
		)
	}
	return writeMetricCSV(w, rows)
}

func writeCombinedCSV(w *os.File, report combinedReportOutput) error {
	rows := [][]string{
		{"metric", "value"},
		{"proxy_saved_tokens", fmt.Sprintf("%d", report.ProxySavedTokens)},
		{"layer0_saved_tokens_est", fmt.Sprintf("%d", report.Layer0SavedTokensEst)},
		{"combined_input_tokens_est", fmt.Sprintf("%d", report.CombinedInputTokensEst)},
		{"combined_saved_tokens_est", fmt.Sprintf("%d", report.CombinedSavedTokensEst)},
		{"combined_ratio_pct", fmt.Sprintf("%.2f", report.CombinedRatioPct)},
		{"analytics_requests", fmt.Sprintf("%d", report.Analytics.TotalRequests)},
		{"decision_requests", fmt.Sprintf("%d", report.Decisions.Requests)},
		{"filter_runs", fmt.Sprintf("%d", report.Filter.Runs)},
	}
	return writeMetricCSV(w, rows)
}

func writeMetricCSV(w *os.File, rows [][]string) error {
	writer := csv.NewWriter(w)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func formatNum(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

func sortedKeys(m map[string]*providerStats) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedProviderViewKeys(m map[string]providerStatsView) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func ratioPct(total, saved int) float64 {
	if total <= 0 || saved <= 0 {
		return 0
	}
	return float64(saved) / float64(total) * 100
}

// reduce compiler warnings for unused imports
var _ = strings.TrimSpace

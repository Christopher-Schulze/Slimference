// Package main implements the scripts/utils tool for offline session analysis.
//
// Usage:
//
//	go run ./scripts/utils session-report <file.jsonl> [--json|--csv]
//	go run ./scripts/utils aggregate-savings [--admin-url=... | --admin-state-file=...] [--filter-db=...] [--json]
//	go run ./scripts/utils workday-savings <start|finish> [--baseline-file=...] [--json]
//	go run ./scripts/utils codex-capture-run [flags] -- <codex run args...>
//	go run ./scripts/utils wss-audit <decisions.jsonl> [--json]
//	go run ./scripts/utils wss-reference-inventory <jsonl-or-dir> [--json]
//	go run ./scripts/utils wss-local-gap <decisions.jsonl> [--json] [--since=<rfc3339>] [--min-local-ratio=<ratio>] [--min-local-saved=<tokens>]
//	go run ./scripts/utils wss-class-distribution <dir-or-decisions.jsonl> [--json] [--since=<rfc3339>|--since-file=<path>] [--min-local-ratio=<ratio>] [--require-headroom]
//	go run ./scripts/utils wss-proof-pack <dir-or-decisions.jsonl> [--json] [--since=<rfc3339>|--since-file=<path>] [--sockets-json=wss-sockets.json] [--audit-json=wss-audit.json] [--require-headroom]
//	go run ./scripts/utils wss-ab-replay <frames.jsonl> [--json|--fail-on-lost|--fail-on-upstream-error|--archive-recovery-note|--tool-output-mutation|--delta-tool-output-mutation-lab|--codex-chunk-dedup]
//	go run ./scripts/utils wss-t354-shape-proof <frames.jsonl-or-dir> [--json] [--t420-handoff-json=debug-wss-sockets.json]
//	go run ./scripts/utils wss-proof-matrix <captures.jsonl> [--json] [--require-live-token-delta]
//	go run ./scripts/utils wss-proof-inventory <dir-or-matrix.jsonl> [--json]
//	go run ./scripts/utils wss-proof-live-row --matrix-row PATH --frames PATH --workload-class CLASS
//	go run ./scripts/utils search-cap-profile (--command CMD --input stdout.txt | --frames frames.jsonl) [--candidate files:matches...] [--json]
//	go run ./scripts/utils search-cap-proof --frames frames.jsonl [--candidate files:matches...] [--json]
//	go run ./scripts/utils release-proof-report <clean-release-matrix.jsonl> [--json] --resource-profile-proof DIR --resource-profile-proof DIR
//	go run ./scripts/utils tls-probe [--profile=<name>] [--json]
//	go run ./scripts/utils recovery-contract-matrix [--json|--fail-on-product-gaps]
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
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./scripts/utils <subcommand> <path>")
		fmt.Fprintln(os.Stderr, "Subcommands: session-report, aggregate-savings, workday-savings, codex-capture-run, wss-audit, wss-reference-inventory, wss-local-gap, wss-class-distribution, wss-proof-pack, wss-ab-replay, wss-t354-shape-proof, wss-proof-matrix, wss-proof-inventory, wss-proof-live-row, search-cap-profile, search-cap-proof, release-proof-report, tls-probe, recovery-contract-matrix")
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
	case "wss-reference-inventory":
		os.Exit(runWSSReferenceInventory(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-local-gap":
		os.Exit(runWSSLocalGap(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-class-distribution":
		os.Exit(runWSSClassDistribution(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-proof-pack":
		os.Exit(runWSSProofPack(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-ab-replay":
		os.Exit(runWSSABReplay(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-t354-shape-proof":
		os.Exit(runWSST354ShapeProof(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-proof-matrix":
		os.Exit(runWSSProofMatrix(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-proof-inventory":
		os.Exit(runWSSProofInventory(os.Args[2:], os.Stdout, os.Stderr))
	case "wss-proof-live-row":
		os.Exit(runWSSProofLiveRow(os.Args[2:], os.Stdout, os.Stderr))
	case "search-cap-profile":
		os.Exit(runSearchCapProfile(os.Args[2:], os.Stdout, os.Stderr))
	case "search-cap-proof":
		os.Exit(runSearchCapProof(os.Args[2:], os.Stdout, os.Stderr))
	case "release-proof-report":
		os.Exit(runReleaseProofReport(os.Args[2:], os.Stdout, os.Stderr))
	case "tls-probe":
		os.Exit(runTLSProbe(os.Args[2:], os.Stdout, os.Stderr))
	case "recovery-contract-matrix":
		os.Exit(runRecoveryContractMatrix(os.Args[2:], os.Stdout, os.Stderr))
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
	FirstTimestamp time.Time                    `json:"first_timestamp"`
	LastTimestamp  time.Time                    `json:"last_timestamp"`
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

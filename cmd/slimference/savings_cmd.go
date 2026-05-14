package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/config"
)

var _ = config.Defaults // keep config import alive even if no direct calls remain

// savingsFlags captures the CLI flags for `slimference savings`. T80.
type savingsFlags struct {
	json    bool
	csv     bool
	project string
}

// SavingsSummary collapses Layer 0 filter, Layer 1+2 proxy compression,
// and Layer 3 cache savings into a single canonical view. Returned by
// `slimference savings <period>` and surfaced via /admin if needed.
type SavingsSummary struct {
	Period                           string  `json:"period"`
	Project                          string  `json:"project,omitempty"`
	Layer0Runs                       int64   `json:"layer0_runs"`
	Layer0SavedTokens                int64   `json:"layer0_saved_tokens"`
	Layer0SavedUSD                   float64 `json:"layer0_saved_usd"`
	ProxyOrigTokens                  int64   `json:"proxy_orig_tokens"`
	ProxyCompTokens                  int64   `json:"proxy_comp_tokens"`
	ProxySavedTokens                 int64   `json:"proxy_saved_tokens"`
	ProxyRequests                    int64   `json:"proxy_requests"`
	ProviderReportedRequests         int64   `json:"provider_reported_requests"`
	ProviderInputTokens              int64   `json:"provider_input_tokens"`
	ProviderCachedTokens             int64   `json:"provider_cached_tokens"`
	ProviderOutputTokens             int64   `json:"provider_output_tokens"`
	OutputReduceInputOverheadTokens  int64   `json:"output_reduce_input_overhead_tokens"`
	CacheReadDiscountTokenEquivalent int64   `json:"cache_read_discount_token_equivalent"`
	NetBillableEquivalentTokens      int64   `json:"net_billable_equivalent_tokens"`
	CacheHits                        int64   `json:"cache_hits"`
	TotalSavedTokens                 int64   `json:"total_saved_tokens"`
	TotalSavedUSD                    float64 `json:"total_saved_usd"`
	USDPerMillion                    float64 `json:"usd_per_million_tokens"`
}

func parseSavingsArgs(args []string) (period string, f savingsFlags, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--json", "-json":
			f.json = true
		case "--csv":
			f.csv = true
		case "--project":
			i++
			if i >= len(args) || args[i] == "" {
				return "", f, fmt.Errorf("--project requires a path")
			}
			f.project = args[i]
		default:
			if a == "" {
				continue
			}
			if strings.HasPrefix(a, "-") {
				return "", f, fmt.Errorf("unknown flag: %s", a)
			}
			if period == "" {
				period = a
			} else {
				return "", f, fmt.Errorf("unexpected extra argument: %s", a)
			}
		}
	}
	if period == "" {
		period = "today"
	}
	return period, f, nil
}

// computeSavings aggregates filter.db (Layer 0) + analytics snapshots
// (Layer 1/2/3) into one SavingsSummary for the period. Best-effort:
// errors from individual sources (missing filter db, unreadable
// snapshot file) are silently treated as "no data" so the command
// still produces a meaningful summary on partial state.
func computeSavings(cfg *config.Config, period, project string, now time.Time) SavingsSummary {
	out := SavingsSummary{
		Period:        period,
		Project:       project,
		USDPerMillion: cfg.Analytics.GainUSDPerMillionTokens,
	}

	filterPath, err := resolveFilterDBPathFn()
	if err == nil {
		if _, statErr := os.Stat(filterPath); statErr == nil {
			rep, err := analytics.QueryFilterGainReport(filterPath, period, now, false, project, out.USDPerMillion)
			if err == nil {
				out.Layer0Runs = rep.FilterGainSummary.Runs
				out.Layer0SavedTokens = rep.FilterGainSummary.TokensSavedEst
				out.Layer0SavedUSD = rep.FilterGainSummary.SavingsUsdEst
			}
		}
	}

	logDir := cfg.Analytics.ResolvedLogDir()
	switch period {
	case "today":
		if snapshots, err := analytics.ReadDailyStats(logDir, now); err == nil {
			accumulateSnapshots(&out, snapshots)
		}
	case "week":
		if snapshots, err := analytics.ReadWeeklyStats(logDir); err == nil {
			accumulateSnapshots(&out, snapshots)
		}
	case "month":
		for i := 0; i < 30; i++ {
			day := now.AddDate(0, 0, -i)
			if snaps, err := analytics.ReadDailyStats(logDir, day); err == nil {
				accumulateSnapshots(&out, snaps)
			}
		}
	case "all":
		for i := 0; i < 365; i++ {
			day := now.AddDate(0, 0, -i)
			if snaps, err := analytics.ReadDailyStats(logDir, day); err == nil {
				accumulateSnapshots(&out, snaps)
			}
		}
	}

	if project == "" {
		accumulateProxyFlightsFromDecisionLog(&out, cfg, period, now)
	}

	out.TotalSavedTokens = out.Layer0SavedTokens + out.ProxySavedTokens
	out.NetBillableEquivalentTokens = out.TotalSavedTokens + out.CacheReadDiscountTokenEquivalent
	if out.USDPerMillion > 0 {
		out.TotalSavedUSD = float64(out.TotalSavedTokens) / 1_000_000.0 * out.USDPerMillion
	}
	return out
}

func accumulateProxyFlightsFromDecisionLog(out *SavingsSummary, cfg *config.Config, period string, now time.Time) {
	path := strings.TrimSpace(cfg.Debug.DecisionsLog)
	if path == "" {
		return
	}
	path = filepath.Clean(config.ExpandHomePath(path))
	summaries, err := replaySessionFn(path)
	if err != nil {
		return
	}
	report, err := analytics.SummarizeProxyFlights(summaries, period, now)
	if err != nil || report.Requests == 0 {
		return
	}
	out.ProxyRequests = int64(report.Requests)
	out.ProviderReportedRequests = int64(report.ProviderReportedRequests)
	out.ProxyOrigTokens = int64(report.EstimatedOriginalInputTokens)
	out.ProxyCompTokens = int64(report.EstimatedFinalInputTokens)
	out.ProxySavedTokens = int64(report.BillableInputSavingsEstimate)
	out.ProviderInputTokens = int64(report.ProviderInputTokens)
	out.ProviderCachedTokens = int64(report.ProviderCachedTokens)
	out.ProviderOutputTokens = int64(report.ProviderOutputTokens)
	out.OutputReduceInputOverheadTokens = int64(report.OutputReduceInputOverheadTokens)
	out.CacheReadDiscountTokenEquivalent = int64(report.CacheReadDiscountTokenEquivalent)
}

func accumulateSnapshots(out *SavingsSummary, snapshots []analytics.AnalyticsSnapshot) {
	for _, snap := range snapshots {
		out.ProxyRequests += int64(snap.TotalRequests)
		out.ProxyOrigTokens += int64(snap.TotalInputTokens)
		// CompTokens approximates the on-the-wire size: original minus
		// the saved input tokens reported by the snapshot.
		comp := int64(snap.TotalInputTokens - snap.SavedInputTokens)
		if comp < 0 {
			comp = 0
		}
		out.ProxyCompTokens += comp
		out.ProxySavedTokens += int64(snap.SavedInputTokens)
		out.CacheHits += int64(snap.CacheHits)
	}
}

// formatSavingsText renders a human-readable savings block. T80.
func formatSavingsText(s SavingsSummary) string {
	var sb strings.Builder
	title := "Slimference savings (" + s.Period + ")"
	if s.Project != "" {
		title += " - project " + s.Project
	}
	sb.WriteString(title + "\n")
	sb.WriteString(strings.Repeat("=", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Layer 0 filter runs:        %d\n", s.Layer0Runs))
	sb.WriteString(fmt.Sprintf("Layer 0 tokens saved:       %s\n", formatInt64Plain(s.Layer0SavedTokens)))
	sb.WriteString(fmt.Sprintf("Proxy requests:             %d\n", s.ProxyRequests))
	sb.WriteString(fmt.Sprintf("Proxy original tokens:      %s\n", formatInt64Plain(s.ProxyOrigTokens)))
	sb.WriteString(fmt.Sprintf("Proxy compressed tokens:    %s\n", formatInt64Plain(s.ProxyCompTokens)))
	sb.WriteString(fmt.Sprintf("Proxy tokens saved:         %s\n", formatInt64Plain(s.ProxySavedTokens)))
	if s.ProviderReportedRequests > 0 {
		sb.WriteString(fmt.Sprintf("Provider-reported requests: %d\n", s.ProviderReportedRequests))
		sb.WriteString(fmt.Sprintf("Provider input tokens:      %s\n", formatInt64Plain(s.ProviderInputTokens)))
		sb.WriteString(fmt.Sprintf("Provider cached tokens:     %s\n", formatInt64Plain(s.ProviderCachedTokens)))
		sb.WriteString(fmt.Sprintf("Provider output tokens:     %s\n", formatInt64Plain(s.ProviderOutputTokens)))
		sb.WriteString(fmt.Sprintf("Cache-read billable equiv.: %s\n", formatInt64Plain(s.CacheReadDiscountTokenEquivalent)))
	}
	sb.WriteString(fmt.Sprintf("Layer 3 cache hits:         %d\n", s.CacheHits))
	sb.WriteString(strings.Repeat("-", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Total tokens saved:         %s\n", formatInt64Plain(s.TotalSavedTokens)))
	if s.NetBillableEquivalentTokens != s.TotalSavedTokens {
		sb.WriteString(fmt.Sprintf("Billable-equivalent saved:  %s\n", formatInt64Plain(s.NetBillableEquivalentTokens)))
	}
	if s.USDPerMillion > 0 {
		sb.WriteString(fmt.Sprintf("Total saved (~$%.2f/M est.): ~$%.4f\n", s.USDPerMillion, s.TotalSavedUSD))
	}
	return sb.String()
}

// formatSavingsCSV emits a single-row CSV summary.
func formatSavingsCSV(s SavingsSummary) string {
	var sb strings.Builder
	sb.WriteString("period,project,layer0_runs,layer0_saved_tokens,proxy_requests,provider_reported_requests,proxy_orig_tokens,proxy_comp_tokens,proxy_saved_tokens,provider_input_tokens,provider_cached_tokens,provider_output_tokens,output_reduce_input_overhead_tokens,cache_read_discount_token_equivalent,net_billable_equivalent_tokens,cache_hits,total_saved_tokens,total_saved_usd\n")
	sb.WriteString(fmt.Sprintf("%s,%s,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%.4f\n",
		s.Period,
		s.Project,
		s.Layer0Runs,
		s.Layer0SavedTokens,
		s.ProxyRequests,
		s.ProviderReportedRequests,
		s.ProxyOrigTokens,
		s.ProxyCompTokens,
		s.ProxySavedTokens,
		s.ProviderInputTokens,
		s.ProviderCachedTokens,
		s.ProviderOutputTokens,
		s.OutputReduceInputOverheadTokens,
		s.CacheReadDiscountTokenEquivalent,
		s.NetBillableEquivalentTokens,
		s.CacheHits,
		s.TotalSavedTokens,
		s.TotalSavedUSD,
	))
	return sb.String()
}

func formatInt64Plain(n int64) string {
	return formatTokensPlain64(n)
}

// handleSavingsCmd implements `slimference savings [today|week|month|all]`.
// T80 unified view that collapses gain + stats + cache into one summary.
func handleSavingsCmd(args []string) {
	period, flags, err := parseSavingsArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}
	switch period {
	case "today", "week", "month", "all":
	default:
		fmt.Fprintln(os.Stderr, "usage: slimference savings [today|week|month|all] [--json] [--csv] [--project <path>]")
		exitFn(1)
	}
	cfg, err := configLoadFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	summary := computeSavings(cfg, period, flags.project, time.Now())
	switch {
	case flags.json:
		out, _ := json.MarshalIndent(&summary, "", "  ")
		fmt.Println(string(out))
	case flags.csv:
		fmt.Print(formatSavingsCSV(summary))
	default:
		fmt.Print(formatSavingsText(summary))
	}
}

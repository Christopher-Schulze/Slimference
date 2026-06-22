package analytics

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

// ProxyFlightGainSummary aggregates provider-reported request accounting from
// the decision-log flight recorder. It is deliberately separated from Layer-0
// filter gain: these numbers describe real proxied LLM requests, including
// provider cache credits, not subprocess output compaction rows.
type ProxyFlightGainSummary struct {
	Period                           string               `json:"period"`
	StartUnix                        int64                `json:"start_unix"`
	EndUnix                          int64                `json:"end_unix"`
	Requests                         int                  `json:"requests"`
	ProviderReportedRequests         int                  `json:"provider_reported_requests"`
	LocalCacheHits                   int                  `json:"local_cache_hits"`
	EstimatedOriginalInputTokens     int                  `json:"estimated_original_input_tokens"`
	EstimatedFinalInputTokens        int                  `json:"estimated_final_input_tokens"`
	ProviderInputTokens              int                  `json:"provider_input_tokens"`
	ProviderCachedTokens             int                  `json:"provider_cached_tokens"`
	ProviderOutputTokens             int                  `json:"provider_output_tokens"`
	ProviderCacheReadTokens          int                  `json:"provider_cache_read_tokens"`
	ProviderCacheCreateTokens        int                  `json:"provider_cache_create_tokens"`
	ProviderCacheNetTokens           int                  `json:"provider_cache_net_tokens"`
	ProviderCacheNegativeNetRequests int                  `json:"provider_cache_negative_net_requests"`
	BillableInputSavingsEstimate     int                  `json:"billable_input_savings_estimate"`
	WireSavingsEstimate              int                  `json:"wire_savings_estimate"`
	OutputWireSavingsEstimate        int                  `json:"output_wire_savings_estimate"`
	ToolPruneSavedTokens             int                  `json:"tool_prune_saved_tokens"`
	ToolPrunePrunedTools             int                  `json:"tool_prune_pruned_tools"`
	ToolPruneReattached              int                  `json:"tool_prune_reattached"`
	ToolPruneMisses                  int                  `json:"tool_prune_misses"`
	ToolPruneRetries                 int                  `json:"tool_prune_retries"`
	OutputReduceInputOverheadTokens  int                  `json:"output_reduce_input_overhead_tokens"`
	CacheReadDiscountTokenEquivalent int                  `json:"cache_read_discount_token_equivalent"`
	NetBillableEquivalentEstimate    int                  `json:"net_billable_equivalent_estimate"`
	PromptCacheHeat                  []PromptCacheHeatRow `json:"prompt_cache_heat,omitempty"`
}

type PromptCacheHeatRow struct {
	StablePrefixHash      string `json:"stable_prefix_hash"`
	Requests              int    `json:"requests"`
	HintsApplied          int    `json:"hints_applied"`
	HintsSkipped          int    `json:"hints_skipped"`
	StablePrefixTokensMax int    `json:"stable_prefix_tokens_max"`
	ProviderCachedTokens  int    `json:"provider_cached_tokens"`
	CacheReadTokens       int    `json:"cache_read_tokens"`
	CacheCreateTokens     int    `json:"cache_create_tokens"`
	CacheNetTokens        int    `json:"cache_net_tokens"`
}

// SummarizeProxyFlights folds decision-log request summaries into one honest
// proxy gain view for the requested period.
func SummarizeProxyFlights(summaries []dbg.RequestSummary, period string, now time.Time) (ProxyFlightGainSummary, error) {
	start, end, err := FilterGainWindow(period, now)
	if err != nil {
		return ProxyFlightGainSummary{}, err
	}
	report := ProxyFlightGainSummary{
		Period:    period,
		StartUnix: start.Unix(),
		EndUnix:   end.Unix(),
	}
	heat := map[string]*PromptCacheHeatRow{}
	for _, summary := range summaries {
		if summary.Timestamp.IsZero() || summary.Timestamp.Before(start) || summary.Timestamp.After(end) {
			continue
		}
		summary.EnsureFlight()
		flight := summary.Flight
		if !isProviderProxyFlight(flight) {
			continue
		}
		tokens := flight.TokenAccounting
		report.Requests++
		if flight.Confidence == "provider_reported" {
			report.ProviderReportedRequests++
		}
		if flight.CacheAccounting.LocalResponseCacheHit {
			report.LocalCacheHits++
		}
		report.EstimatedOriginalInputTokens += tokens.EstimatedOriginalInputTokens
		report.EstimatedFinalInputTokens += tokens.EstimatedFinalInputTokens
		report.ProviderInputTokens += tokens.ProviderInputTokens
		report.ProviderCachedTokens += tokens.ProviderCachedTokens
		cacheRead := tokens.ProviderCachedTokens + flight.CacheAccounting.ProviderCacheReadTokens
		cacheCreate := flight.CacheAccounting.ProviderCacheCreateTokens
		cacheNet := cacheRead - cacheCreate
		report.ProviderCacheReadTokens += cacheRead
		report.ProviderCacheCreateTokens += cacheCreate
		report.ProviderCacheNetTokens += cacheNet
		if cacheNet < 0 {
			report.ProviderCacheNegativeNetRequests++
		}
		report.BillableInputSavingsEstimate += tokens.BillableSavingsEstimate + flight.ToolPrune.SavedTokens
		report.WireSavingsEstimate += tokens.WireSavingsEstimate + flight.ToolPrune.SavedTokens
		report.OutputWireSavingsEstimate += tokens.OutputWireSavingsEstimate
		report.ToolPruneSavedTokens += flight.ToolPrune.SavedTokens
		report.ToolPrunePrunedTools += flight.ToolPrune.PrunedTools
		report.ToolPruneReattached += flight.ToolPrune.Reattached
		if flight.ToolPrune.Miss {
			report.ToolPruneMisses++
		}
		if flight.ToolPrune.Retry {
			report.ToolPruneRetries++
		}
		report.OutputReduceInputOverheadTokens += flight.OutputReduce.AddedTokens
		accumulatePromptCacheHeat(heat, flight)
		if tokens.ProviderOutputTokens > 0 {
			report.ProviderOutputTokens += tokens.ProviderOutputTokens
		} else {
			report.ProviderOutputTokens += tokens.EstimatedOutputTokens
		}
	}
	report.CacheReadDiscountTokenEquivalent = cacheReadDiscountEquivalent(report.ProviderCacheReadTokens, report.ProviderCacheCreateTokens)
	report.NetBillableEquivalentEstimate = report.BillableInputSavingsEstimate + report.CacheReadDiscountTokenEquivalent
	report.PromptCacheHeat = sortedPromptCacheHeat(heat)
	return report, nil
}

func cacheReadDiscountEquivalent(readTokens int, createTokens int) int {
	net := readTokens - createTokens
	if net <= 0 {
		return 0
	}
	return int(float64(net) * 0.9)
}

func accumulatePromptCacheHeat(heat map[string]*PromptCacheHeatRow, flight *dbg.FlightRequestSummary) {
	if flight == nil {
		return
	}
	cache := flight.CacheAccounting
	hash := cache.PromptCacheStablePrefixHash
	if hash == "" {
		return
	}
	row := heat[hash]
	if row == nil {
		row = &PromptCacheHeatRow{StablePrefixHash: hash}
		heat[hash] = row
	}
	row.Requests++
	if cache.PromptCacheHintApplied {
		row.HintsApplied++
	} else if cache.PromptCacheHintReason != "" {
		row.HintsSkipped++
	}
	if cache.PromptCacheStablePrefixTokens > row.StablePrefixTokensMax {
		row.StablePrefixTokensMax = cache.PromptCacheStablePrefixTokens
	}
	row.ProviderCachedTokens += flight.TokenAccounting.ProviderCachedTokens
	row.CacheReadTokens += cache.ProviderCacheReadTokens
	row.CacheCreateTokens += cache.ProviderCacheCreateTokens
	row.CacheNetTokens += flight.TokenAccounting.ProviderCachedTokens + cache.ProviderCacheReadTokens - cache.ProviderCacheCreateTokens
}

func sortedPromptCacheHeat(heat map[string]*PromptCacheHeatRow) []PromptCacheHeatRow {
	if len(heat) == 0 {
		return nil
	}
	rows := make([]PromptCacheHeatRow, 0, len(heat))
	for _, row := range heat {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ProviderCachedTokens != rows[j].ProviderCachedTokens {
			return rows[i].ProviderCachedTokens > rows[j].ProviderCachedTokens
		}
		if rows[i].CacheReadTokens != rows[j].CacheReadTokens {
			return rows[i].CacheReadTokens > rows[j].CacheReadTokens
		}
		if rows[i].HintsApplied != rows[j].HintsApplied {
			return rows[i].HintsApplied > rows[j].HintsApplied
		}
		if rows[i].Requests != rows[j].Requests {
			return rows[i].Requests > rows[j].Requests
		}
		return rows[i].StablePrefixHash < rows[j].StablePrefixHash
	})
	return rows
}

func isProviderProxyFlight(flight *dbg.FlightRequestSummary) bool {
	if flight == nil {
		return false
	}
	switch flight.Provider {
	case "", "local", "unknown":
		return false
	}
	switch flight.Source {
	case "", "proxy", "transparent_connect":
		return true
	default:
		return false
	}
}

// WriteProxyFlightGainCSV renders the proxy flight gain summary as one CSV row.
func WriteProxyFlightGainCSV(w io.Writer, report ProxyFlightGainSummary) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"period",
		"requests",
		"provider_reported_requests",
		"local_cache_hits",
		"estimated_original_input_tokens",
		"estimated_final_input_tokens",
		"provider_input_tokens",
		"provider_cached_tokens",
		"provider_output_tokens",
		"provider_cache_read_tokens",
		"provider_cache_create_tokens",
		"provider_cache_net_tokens",
		"provider_cache_negative_net_requests",
		"billable_input_savings_estimate",
		"wire_savings_estimate",
		"output_wire_savings_estimate",
		"tool_prune_saved_tokens",
		"tool_prune_pruned_tools",
		"tool_prune_reattached",
		"tool_prune_misses",
		"tool_prune_retries",
		"output_reduce_input_overhead_tokens",
		"cache_read_discount_token_equivalent",
		"net_billable_equivalent_estimate",
		"prompt_cache_heat_keys",
		"prompt_cache_heat_top_hash",
		"prompt_cache_heat_top_cached_tokens",
	})
	topHash := ""
	topCached := 0
	if len(report.PromptCacheHeat) > 0 {
		topHash = report.PromptCacheHeat[0].StablePrefixHash
		topCached = report.PromptCacheHeat[0].ProviderCachedTokens
	}
	_ = cw.Write([]string{
		report.Period,
		fmt.Sprintf("%d", report.Requests),
		fmt.Sprintf("%d", report.ProviderReportedRequests),
		fmt.Sprintf("%d", report.LocalCacheHits),
		fmt.Sprintf("%d", report.EstimatedOriginalInputTokens),
		fmt.Sprintf("%d", report.EstimatedFinalInputTokens),
		fmt.Sprintf("%d", report.ProviderInputTokens),
		fmt.Sprintf("%d", report.ProviderCachedTokens),
		fmt.Sprintf("%d", report.ProviderOutputTokens),
		fmt.Sprintf("%d", report.ProviderCacheReadTokens),
		fmt.Sprintf("%d", report.ProviderCacheCreateTokens),
		fmt.Sprintf("%d", report.ProviderCacheNetTokens),
		fmt.Sprintf("%d", report.ProviderCacheNegativeNetRequests),
		fmt.Sprintf("%d", report.BillableInputSavingsEstimate),
		fmt.Sprintf("%d", report.WireSavingsEstimate),
		fmt.Sprintf("%d", report.OutputWireSavingsEstimate),
		fmt.Sprintf("%d", report.ToolPruneSavedTokens),
		fmt.Sprintf("%d", report.ToolPrunePrunedTools),
		fmt.Sprintf("%d", report.ToolPruneReattached),
		fmt.Sprintf("%d", report.ToolPruneMisses),
		fmt.Sprintf("%d", report.ToolPruneRetries),
		fmt.Sprintf("%d", report.OutputReduceInputOverheadTokens),
		fmt.Sprintf("%d", report.CacheReadDiscountTokenEquivalent),
		fmt.Sprintf("%d", report.NetBillableEquivalentEstimate),
		fmt.Sprintf("%d", len(report.PromptCacheHeat)),
		topHash,
		fmt.Sprintf("%d", topCached),
	})
	cw.Flush()
	return cw.Error()
}

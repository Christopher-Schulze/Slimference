package analytics

import (
	"encoding/csv"
	"fmt"
	"io"
	"time"

	dbg "github.com/slimference/slimference/internal/debug"
)

// ProxyFlightGainSummary aggregates provider-reported request accounting from
// the decision-log flight recorder. It is deliberately separated from Layer-0
// filter gain: these numbers describe real proxied LLM requests, including
// provider cache credits, not subprocess output compaction rows.
type ProxyFlightGainSummary struct {
	Period                           string `json:"period"`
	StartUnix                        int64  `json:"start_unix"`
	EndUnix                          int64  `json:"end_unix"`
	Requests                         int    `json:"requests"`
	ProviderReportedRequests         int    `json:"provider_reported_requests"`
	LocalCacheHits                   int    `json:"local_cache_hits"`
	EstimatedOriginalInputTokens     int    `json:"estimated_original_input_tokens"`
	EstimatedFinalInputTokens        int    `json:"estimated_final_input_tokens"`
	ProviderInputTokens              int    `json:"provider_input_tokens"`
	ProviderCachedTokens             int    `json:"provider_cached_tokens"`
	ProviderOutputTokens             int    `json:"provider_output_tokens"`
	BillableInputSavingsEstimate     int    `json:"billable_input_savings_estimate"`
	WireSavingsEstimate              int    `json:"wire_savings_estimate"`
	OutputReduceInputOverheadTokens  int    `json:"output_reduce_input_overhead_tokens"`
	CacheReadDiscountTokenEquivalent int    `json:"cache_read_discount_token_equivalent"`
	NetBillableEquivalentEstimate    int    `json:"net_billable_equivalent_estimate"`
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
		report.BillableInputSavingsEstimate += tokens.BillableSavingsEstimate
		report.WireSavingsEstimate += tokens.WireSavingsEstimate
		report.OutputReduceInputOverheadTokens += flight.OutputReduce.AddedTokens
		if tokens.ProviderOutputTokens > 0 {
			report.ProviderOutputTokens += tokens.ProviderOutputTokens
		} else {
			report.ProviderOutputTokens += tokens.EstimatedOutputTokens
		}
	}
	report.CacheReadDiscountTokenEquivalent = int(float64(report.ProviderCachedTokens) * 0.9)
	report.NetBillableEquivalentEstimate = report.BillableInputSavingsEstimate + report.CacheReadDiscountTokenEquivalent
	return report, nil
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
		"billable_input_savings_estimate",
		"wire_savings_estimate",
		"output_reduce_input_overhead_tokens",
		"cache_read_discount_token_equivalent",
		"net_billable_equivalent_estimate",
	})
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
		fmt.Sprintf("%d", report.BillableInputSavingsEstimate),
		fmt.Sprintf("%d", report.WireSavingsEstimate),
		fmt.Sprintf("%d", report.OutputReduceInputOverheadTokens),
		fmt.Sprintf("%d", report.CacheReadDiscountTokenEquivalent),
		fmt.Sprintf("%d", report.NetBillableEquivalentEstimate),
	})
	cw.Flush()
	return cw.Error()
}

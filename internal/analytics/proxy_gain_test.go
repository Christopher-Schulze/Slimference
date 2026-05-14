package analytics

import (
	"bytes"
	"strings"
	"testing"
	"time"

	dbg "github.com/slimference/slimference/internal/debug"
)

func TestSummarizeProxyFlights(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	summaries := []dbg.RequestSummary{
		{
			RequestID: "req-1",
			Timestamp: now.Add(-time.Hour),
			Tokens: dbg.TokenCounts{
				Original: 1000,
				Final:    700,
				Saved:    300,
			},
			CacheHit:             true,
			ProviderInputTokens:  1200,
			ProviderCachedTokens: 500,
			ProviderOutputTokens: 80,
			OutputReduce: dbg.OutputReduceSummary{
				Applied:     true,
				AddedTokens: 12,
			},
		},
		{
			RequestID:           "req-2",
			Timestamp:           now.Add(-2 * time.Hour),
			OutputTokens:        20,
			ProviderInputTokens: 100,
			Tokens: dbg.TokenCounts{
				Original: 200,
				Final:    220,
				Saved:    -20,
			},
		},
		{
			RequestID: "old",
			Timestamp: now.AddDate(0, 0, -2),
			Tokens: dbg.TokenCounts{
				Original: 999,
				Final:    1,
				Saved:    998,
			},
		},
	}
	report, err := SummarizeProxyFlights(summaries, "today", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Requests != 2 || report.ProviderReportedRequests != 2 || report.LocalCacheHits != 1 {
		t.Fatalf("bad counters: %+v", report)
	}
	if report.EstimatedOriginalInputTokens != 1200 ||
		report.EstimatedFinalInputTokens != 920 ||
		report.ProviderInputTokens != 1300 ||
		report.ProviderCachedTokens != 500 ||
		report.ProviderOutputTokens != 100 ||
		report.BillableInputSavingsEstimate != 280 ||
		report.OutputReduceInputOverheadTokens != 12 {
		t.Fatalf("bad token totals: %+v", report)
	}
	if report.CacheReadDiscountTokenEquivalent != 450 || report.NetBillableEquivalentEstimate != 730 {
		t.Fatalf("bad net estimate: %+v", report)
	}
}

func TestSummarizeProxyFlightsBadPeriod(t *testing.T) {
	t.Parallel()
	if _, err := SummarizeProxyFlights(nil, "bad", time.Now()); err == nil {
		t.Fatal("expected bad period error")
	}
}

func TestWriteProxyFlightGainCSV(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := WriteProxyFlightGainCSV(&buf, ProxyFlightGainSummary{
		Period:                        "today",
		Requests:                      2,
		ProviderCachedTokens:          500,
		NetBillableEquivalentEstimate: 730,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "provider_cached_tokens") || !strings.Contains(out, "730") {
		t.Fatalf("csv output: %q", out)
	}
	if err := WriteProxyFlightGainCSV(promptCacheErrWriter{}, ProxyFlightGainSummary{}); err == nil {
		t.Fatal("expected writer error")
	}
}

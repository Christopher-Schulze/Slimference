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
			Source:    "proxy",
			Provider:  "codex_chatgpt",
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
			Source:              "transparent_connect",
			Provider:            "openai",
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
			Source:    "proxy",
			Provider:  "codex_chatgpt",
			Tokens: dbg.TokenCounts{
				Original: 999,
				Final:    1,
				Saved:    998,
			},
		},
		{
			RequestID: "hook-local",
			Timestamp: now,
			Source:    "readhook",
			Provider:  "local",
			Tokens: dbg.TokenCounts{
				Original: 5000,
				Final:    10,
				Saved:    4990,
			},
		},
		{
			RequestID: "raw-pass",
			Timestamp: now,
			Source:    "transparent_connect",
			Provider:  "unknown",
			Tokens: dbg.TokenCounts{
				Original: 5000,
				Final:    10,
				Saved:    4990,
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

func TestIsProviderProxyFlight(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		flight *dbg.FlightRequestSummary
		want   bool
	}{
		{name: "nil", flight: nil, want: false},
		{name: "missing provider", flight: &dbg.FlightRequestSummary{Source: "proxy"}, want: false},
		{name: "local provider", flight: &dbg.FlightRequestSummary{Source: "proxy", Provider: "local"}, want: false},
		{name: "unknown provider", flight: &dbg.FlightRequestSummary{Source: "transparent_connect", Provider: "unknown"}, want: false},
		{name: "legacy source empty", flight: &dbg.FlightRequestSummary{Provider: "codex_chatgpt"}, want: true},
		{name: "proxy", flight: &dbg.FlightRequestSummary{Source: "proxy", Provider: "codex_chatgpt"}, want: true},
		{name: "transparent connect", flight: &dbg.FlightRequestSummary{Source: "transparent_connect", Provider: "openai"}, want: true},
		{name: "hook source", flight: &dbg.FlightRequestSummary{Source: "hook_post", Provider: "codex_chatgpt"}, want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isProviderProxyFlight(tt.flight); got != tt.want {
				t.Fatalf("isProviderProxyFlight() = %v, want %v", got, tt.want)
			}
		})
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

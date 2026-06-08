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
			CacheReadTokens:      50,
			CacheCreateTokens:    100,
			ProviderInputTokens:  1200,
			ProviderCachedTokens: 500,
			ProviderOutputTokens: 80,
			OutputReduce: dbg.OutputReduceSummary{
				Applied:     true,
				AddedTokens: 12,
			},
			PromptCache: dbg.PromptCacheSummary{
				Applied:            true,
				Reason:             "applied",
				StablePrefixHash:   "hot-a",
				StablePrefixTokens: 1500,
			},
			ToolPrune: dbg.ToolPruneSummary{
				Applied:     true,
				PrunedTools: 2,
				SavedTokens: 40,
				Reattached:  1,
				Miss:        true,
				Retry:       true,
			},
		},
		{
			RequestID:           "req-2",
			Timestamp:           now.Add(-2 * time.Hour),
			Source:              "transparent_connect",
			Provider:            "openai",
			OutputTokens:        20,
			CacheCreateTokens:   300,
			ProviderInputTokens: 100,
			PromptCache: dbg.PromptCacheSummary{
				Reason:             "stable_prefix_too_small",
				StablePrefixHash:   "cold-b",
				StablePrefixTokens: 300,
			},
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
		report.ProviderCacheReadTokens != 550 ||
		report.ProviderCacheCreateTokens != 400 ||
		report.ProviderCacheNetTokens != 150 ||
		report.ProviderCacheNegativeNetRequests != 1 ||
		report.ProviderOutputTokens != 100 ||
		report.BillableInputSavingsEstimate != 320 ||
		report.OutputReduceInputOverheadTokens != 12 {
		t.Fatalf("bad token totals: %+v", report)
	}
	if report.ToolPruneSavedTokens != 40 ||
		report.ToolPrunePrunedTools != 2 ||
		report.ToolPruneReattached != 1 ||
		report.ToolPruneMisses != 1 ||
		report.ToolPruneRetries != 1 {
		t.Fatalf("bad tool-prune totals: %+v", report)
	}
	if report.CacheReadDiscountTokenEquivalent != 135 || report.NetBillableEquivalentEstimate != 455 {
		t.Fatalf("bad net estimate: %+v", report)
	}
	if len(report.PromptCacheHeat) != 2 {
		t.Fatalf("expected two heat rows, got %+v", report.PromptCacheHeat)
	}
	if hot := report.PromptCacheHeat[0]; hot.StablePrefixHash != "hot-a" || hot.HintsApplied != 1 || hot.ProviderCachedTokens != 500 || hot.CacheReadTokens != 50 || hot.CacheCreateTokens != 100 || hot.CacheNetTokens != 450 || hot.StablePrefixTokensMax != 1500 {
		t.Fatalf("bad hot heat row: %+v", hot)
	}
	if cold := report.PromptCacheHeat[1]; cold.StablePrefixHash != "cold-b" || cold.HintsSkipped != 1 || cold.CacheNetTokens != -300 {
		t.Fatalf("bad cold heat row: %+v", cold)
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
		Period:                           "today",
		Requests:                         2,
		ProviderCachedTokens:             500,
		ProviderCacheReadTokens:          700,
		ProviderCacheCreateTokens:        900,
		ProviderCacheNetTokens:           -200,
		ProviderCacheNegativeNetRequests: 1,
		NetBillableEquivalentEstimate:    730,
		PromptCacheHeat: []PromptCacheHeatRow{{
			StablePrefixHash:     "hot-a",
			ProviderCachedTokens: 900,
			CacheNetTokens:       -100,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "provider_cached_tokens") ||
		!strings.Contains(out, "provider_cache_net_tokens") ||
		!strings.Contains(out, "prompt_cache_heat_keys") ||
		!strings.Contains(out, "730") ||
		!strings.Contains(out, "-200") ||
		!strings.Contains(out, "hot-a") ||
		!strings.Contains(out, "900") {
		t.Fatalf("csv output: %q", out)
	}
	if err := WriteProxyFlightGainCSV(promptCacheErrWriter{}, ProxyFlightGainSummary{}); err == nil {
		t.Fatal("expected writer error")
	}
}

func TestSortedPromptCacheHeatTieBreaks(t *testing.T) {
	t.Parallel()
	if rows := sortedPromptCacheHeat(nil); rows != nil {
		t.Fatalf("empty heat map should return nil, got %+v", rows)
	}

	rows := sortedPromptCacheHeat(map[string]*PromptCacheHeatRow{
		"b": {StablePrefixHash: "b", Requests: 1, HintsApplied: 1},
		"a": {StablePrefixHash: "a", Requests: 1, HintsApplied: 1},
		"c": {StablePrefixHash: "c", Requests: 1, CacheReadTokens: 1},
		"d": {StablePrefixHash: "d", Requests: 1, HintsApplied: 2},
		"e": {StablePrefixHash: "e", Requests: 2, HintsApplied: 1},
	})
	if got := []string{rows[0].StablePrefixHash, rows[1].StablePrefixHash, rows[2].StablePrefixHash, rows[3].StablePrefixHash, rows[4].StablePrefixHash}; got[0] != "c" || got[1] != "d" || got[2] != "e" || got[3] != "a" || got[4] != "b" {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestAccumulatePromptCacheHeatEmptyInputs(t *testing.T) {
	t.Parallel()
	heat := map[string]*PromptCacheHeatRow{}
	accumulatePromptCacheHeat(heat, nil)
	accumulatePromptCacheHeat(heat, &dbg.FlightRequestSummary{})
	if len(heat) != 0 {
		t.Fatalf("empty heat inputs should not create rows: %+v", heat)
	}
}

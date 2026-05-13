package debug

import (
	"strings"
	"testing"
	"time"
)

func TestBuildFlightRequestSummary(t *testing.T) {
	t.Parallel()
	summary := RequestSummary{
		RequestID:     "req-1",
		Timestamp:     time.Unix(1700000000, 0).UTC(),
		SessionID:     "session-1",
		TurnID:        "turn-1",
		Source:        "transparent_connect",
		Provider:      "openai",
		Host:          "chatgpt.com",
		Path:          "/backend-api/dev",
		ClientFamily:  "codex_app",
		RouteMode:     "mitm",
		BypassReason:  "none",
		LayersApplied: []int{1, 2},
		Tokens: TokenCounts{
			Original: 1000,
			Final:    600,
			Saved:    400,
			Ratio:    0.6,
		},
		CacheReadTokens:        250,
		CacheCreateTokens:      50,
		ProviderInputTokens:    700,
		ProviderCachedTokens:   250,
		ProviderOutputTokens:   120,
		OutputReduce:           OutputReduceSummary{Applied: true, Profile: "codex", Reason: "applied", AddedTokens: 12},
		PreviousResponseIDUsed: true,
		Errors:                 []string{"recoverable"},
		ProxyLatencyMs:         12.5,
	}

	flight := BuildFlightRequestSummary(summary)
	if flight.SchemaVersion != FlightSchemaVersion || flight.RequestID != "req-1" {
		t.Fatalf("bad flight identity: %+v", flight)
	}
	if flight.TokenAccounting.EstimatedOriginalInputTokens != 1000 ||
		flight.TokenAccounting.EstimatedFinalInputTokens != 600 ||
		flight.TokenAccounting.ProviderCachedTokens != 250 ||
		flight.TokenAccounting.ProviderOutputTokens != 120 ||
		flight.TokenAccounting.BillableSavingsEstimate != 400 {
		t.Fatalf("bad token accounting: %+v", flight.TokenAccounting)
	}
	if !flight.CacheAccounting.PreviousResponseIDUsed || flight.CacheAccounting.PreviousResponseIDBillable {
		t.Fatalf("bad previous_response_id accounting: %+v", flight.CacheAccounting)
	}
	if !flight.OutputReduce.Applied || flight.OutputReduce.AddedTokens != 12 {
		t.Fatalf("bad output reduce accounting: %+v", flight.OutputReduce)
	}
	if !flight.PrivacyRedacted || flight.Confidence != "provider_reported" {
		t.Fatalf("bad privacy/confidence: redacted=%v confidence=%s", flight.PrivacyRedacted, flight.Confidence)
	}
	if len(flight.Events) < 5 {
		t.Fatalf("expected flight event chain, got %+v", flight.Events)
	}
}

func TestEnsureFlightDefaultsAndCacheDecisions(t *testing.T) {
	t.Parallel()
	if got := cacheDecision(RequestSummary{}); got != "observed" {
		t.Fatalf("empty cache decision = %q, want observed", got)
	}
	cases := []struct {
		name    string
		summary RequestSummary
		want    string
	}{
		{name: "local cache", summary: RequestSummary{RequestID: "r1", CacheHit: true}, want: "local_cache_hit"},
		{name: "provider read", summary: RequestSummary{RequestID: "r2", CacheReadTokens: 10}, want: "provider_cache_read"},
		{name: "provider create", summary: RequestSummary{RequestID: "r3", CacheCreateTokens: 10}, want: "provider_cache_create"},
		{name: "output skipped", summary: RequestSummary{RequestID: "r4", OutputReduce: OutputReduceSummary{Reason: "below_min"}}, want: "skipped"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.summary.EnsureFlight()
			if tc.summary.Flight == nil {
				t.Fatal("flight missing")
			}
			var decisions []string
			for _, event := range tc.summary.Flight.Events {
				decisions = append(decisions, event.Decision)
			}
			if !strings.Contains(strings.Join(decisions, ","), tc.want) {
				t.Fatalf("decisions=%v want %s", decisions, tc.want)
			}
		})
	}
}

func TestEnsureFlightKeepsExisting(t *testing.T) {
	t.Parallel()
	existing := &FlightRequestSummary{SchemaVersion: 99, RequestID: "kept"}
	summary := RequestSummary{RequestID: "req", Flight: existing}
	summary.EnsureFlight()
	if summary.Flight != existing || summary.Flight.SchemaVersion != 99 {
		t.Fatalf("existing flight should be kept: %+v", summary.Flight)
	}
}

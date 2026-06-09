package debug

import (
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/evidence"
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
		PromptCache:            PromptCacheSummary{Applied: true, Reason: "applied", KeySet: true, Retention: "24h", StablePrefixHash: "abc123", StablePrefixTokens: 444},
		ToolPrune:              ToolPruneSummary{Applied: true, Reason: "idle_tools", PrunedTools: 3, AlwaysKept: 2, SavedTokens: 120, Reattached: 1, Miss: true, Retry: true, Cooldown: true},
		OutputReduce:           OutputReduceSummary{Applied: true, Profile: "codex", Reason: "applied", AddedTokens: 12},
		PreviousResponseIDUsed: true,
		EvidenceDecisions: []evidence.BlockDecision{{
			Layer:             0,
			Mechanism:         "git_diff",
			ContentClass:      evidence.ContentDiff,
			SafetyClass:       evidence.SafetyStructuredEvidence,
			Action:            evidence.ActionApplied,
			Reason:            "matched",
			Signals:           []evidence.Signal{evidence.SignalChangedHunk},
			PreservedEvidence: []string{"file path", "hunk header"},
			NetTokens:         123,
		}},
		Plan: &PlanSummary{
			Provider:      "openai",
			Model:         "gpt-5",
			RouteMode:     "mitm",
			SafetyBlocked: false,
			Decisions: []PlanDecisionSummary{{
				Layer:                 "l1",
				Action:                "run",
				Reason:                "large_or_structured_input",
				ExpectedSavingsTokens: 200,
				Risk:                  "medium",
				Confidence:            "unknown",
			}},
		},
		Errors:         []string{"recoverable"},
		ProxyLatencyMs: 12.5,
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
	if !flight.CacheAccounting.PromptCacheHintApplied ||
		flight.CacheAccounting.PromptCacheHintReason != "applied" ||
		flight.CacheAccounting.PromptCacheStablePrefixTokens != 444 {
		t.Fatalf("bad prompt-cache accounting: %+v", flight.CacheAccounting)
	}
	if !flight.ToolPrune.Applied ||
		flight.ToolPrune.SavedTokens != 120 ||
		!flight.ToolPrune.Miss ||
		!flight.ToolPrune.Retry ||
		!flight.ToolPrune.Cooldown {
		t.Fatalf("bad tool-prune accounting: %+v", flight.ToolPrune)
	}
	if !flight.OutputReduce.Applied || flight.OutputReduce.AddedTokens != 12 {
		t.Fatalf("bad output reduce accounting: %+v", flight.OutputReduce)
	}
	if flight.Plan == nil || len(flight.Plan.Decisions) != 1 || flight.Plan.Decisions[0].Layer != "l1" {
		t.Fatalf("bad planner summary: %+v", flight.Plan)
	}
	if len(flight.EvidenceDecisions) != 2 ||
		!hasFlightEvidenceMechanism(flight.EvidenceDecisions, "git_diff") ||
		!hasFlightEvidenceMechanism(flight.EvidenceDecisions, "provider_prompt_cache") {
		t.Fatalf("bad evidence manifest: %+v", flight.EvidenceDecisions)
	}
	summary.EvidenceDecisions[0].Signals[0] = evidence.SignalWarning
	if flight.EvidenceDecisions[0].Signals[0] != evidence.SignalChangedHunk {
		t.Fatalf("evidence manifest should be cloned, got %+v", flight.EvidenceDecisions)
	}
	summary.Plan.Decisions[0].Layer = "mutated"
	if flight.Plan.Decisions[0].Layer != "l1" {
		t.Fatalf("planner summary should be cloned, got %+v", flight.Plan.Decisions)
	}
	if !flight.PrivacyRedacted || flight.Confidence != "provider_reported" {
		t.Fatalf("bad privacy/confidence: redacted=%v confidence=%s", flight.PrivacyRedacted, flight.Confidence)
	}
	if len(flight.Events) < 5 {
		t.Fatalf("expected flight event chain, got %+v", flight.Events)
	}
	if !hasFlightStage(flight.Events, "planner", "advice_ready") {
		t.Fatalf("missing planner advice event: %+v", flight.Events)
	}
	if !hasFlightStage(flight.Events, "prompt_cache", "applied") {
		t.Fatalf("missing prompt-cache event: %+v", flight.Events)
	}
	if !hasFlightStage(flight.Events, "tool_prune", "applied") {
		t.Fatalf("missing tool-prune event: %+v", flight.Events)
	}
	if !hasFlightStage(flight.Events, "evidence", "manifest_recorded") {
		t.Fatalf("missing evidence event: %+v", flight.Events)
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

func TestPlannerHelpers(t *testing.T) {
	t.Parallel()
	if got := plannerDecision(&PlanSummary{SafetyBlocked: true}); got != "blocked" {
		t.Fatalf("plannerDecision blocked = %q", got)
	}
	if got := boolString(false); got != "false" {
		t.Fatalf("boolString false = %q", got)
	}
	if got := boolString(true); got != "true" {
		t.Fatalf("boolString true = %q", got)
	}
	if got := intString(0); got != "0" {
		t.Fatalf("intString zero = %q", got)
	}
	if got := intString(12345); got != "12345" {
		t.Fatalf("intString = %q", got)
	}
}

func hasFlightStage(events []FlightEvent, stage, decision string) bool {
	for _, event := range events {
		if event.Stage == stage && event.Decision == decision {
			return true
		}
	}
	return false
}

func hasFlightEvidenceMechanism(decisions []evidence.BlockDecision, mechanism string) bool {
	for _, decision := range decisions {
		if decision.Mechanism == mechanism {
			return true
		}
	}
	return false
}

package planner

import "testing"

func BenchmarkPlan_CodexWSSLargeToolOutput(b *testing.B) {
	facts := RequestFacts{
		Provider:                    "openai",
		Model:                       "gpt-5-codex",
		RouteMode:                   "wss_phasef",
		EstimatedInputTokens:        32000,
		ExpectedOutputTokens:        1200,
		TaskShape:                   "coding",
		ContentClasses:              []string{"tool_output", "source_file", "log", "repeated_tool_output"},
		ProviderCacheSupported:      true,
		PreviousResponseIDAvailable: true,
		WebSocketShapeKnown:         true,
		WebSocketMutationRequested:  true,
		LiveCorpusConfidence:        "medium",
		LatencyBudgetMs:             25,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		plan := Plan(facts)
		if len(plan.Decisions) == 0 {
			b.Fatal("empty plan")
		}
	}
}

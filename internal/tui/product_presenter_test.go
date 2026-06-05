package tui

import (
	"strings"
	"testing"
)

func TestPresentProductStatusSaving(t *testing.T) {
	t.Parallel()
	panel := PresentProductStatus(ProductStatus{
		RouteStatus:                "WSS savings active",
		FallbackReason:             "bridge while repair runs",
		RecertStatus:               "running",
		SavingsStatus:              "saving",
		BillableInputTokensSaved:   12000,
		RequestSideBytesReduced:    1536,
		OutputWireBytesSaved:       2048,
		ProviderCacheReadTokens:    5000,
		ProviderCacheCreateTokens:  700,
		CacheHits:                  3,
		CacheMisses:                1,
		ReadDeltaHits:              2,
		RepeatedOutputHits:         1,
		ChunkDedupHits:             1,
		ToolPruneTokensSaved:       26,
		ToolPrunePrunedTools:       1,
		ToolPruneReattached:        1,
		OutputReduceInjectedTurns:  1,
		OutputReduceObservedTokens: 42,
		OutputReduceInputOverhead:  9,
	})

	for _, want := range []string{
		"saving",
		"WSS savings active",
		"fallback: bridge while repair runs",
		"recert running",
		"12.0K input saved",
		"1.5K request",
		"2.0K output-wire saved",
		"5.0K provider-cache read",
		"700 create",
		"cache 3/4",
		"tool-prune 26 input saved",
		"1 pruned",
		"1 reattach",
		"output-reduce 1 inj",
		"42 out",
		"+9 input",
	} {
		if !strings.Contains(productPanelText(panel), want) {
			t.Fatalf("presenter missing %q in %+v", want, panel)
		}
	}
	if panel.SafetyNeedsWarning {
		t.Fatalf("saving presenter should not warn: %+v", panel)
	}
}

func TestPresentProductStatusAttentionKeepsDebugInternalsOut(t *testing.T) {
	t.Parallel()
	panel := PresentProductStatus(ProductStatus{
		RouteStatus:            "WSS bridge",
		SavingsStatus:          "attention",
		SafetyIssues:           1,
		ToolResolutionMisses:   1,
		WSSParseFailures:       1,
		WSSDegradedSessions:    1,
		WSSCompressionErrors:   1,
		WSSCompressedMutated:   99,
		WSSCompressedInspected: 123,
		WSSByteBridgeOnly:      true,
		WSSMutationActive:      true,
	})
	text := productPanelText(panel)
	for _, want := range []string{"attention", "1 safety issue", "1 tool miss", "1 WSS parse", "1 WSS degraded", "1 WSS compression"} {
		if !strings.Contains(text, want) {
			t.Fatalf("presenter missing %q in %+v", want, panel)
		}
	}
	for _, blocked := range []string{"99", "123", "byte", "mutation"} {
		if strings.Contains(text, blocked) {
			t.Fatalf("presenter leaked debug-only WSS internal %q in %q", blocked, text)
		}
	}
}

func productPanelText(panel ProductPanel) string {
	return strings.Join([]string{
		panel.RouteState,
		panel.RouteLine,
		panel.InputSavedLine,
		panel.RequestReducedLine,
		panel.OutputWireLine,
		panel.ProviderCacheLine,
		panel.ProviderCreateLine,
		panel.CacheLine,
		panel.ToolPruneLine,
		panel.OutputReduceLine,
		panel.SafetyLine,
	}, "\n")
}

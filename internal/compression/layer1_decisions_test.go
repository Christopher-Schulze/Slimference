package compression

import (
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestBuildLayer1DecisionRecordsUsesRegistryAndCounters(t *testing.T) {
	t.Parallel()
	result := Layer1Result{
		JSONSaved:               12,
		DedupSaved:              20,
		NearDedupSaved:          8,
		ToolCompressorSaved:     30,
		ToolOutputInWindowSaved: 7,
		Attempts: map[string]int{
			"json_compact":          1,
			"dedup":                 1,
			"dedup_near":            1,
			"tool_compressor":       1,
			"tool_output_in_window": 1,
			"structure_extract":     1,
			"loop_nudge":            1,
		},
		ArchiveWrites: map[string]int{
			"dedup_near":            1,
			"tool_output_in_window": 2,
		},
	}

	records := BuildLayer1DecisionRecords(result)
	if len(records) != len(Layer1SubLayerRegistry()) {
		t.Fatalf("records=%d registry=%d", len(records), len(Layer1SubLayerRegistry()))
	}

	byID := map[string]Layer1DecisionRecord{}
	for _, record := range records {
		if record.SubLayer == "" || record.Tier == "" || record.Reason == "" {
			t.Fatalf("incomplete decision record: %+v", record)
		}
		byID[record.SubLayer] = record
	}
	if got := byID["json_compact"]; !got.Applied || got.SavedTokens != 12 || got.Reason != "applied_positive_savings" {
		t.Fatalf("json decision = %+v", got)
	}
	if got := byID["dedup"]; !got.Applied || got.SavedTokens != 12 || got.RequiresArchive {
		t.Fatalf("exact dedup decision = %+v", got)
	}
	if got := byID["dedup_near"]; !got.Applied || got.SavedTokens != 8 || !got.RequiresArchive || got.ArchiveWrites != 1 {
		t.Fatalf("near dedup decision = %+v", got)
	}
	if got := byID["tool_compressor"]; !got.Applied || got.SavedTokens != 23 {
		t.Fatalf("tool compressor should exclude in-window attribution: %+v", got)
	}
	if got := byID["tool_output_in_window"]; !got.Applied || got.SavedTokens != 7 || got.ArchiveWrites != 2 {
		t.Fatalf("tool output in-window decision = %+v", got)
	}
	if got := byID["structure_extract"]; !got.Attempted || !got.RequiresArchive || got.Reason != "full_pass_or_archive_unavailable" {
		t.Fatalf("archive-required zero-savings reason = %+v", got)
	}
	if got := byID["loop_nudge"]; got.DefaultEligible || got.Reason != "not_default_eligible" {
		t.Fatalf("non-default zero-savings reason = %+v", got)
	}
	if got := byID["preview_pass"]; got.Attempted || got.Reason != "not_attempted" {
		t.Fatalf("unseen sub-layer should not be reported as attempted: %+v", got)
	}
}

func TestCompressWithSessionPopulatesLayer1Decisions(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	compressor := NewDeterministicCompressor(&cfg.Compression)

	result := compressor.CompressWithSession("sess-decisions", nil)
	if len(result.Decisions) != len(Layer1SubLayerRegistry()) {
		t.Fatalf("decisions=%d registry=%d", len(result.Decisions), len(Layer1SubLayerRegistry()))
	}
	for _, decision := range result.Decisions {
		if decision.Attempted {
			t.Fatalf("empty compression call must not mark %s attempted: %+v", decision.SubLayer, decision)
		}
		if decision.Reason != "not_attempted" {
			t.Fatalf("empty compression call reason for %s = %q", decision.SubLayer, decision.Reason)
		}
	}
}

func TestCompressWithSessionMarksOnlyReachedLayer1Attempts(t *testing.T) {
	t.Parallel()
	cfg := defaultTestCfg(1)
	compressor := NewDeterministicCompressor(cfg)
	msgs := []types.Message{
		buildMessage(t, 0, "user", toolResultBlock("{\n  \"a\": 1,\n  \"b\": 2\n}\n")),
		buildMessage(t, 1, "assistant", textBlock("done")),
		buildMessage(t, 2, "user", textBlock("latest exchange")),
	}

	result := compressor.CompressWithSession("sess-attempts", msgs)
	if result.JSONSaved <= 0 {
		t.Fatalf("JSONSaved=%d want >0", result.JSONSaved)
	}
	jsonDecision := findLayer1Decision(t, result.Decisions, "json_compact")
	if !jsonDecision.Attempted || !jsonDecision.Applied {
		t.Fatalf("json decision should be attempted and applied: %+v", jsonDecision)
	}
	previewDecision := findLayer1Decision(t, result.Decisions, "preview_pass")
	if previewDecision.Attempted || previewDecision.Reason != "not_attempted" {
		t.Fatalf("preview should stay unattempted on small JSON: %+v", previewDecision)
	}
}

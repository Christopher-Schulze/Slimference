package compression

import (
	"testing"

	"github.com/slimference/slimference/internal/config"
)

func TestBuildLayer1DecisionRecordsUsesRegistryAndCounters(t *testing.T) {
	t.Parallel()
	result := Layer1Result{
		JSONSaved:               12,
		ToolCompressorSaved:     30,
		ToolOutputInWindowSaved: 7,
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
	if got := byID["tool_compressor"]; !got.Applied || got.SavedTokens != 23 {
		t.Fatalf("tool compressor should exclude in-window attribution: %+v", got)
	}
	if got := byID["tool_output_in_window"]; !got.Applied || got.SavedTokens != 7 {
		t.Fatalf("tool output in-window decision = %+v", got)
	}
	if got := byID["structure_extract"]; !got.RequiresArchive || got.Reason != "full_pass_or_archive_unavailable" {
		t.Fatalf("archive-required zero-savings reason = %+v", got)
	}
	if got := byID["loop_nudge"]; got.DefaultEligible || got.Reason != "not_default_eligible" {
		t.Fatalf("non-default zero-savings reason = %+v", got)
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
}

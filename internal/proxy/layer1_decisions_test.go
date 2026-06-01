package proxy

import (
	"testing"

	"github.com/slimference/slimference/internal/compression"
)

func TestBuildLayer1DecisionsMapsCompressionRecords(t *testing.T) {
	t.Parallel()
	result := compression.Layer1Result{
		Decisions: []compression.Layer1DecisionRecord{
			{
				SubLayer:        "structure_extract",
				Tier:            compression.Layer1SafetyRecoverableWithArchive,
				Attempted:       true,
				Applied:         true,
				Reason:          "applied_positive_savings",
				SavedTokens:     42,
				RequiresArchive: true,
				Recovery:        "content archive",
				DefaultEligible: true,
			},
		},
	}

	got := buildLayer1Decisions(result)
	if len(got) != 1 {
		t.Fatalf("decisions=%d", len(got))
	}
	if got[0].SubLayer != "structure_extract" ||
		got[0].Tier != "recoverable_with_archive" ||
		!got[0].Attempted ||
		!got[0].Applied ||
		got[0].SavedTokens != 42 ||
		!got[0].RequiresArchive ||
		got[0].Recovery != "content archive" ||
		!got[0].DefaultEligible {
		t.Fatalf("bad decision mapping: %+v", got[0])
	}
}

func TestBuildLayer1DecisionsEmpty(t *testing.T) {
	t.Parallel()
	if got := buildLayer1Decisions(compression.Layer1Result{}); got != nil {
		t.Fatalf("empty decisions should map to nil, got %+v", got)
	}
}

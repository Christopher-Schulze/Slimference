package config

import "testing"

// TestT54_MinTokensForLayer2DefaultLoweredTo15k pins the T54 contract: the
// pre-T54 default of 30000 was too conservative; data in
// docs/savings-assessment.md pointed to a measurable token-savings gap in
// the 15-30k range. Flipping the default back to 30k is a deliberate
// breaking change and must be coupled to a changelog entry.
func TestT54_MinTokensForLayer2DefaultLoweredTo15k(t *testing.T) {
	cfg := Defaults()
	if cfg.Compression.MinTokensForLayer2 != 15000 {
		t.Fatalf("min_tokens_for_layer2 = %d, want 15000 (T54 default flip)",
			cfg.Compression.MinTokensForLayer2)
	}
}

// TestT54_LatencyBudgetDefaultsPresent verifies the latency-guard fields
// ship with sensible defaults so enabling the guard is a single-field
// override.
func TestT54_LatencyBudgetDefaultsPresent(t *testing.T) {
	cfg := Defaults()
	if cfg.Compression.Layer2LatencyBudgetMs != 0 {
		t.Errorf("budget = %d, want 0 (off by default)",
			cfg.Compression.Layer2LatencyBudgetMs)
	}
	if cfg.Compression.Layer2LatencyProjectionMultiplier != 1.2 {
		t.Errorf("multiplier = %v, want 1.2",
			cfg.Compression.Layer2LatencyProjectionMultiplier)
	}
	if cfg.Compression.Layer2LatencyEMAAlpha != 0.2 {
		t.Errorf("alpha = %v, want 0.2", cfg.Compression.Layer2LatencyEMAAlpha)
	}
}

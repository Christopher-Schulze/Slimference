package tui

import (
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// TestProviderTTFTSaving verifies the TTFT saving helper for all branches.
func TestProviderTTFTSaving(t *testing.T) {
	t.Parallel()

	// Zero prefill speed -> 0.
	snap := analytics.AnalyticsSnapshot{}
	if got := providerTTFTSaving(snap, types.Anthropic, 0); got != 0 {
		t.Errorf("prefillSpeed=0: got %v, want 0", got)
	}

	// Negative prefill speed -> 0.
	if got := providerTTFTSaving(snap, types.Anthropic, -1); got != 0 {
		t.Errorf("prefillSpeed=-1: got %v, want 0", got)
	}

	// Provider not in PerProvider -> 0.
	if got := providerTTFTSaving(snap, types.Anthropic, 1000); got != 0 {
		t.Errorf("missing provider: got %v, want 0", got)
	}

	// Provider present but Messages=0 -> 0.
	snapWithProv := analytics.AnalyticsSnapshot{
		PerProvider: map[types.Provider]analytics.ProviderStats{
			types.Anthropic: {Messages: 0, InputTokensSaved: 0},
		},
	}
	if got := providerTTFTSaving(snapWithProv, types.Anthropic, 1000); got != 0 {
		t.Errorf("Messages=0: got %v, want 0", got)
	}

	// Normal case: 2000 tokens saved over 2 messages = 1000 avg saved.
	// prefillSpeed 50000 -> 1000/50000 = 0.02s.
	snapNormal := analytics.AnalyticsSnapshot{
		PerProvider: map[types.Provider]analytics.ProviderStats{
			types.Anthropic: {Messages: 2, InputTokensSaved: 2000},
		},
	}
	got := providerTTFTSaving(snapNormal, types.Anthropic, 50000)
	want := 0.02
	if got < want-0.0001 || got > want+0.0001 {
		t.Errorf("providerTTFTSaving = %f, want %f", got, want)
	}

	// OpenAI not in snapshot -> 0.
	if got := providerTTFTSaving(snapNormal, types.OpenAI, 50000); got != 0 {
		t.Errorf("OpenAI not in snapshot: got %v, want 0", got)
	}
}

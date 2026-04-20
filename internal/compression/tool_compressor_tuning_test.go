package compression

import (
	"testing"
)

func TestDefaultToolCompressorTuning_Values(t *testing.T) {
	def := DefaultToolCompressorTuning()
	if def.AggressiveAfterMultiplier != 2 {
		t.Errorf("multiplier = %d, want 2", def.AggressiveAfterMultiplier)
	}
	if def.GitModerateDiffLimit != 60 {
		t.Errorf("git limit = %d, want 60", def.GitModerateDiffLimit)
	}
	if def.TestMaxFailureLines != 40 {
		t.Errorf("test limit = %d, want 40", def.TestMaxFailureLines)
	}
}

func TestSetToolCompressorTuning_ZeroFieldsFallBackToDefaults(t *testing.T) {
	// Restore defaults after the test so we do not leak state into
	// subsequent tests that compare against the pre-T61 numbers.
	t.Cleanup(func() { SetToolCompressorTuning(DefaultToolCompressorTuning()) })

	SetToolCompressorTuning(ToolCompressorTuning{
		GitModerateDiffLimit: 120,
		// AggressiveAfterMultiplier and TestMaxFailureLines zero -> defaults.
	})
	cur := currentToolTuning()
	if cur.AggressiveAfterMultiplier != 2 {
		t.Errorf("zero multiplier: got %d, want default 2", cur.AggressiveAfterMultiplier)
	}
	if cur.GitModerateDiffLimit != 120 {
		t.Errorf("git limit: got %d, want 120", cur.GitModerateDiffLimit)
	}
	if cur.TestMaxFailureLines != 40 {
		t.Errorf("zero test limit: got %d, want default 40", cur.TestMaxFailureLines)
	}
}

func TestSetToolCompressorTuning_ConcurrentReadsSafe(t *testing.T) {
	t.Cleanup(func() { SetToolCompressorTuning(DefaultToolCompressorTuning()) })
	// Interleave writes with reads to make sure the atomic pointer handles
	// concurrent access without tearing.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			_ = currentToolTuning().GitModerateDiffLimit
		}
	}()
	for i := 0; i < 1000; i++ {
		SetToolCompressorTuning(ToolCompressorTuning{
			AggressiveAfterMultiplier: 1 + i%5,
			GitModerateDiffLimit:      10 + i%100,
			TestMaxFailureLines:       5 + i%90,
		})
	}
	<-done
}

func TestT61_DefaultTuningMatchesPreT61Constants(t *testing.T) {
	t.Cleanup(func() { SetToolCompressorTuning(DefaultToolCompressorTuning()) })
	// Legacy callers that never call SetToolCompressorTuning must see the
	// original hardcoded values (60 / 40 / 2). This is the byte-equal
	// regression guard.
	cur := currentToolTuning()
	if cur.AggressiveAfterMultiplier != 2 ||
		cur.GitModerateDiffLimit != 60 ||
		cur.TestMaxFailureLines != 40 {
		t.Fatalf("default tuning drifted: %+v", cur)
	}
}

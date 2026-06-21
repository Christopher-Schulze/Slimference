package quality

import (
	"sync"
	"testing"
)

func TestLoopBreakTracker_Record(t *testing.T) {
	tr := NewLoopBreakTracker()
	tr.Record(LoopNudgeMeasurement{
		StreakLen:           5,
		NudgeEmitted:        true,
		LoopBroken:          true,
		ObservedSavedTokens: 1200,
	}, "additive")
	tr.Record(LoopNudgeMeasurement{
		StreakLen:           4,
		NudgeEmitted:        true,
		LoopBroken:          false,
		ObservedSavedTokens: 0,
	}, "additive")
	tr.Record(LoopNudgeMeasurement{
		StreakLen:           6,
		NudgeEmitted:        true,
		LoopBroken:          true,
		ObservedSavedTokens: 800,
	}, "subtractive")

	stats := tr.Stats()
	if stats.DetectedTotal != 3 {
		t.Fatalf("detected=%d want 3", stats.DetectedTotal)
	}
	if stats.BrokenTotal != 2 {
		t.Fatalf("broken=%d want 2", stats.BrokenTotal)
	}
	if stats.BrokenRate < 0.66 || stats.BrokenRate > 0.67 {
		t.Fatalf("rate=%v want ~0.667", stats.BrokenRate)
	}
	if stats.AvgSaved < 600 || stats.AvgSaved > 700 {
		t.Fatalf("avg_saved=%v want ~667", stats.AvgSaved)
	}
	addStats, ok := stats.ByStrategy["additive"]
	if !ok {
		t.Fatal("missing additive strategy")
	}
	if addStats.Detected != 2 {
		t.Fatalf("additive detected=%d", addStats.Detected)
	}
	subStats := stats.ByStrategy["subtractive"]
	if subStats.Detected != 1 {
		t.Fatalf("subtractive detected=%d", subStats.Detected)
	}
}

func TestLoopBreakTracker_Reset(t *testing.T) {
	tr := NewLoopBreakTracker()
	tr.Record(LoopNudgeMeasurement{LoopBroken: true}, "additive")
	tr.Reset()
	stats := tr.Stats()
	if stats.DetectedTotal != 0 {
		t.Fatal("should be zero after reset")
	}
}

func TestLoopBreakTracker_Empty(t *testing.T) {
	tr := NewLoopBreakTracker()
	stats := tr.Stats()
	if stats.DetectedTotal != 0 {
		t.Fatal("empty should be 0")
	}
	if stats.BrokenRate != 0 {
		t.Fatal("empty rate should be 0")
	}
}

func TestLoopBreakTracker_Concurrent(t *testing.T) {
	tr := NewLoopBreakTracker()
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			tr.Record(LoopNudgeMeasurement{
				LoopBroken:          true,
				ObservedSavedTokens: 100,
			}, "additive")
		})
	}
	wg.Wait()
	stats := tr.Stats()
	if stats.DetectedTotal != 100 {
		t.Fatalf("detected=%d want 100", stats.DetectedTotal)
	}
}

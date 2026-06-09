package tokens

import (
	"sync"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// TestUsageTracker_Record_IncrementsCounters verifies basic counter accumulation.
func TestUsageTracker_Record_IncrementsCounters(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)

	tr.Record(types.Anthropic, 1000, 700, 200)

	if tr.MessagesSent != 1 {
		t.Errorf("MessagesSent = %d, want 1", tr.MessagesSent)
	}
	if tr.InputTokensOrig != 1000 {
		t.Errorf("InputTokensOrig = %d, want 1000", tr.InputTokensOrig)
	}
	if tr.InputTokensComp != 700 {
		t.Errorf("InputTokensComp = %d, want 700", tr.InputTokensComp)
	}
	if tr.OutputTokens != 200 {
		t.Errorf("OutputTokens = %d, want 200", tr.OutputTokens)
	}
}

// TestUsageTracker_MultipleRecords verifies accumulation across multiple calls.
func TestUsageTracker_MultipleRecords(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)

	tr.Record(types.Anthropic, 1000, 800, 100)
	tr.Record(types.OpenAI, 2000, 1500, 200)
	tr.Record(types.Anthropic, 500, 400, 50)

	if tr.MessagesSent != 3 {
		t.Errorf("MessagesSent = %d, want 3", tr.MessagesSent)
	}
	if tr.InputTokensOrig != 3500 {
		t.Errorf("InputTokensOrig = %d, want 3500", tr.InputTokensOrig)
	}
	if tr.InputTokensComp != 2700 {
		t.Errorf("InputTokensComp = %d, want 2700", tr.InputTokensComp)
	}
	if tr.OutputTokens != 350 {
		t.Errorf("OutputTokens = %d, want 350", tr.OutputTokens)
	}
}

// TestUsageTracker_PerProvider verifies per-provider breakdown.
func TestUsageTracker_PerProvider(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)

	tr.Record(types.Anthropic, 1000, 800, 100)
	tr.Record(types.Anthropic, 500, 400, 50)
	tr.Record(types.OpenAI, 2000, 1800, 200)

	ap, ok := tr.PerProvider[types.Anthropic]
	if !ok {
		t.Fatal("no Anthropic stats")
	}
	if ap.Messages != 2 {
		t.Errorf("Anthropic Messages = %d, want 2", ap.Messages)
	}
	if ap.TokensSaved != 300 { // (1000-800) + (500-400)
		t.Errorf("Anthropic TokensSaved = %d, want 300", ap.TokensSaved)
	}

	op, ok := tr.PerProvider[types.OpenAI]
	if !ok {
		t.Fatal("no OpenAI stats")
	}
	if op.Messages != 1 {
		t.Errorf("OpenAI Messages = %d, want 1", op.Messages)
	}
	if op.TokensSaved != 200 { // 2000-1800
		t.Errorf("OpenAI TokensSaved = %d, want 200", op.TokensSaved)
	}
}

// TestUsageTracker_EstExtraMessages verifies the estimation formula.
func TestUsageTracker_EstExtraMessages(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)

	// Record several requests to build up an average.
	tr.Record(types.Anthropic, 10000, 7000, 500)
	tr.Record(types.Anthropic, 10000, 7000, 500)
	tr.Record(types.Anthropic, 10000, 7000, 500)

	// Saved: 9000 total. Avg compressed per req: 7000. Extra = 9000/7000 = 1.
	extra := tr.EstExtraMessages()
	if extra <= 0 {
		t.Errorf("EstExtraMessages() = %d, want > 0 (saved tokens should fund extra messages)", extra)
	}
}

// TestUsageTracker_EstExtraMessages_NoSavings verifies zero extra when no tokens saved.
func TestUsageTracker_EstExtraMessages_NoSavings(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)
	// No compression: orig == comp
	tr.Record(types.Anthropic, 1000, 1000, 100)

	extra := tr.EstExtraMessages()
	if extra != 0 {
		t.Errorf("EstExtraMessages() = %d, want 0 when no savings", extra)
	}
}

// TestUsageTracker_AvgTTFTImprovement verifies TTFT estimation.
func TestUsageTracker_AvgTTFTImprovement(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)

	// 5000 tokens saved in 1 request -> avg saved = 5000 -> TTFT = 5000/50000 = 0.1s
	tr.Record(types.Anthropic, 10000, 5000, 500)

	got := tr.AvgTTFTImprovement()
	want := 0.1
	if got < want-0.001 || got > want+0.001 {
		t.Errorf("AvgTTFTImprovement() = %f, want ~%f", got, want)
	}
}

// TestUsageTracker_AvgTTFTImprovement_ZeroSpeed verifies zero when prefill speed is 0.
func TestUsageTracker_AvgTTFTImprovement_ZeroSpeed(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(0)
	tr.Record(types.Anthropic, 10000, 5000, 500)

	if got := tr.AvgTTFTImprovement(); got != 0 {
		t.Errorf("AvgTTFTImprovement() with speed=0 = %f, want 0", got)
	}
}

// TestUsageTracker_CompressionRatio verifies the ratio calculation.
func TestUsageTracker_CompressionRatio(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)

	tr.Record(types.Anthropic, 1000, 700, 100)

	ratio := tr.CompressionRatio()
	want := 0.7
	if ratio < want-0.001 || ratio > want+0.001 {
		t.Errorf("CompressionRatio() = %f, want ~%f", ratio, want)
	}
}

// TestUsageTracker_CompressionRatio_NoRequests verifies default ratio with no requests.
func TestUsageTracker_CompressionRatio_NoRequests(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)
	if r := tr.CompressionRatio(); r != 1.0 {
		t.Errorf("CompressionRatio() = %f, want 1.0 with no requests", r)
	}
}

// TestUsageTracker_Snapshot verifies that Snapshot returns an independent copy.
func TestUsageTracker_Snapshot(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)
	tr.Record(types.Anthropic, 1000, 800, 100)

	snap := tr.Snapshot()
	if snap.MessagesSent != 1 {
		t.Errorf("snap.MessagesSent = %d, want 1", snap.MessagesSent)
	}
	if snap.InputTokensOrig != 1000 {
		t.Errorf("snap.InputTokensOrig = %d, want 1000", snap.InputTokensOrig)
	}
	if snap.PrefillSpeed != 50000 {
		t.Errorf("snap.PrefillSpeed = %d, want 50000", snap.PrefillSpeed)
	}
	// Mutating the snapshot should not affect the tracker.
	snap.MessagesSent = 999
	if tr.MessagesSent == 999 {
		t.Error("mutating snapshot affected the tracker")
	}
}

// TestUsageTracker_Snapshot_DeepCopiesProviders verifies that the PerProvider map is deep-copied.
func TestUsageTracker_Snapshot_DeepCopiesProviders(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)
	tr.Record(types.Anthropic, 1000, 800, 100)

	snap := tr.Snapshot()
	ap := snap.PerProvider[types.Anthropic]
	ap.Messages = 9999
	if tr.PerProvider[types.Anthropic].Messages == 9999 {
		t.Error("mutating snapshot PerProvider affected tracker")
	}
}

// TestUsageTracker_ConcurrentRecord verifies thread-safety under concurrent writes.
func TestUsageTracker_ConcurrentRecord(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)
	const goroutines = 50
	const recordsEach = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < recordsEach; j++ {
				tr.Record(types.Anthropic, 100, 80, 10)
			}
		}()
	}
	wg.Wait()

	total := goroutines * recordsEach
	if tr.MessagesSent != total {
		t.Errorf("MessagesSent = %d, want %d after concurrent records", tr.MessagesSent, total)
	}
}

// TestUsageTracker_Record_OrigZero covers the ratio=1.0 branch (usage.go:86-88)
// when orig==0 so the else branch is taken.
func TestUsageTracker_Record_OrigZero(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)
	// orig=0 -> saved=0-comp (clamped to 0), ratio branch: else { ratio = 1.0 }
	tr.Record(types.Anthropic, 0, 0, 50)

	ap := tr.PerProvider[types.Anthropic]
	if ap == nil {
		t.Fatal("no Anthropic entry")
	}
	// AvgRatio should be 1.0 (from the else branch).
	if ap.AvgRatio < 0.999 || ap.AvgRatio > 1.001 {
		t.Errorf("AvgRatio = %v, want ~1.0 when orig==0", ap.AvgRatio)
	}
}

// TestUsageTracker_AvgTTFTImprovement_NoSavings covers the saved<=0 branch (usage.go:116-118).
func TestUsageTracker_AvgTTFTImprovement_NoSavings(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)
	// comp >= orig means no savings.
	tr.Record(types.Anthropic, 100, 200, 50)
	if got := tr.AvgTTFTImprovement(); got != 0 {
		t.Errorf("AvgTTFTImprovement with no savings = %v, want 0", got)
	}
}

// TestUsageTracker_Record_NegativeSavedClamp verifies that negative savings are clamped to 0.
func TestUsageTracker_Record_NegativeSavedClamp(t *testing.T) {
	t.Parallel()
	tr := NewUsageTracker(50000)

	// comp > orig (expansion, not compression) -> saved should be clamped to 0 for per-provider.
	tr.Record(types.Anthropic, 100, 200, 50)

	ap := tr.PerProvider[types.Anthropic]
	if ap.TokensSaved != 0 {
		t.Errorf("TokensSaved = %d, want 0 when comp > orig (clamped)", ap.TokensSaved)
	}
}

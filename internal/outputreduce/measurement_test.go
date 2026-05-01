package outputreduce

import "testing"

func TestTrackerSnapshot(t *testing.T) {
	t.Parallel()
	tr := NewTracker(true, "auto")
	tr.ObserveInjection(Stats{Applied: true, Profile: "openai", AddedTokens: 7, Reason: "applied"})
	tr.ObserveInjection(Stats{Applied: false, Profile: "openai", Reason: "already_present"})
	tr.ObserveOutput(30)
	tr.ObserveOutput(10)
	snap := tr.Snapshot()
	if !snap.Enabled || snap.Profile != "openai" || snap.InjectedTurns != 1 || snap.SkippedTurns != 1 {
		t.Fatalf("snapshot: %+v", snap)
	}
	if snap.InputOverheadTokens != 7 || snap.OutputTokensObserved != 40 || snap.AvgOutputTokens != 20 {
		t.Fatalf("snapshot: %+v", snap)
	}
}

func TestNilTrackerSafe(t *testing.T) {
	var tr *Tracker
	tr.ObserveInjection(Stats{Applied: true})
	tr.ObserveOutput(10)
	if snap := tr.Snapshot(); snap.Enabled {
		t.Fatalf("nil snapshot: %+v", snap)
	}
}

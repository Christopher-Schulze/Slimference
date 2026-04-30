package toolprune

import (
	"strconv"
	"testing"
)

func TestNewUsageTracker_DefaultThreshold(t *testing.T) {
	if u := NewUsageTracker(0); u.idleThreshold != 20 {
		t.Fatalf("default threshold: %d", u.idleThreshold)
	}
}

func TestObserveTurn_BumpsLastSeen(t *testing.T) {
	u := NewUsageTracker(5)
	u.ObserveTurn("s", []string{"Read", "Bash"})
	u.ObserveTurn("s", []string{"Read"})
	if !u.Active("s", "Read") {
		t.Fatal("Read should still be active")
	}
	if !u.Active("s", "Bash") {
		t.Fatal("Bash should be active inside window")
	}
}

func TestObserveTurn_EmptySessionNoop(t *testing.T) {
	u := NewUsageTracker(5)
	u.ObserveTurn("", []string{"Read"})
	if u.Snapshot().Sessions != 0 {
		t.Fatal("empty session must not create state")
	}
}

func TestObserveTurn_EmptyToolNamesSkipped(t *testing.T) {
	u := NewUsageTracker(5)
	u.ObserveTurn("s", []string{""})
	// Empty tool name is filtered out at observe time; the session
	// state exists but the tool map stays empty. Active("") returns
	// fail-open true because the empty-name early-return path fires.
	if !u.Active("s", "") {
		t.Fatal("empty tool name lookup must be fail-open active")
	}
}

func TestActive_OutsideWindowFalse(t *testing.T) {
	u := NewUsageTracker(2)
	u.ObserveTurn("s", []string{"Read"})
	u.ObserveTurn("s", []string{"Other"})
	u.ObserveTurn("s", []string{"Other"})
	u.ObserveTurn("s", []string{"Other"}) // turn 4, Read last seen at 1, distance 3 > 2
	if u.Active("s", "Read") {
		t.Fatal("Read should be outside window")
	}
}

func TestActive_UnknownReturnsTrue(t *testing.T) {
	u := NewUsageTracker(5)
	if !u.Active("missing", "Read") {
		t.Fatal("unknown session must fail-open active")
	}
	u.ObserveTurn("s", []string{"Read"})
	if !u.Active("s", "NeverSeen") {
		t.Fatal("unseen tool must fail-open active")
	}
}

func TestForget(t *testing.T) {
	u := NewUsageTracker(5)
	u.ObserveTurn("s", []string{"Read"})
	u.Forget("s")
	if u.Snapshot().Sessions != 0 {
		t.Fatal("forget did not clear")
	}
	u.Forget("") // no-op
}

func TestSessionEviction(t *testing.T) {
	u := NewUsageTracker(5)
	u.maxSessions = 2
	u.ObserveTurn("a", []string{"x"})
	u.ObserveTurn("b", []string{"x"})
	u.ObserveTurn("c", []string{"x"})
	if u.Snapshot().Sessions > 2 {
		t.Fatalf("eviction failed: %+v", u.Snapshot())
	}
}

func TestMarkPrunedAndReattached(t *testing.T) {
	u := NewUsageTracker(5)
	u.MarkPruned(120)
	u.MarkPruned(0)
	u.MarkPruned(-5)
	u.MarkReattached()
	stats := u.Snapshot()
	if stats.PrunedTotal != 3 || stats.ReattachTotal != 1 || stats.TokensSavedSum != 120 {
		t.Fatalf("counters: %+v", stats)
	}
}

func TestDecide_NoPruning(t *testing.T) {
	u := NewUsageTracker(5)
	u.ObserveTurn("s", []string{"a", "b", "c"})
	d := u.Decide("s", []string{"a", "b", "c"}, 0)
	if len(d.Keep) != 3 || len(d.Pruned) != 0 {
		t.Fatalf("decision: %+v", d)
	}
}

func TestDecide_PrunesIdleTools(t *testing.T) {
	u := NewUsageTracker(2)
	u.ObserveTurn("s", []string{"a", "b"})
	for i := 0; i < 5; i++ {
		u.ObserveTurn("s", []string{"a"}) // b drifts out of window
	}
	d := u.Decide("s", []string{"a", "b"}, 0)
	if len(d.Pruned) != 1 || d.Pruned[0] != "b" {
		t.Fatalf("expected b pruned: %+v", d)
	}
}

func TestDecide_EmptySession(t *testing.T) {
	u := NewUsageTracker(5)
	d := u.Decide("", []string{"a", "b"}, 0)
	if len(d.Keep) != 2 || len(d.Pruned) != 0 {
		t.Fatalf("empty session must keep all: %+v", d)
	}
}

func TestDecide_MinKeepRestoresMostRecentlyUsed(t *testing.T) {
	u := NewUsageTracker(2)
	// Seed three tools with distinct last-seen turns so the ranking
	// inside Decide must swap entries to put the most-recently-used
	// first.
	u.ObserveTurn("s", []string{"oldest"}) // turn 1
	u.ObserveTurn("s", []string{"older"})  // turn 2
	u.ObserveTurn("s", []string{"old"})    // turn 3
	// Drift everything out of the window with filler turns.
	u.ObserveTurn("s", []string{"filler"})
	u.ObserveTurn("s", []string{"filler"})
	u.ObserveTurn("s", []string{"filler"})

	d := u.Decide("s", []string{"oldest", "older", "old"}, 2)
	if len(d.Keep) != 2 {
		t.Fatalf("min keep floor: %+v", d)
	}
	// "old" was seen most recently among the originals, so it must be
	// in keep.
	hasOld := false
	for _, k := range d.Keep {
		if k == "old" {
			hasOld = true
		}
	}
	if !hasOld {
		t.Fatalf("most-recent tool dropped: %+v", d)
	}
}

func TestDecide_MinKeepFloor(t *testing.T) {
	u := NewUsageTracker(2)
	// All tools observed once, then drift out of window.
	u.ObserveTurn("s", []string{"oldest", "older", "old"})
	// Bump only "fresh" so the others go idle.
	u.ObserveTurn("s", []string{"fresh"})
	u.ObserveTurn("s", []string{"fresh"})
	u.ObserveTurn("s", []string{"fresh"})
	u.ObserveTurn("s", []string{"fresh"})

	tools := []string{"oldest", "older", "old", "fresh"}
	d := u.Decide("s", tools, 3)
	if len(d.Keep) != 3 {
		t.Fatalf("min keep floor: %+v", d)
	}
	// "fresh" must be in keep; the highest-priority pruned must be
	// the most recently used among the originals.
	hasFresh := false
	for _, k := range d.Keep {
		if k == "fresh" {
			hasFresh = true
		}
	}
	if !hasFresh {
		t.Fatalf("fresh tool dropped: %+v", d)
	}
}

func TestDecide_MinKeepWithUnknownSession(t *testing.T) {
	u := NewUsageTracker(2)
	d := u.Decide("never-observed", []string{"a", "b"}, 1)
	if len(d.Keep) != 2 {
		t.Fatalf("unknown session: %+v", d)
	}
}

func TestSnapshotBoundedAtCap(t *testing.T) {
	u := NewUsageTracker(5)
	u.maxSessions = 3
	for i := 0; i < 10; i++ {
		u.ObserveTurn("s-"+strconv.Itoa(i), []string{"x"})
	}
	if got := u.Snapshot().Sessions; got > 3 {
		t.Fatalf("cap not enforced: %d", got)
	}
}

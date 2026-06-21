package filetracker

import (
	"crypto/sha256"
	"sync"
	"testing"
)

func TestRecordReadAndGet(t *testing.T) {
	tr := New()
	tr.RecordRead("s1", "x.go", 3, []byte("hello"))
	st, ok := tr.Get("s1", "x.go")
	if !ok {
		t.Fatal("expected state to be tracked")
	}
	if st.Read.Turn != 3 {
		t.Errorf("turn=%d want 3", st.Read.Turn)
	}
	if st.Read.ContentLen != 5 {
		t.Errorf("len=%d want 5", st.Read.ContentLen)
	}
	wantHash := sha256.Sum256([]byte("hello"))
	if st.Read.ContentHash != wantHash {
		t.Errorf("content hash mismatch")
	}
}

func TestRecordMutationAfterRead(t *testing.T) {
	tr := New()
	tr.RecordRead("s1", "x.go", 3, []byte("v1"))
	tr.RecordMutation("s1", "x.go", 5, "apply_patch")
	st, _ := tr.Get("s1", "x.go")
	if !st.IsStale() {
		t.Errorf("read at turn 3 + mutation at turn 5 should be stale")
	}
	if st.Mutation.ToolName != "apply_patch" {
		t.Errorf("tool name=%q", st.Mutation.ToolName)
	}
}

func TestNotStaleWhenReadAfterMutation(t *testing.T) {
	tr := New()
	tr.RecordMutation("s1", "x.go", 3, "Edit")
	tr.RecordRead("s1", "x.go", 5, []byte("after"))
	st, _ := tr.Get("s1", "x.go")
	if st.IsStale() {
		t.Errorf("read at turn 5 after mutation at turn 3 should NOT be stale")
	}
}

func TestIsStaleWhenNeverRead(t *testing.T) {
	st := FileState{}
	if st.IsStale() {
		t.Errorf("never-read state should not be stale")
	}
}

func TestAgeInTurns(t *testing.T) {
	tr := New()
	tr.RecordRead("s1", "x.go", 5, []byte("body"))
	st, _ := tr.Get("s1", "x.go")
	if got := st.AgeInTurns(10); got != 5 {
		t.Errorf("age=%d want 5", got)
	}
	if got := st.AgeInTurns(5); got != 0 {
		t.Errorf("age=%d want 0 (same turn)", got)
	}
	if got := st.AgeInTurns(4); got != 0 {
		t.Errorf("age=%d want 0 (currentTurn < readTurn)", got)
	}
}

func TestAgeInTurnsNeverRead(t *testing.T) {
	st := FileState{}
	if got := st.AgeInTurns(10); got != 0 {
		t.Errorf("never-read should return 0, got %d", got)
	}
}

func TestEmptySessionIgnored(t *testing.T) {
	tr := New()
	tr.RecordRead("", "x.go", 1, []byte("x"))
	tr.RecordMutation("", "x.go", 1, "Edit")
	if tr.SessionCount() != 0 {
		t.Errorf("empty session should not be tracked")
	}
}

func TestEmptyPathIgnored(t *testing.T) {
	tr := New()
	tr.RecordRead("s1", "", 1, []byte("x"))
	tr.RecordMutation("s1", "", 1, "Edit")
	if tr.SessionCount() != 0 {
		t.Errorf("empty path should not allocate session")
	}
}

func TestGetMissingSession(t *testing.T) {
	tr := New()
	if _, ok := tr.Get("nope", "x.go"); ok {
		t.Errorf("missing session should return false")
	}
}

func TestGetMissingPath(t *testing.T) {
	tr := New()
	tr.RecordRead("s1", "y.go", 1, []byte("x"))
	if _, ok := tr.Get("s1", "z.go"); ok {
		t.Errorf("missing path should return false")
	}
}

func TestAll(t *testing.T) {
	tr := New()
	tr.RecordRead("s1", "a.go", 1, []byte("A"))
	tr.RecordRead("s1", "b.go", 2, []byte("BB"))
	tr.RecordMutation("s1", "a.go", 3, "Edit")
	all := tr.All("s1")
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	gotPaths := map[string]bool{}
	for _, s := range all {
		gotPaths[s.Path] = true
	}
	if !gotPaths["a.go"] || !gotPaths["b.go"] {
		t.Errorf("missing entries: %v", gotPaths)
	}
}

func TestAllMissingSession(t *testing.T) {
	tr := New()
	if got := tr.All("nope"); got != nil {
		t.Errorf("missing session All should be nil, got %v", got)
	}
}

func TestForget(t *testing.T) {
	tr := New()
	tr.RecordRead("s1", "a.go", 1, []byte("A"))
	tr.RecordRead("s2", "a.go", 1, []byte("A"))
	if tr.SessionCount() != 2 {
		t.Fatalf("setup expected 2 sessions, got %d", tr.SessionCount())
	}
	tr.Forget("s1")
	if tr.SessionCount() != 1 {
		t.Errorf("after forget expected 1 session, got %d", tr.SessionCount())
	}
	if _, ok := tr.Get("s1", "a.go"); ok {
		t.Errorf("forgotten session still findable")
	}
}

func TestMutationOnNewPathCreatesEntry(t *testing.T) {
	// Mutation arriving before any read should still register state.
	tr := New()
	tr.RecordMutation("s1", "new.go", 1, "Write")
	st, ok := tr.Get("s1", "new.go")
	if !ok {
		t.Fatal("expected mutation to create state")
	}
	if st.Mutation.Turn != 1 || st.Read.Turn != 0 {
		t.Errorf("state=%+v", st)
	}
}

func TestConcurrentAccessRaceDetectorSmoke(t *testing.T) {
	tr := New()
	const writers = 16
	const reads = 200
	var wg sync.WaitGroup
	wg.Add(writers * 2)
	for i := range writers {
		go func() {
			defer wg.Done()
			for j := range reads {
				tr.RecordRead("s", "x.go", j+i*reads, []byte("body"))
			}
		}()
		go func() {
			defer wg.Done()
			for j := range reads {
				tr.RecordMutation("s", "x.go", j+i*reads, "Edit")
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range reads * 2 {
			_, _ = tr.Get("s", "x.go")
			_ = tr.All("s")
			_ = tr.SessionCount()
		}
	}()
	wg.Wait()
	<-done
}

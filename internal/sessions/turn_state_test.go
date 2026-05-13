package sessions

import (
	"sync"
	"testing"
	"time"
)

func TestTurnStateLifecycleAndObservations(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	store := NewTurnStateStore(TurnStoreOptions{
		MaxSessions: 4,
		MaxTurns:    4,
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
	})

	start := store.StartSession("sess-1", "codex", "/repo")
	if start.SessionID != "sess-1" || start.TurnID != "turn-1" || start.ClientFamily != "codex" {
		t.Fatalf("bad start snapshot: %+v", start)
	}
	turn := store.StartTurn("sess-1", "turn-A", "codex", "/repo", "req-1")
	if turn.TurnID != "turn-A" || turn.RequestID != "req-1" {
		t.Fatalf("bad turn snapshot: %+v", turn)
	}
	tool := store.ObserveTool("sess-1", "turn-A", ToolObservation{Name: "Bash", Command: "git status", Decision: "allow"})
	if len(tool.Tools) != 1 || tool.Tools[0].Command != "git status" {
		t.Fatalf("tool snapshot: %+v", tool)
	}
	read := store.ObserveFile("sess-1", "turn-A", FileObservation{Path: "/repo/main.go", Operation: "read"})
	if len(read.FilesRead) != 1 || read.FilesRead[0].Path != "/repo/main.go" {
		t.Fatalf("read snapshot: %+v", read)
	}
	edit := store.ObserveFile("sess-1", "turn-A", FileObservation{Path: "/repo/main.go", Operation: "edit"})
	if len(edit.FilesEdited) != 1 {
		t.Fatalf("edit snapshot: %+v", edit)
	}
	if !store.RecentlyEdited("sess-1", "/repo/main.go", 0) {
		t.Fatal("recent edit not detected")
	}
	first, repeated := store.ObserveGitPathList("sess-1", "turn-A", "/repo", "git status", []string{"b.go", "a.go", "a.go"})
	if repeated || first.Count != 2 || first.Fingerprint == "" {
		t.Fatalf("first git observation=%+v repeated=%v", first, repeated)
	}
	second, repeated := store.ObserveGitPathList("sess-1", "turn-A", "/repo", "git diff --name-only", []string{"a.go", "b.go"})
	if !repeated || second.Source != "git status" {
		t.Fatalf("second git observation=%+v repeated=%v", second, repeated)
	}
	prefix := store.ObservePromptPrefix("sess-1", "turn-A", "hash", 123)
	if len(prefix.PromptPrefixes) != 1 {
		t.Fatalf("prefix snapshot: %+v", prefix)
	}
	resp := store.SetLastResponseID("sess-1", "turn-A", "resp-1")
	if resp.LastResponseID != "resp-1" {
		t.Fatalf("response snapshot: %+v", resp)
	}
	quality := store.MarkQualityEvent("sess-1", "turn-A")
	if quality.QualityEventCount != 1 {
		t.Fatalf("quality snapshot: %+v", quality)
	}
	closed := store.CloseTurn("sess-1", "turn-A")
	if !closed.Closed {
		t.Fatalf("closed snapshot: %+v", closed)
	}
	snap, ok := store.Snapshot("sess-1", "turn-A")
	if !ok || !snap.Closed || len(snap.Tools) != 1 {
		t.Fatalf("snapshot ok=%v snap=%+v", ok, snap)
	}
}

func TestTurnStateCapsAnonymousAndPreviousTurnEdit(t *testing.T) {
	now := time.Unix(2000, 0).UTC()
	store := NewTurnStateStore(TurnStoreOptions{
		MaxSessions: 2,
		MaxTurns:    2,
		MaxAge:      3 * time.Second,
		Now: func() time.Time {
			return now
		},
	})

	store.StartTurn("", "", "codex", "", "")
	if sessions, turns := store.Stats(); sessions != 1 || turns != 1 {
		t.Fatalf("anonymous stats sessions=%d turns=%d", sessions, turns)
	}

	now = now.Add(time.Second)
	store.StartTurn("sess", "t1", "codex", "/repo", "")
	store.ObserveFile("sess", "t1", FileObservation{Path: "/repo/a.go", Operation: "edit"})
	now = now.Add(time.Second)
	store.StartTurn("sess", "t2", "codex", "/repo", "")
	if _, ok := store.Snapshot("sess", ""); !ok {
		t.Fatal("empty turn id should resolve current turn")
	}
	if !store.RecentlyEdited("sess", "/repo/a.go", 1) {
		t.Fatal("previous-turn edit not detected")
	}
	if !store.RecentlyEdited("sess", "/repo/a.go", 99) {
		t.Fatal("oversized previous-turn window should scan from first retained turn")
	}
	if store.RecentlyEdited("sess", "/repo/a.go", -1) {
		t.Fatal("negative previous-turn window should clamp to current turn only")
	}
	if store.RecentlyEdited("missing", "/repo/a.go", 1) {
		t.Fatal("missing session should not report edits")
	}
	if store.RecentlyEdited("sess", "", 1) {
		t.Fatal("empty path should not report edits")
	}
	if store.RecentlyEdited("sess", "/repo/a.go", 0) {
		t.Fatal("current-turn-only edit should not detect previous turn")
	}
	now = now.Add(time.Second)
	store.StartTurn("sess", "t3", "codex", "/repo", "")
	if _, ok := store.Snapshot("sess", "t1"); ok {
		t.Fatal("oldest turn should be evicted")
	}

	now = now.Add(time.Second)
	store.StartTurn("s2", "t1", "", "", "")
	now = now.Add(time.Second)
	store.StartTurn("s3", "t1", "", "", "")
	if _, ok := store.Snapshot("anonymous", "turn-1"); ok {
		t.Fatal("oldest session should be evicted")
	}

	now = now.Add(4 * time.Second)
	store.StartTurn("fresh", "t1", "", "", "")
	if _, ok := store.Snapshot("sess", "t3"); ok {
		t.Fatal("expired session should be evicted")
	}
	if _, ok := store.Snapshot("missing", "t1"); ok {
		t.Fatal("missing session snapshot should be absent")
	}
	if _, ok := store.Snapshot("fresh", "missing"); ok {
		t.Fatal("missing turn snapshot should be absent")
	}
}

func TestTurnStateConcurrentObservationsAndFingerprints(t *testing.T) {
	store := NewTurnStateStore(TurnStoreOptions{})
	store.StartTurn("sess", "turn", "codex", "/repo", "")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.ObserveTool("sess", "turn", ToolObservation{Name: "Bash"})
			store.ObserveFile("sess", "turn", FileObservation{Path: "/repo/file.go", Operation: "read"})
		}()
	}
	wg.Wait()

	snap, ok := store.Snapshot("sess", "turn")
	if !ok {
		t.Fatal("missing snapshot")
	}
	if len(snap.Tools) != 20 {
		t.Fatalf("tools=%d", len(snap.Tools))
	}
	if len(snap.FilesRead) != 1 {
		t.Fatalf("deduped file reads=%d", len(snap.FilesRead))
	}
	fp1 := FingerprintPaths([]string{"./b.go", "a.go", "a.go"})
	fp2 := FingerprintPaths([]string{"a.go", "b.go"})
	if fp1 == "" || fp1 != fp2 {
		t.Fatalf("fingerprints differ fp1=%q fp2=%q", fp1, fp2)
	}
	if FingerprintPaths(nil) != "" {
		t.Fatal("empty fingerprint should be empty")
	}
}

func TestTurnStateEmptyInputs(t *testing.T) {
	store := NewTurnStateStore(TurnStoreOptions{})
	if _, repeated := store.ObserveGitPathList("s", "t", "/repo", "git status", nil); repeated {
		t.Fatal("empty git path list cannot repeat")
	}
	snap := store.ObserveFile("s", "t", FileObservation{Path: "", Operation: "read"})
	if len(snap.FilesRead) != 1 || snap.FilesRead[0].Path != "" {
		t.Fatalf("empty path read should be recorded as empty observation: %+v", snap)
	}

	store.mu.Lock()
	now := store.now().UTC()
	session := store.ensureSessionLocked("direct", now)
	turn := store.startTurnLocked(session, TurnKey(""), "codex", "/repo", "req", now)
	implicit := store.ensureTurnLocked("implicit", "", "codex", "/repo", "req", now)
	store.mu.Unlock()
	if turn.TurnID == "" {
		t.Fatalf("blank internal turn id should be generated: %+v", turn)
	}
	if implicit.TurnID == "" {
		t.Fatalf("implicit turn should be generated: %+v", implicit)
	}
}

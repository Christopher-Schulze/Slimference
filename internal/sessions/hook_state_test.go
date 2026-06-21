package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHookStateLifecycleAndRecentlyEdited(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := StartHookSession(dir, "sess/1"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveHookFile(dir, "sess/1", "src/main.go", "edit"); err != nil {
		t.Fatal(err)
	}
	hit, err := RecentlyEditedHookFile(dir, "sess/1", "src/main.go", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("expected current-turn edit hit")
	}
	if err := StartHookTurn(dir, "sess/1"); err != nil {
		t.Fatal(err)
	}
	currentTurn, err := CurrentHookTurnID(dir, "sess/1")
	if err != nil {
		t.Fatal(err)
	}
	if currentTurn != "turn-2" {
		t.Fatalf("current turn = %q", currentTurn)
	}
	hit, err = RecentlyEditedHookFile(dir, "sess/1", "src/main.go", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("expected previous-turn edit hit")
	}
	hit, err = RecentlyEditedHookFile(dir, "sess/1", "src/main.go", 0)
	if err != nil {
		t.Fatal(err)
	}
	if hit {
		t.Fatal("previous turn must not match when previousTurns=0")
	}
	if err := ObserveHookFile(dir, "sess/1", "src/main.go", "read"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveHookTool(dir, "sess/1", "Bash", "cat src/main.go"); err != nil {
		t.Fatal(err)
	}
	if err := CloseHookTurn(dir, "sess/1"); err != nil {
		t.Fatal(err)
	}
	state, err := LoadHookState(dir, "sess/1")
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "sess_1" || len(state.Turns) != 2 || !state.Turns[1].Closed {
		t.Fatalf("unexpected state: %+v", state)
	}
	if len(state.Turns[1].FilesRead) != 1 || len(state.Turns[1].Tools) != 1 {
		t.Fatalf("observations missing: %+v", state.Turns[1])
	}
}

func TestHookStateGitPathListRepeat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := StartHookSession(dir, "sess-git"); err != nil {
		t.Fatal(err)
	}
	first, repeated, err := ObserveHookGitPathList(dir, "sess-git", "/repo", "git status --short", []string{"b.go", "a.go", "a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if repeated || first.Count != 2 || first.Fingerprint == "" {
		t.Fatalf("first=%+v repeated=%v", first, repeated)
	}
	second, repeated, err := ObserveHookGitPathList(dir, "sess-git", "/repo", "git diff --name-only", []string{"./a.go", "b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !repeated || second.Source != "git status --short" || second.Count != 2 {
		t.Fatalf("second=%+v repeated=%v", second, repeated)
	}
	_, repeated, err = ObserveHookGitPathList(dir, "sess-git", "/other", "git diff --name-only", []string{"a.go", "b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if repeated {
		t.Fatal("different cwd must not repeat")
	}
	empty, repeated, err := ObserveHookGitPathList(dir, "sess-git", "/repo", "git diff --name-only", nil)
	if err != nil || repeated || empty.Fingerprint != "" {
		t.Fatalf("empty=%+v repeated=%v err=%v", empty, repeated, err)
	}
	dirAsFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(dirAsFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ObserveHookGitPathList(dirAsFile, "sess-git", "/repo", "git status", []string{"a.go"}); err == nil {
		t.Fatal("expected git path list observe error")
	}

	for i := range hookStateMaxFilesPerSet + 2 {
		path := "file-" + strconvItoa(i) + ".go"
		if _, _, err := ObserveHookGitPathList(dir, "sess-cap", "/repo", "git status", []string{path}); err != nil {
			t.Fatal(err)
		}
	}
	capped, err := LoadHookState(dir, "sess-cap")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(capped.Turns[len(capped.Turns)-1].GitPathLists); got != hookStateMaxFilesPerSet {
		t.Fatalf("git path list cap=%d", got)
	}
}

func TestHookStateDefaultsAndHelpers(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if got := DefaultHookStateDir(home); got != filepath.Join(home, ".slimference", "turn-state") {
		t.Fatalf("DefaultHookStateDir=%q", got)
	}
	dir := t.TempDir()
	state, err := LoadHookState(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "anonymous" || state.CurrentTurn != "turn-1" {
		t.Fatalf("default state=%+v", state)
	}
	if safeHookSessionID("A-a_1/ b") != "A-a_1__b" {
		t.Fatalf("safeHookSessionID mismatch")
	}
	if normaliseHookPath(" ") != "" {
		t.Fatalf("blank normaliseHookPath should stay blank")
	}
	if normaliseHookPath(" ./x/../main.go ") != "main.go" {
		t.Fatalf("normaliseHookPath mismatch")
	}
	values := appendUniqueCapped(nil, "a", 2)
	values = appendUniqueCapped(values, "a", 2)
	values = appendUniqueCapped(values, "b", 2)
	values = appendUniqueCapped(values, "c", 2)
	values = appendUniqueCapped(values, " ", 2)
	if len(values) != 2 || values[0] != "b" || values[1] != "c" {
		t.Fatalf("appendUniqueCapped=%v", values)
	}
	if strconvItoa(-42) != "-42" {
		t.Fatalf("strconvItoa failed")
	}
	if strconvItoa(0) != "0" {
		t.Fatalf("strconvItoa zero failed")
	}
}

func TestHookStateCorruptAndStaleLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHookState(dir, "bad"); err == nil {
		t.Fatal("expected corrupt state error")
	}
	lock := filepath.Join(dir, "sess.json.lock")
	if err := os.WriteFile(lock, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-hookStateStaleLockAge - time.Second)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if err := StartHookTurn(dir, "sess"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("lock should be removed, err=%v", err)
	}
}

func TestHookStateEdgeBranches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := ObserveHookTool(dir, "s", "", "cat main.go"); err != nil {
		t.Fatal(err)
	}
	if err := ObserveHookTool(dir, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := ObserveHookFile(dir, "s", "", "edit"); err != nil {
		t.Fatal(err)
	}
	if hit, err := RecentlyEditedHookFile(dir, "s", "", 1); err != nil || hit {
		t.Fatalf("blank recently edited: hit=%v err=%v", hit, err)
	}
	if err := StartHookTurn(dir, "s"); err != nil {
		t.Fatal(err)
	}
	if err := StartHookTurn(dir, "s"); err != nil {
		t.Fatal(err)
	}
	if hit, err := RecentlyEditedHookFile(dir, "s", "missing.go", 99); err != nil || hit {
		t.Fatalf("missing recently edited: hit=%v err=%v", hit, err)
	}

	now := time.Now().UTC()
	state := HookState{SessionID: "", Sequence: 0, CurrentTurn: "", Turns: nil}
	normaliseHookState(&state, "real/session", now)
	if state.SessionID != "real_session" || state.Sequence != 1 || state.CurrentTurn != "turn-1" || len(state.Turns) != 1 {
		t.Fatalf("normalised empty state=%+v", state)
	}
	state = HookState{SessionID: "s", Sequence: 1, CurrentTurn: "", Turns: []HookTurnState{newHookTurn("existing", now)}}
	normaliseHookState(&state, "s", now)
	if state.CurrentTurn != "existing" {
		t.Fatalf("expected current turn from existing turn, got %+v", state)
	}
	state.CurrentTurn = "missing"
	turn := currentHookTurn(&state, now)
	if turn.ID == "missing" {
		t.Fatalf("missing current turn should create a real turn: %+v", state)
	}
	state.Turns = nil
	for i := range hookStateMaxTurns + 2 {
		state.Turns = append(state.Turns, newHookTurn(strconvItoa(i+1), now.Add(time.Duration(i)*time.Second)))
	}
	trimHookTurns(&state)
	if len(state.Turns) != hookStateMaxTurns {
		t.Fatalf("turn cap failed: %d", len(state.Turns))
	}
}

func TestHookStateErrorBranches(t *testing.T) {
	dirAsFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(dirAsFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHookState(dirAsFile, "s"); err == nil {
		t.Fatal("expected LoadHookState read error")
	}
	if err := mutateHookState(dirAsFile, "s", func(*HookState, time.Time) error { return nil }); err == nil {
		t.Fatal("expected mutateHookState load error")
	}
	if err := inspectHookState(dirAsFile, "s", func(*HookState) error { return nil }); err == nil {
		t.Fatal("expected inspectHookState load error")
	}
	if err := saveHookState(dirAsFile, &HookState{SessionID: "s"}); err == nil {
		t.Fatal("expected saveHookState mkdir error")
	}
	if err := withHookStateLock(dirAsFile, "s", func() error { return nil }); err == nil {
		t.Fatal("expected lock mkdir error")
	}
	corruptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(corruptDir, "s.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mutateHookState(corruptDir, "s", func(*HookState, time.Time) error { return nil }); err == nil {
		t.Fatal("expected mutateHookState parse error")
	}
	if err := inspectHookState(corruptDir, "s", func(*HookState) error { return nil }); err == nil {
		t.Fatal("expected inspectHookState parse error")
	}

	errBoom := errors.New("boom")
	if err := mutateHookState(t.TempDir(), "s", func(*HookState, time.Time) error { return errBoom }); !errors.Is(err, errBoom) {
		t.Fatalf("expected fn error, got %v", err)
	}

	writeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(writeDir, "s.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveHookState(writeDir, &HookState{SessionID: "s"}); err == nil {
		t.Fatal("expected temp write error")
	}

	origOpen := hookStateOpenFile
	hookStateOpenFile = func(string, int, os.FileMode) (*os.File, error) {
		return nil, errors.New("open")
	}
	defer func() { hookStateOpenFile = origOpen }()
	if err := withHookStateLock(t.TempDir(), "s", func() error { return nil }); err == nil {
		t.Fatal("expected open error")
	}
}

func TestHookStateLockTimeout(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "held.json.lock")
	if err := os.WriteFile(lock, []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := withHookStateLock(dir, "held", func() error { return nil })
	if err == nil {
		t.Fatal("expected held lock error")
	}
	if time.Since(start) < hookStateLockTimeout {
		t.Fatal("lock timeout returned too early")
	}
}

package readcache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadCacheStoreAndHelperCoverage(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if got := DefaultDir(home); got != filepath.Join(home, ".slimference", "read-cache") {
		t.Fatalf("DefaultDir=%q", got)
	}

	dir := tempReadCacheDir(t)
	state, err := LoadSession(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "anonymous" {
		t.Fatalf("LoadSession empty id=%q", state.SessionID)
	}

	state.Files["/tmp/a"] = &FileEntry{Path: "/tmp/a"}
	if err := SaveSession(dir, state); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadSession(dir, "anonymous")
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Files) != 1 {
		t.Fatalf("reloaded files=%d", len(reloaded.Files))
	}

	if err := os.WriteFile(sessionPath(dir, "broken"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSession(dir, "broken"); err == nil {
		t.Fatal("expected broken session JSON error")
	}
	badDirFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badDirFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSession(badDirFile, state); err == nil {
		t.Fatal("expected SaveSession mkdir error")
	}
	if err := os.WriteFile(sessionPath(dir, "hydrated"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hydrated, err := LoadSession(dir, "hydrated")
	if err != nil {
		t.Fatal(err)
	}
	if hydrated.SessionID != "hydrated" || hydrated.Files == nil {
		t.Fatalf("hydrated=%+v", hydrated)
	}
	hydrated.CurrentTurnID = "turn/1"
	if err := SaveSession(dir, hydrated); err != nil {
		t.Fatal(err)
	}
	hydrated, err = LoadSession(dir, "hydrated")
	if err != nil {
		t.Fatal(err)
	}
	if hydrated.CurrentTurnID != "turn_1" {
		t.Fatalf("hydrated turn id=%q", hydrated.CurrentTurnID)
	}

	if got := sanitizeSessionID(" \n "); got != "anonymous" {
		t.Fatalf("sanitizeSessionID blank=%q", got)
	}
	if got, ok := numericValue(7); !ok || got != 7 {
		t.Fatalf("numericValue int=%d ok=%v", got, ok)
	}
	if _, ok := numericValue("7"); ok {
		t.Fatal("numericValue string should fail")
	}
	if got := findString([]any{map[string]any{"file_path": "x"}}, "file_path"); got != "x" {
		t.Fatalf("findString=%q", got)
	}
	if got := findInt([]any{map[string]any{"limit": 9.0}}, "limit"); got != 9 {
		t.Fatalf("findInt=%d", got)
	}
	if got := buildDeltaSummary("x", "same\nsame\n", "same\nsame\n"); got != "" {
		t.Fatalf("buildDeltaSummary same=%q", got)
	}
	summary := buildDeltaSummary("x", "func main() {\n\treturn\n}\n", "func main() {\n    return\n}\n")
	if !strings.Contains(summary, "-\treturn") || !strings.Contains(summary, "+    return") {
		t.Fatalf("delta should preserve indentation-only changes: %q", summary)
	}
	if strings.Contains(summary, "\n\n+") || strings.Contains(summary, "\n\n-") || strings.Contains(summary, "\n\n ") {
		t.Fatalf("delta should not double-space hunk lines: %q", summary)
	}
}

func TestReadCacheEvaluateAndStatsBranches(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	missing := filepath.Join(dir, "missing.txt")
	decision, err := Evaluate(dir, Request{SessionID: "s1", FilePath: missing})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("missing file decision=%+v", decision)
	}

	subdir := filepath.Join(dir, "folder")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	decision, err = Evaluate(dir, Request{SessionID: "s1", FilePath: subdir})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("dir decision=%+v", decision)
	}

	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte(strings.Repeat("x", 10)), 0o644); err != nil {
		t.Fatal(err)
	}
	req := Request{SessionID: "s2", TurnID: "turn/2", FilePath: file}
	if _, err := Evaluate(dir, req); err != nil {
		t.Fatal(err)
	}
	state, err := LoadSession(dir, "s2")
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentTurnID != "turn_2" || state.Files[file].LastTurnID != "turn_2" {
		t.Fatalf("turn state not stored: %+v", state)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("x", 10)), 0o644); err != nil {
		t.Fatal(err)
	}
	decision, err = Evaluate(dir, req)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock && decision.Type != DecisionAllow {
		t.Fatalf("unexpected decision=%+v", decision)
	}

	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte(" \n "), 0o644); err != nil {
		t.Fatal(err)
	}
	stats, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Evaluations != 0 {
		t.Fatalf("blank stats=%+v", stats)
	}
	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStats(dir); err == nil {
		t.Fatal("expected broken stats JSON error")
	}
	badDirFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badDirFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveStats(badDirFile, Stats{}); err == nil {
		t.Fatal("expected SaveStats mkdir error")
	}
}

func TestReadCachePayloadCoverage(t *testing.T) {
	t.Parallel()

	req, err := ExtractRequest([]byte(`{"session_id":"s","turn_id":"turn/1","tool_input":{"file_path":"x","offset":1.9,"limit":7}}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.TurnID != "turn/1" || req.Offset != 1 || req.Limit != 7 {
		t.Fatalf("request=%+v", req)
	}

	req, err = ExtractRequest([]byte(`{"session_id":"s","tool_input":{"nested":{"file_path":"x"},"offset":5}}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.FilePath != "x" || req.Offset != 5 {
		t.Fatalf("nested request=%+v", req)
	}
}

func TestReadCacheInjectedErrorBranches(t *testing.T) {
	dir := tempReadCacheDir(t)
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origSave := readCacheSaveSession
	origReadDir := readCacheReadDir
	origRemoveAll := readCacheRemoveAll
	origReadFile := readCacheReadFile
	defer func() {
		readCacheSaveSession = origSave
		readCacheReadDir = origReadDir
		readCacheRemoveAll = origRemoveAll
		readCacheReadFile = origReadFile
	}()

	if _, err := Evaluate(dir, Request{SessionID: "s1", FilePath: file}); err != nil {
		t.Fatal(err)
	}

	readCacheSaveSession = func(string, *SessionState) error { return errors.New("save") }
	if _, err := Evaluate(dir, Request{SessionID: "s1", FilePath: file}); err == nil {
		t.Fatal("expected Evaluate save session error")
	}

	readCacheSaveSession = origSave
	readCacheReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	if _, err := Snapshot(dir); err == nil {
		t.Fatal("expected Snapshot read dir error")
	}

	readCacheReadDir = origReadDir
	readCacheRemoveAll = func(string) error { return errors.New("removeall") }
	if err := Clear(dir); err == nil {
		t.Fatal("expected Clear remove error")
	}

	readCacheRemoveAll = origRemoveAll
	readCacheReadFile = func(string) ([]byte, error) { return nil, errors.New("read") }
	if _, err := LoadSession(dir, "s1"); err == nil {
		t.Fatal("expected LoadSession read error")
	}
	if _, err := LoadStats(dir); err == nil {
		t.Fatal("expected LoadStats read error")
	}
}

func TestReadCacheMarshalWriteAndSnapshotErrors(t *testing.T) {
	dir := tempReadCacheDir(t)

	origMarshal := readCacheMarshalIndent
	origWrite := readCacheWriteFile
	defer func() {
		readCacheMarshalIndent = origMarshal
		readCacheWriteFile = origWrite
	}()

	readCacheMarshalIndent = func(any, string, string) ([]byte, error) { return nil, errors.New("marshal") }
	if err := SaveSession(dir, &SessionState{SessionID: "s1", Files: map[string]*FileEntry{}}); err == nil {
		t.Fatal("expected SaveSession marshal error")
	}
	if err := SaveStats(dir, Stats{}); err == nil {
		t.Fatal("expected SaveStats marshal error")
	}

	readCacheMarshalIndent = origMarshal
	readCacheWriteFile = func(string, []byte, os.FileMode) error { return errors.New("write") }
	if err := SaveSession(dir, &SessionState{SessionID: "s1", Files: map[string]*FileEntry{}}); err == nil {
		t.Fatal("expected SaveSession write error")
	}
	if err := SaveStats(dir, Stats{}); err == nil {
		t.Fatal("expected SaveStats write error")
	}

	readCacheWriteFile = origWrite
	if err := os.WriteFile(sessionPath(dir, "broken"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(dir); err == nil {
		t.Fatal("expected Snapshot broken session error")
	}
}

func TestReadCacheDeltaAndDecisionCoverage(t *testing.T) {
	dir := tempReadCacheDir(t)
	file := filepath.Join(dir, "delta.txt")
	original := strings.Repeat("same line\n", 40) + "old tail\n"
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	req := Request{SessionID: "sess", FilePath: file}
	if _, err := Evaluate(dir, req); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	updated := strings.Repeat("same line\n", 40) + "new tail\n"
	if err := os.WriteFile(file, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	decision, err := Evaluate(dir, req)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || decision.BlockKind != BlockKindDelta {
		t.Fatalf("decision=%+v", decision)
	}
	if !strings.Contains(decision.Reason, "kind=file-read") {
		t.Fatalf("delta reason=%q", decision.Reason)
	}

	if err := RecordDecision(dir, Decision{Type: DecisionAllow}); err != nil {
		t.Fatal(err)
	}
	stats, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Allows == 0 {
		t.Fatalf("stats=%+v", stats)
	}

	if got := sanitizeSessionID("a/b:c"); got != "a_b_c" {
		t.Fatalf("sanitizeSessionID=%q", got)
	}
	if summary := buildDeltaSummary(file, "a\nb\n", "a\nc\n"); !strings.Contains(summary, "+c") || !strings.Contains(summary, "-b") {
		t.Fatalf("summary=%q", summary)
	}
	if summary := buildDeltaSummary(file, "a\nb\nc\n", "a\nc\n"); !strings.Contains(summary, "-b") {
		t.Fatalf("removed summary=%q", summary)
	}
}

func TestReadCacheAdditionalErrorBranches(t *testing.T) {
	dir := tempReadCacheDir(t)

	origReadFile := readCacheReadFile
	origSave := readCacheSaveSession
	origAbs := readCacheAbsPath
	defer func() {
		readCacheReadFile = origReadFile
		readCacheSaveSession = origSave
		readCacheAbsPath = origAbs
	}()

	readCacheReadFile = func(string) ([]byte, error) { return nil, errors.New("stats read") }
	if _, err := Snapshot(dir); err == nil {
		t.Fatal("expected Snapshot load stats error")
	}
	if err := RecordDecision(dir, Decision{Type: DecisionAllow}); err == nil {
		t.Fatal("expected RecordDecision load stats error")
	}

	readCacheReadFile = origReadFile
	file := filepath.Join(dir, "changed.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Evaluate(dir, Request{SessionID: "s1", FilePath: file}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(file, []byte("package main\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	readCacheSaveSession = func(string, *SessionState) error { return errors.New("save changed") }
	if _, err := Evaluate(dir, Request{SessionID: "s1", FilePath: file}); err == nil {
		t.Fatal("expected changed Evaluate save error")
	}

	if got := sanitizeSessionID("ABC"); got != "ABC" {
		t.Fatalf("uppercase sanitize=%q", got)
	}
	if _, err := Snapshot(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("missing snapshot err=%v", err)
	}

	readCacheReadFile = func(string) ([]byte, error) { return nil, errors.New("load session") }
	if _, err := Evaluate(dir, Request{SessionID: "broken", FilePath: file}); err == nil {
		t.Fatal("expected Evaluate load session error")
	}
	readCacheReadFile = origReadFile

	duplicateSummary := buildDeltaSummary("dup.txt", "a\na\nb\n", "a\nb\na\n")
	if !strings.Contains(duplicateSummary, "-a") || !strings.Contains(duplicateSummary, "+a") {
		t.Fatalf("duplicate-line movement should stay visible: %q", duplicateSummary)
	}

	readCacheAbsPath = func(string) (string, error) { return "", errors.New("abs") }
	if decision, err := Evaluate(dir, Request{SessionID: "abs", FilePath: file}); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("abs decision=%+v err=%v", decision, err)
	}
	readCacheAbsPath = origAbs

	partialDir := tempReadCacheDir(t)
	partialFile := filepath.Join(partialDir, "partial.go")
	if err := os.WriteFile(partialFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	readCacheSaveSession = func(string, *SessionState) error { return errors.New("save partial") }
	if _, err := Evaluate(partialDir, Request{SessionID: "partial", FilePath: partialFile, Limit: 1}); err == nil {
		t.Fatal("expected partial Evaluate save error")
	}
}

func TestSplitMemorySessionKey(t *testing.T) {
	t.Parallel()
	// Normal case: dir\x00sessionID
	dir, sid := splitMemorySessionKey("/repo/project\x00sess-123")
	if dir != "/repo/project" || sid != "sess-123" {
		t.Fatalf("normal split mismatch: dir=%q sid=%q", dir, sid)
	}
	// No separator: returns full key as dir, empty session
	dir, sid = splitMemorySessionKey("no-separator")
	if dir != "no-separator" || sid != "" {
		t.Fatalf("no-sep split mismatch: dir=%q sid=%q", dir, sid)
	}
	// Empty key
	dir, sid = splitMemorySessionKey("")
	if dir != "" || sid != "" {
		t.Fatalf("empty split mismatch: dir=%q sid=%q", dir, sid)
	}
	// Separator at start: empty dir
	dir, sid = splitMemorySessionKey("\x00sess")
	if dir != "" || sid != "sess" {
		t.Fatalf("leading-sep split mismatch: dir=%q sid=%q", dir, sid)
	}
	// Separator at end: empty session
	dir, sid = splitMemorySessionKey("/dir\x00")
	if dir != "/dir" || sid != "" {
		t.Fatalf("trailing-sep split mismatch: dir=%q sid=%q", dir, sid)
	}
}

func TestNormalizeSessionState_NilSafe(t *testing.T) {
	t.Parallel()
	// Must not panic on nil
	normalizeSessionState(nil)
}

func TestNormalizeSessionState_InitializesMaps(t *testing.T) {
	t.Parallel()
	state := &SessionState{SessionID: "  sess-1  ", CurrentTurnID: "turn-0"}
	normalizeSessionState(state)
	if state.Files == nil {
		t.Fatal("Files map must be initialized")
	}
	if state.Outputs == nil {
		t.Fatal("Outputs map must be initialized")
	}
	if state.SessionID != "sess-1" {
		t.Fatalf("SessionID must be sanitized (trimmed): %q", state.SessionID)
	}
}

func TestNormalizeSessionState_PreservesExistingMaps(t *testing.T) {
	t.Parallel()
	files := map[string]*FileEntry{"a.go": {}}
	outputs := map[string]*OutputEntry{"out1": {}}
	state := &SessionState{
		SessionID:     "sess-2",
		CurrentTurnID: "turn-1",
		Files:         files,
		Outputs:       outputs,
	}
	normalizeSessionState(state)
	if len(state.Files) != 1 || state.Files["a.go"] == nil {
		t.Fatal("existing Files map must be preserved")
	}
	if len(state.Outputs) != 1 || state.Outputs["out1"] == nil {
		t.Fatal("existing Outputs map must be preserved")
	}
}

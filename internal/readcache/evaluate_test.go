package readcache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/contentarchive"
)

func TestEvaluate_FirstReadAllowsAndStores(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	decision, err := Evaluate(dir, Request{SessionID: "s1", FilePath: file})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("expected allow, got %#v", decision)
	}
}

func TestEvaluate_UnchangedReadBlocks(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := Request{SessionID: "s1", FilePath: file}
	if _, err := Evaluate(dir, req); err != nil {
		t.Fatal(err)
	}
	decision, err := Evaluate(dir, req)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || !strings.Contains(decision.Reason, "kind=file-read") || !strings.Contains(decision.Reason, "status=unchanged") {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestEvaluate_ChangedFullReadBlocksWithDelta(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	file := filepath.Join(dir, "main.go")
	before := "package main\n" + strings.Repeat("func a() {}\n", 40)
	if err := os.WriteFile(file, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	req := Request{SessionID: "s1", FilePath: file}
	if _, err := Evaluate(dir, req); err != nil {
		t.Fatal(err)
	}

	after := "package main\n" + strings.Repeat("func a() {}\n", 40) + "func b() {}\n"
	if err := os.WriteFile(file, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}

	decision, err := Evaluate(dir, req)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || !strings.Contains(decision.Reason, "kind=file-read") {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestEvaluate_ChangedRangeAllows(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := Request{SessionID: "s1", FilePath: file, Offset: 5, Limit: 10}
	if _, err := Evaluate(dir, req); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(file, []byte("package main\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	decision, err := Evaluate(dir, req)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("expected allow, got %#v", decision)
	}
}

func TestEvaluateObserved_UnchangedAndChangedArchiveBacked(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	req := Request{SessionID: "s1", FilePath: "main.go"}
	before := "package main\n" + strings.Repeat("func a() {}\n", 40)
	decision, err := EvaluateObserved(dir, req, before, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("first observed read should allow: %+v", decision)
	}

	decision, err = EvaluateObserved(dir, req, before, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || decision.BlockKind != BlockKindUnchanged || !strings.Contains(decision.Reason, "kind=file-read") || !strings.Contains(decision.Reason, "archive=local-archive://") {
		t.Fatalf("unchanged observed read should block with archive reference: %+v", decision)
	}

	state, err := LoadSession(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := contentarchive.Get(archiveDir, state.Files["main.go"].ArchiveURI)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != before {
		t.Fatal("archive did not expand original observed content")
	}

	after := before + "func b() {}\n"
	decision, err = EvaluateObserved(dir, req, after, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || decision.BlockKind != BlockKindDelta || !strings.Contains(decision.Reason, "+func b() {}") {
		t.Fatalf("changed observed read should block with delta: %+v", decision)
	}
}

func TestEvaluateObserved_LargeContentUsesArchiveWithoutInlineCache(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	req := Request{SessionID: "s1", FilePath: "large.md"}
	before := "title\n" + strings.Repeat("same line\n", 40000)
	decision, err := EvaluateObserved(dir, req, before, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("first large observed read should allow: %+v", decision)
	}
	state, err := LoadSession(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Files["large.md"]
	if entry == nil || entry.ArchiveURI == "" {
		t.Fatalf("large observed read should archive content: %+v", entry)
	}
	if entry.CachedContent != "" {
		t.Fatalf("large observed read should not inline %d bytes into session JSON", len(entry.CachedContent))
	}

	after := before + "tail addition\n"
	decision, err = EvaluateObserved(dir, req, after, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || decision.BlockKind != BlockKindDelta ||
		!strings.Contains(decision.Reason, "+tail addition") ||
		!strings.Contains(decision.Reason, "kind=full-content") || !strings.Contains(decision.Reason, "uri=local-archive://") {
		t.Fatalf("large changed reread should delta from archive-backed content: %+v", decision)
	}
	state, err = LoadSession(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Files["large.md"].CachedContent != "" {
		t.Fatal("large changed reread should remain archive-backed, not inline-cached")
	}
}

func TestEvaluateObserved_RangedReadsAreDistinctAndDeltaCapable(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	rangeA := Request{SessionID: "s1", FilePath: "main.go", Offset: 1, Limit: 20}
	rangeB := Request{SessionID: "s1", FilePath: "main.go", Offset: 21, Limit: 20}
	before := strings.Repeat("range A line\n", 40)
	otherRange := strings.Repeat("range B line\n", 40)

	if decision, err := EvaluateObserved(dir, rangeA, before, archiveDir, false); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("first ranged read should allow, decision=%+v err=%v", decision, err)
	}
	decision, err := EvaluateObserved(dir, rangeA, before, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || decision.BlockKind != BlockKindUnchanged ||
		!strings.Contains(decision.Reason, "kind=file-read") || !strings.Contains(decision.Reason, "archive=local-archive://") {
		t.Fatalf("identical ranged reread should block with archive reference: %+v", decision)
	}

	decision, err = EvaluateObserved(dir, rangeB, otherRange, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("distinct range must not collide with range A: %+v", decision)
	}

	changed := before + "range A added line\n"
	decision, err = EvaluateObserved(dir, rangeA, changed, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || decision.BlockKind != BlockKindDelta ||
		!strings.Contains(decision.Reason, "+range A added line") {
		t.Fatalf("changed ranged reread should block with position-aware delta: %+v", decision)
	}

	state, err := LoadSession(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Files["main.go#range:1:20"] == nil || state.Files["main.go#range:21:20"] == nil {
		t.Fatalf("ranges must be keyed independently: %+v", state.Files)
	}
}

func TestEvaluateObserved_RecentFullPassTurns(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	req := Request{
		SessionID:               "s1",
		TurnID:                  "turn-1",
		FilePath:                "recent.go",
		RecentFullPassTurnLimit: 1,
	}
	body := strings.Repeat("recent full-pass line\n", 80)

	if decision, err := EvaluateObserved(dir, req, body, archiveDir, false); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("first read: decision=%+v err=%v", decision, err)
	}
	decision, err := EvaluateObserved(dir, req, body, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock {
		t.Fatalf("same-turn repeat should still collapse, got %+v", decision)
	}

	req.TurnID = "turn-2"
	decision, err = EvaluateObserved(dir, req, body, archiveDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("recent cross-turn read should full-pass, got %+v", decision)
	}
}

func TestEvaluateObserved_RecentEditAllowsAndUpdates(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	req := Request{SessionID: "s1", FilePath: "main.go"}
	before := strings.Repeat("old line\n", 20)
	if _, err := EvaluateObserved(dir, req, before, archiveDir, false); err != nil {
		t.Fatal(err)
	}
	after := strings.Repeat("new line\n", 20)
	decision, err := EvaluateObserved(dir, req, after, archiveDir, true)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("recent edit must allow full content: %+v", decision)
	}
	state, err := LoadSession(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Files["main.go"].CachedContent != after {
		t.Fatal("recent edit path should still update stored full content")
	}
}

func TestEvaluateObservedOutput_ExactRepeatBlocks(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	req := OutputRequest{
		SessionID:   "s1",
		TurnID:      "turn-1",
		Key:         "command:python generate.py",
		CommandLine: "python generate.py",
	}
	body := strings.Repeat("deterministic generated output line\n", 40)
	decision, err := EvaluateObservedOutput(dir, req, body, archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionAllow {
		t.Fatalf("first observed output should allow: %+v", decision)
	}

	req.TurnID = "turn-2"
	decision, err = EvaluateObservedOutput(dir, req, body, archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || decision.BlockKind != BlockKindUnchanged ||
		!strings.Contains(decision.Reason, "kind=tool-output") ||
		!strings.Contains(decision.Reason, "archive=local-archive://") {
		t.Fatalf("exact repeated output should block with archive reference: %+v", decision)
	}

	state, err := LoadSession(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Outputs[req.Key]
	if entry == nil || entry.ArchiveURI == "" || entry.CommandLine != req.CommandLine {
		t.Fatalf("output entry not stored: %+v", entry)
	}
	_, archived, err := contentarchive.Get(archiveDir, entry.ArchiveURI)
	if err != nil {
		t.Fatal(err)
	}
	if string(archived) != body {
		t.Fatal("archive did not preserve exact output")
	}
}

func TestEvaluateObservedOutput_ChangedShortOrUnarchivedAllows(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	req := OutputRequest{SessionID: "s1", Key: "command:tool", CommandLine: "tool"}
	if decision, err := EvaluateObservedOutput(dir, req, "short output", archiveDir); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("short output should allow, decision=%+v err=%v", decision, err)
	}
	body := strings.Repeat("old output line\n", 40)
	if decision, err := EvaluateObservedOutput(dir, req, body, ""); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("missing archive dir should allow, decision=%+v err=%v", decision, err)
	}
	if decision, err := EvaluateObservedOutput(dir, req, strings.Repeat("new output line\n", 40), archiveDir); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("changed output should allow, decision=%+v err=%v", decision, err)
	}
}

func TestEvaluateObservedOutput_SearchDeltaBlocks(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	req := OutputRequest{
		SessionID:   "s1",
		Key:         "search:rg\t-n\tneedle\t.",
		CommandLine: "rg -n needle .",
	}
	before := strings.Repeat("pkg/a.go:10:needle old context\n", 30)
	if decision, err := EvaluateObservedOutput(dir, req, before, archiveDir); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("first search output should allow, decision=%+v err=%v", decision, err)
	}
	after := before + "pkg/b.go:42:needle new context\n"
	decision, err := EvaluateObservedOutput(dir, req, after, archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != DecisionBlock || decision.BlockKind != BlockKindDelta ||
		!strings.Contains(decision.Reason, "+pkg/b.go:42:needle new context") ||
		!strings.Contains(decision.Reason, "kind=full-output") || !strings.Contains(decision.Reason, "uri=local-archive://") {
		t.Fatalf("changed search output should block with delta: %+v", decision)
	}

	nonSearch := OutputRequest{SessionID: "s2", Key: "command:tool", CommandLine: "tool"}
	if _, err := EvaluateObservedOutput(dir, nonSearch, before, archiveDir); err != nil {
		t.Fatal(err)
	}
	if decision, err := EvaluateObservedOutput(dir, nonSearch, after, archiveDir); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("changed non-search output should remain allow, decision=%+v err=%v", decision, err)
	}
}

func TestEvaluateObserved_FailOpenBranches(t *testing.T) {
	t.Parallel()

	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	if decision, err := EvaluateObserved(dir, Request{SessionID: "s1", TurnID: "turn-1"}, "content", archiveDir, false); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("empty path should allow, decision=%+v err=%v", decision, err)
	}
	if decision, err := EvaluateObserved(dir, Request{SessionID: "s1", FilePath: "main.go", Offset: 1}, "content", archiveDir, false); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("partial read should allow, decision=%+v err=%v", decision, err)
	}

	req := Request{SessionID: "s2", FilePath: "short.go"}
	short := "one two three four five six seven"
	if decision, err := EvaluateObserved(dir, req, short, archiveDir, false); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("first short read should allow, decision=%+v err=%v", decision, err)
	}
	if decision, err := EvaluateObserved(dir, req, short, archiveDir, false); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("short unarchived reread should allow, decision=%+v err=%v", decision, err)
	}

	req = Request{SessionID: "s3", FilePath: "no-archive.go"}
	before := strings.Repeat("old line\n", 20)
	after := strings.Repeat("new line\n", 20)
	if _, err := EvaluateObserved(dir, req, before, "", false); err != nil {
		t.Fatal(err)
	}
	if decision, err := EvaluateObserved(dir, req, after, "", false); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("missing archive dir should fail open, decision=%+v err=%v", decision, err)
	}

	badArchive := filepath.Join(t.TempDir(), "archive-file")
	if err := os.WriteFile(badArchive, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if decision, err := EvaluateObserved(dir, Request{SessionID: "s4", FilePath: "bad-archive.go"}, before, badArchive, false); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("archive write failure should fail open, decision=%+v err=%v", decision, err)
	}

	req = Request{SessionID: "s5", FilePath: "large-change.go"}
	if _, err := EvaluateObserved(dir, req, strings.Repeat("a", 80), archiveDir, false); err != nil {
		t.Fatal(err)
	}
	if decision, err := EvaluateObserved(dir, req, strings.Repeat("b", 80), archiveDir, false); err != nil || decision.Type != DecisionAllow {
		t.Fatalf("non-shorter delta should allow full content, decision=%+v err=%v", decision, err)
	}
}

func TestEvaluateObserved_InjectedErrorBranches(t *testing.T) {
	dir := tempReadCacheDir(t)
	archiveDir := t.TempDir()
	req := Request{SessionID: "s1", FilePath: "main.go"}
	content := strings.Repeat("line\n", 30)

	origRead := readCacheReadFile
	origSave := readCacheSaveSession
	defer func() {
		readCacheReadFile = origRead
		readCacheSaveSession = origSave
	}()

	readCacheReadFile = func(string) ([]byte, error) { return nil, errors.New("load observed") }
	if _, err := EvaluateObserved(dir, req, content, archiveDir, false); err == nil {
		t.Fatal("expected load-session error")
	}
	readCacheReadFile = origRead

	readCacheSaveSession = func(string, *SessionState) error { return errors.New("save recent") }
	if _, err := EvaluateObserved(dir, req, content, archiveDir, true); err == nil {
		t.Fatal("expected recent-edit save error")
	}
	readCacheSaveSession = origSave

	if _, err := EvaluateObserved(dir, req, content, archiveDir, false); err != nil {
		t.Fatal(err)
	}
	readCacheSaveSession = func(string, *SessionState) error { return errors.New("save unchanged") }
	if _, err := EvaluateObserved(dir, req, content, archiveDir, false); err == nil {
		t.Fatal("expected unchanged save error")
	}
	readCacheSaveSession = origSave

	if _, err := EvaluateObserved(dir, req, content, archiveDir, false); err != nil {
		t.Fatal(err)
	}
	readCacheSaveSession = func(string, *SessionState) error { return errors.New("save changed") }
	if _, err := EvaluateObserved(dir, req, content+"changed\n", archiveDir, false); err == nil {
		t.Fatal("expected changed save error")
	}
}

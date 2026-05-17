package compactsignal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteAndReadMarker_Roundtrip(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.WriteMarker(PhasePre, "sess-1", "turn-42", "auto"); err != nil {
		t.Fatalf("write: %v", err)
	}
	m, ok := s.ReadMarker(PhasePre, "sess-1")
	if !ok {
		t.Fatalf("expected marker present")
	}
	if m.SessionID != "sess-1" || m.TurnID != "turn-42" || m.Trigger != "auto" || m.Phase != "pre" {
		t.Fatalf("marker drift: %+v", m)
	}
	if m.TSUnix <= 0 {
		t.Fatalf("expected positive ts, got %d", m.TSUnix)
	}
}

func TestWriteMarker_EmptySessionIsNoOp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.WriteMarker(PhasePre, "", "t", "auto"); err != nil {
		t.Fatalf("write with empty session must not error: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, PhasePre))
	if len(entries) != 0 {
		t.Fatalf("expected no files for empty session, got %d", len(entries))
	}
}

func TestWriteMarker_UnknownPhaseIsNoOp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.WriteMarker("middle", "sess", "t", "auto"); err != nil {
		t.Fatalf("write with unknown phase must not error: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected no files for unknown phase, got %d", len(entries))
	}
}

func TestHasRecentSignal_WithinWindow(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 5, 15, 22, 0, 0, 0, time.UTC)
	s.nowFn = func() time.Time { return now }
	if err := s.WriteMarker(PhasePre, "s9", "t", "auto"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// 30s later still in window.
	s.nowFn = func() time.Time { return now.Add(30 * time.Second) }
	if !s.HasRecentSignal(PhasePre, "s9", time.Minute) {
		t.Fatalf("expected recent signal within 60s")
	}
}

func TestHasRecentSignal_OutsideWindow(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 5, 15, 22, 0, 0, 0, time.UTC)
	s.nowFn = func() time.Time { return now }
	_ = s.WriteMarker(PhasePre, "s9", "t", "auto")
	// 2 minutes later.
	s.nowFn = func() time.Time { return now.Add(2 * time.Minute) }
	if s.HasRecentSignal(PhasePre, "s9", time.Minute) {
		t.Fatalf("expected signal stale at +2m with 60s window")
	}
}

func TestHasRecentSignal_MissingFile(t *testing.T) {
	s := NewStore(t.TempDir())
	if s.HasRecentSignal(PhasePre, "nope", time.Minute) {
		t.Fatalf("missing marker must not signal")
	}
}

func TestHasRecentSignal_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	target := filepath.Join(dir, PhasePre, "sx.json")
	_ = os.MkdirAll(filepath.Dir(target), 0o755)
	if err := os.WriteFile(target, []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s.HasRecentSignal(PhasePre, "sx", time.Minute) {
		t.Fatalf("malformed marker must fail closed")
	}
}

func TestHasRecentSignal_PhaseScoping(t *testing.T) {
	s := NewStore(t.TempDir())
	_ = s.WriteMarker(PhasePost, "s", "t", "manual")
	if s.HasRecentSignal(PhasePre, "s", time.Minute) {
		t.Fatalf("post-marker must not satisfy pre-query")
	}
	if !s.HasRecentSignal(PhasePost, "s", time.Minute) {
		t.Fatalf("post-marker must satisfy post-query")
	}
}

func TestCleanupOld_DeletesStaleFiles(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// Write two markers. We'll mark one as "old" by setting its mtime.
	_ = s.WriteMarker(PhasePre, "fresh", "t", "auto")
	_ = s.WriteMarker(PhasePre, "stale", "t", "auto")
	stalePath := filepath.Join(dir, PhasePre, "stale.json")
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	removed, err := s.CleanupOld(5 * time.Minute)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if _, err := os.Stat(stalePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale marker still present after cleanup")
	}
	if _, ok := s.ReadMarker(PhasePre, "fresh"); !ok {
		t.Fatalf("fresh marker should still be present")
	}
}

func TestCleanupOld_MissingDirNoError(t *testing.T) {
	s := NewStore(t.TempDir())
	// Never wrote anything; directories do not exist.
	removed, err := s.CleanupOld(time.Minute)
	if err != nil {
		t.Fatalf("cleanup on empty store should not error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 removed, got %d", removed)
	}
}

func TestCleanupOld_PropagatesFirstError(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	_ = s.WriteMarker(PhasePre, "x", "t", "auto")
	// Make the file unremovable by replacing remove with a stub.
	s.remove = func(path string) error { return errors.New("permission denied") }
	// Stale the file.
	target := filepath.Join(dir, PhasePre, "x.json")
	old := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(target, old, old)
	_, err := s.CleanupOld(time.Minute)
	if err == nil || err.Error() != "permission denied" {
		t.Fatalf("expected propagation of first error, got %v", err)
	}
}

func TestWriteMarker_AtomicViaTempRename(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	wrote := false
	s.rename = func(src, dst string) error {
		// Assert that rename source ends in .tmp — proves we went via tempfile.
		if filepath.Base(src) != "s.json.tmp" {
			t.Fatalf("expected temp file 's.json.tmp', got %q", filepath.Base(src))
		}
		wrote = true
		return os.Rename(src, dst)
	}
	if err := s.WriteMarker(PhasePre, "s", "t", "auto"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !wrote {
		t.Fatalf("rename hook not invoked")
	}
}

func TestWriteMarker_RenameFailureRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.rename = func(src, dst string) error { return errors.New("rename boom") }
	if err := s.WriteMarker(PhasePre, "s", "t", "auto"); err == nil {
		t.Fatalf("expected error from rename failure")
	}
	// Temp file should not be left behind.
	entries, _ := os.ReadDir(filepath.Join(dir, PhasePre))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file not cleaned up: %s", e.Name())
		}
	}
}

func TestDefaultStore_RootsUnderHome(t *testing.T) {
	home := "/some/home"
	s := DefaultStore(home)
	want := filepath.Join(home, ".slimference", "run", "compact")
	if s.dir != want {
		t.Fatalf("DefaultStore dir = %q, want %q", s.dir, want)
	}
}

func TestIsKnownPhase(t *testing.T) {
	cases := map[string]bool{
		PhasePre: true, PhasePost: true,
		"middle": false, "": false, "PRE": false,
	}
	for in, want := range cases {
		if got := isKnownPhase(in); got != want {
			t.Errorf("isKnownPhase(%q)=%v want %v", in, got, want)
		}
	}
}

func TestMarker_RoundtripJSON(t *testing.T) {
	in := Marker{Phase: "pre", SessionID: "s", TurnID: "t", Trigger: "auto", TSUnix: 1700000000}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Marker
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("roundtrip drift: %+v != %+v", out, in)
	}
}

func TestWriteMarker_MkdirFailureSurfaces(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.mkdir = func(string, os.FileMode) error { return errors.New("mkdir nope") }
	if err := s.WriteMarker(PhasePre, "s", "t", "auto"); err == nil {
		t.Fatalf("expected mkdir error to surface")
	}
}

func TestWriteMarker_WriteFailureSurfaces(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.write = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	if err := s.WriteMarker(PhasePre, "s", "t", "auto"); err == nil {
		t.Fatalf("expected write error to surface")
	}
}

func TestReadMarker_RejectsUnknownPhase(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, ok := s.ReadMarker("nope", "s"); ok {
		t.Fatalf("ReadMarker should reject unknown phase")
	}
}

func TestReadMarker_RejectsEmptySession(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, ok := s.ReadMarker(PhasePre, ""); ok {
		t.Fatalf("ReadMarker should reject empty session id")
	}
}

func TestCleanupOld_SkipsDirsAndNonJSONFiles(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	preDir := filepath.Join(dir, PhasePre)
	_ = os.MkdirAll(preDir, 0o755)
	// Create a nested directory and a non-json file. CleanupOld must
	// skip them without erroring.
	_ = os.Mkdir(filepath.Join(preDir, "subdir"), 0o755)
	_ = os.WriteFile(filepath.Join(preDir, "readme.txt"), []byte("hi"), 0o644)
	// And one stale json file.
	stale := filepath.Join(preDir, "stale.json")
	_ = os.WriteFile(stale, []byte("{}"), 0o644)
	old := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(stale, old, old)

	removed, err := s.CleanupOld(time.Minute)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed (only the .json), got %d", removed)
	}
	// The subdir and the .txt must still exist.
	if _, err := os.Stat(filepath.Join(preDir, "subdir")); err != nil {
		t.Fatalf("subdir removed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(preDir, "readme.txt")); err != nil {
		t.Fatalf("non-json removed unexpectedly: %v", err)
	}
}

func TestCleanupOld_StatFailureSkipsEntry(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	preDir := filepath.Join(dir, PhasePre)
	_ = os.MkdirAll(preDir, 0o755)
	_ = os.WriteFile(filepath.Join(preDir, "x.json"), []byte("{}"), 0o644)
	s.stat = func(string) (os.FileInfo, error) { return nil, errors.New("stat boom") }
	removed, err := s.CleanupOld(time.Minute)
	if err != nil {
		t.Fatalf("stat failure should be swallowed per-entry, got %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 removed, got %d", removed)
	}
}

func TestCleanupOld_ReadDirFailureSurfacesAsFirstError(t *testing.T) {
	// Create a non-directory at the phase path so ReadDir fails with
	// a real error (not ErrNotExist) on Linux/macOS.
	dir := t.TempDir()
	preBlocker := filepath.Join(dir, PhasePre)
	_ = os.WriteFile(preBlocker, []byte("not-a-dir"), 0o644)
	s := NewStore(dir)
	_, err := s.CleanupOld(time.Minute)
	if err == nil {
		t.Fatalf("expected ReadDir to error when phase path is a file")
	}
}

func TestReadMarker_PhaseFallbackPopulatedOnEmpty(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	// Write a marker with phase missing from JSON.
	target := filepath.Join(dir, PhasePre, "sx.json")
	_ = os.MkdirAll(filepath.Dir(target), 0o755)
	_ = os.WriteFile(target, []byte(`{"session_id":"sx","ts_unix":1}`), 0o644)
	m, ok := s.ReadMarker(PhasePre, "sx")
	if !ok {
		t.Fatalf("read failed")
	}
	if m.Phase != "pre" {
		t.Fatalf("phase fallback not populated, got %q", m.Phase)
	}
}

package toolarchive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestToolArchiveHelpersAndStats(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if got := DefaultDir(home); got != filepath.Join(home, ".slimference", "tool-archive") {
		t.Fatalf("DefaultDir=%q", got)
	}

	dir := t.TempDir()
	stats, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 0 {
		t.Fatalf("unexpected zero stats: %+v", stats)
	}
	if Eligible(Input{}) {
		t.Fatal("empty input should not be eligible")
	}

	want := Stats{Count: 2, Archived: 3, Expanded: 1, BytesRaw: 10, BytesStored: 5}
	if err := SaveStats(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Archived != want.Archived || got.BytesStored != want.BytesStored {
		t.Fatalf("LoadStats=%+v want=%+v", got, want)
	}
	badDirFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badDirFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveStats(badDirFile, Stats{}); err == nil {
		t.Fatal("expected SaveStats mkdir error")
	}

	if Eligible(Input{ToolName: "Bash", Output: strings.Repeat("x", 10)}) {
		t.Fatal("tiny output should not be eligible")
	}
	if !Eligible(Input{ToolUseID: "id", Output: strings.Repeat("line\n", 65)}) {
		t.Fatal("many lines should be eligible")
	}
	if !Eligible(Input{SessionID: "sess", Output: strings.Repeat("x", 3000)}) {
		t.Fatal("session-backed large output should be eligible")
	}
}

func TestToolArchiveFallbackIDListRenderAndPreview(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first, err := Archive(dir, Input{
		ToolName:  "Bash",
		SessionID: "sess 1",
		Command:   "npm test",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.ID == "" || first.ToolUseID != "" {
		t.Fatalf("entry=%+v", first)
	}

	second, err := Archive(dir, Input{
		ToolName:  "Read",
		ToolUseID: "tool-xyz",
		SessionID: "sess-2",
		Command:   "cat out.log",
		Output:    strings.Repeat("other\n", 700),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second == nil {
		t.Fatal("expected second entry")
	}

	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("List=%+v", items)
	}

	rendered := RenderContext(*second)
	for _, want := range []string{"Slimference archived large tool output", "slim://archive/tool-xyz", "slimference expand tool-xyz", "Preview:"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderContext missing %q in %q", want, rendered)
		}
	}

	if got := DefaultPreview("  short \n", 10); got != "short" {
		t.Fatalf("DefaultPreview short=%q", got)
	}
	long := DefaultPreview(strings.Repeat("x", 700), 20)
	if !strings.Contains(long, "archived preview") {
		t.Fatalf("DefaultPreview long=%q", long)
	}
	if got := DefaultPreview("abc", 0); got != "abc" {
		t.Fatalf("DefaultPreview zero limit=%q", got)
	}

	if got := previewText(Input{Preview: "  custom  ", Output: "ignored"}); got != "custom" {
		t.Fatalf("previewText=%q", got)
	}
	if got := sanitizeID(" a/b:c_1 "); got != "abc_1" {
		t.Fatalf("sanitizeID=%q", got)
	}
	if got := sanitizeID(" /: "); got != "" {
		t.Fatalf("sanitizeID empty=%q", got)
	}
	if got := normalizeID("slim://archive/a/b:c"); got != "abc" {
		t.Fatalf("normalizeID=%q", got)
	}
	if got := trimForHash(strings.Repeat("z", 5000)); len(got) != 4096 {
		t.Fatalf("trimForHash len=%d", len(got))
	}
}

func TestToolArchiveErrorAndTrimPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if entry, err := Archive(dir, Input{Output: "tiny"}); err != nil || entry != nil {
		t.Fatalf("Archive ineligible entry=%+v err=%v", entry, err)
	}
	if _, _, err := Expand(dir, "missing"); !os.IsNotExist(err) {
		t.Fatalf("Expand missing err=%v", err)
	}

	if err := os.MkdirAll(entriesDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entriesDir(dir), "bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := List(dir); err == nil {
		t.Fatal("expected broken metadata error")
	}
	if _, err := List(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("missing list err=%v", err)
	}

	statsPath := filepath.Join(dir, statsFilename)
	if err := os.WriteFile(statsPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStats(dir); err == nil {
		t.Fatal("expected broken stats JSON error")
	}
	if _, err := Snapshot(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("missing snapshot err=%v", err)
	}

	dir = t.TempDir()
	for i := 0; i < 102; i++ {
		id := "id-" + strings.Repeat("x", 8) + "-" + string(rune('a'+(i%26))) + "-" + string(rune('0'+(i%10)))
		entry, err := Archive(dir, Input{
			ToolName:  "Bash",
			ToolUseID: id,
			SessionID: "sess",
			Command:   "cmd",
			Output:    strings.Repeat("line\n", 700),
		})
		if err != nil {
			t.Fatal(err)
		}
		if entry == nil {
			t.Fatal("expected archived entry")
		}
	}
	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != maxKeep {
		t.Fatalf("trim count=%d want=%d", len(items), maxKeep)
	}

	entry, err := Archive(t.TempDir(), Input{
		ToolName:  "Bash",
		ToolUseID: "gzip-bad",
		SessionID: "sess",
		Command:   "cmd",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(entriesDir(t.TempDir()), "unused")
	_ = metaPath
	corruptDir := t.TempDir()
	corrupt, err := Archive(corruptDir, Input{
		ToolName:  "Bash",
		ToolUseID: "gzip-bad",
		SessionID: "sess",
		Command:   "cmd",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil {
		t.Fatal(err)
	}
	if corrupt == nil || entry == nil {
		t.Fatal("expected archive entries")
	}
	if err := os.WriteFile(filepath.Join(entriesDir(corruptDir), "gzip-bad.txt.gz"), []byte("not-gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Expand(corruptDir, "gzip-bad"); err == nil {
		t.Fatal("expected gzip reader error")
	}

	truncatedDir := t.TempDir()
	truncated, err := Archive(truncatedDir, Input{
		ToolName:  "Bash",
		ToolUseID: "gzip-truncated",
		SessionID: "sess",
		Command:   "cmd",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil || truncated == nil {
		t.Fatalf("archive err=%v entry=%+v", err, truncated)
	}
	payloadPath := filepath.Join(entriesDir(truncatedDir), "gzip-truncated.txt.gz")
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) < 16 {
		t.Fatalf("payload too short: %d", len(payload))
	}
	if err := os.WriteFile(payloadPath, payload[:len(payload)-8], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Expand(truncatedDir, "gzip-truncated"); err == nil {
		t.Fatal("expected gzip read error")
	}
	if err := os.WriteFile(filepath.Join(entriesDir(corruptDir), "bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEntry(corruptDir, "bad"); err == nil {
		t.Fatal("expected loadEntry JSON error")
	}
}

func TestToolArchiveAdditionalArchiveExpandAndSnapshotBranches(t *testing.T) {
	dir := t.TempDir()
	statsPath := filepath.Join(dir, statsFilename)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statsPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Archive(dir, Input{
		ToolName:  "Bash",
		ToolUseID: "stats-bad",
		SessionID: "sess",
		Command:   "cmd",
		Output:    strings.Repeat("line\n", 700),
	}); err == nil {
		t.Fatal("expected Archive stats load error")
	}

	dir = t.TempDir()
	entry, err := Archive(dir, Input{
		ToolName:  "Bash",
		ToolUseID: "expand-stats-bad",
		SessionID: "sess",
		Command:   "cmd",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil || entry == nil {
		t.Fatalf("archive err=%v entry=%+v", err, entry)
	}
	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, body, err := Expand(dir, entry.ID)
	if err != nil || meta == nil || len(body) == 0 {
		t.Fatalf("expand meta=%+v len=%d err=%v", meta, len(body), err)
	}
	if err := SaveStats(dir, Stats{}); err != nil {
		t.Fatal(err)
	}

	snap, err := Snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Count != 1 || snap.BytesRaw == 0 || snap.BytesStored == 0 || snap.LastArchived.IsZero() {
		t.Fatalf("snapshot=%+v", snap)
	}

	payload, err := defaultCompressArchivePayload("hello world")
	if err != nil || len(payload) == 0 {
		t.Fatalf("compress len=%d err=%v", len(payload), err)
	}
}

func TestToolArchiveAdditionalEligibilityAndTrimPaths(t *testing.T) {
	t.Parallel()

	if Eligible(Input{Output: strings.Repeat("x", 4000)}) {
		t.Fatal("missing identifiers should not be eligible")
	}

	dir := t.TempDir()
	if err := os.MkdirAll(entriesDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(entriesDir(dir), "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry, err := Archive(dir, Input{
		ToolName:  "Bash",
		ToolUseID: "trim-check",
		SessionID: "sess",
		Command:   "cmd",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil || entry == nil {
		t.Fatalf("archive err=%v entry=%+v", err, entry)
	}
	if err := trim(dir, -1); err != nil {
		t.Fatalf("trim keep<0 err=%v", err)
	}
	if items, err := List(dir); err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	emptySnap, err := Snapshot(t.TempDir())
	if err != nil || emptySnap.Count != 0 {
		t.Fatalf("empty snapshot=%+v err=%v", emptySnap, err)
	}

	dir = t.TempDir()
	if err := saveArchivedFixture(dir, "same-a", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if err := saveArchivedFixture(dir, "same-b", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("tie items=%+v", items)
	}

	brokenDir := t.TempDir()
	if err := os.MkdirAll(entriesDir(brokenDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entriesDir(brokenDir), "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(brokenDir); err == nil {
		t.Fatal("expected Snapshot list error")
	}

	if got := compareArchiveEntries(
		Entry{ID: "a", CreatedAt: time.Unix(1, 0)},
		Entry{ID: "b", CreatedAt: time.Unix(2, 0)},
	); got != 1 {
		t.Fatalf("time compare got=%d", got)
	}
	if got := compareArchiveEntries(
		Entry{ID: "a", CreatedAt: time.Unix(3, 0)},
		Entry{ID: "b", CreatedAt: time.Unix(2, 0)},
	); got != -1 {
		t.Fatalf("time reverse compare got=%d", got)
	}
	if got := compareArchiveEntries(
		Entry{ID: "a", CreatedAt: time.Unix(3, 0)},
		Entry{ID: "b", CreatedAt: time.Unix(3, 0)},
	); got <= 0 {
		t.Fatalf("id compare got=%d", got)
	}
}

func TestToolArchiveInjectedErrorBranches(t *testing.T) {
	dir := t.TempDir()
	input := Input{
		ToolName:  "Bash",
		ToolUseID: "tool-1",
		SessionID: "sess",
		Command:   "cmd",
		Output:    strings.Repeat("line\n", 700),
	}

	origCompress := compressArchivePayload
	origWrite := toolArchiveWriteFile
	origMarshal := toolArchiveMarshalIndent
	origReadDir := toolArchiveReadDir
	origOpen := toolArchiveOpen
	origRemove := toolArchiveRemove
	defer func() {
		compressArchivePayload = origCompress
		toolArchiveWriteFile = origWrite
		toolArchiveMarshalIndent = origMarshal
		toolArchiveReadDir = origReadDir
		toolArchiveOpen = origOpen
		toolArchiveRemove = origRemove
	}()

	compressArchivePayload = func(string) ([]byte, error) { return nil, errors.New("compress") }
	if _, err := Archive(dir, input); err == nil {
		t.Fatal("expected Archive compress error")
	}

	compressArchivePayload = origCompress
	toolArchiveWriteFile = func(string, []byte, os.FileMode) error { return errors.New("write") }
	if _, err := Archive(dir, input); err == nil {
		t.Fatal("expected Archive write error")
	}
	if err := SaveStats(dir, Stats{}); err == nil {
		t.Fatal("expected SaveStats write error")
	}

	toolArchiveWriteFile = origWrite
	toolArchiveMarshalIndent = func(any, string, string) ([]byte, error) { return nil, errors.New("marshal") }
	if _, err := Archive(dir, input); err == nil {
		t.Fatal("expected Archive marshal error")
	}
	if err := SaveStats(dir, Stats{}); err == nil {
		t.Fatal("expected SaveStats marshal error")
	}

	toolArchiveMarshalIndent = origMarshal
	toolArchiveReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	if _, err := List(dir); err == nil {
		t.Fatal("expected List read dir error")
	}

	toolArchiveReadDir = origReadDir
	entry, err := Archive(dir, input)
	if err != nil || entry == nil {
		t.Fatalf("Archive err=%v entry=%+v", err, entry)
	}
	toolArchiveOpen = func(string) (*os.File, error) { return nil, errors.New("open") }
	if _, _, err := Expand(dir, entry.ID); err == nil {
		t.Fatal("expected Expand open error")
	}

	toolArchiveOpen = origOpen
	toolArchiveRemove = func(string) error { return errors.New("remove") }
	if err := trim(dir, 0); err == nil {
		t.Fatal("expected trim remove error")
	}
}

func TestToolArchiveAdditionalInjectedArchiveBranches(t *testing.T) {
	dir := t.TempDir()
	input := Input{
		ToolName:  "Bash",
		ToolUseID: "tool-archive-extra",
		SessionID: "sess",
		Command:   "cmd",
		Output:    strings.Repeat("line\n", 700),
	}

	origMkdir := toolArchiveMkdirAll
	origReadFile := toolArchiveReadFile
	origWrite := toolArchiveWriteFile
	origRemove := toolArchiveRemove
	origGzip := newArchiveGzipWriter
	defer func() {
		toolArchiveMkdirAll = origMkdir
		toolArchiveReadFile = origReadFile
		toolArchiveWriteFile = origWrite
		toolArchiveRemove = origRemove
		newArchiveGzipWriter = origGzip
	}()

	toolArchiveMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir") }
	if _, err := Archive(dir, input); err == nil {
		t.Fatal("expected Archive mkdir error")
	}

	toolArchiveMkdirAll = origMkdir
	toolArchiveWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, statsFilename) {
			return errors.New("meta write")
		}
		return origWrite(name, data, perm)
	}
	if _, err := Archive(t.TempDir(), input); err == nil {
		t.Fatal("expected Archive metadata write error")
	}

	toolArchiveWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, statsFilename) {
			return errors.New("stats write")
		}
		return origWrite(name, data, perm)
	}
	if _, err := Archive(t.TempDir(), input); err == nil {
		t.Fatal("expected Archive stats write error")
	}

	archiveTrimDir := t.TempDir()
	for i := 0; i < maxKeep; i++ {
		id := fmt.Sprintf("archive-trim-%03d", i)
		if err := saveArchivedFixture(archiveTrimDir, id, time.Unix(int64(i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	toolArchiveWriteFile = origWrite
	toolArchiveRemove = func(string) error { return errors.New("archive trim") }
	if _, err := Archive(archiveTrimDir, input); err == nil {
		t.Fatal("expected Archive trim error")
	}

	trimDir := t.TempDir()
	for i := 0; i < maxKeep; i++ {
		id := fmt.Sprintf("trim-extra-%03d", i)
		if err := saveArchivedFixture(trimDir, id, time.Unix(int64(i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	toolArchiveWriteFile = origWrite
	toolArchiveRemove = func(path string) error {
		if strings.HasSuffix(path, ".json") {
			return os.ErrNotExist
		}
		return origRemove(path)
	}
	if err := trim(trimDir, maxKeep-1); err != nil {
		t.Fatalf("trim isNotExist err=%v", err)
	}

	if got := sanitizeID("ABC"); got != "ABC" {
		t.Fatalf("uppercase sanitize=%q", got)
	}
	badDirFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badDirFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(badDirFile); err == nil {
		t.Fatal("expected Snapshot load stats error")
	}
	toolArchiveReadFile = func(string) ([]byte, error) { return nil, errors.New("read file") }
	if _, err := List(trimDir); err == nil {
		t.Fatal("expected List read file error")
	}
	toolArchiveReadFile = origReadFile
	expandDir := t.TempDir()
	entryForExpand, err := Archive(expandDir, input)
	if err != nil || entryForExpand == nil {
		t.Fatalf("expand archive err=%v entry=%+v", err, entryForExpand)
	}
	toolArchiveReadFile = func(path string) ([]byte, error) {
		if strings.HasSuffix(path, statsFilename) {
			return nil, errors.New("expand stats")
		}
		return origReadFile(path)
	}
	if meta, body, err := Expand(expandDir, entryForExpand.ID); err != nil || meta == nil || len(body) == 0 {
		t.Fatalf("expand stats meta=%+v len=%d err=%v", meta, len(body), err)
	}
	toolArchiveReadFile = origReadFile
	listErrDir := t.TempDir()
	if err := os.MkdirAll(entriesDir(listErrDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entriesDir(listErrDir), "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := trim(listErrDir, 0); err == nil {
		t.Fatal("expected trim list error")
	}

	removeDir := t.TempDir()
	if err := saveArchivedFixture(removeDir, "remove-gz", time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}
	toolArchiveRemove = func(path string) error {
		if strings.HasSuffix(path, ".txt.gz") {
			return errors.New("remove gz")
		}
		return origRemove(path)
	}
	if err := trim(removeDir, 0); err == nil {
		t.Fatal("expected trim gz remove error")
	}

	newArchiveGzipWriter = func(io.Writer) io.WriteCloser { return archiveWriteCloser{writeErr: errors.New("gzip write")} }
	if _, err := defaultCompressArchivePayload("x"); err == nil {
		t.Fatal("expected gzip write error")
	}
	newArchiveGzipWriter = func(io.Writer) io.WriteCloser { return archiveWriteCloser{closeErr: errors.New("gzip close")} }
	if _, err := defaultCompressArchivePayload("x"); err == nil {
		t.Fatal("expected gzip close error")
	}
}

func saveArchivedFixture(dir string, id string, created time.Time) error {
	if err := os.MkdirAll(entriesDir(dir), 0o755); err != nil {
		return err
	}
	entry := Entry{
		ID:         id,
		URI:        "slim://archive/" + id,
		CreatedAt:  created,
		ToolName:   "Bash",
		ToolUseID:  id,
		SessionID:  "sess",
		Command:    "cmd",
		Preview:    "preview",
		OutputSize: 4,
		StoredSize: 4,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(entriesDir(dir), id+".json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(entriesDir(dir), id+".txt.gz"), []byte("fake"), 0o644)
}

type archiveWriteCloser struct {
	writeErr error
	closeErr error
}

func (a archiveWriteCloser) Write(p []byte) (int, error) {
	if a.writeErr != nil {
		return 0, a.writeErr
	}
	return len(p), nil
}

func (a archiveWriteCloser) Close() error {
	return a.closeErr
}

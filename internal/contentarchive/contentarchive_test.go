package contentarchive

import (
	"bytes"
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

func sampleInput() Input {
	return Input{
		SessionID:    "sess-1",
		MessageIndex: 3,
		BlockIndex:   1,
		SubLayer:     "comment_strip",
		Original:     strings.Repeat("// some comment line that is long enough to be eligible for archiving\n", 5),
		Preview:      "preview head",
	}
}

func TestDefaultDir(t *testing.T) {

	if got := DefaultDir("/tmp/home"); got != "/tmp/home/.slimference/content-archive" {
		t.Fatalf("got %q", got)
	}
}

func TestEligible(t *testing.T) {

	if Eligible(Input{Original: ""}) {
		t.Fatal("empty must not be eligible")
	}
	if Eligible(Input{Original: "tiny"}) {
		t.Fatal("short input must not be eligible")
	}
	if !Eligible(sampleInput()) {
		t.Fatal("sample must be eligible")
	}
}

func TestLimitsDefaults(t *testing.T) {

	zero := Limits{}
	if zero.maxEntries() != defaultMaxEntries {
		t.Fatalf("default max entries: %d", zero.maxEntries())
	}
	if zero.maxBytes() != defaultMaxBytes {
		t.Fatalf("default max bytes: %d", zero.maxBytes())
	}
	custom := Limits{MaxEntries: 17, MaxBytes: 99}
	if custom.maxEntries() != 17 || custom.maxBytes() != 99 {
		t.Fatalf("custom limits: %+v", custom)
	}
}

func TestPut_IneligibleSkips(t *testing.T) {

	dir := t.TempDir()
	got, err := Put(dir, Input{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil entry for ineligible: %#v", got)
	}
}

func TestPutGet_RoundTrip(t *testing.T) {

	dir := t.TempDir()
	entry, err := Put(dir, sampleInput(), Limits{})
	if err != nil || entry == nil {
		t.Fatalf("put: entry=%#v err=%v", entry, err)
	}
	if entry.URI != uriScheme+entry.ID {
		t.Fatalf("uri mismatch: %q", entry.URI)
	}
	if entry.OutputSize() == 0 {
		t.Fatal("expected non-zero size")
	}

	gotEntry, body, err := Get(dir, entry.URI)
	if err != nil {
		t.Fatal(err)
	}
	if gotEntry.ID != entry.ID {
		t.Fatalf("id mismatch: %q vs %q", gotEntry.ID, entry.ID)
	}
	if !bytes.Equal(body, []byte(sampleInput().Original)) {
		t.Fatalf("payload mismatch")
	}
	gotEntryByLegacy, _, err := Get(dir, legacyURIScheme+entry.ID)
	if err != nil {
		t.Fatalf("legacy uri get: %v", err)
	}
	if gotEntryByLegacy.ID != entry.ID {
		t.Fatalf("legacy id mismatch")
	}
}

func TestGet_EmptyID(t *testing.T) {

	if _, _, err := Get(t.TempDir(), "  "); err == nil {
		t.Fatal("expected empty id error")
	}
}

func TestGet_MissingFile(t *testing.T) {

	if _, _, err := Get(t.TempDir(), "missing-id"); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestGet_BadGzip(t *testing.T) {

	dir := t.TempDir()
	if err := os.MkdirAll(entriesDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "abc123"
	meta := Entry{ID: id, URI: uriScheme + id, OriginalSize: 5, StoredSize: 1}
	data, _ := json.MarshalIndent(&meta, "", "  ")
	if err := os.WriteFile(filepath.Join(entriesDir(dir), id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entriesDir(dir), id+".txt.gz"), []byte("not-gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Get(dir, id); err == nil {
		t.Fatal("expected gzip error")
	}
}

func TestGet_TruncatedGzip(t *testing.T) {

	dir := t.TempDir()
	entry, err := Put(dir, sampleInput(), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(entriesDir(dir), entry.ID+".txt.gz")
	contents, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	// Keep enough bytes for gzip.NewReader to accept the header but cut the
	// deflate payload so io.ReadAll fails mid-stream.
	cut := len(contents) - 5
	if cut < 12 {
		cut = 12
	}
	if err := os.WriteFile(payload, contents[:cut], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Get(dir, entry.ID); err == nil {
		t.Fatal("expected read error on truncated gzip body")
	}
}

func TestGet_HeaderOnlyGzip(t *testing.T) {

	dir := t.TempDir()
	entry, err := Put(dir, sampleInput(), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(entriesDir(dir), entry.ID+".txt.gz")
	if err := os.WriteFile(payload, []byte{0x1f, 0x8b}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Get(dir, entry.ID); err == nil {
		t.Fatal("expected gzip new-reader error on truncated header")
	}
}

func TestPut_MkdirError(t *testing.T) {

	saved := mkdirAll
	t.Cleanup(func() { mkdirAll = saved })
	mkdirAll = func(string, os.FileMode) error { return errors.New("mkdir fail") }
	if _, err := Put(t.TempDir(), sampleInput(), Limits{}); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestPut_CompressError(t *testing.T) {

	saved := compressBytes
	t.Cleanup(func() { compressBytes = saved })
	compressBytes = func(string) ([]byte, error) { return nil, errors.New("compress fail") }
	if _, err := Put(t.TempDir(), sampleInput(), Limits{}); err == nil {
		t.Fatal("expected compress error")
	}
}

func TestPut_WriteFileError(t *testing.T) {

	dir := t.TempDir()
	saved := writeFile
	t.Cleanup(func() { writeFile = saved })
	calls := 0
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		calls++
		if calls == 1 {
			return errors.New("write fail")
		}
		return os.WriteFile(name, data, perm)
	}
	if _, err := Put(dir, sampleInput(), Limits{}); err == nil {
		t.Fatal("expected write error")
	}
}

func TestPut_MetaWriteError(t *testing.T) {

	dir := t.TempDir()
	saved := writeFile
	t.Cleanup(func() { writeFile = saved })
	calls := 0
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		calls++
		if calls == 2 {
			return errors.New("meta write fail")
		}
		return os.WriteFile(name, data, perm)
	}
	if _, err := Put(dir, sampleInput(), Limits{}); err == nil {
		t.Fatal("expected meta write error")
	}
}

func TestPut_MarshalIndentError(t *testing.T) {

	saved := marshalIndent
	t.Cleanup(func() { marshalIndent = saved })
	marshalIndent = func(any, string, string) ([]byte, error) { return nil, errors.New("marshal fail") }
	if _, err := Put(t.TempDir(), sampleInput(), Limits{}); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestPut_LoadStatsError(t *testing.T) {

	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Put(dir, sampleInput(), Limits{}); err == nil {
		t.Fatal("expected stats parse error")
	}
}

func TestPut_StatsSaveError(t *testing.T) {

	dir := t.TempDir()
	saved := writeFile
	t.Cleanup(func() { writeFile = saved })
	calls := 0
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		calls++
		// payload write (1), meta write (2), stats save (3) -> fail
		if calls == 3 {
			return errors.New("stats save fail")
		}
		return os.WriteFile(name, data, perm)
	}
	if _, err := Put(dir, sampleInput(), Limits{}); err == nil {
		t.Fatal("expected stats save error")
	}
}

func TestPut_EvictionWhenOverEntryLimit(t *testing.T) {

	dir := t.TempDir()
	limits := Limits{MaxEntries: 2}
	for i := 0; i < 4; i++ {
		input := sampleInput()
		input.MessageIndex = i
		input.Original = fmt.Sprintf("content-%d-%s", i, strings.Repeat("x", 80))
		entry, err := Put(dir, input, limits)
		if err != nil || entry == nil {
			t.Fatalf("put %d: entry=%#v err=%v", i, entry, err)
		}
	}
	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) > 2 {
		t.Fatalf("expected eviction, have %d entries", len(items))
	}
	stats, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Evictions == 0 {
		t.Fatal("expected eviction counter to advance")
	}
}

func TestPut_EvictionWhenOverByteLimit(t *testing.T) {

	dir := t.TempDir()
	limits := Limits{MaxBytes: 1}
	for i := 0; i < 3; i++ {
		input := sampleInput()
		input.MessageIndex = i
		input.Original = fmt.Sprintf("byte-cap-%d-%s", i, strings.Repeat("y", 200))
		if _, err := Put(dir, input, limits); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) > 1 {
		t.Fatalf("byte-cap eviction failed: %d entries", len(items))
	}
}

func TestPut_EvictionRemoveError(t *testing.T) {

	dir := t.TempDir()
	if _, err := Put(dir, sampleInput(), Limits{}); err != nil {
		t.Fatal(err)
	}
	saved := removeFile
	t.Cleanup(func() { removeFile = saved })
	removeFile = func(string) error { return errors.New("remove fail") }
	limits := Limits{MaxEntries: 0, MaxBytes: 1}
	in := sampleInput()
	in.MessageIndex = 99
	in.Original = strings.Repeat("zzz", 64)
	if _, err := Put(dir, in, limits); err == nil {
		t.Fatal("expected eviction remove error to surface")
	}
}

func TestPut_EvictionListError(t *testing.T) {

	dir := t.TempDir()
	if _, err := Put(dir, sampleInput(), Limits{}); err != nil {
		t.Fatal(err)
	}
	saved := readDir
	t.Cleanup(func() { readDir = saved })
	calls := 0
	readDir = func(name string) ([]os.DirEntry, error) {
		calls++
		// First call is during the second Put's enforceLimits -> fail.
		if calls >= 1 {
			return nil, errors.New("read dir fail")
		}
		return os.ReadDir(name)
	}
	in := sampleInput()
	in.MessageIndex = 7
	if _, err := Put(dir, in, Limits{MaxEntries: 1}); err == nil {
		t.Fatal("expected list error during eviction")
	}
}

func TestPut_SnapshotPathPreservesCounters(t *testing.T) {

	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		in := sampleInput()
		in.MessageIndex = i
		if _, err := Put(dir, in, Limits{}); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Archived != 3 || stats.Count == 0 {
		t.Fatalf("counters off: %+v", stats)
	}
}

func TestRecordReInject(t *testing.T) {

	dir := t.TempDir()
	if _, err := Put(dir, sampleInput(), Limits{}); err != nil {
		t.Fatal(err)
	}
	RecordReInject(dir)
	stats, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ReInjectCount != 1 || stats.LastReInjected.IsZero() {
		t.Fatalf("re-inject not recorded: %+v", stats)
	}
}

func TestRecordReInjectBatch_NoOpOnZero(t *testing.T) {
	dir := t.TempDir()
	if _, err := Put(dir, sampleInput(), Limits{}); err != nil {
		t.Fatal(err)
	}
	stats0, _ := LoadStats(dir)
	RecordReInjectBatch(dir, 0)
	RecordReInjectBatch(dir, -5)
	statsAfter, _ := LoadStats(dir)
	if statsAfter.ReInjectCount != stats0.ReInjectCount {
		t.Fatalf("zero/negative count must be no-op: %+v vs %+v", statsAfter, stats0)
	}
}

func TestRecordReInjectBatch_AdvancesByN(t *testing.T) {
	dir := t.TempDir()
	if _, err := Put(dir, sampleInput(), Limits{}); err != nil {
		t.Fatal(err)
	}
	RecordReInjectBatch(dir, 3)
	stats, _ := LoadStats(dir)
	if stats.ReInjectCount != 3 {
		t.Fatalf("expected 3, got %d", stats.ReInjectCount)
	}
}

func TestRecordReInject_LoadErrorSilent(t *testing.T) {

	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Must not panic.
	RecordReInject(dir)
}

func TestList_NoDir(t *testing.T) {

	items, err := List(t.TempDir())
	if err != nil || items != nil {
		t.Fatalf("expected nil/nil, got %#v %v", items, err)
	}
}

func TestList_ReadDirError(t *testing.T) {

	saved := readDir
	t.Cleanup(func() { readDir = saved })
	readDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("nope") }
	if _, err := List(t.TempDir()); err == nil {
		t.Fatal("expected read dir error")
	}
}

func TestList_MalformedEntry(t *testing.T) {

	dir := t.TempDir()
	if err := os.MkdirAll(entriesDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entriesDir(dir), "bad.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(dir); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestList_ReadFileError(t *testing.T) {

	dir := t.TempDir()
	if _, err := Put(dir, sampleInput(), Limits{}); err != nil {
		t.Fatal(err)
	}
	saved := readFile
	t.Cleanup(func() { readFile = saved })
	readFile = func(name string) ([]byte, error) {
		if strings.HasSuffix(name, ".json") && strings.Contains(name, "/entries/") {
			return nil, errors.New("entry read fail")
		}
		return os.ReadFile(name)
	}
	if _, err := List(dir); err == nil {
		t.Fatal("expected read error")
	}
}

func TestList_SkipsDirectoriesAndNonJSON(t *testing.T) {

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(entriesDir(dir), "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entriesDir(dir), "ignore.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected zero entries, got %d", len(items))
	}
}

func TestLoadStats_AbsentReturnsZero(t *testing.T) {

	stats, err := LoadStats(t.TempDir())
	if err != nil || stats.Count != 0 {
		t.Fatalf("unexpected: %+v %v", stats, err)
	}
}

func TestLoadStats_Malformed(t *testing.T) {

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStats(dir); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestLoadStats_ReadError(t *testing.T) {

	saved := readFile
	t.Cleanup(func() { readFile = saved })
	readFile = func(string) ([]byte, error) { return nil, errors.New("read fail") }
	if _, err := LoadStats(t.TempDir()); err == nil {
		t.Fatal("expected read error")
	}
}

func TestSaveStats_MkdirError(t *testing.T) {

	saved := mkdirAll
	t.Cleanup(func() { mkdirAll = saved })
	mkdirAll = func(string, os.FileMode) error { return errors.New("mkdir fail") }
	if err := SaveStats(t.TempDir(), Stats{}); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestSaveStats_MarshalError(t *testing.T) {

	saved := marshalIndent
	t.Cleanup(func() { marshalIndent = saved })
	marshalIndent = func(any, string, string) ([]byte, error) { return nil, errors.New("marshal fail") }
	if err := SaveStats(t.TempDir(), Stats{}); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestSnapshot_PopulatesDerivedFields(t *testing.T) {

	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		in := sampleInput()
		in.MessageIndex = i
		if _, err := Put(dir, in, Limits{}); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := Snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 2 || stats.BytesRaw == 0 || stats.BytesStored == 0 {
		t.Fatalf("snapshot off: %+v", stats)
	}
	if stats.LastArchived.IsZero() {
		t.Fatal("expected last archived timestamp")
	}
}

func TestSnapshot_LoadStatsError(t *testing.T) {

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(dir); err == nil {
		t.Fatal("expected snapshot error")
	}
}

func TestSnapshot_ListError(t *testing.T) {

	saved := readDir
	t.Cleanup(func() { readDir = saved })
	readDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("dir fail") }
	if _, err := Snapshot(t.TempDir()); err == nil {
		t.Fatal("expected list error")
	}
}

func TestReference(t *testing.T) {

	if got := Reference(nil); got != "" {
		t.Fatalf("nil entry must produce empty: %q", got)
	}
	got := Reference(&Entry{URI: uriScheme + "abc"})
	if !strings.Contains(got, uriScheme+"abc") {
		t.Fatalf("missing uri: %q", got)
	}
}

func TestDefaultPreview(t *testing.T) {

	if DefaultPreview("short", 0) != "short" {
		t.Fatal("short preview unchanged")
	}
	long := strings.Repeat("a", 1200)
	got := DefaultPreview(long, 100)
	if !strings.Contains(got, "[archived preview") {
		t.Fatalf("long preview missing tail: %q", got)
	}
	if DefaultPreview("hi", -1) != "hi" {
		t.Fatal("limit < 1 must default")
	}
}

func TestPreviewText_DefaultsWhenEmpty(t *testing.T) {

	in := sampleInput()
	in.Preview = "  "
	got := previewText(in)
	if got == "" {
		t.Fatal("expected default preview")
	}
}

func TestNormalizeID(t *testing.T) {

	if got := normalizeID("local-archive://abc"); got != "abc" {
		t.Fatalf("uri prefix not stripped: %q", got)
	}
	if got := normalizeID("slim://archive/xyz"); got != "xyz" {
		t.Fatalf("legacy prefix not stripped: %q", got)
	}
	if got := normalizeID("  zzz!!@@-_  "); got != "zzz-_" {
		t.Fatalf("sanitised id wrong: %q", got)
	}
}

func TestCompareEntries(t *testing.T) {

	a := Entry{ID: "a", CreatedAt: mustTime(t, "2024-01-01T00:00:00Z")}
	b := Entry{ID: "b", CreatedAt: mustTime(t, "2024-01-02T00:00:00Z")}
	if compareEntries(a, b) <= 0 {
		t.Fatal("older must come after newer")
	}
	if compareEntries(b, a) >= 0 {
		t.Fatal("newer must come before older")
	}
	c := Entry{ID: "z", CreatedAt: a.CreatedAt}
	d := Entry{ID: "a", CreatedAt: a.CreatedAt}
	if compareEntries(c, d) >= 0 {
		t.Fatal("when equal time, larger id must sort first")
	}
}

func TestDefaultCompressBytes_WriterError(t *testing.T) {

	saved := newGzipWriter
	t.Cleanup(func() { newGzipWriter = saved })
	newGzipWriter = func(io.Writer) io.WriteCloser { return errWriter{} }
	if _, err := defaultCompressBytes("x"); err == nil {
		t.Fatal("expected write error")
	}
}

func TestDefaultCompressBytes_CloseError(t *testing.T) {

	saved := newGzipWriter
	t.Cleanup(func() { newGzipWriter = saved })
	newGzipWriter = func(io.Writer) io.WriteCloser { return closeErrWriter{} }
	if _, err := defaultCompressBytes("x"); err == nil {
		t.Fatal("expected close error")
	}
}

func TestTrimForHash(t *testing.T) {

	if got := trimForHash("short"); got != "short" {
		t.Fatal("short unchanged")
	}
	long := strings.Repeat("a", 5000)
	if got := trimForHash(long); len(got) != 4096 {
		t.Fatalf("expected 4096 cap, got %d", len(got))
	}
}

func TestGet_MetaPresentPayloadMissing(t *testing.T) {

	dir := t.TempDir()
	if err := os.MkdirAll(entriesDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "orphaned"
	meta := Entry{ID: id, URI: uriScheme + id, OriginalSize: 1, StoredSize: 1}
	data, _ := json.MarshalIndent(&meta, "", "  ")
	if err := os.WriteFile(filepath.Join(entriesDir(dir), id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Get(dir, id); err == nil {
		t.Fatal("expected open error when payload missing")
	}
}

func TestGet_StatsLoadFailureSwallowed(t *testing.T) {

	dir := t.TempDir()
	entry, err := Put(dir, sampleInput(), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt stats.json after the put so the post-Get update path errors.
	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Get(dir, entry.ID); err != nil {
		t.Fatalf("stats load failure must be swallowed: %v", err)
	}
}

func TestLoadEntry_MalformedJSON(t *testing.T) {

	dir := t.TempDir()
	if err := os.MkdirAll(entriesDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "broken"
	if err := os.WriteFile(filepath.Join(entriesDir(dir), id+".json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Get(dir, id); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestSanitizeID_AllCharClasses(t *testing.T) {

	if got := sanitizeID("AbC-123_zZ!@#"); got != "AbC-123_zZ" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestEnforceLimits_GzRemoveError(t *testing.T) {

	dir := t.TempDir()
	if _, err := Put(dir, sampleInput(), Limits{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Put(dir, Input{
		SessionID:    "sess-2",
		MessageIndex: 99,
		Original:     strings.Repeat("z", 200),
	}, Limits{}); err != nil {
		t.Fatal(err)
	}
	saved := removeFile
	t.Cleanup(func() { removeFile = saved })
	calls := 0
	removeFile = func(name string) error {
		calls++
		if calls == 1 {
			// first remove (the .json sibling) succeeds
			return os.Remove(name)
		}
		return errors.New("gz remove fail")
	}
	in := Input{
		SessionID:    "sess-3",
		MessageIndex: 999,
		Original:     strings.Repeat("a", 200),
	}
	if _, err := Put(dir, in, Limits{MaxEntries: 1}); err == nil {
		t.Fatal("expected gz remove error to surface")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write fail") }
func (errWriter) Close() error              { return nil }

type closeErrWriter struct{}

func (closeErrWriter) Write(p []byte) (int, error) { return len(p), nil }
func (closeErrWriter) Close() error                { return errors.New("close fail") }

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return v
}

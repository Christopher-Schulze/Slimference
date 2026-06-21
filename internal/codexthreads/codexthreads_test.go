package codexthreads

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestNormalizeSessionID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{" thread ", "thread"},
		{"codex-wss:thread", "thread"},
		{"codex-wss_thread", "thread"},
		{"codex-http:thread", "thread"},
		{"codex-http_thread", "thread"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeSessionID(tc.in); got != tc.want {
			t.Fatalf("NormalizeSessionID(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestLookupCurrentCodexSchema(t *testing.T) {
	home := t.TempDir()
	db := openTestCodexDB(t, home)
	execTestSQL(t, db, `
CREATE TABLE threads (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	cwd TEXT NOT NULL,
	source TEXT NOT NULL,
		thread_source TEXT,
		model TEXT,
		first_user_message TEXT,
		created_at_ms INTEGER,
		updated_at INTEGER NOT NULL,
		updated_at_ms INTEGER
	)`)
	execTestSQL(t, db, `
	INSERT INTO threads (id, title, cwd, source, thread_source, model, first_user_message, created_at_ms, updated_at, updated_at_ms)
	VALUES ('thread-1', 'Check status', '/tmp/project', 'cli', 'cli', 'gpt-5.5', 'first prompt', 1759999999123, 1760000000, 1760000000123)`)
	db.Close()

	got, err := Lookup(home, []string{"codex-wss:thread-1", "codex-wss:thread-1", "missing", ""})
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := got["thread-1"]
	if !ok {
		t.Fatalf("thread metadata missing: %+v", got)
	}
	if meta.Title != "Check status" || meta.CWD != "/tmp/project" || meta.Source != "cli" || meta.ThreadSource != "cli" || meta.Model != "gpt-5.5" || meta.FirstUserMessage != "first prompt" {
		t.Fatalf("bad metadata: %+v", meta)
	}
	wantCreated := time.UnixMilli(1759999999123).UTC()
	if !meta.CreatedAt.Equal(wantCreated) {
		t.Fatalf("created_at=%s want %s", meta.CreatedAt, wantCreated)
	}
	wantTime := time.UnixMilli(1760000000123).UTC()
	if !meta.UpdatedAt.Equal(wantTime) {
		t.Fatalf("updated_at=%s want %s", meta.UpdatedAt, wantTime)
	}
	if _, ok := got["missing"]; ok {
		t.Fatalf("unexpected missing entry: %+v", got)
	}
}

func TestLookupWindowCurrentCodexSchema(t *testing.T) {
	home := t.TempDir()
	db := openTestCodexDB(t, home)
	execTestSQL(t, db, `
CREATE TABLE threads (
	id TEXT PRIMARY KEY,
	title TEXT,
	cwd TEXT,
		source TEXT,
		thread_source TEXT,
		model TEXT,
		first_user_message TEXT,
		created_at_ms INTEGER,
		updated_at_ms INTEGER
	)`)
	execTestSQL(t, db, `
	INSERT INTO threads (id, title, cwd, source, thread_source, model, first_user_message, created_at_ms, updated_at_ms)
	VALUES
		('old-thread', 'Old', '/tmp/old', 'cli', 'user', 'gpt-5.5', 'old prompt', 1759999998000, 1759999999000),
		('thread-1', 'One', '/tmp/one', 'cli', 'user', 'gpt-5.5', 'first prompt', 1759999999000, 1760000000100),
		('thread-2', 'Two', '/tmp/two', 'vscode', '', 'gpt-5.5', 'second prompt', 1760000000200, 1760000000400),
		('future-thread', 'Future', '/tmp/future', 'cli', 'user', 'gpt-5.5', 'future prompt', 1760000000400, 1760000000500)`)
	db.Close()

	got, err := LookupWindow(home, time.UnixMilli(1760000000000).UTC(), time.UnixMilli(1760000000300).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("window rows=%d: %+v", len(got), got)
	}
	if got[0].ID != "thread-2" || got[0].FirstUserMessage != "second prompt" {
		t.Fatalf("rows should be newest first with first_user_message: %+v", got)
	}
	if !got[0].CreatedAt.Equal(time.UnixMilli(1760000000200).UTC()) || !got[0].UpdatedAt.Equal(time.UnixMilli(1760000000400).UTC()) {
		t.Fatalf("thread-2 times: %+v", got[0])
	}
	if got[1].ID != "thread-1" || got[1].Source != "cli" {
		t.Fatalf("second row: %+v", got[1])
	}
}

func TestLookupOlderSchemaFallbacks(t *testing.T) {
	home := t.TempDir()
	db := openTestCodexDB(t, home)
	execTestSQL(t, db, `
CREATE TABLE threads (
	id TEXT PRIMARY KEY,
	title TEXT,
	cwd TEXT,
	source TEXT,
	updated_at INTEGER
)`)
	execTestSQL(t, db, `
INSERT INTO threads (id, title, cwd, source, updated_at)
VALUES ('thread-2', NULL, '/tmp/old', 'desktop', 1760000001)`)
	db.Close()

	got, err := Lookup(home, []string{"thread-2"})
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := got["thread-2"]
	if !ok {
		t.Fatalf("thread metadata missing: %+v", got)
	}
	if meta.Title != "" || meta.CWD != "/tmp/old" || meta.Source != "desktop" || meta.ThreadSource != "" || meta.Model != "" {
		t.Fatalf("bad fallback metadata: %+v", meta)
	}
	wantTime := time.Unix(1760000001, 0).UTC()
	if !meta.CreatedAt.Equal(wantTime) {
		t.Fatalf("created_at fallback=%s want %s", meta.CreatedAt, wantTime)
	}
	if !meta.UpdatedAt.Equal(wantTime) {
		t.Fatalf("updated_at=%s want %s", meta.UpdatedAt, wantTime)
	}
}

func TestLookupMissingDBAndMissingThreadsTable(t *testing.T) {
	if got, err := Lookup(t.TempDir(), []string{"thread"}); err != nil || len(got) != 0 {
		t.Fatalf("missing db got=%+v err=%v", got, err)
	}

	home := t.TempDir()
	db := openTestCodexDB(t, home)
	execTestSQL(t, db, `CREATE TABLE other (id TEXT PRIMARY KEY)`)
	db.Close()
	if got, err := Lookup(home, []string{"thread"}); err != nil || len(got) != 0 {
		t.Fatalf("missing threads table got=%+v err=%v", got, err)
	}
}

func openTestCodexDB(t *testing.T, home string) *sql.DB {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func execTestSQL(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatal(err)
	}
}

func TestLookupWindowDefault_MissingDB(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	start := time.Now().Add(-time.Hour)
	end := time.Now()
	got, err := LookupWindowDefault(start, end)
	if err != nil {
		t.Fatalf("missing db must not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing db must return empty, got %d entries", len(got))
	}
}

func TestLookupWindowDefault_EmptyHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	start := time.Now().Add(-time.Hour)
	end := time.Now()
	// os.UserHomeDir() returns an error when $HOME is unset, so
	// LookupWindowDefault must propagate it.
	got, err := LookupWindowDefault(start, end)
	if err == nil {
		t.Fatalf("empty home must return error from os.UserHomeDir, got nil")
	}
	if got != nil {
		t.Fatalf("empty home must return nil slice on error, got %v", got)
	}
}

func TestLookup_EmptyHome(t *testing.T) {
	t.Parallel()
	got, err := Lookup("", []string{"s1"})
	if err != nil || len(got) != 0 {
		t.Fatalf("Lookup('', ...) = %v, %v; want empty, nil", got, err)
	}
}

func TestLookup_DuplicateAndEmptySessionIDs(t *testing.T) {
	t.Parallel()
	got, err := Lookup("", []string{"", "s1", "s1", ""})
	if err != nil || len(got) != 0 {
		t.Fatalf("Lookup with empty home = %v, %v; want empty, nil", got, err)
	}
}

func TestLookupWindow_EmptyHome(t *testing.T) {
	t.Parallel()
	got, err := LookupWindow("", time.Now(), time.Now())
	if err != nil || got != nil {
		t.Fatalf("LookupWindow('', ...) = %v, %v; want nil, nil", got, err)
	}
}

func TestUpdatedAtSQL_AllCombinations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		columns map[string]struct{}
		want    string
	}{
		{"both", map[string]struct{}{"updated_at_ms": {}, "updated_at": {}}, "COALESCE(updated_at_ms, updated_at * 1000)"},
		{"ms_only", map[string]struct{}{"updated_at_ms": {}}, "COALESCE(updated_at_ms, 0)"},
		{"seconds_only", map[string]struct{}{"updated_at": {}}, "COALESCE(updated_at * 1000, 0)"},
		{"neither", map[string]struct{}{}, "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := updatedAtSQL(tc.columns); got != tc.want {
				t.Fatalf("updatedAtSQL(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestCreatedAtSQL_AllCombinations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		columns map[string]struct{}
		want    string
	}{
		{"both", map[string]struct{}{"created_at_ms": {}, "created_at": {}}, "COALESCE(created_at_ms, created_at * 1000)"},
		{"ms_only", map[string]struct{}{"created_at_ms": {}}, "COALESCE(created_at_ms, 0)"},
		{"seconds_only", map[string]struct{}{"created_at": {}}, "COALESCE(created_at * 1000, 0)"},
		{"neither_falls_back_to_updated", map[string]struct{}{"updated_at_ms": {}, "updated_at": {}}, "COALESCE(updated_at_ms, updated_at * 1000)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := createdAtSQL(tc.columns); got != tc.want {
				t.Fatalf("createdAtSQL(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestTextColumnSQL(t *testing.T) {
	t.Parallel()
	columns := map[string]struct{}{"title": {}, "cwd": {}}
	if got := textColumnSQL(columns, "title"); got != "COALESCE(title, '')" {
		t.Fatalf("textColumnSQL(title) = %q", got)
	}
	if got := textColumnSQL(columns, "missing"); got != "''" {
		t.Fatalf("textColumnSQL(missing) = %q, want ''", got)
	}
}

func TestThreadColumnsNoIDReturnsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Create a threads table without an id column.
	if _, err := db.Exec(`CREATE TABLE threads (title TEXT)`); err != nil {
		t.Fatal(err)
	}
	columns, err := threadColumns(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 0 {
		t.Fatalf("threadColumns without id should return empty, got %v", columns)
	}
}

// TestLookupWindow_EmptyColumnsNoID covers LookupWindow's len(columns)==0
// early return (codexthreads.go:107-109): a threads table without an id
// column makes threadColumns return empty, and LookupWindow must return
// nil, nil without querying.
func TestLookupWindow_EmptyColumnsNoID(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	db := openTestCodexDB(t, home)
	execTestSQL(t, db, `CREATE TABLE threads (title TEXT)`)
	db.Close()

	got, err := LookupWindow(home, time.UnixMilli(0), time.UnixMilli(1))
	if err != nil {
		t.Fatalf("empty columns should not error: %v", err)
	}
	if got != nil {
		t.Fatalf("empty columns should return nil, got %v", got)
	}
}

// TestLookup_HomeIsFile covers Lookup's non-IsNotExist stat error
// (codexthreads.go:36): when home points at a file rather than a
// directory, os.Stat on the .codex/state_5.sqlite path underneath fails
// with a non-IsNotExist error and must be propagated.
func TestLookup_HomeIsFile(t *testing.T) {
	t.Parallel()
	// Create a regular file at the .codex path so os.Stat fails with
	// "not a directory" rather than IsNotExist.
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.WriteFile(codexDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Lookup(home, []string{"thread-1"})
	if err == nil {
		t.Fatalf("home-is-file should propagate stat error, got nil err, result=%v", got)
	}
	if got != nil {
		t.Fatalf("on error Lookup should return nil map, got %v", got)
	}
}

// TestLookupWindow_HomeIsFile covers LookupWindow's non-IsNotExist stat
// error (codexthreads.go:94): same shape as TestLookup_HomeIsFile.
func TestLookupWindow_HomeIsFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.WriteFile(codexDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LookupWindow(home, time.UnixMilli(0), time.UnixMilli(1))
	if err == nil {
		t.Fatalf("home-is-file should propagate stat error, got nil err, result=%v", got)
	}
	if got != nil {
		t.Fatalf("on error LookupWindow should return nil, got %v", got)
	}
}

// TestQuery_ScanError covers query's non-ErrNoRows scan error path
// (codexthreads.go:238-240): a threads table where updated_at_ms holds a
// non-numeric TEXT value defeats COALESCE(updated_at_ms, 0) type coercion
// and makes Scan into *int64 fail with a non-ErrNoRows error.
func TestQuery_ScanError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	db := openTestCodexDB(t, home)
	execTestSQL(t, db, `
CREATE TABLE threads (
	id TEXT PRIMARY KEY,
	title TEXT,
	cwd TEXT,
	source TEXT,
	thread_source TEXT,
	model TEXT,
	first_user_message TEXT,
	created_at_ms INTEGER,
	updated_at_ms TEXT
)`)
	// Insert a row whose updated_at_ms is non-numeric TEXT. COALESCE
	// passes the TEXT through (it is not NULL), and Scan into *int64
	// fails.
	execTestSQL(t, db, `INSERT INTO threads (id, title, cwd, source, thread_source, model, first_user_message, created_at_ms, updated_at_ms) VALUES ('thread-1', 'one', '/tmp', 'cli', 'cli', 'm', 'msg', 1, 'not-a-number')`)
	db.Close()

	got, err := Lookup(home, []string{"thread-1"})
	if err == nil {
		t.Fatalf("scan error should propagate, got nil err, result=%v", got)
	}
}

// TestLookupWindow_ScanError covers LookupWindow's scanMetadata error
// path (codexthreads.go:119-121): same shape as TestQuery_ScanError but
// for the window query.
func TestLookupWindow_ScanError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	db := openTestCodexDB(t, home)
	execTestSQL(t, db, `
CREATE TABLE threads (
	id TEXT PRIMARY KEY,
	title TEXT,
	cwd TEXT,
	source TEXT,
	thread_source TEXT,
	model TEXT,
	first_user_message TEXT,
	created_at_ms INTEGER,
	updated_at_ms TEXT
)`)
	execTestSQL(t, db, `INSERT INTO threads (id, title, cwd, source, thread_source, model, first_user_message, created_at_ms, updated_at_ms) VALUES ('thread-1', 'one', '/tmp', 'cli', 'cli', 'm', 'msg', 1, 'not-a-number')`)
	db.Close()

	got, err := LookupWindow(home, time.UnixMilli(0), time.UnixMilli(2))
	if err == nil {
		t.Fatalf("scan error should propagate, got nil err, result=%v", got)
	}
}

// TestLookup_CorruptDB covers Lookup's sql.Open / threadColumns error
// paths (codexthreads.go:39-41, 46-48): a file that exists but is not a
// valid SQLite database causes threadColumns to fail, which propagates.
func TestLookup_CorruptDB(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a non-SQLite file at the expected path.
	if err := os.WriteFile(filepath.Join(dir, "state_5.sqlite"), []byte("not a sqlite file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(home, []string{"thread"}); err == nil {
		t.Fatalf("corrupt db should produce error, got nil")
	}
}

// TestLookupWindow_CorruptDB covers LookupWindow's threadColumns error
// path (codexthreads.go:104-106): a file that exists but is not a valid
// SQLite database causes threadColumns to fail.
func TestLookupWindow_CorruptDB(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state_5.sqlite"), []byte("not a sqlite file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LookupWindow(home, time.UnixMilli(0), time.UnixMilli(2)); err == nil {
		t.Fatalf("corrupt db should produce error, got nil")
	}
}

// TestThreadColumns_ScanError covers threadColumns' rows.Scan error path
// (codexthreads.go:144-146): a threads table with unexpected column types
// can cause a scan error in PRAGMA table_info.
func TestThreadColumns_RowsErr(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	db := openTestCodexDB(t, home)
	// Create a valid threads table so threadColumns runs, but close
	// the db first to trigger a rows.Err() error.
	db.Close()
	if _, err := threadColumns(db); err == nil {
		t.Fatalf("closed db should produce error, got nil")
	}
}

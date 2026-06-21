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

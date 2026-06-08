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
	updated_at INTEGER NOT NULL,
	updated_at_ms INTEGER
)`)
	execTestSQL(t, db, `
INSERT INTO threads (id, title, cwd, source, thread_source, model, first_user_message, updated_at, updated_at_ms)
VALUES ('thread-1', 'Check status', '/tmp/project', 'cli', 'cli', 'gpt-5.5', 'first prompt', 1760000000, 1760000000123)`)
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
	updated_at_ms INTEGER
)`)
	execTestSQL(t, db, `
INSERT INTO threads (id, title, cwd, source, thread_source, model, first_user_message, updated_at_ms)
VALUES
	('old-thread', 'Old', '/tmp/old', 'cli', 'user', 'gpt-5.5', 'old prompt', 1759999999000),
	('thread-1', 'One', '/tmp/one', 'cli', 'user', 'gpt-5.5', 'first prompt', 1760000000100),
	('thread-2', 'Two', '/tmp/two', 'vscode', '', 'gpt-5.5', 'second prompt', 1760000000200)`)
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

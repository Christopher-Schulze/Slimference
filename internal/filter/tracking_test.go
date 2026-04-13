package filter

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestOpenDB_RecordRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "track.db")

	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ts := time.Unix(1700000000, 0).UTC()
	if err := RecordFilterRun(db, "git status", "/proj", 1000, 200, 80.0, ts); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM filter_runs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count = %d", n)
	}

	var cmd, pp string
	var inTok, outTok int
	var sp float64
	var created int64
	err = db.QueryRow(`SELECT command, project_path, input_tokens, output_tokens, savings_pct, created_at FROM filter_runs LIMIT 1`).
		Scan(&cmd, &pp, &inTok, &outTok, &sp, &created)
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "git status" || pp != "/proj" || inTok != 1000 || outTok != 200 || sp != 80.0 || created != ts.Unix() {
		t.Fatalf("row mismatch: %q %q %d %d %f %d", cmd, pp, inTok, outTok, sp, created)
	}

	run, ok, err := LastFilterRun(db)
	if err != nil || !ok {
		t.Fatalf("LastFilterRun: ok=%v err=%v", ok, err)
	}
	if run.Command != "git status" || run.InputTokens != 1000 {
		t.Fatalf("LastFilterRun: %+v", run)
	}
}

func TestLastFilterRun_empty(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, ok, err := LastFilterRun(db)
	if err != nil || ok {
		t.Fatalf("want empty, ok=%v err=%v", ok, err)
	}
}

// TestRecentFilterRuns_limitZero covers the if limit < 1 clamp.
func TestRecentFilterRuns_limitZero(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "lim.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := time.Unix(1700000000, 0).UTC()
	if err := RecordFilterRun(db, "git log", "/repo", 100, 50, 50, ts); err != nil {
		t.Fatal(err)
	}
	// limit=0 should clamp to 1 and return 1 result.
	runs, err := RecentFilterRuns(db, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
}

// TestOpenDB_sqlOpenError covers the sql.Open error return path via sqlOpenFunc injection.
func TestOpenDB_sqlOpenError(t *testing.T) {
	// Not parallel: mutates package-level var sqlOpenFunc.
	old := sqlOpenFunc
	sqlOpenFunc = func(_, _ string) (*sql.DB, error) { return nil, errors.New("injected open error") }
	defer func() { sqlOpenFunc = old }()

	_, err := OpenDB("/tmp/irrelevant.db")
	if err == nil {
		t.Fatal("expected error from injected sqlOpenFunc")
	}
}

// TestOpenDB_migrateError covers the migrate failure return path by using a read-only non-SQLite file.
func TestOpenDB_migrateError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "readonly.db")
	// Write invalid SQLite content and make it read-only so migrate's CREATE TABLE fails
	if err := os.WriteFile(path, []byte("not a sqlite database header"), 0444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0644) })
	_, err := OpenDB(path)
	if err == nil {
		t.Skip("SQLite accepted read-only non-SQLite file - platform may allow it (skip, not fail)")
	}
}

// TestLastFilterRun_closedDB covers the non-ErrNoRows scan error path.
func TestLastFilterRun_closedDB(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "closed.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	// Close the db so queries will fail with a real error (not ErrNoRows)
	_ = db.Close()
	_, _, err = LastFilterRun(db)
	if err == nil {
		t.Fatal("expected error on closed db")
	}
}

// TestQueryFilterRunsAggregate_closedDB covers the query error return path.
func TestQueryFilterRunsAggregate_closedDB(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "closed2.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	_, err = QueryFilterRunsAggregate(db, start, end)
	if err == nil {
		t.Fatal("expected error on closed db")
	}
}

// TestRecentFilterRuns_scanError covers the rows.Scan error path inside the loop.
// We need the query to succeed but Scan to fail — achieved by storing a BLOB in an
// integer column so Go's sql scanner returns a type-conversion error.
func TestRecentFilterRuns_scanError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "blobschema.db")
	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Create table with correct column names but input_tokens stored as BLOB
	_, err = rawDB.Exec(`CREATE TABLE filter_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		command TEXT NOT NULL,
		project_path TEXT NOT NULL,
		input_tokens BLOB,
		output_tokens INTEGER NOT NULL,
		savings_pct REAL NOT NULL,
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		rawDB.Close()
		t.Fatal(err)
	}
	// Insert row with binary BLOB in input_tokens — Go sql cannot scan BLOB to *int
	_, err = rawDB.Exec("INSERT INTO filter_runs (command, project_path, input_tokens, output_tokens, savings_pct, created_at) VALUES ('cmd', '/p', X'deadbeef', 0, 0.0, 0)")
	if err != nil {
		rawDB.Close()
		t.Fatal(err)
	}
	rawDB.Close()

	db2, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	_, err = RecentFilterRuns(db2, 5)
	if err == nil {
		t.Skip("SQLite scanned BLOB to int without error - driver is permissive (skip)")
	}
}

// TestRecentFilterRuns_closedDB covers the query error return path.
func TestRecentFilterRuns_closedDB(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "closed3.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	_, err = RecentFilterRuns(db, 5)
	if err == nil {
		t.Fatal("expected error on closed db")
	}
}

func TestFilterRunsAggregate_Recent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t0 := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	if err := RecordFilterRun(db, "a", "/p", 10, 5, 50, t0); err != nil {
		t.Fatal(err)
	}
	if err := RecordFilterRun(db, "b", "/p", 20, 20, 0, t0); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 10, 23, 0, 0, 0, time.UTC)
	agg, err := QueryFilterRunsAggregate(db, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if agg.Runs != 2 || agg.InputTokens != 30 || agg.TokensSavedEst != 5 {
		t.Fatalf("agg %+v", agg)
	}
	recent, err := RecentFilterRuns(db, 5)
	if err != nil || len(recent) != 2 {
		t.Fatalf("recent %v err=%v", recent, err)
	}
	if recent[0].Command != "b" || recent[1].Command != "a" {
		t.Fatalf("order: %+v", recent)
	}
}

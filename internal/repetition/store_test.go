package repetition

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fileMode = os.FileMode

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "rep.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestDefaultPath(t *testing.T) {
	if got := DefaultPath("/tmp/home"); got != "/tmp/home/.slimference/repetition.db" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRecord_FirstHitInsert(t *testing.T) {
	db := openTestDB(t)
	count, firstMsg, err := Record(db, Key{SessionID: "s", ToolName: "Bash", Command: "git status", Output: "ok"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || firstMsg != 7 {
		t.Fatalf("first hit: count=%d firstMsg=%d", count, firstMsg)
	}
}

func TestRecord_RepeatedHitsBump(t *testing.T) {
	db := openTestDB(t)
	for i := 1; i <= 3; i++ {
		count, first, err := Record(db, Key{SessionID: "s", ToolName: "Bash", Command: "git status", Output: "same"}, i)
		if err != nil {
			t.Fatal(err)
		}
		if count != i {
			t.Fatalf("hit %d: count=%d", i, count)
		}
		if first != 1 {
			t.Fatalf("hit %d: firstMsg drifted: %d", i, first)
		}
	}
}

func TestRecord_DifferentOutputsAreDistinct(t *testing.T) {
	db := openTestDB(t)
	if c, _, err := Record(db, Key{SessionID: "s", ToolName: "Bash", Command: "ls", Output: "a"}, 1); err != nil || c != 1 {
		t.Fatalf("a: count=%d err=%v", c, err)
	}
	if c, _, err := Record(db, Key{SessionID: "s", ToolName: "Bash", Command: "ls", Output: "b"}, 2); err != nil || c != 1 {
		t.Fatalf("b: count=%d err=%v", c, err)
	}
}

func TestRecord_EmptyKeyNoop(t *testing.T) {
	db := openTestDB(t)
	if c, _, err := Record(db, Key{SessionID: "", ToolName: "Bash", Output: "x"}, 1); err != nil || c != 0 {
		t.Fatalf("empty session: %d %v", c, err)
	}
	if c, _, err := Record(db, Key{SessionID: "s", ToolName: "", Command: "", Output: "x"}, 1); err != nil || c != 0 {
		t.Fatalf("empty tool key: %d %v", c, err)
	}
}

func TestForget(t *testing.T) {
	db := openTestDB(t)
	_, _, _ = Record(db, Key{SessionID: "s1", ToolName: "Bash", Output: "a"}, 1)
	_, _, _ = Record(db, Key{SessionID: "s2", ToolName: "Bash", Output: "a"}, 1)
	if err := Forget(db, "s1"); err != nil {
		t.Fatal(err)
	}
	stats, _ := Snapshot(db)
	if stats.UniqueSessions != 1 {
		t.Fatalf("forget did not isolate: %+v", stats)
	}
	if err := Forget(db, ""); err != nil {
		t.Fatalf("empty forget must noop: %v", err)
	}
}

func TestSnapshot(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 4; i++ {
		_, _, _ = Record(db, Key{SessionID: "s", ToolName: "Bash", Output: "same"}, 1)
	}
	stats, err := Snapshot(db)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rows != 1 || stats.MaxHitCount != 4 {
		t.Fatalf("stats: %+v", stats)
	}
}

func TestMarker(t *testing.T) {
	got := Marker("Bash", 7, 3)
	if !strings.Contains(got, "msg #7") || !strings.Contains(got, "Bash") {
		t.Fatalf("marker: %q", got)
	}
	if !strings.Contains(Marker("", 1, 2), "tool") {
		t.Fatal("empty tool name must default to 'tool'")
	}
}

func TestRecord_InsertExecErrorOnConstraintViolation(t *testing.T) {
	db := openTestDB(t)
	// Manually break the schema so INSERT fails the next call: drop and
	// recreate without the required columns.
	if _, err := db.Exec(`DROP TABLE posttool_repetitions`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE posttool_repetitions (session_id TEXT NOT NULL, tool_key TEXT NOT NULL, output_sha TEXT NOT NULL, hit_count INTEGER, first_seen INTEGER, last_seen INTEGER, first_msg INTEGER, extra_required INTEGER NOT NULL, PRIMARY KEY (session_id, tool_key, output_sha))`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Record(db, Key{SessionID: "s", ToolName: "x", Output: "y"}, 1); err == nil {
		t.Fatal("expected INSERT error from missing required column")
	}
}

func TestRecord_UpdateExecErrorOnConstraintViolation(t *testing.T) {
	db := openTestDB(t)
	// Seed a row.
	if _, _, err := Record(db, Key{SessionID: "s", ToolName: "x", Output: "y"}, 1); err != nil {
		t.Fatal(err)
	}
	// Recreate the table with a CHECK constraint that the next UPDATE
	// will violate. Have to recreate the row too so the SELECT in
	// Record returns hit_count=1 first.
	if _, err := db.Exec(`DROP TABLE posttool_repetitions`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE posttool_repetitions (session_id TEXT NOT NULL, tool_key TEXT NOT NULL, output_sha TEXT NOT NULL, hit_count INTEGER NOT NULL CHECK (hit_count < 2), first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL, first_msg INTEGER NOT NULL, PRIMARY KEY (session_id, tool_key, output_sha))`); err != nil {
		t.Fatal(err)
	}
	// Reseed via raw SQL so the row exists; Record's UPDATE will then
	// try to bump hit_count to 2 and the CHECK fires.
	if _, _, err := Record(db, Key{SessionID: "s", ToolName: "x", Output: "y"}, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Record(db, Key{SessionID: "s", ToolName: "x", Output: "y"}, 2); err == nil {
		t.Fatal("expected UPDATE error from CHECK violation")
	}
}

// readOnlyTxBlocker drops a foreign DB into the test that fails the
// first Exec after Begin succeeds. Used to drive Record's UPDATE/INSERT
// error paths without relying on platform-specific filesystem flags.
func TestRecord_ScanErrorPath(t *testing.T) {
	// Drop the table after Open so the SELECT throws an error on a
	// non-NoRows path. This exercises the `scanErr != sql.ErrNoRows`
	// branch.
	db := openTestDB(t)
	if _, err := db.Exec("DROP TABLE posttool_repetitions"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Record(db, Key{SessionID: "s", ToolName: "x", Output: "y"}, 1); err == nil {
		t.Fatal("expected error when underlying table is missing")
	}
}

func TestRecord_QueryErrorOnClosedDB(t *testing.T) {
	db := openTestDB(t)
	_ = db.Close()
	if _, _, err := Record(db, Key{SessionID: "s", ToolName: "x", Output: "y"}, 1); err == nil {
		t.Fatal("expected error on closed db query")
	}
}

func TestSnapshot_ErrorOnClosedDB(t *testing.T) {
	db := openTestDB(t)
	_ = db.Close()
	if _, err := Snapshot(db); err == nil {
		t.Fatal("expected error from closed db")
	}
}

func TestOpen_DirCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep", "nested")
	db, err := Open(filepath.Join(dir, "rep.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
}

func TestOpen_MkdirError(t *testing.T) {
	prev := mkdirAllFunc
	mkdirAllFunc = func(string, fileMode) error { return errors.New("mkdir fail") }
	t.Cleanup(func() { mkdirAllFunc = prev })
	if _, err := Open(filepath.Join(t.TempDir(), "x.db")); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestOpen_OpenError(t *testing.T) {
	prev := sqlOpenFunc
	sqlOpenFunc = func(driver, dsn string) (*sql.DB, error) { return nil, errors.New("fail") }
	t.Cleanup(func() { sqlOpenFunc = prev })
	if _, err := Open(filepath.Join(t.TempDir(), "x.db")); err == nil {
		t.Fatal("expected open error")
	}
}

func TestOpen_MigrateError(t *testing.T) {
	prev := migrateFunc
	migrateFunc = func(*sql.DB) error { return errors.New("migrate fail") }
	t.Cleanup(func() { migrateFunc = prev })
	if _, err := Open(filepath.Join(t.TempDir(), "x.db")); err == nil {
		t.Fatal("expected migrate error")
	}
}

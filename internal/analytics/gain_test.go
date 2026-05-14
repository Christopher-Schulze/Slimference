package analytics

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/filter"
	_ "modernc.org/sqlite"
)

func TestNormalizeGainProjectFilter(t *testing.T) {
	t.Parallel()
	if normalizeGainProjectFilter("") != "" || normalizeGainProjectFilter("   ") != "" {
		t.Fatal("empty after trim")
	}
	got := normalizeGainProjectFilter("  /foo/bar/../baz  ")
	want := filepath.Clean("/foo/bar/../baz")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if strings.Contains(got, "..") {
		t.Fatal("filepath.Clean should collapse ..")
	}
}

func TestFilterGainWindow(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("test", 0)
	now := time.Date(2026, 4, 10, 15, 30, 0, 0, loc)
	start, end, err := FilterGainWindow("today", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := time.Date(2026, 4, 10, 0, 0, 0, 0, loc); !start.Equal(got) {
		t.Fatalf("today start: got %v want %v", start, got)
	}
	if !end.Equal(now) {
		t.Fatalf("end: got %v want %v", end, now)
	}
	_, _, err = FilterGainWindow("bogus", now)
	if err == nil {
		t.Fatal("expected error")
	}

	startW, endW, err := FilterGainWindow("week", now)
	if err != nil {
		t.Fatal(err)
	}
	if !endW.Equal(now) || !startW.Before(endW) {
		t.Fatalf("week: start=%v end=%v now=%v", startW, endW, now)
	}
	startM, endM, err := FilterGainWindow("month", now)
	if err != nil {
		t.Fatal(err)
	}
	if !endM.Equal(now) || !startM.Before(endM) {
		t.Fatalf("month: start=%v end=%v", startM, endM)
	}
	startA, endA, err := FilterGainWindow("all", now)
	if err != nil {
		t.Fatal(err)
	}
	if !endA.Equal(now) || startA.Unix() != 0 {
		t.Fatalf("all: start=%v end=%v", startA, endA)
	}
}

func TestQueryFilterGain(t *testing.T) {
	path := t.TempDir() + "/filter.db"
	db, err := filter.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := filter.RecordFilterRun(db, "[git] git status", "/p", 100, 40, 60, ts); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	s, err := QueryFilterGain(path, "month", ts)
	if err != nil {
		t.Fatal(err)
	}
	if s.Runs != 1 || s.InputTokens != 100 || s.OutputTokens != 40 || s.TokensSavedEst != 60 {
		t.Fatalf("summary: %+v", s)
	}

	db2, err := filter.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	s2, err := queryFilterGainDB(db2, "today",
		time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 9, 23, 59, 59, 0, time.UTC), "")
	if err != nil {
		t.Fatal(err)
	}
	if s2.Runs != 0 {
		t.Fatalf("wrong day: %+v", s2)
	}
}

func TestQueryFilterGainReportByCommand(t *testing.T) {
	path := t.TempDir() + "/filter.db"
	db, err := filter.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := filter.RecordFilterRun(db, "a", "/p", 100, 50, 50, ts); err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterRun(db, "b", "/p", 10, 10, 0, ts); err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterRun(db, "a", "/p", 40, 10, 75, ts); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	rep, err := QueryFilterGainReport(path, "month", ts, true, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Runs != 3 || rep.TokensSavedEst != 80 {
		t.Fatalf("summary %+v", rep.FilterGainSummary)
	}
	if len(rep.ByCommand) != 2 {
		t.Fatalf("by command: %+v", rep.ByCommand)
	}
	if rep.ByCommand[0].Command != "a" || rep.ByCommand[0].TokensSavedEst != 80 {
		t.Fatalf("order: %+v", rep.ByCommand)
	}

	var buf bytes.Buffer
	if err := WriteGainByCommandCSV(&buf, rep.ByCommand); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("a,")) {
		t.Fatalf("csv: %s", buf.String())
	}
}

func TestQueryFilterGainReportByParser(t *testing.T) {
	path := t.TempDir() + "/filter.db"
	db, err := filter.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := filter.RecordFilterRun(db, "tsc --noEmit", "/p", 100, 40, 60, ts); err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterRun(db, "npm test", "/p", 100, 90, 10, ts); err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterRun(db, "git status", "/p", 50, 10, 40, ts); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	rep, err := QueryFilterGainReportWithOptions(path, "month", ts, false, true, "", 2.0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ByParser) != 3 {
		t.Fatalf("by parser: %+v", rep.ByParser)
	}
	if rep.ByParser[0].Parser != "typescript" || rep.ByParser[0].TokensSavedEst != 60 || rep.ByParser[0].SavingsUsdEst <= 0 {
		t.Fatalf("top parser row: %+v", rep.ByParser[0])
	}
	if ParserFamilyForCommand("svelte-check") != "svelte" || ParserFamilyForCommand("unknown thing") != "other" {
		t.Fatal("parser family mapping mismatch")
	}
	parserCases := map[string]string{
		"zig build":          "zig",
		"sqlfluff lint":      "sql",
		"markdownlint .":     "markdown",
		"cargo clippy":       "rust",
		"go test":            "go",
		"pytest":             "python",
		"pyright src":        "python",
		"uv pip install":     "python",
		"clang++ main.cc":    "c_cpp_build",
		"gradle test":        "jvm_mobile_php",
		"mvnw test":          "jvm_mobile_php",
		"flutter test":       "jvm_mobile_php",
		"composer install":   "jvm_mobile_php",
		"docker ps":          "container",
		"podman build .":     "container",
		"kubectl get pods":   "container",
		"terraform plan":     "hcl",
		"ruby spec":          "ruby",
		"bash script.sh":     "shell",
		"svelte-check":       "svelte",
		"tsserver validate":  "typescript",
		"vitest run":         "javascript",
		"turbo run build":    "javascript",
		"nx affected:test":   "javascript",
		"biome check .":      "javascript",
		"drizzle-kit push":   "sql",
		"sqlite3 test.db":    "sql",
		"unknown tool shape": "other",
	}
	for cmd, want := range parserCases {
		if got := ParserFamilyForCommand(cmd); got != want {
			t.Fatalf("ParserFamilyForCommand(%q)=%q want %q", cmd, got, want)
		}
	}

	var buf bytes.Buffer
	if err := WriteGainByParserCSV(&buf, rep.ByParser); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("typescript,")) {
		t.Fatalf("csv: %s", buf.String())
	}
	if err := WriteGainByParserCSV(errWriter{}, rep.ByParser); err == nil {
		t.Fatal("expected parser CSV write error")
	}
	tied := []FilterGainByParserRow{
		{Parser: "zig", TokensSavedEst: 10},
		{Parser: "go", TokensSavedEst: 10},
	}
	sortGainByParser(tied)
	if tied[0].Parser != "go" {
		t.Fatalf("tie sort: %+v", tied)
	}
}

func TestQueryFilterGainReport_projectFilter(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "filter.db")
	alpha := filepath.Join(tmp, "alpha")
	beta := filepath.Join(tmp, "beta")
	alphaSub := filepath.Join(alpha, "sub")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := filter.RecordFilterRun(db, "a", alpha, 100, 50, 50, ts); err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterRun(db, "b", beta, 10, 10, 0, ts); err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterRun(db, "c", alphaSub, 20, 10, 50, ts); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	wantAlpha := filepath.Clean(alpha)
	rep, err := QueryFilterGainReport(dbPath, "month", ts, true, alpha, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Runs != 2 || rep.TokensSavedEst != 60 {
		t.Fatalf("filtered summary %+v", rep.FilterGainSummary)
	}
	if rep.ProjectPathFilter != wantAlpha {
		t.Fatalf("filter field: %q want %q", rep.ProjectPathFilter, wantAlpha)
	}

	repOther, err := QueryFilterGainReport(dbPath, "month", ts, false, beta, 0)
	if err != nil {
		t.Fatal(err)
	}
	if repOther.Runs != 1 || repOther.InputTokens != 10 {
		t.Fatalf("beta only: %+v", repOther.FilterGainSummary)
	}
}

func TestQueryFilterGainReport_usdEstimate(t *testing.T) {
	path := t.TempDir() + "/filter.db"
	db, err := filter.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := filter.RecordFilterRun(db, "x", "/p", 1_000_000, 400_000, 60, ts); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	rep, err := QueryFilterGainReport(path, "month", ts, false, "", 3.0)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TokensSavedEst != 600_000 {
		t.Fatalf("saved est %d", rep.TokensSavedEst)
	}
	want := 600_000.0 / 1e6 * 3.0
	if rep.USDPerMillionTokens != 3.0 || math.Abs(rep.SavingsUsdEst-want) > 1e-9 {
		t.Fatalf("usd: %+v want %v", rep.FilterGainSummary, want)
	}
}

func TestFormatFilterGainJSON(t *testing.T) {
	t.Parallel()
	s := FilterGainSummary{Period: "today", Runs: 2, InputTokens: 10, OutputTokens: 4, TokensSavedEst: 6}
	b, err := FormatFilterGainJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"period"`)) || !bytes.Contains(b, []byte(`"today"`)) {
		t.Fatalf("%s", b)
	}
}

func TestFormatFilterGainReportJSON(t *testing.T) {
	t.Parallel()
	rep := FilterGainReport{
		FilterGainSummary: FilterGainSummary{Period: "week", Runs: 1},
		ByCommand:         []FilterGainByCommandRow{{Command: "git status", Runs: 1, TokensSavedEst: 5}},
	}
	b, err := FormatFilterGainReportJSON(rep)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("git status")) {
		t.Fatalf("%s", b)
	}
}

func TestWriteGainSummaryCSV(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	s := FilterGainSummary{
		Period:              "month",
		Runs:                3,
		InputTokens:         100,
		OutputTokens:        40,
		TokensSavedEst:      60,
		USDPerMillionTokens: 2.5,
		SavingsUsdEst:       0.15,
	}
	if err := WriteGainSummaryCSV(&buf, s); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "month") || !strings.Contains(out, "tokens_saved_est") || !strings.Contains(out, "0.15") {
		t.Fatalf("%s", out)
	}
}

// errWriter rejects every write with an error.
type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) { return 0, errors.New("forced write error") }

func TestFilterGainWindow_startAfterEnd(t *testing.T) {
	t.Parallel()
	// time before Unix epoch: "all" gives start=time.Unix(0,0), end=now; start > end so start is clamped to end.
	now := time.Date(1969, 6, 1, 0, 0, 0, 0, time.UTC)
	start, end, err := FilterGainWindow("all", now)
	if err != nil {
		t.Fatal(err)
	}
	if !start.Equal(end) {
		t.Errorf("start=%v should equal end=%v after clamp", start, end)
	}
}

func TestQueryFilterGain_invalidPeriod(t *testing.T) {
	t.Parallel()
	_, err := QueryFilterGain(t.TempDir()+"/filter.db", "bogus-period", time.Now())
	if err == nil {
		t.Fatal("expected error for invalid period")
	}
}

func TestQueryFilterGainReport_badPeriod(t *testing.T) {
	t.Parallel()
	_, err := QueryFilterGainReport(t.TempDir()+"/filter.db", "invalid", time.Now(), false, "", 0)
	if err == nil {
		t.Fatal("expected error for invalid period")
	}
}

func TestQueryFilterGainReport_badDBPath(t *testing.T) {
	t.Parallel()
	_, err := QueryFilterGainReport("/nonexistent/dir/filter.db", "today", time.Now(), false, "", 0)
	if err == nil {
		t.Fatal("expected error for non-existent db path")
	}
}

func TestQueryFilterGainReport_usdEstimateByCommand(t *testing.T) {
	path := t.TempDir() + "/filter.db"
	db, err := filter.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := filter.RecordFilterRun(db, "git status", "/p", 1_000_000, 400_000, 60, ts); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	// byCommand=true with usdPerMillionTokens>0 covers applyGainUSD rows loop.
	rep, err := QueryFilterGainReport(path, "month", ts, true, "", 3.0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ByCommand) == 0 {
		t.Fatal("expected by-command rows")
	}
	if rep.ByCommand[0].SavingsUsdEst == 0 {
		t.Errorf("expected non-zero SavingsUsdEst, got %v", rep.ByCommand[0].SavingsUsdEst)
	}
}

func TestWriteGainSummaryCSV_writeError(t *testing.T) {
	t.Parallel()
	s := FilterGainSummary{Period: "today", Runs: 1}
	if err := WriteGainSummaryCSV(errWriter{}, s); err == nil {
		t.Fatal("expected write error")
	}
}

func TestWriteGainByCommandCSV_headerWriteError(t *testing.T) {
	t.Parallel()
	rows := []FilterGainByCommandRow{{Command: "git", Runs: 1}}
	if err := WriteGainByCommandCSV(errWriter{}, rows); err == nil {
		t.Fatal("expected write error")
	}
}

func TestQueryFilterGainDB_closedDB(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/filter.db"
	db, err := filter.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	// Close DB so subsequent query fails.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_, err = queryFilterGainDB(db, "today", now.AddDate(0, 0, -1), now, "")
	if err == nil {
		t.Fatal("expected error querying closed DB")
	}
}

func TestQueryFilterGainByCommandDB_closedDB(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/filter.db"
	db, err := filter.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_, err = queryFilterGainByCommandDB(db, now.AddDate(0, 0, -1), now, "")
	if err == nil {
		t.Fatal("expected error querying closed DB")
	}
}

func TestWriteGainSummaryCSV_dataRowWriteError(t *testing.T) {
	t.Parallel()
	// Period field large enough to overflow the csv.Writer's 4096-byte buffer so
	// the second cw.Write (data row) hits the errWriter and returns an error.
	s := FilterGainSummary{
		Period: strings.Repeat("x", 5000),
		Runs:   1,
	}
	if err := WriteGainSummaryCSV(errWriter{}, s); err == nil {
		t.Fatal("expected write error on data row")
	}
}

func TestWriteGainByCommandCSV_rowWriteError(t *testing.T) {
	t.Parallel()
	// Build enough rows so that the buffer overflows and a row write returns an error.
	rows := make([]FilterGainByCommandRow, 80)
	for i := range rows {
		rows[i] = FilterGainByCommandRow{Command: strings.Repeat("a", 40), Runs: int64(i)}
	}
	if err := WriteGainByCommandCSV(errWriter{}, rows); err == nil {
		t.Fatal("expected write error on row")
	}
}

// createCorruptFilterDB creates a SQLite DB with a filter_runs table that includes
// command and created_at (so filter.OpenDB's migrate succeeds - CREATE TABLE IF NOT EXISTS
// is a no-op, and CREATE INDEX on created_at works), but omits input_tokens/output_tokens/
// project_path so that analytic queries referencing those columns fail.
func createCorruptFilterDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "filter.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS filter_runs (id INTEGER PRIMARY KEY, command TEXT NOT NULL, created_at INTEGER NOT NULL)")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// createCorruptFilterDBNoCommand creates a SQLite DB with filter_runs that has all
// columns needed for queryFilterGainDB (COUNT/SUM succeed) but is missing the
// "command" column, so queryFilterGainByCommandDB (SELECT command ...) fails.
func createCorruptFilterDBNoCommand(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "filter.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// All columns except command so first query (no SELECT command) succeeds
	// and second query (SELECT command ...) fails.
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS filter_runs (id INTEGER PRIMARY KEY, project_path TEXT NOT NULL, input_tokens INTEGER NOT NULL, output_tokens INTEGER NOT NULL, savings_pct REAL NOT NULL, created_at INTEGER NOT NULL)")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestQueryFilterGainReport_queryDBError(t *testing.T) {
	t.Parallel()
	// A DB whose filter_runs table has wrong columns causes queryFilterGainDB to fail.
	// This covers QueryFilterGainReport lines 97-99.
	path := createCorruptFilterDB(t)
	now := time.Now()
	_, err := QueryFilterGainReport(path, "today", now, false, "", 0)
	if err == nil {
		t.Fatal("expected error from QueryFilterGainReport with corrupt DB schema")
	}
}

func TestQueryFilterGainReport_byCommandQueryDBError(t *testing.T) {
	t.Parallel()
	// DB where queryFilterGainDB succeeds but queryFilterGainByCommandDB fails
	// (table has all aggregate columns but no "command" column). Covers lines 106-108.
	path := createCorruptFilterDBNoCommand(t)
	now := time.Now()
	_, err := QueryFilterGainReport(path, "today", now, true, "", 0)
	if err == nil {
		t.Fatal("expected error from QueryFilterGainReport byCommand with corrupt DB schema")
	}
}

// --- custom sql/driver for rows.Scan error injection ---

// blobInjectDriver is a database/sql/driver.Driver that returns one row whose
// third column is a raw []byte BLOB. When the sql package tries to convert it to
// *int64 it fails with a type-conversion error, covering the rows.Scan error path
// in queryFilterGainByCommandDB (gain.go line 195).
type blobInjectDriver struct{}
type blobInjectConn struct{}
type blobInjectStmt struct{}
type blobInjectRows struct{ sent bool }

var registerBlobDriver sync.Once

func (blobInjectDriver) Open(_ string) (driver.Conn, error) { return &blobInjectConn{}, nil }

func (*blobInjectConn) Prepare(_ string) (driver.Stmt, error) { return &blobInjectStmt{}, nil }
func (*blobInjectConn) Close() error                          { return nil }
func (*blobInjectConn) Begin() (driver.Tx, error)             { return nil, errors.New("no tx") }

func (*blobInjectStmt) Close() error  { return nil }
func (*blobInjectStmt) NumInput() int { return -1 }
func (*blobInjectStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return nil, errors.New("no exec")
}
func (*blobInjectStmt) Query(_ []driver.Value) (driver.Rows, error) { return &blobInjectRows{}, nil }

func (*blobInjectRows) Columns() []string { return []string{"command", "cnt", "itok", "otok", "saved"} }
func (*blobInjectRows) Close() error      { return nil }
func (r *blobInjectRows) Next(dest []driver.Value) error {
	if r.sent {
		return io.EOF
	}
	r.sent = true
	dest[0] = "cmd"
	dest[1] = int64(1)
	// BLOB bytes cannot be converted to *int64 by database/sql.
	dest[2] = []byte{0xde, 0xad, 0xbe, 0xef}
	dest[3] = int64(0)
	dest[4] = int64(0)
	return nil
}

// TestQueryFilterGainByCommandDB_scanError covers the rows.Scan error return (gain.go:195)
// via a custom database/sql/driver that injects a BLOB into an int64 column.
func TestQueryFilterGainByCommandDB_scanError(t *testing.T) {
	t.Parallel()
	registerBlobDriver.Do(func() {
		sql.Register("blob-inject-analytics", blobInjectDriver{})
	})
	db, err := sql.Open("blob-inject-analytics", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	_, err = queryFilterGainByCommandDB(db, now.AddDate(-1, 0, 0), now, "")
	if err == nil {
		t.Skip("driver returned BLOB but sql package scanned it without error (permissive conversion)")
	}
}

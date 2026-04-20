package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/filter"
)

func TestParseGainArgs(t *testing.T) {
	p, f, err := parseGainArgs(nil)
	if err != nil || p != "today" || f.json || f.byCommand || f.csv {
		t.Fatalf("default: period=%q flags=%+v err=%v", p, f, err)
	}
	p, f, err = parseGainArgs([]string{"month", "--json", "--by-command"})
	if err != nil || p != "month" || !f.json || !f.byCommand || f.csv {
		t.Fatalf("month: period=%q flags=%+v err=%v", p, f, err)
	}
	p, f, err = parseGainArgs([]string{"--json", "week"})
	if err != nil || p != "week" || !f.json {
		t.Fatalf("reordered: period=%q flags=%+v err=%v", p, f, err)
	}
	_, _, err = parseGainArgs([]string{"week", "month"})
	if err == nil {
		t.Fatal("expected error for extra arg")
	}
	_, _, err = parseGainArgs([]string{"--nope"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	p, f, err = parseGainArgs([]string{"all", "--csv", "--by-command"})
	if err != nil || p != "all" || !f.csv || !f.byCommand {
		t.Fatalf("csv: period=%q flags=%+v err=%v", p, f, err)
	}
	p, _, err = parseGainArgs([]string{"", "week"})
	if err != nil || p != "week" {
		t.Fatalf("empty token skip: period=%q err=%v", p, err)
	}
}

func TestHandleSubcommand_gain_today_andJSON(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "make all")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Layer 0 filter gain") || !strings.Contains(out, "Filter runs:") {
		t.Fatalf("stdout: %q", out)
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"gain", "week", "--json"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	out = buf.String()
	if !strings.Contains(out, `"runs"`) || !strings.Contains(out, `"period"`) {
		t.Fatalf("json stdout: %q", out)
	}
}

func TestHandleSubcommand_gain_csv_and_byCommand(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "npm test")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today", "--csv"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "period") || !strings.Contains(out, "runs") {
		t.Fatalf("csv stdout: %q", out)
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"gain", "today", "--by-command"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	out = buf.String()
	if !strings.Contains(out, "By command") || !strings.Contains(out, "npm test") {
		t.Fatalf("by-command stdout: %q", out)
	}
}

func TestHandleSubcommand_gain_csvByCommand(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "[git] git status", "[npm] npm test")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today", "--csv", "--by-command"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "command") || !strings.Contains(out, "git status") || !strings.Contains(out, "npm test") {
		t.Fatalf("csv by-command stdout: %q", out)
	}
}

func TestHandleSubcommand_gain_withProjectFilter(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(dir, "myapp")
	ts := time.Now()
	if err := filter.RecordFilterRun(db, "[git] git status", proj, 100, 40, 60, ts); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today", "--project", proj})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "project") || !strings.Contains(out, "Filter runs:") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_gain_withUSD(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "make")
	cfgPath := filepath.Join(t.TempDir(), "gain-usd.toml")
	content := `[proxy]
listen_address = "127.0.0.1"
listen_port = 8990

[compression]
sliding_window = 4

[analytics]
gain_usd_per_million_tokens = 2.5
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Est. value saved") || !strings.Contains(out, "$2.50") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_gain_noFilterDBFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist", "filter.db")
	t.Setenv("SLIMFERENCE_FILTER_DB", missing)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "No Layer-0 filter runs recorded yet") || !strings.Contains(out, "no filter.db") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_gain_emptyRunsInWindow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "No Layer-0 filter runs in this window") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_gain_periodAll(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "make test")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "all"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Layer 0 filter gain (all)") || !strings.Contains(out, "Filter runs:") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_gain_byCommand_withUSD(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "make", "npm test")
	cfgPath := filepath.Join(t.TempDir(), "gain-usd.toml")
	content := `[proxy]
listen_address = "127.0.0.1"
listen_port = 8990

[compression]
sliding_window = 4

[analytics]
gain_usd_per_million_tokens = 2.5
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today", "--by-command"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "By command") || !strings.Contains(out, "(~$") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestParseGainArgs_project(t *testing.T) {
	period, f, err := parseGainArgs([]string{"--project", "/proj", "week"})
	if err != nil || period != "week" || f.project != "/proj" {
		t.Fatalf("period=%q project=%q err=%v", period, f.project, err)
	}
	period, f, err = parseGainArgs([]string{"month", "--project", "/other"})
	if err != nil || period != "month" || f.project != "/other" {
		t.Fatalf("period before --project: period=%q project=%q err=%v", period, f.project, err)
	}
	_, _, err = parseGainArgs([]string{"--project"})
	if err == nil {
		t.Fatal("want error for missing project path")
	}
}

func TestParseGainArgs_jsonAlias(t *testing.T) {
	_, f, err := parseGainArgs([]string{"-json", "month"})
	if err != nil || !f.json {
		t.Fatalf("err=%v json=%v", err, f.json)
	}
}

func TestParseGainArgs_byCommandDefaultPeriod(t *testing.T) {
	p, f, err := parseGainArgs([]string{"--by-command"})
	if err != nil || p != "today" || !f.byCommand {
		t.Fatalf("period=%q byCommand=%v err=%v", p, f.byCommand, err)
	}
}

func TestParseGainArgs_csvDefaultPeriod(t *testing.T) {
	p, f, err := parseGainArgs([]string{"--csv"})
	if err != nil || p != "today" || !f.csv || f.byCommand {
		t.Fatalf("period=%q csv=%v byCommand=%v err=%v", p, f.csv, f.byCommand, err)
	}
}

func TestHandleSubcommand_gainBadPeriodExits1(t *testing.T) {
	if os.Getenv("TP_SUB_GAIN_BAD") == "1" {
		handleSubcommand([]string{"gain", "tomorrow"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_gainBadPeriodExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_GAIN_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleGainCmd_parseErrorExits1 covers handleGainCmd parseGainArgs error path (main.go:747-750).
func TestHandleGainCmd_parseErrorExits1(t *testing.T) {
	if os.Getenv("TP_GAIN_PARSE_ERR") == "1" {
		handleGainCmd([]string{"--bad-flag"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleGainCmd_parseErrorExits1")
	cmd.Env = append(os.Environ(), "TP_GAIN_PARSE_ERR=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleGainCmd_badPeriodExits1 covers handleGainCmd invalid period path (main.go:753-756).
func TestHandleGainCmd_badPeriodExits1(t *testing.T) {
	if os.Getenv("TP_GAIN_BAD_PERIOD") == "1" {
		handleGainCmd([]string{"yesterday"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleGainCmd_badPeriodExits1")
	cmd.Env = append(os.Environ(), "TP_GAIN_BAD_PERIOD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleGainCmd_statOtherErrorExits1 covers handleGainCmd os.Stat non-IsNotExist error (main.go:767-768).
// Points SLIMFERENCE_FILTER_DB at a file inside a mode-000 directory so os.Stat fails with
// a permission error (not IsNotExist).
func TestHandleGainCmd_statOtherErrorExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on windows")
	}
	if os.Getenv("TP_GAIN_STAT_ERR") == "1" {
		t.Setenv("SLIMFERENCE_FILTER_DB", os.Getenv("TP_GAIN_DB_PATH"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleGainCmd([]string{"today"})
		return
	}
	tmp := t.TempDir()
	blocked := filepath.Join(tmp, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(blocked, "filter.db")
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleGainCmd_statOtherErrorExits1")
	cmd.Env = append(os.Environ(), "TP_GAIN_STAT_ERR=1", "TP_GAIN_DB_PATH="+dbPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleGainCmd_queryErrorExits1 covers handleGainCmd analytics.QueryFilterGainReport error
// (main.go:775-778) by pointing to a corrupt (non-SQLite) database file.
func TestHandleGainCmd_queryErrorExits1(t *testing.T) {
	if os.Getenv("TP_GAIN_QUERY_ERR") == "1" {
		t.Setenv("SLIMFERENCE_FILTER_DB", os.Getenv("TP_GAIN_CORRUPT_DB"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleGainCmd([]string{"today"})
		return
	}
	tmp := t.TempDir()
	corruptDB := filepath.Join(tmp, "corrupt.db")
	if err := os.WriteFile(corruptDB, []byte("not-a-sqlite-database"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleGainCmd_queryErrorExits1")
	cmd.Env = append(os.Environ(), "TP_GAIN_QUERY_ERR=1", "TP_GAIN_CORRUPT_DB="+corruptDB)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleGainCmd_resolvePathError covers the resolveFilterDBPathFn error exit in handleGainCmd.
func TestHandleGainCmd_resolvePathError(t *testing.T) {
	orig := resolveFilterDBPathFn
	defer func() { resolveFilterDBPathFn = orig }()
	resolveFilterDBPathFn = func() (string, error) { return "", errors.New("path error") }
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"gain", "today"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "filter db path") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleGainCmd_writeGainByCommandCSVError covers the WriteGainByCommandCSV error exit.
func TestHandleGainCmd_writeGainByCommandCSVError(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "git status")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	orig := writeGainByCommandCSV
	defer func() { writeGainByCommandCSV = orig }()
	writeGainByCommandCSV = func(w io.Writer, rows []analytics.FilterGainByCommandRow) error {
		return errors.New("csv write failed")
	}

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"gain", "today", "--csv", "--by-command"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "gain") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleGainCmd_writeGainSummaryCSVError covers the WriteGainSummaryCSV error exit.
func TestHandleGainCmd_writeGainSummaryCSVError(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "git status")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	orig := writeGainSummaryCSV
	defer func() { writeGainSummaryCSV = orig }()
	writeGainSummaryCSV = func(w io.Writer, s analytics.FilterGainSummary) error {
		return errors.New("csv write failed")
	}

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"gain", "today", "--csv"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "gain") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

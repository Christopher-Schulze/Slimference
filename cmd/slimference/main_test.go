package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/daemon"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/types"
)

func TestParseDebugPeriodArgs(t *testing.T) {
	p, j, err := parseDebugPeriodArgs([]string{"month", "--json"})
	if err != nil || p != "month" || !j {
		t.Fatalf("got period=%q json=%v err=%v", p, j, err)
	}
	_, _, err = parseDebugPeriodArgs([]string{"--bad"})
	if err == nil {
		t.Fatal("expected bad flag")
	}
	_, _, err = parseDebugPeriodArgs([]string{"today", "week"})
	if err == nil {
		t.Fatal("expected error for two period args")
	}
	// Empty string arg is skipped - covers the `if a == "" { continue }` branch.
	p, _, err = parseDebugPeriodArgs([]string{"", "week"})
	if err != nil || p != "week" {
		t.Fatalf("empty arg skip: got period=%q err=%v", p, err)
	}
	// No args → defaults to "today".
	p, _, err = parseDebugPeriodArgs(nil)
	if err != nil || p != "today" {
		t.Fatalf("default period: got %q err=%v", p, err)
	}
}

// TestHandleDebugTail_limitClamped covers the limit>500 clamp (main.go:967-969).
// Point SLIMFERENCE_FILTER_DB to a non-existent file so mustOpenFilterDB returns (nil, false).
func TestHandleDebugTail_limitClamped(t *testing.T) {
	t.Setenv("SLIMFERENCE_FILTER_DB", filepath.Join(t.TempDir(), "nonexistent.db"))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "tail", "600"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "No filter.db") {
		t.Fatalf("expected no-db message, got: %q", out)
	}
}

// TestHandleDebugTail_emptyStringArg covers the `if a == "" { continue }` branch (main.go:947-948).
func TestHandleDebugTail_emptyStringArg(t *testing.T) {
	t.Setenv("SLIMFERENCE_FILTER_DB", filepath.Join(t.TempDir(), "nonexistent.db"))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "tail", ""})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "No filter.db") {
		t.Fatalf("expected no-db message, got: %q", out)
	}
}

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

func TestFormatTokensPlain64(t *testing.T) {
	if formatTokensPlain64(500) != "500" {
		t.Fatalf("got %q", formatTokensPlain64(500))
	}
	if formatTokensPlain64(1500) != "1.5K" {
		t.Fatalf("got %q", formatTokensPlain64(1500))
	}
	if formatTokensPlain64(2_200_000) != "2.2M" {
		t.Fatalf("got %q", formatTokensPlain64(2_200_000))
	}
	if formatTokensPlain64(-3) != "0" {
		t.Fatalf("got %q", formatTokensPlain64(-3))
	}
}

func TestLayer0PermissionCheck(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIRM_SUDO", "")
	if code, msg := layer0PermissionCheck("echo ok"); code != 0 || msg != "" {
		t.Fatalf("allowed: got code=%d msg=%q", code, msg)
	}
	if code, msg := layer0PermissionCheck("rm -rf /"); code != 2 || msg == "" {
		t.Fatalf("deny: want code 2 and message, got %d %q", code, msg)
	}
	if code, msg := layer0PermissionCheck("sudo apt update"); code != 3 || msg == "" {
		t.Fatalf("ask: want code 3, got %d %q", code, msg)
	}
	t.Setenv("SLIMFERENCE_CONFIRM_SUDO", "1")
	if code, msg := layer0PermissionCheck("sudo apt update"); code != 0 || msg != "" {
		t.Fatalf("sudo allowed with SLIMFERENCE_CONFIRM_SUDO=1: got %d %q", code, msg)
	}
}

// TestIsServerClosed verifies the server closed error detection.
func TestIsServerClosed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"server closed string", errors.New("http: Server closed"), true},
		{"wrapped server closed", fmtErrorfWrapped(http.ErrServerClosed), true},
		{"other error", errors.New("connection refused"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isServerClosed(tt.err); got != tt.want {
				t.Errorf("isServerClosed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func fmtErrorfWrapped(err error) error {
	return errors.New("wrap: " + err.Error())
}

func TestHandleSubcommand_Version(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"version"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "slimference v") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestConfigAdapter_GetPrefillSpeed(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Usage.EstimatedPrefillSpeed = 777
	ca := &configAdapter{cfg: cfg}
	if ca.GetPrefillSpeed() != 777 {
		t.Fatalf("GetPrefillSpeed() = %d", ca.GetPrefillSpeed())
	}
}

func TestHandleSubcommand_debugLast(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Unix(1700000000, 0).UTC()
	if err := filter.RecordFilterRun(db, "echo hi", "/proj", 10, 5, 50.0, ts); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "last"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Last Layer-0 filter run") || !strings.Contains(out, "echo hi") {
		t.Fatalf("stdout: %q", out)
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"debug", "last", "--json"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	out = buf.String()
	if !strings.Contains(out, `"command"`) || !strings.Contains(out, "echo hi") {
		t.Fatalf("json stdout: %q", out)
	}
}

func TestHandleSubcommand_debugLast_noFilterDBFile(t *testing.T) {
	t.Setenv("SLIMFERENCE_FILTER_DB", filepath.Join(t.TempDir(), "missing-filter.db"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "last"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "No filter.db") {
		t.Fatalf("stdout: %q", out)
	}
}

func testOpenFilterDBAndRecord(t *testing.T, commands ...string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	for _, cmd := range commands {
		if err := filter.RecordFilterRun(db, cmd, "/proj", 100, 40, 60, ts); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func TestHandleSubcommand_debugSummary_today(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "git status")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "summary", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Layer-0 filter_runs summary") || !strings.Contains(out, "runs:") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_debugSummary_json(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "ls -la")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "summary", "week", "--json"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, `"runs"`) || !strings.Contains(out, `"period"`) {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_debugTail_empty(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "tail"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "No rows in filter_runs") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_debugTail_rowsAndJSON(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "one", "two")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	old := os.Stdout

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "tail", "5"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Layer-0 filter_runs") || !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Fatalf("stdout: %q", out)
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"debug", "tail", "2", "--json"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	out = buf.String()
	if !strings.Contains(out, `"command"`) || !strings.Contains(out, "two") {
		t.Fatalf("json stdout: %q", out)
	}
}

func TestHandleSubcommand_debugReplay_file(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := "{\"x\":1}\n\n  \nhello\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "replay", path})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Session replay (preview)") || !strings.Contains(out, "non-empty lines: 2") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_debugSummary_invalidPeriodExits1(t *testing.T) {
	if os.Getenv("TP_DEBUG_SUM_BAD") == "1" {
		handleSubcommand([]string{"debug", "summary", "nope"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugSummary_invalidPeriodExits1")
	cmd.Env = append(os.Environ(), "TP_DEBUG_SUM_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugTail_unknownFlagExits1(t *testing.T) {
	if os.Getenv("TP_DEBUG_TAIL_BAD") == "1" {
		handleSubcommand([]string{"debug", "tail", "--nope"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugTail_unknownFlagExits1")
	cmd.Env = append(os.Environ(), "TP_DEBUG_TAIL_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugReplay_usageExits1(t *testing.T) {
	if os.Getenv("TP_DEBUG_REPLAY_USAGE") == "1" {
		handleSubcommand([]string{"debug", "replay"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugReplay_usageExits1")
	cmd.Env = append(os.Environ(), "TP_DEBUG_REPLAY_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugPaths(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "paths"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference debug paths") {
		t.Fatalf("stdout: %q", out)
	}
	if !strings.Contains(out, "filter.db:") || !strings.Contains(out, "tee directory:") {
		t.Fatalf("expected path lines in stdout: %q", out)
	}
}

// TestHandleSubcommand_debugPaths_configFileBranches covers the config-file branches
// for filter_db (1085-1087), tee_dir (1096-1098), and decisions_log (1108-1110).
// These fire when the values come from config file rather than env vars.
func TestHandleSubcommand_debugPaths_configFileBranches(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "tp.toml")
	if err := os.WriteFile(cfgPath, []byte(`[filter]
filter_db = "/cfg/filter.db"
tee_dir = "/cfg/tee"
[debug]
decisions_log = "/cfg/decisions.jsonl"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	// Unset env vars so config file values take effect.
	t.Setenv("SLIMFERENCE_FILTER_DB", "")
	t.Setenv("SLIMFERENCE_TEE_DIR", "")
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "paths"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "[filter] filter_db") {
		t.Fatalf("expected '[filter] filter_db' note, got: %q", out)
	}
	if !strings.Contains(out, "[filter] tee_dir") {
		t.Fatalf("expected '[filter] tee_dir' note, got: %q", out)
	}
	if !strings.Contains(out, "[debug] decisions_log") {
		t.Fatalf("expected '[debug] decisions_log' note, got: %q", out)
	}
}

func TestHandleSubcommand_debugPaths_envOverrides(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "tp.toml")
	if err := os.WriteFile(cfgPath, []byte(`[filter]
filter_db = "/custom/filter.db"
tee_dir = "/custom/tee"
[debug]
decisions_log = "~/decisions.jsonl"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	t.Setenv("SLIMFERENCE_FILTER_DB", "/env/override-filter.db")
	t.Setenv("SLIMFERENCE_TEE_DIR", "/env/override-tee")
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "/env/decisions.jsonl")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "paths"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "[SLIMFERENCE_CONFIG]") || !strings.Contains(out, "[SLIMFERENCE_FILTER_DB]") {
		t.Fatalf("expected env notes in output: %q", out)
	}
	if !strings.Contains(out, "[SLIMFERENCE_TEE_DIR]") || !strings.Contains(out, "[SLIMFERENCE_DEBUG_DECISIONS_LOG]") {
		t.Fatalf("expected tee/decisions env notes: %q", out)
	}
}

func TestHandleSubcommand_doctor_smoke(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing-doctor.toml"))
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", srv.URL)
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("stdout: %q", out)
	}
	if !strings.Contains(out, "All checks passed") {
		t.Fatalf("expected success footer: %q", out)
	}
}

func TestHandleSubcommand_doctor_invalidConfigExits1(t *testing.T) {
	if os.Getenv("TP_DOCTOR_BAD_CFG") == "1" {
		handleSubcommand([]string{"doctor"})
		return
	}
	cfgPath := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(cfgPath, []byte("this is not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_doctor_invalidConfigExits1")
	cmd.Env = append(os.Environ(), "TP_DOCTOR_BAD_CFG=1", "SLIMFERENCE_CONFIG="+cfgPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleSubcommand_doctor_failingChecks covers the check() closure !ok branch
// (main.go:592-595), the MiniMax key-missing branch (615-617), the upstream-unreachable
// branches (624-626, 634-636), and the "Some checks failed" footer (652-654).
func TestHandleSubcommand_doctor_failingChecks(t *testing.T) {
	// Use a bad upstream URL (connection refused immediately) + no MiniMax key.
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	// Explicitly unset MINIMAX_API_KEY so the key check fails.
	t.Setenv("MINIMAX_API_KEY", "")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("expected doctor header in output: %q", out)
	}
	// At least one FAIL line (MiniMax or upstream).
	if !strings.Contains(out, "FAIL") {
		t.Fatalf("expected at least one FAIL in output: %q", out)
	}
}

// TestHandleSubcommand_doctor_configFileMissingBranch covers the
// "not found at ... (using defaults)" branch in the Config file check (main.go:604-606).
// We override HOME so DefaultConfigPath returns a non-existent file.
func TestHandleSubcommand_doctor_configFileMissingBranch(t *testing.T) {
	// Point HOME at a temp dir so ~/.slimference/config.toml does not exist.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	// Also set SLIMFERENCE_CONFIG to point to a missing file so config.Load gets defaults.
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(fakeHome, "cfg.toml"))

	// Use fast-failing upstreams.
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("expected doctor header: %q", out)
	}
}

// TestHandleSubcommand_doctor_configFileExistsBranch covers main.go:607 (return path, true)
// when DefaultConfigPath() resolves to an existing file.
//
// DefaultConfigPath calls expandHome("~") which returns the literal string "~" (because "~"
// has no "~/" prefix), so the effective path is the relative path "~/.slimference/config.toml".
// We build that directory structure inside a temp dir and chdir into it.
func TestHandleSubcommand_doctor_configFileExistsBranch(t *testing.T) {
	tmp := t.TempDir()

	// Recreate the relative structure: <tmp>/~/.slimference/config.toml
	tildeSlimferenceDir := filepath.Join(tmp, "~", ".slimference")
	if err := os.MkdirAll(tildeSlimferenceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tildeSlimferenceDir, "config.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// chdir so that the relative path "~/.slimference/config.toml" resolves to our file.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	// Provide a missing SLIMFERENCE_CONFIG so config.Load uses defaults (empty is fine).
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("expected doctor header: %q", out)
	}
	// The config-file-exists branch returns the resolved path - not the "not found" string.
	if strings.Contains(out, "not found") {
		t.Fatalf("expected config-found output (got not-found): %q", out)
	}
}

// TestHandleSubcommand_doctor_analyticsLogDirError covers main.go:643-645
// when os.MkdirAll for the analytics log dir fails.
func TestHandleSubcommand_doctor_analyticsLogDirError(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(fakeHome, "missing.toml"))

	// Create a regular file, then set log dir to a path inside it so MkdirAll fails.
	blocker := filepath.Join(fakeHome, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	logDirPath := filepath.Join(blocker, "subdir")

	// Write a config that sets analytics.log_dir to the blocked path.
	cfgContent := "[analytics]\nlog_dir = \"" + logDirPath + "\"\n"
	cfgFile := filepath.Join(fakeHome, "test.toml")
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgFile)
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("expected doctor header: %q", out)
	}
	// The analytics MkdirAll error should appear in FAIL output.
	if !strings.Contains(out, "cannot create") {
		t.Fatalf("expected MkdirAll error in output: %q", out)
	}
}

func TestResolveFilterDBPath_TeeDir_env(t *testing.T) {
	t.Setenv("SLIMFERENCE_FILTER_DB", "/tmp/slimference-filter-unit.db")
	p, err := resolveFilterDBPath()
	if err != nil || p != "/tmp/slimference-filter-unit.db" {
		t.Fatalf("filter db: err=%v p=%q", err, p)
	}
	t.Setenv("SLIMFERENCE_TEE_DIR", "/tmp/slimference-tee-unit")
	d, err := resolveTeeDir()
	if err != nil || d != "/tmp/slimference-tee-unit" {
		t.Fatalf("tee: err=%v d=%q", err, d)
	}
}

func TestResolveFilterDBPath_TeeDir_fromConfigFile(t *testing.T) {
	tmp := t.TempDir()
	filterPath := filepath.Join(tmp, "my-filter.db")
	teePath := filepath.Join(tmp, "my-tee")
	cfgPath := filepath.Join(tmp, "config.toml")
	content := fmt.Sprintf(`[filter]
filter_db = %q
tee_dir = %q
`, filterPath, teePath)
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	t.Setenv("SLIMFERENCE_FILTER_DB", "")
	t.Setenv("SLIMFERENCE_TEE_DIR", "")
	p, err := resolveFilterDBPath()
	if err != nil {
		t.Fatalf("resolveFilterDBPath: %v", err)
	}
	if p != filepath.Clean(filterPath) {
		t.Fatalf("filter db: got %q want %q", p, filepath.Clean(filterPath))
	}
	d, err := resolveTeeDir()
	if err != nil {
		t.Fatalf("resolveTeeDir: %v", err)
	}
	if d != filepath.Clean(teePath) {
		t.Fatalf("tee dir: got %q want %q", d, filepath.Clean(teePath))
	}
}

func writeTestAnalyticsConfigToml(t *testing.T, logDir string) string {
	t.Helper()
	absLog, err := filepath.Abs(logDir)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "tp-analytics.toml")
	content := fmt.Sprintf("[analytics]\nlog_dir = %q\n", absLog)
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestHandleSubcommand_configShow(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"config", "show"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, `"Proxy"`) || !strings.Contains(out, `"ListenPort"`) {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_configShow_loadErrorExits1(t *testing.T) {
	if os.Getenv("TP_CFG_SHOW_BAD") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", os.Getenv("TP_CFG_SHOW_BAD_FILE"))
		handleSubcommand([]string{"config", "show"})
		return
	}
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badPath, []byte("this is not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configShow_loadErrorExits1")
	cmd.Env = append(os.Environ(), "TP_CFG_SHOW_BAD=1", "TP_CFG_SHOW_BAD_FILE="+badPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_configInit_writesFile(t *testing.T) {
	// DefaultConfigPath uses filepath.Join("~", ".slimference", "config.toml"), i.e. a
	// literal "~" segment — the file is created relative to the process working directory.
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"config", "init"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	path := filepath.Join(tmp, "~", ".slimference", "config.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config at %s: %v", path, err)
	}
	if !strings.Contains(out, "Config written") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_configInit_secondIsNoop(t *testing.T) {
	if os.Getenv("TP_CFG_INIT_TWICE") == "1" {
		_ = os.Chdir(os.Getenv("TP_CFG_INIT_HOME"))
		handleSubcommand([]string{"config", "init"})
		handleSubcommand([]string{"config", "init"})
		return
	}
	tmp := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configInit_secondIsNoop")
	cmd.Env = append(os.Environ(), "TP_CFG_INIT_TWICE=1", "TP_CFG_INIT_HOME="+tmp)
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("want exit 0: %v out=%s", err, out)
	}
	if !strings.Contains(string(out), "already exists") {
		t.Fatalf("output: %s", out)
	}
}

func TestHandleSubcommand_stats_today_withSnapshot(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))

	p, err := analytics.NewPersister(logDir)
	if err != nil {
		t.Fatal(err)
	}
	snap := analytics.AnalyticsSnapshot{
		SessionStart:     time.Now().UTC(),
		TotalRequests:    2,
		TotalInputTokens: 100,
		CacheHits:        1,
	}
	if err := p.WriteSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	p.Close()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"stats", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Stats") || !strings.Contains(out, "Messages sent:") {
		t.Fatalf("stdout: %q", out)
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

func TestHandleSubcommand_stats_week_and_month(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))

	p, err := analytics.NewPersister(logDir)
	if err != nil {
		t.Fatal(err)
	}
	snap := analytics.AnalyticsSnapshot{
		SessionStart:     time.Now().UTC(),
		TotalRequests:    1,
		TotalInputTokens: 42,
	}
	if err := p.WriteSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	p.Close()

	for _, period := range []string{"week", "month"} {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleSubcommand([]string{"stats", period})
		_ = w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, "Slimference Stats") || !strings.Contains(out, "Messages sent:") {
			t.Fatalf("stats %s: %q", period, out)
		}
	}
}

func TestHandleSubcommand_stats_emptyLogDir_messages(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"stats", "today"}, "No stats for today yet."},
		{[]string{"stats", "week"}, "No stats for this week."},
		{[]string{"stats", "month"}, "No stats for this month."},
	}
	for _, tc := range cases {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleSubcommand(tc.args)
		_ = w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if !strings.Contains(buf.String(), tc.want) {
			t.Fatalf("%v: want %q, got %q", tc.args, tc.want, buf.String())
		}
	}
}

func TestHandleSubcommand_debugSummary_noFilterDB(t *testing.T) {
	t.Setenv("SLIMFERENCE_FILTER_DB", filepath.Join(t.TempDir(), "missing-filter.db"))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "summary", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "No filter.db") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_debugSummary_all_plain(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "stat .")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "summary", "all"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Layer-0 filter_runs summary (all)") || !strings.Contains(out, "runs:") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_stats_configLoadErrorExits1(t *testing.T) {
	if os.Getenv("TP_STATS_BAD_CFG") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", os.Getenv("TP_STATS_BAD_CFG_FILE"))
		handleSubcommand([]string{"stats", "today"})
		return
	}
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badPath, []byte("this is not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_stats_configLoadErrorExits1")
	cmd.Env = append(os.Environ(), "TP_STATS_BAD_CFG=1", "TP_STATS_BAD_CFG_FILE="+badPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugSummary_parseArgsErrorExits1(t *testing.T) {
	if os.Getenv("TP_DEBUG_SUM_PARSE") == "1" {
		handleSubcommand([]string{"debug", "summary", "today", "week"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugSummary_parseArgsErrorExits1")
	cmd.Env = append(os.Environ(), "TP_DEBUG_SUM_PARSE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
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

func TestHandleSubcommand_filter_echo_recordsRun(t *testing.T) {
	if os.Getenv("TP_FILTER_ECHO") == "1" {
		t.Setenv("SLIMFERENCE_FILTER_DB", os.Getenv("TP_FILTER_DB"))
		handleSubcommand([]string{"filter", "--", "echo", "hello"})
		return
	}
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "filter.db")
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_filter_echo_recordsRun")
	cmd.Env = append(os.Environ(), "TP_FILTER_ECHO=1", "TP_FILTER_DB="+dbPath)
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("err=%v out=%s", err, out)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM filter_runs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("filter_runs rows: %d", n)
	}
}

func TestProxyAdapter_GetLayer2Status_layer2Cleared(t *testing.T) {
	cfg := config.Defaults()
	p := proxy.New(cfg)
	p.ClearLayer2ForTesting()
	a := newProxyAdapter(p)
	st := a.GetLayer2Status()
	if st.HasCache || st.Compressing || st.QueueDepth != 0 || !st.LastRun.IsZero() {
		t.Fatalf("got %+v", st)
	}
}

// TestHandleSubcommand_filter_nonZeroExit_teeRecovery covers tee recovery when the child exits non-zero.
func TestHandleSubcommand_filter_nonZeroExit_teeRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	if os.Getenv("TP_FILTER_TEE_FAIL") == "1" {
		t.Setenv("SLIMFERENCE_TEE_DIR", os.Getenv("TP_FILTER_TEE_DIR"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"filter", "--", "sh", "-c", "exit 7"})
		return
	}
	teeDir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_filter_nonZeroExit_teeRecovery")
	cmd.Env = append(os.Environ(),
		"TP_FILTER_TEE_FAIL=1",
		"TP_FILTER_TEE_DIR="+teeDir,
	)
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("want exit 7, got err=%v out=%s", err, out)
	}
	if !strings.Contains(string(out), "saved raw output") {
		t.Fatalf("expected tee message: %q", out)
	}
}

func TestTestUpstream_ok(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	old := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw
	testUpstream("Anthropic", srv.URL)
	testUpstream("OpenAI", srv.URL)
	_ = pw.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, pr)
	out := buf.String()
	if strings.Count(out, "OK - HTTP 200") != 2 {
		t.Fatalf("stdout: %q", out)
	}
}

func TestTestMiniMax_ok(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("MINIMAX_API_KEY", "secret-key")
	cfg := config.Defaults()
	cfg.Compression.MiniMax.BaseURL = srv.URL + "/v1"

	old := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw
	testMiniMax(cfg)
	_ = pw.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, pr)
	if !strings.Contains(buf.String(), "OK - HTTP 200") {
		t.Fatalf("stdout: %q", buf.String())
	}
}

func TestTestMiniMax_noAPIKeyExits1(t *testing.T) {
	if os.Getenv("TP_MINIMAX_NOKEY") == "1" {
		t.Setenv("MINIMAX_API_KEY", "")
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		testMiniMax(cfg)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestTestMiniMax_noAPIKeyExits1")
	cmd.Env = append(os.Environ(), "TP_MINIMAX_NOKEY=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleTestCmd_upstreamAndMinimax(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	for _, sub := range []string{"anthropic", "openai"} {
		old := os.Stdout
		pr, pw, _ := os.Pipe()
		os.Stdout = pw
		handleTestCmd([]string{sub})
		_ = pw.Close()
		os.Stdout = old
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, pr)
		if !strings.Contains(buf.String(), "OK - HTTP 200") {
			t.Fatalf("%s: %q", sub, buf.String())
		}
	}

	t.Setenv("MINIMAX_API_KEY", "k")
	cfgPath := filepath.Join(t.TempDir(), "minimax-cmd.toml")
	content := fmt.Sprintf(`[proxy]
listen_address = "127.0.0.1"
listen_port = 8990

[compression]
sliding_window = 4

[compression.minimax]
base_url = %q
api_key_env = "MINIMAX_API_KEY"
`, srv.URL+"/v1")
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)

	old := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw
	handleTestCmd([]string{"minimax"})
	_ = pw.Close()
	os.Stdout = old
	var buf2 bytes.Buffer
	_, _ = io.Copy(&buf2, pr)
	if !strings.Contains(buf2.String(), "OK - HTTP 200") {
		t.Fatalf("minimax: %q", buf2.String())
	}
}

func TestSetupLogging_jsonFileAndTextStderr(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	t.Cleanup(func() { slog.SetDefault(discard) })

	logPath := filepath.Join(t.TempDir(), "tp.log")
	cfg := config.Defaults()
	cfg.Logging.Level = "warn"
	cfg.Logging.Format = "json"
	cfg.Logging.File = logPath
	setupLogging(cfg)
	slog.Warn("setup-log-test", "k", "v")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("setup-log-test")) {
		t.Fatalf("log file: %s", data)
	}

	cfg2 := config.Defaults()
	cfg2.Logging.Level = "debug"
	cfg2.Logging.Format = "text"
	cfg2.Logging.File = ""
	setupLogging(cfg2)
}

// TestTestIntercept_claude exercises testIntercept in-process so coverage is attributed
// (subprocess-based tests do not contribute to -cover profiles).
func TestTestIntercept_claude(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		testIntercept(cfg, "claude")
		close(done)
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	ok := false
	for range 100 {
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", "slimference-test-intercept")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ok = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !ok {
		t.Fatal("intercept server did not respond with 200")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("testIntercept did not finish")
	}
}

func TestTestIntercept_codex(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		testIntercept(cfg, "codex")
		close(done)
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	ok := false
	for range 100 {
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", "slimference-test-intercept")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "sk-test")
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ok = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !ok {
		t.Fatal("intercept server did not respond with 200")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("testIntercept did not finish")
	}
}

func TestHandleSubcommand_rewrite_stdinHookJSON(t *testing.T) {
	// Use a filterable command (git) so RewriteCommand applies a prefix and exits 0.
	// This tests: JSON extraction from hook stdin + filter dispatch + exit 0 path.
	if os.Getenv("TP_REWRITE_STDIN") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_stdinHookJSON")
	cmd.Env = append(os.Environ(), "TP_REWRITE_STDIN=1")
	cmd.Stdin = strings.NewReader(`{"command":"git status"}`)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("err=%v stderr may be lost", err)
	}
	if strings.TrimSpace(string(out)) != "slimference filter git status" {
		t.Fatalf("got %q", out)
	}
}

// TestHandleSubcommand_rewrite_stdinHookJSON_NoFilter covers the exit-1 path when
// the command read from hook JSON has no matching filter (spec+.md §4.2 exit code 1 = passthrough).
func TestHandleSubcommand_rewrite_stdinHookJSON_NoFilter(t *testing.T) {
	if os.Getenv("TP_REWRITE_NOFIL") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_stdinHookJSON_NoFilter")
	cmd.Env = append(os.Environ(), "TP_REWRITE_NOFIL=1")
	cmd.Stdin = strings.NewReader(`{"command":"echo rewrite-ok"}`)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 (no filter match), got err=%v", err)
	}
}

func TestHandleSubcommand_rewrite_stdinNoCommandExits1(t *testing.T) {
	if os.Getenv("TP_REWRITE_NO_CMD") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_stdinNoCommandExits1")
	cmd.Env = append(os.Environ(), "TP_REWRITE_NO_CMD=1")
	cmd.Stdin = strings.NewReader(`{}`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), `"command"`) {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestHandleSubcommand_rewrite_stdinInvalidJSONExits1(t *testing.T) {
	if os.Getenv("TP_REWRITE_BAD_JSON") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_stdinInvalidJSONExits1")
	cmd.Env = append(os.Environ(), "TP_REWRITE_BAD_JSON=1")
	cmd.Stdin = strings.NewReader(`not-json`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "JSON") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

// TestHandleSubcommand_rewrite_usageTTYExits1 exercises handleRewriteCmd when there are no
// args and stdin is a TTY (usage on stderr, exit 1). Uses /dev/tty when available.
func TestHandleSubcommand_rewrite_usageTTYExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/tty")
	}
	if os.Getenv("TP_REWRITE_TTY_USAGE") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		t.Skipf("open /dev/tty: %v", err)
	}
	defer tty.Close()
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_usageTTYExits1")
	cmd.Env = append(os.Environ(), "TP_REWRITE_TTY_USAGE=1")
	cmd.Stdin = tty
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage: slimference rewrite") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestHandleSubcommand_hook_status(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "status"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if out == "" {
		t.Fatal("expected hook status lines")
	}
}

func TestHandleSubcommand_hook_installRemove_claude_and_codex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "install", "claude"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Installed Claude") {
		t.Fatalf("install claude: %q", buf.String())
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"hook", "install", "codex"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	if !strings.Contains(buf.String(), "Installed Codex hooks") {
		t.Fatalf("install codex: %q", buf.String())
	}

	r3, w3, _ := os.Pipe()
	os.Stdout = w3
	handleSubcommand([]string{"hook", "remove", "claude"})
	_ = w3.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r3)
	if !strings.Contains(buf.String(), "Removed Claude Code") {
		t.Fatalf("remove claude: %q", buf.String())
	}

	r4, w4, _ := os.Pipe()
	os.Stdout = w4
	handleSubcommand([]string{"hook", "remove", "codex"})
	_ = w4.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r4)
	if !strings.Contains(buf.String(), "Removed Slimference hooks from Codex") {
		t.Fatalf("remove codex: %q", buf.String())
	}
}

func TestHandleSubcommand_hook_verify_afterInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r0, w0, _ := os.Pipe()
	os.Stdout = w0
	handleSubcommand([]string{"hook", "install", "claude"})
	_ = w0.Close()
	os.Stdout = old
	_, _ = io.Copy(io.Discard, r0)

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "verify"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "sha256=") {
		t.Fatalf("verify stdout: %q", out)
	}
}

func TestMustOpenFilterDB_invalidSQLiteExits1(t *testing.T) {
	if os.Getenv("TP_MUSTOPEN_BAD") == "1" {
		mustOpenFilterDB()
		return
	}
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "filter.db")
	if err := os.WriteFile(dbPath, []byte("not-a-sqlite-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMustOpenFilterDB_invalidSQLiteExits1")
	cmd.Env = append(os.Environ(), "TP_MUSTOPEN_BAD=1", "SLIMFERENCE_FILTER_DB="+dbPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestMustOpenFilterDB_statNotExistVsOther covers mustOpenFilterDB when os.Stat fails with
// an error other than ErrNotExist (e.g. permission denied traversing a mode-000 directory).
func TestMustOpenFilterDB_statNotExistVsOther(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode not applicable")
	}
	if os.Getenv("TP_MUSTOPEN_STAT") == "1" {
		mustOpenFilterDB()
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

	cmd := exec.Command(os.Args[0], "-test.run=TestMustOpenFilterDB_statNotExistVsOther")
	cmd.Env = append(os.Environ(), "TP_MUSTOPEN_STAT=1", "SLIMFERENCE_FILTER_DB="+dbPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
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

// TestHandleSubcommand_unknownExits1 verifies unknown top-level command exits 1 (subprocess).
func TestHandleSubcommand_unknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_UNKNOWN") == "1" {
		handleSubcommand([]string{"not-a-command"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_unknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_UNKNOWN=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_configUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_CFG_BAD") == "1" {
		handleSubcommand([]string{"config", "not-a-subcommand"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_CFG_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_configUnknownSubcommandExits1(t *testing.T) {
	if os.Getenv("TP_CFG_UNKNOWN_SUB") == "1" {
		handleSubcommand([]string{"config", "nope"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configUnknownSubcommandExits1")
	cmd.Env = append(os.Environ(), "TP_CFG_UNKNOWN_SUB=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestParseGainArgs_jsonAlias(t *testing.T) {
	_, f, err := parseGainArgs([]string{"-json", "month"})
	if err != nil || !f.json {
		t.Fatalf("err=%v json=%v", err, f.json)
	}
}

func TestParseDebugPeriodArgs_jsonAlias(t *testing.T) {
	p, j, err := parseDebugPeriodArgs([]string{"-json", "week"})
	if err != nil || p != "week" || !j {
		t.Fatalf("period=%q json=%v err=%v", p, j, err)
	}
}

func TestParseDebugPeriodArgs_unknownFlag(t *testing.T) {
	_, _, err := parseDebugPeriodArgs([]string{"--not-a-flag"})
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
}

func TestParseDebugPeriodArgs_empty(t *testing.T) {
	p, j, err := parseDebugPeriodArgs(nil)
	if err != nil || p != "today" || j {
		t.Fatalf("period=%q json=%v err=%v", p, j, err)
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

func TestHandleSubcommand_statsUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_STATS_USAGE") == "1" {
		handleSubcommand([]string{"stats"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_statsUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_STATS_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_configUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_CONFIG_USAGE") == "1" {
		handleSubcommand([]string{"config"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_CONFIG_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_testUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_TEST_USAGE") == "1" {
		handleSubcommand([]string{"test"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_testUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_TEST_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_DEBUG_BAD") == "1" {
		handleSubcommand([]string{"debug", "not-a-subcommand"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_DEBUG_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_DEBUG_USAGE") == "1" {
		handleSubcommand([]string{"debug"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_DEBUG_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_statsUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_STATS_BAD") == "1" {
		handleSubcommand([]string{"stats", "not-a-period"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_statsUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_STATS_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
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

func TestHandleSubcommand_filterUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_FILTER_USAGE") == "1" {
		handleSubcommand([]string{"filter"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_filterUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_FILTER_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_USAGE") == "1" {
		handleSubcommand([]string{"hook"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_BAD") == "1" {
		handleSubcommand([]string{"hook", "not-a-subcommand"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugSummaryBadPeriodExits1(t *testing.T) {
	if os.Getenv("TP_SUB_DEBUG_SUM_BAD") == "1" {
		handleSubcommand([]string{"debug", "summary", "never"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugSummaryBadPeriodExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_DEBUG_SUM_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookInstallUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_IN_USAGE") == "1" {
		handleSubcommand([]string{"hook", "install"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookInstallUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_IN_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookInstallUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_IN_BAD") == "1" {
		handleSubcommand([]string{"hook", "install", "emacs"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookInstallUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_IN_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookRemoveUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_RM_USAGE") == "1" {
		handleSubcommand([]string{"hook", "remove"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookRemoveUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_RM_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookRemoveUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_RM_BAD") == "1" {
		handleSubcommand([]string{"hook", "remove", "vim"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookRemoveUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_RM_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugTailUnknownFlagExits1(t *testing.T) {
	if os.Getenv("TP_SUB_TAIL_FLAG") == "1" {
		handleSubcommand([]string{"debug", "tail", "--not-a-flag"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugTailUnknownFlagExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_TAIL_FLAG=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugTailBadLimitExits1(t *testing.T) {
	if os.Getenv("TP_SUB_TAIL_N") == "1" {
		handleSubcommand([]string{"debug", "tail", "0"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugTailBadLimitExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_TAIL_N=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugTailExtraArgExits1(t *testing.T) {
	if os.Getenv("TP_SUB_TAIL_X") == "1" {
		handleSubcommand([]string{"debug", "tail", "10", "20"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugTailExtraArgExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_TAIL_X=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_debugReplayUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_REPLAY") == "1" {
		handleSubcommand([]string{"debug", "replay"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_debugReplayUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_REPLAY=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_testUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_TEST_BAD") == "1" {
		handleSubcommand([]string{"test", "not-a-subcommand"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_testUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_TEST_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_testInterceptUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_TEST_ICPT") == "1" {
		handleSubcommand([]string{"test", "intercept"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_testInterceptUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_TEST_ICPT=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_rewriteLayer0DenyExits2(t *testing.T) {
	if os.Getenv("TP_SUB_RW_DENY") == "1" {
		handleSubcommand([]string{"rewrite", "rm", "-rf", "/"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewriteLayer0DenyExits2")
	cmd.Env = append(os.Environ(), "TP_SUB_RW_DENY=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("want exit 2, got err=%v", err)
	}
}

func TestHandleSubcommand_rewriteSudoExits3(t *testing.T) {
	if os.Getenv("TP_SUB_RW_SUDO") == "1" {
		t.Setenv("SLIMFERENCE_CONFIRM_SUDO", "")
		handleSubcommand([]string{"rewrite", "sudo", "apt", "update"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewriteSudoExits3")
	cmd.Env = append(os.Environ(), "TP_SUB_RW_SUDO=1", "SLIMFERENCE_CONFIRM_SUDO=")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("want exit 3, got err=%v", err)
	}
}

func TestPrintStatsTable_smoke(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printStatsTable([]analytics.AnalyticsSnapshot{
		{
			SessionStart:      time.Now(),
			TotalRequests:     3,
			TotalInputTokens:  1000,
			SavedInputTokens:  100,
			TotalOutputTokens: 50,
			CacheHits:         1,
			MiniMaxCalls:      2,
			SecretsRedacted:   0,
		},
	})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Slimference Stats") {
		t.Fatalf("output: %q", buf.String())
	}
}

func TestPrintStatsTable_empty(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printStatsTable(nil)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if buf.Len() != 0 {
		t.Fatalf("expected no output for empty snapshots, got %q", buf.String())
	}
}

// TestFormatTokensPlain verifies the token formatting helper.
func TestFormatTokensPlain(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{500, "500"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	}
	for _, tt := range tests {
		got := formatTokensPlain(tt.input)
		if got != tt.want {
			t.Errorf("formatTokensPlain(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestSetupLogging_errorLevel covers the "error" case in setupLogging (main.go:1204-1205).
func TestSetupLogging_errorLevel(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "error"
	cfg.Logging.Format = "text"
	setupLogging(cfg)
}

// TestSetupLogging_debugLevel covers the "debug" case in setupLogging (main.go:1200-1201).
func TestSetupLogging_debugLevel(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "debug"
	setupLogging(cfg)
}

func TestSetupLogging_smoke(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "warn"
	cfg.Logging.Format = "json"
	f, err := os.CreateTemp("", "slimference-log-*.log")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(path) }()
	cfg.Logging.File = path
	setupLogging(cfg)
}

func TestProxyAdapter_smoke(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 0
	p := proxy.New(cfg)
	a := newProxyAdapter(p)
	a.SetProviderEnabled(types.Anthropic, false)
	if a.IsProviderEnabled(types.Anthropic) {
		t.Fatal("expected anthropic disabled")
	}
	a.SetProviderEnabled(types.Anthropic, true)
	a.SetLayerEnabled(2, false)
	if a.IsLayerEnabled(2) {
		t.Fatal("expected layer 2 off")
	}
	a.FlushCaches()
	_ = a.GetAnalytics()
	_ = a.GetRecentRequests(2)
	_ = a.GetLayer2Status()
	_ = a.SessionLogger()
	_ = a.GetProviderHealth(types.Anthropic)
	_ = a.GetProviderHealth(types.OpenAI)
	if a.Config().GetListenPort() != cfg.Proxy.ListenPort {
		t.Fatal("config adapter")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestGetLayer2Status_withCache covers the cs != nil branch (main.go:1275-1277) by using
// a proxy whose Layer2 cache has a stored summary.
func TestGetLayer2Status_withCache(t *testing.T) {
	cfg := config.Defaults()
	p := proxy.New(cfg)
	cache := p.GetLayer2Cache()
	if cache == nil {
		t.Skip("no layer2 cache available")
	}
	cache.Store(&summarization.CachedSummary{
		Summary:   "test",
		CreatedAt: time.Now(),
	})
	a := newProxyAdapter(p)
	st := a.GetLayer2Status()
	if !st.HasCache {
		t.Fatalf("expected HasCache=true, got %+v", st)
	}
	if st.LastRun.IsZero() {
		t.Fatalf("expected non-zero LastRun, got %+v", st)
	}
}

// TestHandleDebugLast_noRows covers handleDebugLast when the DB exists but has no rows
// (main.go:1041-1044).
func TestHandleDebugLast_noRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty-filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "last"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No rows in filter_runs") {
		t.Fatalf("expected no-rows message, got: %q", buf.String())
	}
}

// TestHandleDebugPaths_projectFiltersPresent covers the "[present]" branch (main.go:1119-1121)
// when a .slimference/filters.toml file exists in the working directory.
func TestHandleDebugPaths_projectFiltersPresent(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".slimference"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".slimference", "filters.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "paths"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "[present]") {
		t.Fatalf("expected '[present]' for project filters, got: %q", out)
	}
}

// TestTestUpstream_connRefusedExits1 covers testUpstream error path (main.go:499-502).
func TestTestUpstream_connRefusedExits1(t *testing.T) {
	if os.Getenv("TP_UPSTREAM_FAIL") == "1" {
		testUpstream("Test", "http://127.0.0.1:1")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestTestUpstream_connRefusedExits1")
	cmd.Env = append(os.Environ(), "TP_UPSTREAM_FAIL=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestTestMiniMax_connRefusedExits1 covers testMiniMax error path (main.go:516-519).
func TestTestMiniMax_connRefusedExits1(t *testing.T) {
	if os.Getenv("TP_MINIMAX_FAIL") == "1" {
		t.Setenv("MINIMAX_API_KEY", "dummy-key")
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "config: %v\n", err)
			os.Exit(1)
		}
		cfg.Compression.MiniMax.BaseURL = "http://127.0.0.1:1/v1"
		testMiniMax(cfg)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestTestMiniMax_connRefusedExits1")
	cmd.Env = append(os.Environ(), "TP_MINIMAX_FAIL=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleTestCmd_configLoadErrorExits1 covers handleTestCmd config load error (main.go:471-474).
func TestHandleTestCmd_configLoadErrorExits1(t *testing.T) {
	if os.Getenv("TP_TESTCMD_CFG_BAD") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", os.Getenv("TP_BAD_CFG_FILE"))
		handleTestCmd([]string{"anthropic"})
		return
	}
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badPath, []byte("not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleTestCmd_configLoadErrorExits1")
	cmd.Env = append(os.Environ(), "TP_TESTCMD_CFG_BAD=1", "TP_BAD_CFG_FILE="+badPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleFilterCmd_deniedExits2 covers handleFilterCmd layer0 permission deny (main.go:274-277).
func TestHandleFilterCmd_deniedExits2(t *testing.T) {
	if os.Getenv("TP_FILTER_DENY") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleFilterCmd([]string{"rm", "-rf", "/"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleFilterCmd_deniedExits2")
	cmd.Env = append(os.Environ(), "TP_FILTER_DENY=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("want exit 2 (deny), got err=%v", err)
	}
}

// TestHandleFilterCmd_sudoExits3 covers handleFilterCmd layer0 sudo ask path (main.go:274-277 exit 3).
func TestHandleFilterCmd_sudoExits3(t *testing.T) {
	if os.Getenv("TP_FILTER_SUDO") == "1" {
		t.Setenv("SLIMFERENCE_CONFIRM_SUDO", "")
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleFilterCmd([]string{"sudo", "apt", "update"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleFilterCmd_sudoExits3")
	cmd.Env = append(os.Environ(), "TP_FILTER_SUDO=1", "SLIMFERENCE_CONFIRM_SUDO=")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("want exit 3 (sudo ask), got err=%v", err)
	}
}

// TestHandleHookCmd_verifyNotOkExits1 covers the verify !ok branch (main.go:420-422) - hooks not installed.
func TestHandleHookCmd_verifyNotOkExits1(t *testing.T) {
	if os.Getenv("TP_HOOK_VFY_FAIL") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_VFY_HOME"))
		handleSubcommand([]string{"hook", "verify"})
		return
	}
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_verifyNotOkExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_VFY_FAIL=1", "TP_HOOK_VFY_HOME="+home)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 (hooks missing), got err=%v", err)
	}
}

// TestHandleHookCmd_installClaude_success covers hooks.InstallClaude success path (main.go:382).
// Uses a temp HOME so the install writes to a temp dir.
func TestHandleHookCmd_installClaude_success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "install", "claude"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Installed Claude") {
		t.Fatalf("expected install message, got: %q", buf.String())
	}
}

// TestHandleHookCmd_installCodex_success covers hooks.InstallCodex success path (main.go:388).
func TestHandleHookCmd_installCodex_success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "install", "codex"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Installed Codex hooks") {
		t.Fatalf("expected codex install message, got: %q", buf.String())
	}
}

// TestHandleHookCmd_removeClaude_success covers hooks.RemoveClaude success path (main.go:404).
func TestHandleHookCmd_removeClaude_success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	// Install first so there is something to remove.
	if err := os.MkdirAll(filepath.Join(home, ".claude", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "remove", "claude"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Removed Claude Code") {
		t.Fatalf("expected remove message, got: %q", buf.String())
	}
}

// TestHandleHookCmd_removeCodex_success covers hooks.RemoveCodex success path (main.go:410).
func TestHandleHookCmd_removeCodex_success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "remove", "codex"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Removed Slimference hooks from Codex") {
		t.Fatalf("expected remove message, got: %q", buf.String())
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

// TestHandleDebugPaths_configLoadErrorExits1 covers handleDebugPaths config load error (main.go:1068-1071).
func TestHandleDebugPaths_configLoadErrorExits1(t *testing.T) {
	if os.Getenv("TP_DEBUG_PATHS_CFG_BAD") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", os.Getenv("TP_BAD_CFG_PATH"))
		handleSubcommand([]string{"debug", "paths"})
		return
	}
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badPath, []byte("not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleDebugPaths_configLoadErrorExits1")
	cmd.Env = append(os.Environ(), "TP_DEBUG_PATHS_CFG_BAD=1", "TP_BAD_CFG_PATH="+badPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleRewriteCmd_dashDashSkip covers the `if a == "--" { continue }` branch.
// Uses a filterable command so exit 0 with rewritten output is expected (spec+.md §4.2).
func TestHandleRewriteCmd_dashDashSkip(t *testing.T) {
	if os.Getenv("TP_RW_DASHDASH") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleRewriteCmd([]string{"--", "git", "status"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleRewriteCmd_dashDashSkip")
	cmd.Env = append(os.Environ(), "TP_RW_DASHDASH=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("want exit 0, got err=%v out=%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "slimference filter git status" {
		t.Fatalf("expected 'slimference filter git status', got %q", out)
	}
}

// TestHandleRewriteCmd_dashDashSkip_NoFilter covers the exit-1 path when "--" skips
// the separator but the resulting command has no matching filter (spec+.md §4.2 exit 1 = passthrough).
func TestHandleRewriteCmd_dashDashSkip_NoFilter(t *testing.T) {
	if os.Getenv("TP_RW_DASHDASH_NF") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleRewriteCmd([]string{"--", "echo", "hi"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleRewriteCmd_dashDashSkip_NoFilter")
	cmd.Env = append(os.Environ(), "TP_RW_DASHDASH_NF=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 (no filter), got err=%v", err)
	}
}

// TestHandleFilterCmd_prErrExits1 covers the pr.Err != nil branch (main.go:312-315) when
// the command cannot be found (exec failure).
func TestHandleFilterCmd_prErrExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	if os.Getenv("TP_FILTER_PRERR") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleFilterCmd([]string{"nonexistent-command-xyz-abc-1234"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleFilterCmd_prErrExits1")
	cmd.Env = append(os.Environ(), "TP_FILTER_PRERR=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleHookCmd_installClaude_errorExits1 covers hooks.InstallClaude error path (main.go:378-381).
// Makes HOME an unwritable dir so MkdirAll fails inside InstallClaude.
func TestHandleHookCmd_installClaude_errorExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on windows")
	}
	if os.Getenv("TP_HOOK_ICLAUDE_ERR") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_ICLAUDE_HOME"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"hook", "install", "claude"})
		return
	}
	// Create a read-only parent dir so MkdirAll fails when trying to create .claude/hooks.
	tmp := t.TempDir()
	roHome := filepath.Join(tmp, "ro-home")
	if err := os.Mkdir(roHome, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roHome, 0o755) })
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_installClaude_errorExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_ICLAUDE_ERR=1", "TP_HOOK_ICLAUDE_HOME="+roHome)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 from InstallClaude error, got err=%v", err)
	}
}

// TestHandleHookCmd_installCodex_errorExits1 covers hooks.InstallCodex error path (main.go:384-387).
func TestHandleHookCmd_installCodex_errorExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on windows")
	}
	if os.Getenv("TP_HOOK_ICODEX_ERR") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_ICODEX_HOME"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"hook", "install", "codex"})
		return
	}
	tmp := t.TempDir()
	roHome := filepath.Join(tmp, "ro-home")
	if err := os.Mkdir(roHome, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roHome, 0o755) })
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_installCodex_errorExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_ICODEX_ERR=1", "TP_HOOK_ICODEX_HOME="+roHome)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 from InstallCodex error, got err=%v", err)
	}
}

// TestHandleHookCmd_removeClaude_errorExits1 covers hooks.RemoveClaude error path (main.go:400-403).
// Makes settings.json contain invalid JSON so stripClaudePreToolUse fails.
func TestHandleHookCmd_removeClaude_errorExits1(t *testing.T) {
	if os.Getenv("TP_HOOK_RCLAUDE_ERR") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_RCLAUDE_HOME"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"hook", "remove", "claude"})
		return
	}
	home := t.TempDir()
	// Create a settings.json with invalid JSON to force stripClaudePreToolUse to return error.
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("not json {{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_removeClaude_errorExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_RCLAUDE_ERR=1", "TP_HOOK_RCLAUDE_HOME="+home)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 from RemoveClaude error, got err=%v", err)
	}
}

// TestHandleHookCmd_removeCodex_errorExits1 covers hooks.RemoveCodex error path (main.go:406-409).
func TestHandleHookCmd_removeCodex_errorExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on windows")
	}
	if os.Getenv("TP_HOOK_RCODEX_ERR") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_RCODEX_HOME"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"hook", "remove", "codex"})
		return
	}
	home := t.TempDir()
	// Create .codex/AGENTS.md as a directory so WriteFile fails when trying to update it.
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create AGENTS.md as a directory (not a file) so os.WriteFile fails.
	if err := os.Mkdir(filepath.Join(codexDir, "AGENTS.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Also write a file inside so it looks like it has content when read as dir.
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_removeCodex_errorExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_RCODEX_ERR=1", "TP_HOOK_RCODEX_HOME="+home)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 from RemoveCodex error, got err=%v", err)
	}
}

// TestHandleConfigCmd_initMkdirErrorExits1 covers handleConfigCmd "init" MkdirAll error (main.go:443-446).
// Arrange HOME so DefaultConfigPath resolves into a read-only directory that blocks mkdir.
func TestHandleConfigCmd_initMkdirErrorExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on windows")
	}
	if os.Getenv("TP_CFG_INIT_MKDIR_ERR") == "1" {
		// We need DefaultConfigPath() to point inside a read-only directory.
		// DefaultConfigPath() uses os.UserHomeDir() via ~ expansion, but os.Chdir
		// to a dir with a read-only "~" subdir makes filepath.Join("~", ...) resolve literally.
		_ = os.Chdir(os.Getenv("TP_CFG_INIT_MKDIR_DIR"))
		handleSubcommand([]string{"config", "init"})
		return
	}
	tmp := t.TempDir()
	// "~" resolved literally when cwd has a read-only "~" dir.
	tildePath := filepath.Join(tmp, "~")
	if err := os.Mkdir(tildePath, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tildePath, 0o755) })
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleConfigCmd_initMkdirErrorExits1")
	cmd.Env = append(os.Environ(), "TP_CFG_INIT_MKDIR_ERR=1", "TP_CFG_INIT_MKDIR_DIR="+tmp)
	cmd.Dir = tmp
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

// TestHandleDebugReplay_fileErrorExits1 covers handleDebugReplay when the session file
// cannot be read (main.go:1010-1013).
func TestHandleDebugReplay_fileErrorExits1(t *testing.T) {
	if os.Getenv("TP_REPLAY_ERR") == "1" {
		handleSubcommand([]string{"debug", "replay", "/nonexistent/path/session.jsonl"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleDebugReplay_fileErrorExits1")
	cmd.Env = append(os.Environ(), "TP_REPLAY_ERR=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleDebugReplay_replayParseErrorExits1 covers the replaySessionFn error path in
// handleDebugReplay (replay parse: %v) by injecting a failing replaySessionFn.
func TestHandleDebugReplay_replayParseErrorExits1(t *testing.T) {
	if os.Getenv("TP_REPLAY_PARSE_ERR") == "1" {
		dir := t.TempDir()
		path := filepath.Join(dir, "s.jsonl")
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		orig := replaySessionFn
		replaySessionFn = func(_ string) ([]dbg.RequestSummary, error) {
			return nil, fmt.Errorf("injected replay error")
		}
		defer func() { replaySessionFn = orig }()
		handleSubcommand([]string{"debug", "replay", path})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleDebugReplay_replayParseErrorExits1")
	cmd.Env = append(os.Environ(), "TP_REPLAY_PARSE_ERR=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleDebugReplay_noSummaries covers the "No decodable request summaries found." path.
func TestHandleDebugReplay_noSummaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte("not-valid-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "replay", path})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "No decodable request summaries found.") {
		t.Fatalf("expected 'No decodable request summaries found.' in output, got: %q", out)
	}
}

// TestHandleDebugReplay_fullOutput covers the full replay display path including
// tokens, layers, layer1 breakdown, and layer2 stats.
func TestHandleDebugReplay_fullOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	// Two summaries: first with layer1 breakdown, second with layer2 applied.
	content := `{"req_id":"r1","provider":"anthropic","model":"claude-3","layers_applied":[1],"tokens":{"original":1000,"final":800,"saved":200,"ratio":0.8},"layer1_breakdown":{"ansi_strip":{"blocks":3,"saved":200}}}
{"req_id":"r2","provider":"openai","model":"gpt-4","layers_applied":[1,2],"tokens":{"original":2000,"final":1400,"saved":600,"ratio":0.7},"layer2":{"applied":true,"compression_ratio":0.85,"anchor_count":5}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "replay", path})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Session replay (preview)") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "anthropic/claude-3") {
		t.Fatalf("missing r1 provider/model: %q", out)
	}
	if !strings.Contains(out, "openai/gpt-4") {
		t.Fatalf("missing r2 provider/model: %q", out)
	}
	if !strings.Contains(out, "layer1:") {
		t.Fatalf("missing layer1 section: %q", out)
	}
	if !strings.Contains(out, "ansi_strip") {
		t.Fatalf("missing ansi_strip breakdown: %q", out)
	}
	if !strings.Contains(out, "layer2:") {
		t.Fatalf("missing layer2 section: %q", out)
	}
	if !strings.Contains(out, "TOTAL: 2 request(s)  800 tokens saved") {
		t.Fatalf("missing total line: %q", out)
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

// TestHandleDebugSummary_queryErrorExits1 covers handleDebugSummary QueryFilterRunsAggregate error
// (main.go:915-918) by using a corrupt (non-SQLite) database file.
func TestHandleDebugSummary_queryErrorExits1(t *testing.T) {
	if os.Getenv("TP_DBG_SUM_QUERY_ERR") == "1" {
		t.Setenv("SLIMFERENCE_FILTER_DB", os.Getenv("TP_DBG_SUM_CORRUPT_DB"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"debug", "summary", "today"})
		return
	}
	tmp := t.TempDir()
	corruptDB := filepath.Join(tmp, "corrupt.db")
	if err := os.WriteFile(corruptDB, []byte("not-a-sqlite-database"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleDebugSummary_queryErrorExits1")
	cmd.Env = append(os.Environ(), "TP_DBG_SUM_QUERY_ERR=1", "TP_DBG_SUM_CORRUPT_DB="+corruptDB)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleDebugTail_queryErrorExits1 covers handleDebugTail filter.RecentFilterRuns error
// (main.go:977-980) by using a corrupt (non-SQLite) database file.
func TestHandleDebugTail_queryErrorExits1(t *testing.T) {
	if os.Getenv("TP_DBG_TAIL_QUERY_ERR") == "1" {
		t.Setenv("SLIMFERENCE_FILTER_DB", os.Getenv("TP_DBG_TAIL_CORRUPT_DB"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"debug", "tail", "5"})
		return
	}
	tmp := t.TempDir()
	corruptDB := filepath.Join(tmp, "corrupt.db")
	if err := os.WriteFile(corruptDB, []byte("not-a-sqlite-database"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleDebugTail_queryErrorExits1")
	cmd.Env = append(os.Environ(), "TP_DBG_TAIL_QUERY_ERR=1", "TP_DBG_TAIL_CORRUPT_DB="+corruptDB)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleDebugLast_queryErrorExits1 covers handleDebugLast filter.LastFilterRun error
// (main.go:1037-1040) by using a corrupt (non-SQLite) database file.
func TestHandleDebugLast_queryErrorExits1(t *testing.T) {
	if os.Getenv("TP_DBG_LAST_QUERY_ERR") == "1" {
		t.Setenv("SLIMFERENCE_FILTER_DB", os.Getenv("TP_DBG_LAST_CORRUPT_DB"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"debug", "last"})
		return
	}
	tmp := t.TempDir()
	corruptDB := filepath.Join(tmp, "corrupt.db")
	if err := os.WriteFile(corruptDB, []byte("not-a-sqlite-database"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleDebugLast_queryErrorExits1")
	cmd.Env = append(os.Environ(), "TP_DBG_LAST_QUERY_ERR=1", "TP_DBG_LAST_CORRUPT_DB="+corruptDB)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleTestCmd_interceptCallsTestIntercept covers the testIntercept call from handleTestCmd
// (main.go:488) via the intercept subcommand path. Uses an ephemeral listen port.
func TestHandleTestCmd_interceptCallsTestIntercept(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	done := make(chan struct{})
	go func() {
		handleTestCmd([]string{"intercept", "claude"})
		close(done)
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	ok := false
	for range 100 {
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ok = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !ok {
		t.Fatal("handleTestCmd intercept: server did not respond with 200")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handleTestCmd intercept did not finish")
	}
}

// --- readLastDecisionSummaries ---

// TestReadLastDecisionSummaries_fileNotFound covers the os.Open error → nil branch.
func TestReadLastDecisionSummaries_fileNotFound(t *testing.T) {
	t.Parallel()
	got := readLastDecisionSummaries("/nonexistent/path/decisions.jsonl", 5)
	if got != nil {
		t.Errorf("file not found: want nil, got %v", got)
	}
}

// TestReadLastDecisionSummaries_emptyLines covers the len(lines)==0 → nil branch.
func TestReadLastDecisionSummaries_emptyLines(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "decisions.jsonl")
	if err := os.WriteFile(tmp, []byte("\n  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readLastDecisionSummaries(tmp, 5)
	if got != nil {
		t.Errorf("blank-only file: want nil, got %v", got)
	}
}

// TestReadLastDecisionSummaries_validEntries covers the happy path with n < len(lines);
// results must be newest-first.
func TestReadLastDecisionSummaries_validEntries(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "decisions.jsonl")
	content := `{"req_id":"req-1","ts":"2024-01-01T00:00:00Z","provider":"anthropic"}` + "\n" +
		`{"req_id":"req-2","ts":"2024-01-01T01:00:00Z","provider":"openai"}` + "\n" +
		`{"req_id":"req-3","ts":"2024-01-01T02:00:00Z","provider":"anthropic"}` + "\n"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Request last 2; want newest-first: req-3, req-2.
	got := readLastDecisionSummaries(tmp, 2)
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
	if got[0].RequestID != "req-3" || got[1].RequestID != "req-2" {
		t.Errorf("newest-first: want [req-3, req-2], got [%s, %s]", got[0].RequestID, got[1].RequestID)
	}
}

// TestReadLastDecisionSummaries_nLargerThanLines covers the start<0 → start=0 branch.
func TestReadLastDecisionSummaries_nLargerThanLines(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "decisions.jsonl")
	content := `{"req_id":"req-only","ts":"2024-01-01T00:00:00Z","provider":"anthropic"}` + "\n"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// n=10 > 1 line: start = 1-10 = -9 < 0 → start=0.
	got := readLastDecisionSummaries(tmp, 10)
	if len(got) != 1 {
		t.Fatalf("want 1 result, got %d", len(got))
	}
	if got[0].RequestID != "req-only" {
		t.Errorf("want req-only, got %s", got[0].RequestID)
	}
}

// TestReadLastDecisionSummaries_invalidJSONSkipped covers the json.Unmarshal error path.
func TestReadLastDecisionSummaries_invalidJSONSkipped(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "decisions.jsonl")
	content := `{"req_id":"req-good","ts":"2024-01-01T00:00:00Z","provider":"anthropic"}` + "\n" +
		`{not valid json}` + "\n"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readLastDecisionSummaries(tmp, 5)
	if len(got) != 1 {
		t.Fatalf("invalid JSON skipped: want 1 result, got %d", len(got))
	}
	if got[0].RequestID != "req-good" {
		t.Errorf("want req-good, got %s", got[0].RequestID)
	}
}

func TestReadLastDecisionSummaries_nonSummarySkipped(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "decisions.jsonl")
	content := `{"req_id":"req-good","ts":"2024-01-01T00:00:00Z","provider":"anthropic"}` + "\n" +
		`{"x":1}` + "\n"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readLastDecisionSummaries(tmp, 5)
	if len(got) != 1 {
		t.Fatalf("non-summary skipped: want 1 result, got %d", len(got))
	}
	if got[0].RequestID != "req-good" {
		t.Errorf("want req-good, got %s", got[0].RequestID)
	}
}

func TestReadLastDecisionSummaries_scanErrorReturnsNil(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "decisions.jsonl")
	bigLine := make([]byte, 9*1024*1024)
	for i := range bigLine {
		bigLine[i] = 'x'
	}
	bigLine[len(bigLine)-1] = '\n'
	if err := os.WriteFile(tmp, bigLine, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readLastDecisionSummaries(tmp, 5); got != nil {
		t.Fatalf("scan error should return nil, got %v", got)
	}
}

// TestHandleDebugLast_nArg covers the strconv.Atoi(a) && v>0 → n=v branch.
func TestHandleDebugLast_nArg(t *testing.T) {
	t.Setenv("SLIMFERENCE_FILTER_DB", filepath.Join(t.TempDir(), "nonexistent.db"))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugLast([]string{"3"}) // "3" → n=3 (covers Atoi && v>0 branch)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No filter.db") {
		t.Errorf("want no-filter-db message, got: %q", buf.String())
	}
}

// TestHandleDebugLast_withDecisionsLog covers the decisionsPath branch with text output.
func TestHandleDebugLast_withDecisionsLog(t *testing.T) {
	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	content := `{"req_id":"req-A","ts":"2024-01-01T00:00:00Z","provider":"anthropic","model":"claude-3-5","tokens":{"original":100,"final":80,"saved":20,"ratio":0.8},"layers_applied":[1]}` + "\n"
	if err := os.WriteFile(decisionsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugLast(nil)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "req-A") {
		t.Errorf("decisions log text output: want req-A, got: %q", buf.String())
	}
}

// TestHandleDebugLast_withDecisionsLog_jsonOut covers the decisionsPath + jsonOut=true branch.
func TestHandleDebugLast_withDecisionsLog_jsonOut(t *testing.T) {
	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	content := `{"req_id":"req-json","ts":"2024-01-01T00:00:00Z","provider":"openai","model":"gpt-4","tokens":{"original":200,"final":150,"saved":50,"ratio":0.75},"layers_applied":[0]}` + "\n"
	if err := os.WriteFile(decisionsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugLast([]string{"--json"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"req_id"`) {
		t.Errorf("decisions log JSON output: want req_id field, got: %q", buf.String())
	}
}

// TestHandleDebugLast_withLayer1Breakdown covers the layer1_breakdown iteration branch.
func TestHandleDebugLast_withLayer1Breakdown(t *testing.T) {
	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	content := `{"req_id":"req-bd","ts":"2024-01-01T00:00:00Z","provider":"anthropic","model":"claude-3","tokens":{"original":300,"final":200,"saved":100,"ratio":0.67},"layers_applied":[1],"layer1_breakdown":{"strip_comments":{"blocks":5,"saved":50},"compact_json":{"blocks":2,"saved":50}}}` + "\n"
	if err := os.WriteFile(decisionsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugLast(nil)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "layer1") {
		t.Errorf("want layer1 breakdown in output, got: %q", buf.String())
	}
}

// TestTestIntercept_codexProvider covers the "codex" provider branch in testIntercept.
func TestTestIntercept_codexProvider(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	done := make(chan struct{})
	go func() {
		handleTestCmd([]string{"intercept", "codex"})
		close(done)
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	var ok bool
	for range 100 {
		req, reqErr := http.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := client.Do(req)
		if doErr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ok = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !ok {
		t.Fatal("codex intercept: server did not respond with 200")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("codex intercept did not finish")
	}
}

// TestHandleDebugSummary_jsonOut covers the jsonOut=true output path in handleDebugSummary.
func TestHandleDebugSummary_jsonOut(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugSummary([]string{"today", "--json"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "{") {
		t.Errorf("want JSON object in output, got: %q", buf.String())
	}
}

// TestHandleDebugSummary_textOutput covers the jsonOut=false text output path in handleDebugSummary.
func TestHandleDebugSummary_textOutput(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugSummary([]string{"today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "filter_runs summary") {
		t.Errorf("want summary header in text output, got: %q", buf.String())
	}
}

// TestHandleDebugTail_textOutput covers the jsonOut=false text output path in handleDebugTail.
func TestHandleDebugTail_textOutput(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugTail(nil)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	// Empty DB → "No rows in filter_runs."
	if !strings.Contains(buf.String(), "No rows") {
		t.Errorf("want 'No rows' for empty DB, got: %q", buf.String())
	}
}

// TestHandleDebugTail_jsonOut covers the jsonOut=true output path in handleDebugTail.
func TestHandleDebugTail_jsonOut(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugTail([]string{"--json"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	// Empty DB: jsonOut → prints `null` or `[]`; just verify no panic and some output.
	if len(buf.String()) == 0 {
		t.Errorf("want some JSON output, got empty string")
	}
}

// ---- in-process exit-injection helpers & tests ----

// exitPanic is the sentinel type panicked by the injected exitFn.
type exitPanic struct{ code int }

// captureExit runs fn and returns the exit code + whether exitFn was called.
// exitFn is temporarily overridden to panic with exitPanic{code}; the deferred
// recover catches it and returns the code. Any other panic is re-panicked.
func captureExit(fn func()) (code int, exited bool) {
	orig := exitFn
	exitFn = func(c int) { panic(exitPanic{c}) }
	defer func() { exitFn = orig }()

	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(exitPanic); ok {
				code = e.code
				exited = true
			} else {
				panic(r)
			}
		}
	}()

	fn()
	return -1, false
}

// redirectStderr replaces os.Stderr with a pipe and returns a reader and a cleanup
// func. The cleanup func closes the write end and restores os.Stderr. Call it after
// captureExit returns (before reading the pipe) to close the write end.
//
// Pattern for tests that use both captureExit and stderr capture:
//
//	rp, cleanup := redirectStderr()
//	code, exited := captureExit(fn)
//	cleanup()
//	var buf bytes.Buffer; io.Copy(&buf, rp)
func redirectStderr() (r *os.File, cleanup func()) {
	orig := os.Stderr
	rp, wp, _ := os.Pipe()
	os.Stderr = wp
	return rp, func() {
		_ = wp.Close()
		os.Stderr = orig
	}
}

// TestMain_noArgs covers the `runTUIFn()` branch in main() (main.go:75).
func TestMain_noArgs(t *testing.T) {
	orig := runTUIFn
	defer func() { runTUIFn = orig }()
	called := false
	runTUIFn = func() { called = true }

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"slimference"}

	main()
	if !called {
		t.Fatal("runTUIFn was not called")
	}
}

// TestMain_withArgs covers the handleSubcommand branch in main() (main.go:71-74).
func TestMain_withArgs(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"slimference", "version"}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	main()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "slimference v") {
		t.Fatalf("stdout: %q", buf.String())
	}
}

// TestRunTUI_configError covers the config error exit in runTUI() (main.go:81-84).
func TestRunTUI_configError(t *testing.T) {
	tmp := t.TempDir()
	badCfg := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badCfg, []byte("this is not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", badCfg)

	rp, cleanup := redirectStderr()
	code, exited := captureExit(runTUI)
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "config error") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestRunTUI_proxyStartError covers runTUI past config load: proxy setup, goroutine start,
// and the "proxy start failed" exit when the listen port is already in use.
func TestRunTUI_proxyStartError(t *testing.T) {
	// Bind the port the proxy will try to use; this makes p.Start() fail immediately.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(runTUI)
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d stderr=%q", exited, code, buf.String())
	}
	if !strings.Contains(buf.String(), "proxy start failed") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleFilterCmd_getwdError covers the os.Getwd error exit in handleFilterCmd (main.go:293-297).
func TestHandleFilterCmd_getwdError(t *testing.T) {
	orig := osGetwd
	defer func() { osGetwd = orig }()
	osGetwd = func() (string, error) { return "", errors.New("getwd failed") }
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"filter", "--", "echo", "hi"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "getwd") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleRewriteCmd_terminalTrue covers the TTY-detected exit in handleRewriteCmd (main.go:351-354).
func TestHandleRewriteCmd_terminalTrue(t *testing.T) {
	orig := termIsTerminalFn
	defer func() { termIsTerminalFn = orig }()
	termIsTerminalFn = func(fd int) bool { return true }
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"rewrite"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "usage: slimference rewrite") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleRewriteCmd_stdinReadError covers the io.ReadAll error exit in handleRewriteCmd (main.go:355-359).
func TestHandleRewriteCmd_stdinReadError(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
	}()
	termIsTerminalFn = func(fd int) bool { return false }
	readStdinAll = func() ([]byte, error) { return nil, errors.New("read failed") }
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"rewrite"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "read stdin") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleHookCmd_homeDirError covers the os.UserHomeDir error exit in handleHookCmd (main.go:375-379).
func TestHandleHookCmd_homeDirError(t *testing.T) {
	orig := osUserHomeDir
	defer func() { osUserHomeDir = orig }()
	osUserHomeDir = func() (string, error) { return "", errors.New("no home dir") }

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"hook", "install", "claude"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "home") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleConfigCmd_writeFileError covers the os.WriteFile error exit in handleConfigCmd (main.go:460-463).
func TestHandleConfigCmd_writeFileError(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)

	orig := osWriteFile
	defer func() { osWriteFile = orig }()
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		return errors.New("write failed")
	}

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"config", "init"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "write config") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestTestIntercept_timeout covers the timeout exit in testIntercept() (main.go:583-590).
// testInterceptTimeout is set to 1ms so the case fires immediately.
func TestTestIntercept_timeout(t *testing.T) {
	origTimeout := testInterceptTimeout
	defer func() { testInterceptTimeout = origTimeout }()
	testInterceptTimeout = time.Millisecond

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	defer func() { os.Stdout = old }()

	code, exited := captureExit(func() {
		testIntercept(cfg, "claude")
	})
	_ = wp.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "FAIL") {
		t.Fatalf("stdout: %q", buf.String())
	}
}

// TestMustOpenFilterDB_resolvePathError covers the resolveFilterDBPathFn error exit (main.go:254-257).
func TestMustOpenFilterDB_resolvePathError(t *testing.T) {
	orig := resolveFilterDBPathFn
	defer func() { resolveFilterDBPathFn = orig }()
	resolveFilterDBPathFn = func() (string, error) { return "", errors.New("path error") }

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { mustOpenFilterDB() })
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

// TestMustOpenFilterDB_invalidSQLite_inProcess is the in-process equivalent of
// TestMustOpenFilterDB_invalidSQLiteExits1, contributing to coverage.
func TestMustOpenFilterDB_invalidSQLite_inProcess(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "corrupt.db")
	if err := os.WriteFile(dbPath, []byte("not-a-sqlite-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { mustOpenFilterDB() })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "open filter db") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestMustOpenFilterDB_statError_inProcess is the in-process equivalent of
// TestMustOpenFilterDB_statNotExistVsOther, contributing to coverage.
func TestMustOpenFilterDB_statError_inProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode not applicable on Windows")
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
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { mustOpenFilterDB() })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "filter db") {
		t.Fatalf("stderr: %q", buf.String())
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

// TestHandleDebugSummary_queryError_inProcess covers the QueryFilterRunsAggregate error exit.
// Replaces filter_runs with a table that has only (id, created_at) so migrate()'s
// CREATE TABLE IF NOT EXISTS and CREATE INDEX succeed, but the column-specific query fails.
func TestHandleDebugSummary_queryError_inProcess(t *testing.T) {
	tmp := t.TempDir()
	badDB := filepath.Join(tmp, "badschema.db")
	db, err := filter.OpenDB(badDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP INDEX IF EXISTS idx_filter_runs_created; DROP TABLE filter_runs; CREATE TABLE filter_runs (id INTEGER, created_at INTEGER)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	t.Setenv("SLIMFERENCE_FILTER_DB", badDB)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"debug", "summary", "today"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "filter_runs") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleDebugTail_queryError_inProcess covers the RecentFilterRuns error exit.
// Replaces filter_runs with a table that has only (id, created_at) so migrate succeeds
// but the column-specific SELECT query fails.
func TestHandleDebugTail_queryError_inProcess(t *testing.T) {
	tmp := t.TempDir()
	badDB := filepath.Join(tmp, "badschema.db")
	db, err := filter.OpenDB(badDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP INDEX IF EXISTS idx_filter_runs_created; DROP TABLE filter_runs; CREATE TABLE filter_runs (id INTEGER, created_at INTEGER)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	t.Setenv("SLIMFERENCE_FILTER_DB", badDB)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"debug", "tail", "5"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "filter_runs") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleDebugLast_queryError_inProcess covers the LastFilterRun error exit.
// Replaces filter_runs with a table that has only (id, created_at) so migrate succeeds
// but the column-specific SELECT query fails.
func TestHandleDebugLast_queryError_inProcess(t *testing.T) {
	tmp := t.TempDir()
	badDB := filepath.Join(tmp, "badschema.db")
	db, err := filter.OpenDB(badDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP INDEX IF EXISTS idx_filter_runs_created; DROP TABLE filter_runs; CREATE TABLE filter_runs (id INTEGER, created_at INTEGER)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	t.Setenv("SLIMFERENCE_FILTER_DB", badDB)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"debug", "last"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "query filter_runs") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleDebugPaths_resolveErrors covers the resolveFilterDBPathFn and resolveTeeDirFn
// error paths in handleDebugPaths (main.go:1155-1175), and the filterDefaultDataDirFn
// error path (main.go:1177-1180).
func TestHandleDebugPaths_resolveErrors(t *testing.T) {
	origFDB := resolveFilterDBPathFn
	origTee := resolveTeeDirFn
	origData := filterDefaultDataDirFn
	defer func() {
		resolveFilterDBPathFn = origFDB
		resolveTeeDirFn = origTee
		filterDefaultDataDirFn = origData
	}()
	resolveFilterDBPathFn = func() (string, error) { return "", errors.New("fdb error") }
	resolveTeeDirFn = func() (string, error) { return "", errors.New("tee error") }
	filterDefaultDataDirFn = func() (string, error) { return "", errors.New("data error") }

	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	handleSubcommand([]string{"debug", "paths"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "fdb error") || !strings.Contains(out, "tee error") || !strings.Contains(out, "data error") {
		t.Fatalf("expected all error strings in output, got: %q", out)
	}
}

// TestHandleDebugPaths_configError_inProcess covers the config.Load error exit in handleDebugPaths.
func TestHandleDebugPaths_configError_inProcess(t *testing.T) {
	tmp := t.TempDir()
	badCfg := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badCfg, []byte("not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", badCfg)

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"debug", "paths"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "load config") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestProgSender_send_withProg covers the case branch in progSender.send when a program
// is available in the channel: tuiSendProxyEventFn is called and prog is returned.
func TestProgSender_send_withProg(t *testing.T) {
	origSend := tuiSendProxyEventFn
	defer func() { tuiSendProxyEventFn = origSend }()
	var called bool
	tuiSendProxyEventFn = func(_ *tea.Program, _ types.RequestMetrics) { called = true }

	progCh := make(chan *tea.Program, 1)
	var prog *tea.Program // nil pointer is fine since tuiSendProxyEventFn is mocked
	progCh <- prog

	s := &progSender{ch: progCh}
	s.send(types.RequestMetrics{})

	if !called {
		t.Fatal("tuiSendProxyEventFn not called")
	}
	select {
	case got := <-progCh:
		if got != prog {
			t.Fatal("wrong prog returned to channel")
		}
	default:
		t.Fatal("prog not returned to channel after send")
	}
}

// TestProgSender_send_empty covers the default branch in progSender.send when the channel
// holds no program (send is a no-op).
func TestProgSender_send_empty(t *testing.T) {
	origSend := tuiSendProxyEventFn
	defer func() { tuiSendProxyEventFn = origSend }()
	var called bool
	tuiSendProxyEventFn = func(_ *tea.Program, _ types.RequestMetrics) { called = true }

	s := &progSender{ch: make(chan *tea.Program, 1)} // empty channel
	s.send(types.RequestMetrics{})

	if called {
		t.Fatal("tuiSendProxyEventFn should not be called on empty channel")
	}
}

// TestRunTUI_proxyStartOK covers the case <-time.After(proxyStartTimeout) branch in
// runTUI: proxy starts without error within the timeout and runTUIAfterStartFn is called.
func TestRunTUI_proxyStartOK(t *testing.T) {
	// Use a free port (0) so the proxy never fails with "address in use".
	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", "0")
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	origTimeout := proxyStartTimeout
	origAfterStart := runTUIAfterStartFn
	defer func() {
		proxyStartTimeout = origTimeout
		runTUIAfterStartFn = origAfterStart
	}()
	proxyStartTimeout = 50 * time.Millisecond

	afterStartCalled := make(chan *proxy.Proxy, 1)
	runTUIAfterStartFn = func(p *proxy.Proxy, _ chan *tea.Program) {
		afterStartCalled <- p
	}

	runTUI()

	select {
	case p := <-afterStartCalled:
		if p == nil {
			t.Fatal("runTUIAfterStartFn received nil proxy")
		}
		// Shut down the proxy the goroutine started; ignore errors.
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = p.Shutdown(ctx)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: runTUIAfterStartFn never called")
	}
}

// TestRunTUIAfterStart_tuiError covers runTUIAfterStart when program.Run() returns an error:
// signal setup, goroutine cleanup via done channel, TUI error path (exit 1).
func TestRunTUIAfterStart_tuiError(t *testing.T) {
	origRunProg := runTeaProgramFn
	origMakeSig := makeSignalChanFn
	defer func() {
		runTeaProgramFn = origRunProg
		makeSignalChanFn = origMakeSig
	}()

	// Inject a signal channel we control (no real OS signals involved).
	sigCh := make(chan os.Signal, 1)
	makeSignalChanFn = func() chan os.Signal { return sigCh }

	// Inject program.Run to return an error immediately.
	runTeaProgramFn = func(_ *tea.Program) (tea.Model, error) {
		return nil, errors.New("fake TUI error")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Skip("config unavailable:", err)
	}
	p := proxy.New(cfg)
	progCh := make(chan *tea.Program, 1)

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		runTUIAfterStart(p, progCh)
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "TUI error") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestRunTUIAfterStart_signalPath covers the signal goroutine body in runTUIAfterStart:
// when a signal fires, Shutdown is called and exitFn(0) is invoked.
func TestRunTUIAfterStart_signalPath(t *testing.T) {
	origRunProg := runTeaProgramFn
	origMakeSig := makeSignalChanFn
	defer func() {
		runTeaProgramFn = origRunProg
		makeSignalChanFn = origMakeSig
	}()

	// Inject a signal channel we control.
	sigCh := make(chan os.Signal, 1)
	makeSignalChanFn = func() chan os.Signal { return sigCh }

	// exitFn fires in a goroutine - use channel-based capture + runtime.Goexit().
	exitCh := make(chan int, 1)
	blockCh := make(chan struct{}) // blocks runTeaProgramFn until signal is processed
	origExit := exitFn
	exitFn = func(code int) {
		exitCh <- code
		close(blockCh) // unblock the fake program.Run so runTUIAfterStart can finish
		runtime.Goexit()
	}
	defer func() { exitFn = origExit }()

	runTeaProgramFn = func(_ *tea.Program) (tea.Model, error) {
		<-blockCh // block until signal handler fires and closes blockCh
		return nil, nil
	}

	cfg, err := config.Load()
	if err != nil {
		t.Skip("config unavailable:", err)
	}
	p := proxy.New(cfg)
	progCh := make(chan *tea.Program, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runTUIAfterStart(p, progCh)
	}()

	// Give runTUIAfterStart time to reach program.Run (and the signal goroutine to start).
	time.Sleep(10 * time.Millisecond)

	// Trigger the signal goroutine.
	sigCh <- syscall.SIGTERM

	select {
	case code := <-exitCh:
		if code != 0 {
			t.Fatalf("want exit 0, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for signal handler exit")
	}

	wg.Wait()
}

// TestMakeSignalChanFn_default covers the default makeSignalChanFn closure body:
// it creates a channel and registers it with signal.Notify.
func TestMakeSignalChanFn_default(t *testing.T) {
	ch := makeSignalChanFn()
	if ch == nil {
		t.Fatal("makeSignalChanFn: expected non-nil channel")
	}
	signal.Stop(ch)
}

// TestApplyTUIFlags verifies that CLI flags correctly override config values (spec §13.3).
func TestApplyTUIFlags(t *testing.T) {
	t.Parallel()

	base := func() *config.Config {
		cfg := config.Defaults()
		cfg.Proxy.ListenPort = 8080
		cfg.Compression.SlidingWindow = 20
		cfg.Compression.Layer1Enabled = true
		cfg.Compression.Layer2Enabled = true
		cfg.Compression.Layer3Enabled = true
		cfg.Logging.Level = "info"
		return cfg
	}

	t.Run("port", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "9999"})
		if cfg.Proxy.ListenPort != 9999 {
			t.Fatalf("port = %d, want 9999", cfg.Proxy.ListenPort)
		}
	})

	t.Run("port_alias", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"-port", "7777"})
		if cfg.Proxy.ListenPort != 7777 {
			t.Fatalf("port = %d, want 7777", cfg.Proxy.ListenPort)
		}
	})

	t.Run("sliding_window", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--sliding-window", "5"})
		if cfg.Compression.SlidingWindow != 5 {
			t.Fatalf("sliding_window = %d, want 5", cfg.Compression.SlidingWindow)
		}
	})

	t.Run("no_layer1", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--no-layer1"})
		if cfg.Compression.Layer1Enabled {
			t.Fatal("expected Layer1Enabled=false")
		}
		if !cfg.Compression.Layer2Enabled || !cfg.Compression.Layer3Enabled {
			t.Fatal("other layers should be unaffected")
		}
	})

	t.Run("no_layer2", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--no-layer2"})
		if cfg.Compression.Layer2Enabled {
			t.Fatal("expected Layer2Enabled=false")
		}
		if !cfg.Compression.Layer1Enabled || !cfg.Compression.Layer3Enabled {
			t.Fatal("other layers should be unaffected")
		}
	})

	t.Run("no_layer3", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--no-layer3"})
		if cfg.Compression.Layer3Enabled {
			t.Fatal("expected Layer3Enabled=false")
		}
	})

	t.Run("log_level", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--log-level", "debug"})
		if cfg.Logging.Level != "debug" {
			t.Fatalf("log level = %q, want debug", cfg.Logging.Level)
		}
	})

	t.Run("combined_flags", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "1234", "--no-layer2", "--log-level", "warn", "--sliding-window", "3"})
		if cfg.Proxy.ListenPort != 1234 {
			t.Fatalf("port = %d, want 1234", cfg.Proxy.ListenPort)
		}
		if cfg.Compression.Layer2Enabled {
			t.Fatal("expected Layer2Enabled=false")
		}
		if cfg.Logging.Level != "warn" {
			t.Fatalf("log level = %q, want warn", cfg.Logging.Level)
		}
		if cfg.Compression.SlidingWindow != 3 {
			t.Fatalf("sliding_window = %d, want 3", cfg.Compression.SlidingWindow)
		}
	})

	t.Run("unknown_flags_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		// Unknown flags must not panic or change known values.
		applyTUIFlags(cfg, []string{"--unknown-flag", "value", "--another"})
		if cfg.Proxy.ListenPort != 8080 {
			t.Fatalf("port changed unexpectedly: %d", cfg.Proxy.ListenPort)
		}
	})

	t.Run("zero_port_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "0"})
		if cfg.Proxy.ListenPort != 8080 {
			t.Fatalf("port should not change to 0: %d", cfg.Proxy.ListenPort)
		}
	})

	t.Run("non_numeric_port_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "notanumber"})
		if cfg.Proxy.ListenPort != 8080 {
			t.Fatalf("port should not change for non-numeric: %d", cfg.Proxy.ListenPort)
		}
	})

	t.Run("missing_argument_flags_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--port", "--sliding-window", "--log-level"})
		if cfg.Proxy.ListenPort != 8080 || cfg.Compression.SlidingWindow != 20 || cfg.Logging.Level != "info" {
			t.Fatalf("unexpected config after missing args: %+v", cfg)
		}
	})

	t.Run("invalid_sliding_window_ignored", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		applyTUIFlags(cfg, []string{"--sliding-window", "0"})
		if cfg.Compression.SlidingWindow != 20 {
			t.Fatalf("sliding window should stay unchanged: %d", cfg.Compression.SlidingWindow)
		}
	})
}

func TestHandlePostToolCmd(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origGetwd := osGetwd
	origConfigLoad := configLoadFn
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		osGetwd = origGetwd
		configLoadFn = origConfigLoad
	}()

	t.Run("usage_on_unexpected_arg", func(t *testing.T) {
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd([]string{"bad"}) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), "usage: slimference posttool") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
	})

	t.Run("usage_when_terminal", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return true }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd(nil) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), "usage: slimference posttool") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		termIsTerminalFn = origTerm
	})

	t.Run("stdin_read_error", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return nil, errors.New("read fail") }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd(nil) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), "read stdin: read fail") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		readStdinAll = origRead
	})

	t.Run("json_parse_error", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return []byte("{"), nil }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd(nil) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), "filter: JSON") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		readStdinAll = origRead
	})

	t.Run("missing_tool_response_error", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return []byte(`{"command":"git status"}`), nil }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handlePostToolCmd([]string{"--"}) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), `no string field "tool_response"`) {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		readStdinAll = origRead
	})

	t.Run("no_change_emits_nothing", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"command":"echo hi","tool_response":"short output"}`), nil
		}
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 500
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "/tmp", nil }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("expected no stdout when output unchanged, got %q", buf.String())
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})

	t.Run("compacted_output_emits_hook_json", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"command":       "git status",
			"tool_response": strings.Repeat("line\n", 300),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) {
			return payload, nil
		}
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 40
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "", errors.New("no wd") }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd(nil)
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `"hookEventName":"PostToolUse"`) || !strings.Contains(out, `Slimference compacted Bash output for \"git status\"`) || !strings.Contains(out, `[slimference: truncated to 40 characters]`) {
			t.Fatalf("unexpected stdout: %q", out)
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})

	t.Run("compacted_output_without_command_uses_generic_context", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"tool_response": strings.Repeat("line\n", 300),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 40
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handlePostToolCmd([]string{"--"})
		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `Slimference compacted Bash output.`) || strings.Contains(out, `for \"`) {
			t.Fatalf("unexpected stdout: %q", out)
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
	})

	t.Run("encode_error_exits", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		payload, err := json.Marshal(map[string]string{
			"command":       "git status",
			"tool_response": strings.Repeat("line\n", 300),
		})
		if err != nil {
			t.Fatal(err)
		}
		readStdinAll = func() ([]byte, error) { return payload, nil }
		cfg := config.Defaults()
		cfg.Filter.PassthroughMaxChars = 40
		configLoadFn = func() (*config.Config, error) { return cfg, nil }
		osGetwd = func() (string, error) { return "/tmp", nil }

		oldStdout := os.Stdout
		oldStderr := os.Stderr
		rp, wp, _ := os.Pipe()
		_ = wp.Close()
		errR, errW, _ := os.Pipe()
		os.Stdout = wp
		os.Stderr = errW
		code, exited := captureExit(func() { handlePostToolCmd(nil) })
		_ = errW.Close()
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, errR)
		_ = rp.Close()
		if !exited || code != 1 || !strings.Contains(buf.String(), "encode posttool output") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
		readStdinAll = origRead
		configLoadFn = origConfigLoad
		osGetwd = origGetwd
	})
}

func TestHandleSubcommand_PostToolDispatch(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origConfigLoad := configLoadFn
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		configLoadFn = origConfigLoad
	}()

	termIsTerminalFn = func(int) bool { return false }
	payload, err := json.Marshal(map[string]string{
		"command":       "git status",
		"tool_response": strings.Repeat("x", 500),
	})
	if err != nil {
		t.Fatal(err)
	}
	readStdinAll = func() ([]byte, error) {
		return payload, nil
	}
	cfg := config.Defaults()
	cfg.Filter.PassthroughMaxChars = 20
	configLoadFn = func() (*config.Config, error) { return cfg, nil }

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"posttool"})
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"hookEventName":"PostToolUse"`) {
		t.Fatalf("unexpected stdout: %q", buf.String())
	}
}

func TestHandleSubcommand_DaemonAndServiceDispatch(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origRunDaemon := daemonRunFn
	origStop := daemonStopFn
	origInstall := daemonInstallLaunchdFn
	origUninstall := daemonUninstallFn
	origFormatStatus := daemonFormatStatusFn
	origExecutable := osExecutable
	origStartProcess := osStartProcess
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonRunFn = origRunDaemon
		daemonStopFn = origStop
		daemonInstallLaunchdFn = origInstall
		daemonUninstallFn = origUninstall
		daemonFormatStatusFn = origFormatStatus
		osExecutable = origExecutable
		osStartProcess = origStartProcess
	}()

	daemonRunFn = func(func() (int, func(context.Context) error, error)) error { return nil }
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	daemonStopFn = func() error { return nil }
	daemonInstallLaunchdFn = func(string) error { return nil }
	daemonUninstallFn = func() error { return nil }
	daemonFormatStatusFn = func() ([]byte, error) { return []byte(`{"running":false}`), nil }
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	osStartProcess = func(string, []string, *os.ProcAttr) (*os.Process, error) {
		return os.FindProcess(os.Getpid())
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"daemon"})
	handleSubcommand([]string{"start"})
	handleSubcommand([]string{"stop"})
	handleSubcommand([]string{"restart"})
	handleSubcommand([]string{"service", "install"})
	handleSubcommand([]string{"service", "uninstall"})
	handleSubcommand([]string{"service", "status"})
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference daemon started.") || !strings.Contains(out, "Service installed.") || !strings.Contains(out, `"running":false`) {
		t.Fatalf("unexpected stdout: %q", out)
	}
}

func TestSetupLogging_FallbackTextAndInfoLevel(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "text"
	cfg.Logging.File = filepath.Join("/no/such", "dir", "slimference.log")
	setupLogging(cfg)
}

func TestSetupLogging_FallbackJSON(t *testing.T) {
	cfg := config.Defaults()
	cfg.Logging.Level = "warn"
	cfg.Logging.Format = "json"
	cfg.Logging.File = filepath.Join("/no/such", "dir", "slimference.log")
	setupLogging(cfg)
}

func TestServiceControlAdapter_StartDaemon(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origExecutable := osExecutable
	origStartProcess := osStartProcess
	defer func() {
		daemonIsRunningFn = origIsRunning
		osExecutable = origExecutable
		osStartProcess = origStartProcess
	}()

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	started := false
	osStartProcess = func(name string, argv []string, attr *os.ProcAttr) (*os.Process, error) {
		started = true
		if name != "/tmp/slimference" || len(argv) != 2 || argv[1] != "daemon" || attr == nil {
			t.Fatalf("unexpected start args: name=%q argv=%v attr=%#v", name, argv, attr)
		}
		return os.FindProcess(os.Getpid())
	}

	if err := (&serviceControlAdapter{}).StartDaemon(); err != nil {
		t.Fatalf("StartDaemon: %v", err)
	}
	if !started {
		t.Fatal("expected osStartProcess to be called")
	}
}

func TestServiceControlAdapter_StartDaemonErrors(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origExecutable := osExecutable
	origStartProcess := osStartProcess
	defer func() {
		daemonIsRunningFn = origIsRunning
		osExecutable = origExecutable
		osStartProcess = origStartProcess
	}()

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 42, Port: 8990}, nil
	}
	if err := (&serviceControlAdapter{}).StartDaemon(); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected already running error, got %v", err)
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	if err := (&serviceControlAdapter{}).StartDaemon(); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("expected executable error, got %v", err)
	}

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	osStartProcess = func(string, []string, *os.ProcAttr) (*os.Process, error) {
		return nil, errors.New("boom")
	}
	if err := (&serviceControlAdapter{}).StartDaemon(); err == nil || !strings.Contains(err.Error(), "start daemon") {
		t.Fatalf("expected start daemon error, got %v", err)
	}
}

func TestServiceControlAdapter_StopRestartInstallUninstallAndStatus(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origStop := daemonStopFn
	origInstall := daemonInstallLaunchdFn
	origUninstall := daemonUninstallFn
	origExecutable := osExecutable
	origStartProcess := osStartProcess
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonStopFn = origStop
		daemonInstallLaunchdFn = origInstall
		daemonUninstallFn = origUninstall
		osExecutable = origExecutable
		osStartProcess = origStartProcess
	}()

	stopCalls := 0
	daemonStopFn = func() error {
		stopCalls++
		return nil
	}
	if err := (&serviceControlAdapter{}).StopDaemon(); err != nil {
		t.Fatalf("StopDaemon: %v", err)
	}

	isRunningChecks := 0
	startCalls := 0
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		isRunningChecks++
		if isRunningChecks == 1 {
			return true, &daemon.PIDFile{PID: 42, Port: 8990}, nil
		}
		return false, nil, nil
	}
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	osStartProcess = func(string, []string, *os.ProcAttr) (*os.Process, error) {
		startCalls++
		return os.FindProcess(os.Getpid())
	}
	if err := (&serviceControlAdapter{}).RestartDaemon(); err != nil {
		t.Fatalf("RestartDaemon: %v", err)
	}
	if stopCalls != 2 || startCalls != 1 {
		t.Fatalf("unexpected stop/start calls: stop=%d start=%d", stopCalls, startCalls)
	}

	daemonInstallLaunchdFn = func(binary string) error {
		if binary != "/tmp/slimference" {
			t.Fatalf("unexpected binary: %q", binary)
		}
		return nil
	}
	if err := (&serviceControlAdapter{}).InstallService(); err != nil {
		t.Fatalf("InstallService: %v", err)
	}

	daemonUninstallFn = func() error { return nil }
	if err := (&serviceControlAdapter{}).UninstallService(); err != nil {
		t.Fatalf("UninstallService: %v", err)
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 7, Port: 8123}, nil
	}
	running, pid, port := (&serviceControlAdapter{}).DaemonStatus()
	if !running || pid != 7 || port != 8123 {
		t.Fatalf("DaemonStatus: running=%v pid=%d port=%d", running, pid, port)
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	running, pid, port = (&serviceControlAdapter{}).DaemonStatus()
	if running || pid != 0 || port != 0 {
		t.Fatalf("DaemonStatus false case: running=%v pid=%d port=%d", running, pid, port)
	}
}

func TestServiceControlAdapter_RestartAndInstallServiceErrors(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origStop := daemonStopFn
	origExecutable := osExecutable
	origInstall := daemonInstallLaunchdFn
	origStartProcess := osStartProcess
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonStopFn = origStop
		osExecutable = origExecutable
		daemonInstallLaunchdFn = origInstall
		osStartProcess = origStartProcess
	}()

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 42, Port: 8990}, nil
	}
	daemonStopFn = func() error { return errors.New("stop failed") }
	if err := (&serviceControlAdapter{}).RestartDaemon(); err == nil || !strings.Contains(err.Error(), "stop failed") {
		t.Fatalf("expected restart stop error, got %v", err)
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startCalls := 0
	osStartProcess = func(string, []string, *os.ProcAttr) (*os.Process, error) {
		startCalls++
		return os.FindProcess(os.Getpid())
	}
	if err := (&serviceControlAdapter{}).RestartDaemon(); err != nil {
		t.Fatalf("RestartDaemon no-running path: %v", err)
	}
	if startCalls != 1 {
		t.Fatalf("expected one start call, got %d", startCalls)
	}

	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	if err := (&serviceControlAdapter{}).InstallService(); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("expected install service executable error, got %v", err)
	}

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	daemonInstallLaunchdFn = func(string) error { return errors.New("install failed") }
	if err := (&serviceControlAdapter{}).InstallService(); err == nil || !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("expected install launchd error, got %v", err)
	}
}

func TestServiceControlAdapter_HookOperations(t *testing.T) {
	origHome := osUserHomeDir
	origConfigLoad := configLoadFn
	origInstallClaude := installClaudeHookFn
	origInstallCodex := installCodexHookFn
	origRemoveClaude := removeClaudeHookFn
	origRemoveCodex := removeCodexHookFn
	defer func() {
		osUserHomeDir = origHome
		configLoadFn = origConfigLoad
		installClaudeHookFn = origInstallClaude
		installCodexHookFn = origInstallCodex
		removeClaudeHookFn = origRemoveClaude
		removeCodexHookFn = origRemoveCodex
	}()

	osUserHomeDir = func() (string, error) { return "/tmp/home", nil }
	cfg := config.Defaults()
	cfg.Hooks.SlimferenceCommand = "/custom/slimference"
	configLoadFn = func() (*config.Config, error) { return cfg, nil }

	installClaudeCalled := false
	installClaudeHookFn = func(home, cmd string) error {
		installClaudeCalled = true
		if home != "/tmp/home" || cmd != "/custom/slimference" {
			t.Fatalf("unexpected claude args: home=%q cmd=%q", home, cmd)
		}
		return nil
	}
	installCodexHookFn = func(home, cmd string) error {
		if home != "/tmp/home" || cmd != "/custom/slimference" {
			t.Fatalf("unexpected codex args: home=%q cmd=%q", home, cmd)
		}
		return nil
	}
	if err := (&serviceControlAdapter{}).InstallHook("claude"); err != nil {
		t.Fatalf("InstallHook claude: %v", err)
	}
	if !installClaudeCalled {
		t.Fatal("expected claude installer call")
	}
	if err := (&serviceControlAdapter{}).InstallHook("codex"); err != nil {
		t.Fatalf("InstallHook codex: %v", err)
	}
	if err := (&serviceControlAdapter{}).InstallHook("nope"); err == nil {
		t.Fatal("expected unknown hook target error")
	}

	removeClaudeHookFn = func(home string) error {
		if home != "/tmp/home" {
			t.Fatalf("unexpected remove claude home: %q", home)
		}
		return nil
	}
	removeCodexHookFn = func(home string) error {
		if home != "/tmp/home" {
			t.Fatalf("unexpected remove codex home: %q", home)
		}
		return nil
	}
	if err := (&serviceControlAdapter{}).RemoveHook("claude"); err != nil {
		t.Fatalf("RemoveHook claude: %v", err)
	}
	if err := (&serviceControlAdapter{}).RemoveHook("codex"); err != nil {
		t.Fatalf("RemoveHook codex: %v", err)
	}
	if err := (&serviceControlAdapter{}).RemoveHook("nope"); err == nil {
		t.Fatal("expected unknown remove hook target error")
	}

	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	if err := (&serviceControlAdapter{}).InstallHook("claude"); err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("expected home error, got %v", err)
	}
	if err := (&serviceControlAdapter{}).RemoveHook("claude"); err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("expected home error, got %v", err)
	}
}

func TestHandleDaemonStartStopRestartAndServiceCommands(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origRunDaemon := daemonRunFn
	origStop := daemonStopFn
	origInstall := daemonInstallLaunchdFn
	origUninstall := daemonUninstallFn
	origFormatStatus := daemonFormatStatusFn
	origExecutable := osExecutable
	origStartProcess := osStartProcess
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonRunFn = origRunDaemon
		daemonStopFn = origStop
		daemonInstallLaunchdFn = origInstall
		daemonUninstallFn = origUninstall
		daemonFormatStatusFn = origFormatStatus
		osExecutable = origExecutable
		osStartProcess = origStartProcess
	}()

	rp, cleanup := redirectStderr()
	daemonRunFn = func(func() (int, func(context.Context) error, error)) error {
		return errors.New("daemon fail")
	}
	code, exited := captureExit(handleDaemonCmd)
	cleanup()
	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "daemon fail") {
		t.Fatalf("handleDaemonCmd: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return false, nil, nil
	}
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	startCalls := 0
	osStartProcess = func(name string, argv []string, attr *os.ProcAttr) (*os.Process, error) {
		startCalls++
		return os.FindProcess(os.Getpid())
	}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleStartCmd()
	_ = w.Close()
	os.Stdout = oldStdout
	if startCalls != 1 {
		t.Fatalf("expected one start call, got %d", startCalls)
	}

	daemonStopCalls := 0
	daemonStopFn = func() error {
		daemonStopCalls++
		return nil
	}
	handleStopCmd()
	restartChecks := 0
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		restartChecks++
		if restartChecks == 1 {
			return true, &daemon.PIDFile{PID: 1, Port: 2}, nil
		}
		return false, nil, nil
	}
	handleRestartCmd()
	if daemonStopCalls != 2 {
		t.Fatalf("expected stop to be called twice, got %d", daemonStopCalls)
	}

	daemonInstallLaunchdFn = func(binary string) error {
		if binary != "/tmp/slimference" {
			t.Fatalf("unexpected install binary: %q", binary)
		}
		return nil
	}
	daemonUninstallFn = func() error { return nil }
	daemonFormatStatusFn = func() ([]byte, error) { return []byte(`{"running":true}`), nil }
	handleServiceCmd([]string{"install"})
	handleServiceCmd([]string{"uninstall"})
	oldStdout = os.Stdout
	r, w, _ = os.Pipe()
	os.Stdout = w
	handleServiceCmd([]string{"status"})
	_ = w.Close()
	os.Stdout = oldStdout
	var outBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, r)
	if !strings.Contains(outBuf.String(), `"running":true`) {
		t.Fatalf("status stdout: %q", outBuf.String())
	}
}

func TestHandleStartStopRestartAndServiceCommandErrors(t *testing.T) {
	origIsRunning := daemonIsRunningFn
	origStop := daemonStopFn
	origInstall := daemonInstallLaunchdFn
	origUninstall := daemonUninstallFn
	origFormatStatus := daemonFormatStatusFn
	origExecutable := osExecutable
	origStartProcess := osStartProcess
	defer func() {
		daemonIsRunningFn = origIsRunning
		daemonStopFn = origStop
		daemonInstallLaunchdFn = origInstall
		daemonUninstallFn = origUninstall
		daemonFormatStatusFn = origFormatStatus
		osExecutable = origExecutable
		osStartProcess = origStartProcess
	}()

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return false, nil, errors.New("check fail")
	}
	rp, cleanup := redirectStderr()
	code, exited := captureExit(handleStartCmd)
	cleanup()
	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "check daemon") {
		t.Fatalf("handleStartCmd check error: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 9, Port: 8990}, nil
	}
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleStartCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "already running") {
		t.Fatalf("handleStartCmd already running: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleStartCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "executable") {
		t.Fatalf("handleStartCmd executable: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	osStartProcess = func(string, []string, *os.ProcAttr) (*os.Process, error) {
		return nil, errors.New("spawn fail")
	}
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleStartCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "start daemon") {
		t.Fatalf("handleStartCmd spawn: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonStopFn = func() error { return errors.New("stop fail") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleStopCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "stop fail") {
		t.Fatalf("handleStopCmd: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 1, Port: 2}, nil
	}
	rp, cleanup = redirectStderr()
	code, exited = captureExit(handleRestartCmd)
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "stop: stop fail") {
		t.Fatalf("handleRestartCmd: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd(nil) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "usage: slimference service") {
		t.Fatalf("handleServiceCmd usage: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"install"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "executable") {
		t.Fatalf("handleServiceCmd install executable: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	daemonInstallLaunchdFn = func(string) error { return errors.New("install fail") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"install"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "install fail") {
		t.Fatalf("handleServiceCmd install fail: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonUninstallFn = func() error { return errors.New("uninstall fail") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"uninstall"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "uninstall fail") {
		t.Fatalf("handleServiceCmd uninstall fail: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	daemonFormatStatusFn = func() ([]byte, error) { return nil, errors.New("status fail") }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"status"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "status fail") {
		t.Fatalf("handleServiceCmd status fail: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}

	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleServiceCmd([]string{"nope"}) })
	cleanup()
	errBuf.Reset()
	_, _ = io.Copy(&errBuf, rp)
	if !exited || code != 1 || !strings.Contains(errBuf.String(), "unknown service command") {
		t.Fatalf("handleServiceCmd unknown: exited=%v code=%d stderr=%q", exited, code, errBuf.String())
	}
}

func TestStartProxyForDaemon(t *testing.T) {
	origConfigLoad := configLoadFn
	origStartProxyFn := startProxyFn
	defer func() {
		configLoadFn = origConfigLoad
		startProxyFn = origStartProxyFn
	}()

	configLoadFn = func() (*config.Config, error) {
		return nil, errors.New("config failed")
	}
	if _, _, err := startProxyForDaemon(); err == nil || !strings.Contains(err.Error(), "config load") {
		t.Fatalf("expected config load error, got %v", err)
	}

	cfg := config.Defaults()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	startProxyFn = func(*config.Config) (func(context.Context) error, error) {
		return nil, errors.New("proxy failed")
	}
	_, _, err := startProxyForDaemon()
	if err == nil || !strings.Contains(err.Error(), "proxy start") {
		t.Fatalf("expected proxy start error, got %v", err)
	}

	cfg.Proxy.ListenPort = 7777
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	startProxyFn = func(got *config.Config) (func(context.Context) error, error) {
		if got != cfg {
			t.Fatal("startProxyFn should receive loaded config")
		}
		return func(context.Context) error { return nil }, nil
	}
	port, shutdown, err := startProxyForDaemon()
	if err != nil {
		t.Fatalf("startProxyForDaemon success: %v", err)
	}
	if port != cfg.Proxy.ListenPort || shutdown == nil {
		t.Fatalf("unexpected return values: port=%d shutdown_nil=%v", port, shutdown == nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

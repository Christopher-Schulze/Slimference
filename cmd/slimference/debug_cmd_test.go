package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

func isolateDebugNoConfig(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	return tmp
}

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

	p, _, err = parseDebugPeriodArgs([]string{"", "week"})
	if err != nil || p != "week" {
		t.Fatalf("empty arg skip: got period=%q err=%v", p, err)
	}

	p, _, err = parseDebugPeriodArgs(nil)
	if err != nil || p != "today" {
		t.Fatalf("default period: got %q err=%v", p, err)
	}
}

func TestParseDebugFlightExportArgsEdges(t *testing.T) {
	path, csvOut, err := parseDebugFlightExportArgs([]string{"out.csv"})
	if err != nil || path != "out.csv" || !csvOut {
		t.Fatalf("csv suffix: path=%q csv=%v err=%v", path, csvOut, err)
	}
	path, csvOut, err = parseDebugFlightExportArgs([]string{"", "--csv", "out.jsonl"})
	if err != nil || path != "out.jsonl" || !csvOut {
		t.Fatalf("flag csv: path=%q csv=%v err=%v", path, csvOut, err)
	}
	if _, _, err := parseDebugFlightExportArgs([]string{"--bad"}); err == nil {
		t.Fatal("expected bad flag error")
	}
	if _, _, err := parseDebugFlightExportArgs([]string{"one.jsonl", "two.jsonl"}); err == nil {
		t.Fatal("expected extra arg error")
	}
	if _, _, err := parseDebugFlightExportArgs(nil); err == nil {
		t.Fatal("expected usage error")
	}
}

func TestParseDebugBundleArgs(t *testing.T) {
	opts, err := parseDebugBundleArgs([]string{
		"--out", "~/bundle",
		"--flight-limit=999",
		"--filter-limit", "2",
		"--log-lines=2000",
		"--admin-url", "http://127.0.0.1:8990/state",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(opts.OutDir, "bundle") || opts.FlightLimit != 500 || opts.FilterLimit != 2 || opts.LogLines != 1000 {
		t.Fatalf("unexpected opts: %+v", opts)
	}
	if opts.AdminURL != "http://127.0.0.1:8990/state" {
		t.Fatalf("admin url: %q", opts.AdminURL)
	}
	for _, args := range [][]string{
		{"--flight-limit=0"},
		{"--filter-limit", "nope"},
		{"--log-lines"},
		{"--bad"},
		{"extra"},
	} {
		if _, err := parseDebugBundleArgs(args); err == nil {
			t.Fatalf("expected error for %v", args)
		}
	}
	help, err := parseDebugBundleArgs([]string{"--help"})
	if err != nil || !help.Help {
		t.Fatalf("help opts=%+v err=%v", help, err)
	}
}

func TestDefaultDebugBundleAdminURLNormalizesWildcardHosts(t *testing.T) {
	cfg := &config.Config{Proxy: config.ProxyConfig{ListenAddress: "0.0.0.0", ListenPort: 8990}}
	if got := defaultDebugBundleAdminURL(cfg); got != "http://127.0.0.1:8990/_slimference/admin/state" {
		t.Fatalf("wildcard admin URL = %q", got)
	}
	cfg.Proxy.ListenAddress = "::1"
	if got := defaultDebugBundleAdminURL(cfg); got != "http://[::1]:8990/_slimference/admin/state" {
		t.Fatalf("ipv6 admin URL = %q", got)
	}
}

func TestHandleDebugBundleWritesBoundedContentFreeBundle(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	t.Setenv("HOME", home)
	decisionLog := filepath.Join(tmp, "decisions.jsonl")
	filterDB := filepath.Join(tmp, "filter.db")
	analyticsDir := filepath.Join(tmp, "analytics")
	cfgPath := filepath.Join(tmp, "config.toml")
	cfg := fmt.Sprintf(`
[proxy]
listen_address = "127.0.0.1"
listen_port = 8990
[analytics]
log_dir = %q
[filter]
filter_db = %q
[debug]
decisions_log = %q
`, analyticsDir, filterDB, decisionLog)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	if err := os.MkdirAll(analyticsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := filter.OpenDB(filterDB)
	if err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterRun(db, "cat /secret/path.txt", "/Users/example/sensitive-project", 1000, 100, 90, time.Now()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	line, err := json.Marshal(dbg.RequestSummary{
		RequestID: "req-1",
		Timestamp: time.Now(),
		Source:    "proxy",
		Provider:  "codex_chatgpt",
		Model:     "gpt-test",
		Tokens:    dbg.TokenCounts{Original: 100, Final: 40, Saved: 60},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decisionLog, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_slimference/admin/state" {
			t.Fatalf("unexpected admin path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"service":"slimference","savings":{"product":{"status":"saving"}}}`))
	}))
	defer server.Close()
	origStdoutLog := daemonStdoutLogPathFn
	origStderrLog := daemonStderrLogPathFn
	origReadLogs := daemonReadRecentLogLinesFn
	defer func() {
		daemonStdoutLogPathFn = origStdoutLog
		daemonStderrLogPathFn = origStderrLog
		daemonReadRecentLogLinesFn = origReadLogs
	}()
	daemonStdoutLogPathFn = func() string { return filepath.Join(tmp, "stdout.log") }
	daemonStderrLogPathFn = func() string { return filepath.Join(tmp, "stderr.log") }
	daemonReadRecentLogLinesFn = func(path string, lines int, _ time.Time) ([]string, error) {
		if lines != 1 {
			t.Fatalf("log line cap not honored: %d", lines)
		}
		return []string{"level=info msg=ok"}, nil
	}
	outDir := filepath.Join(tmp, "bundle")
	handleDebugBundle([]string{
		"--out", outDir,
		"--admin-url", server.URL + "/_slimference/admin/state",
		"--flight-limit", "1",
		"--filter-limit", "1",
		"--log-lines", "1",
	})
	for _, name := range []string{
		"manifest.json",
		"paths.json",
		"admin-state.json",
		"savings-today.json",
		"decisions-tail.json",
		"flight-tail.json",
		"filter-tail.json",
		"daemon-stdout-tail.json",
		"daemon-stderr-tail.json",
	} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	filterData, err := os.ReadFile(filepath.Join(outDir, "filter-tail.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(filterData), "secret") || strings.Contains(string(filterData), "private-project") {
		t.Fatalf("filter bundle leaked raw command/project: %s", string(filterData))
	}
	if !strings.Contains(string(filterData), "command_hash") || !strings.Contains(string(filterData), "project_hash") {
		t.Fatalf("filter bundle missing hashes: %s", string(filterData))
	}
	var manifest debugBundleManifest
	manifestData, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != debugBundleSchemaVersion || len(manifest.Files) < 8 {
		t.Fatalf("bad manifest: %+v", manifest)
	}
}

func TestWriteDebugBundleMissingSourcesStillWritesBundle(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	origStdoutLog := daemonStdoutLogPathFn
	origStderrLog := daemonStderrLogPathFn
	origReadLogs := daemonReadRecentLogLinesFn
	defer func() {
		daemonStdoutLogPathFn = origStdoutLog
		daemonStderrLogPathFn = origStderrLog
		daemonReadRecentLogLinesFn = origReadLogs
	}()
	daemonStdoutLogPathFn = func() string { return filepath.Join(tmp, "missing-stdout.log") }
	daemonStderrLogPathFn = func() string { return filepath.Join(tmp, "missing-stderr.log") }
	daemonReadRecentLogLinesFn = func(path string, lines int, _ time.Time) ([]string, error) {
		return nil, os.ErrNotExist
	}
	cfg := &config.Config{
		Proxy:     config.ProxyConfig{ListenAddress: "127.0.0.1", ListenPort: 8990},
		Analytics: config.AnalyticsConfig{LogDir: filepath.Join(tmp, "missing-analytics")},
	}
	outDir := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := writeDebugBundle(outDir, cfg, config.LoadInfo{Source: "test"}, debugBundleOptions{
		FlightLimit: 2,
		FilterLimit: 2,
		LogLines:    2,
		AdminURL:    "://bad-url",
	}, time.Now())
	for _, name := range []string{
		"paths.json",
		"admin-state-error.txt",
		"savings-today.json",
		"decisions-tail.json",
		"flight-tail.json",
		"filter-tail.json",
		"daemon-stdout-tail.json",
		"daemon-stderr-tail.json",
	} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	missing := strings.Join(manifest.Missing, " ")
	for _, want := range []string{"admin-state", "decisions-log", "filter-db", "daemon-stdout-tail", "daemon-stderr-tail"} {
		if !strings.Contains(missing, want) {
			t.Fatalf("missing marker %q not found in %q", want, missing)
		}
	}
}

func TestWriteFlightCSVSkipsNilFlightAndWritesRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flights.csv")
	summaries := []dbg.RequestSummary{
		{RequestID: "nil-flight"},
		{
			RequestID: "req-1",
			Source:    "proxy",
			Provider:  "openai",
			Host:      "api.openai.com",
			Path:      "/v1/responses",
			Flight: &dbg.FlightRequestSummary{
				RequestID:  "req-1",
				Source:     "proxy",
				RouteMode:  "mitm",
				Provider:   "openai",
				Host:       "api.openai.com",
				Path:       "/v1/responses",
				Confidence: "measured",
			},
		},
	}
	if err := writeFlightCSV(path, summaries); err != nil {
		t.Fatalf("writeFlightCSV: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "request_id") || !strings.Contains(text, "req-1") {
		t.Fatalf("unexpected csv:\n%s", text)
	}
	if err := writeFlightCSV(t.TempDir(), summaries); err == nil {
		t.Fatal("expected write error when CSV path is a directory")
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

func TestHandleSubcommand_debugLast(t *testing.T) {
	isolateDebugNoConfig(t)
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
	isolateDebugNoConfig(t)
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

func TestHandleSubcommand_debugPaths_XDGConfigSource(t *testing.T) {
	tmp := t.TempDir()
	xdg := filepath.Join(tmp, "xdg")
	cfgDir := filepath.Join(xdg, "slimference")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[debug]
decisions_log = "/xdg/decisions.jsonl"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("SLIMFERENCE_CONFIG", "")
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
	if !strings.Contains(out, cfgPath+" [xdg]") {
		t.Fatalf("expected xdg config source, got: %q", out)
	}
	if !strings.Contains(out, "/xdg/decisions.jsonl [[debug] decisions_log]") {
		t.Fatalf("expected xdg decisions_log source, got: %q", out)
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

// TestHandleDebugLast_noRows covers handleDebugLast when the DB exists but has no rows
// (main.go:1041-1044).
func TestHandleDebugLast_noRows(t *testing.T) {
	isolateDebugNoConfig(t)
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

func TestHandleSubcommand_debugPaths_explicitConfigSource(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "explicit.toml")
	if err := os.WriteFile(cfgPath, []byte("[debug]\ndecisions_log = \"~/debug/decisions.jsonl\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := explicitConfigPath
	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = orig }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "paths"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "config file:      "+cfgPath+" [--config]") {
		t.Fatalf("expected explicit config source, got: %q", out)
	}
	if !strings.Contains(out, "[debug] decisions_log") {
		t.Fatalf("expected decisions log source, got: %q", out)
	}
}

func TestDebugConfigSourceLabel(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"flag":     "--config",
		"env":      "SLIMFERENCE_CONFIG",
		"":         "defaults",
		"xdg":      "xdg",
		"defaults": "defaults",
	}
	for in, want := range tests {
		if got := debugConfigSourceLabel(in); got != want {
			t.Fatalf("debugConfigSourceLabel(%q) = %q, want %q", in, got, want)
		}
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
// tokens, layers, and layer1 breakdown.
func TestHandleDebugReplay_fullOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")

	content := `{"req_id":"r1","provider":"anthropic","model":"claude-3","layers_applied":[1],"tokens":{"original":1000,"final":800,"saved":200,"ratio":0.8},"layer1_breakdown":{"ansi_strip":{"blocks":3,"saved":200}}}
{"req_id":"r2","provider":"openai","model":"gpt-4","layers_applied":[1],"tokens":{"original":2000,"final":1400,"saved":600,"ratio":0.7}}
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
	if !strings.Contains(out, "TOTAL: 2 request(s)  800 tokens saved") {
		t.Fatalf("missing total line: %q", out)
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
		isolateDebugNoConfig(t)
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
	isolateDebugNoConfig(t)
	t.Setenv("SLIMFERENCE_FILTER_DB", filepath.Join(t.TempDir(), "nonexistent.db"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugLast([]string{"3"})
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

func TestHandleDebugFlightCommands(t *testing.T) {
	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	content := `{"req_id":"req-flight","ts":"2024-01-01T00:00:00Z","source":"transparent_connect","provider":"openai","host":"chatgpt.com","path":"/backend-api/dev","route_mode":"mitm","tokens":{"original":300,"final":180,"saved":120,"ratio":0.6},"layers_applied":[1],"cache_read_tokens":90,"output_reduce":{"applied":true,"profile":"codex","reason":"applied","added_tokens":5}}` + "\n"
	if err := os.WriteFile(decisionsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugFlight([]string{"last"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Flight recorder") || !strings.Contains(buf.String(), "read=90") {
		t.Fatalf("flight last output: %q", buf.String())
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleDebugFlight([]string{"tail", "5", "--json"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"schema_version"`) || !strings.Contains(buf.String(), `"token_accounting"`) {
		t.Fatalf("flight tail json output: %q", buf.String())
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleDebugFlight([]string{"replay", decisionsPath})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "req-flight") {
		t.Fatalf("flight replay output: %q", buf.String())
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleDebugFlight([]string{"replay", decisionsPath, "--json"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"request_id": "req-flight"`) {
		t.Fatalf("flight replay json output: %q", buf.String())
	}

	outPath := filepath.Join(tmp, "flight.jsonl")
	r, w, _ = os.Pipe()
	os.Stdout = w
	handleDebugFlight([]string{"export", outPath})
	_ = w.Close()
	os.Stdout = old
	if data, err := os.ReadFile(outPath); err != nil || !strings.Contains(string(data), `"request_id":"req-flight"`) {
		t.Fatalf("flight export data=%q err=%v", string(data), err)
	}

	csvPath := filepath.Join(tmp, "flight.csv")
	r, w, _ = os.Pipe()
	os.Stdout = w
	handleDebugFlight([]string{"export", csvPath})
	_ = w.Close()
	os.Stdout = old
	if data, err := os.ReadFile(csvPath); err != nil || !strings.Contains(string(data), "request_id,source,route_mode") || !strings.Contains(string(data), "req-flight") {
		t.Fatalf("flight csv data=%q err=%v", string(data), err)
	}
}

func TestHandleDebugFlightNoConfiguredLogAndParseArgs(t *testing.T) {
	isolateDebugNoConfig(t)
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugFlight([]string{"last"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No decisions_log configured") {
		t.Fatalf("expected no decisions log message, got %q", buf.String())
	}

	if limit, jsonOut, err := parseDebugFlightArgs([]string{"", "600", "--json"}, 20); err != nil || limit != 500 || !jsonOut {
		t.Fatalf("parse flight args limit=%d json=%v err=%v", limit, jsonOut, err)
	}
	for _, args := range [][]string{{"--bad"}, {"1", "2"}, {"zero"}} {
		if _, _, err := parseDebugFlightArgs(args, 20); err == nil {
			t.Fatalf("expected parse error for %v", args)
		}
	}
}

func TestHandleDebugFlightErrorBranches(t *testing.T) {
	for _, args := range [][]string{nil, []string{"wat"}} {
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handleDebugFlight(args) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || buf.Len() == 0 {
			t.Fatalf("args=%v code=%d exited=%v stderr=%q", args, code, exited, buf.String())
		}
	}

	for _, run := range []func(){
		func() { handleDebugFlightLast([]string{"--bad"}) },
		func() { handleDebugFlightTail([]string{"--bad"}) },
		func() { handleDebugFlightReplay(nil) },
		func() { handleDebugFlightReplay([]string{"file", "--bad"}) },
		func() { handleDebugFlightExport(nil) },
		func() { handleDebugFlightExport([]string{"out.jsonl", "--bad"}) },
		func() { handleDebugFlightExport([]string{"a.jsonl", "b.jsonl"}) },
	} {
		rp, cleanup := redirectStderr()
		code, exited := captureExit(run)
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || buf.Len() == 0 {
			t.Fatalf("expected exit/stderr, code=%d exited=%v stderr=%q", code, exited, buf.String())
		}
	}
}

func TestHandleDebugFlightReplayAndExportErrors(t *testing.T) {
	origReplay := replaySessionFn
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) { return nil, errors.New("replay boom") }
	t.Cleanup(func() { replaySessionFn = origReplay })

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleDebugFlightReplay([]string{"file"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "replay boom") {
		t.Fatalf("replay error code=%d exited=%v stderr=%q", code, exited, buf.String())
	}

	tmp := t.TempDir()
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", filepath.Join(tmp, "decisions.jsonl"))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleDebugFlightExport([]string{filepath.Join(tmp, "out.jsonl")}) })
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "replay boom") {
		t.Fatalf("export replay error code=%d exited=%v stderr=%q", code, exited, buf.String())
	}
}

func TestHandleDebugFlightTailNoConfiguredLogAndExportWriteError(t *testing.T) {
	tmp := isolateDebugNoConfig(t)
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleDebugFlightTail(nil)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No decisions_log configured") {
		t.Fatalf("tail no log output=%q", buf.String())
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleDebugFlightExport([]string{filepath.Join(tmp, "out.jsonl")})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No decisions_log configured") {
		t.Fatalf("export no log output=%q", buf.String())
	}

	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	if err := os.WriteFile(decisionsPath, []byte(`{"req_id":"req-write","tokens":{"original":1,"final":1}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleDebugFlightExport([]string{tmp}) })
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "flight export") {
		t.Fatalf("export write error code=%d exited=%v stderr=%q", code, exited, buf.String())
	}

	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() { handleDebugFlightExport([]string{tmp, "--csv"}) })
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "flight export") {
		t.Fatalf("export csv write error code=%d exited=%v stderr=%q", code, exited, buf.String())
	}
}

func TestPrintFlightSummariesEmptyAndDispatch(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printFlightSummaries(nil, false)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No flight records") {
		t.Fatalf("empty flight output=%q", buf.String())
	}

	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	if err := os.WriteFile(decisionsPath, []byte(`{"req_id":"req-dispatch","tokens":{"original":2,"final":1,"saved":1}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"debug", "flight", "last", "--json"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "req-dispatch") {
		t.Fatalf("flight dispatch output=%q", buf.String())
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

	if len(buf.String()) == 0 {
		t.Errorf("want some JSON output, got empty string")
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
	isolateDebugNoConfig(t)

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
	isolateDebugNoConfig(t)

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
	isolateDebugNoConfig(t)

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

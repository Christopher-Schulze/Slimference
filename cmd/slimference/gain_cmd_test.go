package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/types"
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
	p, f, err = parseGainArgs([]string{"--opportunities", "--json", "week"})
	if err != nil || p != "week" || !f.opportunities || !f.json {
		t.Fatalf("opportunities: period=%q flags=%+v err=%v", p, f, err)
	}
	if _, _, err = parseGainArgs([]string{"--opportunities", "--by-command"}); err == nil {
		t.Fatal("expected invalid opportunities/by-command combination")
	}
	if _, _, err = parseGainArgs([]string{"--opportunities", "--cache"}); err == nil {
		t.Fatal("expected invalid opportunities/cache combination")
	}
	if _, _, err = parseGainArgs([]string{"--opportunities", "--project", "/repo"}); err == nil {
		t.Fatal("expected invalid opportunities/project combination")
	}
}

func TestHandleSubcommand_gain_opportunities(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := filter.RecordFilterObservation(db, commandOutputFirstObservationScope, "[command-output-first:ls] ls -lah generated", "/repo", 900, 900, commandOutputFirstObservationFullPass, now); err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterObservation(db, commandOutputFirstObservationScope, "[command-output-first:go] go test ./...", "/repo", 1200, 1200, commandOutputFirstObservationArchiveFail, now); err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterRun(db, "[command-output-first:git] git diff --stat", "/repo", 500, 100, 80, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", filepath.Join(t.TempDir(), "missing-decisions.jsonl"))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today", "--opportunities"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Savings opportunities") || !strings.Contains(out, "These rows do not count as savings") {
		t.Fatalf("opportunities stdout missing title/disclaimer: %q", out)
	}
	if !strings.Contains(out, "go test ./...") || !strings.Contains(out, "archive_unavailable") {
		t.Fatalf("opportunities stdout missing row: %q", out)
	}
	if strings.Contains(out, "git diff --stat") {
		t.Fatalf("filter_runs savings row leaked into opportunities: %q", out)
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"gain", "today", "--opportunities", "--json"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	if !strings.Contains(buf.String(), `"command"`) || !strings.Contains(buf.String(), `"archive_unavailable"`) {
		t.Fatalf("opportunities json: %q", buf.String())
	}

	r3, w3, _ := os.Pipe()
	os.Stdout = w3
	handleSubcommand([]string{"gain", "today", "--opportunities", "--csv"})
	_ = w3.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r3)
	if !strings.Contains(buf.String(), "scope,command,outcome,runs,input_tokens,output_tokens") || !strings.Contains(buf.String(), "command_output_first") {
		t.Fatalf("opportunities csv: %q", buf.String())
	}
}

func TestHandleSubcommand_gain_opportunitiesEmptyAndNoDB(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.db")
	t.Setenv("SLIMFERENCE_FILTER_DB", missingPath)
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", filepath.Join(t.TempDir(), "missing-decisions.jsonl"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today", "--opportunities"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No command-output-first or WSS shadow-mirror opportunities in this window") {
		t.Fatalf("no-db stdout: %q", buf.String())
	}

	dbPath := filepath.Join(dir, "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"gain", "today", "--opportunities"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	if !strings.Contains(buf.String(), "No command-output-first or WSS shadow-mirror opportunities in this window") {
		t.Fatalf("empty stdout: %q", buf.String())
	}
}

func TestHandleSubcommand_gain_opportunitiesIncludesWSSShadowMirrorHeadroom(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SLIMFERENCE_FILTER_DB", filepath.Join(tmp, "missing-filter.db"))
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "req-wss-headroom",
		Timestamp: time.Now(),
		DebugFacts: map[string]string{
			"wss.request_shape":                            "full_history",
			"wss.shadow_mirror_normalized_density_by_kind": "codex_exec_payload=4000/8000/1/1",
		},
		Tokens: dbg.TokenCounts{Saved: 100},
	})
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today", "--opportunities"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	for _, want := range []string{
		"full_history/codex_exec_payload",
		"wss_shadow_mirror",
		"t408_reference_or_t418_parser_recovery",
		"t408_backend_reference_or_t418_parser_recovery_gate",
		"requires_parser_or_recovery_product_slice",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("WSS opportunity stdout missing %q: %q", want, out)
		}
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"gain", "today", "--opportunities", "--json"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	if !strings.Contains(buf.String(), `"local_tokens_headroom"`) || !strings.Contains(buf.String(), `"candidate_lane"`) {
		t.Fatalf("WSS opportunities json: %q", buf.String())
	}
}

func TestWriteGainOpportunitiesCSVError(t *testing.T) {
	err := writeGainOpportunitiesCSV(errGainOpportunityWriter{}, []gainOpportunityRow{{
		Scope:        commandOutputFirstObservationScope,
		Command:      "ls -lah generated",
		Outcome:      commandOutputFirstObservationFullPass,
		Runs:         1,
		InputTokens:  100,
		OutputTokens: 100,
	}})
	if err == nil {
		t.Fatal("expected CSV writer error")
	}
}

type errGainOpportunityWriter struct{}

func (errGainOpportunityWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestHandleGainOpportunitiesErrorBranches(t *testing.T) {
	origPath := resolveFilterDBPathFn
	t.Cleanup(func() { resolveFilterDBPathFn = origPath })

	resolveFilterDBPathFn = func() (string, error) { return "", errors.New("path failed") }
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"gain", "today", "--opportunities"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "gain --opportunities: path failed") {
		t.Fatalf("path error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	resolveFilterDBPathFn = func() (string, error) { return "bad\x00path", nil }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleSubcommand([]string{"gain", "today", "--opportunities"})
	})
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "gain --opportunities") {
		t.Fatalf("stat error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	corruptDB := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(corruptDB, []byte("not-a-sqlite-database"), 0644); err != nil {
		t.Fatal(err)
	}
	resolveFilterDBPathFn = func() (string, error) { return corruptDB, nil }
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleSubcommand([]string{"gain", "today", "--opportunities"})
	})
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "gain --opportunities") {
		t.Fatalf("corrupt db exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	dbPath := filepath.Join(t.TempDir(), "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterObservation(db, commandOutputFirstObservationScope, "ls -lah generated", "/repo", 100, 100, commandOutputFirstObservationFullPass, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	resolveFilterDBPathFn = func() (string, error) { return dbPath, nil }
	closedOut, err := os.CreateTemp(t.TempDir(), "closed-stdout")
	if err != nil {
		t.Fatal(err)
	}
	if err := closedOut.Close(); err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = closedOut
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleSubcommand([]string{"gain", "today", "--opportunities", "--csv"})
	})
	cleanup()
	os.Stdout = oldStdout
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "gain --opportunities") {
		t.Fatalf("csv error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleGainOpportunities("bad-period", gainCLIFlags{})
	})
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "gain --opportunities") {
		t.Fatalf("period error exited=%v code=%d stderr=%q", exited, code, buf.String())
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

func TestHandleSubcommand_gain_byCommandShowsNegativeRecoveryCost(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "filter.db")
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now()
	if err := filter.RecordFilterRun(db, "[command-output-first:grep] grep needle", "/proj", 100, 30, 70, ts); err != nil {
		t.Fatal(err)
	}
	if err := filter.RecordFilterRun(db, "[archive-recovery:contentarchive] slimference expand local-archive://abc", "/proj", 2, 22, -1000, ts); err != nil {
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
	handleSubcommand([]string{"gain", "today", "--by-command"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Estimated tokens saved: 50") {
		t.Fatalf("summary did not subtract recovery cost: %q", out)
	}
	if !strings.Contains(out, "[archive-recovery:contentarchive]") || !strings.Contains(out, "saved ~-20") {
		t.Fatalf("negative recovery row hidden: %q", out)
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

func TestParseGainArgs_cacheAndByParser(t *testing.T) {
	p, f, err := parseGainArgs([]string{"--cache", "week", "--json"})
	if err != nil || p != "week" || !f.cache || !f.json {
		t.Fatalf("period=%q flags=%+v err=%v", p, f, err)
	}
	p, f, err = parseGainArgs([]string{"--proxy", "today", "--json"})
	if err != nil || p != "today" || !f.proxy || !f.json {
		t.Fatalf("period=%q flags=%+v err=%v", p, f, err)
	}
	p, f, err = parseGainArgs([]string{"--by-parser"})
	if err != nil || p != "today" || !f.byParser {
		t.Fatalf("period=%q flags=%+v err=%v", p, f, err)
	}
	p, f, err = parseGainArgs([]string{"--output", "month", "--csv"})
	if err != nil || p != "month" || !f.output || !f.csv {
		t.Fatalf("period=%q flags=%+v err=%v", p, f, err)
	}
	if _, _, err = parseGainArgs([]string{"--cache", "--by-command"}); err == nil {
		t.Fatal("expected invalid cache/by-command combination")
	}
	if _, _, err = parseGainArgs([]string{"--cache", "--output"}); err == nil {
		t.Fatal("expected invalid cache/output combination")
	}
	if _, _, err = parseGainArgs([]string{"--output", "--project", "/p"}); err == nil {
		t.Fatal("expected invalid output/project combination")
	}
	if _, _, err = parseGainArgs([]string{"--proxy", "--cache"}); err == nil {
		t.Fatal("expected invalid proxy/cache combination")
	}
	if _, _, err = parseGainArgs([]string{"--by-command", "--by-parser"}); err == nil {
		t.Fatal("expected invalid by-command/by-parser combination")
	}
}

func TestHandleSubcommand_gain_byParser(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "tsc --noEmit", "git status")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	cfgPath := filepath.Join(t.TempDir(), "gain-usd.toml")
	content := "[analytics]\ngain_usd_per_million_tokens = 2.5\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today", "--by-parser"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "By parser/tool family") || !strings.Contains(out, "typescript") || !strings.Contains(out, "git") {
		t.Fatalf("by-parser stdout: %q", out)
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "today", "--csv", "--by-parser"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "parser") || !strings.Contains(buf.String(), "typescript") {
		t.Fatalf("csv by-parser stdout: %q", buf.String())
	}
}

func TestHandleSubcommand_gain_cache(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))
	p, err := analytics.NewPersister(logDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.WriteEvent(types.AnalyticsEvent{
		Type:              types.EventRequestProcessed,
		Timestamp:         time.Now(),
		CacheReadTokens:   200,
		CacheCreateTokens: 40,
	}); err != nil {
		t.Fatal(err)
	}
	p.Close()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--cache", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Prompt-cache gain") || !strings.Contains(out, "200") {
		t.Fatalf("cache gain stdout: %q", out)
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--cache", "today", "--json"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"cache_read_tokens"`) {
		t.Fatalf("cache gain json: %q", buf.String())
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--cache", "today", "--csv"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "cache_read_tokens") {
		t.Fatalf("cache gain csv: %q", buf.String())
	}
}

func TestHandleSubcommand_gain_cacheNoRows(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--cache", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No prompt-cache gain") {
		t.Fatalf("cache gain empty: %q", buf.String())
	}
}

func TestHandleSubcommand_gain_output(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))
	p, err := analytics.NewPersister(logDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.WriteEvent(types.AnalyticsEvent{
		Type:                    types.EventRequestProcessed,
		Timestamp:               time.Now(),
		Provider:                types.CodexChatGPT,
		Model:                   "gpt-5.5",
		OutputTokens:            120,
		OutputReduceApplied:     true,
		OutputReduceProfile:     "codex",
		OutputReduceReason:      "applied",
		OutputReduceAddedTokens: 14,
		OutputReduceTaskShape:   "code_edit",
	}); err != nil {
		t.Fatal(err)
	}
	p.Close()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--output", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Output-reduce telemetry") || !strings.Contains(out, "Savings need a live baseline") || !strings.Contains(out, "code_edit") || !strings.Contains(out, "Profile rows:") || !strings.Contains(out, "codex_chatgpt/gpt-5.5") {
		t.Fatalf("output gain stdout: %q", out)
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--output", "today", "--json"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"input_overhead_tokens"`) || !strings.Contains(buf.String(), `"profile_rows"`) {
		t.Fatalf("output gain json: %q", buf.String())
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--output", "today", "--csv"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "input_overhead_tokens") || !strings.Contains(buf.String(), "profile_row,codex_chatgpt,gpt-5.5") {
		t.Fatalf("output gain csv: %q", buf.String())
	}
}

func TestHandleSubcommand_gain_outputNoRows(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--output", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No output-reduce telemetry") {
		t.Fatalf("output gain empty: %q", buf.String())
	}
}

func TestHandleSubcommand_gain_proxy(t *testing.T) {
	tmp := t.TempDir()
	decisionsPath := filepath.Join(tmp, "decisions.jsonl")
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "req-proxy",
		Timestamp: time.Now(),
		Source:    "proxy",
		Provider:  "codex_chatgpt",
		Tokens: dbg.TokenCounts{
			Original: 1000,
			Final:    800,
			Saved:    200,
		},
		ProviderInputTokens:  1100,
		ProviderCachedTokens: 400,
		ProviderOutputTokens: 90,
		OutputReduce: dbg.OutputReduceSummary{
			Applied:     true,
			AddedTokens: 10,
			TaskShape:   "read_only_analysis",
		},
		PromptCache: dbg.PromptCacheSummary{
			Applied:            true,
			Reason:             "applied",
			StablePrefixHash:   "heat-a",
			StablePrefixTokens: 2048,
		},
		ToolPrune: dbg.ToolPruneSummary{
			SavedTokens:   12,
			PrunedTools:   2,
			Reattached:    1,
			Miss:          true,
			Retry:         true,
			SessionKeySet: true,
		},
	})
	for i := range 6 {
		writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
			RequestID:            fmt.Sprintf("req-heat-%d", i),
			Timestamp:            time.Now(),
			Source:               "proxy",
			Provider:             "codex_chatgpt",
			ProviderInputTokens:  100,
			ProviderCachedTokens: 1000 + i,
			ProviderOutputTokens: 10,
			CacheReadTokens:      900 + i,
			CacheCreateTokens:    20 + i,
			PromptCache: dbg.PromptCacheSummary{
				Applied:            true,
				Reason:             "applied",
				StablePrefixHash:   fmt.Sprintf("heat-extra-%d", i),
				StablePrefixTokens: 3000 + i,
			},
		})
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--proxy", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Proxy flight gain") || !strings.Contains(out, "Provider cached tokens") || !strings.Contains(out, "Tool-prune saved tokens") || !strings.Contains(out, "Prompt-cache heat") {
		t.Fatalf("proxy gain stdout: %q", out)
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--proxy", "today", "--json"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"provider_cached_tokens"`) || !strings.Contains(buf.String(), `"net_billable_equivalent_estimate"`) || !strings.Contains(buf.String(), `"prompt_cache_heat"`) {
		t.Fatalf("proxy gain json: %q", buf.String())
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--proxy", "today", "--csv"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "provider_cached_tokens") || !strings.Contains(buf.String(), "prompt_cache_heat_keys") || !strings.Contains(buf.String(), "today") {
		t.Fatalf("proxy gain csv: %q", buf.String())
	}
}

func TestHandleSubcommand_gain_proxyNoRowsOrLog(t *testing.T) {
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "")
	cfgPath := filepath.Join(t.TempDir(), "blank-debug.toml")
	if err := os.WriteFile(cfgPath, []byte("[debug]\ndecisions_log = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--proxy", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No decisions_log configured") {
		t.Fatalf("proxy gain no log: %q", buf.String())
	}

	decisionsPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	if err := os.WriteFile(decisionsPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"gain", "--proxy", "today"})
	_ = w.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No proxy flight records") {
		t.Fatalf("proxy gain no rows: %q", buf.String())
	}
}

func TestHandleGainCmd_proxyErrorBranches(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", filepath.Join(t.TempDir(), "missing.jsonl"))
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"gain", "--proxy", "today"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "gain --proxy") {
		t.Fatalf("replay error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	decisionsPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{RequestID: "req", Timestamp: time.Now()})
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleGainProxy("bad", gainCLIFlags{})
	})
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "gain --proxy") {
		t.Fatalf("bad period exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	orig := writeProxyFlightGainCSV
	defer func() { writeProxyFlightGainCSV = orig }()
	writeProxyFlightGainCSV = func(io.Writer, analytics.ProxyFlightGainSummary) error {
		return errors.New("proxy csv failed")
	}
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleSubcommand([]string{"gain", "--proxy", "today", "--csv"})
	})
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "proxy csv failed") {
		t.Fatalf("csv error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}
}

func TestHandleGainCmd_byParserCSVError(t *testing.T) {
	dbPath := testOpenFilterDBAndRecord(t, "tsc --noEmit")
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	orig := writeGainByParserCSV
	defer func() { writeGainByParserCSV = orig }()
	writeGainByParserCSV = func(io.Writer, []analytics.FilterGainByParserRow) error {
		return errors.New("parser csv failed")
	}

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"gain", "today", "--csv", "--by-parser"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "parser csv failed") {
		t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
	}
}

func TestHandleGainCmd_outputErrorBranches(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "bad.toml"))
	if err := os.WriteFile(os.Getenv("SLIMFERENCE_CONFIG"), []byte("not valid [[["), 0644); err != nil {
		t.Fatal(err)
	}
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"gain", "--output", "today"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "load config") {
		t.Fatalf("config error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	logDirFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(logDirFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDirFile))
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleSubcommand([]string{"gain", "--output", "all"})
	})
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "gain --output") {
		t.Fatalf("report error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))
	orig := writeOutputReduceCSV
	defer func() { writeOutputReduceCSV = orig }()
	writeOutputReduceCSV = func(io.Writer, analytics.OutputReduceReport) error {
		return errors.New("output csv failed")
	}
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleSubcommand([]string{"gain", "--output", "today", "--csv"})
	})
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "output csv failed") {
		t.Fatalf("csv error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}
}

func TestHandleGainCmd_cacheErrorBranches(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "bad.toml"))
	if err := os.WriteFile(os.Getenv("SLIMFERENCE_CONFIG"), []byte("not valid [[["), 0644); err != nil {
		t.Fatal(err)
	}
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"gain", "--cache", "today"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "load config") {
		t.Fatalf("config error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	logDirFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(logDirFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDirFile))
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleSubcommand([]string{"gain", "--cache", "all"})
	})
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "gain --cache") {
		t.Fatalf("report error exited=%v code=%d stderr=%q", exited, code, buf.String())
	}

	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))
	orig := writePromptCacheCSV
	defer func() { writePromptCacheCSV = orig }()
	writePromptCacheCSV = func(io.Writer, analytics.PromptCacheReport) error {
		return errors.New("cache csv failed")
	}
	rp, cleanup = redirectStderr()
	code, exited = captureExit(func() {
		handleSubcommand([]string{"gain", "--cache", "today", "--csv"})
	})
	cleanup()
	buf.Reset()
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "cache csv failed") {
		t.Fatalf("csv error exited=%v code=%d stderr=%q", exited, code, buf.String())
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
	if ee, ok := errors.AsType[*exec.ExitError](err); !ok || ee.ExitCode() != 1 {
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
	if ee, ok := errors.AsType[*exec.ExitError](err); !ok || ee.ExitCode() != 1 {
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
	if ee, ok := errors.AsType[*exec.ExitError](err); !ok || ee.ExitCode() != 1 {
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
	if ee, ok := errors.AsType[*exec.ExitError](err); !ok || ee.ExitCode() != 1 {
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
	if ee, ok := errors.AsType[*exec.ExitError](err); !ok || ee.ExitCode() != 1 {
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

func writeDecisionSummary(t *testing.T, path string, summary dbg.RequestSummary) {
	t.Helper()
	summary.EnsureFlight()
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

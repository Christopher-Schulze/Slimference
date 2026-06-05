package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestParseSoakArgs_Defaults(t *testing.T) {
	t.Parallel()
	period, f, err := parseSoakArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if period != "week" || f.json {
		t.Fatalf("defaults wrong: period=%q flags=%+v", period, f)
	}
}

func TestParseSoakArgs_AllFlags(t *testing.T) {
	t.Parallel()
	period, f, err := parseSoakArgs([]string{"month", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if period != "month" || !f.json {
		t.Fatalf("flags wrong: period=%q f=%+v", period, f)
	}
}

func TestParseSoakArgs_UnknownFlag(t *testing.T) {
	t.Parallel()
	if _, _, err := parseSoakArgs([]string{"--bogus"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseSoakArgs_ExtraPositional(t *testing.T) {
	t.Parallel()
	if _, _, err := parseSoakArgs([]string{"week", "month"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseSoakArgs_EmptyArgsSkipped(t *testing.T) {
	t.Parallel()
	period, _, err := parseSoakArgs([]string{"", "today"})
	if err != nil || period != "today" {
		t.Fatalf("got period=%q err=%v", period, err)
	}
}

func TestDaysFor(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"today": 1, "week": 7, "month": 30, "all": 365,
	}
	for k, want := range cases {
		got, err := daysFor(k)
		if err != nil || got != want {
			t.Fatalf("%s: got=%d err=%v", k, got, err)
		}
	}
	if _, err := daysFor("yearly"); err == nil {
		t.Fatal("expected error for invalid period")
	}
}

func TestClassifyTrend(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []float64
		want string
	}{
		{"empty", nil, "insufficient_data"},
		{"single", []float64{0.5}, "insufficient_data"},
		{"stable", []float64{0.5, 0.51, 0.49, 0.50}, "stable"},
		{"regression", []float64{0.8, 0.79, 0.6, 0.55}, "regression"},
		{"improvement", []float64{0.4, 0.42, 0.6, 0.62}, "improvement"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyTrend(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestMean(t *testing.T) {
	t.Parallel()
	if got := mean(nil); got != 0 {
		t.Fatalf("nil: %v", got)
	}
	if got := mean([]float64{1, 2, 3}); got != 2 {
		t.Fatalf("simple: %v", got)
	}
}

func TestComputeSoakReport_NoData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r, err := computeSoakReport(dir, "week", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r.Snapshots != 0 {
		t.Fatalf("empty dir snapshots: %d", r.Snapshots)
	}
	if r.Verdict != "no data" {
		t.Fatalf("verdict: %q", r.Verdict)
	}
	if r.SafeForT100 || r.SafeForT103 {
		t.Fatal("no-data must not greenlight")
	}
}

func TestComputeSoakReport_BadPeriod(t *testing.T) {
	t.Parallel()
	if _, err := computeSoakReport(t.TempDir(), "yearly", time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

// writeSnapshot writes a single session_snapshot JSONL line for the
// given day so the persistence layer's daily reader picks it up.
func writeSnapshot(t *testing.T, dir string, day time.Time, snap analytics.AnalyticsSnapshot) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, day.Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	payload, _ := json.Marshal(snap)
	envelope := map[string]any{
		"type":      "session_snapshot",
		"timestamp": day,
		"payload":   json.RawMessage(payload),
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(envelope); err != nil {
		t.Fatal(err)
	}
}

func TestComputeSoakReport_HappyPath_Stable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		day := now.AddDate(0, 0, -i)
		writeSnapshot(t, dir, day, analytics.AnalyticsSnapshot{
			TotalRequests:           100,
			TotalInputTokens:        10000,
			SavedInputTokens:        4000,
			Errors:                  0,
			PromptCacheReadRequests: 80,
			PerProvider:             map[types.Provider]analytics.ProviderStats{},
		})
	}
	r, err := computeSoakReport(dir, "week", now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Snapshots != 7 {
		t.Fatalf("snapshots: %d", r.Snapshots)
	}
	if !r.SafeForT100 || !r.SafeForT103 {
		t.Fatalf("clean data should greenlight: %+v", r)
	}
	if !strings.Contains(r.Verdict, "ok to enable both") {
		t.Fatalf("verdict: %q", r.Verdict)
	}
}

func TestComputeSoakReport_Regression(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	// First half (older): high prompt cache hit rate.
	for i := 4; i < 8; i++ {
		writeSnapshot(t, dir, now.AddDate(0, 0, -i), analytics.AnalyticsSnapshot{
			TotalRequests:           100,
			PromptCacheReadRequests: 90,
			PerProvider:             map[types.Provider]analytics.ProviderStats{},
		})
	}
	// Second half (newer): low prompt cache hit rate -> regression.
	for i := 0; i < 4; i++ {
		writeSnapshot(t, dir, now.AddDate(0, 0, -i), analytics.AnalyticsSnapshot{
			TotalRequests:           100,
			PromptCacheReadRequests: 30,
			PerProvider:             map[types.Provider]analytics.ProviderStats{},
		})
	}
	r, err := computeSoakReport(dir, "week", now)
	if err != nil {
		t.Fatal(err)
	}
	if r.PromptCacheTrend != "regression" {
		t.Fatalf("trend: %q", r.PromptCacheTrend)
	}
	if r.SafeForT100 || r.SafeForT103 {
		t.Fatalf("regression must block both: %+v", r)
	}
}

func TestComputeSoakReport_T100UnsafeButT103Safe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		writeSnapshot(t, dir, now.AddDate(0, 0, -i), analytics.AnalyticsSnapshot{
			TotalRequests:           100,
			Errors:                  0,
			PromptCacheReadRequests: 80,
			OverflowRetries:         3, // blocks T100
			PerProvider:             map[types.Provider]analytics.ProviderStats{},
		})
	}
	r, err := computeSoakReport(dir, "week", now)
	if err != nil {
		t.Fatal(err)
	}
	if r.SafeForT100 {
		t.Fatalf("overflow must block T100: %+v", r)
	}
	if !r.SafeForT103 {
		t.Fatalf("T103 should still be safe: %+v", r)
	}
	if !strings.Contains(r.Verdict, "T103 looks safe") {
		t.Fatalf("verdict: %q", r.Verdict)
	}
}

func TestFormatSoakText_T100SafeT103Unsafe(t *testing.T) {
	t.Parallel()
	r := SoakReport{
		Period: "week", Days: 7, Snapshots: 5,
		SafeForT100: true, SafeForT103: false,
		Verdict: "T100 looks safe; T103 needs more soak time",
	}
	out := formatSoakText(r)
	if !strings.Contains(out, "T100 looks safe; T103 needs more soak time") {
		t.Fatalf("text: %s", out)
	}
}

func TestFormatSoakText_NoData(t *testing.T) {
	t.Parallel()
	out := formatSoakText(SoakReport{Period: "week", Days: 7, Verdict: "no data"})
	if !strings.Contains(out, "no analytics snapshots") {
		t.Fatalf("text: %s", out)
	}
}

func TestFormatSoakText_Populated(t *testing.T) {
	t.Parallel()
	r := SoakReport{
		Period: "week", Days: 7, Snapshots: 7, TotalRequests: 700,
		AvgCompressionPct: 40.0, PromptCacheHitRate: 0.85,
		PromptCacheTrend: "stable", ErrorRatePct: 0.5,
		SafeForT100: true, SafeForT103: true, Verdict: "ok to enable both",
	}
	out := formatSoakText(r)
	for _, want := range []string{"Snapshots", "Total requests", "Verdict:", "ok to enable both"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
}

func TestHandleSoakCmd_BadFlag(t *testing.T) {
	origExit := exitFn
	t.Cleanup(func() { exitFn = origExit })
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }

	stderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	handleSoakCmd([]string{"--bogus"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit(1): %v", exits)
	}
}

func TestHandleSoakCmd_BadPeriod(t *testing.T) {
	origExit := exitFn
	t.Cleanup(func() { exitFn = origExit })
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }

	stderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	handleSoakCmd([]string{"yearly"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit(1): %v", exits)
	}
}

func TestHandleSoakCmd_LoadConfigFails(t *testing.T) {
	origExit := exitFn
	origLoad := configLoadFn
	t.Cleanup(func() {
		exitFn = origExit
		configLoadFn = origLoad
	})
	configLoadFn = func() (*config.Config, error) { return nil, errors.New("boom") }
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }

	stderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	handleSoakCmd([]string{"week"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit(1): %v", exits)
	}
}

func TestHandleSoakCmd_TextOutput(t *testing.T) {
	origLoad := configLoadFn
	t.Cleanup(func() { configLoadFn = origLoad })

	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Analytics.LogDir = tmp
	configLoadFn = func() (*config.Config, error) { return cfg, nil }

	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	handleSoakCmd([]string{"today"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Slimference soak report") {
		t.Fatalf("text output: %s", buf.String())
	}
}

func TestHandleSoakCmd_JSONOutput(t *testing.T) {
	origLoad := configLoadFn
	t.Cleanup(func() { configLoadFn = origLoad })

	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Analytics.LogDir = tmp
	configLoadFn = func() (*config.Config, error) { return cfg, nil }

	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	handleSoakCmd([]string{"week", "--json"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"period"`) {
		t.Fatalf("json output: %s", buf.String())
	}
}

// TestHandleSubcommand_soakDispatch covers the case "soak" branch in
// main.go::handleSubcommand.
func TestHandleSubcommand_soakDispatch(t *testing.T) {
	origExit := exitFn
	origLoad := configLoadFn
	t.Cleanup(func() {
		exitFn = origExit
		configLoadFn = origLoad
	})
	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Analytics.LogDir = tmp
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	exitFn = func(int) {}

	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	handleSubcommand([]string{"soak", "today"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "soak report") {
		t.Fatalf("dispatch: %s", buf.String())
	}
}

// TestConfigLoadDefaultForSoak smoke-tests the small adapter helper.
func TestConfigLoadDefaultForSoak(t *testing.T) {
	t.Parallel()
	c, err := configLoadDefaultForSoak()
	if err != nil || c == nil {
		t.Fatalf("got cfg=%v err=%v", c, err)
	}
}

func TestHandleSoakCmd_BadPeriodViaCompute(t *testing.T) {
	origExit := exitFn
	t.Cleanup(func() { exitFn = origExit })
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }

	origCfg := configLoadFn
	t.Cleanup(func() { configLoadFn = origCfg })
	configLoadFn = configLoadDefaultForSoak

	stderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	handleSoakCmd([]string{"bogus_period"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("exits=%v", exits)
	}
	if !strings.Contains(buf.String(), "invalid period") {
		t.Fatalf("stderr: %s", buf.String())
	}
}

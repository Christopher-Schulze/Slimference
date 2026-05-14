package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/config"
	dbg "github.com/slimference/slimference/internal/debug"
)

func TestParseSavingsArgs_Defaults(t *testing.T) {

	period, flags, err := parseSavingsArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if period != "today" {
		t.Fatalf("default period: %q", period)
	}
	if flags.json || flags.csv || flags.project != "" {
		t.Fatalf("default flags wrong: %+v", flags)
	}
}

func TestParseSavingsArgs_AllFlags(t *testing.T) {

	period, flags, err := parseSavingsArgs([]string{"week", "--json", "--csv", "--project", "/tmp/proj"})
	if err != nil {
		t.Fatal(err)
	}
	if period != "week" {
		t.Fatalf("period: %q", period)
	}
	if !flags.json || !flags.csv || flags.project != "/tmp/proj" {
		t.Fatalf("flags wrong: %+v", flags)
	}
}

func TestParseSavingsArgs_UnknownFlag(t *testing.T) {

	if _, _, err := parseSavingsArgs([]string{"--unknown"}); err == nil {
		t.Fatal("unknown flag must error")
	}
}

func TestParseSavingsArgs_ProjectMissing(t *testing.T) {

	if _, _, err := parseSavingsArgs([]string{"--project"}); err == nil {
		t.Fatal("missing project value must error")
	}
}

func TestParseSavingsArgs_DoublePeriod(t *testing.T) {

	if _, _, err := parseSavingsArgs([]string{"today", "week"}); err == nil {
		t.Fatal("two periods must error")
	}
}

func TestParseSavingsArgs_EmptyArgsSkipped(t *testing.T) {

	period, _, err := parseSavingsArgs([]string{"", "today", ""})
	if err != nil {
		t.Fatal(err)
	}
	if period != "today" {
		t.Fatalf("period: %q", period)
	}
}

func TestComputeSavings_NoData(t *testing.T) {

	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	cfg.Analytics.GainUSDPerMillionTokens = 5
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() { resolveFilterDBPathFn = prevPath })
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }

	got := computeSavings(cfg, "today", "", time.Now())
	if got.TotalSavedTokens != 0 {
		t.Fatalf("expected zero totals: %+v", got)
	}
}

func TestAccumulateSnapshots(t *testing.T) {

	out := SavingsSummary{}
	accumulateSnapshots(&out, []analytics.AnalyticsSnapshot{
		{TotalRequests: 5, TotalInputTokens: 1000, SavedInputTokens: 300, CacheHits: 1},
		{TotalRequests: 2, TotalInputTokens: 500, SavedInputTokens: 100, CacheHits: 0},
	})
	if out.ProxyRequests != 7 || out.ProxyOrigTokens != 1500 || out.ProxySavedTokens != 400 {
		t.Fatalf("aggregate wrong: %+v", out)
	}
	if out.ProxyCompTokens != 1100 {
		t.Fatalf("comp tokens wrong: %d", out.ProxyCompTokens)
	}
	if out.CacheHits != 1 {
		t.Fatalf("cache hits: %d", out.CacheHits)
	}
}

func TestAccumulateSnapshots_NegativeSavedClamped(t *testing.T) {

	out := SavingsSummary{}
	accumulateSnapshots(&out, []analytics.AnalyticsSnapshot{
		{TotalRequests: 1, TotalInputTokens: 100, SavedInputTokens: 200},
	})
	if out.ProxyCompTokens != 0 {
		t.Fatalf("negative comp must clamp to 0, got %d", out.ProxyCompTokens)
	}
}

func TestFormatSavingsText(t *testing.T) {

	s := SavingsSummary{
		Period:                           "today",
		Project:                          "/tmp/proj",
		Layer0Runs:                       3,
		Layer0SavedTokens:                100,
		ProxyRequests:                    5,
		ProxyOrigTokens:                  1000,
		ProxyCompTokens:                  600,
		ProxySavedTokens:                 400,
		CacheHits:                        1,
		TotalSavedTokens:                 500,
		ProviderReportedRequests:         2,
		ProviderInputTokens:              1200,
		ProviderCachedTokens:             300,
		ProviderOutputTokens:             80,
		CacheReadDiscountTokenEquivalent: 270,
		NetBillableEquivalentTokens:      770,
		USDPerMillion:                    5,
		TotalSavedUSD:                    0.0025,
	}
	got := formatSavingsText(s)
	for _, want := range []string{"Slimference savings (today)", "/tmp/proj", "Layer 0 filter runs:", "Provider cached tokens:", "Billable-equivalent saved:", "Total tokens saved:", "$0.0025"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatSavingsText_NoUSD(t *testing.T) {

	s := SavingsSummary{Period: "week", TotalSavedTokens: 12}
	got := formatSavingsText(s)
	if strings.Contains(got, "$") {
		t.Fatalf("no USD rate must omit dollar line: %s", got)
	}
}

func TestFormatSavingsCSV(t *testing.T) {

	s := SavingsSummary{Period: "today", Layer0Runs: 1, TotalSavedTokens: 100, USDPerMillion: 5, TotalSavedUSD: 0.5}
	got := formatSavingsCSV(s)
	if !strings.Contains(got, "period,project") || !strings.Contains(got, "today,") {
		t.Fatalf("csv missing header/row: %s", got)
	}
}

func TestHandleSavingsCmd_Text(t *testing.T) {
	origStdout := os.Stdout
	origCfg := configLoadFn
	origPath := resolveFilterDBPathFn
	defer func() {
		os.Stdout = origStdout
		configLoadFn = origCfg
		resolveFilterDBPathFn = origPath
	}()

	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSavingsCmd([]string{"today"})
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Slimference savings") {
		t.Fatalf("output: %q", buf.String())
	}
}

func TestHandleSavingsCmd_JSON(t *testing.T) {
	origStdout := os.Stdout
	origCfg := configLoadFn
	origPath := resolveFilterDBPathFn
	defer func() {
		os.Stdout = origStdout
		configLoadFn = origCfg
		resolveFilterDBPathFn = origPath
	}()

	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSavingsCmd([]string{"week", "--json"})
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"period": "week"`) {
		t.Fatalf("json output: %q", buf.String())
	}
}

func TestHandleSavingsCmd_BadPeriod(t *testing.T) {
	origExit := exitFn
	origStderr := os.Stderr
	defer func() {
		exitFn = origExit
		os.Stderr = origStderr
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	r, w, _ := os.Pipe()
	os.Stderr = w
	handleSavingsCmd([]string{"yearly"})
	_ = w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

func TestHandleSavingsCmd_ConfigError(t *testing.T) {
	origExit := exitFn
	origCfg := configLoadFn
	origStderr := os.Stderr
	defer func() {
		exitFn = origExit
		configLoadFn = origCfg
		os.Stderr = origStderr
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	configLoadFn = func() (*config.Config, error) { return nil, io.ErrUnexpectedEOF }
	r, w, _ := os.Pipe()
	os.Stderr = w
	handleSavingsCmd([]string{"today"})
	_ = w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

func TestHandleSavingsCmd_BadFlag(t *testing.T) {
	origExit := exitFn
	origStderr := os.Stderr
	defer func() {
		exitFn = origExit
		os.Stderr = origStderr
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	r, w, _ := os.Pipe()
	os.Stderr = w
	handleSavingsCmd([]string{"--bogus"})
	_ = w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

func TestHandleSavingsCmd_CSV(t *testing.T) {
	origStdout := os.Stdout
	origCfg := configLoadFn
	origPath := resolveFilterDBPathFn
	defer func() {
		os.Stdout = origStdout
		configLoadFn = origCfg
		resolveFilterDBPathFn = origPath
	}()

	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSavingsCmd([]string{"all", "--csv"})
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "period,project") {
		t.Fatalf("csv: %q", buf.String())
	}
}

func TestComputeSavings_WithSnapshots(t *testing.T) {

	cfg := config.Defaults()
	logDir := t.TempDir()
	cfg.Analytics.LogDir = logDir
	cfg.Analytics.GainUSDPerMillionTokens = 5
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() { resolveFilterDBPathFn = prevPath })
	resolveFilterDBPathFn = func() (string, error) { return "/no/such.db", nil }

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	path := logDir + "/" + now.Format("2006-01-02") + ".jsonl"
	if err := os.WriteFile(path, []byte(`{"type":"session_snapshot","timestamp":"2026-04-30T12:00:00Z","payload":{"total_requests":4,"total_input_tokens":2000,"saved_input_tokens":600,"cache_hits":2}}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	got := computeSavings(cfg, "today", "", now)
	if got.ProxyRequests != 4 || got.ProxyOrigTokens != 2000 || got.ProxySavedTokens != 600 {
		t.Fatalf("snapshot aggregation off: %+v", got)
	}
	if got.TotalSavedUSD <= 0 {
		t.Fatalf("expected non-zero USD: %+v", got)
	}
}

func TestComputeSavings_UsesDecisionLogProxyFlights(t *testing.T) {
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() { resolveFilterDBPathFn = prevPath })
	resolveFilterDBPathFn = func() (string, error) { return "/no/such.db", nil }

	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	decisionsPath := t.TempDir() + "/decisions.jsonl"
	cfg.Debug.DecisionsLog = decisionsPath
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "req-proxy",
		Timestamp: now,
		Source:    "proxy",
		Provider:  "codex_chatgpt",
		Tokens: dbg.TokenCounts{
			Original: 1000,
			Final:    700,
			Saved:    300,
		},
		ProviderInputTokens:  1200,
		ProviderCachedTokens: 500,
		ProviderOutputTokens: 80,
		OutputReduce: dbg.OutputReduceSummary{
			AddedTokens: 12,
		},
	})

	got := computeSavings(cfg, "today", "", now)
	if got.ProxyRequests != 1 || got.ProviderReportedRequests != 1 {
		t.Fatalf("proxy request counters: %+v", got)
	}
	if got.ProxyOrigTokens != 1000 || got.ProxyCompTokens != 700 || got.ProxySavedTokens != 300 {
		t.Fatalf("proxy token counters: %+v", got)
	}
	if got.ProviderInputTokens != 1200 || got.ProviderCachedTokens != 500 || got.ProviderOutputTokens != 80 {
		t.Fatalf("provider counters: %+v", got)
	}
	if got.CacheReadDiscountTokenEquivalent != 450 || got.NetBillableEquivalentTokens != 750 {
		t.Fatalf("billable counters: %+v", got)
	}
}

func TestComputeSavings_ProjectSkipsDecisionLogProxyFlights(t *testing.T) {
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() { resolveFilterDBPathFn = prevPath })
	resolveFilterDBPathFn = func() (string, error) { return "/no/such.db", nil }

	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	decisionsPath := t.TempDir() + "/decisions.jsonl"
	cfg.Debug.DecisionsLog = decisionsPath
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "req-proxy",
		Timestamp: now,
		Source:    "proxy",
		Provider:  "codex_chatgpt",
		Tokens: dbg.TokenCounts{
			Original: 1000,
			Final:    700,
			Saved:    300,
		},
	})

	got := computeSavings(cfg, "today", "/project", now)
	if got.ProxyRequests != 0 || got.ProxySavedTokens != 0 {
		t.Fatalf("project-scoped savings must not use unscoped decision log: %+v", got)
	}
}

func TestComputeSavings_FilterPathError(t *testing.T) {

	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() { resolveFilterDBPathFn = prevPath })
	resolveFilterDBPathFn = func() (string, error) { return "", io.ErrUnexpectedEOF }
	_ = computeSavings(cfg, "today", "", time.Now())
}

func TestComputeSavings_FilterDBPresent(t *testing.T) {

	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() { resolveFilterDBPathFn = prevPath })
	// Write an empty file to satisfy os.Stat; QueryFilterGainReport will
	// fail gracefully on a non-SQLite file but the existence branch is
	// what we want to exercise.
	dbPath := t.TempDir() + "/filter.db"
	if err := os.WriteFile(dbPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	resolveFilterDBPathFn = func() (string, error) { return dbPath, nil }
	_ = computeSavings(cfg, "today", "", time.Now())
}

func TestHandleSubcommand_SavingsDispatch(t *testing.T) {
	origStdout := os.Stdout
	origCfg := configLoadFn
	origPath := resolveFilterDBPathFn
	defer func() {
		os.Stdout = origStdout
		configLoadFn = origCfg
		resolveFilterDBPathFn = origPath
	}()
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	resolveFilterDBPathFn = func() (string, error) { return "/no/such.db", nil }

	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	handleSubcommand([]string{"savings", "today"})
	_ = wp.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !strings.Contains(buf.String(), "Slimference savings") {
		t.Fatalf("dispatcher savings output: %q", buf.String())
	}
}

func TestComputeSavings_AllPeriodsAggregateHistorical(t *testing.T) {

	cfg := config.Defaults()
	logDir := t.TempDir()
	cfg.Analytics.LogDir = logDir
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() { resolveFilterDBPathFn = prevPath })
	resolveFilterDBPathFn = func() (string, error) { return "/no/such.db", nil }

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	// Write snapshots for today, ~3 days ago, ~10 days ago, ~25 days ago,
	// and ~200 days ago so all four period branches see actual data.
	for _, daysAgo := range []int{0, 3, 10, 25, 200} {
		day := now.AddDate(0, 0, -daysAgo)
		path := logDir + "/" + day.Format("2006-01-02") + ".jsonl"
		body := `{"type":"session_snapshot","timestamp":"2026-04-30T12:00:00Z","payload":{"total_requests":1,"total_input_tokens":100,"saved_input_tokens":40}}` + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, period := range []string{"today", "week", "month", "all"} {
		s := computeSavings(cfg, period, "", now)
		if s.Period != period {
			t.Fatalf("period mismatch: %q vs %q", s.Period, period)
		}
	}
}

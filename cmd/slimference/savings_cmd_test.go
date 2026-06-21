package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	"github.com/Christopher-Schulze/Slimference/internal/codexthreads"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/daemon"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
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

func TestAccumulateProxyFlightsReplayErrorIsIgnored(t *testing.T) {
	cfg := config.Defaults()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	prevReplay := replaySessionFn
	t.Cleanup(func() { replaySessionFn = prevReplay })
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return nil, errors.New("replay")
	}
	out := SavingsSummary{}
	accumulateProxyFlightsFromDecisionLog(&out, cfg, "today", time.Now())
	if out.ProxyRequests != 0 {
		t.Fatalf("replay error should leave summary unchanged: %+v", out)
	}
}

func TestComputeSavingsDecisionMechanismBreakdown(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	cfg.Analytics.GainUSDPerMillionTokens = 2.5
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	prevReplay := replaySessionFn
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		resolveFilterDBPathFn = prevPath
	})
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{
			{
				RequestID:            "hook-1",
				Timestamp:            now,
				SessionID:            "sess-1",
				Source:               "hook_post",
				ProviderOutputTokens: 12,
				CacheReadTokens:      4,
				CacheCreateTokens:    2,
				Tokens: dbg.TokenCounts{
					Original: 1000,
					Final:    250,
					Saved:    750,
				},
				Mechanisms: []dbg.MechanismAccounting{
					{Name: "codex_posttool_compaction", Layer: 1, Source: "decision_entry", Count: 1, OriginalTokens: 1000, FinalTokens: 300, SavedTokens: 700, NetTokens: 700, FootprintScoreBucket: "high", FootprintScore: 56000},
					{Name: "codex_archive_replacement", Layer: 1, Source: "decision_entry", Count: 1, OriginalTokens: 300, FinalTokens: 250, SavedTokens: 50, NetTokens: 50},
					{Name: "provider_prompt_cache", Source: "cache_accounting", Count: 1, SavedTokens: 4, AddedTokens: 2, NetTokens: 2},
					{Name: "zero_effect"},
					{Name: "request_total", Count: 1, OriginalTokens: 1000, FinalTokens: 250, SavedTokens: 750, NetTokens: 750},
				},
				EvidenceDecisions: []evidence.BlockDecision{{
					Layer:                1,
					Mechanism:            "codex_posttool_compaction",
					ContentClass:         evidence.ContentTest,
					SafetyClass:          evidence.SafetyDiagnosticPriority,
					Action:               evidence.ActionApplied,
					Signals:              []evidence.Signal{evidence.SignalErrorKeyword, evidence.SignalStacktrace},
					NetTokens:            700,
					CacheImpact:          "provider_cache_read",
					FootprintScoreBucket: "high",
					FootprintScore:       56000,
				}},
			},
			{
				RequestID: "hook-2",
				Timestamp: now,
				SessionID: "zzz",
				Source:    "hook_post",
				Tokens:    dbg.TokenCounts{Original: 20, Final: 10, Saved: 10},
				ToolPrune: dbg.ToolPruneSummary{Applied: true, SavedTokens: 3},
				OutputReduce: dbg.OutputReduceSummary{
					Applied:     true,
					AddedTokens: 2,
				},
				Mechanisms: []dbg.MechanismAccounting{{Name: "aaa_tie", NetTokens: 5}, {Name: "bbb_tie", NetTokens: 5}},
				EvidenceDecisions: []evidence.BlockDecision{{
					Layer:        4,
					Mechanism:    "output_reduce_directive",
					ContentClass: evidence.ContentPlain,
					SafetyClass:  evidence.SafetyFullPass,
					Action:       evidence.ActionFullPass,
					Signals:      []evidence.Signal{evidence.SignalRecency},
					NetTokens:    -2,
				}},
			},
			{
				RequestID: "hook-3",
				Timestamp: now,
				SessionID: "aaa",
				Source:    "hook_post",
				Tokens:    dbg.TokenCounts{Original: 30, AfterLayer0: 25, AfterLayer1: 20, Final: 20, Saved: 10},
			},
			{
				RequestID: "old",
				Timestamp: now.AddDate(0, 0, -2),
				Tokens:    dbg.TokenCounts{Original: 999, Final: 1, Saved: 998},
			},
		}, nil
	}

	got := computeSavings(cfg, "today", "", now)
	if got.DecisionRequests != 3 || got.DecisionOriginalTokens != 1050 || got.DecisionFinalTokens != 280 || got.DecisionNetSavedTokens != 770 {
		t.Fatalf("bad decision totals: %+v", got)
	}
	if got.TotalSavedTokens != got.DecisionNetSavedTokens {
		t.Fatalf("total should use measured decision savings when larger: %+v", got)
	}
	if got.DecisionOutputTokens != 12 || got.DecisionCacheReadTokens != 4 || got.DecisionCacheCreateTokens != 2 || got.DecisionCacheNetTokens != 2 {
		t.Fatalf("bad output/cache totals: %+v", got)
	}
	if got.DecisionCacheHitRequests != 1 || got.DecisionCacheCreateRequests != 1 || got.DecisionCacheNegativeNetRequests != 0 || !nearFloat(got.DecisionCacheHitRate, 1.0/3.0) {
		t.Fatalf("bad cache health totals: %+v", got)
	}
	if got.DecisionEstimatedCostBeforeUSD <= 0 || got.DecisionEstimatedCostAfterUSD <= 0 || got.DecisionEstimatedCostSavedUSD <= 0 {
		t.Fatalf("missing decision cost estimates: %+v", got)
	}
	if len(got.DecisionSessions) != 3 || got.DecisionSessions[0].SessionID != "sess-1" || got.DecisionSessions[0].NetSavedTokens != 750 || got.DecisionSessions[1].SessionID != "aaa" {
		t.Fatalf("bad decision sessions: %+v", got.DecisionSessions)
	}
	if got.DecisionSessions[0].Layer1NetTokens != 750 || got.DecisionSessions[0].Layer2NetTokens != 2 ||
		got.DecisionSessions[0].CacheNetTokens != 2 || got.DecisionSessions[0].CacheHitRequests != 1 || !nearFloat(got.DecisionSessions[0].CacheHitRate, 1.0) {
		t.Fatalf("bad sess-1 layer totals: %+v", got.DecisionSessions[0])
	}
	if got.DecisionSessions[1].Layer0NetTokens != 5 || got.DecisionSessions[1].Layer1NetTokens != 5 {
		t.Fatalf("bad fallback layer totals: %+v", got.DecisionSessions[1])
	}
	if got.DecisionSessions[2].OutputReduceTokens != -2 || got.DecisionSessions[2].ToolPruneTokens != 3 {
		t.Fatalf("bad output/tool session totals: %+v", got.DecisionSessions[2])
	}
	if got.DecisionLayer0NetTokens != 5 || got.DecisionLayer1NetTokens != 755 || got.DecisionLayer2NetTokens != 2 ||
		got.DecisionOutputReduceTokens != -2 || got.DecisionToolPruneTokens != 3 {
		t.Fatalf("bad aggregate layer totals: %+v", got)
	}
	if got.DecisionFootprintScore != 56000 || got.DecisionFootprintScoreBuckets["high"] != 1 ||
		got.DecisionSessions[0].FootprintScore != 56000 || got.DecisionSessions[0].FootprintBuckets["high"] != 1 ||
		got.Mechanisms[0].FootprintScore != 56000 || got.Mechanisms[0].FootprintBuckets["high"] != 1 {
		t.Fatalf("bad footprint scorecard: %+v sessions=%+v mechanisms=%+v", got, got.DecisionSessions, got.Mechanisms)
	}
	if got.DecisionCompoundedEstimateTokens != 70 ||
		got.DecisionSessions[0].CompoundedEstimateTokens != 70 ||
		got.DecisionSessions[0].Scorecard == nil ||
		got.DecisionSessions[0].Scorecard.CompoundedEstimateTokens != 70 ||
		got.Mechanisms[0].CompoundedEstimateTokens != 70 {
		t.Fatalf("bad compounded estimate: %+v sessions=%+v mechanisms=%+v", got, got.DecisionSessions, got.Mechanisms)
	}
	if got.Evidence.Decisions != 3 || got.Evidence.Applied != 2 || got.Evidence.FullPass != 1 ||
		got.Evidence.ByContentClass[string(evidence.ContentTest)] != 1 ||
		got.Evidence.BySignal[string(evidence.SignalCacheHotZone)] != 1 ||
		got.Evidence.ByCacheImpact["provider_cache_read"] != 2 ||
		got.Evidence.ByFootprint["high"] != 1 ||
		got.Evidence.FootprintScore != 56000 ||
		got.Evidence.NetTokens != 700 {
		t.Fatalf("bad evidence totals: %+v", got.Evidence)
	}
	if got.DecisionSessions[0].CostBeforeUSD <= 0 || got.DecisionSessions[0].CostAfterUSD <= 0 || got.DecisionSessions[0].CostSavedUSD <= 0 {
		t.Fatalf("missing session cost estimates: %+v", got.DecisionSessions[0])
	}
	if len(got.Mechanisms) != 5 {
		t.Fatalf("mechanisms=%d: %+v", len(got.Mechanisms), got.Mechanisms)
	}
	if got.Mechanisms[0].Name != "codex_posttool_compaction" || got.Mechanisms[0].NetTokens != 700 {
		t.Fatalf("top mechanism: %+v", got.Mechanisms)
	}
	text := formatSavingsText(got)
	for _, want := range []string{"Decision-log requests", "Decision net saved tokens", "Decision cache net", "33.3% hit", "Decision layer net", "L0=5,L1=755,L2=2,out=-2,tools=3", "Decision footprint score", "56.0K (high=1)", "Decision compounded est.", "70", "Evidence decisions", "cache_hot_zone=1", "Evidence cache impact", "provider_cache_read=2", "Evidence footprint score", "codex_posttool_compaction", "footprint=56.0K/high=1", "compounded_est=70", "session sess-1", "layers=L1=750,L2=2", "cache=2/100.0%"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %s", want, text)
		}
	}
}

func TestComputeSavingsDecisionCompoundedEstimateUsesSessionRemainder(t *testing.T) {
	base := time.Date(2026, 6, 11, 11, 0, 0, 0, time.UTC)
	reportNow := base.Add(10 * time.Second)
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	cfg.Savings.CachedPriceRatio = 0.25
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	prevReplay := replaySessionFn
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		resolveFilterDBPathFn = prevPath
	})
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{
			{
				RequestID: "first-footprint",
				Timestamp: base,
				SessionID: "codex-wss:compounded",
				Tokens:    dbg.TokenCounts{Original: 1000, Final: 900, Saved: 100},
				Mechanisms: []dbg.MechanismAccounting{
					{Name: "high_footprint", Layer: 1, Source: "evidence_decision", Count: 1, OriginalTokens: 1000, FinalTokens: 900, SavedTokens: 100, NetTokens: 100, FootprintScoreBucket: "high", FootprintScore: 800},
				},
			},
			{
				RequestID: "second-footprint",
				Timestamp: base.Add(time.Second),
				SessionID: "codex-wss:compounded",
				Tokens:    dbg.TokenCounts{Original: 800, Final: 720, Saved: 80},
				Mechanisms: []dbg.MechanismAccounting{
					{Name: "mid_footprint", Layer: 1, Source: "evidence_decision", Count: 1, OriginalTokens: 800, FinalTokens: 720, SavedTokens: 80, NetTokens: 80, FootprintScoreBucket: "mid", FootprintScore: 160},
				},
			},
			{
				RequestID: "not-classified",
				Timestamp: base.Add(2 * time.Second),
				SessionID: "codex-wss:compounded",
				Tokens:    dbg.TokenCounts{Original: 500, Final: 450, Saved: 50},
				Mechanisms: []dbg.MechanismAccounting{
					{Name: "unclassified_local", Layer: 1, Source: "evidence_decision", Count: 1, OriginalTokens: 500, FinalTokens: 450, SavedTokens: 50, NetTokens: 50},
				},
			},
		}, nil
	}

	got := computeSavings(cfg, "today", "", reportNow)
	if got.DecisionCompoundedEstimateTokens != 70 {
		t.Fatalf("aggregate compounded estimate=%d, want 70: %+v", got.DecisionCompoundedEstimateTokens, got)
	}
	if len(got.DecisionSessions) != 1 || got.DecisionSessions[0].CompoundedEstimateTokens != 70 {
		t.Fatalf("session compounded estimate wrong: %+v", got.DecisionSessions)
	}
	if got.DecisionSessions[0].Scorecard == nil || got.DecisionSessions[0].Scorecard.CompoundedEstimateTokens != 70 {
		t.Fatalf("session scorecard compounded estimate wrong: %+v", got.DecisionSessions[0].Scorecard)
	}
	byName := map[string]SavingsMechanismSummary{}
	for _, mechanism := range got.Mechanisms {
		byName[mechanism.Name] = mechanism
	}
	if byName["high_footprint"].CompoundedEstimateTokens != 50 ||
		byName["mid_footprint"].CompoundedEstimateTokens != 20 ||
		byName["unclassified_local"].CompoundedEstimateTokens != 0 {
		t.Fatalf("mechanism compounded estimates wrong: %+v", got.Mechanisms)
	}
	text := formatSavingsText(got)
	for _, want := range []string{"Decision compounded est.", "compounded_est=50", "compounded_est=20"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %s", want, text)
		}
	}
}

func TestComputeSavingsLiveCompoundedEstimateUsesHistoricalSessionLength(t *testing.T) {
	startedAt := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	reportNow := startedAt.Add(2 * time.Minute)
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	cfg.Savings.CachedPriceRatio = 0.25
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	prevReplay := replaySessionFn
	prevPath := resolveFilterDBPathFn
	prevDaemon := daemonIsRunningFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		resolveFilterDBPathFn = prevPath
		daemonIsRunningFn = prevDaemon
	})
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 1234, Port: 8990, StartedAt: startedAt}, nil
	}
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		out := make([]dbg.RequestSummary, 0, 7)
		for i := range 6 {
			out = append(out, dbg.RequestSummary{
				RequestID:    fmt.Sprintf("history-%d", i),
				Timestamp:    startedAt.Add(time.Duration(-10+i) * time.Minute),
				SessionID:    "codex-wss:historical",
				ClientFamily: "codex_cli",
			})
		}
		out = append(out, dbg.RequestSummary{
			RequestID:    "live-footprint",
			Timestamp:    startedAt.Add(time.Second),
			SessionID:    "codex-wss:live",
			ClientFamily: "codex_cli",
			DebugFacts:   map[string]string{"wss.turn_seq": "2"},
			Tokens:       dbg.TokenCounts{Original: 1000, Final: 900, Saved: 100},
			Mechanisms: []dbg.MechanismAccounting{
				{Name: "live_high_footprint", Layer: 1, Source: "evidence_decision", Count: 1, OriginalTokens: 1000, FinalTokens: 900, SavedTokens: 100, NetTokens: 100, FootprintScoreBucket: "high", FootprintScore: 600},
			},
		})
		return out, nil
	}

	got := computeSavings(cfg, "live", "", reportNow)
	if got.DecisionCompoundedEstimateTokens != 100 {
		t.Fatalf("live compounded estimate=%d, want 100: %+v", got.DecisionCompoundedEstimateTokens, got)
	}
	if len(got.DecisionSessions) != 1 || got.DecisionSessions[0].CompoundedEstimateTokens != 100 {
		t.Fatalf("live session compounded estimate wrong: %+v", got.DecisionSessions)
	}
	byName := map[string]SavingsMechanismSummary{}
	for _, mechanism := range got.Mechanisms {
		byName[mechanism.Name] = mechanism
	}
	if byName["live_high_footprint"].CompoundedEstimateTokens != 100 {
		t.Fatalf("mechanism compounded estimate wrong: %+v", got.Mechanisms)
	}
}

func TestEstimateCostUSD(t *testing.T) {
	before, after, saved := estimateCostUSD(100, 20, 30, 10, 0, 2.5)
	if !nearFloat(before, 0.0003) || !nearFloat(after, 0.0002025) || !nearFloat(saved, 0.0000975) {
		t.Fatalf("cost estimates: before=%v after=%v saved=%v", before, after, saved)
	}
	before, after, saved = estimateCostUSD(100, 20, 30, 10, 20, 2.5)
	if !nearFloat(before, 0.0003) || !nearFloat(after, 0.0002525) || !nearFloat(saved, 0.0000475) {
		t.Fatalf("cache-create-adjusted estimates: before=%v after=%v saved=%v", before, after, saved)
	}
	before, after, saved = estimateCostUSD(10, 0, 999, 999, 0, 2.5)
	if !nearFloat(before, 0.000025) || after != 0 || !nearFloat(saved, 0.000025) {
		t.Fatalf("clamped cost estimates: before=%v after=%v saved=%v", before, after, saved)
	}
	before, after, saved = estimateCostUSD(10, 0, -20, 0, 0, 2.5)
	if !nearFloat(before, 0.000025) || !nearFloat(after, 0.000025) || saved != 0 {
		t.Fatalf("negative savings cost estimates: before=%v after=%v saved=%v", before, after, saved)
	}
}

func nearFloat(got, want float64) bool {
	return math.Abs(got-want) < 0.0000000001
}

func TestDecisionSessionIDFallbacks(t *testing.T) {
	if got := decisionSessionID(dbg.RequestSummary{SessionID: " sess "}); got != "sess" {
		t.Fatalf("session id: %q", got)
	}
	if got := decisionSessionID(dbg.RequestSummary{SessionID: " empty ", Source: "proxy"}); got != "no-session:proxy" {
		t.Fatalf("empty session fallback: %q", got)
	}
	if got := decisionSessionID(dbg.RequestSummary{Source: " hook_post "}); got != "no-session:hook_post" {
		t.Fatalf("source fallback: %q", got)
	}
	if got := decisionSessionID(dbg.RequestSummary{}); got != "no-session:unknown" {
		t.Fatalf("unknown fallback: %q", got)
	}
}

func TestSavingsSessionsUseCodexThreadMetadata(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")

	prevReplay := replaySessionFn
	prevLookup := lookupCodexThreadMetadataForSavingsFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		lookupCodexThreadMetadataForSavingsFn = prevLookup
	})
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{{
			RequestID:            "req-thread",
			Timestamp:            now,
			SessionID:            "codex-wss:thread-123",
			Source:               "proxy",
			Provider:             "codex_chatgpt",
			ClientFamily:         "codex",
			ProviderCachedTokens: 500,
			Tokens:               dbg.TokenCounts{Original: 1000, Final: 700, Saved: 300},
		}}, nil
	}
	lookupCodexThreadMetadataForSavingsFn = func(ids []string) (map[string]codexthreads.Metadata, error) {
		if len(ids) != 1 || ids[0] != "thread-123" {
			t.Fatalf("thread lookup ids=%v", ids)
		}
		return map[string]codexthreads.Metadata{
			"thread-123": {
				ID:     "thread-123",
				Title:  "› check project status",
				CWD:    "/Users/me/CODE/Demo",
				Source: "cli",
				Model:  "gpt-5.5",
			},
		}, nil
	}

	var got SavingsSummary
	accumulateDecisionMechanismsFromDecisionLog(&got, cfg, "today", now)
	if len(got.DecisionSessions) != 1 {
		t.Fatalf("sessions=%d: %+v", len(got.DecisionSessions), got.DecisionSessions)
	}
	session := got.DecisionSessions[0]
	if session.DisplayName != "check project status" || session.ProjectPath != "/Users/me/CODE/Demo" || session.ClientFamily != "codex_cli" {
		t.Fatalf("bad enriched session: %+v", session)
	}
	text := formatSavingsText(got)
	for _, want := range []string{"Codex CLI", "check project status", "/Users/me/CODE/Demo", "cache=500"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "codex_chatgpt") || strings.Contains(text, "Codex App") {
		t.Fatalf("text leaked wrong client label: %s", text)
	}
}

func TestSavingsSessionsUseCodexHTTPThreadMetadata(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")

	prevReplay := replaySessionFn
	prevLookup := lookupCodexThreadMetadataForSavingsFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		lookupCodexThreadMetadataForSavingsFn = prevLookup
	})
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{{
			RequestID:    "req-http-thread",
			Timestamp:    now,
			SessionID:    "codex-http:thread-http",
			Source:       "proxy",
			Provider:     "codex_chatgpt",
			ClientFamily: "codex",
			Tokens:       dbg.TokenCounts{Original: 2000, Final: 1200, Saved: 800},
		}}, nil
	}
	lookupCodexThreadMetadataForSavingsFn = func(ids []string) (map[string]codexthreads.Metadata, error) {
		if len(ids) != 1 || ids[0] != "thread-http" {
			t.Fatalf("thread lookup ids=%v", ids)
		}
		return map[string]codexthreads.Metadata{
			"thread-http": {
				ID:     "thread-http",
				Title:  "› current goal",
				CWD:    "/Users/me/CODE/Demo",
				Source: "cli",
			},
		}, nil
	}

	var got SavingsSummary
	accumulateDecisionMechanismsFromDecisionLog(&got, cfg, "today", now)
	if len(got.DecisionSessions) != 1 {
		t.Fatalf("sessions=%d: %+v", len(got.DecisionSessions), got.DecisionSessions)
	}
	session := got.DecisionSessions[0]
	if session.DisplayName != "current goal" || session.ProjectPath != "/Users/me/CODE/Demo" || session.ClientFamily != "codex_cli" {
		t.Fatalf("bad enriched HTTP session: %+v", session)
	}
	if label := savingsSessionFallbackLabel(session); !strings.Contains(label, "thread-http") {
		t.Fatalf("fallback label=%q", label)
	}
}

func TestSavingsSessionsKeepParallelCodexThreadsSeparate(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")

	prevReplay := replaySessionFn
	prevLookup := lookupCodexThreadMetadataForSavingsFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		lookupCodexThreadMetadataForSavingsFn = prevLookup
	})
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{
			{
				RequestID:    "req-thread-a",
				Timestamp:    now,
				SessionID:    "codex-wss:thread-a",
				Source:       "proxy",
				Provider:     "codex_chatgpt",
				ClientFamily: "codex",
				Tokens:       dbg.TokenCounts{Original: 1000, Final: 400, Saved: 600},
			},
			{
				RequestID:    "req-thread-b",
				Timestamp:    now.Add(-time.Second),
				SessionID:    "codex-http:thread-b",
				Source:       "proxy",
				Provider:     "codex_chatgpt",
				ClientFamily: "codex",
				Tokens:       dbg.TokenCounts{Original: 1200, Final: 900, Saved: 300},
			},
		}, nil
	}
	lookupCodexThreadMetadataForSavingsFn = func(ids []string) (map[string]codexthreads.Metadata, error) {
		if len(ids) != 2 || ids[0] != "thread-a" || ids[1] != "thread-b" {
			t.Fatalf("thread lookup ids=%v", ids)
		}
		return map[string]codexthreads.Metadata{
			"thread-a": {ID: "thread-a", Title: "› Project status", CWD: "/Users/me/CODE/Demo", Source: "cli"},
			"thread-b": {ID: "thread-b", Title: "› Slimference audit", CWD: "/Users/me/CODE/Slimference", Source: "desktop"},
		}, nil
	}

	var got SavingsSummary
	accumulateDecisionMechanismsFromDecisionLog(&got, cfg, "today", now)
	if len(got.DecisionSessions) != 2 {
		t.Fatalf("sessions=%d: %+v", len(got.DecisionSessions), got.DecisionSessions)
	}
	if got.DecisionSessions[0].SessionID != "codex-wss:thread-a" ||
		got.DecisionSessions[0].DisplayName != "Project status" ||
		got.DecisionSessions[0].ProjectPath != "/Users/me/CODE/Demo" ||
		got.DecisionSessions[0].ClientFamily != "codex_cli" {
		t.Fatalf("bad first session: %+v", got.DecisionSessions[0])
	}
	if got.DecisionSessions[1].SessionID != "codex-http:thread-b" ||
		got.DecisionSessions[1].DisplayName != "Slimference audit" ||
		got.DecisionSessions[1].ProjectPath != "/Users/me/CODE/Slimference" ||
		got.DecisionSessions[1].ClientFamily != "codex_desktop_app" {
		t.Fatalf("bad second session: %+v", got.DecisionSessions[1])
	}
}

func TestSavingsResolvesHashFallbackToLocalCodexThread(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	firstPrompt := "check the project status"
	fallbackID := "fh:" + codexFirstTextHash(firstPrompt)

	prevReplay := replaySessionFn
	prevLookupMeta := lookupCodexThreadMetadataForSavingsFn
	prevLookupWindow := lookupCodexThreadWindowForSavingsFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		lookupCodexThreadMetadataForSavingsFn = prevLookupMeta
		lookupCodexThreadWindowForSavingsFn = prevLookupWindow
	})
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{{
			RequestID:    "req-fallback",
			Timestamp:    now,
			SessionID:    fallbackID,
			Source:       "proxy",
			Provider:     "codex_chatgpt",
			Path:         "/backend-api/codex/responses",
			ClientFamily: "codex_cli",
			Model:        "gpt-5.5",
			Tokens:       dbg.TokenCounts{Original: 1000, Final: 700, Saved: 300},
		}}, nil
	}
	lookupCodexThreadMetadataForSavingsFn = func(ids []string) (map[string]codexthreads.Metadata, error) {
		if len(ids) != 1 || ids[0] != "thread-local" {
			t.Fatalf("thread lookup ids=%v", ids)
		}
		return map[string]codexthreads.Metadata{}, nil
	}
	lookupCodexThreadWindowForSavingsFn = func(start, end time.Time) ([]codexthreads.Metadata, error) {
		if start.After(now) || end.Before(now) {
			t.Fatalf("bad lookup window start=%s end=%s now=%s", start, end, now)
		}
		return []codexthreads.Metadata{
			{
				ID:               "thread-local",
				Title:            "› check project status",
				CWD:              "/Users/me/CODE/Demo",
				Source:           "cli",
				ThreadSource:     "user",
				Model:            "gpt-5.5",
				FirstUserMessage: firstPrompt,
				UpdatedAt:        now,
			},
		}, nil
	}

	var got SavingsSummary
	accumulateDecisionMechanismsFromDecisionLog(&got, cfg, "today", now)
	if got.DecisionCodexRequests != 1 ||
		got.DecisionCodexAttributedRequests != 1 ||
		got.DecisionCodexUnattributedRequests != 0 ||
		got.DecisionCodexAttributionStatus != "ok" {
		t.Fatalf("fallback should resolve cleanly: %+v", got)
	}
	if len(got.DecisionSessions) != 1 {
		t.Fatalf("sessions=%d: %+v", len(got.DecisionSessions), got.DecisionSessions)
	}
	session := got.DecisionSessions[0]
	if session.SessionID != "codex-local:thread-local" ||
		session.DisplayName != "check project status" ||
		session.ProjectPath != "/Users/me/CODE/Demo" ||
		session.ClientFamily != "codex_cli" {
		t.Fatalf("bad resolved session: %+v", session)
	}
}

func TestSavingsHashFallbackMatchesProxyHashWithoutTrimming(t *testing.T) {
	prompt := "  check the project status  "
	fallbackID := "fh:" + codexFirstTextHash(prompt)
	meta, ok := resolveLocalCodexFallbackByHash(
		savingsSessionFacts{SessionID: fallbackID},
		[]codexthreads.Metadata{{ID: "thread-local", FirstUserMessage: prompt}},
	)
	if !ok || meta.ID != "thread-local" {
		t.Fatalf("hash fallback must match the proxy content hash byte-for-byte, ok=%v meta=%+v", ok, meta)
	}
	_, ok = resolveLocalCodexFallbackByHash(
		savingsSessionFacts{SessionID: fallbackID},
		[]codexthreads.Metadata{{ID: "thread-trimmed", FirstUserMessage: strings.TrimSpace(prompt)}},
	)
	if ok {
		t.Fatalf("trimmed local text must not match a proxy hash for the untrimmed first user text")
	}
}

func TestSavingsKeepsAmbiguousHashFallbackUnattributed(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")

	prevReplay := replaySessionFn
	prevLookupWindow := lookupCodexThreadWindowForSavingsFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		lookupCodexThreadWindowForSavingsFn = prevLookupWindow
	})
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{{
			RequestID:    "req-ambiguous",
			Timestamp:    now,
			SessionID:    "fh:aaaaaaaaaaaaaaaa",
			Source:       "proxy",
			Provider:     "codex_chatgpt",
			Path:         "/backend-api/codex/responses",
			ClientFamily: "codex",
			Model:        "gpt-5.5",
			Tokens:       dbg.TokenCounts{Original: 1000, Final: 900, Saved: 100},
		}}, nil
	}
	lookupCodexThreadWindowForSavingsFn = func(time.Time, time.Time) ([]codexthreads.Metadata, error) {
		return []codexthreads.Metadata{
			{ID: "thread-a", Source: "cli", Model: "gpt-5.5", UpdatedAt: now},
			{ID: "thread-b", Source: "vscode", Model: "gpt-5.5", UpdatedAt: now},
		}, nil
	}

	var got SavingsSummary
	accumulateDecisionMechanismsFromDecisionLog(&got, cfg, "today", now)
	if got.DecisionCodexRequests != 1 ||
		got.DecisionCodexAttributedRequests != 0 ||
		got.DecisionCodexUnattributedRequests != 1 ||
		got.DecisionCodexAttributionStatus != "attention" {
		t.Fatalf("ambiguous fallback must stay unattributed: %+v", got)
	}
	if got.DecisionCodexUnattributedReasons["ambiguous_thread_candidates"] != 1 {
		t.Fatalf("ambiguous reason missing: %+v", got.DecisionCodexUnattributedReasons)
	}
	if len(got.DecisionSessions) != 1 || got.DecisionSessions[0].SessionID != "fh:aaaaaaaaaaaaaaaa" {
		t.Fatalf("ambiguous session should remain fallback: %+v", got.DecisionSessions)
	}
}

func TestSavingsResolvesAnonymousCodexFallbackByUniqueActivityEnvelope(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")

	prevReplay := replaySessionFn
	prevLookupMeta := lookupCodexThreadMetadataForSavingsFn
	prevLookupWindow := lookupCodexThreadWindowForSavingsFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		lookupCodexThreadMetadataForSavingsFn = prevLookupMeta
		lookupCodexThreadWindowForSavingsFn = prevLookupWindow
	})
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{
			{
				RequestID: "zero-ping",
				Timestamp: now.Add(-6 * time.Hour),
				Source:    "proxy",
				Provider:  "codex_chatgpt",
				Path:      "/backend-api/codex/responses",
			},
			{
				RequestID: "req-anon-1",
				Timestamp: now,
				SessionID: "empty",
				Source:    "proxy",
				Provider:  "codex_chatgpt",
				Path:      "/backend-api/codex/responses",
				Model:     "gpt-5.5",
				Tokens:    dbg.TokenCounts{Original: 1000, Final: 700, Saved: 300},
			},
			{
				RequestID: "req-anon-2",
				Timestamp: now.Add(10 * time.Minute),
				SessionID: "empty",
				Source:    "proxy",
				Provider:  "codex_chatgpt",
				Path:      "/backend-api/codex/responses",
				Model:     "gpt-5.5",
				Tokens:    dbg.TokenCounts{Original: 1200, Final: 800, Saved: 400},
			},
		}, nil
	}
	lookupCodexThreadMetadataForSavingsFn = func([]string) (map[string]codexthreads.Metadata, error) {
		return map[string]codexthreads.Metadata{}, nil
	}
	lookupCodexThreadWindowForSavingsFn = func(time.Time, time.Time) ([]codexthreads.Metadata, error) {
		return []codexthreads.Metadata{
			{
				ID:        "thread-local",
				Title:     "› check project",
				CWD:       "/Users/me/CODE/Demo",
				Source:    "cli",
				Model:     "gpt-5.5",
				CreatedAt: now.Add(-30 * time.Minute),
				UpdatedAt: now.Add(20 * time.Minute),
			},
			{
				ID:        "thread-too-late",
				Source:    "cli",
				Model:     "gpt-5.5",
				CreatedAt: now.Add(6 * time.Minute),
				UpdatedAt: now.Add(20 * time.Minute),
			},
		}, nil
	}

	var got SavingsSummary
	accumulateDecisionMechanismsFromDecisionLog(&got, cfg, "today", now.Add(20*time.Minute))
	if got.DecisionCodexRequests != 3 ||
		got.DecisionCodexAttributedRequests != 3 ||
		got.DecisionCodexUnattributedRequests != 0 ||
		got.DecisionCodexAttributionStatus != "ok" {
		t.Fatalf("anonymous fallback should resolve cleanly: %+v", got)
	}
	if len(got.DecisionSessions) != 1 {
		t.Fatalf("sessions=%d: %+v", len(got.DecisionSessions), got.DecisionSessions)
	}
	session := got.DecisionSessions[0]
	if session.SessionID != "codex-local:thread-local" ||
		session.DisplayName != "check project" ||
		session.ProjectPath != "/Users/me/CODE/Demo" ||
		session.ClientFamily != "codex_cli" ||
		session.Requests != 3 {
		t.Fatalf("bad resolved anonymous session: %+v", session)
	}
}

func TestSavingsKeepsAmbiguousAnonymousCodexFallbackUnattributed(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	facts := savingsSessionFacts{
		SessionID:              "no-session:proxy",
		Model:                  "gpt-5.5",
		CandidateFirstSeen:     now,
		CandidateLastSeen:      now.Add(10 * time.Minute),
		CodexCandidateRequests: 2,
	}
	candidates := []codexthreads.Metadata{
		{ID: "thread-a", Source: "cli", Model: "gpt-5.5", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(time.Hour)},
		{ID: "thread-b", Source: "vscode", Model: "gpt-5.5", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(time.Hour)},
	}
	if _, reason, ok := resolveLocalCodexFallbackMetadata(facts, candidates); ok || reason != "ambiguous_thread_candidates" {
		t.Fatalf("ambiguous anonymous fallback must stay unattributed")
	}
}

func TestSavingsCodexAttributionHealth(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")

	prevReplay := replaySessionFn
	prevLookup := lookupCodexThreadMetadataForSavingsFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		lookupCodexThreadMetadataForSavingsFn = prevLookup
	})
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{
			{
				RequestID: "codex-http",
				Timestamp: now,
				SessionID: "codex-http:thread-http",
				Provider:  "codex_chatgpt",
				Tokens:    dbg.TokenCounts{Original: 100, Final: 80, Saved: 20},
			},
			{
				RequestID: "codex-anon",
				Timestamp: now,
				Source:    "proxy",
				Provider:  "codex_chatgpt",
				Tokens:    dbg.TokenCounts{Original: 50, Final: 50},
			},
			{
				RequestID: "openai",
				Timestamp: now,
				Source:    "proxy",
				Provider:  "openai",
				Tokens:    dbg.TokenCounts{Original: 200, Final: 100, Saved: 100},
			},
			{
				RequestID: "codex-models-sideband",
				Timestamp: now,
				Source:    "proxy",
				Provider:  "codex_chatgpt",
				Path:      "/backend-api/codex/models",
			},
		}, nil
	}
	lookupCodexThreadMetadataForSavingsFn = func(ids []string) (map[string]codexthreads.Metadata, error) {
		if len(ids) != 1 || ids[0] != "thread-http" {
			t.Fatalf("thread lookup ids=%v", ids)
		}
		return map[string]codexthreads.Metadata{}, nil
	}

	var got SavingsSummary
	accumulateDecisionMechanismsFromDecisionLog(&got, cfg, "today", now)
	if got.DecisionCodexRequests != 2 ||
		got.DecisionCodexAttributedRequests != 1 ||
		got.DecisionCodexUnattributedRequests != 1 ||
		!nearFloat(got.DecisionCodexAttributionRate, 0.5) {
		t.Fatalf("bad Codex attribution health: %+v", got)
	}
	text := formatSavingsText(got)
	if !strings.Contains(text, "Codex attribution:") || !strings.Contains(text, "1/2 attributed (attention, 50.0%, 1 unattributed)") {
		t.Fatalf("text missing attribution health: %s", text)
	}
	if !strings.Contains(text, "Codex unattributed reasons:") || !strings.Contains(text, "no_local_thread_candidates=1") {
		t.Fatalf("text missing unattributed reason: %s", text)
	}
	csv := formatSavingsCSV(got)
	for _, want := range []string{"decision_cache_status", "decision_codex_requests", "decision_codex_attributed_requests", "decision_codex_unattributed_requests", "decision_codex_unattributed_reasons", "decision_codex_attribution_rate", "decision_codex_attribution_status", ",none,2,1,1,no_local_thread_candidates=1,0.500000,attention,"} {
		if !strings.Contains(csv, want) {
			t.Fatalf("csv missing %q: %s", want, csv)
		}
	}
}

func TestAccumulateDecisionMechanismsBranches(t *testing.T) {
	cfg := config.Defaults()
	out := SavingsSummary{}
	accumulateDecisionMechanismsFromDecisionLog(&out, cfg, "today", time.Now())
	if out.DecisionRequests != 0 {
		t.Fatalf("empty decisions_log should not mutate summary: %+v", out)
	}

	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	prevReplay := replaySessionFn
	t.Cleanup(func() { replaySessionFn = prevReplay })
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return nil, errors.New("replay")
	}
	accumulateDecisionMechanismsFromDecisionLog(&out, cfg, "today", time.Now())
	if out.DecisionRequests != 0 {
		t.Fatalf("replay error should not mutate summary: %+v", out)
	}

	replaySessionFn = func(string) ([]dbg.RequestSummary, error) { return nil, nil }
	accumulateDecisionMechanismsFromDecisionLog(&out, cfg, "bad-period", time.Now())
	if out.DecisionRequests != 0 {
		t.Fatalf("bad period should not mutate summary: %+v", out)
	}
}

func TestComputeSavingsLiveUsesCurrentDaemonWindow(t *testing.T) {
	now := time.Date(2026, 6, 8, 18, 0, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")

	prevReplay := replaySessionFn
	prevPath := resolveFilterDBPathFn
	prevDaemon := daemonIsRunningFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		resolveFilterDBPathFn = prevPath
		daemonIsRunningFn = prevDaemon
	})
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 1234, Port: 8990, StartedAt: startedAt}, nil
	}
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{
			{
				RequestID: "old-anon",
				Timestamp: startedAt.Add(-time.Second),
				Source:    "proxy",
				Provider:  "codex_chatgpt",
				Path:      "/backend-api/codex/responses",
				Tokens:    dbg.TokenCounts{Original: 1000, Final: 1000},
			},
			{
				RequestID:         "live-thread",
				Timestamp:         startedAt.Add(time.Second),
				SessionID:         "codex-http:thread-live",
				Source:            "proxy",
				Provider:          "codex_chatgpt",
				Path:              "/backend-api/codex/responses",
				Tokens:            dbg.TokenCounts{Original: 1000, Final: 700, Saved: 300},
				CacheReadTokens:   200,
				CacheCreateTokens: 20,
			},
		}, nil
	}

	got := computeSavings(cfg, "live", "", now)
	if got.DecisionRequests != 1 || got.DecisionNetSavedTokens != 300 {
		t.Fatalf("live window included stale rows: %+v", got)
	}
	if got.DecisionCodexRequests != 1 ||
		got.DecisionCodexAttributedRequests != 1 ||
		got.DecisionCodexUnattributedRequests != 0 ||
		got.DecisionCodexAttributionStatus != "ok" {
		t.Fatalf("live attribution should be clean: %+v", got)
	}
	if got.DecisionCacheNetTokens != 180 || got.DecisionCacheStatus != "ok" || got.DecisionCacheNegativeNetRequests != 0 {
		t.Fatalf("live cache health should be clean: %+v", got)
	}
	if len(got.DecisionSessions) != 1 || got.DecisionSessions[0].SessionID != "codex-http:thread-live" {
		t.Fatalf("live sessions: %+v", got.DecisionSessions)
	}
	text := formatSavingsText(got)
	for _, want := range []string{"Slimference savings (live)", "Codex attribution:", "1/1 attributed (ok, 100.0%, 0 unattributed)", "Decision cache net", "180"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %s", want, text)
		}
	}
}

func TestFormatSavingsTextDecisionCacheAndSigned(t *testing.T) {
	if got := formatSignedInt64Plain(42); got != "42" {
		t.Fatalf("positive signed format: %q", got)
	}
	if got := formatSignedInt64Plain(-42); got != "-42" {
		t.Fatalf("negative signed format: %q", got)
	}
	s := SavingsSummary{
		Period:                         "today",
		DecisionRequests:               1,
		DecisionOriginalTokens:         100,
		DecisionFinalTokens:            80,
		DecisionAddedTokens:            5,
		DecisionNetSavedTokens:         20,
		DecisionOutputTokens:           7,
		DecisionCacheReadTokens:        11,
		DecisionCacheCreateTokens:      3,
		DecisionCacheNetTokens:         8,
		DecisionCacheHitRequests:       1,
		DecisionCacheHitRate:           1,
		DecisionCacheCreateRequests:    1,
		DecisionLayer0NetTokens:        2,
		DecisionLayer1NetTokens:        4,
		DecisionLayer2NetTokens:        6,
		DecisionOutputReduceTokens:     -1,
		DecisionToolPruneTokens:        8,
		DecisionEstimatedCostBeforeUSD: 0.10,
		DecisionEstimatedCostAfterUSD:  0.07,
		DecisionEstimatedCostSavedUSD:  0.03,
		Evidence: SavingsEvidenceSummary{
			Decisions:      2,
			Applied:        1,
			FullPass:       1,
			NetTokens:      3,
			ByContentClass: map[string]int64{"search": 1, "test": 1},
			BySignal:       map[string]int64{"error_keyword": 1, "cache_hot_zone": 1},
			ByCacheImpact:  map[string]int64{"provider_cache_read": 1},
		},
		Mechanisms: []SavingsMechanismSummary{
			{Name: "m00", NetTokens: 10, SavedTokens: 10, Count: 1},
			{Name: "m01", NetTokens: 9, SavedTokens: 9, Count: 1},
			{Name: "m02", NetTokens: 8, SavedTokens: 8, Count: 1},
			{Name: "m03", NetTokens: 7, SavedTokens: 7, Count: 1},
			{Name: "m04", NetTokens: 6, SavedTokens: 6, Count: 1},
			{Name: "m05", NetTokens: 5, SavedTokens: 5, Count: 1},
			{Name: "m06", NetTokens: 4, SavedTokens: 4, Count: 1},
			{Name: "negative", NetTokens: -5, AddedTokens: 5, Count: 1},
			{Name: "hidden", NetTokens: -6, AddedTokens: 6, Count: 1},
		},
		DecisionSessions: []SavingsSessionSummary{
			{SessionID: "s0", NetSavedTokens: 10, Layer1NetTokens: 10, CacheNetTokens: 8, CacheHitRate: 1, OriginalTokens: 100, FinalTokens: 90, CostBeforeUSD: 0.10, CostAfterUSD: 0.09, Requests: 1},
			{SessionID: "s1", NetSavedTokens: 9, Layer2NetTokens: 9, OriginalTokens: 100, FinalTokens: 91, Requests: 1},
			{SessionID: "s2", NetSavedTokens: 8, OriginalTokens: 100, FinalTokens: 92, Requests: 1},
			{SessionID: "s3", NetSavedTokens: 7, OriginalTokens: 100, FinalTokens: 93, Requests: 1},
			{SessionID: "s4", NetSavedTokens: 6, OriginalTokens: 100, FinalTokens: 94, Requests: 1},
			{SessionID: "hidden", NetSavedTokens: 5, OriginalTokens: 100, FinalTokens: 95, Requests: 1},
		},
	}
	text := formatSavingsText(s)
	for _, want := range []string{"Decision output tokens", "Decision cache read/create", "Decision cache net", "100.0% hit", "L0=2,L1=4,L2=6,out=-1,tools=8", "Evidence decisions", "search=1", "cache_hot_zone=1", "provider_cache_read=1", "layers=L1=10", "layers=L2=9", "layers=none", "Decision cost before/after", "cost=~$0.1000/~$0.0900", "cache=8/100.0%", "net=-5"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %s", want, text)
		}
	}
}

func TestComputeSavingsDetectsNegativeCacheNet(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	prevReplay := replaySessionFn
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		resolveFilterDBPathFn = prevPath
	})
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{{
			Timestamp:         now,
			SessionID:         "cache-regression",
			CacheCreateTokens: 90,
			Tokens:            dbg.TokenCounts{Original: 1000, Final: 1000},
		}}, nil
	}

	got := computeSavings(cfg, "today", "", now)
	if got.DecisionCacheNetTokens != -90 || got.DecisionCacheNegativeNetRequests != 1 {
		t.Fatalf("negative cache net not surfaced: %+v", got)
	}
	if len(got.DecisionSessions) != 1 || got.DecisionSessions[0].CacheNetTokens != -90 {
		t.Fatalf("session negative cache net not surfaced: %+v", got.DecisionSessions)
	}
	text := formatSavingsText(got)
	for _, want := range []string{"Decision cache net", "-90", "1 negative net", "cache=-90/0.0%"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %s", want, text)
		}
	}
}

func TestComputeSavingsSessionScorecardSplitsLocalCacheAndEffectiveBilled(t *testing.T) {
	base := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	reportNow := base.Add(10 * time.Second)
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	prevReplay := replaySessionFn
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		resolveFilterDBPathFn = prevPath
	})
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{
			{
				RequestID:              "cached-1",
				Timestamp:              base,
				SessionID:              "codex-wss:scorecard",
				Provider:               "codex_chatgpt",
				ProviderInputTokens:    1000,
				ProviderCachedTokens:   600,
				ProviderOutputTokens:   40,
				PreviousResponseIDUsed: true,
				Tokens:                 dbg.TokenCounts{Original: 900, Final: 700, Saved: 200},
			},
			{
				RequestID:              "cached-2",
				Timestamp:              base.Add(time.Second),
				SessionID:              "codex-wss:scorecard",
				Provider:               "codex_chatgpt",
				ProviderInputTokens:    500,
				ProviderCachedTokens:   100,
				ProviderOutputTokens:   20,
				CacheCreateTokens:      50,
				PreviousResponseIDUsed: true,
				Tokens:                 dbg.TokenCounts{Original: 400, Final: 300, Saved: 100},
			},
		}, nil
	}

	got := computeSavings(cfg, "today", "", reportNow)
	if got.DecisionProviderInputTokens != 1500 ||
		got.DecisionLocalSavedTokens != 300 ||
		got.DecisionNetSavedTokens != 300 ||
		got.DecisionCacheReadTokens != 700 ||
		got.DecisionCacheCreateTokens != 50 ||
		got.DecisionEffectiveBilledTokens != 920 ||
		got.DecisionCounterfactualTokens != 1220 ||
		got.DecisionUncachedCounterfactual != 1800 ||
		!nearFloat(got.DecisionCachedShare, 700.0/1500.0) {
		t.Fatalf("bad aggregate scorecard: %+v", got)
	}
	if !nearFloat(got.CachedPriceRatio, 0.10) ||
		!nearFloat(got.DecisionLocalSavingsRate, 300.0/1800.0) ||
		!nearFloat(got.DecisionCombinedSavingsRate, 300.0/1220.0) ||
		!nearFloat(got.DecisionVsUncachedSavingsRate, 880.0/1800.0) {
		t.Fatalf("bad aggregate scorecard rates: %+v", got)
	}
	if len(got.DecisionSessions) != 1 {
		t.Fatalf("sessions=%d: %+v", len(got.DecisionSessions), got.DecisionSessions)
	}
	session := got.DecisionSessions[0]
	if session.LocalSaved != 300 ||
		session.NetSavedTokens != 300 ||
		session.ProviderInputTokens != 1500 ||
		session.CacheReadTokens != 700 ||
		session.CacheCreateTokens != 50 ||
		session.EffectiveBilled != 920 ||
		!nearFloat(session.CachedShare, 700.0/1500.0) {
		t.Fatalf("bad session scorecard: %+v", session)
	}
	if session.Scorecard == nil ||
		session.Scorecard.CounterfactualTokens != 1220 ||
		session.Scorecard.UncachedCounterfactual != 1800 ||
		session.Scorecard.EffectiveBilledTokens != 920 ||
		!nearFloat(session.Scorecard.LocalSavingsRate, 300.0/1800.0) ||
		!nearFloat(session.Scorecard.CombinedSavingsRate, 300.0/1220.0) ||
		!nearFloat(session.Scorecard.VsUncachedSavingsRate, 880.0/1800.0) {
		t.Fatalf("bad nested session scorecard: %+v", session.Scorecard)
	}
	text := formatSavingsText(got)
	for _, want := range []string{
		"Decision local saved tokens: 300",
		"Decision effective billed:   920",
		"Decision cached share:       46.7%",
		"Decision scorecard:          S_local=16.7% S_combined=24.6% S_vs_uncached=48.9%",
		"local_saved=300",
		"effective_billed=920",
		"cached_share=46.7%",
		"S_local=16.7%",
		"S_combined=24.6%",
		"S_vs_uncached=48.9%",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %s", want, text)
		}
	}
	csv := formatSavingsCSV(got)
	for _, want := range []string{"cached_price_ratio", "decision_local_saved_tokens", "decision_cached_share", "decision_effective_billed_tokens", "decision_s_local", "decision_s_combined", "decision_s_vs_uncached"} {
		if !strings.Contains(csv, want) {
			t.Fatalf("csv missing %q: %s", want, csv)
		}
	}
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 2 {
		t.Fatalf("csv lines=%d: %s", len(lines), csv)
	}
	headers := strings.Split(lines[0], ",")
	values := strings.Split(lines[1], ",")
	if len(headers) != len(values) {
		t.Fatalf("csv header/value mismatch: %d headers, %d values: %s", len(headers), len(values), csv)
	}
	byHeader := map[string]string{}
	for i, header := range headers {
		byHeader[header] = values[i]
	}
	for header, want := range map[string]string{
		"decision_provider_input_tokens":          "1500",
		"decision_local_saved_tokens":             "300",
		"decision_cached_share":                   "0.466667",
		"decision_effective_billed_tokens":        "920",
		"decision_counterfactual_tokens":          "1220",
		"decision_uncached_counterfactual_tokens": "1800",
		"decision_s_local":                        "0.166667",
		"decision_s_combined":                     "0.245902",
		"decision_s_vs_uncached":                  "0.488889",
	} {
		if got := byHeader[header]; got != want {
			t.Fatalf("csv %s=%q, want %q: %s", header, got, want, csv)
		}
	}
}

func TestComputeSavingsAggregateScorecardIncludesFallbackOnlySessions(t *testing.T) {
	base := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	reportNow := base.Add(10 * time.Second)
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	prevReplay := replaySessionFn
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		resolveFilterDBPathFn = prevPath
	})
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{
			{
				RequestID:              "cached",
				Timestamp:              base,
				SessionID:              "codex-wss:cached",
				Provider:               "codex_chatgpt",
				ClientFamily:           "codex_cli",
				RouteMode:              "websocket_phasef",
				ProviderInputTokens:    1000,
				ProviderCachedTokens:   600,
				PreviousResponseIDUsed: true,
				Tokens:                 dbg.TokenCounts{Original: 900, Final: 700, Saved: 200},
			},
			{
				RequestID:    "fallback-only",
				Timestamp:    base.Add(time.Second),
				SessionID:    "codex-wss:fallback",
				Provider:     "codex_chatgpt",
				ClientFamily: "codex_desktop_app",
				RouteMode:    "websocket_phasef",
				Tokens:       dbg.TokenCounts{Original: 1000, Final: 900, Saved: 100},
				EvidenceDecisions: []evidence.BlockDecision{{
					Action:    evidence.ActionFullPass,
					NetTokens: -100,
				}},
			},
		}, nil
	}

	got := computeSavings(cfg, "today", "", reportNow)
	if got.DecisionEffectiveBilledTokens != 1360 ||
		got.DecisionCounterfactualTokens != 1660 ||
		got.DecisionUncachedCounterfactual != 2200 {
		t.Fatalf("aggregate scorecard dropped fallback-only session: %+v", got)
	}
	if !nearFloat(got.DecisionCachedShare, 600.0/1900.0) ||
		!nearFloat(got.DecisionLocalSavingsRate, 300.0/2200.0) ||
		!nearFloat(got.DecisionCombinedSavingsRate, 300.0/1660.0) ||
		!nearFloat(got.DecisionVsUncachedSavingsRate, 840.0/2200.0) {
		t.Fatalf("bad mixed aggregate rates: %+v", got)
	}
	if len(got.DecisionRoutes) != 2 {
		t.Fatalf("routes=%d: %+v", len(got.DecisionRoutes), got.DecisionRoutes)
	}
	routes := map[string]SavingsRouteSummary{}
	for _, route := range got.DecisionRoutes {
		routes[route.RouteKey] = route
	}
	cli := routes["codex_cli/websocket_phasef"]
	if cli.Sessions != 1 ||
		cli.Requests != 1 ||
		cli.ProviderInputTokens != 1000 ||
		cli.CacheReadTokens != 600 ||
		cli.EffectiveBilled != 460 ||
		cli.Scorecard == nil ||
		cli.Evidence == nil ||
		cli.Evidence.Decisions != 1 ||
		cli.Evidence.Applied != 1 ||
		cli.Evidence.ByCacheImpact["provider_cache_read"] != 1 ||
		!nearFloat(cli.Scorecard.CombinedSavingsRate, 200.0/660.0) {
		t.Fatalf("bad cli route: %+v evidence=%+v", cli, cli.Evidence)
	}
	desktop := routes["codex_desktop_app/websocket_phasef"]
	if desktop.Sessions != 1 ||
		desktop.Requests != 1 ||
		desktop.ProviderInputTokens != 0 ||
		desktop.EffectiveBilled != 900 ||
		desktop.Scorecard == nil ||
		desktop.Evidence == nil ||
		desktop.Evidence.Decisions != 1 ||
		desktop.Evidence.FullPass != 1 ||
		!nearFloat(desktop.Scorecard.CombinedSavingsRate, 100.0/1000.0) {
		t.Fatalf("bad desktop route: %+v evidence=%+v", desktop, desktop.Evidence)
	}
	text := formatSavingsText(got)
	for _, want := range []string{
		"Decision effective billed:   1.4K",
		"Decision cached share:       31.6%",
		"Decision scorecard:          S_local=13.6% S_combined=18.1% S_vs_uncached=38.2%",
		"Decision route scorecards:",
		"route codex_cli/websocket_phasef",
		"evidence=1/0/0/0",
		"route codex_desktop_app/websocket_p...",
		"evidence=0/1/0/0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %s", want, text)
		}
	}
}

func TestSavingsScorecardUsesConfiguredCachedPriceRatio(t *testing.T) {
	scorecard := savingsBuildScorecard(1000, 0, 200, 500, 0, 0, 0.25)
	if scorecard.EffectiveBilledTokens != 625 ||
		scorecard.CounterfactualTokens != 825 ||
		scorecard.UncachedCounterfactual != 1200 ||
		!nearFloat(scorecard.CachedPriceRatio, 0.25) ||
		!nearFloat(scorecard.LocalSavingsRate, 200.0/1200.0) ||
		!nearFloat(scorecard.CombinedSavingsRate, 200.0/825.0) ||
		!nearFloat(scorecard.VsUncachedSavingsRate, 575.0/1200.0) {
		t.Fatalf("scorecard: %+v", scorecard)
	}
}

func TestSavingsRouteSummariesUseSessionScorecardMath(t *testing.T) {
	routes := savingsBuildRouteSummaries([]SavingsSessionSummary{
		{
			ClientFamily:             "codex_cli",
			RouteMode:                "websocket_phasef",
			Requests:                 1,
			ProviderInputTokens:      1000,
			FinalTokens:              700,
			LocalSaved:               200,
			CacheReadTokens:          600,
			CacheHitRequests:         1,
			CompoundedEstimateTokens: 40,
			Scorecard: &SavingsScorecard{
				CachedPriceRatio:         0.10,
				CounterfactualTokens:     660,
				UncachedCounterfactual:   1200,
				EffectiveBilledTokens:    460,
				CompoundedEstimateTokens: 40,
			},
			Evidence: &SavingsEvidenceSummary{
				Decisions:     1,
				Applied:       1,
				ByCacheImpact: map[string]int64{"provider_cache_read": 1},
			},
		},
		{
			ClientFamily:             "codex_cli",
			RouteMode:                "websocket_phasef",
			Requests:                 1,
			FinalTokens:              900,
			LocalSaved:               100,
			CacheHitRequests:         0,
			CompoundedEstimateTokens: 10,
			Scorecard: &SavingsScorecard{
				CachedPriceRatio:         0.10,
				CounterfactualTokens:     1000,
				UncachedCounterfactual:   1000,
				EffectiveBilledTokens:    900,
				CompoundedEstimateTokens: 10,
			},
			Evidence: &SavingsEvidenceSummary{
				Decisions:  1,
				FailedOpen: 1,
				BySignal:   map[string]int64{"schema_drift": 1},
			},
		},
	}, 0.10)
	if len(routes) != 1 {
		t.Fatalf("routes=%d: %+v", len(routes), routes)
	}
	route := routes[0]
	if route.RouteKey != "codex_cli/websocket_phasef" ||
		route.Requests != 2 ||
		route.ProviderInputTokens != 1000 ||
		route.LocalSaved != 300 ||
		route.CacheReadTokens != 600 ||
		route.EffectiveBilled != 1360 ||
		route.CompoundedEstimateTokens != 50 ||
		route.Scorecard == nil ||
		route.Scorecard.CompoundedEstimateTokens != 50 ||
		route.Scorecard.CounterfactualTokens != 1660 ||
		route.Scorecard.UncachedCounterfactual != 2200 ||
		route.Evidence == nil ||
		route.Evidence.Decisions != 2 ||
		route.Evidence.Applied != 1 ||
		route.Evidence.FailedOpen != 1 ||
		route.Evidence.ByCacheImpact["provider_cache_read"] != 1 ||
		route.Evidence.BySignal["schema_drift"] != 1 ||
		!nearFloat(route.CachedShare, 600.0/1900.0) ||
		!nearFloat(route.Scorecard.CombinedSavingsRate, 300.0/1660.0) ||
		!nearFloat(route.Scorecard.VsUncachedSavingsRate, 840.0/2200.0) {
		t.Fatalf("route scorecard dropped fallback-only session: %+v", route)
	}
	text := formatSavingsText(SavingsSummary{Period: "today", DecisionRequests: 1, DecisionRoutes: routes})
	if !strings.Contains(text, "route codex_cli/websocket_phasef") ||
		!strings.Contains(text, "cache=0/50.0% compounded_est=50") {
		t.Fatalf("route text should surface compounded estimate: %s", text)
	}
}

func TestComputeSavingsAccountsUpstreamRetryNegativeEvents(t *testing.T) {
	base := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	reportNow := base.Add(10 * time.Second)
	cfg := config.Defaults()
	cfg.Analytics.LogDir = t.TempDir()
	cfg.Analytics.GainUSDPerMillionTokens = 2.5
	cfg.Debug.DecisionsLog = filepath.Join(t.TempDir(), "decisions.jsonl")
	prevReplay := replaySessionFn
	prevPath := resolveFilterDBPathFn
	t.Cleanup(func() {
		replaySessionFn = prevReplay
		resolveFilterDBPathFn = prevPath
	})
	resolveFilterDBPathFn = func() (string, error) { return "/no/such/file.db", nil }
	replaySessionFn = func(string) ([]dbg.RequestSummary, error) {
		return []dbg.RequestSummary{
			{
				RequestID:              "delta-1",
				Timestamp:              base,
				SessionID:              "codex-wss:retry-thread",
				Provider:               "codex_chatgpt",
				PreviousResponseIDUsed: true,
				Tokens:                 dbg.TokenCounts{Original: 1000, Final: 900, Saved: 100},
			},
			{
				RequestID:              "delta-2",
				Timestamp:              base.Add(time.Second),
				SessionID:              "codex-wss:retry-thread",
				Provider:               "codex_chatgpt",
				PreviousResponseIDUsed: true,
				Tokens:                 dbg.TokenCounts{Original: 1200, Final: 1100, Saved: 100},
			},
			{
				RequestID:    "upstream-400",
				Timestamp:    base.Add(2 * time.Second),
				SessionID:    "codex-wss:retry-thread",
				Provider:     "codex_chatgpt",
				BypassReason: "upstream_error",
			},
			{
				RequestID: "full-retry",
				Timestamp: base.Add(3 * time.Second),
				SessionID: "codex-wss:retry-thread",
				Provider:  "codex_chatgpt",
				Tokens:    dbg.TokenCounts{Original: 32000, Final: 32000},
			},
		}, nil
	}

	got := computeSavings(cfg, "today", "", reportNow)
	if got.DecisionNegativeEvents != 1 || got.DecisionNegativeEventTokens != 30900 {
		t.Fatalf("negative retry event not accounted: %+v", got)
	}
	if got.DecisionNetSavedTokens != -30700 {
		t.Fatalf("decision net should subtract retry extra, got %+v", got)
	}
	if got.TotalSavedTokens != -30700 {
		t.Fatalf("combined total should subtract retry extra, got %+v", got)
	}
	if len(got.DecisionSessions) != 1 ||
		got.DecisionSessions[0].NegativeEvents != 1 ||
		got.DecisionSessions[0].NegativeEventTokens != 30900 ||
		got.DecisionSessions[0].NetSavedTokens != -30700 {
		t.Fatalf("session negative retry event not accounted: %+v", got.DecisionSessions)
	}
	text := formatSavingsText(got)
	for _, want := range []string{"Decision net saved tokens:   -30.7K", "Decision negative events", "1 (-30.9K tokens)", "negative=1/-30.9K", "net=-30.7K", "Total tokens saved:         -30.7K"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %s", want, text)
		}
	}
	csv := formatSavingsCSV(got)
	for _, want := range []string{"decision_negative_events", "decision_negative_event_tokens", "-30700,1,30900"} {
		if !strings.Contains(csv, want) {
			t.Fatalf("csv missing %q: %s", want, csv)
		}
	}
}

func TestSavingsNegativeEventsIgnoreLateRetry(t *testing.T) {
	now := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	events, tokens := savingsNegativeEventsForSession([]dbg.RequestSummary{
		{
			Timestamp:              now,
			PreviousResponseIDUsed: true,
			Tokens:                 dbg.TokenCounts{Original: 1000, Final: 900, Saved: 100},
		},
		{
			Timestamp:    now.Add(time.Second),
			BypassReason: "upstream_error",
		},
		{
			Timestamp: now.Add(3 * time.Minute),
			Tokens:    dbg.TokenCounts{Original: 32000, Final: 32000},
		},
	})
	if events != 0 || tokens != 0 {
		t.Fatalf("late retry must not count as negative event: events=%d tokens=%d", events, tokens)
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

package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

func wssClassDistributionTestSummary(id, shape string, original, saved, prefixTokens, outputBytes, providerCached, reasoningItems int) dbg.RequestSummary {
	return dbg.RequestSummary{
		RequestID:            id,
		Path:                 "/backend-api/codex/responses",
		RouteMode:            "websocket_phasef",
		Tokens:               dbg.TokenCounts{Original: original, Final: original - saved, Saved: saved},
		ProviderCachedTokens: providerCached,
		DebugFacts: map[string]string{
			"wss.request_shape":             shape,
			"wss.prefix_total_bytes":        "36000",
			"wss.prefix_estimated_tokens":   strconv.Itoa(prefixTokens),
			"wss.tool_result_output_bytes":  strconv.Itoa(outputBytes),
			"wss.raw_input_reasoning_items": strconv.Itoa(reasoningItems),
		},
	}
}

func floatNearTest(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("ratio mismatch: got=%.9f want=%.9f", got, want)
	}
}

func TestWSSClassDistributionSplitAndAggregate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	deltaDir := filepath.Join(dir, "cap-delta")
	fullHistDir := filepath.Join(dir, "cap-fullhist")
	if err := os.MkdirAll(deltaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fullHistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Delta turn: prefix-dominated (8000 prefix tokens). saved=500 tool-output
	// plus 4000 remaining output bytes (1000 tokens) -> reducible 1500,
	// other = 10000-8000-1500 = 500.
	writeJSONLFile(t, filepath.Join(deltaDir, "decisions.jsonl"),
		wssClassDistributionTestSummary("d1", "delta", 10000, 500, 8000, 4000, 7000, 2))
	// Full-history turn: saved=8000 tool-output plus 24000 remaining bytes
	// (6000 tokens) -> reducible 14000, other = 20000-2000-14000 = 4000.
	writeJSONLFile(t, filepath.Join(fullHistDir, "decisions.jsonl"),
		wssClassDistributionTestSummary("f1", "full_history", 20000, 8000, 2000, 24000, 1000, 1))

	report, err := loadWSSClassDistribution(wssClassDistributionFlags{path: dir})
	if err != nil {
		t.Fatalf("loadWSSClassDistribution() error = %v", err)
	}

	if report.Logs != 2 || report.PhaseFRequests != 2 {
		t.Fatalf("bad log/request totals: logs=%d phasef=%d", report.Logs, report.PhaseFRequests)
	}
	if report.OriginalTokens != 30000 ||
		report.LocalSavedTokens != 8500 ||
		report.PrefixProtectedTokens != 10000 ||
		report.ReducibleToolOutputTokens != 15500 ||
		report.OtherContextTokens != 4500 ||
		report.NonPrefixTokens != 20000 {
		t.Fatalf("bad composition totals: %+v", report)
	}
	// The three components must sum to original exactly.
	if report.PrefixProtectedTokens+report.ReducibleToolOutputTokens+report.OtherContextTokens != report.OriginalTokens {
		t.Fatalf("composition does not sum to original: %d+%d+%d != %d",
			report.PrefixProtectedTokens, report.ReducibleToolOutputTokens, report.OtherContextTokens, report.OriginalTokens)
	}
	// Reducible ceiling must never be below the actual S_local (the invariant the
	// real-capture run exposed): reducible includes the already-saved tool-output.
	if report.ReducibleToolOutputTokens < report.LocalSavedTokens {
		t.Fatalf("reducible %d < saved %d violates ceiling invariant", report.ReducibleToolOutputTokens, report.LocalSavedTokens)
	}
	if report.ReducibleCeilingDeficit != 0 || report.ReducibleHeadroomTokens != 7000 {
		t.Fatalf("bad ceiling deficit/headroom: deficit=%d headroom=%d", report.ReducibleCeilingDeficit, report.ReducibleHeadroomTokens)
	}
	if report.ReasoningItems != 3 || report.ProviderCachedTokens != 8000 || report.PrefixMutationSavedTokens != 0 {
		t.Fatalf("bad reasoning/cache/prefixmut: reasoning=%d cached=%d prefixmut=%d", report.ReasoningItems, report.ProviderCachedTokens, report.PrefixMutationSavedTokens)
	}
	floatNearTest(t, report.ReducibleCeilingRatio, 15500.0/30000.0)
	floatNearTest(t, report.NonPrefixRatio, 20000.0/30000.0)
	floatNearTest(t, report.LocalSavingsRatio, 8500.0/30000.0)
	if report.Verdict != "headroom_present" {
		t.Fatalf("expected headroom_present (reducible ceiling 51.7%% >= 48%%), got %q: %s", report.Verdict, report.VerdictDetail)
	}

	delta := wssClassDistributionFindClass(report.Classes, "delta")
	fullHist := wssClassDistributionFindClass(report.Classes, "full_history")
	if delta == nil || fullHist == nil {
		t.Fatalf("missing class rows: %+v", report.Classes)
	}
	if delta.ReducibleToolOutputTokens != 1500 || delta.PrefixProtectedTokens != 8000 || delta.OtherContextTokens != 500 {
		t.Fatalf("bad delta class split: %+v", delta)
	}
	if fullHist.ReducibleToolOutputTokens != 14000 || fullHist.PrefixProtectedTokens != 2000 || fullHist.OtherContextTokens != 4000 {
		t.Fatalf("bad full_history class split: %+v", fullHist)
	}
	floatNearTest(t, delta.ReducibleCeilingRatio, 0.15)
	floatNearTest(t, fullHist.ReducibleCeilingRatio, 0.70)
	// Classes sorted by original tokens desc: full_history (20000) before delta (10000).
	if report.Classes[0].Class != "full_history" {
		t.Fatalf("expected full_history first by original tokens, got %q", report.Classes[0].Class)
	}
	// Per-log sorted by reducible ceiling desc: cap-fullhist (70%) before cap-delta (15%).
	if len(report.PerLog) != 2 || report.PerLog[0].Name != "cap-fullhist" || report.PerLog[1].Name != "cap-delta" {
		t.Fatalf("bad per-log ordering: %+v", report.PerLog)
	}
}

func TestWSSClassDistributionRouteCeilingVerdict(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	// Single prefix-dominated delta request: reducible = 500 saved + 1000
	// remaining = 1500; ceiling = 1500/10000 = 15% < 48%.
	writeJSONLFile(t, path,
		wssClassDistributionTestSummary("d1", "delta", 10000, 500, 8000, 4000, 7000, 2))

	report, err := loadWSSClassDistribution(wssClassDistributionFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSClassDistribution() error = %v", err)
	}
	if report.Verdict != "route_ceiling_evidence" {
		t.Fatalf("expected route_ceiling_evidence, got %q: %s", report.Verdict, report.VerdictDetail)
	}
	if report.ReducibleCeilingRatio >= report.TargetRatio {
		t.Fatalf("reducible ceiling %.4f should be below target %.4f", report.ReducibleCeilingRatio, report.TargetRatio)
	}
	// target saved = ceil(10000*0.48) = 4800; reducible = 1500; deficit = 3300.
	if report.ReducibleCeilingDeficit != 3300 {
		t.Fatalf("bad ceiling deficit: %d", report.ReducibleCeilingDeficit)
	}
}

func TestWSSClassDistributionSplitProtectsSavedUnderEstimateOverlap(t *testing.T) {
	t.Parallel()

	// Estimate overlap: hard saved (3000) + the byte/4 prefix estimate (8000)
	// exceed original (10000). The prefix is capped to original - saved (7000)
	// so non-prefix mass stays >= the hard saved tool-output; reducible then
	// stays >= saved (3000), and the parts sum to original.
	overlap := wssClassDistributionTestSummary("ov", "full_history", 10000, 3000, 8000, 4000, 0, 0)
	prefix, reducible, other, prefixMut := wssClassDistributionSplit(overlap, 10000, 3000)
	if prefix != 7000 || reducible != 3000 || other != 0 || prefixMut != 0 {
		t.Fatalf("bad overlap split: prefix=%d reducible=%d other=%d prefixMut=%d", prefix, reducible, other, prefixMut)
	}
	if reducible < 3000 {
		t.Fatalf("reducible %d dropped below saved 3000 (ceiling-below-actual bug)", reducible)
	}
	if prefix+reducible+other != 10000 {
		t.Fatalf("overlap split does not sum to original")
	}

	// Degenerate: remaining output bytes exceed the whole request; reducible caps
	// to the non-prefix mass (2000), prefix estimate (8000) is kept.
	huge := wssClassDistributionTestSummary("big", "full_history", 10000, 0, 8000, 100000, 0, 0)
	prefix, reducible, other, _ = wssClassDistributionSplit(huge, 10000, 0)
	if prefix != 8000 || reducible != 2000 || other != 0 {
		t.Fatalf("bad capped split: prefix=%d reducible=%d other=%d", prefix, reducible, other)
	}
}

func TestWSSClassDistributionExcludesPrefixMutationSavings(t *testing.T) {
	t.Parallel()

	// A lab capture where all savings came from Class-C prefix elision must not
	// count those tokens as reducible tool-output.
	summary := wssClassDistributionTestSummary("elide", "delta", 10000, 3000, 4000, 0, 0, 0)
	summary.DebugFacts["wss.stateful_prefix_elision_tokens_saved"] = "3000"
	prefix, reducible, other, prefixMut := wssClassDistributionSplit(summary, 10000, 3000)
	if prefix != 4000 || reducible != 0 || prefixMut != 3000 || other != 6000 {
		t.Fatalf("prefix-mutation split wrong: prefix=%d reducible=%d other=%d prefixMut=%d", prefix, reducible, other, prefixMut)
	}
}

func TestRunWSSClassDistributionJSONAndText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		wssClassDistributionTestSummary("f1", "full_history", 20000, 8000, 2000, 24000, 1000, 1))

	var stdout, stderr bytes.Buffer
	if code := runWSSClassDistribution([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSClassDistribution text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "WSS Class Distribution") ||
		!strings.Contains(stdout.String(), "Reducible ceiling ratio") ||
		!strings.Contains(stdout.String(), "Non-prefix upper bound") ||
		!strings.Contains(stdout.String(), "Verdict:") ||
		!strings.Contains(stdout.String(), "Per request class:") {
		t.Fatalf("text output missing expected fields:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSClassDistribution([]string{path, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSClassDistribution json code=%d stderr=%s", code, stderr.String())
	}
	var report wssClassDistributionReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, stdout.String())
	}
	if report.PhaseFRequests != 1 ||
		report.ReducibleToolOutputTokens != 14000 ||
		report.PrefixProtectedTokens != 2000 ||
		report.Verdict != "headroom_present" {
		t.Fatalf("bad json report: %+v", report)
	}

	// Help and missing-path paths.
	stdout.Reset()
	stderr.Reset()
	if code := runWSSClassDistribution([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help code=%d", code)
	}
	if !strings.Contains(stdout.String(), "wss-class-distribution") {
		t.Fatalf("help text missing command name")
	}
	stdout.Reset()
	stderr.Reset()
	if code := runWSSClassDistribution(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("missing-path code=%d (want 2)", code)
	}
}

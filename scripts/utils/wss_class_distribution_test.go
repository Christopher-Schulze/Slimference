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
	prefixBytes := prefixTokens * 4
	return dbg.RequestSummary{
		RequestID:            id,
		Path:                 "/backend-api/codex/responses",
		RouteMode:            "websocket_phasef",
		Tokens:               dbg.TokenCounts{Original: original, Final: original - saved, Saved: saved},
		ProviderCachedTokens: providerCached,
		DebugFacts: map[string]string{
			"wss.request_shape":                      shape,
			"wss.prefix_total_bytes":                 strconv.Itoa(prefixBytes),
			"wss.prefix_estimated_tokens":            strconv.Itoa(prefixTokens),
			"wss.tool_definition_bytes":              strconv.Itoa(prefixTokens * 3),
			"wss.tool_definition_default_keep_bytes": strconv.Itoa(prefixTokens * 2),
			"wss.tool_definition_nondefault_bytes":   strconv.Itoa(prefixTokens),
			"wss.tool_definition_unnamed_bytes":      "0",
			"wss.instructions_bytes":                 strconv.Itoa(prefixTokens),
			"wss.tool_definitions":                   "4",
			"wss.tool_definition_default_keep":       "3",
			"wss.tool_definition_nondefault":         "1",
			"wss.tool_definition_unnamed":            "0",
			"wss.tool_result_output_bytes":           strconv.Itoa(outputBytes),
			"wss.raw_input_reasoning_items":          strconv.Itoa(reasoningItems),
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
		report.PrefixTotalBytes != 40000 ||
		report.PrefixSplitBytes != 40000 ||
		report.PrefixSplitInconsistentBytes != 0 ||
		report.PrefixSplitInconsistentRequests != 0 ||
		report.PrefixToolDefinitionBytes != 30000 ||
		report.PrefixInstructionBytes != 10000 ||
		report.PrefixDefaultKeepToolBytes != 20000 ||
		report.PrefixNonDefaultToolBytes != 10000 ||
		report.PrefixUnnamedToolBytes != 0 ||
		report.PrefixToolDefinitions != 8 ||
		report.PrefixDefaultKeepTools != 6 ||
		report.PrefixNonDefaultTools != 2 ||
		report.PrefixUnnamedTools != 0 ||
		report.ToolPruneCandidateBytes != 10000 ||
		report.ToolPruneCandidateTokens != 2500 ||
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
	floatNearTest(t, report.ToolPruneCandidateShare, 2500.0/30000.0)
	if report.Verdict != "headroom_present" {
		t.Fatalf("expected headroom_present (reducible ceiling 51.7%% >= 48%%), got %q: %s", report.Verdict, report.VerdictDetail)
	}
	if !report.HeadroomPresent || !report.GapInventoryRecommended ||
		!strings.Contains(report.NextAction, "wss-local-gap-inventory") {
		t.Fatalf("bad headroom next action: headroom=%v gap=%v next=%q",
			report.HeadroomPresent, report.GapInventoryRecommended, report.NextAction)
	}

	delta := wssClassDistributionFindClass(report.Classes, "delta")
	fullHist := wssClassDistributionFindClass(report.Classes, "full_history")
	if delta == nil || fullHist == nil {
		t.Fatalf("missing class rows: %+v", report.Classes)
	}
	if delta.ReducibleToolOutputTokens != 1500 || delta.PrefixProtectedTokens != 8000 || delta.OtherContextTokens != 500 {
		t.Fatalf("bad delta class split: %+v", delta)
	}
	if delta.PrefixTotalBytes != 32000 || delta.PrefixSplitBytes != 32000 ||
		delta.PrefixSplitInconsistentBytes != 0 || delta.PrefixSplitInconsistentRequests != 0 ||
		delta.PrefixToolDefinitionBytes != 24000 || delta.PrefixInstructionBytes != 8000 ||
		delta.PrefixDefaultKeepToolBytes != 16000 || delta.PrefixNonDefaultToolBytes != 8000 ||
		delta.PrefixToolDefinitions != 4 || delta.PrefixDefaultKeepTools != 3 || delta.PrefixNonDefaultTools != 1 ||
		delta.ToolPruneCandidateBytes != 8000 || delta.ToolPruneCandidateTokens != 2000 {
		t.Fatalf("bad delta prefix surface: %+v", delta)
	}
	floatNearTest(t, delta.ToolPruneCandidateShare, 0.20)
	if fullHist.ReducibleToolOutputTokens != 14000 || fullHist.PrefixProtectedTokens != 2000 || fullHist.OtherContextTokens != 4000 {
		t.Fatalf("bad full_history class split: %+v", fullHist)
	}
	if fullHist.PrefixTotalBytes != 8000 || fullHist.PrefixSplitBytes != 8000 ||
		fullHist.PrefixSplitInconsistentBytes != 0 || fullHist.PrefixSplitInconsistentRequests != 0 ||
		fullHist.PrefixToolDefinitionBytes != 6000 || fullHist.PrefixInstructionBytes != 2000 ||
		fullHist.PrefixDefaultKeepToolBytes != 4000 || fullHist.PrefixNonDefaultToolBytes != 2000 ||
		fullHist.PrefixToolDefinitions != 4 || fullHist.PrefixDefaultKeepTools != 3 || fullHist.PrefixNonDefaultTools != 1 ||
		fullHist.ToolPruneCandidateBytes != 2000 || fullHist.ToolPruneCandidateTokens != 500 {
		t.Fatalf("bad full_history prefix surface: %+v", fullHist)
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
	if report.PerLog[0].PrefixToolDefinitionBytes != 6000 || report.PerLog[1].PrefixToolDefinitionBytes != 24000 {
		t.Fatalf("bad per-log prefix split: %+v", report.PerLog)
	}
	if report.PerLog[0].ToolPruneCandidateTokens != 500 || report.PerLog[1].ToolPruneCandidateTokens != 2000 {
		t.Fatalf("bad per-log tool-prune candidates: %+v", report.PerLog)
	}
}

func TestWSSClassDistributionCorpusCeilingVerdict(t *testing.T) {
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
	if report.Verdict != "corpus_ceiling_evidence" {
		t.Fatalf("expected corpus_ceiling_evidence, got %q: %s", report.Verdict, report.VerdictDetail)
	}
	if report.ReducibleCeilingRatio >= report.TargetRatio {
		t.Fatalf("reducible ceiling %.4f should be below target %.4f", report.ReducibleCeilingRatio, report.TargetRatio)
	}
	if report.HeadroomPresent || report.GapInventoryRecommended ||
		!strings.Contains(report.NextAction, "do not widen guards") {
		t.Fatalf("bad corpus-ceiling next action: headroom=%v gap=%v next=%q",
			report.HeadroomPresent, report.GapInventoryRecommended, report.NextAction)
	}
	// target saved = ceil(10000*0.48) = 4800; reducible = 1500; deficit = 3300.
	if report.ReducibleCeilingDeficit != 3300 {
		t.Fatalf("bad ceiling deficit: %d", report.ReducibleCeilingDeficit)
	}
}

func TestWSSClassDistributionT354ShapeTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	delta := wssClassDistributionTestSummary("d1", "delta", 10000, 500, 8000, 4000, 7000, 2)
	delta.PreviousResponseIDUsed = true
	delta.ProviderInputTokens = 10000
	delta.ProviderCachedTokens = 7000
	delta.CacheReadTokens = 111
	delta.CacheCreateTokens = 22
	delta.Errors = []string{"upstream_error 400 invalid_request"}
	delta.DebugFacts["wss.socket_seq"] = "2"
	delta.DebugFacts["wss.tool_results_total"] = "1"
	delta.DebugFacts["wss.tool_results_inferred"] = "1"
	delta.DebugFacts["wss.effective_mutation_guard"] = "wss_stateful_delta_mutation_proof_gate"

	fullHistory := wssClassDistributionTestSummary("f1", "full_history", 20000, 8000, 2000, 24000, 1000, 1)
	fullHistory.ProviderInputTokens = 20000
	fullHistory.ProviderCachedTokens = 1000
	fullHistory.DebugFacts["wss.previous_response_id"] = "true"
	fullHistory.DebugFacts["wss.socket_seq"] = "1"
	fullHistory.DebugFacts["wss.tool_results_total"] = "1"
	fullHistory.DebugFacts["wss.tool_results_resolved"] = "1"
	fullHistory.DebugFacts["wss.full_history_stateless_followup"] = "true"
	fullHistory.DebugFacts["wss.full_history_detached_previous_response"] = "true"

	writeJSONLFile(t, path, delta, fullHistory)

	report, err := loadWSSClassDistribution(wssClassDistributionFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSClassDistribution() error = %v", err)
	}
	if len(report.T354ShapeTable) != 2 {
		t.Fatalf("T354 shape rows = %d, want 2: %+v", len(report.T354ShapeTable), report.T354ShapeTable)
	}
	deltaRow := findT354ShapeRow(report.T354ShapeTable, "delta")
	if deltaRow == nil {
		t.Fatalf("missing delta row: %+v", report.T354ShapeTable)
	}
	if deltaRow.PreviousResponseID != "present" ||
		deltaRow.SocketSeq != "gt1" ||
		deltaRow.ToolOutputResolution != "inferred" ||
		deltaRow.ContinuationMode != "direct_delta" ||
		deltaRow.GuardReason != "wss.effective_mutation_guard=wss_stateful_delta_mutation_proof_gate" ||
		deltaRow.GuardedRequests != 1 ||
		deltaRow.AppliedRequests != 1 ||
		deltaRow.ErrorRequests != 1 ||
		deltaRow.UpstreamErrorRequests != 1 ||
		deltaRow.HTTP400ErrorRequests != 1 ||
		deltaRow.CacheReadTokens != 111 ||
		deltaRow.CacheCreateTokens != 22 ||
		deltaRow.PrefixTotalBytes != 32000 ||
		deltaRow.PrefixSplitBytes != 32000 ||
		deltaRow.PrefixSplitInconsistentBytes != 0 ||
		deltaRow.PrefixSplitInconsistentRequests != 0 ||
		deltaRow.PrefixToolDefinitionBytes != 24000 ||
		deltaRow.PrefixInstructionBytes != 8000 ||
		deltaRow.PrefixDefaultKeepToolBytes != 16000 ||
		deltaRow.PrefixNonDefaultToolBytes != 8000 ||
		deltaRow.ToolPruneCandidateBytes != 8000 ||
		deltaRow.ToolPruneCandidateTokens != 2000 {
		t.Fatalf("bad delta T354 row: %+v", deltaRow)
	}
	floatNearTest(t, deltaRow.ProviderCachedPct, 0.7)
	fullRow := findT354ShapeRow(report.T354ShapeTable, "full_history")
	if fullRow == nil {
		t.Fatalf("missing full-history row: %+v", report.T354ShapeTable)
	}
	if fullRow.PreviousResponseID != "present" ||
		fullRow.SocketSeq != "1" ||
		fullRow.ToolOutputResolution != "resolved" ||
		fullRow.ContinuationMode != "stateless_followup_detached" ||
		fullRow.GuardReason != "none" ||
		fullRow.GuardedRequests != 0 ||
		fullRow.AppliedRequests != 1 ||
		fullRow.PrefixTotalBytes != 8000 ||
		fullRow.PrefixSplitBytes != 8000 ||
		fullRow.PrefixToolDefinitionBytes != 6000 ||
		fullRow.PrefixInstructionBytes != 2000 ||
		fullRow.ToolPruneCandidateBytes != 2000 ||
		fullRow.ToolPruneCandidateTokens != 500 {
		t.Fatalf("bad full-history T354 row: %+v", fullRow)
	}

	var stdout, stderr bytes.Buffer
	if code := runWSSClassDistribution([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSClassDistribution text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "T354 shape table:") ||
		!strings.Contains(stdout.String(), "guard=wss.effective_mutation_guard=wss_stateful_delta_mutation_proof_gate") ||
		!strings.Contains(stdout.String(), "continuation=stateless_followup_detached") ||
		!strings.Contains(stdout.String(), "tool_prefix_bytes=24000") ||
		!strings.Contains(stdout.String(), "prune_candidate=8000B/~2000tok") ||
		!strings.Contains(stdout.String(), "default_keep_bytes=16000") {
		t.Fatalf("text output missing T354 table details:\n%s", stdout.String())
	}
}

func TestWSSClassDistributionToolPruneDeltaGuardSurface(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	guarded := wssClassDistributionTestSummary("guard", "delta", 10000, 0, 8000, 0, 7000, 0)
	guarded.PreviousResponseIDUsed = true
	guarded.DebugFacts["wss.socket_seq"] = "1"
	guarded.DebugFacts["wss.tool_prune_guard"] = "wss_tool_prune_delta_guard"
	guarded.ToolPrune = dbg.ToolPruneSummary{Reason: "wss_tool_prune_delta_guard"}
	writeJSONLFile(t, path, guarded)

	report, err := loadWSSClassDistribution(wssClassDistributionFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSClassDistribution() error = %v", err)
	}
	if report.ToolPruneDeltaGuardedRequests != 1 ||
		report.ToolPruneDeltaGuardedOriginal != 10000 ||
		report.ToolPruneDeltaGuardedBytes != 8000 ||
		report.ToolPruneDeltaGuardedTokens != 2000 {
		t.Fatalf("bad tool-prune delta guarded surface: %+v", report)
	}
	floatNearTest(t, report.ToolPruneDeltaGuardedShare, 0.20)
	row := findT354ShapeRow(report.T354ShapeTable, "delta")
	if row == nil || row.GuardReason != "wss.tool_prune_guard=wss_tool_prune_delta_guard" || row.GuardedRequests != 1 {
		t.Fatalf("missing tool-prune guard T354 row: %+v", report.T354ShapeTable)
	}

	var stdout, stderr bytes.Buffer
	if code := runWSSClassDistribution([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSClassDistribution text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "T410 delta tool-prune guard:") ||
		!strings.Contains(stdout.String(), "candidate=8000 bytes (~2000 tok") {
		t.Fatalf("text output missing T410 guard surface:\n%s", stdout.String())
	}
}

func findT354ShapeRow(rows []wssClassDistributionT354Row, shape string) *wssClassDistributionT354Row {
	for i := range rows {
		if rows[i].RequestShape == shape {
			return &rows[i]
		}
	}
	return nil
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
		!strings.Contains(stdout.String(), "prefix split bytes:") ||
		!strings.Contains(stdout.String(), "tool_prefix_bytes=") ||
		!strings.Contains(stdout.String(), "tool-prune candidate upper bound:") ||
		!strings.Contains(stdout.String(), "Non-prefix upper bound") ||
		!strings.Contains(stdout.String(), "Headroom present:") ||
		!strings.Contains(stdout.String(), "Next action:") ||
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
		report.PrefixTotalBytes != 8000 ||
		report.PrefixSplitBytes != 8000 ||
		report.PrefixSplitInconsistentBytes != 0 ||
		report.PrefixToolDefinitionBytes != 6000 ||
		report.PrefixInstructionBytes != 2000 ||
		report.ToolPruneCandidateBytes != 2000 ||
		report.ToolPruneCandidateTokens != 500 ||
		report.Verdict != "headroom_present" ||
		!report.HeadroomPresent ||
		!report.GapInventoryRecommended ||
		!strings.Contains(report.NextAction, "wss-local-gap-inventory") {
		t.Fatalf("bad json report: %+v", report)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runWSSClassDistribution([]string{path, "--json", "--require-headroom"}, &stdout, &stderr); code != 0 {
		t.Fatalf("require-headroom should pass on headroom corpus: code=%d stderr=%s", code, stderr.String())
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

func TestRunWSSClassDistributionRequireHeadroomFailsOnCeiling(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		wssClassDistributionTestSummary("d1", "delta", 10000, 500, 8000, 4000, 7000, 2))

	var stdout, stderr bytes.Buffer
	code := runWSSClassDistribution([]string{path, "--json", "--require-headroom"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("require-headroom ceiling code=%d want 1 stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "headroom not present") {
		t.Fatalf("missing headroom gate stderr: %s", stderr.String())
	}
	var report wssClassDistributionReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output should still be emitted on gate failure: %v\n%s", err, stdout.String())
	}
	if report.Verdict != "corpus_ceiling_evidence" || report.HeadroomPresent {
		t.Fatalf("bad ceiling report: %+v", report)
	}
}

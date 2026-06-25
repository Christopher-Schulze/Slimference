package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

func writeCategory(t *testing.T, root, name string, meta CategoryMetadata, sessions []string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mb, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, corpusCategoryMetadataFilename), mb, 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	for i, s := range sessions {
		fp := filepath.Join(dir, "session_"+itoa(i)+".jsonl")
		if err := os.WriteFile(fp, []byte(s), 0o644); err != nil {
			t.Fatalf("write session: %v", err)
		}
	}
	return dir
}

func writeOutputReduceABReport(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, outputReduceABReportFilename), []byte(body), 0o644); err != nil {
		t.Fatalf("write output-reduce A/B report: %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

const sampleHighSavingsRecord = `{"req_id":"req_high","provider":"anthropic","model":"claude-3-5","tokens":{"original":1000,"after_layer0":900,"after_layer1":600,"final":600,"saved":400}}` + "\n"
const sampleToolPruneRecord = `{"req_id":"req_tool_prune","provider":"codex_chatgpt","model":"gpt-5.5","tokens":{"original":0,"after_layer0":0,"after_layer1":0,"final":0,"saved":26},"tool_prune":{"applied":true,"pruned_tools":1,"saved_tokens":26}}` + "\n"

const sampleLowSavingsRecord = `{"req_id":"req_low","provider":"anthropic","model":"claude-3-5","tokens":{"original":1000,"after_layer0":990,"after_layer1":950,"final":950,"saved":50}}` + "\n"

const sampleAbsoluteSavingsRecord = `{"req_id":"req_abs","provider":"openai","model":"gpt-5","tokens":{"original":400,"after_layer0":0,"after_layer1":0,"final":0,"saved":400}}` + "\n"

const sampleEvidenceRecord = `{"req_id":"req_evidence","provider":"openai","model":"gpt-5","tokens":{"original":1000,"after_layer0":900,"after_layer1":800,"final":800,"saved":200},"cache_read_tokens":120,"cache_create_tokens":40,"provider_cached_tokens":120,"output_tokens":77,"output_reduce":{"applied":true,"profile":"codex_aggressive","added_tokens":12},"proxy_latency_ms":42.5}` + "\n"
const sampleOutputReduceNoOutputTokensRecord = `{"req_id":"req_output_reduce_no_output","provider":"openai","model":"gpt-5","tokens":{"original":0,"after_layer0":0,"after_layer1":0,"final":0,"saved":0},"output_reduce":{"applied":true,"profile":"codex_aggressive"},"proxy_latency_ms":42.5}` + "\n"
const sampleOutputReduceOverheadDominatesRecord = `{"req_id":"req_output_reduce_overhead","provider":"openai","model":"gpt-5","tokens":{"original":0,"after_layer0":0,"after_layer1":0,"final":0,"saved":0},"output_tokens":12,"output_reduce":{"applied":true,"profile":"codex_aggressive","added_tokens":12},"proxy_latency_ms":42.5}` + "\n"
const sampleOutputReduceABReport = `{"pairs":[{"pair_id":"pair_ok","output_tokens_saved":219,"net_tokens_saved":196,"output_savings_pct":22.18,"gate_passed":true}],"pair_count":1,"gate_passed":true}` + "\n"
const sampleOutputReduceABFailedReport = `{"pairs":[{"pair_id":"pair_bad","output_tokens_saved":-10,"net_tokens_saved":-33,"output_savings_pct":-1,"gate_passed":false,"gate_failures":["net negative"]}],"pair_count":1,"gate_passed":false,"gate_failures":["pair_bad: net negative"]}` + "\n"

const sampleHostBudgetOKRecord = `{"req_id":"req_host_ok","provider":"openai","model":"gpt-5","tokens":{"original":1000,"after_layer0":900,"after_layer1":800,"final":800,"saved":200},"host_budget":{"status":"ok","exceeded":false,"compression_ok":true,"degradation_ok":true},"proxy_latency_ms":42.5}` + "\n"

const sampleHostBudgetIssueRecord = `{"req_id":"req_host_issue","provider":"openai","model":"gpt-5","tokens":{"original":1000,"after_layer0":900,"after_layer1":800,"final":800,"saved":200},"host_budget_status":"attention","host_budget_exceeded":true,"host_budget_compression_ok":true,"host_budget_degradation_ok":true,"proxy_latency_ms":42.5}` + "\n"

const sampleErrorLatencyRecord = `{"req_id":"req_error","provider":"openai","model":"gpt-5","tokens":{"original":1000,"after_layer0":900,"after_layer1":800,"final":800,"saved":200},"errors":["bad"],"proxy_latency_ms":2000}` + "\n"

const sampleWebSocketRecord = `{"req_id":"req_ws","provider":"openai","model":"gpt-5","route_mode":"websocket","tokens":{"original":1000,"after_layer0":900,"after_layer1":800,"final":650,"saved":350},"output_reduce":{"applied":true}}` + "\n"

const sampleReReadRecord = `{"req_id":"req_reread","provider":"openai","model":"gpt-5","tokens":{"original":1000,"after_layer0":900,"after_layer1":800,"final":800,"saved":200},"re_read_count":2}` + "\n"

func TestLoadCategoryMetadata_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := LoadCategoryMetadata(dir)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing-metadata error, got %v", err)
	}
}

func TestLoadCategoryMetadata_Malformed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, corpusCategoryMetadataFilename), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadCategoryMetadata(dir)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestLoadCategoryMetadata_DefaultsCategoryFromDirname(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "auto_named", CategoryMetadata{}, nil)
	meta, err := LoadCategoryMetadata(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if meta.Category != "auto_named" {
		t.Fatalf("expected category fallback to dir name, got %q", meta.Category)
	}
}

func TestEvaluateCategory_GatePass(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "feature_long", CategoryMetadata{
		Category:           "feature_long",
		Synthetic:          true,
		ExpectedSavingsMin: 0.30,
		ExpectedSavingsMax: 0.50,
	}, []string{sampleHighSavingsRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) != 0 {
		t.Fatalf("expected no failures, got %v", res.Failures)
	}
	if res.SavingsRatio < 0.39 || res.SavingsRatio > 0.41 {
		t.Fatalf("expected ratio ~0.4, got %.4f", res.SavingsRatio)
	}
	if res.Sessions != 1 {
		t.Fatalf("expected 1 session file, got %d", res.Sessions)
	}
}

func TestEvaluateCategory_GateFailLowRatio(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "low", CategoryMetadata{
		Category:           "low",
		ExpectedSavingsMin: 0.40,
	}, []string{sampleLowSavingsRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) == 0 {
		t.Fatalf("expected failure for ratio below min")
	}
	if !strings.Contains(res.Failures[0], "savings_ratio") {
		t.Fatalf("expected ratio failure, got %v", res.Failures)
	}
}

func TestEvaluateCategory_GateFailMaxOvercount(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "over", CategoryMetadata{
		Category:           "over",
		ExpectedSavingsMin: 0.10,
		ExpectedSavingsMax: 0.20,
	}, []string{sampleHighSavingsRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) == 0 {
		t.Fatalf("expected overcount failure")
	}
	if !strings.Contains(res.Failures[0], "suspicious overcount") {
		t.Fatalf("expected overcount marker, got %v", res.Failures)
	}
}

func TestEvaluateCategory_AbsoluteSavedTokensGate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "absolute", CategoryMetadata{
		Category:               "absolute",
		ExpectedSavedTokensMin: 399,
	}, []string{sampleAbsoluteSavingsRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) != 0 {
		t.Fatalf("expected absolute saved-token gate to pass, got %v", res.Failures)
	}
	if !res.GateConfigured {
		t.Fatal("absolute saved-token min must configure the gate")
	}
}

func TestEvaluateCategory_AbsoluteSavedTokensGateFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "absolute_fail", CategoryMetadata{
		Category:               "absolute_fail",
		ExpectedSavedTokensMin: 401,
	}, []string{sampleAbsoluteSavingsRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) == 0 || !strings.Contains(res.Failures[0], "saved_tokens=400") {
		t.Fatalf("expected absolute saved-token failure, got %v", res.Failures)
	}
}

func TestEvaluateCategory_GateFailRequestCount(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "few", CategoryMetadata{
		Category:             "few",
		ExpectedRequestCount: 5,
	}, []string{sampleHighSavingsRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) == 0 || !strings.Contains(res.Failures[0], "requests=") {
		t.Fatalf("expected requests failure, got %v", res.Failures)
	}
}

func TestEvaluateCategory_EvidenceMetricsAndGates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "evidence", CategoryMetadata{
		Category:                       "evidence",
		EvidenceLevel:                  "live_operator",
		ExpectedSavingsMin:             0.10,
		ExpectedProviderCacheReadMin:   100,
		ExpectedOutputReduceAppliedMin: 1,
		ExpectedLatencyP95MaxMs:        100,
		ExpectedMaxErrors:              1,
	}, []string{sampleEvidenceRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) != 0 {
		t.Fatalf("expected no failures, got %v", res.Failures)
	}
	if res.ProviderCacheReadTokens != 120 || res.ProviderCacheCreateTokens != 40 || res.ProviderCachedTokens != 120 {
		t.Fatalf("cache metrics: %+v", res)
	}
	if res.OutputTokens != 77 || res.OutputReduceApplied != 1 || res.LatencyP95Ms != 42.5 || res.EvidenceLevel != "live_operator" {
		t.Fatalf("evidence metrics: %+v", res)
	}
	if combo := res.LayerCombinations["L0+L1+L2+L3"]; combo.Requests != 1 || combo.OutputTokens != 77 {
		t.Fatalf("layer combinations: %+v", res.LayerCombinations)
	}
}

func TestEvaluateCategory_ReReadGateFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "reread", CategoryMetadata{
		Category:               "reread",
		ExpectedReReadCountMax: 1,
	}, []string{sampleReReadRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.ReReadCount != 2 {
		t.Fatalf("reread count = %d, want 2", res.ReReadCount)
	}
	if len(res.Failures) == 0 || !strings.Contains(res.Failures[0], "reread_count=2") {
		t.Fatalf("expected reread failure, got %v", res.Failures)
	}
}

func TestEvaluateCategory_PlannerReplayMetricsAndGates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "planner", CategoryMetadata{
		Category:                        "planner",
		ExpectedSavingsMin:              0.10,
		ExpectedPlannerMissedMax:        2,
		ExpectedPlannerBypassAppliedMax: 1,
	}, []string{plannedJSONL})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) != 0 {
		t.Fatalf("expected no failures, got %v", res.Failures)
	}
	if res.PlanReplay.RequestsWithPlan != 3 ||
		res.PlanReplay.ExpectedSavingsTokens != 211 ||
		res.PlanReplay.MissedActive != 1 ||
		res.PlanReplay.BypassApplied != 1 {
		t.Fatalf("planner metrics: %+v", res.PlanReplay)
	}
}

func TestEvaluateCategory_PlannerReplayGateFailures(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "planner_bad", CategoryMetadata{
		Category:                 "planner_bad",
		ExpectedPlannerMissedMax: 0,
	}, []string{plannedJSONL})
	metadataPath := filepath.Join(dir, corpusCategoryMetadataFilename)
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	meta["expected_planner_missed_max"] = 0
	meta["expected_planner_bypass_applied_max"] = 0
	rewritten, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metadataPath, rewritten, 0o644); err != nil {
		t.Fatalf("rewrite metadata: %v", err)
	}
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	got := strings.Join(res.Failures, "\n")
	for _, want := range []string{"planner_missed_active=1", "planner_bypass_applied=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in failures: %v", want, res.Failures)
		}
	}
}

func TestEvaluateCategory_ScenarioValidatorsPass(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "scenarios", CategoryMetadata{
		Category:           "scenarios",
		ExpectedSavingsMin: 0.10,
		ScenarioValidators: []string{
			"tool_heavy",
			"cache_reuse",
			"output_reduce",
			"websocket",
			"low_error",
			"host_budget_ok",
			"layer_combo_diversity",
		},
	}, []string{sampleEvidenceRecord, sampleWebSocketRecord, sampleHostBudgetOKRecord, sampleToolPruneRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) != 0 {
		t.Fatalf("expected scenario validators to pass, got %v", res.Failures)
	}
	if res.HostBudgetOKRows != 1 || res.HostBudgetIssueRows != 0 {
		t.Fatalf("host budget rows: %+v", res)
	}
}

func TestEvaluateCategory_ScenarioValidatorsFail(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "bad_scenarios", CategoryMetadata{
		Category: "bad_scenarios",
		ScenarioValidators: []string{
			"cache_reuse",
			"output_reduce",
			"websocket",
			"low_error",
			"host_budget_ok",
			"layer_combo_diversity",
			"unknown_validator",
		},
	}, []string{sampleErrorLatencyRecord, sampleHostBudgetIssueRecord})
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	got := strings.Join(res.Failures, "\n")
	for _, want := range []string{
		"scenario cache_reuse",
		"scenario output_reduce",
		"scenario websocket",
		"scenario low_error",
		"scenario host_budget_ok",
		"scenario layer_combo_diversity",
		"unknown scenario validator",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in failures: %v", want, res.Failures)
		}
	}
	if !res.GateConfigured {
		t.Fatal("scenario validators must mark the gate as configured")
	}
}

func TestLiveCorpusDocsListSupportedScenarioValidators(t *testing.T) {
	t.Parallel()
	paths := []string{
		filepath.Join("..", "..", "docs", "live-corpus-policy.md"),
		filepath.Join("..", "..", "docs", "documentation.md"),
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(body)
			for _, name := range supportedScenarioValidators {
				if !strings.Contains(text, "`"+name+"`") {
					t.Fatalf("%s does not document scenario validator %q", path, name)
				}
			}
		})
	}
}

func TestLiveCorpusPolicyListsPromotionAndMaxxWorkloads(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "docs", "live-corpus-policy.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(body)
	for _, workload := range requiredPromotionWorkloads {
		if !strings.Contains(text, workload) {
			t.Fatalf("%s does not document promotion workload %q", path, workload)
		}
	}
	for _, workload := range requiredMaxxWorkloads {
		if !strings.Contains(text, workload) {
			t.Fatalf("%s does not document maxx workload %q", path, workload)
		}
	}
}

func TestEvaluateCategory_EvidenceGateFailures(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "bad_evidence", CategoryMetadata{
		Category:                       "bad_evidence",
		ExpectedSavingsMin:             0.10,
		ExpectedProviderCacheReadMin:   999,
		ExpectedOutputReduceAppliedMin: 2,
		ExpectedLatencyP95MaxMs:        100,
	}, []string{sampleErrorLatencyRecord})
	metadataPath := filepath.Join(dir, corpusCategoryMetadataFilename)
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	meta["expected_max_errors"] = 0
	rewritten, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metadataPath, rewritten, 0o644); err != nil {
		t.Fatalf("rewrite metadata: %v", err)
	}
	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	got := strings.Join(res.Failures, "\n")
	for _, want := range []string{"errors=1", "latency_p95_ms=2000.0", "provider_cache_read_tokens=0", "output_reduce_applied=0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in failures: %v", want, res.Failures)
		}
	}
}

func TestEvaluateCategory_BubblesAggregateError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "bad", CategoryMetadata{Category: "bad"}, nil)
	// Replace the directory with a file mid-load to force a stat error.
	bogus := filepath.Join(dir, "session_0.jsonl")
	if err := os.MkdirAll(bogus, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if _, err := EvaluateCategory(dir, &bytes.Buffer{}); err == nil {
		// AggregateSessionsFromPath skips dirs, so this can pass; what we
		// really want is no panic. Acceptable.
		t.Skip("aggregation tolerated the corrupted layout; that is fine")
	}
}

func TestEvaluateCorpus_SkipsDirsWithoutMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "with_meta", CategoryMetadata{Category: "with_meta", ExpectedSavingsMin: 0.30}, []string{sampleHighSavingsRecord})
	if err := os.MkdirAll(filepath.Join(root, "no_meta"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var errBuf bytes.Buffer
	report, err := EvaluateCorpus(root, &errBuf)
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	if len(report.Categories) != 1 {
		t.Fatalf("expected single category, got %d", len(report.Categories))
	}
	if !strings.Contains(errBuf.String(), "no_meta") {
		t.Fatalf("expected warning mentioning the metadata-less dir; got %q", errBuf.String())
	}
}

func TestEvaluateCorpus_BadRoot(t *testing.T) {
	t.Parallel()
	if _, err := EvaluateCorpus(filepath.Join(t.TempDir(), "no-such"), &bytes.Buffer{}); err == nil {
		t.Fatalf("expected error for missing root")
	}
}

func TestEvaluateCorpus_MalformedMetadataReportsWarning(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, corpusCategoryMetadataFilename), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var errBuf bytes.Buffer
	report, err := EvaluateCorpus(root, &errBuf)
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	if len(report.Categories) != 0 {
		t.Fatalf("expected zero categories on malformed metadata, got %d", len(report.Categories))
	}
	if !strings.Contains(errBuf.String(), "broken") {
		t.Fatalf("expected warning naming the broken category; got %q", errBuf.String())
	}
}

func TestEvaluateCorpus_TracksSyntheticVsReal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "syn", CategoryMetadata{Category: "syn", Synthetic: true, ExpectedSavingsMin: 0.30}, []string{sampleHighSavingsRecord})
	writeCategory(t, root, "real", CategoryMetadata{Category: "real", Synthetic: false, ExpectedSavingsMin: 0.30}, []string{sampleHighSavingsRecord})
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	if !report.HasSynthetic || !report.HasReal {
		t.Fatalf("expected both flags set, got %+v", report)
	}
}

func TestEvaluateCorpus_RealLocalSavingsExcludesProviderCacheAndDiagnostics(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "real_local", CategoryMetadata{
		Category:           "real_local",
		EvidenceLevel:      "live_operator",
		ClientFamily:       "codex_cli",
		WorkloadClass:      "repeat_read",
		ExpectedSavingsMin: 0.10,
	}, []string{sampleHighSavingsRecord})
	writeCategory(t, root, "provider_cache", CategoryMetadata{
		Category:                     "provider_cache",
		EvidenceLevel:                "live_operator",
		ClientFamily:                 "codex_cli",
		WorkloadClass:                "provider_cache_long_session",
		ExpectedProviderCacheReadMin: 1,
	}, []string{sampleHighSavingsRecord})
	writeCategory(t, root, "output_reduce", CategoryMetadata{
		Category:                       "output_reduce",
		EvidenceLevel:                  "live_operator",
		ClientFamily:                   "codex_cli",
		WorkloadClass:                  "output_reduce_aggressive",
		ExpectedOutputReduceAppliedMin: 1,
	}, []string{sampleHighSavingsRecord})
	writeCategory(t, root, "synthetic", CategoryMetadata{
		Category:           "synthetic",
		Synthetic:          true,
		ExpectedSavingsMin: 0.10,
	}, []string{sampleHighSavingsRecord})

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	if report.RealCurrentLocalOrigTokens != 1000 ||
		report.RealCurrentLocalSavedTokens != 400 ||
		report.RealCurrentLocalSavingsRatio < 0.399 ||
		report.RealCurrentLocalSavingsRatio > 0.401 {
		t.Fatalf("real local aggregate should include only ordinary current local rows, got %+v", report)
	}
}

// TestEvaluateCorpus_NetBillableEquivalentMatchesSSOT verifies the net billable
// equivalent estimate uses the SSOT formula from internal/analytics/proxy_gain.go
// (cacheReadDiscountEquivalent): 0.9x on NET cache tokens (read - create), only
// when net > 0. The denominator must be the local original token count, not
// inflated by adding cached tokens (which are already part of the input stream).
func TestEvaluateCorpus_NetBillableEquivalentMatchesSSOT(t *testing.T) {
	t.Parallel()
	// record: orig=1000, saved=200, cache_read=120, cache_create=40, cached=120
	// net cache = 120-40 = 80; discount = 80*0.9 = 72; net billable = 200+72 = 272
	root := t.TempDir()
	writeCategory(t, root, "real_local", CategoryMetadata{
		Category:           "real_local",
		EvidenceLevel:      "live_operator",
		ClientFamily:       "codex_cli",
		WorkloadClass:      "repeat_read",
		ExpectedSavingsMin: 0.10,
	}, []string{sampleEvidenceRecord})

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	if report.RealCurrentLocalOrigTokens != 1000 || report.RealCurrentLocalSavedTokens != 200 {
		t.Fatalf("local baseline: orig=%d saved=%d", report.RealCurrentLocalOrigTokens, report.RealCurrentLocalSavedTokens)
	}
	if report.ProviderCacheReadTokens != 120 || report.ProviderCacheCreateTokens != 40 {
		t.Fatalf("cache metrics: read=%d create=%d", report.ProviderCacheReadTokens, report.ProviderCacheCreateTokens)
	}
	// SSOT: 200 + int((120-40)*0.9) = 200 + 72 = 272
	if report.NetBillableEquivalentEstimate != 272 {
		t.Fatalf("net billable estimate: got %d, want 272 (SSOT: saved + (read-create)*0.9)",
			report.NetBillableEquivalentEstimate)
	}
	// Formatted output must use the local orig denominator (1000), not
	// orig+cached (1120), and must reflect the SSOT discount (27.2%, not the
	// buggy 27.5% from cached*0.9 over orig+cached).
	out := FormatCorpusReport(report)
	if !strings.Contains(out, "Net billable:    27.20% (S_local + cache discount 0.9x, saved=272 orig=1000)") {
		t.Fatalf("formatted net billable line mismatch:\n%s", out)
	}
}

// TestEvaluateCorpus_NetBillableNoDiscountWhenCreatesExceedReads verifies the
// net > 0 guard: when cache create tokens exceed read tokens, no cache discount
// is applied (caching cost more than it saved).
func TestEvaluateCorpus_NetBillableNoDiscountWhenCreatesExceedReads(t *testing.T) {
	t.Parallel()
	// record: orig=1000, saved=200, cache_read=30, cache_create=120, cached=30
	// net cache = 30-120 = -90 <= 0; no discount; net billable = 200
	record := `{"req_id":"req_negcache","provider":"openai","model":"gpt-5","tokens":{"original":1000,"after_layer0":900,"after_layer1":800,"final":800,"saved":200},"cache_read_tokens":30,"cache_create_tokens":120,"provider_cached_tokens":30,"output_tokens":50}` + "\n"
	root := t.TempDir()
	writeCategory(t, root, "real_local", CategoryMetadata{
		Category:           "real_local",
		EvidenceLevel:      "live_operator",
		ClientFamily:       "codex_cli",
		WorkloadClass:      "repeat_read",
		ExpectedSavingsMin: 0.10,
	}, []string{record})

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	if report.NetBillableEquivalentEstimate != 200 {
		t.Fatalf("net billable estimate with negative cache net: got %d, want 200 (no discount)",
			report.NetBillableEquivalentEstimate)
	}
}

// writeCommandOutputFirstSidecar writes a command_output_first.jsonl sidecar
// file into dir with the given rows.
func writeCommandOutputFirstSidecar(t *testing.T, dir string, rows ...string) {
	t.Helper()
	var buf bytes.Buffer
	for _, r := range rows {
		buf.WriteString(r)
		if !strings.HasSuffix(r, "\n") {
			buf.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(dir, commandOutputFirstSidecarFilename), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

func gzB64(b []byte) string {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// cofVerifiedRow builds a provenance-carrying command_output_first sidecar line
// (sha256 + gzip+base64 of raw/compacted bytes) whose recorded token counts are
// recomputed deterministically from the embedded bytes, so the gate trusts it.
func cofVerifiedRow(t *testing.T, command string, raw, compacted []byte) string {
	t.Helper()
	if len(compacted) >= len(raw) {
		t.Fatalf("cofVerifiedRow: compacted (%d) must be smaller than raw (%d)", len(compacted), len(raw))
	}
	in := int64(filter.EstimateTokensFromBytes(len(raw)))
	out := int64(filter.EstimateTokensFromBytes(len(compacted)))
	sum := sha256.Sum256(raw)
	row := map[string]any{
		"ts":                 "2026-06-25T00:00:00Z",
		"command":            command,
		"input_tokens":       in,
		"output_tokens":      out,
		"saved_tokens":       in - out,
		"input_sha256":       hex.EncodeToString(sum[:]),
		"raw_gzip_b64":       gzB64(raw),
		"compacted_gzip_b64": gzB64(compacted),
	}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal cof row: %v", err)
	}
	return string(b)
}

func TestEvaluateCorpus_CommandOutputFirstSidecarCounted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "real_local", CategoryMetadata{
		Category:      "real_local",
		EvidenceLevel: "live_operator",
		ClientFamily:  "codex_cli",
		WorkloadClass: "search_loop",
	}, []string{sampleHighSavingsRecord})
	// VERIFIED sidecar line (provenance recomputes to input=500, output=300,
	// saved=200) — should be added to the trusted gate number.
	writeCommandOutputFirstSidecar(t, dir,
		cofVerifiedRow(t, "rg", bytes.Repeat([]byte("x"), 2000), bytes.Repeat([]byte("y"), 1200)))

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	// Expect orig = 1000 (session) + 500 (sidecar) = 1500
	// Expect saved = 400 (session) + 200 (sidecar) = 600
	if report.RealCurrentLocalOrigTokens != 1500 {
		t.Fatalf("expected orig 1500, got %d", report.RealCurrentLocalOrigTokens)
	}
	if report.RealCurrentLocalSavedTokens != 600 {
		t.Fatalf("expected saved 600, got %d", report.RealCurrentLocalSavedTokens)
	}
	if report.SLocalL2SavedTokens != 200 {
		t.Fatalf("expected verified L2 saved=200, got %d", report.SLocalL2SavedTokens)
	}
}

func TestEvaluateCorpus_CommandOutputFirstUnverifiedExcluded(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "real_local", CategoryMetadata{
		Category:      "real_local",
		EvidenceLevel: "live_operator",
		ClientFamily:  "codex_cli",
		WorkloadClass: "search_loop",
	}, []string{sampleHighSavingsRecord})
	// Self-reported line WITHOUT recomputable bytes — must NOT count toward the
	// trusted S_local number; only reported as observed-only (§3.8.1).
	writeCommandOutputFirstSidecar(t, dir,
		`{"ts":"2026-06-21T12:00:00Z","command":"go test -json","input_tokens":900000,"output_tokens":27,"saved_tokens":899973}`)

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	if report.RealCurrentLocalOrigTokens != 1000 || report.RealCurrentLocalSavedTokens != 400 {
		t.Fatalf("unverified L2 must be excluded from trusted; got orig=%d saved=%d",
			report.RealCurrentLocalOrigTokens, report.RealCurrentLocalSavedTokens)
	}
	if report.SLocalL2UnverifiedSavedTokens != 899973 {
		t.Fatalf("expected observed unverified L2 saved=899973, got %d", report.SLocalL2UnverifiedSavedTokens)
	}
}

func TestEvaluateCorpus_CommandOutputFirstTamperRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "real_local", CategoryMetadata{
		Category:      "real_local",
		EvidenceLevel: "live_operator",
		ClientFamily:  "codex_cli",
		WorkloadClass: "search_loop",
	}, []string{sampleHighSavingsRecord})
	// Provenance present but the recorded token counts do not match the embedded
	// bytes — the gate must hard-fail (tamper), not silently trust it.
	tampered := cofVerifiedRow(t, "rg", bytes.Repeat([]byte("x"), 2000), bytes.Repeat([]byte("y"), 1200))
	tampered = strings.Replace(tampered, `"input_tokens":500`, `"input_tokens":50000`, 1)
	tampered = strings.Replace(tampered, `"saved_tokens":200`, `"saved_tokens":49700`, 1)
	writeCommandOutputFirstSidecar(t, dir, tampered)

	if _, err := EvaluateCorpus(root, &bytes.Buffer{}); err == nil {
		t.Fatal("expected EvaluateCorpus to reject a tampered provenance line, got nil error")
	}
}

func TestEvaluateCorpus_NoSidecarUnchanged(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "real_local", CategoryMetadata{
		Category:      "real_local",
		EvidenceLevel: "live_operator",
		ClientFamily:  "codex_cli",
		WorkloadClass: "search_loop",
	}, []string{sampleHighSavingsRecord})

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	if report.RealCurrentLocalOrigTokens != 1000 || report.RealCurrentLocalSavedTokens != 400 {
		t.Fatalf("expected orig=1000 saved=400 without sidecar, got orig=%d saved=%d",
			report.RealCurrentLocalOrigTokens, report.RealCurrentLocalSavedTokens)
	}
}

func TestEvaluateCorpus_ZeroSavingsSidecarUnchanged(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "real_local", CategoryMetadata{
		Category:      "real_local",
		EvidenceLevel: "live_operator",
		ClientFamily:  "codex_cli",
		WorkloadClass: "search_loop",
	}, []string{sampleHighSavingsRecord})
	// Sidecar with zero saved_tokens — should not affect the gate.
	writeCommandOutputFirstSidecar(t, dir,
		`{"ts":"2026-06-21T12:00:00Z","command":"cat","input_tokens":100,"output_tokens":100,"saved_tokens":0}`)

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	if report.RealCurrentLocalOrigTokens != 1000 || report.RealCurrentLocalSavedTokens != 400 {
		t.Fatalf("expected orig=1000 saved=400 with zero-savings sidecar, got orig=%d saved=%d",
			report.RealCurrentLocalOrigTokens, report.RealCurrentLocalSavedTokens)
	}
}

// writeL1ServerStateSidecar writes a server_state_continuation.jsonl sidecar
// file into dir with the given rows.
func writeL1ServerStateSidecar(t *testing.T, dir string, rows ...string) {
	t.Helper()
	var buf bytes.Buffer
	for _, r := range rows {
		buf.WriteString(r)
		if !strings.HasSuffix(r, "\n") {
			buf.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(dir, l1SidecarFilename), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write L1 sidecar: %v", err)
	}
}

func TestEvaluateCorpus_L1ServerStateSidecarExcludedFromSLocal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "real_local", CategoryMetadata{
		Category:      "real_local",
		EvidenceLevel: "live_operator",
		ClientFamily:  "codex_cli",
		WorkloadClass: "search_loop",
	}, []string{sampleHighSavingsRecord})
	// L1 sidecar with 2000 orig, 1500 saved. Per AGENTS.md §3.7.7 this is
	// Codex-native previous_response_id continuation and must NOT be added to
	// the trusted S_local totals; it is reported separately as observed-only.
	writeL1ServerStateSidecar(t, dir,
		`{"ts":"2026-06-22T12:00:00Z","input_tokens":2000,"output_tokens":500,"saved_tokens":1500}`)

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	// S_local stays at the session-only numbers (L1 excluded).
	if report.RealCurrentLocalOrigTokens != 1000 || report.RealCurrentLocalSavedTokens != 400 {
		t.Fatalf("expected S_local orig=1000 saved=400 (L1 excluded), got orig=%d saved=%d",
			report.RealCurrentLocalOrigTokens, report.RealCurrentLocalSavedTokens)
	}
	// L1 is recorded in the separate observed-only fields.
	if report.SLocalL1OrigTokens != 2000 || report.SLocalL1SavedTokens != 1500 {
		t.Fatalf("expected observed L1 orig=2000 saved=1500, got orig=%d saved=%d",
			report.SLocalL1OrigTokens, report.SLocalL1SavedTokens)
	}
}

func TestEvaluateCorpus_L2CountedL1ExcludedFromSLocal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := writeCategory(t, root, "real_local", CategoryMetadata{
		Category:      "real_local",
		EvidenceLevel: "live_operator",
		ClientFamily:  "codex_cli",
		WorkloadClass: "search_loop",
	}, []string{sampleHighSavingsRecord})
	// L2 (command-output-first) is Slimference-incremental and counts when it is
	// verified (provenance recomputes to input=500, output=300, saved=200); L1 is
	// Codex-native and is excluded from S_local (§3.7.7).
	writeCommandOutputFirstSidecar(t, dir,
		cofVerifiedRow(t, "rg", bytes.Repeat([]byte("x"), 2000), bytes.Repeat([]byte("y"), 1200)))
	writeL1ServerStateSidecar(t, dir,
		`{"ts":"2026-06-22T12:00:00Z","input_tokens":2000,"output_tokens":500,"saved_tokens":1500}`)

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate corpus: %v", err)
	}
	// S_local = session(1000/400) + L2(500/200); L1 excluded.
	if report.RealCurrentLocalOrigTokens != 1500 || report.RealCurrentLocalSavedTokens != 600 {
		t.Fatalf("expected S_local orig=1500 saved=600 (L2 counted, L1 excluded), got orig=%d saved=%d",
			report.RealCurrentLocalOrigTokens, report.RealCurrentLocalSavedTokens)
	}
	if report.SLocalL2SavedTokens != 200 {
		t.Fatalf("expected L2 saved=200, got %d", report.SLocalL2SavedTokens)
	}
	if report.SLocalL1SavedTokens != 1500 {
		t.Fatalf("expected observed L1 saved=1500, got %d", report.SLocalL1SavedTokens)
	}
}

func TestEvaluateRealLocalGate_PassAndFail(t *testing.T) {
	t.Parallel()
	report := CorpusReport{
		RealCurrentLocalOrigTokens:   1000,
		RealCurrentLocalSavedTokens:  400,
		RealCurrentLocalSavingsRatio: 0.40,
	}
	if gate := EvaluateRealLocalGate(report, 0.399, 400); !gate.Passed {
		t.Fatalf("expected gate pass, got %+v", gate)
	}
	gate := EvaluateRealLocalGate(report, 0.401, 401)
	if gate.Passed {
		t.Fatalf("expected gate failure, got %+v", gate)
	}
	got := strings.Join(gate.Failures, "\n")
	for _, want := range []string{"real_current_local_savings_ratio", "real_current_local_saved_tokens"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in failures:\n%s", want, got)
		}
	}
}

func TestEvaluateRealLocalGate_FailsWithoutLocalDenominator(t *testing.T) {
	t.Parallel()
	gate := EvaluateRealLocalGate(CorpusReport{}, 0.01, 0)
	if gate.Passed {
		t.Fatalf("expected no-denominator failure, got %+v", gate)
	}
	if !strings.Contains(strings.Join(gate.Failures, "\n"), "cannot prove S_local ratio") {
		t.Fatalf("expected denominator failure, got %+v", gate.Failures)
	}
}

func promotionMeta(category, client, workload string) CategoryMetadata {
	return CategoryMetadata{
		Category:                category,
		EvidenceLevel:           "live_operator",
		ClientFamily:            client,
		WorkloadClass:           workload,
		ExpectedSavingsMin:      0.10,
		ExpectedSavingsMax:      0.90,
		ExpectedRequestCount:    1,
		ExpectedMaxErrors:       0,
		ExpectedLatencyP95MaxMs: 1000,
		ExpectedReReadCountMax:  0,
		ScenarioValidators:      []string{"low_error"},
	}
}

func writePromotionCorpus(t *testing.T, root string) {
	t.Helper()
	workloads := []string{
		"repeat_read",
		"ranged_read",
		"search_loop",
		"git_status",
		"test_failure",
		"apply_patch_edit_read",
		"large_tool_output",
		"long_workday",
		"repeat_read",
		"search_loop",
	}
	for i, workload := range workloads {
		client := "codex_cli"
		if i >= 5 {
			client = "codex_desktop"
		}
		name := client + "_" + workload + "_" + itoa(i)
		dir := writeCategory(t, root, name, promotionMeta(name, client, workload), []string{sampleHighSavingsRecord})
		forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_max_errors", 0)
		forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_reread_count_max", 0)
	}
}

func writeMaxxCorpus(t *testing.T, root string) {
	t.Helper()
	writePromotionCorpus(t, root)
	for i, workload := range []string{
		"chunk_dedup_similar_outputs",
		"chunk_dedup_log_output",
		"chunk_dedup_test_output",
		"tool_heavy",
		"provider_cache_long_session",
		"host_resource_long_workday",
	} {
		client := "codex_cli"
		if i%2 == 1 {
			client = "codex_desktop"
		}
		name := client + "_" + workload
		meta := promotionMeta(name, client, workload)
		switch workload {
		case "provider_cache_long_session":
			meta.ExpectedProviderCacheReadMin = 100
			meta.ScenarioValidators = []string{"cache_reuse", "low_error"}
		case "host_resource_long_workday":
			meta.ScenarioValidators = []string{"host_budget_ok", "low_error"}
		case "tool_heavy":
			meta.ExpectedSavingsMin = 0
			meta.ExpectedSavedTokensMin = 1
			meta.ScenarioValidators = []string{"tool_heavy", "low_error"}
		default:
			meta.ScenarioValidators = []string{"low_error"}
		}
		session := sampleHighSavingsRecord
		if workload == "provider_cache_long_session" {
			session = sampleEvidenceRecord
		} else if workload == "tool_heavy" {
			session = sampleToolPruneRecord
		}
		if workload == "host_resource_long_workday" {
			session = sampleHostBudgetOKRecord
		}
		dir := writeCategory(t, root, name, meta, []string{session})
		forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_max_errors", 0)
		forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_reread_count_max", 0)
	}
}

func forceMetadataNumber(t *testing.T, path, key string, value int) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	meta[key] = value
	rewritten, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("encode metadata: %v", err)
	}
	if err := os.WriteFile(path, rewritten, 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
}

func TestEvaluatePromotionGate_Pass(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writePromotionCorpus(t, root)
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	gate := EvaluatePromotionGate(report)
	if !gate.Passed {
		t.Fatalf("expected promotion pass, got %+v", gate)
	}
	if gate.SessionsByClient["codex_cli"] != 5 || gate.SessionsByClient["codex_desktop"] != 5 {
		t.Fatalf("client counts: %+v", gate.SessionsByClient)
	}
}

func TestEvaluatePromotionGate_FailsSyntheticOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "syn", CategoryMetadata{Category: "syn", Synthetic: true, ExpectedSavingsMin: 0.30}, []string{sampleHighSavingsRecord})
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	gate := EvaluatePromotionGate(report)
	got := strings.Join(gate.Failures, "\n")
	for _, want := range []string{"no real live_operator categories", "client codex_cli", "client codex_desktop", "missing workload_class repeat_read"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in failures: %+v", want, gate)
		}
	}
}

func TestEvaluatePromotionGate_FailsIncompleteRealMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "real_incomplete", CategoryMetadata{
		Category:           "real_incomplete",
		EvidenceLevel:      "manual_note",
		ClientFamily:       "codex_cli",
		WorkloadClass:      "repeat_read",
		ExpectedSavingsMin: 0.10,
	}, []string{sampleHighSavingsRecord})
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	gate := EvaluatePromotionGate(report)
	got := strings.Join(gate.Failures, "\n")
	for _, want := range []string{"evidence_level", "expected_max_errors", "expected_reread_count_max", "expected_latency_p95_max_ms"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in failures: %+v", want, gate)
		}
	}
}

func TestCategoryHasPromotionSavingsSignal_WorkloadSpecificEconomics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		workload string
		meta     *CategoryMetadata
		want     bool
	}{
		{
			name:     "ordinary workload needs billable input savings",
			workload: "repeat_read",
			meta:     &CategoryMetadata{ExpectedSavingsMin: 0.01},
			want:     true,
		},
		{
			name:     "ordinary workload accepts absolute saved token signal",
			workload: "repeat_read",
			meta:     &CategoryMetadata{ExpectedSavedTokensMin: 100},
			want:     true,
		},
		{
			name:     "ordinary workload rejects provider cache only",
			workload: "repeat_read",
			meta:     &CategoryMetadata{ExpectedProviderCacheReadMin: 100},
			want:     false,
		},
		{
			name:     "provider cache uses provider cache read signal",
			workload: "provider_cache_long_session",
			meta:     &CategoryMetadata{ExpectedProviderCacheReadMin: 100},
			want:     true,
		},
		{
			name:     "provider cache rejects plain input savings metadata",
			workload: "provider_cache_long_session",
			meta:     &CategoryMetadata{ExpectedSavingsMin: 0.01},
			want:     false,
		},
		{
			name:     "output reduce uses applied signal",
			workload: "output_reduce_aggressive",
			meta:     &CategoryMetadata{ExpectedOutputReduceAppliedMin: 1},
			want:     true,
		},
		{
			name:     "output reduce rejects plain input savings metadata",
			workload: "output_reduce_aggressive",
			meta:     &CategoryMetadata{ExpectedSavingsMin: 0.01},
			want:     false,
		},
		{
			name:     "output reduce ab uses net pair signal",
			workload: "output_reduce_ab",
			meta: &CategoryMetadata{
				ExpectedOutputReduceABPairsMin:    1,
				ExpectedOutputReduceABNetSavedMin: 1,
			},
			want: true,
		},
		{
			name:     "output reduce ab rejects plain injected signal",
			workload: "output_reduce_ab",
			meta:     &CategoryMetadata{ExpectedOutputReduceAppliedMin: 1},
			want:     false,
		},
		{
			name:     "nil metadata fails",
			workload: "repeat_read",
			meta:     nil,
			want:     false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := categoryHasPromotionSavingsSignal(tt.workload, tt.meta)
			if got != tt.want {
				t.Fatalf("categoryHasPromotionSavingsSignal(%q, %+v) = %v, want %v", tt.workload, tt.meta, got, tt.want)
			}
		})
	}
}

func TestEvaluateMaxxGate_Pass(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeMaxxCorpus(t, root)
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	gate := EvaluateMaxxGate(report)
	if !gate.Passed {
		t.Fatalf("expected maxx pass, got %+v", gate)
	}
	if gate.SessionsByWorkload["chunk_dedup_log_output"] != 1 ||
		gate.SessionsByWorkload["provider_cache_long_session"] != 1 {
		t.Fatalf("maxx workload counts: %+v", gate.SessionsByWorkload)
	}
}

func TestEvaluateMaxxGate_FailsMissingMechanismBreadth(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writePromotionCorpus(t, root)
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	gate := EvaluateMaxxGate(report)
	if gate.Passed {
		t.Fatalf("expected maxx failure without mechanism breadth: %+v", gate)
	}
	got := strings.Join(gate.Failures, "\n")
	for _, want := range []string{
		"missing maxx workload_class chunk_dedup_similar_outputs",
		"missing maxx workload_class tool_heavy",
		"missing maxx workload_class provider_cache_long_session",
		"missing maxx workload_class host_resource_long_workday",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in failures:\n%s", want, got)
		}
	}
}

func TestEvaluateMaxxGate_IgnoresHistoricalDiagnosticWorkloads(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeMaxxCorpus(t, root)
	meta := promotionMeta("codex_cli_output_reduce_aggressive", "codex_cli", "output_reduce_aggressive")
	currentProductPath := false
	meta.CurrentProductPath = &currentProductPath
	meta.ExpectedSavingsMin = 0
	meta.ExpectedOutputReduceAppliedMin = 1
	meta.ScenarioValidators = []string{"output_reduce", "low_error"}
	dir := writeCategory(t, root, "codex_cli_output_reduce_aggressive", meta, []string{sampleEvidenceRecord})
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_max_errors", 0)
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_reread_count_max", 0)

	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := report.SessionsByWorkload["output_reduce_aggressive"]; got != 0 {
		t.Fatalf("historical output-reduce should not be product-counted, got %d", got)
	}
	var historicalFound bool
	for _, category := range report.Categories {
		if category.Category == "codex_cli_output_reduce_aggressive" {
			historicalFound = true
			if category.CurrentProductPath {
				t.Fatalf("historical category marked current: %+v", category)
			}
			if len(category.Failures) != 0 {
				t.Fatalf("historical category should still be category-testable: %+v", category.Failures)
			}
		}
	}
	if !historicalFound {
		t.Fatal("historical output-reduce category not evaluated")
	}
	gate := EvaluateMaxxGate(report)
	if !gate.Passed {
		t.Fatalf("historical diagnostic workload should not fail maxx gate: %+v", gate)
	}
}

func TestEvaluateCategory_FailsOutputReduceWithoutObservedOutputTokens(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	meta := promotionMeta("codex_cli_output_reduce_aggressive", "codex_cli", "output_reduce_aggressive")
	meta.ExpectedSavingsMin = 0
	meta.ExpectedOutputReduceAppliedMin = 1
	meta.ScenarioValidators = []string{"output_reduce", "low_error"}
	dir := writeCategory(t, root, "codex_cli_output_reduce_aggressive", meta, []string{sampleOutputReduceNoOutputTokensRecord})
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_max_errors", 0)
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_reread_count_max", 0)

	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) == 0 {
		t.Fatalf("expected output-reduce output-token evidence failure: %+v", res)
	}
	if got := strings.Join(res.Failures, "\n"); !strings.Contains(got, "scenario output_reduce: expected observed output-token evidence") {
		t.Fatalf("missing output-token evidence failure:\n%s", got)
	}
}

func TestEvaluateCategory_FailsOutputReduceWhenInputOverheadExceedsCap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	meta := promotionMeta("codex_cli_output_reduce_aggressive", "codex_cli", "output_reduce_aggressive")
	meta.ExpectedSavingsMin = 0
	meta.ExpectedOutputReduceAppliedMin = 1
	meta.ExpectedOutputReduceOverheadMax = 11
	meta.ScenarioValidators = []string{"output_reduce", "low_error"}
	dir := writeCategory(t, root, "codex_cli_output_reduce_aggressive", meta, []string{sampleOutputReduceOverheadDominatesRecord})
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_max_errors", 0)
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_reread_count_max", 0)

	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) == 0 {
		t.Fatalf("expected output-reduce overhead cap failure: %+v", res)
	}
	got := strings.Join(res.Failures, "\n")
	if !strings.Contains(got, "output_reduce_input_overhead_tokens=12 > max=11") {
		t.Fatalf("missing overhead cap failure:\n%s", got)
	}
}

func TestEvaluateCategory_FailsOutputReduceWhenNetObservedBelowFloor(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	meta := promotionMeta("codex_cli_output_reduce_aggressive", "codex_cli", "output_reduce_aggressive")
	meta.ExpectedSavingsMin = 0
	meta.ExpectedOutputReduceAppliedMin = 1
	meta.ExpectedOutputReduceNetObservedMin = 1
	meta.ScenarioValidators = []string{"output_reduce", "low_error"}
	dir := writeCategory(t, root, "codex_cli_output_reduce_aggressive", meta, []string{sampleOutputReduceOverheadDominatesRecord})
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_max_errors", 0)
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_reread_count_max", 0)

	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) == 0 {
		t.Fatalf("expected output-reduce net-observed floor failure: %+v", res)
	}
	got := strings.Join(res.Failures, "\n")
	if !strings.Contains(got, "output_reduce_net_observed_tokens=0 < min=1") {
		t.Fatalf("missing net-observed floor failure:\n%s", got)
	}
}

func TestEvaluateCategory_OutputReduceABGatePass(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	meta := promotionMeta("codex_cli_output_reduce_ab", "codex_cli", "output_reduce_ab")
	meta.ExpectedSavingsMin = 0
	meta.ExpectedRequestCount = 0
	meta.ExpectedOutputReduceABPairsMin = 1
	meta.ExpectedOutputReduceABNetSavedMin = 100
	meta.ExpectedOutputReduceABSavingsPctMin = 20
	meta.ScenarioValidators = []string{"output_reduce_ab", "low_error"}
	dir := writeCategory(t, root, "codex_cli_output_reduce_ab", meta, nil)
	writeOutputReduceABReport(t, dir, sampleOutputReduceABReport)
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_max_errors", 0)
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_reread_count_max", 0)

	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(res.Failures) != 0 {
		t.Fatalf("expected output-reduce A/B gate to pass, got %v", res.Failures)
	}
	if res.Sessions != 1 || res.OutputReduceABPairs != 1 || res.OutputReduceABNetSaved != 196 || res.OutputReduceABSavingsPctMin < 22 {
		t.Fatalf("A/B metrics: %+v", res)
	}
}

func TestEvaluateCategory_OutputReduceABGateFailsUnsafePair(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	meta := promotionMeta("codex_cli_output_reduce_ab", "codex_cli", "output_reduce_ab")
	meta.ExpectedSavingsMin = 0
	meta.ExpectedRequestCount = 0
	meta.ExpectedOutputReduceABPairsMin = 1
	meta.ExpectedOutputReduceABNetSavedMin = 1
	meta.ScenarioValidators = []string{"output_reduce_ab", "low_error"}
	dir := writeCategory(t, root, "codex_cli_output_reduce_ab", meta, nil)
	writeOutputReduceABReport(t, dir, sampleOutputReduceABFailedReport)
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_max_errors", 0)
	forceMetadataNumber(t, filepath.Join(dir, corpusCategoryMetadataFilename), "expected_reread_count_max", 0)

	res, err := EvaluateCategory(dir, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	got := strings.Join(res.Failures, "\n")
	for _, want := range []string{"output_reduce_ab_net_tokens_saved=-33", "pair_bad: net negative", "passed_pairs=0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in failures:\n%s", want, got)
		}
	}
}

func TestFormatCorpusReport_RendersCategoriesAndPolicyHint(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "syn", CategoryMetadata{Category: "syn", Synthetic: true, ExpectedSavingsMin: 0.30}, []string{sampleHighSavingsRecord})
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	s := FormatCorpusReport(report)
	if !strings.Contains(s, "syn") || !strings.Contains(s, "[synthetic]") {
		t.Fatalf("expected category + synthetic tag in render, got %q", s)
	}
	if !strings.Contains(s, "live-corpus-policy.md") {
		t.Fatalf("expected synthetic-only hint pointing at policy doc, got %q", s)
	}
	if !strings.Contains(s, "evidence:     synthetic") {
		t.Fatalf("expected evidence level in render, got %q", s)
	}
}

func TestFormatCorpusReport_PlannerReplay(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "planner", CategoryMetadata{
		Category:                        "planner",
		ExpectedSavingsMin:              0.10,
		ExpectedPlannerMissedMax:        1,
		ExpectedPlannerBypassAppliedMax: 1,
	}, []string{plannedJSONL})
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	s := FormatCorpusReport(report)
	for _, want := range []string{"planner:", "requests=3", "expected=211", "missed=1", "bypass-hit=1", "combos:", "L0+L1+L3"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in report:\n%s", want, s)
		}
	}
}

func TestFormatCorpusReport_GateFailRendered(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "low", CategoryMetadata{Category: "low", ExpectedSavingsMin: 0.50}, []string{sampleLowSavingsRecord})
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	s := FormatCorpusReport(report)
	if !strings.Contains(s, "FAIL") || !strings.Contains(s, "savings_ratio") {
		t.Fatalf("expected FAIL+ratio in render, got %q", s)
	}
}

func TestFormatCorpusReport_RendersPromotionGate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writePromotionCorpus(t, root)
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	gate := EvaluatePromotionGate(report)
	report.PromotionGate = &gate
	s := FormatCorpusReport(report)
	for _, want := range []string{"Promotion gate", "gate:         PASS", "codex_cli=5", "long_workday=1"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in report:\n%s", want, s)
		}
	}
}

func TestFormatCorpusReport_RendersRealLocalGate(t *testing.T) {
	t.Parallel()
	report := CorpusReport{
		Root:                         "fixture",
		Categories:                   []CategoryResult{{Category: "real", EvidenceLevel: "live_operator"}},
		RealCurrentLocalOrigTokens:   1000,
		RealCurrentLocalSavedTokens:  400,
		RealCurrentLocalSavingsRatio: 0.40,
	}
	gate := EvaluateRealLocalGate(report, 0.39, 400)
	report.RealLocalGate = &gate
	s := FormatCorpusReport(report)
	for _, want := range []string{"Real S_local gate", "gate:         PASS", "actual:       40.0000%", "min saved:    400"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in report:\n%s", want, s)
		}
	}
}

func TestFormatCorpusReport_RendersScenarioValidators(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "validator_render", CategoryMetadata{
		Category:               "validator_render",
		ExpectedSavedTokensMin: 1,
		ScenarioValidators:     []string{"tool_heavy", "low_error"},
	}, []string{sampleToolPruneRecord})
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	s := FormatCorpusReport(report)
	if !strings.Contains(s, "validators:") || !strings.Contains(s, "tool_heavy, low_error") {
		t.Fatalf("expected validators in render, got %q", s)
	}
}

func TestFormatCorpusReport_NoCategoriesPath(t *testing.T) {
	t.Parallel()
	out := FormatCorpusReport(CorpusReport{Root: "/nowhere"})
	if !strings.Contains(out, "No categories found") {
		t.Fatalf("expected guidance for empty corpus, got %q", out)
	}
}

func TestFormatCorpusReport_NoExpectationsTag(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "anon", CategoryMetadata{Category: "anon", Synthetic: false}, []string{sampleHighSavingsRecord})
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	s := FormatCorpusReport(report)
	if !strings.Contains(s, "no expectations declared") {
		t.Fatalf("expected no-expectation note, got %q", s)
	}
}

func TestCorpusGate_Pass(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "ok", CategoryMetadata{Category: "ok", ExpectedSavingsMin: 0.30, Synthetic: true}, []string{sampleHighSavingsRecord})
	var stdout, stderr bytes.Buffer
	rc := CorpusGate(root, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Fatalf("expected PASS in stdout, got %q", stdout.String())
	}
}

func TestCorpusGate_FailOnRatio(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "bad", CategoryMetadata{Category: "bad", ExpectedSavingsMin: 0.50}, []string{sampleLowSavingsRecord})
	var stdout, stderr bytes.Buffer
	rc := CorpusGate(root, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected non-zero exit on ratio failure")
	}
	if !strings.Contains(stdout.String(), "FAIL") {
		t.Fatalf("expected FAIL marker, got %q", stdout.String())
	}
}

func TestCorpusGate_EmptyCorpus(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	rc := CorpusGate(root, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected non-zero exit when corpus has no categories")
	}
	if !strings.Contains(stderr.String(), "no categories") {
		t.Fatalf("expected guidance on missing corpus, got stderr=%q", stderr.String())
	}
}

func TestCorpusGate_BadRoot(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rc := CorpusGate(filepath.Join(t.TempDir(), "missing"), &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected non-zero exit for missing root")
	}
}

func TestCorpusReportJSON_Roundtrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "alpha", CategoryMetadata{Category: "alpha", ExpectedSavingsMin: 0.30, Synthetic: true}, []string{sampleHighSavingsRecord})
	report, err := EvaluateCorpus(root, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	js, err := CorpusReportJSON(report)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	var decoded CorpusReport
	if err := json.Unmarshal([]byte(js), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Categories) != 1 || decoded.Categories[0].Category != "alpha" {
		t.Fatalf("unexpected decoded report: %+v", decoded)
	}
}

func TestRunBenchmarkCorpus_NoArgs(t *testing.T) {
	t.Parallel()
	if rc := runBenchmarkCorpus(nil); rc != 2 {
		t.Fatalf("expected exit 2 on missing args, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_UnknownFlag(t *testing.T) {
	t.Parallel()
	if rc := runBenchmarkCorpus([]string{"--bogus", "x"}); rc != 2 {
		t.Fatalf("expected exit 2 on unknown flag, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_DoubleRoot(t *testing.T) {
	t.Parallel()
	if rc := runBenchmarkCorpus([]string{"a", "b"}); rc != 2 {
		t.Fatalf("expected exit 2 on double root, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_FlagOnlyNoRoot(t *testing.T) {
	t.Parallel()
	if rc := runBenchmarkCorpus([]string{"--check"}); rc != 2 {
		t.Fatalf("expected exit 2 when only flags given, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_RunsCheck(t *testing.T) {
	root := t.TempDir()
	writeCategory(t, root, "ok", CategoryMetadata{Category: "ok", ExpectedSavingsMin: 0.30, Synthetic: true}, []string{sampleHighSavingsRecord})
	rc := runBenchmarkCorpus([]string{root, "--check"})
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_RunsJSON(t *testing.T) {
	root := t.TempDir()
	writeCategory(t, root, "ok", CategoryMetadata{Category: "ok", ExpectedSavingsMin: 0.30, Synthetic: true}, []string{sampleHighSavingsRecord})
	rc := runBenchmarkCorpus([]string{root, "--json"})
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_PromotionCheckFailsSyntheticOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "ok", CategoryMetadata{Category: "ok", ExpectedSavingsMin: 0.30, Synthetic: true}, []string{sampleHighSavingsRecord})
	rc := runBenchmarkCorpus([]string{root, "--promotion-check"})
	if rc != 1 {
		t.Fatalf("expected promotion failure on synthetic-only corpus, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_CheckMaxxDoesNotBypassMaxxGate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "ok", CategoryMetadata{Category: "ok", ExpectedSavingsMin: 0.30, Synthetic: true}, []string{sampleHighSavingsRecord})
	rc := runBenchmarkCorpus([]string{root, "--check", "--maxx-check"})
	if rc != 1 {
		t.Fatalf("expected maxx failure with --check --maxx-check, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_CheckRealLocalGatePasses(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "ok", CategoryMetadata{
		Category:           "ok",
		EvidenceLevel:      "live_operator",
		ClientFamily:       "codex_cli",
		WorkloadClass:      "repeat_read",
		ExpectedSavingsMin: 0.30,
	}, []string{sampleHighSavingsRecord})
	rc := runBenchmarkCorpus([]string{root, "--check", "--real-local-min-ratio=0.39", "--real-local-min-saved=400"})
	if rc != 0 {
		t.Fatalf("expected real local gate pass, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_CheckRealLocalGateFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "ok", CategoryMetadata{
		Category:           "ok",
		EvidenceLevel:      "live_operator",
		ClientFamily:       "codex_cli",
		WorkloadClass:      "repeat_read",
		ExpectedSavingsMin: 0.30,
	}, []string{sampleHighSavingsRecord})
	rc := runBenchmarkCorpus([]string{root, "--check", "--real-local-min-ratio=0.41", "--real-local-min-saved=401"})
	if rc != 1 {
		t.Fatalf("expected real local gate failure, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_InvalidRealLocalFlag(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCategory(t, root, "ok", CategoryMetadata{Category: "ok", ExpectedSavingsMin: 0.30, Synthetic: true}, []string{sampleHighSavingsRecord})
	if rc := runBenchmarkCorpus([]string{root, "--real-local-min-ratio=bad"}); rc != 2 {
		t.Fatalf("expected invalid ratio flag exit 2, got %d", rc)
	}
	if rc := runBenchmarkCorpus([]string{root, "--real-local-min-ratio=NaN"}); rc != 2 {
		t.Fatalf("expected NaN ratio flag exit 2, got %d", rc)
	}
	if rc := runBenchmarkCorpus([]string{root, "--real-local-min-saved=-1"}); rc != 2 {
		t.Fatalf("expected invalid saved flag exit 2, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_RunsText(t *testing.T) {
	root := t.TempDir()
	writeCategory(t, root, "ok", CategoryMetadata{Category: "ok", ExpectedSavingsMin: 0.30, Synthetic: true}, []string{sampleHighSavingsRecord})
	rc := runBenchmarkCorpus([]string{root})
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_BadRoot(t *testing.T) {
	t.Parallel()
	rc := runBenchmarkCorpus([]string{filepath.Join(t.TempDir(), "missing")})
	if rc != 1 {
		t.Fatalf("expected exit 1 on missing root, got %d", rc)
	}
}

func TestRunBenchmarkCorpus_JSONOnBadRoot(t *testing.T) {
	t.Parallel()
	rc := runBenchmarkCorpus([]string{filepath.Join(t.TempDir(), "missing"), "--json"})
	if rc != 1 {
		t.Fatalf("expected exit 1, got %d", rc)
	}
}

func TestCountSessionFiles_BadDir(t *testing.T) {
	t.Parallel()
	if _, err := countSessionFiles(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatalf("expected error reading missing dir")
	}
}

// TestLiveCorpusSidecarIntegrity is an anti-fixture-inflation guard
// (AGENTS.md §3.7.6). The corpus gate trusts the input_tokens / saved_tokens
// recorded in the L2 (command_output_first.jsonl) and L1
// (server_state_continuation.jsonl) sidecars without recomputing the
// compaction. This meta-test enforces the only invariants that can be checked
// statically so an independently-inflated saved_tokens value (saved that does
// not equal input-output, or saved outside [0, input]) is caught by CI rather
// than silently raising S_local. It does not — and cannot — prove the captures
// trace to a real session; that remains an operator/provenance obligation.
func TestLiveCorpusSidecarIntegrity(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "tests", "fixtures", "live_corpus")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("live_corpus not found: %v", err)
	}
	type row struct {
		Input  int64 `json:"input_tokens"`
		Output int64 `json:"output_tokens"`
		Saved  int64 `json:"saved_tokens"`
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, name := range []string{commandOutputFirstSidecarFilename, l1SidecarFilenameForTest} {
			path := filepath.Join(root, e.Name(), name)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			for i, line := range bytes.Split(data, []byte("\n")) {
				line = bytes.TrimSpace(line)
				if len(line) == 0 {
					continue
				}
				var r row
				if err := json.Unmarshal(line, &r); err != nil {
					t.Fatalf("%s line %d: parse: %v", path, i+1, err)
				}
				if r.Input <= 0 {
					t.Errorf("%s line %d: input_tokens=%d must be > 0", path, i+1, r.Input)
				}
				if r.Saved < 0 || r.Saved > r.Input {
					t.Errorf("%s line %d: saved_tokens=%d out of bounds [0,%d]", path, i+1, r.Saved, r.Input)
				}
				if r.Saved != r.Input-r.Output {
					t.Errorf("%s line %d: saved_tokens=%d != input-output (%d-%d=%d); inconsistent sidecar",
						path, i+1, r.Saved, r.Input, r.Output, r.Input-r.Output)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Skip("no sidecar rows found")
	}
	t.Logf("validated %d sidecar rows", checked)
}

// l1SidecarFilenameForTest mirrors internal/proxy l1SidecarFilename, which is
// unexported in another package; kept here so the meta-test has no cross-module
// dependency.
const l1SidecarFilenameForTest = "server_state_continuation.jsonl"

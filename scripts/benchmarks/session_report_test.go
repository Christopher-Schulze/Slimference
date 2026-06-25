package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jsonStr(s string) json.RawMessage { b, _ := json.Marshal(s); return b }

func TestRecomputeInBandProvenance(t *testing.T) {
	t.Parallel()
	// No provenance fields -> attested (nil, nil).
	if v, err := recomputeInBandProvenance(map[string]json.RawMessage{}); err != nil || v != nil {
		t.Fatalf("no provenance must be (nil,nil), got v=%v err=%v", v, err)
	}
	// Valid: orig 2000 bytes -> 500 tok, final 1200 bytes -> 300 tok, saved 200.
	raw := bytes.Repeat([]byte("x"), 2000)
	fin := bytes.Repeat([]byte("y"), 1200)
	sum := sha256.Sum256(raw)
	m := map[string]json.RawMessage{
		"orig_sha256":    jsonStr(hex.EncodeToString(sum[:])),
		"orig_gzip_b64":  jsonStr(gzB64(raw)),
		"final_gzip_b64": jsonStr(gzB64(fin)),
	}
	v, err := recomputeInBandProvenance(m)
	if err != nil || v == nil {
		t.Fatalf("valid provenance must verify, got v=%v err=%v", v, err)
	}
	if v.orig != 500 || v.saved != 200 {
		t.Fatalf("recompute mismatch: orig=%d saved=%d want 500/200", v.orig, v.saved)
	}
	// Tamper: wrong sha -> fail closed.
	m["orig_sha256"] = jsonStr("deadbeef")
	if _, err := recomputeInBandProvenance(m); !errors.Is(err, errSidecarIntegrity) {
		t.Fatalf("sha mismatch must be errSidecarIntegrity, got %v", err)
	}
	// final larger than orig -> fail closed.
	m2 := map[string]json.RawMessage{"orig_gzip_b64": jsonStr(gzB64(fin)), "final_gzip_b64": jsonStr(gzB64(raw))}
	if _, err := recomputeInBandProvenance(m2); !errors.Is(err, errSidecarIntegrity) {
		t.Fatalf("final>orig must be errSidecarIntegrity, got %v", err)
	}
}

func TestAggregateSessions_InBandProvenanceVerified(t *testing.T) {
	t.Parallel()
	raw := bytes.Repeat([]byte("x"), 2000)
	fin := bytes.Repeat([]byte("y"), 1200)
	sum := sha256.Sum256(raw)
	line := `{"tokens":{"original":500,"final":300,"saved":200},` +
		`"orig_sha256":"` + hex.EncodeToString(sum[:]) + `",` +
		`"orig_gzip_b64":"` + gzB64(raw) + `","final_gzip_b64":"` + gzB64(fin) + `"}`
	agg, err := AggregateSessions(strings.NewReader(line+"\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if agg.inBandVerifiedSaved != 200 || agg.inBandVerifiedOrig != 500 {
		t.Fatalf("verified in-band: got orig=%d saved=%d want 500/200", agg.inBandVerifiedOrig, agg.inBandVerifiedSaved)
	}
	if agg.inBandAttestedSaved != 0 {
		t.Fatalf("a verified row must not also count as attested, got %d", agg.inBandAttestedSaved)
	}
	agg2, err := AggregateSessions(strings.NewReader(`{"tokens":{"original":100,"final":60,"saved":40}}`+"\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("aggregate2: %v", err)
	}
	if agg2.inBandVerifiedSaved != 0 || agg2.inBandAttestedSaved != 40 {
		t.Fatalf("no-provenance row must be attested-only, got verified=%d attested=%d", agg2.inBandVerifiedSaved, agg2.inBandAttestedSaved)
	}
}

const sampleJSONL = `{"req_id":"r1","ts":"2026-04-18T12:00:00Z","provider":"anthropic","model":"claude","total_messages":10,"messages_in_window":5,"layers_applied":[1],"tokens":{"original":1000,"after_layer0":1000,"after_layer1":700,"final":500,"saved":500,"ratio":0.5},"layer1_breakdown":{"dedup":{"blocks":2,"saved":150},"json_compact":{"blocks":1,"saved":100}},"cache_hit":false,"proxy_latency_ms":50}
{"req_id":"r2","ts":"2026-04-18T12:00:05Z","provider":"openai","model":"gpt-4o","total_messages":6,"messages_in_window":4,"layers_applied":[3],"tokens":{"original":600,"after_layer0":600,"after_layer1":600,"final":600,"saved":0,"ratio":1.0},"layer1_breakdown":{},"cache_hit":true,"proxy_latency_ms":2}
`

const codexJSONL = `{"req_id":"c1","ts":"2026-04-29T12:00:00Z","provider":"codex_chatgpt","model":"codex-cli","codex_route":"/v1/responses","total_messages":8,"messages_in_window":4,"layers_applied":[0,1,3],"tokens":{"original":2000,"after_layer0":1800,"after_layer1":1200,"final":900,"saved":1100,"ratio":0.45},"layer1_breakdown":{"tool_compressor":{"blocks":2,"saved":500},"json_compact":{"blocks":1,"saved":100}},"cache_hit":true,"cache_read_tokens":300,"cache_create_tokens":120,"proxy_latency_ms":12}
`

const plannedJSONL = `{"req_id":"p1","provider":"openai","model":"gpt-5","route_mode":"upstream","layers_applied":[0,1],"tokens":{"original":1000,"after_layer0":900,"after_layer1":700,"final":700,"saved":300},"output_reduce":{"applied":true},"plan":{"provider":"openai","route_mode":"upstream","decisions":[{"layer":"l0","action":"run","reason":"tool","expected_savings_tokens":100,"risk":"low","confidence":"high"},{"layer":"l1","action":"cheap_only","reason":"recent","expected_savings_tokens":40,"risk":"low","confidence":"medium"},{"layer":"l3_output","action":"run","reason":"output","expected_savings_tokens":50,"risk":"medium","confidence":"high"},{"layer":"websocket","action":"bypass","reason":"not_ws","risk":"none","confidence":"high"}]}}` + "\n" +
	`{"req_id":"p2","provider":"codex_chatgpt","route_mode":"websocket_tunnel","tokens":{"original":100,"after_layer0":100,"after_layer1":100,"final":100,"saved":0},"plan":{"provider":"codex_chatgpt","route_mode":"websocket_tunnel","safety_blocked":true,"decisions":[{"layer":"websocket","action":"tunnel","reason":"operator_disabled","risk":"none","confidence":"high"},{"layer":"","action":"run"},{"layer":"l2","action":"bypass","reason":"small","risk":"none","confidence":"high"}]}}` + "\n" +
	`{"req_id":"p3","provider":"openai","route_mode":"upstream","cache_hit":true,"cache_read_tokens":5,"previous_response_id_used":true,"tokens":{"original":100,"after_layer0":90,"after_layer1":80,"final":60,"saved":40},"flight":{"route_mode":"upstream","plan":{"provider":"openai","route_mode":"upstream","decisions":[{"layer":"l2","action":"run","reason":"cache","expected_savings_tokens":20,"risk":"low","confidence":"provider_reported"},{"layer":"unknown","action":"run","reason":"unknown","expected_savings_tokens":1,"risk":"medium","confidence":"low"}]}}}` + "\n"

func TestAggregateSessions_HappyPath(t *testing.T) {
	t.Parallel()
	agg, err := AggregateSessions(strings.NewReader(sampleJSONL), nil)
	if err != nil {
		t.Fatal(err)
	}
	if agg.requests != 2 {
		t.Fatalf("requests: %d", agg.requests)
	}
	if agg.origTokens != 1600 {
		t.Fatalf("origTokens: %d", agg.origTokens)
	}
	if agg.savedTokens != 500 {
		t.Fatalf("savedTokens: %d", agg.savedTokens)
	}
	if agg.layer1Saved != 300 {
		t.Fatalf("layer1Saved: %d", agg.layer1Saved)
	}
	if agg.cacheHits != 1 {
		t.Fatalf("cacheHits: %d", agg.cacheHits)
	}
	if agg.perSubLayer["dedup"] != 150 || agg.perSubLayer["json_compact"] != 100 {
		t.Fatalf("perSubLayer: %+v", agg.perSubLayer)
	}
	if agg.perProvider["anthropic"] != 1 || agg.perProvider["openai"] != 1 {
		t.Fatalf("perProvider: %+v", agg.perProvider)
	}
	if agg.layerCombinations["L1"].Requests != 1 || agg.layerCombinations["L2"].Requests != 1 {
		t.Fatalf("layer combinations: %+v", agg.layerCombinations)
	}
}

func TestAggregateSessions_CodexFields(t *testing.T) {
	t.Parallel()
	agg, err := AggregateSessions(strings.NewReader(codexJSONL), nil)
	if err != nil {
		t.Fatal(err)
	}
	if agg.layer0Saved != 200 || agg.layer1Saved != 600 || agg.layer2Saved != 300 {
		t.Fatalf("layers: l0=%d l1=%d l3=%d", agg.layer0Saved, agg.layer1Saved, agg.layer2Saved)
	}
	if agg.perProvider["codex_chatgpt"] != 1 || agg.perCodexRoute["/v1/responses"] != 1 {
		t.Fatalf("splits provider=%+v route=%+v", agg.perProvider, agg.perCodexRoute)
	}
	if agg.cacheReadSum != 300 || agg.cacheCreateSum != 120 {
		t.Fatalf("prompt cache read=%d create=%d", agg.cacheReadSum, agg.cacheCreateSum)
	}
	if combo := agg.layerCombinations["L0+L1+L2"]; combo.Requests != 1 || combo.SavedTokens != 1100 {
		t.Fatalf("codex layer combination: %+v", agg.layerCombinations)
	}
}

func TestAggregateSessions_PlannedVsActual(t *testing.T) {
	t.Parallel()
	agg, err := AggregateSessions(strings.NewReader(plannedJSONL), nil)
	if err != nil {
		t.Fatal(err)
	}
	if agg.planReplay.RequestsWithPlan != 3 || agg.planReplay.Decisions != 8 {
		t.Fatalf("plan replay counts: %+v", agg.planReplay)
	}
	if agg.planReplay.ExpectedSavingsTokens != 211 {
		t.Fatalf("expected plan savings=%d", agg.planReplay.ExpectedSavingsTokens)
	}
	if agg.planReplay.ExpectedActive != 5 || agg.planReplay.ObservedActive != 4 || agg.planReplay.MissedActive != 1 {
		t.Fatalf("active replay: %+v", agg.planReplay)
	}
	if agg.planReplay.BypassApplied != 1 || agg.planReplay.SafetyBlocked != 1 {
		t.Fatalf("bypass/blocked replay: %+v", agg.planReplay)
	}
	if agg.planReplay.ActionCounts["run"] != 4 || agg.planReplay.ActionCounts["cheap_only"] != 1 ||
		agg.planReplay.ActionCounts["bypass"] != 2 || agg.planReplay.ActionCounts["tunnel"] != 1 {
		t.Fatalf("action counts: %+v", agg.planReplay.ActionCounts)
	}
	if agg.planReplay.RiskCounts["medium"] != 2 || agg.planReplay.RiskCounts["none"] != 3 {
		t.Fatalf("risk counts: %+v", agg.planReplay.RiskCounts)
	}
	cloned := clonePlanReplayAggregate(agg.planReplay)
	agg.planReplay.ActionCounts["run"] = 999
	if cloned.ActionCounts["run"] != 4 {
		t.Fatalf("clone aliased action counts: %+v", cloned.ActionCounts)
	}
}

func TestAggregateSessions_MalformedLineSkipped(t *testing.T) {
	t.Parallel()
	corpus := sampleJSONL + "{not json}\n"
	var errBuf bytes.Buffer
	agg, err := AggregateSessions(strings.NewReader(corpus), &errBuf)
	if err != nil {
		t.Fatal(err)
	}
	if agg.requests != 2 {
		t.Fatalf("malformed line must be skipped, got %d", agg.requests)
	}
	if !strings.Contains(errBuf.String(), "malformed") {
		t.Fatalf("warning missing: %q", errBuf.String())
	}
}

func TestAggregateSessions_EmptyLineSkipped(t *testing.T) {
	t.Parallel()
	agg, err := AggregateSessions(strings.NewReader("\n\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if agg.requests != 0 {
		t.Fatalf("requests: %d", agg.requests)
	}
}

func TestFormatSessionReport_Empty(t *testing.T) {
	t.Parallel()
	if got := FormatSessionReport(newSessionReportAggregate()); !strings.Contains(got, "No session records") {
		t.Fatalf("expected empty message: %s", got)
	}
}

func TestFormatSessionReport_NonEmpty(t *testing.T) {
	t.Parallel()
	agg, _ := AggregateSessions(strings.NewReader(sampleJSONL), nil)
	out := FormatSessionReport(agg)
	for _, need := range []string{"Requests:", "Original tokens:", "Layer 0 saved:", "Layer 1 saved:", "Layer 2 saved:", "Cache hit rate:", "Layer combination breakdown:", "L1", "anthropic", "openai"} {
		if !strings.Contains(out, need) {
			t.Fatalf("missing %q in report:\n%s", need, out)
		}
	}
}

func TestFormatSessionReport_CodexRoute(t *testing.T) {
	t.Parallel()
	agg, _ := AggregateSessions(strings.NewReader(codexJSONL), nil)
	out := FormatSessionReport(agg)
	for _, need := range []string{"codex_chatgpt", "Codex traffic route count:", "/v1/responses", "Prompt cache read:"} {
		if !strings.Contains(out, need) {
			t.Fatalf("missing %q in report:\n%s", need, out)
		}
	}
}

func TestFormatSessionReport_PlannerReplay(t *testing.T) {
	t.Parallel()
	agg, _ := AggregateSessions(strings.NewReader(plannedJSONL), nil)
	out := FormatSessionReport(agg)
	for _, need := range []string{"Planner requests:", "Planner expected:", "Planner active:", "Planner misses:", "Planner bypass hit:", "Planner blocked:"} {
		if !strings.Contains(out, need) {
			t.Fatalf("missing %q in report:\n%s", need, out)
		}
	}
}

func TestFormatSessionReport_HostBudgetRows(t *testing.T) {
	t.Parallel()
	agg, err := AggregateSessions(strings.NewReader(sampleHostBudgetOKRecord+sampleHostBudgetIssueRecord), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if agg.hostBudgetOK != 1 || agg.hostBudgetIssues != 1 {
		t.Fatalf("host budget aggregate: ok=%d issue=%d", agg.hostBudgetOK, agg.hostBudgetIssues)
	}
	out := FormatSessionReport(agg)
	if !strings.Contains(out, "Host budget ok/issue:1 / 1") {
		t.Fatalf("host budget not rendered:\n%s", out)
	}
}

func TestFormatSessionMarkdown_Empty(t *testing.T) {
	t.Parallel()
	if got := FormatSessionMarkdown(newSessionReportAggregate()); !strings.Contains(got, "no session records") {
		t.Fatalf("got %s", got)
	}
}

func TestFormatSessionMarkdown_Nonempty(t *testing.T) {
	t.Parallel()
	agg, _ := AggregateSessions(strings.NewReader(sampleJSONL), nil)
	md := FormatSessionMarkdown(agg)
	for _, need := range []string{"| Metric | Value |", "| Requests | 2 |", "| Savings ratio |", "| Layer combination | Requests |", "| L1 | 1 |", "| Provider | Requests |"} {
		if !strings.Contains(md, need) {
			t.Fatalf("missing %q in markdown:\n%s", need, md)
		}
	}
}

func TestFormatSessionMarkdown_CodexRoute(t *testing.T) {
	t.Parallel()
	agg, _ := AggregateSessions(strings.NewReader(codexJSONL), nil)
	md := FormatSessionMarkdown(agg)
	for _, need := range []string{"| Codex traffic route | Requests |", "| /v1/responses | 1 |", "| Prompt cache read tokens | 300 |"} {
		if !strings.Contains(md, need) {
			t.Fatalf("missing %q in markdown:\n%s", need, md)
		}
	}
}

func TestFormatSessionMarkdown_PlannerReplay(t *testing.T) {
	t.Parallel()
	agg, _ := AggregateSessions(strings.NewReader(plannedJSONL), nil)
	md := FormatSessionMarkdown(agg)
	for _, need := range []string{"| Planner requests | 3 |", "| Planner expected savings | 211 |", "| Planner active observed | 4 / 5 |"} {
		if !strings.Contains(md, need) {
			t.Fatalf("missing %q in markdown:\n%s", need, md)
		}
	}
}

func TestSessionReportFromPath_UnreadableFile(t *testing.T) {
	t.Parallel()
	if code := sessionReportFromPath(filepath.Join(t.TempDir(), "nope.jsonl"), "text"); code != 1 {
		t.Fatalf("missing file must exit 1, got %d", code)
	}
}

func TestSessionReportFromPath_Happy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte(sampleJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := sessionReportFromPath(path, "text"); code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	if code := sessionReportFromPath(path, "markdown"); code != 0 {
		t.Fatalf("expected 0 for markdown, got %d", code)
	}
}

func TestAggregateSessionsFromPath_Directory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(sampleJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "codex.jsonl"), []byte(codexJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.json"), []byte(codexJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	agg, err := AggregateSessionsFromPath(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if agg.requests != 3 || agg.perProvider["codex_chatgpt"] != 1 || agg.perCodexRoute["/v1/responses"] != 1 {
		t.Fatalf("aggregate: requests=%d providers=%+v routes=%+v", agg.requests, agg.perProvider, agg.perCodexRoute)
	}
}

func TestAggregateSessionsFromPath_CheckedInCodexFixture(t *testing.T) {
	t.Parallel()
	agg, err := AggregateSessionsFromPath(filepath.Join("..", "..", "tests", "fixtures", "codex"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if agg.requests != 2 {
		t.Fatalf("requests=%d", agg.requests)
	}
	if agg.perProvider["codex_chatgpt"] != 2 {
		t.Fatalf("providers=%+v", agg.perProvider)
	}
	if agg.perCodexRoute["/v1/responses"] != 1 || agg.perCodexRoute["/backend-api/codex/responses"] != 1 {
		t.Fatalf("routes=%+v", agg.perCodexRoute)
	}
	if agg.layer0Saved == 0 || agg.layer1Saved == 0 || agg.layer2Saved == 0 {
		t.Fatalf("layers l0=%d l1=%d l3=%d", agg.layer0Saved, agg.layer1Saved, agg.layer2Saved)
	}
}

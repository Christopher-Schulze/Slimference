package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleJSONL = `{"req_id":"r1","ts":"2026-04-18T12:00:00Z","provider":"anthropic","model":"claude","total_messages":10,"messages_in_window":5,"layers_applied":[1,2],"tokens":{"original":1000,"after_layer0":1000,"after_layer1":700,"after_layer2":500,"final":500,"saved":500,"ratio":0.5},"layer1_breakdown":{"dedup":{"blocks":2,"saved":150},"json_compact":{"blocks":1,"saved":100}},"cache_hit":false,"proxy_latency_ms":50}
{"req_id":"r2","ts":"2026-04-18T12:00:05Z","provider":"openai","model":"gpt-4o","total_messages":6,"messages_in_window":4,"layers_applied":[3],"tokens":{"original":600,"after_layer0":600,"after_layer1":600,"after_layer2":600,"final":600,"saved":0,"ratio":1.0},"layer1_breakdown":{},"cache_hit":true,"proxy_latency_ms":2}
`

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
	if agg.layer2Saved != 200 {
		t.Fatalf("layer2Saved: %d", agg.layer2Saved)
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
	for _, need := range []string{"Requests:", "Original tokens:", "Layer 1 saved:", "Cache hit rate:", "anthropic", "openai"} {
		if !strings.Contains(out, need) {
			t.Fatalf("missing %q in report:\n%s", need, out)
		}
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
	for _, need := range []string{"| Metric | Value |", "| Requests | 2 |", "| Savings ratio |"} {
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

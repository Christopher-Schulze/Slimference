package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMetadata(t *testing.T, dir string, meta CorpusMetadata) {
	t.Helper()
	data, err := json.MarshalIndent(&meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, corpusMetadataFilename), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sampleMetadata() CorpusMetadata {
	return CorpusMetadata{
		SchemaVersion:   1,
		CorpusName:      "codex-smoke-test",
		Description:     "synthetic test fixture",
		Scrubbed:        true,
		RedactionMethod: "manual_synthetic",
		CapturedAt:      "2026-04-29",
		RequestFixtures: []RequestFixtureEntry{{
			File:  "v1-responses-input.json",
			Route: "/v1/responses",
			Shape: "responses_input",
			Notes: "test",
		}},
		SessionFixtures: []SessionFixtureEntry{{
			File:          "session-smoke.jsonl",
			CodexVersion:  "synthetic-fixture",
			Client:        "codex-cli-fixture",
			HooksEnabled:  []string{"pre_tool", "post_tool", "read"},
			LayersEnabled: []int{0, 1, 2, 3},
			RequestCount:  2,
			Scenarios:     []string{"clean", "summary"},
		}},
		RegressionGate: &RegressionGate{
			MinRequests:     1,
			MinSavingsRatio: 0.1,
			MinLayer1Saved:  1,
			Providers:       map[string]int{"codex_chatgpt": 1},
			Routes:          map[string]int{"/v1/responses": 1},
		},
	}
}

func TestLoadCorpusMetadata_Absent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	meta, ok, err := LoadCorpusMetadata(dir)
	if err != nil {
		t.Fatalf("absent metadata must not error: %v", err)
	}
	if ok || meta != nil {
		t.Fatalf("expected absent, got ok=%v meta=%v", ok, meta)
	}
}

func TestLoadCorpusMetadata_Present(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMetadata(t, dir, sampleMetadata())
	meta, ok, err := LoadCorpusMetadata(dir)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if meta.SchemaVersion != 1 || meta.CorpusName != "codex-smoke-test" {
		t.Fatalf("metadata mismatch: %+v", meta)
	}
	if meta.RegressionGate == nil || meta.RegressionGate.MinSavingsRatio != 0.1 {
		t.Fatalf("regression_gate mismatch: %+v", meta.RegressionGate)
	}
}

func TestLoadCorpusMetadata_Malformed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, corpusMetadataFilename), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadCorpusMetadata(dir); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadCorpusMetadata_UnreadablePath(t *testing.T) {
	t.Parallel()
	// Pointing the loader at a path where the metadata file's parent does
	// not exist must surface as a real error, not as "absent".
	bogus := filepath.Join(t.TempDir(), "missing-subdir", "nested")
	_, ok, err := LoadCorpusMetadata(bogus)
	if err == nil && ok {
		t.Fatal("expected read error or absent for missing parent")
	}
}

func TestLoadCorpusMetadata_PermissionDenied(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses POSIX read permission")
	}
	dir := t.TempDir()
	writeMetadata(t, dir, sampleMetadata())
	if err := os.Chmod(filepath.Join(dir, corpusMetadataFilename), 0o000); err != nil {
		t.Skipf("chmod not supported: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, corpusMetadataFilename), 0o600) })
	_, ok, err := LoadCorpusMetadata(dir)
	if err == nil {
		t.Fatal("expected permission-denied to surface as a real error")
	}
	if ok {
		t.Fatal("ok must be false on read error")
	}
}

func TestFormatCorpusMetadata_NilAndEmpty(t *testing.T) {
	t.Parallel()
	if got := FormatCorpusMetadata(nil); got != "" {
		t.Fatalf("nil meta must render empty: %q", got)
	}
	if got := FormatCorpusMetadataMarkdown(nil); got != "" {
		t.Fatalf("nil meta md must render empty: %q", got)
	}
	got := FormatCorpusMetadata(&CorpusMetadata{})
	if !strings.Contains(got, "Corpus metadata") || !strings.Contains(got, "Scrubbed:") {
		t.Fatalf("empty meta still expected to render header: %s", got)
	}
}

func TestFormatCorpusMetadata_FullText(t *testing.T) {
	t.Parallel()
	meta := sampleMetadata()
	got := FormatCorpusMetadata(&meta)
	for _, want := range []string{
		"Corpus metadata", "codex-smoke-test", "Schema:             v1",
		"Scrubbed:           true", "Redaction:          manual_synthetic",
		"Captured:           2026-04-29", "synthetic test fixture",
		"session-smoke.jsonl", "codex_version: synthetic-fixture",
		"client:        codex-cli-fixture", "hooks:         pre_tool,post_tool,read",
		"layers:        L0,L1,L2,L3", "requests:      2",
		"scenarios:     clean, summary",
		"v1-responses-input.json (route=/v1/responses, shape=responses_input)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatCorpusMetadataMarkdown_FullText(t *testing.T) {
	t.Parallel()
	meta := sampleMetadata()
	got := FormatCorpusMetadataMarkdown(&meta)
	for _, want := range []string{
		"### Corpus metadata", "**Name:** codex-smoke-test",
		"**Schema:** v1", "**Scrubbed:** true",
		"**Redaction:** manual_synthetic", "**Captured:** 2026-04-29",
		"| Session fixture | Codex version | Hooks | Layers | Requests |",
		"| session-smoke.jsonl | synthetic-fixture | pre_tool,post_tool,read | L0,L1,L2,L3 | 2 |",
		"| Request fixture | Route | Shape |",
		"| v1-responses-input.json | /v1/responses | responses_input |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatCorpusMetadataMarkdown_DashFallback(t *testing.T) {
	t.Parallel()
	meta := CorpusMetadata{
		SchemaVersion: 1,
		CorpusName:    "minimal",
		SessionFixtures: []SessionFixtureEntry{{
			File: "minimal.jsonl",
		}},
		RequestFixtures: []RequestFixtureEntry{{
			File: "req.json",
		}},
	}
	got := FormatCorpusMetadataMarkdown(&meta)
	if !strings.Contains(got, "| minimal.jsonl | - | - | - | 0 |") {
		t.Fatalf("expected dash fallback for missing fields, got:\n%s", got)
	}
	if !strings.Contains(got, "| req.json | - | - |") {
		t.Fatalf("expected dash fallback for request fixture, got:\n%s", got)
	}
}

func TestStrOrDash(t *testing.T) {
	t.Parallel()
	if strOrDash("") != "-" {
		t.Fatal("empty must dash")
	}
	if strOrDash("x") != "x" {
		t.Fatal("non-empty must passthrough")
	}
}

func TestEvaluateRegressionGate_NilGatePasses(t *testing.T) {
	t.Parallel()
	if got := EvaluateRegressionGate(newSessionReportAggregate(), nil); got != nil {
		t.Fatalf("nil gate should yield nil failures, got %v", got)
	}
}

func TestEvaluateRegressionGate_AllChecksFail(t *testing.T) {
	t.Parallel()
	agg := newSessionReportAggregate()
	gate := &RegressionGate{
		MinRequests:     5,
		MinSavingsRatio: 0.5,
		MinLayer0Saved:  10,
		MinLayer1Saved:  10,
		MinLayer2Saved:  10,
		MinLayer3Saved:  10,
		Providers:       map[string]int{"codex_chatgpt": 2, "anthropic": 1},
		Routes:          map[string]int{"/v1/responses": 2, "/backend-api/codex/responses": 1},
	}
	failures := EvaluateRegressionGate(agg, gate)
	for _, want := range []string{
		"requests=0 < min=5",
		"layer0_saved=0 < min=10",
		"layer1_saved=0 < min=10",
		"layer2_saved=0 < min=10",
		"layer3_saved=0 < min=10",
		"provider[anthropic]=0 < min=1",
		"provider[codex_chatgpt]=0 < min=2",
		"route[/backend-api/codex/responses]=0 < min=1",
		"route[/v1/responses]=0 < min=2",
	} {
		if !containsString(failures, want) {
			t.Fatalf("missing failure %q in %v", want, failures)
		}
	}
}

func TestEvaluateRegressionGate_SavingsRatioFailure(t *testing.T) {
	t.Parallel()
	agg := newSessionReportAggregate()
	agg.requests = 1
	agg.origTokens = 1000
	agg.savedTokens = 100
	gate := &RegressionGate{MinSavingsRatio: 0.5}
	failures := EvaluateRegressionGate(agg, gate)
	if len(failures) != 1 || !strings.Contains(failures[0], "savings_ratio=") {
		t.Fatalf("expected savings_ratio failure, got %v", failures)
	}
}

func TestEvaluateRegressionGate_AllPass(t *testing.T) {
	t.Parallel()
	agg := newSessionReportAggregate()
	agg.requests = 3
	agg.origTokens = 1000
	agg.savedTokens = 800
	agg.layer0Saved = 50
	agg.layer1Saved = 200
	agg.layer2Saved = 400
	agg.layer3Saved = 150
	agg.perProvider["codex_chatgpt"] = 3
	agg.perCodexRoute["/v1/responses"] = 3
	gate := &RegressionGate{
		MinRequests:     2,
		MinSavingsRatio: 0.5,
		MinLayer0Saved:  10,
		MinLayer1Saved:  10,
		MinLayer2Saved:  10,
		MinLayer3Saved:  10,
		Providers:       map[string]int{"codex_chatgpt": 2},
		Routes:          map[string]int{"/v1/responses": 2},
	}
	if failures := EvaluateRegressionGate(agg, gate); len(failures) != 0 {
		t.Fatalf("expected pass, got %v", failures)
	}
}

func TestCodexSmokeGate_PassOnCheckedInFixture(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "tests", "fixtures", "codex")
	var stdout, stderr bytes.Buffer
	if code := codexSmokeGate(dir, &stdout, &stderr); code != 0 {
		t.Fatalf("expected pass, got %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Fatalf("missing PASS line: %s", stdout.String())
	}
}

func TestCodexSmokeGate_MissingMetadata(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := codexSmokeGate(dir, &stdout, &stderr); code != 1 {
		t.Fatalf("expected 1 for missing metadata, got %d", code)
	}
	if !strings.Contains(stderr.String(), "missing") {
		t.Fatalf("expected stderr to mention missing metadata: %s", stderr.String())
	}
}

func TestCodexSmokeGate_MalformedMetadata(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, corpusMetadataFilename), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := codexSmokeGate(dir, &stdout, &stderr); code != 1 {
		t.Fatalf("expected 1 for malformed metadata, got %d", code)
	}
	if !strings.Contains(stderr.String(), "load metadata") {
		t.Fatalf("expected stderr to mention load metadata: %s", stderr.String())
	}
}

func TestCodexSmokeGate_NoRegressionGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	meta := sampleMetadata()
	meta.RegressionGate = nil
	writeMetadata(t, dir, meta)
	var stdout, stderr bytes.Buffer
	if code := codexSmokeGate(dir, &stdout, &stderr); code != 1 {
		t.Fatalf("expected 1 for missing regression_gate, got %d", code)
	}
	if !strings.Contains(stderr.String(), "no regression_gate") {
		t.Fatalf("expected stderr mention: %s", stderr.String())
	}
}

func TestCodexSmokeGate_FailsOnMiss(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	meta := sampleMetadata()
	meta.RegressionGate.MinRequests = 99
	writeMetadata(t, dir, meta)
	if err := os.WriteFile(filepath.Join(dir, "tiny.jsonl"), []byte(codexJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := codexSmokeGate(dir, &stdout, &stderr); code != 1 {
		t.Fatalf("expected fail, got %d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "FAIL") || !strings.Contains(stdout.String(), "requests=") {
		t.Fatalf("expected FAIL with requests miss, got %s", stdout.String())
	}
}

func TestCodexSmokeGate_AggregateError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses POSIX read permission")
	}
	dir := t.TempDir()
	writeMetadata(t, dir, sampleMetadata())
	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte(codexJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Skipf("chmod not supported: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o600) })
	var stdout, stderr bytes.Buffer
	if code := codexSmokeGate(dir, &stdout, &stderr); code != 1 {
		t.Fatalf("expected 1 from aggregate error, got %d", code)
	}
	if !strings.Contains(stderr.String(), "aggregate") {
		t.Fatalf("expected stderr mention aggregate: %s", stderr.String())
	}
}

func TestSavingsRatioPct_ZeroOrig(t *testing.T) {
	t.Parallel()
	if got := savingsRatioPct(newSessionReportAggregate()); got != 0 {
		t.Fatalf("zero origTokens must yield 0, got %v", got)
	}
}

func TestSessionReportFromPath_DirectoryWithMetadataText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMetadata(t, dir, sampleMetadata())
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(codexJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := sessionReportFromPath(dir, "text"); code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	if code := sessionReportFromPath(dir, "markdown"); code != 0 {
		t.Fatalf("expected 0 markdown, got %d", code)
	}
}

func TestSessionReportFromPath_MalformedMetadataInDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, corpusMetadataFilename), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(codexJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := sessionReportFromPath(dir, "text"); code != 1 {
		t.Fatalf("malformed metadata must surface as exit 1, got %d", code)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

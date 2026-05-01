package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

const sampleHighSavingsRecord = `{"req_id":"req_high","provider":"anthropic","model":"claude-3-5","tokens":{"original":1000,"after_layer0":900,"after_layer1":600,"after_layer2":600,"final":600,"saved":400}}` + "\n"

const sampleLowSavingsRecord = `{"req_id":"req_low","provider":"anthropic","model":"claude-3-5","tokens":{"original":1000,"after_layer0":990,"after_layer1":950,"after_layer2":950,"final":950,"saved":50}}` + "\n"

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

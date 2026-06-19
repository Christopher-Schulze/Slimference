package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSSReferenceInventoryCountsFieldAndRawReferenceSignals(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeJSONLFile(t, path,
		map[string]any{
			"request": map[string]any{
				"previous_response_id": "resp_1",
				"input": []any{
					map[string]any{
						"item_id": "item_1",
						"content": []any{
							map[string]any{"file_id": "file_1", "text": "see local-archive://abc"},
						},
					},
				},
			},
		},
		map[string]any{
			"body": `{"reference_id":"ref_1","encrypted_content":"opaque","attachment_id":"att_1","content_reference":"block_1","text":"slim://archive/old"}`,
		})

	report, err := loadWSSReferenceInventory(path)
	if err != nil {
		t.Fatalf("loadWSSReferenceInventory() error = %v", err)
	}
	if report.Files != 1 || report.Lines != 2 || report.JSONRows != 2 || report.ParseErrors != 0 {
		t.Fatalf("bad totals: %+v", report)
	}
	if got := wssReferenceInventoryTestCount(report.FieldKeys, "previous_response_id"); got != 1 {
		t.Fatalf("previous_response_id field count = %d, want 1", got)
	}
	if got := wssReferenceInventoryTestCount(report.FieldKeys, "file_id"); got != 1 {
		t.Fatalf("file_id field count = %d, want 1", got)
	}
	if got := wssReferenceInventoryTestCount(report.RawMentions, "reference_id"); got != 1 {
		t.Fatalf("reference_id raw count = %d, want 1", got)
	}
	if got := wssReferenceInventoryTestCount(report.ReasoningStateFields, "raw:encrypted_content"); got != 1 {
		t.Fatalf("raw encrypted_content reasoning-state count = %d, want 1", got)
	}
	if got := wssReferenceInventoryTestCount(report.LocalReferenceURIs, "local-archive://"); got != 1 {
		t.Fatalf("local archive count = %d, want 1", got)
	}
	if got := wssReferenceInventoryTestCount(report.LocalReferenceURIs, "slim://archive/"); got != 1 {
		t.Fatalf("slim archive count = %d, want 1", got)
	}
	if report.Verdict != "arbitrary_reference_candidate_observed" {
		t.Fatalf("verdict = %q", report.Verdict)
	}
}

func TestWSSReferenceInventoryNoArbitraryReferenceVerdict(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, map[string]any{
		"previous_response_id": "resp_1",
		"item_id":              "item_1",
		"encrypted_content":    "opaque",
	})

	report, err := loadWSSReferenceInventory(path)
	if err != nil {
		t.Fatalf("loadWSSReferenceInventory() error = %v", err)
	}
	if report.Verdict != "no_arbitrary_backend_reference_observed" {
		data, _ := json.MarshalIndent(report, "", "  ")
		t.Fatalf("bad verdict: %s", data)
	}
	if got := wssReferenceInventoryTestCount(report.ReasoningStateFields, "field:encrypted_content"); got != 1 {
		t.Fatalf("field encrypted_content reasoning-state count = %d, want 1", got)
	}
	if len(report.ArbitraryCandidates) != 0 {
		t.Fatalf("unexpected arbitrary candidates: %+v", report.ArbitraryCandidates)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "Class-D ceiling mass") {
		t.Fatalf("missing reasoning no-go note: %+v", report.Notes)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "product mutation must remain off") {
		t.Fatalf("missing no-go note: %+v", report.Notes)
	}
}

func TestWSSReferenceInventoryRawKeyDoesNotCountSubstrings(t *testing.T) {
	t.Parallel()

	line := `{"previous_response_id":"resp_1","body":"{\"previous_response_id\":\"resp_2\"}"}`
	if got := countWSSReferenceInventoryRawKey(line, "previous_response_id"); got != 2 {
		t.Fatalf("previous_response_id raw count = %d, want 2", got)
	}
	if got := countWSSReferenceInventoryRawKey(line, "response_id"); got != 0 {
		t.Fatalf("response_id substring count = %d, want 0", got)
	}
	if got := countWSSReferenceInventoryRawKey(line, "id"); got != 0 {
		t.Fatalf("id substring count = %d, want 0", got)
	}
}

func TestRunWSSReferenceInventoryJSONAndText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeJSONLFile(t, path, map[string]any{"previous_response_id": "resp_1"})

	var stdout, stderr bytes.Buffer
	if code := runWSSReferenceInventory([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "WSS Reference Inventory") ||
		!strings.Contains(stdout.String(), "Verdict:") ||
		!strings.Contains(stdout.String(), "previous_response_id") {
		t.Fatalf("text output missing expected fields:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSReferenceInventory([]string{path, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("json code=%d stderr=%s", code, stderr.String())
	}
	var report wssReferenceInventoryReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output invalid: %v\n%s", err, stdout.String())
	}
	if report.Path != path || report.JSONRows != 1 {
		t.Fatalf("bad json report: %+v", report)
	}
}

func TestWSSReferenceInventoryDirectoryScanAndParseErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONLFile(t, filepath.Join(nested, "a.jsonl"), map[string]any{"file_id": "file_1"})
	if err := os.WriteFile(filepath.Join(nested, "b.json"), []byte("{bad-json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "skip.txt"), []byte(`{"reference_id":"ignored"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := loadWSSReferenceInventory(dir)
	if err != nil {
		t.Fatalf("loadWSSReferenceInventory() error = %v", err)
	}
	if report.Files != 2 || report.Lines != 2 || report.JSONRows != 1 || report.ParseErrors != 1 {
		t.Fatalf("bad directory totals: %+v", report)
	}
	if got := wssReferenceInventoryTestCount(report.FieldKeys, "file_id"); got != 1 {
		t.Fatalf("file_id count = %d, want 1", got)
	}
	if got := wssReferenceInventoryTestCount(report.RawMentions, "reference_id"); got != 0 {
		t.Fatalf("skip.txt should not be scanned, reference_id raw count=%d", got)
	}
}

func wssReferenceInventoryTestCount(rows []wssReferenceInventoryCount, name string) int {
	for _, row := range rows {
		if row.Name == name {
			return row.Count
		}
	}
	return 0
}

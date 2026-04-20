package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// captureStdout swaps os.Stdout for a pipe and returns the captured bytes
// after fn returns. Lets the report wrappers run through their full
// print-to-stdout path without polluting test output.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), err
}

func TestSessionReport_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return sessionReport(path, "text")
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out == "" {
		t.Fatal("expected some output even on empty input")
	}
}

func TestSessionReport_MissingFileErrors(t *testing.T) {
	if err := sessionReport("/nope/does-not-exist.jsonl", "text"); err == nil {
		t.Fatal("expected error on missing file")
	}
}

func TestSessionReport_TextFormatWithOneRow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	entry := `{"type":"request_processed","timestamp":"2026-04-20T12:00:00Z","provider":"anthropic","input_tokens_orig":1000,"input_tokens_comp":500,"output_tokens":200}
`
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return sessionReport(path, "text") })
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("no output for non-empty file")
	}
}

func TestSessionReport_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	entry := `{"type":"request_processed","timestamp":"2026-04-20T12:00:00Z","provider":"openai","input_tokens_orig":500,"input_tokens_comp":300}
`
	_ = os.WriteFile(path, []byte(entry), 0o644)
	out, err := captureStdout(t, func() error { return sessionReport(path, "json") })
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("no JSON output")
	}
}

func TestDecisionReport_MissingFile(t *testing.T) {
	if err := decisionReport("/nope", "text"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecisionReport_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.jsonl")
	_ = os.WriteFile(path, []byte{}, 0o644)
	_, err := captureStdout(t, func() error { return decisionReport(path, "text") })
	if err != nil {
		t.Fatal(err)
	}
}

func TestFilterReport_MissingDB(t *testing.T) {
	if err := filterReport("/nope/does-not-exist.db", "text"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCombinedReport_MissingInputs(t *testing.T) {
	dir := t.TempDir()
	// Non-existent files.
	if err := combinedReport(
		filepath.Join(dir, "a.jsonl"),
		filepath.Join(dir, "b.jsonl"),
		filepath.Join(dir, "c.db"),
		"text",
	); err == nil {
		t.Fatal("expected error on missing inputs")
	}
}

func TestParseOutputFlag_Variants(t *testing.T) {
	cases := map[string]string{
		"--json": "json",
		"--csv":  "csv",
		"":       "text",
	}
	for flag, want := range cases {
		var args []string
		if flag != "" {
			args = []string{flag}
		}
		got, _, err := parseOutputFlag(args)
		if err != nil {
			t.Errorf("%q: err %v", flag, err)
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", flag, got, want)
		}
	}
}

func TestParseOutputFlag_ConflictingErrors(t *testing.T) {
	_, _, err := parseOutputFlag([]string{"--json", "--csv"})
	if err == nil {
		t.Fatal("expected error on --json --csv combination")
	}
}

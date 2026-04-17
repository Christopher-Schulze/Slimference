package debug

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionFileStats(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte("\n{\"a\":1}\n\n  \n{\"b\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, sz, err := SessionFileStats(path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || sz < 10 {
		t.Fatalf("n=%d sz=%d", n, sz)
	}
	if _, _, err := SessionFileStats(dir); err == nil {
		t.Fatal("expected error for directory")
	}
}

// TestSessionFileStats_nonExistentFile verifies the os.Stat error path.
func TestSessionFileStats_nonExistentFile(t *testing.T) {
	t.Parallel()
	_, _, err := SessionFileStats(filepath.Join(t.TempDir(), "nonexistent.jsonl"))
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

// TestSessionFileStats_openError verifies the os.Open error path via permission denial.
func TestSessionFileStats_openError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "noperms.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	_, _, err := SessionFileStats(path)
	if err == nil {
		t.Fatal("expected error for permission-denied file")
	}
}

// TestSessionFileStats_scanError verifies the scanner.Err() path via a line above the 8 MiB scanner limit.
func TestSessionFileStats_scanError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bigline.jsonl")
	bigLine := make([]byte, 9*1024*1024)
	for i := range bigLine {
		bigLine[i] = 'x'
	}
	bigLine[len(bigLine)-1] = '\n'
	if err := os.WriteFile(path, bigLine, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := SessionFileStats(path)
	if err == nil {
		t.Fatal("expected scanner error for line exceeding buffer")
	}
}

// TestReplaySession_happy verifies that valid RequestSummary JSONL lines are parsed in order.
func TestReplaySession_happy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "replay.jsonl")
	content := `{"req_id":"r1","provider":"anthropic","tokens":{"original":1000,"final":800,"saved":200,"ratio":0.8}}
{"req_id":"r2","provider":"openai","tokens":{"original":2000,"final":1500,"saved":500,"ratio":0.75}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	summaries, err := ReplaySession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if summaries[0].RequestID != "r1" || summaries[1].RequestID != "r2" {
		t.Errorf("order wrong: %v %v", summaries[0].RequestID, summaries[1].RequestID)
	}
	if summaries[0].Tokens.Saved != 200 {
		t.Errorf("r1 saved: want 200, got %d", summaries[0].Tokens.Saved)
	}
}

// TestReplaySession_mixedLines verifies that malformed lines are skipped silently.
func TestReplaySession_mixedLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")
	content := `not-json
{"req_id":"r1","provider":"anthropic"}


{"x":1}
{"req_id":"r2","provider":"openai"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	summaries, err := ReplaySession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	found := map[string]bool{}
	for _, s := range summaries {
		found[s.RequestID] = true
	}
	if !found["r1"] || !found["r2"] {
		t.Errorf("expected r1 and r2 in summaries, got %v", summaries)
	}
}

// TestReplaySession_empty verifies an empty or whitespace-only file returns nil slice.
func TestReplaySession_empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte("\n  \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	summaries, err := ReplaySession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries for empty file, got %d", len(summaries))
	}
}

// TestReplaySession_nonExistentFile verifies os.Open error is returned.
func TestReplaySession_nonExistentFile(t *testing.T) {
	t.Parallel()
	_, err := ReplaySession(filepath.Join(t.TempDir(), "nonexistent.jsonl"))
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

// TestReplaySession_scanError verifies scanner.Err() via a line above the 8 MiB scanner limit.
func TestReplaySession_scanError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bigline.jsonl")
	bigLine := make([]byte, 9*1024*1024)
	for i := range bigLine {
		bigLine[i] = 'x'
	}
	bigLine[len(bigLine)-1] = '\n'
	if err := os.WriteFile(path, bigLine, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReplaySession(path)
	if err == nil {
		t.Fatal("expected scanner error for line exceeding buffer")
	}
}

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLaunchdLogPaths verifies the log path helpers pass their canonical
// arguments through expandHome. The test isolates expandHomeFn locally so
// parallel tests that stub it cannot affect this assertion.
func TestLaunchdLogPaths(t *testing.T) {
	orig := expandHomeFn
	expandHomeFn = func(path string) string { return "/ROOT/" + path }
	t.Cleanup(func() { expandHomeFn = orig })
	if got := LaunchdStdoutLogPath(); got != "/ROOT/~/.slimference/logs/daemon.stdout.log" {
		t.Errorf("stdout log: %s", got)
	}
	if got := LaunchdStderrLogPath(); got != "/ROOT/~/.slimference/logs/daemon.stderr.log" {
		t.Errorf("stderr log: %s", got)
	}
}

// TestReadRecentLogLines_missingFile returns nil,nil when the log file
// does not exist yet.
func TestReadRecentLogLines_missingFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "never-written.log")
	lines, err := ReadRecentLogLines(path, 10, time.Time{})
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if lines != nil {
		t.Fatalf("missing file must return nil lines, got %v", lines)
	}
}

// TestReadRecentLogLines_emptyFile returns nil when the file exists but
// is empty.
func TestReadRecentLogLines_emptyFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.log")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadRecentLogLines(path, 10, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if lines != nil {
		t.Fatalf("empty file must return nil, got %v", lines)
	}
}

// TestReadRecentLogLines_honoursN returns only the last n lines.
func TestReadRecentLogLines_honoursN(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "many.log")
	content := "a\nb\nc\nd\ne\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadRecentLogLines(path, 2, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "d" || lines[1] != "e" {
		t.Fatalf("expected last 2 lines [d e], got %v", lines)
	}
}

// TestReadRecentLogLines_allWhenNZero returns every line when n<=0.
func TestReadRecentLogLines_allWhenNZero(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "all.log")
	if err := os.WriteFile(path, []byte("x\ny\nz"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadRecentLogLines(path, 0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected all 3 lines, got %v", lines)
	}
}

// TestReadRecentLogLines_sinceFilter drops lines older than cutoff.
func TestReadRecentLogLines_sinceFilter(t *testing.T) {
	t.Parallel()
	old := `{"time":"2026-04-01T10:00:00Z","msg":"old"}`
	fresh := `{"time":"2026-04-18T12:00:00Z","msg":"fresh"}`
	path := filepath.Join(t.TempDir(), "t.log")
	if err := os.WriteFile(path, []byte(old+"\n"+fresh+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cutoff, _ := time.Parse(time.RFC3339, "2026-04-18T00:00:00Z")
	lines, err := ReadRecentLogLines(path, 0, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "fresh") {
		t.Fatalf("expected only fresh line, got %v", lines)
	}
}

// TestReadRecentLogLines_readError propagates OS errors other than
// fs.ErrNotExist via the stubbed reader.
func TestReadRecentLogLines_readError(t *testing.T) {
	orig := osReadFile
	t.Cleanup(func() { osReadFile = orig })
	osReadFile = func(path string) ([]byte, error) {
		return nil, errors.New("boom")
	}
	_, err := ReadRecentLogLines("whatever", 10, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error to surface, got %v", err)
	}
}

// TestExtractLogLineTime_slogJSON parses slog's "time" field.
func TestExtractLogLineTime_slogJSON(t *testing.T) {
	t.Parallel()
	line := `{"time":"2026-04-18T12:34:56.789Z","level":"INFO","msg":"hi"}`
	ts, ok := extractLogLineTime(line)
	if !ok {
		t.Fatal("expected slog JSON parse")
	}
	if ts.Year() != 2026 || ts.Month() != 4 || ts.Day() != 18 {
		t.Fatalf("parsed wrong date: %v", ts)
	}
}

// TestExtractLogLineTime_RFC3339Prefix parses a leading RFC3339 timestamp.
func TestExtractLogLineTime_RFC3339Prefix(t *testing.T) {
	t.Parallel()
	line := "2026-04-18T12:00:00Z hello world"
	ts, ok := extractLogLineTime(line)
	if !ok {
		t.Fatal("expected prefix parse")
	}
	if ts.Hour() != 12 {
		t.Fatalf("parsed wrong hour: %v", ts)
	}
}

// TestExtractLogLineTime_RFC3339NanoPrefix parses a leading RFC3339Nano prefix.
func TestExtractLogLineTime_RFC3339NanoPrefix(t *testing.T) {
	t.Parallel()
	line := "2026-04-18T12:00:00.123456789Z extra"
	ts, ok := extractLogLineTime(line)
	if !ok {
		t.Fatal("expected RFC3339Nano parse")
	}
	if ts.Nanosecond() == 0 {
		t.Fatal("expected nanos to be preserved")
	}
}

// TestExtractLogLineTime_noTimestamp returns false for plain lines.
func TestExtractLogLineTime_noTimestamp(t *testing.T) {
	t.Parallel()
	if _, ok := extractLogLineTime("just a line"); ok {
		t.Fatal("plain line must not parse as a timestamp")
	}
	if _, ok := extractLogLineTime("short"); ok {
		t.Fatal("short line must not parse")
	}
}

// TestExtractLogLineTime_brokenSlogTime does not crash or claim success on
// a malformed slog time field.
func TestExtractLogLineTime_brokenSlogTime(t *testing.T) {
	t.Parallel()
	if _, ok := extractLogLineTime(`{"time":"not-a-time"}`); ok {
		t.Fatal("malformed slog time must not parse")
	}
	if _, ok := extractLogLineTime(`{"time":"`); ok {
		t.Fatal("unterminated slog time must not parse")
	}
}

// TestFilterSinceLogLinesKeepsUndatedLines ensures lines without a known
// timestamp pass through unchanged even when a cutoff is active.
func TestFilterSinceLogLinesKeepsUndatedLines(t *testing.T) {
	t.Parallel()
	cutoff, _ := time.Parse(time.RFC3339, "2026-04-18T00:00:00Z")
	in := []string{
		"undated message",
		`{"time":"2026-04-01T10:00:00Z","msg":"old"}`,
		`{"time":"2026-04-18T12:00:00Z","msg":"fresh"}`,
	}
	out := filterSinceLogLines(in, cutoff)
	if len(out) != 2 {
		t.Fatalf("expected undated + fresh, got %v", out)
	}
	if out[0] != "undated message" {
		t.Fatalf("undated line must be first: %v", out)
	}
}

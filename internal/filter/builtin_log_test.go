package filter

import (
	"strings"
	"testing"
)

func TestTryCompactLogOutput_shapeDetection(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("2024-01-01T00:00:01Z DEBUG internal state dump key=val\n")
	}
	sb.WriteString("2024-01-01T00:00:02Z ERROR connection refused\n")
	input := sb.String()
	out, ok := TryCompactLogOutput([]string{"custom-logger", "run"}, []byte(input))
	if !ok {
		t.Fatal("shape detection should trigger log compaction")
	}
	s := string(out)
	if !strings.Contains(s, "ERROR") {
		t.Errorf("ERROR lines should be retained")
	}
	if len(s) >= len(input) {
		t.Errorf("output should be shorter: %d vs %d", len(s), len(input))
	}
}

func TestTryCompactLogOutput_noMatch(t *testing.T) {
	t.Parallel()
	input := "some random output\nnot a log\n"
	out, ok := TryCompactLogOutput([]string{"python", "script.py"}, []byte(input))
	if ok {
		t.Fatal("random output should not match")
	}
	if string(out) != input {
		t.Fatal("should pass through unchanged")
	}
}

func TestTryCompactLogOutput_emptyWithArgv(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactLogOutput([]string{"tail", "-f", "app.log"}, nil)
	if ok {
		t.Fatal("empty stdout should not compact")
	}
	if out != nil {
		t.Fatalf("expected nil, got %q", out)
	}
}

func TestTryCompactLogDedup_delegatesToLogOutput(t *testing.T) {
	t.Parallel()
	in := "same\nsame\nsame\nok\n"
	out, ok := TryCompactLogDedup([]string{"docker", "logs", "x"}, []byte(in))
	if !ok {
		t.Fatal("expected dedup")
	}
	if string(out) != "same [×3]\nok\n" {
		t.Fatalf("got %q", out)
	}
}

func TestCollapseConsecutiveDuplicateLines(t *testing.T) {
	t.Parallel()
	if collapseConsecutiveDuplicateLines("a\na\n") != "a [×2]\n" {
		t.Fatal(collapseConsecutiveDuplicateLines("a\na\n"))
	}
}

func TestTryCompactLogDedup_noDedup(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactLogDedup([]string{"docker", "logs", "x"}, []byte("a\nb\nc\n")); ok {
		t.Fatal("unique lines: should not dedup")
	}
	if isKubectlLogsArgv([]string{"kubectl"}) {
		t.Fatal("kubectl: len<2 should return false")
	}
	out, ok := TryCompactLogDedup([]string{"kubectl", "logs", "pod"}, []byte("line\nline\nline\n"))
	if !ok || string(out) != "line [×3]\n" {
		t.Fatalf("kubectl logs dedup: ok=%v %q", ok, out)
	}
}

func TestCollapseConsecutiveDuplicateLines_empty(t *testing.T) {
	t.Parallel()
	// empty body with trailing newline
	if got := collapseConsecutiveDuplicateLines("\n"); got != "\n" {
		t.Fatalf("trailing NL empty body: got %q", got)
	}
	// empty body without trailing newline
	if got := collapseConsecutiveDuplicateLines(""); got != "" {
		t.Fatalf("no NL empty body: got %q", got)
	}
}

func TestTryCompactLogDedup_debugFiltering(t *testing.T) {
	t.Parallel()
	// Large docker log with many DEBUG lines interleaved with INFO/ERROR lines.
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("2024-01-01T00:00:01Z INFO request processed id=")
		sb.WriteString(strings.Repeat("x", 10))
		sb.WriteByte('\n')
		sb.WriteString("2024-01-01T00:00:01Z DEBUG internal state dump key=val\n")
		sb.WriteString("2024-01-01T00:00:01Z TRACE entering handler\n")
	}
	sb.WriteString("2024-01-01T00:00:02Z ERROR connection refused\n")
	input := sb.String()
	out, ok := TryCompactLogDedup([]string{"docker", "logs", "app"}, []byte(input))
	if !ok {
		t.Fatalf("expected log filtering, got pass-through (input %d bytes)", len(input))
	}
	s := string(out)
	if strings.Contains(strings.ToLower(s), "[debug]") || strings.Contains(strings.ToLower(s), " debug ") {
		t.Errorf("DEBUG lines should be stripped, got: %s", s[:min(len(s), 200)])
	}
	if !strings.Contains(s, "ERROR") {
		t.Errorf("ERROR lines should be retained")
	}
	if len(s) >= len(input) {
		t.Errorf("filtered output should be shorter: %d vs %d", len(s), len(input))
	}
}

// TestFilterLogOutput_AllFiltered covers the "kept == 0 → return original" branch.
func TestFilterLogOutput_AllFiltered(t *testing.T) {
	t.Parallel()
	// All lines are DEBUG → kept is empty → return original
	input := "[debug] step 1\n[debug] step 2\n[debug] step 3\n"
	got := filterLogOutput(input)
	if got != input {
		t.Errorf("all-debug input: want original, got %q", got)
	}
}

// TestFilterLogOutput_TruncateLong covers the len(kept) > logMaxLines truncation branch.
func TestFilterLogOutput_TruncateLong(t *testing.T) {
	t.Parallel()
	// Generate >100 INFO lines (not debug/trace) to trigger truncation
	var sb strings.Builder
	for i := 0; i < 120; i++ {
		sb.WriteString("2024-01-01 INFO request processed\n")
	}
	input := sb.String()
	got := filterLogOutput(input)
	if !strings.Contains(got, "more log line(s)") {
		t.Errorf("long log: want truncation notice, got %q", got[:min(len(got), 200)])
	}
	if len(got) >= len(input) {
		t.Errorf("truncated output should be shorter: %d vs %d", len(got), len(input))
	}
}

// TestFilterLogOutput_KeepsErrorPastHeadBudget proves the drawdown fix: an
// error line sitting well past the positional head budget must survive
// truncation instead of being dropped by a blunt head-N cut.
func TestFilterLogOutput_KeepsErrorPastHeadBudget(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 130; i++ {
		sb.WriteString("2024-01-01 INFO request processed\n")
	}
	sb.WriteString("2024-01-01 ERROR fatal: database connection refused\n")
	got := filterLogOutput(sb.String())
	if !strings.Contains(got, "database connection refused") {
		t.Fatalf("error line past head budget was dropped: %q", got[:min(len(got), 240)])
	}
	if !strings.Contains(got, "more log line(s)") {
		t.Fatalf("expected truncation notice, got %q", got[:min(len(got), 240)])
	}
	if len(got) >= len(sb.String()) {
		t.Fatalf("expected truncation to shorten output")
	}
}

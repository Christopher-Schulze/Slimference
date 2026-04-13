package filter

import (
	"strings"
	"testing"
)

func TestTruncateStdoutWithHint(t *testing.T) {
	t.Parallel()
	short := []byte("hi")
	if got := TruncateStdoutWithHint(short, 100); string(got) != "hi" {
		t.Fatalf("%q", got)
	}
	long := []byte(strings.Repeat("a", 50))
	got := TruncateStdoutWithHint(long, 10)
	if !strings.HasPrefix(string(got), strings.Repeat("a", 10)) {
		t.Fatalf("prefix: %q", got)
	}
	if !strings.Contains(string(got), "truncated") {
		t.Fatalf("hint: %q", got)
	}
	if string(TruncateStdoutWithHint(long, 0)) != string(long) {
		t.Fatal("0 should disable")
	}
}

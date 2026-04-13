package compression

import (
	"strings"
	"testing"
)

func TestStripANSICodes_Basic(t *testing.T) {
	t.Parallel()
	s := "\x1b[31mred\x1b[0m plain"
	got := StripANSICodes(s)
	if strings.Contains(got, "\x1b") {
		t.Fatalf("expected escapes removed, got %q", got)
	}
	if !strings.Contains(got, "plain") {
		t.Fatalf("expected plain text kept: %q", got)
	}
}

func TestStripANSICodes_CarriageReturn(t *testing.T) {
	t.Parallel()
	s := "line1\rline2"
	got := StripANSICodes(s)
	if strings.ContainsRune(got, '\r') {
		t.Fatalf("expected \\r removed: %q", got)
	}
}

func TestStripANSICodes_Empty(t *testing.T) {
	t.Parallel()
	if got := StripANSICodes(""); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

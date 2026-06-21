package filter

import (
	"fmt"
	"strings"
	"testing"
)

// TestGroupSearchResultsRobustToNoiseLines proves a single colon-less line (a
// header like rg's "Total output lines: N" or a line cut off by Codex output
// truncation) no longer defeats grouping - the real Codex failure mode. It also
// proves the guard: an output that is mostly non-grep noise stays literal.
func TestGroupSearchResultsRobustToNoiseLines(t *testing.T) {
	var b strings.Builder
	b.WriteString("Total output lines: 1786\n\n") // rg header line
	files := []string{"internal/proxy/wsmitm_phasef.go", "internal/filter/builtin_search.go", "cmd/slimference/main.go"}
	for _, f := range files {
		for ln := 1; ln <= 12; ln++ {
			fmt.Fprintf(&b, "%s:%d:\tsome matching content on this line %d\n", f, ln*7, ln)
		}
	}
	b.WriteString("internal/proxy/wsmitm_phasef.g") // truncated final line, no colon
	in := b.Len()
	out, ok := groupSearchResults([]byte(b.String()), "rg")
	if !ok || len(out) >= in {
		t.Fatalf("noisy-but-valid grep output must still group: ok=%v in=%d out=%d", ok, in, len(out))
	}
	s := string(out)
	if !strings.Contains(s, "[rg]") || !strings.Contains(s, "match(es)") {
		t.Fatalf("grouped output missing summary: %q", s[:min(len(s), 120)])
	}
	if strings.Contains(s, "Total output lines") {
		t.Fatalf("grouped output must not treat Codex envelope metadata as a search file: %q", s)
	}

	// Noise-dominated output (mostly colon-less) must stay literal, not be
	// summarized into a wrong match count.
	var noise strings.Builder
	for i := range 20 {
		fmt.Fprintf(&noise, "just some prose line number %d with no structure\n", i)
	}
	noise.WriteString("one/real.go:5:match\n")
	if _, ok := groupSearchResults([]byte(noise.String()), "rg"); ok {
		t.Fatalf("noise-dominated output must stay literal (guard), got compaction")
	}
}

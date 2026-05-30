package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactGlabList(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactGlabList([]string{"glab", "mr", "list"}, []byte(""))
	if !ok || string(out) != "[glab mr list] empty\n" {
		t.Fatalf("mr list: ok=%v %q", ok, out)
	}
	iss, ok := TryCompactGlabList([]string{"glab", "issue", "list"}, []byte("\n"))
	if !ok || string(iss) != "[glab issue list] empty\n" {
		t.Fatalf("issue list: %q", iss)
	}
	snip, ok := TryCompactGlabList([]string{"glab", "snippet", "list"}, []byte(""))
	if !ok || string(snip) != "[glab snippet list] empty\n" {
		t.Fatalf("snippet list: %q", snip)
	}
	br, ok := TryCompactGlabList([]string{"glab", "branch", "list"}, []byte(""))
	if !ok || string(br) != "[glab branch list] empty\n" {
		t.Fatalf("branch list: %q", br)
	}
	gr, ok := TryCompactGlabList([]string{"glab", "repo", "list"}, []byte(""))
	if !ok || string(gr) != "[glab repo list] empty\n" {
		t.Fatalf("repo list: %q", gr)
	}
	sch, ok := TryCompactGlabList([]string{"glab", "schedule", "list"}, []byte(""))
	if !ok || string(sch) != "[glab schedule list] empty\n" {
		t.Fatalf("schedule list: %q", sch)
	}
	ci, ok := TryCompactGlabList([]string{"glab", "ci", "list"}, []byte(""))
	if !ok || string(ci) != "[glab ci list] empty\n" {
		t.Fatalf("ci list: %q", ci)
	}
	inc, ok := TryCompactGlabList([]string{"glab", "incident", "list"}, []byte("\n"))
	if !ok || string(inc) != "[glab incident list] empty\n" {
		t.Fatalf("incident list: %q", inc)
	}
	reg, ok := TryCompactGlabList([]string{"glab", "registry", "list"}, []byte(""))
	if !ok || string(reg) != "[glab registry list] empty\n" {
		t.Fatalf("registry list: %q", reg)
	}
	job, ok := TryCompactGlabList([]string{"glab", "job", "list"}, []byte("\n"))
	if !ok || string(job) != "[glab job list] empty\n" {
		t.Fatalf("job list: %q", job)
	}
	runner, ok := TryCompactGlabList([]string{"glab", "runner", "list"}, []byte(""))
	if !ok || string(runner) != "[glab runner list] empty\n" {
		t.Fatalf("runner list: %q", runner)
	}
	tok, ok := TryCompactGlabList([]string{"glab", "token", "list"}, []byte("\n"))
	if !ok || string(tok) != "[glab token list] empty\n" {
		t.Fatalf("token list: %q", tok)
	}
	clu, ok := TryCompactGlabList([]string{"glab", "cluster", "list"}, []byte(""))
	if !ok || string(clu) != "[glab cluster list] empty\n" {
		t.Fatalf("cluster list: %q", clu)
	}
	if _, ok := TryCompactGlabList([]string{"glab", "mr", "view", "1"}, []byte("")); ok {
		t.Fatal("not list")
	}
	if _, ok := TryCompactGlabList([]string{"gh", "mr", "list"}, []byte("")); ok {
		t.Fatal("not glab")
	}
}

func TestTryCompactGlabList_guards(t *testing.T) {
	t.Parallel()
	// unknown subcommand
	if _, ok := TryCompactGlabList([]string{"glab", "unknown-sub", "list"}, []byte("")); ok {
		t.Fatal("unknown subcommand should return false")
	}
	// non-empty stdout with few rows (<= glabListMaxRows=15) → pass-through
	if _, ok := TryCompactGlabList([]string{"glab", "mr", "list"}, []byte("!1 My MR\n")); ok {
		t.Fatal("short non-empty stdout should return false")
	}
}

func TestTryCompactGlabList_manyRows(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 25; i++ {
		sb.WriteString(fmt.Sprintf("!%d  MR title %d  feature/branch-%d  OPEN  2024-01-01\n", i, i, i))
	}
	out, ok := TryCompactGlabList([]string{"glab", "mr", "list"}, []byte(sb.String()))
	if !ok {
		t.Fatalf("expected compact for 25 rows, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "[glab mr list] 25 items") {
		t.Errorf("want item count header, got: %q", s)
	}
	if !strings.Contains(s, "+10 more") {
		t.Errorf("want +10 more suffix, got: %q", s)
	}
}

func TestTryCompactGlabList_attentionRowPastCapSurvives(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 25; i++ {
		status := "success"
		if i == 22 {
			status = "failed"
		}
		sb.WriteString(fmt.Sprintf("pipeline-%02d  branch-%02d  %s  2024-01-01\n", i, i, status))
	}
	input := sb.String()
	out, ok := TryCompactGlabList([]string{"glab", "pipeline", "list"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact for 25 rows, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "attention row") || !strings.Contains(s, "pipeline-22") || !strings.Contains(s, "failed") {
		t.Fatalf("late failed row was dropped: %q", s)
	}
	if strings.Contains(s, "pipeline-16") {
		t.Fatalf("benign row past cap should not displace late failure: %q", s)
	}
	if len(s) >= len(input) {
		t.Fatalf("compact should be shorter: %d vs %d", len(s), len(input))
	}
}

// TestTryCompactGlabList_compactNotShorter covers the len(out) >= len(s) guard (line 52-54):
// 16 very short rows where header + preview + suffix exceeds original.
func TestTryCompactGlabList_compactNotShorter(t *testing.T) {
	t.Parallel()
	// 16 rows of "a\n" = 32 chars; compact "[glab mr list] 16 items\n"+"a\n"*15+"... +1 more\n" ≈ 66+ chars > 32.
	var sb strings.Builder
	for i := 0; i < 16; i++ {
		sb.WriteString("a\n")
	}
	_, ok := TryCompactGlabList([]string{"glab", "mr", "list"}, []byte(sb.String()))
	if ok {
		t.Error("compact >= original for very short rows: want false, got true")
	}
}

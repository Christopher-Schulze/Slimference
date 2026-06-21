package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactGhList(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactGhList([]string{"gh", "pr", "list"}, []byte(""))
	if !ok || string(out) != "[gh pr list] empty\n" {
		t.Fatalf("pr list: ok=%v %q", ok, out)
	}
	out2, ok := TryCompactGhList([]string{"/opt/homebrew/bin/gh", "issue", "list"}, []byte("\n"))
	if !ok || string(out2) != "[gh issue list] empty\n" {
		t.Fatalf("issue: %q", out2)
	}
	outR, ok := TryCompactGhList([]string{"gh", "release", "list"}, []byte(""))
	if !ok || string(outR) != "[gh release list] empty\n" {
		t.Fatalf("release list: %q", outR)
	}
	outW, ok := TryCompactGhList([]string{"gh", "workflow", "list"}, []byte(""))
	if !ok || string(outW) != "[gh workflow list] empty\n" {
		t.Fatalf("workflow list: %q", outW)
	}
	outA, ok := TryCompactGhList([]string{"gh", "alias", "list"}, []byte(""))
	if !ok || string(outA) != "[gh alias list] empty\n" {
		t.Fatalf("alias list: %q", outA)
	}
	outG, ok := TryCompactGhList([]string{"gh", "gist", "list"}, []byte(""))
	if !ok || string(outG) != "[gh gist list] empty\n" {
		t.Fatalf("gist list: %q", outG)
	}
	outL, ok := TryCompactGhList([]string{"gh", "label", "list"}, []byte(""))
	if !ok || string(outL) != "[gh label list] empty\n" {
		t.Fatalf("label list: %q", outL)
	}
	outC, ok := TryCompactGhList([]string{"gh", "cache", "list"}, []byte(""))
	if !ok || string(outC) != "[gh cache list] empty\n" {
		t.Fatalf("cache list: %q", outC)
	}
	outS, ok := TryCompactGhList([]string{"gh", "secret", "list"}, []byte(""))
	if !ok || string(outS) != "[gh secret list] empty\n" {
		t.Fatalf("secret list: %q", outS)
	}
	outRepo, ok := TryCompactGhList([]string{"gh", "repo", "list"}, []byte(""))
	if !ok || string(outRepo) != "[gh repo list] empty\n" {
		t.Fatalf("repo list: %q", outRepo)
	}
	outProj, ok := TryCompactGhList([]string{"gh", "project", "list"}, []byte(""))
	if !ok || string(outProj) != "[gh project list] empty\n" {
		t.Fatalf("project list: %q", outProj)
	}
	outGPG, ok := TryCompactGhList([]string{"gh", "gpg-key", "list"}, []byte(""))
	if !ok || string(outGPG) != "[gh gpg-key list] empty\n" {
		t.Fatalf("gpg-key list: %q", outGPG)
	}
	outSSH, ok := TryCompactGhList([]string{"gh", "ssh-key", "list"}, []byte(""))
	if !ok || string(outSSH) != "[gh ssh-key list] empty\n" {
		t.Fatalf("ssh-key list: %q", outSSH)
	}
	outOrg, ok := TryCompactGhList([]string{"gh", "org", "list"}, []byte(""))
	if !ok || string(outOrg) != "[gh org list] empty\n" {
		t.Fatalf("org list: %q", outOrg)
	}
	outMil, ok := TryCompactGhList([]string{"gh", "milestone", "list"}, []byte(""))
	if !ok || string(outMil) != "[gh milestone list] empty\n" {
		t.Fatalf("milestone list: %q", outMil)
	}
	outAuth, ok := TryCompactGhList([]string{"gh", "auth", "list"}, []byte(""))
	if !ok || string(outAuth) != "[gh auth list] empty\n" {
		t.Fatalf("auth list: %q", outAuth)
	}
	outCfg, ok := TryCompactGhList([]string{"gh", "config", "list"}, []byte(""))
	if !ok || string(outCfg) != "[gh config list] empty\n" {
		t.Fatalf("config list: %q", outCfg)
	}
	outTeam, ok := TryCompactGhList([]string{"gh", "team", "list", "--org", "acme"}, []byte(""))
	if !ok || string(outTeam) != "[gh team list] empty\n" {
		t.Fatalf("team list: %q", outTeam)
	}
	outSp, ok := TryCompactGhList([]string{"gh", "sponsor", "list"}, []byte(""))
	if !ok || string(outSp) != "[gh sponsor list] empty\n" {
		t.Fatalf("sponsor list: %q", outSp)
	}
	outAT, ok := TryCompactGhList([]string{"gh", "agent-task", "list"}, []byte(""))
	if !ok || string(outAT) != "[gh agent-task list] empty\n" {
		t.Fatalf("agent-task list: %q", outAT)
	}
	if _, ok := TryCompactGhList([]string{"gh", "pr", "view", "1"}, []byte("")); ok {
		t.Fatal("pr view not list")
	}
	if _, ok := TryCompactGhList([]string{"git", "pr", "list"}, []byte("")); ok {
		t.Fatal("not gh")
	}
}

func TestTryCompactGhList_guards(t *testing.T) {
	t.Parallel()
	// unknown subcommand
	if _, ok := TryCompactGhList([]string{"gh", "unknown-sub", "list"}, []byte("")); ok {
		t.Fatal("unknown subcommand should return false")
	}
	// short non-empty stdout (≤15 rows) → pass through
	if _, ok := TryCompactGhList([]string{"gh", "pr", "list"}, []byte("1\tFix bug\tfix/bug\t2024-01-01\tOPEN\n")); ok {
		t.Fatal("short non-empty stdout should pass through")
	}
}

func TestTryCompactGhList_manyRows(t *testing.T) {
	t.Parallel()
	// Build a large pr list output (>15 rows)
	var sb strings.Builder
	for i := 1; i <= 25; i++ {
		sb.WriteString(fmt.Sprintf("%d\tFix issue #%d\tfix/issue-%d\t2024-01-01\tOPEN\n", i, i, i))
	}
	input := sb.String()
	if _, ok := TryCompactGhList([]string{"gh", "pr", "list"}, []byte(input)); ok {
		t.Fatalf("healthy non-empty gh lists must pass through")
	}
}

func TestTryCompactGhList_attentionRowPastCapSurvives(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 25; i++ {
		state := "SUCCESS"
		if i == 23 {
			state = "FAILURE"
		}
		sb.WriteString(fmt.Sprintf("%d\tci run %d\t%s\t2024-01-01\n", i, i, state))
	}
	input := sb.String()
	out, ok := TryCompactGhList([]string{"gh", "run", "list"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact for 25 rows, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "attention row") || !strings.Contains(s, "ci run 23") || !strings.Contains(s, "FAILURE") {
		t.Fatalf("late failed row was dropped: %q", s)
	}
	if strings.Contains(s, "ci run 16\tSUCCESS") {
		t.Fatalf("benign row past cap should not displace late failure: %q", s)
	}
	if len(s) >= len(input) {
		t.Fatalf("compact should be shorter: %d vs %d", len(s), len(input))
	}
}

// TestTryCompactGhList_compactNotShorter covers the len(out) >= len(s) guard (line 55-57):
// 16 very short one-char rows where header + preview + suffix exceeds original.
func TestTryCompactGhList_compactNotShorter(t *testing.T) {
	t.Parallel()
	// 16 rows of "a\n" = 32 chars; compact "[gh pr list] 16 items\n"+"a\n"*15+"... +1 more\n" ≈ 64 chars > 32.
	var sb strings.Builder
	for range 16 {
		sb.WriteString("a\n")
	}
	_, ok := TryCompactGhList([]string{"gh", "pr", "list"}, []byte(sb.String()))
	if ok {
		t.Error("compact >= original for very short rows: want false, got true")
	}
}

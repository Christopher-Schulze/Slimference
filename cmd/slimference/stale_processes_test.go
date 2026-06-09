package main

import (
	"strings"
	"testing"
)

func TestParseStaleSlimferenceProcesses(t *testing.T) {
	input := `
 111 Ss   /Users/example/.local/bin/slimference daemon
 222 U    /Users/example/.local/bin/slimference stop
 333 UE   /Users/example/.local/bin/slimference.dyld-stuck-20260520T003339 version
 444 S    rg slimference
`
	got := parseStaleSlimferenceProcesses(input, 999)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(got), got)
	}
	if got[0].PID != 222 || got[1].PID != 333 {
		t.Fatalf("unexpected pids: %+v", got)
	}
	notice := formatStaleSlimferenceProcessNotice(got)
	for _, needle := range []string{"2 old stuck", "222(U)", "333(UE)", "reboot clears"} {
		if !strings.Contains(notice, needle) {
			t.Fatalf("notice missing %q: %q", needle, notice)
		}
	}
}

func TestParseStaleSlimferenceProcessesIgnoresSelfAndHealthy(t *testing.T) {
	input := `
	 100 U    /Users/example/.local/bin/slimference status
	 101 Ss   /Users/example/.local/bin/slimference daemon
	 102 S    /bin/bash -lc echo slimference
	 103 U+   /Users/example/.npm-global/lib/node_modules/@openai/codex/bin/codex -c model_provider=slimference-codex
	`
	got := parseStaleSlimferenceProcesses(input, 100)
	if len(got) != 0 {
		t.Fatalf("expected none, got %+v", got)
	}
}

func TestIsSlimferenceProcessArgsRequiresExecutable(t *testing.T) {
	if !isSlimferenceProcessArgs("/Users/example/.local/bin/slimference daemon") {
		t.Fatal("slimference executable should match")
	}
	if !isSlimferenceProcessArgs("/tmp/slimference.dyld-stuck-20260520 version") {
		t.Fatal("slimference dyld-stuck executable should match")
	}
	if isSlimferenceProcessArgs("/Users/example/.npm-global/bin/codex -c model_provider=slimference-codex") {
		t.Fatal("codex provider args must not count as a Slimference process")
	}
}

func TestStaleSlimferenceProcessNoticeIgnoringPID(t *testing.T) {
	prev := psSlimferenceProcessesFn
	psSlimferenceProcessesFn = func() ([]staleSlimferenceProcess, error) {
		return []staleSlimferenceProcess{
			{PID: 101, Stat: "U", Args: "/Users/example/.local/bin/slimference daemon"},
			{PID: 202, Stat: "UE", Args: "/Users/example/.local/bin/slimference stop"},
		}, nil
	}
	t.Cleanup(func() { psSlimferenceProcessesFn = prev })

	notice := staleSlimferenceProcessNoticeIgnoringPID(101)
	if strings.Contains(notice, "101(U)") || !strings.Contains(notice, "202(UE)") {
		t.Fatalf("notice should ignore current daemon pid only: %q", notice)
	}
}

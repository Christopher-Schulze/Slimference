package main

import (
	"strings"
	"testing"
)

func TestParseStaleSlimferenceProcesses(t *testing.T) {
	input := `
 111 Ss   /Users/christopher/.local/bin/slimference daemon
 222 U    /Users/christopher/.local/bin/slimference stop
 333 UE   /Users/christopher/.local/bin/slimference.dyld-stuck-20260520T003339 version
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
 100 U    /Users/christopher/.local/bin/slimference status
 101 Ss   /Users/christopher/.local/bin/slimference daemon
 102 S    /bin/bash -lc echo slimference
`
	got := parseStaleSlimferenceProcesses(input, 100)
	if len(got) != 0 {
		t.Fatalf("expected none, got %+v", got)
	}
}

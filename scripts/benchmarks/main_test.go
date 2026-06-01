package main

import "testing"

func TestNormalizeCLIArgs_DropsGoRunSeparator(t *testing.T) {
	t.Parallel()
	got := normalizeCLIArgs([]string{"--", "-benchtime=100ms", "-pkg=proxy"})
	want := []string{"-benchtime=100ms", "-pkg=proxy"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d args=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg %d=%q want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeCLIArgs_LeavesSubcommand(t *testing.T) {
	t.Parallel()
	got := normalizeCLIArgs([]string{"benchmark-corpus", "tests/fixtures/live_corpus"})
	if len(got) != 2 || got[0] != "benchmark-corpus" || got[1] != "tests/fixtures/live_corpus" {
		t.Fatalf("unexpected args: %v", got)
	}
}

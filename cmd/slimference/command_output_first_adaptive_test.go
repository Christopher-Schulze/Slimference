package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestCommandOutputFirstAdaptiveFullPassFloorThenDemote drives the full
// closed loop: a class is compacted (false = keep compacting) until its measured
// re-fetch rate crosses the per-class break-even (1 - compactedRatio), after
// which it demotes to full-pass (true). compactedRatio 0.34 -> break-even 0.66.
func TestCommandOutputFirstAdaptiveFullPassFloorThenDemote(t *testing.T) {
	prevHome := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = prevHome })
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Setenv(commandOutputFirstSessionEnv, "adaptive-test-session")

	const ratio = 0.34 // break-even re-fetch rate = 0.66

	// Phase 1: 10 distinct invocations of class "rg" -> all fresh applies, never
	// re-fetched, so the control keeps compacting (returns false).
	for i := 0; i < 10; i++ {
		args := []string{"-n", fmt.Sprintf("pattern%d", i), "internal/"}
		if commandOutputFirstAdaptiveFullPass("rg", args, ratio) {
			t.Fatalf("phase1 invocation %d: fresh apply must keep compacting (false)", i)
		}
	}

	// Phase 2: re-fetch the same 10 identities. Each repeat is a re-fetch; the
	// decision uses the PRIOR rate. applied=10, refetch climbs 0..9; the rate
	// crosses 0.66 at prior refetch=7 (10/7=0.70), so the 8th repeat demotes.
	var results []bool
	for i := 0; i < 10; i++ {
		args := []string{"-n", fmt.Sprintf("pattern%d", i), "internal/"}
		results = append(results, commandOutputFirstAdaptiveFullPass("rg", args, ratio))
	}
	// prior 10/6=0.60 -> still compacting
	if results[6] {
		t.Fatalf("repeat #7 (rate 0.60 < 0.66) must keep compacting, got full-pass; results=%v", results)
	}
	// prior 10/7=0.70 >= 0.66 -> demote
	if !results[7] {
		t.Fatalf("repeat #8 (rate 0.70 >= 0.66) must demote to full-pass; results=%v", results)
	}
	if !results[9] {
		t.Fatalf("once demoted the class must stay full-passed; results=%v", results)
	}

	// State persisted to the per-session sidecar.
	sidecar := filepath.Join(home, ".slimference", "analytics", "command_output_first_refetch_adaptive-test-session.json")
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("per-session re-fetch state not persisted: %v", err)
	}
}

// TestCommandOutputFirstAdaptiveFullPassNoSession proves the fail-safe floor:
// with no scoped session context the control never fires and writes no state.
func TestCommandOutputFirstAdaptiveFullPassNoSession(t *testing.T) {
	prevHome := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = prevHome })
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Setenv(commandOutputFirstSessionEnv, "")

	for i := 0; i < 20; i++ {
		if commandOutputFirstAdaptiveFullPass("rg", []string{"x"}, 0.01) {
			t.Fatalf("no session must always keep fixed behavior (false)")
		}
	}
	analytics := filepath.Join(home, ".slimference", "analytics")
	if entries, err := os.ReadDir(analytics); err == nil && len(entries) > 0 {
		t.Fatalf("no session must not write re-fetch state, found %d entries", len(entries))
	}
}

// TestCommandOutputFirstAdaptiveDistinctClassesIndependent proves a re-fetch-heavy
// class does not demote an unrelated, never-re-fetched class.
func TestCommandOutputFirstAdaptiveDistinctClassesIndependent(t *testing.T) {
	prevHome := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = prevHome })
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Setenv(commandOutputFirstSessionEnv, "adaptive-multi-class")

	// Drive "rg" to demotion.
	for i := 0; i < 10; i++ {
		commandOutputFirstAdaptiveFullPass("rg", []string{fmt.Sprintf("p%d", i)}, 0.34)
	}
	for i := 0; i < 10; i++ {
		commandOutputFirstAdaptiveFullPass("rg", []string{fmt.Sprintf("p%d", i)}, 0.34)
	}
	// "go" is freshly applied many times, never re-fetched -> must keep compacting.
	for i := 0; i < 12; i++ {
		if commandOutputFirstAdaptiveFullPass("go", []string{"test", fmt.Sprintf("./pkg%d", i)}, 0.34) {
			t.Fatalf("never-re-fetched class 'go' must keep compacting; demoted at %d", i)
		}
	}
}

func TestCommandOutputFirstCompactionRatio(t *testing.T) {
	if got := commandOutputFirstCompactionRatio(nil, []byte("x")); got != 1.0 {
		t.Fatalf("empty raw must return 1.0, got %v", got)
	}
	if got := commandOutputFirstCompactionRatio([]byte("aaaaaaaaaa"), []byte("aaa")); got != 0.3 {
		t.Fatalf("ratio 3/10 expected 0.3, got %v", got)
	}
}

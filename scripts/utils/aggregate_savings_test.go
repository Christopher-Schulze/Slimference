package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const aggregateSampleAdminState = `{
  "daemon": {"running": true, "pid": 1234, "version": "2.0.2"},
  "wss": {
    "engine_active": true,
    "phasef_bridged": 2,
    "frames_reencoded": 5,
    "compressed_messages_mutated": 5,
    "phasef_mutations": 5,
    "mutation_active": true,
    "byte_bridge_only": false,
    "parse_failures": 0,
    "degraded_sessions": 0,
    "compression_errors": 0
  },
  "savings": {
    "input_tokens_saved": 42000,
    "repdet_rewrites": 7,
    "repdet_bytes_saved": 1024,
    "stale_read_blocks": 1,
    "obsolete_prune_blocks": 0,
    "stop_seq_injections": 0,
    "beterse_injections": 0,
    "streamcut_fires": 3
  }
}`

func writeAggregateStateFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admin-state.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write admin state: %v", err)
	}
	return path
}

func TestAggregateSavingsTextOutputIncludesAllSections(t *testing.T) {
	statePath := writeAggregateStateFile(t, aggregateSampleAdminState)
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--admin-state-file=" + statePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"=== Slimference Aggregate Savings ===",
		"WSS Phase-F (live counters):",
		"phasef_bridged sessions:      2",
		"frames_reencoded:             5",
		"compressed_messages_mutated:  5",
		"phasef_mutations:             5",
		"mutation_active:              true",
		"byte_bridge_only:             false",
		"input_tokens_saved:           42000",
		"Output-Reduce sub-layers (live counters):",
		"repdet_rewrites:       7 (bytes saved: 1024)",
		"stale_read_blocks:     1",
		"HTTP-path Layer-0 filter: not loaded (pass --filter-db=<path>)",
		"Aggregate:",
		"WSS input tokens saved:        42000",
		"Filter Layer-0 tokens saved:   0",
		"TOTAL tokens saved:            42000",
		"Notes:",
		"WSS savings are workload-dependent",
		"mutation_active=true: the reducer chain is producing real WSS savings",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestAggregateSavingsJSONShape(t *testing.T) {
	statePath := writeAggregateStateFile(t, aggregateSampleAdminState)
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--admin-state-file=" + statePath, "--json", "--usd-per-million=3.0"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var got aggregateSavingsReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\noutput:\n%s", err, stdout.String())
	}
	if got.WSS.PhasefMutations != 5 {
		t.Fatalf("wss.phasef_mutations: got=%d want=5", got.WSS.PhasefMutations)
	}
	if got.WSS.InputTokensSaved != 42000 {
		t.Fatalf("wss.input_tokens_saved: got=%d want=42000", got.WSS.InputTokensSaved)
	}
	if got.OutputReduce.RepdetRewrites != 7 {
		t.Fatalf("output_reduce.repdet_rewrites: got=%d want=7", got.OutputReduce.RepdetRewrites)
	}
	if got.Aggregate.TotalTokensSaved != 42000 {
		t.Fatalf("aggregate.total_tokens_saved: got=%d want=42000", got.Aggregate.TotalTokensSaved)
	}
	wantUSD := 42000.0 * 3.0 / 1_000_000.0
	if got.Aggregate.EstUSDSaved < wantUSD-1e-9 || got.Aggregate.EstUSDSaved > wantUSD+1e-9 {
		t.Fatalf("aggregate.estimated_usd_saved: got=%v want=%v", got.Aggregate.EstUSDSaved, wantUSD)
	}
	if got.Source == "" || !strings.HasPrefix(got.Source, "file:") {
		t.Fatalf("source: got=%q want file:<path>", got.Source)
	}
	if len(got.Notes) == 0 {
		t.Fatal("notes must not be empty")
	}
	if got.Generated.IsZero() {
		t.Fatal("generated timestamp must be set")
	}
	if time.Since(got.Generated) > time.Minute {
		t.Fatalf("generated too old: %v", got.Generated)
	}
}

func TestAggregateSavingsByteBridgeNoteOnlyWhenBridgeOnly(t *testing.T) {
	bridgeOnlyState := strings.Replace(aggregateSampleAdminState,
		`"byte_bridge_only": false`,
		`"byte_bridge_only": true`, 1)
	bridgeOnlyState = strings.Replace(bridgeOnlyState,
		`"mutation_active": true`,
		`"mutation_active": false`, 1)
	statePath := writeAggregateStateFile(t, bridgeOnlyState)
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--admin-state-file=" + statePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "byte_bridge_only=true: the current daemon has bridged WSS sessions byte-equal") {
		t.Fatalf("byte_bridge_only=true should add the bridge-only note:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "mutation_active=true: the reducer chain") {
		t.Fatalf("bridge-only state must not also claim mutation_active is producing savings:\n%s", stdout.String())
	}
}

func TestAggregateSavingsHealthWarningOnErrors(t *testing.T) {
	bad := strings.Replace(aggregateSampleAdminState,
		`"parse_failures": 0`,
		`"parse_failures": 2`, 1)
	statePath := writeAggregateStateFile(t, bad)
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--admin-state-file=" + statePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "HEALTH WARN parse=2") {
		t.Fatalf("expected HEALTH WARN line in output:\n%s", stdout.String())
	}
}

func TestAggregateSavingsRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--nope"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit: got=%d want=2", code)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Fatalf("stderr should mention unknown flag: %s", stderr.String())
	}
}

func TestAggregateSavingsRejectsBadPeriod(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--period=yesterday"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit: got=%d want=2", code)
	}
	if !strings.Contains(stderr.String(), "--period must be") {
		t.Fatalf("stderr should mention valid periods: %s", stderr.String())
	}
}

func TestAggregateSavingsRejectsBadUSD(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--usd-per-million=-1"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit: got=%d want=2", code)
	}
	if !strings.Contains(stderr.String(), "--usd-per-million must be") {
		t.Fatalf("stderr should mention valid usd: %s", stderr.String())
	}
}

func TestAggregateSavingsMissingStateFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--admin-state-file=/nonexistent/path/admin-state.json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit: got=%d want=1", code)
	}
	if !strings.Contains(stderr.String(), "read admin state file") {
		t.Fatalf("stderr should mention read error: %s", stderr.String())
	}
}

func TestAggregateSavingsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit: got=%d want=0", code)
	}
	if !strings.Contains(stdout.String(), "aggregate-savings:") {
		t.Fatalf("help should explain the subcommand: %s", stdout.String())
	}
}

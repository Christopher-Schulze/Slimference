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
  "codex_route": {
    "daemon_reachable": true,
    "auto_mode": "wss_phasef",
    "auto_transport": "wss",
    "wss_certified": true,
    "wss_bridge_available": true,
    "needs_recert": false,
    "recert_status": "passed",
    "recert_attempt_id": "attempt-1",
    "recert_started_at": "2026-05-29T10:00:00Z",
    "recert_finished_at": "2026-05-29T10:01:00Z",
    "recert_last_success_at": "2026-05-29T10:01:00Z",
    "recert_log_path": "/tmp/codex-wss-recert.log"
  },
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
    "proxy_layer0_tool_result_blocks": 8,
    "proxy_layer0_tool_use_unresolved_blocks": 2,
    "proxy_layer0_command_resolved_blocks": 6,
    "proxy_layer0_command_unresolved_blocks": 2,
    "proxy_layer0_read_delta_attempts": 3,
    "proxy_layer0_read_delta_misses": 1,
    "proxy_layer0_blocks": 4,
    "proxy_layer0_read_delta_blocks": 2,
    "proxy_layer0_captured_output_blocks": 1,
    "proxy_layer0_codex_exec_envelope_blocks": 1,
    "proxy_layer0_repeated_output_blocks": 1,
    "proxy_layer0_routes": {
      "http": {
        "tool_result_blocks": 1,
        "tool_use_unresolved_blocks": 1,
        "command_resolved_blocks": 0,
        "command_unresolved_blocks": 1,
        "read_delta_attempts": 0,
        "read_delta_misses": 0,
        "requests_modified": 0,
        "tokens_saved": 0,
        "blocks_modified": 0,
        "read_delta_blocks": 0,
        "captured_output_blocks": 0,
        "codex_exec_envelope_blocks": 0,
        "repeated_output_blocks": 0
      },
      "wss_phasef": {
        "tool_result_blocks": 7,
        "tool_use_unresolved_blocks": 1,
        "command_resolved_blocks": 6,
        "command_unresolved_blocks": 1,
        "read_delta_attempts": 3,
        "read_delta_misses": 1,
        "requests_modified": 4,
        "tokens_saved": 42000,
        "blocks_modified": 4,
        "read_delta_blocks": 2,
        "captured_output_blocks": 1,
        "codex_exec_envelope_blocks": 1,
        "repeated_output_blocks": 1
      }
    },
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
		"Codex route / auto-recert:",
		"auto_mode:                 wss_phasef",
		"recert_status:             passed",
		"recert_log:                /tmp/codex-wss-recert.log",
		"WSS Phase-F (live counters):",
		"phasef_bridged sessions:      2",
		"frames_reencoded:             5",
		"compressed_messages_mutated:  5",
		"phasef_mutations:             5",
		"mutation_active:              true",
		"byte_bridge_only:             false",
		"input_tokens_saved:           42000",
		"proxy_layer0_tool_results:    8",
		"proxy_layer0_tool_misses:     2",
		"proxy_layer0_commands:        6",
		"proxy_layer0_command_misses:  2",
		"proxy_layer0_read_attempts:   3",
		"proxy_layer0_read_misses:     1",
		"proxy_layer0_blocks:          4",
		"read_delta:                 2",
		"captured_output:            1",
		"codex_exec_envelope:        1",
		"repeated_output:            1",
		"route_wss_phasef_tokens:      42000",
		"route_wss_phasef_misses:      tool=1 command=1 read=1",
		"route_http_tokens:            0",
		"route_http_misses:            tool=1 command=1 read=0",
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
	if got.CodexRoute.AutoMode != "wss_phasef" ||
		got.CodexRoute.AutoTransport != "wss" ||
		!got.CodexRoute.WSSCertified ||
		got.CodexRoute.RecertAttemptID != "attempt-1" ||
		got.CodexRoute.RecertLogPath != "/tmp/codex-wss-recert.log" ||
		got.CodexRoute.RecertStartedAt == nil ||
		got.CodexRoute.RecertFinishedAt == nil ||
		got.CodexRoute.RecertLastSuccess == nil {
		t.Fatalf("codex route recert snapshot mismatch: %+v", got.CodexRoute)
	}
	if got.WSS.InputTokensSaved != 42000 {
		t.Fatalf("wss.input_tokens_saved: got=%d want=42000", got.WSS.InputTokensSaved)
	}
	if got.WSS.ProxyLayer0ToolResults != 8 || got.WSS.ProxyLayer0ToolMisses != 2 ||
		got.WSS.ProxyLayer0Commands != 6 || got.WSS.ProxyLayer0CommandMisses != 2 ||
		got.WSS.ProxyLayer0ReadAttempts != 3 || got.WSS.ProxyLayer0ReadMisses != 1 ||
		got.WSS.ProxyLayer0ReadDelta != 2 || got.WSS.ProxyLayer0Captured != 1 ||
		got.WSS.ProxyLayer0Envelope != 1 || got.WSS.ProxyLayer0Repeated != 1 {
		t.Fatalf("wss proxy layer0 mechanism attribution mismatch: %+v", got.WSS)
	}
	if got.WSS.ProxyLayer0Routes.WSSPhaseF.TokensSaved != 42000 ||
		got.WSS.ProxyLayer0Routes.WSSPhaseF.ReadDeltaBlocks != 2 ||
		got.WSS.ProxyLayer0Routes.WSSPhaseF.RepeatedBlocks != 1 ||
		got.WSS.ProxyLayer0Routes.HTTP.CommandMisses != 1 {
		t.Fatalf("wss proxy layer0 route attribution mismatch: %+v", got.WSS.ProxyLayer0Routes)
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

func TestAggregateSavingsJSONOmitsZeroRecertTimes(t *testing.T) {
	stateBody := strings.ReplaceAll(aggregateSampleAdminState, `    "recert_started_at": "2026-05-29T10:00:00Z",
    "recert_finished_at": "2026-05-29T10:01:00Z",
    "recert_last_success_at": "2026-05-29T10:01:00Z",
`, "")
	statePath := writeAggregateStateFile(t, stateBody)
	var stdout, stderr bytes.Buffer
	code := runAggregateSavings([]string{"--admin-state-file=" + statePath, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "0001-01-01") ||
		strings.Contains(out, "recert_started_at") ||
		strings.Contains(out, "recert_finished_at") ||
		strings.Contains(out, "recert_last_success_at") ||
		strings.Contains(out, "recert_retry_after") {
		t.Fatalf("zero recert times should be omitted from JSON:\n%s", out)
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

func TestParseAggregateSavingsFlagsAcceptsSpaceSeparatedValues(t *testing.T) {
	flags, err := parseAggregateSavingsFlags([]string{
		"--admin-url", "http://127.0.0.1:8990/state",
		"--admin-state-file", "/tmp/admin-state.json",
		"--filter-db", "/tmp/filter.db",
		"--period", "today",
		"--usd-per-million", "2.5",
		"--json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if flags.adminStateURL != "http://127.0.0.1:8990/state" ||
		flags.adminStateFile != "/tmp/admin-state.json" ||
		flags.filterDB != "/tmp/filter.db" ||
		flags.period != "today" ||
		flags.usdPerMTokens != 2.5 ||
		flags.outputFormat != outputJSON {
		t.Fatalf("space-separated flags parsed incorrectly: %+v", flags)
	}
}

func TestParseAggregateSavingsFlagsRejectsMissingValue(t *testing.T) {
	if _, err := parseAggregateSavingsFlags([]string{"--filter-db"}); err == nil || !strings.Contains(err.Error(), "requires a value") {
		t.Fatalf("missing --filter-db value should be explicit, err=%v", err)
	}
	if _, err := parseAggregateSavingsFlags([]string{"--admin-url", "--json"}); err == nil || !strings.Contains(err.Error(), "requires a value") {
		t.Fatalf("missing --admin-url value should be explicit, err=%v", err)
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
	if !strings.Contains(stdout.String(), "~/.slimference/filter.db") ||
		strings.Contains(stdout.String(), "~/.slimference/analytics/filter.db") {
		t.Fatalf("help should use the canonical filter.db path: %s", stdout.String())
	}
}

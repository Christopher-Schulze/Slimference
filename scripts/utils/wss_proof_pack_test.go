package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

func TestWSSProofPackFreshInstrumentedWindowPasses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "fresh",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 10000, Final: 9000, Saved: 1000},
		DebugFacts: map[string]string{
			"wss.request_shape":                     "full_history",
			"wss.prefix_total_bytes":                "2000",
			"wss.prefix_estimated_tokens":           "500",
			"wss.raw_input_bytes":                   "30000",
			"wss.tool_result_output_bytes":          "12000",
			"wss.output_reduce_reason":              "disabled",
			"wss.tool_results":                      "1",
			"wss.source_tool_bytes":                 "0",
			"wss.shadow_mirror_blocks":              "1",
			"wss.shadow_mirror_bytes":               "12000",
			"wss.shadow_mirror_referenceable_bytes": "6000",
		},
	})

	report, err := loadWSSProofPack(wssProofPackFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSProofPack() error = %v", err)
	}
	if !report.GatePassed ||
		report.LocalGap.InstrumentedRequests != 1 ||
		report.LocalGap.MissingInstrRequests != 0 ||
		report.ClassDistribution.FullHistoryRequests != 1 ||
		report.ReferenceInventory.Lane3AcceptedContracts != 0 ||
		report.SocketCommand != "slimference debug wss-sockets 200 --json" ||
		report.ProofDecision == "" {
		t.Fatalf("bad fresh proof pack: %+v", report)
	}

	var stdout, stderr bytes.Buffer
	code := runWSSProofPack([]string{path, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runWSSProofPack code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var cliReport wssProofPackReport
	if err := json.Unmarshal(stdout.Bytes(), &cliReport); err != nil {
		t.Fatalf("parse proof-pack json: %v\n%s", err, stdout.String())
	}
	if !cliReport.GatePassed || cliReport.LocalGap.InstrumentedRequests != 1 {
		t.Fatalf("bad cli proof pack: %+v", cliReport)
	}
}

func TestWSSProofPackIngestsSocketReconnectHandoff(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	socketsPath := filepath.Join(dir, "wss-sockets.json")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "fresh",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 10000, Final: 9200, Saved: 800},
		DebugFacts: map[string]string{
			"wss.request_shape":            "full_history",
			"wss.prefix_total_bytes":       "2000",
			"wss.prefix_estimated_tokens":  "500",
			"wss.raw_input_bytes":          "30000",
			"wss.tool_result_output_bytes": "12000",
			"wss.tool_results":             "1",
			"wss.source_tool_bytes":        "0",
		},
	})
	writeJSONFile(t, socketsPath, map[string]any{
		"socket_count":                                 2,
		"actionable_sockets":                           1,
		"provider_input_tokens":                        9000,
		"provider_cached_tokens":                       3000,
		"local_saved_tokens":                           700,
		"full_history_requests":                        2,
		"full_history_provider_input_tokens":           9000,
		"reconnect_full_history_requests":              2,
		"reconnect_full_history_provider_input_tokens": 9000,
		"cause_classes":                                map[string]int{"client_full_history_reconnect": 1},
		"close_initiators":                             map[string]int{"client_eof": 1},
		"reconnect_full_history_by_cause": []map[string]any{{
			"cause":                    "client_full_history_reconnect",
			"provider_input_tokens":    9000,
			"retry_resend_cost_tokens": 9000,
		}},
		"t417_reconnect_handoff": []map[string]any{{
			"socket_key":               "codex-wss:thread#2.1",
			"cause":                    "client_full_history_reconnect",
			"continuation_candidate":   "t417_stateless_or_lineage_reroute",
			"reconnect_input_tokens":   9000,
			"retry_resend_cost_tokens": 9000,
			"full_history_requests":    2,
			"provider_input_tokens":    9000,
			"provider_cached_tokens":   3000,
			"local_saved_tokens":       700,
		}},
	})

	report, err := loadWSSProofPack(wssProofPackFlags{path: path, socketsJSON: socketsPath})
	if err != nil {
		t.Fatalf("loadWSSProofPack() error = %v", err)
	}
	if !report.GatePassed ||
		report.SocketSummary == nil ||
		report.SocketSummary.ReconnectFullHistoryRequests != 2 ||
		report.SocketSummary.T417ReconnectHandoffRows != 1 ||
		report.SocketSummary.TopReconnectCause != "client_full_history_reconnect" ||
		report.SocketSummary.ContinuationCandidates["t417_stateless_or_lineage_reroute"] != 1 ||
		report.ProofDecision != "t420_reconnect_handoff_present" {
		t.Fatalf("bad socket handoff proof pack: %+v", report)
	}
}

func TestWSSProofPackReconnectWithoutHandoffFailsClosed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	socketsPath := filepath.Join(dir, "wss-sockets.json")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "fresh",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 10000, Final: 9500, Saved: 500},
		DebugFacts: map[string]string{
			"wss.request_shape":           "delta",
			"wss.prefix_total_bytes":      "2000",
			"wss.prefix_estimated_tokens": "500",
			"wss.raw_input_bytes":         "30000",
		},
	})
	writeJSONFile(t, socketsPath, map[string]any{
		"sockets":                         []any{},
		"reconnect_full_history_requests": 2,
		"reconnect_full_history_provider_input_tokens": 9000,
	})

	report, err := loadWSSProofPack(wssProofPackFlags{path: path, socketsJSON: socketsPath})
	if err != nil {
		t.Fatalf("loadWSSProofPack() error = %v", err)
	}
	if report.GatePassed ||
		report.ProofDecision != "t420_reconnect_handoff_missing" ||
		len(report.GateFailures) != 1 ||
		!strings.Contains(report.GateFailures[0], "reconnect_full_history_without_handoff") {
		t.Fatalf("reconnect without handoff should fail closed: %+v", report)
	}
}

func TestWSSProofPackIngestsAuditReferenceHeadroom(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	auditPath := filepath.Join(dir, "wss-audit.json")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "fresh",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 10000, Final: 9800, Saved: 200},
		DebugFacts: map[string]string{
			"wss.request_shape":           "delta",
			"wss.prefix_total_bytes":      "2000",
			"wss.prefix_estimated_tokens": "500",
			"wss.raw_input_bytes":         "30000",
		},
	})
	writeJSONFile(t, auditPath, map[string]any{
		"requests":        9,
		"phasef_requests": 7,
		"shadow_mirror": map[string]any{
			"requests":                               5,
			"referenceable_bytes":                    12000,
			"normalized_referenceable_bytes":         9000,
			"normalized_referenceable_byte_pct":      60.0,
			"normalized_referenceable_segments":      3,
			"normalized_referenceable_bytes_by_kind": []any{},
			"referenceable_blocks":                   3,
			"referenceable_byte_pct":                 50.0,
			"blocks":                                 5,
			"bytes":                                  24000,
			"normalized_segments":                    4,
			"normalized_bytes":                       15000,
		},
		"shadow_mirror_candidates": []map[string]any{{
			"request_shape":                     "full_history",
			"kind":                              "exact_block",
			"requests":                          4,
			"candidate_lane":                    "t408_backend_reference_contract",
			"next_proof_gate":                   "t408_backend_reference_acceptance_or_exact_rehydrate_contract",
			"promotion_stage":                   "blocked_backend_reference_contract",
			"candidate_local_tokens_estimate":   5000,
			"incremental_local_tokens_headroom": 4200,
			"error_free":                        true,
			"recommended_action":                "run backend reference acceptance probe",
			"promotion_open_blocker_headroom_tokens": map[string]int{
				"reference_only_backend_contract_required": 4200,
			},
		}},
	})

	report, err := loadWSSProofPack(wssProofPackFlags{path: path, auditJSON: auditPath})
	if err != nil {
		t.Fatalf("loadWSSProofPack() error = %v", err)
	}
	if !report.GatePassed ||
		report.AuditSummary == nil ||
		report.AuditSummary.ShadowMirrorReferenceableBytes != 12000 ||
		len(report.AuditSummary.TopCandidates) != 1 ||
		report.AuditSummary.TopCandidates[0].CandidateLane != "t408_backend_reference_contract" ||
		report.AuditSummary.TopCandidates[0].PromotionOpenBlockerHeadroom["reference_only_backend_contract_required"] != 4200 ||
		report.ProofDecision != "t408_reference_contract_headroom_present" {
		t.Fatalf("bad audit headroom proof pack: %+v", report)
	}
}

func TestWSSProofPackIngestsAuditParserRecoveryHeadroom(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	auditPath := filepath.Join(dir, "wss-audit.json")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "fresh",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 10000, Final: 9800, Saved: 200},
		DebugFacts: map[string]string{
			"wss.request_shape":           "delta",
			"wss.prefix_total_bytes":      "2000",
			"wss.prefix_estimated_tokens": "500",
			"wss.raw_input_bytes":         "30000",
		},
	})
	writeJSONFile(t, auditPath, map[string]any{
		"requests":        4,
		"phasef_requests": 4,
		"shadow_mirror_candidates": []map[string]any{{
			"request_shape":                     "full_history",
			"kind":                              "codex_exec_payload",
			"requests":                          2,
			"candidate_lane":                    "t408_reference_or_t418_parser_recovery",
			"next_proof_gate":                   "t408_backend_reference_or_t418_parser_recovery_gate",
			"promotion_stage":                   "blocked_parser_recovery_contract",
			"candidate_local_tokens_estimate":   3000,
			"incremental_local_tokens_headroom": 2600,
			"error_free":                        true,
			"recommended_action":                "rank parser/recovery slice",
		}},
	})

	report, err := loadWSSProofPack(wssProofPackFlags{path: path, auditJSON: auditPath})
	if err != nil {
		t.Fatalf("loadWSSProofPack() error = %v", err)
	}
	if report.ProofDecision != "t408_or_t418_parser_recovery_headroom_present" ||
		report.RecommendedNextStep != "choose backend references or the largest parser/recovery-backed T418 slice from the audit candidate; do not loosen broad WSS guards" {
		t.Fatalf("bad parser/recovery audit decision: %+v", report)
	}
}

func TestWSSProofPackStaleRowsFailUnlessAllowed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "stale",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		Tokens:    dbg.TokenCounts{Original: 9000, Final: 9000, Saved: 0},
	})

	report, err := loadWSSProofPack(wssProofPackFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSProofPack() error = %v", err)
	}
	if report.GatePassed ||
		len(report.GateFailures) != 1 ||
		!strings.Contains(report.GateFailures[0], "missing_instrumentation_requests=1") ||
		report.ProofDecision != "capture_fresh_instrumented_window" {
		t.Fatalf("stale proof pack should fail closed: %+v", report)
	}

	allowed, err := loadWSSProofPack(wssProofPackFlags{path: path, allowStale: true})
	if err != nil {
		t.Fatalf("loadWSSProofPack allowStale error = %v", err)
	}
	if !allowed.GatePassed || strings.Contains(allowed.LocalGapCommand, "--require-instrumented") {
		t.Fatalf("allow-stale proof pack should pass without hard instrumented command: %+v", allowed)
	}

	var stdout, stderr bytes.Buffer
	code := runWSSProofPack([]string{path, "--json"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("runWSSProofPack stale code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestParseWSSProofPackFlags(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sincePath := filepath.Join(dir, "since.txt")
	writeFileForLocalArtifactTest(t, sincePath, "2026-06-20T20:00:00Z\n")

	flags, err := parseWSSProofPackFlags([]string{
		"decisions.jsonl",
		"--since-file=" + sincePath,
		"--sockets-json=wss-sockets.json",
		"--audit-json=wss-audit.json",
		"--min-local-ratio=0.5",
		"--require-headroom",
		"--require-accepted-contract",
		"--allow-stale",
		"--json",
	})
	if err != nil {
		t.Fatalf("parseWSSProofPackFlags() error = %v", err)
	}
	if flags.path != "decisions.jsonl" ||
		flags.sinceFile != sincePath ||
		flags.socketsJSON != "wss-sockets.json" ||
		flags.auditJSON != "wss-audit.json" ||
		flags.minLocalRatio != 0.5 ||
		!flags.requireHeadroom ||
		!flags.requireAcceptedContract ||
		!flags.allowStale ||
		flags.outputFormat != outputJSON {
		t.Fatalf("bad flags: %+v", flags)
	}
	if _, err := parseWSSProofPackFlags([]string{"one", "two"}); err == nil {
		t.Fatal("expected multiple root error")
	}
}

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

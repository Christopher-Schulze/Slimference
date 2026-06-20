package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSST354ShapeProofPassesCleanMutatedDeltaAndFollowingTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-clean.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 180), false),
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 40), true),
		wssT354TestFrame("server_to_client", wssT354TestOutputItemDone("item-mutated", "call_mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.Totals.CandidatesPassing != 1 ||
		report.Totals.MutatedDeltaCandidates != 1 ||
		report.Totals.UpstreamErrorFrames != 0 ||
		report.Totals.Lost != 0 {
		t.Fatalf("clean mutated delta should pass exact T354 shape proof: %+v", report)
	}
	if len(report.Rows) != 1 || len(report.Rows[0].Candidates) != 1 ||
		!report.Rows[0].Candidates[0].FollowingTurnClean {
		t.Fatalf("candidate/following proof missing: %+v", report.Rows)
	}
	if report.Totals.CapturedLocalSavedTokens <= 0 ||
		report.Totals.RetryOrResendExtraTokens != 0 ||
		report.Totals.NetCapturedLocalSavedTokens <= 0 {
		t.Fatalf("candidate economics missing: %+v", report.Totals)
	}
	if report.Totals.ProviderUsage.InputTokens != 2000 ||
		report.Totals.ProviderUsage.CachedTokens != 600 ||
		report.Totals.ProviderUsage.OutputTokens != 24 {
		t.Fatalf("provider usage must stay separate from local savings: %+v", report.Totals.ProviderUsage)
	}
	if report.Totals.MetadataComparisons != 1 ||
		report.Totals.MetadataMismatches != 0 ||
		report.Totals.CandidatesWithServerOutputID != 1 {
		t.Fatalf("metadata consistency and server output-id proof missing: %+v", report.Totals)
	}
	candidate := report.Rows[0].Candidates[0]
	if !candidate.PromotionEligible ||
		candidate.NetCapturedLocalSavedTokens != candidate.CapturedLocalSavedTokens ||
		candidate.EconomicsVerdict != "delta_net_positive" ||
		candidate.ContinuationCandidate != "stateful_delta_proven_slice" ||
		report.Totals.NetPositiveCandidates != 1 ||
		report.Totals.NetPositiveNetSavedTokens != candidate.NetCapturedLocalSavedTokens ||
		report.Totals.TopNetCandidate == nil ||
		report.Totals.TopNetCandidate.ContinuationCandidate != "stateful_delta_proven_slice" {
		t.Fatalf("candidate net-economics ranking missing: candidate=%+v totals=%+v", candidate, report.Totals)
	}
}

func TestWSST354ShapeProofCollapsesAdjacentDuplicateRequestFrames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-duplicate-adjacent.frames.jsonl")
	original := wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 180)
	mutated := wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 40)
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", original, false),
		wssT354TestFrame("client_to_server", original, false),
		wssT354TestFrame("client_to_server", mutated, true),
		wssT354TestFrame("client_to_server", mutated, true),
		wssT354TestFrame("server_to_client", wssT354TestOutputItemDone("item-mutated", "call_mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.Totals.RequestTurns != 3 ||
		report.Totals.MutatedToolOutputCandidates != 1 ||
		report.Totals.CandidatesPassing != 1 ||
		report.Totals.CapturedLocalSavedTokens <= 0 {
		t.Fatalf("adjacent duplicate C2S capture records should collapse to one logical mutated turn: %+v", report)
	}
}

func TestWSST354ShapeProofBlocksMissingFollowingTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-missing-follow.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequest("resp-before", "call_mutated"), true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed ||
		report.Totals.MissingFollowingTurnCandidates != 1 ||
		report.Totals.UnprovenCandidates != 1 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "missing_following_turn") {
		t.Fatalf("missing following turn must block T354 unlock proof: %+v", report)
	}
}

func TestWSST354ShapeProofAcceptsFinalOpenCandidateWhenDownstreamProven(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-shadow-final-open.frames.jsonl")
	writeSearchCapProofCapturedShadowFrames(t, path, "t354-shape-shadow", 96)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.Totals.MutatedToolOutputCandidates != 3 ||
		report.Totals.CandidatesPassing != 2 ||
		report.Totals.MissingFollowingTurnCandidates != 1 ||
		report.Totals.UnsafeCandidates != 0 ||
		report.Totals.UnprovenCandidates != 1 {
		t.Fatalf("final open candidate must be unproven, not unsafe, after clean downstream proof: %+v", report)
	}
	if strings.Contains(strings.Join(report.GateFailures, "\n"), "missing_following_turn") {
		t.Fatalf("final open candidate must not block a proven downstream row: %+v", report)
	}
}

func TestWSST354ShapeProofChargesPairedFullHistoryExpansionOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-paired-full-history-expansion.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a", "call_a", 20), false, 71),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_a", 180), true, 71),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-a-mutated"), false),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a-mutated", "call_b", 20), false, 72),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_b", 160), true, 72),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-b-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-b-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.Totals.CandidatesPassing != 2 {
		t.Fatalf("paired full-history expansion proof should pass cleanly: %+v", report)
	}
	wantRetry := 0
	for _, candidate := range report.Rows[0].Candidates {
		if candidate.Shape != "full_history" {
			continue
		}
		wantRetry += positiveDelta(candidate.RequestTokensEstimate, candidate.CapturedOriginalRequestTokens)
	}
	if wantRetry <= 0 ||
		report.Totals.RetryOrResendExtraTokens != wantRetry ||
		report.Rows[0].Candidates[0].RetryOrResendExtraTokens >= report.Rows[0].Candidates[0].FollowingRequestTokensEstimate {
		t.Fatalf("paired full-history rebuild must charge only expansion over captured original, totals=%+v candidates=%+v want_retry=%d",
			report.Totals, report.Rows[0].Candidates, wantRetry)
	}
}

func TestWSST354ShapeProofChargesUnpairedFullHistoryFollowingConservatively(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-unpaired-full-history-following.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a", "call_a", 180), false),
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a", "call_a", 40), true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-a-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_following", 40), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || len(report.Rows[0].Candidates) != 1 {
		t.Fatalf("unpaired following full-history proof should pass but stay cost-conservative: %+v", report)
	}
	candidate := report.Rows[0].Candidates[0]
	if candidate.FollowingTurnShape != "full_history" ||
		candidate.RetryOrResendExtraTokens != candidate.FollowingRequestTokensEstimate ||
		report.Totals.RetryOrResendExtraTokens != candidate.FollowingRequestTokensEstimate ||
		candidate.NetCapturedLocalSavedTokens != candidate.CapturedLocalSavedTokens-candidate.RetryOrResendExtraTokens {
		t.Fatalf("unpaired full-history following must charge the full following request, totals=%+v candidate=%+v", report.Totals, candidate)
	}
}

func TestWSST354ShapeProofKeepsNegativeNetCandidateGuarded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-negative-net-following.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a", "call_a", 50), false),
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a", "call_a", 40), true),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-a-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_following", 260), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || len(report.Rows[0].Candidates) != 1 {
		t.Fatalf("negative-net candidate can be safety-clean but must not promote: %+v", report)
	}
	candidate := report.Rows[0].Candidates[0]
	if candidate.NetCapturedLocalSavedTokens >= 0 ||
		candidate.PromotionEligible ||
		candidate.EconomicsVerdict != "negative_net" ||
		candidate.ContinuationCandidate != "keep_guarded_retry_resend_negative" ||
		report.Totals.NetPositiveCandidates != 0 ||
		report.Totals.TopNetCandidate != nil {
		t.Fatalf("negative-net candidate should stay guarded: candidate=%+v totals=%+v", candidate, report.Totals)
	}
}

func TestWSST354ShapeProofRanksNetPositiveFullHistoryCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-positive-full-history.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 220), false, 91),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 40), true, 91),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-history-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-history-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.Totals.CandidatesPassing != 1 {
		t.Fatalf("positive full-history candidate should pass: %+v", report)
	}
	candidate := report.Rows[0].Candidates[0]
	if candidate.Shape != "full_history" ||
		!candidate.PromotionEligible ||
		candidate.EconomicsVerdict != "class_b_net_positive" ||
		candidate.ContinuationCandidate != "stateful_preserved_class_b" ||
		candidate.NetCapturedLocalSavedTokens <= 0 ||
		report.Totals.NetPositiveFullHistory != 1 ||
		report.Rows[0].TopNetCandidate == nil ||
		report.Rows[0].TopNetCandidate.Shape != "full_history" ||
		report.Totals.TopNetCandidate == nil ||
		report.Totals.TopNetCandidate.ContinuationCandidate != "stateful_preserved_class_b" {
		t.Fatalf("full-history net-economics ranking mismatch: candidate=%+v row=%+v totals=%+v",
			candidate, report.Rows[0].TopNetCandidate, report.Totals)
	}
	if !strings.Contains(strings.Join(report.Findings, "\n"), "net_positive_full_history_candidates=1") ||
		!strings.Contains(strings.Join(report.Findings, "\n"), "top_net_candidate=full_history") {
		t.Fatalf("findings missing net-positive full-history signal: %+v", report.Findings)
	}
}

func TestWSST354ShapeProofRequiresRecoveryContractForClassB(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-recovery-contract.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 220), false, 91),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 40), true, 91),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-history-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-history-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path, requireRecoveryContract: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.T419RecoveryContract == nil {
		t.Fatalf("recovery contract should be attached and pass: %+v", report)
	}
	if report.T419RecoveryContract.ProductGaps != 0 ||
		!report.T419RecoveryContract.T417ServerStateRowReady ||
		report.T419RecoveryContract.ArchiveBackedRows == 0 ||
		report.T419RecoveryContract.RehydrateBeforeUpstream == 0 {
		t.Fatalf("T419 recovery gate missing Class-B prerequisites: %+v", report.T419RecoveryContract)
	}
	findings := strings.Join(report.Findings, "\n")
	for _, want := range []string{
		"t419_recovery_contract_product_gaps=0",
		"t419_t417_server_state_recovery_ready",
		"top_net_candidate=full_history",
	} {
		if !strings.Contains(findings, want) {
			t.Fatalf("missing finding %q in %+v", want, report.Findings)
		}
	}

	var stdout, stderr bytes.Buffer
	code := runWSST354ShapeProof([]string{path, "--require-recovery-contract"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, "t419_contract:") ||
		!strings.Contains(text, "product_gaps=0") ||
		!strings.Contains(text, "t417_ready=true") {
		t.Fatalf("text report lost T419 recovery contract: %s", text)
	}
}

func TestWSST354ShapeProofIngestsT420ReconnectHandoff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "t354-clean.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 220), false, 91),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 40), true, 91),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-history-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-history-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)
	handoffPath := filepath.Join(dir, "wss-sockets.json")
	writeJSONFile(t, handoffPath, map[string]any{
		"t417_reconnect_handoff": []map[string]any{{
			"socket_key":                              "codex-wss:desktop-thread#2.1",
			"session_id":                              "desktop-thread",
			"cause":                                   "client_full_history_reconnect",
			"request_shapes":                          map[string]int{"full_history": 2},
			"requests":                                2,
			"full_history_requests":                   2,
			"provider_input_tokens":                   9000,
			"provider_cached_tokens":                  3000,
			"local_saved_tokens":                      700,
			"reconnect_input_tokens":                  9000,
			"retry_resend_cost_tokens":                9000,
			"previous_socket_key":                     "codex-wss:desktop-thread#1.1",
			"previous_close_initiator":                "client_eof",
			"reconnect_gap_ms":                        45,
			"attribution":                             "observed_previous_socket",
			"continuation_candidate":                  "t417_stateless_or_lineage_reroute",
			"recommended_action":                      "route exact reconnect class into T417",
			"candidate_potential_local_tokens":        0,
			"unexpected_ignored_forward_compat_field": true,
		}},
	})

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path, t420HandoffPath: handoffPath})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.T420HandoffPath != handoffPath ||
		len(report.T420ReconnectHandoff) != 1 ||
		report.Totals.T420ReconnectHandoffRows != 1 ||
		report.Totals.T420ReconnectInputTokens != 9000 ||
		report.Totals.T420RetryResendCostTokens != 9000 ||
		report.Totals.T420ProviderCachedTokens != 3000 ||
		report.Totals.T420LocalSavedTokens != 700 ||
		report.Totals.T417ReconnectRerouteCandidates != 1 ||
		report.Totals.T420TransportFixCandidates != 0 {
		t.Fatalf("T420 handoff was not ingested as T417 candidate input: %+v", report)
	}
	row := report.T420ReconnectHandoff[0]
	if row.EconomicsVerdict != "t417_reconnect_reroute_input" ||
		row.CandidatePotentialLocalTokens != 9000 ||
		row.RequestShapes["full_history"] != 2 {
		t.Fatalf("T420 handoff candidate economics mismatch: %+v", row)
	}
	findings := strings.Join(report.Findings, "\n")
	for _, want := range []string{
		"t420_reconnect_handoff_rows=1",
		"t420_reconnect_input_tokens=9000",
		"t417_reconnect_reroute_candidates=1",
	} {
		if !strings.Contains(findings, want) {
			t.Fatalf("missing finding %q in %+v", want, report.Findings)
		}
	}
	var stdout, stderr bytes.Buffer
	code := runWSST354ShapeProof([]string{path, "--t420-handoff-json", handoffPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, "t420_handoff:") ||
		!strings.Contains(text, "t417_reroute=1") ||
		!strings.Contains(text, "t420_handoff_row:  socket=codex-wss:desktop-thread#2.1") {
		t.Fatalf("text report lost T420 handoff: %s", text)
	}
}

func TestWSST354ShapeProofIngestsT408OpenSlice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "t354-clean.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 220), false, 91),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 40), true, 91),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-history-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-history-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)
	openSlicePath := filepath.Join(dir, "wss-audit-open-slice.json")
	writeJSONFile(t, openSlicePath, wssAuditReport{
		ShadowMirrorCandidates: []wssShadowMirrorCandidate{{
			RequestShape:                   "full_history",
			Kind:                           "stateful_safe_tool_output",
			CandidateLane:                  "t417_class_b_server_state",
			CandidateLocalTokensEstimate:   1916726,
			IncrementalLocalTokensHeadroom: 890310,
			PromotionOpenRequests:          33,
			PromotionOpenCandidateTokens:   1156175,
			PromotionOpenLocalSavedTokens:  489200,
			PromotionOpenHeadroom:          666975,
			PromotionOpenReady:             true,
			PromotionOpenStage:             "t417_exact_scope_open_slice_candidate",
			ProviderInputTokens:            2340176,
			ProviderCachedTokens:           1420800,
			PromotionBlockers:              []string{"cache_bust_demotion_present_exact_class_scope"},
			TopSessions: []wssShadowMirrorCandidateSession{{
				SessionID:                      "codex-wss:top-open",
				PromotionOpenRequests:          30,
				PromotionOpenHeadroom:          600000,
				PromotionOpenReady:             true,
				PromotionOpenStage:             "t417_exact_scope_open_slice_candidate",
				IncrementalLocalTokensHeadroom: 700000,
				PromotionBlockers:              []string{"cache_bust_demotion_present_exact_class_scope"},
			}},
		}},
	})

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path, t408OpenSlicePath: openSlicePath})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.T408OpenSlicePath != openSlicePath ||
		len(report.T408OpenSlices) != 1 ||
		report.Totals.T408OpenSliceRows != 1 ||
		report.Totals.T408OpenSliceRequests != 33 ||
		report.Totals.T408OpenSliceHeadroomTokens != 666975 ||
		report.Totals.TopT408OpenSlice == nil ||
		report.Totals.TopT408OpenSlice.TopSessionID != "codex-wss:top-open" ||
		report.Totals.TopT408OpenSlice.TopSessionOpenHeadroomTokens != 600000 {
		t.Fatalf("T408 open slice was not ingested as T417 input: %+v", report)
	}
	row := report.T408OpenSlices[0]
	if row.EconomicsVerdict != "t417_open_slice_net_positive" ||
		row.ContinuationCandidate != "t417_exact_scope_open_slice" ||
		row.AggregateBlockers[0] != "cache_bust_demotion_present_exact_class_scope" {
		t.Fatalf("T408 open-slice candidate mismatch: %+v", row)
	}
	findings := strings.Join(report.Findings, "\n")
	for _, want := range []string{
		"t408_open_slice_rows=1",
		"t408_open_slice_headroom_tokens=666975",
		"top_t408_open_slice=full_history/stateful_safe_tool_output",
	} {
		if !strings.Contains(findings, want) {
			t.Fatalf("missing finding %q in %+v", want, report.Findings)
		}
	}
	var stdout, stderr bytes.Buffer
	code := runWSST354ShapeProof([]string{path, "--t408-open-slice-json", openSlicePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, "t408_open_slice:") ||
		!strings.Contains(text, "top_t408_slice:") ||
		!strings.Contains(text, "t408_open_row:") {
		t.Fatalf("text report lost T408 open slice: %s", text)
	}
}

func TestWSST354ShapeProofRejectsReferenceOnlyT408OpenSlice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "t354-clean.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 220), false, 91),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 40), true, 91),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-history-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-history-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)
	openSlicePath := filepath.Join(dir, "wss-audit-reference-only-open-slice.json")
	writeJSONFile(t, openSlicePath, wssAuditReport{
		ShadowMirrorCandidates: []wssShadowMirrorCandidate{{
			RequestShape:                  "full_history",
			Kind:                          "exact_block",
			CandidateLane:                 "t417_class_b_server_state",
			PromotionOpenRequests:         33,
			PromotionOpenCandidateTokens:  1156175,
			PromotionOpenLocalSavedTokens: 489200,
			PromotionOpenHeadroom:         666975,
			PromotionOpenReady:            true,
			PromotionOpenStage:            "t417_exact_scope_open_slice_candidate",
		}},
	})

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path, t408OpenSlicePath: openSlicePath})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || !stringSliceContains(report.GateFailures, "t408_open_slice_candidates=0") || len(report.T408OpenSlices) != 0 {
		t.Fatalf("reference-only shadow mirror slice must not be accepted as product open slice: %+v", report)
	}
}

func TestWSST354ShapeProofFailsClosedForEmptyT408OpenSlice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "t354-clean.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 220), false, 91),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_history", 40), true, 91),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-history-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-history-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)
	openSlicePath := filepath.Join(dir, "wss-audit-no-open-slice.json")
	writeJSONFile(t, openSlicePath, wssAuditReport{
		ShadowMirrorCandidates: []wssShadowMirrorCandidate{{
			RequestShape:          "full_history",
			Kind:                  "exact_block",
			CandidateLane:         "t417_class_b_server_state",
			PromotionOpenHeadroom: 0,
			PromotionOpenReady:    false,
			PromotionOpenBlockers: []string{"no_promotion_open_headroom"},
			PromotionOpenStage:    "t417_no_open_slice_candidate",
		}},
	})

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path, t408OpenSlicePath: openSlicePath})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || !stringSliceContains(report.GateFailures, "t408_open_slice_candidates=0") {
		t.Fatalf("empty explicit T408 open slice should fail closed: %+v", report)
	}
}

func TestWSST354ShapeProofAcceptsEmptyT420ReconnectHandoffObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wss-sockets-empty.json")
	writeJSONFile(t, path, map[string]any{"t417_reconnect_handoff": []any{}})
	rows, err := loadWSST354T420ReconnectHandoff(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty top-level T420 handoff should load as empty rows: %+v", rows)
	}
}

func TestWSST354ShapeProofAcceptsSocketReportWithoutReconnectHandoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wss-sockets-no-reconnect.json")
	writeJSONFile(t, path, map[string]any{
		"sockets":                         []any{},
		"reconnect_full_history_requests": 0,
	})
	rows, err := loadWSST354T420ReconnectHandoff(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("socket report without reconnect mass should load as empty handoff: %+v", rows)
	}
}

func TestWSST354ShapeProofRejectsReconnectSocketReportWithoutHandoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wss-sockets-missing-handoff.json")
	writeJSONFile(t, path, map[string]any{
		"sockets":                         []any{},
		"reconnect_full_history_requests": 2,
	})
	_, err := loadWSST354T420ReconnectHandoff(path)
	if err == nil || !strings.Contains(err.Error(), "no t417_reconnect_handoff rows") {
		t.Fatalf("expected missing handoff error, got %v", err)
	}
}

func TestWSST354ShapeProofBlocksInvalidRequest400(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-400.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestFrame("client_to_server", wssT354TestToolOutputRequest("resp-before", "call_mutated"), true),
		wssT354TestFrame("server_to_client", map[string]any{
			"type":   "error",
			"status": 400,
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "Invalid request",
			},
		}, false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	var stdout, stderr bytes.Buffer
	code := runWSST354ShapeProof([]string{path, "--json"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("run code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssT354ShapeProofReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\nstdout=%s", err, stdout.String())
	}
	if report.GatePassed ||
		report.Totals.HTTP400Errors != 1 ||
		report.Totals.InvalidRequestErrors != 1 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "invalid_request=1") {
		t.Fatalf("invalid_request 400 must block T354 unlock proof: %+v", report)
	}
}

func TestWSST354ShapeProofDoesNotPairDifferentSequences(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-sequence-mismatch.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 180), false, 11),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_mutated", 40), true, 12),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.Totals.CandidatesPassing != 1 {
		t.Fatalf("sequence mismatch fixture should still prove clean downstream shape: %+v", report)
	}
	if report.Totals.CapturedLocalSavedTokens != 0 || report.Totals.NetCapturedLocalSavedTokens != 0 {
		t.Fatalf("different sequences must not create captured local savings: %+v", report.Totals)
	}
}

func TestWSST354ShapeProofBlocksReferenceMetadataMismatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-metadata-mismatch.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_original", 180), false, 31),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_changed", 40), true, 31),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed ||
		report.Totals.MetadataComparisons != 1 ||
		report.Totals.MetadataMismatches != 1 ||
		report.Totals.CandidatesPassing != 0 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "metadata_reference_mismatch") {
		t.Fatalf("reference metadata mismatch must block T354 unlock proof: %+v", report)
	}
	if report.Rows[0].Candidates[0].PromotionEligible ||
		report.Rows[0].Candidates[0].EconomicsVerdict != "unsafe" ||
		report.Rows[0].Candidates[0].ContinuationCandidate != "keep_guarded_safety_failure" {
		t.Fatalf("metadata mismatch must stay unsafe in candidate economics: %+v", report.Rows[0].Candidates[0])
	}
}

func TestWSST354ShapeProofBlocksReferenceMetadataMismatchEvenWithCleanCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-mixed-metadata-mismatch.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a", "call_clean", 180), false, 61),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-a", "call_clean", 40), true, 61),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-clean"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-clean"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-clean-follow"), false),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-clean-follow", "call_original", 180), false, 62),
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-clean-follow", "call_changed", 40), true, 62),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mismatch"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mismatch"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mismatch-follow"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed ||
		report.Totals.CandidatesPassing != 1 ||
		report.Totals.UnsafeCandidates != 1 ||
		report.Totals.MetadataMismatches != 1 ||
		!strings.Contains(strings.Join(report.GateFailures, "\n"), "metadata_reference_mismatch") {
		t.Fatalf("metadata mismatch must remain a hard safety failure even with another clean candidate: %+v", report)
	}
}

func TestWSST354ShapeProofSkipsMetadataMismatchForShapeChangedRebuild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-shape-changed-rebuild.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestToolOutputRequestLines("resp-before", "call_delta", 180), false, 41),
		wssT354TestSequencedFrame("client_to_server", wssT354TestFullHistoryToolOutputRequestLines("call_delta", 40), true, 41),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.Totals.MetadataComparisons != 0 ||
		report.Totals.MetadataMismatches != 0 ||
		report.Totals.CandidatesPassing != 1 {
		t.Fatalf("shape-changing stateless rebuild must not be blocked as metadata mismatch: %+v", report)
	}
}

func TestWSST354ShapeProofIgnoresStructuredToolOutputContentMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "t354-structured-output-content.frames.jsonl")
	writeJSONLFile(t, path,
		wssT354TestSequencedFrame("client_to_server", wssT354TestObjectOutputRequest("resp-before", "call_object", "content-original"), false, 51),
		wssT354TestSequencedFrame("client_to_server", wssT354TestObjectOutputRequest("resp-before", "call_object", "content-mutated"), true, 51),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-mutated"), false),
		wssT354TestFrame("client_to_server", wssT354TestUserDeltaRequest("resp-mutated"), false),
		wssT354TestFrame("server_to_client", wssT354TestCompleted("resp-following"), false),
	)

	report, err := loadWSST354ShapeProofReport(wssT354ShapeProofFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed ||
		report.Totals.MetadataComparisons != 1 ||
		report.Totals.MetadataMismatches != 0 ||
		report.Totals.CandidatesPassing != 1 {
		t.Fatalf("structured tool output content must not be treated as hidden metadata: %+v", report)
	}
}

func wssT354TestFrame(direction string, payload any, mutated bool) map[string]any {
	rec := wssABReplayTestRecord(direction, payload)
	if mutated {
		rec["mutated"] = true
	}
	return rec
}

func wssT354TestSequencedFrame(direction string, payload any, mutated bool, sequence int64) map[string]any {
	rec := wssT354TestFrame(direction, payload, mutated)
	rec["sequence"] = sequence
	return rec
}

func wssT354TestToolOutputRequest(previousResponseID, callID string) map[string]any {
	return wssT354TestToolOutputRequestLines(previousResponseID, callID, 80)
}

func wssT354TestToolOutputRequestLines(previousResponseID, callID string, lines int) map[string]any {
	return map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": previousResponseID,
		"prompt_cache_key":     "t354-shape-proof-test",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  strings.Repeat("stable proof output line\n", lines),
		}},
		"stream": true,
	}
}

func wssT354TestFullHistoryToolOutputRequestLines(callID string, lines int) map[string]any {
	return map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "t354-shape-proof-test",
		"input": []map[string]any{
			{"type": "message", "role": "assistant", "content": "history"},
			{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "rg -n needle internal"}},
			{"type": "function_call_output", "call_id": callID, "output": strings.Repeat("stable proof output line\n", lines)},
		},
		"stream": true,
	}
}

func wssT354TestObjectOutputRequest(previousResponseID, callID, contentID string) map[string]any {
	return map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": previousResponseID,
		"prompt_cache_key":     "t354-shape-proof-test",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": callID,
			"output": map[string]any{
				"id":     contentID,
				"type":   "content",
				"status": "ok",
			},
		}},
		"stream": true,
	}
}

func wssT354TestUserDeltaRequest(previousResponseID string) map[string]any {
	return map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": previousResponseID,
		"prompt_cache_key":     "t354-shape-proof-test",
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
		"stream": true,
	}
}

func wssT354TestCompleted(responseID string) map[string]any {
	return map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": responseID,
			"usage": map[string]any{
				"input_tokens": 1000,
				"input_tokens_details": map[string]any{
					"cached_tokens": 300,
				},
				"output_tokens": 12,
			},
		},
	}
}

func wssT354TestOutputItemDone(itemID, callID string) map[string]any {
	return map[string]any{
		"type":    "response.output_item.done",
		"item_id": itemID,
		"item": map[string]any{
			"type":    "function_call",
			"id":      itemID,
			"call_id": callID,
		},
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

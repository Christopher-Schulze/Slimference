package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

func TestWSSAuditReport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:              "wss-1",
			Timestamp:              time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
			SessionID:              "codex-wss:s1",
			Path:                   "/backend-api/codex/responses",
			RouteMode:              "websocket_phasef",
			ClientFamily:           "codex_cli",
			PreviousResponseIDUsed: true,
			ReReadCount:            2,
			Tokens:                 dbg.TokenCounts{Original: 120, Final: 80, Saved: 40},
			NetSavedTokens:         35,
			CacheReadTokens:        11,
			CacheCreateTokens:      3,
			ProviderInputTokens:    90,
			ProviderCachedTokens:   20,
			ProviderOutputTokens:   7,
			Plan:                   &dbg.PlanSummary{ContentClasses: []string{"tool_output", "repeated_tool_output"}},
			EvidenceDecisions: []evidence.BlockDecision{
				{
					Mechanism:            "stale_read",
					Action:               evidence.ActionApplied,
					Reason:               "positive_net_savings",
					OriginalTokens:       100,
					FinalTokens:          40,
					SavedTokens:          60,
					NetTokens:            60,
					FootprintScore:       600,
					FootprintScoreBucket: "high",
					CacheImpact:          "provider_cache_read",
				},
				{
					Mechanism:   "obsolete_prune",
					Action:      evidence.ActionFullPass,
					Reason:      "cache_bust_guard",
					CacheImpact: "cache_bust_guard",
				},
			},
			DebugFacts: map[string]string{
				"wss.request_shape":                                   "full_history",
				"wss.turn_seq":                                        "2",
				"wss.remaining_turns_estimate":                        "70",
				"wss.socket_seq":                                      "2",
				"wss.socket_close_initiator":                          "client_eof",
				"wss.full_history_detached_previous_response":         "true",
				"wss.full_history_stateless_followup":                 "true",
				"wss.effective_mutation_guard":                        "wss_full_history_downstream_delta_proof_gate",
				"wss.cache_bust_demoted_mechanisms":                   "stale_read",
				"wss.cache_bust_demoted_scope":                        "wss_phasef|full_history|prefix-a",
				"wss.cache_bust_demoted_class_keys":                   "stale_read:git_status",
				"wss.shadow_mirror_blocks":                            "2",
				"wss.shadow_mirror_bytes":                             "1000",
				"wss.shadow_mirror_referenceable_blocks":              "1",
				"wss.shadow_mirror_referenceable_bytes":               "400",
				"wss.shadow_mirror_normalized_segments":               "2",
				"wss.shadow_mirror_normalized_bytes":                  "800",
				"wss.shadow_mirror_normalized_referenceable_segments": "1",
				"wss.shadow_mirror_normalized_referenceable_bytes":    "300",
				"wss.shadow_mirror_normalized_density_by_kind":        "codex_exec_payload=300/500/1/1,tool_result=0/300/0/1",
			},
		},
		dbg.RequestSummary{
			RequestID: "http-1",
			Path:      "/v1/chat/completions",
			RouteMode: "http",
		},
		dbg.RequestSummary{
			RequestID:            "wss-2",
			Path:                 "/backend-api/codex/responses",
			RouteMode:            "websocket_phasef",
			ProviderInputTokens:  50,
			ProviderCachedTokens: 25,
			ProviderOutputTokens: 2,
			CacheReadTokens:      4,
			CacheCreateTokens:    1,
			Tokens:               dbg.TokenCounts{Original: 60, Final: 54, Saved: 6},
			NetSavedTokens:       5,
			Plan:                 &dbg.PlanSummary{ContentClasses: []string{"tool_output"}},
			DebugFacts: map[string]string{
				"wss.request_shape": "delta",
			},
		},
	)

	report, err := loadWSSAuditReport(wssAuditFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.Requests != 3 || report.WSSRequests != 2 || report.PhaseFRequests != 2 {
		t.Fatalf("bad request counts: %+v", report)
	}
	if report.UniqueSessions != 1 || report.MissingSessionID != 1 || report.PreviousResponseIDUsed != 1 ||
		report.PositiveSavings != 2 || report.TokensSaved != 46 {
		t.Fatalf("bad WSS counters: %+v", report)
	}
	if report.ReReadRequests != 1 || report.ReReadCount != 2 {
		t.Fatalf("bad re-read counters: %+v", report)
	}
	if report.RequestShapes["full_history"] != 1 || report.RequestShapes["delta"] != 1 {
		t.Fatalf("bad request shape counts: %+v", report.RequestShapes)
	}
	if report.ResolvedRequestShapes["full_history"] != 1 ||
		report.ResolvedRequestShapes["delta"] != 1 ||
		report.RequestShapeSources["fact"] != 2 {
		t.Fatalf("bad resolved request shape counts: shapes=%+v sources=%+v", report.ResolvedRequestShapes, report.RequestShapeSources)
	}
	if report.FullHistory == nil ||
		report.FullHistory.Requests != 1 ||
		report.FullHistory.Sessions != 1 ||
		report.FullHistory.MissingSessionID != 0 ||
		report.FullHistory.PreviousResponseIDUsed != 1 ||
		report.FullHistory.ProviderInputTokens != 90 ||
		report.FullHistory.ProviderCachedTokens != 20 ||
		report.FullHistory.ProviderOutputTokens != 7 ||
		report.FullHistory.CacheReadTokens != 11 ||
		report.FullHistory.CacheCreateTokens != 3 ||
		report.FullHistory.OriginalTokens != 120 ||
		report.FullHistory.FinalTokens != 80 ||
		report.FullHistory.SavedTokens != 40 ||
		report.FullHistory.NetSavedTokens != 35 ||
		report.FullHistory.ByClientFamily["codex_cli"] != 1 ||
		report.FullHistory.BySocketSeq["2"] != 1 ||
		report.FullHistory.BySocketCloseInitiator["client_eof"] != 1 {
		t.Fatalf("bad full-history Class-B report: %+v", report.FullHistory)
	}
	if report.ShadowMirror == nil ||
		report.ShadowMirror.ReferenceableBytes != 400 ||
		report.ShadowMirror.ReferenceableBytePct != 40 ||
		report.ShadowMirror.NormalizedReferenceableBytes != 300 ||
		report.ShadowMirror.NormalizedReferenceableBytePct != 37.5 ||
		len(report.ShadowMirror.NormalizedReferenceableBytesByKind) != 2 {
		t.Fatalf("bad shadow mirror report: %+v", report.ShadowMirror)
	}
	if got := report.ShadowMirror.NormalizedReferenceableBytesByKind[0]; got.Kind != "codex_exec_payload" || got.ReferenceableBytes != 300 || got.Bytes != 500 || got.ReferenceableBytePct != 60 {
		t.Fatalf("bad shadow mirror kind row: %+v", got)
	}
	if len(report.ShadowMirrorCandidates) != 2 {
		t.Fatalf("shadow mirror candidates = %d, want 2: %+v", len(report.ShadowMirrorCandidates), report.ShadowMirrorCandidates)
	}
	if got := report.ShadowMirrorCandidates[0]; got.RequestShape != "full_history" ||
		got.Kind != "exact_block" ||
		got.ReferenceableBytes != 400 ||
		got.Bytes != 1000 ||
		got.ReferenceableBytePct != 40 ||
		got.ProviderInputTokens != 90 ||
		got.ProviderCachedTokens != 20 ||
		got.ProviderOutputTokens != 7 ||
		got.PreviousResponseIDUsed != 1 ||
		got.DetachedPreviousResponse != 1 ||
		got.FullHistoryStatelessFollowup != 1 ||
		got.CacheBustDemoted != 1 ||
		got.EffectiveMutationGuards["wss_full_history_downstream_delta_proof_gate"] != 1 ||
		got.BySocketSeq["2"] != 1 ||
		got.CandidateLane != "t417_class_b_server_state" ||
		got.PromotionStage != "t417_lineage_candidate_needs_engineering" ||
		!stringSliceContains(got.PromotionBlockers, "mixed_previous_response_state_requires_exact_lineage_split") ||
		!stringSliceContains(got.PromotionBlockers, "cache_bust_demotion_present_exact_class_scope") ||
		got.PromotionBlockerHeadroom["mixed_previous_response_state_requires_exact_lineage_split"] != got.IncrementalLocalTokensHeadroom ||
		got.PromotionBlockerHeadroom["cache_bust_demotion_present_exact_class_scope"] != got.IncrementalLocalTokensHeadroom ||
		got.CacheBustDemotedScopes["wss_phasef|full_history|prefix-a"] != 1 ||
		got.CacheBustDemotedClassKeys["stale_read:git_status"] != 1 {
		t.Fatalf("bad top shadow mirror candidate: %+v", got)
	}
	if got := report.ShadowMirrorCandidates[0].TopSessions; len(got) != 1 ||
		got[0].ProviderInputTokens != 90 ||
		got[0].ProviderCachedTokens != 20 ||
		got[0].PreviousResponseIDUsed != 1 ||
		got[0].DetachedPreviousResponse != 1 ||
		got[0].FullHistoryStatelessFollowup != 1 ||
		got[0].CacheBustDemoted != 1 ||
		got[0].EffectiveMutationGuards["wss_full_history_downstream_delta_proof_gate"] != 1 ||
		got[0].BySocketSeq["2"] != 1 ||
		got[0].PromotionStage != "t417_lineage_candidate_needs_engineering" ||
		!stringSliceContains(got[0].PromotionBlockers, "mixed_previous_response_state_requires_exact_lineage_split") ||
		!stringSliceContains(got[0].PromotionBlockers, "cache_bust_demotion_present_exact_class_scope") ||
		got[0].PromotionBlockerHeadroom["mixed_previous_response_state_requires_exact_lineage_split"] != got[0].IncrementalLocalTokensHeadroom ||
		got[0].PromotionBlockerHeadroom["cache_bust_demotion_present_exact_class_scope"] != got[0].IncrementalLocalTokensHeadroom ||
		got[0].CacheBustDemotedScopes["wss_phasef|full_history|prefix-a"] != 1 ||
		got[0].CacheBustDemotedClassKeys["stale_read:git_status"] != 1 {
		t.Fatalf("bad top shadow mirror candidate sessions: %+v", got)
	}
	if got := report.ShadowMirrorCandidates[1]; got.RequestShape != "full_history" ||
		got.Kind != "codex_exec_payload" ||
		got.ReferenceableBytes != 300 ||
		got.Bytes != 500 ||
		!strings.Contains(got.RecommendedAction, "T417") {
		t.Fatalf("bad normalized shadow mirror candidate: %+v", got)
	}
	if report.ContentClasses["tool_output"] != 2 || report.ContentClasses["repeated_tool_output"] != 1 {
		t.Fatalf("bad content classes: %+v", report.ContentClasses)
	}
	if len(report.ShapeEconomics) != 2 {
		t.Fatalf("shape economics rows = %d, want 2: %+v", len(report.ShapeEconomics), report.ShapeEconomics)
	}
	shapeEconomics := map[string]wssAuditShapeEconomicsSummary{}
	for _, row := range report.ShapeEconomics {
		shapeEconomics[row.Shape] = row
	}
	if full := shapeEconomics["full_history"]; full.Requests != 1 ||
		full.Sources["fact"] != 1 ||
		full.ProviderInputTokens != 90 ||
		full.ProviderCachedTokens != 20 ||
		full.ProviderCachedPct != 20.0/90.0*100 ||
		full.ProviderOutputTokens != 7 ||
		full.CacheReadTokens != 11 ||
		full.CacheCreateTokens != 3 ||
		full.OriginalTokens != 120 ||
		full.FinalTokens != 80 ||
		full.LocalSavedTokens != 40 ||
		full.NetSavedTokens != 35 ||
		full.LocalSavedPct != 40.0/120.0*100 {
		t.Fatalf("bad full-history shape economics: %+v", full)
	}
	if delta := shapeEconomics["delta"]; delta.Requests != 1 ||
		delta.Sources["fact"] != 1 ||
		delta.ProviderInputTokens != 50 ||
		delta.ProviderCachedTokens != 25 ||
		delta.ProviderCachedPct != 50 ||
		delta.ProviderOutputTokens != 2 ||
		delta.CacheReadTokens != 4 ||
		delta.CacheCreateTokens != 1 ||
		delta.OriginalTokens != 60 ||
		delta.FinalTokens != 54 ||
		delta.LocalSavedTokens != 6 ||
		delta.NetSavedTokens != 5 ||
		delta.LocalSavedPct != 10 {
		t.Fatalf("bad delta shape economics: %+v", delta)
	}
	if len(report.HistoryReducers) != 2 {
		t.Fatalf("history reducer count = %d, want 2: %+v", len(report.HistoryReducers), report.HistoryReducers)
	}
	history := map[string]wssHistoryReducerSummary{}
	for _, row := range report.HistoryReducers {
		history[row.Mechanism] = row
	}
	if stale := history["stale_read"]; stale.Decisions != 1 ||
		stale.Applied != 1 ||
		stale.SavedTokens != 60 ||
		stale.NetTokens != 60 ||
		stale.FootprintScore != 600 ||
		stale.Reasons["positive_net_savings"] != 1 ||
		stale.FootprintBuckets["high"] != 1 {
		t.Fatalf("bad stale_read history row: %+v", stale)
	}
	if len(report.FootprintEconomics) != 1 {
		t.Fatalf("footprint economics count = %d, want 1: %+v", len(report.FootprintEconomics), report.FootprintEconomics)
	}
	footprint := report.FootprintEconomics[0]
	if footprint.Bucket != "high" ||
		footprint.TurnBand != "turn_1_3" ||
		footprint.RequestShape != "full_history" ||
		footprint.Decisions != 1 ||
		footprint.Applied != 1 ||
		footprint.SavedTokens != 60 ||
		footprint.NetTokens != 60 ||
		footprint.FootprintScore != 600 ||
		footprint.Mechanisms["stale_read"] != 1 ||
		footprint.CacheImpact["provider_cache_read"] != 1 {
		t.Fatalf("bad footprint economics row: %+v", footprint)
	}
	if report.FootprintCoverage == nil ||
		report.FootprintCoverage.TokenDecisions != 1 ||
		report.FootprintCoverage.AppliedTokenDecisions != 1 ||
		report.FootprintCoverage.WithFootprint != 1 ||
		report.FootprintCoverage.MissingFootprint != 0 ||
		report.FootprintCoverage.WithRemainingTurnsEstimate != 1 ||
		report.FootprintCoverage.MissingRemainingTurnsEstimate != 0 ||
		report.FootprintCoverage.SavedTokens != 60 ||
		report.FootprintCoverage.ByMechanism["stale_read"] != 1 {
		t.Fatalf("bad footprint coverage: %+v", report.FootprintCoverage)
	}
	if obsolete := history["obsolete_prune"]; obsolete.Decisions != 1 ||
		obsolete.FullPass != 1 ||
		obsolete.Reasons["cache_bust_guard"] != 1 ||
		obsolete.CacheImpact["cache_bust_guard"] != 1 {
		t.Fatalf("bad obsolete_prune history row: %+v", obsolete)
	}
	if len(report.Sessions) != 2 {
		t.Fatalf("session count = %d, want 2: %+v", len(report.Sessions), report.Sessions)
	}
	foundMissing := false
	for _, session := range report.Sessions {
		foundMissing = foundMissing || session.SessionID == "(missing)"
	}
	if !foundMissing {
		t.Fatalf("missing session bucket not present: %+v", report.Sessions)
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "session id") || !strings.Contains(notes, "re-read canary") {
		t.Fatalf("expected session/re-read notes, got %+v", report.Notes)
	}
}

func TestWSSAuditShadowMirrorPromotionOpenSlice(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:              "wss-open-1",
			Timestamp:              time.Date(2026, 5, 30, 12, 5, 0, 0, time.UTC),
			SessionID:              "codex-wss:open-slice",
			Path:                   "/backend-api/codex/responses",
			RouteMode:              "websocket_phasef",
			ClientFamily:           "codex_cli",
			PreviousResponseIDUsed: false,
			Tokens:                 dbg.TokenCounts{Original: 220, Final: 180, Saved: 40},
			ProviderInputTokens:    220,
			ProviderCachedTokens:   120,
			ProviderOutputTokens:   8,
			DebugFacts: map[string]string{
				"wss.request_shape": "full_history",
				"wss.socket_seq":    "1",
				"wss.full_history_detached_previous_response":  "true",
				"wss.full_history_stateless_followup":          "true",
				"wss.shadow_mirror_normalized_density_by_kind": "stateful_safe_tool_output=400/800/1/1",
			},
		},
	)

	report, err := loadWSSAuditReport(wssAuditFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if len(report.ShadowMirrorCandidates) != 1 {
		t.Fatalf("shadow mirror candidates = %d, want 1: %+v", len(report.ShadowMirrorCandidates), report.ShadowMirrorCandidates)
	}
	candidate := report.ShadowMirrorCandidates[0]
	if candidate.Kind != "stateful_safe_tool_output" ||
		candidate.PromotionStage != "product_candidate_no_observed_blockers" ||
		!candidate.PromotionOpenReady ||
		candidate.PromotionOpenStage != "t417_exact_scope_open_slice_candidate" ||
		len(candidate.PromotionOpenBlockers) != 0 ||
		len(candidate.PromotionOpenBlockerHeadroom) != 0 ||
		candidate.PromotionOpenRequests != 1 ||
		candidate.PromotionOpenHeadroom <= 0 {
		t.Fatalf("bad open-slice candidate: %+v", candidate)
	}
	if len(candidate.TopSessions) != 1 {
		t.Fatalf("top sessions = %d, want 1: %+v", len(candidate.TopSessions), candidate.TopSessions)
	}
	session := candidate.TopSessions[0]
	if !session.PromotionOpenReady ||
		session.PromotionOpenStage != "t417_exact_scope_open_slice_candidate" ||
		len(session.PromotionOpenBlockers) != 0 ||
		len(session.PromotionOpenBlockerHeadroom) != 0 ||
		session.PromotionOpenRequests != 1 ||
		session.PromotionOpenHeadroom != candidate.PromotionOpenHeadroom {
		t.Fatalf("bad open-slice session: %+v", session)
	}
}

func TestWSSShadowMirrorCandidateActionNamesCommandFamily(t *testing.T) {
	t.Parallel()

	action := wssShadowMirrorCandidateAction("full_history", "codex_exec_payload_command_git")
	if !strings.Contains(action, "exact command family") ||
		!strings.Contains(action, "T417 Class-B continuation") ||
		!strings.Contains(action, "T418 command-output-first") {
		t.Fatalf("command-family action should name T417/T418 ranking path, got %q", action)
	}
	action = wssShadowMirrorCandidateAction("full_history", "tool_result_command_git")
	if !strings.Contains(action, "resolved tool-result command family") ||
		!strings.Contains(action, "T417 Class-B continuation") ||
		!strings.Contains(action, "T418 command-output-first") {
		t.Fatalf("tool-result command-family action should name T417/T418 ranking path, got %q", action)
	}
}

func TestWSSAuditShadowMirrorStatefulSafeCommandClassIsPromotionOpen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:              "wss-stateful-safe-class",
			Timestamp:              time.Date(2026, 5, 30, 12, 5, 0, 0, time.UTC),
			SessionID:              "codex-wss:stateful-safe-class",
			Path:                   "/backend-api/codex/responses",
			RouteMode:              "websocket_phasef",
			ClientFamily:           "codex_cli",
			PreviousResponseIDUsed: false,
			Tokens:                 dbg.TokenCounts{Original: 220, Final: 180, Saved: 40},
			ProviderInputTokens:    220,
			ProviderCachedTokens:   120,
			ProviderOutputTokens:   8,
			DebugFacts: map[string]string{
				"wss.request_shape": "full_history",
				"wss.socket_seq":    "1",
				"wss.full_history_detached_previous_response":     "true",
				"wss.full_history_stateless_followup":             "true",
				"wss.shadow_mirror_normalized_density_by_kind":    "tool_result_command_git=400/800/1/1",
				"wss.shadow_mirror_stateful_safe_density_by_kind": "stateful_safe_tool_output_git_status=400/400/1/1",
			},
		},
	)

	report, err := loadWSSAuditReport(wssAuditFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	var safe *wssShadowMirrorCandidate
	var generic *wssShadowMirrorCandidate
	for i := range report.ShadowMirrorCandidates {
		switch report.ShadowMirrorCandidates[i].Kind {
		case "stateful_safe_tool_output_git_status":
			safe = &report.ShadowMirrorCandidates[i]
		case "tool_result_command_git":
			generic = &report.ShadowMirrorCandidates[i]
		}
	}
	if safe == nil || !safe.PromotionOpenReady || safe.PromotionOpenStage != "t417_exact_scope_open_slice_candidate" || len(safe.PromotionOpenBlockers) != 0 {
		t.Fatalf("stateful-safe command class should be promotion-open: %+v", report.ShadowMirrorCandidates)
	}
	if generic == nil || generic.PromotionOpenReady || !stringSliceContains(generic.PromotionOpenBlockers, "reference_only_backend_contract_required") {
		t.Fatalf("generic command-family row must stay reference-only: %+v", report.ShadowMirrorCandidates)
	}
}

func TestWSSAuditShadowMirrorReferenceOnlyHeadroomIsNotPromotionOpen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:           "wss-reference-only",
			Timestamp:           time.Date(2026, 5, 30, 12, 6, 0, 0, time.UTC),
			SessionID:           "codex-wss:reference-only",
			Path:                "/backend-api/codex/responses",
			RouteMode:           "websocket_phasef",
			ClientFamily:        "codex_cli",
			Tokens:              dbg.TokenCounts{Original: 220, Final: 180, Saved: 40},
			ProviderInputTokens: 220,
			DebugFacts: map[string]string{
				"wss.request_shape": "full_history",
				"wss.socket_seq":    "1",
				"wss.full_history_detached_previous_response":  "true",
				"wss.full_history_stateless_followup":          "true",
				"wss.shadow_mirror_blocks":                     "1",
				"wss.shadow_mirror_bytes":                      "800",
				"wss.shadow_mirror_referenceable_blocks":       "1",
				"wss.shadow_mirror_referenceable_bytes":        "400",
				"wss.shadow_mirror_normalized_density_by_kind": "codex_exec_payload=300/700/1/1",
			},
		},
	)

	report, err := loadWSSAuditReport(wssAuditFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if len(report.ShadowMirrorCandidates) != 2 {
		t.Fatalf("shadow mirror candidates = %d, want 2: %+v", len(report.ShadowMirrorCandidates), report.ShadowMirrorCandidates)
	}
	for _, candidate := range report.ShadowMirrorCandidates {
		if candidate.PromotionOpenHeadroom <= 0 ||
			candidate.PromotionOpenReady ||
			candidate.PromotionOpenStage != "t417_no_open_slice_candidate" ||
			!stringSliceContains(candidate.PromotionOpenBlockers, "reference_only_backend_contract_required") ||
			!stringSliceContains(candidate.PromotionBlockers, "reference_only_backend_contract_required") ||
			candidate.PromotionBlockerHeadroom["reference_only_backend_contract_required"] != candidate.IncrementalLocalTokensHeadroom {
			t.Fatalf("reference-only candidate must keep headroom but fail product open: %+v", candidate)
		}
		if len(candidate.TopSessions) != 1 ||
			candidate.TopSessions[0].PromotionOpenReady ||
			!stringSliceContains(candidate.TopSessions[0].PromotionOpenBlockers, "reference_only_backend_contract_required") {
			t.Fatalf("reference-only session must fail product open: %+v", candidate.TopSessions)
		}
	}
}

func TestWSSAuditShadowMirrorBlockerHeadroomCountsOnlyResidualBlockedMass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:           "wss-open",
			SessionID:           "codex-wss:partial",
			Path:                "/backend-api/codex/responses",
			RouteMode:           "websocket_phasef",
			Tokens:              dbg.TokenCounts{Original: 220, Final: 180, Saved: 40},
			ProviderInputTokens: 220,
			DebugFacts: map[string]string{
				"wss.request_shape": "full_history",
				"wss.socket_seq":    "1",
				"wss.full_history_detached_previous_response":  "true",
				"wss.full_history_stateless_followup":          "true",
				"wss.shadow_mirror_normalized_density_by_kind": "stateful_safe_tool_output=400/400/1/1",
			},
		},
		dbg.RequestSummary{
			RequestID:           "wss-blocked",
			SessionID:           "codex-wss:partial",
			Path:                "/backend-api/codex/responses",
			RouteMode:           "websocket_phasef",
			Tokens:              dbg.TokenCounts{Original: 320, Final: 240, Saved: 80},
			ProviderInputTokens: 320,
			DebugFacts: map[string]string{
				"wss.request_shape": "full_history",
				"wss.socket_seq":    "1",
				"wss.full_history_detached_previous_response":  "true",
				"wss.full_history_stateless_followup":          "true",
				"wss.cache_bust_demoted_mechanisms":            "stale_read",
				"wss.cache_bust_demoted_scope":                 "route=wss_phasef|shape=full_history|prompt_cache_key=partial",
				"wss.cache_bust_demoted_class_keys":            "stale_read:unknown",
				"wss.shadow_mirror_normalized_density_by_kind": "stateful_safe_tool_output=800/800/1/1",
			},
		},
	)

	report, err := loadWSSAuditReport(wssAuditFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if len(report.ShadowMirrorCandidates) != 1 {
		t.Fatalf("shadow mirror candidates = %d, want 1: %+v", len(report.ShadowMirrorCandidates), report.ShadowMirrorCandidates)
	}
	candidate := report.ShadowMirrorCandidates[0]
	wantBlocked := candidate.IncrementalLocalTokensHeadroom - candidate.PromotionOpenHeadroom
	if wantBlocked <= 0 {
		t.Fatalf("test fixture should have residual blocked headroom: %+v", candidate)
	}
	if candidate.PromotionStage != "t417_partial_product_candidate" ||
		candidate.PromotionOpenStage != "t417_exact_scope_open_slice_candidate" ||
		candidate.PromotionBlockerHeadroom["cache_bust_demotion_present_exact_class_scope"] != wantBlocked {
		t.Fatalf("candidate blocker headroom should be residual blocked mass only, want %d: %+v", wantBlocked, candidate)
	}
	if len(candidate.TopSessions) != 1 {
		t.Fatalf("top sessions = %d, want 1: %+v", len(candidate.TopSessions), candidate.TopSessions)
	}
	session := candidate.TopSessions[0]
	wantSessionBlocked := session.IncrementalLocalTokensHeadroom - session.PromotionOpenHeadroom
	if session.PromotionStage != "t417_partial_product_candidate" ||
		session.PromotionBlockerHeadroom["cache_bust_demotion_present_exact_class_scope"] != wantSessionBlocked {
		t.Fatalf("session blocker headroom should be residual blocked mass only, want %d: %+v", wantSessionBlocked, session)
	}
}

func TestWSSAuditFootprintCoverageReportsMissingTokenEvidence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "legacy-positive",
		SessionID: "codex-wss:s1",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:      "read_delta",
			Action:         evidence.ActionApplied,
			Reason:         "positive_net_savings",
			OriginalTokens: 1000,
			FinalTokens:    200,
			SavedTokens:    800,
			NetTokens:      800,
		}, {
			Mechanism: "captured_output",
			Action:    evidence.ActionFullPass,
			Reason:    "latency_budget_full_context",
		}},
		DebugFacts: map[string]string{
			"wss.request_shape": "delta",
		},
	})

	report, err := loadWSSAuditReport(wssAuditFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.FootprintCoverage == nil ||
		report.FootprintCoverage.TokenDecisions != 1 ||
		report.FootprintCoverage.AppliedTokenDecisions != 1 ||
		report.FootprintCoverage.WithFootprint != 0 ||
		report.FootprintCoverage.MissingFootprint != 1 ||
		report.FootprintCoverage.AppliedMissingFootprint != 1 ||
		report.FootprintCoverage.SavedTokens != 800 ||
		report.FootprintCoverage.MissingSavedTokens != 800 ||
		report.FootprintCoverage.MissingByMechanism["read_delta"] != 1 {
		t.Fatalf("bad missing footprint coverage: %+v", report.FootprintCoverage)
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "without footprint_score_bucket") {
		t.Fatalf("missing footprint note absent: %+v", report.Notes)
	}

	var stdout, stderr bytes.Buffer
	if code := runWSSAudit([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Footprint coverage:") ||
		!strings.Contains(stdout.String(), "missing footprint:       1") ||
		!strings.Contains(stdout.String(), "missing mechanisms:      read_delta:1") {
		t.Fatalf("text output missing footprint coverage:\n%s", stdout.String())
	}
}

func TestWSSAuditFootprintGateRequiresRemainingTurnsEstimate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "footprint-without-remaining",
		SessionID: "codex-wss:s1",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:            "stale_read",
			Action:               evidence.ActionApplied,
			Reason:               "positive_net_savings",
			OriginalTokens:       1000,
			FinalTokens:          200,
			SavedTokens:          800,
			NetTokens:            800,
			FootprintScore:       640,
			FootprintScoreBucket: "high",
		}},
		DebugFacts: map[string]string{
			"wss.request_shape": "full_history",
			"wss.turn_seq":      "2",
		},
	})

	report, err := loadWSSAuditReport(wssAuditFlags{
		path:                     path,
		requireFootprintEvidence: true,
	})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.GatePassed ||
		report.FootprintCoverage == nil ||
		report.FootprintCoverage.WithFootprint != 1 ||
		report.FootprintCoverage.MissingRemainingTurnsEstimate != 1 ||
		report.FootprintCoverage.AppliedMissingRemainingTurnsEstimate != 1 ||
		report.FootprintCoverage.MissingRemainingTurnsEstimateMechanism["stale_read"] != 1 {
		t.Fatalf("expected missing remaining-turn gate evidence, report=%+v coverage=%+v", report, report.FootprintCoverage)
	}
	failures := strings.Join(report.GateFailures, "\n")
	if !strings.Contains(failures, "wss.remaining_turns_estimate") {
		t.Fatalf("missing remaining-turn gate failure: %+v", report.GateFailures)
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "pre-EMA") {
		t.Fatalf("missing remaining-turn note: %+v", report.Notes)
	}

	var stdout, stderr bytes.Buffer
	if code := runWSSAudit([]string{path, "--require-footprint-evidence"}, &stdout, &stderr); code != 3 {
		t.Fatalf("runWSSAudit code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "missing remaining-turn:  1") ||
		!strings.Contains(stdout.String(), "wss.remaining_turns_estimate") {
		t.Fatalf("text output missing remaining-turn evidence:\n%s", stdout.String())
	}
}

func TestWSSAuditGateFailures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "wss-1",
		SessionID: "codex-wss:s1",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
		DebugFacts: map[string]string{
			"wss.request_shape": "delta",
		},
	})

	report, err := loadWSSAuditReport(wssAuditFlags{
		path:                     path,
		expectDistinctSessions:   2,
		minPhaseF:                2,
		minFullHistory:           1,
		requireSavings:           true,
		requireFootprintEvidence: true,
	})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.GatePassed || len(report.GateFailures) != 5 {
		t.Fatalf("expected five gate failures, got passed=%v failures=%+v", report.GatePassed, report.GateFailures)
	}
	failures := strings.Join(report.GateFailures, "\n")
	if !strings.Contains(failures, "full-history") ||
		!strings.Contains(failures, "footprint-score economics") {
		t.Fatalf("expected full-history gate failure, got %+v", report.GateFailures)
	}
}

func TestWSSAuditResolvesLegacyUnknownDeltaOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID:              "legacy-delta",
			SessionID:              "codex-wss:s1",
			Path:                   "/backend-api/codex/responses",
			RouteMode:              "websocket_phasef",
			PreviousResponseIDUsed: true,
		},
		dbg.RequestSummary{
			RequestID: "legacy-no-prev",
			SessionID: "codex-wss:s1",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
		},
		dbg.RequestSummary{
			RequestID: "legacy-delta-fact",
			SessionID: "codex-wss:s2",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			DebugFacts: map[string]string{
				"wss.previous_response_id": "true",
			},
		},
		dbg.RequestSummary{
			RequestID: "observed-root",
			SessionID: "codex-wss:s2",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			DebugFacts: map[string]string{
				"wss.request_shape": "root",
			},
		},
	)

	report, err := loadWSSAuditReport(wssAuditFlags{path: path})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.RequestShapes["unknown"] != 3 || report.RequestShapes["root"] != 1 {
		t.Fatalf("observed shapes should preserve unknown legacy rows: %+v", report.RequestShapes)
	}
	if report.ResolvedRequestShapes["delta"] != 2 ||
		report.ResolvedRequestShapes["root"] != 1 ||
		report.ResolvedRequestShapes["unknown"] != 1 {
		t.Fatalf("bad resolved shapes: %+v", report.ResolvedRequestShapes)
	}
	if report.RequestShapeSources["legacy_previous_response_id"] != 1 ||
		report.RequestShapeSources["legacy_previous_response_id_fact"] != 1 ||
		report.RequestShapeSources["fact"] != 1 ||
		report.RequestShapeSources["unresolved"] != 1 {
		t.Fatalf("bad shape sources: %+v", report.RequestShapeSources)
	}
	var s1 wssAuditSessionSummary
	for _, session := range report.Sessions {
		if session.SessionID == "codex-wss:s1" {
			s1 = session
		}
	}
	if s1.ResolvedRequestShapes["delta"] != 1 || s1.ResolvedRequestShapes["unknown"] != 1 {
		t.Fatalf("session resolved shapes missing: %+v", s1)
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "conservatively resolved as delta") ||
		!strings.Contains(notes, "remain shape-unresolved") {
		t.Fatalf("legacy inference notes missing: %+v", report.Notes)
	}
}

func TestWSSAuditHistoryEvidenceGate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "wss-1",
		SessionID: "codex-wss:s1",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
	})

	report, err := loadWSSAuditReport(wssAuditFlags{
		path:                   path,
		requireHistoryEvidence: true,
	})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.GatePassed || len(report.GateFailures) != 1 ||
		!strings.Contains(report.GateFailures[0], "history reducer evidence") {
		t.Fatalf("expected history evidence gate failure, got passed=%v failures=%+v", report.GatePassed, report.GateFailures)
	}
}

func TestWSSAuditSinceFiltersOldRecords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path,
		dbg.RequestSummary{
			RequestID: "old",
			Timestamp: time.Date(2026, 5, 30, 11, 0, 0, 0, time.UTC),
			SessionID: "codex-wss:old",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Saved: 3},
		},
		dbg.RequestSummary{
			RequestID: "new",
			Timestamp: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
			SessionID: "codex-wss:new",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
		},
		dbg.RequestSummary{
			RequestID: "untimed",
			SessionID: "codex-wss:untimed",
			Path:      "/backend-api/codex/responses",
			RouteMode: "websocket_phasef",
			Tokens:    dbg.TokenCounts{Saved: 99},
		},
	)

	report, err := loadWSSAuditReport(wssAuditFlags{
		path:  path,
		since: time.Date(2026, 5, 30, 11, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("loadWSSAuditReport() error = %v", err)
	}
	if report.Requests != 1 || report.UniqueSessions != 1 || report.Sessions[0].SessionID != "codex-wss:new" || report.TokensSaved != 0 {
		t.Fatalf("since filter failed: %+v", report)
	}

	sincePath := filepath.Join(dir, "since.txt")
	writeFileForLocalArtifactTest(t, sincePath, "2026-05-30T11:30:00Z\n")
	var stdout, stderr bytes.Buffer
	if code := runWSSAudit([]string{path, "--since-file", sincePath, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit --since-file code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var cliReport wssAuditReport
	if err := json.Unmarshal(stdout.Bytes(), &cliReport); err != nil {
		t.Fatalf("parse --since-file JSON: %v\n%s", err, stdout.String())
	}
	if cliReport.Requests != 1 || cliReport.UniqueSessions != 1 ||
		cliReport.Sessions[0].SessionID != "codex-wss:new" ||
		cliReport.SinceFile != sincePath ||
		cliReport.Since == nil ||
		!cliReport.Since.Equal(time.Date(2026, 5, 30, 11, 30, 0, 0, time.UTC)) {
		t.Fatalf("--since-file filter failed: %+v", cliReport)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runWSSAudit([]string{path, "--since-file=" + sincePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit text --since-file code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "Since file:") || !strings.Contains(stdout.String(), sincePath) {
		t.Fatalf("text output missing since file:\n%s", stdout.String())
	}
}

func TestRunWSSAuditJSONAndText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID:            "wss-1",
		SessionID:            "codex-wss:s1",
		Path:                 "/backend-api/codex/responses",
		RouteMode:            "websocket_phasef",
		Tokens:               dbg.TokenCounts{Original: 30, Final: 27, Saved: 3},
		Errors:               []string{"provider returned 400"},
		BypassReason:         "upstream_error",
		ClientFamily:         "codex_desktop",
		ProviderInputTokens:  44,
		ProviderCachedTokens: 22,
		ProviderOutputTokens: 6,
		CacheReadTokens:      5,
		CacheCreateTokens:    2,
		NetSavedTokens:       1,
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:            "stale_read",
			Action:               evidence.ActionApplied,
			Reason:               "positive_net_savings",
			SavedTokens:          42,
			NetTokens:            42,
			FootprintScore:       84,
			FootprintScoreBucket: "mid",
		}},
		DebugFacts: map[string]string{
			"wss.request_shape":                                   "full_history",
			"wss.turn_seq":                                        "6",
			"wss.remaining_turns_estimate":                        "64",
			"wss.socket_seq":                                      "3",
			"wss.socket_close_initiator":                          "upstream_eof",
			"wss.shadow_mirror_blocks":                            "1",
			"wss.shadow_mirror_bytes":                             "300",
			"wss.shadow_mirror_referenceable_blocks":              "0",
			"wss.shadow_mirror_referenceable_bytes":               "0",
			"wss.shadow_mirror_normalized_segments":               "1",
			"wss.shadow_mirror_normalized_bytes":                  "200",
			"wss.shadow_mirror_normalized_referenceable_segments": "1",
			"wss.shadow_mirror_normalized_referenceable_bytes":    "120",
			"wss.shadow_mirror_normalized_density_by_kind":        "codex_exec_payload=120/200/1/1",
		},
	})

	var stdout, stderr bytes.Buffer
	if code := runWSSAudit([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Phase-F requests:") ||
		!strings.Contains(stdout.String(), "request shapes:") ||
		!strings.Contains(stdout.String(), "resolved request shapes:") ||
		!strings.Contains(stdout.String(), "request-shape sources:") ||
		!strings.Contains(stdout.String(), "full_history:1") ||
		!strings.Contains(stdout.String(), "Shape economics:") ||
		!strings.Contains(stdout.String(), "full_history requests=1 sources=fact:1 provider=44/22/50.00%") ||
		!strings.Contains(stdout.String(), "Full-history Class-B:") ||
		!strings.Contains(stdout.String(), "provider in/cache/out:   44 / 22 / 6") ||
		!strings.Contains(stdout.String(), "errors/upstream/400:     1 / 1 / 1") ||
		!strings.Contains(stdout.String(), "re-read requests/count:") ||
		!strings.Contains(stdout.String(), "History reducers:") ||
		!strings.Contains(stdout.String(), "stale_read") ||
		!strings.Contains(stdout.String(), "Footprint economics:") ||
		!strings.Contains(stdout.String(), "bucket=mid") ||
		!strings.Contains(stdout.String(), "turn=turn_4_8") ||
		!strings.Contains(stdout.String(), "Footprint coverage:") ||
		!strings.Contains(stdout.String(), "Shadow mirror density:") ||
		!strings.Contains(stdout.String(), "Shadow mirror candidates:") ||
		!strings.Contains(stdout.String(), "shape=full_history") ||
		!strings.Contains(stdout.String(), "lane=t417_class_b_server_state") ||
		!strings.Contains(stdout.String(), "candidate_tokens=") ||
		!strings.Contains(stdout.String(), "headroom=") ||
		!strings.Contains(stdout.String(), "provider=44/22/6") ||
		!strings.Contains(stdout.String(), "prev_id=0") ||
		!strings.Contains(stdout.String(), "sockets=3:1") ||
		!strings.Contains(stdout.String(), "error_free=false") ||
		!strings.Contains(stdout.String(), "gate=fix_or_exclude_erroring_shape_before_promotion") ||
		!strings.Contains(stdout.String(), "stage=not_safe_erroring") ||
		!strings.Contains(stdout.String(), "blockers=erroring_shape") ||
		!strings.Contains(stdout.String(), "top_sessions=codex-wss:s1:27/120/open=0req/0tok/0headroom/open_ready=false/open_stage=t417_no_open_slice_candidate/open_blockers=no_promotion_open_headroom/pi=44/pc=22/prev=0/det=0/stateless=0/followup=0/guard=0/cache_bust=0/cache_classes=-/sockets=3:1/ok=false/fix_or_exclude_erroring_lineage_before_promotion/stage=not_safe_erroring/blockers=erroring_lineage|reference_only_backend_contract_required|missing_detached_or_stateless_followup_signal/blocker_headroom=erroring_lineage:27,missing_detached_or_stateless_followup_signal:27,reference_only_backend_contract_required:27") ||
		!strings.Contains(stdout.String(), "codex_exec_payload") ||
		!strings.Contains(stdout.String(), "codex-wss:s1") {
		t.Fatalf("text output missing details:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	statePath := writeAggregateStateFile(t, aggregateSampleAdminState)
	if code := runWSSAudit([]string{path, "--admin-state-file=" + statePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit policy text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Policy decisions:") ||
		!strings.Contains(stdout.String(), "wss_phasef/chunk_dedup/allow recoverable_chunk_dedup: 3") ||
		!strings.Contains(stdout.String(), "Cache decisions:") ||
		!strings.Contains(stdout.String(), "wss_phasef/read_delta/miss first_observation_seeded: 1") ||
		!strings.Contains(stdout.String(), "Chunk dedup density:") ||
		!strings.Contains(stdout.String(), "references:              4") {
		t.Fatalf("text output missing policy/cache details:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSAudit([]string{path, "--json", "--admin-state-file=" + statePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runWSSAudit json code=%d stderr=%s", code, stderr.String())
	}
	var report wssAuditReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.TokensSaved != 3 {
		t.Fatalf("tokens saved = %d, want 3", report.TokensSaved)
	}
	if report.RequestShapes["full_history"] != 1 {
		t.Fatalf("request shape JSON missing: %+v", report.RequestShapes)
	}
	if report.ResolvedRequestShapes["full_history"] != 1 || report.RequestShapeSources["fact"] != 1 {
		t.Fatalf("resolved request shape JSON missing: shapes=%+v sources=%+v", report.ResolvedRequestShapes, report.RequestShapeSources)
	}
	if report.FullHistory == nil ||
		report.FullHistory.ProviderInputTokens != 44 ||
		report.FullHistory.ProviderCachedTokens != 22 ||
		report.FullHistory.ProviderOutputTokens != 6 ||
		report.FullHistory.UpstreamErrorRequests != 1 ||
		report.FullHistory.HTTP400ErrorRequests != 1 ||
		report.FullHistory.BySocketSeq["3"] != 1 ||
		report.FullHistory.BySocketCloseInitiator["upstream_eof"] != 1 {
		t.Fatalf("full-history JSON missing: %+v", report.FullHistory)
	}
	if len(report.ShapeEconomics) != 1 ||
		report.ShapeEconomics[0].Shape != "full_history" ||
		report.ShapeEconomics[0].ProviderInputTokens != 44 ||
		report.ShapeEconomics[0].ProviderCachedTokens != 22 ||
		report.ShapeEconomics[0].ProviderCachedPct != 50 ||
		report.ShapeEconomics[0].ErrorRequests != 1 ||
		report.ShapeEconomics[0].UpstreamErrorRequests != 1 ||
		report.ShapeEconomics[0].HTTP400ErrorRequests != 1 {
		t.Fatalf("shape economics JSON missing: %+v", report.ShapeEconomics)
	}
	if len(report.HistoryReducers) != 1 || report.HistoryReducers[0].Mechanism != "stale_read" {
		t.Fatalf("history reducer JSON missing: %+v", report.HistoryReducers)
	}
	if len(report.FootprintEconomics) != 1 ||
		report.FootprintEconomics[0].Bucket != "mid" ||
		report.FootprintEconomics[0].TurnBand != "turn_4_8" ||
		report.FootprintEconomics[0].RequestShape != "full_history" ||
		report.FootprintEconomics[0].FootprintScore != 84 {
		t.Fatalf("footprint economics JSON missing: %+v", report.FootprintEconomics)
	}
	if report.FootprintCoverage == nil ||
		report.FootprintCoverage.TokenDecisions != 1 ||
		report.FootprintCoverage.WithFootprint != 1 ||
		report.FootprintCoverage.MissingFootprint != 0 ||
		report.FootprintCoverage.WithRemainingTurnsEstimate != 1 ||
		report.FootprintCoverage.MissingRemainingTurnsEstimate != 0 {
		t.Fatalf("footprint coverage JSON missing: %+v", report.FootprintCoverage)
	}
	if report.ShadowMirror == nil || report.ShadowMirror.NormalizedReferenceableBytes != 120 || report.ShadowMirror.NormalizedReferenceableBytePct != 60 {
		t.Fatalf("shadow mirror missing from JSON report: %+v", report.ShadowMirror)
	}
	if len(report.ShadowMirrorCandidates) != 1 ||
		report.ShadowMirrorCandidates[0].Kind != "codex_exec_payload" ||
		report.ShadowMirrorCandidates[0].CandidateLane != "t417_class_b_server_state" ||
		report.ShadowMirrorCandidates[0].CandidateLocalTokensEstimate <= 0 ||
		report.ShadowMirrorCandidates[0].IncrementalLocalTokensHeadroom <= 0 ||
		report.ShadowMirrorCandidates[0].ErrorFree ||
		report.ShadowMirrorCandidates[0].NextProofGate != "fix_or_exclude_erroring_shape_before_promotion" ||
		report.ShadowMirrorCandidates[0].PromotionStage != "not_safe_erroring" ||
		!stringSliceContains(report.ShadowMirrorCandidates[0].PromotionBlockers, "erroring_shape") ||
		len(report.ShadowMirrorCandidates[0].TopSessions) != 1 ||
		report.ShadowMirrorCandidates[0].TopSessions[0].SessionID != "codex-wss:s1" ||
		report.ShadowMirrorCandidates[0].TopSessions[0].IncrementalLocalTokensHeadroom <= 0 ||
		report.ShadowMirrorCandidates[0].TopSessions[0].ErrorFree ||
		report.ShadowMirrorCandidates[0].TopSessions[0].NextProofGate != "fix_or_exclude_erroring_lineage_before_promotion" ||
		report.ShadowMirrorCandidates[0].TopSessions[0].PromotionStage != "not_safe_erroring" ||
		!stringSliceContains(report.ShadowMirrorCandidates[0].TopSessions[0].PromotionBlockers, "erroring_lineage") {
		t.Fatalf("shadow mirror candidates missing from JSON report: %+v", report.ShadowMirrorCandidates)
	}
	if len(report.Policy) != 2 || report.PolicySource == "" {
		t.Fatalf("policy join missing from JSON report: %+v", report)
	}
	if len(report.Cache) != 2 {
		t.Fatalf("cache join missing from JSON report: %+v", report)
	}
	if report.ChunkDedupReferences != 4 || report.ChunkDedupRefBytes != 8192 || report.ChunkDedupInputBytes != 16384 {
		t.Fatalf("chunk density join missing from JSON report: %+v", report)
	}
}

func TestRunWSSAuditGateFailureExitCode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	writeJSONLFile(t, path, dbg.RequestSummary{
		RequestID: "wss-1",
		Path:      "/backend-api/codex/responses",
		RouteMode: "websocket_phasef",
	})

	var stdout, stderr bytes.Buffer
	if code := runWSSAudit([]string{path, "--expect-distinct-sessions=1"}, &stdout, &stderr); code != 3 {
		t.Fatalf("runWSSAudit gate code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "gate:") || !strings.Contains(stdout.String(), "FAIL") {
		t.Fatalf("gate output missing failure:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSAudit([]string{path, "--require-footprint-evidence"}, &stdout, &stderr); code != 3 {
		t.Fatalf("runWSSAudit footprint gate code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "footprint-score economics evidence") {
		t.Fatalf("footprint gate output missing failure:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWSSAudit([]string{path, "--expect-distinct-sessions=1", "--json"}, &stdout, &stderr); code != 3 {
		t.Fatalf("runWSSAudit json gate code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report wssAuditReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json gate output did not parse: %v\n%s", err, stdout.String())
	}
	if report.GatePassed || len(report.GateFailures) == 0 {
		t.Fatalf("json gate output missing failure: %+v", report)
	}
}

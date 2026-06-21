package main

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
)

const wssShadowMirrorOpportunityScope = "wss_shadow_mirror"

type gainOpportunityRow struct {
	Scope               string   `json:"scope,omitempty"`
	Command             string   `json:"command"`
	Outcome             string   `json:"outcome,omitempty"`
	Runs                int64    `json:"runs"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	LocalTokensHeadroom int64    `json:"local_tokens_headroom,omitempty"`
	CandidateLane       string   `json:"candidate_lane,omitempty"`
	NextProofGate       string   `json:"next_proof_gate,omitempty"`
	PromotionStage      string   `json:"promotion_stage,omitempty"`
	PromotionBlockers   []string `json:"promotion_blockers,omitempty"`
	RecommendedAction   string   `json:"recommended_action,omitempty"`
}

type wssShadowMirrorOpportunityAccumulator struct {
	rows map[string]*gainOpportunityRow
}

func queryWSSShadowMirrorOpportunityRows(period string, now time.Time, limit int) ([]gainOpportunityRow, error) {
	path := gainOpportunityDecisionsLogPath()
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	summaries, err := replaySessionFn(path)
	if err != nil {
		return nil, err
	}
	start, end, err := analytics.FilterGainWindow(period, now)
	if err != nil {
		return nil, err
	}
	var acc wssShadowMirrorOpportunityAccumulator
	for _, summary := range summaries {
		if summary.Timestamp.Before(start) || summary.Timestamp.After(end) {
			continue
		}
		acc.add(summary)
	}
	return acc.finalize(limit), nil
}

func gainOpportunityDecisionsLogPath() string {
	if path := configuredDecisionsLogPath(); path != "" {
		return path
	}
	home, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".slimference", "debug", "decisions.jsonl")
}

func (a *wssShadowMirrorOpportunityAccumulator) add(summary dbg.RequestSummary) {
	if len(summary.DebugFacts) == 0 || !gainWSSShadowMirrorFactsPresent(summary.DebugFacts) {
		return
	}
	shape := gainWSSRequestShape(summary)
	if a.rows == nil {
		a.rows = make(map[string]*gainOpportunityRow)
	}
	if refBytes := gainIntFact(summary.DebugFacts, "wss.shadow_mirror_referenceable_bytes"); refBytes > 0 {
		a.addRow(summary, shape, "exact_block", refBytes)
	}
	for _, row := range parseGainWSSShadowMirrorKindRows(summary.DebugFacts["wss.shadow_mirror_normalized_density_by_kind"]) {
		if row.referenceableBytes > 0 {
			a.addRow(summary, shape, row.kind, row.referenceableBytes)
		}
	}
	for _, row := range parseGainWSSShadowMirrorKindRows(summary.DebugFacts["wss.shadow_mirror_stateful_safe_density_by_kind"]) {
		if row.referenceableBytes > 0 {
			a.addRow(summary, shape, row.kind, row.referenceableBytes)
		}
	}
}

func (a *wssShadowMirrorOpportunityAccumulator) addRow(summary dbg.RequestSummary, shape, kind string, refBytes int) {
	lane := gainWSSShadowMirrorCandidateLane(shape, kind)
	key := shape + "\x00" + kind
	row := a.rows[key]
	if row == nil {
		row = &gainOpportunityRow{
			Scope:             wssShadowMirrorOpportunityScope,
			Command:           shape + "/" + kind,
			Outcome:           lane,
			CandidateLane:     lane,
			NextProofGate:     gainWSSShadowMirrorProofGate(lane, false),
			RecommendedAction: gainWSSShadowMirrorCandidateAction(shape, kind),
		}
		a.rows[key] = row
	}
	row.Runs++
	row.InputTokens += int64(tokens.Estimate(refBytes))
	row.OutputTokens += int64(gainMaxInt(0, summary.Tokens.Saved))
	if lane == "t417_class_b_server_state" {
		if summary.PreviousResponseIDUsed || gainParseBoolFact(summary.DebugFacts["wss.previous_response_id"]) {
			row.PromotionBlockers = append(row.PromotionBlockers, "mixed_previous_response_state_requires_exact_lineage_split")
		}
		if strings.TrimSpace(summary.DebugFacts["wss.structured_mutation_guard"]) != "" {
			row.PromotionBlockers = append(row.PromotionBlockers, "structured_mutation_guard_requires_exact_release_latch")
		}
		if gainParseBoolFact(summary.DebugFacts["wss.history_mutation_recovery_guard"]) {
			row.PromotionBlockers = append(row.PromotionBlockers, "history_recovery_guard_requires_lineage_reset")
		}
		if strings.TrimSpace(summary.DebugFacts["wss.cache_bust_demoted_mechanisms"]) != "" {
			row.PromotionBlockers = append(row.PromotionBlockers, "cache_bust_demotion_present_exact_class_scope")
		}
	}
	if len(summary.Errors) > 0 || gainHasUpstreamOrHTTP400Error(summary) {
		row.PromotionBlockers = append(row.PromotionBlockers, "erroring_shape")
	}
}

func (a *wssShadowMirrorOpportunityAccumulator) finalize(limit int) []gainOpportunityRow {
	if a == nil || len(a.rows) == 0 {
		return nil
	}
	rows := make([]gainOpportunityRow, 0, len(a.rows))
	for _, row := range a.rows {
		row.LocalTokensHeadroom = max(row.InputTokens-row.OutputTokens, 0)
		row.PromotionBlockers = gainDedupeStrings(append(row.PromotionBlockers, gainWSSShadowMirrorLaneBlockers(row.CandidateLane, row.LocalTokensHeadroom)...))
		row.NextProofGate = gainWSSShadowMirrorProofGate(row.CandidateLane, gainContainsString(row.PromotionBlockers, "erroring_shape"))
		row.PromotionStage = gainWSSShadowMirrorPromotionStage(row.CandidateLane, row.PromotionBlockers)
		rows = append(rows, *row)
	}
	sortGainOpportunityRows(rows)
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func commandOutputFirstRowsToGainOpportunities(rows []filter.FilterObservationAggregate) []gainOpportunityRow {
	out := make([]gainOpportunityRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, gainOpportunityRow{
			Scope:               row.Scope,
			Command:             row.Command,
			Outcome:             row.Outcome,
			Runs:                row.Runs,
			InputTokens:         row.InputTokens,
			OutputTokens:        row.OutputTokens,
			LocalTokensHeadroom: row.InputTokens,
		})
	}
	return out
}

func sortGainOpportunityRows(rows []gainOpportunityRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].LocalTokensHeadroom != rows[j].LocalTokensHeadroom {
			return rows[i].LocalTokensHeadroom > rows[j].LocalTokensHeadroom
		}
		if rows[i].InputTokens != rows[j].InputTokens {
			return rows[i].InputTokens > rows[j].InputTokens
		}
		return rows[i].Command < rows[j].Command
	})
}

type gainWSSShadowMirrorKindRow struct {
	kind               string
	referenceableBytes int
}

func parseGainWSSShadowMirrorKindRows(encoded string) []gainWSSShadowMirrorKindRow {
	if strings.TrimSpace(encoded) == "" {
		return nil
	}
	var rows []gainWSSShadowMirrorKindRow
	for part := range strings.SplitSeq(encoded, ",") {
		name, values, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		fields := strings.Split(values, "/")
		if len(fields) < 4 {
			continue
		}
		refBytes, ok := gainParseNonNegativeInt(fields[0])
		if !ok {
			continue
		}
		rows = append(rows, gainWSSShadowMirrorKindRow{kind: strings.TrimSpace(name), referenceableBytes: refBytes})
	}
	return rows
}

func gainWSSRequestShape(summary dbg.RequestSummary) string {
	if shape := strings.TrimSpace(summary.DebugFacts["wss.request_shape"]); shape != "" {
		return shape
	}
	if summary.PreviousResponseIDUsed || gainParseBoolFact(summary.DebugFacts["wss.previous_response_id"]) {
		return "delta"
	}
	if gainParseBoolFact(summary.DebugFacts["wss.delta_shape"]) {
		return "delta"
	}
	return "full_history"
}

func gainWSSShadowMirrorCandidateLane(shape, kind string) string {
	switch strings.TrimSpace(shape) {
	case "full_history":
		kind = strings.TrimSpace(kind)
		if gainWSSShadowMirrorProductizableOpenKind(kind) {
			return "t417_class_b_server_state"
		}
		if kind == "codex_exec_payload" || strings.HasPrefix(kind, "codex_exec_payload_command_") || strings.HasPrefix(kind, "tool_result_command_") {
			return "t408_reference_or_t418_parser_recovery"
		}
		return "t408_backend_reference_contract"
	case "delta":
		return "t405_t354_stateful_delta"
	case "root":
		return "t406_t418_parser_frontier"
	default:
		return "capture_shape_resolution"
	}
}

func gainWSSShadowMirrorCandidateAction(shape, kind string) string {
	shape = strings.TrimSpace(shape)
	kind = strings.TrimSpace(kind)
	switch {
	case shape == "full_history" && gainWSSShadowMirrorProductizableOpenKind(kind):
		return "rank for T417 exact lineage-scoped continuation; this kind has a productizable reducer/recovery contract"
	case shape == "full_history" && kind == "codex_exec_payload":
		return "rank for T408 backend-reference acceptance or T418 command-output-first/parser recovery; do not treat as direct T417 product slice"
	case shape == "full_history" && strings.HasPrefix(kind, "codex_exec_payload_command_"):
		return "rank this exact command family for T418 command-output-first/parser recovery first, with T408 backend-reference acceptance as the direct-reference path"
	case shape == "full_history" && strings.HasPrefix(kind, "tool_result_command_"):
		return "rank this resolved tool-result command family for T418 command-output-first/parser recovery first, with T408 backend-reference acceptance as the direct-reference path"
	case shape == "full_history":
		return "rank for T408 backend-reference acceptance or exact rehydrate-before-upstream contract; no direct T417 activation without a productizable reducer"
	case shape == "delta":
		return "rank for T405/T354 stateful-delta proof, keep current delta guards until downstream-clean"
	case shape == "root":
		return "rank for T406/T418 parser/default-on command-output classes"
	default:
		return "improve shape attribution before product promotion"
	}
}

func gainWSSShadowMirrorLaneBlockers(lane string, headroom int64) []string {
	var blockers []string
	if headroom <= 0 {
		blockers = append(blockers, "no_incremental_local_headroom")
	}
	switch lane {
	case "t408_backend_reference_contract":
		blockers = append(blockers, "reference_only_backend_contract_required")
	case "t408_reference_or_t418_parser_recovery":
		blockers = append(blockers, "reference_only_backend_contract_required", "requires_parser_or_recovery_product_slice")
	case "t405_t354_stateful_delta":
		blockers = append(blockers, "requires_downstream_state_zero400_gate")
	case "t406_t418_parser_frontier":
		blockers = append(blockers, "requires_command_output_first_parser_gate")
	case "capture_shape_resolution":
		blockers = append(blockers, "requires_shape_resolution")
	}
	return blockers
}

func gainWSSShadowMirrorProofGate(lane string, erroring bool) string {
	if erroring {
		return "fix_or_exclude_erroring_shape_before_promotion"
	}
	switch lane {
	case "t417_class_b_server_state":
		return "t417_exact_lineage_net_positive_zero400_gate"
	case "t408_backend_reference_contract":
		return "t408_backend_reference_acceptance_or_exact_rehydrate_contract"
	case "t408_reference_or_t418_parser_recovery":
		return "t408_backend_reference_or_t418_parser_recovery_gate"
	case "t405_t354_stateful_delta":
		return "t405_t354_downstream_state_zero400_gate"
	case "t406_t418_parser_frontier":
		return "t418_command_output_first_or_t406_stateful_safe_parser_gate"
	default:
		return "shape_resolution_gate"
	}
}

func gainWSSShadowMirrorPromotionStage(lane string, blockers []string) string {
	for _, blocker := range blockers {
		if blocker == "erroring_shape" {
			return "not_safe_erroring"
		}
		if blocker == "no_incremental_local_headroom" {
			return "not_economic"
		}
	}
	if len(blockers) == 0 {
		return "product_candidate_no_observed_blockers"
	}
	switch lane {
	case "t417_class_b_server_state":
		return "t417_lineage_candidate_needs_engineering"
	case "t408_backend_reference_contract":
		return "t408_backend_reference_candidate_needs_accepted_contract"
	case "t408_reference_or_t418_parser_recovery":
		return "t408_reference_or_t418_parser_recovery_candidate_needs_contract"
	case "t405_t354_stateful_delta":
		return "t405_t354_candidate_needs_downstream_state_gate"
	case "t406_t418_parser_frontier":
		return "t418_parser_candidate_needs_release_gate"
	default:
		return "needs_shape_resolution"
	}
}

func gainWSSShadowMirrorProductizableOpenKind(kind string) bool {
	kind = strings.TrimSpace(kind)
	return kind == "stateful_safe_tool_output" ||
		kind == "stateful_safe_history_reducer" ||
		kind == "search_cap_stateful_followup" ||
		strings.HasPrefix(kind, "stateful_safe_tool_output_")
}

func gainWSSShadowMirrorFactsPresent(facts map[string]string) bool {
	for key := range facts {
		if strings.HasPrefix(key, "wss.shadow_mirror_") {
			return true
		}
	}
	return false
}

func gainHasUpstreamOrHTTP400Error(summary dbg.RequestSummary) bool {
	for _, msg := range summary.Errors {
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "upstream") || strings.Contains(lower, "400") || strings.Contains(lower, "invalid_request") {
			return true
		}
	}
	return false
}

func gainIntFact(facts map[string]string, key string) int {
	value, ok := gainParseNonNegativeInt(facts[key])
	if !ok {
		return 0
	}
	return value
}

func gainParseNonNegativeInt(value string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func gainParseBoolFact(value string) bool {
	ok, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && ok
}

func gainDedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func gainContainsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func gainMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

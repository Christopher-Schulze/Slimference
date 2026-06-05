// benchmark_corpus.go drives the per-category live-corpus regression
// gate. It walks `<root>/<category>/{*.jsonl, metadata.json}`, aggregates
// each category through the existing session-report aggregator, and
// compares the resulting savings ratio plus per-layer breakdowns against
// declared expectations in `metadata.json`. Used both as the standalone
// `benchmark-corpus` subcommand and inside `scripts/ci`.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	corpusCategoryMetadataFilename = "metadata.json"
	outputReduceABReportFilename   = "output_reduce_ab_report.json"
)

// CategoryMetadata is the minimal description a maintainer commits next
// to a corpus category so the gate has expectations to measure against.
// Categories can gate either on a measured savings ratio or on an absolute
// saved-token counter when the proof source does not preserve the denominator.
type CategoryMetadata struct {
	Category                            string   `json:"category"`
	Description                         string   `json:"description"`
	Synthetic                           bool     `json:"synthetic"`
	EvidenceLevel                       string   `json:"evidence_level"`
	ClientFamily                        string   `json:"client_family,omitempty"`
	WorkloadClass                       string   `json:"workload_class,omitempty"`
	Language                            string   `json:"language"`
	ToolMix                             string   `json:"tool_mix"`
	ExpectedSavingsMin                  float64  `json:"expected_savings_min"`
	ExpectedSavingsMax                  float64  `json:"expected_savings_max"`
	ExpectedSavedTokensMin              int64    `json:"expected_saved_tokens_min,omitempty"`
	ExpectedRequestCount                int      `json:"expected_request_count"`
	ExpectedLayer2Optional              bool     `json:"expected_layer2_optional"`
	ExpectedMaxErrors                   int      `json:"expected_max_errors,omitempty"`
	ExpectedLatencyP95MaxMs             float64  `json:"expected_latency_p95_max_ms,omitempty"`
	ExpectedProviderCacheReadMin        int64    `json:"expected_provider_cache_read_min,omitempty"`
	ExpectedOutputReduceAppliedMin      int      `json:"expected_output_reduce_applied_min,omitempty"`
	ExpectedOutputReduceOverheadMax     int64    `json:"expected_output_reduce_input_overhead_max,omitempty"`
	ExpectedOutputReduceNetObservedMin  int64    `json:"expected_output_reduce_net_observed_min,omitempty"`
	ExpectedOutputReduceABPairsMin      int      `json:"expected_output_reduce_ab_pairs_min,omitempty"`
	ExpectedOutputReduceABNetSavedMin   int64    `json:"expected_output_reduce_ab_net_saved_min,omitempty"`
	ExpectedOutputReduceABSavingsPctMin float64  `json:"expected_output_reduce_ab_savings_pct_min,omitempty"`
	ExpectedReReadCountMax              int      `json:"expected_reread_count_max,omitempty"`
	ExpectedPlannerMissedMax            int      `json:"expected_planner_missed_max,omitempty"`
	ExpectedPlannerBypassAppliedMax     int      `json:"expected_planner_bypass_applied_max,omitempty"`
	ScenarioValidators                  []string `json:"scenario_validators,omitempty"`
	Notes                               string   `json:"notes"`

	expectedMaxErrorsSet                  bool
	expectedReReadCountMaxSet             bool
	expectedLatencyP95MaxSet              bool
	expectedOutputReduceNetObservedMinSet bool
}

// CategoryResult is the per-category outcome of one gate evaluation.
type CategoryResult struct {
	Category                    string                               `json:"category"`
	Path                        string                               `json:"path"`
	Sessions                    int                                  `json:"sessions"`
	Requests                    int                                  `json:"requests"`
	OrigTokens                  int64                                `json:"orig_tokens"`
	SavedTokens                 int64                                `json:"saved_tokens"`
	SavingsRatio                float64                              `json:"savings_ratio"`
	Layer0Saved                 int64                                `json:"layer0_saved"`
	Layer1Saved                 int64                                `json:"layer1_saved"`
	Layer2Saved                 int64                                `json:"layer2_saved"`
	Layer3Saved                 int64                                `json:"layer3_saved"`
	OutputTokens                int64                                `json:"output_tokens"`
	ProviderCacheReadTokens     int64                                `json:"provider_cache_read_tokens"`
	ProviderCacheCreateTokens   int64                                `json:"provider_cache_create_tokens"`
	ProviderCachedTokens        int64                                `json:"provider_cached_tokens"`
	OutputReduceApplied         int                                  `json:"output_reduce_applied"`
	OutputReduceInputOverhead   int64                                `json:"output_reduce_input_overhead_tokens"`
	OutputReduceNetObserved     int64                                `json:"output_reduce_net_observed_tokens"`
	OutputReduceABPairs         int                                  `json:"output_reduce_ab_pairs"`
	OutputReduceABPassedPairs   int                                  `json:"output_reduce_ab_passed_pairs"`
	OutputReduceABOutputSaved   int64                                `json:"output_reduce_ab_output_tokens_saved"`
	OutputReduceABNetSaved      int64                                `json:"output_reduce_ab_net_tokens_saved"`
	OutputReduceABSavingsPctMin float64                              `json:"output_reduce_ab_savings_pct_min,omitempty"`
	OutputReduceABFailures      []string                             `json:"output_reduce_ab_failures,omitempty"`
	ToolPruneApplied            int                                  `json:"tool_prune_applied"`
	ToolPruneSavedTokens        int64                                `json:"tool_prune_saved_tokens"`
	OCRLCandidateCapsules       int                                  `json:"ocrl_candidate_capsules"`
	OCRLArchiveExpansions       int                                  `json:"ocrl_archive_expansions"`
	OCRLSavedTokens             int64                                `json:"ocrl_saved_tokens"`
	OCRLApplied                 int                                  `json:"ocrl_applied"`
	OCRLShadowOnly              int                                  `json:"ocrl_shadow_only"`
	OCRLFullHistoryRows         int                                  `json:"ocrl_full_history_rows"`
	ErrorCount                  int                                  `json:"error_count"`
	ReReadCount                 int                                  `json:"reread_count"`
	HostBudgetOKRows            int                                  `json:"host_budget_ok_rows"`
	HostBudgetIssueRows         int                                  `json:"host_budget_issue_rows"`
	LatencyP95Ms                float64                              `json:"latency_p95_ms"`
	PlanReplay                  planReplayAggregate                  `json:"plan_replay"`
	LayerCombinations           map[string]layerCombinationAggregate `json:"layer_combinations,omitempty"`
	EvidenceLevel               string                               `json:"evidence_level"`
	Synthetic                   bool                                 `json:"synthetic"`
	ClientFamily                string                               `json:"client_family,omitempty"`
	WorkloadClass               string                               `json:"workload_class,omitempty"`
	Failures                    []string                             `json:"failures,omitempty"`
	GateConfigured              bool                                 `json:"gate_configured"`
	Metadata                    *CategoryMetadata                    `json:"metadata,omitempty"`
}

// CorpusReport is the aggregate of all categories.
type CorpusReport struct {
	Root               string               `json:"root"`
	Categories         []CategoryResult     `json:"categories"`
	TotalRequests      int                  `json:"total_requests"`
	OverallRatio       float64              `json:"overall_savings_ratio"`
	HasSynthetic       bool                 `json:"has_synthetic"`
	HasReal            bool                 `json:"has_real"`
	PromotionGate      *PromotionGateReport `json:"promotion_gate,omitempty"`
	MaxxGate           *MaxxGateReport      `json:"maxx_gate,omitempty"`
	SessionsByClient   map[string]int       `json:"sessions_by_client,omitempty"`
	SessionsByWorkload map[string]int       `json:"sessions_by_workload,omitempty"`
}

// PromotionGateReport is the release/default-promotion verdict. It is separate
// from the ordinary corpus gate so synthetic CI smoke fixtures stay useful while
// default-on changes still require real operator evidence.
type PromotionGateReport struct {
	Passed             bool           `json:"passed"`
	RealCategories     int            `json:"real_categories"`
	RealSessions       int            `json:"real_sessions"`
	SessionsByClient   map[string]int `json:"sessions_by_client"`
	SessionsByWorkload map[string]int `json:"sessions_by_workload"`
	Failures           []string       `json:"failures,omitempty"`
}

// MaxxGateReport is the strict end-to-end evidence verdict for the user's
// max-out bar: release proof plus the mechanism-specific proof breadth that
// stays open after the base release matrix passes.
type MaxxGateReport struct {
	Passed             bool           `json:"passed"`
	RealCategories     int            `json:"real_categories"`
	RealSessions       int            `json:"real_sessions"`
	SessionsByClient   map[string]int `json:"sessions_by_client"`
	SessionsByWorkload map[string]int `json:"sessions_by_workload"`
	Failures           []string       `json:"failures,omitempty"`
}

// LoadCategoryMetadata reads `<dir>/metadata.json` and returns it. The
// presence of the file is mandatory for a directory to count as a corpus
// category; absence causes a friendly error so reviewers can spot the
// gap quickly.
func LoadCategoryMetadata(dir string) (*CategoryMetadata, error) {
	path := filepath.Join(dir, corpusCategoryMetadataFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("category %s: missing %s", filepath.Base(dir), corpusCategoryMetadataFilename)
		}
		return nil, err
	}
	var meta CategoryMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	if _, ok := raw["expected_max_errors"]; ok {
		meta.expectedMaxErrorsSet = true
	} else {
		meta.ExpectedMaxErrors = -1
	}
	if _, ok := raw["expected_reread_count_max"]; ok {
		meta.expectedReReadCountMaxSet = true
	} else {
		meta.ExpectedReReadCountMax = -1
	}
	if _, ok := raw["expected_latency_p95_max_ms"]; ok {
		meta.expectedLatencyP95MaxSet = true
	}
	if _, ok := raw["expected_output_reduce_net_observed_min"]; ok {
		meta.expectedOutputReduceNetObservedMinSet = true
	}
	if _, ok := raw["expected_planner_missed_max"]; !ok {
		meta.ExpectedPlannerMissedMax = -1
	}
	if _, ok := raw["expected_planner_bypass_applied_max"]; !ok {
		meta.ExpectedPlannerBypassAppliedMax = -1
	}
	if meta.Category == "" {
		meta.Category = filepath.Base(dir)
	}
	return &meta, nil
}

// EvaluateCategory aggregates all jsonl files under the directory and
// returns a CategoryResult plus the gate failures.
func EvaluateCategory(dir string, errOut io.Writer) (CategoryResult, error) {
	meta, err := LoadCategoryMetadata(dir)
	if err != nil {
		return CategoryResult{}, err
	}
	abSummary, err := loadCategoryOutputReduceABReport(dir)
	if err != nil {
		return CategoryResult{}, err
	}
	agg, err := AggregateSessionsFromPath(dir, errOut)
	if err != nil {
		return CategoryResult{}, fmt.Errorf("aggregate %s: %w", dir, err)
	}
	sessions, err := countSessionFiles(dir)
	if err != nil {
		return CategoryResult{}, err
	}
	if sessions == 0 && abSummary.Pairs > 0 {
		sessions = abSummary.Pairs
	}
	ratio := 0.0
	if agg.origTokens > 0 {
		ratio = float64(agg.savedTokens) / float64(agg.origTokens)
	}
	res := CategoryResult{
		Category:                    meta.Category,
		Path:                        dir,
		Sessions:                    sessions,
		Requests:                    agg.requests,
		OrigTokens:                  agg.origTokens,
		SavedTokens:                 agg.savedTokens,
		SavingsRatio:                ratio,
		Layer0Saved:                 agg.layer0Saved,
		Layer1Saved:                 agg.layer1Saved,
		Layer2Saved:                 agg.layer2Saved,
		Layer3Saved:                 agg.layer3Saved,
		OutputTokens:                agg.outputTokenSum,
		ProviderCacheReadTokens:     agg.cacheReadSum,
		ProviderCacheCreateTokens:   agg.cacheCreateSum,
		ProviderCachedTokens:        agg.providerCachedSum,
		OutputReduceApplied:         agg.outputReduceApplied,
		OutputReduceInputOverhead:   agg.outputReduceInputOverhead,
		OutputReduceNetObserved:     agg.outputTokenSum - agg.outputReduceInputOverhead,
		OutputReduceABPairs:         abSummary.Pairs,
		OutputReduceABPassedPairs:   abSummary.PassedPairs,
		OutputReduceABOutputSaved:   abSummary.OutputSaved,
		OutputReduceABNetSaved:      abSummary.NetSaved,
		OutputReduceABSavingsPctMin: abSummary.SavingsPctMin,
		OutputReduceABFailures:      append([]string(nil), abSummary.Failures...),
		ToolPruneApplied:            agg.toolPruneApplied,
		ToolPruneSavedTokens:        agg.toolPruneSaved,
		OCRLCandidateCapsules:       agg.ocrlCandidateCapsules,
		OCRLArchiveExpansions:       agg.ocrlArchiveExpansions,
		OCRLSavedTokens:             agg.ocrlSavedTokens,
		OCRLApplied:                 agg.ocrlApplied,
		OCRLShadowOnly:              agg.ocrlShadowOnly,
		OCRLFullHistoryRows:         agg.ocrlFullHistoryRows,
		ErrorCount:                  agg.errorCount,
		ReReadCount:                 agg.reReadCount,
		HostBudgetOKRows:            agg.hostBudgetOK,
		HostBudgetIssueRows:         agg.hostBudgetIssues,
		LatencyP95Ms:                percentileFloat64(agg.latenciesMs, 0.95),
		PlanReplay:                  clonePlanReplayAggregate(agg.planReplay),
		LayerCombinations:           cloneLayerCombinations(agg.layerCombinations),
		EvidenceLevel:               normalizeEvidenceLevel(meta),
		Synthetic:                   meta.Synthetic,
		ClientFamily:                strings.TrimSpace(meta.ClientFamily),
		WorkloadClass:               strings.TrimSpace(meta.WorkloadClass),
		GateConfigured: meta.ExpectedSavingsMin > 0 ||
			meta.ExpectedRequestCount > 0 ||
			meta.ExpectedLatencyP95MaxMs > 0 ||
			meta.ExpectedProviderCacheReadMin > 0 ||
			meta.ExpectedOutputReduceAppliedMin > 0 ||
			meta.ExpectedOutputReduceOverheadMax > 0 ||
			meta.ExpectedOutputReduceABPairsMin > 0 ||
			meta.ExpectedOutputReduceABNetSavedMin > 0 ||
			meta.ExpectedOutputReduceABSavingsPctMin > 0 ||
			meta.ExpectedSavedTokensMin > 0 ||
			meta.expectedReReadCountMaxSet ||
			meta.expectedMaxErrorsSet ||
			meta.ExpectedPlannerMissedMax >= 0 ||
			meta.ExpectedPlannerBypassAppliedMax >= 0 ||
			len(meta.ScenarioValidators) > 0,
		Metadata: meta,
	}
	res.Failures = evaluateCategoryGate(res, meta)
	return res, nil
}

type categoryOutputReduceABSummary struct {
	Pairs         int
	PassedPairs   int
	OutputSaved   int64
	NetSaved      int64
	SavingsPctMin float64
	Failures      []string
}

type categoryOutputReduceABReport struct {
	Pairs        []categoryOutputReduceABPair `json:"pairs"`
	PairCount    int                          `json:"pair_count"`
	GatePassed   bool                         `json:"gate_passed"`
	GateFailures []string                     `json:"gate_failures,omitempty"`
}

type categoryOutputReduceABPair struct {
	PairID            string   `json:"pair_id"`
	OutputTokensSaved int64    `json:"output_tokens_saved"`
	NetTokensSaved    int64    `json:"net_tokens_saved"`
	OutputSavingsPct  float64  `json:"output_savings_pct"`
	GatePassed        bool     `json:"gate_passed"`
	GateFailures      []string `json:"gate_failures,omitempty"`
}

func loadCategoryOutputReduceABReport(dir string) (categoryOutputReduceABSummary, error) {
	path := filepath.Join(dir, outputReduceABReportFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return categoryOutputReduceABSummary{}, nil
		}
		return categoryOutputReduceABSummary{}, err
	}
	var report categoryOutputReduceABReport
	if err := json.Unmarshal(data, &report); err != nil {
		return categoryOutputReduceABSummary{}, fmt.Errorf("parse %s: %w", path, err)
	}
	summary := categoryOutputReduceABSummary{Pairs: report.PairCount}
	if summary.Pairs == 0 {
		summary.Pairs = len(report.Pairs)
	}
	if summary.Pairs == 0 {
		summary.Failures = append(summary.Failures, "output-reduce A/B report has no pairs")
	}
	if !report.GatePassed {
		if len(report.GateFailures) == 0 {
			summary.Failures = append(summary.Failures, "output-reduce A/B report gate failed")
		}
		for _, failure := range report.GateFailures {
			summary.Failures = append(summary.Failures, "output-reduce A/B report: "+failure)
		}
	}
	for _, pair := range report.Pairs {
		if pair.GatePassed {
			summary.PassedPairs++
		} else {
			id := strings.TrimSpace(pair.PairID)
			if id == "" {
				id = "unnamed-pair"
			}
			if len(pair.GateFailures) == 0 {
				summary.Failures = append(summary.Failures, id+": pair gate failed")
			}
			for _, failure := range pair.GateFailures {
				summary.Failures = append(summary.Failures, id+": "+failure)
			}
		}
		summary.OutputSaved += pair.OutputTokensSaved
		summary.NetSaved += pair.NetTokensSaved
		if pair.OutputSavingsPct > 0 && (summary.SavingsPctMin == 0 || pair.OutputSavingsPct < summary.SavingsPctMin) {
			summary.SavingsPctMin = pair.OutputSavingsPct
		}
	}
	if summary.PassedPairs != summary.Pairs {
		summary.Failures = append(summary.Failures, fmt.Sprintf("output-reduce A/B passed_pairs=%d != pairs=%d", summary.PassedPairs, summary.Pairs))
	}
	return summary, nil
}

func countSessionFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".jsonl" {
			count++
		}
	}
	return count, nil
}

func evaluateCategoryGate(res CategoryResult, meta *CategoryMetadata) []string {
	var failures []string
	if meta.ExpectedRequestCount > 0 && res.Requests < meta.ExpectedRequestCount {
		failures = append(failures, fmt.Sprintf("requests=%d < expected=%d", res.Requests, meta.ExpectedRequestCount))
	}
	if meta.ExpectedSavingsMin > 0 {
		if res.SavingsRatio+1e-9 < meta.ExpectedSavingsMin {
			failures = append(failures, fmt.Sprintf("savings_ratio=%.4f < min=%.4f", res.SavingsRatio, meta.ExpectedSavingsMin))
		}
	}
	if meta.ExpectedSavingsMax > 0 && res.SavingsRatio > meta.ExpectedSavingsMax+1e-9 {
		failures = append(failures, fmt.Sprintf("savings_ratio=%.4f > max=%.4f (suspicious overcount)", res.SavingsRatio, meta.ExpectedSavingsMax))
	}
	if meta.ExpectedSavedTokensMin > 0 && res.SavedTokens < meta.ExpectedSavedTokensMin {
		failures = append(failures, fmt.Sprintf("saved_tokens=%d < min=%d", res.SavedTokens, meta.ExpectedSavedTokensMin))
	}
	if meta.ExpectedMaxErrors >= 0 && res.ErrorCount > meta.ExpectedMaxErrors {
		failures = append(failures, fmt.Sprintf("errors=%d > max=%d", res.ErrorCount, meta.ExpectedMaxErrors))
	}
	if meta.ExpectedLatencyP95MaxMs > 0 && res.LatencyP95Ms > meta.ExpectedLatencyP95MaxMs+1e-9 {
		failures = append(failures, fmt.Sprintf("latency_p95_ms=%.1f > max=%.1f", res.LatencyP95Ms, meta.ExpectedLatencyP95MaxMs))
	}
	if meta.ExpectedProviderCacheReadMin > 0 && res.ProviderCacheReadTokens < meta.ExpectedProviderCacheReadMin {
		failures = append(failures, fmt.Sprintf("provider_cache_read_tokens=%d < min=%d", res.ProviderCacheReadTokens, meta.ExpectedProviderCacheReadMin))
	}
	if meta.ExpectedOutputReduceAppliedMin > 0 && res.OutputReduceApplied < meta.ExpectedOutputReduceAppliedMin {
		failures = append(failures, fmt.Sprintf("output_reduce_applied=%d < min=%d", res.OutputReduceApplied, meta.ExpectedOutputReduceAppliedMin))
	}
	if meta.ExpectedOutputReduceOverheadMax > 0 && res.OutputReduceInputOverhead > meta.ExpectedOutputReduceOverheadMax {
		failures = append(failures, fmt.Sprintf("output_reduce_input_overhead_tokens=%d > max=%d", res.OutputReduceInputOverhead, meta.ExpectedOutputReduceOverheadMax))
	}
	if meta.expectedOutputReduceNetObservedMinSet && res.OutputReduceNetObserved < meta.ExpectedOutputReduceNetObservedMin {
		failures = append(failures, fmt.Sprintf("output_reduce_net_observed_tokens=%d < min=%d", res.OutputReduceNetObserved, meta.ExpectedOutputReduceNetObservedMin))
	}
	if meta.ExpectedOutputReduceABPairsMin > 0 && res.OutputReduceABPairs < meta.ExpectedOutputReduceABPairsMin {
		failures = append(failures, fmt.Sprintf("output_reduce_ab_pairs=%d < min=%d", res.OutputReduceABPairs, meta.ExpectedOutputReduceABPairsMin))
	}
	if meta.ExpectedOutputReduceABNetSavedMin > 0 && res.OutputReduceABNetSaved < meta.ExpectedOutputReduceABNetSavedMin {
		failures = append(failures, fmt.Sprintf("output_reduce_ab_net_tokens_saved=%d < min=%d", res.OutputReduceABNetSaved, meta.ExpectedOutputReduceABNetSavedMin))
	}
	if meta.ExpectedOutputReduceABSavingsPctMin > 0 && res.OutputReduceABSavingsPctMin+1e-9 < meta.ExpectedOutputReduceABSavingsPctMin {
		failures = append(failures, fmt.Sprintf("output_reduce_ab_savings_pct_min=%.2f < min=%.2f", res.OutputReduceABSavingsPctMin, meta.ExpectedOutputReduceABSavingsPctMin))
	}
	failures = append(failures, res.OutputReduceABFailures...)
	if meta.ExpectedReReadCountMax >= 0 && res.ReReadCount > meta.ExpectedReReadCountMax {
		failures = append(failures, fmt.Sprintf("reread_count=%d > max=%d", res.ReReadCount, meta.ExpectedReReadCountMax))
	}
	if meta.ExpectedPlannerMissedMax >= 0 && res.PlanReplay.MissedActive > meta.ExpectedPlannerMissedMax {
		failures = append(failures, fmt.Sprintf("planner_missed_active=%d > max=%d", res.PlanReplay.MissedActive, meta.ExpectedPlannerMissedMax))
	}
	if meta.ExpectedPlannerBypassAppliedMax >= 0 && res.PlanReplay.BypassApplied > meta.ExpectedPlannerBypassAppliedMax {
		failures = append(failures, fmt.Sprintf("planner_bypass_applied=%d > max=%d", res.PlanReplay.BypassApplied, meta.ExpectedPlannerBypassAppliedMax))
	}
	failures = append(failures, evaluateScenarioValidators(res, meta.ScenarioValidators)...)
	return failures
}

func evaluateScenarioValidators(res CategoryResult, validators []string) []string {
	var failures []string
	for _, raw := range validators {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		switch name {
		case "tool_heavy":
			if res.ToolPruneApplied <= 0 || res.ToolPruneSavedTokens <= 0 {
				failures = append(failures, "scenario tool_heavy: expected tool-prune application with saved tokens")
			}
		case "cache_reuse":
			if res.Layer3Saved <= 0 && res.ProviderCacheReadTokens <= 0 && res.ProviderCachedTokens <= 0 {
				failures = append(failures, "scenario cache_reuse: expected Layer 3 or provider cache evidence")
			}
		case "output_reduce":
			if res.OutputReduceApplied <= 0 {
				failures = append(failures, "scenario output_reduce: expected output-reduce application")
			} else if res.OutputTokens <= 0 {
				failures = append(failures, "scenario output_reduce: expected observed output-token evidence")
			}
		case "output_reduce_ab":
			if res.OutputReduceABPairs <= 0 {
				failures = append(failures, "scenario output_reduce_ab: expected paired A/B evidence")
			} else if res.OutputReduceABPassedPairs != res.OutputReduceABPairs {
				failures = append(failures, fmt.Sprintf("scenario output_reduce_ab: passed_pairs=%d pairs=%d", res.OutputReduceABPassedPairs, res.OutputReduceABPairs))
			} else if res.OutputReduceABNetSaved <= 0 {
				failures = append(failures, fmt.Sprintf("scenario output_reduce_ab: net_saved=%d", res.OutputReduceABNetSaved))
			}
		case "planner_alignment":
			if res.PlanReplay.RequestsWithPlan <= 0 {
				failures = append(failures, "scenario planner_alignment: expected planner decisions")
			} else if res.PlanReplay.MissedActive > 0 || res.PlanReplay.BypassApplied > 0 {
				failures = append(failures, fmt.Sprintf("scenario planner_alignment: missed=%d bypass_applied=%d", res.PlanReplay.MissedActive, res.PlanReplay.BypassApplied))
			}
		case "websocket":
			if !hasLayerCombination(res.LayerCombinations, "WS") {
				failures = append(failures, "scenario websocket: expected websocket layer evidence")
			}
		case "low_error":
			if res.ErrorCount != 0 {
				failures = append(failures, fmt.Sprintf("scenario low_error: errors=%d", res.ErrorCount))
			}
		case "host_budget_ok":
			if res.HostBudgetOKRows <= 0 || res.HostBudgetIssueRows > 0 {
				failures = append(failures, fmt.Sprintf("scenario host_budget_ok: ok_rows=%d issue_rows=%d", res.HostBudgetOKRows, res.HostBudgetIssueRows))
			}
		case "layer_combo_diversity":
			if len(res.LayerCombinations) < 2 {
				failures = append(failures, fmt.Sprintf("scenario layer_combo_diversity: combinations=%d", len(res.LayerCombinations)))
			}
		case "l2_summary":
			if res.Layer2Saved <= 0 {
				failures = append(failures, "scenario l2_summary: expected Layer 2 savings")
			}
		case "ocrl_full_history":
			if res.OCRLApplied <= 0 {
				failures = append(failures, "scenario ocrl_full_history: expected model-facing OCRL applied evidence")
			}
			if res.OCRLFullHistoryRows <= 0 {
				failures = append(failures, "scenario ocrl_full_history: expected full-history OCRL route evidence")
			}
			if res.OCRLCandidateCapsules <= 0 || res.OCRLArchiveExpansions <= 0 {
				failures = append(failures, fmt.Sprintf("scenario ocrl_full_history: candidates=%d archive_expansions=%d", res.OCRLCandidateCapsules, res.OCRLArchiveExpansions))
			}
			if res.OCRLSavedTokens <= 0 {
				failures = append(failures, fmt.Sprintf("scenario ocrl_full_history: saved_tokens=%d", res.OCRLSavedTokens))
			}
			if res.OCRLShadowOnly > 0 {
				failures = append(failures, fmt.Sprintf("scenario ocrl_full_history: shadow_only_rows=%d", res.OCRLShadowOnly))
			}
		default:
			failures = append(failures, fmt.Sprintf("unknown scenario validator %q", raw))
		}
	}
	return failures
}

var supportedScenarioValidators = []string{
	"tool_heavy",
	"cache_reuse",
	"output_reduce",
	"output_reduce_ab",
	"planner_alignment",
	"websocket",
	"low_error",
	"host_budget_ok",
	"layer_combo_diversity",
	"l2_summary",
	"ocrl_full_history",
}

func hasLayerCombination(combos map[string]layerCombinationAggregate, label string) bool {
	for key := range combos {
		for _, part := range strings.Split(key, "+") {
			if part == label {
				return true
			}
		}
	}
	return false
}

func normalizeEvidenceLevel(meta *CategoryMetadata) string {
	level := strings.TrimSpace(meta.EvidenceLevel)
	if level != "" {
		return level
	}
	if meta.Synthetic {
		return "synthetic"
	}
	return "live_operator"
}

var requiredPromotionClientSessions = map[string]int{
	"codex_cli":     5,
	"codex_desktop": 5,
}

var requiredPromotionWorkloads = []string{
	"repeat_read",
	"ranged_read",
	"search_loop",
	"git_status",
	"test_failure",
	"apply_patch_edit_read",
	"large_tool_output",
	"long_workday",
}

var requiredMaxxWorkloads = []string{
	"chunk_dedup_similar_outputs",
	"chunk_dedup_log_output",
	"chunk_dedup_test_output",
	"ocrl_full_history",
	"output_reduce_aggressive",
	"output_reduce_ab",
	"tool_heavy",
	"provider_cache_long_session",
	"host_resource_long_workday",
}

// EvaluatePromotionGate applies the stricter release/default-promotion gate.
// It intentionally ignores synthetic categories: synthetic data may keep CI
// deterministic, but it cannot promote a savings mechanism into product default.
func EvaluatePromotionGate(report CorpusReport) PromotionGateReport {
	gate := PromotionGateReport{
		SessionsByClient:   map[string]int{},
		SessionsByWorkload: map[string]int{},
	}
	for _, c := range report.Categories {
		if c.Synthetic {
			continue
		}
		gate.RealCategories++
		gate.RealSessions += c.Sessions
		client := strings.TrimSpace(c.ClientFamily)
		workload := strings.TrimSpace(c.WorkloadClass)
		if client != "" {
			gate.SessionsByClient[client] += c.Sessions
		}
		if workload != "" {
			gate.SessionsByWorkload[workload] += c.Sessions
		}
		if c.EvidenceLevel != "live_operator" {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: evidence_level=%q is not live_operator", c.Category, c.EvidenceLevel))
		}
		if client == "" {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: missing client_family", c.Category))
		}
		if workload == "" {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: missing workload_class", c.Category))
		}
		if c.Metadata == nil || !c.Metadata.expectedMaxErrorsSet || c.Metadata.ExpectedMaxErrors != 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: expected_max_errors must be explicitly 0", c.Category))
		}
		if c.Metadata == nil || !c.Metadata.expectedReReadCountMaxSet || c.Metadata.ExpectedReReadCountMax < 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: expected_reread_count_max must be explicit", c.Category))
		}
		if c.Metadata == nil || !c.Metadata.expectedLatencyP95MaxSet || c.Metadata.ExpectedLatencyP95MaxMs <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: expected_latency_p95_max_ms must be explicit", c.Category))
		}
		if c.Metadata == nil || !categoryHasPromotionSavingsSignal(c.WorkloadClass, c.Metadata) {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: missing explicit positive savings signal for workload_class %s", c.Category, c.WorkloadClass))
		}
		for _, failure := range c.Failures {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: %s", c.Category, failure))
		}
	}
	if gate.RealCategories == 0 {
		gate.Failures = append(gate.Failures, "no real live_operator categories")
	}
	for client, want := range requiredPromotionClientSessions {
		if got := gate.SessionsByClient[client]; got < want {
			gate.Failures = append(gate.Failures, fmt.Sprintf("client %s sessions=%d < min=%d", client, got, want))
		}
	}
	for _, workload := range requiredPromotionWorkloads {
		if got := gate.SessionsByWorkload[workload]; got <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("missing workload_class %s", workload))
		}
	}
	gate.Passed = len(gate.Failures) == 0
	return gate
}

func categoryHasPromotionSavingsSignal(workload string, meta *CategoryMetadata) bool {
	if meta == nil {
		return false
	}
	switch strings.TrimSpace(workload) {
	case "provider_cache_long_session":
		return meta.ExpectedProviderCacheReadMin > 0
	case "output_reduce_aggressive":
		return meta.ExpectedOutputReduceAppliedMin > 0
	case "output_reduce_ab":
		return meta.ExpectedOutputReduceABPairsMin > 0 && meta.ExpectedOutputReduceABNetSavedMin > 0
	default:
		return meta.ExpectedSavingsMin > 0 || meta.ExpectedSavedTokensMin > 0
	}
}

// EvaluateMaxxGate applies the stricter whole-program max-out gate. It includes
// the release/default-promotion gate and then requires the mechanism-specific
// live workloads for chunk dedup, output-reduce, tool pruning, provider cache,
// and host-resource proof.
func EvaluateMaxxGate(report CorpusReport) MaxxGateReport {
	promotion := EvaluatePromotionGate(report)
	gate := MaxxGateReport{
		RealCategories:     promotion.RealCategories,
		RealSessions:       promotion.RealSessions,
		SessionsByClient:   cloneCountMap(promotion.SessionsByClient),
		SessionsByWorkload: cloneCountMap(promotion.SessionsByWorkload),
	}
	for _, failure := range promotion.Failures {
		gate.Failures = append(gate.Failures, "promotion: "+failure)
	}
	for _, workload := range requiredMaxxWorkloads {
		if got := gate.SessionsByWorkload[workload]; got <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("missing maxx workload_class %s", workload))
		}
	}
	for _, category := range report.Categories {
		if category.Synthetic || category.WorkloadClass != "output_reduce_aggressive" {
			continue
		}
		if category.OutputReduceApplied <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: output_reduce_applied=%d", category.Category, category.OutputReduceApplied))
		}
		if category.OutputTokens <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: output_reduce_aggressive missing observed output-token evidence", category.Category))
		}
	}
	for _, category := range report.Categories {
		if category.Synthetic || category.WorkloadClass != "ocrl_full_history" {
			continue
		}
		if category.OCRLApplied <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: ocrl_full_history applied=%d", category.Category, category.OCRLApplied))
		}
		if category.OCRLFullHistoryRows <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: ocrl_full_history full_history_rows=%d", category.Category, category.OCRLFullHistoryRows))
		}
		if category.OCRLCandidateCapsules <= 0 || category.OCRLArchiveExpansions <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: ocrl_full_history candidates=%d archive_expansions=%d", category.Category, category.OCRLCandidateCapsules, category.OCRLArchiveExpansions))
		}
		if category.OCRLSavedTokens <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: ocrl_full_history saved_tokens=%d", category.Category, category.OCRLSavedTokens))
		}
		if category.OCRLShadowOnly > 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: ocrl_full_history shadow_only_rows=%d", category.Category, category.OCRLShadowOnly))
		}
	}
	for _, category := range report.Categories {
		if category.Synthetic || category.WorkloadClass != "output_reduce_ab" {
			continue
		}
		if category.OutputReduceABPairs <= 0 || category.OutputReduceABPassedPairs != category.OutputReduceABPairs {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: output_reduce_ab pairs=%d passed=%d", category.Category, category.OutputReduceABPairs, category.OutputReduceABPassedPairs))
		}
		if category.OutputReduceABNetSaved <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: output_reduce_ab net_saved=%d", category.Category, category.OutputReduceABNetSaved))
		}
	}
	gate.Passed = len(gate.Failures) == 0
	return gate
}

func cloneCountMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// EvaluateCorpus walks the root directory and produces a CorpusReport.
// Categories are detected as immediate subdirectories that contain a
// `metadata.json`. Subdirectories without one are ignored with a warning
// to errOut so a maintainer who forgot the metadata file sees the hint.
func EvaluateCorpus(root string, errOut io.Writer) (CorpusReport, error) {
	report := CorpusReport{
		Root:               root,
		SessionsByClient:   map[string]int{},
		SessionsByWorkload: map[string]int{},
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return report, fmt.Errorf("read corpus root: %w", err)
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirs = append(dirs, e.Name())
	}
	sort.Strings(dirs)

	var totalOrig, totalSaved int64
	for _, name := range dirs {
		dir := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(dir, corpusCategoryMetadataFilename)); err != nil {
			if errOut != nil {
				fmt.Fprintf(errOut, "warn: skipping %s (no %s)\n", dir, corpusCategoryMetadataFilename)
			}
			continue
		}
		res, err := EvaluateCategory(dir, errOut)
		if err != nil {
			if errOut != nil {
				fmt.Fprintf(errOut, "warn: %v\n", err)
			}
			continue
		}
		report.Categories = append(report.Categories, res)
		report.TotalRequests += res.Requests
		if res.OrigTokens > 0 {
			totalOrig += res.OrigTokens
			totalSaved += res.SavedTokens
		}
		if res.Synthetic {
			report.HasSynthetic = true
		} else {
			report.HasReal = true
		}
		if res.ClientFamily != "" {
			report.SessionsByClient[res.ClientFamily] += res.Sessions
		}
		if res.WorkloadClass != "" {
			report.SessionsByWorkload[res.WorkloadClass] += res.Sessions
		}
	}
	if totalOrig > 0 {
		report.OverallRatio = float64(totalSaved) / float64(totalOrig)
	}
	return report, nil
}

// FormatCorpusReport renders the corpus report as a monospaced block.
func FormatCorpusReport(report CorpusReport) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Live corpus report: %s\n", report.Root))
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n")
	if len(report.Categories) == 0 {
		sb.WriteString("No categories found (each subdir needs metadata.json).\n")
		return sb.String()
	}
	for _, c := range report.Categories {
		tag := ""
		if c.Synthetic {
			tag = " [synthetic]"
		}
		sb.WriteString(fmt.Sprintf("\n--- %s%s ---\n", c.Category, tag))
		sb.WriteString(fmt.Sprintf("  sessions:     %d\n", c.Sessions))
		sb.WriteString(fmt.Sprintf("  requests:     %d\n", c.Requests))
		sb.WriteString(fmt.Sprintf("  orig tokens:  %d\n", c.OrigTokens))
		sb.WriteString(fmt.Sprintf("  saved tokens: %d\n", c.SavedTokens))
		if c.OrigTokens > 0 {
			sb.WriteString(fmt.Sprintf("  ratio:        %.2f%%\n", c.SavingsRatio*100))
		} else if c.SavedTokens > 0 {
			sb.WriteString("  ratio:        n/a (absolute saved-token gate)\n")
		} else {
			sb.WriteString("  ratio:        n/a\n")
		}
		sb.WriteString(fmt.Sprintf("  L0/L1/L2/L3:  %d / %d / %d / %d\n", c.Layer0Saved, c.Layer1Saved, c.Layer2Saved, c.Layer3Saved))
		sb.WriteString(fmt.Sprintf("  output tokens:%d\n", c.OutputTokens))
		sb.WriteString(fmt.Sprintf("  provider cache read/create/cached: %d / %d / %d\n", c.ProviderCacheReadTokens, c.ProviderCacheCreateTokens, c.ProviderCachedTokens))
		if c.OutputReduceApplied > 0 || c.OutputReduceInputOverhead > 0 {
			sb.WriteString(fmt.Sprintf("  output-reduce:%d observed=%d overhead=%d net_observed=%d\n",
				c.OutputReduceApplied, c.OutputTokens, c.OutputReduceInputOverhead, c.OutputReduceNetObserved))
		} else {
			sb.WriteString(fmt.Sprintf("  output-reduce:%d\n", c.OutputReduceApplied))
		}
		if c.OutputReduceABPairs > 0 || len(c.OutputReduceABFailures) > 0 {
			sb.WriteString(fmt.Sprintf("  output-reduce A/B: pairs=%d passed=%d output_saved=%d net=%d savings_min=%.2f%%\n",
				c.OutputReduceABPairs, c.OutputReduceABPassedPairs, c.OutputReduceABOutputSaved, c.OutputReduceABNetSaved, c.OutputReduceABSavingsPctMin))
		}
		if c.ToolPruneApplied > 0 || c.ToolPruneSavedTokens > 0 {
			sb.WriteString(fmt.Sprintf("  tool-prune:   applied=%d saved=%d\n", c.ToolPruneApplied, c.ToolPruneSavedTokens))
		}
		if c.OCRLCandidateCapsules > 0 || c.OCRLSavedTokens > 0 || c.OCRLApplied > 0 || c.OCRLShadowOnly > 0 {
			sb.WriteString(fmt.Sprintf("  OCRL:         applied=%d shadow=%d full_history=%d candidates=%d archive_expansions=%d saved=%d\n",
				c.OCRLApplied, c.OCRLShadowOnly, c.OCRLFullHistoryRows, c.OCRLCandidateCapsules, c.OCRLArchiveExpansions, c.OCRLSavedTokens))
		}
		sb.WriteString(fmt.Sprintf("  errors:       %d\n", c.ErrorCount))
		sb.WriteString(fmt.Sprintf("  re-reads:     %d\n", c.ReReadCount))
		if c.HostBudgetOKRows > 0 || c.HostBudgetIssueRows > 0 {
			sb.WriteString(fmt.Sprintf("  host budget:  ok=%d issues=%d\n", c.HostBudgetOKRows, c.HostBudgetIssueRows))
		}
		sb.WriteString(fmt.Sprintf("  latency p95:  %.1f ms\n", c.LatencyP95Ms))
		if c.ClientFamily != "" || c.WorkloadClass != "" {
			sb.WriteString(fmt.Sprintf("  client/work:  %s / %s\n", strOrUnset(c.ClientFamily), strOrUnset(c.WorkloadClass)))
		}
		if c.Metadata != nil && len(c.Metadata.ScenarioValidators) > 0 {
			sb.WriteString(fmt.Sprintf("  validators:   %s\n", strings.Join(c.Metadata.ScenarioValidators, ", ")))
		}
		if c.PlanReplay.RequestsWithPlan > 0 {
			sb.WriteString(fmt.Sprintf("  planner:      requests=%d decisions=%d expected=%d active=%d/%d missed=%d bypass-hit=%d blocked=%d\n",
				c.PlanReplay.RequestsWithPlan,
				c.PlanReplay.Decisions,
				c.PlanReplay.ExpectedSavingsTokens,
				c.PlanReplay.ObservedActive,
				c.PlanReplay.ExpectedActive,
				c.PlanReplay.MissedActive,
				c.PlanReplay.BypassApplied,
				c.PlanReplay.SafetyBlocked,
			))
		}
		if len(c.LayerCombinations) > 0 {
			sb.WriteString("  combos:\n")
			for _, key := range sortedLayerCombinationKeys(c.LayerCombinations) {
				combo := c.LayerCombinations[key]
				ratioText := "n/a"
				if combo.OrigTokens > 0 {
					ratio := float64(combo.SavedTokens) / float64(combo.OrigTokens) * 100
					ratioText = fmt.Sprintf("%.2f%%", ratio)
				}
				sb.WriteString(fmt.Sprintf("    %-18s requests=%d saved=%d ratio=%s output=%d errors=%d\n",
					key, combo.Requests, combo.SavedTokens, ratioText, combo.OutputTokens, combo.Errors))
			}
		}
		sb.WriteString(fmt.Sprintf("  evidence:     %s\n", c.EvidenceLevel))
		if c.GateConfigured {
			if len(c.Failures) == 0 {
				sb.WriteString("  gate:         PASS\n")
			} else {
				sb.WriteString("  gate:         FAIL\n")
				for _, f := range c.Failures {
					sb.WriteString(fmt.Sprintf("    - %s\n", f))
				}
			}
		} else {
			sb.WriteString("  gate:         (no expectations declared)\n")
		}
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", 60))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Total requests: %d\n", report.TotalRequests))
	sb.WriteString(fmt.Sprintf("Overall ratio:  %.2f%% (known denominators only)\n", report.OverallRatio*100))
	if report.PromotionGate != nil {
		sb.WriteString("\nPromotion gate\n")
		if report.PromotionGate.Passed {
			sb.WriteString("  gate:         PASS\n")
		} else {
			sb.WriteString("  gate:         FAIL\n")
			for _, f := range report.PromotionGate.Failures {
				sb.WriteString(fmt.Sprintf("    - %s\n", f))
			}
		}
		sb.WriteString(fmt.Sprintf("  real sessions:%d\n", report.PromotionGate.RealSessions))
		sb.WriteString(fmt.Sprintf("  clients:      %s\n", formatCountMap(report.PromotionGate.SessionsByClient)))
		sb.WriteString(fmt.Sprintf("  workloads:    %s\n", formatCountMap(report.PromotionGate.SessionsByWorkload)))
	}
	if report.MaxxGate != nil {
		sb.WriteString("\nMaxx gate\n")
		if report.MaxxGate.Passed {
			sb.WriteString("  gate:         PASS\n")
		} else {
			sb.WriteString("  gate:         FAIL\n")
			for _, f := range report.MaxxGate.Failures {
				sb.WriteString(fmt.Sprintf("    - %s\n", f))
			}
		}
		sb.WriteString(fmt.Sprintf("  real sessions:%d\n", report.MaxxGate.RealSessions))
		sb.WriteString(fmt.Sprintf("  clients:      %s\n", formatCountMap(report.MaxxGate.SessionsByClient)))
		sb.WriteString(fmt.Sprintf("  workloads:    %s\n", formatCountMap(report.MaxxGate.SessionsByWorkload)))
	}
	if report.HasSynthetic && !report.HasReal {
		sb.WriteString("\nNOTE: corpus is synthetic-only. See docs/live-corpus-policy.md for the\n")
		sb.WriteString("operator-driven path to a real-session corpus (T118b).\n")
	}
	return sb.String()
}

func strOrUnset(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unset"
	}
	return value
}

func formatCountMap(values map[string]int) string {
	if len(values) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return strings.Join(parts, ", ")
}

// CorpusGate runs EvaluateCorpus and treats any per-category failure as
// a non-zero exit code; used as the CI hook for the live-corpus
// regression check. When the corpus is empty (no categories found),
// CorpusGate exits non-zero so the gap is visible.
func CorpusGate(root string, stdout, stderr io.Writer) int {
	report, err := EvaluateCorpus(root, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "benchmark-corpus: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, FormatCorpusReport(report))
	if len(report.Categories) == 0 {
		fmt.Fprintf(stderr, "benchmark-corpus: corpus root %s has no categories\n", root)
		return 1
	}
	failed := false
	for _, c := range report.Categories {
		if len(c.Failures) > 0 {
			failed = true
		}
	}
	if failed {
		fmt.Fprintf(stdout, "benchmark-corpus: FAIL on %s\n", root)
		return 1
	}
	fmt.Fprintf(stdout, "benchmark-corpus: PASS on %s\n", root)
	return 0
}

// CorpusReportJSON renders the report as canonical JSON for machine use.
func CorpusReportJSON(report CorpusReport) (string, error) {
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// runBenchmarkCorpus is the CLI entrypoint hooked from main.go.
func runBenchmarkCorpus(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: benchmark-corpus <corpus-root> [--check] [--json] [--promotion-check] [--maxx-check]")
		return 2
	}
	check := false
	jsonOut := false
	promotionCheck := false
	maxxCheck := false
	var root string
	for _, a := range args {
		switch a {
		case "--check":
			check = true
		case "--json":
			jsonOut = true
		case "--promotion-check":
			promotionCheck = true
		case "--maxx-check":
			maxxCheck = true
		default:
			if strings.HasPrefix(a, "--") {
				fmt.Fprintf(os.Stderr, "unknown flag %q\n", a)
				return 2
			}
			if root != "" {
				fmt.Fprintln(os.Stderr, "benchmark-corpus takes a single root argument")
				return 2
			}
			root = a
		}
	}
	if root == "" {
		fmt.Fprintln(os.Stderr, "benchmark-corpus: corpus root required")
		return 2
	}
	if check && !promotionCheck && !maxxCheck {
		return CorpusGate(root, os.Stdout, os.Stderr)
	}
	report, err := EvaluateCorpus(root, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark-corpus: %v\n", err)
		return 1
	}
	if promotionCheck {
		gate := EvaluatePromotionGate(report)
		report.PromotionGate = &gate
	}
	if maxxCheck {
		gate := EvaluateMaxxGate(report)
		report.MaxxGate = &gate
	}
	if jsonOut {
		s, err := CorpusReportJSON(report)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchmark-corpus: %v\n", err)
			return 1
		}
		fmt.Print(s)
		if promotionCheck && !report.PromotionGate.Passed {
			return 1
		}
		if maxxCheck && !report.MaxxGate.Passed {
			return 1
		}
		if check {
			return corpusReportExitCode(report, root)
		}
		return 0
	}
	fmt.Print(FormatCorpusReport(report))
	if promotionCheck && !report.PromotionGate.Passed {
		fmt.Fprintf(os.Stdout, "benchmark-corpus promotion: FAIL on %s\n", root)
		return 1
	}
	if maxxCheck && !report.MaxxGate.Passed {
		fmt.Fprintf(os.Stdout, "benchmark-corpus maxx: FAIL on %s\n", root)
		return 1
	}
	if check {
		return corpusReportExitCode(report, root)
	}
	return 0
}

func corpusReportExitCode(report CorpusReport, root string) int {
	if len(report.Categories) == 0 {
		fmt.Fprintf(os.Stderr, "benchmark-corpus: corpus root %s has no categories\n", root)
		return 1
	}
	for _, c := range report.Categories {
		if len(c.Failures) > 0 {
			fmt.Fprintf(os.Stdout, "benchmark-corpus: FAIL on %s\n", root)
			return 1
		}
	}
	fmt.Fprintf(os.Stdout, "benchmark-corpus: PASS on %s\n", root)
	return 0
}

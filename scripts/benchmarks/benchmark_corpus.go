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

const corpusCategoryMetadataFilename = "metadata.json"

// CategoryMetadata is the minimal description a maintainer commits next
// to a corpus category so the gate has expectations to measure against.
// Only ExpectedSavingsMin is mandatory for the gate; everything else is
// human context that gets rendered in reports.
type CategoryMetadata struct {
	Category                        string   `json:"category"`
	Description                     string   `json:"description"`
	Synthetic                       bool     `json:"synthetic"`
	EvidenceLevel                   string   `json:"evidence_level"`
	ClientFamily                    string   `json:"client_family,omitempty"`
	WorkloadClass                   string   `json:"workload_class,omitempty"`
	Language                        string   `json:"language"`
	ToolMix                         string   `json:"tool_mix"`
	ExpectedSavingsMin              float64  `json:"expected_savings_min"`
	ExpectedSavingsMax              float64  `json:"expected_savings_max"`
	ExpectedRequestCount            int      `json:"expected_request_count"`
	ExpectedLayer2Optional          bool     `json:"expected_layer2_optional"`
	ExpectedMaxErrors               int      `json:"expected_max_errors,omitempty"`
	ExpectedLatencyP95MaxMs         float64  `json:"expected_latency_p95_max_ms,omitempty"`
	ExpectedProviderCacheReadMin    int64    `json:"expected_provider_cache_read_min,omitempty"`
	ExpectedOutputReduceAppliedMin  int      `json:"expected_output_reduce_applied_min,omitempty"`
	ExpectedReReadCountMax          int      `json:"expected_reread_count_max,omitempty"`
	ExpectedPlannerMissedMax        int      `json:"expected_planner_missed_max,omitempty"`
	ExpectedPlannerBypassAppliedMax int      `json:"expected_planner_bypass_applied_max,omitempty"`
	ScenarioValidators              []string `json:"scenario_validators,omitempty"`
	Notes                           string   `json:"notes"`

	expectedMaxErrorsSet      bool
	expectedReReadCountMaxSet bool
	expectedLatencyP95MaxSet  bool
}

// CategoryResult is the per-category outcome of one gate evaluation.
type CategoryResult struct {
	Category                  string                               `json:"category"`
	Path                      string                               `json:"path"`
	Sessions                  int                                  `json:"sessions"`
	Requests                  int                                  `json:"requests"`
	OrigTokens                int64                                `json:"orig_tokens"`
	SavedTokens               int64                                `json:"saved_tokens"`
	SavingsRatio              float64                              `json:"savings_ratio"`
	Layer0Saved               int64                                `json:"layer0_saved"`
	Layer1Saved               int64                                `json:"layer1_saved"`
	Layer2Saved               int64                                `json:"layer2_saved"`
	Layer3Saved               int64                                `json:"layer3_saved"`
	OutputTokens              int64                                `json:"output_tokens"`
	ProviderCacheReadTokens   int64                                `json:"provider_cache_read_tokens"`
	ProviderCacheCreateTokens int64                                `json:"provider_cache_create_tokens"`
	ProviderCachedTokens      int64                                `json:"provider_cached_tokens"`
	OutputReduceApplied       int                                  `json:"output_reduce_applied"`
	ErrorCount                int                                  `json:"error_count"`
	ReReadCount               int                                  `json:"reread_count"`
	LatencyP95Ms              float64                              `json:"latency_p95_ms"`
	PlanReplay                planReplayAggregate                  `json:"plan_replay"`
	LayerCombinations         map[string]layerCombinationAggregate `json:"layer_combinations,omitempty"`
	EvidenceLevel             string                               `json:"evidence_level"`
	Synthetic                 bool                                 `json:"synthetic"`
	ClientFamily              string                               `json:"client_family,omitempty"`
	WorkloadClass             string                               `json:"workload_class,omitempty"`
	Failures                  []string                             `json:"failures,omitempty"`
	GateConfigured            bool                                 `json:"gate_configured"`
	Metadata                  *CategoryMetadata                    `json:"metadata,omitempty"`
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
	agg, err := AggregateSessionsFromPath(dir, errOut)
	if err != nil {
		return CategoryResult{}, fmt.Errorf("aggregate %s: %w", dir, err)
	}
	sessions, err := countSessionFiles(dir)
	if err != nil {
		return CategoryResult{}, err
	}
	ratio := 0.0
	if agg.origTokens > 0 {
		ratio = float64(agg.savedTokens) / float64(agg.origTokens)
	}
	res := CategoryResult{
		Category:                  meta.Category,
		Path:                      dir,
		Sessions:                  sessions,
		Requests:                  agg.requests,
		OrigTokens:                agg.origTokens,
		SavedTokens:               agg.savedTokens,
		SavingsRatio:              ratio,
		Layer0Saved:               agg.layer0Saved,
		Layer1Saved:               agg.layer1Saved,
		Layer2Saved:               agg.layer2Saved,
		Layer3Saved:               agg.layer3Saved,
		OutputTokens:              agg.outputTokenSum,
		ProviderCacheReadTokens:   agg.cacheReadSum,
		ProviderCacheCreateTokens: agg.cacheCreateSum,
		ProviderCachedTokens:      agg.providerCachedSum,
		OutputReduceApplied:       agg.outputReduceApplied,
		ErrorCount:                agg.errorCount,
		ReReadCount:               agg.reReadCount,
		LatencyP95Ms:              percentileFloat64(agg.latenciesMs, 0.95),
		PlanReplay:                clonePlanReplayAggregate(agg.planReplay),
		LayerCombinations:         cloneLayerCombinations(agg.layerCombinations),
		EvidenceLevel:             normalizeEvidenceLevel(meta),
		Synthetic:                 meta.Synthetic,
		ClientFamily:              strings.TrimSpace(meta.ClientFamily),
		WorkloadClass:             strings.TrimSpace(meta.WorkloadClass),
		GateConfigured: meta.ExpectedSavingsMin > 0 ||
			meta.ExpectedRequestCount > 0 ||
			meta.ExpectedLatencyP95MaxMs > 0 ||
			meta.ExpectedProviderCacheReadMin > 0 ||
			meta.ExpectedOutputReduceAppliedMin > 0 ||
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
			if res.Layer0Saved <= 0 && res.Layer1Saved <= 0 {
				failures = append(failures, "scenario tool_heavy: expected Layer 0 or Layer 1 savings")
			}
		case "cache_reuse":
			if res.Layer3Saved <= 0 && res.ProviderCacheReadTokens <= 0 && res.ProviderCachedTokens <= 0 {
				failures = append(failures, "scenario cache_reuse: expected Layer 3 or provider cache evidence")
			}
		case "output_reduce":
			if res.OutputReduceApplied <= 0 {
				failures = append(failures, "scenario output_reduce: expected output-reduce application")
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
		case "layer_combo_diversity":
			if len(res.LayerCombinations) < 2 {
				failures = append(failures, fmt.Sprintf("scenario layer_combo_diversity: combinations=%d", len(res.LayerCombinations)))
			}
		case "l2_summary":
			if res.Layer2Saved <= 0 {
				failures = append(failures, "scenario l2_summary: expected Layer 2 savings")
			}
		default:
			failures = append(failures, fmt.Sprintf("unknown scenario validator %q", raw))
		}
	}
	return failures
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
		if c.Metadata == nil || c.Metadata.ExpectedSavingsMin <= 0 {
			gate.Failures = append(gate.Failures, fmt.Sprintf("%s: expected_savings_min must be explicit and positive", c.Category))
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
		totalOrig += res.OrigTokens
		totalSaved += res.SavedTokens
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
		sb.WriteString(fmt.Sprintf("  ratio:        %.2f%%\n", c.SavingsRatio*100))
		sb.WriteString(fmt.Sprintf("  L0/L1/L2/L3:  %d / %d / %d / %d\n", c.Layer0Saved, c.Layer1Saved, c.Layer2Saved, c.Layer3Saved))
		sb.WriteString(fmt.Sprintf("  output tokens:%d\n", c.OutputTokens))
		sb.WriteString(fmt.Sprintf("  provider cache read/create/cached: %d / %d / %d\n", c.ProviderCacheReadTokens, c.ProviderCacheCreateTokens, c.ProviderCachedTokens))
		sb.WriteString(fmt.Sprintf("  output-reduce:%d\n", c.OutputReduceApplied))
		sb.WriteString(fmt.Sprintf("  errors:       %d\n", c.ErrorCount))
		sb.WriteString(fmt.Sprintf("  re-reads:     %d\n", c.ReReadCount))
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
				ratio := 0.0
				if combo.OrigTokens > 0 {
					ratio = float64(combo.SavedTokens) / float64(combo.OrigTokens) * 100
				}
				sb.WriteString(fmt.Sprintf("    %-18s requests=%d saved=%d ratio=%.2f%% output=%d errors=%d\n",
					key, combo.Requests, combo.SavedTokens, ratio, combo.OutputTokens, combo.Errors))
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
	sb.WriteString(fmt.Sprintf("Overall ratio:  %.2f%%\n", report.OverallRatio*100))
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
		fmt.Fprintln(os.Stderr, "Usage: benchmark-corpus <corpus-root> [--check] [--json] [--promotion-check]")
		return 2
	}
	check := false
	jsonOut := false
	promotionCheck := false
	var root string
	for _, a := range args {
		switch a {
		case "--check":
			check = true
		case "--json":
			jsonOut = true
		case "--promotion-check":
			promotionCheck = true
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
	if check && !promotionCheck {
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

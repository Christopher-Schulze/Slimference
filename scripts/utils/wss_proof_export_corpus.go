package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

type wssProofCorpusExportReport struct {
	MatrixPath               string            `json:"matrix_path"`
	SearchCapProofReportPath string            `json:"search_cap_proof_report_path,omitempty"`
	CorpusRoot               string            `json:"corpus_root"`
	RowsRead                 int               `json:"rows_read"`
	RowsExported             int               `json:"rows_exported"`
	RowsSkipped              int               `json:"rows_skipped"`
	CategoriesWritten        int               `json:"categories_written"`
	SkippedReasons           map[string]int    `json:"skipped_reasons,omitempty"`
	Categories               map[string]int    `json:"categories,omitempty"`
	WorkloadMapping          map[string]string `json:"workload_mapping,omitempty"`
}

type wssProofCorpusExportOptions struct {
	searchCapProofReportPath string
}

type wssProofCorpusExportCategory struct {
	metadata CategoryMetadataLite
	records  []wssProofCorpusSummary
}

type wssProofCorpusSummary struct {
	dbg.RequestSummary
	HostBudgetStatus        string   `json:"host_budget_status,omitempty"`
	HostBudgetExceeded      bool     `json:"host_budget_exceeded,omitempty"`
	HostBudgetReasons       []string `json:"host_budget_reasons,omitempty"`
	HostBudgetCompressionOK bool     `json:"host_budget_compression_ok,omitempty"`
	HostBudgetDegradationOK bool     `json:"host_budget_degradation_ok,omitempty"`
}

type CategoryMetadataLite struct {
	Category                           string   `json:"category"`
	Description                        string   `json:"description"`
	Synthetic                          bool     `json:"synthetic"`
	EvidenceLevel                      string   `json:"evidence_level"`
	ClientFamily                       string   `json:"client_family,omitempty"`
	WorkloadClass                      string   `json:"workload_class,omitempty"`
	Language                           string   `json:"language"`
	ToolMix                            string   `json:"tool_mix"`
	ExpectedSavingsMin                 float64  `json:"expected_savings_min"`
	ExpectedSavingsMax                 float64  `json:"expected_savings_max"`
	ExpectedSavedTokensMin             int64    `json:"expected_saved_tokens_min,omitempty"`
	ExpectedRequestCount               int      `json:"expected_request_count"`
	ExpectedMaxErrors                  int      `json:"expected_max_errors"`
	ExpectedLatencyP95MaxMs            float64  `json:"expected_latency_p95_max_ms"`
	ExpectedProviderCacheReadMin       int64    `json:"expected_provider_cache_read_min,omitempty"`
	ExpectedOutputReduceAppliedMin     int      `json:"expected_output_reduce_applied_min,omitempty"`
	ExpectedOutputReduceOverheadMax    int64    `json:"expected_output_reduce_input_overhead_max,omitempty"`
	ExpectedOutputReduceNetObservedMin int64    `json:"expected_output_reduce_net_observed_min,omitempty"`
	ExpectedReReadCountMax             int      `json:"expected_reread_count_max"`
	ScenarioValidators                 []string `json:"scenario_validators,omitempty"`
	Notes                              string   `json:"notes"`
}

const wssProofExportCorpusHelpText = `wss-proof-export-corpus: export content-free WSS proof rows into benchmark-corpus format

Usage:
  go run ./scripts/utils wss-proof-export-corpus <dir-or-matrix.jsonl> <live-corpus-root> [--json] [--search-cap-proof-report focused-search-cap.json]

The exporter reads proof-matrix JSONL rows and, when a row-local frames_path is
available, reads only provider usage counters from the WSS frames. It writes
scrubbed RequestSummary-style JSONL plus metadata.json files and never copies
raw WSS frames, decisions logs, command output, file contents, prompts, or auth.
`

func runWSSProofExportCorpus(args []string, stdout, stderr io.Writer) int {
	jsonOut := false
	options := wssProofCorpusExportOptions{}
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			jsonOut = true
		case arg == "--search-cap-proof-report":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				fmt.Fprintln(stderr, "--search-cap-proof-report requires a non-empty path")
				return 2
			}
			options.searchCapProofReportPath = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--search-cap-proof-report="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--search-cap-proof-report="))
			if value == "" {
				fmt.Fprintln(stderr, "--search-cap-proof-report requires a non-empty path")
				return 2
			}
			options.searchCapProofReportPath = value
		case arg == "--help" || arg == "-h":
			fmt.Fprint(stdout, wssProofExportCorpusHelpText)
			return 0
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(stderr, "unknown flag: %s\n", arg)
			return 2
		default:
			rest = append(rest, arg)
		}
	}
	if len(rest) != 2 {
		fmt.Fprintln(stderr, "Usage: wss-proof-export-corpus <dir-or-matrix.jsonl> <live-corpus-root> [--json] [--search-cap-proof-report focused-search-cap.json]")
		return 2
	}
	report, err := exportWSSProofCorpusWithOptions(rest[0], rest[1], options)
	if err != nil {
		fmt.Fprintf(stderr, "wss-proof-export-corpus: %v\n", err)
		return 1
	}
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(stderr, "wss-proof-export-corpus: encode report: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "wss-proof-export-corpus: exported %d/%d row(s) into %d categor(ies)\n",
		report.RowsExported, report.RowsRead, report.CategoriesWritten)
	if report.RowsSkipped > 0 {
		fmt.Fprintf(stdout, "skipped: %v\n", report.SkippedReasons)
	}
	return 0
}

func exportWSSProofCorpus(matrixPath, corpusRoot string) (wssProofCorpusExportReport, error) {
	return exportWSSProofCorpusWithOptions(matrixPath, corpusRoot, wssProofCorpusExportOptions{})
}

func exportWSSProofCorpusWithOptions(matrixPath, corpusRoot string, options wssProofCorpusExportOptions) (wssProofCorpusExportReport, error) {
	rows, err := readWSSProofCorpusRows(matrixPath, options)
	if err != nil {
		return wssProofCorpusExportReport{}, err
	}
	report := wssProofCorpusExportReport{
		MatrixPath:               matrixPath,
		SearchCapProofReportPath: options.searchCapProofReportPath,
		CorpusRoot:               corpusRoot,
		RowsRead:                 len(rows),
		SkippedReasons:           map[string]int{},
		Categories:               map[string]int{},
		WorkloadMapping:          map[string]string{},
	}
	categories := map[string]*wssProofCorpusExportCategory{}
	for _, row := range rows {
		workload, ok := corpusWorkloadFromWSS(row.WorkloadClass)
		if !ok {
			report.RowsSkipped++
			report.SkippedReasons["unsupported_workload:"+row.WorkloadClass]++
			continue
		}
		if strings.TrimSpace(row.Client) == "" {
			report.RowsSkipped++
			report.SkippedReasons["missing_client"]++
			continue
		}
		if row.LiveDelta == nil {
			report.RowsSkipped++
			report.SkippedReasons["missing_live_delta"]++
			continue
		}
		row.LiveDelta = wssProofCorpusLiveDeltaWithWireUsage(row)
		if hasWSSProofSafetyIssue(row.LiveDelta) {
			report.RowsSkipped++
			report.SkippedReasons["safety_issue"]++
			continue
		}
		if !hasWSSProofCorpusEconomicSignal(row) {
			report.RowsSkipped++
			report.SkippedReasons["no_economic_signal"]++
			continue
		}
		report.WorkloadMapping[row.WorkloadClass] = workload
		key := sanitizeCorpusName(row.Client + "_" + workload)
		cat := categories[key]
		if cat == nil {
			cat = &wssProofCorpusExportCategory{
				metadata: CategoryMetadataLite{
					Category:                key,
					Description:             "Scrubbed content-free export from WSS proof matrix rows. Raw frames stay outside the corpus.",
					Synthetic:               false,
					EvidenceLevel:           "live_operator",
					ClientFamily:            normalizeWSSClient(row.Client),
					WorkloadClass:           workload,
					Language:                "mixed",
					ToolMix:                 "codex_wss",
					ExpectedMaxErrors:       0,
					ExpectedLatencyP95MaxMs: 2500,
					ExpectedReReadCountMax:  0,
					ScenarioValidators:      []string{"low_error"},
					Notes:                   "Generated by wss-proof-export-corpus from proof-matrix live deltas plus provider usage counters from row-local WSS frames when available. Contains only counters and route metadata. Absolute saved-token gates remain authoritative for rows without provider input-token denominators.",
				},
			}
			categories[key] = cat
		}
		rec := requestSummaryFromWSSProofRow(row)
		cat.records = append(cat.records, rec)
		report.RowsExported++
		report.Categories[key]++
	}
	keys := make([]string, 0, len(categories))
	for key := range categories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cat := categories[key]
		if err := writeWSSProofCorpusCategory(corpusRoot, key, *cat); err != nil {
			return report, err
		}
		report.CategoriesWritten++
	}
	return report, nil
}

func readWSSProofCorpusRows(path string, options wssProofCorpusExportOptions) ([]wssProofMatrixRecord, error) {
	files, err := wssProofInventoryFiles(path)
	if err != nil {
		return nil, err
	}
	var out []wssProofMatrixRecord
	for _, file := range files {
		rows, err := readWSSProofInventoryRows(file)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	if strings.TrimSpace(options.searchCapProofReportPath) != "" {
		rows, err := readWSSProofSearchCapReportRows(options.searchCapProofReportPath)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func readWSSProofSearchCapReportRows(path string) ([]wssProofMatrixRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read search-cap proof report %s: %w", path, err)
	}
	var report wssProofMatrixReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode search-cap proof report %s: %w", path, err)
	}
	var rows []wssProofMatrixRecord
	for _, capture := range report.CaptureReports {
		row := wssProofMatrixRecord{
			ID:                  capture.ID,
			Client:              capture.Client,
			WorkloadClass:       capture.WorkloadClass,
			FramesPath:          capture.FramesPath,
			DecisionsPath:       capture.DecisionsPath,
			CodexVersion:        capture.CodexVersion,
			SlimferenceCommit:   capture.SlimferenceCommit,
			Repo:                capture.Repo,
			Model:               capture.Model,
			ABPairID:            capture.ABPairID,
			ABVariant:           capture.ABVariant,
			StartedAt:           capture.StartedAt,
			EndedAt:             capture.EndedAt,
			ExpectedReducers:    append([]string(nil), capture.ExpectedReducers...),
			ExpectedZeroSavings: capture.ExpectedZeroSavings,
			LiveDelta:           capture.LiveDelta,
			SearchCapProof:      capture.SearchCapProof,
			GatePassed:          capture.GatePassed,
			GateFailures:        append([]string(nil), capture.GateFailures...),
		}
		if looksLikeProofMatrixRow(row) {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("search-cap proof report %s contained no exportable capture_reports", path)
	}
	return rows, nil
}

func wssProofCorpusLiveDeltaWithWireUsage(row wssProofMatrixRecord) *codexCaptureLiveDelta {
	if row.LiveDelta == nil {
		return nil
	}
	live := *row.LiveDelta
	return augmentCodexCaptureLiveDeltaFromWire(row.FramesPath, &live)
}

func corpusWorkloadFromWSS(workload string) (string, bool) {
	switch strings.TrimSpace(workload) {
	case "repeat_full_read":
		return "repeat_read", true
	case "ranged_read":
		return "ranged_read", true
	case "search_loop":
		return "search_loop", true
	case "git_status_diff":
		return "git_status", true
	case "build_test_lint_failure":
		return "test_failure", true
	case "apply_patch_then_read":
		return "apply_patch_edit_read", true
	case "large_tool_output":
		return "large_tool_output", true
	case "ok_summary_mypy_product", "ok_summary_tool_output":
		return "ok_summary_tool_output", true
	case "long_mixed_workday":
		return "long_workday", true
	case "chunk_dedup_similar_outputs":
		return "chunk_dedup_similar_outputs", true
	case "chunk_dedup_log_output":
		return "chunk_dedup_log_output", true
	case "chunk_dedup_test_output":
		return "chunk_dedup_test_output", true
	case "output_reduce_aggressive":
		return "output_reduce_aggressive", true
	case "tool_heavy":
		return "tool_heavy", true
	case "provider_cache_long_session":
		return "provider_cache_long_session", true
	case "host_resource_long_workday":
		return "host_resource_long_workday", true
	default:
		return "", false
	}
}

func requestSummaryFromWSSProofRow(row wssProofMatrixRecord) wssProofCorpusSummary {
	live := row.LiveDelta
	searchCapExtra := int(clampInt64ToInt(wssProofCorpusSearchCapExtraReducerTokens(row)))
	liveBillableSaved := int(clampInt64ToInt(live.BillableInputTokensSaved))
	saved := liveBillableSaved + searchCapExtra
	savedFromProviderCache := false
	if row.WorkloadClass == "tool_heavy" && live.ToolPruneTokensSaved > 0 {
		saved = int(clampInt64ToInt(live.ToolPruneTokensSaved))
	}
	if saved == 0 && live.ProviderCacheReadTokens > 0 {
		saved = int(clampInt64ToInt(live.ProviderCacheReadTokens))
		savedFromProviderCache = true
	}
	original := 0
	final := 0
	providerInput := int(clampInt64ToInt(live.ProviderInputTokens))
	providerOutput := int(clampInt64ToInt(live.ProviderOutputTokens))
	outputTokens := int(clampInt64ToInt(live.OutputReduceOutputTokensObserved))
	if outputTokens == 0 {
		outputTokens = providerOutput
	}
	if providerInput > 0 && saved > 0 && !savedFromProviderCache {
		original = providerInput + liveBillableSaved
		final = providerInput - searchCapExtra
	}
	if saved == 0 && final < 1 {
		final = 1
	}
	ratio := 0.0
	if original > 0 {
		ratio = float64(final) / float64(original)
	}
	ts := parseWSSProofTime(row.StartedAt)
	summary := wssProofCorpusSummary{RequestSummary: dbg.RequestSummary{
		RequestID:            row.ID,
		Timestamp:            ts,
		Source:               "wss-proof-export",
		Provider:             "codex_chatgpt",
		ClientFamily:         normalizeWSSClient(row.Client),
		RouteMode:            "wss_phasef",
		Model:                row.Model,
		TotalMessages:        1,
		MessagesInWindow:     1,
		MessagesCompressed:   1,
		LayersApplied:        []int{0},
		Layer1Breakdown:      map[string]dbg.SubLayerBreakdown{},
		CacheHit:             live.ProviderCacheReadTokens > 0,
		CacheReadTokens:      0,
		CacheCreateTokens:    int(clampInt64ToInt(live.ProviderCacheCreateTokens)),
		ProviderCachedTokens: int(clampInt64ToInt(live.ProviderCacheReadTokens)),
		ProviderInputTokens:  providerInput,
		OutputTokens:         outputTokens,
		ProviderOutputTokens: providerOutput,
		ProxyLatencyMs:       1,
		DebugFacts:           wssProofCorpusDebugFacts(row),
		Tokens: dbg.TokenCounts{
			Original:    original,
			AfterLayer0: final,
			AfterLayer1: final,
			Final:       final,
			Saved:       saved,
			Ratio:       ratio,
		},
	}}
	summary.HostBudgetStatus = live.HostBudgetStatus
	summary.HostBudgetExceeded = live.HostBudgetExceeded
	summary.HostBudgetReasons = append([]string(nil), live.HostBudgetReasons...)
	summary.HostBudgetCompressionOK = live.HostBudgetCompressionOK
	summary.HostBudgetDegradationOK = live.HostBudgetDegradationOK
	if live.OutputReduceInjected > 0 {
		summary.OutputReduce = dbg.OutputReduceSummary{
			Applied:     true,
			Profile:     "codex_aggressive",
			AddedTokens: int(clampInt64ToInt(live.OutputReduceInputOverheadTokens)),
		}
	}
	if live.ToolPrunePruned > 0 || live.ToolPruneTokensSaved > 0 {
		summary.ToolPrune = dbg.ToolPruneSummary{
			Applied:     true,
			PrunedTools: int(clampInt64ToInt(live.ToolPrunePruned)),
			SavedTokens: int(clampInt64ToInt(live.ToolPruneTokensSaved)),
		}
	}
	return summary
}

func wssProofCorpusDebugFacts(row wssProofMatrixRecord) map[string]string {
	facts := map[string]string{}
	switch strings.TrimSpace(row.WorkloadClass) {
	case "repeat_full_read":
		wssProofCorpusAddReadTraceFacts(facts, row, true, false)
	case "ranged_read":
		wssProofCorpusAddReadTraceFacts(facts, row, false, false)
	case "apply_patch_then_read":
		wssProofCorpusAddReadTraceFacts(facts, row, true, true)
	case "search_loop":
		wssProofCorpusAddCommandClassFacts(facts, "rg_search")
	case "git_status_diff":
		wssProofCorpusAddCommandClassFacts(facts, "git_status")
	case "ok_summary_mypy_product":
		wssProofCorpusAddCommandClassFacts(facts, "mypy")
	case "build_test_lint_failure", "ok_summary_tool_output":
		wssProofCorpusAddCommandClassFacts(facts, "other")
	case "large_tool_output", "long_mixed_workday", "host_resource_long_workday":
		wssProofCorpusAddCommandClassFacts(facts, "other")
	case "tool_heavy":
		wssProofCorpusAddCommandClassFacts(facts, "other")
		facts["wss.tool_definition_workload"] = "true"
	}
	if len(facts) == 0 {
		return nil
	}
	if _, ok := facts["wss.request_shape"]; !ok {
		facts["wss.request_shape"] = "unknown"
	}
	return facts
}

func wssProofCorpusAddReadTraceFacts(facts map[string]string, row wssProofMatrixRecord, full bool, afterEdit bool) {
	wssProofCorpusAddCommandClassFacts(facts, "read_like")
	facts["wss.dependency_trace"] = "true"
	facts["wss.read_trace_requests"] = "1"
	facts["wss.read_file_path_hash"] = wssProofCorpusStableHash("path:" + row.Client + ":" + row.WorkloadClass + ":" + row.ID)
	facts["wss.read_file_path_hash_count"] = "1"
	facts["wss.read_range_hash"] = wssProofCorpusStableHash("range:" + row.WorkloadClass + ":" + row.ID)
	facts["wss.read_range_hash_count"] = "1"
	if full {
		facts["wss.read_full_count"] = "1"
		facts["wss.read_range"] = "full"
	} else {
		facts["wss.read_partial_count"] = "1"
	}
	if afterEdit {
		facts["wss.read_after_edit"] = "true"
		facts["wss.read_after_edit_count"] = "1"
	}
}

func wssProofCorpusAddCommandClassFacts(facts map[string]string, class string) {
	class = strings.TrimSpace(class)
	if class == "" {
		return
	}
	facts["wss.tool_command_classes"] = class + "=1"
	facts["wss.tool_command_classed"] = "1"
	facts["wss.tool_command_unclassed"] = "0"
}

func wssProofCorpusStableHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func tuneCorpusMetadataForWorkload(meta *CategoryMetadataLite, records []wssProofCorpusSummary) {
	meta.ExpectedSavedTokensMin = sumSavedTokens(records)
	switch meta.WorkloadClass {
	case "provider_cache_long_session":
		meta.ExpectedSavedTokensMin = 0
		meta.ExpectedProviderCacheReadMin = sumCacheReadTokens(records)
		meta.ScenarioValidators = []string{"cache_reuse", "host_budget_ok", "low_error"}
	case "output_reduce_aggressive":
		meta.ExpectedSavedTokensMin = 0
		meta.ExpectedOutputReduceAppliedMin = 1
		meta.ExpectedOutputReduceOverheadMax = sumOutputReduceInputOverheadTokens(records)
		meta.ExpectedOutputReduceNetObservedMin = sumOutputReduceObservedTokens(records) - meta.ExpectedOutputReduceOverheadMax
		meta.ScenarioValidators = []string{"output_reduce", "host_budget_ok", "low_error"}
	case "tool_heavy":
		meta.ScenarioValidators = []string{"tool_heavy", "host_budget_ok", "low_error"}
	case "chunk_dedup_similar_outputs", "chunk_dedup_log_output", "chunk_dedup_test_output", "host_resource_long_workday":
		meta.ScenarioValidators = []string{"host_budget_ok", "low_error"}
	case "ok_summary_tool_output":
		meta.ScenarioValidators = []string{"host_budget_ok", "low_error"}
	default:
		meta.ScenarioValidators = []string{"low_error"}
	}
}

func writeWSSProofCorpusCategory(root, key string, cat wssProofCorpusExportCategory) error {
	dir := filepath.Join(root, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create corpus category %s: %w", dir, err)
	}
	existing, err := readExistingWSSProofCorpusRecords(dir)
	if err != nil {
		return err
	}
	cat.records = mergeWSSProofCorpusRecords(existing, cat.records)
	cat.metadata.ExpectedRequestCount = len(cat.records)
	tuneCorpusMetadataForWorkload(&cat.metadata, cat.records)
	metaPath := filepath.Join(dir, "metadata.json")
	metaData, err := json.MarshalIndent(cat.metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode metadata %s: %w", metaPath, err)
	}
	metaData = append(metaData, '\n')
	if err := os.WriteFile(metaPath, metaData, 0o644); err != nil {
		return fmt.Errorf("write metadata %s: %w", metaPath, err)
	}
	if err := removeStaleWSSProofCorpusExports(dir); err != nil {
		return err
	}
	for i, rec := range cat.records {
		sessionPath := filepath.Join(dir, fmt.Sprintf("session_wss_proof_export_%03d.jsonl", i+1))
		f, err := os.OpenFile(sessionPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("open session export %s: %w", sessionPath, err)
		}
		enc := json.NewEncoder(f)
		if err := enc.Encode(rec); err != nil {
			_ = f.Close()
			return fmt.Errorf("write session export %s: %w", sessionPath, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close session export %s: %w", sessionPath, err)
		}
	}
	return nil
}

func readExistingWSSProofCorpusRecords(dir string) ([]wssProofCorpusSummary, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read corpus category %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "session_wss_proof_export") && strings.HasSuffix(name, ".jsonl") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []wssProofCorpusSummary
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read corpus export %s: %w", path, err)
		}
		line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
		if line == "" {
			continue
		}
		var rec wssProofCorpusSummary
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("decode corpus export %s: %w", path, err)
		}
		out = append(out, rec)
	}
	return out, nil
}

func mergeWSSProofCorpusRecords(existing, incoming []wssProofCorpusSummary) []wssProofCorpusSummary {
	out := make([]wssProofCorpusSummary, 0, len(existing)+len(incoming))
	for _, rec := range existing {
		out = append(out, rec)
	}
	for _, rec := range incoming {
		key := wssProofCorpusRecordKey(rec)
		matched := false
		for i := range out {
			if wssProofCorpusRecordKey(out[i]) != key {
				continue
			}
			matched = true
			if wssProofCorpusRecordHasBetterCounters(rec, out[i]) {
				out[i] = rec
			}
		}
		if !matched {
			out = append(out, rec)
		}
	}
	return out
}

func wssProofCorpusRecordKey(rec wssProofCorpusSummary) string {
	key := strings.TrimSpace(rec.RequestID)
	if key == "" {
		key = rec.Timestamp.Format(time.RFC3339Nano) + ":" + strconv.Itoa(rec.Tokens.Saved)
	}
	return key
}

func wssProofCorpusRecordHasBetterCounters(candidate, current wssProofCorpusSummary) bool {
	if current.Tokens.Original == 0 && candidate.Tokens.Original > 0 {
		return true
	}
	if current.ProviderInputTokens == 0 && candidate.ProviderInputTokens > 0 {
		return true
	}
	if current.ProviderOutputTokens == 0 && candidate.ProviderOutputTokens > 0 {
		return true
	}
	if current.CacheReadTokens > 0 && current.ProviderCachedTokens > 0 && candidate.CacheReadTokens == 0 && candidate.ProviderCachedTokens > 0 {
		return true
	}
	if current.HostBudgetStatus == "" && candidate.HostBudgetStatus != "" {
		return true
	}
	if candidate.Tokens.Original > 0 &&
		candidate.Tokens.Original == current.Tokens.Original &&
		candidate.Tokens.Saved > current.Tokens.Saved &&
		(current.Tokens.Final == 0 || candidate.Tokens.Final < current.Tokens.Final) {
		return true
	}
	return false
}

func removeStaleWSSProofCorpusExports(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read corpus category %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "session_wss_proof_export") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale corpus export %s: %w", path, err)
		}
	}
	return nil
}

func hasWSSProofSafetyIssue(live *codexCaptureLiveDelta) bool {
	if live == nil {
		return true
	}
	return live.ParseFailures > 0 || live.DegradedSessions > 0 || live.CompressionErrors > 0 || live.HostBudgetExceeded
}

func hasWSSProofCorpusEconomicSignal(row wssProofMatrixRecord) bool {
	if row.LiveDelta == nil {
		return false
	}
	switch workload, _ := corpusWorkloadFromWSS(row.WorkloadClass); workload {
	case "provider_cache_long_session":
		return row.LiveDelta.ProviderCacheReadTokens > 0
	case "output_reduce_aggressive":
		return row.LiveDelta.OutputReduceInjected > 0 &&
			row.LiveDelta.OutputReduceOutputTokensObserved > 0
	case "tool_heavy":
		return row.LiveDelta.ToolPruneTokensSaved > 0
	default:
		return row.LiveDelta.BillableInputTokensSaved > 0 ||
			wssProofCorpusSearchCapExtraReducerTokens(row) > 0
	}
}

func wssProofCorpusSearchCapExtraReducerTokens(row wssProofMatrixRecord) int64 {
	if strings.TrimSpace(row.WorkloadClass) != "search_loop" ||
		row.LiveDelta == nil ||
		row.SearchCapProof == nil ||
		!row.GatePassed ||
		len(row.GateFailures) > 0 ||
		!row.SearchCapProof.GatePassed ||
		len(row.SearchCapProof.GateFailures) > 0 ||
		row.SearchCapProof.SelectedCandidate == nil {
		return 0
	}
	proof := row.SearchCapProof
	selected := proof.SelectedCandidate
	if proof.MinCandidateRetainedPct+1e-9 < releaseSearchCapMinRetainedPct ||
		proof.MinSearchOutputs < releaseSearchCapMinSearchOutputs ||
		proof.SearchOutputs < releaseSearchCapMinSearchOutputs ||
		proof.MinExtraReducerTokens < releaseSearchCapMinExtraReducerTokens ||
		selected.MatchRetentionPct+1e-9 < releaseSearchCapMinRetainedPct ||
		selected.ExtraReducerTokens <= 0 {
		return 0
	}
	selectedReplay := releaseSearchCapSelectedReplay(proof)
	if selectedReplay == nil ||
		!searchCapReplayUsesProductLatch(*selectedReplay) ||
		selectedReplay.UpstreamInvalidRequests > 0 ||
		selectedReplay.UpstreamHTTP400Errors > 0 ||
		selectedReplay.UpstreamResponseFailures > 0 ||
		selectedReplay.Lost > 0 ||
		!searchCapReplayUsesProductLatch(proof.DefaultReplay) {
		return 0
	}
	extra := int64(selected.ExtraReducerTokens)
	if row.LiveDelta.ProviderInputTokens <= extra {
		return 0
	}
	return extra
}

func normalizeWSSClient(client string) string {
	switch strings.TrimSpace(strings.ToLower(client)) {
	case "cli", "codex_cli":
		return "codex_cli"
	case "desktop", "codex_desktop":
		return "codex_desktop"
	default:
		return sanitizeCorpusName(client)
	}
}

var nonCorpusNameChars = regexp.MustCompile(`[^a-z0-9_]+`)

func sanitizeCorpusName(in string) string {
	s := strings.ToLower(strings.TrimSpace(in))
	s = strings.ReplaceAll(s, "-", "_")
	s = nonCorpusNameChars.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "unknown"
	}
	return s
}

func parseWSSProofTime(raw string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw)); err == nil {
		return t
	}
	return time.Unix(0, 0).UTC()
}

func clampInt64ToInt(v int64) int {
	if v <= 0 {
		return 0
	}
	maxInt := int64(^uint(0) >> 1)
	if v > maxInt {
		return int(maxInt)
	}
	return int(v)
}

func sumCacheReadTokens(records []wssProofCorpusSummary) int64 {
	var total int64
	for _, rec := range records {
		total += int64(rec.CacheReadTokens + rec.ProviderCachedTokens)
	}
	return total
}

func sumSavedTokens(records []wssProofCorpusSummary) int64 {
	var total int64
	for _, rec := range records {
		total += int64(rec.Tokens.Saved)
	}
	return total
}

func sumOutputReduceInputOverheadTokens(records []wssProofCorpusSummary) int64 {
	var total int64
	for _, rec := range records {
		if !rec.OutputReduce.Applied {
			continue
		}
		total += int64(rec.OutputReduce.AddedTokens)
	}
	return total
}

func sumOutputReduceObservedTokens(records []wssProofCorpusSummary) int64 {
	var total int64
	for _, rec := range records {
		if !rec.OutputReduce.Applied {
			continue
		}
		total += int64(rec.OutputTokens)
	}
	return total
}

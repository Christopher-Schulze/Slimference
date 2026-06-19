package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type releaseProofReportFlags struct {
	matrixPath               string
	resourceProfileProofs    []string
	searchCapProofReportPath string
	codexStatusBeforePath    string
	codexStatusAfterPath     string
	outputFormat             string
	help                     bool
}

type releaseProofReport struct {
	ProofSchemaVersion          int                               `json:"proof_schema_version"`
	MatrixPath                  string                            `json:"matrix_path"`
	ResourceProfileProof        string                            `json:"resource_profile_proof,omitempty"`
	ResourceProfileProofs       []string                          `json:"resource_profile_proofs,omitempty"`
	ResourceProfileProofClients []string                          `json:"resource_profile_proof_clients,omitempty"`
	ResourceProfileProofOK      bool                              `json:"resource_profile_proof_ok"`
	ResourceProfileProofIssues  []string                          `json:"resource_profile_proof_issues,omitempty"`
	SearchCapProof              *releaseSearchCapProofSummary     `json:"search_cap_proof,omitempty"`
	CodexRouteHygiene           *releaseCodexRouteHygieneSummary  `json:"codex_route_hygiene,omitempty"`
	MatrixFiles                 int                               `json:"matrix_files"`
	Rows                        int                               `json:"rows"`
	Clients                     map[string]int                    `json:"clients"`
	WorkloadClasses             map[string]int                    `json:"workload_classes"`
	PositiveEconomicTokenRows   int                               `json:"positive_economic_token_rows"`
	ExpectedZeroRows            int                               `json:"expected_zero_rows"`
	HostBudgetOKRows            int                               `json:"host_budget_ok_rows"`
	HostBudgetIssueRows         int                               `json:"host_budget_issue_rows"`
	ProofEventLossRows          int                               `json:"proof_event_loss_rows"`
	SafetyIssueRows             int                               `json:"safety_issue_rows"`
	ExpectedZeroLocalViolations int                               `json:"expected_zero_local_violations"`
	HostBudgetIssueIDs          []string                          `json:"host_budget_issue_ids,omitempty"`
	ProofEventLossIDs           []string                          `json:"proof_event_loss_ids,omitempty"`
	ExpectedZeroViolationIDs    []string                          `json:"expected_zero_violation_ids,omitempty"`
	Economics                   releaseProofEconomics             `json:"economics"`
	LiveReducerHits             map[string]int64                  `json:"live_reducer_hits"`
	MissingReleaseWorkloads     []string                          `json:"missing_release_workloads,omitempty"`
	MissingMaxxWorkloads        []string                          `json:"missing_maxx_workloads,omitempty"`
	MaxxWorkloadStatus          []wssProofInventoryWorkloadStatus `json:"maxx_workload_status,omitempty"`
	GatePassed                  bool                              `json:"gate_passed"`
	GateFailures                []string                          `json:"gate_failures,omitempty"`
}

type releaseProofEconomics struct {
	LocalBillableInputTokensSaved int64 `json:"local_billable_input_tokens_saved"`
	LocalInputTokensSaved         int64 `json:"local_input_tokens_saved"`
	RequestSideBytesReduced       int64 `json:"request_side_bytes_reduced"`
	OutputWireBytesSaved          int64 `json:"output_wire_bytes_saved"`
	ProviderCacheReadTokens       int64 `json:"provider_cache_read_tokens"`
	ProviderCacheCreateTokens     int64 `json:"provider_cache_create_tokens"`
	ToolPruneTokensSaved          int64 `json:"tool_prune_tokens_saved"`
	OutputReduceInjectedTurns     int64 `json:"output_reduce_injected_turns"`
	OutputReduceInputOverhead     int64 `json:"output_reduce_input_overhead_tokens"`
	OutputReduceObservedTokens    int64 `json:"output_reduce_observed_tokens"`
	OutputReduceNetObservedTokens int64 `json:"output_reduce_net_observed_tokens"`
}

type releaseSearchCapProofSummary struct {
	Path                     string           `json:"path"`
	OK                       bool             `json:"ok"`
	Issues                   []string         `json:"issues,omitempty"`
	Captures                 int              `json:"captures"`
	CLI                      int              `json:"cli"`
	Desktop                  int              `json:"desktop"`
	PositiveSavings          int              `json:"positive_savings_captures"`
	DeltaToolOutputProof     bool             `json:"delta_tool_output_mutation_proof"`
	DownstreamStateProof     bool             `json:"downstream_state_proof"`
	DownstreamCandidates     int              `json:"downstream_state_candidates,omitempty"`
	DownstreamPassing        int              `json:"downstream_state_passing_candidates,omitempty"`
	DownstreamNetSavedTokens int              `json:"downstream_state_net_saved_tokens,omitempty"`
	RequiredReducerHits      map[string]int64 `json:"required_reducer_hits,omitempty"`
	SelectedCandidate        string           `json:"selected_candidate,omitempty"`
	MaxFilesShown            int              `json:"max_files_shown,omitempty"`
	MaxMatchesPerFile        int              `json:"max_matches_per_file,omitempty"`
	TotalExtraReducerTokens  int              `json:"total_extra_reducer_tokens,omitempty"`
	MinMatchRetentionPct     float64          `json:"min_match_retention_pct,omitempty"`
}

type releaseCodexRouteHygieneSummary struct {
	OK     bool     `json:"ok"`
	Before string   `json:"before"`
	After  string   `json:"after"`
	Issues []string `json:"issues,omitempty"`
}

type releaseCodexStatusSnapshot struct {
	Route struct {
		Enabled    bool   `json:"enabled"`
		Complete   bool   `json:"complete"`
		Conflict   string `json:"conflict,omitempty"`
		LegacyKeys bool   `json:"legacy_keys"`
		BaseURL    string `json:"base_url"`
		Transport  string `json:"transport"`
	} `json:"route"`
}

const (
	releaseProofReportSchemaVersion       = 2
	releaseSearchCapMinRetainedPct        = searchCapReleaseMinRetainedPct
	releaseSearchCapMinSearchOutputs      = searchCapReleaseMinSearchOutputs
	releaseSearchCapMinExtraReducerTokens = searchCapReleaseMinExtraReducerTokens
)

const releaseProofReportHelpText = `release-proof-report: produce a content-free release proof summary

Usage:
  go run ./scripts/utils release-proof-report <clean-release-matrix.jsonl> [--json] --resource-profile-proof DIR --resource-profile-proof DIR [--search-cap-proof-report focused-search-cap.json --codex-status-before before.json --codex-status-after after.json]

The report reads proof-matrix rows only, never raw WSS frame payloads. It keeps
local billable-input savings, output-wire savings, provider-cache economics,
tool-prune schema-token savings, host-resource status, and safety counters
separate. Run it on a clean release matrix file or focused release bundle, not
on the whole historical capture archive; historical diagnostic rows remain
visible and fail the strict release gate by id. It fails closed unless resource
profile proof is present for both CLI and Desktop as bundle directories
containing admin-before.json, admin-after.json, ps-before.txt, ps-after.txt,
workday-finish.json, slimference.sample.txt, and matrix.jsonl. The JSON files
must prove an OK host budget with parse/degrade/compression deltas at zero, and
the local matrix.jsonl must contain a positive host_resource_long_workday row
with host_budget_ok for the matching client, because global proof-matrix rows
alone do not replace CLI/Desktop pprof or resource-profile evidence.

For T359 search-cap promotion, pass --search-cap-proof-report pointing at a
content-free focused wss-proof-matrix --json report generated with
--search-cap-candidate thresholds. release-proof-report validates that report
without reading raw frames: gate passed, search_loop only, at least one CLI and
one Desktop row, positive rows, one consistent selected cap across rows, at
least 40% retained matches, at least two resolved search outputs, and positive
extra reducer-token savings. CLI/Desktop/positive-row coverage is recomputed
from validated row-gate-passed capture_reports, not trusted from aggregate
counters alone. Search-cap promotion also requires --codex-status-before and
--codex-status-after snapshots from 'slimference codex status --json'; both must
prove normal direct Codex routing with no marker-owned shared route, no legacy
base-url keys, and no route conflict. Save the --json output as the final
versioned release-proof artifact; that final JSON file is the supported
codex_search_cap_proof_path runtime latch input.`

func runReleaseProofReport(args []string, stdout, stderr io.Writer) int {
	flags, err := parseReleaseProofReportFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, releaseProofReportHelpText)
		return 0
	}
	if flags.matrixPath == "" {
		fmt.Fprintln(stderr, "Usage: release-proof-report <clean-release-matrix.jsonl> [--json] --resource-profile-proof DIR --resource-profile-proof DIR")
		return 2
	}
	report, err := loadReleaseProofReport(flags)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if flags.outputFormat == outputJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		if !report.GatePassed {
			return 3
		}
		return 0
	}
	writeReleaseProofReportText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseReleaseProofReportFlags(args []string) (releaseProofReportFlags, error) {
	flags := releaseProofReportFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--resource-profile-proof":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return flags, fmt.Errorf("--resource-profile-proof requires a path")
			}
			flags.resourceProfileProofs = append(flags.resourceProfileProofs, strings.TrimSpace(args[i]))
		case strings.HasPrefix(arg, "--resource-profile-proof="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--resource-profile-proof="))
			if value == "" {
				return flags, fmt.Errorf("--resource-profile-proof requires a path")
			}
			flags.resourceProfileProofs = append(flags.resourceProfileProofs, value)
		case arg == "--search-cap-proof-report":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return flags, fmt.Errorf("--search-cap-proof-report requires a path")
			}
			flags.searchCapProofReportPath = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--search-cap-proof-report="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--search-cap-proof-report="))
			if value == "" {
				return flags, fmt.Errorf("--search-cap-proof-report requires a path")
			}
			flags.searchCapProofReportPath = value
		case arg == "--codex-status-before":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return flags, fmt.Errorf("--codex-status-before requires a path")
			}
			flags.codexStatusBeforePath = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--codex-status-before="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--codex-status-before="))
			if value == "" {
				return flags, fmt.Errorf("--codex-status-before requires a path")
			}
			flags.codexStatusBeforePath = value
		case arg == "--codex-status-after":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return flags, fmt.Errorf("--codex-status-after requires a path")
			}
			flags.codexStatusAfterPath = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--codex-status-after="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--codex-status-after="))
			if value == "" {
				return flags, fmt.Errorf("--codex-status-after requires a path")
			}
			flags.codexStatusAfterPath = value
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.matrixPath != "" {
				return flags, fmt.Errorf("multiple matrix paths provided")
			}
			flags.matrixPath = arg
		}
	}
	return flags, nil
}

func loadReleaseProofReport(flags releaseProofReportFlags) (releaseProofReport, error) {
	inventory, err := loadWSSProofInventory(flags.matrixPath)
	if err != nil {
		return releaseProofReport{}, err
	}
	resourceProof := validateReleaseResourceProofs(flags.resourceProfileProofs)
	searchCapProof, err := validateReleaseSearchCapProofReport(flags.searchCapProofReportPath)
	if err != nil {
		return releaseProofReport{}, err
	}
	codexHygiene, err := validateReleaseCodexRouteHygiene(flags.codexStatusBeforePath, flags.codexStatusAfterPath)
	if err != nil {
		return releaseProofReport{}, err
	}
	report := releaseProofReport{
		ProofSchemaVersion:          releaseProofReportSchemaVersion,
		MatrixPath:                  flags.matrixPath,
		ResourceProfileProof:        strings.Join(flags.resourceProfileProofs, ","),
		ResourceProfileProofs:       append([]string(nil), flags.resourceProfileProofs...),
		ResourceProfileProofClients: resourceProof.Clients,
		ResourceProfileProofOK:      resourceProof.OK,
		ResourceProfileProofIssues:  resourceProof.Issues,
		SearchCapProof:              searchCapProof,
		CodexRouteHygiene:           codexHygiene,
		MatrixFiles:                 inventory.MatrixFiles,
		Rows:                        inventory.Rows,
		Clients:                     cloneInventoryIntMap(inventory.Clients),
		WorkloadClasses:             cloneInventoryIntMap(inventory.WorkloadClasses),
		PositiveEconomicTokenRows:   inventory.PositiveTokenRows,
		ExpectedZeroRows:            inventory.ExpectedZeroRows,
		HostBudgetOKRows:            inventory.HostBudgetOKRows,
		SafetyIssueRows:             inventory.SafetyIssueRows,
		LiveReducerHits:             cloneInventoryInt64Map(inventory.LiveReducerHits),
		MissingReleaseWorkloads:     append([]string(nil), inventory.MissingReleaseWorkloads...),
		MissingMaxxWorkloads:        append([]string(nil), inventory.MissingMaxxWorkloads...),
		MaxxWorkloadStatus:          append([]wssProofInventoryWorkloadStatus(nil), inventory.MaxxWorkloadStatus...),
	}
	files, err := wssProofInventoryFiles(flags.matrixPath)
	if err != nil {
		return releaseProofReport{}, err
	}
	for _, file := range files {
		rows, err := readWSSProofInventoryRows(file)
		if err != nil {
			return releaseProofReport{}, err
		}
		for _, row := range rows {
			addReleaseProofRow(&report, row)
		}
	}
	report.GateFailures = releaseProofGateFailures(report)
	report.GatePassed = len(report.GateFailures) == 0
	return report, nil
}

func addReleaseProofRow(report *releaseProofReport, row wssProofMatrixRecord) {
	live := row.LiveDelta
	if live == nil {
		return
	}
	report.Economics.LocalBillableInputTokensSaved += live.BillableInputTokensSaved
	report.Economics.LocalInputTokensSaved += live.InputTokensSaved
	report.Economics.RequestSideBytesReduced += live.RequestSideBytesReduced
	report.Economics.OutputWireBytesSaved += live.OutputWireBytesSaved
	report.Economics.ProviderCacheReadTokens += live.ProviderCacheReadTokens
	report.Economics.ProviderCacheCreateTokens += live.ProviderCacheCreateTokens
	report.Economics.ToolPruneTokensSaved += live.ToolPruneTokensSaved
	report.Economics.OutputReduceInjectedTurns += live.OutputReduceInjected
	report.Economics.OutputReduceInputOverhead += live.OutputReduceInputOverheadTokens
	report.Economics.OutputReduceObservedTokens += live.OutputReduceOutputTokensObserved
	report.Economics.OutputReduceNetObservedTokens += live.OutputReduceOutputTokensObserved - live.OutputReduceInputOverheadTokens
	if releaseProofHostBudgetIssue(live) {
		report.HostBudgetIssueRows++
		report.HostBudgetIssueIDs = append(report.HostBudgetIssueIDs, releaseProofRowID(row))
	}
	if live.AnalyticsProofEventsDropped > 0 {
		report.ProofEventLossRows++
		report.ProofEventLossIDs = append(report.ProofEventLossIDs, releaseProofRowID(row))
	}
	if row.ExpectedZeroSavings && wssProofLiveLocalSavingsSignal(live) {
		report.ExpectedZeroLocalViolations++
		report.ExpectedZeroViolationIDs = append(report.ExpectedZeroViolationIDs, releaseProofRowID(row))
	}
}

func releaseProofGateFailures(report releaseProofReport) []string {
	var failures []string
	if report.Rows == 0 {
		failures = append(failures, "no proof-matrix rows")
	}
	if report.PositiveEconomicTokenRows == 0 {
		failures = append(failures, "no positive economic-token proof rows")
	}
	if len(report.MissingReleaseWorkloads) > 0 {
		failures = append(failures, "missing release workloads: "+strings.Join(report.MissingReleaseWorkloads, ", "))
	}
	if len(report.MissingMaxxWorkloads) > 0 {
		failures = append(failures, "missing maxx workloads: "+strings.Join(report.MissingMaxxWorkloads, ", "))
	}
	for _, status := range report.MaxxWorkloadStatus {
		if !status.Complete {
			failures = append(failures, fmt.Sprintf("maxx workload %s incomplete: missing=%s positive_rows=%d host_budget_ok=%d safety_issues=%d",
				status.WorkloadClass,
				formatInventoryStringSlice(status.MissingSignals),
				status.PositiveTokenRows,
				status.HostBudgetOKRows,
				status.SafetyIssueRows))
		}
	}
	if report.SafetyIssueRows > 0 {
		failures = append(failures, fmt.Sprintf("safety issue rows=%d", report.SafetyIssueRows))
	}
	if report.HostBudgetIssueRows > 0 {
		failures = append(failures, fmt.Sprintf("host budget issue rows=%d ids=%s",
			report.HostBudgetIssueRows,
			formatInventoryStringSlice(report.HostBudgetIssueIDs)))
	}
	if report.ProofEventLossRows > 0 {
		failures = append(failures, fmt.Sprintf("proof event loss rows=%d ids=%s",
			report.ProofEventLossRows,
			formatInventoryStringSlice(report.ProofEventLossIDs)))
	}
	if report.ExpectedZeroLocalViolations > 0 {
		failures = append(failures, fmt.Sprintf("expected-zero rows had local savings=%d ids=%s",
			report.ExpectedZeroLocalViolations,
			formatInventoryStringSlice(report.ExpectedZeroViolationIDs)))
	}
	if !report.ResourceProfileProofOK {
		failures = append(failures, "invalid CLI/Desktop pprof/resource profile proof: "+strings.Join(report.ResourceProfileProofIssues, "; "))
	}
	if report.SearchCapProof != nil && !report.SearchCapProof.OK {
		failures = append(failures, "invalid focused search-cap proof: "+strings.Join(report.SearchCapProof.Issues, "; "))
	}
	if report.SearchCapProof != nil {
		if report.CodexRouteHygiene == nil {
			failures = append(failures, "missing Codex route hygiene proof for search-cap promotion")
		} else if !report.CodexRouteHygiene.OK {
			failures = append(failures, "invalid Codex route hygiene proof: "+strings.Join(report.CodexRouteHygiene.Issues, "; "))
		}
	}
	return failures
}

func validateReleaseCodexRouteHygiene(beforePath, afterPath string) (*releaseCodexRouteHygieneSummary, error) {
	beforePath = strings.TrimSpace(beforePath)
	afterPath = strings.TrimSpace(afterPath)
	if beforePath == "" && afterPath == "" {
		return nil, nil
	}
	summary := &releaseCodexRouteHygieneSummary{Before: beforePath, After: afterPath}
	if beforePath == "" {
		summary.Issues = append(summary.Issues, "missing --codex-status-before")
	}
	if afterPath == "" {
		summary.Issues = append(summary.Issues, "missing --codex-status-after")
	}
	if beforePath != "" {
		before, err := readReleaseCodexStatusSnapshot(beforePath)
		if err != nil {
			return nil, fmt.Errorf("read Codex route hygiene before snapshot: %w", err)
		}
		summary.Issues = append(summary.Issues, releaseCodexStatusSnapshotIssues("before", before)...)
	}
	if afterPath != "" {
		after, err := readReleaseCodexStatusSnapshot(afterPath)
		if err != nil {
			return nil, fmt.Errorf("read Codex route hygiene after snapshot: %w", err)
		}
		summary.Issues = append(summary.Issues, releaseCodexStatusSnapshotIssues("after", after)...)
	}
	summary.OK = len(summary.Issues) == 0
	return summary, nil
}

func readReleaseCodexStatusSnapshot(path string) (releaseCodexStatusSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseCodexStatusSnapshot{}, err
	}
	var snapshot releaseCodexStatusSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return releaseCodexStatusSnapshot{}, err
	}
	return snapshot, nil
}

func releaseCodexStatusSnapshotIssues(label string, snapshot releaseCodexStatusSnapshot) []string {
	var issues []string
	if snapshot.Route.Enabled {
		issues = append(issues, label+": advanced shared Codex route enabled")
	}
	if snapshot.Route.Complete {
		issues = append(issues, label+": marker-owned Codex route complete")
	}
	if snapshot.Route.LegacyKeys {
		issues = append(issues, label+": legacy openai_base_url/chatgpt_base_url keys present")
	}
	if strings.TrimSpace(snapshot.Route.Conflict) != "" {
		issues = append(issues, label+": Codex route conflict: "+strings.TrimSpace(snapshot.Route.Conflict))
	}
	return issues
}

func validateReleaseSearchCapProofReport(path string) (*releaseSearchCapProofSummary, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read search-cap proof report: %w", err)
	}
	var proof wssProofMatrixReport
	if err := json.Unmarshal(data, &proof); err != nil {
		return nil, fmt.Errorf("parse search-cap proof report: %w", err)
	}
	summary := &releaseSearchCapProofSummary{
		Path:                path,
		Captures:            proof.Captures,
		CLI:                 proof.CLI,
		Desktop:             proof.Desktop,
		PositiveSavings:     proof.PositiveSavings,
		RequiredReducerHits: cloneInventoryInt64Map(proof.RequiredReducerHits),
	}
	var issues []string
	if !proof.GatePassed {
		issues = append(issues, "focused wss-proof-matrix gate did not pass: "+strings.Join(proof.GateFailures, "; "))
	}
	if proof.GatePassed && len(proof.GateFailures) > 0 {
		issues = append(issues, "focused wss-proof-matrix gate passed but still contains gate failures: "+strings.Join(proof.GateFailures, "; "))
	}
	if proof.Captures < 2 {
		issues = append(issues, fmt.Sprintf("expected at least 2 search_loop captures, got %d", proof.Captures))
	}
	if proof.CLI < 1 {
		issues = append(issues, "missing CLI search_loop capture")
	}
	if proof.Desktop < 1 {
		issues = append(issues, "missing Desktop search_loop capture")
	}
	if proof.PositiveSavings < 2 {
		issues = append(issues, fmt.Sprintf("expected at least 2 positive search-cap proof rows, got %d", proof.PositiveSavings))
	}
	if proof.WorkloadClasses["search_loop"] != proof.Captures {
		issues = append(issues, "focused search-cap proof must contain only search_loop captures")
	}
	if proof.RequiredReducerHits["captured_output"] <= 0 {
		issues = append(issues, "focused search-cap proof missing required captured_output reducer hit")
	}
	validatedSearchLoopReports := 0
	validatedCLIReports := 0
	validatedDesktopReports := 0
	validatedPositiveReports := 0
	productLatchProofReports := 0
	downstreamStateProofReports := 0
	for _, capture := range proof.CaptureReports {
		if strings.TrimSpace(capture.WorkloadClass) != "search_loop" {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": focused proof contains non-search_loop capture")
			continue
		}
		validatedSearchLoopReports++
		candidateValid := true
		if !capture.GatePassed {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": focused search-cap capture row gate failed: "+strings.Join(capture.GateFailures, "; "))
			candidateValid = false
		}
		if capture.GatePassed && len(capture.GateFailures) > 0 {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": focused search-cap capture row gate passed but still contains gate failures: "+strings.Join(capture.GateFailures, "; "))
			candidateValid = false
		}
		if capture.SearchCapProof == nil {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": missing search_cap_proof")
			continue
		}
		if !capture.SearchCapProof.GatePassed {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": search_cap_proof gate failed")
			candidateValid = false
		}
		if capture.SearchCapProof.GatePassed && len(capture.SearchCapProof.GateFailures) > 0 {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": search_cap_proof gate passed but still contains gate failures: "+strings.Join(capture.SearchCapProof.GateFailures, "; "))
			candidateValid = false
		}
		if capture.SearchCapProof.MinCandidateRetainedPct+1e-9 < releaseSearchCapMinRetainedPct {
			issues = append(issues, fmt.Sprintf("%s: search_cap_proof min retention %.2f%% < release min %.2f%%",
				releaseProofSearchCapCaptureID(capture),
				capture.SearchCapProof.MinCandidateRetainedPct,
				releaseSearchCapMinRetainedPct))
			candidateValid = false
		}
		if capture.SearchCapProof.MinSearchOutputs < releaseSearchCapMinSearchOutputs {
			issues = append(issues, fmt.Sprintf("%s: search_cap_proof min search outputs %d < release min %d",
				releaseProofSearchCapCaptureID(capture),
				capture.SearchCapProof.MinSearchOutputs,
				releaseSearchCapMinSearchOutputs))
			candidateValid = false
		}
		if capture.SearchCapProof.SearchOutputs < releaseSearchCapMinSearchOutputs {
			issues = append(issues, fmt.Sprintf("%s: search_cap_proof resolved search outputs %d < release min %d",
				releaseProofSearchCapCaptureID(capture),
				capture.SearchCapProof.SearchOutputs,
				releaseSearchCapMinSearchOutputs))
			candidateValid = false
		}
		if capture.SearchCapProof.MinExtraReducerTokens < releaseSearchCapMinExtraReducerTokens {
			issues = append(issues, fmt.Sprintf("%s: search_cap_proof min extra reducer tokens %d < release min %d",
				releaseProofSearchCapCaptureID(capture),
				capture.SearchCapProof.MinExtraReducerTokens,
				releaseSearchCapMinExtraReducerTokens))
			candidateValid = false
		}
		if !searchCapReplayUsesProductLatch(capture.SearchCapProof.DefaultReplay) {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": search_cap_proof default replay did not prove product search-cap latch mutation")
			candidateValid = false
		}
		downstreamProof := capture.SearchCapProof.DownstreamStateProof
		if !downstreamProof.GatePassed {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": search_cap_proof downstream_state_proof failed: "+strings.Join(downstreamProof.GateFailures, "; "))
			candidateValid = false
		}
		if downstreamProof.GatePassed && len(downstreamProof.GateFailures) > 0 {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": search_cap_proof downstream_state_proof passed but still contains gate failures: "+strings.Join(downstreamProof.GateFailures, "; "))
			candidateValid = false
		}
		if downstreamProof.MutatedSearchOutputCandidates <= 0 {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": search_cap_proof downstream_state_proof has no live mutated search-output candidates")
			candidateValid = false
		}
		if downstreamProof.CandidatesPassing <= 0 {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": search_cap_proof downstream_state_proof has no passing downstream candidates")
			candidateValid = false
		}
		selected := capture.SearchCapProof.SelectedCandidate
		if selected == nil {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": missing selected search-cap candidate")
			continue
		}
		if selected.MatchRetentionPct+1e-9 < releaseSearchCapMinRetainedPct {
			issues = append(issues, fmt.Sprintf("%s: selected search-cap candidate retention %.2f%% < release min %.2f%%",
				releaseProofSearchCapCaptureID(capture),
				selected.MatchRetentionPct,
				releaseSearchCapMinRetainedPct))
			candidateValid = false
		}
		if selected.ExtraReducerTokens <= 0 {
			issues = append(issues, fmt.Sprintf("%s: selected search-cap candidate has non-positive extra reducer tokens %+d",
				releaseProofSearchCapCaptureID(capture),
				selected.ExtraReducerTokens))
			candidateValid = false
		}
		if selected.MaxFilesShown <= 0 || selected.MaxMatchesPerFile <= 0 {
			issues = append(issues, fmt.Sprintf("%s: selected search-cap candidate has invalid cap %d/%d",
				releaseProofSearchCapCaptureID(capture),
				selected.MaxFilesShown,
				selected.MaxMatchesPerFile))
			candidateValid = false
		}
		if selectedReplay := releaseSearchCapSelectedReplay(capture.SearchCapProof); selectedReplay == nil ||
			!searchCapReplayUsesProductLatch(*selectedReplay) {
			issues = append(issues, releaseProofSearchCapCaptureID(capture)+": selected candidate replay did not prove product search-cap latch mutation")
			candidateValid = false
		}
		if summary.SelectedCandidate == "" {
			summary.SelectedCandidate = selected.Name
			summary.MaxFilesShown = selected.MaxFilesShown
			summary.MaxMatchesPerFile = selected.MaxMatchesPerFile
			summary.MinMatchRetentionPct = selected.MatchRetentionPct
		} else if summary.MaxFilesShown != selected.MaxFilesShown || summary.MaxMatchesPerFile != selected.MaxMatchesPerFile {
			issues = append(issues, fmt.Sprintf("%s: selected cap %d/%d differs from %d/%d",
				releaseProofSearchCapCaptureID(capture),
				selected.MaxFilesShown,
				selected.MaxMatchesPerFile,
				summary.MaxFilesShown,
				summary.MaxMatchesPerFile))
		}
		if selected.MatchRetentionPct < summary.MinMatchRetentionPct {
			summary.MinMatchRetentionPct = selected.MatchRetentionPct
		}
		summary.TotalExtraReducerTokens += selected.ExtraReducerTokens
		if candidateValid {
			switch strings.TrimSpace(capture.Client) {
			case "cli":
				validatedCLIReports++
			case "desktop":
				validatedDesktopReports++
			}
			validatedPositiveReports++
			productLatchProofReports++
			downstreamStateProofReports++
			summary.DownstreamCandidates += downstreamProof.MutatedSearchOutputCandidates
			summary.DownstreamPassing += downstreamProof.CandidatesPassing
			summary.DownstreamNetSavedTokens += downstreamProof.NetCapturedLocalSavedTokens
		}
	}
	if validatedSearchLoopReports != proof.Captures {
		issues = append(issues, fmt.Sprintf("validated search_loop capture_reports %d != captures %d", validatedSearchLoopReports, proof.Captures))
	}
	if validatedCLIReports < 1 {
		issues = append(issues, "missing validated CLI search-cap capture report")
	}
	if validatedDesktopReports < 1 {
		issues = append(issues, "missing validated Desktop search-cap capture report")
	}
	if validatedPositiveReports < 2 {
		issues = append(issues, fmt.Sprintf("expected at least 2 validated positive search-cap capture reports, got %d", validatedPositiveReports))
	}
	if productLatchProofReports != validatedPositiveReports || productLatchProofReports < 2 {
		issues = append(issues, fmt.Sprintf("expected product search-cap latch proof for every positive search-cap capture, got %d/%d", productLatchProofReports, validatedPositiveReports))
	} else {
		summary.DeltaToolOutputProof = true
	}
	if downstreamStateProofReports != validatedPositiveReports || downstreamStateProofReports < 2 {
		issues = append(issues, fmt.Sprintf("expected live downstream-state proof for every positive search-cap capture, got %d/%d", downstreamStateProofReports, validatedPositiveReports))
	} else {
		summary.DownstreamStateProof = true
	}
	if summary.SelectedCandidate == "" {
		issues = append(issues, "no selected search-cap candidate")
	}
	if summary.TotalExtraReducerTokens <= 0 {
		issues = append(issues, fmt.Sprintf("total search-cap extra reducer tokens must be positive, got %+d", summary.TotalExtraReducerTokens))
	}
	summary.Issues = issues
	summary.OK = len(issues) == 0
	return summary, nil
}

func releaseSearchCapSelectedReplay(proof *searchCapProofReport) *searchCapProofReplaySummary {
	if proof == nil || proof.SelectedCandidate == nil {
		return nil
	}
	if strings.TrimSpace(proof.SelectedCandidate.Name) == "default_retention_floor" {
		return &proof.DefaultReplay
	}
	for i := range proof.Candidates {
		candidate := &proof.Candidates[i]
		if strings.TrimSpace(candidate.Name) == strings.TrimSpace(proof.SelectedCandidate.Name) &&
			candidate.MaxFilesShown == proof.SelectedCandidate.MaxFilesShown &&
			candidate.MaxMatchesPerFile == proof.SelectedCandidate.MaxMatchesPerFile {
			return candidate.Replay
		}
	}
	return nil
}

func searchCapReplayUsesProductLatch(replay searchCapProofReplaySummary) bool {
	return replay.SearchCapProofLatch &&
		!replay.ToolOutputMutation &&
		!replay.DeltaToolOutputMutation &&
		replay.SearchRequestTurns > 0 &&
		replay.SearchMutatedRequests+replay.SearchCapturedMutated > 0
}

func releaseProofSearchCapCaptureID(capture wssProofMatrixCapture) string {
	if strings.TrimSpace(capture.ID) != "" {
		return strings.TrimSpace(capture.ID)
	}
	if strings.TrimSpace(capture.Client) != "" {
		return strings.TrimSpace(capture.Client)
	}
	return "<unknown-search-cap-capture>"
}

func releaseProofRowID(row wssProofMatrixRecord) string {
	id := strings.TrimSpace(row.ID)
	if id != "" {
		return id
	}
	parts := []string{
		strings.TrimSpace(row.Client),
		strings.TrimSpace(row.WorkloadClass),
		filepath.Base(strings.TrimSpace(row.FramesPath)),
	}
	var nonEmpty []string
	for _, part := range parts {
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	if len(nonEmpty) == 0 {
		return "<unknown-row>"
	}
	return strings.Join(nonEmpty, "/")
}

func releaseProofHostBudgetIssue(live *codexCaptureLiveDelta) bool {
	if live == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(live.HostBudgetStatus))
	if status != "" && status != "ok" {
		return true
	}
	if live.HostBudgetExceeded {
		return true
	}
	if status != "" && (!live.HostBudgetCompressionOK || !live.HostBudgetDegradationOK) {
		return true
	}
	return false
}

type releaseResourceProofValidation struct {
	OK      bool
	Issues  []string
	Clients []string
}

func validateReleaseResourceProofs(paths []string) releaseResourceProofValidation {
	if len(paths) == 0 {
		return releaseResourceProofValidation{
			Issues: []string{"missing --resource-profile-proof bundle directories for cli and desktop"},
		}
	}
	clientSet := make(map[string]bool)
	var issues []string
	for _, path := range paths {
		result := validateReleaseResourceProof(path)
		issues = append(issues, result.Issues...)
		for _, client := range result.Clients {
			clientSet[client] = true
		}
	}
	for _, client := range []string{"cli", "desktop"} {
		if !clientSet[client] {
			issues = append(issues, "missing valid resource proof bundle for "+client)
		}
	}
	return releaseResourceProofValidation{
		OK:      len(issues) == 0,
		Issues:  issues,
		Clients: sortedReleaseProofClients(clientSet),
	}
}

func validateReleaseResourceProof(path string) releaseResourceProofValidation {
	if strings.TrimSpace(path) == "" {
		return releaseResourceProofValidation{
			Issues: []string{"empty --resource-profile-proof path"},
		}
	}
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		return releaseResourceProofValidation{
			Issues: []string{"resource proof bundle is not readable: " + err.Error()},
		}
	}
	if !info.IsDir() {
		return releaseResourceProofValidation{
			Issues: []string{"resource proof path must be a bundle directory, not a file"},
		}
	}

	requiredFiles := []string{
		"admin-before.json",
		"admin-after.json",
		"ps-before.txt",
		"ps-after.txt",
		"workday-finish.json",
		"slimference.sample.txt",
		"matrix.jsonl",
	}
	var issues []string
	for _, name := range requiredFiles {
		if !nonEmptyRegularFile(filepath.Join(path, name)) {
			issues = append(issues, "missing or empty "+name)
		}
	}
	if len(issues) == 0 {
		issues = append(issues, validateReleaseAggregateHostBudget(filepath.Join(path, "admin-before.json"), "admin-before")...)
		issues = append(issues, validateReleaseAggregateHostBudget(filepath.Join(path, "admin-after.json"), "admin-after")...)
		issues = append(issues, validateReleaseWorkdayFinish(filepath.Join(path, "workday-finish.json"))...)
	}
	clients, matrixIssues := validateReleaseResourceMatrix(filepath.Join(path, "matrix.jsonl"))
	issues = append(issues, matrixIssues...)
	return releaseResourceProofValidation{
		OK:      len(issues) == 0,
		Issues:  issues,
		Clients: sortedReleaseProofClients(clients),
	}
}

func nonEmptyRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func validateReleaseAggregateHostBudget(path, label string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{label + ".json is not readable: " + err.Error()}
	}
	var report aggregateSavingsReport
	if err := json.Unmarshal(data, &report); err != nil {
		return []string{label + ".json is not valid aggregate-savings JSON: " + err.Error()}
	}
	issues := releaseHostBudgetIssues(label, report.HostBudget)
	if report.WSS.AnalyticsProofEventsDropped > 0 {
		issues = append(issues, fmt.Sprintf("%s analytics proof events dropped=%d", label, report.WSS.AnalyticsProofEventsDropped))
	}
	return issues
}

func validateReleaseWorkdayFinish(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"workday-finish.json is not readable: " + err.Error()}
	}
	var result workdaySavingsResult
	if err := json.Unmarshal(data, &result); err != nil {
		return []string{"workday-finish.json is not valid workday-savings JSON: " + err.Error()}
	}
	issues := releaseHostBudgetIssues("workday-current", result.Current.HostBudget)
	issues = append(issues, releaseHostBudgetIssues("workday-delta", result.Delta.HostBudget)...)
	if result.Delta.WSS.ParseFailures != 0 ||
		result.Delta.WSS.DegradedSessions != 0 ||
		result.Delta.WSS.CompressionErrors != 0 ||
		result.Delta.WSS.AnalyticsProofEventsDropped != 0 {
		issues = append(issues, "workday delta has non-zero WSS parse/degrade/compression/proof-drop errors")
	}
	return issues
}

func validateReleaseResourceMatrix(path string) (map[string]bool, []string) {
	rows, err := readWSSProofInventoryRows(path)
	if err != nil {
		return nil, []string{"matrix.jsonl is not a valid WSS proof matrix: " + err.Error()}
	}
	clients := make(map[string]bool)
	var sawHostResource bool
	for _, row := range rows {
		if strings.TrimSpace(row.WorkloadClass) != "host_resource_long_workday" {
			continue
		}
		sawHostResource = true
		client := normalizeReleaseResourceClient(row.Client)
		if client == "" {
			continue
		}
		if releaseResourceMatrixRowOK(row) {
			clients[client] = true
		}
	}
	if len(clients) == 0 {
		if sawHostResource {
			return clients, []string{"matrix.jsonl has no positive host_resource_long_workday row with host_budget_ok"}
		}
		return clients, []string{"matrix.jsonl has no host_resource_long_workday row"}
	}
	return clients, nil
}

func releaseResourceMatrixRowOK(row wssProofMatrixRecord) bool {
	live := row.LiveDelta
	if live == nil {
		return false
	}
	if wssProofLiveEconomicTokens(row.WorkloadClass, live) <= 0 {
		return false
	}
	if _, failures := validateExpectedReducers(row.ExpectedReducers, live); len(failures) > 0 {
		return false
	}
	if live.HostBudgetStatus != "ok" || live.HostBudgetExceeded || !live.HostBudgetCompressionOK || !live.HostBudgetDegradationOK {
		return false
	}
	return live.ParseFailures == 0 && live.DegradedSessions == 0 && live.CompressionErrors == 0 && live.AnalyticsProofEventsDropped == 0
}

func normalizeReleaseResourceClient(client string) string {
	switch strings.TrimSpace(client) {
	case "cli", "codex_cli":
		return "cli"
	case "desktop", "codex_desktop":
		return "desktop"
	default:
		return ""
	}
}

func sortedReleaseProofClients(clients map[string]bool) []string {
	var out []string
	for _, client := range []string{"cli", "desktop"} {
		if clients[client] {
			out = append(out, client)
		}
	}
	return out
}

func releaseHostBudgetIssues(prefix string, host aggregateHostBudgetBlock) []string {
	var issues []string
	status := strings.ToLower(strings.TrimSpace(host.Status))
	if status != "ok" {
		issues = append(issues, prefix+" host_budget status is not ok")
	}
	if host.Exceeded {
		issues = append(issues, prefix+" host_budget exceeded")
	}
	if !host.CompressionOK {
		issues = append(issues, prefix+" host_budget compression_ok is false")
	}
	if !host.DegradationOK {
		issues = append(issues, prefix+" host_budget degradation_ok is false")
	}
	if host.RSSBytes <= 0 {
		issues = append(issues, prefix+" host_budget rss_bytes is missing")
	}
	if host.CPUWindowSeconds <= 0 {
		issues = append(issues, prefix+" host_budget cpu_window_seconds is missing")
	}
	return issues
}

func writeReleaseProofReportText(w io.Writer, report releaseProofReport) {
	fmt.Fprintln(w, "Slimference release proof report")
	fmt.Fprintf(w, "Matrix: %s\n", report.MatrixPath)
	fmt.Fprintf(w, "Rows: %d matrix_files=%d positive_economic_rows=%d expected_zero=%d\n",
		report.Rows, report.MatrixFiles, report.PositiveEconomicTokenRows, report.ExpectedZeroRows)
	fmt.Fprintf(w, "Clients: %s\n", formatInventoryIntMap(report.Clients))
	fmt.Fprintf(w, "Workloads: %s\n", formatInventoryIntMap(report.WorkloadClasses))
	fmt.Fprintln(w, "Economics (kept separate, never summed into one headline):")
	fmt.Fprintf(w, "  local_billable_input_tokens_saved: %d\n", report.Economics.LocalBillableInputTokensSaved)
	fmt.Fprintf(w, "  local_input_tokens_saved:          %d\n", report.Economics.LocalInputTokensSaved)
	fmt.Fprintf(w, "  request_side_bytes_reduced:        %d\n", report.Economics.RequestSideBytesReduced)
	fmt.Fprintf(w, "  output_wire_bytes_saved:           %d\n", report.Economics.OutputWireBytesSaved)
	fmt.Fprintf(w, "  provider_cache_read_tokens:        %d\n", report.Economics.ProviderCacheReadTokens)
	fmt.Fprintf(w, "  provider_cache_create_tokens:      %d\n", report.Economics.ProviderCacheCreateTokens)
	fmt.Fprintf(w, "  tool_prune_tokens_saved:           %d\n", report.Economics.ToolPruneTokensSaved)
	fmt.Fprintf(w, "  output_reduce_injected_turns:      %d\n", report.Economics.OutputReduceInjectedTurns)
	fmt.Fprintf(w, "  output_reduce_input_overhead:      %d\n", report.Economics.OutputReduceInputOverhead)
	fmt.Fprintf(w, "  output_reduce_observed_tokens:     %d\n", report.Economics.OutputReduceObservedTokens)
	fmt.Fprintf(w, "  output_reduce_net_observed_tokens: %d\n", report.Economics.OutputReduceNetObservedTokens)
	fmt.Fprintf(w, "Host/safety: host_budget_ok=%d host_budget_issues=%d proof_event_loss=%d safety_issues=%d expected_zero_local_violations=%d resource_profile_ok=%v\n",
		report.HostBudgetOKRows,
		report.HostBudgetIssueRows,
		report.ProofEventLossRows,
		report.SafetyIssueRows,
		report.ExpectedZeroLocalViolations,
		report.ResourceProfileProofOK)
	if len(report.ResourceProfileProofClients) > 0 {
		fmt.Fprintf(w, "Resource proof clients: %s\n", strings.Join(report.ResourceProfileProofClients, ","))
	}
	if report.SearchCapProof != nil {
		fmt.Fprintf(w, "Search-cap proof: ok=%v captures=%d cli=%d desktop=%d selected=%s %d/%d extra_tokens=%d min_retention=%.2f%% downstream=%v downstream_candidates=%d passing=%d\n",
			report.SearchCapProof.OK,
			report.SearchCapProof.Captures,
			report.SearchCapProof.CLI,
			report.SearchCapProof.Desktop,
			report.SearchCapProof.SelectedCandidate,
			report.SearchCapProof.MaxFilesShown,
			report.SearchCapProof.MaxMatchesPerFile,
			report.SearchCapProof.TotalExtraReducerTokens,
			report.SearchCapProof.MinMatchRetentionPct,
			report.SearchCapProof.DownstreamStateProof,
			report.SearchCapProof.DownstreamCandidates,
			report.SearchCapProof.DownstreamPassing)
	}
	if report.CodexRouteHygiene != nil {
		fmt.Fprintf(w, "Codex route hygiene: ok=%v before=%s after=%s\n",
			report.CodexRouteHygiene.OK,
			report.CodexRouteHygiene.Before,
			report.CodexRouteHygiene.After)
	}
	fmt.Fprintf(w, "Live reducer hits: %s\n", formatInventoryInt64Map(report.LiveReducerHits))
	if len(report.MaxxWorkloadStatus) > 0 {
		fmt.Fprintln(w, "Maxx workload status:")
		for _, status := range report.MaxxWorkloadStatus {
			fmt.Fprintf(w, "  %s: complete=%v rows=%d positive=%d host_budget_ok=%d missing=%s\n",
				status.WorkloadClass,
				status.Complete,
				status.Rows,
				status.PositiveTokenRows,
				status.HostBudgetOKRows,
				formatInventoryStringSlice(status.MissingSignals))
		}
	}
	if report.GatePassed {
		fmt.Fprintln(w, "Gate: PASS")
		return
	}
	fmt.Fprintln(w, "Gate: FAIL")
	for _, failure := range report.GateFailures {
		fmt.Fprintf(w, "  - %s\n", failure)
	}
}

func cloneInventoryIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneInventoryInt64Map(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

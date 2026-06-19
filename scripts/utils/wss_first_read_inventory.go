package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
)

type wssFirstReadInventoryFlags struct {
	path                   string
	outputFormat           string
	since                  time.Time
	requireDependencyTrace bool
	help                   bool
}

type wssFirstReadInventoryReport struct {
	Path                           string                        `json:"path"`
	Logs                           int                           `json:"logs"`
	PhaseFRequests                 int                           `json:"phasef_requests"`
	OriginalTokens                 int                           `json:"original_tokens"`
	LocalSavedTokens               int                           `json:"local_saved_tokens"`
	LocalSavingsRatio              float64                       `json:"local_savings_ratio"`
	ProviderCachedTokens           int                           `json:"provider_cached_tokens"`
	CandidateRequests              int                           `json:"candidate_requests"`
	ReadLikeRequests               int                           `json:"read_like_requests"`
	SourceToolRequests             int                           `json:"source_tool_requests"`
	RequestsWithoutCandidateFacts  int                           `json:"requests_without_candidate_facts,omitempty"`
	FirstReadFullPassRequests      int                           `json:"first_read_full_pass_requests"`
	CandidateOutputBytes           int                           `json:"candidate_output_bytes"`
	CandidateOutputTokensEstimate  int                           `json:"candidate_output_tokens_estimate"`
	ExistingReadDeltaRequests      int                           `json:"existing_read_delta_requests"`
	ExistingReadDeltaSavedTokens   int                           `json:"existing_read_delta_saved_tokens"`
	FirstObservationSeededRequests int                           `json:"first_observation_seeded_requests"`
	ReReadRequests                 int                           `json:"re_read_requests"`
	ReReadCount                    int                           `json:"re_read_count"`
	ContextRiskRequests            int                           `json:"context_risk_requests"`
	DependencyTraceRequests        int                           `json:"dependency_trace_requests"`
	MissingDependencyTraceRequests int                           `json:"missing_dependency_trace_requests"`
	DependencyTraceFacts           map[string]int                `json:"dependency_trace_facts,omitempty"`
	RequestShapes                  map[string]int                `json:"request_shapes,omitempty"`
	ReadLikeByShape                map[string]int                `json:"read_like_by_shape,omitempty"`
	ToolCommandClasses             map[string]int                `json:"tool_command_classes,omitempty"`
	Reasons                        map[string]int                `json:"reasons,omitempty"`
	Verdict                        string                        `json:"verdict"`
	NextAction                     string                        `json:"next_action"`
	PerLog                         []wssFirstReadInventoryLogRow `json:"per_log,omitempty"`
	Notes                          []string                      `json:"notes,omitempty"`
}

type wssFirstReadInventoryLogRow struct {
	Name                          string  `json:"name"`
	Path                          string  `json:"path"`
	PhaseFRequests                int     `json:"phasef_requests"`
	OriginalTokens                int     `json:"original_tokens"`
	LocalSavedTokens              int     `json:"local_saved_tokens"`
	CandidateRequests             int     `json:"candidate_requests"`
	RequestsWithoutCandidateFacts int     `json:"requests_without_candidate_facts,omitempty"`
	CandidateOutputTokensEstimate int     `json:"candidate_output_tokens_estimate"`
	DependencyTraceRequests       int     `json:"dependency_trace_requests"`
	ContextRiskRequests           int     `json:"context_risk_requests"`
	ReReadCount                   int     `json:"re_read_count"`
	LocalSavingsRatio             float64 `json:"local_savings_ratio"`
}

type wssFirstReadInventoryRequestSignals struct {
	candidate                 bool
	readLike                  bool
	sourceTool                bool
	firstReadFullPass         bool
	readDelta                 bool
	firstObservationSeeded    bool
	contextRisk               bool
	dependencyTrace           bool
	candidateOutputBytes      int
	readDeltaSavedTokens      int
	reasons                   []string
	dependencyTraceFactCounts map[string]int
	toolCommandClasses        map[string]int
}

type wssFirstReadInventoryAccumulator struct {
	report wssFirstReadInventoryReport
}

const wssFirstReadInventoryHelpText = `wss-first-read-inventory: content-free first-read/scan-read dependency audit

Usage:
  go run ./scripts/utils wss-first-read-inventory <dir-or-decisions.jsonl> [flags]

Flags:
  --since=<rfc3339>              Ignore records before this timestamp
  --since-file=<path>            Read RFC3339 --since value from file
  --require-dependency-trace     Exit 1 unless every candidate has dependency trace facts
  --json                         Output JSON

Directory mode scans recursively for decisions.jsonl, *.decisions.jsonl, and
session*.jsonl live-corpus exports. The report never reads raw tool output. It
counts WSS Phase-F read-like/source-tool rows, estimates first-read output token
mass from byte facts, and proves whether content-free file/range/hash/edit-lineage
dependency facts exist. Missing trace means first_read_elision remains
shadow-only; it is not proof of no savings.`

var wssFirstReadDependencyTraceFactKeys = []string{
	"wss.read_file_hash",
	"wss.read_file_path_hash",
	"wss.read_range",
	"wss.read_range_hash",
	"wss.read_first_seen",
	"wss.read_after_edit",
	"wss.edit_turn_seq",
	"wss.file_hash_before",
	"wss.file_hash_after",
	"wss.changed_range",
	"wss.omitted_range_needed",
	"wss.dependency_trace",
}

func runWSSFirstReadInventory(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSFirstReadInventoryFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssFirstReadInventoryHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-first-read-inventory <dir-or-decisions.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSFirstReadInventory(flags)
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
	} else {
		writeWSSFirstReadInventoryText(stdout, report)
	}
	if flags.requireDependencyTrace && report.MissingDependencyTraceRequests > 0 {
		fmt.Fprintf(stderr, "wss-first-read-inventory: dependency trace missing on %d candidate requests\n", report.MissingDependencyTraceRequests)
		return 1
	}
	return 0
}

func parseWSSFirstReadInventoryFlags(args []string) (wssFirstReadInventoryFlags, error) {
	flags := wssFirstReadInventoryFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--require-dependency-trace":
			flags.requireDependencyTrace = true
		case arg == "--since":
			value, err := aggregateFlagValue(args, &i, arg)
			if err != nil {
				return flags, err
			}
			since, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return flags, fmt.Errorf("--since must be RFC3339: %w", err)
			}
			flags.since = since
		case strings.HasPrefix(arg, "--since="):
			since, err := time.Parse(time.RFC3339, strings.TrimPrefix(arg, "--since="))
			if err != nil {
				return flags, fmt.Errorf("--since must be RFC3339: %w", err)
			}
			flags.since = since
		case arg == "--since-file":
			value, err := aggregateFlagValue(args, &i, arg)
			if err != nil {
				return flags, err
			}
			since, err := parseWSSSinceFile(value)
			if err != nil {
				return flags, err
			}
			flags.since = since
		case strings.HasPrefix(arg, "--since-file="):
			since, err := parseWSSSinceFile(strings.TrimPrefix(arg, "--since-file="))
			if err != nil {
				return flags, err
			}
			flags.since = since
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple inventory roots provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func loadWSSFirstReadInventory(flags wssFirstReadInventoryFlags) (wssFirstReadInventoryReport, error) {
	paths, err := wssFirstReadInventoryPaths(flags.path)
	if err != nil {
		return wssFirstReadInventoryReport{}, err
	}
	if len(paths) == 0 {
		return wssFirstReadInventoryReport{}, fmt.Errorf("no decisions logs found under %s", flags.path)
	}
	acc := wssFirstReadInventoryAccumulator{report: wssFirstReadInventoryReport{Path: flags.path}}
	for _, path := range paths {
		summaries, err := dbg.ReplaySession(path)
		if err != nil {
			return wssFirstReadInventoryReport{}, fmt.Errorf("read decisions %s: %w", path, err)
		}
		logRow := wssFirstReadInventoryLogRow{Name: wssLocalGapInventoryName(path), Path: path}
		logged := false
		for _, summary := range summaries {
			if !flags.since.IsZero() {
				if summary.Timestamp.IsZero() || summary.Timestamp.Before(flags.since) {
					continue
				}
			}
			route := wssAuditRouteMode(summary)
			if !wssAuditIsWSS(summary, route) || !wssAuditIsPhaseF(route) {
				continue
			}
			logged = true
			acc.addPhaseF(summary, &logRow)
		}
		if logged {
			finalizeWSSFirstReadInventoryLogRow(&logRow)
			acc.report.PerLog = append(acc.report.PerLog, logRow)
			acc.report.Logs++
		}
	}
	acc.finalize()
	return acc.report, nil
}

func (a *wssFirstReadInventoryAccumulator) addPhaseF(summary dbg.RequestSummary, logRow *wssFirstReadInventoryLogRow) {
	if a == nil {
		return
	}
	original := maxInt(0, summary.Tokens.Original)
	saved := maxInt(0, summary.Tokens.Saved)
	shape := wssAuditResolveRequestShape(summary).Shape
	if shape == "" {
		shape = "unknown"
	}
	signals := wssFirstReadRequestSignals(summary)

	a.report.PhaseFRequests++
	a.report.OriginalTokens += original
	a.report.LocalSavedTokens += saved
	a.report.ProviderCachedTokens += maxInt(0, summary.ProviderCachedTokens)
	addWSSAuditCount(&a.report.RequestShapes, shape)
	for class, count := range signals.toolCommandClasses {
		addWSSFirstReadCountN(&a.report.ToolCommandClasses, class, count)
	}
	for _, reason := range signals.reasons {
		addWSSAuditCount(&a.report.Reasons, reason)
	}

	logRow.PhaseFRequests++
	logRow.OriginalTokens += original
	logRow.LocalSavedTokens += saved

	if summary.ReReadCount > 0 {
		a.report.ReReadRequests++
		a.report.ReReadCount += summary.ReReadCount
		logRow.ReReadCount += summary.ReReadCount
	}
	if !wssFirstReadHasCandidateTelemetry(summary.DebugFacts) {
		a.report.RequestsWithoutCandidateFacts++
		logRow.RequestsWithoutCandidateFacts++
	}
	if !signals.candidate {
		return
	}

	a.report.CandidateRequests++
	a.report.CandidateOutputBytes += signals.candidateOutputBytes
	a.report.CandidateOutputTokensEstimate += tokens.Estimate(signals.candidateOutputBytes)
	logRow.CandidateRequests++
	logRow.CandidateOutputTokensEstimate += tokens.Estimate(signals.candidateOutputBytes)
	if signals.readLike {
		a.report.ReadLikeRequests++
		addWSSAuditCount(&a.report.ReadLikeByShape, shape)
	}
	if signals.sourceTool {
		a.report.SourceToolRequests++
	}
	if signals.firstReadFullPass {
		a.report.FirstReadFullPassRequests++
	}
	if signals.readDelta {
		a.report.ExistingReadDeltaRequests++
		a.report.ExistingReadDeltaSavedTokens += signals.readDeltaSavedTokens
	}
	if signals.firstObservationSeeded {
		a.report.FirstObservationSeededRequests++
	}
	if signals.contextRisk {
		a.report.ContextRiskRequests++
		logRow.ContextRiskRequests++
	}
	if signals.dependencyTrace {
		a.report.DependencyTraceRequests++
		logRow.DependencyTraceRequests++
		for fact, count := range signals.dependencyTraceFactCounts {
			addWSSFirstReadCountN(&a.report.DependencyTraceFacts, fact, count)
		}
	} else {
		a.report.MissingDependencyTraceRequests++
	}
}

func wssFirstReadRequestSignals(summary dbg.RequestSummary) wssFirstReadInventoryRequestSignals {
	classes := wssLocalGapFactCountPairs(summary.DebugFacts, "wss.tool_command_classes")
	readLike := classes["read_like"] > 0
	sourceTool := wssLocalGapFactInt(summary.DebugFacts, "wss.source_tool_results") > 0 ||
		wssLocalGapFactInt(summary.DebugFacts, "wss.source_tool_bytes") > 0
	candidate := readLike || sourceTool
	signals := wssFirstReadInventoryRequestSignals{
		candidate:          candidate,
		readLike:           readLike,
		sourceTool:         sourceTool,
		toolCommandClasses: classes,
	}
	if candidate {
		signals.candidateOutputBytes = maxInt(
			wssLocalGapFactInt(summary.DebugFacts, "wss.source_tool_bytes"),
			maxInt(
				wssLocalGapFactInt(summary.DebugFacts, "wss.source_tool_max_bytes"),
				wssLocalGapFactInt(summary.DebugFacts, "wss.tool_result_output_bytes"),
			),
		)
	}
	if strings.Contains(summary.BypassReason, "source_tool_output_full_pass") {
		signals.firstReadFullPass = true
		signals.contextRisk = true
		signals.reasons = append(signals.reasons, summary.BypassReason)
	}
	if summary.ReReadCount > 0 {
		signals.contextRisk = true
		signals.reasons = append(signals.reasons, "re_read_count")
	}
	for _, mechanism := range summary.Mechanisms {
		if strings.TrimSpace(mechanism.Name) != "read_delta" {
			continue
		}
		signals.readDelta = true
		signals.readDeltaSavedTokens += maxInt(0, mechanism.SavedTokens)
		if mechanism.Reason != "" {
			signals.reasons = append(signals.reasons, mechanism.Reason)
		}
		if strings.Contains(mechanism.Reason, "first_observation_seeded") {
			signals.firstObservationSeeded = true
		}
	}
	for _, decision := range summary.EvidenceDecisions {
		if decision.Mechanism == "read_delta" {
			signals.readDelta = true
			signals.readDeltaSavedTokens += maxInt(0, decision.SavedTokens)
			if decision.Action == evidence.ActionShadow && strings.Contains(decision.Reason, "first_observation_seeded") {
				signals.firstObservationSeeded = true
			}
		}
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			continue
		}
		signals.reasons = append(signals.reasons, reason)
		if wssFirstReadReasonIsFullPass(reason) {
			signals.firstReadFullPass = true
			signals.contextRisk = true
		}
	}
	signals.dependencyTraceFactCounts = wssFirstReadDependencyTraceFacts(summary.DebugFacts)
	signals.dependencyTrace = len(signals.dependencyTraceFactCounts) > 0
	return signals
}

func wssFirstReadHasCandidateTelemetry(facts map[string]string) bool {
	if facts == nil {
		return false
	}
	for _, key := range []string{
		"wss.tool_command_classes",
		"wss.source_tool_results",
		"wss.source_tool_bytes",
		"wss.source_tool_max_bytes",
		"wss.tool_result_output_bytes",
	} {
		if strings.TrimSpace(facts[key]) != "" {
			return true
		}
	}
	return false
}

func wssFirstReadInventoryPaths(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return []string{root}, nil
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base == "decisions.jsonl" ||
			strings.HasSuffix(base, ".decisions.jsonl") ||
			(strings.HasPrefix(base, "session") && strings.HasSuffix(base, ".jsonl")) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	sort.Strings(paths)
	return paths, nil
}

func wssFirstReadReasonIsFullPass(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	return strings.Contains(reason, "source_tool_output_full_pass") ||
		strings.Contains(reason, "recent_edit_full_context") ||
		strings.Contains(reason, "post_collapse_reread_full_context") ||
		strings.Contains(reason, "first_read") ||
		strings.Contains(reason, "source_context_guard")
}

func wssFirstReadDependencyTraceFacts(facts map[string]string) map[string]int {
	if facts == nil {
		return nil
	}
	var out map[string]int
	for _, key := range wssFirstReadDependencyTraceFactKeys {
		if strings.TrimSpace(facts[key]) == "" {
			continue
		}
		addWSSFirstReadCountN(&out, key, 1)
	}
	return out
}

func (a *wssFirstReadInventoryAccumulator) finalize() {
	a.report.LocalSavingsRatio = wssLocalGapRatio(a.report.LocalSavedTokens, a.report.OriginalTokens)
	sort.Slice(a.report.PerLog, func(i, j int) bool {
		if a.report.PerLog[i].CandidateOutputTokensEstimate != a.report.PerLog[j].CandidateOutputTokensEstimate {
			return a.report.PerLog[i].CandidateOutputTokensEstimate > a.report.PerLog[j].CandidateOutputTokensEstimate
		}
		return a.report.PerLog[i].Name < a.report.PerLog[j].Name
	})
	a.report.Verdict = wssFirstReadInventoryVerdict(a.report)
	a.report.NextAction = wssFirstReadInventoryNextAction(a.report)
	a.report.Notes = wssFirstReadInventoryNotes(a.report)
}

func finalizeWSSFirstReadInventoryLogRow(row *wssFirstReadInventoryLogRow) {
	row.LocalSavingsRatio = wssLocalGapRatio(row.LocalSavedTokens, row.OriginalTokens)
}

func wssFirstReadInventoryVerdict(report wssFirstReadInventoryReport) string {
	switch {
	case report.PhaseFRequests == 0:
		return "no_data"
	case report.CandidateRequests == 0 && report.RequestsWithoutCandidateFacts > 0:
		return "candidate_telemetry_missing"
	case report.CandidateRequests == 0:
		return "no_first_read_surface"
	case report.MissingDependencyTraceRequests > 0:
		return "dependency_trace_missing"
	case report.ContextRiskRequests > 0 || report.ReReadCount > 0:
		return "promotion_blocked_dependency_risk"
	default:
		return "shadow_trace_ready"
	}
}

func wssFirstReadInventoryNextAction(report wssFirstReadInventoryReport) string {
	switch report.Verdict {
	case "no_data":
		return "capture WSS Phase-F decisions before evaluating first-read scan recovery"
	case "candidate_telemetry_missing":
		return "capture or export content-free tool-command/source byte facts before concluding first-read surface is absent"
	case "no_first_read_surface":
		return "do not build first-read mutation for this corpus; no read-like/source-tool surface was observed"
	case "dependency_trace_missing":
		return "keep first_read_elision shadow-only; add file/range/hash/edit-lineage dependency telemetry before designing any preview mutation"
	case "promotion_blocked_dependency_risk":
		return "keep first reads full-pass for risky classes; design mandatory exact expansion before edit/review/debug dependence"
	default:
		return "run a shadow preview protocol on traced captures, then prove no repair/re-read increase before any promotion"
	}
}

func wssFirstReadInventoryNotes(report wssFirstReadInventoryReport) []string {
	var notes []string
	notes = append(notes, "This report is content-free: it uses only decision facts, byte counts, command classes, and mechanism reasons.")
	if report.CandidateRequests > 0 {
		notes = append(notes, fmt.Sprintf("Estimated candidate output tokens are byte/4 planning estimates, not production-ready savings proof: %d tokens across %d candidate requests.", report.CandidateOutputTokensEstimate, report.CandidateRequests))
	}
	if report.MissingDependencyTraceRequests > 0 {
		notes = append(notes, "Missing dependency trace is a hard policy blocker for first-read elision: omitted lines could later be needed for edit, review, or debug decisions.")
	}
	if report.RequestsWithoutCandidateFacts > 0 {
		notes = append(notes, fmt.Sprintf("%d Phase-F requests lack candidate telemetry; no-surface verdicts require fresh rows with tool-command/source byte facts.", report.RequestsWithoutCandidateFacts))
	}
	if report.FirstObservationSeededRequests > 0 {
		notes = append(notes, "first_observation_seeded rows are healthy observe-only cache seeding, not first-read savings; do not count them as S_local until a later read hits safely.")
	}
	if report.ProviderCachedTokens > 0 {
		notes = append(notes, "Provider-cache tokens are reported separately and never counted as S_local.")
	}
	return notes
}

func writeWSSFirstReadInventoryText(w io.Writer, report wssFirstReadInventoryReport) {
	fmt.Fprintf(w, "=== WSS First-Read Inventory: %s ===\n", report.Path)
	fmt.Fprintf(w, "Logs / Phase-F requests:      %d / %d\n", report.Logs, report.PhaseFRequests)
	fmt.Fprintf(w, "S_local saved/ratio:          %d/%d / %.2f%%\n", report.LocalSavedTokens, report.OriginalTokens, report.LocalSavingsRatio*100)
	fmt.Fprintf(w, "Candidate read/source rows:   %d (read_like=%d source_tool=%d)\n", report.CandidateRequests, report.ReadLikeRequests, report.SourceToolRequests)
	fmt.Fprintf(w, "Missing candidate facts:      %d\n", report.RequestsWithoutCandidateFacts)
	fmt.Fprintf(w, "Candidate output est:         %d bytes / %d tokens\n", report.CandidateOutputBytes, report.CandidateOutputTokensEstimate)
	fmt.Fprintf(w, "Read-delta existing:          requests=%d saved=%d first_observation_seeded=%d\n", report.ExistingReadDeltaRequests, report.ExistingReadDeltaSavedTokens, report.FirstObservationSeededRequests)
	fmt.Fprintf(w, "Reread/context risk:          reread_requests=%d reread_count=%d context_risk_requests=%d\n", report.ReReadRequests, report.ReReadCount, report.ContextRiskRequests)
	fmt.Fprintf(w, "Dependency trace:             present=%d missing=%d facts=%s\n", report.DependencyTraceRequests, report.MissingDependencyTraceRequests, formatWSSAuditCounts(report.DependencyTraceFacts))
	fmt.Fprintf(w, "Provider cached tokens:       %d [separate, not S_local]\n", report.ProviderCachedTokens)
	fmt.Fprintf(w, "\nVerdict: %s\n", report.Verdict)
	fmt.Fprintf(w, "Next action: %s\n", report.NextAction)
	if len(report.RequestShapes) > 0 {
		fmt.Fprintf(w, "Request shapes: %s\n", formatWSSAuditCounts(report.RequestShapes))
	}
	if len(report.ReadLikeByShape) > 0 {
		fmt.Fprintf(w, "Read-like by shape: %s\n", formatWSSAuditCounts(report.ReadLikeByShape))
	}
	if len(report.ToolCommandClasses) > 0 {
		fmt.Fprintf(w, "Tool command classes: %s\n", formatWSSAuditCounts(report.ToolCommandClasses))
	}
	if len(report.Reasons) > 0 {
		fmt.Fprintf(w, "Reasons: %s\n", formatWSSAuditCounts(report.Reasons))
	}
	if len(report.PerLog) > 0 {
		fmt.Fprintln(w, "\nPer log:")
		for _, row := range report.PerLog {
			fmt.Fprintf(w, "  %-48s phasef=%d saved=%d/%d %.2f%% candidates=%d missing_facts=%d candidate_tokens=%d trace=%d risk=%d reread=%d\n",
				row.Name,
				row.PhaseFRequests,
				row.LocalSavedTokens,
				row.OriginalTokens,
				row.LocalSavingsRatio*100,
				row.CandidateRequests,
				row.RequestsWithoutCandidateFacts,
				row.CandidateOutputTokensEstimate,
				row.DependencyTraceRequests,
				row.ContextRiskRequests,
				row.ReReadCount)
		}
	}
	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
}

func addWSSFirstReadCountN(counts *map[string]int, key string, count int) {
	key = strings.TrimSpace(key)
	if counts == nil || key == "" || count <= 0 {
		return
	}
	if *counts == nil {
		*counts = make(map[string]int)
	}
	(*counts)[key] += count
}

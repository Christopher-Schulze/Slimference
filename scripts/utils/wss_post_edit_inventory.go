package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
)

type wssPostEditInventoryFlags struct {
	path              string
	outputFormat      string
	since             time.Time
	requireExactState bool
	help              bool
}

type wssPostEditInventoryReport struct {
	Path                               string                       `json:"path"`
	Logs                               int                          `json:"logs"`
	PhaseFRequests                     int                          `json:"phasef_requests"`
	OriginalTokens                     int                          `json:"original_tokens"`
	LocalSavedTokens                   int                          `json:"local_saved_tokens"`
	LocalSavingsRatio                  float64                      `json:"local_savings_ratio"`
	ProviderCachedTokens               int                          `json:"provider_cached_tokens"`
	RequestsWithoutCandidateFacts      int                          `json:"requests_without_candidate_facts,omitempty"`
	PostEditReadRequests               int                          `json:"post_edit_read_requests"`
	PostEditFullReadRequests           int                          `json:"post_edit_full_read_requests"`
	PostEditPartialReadRequests        int                          `json:"post_edit_partial_read_requests"`
	PostEditCandidateOutputBytes       int                          `json:"post_edit_candidate_output_bytes"`
	PostEditCandidateTokensEstimate    int                          `json:"post_edit_candidate_tokens_estimate"`
	ExactStateRequests                 int                          `json:"exact_state_requests"`
	MissingExactStateRequests          int                          `json:"missing_exact_state_requests"`
	RepeatedPostEditStateCandidates    int                          `json:"repeated_post_edit_state_candidates"`
	PatchContextRequests               int                          `json:"patch_context_requests"`
	PatchContextBytes                  int                          `json:"patch_context_bytes"`
	PatchContextTokensEstimate         int                          `json:"patch_context_tokens_estimate"`
	PatchContextExactTelemetryRequests int                          `json:"patch_context_exact_telemetry_requests"`
	MissingPatchContextTelemetry       int                          `json:"missing_patch_context_telemetry_requests"`
	RepeatedPatchContextCandidates     int                          `json:"repeated_patch_context_candidates"`
	PatchContextRepeatedApplied        int                          `json:"patch_context_repeated_applied"`
	PatchContextRepeatedSavedTokens    int                          `json:"patch_context_repeated_saved_tokens"`
	PatchContextRiskRequests           int                          `json:"patch_context_risk_requests"`
	ToolCommandClasses                 map[string]int               `json:"tool_command_classes,omitempty"`
	RequestShapes                      map[string]int               `json:"request_shapes,omitempty"`
	ExactStateFacts                    map[string]int               `json:"exact_state_facts,omitempty"`
	PatchContextKinds                  map[string]int               `json:"patch_context_kinds,omitempty"`
	RiskReasons                        map[string]int               `json:"risk_reasons,omitempty"`
	Verdict                            string                       `json:"verdict"`
	NextAction                         string                       `json:"next_action"`
	PerLog                             []wssPostEditInventoryLogRow `json:"per_log,omitempty"`
	Notes                              []string                     `json:"notes,omitempty"`
}

type wssPostEditInventoryLogRow struct {
	Name                            string  `json:"name"`
	Path                            string  `json:"path"`
	PhaseFRequests                  int     `json:"phasef_requests"`
	OriginalTokens                  int     `json:"original_tokens"`
	LocalSavedTokens                int     `json:"local_saved_tokens"`
	PostEditReadRequests            int     `json:"post_edit_read_requests"`
	ExactStateRequests              int     `json:"exact_state_requests"`
	MissingExactStateRequests       int     `json:"missing_exact_state_requests"`
	RepeatedPostEditStateCandidates int     `json:"repeated_post_edit_state_candidates"`
	PatchContextRequests            int     `json:"patch_context_requests"`
	RepeatedPatchContextCandidates  int     `json:"repeated_patch_context_candidates"`
	PatchContextRepeatedApplied     int     `json:"patch_context_repeated_applied"`
	LocalSavingsRatio               float64 `json:"local_savings_ratio"`
}

type wssPostEditRequestSignals struct {
	postEditRead           bool
	readFull               bool
	readPartial            bool
	candidateOutputBytes   int
	exactState             bool
	exactStateKey          string
	missingExactState      bool
	exactStateFacts        map[string]int
	patchContext           bool
	patchContextBytes      int
	patchContextKind       string
	patchContextHash       string
	patchContextKey        string
	patchContextExact      bool
	patchContextRisk       bool
	patchContextRiskReason string
	toolCommandClasses     map[string]int
}

type wssPostEditInventoryAccumulator struct {
	report       wssPostEditInventoryReport
	seenStateKey map[string]int
	seenPatchKey map[string]int
}

const wssPostEditInventoryHelpText = `wss-post-edit-inventory: content-free post-edit and patch-context audit

Usage:
  go run ./scripts/utils wss-post-edit-inventory <dir-or-decisions.jsonl> [flags]

Flags:
  --since=<rfc3339>              Ignore records before this timestamp
  --since-file=<path>            Read RFC3339 --since value from file
  --require-exact-state          Exit 1 when post-edit or patch candidates lack exact-state telemetry
  --json                         Output JSON

Directory mode scans recursively for decisions.jsonl, *.decisions.jsonl, and
session*.jsonl live-corpus exports. The report never reads raw tool output. It
counts post-edit read surfaces, exact file-state telemetry, repeated unchanged
post-edit read candidates, and exact-repeat patch/diff context candidates.
Missing exact-state facts mean predictive_post_edit and patch_context_dedup stay
shadow-only.`

var wssPostEditExactStateKeys = []string{
	"wss.read_file_path_hash",
	"wss.read_range_hash",
	"wss.file_hash_after",
	"wss.edit_turn_seq",
	"wss.changed_range",
}

func runWSSPostEditInventory(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSPostEditInventoryFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssPostEditInventoryHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-post-edit-inventory <dir-or-decisions.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSPostEditInventory(flags)
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
		writeWSSPostEditInventoryText(stdout, report)
	}
	if flags.requireExactState && (report.MissingExactStateRequests > 0 || report.MissingPatchContextTelemetry > 0) {
		fmt.Fprintf(stderr, "wss-post-edit-inventory: exact-state telemetry missing (post_edit=%d patch=%d)\n",
			report.MissingExactStateRequests, report.MissingPatchContextTelemetry)
		return 1
	}
	return 0
}

func parseWSSPostEditInventoryFlags(args []string) (wssPostEditInventoryFlags, error) {
	flags := wssPostEditInventoryFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--require-exact-state":
			flags.requireExactState = true
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

func loadWSSPostEditInventory(flags wssPostEditInventoryFlags) (wssPostEditInventoryReport, error) {
	paths, err := wssFirstReadInventoryPaths(flags.path)
	if err != nil {
		return wssPostEditInventoryReport{}, err
	}
	if len(paths) == 0 {
		return wssPostEditInventoryReport{}, fmt.Errorf("no decisions logs found under %s", flags.path)
	}
	acc := wssPostEditInventoryAccumulator{
		report:       wssPostEditInventoryReport{Path: flags.path},
		seenStateKey: map[string]int{},
		seenPatchKey: map[string]int{},
	}
	for _, path := range paths {
		summaries, err := dbg.ReplaySession(path)
		if err != nil {
			return wssPostEditInventoryReport{}, fmt.Errorf("read decisions %s: %w", path, err)
		}
		logRow := wssPostEditInventoryLogRow{Name: wssLocalGapInventoryName(path), Path: path}
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
			finalizeWSSPostEditInventoryLogRow(&logRow)
			acc.report.PerLog = append(acc.report.PerLog, logRow)
			acc.report.Logs++
		}
	}
	acc.finalize()
	return acc.report, nil
}

func (a *wssPostEditInventoryAccumulator) addPhaseF(summary dbg.RequestSummary, logRow *wssPostEditInventoryLogRow) {
	if a == nil {
		return
	}
	original := maxInt(0, summary.Tokens.Original)
	saved := maxInt(0, summary.Tokens.Saved)
	shape := wssAuditResolveRequestShape(summary).Shape
	if shape == "" {
		shape = "unknown"
	}
	signals := wssPostEditSignals(summary)

	a.report.PhaseFRequests++
	a.report.OriginalTokens += original
	a.report.LocalSavedTokens += saved
	a.report.ProviderCachedTokens += maxInt(0, summary.ProviderCachedTokens)
	addWSSAuditCount(&a.report.RequestShapes, shape)
	for class, count := range signals.toolCommandClasses {
		addWSSFirstReadCountN(&a.report.ToolCommandClasses, class, count)
	}
	if !wssPostEditHasCandidateTelemetry(summary.DebugFacts) {
		a.report.RequestsWithoutCandidateFacts++
	}

	logRow.PhaseFRequests++
	logRow.OriginalTokens += original
	logRow.LocalSavedTokens += saved

	if signals.postEditRead {
		a.addPostEditRead(signals, logRow)
	}
	if signals.patchContext {
		a.addPatchContext(signals, logRow)
	}
	a.addAppliedPatchContextRepeat(summary, signals, logRow)
}

func (a *wssPostEditInventoryAccumulator) addPostEditRead(signals wssPostEditRequestSignals, logRow *wssPostEditInventoryLogRow) {
	a.report.PostEditReadRequests++
	a.report.PostEditCandidateOutputBytes += signals.candidateOutputBytes
	a.report.PostEditCandidateTokensEstimate += tokens.Estimate(signals.candidateOutputBytes)
	logRow.PostEditReadRequests++
	if signals.readFull {
		a.report.PostEditFullReadRequests++
	}
	if signals.readPartial {
		a.report.PostEditPartialReadRequests++
	}
	if signals.exactState {
		a.report.ExactStateRequests++
		logRow.ExactStateRequests++
		for fact, count := range signals.exactStateFacts {
			addWSSFirstReadCountN(&a.report.ExactStateFacts, fact, count)
		}
		if signals.exactStateKey != "" && a.seenStateKey[signals.exactStateKey] > 0 {
			a.report.RepeatedPostEditStateCandidates++
			logRow.RepeatedPostEditStateCandidates++
		}
		a.seenStateKey[signals.exactStateKey]++
		return
	}
	if signals.missingExactState {
		a.report.MissingExactStateRequests++
		logRow.MissingExactStateRequests++
	}
}

func (a *wssPostEditInventoryAccumulator) addPatchContext(signals wssPostEditRequestSignals, logRow *wssPostEditInventoryLogRow) {
	a.report.PatchContextRequests++
	a.report.PatchContextBytes += signals.patchContextBytes
	a.report.PatchContextTokensEstimate += tokens.Estimate(signals.patchContextBytes)
	logRow.PatchContextRequests++
	if signals.patchContextKind != "" {
		addWSSAuditCount(&a.report.PatchContextKinds, signals.patchContextKind)
	}
	if signals.patchContextRisk {
		a.report.PatchContextRiskRequests++
		if signals.patchContextRiskReason != "" {
			addWSSAuditCount(&a.report.RiskReasons, signals.patchContextRiskReason)
		}
	}
	if signals.patchContextExact {
		a.report.PatchContextExactTelemetryRequests++
		if !signals.patchContextRisk {
			if signals.patchContextKey != "" && a.seenPatchKey[signals.patchContextKey] > 0 {
				a.report.RepeatedPatchContextCandidates++
				logRow.RepeatedPatchContextCandidates++
			}
			a.seenPatchKey[signals.patchContextKey]++
		}
		return
	}
	a.report.MissingPatchContextTelemetry++
}

func (a *wssPostEditInventoryAccumulator) addAppliedPatchContextRepeat(summary dbg.RequestSummary, signals wssPostEditRequestSignals, logRow *wssPostEditInventoryLogRow) {
	applied, savedTokens := wssPostEditAppliedRepeatedPatchContext(summary, signals)
	if applied == 0 {
		return
	}
	a.report.PatchContextRepeatedApplied += applied
	a.report.PatchContextRepeatedSavedTokens += savedTokens
	logRow.PatchContextRepeatedApplied += applied
}

func wssPostEditSignals(summary dbg.RequestSummary) wssPostEditRequestSignals {
	facts := summary.DebugFacts
	classes := wssLocalGapFactCountPairs(facts, "wss.tool_command_classes")
	postEditRead := parseBoolFact(wssPostEditFact(facts, "wss.read_after_edit")) ||
		wssLocalGapFactInt(facts, "wss.read_after_edit_count") > 0
	signals := wssPostEditRequestSignals{
		postEditRead:       postEditRead,
		toolCommandClasses: classes,
	}
	if postEditRead {
		signals.readFull = wssLocalGapFactInt(facts, "wss.read_full_count") > 0
		signals.readPartial = wssLocalGapFactInt(facts, "wss.read_partial_count") > 0
		signals.candidateOutputBytes = maxInt(
			wssLocalGapFactInt(facts, "wss.source_tool_bytes"),
			wssLocalGapFactInt(facts, "wss.tool_result_output_bytes"),
		)
		signals.exactStateFacts = wssPostEditExactStateFacts(facts)
		signals.exactState = wssPostEditHasExactState(facts)
		signals.missingExactState = !signals.exactState
		if signals.exactState {
			signals.exactStateKey = strings.Join([]string{
				wssPostEditFact(facts, "wss.read_file_path_hash"),
				wssPostEditFact(facts, "wss.read_range_hash"),
				wssPostEditFact(facts, "wss.file_hash_after"),
				wssPostEditFact(facts, "wss.edit_turn_seq"),
				wssPostEditFact(facts, "wss.changed_range"),
			}, "|")
		}
	}
	signals.patchContext = wssPostEditPatchCandidate(facts, classes)
	if signals.patchContext {
		signals.patchContextBytes = maxInt(
			wssLocalGapFactInt(facts, "wss.patch_context_bytes"),
			maxInt(
				wssLocalGapFactInt(facts, "wss.tool_result_output_bytes"),
				wssLocalGapFactInt(facts, "wss.source_tool_bytes"),
			),
		)
		signals.patchContextKind = strings.TrimSpace(wssPostEditFact(facts, "wss.patch_context_kind"))
		signals.patchContextHash = strings.TrimSpace(wssPostEditFact(facts, "wss.patch_context_hash"))
		signals.patchContextKey, signals.patchContextExact = wssPostEditPatchExactKey(facts)
		signals.patchContextRisk, signals.patchContextRiskReason = wssPostEditPatchRisk(summary)
	}
	return signals
}

func wssPostEditHasCandidateTelemetry(facts map[string]string) bool {
	if facts == nil {
		return false
	}
	for _, key := range []string{
		"wss.tool_command_classes",
		"wss.read_after_edit",
		"wss.read_after_edit_count",
		"wss.patch_context_candidate",
		"wss.patch_context_hash",
	} {
		if strings.TrimSpace(facts[key]) != "" {
			return true
		}
	}
	return false
}

func wssPostEditExactStateFacts(facts map[string]string) map[string]int {
	if facts == nil {
		return nil
	}
	var out map[string]int
	for _, key := range wssPostEditExactStateKeys {
		if strings.TrimSpace(facts[key]) != "" {
			addWSSFirstReadCountN(&out, key, 1)
		}
	}
	return out
}

func wssPostEditHasExactState(facts map[string]string) bool {
	if facts == nil {
		return false
	}
	for _, key := range wssPostEditExactStateKeys {
		if strings.TrimSpace(facts[key]) == "" {
			return false
		}
	}
	return true
}

func wssPostEditPatchCandidate(facts map[string]string, classes map[string]int) bool {
	if parseBoolFact(wssPostEditFact(facts, "wss.patch_context_candidate")) {
		return true
	}
	for _, class := range []string{"git_diff", "git_diff_stat", "git_show_stat", "patch_context", "apply_patch"} {
		if classes[class] > 0 {
			return true
		}
	}
	return false
}

func wssPostEditPatchExactKey(facts map[string]string) (string, bool) {
	kind := wssPostEditFact(facts, "wss.patch_context_kind")
	if kind == "" {
		kind = wssPostEditFact(facts, "wss.patch_context_kinds")
	}
	hash := wssPostEditFact(facts, "wss.patch_context_hash")
	if hash == "" {
		hash = wssPostEditFact(facts, "wss.patch_context_hashes")
	}
	if kind == "" || hash == "" {
		return "", false
	}
	count := wssPostEditFact(facts, "wss.patch_context_hash_count")
	return strings.Join([]string{kind, count, hash}, "|"), true
}

func wssPostEditPatchRisk(summary dbg.RequestSummary) (bool, string) {
	facts := summary.DebugFacts
	for _, key := range []string{
		"wss.patch_context_failed",
		"wss.patch_context_conflict",
		"wss.patch_context_rejected",
		"wss.patch_context_binary",
		"wss.patch_context_rename",
	} {
		if parseBoolFact(wssPostEditFact(facts, key)) {
			return true, key
		}
	}
	text := strings.ToLower(strings.Join(wssPostEditReasons(summary), " "))
	for _, marker := range []string{"conflict", "rejected", "failed apply", "apply_patch_failed", "binary patch", "rename"} {
		if strings.Contains(text, marker) {
			return true, marker
		}
	}
	return false, ""
}

func wssPostEditAppliedRepeatedPatchContext(summary dbg.RequestSummary, signals wssPostEditRequestSignals) (int, int) {
	if signals.patchContextRisk || wssPostEditSummaryHasPatchRisk(summary) {
		return 0, 0
	}
	applied := 0
	savedTokens := 0
	for _, decision := range summary.EvidenceDecisions {
		if decision.Mechanism != "repeated_tool_output" ||
			decision.Action != evidence.ActionApplied ||
			decision.ContentClass != evidence.ContentDiff {
			continue
		}
		saved := decision.NetTokens
		if saved <= 0 {
			saved = decision.SavedTokens
		}
		if saved <= 0 {
			continue
		}
		applied++
		savedTokens += saved
	}
	return applied, savedTokens
}

func wssPostEditSummaryHasPatchRisk(summary dbg.RequestSummary) bool {
	risk, _ := wssPostEditPatchRisk(summary)
	return risk
}

func wssPostEditReasons(summary dbg.RequestSummary) []string {
	var out []string
	if summary.BypassReason != "" {
		out = append(out, summary.BypassReason)
	}
	out = append(out, summary.Errors...)
	for _, mechanism := range summary.Mechanisms {
		if mechanism.Reason != "" {
			out = append(out, mechanism.Reason)
		}
	}
	for _, decision := range summary.EvidenceDecisions {
		if decision.Reason != "" {
			out = append(out, decision.Reason)
		}
	}
	return out
}

func wssPostEditFact(facts map[string]string, key string) string {
	if facts == nil {
		return ""
	}
	return strings.TrimSpace(facts[key])
}

func (a *wssPostEditInventoryAccumulator) finalize() {
	a.report.LocalSavingsRatio = wssLocalGapRatio(a.report.LocalSavedTokens, a.report.OriginalTokens)
	sort.Slice(a.report.PerLog, func(i, j int) bool {
		if a.report.PerLog[i].PostEditReadRequests != a.report.PerLog[j].PostEditReadRequests {
			return a.report.PerLog[i].PostEditReadRequests > a.report.PerLog[j].PostEditReadRequests
		}
		if a.report.PerLog[i].PatchContextRequests != a.report.PerLog[j].PatchContextRequests {
			return a.report.PerLog[i].PatchContextRequests > a.report.PerLog[j].PatchContextRequests
		}
		return a.report.PerLog[i].Name < a.report.PerLog[j].Name
	})
	a.report.Verdict = wssPostEditInventoryVerdict(a.report)
	a.report.NextAction = wssPostEditInventoryNextAction(a.report)
	a.report.Notes = wssPostEditInventoryNotes(a.report)
}

func finalizeWSSPostEditInventoryLogRow(row *wssPostEditInventoryLogRow) {
	row.LocalSavingsRatio = wssLocalGapRatio(row.LocalSavedTokens, row.OriginalTokens)
}

func wssPostEditInventoryVerdict(report wssPostEditInventoryReport) string {
	switch {
	case report.PhaseFRequests == 0:
		return "no_data"
	case report.PostEditReadRequests == 0 && report.PatchContextRequests == 0 && report.RequestsWithoutCandidateFacts > 0:
		return "candidate_telemetry_missing"
	case report.PostEditReadRequests == 0 && report.PatchContextRequests == 0:
		return "no_post_edit_or_patch_surface"
	case report.MissingExactStateRequests > 0:
		return "post_edit_exact_state_missing"
	case report.PatchContextRepeatedApplied > 0 && report.PatchContextRiskRequests > 0:
		return "product_exact_repeat_active_with_risk_full_pass"
	case report.PatchContextRepeatedApplied > 0 && report.MissingPatchContextTelemetry > 0:
		return "product_exact_repeat_active_with_telemetry_gap"
	case report.MissingPatchContextTelemetry > 0:
		return "patch_context_telemetry_missing"
	case report.PatchContextRiskRequests > 0:
		return "promotion_blocked_patch_risk"
	case report.PatchContextRepeatedApplied > 0:
		return "product_exact_repeat_active"
	case report.RepeatedPostEditStateCandidates == 0 && report.RepeatedPatchContextCandidates == 0:
		return "shadow_measure_only_no_repeat"
	default:
		return "shadow_exact_repeat_ready"
	}
}

func wssPostEditInventoryNextAction(report wssPostEditInventoryReport) string {
	switch report.Verdict {
	case "no_data":
		return "capture WSS Phase-F decisions before evaluating post-edit or patch-context savings"
	case "candidate_telemetry_missing":
		return "export content-free post-edit and patch-context facts before concluding this surface is absent"
	case "no_post_edit_or_patch_surface":
		return "do not build post-edit or patch-context mutation for this corpus; no candidate surface was observed"
	case "post_edit_exact_state_missing":
		return "keep predictive_post_edit shadow-only; add file-hash-after, edit-turn, changed-range, path-hash, and range-hash telemetry before any reducer design"
	case "patch_context_telemetry_missing":
		return "keep patch_context_dedup shadow-only; add exact patch/diff hash, kind, byte count, and failure/conflict flags before any reducer design"
	case "promotion_blocked_patch_risk":
		return "full-pass patch/diff conflict, failed apply, rejected hunk, binary, and rename classes; measure only exact clean repeats"
	case "product_exact_repeat_active_with_risk_full_pass":
		return "keep clean exact-repeat patch/diff savings active; preserve risk full-pass guards and add fresh live rows before considering unchanged-range dedup"
	case "product_exact_repeat_active_with_telemetry_gap":
		return "keep applied exact-repeat patch/diff savings active; add exact patch hash/kind/byte/risk facts to remaining candidate rows before widening the design"
	case "product_exact_repeat_active":
		return "keep clean exact-repeat patch/diff savings active; add negative live rows before considering unchanged-context range dedup"
	case "shadow_measure_only_no_repeat":
		return "retain measurement only; exact-state telemetry exists but this corpus has no repeated unchanged post-edit or patch-context surface"
	default:
		return "prototype replay-only exact repeat compaction with byte-identical expansion tests before any product mutation"
	}
}

func wssPostEditInventoryNotes(report wssPostEditInventoryReport) []string {
	var notes []string
	notes = append(notes, "This report is content-free: it uses only decision facts, byte counts, hashed state facts, command classes, and mechanism reasons.")
	if report.PostEditReadRequests > 0 {
		notes = append(notes, fmt.Sprintf("Post-edit candidate token estimates are planning estimates only: %d tokens across %d request(s).", report.PostEditCandidateTokensEstimate, report.PostEditReadRequests))
	}
	if report.PatchContextRequests > 0 {
		notes = append(notes, fmt.Sprintf("Patch-context token estimates are planning estimates only: %d tokens across %d request(s).", report.PatchContextTokensEstimate, report.PatchContextRequests))
	}
	if report.PatchContextRepeatedApplied > 0 {
		notes = append(notes, fmt.Sprintf("Applied exact-repeat patch/diff savings are real product savings: %d applied block(s), %d saved token(s), provider cache excluded.", report.PatchContextRepeatedApplied, report.PatchContextRepeatedSavedTokens))
	}
	if report.MissingExactStateRequests > 0 {
		notes = append(notes, "Missing exact post-edit state is a hard blocker: first post-edit reads must stay full-context until file version, edit turn, changed range, path, and read range are all known.")
	}
	if report.MissingPatchContextTelemetry > 0 {
		notes = append(notes, "Missing exact patch telemetry is a hard blocker: patch_context_dedup may only consider byte-identical repeated patch/diff context with failure/conflict classes separated.")
	}
	if report.ProviderCachedTokens > 0 {
		notes = append(notes, "Provider-cache tokens are reported separately and never counted as S_local.")
	}
	return notes
}

func writeWSSPostEditInventoryText(w io.Writer, report wssPostEditInventoryReport) {
	fmt.Fprintf(w, "=== WSS Post-Edit Inventory: %s ===\n", report.Path)
	fmt.Fprintf(w, "Logs / Phase-F requests:       %d / %d\n", report.Logs, report.PhaseFRequests)
	fmt.Fprintf(w, "S_local saved/ratio:           %d/%d / %.2f%%\n", report.LocalSavedTokens, report.OriginalTokens, report.LocalSavingsRatio*100)
	fmt.Fprintf(w, "Post-edit reads:               %d (full=%d partial=%d)\n", report.PostEditReadRequests, report.PostEditFullReadRequests, report.PostEditPartialReadRequests)
	fmt.Fprintf(w, "Post-edit candidate est:       %d bytes / %d tokens\n", report.PostEditCandidateOutputBytes, report.PostEditCandidateTokensEstimate)
	fmt.Fprintf(w, "Exact state:                   present=%d missing=%d repeat_candidates=%d facts=%s\n", report.ExactStateRequests, report.MissingExactStateRequests, report.RepeatedPostEditStateCandidates, formatWSSAuditCounts(report.ExactStateFacts))
	fmt.Fprintf(w, "Patch context:                 requests=%d exact=%d missing=%d repeat_candidates=%d risk=%d\n", report.PatchContextRequests, report.PatchContextExactTelemetryRequests, report.MissingPatchContextTelemetry, report.RepeatedPatchContextCandidates, report.PatchContextRiskRequests)
	fmt.Fprintf(w, "Patch exact-repeat applied:    blocks=%d saved_tokens=%d\n", report.PatchContextRepeatedApplied, report.PatchContextRepeatedSavedTokens)
	fmt.Fprintf(w, "Patch candidate est:           %d bytes / %d tokens\n", report.PatchContextBytes, report.PatchContextTokensEstimate)
	fmt.Fprintf(w, "Provider cached tokens:        %d [separate, not S_local]\n", report.ProviderCachedTokens)
	fmt.Fprintf(w, "\nVerdict: %s\n", report.Verdict)
	fmt.Fprintf(w, "Next action: %s\n", report.NextAction)
	if len(report.RequestShapes) > 0 {
		fmt.Fprintf(w, "Request shapes: %s\n", formatWSSAuditCounts(report.RequestShapes))
	}
	if len(report.ToolCommandClasses) > 0 {
		fmt.Fprintf(w, "Tool command classes: %s\n", formatWSSAuditCounts(report.ToolCommandClasses))
	}
	if len(report.PatchContextKinds) > 0 {
		fmt.Fprintf(w, "Patch context kinds: %s\n", formatWSSAuditCounts(report.PatchContextKinds))
	}
	if len(report.RiskReasons) > 0 {
		fmt.Fprintf(w, "Risk reasons: %s\n", formatWSSAuditCounts(report.RiskReasons))
	}
	if len(report.PerLog) > 0 {
		fmt.Fprintln(w, "\nPer log:")
		for _, row := range report.PerLog {
			fmt.Fprintf(w, "  %-48s phasef=%d saved=%d/%d %.2f%% post_edit=%d exact=%d missing=%d repeat=%d patch=%d patch_repeat=%d patch_applied=%d\n",
				row.Name,
				row.PhaseFRequests,
				row.LocalSavedTokens,
				row.OriginalTokens,
				row.LocalSavingsRatio*100,
				row.PostEditReadRequests,
				row.ExactStateRequests,
				row.MissingExactStateRequests,
				row.RepeatedPostEditStateCandidates,
				row.PatchContextRequests,
				row.RepeatedPatchContextCandidates,
				row.PatchContextRepeatedApplied)
		}
	}
	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
}

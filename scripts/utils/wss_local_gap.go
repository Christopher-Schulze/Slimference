package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

type wssLocalGapFlags struct {
	path          string
	outputFormat  string
	since         time.Time
	minLocalRatio float64
	minLocalSaved int
	help          bool
}

type wssLocalGapReport struct {
	Path                    string                       `json:"path"`
	Since                   *time.Time                   `json:"since,omitempty"`
	Requests                int                          `json:"requests"`
	WSSRequests             int                          `json:"wss_requests"`
	PhaseFRequests          int                          `json:"phasef_requests"`
	OriginalTokens          int                          `json:"original_tokens"`
	FinalTokens             int                          `json:"final_tokens"`
	LocalSavedTokens        int                          `json:"local_saved_tokens"`
	LocalSavingsRatio       float64                      `json:"local_savings_ratio"`
	ProviderCacheReadTokens int                          `json:"provider_cache_read_tokens"`
	ProviderCacheTokens     int                          `json:"provider_cache_tokens"`
	ProviderCacheCreate     int                          `json:"provider_cache_create_tokens"`
	OutputTokens            int                          `json:"output_tokens"`
	TargetRatio             float64                      `json:"target_ratio"`
	TargetSavedTokens       int                          `json:"target_saved_tokens"`
	TargetDeficitTokens     int                          `json:"target_deficit_tokens"`
	PositiveSavingsRequests int                          `json:"positive_savings_requests"`
	ZeroSavingsRequests     int                          `json:"zero_savings_requests"`
	ZeroSavingsOrigTokens   int                          `json:"zero_savings_original_tokens"`
	NoEvidenceRequests      int                          `json:"no_evidence_requests"`
	NoEvidenceOrigTokens    int                          `json:"no_evidence_original_tokens"`
	ErrorRequests           int                          `json:"error_requests"`
	UpstreamErrorRequests   int                          `json:"upstream_error_requests"`
	HTTP400ErrorRequests    int                          `json:"http_400_error_requests"`
	RequestShapes           []wssLocalGapShapeRow        `json:"request_shapes,omitempty"`
	RequestGuards           []wssLocalGapRequestGuardRow `json:"request_guards,omitempty"`
	Mechanisms              []wssLocalGapMechanismRow    `json:"mechanisms,omitempty"`
	Guards                  []wssLocalGapGuardRow        `json:"guards,omitempty"`
	ContentClasses          []wssLocalGapContentClassRow `json:"content_classes,omitempty"`
	GatePassed              bool                         `json:"gate_passed"`
	GateFailures            []string                     `json:"gate_failures,omitempty"`
	Notes                   []string                     `json:"notes,omitempty"`
}

type wssLocalGapShapeRow struct {
	Shape               string  `json:"shape"`
	Requests            int     `json:"requests"`
	OriginalTokens      int     `json:"original_tokens"`
	LocalSavedTokens    int     `json:"local_saved_tokens"`
	LocalSavingsRatio   float64 `json:"local_savings_ratio"`
	ProviderCacheTokens int     `json:"provider_cache_tokens"`
	GuardedPotential    int     `json:"guarded_potential_tokens"`
	TargetDeficitTokens int     `json:"target_deficit_tokens"`
	ZeroSavingsRequests int     `json:"zero_savings_requests"`
	ZeroSavingsOrig     int     `json:"zero_savings_original_tokens"`
	NoEvidenceRequests  int     `json:"no_evidence_requests"`
	NoEvidenceOrig      int     `json:"no_evidence_original_tokens"`
	ErrorRequests       int     `json:"error_requests"`
}

type wssLocalGapRequestGuardRow struct {
	Guard                 string         `json:"guard"`
	Requests              int            `json:"requests"`
	OriginalTokens        int            `json:"original_tokens"`
	LocalSavedTokens      int            `json:"local_saved_tokens"`
	ZeroSavingsRequests   int            `json:"zero_savings_requests"`
	ZeroSavingsOrigTokens int            `json:"zero_savings_original_tokens"`
	RequestShapes         map[string]int `json:"request_shapes,omitempty"`
}

type wssLocalGapMechanismRow struct {
	Mechanism        string         `json:"mechanism"`
	Decisions        int            `json:"decisions"`
	Applied          int            `json:"applied"`
	FullPass         int            `json:"full_pass"`
	Skipped          int            `json:"skipped"`
	FailedOpen       int            `json:"failed_open"`
	OriginalTokens   int            `json:"original_tokens"`
	SavedTokens      int            `json:"saved_tokens"`
	NetTokens        int            `json:"net_tokens"`
	GuardedPotential int            `json:"guarded_potential_tokens"`
	Reasons          map[string]int `json:"reasons,omitempty"`
	RequestShapes    map[string]int `json:"request_shapes,omitempty"`
}

type wssLocalGapGuardRow struct {
	Reason           string         `json:"reason"`
	Decisions        int            `json:"decisions"`
	OriginalTokens   int            `json:"original_tokens"`
	SavedTokens      int            `json:"saved_tokens"`
	NetTokens        int            `json:"net_tokens"`
	GuardedPotential int            `json:"guarded_potential_tokens"`
	Mechanisms       map[string]int `json:"mechanisms,omitempty"`
	RequestShapes    map[string]int `json:"request_shapes,omitempty"`
}

type wssLocalGapContentClassRow struct {
	ContentClass     string         `json:"content_class"`
	Decisions        int            `json:"decisions"`
	OriginalTokens   int            `json:"original_tokens"`
	SavedTokens      int            `json:"saved_tokens"`
	GuardedPotential int            `json:"guarded_potential_tokens"`
	Mechanisms       map[string]int `json:"mechanisms,omitempty"`
}

type wssLocalGapAccumulator struct {
	report        wssLocalGapReport
	shapeRows     map[string]*wssLocalGapShapeRow
	requestGuards map[string]*wssLocalGapRequestGuardRow
	mechanismRows map[string]*wssLocalGapMechanismRow
	guardRows     map[string]*wssLocalGapGuardRow
	contentRows   map[string]*wssLocalGapContentClassRow
}

const wssLocalGapHelpText = `wss-local-gap: rank the remaining WSS local-savings gap without provider-cache credit

Usage:
  go run ./scripts/utils wss-local-gap <decisions.jsonl> [flags]

Flags:
  --since=<rfc3339>                 Ignore records before this timestamp
  --min-local-ratio=<ratio>          Fail if S_local is below ratio, for example 0.48
  --min-local-saved=<tokens>         Fail if local saved tokens are below this floor
  --json                            Output JSON

Reads content-free RequestSummary JSONL records. S_local is tokens.saved /
tokens.original over WSS Phase-F rows only. Provider-cache read/create/cached
tokens are reported separately and never counted toward local savings. Target
deficit is the local-token reduction still needed to reach the requested floor.
Guarded potential is the original-token mass carried by evidence decisions whose
action is full_pass; it is an opportunity ledger, not a claim that all tokens are
safely recoverable.`

func runWSSLocalGap(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSLocalGapFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssLocalGapHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-local-gap <decisions.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSLocalGapReport(flags)
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
	writeWSSLocalGapText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseWSSLocalGapFlags(args []string) (wssLocalGapFlags, error) {
	flags := wssLocalGapFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
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
		case arg == "--min-local-ratio":
			value, err := aggregateFlagValue(args, &i, arg)
			if err != nil {
				return flags, err
			}
			ratio, err := parseWSSLocalGapRatio(value)
			if err != nil {
				return flags, err
			}
			flags.minLocalRatio = ratio
		case strings.HasPrefix(arg, "--min-local-ratio="):
			ratio, err := parseWSSLocalGapRatio(strings.TrimPrefix(arg, "--min-local-ratio="))
			if err != nil {
				return flags, err
			}
			flags.minLocalRatio = ratio
		case arg == "--min-local-saved":
			value, err := aggregateFlagValue(args, &i, arg)
			if err != nil {
				return flags, err
			}
			n, err := parseWSSLocalGapNonNegativeInt("--min-local-saved", value)
			if err != nil {
				return flags, err
			}
			flags.minLocalSaved = n
		case strings.HasPrefix(arg, "--min-local-saved="):
			n, err := parseWSSLocalGapNonNegativeInt("--min-local-saved", strings.TrimPrefix(arg, "--min-local-saved="))
			if err != nil {
				return flags, err
			}
			flags.minLocalSaved = n
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple decisions logs provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func parseWSSLocalGapRatio(value string) (float64, error) {
	ratio, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || ratio < 0 || ratio > 1 {
		return 0, fmt.Errorf("--min-local-ratio must be between 0 and 1")
	}
	return ratio, nil
}

func parseWSSLocalGapNonNegativeInt(name, value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return n, nil
}

func loadWSSLocalGapReport(flags wssLocalGapFlags) (wssLocalGapReport, error) {
	summaries, err := dbg.ReplaySession(flags.path)
	if err != nil {
		return wssLocalGapReport{}, fmt.Errorf("read decisions %s: %w", flags.path, err)
	}
	acc := wssLocalGapAccumulator{
		report: wssLocalGapReport{
			Path:       flags.path,
			GatePassed: true,
		},
		shapeRows:     make(map[string]*wssLocalGapShapeRow),
		requestGuards: make(map[string]*wssLocalGapRequestGuardRow),
		mechanismRows: make(map[string]*wssLocalGapMechanismRow),
		guardRows:     make(map[string]*wssLocalGapGuardRow),
		contentRows:   make(map[string]*wssLocalGapContentClassRow),
	}
	if !flags.since.IsZero() {
		since := flags.since
		acc.report.Since = &since
	}
	for _, summary := range summaries {
		if !flags.since.IsZero() {
			if summary.Timestamp.IsZero() || summary.Timestamp.Before(flags.since) {
				continue
			}
		}
		acc.report.Requests++
		route := wssAuditRouteMode(summary)
		if !wssAuditIsWSS(summary, route) {
			continue
		}
		acc.report.WSSRequests++
		if !wssAuditIsPhaseF(route) {
			continue
		}
		acc.addPhaseF(summary)
	}
	acc.finalize(flags)
	return acc.report, nil
}

func (a *wssLocalGapAccumulator) addPhaseF(summary dbg.RequestSummary) {
	a.report.PhaseFRequests++
	shape := wssAuditResolveRequestShape(summary).Shape
	if shape == "" {
		shape = "unknown"
	}
	original := maxInt(0, summary.Tokens.Original)
	saved := maxInt(0, summary.Tokens.Saved)
	a.report.OriginalTokens += original
	a.report.FinalTokens += maxInt(0, summary.Tokens.Final)
	a.report.LocalSavedTokens += saved
	a.report.ProviderCacheReadTokens += maxInt(0, summary.CacheReadTokens)
	a.report.ProviderCacheTokens += maxInt(0, summary.ProviderCachedTokens)
	a.report.ProviderCacheCreate += maxInt(0, summary.CacheCreateTokens)
	a.report.OutputTokens += maxInt(0, summary.OutputTokens+summary.ProviderOutputTokens)
	if saved > 0 {
		a.report.PositiveSavingsRequests++
	} else {
		a.report.ZeroSavingsRequests++
		a.report.ZeroSavingsOrigTokens += original
	}
	if len(summary.EvidenceDecisions) == 0 {
		a.report.NoEvidenceRequests++
		a.report.NoEvidenceOrigTokens += original
	}
	if len(summary.Errors) > 0 {
		a.report.ErrorRequests++
	}
	if wssAuditHasUpstreamError(summary) {
		a.report.UpstreamErrorRequests++
	}
	if wssAuditHasHTTP400Error(summary) {
		a.report.HTTP400ErrorRequests++
	}
	shapeRow := a.shapeRow(shape)
	shapeRow.Requests++
	shapeRow.OriginalTokens += original
	shapeRow.LocalSavedTokens += saved
	shapeRow.ProviderCacheTokens += maxInt(0, summary.CacheReadTokens+summary.ProviderCachedTokens)
	if saved == 0 {
		shapeRow.ZeroSavingsRequests++
		shapeRow.ZeroSavingsOrig += original
	}
	if len(summary.EvidenceDecisions) == 0 {
		shapeRow.NoEvidenceRequests++
		shapeRow.NoEvidenceOrig += original
	}
	if len(summary.Errors) > 0 {
		shapeRow.ErrorRequests++
	}
	a.addRequestGuardFacts(summary, shape, original, saved)
	for _, decision := range summary.EvidenceDecisions {
		a.addDecision(decision, shape)
	}
}

func (a *wssLocalGapAccumulator) addRequestGuardFacts(summary dbg.RequestSummary, shape string, original, saved int) {
	if a == nil {
		return
	}
	if reason := strings.TrimSpace(summary.BypassReason); reason != "" {
		a.addRequestGuard("bypass_reason="+reason, shape, original, saved)
	}
	for _, key := range []string{
		"wss.structured_mutation_guard",
		"wss.effective_mutation_guard",
		"wss.history_mutation_guard",
		"wss.downstream_state_mutation_guard",
		"wss.tool_prune_guard",
	} {
		if summary.DebugFacts == nil {
			continue
		}
		value := strings.TrimSpace(summary.DebugFacts[key])
		if value == "" {
			continue
		}
		a.addRequestGuard(key+"="+value, shape, original, saved)
	}
}

func (a *wssLocalGapAccumulator) addRequestGuard(guard, shape string, original, saved int) {
	row := a.requestGuards[guard]
	if row == nil {
		row = &wssLocalGapRequestGuardRow{Guard: guard}
		a.requestGuards[guard] = row
	}
	row.Requests++
	row.OriginalTokens += original
	row.LocalSavedTokens += saved
	if saved == 0 {
		row.ZeroSavingsRequests++
		row.ZeroSavingsOrigTokens += original
	}
	addWSSAuditCount(&row.RequestShapes, shape)
}

func (a *wssLocalGapAccumulator) addDecision(decision evidence.BlockDecision, shape string) {
	mechanism := strings.TrimSpace(decision.Mechanism)
	if mechanism == "" || mechanism == "provider_prompt_cache" {
		return
	}
	reason := strings.TrimSpace(decision.Reason)
	if reason == "" {
		reason = "(missing)"
	}
	contentClass := strings.TrimSpace(string(decision.ContentClass))
	if contentClass == "" {
		contentClass = "unknown"
	}
	original := maxInt(0, decision.OriginalTokens)
	saved := maxInt(0, decision.SavedTokens)
	net := decision.NetTokens
	guarded := 0
	if decision.Action == evidence.ActionFullPass {
		guarded = original
	}

	mechanismRow := a.mechanismRow(mechanism)
	mechanismRow.Decisions++
	mechanismRow.OriginalTokens += original
	mechanismRow.SavedTokens += saved
	mechanismRow.NetTokens += net
	mechanismRow.GuardedPotential += guarded
	addWSSAuditCount(&mechanismRow.Reasons, reason)
	addWSSAuditCount(&mechanismRow.RequestShapes, shape)
	switch decision.Action {
	case evidence.ActionApplied:
		mechanismRow.Applied++
	case evidence.ActionFullPass:
		mechanismRow.FullPass++
	case evidence.ActionSkipped:
		mechanismRow.Skipped++
	case evidence.ActionFailedOpen:
		mechanismRow.FailedOpen++
	}

	contentRow := a.contentRow(contentClass)
	contentRow.Decisions++
	contentRow.OriginalTokens += original
	contentRow.SavedTokens += saved
	contentRow.GuardedPotential += guarded
	addWSSAuditCount(&contentRow.Mechanisms, mechanism)

	if decision.Action != evidence.ActionFullPass {
		return
	}
	a.shapeRow(shape).GuardedPotential += guarded
	guardRow := a.guardRow(reason)
	guardRow.Decisions++
	guardRow.OriginalTokens += original
	guardRow.SavedTokens += saved
	guardRow.NetTokens += net
	guardRow.GuardedPotential += guarded
	addWSSAuditCount(&guardRow.Mechanisms, mechanism)
	addWSSAuditCount(&guardRow.RequestShapes, shape)
}

func (a *wssLocalGapAccumulator) shapeRow(shape string) *wssLocalGapShapeRow {
	shape = strings.TrimSpace(shape)
	if shape == "" {
		shape = "unknown"
	}
	row := a.shapeRows[shape]
	if row == nil {
		row = &wssLocalGapShapeRow{Shape: shape}
		a.shapeRows[shape] = row
	}
	return row
}

func (a *wssLocalGapAccumulator) mechanismRow(mechanism string) *wssLocalGapMechanismRow {
	row := a.mechanismRows[mechanism]
	if row == nil {
		row = &wssLocalGapMechanismRow{Mechanism: mechanism}
		a.mechanismRows[mechanism] = row
	}
	return row
}

func (a *wssLocalGapAccumulator) guardRow(reason string) *wssLocalGapGuardRow {
	row := a.guardRows[reason]
	if row == nil {
		row = &wssLocalGapGuardRow{Reason: reason}
		a.guardRows[reason] = row
	}
	return row
}

func (a *wssLocalGapAccumulator) contentRow(contentClass string) *wssLocalGapContentClassRow {
	row := a.contentRows[contentClass]
	if row == nil {
		row = &wssLocalGapContentClassRow{ContentClass: contentClass}
		a.contentRows[contentClass] = row
	}
	return row
}

func (a *wssLocalGapAccumulator) finalize(flags wssLocalGapFlags) {
	targetRatio := flags.minLocalRatio
	if targetRatio == 0 {
		targetRatio = 0.48
	}
	a.report.TargetRatio = targetRatio
	a.report.TargetSavedTokens = targetSavedTokens(a.report.OriginalTokens, targetRatio)
	a.report.TargetDeficitTokens = maxInt(0, a.report.TargetSavedTokens-a.report.LocalSavedTokens)
	a.report.LocalSavingsRatio = wssLocalGapRatio(a.report.LocalSavedTokens, a.report.OriginalTokens)
	a.report.RequestShapes = finalizeWSSLocalGapShapes(a.shapeRows, targetRatio)
	a.report.RequestGuards = finalizeWSSLocalGapRequestGuards(a.requestGuards)
	a.report.Mechanisms = finalizeWSSLocalGapMechanisms(a.mechanismRows)
	a.report.Guards = finalizeWSSLocalGapGuards(a.guardRows)
	a.report.ContentClasses = finalizeWSSLocalGapContentClasses(a.contentRows)
	a.report.Notes = wssLocalGapNotes(a.report)
	a.report.GateFailures = wssLocalGapGateFailures(a.report, flags)
	a.report.GatePassed = len(a.report.GateFailures) == 0
}

func finalizeWSSLocalGapShapes(rows map[string]*wssLocalGapShapeRow, targetRatio float64) []wssLocalGapShapeRow {
	out := make([]wssLocalGapShapeRow, 0, len(rows))
	for _, row := range rows {
		copy := *row
		copy.LocalSavingsRatio = wssLocalGapRatio(copy.LocalSavedTokens, copy.OriginalTokens)
		copy.TargetDeficitTokens = maxInt(0, targetSavedTokens(copy.OriginalTokens, targetRatio)-copy.LocalSavedTokens)
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TargetDeficitTokens != out[j].TargetDeficitTokens {
			return out[i].TargetDeficitTokens > out[j].TargetDeficitTokens
		}
		return out[i].Shape < out[j].Shape
	})
	return out
}

func finalizeWSSLocalGapRequestGuards(rows map[string]*wssLocalGapRequestGuardRow) []wssLocalGapRequestGuardRow {
	out := make([]wssLocalGapRequestGuardRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OriginalTokens != out[j].OriginalTokens {
			return out[i].OriginalTokens > out[j].OriginalTokens
		}
		return out[i].Guard < out[j].Guard
	})
	return out
}

func finalizeWSSLocalGapMechanisms(rows map[string]*wssLocalGapMechanismRow) []wssLocalGapMechanismRow {
	out := make([]wssLocalGapMechanismRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GuardedPotential != out[j].GuardedPotential {
			return out[i].GuardedPotential > out[j].GuardedPotential
		}
		if out[i].SavedTokens != out[j].SavedTokens {
			return out[i].SavedTokens > out[j].SavedTokens
		}
		return out[i].Mechanism < out[j].Mechanism
	})
	return out
}

func finalizeWSSLocalGapGuards(rows map[string]*wssLocalGapGuardRow) []wssLocalGapGuardRow {
	out := make([]wssLocalGapGuardRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GuardedPotential != out[j].GuardedPotential {
			return out[i].GuardedPotential > out[j].GuardedPotential
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func finalizeWSSLocalGapContentClasses(rows map[string]*wssLocalGapContentClassRow) []wssLocalGapContentClassRow {
	out := make([]wssLocalGapContentClassRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GuardedPotential != out[j].GuardedPotential {
			return out[i].GuardedPotential > out[j].GuardedPotential
		}
		if out[i].OriginalTokens != out[j].OriginalTokens {
			return out[i].OriginalTokens > out[j].OriginalTokens
		}
		return out[i].ContentClass < out[j].ContentClass
	})
	return out
}

func wssLocalGapRatio(saved, original int) float64 {
	if original <= 0 || saved <= 0 {
		return 0
	}
	return float64(saved) / float64(original)
}

func targetSavedTokens(original int, ratio float64) int {
	if original <= 0 || ratio <= 0 {
		return 0
	}
	return int(math.Ceil(float64(original) * ratio))
}

func wssLocalGapNotes(report wssLocalGapReport) []string {
	var notes []string
	if report.PhaseFRequests == 0 {
		notes = append(notes, "No WSS Phase-F rows found; cannot evaluate S_local for the product WSS path.")
	}
	if report.ProviderCacheReadTokens > 0 || report.ProviderCacheTokens > 0 {
		notes = append(notes, "Provider-cache tokens are present but excluded from S_local by AGENTS.md 3.2.")
	}
	if report.LocalSavingsRatio < 0.48 && report.OriginalTokens > 0 {
		notes = append(notes, "S_local is below the owner target; prioritize the highest guarded_potential_tokens rows before widening any guard.")
	}
	if report.NoEvidenceOrigTokens > 0 {
		notes = append(notes, "Some WSS Phase-F token mass has no evidence decisions; add instrumentation before treating the remaining gap as a known guard problem.")
	}
	if len(report.Guards) == 0 && report.PhaseFRequests > 0 {
		notes = append(notes, "No full-pass evidence decisions found; remaining gap may be uninstrumented or outside Layer-0 evidence.")
	}
	if report.HTTP400ErrorRequests > 0 {
		notes = append(notes, "HTTP 400 rows are present; treat overlapping guard rows as real safety boundaries until fresh proof says otherwise.")
	}
	return notes
}

func wssLocalGapGateFailures(report wssLocalGapReport, flags wssLocalGapFlags) []string {
	var failures []string
	if flags.minLocalRatio > 0 {
		if report.OriginalTokens == 0 {
			failures = append(failures, "original_tokens=0; cannot prove S_local ratio")
		} else if report.LocalSavingsRatio+1e-9 < flags.minLocalRatio {
			failures = append(failures, fmt.Sprintf("local_savings_ratio=%.6f < min=%.6f", report.LocalSavingsRatio, flags.minLocalRatio))
		}
	}
	if flags.minLocalSaved > 0 && report.LocalSavedTokens < flags.minLocalSaved {
		failures = append(failures, fmt.Sprintf("local_saved_tokens=%d < min=%d", report.LocalSavedTokens, flags.minLocalSaved))
	}
	return failures
}

func writeWSSLocalGapText(w io.Writer, report wssLocalGapReport) {
	fmt.Fprintf(w, "=== WSS Local Gap: %s ===\n", filepath.Base(report.Path))
	if report.Since != nil {
		fmt.Fprintf(w, "Since:                    %s\n", report.Since.Format(time.RFC3339))
	}
	fmt.Fprintf(w, "Requests analyzed:         %d\n", report.Requests)
	fmt.Fprintf(w, "WSS / Phase-F requests:    %d / %d\n", report.WSSRequests, report.PhaseFRequests)
	fmt.Fprintf(w, "Local original/final:      %d / %d\n", report.OriginalTokens, report.FinalTokens)
	fmt.Fprintf(w, "S_local saved/ratio:       %d / %.2f%%\n", report.LocalSavedTokens, report.LocalSavingsRatio*100)
	fmt.Fprintf(w, "Target saved/deficit:      %d / %d at %.2f%%\n", report.TargetSavedTokens, report.TargetDeficitTokens, report.TargetRatio*100)
	fmt.Fprintf(w, "Provider cache read/cached/create: %d / %d / %d\n",
		report.ProviderCacheReadTokens,
		report.ProviderCacheTokens,
		report.ProviderCacheCreate)
	fmt.Fprintf(w, "Positive/zero savings reqs:%d / %d\n", report.PositiveSavingsRequests, report.ZeroSavingsRequests)
	fmt.Fprintf(w, "Zero/no-evidence orig:     %d / %d\n", report.ZeroSavingsOrigTokens, report.NoEvidenceOrigTokens)
	fmt.Fprintf(w, "Errors/upstream/400:       %d / %d / %d\n",
		report.ErrorRequests,
		report.UpstreamErrorRequests,
		report.HTTP400ErrorRequests)
	fmt.Fprintf(w, "gate:                      %s\n", passFail(report.GatePassed))
	if len(report.RequestShapes) > 0 {
		fmt.Fprintln(w, "\nRequest shapes:")
		for _, row := range report.RequestShapes {
			fmt.Fprintf(w, "  %-12s requests=%d local=%d/%d %.2f%% deficit=%d zero_orig=%d no_evidence_orig=%d provider_cache=%d guarded_potential=%d errors=%d\n",
				row.Shape,
				row.Requests,
				row.LocalSavedTokens,
				row.OriginalTokens,
				row.LocalSavingsRatio*100,
				row.TargetDeficitTokens,
				row.ZeroSavingsOrig,
				row.NoEvidenceOrig,
				row.ProviderCacheTokens,
				row.GuardedPotential,
				row.ErrorRequests)
		}
	}
	if len(report.RequestGuards) > 0 {
		fmt.Fprintln(w, "\nRequest-level guards:")
		for _, row := range report.RequestGuards {
			fmt.Fprintf(w, "  %-72s requests=%d original=%d saved=%d zero_orig=%d shapes=%s\n",
				row.Guard,
				row.Requests,
				row.OriginalTokens,
				row.LocalSavedTokens,
				row.ZeroSavingsOrigTokens,
				formatWSSAuditCounts(row.RequestShapes))
		}
	}
	if len(report.Guards) > 0 {
		fmt.Fprintln(w, "\nGuarded potential by reason:")
		for _, row := range report.Guards {
			fmt.Fprintf(w, "  %-44s decisions=%d potential=%d saved=%d net=%d mechanisms=%s shapes=%s\n",
				row.Reason,
				row.Decisions,
				row.GuardedPotential,
				row.SavedTokens,
				row.NetTokens,
				formatWSSAuditCounts(row.Mechanisms),
				formatWSSAuditCounts(row.RequestShapes))
		}
	}
	if len(report.Mechanisms) > 0 {
		fmt.Fprintln(w, "\nMechanisms:")
		for _, row := range report.Mechanisms {
			fmt.Fprintf(w, "  %-22s decisions=%d applied=%d full_pass=%d skipped=%d failed_open=%d potential=%d saved=%d net=%d reasons=%s shapes=%s\n",
				row.Mechanism,
				row.Decisions,
				row.Applied,
				row.FullPass,
				row.Skipped,
				row.FailedOpen,
				row.GuardedPotential,
				row.SavedTokens,
				row.NetTokens,
				formatWSSAuditCounts(row.Reasons),
				formatWSSAuditCounts(row.RequestShapes))
		}
	}
	if len(report.ContentClasses) > 0 {
		fmt.Fprintln(w, "\nContent classes:")
		for _, row := range report.ContentClasses {
			fmt.Fprintf(w, "  %-14s decisions=%d original=%d saved=%d potential=%d mechanisms=%s\n",
				row.ContentClass,
				row.Decisions,
				row.OriginalTokens,
				row.SavedTokens,
				row.GuardedPotential,
				formatWSSAuditCounts(row.Mechanisms))
		}
	}
	if len(report.GateFailures) > 0 {
		fmt.Fprintln(w, "\nGate failures:")
		for _, failure := range report.GateFailures {
			fmt.Fprintf(w, "  - %s\n", failure)
		}
	}
	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
}

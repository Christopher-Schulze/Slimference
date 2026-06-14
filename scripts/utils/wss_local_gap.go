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
	Path                     string                       `json:"path"`
	Since                    *time.Time                   `json:"since,omitempty"`
	Requests                 int                          `json:"requests"`
	WSSRequests              int                          `json:"wss_requests"`
	PhaseFRequests           int                          `json:"phasef_requests"`
	OriginalTokens           int                          `json:"original_tokens"`
	FinalTokens              int                          `json:"final_tokens"`
	LocalSavedTokens         int                          `json:"local_saved_tokens"`
	LocalSavingsRatio        float64                      `json:"local_savings_ratio"`
	ProviderCacheReadTokens  int                          `json:"provider_cache_read_tokens"`
	ProviderCacheTokens      int                          `json:"provider_cache_tokens"`
	ProviderCacheCreate      int                          `json:"provider_cache_create_tokens"`
	OutputTokens             int                          `json:"output_tokens"`
	TargetRatio              float64                      `json:"target_ratio"`
	TargetSavedTokens        int                          `json:"target_saved_tokens"`
	TargetDeficitTokens      int                          `json:"target_deficit_tokens"`
	PolicySavingsCeiling     int                          `json:"policy_savings_ceiling_tokens"`
	PolicySavingsCeilingRate float64                      `json:"policy_savings_ceiling_ratio"`
	PolicyCeilingDeficit     int                          `json:"policy_savings_ceiling_deficit_tokens,omitempty"`
	PositiveSavingsRequests  int                          `json:"positive_savings_requests"`
	PositiveSavingsOrig      int                          `json:"positive_savings_original_tokens"`
	PositiveSavingsRatio     float64                      `json:"positive_savings_local_ratio"`
	ZeroSavingsRequests      int                          `json:"zero_savings_requests"`
	ZeroSavingsOrigTokens    int                          `json:"zero_savings_original_tokens"`
	NoEvidenceRequests       int                          `json:"no_evidence_requests"`
	NoEvidenceOrigTokens     int                          `json:"no_evidence_original_tokens"`
	NoEvidenceNeedsInstr     int                          `json:"no_evidence_needs_instrumentation_original_tokens,omitempty"`
	NoEvidenceProtected      int                          `json:"no_evidence_protected_original_tokens,omitempty"`
	NoEvidenceKnownNonTarget int                          `json:"no_evidence_known_non_target_original_tokens,omitempty"`
	NoEvidenceProofBlocked   int                          `json:"no_evidence_proof_blocked_or_candidate_original_tokens,omitempty"`
	ErrorRequests            int                          `json:"error_requests"`
	UpstreamErrorRequests    int                          `json:"upstream_error_requests"`
	HTTP400ErrorRequests     int                          `json:"http_400_error_requests"`
	RequestShapeSources      map[string]int               `json:"request_shape_sources,omitempty"`
	RequestShapes            []wssLocalGapShapeRow        `json:"request_shapes,omitempty"`
	RequestGuards            []wssLocalGapRequestGuardRow `json:"request_guards,omitempty"`
	Mechanisms               []wssLocalGapMechanismRow    `json:"mechanisms,omitempty"`
	Guards                   []wssLocalGapGuardRow        `json:"guards,omitempty"`
	ContentClasses           []wssLocalGapContentClassRow `json:"content_classes,omitempty"`
	ActionablePotential      []wssLocalGapActionableRow   `json:"actionable_potential,omitempty"`
	GatePassed               bool                         `json:"gate_passed"`
	GateFailures             []string                     `json:"gate_failures,omitempty"`
	Notes                    []string                     `json:"notes,omitempty"`
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
	NoEvidenceRequests    int            `json:"no_evidence_requests"`
	NoEvidenceOrigTokens  int            `json:"no_evidence_original_tokens"`
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

type wssLocalGapActionableRow struct {
	Category                         string         `json:"category"`
	Source                           string         `json:"source"`
	TokenBasis                       string         `json:"token_basis"`
	Tokens                           int            `json:"tokens"`
	LocalSavedTokens                 int            `json:"local_saved_tokens,omitempty"`
	Requests                         int            `json:"requests,omitempty"`
	Decisions                        int            `json:"decisions,omitempty"`
	OutputReduceInputTokens          int            `json:"output_reduce_input_tokens,omitempty"`
	OutputReduceEligibleInputTokens  int            `json:"output_reduce_eligible_input_tokens,omitempty"`
	PrefixTotalBytes                 int            `json:"prefix_total_bytes,omitempty"`
	PrefixEstimatedTokens            int            `json:"prefix_estimated_tokens,omitempty"`
	PrefixToolDefinitionBytes        int            `json:"prefix_tool_definition_bytes,omitempty"`
	PrefixInstructionBytes           int            `json:"prefix_instruction_bytes,omitempty"`
	PrefixToolNameBytes              int            `json:"prefix_tool_name_bytes,omitempty"`
	PrefixToolDescriptionBytes       int            `json:"prefix_tool_description_bytes,omitempty"`
	PrefixToolParametersBytes        int            `json:"prefix_tool_parameters_bytes,omitempty"`
	PrefixToolOtherBytes             int            `json:"prefix_tool_other_bytes,omitempty"`
	PrefixToolDefinitions            int            `json:"prefix_tool_definitions,omitempty"`
	PrefixMaxToolDefinitions         int            `json:"prefix_max_tool_definitions,omitempty"`
	PrefixDefaultKeepTools           int            `json:"prefix_default_keep_tools,omitempty"`
	PrefixDefaultKeepBytes           int            `json:"prefix_default_keep_bytes,omitempty"`
	PrefixDefaultDescriptionBytes    int            `json:"prefix_default_description_bytes,omitempty"`
	PrefixDefaultParametersBytes     int            `json:"prefix_default_parameters_bytes,omitempty"`
	PrefixDefaultKeepNames           map[string]int `json:"prefix_default_keep_tool_names,omitempty"`
	PrefixNonDefaultTools            int            `json:"prefix_nondefault_tools,omitempty"`
	PrefixNonDefaultBytes            int            `json:"prefix_nondefault_bytes,omitempty"`
	PrefixNonDefaultDescriptionBytes int            `json:"prefix_nondefault_description_bytes,omitempty"`
	PrefixNonDefaultParametersBytes  int            `json:"prefix_nondefault_parameters_bytes,omitempty"`
	PrefixNonDefaultNames            map[string]int `json:"prefix_nondefault_tool_names,omitempty"`
	PrefixUnnamedTools               int            `json:"prefix_unnamed_tools,omitempty"`
	PrefixUnnamedBytes               int            `json:"prefix_unnamed_bytes,omitempty"`
	PrefixControlContextBytes        int            `json:"prefix_control_context_bytes,omitempty"`
	PrefixNonDefaultCandidateBytes   int            `json:"prefix_nondefault_candidate_bytes,omitempty"`
	PrefixUnclassifiedToolBytes      int            `json:"prefix_unclassified_tool_bytes,omitempty"`
	Policy                           string         `json:"policy"`
	NextStep                         string         `json:"next_step"`
	RequestShapes                    map[string]int `json:"request_shapes,omitempty"`
	Mechanisms                       map[string]int `json:"mechanisms,omitempty"`
	ToolCommandClasses               map[string]int `json:"tool_command_classes,omitempty"`
}

type wssLocalGapAccumulator struct {
	report        wssLocalGapReport
	shapeRows     map[string]*wssLocalGapShapeRow
	requestGuards map[string]*wssLocalGapRequestGuardRow
	mechanismRows map[string]*wssLocalGapMechanismRow
	guardRows     map[string]*wssLocalGapGuardRow
	contentRows   map[string]*wssLocalGapContentClassRow
	actionRows    map[string]*wssLocalGapActionableRow
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
Policy savings ceiling subtracts protected capability-context and known
non-target no-evidence mass from original tokens; it is an upper bound, not a
claim that the remaining mass is actually recoverable. Guarded potential is the
original-token mass carried by evidence decisions whose action is full_pass; it
is an opportunity ledger, not a claim that all tokens are safely recoverable.
Actionable potential separates known guard work from no-evidence instrumentation
gaps and safety guards; rows are diagnostic and not additive unless they share
the same token_basis.`

const (
	wssLocalGapSourceContextMinBytes  = 4096
	wssLocalGapTinyToolOutputMaxBytes = 255
)

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
			Path:                flags.path,
			RequestShapeSources: make(map[string]int),
			GatePassed:          true,
		},
		shapeRows:     make(map[string]*wssLocalGapShapeRow),
		requestGuards: make(map[string]*wssLocalGapRequestGuardRow),
		mechanismRows: make(map[string]*wssLocalGapMechanismRow),
		guardRows:     make(map[string]*wssLocalGapGuardRow),
		contentRows:   make(map[string]*wssLocalGapContentClassRow),
		actionRows:    make(map[string]*wssLocalGapActionableRow),
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
	shapeResolution := wssAuditResolveRequestShape(summary)
	shape := shapeResolution.Shape
	if shape == "" {
		shape = "unknown"
	}
	addWSSAuditCount(&a.report.RequestShapeSources, shapeResolution.Source)
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
		a.report.PositiveSavingsOrig += original
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
	noEvidence := len(summary.EvidenceDecisions) == 0
	toolCommandClasses := wssLocalGapFactCountPairs(summary.DebugFacts, "wss.tool_command_classes")
	a.addRequestGuardFacts(summary, shape, shapeResolution.Source, original, saved, noEvidence)
	if noEvidence {
		a.addNoEvidenceActionable(summary, shape, shapeResolution.Source, original, saved, toolCommandClasses)
	}
	for _, decision := range summary.EvidenceDecisions {
		a.addDecision(decision, shape, toolCommandClasses, summary.DebugFacts)
	}
}

func (a *wssLocalGapAccumulator) addRequestGuardFacts(summary dbg.RequestSummary, shape, shapeSource string, original, saved int, noEvidence bool) {
	if a == nil {
		return
	}
	if noEvidence &&
		strings.TrimSpace(summary.DebugFacts["wss.request_shape"]) == "" &&
		strings.TrimSpace(shapeSource) == "unresolved" {
		a.addRequestGuard("wss.request_shape=(missing)", shape, original, saved, noEvidence)
	}
	if reason := strings.TrimSpace(summary.BypassReason); reason != "" {
		a.addRequestGuard("bypass_reason="+reason, shape, original, saved, noEvidence)
	}
	if noEvidence {
		for _, key := range []string{
			"wss.output_reduce_reason",
			"wss.output_reduce_disabled_predicate",
			"wss.messages",
			"wss.tool_results",
			"wss.tool_result_bytes",
			"wss.tool_result_output_bytes",
			"wss.source_tool_bytes",
		} {
			if summary.DebugFacts == nil {
				continue
			}
			value := strings.TrimSpace(summary.DebugFacts[key])
			if value == "" {
				continue
			}
			a.addRequestGuard("no_evidence:"+key+"="+value, shape, original, saved, true)
		}
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
		a.addRequestGuard(key+"="+value, shape, original, saved, noEvidence)
	}
}

func (a *wssLocalGapAccumulator) addRequestGuard(guard, shape string, original, saved int, noEvidence bool) {
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
	if noEvidence {
		row.NoEvidenceRequests++
		row.NoEvidenceOrigTokens += original
	}
	addWSSAuditCount(&row.RequestShapes, shape)
}

func (a *wssLocalGapAccumulator) addDecision(decision evidence.BlockDecision, shape string, toolCommandClasses map[string]int, facts map[string]string) {
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
	a.addDecisionActionable(reason, mechanism, shape, guarded, saved, wssLocalGapDecisionCommandClasses(decision, toolCommandClasses), facts)
}

func wssLocalGapDecisionCommandClasses(decision evidence.BlockDecision, requestClasses map[string]int) map[string]int {
	class := strings.TrimSpace(decision.CommandClass)
	if class == "" {
		return requestClasses
	}
	return map[string]int{class: 1}
}

func (a *wssLocalGapAccumulator) addDecisionActionable(reason, mechanism, shape string, tokens, saved int, toolCommandClasses map[string]int, facts map[string]string) {
	if a == nil || tokens <= 0 {
		return
	}
	category, source, policy, nextStep := wssLocalGapDecisionActionForFacts(reason, facts)
	a.addActionable(wssLocalGapActionableRow{
		Category:           category,
		Source:             source,
		TokenBasis:         "full_pass_block_original_tokens",
		Tokens:             tokens,
		LocalSavedTokens:   saved,
		Decisions:          1,
		Policy:             policy,
		NextStep:           nextStep,
		ToolCommandClasses: toolCommandClasses,
	}, shape, mechanism)
}

func (a *wssLocalGapAccumulator) addNoEvidenceActionable(summary dbg.RequestSummary, shape, shapeSource string, original, saved int, toolCommandClasses map[string]int) {
	if a == nil || original <= 0 {
		return
	}
	category, source, policy, nextStep := wssLocalGapNoEvidenceAction(summary, shapeSource)
	a.addNoEvidenceClassification(category, original)
	prefixControlContextBytes, prefixNonDefaultCandidateBytes, prefixUnclassifiedToolBytes := wssLocalGapPrefixDecisionSurface(summary.DebugFacts)
	a.addActionable(wssLocalGapActionableRow{
		Category:                         category,
		Source:                           source,
		TokenBasis:                       "request_original_tokens",
		Tokens:                           original,
		LocalSavedTokens:                 saved,
		Requests:                         1,
		OutputReduceInputTokens:          wssLocalGapFactInt(summary.DebugFacts, "wss.output_reduce_input_tokens"),
		OutputReduceEligibleInputTokens:  wssLocalGapFactInt(summary.DebugFacts, "wss.output_reduce_eligible_input_tokens"),
		PrefixTotalBytes:                 wssLocalGapFactInt(summary.DebugFacts, "wss.prefix_total_bytes"),
		PrefixEstimatedTokens:            wssLocalGapFactInt(summary.DebugFacts, "wss.prefix_estimated_tokens"),
		PrefixToolDefinitionBytes:        wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_bytes"),
		PrefixInstructionBytes:           wssLocalGapFactInt(summary.DebugFacts, "wss.instructions_bytes"),
		PrefixToolNameBytes:              wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_name_bytes"),
		PrefixToolDescriptionBytes:       wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_description_bytes"),
		PrefixToolParametersBytes:        wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_parameters_bytes"),
		PrefixToolOtherBytes:             wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_other_bytes"),
		PrefixToolDefinitions:            wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definitions"),
		PrefixMaxToolDefinitions:         wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definitions"),
		PrefixDefaultKeepTools:           wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_default_keep"),
		PrefixDefaultKeepBytes:           wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_default_keep_bytes"),
		PrefixDefaultDescriptionBytes:    wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_default_keep_description_bytes"),
		PrefixDefaultParametersBytes:     wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_default_keep_parameters_bytes"),
		PrefixDefaultKeepNames:           wssLocalGapFactListCounts(summary.DebugFacts, "wss.tool_definition_default_keep_names"),
		PrefixNonDefaultTools:            wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_nondefault"),
		PrefixNonDefaultBytes:            wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_nondefault_bytes"),
		PrefixNonDefaultDescriptionBytes: wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_nondefault_description_bytes"),
		PrefixNonDefaultParametersBytes:  wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_nondefault_parameters_bytes"),
		PrefixNonDefaultNames:            wssLocalGapFactListCounts(summary.DebugFacts, "wss.tool_definition_nondefault_names"),
		PrefixUnnamedTools:               wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_unnamed"),
		PrefixUnnamedBytes:               wssLocalGapFactInt(summary.DebugFacts, "wss.tool_definition_unnamed_bytes"),
		PrefixControlContextBytes:        prefixControlContextBytes,
		PrefixNonDefaultCandidateBytes:   prefixNonDefaultCandidateBytes,
		PrefixUnclassifiedToolBytes:      prefixUnclassifiedToolBytes,
		Policy:                           policy,
		NextStep:                         nextStep,
		ToolCommandClasses:               toolCommandClasses,
	}, shape, "")
}

func (a *wssLocalGapAccumulator) addNoEvidenceClassification(category string, original int) {
	if a == nil || original <= 0 {
		return
	}
	switch wssLocalGapNoEvidenceClassification(category) {
	case "needs_instrumentation":
		a.report.NoEvidenceNeedsInstr += original
	case "protected":
		a.report.NoEvidenceProtected += original
	case "known_non_target":
		a.report.NoEvidenceKnownNonTarget += original
	default:
		a.report.NoEvidenceProofBlocked += original
	}
}

func wssLocalGapNoEvidenceClassification(category string) string {
	switch strings.TrimSpace(category) {
	case "needs_instrumentation":
		return "needs_instrumentation"
	case "prefix_capability_context_guarded", "source_context_guard", "context_fidelity_guard":
		return "protected"
	case "not_tool_output_reducer_target", "small_tool_output_context", "empty_tool_output_context", "not_output_reduce_target", "prefix_bound_tool_output_context", "disabled_by_configuration":
		return "known_non_target"
	default:
		return "proof_blocked_or_candidate"
	}
}

func (a *wssLocalGapAccumulator) addActionable(row wssLocalGapActionableRow, shape, mechanism string) {
	if a == nil || row.Tokens <= 0 {
		return
	}
	key := row.Category + "\x00" + row.Source + "\x00" + row.TokenBasis
	existing := a.actionRows[key]
	if existing == nil {
		copy := row
		a.actionRows[key] = &copy
		existing = &copy
	} else {
		existing.Tokens += row.Tokens
		existing.LocalSavedTokens += row.LocalSavedTokens
		existing.Requests += row.Requests
		existing.Decisions += row.Decisions
		existing.OutputReduceInputTokens += row.OutputReduceInputTokens
		existing.OutputReduceEligibleInputTokens += row.OutputReduceEligibleInputTokens
		existing.PrefixTotalBytes += row.PrefixTotalBytes
		existing.PrefixEstimatedTokens += row.PrefixEstimatedTokens
		existing.PrefixToolDefinitionBytes += row.PrefixToolDefinitionBytes
		existing.PrefixInstructionBytes += row.PrefixInstructionBytes
		existing.PrefixToolNameBytes += row.PrefixToolNameBytes
		existing.PrefixToolDescriptionBytes += row.PrefixToolDescriptionBytes
		existing.PrefixToolParametersBytes += row.PrefixToolParametersBytes
		existing.PrefixToolOtherBytes += row.PrefixToolOtherBytes
		existing.PrefixToolDefinitions += row.PrefixToolDefinitions
		if row.PrefixMaxToolDefinitions > existing.PrefixMaxToolDefinitions {
			existing.PrefixMaxToolDefinitions = row.PrefixMaxToolDefinitions
		}
		existing.PrefixDefaultKeepTools += row.PrefixDefaultKeepTools
		existing.PrefixDefaultKeepBytes += row.PrefixDefaultKeepBytes
		existing.PrefixDefaultDescriptionBytes += row.PrefixDefaultDescriptionBytes
		existing.PrefixDefaultParametersBytes += row.PrefixDefaultParametersBytes
		mergeWSSLocalGapCounts(&existing.PrefixDefaultKeepNames, row.PrefixDefaultKeepNames)
		existing.PrefixNonDefaultTools += row.PrefixNonDefaultTools
		existing.PrefixNonDefaultBytes += row.PrefixNonDefaultBytes
		existing.PrefixNonDefaultDescriptionBytes += row.PrefixNonDefaultDescriptionBytes
		existing.PrefixNonDefaultParametersBytes += row.PrefixNonDefaultParametersBytes
		mergeWSSLocalGapCounts(&existing.PrefixNonDefaultNames, row.PrefixNonDefaultNames)
		existing.PrefixUnnamedTools += row.PrefixUnnamedTools
		existing.PrefixUnnamedBytes += row.PrefixUnnamedBytes
		existing.PrefixControlContextBytes += row.PrefixControlContextBytes
		existing.PrefixNonDefaultCandidateBytes += row.PrefixNonDefaultCandidateBytes
		existing.PrefixUnclassifiedToolBytes += row.PrefixUnclassifiedToolBytes
		mergeWSSLocalGapCounts(&existing.ToolCommandClasses, row.ToolCommandClasses)
	}
	addWSSAuditCount(&existing.RequestShapes, shape)
	if strings.TrimSpace(mechanism) != "" {
		addWSSAuditCount(&existing.Mechanisms, mechanism)
	}
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
	a.report.PositiveSavingsRatio = wssLocalGapRatio(a.report.LocalSavedTokens, a.report.PositiveSavingsOrig)
	a.report.PolicySavingsCeiling = wssLocalGapPolicySavingsCeiling(a.report)
	a.report.PolicySavingsCeilingRate = wssLocalGapRatio(a.report.PolicySavingsCeiling, a.report.OriginalTokens)
	a.report.PolicyCeilingDeficit = maxInt(0, a.report.TargetSavedTokens-a.report.PolicySavingsCeiling)
	a.report.RequestShapes = finalizeWSSLocalGapShapes(a.shapeRows, targetRatio)
	a.report.RequestGuards = finalizeWSSLocalGapRequestGuards(a.requestGuards)
	a.report.Mechanisms = finalizeWSSLocalGapMechanisms(a.mechanismRows)
	a.report.Guards = finalizeWSSLocalGapGuards(a.guardRows)
	a.report.ContentClasses = finalizeWSSLocalGapContentClasses(a.contentRows)
	a.report.ActionablePotential = finalizeWSSLocalGapActionable(a.actionRows)
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

func finalizeWSSLocalGapActionable(rows map[string]*wssLocalGapActionableRow) []wssLocalGapActionableRow {
	out := make([]wssLocalGapActionableRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Source < out[j].Source
	})
	return out
}

func wssLocalGapPolicySavingsCeiling(report wssLocalGapReport) int {
	blocked := maxInt(0, report.NoEvidenceProtected) + maxInt(0, report.NoEvidenceKnownNonTarget)
	return maxInt(0, report.OriginalTokens-blocked)
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
		if wssLocalGapTotalGuardedPotential(report) > 0 {
			notes = append(notes, "S_local is below the owner target; prioritize the highest guarded_potential_tokens rows before widening any guard.")
		} else {
			notes = append(notes, "S_local is below the owner target, but no full-pass evidence potential is present; inspect actionable/no-evidence rows and active positive-savings ratio before changing guards.")
		}
	}
	if report.PolicyCeilingDeficit > 0 && report.OriginalTokens > 0 {
		notes = append(notes, fmt.Sprintf("Policy savings ceiling is %.2f%% under current protected/known-non-target classification; even perfect non-protected reducers still miss the %.2f%% target by %d tokens.", report.PolicySavingsCeilingRate*100, report.TargetRatio*100, report.PolicyCeilingDeficit))
	}
	if report.NoEvidenceNeedsInstr > 0 {
		notes = append(notes, fmt.Sprintf("Some WSS Phase-F no-evidence mass still needs instrumentation (%d original tokens); instrument it before changing guards.", report.NoEvidenceNeedsInstr))
	} else if report.NoEvidenceOrigTokens > 0 {
		notes = append(notes, fmt.Sprintf("WSS Phase-F no-evidence mass is classified by content-free facts: protected=%d known_non_target=%d proof_blocked_or_candidate=%d; do not treat it as a generic instrumentation gap.", report.NoEvidenceProtected, report.NoEvidenceKnownNonTarget, report.NoEvidenceProofBlocked))
	}
	if len(report.ActionablePotential) > 0 {
		notes = append(notes, "Actionable-potential rows classify the next proof/engineering move; they are diagnostic and not a promise that guarded tokens are safely recoverable.")
	}
	if wssLocalGapHasActionableCategory(report.ActionablePotential, "prefix_capability_context_guarded") {
		notes = append(notes, "Capability-prefix rows quantify protected model-facing context, not a safe product-savings candidate.")
	}
	if wssLocalGapHasPrefixDecisionSurface(report.ActionablePotential) {
		notes = append(notes, "Prefix decision-surface bytes split protected instructions/default tools from nondefault proof candidates; this is diagnostic and does not authorize prefix mutation.")
	}
	if len(report.Guards) == 0 && report.PhaseFRequests > 0 {
		if report.NoEvidenceNeedsInstr > 0 || report.NoEvidenceOrigTokens == 0 {
			notes = append(notes, "No full-pass evidence decisions found; remaining gap may be uninstrumented or outside Layer-0 evidence.")
		} else {
			notes = append(notes, "No full-pass evidence decisions found; remaining gap is classified no-evidence mass outside the currently safe Layer-0 reducer surface.")
		}
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

func wssLocalGapHasActionableCategory(rows []wssLocalGapActionableRow, category string) bool {
	for _, row := range rows {
		if row.Category == category {
			return true
		}
	}
	return false
}

func wssLocalGapHasPrefixDecisionSurface(rows []wssLocalGapActionableRow) bool {
	for _, row := range rows {
		if row.PrefixControlContextBytes > 0 || row.PrefixNonDefaultCandidateBytes > 0 || row.PrefixUnclassifiedToolBytes > 0 {
			return true
		}
	}
	return false
}

func wssLocalGapTotalGuardedPotential(report wssLocalGapReport) int {
	total := 0
	for _, row := range report.Guards {
		total += row.GuardedPotential
	}
	return total
}

func wssLocalGapDecisionAction(reason string) (string, string, string) {
	switch strings.TrimSpace(reason) {
	case "wss_search_output_risk_gate":
		return "proof_latch_candidate",
			"allow only through final search-cap proof latch for the exact command/envelope shape",
			"verify the active proof covers this mechanism and command shape, then narrow the guard only for that proofed shape"
	case "wss_stateful_structured_mutation_guard":
		return "stateful_safe_parser_candidate",
			"do not broadly mutate stateful tool output; add exact parser/size/command guards per class",
			"build one deterministic parser class with live A/B before allowing savings"
	case "wss_stateful_delta_mutation_proof_gate":
		return "unsafe_without_fresh_live_proof",
			"previous_response_id delta mutation has known downstream 400 risk",
			"keep full-pass unless a fresh downstream-delta-safe live proof covers this exact mechanism"
	case "wss_source_tool_output_full_pass":
		return "source_context_guard",
			"source-like previous_response_id delta tool output is model-facing repository context",
			"replace only with an exact archive/state mirror plus downstream-delta stability proof for this source lineage"
	case "wss_full_history_downstream_delta_proof_gate":
		return "unsafe_without_fresh_live_proof",
			"full-history reconnect/downstream mutation can poison the following delta turn",
			"keep guarded unless lineage/stateless continuation proof covers this exact path"
	case "wss_recovery_history_mutation_guard":
		return "unsafe_without_fresh_live_proof",
			"recovery lineage was already damaged once; further history mutation needs recovery-specific proof",
			"keep lineage guard unless fresh recovery replay/live proof proves clean continuation"
	case "wss_tool_prune_delta_guard":
		return "unsafe_without_fresh_live_proof",
			"delta tool-schema pruning needs reattach and downstream safety proof",
			"prove reattach plus following-turn stability before enabling"
	case "cache_bust_guard":
		return "cache_stability_guard",
			"provider-cache drop was attributed to this mechanism",
			"tighten by route/shape/prefix hash only after cache-stability telemetry proves the narrower cause"
	case "session_integrity_budget":
		return "resource_budget_guard",
			"session reference budget protects recovery and recency density",
			"raise or reshape only with resource and recovery proof"
	case "latency_budget_full_context":
		return "resource_budget_guard",
			"hotpath latency budget blocked non-essential work",
			"optimize the reducer path before raising latency budgets"
	case "post_collapse_reread_full_context", "recent_edit_full_context", "recent_edit_uncertain_chunk_full_context":
		return "context_fidelity_guard",
			"recent edits or post-collapse rereads require full context for model fidelity",
			"only replace with an exact state mirror or archive-backed proof for the same file lineage"
	default:
		return "unclassified_guard",
			"unknown full-pass reason; do not treat as safe savings",
			"instrument and classify this guard before changing product behavior"
	}
}

func wssLocalGapDecisionActionForFacts(reason string, facts map[string]string) (string, string, string, string) {
	reason = strings.TrimSpace(reason)
	source := "evidence:" + reason
	if reason != "wss_search_output_risk_gate" {
		category, policy, nextStep := wssLocalGapDecisionAction(reason)
		return category, source, policy, nextStep
	}
	blockReason := wssLocalGapSearchProofBlockReason(facts)
	if blockReason == "" {
		category, policy, nextStep := wssLocalGapDecisionAction(reason)
		return category, source, policy, nextStep
	}
	source += ":" + blockReason
	switch blockReason {
	case "tool_use_unbound", "tool_use_empty", "workload_not_search":
		return "search_command_binding_required",
			source,
			"search-looking output is not enough; the search-cap latch requires a bound exact command and search workload",
			"fix command/tool-use binding or workload classification for this shape before considering search-output mutation"
	case "delta_key_missing":
		return "search_key_parser_candidate",
			source,
			"stateful-delta search mutation requires a stable search key for the exact command shape",
			"extend SearchOutputKey parsing only with exact command-shape proof, otherwise keep full-pass"
	case "reducer_ineligible":
		return "search_reducer_ineligible",
			source,
			"the output looked search-like but the command is outside the proven reducer surface",
			"add a reducer proof for the exact command shape or keep full-pass"
	case "latch_disabled":
		return "proof_latch_candidate",
			source,
			"search-output mutation is blocked because the final proof latch is not active",
			"verify and configure the final search-cap proof path before changing runtime guards"
	default:
		category, policy, nextStep := wssLocalGapDecisionAction(reason)
		return category, source, policy, nextStep
	}
}

func wssLocalGapSearchProofBlockReason(facts map[string]string) string {
	counts := wssLocalGapFactCountPairs(facts, "wss.search_proof_block_reasons")
	if len(counts) == 0 {
		return ""
	}
	for _, reason := range []string{"tool_use_unbound", "tool_use_empty", "workload_not_search", "delta_key_missing", "reducer_ineligible", "latch_disabled"} {
		if counts[reason] > 0 {
			return reason
		}
	}
	keys := make([]string, 0, len(counts))
	for reason := range counts {
		if strings.TrimSpace(reason) != "" {
			keys = append(keys, reason)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func wssLocalGapNoEvidenceAction(summary dbg.RequestSummary, shapeSource string) (string, string, string, string) {
	facts := summary.DebugFacts
	outputReason := strings.TrimSpace(facts["wss.output_reduce_reason"])
	toolResults := strings.TrimSpace(facts["wss.tool_results"])
	toolResultOutputBytesFact := strings.TrimSpace(facts["wss.tool_result_output_bytes"])
	toolResultOutputBytes := wssLocalGapFactInt(facts, "wss.tool_result_output_bytes")
	sourceToolBytes := strings.TrimSpace(facts["wss.source_tool_bytes"])
	sourceToolByteCount := wssLocalGapFactInt(facts, "wss.source_tool_bytes")
	guardCategory, guardSource, guardPolicy, guardNextStep, guardOK := wssLocalGapNoEvidenceGuardAction(facts)
	switch {
	case outputReason == "prompt_cache_prefix_full_pass":
		toolDefinitionBytes := wssLocalGapFactInt(facts, "wss.tool_definition_bytes")
		instructionBytes := wssLocalGapFactInt(facts, "wss.instructions_bytes")
		prefixTotalBytes := wssLocalGapFactInt(facts, "wss.prefix_total_bytes")
		if toolDefinitionBytes == 0 && instructionBytes == 0 && prefixTotalBytes == 0 {
			return "needs_instrumentation",
				"no_evidence:prompt_cache_prefix_metrics_missing",
				"prompt-cache-prefix full-pass fired, but no content-free prefix byte split was recorded",
				"capture fresh prefix metrics before treating this as a prefix-savings mechanism candidate"
		}
		defaultKeepBytes := wssLocalGapFactInt(facts, "wss.tool_definition_default_keep_bytes")
		nonDefaultBytes := wssLocalGapFactInt(facts, "wss.tool_definition_nondefault_bytes")
		source := "no_evidence:wss.output_reduce_reason=prompt_cache_prefix_full_pass"
		policy := "prompt-cache-prefix frames must stay byte/semantic stable; no WSS directive injection"
		nextStep := "design a prefix-preserving deterministic reducer or keep full-pass"
		switch {
		case toolDefinitionBytes > 0 && instructionBytes > 0:
			source = "no_evidence:prompt_cache_prefix_tools_and_instructions"
			nextStep = "measure schema-vs-instruction mass, then prove prefix-safe tool-prune or keep full-pass"
			if defaultKeepBytes > 0 && nonDefaultBytes == 0 {
				source = "no_evidence:prompt_cache_prefix_default_keep_tools_and_instructions"
				policy = "default-keep/control tool and instruction prefixes are model-facing capability context; live WSS command proof showed schema elision can suppress command_execution"
				nextStep = "keep this mass in the product path; recover savings through Layer-0 tool-output reducers, search proof latches, or non-capability prefixes"
			}
		case toolDefinitionBytes > 0:
			source = "no_evidence:prompt_cache_prefix_tools"
			nextStep = "measure tool-schema mass, then prove prefix-safe tool-prune before pruning"
			if defaultKeepBytes > 0 && nonDefaultBytes == 0 {
				source = "no_evidence:prompt_cache_prefix_default_keep_tools"
				policy = "default-keep/control tool prefixes are model-facing capability context; live WSS command proof showed schema elision can suppress command_execution"
				nextStep = "keep this mass in the product path; recover savings through Layer-0 tool-output reducers, search proof latches, or non-capability prefixes"
			}
		case instructionBytes > 0:
			source = "no_evidence:prompt_cache_prefix_instructions"
			nextStep = "design an instruction-preserving mechanism; do not inject WSS directives into the prefix"
		}
		category := "prefix_safe_new_mechanism_required"
		if strings.Contains(source, "prompt_cache_prefix_default_keep_tools") {
			category = "prefix_capability_context_guarded"
		}
		return category,
			source,
			policy,
			nextStep
	case strings.TrimSpace(summary.BypassReason) == "wss_previous_response_source_tool_output_full_pass":
		category, policy, nextStep := wssLocalGapDecisionAction("wss_source_tool_output_full_pass")
		return category,
			"no_evidence:bypass_reason=wss_previous_response_source_tool_output_full_pass",
			policy,
			nextStep
	case sourceToolByteCount >= wssLocalGapSourceContextMinBytes:
		category, policy, nextStep := wssLocalGapDecisionAction("wss_source_tool_output_full_pass")
		return category,
			"no_evidence:wss.source_tool_bytes>=4096",
			policy,
			nextStep
	case toolResults != "" && toolResults != "0" && sourceToolBytes == "0" && toolResultOutputBytesFact == "" && wssLocalGapPrefixDominatesRequest(summary):
		return "prefix_bound_tool_output_context",
			"no_evidence:missing_tool_output_bytes_prefix_bound",
			"request token mass is dominated by protected prompt-cache prefix, not measurable tool-output bytes",
			"keep payload-byte instrumentation on; do not widen Layer-0 reducers until non-prefix output bytes are measured"
	case toolResults != "" && toolResults != "0" && toolResultOutputBytesFact != "" && toolResultOutputBytes > 0 && toolResultOutputBytes <= wssLocalGapTinyToolOutputMaxBytes:
		return "small_tool_output_context",
			"no_evidence:wss.tool_result_output_bytes<=255",
			"measured tool-output payload is too small for a net-positive archive or structured replacement",
			"ignore as a request-token savings candidate; rely on larger/repeated outputs or prefix-safe mechanisms"
	case toolResults == "0" && sourceToolBytes == "0":
		return "not_tool_output_reducer_target",
			"no_evidence:no_tool_output",
			"no tool-output bytes were present for Layer-0 reducers",
			"look for prompt/root/context mechanisms, not tool-output guard loosening"
	case toolResults != "" && toolResults != "0" && toolResultOutputBytesFact != "" && toolResultOutputBytes == 0:
		return "empty_tool_output_context",
			"no_evidence:empty_tool_output",
			"tool result items were present but their command-output bytes were zero",
			"ignore this as a tool-output savings candidate; attribute the remaining request mass to prefix or request context"
	case strings.TrimSpace(summary.DebugFacts["wss.request_shape"]) == "" && strings.TrimSpace(shapeSource) == "unresolved":
		return "needs_instrumentation",
			"no_evidence:wss.request_shape_missing",
			"request shape was not recorded, so savings loss cannot be assigned safely",
			"add content-free shape/debug facts before changing guards"
	case strings.TrimSpace(summary.BypassReason) == "wss_previous_response_tool_output_full_pass":
		if !wssLocalGapPreviousResponseBypassHasToolFacts(facts) {
			return "needs_instrumentation",
				"no_evidence:bypass_reason=wss_previous_response_tool_output_full_pass:tool_payload_or_command_missing",
				"previous_response_id tool-output bypass fired, but the row lacks content-free payload or command-class facts",
				"capture fresh rows with tool-result byte facts and command classes before treating this as a proof-only recovery candidate"
		}
		return "unsafe_without_fresh_live_proof",
			"no_evidence:bypass_reason=wss_previous_response_tool_output_full_pass",
			"previous_response_id tool-output bypass protects Codex server state when the exact tool-use binding is unavailable",
			"keep full-pass unless exact command binding plus downstream-delta live proof covers this shape"
	case strings.TrimSpace(summary.BypassReason) != "":
		return "needs_instrumentation",
			"no_evidence:bypass_reason=" + strings.TrimSpace(summary.BypassReason),
			"bypass fired without block-level evidence",
			"wire the bypass to evidence decisions before changing behavior"
	case guardOK:
		return guardCategory, guardSource, guardPolicy, guardNextStep
	case outputReason == "disabled":
		predicate := strings.TrimSpace(facts["wss.output_reduce_disabled_predicate"])
		switch predicate {
		case "tool_output_context", "tool_output_after_layer0_mutation":
			return "not_output_reduce_target",
				"no_evidence:wss.output_reduce_disabled_predicate=" + predicate,
				"output-reduce directives stay off for tool-output turns; Layer-0 reducers own this surface",
				"add or tighten Layer-0 evidence for this tool-output shape before changing output-reduce guards"
		case "layer0_mutation_context":
			return "not_output_reduce_target",
				"no_evidence:wss.output_reduce_disabled_predicate=layer0_mutation_context",
				"output-reduce stays off after Layer-0 mutation to avoid stacking behavioral directives on rewritten input",
				"measure remaining post-Layer0 token mass before considering any follow-up reducer"
		case "no_user_prompt":
			return "not_output_reduce_target",
				"no_evidence:wss.output_reduce_disabled_predicate=no_user_prompt",
				"output-reduce is scoped to user-prompt turns and should not inject into tool-only or lifecycle frames",
				"look for prefix or Layer-0 mechanisms, not output-reduce directive injection"
		case "operator_or_layer_disabled":
			return "disabled_by_configuration",
				"no_evidence:wss.output_reduce_disabled_predicate=operator_or_layer_disabled",
				"the output-reduce layer is disabled by configuration or layer selection",
				"do not treat this as guard waste unless the product default unexpectedly disables Layer 3"
		case "unknown_shape", "unclassified_disabled":
			return "needs_instrumentation",
				"no_evidence:wss.output_reduce_disabled_predicate=" + predicate,
				"output reducer was disabled but the exact shape still needs stronger attribution",
				"record the missing shape/facts before changing behavior"
		}
		return "needs_instrumentation",
			"no_evidence:wss.output_reduce_reason=disabled",
			"output reducer was disabled for this shape without block-level opportunity evidence",
			"record the concrete disabling predicate and eligible token mass"
	default:
		return "needs_instrumentation",
			"no_evidence:unclassified",
			"no block-level evidence exists for this token mass",
			"add content-free evidence decisions before treating it as a savings candidate"
	}
}

func wssLocalGapPrefixDominatesRequest(summary dbg.RequestSummary) bool {
	original := maxInt(0, summary.Tokens.Original)
	if original <= 0 {
		return false
	}
	prefixEstimate := wssLocalGapFactInt(summary.DebugFacts, "wss.prefix_estimated_tokens")
	return prefixEstimate > 0 && prefixEstimate*100 >= original*90
}

func wssLocalGapNoEvidenceGuardAction(facts map[string]string) (string, string, string, string, bool) {
	for _, key := range []string{
		"wss.downstream_state_mutation_guard",
		"wss.history_mutation_guard",
		"wss.structured_mutation_guard",
		"wss.effective_mutation_guard",
		"wss.tool_prune_guard",
	} {
		reason := strings.TrimSpace(facts[key])
		if reason == "" {
			continue
		}
		if reason == "wss_stateful_structured_mutation_guard" {
			if category, source, policy, nextStep, ok := wssLocalGapStatefulStructuredGuardAction(key, reason, facts); ok {
				return category, source, policy, nextStep, true
			}
		}
		category, policy, nextStep := wssLocalGapDecisionAction(reason)
		return category, "no_evidence:" + key + "=" + reason, policy, nextStep, true
	}
	return "", "", "", "", false
}

func wssLocalGapPreviousResponseBypassHasToolFacts(facts map[string]string) bool {
	return wssLocalGapFactInt(facts, "wss.tool_result_bytes") > 0 ||
		wssLocalGapFactInt(facts, "wss.tool_result_output_bytes") > 0 ||
		strings.TrimSpace(facts["wss.tool_command_classes"]) != "" ||
		strings.TrimSpace(facts["wss.tool_command_classed"]) != "" ||
		strings.TrimSpace(facts["wss.tool_command_unclassed"]) != ""
}

func wssLocalGapStatefulStructuredGuardAction(key, reason string, facts map[string]string) (string, string, string, string, bool) {
	source := "no_evidence:" + key + "=" + reason
	if strings.TrimSpace(facts["wss.tool_command_classes"]) != "" {
		return "", "", "", "", false
	}
	classedRaw := strings.TrimSpace(facts["wss.tool_command_classed"])
	unclassedRaw := strings.TrimSpace(facts["wss.tool_command_unclassed"])
	if classedRaw == "" && unclassedRaw == "" {
		return "needs_instrumentation",
			source + ":tool_command_class_missing",
			"stateful structured mutation was guarded but no content-free command-class fact was recorded",
			"capture fresh rows with wss.tool_command_classes and tool-result byte facts before adding parser classes",
			true
	}
	if wssLocalGapFactInt(facts, "wss.tool_command_classed") == 0 && wssLocalGapFactInt(facts, "wss.tool_command_unclassed") > 0 {
		return "stateful_command_binding_required",
			source + ":tool_command_unclassed",
			"stateful tool-output mutation needs a bound or deterministically inferred command class",
			"fix tool-use binding or command inference for this shape before adding parser classes",
			true
	}
	return "", "", "", "", false
}

func wssLocalGapPrefixDecisionSurface(facts map[string]string) (controlContextBytes, nonDefaultCandidateBytes, unclassifiedToolBytes int) {
	if facts == nil {
		return 0, 0, 0
	}
	controlContextBytes = wssLocalGapFactInt(facts, "wss.instructions_bytes") + wssLocalGapFactInt(facts, "wss.tool_definition_default_keep_bytes")
	nonDefaultCandidateBytes = wssLocalGapFactInt(facts, "wss.tool_definition_nondefault_bytes")
	unclassifiedToolBytes = wssLocalGapFactInt(facts, "wss.tool_definition_unnamed_bytes")
	return controlContextBytes, nonDefaultCandidateBytes, unclassifiedToolBytes
}

func wssLocalGapFactInt(facts map[string]string, key string) int {
	if facts == nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(facts[key]))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func wssLocalGapFactListCounts(facts map[string]string, key string) map[string]int {
	if facts == nil {
		return nil
	}
	raw := strings.TrimSpace(facts[key])
	if raw == "" {
		return nil
	}
	var counts map[string]int
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		addWSSAuditCount(&counts, name)
	}
	return counts
}

func wssLocalGapFactCountPairs(facts map[string]string, key string) map[string]int {
	if facts == nil {
		return nil
	}
	raw := strings.TrimSpace(facts[key])
	if raw == "" {
		return nil
	}
	var counts map[string]int
	for _, part := range strings.Split(raw, ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		count := 1
		if ok {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || parsed <= 0 {
				continue
			}
			count = parsed
		}
		if counts == nil {
			counts = make(map[string]int)
		}
		counts[name] += count
	}
	return counts
}

func mergeWSSLocalGapCounts(dst *map[string]int, src map[string]int) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]int, len(src))
	}
	for key, count := range src {
		if strings.TrimSpace(key) == "" || count <= 0 {
			continue
		}
		(*dst)[key] += count
	}
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
	fmt.Fprintf(w, "Positive-savings ratio:    %d/%d / %.2f%%\n", report.LocalSavedTokens, report.PositiveSavingsOrig, report.PositiveSavingsRatio*100)
	fmt.Fprintf(w, "Target saved/deficit:      %d / %d at %.2f%%\n", report.TargetSavedTokens, report.TargetDeficitTokens, report.TargetRatio*100)
	fmt.Fprintf(w, "Policy ceiling/deficit:    %d / %.2f%% / %d\n", report.PolicySavingsCeiling, report.PolicySavingsCeilingRate*100, report.PolicyCeilingDeficit)
	fmt.Fprintf(w, "Provider cache read/cached/create: %d / %d / %d\n",
		report.ProviderCacheReadTokens,
		report.ProviderCacheTokens,
		report.ProviderCacheCreate)
	fmt.Fprintf(w, "Positive/zero savings reqs:%d / %d\n", report.PositiveSavingsRequests, report.ZeroSavingsRequests)
	fmt.Fprintf(w, "Zero/no-evidence orig:     %d / %d\n", report.ZeroSavingsOrigTokens, report.NoEvidenceOrigTokens)
	if report.NoEvidenceOrigTokens > 0 {
		fmt.Fprintf(w, "No-evidence classified:    protected=%d known_non_target=%d proof_blocked_or_candidate=%d needs_instrumentation=%d\n",
			report.NoEvidenceProtected,
			report.NoEvidenceKnownNonTarget,
			report.NoEvidenceProofBlocked,
			report.NoEvidenceNeedsInstr)
	}
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
	if len(report.ActionablePotential) > 0 {
		fmt.Fprintln(w, "\nActionable potential:")
		for _, row := range report.ActionablePotential {
			fmt.Fprintf(w, "  %-40s source=%-58s tokens=%d basis=%s requests=%d decisions=%d saved=%d shapes=%s mechanisms=%s classes=%s\n",
				row.Category,
				row.Source,
				row.Tokens,
				row.TokenBasis,
				row.Requests,
				row.Decisions,
				row.LocalSavedTokens,
				formatWSSAuditCounts(row.RequestShapes),
				formatWSSAuditCounts(row.Mechanisms),
				formatWSSAuditCounts(row.ToolCommandClasses))
			if row.PrefixToolDefinitionBytes > 0 || row.PrefixInstructionBytes > 0 || row.PrefixToolDefinitions > 0 {
				fmt.Fprintf(w, "    prefix: tool_definition_bytes=%d instruction_bytes=%d tool_definitions=%d max_tools_per_request=%d\n",
					row.PrefixToolDefinitionBytes,
					row.PrefixInstructionBytes,
					row.PrefixToolDefinitions,
					row.PrefixMaxToolDefinitions)
				if row.PrefixToolNameBytes > 0 || row.PrefixToolDescriptionBytes > 0 || row.PrefixToolParametersBytes > 0 || row.PrefixToolOtherBytes > 0 {
					fmt.Fprintf(w, "            components: name=%dB description=%dB parameters=%dB other=%dB\n",
						row.PrefixToolNameBytes,
						row.PrefixToolDescriptionBytes,
						row.PrefixToolParametersBytes,
						row.PrefixToolOtherBytes)
				}
				if row.PrefixDefaultKeepTools > 0 || row.PrefixNonDefaultTools > 0 || row.PrefixUnnamedTools > 0 {
					fmt.Fprintf(w, "            default_keep=%d/%dB nondefault=%d/%dB unnamed=%d/%dB\n",
						row.PrefixDefaultKeepTools,
						row.PrefixDefaultKeepBytes,
						row.PrefixNonDefaultTools,
						row.PrefixNonDefaultBytes,
						row.PrefixUnnamedTools,
						row.PrefixUnnamedBytes)
					if row.PrefixDefaultDescriptionBytes > 0 || row.PrefixDefaultParametersBytes > 0 || row.PrefixNonDefaultDescriptionBytes > 0 || row.PrefixNonDefaultParametersBytes > 0 {
						fmt.Fprintf(w, "            component_by_class: default_desc=%dB default_params=%dB nondefault_desc=%dB nondefault_params=%dB\n",
							row.PrefixDefaultDescriptionBytes,
							row.PrefixDefaultParametersBytes,
							row.PrefixNonDefaultDescriptionBytes,
							row.PrefixNonDefaultParametersBytes)
					}
					if len(row.PrefixDefaultKeepNames) > 0 || len(row.PrefixNonDefaultNames) > 0 {
						fmt.Fprintf(w, "            default_keep_names=%s nondefault_names=%s\n",
							formatWSSAuditCounts(row.PrefixDefaultKeepNames),
							formatWSSAuditCounts(row.PrefixNonDefaultNames))
					}
					if row.PrefixControlContextBytes > 0 || row.PrefixNonDefaultCandidateBytes > 0 || row.PrefixUnclassifiedToolBytes > 0 {
						fmt.Fprintf(w, "            decision_surface: control_context=%dB nondefault_candidate=%dB unclassified_tool=%dB\n",
							row.PrefixControlContextBytes,
							row.PrefixNonDefaultCandidateBytes,
							row.PrefixUnclassifiedToolBytes)
					}
				}
			}
			if row.OutputReduceInputTokens > 0 || row.OutputReduceEligibleInputTokens > 0 {
				fmt.Fprintf(w, "    output_reduce: input_tokens=%d eligible_input_tokens=%d\n",
					row.OutputReduceInputTokens,
					row.OutputReduceEligibleInputTokens)
			}
			fmt.Fprintf(w, "    policy: %s\n", row.Policy)
			fmt.Fprintf(w, "    next:   %s\n", row.NextStep)
		}
	}
	if len(report.RequestGuards) > 0 {
		fmt.Fprintln(w, "\nRequest-level guards:")
		for _, row := range report.RequestGuards {
			fmt.Fprintf(w, "  %-72s requests=%d original=%d saved=%d zero_orig=%d no_evidence_orig=%d shapes=%s\n",
				row.Guard,
				row.Requests,
				row.OriginalTokens,
				row.LocalSavedTokens,
				row.ZeroSavingsOrigTokens,
				row.NoEvidenceOrigTokens,
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

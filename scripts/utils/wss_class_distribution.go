package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
)

// wss-class-distribution decomposes WSS Phase-F input mass into the four
// billing classes (roadmap-48pct-wss.md section 1) and answers the binding
// S_local-ceiling question without provider-cache credit:
//
//   - Class C protected prefix (instructions + tool schemas): model-facing
//     capability context, provider-cached after turn 1, never reducible.
//   - reducible tool-output (Layer-0 target): the only mass Layer-0 reducers
//     may compact; estimated from wss.tool_result_output_bytes.
//   - other context (messages, user prompts, reasoning/Class D): model context
//     and encrypted reasoning, not a Layer-0 reducer target.
//
// The headline metric is reducible_ceiling_ratio = reducible_tool_output /
// original: the most optimistic S_local achievable if every tool output were
// compacted to zero. When that ceiling is below the owner target the report
// emits corpus-ceiling evidence; otherwise it reports un-captured headroom so
// the next move is a guard/shape investigation, not a structural claim.

type wssClassDistributionFlags struct {
	path            string
	outputFormat    string
	since           time.Time
	minLocalRatio   float64
	requireHeadroom bool
	help            bool
}

type wssClassDistributionReport struct {
	Path                      string                         `json:"path"`
	TargetRatio               float64                        `json:"target_ratio"`
	Logs                      int                            `json:"logs"`
	PhaseFRequests            int                            `json:"phasef_requests"`
	RequestsWithoutFacts      int                            `json:"requests_without_shape_facts,omitempty"`
	OriginalTokens            int                            `json:"original_tokens"`
	LocalSavedTokens          int                            `json:"local_saved_tokens"`
	LocalSavingsRatio         float64                        `json:"local_savings_ratio"`
	PrefixProtectedTokens     int                            `json:"prefix_protected_tokens"`
	PrefixProtectedShare      float64                        `json:"prefix_protected_share"`
	ReducibleToolOutputTokens int                            `json:"reducible_tool_output_tokens"`
	ReducibleToolOutputShare  float64                        `json:"reducible_tool_output_share"`
	OtherContextTokens        int                            `json:"other_context_tokens"`
	OtherContextShare         float64                        `json:"other_context_share"`
	NonPrefixTokens           int                            `json:"non_prefix_tokens"`
	NonPrefixRatio            float64                        `json:"non_prefix_ratio"`
	PrefixMutationSavedTokens int                            `json:"prefix_mutation_saved_tokens,omitempty"`
	ReducibleCeilingRatio     float64                        `json:"reducible_ceiling_ratio"`
	ReducibleCeilingDeficit   int                            `json:"reducible_ceiling_deficit_tokens,omitempty"`
	ReducibleHeadroomTokens   int                            `json:"reducible_headroom_tokens,omitempty"`
	ProviderCacheReadTokens   int                            `json:"provider_cache_read_tokens"`
	ProviderCachedTokens      int                            `json:"provider_cached_tokens"`
	ReasoningItems            int                            `json:"reasoning_items"`
	Verdict                   string                         `json:"verdict"`
	VerdictDetail             string                         `json:"verdict_detail"`
	HeadroomPresent           bool                           `json:"headroom_present"`
	GapInventoryRecommended   bool                           `json:"gap_inventory_recommended"`
	NextAction                string                         `json:"next_action"`
	Classes                   []wssClassDistributionClassRow `json:"classes"`
	PerLog                    []wssClassDistributionLogRow   `json:"per_log,omitempty"`
	Notes                     []string                       `json:"notes,omitempty"`
}

type wssClassDistributionClassRow struct {
	Class                     string         `json:"class"`
	Requests                  int            `json:"requests"`
	OriginalTokens            int            `json:"original_tokens"`
	LocalSavedTokens          int            `json:"local_saved_tokens"`
	LocalSavingsRatio         float64        `json:"local_savings_ratio"`
	PrefixProtectedTokens     int            `json:"prefix_protected_tokens"`
	PrefixProtectedShare      float64        `json:"prefix_protected_share"`
	ReducibleToolOutputTokens int            `json:"reducible_tool_output_tokens"`
	ReducibleToolOutputShare  float64        `json:"reducible_tool_output_share"`
	OtherContextTokens        int            `json:"other_context_tokens"`
	OtherContextShare         float64        `json:"other_context_share"`
	ReducibleCeilingRatio     float64        `json:"reducible_ceiling_ratio"`
	ProviderCachedTokens      int            `json:"provider_cached_tokens"`
	ShapeSources              map[string]int `json:"shape_sources,omitempty"`
}

type wssClassDistributionLogRow struct {
	Name                      string  `json:"name"`
	Path                      string  `json:"path"`
	PhaseFRequests            int     `json:"phasef_requests"`
	OriginalTokens            int     `json:"original_tokens"`
	LocalSavedTokens          int     `json:"local_saved_tokens"`
	LocalSavingsRatio         float64 `json:"local_savings_ratio"`
	PrefixProtectedTokens     int     `json:"prefix_protected_tokens"`
	ReducibleToolOutputTokens int     `json:"reducible_tool_output_tokens"`
	ReducibleCeilingRatio     float64 `json:"reducible_ceiling_ratio"`
}

// wssClassDistributionClass maps a resolved request shape to a billing-class
// label. delta -> Class A, full_history -> Class B, root stays root; unresolved
// rows are tracked as unknown so optimistic shares can never be claimed for
// them.
const (
	wssClassDistributionClassDelta       = "delta"
	wssClassDistributionClassFullHistory = "full_history"
	wssClassDistributionClassRoot        = "root"
	wssClassDistributionClassUnknown     = "unknown"
)

const wssClassDistributionEpsilon = 1e-9

const wssClassDistributionHelpText = `wss-class-distribution: decompose WSS Phase-F input mass into billing classes

Usage:
  go run ./scripts/utils wss-class-distribution <dir-or-decisions.jsonl> [flags]

Flags:
	--since=<rfc3339>          Ignore records before this timestamp
	--min-local-ratio=<ratio>  Owner S_local target for the verdict, default 0.48
	--require-headroom         Exit 1 unless verdict=headroom_present
	--json                     Output JSON

Directory mode scans recursively for decisions.jsonl and *.decisions.jsonl, the
same content-free RequestSummary records as wss-local-gap. Every WSS Phase-F
request is split into protected prefix tokens (Class C capability context,
estimated from wss.prefix_estimated_tokens), reducible tool-output tokens
(Layer-0 target, tokens.Estimate of wss.tool_result_output_bytes), and other
context tokens (the remainder: messages, user prompts, and Class-D reasoning).
reducible_ceiling_ratio is the most optimistic S_local achievable if every tool
output were compacted to zero; when it is below the target the report records
corpus-ceiling evidence, otherwise it reports un-captured reducible headroom.
Provider-cache tokens are reported separately and never counted as S_local.`

func runWSSClassDistribution(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSClassDistributionFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssClassDistributionHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-class-distribution <dir-or-decisions.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSClassDistribution(flags)
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
		writeWSSClassDistributionText(stdout, report)
	}
	if flags.requireHeadroom && !report.HeadroomPresent {
		fmt.Fprintf(stderr, "wss-class-distribution: headroom not present (%s)\n", report.Verdict)
		return 1
	}
	return 0
}

func parseWSSClassDistributionFlags(args []string) (wssClassDistributionFlags, error) {
	flags := wssClassDistributionFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--require-headroom":
			flags.requireHeadroom = true
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
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple distribution roots provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

type wssClassDistributionAccumulator struct {
	report wssClassDistributionReport
	rows   map[string]*wssClassDistributionClassRow
}

func loadWSSClassDistribution(flags wssClassDistributionFlags) (wssClassDistributionReport, error) {
	paths, err := wssLocalGapInventoryPaths(flags.path)
	if err != nil {
		return wssClassDistributionReport{}, err
	}
	if len(paths) == 0 {
		return wssClassDistributionReport{}, fmt.Errorf("no decisions logs found under %s", flags.path)
	}
	targetRatio := flags.minLocalRatio
	if targetRatio == 0 {
		targetRatio = 0.48
	}
	acc := wssClassDistributionAccumulator{
		report: wssClassDistributionReport{
			Path:        flags.path,
			TargetRatio: targetRatio,
		},
		rows: make(map[string]*wssClassDistributionClassRow),
	}
	for _, path := range paths {
		summaries, err := dbg.ReplaySession(path)
		if err != nil {
			return wssClassDistributionReport{}, fmt.Errorf("read decisions %s: %w", path, err)
		}
		logRow := wssClassDistributionLogRow{Name: wssLocalGapInventoryName(path), Path: path}
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
			finalizeWSSClassDistributionLogRow(&logRow)
			acc.report.PerLog = append(acc.report.PerLog, logRow)
			acc.report.Logs++
		}
	}
	acc.finalize(targetRatio)
	return acc.report, nil
}

func (a *wssClassDistributionAccumulator) addPhaseF(summary dbg.RequestSummary, logRow *wssClassDistributionLogRow) {
	original := maxInt(0, summary.Tokens.Original)
	saved := maxInt(0, summary.Tokens.Saved)
	prefixTokens, reducibleTokens, otherTokens, prefixMutationSaved := wssClassDistributionSplit(summary, original, saved)

	a.report.PhaseFRequests++
	a.report.OriginalTokens += original
	a.report.LocalSavedTokens += saved
	a.report.PrefixProtectedTokens += prefixTokens
	a.report.ReducibleToolOutputTokens += reducibleTokens
	a.report.OtherContextTokens += otherTokens
	a.report.PrefixMutationSavedTokens += prefixMutationSaved
	a.report.ProviderCacheReadTokens += maxInt(0, summary.CacheReadTokens)
	a.report.ProviderCachedTokens += maxInt(0, summary.ProviderCachedTokens)
	a.report.ReasoningItems += wssLocalGapFactInt(summary.DebugFacts, "wss.raw_input_reasoning_items")
	if !wssClassDistributionHasShapeFacts(summary.DebugFacts) {
		a.report.RequestsWithoutFacts++
	}

	resolution := wssAuditResolveRequestShape(summary)
	class := wssClassDistributionClassForShape(resolution.Shape)
	row := a.classRow(class)
	row.Requests++
	row.OriginalTokens += original
	row.LocalSavedTokens += saved
	row.PrefixProtectedTokens += prefixTokens
	row.ReducibleToolOutputTokens += reducibleTokens
	row.OtherContextTokens += otherTokens
	row.ProviderCachedTokens += maxInt(0, summary.ProviderCachedTokens)
	addWSSAuditCount(&row.ShapeSources, resolution.Source)

	logRow.PhaseFRequests++
	logRow.OriginalTokens += original
	logRow.LocalSavedTokens += saved
	logRow.PrefixProtectedTokens += prefixTokens
	logRow.ReducibleToolOutputTokens += reducibleTokens
}

// wssClassDistributionSplit decomposes one request's original tokens into
// protected prefix (Class C), reducible tool-output (the Layer-0 target), and
// other context (messages plus Class-D reasoning). The three components always
// sum to original exactly.
//
// wss.tool_result_output_bytes is measured on the post-mutation messages on the
// main pipeline path, so it only reflects the tool-output that REMAINS after
// Layer-0 ran. The original reducible tool-output is therefore the already
// saved tool-output plus that remaining mass. Prefix-mutation savings
// (lab-only stateful prefix elision) are Class-C reduction, not tool-output, so
// they are excluded from the saved tool-output term to avoid inflating the
// reducible mass with prefix lab experiments.
func wssClassDistributionSplit(summary dbg.RequestSummary, original, saved int) (prefix, reducible, other, prefixMutationSaved int) {
	if original <= 0 {
		return 0, 0, 0, 0
	}
	prefix = wssLocalGapFactInt(summary.DebugFacts, "wss.prefix_estimated_tokens")
	if prefix > original {
		prefix = original
	}
	prefixMutationSaved = wssLocalGapFactInt(summary.DebugFacts, "wss.stateful_prefix_elision_tokens_saved")
	if prefixMutationSaved > saved {
		prefixMutationSaved = saved
	}
	toolOutputSaved := maxInt(0, saved-prefixMutationSaved)
	remaining := tokens.Estimate(wssLocalGapFactInt(summary.DebugFacts, "wss.tool_result_output_bytes"))
	// original and saved are exact o200k counts; prefix and remaining are
	// byte/4 estimates. saved is the hardest signal and is non-prefix
	// (tool-output) mass, so the prefix estimate is capped so that the
	// non-prefix mass (original - prefix) can never sit below the already-saved
	// tool-output. This keeps the non-prefix upper bound and the tool-output
	// estimate at or above the measured S_local, so the ceiling can never be
	// reported below the actual savings.
	maxPrefix := original - toolOutputSaved
	if prefix > maxPrefix {
		prefix = maxPrefix
	}
	if prefix < 0 {
		prefix = 0
	}
	nonPrefix := original - prefix
	// reducible tool-output is the already-saved tool-output plus the
	// remaining post-mutation tool-output bytes, bounded by the non-prefix mass
	// (messages and reasoning are the rest). This over-counts non-reducible
	// first reads, so it is an optimistic ceiling, never a floor.
	reducible = toolOutputSaved + remaining
	if reducible > nonPrefix {
		reducible = nonPrefix
	}
	other = nonPrefix - reducible
	if other < 0 {
		other = 0
	}
	return prefix, reducible, other, prefixMutationSaved
}

func wssClassDistributionHasShapeFacts(facts map[string]string) bool {
	if facts == nil {
		return false
	}
	_, ok := facts["wss.prefix_total_bytes"]
	return ok
}

func wssClassDistributionClassForShape(shape string) string {
	switch strings.TrimSpace(shape) {
	case "delta":
		return wssClassDistributionClassDelta
	case "full_history":
		return wssClassDistributionClassFullHistory
	case "root":
		return wssClassDistributionClassRoot
	default:
		return wssClassDistributionClassUnknown
	}
}

func (a *wssClassDistributionAccumulator) classRow(class string) *wssClassDistributionClassRow {
	row := a.rows[class]
	if row == nil {
		row = &wssClassDistributionClassRow{Class: class}
		a.rows[class] = row
	}
	return row
}

func (a *wssClassDistributionAccumulator) finalize(targetRatio float64) {
	a.report.NonPrefixTokens = maxInt(0, a.report.OriginalTokens-a.report.PrefixProtectedTokens)
	a.report.LocalSavingsRatio = wssLocalGapRatio(a.report.LocalSavedTokens, a.report.OriginalTokens)
	a.report.PrefixProtectedShare = wssLocalGapRatio(a.report.PrefixProtectedTokens, a.report.OriginalTokens)
	a.report.ReducibleToolOutputShare = wssLocalGapRatio(a.report.ReducibleToolOutputTokens, a.report.OriginalTokens)
	a.report.OtherContextShare = wssLocalGapRatio(a.report.OtherContextTokens, a.report.OriginalTokens)
	a.report.NonPrefixRatio = wssLocalGapRatio(a.report.NonPrefixTokens, a.report.OriginalTokens)
	a.report.ReducibleCeilingRatio = wssLocalGapRatio(a.report.ReducibleToolOutputTokens, a.report.OriginalTokens)
	a.report.ReducibleCeilingDeficit = maxInt(0, targetSavedTokens(a.report.OriginalTokens, targetRatio)-a.report.ReducibleToolOutputTokens)
	a.report.ReducibleHeadroomTokens = maxInt(0, a.report.ReducibleToolOutputTokens-a.report.LocalSavedTokens)

	a.report.Classes = make([]wssClassDistributionClassRow, 0, len(a.rows))
	for _, row := range a.rows {
		copy := *row
		copy.LocalSavingsRatio = wssLocalGapRatio(copy.LocalSavedTokens, copy.OriginalTokens)
		copy.PrefixProtectedShare = wssLocalGapRatio(copy.PrefixProtectedTokens, copy.OriginalTokens)
		copy.ReducibleToolOutputShare = wssLocalGapRatio(copy.ReducibleToolOutputTokens, copy.OriginalTokens)
		copy.OtherContextShare = wssLocalGapRatio(copy.OtherContextTokens, copy.OriginalTokens)
		copy.ReducibleCeilingRatio = wssLocalGapRatio(copy.ReducibleToolOutputTokens, copy.OriginalTokens)
		a.report.Classes = append(a.report.Classes, copy)
	}
	sort.Slice(a.report.Classes, func(i, j int) bool {
		if a.report.Classes[i].OriginalTokens != a.report.Classes[j].OriginalTokens {
			return a.report.Classes[i].OriginalTokens > a.report.Classes[j].OriginalTokens
		}
		return a.report.Classes[i].Class < a.report.Classes[j].Class
	})

	sort.Slice(a.report.PerLog, func(i, j int) bool {
		if a.report.PerLog[i].ReducibleCeilingRatio != a.report.PerLog[j].ReducibleCeilingRatio {
			return a.report.PerLog[i].ReducibleCeilingRatio > a.report.PerLog[j].ReducibleCeilingRatio
		}
		return a.report.PerLog[i].Name < a.report.PerLog[j].Name
	})

	a.report.Verdict, a.report.VerdictDetail = wssClassDistributionVerdict(a.report, targetRatio)
	a.report.HeadroomPresent = wssClassDistributionHeadroomPresent(a.report, targetRatio)
	a.report.GapInventoryRecommended = a.report.HeadroomPresent
	a.report.NextAction = wssClassDistributionNextAction(a.report)
	a.report.Notes = wssClassDistributionNotes(a.report, targetRatio)
}

func finalizeWSSClassDistributionLogRow(row *wssClassDistributionLogRow) {
	row.LocalSavingsRatio = wssLocalGapRatio(row.LocalSavedTokens, row.OriginalTokens)
	row.ReducibleCeilingRatio = wssLocalGapRatio(row.ReducibleToolOutputTokens, row.OriginalTokens)
}

func wssClassDistributionVerdict(report wssClassDistributionReport, targetRatio float64) (string, string) {
	if report.OriginalTokens == 0 {
		return "no_data", "No WSS Phase-F mass found; cannot evaluate the S_local ceiling."
	}
	if report.ReducibleCeilingRatio+wssClassDistributionEpsilon < targetRatio {
		return "corpus_ceiling_evidence", fmt.Sprintf(
			"Realistic max S_local for this corpus (every reducible tool output compacted to zero) is %.2f%%, below the %.2f%% target. Protected prefix is %.2f%% and other context (messages plus Class-D reasoning) is %.2f%%; even the absolute non-prefix upper bound (if messages and reasoning were also reducible, which they are not) is only %.2f%%. Reaching the target on this corpus would require reducing prefix, message, or reasoning mass, which the zero-drawdown policy forbids. Treat this as corpus/session-class ceiling evidence unless a fresh long capture shows materially higher Class-B (full_history) mass.",
			report.ReducibleCeilingRatio*100,
			targetRatio*100,
			report.PrefixProtectedShare*100,
			report.OtherContextShare*100,
			report.NonPrefixRatio*100,
		)
	}
	return "headroom_present", fmt.Sprintf(
		"Reducible tool-output ceiling is %.2f%% (>= the %.2f%% target) while actual S_local is %.2f%%; %d reducible tool-output tokens remain un-captured. This is a guard/shape investigation, not a structural ceiling.",
		report.ReducibleCeilingRatio*100,
		targetRatio*100,
		report.LocalSavingsRatio*100,
		report.ReducibleHeadroomTokens,
	)
}

func wssClassDistributionHeadroomPresent(report wssClassDistributionReport, targetRatio float64) bool {
	return report.OriginalTokens > 0 && report.ReducibleCeilingRatio+wssClassDistributionEpsilon >= targetRatio
}

func wssClassDistributionNextAction(report wssClassDistributionReport) string {
	switch report.Verdict {
	case "headroom_present":
		return "run wss-local-gap-inventory on the same capture and patch only the largest exact zero-drawdown blocker"
	case "corpus_ceiling_evidence":
		return "do not widen guards; capture an owner Desktop Class-B/full-history session, prove T354 downstream-delta/server-state safety, or record this session class as capped"
	case "no_data":
		return "capture WSS Phase-F traffic before evaluating savings eligibility"
	default:
		return "inspect verdict_detail before changing any guard"
	}
}

func wssClassDistributionNotes(report wssClassDistributionReport, targetRatio float64) []string {
	var notes []string
	if report.PhaseFRequests == 0 {
		notes = append(notes, "No WSS Phase-F rows found; cannot evaluate S_local class distribution for the product WSS path.")
		return notes
	}
	notes = append(notes, "reducible_ceiling_ratio is optimistic: it assumes every tool output is safely compactable. Real guards (first reads, source output, delta/full-history safety) reduce it further, so actual S_local cannot exceed it.")
	if report.ProviderCacheReadTokens > 0 || report.ProviderCachedTokens > 0 {
		notes = append(notes, "Provider-cache tokens are present but excluded from S_local by AGENTS.md 3.2.")
	}
	if report.PrefixMutationSavedTokens > 0 {
		notes = append(notes, fmt.Sprintf("%d saved tokens came from Class-C prefix mutation (lab-only stateful prefix elision) and were excluded from reducible tool-output; those captures can show actual S_local above the tool-output ceiling because the savings are prefix, not tool output.", report.PrefixMutationSavedTokens))
	}
	delta := wssClassDistributionFindClass(report.Classes, wssClassDistributionClassFullHistory)
	deltaA := wssClassDistributionFindClass(report.Classes, wssClassDistributionClassDelta)
	if delta != nil && deltaA != nil {
		notes = append(notes, fmt.Sprintf(
			"Class-B (full_history) reducible ceiling is %.2f%% on %d requests; Class-A (delta) reducible ceiling is %.2f%% on %d requests. Class B is the local-savings prize; delta turns are prefix-dominated.",
			delta.ReducibleCeilingRatio*100, delta.Requests,
			deltaA.ReducibleCeilingRatio*100, deltaA.Requests,
		))
	}
	if report.RequestsWithoutFacts > 0 {
		notes = append(notes, fmt.Sprintf("%d of %d Phase-F rows lack content-free prefix/shape facts (stale pre-instrumentation captures); their reducible split defaults to other-context and understates nothing but cannot confirm prefix mass.", report.RequestsWithoutFacts, report.PhaseFRequests))
	}
	if report.Verdict == "corpus_ceiling_evidence" {
		notes = append(notes, "Corpus-ceiling evidence: do not widen guards to chase the target on this corpus; the binding next step is a fresh long real-session capture to confirm the Class-B mass share, T354/L9 proof work, or an owner decision on the S_local target physics.")
	} else if report.Verdict == "headroom_present" {
		notes = append(notes, "Headroom present: rank the un-captured reducible tool-output by wss-local-gap guarded_potential and request shape before changing any guard.")
	}
	return notes
}

func wssClassDistributionFindClass(rows []wssClassDistributionClassRow, class string) *wssClassDistributionClassRow {
	for i := range rows {
		if rows[i].Class == class {
			return &rows[i]
		}
	}
	return nil
}

func writeWSSClassDistributionText(w io.Writer, report wssClassDistributionReport) {
	fmt.Fprintf(w, "=== WSS Class Distribution: %s ===\n", report.Path)
	fmt.Fprintf(w, "Logs / Phase-F requests:   %d / %d\n", report.Logs, report.PhaseFRequests)
	fmt.Fprintf(w, "S_local saved/ratio:       %d/%d / %.2f%%\n", report.LocalSavedTokens, report.OriginalTokens, report.LocalSavingsRatio*100)
	fmt.Fprintln(w, "Token composition (estimated):")
	fmt.Fprintf(w, "  Prefix protected (Class C):     %d / %.2f%%  [capability+cache, not reducible]\n", report.PrefixProtectedTokens, report.PrefixProtectedShare*100)
	fmt.Fprintf(w, "  Reducible tool-output (Layer-0): %d / %.2f%%  [the only Layer-0 target]\n", report.ReducibleToolOutputTokens, report.ReducibleToolOutputShare*100)
	fmt.Fprintf(w, "  Other context (msgs/reasoning):  %d / %.2f%%  [model context, not L0-reducible]\n", report.OtherContextTokens, report.OtherContextShare*100)
	if report.PrefixMutationSavedTokens > 0 {
		fmt.Fprintf(w, "  (prefix-mutation lab savings excluded from reducible: %d tokens)\n", report.PrefixMutationSavedTokens)
	}
	fmt.Fprintf(w, "Reducible ceiling ratio:   %.2f%%  (realistic max S_local if every reducible tool-output -> 0)\n", report.ReducibleCeilingRatio*100)
	fmt.Fprintf(w, "Non-prefix upper bound:    %.2f%%  (absolute ceiling if even msgs/reasoning were reducible)\n", report.NonPrefixRatio*100)
	fmt.Fprintf(w, "Reducible ceiling deficit: %d  (target %.2f%% minus reducible mass)\n", report.ReducibleCeilingDeficit, report.TargetRatio*100)
	fmt.Fprintf(w, "Reducible headroom:        %d  (reducible minus already-saved)\n", report.ReducibleHeadroomTokens)
	fmt.Fprintf(w, "Provider cache read/cached: %d / %d  [separate, not S_local]\n", report.ProviderCacheReadTokens, report.ProviderCachedTokens)
	fmt.Fprintf(w, "Reasoning items (Class D):  %d\n", report.ReasoningItems)
	fmt.Fprintf(w, "\nVerdict: %s\n  %s\n", report.Verdict, report.VerdictDetail)
	fmt.Fprintf(w, "Headroom present:          %t\n", report.HeadroomPresent)
	fmt.Fprintf(w, "Gap inventory recommended: %t\n", report.GapInventoryRecommended)
	fmt.Fprintf(w, "Next action:               %s\n", report.NextAction)

	if len(report.Classes) > 0 {
		fmt.Fprintln(w, "\nPer request class:")
		for _, row := range report.Classes {
			fmt.Fprintf(w, "  %-13s requests=%d original=%d saved=%d %.2f%% | prefix=%d(%.2f%%) reducible=%d(%.2f%%) other=%d(%.2f%%) | ceiling=%.2f%% cached=%d sources=%s\n",
				row.Class,
				row.Requests,
				row.OriginalTokens,
				row.LocalSavedTokens,
				row.LocalSavingsRatio*100,
				row.PrefixProtectedTokens,
				row.PrefixProtectedShare*100,
				row.ReducibleToolOutputTokens,
				row.ReducibleToolOutputShare*100,
				row.OtherContextTokens,
				row.OtherContextShare*100,
				row.ReducibleCeilingRatio*100,
				row.ProviderCachedTokens,
				formatWSSAuditCounts(row.ShapeSources))
		}
	}

	if len(report.PerLog) > 0 {
		fmt.Fprintln(w, "\nPer capture (by reducible ceiling):")
		for _, row := range report.PerLog {
			fmt.Fprintf(w, "  %-48s phasef=%d original=%d saved=%d %.2f%% reducible=%d ceiling=%.2f%%\n",
				row.Name,
				row.PhaseFRequests,
				row.OriginalTokens,
				row.LocalSavedTokens,
				row.LocalSavingsRatio*100,
				row.ReducibleToolOutputTokens,
				row.ReducibleCeilingRatio*100)
		}
	}

	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
}

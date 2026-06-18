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
)

type wssLocalGapInventoryFlags struct {
	path          string
	outputFormat  string
	since         time.Time
	minLocalRatio float64
	help          bool
}

type wssLocalGapInventoryReport struct {
	Path              string                       `json:"path"`
	TargetRatio       float64                      `json:"target_ratio"`
	Logs              int                          `json:"logs"`
	PhaseFRequests    int                          `json:"phasef_requests"`
	OriginalTokens    int                          `json:"original_tokens"`
	LocalSavedTokens  int                          `json:"local_saved_tokens"`
	LocalSavingsRate  float64                      `json:"local_savings_ratio"`
	PolicyCeiling     int                          `json:"policy_savings_ceiling_tokens"`
	PolicyCeilingRate float64                      `json:"policy_savings_ceiling_ratio"`
	TargetDeficit     int                          `json:"target_deficit_tokens"`
	CeilingDeficit    int                          `json:"policy_savings_ceiling_deficit_tokens,omitempty"`
	RecoverableGap    int                          `json:"policy_recoverable_gap_tokens,omitempty"`
	GuardedPotential  int                          `json:"guarded_potential_tokens,omitempty"`
	UnattributedGap   int                          `json:"policy_unattributed_gap_tokens,omitempty"`
	Rows              []wssLocalGapInventoryRow    `json:"rows"`
	UnattributedRows  []wssLocalGapUnattributedRow `json:"unattributed_gap,omitempty"`
}

type wssLocalGapInventoryRow struct {
	Name                   string  `json:"name"`
	Path                   string  `json:"path"`
	PhaseFRequests         int     `json:"phasef_requests"`
	OriginalTokens         int     `json:"original_tokens"`
	LocalSavedTokens       int     `json:"local_saved_tokens"`
	LocalSavingsRate       float64 `json:"local_savings_ratio"`
	PolicyCeiling          int     `json:"policy_savings_ceiling_tokens"`
	PolicyCeilingRate      float64 `json:"policy_savings_ceiling_ratio"`
	PolicyProtectedTokens  int     `json:"policy_protected_tokens,omitempty"`
	PolicyKnownNonTarget   int     `json:"policy_known_non_target_tokens,omitempty"`
	TargetDeficit          int     `json:"target_deficit_tokens"`
	CeilingDeficit         int     `json:"policy_savings_ceiling_deficit_tokens,omitempty"`
	RecoverableGap         int     `json:"policy_recoverable_gap_tokens,omitempty"`
	GuardedPotential       int     `json:"guarded_potential_tokens,omitempty"`
	UnattributedGap        int     `json:"policy_unattributed_gap_tokens,omitempty"`
	NoEvidenceProtected    int     `json:"no_evidence_protected_original_tokens,omitempty"`
	NoEvidenceNeedsInstr   int     `json:"no_evidence_needs_instrumentation_original_tokens,omitempty"`
	NoEvidenceProofBlocked int     `json:"no_evidence_proof_blocked_or_candidate_original_tokens,omitempty"`
	UpstreamErrorRequests  int     `json:"upstream_error_requests,omitempty"`
	HTTP400ErrorRequests   int     `json:"http_400_error_requests,omitempty"`
	TopActionCategory      string  `json:"top_action_category,omitempty"`
	TopActionSource        string  `json:"top_action_source,omitempty"`
	TopActionTokens        int     `json:"top_action_tokens,omitempty"`
	TopNonPrefixCategory   string  `json:"top_nonprefix_action_category,omitempty"`
	TopNonPrefixSource     string  `json:"top_nonprefix_action_source,omitempty"`
	TopNonPrefixTokens     int     `json:"top_nonprefix_action_tokens,omitempty"`
}

const wssLocalGapInventoryHelpText = `wss-local-gap-inventory: summarize WSS local-gap policy ceilings across captures

Usage:
  go run ./scripts/utils wss-local-gap-inventory <dir-or-decisions.jsonl> [flags]

Flags:
  --since=<rfc3339>                 Ignore records before this timestamp
  --since-file=<path>               Read RFC3339 --since value from file
  --min-local-ratio=<ratio>          Target S_local ratio, default 0.48
  --json                            Output JSON

Directory mode scans recursively for decisions.jsonl and *.decisions.jsonl.
The report is content-free and uses the same single-log policy-ceiling logic as
wss-local-gap. Recoverable gap is policy_savings_ceiling_tokens minus observed
local_saved_tokens; guarded_potential is concrete full-pass evidence inside that
gap. Unattributed gap is ceiling mass without concrete guarded-token evidence.`

func runWSSLocalGapInventory(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSLocalGapInventoryFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssLocalGapInventoryHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-local-gap-inventory <dir-or-decisions.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSLocalGapInventory(flags)
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
		return 0
	}
	writeWSSLocalGapInventoryText(stdout, report)
	return 0
}

func parseWSSLocalGapInventoryFlags(args []string) (wssLocalGapInventoryFlags, error) {
	flags := wssLocalGapInventoryFlags{outputFormat: outputText}
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
				return flags, fmt.Errorf("multiple inventory roots provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func loadWSSLocalGapInventory(flags wssLocalGapInventoryFlags) (wssLocalGapInventoryReport, error) {
	paths, err := wssLocalGapInventoryPaths(flags.path)
	if err != nil {
		return wssLocalGapInventoryReport{}, err
	}
	if len(paths) == 0 {
		return wssLocalGapInventoryReport{}, fmt.Errorf("no decisions logs found under %s", flags.path)
	}
	targetRatio := flags.minLocalRatio
	if targetRatio == 0 {
		targetRatio = 0.48
	}
	report := wssLocalGapInventoryReport{
		Path:        flags.path,
		TargetRatio: targetRatio,
		Rows:        make([]wssLocalGapInventoryRow, 0, len(paths)),
	}
	unattributedRows := make(map[string]*wssLocalGapUnattributedRow)
	for _, path := range paths {
		gap, err := loadWSSLocalGapReport(wssLocalGapFlags{
			path:          path,
			since:         flags.since,
			minLocalRatio: targetRatio,
		})
		if err != nil {
			return wssLocalGapInventoryReport{}, err
		}
		guardedPotential := wssLocalGapTotalGuardedPotential(gap)
		recoverableGap := maxInt(0, gap.PolicySavingsCeiling-gap.LocalSavedTokens)
		row := wssLocalGapInventoryRow{
			Name:                   wssLocalGapInventoryName(path),
			Path:                   path,
			PhaseFRequests:         gap.PhaseFRequests,
			OriginalTokens:         gap.OriginalTokens,
			LocalSavedTokens:       gap.LocalSavedTokens,
			LocalSavingsRate:       gap.LocalSavingsRatio,
			PolicyCeiling:          gap.PolicySavingsCeiling,
			PolicyCeilingRate:      gap.PolicySavingsCeilingRate,
			PolicyProtectedTokens:  gap.PolicyProtectedTokens,
			PolicyKnownNonTarget:   gap.PolicyKnownNonTarget,
			TargetDeficit:          gap.TargetDeficitTokens,
			CeilingDeficit:         gap.PolicyCeilingDeficit,
			RecoverableGap:         recoverableGap,
			GuardedPotential:       guardedPotential,
			UnattributedGap:        gap.UnattributedGapTokens,
			NoEvidenceProtected:    gap.NoEvidenceProtected,
			NoEvidenceNeedsInstr:   gap.NoEvidenceNeedsInstr,
			NoEvidenceProofBlocked: gap.NoEvidenceProofBlocked,
			UpstreamErrorRequests:  gap.UpstreamErrorRequests,
			HTTP400ErrorRequests:   gap.HTTP400ErrorRequests,
		}
		if len(gap.ActionablePotential) > 0 {
			row.TopActionCategory = gap.ActionablePotential[0].Category
			row.TopActionSource = gap.ActionablePotential[0].Source
			row.TopActionTokens = gap.ActionablePotential[0].Tokens
		}
		if nonPrefix, ok := wssLocalGapTopNonPrefixAction(gap.ActionablePotential); ok {
			row.TopNonPrefixCategory = nonPrefix.Category
			row.TopNonPrefixSource = nonPrefix.Source
			row.TopNonPrefixTokens = nonPrefix.Tokens
		}
		report.Rows = append(report.Rows, row)
		report.Logs++
		report.PhaseFRequests += row.PhaseFRequests
		report.OriginalTokens += row.OriginalTokens
		report.LocalSavedTokens += row.LocalSavedTokens
		report.PolicyCeiling += row.PolicyCeiling
		report.RecoverableGap += row.RecoverableGap
		report.GuardedPotential += row.GuardedPotential
		report.UnattributedGap += row.UnattributedGap
		mergeWSSLocalGapUnattributedRows(unattributedRows, gap.UnattributedGap)
	}
	report.UnattributedRows = finalizeWSSLocalGapUnattributed(unattributedRows)
	report.LocalSavingsRate = wssLocalGapRatio(report.LocalSavedTokens, report.OriginalTokens)
	report.PolicyCeilingRate = wssLocalGapRatio(report.PolicyCeiling, report.OriginalTokens)
	targetSaved := targetSavedTokens(report.OriginalTokens, targetRatio)
	report.TargetDeficit = maxInt(0, targetSaved-report.LocalSavedTokens)
	report.CeilingDeficit = maxInt(0, targetSaved-report.PolicyCeiling)
	sort.Slice(report.Rows, func(i, j int) bool {
		if report.Rows[i].RecoverableGap != report.Rows[j].RecoverableGap {
			return report.Rows[i].RecoverableGap > report.Rows[j].RecoverableGap
		}
		if report.Rows[i].CeilingDeficit != report.Rows[j].CeilingDeficit {
			return report.Rows[i].CeilingDeficit > report.Rows[j].CeilingDeficit
		}
		return report.Rows[i].Name < report.Rows[j].Name
	})
	return report, nil
}

func wssLocalGapTopNonPrefixAction(rows []wssLocalGapActionableRow) (wssLocalGapActionableRow, bool) {
	for _, row := range rows {
		if row.Category != "prefix_capability_context_guarded" {
			return row, true
		}
	}
	return wssLocalGapActionableRow{}, false
}

func wssLocalGapInventoryPaths(root string) ([]string, error) {
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
		if base == "decisions.jsonl" || strings.HasSuffix(base, ".decisions.jsonl") {
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

func wssLocalGapInventoryName(path string) string {
	parent := filepath.Base(filepath.Dir(path))
	if parent != "." && parent != string(filepath.Separator) && parent != "" {
		return parent
	}
	return filepath.Base(path)
}

func mergeWSSLocalGapUnattributedRows(dst map[string]*wssLocalGapUnattributedRow, rows []wssLocalGapUnattributedRow) {
	for _, row := range rows {
		if row.Tokens <= 0 {
			continue
		}
		key := row.Category + "\x00" + row.Source + "\x00" + row.TokenBasis
		existing := dst[key]
		if existing == nil {
			copy := row
			dst[key] = &copy
			continue
		}
		mergeWSSLocalGapUnattributedRow(existing, row)
	}
}

func writeWSSLocalGapInventoryText(w io.Writer, report wssLocalGapInventoryReport) {
	fmt.Fprintf(w, "=== WSS Local Gap Inventory: %s ===\n", report.Path)
	fmt.Fprintf(w, "Logs / Phase-F requests:   %d / %d\n", report.Logs, report.PhaseFRequests)
	fmt.Fprintf(w, "S_local saved/ratio:       %d/%d / %.2f%%\n", report.LocalSavedTokens, report.OriginalTokens, report.LocalSavingsRate*100)
	fmt.Fprintf(w, "Policy ceiling/ratio:      %d/%d / %.2f%%\n", report.PolicyCeiling, report.OriginalTokens, report.PolicyCeilingRate*100)
	fmt.Fprintf(w, "Target/Ceiling/Recoverable deficits: %d / %d / %d\n", report.TargetDeficit, report.CeilingDeficit, report.RecoverableGap)
	fmt.Fprintf(w, "Guarded/Unattributed recoverable gap: %d / %d\n", report.GuardedPotential, report.UnattributedGap)
	if len(report.Rows) == 0 {
		return
	}
	fmt.Fprintln(w, "\nRows:")
	for _, row := range report.Rows {
		fmt.Fprintf(w, "  %-48s phasef=%d local=%d/%d %.2f%% ceiling=%d %.2f%% recoverable=%d guarded=%d unattributed=%d target_gap=%d ceiling_gap=%d protected=%d top=%s:%d next=%s:%d\n",
			row.Name,
			row.PhaseFRequests,
			row.LocalSavedTokens,
			row.OriginalTokens,
			row.LocalSavingsRate*100,
			row.PolicyCeiling,
			row.PolicyCeilingRate*100,
			row.RecoverableGap,
			row.GuardedPotential,
			row.UnattributedGap,
			row.TargetDeficit,
			row.CeilingDeficit,
			row.PolicyProtectedTokens,
			emptyDash(row.TopActionCategory),
			row.TopActionTokens,
			emptyDash(row.TopNonPrefixCategory),
			row.TopNonPrefixTokens)
	}
	if len(report.UnattributedRows) > 0 {
		fmt.Fprintln(w, "\nUnattributed gap:")
		for _, row := range report.UnattributedRows {
			fmt.Fprintf(w, "  %-56s source=%-48s tokens=%d requests=%d ceiling=%d protected=%d saved=%d guarded=%d shapes=%s mechanisms=%s reasons=%s\n",
				row.Category,
				row.Source,
				row.Tokens,
				row.Requests,
				row.PolicyCeilingTokens,
				row.PolicyProtectedTokens,
				row.LocalSavedTokens,
				row.GuardedPotential,
				formatWSSAuditCounts(row.RequestShapes),
				formatWSSAuditCounts(row.Mechanisms),
				formatWSSAuditCounts(row.Reasons))
			fmt.Fprintf(w, "    next: %s\n", row.NextStep)
		}
	}
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type wssProofPackFlags struct {
	path                    string
	outputFormat            string
	since                   time.Time
	sinceFile               string
	socketsJSON             string
	auditJSON               string
	minLocalRatio           float64
	allowStale              bool
	requireHeadroom         bool
	requireAcceptedContract bool
	help                    bool
}

type wssProofPackReport struct {
	Path                string                       `json:"path"`
	Since               *time.Time                   `json:"since,omitempty"`
	SinceFile           string                       `json:"since_file,omitempty"`
	TargetRatio         float64                      `json:"target_ratio"`
	GatePassed          bool                         `json:"gate_passed"`
	GateFailures        []string                     `json:"gate_failures,omitempty"`
	SocketCommand       string                       `json:"socket_command"`
	SocketsJSON         string                       `json:"sockets_json,omitempty"`
	AuditCommand        string                       `json:"audit_command"`
	AuditJSON           string                       `json:"audit_json,omitempty"`
	ClassCommand        string                       `json:"class_distribution_command"`
	LocalGapCommand     string                       `json:"local_gap_command"`
	ReferenceCommand    string                       `json:"reference_inventory_command"`
	SocketSummary       *wssProofPackSocketSummary   `json:"socket_summary,omitempty"`
	AuditSummary        *wssProofPackAuditSummary    `json:"audit_summary,omitempty"`
	ClassDistribution   wssProofPackClassSummary     `json:"class_distribution"`
	LocalGap            wssProofPackLocalGapSummary  `json:"local_gap"`
	ReferenceInventory  wssProofPackReferenceSummary `json:"reference_inventory"`
	TopActionable       []wssProofPackActionable     `json:"top_actionable,omitempty"`
	RootContextLedger   []wssLocalGapRootContextRow  `json:"root_context_ledger,omitempty"`
	RecommendedNextStep string                       `json:"recommended_next_step"`
	ProofDecision       string                       `json:"proof_decision"`
	Notes               []string                     `json:"notes,omitempty"`
}

type wssProofPackClassSummary struct {
	Verdict                   string  `json:"verdict"`
	HeadroomPresent           bool    `json:"headroom_present"`
	GapInventoryRecommended   bool    `json:"gap_inventory_recommended"`
	PhaseFRequests            int     `json:"phasef_requests"`
	OriginalTokens            int     `json:"original_tokens"`
	LocalSavedTokens          int     `json:"local_saved_tokens"`
	LocalSavingsRatio         float64 `json:"local_savings_ratio"`
	ReducibleCeilingRatio     float64 `json:"reducible_ceiling_ratio"`
	ReducibleHeadroomTokens   int     `json:"reducible_headroom_tokens,omitempty"`
	FullHistoryRequests       int     `json:"full_history_requests,omitempty"`
	FullHistoryOriginalTokens int     `json:"full_history_original_tokens,omitempty"`
	FullHistoryLocalRatio     float64 `json:"full_history_local_savings_ratio,omitempty"`
	NextAction                string  `json:"next_action"`
}

type wssProofPackLocalGapSummary struct {
	PhaseFRequests             int     `json:"phasef_requests"`
	OriginalTokens             int     `json:"original_tokens"`
	LocalSavedTokens           int     `json:"local_saved_tokens"`
	LocalSavingsRatio          float64 `json:"local_savings_ratio"`
	PolicySavingsCeiling       int     `json:"policy_savings_ceiling_tokens"`
	PolicySavingsCeilingRatio  float64 `json:"policy_savings_ceiling_ratio"`
	PolicyProtectedTokens      int     `json:"policy_protected_tokens,omitempty"`
	InstrumentedRequests       int     `json:"instrumented_requests,omitempty"`
	InstrumentedOriginalTokens int     `json:"instrumented_original_tokens,omitempty"`
	MissingInstrRequests       int     `json:"missing_instrumentation_requests,omitempty"`
	MissingInstrOriginalTokens int     `json:"missing_instrumentation_original_tokens,omitempty"`
	UnattributedGapTokens      int     `json:"policy_unattributed_gap_tokens,omitempty"`
	NoEvidenceNeedsInstr       int     `json:"no_evidence_needs_instrumentation_original_tokens,omitempty"`
	NoEvidenceProtected        int     `json:"no_evidence_protected_original_tokens,omitempty"`
	NoEvidenceProofBlocked     int     `json:"no_evidence_proof_blocked_or_candidate_original_tokens,omitempty"`
	UpstreamErrorRequests      int     `json:"upstream_error_requests,omitempty"`
	HTTP400ErrorRequests       int     `json:"http_400_error_requests,omitempty"`
}

type wssProofPackReferenceSummary struct {
	Verdict                 string `json:"verdict"`
	Files                   int    `json:"files"`
	JSONRows                int    `json:"json_rows"`
	ParseErrors             int    `json:"parse_errors"`
	Lane3AcceptedContracts  int    `json:"lane3_accepted_contracts"`
	ArbitraryCandidateKinds int    `json:"arbitrary_candidate_kinds,omitempty"`
	LocalReferenceURIKinds  int    `json:"local_reference_uri_kinds,omitempty"`
}

type wssProofPackActionable struct {
	Category string `json:"category"`
	Source   string `json:"source"`
	Tokens   int    `json:"tokens"`
	Basis    string `json:"basis"`
}

type wssProofPackSocketSummary struct {
	SocketCount                             int            `json:"socket_count"`
	ActionableSockets                       int            `json:"actionable_sockets"`
	ProviderInputTokens                     int            `json:"provider_input_tokens"`
	ProviderCachedTokens                    int            `json:"provider_cached_tokens"`
	LocalSavedTokens                        int            `json:"local_saved_tokens"`
	FullHistoryRequests                     int            `json:"full_history_requests"`
	FullHistoryProviderInputTokens          int            `json:"full_history_provider_input_tokens"`
	ReconnectFullHistoryRequests            int            `json:"reconnect_full_history_requests"`
	ReconnectFullHistoryProviderInputTokens int            `json:"reconnect_full_history_provider_input_tokens"`
	T417ReconnectHandoffRows                int            `json:"t417_reconnect_handoff_rows"`
	T420ReconnectHandoffRows                int            `json:"t420_reconnect_handoff_rows,omitempty"`
	TopReconnectCause                       string         `json:"top_reconnect_cause,omitempty"`
	TopReconnectCauseInputTokens            int            `json:"top_reconnect_cause_input_tokens,omitempty"`
	TopReconnectCauseRetryResendCostTokens  int            `json:"top_reconnect_cause_retry_resend_cost_tokens,omitempty"`
	ContinuationCandidates                  map[string]int `json:"continuation_candidates,omitempty"`
	CauseClasses                            map[string]int `json:"cause_classes,omitempty"`
	CloseInitiators                         map[string]int `json:"close_initiators,omitempty"`
}

type wssProofPackAuditSummary struct {
	Requests                             int                          `json:"requests"`
	PhaseFRequests                       int                          `json:"phasef_requests"`
	ShadowMirrorRequests                 int                          `json:"shadow_mirror_requests,omitempty"`
	ShadowMirrorReferenceableBytes       int                          `json:"shadow_mirror_referenceable_bytes,omitempty"`
	ShadowMirrorNormalizedReferenceBytes int                          `json:"shadow_mirror_normalized_referenceable_bytes,omitempty"`
	TopCandidates                        []wssProofPackAuditCandidate `json:"top_candidates,omitempty"`
}

type wssProofPackAuditCandidate struct {
	RequestShape                   string         `json:"request_shape"`
	Kind                           string         `json:"kind"`
	Requests                       int            `json:"requests"`
	CandidateLane                  string         `json:"candidate_lane"`
	NextProofGate                  string         `json:"next_proof_gate"`
	PromotionStage                 string         `json:"promotion_stage"`
	CandidateLocalTokensEstimate   int            `json:"candidate_local_tokens_estimate"`
	IncrementalLocalTokensHeadroom int            `json:"incremental_local_tokens_headroom"`
	PromotionOpenReady             bool           `json:"promotion_open_ready,omitempty"`
	PromotionOpenHeadroom          int            `json:"promotion_open_headroom,omitempty"`
	PromotionOpenStage             string         `json:"promotion_open_stage,omitempty"`
	PromotionOpenBlockers          []string       `json:"promotion_open_blockers,omitempty"`
	PromotionOpenBlockerHeadroom   map[string]int `json:"promotion_open_blocker_headroom_tokens,omitempty"`
	ErrorFree                      bool           `json:"error_free"`
	RecommendedAction              string         `json:"recommended_action"`
}

const wssProofPackHelpText = `wss-proof-pack: content-free WSS proof-window gate for T417/T420/T408

Usage:
  go run ./scripts/utils wss-proof-pack <dir-or-decisions.jsonl> [flags]

Flags:
  --since=<rfc3339>                  Ignore records before this timestamp
  --since-file=<path>                Read RFC3339 --since value from file
  --sockets-json=<path>               Ingest slimference debug wss-sockets --json output
  --audit-json=<path>                 Ingest wss-audit --json shadow-mirror candidate output
  --min-local-ratio=<ratio>           Owner S_local target, default 0.48
  --require-headroom                  Fail unless class-distribution reports headroom
  --require-accepted-contract         Fail unless reference inventory has an accepted Lane 3 backend contract
  --allow-stale                       Do not fail on missing current WSS ownership instrumentation
  --json                             Output JSON

The pack combines wss-class-distribution, wss-local-gap with current
instrumentation requirements, wss-reference-inventory, and optional
wss-sockets plus wss-audit JSON. It prints the matching slimference debug
wss-sockets and wss-audit commands when optional JSON has not been captured yet.
The report is content-free: it carries counts, byte/token estimates, verdicts,
gates, and commands, never prompt or tool-output payloads.`

func runWSSProofPack(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSProofPackFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssProofPackHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-proof-pack <dir-or-decisions.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSProofPack(flags)
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
		writeWSSProofPackText(stdout, report)
	}
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseWSSProofPackFlags(args []string) (wssProofPackFlags, error) {
	flags := wssProofPackFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--allow-stale":
			flags.allowStale = true
		case arg == "--require-headroom":
			flags.requireHeadroom = true
		case arg == "--require-accepted-contract":
			flags.requireAcceptedContract = true
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
			flags.sinceFile = value
		case strings.HasPrefix(arg, "--since-file="):
			value := strings.TrimPrefix(arg, "--since-file=")
			since, err := parseWSSSinceFile(value)
			if err != nil {
				return flags, err
			}
			flags.since = since
			flags.sinceFile = value
		case arg == "--sockets-json":
			value, err := aggregateFlagValue(args, &i, arg)
			if err != nil {
				return flags, err
			}
			flags.socketsJSON = value
		case strings.HasPrefix(arg, "--sockets-json="):
			flags.socketsJSON = strings.TrimPrefix(arg, "--sockets-json=")
		case arg == "--audit-json":
			value, err := aggregateFlagValue(args, &i, arg)
			if err != nil {
				return flags, err
			}
			flags.auditJSON = value
		case strings.HasPrefix(arg, "--audit-json="):
			flags.auditJSON = strings.TrimPrefix(arg, "--audit-json=")
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
				return flags, fmt.Errorf("multiple proof-pack roots provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func loadWSSProofPack(flags wssProofPackFlags) (wssProofPackReport, error) {
	targetRatio := flags.minLocalRatio
	if targetRatio == 0 {
		targetRatio = 0.48
	}
	classReport, err := loadWSSClassDistribution(wssClassDistributionFlags{
		path:          flags.path,
		since:         flags.since,
		minLocalRatio: targetRatio,
	})
	if err != nil {
		return wssProofPackReport{}, err
	}
	localGap, err := loadWSSLocalGapReport(wssLocalGapFlags{
		path:                flags.path,
		since:               flags.since,
		sinceFile:           flags.sinceFile,
		minLocalRatio:       targetRatio,
		requireInstrumented: !flags.allowStale,
	})
	if err != nil {
		return wssProofPackReport{}, err
	}
	referenceReport, err := loadWSSReferenceInventory(flags.path)
	if err != nil {
		return wssProofPackReport{}, err
	}
	var socketSummary *wssProofPackSocketSummary
	if flags.socketsJSON != "" {
		summary, err := loadWSSProofPackSocketSummary(flags.socketsJSON)
		if err != nil {
			return wssProofPackReport{}, err
		}
		socketSummary = &summary
	}
	var auditSummary *wssProofPackAuditSummary
	if flags.auditJSON != "" {
		summary, err := loadWSSProofPackAuditSummary(flags.auditJSON)
		if err != nil {
			return wssProofPackReport{}, err
		}
		auditSummary = &summary
	}
	report := wssProofPackReport{
		Path:               flags.path,
		SinceFile:          flags.sinceFile,
		TargetRatio:        targetRatio,
		SocketCommand:      wssProofPackSocketCommand(flags, targetRatio),
		SocketsJSON:        flags.socketsJSON,
		AuditCommand:       wssProofPackAuditCommand(flags),
		AuditJSON:          flags.auditJSON,
		ClassCommand:       wssProofPackClassCommand(flags, targetRatio),
		LocalGapCommand:    wssProofPackLocalGapCommand(flags, targetRatio),
		ReferenceCommand:   wssProofPackReferenceCommand(flags),
		SocketSummary:      socketSummary,
		AuditSummary:       auditSummary,
		ClassDistribution:  wssProofPackClassSummaryFromReport(classReport),
		LocalGap:           wssProofPackLocalGapSummaryFromReport(localGap),
		ReferenceInventory: wssProofPackReferenceSummaryFromReport(referenceReport),
		TopActionable:      wssProofPackTopActionable(localGap.ActionablePotential, 5),
		RootContextLedger:  localGap.RootContextLedger,
	}
	if !flags.since.IsZero() {
		since := flags.since
		report.Since = &since
	}
	report.GateFailures = wssProofPackGateFailures(flags, socketSummary, classReport, localGap, referenceReport)
	report.GatePassed = len(report.GateFailures) == 0
	report.ProofDecision, report.RecommendedNextStep = wssProofPackDecision(flags, socketSummary, auditSummary, classReport, localGap, referenceReport)
	report.Notes = wssProofPackNotes(socketSummary, auditSummary, classReport, localGap, referenceReport)
	return report, nil
}

func loadWSSProofPackSocketSummary(path string) (wssProofPackSocketSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return wssProofPackSocketSummary{}, fmt.Errorf("read sockets JSON %s: %w", path, err)
	}
	var raw struct {
		SocketCount                             int                            `json:"socket_count"`
		ActionableSockets                       int                            `json:"actionable_sockets"`
		ProviderInputTokens                     int                            `json:"provider_input_tokens"`
		ProviderCachedTokens                    int                            `json:"provider_cached_tokens"`
		LocalSavedTokens                        int                            `json:"local_saved_tokens"`
		FullHistoryRequests                     int                            `json:"full_history_requests"`
		FullHistoryProviderInputTokens          int                            `json:"full_history_provider_input_tokens"`
		ReconnectFullHistoryRequests            int                            `json:"reconnect_full_history_requests"`
		ReconnectFullHistoryProviderInputTokens int                            `json:"reconnect_full_history_provider_input_tokens"`
		ReconnectFullHistoryByCause             []wssProofPackReconnectCause   `json:"reconnect_full_history_by_cause"`
		T417ReconnectHandoff                    []wssProofPackReconnectHandoff `json:"t417_reconnect_handoff"`
		T420ReconnectHandoff                    []wssProofPackReconnectHandoff `json:"t420_reconnect_handoff"`
		CauseClasses                            map[string]int                 `json:"cause_classes"`
		CloseInitiators                         map[string]int                 `json:"close_initiators"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return wssProofPackSocketSummary{}, fmt.Errorf("decode sockets JSON %s: %w", path, err)
	}
	summary := wssProofPackSocketSummary{
		SocketCount:                             raw.SocketCount,
		ActionableSockets:                       raw.ActionableSockets,
		ProviderInputTokens:                     raw.ProviderInputTokens,
		ProviderCachedTokens:                    raw.ProviderCachedTokens,
		LocalSavedTokens:                        raw.LocalSavedTokens,
		FullHistoryRequests:                     raw.FullHistoryRequests,
		FullHistoryProviderInputTokens:          raw.FullHistoryProviderInputTokens,
		ReconnectFullHistoryRequests:            raw.ReconnectFullHistoryRequests,
		ReconnectFullHistoryProviderInputTokens: raw.ReconnectFullHistoryProviderInputTokens,
		T417ReconnectHandoffRows:                len(raw.T417ReconnectHandoff),
		T420ReconnectHandoffRows:                len(raw.T420ReconnectHandoff),
		CauseClasses:                            cloneIntMap(raw.CauseClasses),
		CloseInitiators:                         cloneIntMap(raw.CloseInitiators),
	}
	for _, row := range raw.ReconnectFullHistoryByCause {
		if row.ProviderInputTokens > summary.TopReconnectCauseInputTokens {
			summary.TopReconnectCause = strings.TrimSpace(row.Cause)
			summary.TopReconnectCauseInputTokens = row.ProviderInputTokens
			summary.TopReconnectCauseRetryResendCostTokens = row.RetryResendCost
		}
	}
	for _, row := range raw.T417ReconnectHandoff {
		candidate := strings.TrimSpace(row.ContinuationCandidate)
		if candidate == "" {
			continue
		}
		if summary.ContinuationCandidates == nil {
			summary.ContinuationCandidates = make(map[string]int)
		}
		summary.ContinuationCandidates[candidate]++
	}
	for _, row := range raw.T420ReconnectHandoff {
		candidate := strings.TrimSpace(row.ContinuationCandidate)
		if candidate == "" {
			continue
		}
		if summary.ContinuationCandidates == nil {
			summary.ContinuationCandidates = make(map[string]int)
		}
		summary.ContinuationCandidates[candidate]++
	}
	return summary, nil
}

func loadWSSProofPackAuditSummary(path string) (wssProofPackAuditSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return wssProofPackAuditSummary{}, fmt.Errorf("read audit JSON %s: %w", path, err)
	}
	var raw struct {
		Requests               int                        `json:"requests"`
		PhaseFRequests         int                        `json:"phasef_requests"`
		ShadowMirror           *wssShadowMirrorSummary    `json:"shadow_mirror"`
		ShadowMirrorCandidates []wssShadowMirrorCandidate `json:"shadow_mirror_candidates"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return wssProofPackAuditSummary{}, fmt.Errorf("decode audit JSON %s: %w", path, err)
	}
	summary := wssProofPackAuditSummary{
		Requests:       raw.Requests,
		PhaseFRequests: raw.PhaseFRequests,
	}
	if raw.ShadowMirror != nil {
		summary.ShadowMirrorRequests = raw.ShadowMirror.Requests
		summary.ShadowMirrorReferenceableBytes = raw.ShadowMirror.ReferenceableBytes
		summary.ShadowMirrorNormalizedReferenceBytes = raw.ShadowMirror.NormalizedReferenceableBytes
	}
	for i, row := range raw.ShadowMirrorCandidates {
		if i >= 5 {
			break
		}
		summary.TopCandidates = append(summary.TopCandidates, wssProofPackAuditCandidate{
			RequestShape:                   row.RequestShape,
			Kind:                           row.Kind,
			Requests:                       row.Requests,
			CandidateLane:                  row.CandidateLane,
			NextProofGate:                  row.NextProofGate,
			PromotionStage:                 row.PromotionStage,
			CandidateLocalTokensEstimate:   row.CandidateLocalTokensEstimate,
			IncrementalLocalTokensHeadroom: row.IncrementalLocalTokensHeadroom,
			PromotionOpenReady:             row.PromotionOpenReady,
			PromotionOpenHeadroom:          row.PromotionOpenHeadroom,
			PromotionOpenStage:             row.PromotionOpenStage,
			PromotionOpenBlockers:          append([]string(nil), row.PromotionOpenBlockers...),
			PromotionOpenBlockerHeadroom:   cloneIntMap(row.PromotionOpenBlockerHeadroom),
			ErrorFree:                      row.ErrorFree,
			RecommendedAction:              row.RecommendedAction,
		})
	}
	return summary, nil
}

type wssProofPackReconnectCause struct {
	Cause               string `json:"cause"`
	ProviderInputTokens int    `json:"provider_input_tokens"`
	RetryResendCost     int    `json:"retry_resend_cost_tokens"`
}

type wssProofPackReconnectHandoff struct {
	ContinuationCandidate string `json:"continuation_candidate"`
}

func wssProofPackClassSummaryFromReport(report wssClassDistributionReport) wssProofPackClassSummary {
	fullHistory := wssProofPackClassRow(report.Classes, wssClassDistributionClassFullHistory)
	return wssProofPackClassSummary{
		Verdict:                   report.Verdict,
		HeadroomPresent:           report.HeadroomPresent,
		GapInventoryRecommended:   report.GapInventoryRecommended,
		PhaseFRequests:            report.PhaseFRequests,
		OriginalTokens:            report.OriginalTokens,
		LocalSavedTokens:          report.LocalSavedTokens,
		LocalSavingsRatio:         report.LocalSavingsRatio,
		ReducibleCeilingRatio:     report.ReducibleCeilingRatio,
		ReducibleHeadroomTokens:   report.ReducibleHeadroomTokens,
		FullHistoryRequests:       fullHistory.Requests,
		FullHistoryOriginalTokens: fullHistory.OriginalTokens,
		FullHistoryLocalRatio:     fullHistory.LocalSavingsRatio,
		NextAction:                report.NextAction,
	}
}

func wssProofPackClassRow(rows []wssClassDistributionClassRow, name string) wssClassDistributionClassRow {
	for _, row := range rows {
		if row.Class == name {
			return row
		}
	}
	return wssClassDistributionClassRow{}
}

func wssProofPackLocalGapSummaryFromReport(report wssLocalGapReport) wssProofPackLocalGapSummary {
	return wssProofPackLocalGapSummary{
		PhaseFRequests:             report.PhaseFRequests,
		OriginalTokens:             report.OriginalTokens,
		LocalSavedTokens:           report.LocalSavedTokens,
		LocalSavingsRatio:          report.LocalSavingsRatio,
		PolicySavingsCeiling:       report.PolicySavingsCeiling,
		PolicySavingsCeilingRatio:  report.PolicySavingsCeilingRate,
		PolicyProtectedTokens:      report.PolicyProtectedTokens,
		InstrumentedRequests:       report.InstrumentedRequests,
		InstrumentedOriginalTokens: report.InstrumentedOrigTokens,
		MissingInstrRequests:       report.MissingInstrRequests,
		MissingInstrOriginalTokens: report.MissingInstrOrigTokens,
		UnattributedGapTokens:      report.UnattributedGapTokens,
		NoEvidenceNeedsInstr:       report.NoEvidenceNeedsInstr,
		NoEvidenceProtected:        report.NoEvidenceProtected,
		NoEvidenceProofBlocked:     report.NoEvidenceProofBlocked,
		UpstreamErrorRequests:      report.UpstreamErrorRequests,
		HTTP400ErrorRequests:       report.HTTP400ErrorRequests,
	}
}

func wssProofPackReferenceSummaryFromReport(report wssReferenceInventoryReport) wssProofPackReferenceSummary {
	return wssProofPackReferenceSummary{
		Verdict:                 report.Verdict,
		Files:                   report.Files,
		JSONRows:                report.JSONRows,
		ParseErrors:             report.ParseErrors,
		Lane3AcceptedContracts:  report.Lane3AcceptedContracts,
		ArbitraryCandidateKinds: len(report.ArbitraryCandidates),
		LocalReferenceURIKinds:  len(report.LocalReferenceURIs),
	}
}

func wssProofPackTopActionable(rows []wssLocalGapActionableRow, limit int) []wssProofPackActionable {
	if limit <= 0 || len(rows) == 0 {
		return nil
	}
	capacity := min(len(rows), limit)
	out := make([]wssProofPackActionable, 0, capacity)
	for i, row := range rows {
		if i >= limit {
			break
		}
		out = append(out, wssProofPackActionable{
			Category: row.Category,
			Source:   row.Source,
			Tokens:   row.Tokens,
			Basis:    row.TokenBasis,
		})
	}
	return out
}

func wssProofPackGateFailures(flags wssProofPackFlags, socketSummary *wssProofPackSocketSummary, classReport wssClassDistributionReport, localGap wssLocalGapReport, referenceReport wssReferenceInventoryReport) []string {
	var failures []string
	if !flags.allowStale && localGap.MissingInstrRequests > 0 {
		failures = append(failures, fmt.Sprintf("missing_instrumentation_requests=%d original_tokens=%d", localGap.MissingInstrRequests, localGap.MissingInstrOrigTokens))
	}
	if socketSummary != nil && socketSummary.ReconnectFullHistoryRequests > 0 && socketSummary.T417ReconnectHandoffRows+socketSummary.T420ReconnectHandoffRows == 0 {
		failures = append(failures, fmt.Sprintf("reconnect_full_history_without_handoff requests=%d input_tokens=%d", socketSummary.ReconnectFullHistoryRequests, socketSummary.ReconnectFullHistoryProviderInputTokens))
	}
	if flags.requireHeadroom && !classReport.HeadroomPresent {
		failures = append(failures, "headroom_not_present:"+classReport.Verdict)
	}
	if flags.requireAcceptedContract && referenceReport.Lane3AcceptedContracts == 0 {
		failures = append(failures, "lane3_backend_reference_contract_not_accepted")
	}
	if localGap.UpstreamErrorRequests > 0 || localGap.HTTP400ErrorRequests > 0 {
		failures = append(failures, fmt.Sprintf("upstream_or_400_errors_present upstream=%d http400=%d", localGap.UpstreamErrorRequests, localGap.HTTP400ErrorRequests))
	}
	return failures
}

func wssProofPackDecision(flags wssProofPackFlags, socketSummary *wssProofPackSocketSummary, auditSummary *wssProofPackAuditSummary, classReport wssClassDistributionReport, localGap wssLocalGapReport, referenceReport wssReferenceInventoryReport) (string, string) {
	switch {
	case !flags.allowStale && localGap.MissingInstrRequests > 0:
		return "capture_fresh_instrumented_window",
			"rerun with a since marker after current instrumentation; do not promote T417/T408 from stale rows"
	case localGap.UpstreamErrorRequests > 0 || localGap.HTTP400ErrorRequests > 0:
		return "stability_first",
			"classify upstream/400 errors before widening any savings mechanism"
	case socketSummary != nil && socketSummary.ReconnectFullHistoryRequests > 0 && socketSummary.T417ReconnectHandoffRows+socketSummary.T420ReconnectHandoffRows > 0:
		return "t420_reconnect_handoff_present",
			"feed the same sockets JSON into wss-t354-shape-proof --t420-handoff-json and choose T420 transport fix or T417 reroute by exact cause"
	case socketSummary != nil && socketSummary.ReconnectFullHistoryRequests > 0:
		return "t420_reconnect_handoff_missing",
			"regenerate slimference debug wss-sockets with handoff-capable current binary before engineering reconnect fixes"
	case socketSummary != nil && socketSummary.FullHistoryRequests > 0:
		return "class_b_socket_mass_present",
			"use socket summary plus class distribution to rank T417 Class-B/server-state candidates"
	case wssProofPackAuditTopCandidate(auditSummary, "t417") != nil:
		return "t417_productizable_headroom_present",
			"feed the same audit JSON into wss-t354-shape-proof --t408-open-slice-json and productize only the exact open Class-B slice"
	case wssProofPackAuditTopCandidate(auditSummary, "t408_backend_reference_contract") != nil:
		return "t408_reference_contract_headroom_present",
			"run the backend-reference acceptance/rehydrate contract path for the top audit candidate before touching parser micro-work"
	case wssProofPackAuditTopCandidate(auditSummary, "t408_reference_or_t418_parser_recovery") != nil:
		return "t408_or_t418_parser_recovery_headroom_present",
			"choose backend references or the largest parser/recovery-backed T418 slice from the audit candidate; do not loosen broad WSS guards"
	case referenceReport.Lane3AcceptedContracts > 0:
		return "t408_reference_productization_candidate",
			"implement only the accepted backend-reference slice with rehydrate fallback and exact demotion"
	case classReport.HeadroomPresent:
		return "headroom_present",
			"rank local-gap top actionable rows and patch the largest exact zero-drawdown blocker"
	case classReport.PhaseFRequests == 0:
		return "no_wss_phasef_data",
			"capture WSS Phase-F traffic before evaluating T417/T420/T408"
	case wssProofPackClassRow(classReport.Classes, wssClassDistributionClassFullHistory).Requests > 0:
		return "class_b_present_but_capped",
			"use socket/reconnect handoff and T417 net-economics before touching broad guards"
	default:
		return "corpus_ceiling_or_no_mass",
			"do not micro-optimize; capture Class-B/reconnect or command-output-heavy traffic"
	}
}

func wssProofPackAuditTopCandidate(summary *wssProofPackAuditSummary, lanePrefix string) *wssProofPackAuditCandidate {
	if summary == nil {
		return nil
	}
	for i := range summary.TopCandidates {
		row := &summary.TopCandidates[i]
		if row.IncrementalLocalTokensHeadroom <= 0 {
			continue
		}
		if !row.ErrorFree {
			continue
		}
		if lanePrefix == "t417" {
			if strings.HasPrefix(row.CandidateLane, "t417") || row.PromotionOpenReady {
				return row
			}
			continue
		}
		if row.CandidateLane == lanePrefix {
			return row
		}
	}
	return nil
}

func wssProofPackNotes(socketSummary *wssProofPackSocketSummary, auditSummary *wssProofPackAuditSummary, classReport wssClassDistributionReport, localGap wssLocalGapReport, referenceReport wssReferenceInventoryReport) []string {
	var notes []string
	notes = append(notes, "Provider-cache discount is not counted as S_local.")
	if socketSummary == nil {
		notes = append(notes, "Socket/reconnect classification is command-only until --sockets-json is provided.")
	} else if socketSummary.ReconnectFullHistoryRequests > 0 {
		notes = append(notes, "Reconnect full-history mass preempts parser micro-work; choose T420 transport or T417 reroute from exact handoff rows.")
	}
	if auditSummary == nil {
		notes = append(notes, "Shadow-mirror and parser/recovery headroom classification is absent until --audit-json is provided.")
	} else if len(auditSummary.TopCandidates) > 0 {
		notes = append(notes, "Audit headroom is advisory for ranking; product activation still requires the candidate's exact recovery/reference contract gate.")
	}
	if localGap.MissingInstrRequests > 0 {
		notes = append(notes, "Stale or incomplete ownership facts are a hard proof blocker for broad T417/T408 promotion.")
	}
	if !classReport.HeadroomPresent {
		notes = append(notes, "Class distribution does not currently justify broad guard loosening.")
	}
	if referenceReport.Lane3AcceptedContracts == 0 {
		notes = append(notes, "No accepted Lane 3 backend-reference contract is present in this slice.")
	}
	if localGap.PolicyProtectedTokens > 0 {
		notes = append(notes, "Protected prefix/context mass must stay byte-identical unless a separate zero-drawdown ownership mechanism exists.")
	}
	return notes
}

func wssProofPackSocketCommand(flags wssProofPackFlags, targetRatio float64) string {
	args := []string{"slimference debug wss-sockets 200"}
	if flags.sinceFile != "" {
		args = append(args, "--since-file="+shellQuote(flags.sinceFile))
	} else if !flags.since.IsZero() {
		args = append(args, "--since="+flags.since.Format(time.RFC3339))
	}
	args = append(args, "--json")
	return strings.Join(args, " ")
}

func wssProofPackClassCommand(flags wssProofPackFlags, targetRatio float64) string {
	args := []string{"go run ./scripts/utils wss-class-distribution", shellQuote(flags.path)}
	if flags.sinceFile != "" {
		args = append(args, "--since-file="+shellQuote(flags.sinceFile))
	} else if !flags.since.IsZero() {
		args = append(args, "--since="+flags.since.Format(time.RFC3339))
	}
	args = append(args, fmt.Sprintf("--min-local-ratio=%.6g", targetRatio), "--json")
	return strings.Join(args, " ")
}

func wssProofPackLocalGapCommand(flags wssProofPackFlags, targetRatio float64) string {
	args := []string{"go run ./scripts/utils wss-local-gap", shellQuote(flags.path)}
	if flags.sinceFile != "" {
		args = append(args, "--since-file="+shellQuote(flags.sinceFile))
	} else if !flags.since.IsZero() {
		args = append(args, "--since="+flags.since.Format(time.RFC3339))
	}
	args = append(args, fmt.Sprintf("--min-local-ratio=%.6g", targetRatio))
	if !flags.allowStale {
		args = append(args, "--require-instrumented")
	}
	args = append(args, "--json")
	return strings.Join(args, " ")
}

func wssProofPackReferenceCommand(flags wssProofPackFlags) string {
	args := []string{"go run ./scripts/utils wss-reference-inventory", shellQuote(flags.path)}
	if flags.requireAcceptedContract {
		args = append(args, "--require-accepted-contract")
	}
	args = append(args, "--json")
	return strings.Join(args, " ")
}

func wssProofPackAuditCommand(flags wssProofPackFlags) string {
	args := []string{"go run ./scripts/utils wss-audit", shellQuote(flags.path)}
	if flags.sinceFile != "" {
		args = append(args, "--since-file="+shellQuote(flags.sinceFile))
	} else if !flags.since.IsZero() {
		args = append(args, "--since="+flags.since.Format(time.RFC3339))
	}
	args = append(args, "--json")
	return strings.Join(args, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '_' || r == '-' || r == ':' || r == '=' ||
			(r >= '0' && r <= '9') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z'))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeWSSProofPackText(w io.Writer, report wssProofPackReport) {
	fmt.Fprintf(w, "=== WSS Proof Pack: %s ===\n", report.Path)
	if report.Since != nil {
		fmt.Fprintf(w, "Since:                %s\n", report.Since.Format(time.RFC3339))
	}
	if report.SinceFile != "" {
		fmt.Fprintf(w, "Since file:           %s\n", report.SinceFile)
	}
	fmt.Fprintf(w, "Target S_local:       %.2f%%\n", report.TargetRatio*100)
	fmt.Fprintf(w, "Gate:                 %s\n", passFail(report.GatePassed))
	fmt.Fprintf(w, "Decision:             %s\n", report.ProofDecision)
	fmt.Fprintf(w, "Next:                 %s\n", report.RecommendedNextStep)
	fmt.Fprintf(w, "\nClass distribution:    verdict=%s headroom=%t phasef=%d local=%d/%d %.2f%% ceiling=%.2f%% full_history=%d/%d\n",
		report.ClassDistribution.Verdict,
		report.ClassDistribution.HeadroomPresent,
		report.ClassDistribution.PhaseFRequests,
		report.ClassDistribution.LocalSavedTokens,
		report.ClassDistribution.OriginalTokens,
		report.ClassDistribution.LocalSavingsRatio*100,
		report.ClassDistribution.ReducibleCeilingRatio*100,
		report.ClassDistribution.FullHistoryRequests,
		report.ClassDistribution.FullHistoryOriginalTokens)
	fmt.Fprintf(w, "Local gap:             instrumented=%d missing=%d policy_ceiling=%.2f%% protected=%d unattributed=%d errors=%d/400=%d\n",
		report.LocalGap.InstrumentedRequests,
		report.LocalGap.MissingInstrRequests,
		report.LocalGap.PolicySavingsCeilingRatio*100,
		report.LocalGap.PolicyProtectedTokens,
		report.LocalGap.UnattributedGapTokens,
		report.LocalGap.UpstreamErrorRequests,
		report.LocalGap.HTTP400ErrorRequests)
	fmt.Fprintf(w, "Reference inventory:   verdict=%s lane3_accepted=%d arbitrary_kinds=%d local_ref_kinds=%d\n",
		report.ReferenceInventory.Verdict,
		report.ReferenceInventory.Lane3AcceptedContracts,
		report.ReferenceInventory.ArbitraryCandidateKinds,
		report.ReferenceInventory.LocalReferenceURIKinds)
	if report.SocketSummary != nil {
		fmt.Fprintf(w, "Socket summary:        sockets=%d actionable=%d full_history=%d/%d reconnect_full_history=%d/%d handoff=%d\n",
			report.SocketSummary.SocketCount,
			report.SocketSummary.ActionableSockets,
			report.SocketSummary.FullHistoryRequests,
			report.SocketSummary.FullHistoryProviderInputTokens,
			report.SocketSummary.ReconnectFullHistoryRequests,
			report.SocketSummary.ReconnectFullHistoryProviderInputTokens,
			report.SocketSummary.T417ReconnectHandoffRows+report.SocketSummary.T420ReconnectHandoffRows)
	}
	if report.AuditSummary != nil {
		fmt.Fprintf(w, "Audit summary:         phasef=%d shadow_requests=%d shadow_ref_bytes=%d normalized_ref_bytes=%d candidates=%d\n",
			report.AuditSummary.PhaseFRequests,
			report.AuditSummary.ShadowMirrorRequests,
			report.AuditSummary.ShadowMirrorReferenceableBytes,
			report.AuditSummary.ShadowMirrorNormalizedReferenceBytes,
			len(report.AuditSummary.TopCandidates))
	}
	fmt.Fprintln(w, "\nCommands:")
	fmt.Fprintf(w, "  sockets:   %s\n", report.SocketCommand)
	fmt.Fprintf(w, "  audit:     %s\n", report.AuditCommand)
	fmt.Fprintf(w, "  classes:   %s\n", report.ClassCommand)
	fmt.Fprintf(w, "  local_gap: %s\n", report.LocalGapCommand)
	fmt.Fprintf(w, "  refs:      %s\n", report.ReferenceCommand)
	if report.AuditSummary != nil && len(report.AuditSummary.TopCandidates) > 0 {
		fmt.Fprintln(w, "\nAudit headroom candidates:")
		for _, row := range report.AuditSummary.TopCandidates {
			fmt.Fprintf(w, "  %-12s %-32s lane=%s headroom=%d gate=%s stage=%s\n",
				row.RequestShape,
				row.Kind,
				row.CandidateLane,
				row.IncrementalLocalTokensHeadroom,
				row.NextProofGate,
				row.PromotionStage)
		}
	}
	if len(report.TopActionable) > 0 {
		fmt.Fprintln(w, "\nTop actionable:")
		for _, row := range report.TopActionable {
			fmt.Fprintf(w, "  %-36s tokens=%d basis=%s source=%s\n", row.Category, row.Tokens, row.Basis, row.Source)
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

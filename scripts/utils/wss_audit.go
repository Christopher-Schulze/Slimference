package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
)

type wssAuditReport struct {
	Path                   string                           `json:"path"`
	Requests               int                              `json:"requests"`
	WSSRequests            int                              `json:"wss_requests"`
	PhaseFRequests         int                              `json:"phasef_requests"`
	UniqueSessions         int                              `json:"unique_sessions"`
	MissingSessionID       int                              `json:"missing_session_id"`
	PreviousResponseIDUsed int                              `json:"previous_response_id_used"`
	RequestShapes          map[string]int                   `json:"request_shapes,omitempty"`
	ResolvedRequestShapes  map[string]int                   `json:"resolved_request_shapes,omitempty"`
	RequestShapeSources    map[string]int                   `json:"request_shape_sources,omitempty"`
	ReReadRequests         int                              `json:"re_read_requests"`
	ReReadCount            int                              `json:"re_read_count"`
	PositiveSavings        int                              `json:"positive_savings_requests"`
	TokensSaved            int                              `json:"tokens_saved"`
	Since                  *time.Time                       `json:"since,omitempty"`
	GatePassed             bool                             `json:"gate_passed"`
	GateFailures           []string                         `json:"gate_failures,omitempty"`
	RouteModes             map[string]int                   `json:"route_modes,omitempty"`
	ContentClasses         map[string]int                   `json:"content_classes,omitempty"`
	PolicySource           string                           `json:"policy_source,omitempty"`
	Policy                 []control.ProxyLayer0PolicyEntry `json:"policy,omitempty"`
	Cache                  []control.ProxyLayer0CacheEntry  `json:"cache,omitempty"`
	ChunkDedupReferences   int64                            `json:"chunk_dedup_references,omitempty"`
	ChunkDedupRefBytes     int64                            `json:"chunk_dedup_referenced_bytes,omitempty"`
	ChunkDedupInputBytes   int64                            `json:"chunk_dedup_input_bytes,omitempty"`
	HistoryReducers        []wssHistoryReducerSummary       `json:"history_reducers,omitempty"`
	FootprintEconomics     []wssFootprintEconomicsSummary   `json:"footprint_economics,omitempty"`
	FootprintCoverage      *wssFootprintCoverageSummary     `json:"footprint_coverage,omitempty"`
	ShapeEconomics         []wssAuditShapeEconomicsSummary  `json:"shape_economics,omitempty"`
	FullHistory            *wssFullHistoryClassBSummary     `json:"full_history,omitempty"`
	ShadowMirror           *wssShadowMirrorSummary          `json:"shadow_mirror,omitempty"`
	ShadowMirrorCandidates []wssShadowMirrorCandidate       `json:"shadow_mirror_candidates,omitempty"`
	Sessions               []wssAuditSessionSummary         `json:"sessions,omitempty"`
	Notes                  []string                         `json:"notes,omitempty"`
}

type wssAuditSessionSummary struct {
	SessionID              string         `json:"session_id"`
	Requests               int            `json:"requests"`
	PhaseFRequests         int            `json:"phasef_requests"`
	PreviousResponseIDUsed int            `json:"previous_response_id_used"`
	RequestShapes          map[string]int `json:"request_shapes,omitempty"`
	ResolvedRequestShapes  map[string]int `json:"resolved_request_shapes,omitempty"`
	ReReadCount            int            `json:"re_read_count"`
	TokensSaved            int            `json:"tokens_saved"`
	FirstSeen              time.Time      `json:"first_seen,omitempty"`
	LastSeen               time.Time      `json:"last_seen,omitempty"`
}

type wssFullHistoryClassBSummary struct {
	Requests               int            `json:"requests"`
	Sessions               int            `json:"sessions"`
	MissingSessionID       int            `json:"missing_session_id"`
	PreviousResponseIDUsed int            `json:"previous_response_id_used"`
	ProviderInputTokens    int            `json:"provider_input_tokens"`
	ProviderCachedTokens   int            `json:"provider_cached_tokens"`
	ProviderOutputTokens   int            `json:"provider_output_tokens"`
	CacheReadTokens        int            `json:"cache_read_tokens"`
	CacheCreateTokens      int            `json:"cache_create_tokens"`
	OriginalTokens         int            `json:"original_tokens"`
	FinalTokens            int            `json:"final_tokens"`
	SavedTokens            int            `json:"saved_tokens"`
	NetSavedTokens         int            `json:"net_saved_tokens"`
	ErrorRequests          int            `json:"error_requests"`
	UpstreamErrorRequests  int            `json:"upstream_error_requests"`
	HTTP400ErrorRequests   int            `json:"http_400_error_requests"`
	ByClientFamily         map[string]int `json:"by_client_family,omitempty"`
	BySocketSeq            map[string]int `json:"by_socket_seq,omitempty"`
	BySocketCloseInitiator map[string]int `json:"by_socket_close_initiator,omitempty"`
}

type wssFullHistoryClassBAccumulator struct {
	summary  wssFullHistoryClassBSummary
	sessions map[string]struct{}
}

type wssAuditRequestShapeResolution struct {
	Shape  string
	Source string
}

type wssAuditShapeEconomicsSummary struct {
	Shape                 string         `json:"shape"`
	Requests              int            `json:"requests"`
	Sources               map[string]int `json:"sources,omitempty"`
	ProviderInputTokens   int            `json:"provider_input_tokens"`
	ProviderCachedTokens  int            `json:"provider_cached_tokens"`
	ProviderCachedPct     float64        `json:"provider_cached_pct"`
	ProviderOutputTokens  int            `json:"provider_output_tokens"`
	CacheReadTokens       int            `json:"cache_read_tokens"`
	CacheCreateTokens     int            `json:"cache_create_tokens"`
	OriginalTokens        int            `json:"original_tokens"`
	FinalTokens           int            `json:"final_tokens"`
	LocalSavedTokens      int            `json:"local_saved_tokens"`
	NetSavedTokens        int            `json:"net_saved_tokens"`
	LocalSavedPct         float64        `json:"local_saved_pct"`
	ErrorRequests         int            `json:"error_requests"`
	UpstreamErrorRequests int            `json:"upstream_error_requests"`
	HTTP400ErrorRequests  int            `json:"http_400_error_requests"`
}

type wssAuditShapeEconomicsAccumulator struct {
	rows map[string]*wssAuditShapeEconomicsSummary
}

type wssHistoryReducerSummary struct {
	Mechanism        string         `json:"mechanism"`
	Decisions        int            `json:"decisions"`
	Applied          int            `json:"applied"`
	FullPass         int            `json:"full_pass"`
	Skipped          int            `json:"skipped"`
	FailedOpen       int            `json:"failed_open"`
	OriginalTokens   int            `json:"original_tokens,omitempty"`
	FinalTokens      int            `json:"final_tokens,omitempty"`
	SavedTokens      int            `json:"saved_tokens,omitempty"`
	NetTokens        int            `json:"net_tokens,omitempty"`
	FootprintScore   int            `json:"footprint_score,omitempty"`
	Reasons          map[string]int `json:"reasons,omitempty"`
	FootprintBuckets map[string]int `json:"footprint_score_buckets,omitempty"`
	CacheImpact      map[string]int `json:"cache_impact,omitempty"`
}

type wssFootprintEconomicsSummary struct {
	Bucket         string         `json:"bucket"`
	TurnBand       string         `json:"turn_band"`
	RequestShape   string         `json:"request_shape"`
	Decisions      int            `json:"decisions"`
	Applied        int            `json:"applied"`
	FullPass       int            `json:"full_pass"`
	Skipped        int            `json:"skipped"`
	FailedOpen     int            `json:"failed_open"`
	OriginalTokens int            `json:"original_tokens,omitempty"`
	FinalTokens    int            `json:"final_tokens,omitempty"`
	SavedTokens    int            `json:"saved_tokens,omitempty"`
	NetTokens      int            `json:"net_tokens,omitempty"`
	FootprintScore int            `json:"footprint_score,omitempty"`
	Mechanisms     map[string]int `json:"mechanisms,omitempty"`
	CacheImpact    map[string]int `json:"cache_impact,omitempty"`
}

type wssFootprintEconomicsAccumulator struct {
	rows map[string]*wssFootprintEconomicsSummary
}

type wssFootprintCoverageSummary struct {
	TokenDecisions                         int            `json:"token_decisions"`
	AppliedTokenDecisions                  int            `json:"applied_token_decisions"`
	WithFootprint                          int            `json:"with_footprint"`
	MissingFootprint                       int            `json:"missing_footprint"`
	AppliedMissingFootprint                int            `json:"applied_missing_footprint"`
	WithRemainingTurnsEstimate             int            `json:"with_remaining_turns_estimate"`
	MissingRemainingTurnsEstimate          int            `json:"missing_remaining_turns_estimate"`
	AppliedMissingRemainingTurnsEstimate   int            `json:"applied_missing_remaining_turns_estimate"`
	SavedTokens                            int            `json:"saved_tokens,omitempty"`
	MissingSavedTokens                     int            `json:"missing_saved_tokens,omitempty"`
	ByMechanism                            map[string]int `json:"by_mechanism,omitempty"`
	MissingByMechanism                     map[string]int `json:"missing_by_mechanism,omitempty"`
	MissingRemainingTurnsEstimateMechanism map[string]int `json:"missing_remaining_turns_estimate_by_mechanism,omitempty"`
}

type wssFootprintCoverageAccumulator struct {
	summary wssFootprintCoverageSummary
}

type wssShadowMirrorSummary struct {
	Requests                           int                          `json:"requests"`
	Blocks                             int                          `json:"blocks"`
	Bytes                              int                          `json:"bytes"`
	ReferenceableBlocks                int                          `json:"referenceable_blocks"`
	ReferenceableBytes                 int                          `json:"referenceable_bytes"`
	ReferenceableBytePct               float64                      `json:"referenceable_byte_pct"`
	NormalizedSegments                 int                          `json:"normalized_segments"`
	NormalizedBytes                    int                          `json:"normalized_bytes"`
	NormalizedReferenceableSegments    int                          `json:"normalized_referenceable_segments"`
	NormalizedReferenceableBytes       int                          `json:"normalized_referenceable_bytes"`
	NormalizedReferenceableBytePct     float64                      `json:"normalized_referenceable_byte_pct"`
	NormalizedReferenceableBytesByKind []wssShadowMirrorKindSummary `json:"normalized_referenceable_bytes_by_kind,omitempty"`
}

type wssShadowMirrorKindSummary struct {
	Kind                  string  `json:"kind"`
	Segments              int     `json:"segments"`
	Bytes                 int     `json:"bytes"`
	ReferenceableSegments int     `json:"referenceable_segments"`
	ReferenceableBytes    int     `json:"referenceable_bytes"`
	ReferenceableBytePct  float64 `json:"referenceable_byte_pct"`
}

type wssShadowMirrorAccumulator struct {
	summary wssShadowMirrorSummary
	byKind  map[string]*wssShadowMirrorKindSummary
}

type wssShadowMirrorCandidate struct {
	RequestShape                    string                            `json:"request_shape"`
	Kind                            string                            `json:"kind"`
	Requests                        int                               `json:"requests"`
	ReferenceableSegments           int                               `json:"referenceable_segments"`
	Segments                        int                               `json:"segments"`
	ReferenceableBytes              int                               `json:"referenceable_bytes"`
	Bytes                           int                               `json:"bytes"`
	ReferenceableBytePct            float64                           `json:"referenceable_byte_pct"`
	CandidateLocalTokensEstimate    int                               `json:"candidate_local_tokens_estimate"`
	IncrementalLocalTokensHeadroom  int                               `json:"incremental_local_tokens_headroom"`
	PromotionOpenRequests           int                               `json:"promotion_open_requests,omitempty"`
	PromotionOpenReferenceableBytes int                               `json:"promotion_open_referenceable_bytes,omitempty"`
	PromotionOpenCandidateTokens    int                               `json:"promotion_open_candidate_tokens_estimate,omitempty"`
	PromotionOpenLocalSavedTokens   int                               `json:"promotion_open_local_saved_tokens,omitempty"`
	PromotionOpenHeadroom           int                               `json:"promotion_open_headroom,omitempty"`
	ProviderInputTokens             int                               `json:"provider_input_tokens"`
	ProviderCachedTokens            int                               `json:"provider_cached_tokens"`
	ProviderOutputTokens            int                               `json:"provider_output_tokens"`
	LocalSavedTokens                int                               `json:"local_saved_tokens"`
	PreviousResponseIDUsed          int                               `json:"previous_response_id_used"`
	DetachedPreviousResponse        int                               `json:"detached_previous_response_requests"`
	StatelessContinuation           int                               `json:"stateless_continuation_requests"`
	FullHistoryStatelessFollowup    int                               `json:"full_history_stateless_followup_requests"`
	StructuredMutationGuarded       int                               `json:"structured_mutation_guarded_requests"`
	HistoryRecoveryGuarded          int                               `json:"history_recovery_guarded_requests"`
	CacheBustDemoted                int                               `json:"cache_bust_demoted_requests"`
	CacheBustDemotedScopes          map[string]int                    `json:"cache_bust_demoted_scopes,omitempty"`
	CacheBustDemotedClassKeys       map[string]int                    `json:"cache_bust_demoted_class_keys,omitempty"`
	EffectiveMutationGuards         map[string]int                    `json:"effective_mutation_guards,omitempty"`
	BySocketSeq                     map[string]int                    `json:"by_socket_seq,omitempty"`
	ErrorRequests                   int                               `json:"error_requests"`
	UpstreamErrorRequests           int                               `json:"upstream_error_requests"`
	HTTP400ErrorRequests            int                               `json:"http_400_error_requests"`
	ErrorFree                       bool                              `json:"error_free"`
	CandidateLane                   string                            `json:"candidate_lane"`
	NextProofGate                   string                            `json:"next_proof_gate"`
	PromotionStage                  string                            `json:"promotion_stage"`
	PromotionBlockers               []string                          `json:"promotion_blockers,omitempty"`
	PromotionBlockerHeadroom        map[string]int                    `json:"promotion_blocker_headroom_tokens,omitempty"`
	RecommendedAction               string                            `json:"recommended_action"`
	TopSessions                     []wssShadowMirrorCandidateSession `json:"top_sessions,omitempty"`
	sessionRows                     map[string]*wssShadowMirrorCandidateSession
}

type wssShadowMirrorCandidateSession struct {
	SessionID                       string         `json:"session_id"`
	Requests                        int            `json:"requests"`
	ReferenceableBytes              int            `json:"referenceable_bytes"`
	CandidateLocalTokensEstimate    int            `json:"candidate_local_tokens_estimate"`
	IncrementalLocalTokensHeadroom  int            `json:"incremental_local_tokens_headroom"`
	PromotionOpenRequests           int            `json:"promotion_open_requests,omitempty"`
	PromotionOpenReferenceableBytes int            `json:"promotion_open_referenceable_bytes,omitempty"`
	PromotionOpenCandidateTokens    int            `json:"promotion_open_candidate_tokens_estimate,omitempty"`
	PromotionOpenLocalSavedTokens   int            `json:"promotion_open_local_saved_tokens,omitempty"`
	PromotionOpenHeadroom           int            `json:"promotion_open_headroom,omitempty"`
	ProviderInputTokens             int            `json:"provider_input_tokens"`
	ProviderCachedTokens            int            `json:"provider_cached_tokens"`
	LocalSavedTokens                int            `json:"local_saved_tokens"`
	PreviousResponseIDUsed          int            `json:"previous_response_id_used"`
	DetachedPreviousResponse        int            `json:"detached_previous_response_requests"`
	StatelessContinuation           int            `json:"stateless_continuation_requests"`
	FullHistoryStatelessFollowup    int            `json:"full_history_stateless_followup_requests"`
	StructuredMutationGuarded       int            `json:"structured_mutation_guarded_requests"`
	HistoryRecoveryGuarded          int            `json:"history_recovery_guarded_requests"`
	CacheBustDemoted                int            `json:"cache_bust_demoted_requests"`
	CacheBustDemotedScopes          map[string]int `json:"cache_bust_demoted_scopes,omitempty"`
	CacheBustDemotedClassKeys       map[string]int `json:"cache_bust_demoted_class_keys,omitempty"`
	EffectiveMutationGuards         map[string]int `json:"effective_mutation_guards,omitempty"`
	BySocketSeq                     map[string]int `json:"by_socket_seq,omitempty"`
	ErrorRequests                   int            `json:"error_requests"`
	UpstreamErrorRequests           int            `json:"upstream_error_requests"`
	HTTP400ErrorRequests            int            `json:"http_400_error_requests"`
	ErrorFree                       bool           `json:"error_free"`
	NextProofGate                   string         `json:"next_proof_gate"`
	PromotionStage                  string         `json:"promotion_stage"`
	PromotionBlockers               []string       `json:"promotion_blockers,omitempty"`
	PromotionBlockerHeadroom        map[string]int `json:"promotion_blocker_headroom_tokens,omitempty"`
}

type wssShadowMirrorCandidateAccumulator struct {
	rows map[string]*wssShadowMirrorCandidate
}

type wssAuditFlags struct {
	path                     string
	outputFormat             string
	expectDistinctSessions   int
	minPhaseF                int
	minFullHistory           int
	requireSavings           bool
	requireHistoryEvidence   bool
	requireFootprintEvidence bool
	adminStateFile           string
	since                    time.Time
	help                     bool
}

const wssAuditHelpText = `wss-audit: inspect Codex WSS decisions without raw frame dumps

Usage:
  go run ./scripts/utils wss-audit <decisions.jsonl> [flags]

Flags:
  --expect-distinct-sessions=<n>  Fail if fewer than n non-empty WSS session ids are present
  --min-phasef=<n>                Fail if fewer than n Phase-F request summaries are present
  --min-full-history=<n>          Fail if fewer than n full-history Phase-F request summaries are present
  --require-savings               Fail if no positive input-token savings are present
  --require-history-evidence      Fail if no stale/obsolete reducer evidence is present
  --require-footprint-evidence    Fail if footprint-score or remaining-turn evidence is missing
  --admin-state-file=<path>        Join current /admin/state policy counters into the report
  --since=<rfc3339>               Ignore records before this timestamp
  --json                          Output JSON

Reads content-free RequestSummary JSONL records and reports WSS route coverage,
Phase-F request counts, session-key continuity, previous_response_id usage,
request-shape coverage, per-shape provider-cache/local-savings economics,
full-history Class-B cost/error/socket correlation, positive input-token
savings, T353 history-reducer evidence, T359 footprint-by-turn economics, and
T355 server-state shadow-mirror density. With --admin-state-file it also prints
content-free policy and cache hit/miss counters from the matching admin snapshot.
Legacy rows without request-shape facts are resolved only when an existing
previous_response_id signal proves delta shape; root/full-history rows are never
guessed from absence alone. It does not inspect payload text or auth headers.`

func runWSSAudit(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSAuditFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssAuditHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-audit <decisions.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSAuditReport(flags)
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
	writeWSSAuditText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseWSSAuditFlags(args []string) (wssAuditFlags, error) {
	flags := wssAuditFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			flags.help = true
		case a == "--json":
			flags.outputFormat = outputJSON
		case a == "--require-savings":
			flags.requireSavings = true
		case a == "--require-history-evidence":
			flags.requireHistoryEvidence = true
		case a == "--require-footprint-evidence":
			flags.requireFootprintEvidence = true
		case a == "--admin-state-file":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			flags.adminStateFile = v
		case strings.HasPrefix(a, "--admin-state-file="):
			flags.adminStateFile = strings.TrimPrefix(a, "--admin-state-file=")
		case a == "--since":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return flags, fmt.Errorf("--since must be RFC3339: %w", err)
			}
			flags.since = t
		case strings.HasPrefix(a, "--since="):
			t, err := time.Parse(time.RFC3339, strings.TrimPrefix(a, "--since="))
			if err != nil {
				return flags, fmt.Errorf("--since must be RFC3339: %w", err)
			}
			flags.since = t
		case a == "--expect-distinct-sessions":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--expect-distinct-sessions must be a non-negative integer")
			}
			flags.expectDistinctSessions = n
		case strings.HasPrefix(a, "--expect-distinct-sessions="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--expect-distinct-sessions="))
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--expect-distinct-sessions must be a non-negative integer")
			}
			flags.expectDistinctSessions = n
		case a == "--min-phasef":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--min-phasef must be a non-negative integer")
			}
			flags.minPhaseF = n
		case strings.HasPrefix(a, "--min-phasef="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--min-phasef="))
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--min-phasef must be a non-negative integer")
			}
			flags.minPhaseF = n
		case a == "--min-full-history":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return flags, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--min-full-history must be a non-negative integer")
			}
			flags.minFullHistory = n
		case strings.HasPrefix(a, "--min-full-history="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--min-full-history="))
			if err != nil || n < 0 {
				return flags, fmt.Errorf("--min-full-history must be a non-negative integer")
			}
			flags.minFullHistory = n
		case strings.HasPrefix(a, "-"):
			return flags, fmt.Errorf("unknown flag: %s", a)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple decisions logs provided")
			}
			flags.path = a
		}
	}
	return flags, nil
}

func loadWSSAuditReport(flags wssAuditFlags) (wssAuditReport, error) {
	summaries, err := dbg.ReplaySession(flags.path)
	if err != nil {
		return wssAuditReport{}, fmt.Errorf("read decisions %s: %w", flags.path, err)
	}
	report := wssAuditReport{
		Path:                  flags.path,
		GatePassed:            true,
		RouteModes:            make(map[string]int),
		ContentClasses:        make(map[string]int),
		RequestShapes:         make(map[string]int),
		ResolvedRequestShapes: make(map[string]int),
		RequestShapeSources:   make(map[string]int),
	}
	if !flags.since.IsZero() {
		since := flags.since
		report.Since = &since
	}
	sessionStats := make(map[string]*wssAuditSessionSummary)
	historyReducers := make(map[string]*wssHistoryReducerSummary)
	footprintEconomics := wssFootprintEconomicsAccumulator{rows: make(map[string]*wssFootprintEconomicsSummary)}
	footprintCoverage := wssFootprintCoverageAccumulator{}
	fullHistory := wssFullHistoryClassBAccumulator{}
	shapeEconomics := wssAuditShapeEconomicsAccumulator{rows: make(map[string]*wssAuditShapeEconomicsSummary)}
	shadowMirror := wssShadowMirrorAccumulator{byKind: make(map[string]*wssShadowMirrorKindSummary)}
	shadowMirrorCandidates := wssShadowMirrorCandidateAccumulator{rows: make(map[string]*wssShadowMirrorCandidate)}
	for _, summary := range summaries {
		if !flags.since.IsZero() {
			if summary.Timestamp.IsZero() || summary.Timestamp.Before(flags.since) {
				continue
			}
		}
		report.Requests++
		route := wssAuditRouteMode(summary)
		if route == "" {
			route = "unknown"
		}
		report.RouteModes[route]++
		if !wssAuditIsWSS(summary, route) {
			continue
		}
		report.WSSRequests++
		isPhaseF := wssAuditIsPhaseF(route)
		if isPhaseF {
			report.PhaseFRequests++
			shape := wssAuditRequestShape(summary)
			if shape != "" {
				report.RequestShapes[shape]++
			}
			resolvedShape := wssAuditResolveRequestShape(summary)
			report.ResolvedRequestShapes[resolvedShape.Shape]++
			report.RequestShapeSources[resolvedShape.Source]++
			shapeEconomics.add(summary, resolvedShape)
			footprintEconomics.add(summary, resolvedShape)
			footprintCoverage.add(summary)
			if shape == "full_history" {
				fullHistory.add(summary)
			}
			shadowMirrorCandidates.add(summary, resolvedShape.Shape)
		}
		if strings.TrimSpace(summary.SessionID) == "" {
			report.MissingSessionID++
		}
		if summary.PreviousResponseIDUsed {
			report.PreviousResponseIDUsed++
		}
		if summary.ReReadCount > 0 {
			report.ReReadRequests++
			report.ReReadCount += summary.ReReadCount
		}
		if summary.Tokens.Saved > 0 {
			report.PositiveSavings++
			report.TokensSaved += summary.Tokens.Saved
		}
		for _, class := range wssAuditContentClasses(summary) {
			report.ContentClasses[class]++
		}
		accumulateWSSHistoryReducers(historyReducers, summary.EvidenceDecisions)
		shadowMirror.add(summary.DebugFacts)
		sessionID := strings.TrimSpace(summary.SessionID)
		if sessionID == "" {
			sessionID = "(missing)"
		}
		stats := sessionStats[sessionID]
		if stats == nil {
			stats = &wssAuditSessionSummary{SessionID: sessionID}
			sessionStats[sessionID] = stats
		}
		stats.Requests++
		if isPhaseF {
			stats.PhaseFRequests++
			shape := wssAuditRequestShape(summary)
			if shape != "" {
				addWSSAuditCount(&stats.RequestShapes, shape)
			}
			resolvedShape := wssAuditResolveRequestShape(summary)
			addWSSAuditCount(&stats.ResolvedRequestShapes, resolvedShape.Shape)
		}
		if summary.PreviousResponseIDUsed {
			stats.PreviousResponseIDUsed++
		}
		stats.ReReadCount += summary.ReReadCount
		stats.TokensSaved += maxInt(0, summary.Tokens.Saved)
		if !summary.Timestamp.IsZero() {
			if stats.FirstSeen.IsZero() || summary.Timestamp.Before(stats.FirstSeen) {
				stats.FirstSeen = summary.Timestamp
			}
			if stats.LastSeen.IsZero() || summary.Timestamp.After(stats.LastSeen) {
				stats.LastSeen = summary.Timestamp
			}
		}
	}
	for _, stats := range sessionStats {
		if stats.SessionID != "(missing)" {
			report.UniqueSessions++
		}
		report.Sessions = append(report.Sessions, *stats)
	}
	sort.Slice(report.Sessions, func(i, j int) bool {
		if report.Sessions[i].Requests != report.Sessions[j].Requests {
			return report.Sessions[i].Requests > report.Sessions[j].Requests
		}
		return report.Sessions[i].SessionID < report.Sessions[j].SessionID
	})
	if shadow := shadowMirror.finalize(); shadow != nil {
		report.ShadowMirror = shadow
	}
	report.ShadowMirrorCandidates = shadowMirrorCandidates.finalize()
	if history := fullHistory.finalize(); history != nil {
		report.FullHistory = history
	}
	report.ShapeEconomics = shapeEconomics.finalize()
	report.HistoryReducers = finalizeWSSHistoryReducers(historyReducers)
	report.FootprintEconomics = footprintEconomics.finalize()
	report.FootprintCoverage = footprintCoverage.finalize()
	report.Notes = wssAuditNotes(report)
	report.GateFailures = wssAuditGateFailures(report, flags)
	report.GatePassed = len(report.GateFailures) == 0
	if flags.adminStateFile != "" {
		policy, cache, savings, err := loadWSSAuditTelemetry(flags.adminStateFile)
		if err != nil {
			return wssAuditReport{}, err
		}
		report.PolicySource = "file:" + flags.adminStateFile
		report.Policy = policy
		report.Cache = cache
		report.ChunkDedupReferences = savings.ProxyLayer0ChunkRefs
		report.ChunkDedupRefBytes = savings.ProxyLayer0ChunkRefBytes
		report.ChunkDedupInputBytes = savings.ProxyLayer0ChunkInBytes
	}
	return report, nil
}

func accumulateWSSHistoryReducers(out map[string]*wssHistoryReducerSummary, decisions []evidence.BlockDecision) {
	if out == nil {
		return
	}
	for _, decision := range decisions {
		mechanism := strings.TrimSpace(decision.Mechanism)
		if !isWSSHistoryReducerMechanism(mechanism) {
			continue
		}
		row := out[mechanism]
		if row == nil {
			row = &wssHistoryReducerSummary{Mechanism: mechanism}
			out[mechanism] = row
		}
		row.Decisions++
		switch decision.Action {
		case evidence.ActionApplied:
			row.Applied++
		case evidence.ActionFullPass:
			row.FullPass++
		case evidence.ActionSkipped:
			row.Skipped++
		case evidence.ActionFailedOpen:
			row.FailedOpen++
		}
		row.OriginalTokens += decision.OriginalTokens
		row.FinalTokens += decision.FinalTokens
		row.SavedTokens += decision.SavedTokens
		row.NetTokens += decision.NetTokens
		row.FootprintScore += decision.FootprintScore
		addWSSAuditCount(&row.Reasons, decision.Reason)
		addWSSAuditCount(&row.FootprintBuckets, decision.FootprintScoreBucket)
		addWSSAuditCount(&row.CacheImpact, decision.CacheImpact)
	}
}

func isWSSHistoryReducerMechanism(mechanism string) bool {
	switch mechanism {
	case "stale_read", "obsolete_prune":
		return true
	default:
		return false
	}
}

func addWSSAuditCount(counts *map[string]int, key string) {
	key = strings.TrimSpace(key)
	if counts == nil || key == "" {
		return
	}
	if *counts == nil {
		*counts = make(map[string]int)
	}
	(*counts)[key]++
}

func addWSSAuditCountWithMissing(counts *map[string]int, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "(missing)"
	}
	addWSSAuditCount(counts, key)
}

func addDelimitedWSSAuditCounts(counts *map[string]int, value string) {
	for _, part := range strings.Split(value, ",") {
		addWSSAuditCount(counts, part)
	}
}

func (a *wssAuditShapeEconomicsAccumulator) add(summary dbg.RequestSummary, resolution wssAuditRequestShapeResolution) {
	if a == nil {
		return
	}
	shape := strings.TrimSpace(resolution.Shape)
	if shape == "" {
		shape = "unknown"
	}
	if a.rows == nil {
		a.rows = make(map[string]*wssAuditShapeEconomicsSummary)
	}
	row := a.rows[shape]
	if row == nil {
		row = &wssAuditShapeEconomicsSummary{Shape: shape}
		a.rows[shape] = row
	}
	row.Requests++
	addWSSAuditCount(&row.Sources, resolution.Source)
	row.ProviderInputTokens += maxInt(0, summary.ProviderInputTokens)
	row.ProviderCachedTokens += maxInt(0, summary.ProviderCachedTokens)
	row.ProviderOutputTokens += maxInt(0, summary.ProviderOutputTokens)
	row.CacheReadTokens += maxInt(0, summary.CacheReadTokens)
	row.CacheCreateTokens += maxInt(0, summary.CacheCreateTokens)
	row.OriginalTokens += maxInt(0, summary.Tokens.Original)
	row.FinalTokens += maxInt(0, summary.Tokens.Final)
	row.LocalSavedTokens += summary.Tokens.Saved
	row.NetSavedTokens += summary.NetSavedTokens
	if len(summary.Errors) > 0 {
		row.ErrorRequests++
	}
	if wssAuditHasUpstreamError(summary) {
		row.UpstreamErrorRequests++
	}
	if wssAuditHasHTTP400Error(summary) {
		row.HTTP400ErrorRequests++
	}
}

func (a *wssAuditShapeEconomicsAccumulator) finalize() []wssAuditShapeEconomicsSummary {
	if a == nil || len(a.rows) == 0 {
		return nil
	}
	keys := make([]string, 0, len(a.rows))
	for key := range a.rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]wssAuditShapeEconomicsSummary, 0, len(keys))
	for _, key := range keys {
		row := *a.rows[key]
		row.ProviderCachedPct = pct(row.ProviderCachedTokens, row.ProviderInputTokens)
		row.LocalSavedPct = pct(row.LocalSavedTokens, row.OriginalTokens)
		out = append(out, row)
	}
	return out
}

func (a *wssFullHistoryClassBAccumulator) add(summary dbg.RequestSummary) {
	if a == nil {
		return
	}
	a.summary.Requests++
	sessionID := strings.TrimSpace(summary.SessionID)
	if sessionID == "" {
		a.summary.MissingSessionID++
	} else {
		if a.sessions == nil {
			a.sessions = make(map[string]struct{})
		}
		a.sessions[sessionID] = struct{}{}
	}
	if summary.PreviousResponseIDUsed {
		a.summary.PreviousResponseIDUsed++
	}
	a.summary.ProviderInputTokens += maxInt(0, summary.ProviderInputTokens)
	a.summary.ProviderCachedTokens += maxInt(0, summary.ProviderCachedTokens)
	a.summary.ProviderOutputTokens += maxInt(0, summary.ProviderOutputTokens)
	a.summary.CacheReadTokens += maxInt(0, summary.CacheReadTokens)
	a.summary.CacheCreateTokens += maxInt(0, summary.CacheCreateTokens)
	a.summary.OriginalTokens += maxInt(0, summary.Tokens.Original)
	a.summary.FinalTokens += maxInt(0, summary.Tokens.Final)
	a.summary.SavedTokens += maxInt(0, summary.Tokens.Saved)
	a.summary.NetSavedTokens += maxInt(0, summary.NetSavedTokens)
	if len(summary.Errors) > 0 {
		a.summary.ErrorRequests++
	}
	if wssAuditHasUpstreamError(summary) {
		a.summary.UpstreamErrorRequests++
	}
	if wssAuditHasHTTP400Error(summary) {
		a.summary.HTTP400ErrorRequests++
	}
	addWSSAuditCountWithMissing(&a.summary.ByClientFamily, summary.ClientFamily)
	addWSSAuditCountWithMissing(&a.summary.BySocketSeq, summary.DebugFacts["wss.socket_seq"])
	addWSSAuditCountWithMissing(&a.summary.BySocketCloseInitiator, summary.DebugFacts["wss.socket_close_initiator"])
}

func (a *wssFullHistoryClassBAccumulator) finalize() *wssFullHistoryClassBSummary {
	if a == nil || a.summary.Requests == 0 {
		return nil
	}
	out := a.summary
	out.Sessions = len(a.sessions)
	return &out
}

func wssAuditHasUpstreamError(summary dbg.RequestSummary) bool {
	if strings.Contains(strings.ToLower(summary.BypassReason), "upstream_error") {
		return true
	}
	for _, errText := range summary.Errors {
		if strings.Contains(strings.ToLower(errText), "upstream_error") {
			return true
		}
	}
	return false
}

func wssAuditHasHTTP400Error(summary dbg.RequestSummary) bool {
	if strings.Contains(summary.BypassReason, "400") {
		return true
	}
	for _, errText := range summary.Errors {
		if strings.Contains(errText, "400") {
			return true
		}
	}
	return false
}

func finalizeWSSHistoryReducers(rows map[string]*wssHistoryReducerSummary) []wssHistoryReducerSummary {
	if len(rows) == 0 {
		return nil
	}
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]wssHistoryReducerSummary, 0, len(keys))
	for _, key := range keys {
		out = append(out, *rows[key])
	}
	return out
}

func (a *wssFootprintEconomicsAccumulator) add(summary dbg.RequestSummary, resolution wssAuditRequestShapeResolution) {
	if a == nil || len(summary.EvidenceDecisions) == 0 {
		return
	}
	shape := strings.TrimSpace(resolution.Shape)
	if shape == "" {
		shape = "unknown"
	}
	turnBand := wssFootprintTurnBand(summary.DebugFacts)
	for _, decision := range summary.EvidenceDecisions {
		bucket := strings.TrimSpace(decision.FootprintScoreBucket)
		if bucket == "" && decision.FootprintScore <= 0 {
			continue
		}
		if bucket == "" {
			bucket = "unbucketed"
		}
		if a.rows == nil {
			a.rows = make(map[string]*wssFootprintEconomicsSummary)
		}
		key := bucket + "\x00" + turnBand + "\x00" + shape
		row := a.rows[key]
		if row == nil {
			row = &wssFootprintEconomicsSummary{
				Bucket:       bucket,
				TurnBand:     turnBand,
				RequestShape: shape,
			}
			a.rows[key] = row
		}
		row.Decisions++
		switch decision.Action {
		case evidence.ActionApplied:
			row.Applied++
		case evidence.ActionFullPass:
			row.FullPass++
		case evidence.ActionSkipped:
			row.Skipped++
		case evidence.ActionFailedOpen:
			row.FailedOpen++
		}
		row.OriginalTokens += decision.OriginalTokens
		row.FinalTokens += decision.FinalTokens
		row.SavedTokens += decision.SavedTokens
		row.NetTokens += decision.NetTokens
		row.FootprintScore += decision.FootprintScore
		addWSSAuditCount(&row.Mechanisms, decision.Mechanism)
		addWSSAuditCount(&row.CacheImpact, decision.CacheImpact)
	}
}

func (a *wssFootprintEconomicsAccumulator) finalize() []wssFootprintEconomicsSummary {
	if a == nil || len(a.rows) == 0 {
		return nil
	}
	keys := make([]string, 0, len(a.rows))
	for key := range a.rows {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := a.rows[keys[i]]
		right := a.rows[keys[j]]
		if left.Bucket != right.Bucket {
			return wssFootprintBucketRank(left.Bucket) < wssFootprintBucketRank(right.Bucket)
		}
		if left.TurnBand != right.TurnBand {
			return wssFootprintTurnBandRank(left.TurnBand) < wssFootprintTurnBandRank(right.TurnBand)
		}
		return left.RequestShape < right.RequestShape
	})
	out := make([]wssFootprintEconomicsSummary, 0, len(keys))
	for _, key := range keys {
		out = append(out, *a.rows[key])
	}
	return out
}

func (a *wssFootprintCoverageAccumulator) add(summary dbg.RequestSummary) {
	if a == nil {
		return
	}
	hasRemainingTurnsEstimate := wssHasRemainingTurnsEstimate(summary.DebugFacts)
	for _, decision := range summary.EvidenceDecisions {
		if !wssFootprintCoverageEligible(decision) {
			continue
		}
		mechanism := strings.TrimSpace(decision.Mechanism)
		if mechanism == "" {
			mechanism = "unknown"
		}
		a.summary.TokenDecisions++
		a.summary.SavedTokens += decision.SavedTokens
		addWSSAuditCount(&a.summary.ByMechanism, mechanism)
		if decision.Action == evidence.ActionApplied {
			a.summary.AppliedTokenDecisions++
		}
		if decision.FootprintScore > 0 || strings.TrimSpace(decision.FootprintScoreBucket) != "" {
			a.summary.WithFootprint++
			if hasRemainingTurnsEstimate {
				a.summary.WithRemainingTurnsEstimate++
				continue
			}
			a.summary.MissingRemainingTurnsEstimate++
			addWSSAuditCount(&a.summary.MissingRemainingTurnsEstimateMechanism, mechanism)
			if decision.Action == evidence.ActionApplied {
				a.summary.AppliedMissingRemainingTurnsEstimate++
			}
			continue
		}
		a.summary.MissingFootprint++
		a.summary.MissingSavedTokens += decision.SavedTokens
		addWSSAuditCount(&a.summary.MissingByMechanism, mechanism)
		if decision.Action == evidence.ActionApplied {
			a.summary.AppliedMissingFootprint++
		}
	}
}

func wssFootprintCoverageEligible(decision evidence.BlockDecision) bool {
	return decision.OriginalTokens > 0 ||
		decision.FinalTokens > 0 ||
		decision.SavedTokens > 0 ||
		decision.NetTokens != 0
}

func wssHasRemainingTurnsEstimate(facts map[string]string) bool {
	if len(facts) == 0 {
		return false
	}
	_, ok := parseNonNegativeInt(facts["wss.remaining_turns_estimate"])
	return ok
}

func (a *wssFootprintCoverageAccumulator) finalize() *wssFootprintCoverageSummary {
	if a == nil || a.summary.TokenDecisions == 0 {
		return nil
	}
	out := a.summary
	return &out
}

func wssFootprintTurnBand(facts map[string]string) string {
	if len(facts) == 0 {
		return "unknown"
	}
	turnSeq, ok := parseNonNegativeInt(facts["wss.turn_seq"])
	if !ok || turnSeq <= 0 {
		return "unknown"
	}
	switch {
	case turnSeq <= 3:
		return "turn_1_3"
	case turnSeq <= 8:
		return "turn_4_8"
	default:
		return "turn_9_plus"
	}
}

func wssFootprintBucketRank(bucket string) int {
	switch strings.TrimSpace(bucket) {
	case "high":
		return 0
	case "mid":
		return 1
	case "low":
		return 2
	case "unbucketed":
		return 3
	default:
		return 4
	}
}

func wssFootprintTurnBandRank(band string) int {
	switch strings.TrimSpace(band) {
	case "turn_1_3":
		return 0
	case "turn_4_8":
		return 1
	case "turn_9_plus":
		return 2
	case "unknown":
		return 3
	default:
		return 4
	}
}

func (a *wssShadowMirrorAccumulator) add(facts map[string]string) {
	if a == nil || len(facts) == 0 || !wssShadowMirrorFactsPresent(facts) {
		return
	}
	a.summary.Requests++
	a.summary.Blocks += intFact(facts, "wss.shadow_mirror_blocks")
	a.summary.Bytes += intFact(facts, "wss.shadow_mirror_bytes")
	a.summary.ReferenceableBlocks += intFact(facts, "wss.shadow_mirror_referenceable_blocks")
	a.summary.ReferenceableBytes += intFact(facts, "wss.shadow_mirror_referenceable_bytes")
	a.summary.NormalizedSegments += intFact(facts, "wss.shadow_mirror_normalized_segments")
	a.summary.NormalizedBytes += intFact(facts, "wss.shadow_mirror_normalized_bytes")
	a.summary.NormalizedReferenceableSegments += intFact(facts, "wss.shadow_mirror_normalized_referenceable_segments")
	a.summary.NormalizedReferenceableBytes += intFact(facts, "wss.shadow_mirror_normalized_referenceable_bytes")
	a.addKinds(facts["wss.shadow_mirror_normalized_density_by_kind"])
}

func (a *wssShadowMirrorAccumulator) addKinds(encoded string) {
	if a == nil || strings.TrimSpace(encoded) == "" {
		return
	}
	for _, part := range strings.Split(encoded, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, values, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		pieces := strings.Split(values, "/")
		if len(pieces) != 4 {
			continue
		}
		refBytes, okRefBytes := parseNonNegativeInt(pieces[0])
		bytes, okBytes := parseNonNegativeInt(pieces[1])
		refSegments, okRefSegments := parseNonNegativeInt(pieces[2])
		segments, okSegments := parseNonNegativeInt(pieces[3])
		if !okRefBytes || !okBytes || !okRefSegments || !okSegments {
			continue
		}
		row := a.byKind[name]
		if row == nil {
			row = &wssShadowMirrorKindSummary{Kind: name}
			a.byKind[name] = row
		}
		row.ReferenceableBytes += refBytes
		row.Bytes += bytes
		row.ReferenceableSegments += refSegments
		row.Segments += segments
	}
}

func (a *wssShadowMirrorAccumulator) finalize() *wssShadowMirrorSummary {
	if a == nil || a.summary.Requests == 0 {
		return nil
	}
	out := a.summary
	out.ReferenceableBytePct = pct(out.ReferenceableBytes, out.Bytes)
	out.NormalizedReferenceableBytePct = pct(out.NormalizedReferenceableBytes, out.NormalizedBytes)
	keys := make([]string, 0, len(a.byKind))
	for key := range a.byKind {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		row := *a.byKind[key]
		row.ReferenceableBytePct = pct(row.ReferenceableBytes, row.Bytes)
		out.NormalizedReferenceableBytesByKind = append(out.NormalizedReferenceableBytesByKind, row)
	}
	return &out
}

func (a *wssShadowMirrorCandidateAccumulator) add(summary dbg.RequestSummary, shape string) {
	if a == nil || len(summary.DebugFacts) == 0 {
		return
	}
	shape = strings.TrimSpace(shape)
	if shape == "" {
		shape = "unknown"
	}
	if a.rows == nil {
		a.rows = make(map[string]*wssShadowMirrorCandidate)
	}
	if refBytes := intFact(summary.DebugFacts, "wss.shadow_mirror_referenceable_bytes"); refBytes > 0 {
		bytes := intFact(summary.DebugFacts, "wss.shadow_mirror_bytes")
		segments := intFact(summary.DebugFacts, "wss.shadow_mirror_blocks")
		refSegments := intFact(summary.DebugFacts, "wss.shadow_mirror_referenceable_blocks")
		a.addRow(summary, shape, "exact_block", refBytes, bytes, refSegments, segments)
	}
	for _, row := range parseWSSShadowMirrorKindRows(summary.DebugFacts["wss.shadow_mirror_normalized_density_by_kind"]) {
		if row.ReferenceableBytes <= 0 {
			continue
		}
		a.addRow(summary, shape, row.Kind, row.ReferenceableBytes, row.Bytes, row.ReferenceableSegments, row.Segments)
	}
}

func (a *wssShadowMirrorCandidateAccumulator) addRow(summary dbg.RequestSummary, shape, kind string, refBytes, bytes, refSegments, segments int) {
	if refBytes <= 0 {
		return
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "unknown"
	}
	key := shape + "\x00" + kind
	row := a.rows[key]
	if row == nil {
		row = &wssShadowMirrorCandidate{
			RequestShape:      shape,
			Kind:              kind,
			CandidateLane:     wssShadowMirrorCandidateLane(shape, kind),
			RecommendedAction: wssShadowMirrorCandidateAction(shape, kind),
			sessionRows:       make(map[string]*wssShadowMirrorCandidateSession),
		}
		a.rows[key] = row
	}
	row.Requests++
	row.ReferenceableBytes += refBytes
	row.Bytes += maxInt(0, bytes)
	row.ReferenceableSegments += maxInt(0, refSegments)
	row.Segments += maxInt(0, segments)
	candidateTokens := tokens.Estimate(refBytes)
	row.CandidateLocalTokensEstimate += candidateTokens
	row.ProviderInputTokens += maxInt(0, summary.ProviderInputTokens)
	row.ProviderCachedTokens += maxInt(0, summary.ProviderCachedTokens)
	row.ProviderOutputTokens += maxInt(0, summary.ProviderOutputTokens)
	row.LocalSavedTokens += maxInt(0, summary.Tokens.Saved)
	if len(summary.Errors) > 0 {
		row.ErrorRequests++
	}
	if wssAuditHasUpstreamError(summary) {
		row.UpstreamErrorRequests++
	}
	if wssAuditHasHTTP400Error(summary) {
		row.HTTP400ErrorRequests++
	}
	sessionID := strings.TrimSpace(summary.SessionID)
	if sessionID == "" {
		sessionID = "(missing)"
	}
	session := row.sessionRows[sessionID]
	if session == nil {
		session = &wssShadowMirrorCandidateSession{SessionID: sessionID}
		row.sessionRows[sessionID] = session
	}
	session.Requests++
	session.ReferenceableBytes += refBytes
	session.CandidateLocalTokensEstimate += candidateTokens
	session.ProviderInputTokens += maxInt(0, summary.ProviderInputTokens)
	session.ProviderCachedTokens += maxInt(0, summary.ProviderCachedTokens)
	session.LocalSavedTokens += maxInt(0, summary.Tokens.Saved)
	addWSSShadowMirrorCandidateSignals(summary, row, session)
	if wssShadowMirrorRowPromotionOpen(summary, row.CandidateLane) {
		row.PromotionOpenRequests++
		row.PromotionOpenReferenceableBytes += refBytes
		row.PromotionOpenCandidateTokens += candidateTokens
		row.PromotionOpenLocalSavedTokens += maxInt(0, summary.Tokens.Saved)
		session.PromotionOpenRequests++
		session.PromotionOpenReferenceableBytes += refBytes
		session.PromotionOpenCandidateTokens += candidateTokens
		session.PromotionOpenLocalSavedTokens += maxInt(0, summary.Tokens.Saved)
	}
	if len(summary.Errors) > 0 {
		session.ErrorRequests++
	}
	if wssAuditHasUpstreamError(summary) {
		session.UpstreamErrorRequests++
	}
	if wssAuditHasHTTP400Error(summary) {
		session.HTTP400ErrorRequests++
	}
}

func addWSSShadowMirrorCandidateSignals(summary dbg.RequestSummary, row *wssShadowMirrorCandidate, session *wssShadowMirrorCandidateSession) {
	if row == nil || session == nil {
		return
	}
	if summary.PreviousResponseIDUsed || parseBoolFact(summary.DebugFacts["wss.previous_response_id"]) {
		row.PreviousResponseIDUsed++
		session.PreviousResponseIDUsed++
	}
	if parseBoolFact(summary.DebugFacts["wss.full_history_detached_previous_response"]) {
		row.DetachedPreviousResponse++
		session.DetachedPreviousResponse++
	}
	if parseBoolFact(summary.DebugFacts["wss.stateless_history_continuation"]) {
		row.StatelessContinuation++
		session.StatelessContinuation++
	}
	if parseBoolFact(summary.DebugFacts["wss.full_history_stateless_followup"]) {
		row.FullHistoryStatelessFollowup++
		session.FullHistoryStatelessFollowup++
	}
	if strings.TrimSpace(summary.DebugFacts["wss.structured_mutation_guard"]) != "" {
		row.StructuredMutationGuarded++
		session.StructuredMutationGuarded++
	}
	if parseBoolFact(summary.DebugFacts["wss.history_mutation_recovery_guard"]) {
		row.HistoryRecoveryGuarded++
		session.HistoryRecoveryGuarded++
	}
	if strings.TrimSpace(summary.DebugFacts["wss.cache_bust_demoted_mechanisms"]) != "" {
		row.CacheBustDemoted++
		session.CacheBustDemoted++
		addWSSAuditCountWithMissing(&row.CacheBustDemotedScopes, summary.DebugFacts["wss.cache_bust_demoted_scope"])
		addWSSAuditCountWithMissing(&session.CacheBustDemotedScopes, summary.DebugFacts["wss.cache_bust_demoted_scope"])
		addDelimitedWSSAuditCounts(&row.CacheBustDemotedClassKeys, summary.DebugFacts["wss.cache_bust_demoted_class_keys"])
		addDelimitedWSSAuditCounts(&session.CacheBustDemotedClassKeys, summary.DebugFacts["wss.cache_bust_demoted_class_keys"])
	}
	addWSSAuditCount(&row.EffectiveMutationGuards, summary.DebugFacts["wss.effective_mutation_guard"])
	addWSSAuditCount(&session.EffectiveMutationGuards, summary.DebugFacts["wss.effective_mutation_guard"])
	addWSSAuditCountWithMissing(&row.BySocketSeq, summary.DebugFacts["wss.socket_seq"])
	addWSSAuditCountWithMissing(&session.BySocketSeq, summary.DebugFacts["wss.socket_seq"])
}

func wssShadowMirrorRowPromotionOpen(summary dbg.RequestSummary, lane string) bool {
	if lane != "t417_class_b_server_state" {
		return false
	}
	if strings.TrimSpace(summary.SessionID) == "" ||
		len(summary.Errors) > 0 ||
		wssAuditHasUpstreamError(summary) ||
		wssAuditHasHTTP400Error(summary) {
		return false
	}
	if summary.PreviousResponseIDUsed || parseBoolFact(summary.DebugFacts["wss.previous_response_id"]) {
		return false
	}
	if strings.TrimSpace(summary.DebugFacts["wss.structured_mutation_guard"]) != "" ||
		parseBoolFact(summary.DebugFacts["wss.history_mutation_recovery_guard"]) ||
		strings.TrimSpace(summary.DebugFacts["wss.cache_bust_demoted_mechanisms"]) != "" {
		return false
	}
	return parseBoolFact(summary.DebugFacts["wss.full_history_detached_previous_response"]) ||
		parseBoolFact(summary.DebugFacts["wss.stateless_history_continuation"]) ||
		parseBoolFact(summary.DebugFacts["wss.full_history_stateless_followup"])
}

func (a *wssShadowMirrorCandidateAccumulator) finalize() []wssShadowMirrorCandidate {
	if a == nil || len(a.rows) == 0 {
		return nil
	}
	out := make([]wssShadowMirrorCandidate, 0, len(a.rows))
	for _, row := range a.rows {
		candidate := *row
		candidate.ReferenceableBytePct = pct(candidate.ReferenceableBytes, candidate.Bytes)
		candidate.IncrementalLocalTokensHeadroom = maxInt(0, candidate.CandidateLocalTokensEstimate-candidate.LocalSavedTokens)
		candidate.PromotionOpenHeadroom = maxInt(0, candidate.PromotionOpenCandidateTokens-candidate.PromotionOpenLocalSavedTokens)
		candidate.ErrorFree = candidate.ErrorRequests == 0 && candidate.UpstreamErrorRequests == 0 && candidate.HTTP400ErrorRequests == 0
		candidate.NextProofGate = wssShadowMirrorCandidateProofGate(candidate)
		candidate.PromotionBlockers = wssShadowMirrorCandidatePromotionBlockers(candidate)
		candidate.PromotionBlockerHeadroom = wssShadowMirrorBlockerHeadroom(candidate.PromotionBlockers, candidate.IncrementalLocalTokensHeadroom)
		candidate.PromotionStage = wssShadowMirrorPromotionStage(candidate.CandidateLane, candidate.PromotionBlockers)
		if candidate.CandidateLane == "t417_class_b_server_state" && candidate.PromotionOpenHeadroom > 0 && len(candidate.PromotionBlockers) > 0 {
			candidate.PromotionStage = "t417_partial_product_candidate"
		}
		candidate.TopSessions = finalizeWSSShadowMirrorCandidateSessions(candidate)
		candidate.sessionRows = nil
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IncrementalLocalTokensHeadroom != out[j].IncrementalLocalTokensHeadroom {
			return out[i].IncrementalLocalTokensHeadroom > out[j].IncrementalLocalTokensHeadroom
		}
		if out[i].ReferenceableBytes != out[j].ReferenceableBytes {
			return out[i].ReferenceableBytes > out[j].ReferenceableBytes
		}
		if out[i].RequestShape != out[j].RequestShape {
			return wssShadowMirrorShapeRank(out[i].RequestShape) < wssShadowMirrorShapeRank(out[j].RequestShape)
		}
		if out[i].ReferenceableBytePct != out[j].ReferenceableBytePct {
			return out[i].ReferenceableBytePct > out[j].ReferenceableBytePct
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func finalizeWSSShadowMirrorCandidateSessions(candidate wssShadowMirrorCandidate) []wssShadowMirrorCandidateSession {
	if len(candidate.sessionRows) == 0 {
		return nil
	}
	out := make([]wssShadowMirrorCandidateSession, 0, len(candidate.sessionRows))
	for _, row := range candidate.sessionRows {
		session := *row
		session.IncrementalLocalTokensHeadroom = maxInt(0, session.CandidateLocalTokensEstimate-session.LocalSavedTokens)
		session.PromotionOpenHeadroom = maxInt(0, session.PromotionOpenCandidateTokens-session.PromotionOpenLocalSavedTokens)
		session.ErrorFree = session.ErrorRequests == 0 && session.UpstreamErrorRequests == 0 && session.HTTP400ErrorRequests == 0
		session.NextProofGate = wssShadowMirrorCandidateSessionProofGate(candidate, session)
		session.PromotionBlockers = wssShadowMirrorCandidateSessionPromotionBlockers(candidate, session)
		session.PromotionBlockerHeadroom = wssShadowMirrorBlockerHeadroom(session.PromotionBlockers, session.IncrementalLocalTokensHeadroom)
		session.PromotionStage = wssShadowMirrorPromotionStage(candidate.CandidateLane, session.PromotionBlockers)
		if candidate.CandidateLane == "t417_class_b_server_state" && session.PromotionOpenHeadroom > 0 && len(session.PromotionBlockers) > 0 {
			session.PromotionStage = "t417_partial_product_candidate"
		}
		out = append(out, session)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IncrementalLocalTokensHeadroom != out[j].IncrementalLocalTokensHeadroom {
			return out[i].IncrementalLocalTokensHeadroom > out[j].IncrementalLocalTokensHeadroom
		}
		if out[i].ReferenceableBytes != out[j].ReferenceableBytes {
			return out[i].ReferenceableBytes > out[j].ReferenceableBytes
		}
		return out[i].SessionID < out[j].SessionID
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func wssShadowMirrorCandidateSessionProofGate(candidate wssShadowMirrorCandidate, session wssShadowMirrorCandidateSession) string {
	if !session.ErrorFree {
		return "fix_or_exclude_erroring_lineage_before_promotion"
	}
	return wssShadowMirrorCandidateProofGate(candidate)
}

func wssShadowMirrorCandidateProofGate(candidate wssShadowMirrorCandidate) string {
	if !candidate.ErrorFree {
		return "fix_or_exclude_erroring_shape_before_promotion"
	}
	switch candidate.CandidateLane {
	case "t417_class_b_server_state":
		return "t417_exact_lineage_net_positive_zero400_gate"
	case "t405_t354_stateful_delta":
		return "t405_t354_downstream_state_zero400_gate"
	case "t406_t418_parser_frontier":
		return "t418_command_output_first_or_t406_stateful_safe_parser_gate"
	default:
		return "shape_resolution_gate"
	}
}

func wssShadowMirrorCandidatePromotionBlockers(candidate wssShadowMirrorCandidate) []string {
	var blockers []string
	if candidate.IncrementalLocalTokensHeadroom <= 0 {
		blockers = append(blockers, "no_incremental_local_headroom")
	}
	if !candidate.ErrorFree {
		blockers = append(blockers, "erroring_shape")
	}
	if candidate.RequestShape == "unknown" || strings.TrimSpace(candidate.RequestShape) == "" {
		blockers = append(blockers, "unknown_request_shape")
	}
	switch candidate.CandidateLane {
	case "t417_class_b_server_state":
		blockers = append(blockers, wssShadowMirrorClassBPromotionBlockers(
			candidate.PreviousResponseIDUsed,
			candidate.StructuredMutationGuarded,
			candidate.HistoryRecoveryGuarded,
			candidate.CacheBustDemoted,
			candidate.CacheBustDemotedScopes,
			candidate.CacheBustDemotedClassKeys,
			candidate.DetachedPreviousResponse,
			candidate.StatelessContinuation,
			candidate.FullHistoryStatelessFollowup,
		)...)
	case "t405_t354_stateful_delta":
		blockers = append(blockers, "requires_downstream_state_zero400_gate")
	case "t406_t418_parser_frontier":
		blockers = append(blockers, "requires_command_output_first_parser_gate")
	default:
		blockers = append(blockers, "requires_shape_resolution")
	}
	return dedupeStrings(blockers)
}

func wssShadowMirrorCandidateSessionPromotionBlockers(candidate wssShadowMirrorCandidate, session wssShadowMirrorCandidateSession) []string {
	var blockers []string
	if session.IncrementalLocalTokensHeadroom <= 0 {
		blockers = append(blockers, "no_incremental_local_headroom")
	}
	if !session.ErrorFree {
		blockers = append(blockers, "erroring_lineage")
	}
	if strings.TrimSpace(session.SessionID) == "" || session.SessionID == "(missing)" {
		blockers = append(blockers, "missing_session_id")
	}
	switch candidate.CandidateLane {
	case "t417_class_b_server_state":
		blockers = append(blockers, wssShadowMirrorClassBPromotionBlockers(
			session.PreviousResponseIDUsed,
			session.StructuredMutationGuarded,
			session.HistoryRecoveryGuarded,
			session.CacheBustDemoted,
			session.CacheBustDemotedScopes,
			session.CacheBustDemotedClassKeys,
			session.DetachedPreviousResponse,
			session.StatelessContinuation,
			session.FullHistoryStatelessFollowup,
		)...)
	case "t405_t354_stateful_delta":
		blockers = append(blockers, "requires_downstream_state_zero400_gate")
	case "t406_t418_parser_frontier":
		blockers = append(blockers, "requires_command_output_first_parser_gate")
	default:
		blockers = append(blockers, "requires_shape_resolution")
	}
	return dedupeStrings(blockers)
}

func wssShadowMirrorClassBPromotionBlockers(previousResponseIDUsed, structuredMutationGuarded, historyRecoveryGuarded, cacheBustDemoted int, cacheBustDemotedScopes, cacheBustDemotedClassKeys map[string]int, detachedPreviousResponse, statelessContinuation, fullHistoryStatelessFollowup int) []string {
	var blockers []string
	if previousResponseIDUsed > 0 {
		blockers = append(blockers, "mixed_previous_response_state_requires_exact_lineage_split")
	}
	if structuredMutationGuarded > 0 {
		blockers = append(blockers, "structured_mutation_guard_requires_exact_release_latch")
	}
	if historyRecoveryGuarded > 0 {
		blockers = append(blockers, "history_recovery_guard_requires_lineage_reset")
	}
	if cacheBustDemoted > 0 {
		switch {
		case len(cacheBustDemotedClassKeys) > 0:
			blockers = append(blockers, "cache_bust_demotion_present_exact_class_scope")
		case len(cacheBustDemotedScopes) > 0:
			blockers = append(blockers, "cache_bust_demotion_present_prompt_scope")
		default:
			blockers = append(blockers, "cache_bust_demotion_scope_unknown")
		}
	}
	if detachedPreviousResponse+statelessContinuation+fullHistoryStatelessFollowup == 0 {
		blockers = append(blockers, "missing_detached_or_stateless_followup_signal")
	}
	return blockers
}

func wssShadowMirrorBlockerHeadroom(blockers []string, headroom int) map[string]int {
	if len(blockers) == 0 || headroom <= 0 {
		return nil
	}
	out := make(map[string]int, len(blockers))
	for _, blocker := range blockers {
		blocker = strings.TrimSpace(blocker)
		if blocker == "" {
			continue
		}
		out[blocker] = headroom
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func wssShadowMirrorPromotionStage(lane string, blockers []string) string {
	for _, blocker := range blockers {
		if blocker == "erroring_shape" || blocker == "erroring_lineage" {
			return "not_safe_erroring"
		}
		if blocker == "no_incremental_local_headroom" {
			return "not_economic"
		}
	}
	if len(blockers) == 0 {
		return "product_candidate_no_observed_blockers"
	}
	switch lane {
	case "t417_class_b_server_state":
		return "t417_lineage_candidate_needs_engineering"
	case "t405_t354_stateful_delta":
		return "t405_t354_candidate_needs_downstream_state_gate"
	case "t406_t418_parser_frontier":
		return "t418_parser_candidate_needs_release_gate"
	default:
		return "needs_shape_resolution"
	}
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func parseWSSShadowMirrorKindRows(encoded string) []wssShadowMirrorKindSummary {
	if strings.TrimSpace(encoded) == "" {
		return nil
	}
	var rows []wssShadowMirrorKindSummary
	for _, part := range strings.Split(encoded, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, values, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		pieces := strings.Split(values, "/")
		if len(pieces) != 4 {
			continue
		}
		refBytes, okRefBytes := parseNonNegativeInt(pieces[0])
		bytes, okBytes := parseNonNegativeInt(pieces[1])
		refSegments, okRefSegments := parseNonNegativeInt(pieces[2])
		segments, okSegments := parseNonNegativeInt(pieces[3])
		if !okRefBytes || !okBytes || !okRefSegments || !okSegments {
			continue
		}
		rows = append(rows, wssShadowMirrorKindSummary{
			Kind:                  strings.TrimSpace(name),
			ReferenceableBytes:    refBytes,
			Bytes:                 bytes,
			ReferenceableSegments: refSegments,
			Segments:              segments,
		})
	}
	return rows
}

func wssShadowMirrorShapeRank(shape string) int {
	switch strings.TrimSpace(shape) {
	case "full_history":
		return 0
	case "delta":
		return 1
	case "root":
		return 2
	default:
		return 3
	}
}

func wssShadowMirrorCandidateLane(shape, kind string) string {
	switch strings.TrimSpace(shape) {
	case "full_history":
		return "t417_class_b_server_state"
	case "delta":
		return "t405_t354_stateful_delta"
	case "root":
		return "t406_t418_parser_frontier"
	default:
		return "capture_shape_resolution"
	}
}

func wssShadowMirrorCandidateAction(shape, kind string) string {
	shape = strings.TrimSpace(shape)
	kind = strings.TrimSpace(kind)
	switch {
	case shape == "full_history" && kind == "codex_exec_payload":
		return "rank for T417 Class-B continuation or T418 command-output-first recovery"
	case shape == "full_history":
		return "rank for T417 exact lineage-scoped continuation"
	case shape == "delta":
		return "rank for T405/T354 stateful-delta proof, keep current delta guards until downstream-clean"
	case shape == "root":
		return "rank for T406/T418 parser/default-on command-output classes"
	default:
		return "improve shape attribution before product promotion"
	}
}

func wssShadowMirrorFactsPresent(facts map[string]string) bool {
	for key := range facts {
		if strings.HasPrefix(key, "wss.shadow_mirror_") {
			return true
		}
	}
	return false
}

func intFact(facts map[string]string, key string) int {
	value, ok := parseNonNegativeInt(facts[key])
	if !ok {
		return 0
	}
	return value
}

func parseNonNegativeInt(value string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func pct(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func loadWSSAuditTelemetry(path string) ([]control.ProxyLayer0PolicyEntry, []control.ProxyLayer0CacheEntry, control.SavingsSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, control.SavingsSummary{}, fmt.Errorf("read admin state file %s: %w", path, err)
	}
	state, err := parseAdminStateJSON(data)
	if err != nil {
		return nil, nil, control.SavingsSummary{}, err
	}
	return state.Savings.ProxyLayer0Policy, state.Savings.ProxyLayer0Cache, state.Savings, nil
}

func wssAuditRouteMode(summary dbg.RequestSummary) string {
	if strings.TrimSpace(summary.RouteMode) != "" {
		return strings.TrimSpace(summary.RouteMode)
	}
	if summary.Plan != nil && strings.TrimSpace(summary.Plan.RouteMode) != "" {
		return strings.TrimSpace(summary.Plan.RouteMode)
	}
	return ""
}

func wssAuditIsWSS(summary dbg.RequestSummary, route string) bool {
	route = strings.ToLower(route)
	if strings.Contains(route, "websocket") || strings.Contains(route, "wss") {
		return true
	}
	path := strings.ToLower(summary.Path)
	return strings.Contains(path, "/backend-api/codex/responses") ||
		strings.Contains(path, "/backend-api/codex-bridge/responses")
}

func wssAuditIsPhaseF(route string) bool {
	route = strings.ToLower(strings.TrimSpace(route))
	return route == "websocket_phasef" || route == "wss_phasef" ||
		strings.Contains(route, "phasef")
}

func wssAuditContentClasses(summary dbg.RequestSummary) []string {
	if summary.Plan == nil {
		return nil
	}
	out := make([]string, 0, len(summary.Plan.ContentClasses))
	for _, class := range summary.Plan.ContentClasses {
		class = strings.TrimSpace(class)
		if class != "" {
			out = append(out, class)
		}
	}
	return out
}

func wssAuditRequestShape(summary dbg.RequestSummary) string {
	if summary.DebugFacts == nil {
		return "unknown"
	}
	shape := strings.ToLower(strings.TrimSpace(summary.DebugFacts["wss.request_shape"]))
	switch shape {
	case "root", "delta", "full_history":
		return shape
	case "":
		return "unknown"
	default:
		return "unknown"
	}
}

func wssAuditResolveRequestShape(summary dbg.RequestSummary) wssAuditRequestShapeResolution {
	shape := wssAuditRequestShape(summary)
	if shape != "" && shape != "unknown" {
		return wssAuditRequestShapeResolution{Shape: shape, Source: "fact"}
	}
	if summary.PreviousResponseIDUsed {
		return wssAuditRequestShapeResolution{Shape: "delta", Source: "legacy_previous_response_id"}
	}
	if summary.DebugFacts != nil {
		if parseBoolFact(summary.DebugFacts["wss.previous_response_id"]) {
			return wssAuditRequestShapeResolution{Shape: "delta", Source: "legacy_previous_response_id_fact"}
		}
		if parseBoolFact(summary.DebugFacts["wss.delta_shape"]) {
			return wssAuditRequestShapeResolution{Shape: "delta", Source: "legacy_delta_shape_fact"}
		}
	}
	return wssAuditRequestShapeResolution{Shape: "unknown", Source: "unresolved"}
}

func parseBoolFact(value string) bool {
	ok, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && ok
}

func wssAuditNotes(report wssAuditReport) []string {
	var notes []string
	if report.WSSRequests == 0 {
		notes = append(notes, "No WSS request summaries found in this decisions log.")
	}
	if report.PhaseFRequests > 0 && report.MissingSessionID > 0 {
		notes = append(notes, "Some WSS Phase-F requests have no session id; repeat-read cache continuity is unsafe for those frames.")
	}
	if report.PhaseFRequests > 0 && report.PositiveSavings == 0 {
		notes = append(notes, "Route telemetry is present, but this log has no positive input-token savings.")
	}
	if report.PhaseFRequests > 0 && report.PreviousResponseIDUsed == 0 {
		notes = append(notes, "No previous_response_id usage observed; this may be a first-turn or non-delta capture.")
	}
	if report.PhaseFRequests > 0 && len(report.RequestShapes) == 0 {
		notes = append(notes, "No WSS request-shape facts observed; Class-A/Class-B routing cannot be proven from this log.")
	}
	if report.PhaseFRequests > 0 && report.RequestShapes["unknown"] > 0 {
		notes = append(notes, "Some Phase-F rows have unknown request shape; use a fresh capture before treating shape percentages as complete.")
	}
	if report.ResolvedRequestShapes["delta"] > report.RequestShapes["delta"] {
		notes = append(notes, "Some legacy unknown Phase-F rows were conservatively resolved as delta from previous_response_id evidence; observed request-shape facts remain separate.")
	}
	if report.ResolvedRequestShapes["unknown"] > 0 {
		notes = append(notes, "Some Phase-F rows remain shape-unresolved after conservative legacy inference; do not use them for root/full-history guard widening.")
	}
	if report.RequestShapes["full_history"] > 0 {
		notes = append(notes, "Full-history Class-B rows observed; correlate them with savings, socket lifecycle, and upstream-error counters before widening guards.")
		if report.FullHistory != nil && report.FullHistory.ProviderInputTokens == 0 {
			notes = append(notes, "Full-history rows have no provider input-token usage; run a fresh capture before making Class-B cost claims.")
		}
		if report.FullHistory != nil && report.FullHistory.BySocketSeq["(missing)"] > 0 {
			notes = append(notes, "Some full-history rows have no socket sequence fact; run slimference debug wss-sockets for reconnect-cause attribution before guard changes.")
		}
		if report.FullHistory != nil && report.FullHistory.UpstreamErrorRequests > 0 {
			notes = append(notes, "Full-history rows overlap upstream-error evidence; treat this as a stability boundary, not a savings opportunity.")
		}
	} else if report.PhaseFRequests > 0 && report.RequestShapes["delta"] > 0 {
		notes = append(notes, "This capture has delta-shaped Phase-F traffic but no full-history Class-B rows; do not use it to prove T354 Class-B widening.")
	}
	if report.ReReadCount > 0 {
		notes = append(notes, "WSS re-read canary observed repeated tool keys; review alongside savings to distinguish useful repeat reads from possible context-recall pressure.")
	}
	if report.PhaseFRequests > 0 && len(report.HistoryReducers) == 0 {
		notes = append(notes, "No T353 stale-read / obsolete-prune evidence observed; history reducer calibration needs a fresher capture or a history-heavy workload.")
	}
	if report.PhaseFRequests > 0 && len(report.FootprintEconomics) == 0 {
		notes = append(notes, "No T359 footprint economics observed; threshold scaling constants need a fresher capture with footprint_score_bucket evidence.")
	}
	if report.FootprintCoverage != nil && report.FootprintCoverage.MissingFootprint > 0 {
		notes = append(notes, "Token-bearing WSS evidence decisions without footprint_score_bucket were observed; treat this as stale pre-T359 evidence or emission drift before threshold scaling.")
	}
	if report.FootprintCoverage != nil && report.FootprintCoverage.MissingRemainingTurnsEstimate > 0 {
		notes = append(notes, "Footprint-scored WSS evidence decisions without wss.remaining_turns_estimate were observed; treat this as stale pre-EMA evidence before threshold scaling.")
	}
	if report.PhaseFRequests > 0 && report.ShadowMirror == nil {
		notes = append(notes, "No T355 shadow-mirror telemetry observed; this capture may predate normalized mirror facts or contain no text blocks.")
	}
	if report.ShadowMirror != nil && report.ShadowMirror.NormalizedBytes > 0 && report.ShadowMirror.NormalizedReferenceableBytes == 0 {
		notes = append(notes, "T355 shadow mirror saw normalized bytes but no normalized referenceable bytes in this capture.")
	}
	if report.ShadowMirror != nil && report.ShadowMirror.NormalizedReferenceableBytes > 0 {
		notes = append(notes, "T355 shadow mirror found normalized referenceable bytes; treat this as measurement evidence only, not a product mutation proof.")
	}
	return notes
}

func wssAuditGateFailures(report wssAuditReport, flags wssAuditFlags) []string {
	var failures []string
	if flags.expectDistinctSessions > 0 && report.UniqueSessions < flags.expectDistinctSessions {
		failures = append(failures, fmt.Sprintf("expected at least %d distinct non-empty WSS session ids, got %d", flags.expectDistinctSessions, report.UniqueSessions))
	}
	if flags.minPhaseF > 0 && report.PhaseFRequests < flags.minPhaseF {
		failures = append(failures, fmt.Sprintf("expected at least %d Phase-F request summaries, got %d", flags.minPhaseF, report.PhaseFRequests))
	}
	if flags.minFullHistory > 0 && report.RequestShapes["full_history"] < flags.minFullHistory {
		failures = append(failures, fmt.Sprintf("expected at least %d full-history Phase-F request summaries, got %d", flags.minFullHistory, report.RequestShapes["full_history"]))
	}
	if flags.requireSavings && report.PositiveSavings == 0 {
		failures = append(failures, "expected at least one positive input-token savings request")
	}
	if flags.requireHistoryEvidence && len(report.HistoryReducers) == 0 {
		failures = append(failures, "expected stale-read or obsolete-prune history reducer evidence")
	}
	if flags.requireFootprintEvidence && len(report.FootprintEconomics) == 0 {
		failures = append(failures, "expected footprint-score economics evidence")
	}
	if flags.requireFootprintEvidence && report.FootprintCoverage != nil && report.FootprintCoverage.MissingRemainingTurnsEstimate > 0 {
		failures = append(failures, "expected footprint evidence with wss.remaining_turns_estimate")
	}
	return failures
}

func writeWSSAuditText(w io.Writer, report wssAuditReport) {
	fmt.Fprintf(w, "=== WSS Audit: %s ===\n", filepath.Base(report.Path))
	if report.Since != nil {
		fmt.Fprintf(w, "Since:                   %s\n", report.Since.Format(time.RFC3339))
	}
	fmt.Fprintf(w, "Requests analyzed:        %d\n", report.Requests)
	fmt.Fprintf(w, "WSS requests:             %d\n", report.WSSRequests)
	fmt.Fprintf(w, "Phase-F requests:         %d\n", report.PhaseFRequests)
	fmt.Fprintf(w, "unique session ids:       %d\n", report.UniqueSessions)
	fmt.Fprintf(w, "missing session ids:      %d\n", report.MissingSessionID)
	fmt.Fprintf(w, "previous_response_id:     %d\n", report.PreviousResponseIDUsed)
	if len(report.RequestShapes) > 0 {
		fmt.Fprintf(w, "request shapes:           %s\n", formatWSSAuditCounts(report.RequestShapes))
	}
	if len(report.ResolvedRequestShapes) > 0 {
		fmt.Fprintf(w, "resolved request shapes:  %s\n", formatWSSAuditCounts(report.ResolvedRequestShapes))
	}
	if len(report.RequestShapeSources) > 0 {
		fmt.Fprintf(w, "request-shape sources:    %s\n", formatWSSAuditCounts(report.RequestShapeSources))
	}
	fmt.Fprintf(w, "re-read requests/count:   %d / %d\n", report.ReReadRequests, report.ReReadCount)
	fmt.Fprintf(w, "positive savings reqs:    %d\n", report.PositiveSavings)
	fmt.Fprintf(w, "input tokens saved:       %d\n", report.TokensSaved)
	fmt.Fprintf(w, "gate:                     %s\n", passFail(report.GatePassed))
	if len(report.RouteModes) > 0 {
		fmt.Fprintln(w, "\nRoutes:")
		for _, key := range sortedStringKeys(report.RouteModes) {
			fmt.Fprintf(w, "  %-24s %d\n", key, report.RouteModes[key])
		}
	}
	if len(report.ContentClasses) > 0 {
		fmt.Fprintln(w, "\nContent classes:")
		for _, key := range sortedStringKeys(report.ContentClasses) {
			fmt.Fprintf(w, "  %-24s %d\n", key, report.ContentClasses[key])
		}
	}
	if len(report.ShapeEconomics) > 0 {
		fmt.Fprintln(w, "\nShape economics:")
		for _, row := range report.ShapeEconomics {
			fmt.Fprintf(w, "  %-12s requests=%d sources=%s provider=%d/%d/%.2f%% out=%d local=%d->%d saved=%d net=%d %.2f%% cache_read/create=%d/%d errors=%d/%d/%d\n",
				row.Shape,
				row.Requests,
				formatWSSAuditCounts(row.Sources),
				row.ProviderInputTokens,
				row.ProviderCachedTokens,
				row.ProviderCachedPct,
				row.ProviderOutputTokens,
				row.OriginalTokens,
				row.FinalTokens,
				row.LocalSavedTokens,
				row.NetSavedTokens,
				row.LocalSavedPct,
				row.CacheReadTokens,
				row.CacheCreateTokens,
				row.ErrorRequests,
				row.UpstreamErrorRequests,
				row.HTTP400ErrorRequests)
		}
	}
	if report.FullHistory != nil {
		fmt.Fprintln(w, "\nFull-history Class-B:")
		fmt.Fprintf(w, "  requests/sessions:       %d / %d\n", report.FullHistory.Requests, report.FullHistory.Sessions)
		fmt.Fprintf(w, "  missing session ids:     %d\n", report.FullHistory.MissingSessionID)
		fmt.Fprintf(w, "  previous_response_id:    %d\n", report.FullHistory.PreviousResponseIDUsed)
		fmt.Fprintf(w, "  provider in/cache/out:   %d / %d / %d\n",
			report.FullHistory.ProviderInputTokens,
			report.FullHistory.ProviderCachedTokens,
			report.FullHistory.ProviderOutputTokens)
		fmt.Fprintf(w, "  cache read/create:       %d / %d\n",
			report.FullHistory.CacheReadTokens,
			report.FullHistory.CacheCreateTokens)
		fmt.Fprintf(w, "  local original/final:    %d / %d\n",
			report.FullHistory.OriginalTokens,
			report.FullHistory.FinalTokens)
		fmt.Fprintf(w, "  local saved/net:         %d / %d\n",
			report.FullHistory.SavedTokens,
			report.FullHistory.NetSavedTokens)
		fmt.Fprintf(w, "  errors/upstream/400:     %d / %d / %d\n",
			report.FullHistory.ErrorRequests,
			report.FullHistory.UpstreamErrorRequests,
			report.FullHistory.HTTP400ErrorRequests)
		fmt.Fprintf(w, "  client families:         %s\n", formatWSSAuditCounts(report.FullHistory.ByClientFamily))
		fmt.Fprintf(w, "  socket seqs:             %s\n", formatWSSAuditCounts(report.FullHistory.BySocketSeq))
		fmt.Fprintf(w, "  socket closes:           %s\n", formatWSSAuditCounts(report.FullHistory.BySocketCloseInitiator))
	}
	if len(report.Policy) > 0 {
		fmt.Fprintln(w, "\nPolicy decisions:")
		if report.PolicySource != "" {
			fmt.Fprintf(w, "  source: %s\n", report.PolicySource)
		}
		for _, entry := range report.Policy {
			fmt.Fprintf(w, "  %s/%s/%s %s: %d\n",
				valueOrDash(entry.Route), entry.Mechanism, entry.Action, entry.Reason, entry.Count)
		}
	}
	if len(report.Cache) > 0 {
		fmt.Fprintln(w, "\nCache decisions:")
		if report.PolicySource != "" {
			fmt.Fprintf(w, "  source: %s\n", report.PolicySource)
		}
		for _, entry := range report.Cache {
			fmt.Fprintf(w, "  %s/%s/%s %s: %d\n",
				valueOrDash(entry.Route), entry.Mechanism, entry.Action, entry.Reason, entry.Count)
		}
	}
	if report.ChunkDedupReferences > 0 || report.ChunkDedupRefBytes > 0 || report.ChunkDedupInputBytes > 0 {
		fmt.Fprintln(w, "\nChunk dedup density:")
		if report.PolicySource != "" {
			fmt.Fprintf(w, "  source: %s\n", report.PolicySource)
		}
		fmt.Fprintf(w, "  references:              %d\n", report.ChunkDedupReferences)
		fmt.Fprintf(w, "  referenced bytes:        %d\n", report.ChunkDedupRefBytes)
		fmt.Fprintf(w, "  input bytes:             %d\n", report.ChunkDedupInputBytes)
	}
	if len(report.HistoryReducers) > 0 {
		fmt.Fprintln(w, "\nHistory reducers:")
		for _, row := range report.HistoryReducers {
			fmt.Fprintf(w, "  %-16s decisions=%d applied=%d full_pass=%d skipped=%d failed_open=%d saved=%d net=%d footprint=%d reasons=%s\n",
				row.Mechanism,
				row.Decisions,
				row.Applied,
				row.FullPass,
				row.Skipped,
				row.FailedOpen,
				row.SavedTokens,
				row.NetTokens,
				row.FootprintScore,
				formatWSSAuditCounts(row.Reasons),
			)
		}
	}
	if len(report.FootprintEconomics) > 0 {
		fmt.Fprintln(w, "\nFootprint economics:")
		for _, row := range report.FootprintEconomics {
			fmt.Fprintf(w, "  bucket=%-10s turn=%-11s shape=%-12s decisions=%d applied=%d full_pass=%d saved=%d net=%d footprint=%d mechanisms=%s cache=%s\n",
				row.Bucket,
				row.TurnBand,
				row.RequestShape,
				row.Decisions,
				row.Applied,
				row.FullPass,
				row.SavedTokens,
				row.NetTokens,
				row.FootprintScore,
				formatWSSAuditCounts(row.Mechanisms),
				formatWSSAuditCounts(row.CacheImpact),
			)
		}
	}
	if report.FootprintCoverage != nil {
		fmt.Fprintln(w, "\nFootprint coverage:")
		fmt.Fprintf(w, "  token decisions:         %d\n", report.FootprintCoverage.TokenDecisions)
		fmt.Fprintf(w, "  applied token decisions: %d\n", report.FootprintCoverage.AppliedTokenDecisions)
		fmt.Fprintf(w, "  with footprint:          %d\n", report.FootprintCoverage.WithFootprint)
		fmt.Fprintf(w, "  missing footprint:       %d\n", report.FootprintCoverage.MissingFootprint)
		fmt.Fprintf(w, "  applied missing:         %d\n", report.FootprintCoverage.AppliedMissingFootprint)
		fmt.Fprintf(w, "  with remaining-turn est: %d\n", report.FootprintCoverage.WithRemainingTurnsEstimate)
		fmt.Fprintf(w, "  missing remaining-turn:  %d\n", report.FootprintCoverage.MissingRemainingTurnsEstimate)
		fmt.Fprintf(w, "  saved/missing saved:     %d / %d\n",
			report.FootprintCoverage.SavedTokens,
			report.FootprintCoverage.MissingSavedTokens)
		fmt.Fprintf(w, "  mechanisms:              %s\n", formatWSSAuditCounts(report.FootprintCoverage.ByMechanism))
		fmt.Fprintf(w, "  missing mechanisms:      %s\n", formatWSSAuditCounts(report.FootprintCoverage.MissingByMechanism))
		fmt.Fprintf(w, "  missing remaining mechs: %s\n", formatWSSAuditCounts(report.FootprintCoverage.MissingRemainingTurnsEstimateMechanism))
	}
	if report.ShadowMirror != nil {
		fmt.Fprintln(w, "\nShadow mirror density:")
		fmt.Fprintf(w, "  requests:                %d\n", report.ShadowMirror.Requests)
		fmt.Fprintf(w, "  exact blocks:            %d\n", report.ShadowMirror.Blocks)
		fmt.Fprintf(w, "  exact bytes:             %d\n", report.ShadowMirror.Bytes)
		fmt.Fprintf(w, "  exact referenceable:     %d blocks / %d bytes (%.2f%%)\n",
			report.ShadowMirror.ReferenceableBlocks,
			report.ShadowMirror.ReferenceableBytes,
			report.ShadowMirror.ReferenceableBytePct)
		fmt.Fprintf(w, "  normalized segments:     %d\n", report.ShadowMirror.NormalizedSegments)
		fmt.Fprintf(w, "  normalized bytes:        %d\n", report.ShadowMirror.NormalizedBytes)
		fmt.Fprintf(w, "  normalized referenceable:%d segments / %d bytes (%.2f%%)\n",
			report.ShadowMirror.NormalizedReferenceableSegments,
			report.ShadowMirror.NormalizedReferenceableBytes,
			report.ShadowMirror.NormalizedReferenceableBytePct)
		if len(report.ShadowMirror.NormalizedReferenceableBytesByKind) > 0 {
			fmt.Fprintln(w, "  by kind:")
			for _, row := range report.ShadowMirror.NormalizedReferenceableBytesByKind {
				fmt.Fprintf(w, "    %-24s %d/%d bytes %.2f%% (%d/%d segments)\n",
					row.Kind, row.ReferenceableBytes, row.Bytes, row.ReferenceableBytePct,
					row.ReferenceableSegments, row.Segments)
			}
		}
	}
	if len(report.ShadowMirrorCandidates) > 0 {
		fmt.Fprintln(w, "\nShadow mirror candidates:")
		for _, row := range report.ShadowMirrorCandidates {
			fmt.Fprintf(w, "  shape=%-12s kind=%-20s ref=%d/%d bytes %.2f%% candidate_tokens=%d headroom=%d open=%dreq/%dtok/%dheadroom segments=%d/%d requests=%d provider=%d/%d/%d saved=%d prev_id=%d detached=%d stateless=%d followup=%d struct_guard=%d recovery_guard=%d cache_bust=%d cache_scopes=%s cache_classes=%s eff_guards=%s sockets=%s errors=%d error_free=%v lane=%s gate=%s stage=%s blockers=%s blocker_headroom=%s top_sessions=%s action=%s\n",
				row.RequestShape,
				row.Kind,
				row.ReferenceableBytes,
				row.Bytes,
				row.ReferenceableBytePct,
				row.CandidateLocalTokensEstimate,
				row.IncrementalLocalTokensHeadroom,
				row.PromotionOpenRequests,
				row.PromotionOpenCandidateTokens,
				row.PromotionOpenHeadroom,
				row.ReferenceableSegments,
				row.Segments,
				row.Requests,
				row.ProviderInputTokens,
				row.ProviderCachedTokens,
				row.ProviderOutputTokens,
				row.LocalSavedTokens,
				row.PreviousResponseIDUsed,
				row.DetachedPreviousResponse,
				row.StatelessContinuation,
				row.FullHistoryStatelessFollowup,
				row.StructuredMutationGuarded,
				row.HistoryRecoveryGuarded,
				row.CacheBustDemoted,
				formatWSSAuditCounts(row.CacheBustDemotedScopes),
				formatWSSAuditCounts(row.CacheBustDemotedClassKeys),
				formatWSSAuditCounts(row.EffectiveMutationGuards),
				formatWSSAuditCounts(row.BySocketSeq),
				row.ErrorRequests,
				row.ErrorFree,
				row.CandidateLane,
				row.NextProofGate,
				row.PromotionStage,
				formatStringList(row.PromotionBlockers),
				formatWSSAuditCounts(row.PromotionBlockerHeadroom),
				formatWSSShadowMirrorCandidateSessions(row.TopSessions),
				row.RecommendedAction)
		}
	}
	if len(report.Sessions) > 0 {
		fmt.Fprintln(w, "\nSessions:")
		for _, session := range report.Sessions {
			fmt.Fprintf(w, "  %-32s requests=%d phasef=%d prev_id=%d shapes=%s resolved=%s reread=%d saved=%d\n",
				truncateMiddle(session.SessionID, 32), session.Requests, session.PhaseFRequests,
				session.PreviousResponseIDUsed, formatWSSAuditCounts(session.RequestShapes),
				formatWSSAuditCounts(session.ResolvedRequestShapes), session.ReReadCount, session.TokensSaved)
		}
	}
	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
	if len(report.GateFailures) > 0 {
		fmt.Fprintln(w, "\nGate failures:")
		for _, failure := range report.GateFailures {
			fmt.Fprintf(w, "  - %s\n", failure)
		}
	}
}

func formatWSSShadowMirrorCandidateSessions(rows []wssShadowMirrorCandidateSession) string {
	if len(rows) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf("%s:%d/%d/open=%dreq/%dtok/%dheadroom/pi=%d/pc=%d/prev=%d/det=%d/stateless=%d/followup=%d/guard=%d/cache_bust=%d/cache_classes=%s/sockets=%s/ok=%v/%s/stage=%s/blockers=%s/blocker_headroom=%s",
			truncateMiddle(row.SessionID, 24),
			row.IncrementalLocalTokensHeadroom,
			row.ReferenceableBytes,
			row.PromotionOpenRequests,
			row.PromotionOpenCandidateTokens,
			row.PromotionOpenHeadroom,
			row.ProviderInputTokens,
			row.ProviderCachedTokens,
			row.PreviousResponseIDUsed,
			row.DetachedPreviousResponse,
			row.StatelessContinuation,
			row.FullHistoryStatelessFollowup,
			row.StructuredMutationGuarded+row.HistoryRecoveryGuarded,
			row.CacheBustDemoted,
			formatWSSAuditCounts(row.CacheBustDemotedClassKeys),
			formatWSSAuditCounts(row.BySocketSeq),
			row.ErrorFree,
			row.NextProofGate,
			row.PromotionStage,
			formatStringList(row.PromotionBlockers),
			formatWSSAuditCounts(row.PromotionBlockerHeadroom)))
	}
	return strings.Join(parts, ",")
}

func formatStringList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, "|")
}

func formatWSSAuditCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(counts))
	for _, key := range sortedStringKeys(counts) {
		parts = append(parts, fmt.Sprintf("%s:%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}

func passFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func truncateMiddle(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	head := (maxLen - 3) / 2
	tail := maxLen - 3 - head
	return s[:head] + "..." + s[len(s)-tail:]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

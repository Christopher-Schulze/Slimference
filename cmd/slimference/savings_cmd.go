package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	"github.com/Christopher-Schulze/Slimference/internal/codexthreads"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

var _ = config.Defaults // keep config import alive even if no direct calls remain

// savingsFlags captures the CLI flags for `slimference savings`. T80.
type savingsFlags struct {
	json    bool
	csv     bool
	project string
}

// SavingsSummary collapses Layer 0 filter rows, proxy analytics, cache
// accounting, and measured decision-log savings into one canonical view.
// Returned by `slimference savings <period>` and surfaced via /admin if needed.
type SavingsSummary struct {
	Period                            string                    `json:"period"`
	Project                           string                    `json:"project,omitempty"`
	Layer0Runs                        int64                     `json:"layer0_runs"`
	Layer0SavedTokens                 int64                     `json:"layer0_saved_tokens"`
	Layer0SavedUSD                    float64                   `json:"layer0_saved_usd"`
	ProxyOrigTokens                   int64                     `json:"proxy_orig_tokens"`
	ProxyCompTokens                   int64                     `json:"proxy_comp_tokens"`
	ProxySavedTokens                  int64                     `json:"proxy_saved_tokens"`
	ProxyRequests                     int64                     `json:"proxy_requests"`
	ProviderReportedRequests          int64                     `json:"provider_reported_requests"`
	ProviderInputTokens               int64                     `json:"provider_input_tokens"`
	ProviderCachedTokens              int64                     `json:"provider_cached_tokens"`
	ProviderOutputTokens              int64                     `json:"provider_output_tokens"`
	OutputReduceInputOverheadTokens   int64                     `json:"output_reduce_input_overhead_tokens"`
	CacheReadDiscountTokenEquivalent  int64                     `json:"cache_read_discount_token_equivalent"`
	NetBillableEquivalentTokens       int64                     `json:"net_billable_equivalent_tokens"`
	CacheHits                         int64                     `json:"cache_hits"`
	CachedPriceRatio                  float64                   `json:"cached_price_ratio"`
	TotalSavedTokens                  int64                     `json:"total_saved_tokens"`
	TotalSavedUSD                     float64                   `json:"total_saved_usd"`
	USDPerMillion                     float64                   `json:"usd_per_million_tokens"`
	DecisionRequests                  int64                     `json:"decision_requests"`
	DecisionProviderInputTokens       int64                     `json:"decision_provider_input_tokens"`
	DecisionOriginalTokens            int64                     `json:"decision_original_tokens"`
	DecisionFinalTokens               int64                     `json:"decision_final_tokens"`
	DecisionAddedTokens               int64                     `json:"decision_added_tokens"`
	DecisionLocalSavedTokens          int64                     `json:"decision_local_saved_tokens"`
	DecisionNetSavedTokens            int64                     `json:"decision_net_saved_tokens"`
	DecisionNegativeEvents            int64                     `json:"decision_negative_events"`
	DecisionNegativeEventTokens       int64                     `json:"decision_negative_event_tokens"`
	DecisionOutputTokens              int64                     `json:"decision_output_tokens"`
	DecisionCacheReadTokens           int64                     `json:"decision_cache_read_tokens"`
	DecisionCacheCreateTokens         int64                     `json:"decision_cache_create_tokens"`
	DecisionCacheNetTokens            int64                     `json:"decision_cache_net_tokens"`
	DecisionCacheHitRequests          int64                     `json:"decision_cache_hit_requests"`
	DecisionCacheCreateRequests       int64                     `json:"decision_cache_create_requests"`
	DecisionCacheNegativeNetRequests  int64                     `json:"decision_cache_negative_net_requests"`
	DecisionCacheHitRate              float64                   `json:"decision_cache_hit_rate"`
	DecisionCachedShare               float64                   `json:"decision_cached_share"`
	DecisionEffectiveBilledTokens     int64                     `json:"decision_effective_billed_tokens"`
	DecisionCounterfactualTokens      int64                     `json:"decision_counterfactual_tokens"`
	DecisionUncachedCounterfactual    int64                     `json:"decision_uncached_counterfactual_tokens"`
	DecisionLocalSavingsRate          float64                   `json:"decision_s_local"`
	DecisionCombinedSavingsRate       float64                   `json:"decision_s_combined"`
	DecisionVsUncachedSavingsRate     float64                   `json:"decision_s_vs_uncached"`
	DecisionCacheStatus               string                    `json:"decision_cache_status,omitempty"`
	DecisionCodexRequests             int64                     `json:"decision_codex_requests"`
	DecisionCodexAttributedRequests   int64                     `json:"decision_codex_attributed_requests"`
	DecisionCodexUnattributedRequests int64                     `json:"decision_codex_unattributed_requests"`
	DecisionCodexUnattributedReasons  map[string]int64          `json:"decision_codex_unattributed_reasons,omitempty"`
	DecisionCodexAttributionRate      float64                   `json:"decision_codex_attribution_rate"`
	DecisionCodexAttributionStatus    string                    `json:"decision_codex_attribution_status,omitempty"`
	DecisionLayer0NetTokens           int64                     `json:"decision_layer0_net_tokens"`
	DecisionLayer1NetTokens           int64                     `json:"decision_layer1_net_tokens"`
	DecisionLayer2NetTokens           int64                     `json:"decision_layer2_net_tokens"`
	DecisionLayer3NetTokens           int64                     `json:"decision_layer3_net_tokens"`
	DecisionOutputReduceTokens        int64                     `json:"decision_output_reduce_tokens"`
	DecisionToolPruneTokens           int64                     `json:"decision_tool_prune_tokens"`
	DecisionFootprintScore            int64                     `json:"decision_footprint_score,omitempty"`
	DecisionFootprintScoreBuckets     map[string]int64          `json:"decision_footprint_score_buckets,omitempty"`
	DecisionCompoundedEstimateTokens  int64                     `json:"decision_compounded_estimate_tokens,omitempty"`
	DecisionEstimatedCostBeforeUSD    float64                   `json:"decision_estimated_cost_before_usd"`
	DecisionEstimatedCostAfterUSD     float64                   `json:"decision_estimated_cost_after_usd"`
	DecisionEstimatedCostSavedUSD     float64                   `json:"decision_estimated_cost_saved_usd"`
	Mechanisms                        []SavingsMechanismSummary `json:"mechanisms,omitempty"`
	Evidence                          SavingsEvidenceSummary    `json:"evidence,omitempty"`
	DecisionSessions                  []SavingsSessionSummary   `json:"decision_sessions,omitempty"`
}

type SavingsEvidenceSummary struct {
	Decisions      int64            `json:"decisions,omitempty"`
	Applied        int64            `json:"applied,omitempty"`
	FullPass       int64            `json:"full_pass,omitempty"`
	FailedOpen     int64            `json:"failed_open,omitempty"`
	Skipped        int64            `json:"skipped,omitempty"`
	NetTokens      int64            `json:"net_tokens,omitempty"`
	FootprintScore int64            `json:"footprint_score,omitempty"`
	ByContentClass map[string]int64 `json:"by_content_class,omitempty"`
	BySafetyClass  map[string]int64 `json:"by_safety_class,omitempty"`
	BySignal       map[string]int64 `json:"by_signal,omitempty"`
	ByCacheImpact  map[string]int64 `json:"by_cache_impact,omitempty"`
	ByFootprint    map[string]int64 `json:"by_footprint_score_bucket,omitempty"`
}

type SavingsMechanismSummary struct {
	Name                     string           `json:"name"`
	Layer                    int              `json:"layer,omitempty"`
	Source                   string           `json:"source,omitempty"`
	Count                    int64            `json:"count"`
	OriginalTokens           int64            `json:"original_tokens"`
	FinalTokens              int64            `json:"final_tokens"`
	SavedTokens              int64            `json:"saved_tokens"`
	AddedTokens              int64            `json:"added_tokens"`
	NetTokens                int64            `json:"net_tokens"`
	FootprintScore           int64            `json:"footprint_score,omitempty"`
	FootprintBuckets         map[string]int64 `json:"footprint_score_buckets,omitempty"`
	CompoundedEstimateTokens int64            `json:"compounded_estimate_tokens,omitempty"`
}

type SavingsSessionSummary struct {
	SessionID                string            `json:"session_id"`
	DisplayName              string            `json:"display_name,omitempty"`
	ProjectPath              string            `json:"project_path,omitempty"`
	ClientFamily             string            `json:"client_family,omitempty"`
	Requests                 int64             `json:"requests"`
	ProviderInputTokens      int64             `json:"provider_input_tokens"`
	OriginalTokens           int64             `json:"original_tokens"`
	FinalTokens              int64             `json:"final_tokens"`
	AddedTokens              int64             `json:"added_tokens"`
	LocalSaved               int64             `json:"local_saved"`
	NetSavedTokens           int64             `json:"net_saved_tokens"`
	NegativeEvents           int64             `json:"negative_events,omitempty"`
	NegativeEventTokens      int64             `json:"negative_event_tokens,omitempty"`
	Layer0NetTokens          int64             `json:"layer0_net_tokens,omitempty"`
	Layer1NetTokens          int64             `json:"layer1_net_tokens,omitempty"`
	Layer2NetTokens          int64             `json:"layer2_net_tokens,omitempty"`
	Layer3NetTokens          int64             `json:"layer3_net_tokens,omitempty"`
	OutputReduceTokens       int64             `json:"output_reduce_tokens,omitempty"`
	ToolPruneTokens          int64             `json:"tool_prune_tokens,omitempty"`
	FootprintScore           int64             `json:"footprint_score,omitempty"`
	FootprintBuckets         map[string]int64  `json:"footprint_score_buckets,omitempty"`
	CompoundedEstimateTokens int64             `json:"compounded_estimate_tokens,omitempty"`
	OutputTokens             int64             `json:"output_tokens"`
	CacheReadTokens          int64             `json:"cache_read_tokens"`
	CacheCreateTokens        int64             `json:"cache_create_tokens"`
	CacheNetTokens           int64             `json:"cache_net_tokens"`
	CacheHitRequests         int64             `json:"cache_hit_requests"`
	CacheHitRate             float64           `json:"cache_hit_rate"`
	CachedShare              float64           `json:"cached_share"`
	EffectiveBilled          int64             `json:"effective_billed"`
	Scorecard                *SavingsScorecard `json:"scorecard,omitempty"`
	CostBeforeUSD            float64           `json:"cost_before_usd"`
	CostAfterUSD             float64           `json:"cost_after_usd"`
	CostSavedUSD             float64           `json:"cost_saved_usd"`
}

type SavingsScorecard struct {
	CachedPriceRatio       float64 `json:"cached_price_ratio"`
	CounterfactualTokens   int64   `json:"counterfactual_tokens"`
	UncachedCounterfactual int64   `json:"uncached_counterfactual_tokens"`
	EffectiveBilledTokens  int64   `json:"effective_billed_tokens"`
	LocalSavingsRate       float64 `json:"s_local"`
	CombinedSavingsRate    float64 `json:"s_combined"`
	VsUncachedSavingsRate  float64 `json:"s_vs_uncached"`
}

var lookupCodexThreadMetadataForSavingsFn = lookupCodexThreadMetadataForSavings
var lookupCodexThreadWindowForSavingsFn = lookupCodexThreadWindowForSavings

func parseSavingsArgs(args []string) (period string, f savingsFlags, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--json", "-json":
			f.json = true
		case "--csv":
			f.csv = true
		case "--project":
			i++
			if i >= len(args) || args[i] == "" {
				return "", f, fmt.Errorf("--project requires a path")
			}
			f.project = args[i]
		default:
			if a == "" {
				continue
			}
			if strings.HasPrefix(a, "-") {
				return "", f, fmt.Errorf("unknown flag: %s", a)
			}
			if period == "" {
				period = a
			} else {
				return "", f, fmt.Errorf("unexpected extra argument: %s", a)
			}
		}
	}
	if period == "" {
		period = "today"
	}
	return period, f, nil
}

// computeSavings aggregates filter.db (Layer 0) + analytics snapshots
// (Layer 1/2/3) into one SavingsSummary for the period. Best-effort:
// errors from individual sources (missing filter db, unreadable
// snapshot file) are silently treated as "no data" so the command
// still produces a meaningful summary on partial state.
func computeSavings(cfg *config.Config, period, project string, now time.Time) SavingsSummary {
	out := SavingsSummary{
		Period:           period,
		Project:          project,
		CachedPriceRatio: savingsCachedPriceRatio(cfg),
		USDPerMillion:    cfg.Analytics.GainUSDPerMillionTokens,
	}

	if period != "live" {
		filterPath, err := resolveFilterDBPathFn()
		if err == nil {
			if _, statErr := os.Stat(filterPath); statErr == nil {
				rep, err := analytics.QueryFilterGainReport(filterPath, period, now, false, project, out.USDPerMillion)
				if err == nil {
					out.Layer0Runs = rep.FilterGainSummary.Runs
					out.Layer0SavedTokens = rep.FilterGainSummary.TokensSavedEst
					out.Layer0SavedUSD = rep.FilterGainSummary.SavingsUsdEst
				}
			}
		}
	}

	logDir := cfg.Analytics.ResolvedLogDir()
	switch period {
	case "today":
		if snapshots, err := analytics.ReadDailyStats(logDir, now); err == nil {
			accumulateSnapshots(&out, snapshots)
		}
	case "week":
		if snapshots, err := analytics.ReadWeeklyStats(logDir); err == nil {
			accumulateSnapshots(&out, snapshots)
		}
	case "month":
		for i := 0; i < 30; i++ {
			day := now.AddDate(0, 0, -i)
			if snaps, err := analytics.ReadDailyStats(logDir, day); err == nil {
				accumulateSnapshots(&out, snaps)
			}
		}
	case "all":
		for i := 0; i < 365; i++ {
			day := now.AddDate(0, 0, -i)
			if snaps, err := analytics.ReadDailyStats(logDir, day); err == nil {
				accumulateSnapshots(&out, snaps)
			}
		}
	case "live":
		// Live reporting is decision-log based so historical daily snapshots
		// cannot pollute current-daemon attribution/cache health.
	}

	if project == "" {
		accumulateProxyFlightsFromDecisionLog(&out, cfg, period, now)
		accumulateDecisionMechanismsFromDecisionLog(&out, cfg, period, now)
	}

	baseSavedTokens := out.Layer0SavedTokens + out.ProxySavedTokens
	out.TotalSavedTokens = baseSavedTokens
	if out.DecisionNetSavedTokens > out.TotalSavedTokens {
		out.TotalSavedTokens = out.DecisionNetSavedTokens
	}
	if out.DecisionNegativeEventTokens > 0 && out.TotalSavedTokens == baseSavedTokens {
		out.TotalSavedTokens -= out.DecisionNegativeEventTokens
	}
	out.NetBillableEquivalentTokens = out.TotalSavedTokens + out.CacheReadDiscountTokenEquivalent
	if out.USDPerMillion > 0 {
		out.TotalSavedUSD = float64(out.TotalSavedTokens) / 1_000_000.0 * out.USDPerMillion
	}
	computeDecisionCostEstimates(&out)
	return out
}

func computeDecisionCostEstimates(out *SavingsSummary) {
	if out.USDPerMillion <= 0 {
		return
	}
	out.DecisionEstimatedCostBeforeUSD, out.DecisionEstimatedCostAfterUSD, out.DecisionEstimatedCostSavedUSD = estimateCostUSD(
		out.DecisionOriginalTokens,
		out.DecisionOutputTokens,
		out.DecisionNetSavedTokens,
		out.DecisionCacheReadTokens,
		out.DecisionCacheCreateTokens,
		out.USDPerMillion,
	)
	for i := range out.DecisionSessions {
		before, after, saved := estimateCostUSD(
			out.DecisionSessions[i].OriginalTokens,
			out.DecisionSessions[i].OutputTokens,
			out.DecisionSessions[i].NetSavedTokens,
			out.DecisionSessions[i].CacheReadTokens,
			out.DecisionSessions[i].CacheCreateTokens,
			out.USDPerMillion,
		)
		out.DecisionSessions[i].CostBeforeUSD = before
		out.DecisionSessions[i].CostAfterUSD = after
		out.DecisionSessions[i].CostSavedUSD = saved
	}
}

func accumulateDecisionMechanismsFromDecisionLog(out *SavingsSummary, cfg *config.Config, period string, now time.Time) {
	path := strings.TrimSpace(cfg.Debug.DecisionsLog)
	if path == "" {
		return
	}
	path = filepath.Clean(config.ExpandHomePath(path))
	summaries, err := replaySessionFn(path)
	if err != nil {
		return
	}
	start, end, err := savingsReportWindow(period, now)
	if err != nil {
		return
	}
	byName := map[string]*SavingsMechanismSummary{}
	bySession := map[string]*SavingsSessionSummary{}
	bySessionFacts := map[string]*savingsSessionFacts{}
	sessionRecords := map[string][]dbg.RequestSummary{}
	filteredSummaries := make([]dbg.RequestSummary, 0, len(summaries))
	sessionRequestTotals := map[string]int64{}
	for _, summary := range summaries {
		if summary.Timestamp.IsZero() || summary.Timestamp.Before(start) || summary.Timestamp.After(end) {
			continue
		}
		summary.EnsureEvidenceDecisions()
		summary.EnsureMechanisms()
		filteredSummaries = append(filteredSummaries, summary)
		sessionRequestTotals[decisionSessionID(summary)]++
	}
	cachedPriceRatio := savingsCachedPriceRatio(cfg)
	sessionLengthEstimator := newSavingsSessionLengthEstimator(summaries, start)
	sessionRequestSeen := map[string]int64{}
	for _, summary := range filteredSummaries {
		out.DecisionRequests++
		out.DecisionProviderInputTokens += int64(summary.ProviderInputTokens)
		out.DecisionOriginalTokens += int64(summary.Tokens.Original)
		out.DecisionFinalTokens += int64(summary.Tokens.Final)
		out.DecisionLocalSavedTokens += int64(summary.Tokens.Saved)
		out.DecisionNetSavedTokens += int64(summary.Tokens.Saved)
		out.DecisionOutputTokens += int64(maxSavingsInt(summary.ProviderOutputTokens, summary.OutputTokens))
		cacheRead := int64(summary.CacheReadTokens + summary.ProviderCachedTokens)
		cacheCreate := int64(summary.CacheCreateTokens)
		cacheNet := cacheRead - cacheCreate
		out.DecisionCacheReadTokens += cacheRead
		out.DecisionCacheCreateTokens += cacheCreate
		out.DecisionCacheNetTokens += cacheNet
		if cacheRead > 0 {
			out.DecisionCacheHitRequests++
		}
		if cacheCreate > 0 {
			out.DecisionCacheCreateRequests++
		}
		if cacheNet < 0 {
			out.DecisionCacheNegativeNetRequests++
		}
		sessionID := decisionSessionID(summary)
		sessionRequestSeen[sessionID]++
		remainingRequests := savingsCompoundedRemainingRequests(
			period,
			sessionRequestTotals[sessionID]-sessionRequestSeen[sessionID],
			sessionLengthEstimator,
			summary,
			sessionRequestSeen[sessionID],
		)
		sessionRecords[sessionID] = append(sessionRecords[sessionID], summary)
		updateSavingsSessionFacts(bySessionFacts, sessionID, summary)
		sessionRow := bySession[sessionID]
		if sessionRow == nil {
			sessionRow = &SavingsSessionSummary{SessionID: sessionID}
			bySession[sessionID] = sessionRow
		}
		if sessionRow.ClientFamily == "" {
			sessionRow.ClientFamily = savingsClientFamily(summary)
		}
		sessionRow.Requests++
		sessionRow.ProviderInputTokens += int64(summary.ProviderInputTokens)
		sessionRow.OriginalTokens += int64(summary.Tokens.Original)
		sessionRow.FinalTokens += int64(summary.Tokens.Final)
		sessionRow.LocalSaved += int64(summary.Tokens.Saved)
		sessionRow.NetSavedTokens += int64(summary.Tokens.Saved)
		sessionRow.OutputTokens += int64(maxSavingsInt(summary.ProviderOutputTokens, summary.OutputTokens))
		sessionRow.CacheReadTokens += cacheRead
		sessionRow.CacheCreateTokens += cacheCreate
		sessionRow.CacheNetTokens += cacheNet
		if cacheRead > 0 {
			sessionRow.CacheHitRequests++
		}
		sessionRow.OutputReduceTokens += outputReduceSessionNetTokens(summary)
		sessionRow.ToolPruneTokens += int64(summary.ToolPrune.SavedTokens)
		accumulateSavingsEvidence(&out.Evidence, summary.EvidenceDecisions)
		layerObserved := map[int]bool{}
		for _, mechanism := range summary.Mechanisms {
			if mechanism.Name == "" || mechanism.Name == "request_total" {
				continue
			}
			if mechanism.OriginalTokens == 0 &&
				mechanism.FinalTokens == 0 &&
				mechanism.SavedTokens == 0 &&
				mechanism.AddedTokens == 0 &&
				mechanism.NetTokens == 0 {
				continue
			}
			row := byName[mechanism.Name]
			if row == nil {
				row = &SavingsMechanismSummary{
					Name:   mechanism.Name,
					Layer:  mechanism.Layer,
					Source: mechanism.Source,
				}
				byName[mechanism.Name] = row
			}
			row.Count += int64(maxSavingsInt(mechanism.Count, 1))
			row.OriginalTokens += int64(mechanism.OriginalTokens)
			row.FinalTokens += int64(mechanism.FinalTokens)
			row.SavedTokens += int64(mechanism.SavedTokens)
			row.AddedTokens += int64(mechanism.AddedTokens)
			row.NetTokens += int64(mechanism.NetTokens)
			out.DecisionAddedTokens += int64(mechanism.AddedTokens)
			sessionRow.AddedTokens += int64(mechanism.AddedTokens)
			accumulateSavingsFootprint(sessionRow, row, mechanism)
			accumulateSavingsCompoundedEstimate(sessionRow, row, mechanism, remainingRequests, cachedPriceRatio)
			if layer, ok := savingsMechanismLayer(mechanism); ok {
				layerObserved[layer] = true
				addSessionLayerNet(sessionRow, layer, int64(mechanism.NetTokens))
			}
		}
		addSessionLayerFallback(sessionRow, summary, layerObserved)
	}
	applySavingsNegativeEventAccounting(out, bySession, sessionRecords)
	resolveLocalCodexFallbackSessions(bySession, bySessionFacts, start, end)
	for _, facts := range bySessionFacts {
		out.DecisionCodexRequests += facts.CodexCandidateRequests
		out.DecisionCodexAttributedRequests += facts.AttributedRequests
		accumulateCodexUnattributedReason(out, facts)
	}
	if out.DecisionCodexAttributedRequests > out.DecisionCodexRequests {
		out.DecisionCodexAttributedRequests = out.DecisionCodexRequests
	}
	out.DecisionCodexUnattributedRequests = out.DecisionCodexRequests - out.DecisionCodexAttributedRequests
	out.Mechanisms = out.Mechanisms[:0]
	for _, row := range byName {
		out.Mechanisms = append(out.Mechanisms, *row)
	}
	sort.Slice(out.Mechanisms, func(i, j int) bool {
		if out.Mechanisms[i].NetTokens == out.Mechanisms[j].NetTokens {
			return out.Mechanisms[i].Name < out.Mechanisms[j].Name
		}
		return out.Mechanisms[i].NetTokens > out.Mechanisms[j].NetTokens
	})
	out.DecisionSessions = out.DecisionSessions[:0]
	for _, row := range bySession {
		if row.Requests > 0 {
			row.CacheHitRate = float64(row.CacheHitRequests) / float64(row.Requests)
		}
		row.CachedShare = savingsCachedShare(row.CacheReadTokens, row.ProviderInputTokens, row.FinalTokens)
		row.EffectiveBilled = savingsEffectiveBilledTokens(row.ProviderInputTokens, row.FinalTokens, row.CacheReadTokens, row.CacheCreateTokens, row.NegativeEventTokens, out.CachedPriceRatio)
		scorecard := savingsBuildScorecard(row.ProviderInputTokens, row.FinalTokens, row.LocalSaved, row.CacheReadTokens, row.CacheCreateTokens, row.NegativeEventTokens, out.CachedPriceRatio)
		row.Scorecard = &scorecard
		out.DecisionSessions = append(out.DecisionSessions, *row)
		out.DecisionEffectiveBilledTokens += scorecard.EffectiveBilledTokens
		out.DecisionCounterfactualTokens += scorecard.CounterfactualTokens
		out.DecisionUncachedCounterfactual += scorecard.UncachedCounterfactual
		out.DecisionLayer0NetTokens += row.Layer0NetTokens
		out.DecisionLayer1NetTokens += row.Layer1NetTokens
		out.DecisionLayer2NetTokens += row.Layer2NetTokens
		out.DecisionLayer3NetTokens += row.Layer3NetTokens
		out.DecisionOutputReduceTokens += row.OutputReduceTokens
		out.DecisionToolPruneTokens += row.ToolPruneTokens
		out.DecisionFootprintScore += row.FootprintScore
		mergeSavingsBucketCounts(&out.DecisionFootprintScoreBuckets, row.FootprintBuckets)
		out.DecisionCompoundedEstimateTokens += row.CompoundedEstimateTokens
	}
	enrichSavingsSessions(out.DecisionSessions)
	sort.Slice(out.DecisionSessions, func(i, j int) bool {
		if out.DecisionSessions[i].NetSavedTokens == out.DecisionSessions[j].NetSavedTokens {
			return out.DecisionSessions[i].SessionID < out.DecisionSessions[j].SessionID
		}
		return out.DecisionSessions[i].NetSavedTokens > out.DecisionSessions[j].NetSavedTokens
	})
	if out.DecisionRequests > 0 {
		out.DecisionCacheHitRate = float64(out.DecisionCacheHitRequests) / float64(out.DecisionRequests)
		decisionInputEquivalent := out.DecisionUncachedCounterfactual - out.DecisionLocalSavedTokens
		decisionCacheRead := out.DecisionCacheReadTokens
		if decisionCacheRead > decisionInputEquivalent {
			decisionCacheRead = decisionInputEquivalent
		}
		out.DecisionCachedShare = savingsRate(decisionCacheRead, decisionInputEquivalent)
		out.DecisionLocalSavingsRate = savingsRate(out.DecisionLocalSavedTokens, out.DecisionUncachedCounterfactual)
		out.DecisionCombinedSavingsRate = savingsRate(out.DecisionCounterfactualTokens-out.DecisionEffectiveBilledTokens, out.DecisionCounterfactualTokens)
		out.DecisionVsUncachedSavingsRate = savingsRate(out.DecisionUncachedCounterfactual-out.DecisionEffectiveBilledTokens, out.DecisionUncachedCounterfactual)
		out.DecisionCacheStatus = savingsDecisionCacheStatus(*out)
	}
	if out.DecisionCodexRequests > 0 {
		out.DecisionCodexAttributionRate = float64(out.DecisionCodexAttributedRequests) / float64(out.DecisionCodexRequests)
		out.DecisionCodexAttributionStatus = savingsDecisionCodexAttributionStatus(*out)
	}
}

func accumulateCodexUnattributedReason(out *SavingsSummary, facts *savingsSessionFacts) {
	if out == nil || facts == nil || facts.CodexCandidateRequests <= facts.AttributedRequests {
		return
	}
	reason := strings.TrimSpace(facts.UnattributedReason)
	if reason == "" {
		reason = "missing_thread_identity"
	}
	if out.DecisionCodexUnattributedReasons == nil {
		out.DecisionCodexUnattributedReasons = make(map[string]int64)
	}
	out.DecisionCodexUnattributedReasons[reason] += facts.CodexCandidateRequests - facts.AttributedRequests
}

const savingsNegativeRetryWindow = 120 * time.Second

func applySavingsNegativeEventAccounting(out *SavingsSummary, bySession map[string]*SavingsSessionSummary, records map[string][]dbg.RequestSummary) {
	if out == nil {
		return
	}
	for sessionID, sessionRecords := range records {
		row := bySession[sessionID]
		if row == nil {
			continue
		}
		events, tokens := savingsNegativeEventsForSession(sessionRecords)
		if events == 0 || tokens == 0 {
			continue
		}
		row.NegativeEvents += events
		row.NegativeEventTokens += tokens
		row.NetSavedTokens -= tokens
		out.DecisionNegativeEvents += events
		out.DecisionNegativeEventTokens += tokens
		out.DecisionNetSavedTokens -= tokens
	}
}

func savingsNegativeEventsForSession(records []dbg.RequestSummary) (int64, int64) {
	priorDeltaOriginals := make([]int, 0, len(records))
	var events int64
	var tokens int64
	for i, summary := range records {
		if isSavingsUpstreamError(summary) {
			median := medianPositiveSavingsInt(priorDeltaOriginals)
			if median > 0 {
				if retry, ok := nextSavingsRetryRequest(records[i+1:], summary.Timestamp); ok {
					extra := retry.Tokens.Original - median
					if extra > 0 {
						events++
						tokens += int64(extra)
					}
				}
			}
		}
		if isSavingsPriorDeltaShape(summary) {
			priorDeltaOriginals = append(priorDeltaOriginals, summary.Tokens.Original)
		}
	}
	return events, tokens
}

func isSavingsUpstreamError(summary dbg.RequestSummary) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(summary.BypassReason)), "upstream_error")
}

func isSavingsPriorDeltaShape(summary dbg.RequestSummary) bool {
	return summary.PreviousResponseIDUsed &&
		!isSavingsUpstreamError(summary) &&
		summary.Tokens.Original > 0
}

func nextSavingsRetryRequest(records []dbg.RequestSummary, errorAt time.Time) (dbg.RequestSummary, bool) {
	for _, summary := range records {
		if summary.Timestamp.IsZero() || summary.Tokens.Original <= 0 {
			continue
		}
		if !errorAt.IsZero() {
			if summary.Timestamp.Before(errorAt) {
				continue
			}
			if summary.Timestamp.Sub(errorAt) >= savingsNegativeRetryWindow {
				return dbg.RequestSummary{}, false
			}
		}
		return summary, true
	}
	return dbg.RequestSummary{}, false
}

func medianPositiveSavingsInt(values []int) int {
	positive := make([]int, 0, len(values))
	for _, value := range values {
		if value > 0 {
			positive = append(positive, value)
		}
	}
	if len(positive) == 0 {
		return 0
	}
	sort.Ints(positive)
	mid := len(positive) / 2
	if len(positive)%2 == 1 {
		return positive[mid]
	}
	return (positive[mid-1] + positive[mid]) / 2
}

func accumulateSavingsEvidence(out *SavingsEvidenceSummary, decisions []evidence.BlockDecision) {
	if out == nil {
		return
	}
	for _, decision := range decisions {
		out.Decisions++
		out.NetTokens += int64(decision.NetTokens)
		out.FootprintScore += int64(decision.FootprintScore)
		switch decision.Action {
		case evidence.ActionApplied:
			out.Applied++
		case evidence.ActionFullPass:
			out.FullPass++
		case evidence.ActionFailedOpen:
			out.FailedOpen++
		case evidence.ActionSkipped:
			out.Skipped++
		}
		if decision.ContentClass != "" {
			if out.ByContentClass == nil {
				out.ByContentClass = map[string]int64{}
			}
			out.ByContentClass[string(decision.ContentClass)]++
		}
		if decision.SafetyClass != "" {
			if out.BySafetyClass == nil {
				out.BySafetyClass = map[string]int64{}
			}
			out.BySafetyClass[string(decision.SafetyClass)]++
		}
		if len(decision.Signals) > 0 && out.BySignal == nil {
			out.BySignal = map[string]int64{}
		}
		for _, signal := range decision.Signals {
			if signal != "" {
				out.BySignal[string(signal)]++
			}
		}
		if decision.CacheImpact != "" {
			if out.ByCacheImpact == nil {
				out.ByCacheImpact = map[string]int64{}
			}
			out.ByCacheImpact[decision.CacheImpact]++
		}
		if decision.FootprintScoreBucket != "" {
			if out.ByFootprint == nil {
				out.ByFootprint = map[string]int64{}
			}
			out.ByFootprint[decision.FootprintScoreBucket]++
		}
	}
}

func accumulateSavingsFootprint(session *SavingsSessionSummary, mechanism *SavingsMechanismSummary, item dbg.MechanismAccounting) {
	score := int64(item.FootprintScore)
	bucket := strings.TrimSpace(item.FootprintScoreBucket)
	if score <= 0 && bucket == "" {
		return
	}
	if mechanism != nil {
		mechanism.FootprintScore += score
		addSavingsBucketCount(&mechanism.FootprintBuckets, bucket, 1)
	}
	if session != nil {
		session.FootprintScore += score
		addSavingsBucketCount(&session.FootprintBuckets, bucket, 1)
	}
}

func accumulateSavingsCompoundedEstimate(session *SavingsSessionSummary, mechanism *SavingsMechanismSummary, item dbg.MechanismAccounting, remainingRequests int64, cachedPriceRatio float64) {
	estimate := savingsCompoundedEstimateTokens(item, remainingRequests, cachedPriceRatio)
	if estimate <= 0 {
		return
	}
	if mechanism != nil {
		mechanism.CompoundedEstimateTokens += estimate
	}
	if session != nil {
		session.CompoundedEstimateTokens += estimate
	}
}

func savingsCompoundedEstimateTokens(item dbg.MechanismAccounting, remainingRequests int64, cachedPriceRatio float64) int64 {
	if item.SavedTokens <= 0 || !savingsMechanismHasFootprint(item) {
		return 0
	}
	if remainingRequests < 1 {
		remainingRequests = 1
	}
	if cachedPriceRatio < 0 {
		cachedPriceRatio = 0
	}
	if cachedPriceRatio > 1 {
		cachedPriceRatio = 1
	}
	return int64(float64(item.SavedTokens) * float64(remainingRequests) * cachedPriceRatio)
}

func savingsMechanismHasFootprint(item dbg.MechanismAccounting) bool {
	return item.FootprintScore > 0 || strings.TrimSpace(item.FootprintScoreBucket) != ""
}

const savingsSessionLengthEMAAlpha = 0.1

type savingsSessionLengthEstimator struct {
	byFamily map[string]savingsSessionLengthEstimate
}

type savingsSessionLengthEstimate struct {
	samples  int64
	estimate float64
}

type savingsHistoricalSessionLength struct {
	sessionID string
	family    string
	length    int64
	lastSeen  time.Time
}

func newSavingsSessionLengthEstimator(summaries []dbg.RequestSummary, cutoff time.Time) savingsSessionLengthEstimator {
	estimator := savingsSessionLengthEstimator{byFamily: map[string]savingsSessionLengthEstimate{}}
	if cutoff.IsZero() || len(summaries) == 0 {
		return estimator
	}
	bySession := map[string]*savingsHistoricalSessionLength{}
	for _, summary := range summaries {
		if summary.Timestamp.IsZero() || !summary.Timestamp.Before(cutoff) {
			continue
		}
		sessionID := decisionSessionID(summary)
		row := bySession[sessionID]
		if row == nil {
			row = &savingsHistoricalSessionLength{sessionID: sessionID}
			bySession[sessionID] = row
		}
		row.length++
		if row.family == "" {
			row.family = savingsClientFamily(summary)
		}
		if summary.Timestamp.After(row.lastSeen) {
			row.lastSeen = summary.Timestamp
		}
	}
	rows := make([]savingsHistoricalSessionLength, 0, len(bySession))
	for _, row := range bySession {
		if row.length > 0 {
			rows = append(rows, *row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].lastSeen.Equal(rows[j].lastSeen) {
			return rows[i].sessionID < rows[j].sessionID
		}
		return rows[i].lastSeen.Before(rows[j].lastSeen)
	})
	for _, row := range rows {
		estimator.observe(row.family, row.length)
	}
	return estimator
}

func (e savingsSessionLengthEstimator) observe(family string, length int64) {
	if length < 1 {
		return
	}
	family = savingsEstimatorFamily(family)
	row := e.byFamily[family]
	if row.samples == 0 {
		row.estimate = float64(length)
	} else {
		row.estimate = savingsSessionLengthEMAAlpha*float64(length) + (1-savingsSessionLengthEMAAlpha)*row.estimate
	}
	row.samples++
	e.byFamily[family] = row
}

func (e savingsSessionLengthEstimator) remaining(family string, turnPosition int64) int64 {
	row, ok := e.byFamily[savingsEstimatorFamily(family)]
	if !ok || row.samples == 0 {
		return 1
	}
	if turnPosition < 1 {
		turnPosition = 1
	}
	estimatedLength := int64(row.estimate + 0.5)
	remaining := estimatedLength - turnPosition
	if remaining < 1 {
		return 1
	}
	return remaining
}

func savingsEstimatorFamily(family string) string {
	family = strings.ToLower(strings.TrimSpace(family))
	if family == "" {
		return "unknown"
	}
	return family
}

func savingsCompoundedRemainingRequests(period string, observedRemaining int64, estimator savingsSessionLengthEstimator, summary dbg.RequestSummary, seenInWindow int64) int64 {
	if observedRemaining > 0 {
		return observedRemaining
	}
	if period != "live" {
		return 1
	}
	return estimator.remaining(savingsClientFamily(summary), savingsDecisionTurnPosition(summary, seenInWindow))
}

func savingsDecisionTurnPosition(summary dbg.RequestSummary, seenInWindow int64) int64 {
	if summary.DebugFacts != nil {
		if value := strings.TrimSpace(summary.DebugFacts["wss.turn_seq"]); value != "" {
			if n, err := strconv.ParseInt(value, 10, 64); err == nil && n > 0 {
				return n
			}
		}
	}
	if seenInWindow > 0 {
		return seenInWindow
	}
	return 1
}

func addSavingsBucketCount(counts *map[string]int64, bucket string, n int64) {
	bucket = strings.TrimSpace(bucket)
	if counts == nil || bucket == "" || n <= 0 {
		return
	}
	if *counts == nil {
		*counts = map[string]int64{}
	}
	(*counts)[bucket] += n
}

func mergeSavingsBucketCounts(dst *map[string]int64, src map[string]int64) {
	if len(src) == 0 {
		return
	}
	for bucket, count := range src {
		addSavingsBucketCount(dst, bucket, count)
	}
}

func savingsMechanismLayer(mechanism dbg.MechanismAccounting) (int, bool) {
	name := strings.TrimSpace(mechanism.Name)
	source := strings.TrimSpace(mechanism.Source)
	if name == "provider_prompt_cache" || source == "cache_accounting" {
		return 2, true
	}
	if name == "tool_prune" || source == "tool_prune" || name == "output_reduce_directive" || source == "output_reduce" {
		return 0, false
	}
	if mechanism.Layer > 0 && mechanism.Layer <= 3 {
		return mechanism.Layer, true
	}
	if mechanism.Layer == 0 && (source == "decision_entry" || source == "evidence_decision") {
		return 0, true
	}
	return 0, false
}

func addSessionLayerNet(row *SavingsSessionSummary, layer int, net int64) {
	switch layer {
	case 0:
		row.Layer0NetTokens += net
	case 1:
		row.Layer1NetTokens += net
	case 2:
		row.Layer2NetTokens += net
	case 3:
		row.Layer3NetTokens += net
	}
}

func addSessionLayerFallback(row *SavingsSessionSummary, summary dbg.RequestSummary, layerObserved map[int]bool) {
	if !layerObserved[0] {
		row.Layer0NetTokens += positiveSavingsDelta(summary.Tokens.Original, summary.Tokens.AfterLayer0)
	}
	if !layerObserved[1] {
		row.Layer1NetTokens += positiveSavingsDelta(summary.Tokens.AfterLayer0, summary.Tokens.AfterLayer1)
	}
	if layerObserved[2] {
		return
	}
	layer2Net := cacheLayerNetTokens(summary)
	if layer2Net == 0 {
		layer2Net = positiveSavingsDelta(summary.Tokens.AfterLayer1, summary.Tokens.Final)
	}
	row.Layer2NetTokens += layer2Net
}

func cacheLayerNetTokens(summary dbg.RequestSummary) int64 {
	return int64(summary.CacheReadTokens + summary.ProviderCachedTokens - summary.CacheCreateTokens)
}

func positiveSavingsDelta(before, after int) int64 {
	if before <= 0 || after <= 0 || before <= after {
		return 0
	}
	return int64(before - after)
}

func outputReduceSessionNetTokens(summary dbg.RequestSummary) int64 {
	if !summary.OutputReduce.Applied && summary.OutputReduce.AddedTokens == 0 {
		return 0
	}
	return -int64(summary.OutputReduce.AddedTokens)
}

type savingsSessionFacts struct {
	SessionID              string
	ClientFamily           string
	Model                  string
	FirstSeen              time.Time
	LastSeen               time.Time
	CandidateFirstSeen     time.Time
	CandidateLastSeen      time.Time
	CodexCandidateRequests int64
	AttributedRequests     int64
	UnattributedReason     string
}

func updateSavingsSessionFacts(bySessionFacts map[string]*savingsSessionFacts, sessionID string, summary dbg.RequestSummary) {
	facts := bySessionFacts[sessionID]
	if facts == nil {
		facts = &savingsSessionFacts{SessionID: sessionID}
		bySessionFacts[sessionID] = facts
	}
	if !summary.Timestamp.IsZero() {
		if facts.FirstSeen.IsZero() || summary.Timestamp.Before(facts.FirstSeen) {
			facts.FirstSeen = summary.Timestamp
		}
		if facts.LastSeen.IsZero() || summary.Timestamp.After(facts.LastSeen) {
			facts.LastSeen = summary.Timestamp
		}
	}
	if facts.ClientFamily == "" {
		facts.ClientFamily = savingsClientFamily(summary)
	}
	if facts.Model == "" {
		facts.Model = strings.TrimSpace(summary.Model)
	}
	if !isCodexAttributionCandidate(summary, sessionID) {
		return
	}
	facts.CodexCandidateRequests++
	if isTokenBearingSavingsDecision(summary) && !summary.Timestamp.IsZero() {
		if facts.CandidateFirstSeen.IsZero() || summary.Timestamp.Before(facts.CandidateFirstSeen) {
			facts.CandidateFirstSeen = summary.Timestamp
		}
		if facts.CandidateLastSeen.IsZero() || summary.Timestamp.After(facts.CandidateLastSeen) {
			facts.CandidateLastSeen = summary.Timestamp
		}
	}
	if isCodexAttributedSession(strings.TrimSpace(summary.SessionID)) || isCodexAttributedSession(sessionID) {
		facts.AttributedRequests++
	}
}

func isTokenBearingSavingsDecision(summary dbg.RequestSummary) bool {
	return summary.Tokens.Original > 0 ||
		summary.Tokens.Final > 0 ||
		summary.Tokens.Saved != 0 ||
		summary.CacheReadTokens > 0 ||
		summary.CacheCreateTokens > 0 ||
		summary.ProviderCachedTokens > 0 ||
		summary.ProviderInputTokens > 0 ||
		summary.ProviderOutputTokens > 0 ||
		summary.OutputTokens > 0
}

func resolveLocalCodexFallbackSessions(bySession map[string]*SavingsSessionSummary, byFacts map[string]*savingsSessionFacts, start, end time.Time) {
	if !needsLocalCodexFallbackLookup(byFacts) {
		return
	}
	metadata, err := lookupCodexThreadWindowForSavingsFn(start.Add(-5*time.Minute), end.Add(5*time.Minute))
	if err != nil {
		markLocalCodexFallbackLookupFailure(byFacts, "metadata_lookup_error")
		return
	}
	if len(metadata) == 0 {
		markLocalCodexFallbackLookupFailure(byFacts, "no_local_thread_candidates")
		return
	}
	for sessionID, facts := range byFacts {
		if !needsLocalCodexFallbackResolution(facts) {
			continue
		}
		meta, reason, ok := resolveLocalCodexFallbackMetadata(*facts, metadata)
		if !ok || strings.TrimSpace(meta.ID) == "" {
			facts.UnattributedReason = reason
			continue
		}
		localID := "codex-local:" + strings.TrimSpace(meta.ID)
		row := bySession[sessionID]
		if row == nil {
			continue
		}
		delete(bySession, sessionID)
		delete(byFacts, sessionID)
		if existing := bySession[localID]; existing != nil {
			mergeSavingsSession(existing, row)
			row = existing
		} else {
			row.SessionID = localID
			bySession[localID] = row
		}
		applyCodexThreadMetadata(row, meta)
		facts.SessionID = localID
		facts.AttributedRequests = facts.CodexCandidateRequests
		if family := savingsThreadClientFamily(meta); family != "" {
			facts.ClientFamily = family
		}
		byFacts[localID] = facts
	}
}

func markLocalCodexFallbackLookupFailure(byFacts map[string]*savingsSessionFacts, reason string) {
	for _, facts := range byFacts {
		if needsLocalCodexFallbackResolution(facts) {
			facts.UnattributedReason = reason
		}
	}
}

func needsLocalCodexFallbackLookup(byFacts map[string]*savingsSessionFacts) bool {
	for _, facts := range byFacts {
		if needsLocalCodexFallbackResolution(facts) {
			return true
		}
	}
	return false
}

func needsLocalCodexFallbackResolution(facts *savingsSessionFacts) bool {
	if facts == nil || facts.CodexCandidateRequests == 0 || facts.AttributedRequests > 0 {
		return false
	}
	id := strings.TrimSpace(facts.SessionID)
	return strings.HasPrefix(id, "fh:") || isAnonymousCodexFallbackSession(id)
}

func isAnonymousCodexFallbackSession(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), "no-session:")
}

func resolveLocalCodexFallbackMetadata(facts savingsSessionFacts, candidates []codexthreads.Metadata) (codexthreads.Metadata, string, bool) {
	if meta, ok := resolveLocalCodexFallbackByHash(facts, candidates); ok {
		return meta, "", true
	}
	filtered := make([]codexthreads.Metadata, 0, len(candidates))
	for _, meta := range candidates {
		if !metadataMatchesSavingsFacts(meta, facts) {
			continue
		}
		filtered = append(filtered, meta)
	}
	if len(filtered) != 1 {
		if len(filtered) == 0 {
			return codexthreads.Metadata{}, "no_matching_thread_candidate", false
		}
		return codexthreads.Metadata{}, "ambiguous_thread_candidates", false
	}
	return filtered[0], "", true
}

func resolveLocalCodexFallbackByHash(facts savingsSessionFacts, candidates []codexthreads.Metadata) (codexthreads.Metadata, bool) {
	want := strings.TrimPrefix(strings.TrimSpace(facts.SessionID), "fh:")
	if want == "" {
		return codexthreads.Metadata{}, false
	}
	var matched codexthreads.Metadata
	matches := 0
	for _, meta := range candidates {
		if codexFirstTextHash(meta.FirstUserMessage) != want {
			continue
		}
		matched = meta
		matches++
	}
	if matches != 1 {
		return codexthreads.Metadata{}, false
	}
	return matched, true
}

func metadataMatchesSavingsFacts(meta codexthreads.Metadata, facts savingsSessionFacts) bool {
	if isAnonymousCodexFallbackSession(facts.SessionID) {
		if !metadataActivityEnclosesSavingsFacts(meta, facts) {
			return false
		}
	} else if !metadataTimeOverlapsSavingsFacts(meta, facts) {
		return false
	}
	if model := strings.TrimSpace(facts.Model); model != "" && strings.TrimSpace(meta.Model) != "" && model != strings.TrimSpace(meta.Model) {
		return false
	}
	family := strings.TrimSpace(facts.ClientFamily)
	if family != "" && family != "codex" && savingsThreadClientFamily(meta) != family {
		return false
	}
	return true
}

func metadataTimeOverlapsSavingsFacts(meta codexthreads.Metadata, facts savingsSessionFacts) bool {
	if meta.UpdatedAt.IsZero() || facts.FirstSeen.IsZero() || facts.LastSeen.IsZero() {
		return true
	}
	return !meta.UpdatedAt.Before(facts.FirstSeen.Add(-30*time.Minute)) &&
		!meta.UpdatedAt.After(facts.LastSeen.Add(30*time.Minute))
}

func metadataActivityEnclosesSavingsFacts(meta codexthreads.Metadata, facts savingsSessionFacts) bool {
	if meta.CreatedAt.IsZero() || meta.UpdatedAt.IsZero() || facts.CandidateFirstSeen.IsZero() || facts.CandidateLastSeen.IsZero() {
		return false
	}
	return !meta.CreatedAt.After(facts.CandidateFirstSeen.Add(5*time.Minute)) &&
		!meta.UpdatedAt.Before(facts.CandidateLastSeen.Add(-5*time.Minute))
}

func codexFirstTextHash(text string) string {
	if text == "" {
		return ""
	}
	if len(text) > 200 {
		text = text[:200]
	}
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h[:8])
}

func mergeSavingsSession(dst, src *SavingsSessionSummary) {
	if dst == nil || src == nil {
		return
	}
	dst.Requests += src.Requests
	dst.ProviderInputTokens += src.ProviderInputTokens
	dst.OriginalTokens += src.OriginalTokens
	dst.FinalTokens += src.FinalTokens
	dst.AddedTokens += src.AddedTokens
	dst.LocalSaved += src.LocalSaved
	dst.NetSavedTokens += src.NetSavedTokens
	dst.NegativeEvents += src.NegativeEvents
	dst.NegativeEventTokens += src.NegativeEventTokens
	dst.Layer0NetTokens += src.Layer0NetTokens
	dst.Layer1NetTokens += src.Layer1NetTokens
	dst.Layer2NetTokens += src.Layer2NetTokens
	dst.Layer3NetTokens += src.Layer3NetTokens
	dst.OutputReduceTokens += src.OutputReduceTokens
	dst.ToolPruneTokens += src.ToolPruneTokens
	dst.FootprintScore += src.FootprintScore
	mergeSavingsBucketCounts(&dst.FootprintBuckets, src.FootprintBuckets)
	dst.CompoundedEstimateTokens += src.CompoundedEstimateTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheCreateTokens += src.CacheCreateTokens
	dst.CacheNetTokens += src.CacheNetTokens
	dst.CacheHitRequests += src.CacheHitRequests
	if dst.ClientFamily == "" {
		dst.ClientFamily = src.ClientFamily
	}
	if dst.DisplayName == "" {
		dst.DisplayName = src.DisplayName
	}
	if dst.ProjectPath == "" {
		dst.ProjectPath = src.ProjectPath
	}
}

func savingsCachedShare(cacheReadTokens, providerInputTokens, fallbackInputTokens int64) float64 {
	denominator := providerInputTokens
	if denominator <= 0 {
		denominator = fallbackInputTokens
	}
	if denominator <= 0 || cacheReadTokens <= 0 {
		return 0
	}
	share := float64(cacheReadTokens) / float64(denominator)
	if share > 1 {
		return 1
	}
	return share
}

func savingsEffectiveBilledTokens(providerInputTokens, fallbackInputTokens, cacheReadTokens, cacheCreateTokens, negativeEventTokens int64, cachedPriceRatio float64) int64 {
	inputTokens := providerInputTokens
	if inputTokens <= 0 {
		inputTokens = fallbackInputTokens
	}
	if inputTokens <= 0 {
		inputTokens = 0
	}
	discountableRead := cacheReadTokens
	if discountableRead > inputTokens {
		discountableRead = inputTokens
	}
	effective := inputTokens - cacheReadDiscountEquivalent(discountableRead, cachedPriceRatio) + cacheCreateTokens + negativeEventTokens
	if effective < 0 {
		return 0
	}
	return effective
}

func savingsBuildScorecard(providerInputTokens, fallbackInputTokens, localSavedTokens, cacheReadTokens, cacheCreateTokens, negativeEventTokens int64, cachedPriceRatio float64) SavingsScorecard {
	inputTokens := savingsInputEquivalent(providerInputTokens, fallbackInputTokens)
	inputWithoutLocal := inputTokens + localSavedTokens
	if inputWithoutLocal < 0 {
		inputWithoutLocal = 0
	}
	effective := savingsEffectiveBilledTokens(providerInputTokens, fallbackInputTokens, cacheReadTokens, cacheCreateTokens, negativeEventTokens, cachedPriceRatio)
	counterfactual := savingsCacheAwareBilledTokens(inputWithoutLocal, cacheReadTokens, cacheCreateTokens, cachedPriceRatio)
	uncachedCounterfactual := inputWithoutLocal
	return SavingsScorecard{
		CachedPriceRatio:       cachedPriceRatio,
		CounterfactualTokens:   counterfactual,
		UncachedCounterfactual: uncachedCounterfactual,
		EffectiveBilledTokens:  effective,
		LocalSavingsRate:       savingsRate(localSavedTokens, uncachedCounterfactual),
		CombinedSavingsRate:    savingsRate(counterfactual-effective, counterfactual),
		VsUncachedSavingsRate:  savingsRate(uncachedCounterfactual-effective, uncachedCounterfactual),
	}
}

func savingsInputEquivalent(providerInputTokens, fallbackInputTokens int64) int64 {
	if providerInputTokens > 0 {
		return providerInputTokens
	}
	if fallbackInputTokens > 0 {
		return fallbackInputTokens
	}
	return 0
}

func savingsCacheAwareBilledTokens(inputTokens, cacheReadTokens, cacheCreateTokens int64, cachedPriceRatio float64) int64 {
	if inputTokens < 0 {
		inputTokens = 0
	}
	discountableRead := cacheReadTokens
	if discountableRead > inputTokens {
		discountableRead = inputTokens
	}
	effective := inputTokens - cacheReadDiscountEquivalent(discountableRead, cachedPriceRatio) + cacheCreateTokens
	if effective < 0 {
		return 0
	}
	return effective
}

func savingsRate(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func decisionSessionID(summary dbg.RequestSummary) string {
	sessionID := strings.TrimSpace(summary.SessionID)
	if sessionID != "" && !strings.EqualFold(sessionID, "empty") {
		return sessionID
	}
	if strings.TrimSpace(summary.Source) != "" {
		return "no-session:" + strings.TrimSpace(summary.Source)
	}
	return "no-session:unknown"
}

func isCodexDecisionSummary(summary dbg.RequestSummary, sessionID string) bool {
	provider := strings.ToLower(strings.TrimSpace(summary.Provider))
	client := strings.ToLower(strings.TrimSpace(summary.ClientFamily))
	source := strings.ToLower(strings.TrimSpace(summary.Source))
	return strings.Contains(provider, "codex") ||
		strings.Contains(client, "codex") ||
		strings.Contains(source, "codex") ||
		isCodexAttributedSession(strings.TrimSpace(summary.SessionID)) ||
		isCodexAttributedSession(strings.TrimSpace(sessionID))
}

func isCodexAttributionCandidate(summary dbg.RequestSummary, sessionID string) bool {
	if !isCodexDecisionSummary(summary, sessionID) {
		return false
	}
	path := strings.ToLower(strings.TrimSpace(summary.Path))
	if path != "" && !strings.Contains(path, "responses") && !strings.Contains(path, "chat/completions") {
		return false
	}
	return true
}

func savingsDecisionCacheStatus(s SavingsSummary) string {
	if s.DecisionRequests == 0 {
		return ""
	}
	if s.DecisionCacheReadTokens == 0 && s.DecisionCacheCreateTokens == 0 {
		return "none"
	}
	if s.DecisionCacheNetTokens < 0 || s.DecisionCacheNegativeNetRequests > 0 {
		return "attention"
	}
	if s.DecisionCacheReadTokens > 0 {
		return "ok"
	}
	return "warming"
}

func savingsDecisionCodexAttributionStatus(s SavingsSummary) string {
	if s.DecisionCodexRequests == 0 {
		return ""
	}
	if s.DecisionCodexUnattributedRequests > 0 {
		return "attention"
	}
	return "ok"
}

func savingsClientFamily(summary dbg.RequestSummary) string {
	value := strings.ToLower(strings.TrimSpace(summary.ClientFamily))
	if value != "" {
		return value
	}
	source := strings.ToLower(strings.TrimSpace(summary.Source))
	switch {
	case strings.Contains(source, "cli"):
		return "codex_cli"
	case strings.Contains(source, "desktop"), strings.Contains(source, "app"):
		return "codex_desktop_app"
	default:
		return ""
	}
}

func enrichSavingsSessions(sessions []SavingsSessionSummary) {
	ids := make([]string, 0, len(sessions))
	seen := make(map[string]struct{}, len(sessions))
	for i := range sessions {
		raw := strings.TrimSpace(sessions[i].SessionID)
		if id := codexthreads.NormalizeSessionID(raw); id != "" && isCodexAttributedSession(raw) {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	sort.Strings(ids)
	metadata, err := lookupCodexThreadMetadataForSavingsFn(ids)
	if err != nil {
		return
	}
	for i := range sessions {
		id := codexthreads.NormalizeSessionID(sessions[i].SessionID)
		meta, ok := metadata[id]
		if !ok {
			continue
		}
		applyCodexThreadMetadata(&sessions[i], meta)
	}
}

func lookupCodexThreadMetadataForSavings(ids []string) (map[string]codexthreads.Metadata, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return codexthreads.Lookup(home, ids)
}

func lookupCodexThreadWindowForSavings(start, end time.Time) ([]codexthreads.Metadata, error) {
	return codexthreads.LookupWindowDefault(start, end)
}

func applyCodexThreadMetadata(session *SavingsSessionSummary, meta codexthreads.Metadata) {
	if session == nil {
		return
	}
	if title := savingsThreadDisplayName(meta); title != "" {
		session.DisplayName = title
	}
	if cwd := strings.TrimSpace(meta.CWD); cwd != "" {
		session.ProjectPath = cwd
	}
	if family := savingsThreadClientFamily(meta); family != "" {
		session.ClientFamily = family
	}
}

func savingsThreadDisplayName(meta codexthreads.Metadata) string {
	title := strings.TrimSpace(meta.Title)
	title = strings.TrimLeft(title, "›> \t")
	if title != "" {
		return title
	}
	return compactSavingsPath(meta.CWD)
}

func savingsThreadClientFamily(meta codexthreads.Metadata) string {
	value := strings.ToLower(strings.TrimSpace(firstNonEmpty(meta.Source, meta.ThreadSource)))
	switch {
	case strings.Contains(value, "cli"):
		return "codex_cli"
	case strings.Contains(value, "desktop"), strings.Contains(value, "app"), strings.Contains(value, "chatgpt"), strings.Contains(value, "vscode"):
		return "codex_desktop_app"
	default:
		return ""
	}
}

func isCodexAttributedSession(id string) bool {
	return isCodexThreadSession(id) || isCodexLocalThreadSession(id)
}

func isCodexThreadSession(id string) bool {
	return strings.HasPrefix(id, "codex-wss:") ||
		strings.HasPrefix(id, "codex-wss_") ||
		strings.HasPrefix(id, "codex-http:") ||
		strings.HasPrefix(id, "codex-http_")
}

func isCodexLocalThreadSession(id string) bool {
	return strings.HasPrefix(id, "codex-local:") ||
		strings.HasPrefix(id, "codex-local_")
}

func accumulateProxyFlightsFromDecisionLog(out *SavingsSummary, cfg *config.Config, period string, now time.Time) {
	path := strings.TrimSpace(cfg.Debug.DecisionsLog)
	if path == "" {
		return
	}
	path = filepath.Clean(config.ExpandHomePath(path))
	summaries, err := replaySessionFn(path)
	if err != nil {
		return
	}
	start, end, err := savingsReportWindow(period, now)
	if err != nil {
		return
	}
	report, err := analytics.SummarizeProxyFlights(filterSummariesByWindow(summaries, start, end), "all", now)
	if err != nil || report.Requests == 0 {
		return
	}
	out.ProxyRequests = int64(report.Requests)
	out.ProviderReportedRequests = int64(report.ProviderReportedRequests)
	out.ProxyOrigTokens = int64(report.EstimatedOriginalInputTokens)
	out.ProxyCompTokens = int64(report.EstimatedFinalInputTokens)
	out.ProxySavedTokens = int64(report.BillableInputSavingsEstimate)
	out.ProviderInputTokens = int64(report.ProviderInputTokens)
	out.ProviderCachedTokens = int64(report.ProviderCachedTokens)
	out.ProviderOutputTokens = int64(report.ProviderOutputTokens)
	out.OutputReduceInputOverheadTokens = int64(report.OutputReduceInputOverheadTokens)
	out.CacheReadDiscountTokenEquivalent = int64(report.CacheReadDiscountTokenEquivalent)
}

func savingsReportWindow(period string, now time.Time) (time.Time, time.Time, error) {
	if period != "live" {
		return analytics.FilterGainWindow(period, now)
	}
	start := now.Add(-30 * time.Minute)
	if running, pf, err := daemonIsRunningFn(); err == nil && running && pf != nil && !pf.StartedAt.IsZero() {
		start = pf.StartedAt
	}
	if start.After(now) {
		start = now
	}
	return start, now, nil
}

func filterSummariesByWindow(summaries []dbg.RequestSummary, start, end time.Time) []dbg.RequestSummary {
	if len(summaries) == 0 {
		return nil
	}
	filtered := make([]dbg.RequestSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Timestamp.IsZero() || summary.Timestamp.Before(start) || summary.Timestamp.After(end) {
			continue
		}
		filtered = append(filtered, summary)
	}
	return filtered
}

func accumulateSnapshots(out *SavingsSummary, snapshots []analytics.AnalyticsSnapshot) {
	for _, snap := range snapshots {
		out.ProxyRequests += int64(snap.TotalRequests)
		out.ProxyOrigTokens += int64(snap.TotalInputTokens)
		// CompTokens approximates the on-the-wire size: original minus
		// the saved input tokens reported by the snapshot.
		comp := int64(snap.TotalInputTokens - snap.SavedInputTokens)
		if comp < 0 {
			comp = 0
		}
		out.ProxyCompTokens += comp
		out.ProxySavedTokens += int64(snap.SavedInputTokens)
		out.CacheHits += int64(snap.CacheHits)
	}
}

// formatSavingsText renders a human-readable savings block. T80.
func formatSavingsText(s SavingsSummary) string {
	var sb strings.Builder
	title := "Slimference savings (" + s.Period + ")"
	if s.Project != "" {
		title += " - project " + s.Project
	}
	sb.WriteString(title + "\n")
	sb.WriteString(strings.Repeat("=", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Layer 0 filter runs:        %d\n", s.Layer0Runs))
	sb.WriteString(fmt.Sprintf("Layer 0 tokens saved:       %s\n", formatInt64Plain(s.Layer0SavedTokens)))
	sb.WriteString(fmt.Sprintf("Proxy requests:             %d\n", s.ProxyRequests))
	sb.WriteString(fmt.Sprintf("Proxy original tokens:      %s\n", formatInt64Plain(s.ProxyOrigTokens)))
	sb.WriteString(fmt.Sprintf("Proxy compressed tokens:    %s\n", formatInt64Plain(s.ProxyCompTokens)))
	sb.WriteString(fmt.Sprintf("Proxy tokens saved:         %s\n", formatInt64Plain(s.ProxySavedTokens)))
	if s.ProviderReportedRequests > 0 {
		sb.WriteString(fmt.Sprintf("Provider-reported requests: %d\n", s.ProviderReportedRequests))
		sb.WriteString(fmt.Sprintf("Provider input tokens:      %s\n", formatInt64Plain(s.ProviderInputTokens)))
		sb.WriteString(fmt.Sprintf("Provider cached tokens:     %s\n", formatInt64Plain(s.ProviderCachedTokens)))
		sb.WriteString(fmt.Sprintf("Provider output tokens:     %s\n", formatInt64Plain(s.ProviderOutputTokens)))
		sb.WriteString(fmt.Sprintf("Cache-read billable equiv.: %s\n", formatInt64Plain(s.CacheReadDiscountTokenEquivalent)))
	}
	if s.DecisionRequests > 0 {
		sb.WriteString(fmt.Sprintf("Decision-log requests:       %d\n", s.DecisionRequests))
		if s.DecisionProviderInputTokens > 0 || s.DecisionCacheReadTokens > 0 || s.DecisionCacheCreateTokens > 0 {
			sb.WriteString(fmt.Sprintf("Decision provider input:     %s\n", formatInt64Plain(s.DecisionProviderInputTokens)))
			sb.WriteString(fmt.Sprintf("Decision effective billed:   %s (r=%.2f)\n", formatInt64Plain(s.DecisionEffectiveBilledTokens), s.CachedPriceRatio))
			sb.WriteString(fmt.Sprintf("Decision cached share:       %.1f%%\n", s.DecisionCachedShare*100))
			sb.WriteString(fmt.Sprintf("Decision scorecard:          S_local=%.1f%% S_combined=%.1f%% S_vs_uncached=%.1f%%\n",
				s.DecisionLocalSavingsRate*100,
				s.DecisionCombinedSavingsRate*100,
				s.DecisionVsUncachedSavingsRate*100,
			))
		}
		sb.WriteString(fmt.Sprintf("Decision original tokens:    %s\n", formatInt64Plain(s.DecisionOriginalTokens)))
		sb.WriteString(fmt.Sprintf("Decision final tokens:       %s\n", formatInt64Plain(s.DecisionFinalTokens)))
		sb.WriteString(fmt.Sprintf("Decision added tokens:       %s\n", formatInt64Plain(s.DecisionAddedTokens)))
		sb.WriteString(fmt.Sprintf("Decision local saved tokens: %s\n", formatSignedInt64Plain(s.DecisionLocalSavedTokens)))
		sb.WriteString(fmt.Sprintf("Decision net saved tokens:   %s\n", formatSignedInt64Plain(s.DecisionNetSavedTokens)))
		if s.DecisionNegativeEvents > 0 {
			sb.WriteString(fmt.Sprintf("Decision negative events:    %d (-%s tokens)\n",
				s.DecisionNegativeEvents,
				formatInt64Plain(s.DecisionNegativeEventTokens),
			))
		}
		if s.DecisionOutputTokens > 0 {
			sb.WriteString(fmt.Sprintf("Decision output tokens:      %s\n", formatInt64Plain(s.DecisionOutputTokens)))
		}
		if s.DecisionCacheReadTokens > 0 || s.DecisionCacheCreateTokens > 0 {
			sb.WriteString(fmt.Sprintf("Decision cache read/create:  %s / %s\n", formatInt64Plain(s.DecisionCacheReadTokens), formatInt64Plain(s.DecisionCacheCreateTokens)))
			sb.WriteString(fmt.Sprintf("Decision cache net:          %s (%s, %d hit req, %.1f%% hit, %d create req, %d negative net)\n",
				formatSignedInt64Plain(s.DecisionCacheNetTokens),
				formatSavingsHealthStatus(s.DecisionCacheStatus),
				s.DecisionCacheHitRequests,
				s.DecisionCacheHitRate*100,
				s.DecisionCacheCreateRequests,
				s.DecisionCacheNegativeNetRequests,
			))
		} else if s.DecisionCacheStatus != "" {
			sb.WriteString(fmt.Sprintf("Decision cache status:       %s\n", formatSavingsHealthStatus(s.DecisionCacheStatus)))
		}
		if s.DecisionCodexRequests > 0 {
			sb.WriteString(fmt.Sprintf("Codex attribution:           %d/%d attributed (%s, %.1f%%, %d unattributed)\n",
				s.DecisionCodexAttributedRequests,
				s.DecisionCodexRequests,
				formatSavingsHealthStatus(s.DecisionCodexAttributionStatus),
				s.DecisionCodexAttributionRate*100,
				s.DecisionCodexUnattributedRequests,
			))
			if len(s.DecisionCodexUnattributedReasons) > 0 {
				sb.WriteString(fmt.Sprintf("Codex unattributed reasons:  %s\n", formatCodexUnattributedReasons(s.DecisionCodexUnattributedReasons)))
			}
		}
		sb.WriteString(fmt.Sprintf("Decision layer net:          %s\n", formatDecisionLayerBreakdown(s)))
		if s.DecisionFootprintScore > 0 || len(s.DecisionFootprintScoreBuckets) > 0 {
			sb.WriteString(fmt.Sprintf("Decision footprint score:    %s (%s)\n",
				formatInt64Plain(s.DecisionFootprintScore),
				formatSavingsBucketCounts(s.DecisionFootprintScoreBuckets),
			))
		}
		if s.DecisionCompoundedEstimateTokens > 0 {
			sb.WriteString(fmt.Sprintf("Decision compounded est.:    %s\n", formatInt64Plain(s.DecisionCompoundedEstimateTokens)))
		}
		if s.Evidence.Decisions > 0 {
			sb.WriteString(fmt.Sprintf("Evidence decisions:          %d (%d applied, %d full-pass, %d failed-open, net=%s)\n",
				s.Evidence.Decisions,
				s.Evidence.Applied,
				s.Evidence.FullPass,
				s.Evidence.FailedOpen,
				formatSignedInt64Plain(s.Evidence.NetTokens),
			))
			sb.WriteString(fmt.Sprintf("Evidence classes/signals:    %s / %s\n",
				formatSavingsTopCounts(s.Evidence.ByContentClass, 4),
				formatSavingsTopCounts(s.Evidence.BySignal, 6),
			))
			if len(s.Evidence.ByCacheImpact) > 0 {
				sb.WriteString(fmt.Sprintf("Evidence cache impact:       %s\n", formatSavingsTopCounts(s.Evidence.ByCacheImpact, 4)))
			}
			if s.Evidence.FootprintScore > 0 || len(s.Evidence.ByFootprint) > 0 {
				sb.WriteString(fmt.Sprintf("Evidence footprint score:    %s (%s)\n",
					formatInt64Plain(s.Evidence.FootprintScore),
					formatSavingsBucketCounts(s.Evidence.ByFootprint),
				))
			}
		}
		if s.DecisionEstimatedCostBeforeUSD > 0 || s.DecisionEstimatedCostAfterUSD > 0 || s.DecisionEstimatedCostSavedUSD > 0 {
			sb.WriteString(fmt.Sprintf("Decision cost before/after:  ~$%.4f / ~$%.4f (saved ~$%.4f)\n",
				s.DecisionEstimatedCostBeforeUSD,
				s.DecisionEstimatedCostAfterUSD,
				s.DecisionEstimatedCostSavedUSD,
			))
		}
		for i, mechanism := range s.Mechanisms {
			if i >= 8 {
				break
			}
			footprintText := formatSavingsFootprintSuffix(mechanism.FootprintScore, mechanism.FootprintBuckets)
			compoundedText := formatSavingsCompoundedSuffix(mechanism.CompoundedEstimateTokens)
			sb.WriteString(fmt.Sprintf("  mechanism %-28s net=%s saved=%s added=%s count=%d%s%s\n",
				mechanism.Name,
				formatSignedInt64Plain(mechanism.NetTokens),
				formatInt64Plain(mechanism.SavedTokens),
				formatInt64Plain(mechanism.AddedTokens),
				mechanism.Count,
				footprintText,
				compoundedText,
			))
		}
		for i, session := range s.DecisionSessions {
			if i >= 5 {
				break
			}
			layerText := formatSessionLayerBreakdown(session)
			label := formatSavingsSessionLabel(session)
			negativeText := ""
			if session.NegativeEvents > 0 {
				negativeText = fmt.Sprintf(" negative=%d/-%s", session.NegativeEvents, formatInt64Plain(session.NegativeEventTokens))
			}
			if session.CostBeforeUSD > 0 || session.CostAfterUSD > 0 || session.CostSavedUSD > 0 {
				sb.WriteString(fmt.Sprintf("  session %-58s net=%s local_saved=%s effective_billed=%s cached_share=%.1f%% S_local=%.1f%% S_combined=%.1f%% S_vs_uncached=%.1f%% layers=%s cache=%s/%.1f%% original=%s final=%s%s cost=~$%.4f/~$%.4f%s%s requests=%d\n",
					label,
					formatSignedInt64Plain(session.NetSavedTokens),
					formatSignedInt64Plain(session.LocalSaved),
					formatInt64Plain(session.EffectiveBilled),
					session.CachedShare*100,
					scorecardLocalRate(session)*100,
					scorecardCombinedRate(session)*100,
					scorecardVsUncachedRate(session)*100,
					layerText,
					formatSignedInt64Plain(session.CacheNetTokens),
					session.CacheHitRate*100,
					formatInt64Plain(session.OriginalTokens),
					formatInt64Plain(session.FinalTokens),
					negativeText,
					session.CostBeforeUSD,
					session.CostAfterUSD,
					formatSavingsFootprintSuffix(session.FootprintScore, session.FootprintBuckets),
					formatSavingsCompoundedSuffix(session.CompoundedEstimateTokens),
					session.Requests,
				))
				continue
			}
			sb.WriteString(fmt.Sprintf("  session %-58s net=%s local_saved=%s effective_billed=%s cached_share=%.1f%% S_local=%.1f%% S_combined=%.1f%% S_vs_uncached=%.1f%% layers=%s cache=%s/%.1f%% original=%s final=%s%s%s%s requests=%d\n",
				label,
				formatSignedInt64Plain(session.NetSavedTokens),
				formatSignedInt64Plain(session.LocalSaved),
				formatInt64Plain(session.EffectiveBilled),
				session.CachedShare*100,
				scorecardLocalRate(session)*100,
				scorecardCombinedRate(session)*100,
				scorecardVsUncachedRate(session)*100,
				layerText,
				formatSignedInt64Plain(session.CacheNetTokens),
				session.CacheHitRate*100,
				formatInt64Plain(session.OriginalTokens),
				formatInt64Plain(session.FinalTokens),
				negativeText,
				formatSavingsFootprintSuffix(session.FootprintScore, session.FootprintBuckets),
				formatSavingsCompoundedSuffix(session.CompoundedEstimateTokens),
				session.Requests,
			))
		}
	}
	sb.WriteString(fmt.Sprintf("Layer 2 cache hits:         %d\n", s.CacheHits))
	sb.WriteString(strings.Repeat("-", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Total tokens saved:         %s\n", formatSignedInt64Plain(s.TotalSavedTokens)))
	if s.NetBillableEquivalentTokens != s.TotalSavedTokens {
		sb.WriteString(fmt.Sprintf("Billable-equivalent saved:  %s\n", formatSignedInt64Plain(s.NetBillableEquivalentTokens)))
	}
	if s.USDPerMillion > 0 {
		sb.WriteString(fmt.Sprintf("Total saved (~$%.2f/M est.): ~$%.4f\n", s.USDPerMillion, s.TotalSavedUSD))
	}
	return sb.String()
}

func formatSavingsSessionLabel(session SavingsSessionSummary) string {
	label := strings.TrimSpace(session.DisplayName)
	if label == "" {
		label = savingsSessionFallbackLabel(session)
	}
	if client := formatSavingsClientFamily(session.ClientFamily); client != "" {
		label = client + " - " + label
	}
	if project := compactSavingsPath(session.ProjectPath); project != "" && !strings.Contains(label, project) {
		label += " - " + project
	}
	return truncateSavingsLabel(label, 58)
}

func scorecardLocalRate(session SavingsSessionSummary) float64 {
	if session.Scorecard == nil {
		return 0
	}
	return session.Scorecard.LocalSavingsRate
}

func scorecardCombinedRate(session SavingsSessionSummary) float64 {
	if session.Scorecard == nil {
		return 0
	}
	return session.Scorecard.CombinedSavingsRate
}

func scorecardVsUncachedRate(session SavingsSessionSummary) float64 {
	if session.Scorecard == nil {
		return 0
	}
	return session.Scorecard.VsUncachedSavingsRate
}

func savingsSessionFallbackLabel(session SavingsSessionSummary) string {
	id := strings.TrimSpace(session.SessionID)
	switch {
	case id == "", strings.EqualFold(id, "empty"):
		if session.CacheReadTokens > 0 || session.CacheNetTokens > 0 {
			return "Unattributed provider cache"
		}
		return "Unattributed traffic"
	case strings.HasPrefix(id, "no-session:"):
		if session.CacheReadTokens > 0 || session.CacheNetTokens > 0 {
			return "Unattributed provider cache"
		}
		source := strings.TrimSpace(strings.TrimPrefix(id, "no-session:"))
		if source == "" || source == "unknown" {
			return "Unattributed traffic"
		}
		return "Unattributed " + source
	case isCodexAttributedSession(id):
		return "Codex thread " + truncateSavingsLabel(codexthreads.NormalizeSessionID(id), 12)
	default:
		return truncateSavingsLabel(id, 24)
	}
}

func formatSavingsClientFamily(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(value, "cli"):
		return "Codex CLI"
	case strings.Contains(value, "desktop"), strings.Contains(value, "app"), strings.Contains(value, "chatgpt"), strings.Contains(value, "vscode"):
		return "Codex App"
	default:
		return ""
	}
}

func formatSavingsHealthStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ok":
		return "ok"
	case "attention":
		return "attention"
	case "warming":
		return "warming"
	case "none":
		return "none"
	default:
		return "unknown"
	}
}

func formatCodexUnattributedReasons(reasons map[string]int64) string {
	return formatSavingsTopCounts(reasons, 0)
}

func formatSavingsTopCounts(counts map[string]int64, limit int) string {
	if len(counts) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(counts))
	for key, count := range counts {
		if strings.TrimSpace(key) == "" || count <= 0 {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] == counts[keys[j]] {
			return keys[i] < keys[j]
		}
		return counts[keys[i]] > counts[keys[j]]
	})
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func formatSavingsFootprintSuffix(score int64, buckets map[string]int64) string {
	if score <= 0 && len(buckets) == 0 {
		return ""
	}
	return " footprint=" + formatInt64Plain(score) + "/" + formatSavingsBucketCounts(buckets)
}

func formatSavingsCompoundedSuffix(tokens int64) string {
	if tokens <= 0 {
		return ""
	}
	return " compounded_est=" + formatInt64Plain(tokens)
}

func formatSavingsBucketCounts(counts map[string]int64) string {
	return formatSavingsBucketCountsWithSeparator(counts, ", ")
}

func formatSavingsBucketCountsCSV(counts map[string]int64) string {
	return formatSavingsBucketCountsWithSeparator(counts, "|")
}

func formatSavingsBucketCountsWithSeparator(counts map[string]int64, separator string) string {
	if len(counts) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(counts))
	seen := map[string]bool{}
	for _, key := range []string{"high", "mid", "low"} {
		if counts[key] > 0 {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	other := make([]string, 0, len(counts))
	for key, count := range counts {
		key = strings.TrimSpace(key)
		if key == "" || count <= 0 || seen[key] {
			continue
		}
		other = append(other, key)
	}
	sort.Strings(other)
	keys = append(keys, other...)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, separator)
}

func compactSavingsPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+string(os.PathSeparator)) {
			return "~" + strings.TrimPrefix(path, home)
		}
	}
	return path
}

func truncateSavingsLabel(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	if maxLen <= 1 {
		return value[:maxLen]
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	return value[:maxLen-3] + "..."
}

func formatDecisionLayerBreakdown(s SavingsSummary) string {
	session := SavingsSessionSummary{
		Layer0NetTokens:    s.DecisionLayer0NetTokens,
		Layer1NetTokens:    s.DecisionLayer1NetTokens,
		Layer2NetTokens:    s.DecisionLayer2NetTokens,
		Layer3NetTokens:    s.DecisionLayer3NetTokens,
		OutputReduceTokens: s.DecisionOutputReduceTokens,
		ToolPruneTokens:    s.DecisionToolPruneTokens,
	}
	return formatSessionLayerBreakdown(session)
}

func formatSessionLayerBreakdown(session SavingsSessionSummary) string {
	parts := make([]string, 0, 6)
	appendLayer := func(label string, value int64) {
		if value != 0 {
			parts = append(parts, label+"="+formatSignedInt64Plain(value))
		}
	}
	appendLayer("L0", session.Layer0NetTokens)
	appendLayer("L1", session.Layer1NetTokens)
	appendLayer("L2", session.Layer2NetTokens)
	appendLayer("L3", session.Layer3NetTokens)
	appendLayer("out", session.OutputReduceTokens)
	appendLayer("tools", session.ToolPruneTokens)
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ",")
}

// formatSavingsCSV emits a single-row CSV summary.
func formatSavingsCSV(s SavingsSummary) string {
	var sb strings.Builder
	sb.WriteString("period,project,layer0_runs,layer0_saved_tokens,proxy_requests,provider_reported_requests,proxy_orig_tokens,proxy_comp_tokens,proxy_saved_tokens,provider_input_tokens,provider_cached_tokens,provider_output_tokens,output_reduce_input_overhead_tokens,cache_read_discount_token_equivalent,net_billable_equivalent_tokens,cache_hits,cached_price_ratio,decision_requests,decision_provider_input_tokens,decision_original_tokens,decision_final_tokens,decision_added_tokens,decision_local_saved_tokens,decision_net_saved_tokens,decision_negative_events,decision_negative_event_tokens,decision_output_tokens,decision_cache_read_tokens,decision_cache_create_tokens,decision_cache_net_tokens,decision_cache_hit_requests,decision_cache_hit_rate,decision_cached_share,decision_effective_billed_tokens,decision_counterfactual_tokens,decision_uncached_counterfactual_tokens,decision_s_local,decision_s_combined,decision_s_vs_uncached,decision_cache_create_requests,decision_cache_negative_net_requests,decision_cache_status,decision_codex_requests,decision_codex_attributed_requests,decision_codex_unattributed_requests,decision_codex_unattributed_reasons,decision_codex_attribution_rate,decision_codex_attribution_status,decision_layer0_net_tokens,decision_layer1_net_tokens,decision_layer2_net_tokens,decision_layer3_net_tokens,decision_output_reduce_tokens,decision_tool_prune_tokens,decision_footprint_score,decision_footprint_score_buckets,decision_compounded_estimate_tokens,decision_estimated_cost_before_usd,decision_estimated_cost_after_usd,decision_estimated_cost_saved_usd,total_saved_tokens,total_saved_usd\n")
	sb.WriteString(fmt.Sprintf("%s,%s,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%.6f,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%.6f,%.6f,%d,%d,%d,%.6f,%.6f,%.6f,%d,%d,%s,%d,%d,%d,%s,%.6f,%s,%d,%d,%d,%d,%d,%d,%d,%s,%d,%.4f,%.4f,%.4f,%d,%.4f\n",
		s.Period,
		s.Project,
		s.Layer0Runs,
		s.Layer0SavedTokens,
		s.ProxyRequests,
		s.ProviderReportedRequests,
		s.ProxyOrigTokens,
		s.ProxyCompTokens,
		s.ProxySavedTokens,
		s.ProviderInputTokens,
		s.ProviderCachedTokens,
		s.ProviderOutputTokens,
		s.OutputReduceInputOverheadTokens,
		s.CacheReadDiscountTokenEquivalent,
		s.NetBillableEquivalentTokens,
		s.CacheHits,
		s.CachedPriceRatio,
		s.DecisionRequests,
		s.DecisionProviderInputTokens,
		s.DecisionOriginalTokens,
		s.DecisionFinalTokens,
		s.DecisionAddedTokens,
		s.DecisionLocalSavedTokens,
		s.DecisionNetSavedTokens,
		s.DecisionNegativeEvents,
		s.DecisionNegativeEventTokens,
		s.DecisionOutputTokens,
		s.DecisionCacheReadTokens,
		s.DecisionCacheCreateTokens,
		s.DecisionCacheNetTokens,
		s.DecisionCacheHitRequests,
		s.DecisionCacheHitRate,
		s.DecisionCachedShare,
		s.DecisionEffectiveBilledTokens,
		s.DecisionCounterfactualTokens,
		s.DecisionUncachedCounterfactual,
		s.DecisionLocalSavingsRate,
		s.DecisionCombinedSavingsRate,
		s.DecisionVsUncachedSavingsRate,
		s.DecisionCacheCreateRequests,
		s.DecisionCacheNegativeNetRequests,
		s.DecisionCacheStatus,
		s.DecisionCodexRequests,
		s.DecisionCodexAttributedRequests,
		s.DecisionCodexUnattributedRequests,
		formatCodexUnattributedReasons(s.DecisionCodexUnattributedReasons),
		s.DecisionCodexAttributionRate,
		s.DecisionCodexAttributionStatus,
		s.DecisionLayer0NetTokens,
		s.DecisionLayer1NetTokens,
		s.DecisionLayer2NetTokens,
		s.DecisionLayer3NetTokens,
		s.DecisionOutputReduceTokens,
		s.DecisionToolPruneTokens,
		s.DecisionFootprintScore,
		formatSavingsBucketCountsCSV(s.DecisionFootprintScoreBuckets),
		s.DecisionCompoundedEstimateTokens,
		s.DecisionEstimatedCostBeforeUSD,
		s.DecisionEstimatedCostAfterUSD,
		s.DecisionEstimatedCostSavedUSD,
		s.TotalSavedTokens,
		s.TotalSavedUSD,
	))
	return sb.String()
}

func formatInt64Plain(n int64) string {
	return formatTokensPlain64(n)
}

func formatSignedInt64Plain(n int64) string {
	if n < 0 {
		return "-" + formatTokensPlain64(-n)
	}
	return formatTokensPlain64(n)
}

func estimateCostUSD(inputTokens, outputTokens, savedTokens, cacheReadTokens, cacheCreateTokens int64, usdPerMillion float64) (float64, float64, float64) {
	beforeTokens := inputTokens + outputTokens
	savedEquivalentTokens := savedTokens + cacheReadDiscountEquivalent(cacheReadTokens, 0.10) - cacheCreateTokens
	if savedEquivalentTokens < 0 {
		savedEquivalentTokens = 0
	}
	if savedEquivalentTokens > beforeTokens {
		savedEquivalentTokens = beforeTokens
	}
	afterTokens := beforeTokens - savedEquivalentTokens
	return tokensToUSD(beforeTokens, usdPerMillion), tokensToUSD(afterTokens, usdPerMillion), tokensToUSD(savedEquivalentTokens, usdPerMillion)
}

func cacheReadDiscountEquivalent(tokens int64, cachedPriceRatio float64) int64 {
	if tokens <= 0 {
		return 0
	}
	if cachedPriceRatio < 0 {
		cachedPriceRatio = 0
	}
	if cachedPriceRatio > 1 {
		cachedPriceRatio = 1
	}
	return int64(float64(tokens) * (1 - cachedPriceRatio))
}

func savingsCachedPriceRatio(cfg *config.Config) float64 {
	if cfg == nil {
		return 0.10
	}
	return cfg.Savings.CachedPriceRatio
}

func tokensToUSD(tokens int64, usdPerMillion float64) float64 {
	if tokens <= 0 || usdPerMillion <= 0 {
		return 0
	}
	return float64(tokens) / 1_000_000.0 * usdPerMillion
}

func maxSavingsInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// handleSavingsCmd implements `slimference savings [live|today|week|month|all]`.
// T80 unified view that collapses gain + stats + cache into one summary.
func handleSavingsCmd(args []string) {
	period, flags, err := parseSavingsArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
		return
	}
	switch period {
	case "live", "today", "week", "month", "all":
	default:
		fmt.Fprintln(os.Stderr, "usage: slimference savings [live|today|week|month|all] [--json] [--csv] [--project <path>]")
		exitFn(1)
		return
	}
	cfg, err := configLoadFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	summary := computeSavings(cfg, period, flags.project, time.Now())
	switch {
	case flags.json:
		out, _ := json.MarshalIndent(&summary, "", "  ")
		fmt.Println(string(out))
	case flags.csv:
		fmt.Print(formatSavingsCSV(summary))
	default:
		fmt.Print(formatSavingsText(summary))
	}
}

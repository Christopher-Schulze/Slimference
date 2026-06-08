package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/codexthreads"
	"github.com/slimference/slimference/internal/config"
	dbg "github.com/slimference/slimference/internal/debug"
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
	Period                           string                    `json:"period"`
	Project                          string                    `json:"project,omitempty"`
	Layer0Runs                       int64                     `json:"layer0_runs"`
	Layer0SavedTokens                int64                     `json:"layer0_saved_tokens"`
	Layer0SavedUSD                   float64                   `json:"layer0_saved_usd"`
	ProxyOrigTokens                  int64                     `json:"proxy_orig_tokens"`
	ProxyCompTokens                  int64                     `json:"proxy_comp_tokens"`
	ProxySavedTokens                 int64                     `json:"proxy_saved_tokens"`
	ProxyRequests                    int64                     `json:"proxy_requests"`
	ProviderReportedRequests         int64                     `json:"provider_reported_requests"`
	ProviderInputTokens              int64                     `json:"provider_input_tokens"`
	ProviderCachedTokens             int64                     `json:"provider_cached_tokens"`
	ProviderOutputTokens             int64                     `json:"provider_output_tokens"`
	OutputReduceInputOverheadTokens  int64                     `json:"output_reduce_input_overhead_tokens"`
	CacheReadDiscountTokenEquivalent int64                     `json:"cache_read_discount_token_equivalent"`
	NetBillableEquivalentTokens      int64                     `json:"net_billable_equivalent_tokens"`
	CacheHits                        int64                     `json:"cache_hits"`
	TotalSavedTokens                 int64                     `json:"total_saved_tokens"`
	TotalSavedUSD                    float64                   `json:"total_saved_usd"`
	USDPerMillion                    float64                   `json:"usd_per_million_tokens"`
	DecisionRequests                 int64                     `json:"decision_requests"`
	DecisionOriginalTokens           int64                     `json:"decision_original_tokens"`
	DecisionFinalTokens              int64                     `json:"decision_final_tokens"`
	DecisionAddedTokens              int64                     `json:"decision_added_tokens"`
	DecisionNetSavedTokens           int64                     `json:"decision_net_saved_tokens"`
	DecisionOutputTokens             int64                     `json:"decision_output_tokens"`
	DecisionCacheReadTokens          int64                     `json:"decision_cache_read_tokens"`
	DecisionCacheCreateTokens        int64                     `json:"decision_cache_create_tokens"`
	DecisionCacheNetTokens           int64                     `json:"decision_cache_net_tokens"`
	DecisionCacheHitRequests         int64                     `json:"decision_cache_hit_requests"`
	DecisionCacheCreateRequests      int64                     `json:"decision_cache_create_requests"`
	DecisionCacheNegativeNetRequests int64                     `json:"decision_cache_negative_net_requests"`
	DecisionCacheHitRate             float64                   `json:"decision_cache_hit_rate"`
	DecisionLayer0NetTokens          int64                     `json:"decision_layer0_net_tokens"`
	DecisionLayer1NetTokens          int64                     `json:"decision_layer1_net_tokens"`
	DecisionLayer2NetTokens          int64                     `json:"decision_layer2_net_tokens"`
	DecisionLayer3NetTokens          int64                     `json:"decision_layer3_net_tokens"`
	DecisionOutputReduceTokens       int64                     `json:"decision_output_reduce_tokens"`
	DecisionToolPruneTokens          int64                     `json:"decision_tool_prune_tokens"`
	DecisionEstimatedCostBeforeUSD   float64                   `json:"decision_estimated_cost_before_usd"`
	DecisionEstimatedCostAfterUSD    float64                   `json:"decision_estimated_cost_after_usd"`
	DecisionEstimatedCostSavedUSD    float64                   `json:"decision_estimated_cost_saved_usd"`
	Mechanisms                       []SavingsMechanismSummary `json:"mechanisms,omitempty"`
	DecisionSessions                 []SavingsSessionSummary   `json:"decision_sessions,omitempty"`
}

type SavingsMechanismSummary struct {
	Name           string `json:"name"`
	Layer          int    `json:"layer,omitempty"`
	Source         string `json:"source,omitempty"`
	Count          int64  `json:"count"`
	OriginalTokens int64  `json:"original_tokens"`
	FinalTokens    int64  `json:"final_tokens"`
	SavedTokens    int64  `json:"saved_tokens"`
	AddedTokens    int64  `json:"added_tokens"`
	NetTokens      int64  `json:"net_tokens"`
}

type SavingsSessionSummary struct {
	SessionID          string  `json:"session_id"`
	DisplayName        string  `json:"display_name,omitempty"`
	ProjectPath        string  `json:"project_path,omitempty"`
	ClientFamily       string  `json:"client_family,omitempty"`
	Requests           int64   `json:"requests"`
	OriginalTokens     int64   `json:"original_tokens"`
	FinalTokens        int64   `json:"final_tokens"`
	AddedTokens        int64   `json:"added_tokens"`
	NetSavedTokens     int64   `json:"net_saved_tokens"`
	Layer0NetTokens    int64   `json:"layer0_net_tokens,omitempty"`
	Layer1NetTokens    int64   `json:"layer1_net_tokens,omitempty"`
	Layer2NetTokens    int64   `json:"layer2_net_tokens,omitempty"`
	Layer3NetTokens    int64   `json:"layer3_net_tokens,omitempty"`
	OutputReduceTokens int64   `json:"output_reduce_tokens,omitempty"`
	ToolPruneTokens    int64   `json:"tool_prune_tokens,omitempty"`
	OutputTokens       int64   `json:"output_tokens"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CacheCreateTokens  int64   `json:"cache_create_tokens"`
	CacheNetTokens     int64   `json:"cache_net_tokens"`
	CacheHitRequests   int64   `json:"cache_hit_requests"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
	CostBeforeUSD      float64 `json:"cost_before_usd"`
	CostAfterUSD       float64 `json:"cost_after_usd"`
	CostSavedUSD       float64 `json:"cost_saved_usd"`
}

var lookupCodexThreadMetadataForSavingsFn = lookupCodexThreadMetadataForSavings

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
		Period:        period,
		Project:       project,
		USDPerMillion: cfg.Analytics.GainUSDPerMillionTokens,
	}

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
	}

	if project == "" {
		accumulateProxyFlightsFromDecisionLog(&out, cfg, period, now)
		accumulateDecisionMechanismsFromDecisionLog(&out, cfg, period, now)
	}

	out.TotalSavedTokens = out.Layer0SavedTokens + out.ProxySavedTokens
	if out.DecisionNetSavedTokens > out.TotalSavedTokens {
		out.TotalSavedTokens = out.DecisionNetSavedTokens
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
		out.USDPerMillion,
	)
	for i := range out.DecisionSessions {
		before, after, saved := estimateCostUSD(
			out.DecisionSessions[i].OriginalTokens,
			out.DecisionSessions[i].OutputTokens,
			out.DecisionSessions[i].NetSavedTokens,
			out.DecisionSessions[i].CacheReadTokens,
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
	start, end, err := analytics.FilterGainWindow(period, now)
	if err != nil {
		return
	}
	byName := map[string]*SavingsMechanismSummary{}
	bySession := map[string]*SavingsSessionSummary{}
	for _, summary := range summaries {
		if summary.Timestamp.IsZero() || summary.Timestamp.Before(start) || summary.Timestamp.After(end) {
			continue
		}
		summary.EnsureMechanisms()
		out.DecisionRequests++
		out.DecisionOriginalTokens += int64(summary.Tokens.Original)
		out.DecisionFinalTokens += int64(summary.Tokens.Final)
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
		sessionRow := bySession[sessionID]
		if sessionRow == nil {
			sessionRow = &SavingsSessionSummary{SessionID: sessionID}
			bySession[sessionID] = sessionRow
		}
		if sessionRow.ClientFamily == "" {
			sessionRow.ClientFamily = savingsClientFamily(summary)
		}
		sessionRow.Requests++
		sessionRow.OriginalTokens += int64(summary.Tokens.Original)
		sessionRow.FinalTokens += int64(summary.Tokens.Final)
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
			if layer, ok := savingsMechanismLayer(mechanism); ok {
				layerObserved[layer] = true
				addSessionLayerNet(sessionRow, layer, int64(mechanism.NetTokens))
			}
		}
		addSessionLayerFallback(sessionRow, summary, layerObserved)
	}
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
		out.DecisionSessions = append(out.DecisionSessions, *row)
		out.DecisionLayer0NetTokens += row.Layer0NetTokens
		out.DecisionLayer1NetTokens += row.Layer1NetTokens
		out.DecisionLayer2NetTokens += row.Layer2NetTokens
		out.DecisionLayer3NetTokens += row.Layer3NetTokens
		out.DecisionOutputReduceTokens += row.OutputReduceTokens
		out.DecisionToolPruneTokens += row.ToolPruneTokens
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
	if mechanism.Layer == 0 && source == "decision_entry" {
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
		if id := codexthreads.NormalizeSessionID(raw); id != "" && isCodexThreadSession(raw) {
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
		if title := savingsThreadDisplayName(meta); title != "" {
			sessions[i].DisplayName = title
		}
		if cwd := strings.TrimSpace(meta.CWD); cwd != "" {
			sessions[i].ProjectPath = cwd
		}
		if family := savingsThreadClientFamily(meta); family != "" {
			sessions[i].ClientFamily = family
		}
	}
}

func lookupCodexThreadMetadataForSavings(ids []string) (map[string]codexthreads.Metadata, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return codexthreads.Lookup(home, ids)
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
	case strings.Contains(value, "desktop"), strings.Contains(value, "app"), strings.Contains(value, "chatgpt"):
		return "codex_desktop_app"
	default:
		return ""
	}
}

func isCodexThreadSession(id string) bool {
	return strings.HasPrefix(id, "codex-wss:") ||
		strings.HasPrefix(id, "codex-wss_") ||
		strings.HasPrefix(id, "codex-http:") ||
		strings.HasPrefix(id, "codex-http_")
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
	report, err := analytics.SummarizeProxyFlights(summaries, period, now)
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
		sb.WriteString(fmt.Sprintf("Decision original tokens:    %s\n", formatInt64Plain(s.DecisionOriginalTokens)))
		sb.WriteString(fmt.Sprintf("Decision final tokens:       %s\n", formatInt64Plain(s.DecisionFinalTokens)))
		sb.WriteString(fmt.Sprintf("Decision added tokens:       %s\n", formatInt64Plain(s.DecisionAddedTokens)))
		sb.WriteString(fmt.Sprintf("Decision net saved tokens:   %s\n", formatInt64Plain(s.DecisionNetSavedTokens)))
		if s.DecisionOutputTokens > 0 {
			sb.WriteString(fmt.Sprintf("Decision output tokens:      %s\n", formatInt64Plain(s.DecisionOutputTokens)))
		}
		if s.DecisionCacheReadTokens > 0 || s.DecisionCacheCreateTokens > 0 {
			sb.WriteString(fmt.Sprintf("Decision cache read/create:  %s / %s\n", formatInt64Plain(s.DecisionCacheReadTokens), formatInt64Plain(s.DecisionCacheCreateTokens)))
			sb.WriteString(fmt.Sprintf("Decision cache net:          %s (%d hit req, %.1f%% hit, %d create req, %d negative net)\n",
				formatSignedInt64Plain(s.DecisionCacheNetTokens),
				s.DecisionCacheHitRequests,
				s.DecisionCacheHitRate*100,
				s.DecisionCacheCreateRequests,
				s.DecisionCacheNegativeNetRequests,
			))
		}
		sb.WriteString(fmt.Sprintf("Decision layer net:          %s\n", formatDecisionLayerBreakdown(s)))
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
			sb.WriteString(fmt.Sprintf("  mechanism %-28s net=%s saved=%s added=%s count=%d\n",
				mechanism.Name,
				formatSignedInt64Plain(mechanism.NetTokens),
				formatInt64Plain(mechanism.SavedTokens),
				formatInt64Plain(mechanism.AddedTokens),
				mechanism.Count,
			))
		}
		for i, session := range s.DecisionSessions {
			if i >= 5 {
				break
			}
			layerText := formatSessionLayerBreakdown(session)
			label := formatSavingsSessionLabel(session)
			if session.CostBeforeUSD > 0 || session.CostAfterUSD > 0 || session.CostSavedUSD > 0 {
				sb.WriteString(fmt.Sprintf("  session %-58s net=%s layers=%s cache=%s/%.1f%% original=%s final=%s cost=~$%.4f/~$%.4f requests=%d\n",
					label,
					formatSignedInt64Plain(session.NetSavedTokens),
					layerText,
					formatSignedInt64Plain(session.CacheNetTokens),
					session.CacheHitRate*100,
					formatInt64Plain(session.OriginalTokens),
					formatInt64Plain(session.FinalTokens),
					session.CostBeforeUSD,
					session.CostAfterUSD,
					session.Requests,
				))
				continue
			}
			sb.WriteString(fmt.Sprintf("  session %-58s net=%s layers=%s cache=%s/%.1f%% original=%s final=%s requests=%d\n",
				label,
				formatSignedInt64Plain(session.NetSavedTokens),
				layerText,
				formatSignedInt64Plain(session.CacheNetTokens),
				session.CacheHitRate*100,
				formatInt64Plain(session.OriginalTokens),
				formatInt64Plain(session.FinalTokens),
				session.Requests,
			))
		}
	}
	sb.WriteString(fmt.Sprintf("Layer 2 cache hits:         %d\n", s.CacheHits))
	sb.WriteString(strings.Repeat("-", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Total tokens saved:         %s\n", formatInt64Plain(s.TotalSavedTokens)))
	if s.NetBillableEquivalentTokens != s.TotalSavedTokens {
		sb.WriteString(fmt.Sprintf("Billable-equivalent saved:  %s\n", formatInt64Plain(s.NetBillableEquivalentTokens)))
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
	case isCodexThreadSession(id):
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
	case strings.Contains(value, "desktop"), strings.Contains(value, "app"), strings.Contains(value, "chatgpt"):
		return "Codex App"
	default:
		return ""
	}
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
	sb.WriteString("period,project,layer0_runs,layer0_saved_tokens,proxy_requests,provider_reported_requests,proxy_orig_tokens,proxy_comp_tokens,proxy_saved_tokens,provider_input_tokens,provider_cached_tokens,provider_output_tokens,output_reduce_input_overhead_tokens,cache_read_discount_token_equivalent,net_billable_equivalent_tokens,cache_hits,decision_requests,decision_original_tokens,decision_final_tokens,decision_added_tokens,decision_net_saved_tokens,decision_output_tokens,decision_cache_read_tokens,decision_cache_create_tokens,decision_cache_net_tokens,decision_cache_hit_requests,decision_cache_hit_rate,decision_cache_create_requests,decision_cache_negative_net_requests,decision_layer0_net_tokens,decision_layer1_net_tokens,decision_layer2_net_tokens,decision_layer3_net_tokens,decision_output_reduce_tokens,decision_tool_prune_tokens,decision_estimated_cost_before_usd,decision_estimated_cost_after_usd,decision_estimated_cost_saved_usd,total_saved_tokens,total_saved_usd\n")
	sb.WriteString(fmt.Sprintf("%s,%s,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%.6f,%d,%d,%d,%d,%d,%d,%d,%d,%.4f,%.4f,%.4f,%d,%.4f\n",
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
		s.DecisionRequests,
		s.DecisionOriginalTokens,
		s.DecisionFinalTokens,
		s.DecisionAddedTokens,
		s.DecisionNetSavedTokens,
		s.DecisionOutputTokens,
		s.DecisionCacheReadTokens,
		s.DecisionCacheCreateTokens,
		s.DecisionCacheNetTokens,
		s.DecisionCacheHitRequests,
		s.DecisionCacheHitRate,
		s.DecisionCacheCreateRequests,
		s.DecisionCacheNegativeNetRequests,
		s.DecisionLayer0NetTokens,
		s.DecisionLayer1NetTokens,
		s.DecisionLayer2NetTokens,
		s.DecisionLayer3NetTokens,
		s.DecisionOutputReduceTokens,
		s.DecisionToolPruneTokens,
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

func estimateCostUSD(inputTokens, outputTokens, savedTokens, cacheReadTokens int64, usdPerMillion float64) (float64, float64, float64) {
	beforeTokens := inputTokens + outputTokens
	savedEquivalentTokens := savedTokens + cacheReadDiscountEquivalent(cacheReadTokens)
	if savedEquivalentTokens < 0 {
		savedEquivalentTokens = 0
	}
	if savedEquivalentTokens > beforeTokens {
		savedEquivalentTokens = beforeTokens
	}
	afterTokens := beforeTokens - savedEquivalentTokens
	return tokensToUSD(beforeTokens, usdPerMillion), tokensToUSD(afterTokens, usdPerMillion), tokensToUSD(savedEquivalentTokens, usdPerMillion)
}

func cacheReadDiscountEquivalent(tokens int64) int64 {
	return int64(float64(tokens) * 0.9)
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

// handleSavingsCmd implements `slimference savings [today|week|month|all]`.
// T80 unified view that collapses gain + stats + cache into one summary.
func handleSavingsCmd(args []string) {
	period, flags, err := parseSavingsArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
		return
	}
	switch period {
	case "today", "week", "month", "all":
	default:
		fmt.Fprintln(os.Stderr, "usage: slimference savings [today|week|month|all] [--json] [--csv] [--project <path>]")
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

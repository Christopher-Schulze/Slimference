package debug

import (
	"strings"
	"time"
)

const FlightSchemaVersion = 1

type FlightEvent struct {
	Timestamp time.Time         `json:"ts"`
	Stage     string            `json:"stage"`
	Source    string            `json:"source,omitempty"`
	RouteMode string            `json:"route_mode,omitempty"`
	Decision  string            `json:"decision,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	Layer     int               `json:"layer,omitempty"`
	ElapsedMs float64           `json:"elapsed_ms,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type FlightTokenAccounting struct {
	EstimatedOriginalInputTokens int `json:"estimated_original_input_tokens"`
	EstimatedFinalInputTokens    int `json:"estimated_final_input_tokens"`
	ProviderInputTokens          int `json:"provider_input_tokens,omitempty"`
	ProviderCachedTokens         int `json:"provider_cached_tokens,omitempty"`
	ProviderOutputTokens         int `json:"provider_output_tokens,omitempty"`
	EstimatedOutputTokens        int `json:"estimated_output_tokens,omitempty"`
	BillableSavingsEstimate      int `json:"billable_savings_estimate"`
	WireSavingsEstimate          int `json:"wire_savings_estimate"`
}

type FlightCacheAccounting struct {
	LocalResponseCacheHit         bool   `json:"local_response_cache_hit"`
	ProviderCacheReadTokens       int    `json:"provider_cache_read_tokens,omitempty"`
	ProviderCacheCreateTokens     int    `json:"provider_cache_create_tokens,omitempty"`
	ProviderCachedInputTokens     int    `json:"provider_cached_input_tokens,omitempty"`
	PromptCacheHintApplied        bool   `json:"prompt_cache_hint_applied,omitempty"`
	PromptCacheHintReason         string `json:"prompt_cache_hint_reason,omitempty"`
	PromptCacheStablePrefixHash   string `json:"prompt_cache_stable_prefix_hash,omitempty"`
	PromptCacheStablePrefixTokens int    `json:"prompt_cache_stable_prefix_tokens,omitempty"`
	PreviousResponseIDUsed        bool   `json:"previous_response_id_used,omitempty"`
	PreviousResponseIDBillable    bool   `json:"previous_response_id_billable_saving"`
}

type FlightOutputReduceAccounting struct {
	Applied     bool   `json:"applied"`
	Profile     string `json:"profile,omitempty"`
	Reason      string `json:"reason,omitempty"`
	AddedTokens int    `json:"added_tokens,omitempty"`
	TaskShape   string `json:"task_shape,omitempty"`
}

type FlightToolPruneAccounting struct {
	Applied     bool   `json:"applied"`
	Reason      string `json:"reason,omitempty"`
	PrunedTools int    `json:"pruned_tools,omitempty"`
	AlwaysKept  int    `json:"always_kept,omitempty"`
	SavedTokens int    `json:"saved_tokens,omitempty"`
	Reattached  int    `json:"reattached,omitempty"`
	Miss        bool   `json:"miss,omitempty"`
	Retry       bool   `json:"retry,omitempty"`
	Cooldown    bool   `json:"cooldown,omitempty"`
}

type FlightRequestSummary struct {
	SchemaVersion        int                          `json:"schema_version"`
	RequestID            string                       `json:"request_id"`
	SessionID            string                       `json:"session_id,omitempty"`
	TurnID               string                       `json:"turn_id,omitempty"`
	Source               string                       `json:"source"`
	Provider             string                       `json:"provider,omitempty"`
	Host                 string                       `json:"host,omitempty"`
	Path                 string                       `json:"path,omitempty"`
	ClientFamily         string                       `json:"client_family,omitempty"`
	RouteMode            string                       `json:"route_mode"`
	BypassReason         string                       `json:"bypass_reason,omitempty"`
	Layers               []int                        `json:"layers,omitempty"`
	TokenAccounting      FlightTokenAccounting        `json:"token_accounting"`
	CacheAccounting      FlightCacheAccounting        `json:"cache_accounting"`
	ToolPrune            FlightToolPruneAccounting    `json:"tool_prune"`
	OutputReduce         FlightOutputReduceAccounting `json:"output_reduce"`
	Mechanisms           []MechanismAccounting        `json:"mechanisms,omitempty"`
	Plan                 *PlanSummary                 `json:"plan,omitempty"`
	Errors               []string                     `json:"errors,omitempty"`
	PrivacyRedacted      bool                         `json:"privacy_redaction_state"`
	Confidence           string                       `json:"confidence"`
	Events               []FlightEvent                `json:"events,omitempty"`
	TotalProxyOverheadMs float64                      `json:"total_proxy_overhead_ms,omitempty"`
}

type OutputReduceSummary struct {
	Applied     bool   `json:"applied"`
	Profile     string `json:"profile,omitempty"`
	Reason      string `json:"reason,omitempty"`
	AddedTokens int    `json:"added_tokens,omitempty"`
	TaskShape   string `json:"task_shape,omitempty"`
}

func (s *RequestSummary) EnsureFlight() {
	if s == nil || s.Flight != nil {
		return
	}
	flight := BuildFlightRequestSummary(*s)
	s.Flight = &flight
}

func BuildFlightRequestSummary(s RequestSummary) FlightRequestSummary {
	source := strings.TrimSpace(s.Source)
	if source == "" {
		source = "proxy"
	}
	routeMode := strings.TrimSpace(s.RouteMode)
	if routeMode == "" {
		if s.CacheHit {
			routeMode = "local_cache"
		} else {
			routeMode = "upstream"
		}
	}
	outputTokens := s.OutputTokens
	confidence := "estimated"
	if s.ProviderInputTokens > 0 || s.ProviderOutputTokens > 0 || s.ProviderCachedTokens > 0 {
		confidence = "provider_reported"
	}
	if outputTokens == 0 {
		outputTokens = s.ProviderOutputTokens
	}
	flight := FlightRequestSummary{
		SchemaVersion: FlightSchemaVersion,
		RequestID:     s.RequestID,
		SessionID:     s.SessionID,
		TurnID:        s.TurnID,
		Source:        source,
		Provider:      s.Provider,
		Host:          s.Host,
		Path:          s.Path,
		ClientFamily:  s.ClientFamily,
		RouteMode:     routeMode,
		BypassReason:  s.BypassReason,
		Layers:        append([]int(nil), s.LayersApplied...),
		TokenAccounting: FlightTokenAccounting{
			EstimatedOriginalInputTokens: s.Tokens.Original,
			EstimatedFinalInputTokens:    s.Tokens.Final,
			ProviderInputTokens:          s.ProviderInputTokens,
			ProviderCachedTokens:         s.ProviderCachedTokens,
			ProviderOutputTokens:         s.ProviderOutputTokens,
			EstimatedOutputTokens:        outputTokens,
			BillableSavingsEstimate:      s.Tokens.Saved,
			WireSavingsEstimate:          s.Tokens.Saved,
		},
		CacheAccounting: FlightCacheAccounting{
			LocalResponseCacheHit:         s.CacheHit,
			ProviderCacheReadTokens:       s.CacheReadTokens,
			ProviderCacheCreateTokens:     s.CacheCreateTokens,
			ProviderCachedInputTokens:     s.ProviderCachedTokens,
			PromptCacheHintApplied:        s.PromptCache.Applied,
			PromptCacheHintReason:         s.PromptCache.Reason,
			PromptCacheStablePrefixHash:   s.PromptCache.StablePrefixHash,
			PromptCacheStablePrefixTokens: s.PromptCache.StablePrefixTokens,
			PreviousResponseIDUsed:        s.PreviousResponseIDUsed,
			PreviousResponseIDBillable:    false,
		},
		ToolPrune: FlightToolPruneAccounting{
			Applied:     s.ToolPrune.Applied,
			Reason:      s.ToolPrune.Reason,
			PrunedTools: s.ToolPrune.PrunedTools,
			AlwaysKept:  s.ToolPrune.AlwaysKept,
			SavedTokens: s.ToolPrune.SavedTokens,
			Reattached:  s.ToolPrune.Reattached,
			Miss:        s.ToolPrune.Miss,
			Retry:       s.ToolPrune.Retry,
			Cooldown:    s.ToolPrune.Cooldown,
		},
		OutputReduce: FlightOutputReduceAccounting{
			Applied:     s.OutputReduce.Applied,
			Profile:     s.OutputReduce.Profile,
			Reason:      s.OutputReduce.Reason,
			AddedTokens: s.OutputReduce.AddedTokens,
			TaskShape:   s.OutputReduce.TaskShape,
		},
		Mechanisms:           append([]MechanismAccounting(nil), s.Mechanisms...),
		Plan:                 clonePlanSummary(s.Plan),
		Errors:               append([]string(nil), s.Errors...),
		PrivacyRedacted:      true,
		Confidence:           confidence,
		TotalProxyOverheadMs: s.ProxyLatencyMs,
	}
	flight.Events = buildFlightEvents(s, flight)
	return flight
}

func buildFlightEvents(s RequestSummary, flight FlightRequestSummary) []FlightEvent {
	events := []FlightEvent{
		{
			Timestamp: s.Timestamp,
			Stage:     "ingress",
			Source:    flight.Source,
			RouteMode: flight.RouteMode,
			Decision:  "accepted",
		},
	}
	for _, layer := range s.LayersApplied {
		events = append(events, FlightEvent{
			Timestamp: s.Timestamp,
			Stage:     "layer",
			Layer:     layer,
			Decision:  "applied",
		})
	}
	if s.CacheHit || s.CacheReadTokens > 0 || s.CacheCreateTokens > 0 || s.ProviderCachedTokens > 0 {
		events = append(events, FlightEvent{
			Timestamp: s.Timestamp,
			Stage:     "cache",
			Decision:  cacheDecision(s),
		})
	}
	if s.PromptCache.Reason != "" {
		events = append(events, FlightEvent{
			Timestamp: s.Timestamp,
			Stage:     "prompt_cache",
			Decision:  boolDecision(s.PromptCache.Applied),
			Reason:    s.PromptCache.Reason,
			Fields: map[string]string{
				"key_set":              boolString(s.PromptCache.KeySet),
				"retention":            s.PromptCache.Retention,
				"stable_prefix_hash":   s.PromptCache.StablePrefixHash,
				"stable_prefix_tokens": intString(s.PromptCache.StablePrefixTokens),
			},
		})
	}
	if s.ToolPrune.Applied || s.ToolPrune.Reason != "" {
		events = append(events, FlightEvent{
			Timestamp: s.Timestamp,
			Stage:     "tool_prune",
			Decision:  boolDecision(s.ToolPrune.Applied),
			Reason:    s.ToolPrune.Reason,
			Fields: map[string]string{
				"pruned_tools": intString(s.ToolPrune.PrunedTools),
				"always_kept":  intString(s.ToolPrune.AlwaysKept),
				"saved_tokens": intString(s.ToolPrune.SavedTokens),
				"reattached":   intString(s.ToolPrune.Reattached),
				"miss":         boolString(s.ToolPrune.Miss),
				"retry":        boolString(s.ToolPrune.Retry),
				"cooldown":     boolString(s.ToolPrune.Cooldown),
			},
		})
	}
	if s.OutputReduce.Applied || s.OutputReduce.Reason != "" {
		events = append(events, FlightEvent{
			Timestamp: s.Timestamp,
			Stage:     "output_reduce",
			Decision:  boolDecision(s.OutputReduce.Applied),
			Reason:    s.OutputReduce.Reason,
		})
	}
	if s.Plan != nil {
		events = append(events, FlightEvent{
			Timestamp: s.Timestamp,
			Stage:     "planner",
			Decision:  plannerDecision(s.Plan),
			Fields: map[string]string{
				"decisions":   intString(len(s.Plan.Decisions)),
				"route_mode":  s.Plan.RouteMode,
				"safety_gate": boolString(s.Plan.SafetyBlocked),
			},
		})
	}
	if len(s.Errors) > 0 {
		for _, errText := range s.Errors {
			events = append(events, FlightEvent{
				Timestamp: s.Timestamp,
				Stage:     "error",
				Decision:  "recorded",
				Reason:    errText,
			})
		}
	}
	events = append(events, FlightEvent{
		Timestamp: s.Timestamp,
		Stage:     "egress",
		Decision:  "completed",
		ElapsedMs: s.ProxyLatencyMs,
	})
	return events
}

func clonePlanSummary(plan *PlanSummary) *PlanSummary {
	if plan == nil {
		return nil
	}
	out := *plan
	out.Decisions = append([]PlanDecisionSummary(nil), plan.Decisions...)
	return &out
}

func plannerDecision(plan *PlanSummary) string {
	if plan.SafetyBlocked {
		return "blocked"
	}
	return "advice_ready"
}

func boolDecision(ok bool) string {
	if ok {
		return "applied"
	}
	return "skipped"
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func intString(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func cacheDecision(s RequestSummary) string {
	if s.CacheHit {
		return "local_cache_hit"
	}
	if s.CacheReadTokens > 0 || s.ProviderCachedTokens > 0 {
		return "provider_cache_read"
	}
	if s.CacheCreateTokens > 0 {
		return "provider_cache_create"
	}
	return "observed"
}

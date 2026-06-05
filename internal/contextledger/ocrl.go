package contextledger

import (
	"strconv"
	"strings"
)

type OCRLMode string

const (
	OCRLModeOff    OCRLMode = "off"
	OCRLModeShadow OCRLMode = "shadow"
	OCRLModeAuto   OCRLMode = "auto"
	OCRLModeMax    OCRLMode = "max"
)

type OCRLRoute string

const (
	OCRLRouteUnknown         OCRLRoute = "unknown"
	OCRLRouteFullHistoryHTTP OCRLRoute = "full_history_http"
	OCRLRouteCodexWSS        OCRLRoute = "codex_wss"
)

type OCRLReason string

const (
	OCRLReasonApplied              OCRLReason = "applied"
	OCRLReasonOff                  OCRLReason = "off"
	OCRLReasonShadowOnly           OCRLReason = "shadow_only"
	OCRLReasonRouteNotEligible     OCRLReason = "route_not_eligible"
	OCRLReasonNoCapsules           OCRLReason = "no_capsules"
	OCRLReasonArchiveUnavailable   OCRLReason = "archive_unavailable"
	OCRLReasonMissingTokenCounter  OCRLReason = "missing_token_counter"
	OCRLReasonInvalidTokenCounter  OCRLReason = "invalid_token_counter"
	OCRLReasonMissingOriginalCount OCRLReason = "missing_original_token_count"
	OCRLReasonReplacementTooLarge  OCRLReason = "replacement_too_large"
	OCRLReasonNetSavingsTooSmall   OCRLReason = "net_savings_too_small"
)

type TokenCounter func(text string) int

type OCRLPolicy struct {
	Mode                   OCRLMode
	Route                  OCRLRoute
	Selection              SelectionPolicy
	ArchiveLoader          ArchiveLoader
	CountTokens            TokenCounter
	OriginalTokens         int
	RecoveryOverheadTokens int
	MinNetSavedTokens      int
	MaxReplacementTokens   int
}

type OCRLResult struct {
	Applied                bool            `json:"applied"`
	ShadowOnly             bool            `json:"shadow_only"`
	Reason                 OCRLReason      `json:"reason"`
	Text                   string          `json:"text,omitempty"`
	Selection              SelectionReport `json:"selection"`
	OriginalTokens         int             `json:"original_tokens"`
	ReplacementTokens      int             `json:"replacement_tokens"`
	RecoveryOverheadTokens int             `json:"recovery_overhead_tokens"`
	NetSavedTokens         int             `json:"net_saved_tokens"`
	ArchiveExpansions      int             `json:"archive_expansions"`
}

func BuildOCRLReplacement(capsules []Capsule, policy OCRLPolicy) OCRLResult {
	mode := normalizedOCRLMode(policy.Mode)
	result := OCRLResult{
		Reason:                 OCRLReasonOff,
		OriginalTokens:         policy.OriginalTokens,
		RecoveryOverheadTokens: policy.RecoveryOverheadTokens,
	}
	if mode == OCRLModeOff {
		return result
	}

	result.Selection = SelectCapsules(capsules, policy.Selection)
	selected := selectedCapsules(result.Selection)
	if len(selected) == 0 {
		result.Reason = OCRLReasonNoCapsules
		return result
	}
	for _, capsule := range selected {
		expansions, err := VerifyCapsuleArchives(capsule, policy.ArchiveLoader)
		if err != nil {
			result.Reason = OCRLReasonArchiveUnavailable
			return result
		}
		result.ArchiveExpansions += expansions
	}

	result.Text = RenderOCRLCapsules(selected)
	if policy.CountTokens == nil {
		result.Reason = OCRLReasonMissingTokenCounter
		return result
	}
	result.ReplacementTokens = policy.CountTokens(result.Text)
	if result.Text != "" && result.ReplacementTokens <= 0 {
		result.Reason = OCRLReasonInvalidTokenCounter
		return result
	}
	if policy.OriginalTokens <= 0 {
		result.Reason = OCRLReasonMissingOriginalCount
		return result
	}
	result.NetSavedTokens = policy.OriginalTokens - result.ReplacementTokens - policy.RecoveryOverheadTokens
	if policy.MaxReplacementTokens > 0 && result.ReplacementTokens > policy.MaxReplacementTokens {
		result.Reason = OCRLReasonReplacementTooLarge
		return result
	}

	eligibleRoute := normalizedOCRLRoute(policy.Route) == OCRLRouteFullHistoryHTTP
	if mode == OCRLModeShadow {
		result.ShadowOnly = true
		result.Reason = OCRLReasonShadowOnly
		return result
	}
	if !eligibleRoute {
		result.ShadowOnly = true
		result.Reason = OCRLReasonRouteNotEligible
		return result
	}
	minSaved := policy.MinNetSavedTokens
	if minSaved <= 0 {
		minSaved = 1
	}
	if result.NetSavedTokens < minSaved {
		result.Reason = OCRLReasonNetSavingsTooSmall
		return result
	}
	result.Applied = true
	result.Reason = OCRLReasonApplied
	return result
}

func RenderOCRLCapsules(capsules []Capsule) string {
	var b strings.Builder
	b.Grow(64 + len(capsules)*192)
	b.WriteString("[ocrl:v1 selected=")
	b.WriteString(strconv.Itoa(len(capsules)))
	b.WriteString(" archive_recoverable=true]\n")
	for _, capsule := range capsules {
		b.WriteString("- kind=")
		b.WriteString(string(capsule.Kind))
		b.WriteString(" session=")
		b.WriteString(quoteField(capsule.Provenance.SessionID))
		b.WriteString(" turn=")
		b.WriteString(quoteField(capsule.Provenance.TurnID))
		b.WriteString(" source=")
		b.WriteString(quoteField(capsule.Provenance.Source))
		b.WriteString(" archives=")
		b.WriteString(quoteList(capsule.Archives))
		b.WriteString(" facts=")
		b.WriteString(quoteMap(capsule.Facts))
		b.WriteString(" hashes=")
		b.WriteString(quoteMap(capsule.Hashes))
		b.WriteByte('\n')
	}
	return b.String()
}

func selectedCapsules(report SelectionReport) []Capsule {
	selected := make([]Capsule, 0, report.Capsules)
	for _, decision := range report.Decisions {
		if decision.Action == SelectionCapsule {
			selected = append(selected, decision.Capsule)
		}
	}
	return selected
}

func normalizedOCRLMode(mode OCRLMode) OCRLMode {
	switch mode {
	case OCRLModeShadow, OCRLModeAuto, OCRLModeMax:
		return mode
	default:
		return OCRLModeOff
	}
}

func normalizedOCRLRoute(route OCRLRoute) OCRLRoute {
	switch route {
	case OCRLRouteFullHistoryHTTP, OCRLRouteCodexWSS:
		return route
	default:
		return OCRLRouteUnknown
	}
}

func quoteField(value string) string {
	return strconv.Quote(strings.TrimSpace(value))
}

func quoteList(values []string) string {
	values = sortedStrings(values)
	if len(values) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.Grow(2 + len(values)*12)
	b.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(quoteField(value))
	}
	b.WriteByte(']')
	return b.String()
}

func quoteMap(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	keys = sortedStrings(keys)
	if len(keys) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.Grow(2 + len(keys)*24)
	b.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(quoteField(key))
		b.WriteByte(':')
		b.WriteString(quoteField(values[key]))
	}
	b.WriteByte('}')
	return b.String()
}

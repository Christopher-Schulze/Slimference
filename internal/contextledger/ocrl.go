package contextledger

import (
	"errors"
	"sort"
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
	Mode                     OCRLMode
	Route                    OCRLRoute
	Selection                SelectionPolicy
	ArchiveLoader            ArchiveLoader
	CountTokens              TokenCounter
	OriginalTokens           int
	UseArchiveOriginalTokens bool
	RecoveryOverheadTokens   int
	MinNetSavedTokens        int
	MaxReplacementTokens     int
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
	archiveOriginalTokens := 0
	for _, capsule := range selected {
		expansions, originalTokens, err := verifyCapsuleArchivesForOCRL(capsule, policy.ArchiveLoader, policy.CountTokens, policy.UseArchiveOriginalTokens)
		if err != nil {
			result.Reason = OCRLReasonArchiveUnavailable
			return result
		}
		result.ArchiveExpansions += expansions
		archiveOriginalTokens += originalTokens
	}
	if result.OriginalTokens <= 0 && policy.UseArchiveOriginalTokens {
		result.OriginalTokens = archiveOriginalTokens
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
	if result.OriginalTokens <= 0 {
		result.Reason = OCRLReasonMissingOriginalCount
		return result
	}
	result.NetSavedTokens = result.OriginalTokens - result.ReplacementTokens - policy.RecoveryOverheadTokens
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

func verifyCapsuleArchivesForOCRL(capsule Capsule, load ArchiveLoader, count TokenCounter, countOriginal bool) (int, int, error) {
	if !countOriginal {
		expansions, err := VerifyCapsuleArchives(capsule, load)
		return expansions, 0, err
	}
	if load == nil {
		return 0, 0, errors.New("archive loader is required")
	}
	ids := sortedArchiveIDs(capsule.Archives)
	if len(ids) == 0 {
		return 0, 0, errors.New("capsule has no archive ids")
	}
	originalTokens := 0
	for _, id := range ids {
		body, err := load(id)
		if err != nil {
			return 0, 0, err
		}
		if count != nil {
			originalTokens += count(string(body))
		}
	}
	return len(ids), originalTokens, nil
}

func RenderOCRLCapsules(capsules []Capsule) string {
	var b strings.Builder
	b.Grow(64 + len(capsules)*192)
	archiveScratch := make([]string, 0, 4)
	keyScratch := make([]string, 0, 8)
	quoteScratch := make([]byte, 0, 64)
	b.WriteString("[ocrl:v1 selected=")
	b.WriteString(strconv.Itoa(len(capsules)))
	b.WriteString(" archive_recoverable=true]\n")
	for _, capsule := range capsules {
		b.WriteString("- kind=")
		b.WriteString(string(capsule.Kind))
		b.WriteString(" session=")
		writeQuotedField(&b, capsule.Provenance.SessionID, &quoteScratch)
		b.WriteString(" turn=")
		writeQuotedField(&b, capsule.Provenance.TurnID, &quoteScratch)
		b.WriteString(" source=")
		writeQuotedField(&b, capsule.Provenance.Source, &quoteScratch)
		b.WriteString(" archives=")
		writeQuotedList(&b, capsule.Archives, &archiveScratch, &quoteScratch)
		b.WriteString(" facts=")
		writeQuotedMap(&b, capsule.Facts, &keyScratch, &quoteScratch)
		b.WriteString(" hashes=")
		writeQuotedMap(&b, capsule.Hashes, &keyScratch, &quoteScratch)
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
	normalized := OCRLMode(strings.ToLower(strings.TrimSpace(string(mode))))
	switch normalized {
	case OCRLModeShadow, OCRLModeAuto, OCRLModeMax:
		return normalized
	default:
		return OCRLModeOff
	}
}

func normalizedOCRLRoute(route OCRLRoute) OCRLRoute {
	normalized := OCRLRoute(strings.ToLower(strings.TrimSpace(string(route))))
	switch normalized {
	case OCRLRouteFullHistoryHTTP, OCRLRouteCodexWSS:
		return normalized
	default:
		return OCRLRouteUnknown
	}
}

func writeQuotedList(b *strings.Builder, values []string, scratch *[]string, quoteScratch *[]byte) {
	values = sortedStringsScratch(values, scratch)
	if len(values) == 0 {
		b.WriteString("[]")
		return
	}
	b.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		writeQuotedField(b, value, quoteScratch)
	}
	b.WriteByte(']')
}

func writeQuotedMap(b *strings.Builder, values map[string]string, scratch *[]string, quoteScratch *[]byte) {
	if len(values) == 0 {
		b.WriteString("{}")
		return
	}
	keys := (*scratch)[:0]
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if containsString(keys, key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	*scratch = keys
	if len(keys) == 0 {
		b.WriteString("{}")
		return
	}
	b.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		writeQuotedField(b, key, quoteScratch)
		b.WriteByte(':')
		writeQuotedField(b, values[key], quoteScratch)
	}
	b.WriteByte('}')
}

func writeQuotedField(b *strings.Builder, value string, scratch *[]byte) {
	*scratch = strconv.AppendQuote((*scratch)[:0], strings.TrimSpace(value))
	b.Write(*scratch)
}

func sortedArchiveIDs(values []string) []string {
	if len(values) == 1 {
		value := strings.TrimSpace(values[0])
		if value == "" {
			return nil
		}
		if value != values[0] {
			return []string{value}
		}
		return values[:1]
	}
	return sortedStrings(values)
}

func sortedStringsScratch(values []string, scratch *[]string) []string {
	if len(values) == 0 {
		return nil
	}
	out := (*scratch)[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || containsString(out, value) {
			continue
		}
		out = append(out, value)
	}
	sort.Strings(out)
	*scratch = out
	if len(out) == 0 {
		return nil
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

package summarization

import "regexp"

// mdHeaderRegex matches markdown header lines emitted by free-form
// LLM-style output (kept here as a general utility for repair.go).
var mdHeaderRegex = regexp.MustCompile(`(?m)^#{1,6}\s+.*$`)

// multiBlankLineRegex collapses three-or-more consecutive newlines
// into a single blank-line separator.
var multiBlankLineRegex = regexp.MustCompile(`\n{3,}`)

// capProvider is the narrow capability surface a Summarizer can expose
// to the FallbackChain. Determinism guarantees flow through here:
// providers that advertise SupportsTemperatureZero && SupportsSeed
// satisfy require_deterministic gates.
//
// Decoupled from internal/types to avoid an import cycle and to let
// tests inject custom capability profiles.
type capProvider struct {
	SupportsSeed                bool
	SupportsMinCompletionTokens bool
	// SupportsTemperatureZero records whether the provider is reliably
	// greedy at temperature=0. Read by the FallbackChain when
	// require_deterministic is on.
	SupportsTemperatureZero bool
}

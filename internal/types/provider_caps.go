package types

// ProviderCapabilities describes per-provider determinism levers and
// supported request fields. Used by the summarizer / proxy to decide
// what to set in upstream requests so a request never tries to use an
// unsupported parameter and fails with 4xx. T88.
//
// Adding a new capability: extend the struct, set defaults below, and
// teach the relevant call site to consult `Get()`. Removal must stay
// additive across releases so older configs do not regress.
type ProviderCapabilities struct {
	// SupportsSeed: provider honours a `seed` field for greedy
	// determinism beyond `temperature=0`.
	SupportsSeed bool `json:"supports_seed"`
	// SupportsTemperatureZero: temperature=0 is reliably greedy.
	SupportsTemperatureZero bool `json:"supports_temperature_zero"`
	// SupportsLogprobs: top-logprobs / token-level inspection field is
	// honoured.
	SupportsLogprobs bool `json:"supports_logprobs"`
	// SupportsMinCompletionTokens: provider honours a min-completion
	// or stop-floor parameter to avoid premature stops. T91 gate.
	SupportsMinCompletionTokens bool `json:"supports_min_completion_tokens"`
	// SupportsStopConditions: provider honours user-supplied stop
	// strings or stop conditions.
	SupportsStopConditions bool `json:"supports_stop_conditions"`
	// SupportsResponseID: provider exposes server-side conversation
	// state via `previous_response_id` (T78 gate). When true, the
	// proxy can skip resending the prefix on follow-up turns.
	SupportsResponseID bool `json:"supports_response_id"`
	// SupportsCachedPrefix: provider exposes prompt-caching
	// (Anthropic-style breakpoints).
	SupportsCachedPrefix bool `json:"supports_cached_prefix"`
}

// providerCapsRegistry holds the per-provider defaults. Returns a copy
// on Get so callers cannot mutate shared state.
var providerCapsRegistry = map[Provider]ProviderCapabilities{
	Anthropic: {
		SupportsSeed:                false,
		SupportsTemperatureZero:     true,
		SupportsLogprobs:            false,
		SupportsMinCompletionTokens: false,
		SupportsStopConditions:      true,
		SupportsResponseID:          false,
		SupportsCachedPrefix:        true,
	},
	OpenAI: {
		SupportsSeed:                true,
		SupportsTemperatureZero:     true,
		SupportsLogprobs:            true,
		SupportsMinCompletionTokens: false,
		SupportsStopConditions:      true,
		SupportsResponseID:          true,
		SupportsCachedPrefix:        false,
	},
	CodexChatGPT: {
		SupportsSeed:                false,
		SupportsTemperatureZero:     true,
		SupportsLogprobs:            false,
		SupportsMinCompletionTokens: false,
		SupportsStopConditions:      false,
		SupportsResponseID:          true,
		SupportsCachedPrefix:        false,
	},
}

// CapabilitiesFor returns the capability snapshot for the given
// provider. Unknown providers return a zero-value struct so call sites
// fail closed (treat the capability as absent rather than crash). T88.
func CapabilitiesFor(p Provider) ProviderCapabilities {
	caps, ok := providerCapsRegistry[p]
	if !ok {
		return ProviderCapabilities{}
	}
	return caps
}

// SetProviderCapabilities overrides the registered capability for a
// provider. Intended for tests and for runtime upgrades when a
// provider rolls out a new field. The returned function restores the
// previous capability so callers can defer cleanup. T88.
func SetProviderCapabilities(p Provider, caps ProviderCapabilities) func() {
	prev, had := providerCapsRegistry[p]
	providerCapsRegistry[p] = caps
	return func() {
		if had {
			providerCapsRegistry[p] = prev
		} else {
			delete(providerCapsRegistry, p)
		}
	}
}

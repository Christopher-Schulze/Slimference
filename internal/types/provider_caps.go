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
	// TrustClass labels the provider's data-flow relationship to the
	// operator. T121. One of TrustClassUpstreamProvider,
	// TrustClassExternalThirdParty, or TrustClassUnknown.
	TrustClass string `json:"trust_class"`
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
	SupportsCachedPrefix                bool `json:"supports_cached_prefix"`
	SupportsPromptCacheUsage            bool `json:"supports_prompt_cache_usage"`
	SupportsPromptCacheKey              bool `json:"supports_prompt_cache_key"`
	SupportsPromptCacheRetention        bool `json:"supports_prompt_cache_retention"`
	SupportsPreviousResponseIDHTTP      bool `json:"supports_previous_response_id_http"`
	SupportsPreviousResponseIDWebSocket bool `json:"supports_previous_response_id_websocket"`
	BillsPreviousResponseIDContext      bool `json:"bills_previous_response_id_context"`
}

// TrustClass labels a provider's data-flow relationship to the operator.
// T121. "upstream_provider" = the model the user is talking to (Anthropic,
// OpenAI, Codex). "external_third_party" = a side-channel optimisation
// provider the user did not ask to talk to (MiniMax). "unknown" = not
// declared (treated as external for safety).
const (
	TrustClassUpstreamProvider   = "upstream_provider"
	TrustClassExternalThirdParty = "external_third_party"
	TrustClassUnknown            = "unknown"
)

var providerCapsRegistry = map[Provider]ProviderCapabilities{
	Anthropic: {
		TrustClass:                  TrustClassUpstreamProvider,
		SupportsSeed:                false,
		SupportsTemperatureZero:     true,
		SupportsLogprobs:            false,
		SupportsMinCompletionTokens: false,
		SupportsStopConditions:      true,
		SupportsResponseID:          false,
		SupportsCachedPrefix:        true,
		SupportsPromptCacheUsage:    true,
	},
	OpenAI: {
		TrustClass:                     TrustClassUpstreamProvider,
		SupportsSeed:                   true,
		SupportsTemperatureZero:        true,
		SupportsLogprobs:               true,
		SupportsMinCompletionTokens:    false,
		SupportsStopConditions:         true,
		SupportsResponseID:             true,
		SupportsCachedPrefix:           false,
		SupportsPromptCacheUsage:       true,
		SupportsPromptCacheKey:         true,
		SupportsPromptCacheRetention:   true,
		SupportsPreviousResponseIDHTTP: true,
		BillsPreviousResponseIDContext: true,
	},
	CodexChatGPT: {
		TrustClass:                          TrustClassUpstreamProvider,
		SupportsSeed:                        false,
		SupportsTemperatureZero:             true,
		SupportsLogprobs:                    false,
		SupportsMinCompletionTokens:         false,
		SupportsStopConditions:              false,
		SupportsResponseID:                  true,
		SupportsCachedPrefix:                false,
		SupportsPromptCacheUsage:            true,
		SupportsPreviousResponseIDHTTP:      true,
		SupportsPreviousResponseIDWebSocket: false,
		BillsPreviousResponseIDContext:      true,
	},
	MiniMax: {
		TrustClass:                  TrustClassExternalThirdParty,
		SupportsSeed:                true,
		SupportsTemperatureZero:     true,
		SupportsLogprobs:            false,
		SupportsMinCompletionTokens: false,
		SupportsStopConditions:      false,
		SupportsResponseID:          false,
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

// EffectiveTrustClass returns the trust class for a provider, allowing
// a config-level override. If override is non-empty and a recognised
// value, it takes precedence over the registry default. T121.
func EffectiveTrustClass(p Provider, override string) string {
	if override == TrustClassUpstreamProvider || override == TrustClassExternalThirdParty {
		return override
	}
	caps := CapabilitiesFor(p)
	if caps.TrustClass == "" {
		return TrustClassUnknown
	}
	return caps.TrustClass
}

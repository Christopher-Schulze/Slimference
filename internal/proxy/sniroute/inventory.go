package sniroute

import (
	"strings"
)

// CodexEndpoint describes one known Codex / OpenAI / Anthropic
// endpoint we have observed in the wild. The inventory is used by
// T194's runtime safety guard: a Decision lookup that disagrees with
// the inventory's expected decision triggers a structured warning so
// operators see regressions before users do.
type CodexEndpoint struct {
	Host             string
	PathPrefix       string // matched with HasPrefix
	ExactPath        bool   // when true, PathPrefix is exact-match
	Purpose          string
	ExpectedDecision Decision
	IntroducedIn     string // Codex version / date label
}

// CodexEndpointInventory is the canonical T194 endpoint list. Order
// is significant: more-specific entries come first so the matcher
// returns the right purpose for nested paths (e.g. `/responses/
// compact` before `/responses`).
var CodexEndpointInventory = []CodexEndpoint{
	// chatgpt.com - the bulk of Codex Desktop App + CLI traffic.
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/responses/compact",
		Purpose: "Codex own conversation-compaction sideband", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex 0.130",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/responses",
		ExactPath:        true,
		Purpose:          "Codex conversation (POST or WSS subprotocol responses_websockets)",
		ExpectedDecision: MITMConversation, IntroducedIn: "codex 0.117",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/realtime/",
		Purpose: "Codex Desktop voice / realtime call setup + mgmt", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex desktop 2026.03",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/images/",
		Purpose: "Image generation (gpt-image-1.5) + image edits", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex desktop 2026.04",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/memories/",
		Purpose: "Memory subsystem (trace summarize, list, ...)", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex 0.124",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/plugins",
		Purpose: "Plugin manifest, install, detail", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex desktop 2026.04",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/models",
		ExactPath: true,
		Purpose:   "Model listing (GET)", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex 0.110",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/skills/",
		Purpose: "Skills loader", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex 0.125",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/analytics-events/",
		Purpose: "Telemetry / analytics events", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex 0.115",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/codex/audio/",
		Purpose: "Codex audio sideband", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex 0.128",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/wham/",
		Purpose: "Remote-control server", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "codex 0.128",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/backend-api/",
		Purpose: "Other chatgpt backend-api (auth, web app, feedback)", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "n/a",
	},
	{
		Host: "chatgpt.com", PathPrefix: "/api/",
		Purpose: "Browser web UI APIs", ExpectedDecision: PassthroughTLS,
		IntroducedIn: "n/a",
	},

	// api.openai.com - API-key / Codex CLI fallback / arbitrary OpenAI consumers.
	{
		Host: "api.openai.com", PathPrefix: "/v1/chat/completions",
		ExactPath: true,
		Purpose:   "Chat Completions API", ExpectedDecision: MITMConversation,
		IntroducedIn: "n/a",
	},
	{
		Host: "api.openai.com", PathPrefix: "/v1/responses",
		ExactPath: true,
		Purpose:   "Responses API (Codex CLI API-key flow)", ExpectedDecision: MITMConversation,
		IntroducedIn: "n/a",
	},
	{
		Host: "api.openai.com", PathPrefix: "/v1/audio/",
		Purpose: "Whisper / TTS / translations", ExpectedDecision: PassthroughTLS,
	},
	{
		Host: "api.openai.com", PathPrefix: "/v1/images/",
		Purpose: "Image generation / edits / variations", ExpectedDecision: PassthroughTLS,
	},
	{
		Host: "api.openai.com", PathPrefix: "/v1/embeddings",
		ExactPath: true, Purpose: "Embeddings", ExpectedDecision: PassthroughTLS,
	},
	{
		Host: "api.openai.com", PathPrefix: "/v1/files",
		Purpose: "File upload / list / delete", ExpectedDecision: PassthroughTLS,
	},
	{
		Host: "api.openai.com", PathPrefix: "/v1/models",
		Purpose: "Models listing / retrieve", ExpectedDecision: PassthroughTLS,
	},
	{
		Host: "api.openai.com", PathPrefix: "/v1/threads",
		Purpose: "Assistants API (deprecated)", ExpectedDecision: PassthroughTLS,
	},
	{
		Host: "api.openai.com", PathPrefix: "/v1/assistants",
		Purpose: "Assistants API (deprecated)", ExpectedDecision: PassthroughTLS,
	},

	// api.anthropic.com - Claude Code path. Claude support remains in
	// the codebase, but the product routing mode is Codex-only; known
	// Anthropic endpoints are documented here as passthrough so route
	// drift warnings stay honest.
	{
		Host: "api.anthropic.com", PathPrefix: "/v1/messages/batches",
		Purpose: "Batch Messages API", ExpectedDecision: PassthroughTLS,
	},
	{
		Host: "api.anthropic.com", PathPrefix: "/v1/messages/count_tokens",
		ExactPath: true, Purpose: "Token counting probe", ExpectedDecision: PassthroughTLS,
	},
	{
		Host: "api.anthropic.com", PathPrefix: "/v1/messages",
		ExactPath:        true,
		Purpose:          "Anthropic conversation (Claude Code parked)",
		ExpectedDecision: PassthroughTLS,
	},
	{
		Host: "api.anthropic.com", PathPrefix: "/v1/complete",
		ExactPath: true, Purpose: "Legacy completions", ExpectedDecision: PassthroughTLS,
	},
}

// LookupEndpoint returns the inventory entry matching `host` and
// `path`, plus whether one was found. The first matching entry by
// inventory order wins (more-specific entries are listed first).
func LookupEndpoint(host, path string) (CodexEndpoint, bool) {
	if host == "" || path == "" {
		return CodexEndpoint{}, false
	}
	cleaned := normalisePath(path)
	for _, ep := range CodexEndpointInventory {
		if !strings.EqualFold(ep.Host, host) {
			continue
		}
		if ep.ExactPath {
			if cleaned == ep.PathPrefix {
				return ep, true
			}
			continue
		}
		// Prefix-match logic. A PathPrefix that already ends with "/"
		// is itself a directory pattern; we accept any path equal to
		// or under it. Without the trailing slash we accept exact
		// match or paths that have the prefix followed by "/".
		prefix := ep.PathPrefix
		if strings.HasSuffix(prefix, "/") {
			if strings.HasPrefix(cleaned, prefix) ||
				cleaned == strings.TrimSuffix(prefix, "/") {
				return ep, true
			}
			continue
		}
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return ep, true
		}
	}
	return CodexEndpoint{}, false
}

// VerifyDecision compares the runtime Decision with the inventory's
// ExpectedDecision and returns (true, "") when they agree. On
// disagreement returns (false, reason) so the caller can log or
// auto-downgrade.
//
// When the endpoint isn't in the inventory we return (true, "") - we
// only flag KNOWN endpoints; unknown ones rely on the routing-table
// default (which is passthrough for unknown SNI / path).
func VerifyDecision(host, path string, decision Decision) (bool, string) {
	ep, ok := LookupEndpoint(host, path)
	if !ok {
		return true, ""
	}
	if ep.ExpectedDecision == decision {
		return true, ""
	}
	return false, "expected " + string(ep.ExpectedDecision) +
		" for " + host + ep.PathPrefix + " (" + ep.Purpose + "), got " + string(decision)
}

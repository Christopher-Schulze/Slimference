// Package sniroute implements the T189 routing decision engine: for
// every incoming TLS connection the transparent listener consults a
// declarative table to decide whether to (a) MITM the request and run
// it through the Phase F compression pipeline, (b) transparently
// passthrough at TLS level (decrypt + re-encrypt to upstream without
// inspection), or (c) reject.
//
// The table is matched on:
//
//   - SNI host (extracted from ClientHello before any decryption)
//   - URL path (post-handshake, after we have the first HTTP request)
//   - HTTP method
//   - WebSocket subprotocol (optional)
//   - User-Agent (resolved through internal/control/apps to a known
//     AppID; gates per-app toggle)
//
// The router is provider-agnostic at this layer - it does not know
// about Anthropic vs OpenAI internals. Per-host upstream resolution is
// the consumer's responsibility (DoH lookup of the real chatgpt.com
// IP, then re-encrypt to it).
package sniroute

import (
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/control/apps"
)

// Decision is what the router emits per request.
type Decision string

const (
	// MITMConversation: run the request through the Phase F pipeline
	// (stop-sequence injection, streamcut delay-buffer, repdet
	// rewrite, stale-read aging, obsolete-prune, etc.). The caller
	// has the request body in plaintext and may mutate it.
	MITMConversation Decision = "mitm_conversation"
	// PassthroughTLS: terminate the client's TLS handshake using
	// our CA-signed leaf (mandatory because /etc/hosts redirected
	// to us), then bridge bytes to a freshly-opened TLS connection
	// to the real upstream. Do not inspect or modify.
	PassthroughTLS Decision = "passthrough_tls"
	// Reject: refuse the request with 403. Currently unused; reserved
	// for future allow/deny policy.
	Reject Decision = "reject"
)

// Request captures the post-handshake information the router needs to
// decide. SNI is the only field that may be empty (rare but legal).
type Request struct {
	SNI            string
	Path           string
	Method         string
	UserAgent      string
	Subprotocol    string
	IsWebSocket    bool
	IsLLMHostMatch bool // populated by the caller after SNI early-exit
}

// Resolver computes Decisions from Requests using the policy below.
// Construct via New; lookups are lock-free.
type Resolver struct {
	policy *apps.Manager
}

// New returns a Resolver that consults `policy` to gate
// MITMConversation decisions on the per-app toggle.
func New(policy *apps.Manager) *Resolver {
	return &Resolver{policy: policy}
}

// Resolve evaluates the routing table for req. It is safe for
// concurrent use.
func (r *Resolver) Resolve(req Request) Decision {
	sni := strings.ToLower(req.SNI)
	path := normalisePath(req.Path)

	// Anchor the host first. Unknown hosts always passthrough so the
	// transparent listener never accidentally decrypts non-LLM
	// traffic we serve from /etc/hosts redirects (rare, but defensive).
	switch sni {
	case "chatgpt.com":
		return r.decideChatGPT(req, path)
	case "api.openai.com":
		return r.decideOpenAI(req, path)
	case "api.anthropic.com":
		return r.decideAnthropic(req, path)
	}
	return PassthroughTLS
}

// AppFor returns the AppID identified by the request's User-Agent,
// or "" when no app prefix matches. Public so callers can attribute
// telemetry without re-running detection.
func (r *Resolver) AppFor(ua string) (apps.AppID, bool) {
	if r.policy == nil {
		return "", false
	}
	return r.policy.AppFromUserAgent(ua)
}

func (r *Resolver) decideChatGPT(req Request, path string) Decision {
	// Conversation: POST /backend-api/codex/responses or WSS upgrade
	// on the same path. MITM only when policy says so for the
	// detected app.
	if path == "/backend-api/codex/responses" {
		if !r.allowConversationFor(req.UserAgent) {
			return PassthroughTLS
		}
		if req.IsWebSocket {
			if req.Subprotocol != "" &&
				!strings.HasPrefix(req.Subprotocol, "responses_websockets") {
				// Unknown WS subprotocol on this path: don't MITM,
				// the wire shape may differ.
				return PassthroughTLS
			}
			return MITMConversation
		}
		if req.Method == "" || req.Method == "POST" {
			return MITMConversation
		}
		// GET or other methods on /responses are not conversation
		// requests (probably session probes).
		return PassthroughTLS
	}
	// Every other /backend-api/codex/* path is a Codex Desktop
	// sideband (T194): realtime/voice, images, plugins, memories,
	// models, analytics, etc. Passthrough.
	if strings.HasPrefix(path, "/backend-api/codex/") {
		return PassthroughTLS
	}
	// Non-codex backend-api (web UI, auth, wham/remote-control,
	// feedback, ...) - passthrough.
	if strings.HasPrefix(path, "/backend-api/") {
		return PassthroughTLS
	}
	// chatgpt.com web app root + any other path - passthrough.
	return PassthroughTLS
}

func (r *Resolver) decideOpenAI(req Request, path string) Decision {
	if !r.allowConversationFor(req.UserAgent) {
		return PassthroughTLS
	}
	switch path {
	case "/v1/chat/completions", "/v1/responses":
		if req.Method == "" || req.Method == "POST" {
			return MITMConversation
		}
	}
	return PassthroughTLS
}

func (r *Resolver) decideAnthropic(req Request, path string) Decision {
	// Claude Code is parked while the product path is Codex-only.
	// Keep the host recognised for defensive routing and future code
	// reuse, but never MITM Anthropic traffic in the active build.
	return PassthroughTLS
}

// allowConversationFor consults the per-app policy. Unknown UAs
// passthrough by default (safer; we never MITM an app the operator
// hasn't opted in for).
func (r *Resolver) allowConversationFor(ua string) bool {
	if r.policy == nil {
		// No policy bound: allow MITM. This is the in-process
		// test-mode where the caller wants to exercise routes
		// without standing up a policy manager.
		return true
	}
	id, ok := r.policy.AppFromUserAgent(ua)
	if !ok {
		return false
	}
	return r.policy.Policy().IsEnabled(id)
}

// normalisePath strips a trailing slash and lowercases nothing
// (paths are case-sensitive per RFC 3986). A double-slash collapse
// keeps `/a//b` matching `/a/b` so silly client mistakes still hit
// the right rule.
func normalisePath(p string) string {
	if p == "" {
		return "/"
	}
	// Collapse runs of slashes first so trailing collapse cleanups
	// don't leave a single dangling slash.
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	// Strip trailing slash except for the root path.
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
}

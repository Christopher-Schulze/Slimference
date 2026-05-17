package sniroute

import (
	"strings"
	"testing"
)

func TestInventoryConsistencyWithRouter(t *testing.T) {
	// Every inventory entry's ExpectedDecision must match what the
	// router would actually produce. This guards against drift
	// between the static inventory and the live routing table.
	r := New(nil) // nil policy → don't gate on app toggles
	for _, ep := range CodexEndpointInventory {
		path := ep.PathPrefix
		if !ep.ExactPath {
			path = strings.TrimSuffix(ep.PathPrefix, "/") + "/sample"
		}
		req := Request{
			SNI:       ep.Host,
			Path:      path,
			Method:    "POST",
			UserAgent: "codex_cli_rs/0.130.0",
		}
		got := r.Resolve(req)
		if got != ep.ExpectedDecision {
			t.Errorf("inventory drift: %s %s expected %s, router says %s",
				ep.Host, ep.PathPrefix, ep.ExpectedDecision, got)
		}
	}
}

func TestLookupEndpointExactMatch(t *testing.T) {
	ep, ok := LookupEndpoint("chatgpt.com", "/backend-api/codex/responses")
	if !ok {
		t.Fatalf("conversation endpoint not in inventory")
	}
	if ep.ExpectedDecision != MITMConversation {
		t.Errorf("got decision %s", ep.ExpectedDecision)
	}
}

func TestLookupEndpointPrefixMatch(t *testing.T) {
	ep, ok := LookupEndpoint("chatgpt.com", "/backend-api/codex/realtime/calls/abc-123")
	if !ok {
		t.Fatalf("realtime call not matched by prefix entry")
	}
	if ep.ExpectedDecision != PassthroughTLS {
		t.Errorf("realtime should passthrough, got %s", ep.ExpectedDecision)
	}
}

func TestLookupEndpointHostCaseInsensitive(t *testing.T) {
	ep, ok := LookupEndpoint("ChatGPT.com", "/backend-api/codex/models")
	if !ok {
		t.Fatalf("case-insensitive match failed")
	}
	if ep.ExpectedDecision != PassthroughTLS {
		t.Errorf("models endpoint should passthrough, got %s", ep.ExpectedDecision)
	}
}

func TestLookupEndpointCompactBeforeResponses(t *testing.T) {
	// /backend-api/codex/responses/compact must match the compact
	// entry (passthrough) not the responses entry (MITM).
	ep, ok := LookupEndpoint("chatgpt.com", "/backend-api/codex/responses/compact")
	if !ok {
		t.Fatalf("compact endpoint missing from inventory")
	}
	if ep.ExpectedDecision != PassthroughTLS {
		t.Errorf("compact should passthrough, got %s", ep.ExpectedDecision)
	}
}

func TestLookupEndpointAnthropicMessages(t *testing.T) {
	ep, ok := LookupEndpoint("api.anthropic.com", "/v1/messages")
	if !ok {
		t.Fatalf("not in inventory")
	}
	if ep.ExpectedDecision != PassthroughTLS {
		t.Errorf("got %s", ep.ExpectedDecision)
	}
	if !strings.Contains(ep.Purpose, "parked") {
		t.Errorf("purpose should document parked Claude mode: %q", ep.Purpose)
	}
}

func TestLookupEndpointAnthropicBatches(t *testing.T) {
	ep, ok := LookupEndpoint("api.anthropic.com", "/v1/messages/batches")
	if !ok {
		t.Fatalf("missing")
	}
	if ep.ExpectedDecision != PassthroughTLS {
		t.Errorf("batches must passthrough, got %s", ep.ExpectedDecision)
	}
}

func TestLookupEndpointUnknownHost(t *testing.T) {
	if _, ok := LookupEndpoint("example.com", "/foo"); ok {
		t.Errorf("unknown host should not match inventory")
	}
}

func TestLookupEndpointEmptyHost(t *testing.T) {
	if _, ok := LookupEndpoint("", "/foo"); ok {
		t.Errorf("empty host should not match")
	}
}

func TestLookupEndpointEmptyPath(t *testing.T) {
	if _, ok := LookupEndpoint("chatgpt.com", ""); ok {
		t.Errorf("empty path should not match")
	}
}

func TestLookupEndpointWebUIPath(t *testing.T) {
	ep, ok := LookupEndpoint("chatgpt.com", "/api/auth/session")
	if !ok {
		t.Fatalf("/api/* not in inventory")
	}
	if ep.ExpectedDecision != PassthroughTLS {
		t.Errorf("web UI must passthrough, got %s", ep.ExpectedDecision)
	}
}

func TestLookupEndpointBackendApiCatchAll(t *testing.T) {
	// /backend-api/auth/session must hit the generic /backend-api/
	// catch-all, not the codex-specific ones.
	ep, ok := LookupEndpoint("chatgpt.com", "/backend-api/auth/session")
	if !ok {
		t.Fatalf("backend-api catch-all missing")
	}
	if ep.ExpectedDecision != PassthroughTLS {
		t.Errorf("got %s", ep.ExpectedDecision)
	}
	if !strings.Contains(ep.Purpose, "backend-api") {
		t.Errorf("matched wrong entry: %+v", ep)
	}
}

func TestLookupEndpointConversationExactNotPrefix(t *testing.T) {
	// `/backend-api/codex/responses/extra` should NOT match the
	// MITM-only responses entry (which is exact-path). It should
	// fall through to the generic /backend-api/codex/* catchall
	// path. Without a catchall just for /codex/*, it falls through
	// to /backend-api/* (which expects passthrough).
	ep, ok := LookupEndpoint("chatgpt.com", "/backend-api/codex/responses/extra-suffix")
	if !ok {
		t.Fatalf("expected fallback match")
	}
	if ep.ExpectedDecision != PassthroughTLS {
		t.Errorf("fallback should passthrough, got %s", ep.ExpectedDecision)
	}
}

func TestVerifyDecisionAgreement(t *testing.T) {
	ok, reason := VerifyDecision("chatgpt.com",
		"/backend-api/codex/responses", MITMConversation)
	if !ok || reason != "" {
		t.Errorf("expected agreement, got ok=%v reason=%q", ok, reason)
	}
}

func TestVerifyDecisionDisagreement(t *testing.T) {
	ok, reason := VerifyDecision("chatgpt.com",
		"/backend-api/codex/responses", PassthroughTLS)
	if ok {
		t.Errorf("expected disagreement")
	}
	if reason == "" {
		t.Errorf("reason should be populated")
	}
	if !strings.Contains(reason, "expected mitm_conversation") {
		t.Errorf("reason should describe mismatch: %q", reason)
	}
}

func TestVerifyDecisionUnknownEndpointPasses(t *testing.T) {
	ok, reason := VerifyDecision("example.com", "/foo", PassthroughTLS)
	if !ok || reason != "" {
		t.Errorf("unknown endpoint should not flag drift; got ok=%v reason=%q", ok, reason)
	}
}

func TestEveryInventoryEntryHasPurpose(t *testing.T) {
	for _, ep := range CodexEndpointInventory {
		if ep.Purpose == "" {
			t.Errorf("inventory entry %s%s has empty Purpose", ep.Host, ep.PathPrefix)
		}
	}
}

func TestEveryInventoryEntryHasHost(t *testing.T) {
	for _, ep := range CodexEndpointInventory {
		if ep.Host == "" {
			t.Errorf("inventory entry has empty Host: %+v", ep)
		}
	}
}

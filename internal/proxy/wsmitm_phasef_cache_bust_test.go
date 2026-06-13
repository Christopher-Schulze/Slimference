package proxy

import (
	"encoding/json"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
)

func TestWSPhaseFProviderCacheBustDemotionScopesToRequestShape(t *testing.T) {
	adapter := (&PhaseFDispatcher{Proxy: New(config.Defaults())}).newWSPhaseFAdapter()
	sessionID := "codex-wss:cache-bust-shape"
	readDelta := proxyLayer0MechanismMaskFor(proxyLayer0MechanismReadDelta)

	if event := adapter.observeWSSProviderCacheBustForShape(sessionID, 1000, 820, 0, "full_history"); event.Fired {
		t.Fatalf("first full-history sample must not fire cache-bust guard: %+v", event)
	}
	if event := adapter.observeWSSProviderCacheBustForShape(sessionID, 1000, 810, readDelta, "delta"); event.Fired {
		t.Fatalf("warm delta mutation sample must not fire before next usage frame: %+v", event)
	}
	event := adapter.observeWSSProviderCacheBustForShape(sessionID, 1000, 470, 0, "delta")
	if !event.Fired || event.Trigger != readDelta || event.TriggerRequestShape != "delta" {
		t.Fatalf("cache-bust guard should demote the previous delta mechanism and shape: %+v", event)
	}
	if got := adapter.wssCacheBustDemotedMechanismsForShape(sessionID, "delta"); got != readDelta {
		t.Fatalf("delta demoted mechanism mask=%q, want %q", got.String(), readDelta.String())
	}
	if got := adapter.wssCacheBustDemotedMechanismsForShape(sessionID, "full_history"); got != 0 {
		t.Fatalf("full-history must not inherit delta cache-bust demotion, got %q", got.String())
	}
	if got := adapter.wssCacheBustDemotedMechanisms(sessionID); got != readDelta {
		t.Fatalf("aggregate telemetry mask=%q, want %q", got.String(), readDelta.String())
	}
}

func TestWSPhaseFProviderCacheBustDemotionScopesToPromptCacheKey(t *testing.T) {
	adapter := (&PhaseFDispatcher{Proxy: New(config.Defaults())}).newWSPhaseFAdapter()
	readDelta := proxyLayer0MechanismMaskFor(proxyLayer0MechanismReadDelta)
	scopeA := wssCacheBustScope("delta", "prefix-a")
	scopeB := wssCacheBustScope("delta", "prefix-b")

	crossPrefixSession := "codex-wss:cache-bust-prefix-cross"
	if event := adapter.observeWSSProviderCacheBustForScope(crossPrefixSession, 1000, 820, 0, "delta", scopeA); event.Fired {
		t.Fatalf("first prefix sample must not fire: %+v", event)
	}
	if event := adapter.observeWSSProviderCacheBustForScope(crossPrefixSession, 1000, 810, readDelta, "delta", scopeA); event.Fired {
		t.Fatalf("mutated prefix sample must wait for comparable usage: %+v", event)
	}
	if event := adapter.observeWSSProviderCacheBustForScope(crossPrefixSession, 1000, 470, 0, "delta", scopeB); event.Fired {
		t.Fatalf("cache-share drop on a different prompt-cache key must not demote prefix A: %+v", event)
	}
	if got := adapter.wssCacheBustDemotedMechanismsForScope(crossPrefixSession, "delta", "prefix-a"); got != 0 {
		t.Fatalf("prefix A demoted from prefix B usage drop: %q", got.String())
	}

	samePrefixSession := "codex-wss:cache-bust-prefix-same"
	adapter.observeWSSProviderCacheBustForScope(samePrefixSession, 1000, 820, 0, "delta", scopeA)
	adapter.observeWSSProviderCacheBustForScope(samePrefixSession, 1000, 810, readDelta, "delta", scopeA)
	event := adapter.observeWSSProviderCacheBustForScope(samePrefixSession, 1000, 470, 0, "delta", scopeA)
	if !event.Fired || event.Trigger != readDelta || event.TriggerScope != scopeA {
		t.Fatalf("matching prompt-cache key should demote exact previous scope: %+v", event)
	}
	if got := adapter.wssCacheBustDemotedMechanismsForScope(samePrefixSession, "delta", "prefix-a"); got != readDelta {
		t.Fatalf("prefix A demoted mechanism mask=%q, want %q", got.String(), readDelta.String())
	}
	if got := adapter.wssCacheBustDemotedMechanismsForScope(samePrefixSession, "delta", "prefix-b"); got != 0 {
		t.Fatalf("prefix B must not inherit prefix A demotion, got %q", got.String())
	}
	if got := adapter.wssCacheBustDemotedMechanisms(samePrefixSession); got != readDelta {
		t.Fatalf("aggregate telemetry mask=%q, want %q", got.String(), readDelta.String())
	}
}

func TestWSPhaseFProviderCacheBustLegacyAggregateDemotionStillApplies(t *testing.T) {
	adapter := (&PhaseFDispatcher{Proxy: New(config.Defaults())}).newWSPhaseFAdapter()
	sessionID := "codex-wss:cache-bust-legacy"
	captured := proxyLayer0MechanismMaskFor(proxyLayer0MechanismCapturedOut)

	adapter.mu.Lock()
	adapter.cacheBustSessions = map[string]*wssProviderCacheBustSession{
		sessionID: {demoted: captured},
	}
	adapter.mu.Unlock()

	if got := adapter.wssCacheBustDemotedMechanismsForShape(sessionID, "full_history"); got != captured {
		t.Fatalf("legacy aggregate demotion should still guard when no shape map exists: got %q want %q", got.String(), captured.String())
	}
}

func TestWSSPromptCacheKeyHashFromRawIsStableAndContentFree(t *testing.T) {
	raw := map[string]json.RawMessage{
		"prompt_cache_key": json.RawMessage(`"raw-prefix-secret"`),
	}
	first := wssPromptCacheKeyHashFromRaw(raw)
	second := wssPromptCacheKeyHashFromRaw(raw)
	if first == "" || first != second || first == "raw-prefix-secret" || len(first) != 16 {
		t.Fatalf("prompt-cache key hash not stable/content-free: first=%q second=%q", first, second)
	}
	if got := wssPromptCacheKeyHashFromRaw(map[string]json.RawMessage{}); got != "" {
		t.Fatalf("empty raw prompt-cache hash=%q, want empty", got)
	}
}

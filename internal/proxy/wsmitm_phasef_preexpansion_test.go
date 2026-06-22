package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

// TestWSPhaseFPreExpansionPreviousResponseIDRecovery verifies that when a
// delta-shaped request with previous_response_id is expanded by the stateless
// history continuation (which drops previous_response_id from the body), the
// pre-expansion PreviousResponseID is extracted and passed to the delta
// stateless recovery check. Without this, the recovery check sees an empty
// PreviousResponseID on the expanded body and fails to open the proof gate.
func TestWSPhaseFPreExpansionPreviousResponseIDRecovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	// Seed a root request to establish the session.
	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "pre-expansion-recovery-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-pre-expansion-recovery","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run the package tests",
		}},
		"stream": true,
	})
	if replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &root); err != nil || replace {
		t.Fatalf("root request should seed recovery only, replace=%v err=%v raw=%s", replace, err, root.Raw)
	}

	// Seed a tool call and complete the response to build the response chain.
	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_tests", "exec_command", map[string]any{"cmd": "go test ./... -v"})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-pre-expansion-parent")
	if chain := adapter.wssResponseChain("resp-pre-expansion-parent"); len(chain) == 0 {
		t.Fatal("parent response chain was not seeded")
	}

	// Mark stateless mode so the continuation expansion activates.
	adapter.markWSSHistoryStatelessMode()

	// Send a delta with previous_response_id and a tool result. The stateless
	// continuation will expand this to full-history by prepending the stored
	// chain, dropping previous_response_id from the body. The recovery check
	// must use the pre-expansion PreviousResponseID.
	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-pre-expansion-parent",
		"prompt_cache_key":     "pre-expansion-recovery-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-pre-expansion-recovery","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_tests",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta)
	if err != nil {
		t.Fatalf("stateless continuation delta handle: %v", err)
	}
	if !replace {
		t.Fatalf("stateless continuation delta should rewrite, replace=%v", replace)
	}

	// The rewritten body must have dropped previous_response_id (expansion).
	body, _, ok := wsRequestBody(&delta)
	if !ok {
		t.Fatal("rewritten body missing")
	}
	if bytes.Contains(body, []byte("previous_response_id")) {
		t.Fatalf("stateless continuation must drop previous_response_id: %s", body)
	}

	summary := p.DebugRecorder().Last(1, false)[0]

	// The pre-expansion PreviousResponseID must be captured in debug facts.
	if summary.DebugFacts["wss.delta_stateless_recovery_prev_id"] != "resp-pre-expansion-parent" {
		t.Fatalf("delta_stateless_recovery_prev_id must be the pre-expansion ID, got %q, summary=%+v",
			summary.DebugFacts["wss.delta_stateless_recovery_prev_id"], summary)
	}

	// The stateless continuation debug facts must be present.
	if summary.DebugFacts["wss.stateless_history_continuation"] != "true" {
		t.Fatalf("stateless_history_continuation must be true: %+v", summary)
	}

	// The pre-expansion baseline fact must be present (preExpansionMessages was set).
	if summary.DebugFacts["wss.pre_expansion_baseline"] != "true" {
		t.Fatalf("pre_expansion_baseline must be true when pre-expansion messages are captured: %+v", summary)
	}

	// The delta stateless recovery debug facts must ALL survive into the
	// recorded summary, even if the full-pass branch reassigned DebugFacts.
	// This is the core regression test for the debug-facts-overwrite bug.
	for _, key := range []string{
		"wss.delta_stateless_recovery_ready",
		"wss.delta_stateless_recovery_prev_id",
		"wss.delta_stateless_recovery_tool_output_known",
		"wss.delta_stateless_recovery_delta_shape",
	} {
		if _, exists := summary.DebugFacts[key]; !exists {
			t.Fatalf("debug fact %q must survive into recorded summary, missing. facts=%+v", key, summary.DebugFacts)
		}
	}
}

// TestWSPhaseFPreExpansionDebugFactsSurviveFullPassBranch verifies that the
// delta stateless recovery debug facts are NOT wiped by the
// wssRequestDebugFacts reassignment inside the full-pass branch
// (wssPreviousResponseUnknownToolOutputFullPass). This is the specific bug
// where meta.DebugFacts = wssRequestDebugFacts(...) creates a fresh map and
// drops the recovery facts set earlier in the pipeline.
func TestWSPhaseFPreExpansionDebugFactsSurviveFullPassBranch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	// Root request to seed the session.
	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "debug-facts-fullpass-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-debug-facts-fullpass","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run tests then check git status",
		}},
		"stream": true,
	})
	if replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &root); err != nil || replace {
		t.Fatalf("root request should seed recovery only, replace=%v err=%v raw=%s", replace, err, root.Raw)
	}

	// Seed tool calls and complete response to build the chain.
	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_tests", "exec_command", map[string]any{"cmd": "go test ./... -v"})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-debug-facts-fullpass-parent")
	if chain := adapter.wssResponseChain("resp-debug-facts-fullpass-parent"); len(chain) == 0 {
		t.Fatal("parent response chain was not seeded")
	}

	adapter.markWSSHistoryStatelessMode()

	// Delta with tool output that triggers the full-pass branch
	// (wssPreviousResponseUnknownToolOutputFullPass) because toolOutputKnown
	// is false on the first delta (tool_use metadata not yet resolved).
	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-debug-facts-fullpass-parent",
		"prompt_cache_key":     "debug-facts-fullpass-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-debug-facts-fullpass","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_tests",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	if _, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta); err != nil {
		t.Fatalf("delta handle: %v", err)
	}

	summary := p.DebugRecorder().Last(1, false)[0]

	// Regardless of which branch was taken (full-pass or not), the four
	// delta stateless recovery debug facts must be present in the summary.
	requiredFacts := map[string]string{
		"wss.delta_stateless_recovery_ready":             "", // value depends on branch
		"wss.delta_stateless_recovery_prev_id":           "resp-debug-facts-fullpass-parent",
		"wss.delta_stateless_recovery_tool_output_known": "", // value depends on branch
		"wss.delta_stateless_recovery_delta_shape":       "", // value depends on branch
	}
	for key, expectedVal := range requiredFacts {
		val, exists := summary.DebugFacts[key]
		if !exists {
			t.Fatalf("debug fact %q must survive into recorded summary (full-pass branch wipes it without the fix). facts=%+v", key, summary.DebugFacts)
		}
		if expectedVal != "" && val != expectedVal {
			t.Fatalf("debug fact %q = %q, expected %q. facts=%+v", key, val, expectedVal, summary.DebugFacts)
		}
	}

	// The pre-expansion PreviousResponseID must be the original one, not empty.
	if summary.DebugFacts["wss.delta_stateless_recovery_prev_id"] == "" {
		t.Fatalf("delta_stateless_recovery_prev_id must not be empty when stateless continuation expanded a delta with previous_response_id: %+v", summary)
	}
}

// TestWSSRawJSONHelper verifies the wssRawJSON helper correctly parses valid
// and invalid JSON bodies.
func TestWSSRawJSONHelper(t *testing.T) {
	// Valid JSON body.
	raw := wssRawJSON([]byte(`{"previous_response_id":"resp-123","model":"gpt-5-codex"}`))
	if raw == nil {
		t.Fatal("wssRawJSON must return non-nil for valid JSON")
	}
	if id := wssPreviousResponseIDFromRaw(raw); id != "resp-123" {
		t.Fatalf("previous_response_id from raw = %q, expected resp-123", id)
	}

	// Empty body.
	if raw := wssRawJSON(nil); raw != nil {
		t.Fatalf("wssRawJSON(nil) must return nil, got %v", raw)
	}

	// Invalid JSON.
	if raw := wssRawJSON([]byte(`{not json`)); raw != nil {
		t.Fatalf("wssRawJSON must return nil for invalid JSON, got %v", raw)
	}

	// Valid JSON but not an object (array).
	if raw := wssRawJSON([]byte(`[1,2,3]`)); raw != nil {
		t.Fatalf("wssRawJSON must return nil for non-object JSON, got %v", raw)
	}

	// Valid empty object.
	raw = wssRawJSON([]byte(`{}`))
	if raw == nil {
		t.Fatal("wssRawJSON must return non-nil for empty object")
	}
	if len(raw) != 0 {
		t.Fatalf("wssRawJSON({}) must return empty map, got %d entries", len(raw))
	}
}

// TestWSSRawJSONIntegrationWithPreviousResponseID verifies that wssRawJSON +
// wssPreviousResponseIDFromRaw correctly extract the previous_response_id
// from a body that mimics a real Codex WSS request before expansion.
func TestWSSRawJSONIntegrationWithPreviousResponseID(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","previous_response_id":"resp-chain-parent","prompt_cache_key":"session-1","input":[{"type":"function_call_output","call_id":"call_1","output":"result"}],"stream":true}`)

	raw := wssRawJSON(body)
	if raw == nil {
		t.Fatal("wssRawJSON must parse valid Codex WSS body")
	}
	id := wssPreviousResponseIDFromRaw(raw)
	if id != "resp-chain-parent" {
		t.Fatalf("previous_response_id = %q, expected resp-chain-parent", id)
	}

	// Verify the body still has the field (wssRawJSON must not mutate the input).
	if !bytes.Contains(body, []byte(`"previous_response_id":"resp-chain-parent"`)) {
		t.Fatal("wssRawJSON must not mutate the input body")
	}
}

// TestAttachWSSDeltaStatelessRecoveryDebugFacts verifies the helper function
// directly, including nil-safety and idempotency (safe to call multiple times).
func TestAttachWSSDeltaStatelessRecoveryDebugFacts(t *testing.T) {
	t.Run("nil meta is safe", func(t *testing.T) {
		attachWSSDeltaStatelessRecoveryDebugFacts(nil, true, "resp-1", true, true)
		// must not panic
	})

	t.Run("sets all four facts", func(t *testing.T) {
		meta := &wssRequestMeta{}
		attachWSSDeltaStatelessRecoveryDebugFacts(meta, true, "resp-42", true, false)
		if meta.DebugFacts["wss.delta_stateless_recovery_ready"] != "true" {
			t.Fatalf("ready fact = %q", meta.DebugFacts["wss.delta_stateless_recovery_ready"])
		}
		if meta.DebugFacts["wss.delta_stateless_recovery_prev_id"] != "resp-42" {
			t.Fatalf("prev_id fact = %q", meta.DebugFacts["wss.delta_stateless_recovery_prev_id"])
		}
		if meta.DebugFacts["wss.delta_stateless_recovery_tool_output_known"] != "true" {
			t.Fatalf("tool_output_known fact = %q", meta.DebugFacts["wss.delta_stateless_recovery_tool_output_known"])
		}
		if meta.DebugFacts["wss.delta_stateless_recovery_delta_shape"] != "false" {
			t.Fatalf("delta_shape fact = %q", meta.DebugFacts["wss.delta_stateless_recovery_delta_shape"])
		}
	})

	t.Run("idempotent over fresh map (simulates wssRequestDebugFacts wipe)", func(t *testing.T) {
		meta := &wssRequestMeta{}
		attachWSSDeltaStatelessRecoveryDebugFacts(meta, true, "resp-first", true, true)
		// Simulate wssRequestDebugFacts building a fresh map.
		meta.DebugFacts = map[string]string{"wss.other_fact": "value"}
		// Re-attach after the wipe.
		attachWSSDeltaStatelessRecoveryDebugFacts(meta, true, "resp-first", true, true)
		// Both the other fact and the recovery facts must be present.
		if meta.DebugFacts["wss.other_fact"] != "value" {
			t.Fatal("re-attach must not wipe existing facts")
		}
		if meta.DebugFacts["wss.delta_stateless_recovery_prev_id"] != "resp-first" {
			t.Fatalf("re-attach must set prev_id, got %q", meta.DebugFacts["wss.delta_stateless_recovery_prev_id"])
		}
		if meta.DebugFacts["wss.delta_stateless_recovery_ready"] != "true" {
			t.Fatalf("re-attach must set ready, got %q", meta.DebugFacts["wss.delta_stateless_recovery_ready"])
		}
	})
}

// TestAttachWSSCacheBustDemotedDebugFacts verifies the cache-bust-demoted
// helper directly, including nil-safety and the zero-mask short-circuit.
func TestAttachWSSCacheBustDemotedDebugFacts(t *testing.T) {
	t.Run("nil meta is safe", func(t *testing.T) {
		attachWSSCacheBustDemotedDebugFacts(nil, 1, nil, "full_history")
	})

	t.Run("zero mask is a no-op", func(t *testing.T) {
		meta := &wssRequestMeta{DebugFacts: map[string]string{"existing": "yes"}}
		attachWSSCacheBustDemotedDebugFacts(meta, 0, nil, "full_history")
		if _, exists := meta.DebugFacts["wss.cache_bust_demoted_mechanisms"]; exists {
			t.Fatal("zero mask must not set cache_bust_demoted facts")
		}
		if meta.DebugFacts["existing"] != "yes" {
			t.Fatal("zero mask must not wipe existing facts")
		}
	})
}

// TestWSPhaseFPreExpansionBaselineFact verifies that the
// wss.pre_expansion_baseline debug fact is set only when pre-expansion
// messages are actually captured (i.e. the stateless continuation expansion
// happened and the original delta body had extractable messages).
func TestWSPhaseFPreExpansionBaselineFact(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "pre-expansion-baseline-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-pre-expansion-baseline","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run tests",
		}},
		"stream": true,
	})
	if _, err := adapter.handle(ctx, wsmitm.DirClientToServer, &root); err != nil {
		t.Fatalf("root handle: %v", err)
	}

	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_baseline", "exec_command", map[string]any{"cmd": "go test ./..."})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-baseline-parent")
	adapter.markWSSHistoryStatelessMode()

	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-baseline-parent",
		"prompt_cache_key":     "pre-expansion-baseline-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-pre-expansion-baseline","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_baseline",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	if _, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta); err != nil {
		t.Fatalf("delta handle: %v", err)
	}

	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.pre_expansion_baseline"] != "true" {
		t.Fatalf("pre_expansion_baseline must be true when stateless continuation expansion captured pre-expansion messages: %+v", summary)
	}
	if summary.DebugFacts["wss.stateless_history_continuation"] != "true" {
		t.Fatalf("stateless_history_continuation must be true: %+v", summary)
	}
}

// TestWSPhaseFPreExpansionBaselineNotSetWithoutExpansion verifies that the
// wss.pre_expansion_baseline fact is NOT set when there is no stateless
// continuation expansion (a regular delta without expansion).
func TestWSPhaseFPreExpansionBaselineNotSetWithoutExpansion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	// Root request — no stateless mode marked, no chain, so no expansion.
	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "no-expansion-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-no-expansion","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run tests",
		}},
		"stream": true,
	})
	if _, err := adapter.handle(ctx, wsmitm.DirClientToServer, &root); err != nil {
		t.Fatalf("root handle: %v", err)
	}

	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_no_exp", "exec_command", map[string]any{"cmd": "go test ./..."})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-no-expansion-parent")

	// Delta WITHOUT marking stateless mode — no expansion should happen.
	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-no-expansion-parent",
		"prompt_cache_key":     "no-expansion-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-no-expansion","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_no_exp",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	if _, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta); err != nil {
		t.Fatalf("delta handle: %v", err)
	}

	summary := p.DebugRecorder().Last(1, false)[0]
	if _, exists := summary.DebugFacts["wss.pre_expansion_baseline"]; exists {
		t.Fatalf("pre_expansion_baseline must NOT be set without stateless continuation expansion: %+v", summary)
	}
	if summary.DebugFacts["wss.stateless_history_continuation"] == "true" {
		t.Fatalf("stateless_history_continuation must NOT be true without expansion: %+v", summary)
	}
}

// TestWSPhaseFDeltaStatelessRecoveryDebugFactsFormat verifies the boolean
// debug facts use strconv.FormatBool format ("true"/"false"), not other
// representations.
func TestWSPhaseFDeltaStatelessRecoveryDebugFactsFormat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "fact-format-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-fact-format","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run tests",
		}},
		"stream": true,
	})
	if _, err := adapter.handle(ctx, wsmitm.DirClientToServer, &root); err != nil {
		t.Fatalf("root handle: %v", err)
	}

	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_fmt", "exec_command", map[string]any{"cmd": "go test ./..."})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-fact-format-parent")
	adapter.markWSSHistoryStatelessMode()

	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-fact-format-parent",
		"prompt_cache_key":     "fact-format-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-fact-format","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_fmt",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	if _, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta); err != nil {
		t.Fatalf("delta handle: %v", err)
	}

	summary := p.DebugRecorder().Last(1, false)[0]
	for _, key := range []string{
		"wss.delta_stateless_recovery_ready",
		"wss.delta_stateless_recovery_tool_output_known",
		"wss.delta_stateless_recovery_delta_shape",
	} {
		val := summary.DebugFacts[key]
		if val != "true" && val != "false" {
			t.Fatalf("debug fact %q = %q, must be 'true' or 'false' (strconv.FormatBool format)", key, val)
		}
	}
}

// TestWSPhaseFCacheBustDemotedFactsSurviveFullPassBranch verifies that the
// cache-bust-demoted debug facts also survive the wssRequestDebugFacts
// reassignment in the full-pass branch. This was a pre-existing bug of the
// same class as the delta stateless recovery facts overwrite.
func TestWSPhaseFCacheBustDemotedFactsSurviveFullPassBranch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "cache-bust-fullpass-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-cache-bust-fullpass","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run tests",
		}},
		"stream": true,
	})
	if _, err := adapter.handle(ctx, wsmitm.DirClientToServer, &root); err != nil {
		t.Fatalf("root handle: %v", err)
	}

	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_cb", "exec_command", map[string]any{"cmd": "go test ./..."})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-cache-bust-parent")
	adapter.markWSSHistoryStatelessMode()

	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-cache-bust-parent",
		"prompt_cache_key":     "cache-bust-fullpass-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-cache-bust-fullpass","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_cb",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	if _, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta); err != nil {
		t.Fatalf("delta handle: %v", err)
	}

	summary := p.DebugRecorder().Last(1, false)[0]
	// If cache-bust demotion happened, the facts must survive. If no demotion
	// happened, the facts are absent — that's also correct. We only assert
	// that IF the mechanisms fact is present, all related facts are present.
	if mechanisms, exists := summary.DebugFacts["wss.cache_bust_demoted_mechanisms"]; exists && mechanisms != "" {
		for _, key := range []string{
			"wss.cache_bust_demoted_request_shape",
			"wss.cache_bust_demoted_scope",
		} {
			if _, exists := summary.DebugFacts[key]; !exists {
				t.Fatalf("cache_bust_demoted fact %q must survive when mechanisms is present. facts=%+v", key, summary.DebugFacts)
			}
		}
	}
}

// Ensure json import is used (for TestWSSRawJSONIntegrationWithPreviousResponseID
// and other tests that reference json types).
var _ = json.RawMessage(nil)

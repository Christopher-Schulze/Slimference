package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// TestL3SafeSubset_DeltaCompactionPreservesArchiveRecovery proves that when
// the L3 safe subset compacts a delta turn, the archive recovery marker is
// present in the compacted output, allowing the model to recover the full
// output if needed. This is the core drawdown-policy safety guarantee:
// no context loss because archive recovery is always available.
func TestL3SafeSubset_DeltaCompactionPreservesArchiveRecovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Proxy.ServerStateEnabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	// Seed a parent response chain
	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "l3-safety-archive-recovery",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-archive","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run tests",
		}},
		"stream": true,
	})
	if replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &root); err != nil || replace {
		t.Fatalf("root request should seed recovery only, replace=%v err=%v", replace, err)
	}

	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_l3_arch", "exec_command", map[string]any{"cmd": "go test ./... -v"})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-l3-parent")

	if chain := adapter.wssResponseChain("resp-l3-parent"); len(chain) == 0 {
		t.Fatal("parent response chain was not seeded")
	}

	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-l3-parent",
		"prompt_cache_key":     "l3-safety-archive-recovery",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-archive","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_l3_arch",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	original := append([]byte(nil), delta.Raw...)
	replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta)
	if err != nil {
		t.Fatalf("delta handle: %v", err)
	}
	if !replace {
		t.Fatalf("delta should be compacted (replace=true)")
	}
	if bytes.Equal(delta.Raw, original) {
		t.Fatalf("delta should be modified by compaction")
	}

	mutated := string(delta.Raw)
	// Archive recovery marker must be present — this is the fail-open recovery path
	if !strings.Contains(mutated, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("compacted output missing archive recovery marker: %s", mutated[:min(300, len(mutated))])
	}
	// Failure detail must be preserved (drawdown policy: model access to failure facts)
	if !strings.Contains(mutated, "SLIMFERENCE_TEST_FAILURE_SENTINEL") {
		t.Fatalf("compacted output lost failure detail (context loss): %s", mutated[:min(300, len(mutated))])
	}
	// Passing test noise should be compacted away
	if strings.Contains(mutated, "TestPassing089") {
		t.Fatalf("compacted output still contains passing test noise (no savings)")
	}
}

// TestL3SafeSubset_StoredHistoryStaysByteEqual proves that the L3 safe subset
// never mutates stored history. Only the wire bytes (the new turn being sent)
// are compacted. The response chain stored in the adapter stays byte-equal
// to what was originally received from the server.
func TestL3SafeSubset_StoredHistoryStaysByteEqual(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Proxy.ServerStateEnabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "l3-safety-byte-equal",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-byte-equal","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run tests",
		}},
		"stream": true,
	})
	adapter.handle(ctx, wsmitm.DirClientToServer, &root)

	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_byte_eq", "exec_command", map[string]any{"cmd": "go test ./... -v"})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-byte-equal-parent")

	// Snapshot the stored chain BEFORE delta compaction
	chainBefore := adapter.wssResponseChain("resp-byte-equal-parent")
	if len(chainBefore) == 0 {
		t.Fatal("parent chain not seeded")
	}
	chainBeforeCopy := make(wssResponseChain, len(chainBefore))
	copy(chainBeforeCopy, chainBefore)

	// Now send a delta that triggers compaction
	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-byte-equal-parent",
		"prompt_cache_key":     "l3-safety-byte-equal",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-byte-equal","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_byte_eq",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	_, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta)
	if err != nil {
		t.Fatalf("delta handle: %v", err)
	}

	// Verify stored chain is byte-equal to before compaction
	chainAfter := adapter.wssResponseChain("resp-byte-equal-parent")
	if len(chainAfter) != len(chainBeforeCopy) {
		t.Fatalf("stored chain length changed: before=%d after=%d (stored history was mutated)", len(chainBeforeCopy), len(chainAfter))
	}
	for i, item := range chainBeforeCopy {
		if !bytes.Equal(item, chainAfter[i]) {
			t.Fatalf("stored chain item %d was mutated (not byte-equal): before=%s after=%s", i, item, chainAfter[i])
		}
	}
}

// TestL3SafeSubset_FailOpenOnUnknownToolOutput proves that when tool output
// metadata is missing AND inference cannot resolve the command, the L3 safe
// subset fails open: full output is sent, no compaction, no savings.
// This is the critical drawdown-policy guarantee: no compaction on uncertainty.
func TestL3SafeSubset_FailOpenOnUnknownToolOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Proxy.ServerStateEnabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "l3-safety-fail-open",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-fail-open","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run something",
		}},
		"stream": true,
	})
	adapter.handle(ctx, wsmitm.DirClientToServer, &root)

	// Seed a tool call with an UNKNOWN command (not a recognized pattern)
	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_unknown", "exec_command", map[string]any{"cmd": "some-unknown-binary --flag xyz"})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-fail-open-parent")

	// Delta with output that doesn't match any known inference pattern
	unknownOutput := "Process exited with code 0\nOutput:\nline1\nline2\nline3\n"
	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-fail-open-parent",
		"prompt_cache_key":     "l3-safety-fail-open",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-fail-open","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_unknown",
			"output":  unknownOutput,
		}},
		"stream": true,
	})
	original := append([]byte(nil), delta.Raw...)
	replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta)
	if err != nil {
		t.Fatalf("delta handle: %v", err)
	}

	// With unknown tool output, the safe subset must fail open:
	// either no replacement, or replacement with byte-equal output
	if replace && !bytes.Equal(delta.Raw, original) {
		// If it did replace, the output must not have lost any content
		// (fail-open means full output is sent)
		t.Fatalf("fail-open violated: unknown tool output was compacted (content loss): original=%s mutated=%s", original, delta.Raw)
	}
}

// TestL3SafeSubset_NoCacheBustOnCompaction proves that compaction does not
// cause a cache-bust. The prompt_cache_key must be preserved in the
// compacted output, and the cache-bust session must not fire a demotion.
func TestL3SafeSubset_NoCacheBustOnCompaction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Proxy.ServerStateEnabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	cacheKey := "l3-safety-no-cache-bust"
	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": cacheKey,
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-no-cache-bust","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run tests",
		}},
		"stream": true,
	})
	adapter.handle(ctx, wsmitm.DirClientToServer, &root)

	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_cache", "exec_command", map[string]any{"cmd": "go test ./... -v"})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-no-cache-bust-parent")

	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-no-cache-bust-parent",
		"prompt_cache_key":     cacheKey,
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-no-cache-bust","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_cache",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	_, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta)
	if err != nil {
		t.Fatalf("delta handle: %v", err)
	}

	// Verify prompt_cache_key is preserved in the compacted body
	body, _, ok := wsRequestBody(&delta)
	if !ok {
		t.Fatal("request body missing after compaction")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("compacted body json: %v", err)
	}
	cacheKeyAfter := rawJSONString(raw["prompt_cache_key"])
	if cacheKeyAfter != cacheKey {
		t.Fatalf("prompt_cache_key changed during compaction: before=%q after=%q (cache-bust)", cacheKey, cacheKeyAfter)
	}

	// Verify no cache-bust demotion was recorded
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.cache_bust_demoted"] == "true" {
		t.Fatalf("cache-bust demotion fired during safe compaction (cache regression): %+v", summary)
	}
}

// TestL3SafeSubset_GuardStaysClosedWithoutRecoveryChain proves that the
// stateful delta mutation proof gate stays closed when there is no recovery
// chain available (deltaStatelessRecoveryReady == false). No compaction
// should happen — this is the safety guard that prevents 400s.
func TestL3SafeSubset_GuardStaysClosedWithoutRecoveryChain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Proxy.ServerStateEnabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	ctx := context.Background()

	root := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "l3-safety-no-chain",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-no-chain","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "run tests",
		}},
		"stream": true,
	})
	adapter.handle(ctx, wsmitm.DirClientToServer, &root)

	// Seed tool call but DON'T complete the response — no recovery chain
	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_no_chain", "exec_command", map[string]any{"cmd": "go test ./... -v"})
	// No completeWSSDeltaStatelessResponse call — chain is empty

	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-no-chain-parent", // doesn't exist in chain
		"prompt_cache_key":     "l3-safety-no-chain",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-l3-no-chain","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_no_chain",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	original := append([]byte(nil), delta.Raw...)
	replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta)
	if err != nil {
		t.Fatalf("delta handle: %v", err)
	}

	// Without a recovery chain, the guard must stay closed:
	// no compaction, full output preserved
	if replace && !bytes.Equal(delta.Raw, original) {
		t.Fatalf("guard violated: compaction happened without recovery chain (400 risk): original=%s mutated=%s", original, delta.Raw)
	}

	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.delta_stateless_recovery_ready"] == "true" {
		t.Fatalf("deltaStatelessRecoveryReady should be false without recovery chain: %+v", summary)
	}
}

// TestL3SafeSubset_DetachPreviousResponseIDPreservesBodyStructure proves that
// when detachCodexPreviousResponseID removes previous_response_id from the
// body, the rest of the JSON structure stays intact (no corruption).
func TestL3SafeSubset_DetachPreviousResponseIDPreservesBodyStructure(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","previous_response_id":"resp-123","prompt_cache_key":"key-abc","input":[{"type":"message","role":"user","content":"hello"}],"stream":true}`)

	detached, ok := detachCodexPreviousResponseID(body)
	if !ok {
		t.Fatal("detachCodexPreviousResponseID returned ok=false")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(detached, &raw); err != nil {
		t.Fatalf("detached body is not valid JSON: %v", err)
	}

	// previous_response_id must be removed
	if _, exists := raw["previous_response_id"]; exists {
		t.Fatal("previous_response_id still present after detach")
	}

	// All other fields must be preserved
	if rawJSONString(raw["model"]) != "gpt-5-codex" {
		t.Fatalf("model field lost during detach: %s", raw["model"])
	}
	if rawJSONString(raw["prompt_cache_key"]) != "key-abc" {
		t.Fatalf("prompt_cache_key field lost during detach: %s", raw["prompt_cache_key"])
	}
	if _, exists := raw["input"]; !exists {
		t.Fatal("input field lost during detach")
	}
	if string(raw["stream"]) != "true" {
		t.Fatalf("stream field lost during detach: %s", raw["stream"])
	}
}

// TestL3SafeSubset_DetachFailsOpenOnMissingField proves that
// detachCodexPreviousResponseID fails open (returns false, original body)
// when previous_response_id is not present in the body.
func TestL3SafeSubset_DetachFailsOpenOnMissingField(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","input":[],"stream":true}`)
	_, ok := detachCodexPreviousResponseID(body)
	if ok {
		t.Fatal("detachCodexPreviousResponseID should return ok=false when previous_response_id is missing")
	}
}

// TestL3SafeSubset_DetachFailsOpenOnInvalidJSON proves that
// detachCodexPreviousResponseID fails open on invalid JSON input.
func TestL3SafeSubset_DetachFailsOpenOnInvalidJSON(t *testing.T) {
	body := []byte(`{invalid json`)
	result, ok := detachCodexPreviousResponseID(body)
	if ok {
		t.Fatal("detachCodexPreviousResponseID should return ok=false on invalid JSON")
	}
	if !bytes.Equal(result, body) {
		t.Fatal("detachCodexPreviousResponseID should return original body on failure")
	}
}

// TestL3SafeSubset_ToolOutputResolutionStatsWithInference proves that the
// tool output resolution correctly counts inferred resolutions alongside
// explicit metadata resolutions. This is the mechanism that allows the
// guard to open even when ResponseOutputItemDone is missing.
func TestL3SafeSubset_ToolOutputResolutionStatsWithInference(t *testing.T) {
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{
			{
				Type:         "tool_result",
				Text:         "Process exited with code 0\nOutput:\n M file1.go\n M file2.go\n?? new_file.go\n",
				ToolResultID: "call_git_status",
			},
			{
				Type:         "tool_result",
				Text:         "Process exited with code 0\nOutput:\ninternal/filter/builtin.go:10:func Foo\ninternal/filter/builtin.go:20:func Bar\ninternal/filter/builtin.go:30:func Baz\n",
				ToolResultID: "call_rg",
			},
			{
				Type:         "tool_result",
				Text:         "Process exited with code 0\nOutput:\nrandom output that doesn't match any pattern\n",
				ToolResultID: "call_unknown",
			},
		},
	}}

	// No tool_use metadata — simulates missing ResponseOutputItemDone
	toolUses := map[string]types.ContentBlock{}

	total, resolved, inferred := wssToolOutputResolutionStatsWithToolUses(messages, toolUses)
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
	if resolved != 0 {
		t.Fatalf("expected resolved=0 (no metadata), got %d", resolved)
	}
	// git status and rg patterns should be inferred; unknown should not
	if inferred != 2 {
		t.Fatalf("expected inferred=2 (git status + rg patterns), got %d", inferred)
	}

	// toolOutputKnown should be FALSE because not all tool outputs are known
	// (the unknown one cannot be inferred) — this is the safety guard
	toolOutputKnown := total > 0 && resolved+inferred == total
	if toolOutputKnown {
		t.Fatal("toolOutputKnown should be false when one tool output cannot be inferred (safety guard)")
	}
}

// TestL3SafeSubset_FIFOEvictionDoesNotCorruptActiveChain proves that FIFO
// eviction of old response chains does not corrupt the chain currently being
// used for delta recovery. The active recovery chain must survive eviction.
func TestL3SafeSubset_FIFOEvictionDoesNotCorruptActiveChain(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	// Fill up to the eviction limit with dummy chains
	for i := 0; i < wssRecoveryMaxChains; i++ {
		adapter.mu.Lock()
		adapter.pendingChain = wssResponseChain{
			json.RawMessage(`{"type":"message","role":"user","content":"dummy"}`),
		}
		adapter.mu.Unlock()
		env := parseWSJSON(t, map[string]any{
			"type": string(wsmitm.FrameKindResponseCompleted),
			"response": map[string]any{
				"id":     "resp-dummy-" + strconv.Itoa(i),
				"output": []any{},
			},
		})
		adapter.rememberWSSResponseState(&env)
	}

	// Now insert the "active" chain that we need for recovery
	activeID := "resp-active-recovery"
	activeChain := wssResponseChain{
		json.RawMessage(`{"type":"message","role":"user","content":"active-chain-content"}`),
	}
	adapter.mu.Lock()
	adapter.pendingChain = activeChain
	adapter.mu.Unlock()
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{
			"id":     activeID,
			"output": []any{},
		},
	})
	adapter.rememberWSSResponseState(&env)

	// The active chain must still be present (it's the most recent)
	chain := adapter.wssResponseChain(activeID)
	if len(chain) == 0 {
		t.Fatal("active recovery chain was evicted by FIFO (corruption)")
	}
	if !bytes.Equal(chain[0], activeChain[0]) {
		t.Fatalf("active chain content was corrupted: expected %s got %s", activeChain[0], chain[0])
	}
}

package proxy

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSPhaseFStatelessContinuationReconnectFullHistorySearchCompacts(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexSearchCapDeltaMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(4)
	ctx := context.Background()

	root := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "stateless-continuation-reconnect-search",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-stateless-continuation-reconnect-search","source":"desktop"}`,
			},
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "check search output",
			}},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &root); err != nil || replace {
		t.Fatalf("root seed replace=%v err=%v raw=%s", replace, err, root.Raw)
	}
	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_status_before", "exec_command", map[string]any{"cmd": "git status --short"})
	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_search", "exec_command", map[string]any{"cmd": "cd /repo/search && rg -n needle src"})
	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_status_after", "exec_command", map[string]any{"cmd": "git status --short"})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-stateless-search-parent")
	adapter.markWSSHistoryStatelessMode()

	delta := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-stateless-search-parent",
			"prompt_cache_key":     "stateless-continuation-reconnect-search",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-stateless-continuation-reconnect-search","source":"desktop"}`,
			},
			"input": []map[string]any{
				{"type": "function_call_output", "call_id": "call_status_before", "output": " M internal/proxy/wsmitm_phasef.go\n"},
				{"type": "function_call_output", "call_id": "call_search", "output": proxyWSSSearchOutputFixture("needle", 90)},
				{"type": "function_call_output", "call_id": "call_status_after", "output": " M scripts/utils/wss_local_gap.go\n"},
			},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta); err != nil || !replace {
		t.Fatalf("stateless continuation search should replace, replace=%v err=%v raw=%s", replace, err, delta.Raw)
	}
	body, _, ok := wsRequestBody(&delta)
	if !ok {
		t.Fatal("rewritten stateless continuation body missing")
	}
	if bytes.Contains(body, []byte("previous_response_id")) {
		t.Fatalf("stateless continuation must drop previous_response_id: %s", body)
	}
	raw := string(body)
	if !strings.Contains(raw, "[rg]") ||
		!strings.Contains(raw, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(raw, "src/file_089.go:90:needle") {
		t.Fatalf("stateless continuation search should compact with archive recovery: %s", raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.stateless_history_continuation"] != "true" ||
		summary.DebugFacts["wss.stateless_history_continuation_detached_previous_response"] != "true" ||
		summary.DebugFacts["wss.full_history_detached_previous_response"] != "true" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" ||
		summary.DebugFacts["wss.previous_response_id"] != "false" ||
		summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.effective_mutation_guard"] != "" ||
		summary.DebugFacts["wss.full_history_stateless_followup"] != "true" ||
		summary.Tokens.Saved <= 0 ||
		summary.MessagesCompressed == 0 {
		t.Fatalf("stateless continuation search should save without downstream guard: %+v", summary)
	}
}

func TestWSPhaseFStatelessContinuationSurvivesSocketReconnect(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexSearchCapDeltaMutationEnabled = true
	p := New(cfg)
	ctx := context.Background()

	firstSocket := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	firstSocket.setSocketSeq(2)
	fullHistory := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "stateless-cross-socket-search",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-stateless-cross-socket-search","source":"desktop"}`,
			},
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "search for needle"},
				{"type": "function_call", "call_id": "call_search", "name": "exec_command", "arguments": map[string]any{"cmd": "cd /repo/search && rg -n needle src"}},
				{"type": "function_call_output", "call_id": "call_search", "output": proxyWSSSearchOutputFixture("needle", 90)},
			},
			"stream": true,
		},
	})
	if replace, err := firstSocket.handle(ctx, wsmitm.DirClientToServer, &fullHistory); err != nil || !replace {
		t.Fatalf("first socket full-history should mutate and arm stateless export replace=%v err=%v raw=%s", replace, err, fullHistory.Raw)
	}
	firstSummary := p.DebugRecorder().Last(1, false)[0]
	if firstSummary.DebugFacts["wss.full_history_stateless_followup"] != "true" ||
		firstSummary.Tokens.Saved <= 0 {
		t.Fatalf("first socket full-history should save and arm stateless export: %+v", firstSummary)
	}

	itemDone := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseOutputItemDone),
		"item": map[string]any{
			"type":      "function_call",
			"call_id":   "call_next",
			"name":      "exec_command",
			"arguments": `{"cmd":"git status --short"}`,
		},
	})
	if replace, err := firstSocket.handle(ctx, wsmitm.DirServerToClient, &itemDone); err != nil || replace {
		t.Fatalf("first socket output item replace=%v err=%v", replace, err)
	}
	completed := parseWSJSON(t, map[string]any{
		"type":     string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{"id": "resp-stateless-cross-socket-child", "output": []any{}},
	})
	if replace, err := firstSocket.handle(ctx, wsmitm.DirServerToClient, &completed); err != nil || replace {
		t.Fatalf("first socket completion replace=%v err=%v", replace, err)
	}

	secondSocket := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	secondSocket.setSocketSeq(3)
	delta := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-stateless-cross-socket-child",
			"prompt_cache_key":     "stateless-cross-socket-search",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-stateless-cross-socket-search","source":"desktop"}`,
			},
			"input": []map[string]any{{
				"type":    "function_call_output",
				"call_id": "call_next",
				"output":  " M internal/proxy/wsmitm_phasef.go\n",
			}},
			"stream": true,
		},
	})
	if replace, err := secondSocket.handle(ctx, wsmitm.DirClientToServer, &delta); err != nil || !replace {
		t.Fatalf("second socket delta should become stateless full-history replace=%v err=%v raw=%s", replace, err, delta.Raw)
	}
	body, _, ok := wsRequestBody(&delta)
	if !ok {
		t.Fatal("second socket rewritten body missing")
	}
	if bytes.Contains(body, []byte("previous_response_id")) {
		t.Fatalf("cross-socket stateless continuation must drop previous_response_id: %s", body)
	}
	items, ok := wssInputItems(body)
	if !ok || len(items) <= 1 {
		t.Fatalf("cross-socket stateless continuation must include exported prior chain, items=%d ok=%v body=%s", len(items), ok, body)
	}
	secondSummary := p.DebugRecorder().Last(1, false)[0]
	if secondSummary.DebugFacts["wss.stateless_history_continuation"] != "true" ||
		secondSummary.DebugFacts["wss.request_shape"] != "full_history" ||
		secondSummary.DebugFacts["wss.previous_response_id"] != "false" {
		t.Fatalf("second socket should be recorded as stateless full-history: %+v", secondSummary)
	}
}

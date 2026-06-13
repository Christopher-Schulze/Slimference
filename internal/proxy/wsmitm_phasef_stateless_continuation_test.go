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

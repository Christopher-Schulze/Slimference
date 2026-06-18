package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSPhaseFDefaultDeltaSavingsOpenWhenStatelessRecoveryReady(t *testing.T) {
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
		"prompt_cache_key": "delta-stateless-product-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-delta-stateless-product","source":"desktop"}`,
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

	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_tests", "exec_command", map[string]any{"cmd": "go test ./... -v"})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-delta-parent")
	if chain := adapter.wssResponseChain("resp-delta-parent"); len(chain) == 0 {
		t.Fatal("parent response chain was not seeded")
	}

	delta := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-delta-parent",
		"prompt_cache_key":     "delta-stateless-product-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-delta-stateless-product","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_tests",
			"output":  deltaStatelessGoTestOutput(),
		}},
		"stream": true,
	})
	original := append([]byte(nil), delta.Raw...)
	replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &delta)
	if err != nil {
		t.Fatalf("chain-ready delta handle: %v", err)
	}
	if !replace || bytes.Equal(delta.Raw, original) {
		t.Fatalf("chain-ready default delta must compact, replace=%v raw=%s", replace, delta.Raw)
	}
	mutated := string(delta.Raw)
	if !strings.Contains(mutated, "SLIMFERENCE_TEST_FAILURE_SENTINEL") ||
		strings.Contains(mutated, "TestPassing089") ||
		!strings.Contains(mutated, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("chain-ready delta compaction lost failure detail or archive recovery: %s", mutated)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.BypassReason != "" ||
		summary.Tokens.Saved <= 0 ||
		summary.DebugFacts["wss.delta_stateless_recovery_ready"] != "true" ||
		summary.DebugFacts["wss.delta_stateless_recovery_gate"] != "open" ||
		summary.DebugFacts["wss.stateful_mutation_stateless_followup"] != "true" ||
		summary.DebugFacts["wss.stateful_delta_mutation_blocked"] == "true" ||
		summary.DebugFacts["wss.effective_mutation_guard"] == "wss_stateful_delta_mutation_proof_gate" ||
		summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("chain-ready default delta should save behind stateless recovery facts: %+v", summary)
	}

	seedWSSDeltaStatelessToolCall(t, ctx, adapter, "call_status", "exec_command", map[string]any{"cmd": "git status --short"})
	completeWSSDeltaStatelessResponse(t, ctx, adapter, "resp-delta-child")

	followup := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-delta-child",
		"prompt_cache_key":     "delta-stateless-product-session",
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": `{"thread_id":"thread-delta-stateless-product","source":"desktop"}`,
		},
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_status",
			"output":  " M internal/proxy/wsmitm_phasef.go\n?? internal/proxy/wsmitm_phasef_delta_stateless_test.go\n",
		}},
		"stream": true,
	})
	if replace, err := adapter.handle(ctx, wsmitm.DirClientToServer, &followup); err != nil || !replace {
		t.Fatalf("follow-up delta should rewrite to stateless full-history, replace=%v err=%v raw=%s", replace, err, followup.Raw)
	}
	followupBody, _, ok := wsRequestBody(&followup)
	if !ok {
		t.Fatal("follow-up request body missing")
	}
	if bytes.Contains(followupBody, []byte("previous_response_id")) {
		t.Fatalf("stateless follow-up must drop previous_response_id: %s", followupBody)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(followupBody, &raw); err != nil {
		t.Fatalf("follow-up body json: %v", err)
	}
	var input []json.RawMessage
	if err := json.Unmarshal(raw["input"], &input); err != nil {
		t.Fatalf("follow-up input json: %v", err)
	}
	if len(input) <= 1 {
		t.Fatalf("stateless follow-up must include prior chain, got %d input items: %s", len(input), followupBody)
	}
	followupSummary := p.DebugRecorder().Last(1, false)[0]
	if followupSummary.DebugFacts["wss.stateless_history_continuation"] != "true" ||
		followupSummary.DebugFacts["wss.stateless_history_continuation_detached_previous_response"] != "true" ||
		followupSummary.DebugFacts["wss.full_history_detached_previous_response"] != "true" {
		t.Fatalf("follow-up should carry stateless continuation fact: %+v", followupSummary)
	}
}

func seedWSSDeltaStatelessToolCall(t *testing.T, ctx context.Context, adapter *wsPhaseFAdapter, callID, name string, arguments any) {
	t.Helper()
	itemDone := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseOutputItemDone),
		"item": map[string]any{
			"type":      "function_call",
			"call_id":   callID,
			"name":      name,
			"arguments": arguments,
		},
	})
	if replace, err := adapter.handle(ctx, wsmitm.DirServerToClient, &itemDone); err != nil || replace {
		t.Fatalf("tool call seed replace=%v err=%v", replace, err)
	}
}

func completeWSSDeltaStatelessResponse(t *testing.T, ctx context.Context, adapter *wsPhaseFAdapter, responseID string) {
	t.Helper()
	completed := parseWSJSON(t, map[string]any{
		"type":     string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{"id": responseID, "output": []any{}},
	})
	if replace, err := adapter.handle(ctx, wsmitm.DirServerToClient, &completed); err != nil || replace {
		t.Fatalf("response completion replace=%v err=%v", replace, err)
	}
}

func deltaStatelessGoTestOutput() string {
	var payload strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	payload.WriteString("=== RUN   TestSlimferenceFailure\n")
	payload.WriteString("    fail_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
	payload.WriteString("--- FAIL: TestSlimferenceFailure (0.00s)\n")
	payload.WriteString("FAIL\texample.test/liveproof\t0.015s\n")
	return "Chunk ID: delta-stateless-product\nWall time: 0.0000 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" + payload.String()
}

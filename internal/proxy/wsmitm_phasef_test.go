package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/sniroute"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/savingspolicy"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/staleread"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
	"github.com/Christopher-Schulze/Slimference/internal/toolprune"
	"github.com/Christopher-Schulze/Slimference/internal/types"
	"github.com/Christopher-Schulze/Slimference/internal/wscompact"
)

func TestWSPhaseFRequestSkipsStopOnResponsesShape(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindRequest),
		"trace": "keep-me",
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "conv-1",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Run tests.",
			}},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatal("Responses-shaped request must not be re-encoded for stop injection")
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(env.Body, &body); err != nil {
		t.Fatalf("body JSON: %v", err)
	}
	if _, ok := body["stop"]; ok {
		t.Fatalf("Responses-shaped request must not carry stop: %s", env.Body)
	}
	if got := p.OutputReduceCountersSnapshot().StopSeqRequestsModified; got != 0 {
		t.Fatalf("stop counter=%d, want 0", got)
	}
}

func TestWSSPlannerTokenCountsNoMutationUsesCheapEstimate(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","input":"hello","stream":true}`)
	original, final := wssPlannerTokenCounts(body, []byte(`{"different":true}`), nil, proxyLayer0Stats{}, false)
	want := tokens.Estimate(len(body))
	if original != want || final != want {
		t.Fatalf("no-mutation planner counts = %d/%d, want cheap estimate %d/%d", original, final, want, want)
	}
}

func TestWSPhaseFRepdetRewritesStreamedTextDelta(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	toolOutput := strings.Repeat("large unchanged tool output block ", 18)

	req := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "conv-repdet",
			"input": []map[string]any{
				{
					"type":      "function_call",
					"call_id":   "call_repdet",
					"name":      "exec_command",
					"arguments": map[string]any{"cmd": "cat large.txt"},
				},
				{
					"type":    "function_call_output",
					"call_id": "call_repdet",
					"output":  toolOutput,
				},
			},
			"stream": true,
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &req)
	if err != nil {
		t.Fatalf("request handle: %v", err)
	}
	if replace {
		t.Fatal("Responses-shaped request should seed state without stop mutation")
	}

	resp := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": "Here is the same content again: " + toolOutput,
	})
	replace, err = adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp)
	if err != nil {
		t.Fatalf("response handle: %v", err)
	}
	if !replace {
		t.Fatal("expected repdet to re-encode streamed delta")
	}
	if !strings.Contains(resp.Delta, "[unchanged:") {
		t.Fatalf("repdet marker missing: %q", resp.Delta)
	}
	if got := p.OutputReduceCountersSnapshot().RepdetResponsesRewritten; got != 1 {
		t.Fatalf("repdet counter=%d, want 1", got)
	}
	snap := adapter.snapshot()
	if snap.RequestsSeen != 1 ||
		snap.RequestBodiesSeen != 1 ||
		snap.RequestMessagesIndexed != 1 ||
		snap.ResponseTextDeltasSeen != 1 ||
		snap.Mutations != 1 {
		t.Fatalf("unexpected Phase-F adapter telemetry: %+v", snap)
	}
}

func TestWSPhaseFRepdetSkipsLongUserText(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	promptText := strings.Repeat("large user prompt block ", 18)

	req := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "conv-repdet-user-text",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": promptText,
			}},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &req); err != nil || replace {
		t.Fatalf("request should seed without mutation, replace=%v err=%v", replace, err)
	}

	resp := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": "Echo: " + promptText,
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp)
	if err != nil {
		t.Fatalf("response handle: %v", err)
	}
	if replace || strings.Contains(resp.Delta, "[unchanged:") {
		t.Fatalf("long user text must not seed repdet, replace=%v delta=%q", replace, resp.Delta)
	}
}

func TestWSPhaseFDoesNotStreamcutWSSDelta(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StreamCutEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	resp := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": strings.Repeat("substantive answer ", 8) + "\nHope this helps",
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp)
	if err != nil {
		t.Fatalf("response handle: %v", err)
	}
	if replace {
		t.Fatal("WSS streamcut must stay disabled until terminal-safe semantics are certified")
	}
	if !strings.Contains(resp.Delta, "Hope this helps") {
		t.Fatalf("WSS delta was unexpectedly changed: %q", resp.Delta)
	}
	if got := p.OutputReduceCountersSnapshot().StreamcutFired; got != 0 {
		t.Fatalf("HTTP streamcut counter should not be used by WSS, got %d", got)
	}
}

func TestWSPhaseFRecordsProviderCacheUsageFromCompletedResponse(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	resp := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{
			"id": "resp-cache",
			"usage": map[string]any{
				"input_tokens": 1000,
				"input_tokens_details": map[string]any{
					"cached_tokens": 345,
				},
				"output_tokens": 12,
			},
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp)
	if err != nil {
		t.Fatalf("response handle: %v", err)
	}
	if replace {
		t.Fatal("usage accounting must not mutate the WSS response")
	}

	select {
	case event := <-p.analyticsQueue:
		p.analytics.Record(event)
	default:
		t.Fatal("expected WSS provider usage analytics event")
	}
	got := (&SavingsProbe{Proxy: p}).ProbeSavings(context.Background())
	if got.ProviderCacheReadTokens != 345 || got.Product.ProviderCacheReadTokens != 345 {
		t.Fatalf("provider cache read tokens not surfaced: %+v", got)
	}
	if got.Product.Status != "saving" {
		t.Fatalf("product status=%q, want saving", got.Product.Status)
	}
	if snap := p.outputReduce.Snapshot(); snap.OutputTokensObserved != 12 {
		t.Fatalf("output-reduce WSS output tokens=%d, want 12", snap.OutputTokensObserved)
	}
}

func TestWSPhaseFObservedEditBypassesReadDelta(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Defaults()
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	sessionID := "codex-wss:wss-edit-guard"
	before := "package main\nfunc a() {}\nfunc b() {}\nfunc c() {}\nfunc d() {}\nfunc e() {}\n"
	fresh := before + "changed line\n"

	first := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "wss-edit-guard",
			"input": []map[string]any{
				{"type": "function_call", "call_id": "read-1", "name": "read_file", "arguments": map[string]any{"path": "src/x.go"}},
				{"type": "function_call_output", "call_id": "read-1", "output": before},
			},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &first); err != nil || replace {
		t.Fatalf("first read should pass through, replace=%v err=%v body=%s", replace, err, first.Body)
	}

	second := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"prompt_cache_key":     "wss-edit-guard",
			"previous_response_id": "resp-1",
			"input": []map[string]any{
				{"type": "function_call", "call_id": "edit-1", "name": "apply_patch", "arguments": map[string]any{"path": "src/x.go", "patch": "*** Begin Patch\n*** Update File: src/x.go\n*** End Patch"}},
				{"type": "function_call_output", "call_id": "edit-1", "output": "patch applied"},
				{"type": "function_call", "call_id": "read-2", "name": "read_file", "arguments": map[string]any{"path": "src/x.go"}},
				{"type": "function_call_output", "call_id": "read-2", "output": fresh},
			},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &second); err != nil || replace {
		t.Fatalf("recently edited reread should pass through, replace=%v err=%v body=%s", replace, err, second.Body)
	}
	hit, err := sessions.RecentlyEditedHookFile(sessions.DefaultHookStateDir(home), sessionID, "src/x.go", 2)
	if err != nil || !hit {
		t.Fatalf("WSS edit observation missing, hit=%v err=%v", hit, err)
	}
}

func TestWSPhaseFReReadAfterCollapseRestoresFullRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	var bodyBuilder strings.Builder
	for i := 0; i < 80; i++ {
		bodyBuilder.WriteString("restored read line ")
		bodyBuilder.WriteString(strconv.Itoa(i))
		bodyBuilder.WriteString(" with enough stable content\n")
	}
	bodyText := bodyBuilder.String()
	bodyForTurn := func(turnID, callID string) []byte {
		return mustMarshal(map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "restore-session",
			"input": []map[string]any{
				{"type": "function_call", "call_id": callID, "name": "read_file", "arguments": map[string]any{"path": "src/x.go"}},
				{"type": "function_call_output", "call_id": callID, "output": bodyText},
			},
			"stream": true,
		})
	}

	first, _, changed, stats, reReads := adapter.applyInputPipeline(bodyForTurn("resp-1", "read-1"))
	if changed || reReads != 0 || stats.ReadDeltaMisses != 1 || stats.ReadDeltaBlocks != 0 {
		t.Fatalf("first read should seed only, changed=%v rereads=%d stats=%+v body=%s", changed, reReads, stats, first)
	}
	second, _, changed, stats, reReads := adapter.applyInputPipeline(bodyForTurn("resp-2", "read-2"))
	if !changed || reReads != 1 || stats.ReadDeltaBlocks != 1 || stats.TokensSaved <= 0 ||
		!strings.Contains(string(second), "archive=local-archive://") {
		t.Fatalf("second read should collapse once, changed=%v rereads=%d stats=%+v body=%s", changed, reReads, stats, second)
	}
	third, _, changed, stats, reReads := adapter.applyInputPipeline(bodyForTurn("resp-3", "read-3"))
	if changed || reReads != 1 || stats.ReadDeltaAttempts != 0 || stats.ReadDeltaBlocks != 0 ||
		stats.TokensSaved != 0 || !strings.Contains(string(third), "restored read line") ||
		strings.Contains(string(third), "local-archive://") {
		t.Fatalf("post-collapse reread should be restored full-pass, changed=%v rereads=%d stats=%+v body=%s",
			changed, reReads, stats, third)
	}
}

func TestWSPhaseFPreviousResponseReadDeltaFullPassesBeforeRecencyPolicy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	cfg.Compression.OutputReduce.ReadDeltaRecentFullPassTurns = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	bodyText := strings.Repeat("recency protected read line\n", 80)
	bodyForTurn := func(turnID, callID string) []byte {
		return mustMarshal(map[string]any{
			"model":                "gpt-5-codex",
			"prompt_cache_key":     "recency-session",
			"previous_response_id": turnID,
			"input": []map[string]any{
				{"type": "function_call", "call_id": callID, "name": "read_file", "arguments": map[string]any{"path": "src/recent.go"}},
				{"type": "function_call_output", "call_id": callID, "output": bodyText},
			},
			"stream": true,
		})
	}

	first, _, changed, stats, _ := adapter.applyInputPipeline(bodyForTurn("resp-1", "read-1"))
	if changed || stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 ||
		stats.ReadDeltaAttempts != 1 || stats.ReadDeltaMisses != 1 || stats.ReadDeltaBlocks != 0 ||
		!strings.Contains(string(first), "recency protected read line") {
		t.Fatalf("previous-response first read should seed read-delta without mutation, changed=%v stats=%+v body=%s", changed, stats, first)
	}
	second, _, changed, stats, _ := adapter.applyInputPipeline(bodyForTurn("resp-2", "read-2"))
	if changed || stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 ||
		stats.ReadDeltaAttempts != 1 || stats.ReadDeltaMisses != 1 || stats.ReadDeltaBlocks != 0 ||
		len(stats.CacheEvents) != 1 || stats.CacheEvents[0].Reason != "recent_full_pass_window" ||
		!strings.Contains(string(second), "recency protected read line") ||
		strings.Contains(string(second), "local-archive://") {
		t.Fatalf("previous-response reread should honor recency policy without mutation, changed=%v stats=%+v body=%s", changed, stats, second)
	}
}

func TestWSPhaseFBeTerseInjectsIntoCodexResponsesInputForTreatment(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.BeTerseHintEnabled = true
	cfg.Compression.OutputReduce.BeTerseHintText = "be concise"
	p := New(cfg)
	conversationID := findCodexWSSTreatmentConversation(t, p)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": conversationID,
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Summarize this.",
			}},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !replace {
		t.Fatal("expected be-terse request mutation")
	}
	if !strings.Contains(string(env.Body), "be concise") {
		t.Fatalf("be-terse hint missing from body: %s", env.Body)
	}
	if got := p.OutputReduceCountersSnapshot().BeterseInjections; got != 1 {
		t.Fatalf("beterse counter=%d, want 1", got)
	}
}

func TestWSPhaseFOutputReduceDoesNotInjectCodexWSSDirective(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.ConciseChatEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	origToolOutputScan := wssBodyContainsToolOutputFn
	origUserPromptScan := wssBodyHasUserPromptInputFn
	origPromptCachePrefixScan := wssBodyHasPromptCachePrefixFn
	toolOutputScans := 0
	userPromptScans := 0
	promptCachePrefixScans := 0
	wssBodyContainsToolOutputFn = func(body []byte) bool {
		toolOutputScans++
		return origToolOutputScan(body)
	}
	wssBodyHasUserPromptInputFn = func(body []byte) bool {
		userPromptScans++
		return origUserPromptScan(body)
	}
	wssBodyHasPromptCachePrefixFn = func(body []byte) bool {
		promptCachePrefixScans++
		return origPromptCachePrefixScan(body)
	}
	defer func() {
		wssBodyContainsToolOutputFn = origToolOutputScan
		wssBodyHasUserPromptInputFn = origUserPromptScan
		wssBodyHasPromptCachePrefixFn = origPromptCachePrefixScan
	}()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "output-reduce-session",
			"instructions":    strings.Repeat("stable project instruction ", 2200),
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "What is the current status?",
			}},
			"stream": true,
		},
	})
	original := append([]byte(nil), env.Body...)

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatalf("Codex WSS output-reduce directive must stay byte-equal, got body: %s", env.Body)
	}
	body := string(env.Body)
	if !bytes.Equal(original, env.Body) {
		t.Fatalf("Codex WSS output-reduce changed body\nbefore: %s\nafter: %s", original, env.Body)
	}
	if strings.Contains(body, "#slimference-output-rules") {
		t.Fatalf("Codex WSS must not receive output-reduce instructions: %s", body)
	}
	if strings.Contains(body, `"role":"system"`) {
		t.Fatalf("Codex output-reduce must not inject an input system message: %s", body)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 1 || snap.LastReason != "codex_wss_directive_disabled" {
		t.Fatalf("output-reduce tracker = %+v, want one WSS directive skip", snap)
	}
	if toolOutputScans != 0 {
		t.Fatalf("known non-tool-output request did %d redundant tool-output body scans", toolOutputScans)
	}
	if userPromptScans != 0 {
		t.Fatalf("known user-prompt request did %d redundant user-input body scans", userPromptScans)
	}
	if promptCachePrefixScans != 0 {
		t.Fatalf("known prompt-cache-prefix state did %d redundant body scans", promptCachePrefixScans)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("expected one debug summary, got %d", len(summaries))
	}
	if summaries[0].OutputReduce.Applied || summaries[0].OutputReduce.Reason != "codex_wss_directive_disabled" {
		t.Fatalf("debug output-reduce = %+v, want codex_wss_directive_disabled skip", summaries[0].OutputReduce)
	}
	if summaries[0].DebugFacts["wss.changed"] != "false" || summaries[0].DebugFacts["wss.output_reduce_applied"] != "false" {
		t.Fatalf("unexpected WSS debug facts: %+v", summaries[0].DebugFacts)
	}
}

func TestWSPhaseFOutputReduceUnknownPresenceFallsBackToBodyScans(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.ConciseChatEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	origToolOutputScan := wssBodyContainsToolOutputFn
	origUserPromptScan := wssBodyHasUserPromptInputFn
	toolOutputScans := 0
	userPromptScans := 0
	wssBodyContainsToolOutputFn = func(body []byte) bool {
		toolOutputScans++
		return origToolOutputScan(body)
	}
	wssBodyHasUserPromptInputFn = func(body []byte) bool {
		userPromptScans++
		return origUserPromptScan(body)
	}
	defer func() {
		wssBodyContainsToolOutputFn = origToolOutputScan
		wssBodyHasUserPromptInputFn = origUserPromptScan
	}()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "output-reduce-fallback-session",
			"instructions":    strings.Repeat("stable project instruction ", 2200),
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "What is the current status?",
			}},
			"stream": true,
		},
	})

	_, stats := adapter.applyWSSOutputReduce(env.Body, false, false, false, false, false, false)
	if stats.Reason != "codex_wss_directive_disabled" {
		t.Fatalf("fallback output-reduce reason=%q, want codex_wss_directive_disabled", stats.Reason)
	}
	if toolOutputScans != 1 {
		t.Fatalf("unknown tool-output presence scans=%d, want 1", toolOutputScans)
	}
	if userPromptScans != 1 {
		t.Fatalf("unknown user-prompt presence scans=%d, want 1", userPromptScans)
	}
}

func TestWSPhaseFOutputReduceKnownNoUserPromptSkipsUserInputBodyScan(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.ConciseChatEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	origUserPromptScan := wssBodyHasUserPromptInputFn
	userPromptScans := 0
	wssBodyHasUserPromptInputFn = func(body []byte) bool {
		userPromptScans++
		return origUserPromptScan(body)
	}
	defer func() {
		wssBodyHasUserPromptInputFn = origUserPromptScan
	}()

	body := []byte(`{"model":"gpt-5-codex","input":[{"type":"message","role":"assistant","content":"done"}],"stream":true}`)
	_, stats := adapter.applyWSSOutputReduce(body, false, true, true, false, true, false)
	if stats.Reason != "disabled" {
		t.Fatalf("known no-user output-reduce reason=%q, want disabled", stats.Reason)
	}
	if userPromptScans != 0 {
		t.Fatalf("known no-user request did %d redundant user-input body scans", userPromptScans)
	}
}

func TestWSPhaseFOutputReduceKnownPromptCachePrefixSkipsBodyScan(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.ConciseChatEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	origPromptCachePrefixScan := wssBodyHasPromptCachePrefixFn
	promptCachePrefixScans := 0
	wssBodyHasPromptCachePrefixFn = func(body []byte) bool {
		promptCachePrefixScans++
		return origPromptCachePrefixScan(body)
	}
	defer func() {
		wssBodyHasPromptCachePrefixFn = origPromptCachePrefixScan
	}()

	body := []byte(`{"model":"gpt-5-codex","prompt_cache_key":"abc","instructions":"stable","input":[{"type":"message","role":"user","content":"status"}],"stream":true}`)
	_, stats := adapter.applyWSSOutputReduce(body, false, true, true, true, true, true)
	if stats.Reason != "prompt_cache_prefix_full_pass" {
		t.Fatalf("known prompt-cache-prefix reason=%q, want prompt_cache_prefix_full_pass", stats.Reason)
	}
	if promptCachePrefixScans != 0 {
		t.Fatalf("known prompt-cache-prefix request did %d redundant body scans", promptCachePrefixScans)
	}
}

func TestWSPhaseFOutputReduceUnknownPromptCachePrefixFallsBackToBodyScan(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.ConciseChatEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	origPromptCachePrefixScan := wssBodyHasPromptCachePrefixFn
	promptCachePrefixScans := 0
	wssBodyHasPromptCachePrefixFn = func(body []byte) bool {
		promptCachePrefixScans++
		return origPromptCachePrefixScan(body)
	}
	defer func() {
		wssBodyHasPromptCachePrefixFn = origPromptCachePrefixScan
	}()

	body := []byte(`{"model":"gpt-5-codex","prompt_cache_key":"abc","instructions":"stable","input":[{"type":"message","role":"user","content":"status"}],"stream":true}`)
	_, stats := adapter.applyWSSOutputReduce(body, false, true, true, true, false, false)
	if stats.Reason != "prompt_cache_prefix_full_pass" {
		t.Fatalf("unknown prompt-cache-prefix reason=%q, want prompt_cache_prefix_full_pass", stats.Reason)
	}
	if promptCachePrefixScans != 1 {
		t.Fatalf("unknown prompt-cache-prefix scans=%d, want 1", promptCachePrefixScans)
	}
}

func TestWSSUserPromptPresenceHelpers(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]json.RawMessage
		want bool
	}{
		{
			name: "nil_raw",
			raw:  nil,
			want: false,
		},
		{
			name: "input_string",
			raw:  map[string]json.RawMessage{"input": json.RawMessage(`"summarize this"`)},
			want: true,
		},
		{
			name: "blank_input_string",
			raw:  map[string]json.RawMessage{"input": json.RawMessage(`"   "`)},
			want: false,
		},
		{
			name: "user_message_item",
			raw:  map[string]json.RawMessage{"input": json.RawMessage(`[{"type":"message","role":"user","content":"status"}]`)},
			want: true,
		},
		{
			name: "blank_user_message_content",
			raw:  map[string]json.RawMessage{"input": json.RawMessage(`[{"type":"message","role":"user","content":"   "}]`)},
			want: false,
		},
		{
			name: "empty_user_message_content_parts",
			raw:  map[string]json.RawMessage{"input": json.RawMessage(`[{"type":"message","role":"user","content":[]}]`)},
			want: false,
		},
		{
			name: "image_only_user_message_content",
			raw:  map[string]json.RawMessage{"input": json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_image","url":"local"}]}]`)},
			want: false,
		},
		{
			name: "multi_text_user_message_content",
			raw:  map[string]json.RawMessage{"input": json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"one"},{"type":"text","text":"two"}]}]`)},
			want: true,
		},
		{
			name: "assistant_message_item",
			raw:  map[string]json.RawMessage{"input": json.RawMessage(`[{"type":"message","role":"assistant","content":"done"}]`)},
			want: false,
		},
		{
			name: "invalid_items",
			raw:  map[string]json.RawMessage{"input": json.RawMessage(`{"type":"message","role":"user","content":"status"}`)},
			want: false,
		},
	}
	for _, tt := range tests {
		if got := wssRawHasUserPromptInput(tt.raw); got != tt.want {
			t.Fatalf("%s raw presence=%v, want %v", tt.name, got, tt.want)
		}
		if got := wssInputHasUserPromptInput(tt.raw["input"]); got != tt.want {
			t.Fatalf("%s input presence=%v, want %v", tt.name, got, tt.want)
		}
		body, err := json.Marshal(tt.raw)
		if err != nil {
			t.Fatalf("%s marshal body: %v", tt.name, err)
		}
		if got := wssBodyHasUserPromptInput(body); got != tt.want {
			t.Fatalf("%s body presence=%v, want %v", tt.name, got, tt.want)
		}
		meta := wssRequestMetaFromRaw(tt.raw)
		if meta.HasUserPromptInput != tt.want {
			t.Fatalf("%s meta user prompt=%v, want %v", tt.name, meta.HasUserPromptInput, tt.want)
		}
	}
}

func TestWSSRawHasToolDefinitions(t *testing.T) {
	if wssRawHasToolDefinitions(nil) {
		t.Fatal("nil raw must not report tool definitions")
	}
	if !wssRawHasToolDefinitions(map[string]json.RawMessage{"tools": json.RawMessage(`[]`)}) {
		t.Fatal("tools key presence must report tool definitions")
	}
}

func TestWSSRawHasPromptCachePrefix(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]json.RawMessage
		want bool
	}{
		{name: "nil_raw", raw: nil, want: false},
		{name: "missing_key", raw: map[string]json.RawMessage{"input": json.RawMessage(`[]`)}, want: false},
		{name: "key_without_cacheable_fields", raw: map[string]json.RawMessage{"prompt_cache_key": json.RawMessage(`"abc"`)}, want: false},
		{name: "instructions", raw: map[string]json.RawMessage{"prompt_cache_key": json.RawMessage(`"abc"`), "instructions": json.RawMessage(`"stable"`)}, want: true},
		{name: "input_only", raw: map[string]json.RawMessage{"prompt_cache_key": json.RawMessage(`"abc"`), "input": json.RawMessage(`[]`)}, want: false},
		{name: "tools", raw: map[string]json.RawMessage{"prompt_cache_key": json.RawMessage(`"abc"`), "tools": json.RawMessage(`[]`)}, want: true},
	}
	for _, tt := range tests {
		if got := wssRawHasPromptCachePrefix(tt.raw); got != tt.want {
			t.Fatalf("%s raw prompt-cache-prefix=%v, want %v", tt.name, got, tt.want)
		}
		meta := wssRequestMetaFromRaw(tt.raw)
		if meta.HasPromptCachePrefix != tt.want {
			t.Fatalf("%s meta prompt-cache-prefix=%v, want %v", tt.name, meta.HasPromptCachePrefix, tt.want)
		}
	}
}

func TestWSSRequestDebugFactsExposePrefixByteMetrics(t *testing.T) {
	raw := map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "prefix-metrics",
		"instructions":     "stable instructions",
		"tools": []map[string]any{
			{"type": "function", "name": "exec_command", "description": "run commands"},
			{"type": "function", "name": "apply_patch", "description": "edit files"},
		},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
	}
	body := mustMarshal(raw)
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	meta := wssRequestMetaFromRaw(decoded)
	messages := []types.Message{{
		Role:    "user",
		Content: []types.ContentBlock{{Type: "text", Text: "continue"}},
	}}

	facts := wssRequestDebugFacts(body, body, messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "prompt_cache_prefix_full_pass"})
	if facts["wss.prompt_cache_prefix"] != "true" ||
		facts["wss.has_tool_definitions"] != "true" ||
		facts["wss.tool_definitions"] != "2" ||
		facts["wss.tool_definition_default_keep"] != "2" ||
		facts["wss.tool_definition_nondefault"] != "0" ||
		facts["wss.tool_definition_unnamed"] != "0" ||
		facts["wss.output_reduce_reason"] != "prompt_cache_prefix_full_pass" {
		t.Fatalf("prefix facts missing: %+v", facts)
	}
	if n, err := strconv.Atoi(facts["wss.tool_definition_bytes"]); err != nil || n <= 0 {
		t.Fatalf("tool_definition_bytes=%q err=%v", facts["wss.tool_definition_bytes"], err)
	}
	if n, err := strconv.Atoi(facts["wss.tool_definition_default_keep_bytes"]); err != nil || n <= 0 {
		t.Fatalf("tool_definition_default_keep_bytes=%q err=%v", facts["wss.tool_definition_default_keep_bytes"], err)
	}
	if n, err := strconv.Atoi(facts["wss.tool_definition_nondefault_bytes"]); err != nil || n != 0 {
		t.Fatalf("tool_definition_nondefault_bytes=%q err=%v", facts["wss.tool_definition_nondefault_bytes"], err)
	}
	if n, err := strconv.Atoi(facts["wss.tool_definition_unnamed_bytes"]); err != nil || n != 0 {
		t.Fatalf("tool_definition_unnamed_bytes=%q err=%v", facts["wss.tool_definition_unnamed_bytes"], err)
	}
	if n, err := strconv.Atoi(facts["wss.instructions_bytes"]); err != nil || n <= 0 {
		t.Fatalf("instructions_bytes=%q err=%v", facts["wss.instructions_bytes"], err)
	}
}

func TestWSPhaseFToolPruneGuardUsesMetaToolDefinitions(t *testing.T) {
	deltaMessages := []types.Message{{
		Role:    "user",
		Content: []types.ContentBlock{{Type: "text", Text: "continue"}},
	}}
	fullHistoryMessages := []types.Message{{
		Role:    "assistant",
		Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call_1"}},
	}}

	base := wssRequestMeta{PreviousResponseID: "resp_prev", HasUserPromptInput: true}
	if got := wssToolPruneMutationGuardReason(deltaMessages, base, nil); got != "" {
		t.Fatalf("delta without tools or reattach guard=%q, want empty", got)
	}
	withTools := base
	withTools.HasToolDefinitions = true
	if got := wssToolPruneMutationGuardReason(deltaMessages, withTools, nil); got != "wss_tool_prune_delta_guard" {
		t.Fatalf("delta with tools guard=%q, want wss_tool_prune_delta_guard", got)
	}
	if got := wssToolPruneMutationGuardReason(deltaMessages, base, []string{"Bash"}); got != "wss_tool_prune_delta_guard" {
		t.Fatalf("delta reattach guard=%q, want wss_tool_prune_delta_guard", got)
	}
	if got := wssToolPruneMutationGuardReason(fullHistoryMessages, withTools, nil); got != "" {
		t.Fatalf("full-history tool definitions guard=%q, want empty", got)
	}
}

func TestWSPhaseFConciseChatInjectsOnlyChatHint(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.ConciseChatText = "answer tight but keep important details"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	cfg.Compression.OutputReduce.ConciseChatMinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "concise-chat-session",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "What is the current status?",
			}},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !replace {
		t.Fatal("expected concise chat request mutation")
	}
	body := string(env.Body)
	if !strings.Contains(body, "answer tight but keep important details") {
		t.Fatalf("concise chat hint missing: %s", body)
	}
	if strings.Contains(body, "#slimference-output-rules") || strings.Contains(body, `"role":"system"`) {
		t.Fatalf("concise chat must not use generic directive/system input: %s", body)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 1 || snap.LastReason != "applied" || snap.LastAddedTokens == 0 {
		t.Fatalf("output-reduce tracker = %+v, want concise chat injection", snap)
	}
}

func TestWSPhaseFConciseChatSkipsSemanticallyEmptyUserPrompt(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.ConciseChatText = "answer tight but keep important details"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	cfg.Compression.OutputReduce.ConciseChatMinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "concise-chat-empty-user-session",
			"instructions":    strings.Repeat("stable project instruction ", 2200),
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "   ",
			}},
			"stream": true,
		},
	})
	original := append([]byte(nil), env.Body...)

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace || !bytes.Equal(original, env.Body) {
		t.Fatalf("semantically empty user prompt must stay byte-equal: %s", env.Body)
	}
	if strings.Contains(string(env.Body), "answer tight but keep important details") {
		t.Fatalf("semantically empty user prompt must not receive concise-chat hint: %s", env.Body)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 0 {
		t.Fatalf("semantically empty user prompt should not be an output-reduce candidate: %+v", snap)
	}
}

func TestWSPhaseFOutputReduceSkipsExactReply(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "output-reduce-exact-session",
			"instructions":    strings.Repeat("stable project instruction ", 2200),
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Reply only: EXACT_DONE",
			}},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatalf("exact reply must remain byte-equal, got body: %s", env.Body)
	}
	if strings.Contains(string(env.Body), "#slimference-output-rules") {
		t.Fatalf("exact reply must not receive output-reduce instructions: %s", env.Body)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 1 || snap.LastReason != "exact_reply" {
		t.Fatalf("output-reduce tracker = %+v, want one exact_reply skip", snap)
	}
}

func TestWSPhaseFOutputReduceSkipsToolOutputDelta(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "output-reduce-tool-output-session",
			"instructions":    strings.Repeat("stable project instruction ", 2200),
			"input": []map[string]any{{
				"type":    "function_call_output",
				"call_id": "call_large",
				"output":  strings.Repeat("large terminal line with details\n", 800),
			}},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatalf("tool-output deltas must not receive output-reduce instructions: %s", env.Body)
	}
	if strings.Contains(string(env.Body), "#slimference-output-rules") {
		t.Fatalf("tool-output delta must not receive output-reduce instructions: %s", env.Body)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 0 {
		t.Fatalf("tool-output delta should not be counted as an output-reduce candidate: %+v", snap)
	}
}

func TestWSPhaseFOutputReduceSkipsResponseItemToolOutput(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"conversation_id":      "output-reduce-response-item-tool-output-session",
			"previous_response_id": "resp_tool_turn",
			"instructions":         strings.Repeat("stable project instruction ", 2200),
			"input": []map[string]any{
				{
					"type":    "message",
					"role":    "user",
					"content": "continue from the tool output",
				},
				{
					"type": "response_item",
					"payload": map[string]any{
						"type":    "function_call_output",
						"call_id": "call_status",
						"output":  "M internal/proxy/wsmitm_phasef.go\n",
					},
				},
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatalf("response_item tool-output turn must not receive output-reduce instructions: %s", env.Body)
	}
	if strings.Contains(string(env.Body), "#slimference-output-rules") {
		t.Fatalf("response_item tool-output turn must not receive output-reduce instructions: %s", env.Body)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 0 {
		t.Fatalf("response_item tool-output turn should not be counted as an output-reduce candidate: %+v", snap)
	}
}

func TestWSPhaseFOutputReduceSkipsLayer0CompactedResponseItemToolOutput(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var status strings.Builder
	for i := 0; i < 140; i++ {
		status.WriteString("?? synthetic_layer0_output_reduce_guard_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"conversation_id":      "output-reduce-layer0-tool-output-session",
			"previous_response_id": "resp_tool_turn",
			"instructions":         strings.Repeat("stable project instruction ", 2200),
			"input": []map[string]any{
				{
					"type":    "message",
					"role":    "user",
					"content": "check the project status",
				},
				{
					"type": "response_item",
					"payload": map[string]any{
						"type":      "function_call",
						"call_id":   "call_status",
						"name":      "exec_command",
						"arguments": map[string]any{"cmd": "git status --short"},
					},
				},
				{
					"type": "response_item",
					"payload": map[string]any{
						"type":    "function_call_output",
						"call_id": "call_status",
						"output":  status.String(),
					},
				},
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !replace {
		t.Fatalf("previous-response state-safe tool-output turn should compact before output-reduce: %s", env.Body)
	}
	body := string(env.Body)
	if !strings.Contains(body, "[git status]") || strings.Contains(body, "synthetic_layer0_output_reduce_guard_139.go") {
		t.Fatalf("response_item tool output did not compact safely: %s", body)
	}
	if strings.Contains(body, "#slimference-output-rules") {
		t.Fatalf("tool-output turn must not receive output-reduce instructions: %s", body)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("expected one debug summary, got %d", len(summaries))
	}
	if summaries[0].BypassReason != "" ||
		summaries[0].Tokens.Saved <= 0 ||
		summaries[0].OutputReduce.Applied ||
		summaries[0].OutputReduce.Reason != "disabled" {
		t.Fatalf("previous-response tool-output summary must be savings-positive without output-reduce: %+v", summaries[0])
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 0 {
		t.Fatalf("Layer-0-compacted tool output should not be counted as an output-reduce candidate: %+v", snap)
	}
}

func TestWSPhaseFOutputReduceSkipsEmptyInput(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.Profile = "codex_aggressive"
	cfg.Compression.OutputReduce.MinInputTokens = 1
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "output-reduce-empty-input-session",
			"instructions":    strings.Repeat("stable project instruction ", 2200),
			"input":           []map[string]any{},
			"stream":          true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatalf("empty input must not receive output-reduce instructions: %s", env.Body)
	}
	if strings.Contains(string(env.Body), "#slimference-output-rules") {
		t.Fatalf("empty input must not receive output-reduce instructions: %s", env.Body)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 0 {
		t.Fatalf("empty input should not be counted as an output-reduce candidate: %+v", snap)
	}
}

func TestWSPhaseFRecordsUpstreamInvalidRequestError(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.mu.Lock()
	adapter.sessionID = "codex-wss:error-session"
	adapter.mu.Unlock()

	env := parseWSJSON(t, map[string]any{
		"type":   string(wsmitm.FrameKindError),
		"status": 400,
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": "Invalid request",
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatal("upstream error frames must stay byte-equal")
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("expected one upstream-error summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.SessionID != "codex-wss:error-session" || summary.RouteMode != "websocket_phasef" || summary.BypassReason != "upstream_error" {
		t.Fatalf("bad upstream-error summary identity: %+v", summary)
	}
	if len(summary.Errors) != 1 ||
		!strings.Contains(summary.Errors[0], "status=400") ||
		!strings.Contains(summary.Errors[0], "type=invalid_request_error") ||
		!strings.Contains(summary.Errors[0], "message=Invalid request") {
		t.Fatalf("upstream error details not recorded: %+v", summary.Errors)
	}
}

func TestWSPhaseFRecoveryRetriesInvalidRequestWithFullContext(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var retryPayloads [][]byte
	adapter.setRecoveryWriter(func(payload []byte) error {
		retryPayloads = append(retryPayloads, append([]byte(nil), payload...))
		return nil
	})

	first := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model": "gpt-5.5",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-recovery","source":"desktop"}`,
			},
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "first prompt",
			}},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &first); err != nil || replace {
		t.Fatalf("first request replace=%v err=%v", replace, err)
	}
	firstDone := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{
			"id": "resp-recovery-1",
			"output": []map[string]any{{
				"type":    "message",
				"role":    "assistant",
				"content": "first answer",
			}},
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &firstDone); err != nil || replace {
		t.Fatalf("first completion replace=%v err=%v", replace, err)
	}

	second := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5.5",
			"previous_response_id": "resp-recovery-1",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-recovery","source":"desktop"}`,
			},
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "second prompt",
			}},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &second); err != nil || replace {
		t.Fatalf("second request replace=%v err=%v", replace, err)
	}
	upstreamErr := parseWSJSON(t, map[string]any{
		"type":   string(wsmitm.FrameKindError),
		"status": 400,
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": "Invalid request",
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &upstreamErr)
	if !errors.Is(err, wsmitm.ErrFrameConsumed) || replace {
		t.Fatalf("invalid request should be consumed for recovery, replace=%v err=%v", replace, err)
	}
	if len(retryPayloads) != 1 {
		t.Fatalf("retry payloads=%d want 1", len(retryPayloads))
	}
	retryEnv, parseErr := wsmitm.Parse(retryPayloads[0])
	if parseErr != nil {
		t.Fatalf("parse retry payload: %v", parseErr)
	}
	retryBody, _, ok := wsRequestBody(&retryEnv)
	if !ok {
		t.Fatalf("retry payload has no request body: %s", retryPayloads[0])
	}
	var retry map[string]json.RawMessage
	if err := json.Unmarshal(retryBody, &retry); err != nil {
		t.Fatalf("retry body json: %v", err)
	}
	if _, exists := retry["previous_response_id"]; exists {
		t.Fatalf("retry must remove previous_response_id: %s", retryBody)
	}
	var input []json.RawMessage
	if err := json.Unmarshal(retry["input"], &input); err != nil {
		t.Fatalf("retry input json: %v", err)
	}
	if len(input) != 3 {
		t.Fatalf("retry input len=%d want full chain of 3: %s", len(input), retryBody)
	}

	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].BypassReason != "wss_upstream_recovery_retry" {
		t.Fatalf("missing recovery retry summary: %+v", summaries)
	}
	if summaries[0].DebugFacts["wss.recovery.chain_items"] != "2" ||
		summaries[0].DebugFacts["wss.recovery.current_input_items"] != "1" {
		t.Fatalf("bad recovery facts: %+v", summaries[0].DebugFacts)
	}
	recoveryID := summaries[0].DebugFacts["wss.recovery.id"]
	if recoveryID == "" || summaries[0].DebugFacts["wss.recovery.phase"] != "retry_sent" {
		t.Fatalf("retry summary should carry recovery id and phase: %+v", summaries[0].DebugFacts)
	}

	secondCreated := parseWSJSON(t, map[string]any{
		"type":     string(wsmitm.FrameKindResponseCreated),
		"response": map[string]any{"id": "resp-recovery-2"},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &secondCreated); err != nil || replace {
		t.Fatalf("second created replace=%v err=%v", replace, err)
	}
	summaries = p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].BypassReason != "wss_upstream_recovery_accepted" {
		t.Fatalf("missing recovery accepted summary: %+v", summaries)
	}
	if summaries[0].DebugFacts["wss.recovery.id"] != recoveryID ||
		summaries[0].DebugFacts["wss.recovery.phase"] != "accepted" ||
		summaries[0].DebugFacts["wss.recovery.response_id"] != "resp-recovery-2" {
		t.Fatalf("bad accepted facts: %+v", summaries[0].DebugFacts)
	}

	secondDone := parseWSJSON(t, map[string]any{
		"type":     string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{"id": "resp-recovery-2"},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &secondDone); err != nil || replace {
		t.Fatalf("second completion replace=%v err=%v", replace, err)
	}
	summaries = p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].BypassReason != "wss_upstream_recovery_succeeded" {
		t.Fatalf("missing recovery success summary: %+v", summaries)
	}
	if summaries[0].DebugFacts["wss.recovery.id"] != recoveryID ||
		summaries[0].DebugFacts["wss.recovery.phase"] != "completed" ||
		summaries[0].DebugFacts["wss.recovery.accepted"] != "true" ||
		summaries[0].DebugFacts["wss.recovery.response_id"] != "resp-recovery-2" {
		t.Fatalf("bad recovery success facts: %+v", summaries[0].DebugFacts)
	}
}

func TestWSPhaseFRecoveryChainsFullHistoryWhenPreviousChainMissing(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var retryPayloads [][]byte
	adapter.setRecoveryWriter(func(payload []byte) error {
		retryPayloads = append(retryPayloads, append([]byte(nil), payload...))
		return nil
	})

	fullHistory := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5.5",
			"previous_response_id": "resp-missing-local-chain",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-full-history-recovery","source":"desktop"}`,
			},
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "first prompt"},
				{"type": "function_call", "call_id": "call_old", "name": "exec_command", "arguments": `{"cmd":"cat src/a.txt"}`},
				{"type": "function_call_output", "call_id": "call_old", "output": "old output"},
			},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &fullHistory); err != nil || replace {
		t.Fatalf("full-history request replace=%v err=%v", replace, err)
	}

	itemDone := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseOutputItemDone),
		"item": map[string]any{
			"type":      "function_call",
			"call_id":   "call_next",
			"name":      "exec_command",
			"arguments": `{"cmd":"cat src/b.txt"}`,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &itemDone); err != nil || replace {
		t.Fatalf("output item replace=%v err=%v", replace, err)
	}
	fullHistoryDone := parseWSJSON(t, map[string]any{
		"type":     string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{"id": "resp-full-history-new", "output": []any{}},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &fullHistoryDone); err != nil || replace {
		t.Fatalf("full-history completion replace=%v err=%v", replace, err)
	}

	delta := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5.5",
			"previous_response_id": "resp-full-history-new",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-full-history-recovery","source":"desktop"}`,
			},
			"input": []map[string]any{{
				"type":    "function_call_output",
				"call_id": "call_next",
				"output":  "new output",
			}},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &delta); err != nil || replace {
		t.Fatalf("delta request replace=%v err=%v", replace, err)
	}
	upstreamErr := parseWSJSON(t, map[string]any{
		"type":   string(wsmitm.FrameKindError),
		"status": 400,
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": "Invalid request",
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &upstreamErr)
	if !errors.Is(err, wsmitm.ErrFrameConsumed) || replace {
		t.Fatalf("invalid request should be consumed for recovery, replace=%v err=%v", replace, err)
	}
	if len(retryPayloads) != 1 {
		t.Fatalf("retry payloads=%d want 1", len(retryPayloads))
	}
	retryEnv, parseErr := wsmitm.Parse(retryPayloads[0])
	if parseErr != nil {
		t.Fatalf("parse retry payload: %v", parseErr)
	}
	retryBody, _, ok := wsRequestBody(&retryEnv)
	if !ok {
		t.Fatalf("retry payload has no request body: %s", retryPayloads[0])
	}
	var retry map[string]json.RawMessage
	if err := json.Unmarshal(retryBody, &retry); err != nil {
		t.Fatalf("retry body json: %v", err)
	}
	if _, exists := retry["previous_response_id"]; exists {
		t.Fatalf("retry must remove previous_response_id: %s", retryBody)
	}
	var input []json.RawMessage
	if err := json.Unmarshal(retry["input"], &input); err != nil {
		t.Fatalf("retry input json: %v", err)
	}
	if len(input) != 5 {
		t.Fatalf("retry input len=%d want full-history chain plus current output: %s", len(input), retryBody)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].BypassReason != "wss_upstream_recovery_retry" {
		t.Fatalf("missing recovery retry summary: %+v", summaries)
	}
	if summaries[0].DebugFacts["wss.recovery.chain_items"] != "4" ||
		summaries[0].DebugFacts["wss.recovery.current_input_items"] != "1" {
		t.Fatalf("bad recovery facts: %+v", summaries[0].DebugFacts)
	}
}

func TestWSPhaseFToolPruneRecoveryRetriesFullSchemaAndMarksCooldown(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune-recovery"
	p.toolPrune.ObserveTurn(sessionID, []string{"Bash", "ColdTool"})
	p.toolPrune.ObserveTurn(sessionID, []string{"Bash"})

	var retryPayloads [][]byte
	adapter.setRecoveryWriter(func(payload []byte) error {
		retryPayloads = append(retryPayloads, append([]byte(nil), payload...))
		return nil
	})

	req := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "wss-tool-prune-recovery",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Continue with the available tools.",
			}},
			"tools": []map[string]any{
				codexToolDefinition("Bash", "Run a shell command"),
				codexToolDefinition("ColdTool", strings.Repeat("Idle expensive schema. ", 80)),
			},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &req); err != nil || !replace {
		t.Fatalf("tool-prune request replace=%v err=%v", replace, err)
	}
	if body := string(req.Body); strings.Contains(body, "ColdTool") || !strings.Contains(body, "Bash") {
		t.Fatalf("request should prune only ColdTool before upstream: %s", body)
	}

	upstreamErr := parseWSJSON(t, map[string]any{
		"type":   string(wsmitm.FrameKindError),
		"status": 400,
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": "unknown tool ColdTool",
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &upstreamErr)
	if !errors.Is(err, wsmitm.ErrFrameConsumed) || replace {
		t.Fatalf("missing-tool invalid request should be consumed for full-schema retry, replace=%v err=%v", replace, err)
	}
	if len(retryPayloads) != 1 {
		t.Fatalf("retry payloads=%d want 1", len(retryPayloads))
	}
	retryEnv, parseErr := wsmitm.Parse(retryPayloads[0])
	if parseErr != nil {
		t.Fatalf("parse retry payload: %v", parseErr)
	}
	retryBody, _, ok := wsRequestBody(&retryEnv)
	if !ok {
		t.Fatalf("retry payload has no request body: %s", retryPayloads[0])
	}
	if body := string(retryBody); !strings.Contains(body, "ColdTool") || !strings.Contains(body, "Bash") {
		t.Fatalf("tool-prune recovery must retry with full tool schema: %s", body)
	}
	snap := p.toolPrune.Snapshot()
	if snap.MissTotal != 1 || snap.RetryTotal != 1 || snap.DisabledSessions != 1 {
		t.Fatalf("tool-prune recovery must mark miss/retry/cooldown: %+v", snap)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].BypassReason != "wss_upstream_recovery_retry" {
		t.Fatalf("missing tool-prune recovery summary: %+v", summaries)
	}
	if summaries[0].DebugFacts["wss.recovery.tool_prune_applied"] != "true" ||
		summaries[0].DebugFacts["wss.recovery.tool_prune_missing_tool"] != "true" ||
		summaries[0].DebugFacts["wss.recovery.tool_prune_pruned"] != "1" {
		t.Fatalf("bad tool-prune recovery facts: %+v", summaries[0].DebugFacts)
	}
}

func TestWSPhaseFRecoverySuccessDoesNotRequireCompletedResponseID(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setRecoveryWriter(func([]byte) error { return nil })

	adapter.pendingRecovery = &wssRecoveryCandidate{
		SessionID:          "thread-no-id",
		PreviousResponseID: "resp-prev",
		Model:              "gpt-5.5",
		RetryPayload:       []byte(`{"type":"request","body":{"input":[]}}`),
		RetryBody:          []byte(`{"input":[]}`),
		ChainItems:         2,
		CurrentInputItems:  1,
	}
	errEnv := parseWSJSON(t, map[string]any{
		"type":   string(wsmitm.FrameKindError),
		"status": 400,
		"error":  map[string]any{"type": "invalid_request_error", "message": "Invalid request"},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &errEnv); !errors.Is(err, wsmitm.ErrFrameConsumed) || replace {
		t.Fatalf("invalid request should be consumed for recovery, replace=%v err=%v", replace, err)
	}

	done := parseWSJSON(t, map[string]any{"type": string(wsmitm.FrameKindResponseCompleted)})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &done); err != nil || replace {
		t.Fatalf("completion without id replace=%v err=%v", replace, err)
	}
	summaries := p.DebugRecorder().Last(2, false)
	if len(summaries) != 2 ||
		summaries[0].BypassReason != "wss_upstream_recovery_succeeded" ||
		summaries[1].BypassReason != "wss_upstream_recovery_accepted" {
		t.Fatalf("completion without response id should still mark accepted+succeeded: %+v", summaries)
	}
	if summaries[0].DebugFacts["wss.recovery.phase"] != "completed" ||
		summaries[0].DebugFacts["wss.recovery.accepted"] != "true" {
		t.Fatalf("bad completion-without-id success facts: %+v", summaries[0].DebugFacts)
	}
}

func TestWSPhaseFRecoveryFailureIsLoggedWhenRetryIsRejected(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setRecoveryWriter(func([]byte) error { return nil })
	adapter.pendingRecovery = &wssRecoveryCandidate{
		SessionID:          "thread-retry-fail",
		PreviousResponseID: "resp-prev",
		Model:              "gpt-5.5",
		RetryPayload:       []byte(`{"type":"request","body":{"input":[]}}`),
		RetryBody:          []byte(`{"input":[]}`),
		ChainItems:         2,
		CurrentInputItems:  1,
	}
	firstErr := parseWSJSON(t, map[string]any{
		"type":   string(wsmitm.FrameKindError),
		"status": 400,
		"error":  map[string]any{"type": "invalid_request_error", "message": "Invalid request"},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &firstErr); !errors.Is(err, wsmitm.ErrFrameConsumed) || replace {
		t.Fatalf("first invalid request should be consumed for recovery, replace=%v err=%v", replace, err)
	}
	secondErr := parseWSJSON(t, map[string]any{
		"type":   string(wsmitm.FrameKindError),
		"status": 400,
		"error":  map[string]any{"type": "invalid_request_error", "message": "Invalid request"},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &secondErr); err != nil || replace {
		t.Fatalf("rejected recovery retry should be forwarded, replace=%v err=%v", replace, err)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].BypassReason != "wss_upstream_recovery_failed" {
		t.Fatalf("missing recovery failure summary: %+v", summaries)
	}
	if summaries[0].DebugFacts["wss.recovery.phase"] != "upstream_rejected_retry" ||
		summaries[0].DebugFacts["wss.recovery.error_status"] != "400" {
		t.Fatalf("bad recovery failure facts: %+v", summaries[0].DebugFacts)
	}
}

func TestWSPhaseFRecoveryDoesNotRetryContextWindowErrors(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setRecoveryWriter(func([]byte) error {
		t.Fatal("context-window errors must not retry")
		return nil
	})

	errEnv := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindError),
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": "Your input exceeds the context window of this model. Please adjust your input and try again.",
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &errEnv)
	if err != nil || replace {
		t.Fatalf("context-window error replace=%v err=%v", replace, err)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].BypassReason != "upstream_error" {
		t.Fatalf("context-window error should be recorded normally: %+v", summaries)
	}
	if summaries[0].DebugFacts["wss.recovery.retryable"] != "false" ||
		summaries[0].DebugFacts["wss.recovery.no_retry_reason"] != "not_retryable" {
		t.Fatalf("context-window error should explain why recovery did not retry: %+v", summaries[0].DebugFacts)
	}
}

func TestWSPhaseFPreviousResponseSourceToolOutputFullPasses(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	sourceOutput := strings.Repeat("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"slimference\") }\n", 240)
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp_source_guard",
		"prompt_cache_key":     "source-guard-session",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "read-source",
			"output":  sourceOutput,
		}},
		"stream": true,
	})

	if replace := adapter.handleRequest(&env); replace {
		t.Fatalf("source-like tool output after previous_response_id must full-pass: %s", env.Body)
	}
	if !bytes.Contains(env.Raw, []byte("package main")) || bytes.Contains(env.Raw, []byte("local-archive://")) {
		t.Fatalf("source-like tool output was not preserved: %s", env.Raw)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("expected one debug summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.BypassReason != "wss_previous_response_tool_output_full_pass" ||
		summary.Tokens.Saved != 0 ||
		summary.MessagesCompressed != 0 {
		t.Fatalf("source guard summary should be a no-savings full-pass: %+v", summary)
	}
	if summary.DebugFacts["wss.previous_response_id"] != "true" ||
		summary.DebugFacts["wss.request_shape"] != "delta" ||
		summary.DebugFacts["wss.delta_shape"] != "true" ||
		summary.DebugFacts["wss.source_tool_results"] != "1" ||
		summary.DebugFacts["wss.bypass_reason"] != "wss_previous_response_tool_output_full_pass" {
		t.Fatalf("source guard facts missing: %+v", summary.DebugFacts)
	}
	if summary.Plan == nil || hasPlanAction(summary.Plan.Decisions, "websocket", "mutate", "known_shape_and_high_corpus_confidence") {
		t.Fatalf("source guard must not request websocket mutation: %+v", summary.Plan)
	}
}

func TestWSPhaseFRequestShapeFactsClassifyFullHistory(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			Content: []types.ContentBlock{{
				Type:      "tool_use",
				ToolUseID: "call_read",
				ToolName:  "exec_command",
				ToolInput: `{"cmd":"cat lib/alpha.go"}`,
			}},
		},
		{
			Role: "tool",
			Content: []types.ContentBlock{{
				Type:         "tool_result",
				ToolResultID: "call_read",
				Text:         "package lib\n",
			}},
		},
	}
	meta := wssRequestMeta{PreviousResponseID: "resp-full-history", SessionID: "session-full-history"}
	if wssRequestIsDeltaShape(messages) {
		t.Fatal("full-history messages with assistant tool_use must not classify as delta")
	}
	facts := wssRequestDebugFacts([]byte(`{}`), []byte(`{}`), messages, proxyLayer0Stats{
		StaleReadBlocks:          1,
		StaleReadTokensSaved:     11,
		ObsoletePruneBlocks:      2,
		ObsoletePruneTokensSaved: 22,
	}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.request_shape"] != "full_history" || facts["wss.delta_shape"] != "false" {
		t.Fatalf("bad request-shape facts: %+v", facts)
	}
	if facts["wss.stale_read_blocks"] != "1" || facts["wss.obsolete_prune_tokens"] != "22" {
		t.Fatalf("history reducer facts missing: %+v", facts)
	}
	rootFacts := wssRequestDebugFacts([]byte(`{}`), []byte(`{}`), messages, proxyLayer0Stats{}, false, "", wssRequestMeta{}, outputreduce.Stats{Reason: "disabled"})
	if rootFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("history resend without previous_response_id should classify as full_history: %+v", rootFacts)
	}
	initialMessages := []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{{
			Type: "text",
			Text: "start",
		}},
	}}
	initialFacts := wssRequestDebugFacts([]byte(`{}`), []byte(`{}`), initialMessages, proxyLayer0Stats{}, false, "", wssRequestMeta{}, outputreduce.Stats{Reason: "disabled"})
	if initialFacts["wss.request_shape"] != "root" {
		t.Fatalf("initial request without history should classify as root: %+v", initialFacts)
	}
}

func TestWSPhaseFPreviousResponseToolOutputGuardRequiresKnownToolOutput(t *testing.T) {
	meta := wssRequestMeta{PreviousResponseID: "resp_source_guard"}
	smallSource := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type: "tool_result",
			Text: "package main\nfunc main() {}\n",
		}},
	}}
	if !wssPreviousResponseUnknownToolOutputFullPass(meta, messagesContainToolResult(smallSource), false, false) {
		t.Fatal("unknown small tool-result continuations after previous_response_id must full-pass")
	}

	largeSource := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type: "tool_result",
			Text: strings.Repeat("package main\nfunc main() {}\n", 220),
		}},
	}}
	if !wssPreviousResponseUnknownToolOutputFullPass(meta, messagesContainToolResult(largeSource), false, false) {
		t.Fatal("unknown large tool-result continuations after previous_response_id must full-pass")
	}
	if wssPreviousResponseUnknownToolOutputFullPass(wssRequestMeta{}, true, false, false) {
		t.Fatal("previous_response_id-specific guard must not cover non-continuation tool output")
	}
	statusMessages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_status",
			Text:         "?? safe-status.go\n",
		}},
	}}
	remembered := map[string]types.ContentBlock{
		"call_status": {
			Type:      "tool_use",
			ToolUseID: "call_status",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"git status --short"}`,
		},
	}
	if !wssStatefulToolOutputMutationSafe(meta, true, statusMessages, remembered) {
		t.Fatal("server-known compact git status output should be safe to mutate")
	}
	if wssPreviousResponseUnknownToolOutputFullPass(meta, true, true, true) {
		t.Fatal("safe stateful tool output should not be forced through previous_response_id full-pass")
	}
	if wssPreviousResponseUnknownToolOutputFullPass(meta, true, false, true) {
		t.Fatal("known non-status tool output should keep exact/recoverable reducers available")
	}
	total, resolved, inferred := wssToolOutputResolutionStats(statusMessages, remembered)
	if total != 1 || resolved != 1 || inferred != 0 {
		t.Fatalf("known status tool output should resolve via metadata, total=%d resolved=%d inferred=%d", total, resolved, inferred)
	}
}

func TestWSPhaseFToolOutputStateGuardAllowsStateSafeDefaultWSSMutation(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	output := strings.Repeat("?? state_guard_file.go\n", 240)
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "state-guard-session",
			"input": []map[string]any{
				{"type": "function_call", "call_id": "call_status", "name": "exec_command", "arguments": map[string]any{"cmd": "git status --short"}},
				{"type": "function_call_output", "call_id": "call_status", "output": output},
			},
			"stream": true,
		},
	})

	if replace := adapter.handleRequest(&env); !replace {
		t.Fatalf("default Codex WSS state-safe tool-output request should compact: %s", env.Body)
	}
	if !strings.Contains(string(env.Body), "[git status]") || strings.Contains(string(env.Body), "state_guard_file.go") {
		t.Fatalf("tool output was not compacted safely: %s", env.Body)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("expected one debug summary, got %d", len(summaries))
	}
	if summaries[0].BypassReason != "" ||
		summaries[0].Tokens.Saved <= 0 ||
		summaries[0].DebugFacts["wss.bypass_reason"] != "" {
		t.Fatalf("state-safe summary should report positive savings without bypass: %+v", summaries[0])
	}
}

func TestWSPhaseFProviderCacheBustDetectorDemotesPreviousMutatedMechanism(t *testing.T) {
	adapter := (&PhaseFDispatcher{Proxy: New(config.Defaults())}).newWSPhaseFAdapter()
	sessionID := "codex-wss:cache-bust-detector"
	readDelta := proxyLayer0MechanismMaskFor(proxyLayer0MechanismReadDelta)

	if event := adapter.observeWSSProviderCacheBust(sessionID, 1000, 820, 0); event.Fired {
		t.Fatalf("first sample must not fire cache-bust guard: %+v", event)
	}
	if event := adapter.observeWSSProviderCacheBust(sessionID, 1000, 810, readDelta); event.Fired {
		t.Fatalf("warm previous mutated sample must not fire before next usage frame: %+v", event)
	}
	event := adapter.observeWSSProviderCacheBust(sessionID, 1000, 470, 0)
	if !event.Fired || event.Trigger != readDelta || !event.Demoted.Has(proxyLayer0MechanismReadDelta) {
		t.Fatalf("cache-bust guard must demote the previous mutated mechanism exactly: %+v", event)
	}
	if got := adapter.wssCacheBustDemotedMechanisms(sessionID); got != readDelta {
		t.Fatalf("demoted mechanism mask=%q, want %q", got.String(), readDelta.String())
	}
}

func TestWSPhaseFProviderCacheBustDetectorIgnoresWarmupAndUnmutatedDrops(t *testing.T) {
	adapter := (&PhaseFDispatcher{Proxy: New(config.Defaults())}).newWSPhaseFAdapter()
	sessionID := "codex-wss:cache-bust-warmup"
	repeatedOutput := proxyLayer0MechanismMaskFor(proxyLayer0MechanismRepeatedOut)

	if event := adapter.observeWSSProviderCacheBust(sessionID, 1000, 820, repeatedOutput); event.Fired {
		t.Fatalf("first mutated warmup sample must not fire: %+v", event)
	}
	if event := adapter.observeWSSProviderCacheBust(sessionID, 1000, 430, 0); event.Fired {
		t.Fatalf("second sample is still warmup and must not demote: %+v", event)
	}
	if got := adapter.wssCacheBustDemotedMechanisms(sessionID); got != 0 {
		t.Fatalf("warmup demoted unexpectedly: %q", got.String())
	}

	plainSessionID := "codex-wss:cache-bust-unmutated"
	adapter.observeWSSProviderCacheBust(plainSessionID, 1000, 820, 0)
	adapter.observeWSSProviderCacheBust(plainSessionID, 1000, 810, 0)
	if event := adapter.observeWSSProviderCacheBust(plainSessionID, 1000, 450, 0); event.Fired {
		t.Fatalf("drop after unmutated turn must not demote: %+v", event)
	}
	if got := adapter.wssCacheBustDemotedMechanisms(plainSessionID); got != 0 {
		t.Fatalf("unmutated drop demoted unexpectedly: %q", got.String())
	}
}

func TestWSPhaseFCacheBustDemotionFullPassesMatchingLayer0Mechanism(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	sessionID := "codex-wss:cache-bust-guard-session"
	adapter.mu.Lock()
	adapter.cacheBustSessions = map[string]*wssProviderCacheBustSession{
		sessionID: {demoted: proxyLayer0MechanismMaskFor(proxyLayer0MechanismCapturedOut)},
	}
	adapter.mu.Unlock()

	output := strings.Repeat("?? cache_bust_guard_file.go\n", 240)
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "cache-bust-guard-session",
			"input": []map[string]any{
				{"type": "function_call", "call_id": "call_status", "name": "exec_command", "arguments": map[string]any{"cmd": "git status --short"}},
				{"type": "function_call_output", "call_id": "call_status", "output": output},
			},
			"stream": true,
		},
	})

	if replace := adapter.handleRequest(&env); replace {
		t.Fatalf("cache-bust-demoted captured output must full-pass: %s", env.Body)
	}
	if !strings.Contains(string(env.Body), "cache_bust_guard_file.go") || strings.Contains(string(env.Body), "[git status]") {
		t.Fatalf("cache-bust guard did not preserve original tool output: %s", env.Body)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("expected one debug summary, got %d", len(summaries))
	}
	if summaries[0].Tokens.Saved != 0 || summaries[0].DebugFacts["wss.cache_bust_demoted_mechanisms"] != "captured_output" {
		t.Fatalf("cache-bust full-pass summary should report demotion without savings: %+v", summaries[0])
	}
	guarded := false
	for _, decision := range summaries[0].EvidenceDecisions {
		if decision.Reason == "cache_bust_guard" {
			guarded = true
		}
	}
	if !guarded {
		t.Fatalf("cache-bust full-pass must carry evidence reason: %+v", summaries[0].EvidenceDecisions)
	}
}

func TestWSPhaseFCacheBustDemotionFullPassesHistoryMechanisms(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	sessionID := "codex-wss:single-reconstruct-session"
	adapter.mu.Lock()
	adapter.cacheBustSessions = map[string]*wssProviderCacheBustSession{
		sessionID: {demoted: proxyLayer0MechanismMaskFor(proxyLayer0MechanismStaleRead) | proxyLayer0MechanismMaskFor(proxyLayer0MechanismObsoletePrune)},
	}
	adapter.mu.Unlock()

	body := codexWSStaleObsoleteLayer0Body()

	mutated, _, _, stats, _ := adapter.applyInputPipeline(body)
	mutatedText := string(mutated)
	if !strings.Contains(mutatedText, "stale x content") || strings.Contains(mutatedText, "kind=stale-read") {
		t.Fatalf("cache-bust-demoted stale-read must full-pass original content: %s", mutatedText)
	}
	if !strings.Contains(mutatedText, "obsolete y content") || strings.Contains(mutatedText, "kind=obsolete-read") {
		t.Fatalf("cache-bust-demoted obsolete-prune must full-pass original content: %s", mutatedText)
	}
	if stats.StaleReadBlocks != 0 || stats.ObsoletePruneBlocks != 0 {
		t.Fatalf("guarded history mechanisms must not count as applied stats: %+v", stats)
	}
	if !hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismStaleRead, "cache_bust_guard", evidence.ActionFullPass) ||
		!hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismObsoletePrune, "cache_bust_guard", evidence.ActionFullPass) {
		t.Fatalf("guarded history mechanisms must emit cache-bust evidence: %+v", stats.EvidenceDecisions)
	}
}

func TestWSPhaseFHistoryMutationsAreByteDeterministicAcrossReconnect(t *testing.T) {
	sharedChunk := strings.Repeat("deterministic chunk shared region with cache stable bytes\n", 1000)
	readOutput := strings.Repeat("deterministic read line with stable content\n", 120)
	reportOutput := strings.Repeat("deterministic report row with unchanged non-file data\n", 100)
	statusOutput := strings.Repeat("?? deterministic_status_file.go\n", 180)
	envelopeOutput := deterministicCodexExecEnvelope("deterministic-envelope", false)
	cases := []struct {
		name        string
		sessionID   string
		configure   func(*config.Config)
		seedBodies  [][]byte
		body        []byte
		assertStats func(*testing.T, proxyLayer0Stats)
		assertBody  func(*testing.T, []byte)
	}{
		{
			name:      "read_delta",
			sessionID: "codex-wss:deterministic-read-delta",
			seedBodies: [][]byte{wssToolOutputBody("deterministic-read-delta", "call_read_seed", "read_file",
				map[string]any{"path": "src/deterministic.go"}, readOutput)},
			body: wssToolOutputBody("deterministic-read-delta", "call_read_candidate", "read_file",
				map[string]any{"path": "src/deterministic.go"}, readOutput),
			assertStats: func(t *testing.T, stats proxyLayer0Stats) {
				t.Helper()
				if stats.ReadDeltaBlocks != 1 || stats.TokensSaved <= 0 {
					t.Fatalf("read_delta did not fire deterministically: %+v", stats)
				}
			},
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()
				if !bytes.Contains(body, []byte("kind=file-read")) || !bytes.Contains(body, []byte("archive=local-archive://")) {
					t.Fatalf("read_delta body missing recoverable marker: %s", body)
				}
			},
		},
		{
			name:      "repeated_output",
			sessionID: "codex-wss:deterministic-repeated-output",
			seedBodies: [][]byte{wssToolOutputBody("deterministic-repeated-output", "call_report_seed", "exec_command",
				map[string]any{"cmd": "python generate_report.py"}, reportOutput)},
			body: wssToolOutputBody("deterministic-repeated-output", "call_report_candidate", "exec_command",
				map[string]any{"cmd": "python generate_report.py"}, reportOutput),
			assertStats: func(t *testing.T, stats proxyLayer0Stats) {
				t.Helper()
				if stats.RepeatedOutputBlocks != 1 || stats.TokensSaved <= 0 {
					t.Fatalf("repeated_output did not fire deterministically: %+v", stats)
				}
			},
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()
				if !bytes.Contains(body, []byte("kind=tool-output")) || !bytes.Contains(body, []byte("archive=local-archive://")) {
					t.Fatalf("repeated_output body missing recoverable marker: %s", body)
				}
			},
		},
		{
			name:      "chunk_dedup",
			sessionID: "codex-wss:deterministic-chunk-dedup",
			configure: func(cfg *config.Config) {
				cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = true
				cfg.Compression.OutputReduce.CodexChunkDedupEnabled = true
				cfg.Compression.OutputReduce.CodexChunkDedupMinBytes = 0
				cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent = 100
				cfg.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent = 100
			},
			seedBodies: [][]byte{wssToolOutputBody("deterministic-chunk-dedup", "call_chunk_seed", "read_file",
				map[string]any{"path": "src/chunk-a.go"}, sharedChunk+"tail a\n")},
			body: wssToolOutputBody("deterministic-chunk-dedup", "call_chunk_candidate", "read_file",
				map[string]any{"path": "src/chunk-b.go"}, sharedChunk+"tail b\n"),
			assertStats: func(t *testing.T, stats proxyLayer0Stats) {
				t.Helper()
				if stats.ChunkDedupBlocks != 1 || stats.TokensSaved <= 0 {
					t.Fatalf("chunk_dedup did not fire deterministically: %+v", stats)
				}
			},
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()
				if !bytes.Contains(body, []byte("[context-chunk status=unchanged uri=local-archive://")) {
					t.Fatalf("chunk_dedup body missing recoverable chunk reference: %s", body)
				}
			},
		},
		{
			name:      "captured_output",
			sessionID: "codex-wss:deterministic-captured-output",
			body: wssToolOutputBody("deterministic-captured-output", "call_status", "exec_command",
				map[string]any{"cmd": "git status --short"}, statusOutput),
			assertStats: func(t *testing.T, stats proxyLayer0Stats) {
				t.Helper()
				if stats.CapturedOutputBlocks != 1 || stats.TokensSaved <= 0 {
					t.Fatalf("captured_output did not fire deterministically: %+v", stats)
				}
			},
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()
				if !bytes.Contains(body, []byte("[git status]")) || !bytes.Contains(body, []byte("local-archive://")) {
					t.Fatalf("captured_output body missing compact status archive: %s", body)
				}
			},
		},
		{
			name:      "codex_exec_envelope",
			sessionID: "codex-wss:deterministic-codex-envelope",
			body: wssToolOutputBody("deterministic-codex-envelope", "call_tests", "exec_command",
				map[string]any{"cmd": "go test ./..."}, envelopeOutput),
			assertStats: func(t *testing.T, stats proxyLayer0Stats) {
				t.Helper()
				if stats.CodexExecEnvelopeBlocks != 1 || stats.TokensSaved <= 0 {
					t.Fatalf("codex_exec_envelope did not fire deterministically: %+v", stats)
				}
			},
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()
				if !bytes.Contains(body, []byte("SLIMFERENCE_TEST_FAILURE_SENTINEL")) ||
					!bytes.Contains(body, []byte("local-archive://")) ||
					bytes.Contains(body, []byte("TestPassing089")) {
					t.Fatalf("codex_exec_envelope body lost failure detail or failed compaction: %s", body)
				}
			},
		},
		{
			name:      "stale_read_aging",
			sessionID: "codex-wss:deterministic-stale-read",
			configure: func(cfg *config.Config) {
				cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
				cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
			},
			body: codexWSReadBody("Read", strings.Repeat("stale deterministic file content ", 80), "fresh deterministic file content"),
			assertStats: func(t *testing.T, stats proxyLayer0Stats) {
				t.Helper()
				if stats.StaleReadBlocks != 1 || stats.StaleReadTokensSaved <= 0 || stats.BlocksModified != 1 {
					t.Fatalf("stale_read_aging did not emit request-local stats, got %+v", stats)
				}
			},
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()
				if !bytes.Contains(body, []byte("kind=stale-read")) || bytes.Contains(body, []byte("stale deterministic file content")) {
					t.Fatalf("stale_read_aging body not deterministic/pruned: %s", body)
				}
			},
		},
		{
			name:      "obsolete_read_prune",
			sessionID: "codex-wss:deterministic-obsolete-read",
			configure: func(cfg *config.Config) {
				cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
			},
			body: codexWSObsoleteReadBody(strings.Repeat("obsolete deterministic file content ", 80)),
			assertStats: func(t *testing.T, stats proxyLayer0Stats) {
				t.Helper()
				if stats.ObsoletePruneBlocks != 1 || stats.ObsoletePruneTokensSaved <= 0 || stats.BlocksModified != 1 {
					t.Fatalf("obsolete_read_prune did not emit request-local stats, got %+v", stats)
				}
			},
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()
				if !bytes.Contains(body, []byte("kind=obsolete-read")) || bytes.Contains(body, []byte("obsolete deterministic file content")) {
					t.Fatalf("obsolete_read_prune body not deterministic/pruned: %s", body)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first := runWSSDeterminismWorld(t, tc.sessionID, tc.configure, tc.seedBodies, tc.body)
			second := runWSSDeterminismWorld(t, tc.sessionID, tc.configure, tc.seedBodies, tc.body)
			if !first.changed || !second.changed {
				t.Fatalf("determinism fixture must mutate in both worlds, first=%+v second=%+v", first.stats, second.stats)
			}
			if !bytes.Equal(first.body, second.body) {
				t.Fatalf("mutation is not byte-deterministic across reconnect\nfirst:  %s\nsecond: %s", first.body, second.body)
			}
			tc.assertStats(t, first.stats)
			tc.assertStats(t, second.stats)
			tc.assertBody(t, first.body)
			tc.assertBody(t, second.body)
		})
	}
}

type wssDeterminismRun struct {
	body    []byte
	stats   proxyLayer0Stats
	changed bool
}

func runWSSDeterminismWorld(t *testing.T, sessionID string, configure func(*config.Config), seedBodies [][]byte, body []byte) wssDeterminismRun {
	t.Helper()
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	defer func() { proxyUserHomeDir = oldHome }()
	cleanupPhaseFTempHome(t, home, sessionID)
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	if configure != nil {
		configure(cfg)
	}
	p := New(cfg)
	seedAdapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	for _, seedBody := range seedBodies {
		_, _, _, _, _ = seedAdapter.applyInputPipeline(seedBody)
	}
	reconnectedAdapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	mutated, _, changed, stats, _ := reconnectedAdapter.applyInputPipeline(body)
	return wssDeterminismRun{body: mutated, stats: stats, changed: changed}
}

func wssToolOutputBody(promptCacheKey, callID, toolName string, toolInput map[string]any, output string) []byte {
	return mustMarshal(map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": promptCacheKey,
		"input": []map[string]any{
			{"type": "function_call", "call_id": callID, "name": toolName, "arguments": toolInput},
			{"type": "function_call_output", "call_id": callID, "output": output},
		},
		"stream": true,
	})
}

func deterministicCodexExecEnvelope(chunkID string, passing bool) string {
	var payload strings.Builder
	exitCode := "1"
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	if passing {
		exitCode = "0"
		payload.WriteString("PASS\nok  \texample.test/liveproof\t0.015s\n")
	} else {
		payload.WriteString("=== RUN   TestSlimferenceFailure\n")
		payload.WriteString("    fail_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
		payload.WriteString("--- FAIL: TestSlimferenceFailure (0.00s)\n")
		payload.WriteString("FAIL\texample.test/liveproof\t0.015s\n")
	}
	return "Chunk ID: " + chunkID + "\nWall time: 0.0000 seconds\nProcess exited with code " + exitCode + "\nOriginal token count: 10000\nOutput:\n" + payload.String()
}

func TestWSPhaseFUpstreamErrorQuarantinesSessionMutations(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.mu.Lock()
	adapter.sessionID = "codex-wss:quarantine-session"
	adapter.mu.Unlock()

	errorEnv := parseWSJSON(t, map[string]any{
		"type":   string(wsmitm.FrameKindError),
		"status": 400,
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": "Invalid request",
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &errorEnv); err != nil || replace {
		t.Fatalf("error frame replace=%v err=%v", replace, err)
	}

	largeOutput := strings.Repeat("quarantine repeat output line with enough stable body\n", 1800)
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp_after_error",
		"prompt_cache_key":     "quarantine-session",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "after-error",
			"output":  largeOutput,
		}},
		"stream": true,
	})
	if replace := adapter.handleRequest(&env); replace {
		t.Fatalf("degraded WSS session must full-pass until reconnect: %s", env.Body)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("expected one latest request summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.BypassReason != "wss_session_degraded_full_pass" ||
		summary.Tokens.Saved != 0 ||
		summary.DebugFacts["wss.degraded_reason"] == "" {
		t.Fatalf("quarantine summary missing: %+v", summary)
	}
}

func TestWSPhaseFToolPrunePrunesIdleCodexTools(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune"
	p.toolPrune.ObserveTurn(sessionID, []string{"Bash", "ColdTool"})
	p.toolPrune.ObserveTurn(sessionID, []string{"Bash"})

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "wss-tool-prune",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Continue with the available tools.",
			}},
			"tools": []map[string]any{
				codexToolDefinition("Bash", "Run a shell command"),
				codexToolDefinition("ColdTool", strings.Repeat("Idle expensive schema. ", 80)),
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !replace {
		t.Fatal("expected WSS tool-prune mutation")
	}
	body := string(env.Body)
	if strings.Contains(body, "ColdTool") {
		t.Fatalf("idle tool still present after prune: %s", body)
	}
	if !strings.Contains(body, "Bash") {
		t.Fatalf("always-keep tool was removed: %s", body)
	}
	snap := p.toolPrune.Snapshot()
	if snap.PrunedTotal != 1 || snap.TokensSavedSum <= 0 || snap.AlwaysKeepTotal == 0 {
		t.Fatalf("tool-prune snapshot = %+v, want one pruned tool with savings and always-keep", snap)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if !summary.ToolPrune.Applied ||
		summary.ToolPrune.Reason != "idle_tools" ||
		summary.ToolPrune.PrunedTools != 1 ||
		summary.ToolPrune.SavedTokens <= 0 ||
		summary.ToolPrune.AlwaysKept == 0 {
		t.Fatalf("WSS tool-prune summary did not account applied savings: %+v", summary.ToolPrune)
	}
	if summary.Flight == nil ||
		!summary.Flight.ToolPrune.Applied ||
		summary.Flight.ToolPrune.SavedTokens != summary.ToolPrune.SavedTokens {
		t.Fatalf("WSS flight tool-prune accounting missing: %+v", summary.Flight)
	}
	toolPruneMechanism := mechanismByNameForTest(summary.Mechanisms, "tool_prune")
	if toolPruneMechanism.SavedTokens != summary.ToolPrune.SavedTokens ||
		toolPruneMechanism.NetTokens != summary.ToolPrune.SavedTokens {
		t.Fatalf("WSS tool-prune mechanism accounting mismatch: %+v summary=%+v", toolPruneMechanism, summary.ToolPrune)
	}
}

func TestWSPhaseFToolPruneAcceptsCodexDesktopSpecialToolShapes(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune-desktop-special"
	p.toolPrune.ObserveTurn(sessionID, []string{"ColdTool"})
	p.toolPrune.ObserveTurn(sessionID, nil)

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "wss-tool-prune-desktop-special",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Continue with the available tools.",
			}},
			"tools": []map[string]any{
				{"type": "function", "name": "exec_command", "description": "Run shell commands", "parameters": map[string]any{"type": "object"}},
				{"type": "custom", "name": "apply_patch", "description": "Patch files"},
				{"type": "tool_search", "parameters": map[string]any{"type": "object"}},
				{"type": "web_search", "external_web_access": true},
				{"type": "image_generation", "output_format": "png"},
				codexToolDefinition("ColdTool", strings.Repeat("Idle expensive schema. ", 80)),
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !replace {
		t.Fatal("expected WSS tool-prune mutation")
	}
	body := string(env.Body)
	if strings.Contains(body, "ColdTool") {
		t.Fatalf("idle tool still present after prune: %s", body)
	}
	for _, kept := range []string{"exec_command", "apply_patch", "tool_search", "web_search", "image_generation"} {
		if !strings.Contains(body, kept) {
			t.Fatalf("desktop special tool %q was removed: %s", kept, body)
		}
	}
}

func TestWSPhaseFToolPruneSkipsPreviousResponseDeltaTurns(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune-delta"
	p.toolPrune.ObserveTurn(sessionID, []string{"Bash", "ColdTool"})
	p.toolPrune.ObserveTurn(sessionID, []string{"Bash"})

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-tool-prune-delta",
			"prompt_cache_key":     "wss-tool-prune-delta",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Continue with the available tools.",
			}},
			"tools": []map[string]any{
				codexToolDefinition("Bash", "Run a shell command"),
				codexToolDefinition("ColdTool", strings.Repeat("Idle expensive schema. ", 80)),
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatalf("previous_response_id delta must not prune tool prefix: %s", env.Body)
	}
	body := string(env.Body)
	if !strings.Contains(body, "ColdTool") || !strings.Contains(body, "Bash") {
		t.Fatalf("delta guard must preserve full tool prefix: %s", body)
	}
	snap := p.toolPrune.Snapshot()
	if snap.PrunedTotal != 0 || snap.TokensSavedSum != 0 {
		t.Fatalf("delta guard must not book tool-prune savings: %+v", snap)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.tool_prune_guard"] != "wss_tool_prune_delta_guard" || summary.Tokens.Saved != 0 {
		t.Fatalf("delta tool-prune guard summary missing: %+v", summary)
	}
	if summary.ToolPrune.Applied ||
		summary.ToolPrune.Reason != "wss_tool_prune_delta_guard" ||
		summary.ToolPrune.SavedTokens != 0 {
		t.Fatalf("delta tool-prune guard must be accounted without savings: %+v", summary.ToolPrune)
	}
}

func TestWSPhaseFToolPruneDeltaGuardRefreshesResolvedToolUsage(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune-delta-refresh"
	const callID = "call_cold_tool"
	p.toolPrune.ObserveTurn(sessionID, []string{"ColdTool", "IdleTool"})
	p.toolPrune.ObserveTurn(sessionID, nil)
	p.toolPrune.ObserveTurn(sessionID, nil)

	body := mustMarshal(map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-tool-prune-delta-refresh",
		"input": []map[string]any{
			{"type": "function_call_output", "call_id": callID, "output": "cold tool result just arrived"},
			{"type": "message", "role": "user", "content": "Continue."},
		},
		"tools": []map[string]any{
			codexToolDefinition("ColdTool", strings.Repeat("Recently used expensive schema. ", 80)),
			codexToolDefinition("IdleTool", strings.Repeat("Idle expensive schema. ", 80)),
		},
		"stream": true,
	})
	messages := []types.Message{
		{
			Role: "tool",
			Content: []types.ContentBlock{{
				Type:         "tool_result",
				ToolResultID: callID,
				Text:         "cold tool result just arrived",
			}},
		},
		{
			Role:    "user",
			Content: []types.ContentBlock{{Type: "text", Text: "Continue."}},
		},
	}
	meta := wssRequestMeta{
		SessionID:          sessionID,
		PreviousResponseID: "resp-tool-prune-delta-refresh",
		HasUserPromptInput: true,
		HasToolDefinitions: true,
		ToolUseIndex: map[string]types.ContentBlock{
			callID: {Type: "tool_use", ToolUseID: callID, ToolName: "ColdTool"},
		},
	}

	out, changed, result := adapter.applyWSSToolPrune(body, messages, meta)
	if changed || !bytes.Equal(out, body) {
		t.Fatalf("delta guard must not mutate body: changed=%v out=%s", changed, out)
	}
	if result.GuardReason != "wss_tool_prune_delta_guard" || result.Summary.Applied || result.Summary.SavedTokens != 0 {
		t.Fatalf("delta guard summary mismatch: %+v", result)
	}
	if !p.toolPrune.Active(sessionID, "ColdTool") {
		t.Fatal("delta guard must refresh resolved tool-result usage for ColdTool")
	}
	if p.toolPrune.Active(sessionID, "IdleTool") {
		t.Fatal("unmentioned idle tool should remain prunable")
	}
}

func TestWSPhaseFToolPruneAllowsPreviousResponseFullHistoryTurns(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune-full-history"
	p.toolPrune.ObserveTurn(sessionID, []string{"Bash", "ColdTool"})
	p.toolPrune.ObserveTurn(sessionID, []string{"Bash"})

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-tool-prune-full-history",
			"prompt_cache_key":     "wss-tool-prune-full-history",
			"input": []map[string]any{
				{"type": "function_call", "call_id": "call_bash", "name": "Bash", "arguments": map[string]any{"cmd": "echo ok"}},
				{"type": "function_call_output", "call_id": "call_bash", "output": "ok"},
				{"type": "message", "role": "user", "content": "Continue with the available tools."},
			},
			"tools": []map[string]any{
				codexToolDefinition("Bash", "Run a shell command"),
				codexToolDefinition("ColdTool", strings.Repeat("Idle expensive schema. ", 80)),
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !replace {
		t.Fatal("expected previous_response_id full-history tool-prune mutation")
	}
	body := string(env.Body)
	if strings.Contains(body, "ColdTool") {
		t.Fatalf("full-history idle tool still present after prune: %s", body)
	}
	if !strings.Contains(body, "Bash") {
		t.Fatalf("active tool was removed: %s", body)
	}
	snap := p.toolPrune.Snapshot()
	if snap.PrunedTotal != 1 || snap.TokensSavedSum <= 0 {
		t.Fatalf("tool-prune snapshot = %+v, want one full-history prune with savings", snap)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.tool_prune_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" ||
		summary.DebugFacts["wss.delta_shape"] != "false" {
		t.Fatalf("full-history tool-prune summary should save without delta guard: %+v", summary)
	}
	if !summary.ToolPrune.Applied ||
		summary.ToolPrune.Reason != "idle_tools" ||
		summary.ToolPrune.PrunedTools != 1 ||
		summary.ToolPrune.SavedTokens <= 0 {
		t.Fatalf("full-history WSS tool-prune summary missing savings: %+v", summary.ToolPrune)
	}
}

func TestWSPhaseFToolPruneUsageObservesResolvedToolResults(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:resolved-tool-prune"

	adapter.observeWSSToolPruneUsage(sessionID, []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_exec",
			Text:         "ok",
		}},
	}}, map[string]types.ContentBlock{
		"call_exec": {Type: "tool_use", ToolUseID: "call_exec", ToolName: "ColdTool"},
	})
	p.toolPrune.ObserveTurn(sessionID, nil)
	if !p.toolPrune.Active(sessionID, "ColdTool") {
		t.Fatal("resolved tool result was not observed as active")
	}
	p.toolPrune.ObserveTurn(sessionID, nil)
	decision := p.toolPrune.DecideWithOptions(sessionID, []string{"ColdTool"}, toolprune.DecisionOptions{MinKeep: 0})
	if len(decision.Pruned) != 1 || decision.Pruned[0] != "ColdTool" {
		t.Fatalf("expected resolved tool to become idle-prunable, got %+v", decision)
	}
}

func TestWSPhaseFToolPruneKeepsResolvedToolResultActive(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune-resolved-active"
	const callID = "call_cold_tool"
	p.toolPrune.ObserveTurn(sessionID, []string{"ColdTool", "IdleTool"})
	p.toolPrune.ObserveTurn(sessionID, nil)
	adapter.mu.Lock()
	adapter.sessionID = sessionID
	adapter.toolUseHydrated = true
	adapter.toolUses = map[string]types.ContentBlock{
		callID: {Type: "tool_use", ToolUseID: callID, ToolName: "ColdTool"},
	}
	adapter.mu.Unlock()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "wss-tool-prune-resolved-active",
			"input": []map[string]any{
				{"type": "function_call_output", "call_id": callID, "output": "cold tool result just arrived"},
				{"type": "message", "role": "user", "content": "Continue with the available tools."},
			},
			"tools": []map[string]any{
				codexToolDefinition("ColdTool", strings.Repeat("Recently used expensive schema. ", 80)),
				codexToolDefinition("IdleTool", strings.Repeat("Idle expensive schema. ", 80)),
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !replace {
		t.Fatal("expected WSS tool-prune mutation")
	}
	body := string(env.Body)
	if !strings.Contains(body, "ColdTool") {
		t.Fatalf("resolved active tool was pruned: %s", body)
	}
	if strings.Contains(body, "IdleTool") {
		t.Fatalf("idle tool should still be pruned: %s", body)
	}
	snap := p.toolPrune.Snapshot()
	if snap.PrunedTotal != 1 || snap.TokensSavedSum <= 0 {
		t.Fatalf("tool-prune snapshot = %+v, want one idle prune with savings", snap)
	}
}

func TestWSPhaseFToolPruneUnknownSchemaFullPasses(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "wss-tool-prune-unknown",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Continue.",
			}},
			"tools":  []map[string]any{{"kind": "unknown-provider-shape"}},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatalf("unknown tool schema must full-pass: %s", env.Body)
	}
	if snap := p.toolPrune.Snapshot(); snap.PrunedTotal != 0 {
		t.Fatalf("unknown schema must not prune: %+v", snap)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.ToolPrune.Applied ||
		summary.ToolPrune.Reason != "unknown_tool_schema_full_pass" ||
		summary.ToolPrune.SavedTokens != 0 {
		t.Fatalf("unknown schema full-pass must be accounted without savings: %+v", summary.ToolPrune)
	}
}

func TestWSPhaseFToolPruneReattachesMentionedTool(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune-reattach"
	p.toolPrune.RememberPrunedDef(sessionID, "ColdTool", mustMarshal(codexToolDefinition("ColdTool", "Recovered schema")))

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "wss-tool-prune-reattach",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Please use ColdTool now.",
			}},
			"tools":  []map[string]any{codexToolDefinition("Bash", "Run a shell command")},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !replace {
		t.Fatal("expected reattach mutation")
	}
	body := string(env.Body)
	if !strings.Contains(body, "ColdTool") || !strings.Contains(body, "Bash") {
		t.Fatalf("reattach must keep existing and mentioned tools: %s", body)
	}
	snap := p.toolPrune.Snapshot()
	if snap.ReattachTotal != 1 || snap.PrunedTotal != 0 {
		t.Fatalf("tool-prune snapshot = %+v, want one reattach and no same-turn prune", snap)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.ToolPrune.Applied ||
		summary.ToolPrune.Reattached != 1 ||
		summary.ToolPrune.SavedTokens != 0 {
		t.Fatalf("WSS reattach summary should account overhead without savings claim: %+v", summary.ToolPrune)
	}
}

func TestWSPhaseFToolPruneSkipsReattachOnPreviousResponseDeltaTurns(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune-delta-reattach"
	p.toolPrune.RememberPrunedDef(sessionID, "ColdTool", mustMarshal(codexToolDefinition("ColdTool", "Recovered schema")))

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-tool-prune-delta-reattach",
			"prompt_cache_key":     "wss-tool-prune-delta-reattach",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Please use ColdTool now.",
			}},
			"tools":  []map[string]any{codexToolDefinition("Bash", "Run a shell command")},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatalf("previous_response_id delta must not reattach tool prefix: %s", env.Body)
	}
	body := string(env.Body)
	if strings.Contains(body, `"name":"ColdTool"`) || !strings.Contains(body, `"name":"Bash"`) {
		t.Fatalf("delta guard must preserve current tool prefix byte-shape: %s", body)
	}
	snap := p.toolPrune.Snapshot()
	if snap.ReattachTotal != 0 || snap.PrunedTotal != 0 {
		t.Fatalf("delta guard must not book tool-prune mutation counters: %+v", snap)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.tool_prune_guard"] != "wss_tool_prune_delta_guard" || summary.Tokens.Saved != 0 {
		t.Fatalf("delta reattach guard summary missing: %+v", summary)
	}
	if summary.ToolPrune.Applied ||
		summary.ToolPrune.Reason != "wss_tool_prune_delta_guard" ||
		summary.ToolPrune.Reattached != 0 ||
		summary.ToolPrune.SavedTokens != 0 {
		t.Fatalf("delta reattach guard must be accounted without mutation: %+v", summary.ToolPrune)
	}
}

func TestWSPhaseFToolPruneReattachesPreviousResponseFullHistoryTurns(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:wss-tool-prune-full-history-reattach"
	p.toolPrune.RememberPrunedDef(sessionID, "ColdTool", mustMarshal(codexToolDefinition("ColdTool", "Recovered schema")))

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-tool-prune-full-history-reattach",
			"prompt_cache_key":     "wss-tool-prune-full-history-reattach",
			"input": []map[string]any{
				{"type": "function_call", "call_id": "call_bash", "name": "Bash", "arguments": map[string]any{"cmd": "echo ok"}},
				{"type": "function_call_output", "call_id": "call_bash", "output": "ok"},
				{"type": "message", "role": "user", "content": "Please use ColdTool now."},
			},
			"tools":  []map[string]any{codexToolDefinition("Bash", "Run a shell command")},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !replace {
		t.Fatal("expected previous_response_id full-history reattach mutation")
	}
	body := string(env.Body)
	if !strings.Contains(body, "ColdTool") || !strings.Contains(body, "Bash") {
		t.Fatalf("full-history reattach must keep existing and mentioned tools: %s", body)
	}
	snap := p.toolPrune.Snapshot()
	if snap.ReattachTotal != 1 || snap.PrunedTotal != 0 {
		t.Fatalf("tool-prune snapshot = %+v, want one full-history reattach and no same-turn prune", snap)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.tool_prune_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" ||
		summary.DebugFacts["wss.delta_shape"] != "false" ||
		summary.Tokens.Saved != 0 {
		t.Fatalf("full-history reattach summary should mutate without savings claim or delta guard: %+v", summary)
	}
	if summary.ToolPrune.Applied ||
		summary.ToolPrune.Reattached != 1 ||
		summary.ToolPrune.SavedTokens != 0 {
		t.Fatalf("full-history WSS reattach summary should account overhead without savings claim: %+v", summary.ToolPrune)
	}
}

func TestWSPhaseFArchiveRecoveryNoteInjectsOncePerSession(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	body := func(session string) []byte {
		return mustMarshal(map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": session,
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "continue",
			}},
			"stream": true,
		})
	}

	first, _, changed, _, _ := adapter.applyInputPipeline(body("archive-note-session"))
	if !changed || !strings.Contains(string(first), "local-archive://") ||
		strings.Contains(strings.ToLower(string(first)), "slimference") {
		t.Fatalf("archive recovery note missing or product-voiced: changed=%v body=%s", changed, first)
	}
	second, _, changed, _, _ := adapter.applyInputPipeline(body("archive-note-session"))
	if changed || strings.Contains(string(second), "local-archive://") {
		t.Fatalf("archive recovery note should inject once per session, changed=%v body=%s", changed, second)
	}
	third, _, changed, _, _ := adapter.applyInputPipeline(body("archive-note-other-session"))
	if !changed || !strings.Contains(string(third), "local-archive://") {
		t.Fatalf("distinct session should receive its own note, changed=%v body=%s", changed, third)
	}
}

func TestWSPhaseFChunkDedupWiringForSimilarReads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const promptCacheKey = "chunk-wss-session"
	cleanupPhaseFTempHome(t, home, "codex-wss:"+promptCacheKey)
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = true
	cfg.Compression.OutputReduce.CodexChunkDedupEnabled = true
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	cfg.Compression.OutputReduce.CodexChunkDedupMinBytes = 0
	cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent = 100
	cfg.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent = 100
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	var sharedBuilder strings.Builder
	for i := 0; i < 1600; i++ {
		fmt.Fprintf(&sharedBuilder, "shared cross-file region %04d with stable content %08x\n", i, i*7919+17)
	}
	shared := sharedBuilder.String()
	body := func(path, callID, text string) []byte {
		return mustMarshal(map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": promptCacheKey,
			"input": []map[string]any{
				{"type": "function_call", "call_id": callID, "name": "read_file", "arguments": map[string]any{"path": path}},
				{"type": "function_call_output", "call_id": callID, "output": text},
			},
			"stream": true,
		})
	}

	first, _, changed, stats, _ := adapter.applyInputPipeline(body("a.go", "read-a", shared+strings.Repeat("tail a\n", 120)))
	if !changed || stats.ChunkDedupBlocks != 0 || strings.Contains(string(first), "[context-chunk status=unchanged") {
		t.Fatalf("first similar read should seed chunks only and inject recovery note: changed=%v stats=%+v body=%s", changed, stats, first)
	}
	second, _, changed, stats, _ := adapter.applyInputPipeline(body("b.go", "read-b", shared+strings.Repeat("tail b\n", 120)))
	if !changed || stats.ChunkDedupBlocks != 1 || stats.TokensSaved <= 0 ||
		!strings.Contains(string(second), "[context-chunk status=unchanged uri=local-archive://") {
		t.Fatalf("second similar read should use WSS chunk dedup: changed=%v stats=%+v body=%s", changed, stats, second)
	}
	snap := p.OutputReduceCountersSnapshot()
	if snap.ProxyLayer0ChunkDedupBlocks != 1 || snap.ProxyLayer0Routes.WSSPhaseF.ChunkDedupBlocks != 1 {
		t.Fatalf("chunk dedup counters missing: %+v", snap)
	}
}

func TestWSPhaseFFullHistoryChunkDedupFullPassOnLiveSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const promptCacheKey = "chunk-wss-full-history-guard"
	cleanupPhaseFTempHome(t, home, "codex-wss:"+promptCacheKey)
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = true
	cfg.Compression.OutputReduce.CodexChunkDedupEnabled = true
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	cfg.Compression.OutputReduce.CodexChunkDedupMinBytes = 0
	cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent = 100
	cfg.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent = 100
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(1)
	shared := strings.Repeat("full-history guarded chunk region keeps exact context recoverable\n", 1000)
	body := func(path, callID, text string) []byte {
		return mustMarshal(map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": promptCacheKey,
			"input": []map[string]any{
				{"type": "function_call", "call_id": callID, "name": "read_file", "arguments": map[string]any{"path": path}},
				{"type": "function_call_output", "call_id": callID, "output": text},
			},
			"stream": true,
		})
	}

	_, _, _, stats, _ := adapter.applyInputPipeline(body("a.go", "read-a", shared+"tail a\n"))
	if stats.ChunkDedupBlocks != 0 {
		t.Fatalf("first full-history read should seed chunks only: %+v", stats)
	}
	second, _, _, stats, _ := adapter.applyInputPipeline(body("b.go", "read-b", shared+"tail b\n"))
	if stats.ChunkDedupBlocks != 0 || stats.TokensSaved != 0 || strings.Contains(string(second), "[context-chunk status=unchanged") {
		t.Fatalf("live-socket full-history chunk dedup must full-pass: stats=%+v body=%s", stats, second)
	}
	if !hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismChunkDedup, "wss_full_history_downstream_delta_proof_gate", evidence.ActionFullPass) {
		t.Fatalf("guarded full-history chunk dedup must emit precise evidence: %+v", stats.EvidenceDecisions)
	}
}

func TestWSPhaseFAutoPolicyEnablesRecoverableChunkDedup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const promptCacheKey = "auto-policy-chunk-session"
	cleanupPhaseFTempHome(t, home, "codex-wss:"+promptCacheKey)
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexChunkDedupMinBytes = 0
	cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent = 100
	cfg.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent = 100
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	shared := strings.Repeat("auto policy shared region keeps model context recoverable\n", 1000)
	body := func(path, callID, text string) []byte {
		return mustMarshal(map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": promptCacheKey,
			"input": []map[string]any{
				{"type": "function_call", "call_id": callID, "name": "read_file", "arguments": map[string]any{"path": path}},
				{"type": "function_call_output", "call_id": callID, "output": text},
			},
			"stream": true,
		})
	}

	first, _, changed, stats, _ := adapter.applyInputPipeline(body("a.go", "read-a", shared+"tail a\n"))
	if changed || stats.ChunkDedupBlocks != 0 || strings.Contains(string(first), "local-archive://") {
		t.Fatalf("first auto-policy read should seed only without recovery-note noise: changed=%v stats=%+v body=%s", changed, stats, first)
	}
	second, _, changed, stats, _ := adapter.applyInputPipeline(body("b.go", "read-b", shared+"tail b\n"))
	if !changed || stats.ChunkDedupBlocks != 1 ||
		!strings.Contains(string(second), "[context-chunk status=unchanged uri=local-archive://") ||
		!strings.Contains(string(second), "If a tool result contains [context-archive") {
		t.Fatalf("auto policy should enable recoverable chunk dedup and inject recovery note only when needed: changed=%v stats=%+v body=%s", changed, stats, second)
	}
}

func TestWSPhaseFDefaultPreviousResponseChunkDedupKeepsSavings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const promptCacheKey = "previous-response-chunk-session"
	cleanupPhaseFTempHome(t, home, "codex-wss:"+promptCacheKey)
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexChunkDedupMinBytes = 0
	cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent = 100
	cfg.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent = 100
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	shared := strings.Repeat("previous response shared region stays recoverable\n", 1000)
	body := func(turnID, path, callID, text string) []byte {
		return mustMarshal(map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": turnID,
			"prompt_cache_key":     promptCacheKey,
			"input": []map[string]any{
				{"type": "function_call", "call_id": callID, "name": "read_file", "arguments": map[string]any{"path": path}},
				{"type": "function_call_output", "call_id": callID, "output": text},
			},
			"stream": true,
		})
	}

	first, _, changed, stats, _ := adapter.applyInputPipeline(body("resp-a", "a.go", "read-a", shared+"tail a\n"))
	if changed || stats.ChunkDedupBlocks != 0 || strings.Contains(string(first), "local-archive://") {
		t.Fatalf("first previous-response read should seed only: changed=%v stats=%+v body=%s", changed, stats, first)
	}
	// Live E5 restart proof 2026-06-11: full-history resend mutation
	// continued cleanly with lost=0. Delta-only previous_response_id turns
	// still full-pass elsewhere; history-shaped requests can take the
	// recoverable savings path.
	second, _, changed, stats, _ := adapter.applyInputPipeline(body("resp-b", "b.go", "read-b", shared+"tail b\n"))
	if !changed || stats.ChunkDedupBlocks != 1 || !strings.Contains(string(second), "[context-chunk") {
		t.Fatalf("previous-response full-history turns should mutate by default: changed=%v stats=%+v body=%s", changed, stats, second)
	}
	gated := false
	for _, decision := range stats.EvidenceDecisions {
		if decision.Reason == "wss_stateful_delta_mutation_proof_gate" {
			gated = true
		}
	}
	if gated {
		t.Fatalf("full-history mutation must not carry the delta proof-gate reason: %+v", stats.EvidenceDecisions)
	}
}

func TestWSPhaseFConservativePolicyKeepsChunkDedupOptIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const promptCacheKey = "conservative-policy-chunk-session"
	cleanupPhaseFTempHome(t, home, "codex-wss:"+promptCacheKey)
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexSavingsPolicyMode = "conservative"
	cfg.Compression.OutputReduce.CodexChunkDedupMinBytes = 0
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	shared := strings.Repeat("conservative policy shared region\n", 1000)
	body := func(path, callID, text string) []byte {
		return mustMarshal(map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": promptCacheKey,
			"input": []map[string]any{
				{"type": "function_call", "call_id": callID, "name": "read_file", "arguments": map[string]any{"path": path}},
				{"type": "function_call_output", "call_id": callID, "output": text},
			},
			"stream": true,
		})
	}

	_, _, _, _, _ = adapter.applyInputPipeline(body("a.go", "read-a", shared+"tail a\n"))
	second, _, changed, stats, _ := adapter.applyInputPipeline(body("b.go", "read-b", shared+"tail b\n"))
	if changed || stats.ChunkDedupBlocks != 0 || strings.Contains(string(second), "context-chunk") {
		t.Fatalf("conservative policy should not auto-enable chunk dedup: changed=%v stats=%+v body=%s", changed, stats, second)
	}
}

func TestWSPhaseFPromptCachePrefixBlocksStayByteEqual(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	body := mustMarshal(map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "cached-prefix-session",
		"instructions":     strings.Repeat("cached system instruction block ", 400),
		"tools": []map[string]any{{
			"type":        "function",
			"name":        "exec_command",
			"description": strings.Repeat("cached tool schema block ", 200),
		}},
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": "continue",
		}},
		"stream": true,
	})

	mutated, messages, changed, stats, reReads := adapter.applyInputPipeline(body)
	if changed || !bytes.Equal(mutated, body) {
		t.Fatalf("prompt-cache prefix request must stay byte-equal, changed=%v body=%s", changed, mutated)
	}
	if len(messages) != 1 || stats.TokensSaved != 0 || stats.BlocksModified != 0 || reReads != 0 {
		t.Fatalf("unexpected prefix guard stats: messages=%d stats=%+v rereads=%d", len(messages), stats, reReads)
	}
}

func TestWSPhaseFHandleGuardBranches(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	req := parseWSJSON(t, map[string]any{"type": string(wsmitm.FrameKindRequest), "body": map[string]any{"input": "x"}})

	for _, tc := range []struct {
		name    string
		adapter *wsPhaseFAdapter
		dir     wsmitm.Direction
		env     *wsmitm.Envelope
	}{
		{name: "nil adapter", adapter: nil, dir: wsmitm.DirClientToServer, env: &req},
		{name: "nil proxy", adapter: &wsPhaseFAdapter{}, dir: wsmitm.DirClientToServer, env: &req},
		{name: "nil env", adapter: adapter, dir: wsmitm.DirClientToServer, env: nil},
		{name: "unknown", adapter: adapter, dir: wsmitm.DirClientToServer, env: &wsmitm.Envelope{Kind: wsmitm.FrameKindUnknown}},
		{name: "control", adapter: adapter, dir: wsmitm.DirClientToServer, env: &wsmitm.Envelope{Kind: wsmitm.FrameKindPing}},
		{name: "wrong c2s kind", adapter: adapter, dir: wsmitm.DirClientToServer, env: &wsmitm.Envelope{Kind: wsmitm.FrameKindResponseCompleted}},
		{name: "unknown direction", adapter: adapter, dir: wsmitm.Direction("sideways"), env: &req},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replace, err := tc.adapter.handle(context.Background(), tc.dir, tc.env)
			if err != nil {
				t.Fatalf("handle: %v", err)
			}
			if replace {
				t.Fatal("guard branch must not request replacement")
			}
		})
	}
}

func TestWSPhaseFRequestBodyVariants(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	requestEnv := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"request": map[string]any{
			"model": "gpt-5-codex",
			"input": []map[string]any{{
				"type": "message", "role": "user", "content": "Build.",
			}},
			"stream": true,
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &requestEnv)
	if err != nil {
		t.Fatalf("request variant handle: %v", err)
	}
	if replace || strings.Contains(string(requestEnv.Request), `"stop"`) {
		t.Fatalf("Responses-shaped request variant should not get stop: replace=%v request=%s", replace, requestEnv.Request)
	}

	rawBody := mustMarshal(map[string]any{
		"model": "gpt-5-codex",
		"input": []map[string]any{{
			"type": "message", "role": "user", "content": "Test.",
		}},
		"stream": true,
	})
	rawEnv, err := wsmitm.Parse(rawBody)
	if err != nil {
		t.Fatalf("parse raw body envelope: %v", err)
	}
	body, rawReplace, ok := wsRequestBody(&rawEnv)
	if !ok {
		t.Fatal("raw body variant not detected")
	}
	if !strings.Contains(string(body), `"model":"gpt-5-codex"`) {
		t.Fatalf("raw body mismatch: %s", body)
	}
	next := mustMarshal(map[string]any{
		"model":  "gpt-5-codex",
		"input":  "x",
		"stream": true,
		"stop":   []string{"done"},
	})
	if err := rawReplace(next); err != nil {
		t.Fatalf("raw replace: %v", err)
	}
	if !strings.Contains(string(rawEnv.Raw), `"stop"`) {
		t.Fatalf("raw body variant not replaced: raw=%s", rawEnv.Raw)
	}
}

func TestWSPhaseFTopLevelUnknownRequestBodySeedsState(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	toolOutput := strings.Repeat("top level tool output block ", 20)

	env := parseWSJSON(t, map[string]any{
		"model": "gpt-5-codex",
		"input": []map[string]any{
			{
				"type":      "function_call",
				"call_id":   "call_top_repdet",
				"name":      "exec_command",
				"arguments": map[string]any{"cmd": "cat top.txt"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_top_repdet",
				"output":  toolOutput,
			},
		},
		"stream": true,
	})
	if env.Kind != wsmitm.FrameKindUnknown {
		t.Fatalf("precondition: top-level Responses body should parse unknown, got %q", env.Kind)
	}
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("top-level request handle: %v", err)
	}
	if replace {
		t.Fatal("top-level Responses request should seed state without stop mutation")
	}

	resp := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": "Echo: " + toolOutput,
	})
	replace, err = adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp)
	if err != nil {
		t.Fatalf("delta handle: %v", err)
	}
	if !replace || !strings.Contains(resp.Delta, "[unchanged:") {
		t.Fatalf("top-level request did not seed repdet replace=%v delta=%q", replace, resp.Delta)
	}

	snap := adapter.snapshot()
	if snap.RequestsSeen != 1 || snap.RequestBodiesSeen != 1 ||
		snap.RequestMessagesIndexed != 1 || snap.ResponseTextDeltasSeen != 1 ||
		snap.Mutations != 1 {
		t.Fatalf("unexpected top-level request telemetry: %+v", snap)
	}
}

func TestWSRequestBodyNoBodyAndMalformedRawReplacement(t *testing.T) {
	env := wsmitm.Envelope{Kind: wsmitm.FrameKindRequest, Raw: json.RawMessage(`{"type":"request"}`), Fields: map[string]json.RawMessage{}}
	if _, _, ok := wsRequestBody(&env); ok {
		t.Fatal("plain request envelope should not expose a body")
	}

	env = parseWSJSON(t, map[string]any{
		"model":  "gpt-5-codex",
		"stream": true,
		"input":  "x",
	})
	_, replace, ok := wsRequestBody(&env)
	if !ok {
		t.Fatal("raw request-like envelope should expose itself as body")
	}
	if err := replace([]byte(`not-json`)); err == nil {
		t.Fatal("malformed replacement should fail")
	}
}

func TestWSRequestBodyReturnsReadOnlyAliasAndCopiesReplacement(t *testing.T) {
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":  "gpt-5-codex",
			"stream": true,
			"input":  "x",
		},
	})
	body, replace, ok := wsRequestBody(&env)
	if !ok {
		t.Fatal("request body not detected")
	}
	if len(body) == 0 || len(env.Body) == 0 || &body[0] != &env.Body[0] {
		t.Fatal("wsRequestBody should not copy request bytes before mutation")
	}
	next := []byte(`{"model":"gpt-5-codex","stream":true,"input":"y"}`)
	if err := replace(next); err != nil {
		t.Fatalf("replace: %v", err)
	}
	next[0] = '['
	if !jsonObject(env.Body) || !jsonObject(env.Fields["body"]) {
		t.Fatalf("replacement must copy bytes into envelope fields: body=%s field=%s", env.Body, env.Fields["body"])
	}
}

func TestWSPhaseFStreamcutStaysDisabledAfterMultipleWSSDeltas(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StreamCutEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	first := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": strings.Repeat("substantive answer ", 8) + "\nHope this helps",
	})
	if replace, _ := adapter.handle(context.Background(), wsmitm.DirServerToClient, &first); replace {
		t.Fatal("first WSS delta should not fire streamcut")
	}
	second := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": "trailing words after cut",
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &second)
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if replace || second.Delta != "trailing words after cut" {
		t.Fatalf("second WSS delta changed: replace=%v delta=%q", replace, second.Delta)
	}
	empty := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": "",
	})
	replace, err = adapter.handle(context.Background(), wsmitm.DirServerToClient, &empty)
	if err != nil {
		t.Fatalf("empty handle: %v", err)
	}
	if replace {
		t.Fatal("empty WSS delta should not be re-encoded by streamcut")
	}
	if got := p.OutputReduceCountersSnapshot().StreamcutFired; got != 0 {
		t.Fatalf("WSS streamcut counter=%d, want 0", got)
	}
}

func TestWSPhaseFTerminalResponseRepdetStaysByteEqual(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	promptText := strings.Repeat("stable terminal prompt block ", 18)

	req := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model": "gpt-5-codex",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": promptText,
			}},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &req); err != nil || replace {
		t.Fatalf("request handle replace=%v err=%v", replace, err)
	}

	resp := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{
			"output": []map[string]any{{
				"content": []map[string]any{{
					"type": "output_text",
					"text": "Echo: " + promptText,
				}},
			}},
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp)
	if err != nil {
		t.Fatalf("terminal handle: %v", err)
	}
	if replace || strings.Contains(string(resp.Response), "[unchanged:") {
		t.Fatalf("terminal response should stay byte-equal, replace=%v response=%s", replace, resp.Response)
	}
}

func TestWSPhaseFNoOpResponseBranches(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StreamCutEnabled = true
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	for _, env := range []wsmitm.Envelope{
		parseWSJSON(t, map[string]any{"type": string(wsmitm.FrameKindResponseOutputTextDelta), "delta": ""}),
		parseWSJSON(t, map[string]any{"type": string(wsmitm.FrameKindResponseCompleted)}),
		parseWSJSON(t, map[string]any{"type": string(wsmitm.FrameKindResponseOutputItemAdded), "delta": "ignored"}),
	} {
		replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &env)
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		if replace {
			t.Fatalf("unexpected replacement for %+v", env)
		}
	}
}

func TestWSPhaseFAdditionalNoOpAndHelperBranches(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	cfg.Compression.OutputReduce.StreamCutEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	reqWithoutBody := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": "not-json-object",
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &reqWithoutBody)
	if err != nil {
		t.Fatalf("request without JSON body: %v", err)
	}
	if replace {
		t.Fatal("request without JSON object body must be a no-op")
	}

	deltaNoIndex := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": "there is no previously indexed prompt block here",
	})
	if replace, err = adapter.handle(context.Background(), wsmitm.DirServerToClient, &deltaNoIndex); err != nil || replace {
		t.Fatalf("repdet without index should no-op, replace=%v err=%v", replace, err)
	}

	adapter.repdetIndex = buildRepdetIndex([]types.Message{{
		Role: "user",
		Content: []types.ContentBlock{{
			Type: "text",
			Text: strings.Repeat("stable prompt fragment ", 20),
		}},
	}})
	deltaNoMatch := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": "completely unrelated answer text",
	})
	if replace, err = adapter.handle(context.Background(), wsmitm.DirServerToClient, &deltaNoMatch); err != nil || replace {
		t.Fatalf("repdet no-match should no-op, replace=%v err=%v", replace, err)
	}

	terminalNoMatch := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{
			"output": []map[string]any{{
				"content": []map[string]any{{"type": "output_text", "text": "unrelated terminal text"}},
			}},
		},
	})
	if replace, err = adapter.handle(context.Background(), wsmitm.DirServerToClient, &terminalNoMatch); err != nil || replace {
		t.Fatalf("terminal no-match should no-op, replace=%v err=%v", replace, err)
	}

	if wsEnvelopeLooksLikeRequestBody(&wsmitm.Envelope{Raw: json.RawMessage(`[]`), Fields: map[string]json.RawMessage{}}) {
		t.Fatal("non-object raw envelope must not look like request body")
	}
	if !wsEnvelopeLooksLikeRequestBody(&wsmitm.Envelope{Raw: json.RawMessage(`{"model":"m","stream":true}`), Fields: map[string]json.RawMessage{"model": json.RawMessage(`"m"`), "stream": json.RawMessage(`true`)}}) {
		t.Fatal("model+stream raw envelope should look like a request body")
	}
}

func TestWSPhaseFRequestNoMutationAndStaleReadPipelines(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model": "gpt-5-codex",
			"input": []map[string]any{{
				"type": "message", "role": "user", "content": "no mutation",
			}},
			"stream": true,
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if replace {
		t.Fatal("all-disabled request should not be re-encoded")
	}

	cfg = config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p = New(cfg)
	adapter = (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	if !adapter.p.config.Compression.OutputReduce.StaleReadAgingEnabled {
		t.Fatal("precondition: stale-read config not enabled")
	}
	agedBody := codexWSReadBody("Read", strings.Repeat("old file content ", 80), "fresh file content")
	msgs, _, extractErr := extractMessages(types.CodexChatGPT, agedBody)
	if extractErr != nil {
		t.Fatalf("extract aged body: %v", extractErr)
	}
	aged, stats := staleread.AgeMessages(msgs, staleread.Options{MinTurnGap: 2})
	if stats.BlocksReplaced == 0 {
		for i, msg := range msgs {
			t.Logf("aged msg %d role=%s text=%q blocks=%+v", i, msg.Role, msg.TextContent(), msg.Content)
		}
		t.Fatal("precondition: stale read fixture did not age")
	}
	if _, rebuildErr := reconstructBody(types.CodexChatGPT, agedBody, aged); rebuildErr != nil {
		t.Fatalf("precondition: stale read fixture cannot reconstruct: %v", rebuildErr)
	}
	mutated, _, changed, _, _ := adapter.applyInputPipeline(agedBody)
	if !changed || strings.Contains(string(mutated), "old file content") || !strings.Contains(string(mutated), "kind=stale-read") {
		t.Fatalf("stale-read mutation failed changed=%v body=%s", changed, mutated)
	}
	if got := p.OutputReduceCountersSnapshot().StaleReadBlocksReplaced; got == 0 {
		t.Fatalf("stale-read counter not incremented: %+v", p.OutputReduceCountersSnapshot())
	}

	cfg = config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p = New(cfg)
	adapter = (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	prunedBody := codexWSObsoleteReadBody(strings.Repeat("obsolete file content ", 80))
	mutated, _, changed, _, _ = adapter.applyInputPipeline(prunedBody)
	if !changed || strings.Contains(string(mutated), "obsolete file content") || !strings.Contains(string(mutated), "kind=obsolete-read") {
		t.Fatalf("obsolete-read mutation failed changed=%v body=%s", changed, mutated)
	}
	if got := p.OutputReduceCountersSnapshot().ObsoleteReadBlocksPruned; got == 0 {
		t.Fatalf("obsolete counter not incremented: %+v", p.OutputReduceCountersSnapshot())
	}
}

func TestWSPhaseFFullHistoryHistoryReducersApplyOnLiveSocket(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(1)
	body := codexWSStaleObsoleteLayer0Body()

	mutated, _, changed, stats, _, meta, _ := adapter.applyInputPipelineDetailed(body)
	mutatedText := string(mutated)
	if !changed {
		t.Fatal("live-socket fixture should allow history and non-history savings")
	}
	if strings.Contains(mutatedText, "stale x content") || !strings.Contains(mutatedText, "kind=stale-read") {
		t.Fatalf("live-socket full-history stale-read must apply: %s", mutatedText)
	}
	if strings.Contains(mutatedText, "obsolete y content") || !strings.Contains(mutatedText, "kind=obsolete-read") {
		t.Fatalf("live-socket full-history obsolete-prune must apply: %s", mutatedText)
	}
	if !strings.Contains(mutatedText, "[git status]") || !strings.Contains(mutatedText, "context-archive kind=tool-output") || strings.Contains(mutatedText, "single_reconstruct_179.go") {
		t.Fatalf("live-socket full-history history savings must not block captured-output savings: %s", mutatedText)
	}
	if stats.StaleReadBlocks == 0 || stats.ObsoletePruneBlocks == 0 || stats.TokensSaved <= 0 {
		t.Fatalf("history reducers must count applied savings: %+v", stats)
	}
	if meta.DebugFacts["wss.history_mutation_guard"] != "" ||
		meta.DebugFacts["wss.effective_mutation_guard"] != "" {
		t.Fatalf("history reducers must not be guarded or masquerade as structured mutation guard: %+v", meta.DebugFacts)
	}
	if !hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismStaleRead, "positive_net_savings", evidence.ActionApplied) ||
		!hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismObsoletePrune, "positive_net_savings", evidence.ActionApplied) {
		t.Fatalf("applied full-history reducers must emit precise evidence: %+v", stats.EvidenceDecisions)
	}
	for _, mechanism := range []proxyLayer0Mechanism{proxyLayer0MechanismStaleRead, proxyLayer0MechanismObsoletePrune} {
		found := false
		for _, decision := range stats.EvidenceDecisions {
			if decision.Mechanism != string(mechanism) {
				continue
			}
			found = true
			if decision.OriginalTokens <= 0 || decision.FinalTokens <= 0 || decision.SavedTokens <= 0 ||
				decision.FootprintScore <= 0 || decision.FootprintScoreBucket == "" {
				t.Fatalf("applied %s evidence must carry token and footprint calibration data: %+v", mechanism, decision)
			}
		}
		if !found {
			t.Fatalf("missing applied evidence for %s: %+v", mechanism, stats.EvidenceDecisions)
		}
	}
}

func TestWSPhaseFHistoryMutationLabOpensLiveFullHistoryReducers(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	cfg.Compression.OutputReduce.CodexWSSHistoryMutationLabEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(1)

	mutated, _, changed, stats, _, meta, _ := adapter.applyInputPipelineDetailed(codexWSStaleObsoleteLayer0Body())
	mutatedText := string(mutated)
	if !changed {
		t.Fatal("history mutation lab should mutate the live full-history fixture")
	}
	if strings.Contains(mutatedText, "stale x content") || !strings.Contains(mutatedText, "kind=stale-read") {
		t.Fatalf("history mutation lab should apply stale-read aging: %s", mutatedText)
	}
	if strings.Contains(mutatedText, "obsolete y content") || !strings.Contains(mutatedText, "kind=obsolete-read") {
		t.Fatalf("history mutation lab should apply obsolete-read pruning: %s", mutatedText)
	}
	if stats.StaleReadBlocks == 0 || stats.ObsoletePruneBlocks == 0 || stats.TokensSaved <= 0 {
		t.Fatalf("history mutation lab should count applied history savings: %+v", stats)
	}
	if meta.DebugFacts["wss.history_mutation_guard"] != "" ||
		meta.DebugFacts["wss.effective_mutation_guard"] != "" ||
		meta.DebugFacts["wss.downstream_state_mutation_guard"] != "wss_full_history_downstream_delta_proof_gate" {
		t.Fatalf("history mutation lab should only keep the downstream-state proof gate: %+v", meta.DebugFacts)
	}
	if !hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismStaleRead, "positive_net_savings", evidence.ActionApplied) ||
		!hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismObsoletePrune, "positive_net_savings", evidence.ActionApplied) {
		t.Fatalf("history mutation lab should emit applied history evidence: %+v", stats.EvidenceDecisions)
	}
}

func TestWSPhaseFPreviousResponseBypassKeepsHistoryReducerEvidence(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(1)
	body := mustMarshal(map[string]any{
		"model":                "gpt-5-codex",
		"prompt_cache_key":     "previous-response-history-evidence",
		"previous_response_id": "resp-history-evidence",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "read src/x.go and src/y.go"},
			{"type": "function_call", "call_id": "call_x_old", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_x_old", "output": strings.Repeat("stale x content ", 80)},
			{"type": "function_call", "call_id": "call_y_old", "name": "Read", "arguments": map[string]any{"path": "src/y.go"}},
			{"type": "function_call_output", "call_id": "call_y_old", "output": strings.Repeat("obsolete y content ", 80)},
			{"type": "message", "role": "user", "content": "filler one"},
			{"type": "message", "role": "user", "content": "filler two"},
			{"type": "function_call", "call_id": "call_x_fresh", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_x_fresh", "output": "fresh x content"},
			{"type": "function_call", "call_id": "call_y_edit", "name": "apply_patch", "arguments": map[string]any{"path": "src/y.go", "patch": "@@ ..."}},
			{"type": "function_call_output", "call_id": "call_y_edit", "output": "patch applied"},
			{"type": "function_call_output", "call_id": "call_unknown", "output": "unknown tool output keeps the previous_response guard active"},
		},
		"stream": true,
	})

	mutated, _, changed, stats, _, meta, _ := adapter.applyInputPipelineDetailed(body)
	mutatedText := string(mutated)
	if !changed {
		t.Fatalf("previous_response unknown-tool bypass should apply history-only savings")
	}
	if bytes.Contains(mutated, []byte("previous_response_id")) {
		t.Fatalf("history-only previous_response bypass must detach previous_response_id: %s", mutated)
	}
	if strings.Contains(mutatedText, "stale x content") || !strings.Contains(mutatedText, "kind=stale-read") {
		t.Fatalf("previous_response bypass stale-read mutation missing: %s", mutatedText)
	}
	if strings.Contains(mutatedText, "obsolete y content") || !strings.Contains(mutatedText, "kind=obsolete-read") {
		t.Fatalf("previous_response bypass obsolete-prune mutation missing: %s", mutatedText)
	}
	if meta.BypassReason != "wss_previous_response_history_only" {
		t.Fatalf("expected previous_response bypass, got %+v", meta)
	}
	if stats.StaleReadBlocks == 0 || stats.ObsoletePruneBlocks == 0 || stats.TokensSaved <= 0 {
		t.Fatalf("bypass history savings must count applied savings: %+v", stats)
	}
	if !hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismStaleRead, "positive_net_savings", evidence.ActionApplied) ||
		!hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismObsoletePrune, "positive_net_savings", evidence.ActionApplied) {
		t.Fatalf("previous_response bypass must record applied history evidence: %+v", stats.EvidenceDecisions)
	}
	if meta.DebugFacts["wss.bypass_reason"] != "wss_previous_response_history_only" ||
		meta.DebugFacts["wss.request_shape"] != "full_history" ||
		meta.DebugFacts["wss.stale_read_blocks"] == "0" ||
		meta.DebugFacts["wss.obsolete_prune_blocks"] == "0" ||
		meta.DebugFacts["wss.full_history_detached_previous_response"] != "true" {
		t.Fatalf("debug facts must stay honest for history-only bypass: %+v", meta.DebugFacts)
	}
}

func TestWSPhaseFHistoryLabAppliesBeforePreviousResponseBypass(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	cfg.Compression.OutputReduce.CodexWSSHistoryMutationLabEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(1)
	body := mustMarshal(map[string]any{
		"model":                "gpt-5-codex",
		"prompt_cache_key":     "previous-response-history-lab",
		"previous_response_id": "resp-history-lab",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "read src/x.go and src/y.go"},
			{"type": "function_call", "call_id": "call_x_old", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_x_old", "output": strings.Repeat("stale x content ", 80)},
			{"type": "function_call", "call_id": "call_y_old", "name": "Read", "arguments": map[string]any{"path": "src/y.go"}},
			{"type": "function_call_output", "call_id": "call_y_old", "output": strings.Repeat("obsolete y content ", 80)},
			{"type": "message", "role": "user", "content": "filler one"},
			{"type": "message", "role": "user", "content": "filler two"},
			{"type": "function_call", "call_id": "call_x_fresh", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_x_fresh", "output": "fresh x content"},
			{"type": "function_call", "call_id": "call_y_edit", "name": "apply_patch", "arguments": map[string]any{"path": "src/y.go", "patch": "@@ ..."}},
			{"type": "function_call_output", "call_id": "call_y_edit", "output": "patch applied"},
			{"type": "function_call_output", "call_id": "call_unknown", "output": "unknown tool output keeps the previous_response guard active"},
		},
		"stream": true,
	})

	mutated, _, changed, stats, _, meta, _ := adapter.applyInputPipelineDetailed(body)
	mutatedText := string(mutated)
	if !changed {
		t.Fatal("history lab should apply safe history reducers before previous-response bypass")
	}
	if strings.Contains(mutatedText, "stale x content") || !strings.Contains(mutatedText, "kind=stale-read") {
		t.Fatalf("stale-read mutation missing before bypass: %s", mutatedText)
	}
	if strings.Contains(mutatedText, "obsolete y content") || !strings.Contains(mutatedText, "kind=obsolete-read") {
		t.Fatalf("obsolete-prune mutation missing before bypass: %s", mutatedText)
	}
	if bytes.Contains(mutated, []byte("previous_response_id")) {
		t.Fatalf("history-only full-history mutation must detach previous_response_id: %s", mutated)
	}
	if meta.BypassReason != "wss_previous_response_history_only" ||
		meta.DebugFacts["wss.bypass_reason"] != "wss_previous_response_history_only" {
		t.Fatalf("history-only bypass reason missing: %+v facts=%+v", meta, meta.DebugFacts)
	}
	if meta.DebugFacts["wss.full_history_detached_previous_response"] != "true" {
		t.Fatalf("detach fact missing: %+v", meta.DebugFacts)
	}
	if stats.StaleReadBlocks == 0 || stats.ObsoletePruneBlocks == 0 || stats.TokensSaved <= 0 {
		t.Fatalf("history-only bypass should count applied history savings: %+v", stats)
	}
}

func TestWSPhaseFRecoveryGuardStopsFurtherHistoryLabMutation(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	cfg.Compression.OutputReduce.CodexWSSHistoryMutationLabEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(1)
	adapter.markWSSHistoryMutationRecoveryGuarded()
	body := mustMarshal(map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "history-recovery-guard",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "read src/x.go and src/y.go"},
			{"type": "function_call", "call_id": "call_x_old", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_x_old", "output": strings.Repeat("stale x content ", 80)},
			{"type": "function_call", "call_id": "call_y_old", "name": "Read", "arguments": map[string]any{"path": "src/y.go"}},
			{"type": "function_call_output", "call_id": "call_y_old", "output": strings.Repeat("obsolete y content ", 80)},
			{"type": "message", "role": "user", "content": "filler one"},
			{"type": "message", "role": "user", "content": "filler two"},
			{"type": "function_call", "call_id": "call_x_fresh", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_x_fresh", "output": "fresh x content"},
			{"type": "function_call", "call_id": "call_y_edit", "name": "apply_patch", "arguments": map[string]any{"path": "src/y.go", "patch": "@@ ..."}},
			{"type": "function_call_output", "call_id": "call_y_edit", "output": "patch applied"},
		},
		"stream": true,
	})

	mutated, _, changed, stats, _, meta, _ := adapter.applyInputPipelineDetailed(body)
	if changed || !bytes.Equal(mutated, body) {
		t.Fatalf("recovery guard must full-pass later full-history mutations: changed=%v body=%s", changed, mutated)
	}
	if stats.StaleReadBlocks != 0 || stats.ObsoletePruneBlocks != 0 || stats.TokensSaved != 0 {
		t.Fatalf("recovery-guarded history savings must be evidence-only: %+v", stats)
	}
	if meta.DebugFacts["wss.history_mutation_guard"] != "wss_recovery_history_mutation_guard" {
		t.Fatalf("recovery history guard fact missing: %+v", meta.DebugFacts)
	}
}

func TestWSPhaseFRecoveryGuardScopesToResponseLineage(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	cfg.Compression.OutputReduce.CodexWSSHistoryMutationLabEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(1)
	adapter.markWSSHistoryMutationRecoveryLineage("resp-recovered")

	buildBody := func(previousResponseID, cacheKey string) []byte {
		return mustMarshal(map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     cacheKey,
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "read src/x.go and src/y.go"},
				{"type": "function_call", "call_id": "call_x_old", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
				{"type": "function_call_output", "call_id": "call_x_old", "output": strings.Repeat("stale x content ", 80)},
				{"type": "function_call", "call_id": "call_y_old", "name": "Read", "arguments": map[string]any{"path": "src/y.go"}},
				{"type": "function_call_output", "call_id": "call_y_old", "output": strings.Repeat("obsolete y content ", 80)},
				{"type": "message", "role": "user", "content": "filler one"},
				{"type": "message", "role": "user", "content": "filler two"},
				{"type": "function_call", "call_id": "call_x_fresh", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
				{"type": "function_call_output", "call_id": "call_x_fresh", "output": "fresh x content"},
				{"type": "function_call", "call_id": "call_y_edit", "name": "apply_patch", "arguments": map[string]any{"path": "src/y.go", "patch": "@@ ..."}},
				{"type": "function_call_output", "call_id": "call_y_edit", "output": "patch applied"},
			},
			"stream": true,
		})
	}

	guardedBody := buildBody("resp-recovered", "history-recovery-lineage-guarded")
	guarded, _, guardedChanged, guardedStats, _, guardedMeta, _ := adapter.applyInputPipelineDetailed(guardedBody)
	if guardedChanged || !bytes.Equal(guarded, guardedBody) {
		t.Fatalf("recovery-lineage full-history mutation must stay guarded: changed=%v body=%s", guardedChanged, guarded)
	}
	if guardedStats.TokensSaved != 0 || guardedMeta.DebugFacts["wss.history_mutation_recovery_guard"] != "true" {
		t.Fatalf("recovery-lineage guard missing: stats=%+v facts=%+v", guardedStats, guardedMeta.DebugFacts)
	}
	adapter.mu.Lock()
	adapter.pendingChain = wssResponseChain{json.RawMessage(`{"type":"message","role":"user","content":"lineage"}`)}
	adapter.pendingHistoryRecoveryGuarded = true
	adapter.mu.Unlock()
	childResponse := parseWSJSON(t, map[string]any{
		"type":     string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{"id": "resp-recovered-child", "output": []any{}},
	})
	adapter.rememberWSSResponseState(&childResponse)
	if !adapter.wssHistoryMutationRecoveryGuarded("resp-recovered-child") {
		t.Fatal("guarded recovery request should propagate guard to child response id")
	}

	unrelatedBody := buildBody("resp-unrelated", "history-recovery-lineage-unrelated")
	unrelated, _, unrelatedChanged, unrelatedStats, _, unrelatedMeta, _ := adapter.applyInputPipelineDetailed(unrelatedBody)
	if !unrelatedChanged || bytes.Equal(unrelated, unrelatedBody) {
		t.Fatalf("unrelated full-history request should keep history savings: changed=%v body=%s", unrelatedChanged, unrelated)
	}
	if unrelatedStats.TokensSaved <= 0 || unrelatedMeta.DebugFacts["wss.history_mutation_recovery_guard"] == "true" {
		t.Fatalf("unrelated request should not inherit recovery guard: stats=%+v facts=%+v", unrelatedStats, unrelatedMeta.DebugFacts)
	}
}

func TestWSPhaseFHistoryDetachMakesFollowingDeltaStatelessFullHistory(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	cfg.Compression.OutputReduce.CodexWSSHistoryMutationLabEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(1)

	fullHistory := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-history-parent",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-stateless-history","source":"desktop"}`,
			},
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "read src/x.go and src/y.go"},
				{"type": "function_call", "call_id": "call_x_old", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
				{"type": "function_call_output", "call_id": "call_x_old", "output": strings.Repeat("stale x content ", 80)},
				{"type": "message", "role": "user", "content": "filler one"},
				{"type": "message", "role": "user", "content": "filler two"},
				{"type": "function_call", "call_id": "call_x_fresh", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
				{"type": "function_call_output", "call_id": "call_x_fresh", "output": "fresh x content"},
				{"type": "function_call_output", "call_id": "call_unknown", "output": "unknown tool output keeps the previous_response guard active"},
			},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &fullHistory); err != nil || !replace {
		t.Fatalf("full-history request should mutate and detach replace=%v err=%v", replace, err)
	}
	fullHistoryBody, _, ok := wsRequestBody(&fullHistory)
	if !ok {
		t.Fatal("mutated full-history body missing")
	}
	if bytes.Contains(fullHistoryBody, []byte("previous_response_id")) ||
		!bytes.Contains(fullHistoryBody, []byte("kind=stale-read")) {
		t.Fatalf("full-history mutation should detach previous_response_id and age stale read: %s", fullHistoryBody)
	}

	itemDone := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseOutputItemDone),
		"item": map[string]any{
			"type":      "function_call",
			"call_id":   "call_next",
			"name":      "exec_command",
			"arguments": `{"cmd":"cat src/y.go"}`,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &itemDone); err != nil || replace {
		t.Fatalf("output item replace=%v err=%v", replace, err)
	}
	completed := parseWSJSON(t, map[string]any{
		"type":     string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{"id": "resp-history-child", "output": []any{}},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &completed); err != nil || replace {
		t.Fatalf("completion replace=%v err=%v", replace, err)
	}

	delta := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-history-child",
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"thread_id":"thread-stateless-history","source":"desktop"}`,
			},
			"input": []map[string]any{{
				"type":    "function_call_output",
				"call_id": "call_next",
				"output":  "new y output",
			}},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &delta); err != nil || !replace {
		t.Fatalf("delta should be proactively rewritten as stateless full history replace=%v err=%v", replace, err)
	}
	deltaBody, _, ok := wsRequestBody(&delta)
	if !ok {
		t.Fatal("rewritten delta body missing")
	}
	if bytes.Contains(deltaBody, []byte("previous_response_id")) {
		t.Fatalf("stateless continuation must drop previous_response_id: %s", deltaBody)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(deltaBody, &raw); err != nil {
		t.Fatalf("delta body json: %v", err)
	}
	var input []json.RawMessage
	if err := json.Unmarshal(raw["input"], &input); err != nil {
		t.Fatalf("delta input json: %v", err)
	}
	if len(input) <= 1 {
		t.Fatalf("stateless continuation must send full history, got %d input items: %s", len(input), deltaBody)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 ||
		summaries[0].DebugFacts["wss.stateless_history_continuation"] != "true" {
		t.Fatalf("missing stateless continuation debug fact: %+v", summaries)
	}
}

func TestWSPhaseFRequestCompactsCodexToolOutputLayer0(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var status strings.Builder
	for i := 0; i < 120; i++ {
		status.WriteString(" M internal/proxy/wss_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	body := mustMarshal(map[string]any{
		"model":           "gpt-5-codex",
		"conversation_id": "conv-layer0-wss",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "check git status"},
			{"type": "function_call", "call_id": "call_status", "name": "shell", "arguments": map[string]any{"command": "git status --short"}},
			{"type": "function_call_output", "call_id": "call_status", "output": status.String()},
		},
		"stream": true,
	})

	mutated, _, changed, _, _ := adapter.applyInputPipeline(body)
	if !changed {
		t.Fatal("expected WSS Layer 0 compaction")
	}
	if !strings.Contains(string(mutated), "[git status]") || strings.Contains(string(mutated), "wss_119.go") {
		t.Fatalf("tool output was not compacted: %s", mutated)
	}
	snap := p.OutputReduceCountersSnapshot()
	if snap.ProxyLayer0RequestsModified != 1 || snap.ProxyLayer0TokensSaved == 0 {
		t.Fatalf("Layer 0 counters not recorded: %+v", snap)
	}
}

func TestWSPhaseFRequestSingleReconstructForStagedMutations(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	cfg.Compression.Tuning.ToolPruneEnabled = true
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	const sessionID = "codex-wss:single-reconstruct-session"
	p.toolPrune.ObserveTurn(sessionID, []string{"Read", "ColdTool"})
	p.toolPrune.ObserveTurn(sessionID, []string{"Read"})
	body := codexWSBodyWithTools(t, codexWSStaleObsoleteLayer0Body(),
		codexToolDefinition("Read", "Read files"),
		codexToolDefinition("ColdTool", strings.Repeat("Idle expensive schema. ", 80)),
	)

	origReconstruct := reconstructBodyFn
	origExtract := extractMessagesFn
	reconstructCalls := 0
	extractCalls := 0
	reconstructBodyFn = func(provider types.Provider, originalBody []byte, messages []types.Message) ([]byte, error) {
		reconstructCalls++
		return origReconstruct(provider, originalBody, messages)
	}
	extractMessagesFn = func(provider types.Provider, body []byte) ([]types.Message, map[string]json.RawMessage, error) {
		extractCalls++
		return origExtract(provider, body)
	}
	defer func() {
		reconstructBodyFn = origReconstruct
		extractMessagesFn = origExtract
	}()

	mutated, _, changed, l0Stats, _ := adapter.applyInputPipeline(body)
	mutatedText := string(mutated)
	if !changed {
		t.Fatal("expected staged WSS mutations")
	}
	if reconstructCalls != 1 {
		t.Fatalf("expected exactly one reconstruct for staged mutations, got %d", reconstructCalls)
	}
	if extractCalls != 1 {
		t.Fatalf("expected exactly one extract for staged mutations, got %d", extractCalls)
	}
	if strings.Contains(mutatedText, "stale x content") || !strings.Contains(mutatedText, "kind=stale-read") {
		t.Fatalf("stale-read mutation missing: %s", mutatedText)
	}
	if strings.Contains(mutatedText, "obsolete y content") || !strings.Contains(mutatedText, "kind=obsolete-read") {
		t.Fatalf("obsolete-read mutation missing: %s", mutatedText)
	}
	if !strings.Contains(mutatedText, "[git status]") || strings.Contains(mutatedText, "single_reconstruct_179.go") {
		t.Fatalf("Layer 0 mutation missing: %s", mutatedText)
	}
	if strings.Contains(mutatedText, "ColdTool") || !strings.Contains(mutatedText, "Read") {
		t.Fatalf("tool-prune did not compose with the single reconstruct: %s", mutatedText)
	}
	if l0Stats.TokensSaved == 0 || l0Stats.CapturedOutputBlocks == 0 {
		t.Fatalf("expected Layer 0 savings stats, got %+v", l0Stats)
	}
	snap := p.OutputReduceCountersSnapshot()
	if snap.StaleReadBlocksReplaced == 0 || snap.ObsoleteReadBlocksPruned == 0 || snap.ProxyLayer0RequestsModified == 0 {
		t.Fatalf("expected all staged mutation counters, got %+v", snap)
	}
	toolSnap := p.toolPrune.Snapshot()
	if toolSnap.PrunedTotal != 1 || toolSnap.TokensSavedSum <= 0 {
		t.Fatalf("expected same-request tool-prune savings, got %+v", toolSnap)
	}
	if l0Stats.StaleReadBlocks == 0 || l0Stats.StaleReadTokensSaved <= 0 ||
		l0Stats.ObsoletePruneBlocks == 0 || l0Stats.ObsoletePruneTokensSaved <= 0 {
		t.Fatalf("expected request-local history reducer stats, got %+v", l0Stats)
	}
	if !hasEvidenceDecision(l0Stats.EvidenceDecisions, proxyLayer0MechanismStaleRead, "positive_net_savings", evidence.ActionApplied) ||
		!hasEvidenceDecision(l0Stats.EvidenceDecisions, proxyLayer0MechanismObsoletePrune, "positive_net_savings", evidence.ActionApplied) {
		t.Fatalf("expected request-local history evidence decisions, got %+v", l0Stats.EvidenceDecisions)
	}
}

func codexWSBodyWithTools(t *testing.T, body []byte, tools ...map[string]any) []byte {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("fixture body json: %v", err)
	}
	raw["tools"] = tools
	return mustMarshal(raw)
}

func hasEvidenceDecision(decisions []evidence.BlockDecision, mechanism proxyLayer0Mechanism, reason string, action evidence.Action) bool {
	for _, decision := range decisions {
		if decision.Mechanism == string(mechanism) && decision.Reason == reason && decision.Action == action {
			return true
		}
	}
	return false
}

func TestWSPhaseFStaleObsoletePreserveToolUseIndex(t *testing.T) {
	messages, _, err := extractMessages(types.CodexChatGPT, codexWSStaleObsoleteLayer0Body())
	if err != nil {
		t.Fatalf("extract messages: %v", err)
	}
	before := proxyToolUseIndex(messages)
	if len(before) == 0 {
		t.Fatal("precondition: fixture has no tool uses")
	}
	aged, staleStats := staleread.AgeMessages(messages, staleread.Options{MinTurnGap: 2})
	if staleStats.BlocksReplaced == 0 {
		t.Fatal("precondition: fixture did not produce stale-read mutation")
	}
	pruned, obsoleteStats := staleread.PruneObsoleteReads(aged, staleread.ObsoleteOptions{})
	if obsoleteStats.BlocksReplaced == 0 {
		t.Fatal("precondition: fixture did not produce obsolete-read mutation")
	}
	after := proxyToolUseIndex(pruned)
	assertSameProxyToolUseIndex(t, before, after)
}

func TestWSPhaseFWrapperObserversUseBodySessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Defaults()
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	body := mustMarshal(map[string]any{
		"model":                "gpt-5-codex",
		"prompt_cache_key":     "wrapper-observer-session",
		"previous_response_id": "resp-wrapper",
		"input": []map[string]any{
			{"type": "function_call", "call_id": "call_edit", "name": "apply_patch", "arguments": map[string]any{"path": "src/wrapped.go", "patch": "@@ ..."}},
			{"type": "function_call_output", "call_id": "call_edit", "output": "patch applied"},
			{"type": "function_call", "call_id": "call_read", "name": "read_file", "arguments": map[string]any{"path": "src/wrapped.go"}},
			{"type": "function_call_output", "call_id": "call_read", "output": strings.Repeat("wrapped file content\n", 20)},
		},
		"stream": true,
	})
	messages, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatalf("extract messages: %v", err)
	}

	adapter.observeWSSRecentEdits(body, messages, nil)
	hit, err := sessions.RecentlyEditedHookFile(sessions.DefaultHookStateDir(home), "codex-wss:wrapper-observer-session", "src/wrapped.go", 2)
	if err != nil || !hit {
		t.Fatalf("wrapper recent-edit observer did not use body session id, hit=%v err=%v", hit, err)
	}
	first, firstCount := adapter.observeWSSQualityToolKeys(body, messages, nil)
	second, secondCount := adapter.observeWSSQualityToolKeys(body, messages, nil)
	if len(first) != 0 || firstCount != 0 || len(second) == 0 || secondCount == 0 {
		t.Fatalf("wrapper quality-key observer should detect reread on second call, first=%v/%d second=%v/%d", first, firstCount, second, secondCount)
	}
}

func TestWSSPreviousResponseAndSourceRiskPredicates(t *testing.T) {
	if !wssPreviousResponseIDAvailable([]byte(`{"previous_response_id":"resp_1"}`)) {
		t.Fatal("previous_response_id should be available")
	}
	if wssPreviousResponseIDAvailable([]byte(`{"input":[]}`)) {
		t.Fatal("missing previous_response_id should not be available")
	}
	sourceMessages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type: "tool_result",
			Text: strings.Repeat("package main\nfunc main() {}\n", 220),
		}},
	}}
	if !wssRiskyPreviousResponseSourceToolOutput(wssRequestMeta{PreviousResponseID: "resp_1"}, sourceMessages) {
		t.Fatal("large source-like tool output after previous_response_id should be risky")
	}
	if wssRiskyPreviousResponseSourceToolOutput(wssRequestMeta{}, sourceMessages) {
		t.Fatal("source-risk predicate is previous_response_id-specific")
	}
}

func TestWSSRequestIndexesMatchStandaloneBuilders(t *testing.T) {
	messages, _, err := extractMessages(types.CodexChatGPT, codexWSStaleObsoleteLayer0Body())
	if err != nil {
		t.Fatalf("extract messages: %v", err)
	}
	gotToolUses, gotRepdet := wssRequestIndexes(messages, true)
	assertSameProxyToolUseIndex(t, proxyToolUseIndex(messages), gotToolUses)
	wantRepdet := buildRepdetIndex(messages)
	if gotRepdet == nil {
		t.Fatal("expected repdet index")
	}
	gotBlocks := gotRepdet.Blocks()
	wantBlocks := wantRepdet.Blocks()
	if len(gotBlocks) != len(wantBlocks) {
		t.Fatalf("repdet block count changed got=%d want=%d", len(gotBlocks), len(wantBlocks))
	}
	for i := range wantBlocks {
		if gotBlocks[i] != wantBlocks[i] {
			t.Fatalf("repdet block %d changed got=%+v want=%+v", i, gotBlocks[i], wantBlocks[i])
		}
	}

	_, disabled := wssRequestIndexes(messages, false)
	if disabled != nil {
		t.Fatal("repdet index should not be built when disabled")
	}
}

func TestWSPhaseFRepdetIndexesForwardedMessagesAfterMutation(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var originalStatus strings.Builder
	for i := 0; i < 180; i++ {
		fmt.Fprintf(&originalStatus, " M internal/proxy/repdet_forwarded_original_%03d.go\n", i)
	}
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "repdet-forwarded-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "check git status"},
				{"type": "function_call", "call_id": "call_status", "name": "exec_command", "arguments": map[string]any{"cmd": "git status --short"}},
				{"type": "function_call_output", "call_id": "call_status", "output": originalStatus.String()},
			},
			"stream": true,
		},
	})
	replaced, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if !replaced {
		t.Fatal("precondition: request should be compacted")
	}
	if bytes.Contains(env.Body, []byte("repdet_forwarded_original_179.go")) || !bytes.Contains(env.Body, []byte("[git status]")) {
		t.Fatalf("precondition: request did not forward compacted status output: %s", env.Body)
	}

	delta := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": originalStatus.String(),
	})
	replaced, err = adapter.handle(context.Background(), wsmitm.DirServerToClient, &delta)
	if err != nil {
		t.Fatalf("handle delta: %v", err)
	}
	if replaced || strings.Contains(delta.Delta, "[unchanged:") {
		t.Fatalf("repdet index must match forwarded compacted request, not original output: replaced=%v delta=%q", replaced, delta.Delta)
	}
}

func TestWSPhaseFResponseCreateInfersUnresolvedToolOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	payload.WriteString("=== RUN   TestSlimferenceFailure\n")
	payload.WriteString("    fail_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
	payload.WriteString("--- FAIL: TestSlimferenceFailure (0.00s)\n")
	payload.WriteString("FAIL\texample.test/liveproof\t0.015s\n")
	envelope := "Chunk ID: inferred\nWall time: 0.0000 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" + payload.String()
	turnMeta := `{"session_id":"sess-response-create","thread_id":"sess-response-create","turn_id":"turn-1"}`
	body := mustMarshal(map[string]any{
		"type":                   "response.create",
		"model":                  "gpt-5.5",
		"instructions":           strings.Repeat("stable instruction prefix ", 200),
		"prompt_cache_key":       "sess-response-create",
		"prompt_cache_retention": "24h",
		"generate":               false,
		"include":                []string{"reasoning.encrypted_content"},
		"tools": []map[string]any{{
			"type":        "function",
			"name":        "exec_command",
			"description": strings.Repeat("stable exec tool schema ", 80),
		}},
		"client_metadata": map[string]any{
			"x-codex-turn-metadata": turnMeta,
		},
		"input": []map[string]any{
			{"type": "function_call_output", "call_id": "call_missing", "output": envelope},
		},
	})

	mutated, _, changed, stats, _ := adapter.applyInputPipeline(body)
	if !changed || stats.CodexExecEnvelopeBlocks != 1 || stats.TokensSaved <= 0 {
		t.Fatalf("expected response.create tool-output inference, changed=%v stats=%+v body=%s", changed, stats, mutated)
	}
	if !strings.Contains(string(mutated), "SLIMFERENCE_TEST_FAILURE_SENTINEL") ||
		strings.Contains(string(mutated), "TestPassing089") ||
		!strings.Contains(string(mutated), "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("mutated response.create lost failure detail or archive: %s", mutated)
	}
}

func TestWSPhaseFRequestCompactsCodexResponseItemPayloadLayer0(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var status strings.Builder
	for i := 0; i < 120; i++ {
		status.WriteString(" M internal/proxy/wrapped_wss_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	body := mustMarshal(map[string]any{
		"model":           "gpt-5-codex",
		"conversation_id": "conv-layer0-wss-wrapper",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "check git status"},
			{"type": "response_item", "payload": map[string]any{
				"type":      "function_call",
				"call_id":   "call_status",
				"name":      "exec_command",
				"arguments": map[string]any{"cmd": "git status --short"},
			}},
			{"type": "response_item", "payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call_status",
				"output":  status.String(),
			}},
		},
		"stream": true,
	})

	mutated, _, changed, _, _ := adapter.applyInputPipeline(body)
	if !changed {
		t.Fatal("expected WSS Layer 0 compaction for response_item payload")
	}
	if !strings.Contains(string(mutated), "[git status]") || strings.Contains(string(mutated), "wrapped_wss_119.go") {
		t.Fatalf("wrapped tool output was not compacted: %s", mutated)
	}
	var out struct {
		Input []struct {
			Type    string `json:"type"`
			Payload struct {
				Output string `json:"output"`
			} `json:"payload"`
			Output string `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(mutated, &out); err != nil {
		t.Fatal(err)
	}
	if out.Input[2].Type != "response_item" || !strings.Contains(out.Input[2].Payload.Output, "[git status]") {
		t.Fatalf("wrapper payload was not preserved and rewritten: %s", mutated)
	}
	if out.Input[2].Output != "" {
		t.Fatalf("wrapper top-level output must stay absent: %s", mutated)
	}
	snap := p.OutputReduceCountersSnapshot()
	if snap.ProxyLayer0RequestsModified != 1 || snap.ProxyLayer0TokensSaved == 0 {
		t.Fatalf("Layer 0 counters not recorded: %+v", snap)
	}
}

func TestWSPhaseFRequestCompactsToolOutputAcrossResponsesRequests(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	callEnv := parseWSJSON(t, map[string]any{
		"model": "gpt-5-codex",
		"input": []map[string]any{
			{"type": "response_item", "payload": map[string]any{
				"type":      "function_call",
				"call_id":   "call_status",
				"name":      "exec_command",
				"arguments": map[string]any{"cmd": "git -C /tmp/slimf-l0-live status --short"},
			}},
		},
		"stream": true,
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &callEnv); err != nil || replace {
		t.Fatalf("function-call-only request should only seed state, replace=%v err=%v", replace, err)
	}

	var status strings.Builder
	for i := 0; i < 120; i++ {
		status.WriteString("?? synthetic_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	outputEnv := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "wss-cross-request-compaction",
		"input": []map[string]any{
			{"type": "response_item", "payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call_status",
				"output":  status.String(),
			}},
		},
		"stream": true,
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &outputEnv)
	if err != nil {
		t.Fatalf("tool-output request handle: %v", err)
	}
	if !replace {
		t.Fatal("expected cross-request WSS Layer 0 compaction")
	}
	if !strings.Contains(string(outputEnv.Raw), "[git status]") ||
		!strings.Contains(string(outputEnv.Raw), "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(string(outputEnv.Raw), "synthetic_119.go") {
		t.Fatalf("cross-request tool output was not compacted: %s", outputEnv.Raw)
	}
	snap := p.OutputReduceCountersSnapshot()
	if snap.ProxyLayer0RequestsModified != 1 || snap.ProxyLayer0TokensSaved == 0 {
		t.Fatalf("Layer 0 counters not recorded: %+v", snap)
	}
	telemetry := adapter.snapshot()
	if telemetry.RequestsSeen != 2 || telemetry.RequestMessagesIndexed != 2 || telemetry.Mutations != 1 {
		t.Fatalf("unexpected adapter telemetry: %+v", telemetry)
	}
}

func TestWSPhaseFRequestCompactsToolOutputAfterServerToolCallItem(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	itemDone := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseOutputItemDone),
		"item": map[string]any{
			"type":      "function_call",
			"call_id":   "call_status",
			"name":      "exec_command",
			"arguments": map[string]any{"cmd": "git -C /tmp/slimf-l0-live status --short"},
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &itemDone); err != nil || replace {
		t.Fatalf("server tool item should only seed state, replace=%v err=%v", replace, err)
	}

	var status strings.Builder
	for i := 0; i < 120; i++ {
		status.WriteString("?? server_synthetic_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	outputEnv := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "wss-server-seeded-compaction",
		"input": []map[string]any{
			{"type": "response_item", "payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call_status",
				"output":  status.String(),
			}},
		},
		"stream": true,
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &outputEnv)
	if err != nil {
		t.Fatalf("tool-output request handle: %v", err)
	}
	if !replace {
		t.Fatal("expected WSS Layer 0 compaction from server-side tool call item")
	}
	if !strings.Contains(string(outputEnv.Raw), "[git status]") ||
		!strings.Contains(string(outputEnv.Raw), "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(string(outputEnv.Raw), "server_synthetic_119.go") {
		t.Fatalf("server-seeded tool output was not compacted: %s", outputEnv.Raw)
	}
	if snap := p.OutputReduceCountersSnapshot(); snap.ProxyLayer0RequestsModified != 1 || snap.ProxyLayer0TokensSaved == 0 {
		t.Fatalf("Layer 0 counters not recorded: %+v", snap)
	}
}

func TestWSPhaseFDefaultGatesPreviousResponseGitStatusAfterServerToolCall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	itemDone := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseOutputItemDone),
		"item": map[string]any{
			"type":      "function_call",
			"call_id":   "call_status",
			"name":      "exec_command",
			"arguments": map[string]any{"cmd": "git -C /tmp/slimf-l0-live status --short"},
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &itemDone); err != nil || replace {
		t.Fatalf("server tool item should only seed state, replace=%v err=%v", replace, err)
	}

	var status strings.Builder
	for i := 0; i < 120; i++ {
		status.WriteString("?? previous_response_status_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	outputEnv := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-status",
		"prompt_cache_key":     "wss-server-seeded-compaction",
		"input": []map[string]any{
			{"type": "response_item", "payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call_status",
				"output":  status.String(),
			}},
		},
		"stream": true,
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &outputEnv)
	if err != nil {
		t.Fatalf("tool-output request handle: %v", err)
	}
	if replace {
		t.Fatalf("default previous_response delta git status output must not mutate: %s", outputEnv.Raw)
	}
	if strings.Contains(string(outputEnv.Raw), "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(string(outputEnv.Raw), "previous_response_status_119.go") {
		t.Fatalf("previous_response git status output should stay byte-equal under delta gate: %s", outputEnv.Raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	gated := false
	for _, decision := range summary.EvidenceDecisions {
		if decision.Reason == "wss_stateful_delta_mutation_proof_gate" {
			gated = true
		}
	}
	if summary.BypassReason != "" || !summary.PreviousResponseIDUsed || summary.Tokens.Saved != 0 || !gated {
		t.Fatalf("expected previous_response delta proof-gate summary, got %+v", summary)
	}
	if snap := p.OutputReduceCountersSnapshot(); snap.ProxyLayer0RequestsModified != 0 || snap.ProxyLayer0TokensSaved != 0 {
		t.Fatalf("Layer 0 counters should not record a gated mutation: %+v", snap)
	}
}

func TestWSPhaseFKnownPreviousResponseReadKeepsLayer0Savings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	seedToolCall := func(callID string) {
		itemDone := parseWSJSON(t, map[string]any{
			"type": string(wsmitm.FrameKindResponseOutputItemDone),
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      "exec_command",
				"arguments": map[string]any{"cmd": "cat docs/spec.md"},
			},
		})
		if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &itemDone); err != nil || replace {
			t.Fatalf("server read tool item should only seed state, replace=%v err=%v", replace, err)
		}
	}
	runOutput := func(callID string) (bool, []byte) {
		env := parseWSJSON(t, map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-known-read",
			"prompt_cache_key":     "known-read-session",
			"input": []map[string]any{{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  "Chunk ID: " + callID + "\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + uniqueProxyReadPayload("known previous response read"),
			}},
			"stream": true,
		})
		replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
		if err != nil {
			t.Fatalf("tool-output request handle: %v", err)
		}
		return replace, []byte(env.Raw)
	}

	seedToolCall("call_read_1")
	if replace, raw := runOutput("call_read_1"); replace {
		t.Fatalf("first known source read should seed only, raw=%s", raw)
	}
	firstSummary := p.DebugRecorder().Last(1, false)[0]
	if firstSummary.BypassReason != "" ||
		firstSummary.DebugFacts["wss.structured_mutation_guard"] != "wss_stateful_structured_mutation_guard" ||
		firstSummary.DebugFacts["wss.tool_results_resolved"] != "1" {
		t.Fatalf("first known read should be guarded but not full-pass-bypassed: %+v", firstSummary)
	}

	// Live A/B 2026-06-11 (loop runs 4-8): a read_delta-collapsed delta turn
	// made the FOLLOWING tool turn fail upstream with 400 (bridge control
	// clean), so previous_response_id delta turns full-pass by default and
	// the suppressed mutation carries the proof-gate evidence reason.
	seedToolCall("call_read_2")
	replace, raw := runOutput("call_read_2")
	if replace || bytes.Contains(raw, []byte("[context-elided kind=file-read status=unchanged")) {
		t.Fatalf("second known read must not mutate the delta turn by default: %s", raw)
	}
	secondSummary := p.DebugRecorder().Last(1, false)[0]
	gated := false
	for _, decision := range secondSummary.EvidenceDecisions {
		if decision.Reason == "wss_stateful_delta_mutation_proof_gate" {
			gated = true
		}
	}
	if secondSummary.BypassReason != "" || !gated ||
		secondSummary.DebugFacts["wss.tool_results_resolved"] != "1" ||
		secondSummary.DebugFacts["wss.effective_mutation_guard"] != "wss_stateful_delta_mutation_proof_gate" ||
		secondSummary.DebugFacts["wss.stateful_delta_mutation_blocked"] != "true" {
		t.Fatalf("second known read should carry the proof-gate reason: %+v", secondSummary)
	}
}

func TestWSPhaseFStatefulResolvedToolOutputCompactsWithArchiveWhenDeltaLabEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	// Explicit experimental opt-in: live A/B (2026-06-11) showed stateful WSS
	// structured mutation triggers upstream 400 on follow-up turns, so the
	// default keeps the guard; this test covers the mechanic behind the flag.
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	cfg.Compression.OutputReduce.CodexWSSDeltaToolOutputMutationLabEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	payload.WriteString("=== RUN   TestSlimferenceFailure\n")
	payload.WriteString("    fail_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
	payload.WriteString("--- FAIL: TestSlimferenceFailure (0.00s)\n")
	payload.WriteString("FAIL\texample.test/liveproof\t0.015s\n")
	envelope := "Chunk ID: stateful\nWall time: 0.0000 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" + payload.String()
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-stateful-resolved",
		"prompt_cache_key":     "stateful-resolved-session",
		"input": []map[string]any{
			{"type": "function_call", "call_id": "call_tests", "name": "exec_command", "arguments": map[string]any{"cmd": "go test ./..."}},
			{"type": "function_call_output", "call_id": "call_tests", "output": envelope},
		},
		"stream": true,
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("stateful resolved tool-output handle: %v", err)
	}
	if !replace {
		t.Fatalf("delta lab stateful resolved tool output should compact: %s", env.Raw)
	}
	mutated := string(env.Raw)
	if !strings.Contains(mutated, "SLIMFERENCE_TEST_FAILURE_SENTINEL") ||
		strings.Contains(mutated, "TestPassing089") ||
		!strings.Contains(mutated, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("stateful compaction lost failure detail or archive reference: %s", mutated)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.structured_mutation_guard"] != "" || summary.Tokens.Saved <= 0 {
		t.Fatalf("resolved archived mutation must save without the structured guard: %+v", summary)
	}
}

func TestWSPhaseFBroadToolOutputMutationFlagDoesNotBypassDeltaGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	payload.WriteString("=== RUN   TestSlimferenceFailure\n")
	payload.WriteString("    fail_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
	payload.WriteString("--- FAIL: TestSlimferenceFailure (0.00s)\n")
	payload.WriteString("FAIL\texample.test/liveproof\t0.015s\n")
	envelope := "Chunk ID: broad-delta-gate\nWall time: 0.0000 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" + payload.String()
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-broad-delta-gate",
		"prompt_cache_key":     "broad-delta-gate-session",
		"input": []map[string]any{
			{"type": "function_call_output", "call_id": "call_tests", "output": envelope},
		},
		"stream": true,
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("broad flag stateful delta handle: %v", err)
	}
	if replace ||
		strings.Contains(string(env.Raw), "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(string(env.Raw), "PASS lines elided") {
		t.Fatalf("broad tool-output flag must not mutate previous_response_id delta output: %s", env.Raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	gated := false
	for _, decision := range summary.EvidenceDecisions {
		if decision.Reason == "wss_stateful_delta_mutation_proof_gate" {
			gated = true
		}
	}
	if !gated || summary.DebugFacts["wss.request_shape"] != "delta" ||
		summary.DebugFacts["wss.effective_mutation_guard"] != "wss_stateful_delta_mutation_proof_gate" ||
		summary.DebugFacts["wss.stateful_delta_mutation_blocked"] != "true" {
		t.Fatalf("broad flag delta turn should carry proof-gate evidence: %+v", summary)
	}
}

func TestWSPhaseFDefaultStatefulUnresolvedToolOutputKeepsGuard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&payload, "opaque worker line %03d without any tool shape markers\n", i)
	}
	envelope := "Chunk ID: unresolved\nWall time: 0.0000 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" + payload.String()
	env := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "stateful-unresolved-session",
		"input": []map[string]any{
			{"type": "function_call_output", "call_id": "call_never_seeded", "output": envelope},
		},
		"stream": true,
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("stateful unresolved tool-output handle: %v", err)
	}
	if strings.Contains(string(env.Raw), "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("unresolved stateful output must not be structurally mutated: %s", env.Raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.structured_mutation_guard"] != "wss_stateful_structured_mutation_guard" {
		t.Fatalf("unresolved stateful output should keep the structured guard, replace=%v facts=%+v", replace, summary.DebugFacts)
	}
}

func TestWSPhaseFDefaultStatefulResolvedToolOutputKeepsGuard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	payload.WriteString("PASS\nok  \texample.test/liveproof\t0.015s\n")
	envelope := "Chunk ID: default-guarded\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" + payload.String()
	itemDone := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseOutputItemDone),
		"item": map[string]any{
			"type":      "function_call",
			"call_id":   "call_tests",
			"name":      "exec_command",
			"arguments": map[string]any{"cmd": "go test ./... -v"},
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &itemDone); err != nil || replace {
		t.Fatalf("server tool item should only seed state, replace=%v err=%v", replace, err)
	}
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-default-guarded",
		"prompt_cache_key":     "default-guarded-session",
		"input": []map[string]any{
			{"type": "function_call_output", "call_id": "call_tests", "output": envelope},
		},
		"stream": true,
	})

	// Live A/B 2026-06-11 (loop runs 4-7): structured mutation on the stateful
	// WSS delta flow caused upstream 400s on follow-up turns while the
	// byte-equal bridge stayed clean. The default must keep the guard even for
	// fully resolved, archive-recoverable outputs.
	if _, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env); err != nil {
		t.Fatalf("default stateful tool-output handle: %v", err)
	}
	if strings.Contains(string(env.Raw), "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(string(env.Raw), "PASS lines elided") {
		t.Fatalf("default must not structurally mutate stateful WSS tool output: %s", env.Raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.structured_mutation_guard"] != "wss_stateful_structured_mutation_guard" ||
		summary.DebugFacts["wss.request_shape"] != "delta" ||
		summary.DebugFacts["wss.delta_shape"] != "true" {
		t.Fatalf("default must keep the structured guard: %+v", summary.DebugFacts)
	}
}

func TestWSPhaseFStatefulInferredToolOutputCompactsWithArchiveWhenDeltaLabEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	cfg.Compression.OutputReduce.CodexWSSDeltaToolOutputMutationLabEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	payload.WriteString("=== RUN   TestSlimferenceFailure\n")
	payload.WriteString("    fail_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
	payload.WriteString("--- FAIL: TestSlimferenceFailure (0.00s)\n")
	payload.WriteString("FAIL\texample.test/liveproof\t0.015s\n")
	envelope := "Chunk ID: inferred-stateful\nWall time: 0.0000 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" + payload.String()
	// call_id was never seeded on this socket and is not in the tool-use cache:
	// the evicted/reconnect/never-seen class. The payload shape alone resolves
	// the command class, which must be enough for the archive-backed reducer.
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-inferred-stateful",
		"prompt_cache_key":     "inferred-stateful-session",
		"input": []map[string]any{
			{"type": "function_call_output", "call_id": "call_evicted", "output": envelope},
		},
		"stream": true,
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("stateful inferred tool-output handle: %v", err)
	}
	if !replace {
		t.Fatalf("delta lab inferable stateful tool output should compact: %s", env.Raw)
	}
	mutated := string(env.Raw)
	if !strings.Contains(mutated, "SLIMFERENCE_TEST_FAILURE_SENTINEL") ||
		strings.Contains(mutated, "TestPassing089") ||
		!strings.Contains(mutated, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("inferred compaction lost failure detail or archive reference: %s", mutated)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.BypassReason != "" ||
		summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.tool_results_inferred"] != "1" ||
		summary.Tokens.Saved <= 0 {
		t.Fatalf("inferred mutation must save without guard or bypass: %+v", summary)
	}
}

func TestWSPhaseFAttachesProviderUsageToDecisionRecord(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "usage-attribution-session",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "hello"},
		},
		"stream": true,
	})
	if _, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env); err != nil {
		t.Fatalf("request handle: %v", err)
	}

	done := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{
			"id": "resp-usage-1",
			"usage": map[string]any{
				"input_tokens":         29093,
				"input_tokens_details": map[string]any{"cached_tokens": 3456},
				"output_tokens":        240,
			},
		},
	})
	if _, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &done); err != nil {
		t.Fatalf("response handle: %v", err)
	}

	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.ProviderInputTokens != 29093 ||
		summary.ProviderCachedTokens != 3456 ||
		summary.ProviderOutputTokens != 240 {
		t.Fatalf("decision record must carry provider usage: %+v", summary)
	}
	if summary.Flight == nil || summary.Flight.TokenAccounting.ProviderCachedTokens != 3456 {
		t.Fatalf("flight must carry provider cached tokens: %+v", summary.Flight)
	}
}

func TestWSPhaseFDefaultUnknownPreviousResponseToolOutputFullPasses(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	output := strings.Repeat("?? unknown_previous_response.go\n", 240)
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-unknown",
		"prompt_cache_key":     "unknown-previous-response-session",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_missing",
			"output":  output,
		}},
		"stream": true,
	})
	if replace := adapter.handleRequest(&env); replace {
		t.Fatalf("unknown previous_response tool output must full-pass: %s", env.Raw)
	}
	if !strings.Contains(string(env.Raw), "unknown_previous_response.go") || strings.Contains(string(env.Raw), "[git status]") {
		t.Fatalf("unknown tool output was unexpectedly compacted: %s", env.Raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.BypassReason != "wss_previous_response_tool_output_full_pass" || summary.Tokens.Saved != 0 {
		t.Fatalf("unknown previous_response output should be no-savings full-pass: %+v", summary)
	}
}

func TestWSPhaseFPreviousResponseMixedUnknownToolOutputObservesInferableDelta(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var testOutput strings.Builder
	testOutput.WriteString("Chunk ID: mixed-observe\nWall time: 0.0000 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&testOutput, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	testOutput.WriteString("=== RUN   TestSlimferenceFailure\n")
	testOutput.WriteString("    fail_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
	testOutput.WriteString("--- FAIL: TestSlimferenceFailure (0.00s)\n")
	testOutput.WriteString("FAIL\texample.test/liveproof\t0.015s\n")

	opaqueOutput := strings.Repeat("opaque worker line without deterministic command shape\n", 80)
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-mixed-observe",
		"prompt_cache_key":     "mixed-observe-session",
		"input": []map[string]any{
			{"type": "function_call_output", "call_id": "call_tests_evicted", "output": testOutput.String()},
			{"type": "function_call_output", "call_id": "call_unknown", "output": opaqueOutput},
		},
		"stream": true,
	})
	original := append([]byte(nil), env.Raw...)

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("mixed previous_response handle: %v", err)
	}
	if replace || !bytes.Equal(env.Raw, original) {
		t.Fatalf("mixed unknown previous_response delta must stay byte-equal, replace=%v raw=%s", replace, env.Raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.BypassReason != "wss_previous_response_tool_output_full_pass" ||
		summary.Tokens.Saved != 0 ||
		summary.MessagesCompressed != 0 ||
		summary.DebugFacts["wss.changed"] != "false" ||
		summary.DebugFacts["wss.tool_results_inferred"] != "1" ||
		summary.DebugFacts["wss.tool_results_total"] != "2" {
		t.Fatalf("mixed bypass should stay no-savings but carry inference facts: %+v", summary)
	}
	if !hasEvidenceDecision(summary.EvidenceDecisions, proxyLayer0MechanismRepeatedOut, "wss_stateful_delta_mutation_proof_gate", evidence.ActionFullPass) ||
		!hasEvidenceDecision(summary.EvidenceDecisions, proxyLayer0MechanismCapturedOut, "wss_stateful_delta_mutation_proof_gate", evidence.ActionFullPass) {
		t.Fatalf("inferable block should emit observe-only proof-gate evidence: %+v", summary.EvidenceDecisions)
	}
	if len(summary.EvidenceDecisions) == 0 || len(summary.Mechanisms) == 0 {
		t.Fatalf("mixed bypass should no longer be invisible to local-gap accounting: %+v", summary)
	}
}

func TestMergeWSSLayer0ObservationStatsPreservesObserveOnlyTelemetry(t *testing.T) {
	base := proxyLayer0Stats{
		ToolResultBlocks:        1,
		ReadDeltaAttempts:       1,
		ToolResultBytes:         10,
		TokensSaved:             11,
		BlocksModified:          1,
		ReadDeltaBlocks:         1,
		CapturedOutputBlocks:    1,
		ChunkDedupBlocks:        1,
		ChunkDedupReferences:    1,
		ChunkDedupRefBytes:      12,
		ChunkDedupInputBytes:    13,
		StaleReadBlocks:         1,
		StaleReadBytesSaved:     14,
		StaleReadTokensSaved:    15,
		ReadDeltaKeys:           []string{"base-key"},
		PolicyDecisions:         []savingspolicy.CodexMechanismDecision{{Reason: "base-policy"}},
		CacheEvents:             []proxyLayer0CacheEvent{{Action: proxyLayer0CacheHit, Reason: "base-cache"}},
		EvidenceDecisions:       []evidence.BlockDecision{{Mechanism: string(proxyLayer0MechanismReadDelta), Reason: "base-evidence", Action: evidence.ActionApplied}},
		TotalLatencyNs:          16,
		ReadDeltaLatencyNs:      17,
		FilterLatencyNs:         18,
		RepeatedOutputLatencyNs: 19,
		ChunkDedupLatencyNs:     20,
	}
	observed := proxyLayer0Stats{
		Route:                    codexLayer0RouteWSSPhaseF,
		ToolResultBlocks:         2,
		ToolUseUnresolvedBlocks:  3,
		CommandResolvedBlocks:    4,
		CommandUnresolvedBlocks:  5,
		ReadDeltaAttempts:        6,
		ReadDeltaMisses:          7,
		ToolResultBytes:          8,
		TokensSaved:              9,
		BlocksModified:           10,
		ReadDeltaBlocks:          11,
		CapturedOutputBlocks:     12,
		CodexExecEnvelopeBlocks:  13,
		RepeatedOutputBlocks:     14,
		ChunkDedupBlocks:         15,
		ChunkDedupReferences:     16,
		ChunkDedupRefBytes:       17,
		ChunkDedupInputBytes:     18,
		StaleReadBlocks:          19,
		StaleReadBytesSaved:      20,
		StaleReadTokensSaved:     21,
		ObsoletePruneBlocks:      22,
		ObsoletePruneBytesSaved:  23,
		ObsoletePruneTokensSaved: 24,
		ReadDeltaKeys:            []string{"observed-key"},
		PolicyDecisions:          []savingspolicy.CodexMechanismDecision{{Reason: "observed-policy"}},
		CacheEvents:              []proxyLayer0CacheEvent{{Action: proxyLayer0CacheMiss, Reason: "observed-cache"}},
		EvidenceDecisions:        []evidence.BlockDecision{{Mechanism: string(proxyLayer0MechanismCapturedOut), Reason: "observed-evidence", Action: evidence.ActionFullPass}},
		TotalLatencyNs:           25,
		ReadDeltaLatencyNs:       26,
		FilterLatencyNs:          27,
		RepeatedOutputLatencyNs:  28,
		ChunkDedupLatencyNs:      29,
	}

	got := mergeWSSLayer0ObservationStats(base, observed)
	if got.Route != codexLayer0RouteWSSPhaseF ||
		got.ToolResultBlocks != 3 ||
		got.ToolUseUnresolvedBlocks != 3 ||
		got.CommandResolvedBlocks != 4 ||
		got.CommandUnresolvedBlocks != 5 ||
		got.ReadDeltaAttempts != 7 ||
		got.ReadDeltaMisses != 7 ||
		got.ToolResultBytes != 18 ||
		got.TokensSaved != 20 ||
		got.BlocksModified != 11 ||
		got.ReadDeltaBlocks != 12 ||
		got.CapturedOutputBlocks != 13 ||
		got.CodexExecEnvelopeBlocks != 13 ||
		got.RepeatedOutputBlocks != 14 ||
		got.ChunkDedupBlocks != 16 ||
		got.ChunkDedupReferences != 17 ||
		got.ChunkDedupRefBytes != 29 ||
		got.ChunkDedupInputBytes != 31 ||
		got.StaleReadBlocks != 20 ||
		got.StaleReadBytesSaved != 34 ||
		got.StaleReadTokensSaved != 36 ||
		got.ObsoletePruneBlocks != 22 ||
		got.ObsoletePruneBytesSaved != 23 ||
		got.ObsoletePruneTokensSaved != 24 ||
		got.TotalLatencyNs != 41 ||
		got.ReadDeltaLatencyNs != 43 ||
		got.FilterLatencyNs != 45 ||
		got.RepeatedOutputLatencyNs != 47 ||
		got.ChunkDedupLatencyNs != 49 {
		t.Fatalf("observation merge lost counters: %+v", got)
	}
	if strings.Join(got.ReadDeltaKeys, ",") != "base-key,observed-key" {
		t.Fatalf("read-delta keys not appended: %+v", got.ReadDeltaKeys)
	}
	if len(got.PolicyDecisions) != 2 || got.PolicyDecisions[1].Reason != "observed-policy" {
		t.Fatalf("policy decisions not appended: %+v", got.PolicyDecisions)
	}
	if len(got.CacheEvents) != 2 || got.CacheEvents[1].Reason != "observed-cache" {
		t.Fatalf("cache events not appended: %+v", got.CacheEvents)
	}
	if len(got.EvidenceDecisions) != 2 || got.EvidenceDecisions[1].Reason != "observed-evidence" {
		t.Fatalf("evidence decisions not appended: %+v", got.EvidenceDecisions)
	}
	preserved := mergeWSSLayer0ObservationStats(proxyLayer0Stats{Route: codexLayer0RouteHTTP}, observed)
	if preserved.Route != codexLayer0RouteHTTP {
		t.Fatalf("observation merge must not overwrite an existing route: %q", preserved.Route)
	}
}

func TestProxyLayer0StatsHasTelemetry(t *testing.T) {
	tests := []struct {
		name  string
		stats proxyLayer0Stats
		want  bool
	}{
		{name: "empty"},
		{name: "tool result blocks", stats: proxyLayer0Stats{ToolResultBlocks: 1}, want: true},
		{name: "tool result bytes", stats: proxyLayer0Stats{ToolResultBytes: 1}, want: true},
		{name: "policy decisions", stats: proxyLayer0Stats{PolicyDecisions: []savingspolicy.CodexMechanismDecision{{Reason: "guarded"}}}, want: true},
		{name: "cache events", stats: proxyLayer0Stats{CacheEvents: []proxyLayer0CacheEvent{{Reason: "guarded"}}}, want: true},
		{name: "evidence decisions", stats: proxyLayer0Stats{EvidenceDecisions: []evidence.BlockDecision{{Reason: "guarded"}}}, want: true},
		{name: "latency", stats: proxyLayer0Stats{TotalLatencyNs: 1}, want: true},
	}
	for _, tt := range tests {
		if got := proxyLayer0StatsHasTelemetry(tt.stats); got != tt.want {
			t.Fatalf("%s telemetry=%v, want %v for %+v", tt.name, got, tt.want, tt.stats)
		}
	}
}

func TestWSSGuardedHistoryReducerEvidenceRecordsFullPassDecisions(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	body := codexWSStaleObsoleteLayer0Body()
	messages, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatalf("extract fixture messages: %v", err)
	}

	empty := adapter.wssGuardedHistoryReducerEvidence(body, messages, "", 4)
	if empty.Route != "" || len(empty.EvidenceDecisions) != 0 {
		t.Fatalf("empty guard reason must not create evidence: %+v", empty)
	}

	stats := adapter.wssGuardedHistoryReducerEvidence(body, messages, "unit_history_guard", 4)
	if stats.Route != codexLayer0RouteWSSPhaseF ||
		stats.TokensSaved != 0 ||
		stats.BlocksModified != 0 ||
		stats.StaleReadBlocks != 0 ||
		stats.ObsoletePruneBlocks != 0 ||
		len(stats.EvidenceDecisions) != 2 {
		t.Fatalf("guarded history reducers should be evidence-only full-pass stats: %+v", stats)
	}
	if !hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismStaleRead, "unit_history_guard", evidence.ActionFullPass) ||
		!hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismObsoletePrune, "unit_history_guard", evidence.ActionFullPass) {
		t.Fatalf("guarded history reducers must record precise mechanisms: %+v", stats.EvidenceDecisions)
	}
	for _, decision := range stats.EvidenceDecisions {
		if decision.OriginalTokens <= decision.FinalTokens ||
			decision.SavedTokens <= 0 ||
			decision.FootprintScore <= 0 ||
			decision.FootprintScoreBucket == "" {
			t.Fatalf("guarded history decision lost economics: %+v", decision)
		}
	}
}

func TestWSPhaseFSearchOutputPassesThroughUntilLiveSafe(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	seedToolCall := func(callID string) {
		env := parseWSJSON(t, map[string]any{
			"type": string(wsmitm.FrameKindResponseOutputItemDone),
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      "exec_command",
				"arguments": map[string]any{"cmd": "rg -n TODO src"},
			},
		})
		if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &env); err != nil || replace {
			t.Fatalf("server search tool-call seed should not replace, replace=%v err=%v", replace, err)
		}
	}
	runOutput := func(callID, output string) (bool, []byte) {
		env := parseWSJSON(t, map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-search",
			"prompt_cache_key":     "search-delta-session",
			"input": []map[string]any{{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  output,
			}},
			"stream": true,
		})
		replaced := adapter.handleRequest(&env)
		return replaced, []byte(env.Raw)
	}
	searchOutput := func(changed bool) string {
		var b strings.Builder
		for i := 1; i <= 18; i++ {
			label := "TODO stable"
			if changed && i == 10 {
				label = "TODO stable changed"
			}
			b.WriteString("src/very/long/path/search_fixture.go:")
			b.WriteString(strconv.Itoa(i))
			b.WriteString(":")
			b.WriteString(label)
			b.WriteString(strings.Repeat(" context", 12))
			b.WriteByte('\n')
		}
		return b.String()
	}
	first := searchOutput(false)
	second := searchOutput(true)

	seedToolCall("search-1")
	replaced, raw := runOutput("search-1", first)
	if replaced {
		t.Fatalf("WSS search output must pass through until live-safe, raw=%s", raw)
	}
	seedToolCall("search-2")
	replaced, raw = runOutput("search-2", second)
	if replaced {
		t.Fatalf("changed repeated WSS search output must pass through until live-safe, raw=%s", raw)
	}
	if !bytes.Contains(raw, []byte("src/very/long/path/search_fixture.go:10:TODO stable changed")) ||
		bytes.Contains(raw, []byte("[context-archive kind=tool-output uri=local-archive://")) ||
		bytes.Contains(raw, []byte("[rg] 18 match(es)")) {
		t.Fatalf("WSS search output did not remain byte-preserving enough: %s", raw)
	}
	if snap := p.OutputReduceCountersSnapshot(); snap.ProxyLayer0CapturedBlocks != 0 || snap.ProxyLayer0TokensSaved != 0 {
		t.Fatalf("WSS search output must not record Layer 0 search savings: %+v", snap)
	}
}

func TestWSPhaseFDefaultFullHistorySearchOutputCompactsWithArchive(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	searchOutput := proxyWSSSearchOutputFixture("needle", 90)
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-search-full-history",
		"prompt_cache_key":     "search-full-history-session",
		"input": []map[string]any{
			{"type": "function_call", "call_id": "search-full-history", "name": "exec_command", "arguments": map[string]any{"cmd": "cd /repo/search && rg -n needle src"}},
			{"type": "function_call_output", "call_id": "search-full-history", "output": searchOutput},
		},
		"stream": true,
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("full-history search output handle: %v", err)
	}
	raw := string(env.Raw)
	if !replace ||
		!strings.Contains(raw, "[rg]") ||
		!strings.Contains(raw, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(raw, "src/file_089.go:90:needle") {
		t.Fatalf("default full-history search output should compact with archive recovery: replace=%v raw=%s", replace, raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.request_shape"] != "full_history" ||
		summary.DebugFacts["wss.delta_shape"] != "false" ||
		summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.Tokens.Saved <= 0 ||
		summary.MessagesCompressed == 0 {
		t.Fatalf("full-history search output should save without guards: %+v", summary)
	}
	if snap := p.OutputReduceCountersSnapshot(); snap.ProxyLayer0CapturedBlocks == 0 || snap.ProxyLayer0TokensSaved <= 0 {
		t.Fatalf("full-history search output should record Layer 0 savings: %+v", snap)
	}
}

func TestWSPhaseFFirstSocketFullHistorySearchOutputCompactsWithArchive(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(1)

	searchOutput := proxyWSSSearchOutputFixture("needle", 90)
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-search-first-socket-full-history",
		"prompt_cache_key":     "search-first-socket-full-history-session",
		"input": []map[string]any{
			{"type": "function_call", "call_id": "search-first-socket-full-history", "name": "exec_command", "arguments": map[string]any{"cmd": "cd /repo/search && rg -n needle src"}},
			{"type": "function_call_output", "call_id": "search-first-socket-full-history", "output": searchOutput},
		},
		"stream": true,
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("first-socket full-history search output handle: %v", err)
	}
	raw := string(env.Raw)
	if !replace ||
		!strings.Contains(raw, "[rg]") ||
		!strings.Contains(raw, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(raw, "src/file_089.go:90:needle") {
		t.Fatalf("first-socket full-history search output should compact with archive recovery: replace=%v raw=%s", replace, raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.request_shape"] != "full_history" ||
		summary.DebugFacts["wss.delta_shape"] != "false" ||
		summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.effective_mutation_guard"] != "" ||
		summary.DebugFacts["wss.history_mutation_guard"] != "" ||
		summary.DebugFacts["wss.downstream_state_mutation_guard"] != "wss_full_history_downstream_delta_proof_gate" ||
		summary.Tokens.Saved <= 0 ||
		summary.MessagesCompressed == 0 {
		t.Fatalf("first-socket full-history search output should save without an effective structured guard: %+v", summary)
	}
	if snap := p.OutputReduceCountersSnapshot(); snap.ProxyLayer0CapturedBlocks == 0 || snap.ProxyLayer0TokensSaved <= 0 {
		t.Fatalf("first-socket full-history search output should record Layer 0 savings: %+v", snap)
	}
}

func TestWSPhaseFReconnectFullHistorySearchOutputKeepsDownstreamProofGate(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(2)

	searchOutput := proxyWSSSearchOutputFixture("needle", 90)
	env := parseWSJSON(t, map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "search-reconnect-full-history-session",
		"input": []map[string]any{
			{"type": "function_call", "call_id": "search-reconnect-full-history", "name": "exec_command", "arguments": map[string]any{"cmd": "cd /repo/search && rg -n needle src"}},
			{"type": "function_call_output", "call_id": "search-reconnect-full-history", "output": searchOutput},
		},
		"stream": true,
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("reconnect full-history search output handle: %v", err)
	}
	raw := string(env.Raw)
	if replace ||
		strings.Contains(raw, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(raw, "[rg]") ||
		!strings.Contains(raw, "src/file_089.go:90:needle") {
		t.Fatalf("reconnect full-history search output must full-pass under proof gate: replace=%v raw=%s", replace, raw)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.request_shape"] != "full_history" ||
		summary.DebugFacts["wss.delta_shape"] != "false" ||
		summary.DebugFacts["wss.structured_mutation_guard"] != "wss_full_history_downstream_delta_proof_gate" ||
		summary.DebugFacts["wss.effective_mutation_guard"] != "wss_full_history_downstream_delta_proof_gate" ||
		summary.Tokens.Saved != 0 ||
		summary.MessagesCompressed != 0 {
		t.Fatalf("reconnect full-history search output should be guarded: %+v", summary)
	}
	if !hasEvidenceDecision(summary.EvidenceDecisions, proxyLayer0MechanismCapturedOut, "wss_search_output_risk_gate", evidence.ActionFullPass) {
		t.Fatalf("reconnect full-history search output should keep search risk evidence: %+v", summary.EvidenceDecisions)
	}
	if snap := p.OutputReduceCountersSnapshot(); snap.ProxyLayer0CapturedBlocks != 0 || snap.ProxyLayer0TokensSaved != 0 {
		t.Fatalf("guarded reconnect full-history search output must not record Layer 0 savings: %+v", snap)
	}
}

func TestWSPhaseFRequestRecordsBodyPlannerSummary(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })
	cleanupPhaseFTempHome(t, tmp, "codex-wss:t248-planner-session")

	cfg := config.Defaults()
	cfg.Compression.Tuning.PlannerLiveCorpusConfidence = "high"
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	largeOutput := strings.Repeat("planner telemetry repeat-read line with enough body to trip the candidate gate\n", 1600)
	argsJSON := string(mustMarshal(map[string]any{"cmd": "cat planner-repeat.md", "workdir": tmp}))
	seedToolCall := func(callID string) {
		env := parseWSJSON(t, map[string]any{
			"type": string(wsmitm.FrameKindResponseOutputItemDone),
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      "exec_command",
				"arguments": argsJSON,
			},
		})
		if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &env); err != nil || replace {
			t.Fatalf("seed tool call replace=%v err=%v", replace, err)
		}
	}
	runRead := func(callID string) bool {
		env := parseWSJSON(t, map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "t248-planner-session",
			"input": []map[string]any{{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  largeOutput,
			}},
			"stream": true,
		})
		return adapter.handleRequest(&env)
	}

	seedToolCall("call_first")
	_ = runRead("call_first")
	seedToolCall("call_second")
	if !runRead("call_second") {
		t.Fatal("second repeat read should mutate through read-delta")
	}

	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("expected one latest summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.RouteMode != "websocket_phasef" || summary.Provider != types.CodexChatGPT.String() {
		t.Fatalf("bad WSS body summary identity: %+v", summary)
	}
	if summary.PreviousResponseIDUsed || summary.TotalMessages != 1 || summary.MessagesCompressed != 1 {
		t.Fatalf("bad WSS body summary counters: %+v", summary)
	}
	if summary.ReReadCount != 1 {
		t.Fatalf("WSS re-read canary not recorded: %+v", summary)
	}
	if summary.Tokens.Original <= summary.Tokens.Final || summary.NetSavedTokens <= 0 {
		t.Fatalf("expected positive WSS planner token delta: %+v", summary.Tokens)
	}
	if summary.OutputReduce.Applied || summary.OutputReduce.Reason != "disabled" {
		t.Fatalf("WSS Layer-0 mutation must not be recorded as output-reduce applied: %+v", summary.OutputReduce)
	}
	if summary.Plan == nil {
		t.Fatal("WSS body summary missing planner output")
	}
	for _, want := range []string{"websocket", "tool_output", "repeated_tool_output"} {
		if !hasString(summary.Plan.ContentClasses, want) {
			t.Fatalf("plan content classes=%v missing %s", summary.Plan.ContentClasses, want)
		}
	}
	if !hasPlanAction(summary.Plan.Decisions, "l2", "shadow", "codex_wss_l2_requires_fixture_live_proof") {
		t.Fatalf("WSS L2 proof gate missing: %+v", summary.Plan.Decisions)
	}
	if !hasPlanAction(summary.Plan.Decisions, "websocket", "mutate", "known_shape_and_high_corpus_confidence") {
		t.Fatalf("WSS body shape was not recognized as mutation-capable in planner: %+v", summary.Plan.Decisions)
	}
}

func codexWSReadBody(toolName, oldOutput, freshOutput string) []byte {
	return mustMarshal(map[string]any{
		"model": "gpt-5-codex",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "read src/x.go"},
			{"type": "function_call", "call_id": "call_1", "name": toolName, "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_1", "output": oldOutput},
			{"type": "message", "role": "user", "content": "filler one"},
			{"type": "message", "role": "user", "content": "filler two"},
			{"type": "function_call", "call_id": "call_2", "name": toolName, "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_2", "output": freshOutput},
		},
		"stream": true,
	})
}

func codexWSObsoleteReadBody(oldOutput string) []byte {
	return mustMarshal(map[string]any{
		"model": "gpt-5-codex",
		"input": []map[string]any{
			{"type": "function_call", "call_id": "call_1", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_1", "output": oldOutput},
			{"type": "message", "role": "user", "content": "edit it"},
			{"type": "function_call", "call_id": "call_2", "name": "apply_patch", "arguments": map[string]any{"path": "src/x.go", "patch": "@@ ..."}},
			{"type": "function_call_output", "call_id": "call_2", "output": "patch applied"},
		},
		"stream": true,
	})
}

func codexWSStaleObsoleteLayer0Body() []byte {
	var status strings.Builder
	for i := 0; i < 180; i++ {
		status.WriteString(" M internal/proxy/single_reconstruct_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	return mustMarshal(map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": "single-reconstruct-session",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "read src/x.go and src/y.go"},
			{"type": "function_call", "call_id": "call_x_old", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_x_old", "output": strings.Repeat("stale x content ", 80)},
			{"type": "function_call", "call_id": "call_y_old", "name": "Read", "arguments": map[string]any{"path": "src/y.go"}},
			{"type": "function_call_output", "call_id": "call_y_old", "output": strings.Repeat("obsolete y content ", 80)},
			{"type": "message", "role": "user", "content": "filler one"},
			{"type": "message", "role": "user", "content": "filler two"},
			{"type": "function_call", "call_id": "call_x_fresh", "name": "Read", "arguments": map[string]any{"path": "src/x.go"}},
			{"type": "function_call_output", "call_id": "call_x_fresh", "output": "fresh x content"},
			{"type": "message", "role": "user", "content": "edit src/y.go"},
			{"type": "function_call", "call_id": "call_y_edit", "name": "apply_patch", "arguments": map[string]any{"path": "src/y.go", "patch": "@@ ..."}},
			{"type": "function_call_output", "call_id": "call_y_edit", "output": "patch applied"},
			{"type": "function_call", "call_id": "call_status", "name": "exec_command", "arguments": map[string]any{"cmd": "git status --short"}},
			{"type": "function_call_output", "call_id": "call_status", "output": status.String()},
		},
		"stream": true,
	})
}

func assertSameProxyToolUseIndex(t *testing.T, before, after map[string]types.ContentBlock) {
	t.Helper()
	if len(after) != len(before) {
		t.Fatalf("tool-use index length changed before=%d after=%d", len(before), len(after))
	}
	for id, want := range before {
		got, ok := after[id]
		if !ok {
			t.Fatalf("tool-use index lost %s", id)
		}
		if got.Type != want.Type || got.ToolUseID != want.ToolUseID || got.ToolName != want.ToolName || got.ToolInput != want.ToolInput {
			t.Fatalf("tool-use %s changed before=%+v after=%+v", id, want, got)
		}
	}
}

func TestWSCodexSessionIDFallbacks(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"thread_id":"t1"}`),
		[]byte(`{"conversation_id":"c1"}`),
		[]byte(`{"session_id":"s1"}`),
		[]byte(`{"metadata":{"thread_id":"mt1"}}`),
		[]byte(`{"metadata":{"conversation_id":"mc1"}}`),
		[]byte(`{"metadata":{"session_id":"ms1"}}`),
		[]byte(`{"metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"mtm1\"}"}}`),
		[]byte(`{"client_metadata":{"session_id":"cms1"}}`),
	} {
		if got := wsCodexSessionID(raw); !strings.HasPrefix(got, "codex-wss:") {
			t.Fatalf("missing codex prefix for %s: %q", raw, got)
		}
	}
	for _, raw := range [][]byte{
		[]byte(`not-json`),
		[]byte(`{"metadata":1}`),
		[]byte(`{}`),
		[]byte(`{"user_id":"u1"}`),
		[]byte(`{"metadata":{"user_id":"mu1"}}`),
		[]byte(`{"client_metadata":{"user_id":"cu1"}}`),
	} {
		if got := wsCodexSessionID(raw); got != "" {
			t.Fatalf("unexpected session id for %s: %q", raw, got)
		}
	}
}

func TestWSCodexSessionIDFromCodexResponsesShape(t *testing.T) {
	// Real Codex WSS (Responses API) carries no conversation_id. The narrowest
	// per-thread key is client_metadata's x-codex-turn-metadata when present;
	// prompt_cache_key remains a fallback for frames that do not carry it.
	pck := []byte(`{"model":"gpt-5.5","previous_response_id":"resp_x","prompt_cache_key":"019e51d4-38fa-72c3-9212-69ed7d8936a0","input":[]}`)
	if got := wsCodexSessionID(pck); got != "codex-wss:019e51d4-38fa-72c3-9212-69ed7d8936a0" {
		t.Fatalf("prompt_cache_key not used as session key: %q", got)
	}
	cm := []byte(`{"model":"gpt-5.5","client_metadata":{"x-codex-turn-metadata":"{\"session_id\":\"019e51d6-cf3b-7301-b492-aaaaaaaaaaaa\",\"thread_id\":\"019e51d6-cf3b-7301-b492-aaaaaaaaaaaa\"}"}}`)
	if got := wsCodexSessionID(cm); got != "codex-wss:019e51d6-cf3b-7301-b492-aaaaaaaaaaaa" {
		t.Fatalf("client_metadata thread/session id not used: %q", got)
	}
	// client_metadata wins over prompt_cache_key when both are present because
	// prompt_cache_key can describe a shared cacheable prefix.
	both := []byte(`{"prompt_cache_key":"pck-key","client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"tm-key\"}"}}`)
	if got := wsCodexSessionID(both); got != "codex-wss:tm-key" {
		t.Fatalf("client_metadata should win: %q", got)
	}
	metaTurn := []byte(`{"prompt_cache_key":"pck-key","metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"meta-turn-key\"}"}}`)
	if got := wsCodexSessionID(metaTurn); got != "codex-wss:meta-turn-key" {
		t.Fatalf("metadata turn metadata should win: %q", got)
	}
}

func TestWSSRequestMetaFromRawMatchesBodyHelpers(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","previous_response_id":"resp_prev","prompt_cache_key":"pck-key","client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"thread-key\",\"source\":\"cli\"}"},"input":[]}`)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	meta := wssRequestMetaFromRaw(raw)
	if meta.SessionID != wsCodexSessionID(body) {
		t.Fatalf("session id from raw = %q, body helper = %q", meta.SessionID, wsCodexSessionID(body))
	}
	if meta.PreviousResponseID != wssPreviousResponseID(body) {
		t.Fatalf("previous response id from raw = %q, body helper = %q", meta.PreviousResponseID, wssPreviousResponseID(body))
	}
	if meta.Model != wssPlannerModel(body) {
		t.Fatalf("model from raw = %q, body helper = %q", meta.Model, wssPlannerModel(body))
	}
	if meta.ClientFamily != "codex_cli" {
		t.Fatalf("client family from raw = %q, want codex_cli", meta.ClientFamily)
	}
}

func TestWSPhaseFBeTerseRecordsQualityOutcomeOnTerminalFrame(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.BeTerseHintEnabled = true
	cfg.Compression.OutputReduce.BeTerseHintText = "be concise"
	p := New(cfg)
	conversationID := findCodexWSSTreatmentConversation(t, p)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	req := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": conversationID,
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Summarize this.",
			}},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &req); err != nil || !replace {
		t.Fatalf("request handle replace=%v err=%v", replace, err)
	}
	if snap := p.qualityAB.Snapshot(); snap.TreatmentTotal != 0 {
		t.Fatalf("WSS quality outcome should wait for terminal frame: %+v", snap)
	}

	resp := parseWSJSON(t, map[string]any{
		"type":     string(wsmitm.FrameKindResponseCompleted),
		"response": map[string]any{"id": "resp-ok"},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp); err != nil || replace {
		t.Fatalf("terminal handle replace=%v err=%v", replace, err)
	}
	if snap := p.qualityAB.Snapshot(); snap.TreatmentTotal != 1 || snap.TreatmentFailures != 0 {
		t.Fatalf("WSS terminal success not recorded: %+v", snap)
	}
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp); err != nil || replace {
		t.Fatalf("second terminal handle replace=%v err=%v", replace, err)
	}
	if snap := p.qualityAB.Snapshot(); snap.TreatmentTotal != 1 {
		t.Fatalf("terminal without pending WSS request should not double-record: %+v", snap)
	}
}

func TestWSPhaseFBeTerseRecordsFailedTerminalOutcome(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.BeTerseHintEnabled = true
	cfg.Compression.OutputReduce.BeTerseHintText = "be concise"
	p := New(cfg)
	conversationID := findCodexWSSTreatmentConversation(t, p)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	req := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": conversationID,
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Summarize this.",
			}},
			"stream": true,
		},
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &req); err != nil || !replace {
		t.Fatalf("request handle replace=%v err=%v", replace, err)
	}
	failed := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindResponseFailed),
	})
	if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &failed); err != nil || replace {
		t.Fatalf("failed terminal handle replace=%v err=%v", replace, err)
	}
	if snap := p.qualityAB.Snapshot(); snap.TreatmentTotal != 1 || snap.TreatmentFailures != 1 {
		t.Fatalf("WSS terminal failure not recorded: %+v", snap)
	}
}

func findCodexWSSTreatmentConversation(t *testing.T, p *Proxy) string {
	t.Helper()
	if p.qualityAB == nil {
		t.Fatal("nil qualityAB harness")
	}
	for i := 0; i < 2000; i++ {
		id := "conv-treatment-" + itoa(i)
		if p.qualityAB.Cohort("codex-wss:"+id) == "treatment" {
			return id
		}
	}
	t.Fatal("could not find treatment conversation id")
	return ""
}

func TestMITMConversationForwardsResponsesRequestWithoutStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = true
	p := New(cfg)
	upstreamRemote, upstreamLocal := newPipe()
	d := &PhaseFDispatcher{
		Proxy: p,
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}
	clientRemote, clientLocal := newPipe()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = d.Handle(context.Background(), sniroute.MITMConversation,
			sniroute.Request{SNI: "chatgpt.com"}, clientLocal)
	}()

	raw := mustMarshal(map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model": "gpt-5-codex",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "Build.",
			}},
			"stream": true,
		},
	})
	if _, err := clientRemote.Write(wsFrameBytes(t, raw)); err != nil {
		t.Fatalf("client write: %v", err)
	}

	if err := upstreamRemote.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	frame, err := wscompact.ReadFrame(upstreamRemote)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if strings.Contains(string(frame.Payload), `"stop"`) {
		t.Fatalf("Responses request frame must not get stop: %s", frame.Payload)
	}

	_ = clientRemote.Close()
	_ = upstreamRemote.Close()
	_ = clientLocal.Close()
	wg.Wait()

	if got := p.OutputReduceCountersSnapshot().StopSeqRequestsModified; got != 0 {
		t.Fatalf("stop counter=%d, want 0", got)
	}
}

func parseWSJSON(t *testing.T, v any) wsmitm.Envelope {
	t.Helper()
	raw := mustMarshal(v)
	env, err := wsmitm.Parse(raw)
	if err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	return env
}

func codexToolDefinition(name, description string) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": description,
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{"input": map[string]any{"type": "string"}},
			},
		},
	}
}

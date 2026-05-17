package proxy

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy/sniroute"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
	"github.com/slimference/slimference/internal/staleread"
	"github.com/slimference/slimference/internal/types"
	"github.com/slimference/slimference/internal/wscompact"
)

func TestWSPhaseFRequestInjectsStopAndPreservesUnknownEnvelopeFields(t *testing.T) {
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
	if !replace {
		t.Fatal("expected request frame to be re-encoded")
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(env.Body, &body); err != nil {
		t.Fatalf("body JSON: %v", err)
	}
	if _, ok := body["stop"]; !ok {
		t.Fatalf("stop field missing from mutated body: %s", env.Body)
	}
	wire, err := env.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(wire), `"trace":"keep-me"`) {
		t.Fatalf("unknown envelope field lost on re-encode: %s", wire)
	}
	if got := p.OutputReduceCountersSnapshot().StopSeqRequestsModified; got != 1 {
		t.Fatalf("stop counter=%d, want 1", got)
	}
}

func TestWSPhaseFRepdetRewritesStreamedTextDelta(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.RepetitionDetectionEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	promptText := strings.Repeat("large unchanged prompt block ", 18)

	req := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":           "gpt-5-codex",
			"conversation_id": "conv-repdet",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": promptText,
			}},
			"stream": true,
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &req)
	if err != nil {
		t.Fatalf("request handle: %v", err)
	}
	if !replace {
		t.Fatal("request should mutate through stop-sequence injection")
	}

	resp := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": "Here is the same content again: " + promptText,
	})
	replace, err = adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp)
	if err != nil {
		t.Fatalf("response handle: %v", err)
	}
	if !replace {
		t.Fatal("expected repdet to re-encode streamed delta")
	}
	if !strings.Contains(resp.Delta, "[unchanged: prompt-text]") {
		t.Fatalf("repdet marker missing: %q", resp.Delta)
	}
	if got := p.OutputReduceCountersSnapshot().RepdetResponsesRewritten; got != 1 {
		t.Fatalf("repdet counter=%d, want 1", got)
	}
}

func TestWSPhaseFStreamcutBlanksTrailingCommentaryDelta(t *testing.T) {
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
	if !replace {
		t.Fatal("expected streamcut to re-encode trailing commentary delta")
	}
	if resp.Delta != "" {
		t.Fatalf("delta not blanked after streamcut fire: %q", resp.Delta)
	}
	if got := p.OutputReduceCountersSnapshot().StreamcutFired; got != 1 {
		t.Fatalf("streamcut counter=%d, want 1", got)
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
	if !replace || !strings.Contains(string(requestEnv.Request), `"stop"`) {
		t.Fatalf("request variant not mutated: replace=%v request=%s", replace, requestEnv.Request)
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

func TestWSPhaseFStreamcutAfterFiringBlanksSubsequentDelta(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StreamCutEnabled = true
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	first := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": strings.Repeat("substantive answer ", 8) + "\nHope this helps",
	})
	if replace, _ := adapter.handle(context.Background(), wsmitm.DirServerToClient, &first); !replace {
		t.Fatal("precondition: first delta should fire streamcut")
	}
	second := parseWSJSON(t, map[string]any{
		"type":  string(wsmitm.FrameKindResponseOutputTextDelta),
		"delta": "trailing words after cut",
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &second)
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if !replace || second.Delta != "" || string(second.Fields["delta"]) != `""` {
		t.Fatalf("second delta not blanked: replace=%v delta=%q fields=%s", replace, second.Delta, second.Fields["delta"])
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
		t.Fatal("empty delta after streamcut should not be re-encoded")
	}
}

func TestWSPhaseFTerminalResponseRepdetRewrite(t *testing.T) {
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
	if replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &req); err != nil || !replace {
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
	if !replace || !strings.Contains(string(resp.Response), "[unchanged: prompt-text]") {
		t.Fatalf("terminal response not rewritten: replace=%v response=%s", replace, resp.Response)
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
	if wsStreamcutLine(&wsmitm.Envelope{Kind: wsmitm.FrameKindResponseOutputTextDelta, Delta: "x"}) == nil {
		t.Fatal("streamcut line should marshal")
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
	mutated, _, changed := adapter.applyInputPipeline(agedBody)
	if !changed || strings.Contains(string(mutated), "old file content") || !strings.Contains(string(mutated), "[stale read:") {
		t.Fatalf("stale-read mutation failed changed=%v body=%s", changed, mutated)
	}
	if got := p.OutputReduceCountersSnapshot().StaleReadBlocksReplaced; got == 0 {
		t.Fatalf("stale-read counter not incremented: %+v", p.OutputReduceCountersSnapshot())
	}

	cfg = config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	p = New(cfg)
	adapter = (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	prunedBody := codexWSObsoleteReadBody(strings.Repeat("obsolete file content ", 80))
	mutated, _, changed = adapter.applyInputPipeline(prunedBody)
	if !changed || strings.Contains(string(mutated), "obsolete file content") || !strings.Contains(string(mutated), "[obsolete:") {
		t.Fatalf("obsolete-read mutation failed changed=%v body=%s", changed, mutated)
	}
	if got := p.OutputReduceCountersSnapshot().ObsoleteReadBlocksPruned; got == 0 {
		t.Fatalf("obsolete counter not incremented: %+v", p.OutputReduceCountersSnapshot())
	}
}

func TestWSPhaseFRequestCompactsCodexToolOutputLayer0(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
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

	mutated, _, changed := adapter.applyInputPipeline(body)
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

func TestWSCodexSessionIDFallbacks(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"conversation_id":"c1"}`),
		[]byte(`{"session_id":"s1"}`),
		[]byte(`{"user_id":"u1"}`),
		[]byte(`{"metadata":{"conversation_id":"mc1"}}`),
		[]byte(`{"metadata":{"session_id":"ms1"}}`),
		[]byte(`{"metadata":{"user_id":"mu1"}}`),
	} {
		if got := wsCodexSessionID(raw); !strings.HasPrefix(got, "codex-wss:") {
			t.Fatalf("missing codex prefix for %s: %q", raw, got)
		}
	}
	for _, raw := range [][]byte{[]byte(`not-json`), []byte(`{"metadata":1}`), []byte(`{}`)} {
		if got := wsCodexSessionID(raw); got != "" {
			t.Fatalf("unexpected session id for %s: %q", raw, got)
		}
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

func TestMITMConversationMutatesRequestFrameWhenProxyConfigured(t *testing.T) {
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
	if !strings.Contains(string(frame.Payload), `"stop"`) {
		t.Fatalf("upstream frame was not mutated: %s", frame.Payload)
	}

	_ = clientRemote.Close()
	_ = upstreamRemote.Close()
	_ = clientLocal.Close()
	wg.Wait()

	if got := p.OutputReduceCountersSnapshot().StopSeqRequestsModified; got != 1 {
		t.Fatalf("stop counter=%d, want 1", got)
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

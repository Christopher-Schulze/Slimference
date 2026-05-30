package proxy

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy/sniroute"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/staleread"
	"github.com/slimference/slimference/internal/types"
	"github.com/slimference/slimference/internal/wscompact"
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
	if replace {
		t.Fatal("Responses-shaped request should seed state without stop mutation")
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
	snap := adapter.snapshot()
	if snap.RequestsSeen != 1 ||
		snap.RequestBodiesSeen != 1 ||
		snap.RequestMessagesIndexed != 1 ||
		snap.ResponseTextDeltasSeen != 1 ||
		snap.Mutations != 1 {
		t.Fatalf("unexpected Phase-F adapter telemetry: %+v", snap)
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
	promptText := strings.Repeat("top level prompt block ", 20)

	env := parseWSJSON(t, map[string]any{
		"model": "gpt-5-codex",
		"input": []map[string]any{{
			"type":    "message",
			"role":    "user",
			"content": promptText,
		}},
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
		"delta": "Echo: " + promptText,
	})
	replace, err = adapter.handle(context.Background(), wsmitm.DirServerToClient, &resp)
	if err != nil {
		t.Fatalf("delta handle: %v", err)
	}
	if !replace || !strings.Contains(resp.Delta, "[unchanged: prompt-text]") {
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
	if replace || strings.Contains(string(resp.Response), "[unchanged: prompt-text]") {
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
	mutated, _, changed, _ := adapter.applyInputPipeline(agedBody)
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
	mutated, _, changed, _ = adapter.applyInputPipeline(prunedBody)
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

	mutated, _, changed, _ := adapter.applyInputPipeline(body)
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

func TestWSPhaseFRequestCompactsCodexResponseItemPayloadLayer0(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
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

	mutated, _, changed, _ := adapter.applyInputPipeline(body)
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
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
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
		"model": "gpt-5-codex",
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
	if !strings.Contains(string(outputEnv.Raw), "[git status]") || strings.Contains(string(outputEnv.Raw), "synthetic_119.go") {
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
		status.WriteString("?? server_synthetic_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	outputEnv := parseWSJSON(t, map[string]any{
		"model": "gpt-5-codex",
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
	if !strings.Contains(string(outputEnv.Raw), "[git status]") || strings.Contains(string(outputEnv.Raw), "server_synthetic_119.go") {
		t.Fatalf("server-seeded tool output was not compacted: %s", outputEnv.Raw)
	}
	if snap := p.OutputReduceCountersSnapshot(); snap.ProxyLayer0RequestsModified != 1 || snap.ProxyLayer0TokensSaved == 0 {
		t.Fatalf("Layer 0 counters not recorded: %+v", snap)
	}
}

func TestWSPhaseFRequestRecordsBodyPlannerSummary(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.Layer2Enabled = true
	cfg.Compression.Tuning.PlannerLiveCorpusConfidence = "high"
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	path := filepath.Join(tmp, "planner-repeat.md")
	largeOutput := strings.Repeat("planner telemetry repeat-read line with enough body to trip the candidate gate\n", 1600)
	argsJSON := string(mustMarshal(map[string]any{"cmd": "cat " + path}))
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
			"model":                "gpt-5-codex",
			"previous_response_id": "resp_t248_planner",
			"prompt_cache_key":     "t248-planner-session",
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
	if !summary.PreviousResponseIDUsed || summary.TotalMessages != 1 || summary.MessagesCompressed != 1 {
		t.Fatalf("bad WSS body summary counters: %+v", summary)
	}
	if summary.Tokens.Original <= summary.Tokens.Final || summary.NetSavedTokens <= 0 {
		t.Fatalf("expected positive WSS planner token delta: %+v", summary.Tokens)
	}
	if summary.OutputReduce.Reason != "phasef_read_delta" {
		t.Fatalf("bad WSS output-reduce reason: %+v", summary.OutputReduce)
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
	if !hasPlanAction(summary.Plan.Decisions, "l3", "shadow", "codex_wss_l3_requires_fixture_live_proof") {
		t.Fatalf("WSS L3 proof gate missing: %+v", summary.Plan.Decisions)
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

func TestWSCodexSessionIDFromCodexResponsesShape(t *testing.T) {
	// Real Codex WSS (Responses API) carries no conversation_id; the stable
	// per-thread key is prompt_cache_key, mirrored in client_metadata's
	// x-codex-turn-metadata. Both must resolve so the per-session read-delta
	// context can accumulate across delta-shaped requests.
	pck := []byte(`{"model":"gpt-5.5","previous_response_id":"resp_x","prompt_cache_key":"019e51d4-38fa-72c3-9212-69ed7d8936a0","input":[]}`)
	if got := wsCodexSessionID(pck); got != "codex-wss:019e51d4-38fa-72c3-9212-69ed7d8936a0" {
		t.Fatalf("prompt_cache_key not used as session key: %q", got)
	}
	cm := []byte(`{"model":"gpt-5.5","client_metadata":{"x-codex-turn-metadata":"{\"session_id\":\"019e51d6-cf3b-7301-b492-aaaaaaaaaaaa\",\"thread_id\":\"019e51d6-cf3b-7301-b492-aaaaaaaaaaaa\"}"}}`)
	if got := wsCodexSessionID(cm); got != "codex-wss:019e51d6-cf3b-7301-b492-aaaaaaaaaaaa" {
		t.Fatalf("client_metadata thread/session id not used: %q", got)
	}
	// prompt_cache_key wins over client_metadata when both present (stable key).
	both := []byte(`{"prompt_cache_key":"pck-key","client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"tm-key\"}"}}`)
	if got := wsCodexSessionID(both); got != "codex-wss:pck-key" {
		t.Fatalf("prompt_cache_key should win: %q", got)
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

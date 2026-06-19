package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/toolprune"
)

func TestWSSRecoveryHelperBranches(t *testing.T) {
	if err := writeMaskedWSTextFrame(nil, []byte(`{"type":"ping"}`)); err == nil {
		t.Fatal("nil recovery writer must fail")
	}
	var buf bytes.Buffer
	if err := writeMaskedWSTextFrame(&buf, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("write masked frame: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("masked frame writer produced no bytes")
	}

	cases := []struct {
		name      string
		status    string
		errorType string
		message   string
		want      bool
	}{
		{name: "bare invalid request", status: "400", errorType: "invalid_request_error", message: "Invalid request", want: true},
		{name: "missing previous response", status: "400", errorType: "invalid_request_error", message: "previous_response_id not found", want: true},
		{name: "missing tool after prune", status: "400", errorType: "invalid_request_error", message: "unknown tool ColdTool", want: true},
		{name: "context window is not retryable", status: "400", errorType: "invalid_request_error", message: "context window exceeded", want: false},
		{name: "rate limit is not retryable", status: "429", errorType: "invalid_request_error", message: "rate limit", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wssRetryableInvalidRequest(tc.status, tc.errorType, tc.message); got != tc.want {
				t.Fatalf("retryable=%v want %v", got, tc.want)
			}
		})
	}
	if wssLooksLikeMissingToolDefinitionError("not-a-status", "unknown tool ColdTool") {
		t.Fatal("missing-tool classifier must require a numeric status")
	}
}

func TestWSSRecoveryCandidateFromBodyToolPruneMetadataAndFailures(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":"current"}],"stream":true}`)
	input := []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"user","content":"prior"}`),
		json.RawMessage(`{"type":"message","role":"user","content":"current"}`),
	}
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model": "gpt-5.5",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": "current",
			}},
			"stream": true,
		},
	})
	meta := wssRequestMeta{
		SessionID:          "codex-wss:tool-prune-candidate",
		PreviousResponseID: "resp_old",
		Model:              "gpt-5.5",
		ClientFamily:       "codex_cli",
		ToolPrune: dbg.ToolPruneSummary{
			Applied:     true,
			PrunedTools: 2,
			SavedTokens: 123,
		},
	}

	candidate := wssRecoveryCandidateFromBody(&env, body, meta, input, true, 1, 1)
	if candidate == nil {
		t.Fatal("expected recovery candidate")
	}
	if candidate.SessionID != meta.SessionID || candidate.PreviousResponseID != meta.PreviousResponseID || candidate.Model != meta.Model {
		t.Fatalf("candidate identity mismatch: %+v", candidate)
	}
	if !candidate.ToolPruneApplied || candidate.ToolPrunePruned != 2 || candidate.ToolPruneSaved != 123 {
		t.Fatalf("tool-prune metadata missing: %+v", candidate)
	}
	if candidate.ClientFamily != "codex_cli" {
		t.Fatalf("candidate client family=%q, want codex_cli", candidate.ClientFamily)
	}
	if candidate.ChainItems != 1 || candidate.CurrentInputItems != 1 || candidate.OriginalBytes != len(body) || candidate.RetryBytes != len(candidate.RetryBody) {
		t.Fatalf("candidate sizing mismatch: %+v", candidate)
	}
	if bytes.Contains(candidate.RetryBody, []byte("previous_response_id")) || !bytes.Contains(candidate.RetryBody, []byte("prior")) {
		t.Fatalf("retry body did not expand full context correctly: %s", candidate.RetryBody)
	}
	if len(candidate.RetryPayload) == 0 {
		t.Fatal("candidate retry payload must be materialized")
	}

	if got := wssRecoveryCandidateFromBody(&env, nil, meta, input, true, 0, 0); got != nil {
		t.Fatalf("empty body must not produce a candidate: %+v", got)
	}
	if got := wssRecoveryCandidateFromBody(&env, body, meta, nil, true, 0, 0); got != nil {
		t.Fatalf("empty input must not produce a candidate: %+v", got)
	}
	if got := wssRecoveryCandidateFromBody(&env, []byte(`{"input":`), meta, input, true, 0, 0); got != nil {
		t.Fatalf("malformed body must not produce a candidate: %+v", got)
	}
	badEnv := wsmitm.Envelope{Kind: wsmitm.FrameKindRequest, Raw: json.RawMessage(`{"type":"request"}`), Fields: map[string]json.RawMessage{}}
	if got := wssRecoveryCandidateFromBody(&badEnv, body, meta, input, true, 0, 0); got != nil {
		t.Fatalf("envelope without replaceable body must not produce a candidate: %+v", got)
	}
}

func TestWSSRecoveryRootToolPruneRetriesOnlyMissingTool(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	p.toolPrune = toolprune.NewUsageTracker(1)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	writes := 0
	adapter.setRecoveryWriter(func(payload []byte) error {
		writes++
		if string(payload) != "retry-payload" {
			t.Fatalf("unexpected retry payload: %q", payload)
		}
		return nil
	})
	setRootToolPruneCandidate := func() {
		adapter.mu.Lock()
		adapter.pendingRecovery = &wssRecoveryCandidate{
			SessionID:        "codex-wss:root-tool-prune",
			RetryPayload:     []byte("retry-payload"),
			RetryBody:        []byte(`{"input":[]}`),
			ToolPruneApplied: true,
			ToolPrunePruned:  1,
			ToolPruneSaved:   42,
		}
		adapter.mu.Unlock()
	}

	setRootToolPruneCandidate()
	if adapter.tryWSSRecoveryRetry("400", "invalid_request_error", "Invalid request", "generic invalid request") {
		t.Fatal("root tool-prune recovery must not retry generic invalid_request errors")
	}
	if writes != 0 {
		t.Fatalf("generic invalid_request unexpectedly wrote retry payloads=%d", writes)
	}

	setRootToolPruneCandidate()
	if !adapter.tryWSSRecoveryRetry("400", "invalid_request_error", "unknown tool ColdTool", "missing tool") {
		t.Fatal("root tool-prune recovery must retry missing-tool invalid_request errors")
	}
	if writes != 1 {
		t.Fatalf("missing-tool retry writes=%d want 1", writes)
	}
	snap := p.toolPrune.Snapshot()
	if snap.MissTotal != 1 || snap.RetryTotal != 1 || snap.DisabledSessions != 1 {
		t.Fatalf("tool-prune miss/retry cooldown not marked: %+v", snap)
	}
}

func TestWSSRecoveryBodyRewriteHelpers(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":"old"}],"stream":true}`)
	input := []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"user","content":"new"}`),
		json.RawMessage(`{"type":"function_call_output","call_id":"call_1","output":"out"}`),
	}
	rewritten, ok := wssBodyWithInput(body, input, true)
	if !ok {
		t.Fatal("rewrite body with input failed")
	}
	if bytes.Contains(rewritten, []byte("previous_response_id")) {
		t.Fatalf("rewrite must drop previous_response_id: %s", rewritten)
	}
	items, ok := wssInputItems(rewritten)
	if !ok || len(items) != 2 {
		t.Fatalf("rewritten input len=%d ok=%v body=%s", len(items), ok, rewritten)
	}
	if _, ok := wssInputItems([]byte(`{"input":{}}`)); ok {
		t.Fatal("non-array input must not parse as recovery input")
	}

	detached, ok := detachCodexPreviousResponseID(body)
	if !ok {
		t.Fatal("detach previous_response_id failed")
	}
	if strings.Contains(string(detached), "previous_response_id") {
		t.Fatalf("detach kept previous_response_id: %s", detached)
	}
	if same, ok := detachCodexPreviousResponseID([]byte(`{"input":[]}`)); ok || string(same) != `{"input":[]}` {
		t.Fatalf("detach without previous_response_id changed body ok=%v body=%s", ok, same)
	}
}

func TestWSSStatelessHistoryContinuationBody(t *testing.T) {
	adapter := &wsPhaseFAdapter{}
	body := []byte(`{"model":"gpt-5.5","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":"current"}],"stream":true}`)
	if rewritten, ok := adapter.wssStatelessHistoryContinuationBody(body); ok || !bytes.Equal(rewritten, body) {
		t.Fatalf("stateless rewrite must stay disabled before mark ok=%v body=%s", ok, rewritten)
	}

	adapter.markWSSHistoryStatelessMode()
	if !adapter.wssHistoryStatelessMode() {
		t.Fatal("history stateless mode must be observable after mark")
	}
	if rewritten, ok := adapter.wssStatelessHistoryContinuationBody([]byte(`{"previous_response_id":"resp_old","input":[`)); ok || !bytes.Equal(rewritten, []byte(`{"previous_response_id":"resp_old","input":[`)) {
		t.Fatalf("invalid json must not rewrite ok=%v body=%s", ok, rewritten)
	}
	if rewritten, ok := adapter.wssStatelessHistoryContinuationBody([]byte(`{"input":[{"type":"message","role":"user","content":"current"}]}`)); ok || bytes.Contains(rewritten, []byte("previous_response_id")) {
		t.Fatalf("body without previous_response_id must not rewrite ok=%v body=%s", ok, rewritten)
	}
	if rewritten, ok := adapter.wssStatelessHistoryContinuationBody(body); ok || !bytes.Equal(rewritten, body) {
		t.Fatalf("missing prior chain must not rewrite ok=%v body=%s", ok, rewritten)
	}

	adapter.mu.Lock()
	adapter.responseChains = map[string]wssResponseChain{
		"resp_old": {
			json.RawMessage(`{"type":"message","role":"user","content":"prior"}`),
			json.RawMessage(`{"type":"function_call_output","call_id":"call_old","output":"old out"}`),
		},
	}
	adapter.mu.Unlock()

	rewritten, ok := adapter.wssStatelessHistoryContinuationBody(body)
	if !ok {
		t.Fatal("stateless continuation with prior chain should rewrite")
	}
	if bytes.Contains(rewritten, []byte("previous_response_id")) {
		t.Fatalf("stateless continuation must drop previous_response_id: %s", rewritten)
	}
	items, ok := wssInputItems(rewritten)
	if !ok || len(items) != 3 {
		t.Fatalf("stateless continuation input len=%d ok=%v body=%s", len(items), ok, rewritten)
	}
	if !strings.Contains(string(rewritten), "prior") || !strings.Contains(string(rewritten), "current") {
		t.Fatalf("stateless continuation must preserve prior and current input: %s", rewritten)
	}
}

func TestWSSStatelessHistoryContinuationNilAdapter(t *testing.T) {
	body := []byte(`{"previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":"current"}]}`)
	var adapter *wsPhaseFAdapter
	if adapter.wssHistoryStatelessMode() {
		t.Fatal("nil adapter must not report stateless mode")
	}
	if rewritten, ok := adapter.wssStatelessHistoryContinuationBody(body); ok || !bytes.Equal(rewritten, body) {
		t.Fatalf("nil adapter must not rewrite ok=%v body=%s", ok, rewritten)
	}
	adapter.markWSSHistoryStatelessMode()
}

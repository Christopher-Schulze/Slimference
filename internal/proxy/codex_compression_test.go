package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestExtractMessages_CodexResponsesInput(t *testing.T) {
	t.Parallel()
	body := readCodexFixture(t, "tests/fixtures/codex/v1-responses-input.json")

	msgs, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 10 {
		t.Fatalf("len=%d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].TextContent() == "" {
		t.Fatalf("first message: %#v", msgs[0])
	}
	if msgs[3].Role != "tool" || msgs[3].Content[0].Type != "tool_result" || msgs[3].Content[0].ToolResultID != "call_1" {
		t.Fatalf("tool output mapping: %#v", msgs[3])
	}

	msgs[3].Content[0].Text = `{"status":"compact"}`
	out, err := reconstructBody(types.CodexChatGPT, body, msgs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"metadata"`) || !strings.Contains(s, `"conversation_id"`) {
		t.Fatalf("body metadata not preserved: %s", s)
	}
	if !strings.Contains(s, `{\"status\":\"compact\"}`) {
		t.Fatalf("tool output rewrite missing: %s", s)
	}
}

func TestExtractMessages_CodexMessagesShapeUsesOpenAIParser(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"codex-test","messages":[{"role":"user","content":"hi"},{"role":"tool","tool_call_id":"call_x","content":"tool out"}],"store":true}`)
	msgs, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Content[0].Type != "tool_result" {
		t.Fatalf("unexpected messages: %#v", msgs)
	}
	msgs[1].Content[0].Text = "compact out"
	out, err := reconstructBody(types.CodexChatGPT, body, msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "compact out") || !strings.Contains(string(out), `"store"`) {
		t.Fatalf("reconstruct: %s", out)
	}
}

func TestExtractMessages_CodexInputStringRoundTrip(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"codex-test","input":"please inspect the repository","stream":false}`)

	msgs, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].TextContent() != "please inspect the repository" {
		t.Fatalf("unexpected string input messages: %#v", msgs)
	}

	msgs[0].Content[0].Text = "inspect repo"
	out, err := reconstructBody(types.CodexChatGPT, body, msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"input":"inspect repo"`) || !strings.Contains(string(out), `"stream":false`) {
		t.Fatalf("string input reconstruct: %s", out)
	}
}

func TestExtractCodexInputMessages_ErrorAndEmptyBranches(t *testing.T) {
	t.Parallel()
	extra := map[string]json.RawMessage{"model": json.RawMessage(`"codex-test"`)}

	msgs, returned, err := extractCodexInputMessages(nil, extra)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 || returned["model"] == nil {
		t.Fatalf("empty input returned messages=%#v extra=%#v", msgs, returned)
	}

	if _, _, err := extractCodexInputMessages(json.RawMessage(`"unterminated`), extra); err == nil {
		t.Fatal("expected invalid input string error")
	}
	if _, _, err := extractCodexInputMessages(json.RawMessage(`{}`), extra); err == nil {
		t.Fatal("expected invalid input items error")
	}
	if msgs, _, err := extractCodexInputMessages(json.RawMessage(`[{"type":"unknown","content":"ignored"},{"type":"message"}]`), extra); err != nil || len(msgs) != 0 {
		t.Fatalf("unknown/no-content items should be ignored, messages=%#v err=%v", msgs, err)
	}
	if msgs, _, err := extractCodexInputMessages(json.RawMessage(`[]`), extra); err != nil || len(msgs) != 0 {
		t.Fatalf("empty input array should produce no messages, messages=%#v err=%v", msgs, err)
	}
	if _, _, err := extractCodexInputMessages(json.RawMessage(`["not-an-object"]`), extra); err == nil {
		t.Fatal("expected non-object input item error")
	}
	if _, _, err := extractCodexInputMessages(json.RawMessage(`[{]`), extra); err == nil {
		t.Fatal("expected malformed input item error")
	}
}

func TestCodexInputItemToMessage_Branches(t *testing.T) {
	t.Parallel()

	msg, ok, err := codexInputItemToMessage(7, json.RawMessage(`{"role":"system","content":"policy text"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Index != 7 || msg.Role != "system" || msg.TextContent() != "policy text" {
		t.Fatalf("explicit role content: ok=%v msg=%#v", ok, msg)
	}

	msg, ok, err = codexInputItemToMessage(8, json.RawMessage(`{"type":"function_call","id":"call_fallback","name":"read_file","arguments":{"path":"README.md"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Role != "assistant" || msg.Content[0].ToolUseID != "call_fallback" || !strings.Contains(msg.Content[0].ToolInput, "README.md") {
		t.Fatalf("function call fallback mapping: ok=%v msg=%#v", ok, msg)
	}

	msg, ok, err = codexInputItemToMessage(9, json.RawMessage(`{"type":"function_call_output","id":"call_out","output":{"ok":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Role != "tool" || msg.Content[0].ToolResultID != "call_out" || msg.Content[0].Text != `{"ok":true}` {
		t.Fatalf("function output fallback mapping: ok=%v msg=%#v", ok, msg)
	}

	if msg, ok, err = codexInputItemToMessage(10, json.RawMessage(`{"type":"unknown","content":"ignored"}`)); err != nil || ok || msg.Role != "" {
		t.Fatalf("unknown item should be ignored, ok=%v msg=%#v err=%v", ok, msg, err)
	}
	if msg, ok, err = codexInputItemToMessage(10, json.RawMessage(`{"type":"message","content":[{"type":"input_text","text":"one"},{"type":"output_text","text":"two"}]}`)); err != nil || ok || msg.Role != "" {
		t.Fatalf("multi-text content should be unsupported, ok=%v msg=%#v err=%v", ok, msg, err)
	}
	if msg, ok, err = codexInputItemToMessage(10, json.RawMessage(`{"type":"message","content":{"unexpected":true}}`)); err != nil || ok || msg.Role != "" {
		t.Fatalf("object content should be unsupported, ok=%v msg=%#v err=%v", ok, msg, err)
	}
	if msg, ok, err = codexInputItemToMessage(10, json.RawMessage(`{"type":"message","role":"assistant"}`)); err != nil || ok || msg.Role != "" {
		t.Fatalf("content-free message should be unsupported, ok=%v msg=%#v err=%v", ok, msg, err)
	}
	if _, _, err = codexInputItemToMessage(11, json.RawMessage(`{]`)); err == nil {
		t.Fatal("expected malformed item error")
	}

	if codexRoleForInputType("unrecognized") != "" || rawJSONString(json.RawMessage(`{"not":"string"}`)) != "" || rawJSONText(nil) != "" || firstNonEmpty("", "") != "" {
		t.Fatal("expected empty helper fallbacks")
	}
}

func TestCodexTextFromContent_Branches(t *testing.T) {
	t.Parallel()

	text, path, index := codexTextFromContent(nil)
	if text != "" || path != "" || index != -1 {
		t.Fatalf("empty content text=%q path=%q index=%d", text, path, index)
	}

	text, path, index = codexTextFromContent(json.RawMessage(`{"unexpected":true}`))
	if text != `{"unexpected":true}` || path != "" || index != -1 {
		t.Fatalf("object fallback text=%q path=%q index=%d", text, path, index)
	}

	text, path, index = codexTextFromContent(json.RawMessage(`[{"type":"input_text","text":"first"},{"type":"output_text","text":"second"},{"type":"image","url":"local"}]`))
	if text != "first\nsecond" || path != "" || index != -1 {
		t.Fatalf("multi text fallback text=%q path=%q index=%d", text, path, index)
	}

	text, path, index = codexTextFromContent(json.RawMessage(`[{"type":"image","url":"local"}]`))
	if text != "" || path != "" || index != -1 {
		t.Fatalf("non-text parts text=%q path=%q index=%d", text, path, index)
	}
}

func TestMessagesToCodexInputJSON_Fallbacks(t *testing.T) {
	t.Parallel()

	originalString := json.RawMessage(`"unchanged"`)
	out, err := messagesToCodexInputJSON(originalString, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(originalString) {
		t.Fatalf("empty messages should preserve string input: %s", out)
	}

	originalItems := json.RawMessage(`[{"type":"message","role":"user","content":"unchanged"}]`)
	out, err = messagesToCodexInputJSON(originalItems, []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "no raw"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(originalItems) {
		t.Fatalf("missing raw item should preserve original input: %s", out)
	}

	_, err = messagesToCodexInputJSON(originalItems, []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{{
			Type: "text",
			Text: "replacement",
			RawBlock: codexInputItemRaw{
				Fields: map[string]json.RawMessage{
					"type":    json.RawMessage(`"message"`),
					"role":    json.RawMessage(`"user"`),
					"content": json.RawMessage(`"not-an-array"`),
				},
				TextPath:  "content_part_text",
				TextIndex: 0,
			},
		}},
	}})
	if err == nil {
		t.Fatal("expected invalid content_part_text raw error")
	}

	raw := codexInputItemRaw{
		Fields: map[string]json.RawMessage{
			"type":    json.RawMessage(`"message"`),
			"role":    json.RawMessage(`"user"`),
			"content": json.RawMessage(`[{"type":"input_text","text":"same"}]`),
		},
		TextPath:  "content_part_text",
		TextIndex: 99,
	}
	item, err := codexMessageToInputItem(types.Message{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "replacement"}}}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(item), "replacement") {
		t.Fatalf("out-of-range text index should leave content unchanged: %s", item)
	}
}

func TestServeHTTP_CodexResponsesCompressionAndHeaders(t *testing.T) {
	t.Parallel()
	body := readCodexFixture(t, "tests/fixtures/codex/v1-responses-input.json")
	var capturedBody []byte
	var capturedAuth, capturedUA, capturedSession string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		capturedAuth = r.Header.Get("Authorization")
		capturedUA = r.Header.Get("User-Agent")
		capturedSession = r.Header.Get("x-codex-session-id")
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_test","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = true
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.SlidingWindow = 1
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer codex-oauth-token")
	req.Header.Set("User-Agent", "codex/0.125.0 (rust)")
	req.Header.Set("x-codex-session-id", "sess-redacted")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if capturedAuth != "Bearer codex-oauth-token" || capturedUA == "" || capturedSession != "sess-redacted" {
		t.Fatalf("headers auth=%q ua=%q session=%q", capturedAuth, capturedUA, capturedSession)
	}
	if len(capturedBody) == 0 || len(capturedBody) >= len(body) {
		t.Fatalf("expected shorter upstream body, original=%d captured=%d body=%s", len(body), len(capturedBody), capturedBody)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].Provider != "codex_chatgpt" || summaries[0].Tokens.Saved <= 0 {
		t.Fatalf("debug summary missing codex savings: %#v", summaries)
	}
}

func TestServeHTTP_GenericOpenAIResponsesPassthrough(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"gpt-test","input":"generic responses request"}`)
	var capturedBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_openai","output":[]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "openai-python/2.0")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if string(capturedBody) != string(body) {
		t.Fatalf("generic OpenAI /v1/responses must pass through byte-equal: got %s want %s", capturedBody, body)
	}
	if summaries := p.DebugRecorder().Last(1, false); len(summaries) != 0 {
		t.Fatalf("passthrough request should not record compression summary: %#v", summaries)
	}
}

func TestServeHTTP_CodexUnknownShapePassthrough(t *testing.T) {
	t.Parallel()
	body := []byte(`{"conversation_id":"conv-redacted","metadata":{"route":"unknown"}}`)
	var capturedBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/conversations", strings.NewReader(string(body)))
	req.Header.Set("User-Agent", "codex/0.125.0 (rust)")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if string(capturedBody) != string(body) {
		t.Fatalf("unknown shape must pass through byte-equal: got %s want %s", capturedBody, body)
	}
}

func readCodexFixture(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

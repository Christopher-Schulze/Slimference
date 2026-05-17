package beterse

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestInjectAnthropicNoSystem(t *testing.T) {
	body := []byte(`{"model":"claude","messages":[]}`)
	out, res := Inject(types.Anthropic, body, "")
	if !res.Applied {
		t.Fatalf("expected applied")
	}
	if res.FieldUsed != "system" {
		t.Errorf("field=%q", res.FieldUsed)
	}
	if !strings.Contains(string(out), DefaultHint) {
		t.Errorf("default hint missing: %s", out)
	}
}

func TestInjectAnthropicStringSystem(t *testing.T) {
	body := []byte(`{"system":"You are a helpful assistant.","messages":[]}`)
	out, res := Inject(types.Anthropic, body, "be brief.")
	if !res.Applied {
		t.Fatal("expected applied")
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(out, &raw)
	var s string
	_ = json.Unmarshal(raw["system"], &s)
	if !strings.Contains(s, "You are a helpful assistant.") {
		t.Errorf("existing prompt lost: %q", s)
	}
	if !strings.Contains(s, "be brief.") {
		t.Errorf("hint not appended: %q", s)
	}
}

func TestInjectAnthropicArraySystem(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"Existing instruction."}],"messages":[]}`)
	out, res := Inject(types.Anthropic, body, "hint-x")
	if !res.Applied {
		t.Fatalf("expected applied")
	}
	if !strings.Contains(string(out), "hint-x") {
		t.Errorf("hint missing in array form: %s", out)
	}
	if !strings.Contains(string(out), "Existing instruction.") {
		t.Errorf("original block missing")
	}
}

func TestInjectAnthropicIdempotentString(t *testing.T) {
	body := []byte(`{"system":"existing\n\nhint-x"}`)
	_, res := Inject(types.Anthropic, body, "hint-x")
	if res.Applied {
		t.Errorf("idempotent: hint already present, should not re-inject")
	}
}

func TestInjectAnthropicIdempotentArray(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"hint-x is present"}]}`)
	_, res := Inject(types.Anthropic, body, "hint-x")
	if res.Applied {
		t.Errorf("idempotent: hint present in array, should not re-inject")
	}
}

func TestInjectAnthropicSystemArrayMalformed(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":42}]}`)
	out, res := Inject(types.Anthropic, body, "hint")
	// Block has non-string text; the idempotent guard's
	// Unmarshal fails on that block but the loop continues. The
	// hint should still append.
	if !res.Applied {
		t.Errorf("expected append on malformed-text array, got %s", out)
	}
}

func TestInjectAnthropicMalformedSystem(t *testing.T) {
	// System is a number - neither string nor array - skipped.
	body := []byte(`{"system":42}`)
	_, res := Inject(types.Anthropic, body, "x")
	if res.Applied {
		t.Errorf("unknown system shape should not inject")
	}
}

func TestInjectAnthropicEmptySystem(t *testing.T) {
	body := []byte(`{"system":""}`)
	out, res := Inject(types.Anthropic, body, "hint")
	if !res.Applied {
		t.Errorf("empty system string should accept hint, got %s", out)
	}
}

func TestInjectAnthropicSystemArrayBadJSON(t *testing.T) {
	// Array of numbers - valid JSON outer, but unmarshal to
	// []map[string]json.RawMessage fails.
	body := []byte(`{"system":[42]}`)
	_, res := Inject(types.Anthropic, body, "x")
	if res.Applied {
		t.Errorf("array of non-objects should not inject")
	}
}

func TestInjectOpenAIPrependSystem(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`)
	out, res := Inject(types.OpenAI, body, "be terse.")
	if !res.Applied {
		t.Fatalf("expected applied")
	}
	if res.FieldUsed != "messages[system]" {
		t.Errorf("field=%q", res.FieldUsed)
	}
	if !strings.Contains(string(out), "be terse.") {
		t.Errorf("hint missing: %s", out)
	}
	// Decode and assert the prepended system message is first.
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(out, &raw)
	var msgs []map[string]string
	_ = json.Unmarshal(raw["messages"], &msgs)
	if len(msgs) < 1 {
		t.Fatalf("no messages")
	}
	if msgs[0]["role"] != "system" || !strings.Contains(msgs[0]["content"], "be terse.") {
		t.Errorf("system message not prepended: %+v", msgs)
	}
}

func TestInjectOpenAIExistingSystem(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are X."},{"role":"user","content":"hi"}]}`)
	out, res := Inject(types.OpenAI, body, "be terse.")
	if !res.Applied {
		t.Fatalf("expected applied")
	}
	if !strings.Contains(string(out), "You are X.") {
		t.Errorf("existing system message lost")
	}
	if !strings.Contains(string(out), "be terse.") {
		t.Errorf("hint missing")
	}
}

func TestInjectOpenAIIdempotent(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are X.\n\nbe terse."}]}`)
	_, res := Inject(types.OpenAI, body, "be terse.")
	if res.Applied {
		t.Errorf("idempotent: hint already present")
	}
}

func TestInjectOpenAINoMessages(t *testing.T) {
	body := []byte(`{"model":"gpt-5"}`)
	_, res := Inject(types.OpenAI, body, "x")
	if res.Applied {
		t.Errorf("no messages field should fail safely")
	}
}

func TestInjectOpenAIMessagesNotArray(t *testing.T) {
	body := []byte(`{"messages":"not_array"}`)
	_, res := Inject(types.OpenAI, body, "x")
	if res.Applied {
		t.Errorf("non-array messages should fail safely")
	}
}

func TestInjectOpenAISystemRoleNoContent(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system"}]}`)
	out, res := Inject(types.OpenAI, body, "hint")
	// Head is system but has no content; we fall through to prepend a new system message.
	if !res.Applied {
		t.Errorf("expected fallback to prepend, got %s", out)
	}
}

func TestInjectOpenAISystemRoleContentNotString(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":42}]}`)
	out, res := Inject(types.OpenAI, body, "hint")
	// content is numeric - fallback to prepend.
	if !res.Applied {
		t.Errorf("expected fallback prepend, got %s", out)
	}
}

func TestInjectCodexChatGPT(t *testing.T) {
	// Codex routes through the same OpenAI helper.
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	_, res := Inject(types.CodexChatGPT, body, "hint")
	if !res.Applied {
		t.Errorf("codex_chatgpt should accept hint")
	}
}

func TestInjectCodexResponsesInputPrependsSystemMessage(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","input":[{"type":"message","role":"user","content":"hi"}],"stream":true}`)
	out, res := Inject(types.CodexChatGPT, body, "hint")
	if !res.Applied {
		t.Fatalf("expected applied")
	}
	if res.FieldUsed != "input[system]" {
		t.Fatalf("field=%q, want input[system]", res.FieldUsed)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(out, &raw)
	var items []map[string]string
	_ = json.Unmarshal(raw["input"], &items)
	if len(items) < 2 {
		t.Fatalf("expected prepended system item, got %s", out)
	}
	if items[0]["role"] != "system" || items[0]["content"] != "hint" {
		t.Fatalf("bad system item: %+v", items[0])
	}
	if items[1]["content"] != "hi" {
		t.Fatalf("user item not preserved: %+v", items)
	}
}

func TestInjectCodexResponsesInputExistingSystemIdempotent(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"system","content":"hint"},{"type":"message","role":"user","content":"hi"}]}`)
	_, res := Inject(types.CodexChatGPT, body, "hint")
	if res.Applied {
		t.Fatalf("already-present hint should not re-inject")
	}
}

func TestInjectCodexResponsesInputExistingSystemAppendsAndSkipsBadContent(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"system","content":42},{"type":"message","role":"system","content":"base"},{"type":"message","role":"user","content":"hi"}]}`)
	out, res := Inject(types.CodexChatGPT, body, "hint")
	if !res.Applied {
		t.Fatal("expected append to string system item after skipping bad content")
	}
	if res.FieldUsed != "input[system]" {
		t.Fatalf("field=%q, want input[system]", res.FieldUsed)
	}
	if !strings.Contains(string(out), "base\\n\\nhint") {
		t.Fatalf("hint not appended to existing system item: %s", out)
	}
}

func TestInjectCodexResponsesInputNoMatchEdges(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{not-json`),
		[]byte(`{"model":"gpt-5-codex"}`),
		[]byte(`{"input":"not-array"}`),
	} {
		out, res := Inject(types.CodexChatGPT, body, "hint")
		if res.Applied {
			t.Fatalf("unexpected apply for %s", body)
		}
		if string(out) != string(body) {
			t.Fatalf("body mutated for %s: %s", body, out)
		}
	}
	if got, ok := rawStringOK(json.RawMessage(`42`)); ok || got != "" {
		t.Fatalf("rawStringOK numeric = (%q,%v), want empty false", got, ok)
	}
}

func TestInjectEmptyBody(t *testing.T) {
	out, res := Inject(types.Anthropic, nil, "x")
	if res.Applied {
		t.Errorf("nil body should not apply")
	}
	if out != nil {
		t.Errorf("body mutated from nil")
	}
}

func TestInjectMalformedJSON(t *testing.T) {
	out, res := Inject(types.Anthropic, []byte(`{not json`), "x")
	if res.Applied {
		t.Errorf("malformed JSON: should not apply")
	}
	if string(out) != `{not json` {
		t.Errorf("body mutated")
	}
	out2, res2 := Inject(types.OpenAI, []byte(`{not json`), "x")
	if res2.Applied {
		t.Errorf("OpenAI malformed: should not apply")
	}
	if string(out2) != `{not json` {
		t.Errorf("body mutated")
	}
}

func TestInjectUnknownProvider(t *testing.T) {
	out, res := Inject(types.MiniMax, []byte(`{"x":1}`), "hint")
	if res.Applied {
		t.Errorf("unknown provider should be no-op")
	}
	if string(out) != `{"x":1}` {
		t.Errorf("body mutated")
	}
}

func TestDefaultHintConstant(t *testing.T) {
	if DefaultHint == "" {
		t.Errorf("DefaultHint must not be empty")
	}
}

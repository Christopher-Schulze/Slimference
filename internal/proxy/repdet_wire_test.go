package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/outstop/repdet"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// errReader returns an error on every Read - exercises the
// io.ReadAll error branch in passthrough*WithRepdet helpers.
type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("injected read error") }
func (errReader) Close() error               { return nil }

// oversizedReader returns more bytes than maxUpstreamResponseBodySize
// to trigger the size-limit branch.
type oversizedReader struct {
	remaining int
}

func (o *oversizedReader) Read(p []byte) (int, error) {
	if o.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > o.remaining {
		n = o.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = 'A'
	}
	o.remaining -= n
	return n, nil
}

func (o *oversizedReader) Close() error { return nil }

func makeRepdetIndexWith(block string) *repdet.Index {
	idx := repdet.NewIndex()
	idx.AddBlock("src/test.go", 1, 50, block)
	return idx
}

func TestRewriteAnthropicResponseBodyEmptyIndex(t *testing.T) {
	idx := repdet.NewIndex()
	out, saved := rewriteAnthropicResponseBody([]byte(`{"content":[{"type":"text","text":"hi"}]}`), idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0", saved)
	}
	if string(out) != `{"content":[{"type":"text","text":"hi"}]}` {
		t.Errorf("body mutated: %s", out)
	}
}

func TestRewriteAnthropicResponseBodyShort(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	out, saved := rewriteAnthropicResponseBody([]byte(`{"a":1}`), idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 (body too short)", saved)
	}
	if string(out) != `{"a":1}` {
		t.Errorf("body mutated: %s", out)
	}
}

func TestRewriteAnthropicResponseBodyMalformedJSON(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	// Body must be > MinMatch to reach the unmarshal step.
	bogus := append([]byte("{not json"), make([]byte, repdet.MinMatch)...)
	out, saved := rewriteAnthropicResponseBody(bogus, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 on malformed JSON", saved)
	}
	if string(out) != string(bogus) {
		t.Errorf("body mutated on malformed JSON")
	}
}

func TestRewriteAnthropicResponseBodyNoContentField(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"foo":"bar","baz":"` + strings.Repeat("Y", 300) + `"}`)
	out, saved := rewriteAnthropicResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 (no content field)", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated when no content field")
	}
}

func TestRewriteAnthropicResponseBodyContentNotArray(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"content":"not an array","pad":"` + strings.Repeat("P", 300) + `"}`)
	out, saved := rewriteAnthropicResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 (content not array)", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated when content isn't array")
	}
}

func TestRewriteAnthropicResponseBodyBlockWithoutType(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"content":[{"foo":"bar"}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, saved := rewriteAnthropicResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 (block has no type)", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated when block lacks type")
	}
}

func TestRewriteAnthropicResponseBodyBlockNonText(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"content":[{"type":"tool_use","name":"X"}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, saved := rewriteAnthropicResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 (non-text block)", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated when block is tool_use")
	}
}

func TestRewriteAnthropicResponseBodyTypeFieldNotString(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"content":[{"type":123}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteAnthropicResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when type is numeric")
	}
}

func TestRewriteAnthropicResponseBodyTextFieldMissing(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"content":[{"type":"text"}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteAnthropicResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when text field is missing")
	}
}

func TestRewriteAnthropicResponseBodyTextFieldNotString(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"content":[{"type":"text","text":42}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteAnthropicResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when text field is numeric")
	}
}

func TestRewriteAnthropicResponseBodyNoMatchInText(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"content":[{"type":"text","text":"` + strings.Repeat("Y", 400) + `"}]}`)
	out, saved := rewriteAnthropicResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 (no match in text)", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated when no match")
	}
}

func TestRewriteAnthropicResponseBodyEchoRewritten(t *testing.T) {
	block := strings.Repeat("Echo content. ", 30)
	idx := makeRepdetIndexWith(block)
	body, _ := json.Marshal(map[string]any{
		"content": []map[string]any{{"type": "text", "text": "intro " + block + " outro"}},
	})
	out, saved := rewriteAnthropicResponseBody(body, idx)
	if saved == 0 {
		t.Fatalf("expected saved>0, got 0")
	}
	if !strings.Contains(string(out), "[unchanged:") {
		t.Errorf("marker missing: %s", out)
	}
	if strings.Contains(string(out), block) {
		t.Errorf("echo block not replaced")
	}
}

func TestBuildRepdetIndexEmptyMessages(t *testing.T) {
	idx := buildRepdetIndex(nil)
	if len(idx.Blocks()) != 0 {
		t.Errorf("expected empty index, got %d blocks", len(idx.Blocks()))
	}
}

func TestBuildRepdetIndexSkipsShortText(t *testing.T) {
	idx := buildRepdetIndex([]types.Message{{
		Role: "user",
		Content: []types.ContentBlock{
			{Type: "text", Text: "short prompt"},
		},
	}})
	if len(idx.Blocks()) != 0 {
		t.Errorf("expected short text to be skipped, got %d blocks", len(idx.Blocks()))
	}
}

func TestBuildRepdetIndexIncludesLongText(t *testing.T) {
	longText := strings.Repeat("X", repdet.MinMatch+repdet.WindowSize+10)
	idx := buildRepdetIndex([]types.Message{{
		Role: "user",
		Content: []types.ContentBlock{
			{Type: "text", Text: longText},
		},
	}})
	if len(idx.Blocks()) != 1 {
		t.Errorf("expected long text indexed, got %d blocks", len(idx.Blocks()))
	}
}

// --- OpenAI / Codex wire (T183) ---

func TestRewriteOpenAIResponseBodyEmptyIndex(t *testing.T) {
	idx := repdet.NewIndex()
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	out, saved := rewriteOpenAIResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated under empty index")
	}
}

func TestRewriteOpenAIResponseBodyShort(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	out, saved := rewriteOpenAIResponseBody([]byte(`{"a":1}`), idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 (body too short)", saved)
	}
	if string(out) != `{"a":1}` {
		t.Errorf("body mutated")
	}
}

func TestRewriteOpenAIResponseBodyMalformedJSON(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	bogus := append([]byte("{not json"), make([]byte, repdet.MinMatch)...)
	out, saved := rewriteOpenAIResponseBody(bogus, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 on malformed JSON", saved)
	}
	if string(out) != string(bogus) {
		t.Errorf("body mutated on malformed JSON")
	}
}

func TestRewriteOpenAIChatCompletionsRewritten(t *testing.T) {
	block := strings.Repeat("Echo line content. ", 30)
	idx := makeRepdetIndexWith(block)
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": "intro " + block + " outro"}},
		},
	})
	out, saved := rewriteOpenAIResponseBody(body, idx)
	if saved == 0 {
		t.Fatalf("expected saved>0, got 0; body=%s", out)
	}
	if !strings.Contains(string(out), "[unchanged:") {
		t.Errorf("marker missing: %s", out)
	}
	if strings.Contains(string(out), block) {
		t.Errorf("echo block not replaced")
	}
}

func TestRewriteOpenAIChatCompletionsArrayContent(t *testing.T) {
	// Tool-call responses sometimes ship content as an array. The
	// unmarshal-to-string fails; the helper must skip cleanly.
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"x"}]}}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, saved := rewriteOpenAIResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 (array content)", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated on array content")
	}
}

func TestRewriteOpenAIChatCompletionsNullContent(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{}}},
		},
		"pad": strings.Repeat("P", 300),
	})
	out, saved := rewriteOpenAIResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 (null content)", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated on null content")
	}
}

func TestRewriteOpenAIChatCompletionsMessageNotObject(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"choices":[{"message":"not_an_object"}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated on non-object message")
	}
}

func TestRewriteOpenAIChatCompletionsChoicesNotArray(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"choices":"not_array","pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when choices isn't array")
	}
}

func TestRewriteOpenAIChatCompletionsNoMessage(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"choices":[{"index":0}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when choice lacks message")
	}
}

func TestRewriteOpenAIChatCompletionsNoContent(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"choices":[{"message":{"role":"assistant"}}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when message lacks content")
	}
}

func TestRewriteOpenAIResponsesAPIRewritten(t *testing.T) {
	block := strings.Repeat("Echo line content. ", 30)
	idx := makeRepdetIndexWith(block)
	body, _ := json.Marshal(map[string]any{
		"output": []map[string]any{{
			"content": []map[string]any{
				{"type": "output_text", "text": "intro " + block + " outro"},
			},
		}},
	})
	out, saved := rewriteOpenAIResponseBody(body, idx)
	if saved == 0 {
		t.Fatalf("expected saved>0 for responses API; body=%s", out)
	}
	if !strings.Contains(string(out), "[unchanged:") {
		t.Errorf("marker missing: %s", out)
	}
	if strings.Contains(string(out), block) {
		t.Errorf("echo block not replaced in responses API shape")
	}
}

func TestRewriteOpenAIResponsesAPITypeText(t *testing.T) {
	// Older Responses-API variant uses type="text".
	block := strings.Repeat("Echo line content. ", 30)
	idx := makeRepdetIndexWith(block)
	body, _ := json.Marshal(map[string]any{
		"output": []map[string]any{{
			"content": []map[string]any{
				{"type": "text", "text": "before " + block + " after"},
			},
		}},
	})
	out, saved := rewriteOpenAIResponseBody(body, idx)
	if saved == 0 {
		t.Fatalf("expected saved>0 for type=text variant")
	}
	if strings.Contains(string(out), block) {
		t.Errorf("echo block not replaced")
	}
}

func TestRewriteOpenAIResponsesAPIOutputNotArray(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"output":"not_array","pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when output isn't array")
	}
}

func TestRewriteOpenAIResponsesAPIOutputItemWithoutContent(t *testing.T) {
	// Output array contains an item with no content field; second
	// item has valid content - first must be skipped, second processed.
	block := strings.Repeat("Echo line content. ", 30)
	idx := makeRepdetIndexWith(block)
	body, _ := json.Marshal(map[string]any{
		"output": []map[string]any{
			{"role": "assistant"}, // no content field
			{"content": []map[string]any{
				{"type": "output_text", "text": "intro " + block + " outro"},
			}},
		},
	})
	out, saved := rewriteOpenAIResponseBody(body, idx)
	if saved == 0 {
		t.Fatalf("expected saved>0 when second item carries content; got 0; out=%s", out)
	}
}

func TestRewriteOpenAIResponsesAPIContentNotArray(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"output":[{"content":"not_array"}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when output[].content isn't array")
	}
}

func TestRewriteOpenAIResponsesAPIPartNoType(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"output":[{"content":[{"foo":"bar"}]}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when part lacks type")
	}
}

func TestRewriteOpenAIResponsesAPIPartUnknownType(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"output":[{"content":[{"type":"image"}]}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when part type is non-text")
	}
}

func TestRewriteOpenAIResponsesAPITypeNotString(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"output":[{"content":[{"type":42,"text":"x"}]}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when type is numeric")
	}
}

func TestRewriteOpenAIResponsesAPINoText(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"output":[{"content":[{"type":"output_text"}]}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when part lacks text")
	}
}

func TestRewriteOpenAIResponsesAPITextNotString(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("Z", 500))
	body := []byte(`{"output":[{"content":[{"type":"output_text","text":42}]}],"pad":"` + strings.Repeat("P", 300) + `"}`)
	out, _ := rewriteOpenAIResponseBody(body, idx)
	if string(out) != string(body) {
		t.Errorf("body mutated when text is numeric")
	}
}

func TestRewriteOpenAIResponsesAPINoMatch(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"output":[{"content":[{"type":"output_text","text":"` + strings.Repeat("Y", 400) + `"}]}]}`)
	out, saved := rewriteOpenAIResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 when no match", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated when no match")
	}
}

func TestRewriteOpenAINoChoicesNoOutput(t *testing.T) {
	idx := makeRepdetIndexWith(strings.Repeat("X", 500))
	body := []byte(`{"id":"x","model":"gpt-5","pad":"` + strings.Repeat("P", 300) + `"}`)
	out, saved := rewriteOpenAIResponseBody(body, idx)
	if saved != 0 {
		t.Errorf("saved=%d want 0 when no recognised shape", saved)
	}
	if string(out) != string(body) {
		t.Errorf("body mutated")
	}
}

func TestPassthroughAnthropicWithRepdetIoError(t *testing.T) {
	p := New(config.Defaults())
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       errReader{},
	}
	rec := httptest.NewRecorder()
	out := p.passthroughAnthropicWithRepdet(rec, resp, nil, nil)
	if out != nil {
		t.Errorf("expected nil on io error, got %d bytes", len(out))
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status=%d want 502 on io error", rec.Code)
	}
}

func TestPassthroughAnthropicWithRepdetOversized(t *testing.T) {
	p := New(config.Defaults())
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       &oversizedReader{remaining: maxUpstreamResponseBodySize + 100},
	}
	rec := httptest.NewRecorder()
	out := p.passthroughAnthropicWithRepdet(rec, resp, nil, nil)
	if out != nil {
		t.Errorf("expected nil on oversized response, got %d bytes", len(out))
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status=%d want 502 on oversized response", rec.Code)
	}
}

func TestPassthroughOpenAIWithRepdetIoError(t *testing.T) {
	p := New(config.Defaults())
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       errReader{},
	}
	rec := httptest.NewRecorder()
	out := p.passthroughOpenAIWithRepdet(rec, resp, nil, nil)
	if out != nil {
		t.Errorf("expected nil on io error")
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status=%d want 502", rec.Code)
	}
}

func TestPassthroughOpenAIWithRepdetOversized(t *testing.T) {
	p := New(config.Defaults())
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       &oversizedReader{remaining: maxUpstreamResponseBodySize + 100},
	}
	rec := httptest.NewRecorder()
	out := p.passthroughOpenAIWithRepdet(rec, resp, nil, nil)
	if out != nil {
		t.Errorf("expected nil on oversized response")
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status=%d want 502", rec.Code)
	}
}

func TestBlockNameForToolResultFallbacks(t *testing.T) {
	cases := []struct {
		name string
		b    types.ContentBlock
		want string
	}{
		{"tool_name wins", types.ContentBlock{ToolName: "Read"}, "Read"},
		{"tool_use_id fallback", types.ContentBlock{ToolUseID: "tu1"}, "tool:tu1"},
		{"generic last resort", types.ContentBlock{}, "tool_result"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := blockNameForToolResult(c.b); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

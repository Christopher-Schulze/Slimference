package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestExtractServerStateKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		prov types.Provider
		body string
		want string
	}{
		{"openai metadata.session_id", types.OpenAI, `{"metadata":{"session_id":"sess-A"}}`, "sess-A"},
		{"openai metadata.conversation_id", types.OpenAI, `{"metadata":{"conversation_id":"conv-A"}}`, "conv-A"},
		{"openai previous_response_id fallback", types.OpenAI, `{"previous_response_id":"resp-1"}`, "resp-1"},
		{"openai none", types.OpenAI, `{"messages":[{"role":"user","content":"hi"}]}`, ""},
		{"codex top conversation_id", types.CodexChatGPT, `{"conversation_id":"c1"}`, "c1"},
		{"codex metadata fallback", types.CodexChatGPT, `{"metadata":{"conversation_id":"c2"}}`, "c2"},
		{"anthropic always empty", types.Anthropic, `{"metadata":{"session_id":"x"}}`, ""},
		{"empty body", types.OpenAI, ``, ""},
		{"malformed body", types.OpenAI, `{`, ""},
		{"unknown provider", types.Provider(99), `{"id":"x"}`, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractServerStateKey(tc.prov, []byte(tc.body))
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRewriteWithPreviousID_OpenAIChatShape(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"gpt-4","messages":[{"role":"system","content":"sys"},{"role":"user","content":"a"},{"role":"assistant","content":"a-resp"},{"role":"user","content":"b"}]}`)
	out, ok := rewriteWithPreviousID(types.OpenAI, body, "resp-A")
	if !ok {
		t.Fatal("rewrite must succeed")
	}
	s := string(out)
	if !strings.Contains(s, `"previous_response_id":"resp-A"`) {
		t.Fatalf("missing previous_response_id: %s", s)
	}
	if strings.Contains(s, `"a-resp"`) {
		t.Fatalf("assistant turn must be dropped: %s", s)
	}
	if !strings.Contains(s, `"b"`) {
		t.Fatalf("last user turn must remain: %s", s)
	}
}

func TestRewriteWithPreviousID_OpenAIResponsesInput(t *testing.T) {
	t.Parallel()
	body := []byte(`{"input":[{"role":"user","content":"a"},{"role":"assistant","content":"a-resp"},{"role":"user","content":"b"}]}`)
	out, ok := rewriteWithPreviousID(types.OpenAI, body, "resp-X")
	if !ok {
		t.Fatal("rewrite must succeed")
	}
	if !strings.Contains(string(out), `"previous_response_id":"resp-X"`) {
		t.Fatalf("missing prev id: %s", out)
	}
	if strings.Contains(string(out), `"a-resp"`) {
		t.Fatalf("assistant turn leaked: %s", out)
	}
}

func TestRewriteWithPreviousID_StringInputKept(t *testing.T) {
	t.Parallel()
	body := []byte(`{"input":"hello"}`)
	out, ok := rewriteWithPreviousID(types.OpenAI, body, "resp-1")
	if !ok {
		t.Fatal("rewrite with string input must succeed")
	}
	if !strings.Contains(string(out), `"input":"hello"`) {
		t.Fatalf("string input must remain: %s", out)
	}
	if !strings.Contains(string(out), `"previous_response_id":"resp-1"`) {
		t.Fatalf("prev id missing: %s", out)
	}
}

func TestRewriteWithPreviousID_Codex(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"conversation_id":"c1"}`)
	out, ok := rewriteWithPreviousID(types.CodexChatGPT, body, "resp-C")
	if !ok {
		t.Fatal("codex rewrite must succeed")
	}
	if !strings.Contains(string(out), `"previous_response_id":"resp-C"`) {
		t.Fatalf("prev id missing: %s", out)
	}
	if !strings.Contains(string(out), `"conversation_id":"c1"`) {
		t.Fatalf("conversation id must be preserved: %s", out)
	}
}

func TestRewriteWithPreviousID_Failures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		prov types.Provider
		body string
		prev string
	}{
		{"empty prev id", types.OpenAI, `{"messages":[{"role":"user","content":"x"}]}`, ""},
		{"empty body", types.OpenAI, ``, "p"},
		{"malformed json", types.OpenAI, `{`, "p"},
		{"no messages or input", types.OpenAI, `{"model":"x"}`, "p"},
		{"no user turn", types.OpenAI, `{"messages":[{"role":"assistant","content":"x"}]}`, "p"},
		{"unsupported provider", types.Anthropic, `{"messages":[{"role":"user","content":"x"}]}`, "p"},
		{"messages not an array", types.OpenAI, `{"messages":"oops"}`, "p"},
		{"input neither array nor string", types.OpenAI, `{"input":42}`, "p"},
		{"message entry not object", types.OpenAI, `{"messages":["bare-string"]}`, "p"},
		{"input array no user turn", types.OpenAI, `{"input":[{"role":"system","content":"x"}]}`, "p"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := rewriteWithPreviousID(tc.prov, []byte(tc.body), tc.prev)
			if ok {
				t.Fatalf("expected failure, got ok with %s", got)
			}
			if !bytes.Equal(got, []byte(tc.body)) {
				t.Fatalf("body must be returned unchanged on failure: got %s want %s", got, tc.body)
			}
		})
	}
}

func TestExtractServerStateKey_NonStringFields(t *testing.T) {
	t.Parallel()
	// metadata is an array, not an object → nestedString unmarshal error path.
	got := extractServerStateKey(types.OpenAI, []byte(`{"metadata":[1,2]}`))
	if got != "" {
		t.Fatalf("non-object metadata must yield empty key: %q", got)
	}
	// previous_response_id is a number, not a string → topString unmarshal error path.
	got = extractServerStateKey(types.OpenAI, []byte(`{"previous_response_id":42}`))
	if got != "" {
		t.Fatalf("non-string previous_response_id must yield empty key: %q", got)
	}
}

func TestExtractResponseID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		prov types.Provider
		body string
		want string
	}{
		{"openai id", types.OpenAI, `{"id":"resp_123"}`, "resp_123"},
		{"openai response_id fallback", types.OpenAI, `{"response_id":"r2"}`, "r2"},
		{"codex conversation_id", types.CodexChatGPT, `{"conversation_id":"c1"}`, "c1"},
		{"codex id fallback", types.CodexChatGPT, `{"id":"x1"}`, "x1"},
		{"anthropic ignored", types.Anthropic, `{"id":"x"}`, ""},
		{"empty body", types.OpenAI, ``, ""},
		{"malformed body", types.OpenAI, `{`, ""},
		{"openai no id", types.OpenAI, `{"object":"response"}`, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractResponseID(tc.prov, []byte(tc.body))
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestIsUnknownPreviousIDError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"4xx prev id not found", 404, `{"error":{"message":"previous_response_id not found"}}`, true},
		{"4xx unknown response", 400, `{"error":"unknown response"}`, true},
		{"4xx conversation not found", 404, `{"error":"conversation not found"}`, true},
		{"4xx response not found", 404, `{"error":"response not found"}`, true},
		{"5xx not classified", 500, `{"error":"previous_response_id missing"}`, false},
		{"2xx not classified", 200, `{"id":"x"}`, false},
		{"4xx unrelated", 400, `{"error":"validation failed"}`, false},
		{"4xx empty body", 400, ``, false},
		{"4xx signal but no error word", 400, `{"detail":"previous_response_id stale only"}`, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isUnknownPreviousIDError(tc.status, []byte(tc.body))
			if got != tc.want {
				t.Fatalf("status=%d body=%q got=%v want=%v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func TestPeekUnknownPreviousIDError(t *testing.T) {
	t.Parallel()

	t.Run("recover signal triggers true and body still readable", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader(`{"error":"previous_response_id not found"}`)),
		}
		ok, peeked := peekUnknownPreviousIDError(resp)
		if !ok {
			t.Fatal("expected recover signal")
		}
		if !strings.Contains(string(peeked), "previous_response_id") {
			t.Fatalf("peeked body wrong: %s", peeked)
		}
		// Body must remain readable for the caller.
		again, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(again), "previous_response_id") {
			t.Fatalf("body not restored: %s", again)
		}
	})

	t.Run("non-4xx returns false and does not touch body", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`OK`)),
		}
		ok, peeked := peekUnknownPreviousIDError(resp)
		if ok || peeked != nil {
			t.Fatalf("non-4xx must skip: ok=%v peeked=%q", ok, peeked)
		}
	})

	t.Run("nil resp safe", func(t *testing.T) {
		t.Parallel()
		ok, _ := peekUnknownPreviousIDError(nil)
		if ok {
			t.Fatal("nil response must not signal recovery")
		}
	})

	t.Run("4xx without recover signal is rejected", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{
			StatusCode: 400,
			Body:       io.NopCloser(strings.NewReader(`{"error":"validation failed"}`)),
		}
		ok, peeked := peekUnknownPreviousIDError(resp)
		if ok {
			t.Fatal("unrelated 4xx must not trigger recovery")
		}
		if !strings.Contains(string(peeked), "validation failed") {
			t.Fatalf("peeked body wrong: %s", peeked)
		}
	})
}

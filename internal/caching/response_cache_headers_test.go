package caching

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/types"
)

func TestResponseCache_ComputeRequestKeyWithHeaders_partitionsBySemanticHeaders(t *testing.T) {
	t.Parallel()

	cache := NewResponseCache(10, time.Minute)
	body := []byte(`{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hello"}]}`)

	headersA := http.Header{
		"X-Api-Key":         []string{"key-a"},
		"Anthropic-Version": []string{"2023-06-01"},
		"Anthropic-Beta":    []string{"files-2025-01-01,prompt-caching-2024-07-31"},
	}
	headersB := http.Header{
		"Anthropic-Beta":    []string{"prompt-caching-2024-07-31", "files-2025-01-01"},
		"Anthropic-Version": []string{"2023-06-01"},
		"X-Api-Key":         []string{"key-a"},
	}
	headersC := http.Header{
		"X-Api-Key":         []string{"key-b"},
		"Anthropic-Version": []string{"2023-06-01"},
		"Anthropic-Beta":    []string{"prompt-caching-2024-07-31"},
	}
	headersD := http.Header{
		"X-Api-Key":         []string{"key-a"},
		"Anthropic-Version": []string{"2023-01-01"},
	}

	keyA := cache.ComputeRequestKeyWithHeaders(types.Anthropic, body, headersA)
	keyB := cache.ComputeRequestKeyWithHeaders(types.Anthropic, body, headersB)
	keyC := cache.ComputeRequestKeyWithHeaders(types.Anthropic, body, headersC)
	keyD := cache.ComputeRequestKeyWithHeaders(types.Anthropic, body, headersD)

	if keyA != keyB {
		t.Fatal("equivalent semantic headers should canonicalize to the same cache key")
	}
	if keyA == keyC {
		t.Fatal("different account credentials must not share a cache key")
	}
	if keyA == keyD {
		t.Fatal("different Anthropic version headers must not share a cache key")
	}
}

func TestResponseCache_ComputeRequestKeyWithRoutePartitionsEndpoints(t *testing.T) {
	t.Parallel()

	cache := NewResponseCache(10, time.Minute)
	body := []byte(`{"model":"gpt-5","input":"hello"}`)
	headers := http.Header{"Authorization": []string{"Bearer same"}}

	responsesKey := cache.ComputeRequestKeyWithRoute(types.OpenAI, "POST /v1/responses", body, headers)
	chatKey := cache.ComputeRequestKeyWithRoute(types.OpenAI, "POST /v1/chat/completions", body, headers)
	queryKey := cache.ComputeRequestKeyWithRoute(types.OpenAI, "POST /v1/responses?experimental=1", body, headers)
	getKey := cache.ComputeRequestKeyWithRoute(types.OpenAI, "GET /v1/responses", body, headers)
	legacyKey := cache.ComputeRequestKeyWithHeaders(types.OpenAI, body, headers)

	if responsesKey == chatKey {
		t.Fatal("different provider endpoints must not share a response-cache key")
	}
	if responsesKey == queryKey {
		t.Fatal("different endpoint queries must not share a response-cache key")
	}
	if responsesKey == getKey {
		t.Fatal("different HTTP methods must not share a response-cache key")
	}
	if responsesKey == legacyKey {
		t.Fatal("route-aware and legacy route-empty keys must differ")
	}
}

func TestIsRequestCacheSafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "missing temperature is not replay safe",
			body: `{"model":"claude","messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "explicit zero temperature still cache safe",
			body: `{"model":"claude","temperature":0,"messages":[{"role":"user","content":"hello"}]}`,
			want: true,
		},
		{
			name: "temperature above zero disables cache",
			body: `{"model":"claude","temperature":0.3,"messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "top_p below one disables cache",
			body: `{"model":"claude","temperature":0,"top_p":0.8,"messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "multiple completions disable cache",
			body: `{"model":"gpt-4","temperature":0,"n":2,"messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "streaming disables cache",
			body: `{"model":"gpt-4","temperature":0,"stream":true,"messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "string boolean disables cache",
			body: `{"model":"gpt-4","temperature":0,"stream":"true","messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "string numeric disables cache",
			body: `{"model":"gpt-4","temperature":0,"top_p":"0.5","messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "tools disable cache",
			body: `{"model":"gpt-4","temperature":0,"tools":[{"type":"function","function":{"name":"read_file"}}],"messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "tool choice disables cache",
			body: `{"model":"gpt-4","temperature":0,"tool_choice":{"type":"function","function":{"name":"read_file"}},"messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "tool role disables cache",
			body: `{"model":"gpt-4","temperature":0,"messages":[{"role":"tool","content":"result"}]}`,
			want: false,
		},
		{
			name: "responses function call output disables cache",
			body: `{"model":"gpt-4","input":[{"type":"function_call_output","call_id":"c","output":"result"}]}`,
			want: false,
		},
		{
			name: "invalid json is not cache safe",
			body: `{"model":`,
			want: false,
		},
		{
			name: "empty body is not cache safe",
			body: ``,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRequestCacheSafe([]byte(tt.body)); got != tt.want {
				t.Fatalf("IsRequestCacheSafe() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanonicalizeCacheHeaders(t *testing.T) {
	t.Parallel()

	if got := canonicalizeCacheHeaders(nil); got != nil {
		t.Fatalf("nil headers = %q, want nil", string(got))
	}
	if got := canonicalizeCacheHeaders(http.Header{"User-Agent": []string{"codex"}}); got != nil {
		t.Fatalf("irrelevant headers = %q, want nil", string(got))
	}

	headers := http.Header{
		"Authorization":       []string{"Bearer secret-token"},
		"OpenAI-Beta":         []string{"responses=v1, assistants=v2", "", " assistants=v2 "},
		"OpenAI-Project":      []string{"  proj-123  "},
		"OpenAI-Organization": []string{"org-1"},
		"Anthropic-Version":   []string{"   "},
		"User-Agent":          []string{"ignored"},
	}
	got := canonicalizeCacheHeaders(headers)
	if got == nil {
		t.Fatal("expected canonicalized headers")
	}

	var decoded map[string][]string
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal canonical headers: %v", err)
	}
	if len(decoded["authorization"]) != 1 || !strings.HasPrefix(decoded["authorization"][0], "sha256:") {
		t.Fatalf("authorization = %#v, want hashed value", decoded["authorization"])
	}
	if strings.Contains(decoded["authorization"][0], "secret-token") {
		t.Fatal("authorization header must not retain the raw secret")
	}
	if got := decoded["openai-beta"]; len(got) != 3 || got[0] != "assistants=v2" || got[2] != "responses=v1" {
		t.Fatalf("openai-beta = %#v, want sorted split values", got)
	}
	if _, ok := decoded["user-agent"]; ok {
		t.Fatal("irrelevant headers must be omitted")
	}
	if _, ok := decoded["anthropic-version"]; ok {
		t.Fatal("empty relevant headers must be omitted")
	}
}

func TestCanonicalizeCacheHeaders_marshalError(t *testing.T) {
	orig := jsonMarshalFn
	jsonMarshalFn = func(v interface{}) ([]byte, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		jsonMarshalFn = orig
	})

	got := canonicalizeCacheHeaders(http.Header{"Authorization": []string{"Bearer secret-token"}})
	if got != nil {
		t.Fatalf("canonicalizeCacheHeaders() = %q, want nil on marshal error", string(got))
	}
}

func TestCachePrimitiveHelpers(t *testing.T) {
	t.Parallel()

	if !truthyBool(true) {
		t.Fatal("bool true must be truthy")
	}
	if truthyBool(false) {
		t.Fatal("bool false must not be truthy")
	}
	if !truthyBool(" TRUE ") {
		t.Fatal("string true must be truthy")
	}
	if truthyBool(1) {
		t.Fatal("non-bool non-string values must not be truthy")
	}

	numericCases := []struct {
		name  string
		value interface{}
		want  float64
		ok    bool
	}{
		{name: "float64", value: float64(1.5), want: 1.5, ok: true},
		{name: "float32", value: float32(2.5), want: 2.5, ok: true},
		{name: "int", value: 3, want: 3, ok: true},
		{name: "int64", value: int64(4), want: 4, ok: true},
		{name: "json number", value: json.Number("5.5"), want: 5.5, ok: true},
		{name: "string", value: " 6.25 ", want: 6.25, ok: true},
		{name: "bad json number", value: json.Number("x"), ok: false},
		{name: "bad string", value: "nope", ok: false},
		{name: "unsupported", value: true, ok: false},
	}

	for _, tt := range numericCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := numericValue(tt.value)
			if ok != tt.ok {
				t.Fatalf("numericValue(%v) ok = %v, want %v", tt.value, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Fatalf("numericValue(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

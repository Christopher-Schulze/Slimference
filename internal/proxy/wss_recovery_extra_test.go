package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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

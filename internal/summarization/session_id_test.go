package summarization

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestExtractSessionID_AnthropicOrg(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("anthropic-organization-id", "org-123")
	id := ExtractSessionID(types.Anthropic, []byte(`{"messages":[{"role":"user","content":"hello"}]}`), h)
	if id != "anthropic:org-123" {
		t.Fatalf("got %q", id)
	}
}

func TestExtractSessionID_AnthropicNoHeaders(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"fallback test"}]}`)
	id := ExtractSessionID(types.Anthropic, body, http.Header{})
	if id == "" || id == "empty" {
		t.Fatalf("expected fallback hash, got %q", id)
	}
}

func TestExtractSessionID_AnthropicBadJSON(t *testing.T) {
	t.Parallel()
	id := ExtractSessionID(types.Anthropic, []byte(`not json`), http.Header{})
	if id == "" {
		t.Fatalf("expected some id, got %q", id)
	}
}

func TestExtractSessionID_OpenAINoHeaders(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"openai fallback"}]}`)
	id := ExtractSessionID(types.OpenAI, body, http.Header{})
	if id == "" || id == "empty" {
		t.Fatalf("expected fallback hash, got %q", id)
	}
}

func TestExtractSessionID_EmptyBody(t *testing.T) {
	t.Parallel()
	id := ExtractSessionID(types.Anthropic, nil, http.Header{})
	if id != "empty" {
		t.Fatalf("got %q", id)
	}
}

func TestExtractSessionID_EmptyMessages(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[]}`)
	id := ExtractSessionID(types.Anthropic, body, http.Header{})
	if id != "empty" {
		t.Fatalf("got %q", id)
	}
}

func TestExtractSessionID_NonStringContent(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":123}]}`)
	id := ExtractSessionID(types.Anthropic, body, http.Header{})
	if id == "" {
		t.Fatalf("expected some id for non-string content, got %q", id)
	}
}

func TestExtractSessionID_LongContentTruncation(t *testing.T) {
	t.Parallel()
	longText := strings.Repeat("x", 500)
	body := []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":"%s"}]}`, longText))
	id := ExtractSessionID(types.Anthropic, body, http.Header{})
	if id == "" || id == "empty" {
		t.Fatalf("expected hash for long content, got %q", id)
	}
}

func TestExtractSessionID_MetadataBadJSON(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("anthropic-organization-id", "org-x")
	id := ExtractSessionID(types.Anthropic, []byte(`not json`), h)
	if id != "anthropic:org-x" {
		t.Fatalf("expected org-only id, got %q", id)
	}
}

func TestExtractSessionID_PreviousResponseBadJSON(t *testing.T) {
	t.Parallel()
	id := ExtractSessionID(types.OpenAI, []byte(`not json`), http.Header{})
	if id == "" {
		t.Fatalf("expected fallback, got %q", id)
	}
}

func TestExtractSessionID_AnthropicWithUserID(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("anthropic-organization-id", "org-abc")
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"metadata":{"user_id":"u-42"}}`)
	id := ExtractSessionID(types.Anthropic, body, h)
	if id != "anthropic:org-abc:u-42" {
		t.Fatalf("got %q", id)
	}
}

func TestExtractSessionID_AnthropicTraceID(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("anthropic-trace-id", "trace-xyz")
	id := ExtractSessionID(types.Anthropic, nil, h)
	if id != "anthropic:trace-xyz" {
		t.Fatalf("got %q", id)
	}
}

func TestExtractSessionID_OpenAIConversationID(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("openai-conversation-id", "conv-789")
	id := ExtractSessionID(types.OpenAI, nil, h)
	if id != "openai:conv-789" {
		t.Fatalf("got %q", id)
	}
}

func TestExtractSessionID_OpenAIPreviousResponseID(t *testing.T) {
	t.Parallel()
	body := []byte(`{"previous_response_id":"resp-001","messages":[{"role":"user","content":"hey"}]}`)
	id := ExtractSessionID(types.OpenAI, body, http.Header{})
	if id != "openai:resp-001" {
		t.Fatalf("got %q", id)
	}
}

func TestExtractSessionID_ContentHashFallback(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"unique prompt text here"}]}`)
	id := ExtractSessionID(types.Anthropic, body, http.Header{})
	if id == "" || id == "empty" {
		t.Fatalf("expected content hash, got %q", id)
	}
	if id[:3] != "fh:" {
		t.Fatalf("expected fh: prefix, got %q", id)
	}
}

func TestExtractSessionID_SameContentSameHash(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"identical prompt"}]}`)
	id1 := ExtractSessionID(types.Anthropic, body, http.Header{})
	id2 := ExtractSessionID(types.OpenAI, body, http.Header{})
	if id1 != id2 {
		t.Fatalf("same content should produce same fallback: %q vs %q", id1, id2)
	}
}

func TestExtractSessionID_DifferentContentDifferentHash(t *testing.T) {
	t.Parallel()
	b1 := []byte(`{"messages":[{"role":"user","content":"prompt A"}]}`)
	b2 := []byte(`{"messages":[{"role":"user","content":"prompt B"}]}`)
	id1 := ExtractSessionID(types.Anthropic, b1, http.Header{})
	id2 := ExtractSessionID(types.Anthropic, b2, http.Header{})
	if id1 == id2 {
		t.Fatalf("different prompts should produce different hashes: %q", id1)
	}
}

func TestExtractSessionID_StructuredContent(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"structured hello"}]}]}`)
	id := ExtractSessionID(types.Anthropic, body, http.Header{})
	if id == "" || id == "empty" {
		t.Fatalf("expected content hash from structured content, got %q", id)
	}
}

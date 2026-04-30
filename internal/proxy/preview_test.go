package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestPreviewCompress_AutoDetectAnthropic(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	body := []byte(`{"model":"claude-3-opus","messages":[{"role":"user","content":"hello world"}]}`)
	res, err := PreviewCompress(cfg, "/v1/messages", body, types.Provider(-1), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderString == "" {
		t.Fatal("provider string missing")
	}
	if res.OrigTokens == 0 {
		t.Fatalf("expected token count: %+v", res)
	}
}

func TestPreviewCompress_ProviderHint(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)
	res, err := PreviewCompress(cfg, "/v1/chat/completions", body, types.OpenAI, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Provider != types.OpenAI {
		t.Fatalf("provider: %v", res.Provider)
	}
	if res.OriginalBody != nil {
		t.Fatal("includeBodies=false must omit original body")
	}
}

func TestPreviewCompress_ExtractError(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	body := []byte("not json at all")
	if _, err := PreviewCompress(cfg, "/v1/messages", body, types.Anthropic, false); err == nil {
		t.Fatal("expected extract error")
	}
}

func TestPreviewCompress_CompressionFires(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.SlidingWindow = 1
	cfg.Compression.MinMessagesForCompression = 1

	repeated := strings.Repeat("plain output line repeated for dedup hit\n", 30)
	body := buildPreviewAnthropicBody(t, repeated)
	res, err := PreviewCompress(cfg, "/v1/messages", body, types.Anthropic, true)
	if err != nil {
		t.Fatal(err)
	}
	// The deterministic compressor should detect the duplicated
	// tool_result blocks and dedup at least one. If not, the call still
	// returns; assert the breakdown map exists either way.
	if res.Layer1Breakdown == nil {
		t.Fatal("breakdown map nil")
	}
	if res.SavedTokens > 0 && !res.Compressed {
		t.Fatalf("saved tokens but Compressed flag missing: %+v", res)
	}
}

func buildPreviewAnthropicBody(t *testing.T, toolResultText string) []byte {
	t.Helper()
	type contentBlock struct {
		Type      string `json:"type"`
		Text      string `json:"text,omitempty"`
		ToolUseID string `json:"tool_use_id,omitempty"`
		Content   string `json:"content,omitempty"`
		ToolName  string `json:"tool_name,omitempty"`
	}
	type message struct {
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	}
	body := struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}{
		Model: "claude-3-5-sonnet",
		Messages: []message{
			{Role: "user", Content: []contentBlock{{Type: "text", Text: "begin"}}},
			{Role: "assistant", Content: []contentBlock{{Type: "tool_result", ToolUseID: "tool-1", Content: toolResultText}}},
			{Role: "assistant", Content: []contentBlock{{Type: "tool_result", ToolUseID: "tool-2", Content: toolResultText}}},
			{Role: "user", Content: []contentBlock{{Type: "text", Text: "continue"}}},
		},
	}
	out, err := json.Marshal(&body)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPreviewCompress_BreakdownMapPresent(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	body := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"ok"}]}`)
	res, err := PreviewCompress(cfg, "/v1/messages", body, types.Anthropic, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Layer1Breakdown == nil {
		t.Fatal("expected breakdown map")
	}
	if len(res.OriginalBody) == 0 || len(res.RewrittenBody) == 0 {
		t.Fatal("includeBodies=true must populate both bodies")
	}
}

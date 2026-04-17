package summarization

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestLayer2_ShouldTriggerCompression_respectsMinTokensForLayer2(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1000

	l := NewLayer2(&cfg)
	msgs := []types.Message{
		msg(t, 0, "user", "short"),
		msg(t, 1, "assistant", "tail"),
	}
	if l.ShouldTriggerCompression(msgs) {
		t.Fatal("compression should not trigger below min_tokens_for_layer2")
	}
}

func TestLayer2_RunCompressionJobContext_honorsCancellation(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"- summary"}}]}`)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	cfg.SlidingWindow = 2
	cfg.MinMessagesForCompression = 4
	cfg.MinTokensForLayer2 = 1

	l := NewLayer2(&cfg)
	msgs := make([]types.Message, 12)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = msg(t, i, role, strings.Repeat("word ", 20))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l.RunCompressionJobContext(ctx, msgs)

	if cur, _ := l.cache.GetCurrent(); cur != nil {
		t.Fatalf("expected no cached summary on cancelled context, got %#v", cur)
	}
	if calls.Load() != 0 {
		t.Fatalf("expected no HTTP calls after cancellation, got %d", calls.Load())
	}
}

func TestLayer2_FormatMessagesForSummarization_fencesStrictCodeBlocks(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults().Compression
	cfg.Summary.Strict = true
	l := NewLayer2(&cfg)

	msgs := []types.Message{
		toolResultMsg(t, 0, "func handleLogin() error {\n\treturn nil\n}"),
	}
	out := l.FormatMessagesForSummarization(msgs)
	if !strings.Contains(out, "```text") {
		t.Fatalf("expected fenced code-like content in strict mode, got %q", out)
	}
}

func TestValidator_MissingFunctionNamesOutsideCodeFences(t *testing.T) {
	t.Parallel()

	v := NewCompressionValidator()
	original := "The error is in func handleLogin() error { return nil } inside internal/proxy/handler.go."
	msgs := buildValidatorMessages(t, original)
	origTokens := countTokens(strings.Repeat(original+" ", 20))
	summary := "- internal/proxy/handler.go needs a fix.\n- " + strings.Repeat("generic summary text ", 20)

	result := v.Validate(msgs, summary, origTokens)
	if result.Valid {
		t.Fatal("expected validator to fail when plain-text function names are omitted")
	}
	if !strings.Contains(result.FailReason, "function name preservation") {
		t.Fatalf("unexpected fail reason: %s", result.FailReason)
	}
}

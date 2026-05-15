package summarization

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

// TestLayer2_RunCompressionJob_storesSummary runs the full async job path against a fake MiniMax API.
func TestLayer2_RunCompressionJob_storesSummary(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	summaryText := "- alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau upsilon phi chi psi omega"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, summaryText)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	cfg.SlidingWindow = 5
	cfg.MinMessagesForCompression = 8
	cfg.MinTokensForLayer2 = 1

	l := NewLayer2(&cfg)

	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = msg(t, i, role, strings.Repeat("word ", 25))
	}

	l.RunCompressionJob(msgs)

	cached, _ := l.cache.GetCurrent()
	if cached == nil || cached.Summary != summaryText {
		t.Fatalf("expected stored summary, got %#v", cached)
	}
	if cached.CoveredRange[1] < 7 {
		t.Fatalf("unexpected covered range: %v", cached.CoveredRange)
	}
}

func TestLayer2_RunCompressionJob_summarizeHTTPError(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`fail`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	cfg.SlidingWindow = 5
	cfg.MinMessagesForCompression = 8
	cfg.MinTokensForLayer2 = 1

	l := NewLayer2(&cfg)
	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = msg(t, i, role, strings.Repeat("word ", 25))
	}

	l.RunCompressionJob(msgs)

	if cur, _ := l.cache.GetCurrent(); cur != nil {
		t.Fatalf("expected no cache on HTTP error, got %#v", cur)
	}
}

// TestLayer2_RunCompressionJob_emptyToSummarize covers the "len(toSummarize) == 0" early
// return (layer2.go:125-127) by making all messages within the compressible prefix anchors.
// Every message has an edit tool_use block, so AnchorDetector marks them all as anchors,
// filterNonAnchored returns an empty slice, and RunCompressionJob returns immediately.
func TestLayer2_RunCompressionJob_emptyToSummarize(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.SlidingWindow = 5
	cfg.MinMessagesForCompression = 8
	cfg.MinTokensForLayer2 = 1

	l := NewLayer2(&cfg)

	// 20 messages alternating user/assistant, each with an edit tool_use block.
	// CompressiblePrefixEnd with SlidingWindow=5 and 10 user messages gives
	// prefixEnd=10 >= minMsgs=8, so the prefix check passes.
	// All 10 messages in the compressible prefix are anchors (via isAnchorEdit),
	// so filterNonAnchored returns nil and RunCompressionJob hits the empty-toSummarize return.
	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Index: i,
			Role:  role,
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolName: "Write"},
			},
		}
	}

	// Must not block, panic, or call the summarizer.
	l.RunCompressionJob(msgs)

	// No summary should be stored since we returned early.
	if cur, _ := l.cache.GetCurrent(); cur != nil {
		t.Fatalf("expected no cached summary for all-anchor messages, got %#v", cur)
	}
}

func TestLayer2_RunCompressionJob_inputTokenCap(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		bullets := "- processed large input with truncation applied\n- file src/main.go was read and analyzed\n- tests passed with 15 runs and zero failures\n- decision to refactor handler approved by user"
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, bullets)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	cfg.SlidingWindow = 5
	cfg.MinMessagesForCompression = 8
	cfg.MinTokensForLayer2 = 1

	l := NewLayer2(&cfg)

	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		text := strings.Repeat("abcde ", 10000)
		if i == 0 {
			text += "\nsrc/main.go was read before truncation\n"
		}
		msgs[i] = msg(t, i, role, text)
	}

	l.RunCompressionJob(msgs)

	cached, _ := l.cache.GetCurrent()
	if cached == nil {
		t.Fatal("expected cached summary after input token cap truncation")
	}
	if requestCount < 1 {
		t.Fatalf("expected at least 1 API request, got %d", requestCount)
	}
}

func TestLayer2_RunCompressionJob_capturesAnchorMessages(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	summaryText := "- alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau upsilon phi chi psi omega"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, summaryText)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	cfg.SlidingWindow = 5
	cfg.MinMessagesForCompression = 8
	cfg.MinTokensForLayer2 = 1

	l := NewLayer2(&cfg)

	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = msg(t, i, role, strings.Repeat("word ", 25))
	}
	msgs[3] = toolUseMsg(t, 3, "edit_file")
	msgs[7] = msg(t, 7, "assistant", "error: something failed")

	l.RunCompressionJob(msgs)

	cached, _ := l.cache.GetCurrent()
	if cached == nil {
		t.Fatal("expected cached summary")
	}
	if len(cached.AnchorMessages) == 0 {
		t.Fatal("expected anchor messages to be captured")
	}
	if len(cached.AnchorsInlined) != len(cached.AnchorMessages) {
		t.Fatalf("AnchorsInlined=%d != AnchorMessages=%d", len(cached.AnchorsInlined), len(cached.AnchorMessages))
	}
}

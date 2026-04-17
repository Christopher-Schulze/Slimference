package summarization

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
	"golang.org/x/time/rate"
)

type cancelSummarizer struct {
	name       string
	configured bool
	summarize  func(context.Context) (string, error)
	callCount  atomic.Int32
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type stagedErrContext struct {
	context.Context
	errAfter int32
	calls    atomic.Int32
}

func (c *stagedErrContext) Done() <-chan struct{} { return nil }

func (c *stagedErrContext) Err() error {
	if c.calls.Add(1) >= c.errAfter {
		return context.Canceled
	}
	return nil
}

type cancelAfterReadBody struct {
	data   []byte
	cancel func()
	read   bool
}

func (b *cancelAfterReadBody) Read(p []byte) (int, error) {
	if b.read {
		return 0, io.EOF
	}
	b.read = true
	n := copy(p, b.data)
	if b.cancel != nil {
		b.cancel()
	}
	return n, io.EOF
}

func (b *cancelAfterReadBody) Close() error { return nil }

type cancelingErrorBody struct {
	cancel func()
}

func (b *cancelingErrorBody) Read([]byte) (int, error) {
	if b.cancel != nil {
		b.cancel()
	}
	return 0, errors.New("forced body read failure")
}

func (b *cancelingErrorBody) Close() error { return nil }

func (s *cancelSummarizer) Name() string { return s.name }

func (s *cancelSummarizer) IsConfigured() bool { return s.configured }

func (s *cancelSummarizer) Summarize(ctx context.Context, _ string, _, _, _ int) (string, error) {
	s.callCount.Add(1)
	return s.summarize(ctx)
}

func TestMiniMaxClient_doRequest_honorsCanceledContext(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"

	client := NewMiniMaxClient(cfg.MiniMax)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.doRequest(ctx, mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("expected no upstream call after cancellation, got %d", calls.Load())
	}
}

func TestMiniMaxClient_Summarize_backoffHonorsCancellation(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 3

	client := NewMiniMaxClient(cfg.MiniMax)
	client.limiter = rate.NewLimiter(rate.Inf, 1)

	origWait := backoffWaitFn
	defer func() {
		backoffWaitFn = origWait
	}()

	ctx, cancel := context.WithCancel(context.Background())
	backoffWaitFn = func(ctx context.Context, _ time.Duration) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}

	_, err := client.Summarize(ctx, "input", 0, 1, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation during backoff, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected single upstream attempt before cancellation, got %d", calls.Load())
	}
}

func TestBackoffWaitFn_returnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := backoffWaitFn(ctx, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled backoff wait, got %v", err)
	}
}

func TestMiniMaxClient_Summarize_stopsAfterRetryableResponseCancelsContext(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		cancel()
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 2

	client := NewMiniMaxClient(cfg.MiniMax)
	client.limiter = rate.NewLimiter(rate.Inf, 1)

	_, err := client.Summarize(ctx, "input", 0, 1, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled summarize result, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected single request before cancellation, got %d", calls.Load())
	}
}

func TestFallbackChain_stopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	primary := &cancelSummarizer{
		name:       "primary",
		configured: true,
		summarize: func(ctx context.Context) (string, error) {
			cancel()
			return "", ctx.Err()
		},
	}
	secondary := &cancelSummarizer{
		name:       "secondary",
		configured: true,
		summarize: func(context.Context) (string, error) {
			return "- should not run", nil
		},
	}

	chain := NewFallbackChain(primary, secondary)
	_, _, err := chain.Summarize(ctx, "input", 0, 1, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled error, got %v", err)
	}
	if secondary.callCount.Load() != 0 {
		t.Fatalf("expected no fallback call after cancellation, got %d", secondary.callCount.Load())
	}
}

func TestFallbackChain_canceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	chain := NewFallbackChain(&cancelSummarizer{
		name:       "primary",
		configured: true,
		summarize: func(context.Context) (string, error) {
			return "- should not run", nil
		},
	})

	_, _, err := chain.Summarize(ctx, "input", 0, 1, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled-before-start error, got %v", err)
	}
}

func TestFallbackChain_canceledBeforeSecondProvider(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	primary := &cancelSummarizer{
		name:       "primary",
		configured: true,
		summarize: func(context.Context) (string, error) {
			cancel()
			return "", errors.New("first failure")
		},
	}
	secondary := &cancelSummarizer{
		name:       "secondary",
		configured: true,
		summarize: func(context.Context) (string, error) {
			return "- should not run", nil
		},
	}

	chain := NewFallbackChain(primary, secondary)
	_, _, err := chain.Summarize(ctx, "input", 0, 1, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled-before-second-provider error, got %v", err)
	}
	if secondary.callCount.Load() != 0 {
		t.Fatalf("expected no secondary provider call, got %d", secondary.callCount.Load())
	}
}

func TestFallbackChain_canceledAtSecondProviderGuard(t *testing.T) {
	ctx := &stagedErrContext{Context: context.Background(), errAfter: 3}
	secondary := &cancelSummarizer{
		name:       "secondary",
		configured: true,
		summarize: func(context.Context) (string, error) {
			return "- should not run", nil
		},
	}

	chain := NewFallbackChain(nil, secondary)
	_, _, err := chain.Summarize(ctx, "input", 0, 1, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled error at second-provider guard, got %v", err)
	}
	if secondary.callCount.Load() != 0 {
		t.Fatalf("expected no secondary provider call, got %d", secondary.callCount.Load())
	}
}

func TestLayer2_RunCompressionJobContext_skipsCacheWriteAfterCancel(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1

	layer := NewLayer2(&cfg)
	ctx, cancel := context.WithCancel(context.Background())
	layer.chain.SetProviders(&cancelSummarizer{
		name:       "cancel-after-summary",
		configured: true,
		summarize: func(context.Context) (string, error) {
			cancel()
			return "- src/main.go contains HandleMain\n- src/lib/util.go contains helper", nil
		},
	})

	layer.RunCompressionJobContext(ctx, coverageMessages())

	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatalf("expected no cache write after cancellation, got %#v", cached)
	}
}

func TestLayer2_RunCompressionJobContext_canceledAfterPreprocess(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1

	layer := NewLayer2(&cfg)
	hugeText := bytes.Repeat([]byte("line with context payload\n"), 500000)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: string(hugeText)}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ack"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(1 * time.Millisecond)
		cancel()
	}()

	layer.RunCompressionJobContext(ctx, msgs)

	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatalf("expected preprocess-time cancellation to skip caching, got %#v", cached)
	}
}

func TestLayer2_RunCompressionJobContext_canceledAfterSummarizeError(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1

	layer := NewLayer2(&cfg)
	ctx, cancel := context.WithCancel(context.Background())
	layer.chain.SetProviders(&cancelSummarizer{
		name:       "cancel-error",
		configured: true,
		summarize: func(context.Context) (string, error) {
			cancel()
			return "", errors.New("boom")
		},
	})

	layer.RunCompressionJobContext(ctx, coverageMessages())
	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatalf("expected no cache write after summarize error cancellation, got %#v", cached)
	}
}

func TestLayer2_RunCompressionJobContext_canceledDuringRetrySuccess(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
	cfg.Summary.Strict = true

	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "src/main.go\nfunc HandleMain() {}\n" + strings.Repeat("context filler ", 20)}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ack"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "src/lib/util.go\nfunc helper() {}\n" + strings.Repeat("more filler ", 20)}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
	}

	layer := NewLayer2(&cfg)
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	layer.chain.SetProviders(&cancelSummarizer{
		name:       "retry-cancel",
		configured: true,
		summarize: func(context.Context) (string, error) {
			if calls.Add(1) == 1 {
				return "not bullets", nil
			}
			cancel()
			return "- src/main.go contains HandleMain\n- src/lib/util.go contains helper", nil
		},
	})

	layer.RunCompressionJobContext(ctx, msgs)
	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatalf("expected retry-path cancellation to skip caching, got %#v", cached)
	}
}

func TestLayer2_RunCompressionJobContext_canceledAfterValidSummary(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1

	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "src/main.go\nfunc HandleMain() {}\n" + strings.Repeat("context filler ", 20)}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ack"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "src/lib/util.go\nfunc helper() {}\n" + strings.Repeat("more filler ", 20)}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
	}

	layer := NewLayer2(&cfg)
	ctx, cancel := context.WithCancel(context.Background())
	layer.chain.SetProviders(&cancelSummarizer{
		name:       "cancel-valid",
		configured: true,
		summarize: func(context.Context) (string, error) {
			cancel()
			return "- src/main.go contains HandleMain\n- src/lib/util.go contains helper", nil
		},
	})

	layer.RunCompressionJobContext(ctx, msgs)
	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatalf("expected final cancellation gate to skip caching, got %#v", cached)
	}
}

func TestApplyProgressiveTiersWithContext_canceledContextKeepsTailVerbatim(t *testing.T) {
	cfg := config.Defaults().Compression
	layer := NewLayer2(&cfg)

	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "one"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "two"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "three"}}},
	}
	tiers := []CompressionTier{
		{Name: "tier-1", MsgRange: [2]int{0, 1}, TargetRatio: 0.2},
		{Name: "window", MsgRange: [2]int{2, 2}, TargetRatio: 1.0},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := layer.applyProgressiveTiersWithContext(ctx, msgs, tiers)
	if len(out) != len(msgs) {
		t.Fatalf("expected verbatim tail on cancellation, got %d messages", len(out))
	}
	for i, msg := range out {
		if msg.Index != i {
			t.Fatalf("message %d index = %d, want %d", i, msg.Index, i)
		}
		if got := msg.Content[0].Text; got != msgs[i].Content[0].Text {
			t.Fatalf("message %d text = %q, want %q", i, got, msgs[i].Content[0].Text)
		}
	}
}

func TestLayer2_WithJobTimeout_preservesParentDeadline(t *testing.T) {
	cfg := config.Defaults().Compression
	layer := NewLayer2(&cfg)

	parent, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	ctx, release := layer.withJobTimeout(parent)
	defer release()

	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", ctx.Err())
	}
}

func TestLayer2_WithJobTimeout_nilParentUsesBackground(t *testing.T) {
	cfg := config.Defaults().Compression
	layer := NewLayer2(&cfg)

	ctx, cancel := layer.withJobTimeout(nil)
	defer cancel()

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
}

func TestMiniMaxClient_Summarize_contextCancellationBubblesUp(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0

	client := NewMiniMaxClient(cfg.MiniMax)
	client.limiter = rate.NewLimiter(rate.Inf, 1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Summarize(ctx, "input", 0, 1, 100)
		done <- err
	}()

	cancel()

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestMiniMaxClient_doRequest_nilContextUsesBackground(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"

	client := NewMiniMaxClient(cfg.MiniMax)
	got, err := client.doRequest(nil, mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected nil-context error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("response = %q, want ok", got)
	}
}

func TestMiniMaxClient_doRequest_returnsCanceledWhenTransportFailsAfterCancel(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	cfg := config.Defaults().Compression
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"

	ctx, cancel := context.WithCancel(context.Background())
	client := NewMiniMaxClient(cfg.MiniMax)
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			cancel()
			return nil, req.Context().Err()
		}),
	}

	_, err := client.doRequest(ctx, mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled transport error, got %v", err)
	}
}

func TestMiniMaxClient_doRequest_returnsCanceledWhenBodyReadFailsAfterCancel(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	cfg := config.Defaults().Compression
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"

	ctx, cancel := context.WithCancel(context.Background())
	client := NewMiniMaxClient(cfg.MiniMax)
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       &cancelingErrorBody{cancel: cancel},
			}, nil
		}),
	}

	_, err := client.doRequest(ctx, mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled body-read error, got %v", err)
	}
}

func TestMiniMaxClient_doRequest_returnsCanceledAfterSuccessfulRead(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")

	cfg := config.Defaults().Compression
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"

	ctx, cancel := context.WithCancel(context.Background())
	client := NewMiniMaxClient(cfg.MiniMax)
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: &cancelAfterReadBody{
					data:   []byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
					cancel: cancel,
				},
			}, nil
		}),
	}

	_, err := client.doRequest(ctx, mmRequest{
		Model:    cfg.MiniMax.Model,
		Messages: []mmMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled-after-read error, got %v", err)
	}
}

func TestApplyProgressiveTiersWithContext_canceledAfterSummarizeErrorKeepsTail(t *testing.T) {
	cfg := config.Defaults().Compression
	layer := NewLayer2(&cfg)
	ctx, cancel := context.WithCancel(context.Background())
	layer.chain.SetProviders(&cancelSummarizer{
		name:       "cancel-progressive-error",
		configured: true,
		summarize: func(context.Context) (string, error) {
			cancel()
			return "", errors.New("boom")
		},
	})

	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "one"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "two"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "three"}}},
	}
	tiers := []CompressionTier{{Name: "tier-1", MsgRange: [2]int{0, 1}, TargetRatio: 0.2}}

	out := layer.applyProgressiveTiersWithContext(ctx, msgs, tiers)
	if len(out) != len(msgs) {
		t.Fatalf("expected full verbatim tail after cancellation, got %d", len(out))
	}
}

func TestApplyProgressiveTiersWithContext_canceledAfterValidationKeepsTail(t *testing.T) {
	cfg := config.Defaults().Compression
	layer := NewLayer2(&cfg)
	ctx, cancel := context.WithCancel(context.Background())
	layer.chain.SetProviders(&cancelSummarizer{
		name:       "cancel-progressive-valid",
		configured: true,
		summarize: func(context.Context) (string, error) {
			cancel()
			return "- some summary", nil
		},
	})

	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("word ", 200)}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "two"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "three"}}},
	}
	tiers := []CompressionTier{{Name: "tier-1", MsgRange: [2]int{0, 1}, TargetRatio: 0.2}}

	out := layer.applyProgressiveTiersWithContext(ctx, msgs, tiers)
	if len(out) != len(msgs) {
		t.Fatalf("expected full verbatim tail after validation-time cancellation, got %d", len(out))
	}
}

func TestAppendVerbatimTail_bounds(t *testing.T) {
	msgs := []types.Message{{Index: 99, Role: "user"}, {Index: 100, Role: "assistant"}}

	got := appendVerbatimTail(nil, msgs, -1, 0)
	if len(got) != 2 || got[0].Index != 0 || got[1].Index != 1 {
		t.Fatalf("negative start append = %#v", got)
	}

	got = appendVerbatimTail(nil, msgs, len(msgs), 0)
	if len(got) != 0 {
		t.Fatalf("expected empty append when start>=len, got %#v", got)
	}
}

func Example_appendVerbatimTail() {
	out := appendVerbatimTail(nil, []types.Message{{Index: 99, Role: "user"}}, 0, 0)
	fmt.Println(len(out), out[0].Index)
	// Output: 1 0
}

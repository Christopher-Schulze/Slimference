package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

type proxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f proxyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type armedCancelContext struct {
	context.Context
	errAfterArm int32
	armed       atomic.Bool
	calls       atomic.Int32
}

func (c *armedCancelContext) Done() <-chan struct{} { return nil }

func (c *armedCancelContext) arm() {
	c.calls.Store(0)
	c.armed.Store(true)
}

func (c *armedCancelContext) Err() error {
	if !c.armed.Load() {
		return nil
	}
	if c.calls.Add(1) >= c.errAfterArm {
		return context.Canceled
	}
	return nil
}

func TestCompressionWorker_shutdownSkipsQueuedJobs(t *testing.T) {
	p := New(config.Defaults())
	p.compressQueue <- types.CompressJob{
		Messages: []types.Message{
			{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "queued"}}},
		},
		Timestamp: time.Now(),
	}

	close(p.shutdownCh)
	p.wg.Add(1)
	go p.compressionWorker()
	p.wg.Wait()

	if got := len(p.analyticsQueue); got != 0 {
		t.Fatalf("expected queued compression job to be skipped during shutdown, got %d analytics events", got)
	}
}

func TestProxy_compressionContext_nilWorkerFallsBackToBackground(t *testing.T) {
	p := &Proxy{}
	if p.compressionContext() == nil {
		t.Fatal("expected background fallback context")
	}
}

func TestDoUpstreamRequest_overflowCanceledBeforeAggressiveRetry(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	p.httpClients[types.Anthropic] = &http.Client{
		Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
			cancel()
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("context_length_exceeded")),
			}, nil
		}),
	}

	orig := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(context.WithValue(ctx, origBodyKey{}, orig))

	_, err := p.doUpstreamRequest(r, types.Anthropic, orig)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation before aggressive retry, got %v", err)
	}
}

func TestDoUpstreamRequest_overflowAggressiveBuildErrorReturnsCanceledContext(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	p.httpClients[types.Anthropic] = &http.Client{
		Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("context_length_exceeded")),
			}, nil
		}),
	}

	orig := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`)
	msgs, _, err := extractMessages(types.Anthropic, orig)
	if err != nil {
		t.Fatal(err)
	}
	stash := pipelineStash{messages: msgs, origBody: orig, provider: types.Anthropic}
	r0 := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	base := context.WithValue(context.Background(), pipelineStashKey{}, stash)
	base = context.WithValue(base, origBodyKey{}, orig)
	reqCtx := &armedCancelContext{Context: base, errAfterArm: 1}
	r := r0.WithContext(reqCtx)

	origReconstruct := reconstructBodyFn
	reconstructBodyFn = func(types.Provider, []byte, []types.Message) ([]byte, error) {
		reqCtx.arm()
		return nil, errors.New("rebuild failed")
	}
	defer func() {
		reconstructBodyFn = origReconstruct
	}()

	_, err = p.doUpstreamRequest(r, types.Anthropic, orig)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context after aggressive-build failure, got %v", err)
	}
}

func TestDoUpstreamRequest_overflowCanceledBeforeOriginalFallback(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)
	p.httpClients[types.Anthropic] = &http.Client{
		Transport: proxyRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("context_length_exceeded")),
			}, nil
		}),
	}

	orig := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`)
	msgs, _, err := extractMessages(types.Anthropic, orig)
	if err != nil {
		t.Fatal(err)
	}
	stash := pipelineStash{messages: msgs, origBody: orig, provider: types.Anthropic}
	base := context.WithValue(context.Background(), pipelineStashKey{}, stash)
	base = context.WithValue(base, origBodyKey{}, orig)
	ctx := &armedCancelContext{Context: base, errAfterArm: 2}
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)

	origReconstruct := reconstructBodyFn
	reconstructBodyFn = func(types.Provider, []byte, []types.Message) ([]byte, error) {
		ctx.arm()
		return nil, nil
	}
	defer func() {
		reconstructBodyFn = origReconstruct
	}()

	_, err = p.doUpstreamRequest(r, types.Anthropic, orig)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context before original fallback, got %v", err)
	}
}

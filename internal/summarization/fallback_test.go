package summarization

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
)

type stubSummarizer struct {
	name       string
	configured bool
	result     string
	err        error
	callCount  int
}

func (s *stubSummarizer) Name() string       { return s.name }
func (s *stubSummarizer) IsConfigured() bool { return s.configured }
func (s *stubSummarizer) Summarize(_ context.Context, inputText string, startMsg, endMsg, targetTokens int) (string, error) {
	s.callCount++
	return s.result, s.err
}

func TestFallbackChain_primarySucceeds(t *testing.T) {
	t.Parallel()
	primary := &stubSummarizer{name: "primary", configured: true, result: "- summary line 1\n- summary line 2"}
	fallback := &stubSummarizer{name: "fallback", configured: true, result: "- fallback result"}
	chain := NewFallbackChain(primary, fallback)

	summary, provider, err := chain.Summarize(context.Background(), "input text", 0, 10, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "primary" {
		t.Fatalf("expected primary, got %s", provider)
	}
	if !strings.Contains(summary, "summary line 1") {
		t.Fatalf("wrong summary: %s", summary)
	}
	if fallback.callCount != 0 {
		t.Fatal("fallback should not have been called")
	}
}

func TestFallbackChain_primaryFails_fallbackSucceeds(t *testing.T) {
	t.Parallel()
	primary := &stubSummarizer{name: "primary", configured: true, err: fmt.Errorf("rate limited")}
	fallback := &stubSummarizer{name: "fallback", configured: true, result: "- fallback line 1\n- fallback line 2"}
	chain := NewFallbackChain(primary, fallback)

	summary, provider, err := chain.Summarize(context.Background(), "input text", 0, 10, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "fallback" {
		t.Fatalf("expected fallback, got %s", provider)
	}
	if !strings.Contains(summary, "fallback line 1") {
		t.Fatalf("wrong summary: %s", summary)
	}
	if primary.callCount != 1 {
		t.Fatal("primary should have been called once")
	}
}

func TestFallbackChain_primaryUnconfigured_fallbackUsed(t *testing.T) {
	t.Parallel()
	primary := &stubSummarizer{name: "primary", configured: false}
	fallback := &stubSummarizer{name: "fallback", configured: true, result: "- ok\n- result"}
	chain := NewFallbackChain(primary, fallback)

	summary, provider, err := chain.Summarize(context.Background(), "input text", 0, 10, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "fallback" {
		t.Fatalf("expected fallback, got %s", provider)
	}
	if primary.callCount != 0 {
		t.Fatal("unconfigured primary should not be called")
	}
	_ = summary
}

func TestFallbackChain_allFail(t *testing.T) {
	t.Parallel()
	primary := &stubSummarizer{name: "primary", configured: true, err: fmt.Errorf("timeout")}
	fallback := &stubSummarizer{name: "fallback", configured: true, err: fmt.Errorf("auth error")}
	chain := NewFallbackChain(primary, fallback)

	_, _, err := chain.Summarize(context.Background(), "input text", 0, 10, 100)
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !strings.Contains(err.Error(), "all 2") {
		t.Fatalf("error should mention all providers failed: %v", err)
	}
}

func TestFallbackChain_noProviders(t *testing.T) {
	t.Parallel()
	chain := NewFallbackChain()
	_, _, err := chain.Summarize(context.Background(), "input text", 0, 10, 100)
	if err == nil {
		t.Fatal("expected error with no providers")
	}
	if !strings.Contains(err.Error(), "no summarization providers") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestFallbackChain_nilProvidersSkipped(t *testing.T) {
	t.Parallel()
	primary := &stubSummarizer{name: "good", configured: true, result: "- result"}
	chain := NewFallbackChain(nil, primary, nil)

	summary, provider, err := chain.Summarize(context.Background(), "input text", 0, 10, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "good" {
		t.Fatalf("expected good, got %s", provider)
	}
	if !strings.Contains(summary, "result") {
		t.Fatalf("wrong summary: %s", summary)
	}
}

func TestFallbackChain_allUnconfigured(t *testing.T) {
	t.Parallel()
	p1 := &stubSummarizer{name: "p1", configured: false}
	p2 := &stubSummarizer{name: "p2", configured: false}
	chain := NewFallbackChain(p1, p2)

	_, _, err := chain.Summarize(context.Background(), "input text", 0, 10, 100)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no configured") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestFallbackChain_ActiveProviderName(t *testing.T) {
	t.Parallel()
	p1 := &stubSummarizer{name: "p1", configured: false}
	p2 := &stubSummarizer{name: "p2", configured: true}
	chain := NewFallbackChain(p1, p2)

	if chain.ActiveProviderName() != "p2" {
		t.Fatalf("expected p2, got %s", chain.ActiveProviderName())
	}
}

func TestFallbackChain_ActiveProviderName_none(t *testing.T) {
	t.Parallel()
	p1 := &stubSummarizer{name: "p1", configured: false}
	chain := NewFallbackChain(p1)

	if chain.ActiveProviderName() != "" {
		t.Fatalf("expected empty, got %s", chain.ActiveProviderName())
	}
}

func TestFallbackChain_SetProviders(t *testing.T) {
	t.Parallel()
	chain := NewFallbackChain()
	p := &stubSummarizer{name: "dynamic", configured: true, result: "- dynamic result"}
	chain.SetProviders(p)

	summary, provider, err := chain.Summarize(context.Background(), "input text", 0, 10, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "dynamic" {
		t.Fatalf("expected dynamic, got %s", provider)
	}
	if !strings.Contains(summary, "dynamic") {
		t.Fatalf("wrong summary: %s", summary)
	}
}

func TestFallbackChain_Providers(t *testing.T) {
	t.Parallel()
	p1 := &stubSummarizer{name: "p1", configured: true}
	p2 := &stubSummarizer{name: "p2", configured: true}
	chain := NewFallbackChain(p1, p2)

	providers := chain.Providers()
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}
}

func TestLayer2_AddFallbackProvider(t *testing.T) {
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)

	fb := &stubSummarizer{name: "fallback-1", configured: true, result: "- fb result"}
	l.AddFallbackProvider(fb)

	providers := l.chain.Providers()
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers (primary + fallback), got %d", len(providers))
	}
	if providers[1].Name() != "fallback-1" {
		t.Fatalf("second provider should be fallback-1, got %s", providers[1].Name())
	}
}

func TestMiniMaxClient_implementsSummarizer(t *testing.T) {
	cfg := config.Defaults().Compression
	var _ Summarizer = NewMiniMaxClient(cfg.MiniMax)
}

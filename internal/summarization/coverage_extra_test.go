package summarization

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

type coverageSummarizer struct {
	name        string
	configured  bool
	summaries   []string
	errs        []error
	callCount   int
	lastContext context.Context
	lastInput   string
	lastTarget  int
	onSummarize func()
}

func (s *coverageSummarizer) Name() string { return s.name }

func (s *coverageSummarizer) IsConfigured() bool { return s.configured }

func (s *coverageSummarizer) Summarize(ctx context.Context, input string, _, _, target int) (string, error) {
	s.lastContext = ctx
	s.lastInput = input
	s.lastTarget = target
	index := s.callCount
	s.callCount++
	if index < len(s.errs) && s.errs[index] != nil {
		return "", s.errs[index]
	}
	if s.onSummarize != nil {
		s.onSummarize()
	}
	if index < len(s.summaries) {
		return s.summaries[index], nil
	}
	return "", nil
}

func coverageMessages() []types.Message {
	longContext := strings.Repeat("context filler ", 20)
	return []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "src/main.go\nfunc HandleMain() {}\nerror: boom\n" + longContext},
			},
		},
		{
			Index: 1,
			Role:  "assistant",
			Content: []types.ContentBlock{
				{Type: "text", Text: "ack"},
			},
		},
		{
			Index: 2,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "continue src/lib/util.go\nfn helper() {}\npanic: bad\n" + longContext},
			},
		},
		{
			Index: 3,
			Role:  "assistant",
			Content: []types.ContentBlock{
				{Type: "text", Text: "done"},
			},
		},
	}
}

func TestLayer2_RunCompressionJobContext_RetrysValidationFailure(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
	cfg.Summary.Strict = true

	layer := NewLayer2(&cfg)
	retryMessages := []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "src/main.go\nfunc HandleMain() {}\n" + strings.Repeat("context filler ", 20)},
			},
		},
		{
			Index: 1,
			Role:  "assistant",
			Content: []types.ContentBlock{
				{Type: "text", Text: "ack"},
			},
		},
		{
			Index: 2,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: "src/lib/util.go\nfn helper() {}\n" + strings.Repeat("more filler ", 20)},
			},
		},
		{
			Index: 3,
			Role:  "assistant",
			Content: []types.ContentBlock{
				{Type: "text", Text: "done"},
			},
		},
	}
	fake := &coverageSummarizer{
		name:       "fake",
		configured: true,
		summaries: []string{
			"not bullets",
			"- src/main.go contains HandleMain and enough context to satisfy retry validation",
		},
	}
	layer.chain.SetProviders(fake)

	layer.RunCompressionJobContext(context.Background(), retryMessages)

	cached, _ := layer.cache.GetCurrent()
	if cached == nil {
		t.Fatal("expected cached summary after retry success")
	}
	if fake.callCount != 2 {
		t.Fatalf("expected retry, got %d calls", fake.callCount)
	}
}

func TestLayer2_RunCompressionJobContext_CanceledBeforeWork(t *testing.T) {
	cfg := config.Defaults().Compression
	layer := NewLayer2(&cfg)
	layer.chain.SetProviders(&coverageSummarizer{name: "fake", configured: true, summaries: []string{"- never used"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	layer.runCompressionJob(ctx, legacySessionID, coverageMessages())
	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatalf("canceled job must not cache summary: %#v", cached)
	}
}

func TestLayer2_RunCompressionJobContext_CanceledBeforeStore(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinTokensForLayer2 = 1
	layer := NewLayer2(&cfg)
	ctx, cancel := context.WithCancel(context.Background())
	layer.chain.SetProviders(&coverageSummarizer{
		name:        "fake",
		configured:  true,
		summaries:   []string{"- src/main.go HandleMain error: boom\n- src/lib/util.go helper panic: bad"},
		onSummarize: cancel,
	})
	layer.runCompressionJob(ctx, legacySessionID, coverageMessages())
	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatalf("canceled job must not store accepted summary: %#v", cached)
	}
}

func TestEstimateTokens_CJKAndWhitespace(t *testing.T) {
	if got := estimateTokens("   "); got != 1 {
		t.Fatalf("whitespace-only token estimate = %d, want 1", got)
	}
	if got := estimateTokens("漢字かなカナ"); got != 6 {
		t.Fatalf("CJK token estimate = %d, want 6", got)
	}
}

func TestFormatBlockForSummarization_StrictBranches(t *testing.T) {
	if got := formatBlockForSummarization("", true); got != "" {
		t.Fatalf("empty block changed: %q", got)
	}
	if got := formatBlockForSummarization("plain prose", false); got != "plain prose" {
		t.Fatalf("non-strict block changed: %q", got)
	}
	fenced := formatBlockForSummarization("src/main.go\nfunc x() {}", true)
	if !strings.Contains(fenced, "```text") {
		t.Fatalf("expected fenced block, got %q", fenced)
	}
}

func TestShouldFenceSummarizationBlock_MultilinePath(t *testing.T) {
	if !shouldFenceSummarizationBlock("line\nsrc/main.go") {
		t.Fatal("expected multiline path block to be fenced")
	}
}

func TestLayer2_JobTimeout_DefaultFallback(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MiniMax.ConnectTimeoutSeconds = -10
	cfg.MiniMax.ResponseTimeoutSeconds = -10
	layer := NewLayer2(&cfg)
	if got := layer.jobTimeout(); got != 35*time.Second {
		t.Fatalf("jobTimeout = %v, want 35s", got)
	}
}

func TestValidator_RejectsThinkArtifactsWithBullets(t *testing.T) {
	v := NewCompressionValidator()
	result := v.Validate(coverageMessages(), "- valid bullet\n<think>nope</think>", 100)
	if result.Valid || !strings.Contains(result.FailReason, "chain-of-thought") {
		t.Fatalf("expected think-artifact rejection, got %+v", result)
	}
}

func TestJoinMessages_CoversAllBlockTypes(t *testing.T) {
	text := joinMessages([]types.Message{
		{
			Content: []types.ContentBlock{
				{Type: "text", Text: "plain"},
				{Type: "tool_result", Text: "tool output"},
				{Type: "tool_use", ToolName: "bash", ToolInput: "{\"command\":\"go test\"}"},
				{Type: "image", Text: "ignored"},
			},
		},
	})
	if !strings.Contains(text, "plain") || !strings.Contains(text, "tool output") || !strings.Contains(text, "bash") {
		t.Fatalf("joinMessages missing expected fields: %q", text)
	}
	if strings.Contains(text, "ignored") {
		t.Fatalf("joinMessages should ignore unsupported block types: %q", text)
	}
}

func TestMiniMaxClient_Summarize_RetryableExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer server.Close()

	t.Setenv("MINIMAX_API_KEY", "test-key")
	client := NewMiniMaxClient(config.MiniMaxConfig{
		BaseURL:                server.URL,
		APIKeyEnv:              "MINIMAX_API_KEY",
		Model:                  "m",
		MaxRetries:             1,
		RateLimitRPM:           600,
		ConnectTimeoutSeconds:  1,
		ResponseTimeoutSeconds: 1,
	})

	_, err := client.Summarize(context.Background(), "input", 0, 1, 100)
	if err == nil || !strings.Contains(err.Error(), "failed after 2 attempts") {
		t.Fatalf("expected retry exhaustion error, got %v", err)
	}
}

func TestLayer2_RunCompressionJobContext_MinTokensGate(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 10_000

	layer := NewLayer2(&cfg)
	layer.RunCompressionJobContext(context.Background(), coverageMessages())

	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatal("expected no cache entry when min-tokens gate blocks compression")
	}
}

func TestLayer2_RunCompressionJobContext_TruncatesHugeInputAndClampsTarget(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
	cfg.Summary.TargetRatio = 0.01

	layer := NewLayer2(&cfg)
	fake := &coverageSummarizer{
		name:       "fake",
		configured: true,
		summaries:  []string{"- src/main.go\n- func HandleMain()"},
	}
	layer.chain.SetProviders(fake)

	droppedPrefix := strings.Repeat("dropword ", 70000)
	huge := droppedPrefix + "\nsrc/main.go\nfunc HandleMain() {}\n"
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: huge}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ack"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail src/main.go"}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
	}

	layer.RunCompressionJobContext(context.Background(), msgs)

	if fake.callCount == 0 {
		t.Fatal("expected summarize call for huge input")
	}
	if strings.Contains(fake.lastInput, strings.Repeat("dropword ", 1000)) {
		t.Fatal("expected huge prefix to be truncated before summarization")
	}
}

func TestLayer2_RunCompressionJobContext_RecapsHighTokenDensityAfterByteCap(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
	cfg.Summary.TargetRatio = 0.01

	layer := NewLayer2(&cfg)
	fake := &coverageSummarizer{
		name:       "fake",
		configured: true,
		summaries:  []string{"- src/main.go\n- func HandleMain()"},
	}
	layer.chain.SetProviders(fake)

	cjkLines := strings.Repeat("漢字a\n漢字b\n", 50000)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: cjkLines + "src/main.go\nfunc HandleMain() {}\n"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ack"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail src/main.go"}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
	}

	layer.RunCompressionJobContext(context.Background(), msgs)

	if fake.callCount == 0 {
		t.Fatal("expected summarize call for high-token-density input")
	}
	if fake.lastTarget <= 0 {
		t.Fatalf("expected target tokens to be computed, got %d", fake.lastTarget)
	}
}

func TestLayer2_RunCompressionJobContext_TargetClampToMinimum(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
	cfg.Summary.TargetRatio = 0.01

	layer := NewLayer2(&cfg)
	fake := &coverageSummarizer{
		name:       "fake",
		configured: true,
		summaries:  []string{"- src/main.go"},
	}
	layer.chain.SetProviders(fake)

	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "src/main.go\nfunc HandleMain() {}\n"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ack"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "src/lib/util.go\nfunc helper() {}\n"}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "done"}}},
	}

	layer.RunCompressionJobContext(context.Background(), msgs)

	if fake.lastTarget != 100 {
		t.Fatalf("expected target clamp to 100, got %d", fake.lastTarget)
	}
}

func TestLayer2_RunCompressionJobContext_RetryRequestFailure(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
	cfg.Summary.Strict = true

	layer := NewLayer2(&cfg)
	fake := &coverageSummarizer{
		name:       "fake",
		configured: true,
		summaries:  []string{"not bullets"},
		errs:       []error{nil, errors.New("retry boom")},
	}
	layer.chain.SetProviders(fake)

	layer.RunCompressionJobContext(context.Background(), coverageMessages())

	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatal("expected retry request failure to leave cache empty")
	}
}

func TestLayer2_RunCompressionJobContext_RetryStillInvalid(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 1
	cfg.Summary.Strict = true

	layer := NewLayer2(&cfg)
	fake := &coverageSummarizer{
		name:       "fake",
		configured: true,
		summaries:  []string{"not bullets", "still not bullets"},
	}
	layer.chain.SetProviders(fake)

	layer.RunCompressionJobContext(context.Background(), coverageMessages())

	if fake.callCount != 2 {
		t.Fatalf("expected validation retry, got %d calls", fake.callCount)
	}
	if cached, _ := layer.cache.GetCurrent(); cached != nil {
		t.Fatal("expected invalid retry result to leave cache empty")
	}
}

func TestShouldTriggerCompression_MinTokensFalse(t *testing.T) {
	cfg := config.Defaults().Compression
	cfg.MinMessagesForCompression = 1
	cfg.SlidingWindow = 1
	cfg.MinTokensForLayer2 = 10_000

	layer := NewLayer2(&cfg)
	if layer.ShouldTriggerCompression(coverageMessages()) {
		t.Fatal("expected false when prefix token count is below min-tokens threshold")
	}
}

func TestContentDensityAndAdaptiveTargetEdgeBranches(t *testing.T) {
	if got := contentDensity([]types.Message{{Content: []types.ContentBlock{{Type: "tool_use"}}}}); got != 0.5 {
		t.Fatalf("totalChars=0 density = %v, want 0.5", got)
	}

	dense := []types.Message{
		{Content: []types.ContentBlock{{Type: "text", Text: "x ./a.go ../b.go src/c.go"}}},
	}
	if got := contentDensity(dense); got != 1.0 {
		t.Fatalf("dense content should clamp to 1.0, got %v", got)
	}
	if got := computeAdaptiveTarget(10_000, dense, 0.55); got != 6000 {
		t.Fatalf("expected ratio cap at 0.60, got %d", got)
	}

	if got := contentDensityText(""); got != 0.5 {
		t.Fatalf("empty text density = %v, want 0.5", got)
	}
	if got := contentDensityText("plain prose without newline"); got != 0 {
		t.Fatalf("plain single-line density = %v, want 0", got)
	}
	pathHeavy := strings.Repeat("src/a.go ", 20)
	if got := contentDensityText(pathHeavy); got != 1.0 {
		t.Fatalf("path-heavy text should clamp to 1.0, got %v", got)
	}
	if got := capSummarizationInput("abcdef", 0); got != "abcdef" {
		t.Fatalf("disabled cap changed input: %q", got)
	}
	if got := capSummarizationInput("short", 10); got != "short" {
		t.Fatalf("short input changed: %q", got)
	}
	if got := capSummarizationInput("abcdef", 1); got != "cdef" {
		t.Fatalf("no-newline cap = %q, want cdef", got)
	}
}

func TestLooksLikePathAndFenceNegativeBranches(t *testing.T) {
	if !looksLikePath("./x") {
		t.Fatal("expected ./ path detection")
	}
	if !looksLikePath("../x") {
		t.Fatal("expected ../ path detection")
	}
	if shouldFenceSummarizationBlock("hello\nworld") {
		t.Fatal("plain multiline prose should not be fenced")
	}
	if shouldFenceSummarizationBlock("hello\n\nworld") {
		t.Fatal("blank-line prose should not be fenced")
	}
}

func TestMiniMaxClient_Summarize_BackoffCapBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer server.Close()

	origWait := backoffWaitFn
	backoffWaitFn = func(context.Context, time.Duration) error { return nil }
	defer func() {
		backoffWaitFn = origWait
	}()

	t.Setenv("MINIMAX_API_KEY", "test-key")
	client := NewMiniMaxClient(config.MiniMaxConfig{
		BaseURL:                server.URL,
		APIKeyEnv:              "MINIMAX_API_KEY",
		Model:                  "m",
		MaxRetries:             6,
		RateLimitRPM:           600,
		ConnectTimeoutSeconds:  1,
		ResponseTimeoutSeconds: 1,
	})

	_, err := client.Summarize(context.Background(), "input", 0, 1, 100)
	if err == nil || !strings.Contains(err.Error(), "failed after 7 attempts") {
		t.Fatalf("expected retry exhaustion after capped backoff retries, got %v", err)
	}
}

func TestDeduplicateBullets_SubstringAndFuzzyBranches(t *testing.T) {
	input := strings.Join([]string{
		"- src/main.go changed",
		"- src/main.go changed with helper update",
		"- function HandleMain processes request path and returns success",
		"- HandleMain processes request path and returns success quickly",
	}, "\n")
	got := deduplicateBullets(input)
	if strings.Count(got, "\n") >= 3 {
		t.Fatalf("expected deduplicated bullets, got %q", got)
	}
	if !strings.Contains(got, "changed with helper update") {
		t.Fatalf("expected longer substring bullet to remain, got %q", got)
	}
}

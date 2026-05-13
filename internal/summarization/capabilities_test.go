package summarization

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
)

// newMockMiniMaxServer is a tiny fixture for capability tests: it
// captures the request body via the given handler and returns the
// supplied response payload. Caller is responsible for srv.Close().
func newMockMiniMaxServer(t *testing.T, handler func(body []byte) []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		resp := handler(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)
	}))
}

// defaultMiniMaxConfig builds a baseline MiniMaxConfig pointed at the
// mock server URL.
func defaultMiniMaxConfig(baseURL string) config.MiniMaxConfig {
	return config.MiniMaxConfig{
		BaseURL:                baseURL,
		APIKeyEnv:              "MINIMAX_API_KEY",
		Model:                  "test-model",
		Temperature:            0,
		MaxRetries:             0,
		ConnectTimeoutSeconds:  2,
		ResponseTimeoutSeconds: 4,
		RateLimitRPM:           120,
		EnableReasoningSplit:   true,
	}
}

func TestComputeStableSeed_Deterministic(t *testing.T) {
	a := computeStableSeed("m1", 0, 5, "hello world")
	b := computeStableSeed("m1", 0, 5, "hello world")
	if a != b {
		t.Fatalf("seed must be deterministic: %d vs %d", a, b)
	}
}

func TestComputeStableSeed_DifferentInputs(t *testing.T) {
	a := computeStableSeed("m1", 0, 5, "hello")
	b := computeStableSeed("m1", 0, 5, "world")
	if a == b {
		t.Fatalf("different inputs must yield different seeds: %d", a)
	}
}

func TestComputeStableSeed_PositiveBounded(t *testing.T) {
	for _, in := range []string{"", "a", strings.Repeat("x", 4096), "z\x00\xffÿ中文"} {
		seed := computeStableSeed("m", 1, 2, in)
		if seed < 0 {
			t.Fatalf("seed must be non-negative, got %d for %q", seed, in)
		}
	}
}

func TestSummarize_PayloadIncludesMinAndSeedWhenSupported(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	captured := make(chan []byte, 1)
	srv := newMockMiniMaxServer(t, func(body []byte) []byte {
		select {
		case captured <- body:
		default:
		}
		return []byte(`{"choices":[{"message":{"role":"assistant","content":"- fact one [msg:0]\n- fact two [msg:1]\n- fact three [msg:2]\n- fact four [msg:3]\n- fact five [msg:4]"}}]}`)
	})
	defer srv.Close()

	cfg := defaultMiniMaxConfig(srv.URL)
	cfg.MaxRetries = 0
	client := NewMiniMaxClient(cfg)
	client.SetCapabilities(capProvider{SupportsSeed: true, SupportsMinCompletionTokens: true})

	out, err := client.Summarize(t.Context(), "input transcript text", 0, 5, 200)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected non-empty summary")
	}
	body := <-captured
	if !bytes_contains(body, "min_tokens") {
		t.Fatalf("min_tokens missing: %s", string(body))
	}
	if !bytes_contains(body, "seed") {
		t.Fatalf("seed missing: %s", string(body))
	}
}

func TestSummarize_PayloadOmitsOptionalsByDefault(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	captured := make(chan []byte, 1)
	srv := newMockMiniMaxServer(t, func(body []byte) []byte {
		select {
		case captured <- body:
		default:
		}
		return []byte(`{"choices":[{"message":{"role":"assistant","content":"- fact one [msg:0]\n- fact two [msg:1]\n- fact three [msg:2]\n- fact four [msg:3]\n- fact five [msg:4]"}}]}`)
	})
	defer srv.Close()

	cfg := defaultMiniMaxConfig(srv.URL)
	cfg.MaxRetries = 0
	client := NewMiniMaxClient(cfg)
	// caps left at zero value -> no optional fields

	if _, err := client.Summarize(t.Context(), "input transcript text", 0, 5, 200); err != nil {
		t.Fatal(err)
	}
	body := <-captured
	if bytes_contains(body, "min_tokens") || bytes_contains(body, `"seed":`) {
		t.Fatalf("optional fields leaked into default payload: %s", string(body))
	}
	if !bytes_contains(body, `"reasoning_split":true`) {
		t.Fatalf("MiniMax reasoning_split default missing: %s", string(body))
	}
}

func TestSummarize_PayloadUsesConfiguredSampling(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	captured := make(chan []byte, 1)
	srv := newMockMiniMaxServer(t, func(body []byte) []byte {
		select {
		case captured <- body:
		default:
		}
		return []byte(`{"choices":[{"message":{"role":"assistant","content":"- fact one [msg:0]\n- fact two [msg:1]\n- fact three [msg:2]\n- fact four [msg:3]\n- fact five [msg:4]"}}]}`)
	})
	defer srv.Close()

	cfg := defaultMiniMaxConfig(srv.URL)
	cfg.Temperature = 0.2
	cfg.TopP = 0.75
	client := NewMiniMaxClient(cfg)
	if _, err := client.Summarize(t.Context(), "input transcript text", 0, 5, 200); err != nil {
		t.Fatal(err)
	}
	body := string(<-captured)
	if !strings.Contains(body, `"temperature":0.2`) || !strings.Contains(body, `"top_p":0.75`) {
		t.Fatalf("configured sampling not present: %s", body)
	}
}

func TestSummarize_CanDisableReasoningSplit(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	captured := make(chan []byte, 1)
	srv := newMockMiniMaxServer(t, func(body []byte) []byte {
		select {
		case captured <- body:
		default:
		}
		return []byte(`{"choices":[{"message":{"role":"assistant","content":"- fact one [msg:0]\n- fact two [msg:1]\n- fact three [msg:2]\n- fact four [msg:3]\n- fact five [msg:4]"}}]}`)
	})
	defer srv.Close()

	cfg := defaultMiniMaxConfig(srv.URL)
	cfg.EnableReasoningSplit = false
	client := NewMiniMaxClient(cfg)
	if _, err := client.Summarize(t.Context(), "input transcript text", 0, 5, 200); err != nil {
		t.Fatal(err)
	}
	body := string(<-captured)
	if strings.Contains(body, "reasoning_split") {
		t.Fatalf("reasoning_split should be omitted when disabled: %s", body)
	}
}

func TestSummarize_MinTokensFloorClamped(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	captured := make(chan []byte, 1)
	srv := newMockMiniMaxServer(t, func(body []byte) []byte {
		select {
		case captured <- body:
		default:
		}
		return []byte(`{"choices":[{"message":{"role":"assistant","content":"- a [msg:0]\n- b [msg:1]\n- c [msg:2]\n- d [msg:3]\n- e [msg:4]"}}]}`)
	})
	defer srv.Close()

	cfg := defaultMiniMaxConfig(srv.URL)
	cfg.MaxRetries = 0
	client := NewMiniMaxClient(cfg)
	client.SetCapabilities(capProvider{SupportsMinCompletionTokens: true})
	// Tiny target -> floor clamp at 32.
	if _, err := client.Summarize(t.Context(), "input", 0, 1, 10); err != nil {
		t.Fatal(err)
	}
	body := <-captured
	if !bytes_contains(body, `"min_tokens":32`) {
		t.Fatalf("expected min_tokens=32 floor, got: %s", string(body))
	}
}

func bytes_contains(haystack []byte, needle string) bool {
	return strings.Contains(string(haystack), needle)
}

func TestSetCapabilities_RoundTrip(t *testing.T) {
	c := &MiniMaxClient{}
	if got := c.Capabilities(); got.SupportsSeed || got.SupportsMinCompletionTokens {
		t.Fatalf("default caps must be all-false: %+v", got)
	}
	c.SetCapabilities(capProvider{SupportsSeed: true, SupportsMinCompletionTokens: true})
	if got := c.Capabilities(); !got.SupportsSeed || !got.SupportsMinCompletionTokens {
		t.Fatalf("override did not stick: %+v", got)
	}
}

// TestNewLayer2_WiresMiniMaxCapabilities asserts T91: NewLayer2 propagates
// `[compression.minimax] enable_seed` / `enable_min_tokens` to the MiniMax
// client through SetCapabilities, defaulting both off.
func TestNewLayer2_WiresMiniMaxCapabilities(t *testing.T) {
	t.Parallel()

	t.Run("default off", func(t *testing.T) {
		t.Parallel()
		full := config.Defaults()
		layer := NewLayer2(&full.Compression)
		mm := primaryMiniMax(t, layer)
		got := mm.Capabilities()
		if got.SupportsSeed || got.SupportsMinCompletionTokens {
			t.Fatalf("defaults must be off, got %+v", got)
		}
	})

	t.Run("flags propagate", func(t *testing.T) {
		t.Parallel()
		full := config.Defaults()
		full.Compression.MiniMax.EnableSeed = true
		full.Compression.MiniMax.EnableMinTokens = true
		layer := NewLayer2(&full.Compression)
		mm := primaryMiniMax(t, layer)
		got := mm.Capabilities()
		if !got.SupportsSeed || !got.SupportsMinCompletionTokens {
			t.Fatalf("flags not propagated: %+v", got)
		}
	})
}

func primaryMiniMax(t *testing.T, layer *Layer2) *MiniMaxClient {
	t.Helper()
	providers := layer.chain.Providers()
	if len(providers) == 0 {
		t.Fatal("chain has no providers")
	}
	mm, ok := providers[0].(*MiniMaxClient)
	if !ok {
		t.Fatalf("primary is not *MiniMaxClient: %T", providers[0])
	}
	return mm
}

package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/readcache"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/types"
)

func TestAdminStatusSnapshot_Layer2Details(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Defaults()
	p := New(cfg)

	createdAt := time.Unix(1700000000, 0).UTC()
	p.layer2.GetCache().Compressing.Store(true)
	p.layer2.GetCache().Store(&summarization.CachedSummary{CreatedAt: createdAt})
	p.compressQueue <- types.CompressJob{}
	if err := readcache.RecordDecision(readcache.DefaultDir(home), readcache.Decision{Type: readcache.DecisionBlock, BlockKind: readcache.BlockKindDelta}); err != nil {
		t.Fatalf("record read cache: %v", err)
	}

	got := p.adminStatusSnapshot()
	if !got.Layer2.HasCache {
		t.Fatal("expected layer2 cache in admin snapshot")
	}
	if !got.Layer2.Compressing {
		t.Fatal("expected compressing flag in admin snapshot")
	}
	if got.Layer2.QueueDepth != 1 {
		t.Fatalf("queue depth: got %d want 1", got.Layer2.QueueDepth)
	}
	if !got.Layer2.LastRun.Equal(createdAt) {
		t.Fatalf("last run: got %s want %s", got.Layer2.LastRun, createdAt)
	}
	if got.ReadCache.DeltaBlocks != 1 || got.ReadCache.Blocks != 1 {
		t.Fatalf("unexpected read cache snapshot: %+v", got.ReadCache)
	}
}

func TestAdminStatusSnapshot_WithoutLayer2(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	p := New(cfg)
	p.ClearLayer2ForTesting()

	got := p.adminStatusSnapshot()
	if got.Layer2.HasCache || got.Layer2.Compressing || !got.Layer2.LastRun.IsZero() || got.Layer2.QueueDepth != 0 {
		t.Fatalf("unexpected layer2 status without layer2: %+v", got.Layer2)
	}
}

func TestAdminStatusSnapshot_WithoutQualityAB(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := New(config.Defaults())
	p.qualityAB = nil
	got := p.adminStatusSnapshot()
	if got.QualityAB.Enabled || got.QualityAB.ControlTotal != 0 || got.QualityAB.TreatmentTotal != 0 {
		t.Fatalf("unexpected qualityab telemetry without harness: %+v", got.QualityAB)
	}
}

func TestAdminStatusSnapshot_PromptCacheProviderTelemetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	p := New(cfg)
	p.processAnalyticsEvent(types.AnalyticsEvent{
		Type:              types.EventRequestProcessed,
		Provider:          types.Anthropic,
		CacheReadTokens:   1000,
		CacheCreateTokens: 250,
	})

	got := p.adminStatusSnapshot()
	if got.PromptCache.CacheReadTokens != 1000 {
		t.Fatalf("cache read tokens = %d, want 1000", got.PromptCache.CacheReadTokens)
	}
	if got.PromptCache.CacheCreateTokens != 250 {
		t.Fatalf("cache create tokens = %d, want 250", got.PromptCache.CacheCreateTokens)
	}
	if got.PromptCache.EstimatedSavedReadTokens != 900 {
		t.Fatalf("estimated saved read tokens = %d, want 900", got.PromptCache.EstimatedSavedReadTokens)
	}
}

func TestAdminStatusSnapshot_WithoutOutputReduceTracker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	p := New(cfg)
	p.outputReduce = nil

	got := p.adminStatusSnapshot()
	if got.OutputReduce.InjectedTurns != 0 || got.OutputReduce.SkippedTurns != 0 || got.OutputReduce.OutputTokensObserved != 0 {
		t.Fatalf("unexpected output-reduce snapshot without tracker: %+v", got.OutputReduce)
	}
}

func TestDecodeAdminJSON_NilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, AdminStatusPath, http.NoBody)
	req.Body = nil
	var dst map[string]any
	if decodeAdminJSON(req, &dst) {
		t.Fatal("nil body should not decode")
	}
}

func TestAdminProviderHandler_AnthropicSuccess(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	body := []byte(`{"provider":"anthropic","enabled":false}`)
	req := httptest.NewRequest(http.MethodPost, AdminProviderPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code: %d", rec.Code)
	}
	if p.IsProviderEnabled(types.Anthropic) {
		t.Fatal("anthropic provider should be disabled")
	}
}

func TestAdminProviderHandler_CodexSuccess(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	body := []byte(`{"provider":"codex_chatgpt","enabled":false}`)
	req := httptest.NewRequest(http.MethodPost, AdminProviderPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code: %d", rec.Code)
	}
	if p.IsProviderEnabled(types.CodexChatGPT) {
		t.Fatal("codex_chatgpt provider should be disabled")
	}
}

func TestAdminProviderHandler_WrongMethod(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	req := httptest.NewRequest(http.MethodGet, AdminProviderPath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code: got %d want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAdminLayerHandler_MethodAndBadJSON(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	tests := []struct {
		name string
		req  *http.Request
		want int
	}{
		{
			name: "wrong method",
			req:  httptest.NewRequest(http.MethodGet, AdminLayerPath, nil),
			want: http.StatusMethodNotAllowed,
		},
		{
			name: "bad json",
			req:  httptest.NewRequest(http.MethodPost, AdminLayerPath, bytes.NewReader([]byte(`{`))),
			want: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			p.Handler().ServeHTTP(rec, tc.req)
			if rec.Code != tc.want {
				t.Fatalf("status code: got %d want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestAdminHandlers_JSONResponses(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	req := httptest.NewRequest(http.MethodPost, AdminFlushPath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content type: %q", contentType)
	}

	var got adminActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.OK {
		t.Fatal("expected ok response")
	}
}

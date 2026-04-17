package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/summarization"
)

func TestServeHTTP_zeroDownsideRevertsBeforeForwarding(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = true
	cfg.Compression.Layer3Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	p.layer2.GetCache().Store(&summarization.CachedSummary{
		Summary:          strings.Repeat("oversized summary ", 80),
		CoveredRange:     [2]int{0, 1},
		OriginalTokens:   10,
		CompressedTokens: 2000,
		CreatedAt:        time.Now(),
	})

	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"tail"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, rec.Body.String())
	}

	var forwarded struct {
		Messages []struct {
			Role    string      `json:"role"`
			Content interface{} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(capturedBody, &forwarded); err != nil {
		t.Fatalf("parse forwarded body: %v\nbody: %s", err, capturedBody)
	}
	if len(forwarded.Messages) != 3 {
		t.Fatalf("expected original 3 messages after revert, got %d; body=%s", len(forwarded.Messages), capturedBody)
	}
	if strings.Contains(string(capturedBody), "Conversation summary covering messages") {
		t.Fatalf("forwarded body still contains synthetic summary after zero-downside revert: %s", capturedBody)
	}
}

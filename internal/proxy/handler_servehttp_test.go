package proxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokenproxy/tokenproxy/internal/config"
	"github.com/tokenproxy/tokenproxy/internal/types"
)

var errReadBodyTest = errors.New("read body failed")

type alwaysFailBody struct{}

func (alwaysFailBody) Read([]byte) (int, error) { return 0, errReadBodyTest }

func TestServeHTTP_passthroughGET(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"upstream":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	p := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, rec.Body.String())
	}
}

func TestServeHTTP_passthroughProviderDisabled(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	p := New(cfg)
	p.SetProviderEnabled(types.Anthropic, false)

	body := `{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
}

func TestServeHTTP_readBodyFailedCompressible(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Body = io.NopCloser(alwaysFailBody{})
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read body") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_readBodyFailedPassthrough(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	req.Body = io.NopCloser(alwaysFailBody{})
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read body") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

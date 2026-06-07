package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/slimference/slimference/internal/config"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/readcache"
	"github.com/slimference/slimference/internal/types"
)

func TestAdminStatusHandler(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	p := New(cfg)
	p.DebugRecorder().Record(dbg.RequestSummary{
		RequestID: "admin-flight",
		Tokens:    dbg.TokenCounts{Original: 20, Final: 12, Saved: 8},
	})

	req := httptest.NewRequest(http.MethodGet, AdminStatusPath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code: %d", rec.Code)
	}

	var got AdminStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if got.Service != "slimference" || got.ListenPort != cfg.Proxy.ListenPort {
		t.Fatalf("unexpected status payload: %+v", got)
	}
	if got.Layers["1"] != cfg.Compression.Layer1Enabled {
		t.Fatalf("layer state mismatch: %+v", got.Layers)
	}
	if got.ReadCache.Evaluations != 0 {
		t.Fatalf("unexpected read cache status: %+v", got.ReadCache)
	}
	if len(got.RecentFlights) != 1 || got.RecentFlights[0].RequestID != "admin-flight" {
		t.Fatalf("unexpected recent flights: %+v", got.RecentFlights)
	}
}

func TestAdminProviderHandler(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	body := []byte(`{"provider":"openai","enabled":false}`)
	req := httptest.NewRequest(http.MethodPost, AdminProviderPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code: %d", rec.Code)
	}
	if p.IsProviderEnabled(types.OpenAI) {
		t.Fatal("openai provider should be disabled")
	}
}

func TestAdminLayerHandler(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	body := []byte(`{"layer":2,"enabled":false}`)
	req := httptest.NewRequest(http.MethodPost, AdminLayerPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code: %d", rec.Code)
	}
	if p.IsLayerEnabled(2) {
		t.Fatal("layer 2 should be disabled")
	}
}

func TestAdminFlushHandler(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Defaults()
	p := New(cfg)
	if err := readcache.RecordDecision(readcache.DefaultDir(home), readcache.Decision{Type: readcache.DecisionBlock, BlockKind: readcache.BlockKindUnchanged}); err != nil {
		t.Fatalf("record read cache: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, AdminFlushPath, nil)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code: %d", rec.Code)
	}
	if _, err := os.Stat(readcache.DefaultDir(home)); !os.IsNotExist(err) {
		t.Fatalf("read cache dir should be removed, err=%v", err)
	}
}

func TestAdminHandlers_BadRequests(t *testing.T) {
	cfg := config.Defaults()
	p := New(cfg)

	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{
			name: "status wrong method",
			req:  httptest.NewRequest(http.MethodPost, AdminStatusPath, nil),
			want: http.StatusMethodNotAllowed,
		},
		{
			name: "provider bad json",
			req:  httptest.NewRequest(http.MethodPost, AdminProviderPath, bytes.NewReader([]byte(`{`))),
			want: http.StatusBadRequest,
		},
		{
			name: "provider bad name",
			req:  httptest.NewRequest(http.MethodPost, AdminProviderPath, bytes.NewReader([]byte(`{"provider":"x","enabled":true}`))),
			want: http.StatusBadRequest,
		},
		{
			name: "layer bad range",
			req:  httptest.NewRequest(http.MethodPost, AdminLayerPath, bytes.NewReader([]byte(`{"layer":9,"enabled":true}`))),
			want: http.StatusBadRequest,
		},
		{
			name: "flush wrong method",
			req:  httptest.NewRequest(http.MethodGet, AdminFlushPath, nil),
			want: http.StatusMethodNotAllowed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			p.Handler().ServeHTTP(rec, tc.req)
			if rec.Code != tc.want {
				t.Fatalf("status code: got %d want %d", rec.Code, tc.want)
			}
		})
	}
}

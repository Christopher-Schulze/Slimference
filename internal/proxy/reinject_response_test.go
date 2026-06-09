package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
)

// TestServeHTTP_T76c_RecordsReInjectOnUpstreamEcho verifies that the
// proxy increments the contentarchive re_inject_count when the
// upstream response body echoes a local-archive URI. The expansion
// itself happens on the *next* request via reinjectArchivedContent
// (already covered by T76 WP3 happy-path tests).
func TestServeHTTP_T76c_RecordsReInjectOnUpstreamEcho(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed the archive so the URI in the upstream response resolves
	// to something real (the recorder still bumps re_inject_count
	// even when the bytes are not consumed in this turn).
	dir := contentarchive.DefaultDir(home)
	if _, err := contentarchive.Put(dir, contentarchive.Input{
		SessionID:    "sess-T76c",
		MessageIndex: 1,
		BlockIndex:   0,
		SubLayer:     "structure_extract",
		Original:     strings.Repeat("archived line\n", 8),
	}, contentarchive.Limits{}); err != nil {
		t.Fatal(err)
	}
	statsBefore, _ := contentarchive.LoadStats(dir)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Assistant turn echoes a local-archive URI.
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"see local-archive://abcdef-1234"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}

	statsAfter, err := contentarchive.LoadStats(dir)
	if err != nil {
		t.Fatalf("load stats: %v", err)
	}
	if statsAfter.ReInjectCount <= statsBefore.ReInjectCount {
		t.Fatalf("re_inject_count must advance: before=%d after=%d",
			statsBefore.ReInjectCount, statsAfter.ReInjectCount)
	}
}

// TestServeHTTP_T76c_NoOpWhenNoArchiveURI verifies the path is a
// no-op when the response carries no archive references.
func TestServeHTTP_T76c_NoOpWhenNoArchiveURI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := contentarchive.DefaultDir(home)
	statsBefore, _ := contentarchive.LoadStats(dir)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"plain reply"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	statsAfter, _ := contentarchive.LoadStats(dir)
	if statsAfter.ReInjectCount != statsBefore.ReInjectCount {
		t.Fatalf("re_inject_count must NOT advance for plain replies: before=%d after=%d",
			statsBefore.ReInjectCount, statsAfter.ReInjectCount)
	}
}

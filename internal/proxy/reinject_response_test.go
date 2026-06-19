package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
)

func TestServeHTTP_ReinjectsArchiveBeforeUpstream(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	original := strings.Repeat("exact archived body before upstream\n", 4)
	id := writeArchiveEntry(t, home, original)

	seenBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		seenBody <- string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"need local-archive://` + id + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	upstreamBody := <-seenBody
	if !strings.Contains(upstreamBody, "need local-archive://"+id) {
		t.Fatalf("original archive marker must remain visible for traceability: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, "[reinjected from local-archive://"+id+"]") ||
		!strings.Contains(upstreamBody, "exact archived body before upstream") {
		t.Fatalf("archive body was not rehydrated before upstream: %s", upstreamBody)
	}

	var decoded struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(upstreamBody), &decoded); err != nil {
		t.Fatalf("upstream body json: %v", err)
	}
	if len(decoded.Messages) != 1 {
		t.Fatalf("rehydrated body not present in model-visible message content: %+v", decoded.Messages)
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(decoded.Messages[0].Content, &blocks); err != nil {
		t.Fatalf("content blocks json: %v", err)
	}
	foundExactBody := false
	for _, block := range blocks {
		if strings.Contains(block.Text, original) {
			foundExactBody = true
			break
		}
	}
	if !foundExactBody {
		t.Fatalf("rehydrated exact body not present in decoded model-visible content: %+v", blocks)
	}
}

func TestServeHTTP_MissingArchiveFailsOpenBeforeUpstream(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	seenBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		seenBody <- string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"need local-archive://missing-id"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	upstreamBody := <-seenBody
	if !strings.Contains(upstreamBody, "local-archive://missing-id") {
		t.Fatalf("missing archive marker must stay byte-visible: %s", upstreamBody)
	}
	if strings.Contains(upstreamBody, "[reinjected from") {
		t.Fatalf("missing archive must not invent a rehydrated block: %s", upstreamBody)
	}
	stats, err := contentarchive.LoadStats(contentarchive.DefaultDir(home))
	if err != nil {
		t.Fatalf("load stats: %v", err)
	}
	if stats.ReInjectCount != 0 || stats.Expanded != 0 {
		t.Fatalf("missing archive must not count expansion/reinject: %+v", stats)
	}
}

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

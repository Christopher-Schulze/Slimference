package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/slimference/slimference/internal/config"
)

// TestStaleReadAgingWiredIntoHandler proves the T170 aging runs
// against the messages of the request and reaches the upstream with
// older Read tool_results collapsed into markers.
func TestStaleReadAgingWiredIntoHandler(t *testing.T) {
	var capturedBody atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		capturedBody.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = true
	cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = 2
	p := New(cfg)

	bigBody := strings.Repeat("file body line. ", 50)

	req := map[string]any{
		"model": "claude",
		"messages": []map[string]any{
			{"role": "user", "content": "please read src/x.go"},
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "tu1", "name": "Read", "input": map[string]any{"path": "src/x.go"}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu1", "content": bigBody},
			}},
			{"role": "user", "content": "filler 1"},
			{"role": "user", "content": "filler 2"},
			{"role": "user", "content": "please re-read src/x.go"},
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "tu2", "name": "Read", "input": map[string]any{"path": "src/x.go"}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu2", "content": "fresh content"},
			}},
		},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	httpReq.Header.Set("x-api-key", "test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	got := capturedBody.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	upstreamBody := *got

	if strings.Contains(upstreamBody, bigBody) {
		t.Errorf("older read content not aged: still present in upstream body")
	}
	if !strings.Contains(upstreamBody, "kind=stale-read") {
		t.Errorf("aging marker missing in upstream body: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, "src/x.go") {
		t.Errorf("path missing in upstream body")
	}
	if !strings.Contains(upstreamBody, "fresh content") {
		t.Errorf("fresh read removed: %s", upstreamBody)
	}

	snap := p.outputReduceCounters.Snapshot()
	if snap.StaleReadBlocksReplaced == 0 {
		t.Errorf("counter not incremented: %+v", snap)
	}
}

// TestObsoleteReadPruneWiredIntoHandler proves T174 prunes a read
// whose file has been mutated by a subsequent apply_patch.
func TestObsoleteReadPruneWiredIntoHandler(t *testing.T) {
	var capturedBody atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		capturedBody.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = true
	p := New(cfg)

	bigBody := strings.Repeat("pre-edit file content. ", 50)
	req := map[string]any{
		"model": "claude",
		"messages": []map[string]any{
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "tu1", "name": "Read", "input": map[string]any{"path": "src/x.go"}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu1", "content": bigBody},
			}},
			{"role": "user", "content": "now please edit"},
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "tu2", "name": "apply_patch", "input": map[string]any{"path": "src/x.go", "patch": "@@ ..."}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu2", "content": "patch applied"},
			}},
			{"role": "user", "content": "now reflect"},
		},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	httpReq.Header.Set("x-api-key", "test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	got := capturedBody.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	upstreamBody := *got
	if strings.Contains(upstreamBody, bigBody) {
		t.Errorf("pre-edit read content not pruned: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, "kind=obsolete-read") {
		t.Errorf("obsolete marker missing: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, "src/x.go") {
		t.Errorf("path missing in marker")
	}
	snap := p.outputReduceCounters.Snapshot()
	if snap.ObsoleteReadBlocksPruned == 0 {
		t.Errorf("counter not incremented: %+v", snap)
	}
}

// TestObsoleteReadPruneDisabledLeavesMessages confirms the toggle.
func TestObsoleteReadPruneDisabledLeavesMessages(t *testing.T) {
	var capturedBody atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		capturedBody.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)

	bigBody := strings.Repeat("v1 ", 100)
	req := map[string]any{
		"model": "claude",
		"messages": []map[string]any{
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "tu1", "name": "Read", "input": map[string]any{"path": "x.go"}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu1", "content": bigBody},
			}},
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "tu2", "name": "Edit", "input": map[string]any{"path": "x.go"}},
			}},
		},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	httpReq.Header.Set("x-api-key", "test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	if !strings.Contains(*capturedBody.Load(), bigBody) {
		t.Errorf("disabled toggle still pruned: %s", *capturedBody.Load())
	}
}

// TestStaleReadAgingDisabledLeavesMessagesIntact confirms the toggle.
func TestStaleReadAgingDisabledLeavesMessagesIntact(t *testing.T) {
	var capturedBody atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		capturedBody.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	p := New(cfg)

	bigBody := strings.Repeat("file body line. ", 50)
	req := map[string]any{
		"model": "claude",
		"messages": []map[string]any{
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "tu1", "name": "Read", "input": map[string]any{"path": "src/x.go"}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu1", "content": bigBody},
			}},
			{"role": "user", "content": "filler"},
			{"role": "user", "content": "filler"},
			{"role": "user", "content": "filler"},
			{"role": "assistant", "content": []map[string]any{
				{"type": "tool_use", "id": "tu2", "name": "Read", "input": map[string]any{"path": "src/x.go"}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu2", "content": "fresh"},
			}},
		},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	httpReq.Header.Set("x-api-key", "test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)

	got := capturedBody.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	if !strings.Contains(*got, bigBody) {
		t.Errorf("disabled toggle still aged content")
	}
}

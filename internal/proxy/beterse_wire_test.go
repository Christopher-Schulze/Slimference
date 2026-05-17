package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/slimference/slimference/internal/beterse"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/qualityab"
)

// findTreatmentOrgUserPair iterates org/user pairs until the
// resulting anthropic-shaped sessionID lands in the treatment
// cohort. Returns (org, user, sessionID).
func findTreatmentOrgUserPair(h *qualityab.Harness) (string, string, string) {
	for i := 0; i < 2000; i++ {
		org := fmt.Sprintf("org-A%d", i)
		user := fmt.Sprintf("user-B%d", i*3+1)
		sid := "anthropic:" + org + ":" + user
		if h.Cohort(sid) == qualityab.CohortTreatment {
			return org, user, sid
		}
	}
	return "", "", ""
}

func findControlOrgUserPair(h *qualityab.Harness) (string, string, string) {
	for i := 0; i < 2000; i++ {
		org := fmt.Sprintf("co-A%d", i)
		user := fmt.Sprintf("cu-B%d", i*5+2)
		sid := "anthropic:" + org + ":" + user
		if h.Cohort(sid) == qualityab.CohortControl {
			return org, user, sid
		}
	}
	return "", "", ""
}

// TestBeTerseHintInjectedForTreatmentSession proves T169 injects the
// hint when the toggle is on and the session falls into the
// treatment cohort.
func TestBeTerseHintInjectedForTreatmentSession(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = true
	p := New(cfg)
	org, user, _ := findTreatmentOrgUserPair(p.qualityAB)
	if org == "" {
		t.Fatal("could not find treatment org/user")
	}

	req := map[string]any{
		"model":    "claude",
		"system":   "You are helpful.",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
		"metadata": map[string]any{"user_id": user},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	httpReq.Header.Set("x-api-key", "test")
	httpReq.Header.Set("anthropic-organization-id", org)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := captured.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	if !strings.Contains(*got, beterse.DefaultHint) {
		t.Errorf("hint not injected for treatment cohort: %s", *got)
	}
	if !strings.Contains(*got, "You are helpful.") {
		t.Errorf("existing system prompt lost")
	}
	snap := p.outputReduceCounters.Snapshot()
	if snap.BeterseInjections == 0 {
		t.Errorf("counter not incremented")
	}
}

// TestBeTerseHintNotInjectedForControlSession proves the cohort
// routing keeps control sessions unchanged.
func TestBeTerseHintNotInjectedForControlSession(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = true
	p := New(cfg)
	org, user, _ := findControlOrgUserPair(p.qualityAB)
	if org == "" {
		t.Fatal("could not find control org/user")
	}

	req := map[string]any{
		"model":    "claude",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
		"metadata": map[string]any{"user_id": user},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	httpReq.Header.Set("x-api-key", "test")
	httpReq.Header.Set("anthropic-organization-id", org)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	got := captured.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	if strings.Contains(*got, beterse.DefaultHint) {
		t.Errorf("hint injected for control cohort: %s", *got)
	}
}

// TestBeTerseHintDisabledNeverInjects confirms the master toggle.
func TestBeTerseHintDisabledNeverInjects(t *testing.T) {
	var captured atomic.Pointer[string]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	p := New(cfg)

	req := map[string]any{
		"model":    "claude",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	httpReq.Header.Set("x-api-key", "test")
	httpReq.Header.Set("X-Session-Id", "any-session")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	got := captured.Load()
	if got == nil {
		t.Fatal("upstream did not receive body")
	}
	if strings.Contains(*got, beterse.DefaultHint) {
		t.Errorf("hint injected while disabled: %s", *got)
	}
}

func TestBeTerseTreatmentNoInjectionRecordsControlError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = true
	p := New(cfg)
	org, user, _ := findTreatmentOrgUserPair(p.qualityAB)
	if org == "" {
		t.Fatal("could not find treatment org/user")
	}

	req := map[string]any{
		"model":    "claude",
		"system":   123,
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
		"metadata": map[string]any{"user_id": user},
	}
	rb, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(rb)))
	httpReq.Header.Set("anthropic-organization-id", org)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

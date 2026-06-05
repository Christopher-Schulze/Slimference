package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/abharness"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/contextledger"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/types"
)

func TestApplyHTTPFullHistoryOCRLBuildsShadowProofWithoutReplacingContext(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OCRL.Mode = "max"
	cfg.Compression.OCRL.MinNetSavedTokens = 1
	cfg.Compression.OCRL.MaxCapsules = 4
	p := New(cfg)

	oldText := strings.Repeat("legacy assistant tool observation with stable unique payload alpha42 ", 220)
	recentText := "current assistant response stays verbatim"
	messages := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: oldText}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "current user instruction must never be replaced"}}},
		{Index: 2, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: recentText}}},
	}

	result := p.applyHTTPFullHistoryOCRL(types.OpenAI, "sess-ocrl-full-history", messages, 1, 0)
	if result.Applied || !result.HasSummary || result.Saved != 0 {
		t.Fatalf("expected shadow-only OCRL proof without product savings: %+v", result)
	}
	if got := result.Messages[0].Content[0].Text; got != oldText {
		t.Fatalf("shadow OCRL must not replace old context: %q", got)
	}
	if result.Messages[1].Content[0].Text != messages[1].Content[0].Text || result.Messages[2].Content[0].Text != recentText {
		t.Fatalf("recent/user blocks changed: %+v", result.Messages)
	}
	if !result.Summary.TelemetryOnly ||
		result.Summary.OCRLRoute != string(contextledger.OCRLRouteFullHistoryHTTP) ||
		result.Summary.OCRLReason != string(contextledger.OCRLReasonShadowOnly) ||
		!result.Summary.OCRLShadowOnly ||
		result.Summary.OCRLCandidateCapsules != 1 ||
		result.Summary.OCRLArchiveExpansions != 1 ||
		result.Summary.OCRLShadowSavedTokens <= 0 {
		t.Fatalf("summary missing shadow OCRL proof evidence: %+v", result.Summary)
	}
}

func TestApplyHTTPFullHistoryOCRLShadowProofLeavesRawContextRecoverable(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OCRL.Mode = "max"
	cfg.Compression.OCRL.MinNetSavedTokens = 1
	cfg.Compression.OCRL.MaxCapsules = 8
	p := New(cfg)

	oldBuildLog := strings.Repeat("old build log context with stable error-free package inventory omega11 ", 180)
	oldSearchLog := strings.Repeat("old search context showing repeated repository hits under src/ theta22 ", 180)
	recentText := "recent assistant tail stays visible"
	before := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: oldBuildLog}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: oldSearchLog}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "current user instruction stays visible"}}},
		{Index: 3, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: recentText}}},
	}

	proof := p.applyHTTPFullHistoryOCRL(types.OpenAI, "sess-ocrl-ab-proof", before, 1, 0)
	if proof.Applied || proof.Saved != 0 || proof.Summary.OCRLArchiveExpansions != 2 || proof.Summary.OCRLShadowSavedTokens <= 0 {
		t.Fatalf("expected shadow OCRL proof with two archive-backed targets: %+v", proof)
	}
	afterText := proof.Messages[0].Content[0].Text + "\n" + proof.Messages[1].Content[0].Text
	if strings.Contains(afterText, "[ocrl:v1") ||
		!strings.Contains(afterText, oldBuildLog) ||
		!strings.Contains(afterText, oldSearchLog) {
		t.Fatalf("shadow OCRL must keep old context visible: %q", afterText)
	}

	archiveDir := contentarchive.DefaultDir(home)
	report := abharness.CompareWithArchiveExpansion([]abharness.Turn{
		{Before: before, After: proof.Messages},
	}, func(id string) ([]byte, error) {
		_, body, err := contentarchive.Get(archiveDir, id)
		return body, err
	})
	if report.Lost() != 0 {
		t.Fatalf("shadow OCRL must have zero lost context: %+v", report.Elisions)
	}
	if len(report.Elisions) != 0 || report.Saved() != 0 {
		t.Fatalf("shadow OCRL must not elide model context, got %+v saved=%d", report.Elisions, report.Saved())
	}
}

func TestApplyHTTPFullHistoryOCRLShadowBuildsProofButDoesNotMutate(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OCRL.Mode = "shadow"
	p := New(cfg)

	oldText := strings.Repeat("shadow-only old assistant observation with stable archive payload beta77 ", 220)
	messages := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: oldText}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "recent assistant tail"}}},
	}

	result := p.applyHTTPFullHistoryOCRL(types.OpenAI, "sess-ocrl-shadow-http", messages, 1, 0)
	if result.Applied || !result.HasSummary {
		t.Fatalf("shadow OCRL must report proof without applying: %+v", result)
	}
	if result.Messages[0].Content[0].Text != oldText {
		t.Fatalf("shadow OCRL mutated model context: %+v", result.Messages[0].Content[0])
	}
	if result.Summary.OCRLReason != string(contextledger.OCRLReasonShadowOnly) ||
		!result.Summary.OCRLShadowOnly ||
		result.Summary.OCRLCandidateCapsules != 1 ||
		result.Summary.OCRLArchiveExpansions != 1 ||
		result.Summary.OCRLShadowSavedTokens <= 0 {
		t.Fatalf("shadow summary missing proof accounting: %+v", result.Summary)
	}
}

func TestApplyHTTPFullHistoryOCRLSkipsUserAndQualityPressure(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OCRL.Mode = "max"
	p := New(cfg)

	userText := strings.Repeat("old user instruction that must remain fully visible ", 220)
	messages := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: userText}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "recent assistant tail"}}},
	}
	userOnly := p.applyHTTPFullHistoryOCRL(types.OpenAI, "sess-ocrl-user-skip", messages, 1, 0)
	if userOnly.Applied || userOnly.HasSummary || userOnly.Messages[0].Content[0].Text != userText {
		t.Fatalf("user context must be a full-pass: %+v", userOnly)
	}

	oldAssistant := strings.Repeat("old assistant observation skipped under quality pressure gamma99 ", 220)
	messages[0] = types.Message{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: oldAssistant}}}
	qualityPressure := p.applyHTTPFullHistoryOCRL(types.OpenAI, "sess-ocrl-quality-pressure", messages, 1, 1)
	if qualityPressure.Applied || qualityPressure.HasSummary || qualityPressure.Messages[0].Content[0].Text != oldAssistant {
		t.Fatalf("quality pressure must full-pass without product mutation: %+v", qualityPressure)
	}
}

func TestServeHTTPKeepsOCRLFullHistoryShadowOnly(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	var upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"done"}}],"model":"gpt-5"}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = true
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.SlidingWindow = 1
	cfg.Compression.OCRL.Mode = "max"
	cfg.Compression.OCRL.MinNetSavedTokens = 1
	cfg.Secrets.Mode = "off"
	p := New(cfg)
	p.debugRecorder = dbg.NewRecorder(10, "")

	oldText := strings.Repeat("old full-history assistant context sent to upstream only through OCRL delta900 ", 220)
	body := `{"model":"gpt-5","messages":[` +
		`{"role":"assistant","content":` + quoteJSON(oldText) + `},` +
		`{"role":"user","content":"current user message must stay visible"},` +
		`{"role":"assistant","content":"recent assistant tail"}` +
		`]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, rec.Body.String())
	}
	if strings.Contains(upstreamBody, "[ocrl:v1") || !strings.Contains(upstreamBody, oldText) {
		t.Fatalf("upstream request must keep original old context:\n%s", upstreamBody)
	}
	last := p.debugRecorder.Last(1, false)
	if len(last) != 1 {
		t.Fatal("missing debug summary")
	}
	if last[0].ContextLedger.OCRLRoute != string(contextledger.OCRLRouteFullHistoryHTTP) ||
		last[0].ContextLedger.OCRLReason != string(contextledger.OCRLReasonShadowOnly) ||
		!last[0].ContextLedger.OCRLShadowOnly ||
		!last[0].ContextLedger.TelemetryOnly ||
		last[0].ContextLedger.OCRLShadowSavedTokens <= 0 {
		t.Fatalf("debug summary missing OCRL shadow evidence: %+v", last[0].ContextLedger)
	}
}

func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

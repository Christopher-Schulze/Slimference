package summarization

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestDetermineCompressionTiers(t *testing.T) {
	t.Parallel()
	if tiers := DetermineCompressionTiers(5, 5); tiers != nil {
		t.Fatalf("compressible <= 0 want nil, got %#v", tiers)
	}
	// total 30, window 5 -> compressible 25 -> multi-tier path
	tiers := DetermineCompressionTiers(30, 5)
	if len(tiers) < 2 {
		t.Fatalf("expected multi-tier, got %#v", tiers)
	}
	if tiers[0].Name != "tier-1" {
		t.Fatalf("first tier: %#v", tiers[0])
	}
	// Short session: single tier
	short := DetermineCompressionTiers(24, 5)
	if len(short) != 1 || short[0].Name != "tier-single" {
		t.Fatalf("short session: %#v", short)
	}
	// compressible == 20 → multi-tier-1 + window (not tier-single).
	exactly20 := DetermineCompressionTiers(25, 5)
	if len(exactly20) < 2 {
		t.Fatalf("compressible 20: %#v", exactly20)
	}
	if exactly20[0].Name != "tier-1" || exactly20[len(exactly20)-1].Name != "window" {
		t.Fatalf("unexpected layout: %#v", exactly20)
	}
}

func TestRatioStr(t *testing.T) {
	t.Parallel()
	if got := ratioStr(0); got != "0%" {
		t.Errorf("ratioStr(0) = %q", got)
	}
	if got := ratioStr(0.25); got != "25%" {
		t.Errorf("ratioStr(0.25) = %q", got)
	}
	if got := ratioStr(1); got != "100%" {
		t.Errorf("ratioStr(1) = %q", got)
	}
}

func TestApplyProgressiveTiers_verbatimRatio(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "x"}}},
	}
	tiers := []CompressionTier{
		{Name: "window", MsgRange: [2]int{0, 0}, TargetRatio: 1.0},
	}
	out := l.ApplyProgressiveTiers(msgs, tiers)
	if len(out) != 1 || out[0].Content[0].Text != "x" {
		t.Fatalf("%#v", out)
	}
}

func TestApplyProgressiveTiers_nilTiers(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "keep"}}},
	}
	out := l.ApplyProgressiveTiers(msgs, nil)
	if len(out) != 1 || out[0].Content[0].Text != "keep" {
		t.Fatalf("%#v", out)
	}
}

func TestApplyProgressiveTiers_minimaxNotConfiguredVerbatim(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.MiniMax.APIKeyEnv = "__SLIMFERENCE_NO_MINIMAX_KEY__"
	l := NewLayer2(&cfg)
	body := strings.Repeat("plain ", 200)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: body}}},
	}
	tiers := []CompressionTier{{Name: "t1", MsgRange: [2]int{0, 0}, TargetRatio: 0.2}}
	out := l.ApplyProgressiveTiers(msgs, tiers)
	if len(out) != 1 || out[0].Content[0].Text != body {
		t.Fatalf("want verbatim when MiniMax not configured, got %#v", out)
	}
}

func TestApplyProgressiveTiers_summarizeHTTPErrorKeepsVerbatim(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	l := NewLayer2(&cfg)
	body := strings.Repeat("word ", 120)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: body}}},
	}
	tiers := []CompressionTier{{Name: "tier-x", MsgRange: [2]int{0, 0}, TargetRatio: 0.3}}
	out := l.ApplyProgressiveTiers(msgs, tiers)
	if len(out) != 1 || out[0].Content[0].Text != body {
		t.Fatalf("expected verbatim on summarize error, got %#v", out)
	}
}

func TestApplyProgressiveTiers_validationFailKeepsVerbatim(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"}}]}`))
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	l := NewLayer2(&cfg)
	body := strings.Repeat("word ", 400)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: body}}},
	}
	tiers := []CompressionTier{{Name: "tier-y", MsgRange: [2]int{0, 0}, TargetRatio: 0.25}}
	out := l.ApplyProgressiveTiers(msgs, tiers)
	if len(out) != 1 || out[0].Content[0].Text != body {
		t.Fatalf("expected verbatim on validation failure, got %#v", out)
	}
}

// TestDetermineCompressionTiers_tier3 covers the compressible > 35 branch (lines 69-75).
func TestDetermineCompressionTiers_tier3(t *testing.T) {
	t.Parallel()
	// total 50, window 5 -> compressible 45 -> has tier-1 (0-19), tier-2 (20-34), tier-3 (35-44), window.
	tiers := DetermineCompressionTiers(50, 5)
	if len(tiers) < 4 {
		t.Fatalf("expected 4 tiers (1+2+3+window), got %d: %#v", len(tiers), tiers)
	}
	var hasTier3 bool
	for _, ti := range tiers {
		if ti.Name == "tier-3" {
			hasTier3 = true
		}
	}
	if !hasTier3 {
		t.Fatalf("expected tier-3 in output: %#v", tiers)
	}
}

// TestApplyProgressiveTiers_startBeyondEnd covers start >= len(messages) break (lines 103-104).
func TestApplyProgressiveTiers_startBeyondEnd(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "x"}}},
	}
	// Tier with start=5 > len(msgs)=1 -> break immediately.
	tiers := []CompressionTier{
		{Name: "t1", MsgRange: [2]int{5, 10}, TargetRatio: 0.3},
	}
	out := l.ApplyProgressiveTiers(msgs, tiers)
	// Should return empty since the break fires before adding anything.
	if len(out) != 0 {
		t.Fatalf("expected empty result when start >= len(msgs), got %d messages", len(out))
	}
}

// TestApplyProgressiveTiers_allAnchorsInTier covers the "all messages are anchors" path (lines 131-138).
func TestApplyProgressiveTiers_allAnchorsInTier(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	cfg := config.Defaults().Compression
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	l := NewLayer2(&cfg)

	// Edit tool_use messages are anchors - all messages in the tier are anchors.
	msgs := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "edit_file"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "write_file"}}},
	}
	tiers := []CompressionTier{
		{Name: "tier-anchors", MsgRange: [2]int{0, 1}, TargetRatio: 0.2},
	}
	out := l.ApplyProgressiveTiers(msgs, tiers)
	// All are anchors -> kept verbatim.
	if len(out) != 2 {
		t.Fatalf("expected 2 verbatim anchor messages, got %d", len(out))
	}
}

func TestApplyProgressiveTiers_summarizeSuccess(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	summaryText := "- alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, summaryText)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	l := NewLayer2(&cfg)
	body := strings.Repeat("word ", 300)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: body}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ack"}}},
	}
	tiers := []CompressionTier{{Name: "tier-z", MsgRange: [2]int{0, 1}, TargetRatio: 0.4}}
	out := l.ApplyProgressiveTiers(msgs, tiers)
	var sawSummary bool
	for _, m := range out {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "[tier-z summary") && strings.Contains(b.Text, summaryText) {
				sawSummary = true
			}
		}
	}
	if !sawSummary {
		t.Fatalf("expected tier summary message in %#v", out)
	}
}

func TestApplyProgressiveTiers_endBeyondSlice(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	l := NewLayer2(&cfg)
	msgs := []types.Message{
		{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "a"}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "b"}}},
	}
	tiers := []CompressionTier{
		{Name: "window", MsgRange: [2]int{0, 10}, TargetRatio: 1.0},
	}
	out := l.ApplyProgressiveTiers(msgs, tiers)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages after end clamping, got %d", len(out))
	}
}

// TestApplyProgressiveTiers_summarizeWithAnchors covers anchor re-insertion path (line 177).
// Some messages in a tier are anchors; the rest get summarized successfully.
func TestApplyProgressiveTiers_summarizeWithAnchors(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "test-key")
	summaryText := "- alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, summaryText)
	}))
	defer srv.Close()

	cfg := config.Defaults().Compression
	cfg.MiniMax.BaseURL = srv.URL
	cfg.MiniMax.APIKeyEnv = "MINIMAX_API_KEY"
	cfg.MiniMax.MaxRetries = 0
	l := NewLayer2(&cfg)

	msgs := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "edit_file"}}},
		{Index: 1, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("hello ", 100)}}},
		{Index: 2, Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("response ", 100)}}},
	}
	tiers := []CompressionTier{
		{Name: "tier-mixed", MsgRange: [2]int{0, 2}, TargetRatio: 0.3},
	}
	out := l.ApplyProgressiveTiers(msgs, tiers)
	// Should have the anchor msg + a summary msg.
	if len(out) < 2 {
		t.Fatalf("expected anchor + summary, got %d messages: %#v", len(out), out)
	}
	// The first output message should be the anchor (edit_file tool_use).
	foundAnchor := false
	for _, m := range out {
		for _, b := range m.Content {
			if b.Type == "tool_use" && b.ToolName == "edit_file" {
				foundAnchor = true
			}
		}
	}
	if !foundAnchor {
		t.Fatalf("anchor message missing from output: %#v", out)
	}
}

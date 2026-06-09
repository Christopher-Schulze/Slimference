package checkpoints

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestCaptureAndRestoreBest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	input := CaptureInput{
		Trigger: TriggerManual,
		Snapshot: analytics.AnalyticsSnapshot{
			TotalRequests:    4,
			SavedInputTokens: 1200,
		},
		RecentRequests: []types.RequestMetrics{{
			Timestamp:        time.Unix(10, 0),
			Provider:         types.Anthropic,
			InputTokensOrig:  20000,
			InputTokensComp:  15000,
			CompressionRatio: 0.75,
			Layers:           []int{1, 2},
			LatencyMs:        50,
		}},
		Logs: []sessions.LogEntry{{
			Timestamp: time.Unix(20, 0),
			Level:     "INFO",
			Message:   "compression complete",
		}},
		Decisions: []dbg.RequestSummary{{
			RequestID: "req-1",
			Timestamp: time.Unix(30, 0),
			Provider:  "anthropic",
			Model:     "claude-3-7-sonnet",
			Tokens:    dbg.TokenCounts{Saved: 500, Ratio: 0.7},
		}},
		Event: types.AnalyticsEvent{
			Provider:         types.Anthropic,
			Model:            "claude-3-7-sonnet",
			InputTokensOrig:  22000,
			InputTokensComp:  16000,
			OutputTokens:     1000,
			CompressionRatio: 0.73,
		},
	}
	cp, err := Capture(dir, input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cp.Body, "Recent decision summaries") {
		t.Fatalf("checkpoint body missing decisions: %q", cp.Body)
	}

	got, err := RestoreBest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != cp.ID {
		t.Fatalf("restore id=%q want %q", got.ID, cp.ID)
	}

	stats, err := Snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 1 || stats.Restores != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestMaybeCapture_AutoTriggerAndCooldown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	input := CaptureInput{
		Snapshot: analytics.AnalyticsSnapshot{TotalRequests: 2},
		Event: types.AnalyticsEvent{
			Type:             types.EventRequestProcessed,
			Provider:         types.OpenAI,
			Model:            "gpt-4o",
			InputTokensOrig:  110000,
			InputTokensComp:  100000,
			CompressionRatio: 0.91,
		},
	}
	if _, ok, err := MaybeCapture(dir, input); err != nil || !ok {
		t.Fatalf("first capture ok=%v err=%v", ok, err)
	}
	if _, ok, err := MaybeCapture(dir, input); err != nil || ok {
		t.Fatalf("cooldown should skip: ok=%v err=%v", ok, err)
	}
}

func TestRestoreByID_NotFound(t *testing.T) {
	t.Parallel()

	_, err := RestoreByID(t.TempDir(), "missing")
	if !os.IsNotExist(err) {
		t.Fatalf("err=%v", err)
	}
}

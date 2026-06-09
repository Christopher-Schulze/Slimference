package proxy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestMaybeCaptureCheckpoint_ErrorPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	blocker := filepath.Join(home, ".slimference")
	if err := os.WriteFile(blocker, []byte("block"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New(config.Defaults())
	p.maybeCaptureCheckpoint(types.AnalyticsEvent{
		Type:             types.EventOverflowRetry,
		Provider:         types.Anthropic,
		Model:            "claude-3-7-sonnet",
		InputTokensOrig:  120000,
		InputTokensComp:  110000,
		CompressionRatio: 0.92,
	})
}

func TestMaybeCaptureCheckpoint_NoHomePath(t *testing.T) {
	t.Setenv("HOME", "")

	cfg := config.Defaults()
	cfg.Analytics.LogDir = ""
	p := New(cfg)
	p.maybeCaptureCheckpoint(types.AnalyticsEvent{Type: types.EventOverflowRetry})
}

func TestProxy_FlushCaches_ReadCacheClearError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	blocker := filepath.Join(home, ".slimference")
	if err := os.WriteFile(blocker, []byte("block"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New(config.Defaults())
	p.FlushCaches()
}

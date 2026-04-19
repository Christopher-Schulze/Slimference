package proxy

import (
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestMaybeCaptureCheckpoint_NilAndSuccessPaths(t *testing.T) {
	var nilProxy *Proxy
	nilProxy.maybeCaptureCheckpoint(types.AnalyticsEvent{Type: types.EventOverflowRetry})

	home := t.TempDir()
	t.Setenv("HOME", home)
	p := New(config.Defaults())
	p.maybeCaptureCheckpoint(types.AnalyticsEvent{
		Type:             types.EventOverflowRetry,
		Provider:         types.Anthropic,
		Model:            "claude-3-7-sonnet",
		InputTokensOrig:  100000,
		InputTokensComp:  90000,
		CompressionRatio: 0.9,
	})
	status := p.adminStatusSnapshot()
	if status.Checkpoints.Captures == 0 {
		t.Fatalf("checkpoint status=%+v", status.Checkpoints)
	}
}

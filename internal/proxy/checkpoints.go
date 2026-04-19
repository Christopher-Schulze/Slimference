package proxy

import (
	"log/slog"
	"os"

	"github.com/slimference/slimference/internal/checkpoints"
	"github.com/slimference/slimference/internal/types"
)

func (p *Proxy) maybeCaptureCheckpoint(event types.AnalyticsEvent) {
	if p == nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	input := checkpoints.CaptureInput{
		Snapshot:       p.analytics.Snapshot(),
		RecentRequests: p.analytics.RecentRequests(10),
		Event:          event,
	}
	if p.sessionLogger != nil {
		input.Logs = p.sessionLogger.Recent(10)
	}
	if p.debugRecorder != nil {
		input.Decisions = p.debugRecorder.Last(3, false)
	}
	if _, ok, err := checkpoints.MaybeCapture(checkpoints.DefaultDir(home), input); err != nil {
		slog.Warn("checkpoint capture failed", "error", err)
	} else if ok {
		slog.Info("checkpoint captured", "trigger", input.Trigger, "provider", event.Provider, "model", event.Model)
	}
}

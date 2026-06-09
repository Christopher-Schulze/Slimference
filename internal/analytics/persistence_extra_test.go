package analytics

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestPersister_WriteEvent_MarshalErrorNaN(t *testing.T) {
	p, err := NewPersister(t.TempDir())
	if err != nil {
		t.Fatalf("NewPersister: %v", err)
	}
	defer p.Close()

	err = p.WriteEvent(types.AnalyticsEvent{
		Type:             types.EventRequestProcessed,
		Timestamp:        time.Now(),
		CompressionRatio: math.NaN(),
	})
	if err == nil || !strings.Contains(err.Error(), "marshal event") {
		t.Fatalf("expected marshal event error, got %v", err)
	}
}

func TestPersister_WriteSnapshot_MarshalErrorNaN(t *testing.T) {
	p, err := NewPersister(t.TempDir())
	if err != nil {
		t.Fatalf("NewPersister: %v", err)
	}
	defer p.Close()

	err = p.WriteSnapshot(AnalyticsSnapshot{
		SessionStart:       time.Now(),
		LatencyAnthropicMs: math.NaN(),
	})
	if err == nil || !strings.Contains(err.Error(), "marshal snapshot") {
		t.Fatalf("expected marshal snapshot error, got %v", err)
	}
}

func TestPersister_writeLine_MarshalEnvelopeError(t *testing.T) {
	p, err := NewPersister(t.TempDir())
	if err != nil {
		t.Fatalf("NewPersister: %v", err)
	}
	defer p.Close()

	err = p.writeLine("analytics_event", json.RawMessage("{"))
	if err == nil || !strings.Contains(err.Error(), "marshal envelope") {
		t.Fatalf("expected marshal envelope error, got %v", err)
	}
}

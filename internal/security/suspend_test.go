package security

import (
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func detectorForSuspendTest(t *testing.T) *Detector {
	t.Helper()
	return NewDetector("redact", nil, nil)
}

func TestSuspendUntil_Clamps(t *testing.T) {
	d := detectorForSuspendTest(t)
	// Ask for 24h; must be clamped to MaxSuspendDuration.
	effective := d.SuspendUntil(time.Now().Add(24 * time.Hour))
	max := time.Now().Add(MaxSuspendDuration).Add(time.Second)
	min := time.Now().Add(MaxSuspendDuration).Add(-time.Second)
	if effective.Before(min) || effective.After(max) {
		t.Fatalf("effective deadline %v not within clamp window [%v, %v]",
			effective, min, max)
	}
}

func TestSuspendUntil_PastClears(t *testing.T) {
	d := detectorForSuspendTest(t)
	d.SuspendUntil(time.Now().Add(time.Minute))
	active, _ := d.SuspendState()
	if !active {
		t.Fatal("expected active after 1-min suspend")
	}
	d.SuspendUntil(time.Now().Add(-time.Hour))
	active, _ = d.SuspendState()
	if active {
		t.Fatal("past time did not clear suspension")
	}
}

func TestSuspendUntil_ZeroClears(t *testing.T) {
	d := detectorForSuspendTest(t)
	d.SuspendUntil(time.Now().Add(time.Minute))
	d.SuspendUntil(time.Time{})
	active, _ := d.SuspendState()
	if active {
		t.Fatal("zero Time did not clear suspension")
	}
}

func TestSuspendState_ExpiresLazy(t *testing.T) {
	d := detectorForSuspendTest(t)
	// 10ms suspension - wait it out, then read state; must auto-clear.
	d.SuspendUntil(time.Now().Add(10 * time.Millisecond))
	time.Sleep(50 * time.Millisecond)
	active, _ := d.SuspendState()
	if active {
		t.Fatal("expired suspension still reported active")
	}
}

func TestScanMessages_HonoursSuspension(t *testing.T) {
	d := NewDetector("redact", nil, nil)
	msgs := []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{{
			Type: "text",
			Text: "AKIAIOSFODNN7EXAMPLE is a fake AWS key",
		}},
	}}

	// Baseline: redaction active, the text is rewritten.
	out, detections, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("baseline scan err: %v", err)
	}
	if len(detections) == 0 {
		t.Fatal("expected at least one detection under redact mode")
	}
	if out[0].Content[0].Text == msgs[0].Content[0].Text {
		t.Fatal("expected text mutated under redact mode")
	}

	// Suspend and rescan: original bytes must pass through.
	d.SuspendUntil(time.Now().Add(5 * time.Minute))
	out, detections, err = d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("suspended scan err: %v", err)
	}
	if len(detections) != 0 {
		t.Fatalf("suspended scan produced %d detections; want 0", len(detections))
	}
	if out[0].Content[0].Text != msgs[0].Content[0].Text {
		t.Fatal("suspended scan still mutated text")
	}
}

func TestMode_Exposed(t *testing.T) {
	d := NewDetector("warn", nil, nil)
	if got := d.Mode(); got != "warn" {
		t.Fatalf("mode = %q, want warn", got)
	}
}

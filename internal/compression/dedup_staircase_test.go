package compression

import (
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
)

func TestResolveDedupThreshold_StaircaseSteps(t *testing.T) {
	cfg := &config.CompressionConfig{
		DedupSimilarityThreshold: 0.85,
		Tuning: config.TuningConfig{
			DedupStaircase: []config.StaircaseStep{
				{MsgCountLE: 10, Threshold: 0.88},
				{MsgCountLE: 20, Threshold: 0.85},
				{MsgCountLE: 40, Threshold: 0.82},
				{MsgCountLE: 1_000_000, Threshold: 0.78},
			},
		},
	}
	c := NewDeterministicCompressor(cfg)

	cases := []struct {
		msgs int
		want float64
	}{
		{0, 0.88},
		{5, 0.88},
		{10, 0.88},
		{11, 0.85},
		{20, 0.85},
		{21, 0.82},
		{40, 0.82},
		{41, 0.78},
		{1000, 0.78},
	}
	for _, tc := range cases {
		got := c.resolveDedupThreshold(tc.msgs)
		if got != tc.want {
			t.Errorf("msgs=%d: threshold = %v, want %v", tc.msgs, got, tc.want)
		}
	}
}

func TestResolveDedupThreshold_EmptyStaircaseUsesScalar(t *testing.T) {
	cfg := &config.CompressionConfig{
		DedupSimilarityThreshold: 0.73,
		Tuning:                   config.TuningConfig{DedupStaircase: nil},
	}
	c := NewDeterministicCompressor(cfg)
	if got := c.resolveDedupThreshold(500); got != 0.73 {
		t.Fatalf("empty staircase: got %v, want 0.73 (scalar fallback)", got)
	}
}

func TestResolveDedupThreshold_InvalidStepFallsBack(t *testing.T) {
	cfg := &config.CompressionConfig{
		DedupSimilarityThreshold: 0.80,
		Tuning: config.TuningConfig{
			DedupStaircase: []config.StaircaseStep{
				{MsgCountLE: 10, Threshold: 0},   // zero: invalid
				{MsgCountLE: 20, Threshold: 1.5}, // > 1: invalid
				{MsgCountLE: 30, Threshold: 0.75},
			},
		},
	}
	c := NewDeterministicCompressor(cfg)
	// First step has invalid threshold -> falls back to scalar.
	if got := c.resolveDedupThreshold(5); got != 0.80 {
		t.Errorf("invalid zero step: got %v, want scalar 0.80", got)
	}
	if got := c.resolveDedupThreshold(15); got != 0.80 {
		t.Errorf("invalid >1 step: got %v, want scalar 0.80", got)
	}
	// Valid step still resolves.
	if got := c.resolveDedupThreshold(25); got != 0.75 {
		t.Errorf("valid step: got %v, want 0.75", got)
	}
	// Above all steps: scalar fallback.
	if got := c.resolveDedupThreshold(50); got != 0.80 {
		t.Errorf("beyond staircase: got %v, want scalar 0.80", got)
	}
}

func TestT53_DefaultStaircasePresent(t *testing.T) {
	// Contract: built-in defaults include the staircase so new installations
	// get the adaptive behaviour without having to configure it explicitly.
	cfg := config.Defaults()
	if len(cfg.Compression.Tuning.DedupStaircase) == 0 {
		t.Fatal("T53 contract broken: default DedupStaircase must not be empty")
	}
	// First step must be higher than the last (monotone downward).
	steps := cfg.Compression.Tuning.DedupStaircase
	if steps[0].Threshold <= steps[len(steps)-1].Threshold {
		t.Fatalf("staircase not monotone: first=%v last=%v",
			steps[0].Threshold, steps[len(steps)-1].Threshold)
	}
}

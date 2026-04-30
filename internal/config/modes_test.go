package config

import (
	"strings"
	"testing"
)

// TestApplyL2OperatingMode_emptyIsNoop keeps explicit fields intact when no
// mode is configured.
func TestApplyL2OperatingMode_emptyIsNoop(t *testing.T) {
	t.Parallel()
	s := &SummaryConfig{TargetRatio: 0.42, MinRatio: 0.01, MaxRatio: 0.9, Strict: true}
	if err := ApplyL2OperatingMode(s, ""); err != nil {
		t.Fatalf("empty mode must not error: %v", err)
	}
	if s.TargetRatio != 0.42 || s.MinRatio != 0.01 || s.MaxRatio != 0.9 || !s.Strict {
		t.Fatalf("empty mode must not mutate: %+v", s)
	}
}

// TestApplyL2OperatingMode_nilSummarySafe does not crash on a nil pointer.
func TestApplyL2OperatingMode_nilSummarySafe(t *testing.T) {
	t.Parallel()
	if err := ApplyL2OperatingMode(nil, ModeStrict); err != nil {
		t.Fatalf("nil summary must not error: %v", err)
	}
}

// TestApplyL2OperatingMode_unknownRejected returns a descriptive error.
func TestApplyL2OperatingMode_unknownRejected(t *testing.T) {
	t.Parallel()
	s := &SummaryConfig{}
	err := ApplyL2OperatingMode(s, "turbo")
	if err == nil {
		t.Fatal("unknown mode must error")
	}
	if !strings.Contains(err.Error(), "strict|balanced|fast") {
		t.Fatalf("error must advertise valid modes, got %v", err)
	}
}

// TestApplyL2OperatingMode_profiles verifies each profile's numeric shape.
func TestApplyL2OperatingMode_profiles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode       string
		wantStrict bool
		wantTarget float64
		wantMax    float64
		wantMin    float64
	}{
		{ModeStrict, true, 0.15, 0.30, 0.05},
		{ModeBalanced, true, 0.20, 0.40, 0.05},
		{ModeFast, false, 0.30, 0.50, 0.10},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			s := &SummaryConfig{}
			if err := ApplyL2OperatingMode(s, tc.mode); err != nil {
				t.Fatal(err)
			}
			if s.Strict != tc.wantStrict || s.TargetRatio != tc.wantTarget ||
				s.MaxRatio != tc.wantMax || s.MinRatio != tc.wantMin {
				t.Fatalf("%s profile mismatch: %+v", tc.mode, s)
			}
			if s.Mode != tc.mode {
				t.Fatalf("mode must be normalised to %q, got %q", tc.mode, s.Mode)
			}
		})
	}
}

// TestApplyL2OperatingMode_explicitOverrides keep positive operator values
// when they compete with the profile.
func TestApplyL2OperatingMode_explicitOverrides(t *testing.T) {
	t.Parallel()
	s := &SummaryConfig{TargetRatio: 0.42, MaxRatio: 0.88, MinRatio: 0.02}
	if err := ApplyL2OperatingMode(s, ModeStrict); err != nil {
		t.Fatal(err)
	}
	if s.TargetRatio != 0.42 || s.MaxRatio != 0.88 || s.MinRatio != 0.02 {
		t.Fatalf("explicit positive overrides must survive, got %+v", s)
	}
	// Strict is an unambiguous boolean and tracks the profile.
	if !s.Strict {
		t.Fatal("strict must track the profile")
	}
}

// TestApplyL2OperatingMode_caseInsensitive accepts mixed case.
func TestApplyL2OperatingMode_caseInsensitive(t *testing.T) {
	t.Parallel()
	s := &SummaryConfig{}
	if err := ApplyL2OperatingMode(s, "  FAST "); err != nil {
		t.Fatalf("case/whitespace normalisation must work: %v", err)
	}
	if s.Mode != ModeFast {
		t.Fatalf("mode normalisation: %q", s.Mode)
	}
}

// TestDefaultsApplyBalancedProfile asserts Defaults() returns a ready-to-use
// SummaryConfig with the balanced profile already materialised.
func TestDefaultsApplyBalancedProfile(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	if cfg.Compression.Summary.Mode != ModeBalanced {
		t.Fatalf("default mode must be balanced, got %q", cfg.Compression.Summary.Mode)
	}
	if cfg.Compression.Summary.TargetRatio == 0 {
		t.Fatal("balanced profile must materialise TargetRatio")
	}
}

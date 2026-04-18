package config

import (
	"fmt"
	"strings"
)

// Operating modes for Layer 2 summarization (T36).
//
// The three modes form coherent bundles that resolve the tension between
// correctness, aggressiveness and latency. Selecting a mode sets Strict,
// TargetRatio, MaxRatio and MinRatio to a known-good set; explicit
// individual knobs in the TOML still win via override order below.
const (
	ModeStrict   = "strict"
	ModeBalanced = "balanced"
	ModeFast     = "fast"
)

// modeProfile is the canonical profile applied by ApplyL2OperatingMode. The
// values intentionally sit outside SummaryConfig so the override merge is
// trivially auditable.
type modeProfile struct {
	Strict      bool
	TargetRatio float64
	MaxRatio    float64
	MinRatio    float64
}

var modeProfiles = map[string]modeProfile{
	ModeStrict: {
		// Correctness > aggressiveness > latency. Tight validator, low
		// target so the summariser produces compact and well-anchored
		// output, even at the cost of more retries.
		Strict:      true,
		TargetRatio: 0.15,
		MaxRatio:    0.30,
		MinRatio:    0.05,
	},
	ModeBalanced: {
		// Default. Current shipping behaviour. Correctness > latency >
		// aggressiveness.
		Strict:      true,
		TargetRatio: 0.20,
		MaxRatio:    0.40,
		MinRatio:    0.05,
	},
	ModeFast: {
		// Latency > correctness > aggressiveness. Looser validator and a
		// larger target ratio to accept summaries on the first pass.
		Strict:      false,
		TargetRatio: 0.30,
		MaxRatio:    0.50,
		MinRatio:    0.10,
	},
}

// ApplyL2OperatingMode mutates s in place with the selected mode's profile,
// then re-applies any non-zero overrides already present in s so explicit
// TOML/env knobs continue to win. An unknown, non-empty mode is a
// configuration error and returned to the caller; empty mode is a no-op.
//
// Precedence: mode profile < explicit TOML overrides < env overrides
// (env overrides are applied by applyEnvOverrides later in the load path).
func ApplyL2OperatingMode(s *SummaryConfig, mode string) error {
	if s == nil {
		return nil
	}
	trimmed := strings.ToLower(strings.TrimSpace(mode))
	if trimmed == "" {
		return nil
	}
	profile, ok := modeProfiles[trimmed]
	if !ok {
		return fmt.Errorf("compression.summary.mode must be strict|balanced|fast, got %q", mode)
	}
	// Snapshot any non-zero overrides present in s before the profile wins.
	overrideTarget := s.TargetRatio
	overrideMax := s.MaxRatio
	overrideMin := s.MinRatio
	// "Strict" is a bool; we need an explicit sentinel to know whether the
	// operator overrode it. Absent a sentinel we honour the mode's default
	// and document it. To keep this simple, an explicit strict=false under
	// a strict mode still loses to the profile. Operators who need that
	// combination can simply set mode="fast" or omit mode entirely.
	s.Strict = profile.Strict
	s.TargetRatio = profile.TargetRatio
	s.MaxRatio = profile.MaxRatio
	s.MinRatio = profile.MinRatio
	// Re-apply numeric overrides when they are strictly positive (i.e. the
	// operator intentionally set them). Zero means "unset".
	if overrideTarget > 0 {
		s.TargetRatio = overrideTarget
	}
	if overrideMax > 0 {
		s.MaxRatio = overrideMax
	}
	if overrideMin > 0 {
		s.MinRatio = overrideMin
	}
	s.Mode = trimmed
	return nil
}

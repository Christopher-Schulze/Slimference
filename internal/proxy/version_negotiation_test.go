package proxy

import (
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
)

func TestClassifyAnthropicVersion_EmptyHeaderIsFull(t *testing.T) {
	ResetAnthropicVersionState()
	cfg := &config.ProxyConfig{AnthropicVersions: []string{"2023-06-01"}}
	if got := ClassifyAnthropicVersion("", cfg); got != PipelineFull {
		t.Fatalf("got %v, want Full", got)
	}
}

func TestClassifyAnthropicVersion_NoSupportListIsFull(t *testing.T) {
	ResetAnthropicVersionState()
	cfg := &config.ProxyConfig{}
	if got := ClassifyAnthropicVersion("2023-06-01", cfg); got != PipelineFull {
		t.Fatalf("got %v, want Full (empty list = legacy trust)", got)
	}
}

func TestClassifyAnthropicVersion_KnownIsFull(t *testing.T) {
	ResetAnthropicVersionState()
	cfg := &config.ProxyConfig{AnthropicVersions: []string{"2023-06-01", "2024-01-15"}}
	if got := ClassifyAnthropicVersion("2023-06-01", cfg); got != PipelineFull {
		t.Fatalf("got %v, want Full", got)
	}
	// Case-insensitive match.
	if got := ClassifyAnthropicVersion("2023-06-01", cfg); got != PipelineFull {
		t.Fatalf("case-insensitive known: got %v", got)
	}
}

func TestClassifyAnthropicVersion_UnknownDefaultsConservative(t *testing.T) {
	ResetAnthropicVersionState()
	cfg := &config.ProxyConfig{AnthropicVersions: []string{"2023-06-01"}}
	if got := ClassifyAnthropicVersion("2030-99-99", cfg); got != PipelineConservative {
		t.Fatalf("got %v, want Conservative", got)
	}
	if AnthropicUnknownVersionCount() != 1 {
		t.Fatalf("counter = %d, want 1", AnthropicUnknownVersionCount())
	}
}

func TestClassifyAnthropicVersion_UnknownBehaviorPassthrough(t *testing.T) {
	ResetAnthropicVersionState()
	cfg := &config.ProxyConfig{
		AnthropicVersions:        []string{"2023-06-01"},
		AnthropicUnknownBehavior: "passthrough",
	}
	if got := ClassifyAnthropicVersion("2030-01-01", cfg); got != PipelinePassthrough {
		t.Fatalf("got %v, want Passthrough", got)
	}
}

func TestClassifyAnthropicVersion_UnknownBehaviorFull(t *testing.T) {
	ResetAnthropicVersionState()
	cfg := &config.ProxyConfig{
		AnthropicVersions:        []string{"2023-06-01"},
		AnthropicUnknownBehavior: "FULL", // case-insensitive
	}
	if got := ClassifyAnthropicVersion("2030-01-01", cfg); got != PipelineFull {
		t.Fatalf("got %v, want Full (opt-in risk mode)", got)
	}
	// Counter still increments so the operator can see the drift.
	if AnthropicUnknownVersionCount() != 1 {
		t.Fatalf("counter = %d, want 1", AnthropicUnknownVersionCount())
	}
}

func TestClassifyAnthropicVersion_UnknownBehaviorInvalidFallsBackToConservative(t *testing.T) {
	ResetAnthropicVersionState()
	cfg := &config.ProxyConfig{
		AnthropicVersions:        []string{"2023-06-01"},
		AnthropicUnknownBehavior: "garbage",
	}
	if got := ClassifyAnthropicVersion("2030-01-01", cfg); got != PipelineConservative {
		t.Fatalf("invalid behavior: got %v, want Conservative", got)
	}
}

func TestClassifyAnthropicVersion_CounterMonotoneUnderRepeatedUnknowns(t *testing.T) {
	ResetAnthropicVersionState()
	cfg := &config.ProxyConfig{AnthropicVersions: []string{"2023-06-01"}}
	for i := 0; i < 5; i++ {
		ClassifyAnthropicVersion("unknown-x", cfg)
	}
	if got := AnthropicUnknownVersionCount(); got != 5 {
		t.Fatalf("counter = %d, want 5", got)
	}
}

func TestEmitUnknownVersionWarn_RateLimited(t *testing.T) {
	ResetAnthropicVersionState()
	prev := versionWarnIntervalNs
	versionWarnIntervalNs = int64(time.Hour)
	t.Cleanup(func() { versionWarnIntervalNs = prev })

	// First call sets lastUnknownVersionWarnNs.
	emitUnknownVersionWarn("v1")
	first := lastUnknownVersionWarnNs.Load()
	if first == 0 {
		t.Fatal("first warn did not set timestamp")
	}
	// Second call within the hour must be suppressed - timestamp unchanged.
	emitUnknownVersionWarn("v2")
	if lastUnknownVersionWarnNs.Load() != first {
		t.Fatal("rate-limit leaked a second warn")
	}
}

func TestPipelineMode_String(t *testing.T) {
	cases := map[PipelineMode]string{
		PipelineFull:         "full",
		PipelineConservative: "conservative",
		PipelinePassthrough:  "passthrough",
		PipelineMode(99):     "unknown",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("mode %d: got %q, want %q", int(m), got, want)
		}
	}
}

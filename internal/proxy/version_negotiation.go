package proxy

import (
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/config"
)

// PipelineMode captures the compression pipeline behaviour selected per
// request by the version-negotiation logic (T62).
type PipelineMode int

const (
	// PipelineFull runs every enabled layer normally.
	PipelineFull PipelineMode = iota
	// PipelineConservative skips Layer 1 - the byte stream is
	// forwarded as-is so we cannot mis-compress a format we do not know.
	// Layer 3 response cache still applies because it operates on request
	// hashes, not semantic content.
	PipelineConservative
	// PipelinePassthrough runs no compression at all; equivalent to a
	// transparent proxy. Used when users explicitly opt out of any risk.
	PipelinePassthrough
)

func (m PipelineMode) String() string {
	switch m {
	case PipelineFull:
		return "full"
	case PipelineConservative:
		return "conservative"
	case PipelinePassthrough:
		return "passthrough"
	}
	return "unknown"
}

// anthropicUnknownVersionSeen counts total requests that carried an
// unrecognised anthropic-version header. Exposed on /admin/status.
var anthropicUnknownVersionSeen atomic.Int64

// AnthropicUnknownVersionCount returns the cumulative unknown-version count.
func AnthropicUnknownVersionCount() int64 {
	return anthropicUnknownVersionSeen.Load()
}

// lastUnknownVersionWarnNs is updated atomically to rate-limit the slog
// warning to at most one per minute per process (not per version value,
// so pathological clients cannot flood logs).
var lastUnknownVersionWarnNs atomic.Int64

// versionWarnIntervalNs mirrors analyticsWarnIntervalNs and is test-tunable.
var versionWarnIntervalNs int64 = int64(time.Minute)

// ResetAnthropicVersionState zeroes all version-negotiation atomics. Tests
// call this to isolate each case.
func ResetAnthropicVersionState() {
	anthropicUnknownVersionSeen.Store(0)
	lastUnknownVersionWarnNs.Store(0)
}

// ClassifyAnthropicVersion returns the PipelineMode to use for a request
// carrying the given anthropic-version header. An empty header or missing
// config.AnthropicVersions list is treated as trusted (PipelineFull) so
// legacy deployments keep byte-equal behaviour. Unknown versions trigger a
// rate-limited slog.Warn and increment the unknown-seen counter.
func ClassifyAnthropicVersion(header string, cfg *config.ProxyConfig) PipelineMode {
	header = strings.TrimSpace(header)

	// Empty header: we only inspect the field when present. Most SDKs send
	// it, but absence is not by itself a risk signal.
	if header == "" {
		return PipelineFull
	}

	// No supported-list configured: full trust (backwards compatible).
	if cfg == nil || len(cfg.AnthropicVersions) == 0 {
		return PipelineFull
	}

	for _, v := range cfg.AnthropicVersions {
		if strings.EqualFold(v, header) {
			return PipelineFull
		}
	}

	// Unknown version: update telemetry + decide via configured behaviour.
	anthropicUnknownVersionSeen.Add(1)
	emitUnknownVersionWarn(header)

	behavior := PipelineConservative
	switch strings.ToLower(strings.TrimSpace(cfg.AnthropicUnknownBehavior)) {
	case "full":
		behavior = PipelineFull
	case "passthrough":
		behavior = PipelinePassthrough
	case "", "conservative":
		behavior = PipelineConservative
	}
	return behavior
}

func emitUnknownVersionWarn(header string) {
	now := time.Now().UnixNano()
	last := lastUnknownVersionWarnNs.Load()
	if now-last < versionWarnIntervalNs {
		return
	}
	lastUnknownVersionWarnNs.Store(now)
	slog.Warn("anthropic_version_unknown",
		"event", "anthropic_version_unknown",
		"header", header,
		"seen_total", anthropicUnknownVersionSeen.Load(),
	)
}

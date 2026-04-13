package sessions

import (
	"fmt"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/analytics"
)

// SessionStats is a consolidated summary of a completed or in-progress session.
type SessionStats struct {
	SessionStart  time.Time
	Duration      time.Duration
	TotalRequests int
	CacheHits     int

	// Token counters.
	InputTokensOrig int
	InputTokensComp int
	TokensSaved     int
	OutputTokens    int

	// Derived ratios.
	CompressionRatio float64 // CompressedTokens / OriginalTokens; lower is better
	CompressionPct   float64 // percentage of tokens saved: (1 - ratio) * 100

	// Session value metrics.
	ExtraMessages         int     // estimated additional requests funded by saved tokens
	AvgTTFTImprovementSec float64 // avg TTFT improvement per request in seconds
}

// defaultPrefillSpeed is the assumed upstream prefill throughput in tokens/second
// used when estimating TTFT improvement from saved tokens.
// Based on typical Anthropic Claude 3.5 Sonnet observed throughput.
const defaultPrefillSpeed = 3000

// AggregateFromSnapshots folds a slice of AnalyticsSnapshot values into a single
// SessionStats summary. The earliest SessionStart across all snapshots determines the
// session start time; duration is measured to the latest SessionStart (each snapshot
// carries its own start, so the last one's start + session elapsed approximates end).
// Token counters are taken from the last (most recent) snapshot since they are cumulative.
func AggregateFromSnapshots(snapshots []analytics.AnalyticsSnapshot) SessionStats {
	if len(snapshots) == 0 {
		return SessionStats{}
	}

	// Find earliest and latest SessionStart across all snapshots.
	earliest := snapshots[0].SessionStart
	latest := snapshots[0].SessionStart
	for _, s := range snapshots[1:] {
		if s.SessionStart.Before(earliest) {
			earliest = s.SessionStart
		}
		if s.SessionStart.After(latest) {
			latest = s.SessionStart
		}
	}

	// Use the most recent snapshot for cumulative counters.
	last := snapshots[len(snapshots)-1]

	inputTokensOrig := last.TotalInputTokens
	tokensSaved := last.SavedInputTokens
	inputTokensComp := inputTokensOrig - tokensSaved
	if inputTokensComp < 0 {
		inputTokensComp = 0
	}

	var compressionRatio float64
	if inputTokensOrig > 0 {
		compressionRatio = float64(inputTokensComp) / float64(inputTokensOrig)
	} else {
		compressionRatio = 1.0
	}
	compressionPct := (1.0 - compressionRatio) * 100.0

	// Estimate extra messages: saved tokens / avg compressed tokens per request.
	var extraMessages int
	if last.TotalRequests > 0 && inputTokensComp > 0 {
		avgComp := inputTokensComp / last.TotalRequests
		if avgComp > 0 {
			extraMessages = tokensSaved / avgComp
		}
	}

	// Estimate avg TTFT improvement: avg saved tokens per request / prefill speed.
	var avgTTFT float64
	if last.TotalRequests > 0 && tokensSaved > 0 {
		avgSaved := float64(tokensSaved) / float64(last.TotalRequests)
		avgTTFT = avgSaved / float64(defaultPrefillSpeed)
	}

	// Duration: time span across the snapshots. If all share the same SessionStart
	// (single session), use the gap between earliest and latest snapshot's collection.
	// Fall back to 0 when only one snapshot exists.
	duration := latest.Sub(earliest)

	return SessionStats{
		SessionStart:          earliest,
		Duration:              duration,
		TotalRequests:         last.TotalRequests,
		CacheHits:             last.CacheHits,
		InputTokensOrig:       inputTokensOrig,
		InputTokensComp:       inputTokensComp,
		TokensSaved:           tokensSaved,
		OutputTokens:          last.TotalOutputTokens,
		CompressionRatio:      compressionRatio,
		CompressionPct:        compressionPct,
		ExtraMessages:         extraMessages,
		AvgTTFTImprovementSec: avgTTFT,
	}
}

// FormatStatsTable renders stats as a plain-text table suitable for stdout.
// No external dependencies are used; output is aligned with spaces.
func FormatStatsTable(stats SessionStats) string {
	var sb strings.Builder

	sep := strings.Repeat("-", 50)
	line := func(label, value string) {
		sb.WriteString(fmt.Sprintf("  %-32s %s\n", label, value))
	}

	sb.WriteString(sep + "\n")
	sb.WriteString("  Slimference Session Stats\n")
	sb.WriteString(sep + "\n")

	// Session info.
	sb.WriteString("\n")
	line("Session start:", stats.SessionStart.Format("2006-01-02 15:04:05"))
	line("Session duration:", formatDuration(stats.Duration))
	line("Total requests:", fmt.Sprintf("%d", stats.TotalRequests))
	line("Cache hits:", fmt.Sprintf("%d", stats.CacheHits))

	// Token counts.
	sb.WriteString("\n")
	sb.WriteString("  Token Compression\n")
	sb.WriteString(strings.Repeat("-", 50) + "\n")
	line("Input tokens (original):", formatInt(stats.InputTokensOrig))
	line("Input tokens (compressed):", formatInt(stats.InputTokensComp))
	line("Tokens saved:", formatInt(stats.TokensSaved))
	line("Output tokens:", formatInt(stats.OutputTokens))
	line("Compression ratio:", fmt.Sprintf("%.1f%%", stats.CompressionPct))

	// Value metrics.
	sb.WriteString("\n")
	sb.WriteString("  Session Value\n")
	sb.WriteString(strings.Repeat("-", 50) + "\n")
	line("Extra messages gained:", fmt.Sprintf("~%d", stats.ExtraMessages))
	line("Avg TTFT improvement:", fmt.Sprintf("%.2fs per request", stats.AvgTTFTImprovementSec))

	sb.WriteString("\n" + sep + "\n")
	return sb.String()
}

// formatInt formats an integer with thousands separators.
func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	offset := len(s) % 3
	for i, c := range []byte(s) {
		if i > 0 && (i-offset)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, c)
	}
	return string(result)
}

// formatDuration renders a duration as a human-readable string without sub-second precision.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}

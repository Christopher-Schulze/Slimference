package sessions

import (
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
)

// TestAggregateFromSnapshots_Empty verifies that an empty slice returns zero stats.
func TestAggregateFromSnapshots_Empty(t *testing.T) {
	t.Parallel()
	stats := AggregateFromSnapshots(nil)
	if stats.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d, want 0 for nil input", stats.TotalRequests)
	}
	if stats.TokensSaved != 0 {
		t.Errorf("TokensSaved = %d, want 0 for nil input", stats.TokensSaved)
	}
}

// TestAggregateFromSnapshots_Single verifies aggregation of a single snapshot.
func TestAggregateFromSnapshots_Single(t *testing.T) {
	t.Parallel()
	now := time.Now()
	snaps := []analytics.AnalyticsSnapshot{
		{
			SessionStart:    now,
			TotalRequests:   50,
			TotalInputTokens: 500000,
			SavedInputTokens: 300000,
			TotalOutputTokens: 50000,
			CacheHits:       10,
		},
	}

	stats := AggregateFromSnapshots(snaps)

	if stats.TotalRequests != 50 {
		t.Errorf("TotalRequests = %d, want 50", stats.TotalRequests)
	}
	if stats.InputTokensOrig != 500000 {
		t.Errorf("InputTokensOrig = %d, want 500000", stats.InputTokensOrig)
	}
	if stats.TokensSaved != 300000 {
		t.Errorf("TokensSaved = %d, want 300000", stats.TokensSaved)
	}
	if stats.InputTokensComp != 200000 {
		t.Errorf("InputTokensComp = %d, want 200000", stats.InputTokensComp)
	}
	if stats.CacheHits != 10 {
		t.Errorf("CacheHits = %d, want 10", stats.CacheHits)
	}
}

// TestAggregateFromSnapshots_CompressionPct verifies the compression percentage calculation.
func TestAggregateFromSnapshots_CompressionPct(t *testing.T) {
	t.Parallel()
	snaps := []analytics.AnalyticsSnapshot{
		{
			SessionStart:    time.Now(),
			TotalInputTokens: 1000,
			SavedInputTokens: 600,
			TotalRequests:   10,
		},
	}

	stats := AggregateFromSnapshots(snaps)

	// 600 saved out of 1000 = 60% compression.
	want := 60.0
	if stats.CompressionPct < want-0.1 || stats.CompressionPct > want+0.1 {
		t.Errorf("CompressionPct = %.2f, want ~%.2f", stats.CompressionPct, want)
	}
}

// TestAggregateFromSnapshots_ExtraMessages verifies extra messages estimation.
func TestAggregateFromSnapshots_ExtraMessages(t *testing.T) {
	t.Parallel()
	snaps := []analytics.AnalyticsSnapshot{
		{
			SessionStart:    time.Now(),
			TotalRequests:   10,
			TotalInputTokens: 10000,
			SavedInputTokens: 5000,
		},
	}

	stats := AggregateFromSnapshots(snaps)

	// comp = 10000-5000 = 5000. avg comp per req = 500. extra = 5000/500 = 10.
	if stats.ExtraMessages != 10 {
		t.Errorf("ExtraMessages = %d, want 10", stats.ExtraMessages)
	}
}

// TestAggregateFromSnapshots_ZeroTokens verifies handling when no tokens were processed.
func TestAggregateFromSnapshots_ZeroTokens(t *testing.T) {
	t.Parallel()
	snaps := []analytics.AnalyticsSnapshot{
		{
			SessionStart: time.Now(),
		},
	}

	stats := AggregateFromSnapshots(snaps)
	if stats.CompressionRatio != 1.0 {
		t.Errorf("CompressionRatio = %f, want 1.0 with zero tokens", stats.CompressionRatio)
	}
	if stats.CompressionPct != 0 {
		t.Errorf("CompressionPct = %f, want 0 with zero tokens", stats.CompressionPct)
	}
}

// TestFormatStatsTable verifies the output contains expected sections.
func TestFormatStatsTable(t *testing.T) {
	stats := SessionStats{
		SessionStart:    time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC),
		Duration:        2*time.Hour + 30*time.Minute,
		TotalRequests:   42,
		CacheHits:       5,
		InputTokensOrig: 100000,
		InputTokensComp: 40000,
		TokensSaved:     60000,
		OutputTokens:    15000,
		CompressionPct:  60.0,
		ExtraMessages:   10,
	}

	output := FormatStatsTable(stats)

	if !strings.Contains(output, "Slimference Session Stats") {
		t.Error("output should contain header")
	}
	if !strings.Contains(output, "42") {
		t.Error("output should contain request count")
	}
	if !strings.Contains(output, "60.0%") {
		t.Error("output should contain compression percentage")
	}
}

// TestFormatInt verifies the thousands-separator formatting helper.
func TestFormatInt(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{1000000, "1,000,000"},
	}
	for _, tt := range tests {
		got := formatInt(tt.input)
		if got != tt.want {
			t.Errorf("formatInt(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestFormatDuration verifies the human-readable duration formatter.
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m 30s"},
		{2*time.Hour + 15*time.Minute, "2h 15m"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.input)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestAggregateFromSnapshots_MultipleTimeOrdering verifies the Before/After branches when
// three snapshots have different SessionStart values requiring earliest/latest tracking.
func TestAggregateFromSnapshots_MultipleTimeOrdering(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(-2 * time.Hour) // earlier than t0 - triggers Before branch
	t2 := t0.Add(3 * time.Hour)  // later than t0 - triggers After branch
	snaps := []analytics.AnalyticsSnapshot{
		{SessionStart: t0, TotalRequests: 5, TotalInputTokens: 100, SavedInputTokens: 40},
		{SessionStart: t1, TotalRequests: 3},
		{SessionStart: t2, TotalRequests: 7, TotalInputTokens: 200, SavedInputTokens: 80},
	}
	stats := AggregateFromSnapshots(snaps)
	// earliest = t1, latest = t2 - duration = t2 - t1 = 5 hours
	wantDur := t2.Sub(t1)
	if stats.Duration != wantDur {
		t.Errorf("Duration = %v, want %v", stats.Duration, wantDur)
	}
	if !stats.SessionStart.Equal(t1) {
		t.Errorf("SessionStart = %v, want %v (earliest)", stats.SessionStart, t1)
	}
}

// TestAggregateFromSnapshots_NegativeComp_clamped verifies the guard that clamps
// inputTokensComp to 0 when SavedInputTokens > TotalInputTokens.
func TestAggregateFromSnapshots_NegativeComp_clamped(t *testing.T) {
	t.Parallel()
	// SavedInputTokens > TotalInputTokens - comp would be negative - clamped to 0
	snaps := []analytics.AnalyticsSnapshot{
		{
			SessionStart:     time.Now(),
			TotalInputTokens: 100,
			SavedInputTokens: 200, // more saved than total - guard clamps to 0
		},
	}
	stats := AggregateFromSnapshots(snaps)
	if stats.InputTokensComp != 0 {
		t.Errorf("InputTokensComp = %d, want 0 (clamped from negative)", stats.InputTokensComp)
	}
}

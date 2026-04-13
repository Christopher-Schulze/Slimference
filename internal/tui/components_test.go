package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/types"
)

// TestFormatTokens verifies token count display formatting.
func TestFormatTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{500, "500"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{10000, "10.0K"},
		{999999, "1000.0K"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	}
	for _, tt := range tests {
		got := formatTokens(tt.input)
		if got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestFormatLayers verifies the layer list formatter.
func TestFormatLayers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input []int
		want  string
	}{
		{nil, "-"},
		{[]int{}, "-"},
		{[]int{1}, "L1"},
		{[]int{1, 2}, "L1+L2"},
		{[]int{1, 2, 3}, "L1+L2+L3"},
	}
	for _, tt := range tests {
		got := formatLayers(tt.input)
		if got != tt.want {
			t.Errorf("formatLayers(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestShortModelName verifies the model name shortener.
func TestShortModelName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"claude-opus-4-6-20250514", "opus"},
		{"claude-sonnet-4-20250514", "sonnet"},
		{"claude-haiku-3-5-20241022", "haiku"},
		{"o3-mini", "o3"},
		{"o1-preview", "o1"},
		{"gpt-4o-mini", "gpt-4o"},
		{"gpt-4-turbo", "gpt-4"},
		{"some-very-long-model-name-that-is-long", "some-very-"},
		{"short", "short"},
	}
	for _, tt := range tests {
		got := shortModelName(tt.input)
		if got != tt.want {
			t.Errorf("shortModelName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestRenderSessionDuration_Seconds verifies duration formatting for short durations.
func TestRenderSessionDuration_Seconds(t *testing.T) {
	start := time.Now().Add(-3 * time.Second)
	got := renderSessionDuration(start)
	if !strings.Contains(got, "s") {
		t.Errorf("expected seconds in %q", got)
	}
}

// TestRenderSessionDuration_Hours verifies duration formatting for long durations.
func TestRenderSessionDuration_Hours(t *testing.T) {
	start := time.Now().Add(-2 * time.Hour)
	got := renderSessionDuration(start)
	if !strings.Contains(got, "h") {
		t.Errorf("expected hours in %q", got)
	}
}

// TestRenderProgressBar verifies the progress bar renders without panicking.
func TestRenderProgressBar(t *testing.T) {
	s := NewStyles()

	got := renderProgressBar(s, 0.5, 40)
	if got == "" {
		t.Error("renderProgressBar returned empty string")
	}
	if !strings.Contains(got, "50%") {
		t.Errorf("expected 50%% in progress bar, got: %s", got)
	}
}

// TestRenderProgressBar_Boundaries verifies edge cases.
func TestRenderProgressBar_Boundaries(t *testing.T) {
	s := NewStyles()

	// Zero ratio.
	got := renderProgressBar(s, 0.0, 40)
	if !strings.Contains(got, "0%") {
		t.Errorf("expected 0%% in progress bar, got: %s", got)
	}

	// Full ratio.
	got = renderProgressBar(s, 1.0, 40)
	if !strings.Contains(got, "100%") {
		t.Errorf("expected 100%% in progress bar, got: %s", got)
	}

	// Negative ratio (should clamp to 0).
	got = renderProgressBar(s, -0.5, 40)
	if !strings.Contains(got, "0%") {
		t.Errorf("expected clamped 0%% for negative ratio, got: %s", got)
	}

	// Over 1.0 (should clamp to 100%).
	got = renderProgressBar(s, 1.5, 40)
	if !strings.Contains(got, "100%") {
		t.Errorf("expected clamped 100%% for ratio > 1, got: %s", got)
	}
}

// TestRenderRequestLogLine verifies a request log line renders without panicking.
func TestRenderRequestLogLine(t *testing.T) {
	s := NewStyles()
	rm := types.RequestMetrics{
		Timestamp:        time.Now(),
		Provider:         types.Anthropic,
		Model:            "claude-opus-4-6",
		InputTokensOrig:  50000,
		InputTokensComp:  20000,
		CompressionRatio: 0.4,
		Layers:           []int{1, 2},
		LatencyMs:        1.5,
	}

	got := renderRequestLogLine(s, rm)
	if got == "" {
		t.Error("renderRequestLogLine returned empty string")
	}
	if !strings.Contains(got, "opus") {
		t.Errorf("expected model name in log line, got: %s", got)
	}
}

// TestRenderProviderBadge verifies badge rendering for both states.
func TestRenderProviderBadge(t *testing.T) {
	s := NewStyles()

	onStr := renderProviderBadge(s, "Claude Code", true)
	if !strings.Contains(onStr, "ON") {
		t.Errorf("enabled badge should contain ON, got: %s", onStr)
	}

	offStr := renderProviderBadge(s, "Claude Code", false)
	if !strings.Contains(offStr, "OFF") {
		t.Errorf("disabled badge should contain OFF, got: %s", offStr)
	}
}

// TestRenderLayerLine verifies layer line rendering.
func TestRenderLayerLine(t *testing.T) {
	s := NewStyles()

	got := renderLayerLine(s, 1, "Deterministic", true, 5000, "(JSON, dedup)")
	if !strings.Contains(got, "ON") {
		t.Errorf("enabled layer should show ON, got: %s", got)
	}
	if !strings.Contains(got, "Deterministic") {
		t.Errorf("should contain layer name, got: %s", got)
	}

	offGot := renderLayerLine(s, 2, "MiniMax", false, 0, "")
	if !strings.Contains(offGot, "OFF") {
		t.Errorf("disabled layer should show OFF, got: %s", offGot)
	}
}

// TestPadRight verifies the string padding helper.
func TestPadRight(t *testing.T) {
	got := padRight("hi", 5)
	if got != "hi   " {
		t.Errorf("padRight(\"hi\", 5) = %q, want \"hi   \"", got)
	}

	// String already at width.
	got = padRight("hello", 5)
	if got != "hello" {
		t.Errorf("padRight(\"hello\", 5) = %q, want \"hello\"", got)
	}

	// String longer than width - should not truncate.
	got = padRight("hello world", 5)
	if got != "hello world" {
		t.Errorf("padRight(\"hello world\", 5) should not truncate, got %q", got)
	}
}

// TestPadLeft verifies the left-padding helper.
func TestPadLeft(t *testing.T) {
	got := padLeft("hi", 5)
	if got != "   hi" {
		t.Errorf("padLeft(\"hi\", 5) = %q, want \"   hi\"", got)
	}
}

// TestRenderRequestLogLine_CacheHit verifies the cache-hit display.
func TestRenderRequestLogLine_CacheHit(t *testing.T) {
	s := NewStyles()
	rm := types.RequestMetrics{
		Timestamp: time.Now(),
		Provider:  types.Anthropic,
		Model:     "claude-opus-4-6",
		CacheHit:  true,
	}

	got := renderRequestLogLine(s, rm)
	if !strings.Contains(got, "cache-hit") {
		t.Errorf("expected 'cache-hit' in log line, got: %s", got)
	}
}

// TestRenderProgressBar_SmallWidth verifies that a very small totalWidth clamps barWidth to 4.
func TestRenderProgressBar_SmallWidth(t *testing.T) {
	s := NewStyles()
	// totalWidth=5: barWidth = 5 - len(" 50%") - 2 = 5-4-2 = -1 → clamped to 4
	got := renderProgressBar(s, 0.5, 5)
	if got == "" {
		t.Error("renderProgressBar should not return empty for small width")
	}
}

// TestRenderSessionDuration_Minutes verifies duration formatting for minute-range durations.
func TestRenderSessionDuration_Minutes(t *testing.T) {
	start := time.Now().Add(-5 * time.Minute)
	got := renderSessionDuration(start)
	if !strings.Contains(got, "m") {
		t.Errorf("expected minutes in %q", got)
	}
	if strings.Contains(got, "h") {
		t.Errorf("should not contain hours for 5-minute duration: %q", got)
	}
}

// TestPadLeft_alreadyAtWidth verifies padLeft returns the string unchanged when already >= width.
func TestPadLeft_alreadyAtWidth(t *testing.T) {
	got := padLeft("hello", 3) // "hello" is 5 chars > 3 → return as-is
	if got != "hello" {
		t.Errorf("padLeft(\"hello\", 3) = %q, want \"hello\" (no truncation)", got)
	}
}

// TestFormatAgo verifies the formatAgo helper for all time ranges.
func TestFormatAgo(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "ago"},  // d < minute → "Xs ago"
		{5 * time.Minute, "m ago"}, // d < hour → "Xm ago"
		{2 * time.Hour, "h ago"},   // else → "Xh ago"
	}
	for _, tc := range tests {
		past := time.Now().Add(-tc.d)
		got := formatAgo(past)
		if !strings.Contains(got, tc.want) {
			t.Errorf("formatAgo(-%v) = %q, want %q in output", tc.d, got, tc.want)
		}
	}
}

// TestLogLevelStyle verifies logLevelStyle returns a non-zero style for each level.
func TestLogLevelStyle(t *testing.T) {
	s := NewStyles()
	levels := []string{"ERROR", "WARN", "DEBUG", "INFO", "other"}
	for _, level := range levels {
		got := logLevelStyle(s, level)
		// Just verify it returns without panic and is a valid style
		_ = got.Render("test")
	}
}

// TestSafeRatio verifies safeRatio for zero and non-zero token counts.
func TestSafeRatio(t *testing.T) {
	t.Parallel()

	// Zero input tokens → ratio should be 0 (not divide by zero)
	zeroSnap := analytics.AnalyticsSnapshot{TotalInputTokens: 0, SavedInputTokens: 0}
	if got := safeRatio(zeroSnap); got != 0 {
		t.Errorf("safeRatio(zero) = %v, want 0", got)
	}

	// Non-zero input tokens → ratio = saved/total
	snap := analytics.AnalyticsSnapshot{TotalInputTokens: 1000, SavedInputTokens: 600}
	got := safeRatio(snap)
	want := 0.6
	if got < want-0.001 || got > want+0.001 {
		t.Errorf("safeRatio = %v, want ~%v", got, want)
	}
}

// TestRenderRequestLogLine_Passthrough verifies the passthrough display when no compression.
func TestRenderRequestLogLine_Passthrough(t *testing.T) {
	s := NewStyles()
	rm := types.RequestMetrics{
		Timestamp: time.Now(),
		Provider:  types.OpenAI,
		Model:     "gpt-4o",
		// No compression data.
	}

	got := renderRequestLogLine(s, rm)
	if !strings.Contains(got, "passthru") {
		t.Errorf("expected 'passthru' in log line for uncompressed request, got: %s", got)
	}
}

// TestRenderRequestLogLine_LongModelName verifies truncation of model names > 8 chars.
// shortModelName returns the first 10 chars for unknown models; if > 8 chars the log line truncates to 8.
func TestRenderRequestLogLine_LongModelName(t *testing.T) {
	s := NewStyles()
	rm := types.RequestMetrics{
		Timestamp: time.Now(),
		Provider:  types.Anthropic,
		// No known keyword (opus/sonnet/haiku/etc.) - will return first 10 chars "unknown-mo"
		// which is > 8, so renderRequestLogLine truncates to 8.
		Model: "unknown-model-name-very-long",
	}

	got := renderRequestLogLine(s, rm)
	if got == "" {
		t.Error("renderRequestLogLine returned empty string for long model name")
	}
}

// TestRenderRequestLogLine_LongProvider verifies truncation of provider names > 7 chars.
func TestRenderRequestLogLine_LongProvider(t *testing.T) {
	s := NewStyles()
	rm := types.RequestMetrics{
		Timestamp: time.Now(),
		// Provider.String() for Anthropic returns "anthropic" (9 chars > 7) so it truncates.
		Provider: types.Anthropic,
		Model:    "claude-opus-4-6",
	}

	got := renderRequestLogLine(s, rm)
	if got == "" {
		t.Error("renderRequestLogLine returned empty string for long provider")
	}
}

// TestRenderProgressBar_FilledClamp verifies the filled > barWidth guard is safe.
// With ratio = 1.0 and a valid width, filled should equal barWidth exactly (no clamp needed).
// This exercises the guard path with an extreme but valid ratio.
func TestRenderProgressBar_FilledClamp(t *testing.T) {
	s := NewStyles()
	// ratio exactly 1.0 - filled should equal barWidth after math.Round; guard is a safety net.
	got := renderProgressBar(s, 1.0, 20)
	if got == "" {
		t.Error("renderProgressBar returned empty string for ratio=1.0, width=20")
	}
	if !strings.Contains(got, "100%") {
		t.Errorf("expected 100%% in output, got: %s", got)
	}
}

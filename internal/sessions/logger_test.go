package sessions

import (
	"strings"
	"testing"
	"time"
)

// TestSessionLogger_Log_And_Recent verifies that logged entries are retrievable via Recent.
func TestSessionLogger_Log_And_Recent(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	l.Log("INFO", "proxy", "request processed", "provider", "anthropic", "tokens", 100)
	l.Log("WARN", "proxy", "rate limited", "provider", "openai")

	entries := l.Recent(10)
	if len(entries) != 2 {
		t.Fatalf("Recent(10) returned %d entries, want 2", len(entries))
	}

	if entries[0].Level != "INFO" {
		t.Errorf("entries[0].Level = %q, want INFO", entries[0].Level)
	}
	if entries[0].Component != "proxy" {
		t.Errorf("entries[0].Component = %q, want proxy", entries[0].Component)
	}
	if entries[0].Message != "request processed" {
		t.Errorf("entries[0].Message = %q, want 'request processed'", entries[0].Message)
	}
	if entries[1].Level != "WARN" {
		t.Errorf("entries[1].Level = %q, want WARN", entries[1].Level)
	}
}

// TestSessionLogger_Recent_LimitsOutput verifies that Recent(n) returns at most n entries.
func TestSessionLogger_Recent_LimitsOutput(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	for i := 0; i < 10; i++ {
		l.Log("INFO", "test", "msg")
	}

	entries := l.Recent(3)
	if len(entries) != 3 {
		t.Fatalf("Recent(3) returned %d entries, want 3", len(entries))
	}

	// Should be the last 3 in chronological order.
	if entries[0].Message != "msg" {
		t.Errorf("entries[0].Message = %q, want 'msg'", entries[0].Message)
	}
}

// TestSessionLogger_Recent_MoreThanStored verifies that Recent(n) with n > stored returns all entries.
func TestSessionLogger_Recent_MoreThanStored(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	l.Log("INFO", "test", "only one")
	entries := l.Recent(100)
	if len(entries) != 1 {
		t.Fatalf("Recent(100) returned %d entries, want 1", len(entries))
	}
}

// TestSessionLogger_Recent_Empty verifies that Recent returns empty when nothing has been logged.
func TestSessionLogger_Recent_Empty(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()
	entries := l.Recent(10)
	if len(entries) != 0 {
		t.Errorf("Recent(10) on empty logger = %d entries, want 0", len(entries))
	}
}

// TestSessionLogger_Format_PlainMessage verifies the output format without fields.
func TestSessionLogger_Format_PlainMessage(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	entry := LogEntry{
		Timestamp: time.Date(2026, 4, 9, 14, 32, 1, 0, time.UTC),
		Level:     "INFO",
		Component: "proxy",
		Message:   "request processed",
	}

	got := l.Format(entry)

	if !strings.HasPrefix(got, "14:32:01") {
		t.Errorf("Format output should start with timestamp, got: %s", got)
	}
	if !strings.Contains(got, "INFO") {
		t.Errorf("Format output should contain level, got: %s", got)
	}
	if !strings.Contains(got, "proxy:") {
		t.Errorf("Format output should contain component, got: %s", got)
	}
	if !strings.Contains(got, "request processed") {
		t.Errorf("Format output should contain message, got: %s", got)
	}
}

// TestSessionLogger_Format_WithFields verifies that fields are rendered as key=value pairs.
func TestSessionLogger_Format_WithFields(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	entry := LogEntry{
		Timestamp: time.Date(2026, 4, 9, 14, 32, 1, 0, time.UTC),
		Level:     "INFO",
		Component: "proxy",
		Message:   "done",
		Fields:    map[string]any{"provider": "anthropic", "tokens": 500},
	}

	got := l.Format(entry)

	if !strings.Contains(got, "provider=anthropic") {
		t.Errorf("Format should contain provider=anthropic, got: %s", got)
	}
	if !strings.Contains(got, "tokens=500") {
		t.Errorf("Format should contain tokens=500, got: %s", got)
	}
}

// TestSessionLogger_Subscribe_ReceivesEntries verifies that a subscriber receives logged entries.
func TestSessionLogger_Subscribe_ReceivesEntries(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	ch := l.Subscribe()
	defer l.Unsubscribe(ch)

	l.Log("INFO", "test", "hello subscriber")

	select {
	case entry := <-ch:
		if entry.Message != "hello subscriber" {
			t.Errorf("subscriber received message %q, want 'hello subscriber'", entry.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive entry within 1 second")
	}
}

// TestSessionLogger_Unsubscribe_StopsDelivery verifies that unsubscribed channels stop receiving.
func TestSessionLogger_Unsubscribe_StopsDelivery(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	ch := l.Subscribe()
	l.Unsubscribe(ch)

	// After unsubscribe, the channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("unsubscribed channel should be closed")
	}
}

// TestSessionLogger_Subscribe_DropsOnFullBuffer verifies that logging does not block
// when the subscriber buffer is full.
func TestSessionLogger_Subscribe_DropsOnFullBuffer(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	ch := l.Subscribe()
	defer l.Unsubscribe(ch)

	// Fill the subscriber buffer (cap 50) without reading.
	for i := 0; i < 60; i++ {
		l.Log("INFO", "test", "fill buffer")
	}

	// If we got here, the logger did not block.
	// Read at least one entry to confirm the channel is alive.
	select {
	case <-ch:
		// ok
	default:
		t.Error("expected at least one entry in subscriber channel")
	}
}

// TestSessionLogger_FieldsParsing verifies that odd-length fields are handled gracefully.
func TestSessionLogger_FieldsParsing(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	// Odd number of field args: trailing value is ignored.
	l.Log("INFO", "test", "odd fields", "key1", "val1", "orphan_key")

	entries := l.Recent(1)
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if entries[0].Fields["key1"] != "val1" {
		t.Errorf("Fields[key1] = %v, want val1", entries[0].Fields["key1"])
	}
	if _, exists := entries[0].Fields["orphan_key"]; exists {
		t.Error("orphan_key should not be in fields map (unpaired value)")
	}
}

// TestSessionLogger_RingBufferOverflow verifies that the 200-entry buffer wraps around.
func TestSessionLogger_RingBufferOverflow(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()

	for i := 0; i < 300; i++ {
		l.Log("INFO", "test", "msg")
	}

	entries := l.Recent(200)
	if len(entries) != 200 {
		t.Fatalf("Recent(200) = %d entries, want 200 (buffer capacity)", len(entries))
	}
}

// TestParseFields_nonStringKey verifies that parseFields skips entries where the key is not a string.
func TestParseFields_nonStringKey(t *testing.T) {
	t.Parallel()
	l := NewSessionLogger()
	// key=42 (int) is not a string - k, ok := kv[i].(string) fails - !ok - continue
	l.Log("INFO", "test", "msg", 42, "value_ignored")
	entries := l.Recent(1)
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	// Non-string key is skipped, so no fields should be set for it
	if _, found := entries[0].Fields[""]; found {
		t.Error("unexpected empty-key field")
	}
}

// TestPadLevel_longLevel verifies the padLevel truncation and padding branches.
func TestPadLevel_longLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"DEBUG", "DEBUG"},  // len=5=width - return level[:5]
		{"ERROR", "ERROR"},
		{"DEBUGX", "DEBUG"}, // len=6>5 - truncated
		{"WA", "WA   "},    // len=2<5 - padded
	}
	for _, tc := range tests {
		got := padLevel(tc.input)
		if got != tc.want {
			t.Errorf("padLevel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

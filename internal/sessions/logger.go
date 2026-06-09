// Package sessions provides per-session request logging and data export utilities.
package sessions

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// LogEntry represents a single log line captured during a session.
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Component string
	Message   string
	Fields    map[string]any
}

// SessionLogger captures log entries into a fixed-size ring buffer and fans them out
// to any registered subscribers. Safe for concurrent use.
type SessionLogger struct {
	entries     *types.RingBuffer[LogEntry]
	subscribers []chan LogEntry
	mu          sync.Mutex
}

// NewSessionLogger returns a SessionLogger with a 200-entry ring buffer.
func NewSessionLogger() *SessionLogger {
	return &SessionLogger{
		entries: types.NewRingBuffer[LogEntry](200),
	}
}

// Log appends a new entry to the ring buffer and non-blocking sends it to all subscribers.
// fields must be an even number of key-value pairs (k string, v any, k string, v any, ...).
func (l *SessionLogger) Log(level, component, msg string, fields ...any) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Component: component,
		Message:   msg,
		Fields:    parseFields(fields),
	}
	l.entries.Push(entry)

	l.mu.Lock()
	subs := make([]chan LogEntry, len(l.subscribers))
	copy(subs, l.subscribers)
	l.mu.Unlock()

	for _, ch := range subs {
		trySend(ch, entry)
	}
}

// Recent returns the last n entries in chronological order.
// If fewer than n entries exist, all entries are returned.
func (l *SessionLogger) Recent(n int) []LogEntry {
	return l.entries.Last(n)
}

// Subscribe returns a buffered channel that receives every new log entry.
// The channel has a buffer of 50. When the buffer is full, entries are dropped silently.
// Call Unsubscribe when done to avoid a goroutine leak.
func (l *SessionLogger) Subscribe() <-chan LogEntry {
	ch := make(chan LogEntry, 50)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.subscribers = append(l.subscribers, ch)
	return ch
}

// Unsubscribe removes ch from the subscriber list and closes it.
func (l *SessionLogger) Unsubscribe(ch <-chan LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, sub := range l.subscribers {
		if sub == ch {
			l.subscribers = append(l.subscribers[:i], l.subscribers[i+1:]...)
			close(sub)
			return
		}
	}
}

// Format renders entry as a human-readable log line suitable for terminal output.
// Format: "HH:MM:SS LEVEL component: message key=value key=value..."
func (l *SessionLogger) Format(entry LogEntry) string {
	ts := entry.Timestamp.Format("15:04:05")
	var sb strings.Builder
	sb.WriteString(ts)
	sb.WriteByte(' ')
	sb.WriteString(padLevel(entry.Level))
	sb.WriteByte(' ')
	sb.WriteString(entry.Component)
	sb.WriteString(": ")
	sb.WriteString(entry.Message)

	keys := sortedFieldKeys(entry.Fields)
	for _, k := range keys {
		v := entry.Fields[k]
		sb.WriteByte(' ')
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(fmt.Sprintf("%v", v))
	}
	return sb.String()
}

func sortedFieldKeys(fields map[string]any) []string {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// trySend delivers entry to ch without blocking and without panicking if ch is closed.
// Unsubscribe closes ch while Log may hold a stale copy - this guard prevents the panic.
func trySend(ch chan LogEntry, entry LogEntry) {
	defer func() { recover() }() //nolint:errcheck
	select {
	case ch <- entry:
	default:
		// Subscriber buffer full - drop rather than block.
	}
}

// parseFields converts a flat key-value variadic slice into a map.
// Keys must be strings; unpaired trailing values are ignored.
func parseFields(kv []any) map[string]any {
	if len(kv) == 0 {
		return nil
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		m[k] = kv[i+1]
	}
	return m
}

// padLevel right-pads the level string to 5 chars for aligned output.
func padLevel(level string) string {
	const width = 5
	if len(level) >= width {
		return level[:width]
	}
	return level + strings.Repeat(" ", width-len(level))
}

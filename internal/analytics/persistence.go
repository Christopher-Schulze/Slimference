package analytics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

const dateFormat = "2006-01-02"

// PersistenceConfig holds configuration for the analytics persister.
type PersistenceConfig struct {
	LogDir string
}

// Persister appends analytics events and snapshots to daily JSONL log files.
type Persister struct {
	logDir      string
	currentFile *os.File
	currentDate string // YYYY-MM-DD of the open file
	mu          sync.Mutex
}

// persistedEvent is the on-disk envelope for all written records.
type persistedEvent struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// NewPersister creates a Persister that writes to logDir.
// The directory is created if it does not exist.
// Today's JSONL file is opened immediately for append.
func NewPersister(logDir string) (*Persister, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("analytics persister: create log dir: %w", err)
	}
	p := &Persister{logDir: logDir}
	if err := p.openFile(time.Now()); err != nil {
		return nil, err
	}
	return p, nil
}

// WriteEvent appends a single AnalyticsEvent as a JSON line.
func (p *Persister) WriteEvent(event types.AnalyticsEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.rotateIfNeeded(); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("analytics persister: marshal event: %w", err)
	}
	return p.writeLine("analytics_event", raw)
}

// WriteSnapshot appends a full AnalyticsSnapshot as a JSON line.
func (p *Persister) WriteSnapshot(snapshot AnalyticsSnapshot) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.rotateIfNeeded(); err != nil {
		return err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("analytics persister: marshal snapshot: %w", err)
	}
	return p.writeLine("session_snapshot", raw)
}

// Close flushes and closes the underlying log file.
func (p *Persister) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.currentFile != nil {
		if err := p.currentFile.Sync(); err != nil {
			slog.Warn("analytics persister: sync on close failed",
				slog.String("err", err.Error()),
			)
		}
		if err := p.currentFile.Close(); err != nil {
			slog.Warn("analytics persister: close failed",
				slog.String("err", err.Error()),
			)
		}
		p.currentFile = nil
	}
}

// RotateIfNeeded opens a new log file if the date has changed since the last write.
// Thread-safe - acquires the mutex internally.
func (p *Persister) RotateIfNeeded() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.rotateIfNeeded(); err != nil {
		slog.Warn("analytics persister: rotation failed", slog.String("err", err.Error()))
	}
}

// ReadDailyStats reads and returns all AnalyticsSnapshots from a given day's log file.
func ReadDailyStats(logDir string, date time.Time) ([]AnalyticsSnapshot, error) {
	path := filepath.Join(logDir, date.Format(dateFormat)+".jsonl")
	return readSnapshots(path)
}

// ReadWeeklyStats aggregates snapshots from the last 7 days (today inclusive).
func ReadWeeklyStats(logDir string) ([]AnalyticsSnapshot, error) {
	now := time.Now()
	var all []AnalyticsSnapshot
	for i := range 7 {
		day := now.AddDate(0, 0, -i)
		snaps, err := ReadDailyStats(logDir, day)
		if err != nil {
			if os.IsNotExist(err) {
				continue // no data for that day
			}
			return nil, fmt.Errorf("analytics: read stats for %s: %w", day.Format(dateFormat), err)
		}
		all = append(all, snaps...)
	}
	return all, nil
}

// --- internal helpers ---

// rotateIfNeeded switches to a new file if the calendar date has changed.
// Must be called with p.mu held.
func (p *Persister) rotateIfNeeded() error {
	today := time.Now().Format(dateFormat)
	if p.currentDate == today && p.currentFile != nil {
		return nil
	}
	if p.currentFile != nil {
		_ = p.currentFile.Sync()
		_ = p.currentFile.Close()
		p.currentFile = nil
	}
	return p.openFile(time.Now())
}

// openFile opens (or creates) the JSONL file for the given date.
// Must be called with p.mu held.
func (p *Persister) openFile(date time.Time) error {
	name := filepath.Join(p.logDir, date.Format(dateFormat)+".jsonl")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("analytics persister: open log file %s: %w", name, err)
	}
	p.currentFile = f
	p.currentDate = date.Format(dateFormat)
	slog.Debug("analytics persister: opened log file", slog.String("path", name))
	return nil
}

// writeLine serialises and appends one JSON record terminated by a newline.
// Must be called with p.mu held.
func (p *Persister) writeLine(recordType string, raw json.RawMessage) error {
	env := persistedEvent{
		Type:      recordType,
		Timestamp: time.Now().UTC(),
		Payload:   raw,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("analytics persister: marshal envelope: %w", err)
	}
	data = append(data, '\n')
	if _, err := p.currentFile.Write(data); err != nil {
		return fmt.Errorf("analytics persister: write line: %w", err)
	}
	return nil
}

// readSnapshots parses all session_snapshot records from a JSONL file.
func readSnapshots(path string) ([]AnalyticsSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	var snapshots []AnalyticsSnapshot
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 8<<20) // 8 MiB per line
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var env persistedEvent
		if err := json.Unmarshal(line, &env); err != nil {
			slog.Warn("analytics: skip malformed line",
				slog.String("path", path),
				slog.Int("line", lineNum),
				slog.String("err", err.Error()),
			)
			continue
		}
		if env.Type != "session_snapshot" {
			continue
		}
		var snap AnalyticsSnapshot
		if err := json.Unmarshal(env.Payload, &snap); err != nil {
			slog.Warn("analytics: skip malformed snapshot",
				slog.String("path", path),
				slog.Int("line", lineNum),
				slog.String("err", err.Error()),
			)
			continue
		}
		snapshots = append(snapshots, snap)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("analytics: scan %s: %w", path, err)
	}
	return snapshots, nil
}

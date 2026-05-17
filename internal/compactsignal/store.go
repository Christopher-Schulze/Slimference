// Package compactsignal implements a tiny file-backed signal store the
// Codex PreCompact / PostCompact hooks write into and the Slimference
// proxy reads on the hot path.
//
// Why a file-backed store rather than a socket: hooks run as
// short-lived subprocesses with no shared address space with the
// daemon. A 100-byte JSON file under
// ~/.slimference/run/compact/<phase>/<session_id>.json is the cheapest
// reliable IPC channel: atomic write via temp+rename, sub-millisecond
// stat-and-read in the proxy.
//
// Contract:
//   - WriteMarker(phase, sessionID, ...) is called from
//     handleCodexPreCompactHook / handleCodexPostCompactHook in
//     cmd/slimference. Never blocks the hook contract: any I/O error is
//     swallowed (best-effort).
//   - HasRecentSignal(sessionID, phase, maxAge) is called from the
//     proxy hot path. Lock-free read of mtime+ndjson; safe at any
//     concurrency.
//   - CleanupOld(maxAge) prunes stale markers; called periodically by
//     the proxy janitor.
package compactsignal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Phase names match the Codex hook event titles (lower-case, no
// hyphen): "pre" maps to PreCompact, "post" to PostCompact.
const (
	PhasePre  = "pre"
	PhasePost = "post"
)

// Marker is the on-disk payload. JSON-marshalled with encoding/json so
// the hook subprocess and the proxy share a binary-stable format.
type Marker struct {
	Phase     string `json:"phase"`
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id,omitempty"`
	Trigger   string `json:"trigger,omitempty"`
	TSUnix    int64  `json:"ts_unix"`
}

// Store is the directory-rooted handle. Construct one via
// DefaultStore(home) or NewStore(dir).
type Store struct {
	dir    string
	nowFn  func() time.Time
	mkdir  func(string, os.FileMode) error
	write  func(string, []byte, os.FileMode) error
	stat   func(string) (os.FileInfo, error)
	read   func(string) ([]byte, error)
	remove func(string) error
	rename func(string, string) error
	mu     sync.Mutex
}

// DefaultStore returns a Store rooted at
// $home/.slimference/run/compact. The directory does not need to
// exist; it is created lazily on the first WriteMarker.
func DefaultStore(home string) *Store {
	return NewStore(filepath.Join(home, ".slimference", "run", "compact"))
}

// NewStore returns a Store rooted at the given directory. Tests use
// this with a t.TempDir to avoid polluting the user's home.
func NewStore(dir string) *Store {
	return &Store{
		dir:    dir,
		nowFn:  time.Now,
		mkdir:  os.MkdirAll,
		write:  os.WriteFile,
		stat:   os.Stat,
		read:   os.ReadFile,
		remove: os.Remove,
		rename: os.Rename,
	}
}

// WriteMarker writes (or overwrites) the marker file for one
// (phase, sessionID) pair. Best-effort: returns nil for empty sessionID
// or any I/O failure. Atomic via temp+rename so a half-written marker
// can never be observed by a concurrent reader.
func (s *Store) WriteMarker(phase, sessionID, turnID, trigger string) error {
	if !isKnownPhase(phase) || sessionID == "" {
		return nil
	}
	dir := filepath.Join(s.dir, phase)
	if err := s.mkdir(dir, 0o755); err != nil {
		return err
	}
	m := Marker{
		Phase:     phase,
		SessionID: sessionID,
		TurnID:    turnID,
		Trigger:   trigger,
		TSUnix:    s.nowFn().Unix(),
	}
	// json.Marshal cannot fail for Marker: every field is a plain
	// JSON-representable type. If a future schema change adds a field
	// whose marshal can fail (channels, funcs, NaN floats), reinstate
	// the error wrap.
	data, _ := json.Marshal(m)
	target := filepath.Join(dir, sessionID+".json")
	tmp := target + ".tmp"
	if err := s.write(tmp, data, 0o644); err != nil {
		return err
	}
	if err := s.rename(tmp, target); err != nil {
		_ = s.remove(tmp)
		return err
	}
	return nil
}

// HasRecentSignal returns true if a marker exists for (phase,
// sessionID) and its TSUnix is within maxAge of now. The proxy uses
// this as a sub-microsecond hot-path probe.
//
// On any I/O / parse error, returns false (fail-cold; the worst that
// happens is we miss one aggressive-compaction opportunity).
func (s *Store) HasRecentSignal(phase, sessionID string, maxAge time.Duration) bool {
	m, ok := s.ReadMarker(phase, sessionID)
	if !ok {
		return false
	}
	age := s.nowFn().Sub(time.Unix(m.TSUnix, 0))
	return age >= 0 && age <= maxAge
}

// ReadMarker returns the parsed marker for (phase, sessionID). Returns
// ok=false when the marker is missing, unreadable, or malformed.
func (s *Store) ReadMarker(phase, sessionID string) (Marker, bool) {
	if !isKnownPhase(phase) || sessionID == "" {
		return Marker{}, false
	}
	target := filepath.Join(s.dir, phase, sessionID+".json")
	data, err := s.read(target)
	if err != nil {
		return Marker{}, false
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return Marker{}, false
	}
	if m.Phase == "" {
		m.Phase = phase
	}
	return m, true
}

// CleanupOld removes markers older than maxAge across both phases.
// Used by a periodic janitor goroutine in the proxy daemon. Returns
// the count removed and the first error encountered (other errors are
// swallowed; the cleaner is best-effort).
func (s *Store) CleanupOld(maxAge time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed int
	var firstErr error
	cutoff := s.nowFn().Add(-maxAge)
	for _, phase := range []string{PhasePre, PhasePost} {
		dir := filepath.Join(s.dir, phase)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			full := filepath.Join(dir, name)
			info, err := s.stat(full)
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				if err := s.remove(full); err == nil {
					removed++
				} else if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return removed, firstErr
}

// isKnownPhase guards the public API from typos / future phases.
func isKnownPhase(phase string) bool {
	return phase == PhasePre || phase == PhasePost
}

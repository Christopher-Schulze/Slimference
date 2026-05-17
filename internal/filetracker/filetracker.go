// Package filetracker maintains per-session state about which files
// the model has read and which files have been mutated. It is the
// shared substrate for T170 (stale-file-read aging) and T174
// (multi-turn obsolete-message pruning).
//
// Pure in-process Go: no daemon, no DB, no disk. The proxy embeds one
// Tracker and consults it synchronously during the compression
// pipeline. Lookups are O(1) map operations under an RWMutex.
package filetracker

import (
	"crypto/sha256"
	"sync"
)

// ReadObservation records what the model saw the last time a file
// was read in this session. ContentHash is the sha256 of the file
// bytes the proxy passed through.
type ReadObservation struct {
	Turn        int
	ContentHash [32]byte
	ContentLen  int
}

// MutationObservation records the most recent file-mutating
// operation. Turn is the conversation index when the mutation was
// applied; the rest is metadata for log surfacing.
type MutationObservation struct {
	Turn     int
	ToolName string
}

// FileState aggregates everything we know about one path within one
// session. Either field can be the zero value (never read / never
// mutated). Reads and mutations interleave in conversation time; the
// consumer compares Turn fields to decide which is "newer".
type FileState struct {
	Path     string
	Read     ReadObservation
	Mutation MutationObservation
}

// IsStale reports whether the recorded read is older than the
// recorded mutation. A stale read can be replaced with a hash marker
// (T174) because we know the file has changed.
func (s FileState) IsStale() bool {
	if s.Read.Turn == 0 {
		return false
	}
	return s.Mutation.Turn > s.Read.Turn
}

// AgeInTurns returns how many turns have passed since this file was
// last read. Caller supplies the current turn index. Zero when the
// file was never read.
func (s FileState) AgeInTurns(currentTurn int) int {
	if s.Read.Turn == 0 {
		return 0
	}
	if currentTurn <= s.Read.Turn {
		return 0
	}
	return currentTurn - s.Read.Turn
}

// Tracker holds the per-session file state. Methods are safe for
// concurrent use - the proxy processes requests from one session
// serially today, but the lock makes the API future-proof.
type Tracker struct {
	mu       sync.RWMutex
	sessions map[string]map[string]*FileState
}

// New returns an empty Tracker.
func New() *Tracker {
	return &Tracker{sessions: map[string]map[string]*FileState{}}
}

// RecordRead notes that the model has read `path` at `turn`.
// `content` is the actual file bytes - the Tracker hashes them for
// future comparison. Pass an empty session to skip recording.
func (t *Tracker) RecordRead(session, path string, turn int, content []byte) {
	if session == "" || path == "" {
		return
	}
	hash := sha256.Sum256(content)
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[session]
	if !ok {
		s = map[string]*FileState{}
		t.sessions[session] = s
	}
	state, ok := s[path]
	if !ok {
		state = &FileState{Path: path}
		s[path] = state
	}
	state.Read = ReadObservation{Turn: turn, ContentHash: hash, ContentLen: len(content)}
}

// RecordMutation notes that the model invoked a file-mutating tool
// (apply_patch, Write, Edit, …) on `path` at `turn`.
func (t *Tracker) RecordMutation(session, path string, turn int, toolName string) {
	if session == "" || path == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[session]
	if !ok {
		s = map[string]*FileState{}
		t.sessions[session] = s
	}
	state, ok := s[path]
	if !ok {
		state = &FileState{Path: path}
		s[path] = state
	}
	state.Mutation = MutationObservation{Turn: turn, ToolName: toolName}
}

// Get returns the state for `path` in `session`. The second return
// is false when the file was never observed in this session.
func (t *Tracker) Get(session, path string) (FileState, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.sessions[session]
	if !ok {
		return FileState{}, false
	}
	state, ok := s[path]
	if !ok {
		return FileState{}, false
	}
	return *state, true
}

// All returns a snapshot of every tracked file in `session`. Returns
// nil when the session has no observations.
func (t *Tracker) All(session string) []FileState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.sessions[session]
	if !ok {
		return nil
	}
	out := make([]FileState, 0, len(s))
	for _, st := range s {
		out = append(out, *st)
	}
	return out
}

// Forget drops all observations for `session`. Called when the
// session ends so the tracker doesn't accumulate dead state.
func (t *Tracker) Forget(session string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, session)
}

// SessionCount returns the number of sessions currently tracked.
// Surfaced for /admin/status telemetry.
func (t *Tracker) SessionCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.sessions)
}

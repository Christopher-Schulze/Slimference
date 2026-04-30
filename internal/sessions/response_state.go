package sessions

import (
	"sync"
	"sync/atomic"
	"time"
)

// ResponseStateStore tracks the latest server-side response identifier
// per logical session for providers that support continuation via
// previous_response_id (OpenAI Responses) or equivalents (Codex
// ChatGPT). T78. Caps memory by evicting the least-recently-used
// session when the entry budget is exceeded.
type ResponseStateStore struct {
	mu           sync.Mutex
	maxEntries   int
	entries      map[string]*responseStateEntry
	skipTotal    atomic.Int64
	recoverTotal atomic.Int64
}

type responseStateEntry struct {
	responseID string
	updatedAt  time.Time
}

// NewResponseStateStore returns a store capped at maxEntries. <= 0
// falls back to a default of 1024 sessions.
func NewResponseStateStore(maxEntries int) *ResponseStateStore {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	return &ResponseStateStore{
		maxEntries: maxEntries,
		entries:    make(map[string]*responseStateEntry),
	}
}

// Set records the latest response id observed for sessionID. An empty
// session or response id is a no-op so callers can pass through
// unconditionally.
func (s *ResponseStateStore) Set(sessionID, responseID string) {
	if sessionID == "" || responseID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[sessionID] = &responseStateEntry{
		responseID: responseID,
		updatedAt:  time.Now(),
	}
	if len(s.entries) > s.maxEntries {
		s.evictOldestLocked()
	}
}

// Get returns the latest response id known for sessionID, or empty.
// Empty session is always empty.
func (s *ResponseStateStore) Get(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[sessionID]; ok {
		return e.responseID
	}
	return ""
}

// MarkSkipped increments the cumulative server-state-skip counter so
// /admin/status.server_state.skip_total reflects how often the proxy
// would (or did) elide the prefix on a follow-up request.
func (s *ResponseStateStore) MarkSkipped() {
	s.skipTotal.Add(1)
}

// SkipTotal returns the cumulative skip counter.
func (s *ResponseStateStore) SkipTotal() int64 { return s.skipTotal.Load() }

// MarkRecover increments the cumulative recovery counter so
// /admin/status.server_state.recover_total reflects how often a stale
// previous_response_id forced a retry with the full body.
func (s *ResponseStateStore) MarkRecover() {
	s.recoverTotal.Add(1)
}

// RecoverTotal returns the cumulative recovery counter.
func (s *ResponseStateStore) RecoverTotal() int64 { return s.recoverTotal.Load() }

// Forget drops state for one session. Used when the operator clears a
// session manually or when the upstream rejects the previous id.
func (s *ResponseStateStore) Forget(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, sessionID)
}

// Snapshot exposes the live counters for telemetry. T78.
type ResponseStateSnapshot struct {
	Sessions     int   `json:"sessions"`
	SkipTotal    int64 `json:"skip_total"`
	RecoverTotal int64 `json:"recover_total"`
}

func (s *ResponseStateStore) Snapshot() ResponseStateSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ResponseStateSnapshot{
		Sessions:     len(s.entries),
		SkipTotal:    s.skipTotal.Load(),
		RecoverTotal: s.recoverTotal.Load(),
	}
}

func (s *ResponseStateStore) evictOldestLocked() {
	oldestID := ""
	oldestAt := time.Time{}
	for id, e := range s.entries {
		if oldestID == "" || e.updatedAt.Before(oldestAt) {
			oldestID = id
			oldestAt = e.updatedAt
		}
	}
	if oldestID != "" {
		delete(s.entries, oldestID)
	}
}

// Package promptcache implements the cross-turn prefix-stability
// tracker. The proxy uses it to decide how aggressively to place
// Anthropic prompt-cache breakpoints (cache_control: ephemeral).
//
// Why this exists: Anthropic's prompt cache gives a ~90% discount on
// cached input tokens, but only when the prefix up to the breakpoint
// is byte-identical to a previous request. Place breakpoints
// optimistically and you cache-miss; place them conservatively and
// you leave discount on the table.
//
// The Tracker maintains a per-session record of recent prefix hashes.
// On every request it reports HitCount (consecutive matches) and a
// derived Confidence level the caller can use to bias breakpoint
// placement: low confidence → keep current heuristic; high confidence
// → push breakpoints to the latest stable boundary so more tokens
// flow into the cache.
//
// All state is in-memory, lock-protected, bounded by a configurable
// LRU. No on-disk persistence — the tracker resets on daemon
// restart, which is fine because the first turn of a new session
// always misses and is the baseline anyway.
package promptcache

import (
	"sync"
	"time"
)

// PrefixHash is the byte hash of the stable-prefix bytes. 32 bytes is
// a sha256 truncation; callers free to use any 32-byte deterministic
// digest as long as the same prefix produces the same value.
type PrefixHash = [32]byte

// Confidence enumerates how confident the tracker is that the same
// prefix will appear again in the next turn.
type Confidence int

const (
	// ConfidenceCold means we have never seen this prefix in this
	// session. Breakpoints should follow the default conservative
	// placement.
	ConfidenceCold Confidence = iota
	// ConfidenceWarm means the prefix matched the immediately-prior
	// turn exactly once. Probably stable but not yet proven.
	// Breakpoints can shift slightly later.
	ConfidenceWarm
	// ConfidenceHot means the prefix has matched twice or more in a
	// row. Strong evidence the prefix is stable. Breakpoints should
	// be pushed to the very latest position in the stable prefix to
	// maximise cached-token volume.
	ConfidenceHot
)

// Observation is the result of recording a new prefix hash for a
// session. Callers use Confidence to bias breakpoint placement and
// HitCount for analytics.
type Observation struct {
	Confidence Confidence
	HitCount   int
	IsFirst    bool // true if this is the first observation for the session
}

// Tracker is the per-session prefix-stability state store. Safe for
// concurrent use; lock granularity is process-global (one mutex). The
// hot path (Observe + occasional eviction) is microsecond-scale.
type Tracker struct {
	mu      sync.Mutex
	entries map[string]*entry
	order   []string // LRU order
	max     int
	ttl     time.Duration
	now     func() time.Time
}

type entry struct {
	hash      PrefixHash
	hitCount  int
	lastSeen  time.Time
	firstSeen time.Time
}

// NewTracker constructs a Tracker with the given LRU capacity and TTL.
// maxSessions ≤ 0 falls back to 1024; ttl ≤ 0 falls back to
// 30 minutes (long enough to cover typical coding sessions, short
// enough to forget stale state across days).
func NewTracker(maxSessions int, ttl time.Duration) *Tracker {
	if maxSessions <= 0 {
		maxSessions = 1024
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &Tracker{
		entries: make(map[string]*entry, maxSessions),
		max:     maxSessions,
		ttl:     ttl,
		now:     time.Now,
	}
}

// Observe records the prefix hash for a session and returns the
// resulting Observation. The caller passes the hash computed from
// whatever it considers the stable prefix (typically: system prompt
// + tool definitions + first K messages up to CompressiblePrefixEnd).
//
// Mutates internal state. Safe for concurrent calls across sessions
// and within the same session.
func (t *Tracker) Observe(sessionID string, hash PrefixHash) Observation {
	if sessionID == "" {
		return Observation{Confidence: ConfidenceCold, HitCount: 0, IsFirst: true}
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	t.evictExpiredLocked(now)

	e, ok := t.entries[sessionID]
	if !ok {
		t.insertLocked(sessionID, hash, now)
		return Observation{Confidence: ConfidenceCold, HitCount: 1, IsFirst: true}
	}

	// Same hash → bump streak; different hash → reset to 1.
	if e.hash == hash {
		e.hitCount++
	} else {
		e.hash = hash
		e.hitCount = 1
		e.firstSeen = now
	}
	e.lastSeen = now

	// Move to MRU position in the LRU order.
	t.touchLocked(sessionID)

	return Observation{
		Confidence: confidenceFor(e.hitCount),
		HitCount:   e.hitCount,
		IsFirst:    false,
	}
}

// confidenceFor maps a hit-count streak to a Confidence level.
// Threshold chosen so a single coincidental match (hitCount=2) is
// already Hot — Anthropic's cache returns hit/miss telemetry on the
// very first cached read, so a brief over-aggression is recoverable
// on the next turn.
func confidenceFor(hitCount int) Confidence {
	switch {
	case hitCount >= 2:
		return ConfidenceHot
	case hitCount == 1:
		return ConfidenceCold
	default:
		return ConfidenceCold
	}
}

// Snapshot returns a copy of the current entry for inspection. Used
// by diagnostics (slimference doctor, debug surfaces). Returns
// (Observation{}, false) when no entry is present.
func (t *Tracker) Snapshot(sessionID string) (Observation, bool) {
	if sessionID == "" {
		return Observation{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[sessionID]
	if !ok {
		return Observation{}, false
	}
	return Observation{
		Confidence: confidenceFor(e.hitCount),
		HitCount:   e.hitCount,
		IsFirst:    false,
	}, true
}

// Forget removes the entry for one session. Used when a session
// closes cleanly so the LRU doesn't accumulate stale rows.
func (t *Tracker) Forget(sessionID string) {
	if sessionID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, sessionID)
	for i, s := range t.order {
		if s == sessionID {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
}

// Len reports the current number of tracked sessions. Diagnostics.
func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

// insertLocked adds a new session entry. Caller holds mu. Evicts the
// LRU tail if at capacity.
func (t *Tracker) insertLocked(sessionID string, hash PrefixHash, now time.Time) {
	if len(t.entries) >= t.max {
		// Evict LRU tail.
		oldest := t.order[0]
		t.order = t.order[1:]
		delete(t.entries, oldest)
	}
	t.entries[sessionID] = &entry{
		hash:      hash,
		hitCount:  1,
		lastSeen:  now,
		firstSeen: now,
	}
	t.order = append(t.order, sessionID)
}

// touchLocked moves the named session to MRU position. Caller holds
// mu.
func (t *Tracker) touchLocked(sessionID string) {
	for i, s := range t.order {
		if s == sessionID {
			if i == len(t.order)-1 {
				return // already MRU
			}
			t.order = append(t.order[:i], t.order[i+1:]...)
			t.order = append(t.order, sessionID)
			return
		}
	}
}

// evictExpiredLocked drops entries older than t.ttl. Caller holds
// mu. Cheap because the order slice is naturally sorted by
// last-touched time when touchLocked is used consistently.
func (t *Tracker) evictExpiredLocked(now time.Time) {
	cutoff := now.Add(-t.ttl)
	for len(t.order) > 0 {
		head := t.order[0]
		e, ok := t.entries[head]
		if !ok {
			t.order = t.order[1:]
			continue
		}
		if e.lastSeen.After(cutoff) {
			return // remaining entries are newer (MRU order)
		}
		delete(t.entries, head)
		t.order = t.order[1:]
	}
}

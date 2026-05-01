package summarization

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/types"
)

// CachedSummary holds a MiniMax summary alongside its metadata for cache decisions.
type CachedSummary struct {
	Summary          string
	CoveredRange     [2]int
	AnchorsInlined   []int
	OriginalTokens   int
	CompressedTokens int
	CompressionRatio float64
	CreatedAt        time.Time

	// Hash is the SHA-256 fingerprint of the serialised input messages for
	// invalidation when the conversation changes upstream.
	Hash [32]byte

	// AnchorMessages (T111) are deep-copied snapshots of the messages at
	// AnchorIndices. ApplyToMessages re-injects them verbatim into the
	// compressed output so the upstream model does not lose critical edits,
	// errors, decisions, or config touches.
	AnchorMessages []types.Message
}

// --- Session-keyed cache (T110) ---

const defaultMaxSessions = 64

type sessionEntry struct {
	current     *CachedSummary
	previous    *CachedSummary
	compressing atomic.Bool
	lastTouch   time.Time
}

// SessionCache is a session-keyed multi-slot store for Layer 2 summaries.
// Each session gets its own current/previous pair. LRU eviction applies when
// the number of distinct sessions exceeds maxSessions. Thread-safe.
type SessionCache struct {
	mu              sync.RWMutex
	entries         map[string]*sessionEntry
	lru             *list.List
	lruKeys         map[string]*list.Element
	maxSessions     int
	hits            atomic.Int64
	misses          atomic.Int64
	evictions       atomic.Int64
	staleHits       atomic.Int64
	hashMisses      atomic.Int64
	anchorsTotal    atomic.Int64
	anchorsVerbatim atomic.Int64
	anchorsDemoted  atomic.Int64
}

func NewSessionCache(maxSessions int) *SessionCache {
	if maxSessions <= 0 {
		maxSessions = defaultMaxSessions
	}
	return &SessionCache{
		entries:     make(map[string]*sessionEntry),
		lru:         list.New(),
		lruKeys:     make(map[string]*list.Element),
		maxSessions: maxSessions,
	}
}

func (sc *SessionCache) getOrCreateEntry(sessionID string) *sessionEntry {
	if e, ok := sc.entries[sessionID]; ok {
		e.lastTouch = time.Now()
		if elem, ok := sc.lruKeys[sessionID]; ok {
			sc.lru.MoveToFront(elem)
		}
		return e
	}
	sc.evictIfNeeded()
	e := &sessionEntry{lastTouch: time.Now()}
	sc.entries[sessionID] = e
	sc.lruKeys[sessionID] = sc.lru.PushFront(sessionID)
	return e
}

func (sc *SessionCache) evictIfNeeded() {
	for len(sc.entries) >= sc.maxSessions {
		oldest := sc.lru.Back()
		key := sc.lru.Remove(oldest).(string)
		delete(sc.entries, key)
		delete(sc.lruKeys, key)
		sc.evictions.Add(1)
	}
}

func (sc *SessionCache) Store(sessionID string, s *CachedSummary) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	e := sc.getOrCreateEntry(sessionID)
	e.previous = e.current
	e.current = s
}

func (sc *SessionCache) Get(sessionID string) *CachedSummary {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	e, ok := sc.entries[sessionID]
	if !ok || e.current == nil {
		sc.misses.Add(1)
		return nil
	}
	sc.hits.Add(1)
	return e.current
}

func (sc *SessionCache) GetCurrent(sessionID string) (*CachedSummary, [2]int) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	e, ok := sc.entries[sessionID]
	if !ok || e.current == nil {
		sc.misses.Add(1)
		return nil, [2]int{}
	}
	sc.hits.Add(1)
	return e.current, e.current.CoveredRange
}

func (sc *SessionCache) GetCurrentWithHash(sessionID string, inputHash [32]byte) (*CachedSummary, [2]int) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	e, ok := sc.entries[sessionID]
	if !ok || e.current == nil {
		sc.misses.Add(1)
		return nil, [2]int{}
	}
	if e.current.Hash != inputHash {
		sc.hashMisses.Add(1)
		return nil, [2]int{}
	}
	sc.hits.Add(1)
	return e.current, e.current.CoveredRange
}

func (sc *SessionCache) Compressing(sessionID string) bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	e, ok := sc.entries[sessionID]
	if !ok {
		return false
	}
	return e.compressing.Load()
}

func (sc *SessionCache) SetCompressing(sessionID string, v bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	e := sc.getOrCreateEntry(sessionID)
	e.compressing.Store(v)
}

func (sc *SessionCache) Invalidate(sessionID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	e, ok := sc.entries[sessionID]
	if !ok {
		return
	}
	e.current = nil
	e.previous = nil
}

func (sc *SessionCache) InvalidateAll() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.entries = make(map[string]*sessionEntry)
	sc.lru = list.New()
	sc.lruKeys = make(map[string]*list.Element)
}

func (sc *SessionCache) IsStale(sessionID string, maxAge time.Duration) bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	e, ok := sc.entries[sessionID]
	if !ok || e.current == nil {
		sc.staleHits.Add(1)
		return true
	}
	stale := time.Since(e.current.CreatedAt) > maxAge
	if stale {
		sc.staleHits.Add(1)
	}
	return stale
}

func (sc *SessionCache) SessionCount() int {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return len(sc.entries)
}

func (sc *SessionCache) Stats() CacheStats {
	return CacheStats{
		Sessions:        sc.SessionCount(),
		Hits:            sc.hits.Load(),
		Misses:          sc.misses.Load(),
		Evictions:       sc.evictions.Load(),
		StaleHits:       sc.staleHits.Load(),
		HashMisses:      sc.hashMisses.Load(),
		AnchorsTotal:    sc.anchorsTotal.Load(),
		AnchorsVerbatim: sc.anchorsVerbatim.Load(),
		AnchorsDemoted:  sc.anchorsDemoted.Load(),
	}
}

type CacheStats struct {
	Sessions        int   `json:"sessions"`
	Hits            int64 `json:"hits"`
	Misses          int64 `json:"misses"`
	Evictions       int64 `json:"evictions"`
	StaleHits       int64 `json:"stale_hits"`
	HashMisses      int64 `json:"hash_mismatches"`
	AnchorsTotal    int64 `json:"anchors_total"`
	AnchorsVerbatim int64 `json:"anchors_verbatim"`
	AnchorsDemoted  int64 `json:"anchors_demoted"`
}

// --- Legacy SummaryCache (backward compat wrapper) ---

const legacySessionID = ""

type SummaryCache struct {
	Compressing atomic.Bool
	inner       *SessionCache
}

func NewSummaryCache() *SummaryCache {
	return &SummaryCache{inner: NewSessionCache(defaultMaxSessions)}
}

func (c *SummaryCache) Get() *CachedSummary {
	return c.inner.Get(legacySessionID)
}

func (c *SummaryCache) GetCurrent() (*CachedSummary, [2]int) {
	return c.inner.GetCurrent(legacySessionID)
}

func (c *SummaryCache) Store(s *CachedSummary) {
	c.inner.Store(legacySessionID, s)
}

func (c *SummaryCache) Invalidate() {
	c.inner.Invalidate(legacySessionID)
}

func (c *SummaryCache) IsStale(maxAge time.Duration) bool {
	return c.inner.IsStale(legacySessionID, maxAge)
}

func (c *SummaryCache) GetInner() *SessionCache {
	return c.inner
}

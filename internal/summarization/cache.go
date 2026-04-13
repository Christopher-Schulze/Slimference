package summarization

import (
	"sync"
	"sync/atomic"
	"time"
)

// CachedSummary holds a MiniMax summary alongside its metadata for cache decisions.
type CachedSummary struct {
	// Summary is the compressed text produced by MiniMax.
	Summary string

	// CoveredRange is the [start, end] message index range that this summary covers.
	CoveredRange [2]int

	// AnchorsInlined lists the message indices that were preserved verbatim inside the summary.
	AnchorsInlined []int

	// OriginalTokens is the estimated token count of the messages before compression.
	OriginalTokens int

	// CompressedTokens is the estimated token count of the summary text.
	CompressedTokens int

	// CompressionRatio is CompressedTokens / OriginalTokens.
	CompressionRatio float64

	// CreatedAt is the wall-clock time when this summary was stored.
	CreatedAt time.Time

	// Hash is the SHA-256 fingerprint of the serialised input messages for
	// invalidation when the conversation changes upstream.
	Hash [32]byte
}

// SummaryCache is a two-slot in-memory store for the current and previous summaries.
// Concurrent reads are always safe; writes are serialised with a mutex.
type SummaryCache struct {
	mu          sync.RWMutex
	current     *CachedSummary
	previous    *CachedSummary
	Compressing atomic.Bool
}

// NewSummaryCache allocates an empty SummaryCache.
func NewSummaryCache() *SummaryCache {
	return &SummaryCache{}
}

// Get returns the current cached summary, or nil if none exists yet.
func (c *SummaryCache) Get() *CachedSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// GetCurrent returns the current summary together with its covered message range.
// Both return values are zero if no summary has been stored yet.
func (c *SummaryCache) GetCurrent() (*CachedSummary, [2]int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current == nil {
		return nil, [2]int{}
	}
	return c.current, c.current.CoveredRange
}

// Store saves a new summary, demoting the current entry to previous.
func (c *SummaryCache) Store(s *CachedSummary) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.previous = c.current
	c.current = s
}

// Invalidate discards both the current and previous summaries.
func (c *SummaryCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = nil
	c.previous = nil
}

// IsStale returns true if there is no current summary or it was created more than
// maxAge ago.
func (c *SummaryCache) IsStale(maxAge time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current == nil {
		return true
	}
	return time.Since(c.current.CreatedAt) > maxAge
}

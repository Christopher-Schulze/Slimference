package caching

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"time"

	"github.com/slimference/slimference/internal/types"
)

// CacheEntry holds a cached HTTP response with metadata.
type CacheEntry struct {
	Response    []byte
	Headers     map[string][]string
	StatusCode  int
	CreatedAt   time.Time
	HitCount    int
	TokensSaved int
}

// ResponseCache is a thread-safe, size-bounded LRU cache for identical requests.
// Entries are keyed by a SHA-256 hash of the request content.
type ResponseCache struct {
	mu      sync.RWMutex
	entries map[[32]byte]*CacheEntry
	maxSize int
	ttl     time.Duration
	keys    [][32]byte // insertion-order for LRU eviction
}

// NewResponseCache creates a ResponseCache with the given capacity and TTL.
func NewResponseCache(maxSize int, ttl time.Duration) *ResponseCache {
	return &ResponseCache{
		entries: make(map[[32]byte]*CacheEntry, maxSize),
		maxSize: maxSize,
		ttl:     ttl,
		keys:    make([][32]byte, 0, maxSize),
	}
}

// ComputeKey returns a deterministic SHA-256 key for the given messages and model.
// The hash covers each message's role and content text plus the model name,
// independent of provider-specific wire format.
func (c *ResponseCache) ComputeKey(messages []types.Message, model string) [32]byte {
	h := sha256.New()
	buf := make([]byte, 4)
	for _, msg := range messages {
		binary.LittleEndian.PutUint32(buf, uint32(len(msg.Role)))
		h.Write(buf)
		h.Write([]byte(msg.Role))
		text := msg.TextContent()
		binary.LittleEndian.PutUint32(buf, uint32(len(text)))
		h.Write(buf)
		h.Write([]byte(text))
	}
	h.Write([]byte(model))
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}

// Get returns the cache entry for key if present and not expired.
// HitCount is incremented and the entry is promoted to most-recently-used on a hit.
func (c *ResponseCache) Get(key [32]byte) (*CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if c.ttl > 0 && time.Since(entry.CreatedAt) > c.ttl {
		c.deleteKey(key)
		return nil, false
	}
	entry.HitCount++
	c.promoteKey(key)
	return entry, true
}

// Set stores an entry. When the cache is full, the least-recently-used entry is evicted.
// Updating an existing key promotes it to most-recently-used.
func (c *ResponseCache) Set(key [32]byte, entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		if len(c.keys) >= c.maxSize {
			oldest := c.keys[0]
			c.keys = c.keys[1:]
			delete(c.entries, oldest)
		}
		c.keys = append(c.keys, key)
	} else {
		c.promoteKey(key)
	}
	c.entries[key] = entry
}

// Invalidate removes all entries whose response body contains path as a substring.
// Used to purge stale entries when a file referenced in a response changes.
func (c *ResponseCache) Invalidate(path string) {
	if path == "" {
		return
	}
	needle := []byte(path)
	c.mu.Lock()
	defer c.mu.Unlock()
	var remaining [][32]byte
	for _, key := range c.keys {
		entry, ok := c.entries[key]
		if ok && bytes.Contains(entry.Response, needle) {
			delete(c.entries, key)
			continue
		}
		remaining = append(remaining, key)
	}
	c.keys = remaining
}

// Flush removes all entries from the cache.
func (c *ResponseCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[[32]byte]*CacheEntry, c.maxSize)
	c.keys = c.keys[:0]
}

// Cleanup removes all entries that have exceeded the TTL.
// Intended to be called periodically by a background goroutine.
func (c *ResponseCache) Cleanup() {
	if c.ttl <= 0 {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	var remaining [][32]byte
	for _, key := range c.keys {
		entry, ok := c.entries[key]
		if ok && now.Sub(entry.CreatedAt) > c.ttl {
			delete(c.entries, key)
			continue
		}
		remaining = append(remaining, key)
	}
	c.keys = remaining
}

// Len returns the number of entries currently in the cache.
func (c *ResponseCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// promoteKey moves key to the end of c.keys (most-recently-used position).
// Must be called with c.mu held for write.
func (c *ResponseCache) promoteKey(key [32]byte) {
	for i, k := range c.keys {
		if k == key {
			c.keys = append(c.keys[:i], c.keys[i+1:]...)
			c.keys = append(c.keys, key)
			return
		}
	}
}

// deleteKey removes a single key from both the map and the ordered slice.
// Must be called with c.mu held for write.
func (c *ResponseCache) deleteKey(key [32]byte) {
	delete(c.entries, key)
	for i, k := range c.keys {
		if k == key {
			c.keys = append(c.keys[:i], c.keys[i+1:]...)
			return
		}
	}
}

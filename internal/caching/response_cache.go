package caching

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/slimference/slimference/internal/types"
)

// CacheEntry holds a cached HTTP response with metadata.
type CacheEntry struct {
	Response        []byte
	Headers         map[string][]string
	StatusCode      int
	CreatedAt       time.Time
	HitCount        int
	TokensSaved     int
	DependencyPaths []string
}

// ResponseCache is a thread-safe, size-bounded LRU cache for identical requests.
// Entries are keyed by a SHA-256 hash of the request content.
//
// Two-stage lookup (T20):
//   - Stage A (pre-compression): origToCompressed maps the hash of the
//     original request body to the authoritative Stage B key. A Stage A hit
//     lets the caller skip the entire compression pipeline on repeated
//     identical requests.
//   - Stage B (post-compression): entries keyed on the canonical compressed
//     body remain the single source of truth. Stage A is purely a pointer.
//
// Pointer lifetime: Stage A entries are pruned whenever their target Stage B
// entry is deleted (eviction, invalidation, TTL cleanup).
type ResponseCache struct {
	mu               sync.RWMutex
	entries          map[[32]byte]*CacheEntry
	origToCompressed map[[32]byte][32]byte
	maxSize          int
	ttl              time.Duration
	keys             [][32]byte // insertion-order for LRU eviction
}

// NewResponseCache creates a ResponseCache with the given capacity and TTL.
func NewResponseCache(maxSize int, ttl time.Duration) *ResponseCache {
	return &ResponseCache{
		entries:          make(map[[32]byte]*CacheEntry, maxSize),
		origToCompressed: make(map[[32]byte][32]byte, maxSize),
		maxSize:          maxSize,
		ttl:              ttl,
		keys:             make([][32]byte, 0, maxSize),
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

// ComputeRequestKey returns a deterministic SHA-256 key for the full request body.
// The body is canonicalized so insignificant JSON whitespace/key ordering do not
// affect cache hits. Provider is included to prevent cross-provider collisions.
func (c *ResponseCache) ComputeRequestKey(provider types.Provider, body []byte) [32]byte {
	return c.ComputeRequestKeyWithHeaders(provider, body, nil)
}

// ComputeRequestKeyWithHeaders returns a deterministic SHA-256 key for the effective request.
// The key covers provider, canonical JSON body, and semantically relevant request headers so
// cross-account or version/beta requests cannot alias each other in the response cache.
func (c *ResponseCache) ComputeRequestKeyWithHeaders(provider types.Provider, body []byte, headers http.Header) [32]byte {
	h := sha256.New()
	h.Write([]byte(provider.String()))
	h.Write([]byte{0})
	h.Write(canonicalizeJSON(body))
	if headerBytes := canonicalizeCacheHeaders(headers); len(headerBytes) > 0 {
		h.Write([]byte{0})
		h.Write(headerBytes)
	}
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

// GetByOriginal resolves the original-body pointer (Stage A) to the
// authoritative compressed entry (Stage B). It returns the entry and its
// Stage B key so the caller can refresh analytics using the stable id.
// A miss at either stage returns (nil, zero, false).
func (c *ResponseCache) GetByOriginal(origKey [32]byte) (*CacheEntry, [32]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	compKey, ok := c.origToCompressed[origKey]
	if !ok {
		return nil, [32]byte{}, false
	}
	entry, ok := c.entries[compKey]
	if !ok {
		// Orphan pointer: drop and report miss.
		delete(c.origToCompressed, origKey)
		return nil, [32]byte{}, false
	}
	if c.ttl > 0 && time.Since(entry.CreatedAt) > c.ttl {
		c.deleteKey(compKey)
		return nil, [32]byte{}, false
	}
	entry.HitCount++
	c.promoteKey(compKey)
	return entry, compKey, true
}

// RegisterOriginalPointer registers a Stage A pointer origKey -> compKey.
// Safe to call after a Stage B Set so repeated identical requests can skip
// the compression pipeline on the next lookup.
func (c *ResponseCache) RegisterOriginalPointer(origKey, compKey [32]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[compKey]; !ok {
		// No Stage B entry to point at; skip silently.
		return
	}
	c.origToCompressed[origKey] = compKey
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
			c.prunePointersTo(oldest)
		}
		c.keys = append(c.keys, key)
	} else {
		c.promoteKey(key)
	}
	c.entries[key] = entry
}

// Invalidate removes all entries whose tracked dependency paths match path.
// Matching is path-aware: exact normalized matches work for absolute->absolute and
// relative->relative references, and suffix matches handle relative paths stored in
// prompts when the file watcher reports an absolute OS path.
func (c *ResponseCache) Invalidate(path string) {
	normalized := normalizeDependencyPath(path)
	if normalized == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var remaining [][32]byte
	for _, key := range c.keys {
		entry, ok := c.entries[key]
		if ok && cacheEntryDependsOnPath(entry, normalized) {
			delete(c.entries, key)
			c.prunePointersTo(key)
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
	c.origToCompressed = make(map[[32]byte][32]byte, c.maxSize)
	c.keys = c.keys[:0]
}

// AgeHistogram reports the age distribution of currently cached entries.
// Used by /admin/status.cache to surface T102 cache aging telemetry. All
// values are in milliseconds since the entry was created.
type AgeHistogram struct {
	Count int   `json:"count"`
	P50Ms int64 `json:"p50_ms"`
	P95Ms int64 `json:"p95_ms"`
	P99Ms int64 `json:"p99_ms"`
	MaxMs int64 `json:"max_ms"`
}

// AgeSnapshot computes the age histogram for the current cache state. T102.
func (c *ResponseCache) AgeSnapshot() AgeHistogram {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.entries) == 0 {
		return AgeHistogram{}
	}
	now := time.Now()
	ages := make([]int64, 0, len(c.entries))
	for _, entry := range c.entries {
		ages = append(ages, now.Sub(entry.CreatedAt).Milliseconds())
	}
	sort.Slice(ages, func(i, j int) bool { return ages[i] < ages[j] })
	pct := func(p float64) int64 {
		idx := int(float64(len(ages)-1) * p)
		return ages[idx]
	}
	return AgeHistogram{
		Count: len(ages),
		P50Ms: pct(0.50),
		P95Ms: pct(0.95),
		P99Ms: pct(0.99),
		MaxMs: ages[len(ages)-1],
	}
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
			c.prunePointersTo(key)
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

// deleteKey removes a single key from both the map and the ordered slice
// and prunes any Stage A pointers referencing it.
// Must be called with c.mu held for write.
func (c *ResponseCache) deleteKey(key [32]byte) {
	delete(c.entries, key)
	c.prunePointersTo(key)
	for i, k := range c.keys {
		if k == key {
			c.keys = append(c.keys[:i], c.keys[i+1:]...)
			return
		}
	}
}

// prunePointersTo removes every Stage A pointer that targets compKey.
// Must be called with c.mu held for write.
func (c *ResponseCache) prunePointersTo(compKey [32]byte) {
	for orig, target := range c.origToCompressed {
		if target == compKey {
			delete(c.origToCompressed, orig)
		}
	}
}

var dependencyPathRegex = regexp.MustCompile(`(?:\.\.?/|/)?[A-Za-z0-9._+-]+(?:/[A-Za-z0-9._+-]+)+\.[A-Za-z0-9]{1,12}`)
var jsonMarshalFn = json.Marshal

// ExtractDependencyPaths returns normalized file-like paths found anywhere in the JSON body.
// It scans string values recursively so request parameters like messages/system/tools are covered.
func ExtractDependencyPaths(body []byte) []string {
	var root interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return extractDependencyPathsFromString(string(body))
	}
	paths := make(map[string]struct{})
	collectDependencyPaths(root, paths)
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// IsRequestCacheSafe reports whether a request body is safe to serve from the response cache
// without changing expected model behavior. Explicit stochastic settings disable Layer 3.
func IsRequestCacheSafe(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var root map[string]interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return false
	}
	if truthyBool(root["stream"]) {
		return false
	}
	if n, ok := numericValue(root["n"]); ok && n > 1 {
		return false
	}
	if temp, ok := numericValue(root["temperature"]); ok && temp > 0 {
		return false
	}
	if topP, ok := numericValue(root["top_p"]); ok && topP > 0 && topP < 1 {
		return false
	}
	if requestCanProduceToolCalls(root) {
		return false
	}
	return true
}

func requestCanProduceToolCalls(root map[string]interface{}) bool {
	for _, key := range []string{"tools", "functions"} {
		if value, ok := root[key]; ok && nonEmptyJSONValue(value) {
			return true
		}
	}
	if value, ok := root["tool_choice"]; ok && nonEmptyJSONValue(value) {
		return true
	}
	if value, ok := root["function_call"]; ok && nonEmptyJSONValue(value) {
		return true
	}
	return containsToolRole(root["messages"]) || containsToolRole(root["input"])
}

func nonEmptyJSONValue(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "none"
	case []interface{}:
		return len(v) > 0
	case map[string]interface{}:
		return len(v) > 0
	default:
		return true
	}
}

func containsToolRole(value interface{}) bool {
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if containsToolRole(item) {
				return true
			}
		}
	case map[string]interface{}:
		if role, ok := v["role"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(role)) {
			case "tool", "function":
				return true
			}
		}
		if typ, ok := v["type"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(typ)) {
			case "tool_call", "function_call", "function_call_output":
				return true
			}
		}
		for _, child := range v {
			if containsToolRole(child) {
				return true
			}
		}
	}
	return false
}

func canonicalizeJSON(body []byte) []byte {
	var root interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return []byte(strings.TrimSpace(string(body)))
	}
	data, err := jsonMarshalFn(root)
	if err != nil {
		return []byte(strings.TrimSpace(string(body)))
	}
	return data
}

func collectDependencyPaths(v interface{}, paths map[string]struct{}) {
	switch current := v.(type) {
	case map[string]interface{}:
		for _, child := range current {
			collectDependencyPaths(child, paths)
		}
	case []interface{}:
		for _, child := range current {
			collectDependencyPaths(child, paths)
		}
	case string:
		for _, path := range extractDependencyPathsFromString(current) {
			paths[path] = struct{}{}
		}
	}
}

func extractDependencyPathsFromString(text string) []string {
	raw := dependencyPathRegex.FindAllString(text, -1)
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, match := range raw {
		path := normalizeDependencyPath(match)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func normalizeDependencyPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(trimmed))
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
}

func cacheEntryDependsOnPath(entry *CacheEntry, changedPath string) bool {
	for _, dep := range entry.DependencyPaths {
		normalizedDep := normalizeDependencyPath(dep)
		if normalizedDep == "" {
			continue
		}
		if normalizedDep == changedPath {
			return true
		}
		if strings.HasSuffix(changedPath, "/"+normalizedDep) {
			return true
		}
		if strings.HasSuffix(normalizedDep, "/"+changedPath) {
			return true
		}
	}
	return false
}

func canonicalizeCacheHeaders(headers http.Header) []byte {
	if len(headers) == 0 {
		return nil
	}
	canonical := make(map[string][]string)
	for name, values := range headers {
		key := strings.ToLower(strings.TrimSpace(name))
		if !cacheRelevantHeader(key) {
			continue
		}
		normalized := normalizeCacheHeaderValues(key, values)
		if len(normalized) == 0 {
			continue
		}
		canonical[key] = normalized
	}
	if len(canonical) == 0 {
		return nil
	}
	data, err := jsonMarshalFn(canonical)
	if err != nil {
		return nil
	}
	return data
}

func cacheRelevantHeader(name string) bool {
	switch name {
	case "authorization", "x-api-key", "anthropic-version", "anthropic-beta", "openai-organization", "openai-project", "openai-beta":
		return true
	default:
		return false
	}
}

func normalizeCacheHeaderValues(name string, values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if name == "anthropic-beta" || name == "openai-beta" {
			for _, part := range strings.Split(trimmed, ",") {
				token := strings.TrimSpace(part)
				if token != "" {
					normalized = append(normalized, token)
				}
			}
			continue
		}
		if cacheSensitiveHeader(name) {
			sum := sha256.Sum256([]byte(trimmed))
			normalized = append(normalized, "sha256:"+hexDigest(sum))
			continue
		}
		normalized = append(normalized, trimmed)
	}
	sort.Strings(normalized)
	return normalized
}

func cacheSensitiveHeader(name string) bool {
	switch name {
	case "authorization", "x-api-key":
		return true
	default:
		return false
	}
}

func hexDigest(sum [32]byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 64)
	for i, b := range sum {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&0x0f]
	}
	return string(out)
}

func truthyBool(v interface{}) bool {
	switch current := v.(type) {
	case bool:
		return current
	case string:
		return strings.EqualFold(strings.TrimSpace(current), "true")
	default:
		return false
	}
}

func numericValue(v interface{}) (float64, bool) {
	switch current := v.(type) {
	case float64:
		return current, true
	case float32:
		return float64(current), true
	case int:
		return float64(current), true
	case int64:
		return float64(current), true
	case json.Number:
		f, err := current.Float64()
		if err == nil {
			return f, true
		}
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(current), 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

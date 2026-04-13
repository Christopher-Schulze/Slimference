package caching

import (
	"testing"
	"time"

	"github.com/slimference/slimference/internal/types"
)

// buildMessages is a test helper that constructs a simple Message slice.
func buildMessages(t *testing.T, pairs ...string) []types.Message {
	t.Helper()
	if len(pairs)%2 != 0 {
		t.Fatal("buildMessages: pairs must come as role,text,role,text,...")
	}
	msgs := make([]types.Message, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		msgs = append(msgs, types.Message{
			Index: i / 2,
			Role:  pairs[i],
			Content: []types.ContentBlock{
				{Type: "text", Text: pairs[i+1]},
			},
		})
	}
	return msgs
}

// makeEntry constructs a minimal CacheEntry for use in tests.
func makeEntry(body string) *CacheEntry {
	return &CacheEntry{
		Response:   []byte(body),
		StatusCode: 200,
		CreatedAt:  time.Now(),
	}
}

// TestResponseCache_SetAndGet verifies basic set-then-get behaviour.
func TestResponseCache_SetAndGet(t *testing.T) {
	t.Parallel()

	cache := NewResponseCache(10, time.Minute)
	msgs := buildMessages(t, "user", "hello", "assistant", "hi")

	key := cache.ComputeKey(msgs, "claude-3-5-sonnet-20241022")
	entry := makeEntry(`{"text":"hello"}`)

	cache.Set(key, entry)
	got, ok := cache.Get(key)
	if !ok {
		t.Fatal("Get returned false, expected a hit")
	}
	if string(got.Response) != string(entry.Response) {
		t.Errorf("Response = %q, want %q", got.Response, entry.Response)
	}
	if got.HitCount != 1 {
		t.Errorf("HitCount = %d, want 1 after one hit", got.HitCount)
	}
}

// TestResponseCache_TTLExpiry verifies that entries are not returned after TTL.
func TestResponseCache_TTLExpiry(t *testing.T) {
	t.Parallel()

	ttl := 50 * time.Millisecond
	cache := NewResponseCache(10, ttl)
	msgs := buildMessages(t, "user", "ttl test")

	key := cache.ComputeKey(msgs, "claude-3-5-sonnet-20241022")
	entry := &CacheEntry{
		Response:  []byte(`{"ok":true}`),
		CreatedAt: time.Now().Add(-2 * ttl), // already expired
	}

	cache.Set(key, entry)
	_, ok := cache.Get(key)
	if ok {
		t.Error("Get returned true for an already-expired entry")
	}
}

// TestResponseCache_MaxSize_Eviction verifies that adding beyond maxSize evicts the oldest entry.
func TestResponseCache_MaxSize_Eviction(t *testing.T) {
	t.Parallel()

	maxSize := 3
	cache := NewResponseCache(maxSize, time.Hour)

	// Fill to capacity.
	keys := make([][32]byte, maxSize+1)
	for i := 0; i <= maxSize; i++ {
		msgs := buildMessages(t, "user", string(rune('A'+i)))
		keys[i] = cache.ComputeKey(msgs, "m")
		cache.Set(keys[i], makeEntry("body"))
	}

	// The first entry (index 0) must have been evicted.
	_, ok := cache.Get(keys[0])
	if ok {
		t.Error("oldest entry should have been evicted after maxSize exceeded")
	}
	// The newest entry must still be present.
	_, ok = cache.Get(keys[maxSize])
	if !ok {
		t.Error("newest entry should still be present after eviction")
	}
}

// TestResponseCache_Flush verifies that Flush removes all entries.
func TestResponseCache_Flush(t *testing.T) {
	t.Parallel()

	cache := NewResponseCache(10, time.Hour)
	for i := 0; i < 5; i++ {
		msgs := buildMessages(t, "user", string(rune('a'+i)))
		key := cache.ComputeKey(msgs, "m")
		cache.Set(key, makeEntry("body"))
	}

	cache.Flush()

	// All entries must be gone.
	for i := 0; i < 5; i++ {
		msgs := buildMessages(t, "user", string(rune('a'+i)))
		key := cache.ComputeKey(msgs, "m")
		_, ok := cache.Get(key)
		if ok {
			t.Errorf("entry %d still present after Flush", i)
		}
	}
}

// TestComputeKey_Deterministic verifies key stability and distinguishes different inputs.
func TestComputeKey_Deterministic(t *testing.T) {
	t.Parallel()

	cache := NewResponseCache(10, time.Hour)
	msgs := buildMessages(t, "user", "same content")
	model := "claude-3-5-sonnet-20241022"

	key1 := cache.ComputeKey(msgs, model)
	key2 := cache.ComputeKey(msgs, model)
	if key1 != key2 {
		t.Error("ComputeKey is not deterministic for identical inputs")
	}

	keyOtherModel := cache.ComputeKey(msgs, "gpt-4o")
	if key1 == keyOtherModel {
		t.Error("ComputeKey should differ for different model names")
	}

	msgsAlt := buildMessages(t, "user", "different content")
	keyAlt := cache.ComputeKey(msgsAlt, model)
	if key1 == keyAlt {
		t.Error("ComputeKey should differ for different message content")
	}
}

// TestResponseCache_Miss verifies that a cache miss returns false.
func TestResponseCache_Miss(t *testing.T) {
	t.Parallel()

	cache := NewResponseCache(10, time.Hour)
	msgs := buildMessages(t, "user", "never stored")
	key := cache.ComputeKey(msgs, "m")

	_, ok := cache.Get(key)
	if ok {
		t.Error("Get on an empty cache should return false")
	}
}

func TestResponseCache_Invalidate_bySubstring(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Hour)
	msgs1 := buildMessages(t, "user", "one")
	msgs2 := buildMessages(t, "user", "two")
	k1 := cache.ComputeKey(msgs1, "m")
	k2 := cache.ComputeKey(msgs2, "m")
	cache.Set(k1, makeEntry(`{"file":"/proj/src/foo.go"}`))
	cache.Set(k2, makeEntry(`{"other":true}`))

	cache.Invalidate("/proj/src/foo.go")

	if _, ok := cache.Get(k1); ok {
		t.Fatal("entry containing needle should be removed")
	}
	if _, ok := cache.Get(k2); !ok {
		t.Fatal("entry without needle should remain")
	}
}

func TestResponseCache_Invalidate_emptyPathNoop(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, time.Hour)
	msgs := buildMessages(t, "user", "keep")
	k := cache.ComputeKey(msgs, "m")
	cache.Set(k, makeEntry("body"))
	cache.Invalidate("")
	if _, ok := cache.Get(k); !ok {
		t.Fatal("Invalidate(\"\") must not remove entries")
	}
}

func TestResponseCache_Cleanup_removesExpired(t *testing.T) {
	t.Parallel()
	ttl := time.Hour
	cache := NewResponseCache(10, ttl)
	msgs := buildMessages(t, "user", "old")
	k := cache.ComputeKey(msgs, "m")
	cache.Set(k, &CacheEntry{
		Response:   []byte(`ok`),
		StatusCode: 200,
		CreatedAt:  time.Now().Add(-3 * ttl),
	})
	cache.Cleanup()
	if _, ok := cache.Get(k); ok {
		t.Fatal("Cleanup should drop expired entries")
	}
}

// TestResponseCache_Cleanup_keepsValid covers the remaining=append branch (response_cache.go:141)
// where a non-expired entry stays in the cache after Cleanup removes the expired one.
func TestResponseCache_Cleanup_keepsValid(t *testing.T) {
	t.Parallel()
	ttl := time.Hour
	cache := NewResponseCache(10, ttl)

	// Expired entry.
	msgsOld := buildMessages(t, "user", "old")
	kOld := cache.ComputeKey(msgsOld, "m")
	cache.Set(kOld, &CacheEntry{Response: []byte(`old`), StatusCode: 200, CreatedAt: time.Now().Add(-3 * ttl)})

	// Fresh entry.
	msgsNew := buildMessages(t, "user", "new")
	kNew := cache.ComputeKey(msgsNew, "m")
	cache.Set(kNew, &CacheEntry{Response: []byte(`new`), StatusCode: 200, CreatedAt: time.Now()})

	cache.Cleanup()

	if _, ok := cache.Get(kOld); ok {
		t.Fatal("Cleanup should have removed expired entry")
	}
	if _, ok := cache.Get(kNew); !ok {
		t.Fatal("Cleanup should have kept fresh entry")
	}
}

func TestResponseCache_Cleanup_skippedWhenNoTTL(t *testing.T) {
	t.Parallel()
	cache := NewResponseCache(10, 0)
	msgs := buildMessages(t, "user", "ttl0")
	k := cache.ComputeKey(msgs, "m")
	cache.Set(k, &CacheEntry{
		Response:   []byte(`ok`),
		StatusCode: 200,
		CreatedAt:  time.Now().Add(-24 * time.Hour),
	})
	cache.Cleanup()
	if _, ok := cache.Get(k); !ok {
		t.Fatal("with TTL 0, Cleanup is a no-op and entry remains valid to Get")
	}
}

// Package tokens provides token counting and usage tracking utilities.
package tokens

import (
	"crypto/sha256"
	"sync"

	"github.com/Christopher-Schulze/Slimference/internal/types"
	tiktoken "github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

// Counter holds a lazily-initialized, goroutine-safe tiktoken encoder for one
// encoding. A zero-value encoding string means cl100k_base (the historical
// default).
type Counter struct {
	encoding string
	once     sync.Once
	enc      *tiktoken.Tiktoken
}

// global is the package-level cl100k_base Counter used by the free functions.
var global Counter

// o200kGlobal counts with o200k_base, the encoding GPT-4o / GPT-5-codex bill
// in. Codex token guards must use this so before/after token comparisons match
// the model's real accounting instead of the cl100k_base approximation.
var o200kGlobal = Counter{encoding: "o200k_base"}

// loaderOnce wires tiktoken's BPE loader to the embedded offline asset loader
// exactly once. Without it, GetEncoding downloads the BPE ranks over the
// network on first use, which is both a doctrine violation (out-of-band network
// from the proxy process) and a reliability hole: offline the encoder stays nil
// and Count returns 0, silently defeating the savings guards. The offline
// loader embeds cl100k_base and o200k_base so counting is deterministic and
// network-free.
var loaderOnce sync.Once

const (
	tokenCountCacheMinBytes = 4096
	tokenCountCacheMaxItems = 2048
)

type tokenCountCacheKey struct {
	encoding string
	length   int
	hash     [32]byte
}

var tokenCountCache = struct {
	sync.Mutex
	values map[tokenCountCacheKey]int
	order  []tokenCountCacheKey
}{
	values: map[tokenCountCacheKey]int{},
}

func ensureOfflineLoader() {
	loaderOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
	})
}

func (c *Counter) encodingName() string {
	if c.encoding == "" {
		return "cl100k_base"
	}
	return c.encoding
}

// encoder returns the cached encoder for this counter's encoding, initializing
// it once against the offline embedded BPE assets.
func (c *Counter) encoder() *tiktoken.Tiktoken {
	c.once.Do(func() {
		ensureOfflineLoader()
		enc, _ := tiktoken.GetEncoding(c.encodingName())
		c.enc = enc
	})
	return c.enc
}

// Count returns the token count for text using this counter's encoding.
// Returns 0 on encoder initialization failure.
func (c *Counter) Count(text string) int {
	if len(text) >= tokenCountCacheMinBytes {
		if count, ok := tokenCountCacheGet(c.encodingName(), text); ok {
			return count
		}
	}
	enc := c.encoder()
	if enc == nil {
		return 0
	}
	count := len(enc.Encode(text, nil, nil))
	if len(text) >= tokenCountCacheMinBytes {
		tokenCountCachePut(c.encodingName(), text, count)
	}
	return count
}

// CountMessages sums the token count across all content blocks in all messages.
// Tool inputs and tool names are included in the count.
func (c *Counter) CountMessages(messages []types.Message) int {
	total := 0
	for i := range messages {
		for j := range messages[i].Content {
			b := &messages[i].Content[j]
			if b.Text != "" {
				total += c.Count(b.Text)
			}
			if b.ToolInput != "" {
				total += c.Count(b.ToolInput)
			}
			if b.ToolName != "" {
				total += c.Count(b.ToolName)
			}
		}
	}
	return total
}

// Count counts tokens in a string using the package-level Counter.
// Returns 0 on error.
func Count(text string) int {
	return global.Count(text)
}

// CountMessages sums token counts for all content blocks in all messages
// using the package-level Counter.
func CountMessages(messages []types.Message) int {
	return global.CountMessages(messages)
}

// CountString is a convenience alias for Count.
func CountString(s string) int {
	return Count(s)
}

// Estimate returns a fast token estimate as byteLen / 4.
// Useful when full encoding is too slow (e.g. large binary blobs).
func Estimate(byteLen int) int {
	return byteLen / 4
}

func tokenCountCacheGet(encoding, text string) (int, bool) {
	key := tokenCountCacheKeyFor(encoding, text)
	tokenCountCache.Lock()
	defer tokenCountCache.Unlock()
	count, ok := tokenCountCache.values[key]
	return count, ok
}

func tokenCountCachePut(encoding, text string, count int) {
	key := tokenCountCacheKeyFor(encoding, text)
	tokenCountCache.Lock()
	defer tokenCountCache.Unlock()
	if _, ok := tokenCountCache.values[key]; ok {
		tokenCountCache.values[key] = count
		return
	}
	if len(tokenCountCache.order) >= tokenCountCacheMaxItems {
		evict := tokenCountCache.order[0]
		copy(tokenCountCache.order, tokenCountCache.order[1:])
		tokenCountCache.order = tokenCountCache.order[:len(tokenCountCache.order)-1]
		delete(tokenCountCache.values, evict)
	}
	tokenCountCache.values[key] = count
	tokenCountCache.order = append(tokenCountCache.order, key)
}

func tokenCountCacheKeyFor(encoding, text string) tokenCountCacheKey {
	return tokenCountCacheKey{
		encoding: encoding,
		length:   len(text),
		hash:     sha256.Sum256([]byte(text)),
	}
}

func resetTokenCountCacheForTest() {
	tokenCountCache.Lock()
	defer tokenCountCache.Unlock()
	tokenCountCache.values = map[tokenCountCacheKey]int{}
	tokenCountCache.order = nil
}

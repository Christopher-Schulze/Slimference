// Package tokens provides token counting and usage tracking utilities.
package tokens

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
	"github.com/slimference/slimference/internal/types"
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

// Count returns the token count for text using cl100k_base encoding.
// Returns 0 on encoder initialization failure.
func (c *Counter) Count(text string) int {
	enc := c.encoder()
	if enc == nil {
		return 0
	}
	return len(enc.Encode(text, nil, nil))
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

// Package tokens provides token counting and usage tracking utilities.
package tokens

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	"github.com/slimference/slimference/internal/types"
)

// Counter holds a lazily-initialized, goroutine-safe tiktoken encoder.
type Counter struct {
	once sync.Once
	enc  *tiktoken.Tiktoken
}

// global is the package-level Counter used by the free functions.
var global Counter

// encoder returns the cached cl100k_base encoder, initializing it once.
func (c *Counter) encoder() *tiktoken.Tiktoken {
	c.once.Do(func() {
		enc, _ := tiktoken.GetEncoding("cl100k_base")
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

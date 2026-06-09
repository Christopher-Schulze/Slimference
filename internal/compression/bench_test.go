package compression

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// benchCompressor returns a DeterministicCompressor with default config.
func benchCompressor() *DeterministicCompressor {
	cfg := config.Defaults()
	return NewDeterministicCompressor(&cfg.Compression)
}

// benchMessages returns n messages with alternating user/assistant roles.
// Each message has one text content block of the given char count.
func benchMessages(n, chars int) []types.Message {
	text := strings.Repeat("a", chars)
	msgs := make([]types.Message, n)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Role:    role,
			Content: []types.ContentBlock{{Type: "text", Text: text}},
		}
	}
	return msgs
}

// benchMessagesCode returns n messages containing Go code (comment strip + structure targets).
func benchMessagesCode(n int) []types.Message {
	code := strings.Repeat(`// Package foo implements bar.
package foo

import "fmt"

// Foo does something.
func Foo(x int) int {
	// inline comment
	if x > 0 {
		fmt.Println(x)
		return x * 2
	}
	return 0
}

`, 30)
	msgs := make([]types.Message, n)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Role:    role,
			Content: []types.ContentBlock{{Type: "text", Text: code}},
		}
	}
	return msgs
}

func BenchmarkCompress_small_8msg(b *testing.B) {
	c := benchCompressor()
	msgs := benchMessages(8, 200)
	b.ResetTimer()
	for b.Loop() {
		c.Reset()
		c.Compress(msgs)
	}
}

func BenchmarkCompress_medium_12msg(b *testing.B) {
	c := benchCompressor()
	msgs := benchMessages(12, 2000)
	b.ResetTimer()
	for b.Loop() {
		c.Reset()
		c.Compress(msgs)
	}
}

func BenchmarkCompress_large_22msg(b *testing.B) {
	c := benchCompressor()
	msgs := benchMessages(22, 10000)
	b.ResetTimer()
	for b.Loop() {
		c.Reset()
		c.Compress(msgs)
	}
}

func BenchmarkCompress_code_12msg(b *testing.B) {
	c := benchCompressor()
	msgs := benchMessagesCode(12)
	b.ResetTimer()
	for b.Loop() {
		c.Reset()
		c.Compress(msgs)
	}
}

func BenchmarkStripANSICodes_short(b *testing.B) {
	input := "\x1b[32mok\x1b[0m " + strings.Repeat("normal text ", 20)
	b.ResetTimer()
	for b.Loop() {
		StripANSICodes(input)
	}
}

func BenchmarkStripANSICodes_long(b *testing.B) {
	input := strings.Repeat("\x1b[32mFoo\x1b[0m \x1b[31mBar\x1b[0m ", 500)
	b.ResetTimer()
	for b.Loop() {
		StripANSICodes(input)
	}
}

func BenchmarkStripComments_go(b *testing.B) {
	code := strings.Repeat("// comment\nfunc Foo() {} // inline\n", 200)
	b.ResetTimer()
	for b.Loop() {
		StripComments(code, "go")
	}
}

// largeBodyMessages produces a 200KB-style payload that mirrors what
// Layer 1 sees on heavy tool-result traffic: tool_result blocks with
// JSON-shaped content that triggers ANSI strip + JSON compact + dedup
// + structure-extract. Used by the T104b benchmark pair below.
func largeBodyMessages(nPairs int, perBlock int) []types.Message {
	body := "{\n" + strings.Repeat("  \"x\": \"value\",\n", perBlock/16) + "  \"end\": true\n}"
	msgs := make([]types.Message, 0, nPairs*2+1)
	for i := 0; i < nPairs; i++ {
		msgs = append(msgs,
			types.Message{Role: "user", Content: []types.ContentBlock{{Type: "tool_result", Text: body}}},
			types.Message{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "ok"}}},
		)
	}
	msgs = append(msgs, types.Message{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "tail"}}})
	return msgs
}

// BenchmarkCompress_LargeBody_Sequential exercises Layer 1 on a
// 200KB-style payload with the message-level fan-out off (T104b
// baseline).
func BenchmarkCompress_LargeBody_Sequential(b *testing.B) {
	c := benchCompressor()
	msgs := largeBodyMessages(20, 1024)
	b.ResetTimer()
	for b.Loop() {
		c.Reset()
		c.Compress(msgs)
	}
}

// BenchmarkCompress_LargeBody_Parallel exercises Layer 1 on the same
// payload with [compression.tuning] coordinator_parallel = true (T104
// message-level fan-out). Compare against the _Sequential bench above
// to decide whether T104b's stage-partitioned variant is worth the
// extra refactor.
func BenchmarkCompress_LargeBody_Parallel(b *testing.B) {
	cfg := config.Defaults().Compression
	cfg.Tuning.CoordinatorParallel = true
	c := NewDeterministicCompressor(&cfg)
	msgs := largeBodyMessages(20, 1024)
	b.ResetTimer()
	for b.Loop() {
		c.Reset()
		c.Compress(msgs)
	}
}

func BenchmarkExtractStructure_go(b *testing.B) {
	code := strings.Repeat(`// Package foo
package foo

// Foo is exported.
func Foo(x int) int { return x }

// Bar is a type.
type Bar struct{ X int }

`, 50)
	b.ResetTimer()
	for b.Loop() {
		ExtractStructure(code, "go")
	}
}

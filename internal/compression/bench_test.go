package compression

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
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

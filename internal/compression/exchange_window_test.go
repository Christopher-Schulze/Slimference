package compression

import (
	"testing"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

func msg(role, text string) types.Message {
	return types.Message{
		Role: role,
		Content: []types.ContentBlock{
			{Type: "text", Text: text},
		},
	}
}

func TestCompressiblePrefixEnd(t *testing.T) {
	t.Parallel()
	// No user messages: entire slice may be compressed.
	onlyAsst := []types.Message{msg("assistant", "x")}
	if got := CompressiblePrefixEnd(onlyAsst, 3); got != len(onlyAsst) {
		t.Fatalf("no user: got %d want %d", got, len(onlyAsst))
	}
	// Fewer user turns than window: nothing compressible outside window.
	oneUser := []types.Message{msg("user", "a")}
	if got := CompressiblePrefixEnd(oneUser, 2); got != 0 {
		t.Fatalf("one user / window 2: got %d", got)
	}
	// Three user turns, window 1 → exclusive end at last user index.
	chain := []types.Message{
		msg("user", "a"),
		msg("assistant", "b"),
		msg("user", "c"),
		msg("assistant", "d"),
		msg("user", "e"),
	}
	if got := CompressiblePrefixEnd(chain, 1); got != 4 {
		t.Fatalf("window 1: got %d want 4", got)
	}
	if got := CompressiblePrefixEnd(chain, 2); got != 2 {
		t.Fatalf("window 2: got %d want 2", got)
	}
	// Non-positive window behaves like 1.
	if got := CompressiblePrefixEnd(chain, 0); got != 4 {
		t.Fatalf("window 0: got %d want 4", got)
	}
}

package proxy

import (
	"crypto/sha256"

	"github.com/Christopher-Schulze/Slimference/internal/promptcache"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// observePromptCacheStability hashes the bytes of the stable prefix
// for this request and feeds the hash into the per-session
// promptcache.Tracker. Returns the resulting Observation so the caller
// can bias breakpoint placement (cold = current heuristic, hot = push
// breakpoints to the latest stable position to maximise the cached
// token volume on the next request).
//
// Returns a zero Observation when the proxy was constructed without a
// tracker (defensive against test fixtures that build a Proxy
// directly) or the session ID is empty.
func (p *Proxy) observePromptCacheStability(sessionID string, messages []types.Message, stableBoundary int) promptcache.Observation {
	if p == nil || p.promptCacheStability == nil || sessionID == "" {
		return promptcache.Observation{}
	}
	hash := hashStablePrefix(messages, stableBoundary)
	return p.promptCacheStability.Observe(sessionID, hash)
}

// hashStablePrefix returns a deterministic 32-byte digest of the
// stable-prefix content. The digest covers role + text + tool input
// + tool name across every message before stableBoundary, plus the
// boundary index itself. Tools/system blocks that appear in identical
// byte form across turns produce identical hashes.
func hashStablePrefix(messages []types.Message, stableBoundary int) [32]byte {
	end := min(stableBoundary, len(messages))
	h := sha256.New()
	// Length-delimit each section so adjacent fields can't collide
	// (e.g. "ab"+"c" vs "a"+"bc").
	var lenBuf [8]byte
	writeLen := func(n int) {
		lenBuf[0] = byte(n >> 56)
		lenBuf[1] = byte(n >> 48)
		lenBuf[2] = byte(n >> 40)
		lenBuf[3] = byte(n >> 32)
		lenBuf[4] = byte(n >> 24)
		lenBuf[5] = byte(n >> 16)
		lenBuf[6] = byte(n >> 8)
		lenBuf[7] = byte(n)
		_, _ = h.Write(lenBuf[:])
	}
	writeStr := func(s string) {
		writeLen(len(s))
		_, _ = h.Write([]byte(s))
	}
	writeLen(end)
	for i := range end {
		m := messages[i]
		writeStr(m.Role)
		writeLen(len(m.Content))
		for _, b := range m.Content {
			writeStr(b.Type)
			writeStr(b.Text)
			writeStr(b.ToolName)
			writeStr(b.ToolInput)
			writeStr(b.ToolUseID)
			writeStr(b.ToolResultID)
		}
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

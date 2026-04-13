package compression

import (
	"crypto/sha256"
	"strings"
	"sync"
)

const minCharsForNearDedup = 120

type nearEntry struct {
	idx int
	sig [minHashDim]uint64
}

// ContentIndex tracks SHA256 exact matches and MinHash signatures for near-duplicate detection.
type ContentIndex struct {
	mu    sync.Mutex
	exact map[[32]byte]int // SHA256 -> first message index
	near  []nearEntry      // order of appearance for LSH-style linear scan
}

// NewContentIndex returns an initialized ContentIndex.
func NewContentIndex() *ContentIndex {
	return &ContentIndex{
		exact: make(map[[32]byte]int),
	}
}

// CheckAndRecord checks exact and near-duplicates, then records new content.
// Thread-safe. exactDupe/nearDupe indicate which case matched; firstIdx is the earlier message index.
func (ci *ContentIndex) CheckAndRecord(text string, msgIdx int, similarityThreshold float64) (exactDupe, nearDupe bool, firstIdx int) {
	normalized := normalizeForHash(text)
	hash := sha256.Sum256([]byte(normalized))

	ci.mu.Lock()
	defer ci.mu.Unlock()

	if idx, seen := ci.exact[hash]; seen {
		return true, false, idx
	}

	if len(normalized) >= minCharsForNearDedup && similarityThreshold > 0 {
		sig := minHashSignatureFromText(normalized)
		for _, e := range ci.near {
			if e.idx == msgIdx {
				continue
			}
			if minHashJaccardEstimate(sig, e.sig) >= similarityThreshold {
				return false, true, e.idx
			}
		}
	}

	ci.exact[hash] = msgIdx
	if len(normalized) >= minCharsForNearDedup {
		sig := minHashSignatureFromText(normalized)
		ci.near = append(ci.near, nearEntry{idx: msgIdx, sig: sig})
	}
	return false, false, -1
}

// Reset clears all recorded hashes. Called on cache flush.
func (ci *ContentIndex) Reset() {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	ci.exact = make(map[[32]byte]int)
	ci.near = nil
}

// normalizeForHash trims whitespace and normalizes line endings to \n.
func normalizeForHash(text string) string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimSpace(normalized)
}

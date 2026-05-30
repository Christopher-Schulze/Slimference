// Package chunkdedup provides content-defined chunking (FastCDC) and a
// content-addressed chunk identity, the foundation for deduplicating PARTIAL
// overlap in repeated tool outputs and file reads (a re-read after a small edit
// shares most of its chunks with the prior read). T255.
package chunkdedup

import (
	"crypto/sha256"
	"encoding/hex"
)

// Default chunk-size bounds in bytes. Tuned for code/log tool output: small
// enough to catch partial overlap after edits, large enough to keep the
// per-chunk reference overhead well below the saved bytes.
const (
	DefaultMinSize = 2 * 1024
	DefaultAvgSize = 8 * 1024
	DefaultMaxSize = 64 * 1024
)

// Config tunes the FastCDC chunker. Zero fields fall back to the Default* bounds.
type Config struct {
	MinSize int
	AvgSize int
	MaxSize int
}

func (c Config) normalized() (min, avg, max int, maskS, maskL uint64) {
	min, avg, max = c.MinSize, c.AvgSize, c.MaxSize
	if min <= 0 {
		min = DefaultMinSize
	}
	if avg <= 0 {
		avg = DefaultAvgSize
	}
	if max <= 0 {
		max = DefaultMaxSize
	}
	if min > avg {
		min = avg
	}
	if max < avg {
		max = avg
	}
	bits := log2(uint(avg))
	// Normalized chunking: a stricter (more one-bits) mask before the average
	// size makes early cuts rare; a looser mask after it makes a cut likely
	// soon, pulling the chunk-size distribution toward the average.
	maskS = lowMask(bits + 2)
	maskL = lowMask(bits - 2)
	return min, avg, max, maskS, maskL
}

func lowMask(n uint) uint64 {
	if n >= 64 {
		return ^uint64(0)
	}
	return (uint64(1) << n) - 1
}

func log2(n uint) uint {
	var b uint
	for n > 1 {
		n >>= 1
		b++
	}
	return b
}

// gearTable holds 256 deterministic pseudo-random uint64 values (splitmix64 from
// a fixed seed). Computed once at package load with no runtime randomness, so
// chunk boundaries are fully reproducible across runs and processes.
var gearTable = buildGearTable()

func buildGearTable() [256]uint64 {
	var t [256]uint64
	x := uint64(0x9E3779B97F4A7C15)
	for i := range t {
		x += 0x9E3779B97F4A7C15
		z := x
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		z = z ^ (z >> 31)
		t[i] = z
	}
	return t
}

// Chunk splits data into content-defined chunks using FastCDC with normalized
// chunking. Boundaries depend only on local content, so inserting or deleting
// bytes shifts only the affected chunk(s) and leaves the rest aligned. The
// concatenation of the returned chunks equals data.
func Chunk(data []byte, cfg Config) [][]byte {
	min, avg, max, maskS, maskL := cfg.normalized()
	var chunks [][]byte
	for len(data) > 0 {
		n := nextCut(data, min, avg, max, maskS, maskL)
		chunks = append(chunks, data[:n:n])
		data = data[n:]
	}
	return chunks
}

func nextCut(data []byte, min, avg, max int, maskS, maskL uint64) int {
	n := len(data)
	if n <= min {
		return n
	}
	if n > max {
		n = max
	}
	mid := avg
	if mid > n {
		mid = n
	}
	var hash uint64
	i := min
	for ; i < mid; i++ {
		hash = (hash << 1) + gearTable[data[i]]
		if hash&maskS == 0 {
			return i + 1
		}
	}
	for ; i < n; i++ {
		hash = (hash << 1) + gearTable[data[i]]
		if hash&maskL == 0 {
			return i + 1
		}
	}
	return n
}

// ChunkID returns the content-addressed identifier (hex sha256) of a chunk.
// Identical chunks share an id, which is what lets the store dedup them.
func ChunkID(chunk []byte) string {
	sum := sha256.Sum256(chunk)
	return hex.EncodeToString(sum[:])
}

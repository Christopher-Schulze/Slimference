package chunkdedup

import (
	"bytes"
	"testing"
)

// genBytes returns n deterministic pseudo-random bytes (LCG from seed). No
// runtime randomness so tests are reproducible.
func genBytes(n, seed int) []byte {
	b := make([]byte, n)
	x := uint64(seed)*0x9E3779B97F4A7C15 + 1
	for i := range b {
		x = x*6364136223846793005 + 1442695040888963407
		b[i] = byte(x >> 33)
	}
	return b
}

func chunkIDs(chunks [][]byte) []string {
	ids := make([]string, len(chunks))
	for i, c := range chunks {
		ids[i] = ChunkID(c)
	}
	return ids
}

func TestChunk_Deterministic(t *testing.T) {
	t.Parallel()
	data := genBytes(100*1024, 5)
	a := chunkIDs(Chunk(data, Config{}))
	b := chunkIDs(Chunk(data, Config{}))
	if len(a) != len(b) {
		t.Fatalf("nondeterministic chunk count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("chunk %d differs between runs", i)
		}
	}
}

func TestChunk_RespectsBoundsAndReconstructs(t *testing.T) {
	t.Parallel()
	data := genBytes(200*1024, 3)
	chunks := Chunk(data, Config{})
	var recon []byte
	for i, c := range chunks {
		recon = append(recon, c...)
		if len(c) > DefaultMaxSize {
			t.Fatalf("chunk %d exceeds max: %d", i, len(c))
		}
		if i < len(chunks)-1 && len(c) <= DefaultMinSize {
			t.Fatalf("non-final chunk %d not above min: %d", i, len(c))
		}
	}
	if !bytes.Equal(recon, data) {
		t.Fatal("concatenated chunks do not reconstruct the input")
	}
}

// TestChunk_BoundaryStableUnderInsertion is the load-bearing property: a small
// insertion in the middle must shift only the affected chunk(s), leaving most
// chunks (and thus their content-addressed ids) intact so a re-read after an
// edit can reuse them.
func TestChunk_BoundaryStableUnderInsertion(t *testing.T) {
	t.Parallel()
	data := genBytes(256*1024, 7)
	a := chunkIDs(Chunk(data, Config{}))
	if len(a) < 10 {
		t.Fatalf("expected many chunks, got %d", len(a))
	}
	at := 128 * 1024
	edited := make([]byte, 0, len(data)+17)
	edited = append(edited, data[:at]...)
	edited = append(edited, genBytes(17, 99)...)
	edited = append(edited, data[at:]...)
	b := chunkIDs(Chunk(edited, Config{}))

	bset := make(map[string]struct{}, len(b))
	for _, id := range b {
		bset[id] = struct{}{}
	}
	shared := 0
	for _, id := range a {
		if _, ok := bset[id]; ok {
			shared++
		}
	}
	frac := float64(shared) / float64(len(a))
	if frac < 0.7 {
		t.Fatalf("boundary instability: only %.0f%% of chunks survived a 17-byte insertion (%d/%d)", frac*100, shared, len(a))
	}
}

func TestChunk_SmallAndEmpty(t *testing.T) {
	t.Parallel()
	if got := Chunk(nil, Config{}); got != nil {
		t.Fatalf("empty -> %d chunks, want 0", len(got))
	}
	small := []byte("tiny payload")
	chunks := Chunk(small, Config{})
	if len(chunks) != 1 || !bytes.Equal(chunks[0], small) {
		t.Fatalf("small data should be a single chunk, got %d", len(chunks))
	}
}

func TestChunk_CustomConfigBounds(t *testing.T) {
	t.Parallel()
	cfg := Config{MinSize: 64, AvgSize: 256, MaxSize: 1024}
	data := genBytes(20*1024, 11)
	chunks := Chunk(data, cfg)
	var recon []byte
	for i, c := range chunks {
		recon = append(recon, c...)
		if len(c) > cfg.MaxSize {
			t.Fatalf("chunk %d exceeds custom max: %d", i, len(c))
		}
	}
	if !bytes.Equal(recon, data) {
		t.Fatal("custom-config reconstruction mismatch")
	}
	if len(chunks) < 10 {
		t.Fatalf("small avg should produce many chunks, got %d", len(chunks))
	}
}

func TestChunkID_StableAndDistinct(t *testing.T) {
	t.Parallel()
	if ChunkID([]byte("abc")) != ChunkID([]byte("abc")) {
		t.Fatal("ChunkID unstable for identical content")
	}
	if ChunkID([]byte("abc")) == ChunkID([]byte("abd")) {
		t.Fatal("ChunkID collision for distinct content")
	}
}

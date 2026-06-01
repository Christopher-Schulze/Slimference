package chunkdedup

import "testing"

func BenchmarkChunk_256KB(b *testing.B) {
	data := genBytes(256*1024, 101)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = Chunk(data, Config{})
	}
}

func BenchmarkStoreEncodeWithReport_PartialOverlap64KB(b *testing.B) {
	shared := genBytes(48*1024, 102)
	tailA := genBytes(16*1024, 103)
	tailB := genBytes(16*1024, 104)
	dataA := append(append([]byte{}, shared...), tailA...)
	dataB := append(append([]byte{}, shared...), tailB...)
	store := NewStore(Config{}, archiveFake(nil))
	store.EncodeWithReport("bench", dataA)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result := store.EncodeWithReport("bench", dataB)
		if result.Saved <= 0 {
			b.Fatal("expected partial-overlap savings")
		}
	}
}

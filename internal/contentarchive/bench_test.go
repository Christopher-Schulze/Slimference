package contentarchive

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkPut_ArchiveWrite64KB(b *testing.B) {
	dir := b.TempDir()
	base := strings.Repeat("archive payload line with enough entropy for gzip\n", 1200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry, err := Put(dir, Input{
			SessionID:    "bench",
			MessageIndex: i,
			BlockIndex:   0,
			SubLayer:     "bench_archive_write",
			Original:     fmt.Sprintf("%06d\n%s", i, base),
			Preview:      "benchmark archive write",
		}, Limits{})
		if err != nil {
			b.Fatal(err)
		}
		if entry == nil {
			b.Fatal("expected archived entry")
		}
	}
}

func BenchmarkGet_ArchiveRead64KB(b *testing.B) {
	dir := b.TempDir()
	body := strings.Repeat("archive payload line with enough entropy for gzip\n", 1200)
	entry, err := Put(dir, Input{
		SessionID:    "bench",
		MessageIndex: 1,
		BlockIndex:   0,
		SubLayer:     "bench_archive_read",
		Original:     body,
		Preview:      "benchmark archive read",
	}, Limits{})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, got, err := Get(dir, entry.URI)
		if err != nil {
			b.Fatal(err)
		}
		if len(got) != len(body) {
			b.Fatalf("expanded bytes=%d want %d", len(got), len(body))
		}
	}
}

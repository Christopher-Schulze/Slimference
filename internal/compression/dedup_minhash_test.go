package compression

import (
	"strings"
	"testing"
)

func TestWordShingles(t *testing.T) {
	t.Parallel()
	if wordShingles(nil, 3) != nil {
		t.Fatal("nil words")
	}
	one := []string{"only"}
	sh := wordShingles(one, 3)
	if len(sh) != 1 || sh[0] != "only" {
		t.Fatalf("%#v", sh)
	}
	multi := []string{"a", "b", "c", "d"}
	sh2 := wordShingles(multi, 2)
	if len(sh2) != 3 || sh2[0] != "a b" || sh2[2] != "c d" {
		t.Fatalf("%#v", sh2)
	}
}

func TestMinHashJaccardEstimate(t *testing.T) {
	t.Parallel()
	var a, b [minHashDim]uint64
	a[0] = 42
	b[0] = 42
	b[1] = 99
	j := minHashJaccardEstimate(a, b)
	if j <= 0 || j >= 1 {
		t.Fatalf("expected (0,1), got %v", j)
	}
	var same [minHashDim]uint64
	if minHashJaccardEstimate(same, same) != 1.0 {
		t.Fatal("identical sigs => 1.0")
	}
}

func TestMinHashSignatureFromText_empty(t *testing.T) {
	t.Parallel()
	sig := minHashSignatureFromText("   ")
	if sig != [minHashDim]uint64{} {
		t.Fatal("empty input")
	}
}

func TestMinHashSignatureFromText_matchesLegacyJoin(t *testing.T) {
	t.Parallel()
	text := "alpha  beta\ngamma\t delta epsilon zeta"
	got := minHashSignatureFromText(text)
	shingles := wordShingles(tokenizeWords(text), 3)
	var want [minHashDim]uint64
	for i := range minHashDim {
		var minv uint64 = 1<<64 - 1
		seed := uint64(i + 1)
		for _, sh := range shingles {
			h := hashWithSeed(sh, seed)
			if h < minv {
				minv = h
			}
		}
		want[i] = minv
	}
	if got != want {
		t.Fatal("optimized minhash must match legacy joined shingles")
	}
}

func BenchmarkMinHashSignatureFromText(b *testing.B) {
	text := strings.Repeat("alpha beta gamma delta epsilon zeta eta theta iota kappa ", 200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sig := minHashSignatureFromText(text)
		if sig == [minHashDim]uint64{} {
			b.Fatal("empty signature")
		}
	}
}

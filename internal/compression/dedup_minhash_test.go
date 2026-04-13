package compression

import (
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

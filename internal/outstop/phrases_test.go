package outstop

import (
	"strings"
	"testing"
)

func TestPhrasesNonEmpty(t *testing.T) {
	got := Phrases()
	if len(got) == 0 {
		t.Fatalf("Phrases() returned empty")
	}
	for i, p := range got {
		if !strings.HasPrefix(p, "\n") {
			t.Errorf("phrase[%d]=%q must start with \\n to anchor at paragraph break", i, p)
		}
		if len(p) < 5 {
			t.Errorf("phrase[%d]=%q too short, false-positive risk", i, p)
		}
	}
}

func TestPhrasesReturnsCopy(t *testing.T) {
	a := Phrases()
	a[0] = "MUTATED"
	b := Phrases()
	if b[0] == "MUTATED" {
		t.Fatalf("Phrases() returned shared slice; expected independent copy")
	}
}

func TestPhrasesTopN(t *testing.T) {
	cases := []struct {
		n    int
		want int
	}{
		{n: 0, want: 0},
		{n: -1, want: 0},
		{n: 1, want: 1},
		{n: 4, want: 4},
		{n: 1000, want: len(curated.Items)},
	}
	for _, c := range cases {
		got := PhrasesTopN(c.n)
		if len(got) != c.want {
			t.Errorf("PhrasesTopN(%d) len=%d want %d", c.n, len(got), c.want)
		}
	}
}

func TestPhrasesTopNPreservesOrder(t *testing.T) {
	full := Phrases()
	got := PhrasesTopN(3)
	if len(got) != 3 {
		t.Fatalf("got len=%d want 3", len(got))
	}
	for i := 0; i < 3; i++ {
		if got[i] != full[i] {
			t.Errorf("TopN[%d]=%q want %q (declared order must be stable)", i, got[i], full[i])
		}
	}
}

func TestVersionStable(t *testing.T) {
	if Version() == "" {
		t.Fatalf("Version() is empty")
	}
	if !strings.HasPrefix(Version(), "v") {
		t.Errorf("Version()=%q should start with 'v'", Version())
	}
}

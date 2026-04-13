package security

import "testing"

func TestCompilePattern(t *testing.T) {
	t.Parallel()
	p, err := CompilePattern("custom", `AKIA[0-9A-Z]{16}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "custom" || p.Regex == nil {
		t.Fatalf("%+v", p)
	}
	_, err = CompilePattern("bad", "(")
	if err == nil {
		t.Fatal("invalid regex should error")
	}
}

func TestShannonEntropy(t *testing.T) {
	t.Parallel()
	if shannonEntropy("") != 0 {
		t.Fatal("empty string => 0")
	}
	// Two symbols at 50/50 → 1 bit.
	e := shannonEntropy("abababab")
	if e < 0.99 || e > 1.01 {
		t.Fatalf("got %f want ~1.0", e)
	}
}

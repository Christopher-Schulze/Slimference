package wscompact

import (
	"bytes"
	"testing"
)

func TestDeflateRoundTripNoContext(t *testing.T) {
	inflater := NewInflateContext(true)
	deflater := NewDeflateContext(true)
	for _, msg := range [][]byte{
		[]byte(`{"type":"request","body":"alpha alpha alpha"}`),
		[]byte(`{"type":"request","body":"beta beta beta"}`),
	} {
		compressed, err := deflater.Deflate(msg)
		if err != nil {
			t.Fatal(err)
		}
		got, err := inflater.Inflate(compressed)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, msg) {
			t.Fatalf("round-trip mismatch got %q want %q", got, msg)
		}
	}
}

func TestDeflateRoundTripContextTakeover(t *testing.T) {
	inflater := NewInflateContext(false)
	deflater := NewDeflateContext(false)
	first := []byte(`{"type":"request","body":"repeatable-history-repeatable-history-repeatable-history"}`)
	second := []byte(`{"type":"request","body":"repeatable-history-repeatable-history-tail"}`)

	c1, err := deflater.Deflate(first)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := inflater.Inflate(c1); err != nil || !bytes.Equal(got, first) {
		t.Fatalf("first inflate got %q err %v", got, err)
	}
	c2, err := deflater.Deflate(second)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := inflater.Inflate(c2); err != nil || !bytes.Equal(got, second) {
		t.Fatalf("second inflate got %q err %v", got, err)
	}
	if len(c2) >= len(second) {
		t.Fatalf("context takeover did not produce compact output: compressed=%d plain=%d", len(c2), len(second))
	}
}

func TestDeflateObserveKeepsDestinationDictionaryInSync(t *testing.T) {
	sourceDeflater := NewDeflateContext(false)
	sourceInflater := NewInflateContext(false)
	destinationDeflater := NewDeflateContext(false)
	destinationInflater := NewInflateContext(false)

	first := []byte(`{"type":"request","body":"shared-dictionary-shared-dictionary-shared-dictionary"}`)
	rawFirst, err := sourceDeflater.Deflate(first)
	if err != nil {
		t.Fatal(err)
	}
	plainFirst, err := sourceInflater.Inflate(rawFirst)
	if err != nil {
		t.Fatal(err)
	}
	if err := destinationDeflater.Observe(plainFirst); err != nil {
		t.Fatal(err)
	}
	if got, err := destinationInflater.Inflate(rawFirst); err != nil || !bytes.Equal(got, first) {
		t.Fatalf("destination first got %q err %v", got, err)
	}

	mutatedSecond := []byte(`{"type":"request","body":"shared-dictionary-mutated"}`)
	encoded, err := destinationDeflater.Deflate(mutatedSecond)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := destinationInflater.Inflate(encoded); err != nil || !bytes.Equal(got, mutatedSecond) {
		t.Fatalf("mutated second got %q err %v", got, err)
	}
}

func TestInflateCorruptionBlocksContext(t *testing.T) {
	inflater := NewInflateContext(false)
	if _, err := inflater.Inflate([]byte{0xff, 0xff, 0xff}); err == nil {
		t.Fatal("expected corruption error")
	}
	if !inflater.Blocked() {
		t.Fatal("context should block after corruption")
	}
	if _, err := inflater.Inflate(nil); err == nil {
		t.Fatal("expected blocked error")
	}
}

func TestInflateWithLimitBlocksOversizedPlaintext(t *testing.T) {
	deflater := NewDeflateContext(true)
	inflater := NewInflateContext(true)
	compressed, err := deflater.Deflate([]byte("0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inflater.InflateWithLimit(compressed, 4); err == nil {
		t.Fatal("expected inflate limit error")
	}
	if !inflater.Blocked() {
		t.Fatal("inflater should block after oversized plaintext")
	}
}

func TestDeflateBlockedBranches(t *testing.T) {
	var nilDeflater *DeflateContext
	if !nilDeflater.Blocked() {
		t.Fatal("nil deflater should report blocked")
	}
	if _, err := nilDeflater.Deflate([]byte("x")); err == nil {
		t.Fatal("nil deflater should reject Deflate")
	}
	if err := nilDeflater.Observe([]byte("x")); err == nil {
		t.Fatal("nil deflater should reject Observe")
	}

	deflater := NewDeflateContext(false)
	if deflater.Blocked() {
		t.Fatal("fresh deflater should not be blocked")
	}
	deflater.blocked = true
	if !deflater.Blocked() {
		t.Fatal("manually blocked deflater should report blocked")
	}
	if _, err := deflater.Deflate([]byte("x")); err == nil {
		t.Fatal("blocked deflater should reject Deflate")
	}
	if err := deflater.Observe([]byte("x")); err == nil {
		t.Fatal("blocked deflater should reject Observe")
	}
}

func TestAppendDictBounds(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), maxDeflateDict+10)
	got := appendDict(nil, payload)
	if len(got) != maxDeflateDict {
		t.Fatalf("dict len=%d", len(got))
	}
	if !bytes.Equal(got, payload[10:]) {
		t.Fatal("dict did not keep tail")
	}

	dict := bytes.Repeat([]byte("d"), maxDeflateDict)
	got = appendDict(dict, []byte("tail"))
	if len(got) != maxDeflateDict {
		t.Fatalf("dict+payload len=%d", len(got))
	}
	if !bytes.HasSuffix(got, []byte("tail")) {
		t.Fatal("dict append did not keep payload tail")
	}
}

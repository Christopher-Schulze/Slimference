package chunkdedup

import (
	"bytes"
	"testing"
	"time"
)

func archiveFake(captured map[string][]byte) ArchiveFunc {
	return func(_, id string, chunk []byte) string {
		if captured != nil {
			captured[id] = append([]byte(nil), chunk...)
		}
		return "local-archive://" + id
	}
}

func TestStore_RepeatSendDedups(t *testing.T) {
	t.Parallel()
	archived := map[string][]byte{}
	store := NewStore(Config{}, archiveFake(archived))
	data := genBytes(64*1024, 21)

	enc1, saved1 := store.Encode("s1", data)
	if saved1 != 0 || !bytes.Equal(enc1, data) {
		t.Fatalf("first send should pass through with no saving: saved=%d", saved1)
	}
	enc2, saved2 := store.Encode("s1", data)
	if saved2 <= 0 {
		t.Fatalf("identical resend should dedup, saved=%d", saved2)
	}
	if len(enc2) >= len(data) {
		t.Fatalf("encoded resend should be shorter: %d vs %d", len(enc2), len(data))
	}
	if !bytes.Contains(enc2, []byte("local-archive://")) {
		t.Fatal("encoded resend must carry recoverable refs")
	}
	if !bytes.Contains(enc2, []byte("[context-chunk status=unchanged")) {
		t.Fatalf("encoded resend must use neutral context-chunk markers: %q", enc2[:min(len(enc2), 120)])
	}
	if len(archived) == 0 {
		t.Fatal("referenced chunks must be archived for recovery")
	}
}

func TestStore_PartialOverlapDedups(t *testing.T) {
	t.Parallel()
	store := NewStore(Config{}, archiveFake(nil))
	shared := genBytes(48*1024, 5)
	tail1 := genBytes(16*1024, 6)
	tail2 := genBytes(16*1024, 7)

	d1 := append(append([]byte{}, shared...), tail1...)
	d2 := append(append([]byte{}, shared...), tail2...)
	store.Encode("s", d1)
	enc, saved := store.Encode("s", d2)
	if saved <= 0 {
		t.Fatalf("partial overlap (shared 48KB prefix) should save, saved=%d", saved)
	}
	if !bytes.Contains(enc, []byte("local-archive://")) {
		t.Fatal("shared chunks should be referenced")
	}
	// Partial: shorter than full d2 but longer than a fully-deduped resend.
	if len(enc) >= len(d2) {
		t.Fatalf("partial encode should shorten: %d vs %d", len(enc), len(d2))
	}
}

func TestStore_Reset(t *testing.T) {
	t.Parallel()
	store := NewStore(Config{}, archiveFake(nil))
	data := genBytes(32*1024, 9)
	store.Encode("s", data)
	store.Reset("s")
	_, saved := store.Encode("s", data)
	if saved != 0 {
		t.Fatalf("after reset, resend must be treated as a first send: saved=%d", saved)
	}
}

func TestStore_NoArchiveDoesNotEmitUnrecoverableRefs(t *testing.T) {
	t.Parallel()
	store := NewStore(Config{}, nil)
	data := genBytes(64*1024, 31)
	store.Encode("s", data)
	enc, saved := store.Encode("s", data)
	if saved != 0 {
		t.Fatalf("without archive, repeated content must not be deduped: saved=%d", saved)
	}
	if !bytes.Equal(enc, data) {
		t.Fatalf("without archive, repeated content must pass through verbatim")
	}
	if bytes.Contains(enc, []byte("[unchanged region: ]")) {
		t.Fatal("must never emit an empty unrecoverable reference")
	}
}

func TestStore_SessionsIndependent(t *testing.T) {
	t.Parallel()
	store := NewStore(Config{}, archiveFake(nil))
	data := genBytes(32*1024, 13)
	store.Encode("a", data)
	if _, savedB := store.Encode("b", data); savedB != 0 {
		t.Fatalf("session b must not dedup session a's content: saved=%d", savedB)
	}
	if _, savedA := store.Encode("a", data); savedA <= 0 {
		t.Fatal("session a should dedup its own previously sent content")
	}
}

func TestStore_TTLExpiresSeenChunks(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	store := NewStoreWithLimits(Config{}, StoreLimits{TTL: time.Minute}, archiveFake(nil))
	store.now = func() time.Time { return now }
	data := genBytes(32*1024, 41)

	store.Encode("s", data)
	if _, saved := store.Encode("s", data); saved <= 0 {
		t.Fatal("precondition: repeated data should dedup before TTL expiry")
	}
	now = now.Add(2 * time.Minute)
	if _, saved := store.Encode("s", data); saved != 0 {
		t.Fatalf("expired session must fail open and reseed, saved=%d", saved)
	}
}

func TestStore_LRUBoundsSessionsAndChunks(t *testing.T) {
	t.Parallel()
	now := time.Unix(2000, 0)
	store := NewStoreWithLimits(Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, StoreLimits{
		MaxSessions:         1,
		MaxChunksPerSession: 2,
		TTL:                 time.Hour,
	}, archiveFake(nil))
	store.now = func() time.Time { return now }

	dataA := genBytes(16*1024, 51)
	store.Encode("a", dataA)
	now = now.Add(time.Second)
	store.Encode("b", dataA)
	if _, saved := store.Encode("a", dataA); saved != 0 {
		t.Fatalf("session a should have been evicted by MaxSessions=1, saved=%d", saved)
	}

	store.Reset("b")
	dataB := genBytes(24*1024, 52)
	store.Encode("b", dataB)
	_, saved := store.Encode("b", dataB)
	if saved >= len(dataB)/2 {
		t.Fatalf("MaxChunksPerSession=2 should keep only a small tail of chunks, saved=%d len=%d", saved, len(dataB))
	}
}

func TestDecodeReferences(t *testing.T) {
	t.Parallel()
	body := []byte("expanded chunk body")
	ref := FormatReference("local-archive://abc123", len(body))
	got, changed := DecodeReferences("before "+ref+" after", func(uri string) ([]byte, bool) {
		if uri != "local-archive://abc123" {
			return nil, false
		}
		return body, true
	})
	if !changed || got != "before expanded chunk body after" {
		t.Fatalf("DecodeReferences mismatch changed=%v got=%q", changed, got)
	}
	if got := FormatReference("local-archive://abc123", -4); !bytes.Contains([]byte(got), []byte("bytes=0")) {
		t.Fatalf("negative sizes should normalize to zero: %q", got)
	}
	if got, changed := DecodeReferences(FormatReference("local-archive://abc123", len(body)+1), func(string) ([]byte, bool) {
		return body, true
	}); changed || got == string(body) {
		t.Fatalf("size mismatch must fail open changed=%v got=%q", changed, got)
	}
	if got, changed := DecodeReferences(ref, func(string) ([]byte, bool) { return nil, false }); changed || got != ref {
		t.Fatalf("missing expansion must fail open changed=%v got=%q", changed, got)
	}
	if got, changed := DecodeReferences("plain text", nil); changed || got != "plain text" {
		t.Fatalf("nil expander must no-op changed=%v got=%q", changed, got)
	}
}

func TestStore_NoOps(t *testing.T) {
	t.Parallel()
	var nilStore *Store
	if enc, saved := nilStore.Encode("s", []byte("x")); saved != 0 || string(enc) != "x" {
		t.Fatal("nil store should be a no-op")
	}
	nilStore.Reset("s") // must not panic

	store := NewStore(Config{}, nil)
	if _, saved := store.Encode("", []byte("data")); saved != 0 {
		t.Fatal("empty session should be a no-op")
	}
	if _, saved := store.Encode("s", nil); saved != 0 {
		t.Fatal("empty data should be a no-op")
	}
}

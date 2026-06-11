package chunkdedup

import (
	"bytes"
	"fmt"
	"strings"
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

func TestStore_RepeatedChunksInFirstSendOnlySeed(t *testing.T) {
	t.Parallel()
	archived := map[string][]byte{}
	store := NewStore(Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, archiveFake(archived))
	data := []byte(strings.Repeat("same first-send line\n", 9000))

	first := store.EncodeWithReport("s1", data)
	if first.Saved != 0 || first.Verified || !bytes.Equal(first.Data, data) {
		t.Fatalf("first send must not reference chunks introduced by the same output: saved=%d verified=%v", first.Saved, first.Verified)
	}
	if len(archived) != 0 {
		t.Fatalf("first send should not archive same-output chunk refs, archived=%d", len(archived))
	}

	second := store.EncodeWithReport("s1", data)
	if second.Saved <= 0 || !second.Verified || !bytes.Contains(second.Data, []byte("local-archive://")) {
		t.Fatalf("second send should dedup chunks seeded by the first send: saved=%d verified=%v", second.Saved, second.Verified)
	}
}

func TestStore_EncodeWithReportVerifiesReferences(t *testing.T) {
	t.Parallel()
	archived := map[string][]byte{}
	store := NewStore(Config{}, archiveFake(archived))
	data := genBytes(64*1024, 22)

	first := store.EncodeWithReport("s1", data)
	if first.Saved != 0 || first.Verified {
		t.Fatalf("first send should only seed state: saved=%d verified=%v", first.Saved, first.Verified)
	}
	second := store.EncodeWithReport("s1", data)
	if second.Saved <= 0 {
		t.Fatalf("second send should save, saved=%d", second.Saved)
	}
	if !second.Verified {
		t.Fatal("changed chunk-dedup output must be locally verified")
	}
	if second.ReferenceCount <= 0 || second.ReferencedBytes <= 0 {
		t.Fatalf("expected reference report, count=%d bytes=%d", second.ReferenceCount, second.ReferencedBytes)
	}
	decoded, changed := DecodeReferences(string(second.Data), func(uri string) ([]byte, bool) {
		const prefix = "local-archive://"
		if !strings.HasPrefix(uri, prefix) {
			return nil, false
		}
		chunk, ok := archived[strings.TrimPrefix(uri, prefix)]
		return chunk, ok
	})
	if !changed || decoded != string(data) {
		t.Fatalf("verified references should expand to original content changed=%v", changed)
	}
}

func TestStore_FailsOpenWhenArchiveURICollides(t *testing.T) {
	t.Parallel()
	store := NewStore(Config{}, func(_, _ string, _ []byte) string {
		return "local-archive://same"
	})
	data := genBytes(64*1024, 23)

	store.Encode("s1", data)
	result := store.EncodeWithReport("s1", data)
	if result.Saved != 0 || result.Verified {
		t.Fatalf("unverifiable colliding archive URIs must fail open: saved=%d verified=%v", result.Saved, result.Verified)
	}
	if !bytes.Equal(result.Data, data) {
		t.Fatal("unverifiable colliding archive URIs must pass through original data")
	}
}

func TestStore_SessionReferenceBudgetLimitsReferences(t *testing.T) {
	t.Parallel()
	store := NewStoreWithLimits(Config{}, StoreLimits{MaxSessionRefPct: 40}, archiveFake(nil))
	data := genBytes(64*1024, 24)

	store.Encode("s1", data)
	result := store.EncodeWithReport("s1", data)
	if result.Saved <= 0 || !result.Verified {
		t.Fatalf("session budget should allow bounded references: saved=%d verified=%v", result.Saved, result.Verified)
	}
	if result.ReferencedBytes*100 > (len(data)*2)*40 {
		t.Fatalf("session reference budget exceeded: referenced=%d visible=%d", result.ReferencedBytes, len(data)*2)
	}
}

func TestStore_SessionReferenceBudgetCountsSeedOutputs(t *testing.T) {
	t.Parallel()
	store := NewStoreWithLimits(Config{}, StoreLimits{MaxSessionRefPct: 70}, archiveFake(nil))
	shared := genBytes(48*1024, 29)
	tailA := genBytes(16*1024, 30)
	tailB := genBytes(16*1024, 31)
	first := append(append([]byte{}, shared...), tailA...)
	second := append(append([]byte{}, shared...), tailB...)

	seed := store.EncodeWithReportWithMaxReferencePercent("s1", first, 90)
	if seed.Saved != 0 || seed.Verified {
		t.Fatalf("first output should seed only: saved=%d verified=%v", seed.Saved, seed.Verified)
	}
	result := store.EncodeWithReportWithMaxReferencePercent("s1", second, 90)
	if result.Saved <= 0 || !result.Verified {
		t.Fatalf("session budget should include the full seed output and allow safe later references: saved=%d verified=%v", result.Saved, result.Verified)
	}
}

func TestStore_ReferenceBudgetAvailableReflectsRemainingBudget(t *testing.T) {
	t.Parallel()
	store := NewStoreWithLimits(Config{}, StoreLimits{MaxSessionRefPct: 20}, archiveFake(nil))
	data := genBytes(64*1024, 32)

	if !store.ReferenceBudgetAvailable("s1", 4096) {
		t.Fatal("new sessions should have budget available so first outputs can seed")
	}
	store.EncodeWithReportWithMaxReferencePercent("s1", data, 100)
	if !store.ReferenceBudgetAvailable("s1", 4096) {
		t.Fatal("seed output should create budget for later references")
	}
	result := store.EncodeWithReportWithMaxReferencePercent("s1", data, 100)
	if result.Saved <= 0 || !result.Verified {
		t.Fatalf("second output should consume reference budget: saved=%d verified=%v", result.Saved, result.Verified)
	}
	if store.ReferenceBudgetAvailable("s1", len(data)) {
		t.Fatalf("session should not report enough budget for another full-size reference after consumption: result=%+v", result)
	}
	if store.ReferenceBudgetAvailable("", 1) {
		t.Fatal("empty session id should not report available budget")
	}
}

func TestStore_OutputReferenceBudgetLimitsReferences(t *testing.T) {
	t.Parallel()
	store := NewStoreWithLimits(Config{}, StoreLimits{MaxSessionRefPct: 60}, archiveFake(nil))
	shared := genBytes(48*1024, 25)
	tailA := genBytes(16*1024, 26)
	tailB := genBytes(16*1024, 27)
	tailC := genBytes(16*1024, 28)
	dataA := append(append([]byte{}, shared...), tailA...)
	dataB := append(append([]byte{}, shared...), tailB...)
	dataC := append(append([]byte{}, tailB...), tailC...)

	store.EncodeWithReportWithMaxReferencePercent("s1", dataA, 100)
	capped := store.EncodeWithReportWithMaxReferencePercent("s1", dataB, 10)
	if capped.Saved <= 0 || !capped.Verified {
		t.Fatalf("dense per-output reference candidate should save within cap: saved=%d verified=%v", capped.Saved, capped.Verified)
	}
	if capped.ReferencedBytes*100 > len(dataB)*10 {
		t.Fatalf("per-output reference budget exceeded: referenced=%d len=%d", capped.ReferencedBytes, len(dataB))
	}

	accepted := store.EncodeWithReportWithMaxReferencePercent("s1", dataC, 100)
	if accepted.Saved <= 0 || !accepted.Verified {
		t.Fatalf("bounded prior candidate should leave enough session budget: saved=%d verified=%v", accepted.Saved, accepted.Verified)
	}
}

func TestEncodePlanPrioritizesLargestSavingsWithinReferenceBudget(t *testing.T) {
	t.Parallel()
	small := []byte(strings.Repeat("small repeated chunk line\n", 60))
	large := []byte(strings.Repeat("large repeated chunk line with more savings\n", 130))
	tail := []byte(strings.Repeat("tail repeated chunk line\n", 60))
	data := append(append(append([]byte{}, small...), large...), tail...)
	plan := chunkPlan{
		chunks:   [][]byte{small, large, tail},
		ids:      []string{"small", "large", "tail"},
		repeated: []bool{true, true, true},
	}

	result := encodePlan(data, plan, len(large), func(id string, _ []byte) (string, bool) {
		return "local-archive://" + id, true
	})

	if result.Saved <= 0 || !result.Verified {
		t.Fatalf("expected verified reference savings, saved=%d verified=%v", result.Saved, result.Verified)
	}
	if result.ReferenceCount != 1 || result.ReferencedBytes != len(large) {
		t.Fatalf("expected only the largest chunk to consume the budget: count=%d bytes=%d wantBytes=%d", result.ReferenceCount, result.ReferencedBytes, len(large))
	}
	if !bytes.Contains(result.Data, []byte("local-archive://large")) {
		t.Fatalf("large repeated chunk should be referenced: %q", result.Data[:min(len(result.Data), 180)])
	}
	if bytes.Contains(result.Data, []byte("local-archive://small")) || bytes.Contains(result.Data, []byte("local-archive://tail")) {
		t.Fatalf("smaller chunks should stay verbatim when only one reference fits: %q", result.Data[:min(len(result.Data), 220)])
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

func TestStore_LineOrientedLogsDedupWithinReferenceBudget(t *testing.T) {
	t.Parallel()
	archived := map[string][]byte{}
	store := NewStoreWithLimits(Config{}, StoreLimits{MaxSessionRefPct: 100}, archiveFake(archived))
	first := genLineOrientedLog("alpha/test_case_a.go:42 expected=17 actual=19")
	second := genLineOrientedLog("alpha/test_case_b.go:77 expected=23 actual=29")

	seed := store.EncodeWithReportWithMaxReferencePercent("s-log", first, 90)
	if seed.Saved != 0 || seed.Verified || !bytes.Equal(seed.Data, first) {
		t.Fatalf("first log output should seed only: saved=%d verified=%v", seed.Saved, seed.Verified)
	}

	result := store.EncodeWithReportWithMaxReferencePercent("s-log", second, 90)
	if result.Saved <= 0 || !result.Verified {
		t.Fatalf("second similar log should dedup within budget: saved=%d verified=%v", result.Saved, result.Verified)
	}
	if result.ReferencedBytes*100 > len(second)*90 {
		t.Fatalf("referenced bytes exceed per-output budget: referenced=%d len=%d", result.ReferencedBytes, len(second))
	}
	if !bytes.Contains(result.Data, []byte("local-archive://")) {
		t.Fatal("deduped log must carry recoverable chunk refs")
	}
	decoded, changed := DecodeReferences(string(result.Data), func(uri string) ([]byte, bool) {
		const prefix = "local-archive://"
		if !strings.HasPrefix(uri, prefix) {
			return nil, false
		}
		chunk, ok := archived[strings.TrimPrefix(uri, prefix)]
		return chunk, ok
	})
	if !changed || decoded != string(second) {
		t.Fatalf("deduped log should reconstruct exact second output changed=%v", changed)
	}
}

func genLineOrientedLog(failure string) []byte {
	var b strings.Builder
	for i := 0; i < 520; i++ {
		if i == 260 {
			fmt.Fprintf(&b, "FAIL package %s slow-path checksum mismatch\n", failure)
			continue
		}
		fmt.Fprintf(&b, "INFO worker=%02d shard=%02d package=alpha case=%03d status=ok checksum=stable trace=compile-test-loop\n", i%17, i%13, i)
	}
	return []byte(b.String())
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

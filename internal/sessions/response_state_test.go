package sessions

import (
	"strconv"
	"testing"
)

func TestNewResponseStateStore_DefaultCap(t *testing.T) {
	t.Parallel()
	s := NewResponseStateStore(0)
	if s.maxEntries != 1024 {
		t.Fatalf("default cap: %d", s.maxEntries)
	}
}

func TestResponseStateStore_SetGet(t *testing.T) {
	t.Parallel()
	s := NewResponseStateStore(10)
	s.Set("sess-1", "resp-A")
	if got := s.Get("sess-1"); got != "resp-A" {
		t.Fatalf("get: %q", got)
	}
	if got := s.Get("missing"); got != "" {
		t.Fatalf("missing: %q", got)
	}
}

func TestResponseStateStore_EmptyIDsIgnored(t *testing.T) {
	t.Parallel()
	s := NewResponseStateStore(10)
	s.Set("", "resp-A")
	s.Set("sess-1", "")
	if s.Get("sess-1") != "" {
		t.Fatal("empty response must not be stored")
	}
	if s.Get("") != "" {
		t.Fatal("empty session lookup must be empty")
	}
}

func TestResponseStateStore_Eviction(t *testing.T) {
	t.Parallel()
	s := NewResponseStateStore(2)
	s.Set("a", "1")
	s.Set("b", "2")
	s.Set("c", "3")
	if got := s.Snapshot().Sessions; got > 2 {
		t.Fatalf("eviction failed: %d", got)
	}
}

func TestResponseStateStore_ManyEntriesBoundedByCap(t *testing.T) {
	t.Parallel()
	s := NewResponseStateStore(3)
	for i := 0; i < 50; i++ {
		s.Set("sess-"+strconv.Itoa(i), "resp-"+strconv.Itoa(i))
	}
	if got := s.Snapshot().Sessions; got > 3 {
		t.Fatalf("cap not enforced: %d", got)
	}
}

func TestResponseStateStore_Forget(t *testing.T) {
	t.Parallel()
	s := NewResponseStateStore(10)
	s.Set("sess-1", "resp-A")
	s.Forget("sess-1")
	if s.Get("sess-1") != "" {
		t.Fatal("forget did not clear")
	}
}

func TestResponseStateStore_SkipCounter(t *testing.T) {
	t.Parallel()
	s := NewResponseStateStore(10)
	s.MarkSkipped()
	s.MarkSkipped()
	if s.SkipTotal() != 2 {
		t.Fatalf("skip total: %d", s.SkipTotal())
	}
	if s.Snapshot().SkipTotal != 2 {
		t.Fatalf("snapshot skip: %d", s.Snapshot().SkipTotal)
	}
}

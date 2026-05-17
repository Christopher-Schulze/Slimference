package proxy

import (
	"testing"
	"time"

	"github.com/slimference/slimference/internal/compactsignal"
)

func TestPrecompactShrinkWindow(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 0},
		{1, 1},
		{2, 1},
		{4, 2},
		{5, 2},
		{10, 5},
		{100, 50},
	}
	for _, c := range cases {
		if got := precompactShrinkWindow(c.in); got != c.want {
			t.Errorf("precompactShrinkWindow(%d)=%d, want %d", c.in, got, c.want)
		}
	}
}

func TestHasRecentPreCompactSignal_NilProxy(t *testing.T) {
	var p *Proxy
	if p.hasRecentPreCompactSignal("s") {
		t.Fatalf("nil proxy must not signal")
	}
}

func TestHasRecentPreCompactSignal_NilStore(t *testing.T) {
	p := &Proxy{}
	if p.hasRecentPreCompactSignal("s") {
		t.Fatalf("proxy without compactSignals must not signal")
	}
}

func TestHasRecentPreCompactSignal_EmptySession(t *testing.T) {
	p := &Proxy{compactSignals: compactsignal.NewStore(t.TempDir())}
	if p.hasRecentPreCompactSignal("") {
		t.Fatalf("empty session must not signal")
	}
}

func TestHasRecentPreCompactSignal_FreshMarker(t *testing.T) {
	dir := t.TempDir()
	store := compactsignal.NewStore(dir)
	if err := store.WriteMarker(compactsignal.PhasePre, "sess-A", "turn-1", "auto"); err != nil {
		t.Fatalf("write: %v", err)
	}
	p := &Proxy{compactSignals: store}
	if !p.hasRecentPreCompactSignal("sess-A") {
		t.Fatalf("fresh marker must trigger signal")
	}
}

func TestHasRecentPreCompactSignal_PostMarkerNotSeenAsPre(t *testing.T) {
	dir := t.TempDir()
	store := compactsignal.NewStore(dir)
	_ = store.WriteMarker(compactsignal.PhasePost, "sess-A", "turn-1", "auto")
	p := &Proxy{compactSignals: store}
	if p.hasRecentPreCompactSignal("sess-A") {
		t.Fatalf("post marker must not satisfy pre query")
	}
}

func TestPrecompactSignalTTL_DocumentedConstant(t *testing.T) {
	// Pin the documented contract: the TTL is 60 seconds. If you raise
	// this, update the t164/t163 docs and the proxy log line in
	// handler.go because operators reason about this window.
	if precompactSignalTTL != 60*time.Second {
		t.Fatalf("precompactSignalTTL drift: %v != 60s", precompactSignalTTL)
	}
}

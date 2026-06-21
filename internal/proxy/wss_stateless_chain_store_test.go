package proxy

import (
	"encoding/json"
	"testing"
)

func TestWSSStatelessChainStore_PutAndGet(t *testing.T) {
	t.Parallel()
	s := newWSSStatelessChainStore(10)
	chain := wssResponseChain{json.RawMessage(`{"role":"assistant"}`), json.RawMessage(`{"role":"tool"}`)}
	s.put("resp-1", chain)
	got := s.get("resp-1")
	if len(got) != 2 {
		t.Fatalf("get after put: len=%d, want 2", len(got))
	}
}

func TestWSSStatelessChainStore_NilSafe(t *testing.T) {
	t.Parallel()
	var s *wssStatelessChainStore
	s.put("resp-1", wssResponseChain{json.RawMessage(`{}`)})
	if got := s.get("resp-1"); got != nil {
		t.Fatalf("nil store get should return nil, got %v", got)
	}
}

func TestWSSStatelessChainStore_EmptyResponseID(t *testing.T) {
	t.Parallel()
	s := newWSSStatelessChainStore(10)
	s.put("", wssResponseChain{json.RawMessage(`{}`)})
	if got := s.get(""); got != nil {
		t.Fatalf("empty responseID get should return nil, got %v", got)
	}
	s.put("  ", wssResponseChain{json.RawMessage(`{}`)})
	if got := s.get("  "); got != nil {
		t.Fatalf("whitespace responseID get should return nil, got %v", got)
	}
}

func TestWSSStatelessChainStore_EmptyChain(t *testing.T) {
	t.Parallel()
	s := newWSSStatelessChainStore(10)
	s.put("resp-1", nil)
	if got := s.get("resp-1"); got != nil {
		t.Fatalf("empty chain put should not store, got %v", got)
	}
}

func TestWSSStatelessChainStore_DefaultMax(t *testing.T) {
	t.Parallel()
	s := newWSSStatelessChainStore(0)
	if s.max != wssRecoveryMaxChains {
		t.Fatalf("default max should be wssRecoveryMaxChains=%d, got %d", wssRecoveryMaxChains, s.max)
	}
}

func TestWSSStatelessChainStore_EvictsOldest(t *testing.T) {
	t.Parallel()
	s := newWSSStatelessChainStore(2)
	chain := wssResponseChain{json.RawMessage(`{}`)}
	s.put("r1", chain)
	s.put("r2", chain)
	s.put("r3", chain)
	if got := s.get("r1"); got != nil {
		t.Fatalf("r1 should be evicted, got %v", got)
	}
	if got := s.get("r2"); got == nil {
		t.Fatal("r2 should still exist")
	}
	if got := s.get("r3"); got == nil {
		t.Fatal("r3 should still exist")
	}
}

func TestWSSStatelessChainStore_PutOverwritesExisting(t *testing.T) {
	t.Parallel()
	s := newWSSStatelessChainStore(10)
	s.put("r1", wssResponseChain{json.RawMessage(`{"v":1}`)})
	s.put("r1", wssResponseChain{json.RawMessage(`{"v":2}`), json.RawMessage(`{"v":3}`)})
	got := s.get("r1")
	if len(got) != 2 {
		t.Fatalf("overwrite should update chain, got len=%d", len(got))
	}
}

func TestProxyRememberAndGetWSSStatelessChain_NilProxy(t *testing.T) {
	t.Parallel()
	var p *Proxy
	p.rememberWSSStatelessChain("r1", wssResponseChain{json.RawMessage(`{}`)})
	if got := p.wssStatelessChain("r1"); got != nil {
		t.Fatalf("nil proxy should return nil, got %v", got)
	}
}

func TestProxyRememberAndGetWSSStatelessChain_NilStore(t *testing.T) {
	t.Parallel()
	p := &Proxy{}
	p.rememberWSSStatelessChain("r1", wssResponseChain{json.RawMessage(`{}`)})
	if got := p.wssStatelessChain("r1"); got != nil {
		t.Fatalf("nil store should return nil, got %v", got)
	}
}

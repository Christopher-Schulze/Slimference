package proxy

import (
	"strings"
	"sync"
)

type wssStatelessChainStore struct {
	mu     sync.Mutex
	max    int
	order  []string
	chains map[string]wssResponseChain
}

func newWSSStatelessChainStore(max int) *wssStatelessChainStore {
	if max <= 0 {
		max = wssRecoveryMaxChains
	}
	return &wssStatelessChainStore{
		max:    max,
		chains: make(map[string]wssResponseChain),
	}
}

func (s *wssStatelessChainStore) put(responseID string, chain wssResponseChain) {
	responseID = strings.TrimSpace(responseID)
	if s == nil || responseID == "" || len(chain) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chains == nil {
		s.chains = make(map[string]wssResponseChain)
	}
	if _, exists := s.chains[responseID]; !exists {
		s.order = append(s.order, responseID)
	}
	s.chains[responseID] = wssResponseChain(cloneWSSRawItems(chain))
	for len(s.order) > s.max {
		oldest := s.order[0]
		copy(s.order, s.order[1:])
		s.order = s.order[:len(s.order)-1]
		delete(s.chains, oldest)
	}
}

func (s *wssStatelessChainStore) get(responseID string) wssResponseChain {
	responseID = strings.TrimSpace(responseID)
	if s == nil || responseID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return wssResponseChain(cloneWSSRawItems(s.chains[responseID]))
}

func (p *Proxy) rememberWSSStatelessChain(responseID string, chain wssResponseChain) {
	if p == nil || p.wssStatelessChains == nil {
		return
	}
	p.wssStatelessChains.put(responseID, chain)
}

func (p *Proxy) wssStatelessChain(responseID string) wssResponseChain {
	if p == nil || p.wssStatelessChains == nil {
		return nil
	}
	return p.wssStatelessChains.get(responseID)
}

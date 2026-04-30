package proxy

import (
	"sync"
	"time"

	"github.com/slimference/slimference/internal/types"
)

// healthMonitor tracks per-provider upstream health from actual request outcomes.
// Does NOT ping upstream APIs (spec §16.4 invisibility contract).
// Ring buffer holds last 20 results per provider; status is computed on read.
type healthMonitor struct {
	mu      sync.RWMutex
	results map[types.Provider]*providerRing
}

// providerRing is a fixed-capacity ring buffer of request outcomes for one provider.
type providerRing struct {
	buf         [20]bool  // true = success, false = error
	head        int       // next write position
	count       int       // entries stored, capped at 20
	lastSuccess time.Time
	lastError   time.Time
}

func newHealthMonitor() *healthMonitor {
	return &healthMonitor{
		results: map[types.Provider]*providerRing{
			types.Anthropic:    {},
			types.OpenAI:       {},
			types.CodexChatGPT: {},
		},
	}
}

// anyDegraded reports whether at least one provider's current status
// is degraded or down. T83 visibility helper.
func (h *healthMonitor) anyDegraded() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for prov := range h.results {
		// We have to release the read lock to call getStatus which
		// takes its own RLock; same lock is reentrant for RWMutex
		// readers so the nested RLock is safe.
		info := h.getStatusLocked(prov)
		if info.Status == types.ProviderHealthDegraded || info.Status == types.ProviderHealthDown {
			return true
		}
	}
	return false
}

// record adds a request outcome for the given provider.
func (h *healthMonitor) record(prov types.Provider, success bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.results[prov]
	if !ok {
		return
	}
	r.buf[r.head] = success
	r.head = (r.head + 1) % 20
	if r.count < 20 {
		r.count++
	}
	now := time.Now()
	if success {
		r.lastSuccess = now
	} else {
		r.lastError = now
	}
}

// getStatus computes the current health snapshot for the given provider.
// Rules (spec §17.5):
//   - idle:     no activity in last 5 minutes
//   - down:     last 3 consecutive results all failed
//   - degraded: >20% error rate in stored window
//   - healthy:  otherwise
func (h *healthMonitor) getStatus(prov types.Provider) types.ProviderHealthInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.getStatusLocked(prov)
}

// getStatusLocked is getStatus without taking the lock. The caller must
// already hold h.mu (read or write). Used by helpers like anyDegraded
// that take the lock once and call into multiple per-provider lookups.
func (h *healthMonitor) getStatusLocked(prov types.Provider) types.ProviderHealthInfo {
	r, ok := h.results[prov]
	if !ok || r.count == 0 {
		return types.ProviderHealthInfo{Status: types.ProviderHealthIdle}
	}

	// Idle: no activity in the last 5 minutes.
	lastActivity := r.lastSuccess
	if r.lastError.After(lastActivity) {
		lastActivity = r.lastError
	}
	if time.Since(lastActivity) > 5*time.Minute {
		return types.ProviderHealthInfo{
			Status:      types.ProviderHealthIdle,
			LastSuccess: r.lastSuccess,
			LastError:   r.lastError,
		}
	}

	// Count errors in the stored window.
	errors := 0
	for i := 0; i < r.count; i++ {
		idx := (r.head - r.count + i + 20) % 20
		if !r.buf[idx] {
			errors++
		}
	}
	errorRate := float64(errors) / float64(r.count)

	// Down: last 3 consecutive results all failed.
	status := types.ProviderHealthHealthy
	if r.count >= 3 {
		allFailed := true
		for i := 0; i < 3; i++ {
			idx := (r.head - 1 - i + 20) % 20
			if r.buf[idx] {
				allFailed = false
				break
			}
		}
		if allFailed {
			status = types.ProviderHealthDown
		}
	}
	if status == types.ProviderHealthHealthy && errorRate > 0.20 {
		status = types.ProviderHealthDegraded
	}

	return types.ProviderHealthInfo{
		Status:      status,
		LastSuccess: r.lastSuccess,
		LastError:   r.lastError,
		ErrorRate:   errorRate,
	}
}

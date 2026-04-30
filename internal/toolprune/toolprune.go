// Package toolprune tracks per-session tool usage so the proxy can
// strip idle tool definitions from the request body before sending.
// T103. Pruning is gated by `[compression.tuning] tool_prune_enabled`
// so the default is no behavioural change; once a tool definition is
// pruned, T76 WP3 (opportunistic re-injection) handles re-attachment
// when the model references the local-archive URI.
package toolprune

import (
	"encoding/json"
	"sync"
	"time"
)

// UsageTracker holds per-session tool-name -> last-seen-turn maps with
// a sliding window. When a tool is observed inside the window, it is
// considered "active" and survives pruning.
type UsageTracker struct {
	mu             sync.Mutex
	idleThreshold  int
	maxSessions    int
	sessions       map[string]*sessionUsage
	prunedTotal    int64
	reattachTotal  int64
	tokensSavedSum int64
}

type sessionUsage struct {
	turn         int
	lastSeen     map[string]int
	lastActivity time.Time
	// prunedDefs caches the JSON bodies of tool definitions that were
	// pruned from this session so a follow-up turn that mentions the
	// tool name can reattach the original definition. T103b.
	prunedDefs map[string]json.RawMessage
}

// NewUsageTracker returns a tracker with the supplied idle threshold
// (number of turns a tool can go unused before being eligible to
// prune). idleThreshold <= 0 falls back to 20.
func NewUsageTracker(idleThreshold int) *UsageTracker {
	if idleThreshold <= 0 {
		idleThreshold = 20
	}
	return &UsageTracker{
		idleThreshold: idleThreshold,
		maxSessions:   1024,
		sessions:      make(map[string]*sessionUsage),
	}
}

// ObserveTurn records that the supplied tools were used in the
// session's current turn. Empty session id is a no-op. Tool names are
// case-sensitive: callers should normalise upstream if needed.
func (u *UsageTracker) ObserveTurn(sessionID string, toolNames []string) {
	if sessionID == "" {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	st, ok := u.sessions[sessionID]
	if !ok {
		if len(u.sessions) >= u.maxSessions {
			u.evictOldestLocked()
		}
		st = &sessionUsage{lastSeen: make(map[string]int)}
		u.sessions[sessionID] = st
	}
	st.turn++
	st.lastActivity = time.Now()
	for _, name := range toolNames {
		if name == "" {
			continue
		}
		st.lastSeen[name] = st.turn
	}
}

// Active reports whether toolName is still within the session's idle
// window. Unknown sessions / tools return true (fail-open) so a
// brand-new tool is never accidentally pruned.
func (u *UsageTracker) Active(sessionID, toolName string) bool {
	if sessionID == "" || toolName == "" {
		return true
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	st, ok := u.sessions[sessionID]
	if !ok {
		return true
	}
	last, seen := st.lastSeen[toolName]
	if !seen {
		return true
	}
	return st.turn-last <= u.idleThreshold
}

// MarkPruned bumps the cumulative pruned-tool counter and the saved
// token estimate. Used by the actual pruner side and surfaced via
// /admin/status.toolprune.
func (u *UsageTracker) MarkPruned(savedTokens int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.prunedTotal++
	if savedTokens > 0 {
		u.tokensSavedSum += int64(savedTokens)
	}
}

// MarkReattached bumps the reattach counter so operators can monitor
// how often re-injection rescues a pruned tool.
func (u *UsageTracker) MarkReattached() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reattachTotal++
}

// RememberPrunedDef caches the original JSON definition of a tool that
// was just pruned from the session's request body so a future turn can
// reattach it. Empty session id, name, or def is a no-op. T103b.
func (u *UsageTracker) RememberPrunedDef(sessionID, toolName string, def json.RawMessage) {
	if sessionID == "" || toolName == "" || len(def) == 0 {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	st, ok := u.sessions[sessionID]
	if !ok {
		if len(u.sessions) >= u.maxSessions {
			u.evictOldestLocked()
		}
		st = &sessionUsage{
			lastSeen:   make(map[string]int),
			prunedDefs: make(map[string]json.RawMessage),
		}
		u.sessions[sessionID] = st
	}
	if st.prunedDefs == nil {
		st.prunedDefs = make(map[string]json.RawMessage)
	}
	clone := make(json.RawMessage, len(def))
	copy(clone, def)
	st.prunedDefs[toolName] = clone
	st.lastActivity = time.Now()
}

// PrunedToolNames returns the list of tool names with cached pruned
// definitions for the given session. Used by callers to feed the
// reattach detector. T103b.
func (u *UsageTracker) PrunedToolNames(sessionID string) []string {
	if sessionID == "" {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	st, ok := u.sessions[sessionID]
	if !ok || len(st.prunedDefs) == 0 {
		return nil
	}
	out := make([]string, 0, len(st.prunedDefs))
	for n := range st.prunedDefs {
		out = append(out, n)
	}
	return out
}

// LookupPrunedDefs returns the previously-pruned definitions for the
// supplied tool names. Tools without a cached definition are simply
// omitted from the returned map. The returned definitions are removed
// from the cache so a subsequent request does not re-attach them
// twice. T103b.
func (u *UsageTracker) LookupPrunedDefs(sessionID string, names []string) map[string]json.RawMessage {
	if sessionID == "" || len(names) == 0 {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	st, ok := u.sessions[sessionID]
	if !ok || len(st.prunedDefs) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(names))
	for _, n := range names {
		if def, has := st.prunedDefs[n]; has {
			out[n] = def
			delete(st.prunedDefs, n)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Forget drops state for one session. Called when a session ends or
// when the operator clears it.
func (u *UsageTracker) Forget(sessionID string) {
	if sessionID == "" {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.sessions, sessionID)
}

// Stats returns the live tracker counters.
type Stats struct {
	Sessions       int   `json:"sessions"`
	PrunedTotal    int64 `json:"pruned_total"`
	ReattachTotal  int64 `json:"reattach_total"`
	TokensSavedSum int64 `json:"tokens_saved_sum"`
}

// Snapshot exposes the current counters for /admin/status surfaces.
func (u *UsageTracker) Snapshot() Stats {
	u.mu.Lock()
	defer u.mu.Unlock()
	return Stats{
		Sessions:       len(u.sessions),
		PrunedTotal:    u.prunedTotal,
		ReattachTotal:  u.reattachTotal,
		TokensSavedSum: u.tokensSavedSum,
	}
}

func (u *UsageTracker) evictOldestLocked() {
	oldest := ""
	oldestAt := time.Time{}
	for id, st := range u.sessions {
		if oldest == "" || st.lastActivity.Before(oldestAt) {
			oldest = id
			oldestAt = st.lastActivity
		}
	}
	if oldest != "" {
		delete(u.sessions, oldest)
	}
}

// PruneDecision describes the result of a single prune evaluation:
// which tools to keep, which to drop, and why.
type PruneDecision struct {
	Keep    []string
	Pruned  []string
}

// Decide returns a PruneDecision for the supplied tool list given the
// session's current usage. minKeep ensures at least N tools always
// stay attached even if all are idle (caller's safety net against
// over-pruning). minKeep <= 0 disables the floor.
func (u *UsageTracker) Decide(sessionID string, tools []string, minKeep int) PruneDecision {
	out := PruneDecision{
		Keep:   make([]string, 0, len(tools)),
		Pruned: make([]string, 0, len(tools)),
	}
	if sessionID == "" {
		out.Keep = append(out.Keep, tools...)
		return out
	}
	for _, t := range tools {
		if u.Active(sessionID, t) {
			out.Keep = append(out.Keep, t)
		} else {
			out.Pruned = append(out.Pruned, t)
		}
	}
	if minKeep > 0 && len(out.Keep) < minKeep {
		// Restore the most-recently-used pruned tools until the floor
		// is met. Most-recently-used = highest lastSeen turn.
		u.mu.Lock()
		st, ok := u.sessions[sessionID]
		if ok {
			ranking := make([]string, 0, len(out.Pruned))
			ranking = append(ranking, out.Pruned...)
			// Simple selection sort by lastSeen desc; tool counts are
			// always small (<50) so n^2 is fine.
			for i := 0; i < len(ranking); i++ {
				for j := i + 1; j < len(ranking); j++ {
					if st.lastSeen[ranking[j]] > st.lastSeen[ranking[i]] {
						ranking[i], ranking[j] = ranking[j], ranking[i]
					}
				}
			}
			for _, name := range ranking {
				if len(out.Keep) >= minKeep {
					break
				}
				out.Keep = append(out.Keep, name)
				// Drop from Pruned list.
				for i, p := range out.Pruned {
					if p == name {
						out.Pruned = append(out.Pruned[:i], out.Pruned[i+1:]...)
						break
					}
				}
			}
		}
		u.mu.Unlock()
	}
	return out
}

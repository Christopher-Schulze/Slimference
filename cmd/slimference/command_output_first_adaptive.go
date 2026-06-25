package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

// Adaptive per-command-class compaction budget — the cmd-side glue for the
// pure control in internal/filter/adaptive_budget.go. It maintains per-session,
// per-class re-fetch counters (reconnect-safe on disk) and decides, per
// invocation, whether the class has proven net-negative and should be
// full-passed. Fail-open everywhere: any error returns false, i.e. today's
// fixed L2 behavior. The on-disk state is a small JSON sidecar next to the
// command-output-first capture sidecar.
//
// Concurrency note: Codex tool calls are sequential within a session, so the
// read-modify-write here is race-free in practice; a rare concurrent overlap
// degrades to a lost increment, which only makes the conservative control even
// more conservative (it never over-suppresses). Writes are atomic (temp+rename)
// so the file is never torn.

// commandOutputFirstRefetchSeenCap bounds the per-session set of seen command
// identities. Past the cap, new identities are not recorded, so an undetected
// re-fetch is undercounted — the conservative direction (keeps compacting).
const commandOutputFirstRefetchSeenCap = 4096

type commandOutputFirstClassCounts struct {
	Applied int `json:"applied"`
	Refetch int `json:"refetch"`
}

type commandOutputFirstRefetchState struct {
	Classes map[string]commandOutputFirstClassCounts `json:"classes"`
	Seen    []string                                 `json:"seen"`
	seenSet map[string]struct{}                      // built on load; not serialized
}

func (s *commandOutputFirstRefetchState) seenHas(identity string) bool {
	_, ok := s.seenSet[identity]
	return ok
}

func (s *commandOutputFirstRefetchState) seenAdd(identity string) {
	if len(s.Seen) >= commandOutputFirstRefetchSeenCap {
		return
	}
	s.Seen = append(s.Seen, identity)
	s.seenSet[identity] = struct{}{}
}

// commandOutputFirstAdaptiveFullPass records this invocation in the per-session
// re-fetch counters and reports whether the class should be full-passed (the
// adaptive budget proved it net-negative). compactedRatio is the marginal
// compaction ratio of this invocation (compacted_bytes / raw_bytes). The
// decision uses the PRIOR counts so an invocation never biases its own choice.
func commandOutputFirstAdaptiveFullPass(command string, args []string, compactedRatio float64) bool {
	sessionID := strings.TrimSpace(os.Getenv(commandOutputFirstSessionEnv))
	if sessionID == "" {
		return false // no scoped session context -> fixed behavior
	}
	homeDir, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return false
	}
	path := filepath.Join(homeDir, ".slimference", "analytics", "command_output_first_refetch_"+sessionID+".json")
	state := loadCommandOutputFirstRefetchState(path)

	counts := state.Classes[command]
	// Decide from the prior rate (before recording this invocation).
	fullPass := filter.AdaptiveCompactionShouldFullPass(counts.Applied, counts.Refetch, compactedRatio)

	// A repeat of an already-applied identity is a re-fetch of a previously
	// compacted output; a fresh identity is a new apply.
	identity := commandOutputFirstRefetchIdentity(command, args)
	if state.seenHas(identity) {
		counts.Refetch++
	} else {
		counts.Applied++
		state.seenAdd(identity)
	}
	state.Classes[command] = counts
	saveCommandOutputFirstRefetchState(path, state)
	return fullPass
}

// commandOutputFirstCompactionRatio is the marginal compaction ratio
// (compacted_bytes / raw_bytes) used as the per-class break-even threshold. An
// empty raw stream returns 1.0 (no compaction value -> demotes readily).
func commandOutputFirstCompactionRatio(raw, compacted []byte) float64 {
	if len(raw) <= 0 {
		return 1.0
	}
	return float64(len(compacted)) / float64(len(raw))
}

func commandOutputFirstRefetchIdentity(command string, args []string) string {
	cwd, _ := os.Getwd()
	h := sha256.New()
	h.Write([]byte(command))
	h.Write([]byte{0})
	for _, a := range args {
		h.Write([]byte(a))
		h.Write([]byte{0})
	}
	h.Write([]byte(cwd))
	return hex.EncodeToString(h.Sum(nil))
}

func loadCommandOutputFirstRefetchState(path string) *commandOutputFirstRefetchState {
	state := &commandOutputFirstRefetchState{
		Classes: map[string]commandOutputFirstClassCounts{},
		seenSet: map[string]struct{}{},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	var loaded commandOutputFirstRefetchState
	if err := json.Unmarshal(data, &loaded); err != nil {
		return state // corrupt -> start fresh (fail-open)
	}
	if loaded.Classes != nil {
		state.Classes = loaded.Classes
	}
	state.Seen = loaded.Seen
	for _, id := range loaded.Seen {
		state.seenSet[id] = struct{}{}
	}
	return state
}

func saveCommandOutputFirstRefetchState(path string, state *commandOutputFirstRefetchState) {
	if err := osMkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, path) // atomic replace; never tears the file
}

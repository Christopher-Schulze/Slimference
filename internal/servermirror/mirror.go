// Package servermirror tracks, per WSS session, the content Slimference has
// already forwarded upstream (= what the OpenAI Responses server now holds along
// the previous_response_id chain). A later client frame can then be diffed
// against known server state to reference content the model provably already
// has, which is lossless and therefore zero-drawdown by construction.
//
// This is the SHADOW core (T254 design + shadow gate): it OBSERVES forwarded
// content and PREDICTS potential savings. It never mutates a frame. The
// mutation gate (actually replacing frames using the mirror) is a separate,
// later, live-proven step.
//
// Safety invariant (no-false-elision): Predict marks a block as already-on-server
// ONLY when its exact content hash was recorded by a prior Observe for the same
// session. Eviction/bounding can only make Predict UNDER-report savings (mark a
// truly-forwarded block as novel); it can never cause a false elision.
package servermirror

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/slimference/slimference/internal/types"
)

// maxBlocksPerSession bounds the per-session hash set so a long conversation
// cannot grow memory without limit. When full, new hashes are not recorded,
// which only under-reports future savings (never a false elision).
const maxBlocksPerSession = 50000

// Mirror is a concurrency-safe, per-session record of forwarded content hashes.
type Mirror struct {
	mu       sync.Mutex
	sessions map[string]map[string]struct{}
}

// New returns an empty Mirror.
func New() *Mirror {
	return &Mirror{sessions: make(map[string]map[string]struct{})}
}

// Observe records the content of msgs as now held by the server for sessionID.
// Only non-empty text blocks are recorded. Call this with exactly the messages
// Slimference forwarded upstream (post-mutation), never with local file bytes or
// unforwarded content.
func (m *Mirror) Observe(sessionID string, msgs []types.Message) {
	if m == nil || sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	set := m.sessions[sessionID]
	if set == nil {
		set = make(map[string]struct{})
		m.sessions[sessionID] = set
	}
	for _, msg := range msgs {
		for _, b := range msg.Content {
			if b.Text == "" {
				continue
			}
			if len(set) >= maxBlocksPerSession {
				return
			}
			set[hashContent(b.Text)] = struct{}{}
		}
	}
}

// Prediction classifies one content block of a new client frame.
type Prediction struct {
	Block            int
	AlreadyForwarded bool
	Bytes            int
}

// Report summarises a Predict pass. PotentialSavedBytes is the byte total of
// blocks the server already holds (referenceable losslessly); it is a SHADOW
// estimate, no frame is changed.
type Report struct {
	Blocks              int
	ReferenceableBlocks int
	PotentialSavedBytes int
	Predictions         []Prediction
}

// Predict reports, without mutating, which blocks of msgs the server already
// holds for sessionID (and are therefore losslessly referenceable) versus novel
// content that must be sent. It enforces no-false-elision: a block is marked
// AlreadyForwarded only if its exact content hash was previously Observed.
func (m *Mirror) Predict(sessionID string, msgs []types.Message) Report {
	var rep Report
	if m == nil || sessionID == "" {
		// Count blocks so callers still see the shape; nothing referenceable.
		for _, msg := range msgs {
			for _, b := range msg.Content {
				if b.Text == "" {
					continue
				}
				rep.Blocks++
			}
		}
		return rep
	}
	m.mu.Lock()
	set := m.sessions[sessionID]
	known := make(map[string]struct{}, len(set))
	for h := range set {
		known[h] = struct{}{}
	}
	m.mu.Unlock()

	blockIdx := 0
	for _, msg := range msgs {
		for _, b := range msg.Content {
			if b.Text == "" {
				continue
			}
			rep.Blocks++
			idx := blockIdx
			blockIdx++
			_, forwarded := known[hashContent(b.Text)]
			rep.Predictions = append(rep.Predictions, Prediction{
				Block:            idx,
				AlreadyForwarded: forwarded,
				Bytes:            len(b.Text),
			})
			if forwarded {
				rep.ReferenceableBlocks++
				rep.PotentialSavedBytes += len(b.Text)
			}
		}
	}
	return rep
}

// Reset clears a session's recorded state (e.g. on cache flush).
func (m *Mirror) Reset(sessionID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
}

func hashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

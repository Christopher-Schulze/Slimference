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
// Safety invariant (no-false-elision): Predict marks an exact block as
// already-on-server ONLY when its exact content hash was recorded by a prior
// Observe for the same session. The normalized shadow path applies the same rule
// to normalized text segments such as Codex exec payloads with volatile headers
// stripped. Eviction/bounding can only make Predict UNDER-report savings (mark a
// truly-forwarded block/segment as novel); it can never cause a false elision.
package servermirror

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// maxBlocksPerSession bounds the per-session hash set so a long conversation
// cannot grow memory without limit. When full, new hashes are not recorded,
// which only under-reports future savings (never a false elision).
const maxBlocksPerSession = 50000

// Mirror is a concurrency-safe, per-session record of forwarded content hashes.
type Mirror struct {
	mu                 sync.Mutex
	sessions           map[string]map[string]struct{}
	normalizedSessions map[string]map[string]struct{}
}

// New returns an empty Mirror.
func New() *Mirror {
	return &Mirror{
		sessions:           make(map[string]map[string]struct{}),
		normalizedSessions: make(map[string]map[string]struct{}),
	}
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
	normalizedSet := m.normalizedSessions[sessionID]
	if normalizedSet == nil {
		normalizedSet = make(map[string]struct{})
		m.normalizedSessions[sessionID] = normalizedSet
	}
	for _, segment := range normalizedSegments(msgs) {
		if len(normalizedSet) >= maxBlocksPerSession {
			return
		}
		normalizedSet[hashContent(segment.Text)] = struct{}{}
	}
}

// Prediction classifies one content block of a new client frame.
type Prediction struct {
	Block            int
	AlreadyForwarded bool
	Bytes            int
}

// SegmentPrediction classifies a normalized text segment of a new client frame.
// It is shadow-only: normalized segments are never substituted into a frame.
type SegmentPrediction struct {
	Block            int
	Segment          int
	Kind             string
	AlreadyForwarded bool
	Bytes            int
}

// SegmentKindReport groups normalized shadow predictions by content kind.
type SegmentKindReport struct {
	Segments              int
	ReferenceableSegments int
	Bytes                 int
	PotentialSavedBytes   int
}

// Report summarises a Predict pass. PotentialSavedBytes is the byte total of
// blocks the server already holds (referenceable losslessly); it is a SHADOW
// estimate, no frame is changed.
type Report struct {
	Blocks                              int
	BlockBytes                          int
	ReferenceableBlocks                 int
	PotentialSavedBytes                 int
	NormalizedSegments                  int
	NormalizedBytes                     int
	NormalizedReferenceableSegments     int
	NormalizedPotentialSavedBytes       int
	Predictions                         []Prediction
	NormalizedPredictions               []SegmentPrediction
	NormalizedPotentialSavedBytesByKind map[string]SegmentKindReport
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
	normalizedSet := m.normalizedSessions[sessionID]
	normalizedKnown := make(map[string]struct{}, len(normalizedSet))
	for h := range normalizedSet {
		normalizedKnown[h] = struct{}{}
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
			rep.BlockBytes += len(b.Text)
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
	for _, segment := range normalizedSegments(msgs) {
		rep.NormalizedSegments++
		rep.NormalizedBytes += len(segment.Text)
		_, forwarded := normalizedKnown[hashContent(segment.Text)]
		rep.NormalizedPredictions = append(rep.NormalizedPredictions, SegmentPrediction{
			Block:            segment.Block,
			Segment:          segment.Segment,
			Kind:             segment.Kind,
			AlreadyForwarded: forwarded,
			Bytes:            len(segment.Text),
		})
		if rep.NormalizedPotentialSavedBytesByKind == nil {
			rep.NormalizedPotentialSavedBytesByKind = map[string]SegmentKindReport{}
		}
		kindReport := rep.NormalizedPotentialSavedBytesByKind[segment.Kind]
		kindReport.Segments++
		kindReport.Bytes += len(segment.Text)
		if forwarded {
			rep.NormalizedReferenceableSegments++
			rep.NormalizedPotentialSavedBytes += len(segment.Text)
			kindReport.ReferenceableSegments++
			kindReport.PotentialSavedBytes += len(segment.Text)
		}
		rep.NormalizedPotentialSavedBytesByKind[segment.Kind] = kindReport
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
	delete(m.normalizedSessions, sessionID)
	m.mu.Unlock()
}

func hashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type normalizedSegment struct {
	Block   int
	Segment int
	Kind    string
	Text    string
}

func normalizedSegments(msgs []types.Message) []normalizedSegment {
	var out []normalizedSegment
	blockIdx := 0
	for _, msg := range msgs {
		for _, b := range msg.Content {
			if b.Text == "" {
				continue
			}
			if _, payload, ok := splitCodexExecEnvelope(b.Text); ok {
				out = append(out, normalizedSegment{
					Block:   blockIdx,
					Segment: 0,
					Kind:    "codex_exec_payload",
					Text:    payload,
				})
			} else {
				out = append(out, normalizedSegment{
					Block:   blockIdx,
					Segment: 0,
					Kind:    normalizedSegmentKind(msg, b),
					Text:    b.Text,
				})
			}
			blockIdx++
		}
	}
	return out
}

func normalizedSegmentKind(msg types.Message, block types.ContentBlock) string {
	if kind := strings.TrimSpace(block.Type); kind != "" {
		return kind
	}
	if role := strings.TrimSpace(msg.Role); role != "" {
		return role
	}
	return "text"
}

func splitCodexExecEnvelope(text string) (header, payload string, ok bool) {
	if !strings.Contains(text, "Process exited with code ") {
		return "", "", false
	}
	for _, marker := range []string{"\nOutput:\n", "\r\nOutput:\r\n"} {
		idx := strings.Index(text, marker)
		if idx < 0 {
			continue
		}
		header = text[:idx+len(marker)]
		payload = text[idx+len(marker):]
		return header, payload, payload != ""
	}
	return "", "", false
}

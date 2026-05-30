package chunkdedup

import (
	"bytes"
	"fmt"
	"sync"
)

// ArchiveFunc persists a chunk so a reference to it can later be expanded, and
// returns the recovery URI the model is told (via the recovery contract) it may
// request. The proxy wires this to internal/contentarchive; tests inject a fake.
type ArchiveFunc func(sessionID, chunkID string, chunk []byte) string

// Store deduplicates content at chunk granularity within a session: chunks
// already sent to the model in this session are replaced by a compact,
// recoverable reference. Safe for concurrent sessions.
type Store struct {
	cfg     Config
	archive ArchiveFunc
	mu      sync.Mutex
	seen    map[string]map[string]struct{} // sessionID -> chunk ids already sent
}

// NewStore returns a chunk-dedup store. archive may be nil (references then carry
// an empty URI and are not recoverable; production always supplies one).
func NewStore(cfg Config, archive ArchiveFunc) *Store {
	return &Store{cfg: cfg, archive: archive, seen: map[string]map[string]struct{}{}}
}

// Encode chunks data and replaces chunks already sent in this session with a
// recoverable reference. Returns the encoded bytes and the number of bytes
// saved. Every chunk of data is recorded as sent (so a later identical chunk can
// be referenced) regardless of whether this call itself saved anything. When no
// net saving is possible the original data is returned with 0; the dedup state
// is still updated because the model received the full data.
func (s *Store) Encode(sessionID string, data []byte) ([]byte, int) {
	if s == nil || sessionID == "" || len(data) == 0 {
		return data, 0
	}
	chunks := Chunk(data, s.cfg)

	s.mu.Lock()
	sessionSeen := s.seen[sessionID]
	if sessionSeen == nil {
		sessionSeen = make(map[string]struct{})
		s.seen[sessionID] = sessionSeen
	}
	var out bytes.Buffer
	out.Grow(len(data))
	saved := 0
	for _, c := range chunks {
		id := ChunkID(c)
		if _, seenBefore := sessionSeen[id]; seenBefore {
			uri := ""
			if s.archive != nil {
				uri = s.archive(sessionID, id, c)
			}
			ref := fmt.Sprintf("[unchanged region: %s]", uri)
			if len(ref) < len(c) {
				out.WriteString(ref)
				saved += len(c) - len(ref)
				continue
			}
		}
		out.Write(c)
		sessionSeen[id] = struct{}{}
	}
	s.mu.Unlock()

	if saved <= 0 {
		return data, 0
	}
	return out.Bytes(), saved
}

// Reset clears a session's seen set (e.g. on cache flush).
func (s *Store) Reset(sessionID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.seen, sessionID)
	s.mu.Unlock()
}

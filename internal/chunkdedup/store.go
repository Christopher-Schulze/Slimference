package chunkdedup

import (
	"bytes"
	"sync"
	"time"
)

// ArchiveFunc persists a chunk so a reference to it can later be expanded, and
// returns the recovery URI the model is told (via the recovery contract) it may
// request. The proxy wires this to internal/contentarchive; tests inject a fake.
type ArchiveFunc func(sessionID, chunkID string, chunk []byte) string

// StoreLimits bound the in-memory chunk identity store. Zero fields fall back
// to conservative defaults.
type StoreLimits struct {
	MaxSessions         int
	MaxChunksPerSession int
	TTL                 time.Duration
}

const (
	defaultMaxSessions         = 256
	defaultMaxChunksPerSession = 8192
	defaultTTL                 = 4 * time.Hour
)

func (l StoreLimits) normalized() StoreLimits {
	if l.MaxSessions <= 0 {
		l.MaxSessions = defaultMaxSessions
	}
	if l.MaxChunksPerSession <= 0 {
		l.MaxChunksPerSession = defaultMaxChunksPerSession
	}
	if l.TTL <= 0 {
		l.TTL = defaultTTL
	}
	return l
}

// Store deduplicates content at chunk granularity within a session: chunks
// already sent to the model in this session are replaced by a compact,
// recoverable reference. Safe for concurrent sessions.
type Store struct {
	cfg     Config
	limits  StoreLimits
	archive ArchiveFunc
	now     func() time.Time

	mu       sync.Mutex
	sessions map[string]*sessionChunks
}

type sessionChunks struct {
	chunks   map[string]chunkState
	lastSeen time.Time
	seq      uint64
}

type chunkState struct {
	lastSeen time.Time
	seq      uint64
}

// NewStore returns a chunk-dedup store. When archive is nil or returns an empty
// URI, Encode fails open and keeps repeated chunks verbatim so it never emits an
// unrecoverable reference.
func NewStore(cfg Config, archive ArchiveFunc) *Store {
	return NewStoreWithLimits(cfg, StoreLimits{}, archive)
}

// NewStoreWithLimits returns a Store with explicit bounds. It is primarily used
// by tests and by the proxy, which wires operator-configured limits.
func NewStoreWithLimits(cfg Config, limits StoreLimits, archive ArchiveFunc) *Store {
	return &Store{
		cfg:      cfg,
		limits:   limits.normalized(),
		archive:  archive,
		now:      time.Now,
		sessions: map[string]*sessionChunks{},
	}
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
	ids := make([]string, len(chunks))
	for i, c := range chunks {
		ids[i] = ChunkID(c)
	}

	now := s.now()
	repeated := make([]bool, len(chunks))
	s.mu.Lock()
	s.pruneExpiredLocked(now)
	session := s.sessions[sessionID]
	if session == nil {
		session = &sessionChunks{chunks: make(map[string]chunkState)}
		s.sessions[sessionID] = session
	}
	session.lastSeen = now
	for i, id := range ids {
		if _, seenBefore := session.chunks[id]; seenBefore {
			repeated[i] = true
		}
		session.seq++
		session.chunks[id] = chunkState{lastSeen: now, seq: session.seq}
	}
	s.pruneSessionLocked(session)
	s.pruneSessionsLocked()
	s.mu.Unlock()

	var out bytes.Buffer
	out.Grow(len(data))
	saved := 0
	for i, c := range chunks {
		if repeated[i] {
			if s.archive != nil {
				uri := s.archive(sessionID, ids[i], c)
				if uri != "" {
					ref := FormatReference(uri, len(c))
					if len(ref) < len(c) {
						out.WriteString(ref)
						saved += len(c) - len(ref)
						continue
					}
				}
			}
		}
		out.Write(c)
	}

	if saved <= 0 {
		return data, 0
	}
	return out.Bytes(), saved
}

func (s *Store) pruneExpiredLocked(now time.Time) {
	cutoff := now.Add(-s.limits.TTL)
	for sessionID, session := range s.sessions {
		if session.lastSeen.Before(cutoff) {
			delete(s.sessions, sessionID)
			continue
		}
		for id, state := range session.chunks {
			if state.lastSeen.Before(cutoff) {
				delete(session.chunks, id)
			}
		}
		if len(session.chunks) == 0 {
			delete(s.sessions, sessionID)
		}
	}
}

func (s *Store) pruneSessionLocked(session *sessionChunks) {
	excess := len(session.chunks) - s.limits.MaxChunksPerSession
	if excess <= 0 {
		return
	}
	for excess > 0 {
		var oldestID string
		var oldest chunkState
		first := true
		for id, state := range session.chunks {
			if first || state.seq < oldest.seq {
				oldestID = id
				oldest = state
				first = false
			}
		}
		if oldestID == "" {
			return
		}
		delete(session.chunks, oldestID)
		excess--
	}
}

func (s *Store) pruneSessionsLocked() {
	excess := len(s.sessions) - s.limits.MaxSessions
	for excess > 0 {
		var oldestID string
		var oldest time.Time
		first := true
		for id, session := range s.sessions {
			if first || session.lastSeen.Before(oldest) {
				oldestID = id
				oldest = session.lastSeen
				first = false
			}
		}
		if oldestID == "" {
			return
		}
		delete(s.sessions, oldestID)
		excess--
	}
}

// Reset clears a session's seen set (e.g. on cache flush).
func (s *Store) Reset(sessionID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

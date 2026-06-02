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
	MaxSessionRefPct    int
}

const (
	defaultMaxSessions         = 256
	defaultMaxChunksPerSession = 8192
	defaultTTL                 = 4 * time.Hour
	defaultMaxSessionRefPct    = 100
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
	if l.MaxSessionRefPct <= 0 {
		l.MaxSessionRefPct = defaultMaxSessionRefPct
	}
	if l.MaxSessionRefPct > 100 {
		l.MaxSessionRefPct = 100
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
	refBytes int
	inBytes  int
}

type chunkState struct {
	lastSeen time.Time
	seq      uint64
}

// EncodeResult describes a chunk-dedup attempt without exposing content. Data is
// the byte stream that should be sent upstream; if Saved is zero it is the
// original input.
type EncodeResult struct {
	Data            []byte
	Saved           int
	ReferenceCount  int
	ReferencedBytes int
	Verified        bool
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
	result := s.EncodeWithReport(sessionID, data)
	return result.Data, result.Saved
}

// EncodeWithReport is Encode plus content-free metadata for tests and callers
// that need to audit chunk-reference density. Any non-verifiable reference set
// fails open to the original input.
func (s *Store) EncodeWithReport(sessionID string, data []byte) EncodeResult {
	return s.EncodeWithReportWithMaxReferencePercent(sessionID, data, 100)
}

// EncodeWithReportWithMaxReferencePercent applies a per-output reference-density
// cap before a candidate is accepted into the cumulative session reference
// budget. The session budget denominator counts every observed output passed to
// Encode, including first-send seed outputs and rejected candidates that
// full-pass. Those bytes are model-visible context and should increase the safe
// budget for later references.
func (s *Store) EncodeWithReportWithMaxReferencePercent(sessionID string, data []byte, maxReferencePercent int) EncodeResult {
	if s == nil || sessionID == "" || len(data) == 0 {
		return EncodeResult{Data: data}
	}
	if maxReferencePercent <= 0 || maxReferencePercent > 100 {
		maxReferencePercent = 100
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
	session.inBytes += len(data)
	for i, id := range ids {
		if _, seenBefore := session.chunks[id]; seenBefore {
			repeated[i] = true
		}
	}
	for _, id := range ids {
		session.seq++
		session.chunks[id] = chunkState{lastSeen: now, seq: session.seq}
	}
	s.pruneSessionLocked(session)
	s.pruneSessionsLocked()
	s.mu.Unlock()

	var out bytes.Buffer
	out.Grow(len(data))
	saved := 0
	referenceCount := 0
	referencedBytes := 0
	expansions := map[string][]byte{}
	for i, c := range chunks {
		if repeated[i] {
			if s.archive != nil {
				uri := s.archive(sessionID, ids[i], c)
				if uri != "" {
					ref := FormatReference(uri, len(c))
					if len(ref) < len(c) {
						out.WriteString(ref)
						saved += len(c) - len(ref)
						referenceCount++
						referencedBytes += len(c)
						expansions[uri] = append([]byte(nil), c...)
						continue
					}
				}
			}
		}
		out.Write(c)
	}

	if saved <= 0 {
		return EncodeResult{Data: data}
	}
	encoded := out.Bytes()
	decoded, changed := DecodeReferences(string(encoded), func(uri string) ([]byte, bool) {
		chunk, ok := expansions[uri]
		return chunk, ok
	})
	if !changed || !bytes.Equal([]byte(decoded), data) {
		return EncodeResult{Data: data}
	}
	if referencedBytes*100 > len(data)*maxReferencePercent {
		return EncodeResult{Data: data}
	}
	if !s.recordReferenceBudget(sessionID, referencedBytes) {
		return EncodeResult{Data: data}
	}
	return EncodeResult{
		Data:            encoded,
		Saved:           saved,
		ReferenceCount:  referenceCount,
		ReferencedBytes: referencedBytes,
		Verified:        true,
	}
}

func (s *Store) recordReferenceBudget(sessionID string, referencedBytes int) bool {
	if s == nil || sessionID == "" || referencedBytes <= 0 {
		return true
	}
	limit := s.limits.MaxSessionRefPct
	if limit <= 0 || limit >= 100 {
		s.mu.Lock()
		if session := s.sessions[sessionID]; session != nil {
			session.refBytes += referencedBytes
		}
		s.mu.Unlock()
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[sessionID]
	if session == nil {
		return true
	}
	nextInput := session.inBytes
	nextRef := session.refBytes + referencedBytes
	if nextInput > 0 && nextRef*100 > nextInput*limit {
		return false
	}
	session.refBytes = nextRef
	return true
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

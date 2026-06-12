package chunkdedup

import (
	"bytes"
	"sort"
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

type chunkPlan struct {
	chunks   [][]byte
	ids      []string
	repeated []bool
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

// Observe records model-visible data without emitting references. Use this when
// a caller deliberately full-passes a chunk-dedup candidate but still needs the
// store's session denominator and seen-chunk state to match what the model saw.
func (s *Store) Observe(sessionID string, data []byte) {
	if s == nil || sessionID == "" || len(data) == 0 {
		return
	}
	fastPlan := newChunkPlan(Chunk(data, s.cfg), "")
	linePlan, hasLinePlan := newLineChunkPlan(data, s.cfg)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	session := s.sessions[sessionID]
	if session == nil {
		session = &sessionChunks{chunks: make(map[string]chunkState)}
		s.sessions[sessionID] = session
	}
	session.lastSeen = now
	session.inBytes += len(data)
	seedPlanLocked(session, fastPlan, now)
	if hasLinePlan {
		seedPlanLocked(session, linePlan, now)
	}
	s.pruneSessionLocked(session)
	s.pruneSessionsLocked()
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
	fastPlan := newChunkPlan(Chunk(data, s.cfg), "")
	linePlan, hasLinePlan := newLineChunkPlan(data, s.cfg)

	now := s.now()
	sessionReferenceLimit := len(data)
	s.mu.Lock()
	s.pruneExpiredLocked(now)
	session := s.sessions[sessionID]
	if session == nil {
		session = &sessionChunks{chunks: make(map[string]chunkState)}
		s.sessions[sessionID] = session
	}
	session.lastSeen = now
	session.inBytes += len(data)
	sessionReferenceLimit = s.remainingReferenceBudgetLocked(session)
	markRepeatedLocked(session, &fastPlan)
	if hasLinePlan {
		markRepeatedLocked(session, &linePlan)
	}
	seedPlanLocked(session, fastPlan, now)
	if hasLinePlan {
		seedPlanLocked(session, linePlan, now)
	}
	s.pruneSessionLocked(session)
	s.pruneSessionsLocked()
	s.mu.Unlock()

	if s.archive == nil {
		return EncodeResult{Data: data}
	}
	maxReferenceBytes := len(data) * maxReferencePercent / 100
	if sessionReferenceLimit < maxReferenceBytes {
		maxReferenceBytes = sessionReferenceLimit
	}
	if maxReferenceBytes <= 0 {
		return EncodeResult{Data: data}
	}

	bestPlan := fastPlan
	best := encodePlan(data, fastPlan, maxReferenceBytes, func(id string, _ []byte) (string, bool) {
		return "local-archive://" + id, true
	})
	if hasLinePlan {
		line := encodePlan(data, linePlan, maxReferenceBytes, func(id string, _ []byte) (string, bool) {
			return "local-archive://" + id, true
		})
		if line.Saved > best.Saved {
			bestPlan = linePlan
			best = line
		}
	}
	if best.Saved <= 0 {
		return EncodeResult{Data: data}
	}
	result := encodePlan(data, bestPlan, maxReferenceBytes, func(id string, chunk []byte) (string, bool) {
		uri := s.archive(sessionID, id, chunk)
		return uri, uri != ""
	})
	if result.Saved <= 0 {
		return EncodeResult{Data: data}
	}
	if !s.recordReferenceBudget(sessionID, result.ReferencedBytes) {
		return EncodeResult{Data: data}
	}
	return result
}

func newChunkPlan(chunks [][]byte, prefix string) chunkPlan {
	plan := chunkPlan{
		chunks:   chunks,
		ids:      make([]string, len(chunks)),
		repeated: make([]bool, len(chunks)),
	}
	for i, c := range chunks {
		plan.ids[i] = prefix + ChunkID(c)
	}
	return plan
}

func markRepeatedLocked(session *sessionChunks, plan *chunkPlan) {
	for i, id := range plan.ids {
		if _, seenBefore := session.chunks[id]; seenBefore {
			plan.repeated[i] = true
		}
	}
}

func seedPlanLocked(session *sessionChunks, plan chunkPlan, now time.Time) {
	for _, id := range plan.ids {
		session.seq++
		session.chunks[id] = chunkState{lastSeen: now, seq: session.seq}
	}
}

func (s *Store) remainingReferenceBudgetLocked(session *sessionChunks) int {
	limit := s.limits.MaxSessionRefPct
	if limit <= 0 || limit >= 100 {
		return int(^uint(0) >> 1)
	}
	maxRef := session.inBytes * limit / 100
	remaining := maxRef - session.refBytes
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ReferenceBudgetAvailable reports whether the session still has enough
// cumulative reference budget for another useful reference. It is content-free
// and lets the runtime policy full-pass chunk-dedup before spending hot-path
// CPU on an encode that would be rejected by the session integrity budget.
func (s *Store) ReferenceBudgetAvailable(sessionID string, minReferenceBytes int) bool {
	return s.ReferenceBudgetAvailableAfterInput(sessionID, 0, minReferenceBytes)
}

// ReferenceBudgetAvailableAfterInput is ReferenceBudgetAvailable evaluated as
// if inputBytes were about to be observed as model-facing context. This mirrors
// Encode's integrity-budget denominator without mutating store state.
func (s *Store) ReferenceBudgetAvailableAfterInput(sessionID string, inputBytes, minReferenceBytes int) bool {
	if s == nil || sessionID == "" {
		return false
	}
	if minReferenceBytes <= 0 {
		minReferenceBytes = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[sessionID]
	if session == nil {
		return true
	}
	limit := s.limits.MaxSessionRefPct
	if limit <= 0 || limit >= 100 {
		return true
	}
	if inputBytes < 0 {
		inputBytes = 0
	}
	maxRef := (session.inBytes + inputBytes) * limit / 100
	remaining := maxRef - session.refBytes
	return remaining >= minReferenceBytes
}

func encodePlan(data []byte, plan chunkPlan, maxReferenceBytes int, archive func(id string, chunk []byte) (string, bool)) EncodeResult {
	var out bytes.Buffer
	out.Grow(len(data))
	saved := 0
	referenceCount := 0
	referencedBytes := 0
	expansions := map[string][]byte{}
	selectedReferences := selectReferenceIndexes(plan, maxReferenceBytes)
	for i, c := range plan.chunks {
		if _, selected := selectedReferences[i]; selected && referencedBytes+len(c) <= maxReferenceBytes {
			if uri, ok := archive(plan.ids[i], c); ok {
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
	return EncodeResult{
		Data:            encoded,
		Saved:           saved,
		ReferenceCount:  referenceCount,
		ReferencedBytes: referencedBytes,
		Verified:        true,
	}
}

type referenceCandidate struct {
	index int
	bytes int
	saved int
}

func selectReferenceIndexes(plan chunkPlan, maxReferenceBytes int) map[int]struct{} {
	if maxReferenceBytes <= 0 {
		return nil
	}
	candidates := make([]referenceCandidate, 0, len(plan.chunks))
	for i, c := range plan.chunks {
		if !plan.repeated[i] || len(c) > maxReferenceBytes {
			continue
		}
		ref := FormatReference("local-archive://"+plan.ids[i], len(c))
		saved := len(c) - len(ref)
		if saved <= 0 {
			continue
		}
		candidates = append(candidates, referenceCandidate{
			index: i,
			bytes: len(c),
			saved: saved,
		})
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].saved != candidates[j].saved {
			return candidates[i].saved > candidates[j].saved
		}
		if candidates[i].bytes != candidates[j].bytes {
			return candidates[i].bytes > candidates[j].bytes
		}
		return candidates[i].index < candidates[j].index
	})
	selected := make(map[int]struct{}, len(candidates))
	referencedBytes := 0
	for _, c := range candidates {
		if referencedBytes+c.bytes > maxReferenceBytes {
			continue
		}
		selected[c.index] = struct{}{}
		referencedBytes += c.bytes
	}
	return selected
}

const (
	lineChunkIDPrefix = "line-"
	lineChunkMinLines = 32
)

func newLineChunkPlan(data []byte, cfg Config) (chunkPlan, bool) {
	chunks, ok := lineChunks(data, cfg)
	if !ok {
		return chunkPlan{}, false
	}
	return newChunkPlan(chunks, lineChunkIDPrefix), true
}

func lineChunks(data []byte, cfg Config) ([][]byte, bool) {
	if len(data) == 0 || bytes.Count(data, []byte{'\n'}) < lineChunkMinLines {
		return nil, false
	}
	min, avg, max, _, _ := cfg.normalized()
	if min < 512 {
		min = 512
	}
	if avg < min {
		avg = min
	}
	if max < avg {
		max = avg
	}
	var chunks [][]byte
	blockStart := 0
	lineStart := 0
	lineCount := 0
	for lineStart < len(data) {
		lineEnd := lineStart
		for lineEnd < len(data) && data[lineEnd] != '\n' {
			lineEnd++
		}
		if lineEnd < len(data) {
			lineEnd++
		}
		line := data[lineStart:lineEnd]
		lineCount++
		blockLen := lineEnd - blockStart
		if blockLen >= max || (blockLen >= avg && lineBoundary(line)) {
			chunks = append(chunks, data[blockStart:lineEnd:lineEnd])
			blockStart = lineEnd
		}
		lineStart = lineEnd
	}
	if blockStart < len(data) {
		chunks = append(chunks, data[blockStart:len(data):len(data)])
	}
	if lineCount < lineChunkMinLines || len(chunks) < 2 {
		return nil, false
	}
	return chunks, true
}

func lineBoundary(line []byte) bool {
	hash := uint64(1469598103934665603)
	for _, b := range line {
		hash ^= uint64(b)
		hash *= 1099511628211
	}
	return hash&lowMask(3) == 0
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

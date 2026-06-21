package sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SessionKey string
type TurnKey string

type ToolObservation struct {
	Name       string    `json:"name"`
	Command    string    `json:"command,omitempty"`
	Decision   string    `json:"decision,omitempty"`
	ArchiveID  string    `json:"archive_id,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

type FileObservation struct {
	Path       string    `json:"path"`
	Operation  string    `json:"operation"`
	Hash       string    `json:"hash,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

type GitPathListObservation struct {
	Source      string    `json:"source"`
	CWD         string    `json:"cwd,omitempty"`
	Fingerprint string    `json:"fingerprint"`
	Count       int       `json:"count"`
	ObservedAt  time.Time `json:"observed_at"`
}

type PromptPrefixObservation struct {
	Hash       string    `json:"hash"`
	Tokens     int       `json:"tokens"`
	ObservedAt time.Time `json:"observed_at"`
}

type TurnSnapshot struct {
	SessionID         string                    `json:"session_id"`
	TurnID            string                    `json:"turn_id"`
	RequestID         string                    `json:"request_id,omitempty"`
	ClientFamily      string                    `json:"client_family,omitempty"`
	CWD               string                    `json:"cwd,omitempty"`
	Closed            bool                      `json:"closed"`
	StartedAt         time.Time                 `json:"started_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
	Tools             []ToolObservation         `json:"tools,omitempty"`
	FilesRead         []FileObservation         `json:"files_read,omitempty"`
	FilesEdited       []FileObservation         `json:"files_edited,omitempty"`
	GitPathLists      []GitPathListObservation  `json:"git_path_lists,omitempty"`
	PromptPrefixes    []PromptPrefixObservation `json:"prompt_prefixes,omitempty"`
	LastResponseID    string                    `json:"last_response_id,omitempty"`
	QualityEventCount int                       `json:"quality_event_count,omitempty"`
}

type TurnStoreOptions struct {
	MaxSessions int
	MaxTurns    int
	MaxAge      time.Duration
	Now         func() time.Time
}

type TurnStateStore struct {
	mu          sync.Mutex
	maxSessions int
	maxTurns    int
	maxAge      time.Duration
	now         func() time.Time
	sessions    map[SessionKey]*turnSessionState
}

type turnSessionState struct {
	ID          SessionKey
	CurrentTurn TurnKey
	Sequence    int
	UpdatedAt   time.Time
	Turns       map[TurnKey]*turnState
}

type turnState struct {
	TurnSnapshot
	fileReadIndex    map[string]struct{}
	fileEditIndex    map[string]struct{}
	gitPathListIndex map[string]GitPathListObservation
}

func NewTurnStateStore(opts TurnStoreOptions) *TurnStateStore {
	maxSessions := opts.MaxSessions
	if maxSessions <= 0 {
		maxSessions = 256
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 16
	}
	maxAge := opts.MaxAge
	if maxAge <= 0 {
		maxAge = 6 * time.Hour
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &TurnStateStore{
		maxSessions: maxSessions,
		maxTurns:    maxTurns,
		maxAge:      maxAge,
		now:         now,
		sessions:    make(map[SessionKey]*turnSessionState),
	}
}

func (s *TurnStateStore) StartSession(sessionID, clientFamily, cwd string) TurnSnapshot {
	key := normalizeSessionKey(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	session := s.ensureSessionLocked(key, now)
	session.Turns = make(map[TurnKey]*turnState)
	session.Sequence = 0
	session.CurrentTurn = ""
	session.UpdatedAt = now
	return s.startTurnLocked(session, "turn-1", clientFamily, cwd, "", now).snapshot()
}

func (s *TurnStateStore) StartTurn(sessionID, turnID, clientFamily, cwd, requestID string) TurnSnapshot {
	key := normalizeSessionKey(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	session := s.ensureSessionLocked(key, now)
	if strings.TrimSpace(turnID) == "" {
		session.Sequence++
		turnID = "turn-" + strconv.Itoa(session.Sequence)
	}
	return s.startTurnLocked(session, TurnKey(turnID), clientFamily, cwd, requestID, now).snapshot()
}

func (s *TurnStateStore) ObserveTool(sessionID, turnID string, obs ToolObservation) TurnSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	turn := s.ensureTurnLocked(sessionID, turnID, "", "", "", now)
	turn.Tools = append(turn.Tools, normaliseToolObservation(obs, now))
	turn.UpdatedAt = now
	return turn.snapshot()
}

func (s *TurnStateStore) ObserveFile(sessionID, turnID string, obs FileObservation) TurnSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	turn := s.ensureTurnLocked(sessionID, turnID, "", "", "", now)
	obs = normaliseFileObservation(obs, now)
	switch obs.Operation {
	case "edit":
		if _, ok := turn.fileEditIndex[obs.Path]; !ok {
			turn.FilesEdited = append(turn.FilesEdited, obs)
			turn.fileEditIndex[obs.Path] = struct{}{}
		}
	default:
		obs.Operation = "read"
		if _, ok := turn.fileReadIndex[obs.Path]; !ok {
			turn.FilesRead = append(turn.FilesRead, obs)
			turn.fileReadIndex[obs.Path] = struct{}{}
		}
	}
	turn.UpdatedAt = now
	return turn.snapshot()
}

func (s *TurnStateStore) ObserveGitPathList(sessionID, turnID, cwd, source string, paths []string) (GitPathListObservation, bool) {
	fp := FingerprintPaths(paths)
	if fp == "" {
		return GitPathListObservation{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	turn := s.ensureTurnLocked(sessionID, turnID, "", cwd, "", now)
	key := cwd + "\x00" + fp
	if previous, ok := turn.gitPathListIndex[key]; ok {
		return previous, true
	}
	obs := GitPathListObservation{
		Source:      strings.TrimSpace(source),
		CWD:         filepath.Clean(cwd),
		Fingerprint: fp,
		Count:       len(sortedUniqueStrings(paths)),
		ObservedAt:  now,
	}
	turn.GitPathLists = append(turn.GitPathLists, obs)
	turn.gitPathListIndex[key] = obs
	turn.UpdatedAt = obs.ObservedAt
	return obs, false
}

func (s *TurnStateStore) ObservePromptPrefix(sessionID, turnID, hash string, tokens int) TurnSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	turn := s.ensureTurnLocked(sessionID, turnID, "", "", "", now)
	turn.PromptPrefixes = append(turn.PromptPrefixes, PromptPrefixObservation{
		Hash:       strings.TrimSpace(hash),
		Tokens:     tokens,
		ObservedAt: now,
	})
	turn.UpdatedAt = now
	return turn.snapshot()
}

func (s *TurnStateStore) SetLastResponseID(sessionID, turnID, responseID string) TurnSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	turn := s.ensureTurnLocked(sessionID, turnID, "", "", "", now)
	turn.LastResponseID = strings.TrimSpace(responseID)
	turn.UpdatedAt = now
	return turn.snapshot()
}

func (s *TurnStateStore) MarkQualityEvent(sessionID, turnID string) TurnSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	turn := s.ensureTurnLocked(sessionID, turnID, "", "", "", now)
	turn.QualityEventCount++
	turn.UpdatedAt = now
	return turn.snapshot()
}

func (s *TurnStateStore) CloseTurn(sessionID, turnID string) TurnSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	turn := s.ensureTurnLocked(sessionID, turnID, "", "", "", now)
	turn.Closed = true
	turn.UpdatedAt = now
	return turn.snapshot()
}

func (s *TurnStateStore) RecentlyEdited(sessionID, path string, previousTurns int) bool {
	key := normalizeSessionKey(sessionID)
	target := normalizePath(path)
	if target == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[key]
	if session == nil {
		return false
	}
	turns := s.sortedTurnsLocked(session)
	if previousTurns < 0 {
		previousTurns = 0
	}
	start := max(len(turns)-1-previousTurns, 0)
	for _, turn := range turns[start:] {
		if _, ok := turn.fileEditIndex[target]; ok {
			return true
		}
	}
	return false
}

func (s *TurnStateStore) Snapshot(sessionID, turnID string) (TurnSnapshot, bool) {
	key := normalizeSessionKey(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[key]
	if session == nil {
		return TurnSnapshot{}, false
	}
	tkey := TurnKey(strings.TrimSpace(turnID))
	if tkey == "" {
		tkey = session.CurrentTurn
	}
	turn := session.Turns[tkey]
	if turn == nil {
		return TurnSnapshot{}, false
	}
	return turn.snapshot(), true
}

func (s *TurnStateStore) Stats() (sessions int, turns int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.sessions {
		sessions++
		turns += len(session.Turns)
	}
	return sessions, turns
}

func (s *TurnStateStore) ensureTurnLocked(sessionID, turnID, clientFamily, cwd, requestID string, now time.Time) *turnState {
	key := normalizeSessionKey(sessionID)
	session := s.ensureSessionLocked(key, now)
	tkey := TurnKey(strings.TrimSpace(turnID))
	if tkey == "" {
		tkey = session.CurrentTurn
	}
	if tkey == "" || session.Turns[tkey] == nil {
		session.Sequence++
		tkey = TurnKey("turn-" + strconv.Itoa(session.Sequence))
		return s.startTurnLocked(session, tkey, clientFamily, cwd, requestID, now)
	}
	turn := session.Turns[tkey]
	session.CurrentTurn = tkey
	session.UpdatedAt = now
	return turn
}

func (s *TurnStateStore) ensureSessionLocked(key SessionKey, now time.Time) *turnSessionState {
	s.evictExpiredLocked(now)
	session := s.sessions[key]
	if session == nil {
		session = &turnSessionState{ID: key, Turns: make(map[TurnKey]*turnState)}
		s.sessions[key] = session
	}
	session.UpdatedAt = now
	if len(s.sessions) > s.maxSessions {
		s.evictOldestSessionLocked()
	}
	return session
}

func (s *TurnStateStore) startTurnLocked(session *turnSessionState, turnID TurnKey, clientFamily, cwd, requestID string, now time.Time) *turnState {
	if strings.TrimSpace(string(turnID)) == "" {
		session.Sequence++
		turnID = TurnKey("turn-" + strconv.Itoa(session.Sequence))
	}
	turn := newTurnState(string(session.ID), string(turnID), clientFamily, cwd, requestID, now)
	session.Turns[turnID] = turn
	session.CurrentTurn = turnID
	session.UpdatedAt = now
	s.evictOldestTurnsLocked(session)
	return turn
}

func newTurnState(sessionID, turnID, clientFamily, cwd, requestID string, now time.Time) *turnState {
	return &turnState{
		TurnSnapshot: TurnSnapshot{
			SessionID:    string(normalizeSessionKey(sessionID)),
			TurnID:       strings.TrimSpace(turnID),
			RequestID:    strings.TrimSpace(requestID),
			ClientFamily: strings.TrimSpace(clientFamily),
			CWD:          filepath.Clean(cwd),
			StartedAt:    now,
			UpdatedAt:    now,
		},
		fileReadIndex:    make(map[string]struct{}),
		fileEditIndex:    make(map[string]struct{}),
		gitPathListIndex: make(map[string]GitPathListObservation),
	}
}

func (s *TurnStateStore) sortedTurnsLocked(session *turnSessionState) []*turnState {
	turns := make([]*turnState, 0, len(session.Turns))
	for _, turn := range session.Turns {
		turns = append(turns, turn)
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i].StartedAt.Before(turns[j].StartedAt) })
	return turns
}

func (s *TurnStateStore) evictOldestTurnsLocked(session *turnSessionState) {
	for len(session.Turns) > s.maxTurns {
		oldestKey := TurnKey("")
		oldestAt := time.Time{}
		for key, turn := range session.Turns {
			if oldestKey == "" || turn.UpdatedAt.Before(oldestAt) {
				oldestKey = key
				oldestAt = turn.UpdatedAt
			}
		}
		delete(session.Turns, oldestKey)
	}
}

func (s *TurnStateStore) evictOldestSessionLocked() {
	oldestKey := SessionKey("")
	oldestAt := time.Time{}
	for key, session := range s.sessions {
		if oldestKey == "" || session.UpdatedAt.Before(oldestAt) {
			oldestKey = key
			oldestAt = session.UpdatedAt
		}
	}
	delete(s.sessions, oldestKey)
}

func (s *TurnStateStore) evictExpiredLocked(now time.Time) {
	for key, session := range s.sessions {
		if now.Sub(session.UpdatedAt) > s.maxAge {
			delete(s.sessions, key)
		}
	}
}

func (t *turnState) snapshot() TurnSnapshot {
	out := t.TurnSnapshot
	out.Tools = append([]ToolObservation(nil), t.Tools...)
	out.FilesRead = append([]FileObservation(nil), t.FilesRead...)
	out.FilesEdited = append([]FileObservation(nil), t.FilesEdited...)
	out.GitPathLists = append([]GitPathListObservation(nil), t.GitPathLists...)
	out.PromptPrefixes = append([]PromptPrefixObservation(nil), t.PromptPrefixes...)
	return out
}

func normalizeSessionKey(sessionID string) SessionKey {
	return SessionKey(SafeSessionID(sessionID))
}

func normaliseToolObservation(obs ToolObservation, now time.Time) ToolObservation {
	obs.Name = strings.TrimSpace(obs.Name)
	obs.Command = strings.TrimSpace(obs.Command)
	obs.Decision = strings.TrimSpace(obs.Decision)
	obs.ArchiveID = strings.TrimSpace(obs.ArchiveID)
	if obs.ObservedAt.IsZero() {
		obs.ObservedAt = now
	}
	return obs
}

func normaliseFileObservation(obs FileObservation, now time.Time) FileObservation {
	obs.Path = normalizePath(obs.Path)
	obs.Operation = strings.TrimSpace(obs.Operation)
	obs.Hash = strings.TrimSpace(obs.Hash)
	if obs.ObservedAt.IsZero() {
		obs.ObservedAt = now
	}
	return obs
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func FingerprintPaths(paths []string) string {
	normalised := sortedUniqueStrings(paths)
	if len(normalised) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(normalised, "\x00")))
	return hex.EncodeToString(sum[:])
}

func sortedUniqueStrings(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(path)), "./")
		if path != "" {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	uniq := out[:0]
	for _, path := range out {
		if len(uniq) > 0 && uniq[len(uniq)-1] == path {
			continue
		}
		uniq = append(uniq, path)
	}
	return uniq
}

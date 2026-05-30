package readcache

import (
	"path/filepath"
	"sync"
	"time"
)

const readCacheAsyncFlushDelay = 50 * time.Millisecond

type memorySessionEntry struct {
	state          *SessionState
	dirty          bool
	flushScheduled bool
	lastUsed       time.Time
}

var readCacheMemory = struct {
	mu       sync.Mutex
	sessions map[string]*memorySessionEntry
}{
	sessions: map[string]*memorySessionEntry{},
}

func loadSessionCached(dir string, sessionID string) (*SessionState, error) {
	safeID := sanitizeSessionID(sessionID)
	key := memorySessionKey(dir, safeID)

	readCacheMemory.mu.Lock()
	if entry := readCacheMemory.sessions[key]; entry != nil {
		entry.lastUsed = time.Now()
		state := cloneSessionState(entry.state)
		readCacheMemory.mu.Unlock()
		return state, nil
	}
	readCacheMemory.mu.Unlock()

	state, err := loadSessionFromDisk(dir, safeID)
	if err != nil {
		return nil, err
	}

	readCacheMemory.mu.Lock()
	readCacheMemory.sessions[key] = &memorySessionEntry{
		state:    cloneSessionState(state),
		lastUsed: time.Now(),
	}
	state = cloneSessionState(readCacheMemory.sessions[key].state)
	readCacheMemory.mu.Unlock()
	return state, nil
}

// SaveSessionAsync updates the in-memory readcache session state and schedules a
// bounded write-behind flush. It validates the session can be marshalled before
// returning, but it deliberately keeps disk writes off the hot path.
func SaveSessionAsync(dir string, state *SessionState) error {
	normalized := cloneSessionState(state)
	normalizeSessionState(normalized)
	if _, err := readCacheMarshalIndent(normalized, "", "  "); err != nil {
		return err
	}

	key := memorySessionKey(dir, normalized.SessionID)
	readCacheMemory.mu.Lock()
	entry := readCacheMemory.sessions[key]
	if entry == nil {
		entry = &memorySessionEntry{}
		readCacheMemory.sessions[key] = entry
	}
	entry.state = normalized
	entry.dirty = true
	entry.lastUsed = time.Now()
	if !entry.flushScheduled {
		entry.flushScheduled = true
		go delayedFlushSession(dir, normalized.SessionID)
	}
	readCacheMemory.mu.Unlock()
	return nil
}

func delayedFlushSession(dir string, sessionID string) {
	time.Sleep(readCacheAsyncFlushDelay)
	_ = FlushSession(dir, sessionID)
}

func FlushSession(dir string, sessionID string) error {
	safeID := sanitizeSessionID(sessionID)
	key := memorySessionKey(dir, safeID)

	readCacheMemory.mu.Lock()
	entry := readCacheMemory.sessions[key]
	if entry == nil || !entry.dirty {
		if entry != nil {
			entry.flushScheduled = false
		}
		readCacheMemory.mu.Unlock()
		return nil
	}
	state := cloneSessionState(entry.state)
	entry.dirty = false
	entry.flushScheduled = false
	readCacheMemory.mu.Unlock()

	if err := saveSessionToDisk(dir, state); err != nil {
		readCacheMemory.mu.Lock()
		if retry := readCacheMemory.sessions[key]; retry != nil {
			retry.dirty = true
		}
		readCacheMemory.mu.Unlock()
		return err
	}
	rememberSessionClean(dir, state)
	return nil
}

func FlushDir(dir string) error {
	readCacheMemory.mu.Lock()
	sessions := make([]string, 0)
	for key := range readCacheMemory.sessions {
		entryDir, sessionID := splitMemorySessionKey(key)
		if filepath.Clean(entryDir) == filepath.Clean(dir) {
			sessions = append(sessions, sessionID)
		}
	}
	readCacheMemory.mu.Unlock()

	for _, sessionID := range sessions {
		if err := FlushSession(dir, sessionID); err != nil {
			return err
		}
	}
	return nil
}

func FlushAll() error {
	readCacheMemory.mu.Lock()
	type flushTarget struct {
		dir       string
		sessionID string
	}
	targets := make([]flushTarget, 0, len(readCacheMemory.sessions))
	for key := range readCacheMemory.sessions {
		dir, sessionID := splitMemorySessionKey(key)
		targets = append(targets, flushTarget{dir: dir, sessionID: sessionID})
	}
	readCacheMemory.mu.Unlock()

	for _, target := range targets {
		if err := FlushSession(target.dir, target.sessionID); err != nil {
			return err
		}
	}
	return nil
}

func rememberSessionClean(dir string, state *SessionState) {
	if state == nil {
		return
	}
	normalized := cloneSessionState(state)
	normalizeSessionState(normalized)
	key := memorySessionKey(dir, normalized.SessionID)
	readCacheMemory.mu.Lock()
	readCacheMemory.sessions[key] = &memorySessionEntry{
		state:    normalized,
		lastUsed: time.Now(),
	}
	readCacheMemory.mu.Unlock()
}

func clearMemoryDir(dir string) {
	cleanDir := filepath.Clean(dir)
	readCacheMemory.mu.Lock()
	for key := range readCacheMemory.sessions {
		entryDir, _ := splitMemorySessionKey(key)
		if filepath.Clean(entryDir) == cleanDir {
			delete(readCacheMemory.sessions, key)
		}
	}
	readCacheMemory.mu.Unlock()
}

func memorySessionKey(dir string, sessionID string) string {
	return filepath.Clean(dir) + "\x00" + sanitizeSessionID(sessionID)
}

func splitMemorySessionKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

func cloneSessionState(in *SessionState) *SessionState {
	if in == nil {
		return &SessionState{Files: map[string]*FileEntry{}, Outputs: map[string]*OutputEntry{}}
	}
	out := &SessionState{
		SessionID:     in.SessionID,
		CurrentTurnID: in.CurrentTurnID,
		TurnSeq:       in.TurnSeq,
		Files:         make(map[string]*FileEntry, len(in.Files)),
		Outputs:       make(map[string]*OutputEntry, len(in.Outputs)),
	}
	for key, entry := range in.Files {
		if entry == nil {
			continue
		}
		copied := *entry
		out.Files[key] = &copied
	}
	for key, entry := range in.Outputs {
		if entry == nil {
			continue
		}
		copied := *entry
		out.Outputs[key] = &copied
	}
	normalizeSessionState(out)
	return out
}

func normalizeSessionState(state *SessionState) {
	if state == nil {
		return
	}
	state.SessionID = sanitizeSessionID(state.SessionID)
	state.CurrentTurnID = safeTurn(state.CurrentTurnID)
	if state.Files == nil {
		state.Files = map[string]*FileEntry{}
	}
	if state.Outputs == nil {
		state.Outputs = map[string]*OutputEntry{}
	}
}

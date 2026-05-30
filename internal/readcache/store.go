package readcache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/sessions"
)

var (
	readCacheMkdirAll      = os.MkdirAll
	readCacheAbsPath       = filepath.Abs
	readCacheReadFile      = os.ReadFile
	readCacheReadDir       = os.ReadDir
	readCacheRemoveAll     = os.RemoveAll
	readCacheRemove        = os.Remove
	readCacheWriteFile     = os.WriteFile
	readCacheMarshalIndent = json.MarshalIndent
	readCacheUnmarshal     = json.Unmarshal
	readCacheSaveSession   = SaveSession
)

const (
	defaultMaxSessions   = 500
	defaultSessionMaxAge = 14 * 24 * time.Hour
	pruneEverySaves      = 64
)

// sessionSaveCount gates the opportunistic prune so the session directory is
// scanned at most once every pruneEverySaves writes rather than on every
// hot-path SaveSession.
var sessionSaveCount atomic.Uint64

func DefaultDir(home string) string {
	return filepath.Join(home, ".slimference", "read-cache")
}

func LoadSession(dir string, sessionID string) (*SessionState, error) {
	safeID := sanitizeSessionID(sessionID)
	path := sessionPath(dir, safeID)

	data, err := readCacheReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SessionState{SessionID: safeID, Files: map[string]*FileEntry{}}, nil
		}
		return nil, err
	}

	var state SessionState
	if err := readCacheUnmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("readcache: parse session %s: %w", safeID, err)
	}
	if state.Files == nil {
		state.Files = map[string]*FileEntry{}
	}
	if state.Outputs == nil {
		state.Outputs = map[string]*OutputEntry{}
	}
	if state.SessionID == "" {
		state.SessionID = safeID
	}
	state.CurrentTurnID = sessions.SafeOptionalTurnID(state.CurrentTurnID)
	return &state, nil
}

func SaveSession(dir string, state *SessionState) error {
	if err := readCacheMkdirAll(dir, 0o755); err != nil {
		return err
	}
	state.SessionID = sanitizeSessionID(state.SessionID)
	state.CurrentTurnID = sessions.SafeOptionalTurnID(state.CurrentTurnID)
	if state.Files == nil {
		state.Files = map[string]*FileEntry{}
	}
	if state.Outputs == nil {
		state.Outputs = map[string]*OutputEntry{}
	}

	data, err := readCacheMarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := readCacheWriteFile(sessionPath(dir, state.SessionID), append(data, '\n'), 0o644); err != nil {
		return err
	}
	// Opportunistically bound the session directory so it cannot grow without
	// limit across many conversations. Cheap amortised: scans once every
	// pruneEverySaves writes. Best-effort; a prune error never fails a save.
	if sessionSaveCount.Add(1)%pruneEverySaves == 0 {
		_, _ = PruneSessions(dir, 0, 0)
	}
	return nil
}

// PruneSessions removes session state files older than maxAge, then removes the
// oldest remaining (by mtime) until at most maxEntries are left. Zero or negative
// bounds fall back to the package defaults. Returns the number of files pruned.
// Best-effort: a removal error stops and is returned; a missing directory is not
// an error. Active sessions are safe because their files carry a fresh mtime and
// are never the oldest.
func PruneSessions(dir string, maxEntries int, maxAge time.Duration) (int, error) {
	if maxEntries <= 0 {
		maxEntries = defaultMaxSessions
	}
	if maxAge <= 0 {
		maxAge = defaultSessionMaxAge
	}
	entries, err := readCacheReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	type sessFile struct {
		name    string
		modTime time.Time
	}
	files := make([]sessFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, sessFile{name: e.Name(), modTime: info.ModTime()})
	}
	// Newest first.
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })

	pruned := 0
	cutoff := time.Now().Add(-maxAge)
	kept := make([]sessFile, 0, len(files))
	for _, f := range files {
		if f.modTime.Before(cutoff) {
			if err := readCacheRemove(filepath.Join(dir, f.name)); err != nil && !os.IsNotExist(err) {
				return pruned, err
			}
			pruned++
			continue
		}
		kept = append(kept, f)
	}
	// kept is newest-first; remove everything beyond the entry cap (the oldest).
	for i := maxEntries; i < len(kept); i++ {
		if err := readCacheRemove(filepath.Join(dir, kept[i].name)); err != nil && !os.IsNotExist(err) {
			return pruned, err
		}
		pruned++
	}
	return pruned, nil
}

func sessionPath(dir string, sessionID string) string {
	return filepath.Join(dir, sanitizeSessionID(sessionID)+".json")
}

func sanitizeSessionID(sessionID string) string {
	return sessions.SafeSessionID(sessionID)
}

package readcache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/slimference/slimference/internal/sessions"
)

var (
	readCacheMkdirAll      = os.MkdirAll
	readCacheAbsPath       = filepath.Abs
	readCacheReadFile      = os.ReadFile
	readCacheReadDir       = os.ReadDir
	readCacheRemoveAll     = os.RemoveAll
	readCacheWriteFile     = os.WriteFile
	readCacheMarshalIndent = json.MarshalIndent
	readCacheUnmarshal     = json.Unmarshal
	readCacheSaveSession   = SaveSession
)

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
	return readCacheWriteFile(sessionPath(dir, state.SessionID), append(data, '\n'), 0o644)
}

func sessionPath(dir string, sessionID string) string {
	return filepath.Join(dir, sanitizeSessionID(sessionID)+".json")
}

func sanitizeSessionID(sessionID string) string {
	return sessions.SafeSessionID(sessionID)
}

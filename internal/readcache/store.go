package readcache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if state.SessionID == "" {
		state.SessionID = safeID
	}
	return &state, nil
}

func SaveSession(dir string, state *SessionState) error {
	if err := readCacheMkdirAll(dir, 0o755); err != nil {
		return err
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
	if strings.TrimSpace(sessionID) == "" {
		return "unknown-session"
	}
	var b strings.Builder
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

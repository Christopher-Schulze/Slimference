// Package toolusecache persists per-session Codex tool-use command metadata
// (call_id -> tool name + command arguments) so cross-turn read-delta resolution
// survives a WSS socket reconnect. The in-memory per-socket map would otherwise
// reset on reconnect and a later re-read would go unresolved (no savings).
//
// Content-free by construction: it stores only the tool name and command
// arguments (the metadata needed to resolve a function_call_output back to its
// command), never tool OUTPUT or auth. T251 (item 15).
package toolusecache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/slimference/slimference/internal/sessions"
)

const (
	// MaxEntriesPerSession caps a single session's resolution map so a long
	// conversation cannot grow the file without bound.
	MaxEntriesPerSession = 4096
	defaultMaxSessions   = 500
	defaultMaxAge        = 14 * 24 * time.Hour
)

// Indirection points for tests.
var (
	readFile  = os.ReadFile
	writeFile = os.WriteFile
	mkdirAll  = os.MkdirAll
	readDir   = os.ReadDir
	removeOne = os.Remove
)

// Entry is the resolution metadata for one tool call. No tool output.
type Entry struct {
	ToolUseID string `json:"tool_use_id"`
	ToolName  string `json:"tool_name"`
	ToolInput string `json:"tool_input"`
	Type      string `json:"type"`
}

// DefaultDir returns the canonical on-disk cache root.
func DefaultDir(home string) string {
	return filepath.Join(home, ".slimference", "tooluse-cache")
}

// CollapsedKeysDir returns the on-disk root for persisted collapsed read keys.
// Scan/read recovery uses it so the re-read full-pass canary survives a WSS
// socket reconnect (the in-memory collapsed-key set is per-socket otherwise).
func CollapsedKeysDir(home string) string {
	return filepath.Join(home, ".slimference", "collapsed-keys")
}

// ScanReadKeysDir returns the on-disk root for the scan-origin subset of
// collapsed keys, used telemetry-only to measure what fraction of scan-elided
// reads the model later re-reads (the body-was-needed rate). Survives reconnect.
func ScanReadKeysDir(home string) string {
	return filepath.Join(home, ".slimference", "scan-read-keys")
}

// ScanRereadKeysDir returns the on-disk root for the subset of scan-read keys
// the model actually re-read (the B set). Persisted per session so the auto
// self-regulation re-read rate (|B|/|A|) survives a WSS reconnect.
func ScanRereadKeysDir(home string) string {
	return filepath.Join(home, ".slimference", "scan-reread-keys")
}

func sessionPath(dir, sessionID string) string {
	return filepath.Join(dir, sessions.SafeSessionID(sessionID)+".json")
}

// Load returns the persisted entries for a session keyed by call_id. A missing
// file yields an empty map and no error.
func Load(dir, sessionID string) (map[string]Entry, error) {
	data, err := readFile(sessionPath(dir, sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Entry{}, nil
		}
		return nil, err
	}
	var entries map[string]Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		entries = map[string]Entry{}
	}
	return entries, nil
}

// Merge overlays add onto the persisted session entries, caps the total at
// MaxEntriesPerSession (dropping the lowest call_ids deterministically), writes
// the result, and returns the merged map. Empty add is a pure Load (no write).
func Merge(dir, sessionID string, add map[string]Entry) (map[string]Entry, error) {
	if len(add) == 0 {
		return Load(dir, sessionID)
	}
	existing, err := Load(dir, sessionID)
	if err != nil {
		return nil, err
	}
	for id, e := range add {
		if id == "" {
			continue
		}
		existing[id] = e
	}
	if len(existing) > MaxEntriesPerSession {
		ids := make([]string, 0, len(existing))
		for id := range existing {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids[:len(existing)-MaxEntriesPerSession] {
			delete(existing, id)
		}
	}
	if err := save(dir, sessionID, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func save(dir, sessionID string, entries map[string]Entry) error {
	if err := mkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(sessionPath(dir, sessionID), append(data, '\n'), 0o644)
}

// Prune removes session files older than maxAge, then the oldest by mtime beyond
// maxEntries. Zero/negative bounds fall back to defaults. Missing dir is not an
// error. Best-effort: a removal error stops and is returned.
func Prune(dir string, maxEntries int, maxAge time.Duration) (int, error) {
	if maxEntries <= 0 {
		maxEntries = defaultMaxSessions
	}
	if maxAge <= 0 {
		maxAge = defaultMaxAge
	}
	entries, err := readDir(dir)
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
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })

	pruned := 0
	cutoff := time.Now().Add(-maxAge)
	kept := make([]sessFile, 0, len(files))
	for _, f := range files {
		if f.modTime.Before(cutoff) {
			if err := removeOne(filepath.Join(dir, f.name)); err != nil && !os.IsNotExist(err) {
				return pruned, err
			}
			pruned++
			continue
		}
		kept = append(kept, f)
	}
	for i := maxEntries; i < len(kept); i++ {
		if err := removeOne(filepath.Join(dir, kept[i].name)); err != nil && !os.IsNotExist(err) {
			return pruned, err
		}
		pruned++
	}
	return pruned, nil
}

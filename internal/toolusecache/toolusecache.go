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
	"sync"
	"time"

	"github.com/slimference/slimference/internal/sessions"
)

const (
	// MaxEntriesPerSession caps a single session's resolution map so a long
	// conversation cannot grow the file without bound.
	MaxEntriesPerSession = 4096
	defaultMaxSessions   = 500
	defaultMaxAge        = 14 * 24 * time.Hour
	asyncFlushDelay      = 50 * time.Millisecond
)

// Indirection points for tests.
var (
	readFile  = os.ReadFile
	writeFile = os.WriteFile
	mkdirAll  = os.MkdirAll
	readDir   = os.ReadDir
	removeOne = os.Remove
	removeAll = os.RemoveAll
)

type memoryEntry struct {
	entries        map[string]Entry
	dirty          bool
	flushScheduled bool
	lastUsed       time.Time
}

var memory = struct {
	mu       sync.Mutex
	sessions map[string]*memoryEntry
}{
	sessions: map[string]*memoryEntry{},
}

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

func sessionPath(dir, sessionID string) string {
	return filepath.Join(dir, sessions.SafeSessionID(sessionID)+".json")
}

// Load returns the persisted entries for a session keyed by call_id. A missing
// file yields an empty map and no error.
func Load(dir, sessionID string) (map[string]Entry, error) {
	safeID := sessions.SafeSessionID(sessionID)
	key := memoryKey(dir, safeID)

	memory.mu.Lock()
	if entry := memory.sessions[key]; entry != nil {
		entry.lastUsed = time.Now()
		entries := cloneEntries(entry.entries)
		memory.mu.Unlock()
		return entries, nil
	}
	memory.mu.Unlock()

	data, err := readFile(sessionPath(dir, safeID))
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
	rememberClean(dir, safeID, entries)
	return cloneEntries(entries), nil
}

// Merge overlays add onto the persisted session entries, caps the total at
// MaxEntriesPerSession (dropping the lowest call_ids deterministically), writes
// the result, and returns the merged map. Empty add is a pure Load (no write).
func Merge(dir, sessionID string, add map[string]Entry) (map[string]Entry, error) {
	if len(add) == 0 {
		return Load(dir, sessionID)
	}
	merged, err := mergeEntries(dir, sessionID, add)
	if err != nil {
		return nil, err
	}
	if err := save(dir, sessionID, merged); err != nil {
		return nil, err
	}
	rememberClean(dir, sessionID, merged)
	return cloneEntries(merged), nil
}

// MergeAsync overlays add onto the in-memory session entries and schedules a
// bounded write-behind flush. Reconnects in the same process can Load the merged
// metadata immediately, while disk I/O stays off the WSS frame hot path.
func MergeAsync(dir, sessionID string, add map[string]Entry) (map[string]Entry, error) {
	if len(add) == 0 {
		return Load(dir, sessionID)
	}
	merged, err := mergeEntries(dir, sessionID, add)
	if err != nil {
		return nil, err
	}
	if _, err := json.MarshalIndent(merged, "", "  "); err != nil {
		return nil, err
	}

	safeID := sessions.SafeSessionID(sessionID)
	key := memoryKey(dir, safeID)
	memory.mu.Lock()
	entry := memory.sessions[key]
	if entry == nil {
		entry = &memoryEntry{}
		memory.sessions[key] = entry
	}
	entry.entries = cloneEntries(merged)
	entry.dirty = true
	entry.lastUsed = time.Now()
	if !entry.flushScheduled {
		entry.flushScheduled = true
		go delayedFlush(dir, safeID)
	}
	out := cloneEntries(entry.entries)
	memory.mu.Unlock()
	return out, nil
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

func mergeEntries(dir, sessionID string, add map[string]Entry) (map[string]Entry, error) {
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
	if len(existing) <= MaxEntriesPerSession {
		return existing, nil
	}
	ids := make([]string, 0, len(existing))
	for id := range existing {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids[:len(existing)-MaxEntriesPerSession] {
		delete(existing, id)
	}
	return existing, nil
}

func delayedFlush(dir, sessionID string) {
	time.Sleep(asyncFlushDelay)
	_ = FlushSession(dir, sessionID)
}

func FlushSession(dir, sessionID string) error {
	safeID := sessions.SafeSessionID(sessionID)
	key := memoryKey(dir, safeID)

	memory.mu.Lock()
	entry := memory.sessions[key]
	if entry == nil || !entry.dirty {
		if entry != nil {
			entry.flushScheduled = false
		}
		memory.mu.Unlock()
		return nil
	}
	entries := cloneEntries(entry.entries)
	entry.dirty = false
	entry.flushScheduled = false
	memory.mu.Unlock()

	if err := save(dir, safeID, entries); err != nil {
		memory.mu.Lock()
		if retry := memory.sessions[key]; retry != nil {
			retry.dirty = true
		}
		memory.mu.Unlock()
		return err
	}
	rememberClean(dir, safeID, entries)
	return nil
}

func FlushAll() error {
	memory.mu.Lock()
	type target struct {
		dir       string
		sessionID string
	}
	targets := make([]target, 0, len(memory.sessions))
	for key := range memory.sessions {
		dir, sessionID := splitMemoryKey(key)
		targets = append(targets, target{dir: dir, sessionID: sessionID})
	}
	memory.mu.Unlock()

	for _, t := range targets {
		if err := FlushSession(t.dir, t.sessionID); err != nil {
			return err
		}
	}
	return nil
}

// Clear removes persisted state for a cache root and drops matching in-memory
// sessions. It is used by product cache flushes; missing directories are fine.
func Clear(dir string) error {
	clearMemoryDir(dir)
	if err := removeAll(dir); err != nil {
		return err
	}
	return nil
}

func rememberClean(dir, sessionID string, entries map[string]Entry) {
	key := memoryKey(dir, sessions.SafeSessionID(sessionID))
	memory.mu.Lock()
	memory.sessions[key] = &memoryEntry{
		entries:  cloneEntries(entries),
		lastUsed: time.Now(),
	}
	memory.mu.Unlock()
}

func cloneEntries(in map[string]Entry) map[string]Entry {
	out := make(map[string]Entry, len(in))
	for id, entry := range in {
		out[id] = entry
	}
	return out
}

func memoryKey(dir, sessionID string) string {
	return filepath.Clean(dir) + "\x00" + sessions.SafeSessionID(sessionID)
}

func splitMemoryKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

func clearMemoryDir(dir string) {
	cleanDir := filepath.Clean(dir)
	memory.mu.Lock()
	for key := range memory.sessions {
		entryDir, _ := splitMemoryKey(key)
		if filepath.Clean(entryDir) == cleanDir {
			delete(memory.sessions, key)
		}
	}
	memory.mu.Unlock()
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

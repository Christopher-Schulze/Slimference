package readcache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/sessions"
)

const (
	maxCachedFileBytes          = 64 * 1024
	maxObservedInlineCacheBytes = 256 * 1024
	minObservedOutputBytes      = 512
)

func Evaluate(dir string, req Request) (Decision, error) {
	state, err := LoadSession(dir, req.SessionID)
	if err != nil {
		return Decision{}, err
	}
	if turnID := safeTurn(req.TurnID); turnID != "" {
		state.CurrentTurnID = turnID
	}

	absPath, err := readCacheAbsPath(req.FilePath)
	if err != nil {
		return Decision{Type: DecisionAllow}, nil
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		decision := Decision{Type: DecisionAllow}
		return decision, RecordDecision(dir, decision)
	}

	req.FilePath = absPath
	entryKey := requestEntryKey(req)
	entry := state.Files[entryKey]
	if entry == nil {
		entry = &FileEntry{Path: absPath}
		state.Files[entryKey] = entry
	}

	modTime := info.ModTime().UnixNano()
	if sameRange(entry, req) && entry.ModTimeUnixNs == modTime {
		return blockUnchanged(dir, state, entry, req, modTime)
	}

	return evaluateChanged(dir, state, entry, req, modTime, info.Size())
}

func EvaluateObserved(dir string, req Request, content string, archiveDir string, recentlyEdited bool) (Decision, error) {
	state, err := LoadSession(dir, req.SessionID)
	if err != nil {
		return Decision{}, err
	}
	if turnID := safeTurn(req.TurnID); turnID != "" {
		state.CurrentTurnID = turnID
	}
	path := strings.TrimSpace(req.FilePath)
	if path == "" || strings.TrimSpace(req.SessionID) == "" {
		decision := Decision{Type: DecisionAllow}
		return decision, RecordDecision(dir, decision)
	}
	entryKey := requestEntryKey(Request{FilePath: path, Offset: req.Offset, Limit: req.Limit})
	entry := state.Files[entryKey]
	if entry == nil {
		entry = &FileEntry{Path: path}
		state.Files[entryKey] = entry
	}

	hash := hashObservedContent(content)
	oldContent := entry.CachedContent
	if oldContent == "" && entry.ArchiveURI != "" && archiveDir != "" {
		if _, body, err := contentarchive.Get(archiveDir, entry.ArchiveURI); err == nil {
			oldContent = string(body)
		}
	}
	if recentlyEdited {
		archiveURI, _ := archiveObservedContent(archiveDir, req, content)
		updateObservedEntry(entry, req, hash, archiveURI, content)
		if err := readCacheSaveSession(dir, state); err != nil {
			return Decision{}, err
		}
		decision := Decision{Type: DecisionAllow}
		return decision, RecordDecision(dir, decision)
	}
	if entry.ContentHash != "" && entry.ContentHash == hash && entry.ArchiveURI != "" {
		updateObservedEntry(entry, req, hash, entry.ArchiveURI, content)
		if err := readCacheSaveSession(dir, state); err != nil {
			return Decision{}, err
		}
		decision := Decision{
			Type:      DecisionBlock,
			Reason:    unchangedReference(req.FilePath, entry.ArchiveURI),
			BlockKind: BlockKindUnchanged,
		}
		return decision, RecordDecision(dir, decision)
	}

	oldHash := entry.ContentHash
	archiveURI, archived := archiveObservedContent(archiveDir, req, content)
	updateObservedEntry(entry, req, hash, archiveURI, content)
	if err := readCacheSaveSession(dir, state); err != nil {
		return Decision{}, err
	}
	if archived && oldHash != "" && oldContent != "" {
		delta := buildDeltaSummary(req.FilePath, oldContent, content)
		if delta != "" {
			delta = delta + "\nFull content: " + archiveURI
			if len(delta) < len(content) {
				decision := Decision{Type: DecisionBlock, Reason: delta, BlockKind: BlockKindDelta}
				return decision, RecordDecision(dir, decision)
			}
		}
	}
	decision := Decision{Type: DecisionAllow}
	return decision, RecordDecision(dir, decision)
}

func EvaluateObservedOutput(dir string, req OutputRequest, content string, archiveDir string) (Decision, error) {
	state, err := LoadSession(dir, req.SessionID)
	if err != nil {
		return Decision{}, err
	}
	if turnID := safeTurn(req.TurnID); turnID != "" {
		state.CurrentTurnID = turnID
	}
	key := strings.TrimSpace(req.Key)
	if key == "" || strings.TrimSpace(req.SessionID) == "" || len(content) < minObservedOutputBytes {
		decision := Decision{Type: DecisionAllow}
		return decision, RecordDecision(dir, decision)
	}
	if state.Outputs == nil {
		state.Outputs = map[string]*OutputEntry{}
	}
	entry := state.Outputs[key]
	if entry == nil {
		entry = &OutputEntry{Key: key}
		state.Outputs[key] = entry
	}

	hash := hashObservedContent(content)
	oldContent := entry.CachedContent
	if oldContent == "" && entry.ArchiveURI != "" && archiveDir != "" {
		if _, body, err := contentarchive.Get(archiveDir, entry.ArchiveURI); err == nil {
			oldContent = string(body)
		}
	}
	if entry.ContentHash != "" && entry.ContentHash == hash && entry.ArchiveURI != "" {
		updateObservedOutputEntry(entry, req, hash, entry.ArchiveURI, content)
		if err := readCacheSaveSession(dir, state); err != nil {
			return Decision{}, err
		}
		decision := Decision{
			Type:      DecisionBlock,
			Reason:    unchangedOutputReference(req.CommandLine, entry.ArchiveURI),
			BlockKind: BlockKindUnchanged,
		}
		return decision, RecordDecision(dir, decision)
	}

	oldHash := entry.ContentHash
	archiveURI, archived := archiveObservedOutputContent(archiveDir, req, content)
	updateObservedOutputEntry(entry, req, hash, archiveURI, content)
	if err := readCacheSaveSession(dir, state); err != nil {
		return Decision{}, err
	}
	if !archived {
		decision := Decision{Type: DecisionAllow}
		return decision, RecordDecision(dir, decision)
	}
	if oldHash != "" && oldContent != "" && outputDeltaEligible(req.Key) {
		delta := buildDeltaSummary("tool output for "+strings.TrimSpace(req.CommandLine), oldContent, content)
		if delta != "" {
			delta = delta + "\nFull output: " + archiveURI
			if len(delta) < len(content) {
				decision := Decision{Type: DecisionBlock, Reason: delta, BlockKind: BlockKindDelta}
				return decision, RecordDecision(dir, decision)
			}
		}
	}
	decision := Decision{Type: DecisionAllow}
	return decision, RecordDecision(dir, decision)
}

func blockUnchanged(dir string, state *SessionState, entry *FileEntry, req Request, modTime int64) (Decision, error) {
	updateEntry(entry, req, modTime, entry.CachedContent)
	if err := readCacheSaveSession(dir, state); err != nil {
		return Decision{}, err
	}
	decision := Decision{
		Type:      DecisionBlock,
		Reason:    fmt.Sprintf("Read note: %s is already in context and unchanged.", req.FilePath),
		BlockKind: BlockKindUnchanged,
	}
	return decision, RecordDecision(dir, decision)
}

func evaluateChanged(dir string, state *SessionState, entry *FileEntry, req Request, modTime int64, size int64) (Decision, error) {
	if req.IsFullFileRead() && entry.CachedContent != "" && size <= maxCachedFileBytes {
		newContent, err := os.ReadFile(req.FilePath)
		if err == nil {
			delta := buildDeltaSummary(req.FilePath, entry.CachedContent, string(newContent))
			updateEntry(entry, req, modTime, string(newContent))
			if err := readCacheSaveSession(dir, state); err != nil {
				return Decision{}, err
			}
			if delta != "" && len(delta) < len(newContent) {
				decision := Decision{Type: DecisionBlock, Reason: delta, BlockKind: BlockKindDelta}
				return decision, RecordDecision(dir, decision)
			}
			decision := Decision{Type: DecisionAllow}
			return decision, RecordDecision(dir, decision)
		}
	}

	content := ""
	if req.IsFullFileRead() && size <= maxCachedFileBytes {
		if raw, err := os.ReadFile(req.FilePath); err == nil {
			content = string(raw)
		}
	}
	updateEntry(entry, req, modTime, content)
	if err := readCacheSaveSession(dir, state); err != nil {
		return Decision{}, err
	}
	decision := Decision{Type: DecisionAllow}
	return decision, RecordDecision(dir, decision)
}

func sameRange(entry *FileEntry, req Request) bool {
	return entry.Offset == req.Offset && entry.Limit == req.Limit
}

func requestEntryKey(req Request) string {
	path := strings.TrimSpace(req.FilePath)
	if req.Offset == 0 && req.Limit == 0 {
		return path
	}
	return fmt.Sprintf("%s#range:%d:%d", path, req.Offset, req.Limit)
}

func updateEntry(entry *FileEntry, req Request, modTime int64, content string) {
	entry.Path = req.FilePath
	entry.LastTurnID = safeTurn(req.TurnID)
	entry.Offset = req.Offset
	entry.Limit = req.Limit
	entry.ModTimeUnixNs = modTime
	entry.CachedContent = content
	entry.ContentHash = hashObservedContent(content)
}

func updateObservedEntry(entry *FileEntry, req Request, hash string, archiveURI string, content string) {
	entry.Path = strings.TrimSpace(req.FilePath)
	entry.LastTurnID = safeTurn(req.TurnID)
	entry.Offset = req.Offset
	entry.Limit = req.Limit
	entry.ModTimeUnixNs = 0
	entry.ContentHash = hash
	entry.ArchiveURI = archiveURI
	if entry.ArchiveURI != "" && len(content) > maxObservedInlineCacheBytes {
		entry.CachedContent = ""
		return
	}
	entry.CachedContent = content
}

func updateObservedOutputEntry(entry *OutputEntry, req OutputRequest, hash string, archiveURI string, content string) {
	entry.Key = strings.TrimSpace(req.Key)
	entry.CommandLine = strings.TrimSpace(req.CommandLine)
	entry.LastTurnID = safeTurn(req.TurnID)
	entry.ContentHash = hash
	entry.ArchiveURI = archiveURI
	if entry.ArchiveURI != "" && len(content) > maxObservedInlineCacheBytes {
		entry.CachedContent = ""
		return
	}
	entry.CachedContent = content
}

func hashObservedContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func archiveObservedContent(archiveDir string, req Request, content string) (string, bool) {
	if archiveDir == "" {
		return "", false
	}
	entry, err := contentarchive.Put(archiveDir, contentarchive.Input{
		SessionID:    req.SessionID,
		MessageIndex: 0,
		BlockIndex:   0,
		SubLayer:     "readcache_proxy_delta",
		Original:     content,
		Preview:      fmt.Sprintf("file read %s", req.FilePath),
	}, contentarchive.Limits{})
	if err != nil || entry == nil {
		return "", false
	}
	return entry.URI, true
}

func archiveObservedOutputContent(archiveDir string, req OutputRequest, content string) (string, bool) {
	if archiveDir == "" {
		return "", false
	}
	entry, err := contentarchive.Put(archiveDir, contentarchive.Input{
		SessionID:    req.SessionID,
		MessageIndex: 0,
		BlockIndex:   0,
		SubLayer:     "tool_output_exact_dedup",
		Original:     content,
		Preview:      fmt.Sprintf("tool output %s", strings.TrimSpace(req.CommandLine)),
	}, contentarchive.Limits{})
	if err != nil || entry == nil {
		return "", false
	}
	return entry.URI, true
}

func unchangedReference(path string, archiveURI string) string {
	return fmt.Sprintf("Read note for %s: unchanged since previous full read.\nFull content: %s", path, archiveURI)
}

func unchangedOutputReference(commandLine string, archiveURI string) string {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		commandLine = "this tool command"
	}
	return fmt.Sprintf("Tool output note for %s: unchanged since previous emitted output.\nPrevious output: %s", commandLine, archiveURI)
}

func outputDeltaEligible(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), "search:")
}

func safeTurn(turnID string) string {
	return sessions.SafeOptionalTurnID(turnID)
}

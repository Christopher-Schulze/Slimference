package readcache

import (
	"fmt"
	"os"

	"github.com/slimference/slimference/internal/sessions"
)

const maxCachedFileBytes = 64 * 1024

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

	entry := state.Files[absPath]
	if entry == nil {
		entry = &FileEntry{Path: absPath}
		state.Files[absPath] = entry
	}

	req.FilePath = absPath
	modTime := info.ModTime().UnixNano()
	if sameRange(entry, req) && entry.ModTimeUnixNs == modTime {
		return blockUnchanged(dir, state, entry, req, modTime)
	}

	return evaluateChanged(dir, state, entry, req, modTime, info.Size())
}

func blockUnchanged(dir string, state *SessionState, entry *FileEntry, req Request, modTime int64) (Decision, error) {
	updateEntry(entry, req, modTime, entry.CachedContent)
	if err := readCacheSaveSession(dir, state); err != nil {
		return Decision{}, err
	}
	decision := Decision{
		Type:      DecisionBlock,
		Reason:    fmt.Sprintf("Slimference blocked this Read: %s is already in context and unchanged.", req.FilePath),
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

func updateEntry(entry *FileEntry, req Request, modTime int64, content string) {
	entry.Path = req.FilePath
	entry.LastTurnID = safeTurn(req.TurnID)
	entry.Offset = req.Offset
	entry.Limit = req.Limit
	entry.ModTimeUnixNs = modTime
	entry.CachedContent = content
}

func safeTurn(turnID string) string {
	return sessions.SafeOptionalTurnID(turnID)
}

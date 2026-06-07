package tui

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	dbg "github.com/slimference/slimference/internal/debug"
	_ "modernc.org/sqlite"
)

type codexThreadMetadata struct {
	ID           string
	Title        string
	CWD          string
	Source       string
	ThreadSource string
	Model        string
	UpdatedAt    time.Time
}

var (
	sqlOpenCodexThreadFunc       = func(driver, dsn string) (*sql.DB, error) { return sql.Open(driver, dsn) }
	loadCodexThreadMetadataFunc  = loadCodexThreadMetadata
	codexThreadMetadataCacheTTL  = 2 * time.Second
	codexThreadMetadataCacheMu   sync.Mutex
	codexThreadMetadataCacheAt   time.Time
	codexThreadMetadataCacheData = map[string]codexThreadMetadata{}
)

func lookupCodexThreadMetadataForFlights(flights []dbg.FlightRequestSummary) map[string]codexThreadMetadata {
	ids := make([]string, 0, len(flights))
	for _, flight := range flights {
		if id := normalizeCodexSessionID(flight.SessionID); id != "" {
			ids = append(ids, id)
		}
	}
	return lookupCodexThreadMetadata(ids)
}

func lookupCodexThreadMetadata(sessionIDs []string) map[string]codexThreadMetadata {
	unique := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		id := normalizeCodexSessionID(sessionID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return map[string]codexThreadMetadata{}
	}

	now := time.Now()
	codexThreadMetadataCacheMu.Lock()
	defer codexThreadMetadataCacheMu.Unlock()

	if now.Sub(codexThreadMetadataCacheAt) <= codexThreadMetadataCacheTTL {
		if out, ok := cachedCodexThreads(unique); ok {
			return out
		}
	}

	loaded, err := loadCodexThreadMetadataFunc(unique)
	if err == nil {
		if codexThreadMetadataCacheData == nil {
			codexThreadMetadataCacheData = map[string]codexThreadMetadata{}
		}
		for id, meta := range loaded {
			codexThreadMetadataCacheData[id] = meta
		}
		codexThreadMetadataCacheAt = now
	}
	out, _ := cachedCodexThreads(unique)
	return out
}

func cachedCodexThreads(ids []string) (map[string]codexThreadMetadata, bool) {
	out := make(map[string]codexThreadMetadata, len(ids))
	for _, id := range ids {
		meta, ok := codexThreadMetadataCacheData[id]
		if !ok {
			return out, false
		}
		out[id] = meta
	}
	return out, true
}

func loadCodexThreadMetadata(sessionIDs []string) (map[string]codexThreadMetadata, error) {
	home, err := userHomeDirFn()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".codex", "state_5.sqlite")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return map[string]codexThreadMetadata{}, nil
		}
		return nil, err
	}
	db, err := sqlOpenCodexThreadFunc("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	out := make(map[string]codexThreadMetadata, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		id := normalizeCodexSessionID(sessionID)
		if id == "" {
			continue
		}
		meta, ok, err := queryCodexThreadMetadata(db, id)
		if err != nil {
			return out, err
		}
		if ok {
			out[id] = meta
		}
	}
	return out, nil
}

func queryCodexThreadMetadata(db *sql.DB, id string) (codexThreadMetadata, bool, error) {
	row := db.QueryRow(`
SELECT id, title, cwd, source, COALESCE(thread_source, ''), COALESCE(model, ''), COALESCE(updated_at_ms, updated_at * 1000)
FROM threads
WHERE id = ?
LIMIT 1`, id)
	var meta codexThreadMetadata
	var updatedAtMS int64
	err := row.Scan(&meta.ID, &meta.Title, &meta.CWD, &meta.Source, &meta.ThreadSource, &meta.Model, &updatedAtMS)
	if err == sql.ErrNoRows {
		return codexThreadMetadata{}, false, nil
	}
	if err != nil {
		return codexThreadMetadata{}, false, err
	}
	if updatedAtMS > 0 {
		meta.UpdatedAt = time.UnixMilli(updatedAtMS).UTC()
	}
	return meta, true, nil
}

func normalizeCodexSessionID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "codex-wss:")
	value = strings.TrimPrefix(value, "codex-wss_")
	return value
}

func resetCodexThreadMetadataCacheForTest() {
	codexThreadMetadataCacheMu.Lock()
	defer codexThreadMetadataCacheMu.Unlock()
	codexThreadMetadataCacheAt = time.Time{}
	codexThreadMetadataCacheData = map[string]codexThreadMetadata{}
}

package codexthreads

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Metadata struct {
	ID           string
	Title        string
	CWD          string
	Source       string
	ThreadSource string
	Model        string
	UpdatedAt    time.Time
}

func LookupDefault(sessionIDs []string) (map[string]Metadata, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return Lookup(home, sessionIDs)
}

func Lookup(home string, sessionIDs []string) (map[string]Metadata, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return map[string]Metadata{}, nil
	}
	path := filepath.Join(home, ".codex", "state_5.sqlite")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return map[string]Metadata{}, nil
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	out := make(map[string]Metadata, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		id := NormalizeSessionID(sessionID)
		if id == "" {
			continue
		}
		meta, ok, err := query(db, id)
		if err != nil {
			return out, err
		}
		if ok {
			out[id] = meta
		}
	}
	return out, nil
}

func query(db *sql.DB, id string) (Metadata, bool, error) {
	row := db.QueryRow(`
SELECT id, title, cwd, source, COALESCE(thread_source, ''), COALESCE(model, ''), COALESCE(updated_at_ms, updated_at * 1000)
FROM threads
WHERE id = ?
LIMIT 1`, id)
	var meta Metadata
	var updatedAtMS int64
	err := row.Scan(&meta.ID, &meta.Title, &meta.CWD, &meta.Source, &meta.ThreadSource, &meta.Model, &updatedAtMS)
	if err == sql.ErrNoRows {
		return Metadata{}, false, nil
	}
	if err != nil {
		return Metadata{}, false, err
	}
	if updatedAtMS > 0 {
		meta.UpdatedAt = time.UnixMilli(updatedAtMS).UTC()
	}
	return meta, true, nil
}

func NormalizeSessionID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "codex-wss:")
	value = strings.TrimPrefix(value, "codex-wss_")
	return value
}

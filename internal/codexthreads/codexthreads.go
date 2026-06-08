package codexthreads

import (
	"database/sql"
	"fmt"
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

	columns, err := threadColumns(db)
	if err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return map[string]Metadata{}, nil
	}
	querySQL := threadQuerySQL(columns)

	out := make(map[string]Metadata, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		id := NormalizeSessionID(sessionID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		meta, ok, err := query(db, querySQL, id)
		if err != nil {
			return out, err
		}
		if ok {
			out[id] = meta
		}
	}
	return out, nil
}

func threadColumns(db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.Query(`PRAGMA table_info(threads)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, ok := columns["id"]; !ok {
		return map[string]struct{}{}, nil
	}
	return columns, nil
}

func threadQuerySQL(columns map[string]struct{}) string {
	return fmt.Sprintf(`
SELECT id, %s, %s, %s, %s, %s, %s
FROM threads
WHERE id = ?
LIMIT 1`,
		textColumnSQL(columns, "title"),
		textColumnSQL(columns, "cwd"),
		textColumnSQL(columns, "source"),
		textColumnSQL(columns, "thread_source"),
		textColumnSQL(columns, "model"),
		updatedAtSQL(columns),
	)
}

func textColumnSQL(columns map[string]struct{}, name string) string {
	if _, ok := columns[name]; ok {
		return "COALESCE(" + name + ", '')"
	}
	return "''"
}

func updatedAtSQL(columns map[string]struct{}) string {
	_, hasUpdatedAtMS := columns["updated_at_ms"]
	_, hasUpdatedAt := columns["updated_at"]
	switch {
	case hasUpdatedAtMS && hasUpdatedAt:
		return "COALESCE(updated_at_ms, updated_at * 1000)"
	case hasUpdatedAtMS:
		return "COALESCE(updated_at_ms, 0)"
	case hasUpdatedAt:
		return "COALESCE(updated_at * 1000, 0)"
	default:
		return "0"
	}
}

func query(db *sql.DB, querySQL string, id string) (Metadata, bool, error) {
	row := db.QueryRow(querySQL, id)
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

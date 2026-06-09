// Package repetition stores per-session tool-output repetition counts so
// the PostToolUse hook can emit a "[same as msg #N]" marker on the
// third or later occurrence of an identical (tool, args, output) tuple.
// T93. Storage is a single SQLite table; opening is one-shot per
// subprocess so the API stays trivial: Open -> Record -> Close.
package repetition

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	_ "modernc.org/sqlite"
)

// sqlOpenFunc / mkdirAllFunc / migrateFunc are overridable in tests so
// the error paths can be exercised without root permissions or a
// broken filesystem.
var (
	sqlOpenFunc           = func(driver, dsn string) (*sql.DB, error) { return sql.Open(driver, dsn) }
	mkdirAllFunc          = os.MkdirAll
	migrateFunc           = migrate
	ensureTextColumnFunc  = ensureTextColumn
	scanTextColumnFunc    = scanTextColumn
	textColumnRowsErrFunc = func(rows *sql.Rows) error { return rows.Err() }
)

// DefaultPath returns the canonical on-disk path for the repetition DB.
func DefaultPath(home string) string {
	return filepath.Join(home, ".slimference", "repetition.db")
}

// Open opens (and migrates) the repetition store. Caller must Close.
func Open(path string) (*sql.DB, error) {
	if err := mkdirAllFunc(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sqlOpenFunc("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrateFunc(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS posttool_repetitions (
  session_id TEXT NOT NULL,
  first_turn_id TEXT NOT NULL DEFAULT '',
  last_turn_id TEXT NOT NULL DEFAULT '',
  tool_key   TEXT NOT NULL,
  output_sha TEXT NOT NULL,
  hit_count  INTEGER NOT NULL,
  first_seen INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL,
  first_msg  INTEGER NOT NULL,
  PRIMARY KEY (session_id, tool_key, output_sha)
);
CREATE INDEX IF NOT EXISTS idx_posttool_rep_last ON posttool_repetitions(last_seen);
`)
	if err != nil {
		return err
	}
	if err := ensureTextColumnFunc(db, "posttool_repetitions", "first_turn_id"); err != nil {
		return err
	}
	return ensureTextColumnFunc(db, "posttool_repetitions", "last_turn_id")
}

// Key normalises the tuple identity used for repetition matching.
// Output is hashed so the store stays compact even for large bodies.
type Key struct {
	SessionID string
	TurnID    string
	ToolName  string
	Command   string
	Output    string
}

// hashKey returns the (session, tool_key, output_sha) triple used to
// look up a row.
func hashKey(k Key) (sessionID, toolKey, outputSHA string) {
	tool := strings.TrimSpace(k.ToolName) + "|" + strings.TrimSpace(k.Command)
	sum := sha256.Sum256([]byte(strings.TrimSpace(k.Output)))
	return sessions.SafeOptionalSessionID(k.SessionID), tool, hex.EncodeToString(sum[:])
}

// Record bumps the counter for k and returns the resulting hit count
// plus the message index the row was first associated with. msgIdx is
// the caller-provided "current message" used for the first row write.
// Empty session or empty tool key is a no-op (returns count=0).
func Record(db *sql.DB, k Key, msgIdx int) (count int, firstMsg int, err error) {
	sess, tool, sha := hashKey(k)
	if sess == "" || tool == "|" {
		return 0, 0, nil
	}
	turnID := sessions.SafeOptionalTurnID(k.TurnID)
	now := time.Now().Unix()

	row := db.QueryRow(`
SELECT hit_count, first_msg FROM posttool_repetitions
WHERE session_id = ? AND tool_key = ? AND output_sha = ?`, sess, tool, sha)
	var prev int
	var prevMsg int
	scanErr := row.Scan(&prev, &prevMsg)
	if scanErr == nil {
		newCount := prev + 1
		if _, err := db.Exec(`
UPDATE posttool_repetitions SET hit_count = ?, last_seen = ?, last_turn_id = ?
WHERE session_id = ? AND tool_key = ? AND output_sha = ?`,
			newCount, now, turnID, sess, tool, sha); err != nil {
			return 0, 0, err
		}
		return newCount, prevMsg, nil
	}
	if scanErr != sql.ErrNoRows {
		return 0, 0, scanErr
	}
	if _, err := db.Exec(`
INSERT INTO posttool_repetitions (session_id, first_turn_id, last_turn_id, tool_key, output_sha, hit_count, first_seen, last_seen, first_msg)
VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)`, sess, turnID, turnID, tool, sha, now, now, msgIdx); err != nil {
		return 0, 0, err
	}
	return 1, msgIdx, nil
}

func ensureTextColumn(db *sql.DB, table, column string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		name, err := scanTextColumnFunc(rows)
		if err != nil {
			return err
		}
		if name == column {
			return textColumnRowsErrFunc(rows)
		}
	}
	if err := textColumnRowsErrFunc(rows); err != nil {
		return err
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " TEXT NOT NULL DEFAULT ''")
	return err
}

func scanTextColumn(rows *sql.Rows) (string, error) {
	var cid int
	var name, typ string
	var notNull int
	var defaultValue any
	var pk int
	if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
		return "", err
	}
	return name, nil
}

// Forget drops state for one session id. Used when an operator clears
// a session manually or when a session ends.
func Forget(db *sql.DB, sessionID string) error {
	sessionID = sessions.SafeOptionalSessionID(sessionID)
	if sessionID == "" {
		return nil
	}
	_, err := db.Exec(`DELETE FROM posttool_repetitions WHERE session_id = ?`, sessionID)
	return err
}

// Stats reports aggregate counters for /admin/status surfaces.
type Stats struct {
	Rows           int64 `json:"rows"`
	UniqueSessions int64 `json:"unique_sessions"`
	MaxHitCount    int64 `json:"max_hit_count"`
}

// Snapshot returns the current store snapshot. Errors are returned
// rather than logged so callers can decide whether to surface them.
func Snapshot(db *sql.DB) (Stats, error) {
	row := db.QueryRow(`
SELECT COUNT(*), COUNT(DISTINCT session_id), COALESCE(MAX(hit_count), 0)
FROM posttool_repetitions`)
	var s Stats
	if err := row.Scan(&s.Rows, &s.UniqueSessions, &s.MaxHitCount); err != nil {
		return Stats{}, err
	}
	return s, nil
}

// Marker returns the canonical "[same as msg #N (Mx Tool)]" string for
// a repetition hit. Format kept short so it survives downstream
// compression and stays unambiguous in transcripts.
func Marker(toolName string, firstMsg, hitCount int) string {
	if toolName == "" {
		toolName = "tool"
	}
	return fmt.Sprintf("[%s output identical to msg #%d (seen %d times)]", toolName, firstMsg, hitCount)
}

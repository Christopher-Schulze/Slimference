// Package filter will host Layer-0 pre-entry filtering. This file holds SQLite persistence
// for per-command filter savings (spec+: slimference filter tracking).
package filter

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// sqlOpenFunc is set to sql.Open; replaced in tests to inject open errors.
var sqlOpenFunc = func(driver, dsn string) (*sql.DB, error) { return sql.Open(driver, dsn) }

// OpenDB opens (and migrates) the filter tracking database at path (e.g. ~/.slimference/filter.db).
func OpenDB(path string) (*sql.DB, error) {
	db, err := sqlOpenFunc("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS filter_runs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	command TEXT NOT NULL,
	project_path TEXT NOT NULL,
	input_tokens INTEGER NOT NULL,
	output_tokens INTEGER NOT NULL,
	savings_pct REAL NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_filter_runs_created ON filter_runs(created_at);
CREATE TABLE IF NOT EXISTS filter_observations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	scope TEXT NOT NULL,
	command TEXT NOT NULL,
	project_path TEXT NOT NULL,
	input_tokens INTEGER NOT NULL,
	output_tokens INTEGER NOT NULL,
	outcome TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_filter_observations_created ON filter_observations(created_at);
CREATE INDEX IF NOT EXISTS idx_filter_observations_scope_created ON filter_observations(scope, created_at);
`)
	return err
}

// RecordFilterRun appends one tracking row. createdAt is stored as Unix seconds.
func RecordFilterRun(db *sql.DB, command, projectPath string, inputTokens, outputTokens int, savingsPct float64, createdAt time.Time) error {
	_, err := db.Exec(
		`INSERT INTO filter_runs (command, project_path, input_tokens, output_tokens, savings_pct, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		command, projectPath, inputTokens, outputTokens, savingsPct, createdAt.Unix(),
	)
	return err
}

// RecordFilterObservation appends one non-savings observation row. It is used
// for local opportunity ranking and must not affect filter_runs savings totals.
func RecordFilterObservation(db *sql.DB, scope, command, projectPath string, inputTokens, outputTokens int, outcome string, createdAt time.Time) error {
	_, err := db.Exec(
		`INSERT INTO filter_observations (scope, command, project_path, input_tokens, output_tokens, outcome, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		scope, command, projectPath, inputTokens, outputTokens, outcome, createdAt.Unix(),
	)
	return err
}

// FilterRun is one persisted Layer-0 tracking row.
type FilterRun struct {
	ID           int64     `json:"id"`
	Command      string    `json:"command"`
	ProjectPath  string    `json:"project_path"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	SavingsPct   float64   `json:"savings_pct"`
	CreatedAt    time.Time `json:"created_at"`
}

// FilterObservationAggregate is a grouped non-savings observation row.
type FilterObservationAggregate struct {
	Scope        string `json:"scope,omitempty"`
	Command      string `json:"command"`
	Outcome      string `json:"outcome,omitempty"`
	Runs         int64  `json:"runs"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

// QueryFilterObservationByCommand returns local opportunity rows sorted by
// observed input-token mass, not by savings.
func QueryFilterObservationByCommand(db *sql.DB, scope string, start, end time.Time, limit int) ([]FilterObservationAggregate, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := db.Query(`
SELECT command,
       outcome,
       COUNT(*),
       COALESCE(SUM(input_tokens), 0),
       COALESCE(SUM(output_tokens), 0)
FROM filter_observations
WHERE scope = ? AND created_at >= ? AND created_at <= ?
GROUP BY command, outcome
ORDER BY SUM(input_tokens) DESC, COUNT(*) DESC, command ASC
LIMIT ?`, scope, start.Unix(), end.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FilterObservationAggregate, 0, limit)
	for rows.Next() {
		var row FilterObservationAggregate
		row.Scope = scope
		if err := rows.Scan(&row.Command, &row.Outcome, &row.Runs, &row.InputTokens, &row.OutputTokens); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// LastFilterRun returns the newest row by primary key, or ok=false if the table is empty.
func LastFilterRun(db *sql.DB) (run FilterRun, ok bool, err error) {
	row := db.QueryRow(`
SELECT id, command, project_path, input_tokens, output_tokens, savings_pct, created_at
FROM filter_runs
ORDER BY id DESC
LIMIT 1`)
	var createdUnix int64
	err = row.Scan(&run.ID, &run.Command, &run.ProjectPath, &run.InputTokens, &run.OutputTokens, &run.SavingsPct, &createdUnix)
	if err == sql.ErrNoRows {
		return FilterRun{}, false, nil
	}
	if err != nil {
		return FilterRun{}, false, err
	}
	run.CreatedAt = time.Unix(createdUnix, 0).UTC()
	return run, true, nil
}

// FilterRunsAggregate is SUM/COUNT over filter_runs in [start, end] (inclusive Unix seconds).
type FilterRunsAggregate struct {
	Period         string `json:"period,omitempty"`
	StartUnix      int64  `json:"start_unix"`
	EndUnix        int64  `json:"end_unix"`
	Runs           int64  `json:"runs"`
	InputTokens    int64  `json:"input_tokens"`
	OutputTokens   int64  `json:"output_tokens"`
	TokensSavedEst int64  `json:"tokens_saved_est"`
}

// QueryFilterRunsAggregate returns SUM/COUNT for rows with created_at between start and end (inclusive).
func QueryFilterRunsAggregate(db *sql.DB, start, end time.Time) (FilterRunsAggregate, error) {
	startSec := start.Unix()
	endSec := end.Unix()
	var runs, inTok, outTok, saved sql.NullInt64
	err := db.QueryRow(`
SELECT COUNT(*),
       COALESCE(SUM(input_tokens), 0),
       COALESCE(SUM(output_tokens), 0),
       COALESCE(SUM(input_tokens - output_tokens), 0)
FROM filter_runs
WHERE created_at >= ? AND created_at <= ?
`, startSec, endSec).Scan(&runs, &inTok, &outTok, &saved)
	if err != nil {
		return FilterRunsAggregate{}, err
	}
	return FilterRunsAggregate{
		StartUnix:      startSec,
		EndUnix:        endSec,
		Runs:           runs.Int64,
		InputTokens:    inTok.Int64,
		OutputTokens:   outTok.Int64,
		TokensSavedEst: saved.Int64,
	}, nil
}

// RecentFilterRuns returns up to limit newest rows by id (newest first).
func RecentFilterRuns(db *sql.DB, limit int) ([]FilterRun, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := db.Query(`
SELECT id, command, project_path, input_tokens, output_tokens, savings_pct, created_at
FROM filter_runs
ORDER BY id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FilterRun
	for rows.Next() {
		var r FilterRun
		var createdUnix int64
		if err := rows.Scan(&r.ID, &r.Command, &r.ProjectPath, &r.InputTokens, &r.OutputTokens, &r.SavingsPct, &createdUnix); err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(createdUnix, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

package analytics

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/filter"
)

// FilterGainSummary aggregates rows from filter_runs (Layer 0 SQLite tracking).
type FilterGainSummary struct {
	Period       string `json:"period"`
	StartUnix    int64  `json:"start_unix"`
	EndUnix      int64  `json:"end_unix"`
	Runs         int64  `json:"runs"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	// TokensSavedEst sums max(0, input_tokens-output_tokens) per run (byte/4 estimates).
	TokensSavedEst int64 `json:"tokens_saved_est"`
	// ProjectPathFilter is the normalized --project filter path, if any.
	ProjectPathFilter string `json:"project_path_filter,omitempty"`
	// USDPerMillionTokens is the rate used for SavingsUsdEst (from config/env when > 0).
	USDPerMillionTokens float64 `json:"usd_per_million_tokens,omitempty"`
	// SavingsUsdEst is rough $ = tokens_saved_est / 1e6 * USDPerMillionTokens.
	SavingsUsdEst float64 `json:"savings_usd_est,omitempty"`
}

// FilterGainByCommandRow is one command label (as stored) with aggregates for the window.
type FilterGainByCommandRow struct {
	Command        string  `json:"command"`
	Runs           int64   `json:"runs"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	TokensSavedEst int64   `json:"tokens_saved_est"`
	SavingsUsdEst  float64 `json:"savings_usd_est,omitempty"`
}

// FilterGainByParserRow groups Layer 0 filter-run rows by parser/tool family.
type FilterGainByParserRow struct {
	Parser         string  `json:"parser"`
	Runs           int64   `json:"runs"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	TokensSavedEst int64   `json:"tokens_saved_est"`
	SavingsUsdEst  float64 `json:"savings_usd_est,omitempty"`
}

// FilterGainReport is the summary plus an optional per-command breakdown.
type FilterGainReport struct {
	FilterGainSummary
	ByCommand []FilterGainByCommandRow `json:"by_command,omitempty"`
	ByParser  []FilterGainByParserRow  `json:"by_parser,omitempty"`
}

// FilterGainWindow returns [start, end] for named periods (local time). end is inclusive for display.
func FilterGainWindow(period string, now time.Time) (start, end time.Time, err error) {
	end = now
	switch period {
	case "today":
		y, m, d := now.Date()
		start = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case "week":
		start = now.AddDate(0, 0, -7)
	case "month":
		start = now.AddDate(0, 0, -30)
	case "all":
		start = time.Unix(0, 0).In(now.Location())
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("unknown period %q", period)
	}
	if start.After(end) {
		start = end
	}
	return start, end, nil
}

// QueryFilterGain reads ~/.slimference/filter.db (or any path with the same schema) and aggregates.
func QueryFilterGain(dbPath string, period string, now time.Time) (FilterGainSummary, error) {
	r, err := QueryFilterGainReport(dbPath, period, now, false, "", 0)
	if err != nil {
		return FilterGainSummary{}, err
	}
	return r.FilterGainSummary, nil
}

// QueryFilterGainReport loads summary and optionally per-command rows in one DB open.
// projectRoot filters by project_path (exact match or subdirectory of projectRoot); empty = all projects.
// usdPerMillionTokens, if > 0, fills SavingsUsdEst fields (tokens_saved / 1e6 * rate).
func QueryFilterGainReport(dbPath string, period string, now time.Time, byCommand bool, projectRoot string, usdPerMillionTokens float64) (FilterGainReport, error) {
	return QueryFilterGainReportWithOptions(dbPath, period, now, byCommand, false, projectRoot, usdPerMillionTokens)
}

// QueryFilterGainReportWithOptions loads summary and optional per-command /
// per-parser rows in one DB open.
func QueryFilterGainReportWithOptions(dbPath string, period string, now time.Time, byCommand, byParser bool, projectRoot string, usdPerMillionTokens float64) (FilterGainReport, error) {
	start, end, err := FilterGainWindow(period, now)
	if err != nil {
		return FilterGainReport{}, err
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		return FilterGainReport{}, err
	}
	defer db.Close()
	proj := normalizeGainProjectFilter(projectRoot)
	summary, err := queryFilterGainDB(db, period, start, end, proj)
	if err != nil {
		return FilterGainReport{}, err
	}
	rep := FilterGainReport{FilterGainSummary: summary}
	if !byCommand && !byParser {
		applyGainUSD(&rep.FilterGainSummary, nil, usdPerMillionTokens)
		return rep, nil
	}
	if byCommand || byParser {
		rows, err := queryFilterGainByCommandDB(db, start, end, proj)
		if err != nil {
			return FilterGainReport{}, err
		}
		if byCommand {
			rep.ByCommand = rows
		}
		if byParser {
			rep.ByParser = groupGainRowsByParser(rows)
		}
	}
	applyGainUSD(&rep.FilterGainSummary, &rep.ByCommand, usdPerMillionTokens)
	applyGainParserUSD(&rep.ByParser, usdPerMillionTokens)
	return rep, nil
}

func applyGainUSD(s *FilterGainSummary, rows *[]FilterGainByCommandRow, usdPerMillion float64) {
	if usdPerMillion <= 0 || s == nil {
		return
	}
	s.USDPerMillionTokens = usdPerMillion
	s.SavingsUsdEst = float64(s.TokensSavedEst) / 1e6 * usdPerMillion
	if rows == nil {
		return
	}
	for i := range *rows {
		(*rows)[i].SavingsUsdEst = float64((*rows)[i].TokensSavedEst) / 1e6 * usdPerMillion
	}
}

func applyGainParserUSD(rows *[]FilterGainByParserRow, usdPerMillion float64) {
	if usdPerMillion <= 0 || rows == nil {
		return
	}
	for i := range *rows {
		(*rows)[i].SavingsUsdEst = float64((*rows)[i].TokensSavedEst) / 1e6 * usdPerMillion
	}
}

func normalizeGainProjectFilter(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return filepath.Clean(s)
}

func queryFilterGainDB(db *sql.DB, period string, start, end time.Time, projectRoot string) (FilterGainSummary, error) {
	startSec := start.Unix()
	endSec := end.Unix()
	var runs, inTok, outTok, saved sql.NullInt64
	// Prefix match uses / for both sides so subdirs work on Windows paths in DB.
	err := db.QueryRow(`
SELECT COUNT(*),
       COALESCE(SUM(input_tokens), 0),
       COALESCE(SUM(output_tokens), 0),
       COALESCE(SUM(CASE WHEN input_tokens > output_tokens THEN input_tokens - output_tokens ELSE 0 END), 0)
FROM filter_runs
WHERE created_at >= ? AND created_at <= ?
  AND (? = '' OR project_path = ?
   OR (? != '' AND instr(
        REPLACE(project_path, CHAR(92), '/'),
        REPLACE(?, CHAR(92), '/') || '/') = 1))
`, startSec, endSec, projectRoot, projectRoot, projectRoot, projectRoot).Scan(&runs, &inTok, &outTok, &saved)
	if err != nil {
		return FilterGainSummary{}, err
	}
	summary := FilterGainSummary{
		Period:            period,
		StartUnix:         startSec,
		EndUnix:           endSec,
		Runs:              runs.Int64,
		InputTokens:       inTok.Int64,
		OutputTokens:      outTok.Int64,
		TokensSavedEst:    saved.Int64,
		ProjectPathFilter: projectRoot,
	}
	return summary, nil
}

func queryFilterGainByCommandDB(db *sql.DB, start, end time.Time, projectRoot string) ([]FilterGainByCommandRow, error) {
	startSec := start.Unix()
	endSec := end.Unix()
	rows, err := db.Query(`
SELECT command,
       COUNT(*),
       COALESCE(SUM(input_tokens), 0),
       COALESCE(SUM(output_tokens), 0),
       COALESCE(SUM(CASE WHEN input_tokens > output_tokens THEN input_tokens - output_tokens ELSE 0 END), 0)
FROM filter_runs
WHERE created_at >= ? AND created_at <= ?
  AND (? = '' OR project_path = ?
   OR (? != '' AND instr(
        REPLACE(project_path, CHAR(92), '/'),
        REPLACE(?, CHAR(92), '/') || '/') = 1))
GROUP BY command
ORDER BY 5 DESC
`, startSec, endSec, projectRoot, projectRoot, projectRoot, projectRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FilterGainByCommandRow
	for rows.Next() {
		var r FilterGainByCommandRow
		if err := rows.Scan(&r.Command, &r.Runs, &r.InputTokens, &r.OutputTokens, &r.TokensSavedEst); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func groupGainRowsByParser(rows []FilterGainByCommandRow) []FilterGainByParserRow {
	grouped := make(map[string]*FilterGainByParserRow)
	for _, row := range rows {
		parser := ParserFamilyForCommand(row.Command)
		dst := grouped[parser]
		if dst == nil {
			dst = &FilterGainByParserRow{Parser: parser}
			grouped[parser] = dst
		}
		dst.Runs += row.Runs
		dst.InputTokens += row.InputTokens
		dst.OutputTokens += row.OutputTokens
		dst.TokensSavedEst += row.TokensSavedEst
	}
	out := make([]FilterGainByParserRow, 0, len(grouped))
	for _, row := range grouped {
		out = append(out, *row)
	}
	sortGainByParser(out)
	return out
}

func sortGainByParser(rows []FilterGainByParserRow) {
	for i := 1; i < len(rows); i++ {
		current := rows[i]
		j := i - 1
		for ; j >= 0 && (rows[j].TokensSavedEst < current.TokensSavedEst ||
			(rows[j].TokensSavedEst == current.TokensSavedEst && rows[j].Parser > current.Parser)); j-- {
			rows[j+1] = rows[j]
		}
		rows[j+1] = current
	}
}

// ParserFamilyForCommand maps a persisted filter_runs command label to the
// practical parser/tool family used by gain --by-parser.
func ParserFamilyForCommand(command string) string {
	c := strings.ToLower(command)
	switch {
	case strings.Contains(c, "svelte"):
		return "svelte"
	case commandContainsAny(c, "typescript", "tsc", "tsx", "tsserver", "vue-tsc"):
		return "typescript"
	case commandContainsAny(c,
		"javascript", "node", "bun", "npm", "pnpm", "yarn",
		"vite", "next", "react", "jest", "vitest", "playwright",
		"eslint", "biome", "oxlint", "turbo", "nx", "lerna",
	):
		return "javascript"
	case strings.Contains(c, "zig"):
		return "zig"
	case commandContainsAny(c,
		"sql", "psql", "sqlite", "mysql", "mariadb", "migration",
		"prisma", "drizzle", "sqlfluff", "sqruff",
	):
		return "sql"
	case strings.Contains(c, "markdown") || strings.Contains(c, "mdformat"):
		return "markdown"
	case commandContainsAny(c, "cargo", "rust", "clippy", "rustfmt"):
		return "rust"
	case strings.Contains(c, "go ") || strings.Contains(c, "go-") || strings.Contains(c, "gopls"):
		return "go"
	case commandContainsAny(c,
		"python", "pytest", "unittest", "mypy", "ruff", "pylint",
		"flake8", "pyright", "basedpyright", "uv ", "[uv]",
		"poetry",
	):
		return "python"
	case commandContainsAny(c,
		"javac", "java ", "kotlinc", "kotlin", "gradle", "mvn ", "mvnw",
		"maven", "sbt", "scalac", "swift", "xcodebuild", "dart",
		"flutter", "php", "phpunit", "phpstan", "psalm", "composer",
	):
		return "jvm_mobile_php"
	case commandContainsAny(c, "gcc", "g++", "clang", "clang++", "cmake", "ninja", "make"):
		return "c_cpp_build"
	case commandContainsAny(c, "docker", "podman", "nerdctl", "kubectl", "kube", "oc ", "[oc]", "helm", "hadolint"):
		return "container"
	case commandContainsAny(c, "terraform", "tofu", "hcl"):
		return "hcl"
	case strings.Contains(c, "git"):
		return "git"
	case strings.Contains(c, "ruby"):
		return "ruby"
	case strings.Contains(c, "shell") || strings.Contains(c, "bash") || strings.Contains(c, "zsh") || strings.Contains(c, "shfmt"):
		return "shell"
	default:
		return "other"
	}
}

func commandContainsAny(command string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(command, needle) {
			return true
		}
	}
	return false
}

// FormatFilterGainReportJSON pretty-prints a report for CLI --json.
func FormatFilterGainReportJSON(r FilterGainReport) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// FormatFilterGainJSON pretty-prints summary-only JSON (same shape as report without by_command).
func FormatFilterGainJSON(s FilterGainSummary) ([]byte, error) {
	return FormatFilterGainReportJSON(FilterGainReport{FilterGainSummary: s})
}

// WriteGainSummaryCSV writes one header row and one data row for the window summary.
func WriteGainSummaryCSV(w io.Writer, s FilterGainSummary) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"period", "runs", "input_tokens", "output_tokens", "tokens_saved_est", "usd_per_million_tokens", "savings_usd_est"})
	row := []string{
		s.Period,
		strconv.FormatInt(s.Runs, 10),
		strconv.FormatInt(s.InputTokens, 10),
		strconv.FormatInt(s.OutputTokens, 10),
		strconv.FormatInt(s.TokensSavedEst, 10),
		strconv.FormatFloat(s.USDPerMillionTokens, 'g', -1, 64),
		strconv.FormatFloat(s.SavingsUsdEst, 'g', -1, 64),
	}
	if err := cw.Write(row); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

// WriteGainByCommandCSV writes a CSV table of per-command aggregates.
func WriteGainByCommandCSV(w io.Writer, rows []FilterGainByCommandRow) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"command", "runs", "input_tokens", "output_tokens", "tokens_saved_est", "savings_usd_est"})
	for _, r := range rows {
		line := []string{
			r.Command,
			strconv.FormatInt(r.Runs, 10),
			strconv.FormatInt(r.InputTokens, 10),
			strconv.FormatInt(r.OutputTokens, 10),
			strconv.FormatInt(r.TokensSavedEst, 10),
			strconv.FormatFloat(r.SavingsUsdEst, 'g', -1, 64),
		}
		_ = cw.Write(line)
	}
	cw.Flush()
	return cw.Error()
}

// WriteGainByParserCSV writes a CSV table of per-parser aggregates.
func WriteGainByParserCSV(w io.Writer, rows []FilterGainByParserRow) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"parser", "runs", "input_tokens", "output_tokens", "tokens_saved_est", "savings_usd_est"})
	for _, r := range rows {
		line := []string{
			r.Parser,
			strconv.FormatInt(r.Runs, 10),
			strconv.FormatInt(r.InputTokens, 10),
			strconv.FormatInt(r.OutputTokens, 10),
			strconv.FormatInt(r.TokensSavedEst, 10),
			strconv.FormatFloat(r.SavingsUsdEst, 'g', -1, 64),
		}
		_ = cw.Write(line)
	}
	cw.Flush()
	return cw.Error()
}

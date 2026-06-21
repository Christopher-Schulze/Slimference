package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

const debugBundleSchemaVersion = 1

type debugBundleOptions struct {
	OutDir      string
	FlightLimit int
	FilterLimit int
	LogLines    int
	AdminURL    string
	Help        bool
}

type debugBundleManifest struct {
	SchemaVersion int                     `json:"schema_version"`
	GeneratedAt   time.Time               `json:"generated_at"`
	Version       string                  `json:"slimference_version"`
	OutputDir     string                  `json:"output_dir"`
	Limits        debugBundleManifestCaps `json:"limits"`
	Files         map[string]string       `json:"files"`
	Missing       []string                `json:"missing,omitempty"`
	Notes         []string                `json:"notes,omitempty"`
}

type debugBundleManifestCaps struct {
	FlightRows int `json:"flight_rows"`
	FilterRows int `json:"filter_rows"`
	LogLines   int `json:"log_lines"`
}

type debugBundlePaths struct {
	ConfigFile   string `json:"config_file"`
	ConfigSource string `json:"config_source"`
	AnalyticsDir string `json:"analytics_dir"`
	DataDir      string `json:"data_dir,omitempty"`
	FilterDB     string `json:"filter_db,omitempty"`
	TeeDir       string `json:"tee_dir,omitempty"`
	DecisionsLog string `json:"decisions_log,omitempty"`
	DaemonStdout string `json:"daemon_stdout"`
	DaemonStderr string `json:"daemon_stderr"`
}

type debugBundleFilterRun struct {
	ID           int64     `json:"id"`
	CommandHash  string    `json:"command_hash,omitempty"`
	ProjectHash  string    `json:"project_hash,omitempty"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	SavedTokens  int       `json:"saved_tokens"`
	SavingsPct   float64   `json:"savings_pct"`
	CreatedAt    time.Time `json:"created_at"`
}

type debugBundleLogTail struct {
	Path  string   `json:"path"`
	Lines []string `json:"lines"`
	Error string   `json:"error,omitempty"`
}

func handleDebugBundle(args []string) {
	opts, err := parseDebugBundleArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
		return
	}
	if opts.Help {
		fmt.Println("usage: slimference debug bundle [--out DIR] [--flight-limit N] [--filter-limit N] [--log-lines N] [--admin-url URL]")
		return
	}
	cfg, info, err := config.LoadWithOptions(config.LoadOptions{ExplicitPath: explicitConfigPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	outDir := opts.OutDir
	if outDir == "" {
		outDir = defaultDebugBundleDir(time.Now().UTC())
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "debug bundle mkdir: %v\n", err)
		exitFn(1)
		return
	}
	manifest := writeDebugBundle(outDir, cfg, info, opts, time.Now().UTC())
	manifestPath := filepath.Join(outDir, "manifest.json")
	if err := writeDebugBundleJSON(manifestPath, manifest); err != nil {
		fmt.Fprintf(os.Stderr, "debug bundle manifest: %v\n", err)
		exitFn(1)
		return
	}
	fmt.Printf("Debug diagnostics bundle written to %s\n", outDir)
	fmt.Printf("Manifest: %s\n", manifestPath)
	if len(manifest.Missing) > 0 {
		fmt.Printf("Missing optional sources: %s\n", strings.Join(manifest.Missing, ", "))
	}
}

func parseDebugBundleArgs(args []string) (debugBundleOptions, error) {
	opts := debugBundleOptions{FlightLimit: 200, FilterLimit: 200, LogLines: 200}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "":
			continue
		case arg == "--help" || arg == "-h":
			opts.Help = true
		case arg == "--out":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return opts, fmt.Errorf("--out requires a directory")
			}
			opts.OutDir = filepath.Clean(config.ExpandHomePath(args[i]))
		case strings.HasPrefix(arg, "--out="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--out="))
			if value == "" {
				return opts, fmt.Errorf("--out requires a directory")
			}
			opts.OutDir = filepath.Clean(config.ExpandHomePath(value))
		case arg == "--flight-limit":
			i++
			n, err := parseDebugBundlePositiveInt(args, i, "--flight-limit")
			if err != nil {
				return opts, err
			}
			opts.FlightLimit = n
		case strings.HasPrefix(arg, "--flight-limit="):
			n, err := parseDebugBundleIntValue(strings.TrimPrefix(arg, "--flight-limit="), "--flight-limit")
			if err != nil {
				return opts, err
			}
			opts.FlightLimit = n
		case arg == "--filter-limit":
			i++
			n, err := parseDebugBundlePositiveInt(args, i, "--filter-limit")
			if err != nil {
				return opts, err
			}
			opts.FilterLimit = n
		case strings.HasPrefix(arg, "--filter-limit="):
			n, err := parseDebugBundleIntValue(strings.TrimPrefix(arg, "--filter-limit="), "--filter-limit")
			if err != nil {
				return opts, err
			}
			opts.FilterLimit = n
		case arg == "--log-lines":
			i++
			n, err := parseDebugBundlePositiveInt(args, i, "--log-lines")
			if err != nil {
				return opts, err
			}
			opts.LogLines = n
		case strings.HasPrefix(arg, "--log-lines="):
			n, err := parseDebugBundleIntValue(strings.TrimPrefix(arg, "--log-lines="), "--log-lines")
			if err != nil {
				return opts, err
			}
			opts.LogLines = n
		case arg == "--admin-url":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return opts, fmt.Errorf("--admin-url requires a URL")
			}
			opts.AdminURL = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--admin-url="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--admin-url="))
			if value == "" {
				return opts, fmt.Errorf("--admin-url requires a URL")
			}
			opts.AdminURL = value
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown flag: %s", arg)
		default:
			return opts, fmt.Errorf("unexpected argument: %s", arg)
		}
	}
	opts.FlightLimit = clampDebugBundleLimit(opts.FlightLimit, 500)
	opts.FilterLimit = clampDebugBundleLimit(opts.FilterLimit, 500)
	opts.LogLines = clampDebugBundleLimit(opts.LogLines, 1000)
	return opts, nil
}

func parseDebugBundlePositiveInt(args []string, index int, name string) (int, error) {
	if index >= len(args) {
		return 0, fmt.Errorf("%s requires a positive integer", name)
	}
	return parseDebugBundleIntValue(args[index], name)
}

func parseDebugBundleIntValue(value, name string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s requires a positive integer", name)
	}
	return n, nil
}

func clampDebugBundleLimit(n, max int) int {
	if n < 1 {
		return 1
	}
	if n > max {
		return max
	}
	return n
}

func defaultDebugBundleDir(now time.Time) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", "slimference-diagnostics-"+now.Format("20060102T150405Z"))
	}
	return filepath.Join(home, ".slimference", "exports", "diagnostics-"+now.Format("20060102T150405Z"))
}

func writeDebugBundle(outDir string, cfg *config.Config, info config.LoadInfo, opts debugBundleOptions, now time.Time) debugBundleManifest {
	manifest := debugBundleManifest{
		SchemaVersion: debugBundleSchemaVersion,
		GeneratedAt:   now,
		Version:       version,
		OutputDir:     outDir,
		Limits: debugBundleManifestCaps{
			FlightRows: opts.FlightLimit,
			FilterRows: opts.FilterLimit,
			LogLines:   opts.LogLines,
		},
		Files: map[string]string{},
		Notes: []string{
			"Bundle is bounded and content-free by design: no raw prompts, tool outputs, raw WSS frames, auth material, or capture archives.",
			"Filter rows hash command/project strings instead of exporting them.",
		},
	}
	writeDebugBundlePaths(outDir, cfg, info, &manifest)
	writeDebugBundleAdminState(outDir, cfg, opts, &manifest)
	writeDebugBundleSavings(outDir, cfg, &manifest, now)
	writeDebugBundleDecisions(outDir, cfg, opts, &manifest)
	writeDebugBundleFilterRuns(outDir, opts, &manifest)
	writeDebugBundleDaemonLogs(outDir, opts, &manifest)
	return manifest
}

func writeDebugBundlePaths(outDir string, cfg *config.Config, info config.LoadInfo, manifest *debugBundleManifest) {
	configPath := ""
	if info.ResolvedPath != "" {
		configPath = info.ResolvedPath
	}
	dataDir, _ := filterDefaultDataDirFn()
	filterPath, _ := resolveFilterDBPathFn()
	teeDir, _ := resolveTeeDirFn()
	paths := debugBundlePaths{
		ConfigFile:   configPath,
		ConfigSource: info.Source,
		AnalyticsDir: cfg.Analytics.ResolvedLogDir(),
		DataDir:      dataDir,
		FilterDB:     filterPath,
		TeeDir:       teeDir,
		DecisionsLog: filepath.Clean(config.ExpandHomePath(strings.TrimSpace(cfg.Debug.DecisionsLog))),
		DaemonStdout: daemonStdoutLogPathFn(),
		DaemonStderr: daemonStderrLogPathFn(),
	}
	if strings.TrimSpace(cfg.Debug.DecisionsLog) == "" {
		paths.DecisionsLog = ""
	}
	writeDebugBundleJSONFile(outDir, "paths.json", paths, manifest)
}

func writeDebugBundleAdminState(outDir string, cfg *config.Config, opts debugBundleOptions, manifest *debugBundleManifest) {
	adminURL := strings.TrimSpace(opts.AdminURL)
	if adminURL == "" {
		adminURL = defaultDebugBundleAdminURL(cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adminURL, nil)
	if err != nil {
		writeDebugBundleTextFile(outDir, "admin-state-error.txt", err.Error()+"\n", manifest)
		manifest.Missing = append(manifest.Missing, "admin-state")
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeDebugBundleTextFile(outDir, "admin-state-error.txt", err.Error()+"\n", manifest)
		manifest.Missing = append(manifest.Missing, "admin-state")
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		writeDebugBundleTextFile(outDir, "admin-state-error.txt", err.Error()+"\n", manifest)
		manifest.Missing = append(manifest.Missing, "admin-state")
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		writeDebugBundleTextFile(outDir, "admin-state-error.txt", fmt.Sprintf("status=%d\n%s\n", resp.StatusCode, string(data)), manifest)
		manifest.Missing = append(manifest.Missing, "admin-state")
		return
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		writeDebugBundleTextFile(outDir, "admin-state-error.txt", "invalid json: "+err.Error()+"\n", manifest)
		manifest.Missing = append(manifest.Missing, "admin-state")
		return
	}
	writeDebugBundleJSONFile(outDir, "admin-state.json", decoded, manifest)
}

func defaultDebugBundleAdminURL(cfg *config.Config) string {
	host := strings.TrimSpace(cfg.Proxy.ListenAddress)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%d/_slimference/admin/state", host, cfg.Proxy.ListenPort)
}

func writeDebugBundleSavings(outDir string, cfg *config.Config, manifest *debugBundleManifest, now time.Time) {
	summary := computeSavings(cfg, "today", "", now)
	writeDebugBundleJSONFile(outDir, "savings-today.json", summary, manifest)
}

func writeDebugBundleDecisions(outDir string, cfg *config.Config, opts debugBundleOptions, manifest *debugBundleManifest) {
	path := filepath.Clean(config.ExpandHomePath(strings.TrimSpace(cfg.Debug.DecisionsLog)))
	if strings.TrimSpace(cfg.Debug.DecisionsLog) == "" {
		manifest.Missing = append(manifest.Missing, "decisions-log")
		writeDebugBundleJSONFile(outDir, "decisions-tail.json", []dbg.RequestSummary{}, manifest)
		writeDebugBundleJSONFile(outDir, "flight-tail.json", []dbg.FlightRequestSummary{}, manifest)
		writeDebugBundleWSSSockets(outDir, "", nil, opts, manifest)
		return
	}
	summaries := readLastDecisionSummaries(path, opts.FlightLimit)
	if len(summaries) == 0 {
		manifest.Missing = append(manifest.Missing, "decisions-log-rows")
	}
	flights := make([]dbg.FlightRequestSummary, 0, len(summaries))
	for _, summary := range summaries {
		summary.EnsureFlight()
		if summary.Flight != nil {
			flights = append(flights, *summary.Flight)
		}
	}
	writeDebugBundleJSONFile(outDir, "decisions-tail.json", summaries, manifest)
	writeDebugBundleJSONFile(outDir, "flight-tail.json", flights, manifest)
	writeDebugBundleWSSSockets(outDir, path, summaries, opts, manifest)
}

func writeDebugBundleWSSSockets(outDir, decisionLogPath string, summaries []dbg.RequestSummary, opts debugBundleOptions, manifest *debugBundleManifest) {
	report := buildWSSSocketReportWithOptions(decisionLogPath, summaries, wssSocketDebugArgs{Limit: opts.FlightLimit})
	writeDebugBundleJSONFile(outDir, "wss-sockets.json", report, manifest)
}

func writeDebugBundleFilterRuns(outDir string, opts debugBundleOptions, manifest *debugBundleManifest) {
	db, ok := openDebugBundleFilterDB(manifest)
	if !ok {
		writeDebugBundleJSONFile(outDir, "filter-tail.json", []debugBundleFilterRun{}, manifest)
		return
	}
	defer db.Close()
	runs, err := filter.RecentFilterRuns(db, opts.FilterLimit)
	if err != nil {
		manifest.Missing = append(manifest.Missing, "filter-runs")
		writeDebugBundleTextFile(outDir, "filter-tail-error.txt", err.Error()+"\n", manifest)
		return
	}
	out := make([]debugBundleFilterRun, 0, len(runs))
	for _, run := range runs {
		saved := max(run.InputTokens-run.OutputTokens, 0)
		out = append(out, debugBundleFilterRun{
			ID:           run.ID,
			CommandHash:  debugBundleHash(run.Command),
			ProjectHash:  debugBundleHash(run.ProjectPath),
			InputTokens:  run.InputTokens,
			OutputTokens: run.OutputTokens,
			SavedTokens:  saved,
			SavingsPct:   run.SavingsPct,
			CreatedAt:    run.CreatedAt,
		})
	}
	writeDebugBundleJSONFile(outDir, "filter-tail.json", out, manifest)
}

func openDebugBundleFilterDB(manifest *debugBundleManifest) (*sql.DB, bool) {
	path, err := resolveFilterDBPathFn()
	if err != nil {
		manifest.Missing = append(manifest.Missing, "filter-db")
		return nil, false
	}
	if _, err := os.Stat(path); err != nil {
		manifest.Missing = append(manifest.Missing, "filter-db")
		return nil, false
	}
	db, err := filter.OpenDB(path)
	if err != nil {
		manifest.Missing = append(manifest.Missing, "filter-db")
		return nil, false
	}
	return db, true
}

func writeDebugBundleDaemonLogs(outDir string, opts debugBundleOptions, manifest *debugBundleManifest) {
	writeDebugBundleLogTail(outDir, "daemon-stdout-tail.json", daemonStdoutLogPathFn(), opts.LogLines, manifest)
	writeDebugBundleLogTail(outDir, "daemon-stderr-tail.json", daemonStderrLogPathFn(), opts.LogLines, manifest)
}

func writeDebugBundleLogTail(outDir, name, path string, limit int, manifest *debugBundleManifest) {
	lines, err := daemonReadRecentLogLinesFn(path, limit, time.Time{})
	tail := debugBundleLogTail{Path: path, Lines: lines}
	if err != nil {
		tail.Error = err.Error()
		manifest.Missing = append(manifest.Missing, strings.TrimSuffix(name, ".json"))
	}
	writeDebugBundleJSONFile(outDir, name, tail, manifest)
}

func debugBundleHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func writeDebugBundleJSONFile(outDir, name string, value any, manifest *debugBundleManifest) {
	path := filepath.Join(outDir, name)
	if err := writeDebugBundleJSON(path, value); err != nil {
		manifest.Missing = append(manifest.Missing, name)
		return
	}
	manifest.Files[name] = path
}

func writeDebugBundleTextFile(outDir, name, text string, manifest *debugBundleManifest) {
	path := filepath.Join(outDir, name)
	if err := osWriteFile(path, []byte(text), 0o600); err != nil {
		manifest.Missing = append(manifest.Missing, name)
		return
	}
	manifest.Files[name] = path
}

func writeDebugBundleJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return osWriteFile(path, data, 0o600)
}

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type wssProofInventoryFlags struct {
	path         string
	outputFormat string
	help         bool
}

type wssProofInventoryReport struct {
	Path                    string           `json:"path"`
	MatrixFiles             int              `json:"matrix_files"`
	Rows                    int              `json:"rows"`
	Clients                 map[string]int   `json:"clients"`
	WorkloadClasses         map[string]int   `json:"workload_classes"`
	ExpectedReducers        map[string]int   `json:"expected_reducers"`
	LiveReducerHits         map[string]int64 `json:"live_reducer_hits"`
	PositiveTokenRows       int              `json:"positive_token_rows"`
	ExpectedZeroRows        int              `json:"expected_zero_rows"`
	HostBudgetOKRows        int              `json:"host_budget_ok_rows"`
	SafetyIssueRows         int              `json:"safety_issue_rows"`
	MissingReleaseWorkloads []string         `json:"missing_release_workloads,omitempty"`
	MissingMaxxWorkloads    []string         `json:"missing_maxx_workloads,omitempty"`
}

var maxxWSSProofWorkloads = []string{
	"chunk_dedup_similar_outputs",
	"chunk_dedup_log_output",
	"chunk_dedup_test_output",
	"output_reduce_aggressive",
	"tool_heavy",
	"provider_cache_long_session",
	"host_resource_long_workday",
}

var inventoryReducerNames = []string{
	"read_delta",
	"captured_output",
	"codex_exec_envelope",
	"repeated_output",
	"chunk_dedup",
	"chunk_dedup_refs",
	"tool_prune",
	"tool_prune_reattach",
	"tool_prune_retry",
	"output_reduce_injected",
	"output_reduce_skipped",
	"output_reduce_downgraded",
	"stop_seq",
	"streamcut",
	"repdet",
	"stale_read",
	"obsolete_prune",
	"beterse",
	"host_budget_ok",
}

const wssProofInventoryHelpText = `wss-proof-inventory: inventory local Codex WSS proof matrix rows

Usage:
  go run ./scripts/utils wss-proof-inventory <dir-or-matrix.jsonl> [--json]

The tool scans only proof-matrix JSONL rows, never raw WSS frame payloads. It is
for quickly seeing which live workload classes and reducer signals already
exist locally and which release/maxx proof classes are still missing.`

func runWSSProofInventory(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSProofInventoryFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssProofInventoryHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-proof-inventory <dir-or-matrix.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSProofInventory(flags.path)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if flags.outputFormat == outputJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	writeWSSProofInventoryText(stdout, report)
	return 0
}

func parseWSSProofInventoryFlags(args []string) (wssProofInventoryFlags, error) {
	flags := wssProofInventoryFlags{outputFormat: outputText}
	for _, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple inventory paths provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func loadWSSProofInventory(path string) (wssProofInventoryReport, error) {
	report := wssProofInventoryReport{
		Path:             path,
		Clients:          make(map[string]int),
		WorkloadClasses:  make(map[string]int),
		ExpectedReducers: make(map[string]int),
		LiveReducerHits:  make(map[string]int64),
	}
	files, err := wssProofInventoryFiles(path)
	if err != nil {
		return wssProofInventoryReport{}, err
	}
	for _, file := range files {
		rows, err := readWSSProofInventoryRows(file)
		if err != nil {
			return wssProofInventoryReport{}, err
		}
		if len(rows) == 0 {
			continue
		}
		report.MatrixFiles++
		for _, row := range rows {
			report.Rows++
			client := normalizeProofClient(row.Client)
			if client == "" {
				client = "unknown"
			}
			report.Clients[client]++
			if row.WorkloadClass != "" {
				report.WorkloadClasses[row.WorkloadClass]++
			}
			if row.ExpectedZeroSavings {
				report.ExpectedZeroRows++
			}
			for _, reducer := range row.ExpectedReducers {
				name := strings.TrimSpace(reducer)
				if name == "" {
					continue
				}
				report.ExpectedReducers[name]++
			}
			if row.LiveDelta == nil {
				continue
			}
			if row.LiveDelta.BillableInputTokensSaved > 0 {
				report.PositiveTokenRows++
			}
			if row.LiveDelta.ParseFailures+row.LiveDelta.DegradedSessions+row.LiveDelta.CompressionErrors > 0 {
				report.SafetyIssueRows++
			}
			for _, name := range inventoryReducerNames {
				count, ok := liveReducerCount(name, row.LiveDelta)
				if ok && count > 0 {
					report.LiveReducerHits[name] += count
				}
			}
			if count, ok := liveReducerCount("host_budget_ok", row.LiveDelta); ok && count > 0 {
				report.HostBudgetOKRows++
			}
		}
	}
	report.MissingReleaseWorkloads = missingWSSProofWorkloads(report.WorkloadClasses, requiredWSSProofWorkloads)
	report.MissingMaxxWorkloads = missingWSSProofWorkloads(report.WorkloadClasses, maxxWSSProofWorkloads)
	return report, nil
}

func wssProofInventoryFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat inventory path %s: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk inventory path %s: %w", path, err)
	}
	sort.Strings(files)
	return files, nil
}

func readWSSProofInventoryRows(path string) ([]wssProofMatrixRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open proof inventory %s: %w", path, err)
	}
	defer f.Close()
	var rows []wssProofMatrixRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row wssProofMatrixRecord
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if !looksLikeProofMatrixRow(row) {
			continue
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan proof inventory %s: %w", path, err)
	}
	return rows, nil
}

func looksLikeProofMatrixRow(row wssProofMatrixRecord) bool {
	return strings.TrimSpace(row.Client) != "" &&
		strings.TrimSpace(row.WorkloadClass) != "" &&
		strings.TrimSpace(row.FramesPath) != ""
}

func writeWSSProofInventoryText(w io.Writer, report wssProofInventoryReport) {
	fmt.Fprintln(w, "WSS proof inventory")
	fmt.Fprintf(w, "Path: %s\n", report.Path)
	fmt.Fprintf(w, "Matrix files: %d\n", report.MatrixFiles)
	fmt.Fprintf(w, "Rows: %d\n", report.Rows)
	fmt.Fprintf(w, "Positive token rows: %d\n", report.PositiveTokenRows)
	fmt.Fprintf(w, "Expected-zero rows: %d\n", report.ExpectedZeroRows)
	fmt.Fprintf(w, "Host-budget-ok rows: %d\n", report.HostBudgetOKRows)
	fmt.Fprintf(w, "Safety issue rows: %d\n", report.SafetyIssueRows)
	fmt.Fprintf(w, "Clients: %s\n", formatInventoryIntMap(report.Clients))
	fmt.Fprintf(w, "Workloads: %s\n", formatInventoryIntMap(report.WorkloadClasses))
	fmt.Fprintf(w, "Expected reducers: %s\n", formatInventoryIntMap(report.ExpectedReducers))
	fmt.Fprintf(w, "Live reducer hits: %s\n", formatInventoryInt64Map(report.LiveReducerHits))
	if len(report.MissingReleaseWorkloads) > 0 {
		fmt.Fprintf(w, "Missing release workloads: %s\n", strings.Join(report.MissingReleaseWorkloads, ", "))
	}
	if len(report.MissingMaxxWorkloads) > 0 {
		fmt.Fprintf(w, "Missing maxx workloads: %s\n", strings.Join(report.MissingMaxxWorkloads, ", "))
	}
}

func formatInventoryIntMap(values map[string]int) string {
	if len(values) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return strings.Join(parts, ", ")
}

func formatInventoryInt64Map(values map[string]int64) string {
	if len(values) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return strings.Join(parts, ", ")
}

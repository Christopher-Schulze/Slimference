package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type wssProofMatrixFlags struct {
	path                  string
	outputFormat          string
	requireLiveTokenDelta bool
	help                  bool
}

type wssProofMatrixOptions struct {
	requireLiveTokenDelta bool
}

type wssProofMatrixCapture struct {
	ID                  string                 `json:"id"`
	Client              string                 `json:"client"`
	WorkloadClass       string                 `json:"workload_class"`
	FramesPath          string                 `json:"frames_path"`
	DecisionsPath       string                 `json:"decisions_path,omitempty"`
	CodexVersion        string                 `json:"codex_version,omitempty"`
	SlimferenceCommit   string                 `json:"slimference_commit,omitempty"`
	Repo                string                 `json:"repo,omitempty"`
	Model               string                 `json:"model,omitempty"`
	StartedAt           string                 `json:"started_at,omitempty"`
	EndedAt             string                 `json:"ended_at,omitempty"`
	ExpectedReducers    []string               `json:"expected_reducers,omitempty"`
	ExpectedZeroSavings bool                   `json:"expected_zero_savings,omitempty"`
	ExpectedReducerHits map[string]int64       `json:"expected_reducer_hits,omitempty"`
	LiveDelta           *codexCaptureLiveDelta `json:"live_delta,omitempty"`
	Replay              wssABReplayReport      `json:"replay"`
	Audit               *wssAuditReport        `json:"audit,omitempty"`
	GatePassed          bool                   `json:"gate_passed"`
	GateFailures        []string               `json:"gate_failures,omitempty"`
}

type wssProofMatrixReport struct {
	Path                      string                  `json:"path"`
	Captures                  int                     `json:"captures"`
	CLI                       int                     `json:"cli"`
	Desktop                   int                     `json:"desktop"`
	PositiveSavings           int                     `json:"positive_savings_captures"`
	PositiveTokenSavings      int                     `json:"positive_token_savings_captures"`
	PositiveReplayByteSavings int                     `json:"positive_replay_byte_savings_captures"`
	ExpectedZero              int                     `json:"expected_zero_captures"`
	WorkloadClasses           map[string]int          `json:"workload_classes"`
	MissingWorkloads          []string                `json:"missing_workloads,omitempty"`
	CapturesWithIssues        int                     `json:"captures_with_issues"`
	GatePassed                bool                    `json:"gate_passed"`
	GateFailures              []string                `json:"gate_failures,omitempty"`
	CaptureReports            []wssProofMatrixCapture `json:"capture_reports"`
}

type wssProofMatrixRecord struct {
	ID                  string                 `json:"id"`
	Client              string                 `json:"client"`
	WorkloadClass       string                 `json:"workload_class"`
	FramesPath          string                 `json:"frames_path"`
	DecisionsPath       string                 `json:"decisions_path"`
	CodexVersion        string                 `json:"codex_version"`
	SlimferenceCommit   string                 `json:"slimference_commit"`
	Repo                string                 `json:"repo"`
	Model               string                 `json:"model"`
	StartedAt           string                 `json:"started_at"`
	EndedAt             string                 `json:"ended_at"`
	ExpectedReducers    []string               `json:"expected_reducers"`
	ExpectedZeroSavings bool                   `json:"expected_zero_savings"`
	LiveDelta           *codexCaptureLiveDelta `json:"live_delta,omitempty"`
}

var requiredWSSProofWorkloads = []string{
	"repeat_full_read",
	"similar_files",
	"changed_file",
	"ranged_read",
	"search_loop",
	"git_status_diff",
	"build_test_lint_failure",
	"apply_patch_then_read",
	"long_mixed_workday",
	"no_savings_control",
}

const wssProofMatrixHelpText = `wss-proof-matrix: verify the Codex WSS real-workload proof matrix

Usage:
  go run ./scripts/utils wss-proof-matrix <captures.jsonl> [--json] [--require-live-token-delta]

Input JSONL rows:
  {"id":"cli-repeat-1","client":"cli","workload_class":"repeat_full_read","frames_path":"/tmp/frames.jsonl","decisions_path":"~/.slimference/debug/decisions.jsonl","expected_reducers":["read_delta"]}

The tool replays each frames file with wss-ab-replay semantics, optionally audits
the matching decisions log, and emits a content-free PASS/FAIL matrix. Use
--require-live-token-delta for release proofs where replay bytes are not allowed
to stand in for real billable token deltas. Raw frame payloads stay local and are
not copied into the report.`

func runWSSProofMatrix(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSProofMatrixFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssProofMatrixHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-proof-matrix <captures.jsonl> [--json]")
		return 2
	}
	report, err := loadWSSProofMatrixReportWithOptions(flags.path, wssProofMatrixOptions{
		requireLiveTokenDelta: flags.requireLiveTokenDelta,
	})
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
		if !report.GatePassed {
			return 3
		}
		return 0
	}
	writeWSSProofMatrixText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseWSSProofMatrixFlags(args []string) (wssProofMatrixFlags, error) {
	flags := wssProofMatrixFlags{outputFormat: outputText}
	for _, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--require-live-token-delta":
			flags.requireLiveTokenDelta = true
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple proof matrix files provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func loadWSSProofMatrixReport(path string) (wssProofMatrixReport, error) {
	return loadWSSProofMatrixReportWithOptions(path, wssProofMatrixOptions{})
}

func loadWSSProofMatrixReportWithOptions(path string, options wssProofMatrixOptions) (wssProofMatrixReport, error) {
	records, err := readWSSProofMatrixRecords(path)
	if err != nil {
		return wssProofMatrixReport{}, err
	}
	report := wssProofMatrixReport{
		Path:            path,
		WorkloadClasses: make(map[string]int),
		GatePassed:      true,
	}
	baseDir := filepath.Dir(path)
	for _, record := range records {
		capture := wssProofMatrixCapture{
			ID:                  record.ID,
			Client:              normalizeProofClient(record.Client),
			WorkloadClass:       strings.TrimSpace(record.WorkloadClass),
			FramesPath:          resolveProofPath(baseDir, record.FramesPath),
			DecisionsPath:       resolveProofPath(baseDir, record.DecisionsPath),
			CodexVersion:        record.CodexVersion,
			SlimferenceCommit:   record.SlimferenceCommit,
			Repo:                record.Repo,
			Model:               record.Model,
			StartedAt:           record.StartedAt,
			EndedAt:             record.EndedAt,
			ExpectedReducers:    append([]string(nil), record.ExpectedReducers...),
			ExpectedZeroSavings: record.ExpectedZeroSavings,
			LiveDelta:           record.LiveDelta,
			GatePassed:          true,
		}
		if capture.ID == "" {
			capture.ID = fmt.Sprintf("capture-%02d", len(report.CaptureReports)+1)
		}
		capture.GateFailures = validateWSSProofMetadata(capture)

		replay, err := loadWSSABReplayReport(wssABReplayFlags{path: capture.FramesPath, failOnLost: true})
		if err != nil {
			capture.GateFailures = append(capture.GateFailures, fmt.Sprintf("replay failed: %v", err))
		} else {
			capture.Replay = replay
			if !replay.GatePassed {
				capture.GateFailures = append(capture.GateFailures, replay.GateFailures...)
			}
			if replay.BytesSaved > 0 {
				report.PositiveReplayByteSavings++
			}
		}
		if capture.LiveDelta != nil {
			tokenPositive := capture.LiveDelta.BillableInputTokensSaved > 0
			if tokenPositive {
				report.PositiveTokenSavings++
				report.PositiveSavings++
			}
			if capture.ExpectedZeroSavings && tokenPositive {
				capture.GateFailures = append(capture.GateFailures, "expected zero savings, got positive live billable_input_tokens_saved")
			}
			if !capture.ExpectedZeroSavings && !tokenPositive {
				capture.GateFailures = append(capture.GateFailures, "expected positive live billable_input_tokens_saved, got <=0")
			}
			if safety := capture.LiveDelta.ParseFailures + capture.LiveDelta.DegradedSessions + capture.LiveDelta.CompressionErrors; safety > 0 {
				capture.GateFailures = append(capture.GateFailures,
					fmt.Sprintf("live safety counters non-zero: parse=%d degraded=%d compression_errors=%d",
						capture.LiveDelta.ParseFailures, capture.LiveDelta.DegradedSessions, capture.LiveDelta.CompressionErrors))
			}
			hits, failures := validateExpectedReducers(capture.ExpectedReducers, capture.LiveDelta)
			capture.ExpectedReducerHits = hits
			capture.GateFailures = append(capture.GateFailures, failures...)
		} else if options.requireLiveTokenDelta {
			capture.GateFailures = append(capture.GateFailures, "live_delta is required in --require-live-token-delta mode")
		} else if capture.Replay.Path != "" {
			if !capture.ExpectedZeroSavings && capture.Replay.BytesSaved <= 0 {
				capture.GateFailures = append(capture.GateFailures, "expected positive savings, no live token delta and replay bytes_saved<=0")
			}
			if capture.Replay.BytesSaved > 0 {
				report.PositiveSavings++
			}
		}
		if capture.DecisionsPath != "" {
			audit, err := loadWSSAuditReport(wssAuditFlags{path: capture.DecisionsPath, minPhaseF: 1})
			if err != nil {
				capture.GateFailures = append(capture.GateFailures, fmt.Sprintf("audit failed: %v", err))
			} else {
				capture.Audit = &audit
				if !audit.GatePassed {
					capture.GateFailures = append(capture.GateFailures, audit.GateFailures...)
				}
			}
		}
		capture.GatePassed = len(capture.GateFailures) == 0
		if !capture.GatePassed {
			report.CapturesWithIssues++
		}
		report.Captures++
		switch capture.Client {
		case "cli":
			report.CLI++
		case "desktop":
			report.Desktop++
		}
		if capture.ExpectedZeroSavings {
			report.ExpectedZero++
		}
		if capture.WorkloadClass != "" {
			report.WorkloadClasses[capture.WorkloadClass]++
		}
		report.CaptureReports = append(report.CaptureReports, capture)
	}
	report.MissingWorkloads = missingWSSProofWorkloads(report.WorkloadClasses)
	report.GateFailures = wssProofMatrixGateFailures(report)
	report.GatePassed = len(report.GateFailures) == 0
	return report, nil
}

func readWSSProofMatrixRecords(path string) ([]wssProofMatrixRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open proof matrix %s: %w", path, err)
	}
	defer f.Close()
	var records []wssProofMatrixRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record wssProofMatrixRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("%s:%d: decode proof row: %w", path, lineNo, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan proof matrix %s: %w", path, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("proof matrix %s contained no captures", path)
	}
	return records, nil
}

func validateWSSProofMetadata(capture wssProofMatrixCapture) []string {
	var failures []string
	if capture.Client != "cli" && capture.Client != "desktop" {
		failures = append(failures, "client must be cli or desktop")
	}
	if capture.WorkloadClass == "" {
		failures = append(failures, "workload_class is required")
	}
	if capture.FramesPath == "" {
		failures = append(failures, "frames_path is required")
	}
	return failures
}

func validateExpectedReducers(expected []string, live *codexCaptureLiveDelta) (map[string]int64, []string) {
	if len(expected) == 0 {
		return nil, nil
	}
	hits := make(map[string]int64, len(expected))
	var failures []string
	for _, raw := range expected {
		name := strings.TrimSpace(raw)
		if name == "" || name == "none" {
			continue
		}
		count, ok := liveReducerCount(name, live)
		if !ok {
			failures = append(failures, "unknown expected reducer: "+name)
			continue
		}
		hits[name] = count
		if count <= 0 {
			failures = append(failures, fmt.Sprintf("expected reducer %s did not fire in live delta", name))
		}
	}
	if len(hits) == 0 {
		return nil, failures
	}
	return hits, failures
}

func liveReducerCount(name string, live *codexCaptureLiveDelta) (int64, bool) {
	if live == nil {
		return 0, false
	}
	switch name {
	case "read_delta":
		return live.ProxyLayer0ReadDelta, true
	case "captured_output":
		return live.ProxyLayer0Captured, true
	case "codex_exec_envelope":
		return live.ProxyLayer0Envelope, true
	case "repeated_output":
		return live.ProxyLayer0Repeated, true
	case "chunk_dedup":
		return live.ProxyLayer0ChunkDedup, true
	default:
		return 0, false
	}
}

func wssProofMatrixGateFailures(report wssProofMatrixReport) []string {
	var failures []string
	if report.Captures < 10 {
		failures = append(failures, fmt.Sprintf("expected at least 10 captures, got %d", report.Captures))
	}
	if report.CLI < 5 {
		failures = append(failures, fmt.Sprintf("expected at least 5 CLI captures, got %d", report.CLI))
	}
	if report.Desktop < 5 {
		failures = append(failures, fmt.Sprintf("expected at least 5 Desktop captures, got %d", report.Desktop))
	}
	if len(report.MissingWorkloads) > 0 {
		failures = append(failures, "missing workload classes: "+strings.Join(report.MissingWorkloads, ", "))
	}
	if report.PositiveSavings+report.ExpectedZero < 7 {
		failures = append(failures, fmt.Sprintf("expected at least 7 positive-token-savings or expected-zero captures, got %d", report.PositiveSavings+report.ExpectedZero))
	}
	if report.CapturesWithIssues > 0 {
		failures = append(failures, fmt.Sprintf("%d capture(s) failed per-capture gates", report.CapturesWithIssues))
	}
	return failures
}

func missingWSSProofWorkloads(classes map[string]int) []string {
	var missing []string
	for _, class := range requiredWSSProofWorkloads {
		if classes[class] == 0 {
			missing = append(missing, class)
		}
	}
	return missing
}

func normalizeProofClient(client string) string {
	return strings.ToLower(strings.TrimSpace(client))
}

func resolveProofPath(baseDir, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func writeWSSProofMatrixText(w io.Writer, report wssProofMatrixReport) {
	fmt.Fprintf(w, "WSS proof matrix: %s\n", report.Path)
	fmt.Fprintf(w, "  captures:          %d\n", report.Captures)
	fmt.Fprintf(w, "  cli/desktop:       %d / %d\n", report.CLI, report.Desktop)
	fmt.Fprintf(w, "  positive/zero:     %d / %d\n", report.PositiveSavings, report.ExpectedZero)
	fmt.Fprintf(w, "  token/replay+:     %d / %d\n", report.PositiveTokenSavings, report.PositiveReplayByteSavings)
	fmt.Fprintf(w, "  capture issues:    %d\n", report.CapturesWithIssues)
	fmt.Fprintf(w, "  gate:              %s\n", passFail(report.GatePassed))
	if len(report.WorkloadClasses) > 0 {
		fmt.Fprintln(w, "\nWorkloads:")
		for _, key := range sortedStringKeys(report.WorkloadClasses) {
			fmt.Fprintf(w, "  %-28s %d\n", key, report.WorkloadClasses[key])
		}
	}
	if len(report.CaptureReports) > 0 {
		fmt.Fprintln(w, "\nCaptures:")
		for _, capture := range report.CaptureReports {
			status := passFail(capture.GatePassed)
			tokens := int64(0)
			if capture.LiveDelta != nil {
				tokens = capture.LiveDelta.BillableInputTokensSaved
			}
			fmt.Fprintf(w, "  %-24s %-7s %-24s billable_tokens=%d replay_bytes=%d mutated=%d gate=%s\n",
				capture.ID, capture.Client, capture.WorkloadClass,
				tokens, capture.Replay.BytesSaved, capture.Replay.MutatedRequests, status)
			for _, failure := range capture.GateFailures {
				fmt.Fprintf(w, "    - %s\n", failure)
			}
		}
	}
	if len(report.GateFailures) > 0 {
		fmt.Fprintln(w, "\nGate failures:")
		for _, failure := range report.GateFailures {
			fmt.Fprintf(w, "  - %s\n", failure)
		}
	}
}

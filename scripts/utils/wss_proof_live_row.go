package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type wssProofLiveRowFlags struct {
	matrixPath        string
	framesPath        string
	id                string
	client            string
	workloadClass     string
	codexVersion      string
	slimferenceCommit string
	repo              string
	model             string
	abPairID          string
	abVariant         string
	host              string
	port              string
	expectedReducers  []string
	jsonOut           bool
	help              bool
}

type wssProofLiveRowReport struct {
	MatrixPath string                 `json:"matrix_path"`
	FramesPath string                 `json:"frames_path"`
	LiveDelta  *codexCaptureLiveDelta `json:"live_delta"`
}

const wssProofLiveRowHelpText = `wss-proof-live-row: append current content-free admin counters as a WSS proof matrix row

Usage:
  go run ./scripts/utils wss-proof-live-row --matrix-row PATH --frames PATH --workload-class CLASS [flags]

Flags:
  --id VALUE                  Matrix row id
  --client cli|desktop        Matrix row client (default: desktop)
  --host HOST                 Daemon host (default: 127.0.0.1)
  --port PORT                 Daemon port (default: 8990)
  --expected-reducer NAME     Required live signal, repeatable
  --codex-version VALUE       Matrix row Codex version
  --slimference-commit VALUE  Matrix row Slimference commit
  --repo VALUE                Repository label
  --model VALUE               Model label
  --ab-pair-id VALUE          Optional A/B pair id for output-reduce proofs
  --ab-variant VALUE          Optional A/B variant: baseline or directive
  --json                      Print JSON report

This tool is for interactive Desktop proofs where codex-capture-run cannot own
the Codex process. It reads /_slimference/admin/state plus /status, writes only
content-free counters, and never copies raw frames into the matrix row.`

func runWSSProofLiveRow(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSProofLiveRowFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssProofLiveRowHelpText)
		return 0
	}
	if err := validateWSSProofLiveRowFlags(flags); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	snapshot, err := loadCodexCaptureAdminSnapshot(context.Background(), codexCaptureRunFlags{
		host: flags.host,
		port: flags.port,
	})
	if err != nil {
		fmt.Fprintf(stderr, "read live admin counters: %v\n", err)
		return 1
	}
	live := deltaCodexCaptureAdminSnapshot(codexCaptureAdminSnapshot{}, snapshot)
	if failures := validateCodexCaptureExpectedReducers(flags.expectedReducers, live); len(failures) > 0 {
		fmt.Fprintf(stderr, "validate expected reducers: %s\n", strings.Join(failures, "; "))
		return 3
	}
	result := codexCaptureRunResult{
		CapturePath: flags.framesPath,
		LiveDelta:   live,
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
		EndedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	captureFlags := codexCaptureRunFlags{
		matrixPath:        flags.matrixPath,
		id:                flags.id,
		client:            flags.client,
		workloadClass:     flags.workloadClass,
		codexVersion:      flags.codexVersion,
		slimferenceCommit: flags.slimferenceCommit,
		repo:              flags.repo,
		model:             flags.model,
		abPairID:          flags.abPairID,
		abVariant:         flags.abVariant,
		expectedReducers:  append([]string(nil), flags.expectedReducers...),
	}
	if err := appendCodexCaptureMatrixRow(captureFlags, result); err != nil {
		fmt.Fprintf(stderr, "append matrix row: %v\n", err)
		return 1
	}
	report := wssProofLiveRowReport{MatrixPath: flags.matrixPath, FramesPath: flags.framesPath, LiveDelta: live}
	if flags.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(stderr, "encode report: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "wss-proof-live-row: appended %s (%s) to %s\n", flags.id, flags.workloadClass, flags.matrixPath)
	fmt.Fprintf(stdout, "  tool_prune_pruned:       %d\n", live.ToolPrunePruned)
	fmt.Fprintf(stdout, "  tool_prune_tokens_saved: %d\n", live.ToolPruneTokensSaved)
	fmt.Fprintf(stdout, "  host_budget:             %s exceeded=%v\n", live.HostBudgetStatus, live.HostBudgetExceeded)
	return 0
}

func parseWSSProofLiveRowFlags(args []string) (wssProofLiveRowFlags, error) {
	flags := wssProofLiveRowFlags{
		client: "desktop",
		host:   "127.0.0.1",
		port:   "8990",
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.jsonOut = true
		case arg == "--matrix-row", arg == "--frames", arg == "--id", arg == "--client",
			arg == "--workload-class", arg == "--host", arg == "--port", arg == "--expected-reducer",
			arg == "--codex-version", arg == "--slimference-commit", arg == "--repo", arg == "--model",
			arg == "--ab-pair-id", arg == "--ab-variant":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("%s requires a value", arg)
			}
			i++
			if err := setWSSProofLiveRowFlag(&flags, arg, args[i]); err != nil {
				return flags, err
			}
		case strings.HasPrefix(arg, "--"):
			name, value, ok := strings.Cut(arg, "=")
			if !ok {
				return flags, fmt.Errorf("unknown flag: %s", arg)
			}
			if err := setWSSProofLiveRowFlag(&flags, name, value); err != nil {
				return flags, err
			}
		default:
			return flags, fmt.Errorf("unexpected argument: %s", arg)
		}
	}
	var err error
	flags.matrixPath, err = expandCodexCapturePath(flags.matrixPath)
	if err != nil {
		return flags, err
	}
	flags.framesPath, err = expandCodexCapturePath(flags.framesPath)
	if err != nil {
		return flags, err
	}
	return flags, nil
}

func setWSSProofLiveRowFlag(flags *wssProofLiveRowFlags, name, value string) error {
	value = strings.TrimSpace(value)
	switch name {
	case "--matrix-row":
		flags.matrixPath = value
	case "--frames":
		flags.framesPath = value
	case "--id":
		flags.id = value
	case "--client":
		flags.client = strings.ToLower(value)
	case "--workload-class":
		flags.workloadClass = value
	case "--host":
		flags.host = value
	case "--port":
		flags.port = value
	case "--expected-reducer":
		if value != "" {
			flags.expectedReducers = append(flags.expectedReducers, value)
		}
	case "--codex-version":
		flags.codexVersion = value
	case "--slimference-commit":
		flags.slimferenceCommit = value
	case "--repo":
		flags.repo = value
	case "--model":
		flags.model = value
	case "--ab-pair-id":
		flags.abPairID = value
	case "--ab-variant":
		flags.abVariant = strings.ToLower(value)
	default:
		return fmt.Errorf("unknown flag: %s", name)
	}
	return nil
}

func validateWSSProofLiveRowFlags(flags wssProofLiveRowFlags) error {
	switch {
	case flags.matrixPath == "":
		return fmt.Errorf("--matrix-row is required")
	case flags.framesPath == "":
		return fmt.Errorf("--frames is required")
	case flags.workloadClass == "":
		return fmt.Errorf("--workload-class is required")
	case flags.client != "cli" && flags.client != "desktop":
		return fmt.Errorf("--client must be cli or desktop")
	}
	if err := validateABProofFlags(flags.abPairID, flags.abVariant); err != nil {
		return err
	}
	if _, err := os.Stat(flags.framesPath); err != nil {
		return fmt.Errorf("frames path %s: %w", flags.framesPath, err)
	}
	return nil
}

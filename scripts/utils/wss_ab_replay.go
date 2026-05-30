package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/abharness"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
)

type wssABReplayFlags struct {
	path                   string
	outputFormat           string
	failOnLost             bool
	archiveRecoveryNote    bool
	allowRecoveryNoteExtra bool
	codexChunkDedup        bool
	chunkDedupMinBytes     int
	help                   bool
}

type wssABReplayReport struct {
	Path            string              `json:"path"`
	Frames          int                 `json:"frames"`
	RequestTurns    int                 `json:"request_turns"`
	MutatedRequests int                 `json:"mutated_requests"`
	BytesBefore     int                 `json:"bytes_before"`
	BytesAfter      int                 `json:"bytes_after"`
	BytesSaved      int                 `json:"bytes_saved"`
	Lost            int                 `json:"lost"`
	ExpectedExtras  int                 `json:"expected_extras,omitempty"`
	Elisions        []abharness.Elision `json:"elisions,omitempty"`
	GatePassed      bool                `json:"gate_passed"`
	GateFailures    []string            `json:"gate_failures,omitempty"`
	Notes           []string            `json:"notes,omitempty"`
}

const wssABReplayHelpText = `wss-ab-replay: run Codex WSS frames through the Phase-F comprehension A/B harness

Usage:
  go run ./scripts/utils wss-ab-replay <frames.jsonl> [flags]

Flags:
  --json                   Output JSON
  --fail-on-lost            Exit 3 if the replay reports lost comprehension
  --archive-recovery-note   Enable the default-off recovery note during replay
  --allow-recovery-note-extra
                           Do not fail the gate for the expected once-per-session
                           recovery-note extra block
  --codex-chunk-dedup       Enable default-off Codex content-defined chunk dedup
                           during replay; implies --archive-recovery-note and
                           --allow-recovery-note-extra
  --chunk-dedup-min-bytes N Set the replay chunk-dedup minimum input bytes

Input format: JSONL records with direction and payload:
  {"direction":"client_to_server","payload":{"model":"gpt-5-codex","input":[]}}
  {"direction":"server_to_client","payload":"{\"type\":\"response.output_item.done\"}"}`

func runWSSABReplay(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSABReplayFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssABReplayHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-ab-replay <frames.jsonl> [--json|--fail-on-lost|--archive-recovery-note|--codex-chunk-dedup]")
		return 2
	}
	report, err := loadWSSABReplayReport(flags)
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
	writeWSSABReplayText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseWSSABReplayFlags(args []string) (wssABReplayFlags, error) {
	flags := wssABReplayFlags{outputFormat: outputText, chunkDedupMinBytes: -1}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--fail-on-lost":
			flags.failOnLost = true
		case arg == "--archive-recovery-note":
			flags.archiveRecoveryNote = true
		case arg == "--allow-recovery-note-extra":
			flags.allowRecoveryNoteExtra = true
		case arg == "--codex-chunk-dedup":
			flags.codexChunkDedup = true
			flags.archiveRecoveryNote = true
			flags.allowRecoveryNoteExtra = true
		case arg == "--chunk-dedup-min-bytes":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("--chunk-dedup-min-bytes requires a value")
			}
			i++
			n, err := parseNonNegativeIntFlag("--chunk-dedup-min-bytes", args[i])
			if err != nil {
				return flags, err
			}
			flags.chunkDedupMinBytes = n
		case strings.HasPrefix(arg, "--chunk-dedup-min-bytes="):
			n, err := parseNonNegativeIntFlag("--chunk-dedup-min-bytes", strings.TrimPrefix(arg, "--chunk-dedup-min-bytes="))
			if err != nil {
				return flags, err
			}
			flags.chunkDedupMinBytes = n
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple replay files provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func loadWSSABReplayReport(flags wssABReplayFlags) (wssABReplayReport, error) {
	frames, err := readWSSABReplayFrames(flags.path)
	if err != nil {
		return wssABReplayReport{}, err
	}
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = flags.archiveRecoveryNote
	if flags.codexChunkDedup {
		cfg.Compression.OutputReduce.CodexChunkDedupEnabled = true
		if flags.chunkDedupMinBytes >= 0 {
			cfg.Compression.OutputReduce.CodexChunkDedupMinBytes = flags.chunkDedupMinBytes
		}
	}
	result, err := proxy.RunWSSPhaseFABReplay(cfg, frames)
	if err != nil {
		return wssABReplayReport{}, fmt.Errorf("run WSS A/B replay: %w", err)
	}
	report := wssABReplayReport{
		Path:            flags.path,
		Frames:          len(frames),
		RequestTurns:    result.RequestTurns,
		MutatedRequests: result.MutatedRequests,
		BytesBefore:     result.Report.BytesBefore,
		BytesAfter:      result.Report.BytesAfter,
		BytesSaved:      result.Report.Saved(),
		Lost:            result.Report.Lost(),
		Elisions:        result.Report.Elisions,
		GatePassed:      true,
	}
	if flags.archiveRecoveryNote {
		report.Notes = append(report.Notes, "archive recovery note was enabled for this replay; treat extra model-facing blocks as expected audit findings, not a default-on proof")
	}
	if flags.codexChunkDedup {
		report.Notes = append(report.Notes, "Codex chunk dedup was enabled for this replay; this is a proof-gated default-off path")
	}
	report.ExpectedExtras = expectedRecoveryNoteExtras(report.Elisions)
	gateLost := report.Lost
	if flags.allowRecoveryNoteExtra {
		gateLost -= report.ExpectedExtras
		if gateLost < 0 {
			gateLost = 0
		}
	}
	if flags.failOnLost && gateLost > 0 {
		report.GatePassed = false
		report.GateFailures = append(report.GateFailures, fmt.Sprintf("lost=%d > 0", gateLost))
	}
	return report, nil
}

func parseNonNegativeIntFlag(name, raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if n < 0 {
		return 0, fmt.Errorf("%s must be >= 0", name)
	}
	return n, nil
}

func expectedRecoveryNoteExtras(elisions []abharness.Elision) int {
	shiftedPreviews := map[string]struct{}{}
	for _, elision := range elisions {
		if elision.Severity == abharness.SeverityReferenced {
			shiftedPreviews[elision.Preview] = struct{}{}
		}
	}
	n := 0
	for _, elision := range elisions {
		if elision.Severity != abharness.SeverityExtra {
			continue
		}
		if strings.Contains(elision.Preview, "local-archive://<id>") {
			n++
			continue
		}
		if _, shifted := shiftedPreviews[elision.Preview]; shifted {
			n++
		}
	}
	return n
}

func readWSSABReplayFrames(path string) ([]proxy.WSSABReplayFrame, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open replay %s: %w", path, err)
	}
	defer f.Close()

	var frames []proxy.WSSABReplayFrame
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		frame, err := parseWSSABReplayFrameLine([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		frames = append(frames, frame)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan replay %s: %w", path, err)
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("replay %s contained no frames", path)
	}
	return frames, nil
}

func parseWSSABReplayFrameLine(line []byte) (proxy.WSSABReplayFrame, error) {
	var rec struct {
		Direction string          `json:"direction"`
		Dir       string          `json:"dir"`
		Payload   json.RawMessage `json:"payload"`
		Frame     json.RawMessage `json:"frame"`
	}
	if err := json.Unmarshal(line, &rec); err != nil {
		return proxy.WSSABReplayFrame{}, fmt.Errorf("decode replay record: %w", err)
	}
	direction, ok := parseWSSABReplayDirection(firstNonEmptyString(rec.Direction, rec.Dir))
	if !ok {
		return proxy.WSSABReplayFrame{}, fmt.Errorf("direction must be client_to_server or server_to_client")
	}
	payload := rec.Payload
	if len(payload) == 0 {
		payload = rec.Frame
	}
	body, err := normalizeWSSABReplayPayload(payload)
	if err != nil {
		return proxy.WSSABReplayFrame{}, err
	}
	return proxy.WSSABReplayFrame{Direction: direction, Payload: body}, nil
}

func parseWSSABReplayDirection(raw string) (wsmitm.Direction, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "client_to_server", "client", "request", string(wsmitm.DirClientToServer):
		return wsmitm.DirClientToServer, true
	case "server_to_client", "server", "response", string(wsmitm.DirServerToClient):
		return wsmitm.DirServerToClient, true
	default:
		return "", false
	}
}

func normalizeWSSABReplayPayload(raw json.RawMessage) ([]byte, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("payload is required")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("decode payload string: %w", err)
		}
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("payload string is empty")
		}
		return []byte(s), nil
	}
	if !json.Valid(raw) || raw[0] != '{' {
		return nil, fmt.Errorf("payload must be valid JSON object or JSON string")
	}
	return append([]byte(nil), raw...), nil
}

func writeWSSABReplayText(w io.Writer, report wssABReplayReport) {
	fmt.Fprintf(w, "WSS A/B replay: %s\n", report.Path)
	fmt.Fprintf(w, "  frames:           %d\n", report.Frames)
	fmt.Fprintf(w, "  request_turns:    %d\n", report.RequestTurns)
	fmt.Fprintf(w, "  mutated_requests: %d\n", report.MutatedRequests)
	fmt.Fprintf(w, "  bytes_before:     %d\n", report.BytesBefore)
	fmt.Fprintf(w, "  bytes_after:      %d\n", report.BytesAfter)
	fmt.Fprintf(w, "  bytes_saved:      %d\n", report.BytesSaved)
	fmt.Fprintf(w, "  lost:             %d\n", report.Lost)
	if report.ExpectedExtras > 0 {
		fmt.Fprintf(w, "  expected_extras:  %d\n", report.ExpectedExtras)
	}
	fmt.Fprintf(w, "  gate:             %s\n", passFail(report.GatePassed))
	if len(report.GateFailures) > 0 {
		fmt.Fprintln(w, "  gate_failures:")
		for _, failure := range report.GateFailures {
			fmt.Fprintf(w, "    - %s\n", failure)
		}
	}
	if len(report.Elisions) > 0 {
		fmt.Fprintln(w, "  elisions:")
		for _, elision := range report.Elisions {
			fmt.Fprintf(w, "    - turn=%d block=%d severity=%s bytes=%d preview=%q\n",
				elision.Turn, elision.Block, elision.Severity, elision.Bytes, elision.Preview)
		}
	}
	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "  notes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "    - %s\n", note)
		}
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

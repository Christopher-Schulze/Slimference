package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/slimference/slimference/internal/abharness"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
)

type wssABReplayFlags struct {
	path                string
	outputFormat        string
	failOnLost          bool
	archiveRecoveryNote bool
	help                bool
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
		fmt.Fprintln(stderr, "Usage: wss-ab-replay <frames.jsonl> [--json|--fail-on-lost|--archive-recovery-note]")
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
	flags := wssABReplayFlags{outputFormat: outputText}
	for _, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--fail-on-lost":
			flags.failOnLost = true
		case arg == "--archive-recovery-note":
			flags.archiveRecoveryNote = true
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
	if flags.failOnLost && report.Lost > 0 {
		report.GatePassed = false
		report.GateFailures = append(report.GateFailures, fmt.Sprintf("lost=%d > 0", report.Lost))
	}
	return report, nil
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

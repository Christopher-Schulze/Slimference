package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/abharness"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

type wssABReplayFlags struct {
	path                       string
	outputFormat               string
	failOnLost                 bool
	failOnUpstreamError        bool
	archiveRecoveryNote        bool
	allowRecoveryNoteExtra     bool
	toolOutputMutation         bool
	deltaToolOutputMutationLab bool
	codexChunkDedup            bool
	chunkDedupMinBytes         int
	help                       bool
}

type wssABReplayReport struct {
	Path                       string              `json:"path"`
	Frames                     int                 `json:"frames"`
	RequestTurns               int                 `json:"request_turns"`
	MutatedRequests            int                 `json:"mutated_requests"`
	CapturedMutatedRequests    int                 `json:"captured_mutated_requests,omitempty"`
	RequestShapes              replayShapeCounts   `json:"request_shapes"`
	MutatedShapes              replayShapeCounts   `json:"mutated_shapes"`
	CapturedMutatedShapes      replayShapeCounts   `json:"captured_mutated_shapes,omitempty"`
	BytesBefore                int                 `json:"bytes_before"`
	BytesAfter                 int                 `json:"bytes_after"`
	BytesSaved                 int                 `json:"bytes_saved"`
	ReducerTokensSaved         int                 `json:"reducer_tokens_saved"`
	ReducerBlocksModified      int                 `json:"reducer_blocks_modified"`
	ReducerReadDeltaBlocks     int                 `json:"reducer_read_delta_blocks"`
	ReducerRepeatedBlocks      int                 `json:"reducer_repeated_output_blocks"`
	ReducerChunkBlocks         int                 `json:"reducer_chunk_dedup_blocks"`
	ReducerCapturedBlocks      int                 `json:"reducer_captured_output_blocks"`
	ReducerEnvelopeBlocks      int                 `json:"reducer_codex_envelope_blocks"`
	ReducerChunkRefs           int                 `json:"reducer_chunk_dedup_references"`
	ReducerChunkRefBytes       int                 `json:"reducer_chunk_dedup_referenced_bytes"`
	ReducerChunkInputBytes     int                 `json:"reducer_chunk_dedup_input_bytes"`
	UpstreamErrorFrames        int                 `json:"upstream_error_frames"`
	UpstreamHTTP400Errors      int                 `json:"upstream_http_400_errors"`
	UpstreamInvalidRequests    int                 `json:"upstream_invalid_request_errors"`
	UpstreamResponseFailures   int                 `json:"upstream_response_failed_frames"`
	SearchRequestTurns         int                 `json:"search_request_turns"`
	SearchMutatedRequests      int                 `json:"search_mutated_requests"`
	SearchCapturedMutated      int                 `json:"search_captured_mutated_requests,omitempty"`
	SearchUpstreamErrors       int                 `json:"search_upstream_error_frames"`
	SearchHTTP400Errors        int                 `json:"search_http_400_errors"`
	SearchInvalidRequests      int                 `json:"search_invalid_request_errors"`
	SearchResponseFailures     int                 `json:"search_response_failed_frames"`
	ToolOutputMutation         bool                `json:"tool_output_mutation_enabled"`
	DeltaToolOutputMutationLab bool                `json:"delta_tool_output_mutation_lab_enabled,omitempty"`
	Lost                       int                 `json:"lost"`
	ExpectedExtras             int                 `json:"expected_extras,omitempty"`
	Elisions                   []abharness.Elision `json:"elisions,omitempty"`
	GatePassed                 bool                `json:"gate_passed"`
	GateFailures               []string            `json:"gate_failures,omitempty"`
	Notes                      []string            `json:"notes,omitempty"`
}

type replayShapeCounts struct {
	Root        int `json:"root"`
	Delta       int `json:"delta"`
	FullHistory int `json:"full_history"`
}

const wssABReplayHelpText = `wss-ab-replay: run Codex WSS frames through the Phase-F comprehension A/B harness

Usage:
  go run ./scripts/utils wss-ab-replay <frames.jsonl> [flags]

Flags:
  --json                   Output JSON
  --fail-on-lost            Exit 3 if the replay reports lost comprehension
  --fail-on-upstream-error  Exit 3 if the replay observed upstream error or
                           response.failed frames
  --archive-recovery-note   Enable the default-off recovery note during replay
  --allow-recovery-note-extra
                           Do not fail the gate for the expected once-per-session
                           recovery-note extra block
  --tool-output-mutation    Enable broader lab/proof Codex WSS tool-output
                           mutation during replay; product default still
                           allows safe read-delta savings and keeps unknown
                           or unsafe stateful WSS tool-output bodies byte-equal
  --delta-tool-output-mutation-lab
                           Also bypass the previous_response_id delta mutation
                           proof gate; only for reproducing known T354 400s
  --codex-chunk-dedup       Force Codex content-defined chunk dedup during replay;
                           useful for threshold experiments and implies
                           --archive-recovery-note,
                           --allow-recovery-note-extra, and
                           --tool-output-mutation
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
		fmt.Fprintln(stderr, "Usage: wss-ab-replay <frames.jsonl> [--json|--fail-on-lost|--fail-on-upstream-error|--archive-recovery-note|--tool-output-mutation|--delta-tool-output-mutation-lab|--codex-chunk-dedup]")
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
		case arg == "--fail-on-upstream-error":
			flags.failOnUpstreamError = true
		case arg == "--archive-recovery-note":
			flags.archiveRecoveryNote = true
		case arg == "--allow-recovery-note-extra":
			flags.allowRecoveryNoteExtra = true
		case arg == "--tool-output-mutation" || arg == "--codex-wss-tool-output-mutation":
			flags.toolOutputMutation = true
		case arg == "--delta-tool-output-mutation-lab":
			flags.deltaToolOutputMutationLab = true
			flags.toolOutputMutation = true
		case arg == "--codex-chunk-dedup":
			flags.codexChunkDedup = true
			flags.archiveRecoveryNote = true
			flags.allowRecoveryNoteExtra = true
			flags.toolOutputMutation = true
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
	upstream := wssABReplayUpstreamDiagnostics(frames)
	cfg := config.Defaults()
	toolOutputMutation := flags.toolOutputMutation || flags.codexChunkDedup
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = flags.archiveRecoveryNote
	cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = toolOutputMutation
	cfg.Compression.OutputReduce.CodexWSSDeltaToolOutputMutationLabEnabled = flags.deltaToolOutputMutationLab
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
		Path:                       flags.path,
		Frames:                     len(frames),
		RequestTurns:               result.RequestTurns,
		MutatedRequests:            result.MutatedRequests,
		CapturedMutatedRequests:    result.CapturedMutatedRequests,
		RequestShapes:              replayShapeCountsFromProxy(result.RequestShapes),
		MutatedShapes:              replayShapeCountsFromProxy(result.MutatedShapes),
		CapturedMutatedShapes:      replayShapeCountsFromProxy(result.CapturedMutatedShapes),
		BytesBefore:                result.Report.BytesBefore,
		BytesAfter:                 result.Report.BytesAfter,
		BytesSaved:                 result.Report.Saved(),
		ReducerTokensSaved:         result.ReducerStats.TokensSaved,
		ReducerBlocksModified:      result.ReducerStats.BlocksModified,
		ReducerReadDeltaBlocks:     result.ReducerStats.ReadDeltaBlocks,
		ReducerRepeatedBlocks:      result.ReducerStats.RepeatedOutputBlocks,
		ReducerChunkBlocks:         result.ReducerStats.ChunkDedupBlocks,
		ReducerCapturedBlocks:      result.ReducerStats.CapturedOutputBlocks,
		ReducerEnvelopeBlocks:      result.ReducerStats.CodexEnvelopeBlocks,
		ReducerChunkRefs:           result.ReducerStats.ChunkDedupReferences,
		ReducerChunkRefBytes:       result.ReducerStats.ChunkDedupRefBytes,
		ReducerChunkInputBytes:     result.ReducerStats.ChunkDedupInputBytes,
		UpstreamErrorFrames:        upstream.ErrorFrames,
		UpstreamHTTP400Errors:      upstream.HTTP400Errors,
		UpstreamInvalidRequests:    upstream.InvalidRequestErrors,
		UpstreamResponseFailures:   upstream.ResponseFailedFrames,
		SearchRequestTurns:         result.SearchStats.RequestTurns,
		SearchMutatedRequests:      result.SearchStats.MutatedRequests,
		SearchCapturedMutated:      result.SearchStats.CapturedMutatedRequests,
		SearchUpstreamErrors:       result.SearchStats.UpstreamErrorFrames,
		SearchHTTP400Errors:        result.SearchStats.HTTP400Errors,
		SearchInvalidRequests:      result.SearchStats.InvalidRequestErrors,
		SearchResponseFailures:     result.SearchStats.ResponseFailedFrames,
		ToolOutputMutation:         toolOutputMutation,
		DeltaToolOutputMutationLab: flags.deltaToolOutputMutationLab,
		Lost:                       result.Report.Lost(),
		Elisions:                   result.Report.Elisions,
		GatePassed:                 true,
	}
	if result.ExpectedInstructionExtras > 0 {
		report.Notes = append(report.Notes, "known output-reduce instruction additions were audited as expected extras; unknown instruction changes still fail the lost-comprehension gate")
	}
	if flags.archiveRecoveryNote {
		report.Notes = append(report.Notes, "archive recovery note was enabled for this replay; treat extra model-facing blocks as expected audit findings, not a default-on proof")
	}
	if toolOutputMutation {
		report.Notes = append(report.Notes, "broader Codex WSS tool-output mutation was enabled for this lab/proof replay; product default keeps safe read-delta savings while previous_response_id delta, unknown, or unsafe stateful WSS tool-output bodies stay byte-equal")
	}
	if flags.deltaToolOutputMutationLab {
		report.Notes = append(report.Notes, "previous_response_id delta tool-output mutation lab override was enabled; this is only for reproducing known T354 follow-up 400 failures")
	}
	if flags.codexChunkDedup {
		report.Notes = append(report.Notes, "Codex chunk dedup was forced for this replay; auto policy may also enable it without this flag")
	}
	report.ExpectedExtras = expectedRecoveryNoteExtras(report.Elisions) + result.ExpectedInstructionExtras
	gateLost := report.Lost
	allowExpectedExtras := flags.allowRecoveryNoteExtra || flags.codexChunkDedup || !flags.archiveRecoveryNote
	if allowExpectedExtras {
		gateLost -= report.ExpectedExtras
		if gateLost < 0 {
			gateLost = 0
		}
	}
	if flags.failOnLost && gateLost > 0 {
		report.GatePassed = false
		report.GateFailures = append(report.GateFailures, fmt.Sprintf("lost=%d > 0", gateLost))
	}
	if flags.failOnUpstreamError && report.UpstreamErrorFrames > 0 {
		report.GatePassed = false
		report.GateFailures = append(report.GateFailures,
			fmt.Sprintf("upstream_error_frames=%d invalid_request=%d http_400=%d response_failed=%d",
				report.UpstreamErrorFrames,
				report.UpstreamInvalidRequests,
				report.UpstreamHTTP400Errors,
				report.UpstreamResponseFailures))
	}
	return report, nil
}

type wssABReplayUpstreamReport struct {
	ErrorFrames          int
	HTTP400Errors        int
	InvalidRequestErrors int
	ResponseFailedFrames int
}

func wssABReplayUpstreamDiagnostics(frames []proxy.WSSABReplayFrame) wssABReplayUpstreamReport {
	var out wssABReplayUpstreamReport
	for _, frame := range frames {
		if frame.Direction != wsmitm.DirServerToClient {
			continue
		}
		env, err := wsmitm.Parse(frame.Payload)
		if err != nil {
			continue
		}
		switch env.Kind {
		case wsmitm.FrameKindError:
			out.ErrorFrames++
			status, errorType := wssABReplayErrorStatusAndType(frame.Payload)
			if status == "400" {
				out.HTTP400Errors++
			}
			if errorType == "invalid_request_error" {
				out.InvalidRequestErrors++
			}
		case wsmitm.FrameKindResponseFailed:
			out.ErrorFrames++
			out.ResponseFailedFrames++
			status, errorType := wssABReplayErrorStatusAndType(frame.Payload)
			if status == "400" {
				out.HTTP400Errors++
			}
			if errorType == "invalid_request_error" {
				out.InvalidRequestErrors++
			}
		}
	}
	return out
}

func wssABReplayErrorStatusAndType(payload []byte) (string, string) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		return "", ""
	}
	status := wssABReplayJSONScalar(root["status"])
	errorType := wssABReplayNestedErrorType(root["error"])
	if responseRaw := root["response"]; len(responseRaw) > 0 {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(responseRaw, &response); err == nil {
			if status == "" {
				status = wssABReplayJSONScalar(response["status"])
			}
			if errorType == "" {
				errorType = wssABReplayNestedErrorType(response["error"])
			}
		}
	}
	return status, errorType
}

func wssABReplayNestedErrorType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	return wssABReplayJSONScalar(fields["type"])
}

func wssABReplayJSONScalar(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return strings.Trim(strings.TrimSpace(string(raw)), `"`)
}

func replayShapeCountsFromProxy(counts proxy.WSSABReplayShapeCounts) replayShapeCounts {
	return replayShapeCounts{
		Root:        counts.Root,
		Delta:       counts.Delta,
		FullHistory: counts.FullHistory,
	}
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
		if strings.Contains(elision.Preview, "[context-chunk ") && strings.Contains(elision.Preview, "local-archive://") {
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
		Mutated   bool            `json:"mutated"`
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
	return proxy.WSSABReplayFrame{Direction: direction, Payload: body, Mutated: rec.Mutated}, nil
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
	if report.CapturedMutatedRequests > 0 {
		fmt.Fprintf(w, "  captured_mutated: %d\n", report.CapturedMutatedRequests)
	}
	fmt.Fprintf(w, "  request_shapes:   root=%d delta=%d full_history=%d\n",
		report.RequestShapes.Root, report.RequestShapes.Delta, report.RequestShapes.FullHistory)
	fmt.Fprintf(w, "  mutated_shapes:   root=%d delta=%d full_history=%d\n",
		report.MutatedShapes.Root, report.MutatedShapes.Delta, report.MutatedShapes.FullHistory)
	if report.CapturedMutatedRequests > 0 {
		fmt.Fprintf(w, "  captured_shapes:  root=%d delta=%d full_history=%d\n",
			report.CapturedMutatedShapes.Root, report.CapturedMutatedShapes.Delta, report.CapturedMutatedShapes.FullHistory)
	}
	fmt.Fprintf(w, "  bytes_before:     %d\n", report.BytesBefore)
	fmt.Fprintf(w, "  bytes_after:      %d\n", report.BytesAfter)
	fmt.Fprintf(w, "  bytes_saved:      %d\n", report.BytesSaved)
	fmt.Fprintf(w, "  reducer_tokens:   %d\n", report.ReducerTokensSaved)
	fmt.Fprintf(w, "  tool_output_mut:  %t\n", report.ToolOutputMutation)
	if report.ReducerBlocksModified > 0 {
		fmt.Fprintf(w, "  reducer_blocks:   modified=%d read_delta=%d repeated=%d chunk=%d captured=%d envelope=%d\n",
			report.ReducerBlocksModified, report.ReducerReadDeltaBlocks, report.ReducerRepeatedBlocks,
			report.ReducerChunkBlocks, report.ReducerCapturedBlocks, report.ReducerEnvelopeBlocks)
	}
	if report.ReducerChunkRefs > 0 || report.ReducerChunkRefBytes > 0 || report.ReducerChunkInputBytes > 0 {
		fmt.Fprintf(w, "  chunk_refs:       refs=%d referenced_bytes=%d input_bytes=%d\n",
			report.ReducerChunkRefs, report.ReducerChunkRefBytes, report.ReducerChunkInputBytes)
	}
	fmt.Fprintf(w, "  upstream_errors:  frames=%d invalid_request=%d http_400=%d response_failed=%d\n",
		report.UpstreamErrorFrames,
		report.UpstreamInvalidRequests,
		report.UpstreamHTTP400Errors,
		report.UpstreamResponseFailures)
	fmt.Fprintf(w, "  search_turns:     requests=%d mutated=%d captured=%d upstream_errors=%d invalid_request=%d http_400=%d response_failed=%d\n",
		report.SearchRequestTurns,
		report.SearchMutatedRequests,
		report.SearchCapturedMutated,
		report.SearchUpstreamErrors,
		report.SearchInvalidRequests,
		report.SearchHTTP400Errors,
		report.SearchResponseFailures)
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

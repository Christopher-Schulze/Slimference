package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/proxy"
)

type wssShadowMirrorReplayFlags struct {
	path         string
	outputFormat string
	socketSeq    uint64
	help         bool
}

const wssShadowMirrorReplayHelpText = `wss-shadow-mirror-replay: replay captured Codex WSS frames through the shadow server-state mirror

Usage:
  go run ./scripts/utils wss-shadow-mirror-replay <frames.jsonl> [--json] [--socket-seq=N]

The report is content-free. It reclassifies existing frame captures with the
current servermirror logic so large generic shadow-mirror headroom can be ranked
into concrete command/parser/recovery lanes without touching runtime traffic.`

type wssShadowMirrorReplayReport struct {
	Path         string `json:"path"`
	Files        int    `json:"files,omitempty"`
	SkippedFiles int    `json:"skipped_files,omitempty"`
	proxy.WSSShadowMirrorReplayResult
	TopFiles []wssShadowMirrorReplayFileReport `json:"top_files,omitempty"`
}

type wssShadowMirrorReplayFileReport struct {
	Path                    string `json:"path"`
	RequestTurns            int    `json:"request_turns"`
	CapturedMutatedRequests int    `json:"captured_mutated_requests,omitempty"`
	NormalizedBytes         int    `json:"normalized_bytes"`
	ReferenceableBytes      int    `json:"referenceable_bytes"`
	CandidateTokensEstimate int    `json:"candidate_tokens_estimate"`
}

func runWSSShadowMirrorReplay(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSShadowMirrorReplayFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssShadowMirrorReplayHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-shadow-mirror-replay <frames.jsonl> [--json] [--socket-seq=N]")
		return 2
	}
	report, err := loadWSSShadowMirrorReplayReport(flags)
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
	writeWSSShadowMirrorReplayText(stdout, report)
	return 0
}

func parseWSSShadowMirrorReplayFlags(args []string) (wssShadowMirrorReplayFlags, error) {
	flags := wssShadowMirrorReplayFlags{outputFormat: outputText}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--socket-seq":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("--socket-seq requires a value")
			}
			i++
			n, err := parseSocketSeqFlag("--socket-seq", args[i])
			if err != nil {
				return flags, err
			}
			flags.socketSeq = n
		case strings.HasPrefix(arg, "--socket-seq="):
			n, err := parseSocketSeqFlag("--socket-seq", strings.TrimPrefix(arg, "--socket-seq="))
			if err != nil {
				return flags, err
			}
			flags.socketSeq = n
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

func loadWSSShadowMirrorReplayReport(flags wssShadowMirrorReplayFlags) (wssShadowMirrorReplayReport, error) {
	paths, err := collectWSSShadowMirrorReplayFiles(flags.path)
	if err != nil {
		return wssShadowMirrorReplayReport{}, err
	}
	report := wssShadowMirrorReplayReport{Path: flags.path, Files: len(paths)}
	for _, path := range paths {
		frames, err := readWSSABReplayFrames(path)
		if err != nil {
			if strings.Contains(err.Error(), "contained no frames") {
				report.SkippedFiles++
				continue
			}
			return wssShadowMirrorReplayReport{}, err
		}
		frames = filterWSSABReplayFramesBySocketSeq(frames, flags.socketSeq)
		if len(frames) == 0 {
			if flags.socketSeq > 0 {
				continue
			}
			return wssShadowMirrorReplayReport{}, fmt.Errorf("replay %s contained no frames", path)
		}
		fileResult, err := proxy.RunWSSShadowMirrorReplay(frames)
		if err != nil {
			return wssShadowMirrorReplayReport{}, fmt.Errorf("%s: %w", path, err)
		}
		addWSSShadowMirrorReplayResult(&report.WSSShadowMirrorReplayResult, fileResult)
		if fileResult.Normalized.ReferenceableBytes > 0 {
			report.TopFiles = append(report.TopFiles, wssShadowMirrorReplayFileReport{
				Path:                    path,
				RequestTurns:            fileResult.RequestTurns,
				CapturedMutatedRequests: fileResult.CapturedMutatedRequests,
				NormalizedBytes:         fileResult.Normalized.Bytes,
				ReferenceableBytes:      fileResult.Normalized.ReferenceableBytes,
				CandidateTokensEstimate: fileResult.Normalized.CandidateTokensEstimate,
			})
		}
	}
	if report.RequestTurns == 0 {
		if flags.socketSeq > 0 {
			return wssShadowMirrorReplayReport{}, fmt.Errorf("no replay frames for socket_seq=%d in %s", flags.socketSeq, flags.path)
		}
		return wssShadowMirrorReplayReport{}, fmt.Errorf("replay %s contained no request turns", flags.path)
	}
	finalizeWSSShadowMirrorReplayResult(&report.WSSShadowMirrorReplayResult)
	sort.Slice(report.TopFiles, func(i, j int) bool {
		if report.TopFiles[i].ReferenceableBytes != report.TopFiles[j].ReferenceableBytes {
			return report.TopFiles[i].ReferenceableBytes > report.TopFiles[j].ReferenceableBytes
		}
		return report.TopFiles[i].Path < report.TopFiles[j].Path
	})
	if len(report.TopFiles) > 20 {
		report.TopFiles = report.TopFiles[:20]
	}
	return report, nil
}

func collectWSSShadowMirrorReplayFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat replay path %s: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var paths []string
	err = filepath.WalkDir(path, func(child string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		lower := strings.ToLower(name)
		if name == "frames.jsonl" || strings.Contains(lower, "frames") || strings.Contains(lower, "capture") {
			paths = append(paths, child)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan replay dir %s: %w", path, err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("replay dir %s contained no frame capture jsonl files", path)
	}
	return paths, nil
}

func addWSSShadowMirrorReplayResult(total *proxy.WSSShadowMirrorReplayResult, next proxy.WSSShadowMirrorReplayResult) {
	total.Frames += next.Frames
	total.RequestTurns += next.RequestTurns
	total.CapturedMutatedRequests += next.CapturedMutatedRequests
	total.MissingSessionID += next.MissingSessionID
	total.RequestShapes.Root += next.RequestShapes.Root
	total.RequestShapes.Delta += next.RequestShapes.Delta
	total.RequestShapes.FullHistory += next.RequestShapes.FullHistory
	addWSSShadowMirrorReplayExact(&total.Exact, next.Exact)
	addWSSShadowMirrorReplayExact(&total.Normalized, next.Normalized)
	total.Rows = mergeWSSShadowMirrorReplayRows(total.Rows, next.Rows)
	total.StatefulSafeRows = mergeWSSShadowMirrorReplayRows(total.StatefulSafeRows, next.StatefulSafeRows)
	total.Notes = append(total.Notes, next.Notes...)
}

func addWSSShadowMirrorReplayExact(total *proxy.WSSShadowMirrorReplayExact, next proxy.WSSShadowMirrorReplayExact) {
	total.BlocksOrSegments += next.BlocksOrSegments
	total.Bytes += next.Bytes
	total.Referenceable += next.Referenceable
	total.ReferenceableBytes += next.ReferenceableBytes
}

func mergeWSSShadowMirrorReplayRows(existing []proxy.WSSShadowMirrorReplayRow, next []proxy.WSSShadowMirrorReplayRow) []proxy.WSSShadowMirrorReplayRow {
	byKey := make(map[string]*proxy.WSSShadowMirrorReplayRow, len(existing)+len(next))
	for i := range existing {
		row := existing[i]
		byKey[row.RequestShape+"\x00"+row.Kind] = &row
	}
	for _, row := range next {
		key := row.RequestShape + "\x00" + row.Kind
		acc := byKey[key]
		if acc == nil {
			row.ReferenceableBytePct = 0
			row.CandidateTokensEstimate = 0
			byKey[key] = &row
			continue
		}
		acc.Requests += row.Requests
		acc.ReferenceableRequests += row.ReferenceableRequests
		acc.Segments += row.Segments
		acc.Bytes += row.Bytes
		acc.ReferenceableSegments += row.ReferenceableSegments
		acc.ReferenceableBytes += row.ReferenceableBytes
	}
	out := make([]proxy.WSSShadowMirrorReplayRow, 0, len(byKey))
	for _, row := range byKey {
		out = append(out, *row)
	}
	return out
}

func finalizeWSSShadowMirrorReplayResult(report *proxy.WSSShadowMirrorReplayResult) {
	report.Exact.ReferenceableBytePct = wssShadowMirrorReplayPercent(report.Exact.ReferenceableBytes, report.Exact.Bytes)
	report.Exact.CandidateTokensEstimate = wssShadowMirrorReplayTokens(report.Exact.ReferenceableBytes)
	report.Normalized.ReferenceableBytePct = wssShadowMirrorReplayPercent(report.Normalized.ReferenceableBytes, report.Normalized.Bytes)
	report.Normalized.CandidateTokensEstimate = wssShadowMirrorReplayTokens(report.Normalized.ReferenceableBytes)
	report.Rows = finalizeWSSShadowMirrorReplayRows(report.Rows)
	report.StatefulSafeRows = finalizeWSSShadowMirrorReplayRows(report.StatefulSafeRows)
	report.Notes = dedupeWSSShadowMirrorReplayStrings(report.Notes)
}

func finalizeWSSShadowMirrorReplayRows(rows []proxy.WSSShadowMirrorReplayRow) []proxy.WSSShadowMirrorReplayRow {
	out := rows[:0]
	for _, row := range rows {
		if row.ReferenceableBytes <= 0 {
			continue
		}
		row.ReferenceableBytePct = wssShadowMirrorReplayPercent(row.ReferenceableBytes, row.Bytes)
		row.CandidateTokensEstimate = wssShadowMirrorReplayTokens(row.ReferenceableBytes)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReferenceableBytes != out[j].ReferenceableBytes {
			return out[i].ReferenceableBytes > out[j].ReferenceableBytes
		}
		if rankI, rankJ := wssShadowMirrorReplayShapeRank(out[i].RequestShape), wssShadowMirrorReplayShapeRank(out[j].RequestShape); rankI != rankJ {
			return rankI < rankJ
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func wssShadowMirrorReplayShapeRank(shape string) int {
	switch strings.TrimSpace(shape) {
	case "full_history":
		return 0
	case "delta":
		return 1
	case "root":
		return 2
	default:
		return 3
	}
}

func wssShadowMirrorReplayPercent(num, den int) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) * 100 / float64(den)
}

func wssShadowMirrorReplayTokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

func dedupeWSSShadowMirrorReplayStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func writeWSSShadowMirrorReplayText(w io.Writer, report wssShadowMirrorReplayReport) {
	fmt.Fprintf(w, "WSS shadow mirror replay: %s\n", report.Path)
	if report.Files > 1 {
		fmt.Fprintf(w, "  files:              %d\n", report.Files)
	}
	if report.SkippedFiles > 0 {
		fmt.Fprintf(w, "  skipped_files:      %d\n", report.SkippedFiles)
	}
	fmt.Fprintf(w, "  frames:             %d\n", report.Frames)
	fmt.Fprintf(w, "  request_turns:      %d\n", report.RequestTurns)
	if report.CapturedMutatedRequests > 0 {
		fmt.Fprintf(w, "  captured_mutated:   %d\n", report.CapturedMutatedRequests)
	}
	if report.MissingSessionID > 0 {
		fmt.Fprintf(w, "  missing_session_id: %d\n", report.MissingSessionID)
	}
	fmt.Fprintf(w, "  request_shapes:     root=%d delta=%d full_history=%d\n",
		report.RequestShapes.Root,
		report.RequestShapes.Delta,
		report.RequestShapes.FullHistory)
	fmt.Fprintf(w, "  exact:              bytes=%d referenceable_bytes=%d pct=%.2f candidate_tokens=%d\n",
		report.Exact.Bytes,
		report.Exact.ReferenceableBytes,
		report.Exact.ReferenceableBytePct,
		report.Exact.CandidateTokensEstimate)
	fmt.Fprintf(w, "  normalized:         bytes=%d referenceable_bytes=%d pct=%.2f candidate_tokens=%d\n",
		report.Normalized.Bytes,
		report.Normalized.ReferenceableBytes,
		report.Normalized.ReferenceableBytePct,
		report.Normalized.CandidateTokensEstimate)
	writeWSSShadowMirrorReplayRows(w, "  rows:", report.Rows)
	writeWSSShadowMirrorReplayRows(w, "  stateful_safe_rows:", report.StatefulSafeRows)
	if len(report.TopFiles) > 0 {
		fmt.Fprintln(w, "  top_files:")
		for _, row := range report.TopFiles {
			fmt.Fprintf(w, "    - referenceable_bytes=%d candidate_tokens=%d request_turns=%d path=%s\n",
				row.ReferenceableBytes,
				row.CandidateTokensEstimate,
				row.RequestTurns,
				row.Path)
		}
	}
	for _, note := range report.Notes {
		fmt.Fprintf(w, "  note: %s\n", note)
	}
}

func writeWSSShadowMirrorReplayRows(w io.Writer, title string, rows []proxy.WSSShadowMirrorReplayRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, title)
	for _, row := range rows {
		fmt.Fprintf(w, "    - shape=%s kind=%s requests=%d referenceable_requests=%d bytes=%d referenceable_bytes=%d pct=%.2f candidate_tokens=%d\n",
			row.RequestShape,
			row.Kind,
			row.Requests,
			row.ReferenceableRequests,
			row.Bytes,
			row.ReferenceableBytes,
			row.ReferenceableBytePct,
			row.CandidateTokensEstimate)
	}
}

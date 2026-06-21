package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

type searchCapProfileReport struct {
	Path              string                     `json:"path"`
	SocketSeq         uint64                     `json:"socket_seq,omitempty"`
	Command           string                     `json:"command"`
	Workdir           string                     `json:"workdir,omitempty"`
	Source            string                     `json:"source"`
	Frames            int                        `json:"frames,omitempty"`
	SearchOutputs     int                        `json:"search_outputs"`
	GatePassed        bool                       `json:"gate_passed"`
	GateFailures      []string                   `json:"gate_failures,omitempty"`
	SelectedCandidate *searchCapProfileSelection `json:"selected_candidate,omitempty"`
	Profiles          []searchCapProfileRow      `json:"profiles"`
}

type searchCapProfileRow struct {
	Name                    string  `json:"name"`
	MaxFilesShown           int     `json:"max_files_shown"`
	MaxMatchesPerFile       int     `json:"max_matches_per_file"`
	MinRetainedPct          float64 `json:"min_retained_pct,omitempty"`
	Applied                 bool    `json:"applied"`
	InputBytes              int     `json:"input_bytes"`
	OutputBytes             int     `json:"output_bytes"`
	SavedBytes              int     `json:"saved_bytes"`
	SavingsPct              float64 `json:"savings_pct"`
	OriginalFiles           int     `json:"original_files"`
	OriginalMatches         int     `json:"original_matches"`
	ShownFiles              int     `json:"shown_files"`
	ShownMatches            int     `json:"shown_matches"`
	OmittedFiles            int     `json:"omitted_files"`
	OmittedMatches          int     `json:"omitted_matches"`
	MatchRetentionPct       float64 `json:"match_retention_pct"`
	SavedBytesVsDefault     int     `json:"saved_bytes_vs_default,omitempty"`
	OmittedMatchesVsDefault int     `json:"omitted_matches_vs_default,omitempty"`
}

type searchCapProfileSelection struct {
	Name                    string  `json:"name"`
	MaxFilesShown           int     `json:"max_files_shown"`
	MaxMatchesPerFile       int     `json:"max_matches_per_file"`
	MinRetainedPct          float64 `json:"min_retained_pct,omitempty"`
	SavedBytesVsDefault     int     `json:"saved_bytes_vs_default"`
	MatchRetentionPct       float64 `json:"match_retention_pct"`
	OmittedMatchesVsDefault int     `json:"omitted_matches_vs_default"`
}

type searchCapProfileCandidate struct {
	Name    string
	Options filter.SearchCompactOptions
}

const (
	searchCapReleaseMinRetainedPct        = 40.0
	searchCapReleaseMinSearchOutputs      = 2
	searchCapReleaseMinExtraReducerTokens = 1
)

var searchCapReleaseCandidateSpecs = []struct {
	name    string
	files   int
	matches int
}{
	{name: "candidate_30x15", files: 30, matches: 15},
	{name: "candidate_25x15", files: 25, matches: 15},
	{name: "candidate_20x10", files: 20, matches: 10},
}

type searchCapProfileCandidateFlags []searchCapProfileCandidate

func (f *searchCapProfileCandidateFlags) String() string {
	if f == nil || len(*f) == 0 {
		return ""
	}
	parts := make([]string, 0, len(*f))
	for _, candidate := range *f {
		parts = append(parts, fmt.Sprintf("%d:%d", candidate.Options.MaxFilesShown, candidate.Options.MaxMatchesPerFile))
	}
	return strings.Join(parts, ",")
}

func (f *searchCapProfileCandidateFlags) Set(raw string) error {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return fmt.Errorf("candidate must be files:matches")
	}
	files, err := parsePositiveSearchCapProfileInt("candidate files", parts[0])
	if err != nil {
		return err
	}
	matches, err := parsePositiveSearchCapProfileInt("candidate matches", parts[1])
	if err != nil {
		return err
	}
	*f = append(*f, searchCapProfileCandidate{
		Name: fmt.Sprintf("candidate_%dx%d", files, matches),
		Options: filter.SearchCompactOptions{
			MaxFilesShown:     files,
			MaxMatchesPerFile: matches,
		},
	})
	return nil
}

func parsePositiveSearchCapProfileInt(label, raw string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be a positive integer", label)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", label)
	}
	return n, nil
}

func runSearchCapProfile(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("search-cap-profile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var command string
	var workdir string
	var inputPath string
	var framesPath string
	var socketSeq uint64
	var jsonOut bool
	var aggressiveFiles int
	var aggressiveMatches int
	var candidates searchCapProfileCandidateFlags
	var requireApplicable bool
	var requireAggressiveSavings bool
	var minAggressiveRetainedPct float64
	var minCandidateRetainedPct float64
	fs.StringVar(&command, "command", "", "Search command line that produced the output")
	fs.StringVar(&workdir, "workdir", "", "Optional absolute workdir used to normalize search commands")
	fs.StringVar(&inputPath, "input", "", "Path to captured search stdout")
	fs.StringVar(&framesPath, "frames", "", "Path to WSS frame capture JSONL")
	fs.Uint64Var(&socketSeq, "socket-seq", 0, "Replay only records captured from WSS socket_seq N")
	fs.BoolVar(&jsonOut, "json", false, "Output JSON")
	fs.IntVar(&aggressiveFiles, "aggressive-files", 10, "Aggressive profile file cap")
	fs.IntVar(&aggressiveMatches, "aggressive-matches", 5, "Aggressive profile per-file match cap")
	fs.Var(&candidates, "candidate", "Candidate cap as files:matches; repeatable")
	fs.BoolVar(&requireApplicable, "require-applicable", false, "Fail if the input is not compactable search output")
	fs.BoolVar(&requireAggressiveSavings, "require-aggressive-savings", false, "Fail if every non-default profile does not save more bytes than default")
	fs.Float64Var(&minAggressiveRetainedPct, "min-aggressive-retained-pct", 0, "Fail if aggressive match retention is below this percentage")
	fs.Float64Var(&minCandidateRetainedPct, "min-candidate-retained-pct", 0, "Fail if any non-default candidate match retention is below this percentage")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if aggressiveFiles <= 0 {
		fmt.Fprintln(stderr, "--aggressive-files must be a positive integer")
		return 2
	}
	if aggressiveMatches <= 0 {
		fmt.Fprintln(stderr, "--aggressive-matches must be a positive integer")
		return 2
	}
	if minAggressiveRetainedPct < 0 {
		fmt.Fprintln(stderr, "--min-aggressive-retained-pct must be >= 0")
		return 2
	}
	if minAggressiveRetainedPct > 100 {
		fmt.Fprintln(stderr, "--min-aggressive-retained-pct must be <= 100")
		return 2
	}
	if minCandidateRetainedPct < 0 {
		fmt.Fprintln(stderr, "--min-candidate-retained-pct must be >= 0")
		return 2
	}
	if minCandidateRetainedPct > 100 {
		fmt.Fprintln(stderr, "--min-candidate-retained-pct must be <= 100")
		return 2
	}
	if fs.NArg() != 0 || ((strings.TrimSpace(inputPath) == "") == (strings.TrimSpace(framesPath) == "")) {
		fmt.Fprintln(stderr, "Usage: search-cap-profile (--command <cmd> --input <stdout.txt> | --frames <frames.jsonl>) [--json]")
		return 2
	}
	report, err := loadSearchCapProfileReport(searchCapProfileFlags{
		command:                  command,
		workdir:                  workdir,
		inputPath:                inputPath,
		framesPath:               framesPath,
		socketSeq:                socketSeq,
		aggressiveFiles:          aggressiveFiles,
		aggressiveMatches:        aggressiveMatches,
		candidates:               candidates,
		requireApplicable:        requireApplicable,
		requireAggressiveSavings: requireAggressiveSavings,
		minAggressiveRetainedPct: minAggressiveRetainedPct,
		minCandidateRetainedPct:  minCandidateRetainedPct,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if jsonOut {
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
	writeSearchCapProfileText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

type searchCapProfileFlags struct {
	command                  string
	workdir                  string
	inputPath                string
	framesPath               string
	socketSeq                uint64
	aggressiveFiles          int
	aggressiveMatches        int
	candidates               []searchCapProfileCandidate
	requireApplicable        bool
	requireAggressiveSavings bool
	minAggressiveRetainedPct float64
	minCandidateRetainedPct  float64
}

func loadSearchCapProfileReport(flags searchCapProfileFlags) (searchCapProfileReport, error) {
	if strings.TrimSpace(flags.framesPath) != "" {
		return loadSearchCapProfileFramesReport(flags)
	}
	data, err := os.ReadFile(flags.inputPath)
	if err != nil {
		return searchCapProfileReport{}, fmt.Errorf("read search output %s: %w", flags.inputPath, err)
	}
	command := normalizedSearchCapCommand(flags.command, flags.workdir)
	argv := filter.ArgvForCapturedOutput(command)
	report := searchCapProfileReport{
		Path:          flags.inputPath,
		Command:       command,
		Workdir:       strings.TrimSpace(flags.workdir),
		Source:        "input",
		SearchOutputs: 1,
		GatePassed:    true,
	}
	report.Profiles = buildSearchCapProfileRows(
		buildSearchCapProfileRow("default", argv, data, searchCapProfileDefaultOptions(flags)),
		searchCapProfileCandidates(flags),
		func(candidate searchCapProfileCandidate) searchCapProfileRow {
			return buildSearchCapProfileRow(candidate.Name, argv, data, candidate.Options)
		},
	)
	report.SelectedCandidate = selectSearchCapProfileCandidate(report, flags)
	report.GateFailures = searchCapProfileGateFailures(report, flags)
	report.GatePassed = len(report.GateFailures) == 0
	return report, nil
}

func loadSearchCapProfileFramesReport(flags searchCapProfileFlags) (searchCapProfileReport, error) {
	frames, err := readWSSABReplayFrames(flags.framesPath)
	if err != nil {
		return searchCapProfileReport{}, err
	}
	frames = filterWSSABReplayFramesBySocketSeq(frames, flags.socketSeq)
	if len(frames) == 0 {
		if flags.socketSeq > 0 {
			return searchCapProfileReport{}, fmt.Errorf("no replay frames for socket_seq=%d in %s", flags.socketSeq, flags.framesPath)
		}
		return searchCapProfileReport{}, fmt.Errorf("replay %s contained no frames", flags.framesPath)
	}
	outputs, err := searchCapProfileOutputsFromFrames(frames)
	if err != nil {
		return searchCapProfileReport{}, err
	}
	report := searchCapProfileReport{
		Path:          flags.framesPath,
		SocketSeq:     flags.socketSeq,
		Source:        "frames",
		Frames:        len(frames),
		SearchOutputs: len(outputs),
		GatePassed:    true,
	}
	report.Profiles = buildSearchCapProfileRows(
		aggregateSearchCapProfileRows("default", outputs, searchCapProfileDefaultOptions(flags)),
		searchCapProfileCandidates(flags),
		func(candidate searchCapProfileCandidate) searchCapProfileRow {
			return aggregateSearchCapProfileRows(candidate.Name, outputs, candidate.Options)
		},
	)
	report.SelectedCandidate = selectSearchCapProfileCandidate(report, flags)
	report.GateFailures = searchCapProfileGateFailures(report, flags)
	report.GatePassed = len(report.GateFailures) == 0
	return report, nil
}

type searchCapProfileOutput struct {
	command string
	output  []byte
}

type searchCapProfileToolUse struct {
	command string
	workdir string
}

func searchCapProfileOutputsFromFrames(frames []proxy.WSSABReplayFrame) ([]searchCapProfileOutput, error) {
	toolUses := make(map[string]searchCapProfileToolUse)
	var outputs []searchCapProfileOutput
	for i, frame := range frames {
		if frame.Mutated {
			continue
		}
		switch frame.Direction {
		case wsmitm.DirServerToClient:
			rememberSearchCapProfileToolUses(toolUses, frame.Payload)
		case wsmitm.DirClientToServer:
			rememberSearchCapProfileToolUses(toolUses, frame.Payload)
			found, err := searchCapProfileOutputsFromRequest(toolUses, frame.Payload)
			if err != nil {
				return nil, fmt.Errorf("extract search outputs from frame %d: %w", i, err)
			}
			outputs = append(outputs, found...)
		default:
			return nil, fmt.Errorf("frame %d has unsupported direction %q", i, frame.Direction)
		}
	}
	return outputs, nil
}

func rememberSearchCapProfileToolUses(toolUses map[string]searchCapProfileToolUse, body []byte) {
	for _, item := range searchCapProfileInputItems(body) {
		callID := strings.TrimSpace(rawStringField(item, "call_id"))
		if callID == "" {
			callID = strings.TrimSpace(rawStringField(item, "id"))
		}
		if callID == "" || strings.TrimSpace(rawStringField(item, "type")) != "function_call" {
			continue
		}
		toolUse := searchCapProfileToolUseFromFunctionCall(item)
		if toolUse.command == "" {
			continue
		}
		toolUses[callID] = toolUse
	}
}

func searchCapProfileOutputsFromRequest(toolUses map[string]searchCapProfileToolUse, body []byte) ([]searchCapProfileOutput, error) {
	var outputs []searchCapProfileOutput
	for _, item := range searchCapProfileInputItems(body) {
		if strings.TrimSpace(rawStringField(item, "type")) != "function_call_output" {
			continue
		}
		callID := strings.TrimSpace(rawStringField(item, "call_id"))
		if callID == "" {
			callID = strings.TrimSpace(rawStringField(item, "id"))
		}
		toolUse := toolUses[callID]
		if strings.TrimSpace(toolUse.command) == "" {
			continue
		}
		normalized := normalizedSearchCapCommand(toolUse.command, toolUse.workdir)
		argv := filter.ArgvForCapturedOutput(normalized)
		output := searchCapProfileToolOutputPayload(rawStringField(item, "output"))
		if _, ok := filter.SearchCompactProfile(argv, []byte(output), filter.SearchCompactOptions{}); !ok {
			continue
		}
		outputs = append(outputs, searchCapProfileOutput{
			command: normalized,
			output:  []byte(output),
		})
	}
	return outputs, nil
}

func searchCapProfileInputItems(body []byte) []map[string]json.RawMessage {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	for _, key := range []string{"input", "output"} {
		itemsRaw, ok := raw[key]
		if !ok {
			continue
		}
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(itemsRaw, &items); err == nil {
			return items
		}
		var wrapped struct {
			Output []map[string]json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(itemsRaw, &wrapped); err == nil && len(wrapped.Output) > 0 {
			return wrapped.Output
		}
	}
	if itemRaw, ok := raw["item"]; ok {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(itemRaw, &item); err == nil && len(item) > 0 {
			return []map[string]json.RawMessage{item}
		}
	}
	if responseRaw, ok := raw["response"]; ok {
		var response struct {
			Output []map[string]json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(responseRaw, &response); err == nil {
			return response.Output
		}
	}
	return nil
}

func searchCapProfileToolUseFromFunctionCall(item map[string]json.RawMessage) searchCapProfileToolUse {
	argsRaw, ok := item["arguments"]
	if !ok {
		return searchCapProfileToolUse{}
	}
	var argsString string
	if err := json.Unmarshal(argsRaw, &argsString); err == nil {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(argsString), &obj); err == nil {
			return searchCapProfileToolUseFromArgs(obj)
		}
		return searchCapProfileToolUse{}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(argsRaw, &obj); err != nil {
		return searchCapProfileToolUse{}
	}
	return searchCapProfileToolUseFromArgs(obj)
}

func searchCapProfileToolUseFromArgs(args map[string]json.RawMessage) searchCapProfileToolUse {
	toolUse := searchCapProfileToolUse{workdir: strings.TrimSpace(rawStringField(args, "workdir"))}
	for _, key := range []string{"cmd", "command", "command_line"} {
		if command := strings.TrimSpace(rawStringField(args, key)); command != "" {
			toolUse.command = command
			return toolUse
		}
	}
	if argvRaw, ok := args["argv"]; ok {
		var argv []string
		if err := json.Unmarshal(argvRaw, &argv); err == nil && len(argv) > 0 {
			toolUse.command = strings.Join(argv, " ")
			return toolUse
		}
	}
	return toolUse
}

func rawStringField(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func searchCapProfileToolOutputPayload(output string) string {
	if !strings.Contains(output, "Process exited with code ") {
		return output
	}
	for _, marker := range []string{"\nOutput:\n", "\r\nOutput:\r\n"} {
		_, after, ok := strings.Cut(output, marker)
		if !ok {
			continue
		}
		payload := after
		if payload != "" {
			return payload
		}
	}
	return output
}

func normalizedSearchCapCommand(command, workdir string) string {
	command = strings.TrimSpace(command)
	if normalized := filter.NormalizeSearchCommandLine(command, workdir); normalized != "" {
		return normalized
	}
	return command
}

func aggregateSearchCapProfileRows(name string, outputs []searchCapProfileOutput, options filter.SearchCompactOptions) searchCapProfileRow {
	options = searchCapProfileOptionsWithDefaults(options)
	row := searchCapProfileRow{
		Name:              name,
		MaxFilesShown:     options.MaxFilesShown,
		MaxMatchesPerFile: options.MaxMatchesPerFile,
		MinRetainedPct:    options.MinRetainedPct,
	}
	for _, output := range outputs {
		item := buildSearchCapProfileRow(name, filter.ArgvForCapturedOutput(output.command), output.output, options)
		row.Applied = row.Applied || item.Applied
		row.InputBytes += item.InputBytes
		row.OutputBytes += item.OutputBytes
		row.SavedBytes += item.SavedBytes
		row.OriginalFiles += item.OriginalFiles
		row.OriginalMatches += item.OriginalMatches
		row.ShownFiles += item.ShownFiles
		row.ShownMatches += item.ShownMatches
		row.OmittedFiles += item.OmittedFiles
		row.OmittedMatches += item.OmittedMatches
	}
	row.SavingsPct = ratioPct(row.InputBytes, row.SavedBytes)
	row.MatchRetentionPct = ratioPct(row.OriginalMatches, row.ShownMatches)
	return row
}

func searchCapProfileCandidates(flags searchCapProfileFlags) []searchCapProfileCandidate {
	minRetention := searchCapProfileMinimumRetention(flags)
	if len(flags.candidates) > 0 {
		out := make([]searchCapProfileCandidate, len(flags.candidates))
		copy(out, flags.candidates)
		for i := range out {
			out[i].Options.MinRetainedPct = minRetention
		}
		return out
	}
	if minRetention > 0 {
		return searchCapReleaseCandidates(minRetention)
	}
	return []searchCapProfileCandidate{{
		Name: "aggressive",
		Options: filter.SearchCompactOptions{
			MaxFilesShown:     flags.aggressiveFiles,
			MaxMatchesPerFile: flags.aggressiveMatches,
			MinRetainedPct:    minRetention,
		},
	}}
}

func searchCapReleaseCandidates(minRetention float64) []searchCapProfileCandidate {
	out := make([]searchCapProfileCandidate, 0, len(searchCapReleaseCandidateSpecs))
	for _, spec := range searchCapReleaseCandidateSpecs {
		out = append(out, searchCapProfileCandidate{
			Name: spec.name,
			Options: filter.SearchCompactOptions{
				MaxFilesShown:     spec.files,
				MaxMatchesPerFile: spec.matches,
				MinRetainedPct:    minRetention,
			},
		})
	}
	return out
}

func searchCapProfileDefaultOptions(flags searchCapProfileFlags) filter.SearchCompactOptions {
	return filter.SearchCompactOptions{MinRetainedPct: searchCapProfileMinimumRetention(flags)}
}

func buildSearchCapProfileRows(defaultRow searchCapProfileRow, candidates []searchCapProfileCandidate, build func(searchCapProfileCandidate) searchCapProfileRow) []searchCapProfileRow {
	rows := make([]searchCapProfileRow, 0, 1+len(candidates))
	rows = append(rows, defaultRow)
	for _, candidate := range candidates {
		row := build(candidate)
		row.SavedBytesVsDefault = row.SavedBytes - defaultRow.SavedBytes
		row.OmittedMatchesVsDefault = row.OmittedMatches - defaultRow.OmittedMatches
		rows = append(rows, row)
	}
	return rows
}

func buildSearchCapProfileRow(name string, argv []string, data []byte, options filter.SearchCompactOptions) searchCapProfileRow {
	stats, _ := filter.SearchCompactProfile(argv, data, options)
	options = searchCapProfileOptionsWithDefaults(options)
	savedBytes := max(stats.InputBytes-stats.OutputBytes, 0)
	return searchCapProfileRow{
		Name:              name,
		MaxFilesShown:     options.MaxFilesShown,
		MaxMatchesPerFile: options.MaxMatchesPerFile,
		MinRetainedPct:    options.MinRetainedPct,
		Applied:           stats.Applied,
		InputBytes:        stats.InputBytes,
		OutputBytes:       stats.OutputBytes,
		SavedBytes:        savedBytes,
		SavingsPct:        ratioPct(stats.InputBytes, savedBytes),
		OriginalFiles:     stats.OriginalFiles,
		OriginalMatches:   stats.OriginalMatches,
		ShownFiles:        stats.ShownFiles,
		ShownMatches:      stats.ShownMatches,
		OmittedFiles:      stats.OmittedFiles,
		OmittedMatches:    stats.OmittedMatches,
		MatchRetentionPct: ratioPct(stats.OriginalMatches, stats.ShownMatches),
	}
}

func searchCapProfileOptionsWithDefaults(options filter.SearchCompactOptions) filter.SearchCompactOptions {
	if options.MaxFilesShown <= 0 {
		options.MaxFilesShown = 30
	}
	if options.MaxMatchesPerFile <= 0 {
		options.MaxMatchesPerFile = 20
	}
	return options
}

func searchCapProfileGateFailures(report searchCapProfileReport, flags searchCapProfileFlags) []string {
	var failures []string
	defaultRow := report.Profiles[0]
	if flags.requireApplicable && (report.SearchOutputs == 0 || !defaultRow.Applied) {
		failures = append(failures, "expected compactable search output")
	}
	minRetention := searchCapProfileMinimumRetention(flags)
	for _, row := range report.Profiles[1:] {
		if flags.requireAggressiveSavings && row.SavedBytes <= defaultRow.SavedBytes {
			failures = append(failures, fmt.Sprintf("expected %s profile to save more bytes than default", row.Name))
		}
		if minRetention > 0 && row.MatchRetentionPct+1e-9 < minRetention {
			failures = append(failures, fmt.Sprintf("%s match retention %.2f%% < min %.2f%%", row.Name, row.MatchRetentionPct, minRetention))
		}
	}
	return failures
}

func selectSearchCapProfileCandidate(report searchCapProfileReport, flags searchCapProfileFlags) *searchCapProfileSelection {
	if len(report.Profiles) < 2 {
		return nil
	}
	minRetention := searchCapProfileMinimumRetention(flags)
	var selected *searchCapProfileRow
	for i := range report.Profiles[1:] {
		row := &report.Profiles[i+1]
		if !row.Applied || row.SavedBytesVsDefault <= 0 {
			continue
		}
		if minRetention > 0 && row.MatchRetentionPct+1e-9 < minRetention {
			continue
		}
		if selected == nil ||
			row.SavedBytesVsDefault > selected.SavedBytesVsDefault ||
			(row.SavedBytesVsDefault == selected.SavedBytesVsDefault && row.MatchRetentionPct > selected.MatchRetentionPct) {
			selected = row
		}
	}
	if selected == nil {
		return nil
	}
	return &searchCapProfileSelection{
		Name:                    selected.Name,
		MaxFilesShown:           selected.MaxFilesShown,
		MaxMatchesPerFile:       selected.MaxMatchesPerFile,
		MinRetainedPct:          selected.MinRetainedPct,
		SavedBytesVsDefault:     selected.SavedBytesVsDefault,
		MatchRetentionPct:       selected.MatchRetentionPct,
		OmittedMatchesVsDefault: selected.OmittedMatchesVsDefault,
	}
}

func searchCapProfileMinimumRetention(flags searchCapProfileFlags) float64 {
	minRetention := flags.minAggressiveRetainedPct
	if flags.minCandidateRetainedPct > minRetention {
		minRetention = flags.minCandidateRetainedPct
	}
	return minRetention
}

func writeSearchCapProfileText(w io.Writer, report searchCapProfileReport) {
	fmt.Fprintf(w, "=== Search Cap Profile: %s ===\n", report.Path)
	if report.Command != "" {
		fmt.Fprintf(w, "command: %s\n", report.Command)
	}
	fmt.Fprintf(w, "source:  %s\n", report.Source)
	if report.Frames > 0 {
		fmt.Fprintf(w, "frames:  %d\n", report.Frames)
	}
	fmt.Fprintf(w, "search outputs: %d\n", report.SearchOutputs)
	fmt.Fprintf(w, "gate:    %s\n", passFail(report.GatePassed))
	if report.SelectedCandidate != nil {
		fmt.Fprintf(w, "selected candidate: %s (%d/%d, min %.2f%%, saved %+d bytes, %.2f%% retained)\n",
			report.SelectedCandidate.Name,
			report.SelectedCandidate.MaxFilesShown,
			report.SelectedCandidate.MaxMatchesPerFile,
			report.SelectedCandidate.MinRetainedPct,
			report.SelectedCandidate.SavedBytesVsDefault,
			report.SelectedCandidate.MatchRetentionPct)
	}
	for _, row := range report.Profiles {
		fmt.Fprintf(w, "\n%s profile:\n", row.Name)
		if row.MinRetainedPct > 0 {
			fmt.Fprintf(w, "  caps files/matches:     %d / %d (min %.2f%% retained)\n", row.MaxFilesShown, row.MaxMatchesPerFile, row.MinRetainedPct)
		} else {
			fmt.Fprintf(w, "  caps files/matches:     %d / %d\n", row.MaxFilesShown, row.MaxMatchesPerFile)
		}
		fmt.Fprintf(w, "  applied:                %v\n", row.Applied)
		fmt.Fprintf(w, "  bytes in/out/saved:     %d / %d / %d (%.2f%%)\n", row.InputBytes, row.OutputBytes, row.SavedBytes, row.SavingsPct)
		fmt.Fprintf(w, "  files shown/total:      %d / %d (omitted %d)\n", row.ShownFiles, row.OriginalFiles, row.OmittedFiles)
		fmt.Fprintf(w, "  matches shown/total:    %d / %d (%.2f%% retained, omitted %d)\n", row.ShownMatches, row.OriginalMatches, row.MatchRetentionPct, row.OmittedMatches)
		if row.Name != "default" {
			fmt.Fprintf(w, "  delta vs default:       saved %+d bytes, omitted %+d matches\n", row.SavedBytesVsDefault, row.OmittedMatchesVsDefault)
		}
	}
	if len(report.GateFailures) > 0 {
		fmt.Fprintln(w, "\nGate failures:")
		for _, failure := range report.GateFailures {
			fmt.Fprintf(w, "  - %s\n", failure)
		}
	}
}

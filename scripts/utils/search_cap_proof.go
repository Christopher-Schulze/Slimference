package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

type searchCapProofReport struct {
	Path                    string                       `json:"path"`
	Frames                  int                          `json:"frames"`
	SearchOutputs           int                          `json:"search_outputs"`
	MinCandidateRetainedPct float64                      `json:"min_candidate_retained_pct,omitempty"`
	MinSearchOutputs        int                          `json:"min_search_outputs,omitempty"`
	MinExtraReducerTokens   int                          `json:"min_extra_reducer_tokens,omitempty"`
	DefaultReplay           searchCapProofReplaySummary  `json:"default_replay"`
	SelectedCandidate       *searchCapProofSelection     `json:"selected_candidate,omitempty"`
	Candidates              []searchCapProofCandidateRow `json:"candidates"`
	GatePassed              bool                         `json:"gate_passed"`
	GateFailures            []string                     `json:"gate_failures,omitempty"`
}

type searchCapProofCandidateRow struct {
	Name                    string                       `json:"name"`
	MaxFilesShown           int                          `json:"max_files_shown"`
	MaxMatchesPerFile       int                          `json:"max_matches_per_file"`
	Applied                 bool                         `json:"applied"`
	SavedBytesVsDefault     int                          `json:"saved_bytes_vs_default"`
	MatchRetentionPct       float64                      `json:"match_retention_pct"`
	OmittedMatchesVsDefault int                          `json:"omitted_matches_vs_default"`
	ExtraReducerTokens      int                          `json:"extra_reducer_tokens,omitempty"`
	Replay                  *searchCapProofReplaySummary `json:"replay,omitempty"`
	GatePassed              bool                         `json:"gate_passed"`
	GateFailures            []string                     `json:"gate_failures,omitempty"`
}

type searchCapProofSelection struct {
	Name                    string  `json:"name"`
	MaxFilesShown           int     `json:"max_files_shown"`
	MaxMatchesPerFile       int     `json:"max_matches_per_file"`
	ExtraReducerTokens      int     `json:"extra_reducer_tokens"`
	SavedBytesVsDefault     int     `json:"saved_bytes_vs_default"`
	MatchRetentionPct       float64 `json:"match_retention_pct"`
	OmittedMatchesVsDefault int     `json:"omitted_matches_vs_default"`
}

type searchCapProofReplaySummary struct {
	ReducerTokensSaved       int  `json:"reducer_tokens_saved"`
	BytesSaved               int  `json:"bytes_saved"`
	SearchRequestTurns       int  `json:"search_request_turns"`
	SearchMutatedRequests    int  `json:"search_mutated_requests"`
	SearchCapturedMutated    int  `json:"search_captured_mutated_requests,omitempty"`
	SearchCapProofLatch      bool `json:"search_cap_proof_latch_enabled,omitempty"`
	ToolOutputMutation       bool `json:"tool_output_mutation_enabled"`
	DeltaToolOutputMutation  bool `json:"delta_tool_output_mutation_proof_enabled"`
	UpstreamErrorFrames      int  `json:"upstream_error_frames"`
	UpstreamInvalidRequests  int  `json:"upstream_invalid_request_errors"`
	UpstreamHTTP400Errors    int  `json:"upstream_http_400_errors"`
	UpstreamResponseFailures int  `json:"upstream_response_failed_frames"`
	Lost                     int  `json:"lost"`
	GatePassed               bool `json:"gate_passed"`
}

type searchCapProofFlags struct {
	framesPath              string
	candidates              []searchCapProfileCandidate
	minCandidateRetainedPct float64
	minSearchOutputs        int
	minExtraReducerTokens   int
}

func runSearchCapProof(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("search-cap-proof", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var framesPath string
	var candidates searchCapProfileCandidateFlags
	var minCandidateRetainedPct float64
	var minSearchOutputs int
	var minExtraReducerTokens int
	var jsonOut bool
	fs.StringVar(&framesPath, "frames", "", "Path to WSS frame capture JSONL")
	fs.Var(&candidates, "candidate", "Candidate cap as files:matches; repeatable")
	fs.Float64Var(&minCandidateRetainedPct, "min-candidate-retained-pct", 0, "Reject candidates below this match-retention percentage")
	fs.IntVar(&minSearchOutputs, "min-search-outputs", 0, "Reject captures with fewer resolved search outputs")
	fs.IntVar(&minExtraReducerTokens, "min-extra-reducer-tokens", 1, "Reject candidates with fewer extra reducer tokens than this")
	fs.BoolVar(&jsonOut, "json", false, "Output JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if minCandidateRetainedPct < 0 {
		fmt.Fprintln(stderr, "--min-candidate-retained-pct must be >= 0")
		return 2
	}
	if minSearchOutputs < 0 {
		fmt.Fprintln(stderr, "--min-search-outputs must be >= 0")
		return 2
	}
	if minExtraReducerTokens < 0 {
		fmt.Fprintln(stderr, "--min-extra-reducer-tokens must be >= 0")
		return 2
	}
	if fs.NArg() != 0 || strings.TrimSpace(framesPath) == "" || len(candidates) == 0 {
		fmt.Fprintln(stderr, "Usage: search-cap-proof --frames <frames.jsonl> --candidate <files:matches> [--candidate <files:matches>...] [--json]")
		return 2
	}
	report, err := loadSearchCapProofReport(searchCapProofFlags{
		framesPath:              framesPath,
		candidates:              candidates,
		minCandidateRetainedPct: minCandidateRetainedPct,
		minSearchOutputs:        minSearchOutputs,
		minExtraReducerTokens:   minExtraReducerTokens,
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
	writeSearchCapProofText(stdout, report)
	if !report.GatePassed {
		return 3
	}
	return 0
}

func loadSearchCapProofReport(flags searchCapProofFlags) (searchCapProofReport, error) {
	profile, err := loadSearchCapProfileReport(searchCapProfileFlags{
		framesPath: flags.framesPath,
		candidates: flags.candidates,
		inputPath:  "",
		command:    "",
		workdir:    "",
	})
	if err != nil {
		return searchCapProofReport{}, err
	}
	defaultReplay, err := loadWSSABReplayReport(wssABReplayFlags{
		path:                flags.framesPath,
		failOnLost:          true,
		failOnUpstreamError: true,
		searchCapProofLatch: true,
	})
	if err != nil {
		return searchCapProofReport{}, err
	}
	report := searchCapProofReport{
		Path:                    flags.framesPath,
		Frames:                  profile.Frames,
		SearchOutputs:           profile.SearchOutputs,
		MinCandidateRetainedPct: flags.minCandidateRetainedPct,
		MinSearchOutputs:        flags.minSearchOutputs,
		MinExtraReducerTokens:   flags.minExtraReducerTokens,
		DefaultReplay:           searchCapProofReplaySummaryFrom(defaultReplay),
		GatePassed:              true,
	}
	if profile.SearchOutputs == 0 || len(profile.Profiles) == 0 || !profile.Profiles[0].Applied {
		report.GateFailures = append(report.GateFailures, "expected compactable resolved search output")
	}
	if flags.minSearchOutputs > 0 && profile.SearchOutputs < flags.minSearchOutputs {
		report.GateFailures = append(report.GateFailures, fmt.Sprintf("search outputs %d < min %d", profile.SearchOutputs, flags.minSearchOutputs))
	}
	if !defaultReplay.GatePassed {
		report.GateFailures = append(report.GateFailures, prefixedSearchCapProofFailures("default replay", defaultReplay.GateFailures)...)
	}
	var selected *searchCapProofSelection
	for _, row := range profile.Profiles[1:] {
		candidate := searchCapProofCandidateRow{
			Name:                    row.Name,
			MaxFilesShown:           row.MaxFilesShown,
			MaxMatchesPerFile:       row.MaxMatchesPerFile,
			Applied:                 row.Applied,
			SavedBytesVsDefault:     row.SavedBytesVsDefault,
			MatchRetentionPct:       row.MatchRetentionPct,
			OmittedMatchesVsDefault: row.OmittedMatchesVsDefault,
			GatePassed:              true,
		}
		candidate.GateFailures = append(candidate.GateFailures, searchCapProofProfileFailures(row, flags)...)
		if len(candidate.GateFailures) == 0 {
			replay, err := loadWSSABReplayReport(wssABReplayFlags{
				path:                flags.framesPath,
				failOnLost:          true,
				failOnUpstreamError: true,
				searchCapProofLatch: true,
				searchCapFiles:      row.MaxFilesShown,
				searchCapMatches:    row.MaxMatchesPerFile,
			})
			if err != nil {
				return searchCapProofReport{}, err
			}
			summary := searchCapProofReplaySummaryFrom(replay)
			candidate.Replay = &summary
			candidate.ExtraReducerTokens = replay.ReducerTokensSaved - defaultReplay.ReducerTokensSaved
			if !replay.GatePassed {
				candidate.GateFailures = append(candidate.GateFailures, prefixedSearchCapProofFailures("replay", replay.GateFailures)...)
			}
			if candidate.ExtraReducerTokens < flags.minExtraReducerTokens {
				candidate.GateFailures = append(candidate.GateFailures, fmt.Sprintf("replay reducer tokens %+d vs default; expected at least +%d", candidate.ExtraReducerTokens, flags.minExtraReducerTokens))
			}
		}
		candidate.GatePassed = len(candidate.GateFailures) == 0
		report.Candidates = append(report.Candidates, candidate)
		if candidate.GatePassed && searchCapProofCandidateBeatsSelection(candidate, selected) {
			selected = &searchCapProofSelection{
				Name:                    candidate.Name,
				MaxFilesShown:           candidate.MaxFilesShown,
				MaxMatchesPerFile:       candidate.MaxMatchesPerFile,
				ExtraReducerTokens:      candidate.ExtraReducerTokens,
				SavedBytesVsDefault:     candidate.SavedBytesVsDefault,
				MatchRetentionPct:       candidate.MatchRetentionPct,
				OmittedMatchesVsDefault: candidate.OmittedMatchesVsDefault,
			}
		}
	}
	if selected == nil {
		report.GateFailures = append(report.GateFailures, "no candidate passed profile retention, replay lost=0, upstream-error, and positive replay-savings gates")
	} else if len(report.GateFailures) == 0 {
		report.SelectedCandidate = selected
	}
	report.GatePassed = len(report.GateFailures) == 0
	return report, nil
}

func searchCapProofProfileFailures(row searchCapProfileRow, flags searchCapProofFlags) []string {
	var failures []string
	if !row.Applied {
		failures = append(failures, "profile did not apply")
	}
	if row.SavedBytesVsDefault <= 0 {
		failures = append(failures, "profile did not save more bytes than default")
	}
	if flags.minCandidateRetainedPct > 0 && row.MatchRetentionPct+1e-9 < flags.minCandidateRetainedPct {
		failures = append(failures, fmt.Sprintf("match retention %.2f%% < min %.2f%%", row.MatchRetentionPct, flags.minCandidateRetainedPct))
	}
	return failures
}

func searchCapProofReplaySummaryFrom(report wssABReplayReport) searchCapProofReplaySummary {
	return searchCapProofReplaySummary{
		ReducerTokensSaved:       report.ReducerTokensSaved,
		BytesSaved:               report.BytesSaved,
		SearchRequestTurns:       report.SearchRequestTurns,
		SearchMutatedRequests:    report.SearchMutatedRequests,
		SearchCapturedMutated:    report.SearchCapturedMutated,
		SearchCapProofLatch:      report.SearchCapProofLatch,
		ToolOutputMutation:       report.ToolOutputMutation,
		DeltaToolOutputMutation:  report.DeltaToolOutputMutationLab,
		UpstreamErrorFrames:      report.UpstreamErrorFrames,
		UpstreamInvalidRequests:  report.UpstreamInvalidRequests,
		UpstreamHTTP400Errors:    report.UpstreamHTTP400Errors,
		UpstreamResponseFailures: report.UpstreamResponseFailures,
		Lost:                     report.Lost,
		GatePassed:               report.GatePassed,
	}
}

func searchCapProofCandidateBeatsSelection(candidate searchCapProofCandidateRow, selected *searchCapProofSelection) bool {
	if selected == nil {
		return true
	}
	if candidate.ExtraReducerTokens != selected.ExtraReducerTokens {
		return candidate.ExtraReducerTokens > selected.ExtraReducerTokens
	}
	if candidate.SavedBytesVsDefault != selected.SavedBytesVsDefault {
		return candidate.SavedBytesVsDefault > selected.SavedBytesVsDefault
	}
	return candidate.MatchRetentionPct > selected.MatchRetentionPct
}

func prefixedSearchCapProofFailures(prefix string, failures []string) []string {
	if len(failures) == 0 {
		return nil
	}
	out := make([]string, 0, len(failures))
	for _, failure := range failures {
		out = append(out, prefix+": "+failure)
	}
	return out
}

func writeSearchCapProofText(w io.Writer, report searchCapProofReport) {
	fmt.Fprintf(w, "=== Search Cap Proof: %s ===\n", report.Path)
	fmt.Fprintf(w, "frames:         %d\n", report.Frames)
	fmt.Fprintf(w, "search outputs: %d\n", report.SearchOutputs)
	if report.MinCandidateRetainedPct > 0 {
		fmt.Fprintf(w, "min retention:  %.2f%%\n", report.MinCandidateRetainedPct)
	}
	if report.MinSearchOutputs > 0 {
		fmt.Fprintf(w, "min outputs:    %d\n", report.MinSearchOutputs)
	}
	if report.MinExtraReducerTokens > 0 {
		fmt.Fprintf(w, "min extra tok:  %d\n", report.MinExtraReducerTokens)
	}
	fmt.Fprintf(w, "default replay: reducer_tokens=%d bytes_saved=%d lost=%d upstream_errors=%d gate=%s\n",
		report.DefaultReplay.ReducerTokensSaved,
		report.DefaultReplay.BytesSaved,
		report.DefaultReplay.Lost,
		report.DefaultReplay.UpstreamErrorFrames,
		passFail(report.DefaultReplay.GatePassed))
	if report.SelectedCandidate != nil {
		fmt.Fprintf(w, "selected candidate: %s (%d/%d, extra reducer tokens %+d, %.2f%% retained)\n",
			report.SelectedCandidate.Name,
			report.SelectedCandidate.MaxFilesShown,
			report.SelectedCandidate.MaxMatchesPerFile,
			report.SelectedCandidate.ExtraReducerTokens,
			report.SelectedCandidate.MatchRetentionPct)
	}
	fmt.Fprintf(w, "gate:           %s\n", passFail(report.GatePassed))
	for _, candidate := range report.Candidates {
		fmt.Fprintf(w, "\n%s candidate:\n", candidate.Name)
		fmt.Fprintf(w, "  caps files/matches: %d / %d\n", candidate.MaxFilesShown, candidate.MaxMatchesPerFile)
		fmt.Fprintf(w, "  profile:            applied=%t saved_vs_default=%+d retained=%.2f%% omitted_matches_vs_default=%+d\n",
			candidate.Applied,
			candidate.SavedBytesVsDefault,
			candidate.MatchRetentionPct,
			candidate.OmittedMatchesVsDefault)
		if candidate.Replay != nil {
			fmt.Fprintf(w, "  replay:             reducer_tokens=%d extra=%+d bytes_saved=%d lost=%d upstream_errors=%d gate=%s\n",
				candidate.Replay.ReducerTokensSaved,
				candidate.ExtraReducerTokens,
				candidate.Replay.BytesSaved,
				candidate.Replay.Lost,
				candidate.Replay.UpstreamErrorFrames,
				passFail(candidate.Replay.GatePassed))
		}
		fmt.Fprintf(w, "  gate:               %s\n", passFail(candidate.GatePassed))
		if len(candidate.GateFailures) > 0 {
			fmt.Fprintln(w, "  gate_failures:")
			for _, failure := range candidate.GateFailures {
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

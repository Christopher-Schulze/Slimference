package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

type searchCapProofReport struct {
	Path                    string                        `json:"path"`
	SocketSeq               uint64                        `json:"socket_seq,omitempty"`
	Frames                  int                           `json:"frames"`
	SearchOutputs           int                           `json:"search_outputs"`
	MinCandidateRetainedPct float64                       `json:"min_candidate_retained_pct,omitempty"`
	MinSearchOutputs        int                           `json:"min_search_outputs,omitempty"`
	MinExtraReducerTokens   int                           `json:"min_extra_reducer_tokens,omitempty"`
	ProductReplay           searchCapProofReplaySummary   `json:"product_replay"`
	DefaultReplay           searchCapProofReplaySummary   `json:"default_replay"`
	DownstreamStateProof    searchCapDownstreamStateProof `json:"downstream_state_proof"`
	SelectedCandidate       *searchCapProofSelection      `json:"selected_candidate,omitempty"`
	Candidates              []searchCapProofCandidateRow  `json:"candidates"`
	GatePassed              bool                          `json:"gate_passed"`
	GateFailures            []string                      `json:"gate_failures,omitempty"`
}

type searchCapProofCandidateRow struct {
	Name                      string                       `json:"name"`
	MaxFilesShown             int                          `json:"max_files_shown"`
	MaxMatchesPerFile         int                          `json:"max_matches_per_file"`
	MinRetainedPct            float64                      `json:"min_retained_pct,omitempty"`
	Applied                   bool                         `json:"applied"`
	SavedBytesVsDefault       int                          `json:"saved_bytes_vs_default"`
	MatchRetentionPct         float64                      `json:"match_retention_pct"`
	OmittedMatchesVsDefault   int                          `json:"omitted_matches_vs_default"`
	ExtraReducerTokens        int                          `json:"extra_reducer_tokens,omitempty"`
	ProductExtraReducerTokens int                          `json:"product_extra_reducer_tokens,omitempty"`
	ProductExtraBytes         int                          `json:"product_extra_bytes,omitempty"`
	Replay                    *searchCapProofReplaySummary `json:"replay,omitempty"`
	GatePassed                bool                         `json:"gate_passed"`
	GateFailures              []string                     `json:"gate_failures,omitempty"`
}

type searchCapProofSelection struct {
	Name                      string  `json:"name"`
	MaxFilesShown             int     `json:"max_files_shown"`
	MaxMatchesPerFile         int     `json:"max_matches_per_file"`
	MinRetainedPct            float64 `json:"min_retained_pct,omitempty"`
	ExtraReducerTokens        int     `json:"extra_reducer_tokens"`
	ProductExtraReducerTokens int     `json:"product_extra_reducer_tokens,omitempty"`
	ProductExtraBytes         int     `json:"product_extra_bytes,omitempty"`
	SavedBytesVsDefault       int     `json:"saved_bytes_vs_default"`
	MatchRetentionPct         float64 `json:"match_retention_pct"`
	OmittedMatchesVsDefault   int     `json:"omitted_matches_vs_default"`
}

type searchCapProofReplaySummary struct {
	ReducerTokensSaved       int     `json:"reducer_tokens_saved"`
	BytesSaved               int     `json:"bytes_saved"`
	SearchRequestTurns       int     `json:"search_request_turns"`
	SearchMutatedRequests    int     `json:"search_mutated_requests"`
	SearchCapturedMutated    int     `json:"search_captured_mutated_requests,omitempty"`
	SearchCapProofLatch      bool    `json:"search_cap_proof_latch_enabled,omitempty"`
	SearchCapMinRetainedPct  float64 `json:"search_cap_min_retained_pct,omitempty"`
	ToolOutputMutation       bool    `json:"tool_output_mutation_enabled"`
	DeltaToolOutputMutation  bool    `json:"delta_tool_output_mutation_proof_enabled"`
	UpstreamErrorFrames      int     `json:"upstream_error_frames"`
	UpstreamInvalidRequests  int     `json:"upstream_invalid_request_errors"`
	UpstreamHTTP400Errors    int     `json:"upstream_http_400_errors"`
	UpstreamResponseFailures int     `json:"upstream_response_failed_frames"`
	Lost                     int     `json:"lost"`
	GatePassed               bool    `json:"gate_passed"`
}

type searchCapDownstreamStateProof struct {
	MutatedSearchOutputCandidates  int                     `json:"mutated_search_output_candidates"`
	MutatedDeltaCandidates         int                     `json:"mutated_delta_candidates"`
	MutatedFullHistoryCandidates   int                     `json:"mutated_full_history_candidates"`
	CandidatesWithCleanCurrent     int                     `json:"candidates_with_clean_current_turn"`
	CandidatesWithFollowingTurn    int                     `json:"candidates_with_following_turn"`
	CandidatesWithCleanFollowing   int                     `json:"candidates_with_clean_following"`
	CandidatesPassing              int                     `json:"candidates_passing"`
	MissingFollowingTurnCandidates int                     `json:"missing_following_turn_candidates"`
	UnsafeCandidates               int                     `json:"unsafe_candidates"`
	UnprovenCandidates             int                     `json:"unproven_candidates,omitempty"`
	UpstreamErrorFrames            int                     `json:"upstream_error_frames"`
	InvalidRequestErrors           int                     `json:"invalid_request_errors"`
	HTTP400Errors                  int                     `json:"http_400_errors"`
	ResponseFailedFrames           int                     `json:"response_failed_frames"`
	Lost                           int                     `json:"lost"`
	CapturedLocalSavedTokens       int                     `json:"captured_local_saved_tokens_estimate"`
	RetryOrResendExtraTokens       int                     `json:"retry_or_resend_extra_tokens_estimate"`
	NetCapturedLocalSavedTokens    int                     `json:"net_captured_local_saved_tokens_estimate"`
	CandidateProofs                []wssT354CandidateProof `json:"candidate_proofs,omitempty"`
	GatePassed                     bool                    `json:"gate_passed"`
	GateFailures                   []string                `json:"gate_failures,omitempty"`
}

type searchCapProofFlags struct {
	framesPath              string
	socketSeq               uint64
	candidates              []searchCapProfileCandidate
	minCandidateRetainedPct float64
	minSearchOutputs        int
	minExtraReducerTokens   int
}

func runSearchCapProof(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("search-cap-proof", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var framesPath string
	var socketSeq uint64
	var candidates searchCapProfileCandidateFlags
	minCandidateRetainedPct := searchCapReleaseMinRetainedPct
	minSearchOutputs := searchCapReleaseMinSearchOutputs
	minExtraReducerTokens := searchCapReleaseMinExtraReducerTokens
	var jsonOut bool
	fs.StringVar(&framesPath, "frames", "", "Path to WSS frame capture JSONL")
	fs.Uint64Var(&socketSeq, "socket-seq", 0, "Replay only records captured from WSS socket_seq N")
	fs.Var(&candidates, "candidate", "Candidate cap as files:matches; repeatable; defaults to release ladder 30:15,25:15,20:10")
	fs.Float64Var(&minCandidateRetainedPct, "min-candidate-retained-pct", searchCapReleaseMinRetainedPct, "Reject candidates below this match-retention percentage")
	fs.IntVar(&minSearchOutputs, "min-search-outputs", searchCapReleaseMinSearchOutputs, "Reject captures with fewer resolved search outputs")
	fs.IntVar(&minExtraReducerTokens, "min-extra-reducer-tokens", searchCapReleaseMinExtraReducerTokens, "Reject candidates with fewer extra reducer tokens than this")
	fs.BoolVar(&jsonOut, "json", false, "Output JSON")
	if err := fs.Parse(args); err != nil {
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
	if minSearchOutputs < 0 {
		fmt.Fprintln(stderr, "--min-search-outputs must be >= 0")
		return 2
	}
	if minExtraReducerTokens < 0 {
		fmt.Fprintln(stderr, "--min-extra-reducer-tokens must be >= 0")
		return 2
	}
	if fs.NArg() != 0 || strings.TrimSpace(framesPath) == "" {
		fmt.Fprintln(stderr, "Usage: search-cap-proof --frames <frames.jsonl> [--candidate <files:matches>...] [--json]")
		return 2
	}
	report, err := loadSearchCapProofReport(searchCapProofFlags{
		framesPath:              framesPath,
		socketSeq:               socketSeq,
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
	if len(flags.candidates) == 0 {
		flags.candidates = searchCapReleaseCandidates(flags.minCandidateRetainedPct)
	}
	profile, err := loadSearchCapProfileReport(searchCapProfileFlags{
		framesPath:              flags.framesPath,
		socketSeq:               flags.socketSeq,
		candidates:              flags.candidates,
		inputPath:               "",
		command:                 "",
		workdir:                 "",
		minCandidateRetainedPct: flags.minCandidateRetainedPct,
	})
	if err != nil {
		return searchCapProofReport{}, err
	}
	productReplay, err := loadWSSABReplayReport(wssABReplayFlags{
		path:                flags.framesPath,
		socketSeq:           flags.socketSeq,
		failOnLost:          true,
		failOnUpstreamError: true,
	})
	if err != nil {
		return searchCapProofReport{}, err
	}
	defaultReplay, err := loadWSSABReplayReport(wssABReplayFlags{
		path:                    flags.framesPath,
		socketSeq:               flags.socketSeq,
		failOnLost:              true,
		failOnUpstreamError:     true,
		searchCapProofLatch:     true,
		searchCapMinRetainedPct: flags.minCandidateRetainedPct,
	})
	if err != nil {
		return searchCapProofReport{}, err
	}
	downstreamProof, err := loadSearchCapDownstreamStateProof(flags.framesPath, flags.socketSeq)
	if err != nil {
		return searchCapProofReport{}, err
	}
	report := searchCapProofReport{
		Path:                    flags.framesPath,
		SocketSeq:               flags.socketSeq,
		Frames:                  profile.Frames,
		SearchOutputs:           profile.SearchOutputs,
		MinCandidateRetainedPct: flags.minCandidateRetainedPct,
		MinSearchOutputs:        flags.minSearchOutputs,
		MinExtraReducerTokens:   flags.minExtraReducerTokens,
		ProductReplay:           searchCapProofReplaySummaryFrom(productReplay),
		DefaultReplay:           searchCapProofReplaySummaryFrom(defaultReplay),
		DownstreamStateProof:    downstreamProof,
		GatePassed:              true,
	}
	if profile.SearchOutputs == 0 || len(profile.Profiles) == 0 || !profile.Profiles[0].Applied {
		report.GateFailures = append(report.GateFailures, "expected compactable resolved search output")
	}
	if flags.minSearchOutputs > 0 && profile.SearchOutputs < flags.minSearchOutputs {
		report.GateFailures = append(report.GateFailures, fmt.Sprintf("search outputs %d < min %d", profile.SearchOutputs, flags.minSearchOutputs))
	}
	if !productReplay.GatePassed {
		report.GateFailures = append(report.GateFailures, prefixedSearchCapProofFailures("product replay", productReplay.GateFailures)...)
	}
	if !defaultReplay.GatePassed {
		report.GateFailures = append(report.GateFailures, prefixedSearchCapProofFailures("default replay", defaultReplay.GateFailures)...)
	}
	if !downstreamProof.GatePassed {
		report.GateFailures = append(report.GateFailures, prefixedSearchCapProofFailures("downstream_state_proof", downstreamProof.GateFailures)...)
	}
	if downstreamProof.NetCapturedLocalSavedTokens <= 0 {
		report.GateFailures = append(report.GateFailures, fmt.Sprintf("downstream_state_proof: net captured local saved tokens must be positive, got %+d", downstreamProof.NetCapturedLocalSavedTokens))
	}
	var selected *searchCapProofSelection
	for _, row := range profile.Profiles[1:] {
		candidate := searchCapProofCandidateRow{
			Name:                    row.Name,
			MaxFilesShown:           row.MaxFilesShown,
			MaxMatchesPerFile:       row.MaxMatchesPerFile,
			MinRetainedPct:          row.MinRetainedPct,
			Applied:                 row.Applied,
			SavedBytesVsDefault:     row.SavedBytesVsDefault,
			MatchRetentionPct:       row.MatchRetentionPct,
			OmittedMatchesVsDefault: row.OmittedMatchesVsDefault,
			GatePassed:              true,
		}
		candidate.GateFailures = append(candidate.GateFailures, searchCapProofProfileFailures(row, flags)...)
		if len(candidate.GateFailures) == 0 {
			replay, err := loadWSSABReplayReport(wssABReplayFlags{
				path:                    flags.framesPath,
				socketSeq:               flags.socketSeq,
				failOnLost:              true,
				failOnUpstreamError:     true,
				searchCapProofLatch:     true,
				searchCapFiles:          row.MaxFilesShown,
				searchCapMatches:        row.MaxMatchesPerFile,
				searchCapMinRetainedPct: row.MinRetainedPct,
			})
			if err != nil {
				return searchCapProofReport{}, err
			}
			summary := searchCapProofReplaySummaryFrom(replay)
			candidate.Replay = &summary
			candidate.ExtraReducerTokens = replay.ReducerTokensSaved - defaultReplay.ReducerTokensSaved
			candidate.ProductExtraReducerTokens = replay.ReducerTokensSaved - productReplay.ReducerTokensSaved
			candidate.ProductExtraBytes = replay.BytesSaved - productReplay.BytesSaved
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
				Name:                      candidate.Name,
				MaxFilesShown:             candidate.MaxFilesShown,
				MaxMatchesPerFile:         candidate.MaxMatchesPerFile,
				MinRetainedPct:            candidate.MinRetainedPct,
				ExtraReducerTokens:        candidate.ExtraReducerTokens,
				ProductExtraReducerTokens: candidate.ProductExtraReducerTokens,
				ProductExtraBytes:         candidate.ProductExtraBytes,
				SavedBytesVsDefault:       candidate.SavedBytesVsDefault,
				MatchRetentionPct:         candidate.MatchRetentionPct,
				OmittedMatchesVsDefault:   candidate.OmittedMatchesVsDefault,
			}
		}
	}
	if selected == nil {
		if len(report.GateFailures) == 0 {
			selected = searchCapProofDefaultRetentionFloorSelection(profile.Profiles[0], productReplay, defaultReplay, flags)
		}
		if selected == nil {
			report.GateFailures = append(report.GateFailures, "no candidate or default retention-floor latch passed profile retention, replay lost=0, upstream-error, and positive replay-savings gates")
		} else {
			report.SelectedCandidate = selected
		}
	} else if len(report.GateFailures) == 0 {
		report.SelectedCandidate = selected
	}
	report.GatePassed = len(report.GateFailures) == 0
	return report, nil
}

func loadSearchCapDownstreamStateProof(path string, socketSeq uint64) (searchCapDownstreamStateProof, error) {
	frames, err := readWSSABReplayFrames(path)
	if err != nil {
		return searchCapDownstreamStateProof{}, err
	}
	frames = filterWSSABReplayFramesBySocketSeq(frames, socketSeq)
	if len(frames) == 0 {
		if socketSeq > 0 {
			return searchCapDownstreamStateProof{}, fmt.Errorf("no replay frames for socket_seq=%d in %s", socketSeq, path)
		}
		return searchCapDownstreamStateProof{}, fmt.Errorf("replay %s contained no frames", path)
	}
	replay, err := loadWSSABReplayReport(wssABReplayFlags{path: path, socketSeq: socketSeq})
	if err != nil {
		return searchCapDownstreamStateProof{}, err
	}
	upstream := wssABReplayUpstreamDiagnostics(frames)
	turnFrames := wssT354CanonicalTurnFrames(frames)
	mutatedSearchOutputTurns, err := searchCapMutatedSearchOutputTurnMarkers(turnFrames)
	if err != nil {
		return searchCapDownstreamStateProof{}, err
	}
	turns := wssT354TurnsFromFrames(turnFrames)
	proof := searchCapDownstreamStateProof{
		UpstreamErrorFrames:  upstream.ErrorFrames,
		InvalidRequestErrors: upstream.InvalidRequestErrors,
		HTTP400Errors:        upstream.HTTP400Errors,
		ResponseFailedFrames: upstream.ResponseFailedFrames,
		Lost:                 replay.Lost,
		GatePassed:           true,
	}
	if len(mutatedSearchOutputTurns) != len(turns) {
		proof.GateFailures = append(proof.GateFailures, fmt.Sprintf("turn marker mismatch: search_markers=%d request_turns=%d", len(mutatedSearchOutputTurns), len(turns)))
	}
	for i, turn := range turns {
		if i >= len(mutatedSearchOutputTurns) || !mutatedSearchOutputTurns[i] {
			continue
		}
		candidate := wssT354CandidateProof{
			TurnIndex:                     i,
			Shape:                         turn.shape,
			PreviousResponseID:            turn.previousResponseID,
			ToolOutputs:                   turn.toolOutputs,
			CustomToolOutputs:             turn.customToolOutputs,
			RequestTokensEstimate:         turn.requestTokensEstimate,
			CapturedOriginalRequestTokens: turn.capturedOriginalRequestTokens,
			CapturedLocalSavedTokens:      turn.capturedLocalSavedTokens,
			CurrentTurnClean:              wssT354TurnClean(turn),
			CurrentTurnHealth:             wssT354TurnHealthFromTurn(turn),
		}
		following, hasFollowing := wssT354NextLogicalTurn(turns, i)
		if hasFollowing {
			followingHealth := wssT354TurnHealthFromTurn(following)
			candidate.FollowingTurnPresent = true
			candidate.FollowingTurnShape = following.shape
			candidate.FollowingRequestTokensEstimate = following.requestTokensEstimate
			candidate.FollowingTurnClean = wssT354TurnClean(following)
			candidate.FollowingTurnHealth = &followingHealth
		}
		candidate.RetryOrResendExtraTokens = wssT354RetryOrResendExtraTokens(turn, following, hasFollowing)
		if candidate.Shape != "delta" && candidate.Shape != "full_history" {
			candidate.BlockReasons = append(candidate.BlockReasons, "unsupported_shape_"+candidate.Shape)
		}
		candidate.BlockReasons = append(candidate.BlockReasons, wssT354CandidateBlockReasons(candidate)...)
		candidate.BlockReasons = compactStringList(candidate.BlockReasons)
		candidate.UnlockProofPassing = len(candidate.BlockReasons) == 0
		searchCapAccumulateDownstreamCandidate(&proof, candidate)
	}
	proof.NetCapturedLocalSavedTokens = proof.CapturedLocalSavedTokens - proof.RetryOrResendExtraTokens
	proof.GateFailures = append(proof.GateFailures, searchCapDownstreamStateGateFailures(proof)...)
	proof.GateFailures = compactStringList(proof.GateFailures)
	proof.GatePassed = len(proof.GateFailures) == 0
	return proof, nil
}

func searchCapAccumulateDownstreamCandidate(proof *searchCapDownstreamStateProof, candidate wssT354CandidateProof) {
	proof.MutatedSearchOutputCandidates++
	switch candidate.Shape {
	case "delta":
		proof.MutatedDeltaCandidates++
	case "full_history":
		proof.MutatedFullHistoryCandidates++
	}
	if candidate.CurrentTurnClean {
		proof.CandidatesWithCleanCurrent++
	}
	if candidate.FollowingTurnPresent {
		proof.CandidatesWithFollowingTurn++
	} else {
		proof.MissingFollowingTurnCandidates++
	}
	if candidate.FollowingTurnClean {
		proof.CandidatesWithCleanFollowing++
	}
	if candidate.UnlockProofPassing {
		proof.CandidatesPassing++
	} else if searchCapDownstreamCandidateHasSafetyFailure(candidate) {
		proof.UnsafeCandidates++
	} else {
		proof.UnprovenCandidates++
	}
	proof.CapturedLocalSavedTokens += candidate.CapturedLocalSavedTokens
	proof.RetryOrResendExtraTokens += candidate.RetryOrResendExtraTokens
	proof.CandidateProofs = append(proof.CandidateProofs, candidate)
}

func searchCapDownstreamStateGateFailures(proof searchCapDownstreamStateProof) []string {
	var failures []string
	if proof.MutatedSearchOutputCandidates == 0 {
		failures = append(failures, "no live mutated search-output downstream candidate observed")
	}
	if proof.UpstreamErrorFrames > 0 {
		failures = append(failures, fmt.Sprintf("upstream_error_frames=%d", proof.UpstreamErrorFrames))
	}
	if proof.InvalidRequestErrors > 0 {
		failures = append(failures, fmt.Sprintf("invalid_request=%d", proof.InvalidRequestErrors))
	}
	if proof.HTTP400Errors > 0 {
		failures = append(failures, fmt.Sprintf("http_400=%d", proof.HTTP400Errors))
	}
	if proof.ResponseFailedFrames > 0 {
		failures = append(failures, fmt.Sprintf("response_failed=%d", proof.ResponseFailedFrames))
	}
	if proof.Lost > 0 {
		failures = append(failures, fmt.Sprintf("lost=%d", proof.Lost))
	}
	if proof.MutatedSearchOutputCandidates > 0 && proof.CandidatesPassing == 0 {
		failures = append(failures, "no live mutated search-output candidate has clean current and following turn")
	}
	for _, candidate := range proof.CandidateProofs {
		for _, reason := range candidate.BlockReasons {
			if !searchCapDownstreamCandidateReasonIsSafetyFailure(candidate, reason, proof.CandidatesPassing) {
				continue
			}
			failures = append(failures, fmt.Sprintf("candidate_%d:%s", candidate.TurnIndex, reason))
		}
	}
	return failures
}

func searchCapDownstreamCandidateReasonIsSafetyFailure(candidate wssT354CandidateProof, reason string, passingCandidates int) bool {
	return wssT354CandidateReasonIsSafetyFailure(candidate, reason, passingCandidates)
}

func searchCapDownstreamCandidateHasSafetyFailure(candidate wssT354CandidateProof) bool {
	return wssT354CandidateHasSafetyFailure(candidate)
}

func searchCapDownstreamCandidateReasonHasSafetyFailure(candidate wssT354CandidateProof, reason string) bool {
	return wssT354CandidateReasonHasSafetyFailure(candidate, reason)
}

func searchCapMutatedSearchOutputTurnMarkers(frames []proxy.WSSABReplayFrame) ([]bool, error) {
	toolUses := make(map[string]searchCapProfileToolUse)
	var markers []bool
	for i, frame := range frames {
		switch frame.Direction {
		case wsmitm.DirServerToClient:
			rememberSearchCapProfileToolUses(toolUses, frame.Payload)
		case wsmitm.DirClientToServer:
			body, root, ok := wssT354RequestBody(frame.Payload)
			if !ok || !wssT354LooksLikeRequestBody(root) {
				continue
			}
			rememberSearchCapProfileToolUses(toolUses, body)
			markers = append(markers, frame.Mutated && searchCapRequestHasNamedSearchFunctionOutput(toolUses, body))
		default:
			return nil, fmt.Errorf("frame %d has unsupported direction %q", i, frame.Direction)
		}
	}
	return markers, nil
}

func searchCapRequestHasNamedSearchFunctionOutput(toolUses map[string]searchCapProfileToolUse, body []byte) bool {
	for _, item := range searchCapProfileInputItems(body) {
		if strings.TrimSpace(rawStringField(item, "type")) != "function_call_output" {
			continue
		}
		callID := strings.TrimSpace(rawStringField(item, "call_id"))
		if callID == "" {
			callID = strings.TrimSpace(rawStringField(item, "id"))
		}
		toolUse := toolUses[callID]
		if filter.NormalizeSearchCommandLine(toolUse.command, toolUse.workdir) != "" {
			return true
		}
	}
	return false
}

func searchCapProofDefaultRetentionFloorSelection(row searchCapProfileRow, productReplay, defaultReplay wssABReplayReport, flags searchCapProofFlags) *searchCapProofSelection {
	if !row.Applied || !productReplay.GatePassed || !defaultReplay.GatePassed || !searchCapReplayUsesProductLatch(searchCapProofReplaySummaryFrom(defaultReplay)) {
		return nil
	}
	if flags.minCandidateRetainedPct > 0 && row.MatchRetentionPct+1e-9 < flags.minCandidateRetainedPct {
		return nil
	}
	extraTokens := defaultReplay.ReducerTokensSaved - productReplay.ReducerTokensSaved
	if extraTokens < flags.minExtraReducerTokens {
		return nil
	}
	extraBytes := defaultReplay.BytesSaved - productReplay.BytesSaved
	if extraTokens <= 0 && extraBytes <= 0 {
		return nil
	}
	return &searchCapProofSelection{
		Name:                      "default_retention_floor",
		MaxFilesShown:             row.MaxFilesShown,
		MaxMatchesPerFile:         row.MaxMatchesPerFile,
		MinRetainedPct:            row.MinRetainedPct,
		ExtraReducerTokens:        extraTokens,
		ProductExtraReducerTokens: extraTokens,
		ProductExtraBytes:         extraBytes,
		SavedBytesVsDefault:       extraBytes,
		MatchRetentionPct:         row.MatchRetentionPct,
		OmittedMatchesVsDefault:   row.OmittedMatchesVsDefault,
	}
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
		SearchCapMinRetainedPct:  report.SearchCapMinRetainedPct,
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
	fmt.Fprintf(w, "product replay: reducer_tokens=%d bytes_saved=%d lost=%d upstream_errors=%d gate=%s\n",
		report.ProductReplay.ReducerTokensSaved,
		report.ProductReplay.BytesSaved,
		report.ProductReplay.Lost,
		report.ProductReplay.UpstreamErrorFrames,
		passFail(report.ProductReplay.GatePassed))
	fmt.Fprintf(w, "default replay: reducer_tokens=%d bytes_saved=%d lost=%d upstream_errors=%d gate=%s\n",
		report.DefaultReplay.ReducerTokensSaved,
		report.DefaultReplay.BytesSaved,
		report.DefaultReplay.Lost,
		report.DefaultReplay.UpstreamErrorFrames,
		passFail(report.DefaultReplay.GatePassed))
	fmt.Fprintf(w, "downstream proof: mutated_search_candidates=%d passing=%d lost=%d upstream_errors=%d gate=%s\n",
		report.DownstreamStateProof.MutatedSearchOutputCandidates,
		report.DownstreamStateProof.CandidatesPassing,
		report.DownstreamStateProof.Lost,
		report.DownstreamStateProof.UpstreamErrorFrames,
		passFail(report.DownstreamStateProof.GatePassed))
	if report.SelectedCandidate != nil {
		fmt.Fprintf(w, "selected candidate: %s (%d/%d, min %.2f%%, extra reducer tokens %+d, product extra %+d, %.2f%% retained)\n",
			report.SelectedCandidate.Name,
			report.SelectedCandidate.MaxFilesShown,
			report.SelectedCandidate.MaxMatchesPerFile,
			report.SelectedCandidate.MinRetainedPct,
			report.SelectedCandidate.ExtraReducerTokens,
			report.SelectedCandidate.ProductExtraReducerTokens,
			report.SelectedCandidate.MatchRetentionPct)
	}
	fmt.Fprintf(w, "gate:           %s\n", passFail(report.GatePassed))
	for _, candidate := range report.Candidates {
		fmt.Fprintf(w, "\n%s candidate:\n", candidate.Name)
		if candidate.MinRetainedPct > 0 {
			fmt.Fprintf(w, "  caps files/matches: %d / %d (min %.2f%% retained)\n", candidate.MaxFilesShown, candidate.MaxMatchesPerFile, candidate.MinRetainedPct)
		} else {
			fmt.Fprintf(w, "  caps files/matches: %d / %d\n", candidate.MaxFilesShown, candidate.MaxMatchesPerFile)
		}
		fmt.Fprintf(w, "  profile:            applied=%t saved_vs_default=%+d retained=%.2f%% omitted_matches_vs_default=%+d\n",
			candidate.Applied,
			candidate.SavedBytesVsDefault,
			candidate.MatchRetentionPct,
			candidate.OmittedMatchesVsDefault)
		if candidate.Replay != nil {
			fmt.Fprintf(w, "  replay:             reducer_tokens=%d extra=%+d product_extra=%+d bytes_saved=%d lost=%d upstream_errors=%d gate=%s\n",
				candidate.Replay.ReducerTokensSaved,
				candidate.ExtraReducerTokens,
				candidate.ProductExtraReducerTokens,
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

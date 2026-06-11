package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/chunkdedup"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/readcache"
	"github.com/Christopher-Schulze/Slimference/internal/savingspolicy"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

type proxyLayer0Mechanism string

const (
	proxyLayer0MechanismReadDelta     proxyLayer0Mechanism = "read_delta"
	proxyLayer0MechanismCapturedOut   proxyLayer0Mechanism = "captured_output"
	proxyLayer0MechanismCodexEnvelope proxyLayer0Mechanism = "codex_exec_envelope"
	proxyLayer0MechanismRepeatedOut   proxyLayer0Mechanism = "repeated_tool_output"
	proxyLayer0MechanismChunkDedup    proxyLayer0Mechanism = "chunk_dedup"
)

type proxyLayer0CacheAction string

const (
	proxyLayer0CacheHit  proxyLayer0CacheAction = "hit"
	proxyLayer0CacheMiss proxyLayer0CacheAction = "miss"
)

type proxyLayer0CacheEvent struct {
	Mechanism savingspolicy.CodexMechanism
	Action    proxyLayer0CacheAction
	Reason    string
}

type codexLayer0Route string

const (
	codexLayer0RouteUnspecified codexLayer0Route = ""
	codexLayer0RouteHTTP        codexLayer0Route = "http"
	codexLayer0RouteWSSPhaseF   codexLayer0Route = "wss_phasef"
)

type codexLayer0Request struct {
	Route                     codexLayer0Route
	Messages                  []types.Message
	SessionID                 string
	TurnID                    string
	RememberedToolUse         map[string]types.ContentBlock
	SuppressedToolKey         map[string]struct{}
	RecentFullPassTurns       int
	ChunkDedupEnabled         bool
	ExplicitChunkDedup        bool
	ChunkDedupProof           savingspolicy.CodexProof
	ChunkDedupMinBytes        int
	ChunkDedupMaxRefPct       int
	ChunkStore                *chunkdedup.Store
	PolicyMode                string
	ArchiveRecovery           bool
	RecentEditUncertainty     bool
	HostBudgetExceeded        bool
	LatencyBudgetExceeded     bool
	ChunkIntegrityBudgetHit   bool
	StructuredMutationBlocked bool
}

type codexLayer0Result struct {
	Messages []types.Message
	Stats    proxyLayer0Stats
}

type codexChunkDedupSettings struct {
	Store           *chunkdedup.Store
	Enabled         bool
	MinBytes        int
	MaxRefPct       int
	Explicit        bool
	PolicyMode      string
	ArchiveRecovery bool
	Proof           savingspolicy.CodexProof
}

type proxyLayer0Stats struct {
	Route                   codexLayer0Route
	ToolResultBlocks        int
	ToolUseUnresolvedBlocks int
	CommandResolvedBlocks   int
	CommandUnresolvedBlocks int
	ReadDeltaAttempts       int
	ReadDeltaMisses         int
	TokensSaved             int
	BlocksModified          int
	ReadDeltaBlocks         int
	CapturedOutputBlocks    int
	CodexExecEnvelopeBlocks int
	RepeatedOutputBlocks    int
	ChunkDedupBlocks        int
	ChunkDedupReferences    int
	ChunkDedupRefBytes      int
	ChunkDedupInputBytes    int
	ReadDeltaKeys           []string
	PolicyDecisions         []savingspolicy.CodexMechanismDecision
	CacheEvents             []proxyLayer0CacheEvent
	EvidenceDecisions       []evidence.BlockDecision
	TotalLatencyNs          int64
	ReadDeltaLatencyNs      int64
	FilterLatencyNs         int64
	RepeatedOutputLatencyNs int64
	ChunkDedupLatencyNs     int64
}

func (s proxyLayer0Stats) finish(start time.Time) proxyLayer0Stats {
	if !start.IsZero() {
		s.TotalLatencyNs += time.Since(start).Nanoseconds()
	}
	return s
}

func (s proxyLayer0Stats) withoutSavings() proxyLayer0Stats {
	s.TokensSaved = 0
	s.BlocksModified = 0
	s.ReadDeltaBlocks = 0
	s.CapturedOutputBlocks = 0
	s.CodexExecEnvelopeBlocks = 0
	s.RepeatedOutputBlocks = 0
	s.ChunkDedupBlocks = 0
	s.ChunkDedupReferences = 0
	s.ChunkDedupRefBytes = 0
	s.ChunkDedupInputBytes = 0
	s.ReadDeltaKeys = nil
	s.PolicyDecisions = nil
	s.CacheEvents = nil
	s.EvidenceDecisions = nil
	return s
}

func applyProxyLayer0(messages []types.Message) ([]types.Message, int) {
	return applyProxyLayer0WithSession(messages, "")
}

func applyProxyLayer0WithSession(messages []types.Message, sessionID string) ([]types.Message, int) {
	return applyProxyLayer0WithSessionAndToolUses(messages, sessionID, nil)
}

func applyProxyLayer0WithSessionAndToolUses(messages []types.Message, sessionID string, rememberedToolUses map[string]types.ContentBlock) ([]types.Message, int) {
	out, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, sessionID, rememberedToolUses)
	return out, stats.TokensSaved
}

func applyProxyLayer0WithSessionAndToolUsesDetailed(messages []types.Message, sessionID string, rememberedToolUses map[string]types.ContentBlock) ([]types.Message, proxyLayer0Stats) {
	result := reduceCodexLayer0(codexLayer0Request{
		Route:             codexLayer0RouteUnspecified,
		Messages:          messages,
		SessionID:         sessionID,
		RememberedToolUse: rememberedToolUses,
	})
	return result.Messages, result.Stats
}

func (p *Proxy) codexChunkDedupSettings() codexChunkDedupSettings {
	if p == nil || p.config == nil || p.codexChunkDedup == nil {
		return codexChunkDedupSettings{}
	}
	or := p.config.Compression.OutputReduce
	mode := or.CodexSavingsPolicyMode
	policyMode := savingspolicy.NormalizeCodexMode(mode)
	proof := savingspolicy.NormalizeCodexProof(or.CodexChunkDedupProofLevel)
	archiveRecovery := or.ArchiveRecoveryNoteEnabled || policyMode == savingspolicy.CodexModeAuto || policyMode == savingspolicy.CodexModeMax
	if !archiveRecovery {
		return codexChunkDedupSettings{
			MaxRefPct:       or.CodexChunkDedupMaxReferencePercent,
			Explicit:        or.CodexChunkDedupEnabled,
			PolicyMode:      mode,
			ArchiveRecovery: false,
			Proof:           proof,
		}
	}
	chunkAvailable := or.CodexChunkDedupEnabled || policyMode == savingspolicy.CodexModeAuto || policyMode == savingspolicy.CodexModeMax
	if !chunkAvailable {
		return codexChunkDedupSettings{
			MaxRefPct:       or.CodexChunkDedupMaxReferencePercent,
			Explicit:        or.CodexChunkDedupEnabled,
			PolicyMode:      mode,
			ArchiveRecovery: archiveRecovery,
			Proof:           proof,
		}
	}
	return codexChunkDedupSettings{
		Store:           p.codexChunkDedup,
		Enabled:         true,
		MinBytes:        or.CodexChunkDedupMinBytes,
		MaxRefPct:       or.CodexChunkDedupMaxReferencePercent,
		Explicit:        or.CodexChunkDedupEnabled,
		PolicyMode:      mode,
		ArchiveRecovery: archiveRecovery,
		Proof:           proof,
	}
}

func (p *Proxy) codexHTTPChunkDedupSettings() codexChunkDedupSettings {
	settings := p.codexChunkDedupSettings()
	settings.Store = nil
	settings.Enabled = false
	settings.Explicit = false
	settings.ArchiveRecovery = false
	return settings
}

func reduceCodexLayer0(req codexLayer0Request) codexLayer0Result {
	started := time.Now()
	toolUses := proxyToolUseIndex(req.Messages)
	for id, use := range req.RememberedToolUse {
		if _, ok := toolUses[id]; !ok {
			toolUses[id] = use
		}
	}
	var out []types.Message
	stats := proxyLayer0Stats{Route: req.Route}
	recentEditUncertainty := req.RecentEditUncertainty || len(proxyEditedPathsFromMessages(req.Messages, req.RememberedToolUse)) > 0

	for msgIdx, msg := range req.Messages {
		for blockIdx, block := range msg.Content {
			if block.Type != "tool_result" {
				continue
			}
			stats.ToolResultBlocks++
			use, toolUseResolved := proxyResolveToolUseDetailed(block, toolUses)
			commandLine := proxyLayer0CommandLine(use)
			if commandLine == "" {
				commandLine = proxyInferCommandLineFromToolResult(block.Text)
				if commandLine == "" {
					stats.CommandUnresolvedBlocks++
					if !toolUseResolved {
						stats.ToolUseUnresolvedBlocks++
					}
					continue
				}
			}
			stats.CommandResolvedBlocks++
			if proxyCommandLineInvokesReconc(commandLine) {
				continue
			}
			toolKey := proxyLayer0QualityToolKeyForUse(use, commandLine)
			beforeTokens := -1
			countBeforeTokens := func() int {
				if beforeTokens < 0 {
					// Codex bills in o200k_base. Keep exact counting on the
					// real mutation/savings path, but do not load the heavy
					// encoder for no-op cache misses.
					beforeTokens = tokens.ForProvider(types.CodexChatGPT).CountString(block.Text)
				}
				return beforeTokens
			}
			countCandidateTokens := func(text string) int {
				return tokens.ForProvider(types.CodexChatGPT).CountString(text)
			}
			readCtx := proxyReadFileContext(req.SessionID, commandLine)
			readReq := readRequestFromCommandLine(commandLine)
			readCommand := readReq.FilePath != ""
			workload := savingspolicy.CodexWorkloadCommand
			if readCommand {
				workload = savingspolicy.CodexWorkloadRead
			} else if filter.SearchOutputKeyFromCommandLine(commandLine) != "" {
				workload = savingspolicy.CodexWorkloadSearch
			}
			// Under the latency latch every block loosens to full pass, so the
			// O(output) search-risk scan and the chunk-store budget probe would
			// be pure overhead that keeps the latch from recovering.
			wssSearchOutputBlocked := !req.LatencyBudgetExceeded && req.Route == codexLayer0RouteWSSPhaseF && proxyWSSSearchOutputRisk(commandLine, block.Text, workload)
			chunkIntegrityBudgetHit := req.ChunkIntegrityBudgetHit
			if !req.LatencyBudgetExceeded && !chunkIntegrityBudgetHit && req.ChunkStore != nil {
				chunkIntegrityBudgetHit = !req.ChunkStore.ReferenceBudgetAvailableAfterInput(req.SessionID, len(block.Text), req.ChunkDedupMinBytes)
			}
			_, postCollapseReRead := req.SuppressedToolKey[toolKey]
			policy := savingspolicy.DecideCodexToolOutput(savingspolicy.CodexToolOutputInput{
				Mode:                     req.PolicyMode,
				Route:                    savingspolicy.CodexRoute(req.Route),
				Workload:                 workload,
				ArchiveRecoveryAvailable: req.ArchiveRecovery && req.ChunkDedupEnabled && req.ChunkStore != nil,
				ExplicitChunkDedup:       req.ExplicitChunkDedup,
				ChunkProof:               req.ChunkDedupProof,
				OutputBytes:              len(block.Text),
				ChunkMinBytes:            req.ChunkDedupMinBytes,
				IsRead:                   readCommand,
				RecentlyEdited:           readCtx.RecentlyEdited,
				PostCollapseReRead:       postCollapseReRead && toolKey != "",
				RecentEditUncertainty:    recentEditUncertainty && !readCtx.RecentlyEdited,
				HostBudgetExceeded:       req.HostBudgetExceeded,
				LatencyBudgetExceeded:    req.LatencyBudgetExceeded,
				ChunkIntegrityBudgetHit:  chunkIntegrityBudgetHit,
			})
			stats.PolicyDecisions = append(stats.PolicyDecisions, policy.Mechanisms...)
			if policy.Loosened || (!policy.ReadDelta && !policy.RepeatedOutput && !policy.ChunkDedup) {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, "", proxyLayer0MechanismCapturedOut, evidence.ActionFullPass, policy.Reason, 0, 0, workload))
				continue
			}
			readDeltaAttempted := policy.ReadDelta && readDeltaEligible(req.SessionID, commandLine)
			if readDeltaAttempted {
				stats.ReadDeltaAttempts++
			}
			afterText, changed := "", false
			mechanism := proxyLayer0MechanismReadDelta
			chunkReport := chunkdedup.EncodeResult{}
			chunkAllowed := chunkDedupAllowedForCommand(commandLine, readCommand)
			if policy.ReadDelta {
				var cacheReason string
				latencyStart := time.Now()
				afterText, changed, cacheReason, _ = compactProxyReadDeltaWithDecision(req.SessionID, req.TurnID, commandLine, block.Text, readCtx, req.RecentFullPassTurns)
				stats.ReadDeltaLatencyNs += time.Since(latencyStart).Nanoseconds()
				if readDeltaAttempted {
					action := proxyLayer0CacheMiss
					if changed {
						action = proxyLayer0CacheHit
					}
					stats.CacheEvents = append(stats.CacheEvents, proxyLayer0CacheEvent{
						Mechanism: savingspolicy.CodexMechanismReadDelta,
						Action:    action,
						Reason:    cacheReason,
					})
				}
				if readDeltaAttempted && !changed {
					stats.ReadDeltaMisses++
				}
			}
			if readCommand && !changed {
				if policy.ChunkDedup && chunkAllowed {
					latencyStart := time.Now()
					afterText, changed, mechanism, chunkReport = compactProxyChunkDedup(req.ChunkStore, req.SessionID, block.Text, req.ChunkDedupMinBytes, req.ChunkDedupMaxRefPct)
					stats.ChunkDedupLatencyNs += time.Since(latencyStart).Nanoseconds()
				}
				if !changed {
					continue
				}
			}
			preFilterRepeated := false
			if !readCommand && workload == savingspolicy.CodexWorkloadSearch && !wssSearchOutputBlocked && policy.RepeatedOutput {
				preFilterRepeated = true
				latencyStart := time.Now()
				repeatedText, repeated, cacheReason := compactProxyRepeatedToolOutputWithKeyDetailed(req.SessionID, toolKey, commandLine, block.Text)
				stats.RepeatedOutputLatencyNs += time.Since(latencyStart).Nanoseconds()
				action := proxyLayer0CacheMiss
				if repeated {
					action = proxyLayer0CacheHit
				}
				stats.CacheEvents = append(stats.CacheEvents, proxyLayer0CacheEvent{
					Mechanism: savingspolicy.CodexMechanismRepeatedOutput,
					Action:    action,
					Reason:    cacheReason,
				})
				if repeated {
					afterText = repeatedText
					changed = true
					mechanism = proxyLayer0MechanismRepeatedOut
				}
			}
			if wssSearchOutputBlocked {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, "", proxyLayer0MechanismCapturedOut, evidence.ActionFullPass, "wss_search_output_risk_gate", 0, 0, workload))
			}
			if !changed && !wssSearchOutputBlocked {
				latencyStart := time.Now()
				afterText, changed, mechanism = compactProxyLayer0TextDetailed(commandLine, block.Text, readCtx)
				stats.FilterLatencyNs += time.Since(latencyStart).Nanoseconds()
			}
			if changed && req.StructuredMutationBlocked &&
				(mechanism == proxyLayer0MechanismCapturedOut || mechanism == proxyLayer0MechanismCodexEnvelope) {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, afterText, mechanism, evidence.ActionFullPass, "wss_stateful_structured_mutation_guard", 0, 0, workload))
				changed = false
				afterText = ""
			}
			if changed && req.Route == codexLayer0RouteWSSPhaseF &&
				(mechanism == proxyLayer0MechanismCapturedOut || mechanism == proxyLayer0MechanismCodexEnvelope) {
				archivedText, archived := archiveProxyCapturedOutput(req.SessionID, commandLine, afterText, block.Text)
				if !archived {
					changed = false
				} else {
					afterText = archivedText
				}
			}
			candidateText := block.Text
			candidateEligible := true
			if changed {
				candidateText = afterText
				candidateEligible = countCandidateTokens(candidateText) < countBeforeTokens()
			}
			if !readCommand && !preFilterRepeated && !wssSearchOutputBlocked && candidateEligible && policy.RepeatedOutput {
				latencyStart := time.Now()
				repeatedText, repeated, cacheReason := compactProxyRepeatedToolOutputWithKeyDetailed(req.SessionID, toolKey, commandLine, candidateText)
				stats.RepeatedOutputLatencyNs += time.Since(latencyStart).Nanoseconds()
				action := proxyLayer0CacheMiss
				if repeated {
					action = proxyLayer0CacheHit
				}
				stats.CacheEvents = append(stats.CacheEvents, proxyLayer0CacheEvent{
					Mechanism: savingspolicy.CodexMechanismRepeatedOutput,
					Action:    action,
					Reason:    cacheReason,
				})
				if repeated {
					afterText = repeatedText
					changed = true
					mechanism = proxyLayer0MechanismRepeatedOut
				}
			}
			if !changed && policy.ChunkDedup && chunkAllowed {
				latencyStart := time.Now()
				afterText, changed, mechanism, chunkReport = compactProxyChunkDedup(req.ChunkStore, req.SessionID, candidateText, req.ChunkDedupMinBytes, req.ChunkDedupMaxRefPct)
				stats.ChunkDedupLatencyNs += time.Since(latencyStart).Nanoseconds()
			}
			if !changed {
				continue
			}
			before := countBeforeTokens()
			afterTokens := countCandidateTokens(afterText)
			if afterTokens < before {
				if out == nil {
					out = cloneMessages(req.Messages)
				}
				out[msgIdx].Content[blockIdx].Text = afterText
				stats.TokensSaved += before - afterTokens
				stats.BlocksModified++
				switch mechanism {
				case proxyLayer0MechanismReadDelta:
					stats.ReadDeltaBlocks++
					if toolKey != "" {
						stats.ReadDeltaKeys = append(stats.ReadDeltaKeys, toolKey)
					}
				case proxyLayer0MechanismCodexEnvelope:
					stats.CodexExecEnvelopeBlocks++
				case proxyLayer0MechanismRepeatedOut:
					stats.RepeatedOutputBlocks++
				case proxyLayer0MechanismChunkDedup:
					stats.ChunkDedupBlocks++
					stats.ChunkDedupReferences += chunkReport.ReferenceCount
					stats.ChunkDedupRefBytes += chunkReport.ReferencedBytes
					stats.ChunkDedupInputBytes += len(candidateText)
				default:
					stats.CapturedOutputBlocks++
				}
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, afterText, mechanism, evidence.ActionApplied, "positive_net_savings", before, afterTokens, workload))
			} else {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, afterText, mechanism, evidence.ActionSkipped, "negative_or_zero_net_savings", before, afterTokens, workload))
			}
		}
	}

	if out == nil {
		return codexLayer0Result{Messages: req.Messages, Stats: stats.finish(started)}
	}
	return codexLayer0Result{Messages: out, Stats: stats.finish(started)}
}

func proxyLayer0EvidenceDecision(commandLine string, beforeText string, afterText string, mechanism proxyLayer0Mechanism, action evidence.Action, reason string, beforeTokens int, afterTokens int, workload savingspolicy.CodexWorkload) evidence.BlockDecision {
	argv := strings.Fields(commandLine)
	analysis := evidence.Analyze(argv, []byte(beforeText))
	preserved := proxyLayer0PreservedEvidence(mechanism, workload)
	safety := proxyLayer0EvidenceSafety(mechanism)
	recovery := proxyLayer0EvidenceRecovery(mechanism)
	if afterText == "" && beforeTokens == 0 && afterTokens == 0 {
		return evidence.DecisionFromObservation(0, string(mechanism), safety, action, reason, analysis, preserved, recovery, 0, 0)
	}
	return evidence.DecisionFromObservation(0, string(mechanism), safety, action, reason, analysis, preserved, recovery, beforeTokens, afterTokens)
}

func proxyLayer0EvidenceSafety(mechanism proxyLayer0Mechanism) evidence.SafetyClass {
	switch mechanism {
	case proxyLayer0MechanismReadDelta, proxyLayer0MechanismRepeatedOut:
		return evidence.SafetyExact
	case proxyLayer0MechanismChunkDedup:
		return evidence.SafetyRecoverable
	case proxyLayer0MechanismCapturedOut, proxyLayer0MechanismCodexEnvelope:
		return evidence.SafetyStructuredEvidence
	default:
		return evidence.SafetyUnknown
	}
}

func proxyLayer0EvidenceRecovery(mechanism proxyLayer0Mechanism) string {
	switch mechanism {
	case proxyLayer0MechanismReadDelta, proxyLayer0MechanismRepeatedOut:
		return "previous in-session exact block"
	case proxyLayer0MechanismChunkDedup:
		return "local archive chunk recovery"
	case proxyLayer0MechanismCapturedOut, proxyLayer0MechanismCodexEnvelope:
		return "parser fail-open to original output"
	default:
		return "fail-open to original output"
	}
}

func proxyLayer0PreservedEvidence(mechanism proxyLayer0Mechanism, workload savingspolicy.CodexWorkload) []string {
	switch mechanism {
	case proxyLayer0MechanismReadDelta:
		return []string{"file path", "changed hunk", "full prior read"}
	case proxyLayer0MechanismRepeatedOut:
		return []string{"command identity", "previous exact output"}
	case proxyLayer0MechanismChunkDedup:
		return []string{"chunk identity", "archive uri", "fresh unmatched content"}
	}
	switch workload {
	case savingspolicy.CodexWorkloadSearch:
		return []string{"file", "line", "match text", "match count", "omitted count"}
	case savingspolicy.CodexWorkloadRead:
		return []string{"file path", "range", "changed hunk", "recency guard"}
	default:
		return []string{"error line", "warning", "path", "line", "summary", "exit status"}
	}
}

func proxyWSSSearchOutputRisk(commandLine, text string, workload savingspolicy.CodexWorkload) bool {
	if workload == savingspolicy.CodexWorkloadSearch {
		return true
	}
	workDir, filterCommandLine := proxyLayer0FilterCommandForCompaction(commandLine)
	if filter.SearchOutputReducerEligibleFromCommandLine(filterCommandLine, workDir) {
		return true
	}
	return proxyToolResultLooksLikeSearchOutput(text)
}

func proxyCommandLineInvokesReconc(commandLine string) bool {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return false
	}
	_, inner, ok := splitLeadingCDCommand(commandLine)
	if ok && proxyCommandLineInvokesReconc(inner) {
		return true
	}
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) >= 3 && looksLikeShellExecutable(argv[0]) &&
		strings.HasPrefix(argv[1], "-") && strings.Contains(argv[1], "c") {
		return proxyCommandLineInvokesReconc(argv[2])
	}
	if len(argv) == 0 {
		return false
	}
	if proxyArgv0LooksLikeReconc(argv[0]) {
		return true
	}
	return proxyGoRunInvokesReconc(argv)
}

func proxyArgv0LooksLikeReconc(argv0 string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(argv0)))
	base = strings.TrimSuffix(base, ".exe")
	return base == "reconc" || strings.HasPrefix(base, "reconc-")
}

func proxyGoRunInvokesReconc(argv []string) bool {
	if len(argv) < 3 || strings.ToLower(filepath.Base(strings.TrimSpace(argv[0]))) != "go" || argv[1] != "run" {
		return false
	}
	for _, arg := range argv[2:] {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if arg == "--" {
			return false
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(arg))
		if filepath.Base(clean) == "reconc" || strings.HasSuffix(clean, "/cmd/reconc") || clean == "cmd/reconc" {
			return true
		}
	}
	return false
}

func proxyToolResultLooksLikeSearchOutput(text string) bool {
	if proxyLooksLikeSearchOutput(text) {
		return true
	}
	_, payload, ok := splitCodexExecEnvelope(text)
	return ok && proxyLooksLikeSearchOutput(payload)
}

func proxyEditedPathsFromMessages(messages []types.Message, rememberedToolUses map[string]types.ContentBlock) []string {
	toolUses := proxyToolUseIndex(messages)
	for id, use := range rememberedToolUses {
		if _, ok := toolUses[id]; !ok {
			toolUses[id] = use
		}
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				for _, path := range proxyLayer0EditPaths(block) {
					add(path)
				}
				continue
			}
			if block.Type != "tool_result" {
				continue
			}
			use, _ := proxyResolveToolUseDetailed(block, toolUses)
			for _, path := range proxyLayer0EditPaths(use) {
				add(path)
			}
		}
	}
	return out
}

func proxyLayer0EditPaths(block types.ContentBlock) []string {
	input := strings.TrimSpace(block.ToolInput)
	if input == "" {
		return nil
	}
	rawUnwrapped := false
	if raw := rawJSONString(json.RawMessage(input)); raw != "" {
		input = raw
		rawUnwrapped = true
	}
	if looksLikeEditTool(block.ToolName) {
		if rawUnwrapped {
			if strings.Contains(input, "*** ") || strings.Contains(input, "+++ ") || strings.Contains(input, "--- ") {
				return proxyPatchPathsFromText(input, "")
			}
			return []string{input}
		}
	}
	var out []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path != "" {
			out = append(out, path)
		}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &obj); err != nil {
		if looksLikeEditTool(block.ToolName) {
			add(input)
		}
		if looksLikeShellTool(block.ToolName) && strings.Contains(input, "apply_patch") {
			out = append(out, proxyPatchPathsFromText(input, "")...)
		}
		return compactStringSet(out)
	}
	workdir := proxyToolWorkdir(obj)
	if looksLikeEditTool(block.ToolName) {
		for _, key := range []string{"path", "file_path", "filepath", "absolute_path", "target", "source_path"} {
			if path := strings.TrimSpace(rawJSONString(obj[key])); path != "" {
				add(proxyPathWithWorkdir(path, workdir))
			}
		}
	}
	for _, key := range []string{"patch", "diff", "changes"} {
		if patch := strings.TrimSpace(rawJSONString(obj[key])); patch != "" {
			for _, path := range proxyPatchPathsFromText(patch, workdir) {
				add(path)
			}
		}
	}
	for _, key := range []string{"command", "cmd", "command_line", "cmdline", "commandLine", "shell_command", "shellCommand"} {
		if command := strings.TrimSpace(rawJSONString(obj[key])); strings.Contains(command, "apply_patch") {
			for _, path := range proxyPatchPathsFromText(command, workdir) {
				add(path)
			}
		}
	}
	return compactStringSet(out)
}

func looksLikeEditTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "edit", "write", "apply_patch", "multiedit", "multi_edit", "update_file",
		"write_file", "replace_file", "file.write", "fs.write", "mcp.write_file":
		return true
	default:
		return false
	}
}

func proxyPatchPathsFromText(patch, workdir string) []string {
	var out []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if i := strings.IndexByte(path, '\t'); i >= 0 {
			path = strings.TrimSpace(path[:i])
		}
		path = strings.TrimPrefix(path, "a/")
		path = strings.TrimPrefix(path, "b/")
		if path == "" || path == "/dev/null" {
			return
		}
		out = append(out, proxyPathWithWorkdir(path, workdir))
	}
	for _, line := range strings.Split(patch, "\n") {
		for _, prefix := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: ", "+++ ", "--- "} {
			if strings.HasPrefix(line, prefix) {
				add(strings.TrimPrefix(line, prefix))
			}
		}
	}
	return compactStringSet(out)
}

func compactStringSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
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

func readDeltaEligible(sessionID, commandLine string) bool {
	return strings.TrimSpace(sessionID) != "" && readRequestFromCommandLine(commandLine).FilePath != ""
}

func proxyLayer0QualityToolKey(commandLine string) string {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return ""
	}
	if req := readRequestFromCommandLine(commandLine); req.FilePath != "" {
		key := "read:" + filepath.Clean(req.FilePath)
		if req.Offset != 0 || req.Limit != 0 {
			key += ":range:" + strconv.Itoa(req.Offset) + ":" + strconv.Itoa(req.Limit)
		}
		return key
	}
	if key := filter.RepoScopedSearchOutputKeyFromCommandLine(commandLine); key != "" {
		return "search:" + key
	}
	if filter.SearchOutputKeyFromCommandLine(commandLine) != "" {
		return ""
	}
	return "command:" + commandLine
}

func proxyLayer0QualityToolKeyForUse(block types.ContentBlock, commandLine string) string {
	key := proxyLayer0QualityToolKey(commandLine)
	if !strings.HasPrefix(key, "command:") {
		return key
	}
	workdir := proxyLayer0ToolWorkdir(block)
	if workdir == "" {
		return key
	}
	deps := proxyLayer0DependencyFingerprint(commandLine, workdir)
	if deps != "" {
		return "command:cwd:" + workdir + ":deps:" + deps + ":" + strings.TrimPrefix(key, "command:")
	}
	return "command:cwd:" + workdir + ":" + strings.TrimPrefix(key, "command:")
}

func proxyLayer0ToolWorkdir(block types.ContentBlock) string {
	input := strings.TrimSpace(block.ToolInput)
	if input == "" {
		return ""
	}
	if raw := rawJSONString(json.RawMessage(input)); raw != "" {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &obj); err != nil {
		return ""
	}
	return proxyToolWorkdir(obj)
}

const proxyLayer0DependencyMaxBytes = 2 * 1024 * 1024

var proxyLayer0DependencyFiles = []string{
	"go.mod",
	"go.sum",
	"Cargo.toml",
	"Cargo.lock",
	"package.json",
	"package-lock.json",
	"pnpm-lock.yaml",
	"yarn.lock",
	"bun.lock",
	"bun.lockb",
	"pyproject.toml",
	"poetry.lock",
	"requirements.txt",
	"uv.lock",
	"Pipfile.lock",
	"pytest.ini",
	"tox.ini",
	"vitest.config.ts",
	"vitest.config.mts",
	"jest.config.js",
	"jest.config.ts",
}

func proxyLayer0DependencyFingerprint(commandLine, workdir string) string {
	if !proxyLayer0DependencySensitiveCommand(commandLine) {
		return ""
	}
	workdir = proxyCleanAbsWorkdir(workdir)
	if workdir == "" {
		return ""
	}
	hash := sha256.New()
	files := 0
	for _, path := range proxyLayer0DependencyFileCandidates(workdir) {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > proxyLayer0DependencyMaxBytes {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(workdir, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		hash.Write([]byte(rel))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
		files++
	}
	if files == 0 {
		return ""
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	return strconv.Itoa(files) + "-" + sum[:16]
}

func proxyLayer0DependencySensitiveCommand(commandLine string) bool {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	if base == "npx" && len(argv) >= 2 {
		base = strings.ToLower(filepath.Base(argv[1]))
	}
	if (base == "pnpm" || base == "yarn" || base == "bun") && len(argv) >= 3 && (argv[1] == "exec" || argv[1] == "dlx") {
		base = strings.ToLower(filepath.Base(argv[2]))
	}
	switch base {
	case "go", "cargo", "npm", "pnpm", "yarn", "bun", "pytest", "tox", "uv", "pip", "python", "python3", "node", "jest", "vitest", "tsc", "eslint":
		return true
	default:
		return false
	}
}

func proxyLayer0DependencyFileCandidates(workdir string) []string {
	seen := map[string]struct{}{}
	var out []string
	dir := filepath.Clean(workdir)
	for depth := 0; depth < 5 && dir != "" && dir != string(filepath.Separator); depth++ {
		for _, name := range proxyLayer0DependencyFiles {
			path := filepath.Join(dir, name)
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			out = append(out, path)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

func compactProxyLayer0Text(commandLine, text string, ctx filter.FileReadContext) (string, bool) {
	out, changed, _ := compactProxyLayer0TextDetailed(commandLine, text, ctx)
	return out, changed
}

func compactProxyLayer0TextDetailed(commandLine, text string, ctx filter.FileReadContext) (string, bool, proxyLayer0Mechanism) {
	filterWorkDir, filterCommandLine := proxyLayer0FilterCommandForCompaction(commandLine)
	out, changed := compactCodexExecEnvelopeWithWorkDir(filterWorkDir, filterCommandLine, text, ctx)
	if changed {
		return out, true, proxyLayer0MechanismCodexEnvelope
	}
	if inferred := proxyInferCommandLineFromToolResult(text); inferred != "" && inferred != commandLine {
		inferredWorkDir, inferredCommandLine := proxyLayer0FilterCommandForCompaction(inferred)
		out, changed = compactCodexExecEnvelopeWithWorkDir(inferredWorkDir, inferredCommandLine, text, ctx)
		if changed {
			return out, true, proxyLayer0MechanismCodexEnvelope
		}
	}
	compacted, changed := filter.CompactCapturedOutputWithContext(filterWorkDir, filterCommandLine, text, 0, ctx)
	if changed {
		return string(compacted), true, proxyLayer0MechanismCapturedOut
	}
	if inferred := proxyInferCommandLineFromToolResult(text); inferred != "" && inferred != commandLine {
		inferredWorkDir, inferredCommandLine := proxyLayer0FilterCommandForCompaction(inferred)
		compacted, changed = filter.CompactCapturedOutputWithContext(inferredWorkDir, inferredCommandLine, text, 0, ctx)
		if changed {
			return string(compacted), true, proxyLayer0MechanismCapturedOut
		}
	}
	return "", false, ""
}

func proxyLayer0FilterCommandForCompaction(commandLine string) (string, string) {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return "", ""
	}
	workdir, inner, ok := splitLeadingCDCommand(commandLine)
	if ok && proxyLayer0SafeInnerFilterCommand(inner) {
		return workdir, inner
	}
	return "", commandLine
}

func proxyLayer0SafeInnerFilterCommand(commandLine string) bool {
	if strings.TrimSpace(commandLine) == "" {
		return false
	}
	for _, marker := range []string{"&&", "||", ";", "|", "\n", "\r", "`", "$(", "<", ">"} {
		if strings.Contains(commandLine, marker) {
			return false
		}
	}
	return len(filter.ArgvForCapturedOutput(commandLine)) > 0
}

func proxyInferCommandLineFromToolResult(text string) string {
	_, payload, ok := splitCodexExecEnvelope(text)
	if !ok {
		return ""
	}
	if strings.TrimSpace(payload) == "" {
		return ""
	}
	if proxyLooksLikeGoTestOutput(payload) {
		return "go test"
	}
	if proxyLooksLikeSearchOutput(payload) {
		return "rg"
	}
	if proxyLooksLikeGitStatusOutput(payload) {
		return "git status --short"
	}
	return ""
}

func proxyLooksLikeGoTestOutput(payload string) bool {
	if !strings.Contains(payload, "=== RUN") {
		return false
	}
	for _, marker := range []string{"\n--- PASS:", "\n--- FAIL:", "\nFAIL\t", "\nPASS\n", "\nFAIL\n"} {
		if strings.Contains(payload, marker) {
			return true
		}
	}
	return false
}

func proxyLooksLikeSearchOutput(payload string) bool {
	nonEmpty := 0
	matches := 0
	for _, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Total output lines:") {
			continue
		}
		nonEmpty++
		if proxyLooksLikeSearchResultLine(line) {
			matches++
		}
		if nonEmpty >= 12 {
			break
		}
	}
	return matches >= 3 && matches*2 >= nonEmpty
}

func proxyLooksLikeSearchResultLine(line string) bool {
	first := strings.IndexByte(line, ':')
	if first <= 0 {
		return false
	}
	second := strings.IndexByte(line[first+1:], ':')
	if second <= 0 {
		return false
	}
	lineNo := line[first+1 : first+1+second]
	if lineNo == "" {
		return false
	}
	for _, ch := range lineNo {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	path := line[:first]
	return strings.Contains(path, "/") || strings.Contains(path, ".")
}

func proxyLooksLikeGitStatusOutput(payload string) bool {
	nonEmpty := 0
	statusLines := 0
	for _, line := range strings.Split(payload, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "Total output lines:") {
			continue
		}
		nonEmpty++
		if len(line) >= 3 && proxyLooksLikeGitStatusCode(line[:2]) && (line[2] == ' ' || line[2] == '\t') {
			statusLines++
		}
		if nonEmpty >= 12 {
			break
		}
	}
	return statusLines >= 3 && statusLines*2 >= nonEmpty
}

func proxyLooksLikeGitStatusCode(code string) bool {
	if len(code) != 2 {
		return false
	}
	valid := " MADRCU?!"
	return strings.ContainsRune(valid, rune(code[0])) && strings.ContainsRune(valid, rune(code[1]))
}

func archiveProxyCapturedOutput(sessionID, commandLine, compacted, original string) (string, bool) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(compacted) == "" || original == "" {
		return "", false
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return "", false
	}
	archiveOriginal := original
	if _, payload, ok := splitCodexExecEnvelope(original); ok {
		archiveOriginal = payload
	}
	entry, err := contentarchive.Put(contentarchive.DefaultDir(home), contentarchive.Input{
		SessionID: sessionID,
		SubLayer:  "codex_wss_captured_output",
		Original:  archiveOriginal,
		Preview:   fmt.Sprintf("tool output %s", strings.TrimSpace(commandLine)),
	}, contentarchive.Limits{})
	if err != nil || entry == nil || strings.TrimSpace(entry.URI) == "" {
		return "", false
	}
	return strings.TrimRight(compacted, "\n") + "\n[context-archive kind=tool-output uri=" + entry.URI + "]", true
}

func compactCodexExecEnvelope(commandLine, text string, ctx filter.FileReadContext) (string, bool) {
	return compactCodexExecEnvelopeWithWorkDir("", commandLine, text, ctx)
}

func compactCodexExecEnvelopeWithWorkDir(workDir, commandLine, text string, ctx filter.FileReadContext) (string, bool) {
	header, payload, ok := splitCodexExecEnvelope(text)
	if !ok {
		return "", false
	}
	compacted, changed := filter.CompactCapturedOutputWithContext(workDir, commandLine, payload, 0, ctx)
	if !changed {
		return "", false
	}
	return header + string(compacted), true
}

func compactProxyReadDelta(sessionID, turnID, commandLine, text string, ctx filter.FileReadContext, recentFullPassTurns int) (string, bool) {
	out, ok, _ := compactProxyReadDeltaDetailed(sessionID, turnID, commandLine, text, ctx, recentFullPassTurns)
	return out, ok
}

func compactProxyReadDeltaDetailed(sessionID, turnID, commandLine, text string, ctx filter.FileReadContext, recentFullPassTurns int) (string, bool, string) {
	out, ok, reason, _ := compactProxyReadDeltaWithDecision(sessionID, turnID, commandLine, text, ctx, recentFullPassTurns)
	return out, ok, reason
}

func compactProxyReadDeltaWithDecision(sessionID, turnID, commandLine, text string, ctx filter.FileReadContext, recentFullPassTurns int) (string, bool, string, readcache.Decision) {
	req := readRequestFromCommandLine(commandLine)
	if req.FilePath == "" || strings.TrimSpace(sessionID) == "" {
		if req.FilePath == "" {
			return "", false, "not_read_command", readcache.Decision{}
		}
		return "", false, "missing_session", readcache.Decision{}
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return "", false, "home_error", readcache.Decision{}
	}
	evaluateText := text
	envelopeHeader := ""
	if header, payload, ok := splitCodexExecEnvelope(text); ok {
		envelopeHeader = header
		evaluateText = payload
	}
	decision, err := readcache.EvaluateObserved(readcache.DefaultDir(home), readcache.Request{
		SessionID:               sessionID,
		TurnID:                  turnID,
		FilePath:                req.FilePath,
		Offset:                  req.Offset,
		Limit:                   req.Limit,
		RecentFullPassTurnLimit: recentFullPassTurns,
	}, evaluateText, contentarchive.DefaultDir(home), ctx.RecentlyEdited)
	if err != nil || decision.Type != readcache.DecisionBlock || decision.Reason == "" {
		if err != nil {
			return "", false, "readcache_error", decision
		}
		if decision.Reason != "" {
			return "", false, decision.Reason, decision
		}
		return "", false, "full_pass", decision
	}
	reason := envelopeHeader + decision.Reason
	if decision.BlockKind != "" {
		return reason, true, string(decision.BlockKind), decision
	}
	return reason, true, "block", decision
}

func readRequestFromCommandLine(commandLine string) readcache.Request {
	req, ok := filter.ReadRequestFromCommandLine(commandLine)
	if !ok {
		return readcache.Request{}
	}
	return readcache.Request{FilePath: req.Path, Offset: req.Offset, Limit: req.Limit}
}

func compactProxyRepeatedToolOutput(sessionID, commandLine, text string) (string, bool) {
	return compactProxyRepeatedToolOutputWithKey(sessionID, proxyLayer0QualityToolKey(commandLine), commandLine, text)
}

func compactProxyRepeatedToolOutputWithKey(sessionID, key, commandLine, text string) (string, bool) {
	out, ok, _ := compactProxyRepeatedToolOutputWithKeyDetailed(sessionID, key, commandLine, text)
	return out, ok
}

func compactProxyRepeatedToolOutputWithKeyDetailed(sessionID, key, commandLine, text string) (string, bool, string) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(key) == "" {
		if strings.TrimSpace(sessionID) == "" {
			return "", false, "missing_session"
		}
		return "", false, "missing_key"
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return "", false, "home_error"
	}
	evaluateText := text
	envelopeHeader := ""
	if header, payload, ok := splitCodexExecEnvelope(text); ok {
		envelopeHeader = header
		evaluateText = payload
	}
	decision, err := readcache.EvaluateObservedOutput(readcache.DefaultDir(home), readcache.OutputRequest{
		SessionID:   sessionID,
		Key:         key,
		CommandLine: commandLine,
	}, evaluateText, contentarchive.DefaultDir(home))
	if err != nil || decision.Type != readcache.DecisionBlock || decision.Reason == "" {
		if err != nil {
			return "", false, "readcache_error"
		}
		if decision.Reason != "" {
			return "", false, decision.Reason
		}
		return "", false, "full_pass"
	}
	reason := envelopeHeader + decision.Reason
	if decision.BlockKind != "" {
		return reason, true, string(decision.BlockKind)
	}
	return reason, true, "block"
}

func compactProxyChunkDedup(store *chunkdedup.Store, sessionID, text string, minBytes, maxReferencePercent int) (string, bool, proxyLayer0Mechanism, chunkdedup.EncodeResult) {
	if store == nil || strings.TrimSpace(sessionID) == "" || len(text) == 0 {
		return "", false, "", chunkdedup.EncodeResult{}
	}
	if header, payload, ok := splitCodexExecEnvelope(text); ok {
		encoded, changed, report := encodeProxyChunkDedup(store, sessionID, payload, minBytes, maxReferencePercent)
		if changed {
			return header + encoded, true, proxyLayer0MechanismChunkDedup, report
		}
		return "", false, "", chunkdedup.EncodeResult{}
	}
	encoded, changed, report := encodeProxyChunkDedup(store, sessionID, text, minBytes, maxReferencePercent)
	if !changed {
		return "", false, "", chunkdedup.EncodeResult{}
	}
	return encoded, true, proxyLayer0MechanismChunkDedup, report
}

func chunkDedupAllowedForCommand(commandLine string, readCommand bool) bool {
	raw := strings.ToLower(strings.TrimSpace(commandLine))
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		if chunkDedupBlockedRawCommand(raw) {
			return false
		}
		return true
	}
	if argvHasPatchLikeFile(argv) {
		return false
	}
	if readCommand {
		return true
	}
	base := strings.ToLower(filepath.Base(strings.TrimSpace(argv[0])))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "apply_patch", "patch", "diff", "colordiff", "combinediff", "interdiff", "filterdiff", "difft", "diff-so-fancy", "delta":
		return false
	case "git":
		if subcommand := gitSubcommand(argv); subcommand != "" {
			switch subcommand {
			case "diff", "show", "apply", "am", "format-patch":
				return false
			case "log":
				return gitLogChunkDedupSafe(argv)
			}
		}
	case "gh":
		if ghCommandProducesDiff(argv) {
			return false
		}
	case "jj":
		if jjCommandProducesDiff(argv) {
			return false
		}
	case "hg", "svn":
		if vcsSubcommand(argv, base) == "diff" {
			return false
		}
	}
	for _, arg := range argv {
		lower := strings.ToLower(strings.TrimSpace(arg))
		if lower == "apply_patch" || lower == "patch" ||
			strings.Contains(lower, "*** begin patch") ||
			strings.Contains(lower, "*** update file:") ||
			strings.Contains(lower, "*** add file:") ||
			strings.Contains(lower, "*** delete file:") {
			return false
		}
	}
	return true
}

func chunkDedupBlockedRawCommand(raw string) bool {
	if raw == "" {
		return false
	}
	if strings.Contains(raw, "apply_patch") ||
		strings.Contains(raw, "*** begin patch") ||
		strings.Contains(raw, "*** update file:") ||
		strings.Contains(raw, "*** add file:") ||
		strings.Contains(raw, "*** delete file:") {
		return true
	}
	for _, needle := range []string{
		"git diff", "git show", "git apply", "git am", "git format-patch",
		"gh pr diff", "gh pr view --patch", "jj diff", "jj show",
		"hg diff", "svn diff", "diff -", "colordiff ", "diff-so-fancy",
	} {
		if strings.Contains(raw, needle) {
			return true
		}
	}
	for _, words := range [][]string{
		{"git", "diff"},
		{"git", "show"},
		{"git", "format-patch"},
		{"gh", "pr", "diff"},
		{"jj", "diff"},
		{"jj", "show"},
		{"hg", "diff"},
		{"svn", "diff"},
	} {
		if rawHasOrderedWords(raw, words...) {
			return true
		}
	}
	return false
}

func rawHasOrderedWords(raw string, words ...string) bool {
	if raw == "" || len(words) == 0 {
		return false
	}
	tokens := strings.FieldsFunc(raw, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	})
	next := 0
	for _, token := range tokens {
		if token == words[next] {
			next++
			if next == len(words) {
				return true
			}
		}
	}
	return false
}

func gitSubcommand(argv []string) string {
	if len(argv) < 2 || strings.ToLower(filepath.Base(strings.TrimSpace(argv[0]))) != "git" {
		return ""
	}
	for i := 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == "" {
			continue
		}
		switch {
		case arg == "-C" || arg == "-c" || arg == "--git-dir" || arg == "--work-tree":
			i++
			continue
		case strings.HasPrefix(arg, "--git-dir="), strings.HasPrefix(arg, "--work-tree="), strings.HasPrefix(arg, "-c"):
			continue
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return strings.ToLower(arg)
		}
	}
	return ""
}

func gitLogChunkDedupSafe(argv []string) bool {
	for _, arg := range argv[1:] {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "-p", "--patch", "--stat", "--patch-with-stat", "--name-status", "--name-only", "--numstat", "--raw":
			return false
		}
	}
	return true
}

func ghCommandProducesDiff(argv []string) bool {
	if len(argv) < 3 || strings.ToLower(filepath.Base(strings.TrimSpace(argv[0]))) != "gh" {
		return false
	}
	for i := 1; i < len(argv)-1; i++ {
		if strings.ToLower(strings.TrimSpace(argv[i])) == "pr" {
			next := strings.ToLower(strings.TrimSpace(argv[i+1]))
			if next == "diff" {
				return true
			}
			if next == "view" {
				for _, arg := range argv[i+2:] {
					if strings.ToLower(strings.TrimSpace(arg)) == "--patch" {
						return true
					}
				}
			}
		}
	}
	return false
}

func jjCommandProducesDiff(argv []string) bool {
	if len(argv) < 2 || strings.ToLower(filepath.Base(strings.TrimSpace(argv[0]))) != "jj" {
		return false
	}
	switch vcsSubcommand(argv, "jj") {
	case "diff", "show":
		return true
	}
	return false
}

func vcsSubcommand(argv []string, bin string) string {
	if len(argv) < 2 || strings.ToLower(filepath.Base(strings.TrimSpace(argv[0]))) != bin {
		return ""
	}
	for _, arg := range argv[1:] {
		arg = strings.ToLower(strings.TrimSpace(arg))
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func argvHasPatchLikeFile(argv []string) bool {
	for _, arg := range argv {
		lower := strings.ToLower(strings.TrimSpace(arg))
		if strings.HasSuffix(lower, ".patch") || strings.HasSuffix(lower, ".diff") {
			return true
		}
	}
	return false
}

func encodeProxyChunkDedup(store *chunkdedup.Store, sessionID, text string, minBytes, maxReferencePercent int) (string, bool, chunkdedup.EncodeResult) {
	if minBytes < 0 {
		minBytes = 0
	}
	if maxReferencePercent <= 0 || maxReferencePercent > 100 {
		maxReferencePercent = 100
	}
	if len(text) < minBytes {
		return "", false, chunkdedup.EncodeResult{}
	}
	result := store.EncodeWithReportWithMaxReferencePercent(sessionID, []byte(text), maxReferencePercent)
	if result.Saved <= 0 || bytesEqualString(result.Data, text) {
		return "", false, chunkdedup.EncodeResult{}
	}
	return string(result.Data), true, result
}

func bytesEqualString(data []byte, text string) bool {
	if len(data) != len(text) {
		return false
	}
	return string(data) == text
}

func proxyReadFileContext(sessionID string, commandLine string) filter.FileReadContext {
	ctx := filter.FileReadContext{Mode: "scan"}
	path := filter.ReadPathFromCommandLine(commandLine)
	if path == "" || strings.TrimSpace(sessionID) == "" {
		return ctx
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return ctx
	}
	recent, err := sessions.RecentlyEditedHookFile(sessions.DefaultHookStateDir(home), sessionID, path, 2)
	if err == nil && recent {
		ctx.RecentlyEdited = true
	}
	return ctx
}

func splitCodexExecEnvelope(text string) (header, payload string, ok bool) {
	if !strings.Contains(text, "Process exited with code ") {
		return "", "", false
	}
	for _, marker := range []string{"\nOutput:\n", "\r\nOutput:\r\n"} {
		idx := strings.Index(text, marker)
		if idx < 0 {
			continue
		}
		header = text[:idx+len(marker)]
		payload = text[idx+len(marker):]
		return header, payload, payload != ""
	}
	return "", "", false
}

func proxyToolUseIndex(messages []types.Message) map[string]types.ContentBlock {
	index := make(map[string]types.ContentBlock)
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ToolUseID != "" {
				index[block.ToolUseID] = block
			}
		}
	}
	return index
}

func proxyResolveToolUse(block types.ContentBlock, toolUses map[string]types.ContentBlock) types.ContentBlock {
	use, _ := proxyResolveToolUseDetailed(block, toolUses)
	return use
}

func proxyResolveToolUseDetailed(block types.ContentBlock, toolUses map[string]types.ContentBlock) (types.ContentBlock, bool) {
	if len(toolUses) == 0 {
		return block, block.ToolInput != "" || block.ToolName != ""
	}
	id := block.ToolResultID
	if id == "" {
		id = block.ToolUseID
	}
	if id == "" {
		return block, block.ToolInput != "" || block.ToolName != ""
	}
	use, ok := toolUses[id]
	if !ok {
		return block, block.ToolInput != "" || block.ToolName != ""
	}
	return use, true
}

func proxyLayer0CommandLine(block types.ContentBlock) string {
	input := strings.TrimSpace(block.ToolInput)
	if input == "" {
		return ""
	}
	if raw := rawJSONString(json.RawMessage(input)); raw != "" {
		input = raw
		if looksLikeReadTool(block.ToolName) {
			return "cat " + quoteShellArg(input)
		}
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &obj); err == nil {
		workdir := proxyToolWorkdir(obj)
		for _, key := range []string{"command", "cmd", "command_line", "cmdline", "commandLine", "shell_command", "shellCommand"} {
			if s := strings.TrimSpace(rawJSONString(obj[key])); s != "" {
				return applyWorkdirToLayer0Command(normalizeLayer0CommandLine(s), workdir)
			}
		}
		for _, key := range []string{"command", "argv", "args", "cmd_args", "command_args"} {
			if argv := proxyStringArray(obj[key]); len(argv) > 0 {
				return applyWorkdirToLayer0Command(normalizeLayer0CommandLine(joinShellArgs(argv)), workdir)
			}
		}
		if looksLikeReadTool(block.ToolName) {
			for _, key := range []string{"path", "file_path", "filepath", "absolute_path", "uri", "target", "source_path"} {
				if path := strings.TrimSpace(rawJSONString(obj[key])); path != "" {
					return "cat " + quoteShellArg(proxyPathWithWorkdir(path, workdir))
				}
			}
		}
	}

	if looksLikeShellTool(block.ToolName) {
		return normalizeLayer0CommandLine(input)
	}
	return ""
}

func applyWorkdirToLayer0Command(commandLine, workdir string) string {
	commandLine = applyWorkdirToReadCommand(commandLine, workdir)
	if git := applyWorkdirToGitCommand(commandLine, workdir); git != "" {
		return git
	}
	if search := filter.NormalizeSearchCommandLine(commandLine, workdir); search != "" {
		return search
	}
	return commandLine
}

func proxyToolWorkdir(obj map[string]json.RawMessage) string {
	for _, key := range []string{"workdir", "cwd", "working_directory", "workingDirectory", "workingDir", "current_working_directory", "directory"} {
		if dir := strings.TrimSpace(rawJSONString(obj[key])); dir != "" && filepath.IsAbs(dir) {
			return filepath.Clean(dir)
		}
	}
	return ""
}

func applyWorkdirToReadCommand(commandLine, workdir string) string {
	workdir = proxyCleanAbsWorkdir(workdir)
	if workdir == "" {
		return commandLine
	}
	path := filter.ReadPathFromCommandLine(commandLine)
	if path == "" || filepath.IsAbs(path) {
		return commandLine
	}
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		return commandLine
	}
	out := append([]string(nil), argv...)
	for i := len(out) - 1; i >= 1; i-- {
		if out[i] == path {
			out[i] = filepath.Clean(filepath.Join(workdir, path))
			return joinShellArgs(out)
		}
	}
	return commandLine
}

func proxyPathWithWorkdir(path, workdir string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	workdir = proxyCleanAbsWorkdir(workdir)
	if workdir == "" {
		return path
	}
	return filepath.Clean(filepath.Join(workdir, path))
}

func proxyCleanAbsWorkdir(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" || !filepath.IsAbs(workdir) {
		return ""
	}
	return filepath.Clean(workdir)
}

func normalizeLayer0CommandLine(commandLine string) string {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if stripped := stripSlimferenceFilterWrapper(argv); stripped != "" {
		return stripped
	}
	if len(argv) >= 3 && looksLikeShellExecutable(argv[0]) && strings.Contains(argv[1], "c") && strings.HasPrefix(argv[1], "-") {
		return normalizeLayer0CommandLine(argv[2])
	}
	if stripped := normalizeLeadingCDCommand(commandLine); stripped != "" {
		return stripped
	}
	return commandLine
}

func normalizeLeadingCDCommand(commandLine string) string {
	workdir, inner, ok := splitLeadingCDCommand(commandLine)
	if !ok {
		return ""
	}
	if filter.ReadPathFromCommandLine(inner) != "" {
		return applyWorkdirToReadCommand(inner, workdir)
	}
	if git := applyWorkdirToGitCommand(inner, workdir); git != "" {
		return git
	}
	if search := filter.NormalizeSearchCommandLine(inner, workdir); search != "" {
		return search
	}
	return ""
}

func splitLeadingCDCommand(commandLine string) (string, string, bool) {
	idx := strings.Index(commandLine, "&&")
	if idx < 0 {
		return "", "", false
	}
	prefix := strings.TrimSpace(commandLine[:idx])
	rest := strings.TrimSpace(commandLine[idx+len("&&"):])
	if rest == "" {
		return "", "", false
	}
	argv := filter.ArgvForCapturedOutput(prefix)
	if len(argv) != 2 || strings.ToLower(filepath.Base(argv[0])) != "cd" {
		return "", "", false
	}
	workdir := proxyCleanAbsWorkdir(argv[1])
	if workdir == "" {
		return "", "", false
	}
	inner := normalizeLayer0CommandLine(rest)
	if strings.TrimSpace(inner) == "" {
		return "", "", false
	}
	return workdir, inner, true
}

func applyWorkdirToGitCommand(commandLine, workdir string) string {
	workdir = proxyCleanAbsWorkdir(workdir)
	if workdir == "" {
		return ""
	}
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) < 2 || filepath.Base(strings.TrimSpace(argv[0])) != "git" {
		return ""
	}
	for i := 1; i < len(argv); i++ {
		if argv[i] == "-C" || strings.HasPrefix(argv[i], "--git-dir") {
			return commandLine
		}
	}
	out := make([]string, 0, len(argv)+2)
	out = append(out, argv[0], "-C", workdir)
	out = append(out, argv[1:]...)
	return joinShellArgs(out)
}

func stripSlimferenceFilterWrapper(argv []string) string {
	if len(argv) < 3 {
		return ""
	}
	if filepath.Base(strings.TrimSpace(argv[0])) != "slimference" || argv[1] != "filter" {
		return ""
	}
	start := 2
	for start < len(argv) {
		switch argv[start] {
		case "--", "--stream":
			start++
		default:
			if strings.HasPrefix(argv[start], "-") {
				return ""
			}
			return joinShellArgs(argv[start:])
		}
	}
	return ""
}

func proxyStringArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func looksLikeShellTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "shell", "sh", "exec", "exec_command", "command", "terminal",
		"local_shell", "local_shell_call", "bash_command", "run_command", "terminal.exec",
		"container.exec", "shell_command", "execute", "run", "terminal_command":
		return true
	default:
		return false
	}
}

func looksLikeShellExecutable(name string) bool {
	switch strings.ToLower(filepath.Base(strings.TrimSpace(name))) {
	case "bash", "sh", "zsh":
		return true
	default:
		return false
	}
}

func looksLikeReadTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "cat", "view", "open", "open_file", "read_file", "readfile", "view_file",
		"file_read", "read_path", "view_path", "open_path", "file.read", "fs.read",
		"read_file_tool", "local_file_read", "mcp.read_file":
		return true
	default:
		return false
	}
}

func joinShellArgs(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, quoteShellArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteShellArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if strings.Contains(arg, "$") && !strings.Contains(arg, "'") {
		return "'" + arg + "'"
	}
	if strings.IndexFunc(arg, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"' || r == '\\' ||
			r == '|' || r == '&' || r == ';' || r == '<' || r == '>' || r == '$' ||
			r == '*' || r == '?' || r == '(' || r == ')' || r == '`'
	}) < 0 {
		return arg
	}
	return strconv.Quote(arg)
}

func cloneMessages(messages []types.Message) []types.Message {
	out := make([]types.Message, len(messages))
	copy(out, messages)
	for i := range out {
		out[i].Content = append([]types.ContentBlock(nil), messages[i].Content...)
	}
	return out
}

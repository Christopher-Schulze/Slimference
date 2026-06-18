package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
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
	proxyLayer0MechanismStaleRead     proxyLayer0Mechanism = "stale_read"
	proxyLayer0MechanismObsoletePrune proxyLayer0Mechanism = "obsolete_prune"
)

const proxyInferredPlainPathListCommandLine = "(inferred plain path-list)"

type proxyLayer0MechanismMask uint32

const (
	proxyLayer0MechanismMaskReadDelta proxyLayer0MechanismMask = 1 << iota
	proxyLayer0MechanismMaskCapturedOutput
	proxyLayer0MechanismMaskCodexExecEnvelope
	proxyLayer0MechanismMaskRepeatedToolOutput
	proxyLayer0MechanismMaskChunkDedup
	proxyLayer0MechanismMaskStaleRead
	proxyLayer0MechanismMaskObsoletePrune
)

func proxyLayer0MechanismMaskFor(mechanism proxyLayer0Mechanism) proxyLayer0MechanismMask {
	switch mechanism {
	case proxyLayer0MechanismReadDelta:
		return proxyLayer0MechanismMaskReadDelta
	case proxyLayer0MechanismCapturedOut:
		return proxyLayer0MechanismMaskCapturedOutput
	case proxyLayer0MechanismCodexEnvelope:
		return proxyLayer0MechanismMaskCodexExecEnvelope
	case proxyLayer0MechanismRepeatedOut:
		return proxyLayer0MechanismMaskRepeatedToolOutput
	case proxyLayer0MechanismChunkDedup:
		return proxyLayer0MechanismMaskChunkDedup
	case proxyLayer0MechanismStaleRead:
		return proxyLayer0MechanismMaskStaleRead
	case proxyLayer0MechanismObsoletePrune:
		return proxyLayer0MechanismMaskObsoletePrune
	default:
		return 0
	}
}

func proxyLayer0MechanismMaskFromStats(stats proxyLayer0Stats) proxyLayer0MechanismMask {
	var mask proxyLayer0MechanismMask
	if stats.ReadDeltaBlocks > 0 {
		mask |= proxyLayer0MechanismMaskReadDelta
	}
	if stats.CapturedOutputBlocks > 0 {
		mask |= proxyLayer0MechanismMaskCapturedOutput
	}
	if stats.CodexExecEnvelopeBlocks > 0 {
		mask |= proxyLayer0MechanismMaskCodexExecEnvelope
	}
	if stats.RepeatedOutputBlocks > 0 {
		mask |= proxyLayer0MechanismMaskRepeatedToolOutput
	}
	if stats.ChunkDedupBlocks > 0 {
		mask |= proxyLayer0MechanismMaskChunkDedup
	}
	if stats.StaleReadBlocks > 0 {
		mask |= proxyLayer0MechanismMaskStaleRead
	}
	if stats.ObsoletePruneBlocks > 0 {
		mask |= proxyLayer0MechanismMaskObsoletePrune
	}
	return mask
}

func (m proxyLayer0MechanismMask) Has(mechanism proxyLayer0Mechanism) bool {
	bit := proxyLayer0MechanismMaskFor(mechanism)
	return bit != 0 && m&bit != 0
}

func (m proxyLayer0MechanismMask) String() string {
	if m == 0 {
		return ""
	}
	names := make([]string, 0, 5)
	for _, mechanism := range []proxyLayer0Mechanism{
		proxyLayer0MechanismReadDelta,
		proxyLayer0MechanismCapturedOut,
		proxyLayer0MechanismCodexEnvelope,
		proxyLayer0MechanismRepeatedOut,
		proxyLayer0MechanismChunkDedup,
		proxyLayer0MechanismStaleRead,
		proxyLayer0MechanismObsoletePrune,
	} {
		if m.Has(mechanism) {
			names = append(names, string(mechanism))
		}
	}
	return strings.Join(names, ",")
}

func proxyLayer0CacheBustClassKeyForString(mechanism string, class evidence.ContentClass) string {
	mechanism = strings.TrimSpace(mechanism)
	if mechanism == "" {
		return ""
	}
	classText := strings.TrimSpace(string(class))
	if classText == "" {
		classText = string(evidence.ContentUnknown)
	}
	return mechanism + ":" + classText
}

func proxyLayer0CacheBustClassKeyForMechanism(mechanism proxyLayer0Mechanism, class evidence.ContentClass) string {
	return proxyLayer0CacheBustClassKeyForString(string(mechanism), class)
}

func proxyLayer0CacheBustClassKey(mechanism proxyLayer0Mechanism, commandLine string, beforeText string) string {
	analysis := evidence.Analyze(strings.Fields(commandLine), []byte(beforeText))
	general := proxyLayer0CacheBustClassKeyForMechanism(mechanism, analysis.ContentClass)
	if general == "" || mechanism != proxyLayer0MechanismCapturedOut || analysis.ContentClass != evidence.ContentSearch {
		if commandKey := proxyLayer0CacheBustCommandIdentityKey(commandLine); commandKey != "" && proxyLayer0CacheBustMechanismUsesCommandIdentity(mechanism) {
			return general + ":cmd=" + commandKey
		}
		return general
	}
	_, filterCommandLine := proxyLayer0FilterCommandForCompaction(commandLine)
	searchKey := filter.SearchOutputKeyFromCommandLine(filterCommandLine)
	if searchKey == "" {
		return general
	}
	return general + ":key=" + proxyLayer0CacheBustStableKeyHash(searchKey)
}

func proxyLayer0CacheBustStableKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

func proxyLayer0CacheBustCommandIdentityKey(commandLine string) string {
	workdir, filterCommandLine := proxyLayer0FilterCommandForCompaction(commandLine)
	filterCommandLine = strings.TrimSpace(filterCommandLine)
	if filterCommandLine == "" {
		return ""
	}
	identity := "cmd=" + filterCommandLine
	if workdir = strings.TrimSpace(workdir); workdir != "" {
		identity = "cwd=" + filepath.Clean(workdir) + "\n" + identity
	}
	return proxyLayer0CacheBustStableKeyHash(identity)
}

func proxyLayer0CacheBustMechanismUsesCommandIdentity(mechanism proxyLayer0Mechanism) bool {
	switch mechanism {
	case proxyLayer0MechanismReadDelta,
		proxyLayer0MechanismCapturedOut,
		proxyLayer0MechanismCodexEnvelope,
		proxyLayer0MechanismRepeatedOut,
		proxyLayer0MechanismStaleRead:
		return true
	default:
		return false
	}
}

func proxyLayer0CacheBustClassKeysFromStats(stats proxyLayer0Stats) map[string]struct{} {
	out := cloneProxyLayer0CacheBustClassKeys(stats.CacheBustClassKeys)
	if out == nil {
		out = make(map[string]struct{})
	}
	for _, decision := range stats.EvidenceDecisions {
		if decision.Action != evidence.ActionApplied {
			continue
		}
		key := proxyLayer0CacheBustClassKeyForString(decision.Mechanism, decision.ContentClass)
		if key == "" {
			continue
		}
		if proxyLayer0CacheBustClassKeysCover(out, key) {
			continue
		}
		out[key] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func proxyLayer0CacheBustClassKeysCover(keys map[string]struct{}, generalKey string) bool {
	if len(keys) == 0 || strings.TrimSpace(generalKey) == "" {
		return false
	}
	if _, ok := keys[generalKey]; ok {
		return true
	}
	prefix := generalKey + ":"
	for key := range keys {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func cloneProxyLayer0CacheBustClassKeys(keys map[string]struct{}) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(keys))
	for key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func proxyLayer0CacheBustClassKeysString(keys map[string]struct{}) string {
	return strings.Join(proxyLayer0CacheBustClassKeysSlice(keys), ",")
}

func proxyLayer0CacheBustClassKeysSlice(keys map[string]struct{}) []string {
	if len(keys) == 0 {
		return nil
	}
	parts := make([]string, 0, len(keys))
	for key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			parts = append(parts, key)
		}
	}
	sort.Strings(parts)
	return parts
}

func proxyLayer0CacheBustCandidateDemoted(req codexLayer0Request, commandLine string, beforeText string, mechanism proxyLayer0Mechanism) bool {
	if !req.CacheBustDemotedMechanisms.Has(mechanism) {
		return false
	}
	if len(req.CacheBustDemotedClassKeys) == 0 {
		return true
	}
	key := proxyLayer0CacheBustClassKey(mechanism, commandLine, beforeText)
	if _, demoted := req.CacheBustDemotedClassKeys[key]; demoted {
		return true
	}
	analysis := evidence.Analyze(strings.Fields(commandLine), []byte(beforeText))
	general := proxyLayer0CacheBustClassKeyForMechanism(mechanism, analysis.ContentClass)
	_, demoted := req.CacheBustDemotedClassKeys[general]
	return demoted
}

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
	ToolUseIndex              map[string]types.ContentBlock
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
	TurnSeq                   int
	RemainingTurnsEstimate    int
	CachedPriceRatio          float64
	SearchCompactOptions      filter.SearchCompactOptions
	UniformChunkDedupBudget   bool
	RecentEditUncertainty     bool
	HostBudgetExceeded        bool
	LatencyBudgetExceeded     bool
	ChunkIntegrityBudgetHit   bool
	StructuredMutationBlocked bool
	// WSSSearchMutationAllowed opens only named, tool-use-bound search output
	// after either the old full-history/lab proof or the final search-cap proof
	// latch. The latch is intentionally narrower than broad tool-output delta
	// mutation: non-search, inferred search, and unknown output stay guarded.
	WSSSearchMutationAllowed   bool
	CacheBustDemotedMechanisms proxyLayer0MechanismMask
	// CacheBustDemotedClassKeys narrows a provider-cache-bust demotion to
	// content-free mechanism:content_class keys when the triggering request
	// was classifiable. Empty preserves the legacy broad mechanism guard.
	CacheBustDemotedClassKeys map[string]struct{}
	// HistoryMutationGuardReason suppresses history/chunk reducers that can
	// perturb Codex server state and break the following previous_response_id
	// delta turn. It is intentionally narrower than the structured/output
	// mutation guards so archive-backed search/output paths can keep their own
	// proof gates.
	HistoryMutationGuardReason string
	// StatefulDeltaMutationBlocked suppresses broad/default wire mutation while
	// keeping reducers observing/seeding. Live A/B (2026-06-11, loop runs
	// 4-8): unscoped function_call_output mutation on a previous_response_id
	// delta turn made the FOLLOWING tool turn fail upstream with 400
	// invalid_request (byte-equal bridge control stayed clean), and Codex
	// healed via a full-history resend retry that cost more than the mutation
	// saved. The only product bypass is the narrower WSSSearchMutationAllowed
	// search-cap latch for named grep-style search output.
	StatefulDeltaMutationBlocked bool
}

type codexLayer0Result struct {
	Messages []types.Message
	Stats    proxyLayer0Stats
}

type proxyChunkDedupPriorityCandidate struct {
	Key         [2]int
	Order       int
	Score       int
	BudgetBytes int
}

type proxyChunkDedupPriorityPlan struct {
	ByBlock    map[[2]int]proxyChunkDedupPriorityCandidate
	Candidates []proxyChunkDedupPriorityCandidate
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
	// ToolResultBytes is the total tool-result payload the reducer pass had
	// to process; the latency budget scales with it so legitimate work on
	// large outputs does not count as overhead pressure.
	ToolResultBytes          int
	TokensSaved              int
	BlocksModified           int
	ReadDeltaBlocks          int
	CapturedOutputBlocks     int
	CodexExecEnvelopeBlocks  int
	RepeatedOutputBlocks     int
	ChunkDedupBlocks         int
	ChunkDedupReferences     int
	ChunkDedupRefBytes       int
	ChunkDedupInputBytes     int
	StaleReadBlocks          int
	StaleReadBytesSaved      int
	StaleReadTokensSaved     int
	ObsoletePruneBlocks      int
	ObsoletePruneBytesSaved  int
	ObsoletePruneTokensSaved int
	WSSSearchRiskBlocks      int
	WSSSearchProofAllowed    int
	WSSSearchProofBlocked    int
	WSSSearchProofReasons    map[string]int
	ReadDeltaKeys            []string
	PolicyDecisions          []savingspolicy.CodexMechanismDecision
	CacheEvents              []proxyLayer0CacheEvent
	EvidenceDecisions        []evidence.BlockDecision
	CacheBustClassKeys       map[string]struct{}
	TotalLatencyNs           int64
	ReadDeltaLatencyNs       int64
	FilterLatencyNs          int64
	RepeatedOutputLatencyNs  int64
	ChunkDedupLatencyNs      int64
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
	s.StaleReadBlocks = 0
	s.StaleReadBytesSaved = 0
	s.StaleReadTokensSaved = 0
	s.ObsoletePruneBlocks = 0
	s.ObsoletePruneBytesSaved = 0
	s.ObsoletePruneTokensSaved = 0
	s.WSSSearchRiskBlocks = 0
	s.WSSSearchProofAllowed = 0
	s.WSSSearchProofBlocked = 0
	s.WSSSearchProofReasons = nil
	s.ReadDeltaKeys = nil
	s.PolicyDecisions = nil
	s.CacheEvents = nil
	s.EvidenceDecisions = nil
	s.CacheBustClassKeys = nil
	return s
}

func proxyLayer0StatsNeedsArchiveRecoveryNote(stats proxyLayer0Stats) bool {
	if stats.ReadDeltaBlocks > 0 || stats.RepeatedOutputBlocks > 0 || stats.ChunkDedupBlocks > 0 {
		return true
	}
	if stats.Route == codexLayer0RouteWSSPhaseF &&
		(stats.CapturedOutputBlocks > 0 || stats.CodexExecEnvelopeBlocks > 0) {
		return true
	}
	return false
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

func (p *Proxy) codexHTTPChunkDedupSettings(provider types.Provider) codexChunkDedupSettings {
	settings := p.codexChunkDedupSettings()
	if provider != types.CodexChatGPT {
		settings.Store = nil
		settings.Enabled = false
		settings.Explicit = false
		settings.ArchiveRecovery = false
	}
	return settings
}

func reduceCodexLayer0(req codexLayer0Request) codexLayer0Result {
	started := time.Now()
	baseToolUses := req.ToolUseIndex
	if baseToolUses == nil {
		baseToolUses = proxyToolUseIndex(req.Messages)
	}
	toolUses := mergedProxyToolUseIndex(baseToolUses, req.RememberedToolUse)
	chunkPriorityPlan := proxyPlanChunkDedupPriority(req, toolUses)
	cow := messageCow{original: req.Messages}
	stats := proxyLayer0Stats{Route: req.Route}
	recentEditUncertainty := req.RecentEditUncertainty || len(proxyEditedPathsFromMessagesWithToolUses(req.Messages, toolUses)) > 0

	for msgIdx, msg := range req.Messages {
		for blockIdx, block := range msg.Content {
			if block.Type != "tool_result" {
				continue
			}
			stats.ToolResultBlocks++
			stats.ToolResultBytes += len(block.Text)
			use, toolUseResolved := proxyResolveToolUseDetailed(block, toolUses)
			commandLine := proxyLayer0CommandLine(use)
			commandFromToolUse := commandLine != ""
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
			readCtx.SearchCompactOptions = req.SearchCompactOptions
			readReq := readRequestFromCommandLine(commandLine)
			readCommand := readReq.FilePath != ""
			statefulSafeToolOutputBlock := req.Route == codexLayer0RouteWSSPhaseF &&
				!readCommand &&
				wssSafeStatefulStatusCommandOutput(commandLine, block.Text)
			workload := savingspolicy.CodexWorkloadCommand
			if readCommand {
				workload = savingspolicy.CodexWorkloadRead
			} else if proxyWSSPathListOutputReducerEligible(commandLine) {
				workload = savingspolicy.CodexWorkloadCommand
			} else if proxyWSSSearchOutputReducerEligible(commandLine) {
				workload = savingspolicy.CodexWorkloadSearch
			}
			chunkMinBytes := proxyScaledChunkDedupMinBytes(req.ChunkDedupMinBytes, len(block.Text), req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio)
			wssSearchProofAllowed, wssSearchProofReason := proxyWSSSearchOutputProofDecision(commandLine, use, commandFromToolUse, workload, req.WSSSearchMutationAllowed, req.StatefulDeltaMutationBlocked)
			wssSearchRisk := req.Route == codexLayer0RouteWSSPhaseF &&
				!statefulSafeToolOutputBlock &&
				proxyWSSSearchOutputRisk(commandLine, block.Text, workload)
			wssSearchOutputBlocked := wssSearchRisk
			if wssSearchOutputBlocked && wssSearchProofAllowed {
				wssSearchOutputBlocked = false
			}
			if wssSearchRisk {
				stats.WSSSearchRiskBlocks++
				if wssSearchProofAllowed {
					stats.WSSSearchProofAllowed++
				} else {
					stats.WSSSearchProofBlocked++
					if stats.WSSSearchProofReasons == nil {
						stats.WSSSearchProofReasons = make(map[string]int, 1)
					}
					stats.WSSSearchProofReasons[wssSearchProofReason]++
				}
			}
			chunkIntegrityBudgetHit := req.ChunkIntegrityBudgetHit
			if !req.LatencyBudgetExceeded && !chunkIntegrityBudgetHit && req.ChunkStore != nil {
				chunkIntegrityBudgetHit = !req.ChunkStore.ReferenceBudgetAvailableAfterInput(req.SessionID, len(block.Text), chunkMinBytes)
			}
			if !req.LatencyBudgetExceeded && !chunkIntegrityBudgetHit && req.ChunkStore != nil {
				if reservedBytes := chunkPriorityPlan.higherPriorityBudgetBytes(msgIdx, blockIdx); reservedBytes > 0 {
					minReferenceBytes := chunkMinBytes
					if minReferenceBytes <= 0 {
						minReferenceBytes = 1
					}
					if !req.ChunkStore.ReferenceBudgetAvailableAfterInput(req.SessionID, len(block.Text), minReferenceBytes+reservedBytes) {
						chunkIntegrityBudgetHit = true
					}
				}
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
				ChunkMinBytes:            chunkMinBytes,
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
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, "", proxyLayer0MechanismCapturedOut, evidence.ActionFullPass, policy.Reason, 0, 0, workload, req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio))
				continue
			}
			readDeltaAttempted := policy.ReadDelta && readDeltaEligible(req.SessionID, commandLine)
			afterText, changed := "", false
			mechanism := proxyLayer0MechanismReadDelta
			chunkReport := chunkdedup.EncodeResult{}
			chunkAllowed := chunkDedupAllowedForCommand(commandLine, readCommand)
			// The delta guard protects Codex server state, not just output shape.
			// Safe output classes only narrow the broader structured mutation guard.
			statefulDeltaBlockedForBlock := req.StatefulDeltaMutationBlocked
			structuredMutationBlockedForBlock := req.StructuredMutationBlocked && !statefulSafeToolOutputBlock
			candidateEvidenceDecision := func(mechanism proxyLayer0Mechanism, action evidence.Action, reason string) evidence.BlockDecision {
				return proxyLayer0EvidenceDecision(commandLine, block.Text, afterText, mechanism, action, reason, countBeforeTokens(), countCandidateTokens(afterText), workload, req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio)
			}
			guardedCandidateEvidenceDecision := func(mechanism proxyLayer0Mechanism, reason string) evidence.BlockDecision {
				before := countBeforeTokens()
				return proxyLayer0EvidenceDecision(commandLine, block.Text, block.Text, mechanism, evidence.ActionFullPass, reason, before, before, workload, req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio)
			}
			evidenceStart := len(stats.EvidenceDecisions)
			observationMechanism := proxyLayer0Mechanism("")
			observationReason := ""
			noteObservation := func(mechanism proxyLayer0Mechanism, reason string) {
				if observationMechanism != "" || mechanism == "" || proxyLayer0ObservationReason(mechanism, reason) == "" {
					return
				}
				observationMechanism = mechanism
				observationReason = reason
			}
			recordObserveOnlyEvidence := func() {
				if observationMechanism == "" || len(stats.EvidenceDecisions) != evidenceStart {
					return
				}
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0ObservationEvidenceDecision(commandLine, observationMechanism, observationReason, workload))
			}
			recordChunkIntegrityBudgetFullPass := func() {
				minBytes := chunkMinBytes
				if minBytes <= 0 {
					minBytes = 1
				}
				if chunkIntegrityBudgetHit && req.ChunkStore != nil && req.SessionID != "" && !req.HostBudgetExceeded && !req.LatencyBudgetExceeded && chunkAllowed && len(block.Text) >= minBytes {
					before := countBeforeTokens()
					stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, block.Text, proxyLayer0MechanismChunkDedup, evidence.ActionFullPass, "session_integrity_budget", before, before, workload, req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio))
					req.ChunkStore.Observe(req.SessionID, []byte(block.Text))
				}
			}
			downstreamStateGuardReason := ""
			if req.HistoryMutationGuardReason != "" {
				downstreamStateGuardReason = req.HistoryMutationGuardReason
			} else if req.StatefulDeltaMutationBlocked {
				downstreamStateGuardReason = "wss_stateful_delta_mutation_proof_gate"
			}
			if readCommand && downstreamStateGuardReason != "" {
				if policy.ReadDelta {
					if readDeltaAttempted {
						stats.ReadDeltaAttempts++
						latencyStart := time.Now()
						_, readChanged, cacheReason, _ := compactProxyReadDeltaWithDecision(req.SessionID, req.TurnID, commandLine, block.Text, readCtx, req.RecentFullPassTurns)
						stats.ReadDeltaLatencyNs += time.Since(latencyStart).Nanoseconds()
						action := proxyLayer0CacheMiss
						if readChanged {
							action = proxyLayer0CacheHit
						}
						stats.CacheEvents = append(stats.CacheEvents, proxyLayer0CacheEvent{
							Mechanism: savingspolicy.CodexMechanismReadDelta,
							Action:    action,
							Reason:    cacheReason,
						})
						if !readChanged {
							stats.ReadDeltaMisses++
							stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0ObservationEvidenceDecision(commandLine, proxyLayer0MechanismReadDelta, cacheReason, workload))
						}
					}
					stats.EvidenceDecisions = append(stats.EvidenceDecisions, guardedCandidateEvidenceDecision(proxyLayer0MechanismReadDelta, downstreamStateGuardReason))
				}
				if policy.ChunkDedup && chunkAllowed {
					stats.EvidenceDecisions = append(stats.EvidenceDecisions, guardedCandidateEvidenceDecision(proxyLayer0MechanismChunkDedup, downstreamStateGuardReason))
					if req.ChunkStore != nil {
						req.ChunkStore.Observe(req.SessionID, []byte(block.Text))
					}
				}
				continue
			}
			statefulDeltaOutputMutationAllowed := wssSearchProofAllowed
			if !readCommand && statefulDeltaBlockedForBlock && !statefulDeltaOutputMutationAllowed {
				if policy.RepeatedOutput {
					latencyStart := time.Now()
					_, repeated, cacheReason := compactProxyRepeatedToolOutputWithKeyDetailed(req.SessionID, toolKey, commandLine, block.Text)
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
					if !repeated {
						stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0ObservationEvidenceDecision(commandLine, proxyLayer0MechanismRepeatedOut, cacheReason, workload))
					}
					stats.EvidenceDecisions = append(stats.EvidenceDecisions, guardedCandidateEvidenceDecision(proxyLayer0MechanismRepeatedOut, "wss_stateful_delta_mutation_proof_gate"))
				}
				reason := "wss_stateful_delta_mutation_proof_gate"
				if wssSearchOutputBlocked {
					reason = "wss_search_output_risk_gate"
				}
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, guardedCandidateEvidenceDecision(proxyLayer0MechanismCapturedOut, reason))
				continue
			}
			if readDeltaAttempted {
				stats.ReadDeltaAttempts++
			}
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
					noteObservation(proxyLayer0MechanismReadDelta, cacheReason)
				}
			}
			if readCommand && !changed {
				if policy.ChunkDedup && chunkAllowed {
					latencyStart := time.Now()
					afterText, changed, mechanism, chunkReport = compactProxyChunkDedup(req.ChunkStore, req.SessionID, block.Text, chunkMinBytes, req.ChunkDedupMaxRefPct)
					stats.ChunkDedupLatencyNs += time.Since(latencyStart).Nanoseconds()
				}
				if !changed {
					recordChunkIntegrityBudgetFullPass()
					recordObserveOnlyEvidence()
					continue
				}
			}
			preFilterRepeated := false
			if !readCommand && workload == savingspolicy.CodexWorkloadSearch && !wssSearchOutputBlocked && !statefulDeltaBlockedForBlock && policy.RepeatedOutput {
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
				} else {
					noteObservation(proxyLayer0MechanismRepeatedOut, cacheReason)
				}
			}
			if wssSearchOutputBlocked {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, "", proxyLayer0MechanismCapturedOut, evidence.ActionFullPass, "wss_search_output_risk_gate", 0, 0, workload, req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio))
			}
			if !changed && !wssSearchOutputBlocked && !readCommand && wssSearchProofAllowed {
				latencyStart := time.Now()
				afterText, changed = compactProxyLayer0CapturedOutputFirst(commandLine, block.Text, readCtx)
				stats.FilterLatencyNs += time.Since(latencyStart).Nanoseconds()
				if changed {
					mechanism = proxyLayer0MechanismCapturedOut
				}
			}
			if !changed && !wssSearchOutputBlocked {
				latencyStart := time.Now()
				afterText, changed, mechanism = compactProxyLayer0TextDetailed(commandLine, block.Text, readCtx)
				stats.FilterLatencyNs += time.Since(latencyStart).Nanoseconds()
			}
			searchDeltaProofCandidate := workload == savingspolicy.CodexWorkloadSearch &&
				wssSearchProofAllowed &&
				proxyWSSSearchProofMechanism(mechanism)
			if statefulDeltaBlockedForBlock && mechanism != proxyLayer0MechanismCapturedOut {
				searchDeltaProofCandidate = false
			}
			if changed && structuredMutationBlockedForBlock && !searchDeltaProofCandidate &&
				(mechanism == proxyLayer0MechanismCapturedOut || mechanism == proxyLayer0MechanismCodexEnvelope) {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, candidateEvidenceDecision(mechanism, evidence.ActionFullPass, "wss_stateful_structured_mutation_guard"))
				changed = false
				afterText = ""
			}
			if changed && req.HistoryMutationGuardReason != "" && proxyLayer0DownstreamStateMechanism(mechanism) {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, candidateEvidenceDecision(mechanism, evidence.ActionFullPass, req.HistoryMutationGuardReason))
				changed = false
				afterText = ""
			}
			if changed && proxyLayer0CacheBustCandidateDemoted(req, commandLine, block.Text, mechanism) {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, candidateEvidenceDecision(mechanism, evidence.ActionFullPass, "cache_bust_guard"))
				changed = false
				afterText = ""
			}
			if changed && (!statefulDeltaBlockedForBlock || searchDeltaProofCandidate) && req.Route == codexLayer0RouteWSSPhaseF &&
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
			if !readCommand && !preFilterRepeated && !wssSearchOutputBlocked && !statefulDeltaBlockedForBlock && candidateEligible && policy.RepeatedOutput {
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
				} else {
					noteObservation(proxyLayer0MechanismRepeatedOut, cacheReason)
				}
			}
			if !changed && policy.ChunkDedup && chunkAllowed {
				latencyStart := time.Now()
				afterText, changed, mechanism, chunkReport = compactProxyChunkDedup(req.ChunkStore, req.SessionID, candidateText, chunkMinBytes, req.ChunkDedupMaxRefPct)
				stats.ChunkDedupLatencyNs += time.Since(latencyStart).Nanoseconds()
			}
			if !changed {
				recordChunkIntegrityBudgetFullPass()
				recordObserveOnlyEvidence()
				continue
			}
			if proxyLayer0CacheBustCandidateDemoted(req, commandLine, block.Text, mechanism) {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, candidateEvidenceDecision(mechanism, evidence.ActionFullPass, "cache_bust_guard"))
				continue
			}
			if req.HistoryMutationGuardReason != "" && proxyLayer0DownstreamStateMechanism(mechanism) {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, candidateEvidenceDecision(mechanism, evidence.ActionFullPass, req.HistoryMutationGuardReason))
				continue
			}
			if statefulDeltaBlockedForBlock && !searchDeltaProofCandidate {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, candidateEvidenceDecision(mechanism, evidence.ActionFullPass, "wss_stateful_delta_mutation_proof_gate"))
				continue
			}
			before := countBeforeTokens()
			afterTokens := countCandidateTokens(afterText)
			if afterTokens < before {
				cow.setText(msgIdx, blockIdx, afterText)
				stats.TokensSaved += before - afterTokens
				stats.BlocksModified++
				if key := proxyLayer0CacheBustClassKey(mechanism, commandLine, block.Text); key != "" {
					if stats.CacheBustClassKeys == nil {
						stats.CacheBustClassKeys = make(map[string]struct{}, 1)
					}
					stats.CacheBustClassKeys[key] = struct{}{}
				}
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
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, afterText, mechanism, evidence.ActionApplied, "positive_net_savings", before, afterTokens, workload, req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio))
			} else {
				stats.EvidenceDecisions = append(stats.EvidenceDecisions, proxyLayer0EvidenceDecision(commandLine, block.Text, afterText, mechanism, evidence.ActionSkipped, "negative_or_zero_net_savings", before, afterTokens, workload, req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio))
			}
		}
	}

	if cow.out == nil {
		return codexLayer0Result{Messages: req.Messages, Stats: stats.finish(started)}
	}
	return codexLayer0Result{Messages: cow.out, Stats: stats.finish(started)}
}

func proxyPlanChunkDedupPriority(req codexLayer0Request, toolUses map[string]types.ContentBlock) proxyChunkDedupPriorityPlan {
	if !req.ChunkDedupEnabled || req.ChunkStore == nil || strings.TrimSpace(req.SessionID) == "" {
		return proxyChunkDedupPriorityPlan{}
	}
	plan := proxyChunkDedupPriorityPlan{ByBlock: make(map[[2]int]proxyChunkDedupPriorityCandidate)}
	for msgIdx, msg := range req.Messages {
		for blockIdx, block := range msg.Content {
			if block.Type != "tool_result" || len(block.Text) == 0 {
				continue
			}
			use, _ := proxyResolveToolUseDetailed(block, toolUses)
			commandLine := proxyLayer0CommandLine(use)
			if commandLine == "" {
				commandLine = proxyInferCommandLineFromToolResult(block.Text)
			}
			if commandLine == "" || proxyCommandLineInvokesReconc(commandLine) {
				continue
			}
			readCommand := readRequestFromCommandLine(commandLine).FilePath != ""
			if !chunkDedupAllowedForCommand(commandLine, readCommand) {
				continue
			}
			minBytes := proxyScaledChunkDedupMinBytes(req.ChunkDedupMinBytes, len(block.Text), req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio)
			if len(block.Text) < minBytes {
				continue
			}
			budgetBytes := proxyChunkDedupCandidateBudgetBytes(len(block.Text), req.ChunkDedupMaxRefPct)
			score := proxyFootprintScoreWithEstimate(len(block.Text)/4, 0, req.TurnSeq, req.RemainingTurnsEstimate, req.CachedPriceRatio)
			if req.UniformChunkDedupBudget {
				score = proxyChunkDedupUniformPriorityScore(len(plan.Candidates))
			}
			candidate := proxyChunkDedupPriorityCandidate{
				Key:         [2]int{msgIdx, blockIdx},
				Order:       len(plan.Candidates),
				Score:       score,
				BudgetBytes: budgetBytes,
			}
			plan.Candidates = append(plan.Candidates, candidate)
			plan.ByBlock[candidate.Key] = candidate
		}
	}
	if len(plan.Candidates) == 0 {
		return proxyChunkDedupPriorityPlan{}
	}
	return plan
}

func proxyChunkDedupUniformPriorityScore(order int) int {
	const base = 1_000_000_000
	if order < 0 {
		return base
	}
	if order >= base {
		return 1
	}
	return base - order
}

func proxyChunkDedupCandidateBudgetBytes(outputBytes int, maxReferencePercent int) int {
	if outputBytes <= 0 {
		return 1
	}
	if maxReferencePercent <= 0 || maxReferencePercent > 100 {
		maxReferencePercent = 100
	}
	budgetBytes := outputBytes * maxReferencePercent / 100
	if budgetBytes <= 0 {
		return 1
	}
	return budgetBytes
}

func (p proxyChunkDedupPriorityPlan) higherPriorityBudgetBytes(msgIdx int, blockIdx int) int {
	current, ok := p.ByBlock[[2]int{msgIdx, blockIdx}]
	if !ok || current.Score <= 0 {
		return 0
	}
	total := 0
	for _, candidate := range p.Candidates {
		if candidate.Key == current.Key || candidate.Score <= current.Score {
			continue
		}
		total += candidate.BudgetBytes
	}
	return total
}

func proxyLayer0DownstreamStateMechanism(mechanism proxyLayer0Mechanism) bool {
	switch mechanism {
	case proxyLayer0MechanismReadDelta, proxyLayer0MechanismStaleRead, proxyLayer0MechanismObsoletePrune, proxyLayer0MechanismChunkDedup:
		return true
	default:
		return false
	}
}

type messageCow struct {
	original []types.Message
	out      []types.Message
	cloned   []bool
}

func (c *messageCow) setText(msgIdx int, blockIdx int, text string) {
	if c.out == nil {
		c.out = make([]types.Message, len(c.original))
		copy(c.out, c.original)
		c.cloned = make([]bool, len(c.original))
	}
	if !c.cloned[msgIdx] {
		c.out[msgIdx].Content = append([]types.ContentBlock(nil), c.original[msgIdx].Content...)
		c.cloned[msgIdx] = true
	}
	c.out[msgIdx].Content[blockIdx].Text = text
}

func proxyLayer0EvidenceDecision(commandLine string, beforeText string, afterText string, mechanism proxyLayer0Mechanism, action evidence.Action, reason string, beforeTokens int, afterTokens int, workload savingspolicy.CodexWorkload, turnSeq int, remainingTurnsEstimate int, cachedPriceRatio float64) evidence.BlockDecision {
	argv := strings.Fields(commandLine)
	analysis := evidence.Analyze(argv, []byte(beforeText))
	preserved := proxyLayer0PreservedEvidence(mechanism, workload)
	safety := proxyLayer0EvidenceSafety(mechanism)
	recovery := proxyLayer0EvidenceRecovery(mechanism)
	if action == evidence.ActionFullPass && beforeText != "" && afterText == "" && beforeTokens == 0 && afterTokens == 0 {
		beforeTokens = tokens.Estimate(len(beforeText))
		if beforeTokens == 0 {
			beforeTokens = 1
		}
		afterTokens = beforeTokens
	}
	var decision evidence.BlockDecision
	if afterText == "" && beforeTokens == 0 && afterTokens == 0 {
		decision = evidence.DecisionFromObservation(0, string(mechanism), safety, action, reason, analysis, preserved, recovery, 0, 0)
	} else {
		decision = evidence.DecisionFromObservation(0, string(mechanism), safety, action, reason, analysis, preserved, recovery, beforeTokens, afterTokens)
	}
	if class := wssToolCommandClass(commandLine); class != "" {
		decision.CommandClass = class
	}
	decision.FootprintScore = proxyFootprintScoreWithEstimate(decision.OriginalTokens, decision.SavedTokens, turnSeq, remainingTurnsEstimate, cachedPriceRatio)
	decision.FootprintScoreBucket = proxyFootprintScoreBucketFromScore(decision.FootprintScore)
	return decision
}

func proxyLayer0ObservationEvidenceDecision(commandLine string, mechanism proxyLayer0Mechanism, reason string, workload savingspolicy.CodexWorkload) evidence.BlockDecision {
	decision := evidence.DecisionFromObservation(
		0,
		string(mechanism),
		proxyLayer0EvidenceSafety(mechanism),
		evidence.ActionShadow,
		proxyLayer0ObservationReason(mechanism, reason),
		proxyLayer0ObservationAnalysis(workload),
		proxyLayer0PreservedEvidence(mechanism, workload),
		"wire unchanged; observe-only state update",
		0,
		0,
	)
	if class := wssToolCommandClass(commandLine); class != "" {
		decision.CommandClass = class
	}
	return decision
}

func proxyLayer0ObservationAnalysis(workload savingspolicy.CodexWorkload) evidence.Analysis {
	switch workload {
	case savingspolicy.CodexWorkloadSearch:
		return evidence.Analysis{ContentClass: evidence.ContentSearch, Signals: []evidence.Signal{evidence.SignalCount, evidence.SignalPath}}
	case savingspolicy.CodexWorkloadRead:
		return evidence.Analysis{ContentClass: evidence.ContentCode, Signals: []evidence.Signal{evidence.SignalPath, evidence.SignalRecency}}
	default:
		return evidence.Analysis{ContentClass: evidence.ContentPlain, Signals: []evidence.Signal{evidence.SignalDedupe}}
	}
}

func proxyLayer0ObservationReason(mechanism proxyLayer0Mechanism, reason string) string {
	reason = strings.TrimSpace(reason)
	switch reason {
	case "first_observation_seeded",
		"archive_unavailable_full_pass",
		"previous_content_unavailable_full_pass",
		"changed_non_delta_output_full_pass",
		"delta_not_shorter_full_pass",
		"no_delta_full_pass",
		"recently_edited_full_pass",
		"recent_full_pass_window",
		"missing_path_or_session",
		"missing_key_session_or_short_output",
		"missing_session",
		"missing_key",
		"short_output",
		"not_read_command",
		"readcache_error",
		"home_error",
		"full_pass":
	default:
		return ""
	}
	switch mechanism {
	case proxyLayer0MechanismReadDelta:
		return "read_delta_" + reason
	case proxyLayer0MechanismRepeatedOut:
		return "repeated_output_" + reason
	default:
		return string(mechanism) + "_" + reason
	}
}

func proxyFootprintScoreBucket(originalTokens int, savedTokens int, turnSeq int) string {
	return proxyFootprintScoreBucketFromScore(proxyFootprintScore(originalTokens, savedTokens, turnSeq))
}

func proxyFootprintScore(originalTokens int, savedTokens int, turnSeq int) int {
	return proxyFootprintScoreWithCachedPriceRatio(originalTokens, savedTokens, turnSeq, proxyDefaultCachedPriceRatio)
}

const proxyDefaultCachedPriceRatio = 0.10

func proxyFootprintRemainingTurnsEstimate(turnSeq int) int {
	switch {
	case turnSeq > 0 && turnSeq <= 3:
		return 70
	case turnSeq > 3 && turnSeq <= 8:
		return 30
	default:
		return 0
	}
}

func proxyFootprintScoreWithCachedPriceRatio(originalTokens int, savedTokens int, turnSeq int, cachedPriceRatio float64) int {
	return proxyFootprintScoreWithEstimate(originalTokens, savedTokens, turnSeq, 0, cachedPriceRatio)
}

func proxyFootprintScoreWithEstimate(originalTokens int, savedTokens int, turnSeq int, remainingTurnsEstimate int, cachedPriceRatio float64) int {
	tokens := savedTokens
	if tokens <= 0 {
		tokens = originalTokens
	}
	if tokens <= 0 {
		return 0
	}
	if cachedPriceRatio <= 0 {
		cachedPriceRatio = proxyDefaultCachedPriceRatio
	}
	if remainingTurnsEstimate <= 0 {
		remainingTurnsEstimate = proxyFootprintRemainingTurnsEstimate(turnSeq)
	}
	equivalentTurns := 1 + float64(remainingTurnsEstimate)*cachedPriceRatio
	return int(math.Round(float64(tokens) * equivalentTurns))
}

func proxyFootprintScoreBucketFromScore(score int) string {
	switch {
	case score <= 0:
		return ""
	case score >= 32000:
		return "high"
	case score >= 8000:
		return "mid"
	default:
		return "low"
	}
}

func proxyScaledChunkDedupMinBytes(baseMinBytes int, outputBytes int, turnSeq int, remainingTurnsEstimate int, cachedPriceRatio float64) int {
	if baseMinBytes <= 1 || outputBytes <= 0 || turnSeq <= 0 {
		return baseMinBytes
	}
	// T359: only loosen on clearly high compounded footprint. Do not raise
	// thresholds here; that needs a proven remaining-turn estimator.
	estimatedTokens := outputBytes / 4
	if estimatedTokens <= 0 {
		estimatedTokens = 1
	}
	if proxyFootprintScoreBucketFromScore(proxyFootprintScoreWithEstimate(estimatedTokens, 0, turnSeq, remainingTurnsEstimate, cachedPriceRatio)) != "high" {
		return baseMinBytes
	}
	scaled := baseMinBytes / 2
	if scaled < 1 {
		return 1
	}
	return scaled
}

func proxyLayer0EvidenceSafety(mechanism proxyLayer0Mechanism) evidence.SafetyClass {
	switch mechanism {
	case proxyLayer0MechanismReadDelta, proxyLayer0MechanismRepeatedOut:
		return evidence.SafetyExact
	case proxyLayer0MechanismChunkDedup, proxyLayer0MechanismStaleRead, proxyLayer0MechanismObsoletePrune:
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
	case proxyLayer0MechanismStaleRead:
		return "re-read the path; newer full read remains in context"
	case proxyLayer0MechanismObsoletePrune:
		return "re-read the path after the edit"
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
	case proxyLayer0MechanismStaleRead:
		return []string{"file path", "superseding read turn", "newer full read"}
	case proxyLayer0MechanismObsoletePrune:
		return []string{"file path", "edit turn", "post-edit context"}
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

func proxyHistoryMutationEvidenceDecision(mechanism proxyLayer0Mechanism, action evidence.Action, reason string, beforeTokens int, afterTokens int, turnSeq int, cachedPriceRatio float64) evidence.BlockDecision {
	analysis := evidence.Analysis{
		ContentClass: evidence.ContentUnknown,
		Signals:      []evidence.Signal{evidence.SignalPath, evidence.SignalRecency},
	}
	decision := evidence.DecisionFromObservation(
		0,
		string(mechanism),
		proxyLayer0EvidenceSafety(mechanism),
		action,
		reason,
		analysis,
		proxyLayer0PreservedEvidence(mechanism, savingspolicy.CodexWorkloadUnknown),
		proxyLayer0EvidenceRecovery(mechanism),
		beforeTokens,
		afterTokens,
	)
	decision.FootprintScore = proxyFootprintScoreWithCachedPriceRatio(decision.OriginalTokens, decision.SavedTokens, turnSeq, cachedPriceRatio)
	decision.FootprintScoreBucket = proxyFootprintScoreBucketFromScore(decision.FootprintScore)
	return decision
}

func proxyWSSSearchOutputRisk(commandLine, text string, workload savingspolicy.CodexWorkload) bool {
	if proxyWSSSearchOutputCommandRisk(commandLine, workload) {
		return true
	}
	return proxyToolResultLooksLikeSearchOutput(text)
}

func proxyWSSSearchOutputCommandRisk(commandLine string, workload savingspolicy.CodexWorkload) bool {
	if proxyWSSPathListOutputReducerEligible(commandLine) {
		return false
	}
	if workload == savingspolicy.CodexWorkloadSearch {
		return true
	}
	return proxyWSSSearchOutputReducerEligible(commandLine)
}

func proxyWSSPathListOutputReducerEligible(commandLine string) bool {
	_, filterCommandLine := proxyLayer0FilterCommandForCompaction(commandLine)
	return filter.PathListOutputReducerEligibleFromCommandLine(filterCommandLine)
}

func proxyWSSSearchOutputReducerEligible(commandLine string) bool {
	if filter.SearchOutputKeyFromCommandLine(commandLine) != "" {
		return true
	}
	workDir, filterCommandLine := proxyLayer0FilterCommandForCompaction(commandLine)
	return filter.SearchOutputReducerEligibleFromCommandLine(filterCommandLine, workDir)
}

func proxyWSSSearchOutputProofAllowed(commandLine string, use types.ContentBlock, commandFromToolUse bool, workload savingspolicy.CodexWorkload) bool {
	allowed, _ := proxyWSSSearchOutputProofDecision(commandLine, use, commandFromToolUse, workload, true, false)
	return allowed
}

func proxyWSSSearchOutputProofDecision(commandLine string, use types.ContentBlock, commandFromToolUse bool, workload savingspolicy.CodexWorkload, latchEnabled bool, statefulDeltaBlocked bool) (bool, string) {
	if !latchEnabled {
		return false, "latch_disabled"
	}
	if !commandFromToolUse || workload != savingspolicy.CodexWorkloadSearch {
		if !commandFromToolUse {
			return false, "tool_use_unbound"
		}
		return false, "workload_not_search"
	}
	if strings.TrimSpace(use.ToolName) == "" && strings.TrimSpace(use.ToolInput) == "" {
		return false, "tool_use_empty"
	}
	if !proxyWSSSearchOutputReducerEligible(commandLine) {
		return false, "reducer_ineligible"
	}
	if statefulDeltaBlocked && !proxyWSSSearchOutputDeltaProofAllowed(commandLine) {
		return false, "delta_key_missing"
	}
	return true, "allowed"
}

func proxyWSSSearchOutputDeltaProofAllowed(commandLine string) bool {
	_, filterCommandLine := proxyLayer0FilterCommandForCompaction(commandLine)
	return filter.SearchOutputKeyFromCommandLine(filterCommandLine) != ""
}

func proxyWSSSearchProofMechanism(mechanism proxyLayer0Mechanism) bool {
	return mechanism == proxyLayer0MechanismCapturedOut ||
		mechanism == proxyLayer0MechanismCodexEnvelope
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
	toolUses := mergedProxyToolUseIndex(proxyToolUseIndex(messages), rememberedToolUses)
	return proxyEditedPathsFromMessagesWithToolUses(messages, toolUses)
}

func proxyEditedPathsFromMessagesWithToolUses(messages []types.Message, toolUses map[string]types.ContentBlock) []string {
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
	if proxyInferredPlainPathListCommand(commandLine) {
		out, changed := compactProxyInferredPlainPathList(text)
		if changed {
			return out, true, proxyLayer0MechanismCodexEnvelope
		}
		return "", false, ""
	}
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

func compactProxyLayer0CapturedOutputFirst(commandLine, text string, ctx filter.FileReadContext) (string, bool) {
	filterWorkDir, filterCommandLine := proxyLayer0FilterCommandForCompaction(commandLine)
	compacted, changed := filter.CompactCapturedOutputWithContext(filterWorkDir, filterCommandLine, text, 0, ctx)
	if changed {
		return string(compacted), true
	}
	if _, payload, ok := splitCodexExecEnvelope(text); ok {
		compacted, changed = filter.CompactCapturedOutputWithContext(filterWorkDir, filterCommandLine, payload, 0, ctx)
		if changed {
			return string(compacted), true
		}
	}
	return "", false
}

func proxyInferredPlainPathListCommand(commandLine string) bool {
	return strings.TrimSpace(commandLine) == proxyInferredPlainPathListCommandLine
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
	if proxyLooksLikeGitDiffStatOutput(payload) {
		return "git diff --stat"
	}
	if proxyLooksLikeGitShowStatOutput(payload) {
		return "git show --stat"
	}
	if proxyLooksLikeGitDiffNameStatusOutput(payload) {
		return "git diff --name-status"
	}
	if proxyLooksLikeGitLogOnelineOutput(payload) {
		return "git log --oneline -n 200"
	}
	if proxyLooksLikeWcOutput(payload) {
		return "wc -l"
	}
	if proxyLooksLikePlainPathListOutput(payload) {
		return proxyInferredPlainPathListCommandLine
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
	for len(payload) > 0 && nonEmpty < 12 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Total output lines:") {
			continue
		}
		nonEmpty++
		if proxyLooksLikeSearchResultLine(line) {
			matches++
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

func proxyLooksLikeGitDiffStatOutput(payload string) bool {
	_, ok := filter.TryCompactGitDiff([]string{"git", "diff", "--stat"}, []byte(payload))
	return ok
}

func proxyLooksLikeGitShowStatOutput(payload string) bool {
	return wssSafeGitShowStatOutput("git show --stat", payload)
}

func proxyLooksLikeGitDiffNameStatusOutput(payload string) bool {
	return wssSafeGitDiffNameStatusPathListOutput("git diff --name-status", payload)
}

func proxyLooksLikeGitLogOnelineOutput(payload string) bool {
	return wssGitLogOnelinePayloadSafe(payload, wssSafeGitLogOnelineMaxCommits)
}

func proxyLooksLikeWcOutput(payload string) bool {
	return wssSafeWcOutput("wc -l", payload)
}

func proxyLooksLikePlainPathListOutput(payload string) bool {
	if !wssSafeBoundedPlainPathListPayload(payload, wssSafeRgFilesOutputMaxBytes, wssSafeRgFilesOutputMaxEntries) {
		return false
	}
	_, ok := filter.TryCompactPlainPathListOutput([]byte(payload))
	return ok
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

func compactProxyInferredPlainPathList(text string) (string, bool) {
	header, payload, ok := splitCodexExecEnvelope(text)
	if !ok {
		return "", false
	}
	compacted, changed := filter.TryCompactPlainPathListOutput([]byte(payload))
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

func mergedProxyToolUseIndex(index map[string]types.ContentBlock, remembered map[string]types.ContentBlock) map[string]types.ContentBlock {
	if len(index) == 0 {
		if len(remembered) == 0 {
			return nil
		}
		out := make(map[string]types.ContentBlock, len(remembered))
		for id, use := range remembered {
			out[id] = use
		}
		return out
	}
	if len(remembered) == 0 {
		return index
	}
	out := make(map[string]types.ContentBlock, len(index)+len(remembered))
	for id, use := range index {
		out[id] = use
	}
	for id, use := range remembered {
		if _, ok := out[id]; !ok {
			out[id] = use
		}
	}
	return out
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
	if normalized := filter.NormalizeReadCommandLine(commandLine, workdir); normalized != "" {
		return normalized
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

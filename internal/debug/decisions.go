package debug

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

func writeDecisionLine(w io.Writer, line []byte) error {
	_, err := fmt.Fprintf(w, "%s\n", line)
	return err
}

// decisionsLogStatEvery rate-limits the external-rotation check on the open
// decisions-log handle. Keeping the handle open removes the per-record
// open/MkdirAll/close syscall churn from the request hot path; the periodic
// stat re-opens the file when something rotated or removed it underneath.
const decisionsLogStatEvery = 30 * time.Second

// DecisionEntry records one compression decision for one content block.
// Written per sub-layer operation. Aggregated at DEBUG level into RequestSummary.
type DecisionEntry struct {
	Timestamp    time.Time         `json:"ts"`
	RequestID    string            `json:"req_id"`
	MessageIdx   int               `json:"msg_idx"`
	BlockIdx     int               `json:"block_idx"`
	ContentType  string            `json:"content_type"` // "text", "tool_result", "image"
	ContentClass string            `json:"content_class,omitempty"`
	Layer        int               `json:"layer"`     // 0, 1, 2, 3
	SubLayer     string            `json:"sub_layer"` // "json_compact", "dedup", "ansi_strip", etc.
	Action       string            `json:"action"`    // "compressed", "skipped", "passthrough", "short_circuit"
	Reason       string            `json:"reason"`
	SafetyClass  string            `json:"safety_class,omitempty"`
	Signals      []string          `json:"signals,omitempty"`
	Recovery     string            `json:"recovery,omitempty"`
	TokensBefore int               `json:"tokens_before"`
	TokensAfter  int               `json:"tokens_after"`
	SavedTokens  int               `json:"saved"`
	Settings     map[string]string `json:"settings,omitempty"`
}

// SubLayerBreakdown tracks aggregate savings for one sub-layer within a request.
type SubLayerBreakdown struct {
	Blocks int `json:"blocks"`
	Saved  int `json:"saved"`
}

// Layer1DecisionSummary records the content-free per-sub-layer Layer 1 decision
// emitted by the compressor. It carries safety-contract metadata and numeric
// impact only, never raw prompt or tool payload.
type Layer1DecisionSummary struct {
	SubLayer        string `json:"sub_layer"`
	Tier            string `json:"tier"`
	Attempted       bool   `json:"attempted"`
	Applied         bool   `json:"applied"`
	Reason          string `json:"reason"`
	SavedTokens     int    `json:"saved_tokens,omitempty"`
	RequiresArchive bool   `json:"requires_archive,omitempty"`
	ArchiveWrites   int    `json:"archive_writes,omitempty"`
	Recovery        string `json:"recovery,omitempty"`
	DefaultEligible bool   `json:"default_eligible"`
}

// MechanismAccounting records token impact for one concrete saving or overhead
// mechanism. Saved is gross reduction, Added is extra prompt/context cost, Net
// is Saved-Added. Negative Net is allowed and explicitly marks regressions.
type MechanismAccounting struct {
	Name                 string `json:"name"`
	Layer                int    `json:"layer,omitempty"`
	Source               string `json:"source,omitempty"`
	Count                int    `json:"count"`
	OriginalTokens       int    `json:"original_tokens,omitempty"`
	FinalTokens          int    `json:"final_tokens,omitempty"`
	SavedTokens          int    `json:"saved_tokens,omitempty"`
	AddedTokens          int    `json:"added_tokens,omitempty"`
	NetTokens            int    `json:"net_tokens"`
	Reason               string `json:"reason,omitempty"`
	ContentClass         string `json:"content_class,omitempty"`
	SafetyClass          string `json:"safety_class,omitempty"`
	FootprintScoreBucket string `json:"footprint_score_bucket,omitempty"`
	FootprintScore       int    `json:"footprint_score,omitempty"`
}

// TokenCounts holds before/after token totals for a request.
type TokenCounts struct {
	Original    int     `json:"original"`
	AfterLayer0 int     `json:"after_layer0"`
	AfterLayer1 int     `json:"after_layer1"`
	Final       int     `json:"final"`
	Saved       int     `json:"saved"`
	Ratio       float64 `json:"ratio"` // final/original
}

// PromptCacheSummary records content-free provider-cache hint decisions.
// StablePrefixHash is already truncated and content-derived only; it must
// never contain raw prompt text.
type PromptCacheSummary struct {
	Applied            bool   `json:"applied"`
	Reason             string `json:"reason,omitempty"`
	KeySet             bool   `json:"key_set,omitempty"`
	Retention          string `json:"retention,omitempty"`
	StablePrefixHash   string `json:"stable_prefix_hash,omitempty"`
	StablePrefixTokens int    `json:"stable_prefix_tokens,omitempty"`
}

// ToolPruneSummary records content-free Layer 3 tool-schema pruning telemetry.
type ToolPruneSummary struct {
	Applied       bool   `json:"applied"`
	Reason        string `json:"reason,omitempty"`
	SessionKeySet bool   `json:"session_key_set,omitempty"`
	PrunedTools   int    `json:"pruned_tools,omitempty"`
	AlwaysKept    int    `json:"always_kept,omitempty"`
	SavedTokens   int    `json:"saved_tokens,omitempty"`
	Reattached    int    `json:"reattached,omitempty"`
	Miss          bool   `json:"miss,omitempty"`
	Retry         bool   `json:"retry,omitempty"`
	Cooldown      bool   `json:"cooldown,omitempty"`
}

// PlanDecisionSummary records one dry-run planner decision for observability.
// It is intentionally content-free: only layer names, actions, reasons and
// numeric estimates are emitted so debug logs never duplicate prompt text.
type PlanDecisionSummary struct {
	Layer                 string `json:"layer"`
	Action                string `json:"action"`
	Reason                string `json:"reason"`
	ExpectedSavingsTokens int    `json:"expected_savings_tokens,omitempty"`
	Risk                  string `json:"risk"`
	Confidence            string `json:"confidence"`
}

// PlanSummary records the cross-layer dry-run planner output attached to a
// request. The proxy currently treats it as advice-only telemetry.
type PlanSummary struct {
	Provider       string                `json:"provider,omitempty"`
	Model          string                `json:"model,omitempty"`
	RouteMode      string                `json:"route_mode,omitempty"`
	ContentClasses []string              `json:"content_classes,omitempty"`
	SafetyBlocked  bool                  `json:"safety_blocked"`
	Decisions      []PlanDecisionSummary `json:"decisions,omitempty"`
}

// RequestSummary aggregates all decision entries for one proxy request.
// This is the top-level object returned by "slimference debug last".
type RequestSummary struct {
	RequestID              string                       `json:"req_id"`
	Timestamp              time.Time                    `json:"ts"`
	SessionID              string                       `json:"session_id,omitempty"`
	TurnID                 string                       `json:"turn_id,omitempty"`
	Source                 string                       `json:"source,omitempty"`
	Provider               string                       `json:"provider"`
	Host                   string                       `json:"host,omitempty"`
	Path                   string                       `json:"path,omitempty"`
	ClientFamily           string                       `json:"client_family,omitempty"`
	RouteMode              string                       `json:"route_mode,omitempty"`
	BypassReason           string                       `json:"bypass_reason,omitempty"`
	Model                  string                       `json:"model"`
	TotalMessages          int                          `json:"total_messages"`
	MessagesInWindow       int                          `json:"messages_in_window"`
	MessagesCompressed     int                          `json:"messages_compressed"`
	LayersApplied          []int                        `json:"layers_applied"`
	Tokens                 TokenCounts                  `json:"tokens"`
	Layer1Breakdown        map[string]SubLayerBreakdown `json:"layer1_breakdown"`
	Layer1Decisions        []Layer1DecisionSummary      `json:"layer1_decisions,omitempty"`
	CacheHit               bool                         `json:"cache_hit"`
	CacheReadTokens        int                          `json:"cache_read_tokens"`
	CacheCreateTokens      int                          `json:"cache_create_tokens"`
	ProviderInputTokens    int                          `json:"provider_input_tokens,omitempty"`
	ProviderCachedTokens   int                          `json:"provider_cached_tokens,omitempty"`
	ProviderOutputTokens   int                          `json:"provider_output_tokens,omitempty"`
	OutputTokens           int                          `json:"output_tokens,omitempty"`
	PromptCache            PromptCacheSummary           `json:"prompt_cache,omitempty"`
	ToolPrune              ToolPruneSummary             `json:"tool_prune,omitempty"`
	OutputReduce           OutputReduceSummary          `json:"output_reduce,omitempty"`
	Mechanisms             []MechanismAccounting        `json:"mechanisms,omitempty"`
	EvidenceDecisions      []evidence.BlockDecision     `json:"evidence_decisions,omitempty"`
	DebugFacts             map[string]string            `json:"debug_facts,omitempty"`
	PreviousResponseIDUsed bool                         `json:"previous_response_id_used,omitempty"`
	SecretsRedacted        int                          `json:"secrets_redacted"`
	Errors                 []string                     `json:"errors,omitempty"`
	ProxyLatencyMs         float64                      `json:"proxy_latency_ms"`
	ReReadCount            int                          `json:"re_read_count"`     // T77
	NetSavedTokens         int                          `json:"net_saved_tokens"`  // T77
	AdaptiveWindow         AdaptiveWindowSummary        `json:"adaptive_window"`   // T112
	Plan                   *PlanSummary                 `json:"plan,omitempty"`    // T149 dry-run planner
	Entries                []DecisionEntry              `json:"entries,omitempty"` // only with --trace
	Flight                 *FlightRequestSummary        `json:"flight,omitempty"`
}

type AdaptiveWindowSummary struct {
	Size   int     `json:"size"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// Recorder collects DecisionEntries and RequestSummaries in memory.
// Thread-safe. Keeps the last maxSummaries request summaries in a ring buffer.
type Recorder struct {
	mu           sync.Mutex
	summaries    []RequestSummary // ring buffer, newest at highest index % cap
	head         int              // next write position
	count        int              // filled slots (up to cap)
	cap          int
	decisionsLog string // path for JSONL flush on each request (empty = off)
	writeLineFn  func(io.Writer, []byte) error

	// Decisions-log writer state: one open append handle instead of an
	// open/write/close cycle per record. Guarded by logMu, not mu, so slow
	// disk writes never block ring reads.
	logMu     sync.Mutex
	logFile   *os.File
	logStatAt time.Time
}

// NewRecorder creates a Recorder that retains up to capacity request summaries.
func NewRecorder(capacity int, decisionsLog string) *Recorder {
	if capacity <= 0 {
		capacity = 100
	}
	return &Recorder{
		summaries:    make([]RequestSummary, capacity),
		cap:          capacity,
		decisionsLog: normalizeDecisionsLogPath(decisionsLog),
		writeLineFn:  writeDecisionLine,
	}
}

func normalizeDecisionsLogPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, path[2:])
			}
		}
	}
	return filepath.Clean(path)
}

// Record appends a completed RequestSummary to the ring and optionally flushes to JSONL.
func (r *Recorder) Record(s RequestSummary) {
	s.EnsureEvidenceDecisions()
	s = RedactRequestSummary(s)
	s.EnsureMechanisms()
	s.EnsureFlight()
	r.mu.Lock()
	r.summaries[r.head%r.cap] = s
	r.head++
	if r.count < r.cap {
		r.count++
	}
	path := r.decisionsLog
	r.mu.Unlock()

	if path != "" {
		r.flushJSONL(path, s)
	}
}

func (s *RequestSummary) EnsureMechanisms() {
	if s == nil || len(s.Mechanisms) > 0 {
		return
	}
	s.Mechanisms = BuildMechanismAccounting(*s)
}

func (s *RequestSummary) EnsureEvidenceDecisions() {
	if s == nil || !hasCacheHotZoneEvidence(*s) || hasEvidenceMechanism(s.EvidenceDecisions, "provider_prompt_cache") {
		return
	}
	saved := s.CacheReadTokens + s.ProviderCachedTokens
	added := s.CacheCreateTokens
	action := evidence.ActionSkipped
	if s.PromptCache.Applied || saved > 0 || added > 0 {
		action = evidence.ActionApplied
	}
	reason := strings.TrimSpace(s.PromptCache.Reason)
	if reason == "" {
		reason = cacheDecision(*s)
	}
	decision := evidence.BlockDecision{
		Layer:             2,
		Mechanism:         "provider_prompt_cache",
		ContentClass:      evidence.ContentUnknown,
		SafetyClass:       evidence.SafetyExact,
		Action:            action,
		Reason:            reason,
		Signals:           []evidence.Signal{evidence.SignalCacheHotZone},
		PreservedEvidence: []string{"stable prefix hash", "provider cache read tokens", "provider cache create tokens"},
		Recovery:          "provider-owned cache hint only; fail-open keeps original request shape",
		OriginalTokens:    s.PromptCache.StablePrefixTokens,
		SavedTokens:       saved,
		AddedTokens:       added,
		NetTokens:         saved - added,
		CacheImpact:       cacheDecision(*s),
	}
	s.EvidenceDecisions = append(s.EvidenceDecisions, decision)
}

func hasCacheHotZoneEvidence(s RequestSummary) bool {
	return s.PromptCache.Applied ||
		strings.TrimSpace(s.PromptCache.Reason) != "" ||
		s.PromptCache.StablePrefixTokens > 0 ||
		s.CacheHit ||
		s.CacheReadTokens > 0 ||
		s.CacheCreateTokens > 0 ||
		s.ProviderCachedTokens > 0
}

func hasEvidenceMechanism(decisions []evidence.BlockDecision, mechanism string) bool {
	for _, decision := range decisions {
		if strings.EqualFold(strings.TrimSpace(decision.Mechanism), mechanism) {
			return true
		}
	}
	return false
}

func BuildMechanismAccounting(s RequestSummary) []MechanismAccounting {
	var out []MechanismAccounting
	for _, entry := range s.Entries {
		name := strings.TrimSpace(entry.SubLayer)
		if name == "" {
			name = "unnamed"
		}
		added := 0
		saved := entry.SavedTokens
		if saved < 0 {
			added = -saved
			saved = 0
		}
		out = append(out, MechanismAccounting{
			Name:           name,
			Layer:          entry.Layer,
			Source:         "decision_entry",
			Count:          1,
			OriginalTokens: entry.TokensBefore,
			FinalTokens:    entry.TokensAfter,
			SavedTokens:    saved,
			AddedTokens:    added,
			NetTokens:      saved - added,
			Reason:         entry.Reason,
			ContentClass:   entry.ContentClass,
			SafetyClass:    entry.SafetyClass,
		})
	}
	for name, bd := range s.Layer1Breakdown {
		out = append(out, MechanismAccounting{
			Name:        name,
			Layer:       1,
			Source:      "layer1_breakdown",
			Count:       bd.Blocks,
			SavedTokens: bd.Saved,
			NetTokens:   bd.Saved,
		})
	}
	for _, decision := range s.EvidenceDecisions {
		name := strings.TrimSpace(decision.Mechanism)
		if name == "" || strings.EqualFold(name, "provider_prompt_cache") {
			continue
		}
		out = append(out, MechanismAccounting{
			Name:                 name,
			Layer:                decision.Layer,
			Source:               "evidence_decision",
			Count:                1,
			OriginalTokens:       decision.OriginalTokens,
			FinalTokens:          decision.FinalTokens,
			SavedTokens:          decision.SavedTokens,
			AddedTokens:          decision.AddedTokens,
			NetTokens:            decision.NetTokens,
			Reason:               decision.Reason,
			ContentClass:         string(decision.ContentClass),
			SafetyClass:          string(decision.SafetyClass),
			FootprintScoreBucket: decision.FootprintScoreBucket,
			FootprintScore:       decision.FootprintScore,
		})
	}
	if s.PromptCache.Applied || s.PromptCache.Reason != "" || s.CacheReadTokens > 0 || s.CacheCreateTokens > 0 || s.ProviderCachedTokens > 0 {
		cacheSaved := s.CacheReadTokens + s.ProviderCachedTokens
		out = append(out, MechanismAccounting{
			Name:           "provider_prompt_cache",
			Source:         "cache_accounting",
			Count:          boolCount(s.PromptCache.Applied || cacheSaved > 0 || s.CacheCreateTokens > 0),
			OriginalTokens: s.PromptCache.StablePrefixTokens,
			SavedTokens:    cacheSaved,
			AddedTokens:    s.CacheCreateTokens,
			NetTokens:      cacheSaved - s.CacheCreateTokens,
			Reason:         s.PromptCache.Reason,
		})
	}
	if s.ToolPrune.Applied || s.ToolPrune.Reason != "" || s.ToolPrune.SavedTokens > 0 || s.ToolPrune.Reattached > 0 {
		out = append(out, MechanismAccounting{
			Name:        "tool_prune",
			Layer:       4,
			Source:      "tool_prune",
			Count:       boolCount(s.ToolPrune.Applied),
			SavedTokens: s.ToolPrune.SavedTokens,
			AddedTokens: s.ToolPrune.Reattached,
			NetTokens:   s.ToolPrune.SavedTokens - s.ToolPrune.Reattached,
			Reason:      s.ToolPrune.Reason,
		})
	}
	if s.OutputReduce.Applied || s.OutputReduce.Reason != "" || s.OutputReduce.AddedTokens > 0 {
		out = append(out, MechanismAccounting{
			Name:        "output_reduce_directive",
			Source:      "output_reduce",
			Count:       boolCount(s.OutputReduce.Applied),
			AddedTokens: s.OutputReduce.AddedTokens,
			NetTokens:   -s.OutputReduce.AddedTokens,
			Reason:      s.OutputReduce.Reason,
		})
	}
	if s.Tokens.Original > 0 || s.Tokens.Final > 0 {
		out = append(out, MechanismAccounting{
			Name:           "request_total",
			Source:         "request",
			Count:          1,
			OriginalTokens: s.Tokens.Original,
			FinalTokens:    s.Tokens.Final,
			SavedTokens:    s.Tokens.Saved,
			AddedTokens:    positive(s.OutputReduce.AddedTokens),
			NetTokens:      s.Tokens.Saved - positive(s.OutputReduce.AddedTokens),
			Reason:         s.BypassReason,
		})
	}
	return out
}

func boolCount(v bool) int {
	if v {
		return 1
	}
	return 0
}

func positive(v int) int {
	if v > 0 {
		return v
	}
	return 0
}

// Last returns the most recent n request summaries, newest first.
// Entries are omitted unless withEntries is true.
func (r *Recorder) Last(n int, withEntries bool) []RequestSummary {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || r.count == 0 {
		return nil
	}
	if n > r.count {
		n = r.count
	}
	out := make([]RequestSummary, n)
	for i := 0; i < n; i++ {
		// newest = head-1, second newest = head-2, etc.
		idx := ((r.head-1-i)%r.cap + r.cap) % r.cap
		s := r.summaries[idx]
		if !withEntries {
			s.Entries = nil
		}
		out[i] = s
	}
	return out
}

// FilterHitCount summarizes how many times each sub-layer fired and total tokens saved.
type FilterHitCount struct {
	SubLayer string `json:"sub_layer"`
	Hits     int    `json:"hits"`
	Saved    int    `json:"saved"`
}

// Aggregate computes totals across all stored request summaries.
func (r *Recorder) Aggregate() map[string]SubLayerBreakdown {
	r.mu.Lock()
	defer r.mu.Unlock()
	agg := map[string]SubLayerBreakdown{}
	for i := 0; i < r.count; i++ {
		s := r.summaries[i]
		for name, bd := range s.Layer1Breakdown {
			cur := agg[name]
			cur.Blocks += bd.Blocks
			cur.Saved += bd.Saved
			agg[name] = cur
		}
	}
	return agg
}

// flushJSONL appends the summary as a JSONL line to path through a kept-open
// append handle. Every record is written through immediately (readers such as
// `stats`/`savings` always see current data); only the per-record open/close
// churn is removed.
func (r *Recorder) flushJSONL(path string, s RequestSummary) {
	line, err := json.Marshal(s)
	if err != nil {
		slog.Warn("debug recorder: marshal summary failed",
			slog.String("path", path),
			slog.String("err", err.Error()),
		)
		return
	}
	r.logMu.Lock()
	defer r.logMu.Unlock()
	if !r.ensureLogFileLocked(path) {
		return
	}
	if err := r.writeLineFn(r.logFile, line); err != nil {
		slog.Warn("debug recorder: write decisions log failed",
			slog.String("path", path),
			slog.String("err", err.Error()),
		)
		r.closeLogLocked()
	}
}

// ensureLogFileLocked opens the append handle, or re-opens it when the file
// was rotated or removed externally (checked at most every
// decisionsLogStatEvery). Caller holds logMu.
func (r *Recorder) ensureLogFileLocked(path string) bool {
	now := time.Now()
	if r.logFile != nil {
		if now.Sub(r.logStatAt) < decisionsLogStatEvery {
			return true
		}
		r.logStatAt = now
		if st, err := os.Stat(path); err == nil {
			if fi, ferr := r.logFile.Stat(); ferr == nil && os.SameFile(st, fi) {
				return true
			}
		}
		r.closeLogLocked()
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			slog.Warn("debug recorder: create decisions log directory failed",
				slog.String("path", path),
				slog.String("err", err.Error()),
			)
			return false
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		slog.Warn("debug recorder: open decisions log failed",
			slog.String("path", path),
			slog.String("err", err.Error()),
		)
		return false
	}
	r.logFile = f
	r.logStatAt = now
	return true
}

func (r *Recorder) closeLogLocked() {
	if r.logFile != nil {
		_ = r.logFile.Close()
		r.logFile = nil
	}
}

// AttachProviderUsage enriches the recorded summary for requestID with the
// provider-reported usage from the response (which arrives after the request
// summary was recorded on streaming routes such as Codex WSS) and appends a
// superseding JSONL line under the same request id. Readers keep the newest
// line per request id (ReplaySession dedupes last-wins; tail readers iterate
// newest-first), so the enriched record replaces the request-time one without
// rewriting the log. Returns false when requestID is no longer in the ring.
func (r *Recorder) AttachProviderUsage(requestID string, inputTokens, cachedTokens, createTokens, outputTokens int) bool {
	if r == nil || requestID == "" {
		return false
	}
	var updated *RequestSummary
	r.mu.Lock()
	for i := 0; i < r.count; i++ {
		idx := ((r.head-1-i)%r.cap + r.cap) % r.cap
		if r.summaries[idx].RequestID != requestID {
			continue
		}
		s := &r.summaries[idx]
		if inputTokens > 0 {
			s.ProviderInputTokens = inputTokens
		}
		if cachedTokens > 0 {
			s.ProviderCachedTokens = cachedTokens
		}
		if createTokens > 0 {
			s.CacheCreateTokens = createTokens
		}
		if outputTokens > 0 {
			s.ProviderOutputTokens = outputTokens
		}
		s.Flight = nil
		s.EnsureFlight()
		clone := *s
		updated = &clone
		break
	}
	path := r.decisionsLog
	r.mu.Unlock()
	if updated == nil {
		return false
	}
	if path != "" {
		r.flushJSONL(path, *updated)
	}
	return true
}

// AttachDebugFacts merges content-free diagnostic facts into a recorded summary
// and appends a superseding JSONL line under the same request id. This keeps
// request counts stable while allowing later-arriving transport facts, such as
// WSS socket closure metadata, to persist in the decision log.
func (r *Recorder) AttachDebugFacts(requestID string, facts map[string]string) bool {
	if r == nil || requestID == "" || len(facts) == 0 {
		return false
	}
	var updated *RequestSummary
	r.mu.Lock()
	for i := 0; i < r.count; i++ {
		idx := ((r.head-1-i)%r.cap + r.cap) % r.cap
		if r.summaries[idx].RequestID != requestID {
			continue
		}
		s := &r.summaries[idx]
		if s.DebugFacts == nil {
			s.DebugFacts = make(map[string]string, len(facts))
		}
		for k, v := range facts {
			if strings.TrimSpace(k) == "" {
				continue
			}
			s.DebugFacts[k] = v
		}
		s.Flight = nil
		s.EnsureFlight()
		clone := *s
		updated = &clone
		break
	}
	path := r.decisionsLog
	r.mu.Unlock()
	if updated == nil {
		return false
	}
	if path != "" {
		r.flushJSONL(path, *updated)
	}
	return true
}

// Close releases the decisions-log handle. Safe to call multiple times and on
// a recorder that never wrote.
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.logMu.Lock()
	r.closeLogLocked()
	r.logMu.Unlock()
}

// NopRecorder is a no-op Recorder for when debug recording is disabled.
// Avoids nil checks in the hot path.
type NopRecorder struct{}

func (NopRecorder) Record(s RequestSummary) {
	_ = s
}

func (NopRecorder) Last(_ int, _ bool) []RequestSummary {
	return nil
}

func (NopRecorder) Aggregate() map[string]SubLayerBreakdown {
	return nil
}

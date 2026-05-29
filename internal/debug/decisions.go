package debug

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func writeDecisionLine(f *os.File, line []byte) error {
	_, err := fmt.Fprintf(f, "%s\n", line)
	return err
}

// DecisionEntry records one compression decision for one content block.
// Written per sub-layer operation. Aggregated at DEBUG level into RequestSummary.
type DecisionEntry struct {
	Timestamp    time.Time         `json:"ts"`
	RequestID    string            `json:"req_id"`
	MessageIdx   int               `json:"msg_idx"`
	BlockIdx     int               `json:"block_idx"`
	ContentType  string            `json:"content_type"` // "text", "tool_result", "image"
	Layer        int               `json:"layer"`        // 0, 1, 2, 3
	SubLayer     string            `json:"sub_layer"`    // "json_compact", "dedup", "ansi_strip", etc.
	Action       string            `json:"action"`       // "compressed", "skipped", "passthrough", "short_circuit"
	Reason       string            `json:"reason"`
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

// MechanismAccounting records token impact for one concrete saving or overhead
// mechanism. Saved is gross reduction, Added is extra prompt/context cost, Net
// is Saved-Added. Negative Net is allowed and explicitly marks regressions.
type MechanismAccounting struct {
	Name           string `json:"name"`
	Layer          int    `json:"layer,omitempty"`
	Source         string `json:"source,omitempty"`
	Count          int    `json:"count"`
	OriginalTokens int    `json:"original_tokens,omitempty"`
	FinalTokens    int    `json:"final_tokens,omitempty"`
	SavedTokens    int    `json:"saved_tokens,omitempty"`
	AddedTokens    int    `json:"added_tokens,omitempty"`
	NetTokens      int    `json:"net_tokens"`
	Reason         string `json:"reason,omitempty"`
}

// TokenCounts holds before/after token totals for a request.
type TokenCounts struct {
	Original    int     `json:"original"`
	AfterLayer0 int     `json:"after_layer0"`
	AfterLayer1 int     `json:"after_layer1"`
	AfterLayer2 int     `json:"after_layer2"`
	Final       int     `json:"final"`
	Saved       int     `json:"saved"`
	Ratio       float64 `json:"ratio"` // final/original
}

// Layer2Summary holds Layer 2 stats for a request.
type Layer2Summary struct {
	Applied          bool    `json:"applied"`
	CacheHit         bool    `json:"cache_hit"`
	CoveredRangeFrom int     `json:"covered_range_from"`
	CoveredRangeTo   int     `json:"covered_range_to"`
	OriginalTokens   int     `json:"original_tokens"`
	CompressedTokens int     `json:"compressed_tokens"`
	AnchorCount      int     `json:"anchor_count"`
	CompressionRatio float64 `json:"compression_ratio"`
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

// ToolPruneSummary records content-free Layer 4 tool-schema pruning telemetry.
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
	Layer2                 Layer2Summary                `json:"layer2"`
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
	writeLineFn  func(*os.File, []byte) error
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
	if s.Layer2.Applied || s.Layer2.OriginalTokens > 0 || s.Layer2.CompressedTokens > 0 {
		saved := s.Layer2.OriginalTokens - s.Layer2.CompressedTokens
		if saved < 0 {
			saved = 0
		}
		out = append(out, MechanismAccounting{
			Name:           "layer2_summarization",
			Layer:          2,
			Source:         "layer2",
			Count:          boolCount(s.Layer2.Applied),
			OriginalTokens: s.Layer2.OriginalTokens,
			FinalTokens:    s.Layer2.CompressedTokens,
			SavedTokens:    saved,
			NetTokens:      saved,
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

// flushJSONL appends the summary as a JSONL line to path.
func (r *Recorder) flushJSONL(path string, s RequestSummary) {
	line, err := json.Marshal(s)
	if err != nil {
		slog.Warn("debug recorder: marshal summary failed",
			slog.String("path", path),
			slog.String("err", err.Error()),
		)
		return
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			slog.Warn("debug recorder: create decisions log directory failed",
				slog.String("path", path),
				slog.String("err", err.Error()),
			)
			return
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		slog.Warn("debug recorder: open decisions log failed",
			slog.String("path", path),
			slog.String("err", err.Error()),
		)
		return
	}
	defer f.Close()
	if err := r.writeLineFn(f, line); err != nil {
		slog.Warn("debug recorder: write decisions log failed",
			slog.String("path", path),
			slog.String("err", err.Error()),
		)
	}
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

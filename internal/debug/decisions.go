package debug

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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

// RequestSummary aggregates all decision entries for one proxy request.
// This is the top-level object returned by "slimference debug last".
type RequestSummary struct {
	RequestID          string                       `json:"req_id"`
	Timestamp          time.Time                    `json:"ts"`
	Provider           string                       `json:"provider"`
	Model              string                       `json:"model"`
	TotalMessages      int                          `json:"total_messages"`
	MessagesInWindow   int                          `json:"messages_in_window"`
	MessagesCompressed int                          `json:"messages_compressed"`
	LayersApplied      []int                        `json:"layers_applied"`
	Tokens             TokenCounts                  `json:"tokens"`
	Layer1Breakdown    map[string]SubLayerBreakdown `json:"layer1_breakdown"`
	Layer2             Layer2Summary                `json:"layer2"`
	CacheHit           bool                         `json:"cache_hit"`
	CacheReadTokens    int                          `json:"cache_read_tokens"`
	CacheCreateTokens  int                          `json:"cache_create_tokens"`
	SecretsRedacted    int                          `json:"secrets_redacted"`
	ProxyLatencyMs     float64                      `json:"proxy_latency_ms"`
	Entries            []DecisionEntry              `json:"entries,omitempty"` // only with --trace
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
		decisionsLog: decisionsLog,
		writeLineFn:  writeDecisionLine,
	}
}

// Record appends a completed RequestSummary to the ring and optionally flushes to JSONL.
func (r *Recorder) Record(s RequestSummary) {
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

func (NopRecorder) Record(_ RequestSummary)                 {}
func (NopRecorder) Last(_ int, _ bool) []RequestSummary     { return nil }
func (NopRecorder) Aggregate() map[string]SubLayerBreakdown { return nil }

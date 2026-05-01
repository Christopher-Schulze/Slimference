// Package types defines the core data structures shared across all Slimference packages.
package types

import (
	"sync"
	"time"
)

// Provider represents an LLM API provider.
type Provider int

const (
	Anthropic Provider = iota
	OpenAI
	// CodexChatGPT is OpenAI's Codex CLI (the ChatGPT-subscription product).
	// Traffic hits chatgpt.com/backend-api/codex/* rather than api.openai.com,
	// so it needs separate routing even though the body format is OpenAI-flavoured.
	// See T66.
	CodexChatGPT
	// MiniMax is the summarization side-channel provider. Not an upstream that
	// the user talks to directly; it receives compressed conversation prefixes
	// for abstractive summarization. T121.
	MiniMax
)

func (p Provider) String() string {
	switch p {
	case Anthropic:
		return "anthropic"
	case OpenAI:
		return "openai"
	case CodexChatGPT:
		return "codex_chatgpt"
	case MiniMax:
		return "minimax"
	default:
		return "unknown"
	}
}

// AnchorType classifies why a message must not be summarized.
type AnchorType int

const (
	AnchorNone      AnchorType = iota
	AnchorEdit                 // contains file edit/write tool use
	AnchorError                // contains error trace or failure
	AnchorDecision             // user confirmed or rejected a plan
	AnchorArchitect            // contains architecture decisions
	AnchorConfig               // contains config or env changes
)

// ToolResultType classifies the content type of a tool_result block.
// Classification enables specialized compression in L1.9.
type ToolResultType int

const (
	ToolTypeUnknown       ToolResultType = iota
	ToolTypeGitOutput                    // git status, log, diff, show output
	ToolTypeTestOutput                   // test runner output (go test, cargo test, etc.)
	ToolTypeBuildOutput                  // compiler/build output
	ToolTypeLintOutput                   // linter output (eslint, clippy, etc.)
	ToolTypeFileRead                     // file content (cat, read tool)
	ToolTypeSearchResult                 // grep, find, glob results
	ToolTypeJSONData                     // JSON API responses, config files
	ToolTypeLogOutput                    // application/system logs
	ToolTypeDirListing                   // ls, tree output
	ToolTypeCommandOutput                // generic command output
)

// ToolResultPriority controls how MiniMax summarization treats content.
type ToolResultPriority int

const (
	PriorityLow    ToolResultPriority = iota // may reduce to one-liner
	PriorityMedium                           // preserve key facts, may paraphrase
	PriorityHigh                             // preserve verbatim (errors, edits, decisions)
)

// CompressionLevel records which compression layers have been applied.
type CompressionLevel int

const (
	CompressionNone   CompressionLevel = 0
	CompressionLayer1 CompressionLevel = 1
	CompressionLayer2 CompressionLevel = 2
)

// ContentBlock is a unified content block across providers.
type ContentBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text,omitempty"`
	ToolName     string        `json:"tool_name,omitempty"`
	ToolInput    string        `json:"tool_input,omitempty"`
	ToolUseID    string        `json:"tool_use_id,omitempty"`
	ToolResultID string        `json:"tool_result_id,omitempty"`
	ImageData    string        `json:"image_data,omitempty"`
	ImageSource  interface{}   `json:"image_source,omitempty"` // preserved raw for passthrough
	RawBlock     interface{}   `json:"raw_block,omitempty"`    // original parsed block for passthrough
	CacheControl *CacheControl `json:"cache_control,omitempty"`
	// ArchiveID is set when a lossy Layer 1 sub-layer mutated this block. It
	// references an internal/contentarchive entry that holds the original
	// bytes so the proxy can opportunistically re-inject content if the
	// model later asks about it. Empty means "not archived" (either the
	// block is intact or archiving was disabled). T76.
	ArchiveID string `json:"archive_id,omitempty"`
}

// CacheControl enables Anthropic prompt caching on a content block.
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// MessageMetadata tracks compression state and statistics for a message.
type MessageMetadata struct {
	OriginalTokens   int              `json:"original_tokens"`
	CompressedTokens int              `json:"compressed_tokens"`
	IsAnchor         bool             `json:"is_anchor"`
	AnchorType       AnchorType       `json:"anchor_type,omitempty"`
	CompressionLevel CompressionLevel `json:"compression_level"`
	WasDeduped       bool             `json:"was_deduped"`
	WasStructured    bool             `json:"was_structured"`
	OriginalHash     [32]byte         `json:"-"`
}

// Message is the internal normalized message representation used throughout the pipeline.
type Message struct {
	Index    int             `json:"index"`
	Role     string          `json:"role"` // "user", "assistant", "system", "tool"
	Content  []ContentBlock  `json:"content"`
	Metadata MessageMetadata `json:"metadata"`
}

// TextContent returns all text content blocks concatenated.
func (m *Message) TextContent() string {
	var out string
	for _, b := range m.Content {
		if b.Type == "text" && b.Text != "" {
			if out != "" {
				out += "\n"
			}
			out += b.Text
		}
	}
	return out
}

// HasToolResult reports whether any content block is a tool result.
func (m *Message) HasToolResult() bool {
	for _, b := range m.Content {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

// HasToolUse reports whether any content block is a tool use.
func (m *Message) HasToolUse() bool {
	for _, b := range m.Content {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

// CompressJob is sent to the async compression worker goroutine.
type CompressJob struct {
	Messages  []Message
	Timestamp time.Time
	SessionID string
}

// EventType classifies analytics events.
type EventType int

const (
	EventRequestProcessed EventType = iota
	EventCacheHit
	EventCompressionComplete
	EventSecretDetected
	EventErrorOccurred
	EventLayerToggled
	EventRateLimitRetry // 429/529 from upstream; proxy will sleep and retry
	EventOverflowRetry  // context-length error from upstream; proxy retries with aggressive compression
)

// AnalyticsEvent is sent from the proxy hot path to the analytics collector goroutine.
type AnalyticsEvent struct {
	Type             EventType
	Timestamp        time.Time
	Provider         Provider
	Model            string
	InputTokensOrig  int
	InputTokensComp  int
	OutputTokens     int
	CompressionRatio float64
	Layers           []int
	LatencyMs        float64
	CacheHit         bool
	SecretsFound     int
	TokensSaved      int
	Error            string
	// CacheReadTokens is the number of upstream-cached (prompt-cache-hit)
	// input tokens reported by the provider for this request. Anthropic
	// surfaces this via usage.cache_read_input_tokens; OpenAI does not
	// yet expose an equivalent. Zero when absent.
	CacheReadTokens int
	// CacheCreateTokens is the number of tokens newly cached by this
	// request (Anthropic usage.cache_creation_input_tokens).
	CacheCreateTokens int
	// OutputReduceApplied reports whether T130 injected output-discipline
	// instructions into this request.
	OutputReduceApplied bool
	// OutputReduceProfile is the effective T130 profile used for the request.
	OutputReduceProfile string
	// OutputReduceReason is the apply/skip reason recorded by T130.
	OutputReduceReason string
	// OutputReduceAddedTokens is the estimated input-token overhead introduced
	// by the injected T130 directive.
	OutputReduceAddedTokens int
}

// RequestMetrics records per-request statistics kept in the ring buffer.
type RequestMetrics struct {
	Timestamp         time.Time
	Provider          Provider
	Model             string
	InputTokensOrig   int
	InputTokensComp   int
	OutputTokens      int
	CompressionRatio  float64
	Layers            []int
	LatencyMs         float64
	CacheHit          bool
	CacheReadTokens   int
	CacheCreateTokens int
}

// RingBuffer is a generic fixed-capacity circular buffer, safe for concurrent use.
type RingBuffer[T any] struct {
	mu    sync.RWMutex
	items []T
	head  int
	size  int
	cap   int
}

// NewRingBuffer returns a new RingBuffer with the given capacity.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	return &RingBuffer[T]{
		items: make([]T, capacity),
		cap:   capacity,
	}
}

// Push appends an item, overwriting the oldest entry when full.
func (rb *RingBuffer[T]) Push(item T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.items[rb.head] = item
	rb.head = (rb.head + 1) % rb.cap
	if rb.size < rb.cap {
		rb.size++
	}
}

// Last returns up to n most-recent items in insertion order.
func (rb *RingBuffer[T]) Last(n int) []T {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if n > rb.size {
		n = rb.size
	}
	result := make([]T, n)
	for i := range n {
		idx := (rb.head - n + i + rb.cap) % rb.cap
		result[i] = rb.items[idx]
	}
	return result
}

// Len returns the number of items currently stored.
func (rb *RingBuffer[T]) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size
}

// ProviderHealthStatus describes the health of an upstream API provider,
// derived from actual request outcomes (no polling - see spec §16.4).
type ProviderHealthStatus int

const (
	ProviderHealthIdle     ProviderHealthStatus = iota // no requests in 5+ minutes
	ProviderHealthHealthy                              // recent requests succeeding
	ProviderHealthDegraded                             // >20% error rate in recent requests
	ProviderHealthDown                                 // last 3 consecutive requests failed
)

// ProviderHealthInfo is a point-in-time health snapshot used by the TUI.
type ProviderHealthInfo struct {
	Status      ProviderHealthStatus
	LastSuccess time.Time
	LastError   time.Time
	ErrorRate   float64 // fraction of recent results that were errors (0.0-1.0)
}

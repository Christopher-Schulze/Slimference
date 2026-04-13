# TokenProxy - Technical Specification

Version: 1.0.0-final
Date: 2026-04-09
Language: Go 1.24+
Architecture: Layered Compression Proxy with Async Pre-Processing Pipeline

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Problem Statement & Usage Economics](#2-problem-statement--economics)
3. [Architecture Overview](#3-architecture-overview)
4. [Layer 1: Deterministic Compression Engine](#4-layer-1-deterministic-compression-engine)
5. [Layer 2: Intelligent Compression via MiniMax M2.7](#5-layer-2-intelligent-compression-via-minimax-m27)
6. [Layer 3: Caching & Optimization](#6-layer-3-caching--optimization)
7. [Layer 4: Go Concurrency Pipeline](#7-layer-4-go-concurrency-pipeline)
8. [Multi-Provider Support & OAuth Authentication](#8-multi-provider-support)
   - 8.3 OAuth Passthrough (no API keys required)
   - 8.4 Provider Compatibility Matrix
   - 8.5 Setup per CLI
   - 8.6 Pre-Flight Validation Test
9. [Secret Detection & Redaction](#9-secret-detection--redaction)
10. [Analytics & Observability (BubbleTea TUI)](#10-analytics--observability)
11. [Configuration System](#11-configuration-system)
12. [Usage & Quick Setup](#12-usage--quick-setup)
13. [Data Structures & Types](#13-data-structures--types)
14. [API Compatibility Layer & Provider Invisibility](#14-api-compatibility-layer)
   - 14.4 Provider Invisibility Contract
15. [Error Handling & Resilience](#15-error-handling--resilience)
   - 15.3 Auto-Retry: Rate Limit (429)
   - 15.4 Auto-Retry: Context Overflow
   - 15.5 API Health Monitoring
   - 15.6 Request Logging
   - 15.7 Latency Tracking
16. [Testing Strategy](#16-testing-strategy)
17. [Performance Targets](#17-performance-targets)
18. [Dependency Inventory](#18-dependency-inventory)
19. [Project Structure](#19-project-structure)
20. [Build & Distribution](#20-build--distribution)
21. [Rollout Plan](#21-rollout-plan)

---

## 1. Executive Summary

TokenProxy is a transparent HTTP reverse proxy written in Go that sits between LLM CLI tools
(Claude Code, OpenAI Codex) and their respective APIs. It intercepts outgoing requests,
applies multi-layered token compression to the conversation history, and forwards the
optimized request to the upstream API. Responses are streamed through unmodified.

### Core Value Proposition

- **60-80% token savings** on long coding sessions (measured as input tokens billed)
- **Zero perceived latency** through async pre-compression during user idle time
- **Quality improvement** through noise reduction (less "Lost in the Middle" degradation)
- **30-40% faster Time-to-First-Token** as a side effect of shorter prompts
- **Security layer** with automatic secret detection and redaction
- **Full observability** with real-time token tracking and cost analytics
- **Provider-agnostic** - works with Anthropic and OpenAI API formats

### Design Principles

1. **Zero-downside guarantee**: If compression would degrade quality, skip it. Uncompressed
   passthrough is always the fallback. The proxy must NEVER make things worse.
2. **Additive-only transformation**: The proxy only REMOVES or REPLACES tokens. It never
   ADDS content to the conversation that the user/model did not produce.
3. **Transparency**: Every compression decision is logged. The user can inspect exactly
   what was changed and why.
4. **Graceful degradation**: If MiniMax is down, Layer 2 is skipped. If tree-sitter fails
   on a file, that file passes through uncompressed. Every layer is independently optional.
5. **Complete provider invisibility**: The proxy MUST be architecturally undetectable by
   upstream providers. See Section 14.4 for the full invisibility contract.

---

## 2. Problem Statement & Economics

### The Token Accumulation Problem

LLM APIs are stateless. Every request must include the full conversation history.
For a coding session with N message exchanges averaging S tokens each:

- Input tokens for message N: approximately N * S
- Total tokens over N messages: S * N * (N+1) / 2
- This is O(N^2) cumulative cost growth

### Real-World Usage Analysis (Subscription Plans)

For subscription users (Claude Code Max, Codex Pro), the benefit is NOT cost savings
but **usage multiplier** - more messages before hitting rate limits.

Assumptions: average exchange = 7,700 tokens, 70% compression

| Message # | Input Tokens (raw) | Input Tokens (proxy) | Usage saved |
|---|---|---|---|
| 1 | 7,700 | 7,700 | 0% (too short to compress) |
| 10 | 77,000 | 38,500 | 50% (Layer 1 only) |
| 20 | 154,000 | 50,800 | 67% (Layer 1 + 2) |
| 30 | 231,000 | 76,200 | 67% (Layer 1 + 2) |
| 50 | 385,000 | 127,000 | 67% (Layer 1 + 2) |

**Impact on subscription rate limits (OAuth/subscription plans):**

The proxy saves INPUT tokens only. Input dominates total usage at 95-99% in coding
sessions (message 10+). Output tokens (model responses) pass through unchanged.
Since rate limits count total tokens (input + output), the effective savings on
total usage closely match the input savings.

| Session length | Total tokens saved | Effective usage multiplier |
|---|---|---|
| Short (5-10 messages) | 30-50% | ~1.5x capacity |
| Medium (15-25 messages) | 55-65% | ~2-2.5x capacity |
| Long (30+ messages) | 65-67% | ~3x capacity |

Concrete impact:

| Metric | Without Proxy | With Proxy | Improvement |
|---|---|---|---|
| Messages before rate limit | ~30 | ~70-90 | **2-3x more work** |
| /compact frequency | every ~25 msgs | every ~75 msgs | **3x longer sessions** |
| TTFT at message 30 | ~4s | ~1.5s | **2.5x faster** |
| Session continuity | dies at context limit | auto-retry with compression | **no more session deaths** |

### What Exactly Gets Compressed (and What Does Not)

```
NEVER compressed (always sent in full):
  - Your current prompt/message
  - System prompt / CLAUDE.md
  - Recent messages (sliding window, last 5 exchanges)
  - Anchor messages (edits, errors, decisions)

COMPRESSED (old history only):
  - Old assistant responses -> summarized by MiniMax
  - Old tool results (file reads, greps) -> JSON minified, deduplicated, signatures
  - Old user messages -> included in MiniMax summary
  - Repeated file contents -> replaced with reference to first occurrence
```

The proxy changes ONLY the old history portion of the request.
Your current work context is always preserved at full fidelity.

---

## 3. Architecture Overview

```
                    CLI Tool (Claude Code / Codex)
                              |
                              | HTTP Request (Messages API)
                              |
                              v
                    +-------------------+
                    |   TokenProxy      |
                    |   (localhost)     |
                    +-------------------+
                              |
            +-----------------+-----------------+
            |                 |                 |
            v                 v                 v
    +---------------+ +---------------+ +---------------+
    | Layer 1       | | Layer 2       | | Layer 3       |
    | Deterministic | | Intelligent   | | Caching &     |
    | Compression   | | Compression   | | Optimization  |
    | (pure Go)     | | (MiniMax API) | | (fsnotify)    |
    +---------------+ +---------------+ +---------------+
            |                 |                 |
            +-----------------+-----------------+
                              |
                              v
                    +-------------------+
                    | Layer 4           |
                    | Go Concurrency    |
                    | Pipeline          |
                    +-------------------+
                              |
                              | Optimized HTTP Request
                              v
                    Upstream API (Anthropic / OpenAI)
                              |
                              | SSE Stream (unmodified)
                              v
                    CLI Tool (response displayed)
```

### Request Flow (detailed)

```
1. CLI sends HTTP POST to localhost:PORT
2. Proxy parses request body (Anthropic or OpenAI format detected)
3. Proxy extracts message array from request
4. Layer 1 runs synchronously (<1ms):
   a. JSON minification of tool results
   b. Whitespace/comment stripping from code blocks
   c. Hash-based deduplication of repeated content
   d. Tree-sitter signature extraction for old code blocks
   e. Delta encoding for repeated file reads
5. Check Layer 2 cache:
   a. If pre-compressed summary exists for old messages -> use it
   b. If not -> skip Layer 2 for THIS request, trigger async compression
6. Layer 3 checks:
   a. Prompt cache prefix optimization (Anthropic only)
   b. Response cache lookup (if identical request seen recently)
7. Reconstruct request with compressed messages
8. Forward to upstream API
9. Stream response back to CLI (SSE passthrough)
10. After response complete:
    a. Store response in cache
    b. Trigger async Layer 2 compression for NEXT request
    c. Update analytics counters
```

---

## 4. Layer 1: Deterministic Compression Engine

Layer 1 is pure Go, zero external dependencies, zero latency, zero risk.
It runs synchronously on every request in under 1ms.

### 4.1 JSON Minification

**Target:** Tool result content that contains JSON (package.json, tsconfig, API responses, etc.)

**Implementation:**

```go
// Compact JSON tool results in-place
// Preserves semantic meaning, removes formatting whitespace
//
// Input:  {"name": "my-app",\n  "version": "1.0.0",\n  "dependencies": {\n    ...
// Output: {"name":"my-app","version":"1.0.0","dependencies":{...
//
// Uses encoding/json.Compact from stdlib - battle-tested, zero allocation overhead
```

**Rules:**
- Only apply to content that IS valid JSON (validate first via json.Valid)
- Never apply to user messages or assistant messages - only tool_result content
- Preserve JSON structure completely - this is lossless compression

**Expected savings:** 10-25% on JSON-heavy tool results
**Risk:** Zero (lossless, stdlib implementation)

### 4.2 Code Comment & Whitespace Stripping

**Target:** Code content in tool results (file reads, grep results)

**Implementation:**

```go
// Language-aware comment and excess whitespace removal
// Strips: single-line comments (// # --), multi-line comments (/* */ {- -})
// Normalizes: multiple blank lines -> single blank line
// Preserves: string literals containing comment-like patterns
//
// Detection: language inferred from file extension in tool call metadata
// Fallback: if language unknown, only normalize whitespace (safe default)
```

**Language support (initial):**
- Go: `//`, `/* */`
- TypeScript/JavaScript: `//`, `/* */`
- Rust: `//`, `/* */`, `///` (doc comments treated as comments in old messages)
- Python: `#`, `""" """`
- CSS: `/* */`
- HTML: `<!-- -->`
- YAML: `#`
- TOML: `#`

**Rules:**
- Only strip comments in messages OLDER than the configurable sliding window (default: last 5 messages)
- Never strip comments in the most recent messages (the model might need them)
- String literal detection: simple state machine tracking quote open/close
- If in doubt (complex string interpolation, heredocs): skip that line entirely

**Expected savings:** 5-15% on code-heavy conversations
**Risk:** Minimal. Conservative approach (skip when uncertain) prevents data loss.

### 4.3 Hash-Based Content Deduplication

**Target:** Identical or near-identical content appearing multiple times in the conversation
(e.g., same file read twice, same grep output repeated)

**Implementation:**

```go
// Content fingerprinting and deduplication
//
// For each tool_result content block:
// 1. Compute SHA256 hash of normalized content (whitespace-normalized)
// 2. Check against seen-content map
// 3. If exact match found in an earlier message:
//    Replace content with: "[Content identical to tool result in message {N}]"
// 4. Store hash -> message index mapping
//
// Near-duplicate detection (for files that changed slightly):
// 1. Compute MinHash signature (k=128 permutations, shingle size=3)
// 2. Estimate Jaccard similarity via LSH
// 3. If similarity > 0.85:
//    Compute actual diff (Myers diff algorithm)
//    Replace content with: "[File changed from message {N}]:\n{unified diff}"
//    Only if diff is shorter than original content
```

**Data structures:**

```go
type ContentIndex struct {
    mu       sync.RWMutex
    exact    map[[32]byte]int        // SHA256 -> message index (exact match)
    minhash  map[int]*MinHashSig     // message index -> MinHash signature
    lsh      *LSHIndex               // locality-sensitive hashing for near-dupes
}
```

**Rules:**
- Only deduplicate tool_result content, never user or assistant messages
- Only reference EARLIER messages (the model has already seen them)
- If the referenced message was itself compressed (e.g., by Layer 2 summary),
  do NOT deduplicate - the reference would point to missing content
- Near-duplicate: only use diff replacement if diff is <50% of original content length

**Expected savings:** 10-20% in typical coding sessions (files are read repeatedly)
**Risk:** Low. The model understands references like "[identical to message N]"
naturally. Fallback: if compression ratio <10%, skip (not worth the reference overhead).

### 4.4 Tree-Sitter Code Structure Extraction

**Target:** Large code files in OLD tool results. Replace full file content with
structural summary (signatures, types, imports only).

**Implementation:**

```go
// Tree-sitter based code structure extraction
//
// For tool_result content containing code files in messages OLDER than window:
// 1. Parse with go-tree-sitter (language detected from file extension)
// 2. Extract: function signatures, type/interface/struct definitions,
//    import statements, const/var declarations, class definitions
// 3. Drop: function bodies, method implementations, comment blocks
// 4. Reconstruct as condensed structural summary
//
// Example transformation:
//
// BEFORE (450 tokens):
// func (s *Server) handleRequest(ctx context.Context, req *Request) (*Response, error) {
//     if req == nil {
//         return nil, fmt.Errorf("nil request")
//     }
//     validated, err := s.validator.Validate(ctx, req)
//     if err != nil {
//         return nil, fmt.Errorf("validation: %w", err)
//     }
//     result, err := s.processor.Process(ctx, validated)
//     if err != nil {
//         return nil, fmt.Errorf("processing: %w", err)
//     }
//     return &Response{Data: result}, nil
// }
//
// AFTER (35 tokens):
// func (s *Server) handleRequest(ctx context.Context, req *Request) (*Response, error)
```

**Supported languages (via go-tree-sitter queries):**

| Language | Extracted Nodes | Query Complexity |
|---|---|---|
| Go | func signatures, type/struct/interface decls, imports, const/var | Medium |
| TypeScript | function/arrow sigs, interface/type/class decls, imports, const/let | Medium |
| JavaScript | function sigs, class decls, imports, const/let/var | Medium |
| Rust | fn signatures, struct/enum/trait/impl decls, use statements, const | Medium |
| Python | def signatures, class decls, import, global assignments | Low |

**Tree-sitter query example (Go):**

```scheme
; Function declarations - capture only signature
(function_declaration
  name: (identifier) @func.name
  parameters: (parameter_list) @func.params
  result: (_)? @func.result) @func.decl

; Type declarations
(type_declaration
  (type_spec
    name: (type_identifier) @type.name
    type: (_) @type.def)) @type.decl

; Import declarations
(import_declaration) @import
```

**Rules:**
- ONLY apply to messages outside the sliding window (not recent messages)
- ONLY apply to tool_result content that contains recognizable code files
- If tree-sitter parse fails (syntax error, unknown language): pass through uncompressed
- Preserve the FULL content of files that were EDITED (not just read) - the model needs
  to see the full file to understand the edit context
- Minimum file size threshold: 500 tokens. Smaller files are not worth compressing.
- Add header to compressed output: "[Structural summary of {filename} - full content was
  in message {N}]"

**Expected savings:** 40-60% on large code files in old messages
**Risk:** Medium. The model loses function body details from old messages. This is
acceptable because: (a) if the model needs the full file, it will re-read it via tool
call, (b) structural summaries preserve the API surface which is what matters for
understanding dependencies.

### 4.5 Delta Encoding for File Revisions

**Target:** When the same file appears multiple times across the conversation
(read -> edit -> read again -> edit again), only send the diff for subsequent appearances.

**Implementation:**

```go
// File revision tracking and delta encoding
//
// Maintain a map of filepath -> []ContentVersion
// Each version stores: message index, content hash, full content
//
// When a new tool_result contains a file that was seen before:
// 1. Look up previous version
// 2. Compute unified diff (Myers algorithm)
// 3. If diff is shorter than full content:
//    Replace with: "[Delta from {filepath} at message {N}]:\n{unified diff}"
// 4. If diff is longer (major rewrite): keep full content
//
// Special handling for Edit tool results:
// - Edit results already contain old_string/new_string diffs
// - These are already minimal - do not compress further
// - But DO update the file version tracker with the implied new content

type FileVersionTracker struct {
    mu       sync.RWMutex
    versions map[string][]FileVersion  // filepath -> chronological versions
}

type FileVersion struct {
    MessageIdx int
    Hash       [32]byte
    Content    string
    Timestamp  time.Time
}
```

**Expected savings:** 5-15% (depends heavily on how often files are re-read)
**Risk:** Low. Unified diffs are well-understood by LLMs.

### 4.6 Prompt Cache Prefix Optimization (Anthropic-specific)

**Target:** Maximize Anthropic's server-side prompt caching by ensuring the message
prefix is as stable and long as possible across requests.

**Background:** Anthropic caches the KV-cache for message prefixes that are byte-identical
across requests. Cached tokens cost 10% of normal input price. The cache has a 5-minute
TTL that refreshes on hit. Minimum cacheable block: 1024 tokens.

**Implementation:**

```go
// Prompt cache prefix optimization for Anthropic Messages API
//
// Strategy: Ensure the first N messages are byte-identical across requests.
// This means: NEVER modify recent messages (they change every request).
// ALWAYS place compressed/static content at the beginning.
//
// Message ordering for maximum cache hits:
//
// Position 1: System prompt (never changes within session)
//             -> Add cache_control breakpoint here
// Position 2: [Compressed history summary from Layer 2]
//             -> This changes every ~5 messages, but is stable between changes
//             -> Add cache_control breakpoint here
// Position 3: [Layer 1 compressed old messages]
//             -> Relatively stable
// Position 4: [Recent messages in full - sliding window]
//             -> Changes every request (no caching possible)
//
// The proxy injects cache_control: {"type": "ephemeral"} at strategic points.
// Specifically: on the LAST content block before the volatile section begins.

type CacheBreakpointStrategy struct {
    // Index of the last message that is considered "stable" (compressed/summarized)
    StableMessageBoundary int
    // Whether the system prompt has a breakpoint
    SystemPromptBreakpoint bool
    // Whether the compressed history has a breakpoint
    HistorySummaryBreakpoint bool
}
```

**Rules:**
- Only apply to Anthropic API requests (detected via request format)
- Never add more than 4 cache_control breakpoints (Anthropic limit)
- Minimum 1024 tokens per cached block (Anthropic minimum)
- If the compressed history summary changes (new compression), the cache
  for that block is invalidated - this is expected and acceptable

**Expected savings:** 50-90% cost reduction on the cached prefix portion
(which is the largest part of the request after Layer 2 compression)
**Risk:** Zero. cache_control is a standard Anthropic API feature.

---

## 5. Layer 2: Intelligent Compression via MiniMax M2.7

Layer 2 uses MiniMax M2.7 to abstractively summarize old conversation history.
This is the highest-impact layer but requires external API calls.

### 5.1 Compression Strategy: Sliding Window with Anchor Points

```
Message History Zones:

|<-- Zone A: Summary -->|<-- Zone B: Compressed -->|<-- Zone C: Full -->|
|   (abstractive        |   (Layer 1 only,         |   (unmodified,     |
|    summary by         |    signatures/dedup)      |    recent messages)|
|    MiniMax)           |                           |                    |
|                       |                           |                    |
| Messages 1..K        | Messages K+1..N-W         | Messages N-W+1..N  |
|                       |                           |                    |
| Anchor points         |                           |                    |
| preserved inline     |                           |                    |

K = summary boundary (moves forward as conversation grows)
W = sliding window size (configurable, default 5 exchanges)
N = total messages
```

### 5.2 Anchor Point Detection (Algorithmic, No LLM)

Certain messages must NEVER be summarized because they contain critical context:

```go
type AnchorType int

const (
    AnchorEdit       AnchorType = iota  // Message contains file edit/write tool use
    AnchorError                          // Message contains error trace or failure
    AnchorDecision                       // User confirmed/rejected a plan
    AnchorArchitect                      // Message contains architecture decisions
    AnchorConfig                         // Message contains config/env changes
)

// Detection rules (all pure string/pattern matching, no LLM):
//
// AnchorEdit:
//   - tool_use with name containing "edit", "write", "create", "delete"
//   - tool_result following such a tool_use
//
// AnchorError:
//   - Content containing: "error", "Error", "ERROR", "panic", "FAIL",
//     "traceback", "exception", "fatal"
//   - Content containing stack traces (detected by: repeated "at " or "goroutine"
//     or "File \"" patterns)
//
// AnchorDecision:
//   - User messages containing: "yes", "ja", "do it", "go ahead", "approved",
//     "no", "nein", "don't", "stop", "nicht", "cancel"
//   - User messages shorter than 50 tokens following an assistant question
//
// AnchorArchitect:
//   - Assistant messages containing: "architecture", "design", "approach",
//     "strategy", "plan", "trade-off"
//   - Messages with bullet lists (>3 items) following a user question
//
// AnchorConfig:
//   - Tool results containing file paths matching: *.json, *.toml, *.yaml,
//     *.env, Makefile, Dockerfile, *.conf

type AnchorDetector struct {
    patterns map[AnchorType][]AnchorPattern
}

type AnchorPattern struct {
    ContentRegex    *regexp.Regexp  // nil = skip this check
    ToolNameRegex   *regexp.Regexp  // nil = skip this check
    RoleFilter      string          // "user", "assistant", "tool", "" = any
    MaxTokenLength  int             // 0 = no limit
}
```

**Anchor handling in summary:**
- Anchored messages are EXCLUDED from the summarization input
- They are preserved verbatim in Zone A, interleaved with the summary
- The summary text explicitly references anchor points:
  "Between messages 5-12: [summary]. Note: file auth/middleware.ts was edited in
  message 8 (preserved below)."

### 5.3 MiniMax Summarization Prompt

```go
const summarizationSystemPrompt = `You are a conversation compressor for an AI coding assistant session.
Your job: condense the provided conversation history into a minimal but complete summary.

RULES:
1. Preserve ALL: file paths, function names, variable names, error messages, decisions made
2. Preserve ALL: architectural choices, rejected alternatives, configuration values
3. Preserve the SEQUENCE of events (what was tried first, what failed, what worked)
4. Use compact notation: "User asked to refactor auth -> Assistant edited auth/middleware.ts
   (extracted validateToken fn) -> Build succeeded"
5. For code changes: state WHAT changed and WHERE, not the full code
6. For errors: state the error message and resolution, not the full trace
7. For tool results: state the key finding, not the raw output
8. Output language: match the dominant language of the conversation
9. Target length: 15-20% of original token count
10. If critical detail would be lost by compression: keep it verbatim

FORMAT:
Output a single block of text. No headers, no markdown formatting.
Use " -> " for sequential events. Use "; " for parallel facts.
Reference message numbers where useful for context.`

const summarizationUserPrompt = `Compress this conversation segment (messages %d to %d).
Preserve all technical details, file paths, decisions, and outcomes.
Target: %d tokens maximum.

CONVERSATION:
%s`
```

### 5.4 MiniMax API Integration

```go
type MiniMaxClient struct {
    baseURL    string              // https://api.minimax.io/v1
    apiKey     string              // from config
    model      string              // minimax-m2.7
    httpClient *http.Client        // with timeouts
    limiter    *rate.Limiter       // self-imposed rate limit (conservative)
}

// API call for summarization
// Uses OpenAI-compatible chat completions endpoint
//
// POST https://api.minimax.io/v1/chat/completions
// {
//   "model": "minimax-m2.7",
//   "messages": [
//     {"role": "system", "content": summarizationSystemPrompt},
//     {"role": "user", "content": summarizationUserPrompt}
//   ],
//   "max_tokens": targetTokens,
//   "temperature": 0.1,  // low temperature for consistent, factual summaries
//   "stream": false       // no streaming needed for summarization
// }
//
// Response parsed and stored in compression cache.
```

**Timeout configuration:**
- Connect timeout: 5 seconds
- Response timeout: 30 seconds (M2.7 TTFT ~2s + generation)
- Total timeout: 45 seconds (absolute maximum)

**Retry policy:**
- Max 2 retries with exponential backoff (1s, 3s)
- On persistent failure: skip Layer 2 entirely for this cycle, log warning
- Never block the main request pipeline waiting for retries

### 5.5 Summary Caching & Invalidation

```go
type SummaryCache struct {
    mu          sync.RWMutex
    current     *CachedSummary
    previous    *CachedSummary  // keep one previous version for rollback
    compressing atomic.Bool     // true while async compression is in progress
}

type CachedSummary struct {
    Summary         string      // the compressed text
    CoveredRange    [2]int      // [startMsg, endMsg] indices covered by this summary
    AnchorsInlined  []int       // message indices preserved verbatim within summary
    OriginalTokens  int         // tokens before compression
    CompressedTokens int        // tokens after compression
    CompressionRatio float64    // CompressedTokens / OriginalTokens
    CreatedAt       time.Time
    Hash            [32]byte    // hash of input messages (for invalidation)
}

// Invalidation triggers:
// 1. New messages added beyond the covered range -> needs extension
// 2. Sliding window moved -> old window messages need summarizing
// 3. Manual invalidation via CLI command
// 4. Cache age > 30 minutes (safety net)
```

### 5.6 Progressive Compression Tiers

For very long sessions (50+ messages), apply multi-tier compression:

```
Tier 1 (messages 1-20):    Summarized to ~10% of original
Tier 2 (messages 21-35):   Summarized to ~20% of original
Tier 3 (messages 36-N-W):  Layer 1 compression only (~60% of original)
Full   (messages N-W..N):  Unmodified (sliding window)

As the conversation grows, Tier 1 gets re-summarized more aggressively.
Tier 2 becomes Tier 1 when enough new messages arrive.
```

This ensures that very old context is ultra-compact while recent work
retains full detail.

### 5.7 Quality Safeguard: Compression Validation

After MiniMax generates a summary, validate it before using:

```go
type CompressionValidator struct {
    // Minimum preservation checks (all must pass):
    checks []ValidationCheck
}

type ValidationCheck struct {
    Name        string
    Description string
    Check       func(original []Message, summary string) (bool, string)
}

// Validation checks:
//
// 1. FilePathPreservation:
//    Extract all file paths from original messages (regex: common path patterns)
//    Verify that >90% appear in the summary
//    Fail reason: "Summary lost file paths: {list}"
//
// 2. FunctionNamePreservation:
//    Extract function/method names from code blocks in original
//    Verify >80% appear in summary
//    Fail reason: "Summary lost function references: {list}"
//
// 3. ErrorPreservation:
//    If original contains error messages, verify key error strings in summary
//    Fail reason: "Summary lost error context: {list}"
//
// 4. MinimumLength:
//    Summary must be >5% of original token count
//    If shorter: suspiciously lossy, reject
//    Fail reason: "Summary too short ({ratio}% of original)"
//
// 5. MaximumLength:
//    Summary must be <40% of original token count
//    If longer: compression not effective enough, fall back to Layer 1 only
//    Fail reason: "Summary not compressed enough ({ratio}% of original)"
//
// On validation failure:
// - Log the failure with reason
// - Discard the summary
// - Fall back to Layer 1 compression only for this range
// - Retry summarization on next cycle with adjusted prompt
```

---

## 6. Layer 3: Caching & Optimization

### 6.1 Response Cache

Cache complete API responses for identical requests. Useful when:
- User runs a command, sees the output, runs the exact same command again
- CLI tools retry on timeout with identical payload

```go
type ResponseCache struct {
    mu      sync.RWMutex
    entries map[[32]byte]*CacheEntry  // hash of request body -> response
    maxSize int                        // max entries (default: 100)
    ttl     time.Duration              // default: 5 minutes
}

type CacheEntry struct {
    Response    []byte      // full HTTP response body
    Headers     http.Header // response headers (content-type, etc.)
    StatusCode  int
    CreatedAt   time.Time
    HitCount    int
    TokensSaved int         // input tokens saved by this cache hit
}

// Cache key computation:
// SHA256 of: sorted message array content + model name + temperature
// Excludes: request ID, timestamps, stream flag (stream vs non-stream same content)
//
// Cache invalidation:
// - TTL expiry (default 5 minutes)
// - File change detection (any file in the conversation changed on disk)
// - Manual flush via CLI
// - LRU eviction when maxSize reached
```

### 6.2 File Change Detection (fsnotify)

```go
type FileWatcher struct {
    watcher     *fsnotify.Watcher
    trackedFiles map[string]time.Time  // filepath -> last known modtime
    onChange     func(path string)      // callback to invalidate caches
    mu          sync.RWMutex
}

// Watches all files that appear in tool_result content (file reads/edits).
// On change:
// 1. Invalidate response cache entries that reference this file
// 2. Invalidate deduplication index entries for this file
// 3. Mark file version tracker entry as stale
// 4. Log the change for analytics
//
// Watch strategy:
// - Watch directories, not individual files (more efficient)
// - Debounce: 100ms after last change event before triggering invalidation
//   (editors often write multiple times during save)
// - Max watched directories: 50 (prevent resource exhaustion)
// - Auto-unwatch directories not referenced in last 30 minutes
```

### 6.3 Usage Tracker

Tracks token usage and estimates effective capacity gains for subscription users.

```go
type UsageTracker struct {
    mu sync.RWMutex

    // Session counters
    SessionStart       time.Time
    MessagesSent       int           // total API requests this session
    InputTokensOrig    int           // total input tokens before compression
    InputTokensComp    int           // total input tokens after compression
    InputTokensSaved   int           // difference (what we saved)
    OutputTokens       int           // total output tokens (passthrough, tracked for stats)

    // Per-provider breakdown
    PerProvider        map[Provider]*ProviderUsage

    // Running averages
    AvgTokensPerReq    int           // running average of compressed input tokens per request
    AvgCompressionPct  float64       // running average compression percentage

    // Derived metrics (recalculated on each update)
    EstExtraMessages   int           // InputTokensSaved / AvgTokensPerReq
    AvgTTFTImprovement float64       // seconds saved per request (based on prefill speed estimate)

    // Config
    PrefillSpeed       int           // estimated tokens/second for upstream model (from config)
}

type ProviderUsage struct {
    Messages     int
    TokensSaved  int
    AvgRatio     float64
}

// Token counting:
// - Use tiktoken-go with cl100k_base encoding for approximate counts
// - Claude uses a different tokenizer but cl100k_base is within ~5% accuracy
// - Count BOTH input and output tokens (output counted from streamed SSE response)

// EstExtraMessages calculation:
// Every token we save is a token that DOESN'T count against the rate limit.
// If we saved 835K tokens and avg request uses 8.7K tokens (after compression),
// that's ~96 extra requests we could have sent.
// This is the most tangible metric for subscription users.

// AvgTTFTImprovement calculation:
// Prefill time is roughly proportional to input token count.
// If PrefillSpeed = 50,000 tok/s and we reduced input by 58K tokens:
// TTFT saved = 58,000 / 50,000 = 1.16 seconds per request.
```

---

## 7. Layer 4: Go Concurrency Pipeline

This is the core engine that makes everything work without perceived latency.

### 7.1 Goroutine Architecture

```go
type Proxy struct {
    // Core
    server          *http.Server
    upstreamClients map[Provider]*UpstreamClient

    // Layer 1
    compressor      *DeterministicCompressor
    contentIndex    *ContentIndex
    fileTracker     *FileVersionTracker
    treeSitter      *TreeSitterEngine

    // Layer 2
    minimax         *MiniMaxClient
    summaryCache    *SummaryCache
    anchorDetector  *AnchorDetector
    validator       *CompressionValidator

    // Layer 3
    responseCache   *ResponseCache
    fileWatcher     *FileWatcher
    usageTracker    *UsageTracker

    // Layer 4
    compressQueue   chan CompressJob      // buffered channel for async compression
    analyticsQueue  chan AnalyticsEvent   // buffered channel for analytics events
    shutdownCh      chan struct{}          // signal graceful shutdown
    wg              sync.WaitGroup        // track active goroutines

    // Runtime toggles (controlled by TUI keypresses)
    providerEnabled [2]atomic.Bool        // [Anthropic, OpenAI] - true = compress, false = passthrough
    layerEnabled    [3]atomic.Bool        // [Layer1, Layer2, Layer3] - toggled via TUI

    // TUI communication
    tuiProgram      *tea.Program          // reference to send proxyEventMsg into TUI

    // Config
    config          *Config
}

// Toggle methods (called from TUI Update function, must be goroutine-safe):

func (p *Proxy) SetProviderEnabled(prov Provider, enabled bool) {
    p.providerEnabled[prov].Store(enabled)
}

func (p *Proxy) SetLayerEnabled(layer int, enabled bool) {
    p.layerEnabled[layer-1].Store(enabled) // layer is 1-indexed
}

func (p *Proxy) IsProviderEnabled(prov Provider) bool {
    return p.providerEnabled[prov].Load()
}

func (p *Proxy) IsLayerEnabled(layer int) bool {
    return p.layerEnabled[layer-1].Load()
}

// Goroutine lifecycle (started in Proxy.Start()):
//
// G1: HTTP Server (main)
//     - Accepts connections, dispatches handlers
//     - One goroutine per active request (Go's default HTTP behavior)
//
// G2: Compression Worker
//     - Reads from compressQueue channel
//     - Calls MiniMax API to generate summaries
//     - Validates and stores results in summaryCache
//     - Single worker (serialized compression to avoid redundant work)
//
// G3: File Watcher
//     - Listens on fsnotify events
//     - Debounces and triggers cache invalidation
//
// G4: Analytics Collector
//     - Reads from analyticsQueue channel
//     - Aggregates metrics
//     - Sends proxyEventMsg to BubbleTea TUI via tea.Program.Send()
//     - Writes periodic flush to persistent JSONL log
//
// G5: Budget Reset Timer
//     - Resets hourly/daily token counters on schedule
//
// G6: Cache Janitor
//     - Periodic cleanup of expired cache entries (every 60 seconds)
//     - Removes stale file watcher subscriptions
//
// G7: BubbleTea TUI (runs on MAIN goroutine)
//     - tea.Program.Run() blocks main goroutine
//     - Receives proxyEventMsg from G4 via tea.Program.Send()
//     - Receives tickMsg every 500ms for periodic refresh
//     - Handles keyboard input (toggle providers, layers, views)
//     - On quit: triggers proxy.Shutdown() then returns
```

### 7.2 Request Handler (Hot Path)

```go
func (p *Proxy) handleRequest(w http.ResponseWriter, r *http.Request) {
    // 1. Parse request
    provider, body, err := p.parseRequest(r)

    // 2. Extract messages
    messages := p.extractMessages(provider, body)

    // 3. Response cache check
    cacheKey := p.responseCache.ComputeKey(messages, body)
    if cached, ok := p.responseCache.Get(cacheKey); ok {
        p.streamCachedResponse(w, cached)
        p.analyticsQueue <- AnalyticsEvent{Type: CacheHit, TokensSaved: cached.TokensSaved}
        return
    }

    // 5. Layer 1: Deterministic compression (synchronous, <1ms)
    compressed := p.compressor.Compress(messages, p.contentIndex, p.fileTracker)

    // 6. Layer 2: Check for pre-computed summary
    if summary, ok := p.summaryCache.Get(compressed); ok {
        compressed = p.applySummary(compressed, summary)
    }

    // 7. Layer 3: Prompt cache optimization (Anthropic only)
    if provider == Anthropic {
        compressed = p.optimizeCacheBreakpoints(compressed)
    }

    // 8. Reconstruct request body with compressed messages
    newBody := p.reconstructBody(provider, body, compressed)

    // 9. Token accounting
    originalTokens := p.countTokens(messages)
    compressedTokens := p.countTokens(compressed)

    // 10. Forward to upstream (streaming)
    outputTokens := p.forwardAndStream(w, r, provider, newBody)

    // 11. Post-request async work (non-blocking)
    p.analyticsQueue <- AnalyticsEvent{
        Type:            RequestProcessed,
        OriginalTokens:  originalTokens,
        CompressedTokens: compressedTokens,
        OutputTokens:    outputTokens,
        Provider:        provider,
        CompressionRatio: float64(compressedTokens) / float64(originalTokens),
    }

    // 12. Trigger async Layer 2 compression if needed
    if p.shouldTriggerCompression(compressed) {
        select {
        case p.compressQueue <- CompressJob{Messages: messages, Timestamp: time.Now()}:
            // job queued
        default:
            // queue full, skip (compression already in progress)
        }
    }
}
```

### 7.3 SSE Streaming Relay

```go
// Stream Server-Sent Events from upstream to client
// Must be zero-copy, minimal buffering, immediate forwarding
//
// Implementation: read upstream response line by line, write to client immediately
// Count output tokens by parsing SSE data events containing content deltas

func (p *Proxy) forwardAndStream(w http.ResponseWriter, r *http.Request,
    provider Provider, body []byte) (outputTokens int) {

    // Create upstream request
    upstreamReq := p.buildUpstreamRequest(r, provider, body)

    // Send to upstream
    resp, err := p.upstreamClients[provider].Do(upstreamReq)
    if err != nil {
        p.proxyError(w, err)
        return 0
    }
    defer resp.Body.Close()

    // Copy headers
    for k, vv := range resp.Header {
        for _, v := range vv {
            w.Header().Add(k, v)
        }
    }
    w.WriteHeader(resp.StatusCode)

    // Stream SSE events
    flusher, ok := w.(http.Flusher)
    if !ok {
        // Non-streaming response (shouldn't happen with SSE, but handle gracefully)
        io.Copy(w, resp.Body)
        return 0
    }

    scanner := bufio.NewScanner(resp.Body)
    scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

    for scanner.Scan() {
        line := scanner.Bytes()
        w.Write(line)
        w.Write([]byte("\n"))
        flusher.Flush()

        // Count output tokens from content delta events
        outputTokens += p.extractTokenCountFromSSE(provider, line)
    }

    return outputTokens
}
```

### 7.4 Async Compression Worker

```go
func (p *Proxy) compressionWorker() {
    defer p.wg.Done()

    for {
        select {
        case job := <-p.compressQueue:
            p.runCompressionJob(job)
        case <-p.shutdownCh:
            return
        }
    }
}

func (p *Proxy) runCompressionJob(job CompressJob) {
    p.summaryCache.compressing.Store(true)
    defer p.summaryCache.compressing.Store(false)

    // Determine which messages need summarizing
    windowSize := p.config.SlidingWindowSize
    totalMessages := len(job.Messages)

    if totalMessages <= windowSize+2 {
        return // too few messages to warrant compression
    }

    summaryBoundary := totalMessages - windowSize
    messagesToSummarize := job.Messages[:summaryBoundary]

    // Detect anchor points
    anchors := p.anchorDetector.Detect(messagesToSummarize)

    // Filter out anchored messages from summarization input
    summarizable := filterNonAnchored(messagesToSummarize, anchors)

    // Check if we already have a valid summary covering part of this range
    existing, existingRange := p.summaryCache.GetCurrent()
    if existing != nil && existingRange[1] >= summaryBoundary-3 {
        // Existing summary is close enough, just extend it
        newMessages := job.Messages[existingRange[1]:summaryBoundary]
        if len(newMessages) < 3 {
            return // not enough new messages to warrant re-compression
        }
        // Incremental summarization: summarize only new messages and append
        p.incrementalSummarize(existing, newMessages, anchors)
        return
    }

    // Full summarization via MiniMax
    inputText := p.formatMessagesForSummarization(summarizable)
    targetTokens := p.countTokens(summarizable) / 5 // target 20% of original

    summary, err := p.minimax.Summarize(inputText, targetTokens)
    if err != nil {
        slog.Warn("MiniMax summarization failed", "error", err)
        return // graceful degradation: skip Layer 2
    }

    // Validate summary quality
    valid, reason := p.validator.Validate(messagesToSummarize, summary)
    if !valid {
        slog.Warn("Summary validation failed", "reason", reason)
        return // graceful degradation: skip Layer 2
    }

    // Store in cache
    p.summaryCache.Store(&CachedSummary{
        Summary:          summary,
        CoveredRange:     [2]int{0, summaryBoundary},
        AnchorsInlined:   anchors,
        OriginalTokens:   p.countTokens(messagesToSummarize),
        CompressedTokens: p.countTokens([]Message{{Content: summary}}),
        CompressionRatio: float64(p.countTokens([]Message{{Content: summary}})) /
                          float64(p.countTokens(messagesToSummarize)),
        CreatedAt:        time.Now(),
    })

    slog.Info("Compression complete",
        "originalTokens", p.countTokens(messagesToSummarize),
        "compressedTokens", p.countTokens([]Message{{Content: summary}}),
        "ratio", fmt.Sprintf("%.1f%%", p.summaryCache.current.CompressionRatio*100),
    )
}
```

### 7.5 Graceful Shutdown

```go
func (p *Proxy) Shutdown(ctx context.Context) error {
    // 1. Stop accepting new requests
    p.server.Shutdown(ctx)

    // 2. Signal all workers to stop
    close(p.shutdownCh)

    // 3. Wait for in-flight requests and workers to finish
    done := make(chan struct{})
    go func() {
        p.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        slog.Info("All workers stopped gracefully")
    case <-ctx.Done():
        slog.Warn("Shutdown timeout, some workers may not have finished")
    }

    // 4. Persist analytics to disk
    p.analytics.FlushToDisk()

    // 5. Close file watcher
    p.fileWatcher.Close()

    return nil
}
```

---

## 8. Multi-Provider Support

### 8.1 Provider Detection

```go
type Provider int

const (
    Anthropic Provider = iota
    OpenAI
)

// Detection based on request format:
//
// Anthropic Messages API:
// - POST /v1/messages
// - Body contains: "model", "messages" with role/content, "max_tokens"
// - Messages have: role "user"/"assistant", content is string or []ContentBlock
// - Tool use: type "tool_use" / "tool_result"
//
// OpenAI Chat Completions API:
// - POST /v1/chat/completions
// - Body contains: "model", "messages" with role/content
// - Messages have: role "user"/"assistant"/"system"/"tool"
// - Tool use: "tool_calls" array / role "tool"
//
// Auto-detection: check URL path first, then body structure as fallback

func detectProvider(r *http.Request, body []byte) Provider {
    path := r.URL.Path
    if strings.Contains(path, "/messages") {
        return Anthropic
    }
    if strings.Contains(path, "/chat/completions") {
        return OpenAI
    }
    // Fallback: check body structure
    if gjson.GetBytes(body, "max_tokens").Exists() &&
       !gjson.GetBytes(body, "frequency_penalty").Exists() {
        return Anthropic
    }
    return OpenAI
}
```

### 8.2 Message Format Normalization

Internally, the proxy works with a normalized message format. Input is converted
from provider format to internal, compressed, then converted back.

```go
// Internal normalized message format
type Message struct {
    Index       int              // position in original conversation
    Role        string           // "user", "assistant", "system", "tool"
    Content     []ContentBlock   // unified content blocks
    ToolUse     *ToolUse         // if this message contains a tool call
    ToolResult  *ToolResult      // if this message is a tool result
    Metadata    MessageMetadata  // compression metadata
}

type ContentBlock struct {
    Type         string  // "text", "image", "tool_use", "tool_result"
    Text         string  // for text blocks
    ToolName     string  // for tool_use blocks
    ToolInput    string  // for tool_use blocks (JSON string)
    ToolResultID string  // for tool_result blocks
    CacheControl *CacheControl // for Anthropic prompt caching
}

type MessageMetadata struct {
    OriginalTokens   int
    CompressedTokens int
    IsAnchor         bool
    AnchorType       AnchorType
    CompressionLevel int  // 0=none, 1=Layer1, 2=Layer2
}
```

### 8.3 Authentication: OAuth Passthrough (No API Keys Required)

Both supported CLIs use OAuth/subscription authentication, not API keys.
The proxy does NOT need credentials. It passes through all auth headers transparently.

**Claude Code (Anthropic) - OAuth via Bridge:**

```
Authentication flow:
1. User logs into Claude Code via browser OAuth (one-time)
2. Claude Code stores OAuth session in ~/.claude.json
3. Claude Code's "Bridge" system manages token refresh
4. Each API request includes OAuth bearer tokens in headers

Proxy behavior:
- Proxy receives request with OAuth headers from Claude Code
- Proxy forwards ALL headers unchanged (including auth)
- Anthropic sees a normal Claude Code request with valid OAuth
- Proxy never reads, stores, or logs any auth tokens
```

**Compatibility evidence (from Claude Code changelog on this system):**
- "Fixed API 400 errors when using ANTHROPIC_BASE_URL with a third-party gateway"
- "Fixed tool search to activate even with ANTHROPIC_BASE_URL"
- This confirms Anthropic actively supports ANTHROPIC_BASE_URL with OAuth sessions.

**Codex (OpenAI) - OAuth via ChatGPT:**

```
Authentication flow:
1. User logs into Codex via `codex login` (Google OAuth)
2. Codex stores JWT tokens in ~/.codex/auth.json (id_token, access_token, refresh_token)
3. Token refresh handled automatically via https://auth.openai.com/oauth/token
4. Each API request includes ChatGPT-Account-Id header + Bearer token

Proxy behavior:
- Same as Claude Code: all headers forwarded unchanged
- Proxy never touches auth tokens

Caveat:
- Codex uses TWO backends: api.openai.com/v1 AND chatgpt.com/backend-api/codex
- The base URL override (openai_base_url in config.toml) likely redirects only
  the api.openai.com calls, not the chatgpt.com backend calls
- Some Codex features may bypass the proxy - compression still applies to
  the redirected calls, partial benefit is expected
- Requires testing to determine exact coverage
```

**Upstream client configuration:**

```go
type UpstreamConfig struct {
    Anthropic struct {
        BaseURL string // default: https://api.anthropic.com
    }
    OpenAI struct {
        BaseURL string // default: https://api.openai.com
    }
}

// The proxy NEVER stores, reads, or logs authentication tokens.
// All auth headers (OAuth bearer tokens, session IDs, account IDs)
// are forwarded from the incoming request to upstream unchanged.
// The proxy is a transparent passthrough for authentication.
```

### 8.4 Provider Compatibility Matrix

| Feature | Claude Code | Codex |
|---|---|---|
| Base URL override | ANTHROPIC_BASE_URL (env) | openai_base_url (config.toml) |
| Auth type | OAuth via Bridge | OAuth via ChatGPT |
| Auth passthrough | Headers forwarded 1:1 | Headers forwarded 1:1 |
| Official support | Yes (changelog confirms) | Supported but deprecated env var |
| Coverage | Full (all API calls) | Partial (api.openai.com only, chatgpt.com backend may bypass) |
| Confidence level | HIGH | MEDIUM (test required) |

### 8.5 Setup: How Each CLI Connects to the Proxy

**Claude Code:**
```bash
# Add to ~/.zshrc:
export ANTHROPIC_BASE_URL=http://127.0.0.1:8990
# That's it. Claude Code reads this on every launch.
# OAuth login continues to work normally (login goes to Anthropic directly).
# Only API requests (messages) are routed through the proxy.
```

**Codex:**
```toml
# Add to ~/.codex/config.toml:
openai_base_url = "http://127.0.0.1:8990"
# Or via environment variable (deprecated but functional):
# export OPENAI_BASE_URL=http://127.0.0.1:8990
```

### 8.6 Pre-Flight Validation Test

Before first use, run the built-in connectivity test:

```bash
# This starts a temporary HTTP listener, instructs the user to
# run a test command with the CLI, and verifies the request arrives.
tokenproxy test intercept claude    # tests Claude Code routing
tokenproxy test intercept codex     # tests Codex routing
```

Implementation:
```go
// tokenproxy test intercept <provider>
//
// 1. Start HTTP listener on configured port
// 2. Print instructions:
//    "Open another terminal and run:"
//    "  ANTHROPIC_BASE_URL=http://127.0.0.1:8990 claude 'test'"
// 3. Wait up to 60 seconds for an incoming request
// 4. On request received:
//    - Print: "Request received from Claude Code"
//    - Print: headers (User-Agent, auth type, anthropic-version)
//    - Print: body size, message count, model
//    - Return 200 with minimal valid response
//    - Print: "PASS - proxy intercept works"
// 5. On timeout:
//    - Print: "FAIL - no request received"
//    - Print: troubleshooting steps
```

### 8.7 Provider-Specific Optimizations

**Anthropic-only features:**
- Prompt cache breakpoint injection (cache_control)
- Extended thinking support (pass through unmodified)
- Content block structure preservation (image blocks, tool_use blocks)

**OpenAI-only features:**
- System message optimization (single system message at position 0)
- Function calling format preservation
- Logprobs passthrough

---

## 9. Secret Detection & Redaction

Bonus security layer: scan all outgoing requests for accidentally included secrets.

### 9.1 Detection Patterns

```go
type SecretPattern struct {
    Name    string
    Regex   *regexp.Regexp
    Entropy float64  // minimum Shannon entropy to trigger (0 = regex only)
}

var defaultPatterns = []SecretPattern{
    // API Keys
    {Name: "AWS Access Key",       Regex: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
    {Name: "AWS Secret Key",       Regex: regexp.MustCompile(`(?i)aws_secret_access_key\s*=\s*\S+`)},
    {Name: "GitHub Token",         Regex: regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`)},
    {Name: "GitHub Token (fine)",  Regex: regexp.MustCompile(`github_pat_[a-zA-Z0-9_]{82}`)},
    {Name: "Anthropic API Key",    Regex: regexp.MustCompile(`sk-ant-[a-zA-Z0-9-]{90,}`)},
    {Name: "OpenAI API Key",       Regex: regexp.MustCompile(`sk-[a-zA-Z0-9]{48,}`)},
    {Name: "Stripe Key",           Regex: regexp.MustCompile(`(?:sk|pk)_(?:test|live)_[a-zA-Z0-9]{24,}`)},
    {Name: "Generic Bearer Token", Regex: regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_.~+/=-]{20,}`)},

    // Passwords and secrets in env/config
    {Name: "Password in config",   Regex: regexp.MustCompile(`(?i)(?:password|passwd|secret|token)\s*[:=]\s*["']?\S{8,}`)},
    {Name: ".env value",           Regex: regexp.MustCompile(`(?i)(?:DATABASE_URL|REDIS_URL|SECRET_KEY|PRIVATE_KEY)\s*=\s*\S+`)},

    // Private keys
    {Name: "RSA Private Key",      Regex: regexp.MustCompile(`-----BEGIN (?:RSA )?PRIVATE KEY-----`)},
    {Name: "SSH Private Key",      Regex: regexp.MustCompile(`-----BEGIN OPENSSH PRIVATE KEY-----`)},

    // High-entropy strings (potential secrets)
    {Name: "High-entropy string",  Regex: regexp.MustCompile(`[a-zA-Z0-9+/=]{40,}`), Entropy: 4.5},
}
```

### 9.2 Redaction Behavior

```go
// When a secret is detected:
// 1. Replace the matched string with: [REDACTED:{pattern_name}]
// 2. Log the detection (pattern name + message index, NOT the secret value)
// 3. Increment analytics counter
// 4. Continue processing (do not block the request)
//
// Configuration options:
// - mode: "redact" (default) | "warn" (log but don't redact) | "block" (reject request)
// - allowlist: paths that are exempt (e.g., test fixtures with fake keys)
// - custom_patterns: user-defined regex patterns
```

---

## 10. Analytics & Observability

### 10.1 Metrics Tracked

```go
type Analytics struct {
    mu sync.RWMutex

    // Per-session metrics
    SessionStart       time.Time
    TotalRequests      int
    TotalInputTokens   int     // original (before compression)
    TotalOutputTokens  int     // from API responses
    SavedInputTokens   int     // tokens removed by compression
    CacheHits          int
    CacheMisses        int
    Layer1Savings      int     // tokens saved by Layer 1 alone
    Layer2Savings      int     // tokens saved by Layer 2 alone
    Layer3Savings      int     // tokens saved by caching
    SecretsRedacted    int
    CompressionCalls   int     // MiniMax API calls made
    Errors             int

    // Per-request history (ring buffer, last 100 requests)
    RequestLog         *RingBuffer[RequestMetrics]

    // Usage tracking (subscription-focused, not cost-focused)
    AvgTokensPerRequest    int       // running average of input tokens per request
    EstExtraMessages       int       // estimated additional messages gained from savings
    AvgTTFTImprovement     float64   // estimated TTFT improvement in seconds
}

// EstExtraMessages calculation:
// If we saved S tokens total, and avg request uses A tokens (after compression),
// then extra messages = S / A
// This tells the user: "you got ~N extra messages out of your rate limit"
//
// AvgTTFTImprovement estimation:
// Anthropic Opus prefill speed ~50K tokens/second (approximate)
// TTFT reduction = SavedInputTokens / 50000 / TotalRequests (seconds per request)
// This is a rough estimate but gives the user a feel for the speed improvement

type RequestMetrics struct {
    Timestamp        time.Time
    Provider         Provider
    Model            string
    InputTokensOrig  int
    InputTokensComp  int
    OutputTokens     int
    CompressionRatio float64
    Layers           []int         // which layers applied [1], [1,2], [1,2,3]
    Latency          time.Duration // proxy overhead only (not upstream)
    CacheHit         bool
}
```

### 10.2 Interactive TUI Dashboard (BubbleTea + Lipgloss)

The TUI IS the application. No separate CLI commands, no daemon mode.
`tokenproxy` starts the TUI. The proxy runs as goroutines inside the same process.
Close the TUI = proxy stops. Open = proxy runs.

**Framework:** charmbracelet/bubbletea (Elm-architecture TUI) + lipgloss (styling)

**Layout - single screen, live-updating, keyboard-driven:**

```
╭── TokenProxy v1.0.0 ──────────────────────────── Session: 2h 14m ──╮
│                                                                      │
│  ● Claude Code  [ON]       ● Codex  [ON]          Port: 8990       │
│                                                                      │
│  Usage ───────────────────────────────────────────────────────       │
│  Messages: 47 sent    Tokens: 1,247K -> 412K  (835K saved)         │
│  Compression: 67%     ~23 extra messages gained this session        │
│  Avg TTFT improvement: ~1.8s faster per response                    │
│                                                                      │
│  ████████████████████░░░░░░░░░  67%  compression ratio              │
│                                                                      │
│  Layers ───────────────────────────────────────────────────────      │
│  [1] Deterministic  ● ON   312K saved   (JSON, dedup, tree-sitter) │
│  [2] MiniMax        ● ON   498K saved   last: 2m ago  queue: 0    │
│  [3] Cache          ● ON    24K saved   hits: 8/47  (17%)         │
│                                                                      │
│  Live ─────────────────────────────────────────────────────────      │
│  14:32  claude   opus    87.4K -> 29.1K  (67%)  L1+L2  0.4ms      │
│  14:31  claude   opus    84.2K -> 28.6K  (66%)  L1+L2  0.3ms      │
│  14:31  codex    o3      42.3K -> 18.2K  (57%)  L1     0.2ms      │
│  14:30  claude   opus    79.1K -> 24.1K  (66%)  L1+L2  0.4ms      │
│  14:29  claude   opus    71.0K -> 24.1K  (66%)  L1+L2  0.3ms      │
│                                                                      │
│  Secrets: 3 redacted   Retries: 2 (1x 429, 1x overflow)           │
╰──── [c] claude  [x] codex  [1-3] layers  [s] stats  [q] quit ──────╯
```

**Keyboard controls (single keypress, no Enter needed):**

| Key | Action | Visual Feedback |
|---|---|---|
| `c` | Toggle Claude Code proxy ON/OFF | Indicator changes ● ON / ○ OFF, color green/gray |
| `x` | Toggle Codex proxy ON/OFF | Same indicator toggle |
| `1` | Toggle Layer 1 (Deterministic) | Layer line changes ON/OFF |
| `2` | Toggle Layer 2 (MiniMax) | Layer line changes ON/OFF |
| `3` | Toggle Layer 3 (Cache) | Layer line changes ON/OFF |
| `s` | Toggle stats detail view | Switches to full stats screen and back |
| `d` | Toggle debug log view | Shows last 20 slog entries at bottom |
| `f` | Flush all caches | Brief flash confirmation "Caches flushed" |
| `q` | Quit (graceful shutdown) | Shows "Shutting down..." then exits |

**When a provider is toggled OFF:**
- Requests for that provider are passed through UNMODIFIED (zero compression)
- The indicator shows ○ OFF in gray
- Savings counters for that provider pause
- Useful when: debugging, testing without proxy, or if compression causes issues

**When a layer is toggled OFF:**
- That compression step is skipped in the pipeline
- Other layers continue to function
- Useful when: Layer 2 MiniMax is slow, or you want to isolate which layer
  causes a specific behavior

**BubbleTea Model Architecture:**

```go
// The TUI follows the Elm Architecture (Model-Update-View):
//
// Model: holds all state (proxy stats, toggle states, request log)
// Update: handles keyboard input + tick events + proxy events
// View: renders the current state to a string (lipgloss styled)

type TUIModel struct {
    // Proxy reference
    proxy *proxy.Proxy

    // Toggle states
    claudeEnabled bool  // default: true
    codexEnabled  bool  // default: true
    layer1Enabled bool  // default: true
    layer2Enabled bool  // default: true
    layer3Enabled bool  // default: true

    // Current view
    view          ViewMode  // main, stats, debug

    // Live data (updated via channel from proxy goroutines)
    stats         *analytics.Analytics
    requestLog    []RequestMetrics  // last 10 requests for live view
    lastUpdate    time.Time

    // UI state
    width         int  // terminal width (from WindowSizeMsg)
    height        int  // terminal height
    flashMessage  string  // temporary status message (e.g., "Caches flushed")
    flashExpiry   time.Time
}

type ViewMode int

const (
    ViewMain  ViewMode = iota  // default dashboard
    ViewStats                   // detailed statistics
    ViewDebug                   // debug log tail
)

// Messages (events flowing into the TUI):
//
// tea.KeyMsg          - keyboard input
// tea.WindowSizeMsg   - terminal resize
// tickMsg             - periodic refresh (every 500ms)
// proxyEventMsg       - new request processed (from proxy goroutine via channel)
// compressionDoneMsg  - MiniMax compression completed
// flashExpiredMsg     - clear flash message

type tickMsg time.Time
type proxyEventMsg RequestMetrics
type compressionDoneMsg struct{ ratio float64 }
type flashExpiredMsg struct{}
```

**Update function (core logic):**

```go
func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {

    case tea.KeyMsg:
        switch msg.String() {
        case "c":
            m.claudeEnabled = !m.claudeEnabled
            m.proxy.SetProviderEnabled(proxy.Anthropic, m.claudeEnabled)
        case "x":
            m.codexEnabled = !m.codexEnabled
            m.proxy.SetProviderEnabled(proxy.OpenAI, m.codexEnabled)
        case "1":
            m.layer1Enabled = !m.layer1Enabled
            m.proxy.SetLayerEnabled(1, m.layer1Enabled)
        case "2":
            m.layer2Enabled = !m.layer2Enabled
            m.proxy.SetLayerEnabled(2, m.layer2Enabled)
        case "3":
            m.layer3Enabled = !m.layer3Enabled
            m.proxy.SetLayerEnabled(3, m.layer3Enabled)
        case "s":
            if m.view == ViewStats { m.view = ViewMain } else { m.view = ViewStats }
        case "d":
            if m.view == ViewDebug { m.view = ViewMain } else { m.view = ViewDebug }
        case "f":
            m.proxy.FlushCaches()
            m.flashMessage = "All caches flushed"
            m.flashExpiry = time.Now().Add(2 * time.Second)
            return m, flashTimer()
        case "q", "ctrl+c":
            m.proxy.Shutdown(context.Background())
            return m, tea.Quit
        }

    case tickMsg:
        m.stats = m.proxy.GetAnalytics()
        m.requestLog = m.proxy.GetRecentRequests(10)
        m.lastUpdate = time.Now()
        return m, tickCmd()

    case proxyEventMsg:
        m.requestLog = append(m.requestLog, RequestMetrics(msg))
        if len(m.requestLog) > 10 {
            m.requestLog = m.requestLog[len(m.requestLog)-10:]
        }

    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
    }

    return m, nil
}
```

**View function (rendering with lipgloss):**

```go
func (m TUIModel) View() string {
    switch m.view {
    case ViewStats:
        return m.renderStatsView()
    case ViewDebug:
        return m.renderDebugView()
    default:
        return m.renderMainView()
    }
}

func (m TUIModel) renderMainView() string {
    // Lipgloss styles
    border := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("62")).  // subtle purple
        Padding(0, 1).
        Width(m.width - 2)

    title := lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("170"))  // warm accent

    onBadge := lipgloss.NewStyle().
        Foreground(lipgloss.Color("42")).  // green
        Bold(true).
        Render("● ON")

    offBadge := lipgloss.NewStyle().
        Foreground(lipgloss.Color("240")).  // gray
        Render("○ OFF")

    savedStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color("42")).  // green
        Bold(true)

    dimStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color("240"))

    // Build sections
    header := title.Render("TokenProxy v1.0.0") +
        dimStyle.Render("  Session: " + m.sessionDuration())

    // Provider toggles
    claudeStatus := onBadge
    if !m.claudeEnabled { claudeStatus = offBadge }
    codexStatus := onBadge
    if !m.codexEnabled { codexStatus = offBadge }

    providers := fmt.Sprintf("  %s Claude Code  %s     %s Codex  %s          Port: %d",
        claudeIndicator(m.claudeEnabled), claudeStatus,
        codexIndicator(m.codexEnabled), codexStatus,
        m.proxy.Config().ListenPort)

    // Usage section
    ratio := m.stats.CompressionRatio()
    bar := renderProgressBar(ratio, m.width-6)
    usage := fmt.Sprintf(
        "  Messages: %d sent    Tokens: %s -> %s  (%s saved)\n"+
        "  Compression: %s     ~%d extra messages gained this session\n"+
        "  Avg TTFT improvement: ~%.1fs faster per response",
        m.stats.TotalRequests,
        formatTokens(m.stats.TotalInputTokens),
        formatTokens(m.stats.TotalInputTokens - m.stats.SavedInputTokens),
        savedStyle.Render(formatTokens(m.stats.SavedInputTokens)),
        savedStyle.Render(fmt.Sprintf("%d%%", int(ratio*100))),
        m.stats.EstExtraMessages,
        m.stats.AvgTTFTImprovement)

    // Layer status lines
    layers := m.renderLayerLines()

    // Live request log
    live := m.renderRequestLog()

    // Footer
    footer := m.renderFooter()

    // Compose
    content := strings.Join([]string{
        header, "", providers, "",
        dimStyle.Render("  Usage"), usage, "  " + bar, "",
        dimStyle.Render("  Layers"), layers, "",
        dimStyle.Render("  Live"), live, "",
        footer,
    }, "\n")

    return border.Render(content)
}
```

**Stats detail view (toggle with `s`):**

```
╭── TokenProxy - Detailed Statistics ─────────────────────────────────╮
│                                                                      │
│  Session Summary                                                     │
│  Started: 2026-04-09 12:18:04    Duration: 2h 14m                  │
│  Messages: 47 sent    Errors: 0    MiniMax calls: 12               │
│                                                                      │
│  Usage Savings                                                       │
│  ┌──────────────────┬───────────┬───────────┬────────┐              │
│  │ Metric           │ Original  │ After     │ Saved  │              │
│  ├──────────────────┼───────────┼───────────┼────────┤              │
│  │ Total Input      │ 1,247,000 │ 412,510   │ 67%    │              │
│  │ Layer 1 (determ) │           │           │ 312K   │              │
│  │ Layer 2 (MiniMax)│           │           │ 498K   │              │
│  │ Layer 3 (cache)  │           │           │ 24.5K  │              │
│  │ Total Output     │ 234,000   │ (passthru)│ -      │              │
│  └──────────────────┴───────────┴───────────┴────────┘              │
│                                                                      │
│  Effective Capacity Gain                                             │
│  ┌──────────────────────────────────────────────────┐               │
│  │ Extra messages gained:  ~23 additional           │               │
│  │ Session extension:      ~3x longer before limit  │               │
│  │ Avg TTFT improvement:   ~1.8s faster/response    │               │
│  │ Avg compression ratio:  67% (per request)        │               │
│  │ Avg tokens/request:     8,776 (was 26,532)       │               │
│  └──────────────────────────────────────────────────┘               │
│                                                                      │
│  Per Provider                                                        │
│  ┌──────────────────┬──────────┬──────────┬──────────┐              │
│  │ Provider         │ Messages │ Saved    │ Avg %    │              │
│  ├──────────────────┼──────────┼──────────┼──────────┤              │
│  │ Claude Code      │ 38       │ 724K tok │ 69%      │              │
│  │ Codex            │ 9        │ 111K tok │ 57%      │              │
│  └──────────────────┴──────────┴──────────┴──────────┘              │
│                                                                      │
│  MiniMax Compression Engine                                          │
│  Calls: 12    Avg latency: 4.2s    Avg ratio: 18%                  │
│  Queue depth: 0    Last run: 2m ago    Failures: 0                  │
│                                                                      │
│  Latency (avg last 20 requests)                                      │
│  ┌──────────────────┬────────┬────────┬────────┐                    │
│  │ Provider         │ TTFT   │ Total  │ Proxy  │                    │
│  ├──────────────────┼────────┼────────┼────────┤                    │
│  │ Claude Opus      │ 1.8s   │ 12.4s  │ 0.4ms  │                    │
│  │ Codex o3         │ 3.1s   │ 8.7s   │ 0.3ms  │                    │
│  └──────────────────┴────────┴────────┴────────┘                    │
│                                                                      │
│  Resilience                                                          │
│  Auto-retries: 2 (1x rate-limit, 1x context-overflow)              │
│  Secrets redacted: 3 (2x AWS Access Key, 1x GitHub Token)          │
│                                                                      │
╰────────────────────────────────────── [s] back  [q] quit ───────────╯
```

**Debug log view (toggle with `d`):**

```
╭── TokenProxy - Debug Log ───────────────────────────────────────────╮
│                                                                      │
│  14:32:01 INFO  request_processed provider=anthropic model=opus     │
│    input_orig=87400 input_comp=29100 ratio=0.33 layers=[1,2]       │
│  14:32:01 DEBUG layer1 json_compact saved=4200 dedup=12300          │
│  14:32:01 DEBUG layer2 summary_cache_hit range=[0,38]               │
│  14:31:58 INFO  compression_complete range=[0,38] ratio=0.18        │
│    original=198000 compressed=35640 anchors=[8,14,22,31]            │
│  14:31:45 INFO  request_processed provider=anthropic model=opus     │
│    input_orig=84200 input_comp=28600 ratio=0.34 layers=[1,2]       │
│  14:31:45 DEBUG layer1 treesitter saved=8900 files=3                │
│  14:31:12 INFO  request_processed provider=openai model=o3          │
│    input_orig=42300 input_comp=18200 ratio=0.43 layers=[1]         │
│  14:31:12 DEBUG layer2 skipped reason=provider_disabled              │
│  14:30:33 WARN  secret_detected type=aws_access_key msg_idx=42     │
│    action=redacted                                                   │
│  14:30:33 INFO  request_processed provider=anthropic model=opus     │
│    input_orig=79100 input_comp=24100 ratio=0.30 layers=[1,2]       │
│  ...                                                                 │
│                                                                      │
╰──────────────────────────────────────── [d] back  [q] quit ─────────╯
```

**Startup sequence (what happens when you run `tokenproxy`):**

```go
func main() {
    // 1. Load config (TOML + env overrides)
    cfg := config.Load()

    // 2. Initialize proxy (all layers, goroutines)
    p := proxy.New(cfg)

    // 3. Start proxy HTTP server in background goroutine
    go p.Start()

    // 4. Start BubbleTea TUI (blocks main goroutine)
    model := tui.NewModel(p)
    program := tea.NewProgram(model, tea.WithAltScreen())

    // 5. Run TUI (blocks until quit)
    if _, err := program.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
        os.Exit(1)
    }

    // 6. TUI exited -> proxy already shut down in quit handler
}
```

**Additional CLI subcommands (non-TUI, for scripting/automation):**

```bash
# These run without TUI, print to stdout, and exit:

tokenproxy config init     # Generate default ~/.tokenproxy/config.toml
tokenproxy config show     # Print resolved config (file + env)
tokenproxy test minimax    # Test MiniMax API connectivity, print result
tokenproxy test anthropic  # Test Anthropic API connectivity
tokenproxy test openai     # Test OpenAI API connectivity
tokenproxy doctor          # Run all connectivity + config diagnostics
tokenproxy stats today     # Print today's stats from persisted analytics
tokenproxy stats week      # Print this week's stats
tokenproxy version         # Print version and exit
```

These are utility commands that do NOT start the proxy or TUI.
They are for setup, diagnostics, and reviewing historical data.

**Responsive layout (adapts to terminal size):**

```go
// The TUI adapts to terminal dimensions via tea.WindowSizeMsg:
//
// Width >= 80:  Full layout as shown above
// Width 60-79:  Compact layout (shorter labels, no table borders)
// Width < 60:   Minimal layout (savings + last 3 requests only)
//
// Height >= 30: Full layout with 10 request log entries
// Height 20-29: Reduced to 5 request log entries
// Height < 20:  Header + savings + providers only (no request log)
//
// The progress bar width scales linearly with terminal width.
// Table columns are right-aligned with dynamic padding.
```

### 10.3 Log Output

```go
// Structured logging via slog
// Log levels:
//   INFO:  request processed, compression stats, cache hits
//   WARN:  MiniMax call failed (graceful degradation), validation failure
//   ERROR: upstream connection failed, parse error
//   DEBUG: detailed compression decisions, anchor detection, token counts

// Log format example:
// 2026-04-09T14:32:01Z INFO  request_processed provider=anthropic model=claude-opus-4-6
//   input_orig=87400 input_comp=29100 output=3200 ratio=0.33 layers=[1,2]
//   cache_hit=false latency_ms=0.4 secrets_redacted=0
```

### 10.4 Persistent Analytics

```go
// Session analytics are persisted to disk on shutdown and on periodic flush (every 5 min).
// Format: JSON lines (one JSON object per line) appended to analytics log file.
// Location: ~/.tokenproxy/analytics/YYYY-MM-DD.jsonl
//
// Enables: historical analysis of token savings, cost tracking over time,
// identification of usage patterns, ROI measurement.
```

---

## 11. Configuration System

### 11.1 Config File

Location: `~/.tokenproxy/config.toml`

```toml
# TokenProxy Configuration

[proxy]
listen_address = "127.0.0.1"
listen_port = 8990
# Set to true to also listen on IPv6 loopback
ipv6 = false

[upstream.anthropic]
base_url = "https://api.anthropic.com"
# API key is NEVER stored here - passed through from client headers

[upstream.openai]
base_url = "https://api.openai.com"

[compression]
# Enable/disable individual layers
layer1_enabled = true
layer2_enabled = true
layer3_enabled = true

# Sliding window: number of recent message exchanges to keep uncompressed
sliding_window = 5

# Minimum conversation length before any compression triggers
min_messages_for_compression = 8

# Minimum token count before Layer 2 triggers
min_tokens_for_layer2 = 30000

# Tree-sitter: minimum file size (tokens) to apply signature extraction
tree_sitter_min_tokens = 500

# Tree-sitter: languages to support (others pass through uncompressed)
tree_sitter_languages = ["go", "typescript", "javascript", "rust", "python"]

# Deduplication: similarity threshold for near-duplicate detection (0.0-1.0)
dedup_similarity_threshold = 0.85

[compression.minimax]
base_url = "https://api.minimax.io/v1"
api_key_env = "MINIMAX_API_KEY"  # environment variable name containing the key
model = "minimax-m2.7"
temperature = 0.1
max_retries = 2
connect_timeout_seconds = 5
response_timeout_seconds = 30
# Self-imposed rate limit (requests per minute) to stay well within plan limits
rate_limit_rpm = 10

[compression.summary]
# Target compression ratio for MiniMax summaries (0.0-1.0)
target_ratio = 0.20
# Maximum acceptable ratio (above this, summary is rejected)
max_ratio = 0.40
# Minimum acceptable ratio (below this, summary is suspiciously lossy)
min_ratio = 0.05

[cache]
# Response cache
response_cache_max_entries = 100
response_cache_ttl_seconds = 300

# Summary cache auto-refresh interval
summary_refresh_interval_seconds = 1800

[usage]
# TTFT estimation: approximate prefill speed of the upstream model (tokens/second)
# Used to calculate TTFT improvement shown in dashboard
# Opus ~50K tok/s, Sonnet ~80K tok/s, o3 ~40K tok/s
estimated_prefill_speed = 50000

[secrets]
# Secret detection mode: "redact", "warn", "block", "off"
mode = "redact"
# Custom patterns (in addition to built-in patterns)
# custom_patterns = [
#   { name = "Internal API", regex = "internal-api-[a-z0-9]{32}" }
# ]
# Paths exempt from secret detection
# allowlist = ["test/fixtures/**"]

[analytics]
# Enable terminal dashboard
dashboard = true
# Persistent log directory
log_dir = "~/.tokenproxy/analytics"
# Dashboard refresh interval
dashboard_refresh_seconds = 2

[logging]
# Log level: "debug", "info", "warn", "error"
level = "info"
# Log format: "text", "json"
format = "text"
# Log file (empty = stderr only)
file = ""
```

### 11.2 Environment Variable Overrides

Every config value can be overridden via environment variable:

```bash
TOKENPROXY_LISTEN_PORT=9090
TOKENPROXY_COMPRESSION_SLIDING_WINDOW=8
TOKENPROXY_BUDGET_DAILY_TOKEN_LIMIT=5000000
TOKENPROXY_SECRETS_MODE=block
# etc.

# Pattern: TOKENPROXY_{SECTION}_{KEY} in uppercase, dots replaced by underscores
```

### 11.3 CLI Flag Overrides

```bash
tokenproxy --port 9090 --sliding-window 8 --no-layer2
# CLI flags override env vars, env vars override config file
```

---

## 12. Usage & Quick Setup

### 12.1 One-Command Start

```bash
# That's it. One command. TUI opens, proxy runs.
tokenproxy
```

No `start`, no `--daemon`, no PID files, no systemd units. Open a terminal tab,
run `tokenproxy`, leave it running. Close it when done. The proxy lifecycle is
identical to the TUI lifecycle.

### 12.2 First-Time Setup

```bash
# 1. Install
go install github.com/user/tokenproxy@latest

# 2. Generate default config
tokenproxy config init
# -> Creates ~/.tokenproxy/config.toml with sensible defaults

# 3. Set MiniMax API key (for Layer 2 compression)
export MINIMAX_API_KEY="your-key-here"
# Add to ~/.zshrc for persistence

# 4. Verify proxy intercept works with your OAuth sessions
tokenproxy test intercept claude   # follow on-screen instructions
tokenproxy test intercept codex    # follow on-screen instructions

# 5. Route CLI tools through the proxy (add to ~/.zshrc)
export ANTHROPIC_BASE_URL=http://127.0.0.1:8990   # Claude Code
# For Codex, add to ~/.codex/config.toml:
#   openai_base_url = "http://127.0.0.1:8990"

# 6. Run full diagnostics
tokenproxy doctor

# 7. Start
tokenproxy
```

**Important: OAuth login is NOT affected.** The proxy only intercepts API requests
(message calls). OAuth authentication flows (browser login, token refresh) go directly
to Anthropic/OpenAI as before. You do NOT need to log in again or change any credentials.
Your existing Claude Code and Codex sessions continue to work.

### 12.3 Recommended Terminal Layout

```
Terminal Window:
┌─────────────────────────────────────┬─────────────────────────────────────┐
│                                     │                                     │
│   Tab 1: tokenproxy                │   Tab 2: claude / codex             │
│   (TUI dashboard, always visible)  │   (your actual coding CLI)         │
│                                     │                                     │
│   You see savings in real-time     │   Works exactly as before           │
│   while you code in the other tab  │   (proxy is transparent)           │
│                                     │                                     │
└─────────────────────────────────────┴─────────────────────────────────────┘
```

### 12.4 Utility Subcommands (non-TUI)

These commands run without TUI, print to stdout, and exit immediately.
For setup, diagnostics, and reviewing historical data:

```bash
tokenproxy config init       # Generate default config file
tokenproxy config show       # Print resolved config (file + env merged)
tokenproxy test minimax      # Test MiniMax API connectivity
tokenproxy test anthropic    # Test Anthropic upstream reachability
tokenproxy test openai       # Test OpenAI upstream reachability
tokenproxy doctor            # Run ALL diagnostics (config + connectivity + permissions)
tokenproxy stats today       # Print today's token savings from persisted analytics
tokenproxy stats week        # Print this week's aggregated stats
tokenproxy stats month       # Print this month's aggregated stats
tokenproxy version           # Print version string
```

### 12.5 Shell Integration (optional convenience)

```bash
# Add to ~/.zshrc to auto-set env vars when tokenproxy is running:

if curl -s http://127.0.0.1:8990/health > /dev/null 2>&1; then
    export ANTHROPIC_BASE_URL=http://127.0.0.1:8990
    export OPENAI_BASE_URL=http://127.0.0.1:8990
fi
```

This way, if you forget to start tokenproxy, your CLIs talk directly to
the real APIs (no proxy, no breakage). If tokenproxy is running, they
automatically route through it.

---

## 13. Data Structures & Types

### 13.1 Core Types

```go
package proxy

import (
    "crypto/sha256"
    "net/http"
    "sync"
    "sync/atomic"
    "time"
)

// Provider represents an LLM API provider
type Provider int

const (
    Anthropic Provider = iota
    OpenAI
)

func (p Provider) String() string {
    switch p {
    case Anthropic:
        return "anthropic"
    case OpenAI:
        return "openai"
    default:
        return "unknown"
    }
}

// Message is the internal normalized message representation
type Message struct {
    Index       int              `json:"index"`
    Role        string           `json:"role"`
    Content     []ContentBlock   `json:"content"`
    Metadata    MessageMetadata  `json:"metadata"`
}

// ContentBlock is a unified content block across providers
type ContentBlock struct {
    Type         string        `json:"type"`
    Text         string        `json:"text,omitempty"`
    ToolName     string        `json:"tool_name,omitempty"`
    ToolInput    string        `json:"tool_input,omitempty"`
    ToolUseID    string        `json:"tool_use_id,omitempty"`
    ToolResultID string        `json:"tool_result_id,omitempty"`
    ImageData    string        `json:"image_data,omitempty"`
    CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// CacheControl for Anthropic prompt caching
type CacheControl struct {
    Type string `json:"type"` // "ephemeral"
}

// MessageMetadata tracks compression state
type MessageMetadata struct {
    OriginalTokens   int        `json:"original_tokens"`
    CompressedTokens int        `json:"compressed_tokens"`
    IsAnchor         bool       `json:"is_anchor"`
    AnchorType       AnchorType `json:"anchor_type,omitempty"`
    CompressionLevel int        `json:"compression_level"` // 0, 1, 2
    WasDeduped       bool       `json:"was_deduped"`
    WasTreeSittered  bool       `json:"was_tree_sittered"`
    OriginalHash     [32]byte   `json:"-"`
}

// AnchorType classifies why a message is an anchor
type AnchorType int

const (
    AnchorNone     AnchorType = iota
    AnchorEdit                        // contains file edit/write
    AnchorError                       // contains error/failure
    AnchorDecision                    // user confirmed/rejected
    AnchorConfig                      // config file change
)

// CompressJob is sent to the async compression worker
type CompressJob struct {
    Messages  []Message
    Timestamp time.Time
}

// AnalyticsEvent is sent to the analytics collector
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
}

type EventType int

const (
    RequestProcessed EventType = iota
    CacheHit
    CompressionComplete
    SecretDetected
    BudgetWarning
    ErrorOccurred
)
```

### 13.2 Ring Buffer for Request History

```go
type RingBuffer[T any] struct {
    mu    sync.RWMutex
    items []T
    head  int
    size  int
    cap   int
}

func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
    return &RingBuffer[T]{
        items: make([]T, capacity),
        cap:   capacity,
    }
}

func (rb *RingBuffer[T]) Push(item T) {
    rb.mu.Lock()
    defer rb.mu.Unlock()
    rb.items[rb.head] = item
    rb.head = (rb.head + 1) % rb.cap
    if rb.size < rb.cap {
        rb.size++
    }
}

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
```

---

## 14. API Compatibility Layer

### 14.1 Anthropic Messages API

**Supported endpoints:**
- `POST /v1/messages` (main endpoint, full compression pipeline)
- `POST /v1/messages/batches` (passthrough, no compression)
- `GET /v1/messages/batches/*` (passthrough)
- All other paths: passthrough (no modification)

**Request body handling:**

```go
// Anthropic request structure (relevant fields only)
type AnthropicRequest struct {
    Model     string             `json:"model"`
    Messages  []AnthropicMessage `json:"messages"`
    System    json.RawMessage    `json:"system,omitempty"`    // string or []ContentBlock
    MaxTokens int                `json:"max_tokens"`
    Stream    bool               `json:"stream,omitempty"`
    // All other fields passed through unmodified
}

type AnthropicMessage struct {
    Role    string          `json:"role"`
    Content json.RawMessage `json:"content"` // string or []ContentBlock
}

// Content can be:
// - string (simple text message)
// - []ContentBlock where each block is:
//   - {type: "text", text: "..."}
//   - {type: "image", source: {type: "base64", ...}}
//   - {type: "tool_use", id: "...", name: "...", input: {...}}
//   - {type: "tool_result", tool_use_id: "...", content: "..." or [...]}
//
// The proxy must handle ALL of these formats correctly.
// Tool use blocks are the primary compression target.
// Image blocks are NEVER compressed (passed through as-is).
// Text blocks are compressed only in old messages.
```

### 14.2 OpenAI Chat Completions API

**Supported endpoints:**
- `POST /v1/chat/completions` (main endpoint, full compression pipeline)
- All other paths: passthrough

**Request body handling:**

```go
// OpenAI request structure (relevant fields only)
type OpenAIRequest struct {
    Model    string          `json:"model"`
    Messages []OpenAIMessage `json:"messages"`
    Stream   bool            `json:"stream,omitempty"`
    // All other fields passed through unmodified
}

type OpenAIMessage struct {
    Role       string          `json:"role"` // system, user, assistant, tool
    Content    json.RawMessage `json:"content"`
    ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
    ToolCallID string          `json:"tool_call_id,omitempty"`
    Name       string          `json:"name,omitempty"`
}
```

### 14.3 Header Passthrough

```go
// ALL request headers are passed through to upstream EXCEPT:
// - Host (rewritten to upstream host)
// - Content-Length (recalculated after compression)
// - Connection (handled by Go's HTTP client)
//
// Critical headers that MUST be forwarded:
// - x-api-key (Anthropic authentication)
// - Authorization (OpenAI authentication)
// - anthropic-version (API version)
// - anthropic-beta (beta features)
// - Content-Type (always application/json)
//
// ALL response headers from upstream are passed through to client unmodified.
```

### 14.4 Provider Invisibility Contract (ABSOLUTE REQUIREMENT)

The proxy MUST be architecturally undetectable by upstream providers.
This is a hard design constraint, not an optimization goal.

**Why this matters:**
- Anthropic has banned third-party CLIs (OpenClaw etc.) from using their subscription APIs
- OpenAI has similar ToS protections
- The proxy must NOT appear as an alternative client or API intermediary

**Why our architecture is inherently invisible:**

```
The proxy is a LOCAL transparent reverse proxy. The upstream provider sees
a request that is IDENTICAL to one that Claude Code / Codex would normally send.

What the provider CAN see:          What they ACTUALLY see:
─────────────────────────           ─────────────────────────
Source IP address                   User's real IP (proxy is localhost)
TLS fingerprint                     Go's standard crypto/tls (same as any Go HTTP client)
HTTP headers                        Exact headers from Claude Code (forwarded 1:1)
User-Agent                          "claude-code/X.Y.Z" (original, unchanged)
API key                             User's own subscription key (unchanged)
Request body format                 Valid Messages API JSON (unchanged format)
Message content                     Shorter history (identical to using /compact)
Request timing                      Normal timing (proxy adds <5ms)
```

**What the proxy is functionally equivalent to:**
A user who frequently uses /compact and writes concise messages. That's it.
There is no detectable difference between:
- Claude Code with /compact at message 15 -> sends 50K tokens
- Claude Code through TokenProxy at message 15 -> sends 50K tokens

Both produce valid, shorter conversation histories. The API cannot distinguish them.

**Implementation rules (MANDATORY, NO EXCEPTIONS):**

```go
// 1. NEVER add proxy-identifying headers
//    No X-Forwarded-For, no Via, no X-Proxy-*, no custom headers. Ever.

// 2. NEVER modify the User-Agent header
//    Forward exactly what Claude Code / Codex sends. Never append, never change.

// 3. NEVER modify request headers beyond the three standard exceptions
//    Only Host (must match upstream), Content-Length (body changed),
//    Connection (Go HTTP standard). Everything else: byte-identical passthrough.

// 4. NEVER add metadata to the message content
//    No "[compressed by TokenProxy]" markers, no version stamps, no watermarks.
//    The compressed content must read like natural conversation.

// 5. NEVER change the request URL path or query parameters
//    Forward /v1/messages as /v1/messages. No rewrites.

// 6. NEVER buffer full responses before forwarding
//    Stream SSE events immediately as received. No response-level modifications.

// 7. MiniMax summarization prompts must instruct the model to produce
//    natural-sounding summaries, not technical compression artifacts.
//    Example: "User asked to refactor auth" not "[COMPRESSED: msgs 1-5, topic: auth]"

// 8. The proxy MUST NOT generate any outbound traffic to the upstream providers
//    beyond what the CLI tool initiates. No health checks, no pings, no prefetches
//    to Anthropic/OpenAI. (MiniMax is a separate provider and exempt from this rule.)
```

**Header forwarding implementation:**

```go
func (p *Proxy) buildUpstreamRequest(original *http.Request, provider Provider, body []byte) *http.Request {
    upstream := p.upstreamClients[provider].BaseURL

    req, _ := http.NewRequestWithContext(
        original.Context(),
        original.Method,
        upstream + original.URL.Path,
        bytes.NewReader(body),
    )

    // Copy ALL headers from original request
    for key, values := range original.Header {
        for _, value := range values {
            req.Header.Add(key, value)
        }
    }

    // Only override the three necessary headers
    req.Header.Set("Host", req.URL.Host)
    req.Header.Set("Content-Length", strconv.Itoa(len(body)))
    req.Header.Del("Connection") // let Go's HTTP client handle this

    // NEVER add any other headers. The request must be a perfect clone.
    return req
}
```

---

## 15. Error Handling & Resilience

### 15.1 Error Categories and Responses

```go
// Category 1: Parse errors (malformed request)
// Response: 400 Bad Request with error details
// Action: Pass original request through unmodified as fallback

// Category 2: Upstream connection failure
// Response: 502 Bad Gateway
// Action: Retry once after 1s, then return error
// NEVER cache or compress during retry

// Category 3: Compression error (tree-sitter crash, regex panic, etc.)
// Response: None (transparent to client)
// Action: Skip the failing compression step, continue with remaining steps
// Log the error for debugging

// Category 4: MiniMax API error
// Response: None (transparent to client)
// Action: Skip Layer 2 entirely, use Layer 1 only
// Log warning, increment error counter

// Category 5: Streaming error (connection dropped mid-stream)
// Response: Close connection to client
// Action: Log partial response metrics, clean up resources

// FUNDAMENTAL RULE:
// The proxy must NEVER cause a request to fail that would have succeeded
// without the proxy. If in doubt, pass through unmodified.
```

### 15.2 Panic Recovery

```go
// Every goroutine that processes external input is wrapped in a recover:
func safeGo(fn func()) {
    go func() {
        defer func() {
            if r := recover(); r != nil {
                slog.Error("panic recovered",
                    "error", r,
                    "stack", string(debug.Stack()),
                )
            }
        }()
        fn()
    }()
}

// The HTTP handler is additionally wrapped:
func (p *Proxy) recoverMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if rec := recover(); rec != nil {
                slog.Error("handler panic", "error", rec, "path", r.URL.Path)
                // Forward original request unmodified
                p.passthroughRequest(w, r)
            }
        }()
        next.ServeHTTP(w, r)
    })
}
```

### 15.3 Auto-Retry: Rate Limit (429)

When the upstream API returns HTTP 429 (Too Many Requests), the proxy automatically
retries instead of forwarding the error to the CLI tool.

```go
// Rate limit retry logic (in forwardAndStream):
//
// 1. Send request to upstream
// 2. If response status == 429:
//    a. Read Retry-After header (seconds to wait)
//    b. If no Retry-After: default to 5 seconds
//    c. Cap wait at 30 seconds maximum
//    d. Log: "Rate limited, retrying in {N}s"
//    e. Send TUI event: show "Rate limited - retrying..." indicator
//    f. time.Sleep(retryAfter)
//    g. Retry the SAME request (no re-compression, body unchanged)
//    h. Maximum 2 retries, then forward the 429 to the CLI
//
// This is standard HTTP behavior. The API explicitly provides Retry-After
// for this purpose. Claude Code itself does this internally.
// The upstream sees normal retry behavior, nothing unusual.

func (p *Proxy) forwardWithRetry(req *http.Request, body []byte, maxRetries int) (*http.Response, error) {
    for attempt := range maxRetries + 1 {
        resp, err := p.upstreamClients[detectProvider(req, body)].Do(req)
        if err != nil {
            return nil, err
        }

        if resp.StatusCode != http.StatusTooManyRequests || attempt == maxRetries {
            return resp, nil
        }

        resp.Body.Close()
        retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), 5*time.Second)
        if retryAfter > 30*time.Second {
            retryAfter = 30 * time.Second
        }

        slog.Info("rate limited, retrying",
            "attempt", attempt+1,
            "retry_after", retryAfter,
        )
        p.sendTUIEvent(RateLimitEvent{RetryAfter: retryAfter})
        time.Sleep(retryAfter)

        // Rebuild request (body reader was consumed)
        req.Body = io.NopCloser(bytes.NewReader(body))
    }
    return nil, fmt.Errorf("max retries exceeded")
}
```

### 15.4 Auto-Retry: Context Overflow with Aggressive Compression

When the upstream API returns an error indicating the context is too long,
the proxy automatically compresses more aggressively and retries.

```go
// Context overflow recovery:
//
// 1. Send request to upstream
// 2. If response contains context length error:
//    Anthropic: status 400 + error.type == "invalid_request_error" +
//               message contains "too many tokens" or "context length"
//    OpenAI:    status 400 + error.code == "context_length_exceeded"
// 3. Recovery strategy:
//    a. Reduce sliding window from configured size to 2 (keep only last 2 exchanges)
//    b. If Layer 2 was off: temporarily enable it for this request
//    c. If Layer 2 was on: force re-summarization with more aggressive target (10% instead of 20%)
//    d. Re-run compression pipeline with aggressive settings
//    e. Retry the request with the more compressed body
//    f. Maximum 1 recovery attempt (if still too long, forward the error)
//    g. Send TUI event: "Context overflow - compressed and retried"
//
// This is a MASSIVE quality-of-life improvement:
// Instead of: session dies with error, user must /compact manually and re-type
// Now:        proxy handles it transparently, user gets their response

func (p *Proxy) isContextOverflowError(provider Provider, resp *http.Response, body []byte) bool {
    if resp.StatusCode != http.StatusBadRequest {
        return false
    }
    switch provider {
    case Anthropic:
        errType := gjson.GetBytes(body, "error.type").String()
        errMsg := gjson.GetBytes(body, "error.message").String()
        return errType == "invalid_request_error" &&
            (strings.Contains(errMsg, "too many tokens") ||
             strings.Contains(errMsg, "context length"))
    case OpenAI:
        code := gjson.GetBytes(body, "error.code").String()
        return code == "context_length_exceeded"
    }
    return false
}
```

### 15.5 API Health Monitoring (Live in TUI)

Background goroutine monitors upstream API availability and shows status in TUI.

```go
// Health monitor runs as a background goroutine.
// IMPORTANT: It does NOT ping the upstream LLM APIs directly.
// (See Section 14.4 - no outbound traffic to providers beyond what CLI initiates.)
//
// Instead, it tracks health from ACTUAL request results:
// - Last successful request timestamp per provider
// - Last error per provider
// - Running error rate (errors / requests in last 5 minutes)
//
// TUI indicators:
// - Provider idle (no requests in 5+ min): gray dot
// - Provider healthy (last request succeeded): green dot
// - Provider degraded (>20% error rate): yellow dot
// - Provider down (last 3 requests all failed): red dot
//
// MiniMax health is tracked separately (Layer 2 can degrade independently).

type HealthMonitor struct {
    mu sync.RWMutex
    providers map[Provider]*ProviderHealth
    minimax   *ProviderHealth
}

type ProviderHealth struct {
    LastSuccess   time.Time
    LastError     time.Time
    LastErrorMsg  string
    RecentResults *RingBuffer[bool]  // last 20 results (true=success, false=error)
    Status        HealthStatus       // idle, healthy, degraded, down
}

type HealthStatus int

const (
    HealthIdle     HealthStatus = iota  // no recent requests
    HealthHealthy                        // working normally
    HealthDegraded                       // some errors
    HealthDown                           // all recent requests failed
)
```

### 15.6 Request Logging (Session History)

Every request/response pair is logged to disk for debugging and history review.

```go
// Log location: ~/.tokenproxy/sessions/YYYY-MM-DD_HH-MM-SS.jsonl
// One JSONL file per proxy session (from start to quit).
// Each line is one request/response pair.
//
// Logged fields (per request):
// - Timestamp
// - Provider + model
// - Input tokens (original + compressed)
// - Output tokens
// - Compression ratio
// - Which layers applied
// - Proxy latency (overhead only)
// - Upstream latency (TTFT + total)
// - HTTP status code
// - Whether retry was triggered (429 or context overflow)
// - Errors (if any)
//
// NOT logged (privacy):
// - Message content (never logged)
// - API keys (never logged)
// - Full request/response bodies (never logged)
//
// CLI command to review:
// tokenproxy sessions list          # list session files
// tokenproxy sessions show latest   # print stats for latest session
// tokenproxy sessions export latest --format markdown > session.md

type SessionLogEntry struct {
    Timestamp       time.Time       `json:"ts"`
    Provider        string          `json:"provider"`
    Model           string          `json:"model"`
    InputOriginal   int             `json:"input_orig"`
    InputCompressed int             `json:"input_comp"`
    OutputTokens    int             `json:"output"`
    Ratio           float64         `json:"ratio"`
    Layers          []int           `json:"layers"`
    ProxyLatencyMs  float64         `json:"proxy_ms"`
    UpstreamTTFTMs  float64         `json:"ttft_ms"`
    UpstreamTotalMs float64         `json:"total_ms"`
    StatusCode      int             `json:"status"`
    Retried         bool            `json:"retried,omitempty"`
    RetryReason     string          `json:"retry_reason,omitempty"`
    Error           string          `json:"error,omitempty"`
}
```

### 15.7 Latency Tracking (Per Provider, Visible in TUI)

```go
// Track upstream latency metrics per provider, shown in TUI stats view.
//
// Metrics:
// - TTFT (Time to First Token): time from request sent to first SSE data event
// - Total response time: time from request sent to final SSE [DONE] event
// - Proxy overhead: time spent in compression pipeline (should be <5ms)
//
// These are derived from actual requests - no additional API calls.
//
// TUI display (in stats view):
//
// Latency (last 20 requests)
// ┌──────────────┬────────┬────────┬────────┐
// │ Provider     │ TTFT   │ Total  │ Proxy  │
// ├──────────────┼────────┼────────┼────────┤
// │ Claude Opus  │ 1.8s   │ 12.4s  │ 0.4ms  │
// │ Codex o3     │ 3.1s   │ 8.7s   │ 0.3ms  │
// │ MiniMax M2.7 │ 2.0s   │ 5.3s   │ n/a    │
// └──────────────┴────────┴────────┴────────┘

type LatencyTracker struct {
    mu       sync.RWMutex
    perModel map[string]*ModelLatency  // "anthropic:claude-opus-4-6" -> latency stats
}

type ModelLatency struct {
    RecentTTFT    *RingBuffer[time.Duration]  // last 20
    RecentTotal   *RingBuffer[time.Duration]  // last 20
    RecentProxy   *RingBuffer[time.Duration]  // last 20
}

func (lt *LatencyTracker) AvgTTFT(modelKey string) time.Duration {
    lt.mu.RLock()
    defer lt.mu.RUnlock()
    ml, ok := lt.perModel[modelKey]
    if !ok { return 0 }
    entries := ml.RecentTTFT.Last(20)
    if len(entries) == 0 { return 0 }
    var total time.Duration
    for _, d := range entries { total += d }
    return total / time.Duration(len(entries))
}
```

### 15.8 Health Check Endpoint

```go
// GET /health returns proxy status
// Used by: process managers, monitoring, doctor command
//
// Response:
// {
//   "status": "healthy",          // healthy, degraded, unhealthy
//   "uptime_seconds": 3600,
//   "version": "1.0.0",
//   "layers": {
//     "layer1": "active",
//     "layer2": "active",         // or "degraded" if MiniMax unreachable
//     "layer3": "active"
//   },
//   "upstream": {
//     "anthropic": "reachable",   // or "unreachable"
//     "openai": "reachable"
//   },
//   "minimax": "reachable",
//   "cache_entries": 42,
//   "compression_queue_depth": 0
// }
```

---

## 16. Testing Strategy

### 16.1 Unit Tests

```
Layer 1 compression:
- JSON minification: valid/invalid JSON, nested objects, arrays, edge cases
- Comment stripping: per language, string literal preservation, multiline
- Deduplication: exact match, near-duplicate, no-match, edge similarities
- Tree-sitter: per language, parse failures, minimum size threshold
- Delta encoding: identical files, small diffs, large diffs, new files
- Prompt cache: breakpoint placement, minimum size, multiple breakpoints

Layer 2:
- Anchor detection: per anchor type, false positives, edge cases
- Summary validation: pass/fail scenarios, boundary ratios
- MiniMax client: mock HTTP responses, timeouts, retries, errors

Layer 3:
- Response cache: hit/miss, TTL expiry, LRU eviction, invalidation
- Usage tracker: extra messages calculation, TTFT estimation, per-provider breakdown
- File watcher: change detection, debounce, directory limits

Resilience:
- 429 retry: correct Retry-After parsing, max retry cap, exponential backoff
- Context overflow: error detection per provider, aggressive recompression, single retry
- Health monitor: status transitions (idle->healthy->degraded->down), recent results tracking
- Latency tracker: TTFT measurement accuracy, rolling average, per-model breakdown
- Session logger: JSONL format correctness, no sensitive data leaks, file rotation

Invisibility:
- Header forwarding: ALL original headers preserved, no proxy headers added
- User-Agent: never modified, byte-identical passthrough
- Request body: valid API format after compression (parseable by upstream)

Layer 4:
- Goroutine lifecycle: start, graceful shutdown, timeout
- Channel operations: full queue handling, backpressure
- Race condition testing: -race flag on all tests
```

### 16.2 Integration Tests

```
End-to-end request flow:
- Anthropic format: full request -> compression -> upstream mock -> response stream
- OpenAI format: same flow
- Mixed session: alternating providers
- Large conversation: 50+ messages, verify compression ratios
- Streaming: SSE relay correctness, no data loss, no extra latency
- Error recovery: upstream failure mid-stream, MiniMax timeout

Resilience:
- 429 retry: mock 429 response -> verify retry -> verify eventual success
- Context overflow: mock 400 context error -> verify aggressive recompression -> retry
- Chained failure: 429 then context overflow in same session -> verify both handled
- Header preservation: capture all headers sent to upstream mock, verify 1:1 match
  with original (except Host/Content-Length/Connection)
```

### 16.3 Benchmark Tests

```go
// Benchmark targets (per operation):
//
// BenchmarkJSONMinification          <100us for 10KB JSON
// BenchmarkCommentStripping          <200us for 10KB code
// BenchmarkHashDeduplication         <50us per content block
// BenchmarkTreeSitterParse           <5ms for 1000-line file
// BenchmarkDeltaEncoding             <1ms for 10KB diff
// BenchmarkFullLayer1Pipeline        <2ms for typical request (100KB)
// BenchmarkMessageNormalization      <500us for 50-message conversation
// BenchmarkProviderDetection         <10us
// BenchmarkSecretDetection           <1ms for 100KB content
// BenchmarkTokenCounting             <500us for 10KB text
//
// Total Layer 1 overhead target: <5ms per request
```

---

## 17. Performance Targets

| Metric | Target | Notes |
|---|---|---|
| Layer 1 latency | <5ms | Synchronous, per-request |
| Proxy overhead (total) | <10ms | Excluding Layer 2 (async) |
| Memory usage (idle) | <50MB | No active caches |
| Memory usage (active) | <200MB | Full caches, active session |
| Startup time | <500ms | Including config load, tree-sitter init |
| SSE relay latency | <1ms per event | Zero-copy passthrough target |
| Response cache lookup | <100us | In-memory hash map |
| Token counting | <500us per 10KB | Using tiktoken-go |
| Concurrent requests | 10+ | Limited by upstream, not proxy |
| Binary size | <30MB | Static compilation with tree-sitter |

---

## 18. Dependency Inventory

### Required (stdlib + minimal external)

| Dependency | Purpose | Justification |
|---|---|---|
| `net/http` | HTTP server + client | stdlib |
| `encoding/json` | JSON parse/serialize | stdlib |
| `crypto/sha256` | Content hashing | stdlib |
| `bufio` | SSE stream scanning | stdlib |
| `regexp` | Pattern matching | stdlib |
| `sync`, `sync/atomic` | Concurrency primitives | stdlib |
| `log/slog` | Structured logging | stdlib (Go 1.21+) |
| `github.com/smacker/go-tree-sitter` | Code structure extraction | Only mature Go tree-sitter binding |
| `github.com/fsnotify/fsnotify` | File change detection | De facto standard, 7k+ stars |
| `github.com/pkoukk/tiktoken-go` | Token counting | Most accurate Go tiktoken port |
| `github.com/BurntSushi/toml` | Config file parsing | De facto standard for TOML in Go |
| `github.com/tidwall/gjson` | Fast JSON path queries | Zero-allocation JSON reading |
| `golang.org/x/time/rate` | Rate limiting | Official extended stdlib |

### Required (TUI - core user interface)

| Dependency | Purpose | Justification |
|---|---|---|
| `github.com/charmbracelet/bubbletea` | Terminal UI framework (Elm architecture) | Best-in-class Go TUI, the TUI IS the app |
| `github.com/charmbracelet/lipgloss` | Terminal styling (borders, colors, layout) | Companion to bubbletea, no alternative |
| `github.com/charmbracelet/bubbles` | Pre-built TUI components (progress bar, table, viewport) | Avoids reimplementing standard widgets |

### Tree-sitter language grammars

| Grammar | Dependency |
|---|---|
| Go | `github.com/smacker/go-tree-sitter/golang` |
| TypeScript | `github.com/smacker/go-tree-sitter/typescript/typescript` |
| JavaScript | `github.com/smacker/go-tree-sitter/javascript` |
| Rust | `github.com/smacker/go-tree-sitter/rust` |
| Python | `github.com/smacker/go-tree-sitter/python` |

**Total external dependencies: 11 required + 5 grammars = 16**

---

## 19. Project Structure

```
TokenProxy/
  cmd/
    tokenproxy/
      main.go                  # Entrypoint: subcommand dispatch (TUI default, utilities)
  internal/
    proxy/
      proxy.go                 # Proxy struct, lifecycle (Start/Shutdown), toggle methods
      handler.go               # HTTP request handler (hot path)
      streaming.go             # SSE relay implementation
      provider.go              # Provider detection + format normalization
    compression/
      layer1.go                # Layer 1 orchestrator
      json_compact.go          # JSON minification
      comment_strip.go         # Language-aware comment removal
      dedup.go                 # Hash-based deduplication
      dedup_minhash.go         # MinHash/LSH near-duplicate detection
      treesitter.go            # Tree-sitter code extraction
      treesitter_queries.go    # Tree-sitter query definitions per language
      delta.go                 # Delta encoding for file revisions
      prompt_cache.go          # Anthropic prompt cache optimization
    summarization/
      layer2.go                # Layer 2 orchestrator
      minimax.go               # MiniMax M2.7 API client
      anchor.go                # Anchor point detection
      validator.go             # Summary quality validation
      cache.go                 # Summary cache management
      progressive.go           # Progressive compression tiers
    caching/
      response_cache.go        # Response cache (Layer 3)
      file_watcher.go          # fsnotify-based cache invalidation
    tokens/
      counter.go               # tiktoken-based token counting
      usage.go                 # Usage tracking (messages, savings, extra capacity estimation)
    resilience/
      retry.go                 # Auto-retry for 429 + context overflow
      health.go                # API health monitoring (from actual request results)
      latency.go               # Per-provider latency tracking
    sessions/
      logger.go                # Session request/response logging (JSONL)
      export.go                # Session export (markdown, stats summary)
    security/
      secrets.go               # Secret detection + redaction
      patterns.go              # Built-in secret patterns
    analytics/
      collector.go             # Analytics event aggregation
      persistence.go           # Disk persistence (JSONL)
    tui/
      model.go                 # BubbleTea model (state, Init, Update, View)
      views.go                 # View renderers (main, stats, debug)
      styles.go                # Lipgloss style definitions (colors, borders, badges)
      components.go            # Reusable TUI components (progress bar, status line, table)
      keys.go                  # Keybinding definitions + help text
    config/
      config.go                # Config struct + loading (TOML + env + flags)
      defaults.go              # Default configuration values
    types/
      message.go               # Core message types
      provider.go              # Provider type + helpers
      events.go                # Event types for analytics
    util/
      ringbuffer.go            # Generic ring buffer
      safego.go                # Panic-recovering goroutine launcher
  docs/
    context.md                 # Active development worklog
    documentation.md           # Full technical documentation
    changelog.md               # Version history
    map.md                     # File index + architecture map
  scripts/
  go.mod
  go.sum
  LICENSE
```

---

## 20. Build & Distribution

### Build

```bash
# Development build
go build -o tokenproxy ./cmd/tokenproxy

# Release build (optimized, stripped)
CGO_ENABLED=1 go build -ldflags="-s -w -X main.version=1.0.0" -o tokenproxy ./cmd/tokenproxy

# CGO_ENABLED=1 is required for tree-sitter (C bindings)
# This means cross-compilation needs target C toolchain

# macOS universal binary (Apple Silicon + Intel)
GOOS=darwin GOARCH=arm64 go build -o tokenproxy-darwin-arm64 ./cmd/tokenproxy
GOOS=darwin GOARCH=amd64 go build -o tokenproxy-darwin-amd64 ./cmd/tokenproxy
lipo -create -output tokenproxy tokenproxy-darwin-arm64 tokenproxy-darwin-amd64
```

### Installation

```bash
# From source
go install github.com/user/tokenproxy@latest

# Or: download pre-built binary from GitHub releases
# Or: brew install tokenproxy (future)
```

### Shell Integration

```bash
# Add to ~/.zshrc or ~/.bashrc:

# Start TokenProxy on shell init (if not already running)
if ! pgrep -x tokenproxy > /dev/null; then
    tokenproxy start --daemon
fi

# Route all LLM CLI tools through the proxy
export ANTHROPIC_BASE_URL=http://127.0.0.1:8990
export OPENAI_BASE_URL=http://127.0.0.1:8990
```

---

## 21. Rollout Plan

### Phase 1: Core Proxy + Layer 1 (Week 1)

Deliverables:
- HTTP reverse proxy with provider detection
- SSE streaming relay
- JSON minification
- Comment stripping (Go, TS, JS, Python, Rust)
- Hash-based deduplication
- Basic token counting
- Config system (TOML + env + flags)
- CLI: start, stop, status
- Unit tests for all Layer 1 components
- Integration test: full request/response cycle

Milestone: proxy works transparently with Claude Code and Codex.
Expected savings: 30-50% on long sessions.

### Phase 2: Layer 2 + Analytics (Week 2)

Deliverables:
- MiniMax M2.7 integration
- Anchor point detection
- Async pre-compression pipeline
- Summary caching + validation
- Progressive compression tiers
- Analytics collector
- Terminal dashboard (basic)
- Persistent analytics (JSONL)
- Secret detection + redaction
- CLI: stats, cache commands

Milestone: full compression pipeline with real-time visibility.
Expected savings: 55-75% on long sessions.

### Phase 3: Polish + Advanced Features (Week 3)

Deliverables:
- Tree-sitter code extraction (all 5 languages)
- Delta encoding for file revisions
- Prompt cache optimization (Anthropic)
- Near-duplicate detection (MinHash/LSH)
- File watcher (fsnotify)
- Usage tracker with extra-messages estimation
- Response caching
- Terminal dashboard (full)
- CLI: doctor, test commands
- Benchmark tests
- Comprehensive integration tests

Milestone: production-ready, all layers active, full observability.
Expected savings: 60-80% on long sessions.

---

## Appendix A: Token Savings Breakdown (Expected)

| Technique | Savings (standalone) | Savings (combined) | Layer |
|---|---|---|---|
| JSON minification | 10-25% | 10-15% | 1 |
| Comment stripping | 5-15% | 5-8% | 1 |
| Hash deduplication | 10-20% | 8-12% | 1 |
| Tree-sitter extraction | 15-30% | 10-15% | 1 |
| Delta encoding | 5-15% | 3-8% | 1 |
| MiniMax summarization | 40-60% | 25-35% | 2 |
| Prompt cache optimization | 50-90% cost | 20-40% cost | 3 |
| Response caching | 100% per hit | 5-10% overall | 3 |
| **Combined total** | - | **60-80%** | all |

Note: "combined" is lower than "standalone" because techniques overlap.
Deduplication removes content that tree-sitter would have compressed.
Summarization covers content that was already partially compressed by Layer 1.
The combined estimate accounts for this overlap conservatively.

## Appendix B: Usage Capacity Model (Subscription Plans)

### Proxy operating overhead

| Component | Cost/month | Resource Impact |
|---|---|---|
| MiniMax API calls | $0 | Included in existing Pro Plan (unlimited) |
| Compute (local Mac) | $0 | <200MB RAM, negligible CPU |
| Storage (analytics) | $0 | ~1MB/day JSONL |

### Effective capacity multiplier

At 67% average compression, your subscription rate limits effectively expand:

| Scenario | Messages/day (raw) | Messages/day (proxy) | Multiplier |
|---|---|---|---|
| Light use | ~30 before limit | ~90 before limit | **3x** |
| Medium use | ~80 before limit | ~240 before limit | **3x** |
| Heavy use | ~150 before limit | ~450 before limit | **3x** |

### Additional tangible benefits

| Benefit | Estimated Improvement |
|---|---|
| Time-to-First-Token | 30-40% faster (shorter prefill) |
| /compact frequency | 3x less often needed (context fills slower) |
| Session length | 3x more messages before context limit |
| Response quality | Measurably better on long sessions (less "Lost in the Middle") |

## Appendix C: Risk Assessment

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Summary loses critical context | Low | High | Anchor points + validation + conservative ratios |
| MiniMax API outage | Low | Low | Graceful degradation to Layer 1 only |
| Tree-sitter parse failure | Medium | Low | Per-file fallback to uncompressed |
| API format change (Anthropic/OpenAI) | Low | Medium | Version-pinned parsing, passthrough on parse error |
| Token count inaccuracy | Medium | Low | tiktoken approximation is within 5%, stats are estimates anyway |
| Secret detection false positive | Medium | Low | Allowlist, warn mode, easy to disable per-pattern |
| Proxy crash | Very Low | Medium | Panic recovery, daemon auto-restart, stateless design |

# TokenProxy - Technical Specification

Version: 2.0.0-draft
Date: 2026-04-10
Language: Go 1.24+
Architecture: Dual-Mode Token Optimization Engine (Pre-Entry Filtering + Post-Entry Compression Proxy)

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Problem Statement & Usage Economics](#2-problem-statement--economics)
3. [Architecture Overview](#3-architecture-overview)
4. [Layer 0: Pre-Entry Filtering (CLI Filter Engine)](#4-layer-0-pre-entry-filtering)
   - 4.1 Filter Subcommand
   - 4.2 Command Rewriting Engine
   - 4.3 Hook System (Claude Code, Codex — v1 scope)
   - 4.4 Built-in Filters (24 modules)
   - 4.5 TOML Filter DSL (Custom Filters)
   - 4.6 Filter Dispatch Priority
   - 4.7 Tee Recovery System
   - 4.8 Filter Tracking (SQLite)
   - 4.9 Permission & Trust Model
5. [Layer 1: Deterministic Compression Engine](#5-layer-1-deterministic-compression-engine)
   - 5.1-5.6 Original sub-layers (JSON, comments, dedup, structure, delta, prompt cache)
   - 5.7 ANSI & Progress Strip
   - 5.8 Tool-Result Classifier
   - 5.9 Tool-Output Compressor (RTK-style filters on old messages)
   - 5.10 Success Short-Circuit
   - 5.11 Image Base64 Replacement
   - 5.12 Repeated Tool Collapse
   - 5.13 Conversation Graph Pruning
   - 5.14 Pre-Filtered Content Tagging
6. [Layer 2: Intelligent Compression via MiniMax M2.7](#6-layer-2-intelligent-compression-via-minimax-m27)
   - 6.8 Adaptive Sliding Window
   - 6.9 Tool Result Priority Classification
7. [Layer 3: Caching & Optimization](#7-layer-3-caching--optimization)
8. [Layer 4: Go Concurrency Pipeline](#8-layer-4-go-concurrency-pipeline)
9. [Multi-Provider Support & OAuth Authentication](#9-multi-provider-support)
   - 9.3 OAuth Passthrough (no API keys required)
   - 9.4 Provider Compatibility Matrix
   - 9.5 Setup per CLI
   - 9.6 Pre-Flight Validation Test
10. [Secret Detection & Redaction](#10-secret-detection--redaction)
11. [Analytics & Observability (BubbleTea TUI)](#11-analytics--observability)
12. [Debug & Observability System (AI-Agent-Optimized)](#12-debug--observability-system)
13. [Configuration System](#13-configuration-system)
14. [Usage & Quick Setup](#14-usage--quick-setup)
15. [Data Structures & Types](#15-data-structures--types)
16. [API Compatibility Layer & Provider Invisibility](#16-api-compatibility-layer)
   - 16.4 Provider Invisibility Contract
17. [Error Handling & Resilience](#17-error-handling--resilience)
   - 17.3 Auto-Retry: Rate Limit (429)
   - 17.4 Auto-Retry: Context Overflow
   - 17.5 API Health Monitoring
   - 17.6 Request Logging
   - 17.7 Latency Tracking
18. [Testing Strategy](#18-testing-strategy)
19. [Performance Targets](#19-performance-targets)
20. [Dependency Inventory](#20-dependency-inventory)
21. [Project Structure](#21-project-structure)
22. [Build & Distribution](#22-build--distribution)
23. [Rollout Plan](#23-rollout-plan)
24. [Appendix A: Token Savings Breakdown](#appendix-a)
25. [Appendix B: Usage Capacity Model](#appendix-b)
26. [Appendix C: Risk Assessment](#appendix-c)
27. [Appendix D: Drawback Analysis](#appendix-d)
28. [Appendix E: Synergy Cascade Effects](#appendix-e)

---

## Document authority and normative decisions

- **`spec+.md` (this file)** is the **normative** v2 technical specification for implementation and reviews.
- **`spec.md`** is the **historical** v1.0-final snapshot (frozen). Use it only for design context; it must not override `spec+.md`.
- **Repository implementation order** for the current codebase is additionally constrained by **`handover.md` (repo root) §4** (dependency order: Layer 1 hardening → Layer 0 → Layer 2 extensions → advanced Layer 1 + debug). **`spec+.md` §23** is a **feature grouping / completeness checklist**, not a calendar; when the two differ on sequencing, **`handover.md` §4 is authoritative** (see also `docs/HANDOVER.md` → link).

**Locked technical choices (normative):**

| Topic | Decision | Rationale |
| --- | --- | --- |
| SQLite (Layer 0 tracking) | **`modernc.org/sqlite`** (pure Go) | No CGO; simpler CI and cross-compilation; ample for local tracking/logging workloads. |
| JSON in Go | **`encoding/json`** (stdlib) | Matches existing code style; no extra dependency; use `json.RawMessage` / small helpers where path-style access is needed. |

**Product choices (normative for v2.0):**

| Topic | Decision |
| --- | --- |
| Sliding window | **`sliding_window = 5` means five recent user-started exchanges** (see §13.1 and §5). |
| Layer 0 hooks (install targets) | **v1: Claude Code + OpenAI Codex only** (see §4.3). |

---

## 1. Executive Summary

TokenProxy is a dual-mode token optimization engine written in Go. It operates at two
distinct layers simultaneously:

**Mode 1 - Pre-Entry Filtering (Layer 0):** A CLI filter engine that intercepts shell
commands executed by LLM agents (via hook system), runs them, filters the output using
24 specialized command-aware filters, and returns compact results. This shrinks tool
output BEFORE it enters the conversation - affecting ALL messages including the sliding
window that the proxy never touches.

**Mode 2 - Post-Entry Compression (Layers 1-3):** A transparent HTTP reverse proxy that
sits between LLM CLI tools (Claude Code, OpenAI Codex) and their respective APIs. It
intercepts outgoing requests, applies multi-layered token compression to the conversation
history, and forwards the optimized request to the upstream API. Responses are streamed
through unmodified.

Both modes run from a single Go binary. The combination is multiplicative: Layer 0
shrinks input at entry time, Layers 1-3 compress the already-smaller history further.

### Core Value Proposition

- **85-90% token savings** on long coding sessions (combined Layer 0 + Layers 1-3)
- **Zero perceived latency** through async pre-compression during user idle time
- **Quality improvement** through noise reduction (less "Lost in the Middle" degradation)
- **2-4x faster Time-to-First-Token** as a side effect of shorter prompts
- **5-7x message capacity** before hitting subscription rate limits
- **Security layer** with automatic secret detection and redaction
- **Full observability** with real-time token tracking, cost analytics, and AI-agent-optimized debug system
- **Provider-agnostic** - works with Anthropic and OpenAI API formats
- **Single binary** - no Rust sidecar, no external dependencies, pure Go

### Design Principles

1. **Zero-downside guarantee**: If compression would degrade quality, skip it. Uncompressed
   passthrough is always the fallback. The proxy must NEVER make things worse.
2. **Additive-only transformation**: The proxy only REMOVES or REPLACES tokens. It never
   ADDS content to the conversation that the user/model did not produce.
3. **Transparency**: Every compression decision is logged. The user can inspect exactly
   what was changed and why.
4. **Graceful degradation**: If MiniMax is down, Layer 2 is skipped. If regex extraction
   fails on a file, that file passes through uncompressed. Every layer is independently optional.
5. **Complete provider invisibility**: The proxy MUST be architecturally undetectable by
   upstream providers. See Section 16.4 for the full invisibility contract.
6. **Multiplicative layering**: Each optimization layer amplifies the next. Layer 0
   output makes Layer 1 dedup/delta/cache more effective. Smaller messages mean
   better MiniMax summaries. Deterministic filtering stabilizes cache keys.

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

| Session length | Proxy only (L1-3) | Combined (L0+L1-3) | Combined multiplier |
|---|---|---|---|
| Short (5-10 messages) | 30-50% | 70-85% | ~3-4x capacity |
| Medium (15-25 messages) | 55-65% | 80-88% | ~5-7x capacity |
| Long (30+ messages) | 65-67% | 85-90% | ~7-10x capacity |

The combined savings are multiplicative because Layer 0 prevents tokens from ENTERING
the conversation. Every token RTK-style filtering removes at entry time is a token
that NEVER gets sent in any future request. Over a 30-message session, a token saved
at message 5 is saved 25 more times in subsequent requests.

Concrete impact:

| Metric | Without Proxy | Proxy Only (L1-3) | Combined (L0+L1-3) |
|---|---|---|---|
| Messages before rate limit | ~30 | ~70-90 | ~180-230+ |
| /compact frequency | every ~25 msgs | every ~75 msgs | rarely needed |
| TTFT at message 30 | ~4s | ~1.5s | ~0.6s |
| Session continuity | dies at context limit | auto-retry | auto-retry + pre-filtered |

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
                    +---------+---------+
                    |                   |
                    v                   v
          +------------------+   HTTP Request (Messages API)
          | Layer 0          |          |
          | Pre-Entry Filter |          v
          | (Hook System)    |   +-------------------+
          |                  |   |   TokenProxy       |
          | "git status"     |   |   HTTP Proxy       |
          |   -> tokenproxy  |   |   (localhost:8990) |
          |      filter      |   +-------------------+
          |   -> "3 mod,     |          |
          |      1 staged"   |   +------+------+------+
          +------------------+   |      |      |      |
                    |            v      v      v      |
                    |       +------+ +------+ +------+|
          Filtered output   | L1   | | L2   | | L3   ||
          enters the        | Det. | | Mini | | Cache||
          conversation      | Comp | | Max  | | Opt  ||
                    |       +------+ +------+ +------+|
                    |            |      |      |      |
                    v            +------+------+------+
          tool_result in                |
          message history        +------+------+
          (already compact)      | Layer 4     |
                                 | Concurrency |
                                 +------+------+
                                        |
                                        v
                              Upstream API (Anthropic / OpenAI)
                                        |
                                        | SSE Stream (unmodified)
                                        v
                              CLI Tool (response displayed)
```

### Two Optimization Points, One Binary

```
LAYER 0 (Pre-Entry) - Affects ALL messages including sliding window
=================================================================
1. LLM agent decides to run "git status"
2. Hook intercepts: calls "tokenproxy filter git status"
3. tokenproxy executes "git status", captures stdout/stderr
4. Built-in filter compacts: 40 lines -> "3 modified, 1 staged"
5. Compact output returned to agent -> becomes tool_result
6. This tool_result is ALREADY small when it enters the conversation
7. Effect: even messages INSIDE the sliding window are pre-filtered

LAYERS 1-3 (Post-Entry) - Affects old messages outside sliding window
=================================================================
1. LLM agent sends API request with full conversation history
2. Request arrives at localhost:8990
3. Layer 1: deterministic compression on old messages (<1ms)
   - **Normative order matches §3 “Request Flow (detailed)” steps 4a–4m:** ANSI strip first, then JSON compact or comment strip, then dedup (exact + near), structure extract, delta, then classifier/compressor extensions, success short-circuit, image/repeat/graph/prefilter (see §5).
   - NEW: tool-result classification + RTK-style filtering on old tool_results
   - NEW: success short-circuit, image replace, repeated collapse, graph pruning
4. Layer 2: MiniMax summary check (async, 0ms hot path)
5. Layer 3: response cache + prompt cache optimization
6. Compressed request forwarded to upstream
7. SSE response streamed back unmodified
```

### Why Both Layers Are Necessary (Not Redundant)

| Aspect | Layer 0 only | Layers 1-3 only | Both combined |
|---|---|---|---|
| Sliding window messages | Filtered (smaller) | Untouched (full size) | Filtered (smaller) |
| Old messages | Once-filtered | Multi-layer compressed | Both (multiplicative) |
| User/assistant text | Not affected | Summarized by MiniMax | Summarized |
| Session before hook install | Not affected | Fully compressed | Fully compressed |
| Savings (long session) | 48% (tool output only) | 67% (all message types) | 85-90% |

### Request Flow (detailed)

```
LAYER 0 PATH (Pre-Entry, happens BEFORE the API request):
0a. LLM agent invokes tool (e.g., Bash with "git status")
0b. Hook intercepts: calls "tokenproxy filter git status"
0c. tokenproxy spawns "git status" subprocess, captures stdout/stderr
0d. Filter dispatch: classify command -> select built-in filter or TOML match
0e. Apply filter pipeline: ANSI strip -> filter logic -> truncation
0f. Track: record input/output tokens in SQLite
0g. Return filtered output to agent (exit code preserved)
0h. Agent receives compact tool_result -> adds to conversation

LAYERS 1-3 PATH (Post-Entry, happens on each API request):
1. CLI sends HTTP POST to localhost:PORT
2. Proxy parses request body (Anthropic or OpenAI format detected)
3. Proxy extracts message array from request
4. Layer 1 runs synchronously (<5ms):
   a. ANSI escape code and progress bar stripping
   b. JSON minification of tool results
   c. Whitespace/comment stripping from code blocks (10 languages)
   d. Hash-based deduplication (SHA256 exact + MinHash/LSH near-duplicate)
   e. Regex-based code structure extraction for old code blocks
   f. Delta encoding for repeated file reads
   g. Tool-result classification (git/test/build/lint/json/code/log)
   h. Tool-output compression (RTK-style: stats extraction, failure focus, grouping)
   i. Success short-circuit ("0 errors" -> one-liner)
   j. Image base64 replacement (base64 -> text description)
   k. Repeated tool collapse (identical tool calls -> reference)
   l. Conversation graph pruning (redundant read-edit-read -> prune old read)
   m. Pre-filtered content tagging (skip redundant ops on Layer 0 output)
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

## 4. Layer 0: Pre-Entry Filtering (CLI Filter Engine)

Layer 0 operates BEFORE data enters the conversation. It intercepts CLI commands
executed by LLM agents, runs them, and filters the output through specialized
command-aware filters. This is the Go equivalent of RTK (Rust Token Killer),
fully integrated into the same binary as the HTTP proxy.

**Key difference from Layers 1-3:** Layer 0 affects ALL messages including the
sliding window. Layers 1-3 only touch old messages outside the window.

### 4.1 Filter Subcommand

```go
// tokenproxy filter <command> [args...]
//
// Executes the given command, captures stdout/stderr, applies the best matching
// filter, prints the filtered output, and exits with the command's exit code.
//
// Example:
//   tokenproxy filter git status
//   -> Executes "git status"
//   -> Applies git_status filter
//   -> Prints: "3 modified, 1 staged, 2 untracked"
//   -> Exits with git's exit code (0)
//
// If no filter matches: prints raw output unchanged (passthrough).
// If filter fails: prints raw output unchanged + stderr warning.
// Exit code is ALWAYS preserved from the underlying command.

func runFilter(args []string) {
    // 1. Join args into command string
    // 2. Classify command -> select filter
    // 3. Execute command via subprocess (sh -c on Unix, cmd /C on Windows)
    // 4. Capture stdout + stderr separately
    // 5. Apply filter to stdout
    // 6. If filter error: fallback to raw stdout
    // 7. Print filtered output to stdout
    // 8. Print stderr to stderr (unfiltered, always)
    // 9. If exit code != 0: tee raw output to recovery file
    // 10. Track: record input/output tokens in SQLite
    // 11. Exit with command's exit code
}
```

**Subprocess execution:**

```go
// Execute command with full shell interpretation (pipes, redirects, env vars).
// On Unix: sh -c "command args..."
// Captures stdout and stderr separately.
// Timeout: 120 seconds (configurable).
// Working directory: inherited from caller (the LLM agent's cwd).

func executeCommand(cmd string) (stdout, stderr []byte, exitCode int, err error) {
    ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
    defer cancel()

    proc := exec.CommandContext(ctx, "sh", "-c", cmd)
    var outBuf, errBuf bytes.Buffer
    proc.Stdout = &outBuf
    proc.Stderr = &errBuf
    proc.Dir = "" // inherit cwd

    err = proc.Run()
    exitCode = proc.ProcessState.ExitCode()
    return outBuf.Bytes(), errBuf.Bytes(), exitCode, err
}
```

### 4.2 Command Rewriting Engine

The rewriting engine translates raw shell commands into their tokenproxy-filtered
equivalents. It handles compound commands, pipes, redirects, and quoting correctly.

```go
// rewriteCommand rewrites a raw shell command for RTK-style filtering.
//
// Input:  "cargo fmt --all && cargo test 2>&1 | tail -20"
// Output: "tokenproxy filter cargo fmt --all && tokenproxy filter cargo test 2>&1 | tail -20"
//
// Rules:
// - Split compound commands on && || ;
// - Rewrite each segment independently
// - NEVER rewrite the right side of a pipe (it processes output, not a command)
// - Exception: find/fd before pipe IS rewritten (find produces file lists)
// - Preserve redirects (2>&1, >output.txt) attached to the correct segment
// - If command is already "tokenproxy filter ...": pass through unchanged
// - If TOKENPROXY_DISABLED=1 prefix: pass through unchanged
// - If command has no filter match: pass through unchanged (exit code 1 to hook)

func rewriteCommand(cmd string, excluded []string) (rewritten string, hasFilter bool)
```

**Shell tokenizer:**

```go
// tokenize splits a shell command into typed tokens, correctly handling:
// - Single-quoted strings ('don't split this')
// - Double-quoted strings ("or $this")
// - Escape sequences (\", \\, \$)
// - Operators: && || ; | & (as distinct from arguments)
// - Redirects: > >> 2>&1 &> &>> < <<
// - Heredocs: << and <<< (returned as single token)
// - Arithmetic expansion: $(( )) (returned as single token)
// - Variable expansion: $VAR ${VAR} (flagged but not expanded)
// - Globs: * ? (flagged as shellism)
//
// Returns tokens with byte offsets for accurate re-assembly of rewritten command.

type TokenKind int
const (
    TokenArg      TokenKind = iota  // regular argument
    TokenOperator                    // && || ;
    TokenPipe                        // |
    TokenRedirect                    // > >> 2>&1 etc.
    TokenShellism                    // * ? $var backticks
)

type ParsedToken struct {
    Kind   TokenKind
    Value  string
    Offset int  // byte offset in original string
}

func tokenize(input string) []ParsedToken
```

**Rewrite rules (60+ patterns, compiled into RegexSet for O(n) matching):**

```go
type RewriteRule struct {
    Pattern        string    // regex matching the command
    FilterCmd      string    // tokenproxy filter prefix
    Category       string    // "Git", "Build", "Test", "Files", etc.
    EstSavingsPct  float64   // expected savings percentage
    RewritePrefixes []string // command prefixes that trigger this rule
}

// Example rules:
var rules = []RewriteRule{
    {Pattern: `^git\s+(status|log|diff|show|add|commit|push|pull|branch|fetch|stash)`,
     FilterCmd: "tokenproxy filter git", Category: "Git", EstSavingsPct: 80.0},
    {Pattern: `^cargo\s+(build|test|clippy|check|fmt)`,
     FilterCmd: "tokenproxy filter cargo", Category: "Build", EstSavingsPct: 85.0},
    {Pattern: `^(cat|head|tail)\s+`,
     FilterCmd: "tokenproxy filter read", Category: "Files", EstSavingsPct: 60.0},
    {Pattern: `^(rg|grep)\s+`,
     FilterCmd: "tokenproxy filter grep", Category: "Files", EstSavingsPct: 75.0},
    {Pattern: `^go\s+(test|build|vet)`,
     FilterCmd: "tokenproxy filter go", Category: "Build", EstSavingsPct: 85.0},
    {Pattern: `^(pytest|python -m pytest)`,
     FilterCmd: "tokenproxy filter pytest", Category: "Test", EstSavingsPct: 90.0},
    // ... 60+ rules total, covering all ecosystems
}
```

### 4.3 Hook System

The hook system installs shell hooks into LLM CLI tools that auto-rewrite commands
through the filter engine. This is the entry point for Layer 0.

**v1.0 scope (normative):** first-class install targets are **Claude Code** and **OpenAI Codex** only. Other agents below are **non-goals for v1** (may be documented for parity with RTK or future work—they are not required for v2.0.0 deliverables).

```bash
# Installation (v1):
tokenproxy hook install claude     # Install hook for Claude Code
tokenproxy hook install codex      # Install hook for Codex

# Verification:
tokenproxy hook verify             # Check all installed hooks (SHA-256 integrity)
tokenproxy hook status             # Show which hooks are installed and active

# Removal:
tokenproxy hook remove claude      # Remove Claude Code hook
```

**Claude Code hook installation:**

```go
// tokenproxy hook install claude
//
// 1. Create ~/.claude/hooks/ directory
// 2. Write rewrite hook script to ~/.claude/hooks/tokenproxy-rewrite.sh
// 3. Compute SHA-256 hash of hook script
// 4. Patch ~/.claude/settings.json: add PreToolUse hook configuration
//    {
//      "hooks": {
//        "PreToolUse": [{
//          "matcher": "Bash",
//          "hooks": [{
//            "type": "command",
//            "command": "bash ~/.claude/hooks/tokenproxy-rewrite.sh"
//          }]
//        }]
//      }
//    }
// 5. Verify hook is registered: tokenproxy hook verify

// Hook script (tokenproxy-rewrite.sh):
// Reads the tool input JSON from Claude Code's hook system,
// extracts the command, calls "tokenproxy rewrite <cmd>",
// and based on exit code:
//   0 = rewrite allowed, stdout contains rewritten command
//   1 = no filter match, passthrough unchanged
//   2 = deny rule matched, block command
//   3 = ask rule matched, rewrite but let Claude Code prompt user
```

**Codex hook installation:**

```go
// tokenproxy hook install codex
//
// Codex supports base URL override but not PreToolUse hooks directly.
// Instead, we write an AGENTS.md file that instructs the agent to prefix
// shell commands with "tokenproxy filter":
//
// 1. Create ~/.codex/AGENTS.md (or append to existing)
// 2. Add instruction block:
//    "When executing shell commands, prefix with 'tokenproxy filter' for
//     optimized output. Example: tokenproxy filter git status"
// 3. This is a suggestion-based approach (~70-85% adoption rate)
//
// For full adoption, Codex would need a hook system similar to Claude Code.
```

**v1 install targets (normative):**

| Agent | Hook Type | Adoption Rate | Installation |
|---|---|---|---|
| Claude Code | PreToolUse shell hook | 100% (auto-rewrite) | `tokenproxy hook install claude` |
| Codex | AGENTS.md instruction | ~70-85% (suggestion) | `tokenproxy hook install codex` |

**Future / additional agents (non-normative for v1; RTK-style parity roadmap):**

| Agent | Hook Type | Adoption Rate | Installation |
|---|---|---|---|
| Cursor | preToolUse hook | 100% (auto-rewrite) | `tokenproxy hook install cursor` (future) |
| GitHub Copilot (VS Code) | PreToolUse hook | 100% (auto-rewrite) | `tokenproxy hook install copilot` (future) |
| GitHub Copilot CLI | deny-with-suggestion | ~70% (suggestion) | same as copilot |
| Gemini CLI | BeforeTool hook | 100% (auto-rewrite) | `tokenproxy hook install gemini` |
| Windsurf | .windsurfrules instruction | ~70% (suggestion) | `tokenproxy hook install windsurf` |
| Cline/Roo Code | .clinerules instruction | ~70% (suggestion) | `tokenproxy hook install cline` |
| OpenCode | Plugin (tool.execute.before) | 100% (auto-rewrite) | `tokenproxy hook install opencode` |
| OpenClaw | Plugin (before_tool_call) | 100% (auto-rewrite) | `tokenproxy hook install openclaw` |

### 4.4 Built-in Filters (24 Modules)

Each filter is a pure Go function that takes raw command output and returns
filtered output. All filters follow the same contract:

```go
// FilterFunc takes raw stdout and returns filtered output.
// On error, return the raw input unchanged (passthrough fallback).
// The filter MUST NOT modify stderr (always passed through separately).
type FilterFunc func(stdout string, args []string, verbose int) (string, error)
```

**Complete filter inventory:**

| # | Filter | Commands | Strategy | Savings |
|---|---|---|---|---|
| F01 | git_status | git status | Stats extraction: "M modified, S staged, U untracked" | 85-95% |
| F02 | git_log | git log | Condensed: hash + one-line message + file stats | 80-95% |
| F03 | git_diff | git diff | Stats + compacted hunks (context lines preserved for failures) | 70-90% |
| F04 | git_show | git show | Commit header + stat + compacted diff | 80% |
| F05 | git_write | git add/commit/push/pull/branch/fetch/stash | One-line confirmation with key info | 90%+ |
| F06 | file_read | cat/head/tail | Language-aware comment strip (10 langs), optional signature-only | 30-74% |
| F07 | build_output | go build, cargo build, tsc, npm build, next build | Errors only, short-circuit on success ("Build ok") | 80-99% |
| F08 | test_output | go test, cargo test, pytest, vitest, playwright, rspec | Failures only + traces, count summary, state machine | 90-99% |
| F09 | lint_output | eslint, clippy, golangci-lint, ruff, rubocop, biome | Group by rule, count violations, show first N | 80-90% |
| F10 | search_results | grep, rg, find, fd | Group by file, limit per file, tree for find | 70-80% |
| F11 | dir_listing | ls, tree | Tree compression with file counts per directory | 65-80% |
| F12 | pkg_manager | npm, pnpm, pip, go mod, cargo install | Compact list, strip progress bars/spinners | 70-85% |
| F13 | container | docker ps/images/logs, kubectl get/logs | Essential fields only, truncate logs | 60-80% |
| F14 | json_output | curl responses, API output, jq | Schema extraction (keys + types, strip large values) | 80-95% |
| F15 | log_dedup | docker logs, app logs, journalctl | Dedup with occurrence counts, pattern detection | 70-85% |
| F16 | aws_cli | aws sts/s3/ec2/lambda/rds/cloudformation | Compact JSON, strip metadata, redact secrets | 60-80% |
| F17 | ansi_strip | (applied to all commands as pre-processing) | Strip ANSI escapes + progress bars + \\r overwrites | 5-15% |
| F18 | gh_cli | gh pr/issue/run/repo | Strip ASCII art, compact metadata, essential fields | 80-87% |
| F19 | psql_output | psql | Strip table borders, compact column output | 60-75% |
| F20 | dotnet_output | dotnet build/test/restore | Errors only, TRX/binlog parsing for test results | 70-85% |
| F21 | ruby_output | rake, rspec, rubocop | Failure focus, JSON mode (rspec --format json), cop grouping | 60-90% |
| F22 | go_test | go test -json | NDJSON stream parsing, interleaved package events, failures only | 90%+ |
| F23 | python_typecheck | mypy | Type error grouping by file and error code | 80% |
| F24 | format_output | prettier, gofmt, cargo fmt, black | Strip file list, show summary ("N files formatted") | 85-95% |

**Filter implementation examples:**

```go
// F01: git_status - Stats extraction
func filterGitStatus(stdout string, args []string, verbose int) (string, error) {
    lines := strings.Split(stdout, "\n")
    var modified, staged, untracked, deleted int
    var branch string

    for _, line := range lines {
        trimmed := strings.TrimSpace(line)
        switch {
        case strings.HasPrefix(trimmed, "On branch "):
            branch = strings.TrimPrefix(trimmed, "On branch ")
        case strings.HasPrefix(trimmed, "M ") || strings.HasPrefix(trimmed, " M"):
            modified++
        case strings.HasPrefix(trimmed, "A ") || strings.HasPrefix(trimmed, "MM"):
            staged++
        case strings.HasPrefix(trimmed, "??"):
            untracked++
        case strings.HasPrefix(trimmed, "D ") || strings.HasPrefix(trimmed, " D"):
            deleted++
        }
    }

    var parts []string
    if branch != "" { parts = append(parts, "branch: "+branch) }
    if staged > 0 { parts = append(parts, fmt.Sprintf("%d staged", staged)) }
    if modified > 0 { parts = append(parts, fmt.Sprintf("%d modified", modified)) }
    if deleted > 0 { parts = append(parts, fmt.Sprintf("%d deleted", deleted)) }
    if untracked > 0 { parts = append(parts, fmt.Sprintf("%d untracked", untracked)) }

    if len(parts) == 0 {
        return "clean", nil
    }
    return strings.Join(parts, ", "), nil
}

// F08: test_output - Failure focus with state machine
// Parses test output from multiple ecosystems (go test, cargo test, pytest, vitest).
// Shows: summary line + full details of FAILED tests only.
// Passed tests reduced to count.
//
// Example output:
//   42 passed, 2 failed
//   FAIL TestAuth/expired_token (auth_test.go:47)
//     expected: 401
//     got:      200
//   FAIL TestDB/connection_pool (db_test.go:123)
//     panic: runtime error: index out of range

// F10: search_results - Group by file
// Input: 200 lines of grep output
// Output: grouped by file with per-file limit
//
// src/main.go (3 matches)
//   12: func main() {
//   45: func handleRequest(
//   89: func shutdown(
// src/config/config.go (2 matches)
//   8: type Config struct {
//   34: func Load() (*Config, error) {
// ... +5 files (12 more matches)
```

### 4.5 TOML Filter DSL (Custom Filters)

Users can define custom filters in TOML without writing Go code.
This covers commands that the built-in filters do not handle.

```toml
# .tokenproxy/filters.toml (project-local, committed with repo)
# or ~/.tokenproxy/filters.toml (user-global)

schema_version = 1

[filters.my-build-tool]
description = "Compact my-build-tool output"
match_command = "^my-build-tool\\s+build"
strip_ansi = true
replace = [
    { pattern = "^\\[info\\].*$", replacement = "" },
    { pattern = "^Compiling (\\d+) files", replacement = "Compiling $1 files..." }
]
match_output = [
    { pattern = "BUILD SUCCESS", message = "[ok] Build succeeded" },
    { pattern = "BUILD FAILURE", message = "[FAIL] Build failed", unless = "" }
]
strip_lines_matching = ["^\\s*$", "^Downloading ", "^\\[debug\\]"]
keep_lines_matching = []
truncate_lines_at = 200
head_lines = 0
tail_lines = 0
max_lines = 50
on_empty = "[ok] my-build-tool: no output"
```

**8-stage TOML filter pipeline (applied in order):**

| Stage | Field | Description |
|---|---|---|
| 1 | `strip_ansi` | Remove ANSI escape codes from entire output |
| 2 | `replace` | Regex substitutions, line-by-line, chainable (rule N+1 on output of N) |
| 3 | `match_output` | Short-circuit: if pattern matches full blob, return message immediately. `unless` skips if also matches. |
| 4 | `strip_lines_matching` / `keep_lines_matching` | Filter lines by regex (strip = remove matching, keep = remove non-matching) |
| 5 | `truncate_lines_at` | Truncate each line to N characters |
| 6 | `head_lines` / `tail_lines` | Keep first/last N lines only |
| 7 | `max_lines` | Absolute line cap (applied after head/tail) |
| 8 | `on_empty` | If result is empty string, return this message instead |

**Lookup priority (first match wins):**
1. `.tokenproxy/filters.toml` (project-local)
2. `~/.tokenproxy/filters.toml` (user-global)
3. Built-in filters (compiled into binary)
4. Generic ANSI strip + truncation
5. Passthrough (no filter)

### 4.6 Filter Dispatch Priority

```go
// When tokenproxy filter receives a command:
//
// 1. Try built-in filter (classify command via RegexSet, call specialized filter)
// 2. Try TOML filter match (check match_command against all TOML filter definitions)
// 3. Apply generic ANSI strip + line truncation (always, even for unmatched commands)
// 4. If command is completely unknown: passthrough (print raw output)
//
// The generic ANSI strip ensures that even unmatched commands get basic cleanup.
// This prevents ANSI garbage from entering the conversation.

func dispatchFilter(cmd string, stdout string, args []string) (string, error) {
    // Step 1: Built-in
    if filter := classifyCommand(cmd); filter != nil {
        result, err := filter(stdout, args, 0)
        if err == nil { return result, nil }
        // Fallback on error
    }

    // Step 2: TOML
    if tomlFilter := matchTOMLFilter(cmd); tomlFilter != nil {
        return tomlFilter.Apply(stdout), nil
    }

    // Step 3: Generic cleanup
    cleaned := stripANSI(stdout)
    cleaned = normalizeWhitespace(cleaned)
    if len(cleaned) > maxPassthroughChars {
        cleaned = truncateWithHint(cleaned, maxPassthroughChars)
    }
    return cleaned, nil
}
```

### 4.7 Tee Recovery System

When a filtered command fails (non-zero exit code), the raw unfiltered output
is saved to disk for later inspection. This prevents information loss.

```go
// Tee system: save raw output on failure
//
// Location: ~/.tokenproxy/tee/{epoch}_{sanitized_command}.log
// Rotation: keep last 20 files, max 1MB each
// Trigger: exit code != 0, OR filter truncated output significantly (>80% removed)
//
// After saving, print hint to stderr:
//   "[full output: ~/.tokenproxy/tee/1712700000_cargo_test.log]"
//
// The LLM agent can read this file if it needs the full unfiltered output.
// This ensures zero information loss even with aggressive filtering.

func teeIfNeeded(cmd string, rawOutput string, filtered string, exitCode int) {
    if exitCode == 0 && len(filtered) > len(rawOutput)/5 {
        return // no need to tee (successful + not heavily truncated)
    }
    if len(rawOutput) < 500 {
        return // too small to bother
    }

    slug := sanitizeSlug(cmd)
    filename := fmt.Sprintf("%d_%s.log", time.Now().Unix(), slug)
    path := filepath.Join(teeDir(), filename)

    os.MkdirAll(filepath.Dir(path), 0755)
    os.WriteFile(path, []byte(rawOutput), 0644)
    cleanupOldTeeFiles(20)

    fmt.Fprintf(os.Stderr, "[full output: %s]\n", path)
}
```

### 4.8 Filter Tracking (SQLite)

All filter executions are recorded in a local SQLite database for analytics.

```go
// Database: ~/.tokenproxy/tracking.db
// Retention: 90 days automatic cleanup
//
// Schema:
// CREATE TABLE filter_executions (
//     id INTEGER PRIMARY KEY,
//     timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
//     original_cmd TEXT,
//     filter_name TEXT,
//     input_tokens INTEGER,     -- raw output length / 4
//     output_tokens INTEGER,    -- filtered output length / 4
//     savings_pct REAL,
//     execution_time_ms INTEGER,
//     exit_code INTEGER,
//     project_path TEXT,
//     tee_path TEXT              -- path to tee file if saved, NULL otherwise
// );
//
// Token approximation: chars / 4 (same as RTK, consistent with LLM tokenizers)

// Analytics query:
// tokenproxy gain
// -> shows total saved, per-command breakdown, daily/weekly/monthly, project-scoped
```

### 4.9 Permission & Trust Model

Commands can be classified as allow/ask/deny for security.

```go
// Permission rules (configurable in config.toml):
//
// [hooks.permissions]
// deny = ["rm -rf", "dd if=", "mkfs", ":(){ :|:& };:"]
// ask = ["git push --force", "git reset --hard", "docker system prune"]
// exclude_commands = ["curl", "wget"]  // never rewrite these
//
// When a hook receives a command:
// 1. Check deny list -> if match, exit code 2 (hook blocks command)
// 2. Check ask list -> if match, exit code 3 (hook rewrites but prompts user)
// 3. Check exclude list -> if match, exit code 1 (passthrough, no rewrite)
// 4. Otherwise -> rewrite and allow (exit code 0)

type PermissionVerdict int
const (
    PermissionAllow   PermissionVerdict = iota  // rewrite and auto-allow
    PermissionAsk                                // rewrite but prompt user
    PermissionDeny                               // block command entirely
    PermissionSkip                               // passthrough unchanged
)

func checkPermission(cmd string, config *HooksConfig) PermissionVerdict
```

---

## 5. Layer 1: Deterministic Compression Engine

Layer 1 is pure Go, zero external dependencies, zero latency, zero risk.
It runs synchronously on every request. Total execution time target: <5ms.

**Expanded from original spec:** Layer 1 now includes 14 sub-layers (up from 6).
Sub-layers 5.1-5.6 are the original deterministic compressors.
Sub-layers 5.7-5.14 are new additions that bring RTK-style intelligence to the
post-entry compression pipeline.

**Normative execution order:** The single source of truth for **which sub-layer runs when** is **§3 “Request Flow (detailed)” steps 4a–4m** (ANSI → JSON or comments → dedup including near-duplicate → structure → delta → classifier → tool compressor → success short-circuit → image → repeated collapse → graph prune → pre-filter tag). Sections 5.1–5.14 define **what each sub-layer does**; do not reorder them differently from §3 without updating both places.

### 5.1 JSON Minification

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

### 5.2 Code Comment & Whitespace Stripping

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

**Language support (10 languages):**
- Go: `//`, `/* */`
- TypeScript/JavaScript: `//`, `/* */`
- Rust: `//`, `/* */`, `///` (doc comments treated as comments in old messages)
- Python: `#`, `""" """`
- C/C++: `//`, `/* */`
- Java: `//`, `/* */`, `/** */`
- Ruby: `#`, `=begin`/`=end`
- Shell (bash/zsh): `#`
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

### 5.3 Hash-Based Content Deduplication

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

### 5.4 Regex-Based Code Structure Extraction

**Target:** Large code files in OLD tool results. Replace full file content with
structural summary (signatures, types, imports only).

**Implementation note:** Uses regex-based extraction (NOT tree-sitter/CGO).
This eliminates CGO build complexity, enables trivial cross-compilation, and
achieves 80%+ of tree-sitter's extraction quality with zero dependency risk.
The regex approach is battle-tested in RTK's filter.rs (10 languages, production use).

```go
// Regex-based code structure extraction
//
// For tool_result content containing code files in messages OLDER than window:
// 1. Detect language from file extension
// 2. Apply language-specific regex patterns to extract structure
// 3. Extract: function signatures, type/interface/struct definitions,
//    import statements, const/var declarations, class definitions
// 4. Drop: function bodies, method implementations, comment blocks
// 5. Reconstruct as condensed structural summary
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

**Supported languages (10, via regex patterns):**

| Language | Extracted Patterns | Regex Complexity |
|---|---|---|
| Go | `func` signatures, `type`/`struct`/`interface` decls, `import`, `const`/`var` | Medium |
| TypeScript | `function`/arrow sigs, `interface`/`type`/`class` decls, `import`, `const`/`let` | Medium |
| JavaScript | `function` sigs, `class` decls, `import`, `const`/`let`/`var` | Medium |
| Rust | `fn` signatures, `struct`/`enum`/`trait`/`impl` decls, `use`, `const` | Medium |
| Python | `def` signatures, `class` decls, `import`, global assignments | Low |
| C | function prototypes, `struct`/`typedef`/`enum` decls, `#include` | Medium |
| C++ | same as C + `class`/`namespace`/`template` decls | High |
| Java | `method` signatures, `class`/`interface`/`enum` decls, `import` | Medium |
| Ruby | `def`/`class`/`module` decls, `require`/`include` | Low |
| Shell | `function` decls, variable assignments, `source`/`.` includes | Low |

**Regex extraction example (Go):**

```go
// Go structure extraction patterns (compiled once via sync.Once)
var goPatterns = []StructurePattern{
    {Name: "import", Regex: regexp.MustCompile(`(?m)^import\s+(?:\([\s\S]*?\)|"[^"]*")`)},
    {Name: "func",   Regex: regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s+)?(\w+)\s*\([^)]*\)(?:\s*(?:\([^)]*\)|[^{]*))?`)},
    {Name: "type",   Regex: regexp.MustCompile(`(?m)^type\s+\w+\s+(?:struct|interface)\s*\{`)},
    {Name: "const",  Regex: regexp.MustCompile(`(?m)^(?:const|var)\s+(?:\([\s\S]*?\)|\w+)`)},
}

// For aggressive mode: extract only the matched patterns, drop everything else.
// For minimal mode: strip comments and blank lines, keep code.
// For none mode: passthrough.
```

**Rules:**
- ONLY apply to messages outside the sliding window (not recent messages)
- ONLY apply to tool_result content that contains recognizable code files
- If regex extraction fails (unknown language, no matches): pass through uncompressed
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

### 5.5 Delta Encoding for File Revisions

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

### 5.6 Prompt Cache Prefix Optimization (Anthropic-specific)

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

### 5.7 ANSI Escape Code & Progress Bar Stripping

**Target:** Old tool_result content containing ANSI color codes, cursor movement
sequences, carriage return overwrites, and progress bar artifacts.

**Implementation:**

```go
// Strip all ANSI escape sequences and progress bar patterns from old tool_results.
// Applied as the FIRST step in the Layer 1 pipeline (before other sub-layers).
//
// Patterns removed:
// - ANSI CSI sequences: \x1b\[[0-9;]*[a-zA-Z]  (colors, cursor, clear)
// - ANSI OSC sequences: \x1b\][^\x07]*\x07      (title, hyperlinks)
// - Carriage return overwrites: lines containing \r (progress bars)
// - Spinner patterns: lines matching [\|/\-] or [==>   ] patterns
// - npm/pip/cargo progress: "Downloading [==>  ] 45%" etc.

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07`)
var progressLineRegex = regexp.MustCompile(`(?m)^.*\r[^\n]*$`)
var spinnerRegex = regexp.MustCompile(`(?m)^[\s]*[|/\\\-]\s+.*$`)

func stripANSIAndProgress(content string) string {
    result := ansiRegex.ReplaceAllString(content, "")
    result = progressLineRegex.ReplaceAllString(result, "")
    result = spinnerRegex.ReplaceAllString(result, "")
    // Normalize resulting blank lines
    result = regexp.MustCompile(`\n{3,}`).ReplaceAllString(result, "\n\n")
    return strings.TrimSpace(result)
}
```

**Why this matters:** ANSI codes are invisible garbage that wastes tokens. A single
colored line can contain 50+ bytes of escape sequences (5-12 tokens). Progress bars
that overwrote themselves via \r produce multiple versions of the same line. Stripping
these is pure upside with zero information loss.

**Expected savings:** 2-5% on all tool_results (higher for colorized CLI output)
**Risk:** Zero. ANSI codes carry no semantic information.

### 5.8 Tool-Result Classifier

**Target:** Classify old tool_result content blocks by type to enable specialized
compression in sub-layer 5.9.

```go
// ToolResultType identifies the content type of a tool_result block.
// Classification is based on:
// 1. tool_name from the preceding tool_use block (most reliable)
// 2. Content pattern matching (fallback when tool_name is unavailable)

type ToolResultType int
const (
    ToolTypeUnknown     ToolResultType = iota
    ToolTypeGitOutput                          // git status, log, diff, show output
    ToolTypeTestOutput                         // test runner output (go test, cargo test, etc.)
    ToolTypeBuildOutput                        // compiler/build output
    ToolTypeLintOutput                         // linter output (eslint, clippy, etc.)
    ToolTypeFileRead                           // file content (cat, read tool)
    ToolTypeSearchResult                       // grep, find, glob results
    ToolTypeJSONData                           // JSON API responses, config files
    ToolTypeLogOutput                          // application/system logs
    ToolTypeDirListing                         // ls, tree output
    ToolTypeCommandOutput                      // generic command output
)

// Classification rules (ordered by specificity):
//
// tool_name "Bash" + content starts with "On branch" or "HEAD detached" -> GitOutput
// tool_name "Bash" + content contains "PASS" or "FAIL" or "test" with counts -> TestOutput
// tool_name "Bash" + content contains "error:" or "warning:" with file:line -> BuildOutput
// tool_name "Bash" + content contains rule codes (E0001, no-unused-vars) -> LintOutput
// tool_name "Read" or "View" -> FileRead
// tool_name "Grep" or "Glob" -> SearchResult
// tool_name "Bash" + content starts with "{" or "[" and is valid JSON -> JSONData
// Content has timestamps + repeated patterns -> LogOutput
// Content has file permissions + sizes (ls -la) -> DirListing
// Default -> CommandOutput

func classifyToolResult(toolName string, content string) ToolResultType
```

**Expected savings:** 0% directly (classifier enables 5.9)
**Risk:** Misclassification falls back to generic compression. Never worse than current behavior.

### 5.9 Tool-Output Compressor (RTK-Style Filters on Old Messages)

**Target:** Apply the same filtering strategies as Layer 0 (Section 4.4) to old
tool_result content blocks that are outside the sliding window. This handles:
- Messages from before the hook was installed
- Messages from CLI tools that don't support hooks
- Messages where the hook was temporarily disabled

```go
// For each old tool_result classified by 5.8:
// Apply the corresponding filter function from the Layer 0 filter inventory.
//
// This is the SAME filter code used in "tokenproxy filter" (Section 4.4),
// but applied to content that is already in the conversation history.
//
// Key difference from Layer 0: these filters run on content the model has
// ALREADY SEEN. The model does not need the raw output anymore - it needs
// the key information. So these filters can be MORE aggressive than Layer 0.
//
// Example: A git diff from message 5 that spans 200 lines.
// Layer 0 (at entry time): might keep 50 lines (key hunks + stats)
// Layer 1.9 (post-entry, old message): might reduce to 5 lines (just stats)
//
// Aggressiveness is controlled by message age:
// - Messages in sliding window: NEVER filtered by 5.9 (protected)
// - Messages 1-2 windows old: moderate filtering (same as Layer 0 level)
// - Messages 3+ windows old: aggressive filtering (stats/summary only)

func compressToolOutput(toolType ToolResultType, content string, messageAge int) string {
    switch toolType {
    case ToolTypeGitOutput:
        return filterGitCompact(content, messageAge)
    case ToolTypeTestOutput:
        return filterTestCompact(content, messageAge)
    case ToolTypeBuildOutput:
        return filterBuildCompact(content, messageAge)
    case ToolTypeLintOutput:
        return filterLintCompact(content, messageAge)
    // ... etc for all types
    default:
        return content // passthrough for unclassified
    }
}
```

**Expected savings:** 15-30% on old tool_results (on top of other Layer 1 sub-layers)
**Risk:** Low. Only applied to old messages. Falls back to passthrough on error.

### 5.10 Success Short-Circuit

**Target:** Old tool_result content that indicates a successful operation with no
meaningful output (clean builds, all tests passing, successful installs).

```go
// Detect and replace verbose success output with one-liner in old messages.
//
// Patterns (regex, applied to full tool_result content):
//
// Build success:
//   "0 errors, 0 warnings" -> "[ok] Build clean"
//   "BUILD SUCCESSFUL" -> "[ok] Build successful"
//   "Compiled successfully" -> "[ok] Compiled"
//
// Test success:
//   "X passed, 0 failed" -> "[ok] X tests passed"
//   "All X tests passed" -> "[ok] X tests passed"
//   "ok  \t" (go test summary) -> "[ok] Tests passed"
//
// Install/update:
//   "added N packages" -> "[ok] N packages installed"
//   "up to date" -> "[ok] Dependencies up to date"
//
// Format/lint clean:
//   "" (empty output from formatter) -> "[ok] Formatted"
//   "All matched files use Prettier" -> "[ok] Format clean"
//
// Each pattern has an optional "unless" condition that prevents short-circuit
// when errors or warnings ARE present (prevents false positives).

type ShortCircuitRule struct {
    Pattern *regexp.Regexp
    Unless  *regexp.Regexp  // nil = no unless check
    Message string
}

var shortCircuitRules = []ShortCircuitRule{
    {Pattern: regexp.MustCompile(`(?i)0 errors?,?\s*0 warnings?`),
     Message: "[ok] Build clean"},
    {Pattern: regexp.MustCompile(`(?i)(\d+) passed?,?\s*0 failed`),
     Message: "[ok] $1 tests passed"},
    // ... 20+ rules
}
```

**Expected savings:** 3-5% overall (massive per-hit: 95%+ on clean build/test output)
**Risk:** Zero. "unless" conditions prevent false short-circuits. Only old messages.

### 5.11 Image Base64 Replacement

**Target:** Base64-encoded image data in old tool_result content blocks.
Screenshots can be 50-200K tokens. In old messages, the pixel data is worthless.

```go
// Detect and replace base64 image data in old messages.
//
// Detection:
// 1. ContentBlock.Type == "image" with ImageData field (Anthropic format)
// 2. Content containing data:image/png;base64,... or data:image/jpeg;base64,...
// 3. Large base64 strings (>1000 chars of [A-Za-z0-9+/=])
//
// Replacement strategy:
// - If image is a terminal screenshot (detected by: tool_name contains "screenshot"
//   or preceding tool_use is computer/browser tool):
//   Try to extract readable text: base64-decode -> look for printable ASCII sequences
//   Replace with: "[Terminal screenshot: extracted text follows]\n{extracted_text}"
//   This preserves OCR-extractable information.
//
// - If image is a photo/diagram (non-terminal):
//   Replace with: "[Image: {width}x{height} {format}, from message {N}]"
//   Dimensions extracted from base64 header (PNG IHDR chunk / JPEG SOF marker).
//
// - If type cannot be determined:
//   Replace with: "[Image data removed from old message {N}]"
//
// NEVER replace images in the sliding window (model might need to see them).

func replaceImageBase64(block ContentBlock, msgAge int) ContentBlock {
    if msgAge == 0 { return block } // never touch sliding window
    if block.Type != "image" && !containsBase64Image(block.Text) {
        return block
    }
    // ... extraction/replacement logic
}
```

**Expected savings:** 5-15% PER IMAGE (a single screenshot can be 100K+ tokens)
**Risk:** Low. Only old images. Terminal screenshot text is preserved via extraction.

### 5.12 Repeated Tool Collapse

**Target:** Identical tool_use + tool_result pairs appearing multiple times
in the conversation. LLM agents frequently re-run the same commands.

```go
// Detect and collapse repeated identical tool calls in old messages.
//
// Detection: hash(tool_name + tool_input) compared against seen-calls index.
// If identical call found in an earlier message AND the result is also identical
// (hash of tool_result content matches):
//
// Replace tool_result content with:
//   "[Identical to {tool_name} result in message {N}]"
//
// If the call is identical but the result differs (e.g., git status changed):
//   Do NOT collapse (the difference is meaningful information).
//
// This is DIFFERENT from dedup (5.3): dedup compares raw content strings.
// Repeated-tool compares (tool_name, tool_input) tuples. This catches cases
// where the same grep returns differently-formatted but semantically identical
// results (ANSI vs no-ANSI, different terminal widths).

type ToolCallIndex struct {
    mu    sync.Mutex
    calls map[[32]byte]int  // hash(tool_name+input) -> first message index
    results map[[32]byte][32]byte // call_hash -> result_hash (to verify identical results)
}

func collapseRepeatedTools(
    block ContentBlock, toolName string, toolInput string,
    index *ToolCallIndex, msgIdx int,
) ContentBlock
```

**Expected savings:** 3-5% (higher in sessions with repetitive grep/find/read cycles)
**Risk:** Zero. Only collapses when BOTH call AND result are identical.

### 5.13 Conversation Graph Pruning

**Target:** Redundant file operations in old messages. When a file is read, then
edited, then read again - the first read is redundant (the latest read has current content).

```go
// Build a dependency graph of file operations across the conversation.
//
// Track: filepath -> []Operation{type, messageIdx}
// Operation types: Read, Edit, Write, Delete
//
// Pruning rule:
// If a file has operations [Read@5, Edit@8, Read@12]:
//   Message 5's Read is a CANDIDATE for pruning because:
//   - Message 12 has a newer Read (more current content)
//   - The content at message 5 is outdated
//
// SAFETY CHECK before pruning:
// Scan messages 6-11 for ANY reference to "message 5" (by index).
// If found: do NOT prune (some message explicitly references it).
// Pattern: look for "message 5", "msg 5", "in message 5", "[5]" etc.
//
// If safe to prune:
// Replace message 5's file content with:
//   "[File {path} was read here but superseded by read in message {12}]"
//
// This is more aggressive than delta encoding (5.5): delta keeps the old
// content as a diff base. Pruning removes it entirely. The latest read
// has the authoritative content.

type FileOpGraph struct {
    mu    sync.RWMutex
    files map[string][]FileOp  // filepath -> chronological operations
}

type FileOp struct {
    Type    FileOpType  // Read, Edit, Write, Delete
    MsgIdx  int
    Hash    [32]byte
}

func (g *FileOpGraph) FindPrunablReads(messages []Message) []PruneCandidate {
    // For each file with Read->Edit->Read pattern:
    // Check no later message references the old read's message index
    // Return list of (messageIdx, filepath) pairs safe to prune
}
```

**Expected savings:** 5-10% on edit-heavy sessions (file read cycles are common)
**Risk:** Low. Safety check prevents pruning referenced messages. Fallback: skip prune.

### 5.14 Pre-Filtered Content Tagging

**Target:** Optimization for content that was already filtered by Layer 0.
Avoids redundant Layer 1 processing on already-compact content.

```go
// When Layer 0 filters a command output, the resulting tool_result is already
// compact. Layer 1 sub-layers like comment stripping, JSON compact, and structure
// extraction would find nothing to compress (or worse, could mangle the already-
// compressed format).
//
// Detection: Layer 0 adds a marker to filtered content:
//   The hook system sets a custom header or metadata flag that the proxy can detect.
//   Alternatively: content from Layer 0 follows recognizable patterns
//   (e.g., starts with "[ok]", contains "[N matches]", uses compact git format).
//
// When pre-filtered content is detected in a tool_result:
// - Skip: comment stripping (5.2), JSON compact (5.1), structure extraction (5.4)
// - Apply: dedup (5.3), delta (5.5) - these are still useful on pre-filtered content
// - Apply: success short-circuit (5.10) - pre-filtered success output can be further reduced
// - Apply: repeated tool collapse (5.12) - identical filtered results can still be collapsed
//
// This saves ~0.5ms per pre-filtered tool_result in the Layer 1 pipeline.
// No token savings directly, but reduces CPU work and prevents format mangling.

func isPreFiltered(content string) bool {
    // Check for Layer 0 format markers:
    // - Starts with "[ok] " (success short-circuit from Layer 0)
    // - Contains "[N matches]" (search result format from Layer 0)
    // - Matches compact git status format ("N modified, N staged")
    // - Contains "[full output:" tee reference
    return preFilteredMarkerRegex.MatchString(content)
}
```

**Expected savings:** 0% tokens (performance optimization only)
**Risk:** Zero. Worst case: sub-layers run on already-compact content (no harm).

---

## 6. Layer 2: Intelligent Compression via MiniMax M2.7

Layer 2 uses MiniMax M2.7 to abstractively summarize old conversation history.
This is the highest-impact layer but requires external API calls.

### 6.1 Compression Strategy: Sliding Window with Anchor Points

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

### 6.2 Anchor Point Detection (Algorithmic, No LLM)

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

### 6.3 MiniMax Summarization Prompt

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

### 6.4 MiniMax API Integration

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

### 6.5 Summary Caching & Invalidation

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

### 6.6 Progressive Compression Tiers

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

### 6.7 Quality Safeguard: Compression Validation

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

### 6.8 Adaptive Sliding Window

**Enhancement:** Instead of a fixed sliding window size (default: 5 exchanges),
dynamically adjust the window based on session complexity.

```go
// The sliding window determines how many recent exchanges are NEVER compressed.
// Fixed window = suboptimal in two ways:
// - Simple Q&A sessions: window 5 is wasteful (could compress more)
// - Complex multi-file refactors: window 5 might miss critical recent context
//
// Adaptive window algorithm:
//
// BaseWindow = config.SlidingWindow (default: 5)
//
// Complexity score (0.0 - 1.0) computed from recent messages:
// - UniqueFilePaths: count of distinct file paths in last 10 messages
// - AnchorDensity: fraction of last 10 messages that are anchors
// - ToolCallDiversity: count of distinct tool names in last 10 messages
//
// score = 0.3 * normalize(UniqueFilePaths, 1, 15)
//       + 0.4 * AnchorDensity
//       + 0.3 * normalize(ToolCallDiversity, 1, 8)
//
// AdaptiveWindow = BaseWindow + round(score * 4) - 2
// Clamped to [BaseWindow - 2, BaseWindow + 2]
// Minimum: 3 (always protect at least 3 recent exchanges)
// Maximum: BaseWindow + 2
//
// Effect:
// - Simple session (score ~0.1): window = 3 -> more compressible history
// - Normal session (score ~0.5): window = 5 -> default behavior
// - Complex session (score ~0.9): window = 7 -> more context preserved

func adaptiveWindowSize(messages []Message, baseWindow int) int {
    if len(messages) < baseWindow+2 {
        return baseWindow // not enough messages to adapt
    }
    recentMsgs := messages[max(0, len(messages)-10):]
    score := computeComplexityScore(recentMsgs)
    adjusted := baseWindow + int(math.Round(score*4)) - 2
    return max(3, min(baseWindow+2, adjusted))
}
```

**Expected savings:** 3-8% average (more on simple sessions, less on complex ones)
**Risk:** Minimal. Window only shrinks by 2 at most. Complex sessions get MORE protection.

### 6.9 Tool Result Priority Classification

**Enhancement:** Classify tool results as HIGH/MEDIUM/LOW priority for differentiated
compression in the MiniMax summarization step.

```go
// Priority affects how MiniMax treats content in the summarization prompt:
// - HIGH: include verbatim in summary (errors, edits, decisions)
// - MEDIUM: include key facts, may paraphrase (file reads, search results)
// - LOW: may reduce to one-liner (successful builds, clean tests, directory listings)
//
// Classification (based on ToolResultType from 5.8 + content analysis):
//
// HIGH priority:
// - AnchorEdit messages (file edits)
// - AnchorError messages (error traces)
// - AnchorDecision messages (user confirmations)
// - Test failures (FAIL in test output)
// - Build failures (error: in build output)
//
// MEDIUM priority:
// - File reads (model may need to recall file content)
// - Search results (model may need to recall what was found)
// - Git diff output (model may need to recall what changed)
//
// LOW priority:
// - Successful builds ("0 errors")
// - All-passing test runs ("42 passed, 0 failed")
// - Directory listings (model can re-run ls)
// - Package install success ("added 5 packages")
//
// The MiniMax summarization prompt is augmented:
//   "HIGH priority items must be preserved verbatim.
//    LOW priority items may be reduced to one-line summaries.
//    MEDIUM priority items should preserve key facts but may paraphrase."

type ToolResultPriority int
const (
    PriorityLow    ToolResultPriority = iota
    PriorityMedium
    PriorityHigh
)

func classifyPriority(toolType ToolResultType, content string, isAnchor bool) ToolResultPriority
```

**Expected savings:** 3-5% (MiniMax can compress LOW priority content more aggressively)
**Risk:** Zero. HIGH priority content gets MORE protection. LOW priority content is
the kind that the model can easily re-acquire via tool calls.

---

## 7. Layer 3: Caching & Optimization

### 7.1 Response Cache

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

### 7.2 File Change Detection (fsnotify)

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

### 7.3 Usage Tracker

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

## 8. Layer 4: Go Concurrency Pipeline

This is the core engine that makes everything work without perceived latency.

### 8.1 Goroutine Architecture

```go
type Proxy struct {
    // Core
    server          *http.Server
    upstreamClients map[Provider]*UpstreamClient

    // Layer 1
    compressor      *DeterministicCompressor
    contentIndex    *ContentIndex
    fileTracker     *FileVersionTracker
    structExtractor *StructureExtractor

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

### 8.2 Request Handler (Hot Path)

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

### 8.3 SSE Streaming Relay

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

### 8.4 Async Compression Worker

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

### 8.5 Graceful Shutdown

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

## 9. Multi-Provider Support

### 9.1 Provider Detection

```go
import (
    "encoding/json"
    "net/http"
    "strings"
)

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
    // Fallback: check body structure (use encoding/json — normative; no gjson dependency)
    var probe map[string]json.RawMessage
    if json.Unmarshal(body, &probe) == nil {
        _, hasMaxTokens := probe["max_tokens"]
        _, hasFreqPenalty := probe["frequency_penalty"]
        if hasMaxTokens && !hasFreqPenalty {
            return Anthropic
        }
    }
    return OpenAI
}
```

### 9.2 Message Format Normalization

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

### 9.3 Authentication: OAuth Passthrough (No API Keys Required)

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

### 9.4 Provider Compatibility Matrix

| Feature | Claude Code | Codex |
|---|---|---|
| Base URL override | ANTHROPIC_BASE_URL (env) | openai_base_url (config.toml) |
| Auth type | OAuth via Bridge | OAuth via ChatGPT |
| Auth passthrough | Headers forwarded 1:1 | Headers forwarded 1:1 |
| Official support | Yes (changelog confirms) | Supported but deprecated env var |
| Coverage | Full (all API calls) | Partial (api.openai.com only, chatgpt.com backend may bypass) |
| Confidence level | HIGH | MEDIUM (test required) |

### 9.5 Setup: How Each CLI Connects to the Proxy

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

### 9.6 Pre-Flight Validation Test

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

### 9.7 Provider-Specific Optimizations

**Anthropic-only features:**
- Prompt cache breakpoint injection (cache_control)
- Extended thinking support (pass through unmodified)
- Content block structure preservation (image blocks, tool_use blocks)

**OpenAI-only features:**
- System message optimization (single system message at position 0)
- Function calling format preservation
- Logprobs passthrough

---

## 10. Secret Detection & Redaction

Bonus security layer: scan all outgoing requests for accidentally included secrets.

### 10.1 Detection Patterns

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

### 10.2 Redaction Behavior

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

## 11. Analytics & Observability

### 11.1 Metrics Tracked

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

### 11.2 Interactive TUI Dashboard (BubbleTea + Lipgloss)

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
│  [1] Deterministic  ● ON   312K saved   (JSON, dedup, structure)   │
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
│  14:31:45 DEBUG layer1 structure_extract saved=8900 files=3          │
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

# Configuration
tokenproxy config init       # Generate default ~/.tokenproxy/config.toml
tokenproxy config show       # Print resolved config (file + env)

# Connectivity tests
tokenproxy test minimax      # Test MiniMax API connectivity
tokenproxy test anthropic    # Test Anthropic upstream reachability
tokenproxy test openai       # Test OpenAI upstream reachability
tokenproxy doctor            # Run ALL diagnostics (config + connectivity + hooks)

# Layer 0: Pre-entry filtering
tokenproxy filter <cmd>      # Execute command with filter (RTK-replacement)
tokenproxy hook install <agent>  # v1: claude | codex (see §4.3)
tokenproxy hook verify       # Check hook integrity (SHA-256)
tokenproxy hook status       # Show installed hooks
tokenproxy hook remove <agent>   # Remove hook

# Analytics
tokenproxy stats today       # Print today's stats from persisted analytics
tokenproxy stats week        # Print this week's stats
tokenproxy stats month       # Print this month's stats
tokenproxy gain              # Token savings dashboard (like rtk gain)
tokenproxy gain --project    # Project-scoped savings
tokenproxy gain --format json    # Machine-readable export

# Debug (AI-agent-optimized)
tokenproxy debug last        # Last request decision tree (JSON)
tokenproxy debug last 5      # Last 5 requests
tokenproxy debug summary     # Aggregated patterns
tokenproxy debug tail        # Streaming JSONL output
tokenproxy debug tail --level trace --layer 1  # Filtered stream

# Misc
tokenproxy version           # Print version and exit
```

These are utility commands that do NOT start the proxy or TUI.
They are for setup, diagnostics, Layer 0 filtering, and reviewing data.

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

### 11.3 Log Output

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

### 11.4 Persistent Analytics

```go
// Session analytics are persisted to disk on shutdown and on periodic flush (every 5 min).
// Format: JSON lines (one JSON object per line) appended to analytics log file.
// Location: ~/.tokenproxy/analytics/YYYY-MM-DD.jsonl
//
// Enables: historical analysis of token savings, cost tracking over time,
// identification of usage patterns, ROI measurement.
```

---

## 12. Debug & Observability System (AI-Agent-Optimized)

The debug system is designed for AI agents to consume, not humans. Output is
structured JSONL optimized for `jq` piping. The system provides full transparency
into every compression decision without flooding the context with unnecessary tokens.

### 12.1 Debug Subcommand

```bash
# Show last request with full filter decision tree
tokenproxy debug last
# Output: single JSON object with all layer decisions

# Show last N requests
tokenproxy debug last 5

# Aggregated compression patterns (which filters hit most, savings distribution)
tokenproxy debug summary
# Output: JSON with filter_hit_counts, avg_savings_by_type, content_type_distribution

# Streaming JSONL tail (like TUI debug but to stdout for AI agents)
tokenproxy debug tail
# Output: continuous JSONL stream, one line per event (Ctrl+C to stop)

# Streaming with filter
tokenproxy debug tail --level debug --layer 1
# Only Layer 1 events at debug level

# Replay a session through the filter pipeline with full logging
tokenproxy debug replay ~/.tokenproxy/sessions/2026-04-10_12-00-00.jsonl
# Replays stored request data, shows what each layer would do now
```

### 12.2 Decision Chain Format

Every content block processed by any layer gets a decision log entry.
This is the core format for understanding WHY something was/wasn't compressed.

```go
// DecisionEntry records one compression decision for one content block.
// Written to the debug log at TRACE level.
// Aggregated at DEBUG level (one per request).
// Summarized at INFO level (session aggregates).

type DecisionEntry struct {
    Timestamp   time.Time          `json:"ts"`
    RequestID   string             `json:"req_id"`       // unique per API request
    MessageIdx  int                `json:"msg_idx"`       // position in conversation
    BlockIdx    int                `json:"block_idx"`     // position in message content
    ContentType string             `json:"content_type"`  // "text", "tool_result", "image"
    Layer       int                `json:"layer"`         // 0, 1, 2, 3
    SubLayer    string             `json:"sub_layer"`     // "json_compact", "dedup", "ansi_strip", etc.
    Action      string             `json:"action"`        // "compressed", "skipped", "passthrough", "short_circuit"
    Reason      string             `json:"reason"`        // why this action was taken
    TokensBefore int               `json:"tokens_before"` // estimated tokens before this sub-layer
    TokensAfter  int               `json:"tokens_after"`  // estimated tokens after this sub-layer
    SavedTokens  int               `json:"saved"`         // tokens_before - tokens_after
    Settings    map[string]string  `json:"settings"`      // relevant config values that affected this decision
}

// Example decision chain for one tool_result block:
//
// {"ts":"...","msg_idx":5,"layer":1,"sub_layer":"ansi_strip","action":"compressed",
//  "reason":"42 ANSI sequences removed","tokens_before":1200,"tokens_after":1150,"saved":50}
// {"ts":"...","msg_idx":5,"layer":1,"sub_layer":"tool_classifier","action":"classified",
//  "reason":"detected as ToolTypeGitOutput (tool_name=Bash, pattern: 'On branch')"}
// {"ts":"...","msg_idx":5,"layer":1,"sub_layer":"tool_compressor","action":"compressed",
//  "reason":"git_status filter applied: 40 lines -> '3 modified, 1 staged'","tokens_before":1150,"tokens_after":30,"saved":1120}
// {"ts":"...","msg_idx":5,"layer":1,"sub_layer":"dedup","action":"skipped",
//  "reason":"no exact match in content index (hash: a1b2c3...)"}
```

### 12.3 Request Summary Format

Per-request summary (DEBUG level) aggregates all decision entries:

```json
{
  "req_id": "req_abc123",
  "ts": "2026-04-10T14:32:01Z",
  "provider": "anthropic",
  "model": "claude-opus-4-6",
  "total_messages": 47,
  "messages_in_window": 5,
  "messages_compressed": 42,
  "layers_applied": [1, 2],
  "tokens": {
    "original": 87400,
    "after_layer0": 65000,
    "after_layer1": 42000,
    "after_layer2": 29100,
    "final": 29100,
    "saved": 58300,
    "ratio": 0.33
  },
  "layer1_breakdown": {
    "ansi_strip": {"blocks": 42, "saved": 850},
    "json_compact": {"blocks": 8, "saved": 2100},
    "comment_strip": {"blocks": 12, "saved": 3400},
    "dedup": {"blocks": 3, "saved": 8900},
    "structure_extract": {"blocks": 5, "saved": 4200},
    "tool_compressor": {"blocks": 15, "saved": 12300},
    "success_shortcircuit": {"blocks": 4, "saved": 6800},
    "image_replace": {"blocks": 0, "saved": 0},
    "repeated_collapse": {"blocks": 2, "saved": 1900},
    "graph_pruning": {"blocks": 1, "saved": 2500}
  },
  "layer2": {
    "applied": true,
    "cache_hit": true,
    "covered_range": [0, 38],
    "original_tokens": 198000,
    "compressed_tokens": 35640,
    "anchor_count": 4
  },
  "cache_hit": false,
  "secrets_redacted": 0,
  "proxy_latency_ms": 3.2
}
```

### 12.4 Debug Log Levels

| Level | Content | Token cost per request | Use case |
|---|---|---|---|
| TRACE | Every sub-layer decision per block | ~500-2000 tokens | Deep debugging specific compression issue |
| DEBUG | Per-request summary with layer breakdown | ~200-400 tokens | Verify compression behavior |
| INFO | Aggregate stats (session-level) | ~50-100 tokens | General monitoring |
| WARN/ERROR | Problems only | ~0-50 tokens | Production monitoring |

**AI-agent workflow:**

```bash
# Quick check: "is the proxy working correctly?"
tokenproxy debug last | jq '.tokens.ratio'
# Output: 0.33

# Investigate: "why was message 15 not compressed?"
tokenproxy debug last | jq '.layer1_breakdown'
# Shows per-sub-layer savings

# Deep dive: "what happened to the git diff in message 15?"
tokenproxy debug last --trace | jq 'select(.msg_idx == 15)'
# Shows every decision for message 15

# Pattern analysis: "which filter saves the most?"
tokenproxy debug summary | jq '.filter_hit_counts | sort_by(-.saved) | .[0:5]'
# Top 5 filters by tokens saved
```

---

## 13. Configuration System

### 13.1 Config File

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

# Sliding window: number of recent USER-STARTED EXCHANGES to leave uncompressed.
# An exchange begins at each message with role "user" (tool-result user messages count).
# Default 5 = last five user turns and everything after the fifth-from-last user message stays hot.
sliding_window = 5

# Minimum conversation length before any compression triggers
min_messages_for_compression = 8

# Minimum token count before Layer 2 triggers
min_tokens_for_layer2 = 30000

# Structure extraction: minimum file size (tokens) to apply signature extraction
structure_min_tokens = 500

# Structure extraction: languages to support (others pass through uncompressed)
structure_languages = ["go", "typescript", "javascript", "rust", "python", "c", "cpp", "java", "ruby", "shell"]

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

[filter]
# Layer 0: Pre-entry filtering configuration
enabled = true
# Maximum output length for passthrough commands (chars, not tokens)
passthrough_max_chars = 2000
# SQLite tracking database path (empty = default ~/.tokenproxy/tracking.db)
tracking_db = ""
# Tracking retention in days
tracking_retention_days = 90
# Tee recovery mode: "failures" (default), "always", "never"
tee_mode = "failures"
# Maximum tee files to keep
tee_max_files = 20
# Command execution timeout in seconds
command_timeout_seconds = 120

[filter.limits]
# Per-filter output limits
grep_max_results = 200
grep_max_per_file = 25
git_status_max_files = 15
git_status_max_untracked = 10
test_max_failure_lines = 50
log_max_unique_lines = 30

[hooks]
# Commands to never rewrite (passthrough always)
exclude_commands = []
# Permission rules
# deny = ["rm -rf /", "dd if=", "mkfs"]
# ask = ["git push --force", "git reset --hard"]

[analytics]
# Enable terminal dashboard
dashboard = true
# Persistent log directory
log_dir = "~/.tokenproxy/analytics"
# Dashboard refresh interval
dashboard_refresh_seconds = 2

[debug]
# Debug output level for AI-agent consumption: "trace", "debug", "info", "warn", "error"
level = "info"
# Debug output format: "jsonl" (for AI agents, default), "text" (for humans)
format = "jsonl"
# Maximum decision entries to keep in memory (ring buffer)
max_entries = 1000

[logging]
# Log level: "debug", "info", "warn", "error"
level = "info"
# Log format: "text", "json"
format = "text"
# Log file (empty = stderr only)
file = ""
```

### 13.2 Environment Variable Overrides

Every config value can be overridden via environment variable:

```bash
TOKENPROXY_LISTEN_PORT=9090
TOKENPROXY_COMPRESSION_SLIDING_WINDOW=8
TOKENPROXY_BUDGET_DAILY_TOKEN_LIMIT=5000000
TOKENPROXY_SECRETS_MODE=block
# etc.

# Pattern: TOKENPROXY_{SECTION}_{KEY} in uppercase, dots replaced by underscores
```

### 13.3 CLI Flag Overrides

```bash
tokenproxy --port 9090 --sliding-window 8 --no-layer2
# CLI flags override env vars, env vars override config file
```

---

## 14. Usage & Quick Setup

### 14.1 One-Command Start

```bash
# That's it. One command. TUI opens, proxy runs.
tokenproxy
```

No `start`, no `--daemon`, no PID files, no systemd units. Open a terminal tab,
run `tokenproxy`, leave it running. Close it when done. The proxy lifecycle is
identical to the TUI lifecycle.

### 14.2 First-Time Setup

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

# 6. Install hooks for pre-entry filtering (Layer 0)
tokenproxy hook install claude     # Claude Code
tokenproxy hook install codex      # Codex (if used)

# 7. Run full diagnostics
tokenproxy doctor

# 8. Start
tokenproxy
```

**Important: OAuth login is NOT affected.** The proxy only intercepts API requests
(message calls). OAuth authentication flows (browser login, token refresh) go directly
to Anthropic/OpenAI as before. You do NOT need to log in again or change any credentials.
Your existing Claude Code and Codex sessions continue to work.

### 14.3 Recommended Terminal Layout

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

### 14.4 Utility Subcommands (non-TUI)

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

### 14.5 Shell Integration (optional convenience)

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

## 15. Data Structures & Types

### 15.1 Core Types

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
    WasStructureExtracted bool   `json:"was_structure_extracted"`
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

### 15.2 Ring Buffer for Request History

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

## 16. API Compatibility Layer

### 16.1 Anthropic Messages API

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

### 16.2 OpenAI Chat Completions API

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

### 16.3 Header Passthrough

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

### 16.4 Provider Invisibility Contract (ABSOLUTE REQUIREMENT)

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

## 17. Error Handling & Resilience

### 17.1 Error Categories and Responses

```go
// Category 1: Parse errors (malformed request)
// Response: 400 Bad Request with error details
// Action: Pass original request through unmodified as fallback

// Category 2: Upstream connection failure
// Response: 502 Bad Gateway
// Action: Retry once after 1s, then return error
// NEVER cache or compress during retry

// Category 3: Compression error (regex panic, filter error, etc.)
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

### 17.2 Panic Recovery

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

### 17.3 Auto-Retry: Rate Limit (429)

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

### 17.4 Auto-Retry: Context Overflow with Aggressive Compression

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
        var errBody struct {
            Error struct {
                Type    string `json:"type"`
                Message string `json:"message"`
            } `json:"error"`
        }
        if json.Unmarshal(body, &errBody) != nil {
            return false
        }
        errType := errBody.Error.Type
        errMsg := errBody.Error.Message
        return errType == "invalid_request_error" &&
            (strings.Contains(errMsg, "too many tokens") ||
             strings.Contains(errMsg, "context length"))
    case OpenAI:
        var errBody struct {
            Error struct {
                Code string `json:"code"`
            } `json:"error"`
        }
        if json.Unmarshal(body, &errBody) != nil {
            return false
        }
        return errBody.Error.Code == "context_length_exceeded"
    }
    return false
}
```

### 17.5 API Health Monitoring (Live in TUI)

Background goroutine monitors upstream API availability and shows status in TUI.

```go
// Health monitor runs as a background goroutine.
// IMPORTANT: It does NOT ping the upstream LLM APIs directly.
// (See Section 16.4 - no outbound traffic to providers beyond what CLI initiates.)
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

### 17.6 Request Logging (Session History)

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

### 17.7 Latency Tracking (Per Provider, Visible in TUI)

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

### 17.8 Health Check Endpoint

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

## 18. Testing Strategy

### 18.1 Unit Tests

```
Layer 1 compression:
- JSON minification: valid/invalid JSON, nested objects, arrays, edge cases
- Comment stripping: per language, string literal preservation, multiline
- Deduplication: exact match, near-duplicate, no-match, edge similarities
- Structure extraction: per language (10), regex failures, minimum size threshold
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

### 18.2 Integration Tests

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

### 18.3 Benchmark Tests

```go
// Benchmark targets (per operation):
//
// BenchmarkJSONMinification          <100us for 10KB JSON
// BenchmarkCommentStripping          <200us for 10KB code
// BenchmarkHashDeduplication         <50us per content block
// BenchmarkStructureExtraction       <2ms for 1000-line file
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

## 19. Performance Targets

| Metric | Target | Notes |
|---|---|---|
| Layer 1 latency | <5ms | Synchronous, per-request |
| Proxy overhead (total) | <10ms | Excluding Layer 2 (async) |
| Memory usage (idle) | <50MB | No active caches |
| Memory usage (active) | <200MB | Full caches, active session |
| Startup time | <100ms | Config load + regex compilation (no CGO init) |
| SSE relay latency | <1ms per event | Zero-copy passthrough target |
| Response cache lookup | <100us | In-memory hash map |
| Token counting | <500us per 10KB | Using tiktoken-go |
| Concurrent requests | 10+ | Limited by upstream, not proxy |
| Binary size | <15MB | No CGO tree-sitter grammars (pure Go + SQLite) |

---

## 20. Dependency Inventory

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
| `github.com/fsnotify/fsnotify` | File change detection | De facto standard, 7k+ stars |
| `modernc.org/sqlite` | Filter tracking database (Layer 0) | **Normative:** pure Go SQLite (no CGO); sufficient for local tracking |
| `github.com/pkoukk/tiktoken-go` | Token counting | Most accurate Go tiktoken port |
| `github.com/BurntSushi/toml` | Config file parsing | De facto standard for TOML in Go |
| `golang.org/x/time/rate` | Rate limiting | Official extended stdlib |

**JSON handling (normative):** all production Go code uses **`encoding/json`**. Spec fragments that showed path-style access illustrate intent only—implement with `json.Unmarshal` into structs or `map[string]json.RawMessage` (see §9.1 and §17.4).

### Required (TUI - core user interface)

| Dependency | Purpose | Justification |
|---|---|---|
| `github.com/charmbracelet/bubbletea` | Terminal UI framework (Elm architecture) | Best-in-class Go TUI, the TUI IS the app |
| `github.com/charmbracelet/lipgloss` | Terminal styling (borders, colors, layout) | Companion to bubbletea, no alternative |
| `github.com/charmbracelet/bubbles` | Pre-built TUI components (progress bar, table, viewport) | Avoids reimplementing standard widgets |

**Note:** Code structure extraction uses regex patterns (not tree-sitter/CGO).
This eliminates CGO dependency, enables trivial cross-compilation, and simplifies builds.

**Total external dependencies: 11 required + 3 TUI = 14** (pure Go; **no CGO** required for SQLite when using `modernc.org/sqlite`)

---

## 21. Project Structure

```
TokenProxy/
  cmd/
    tokenproxy/
      main.go                  # Entrypoint: subcommand dispatch (TUI default, filter, hook, gain, debug)
  internal/
    proxy/
      proxy.go                 # Proxy struct, lifecycle (Start/Shutdown), toggle methods
      handler.go               # HTTP request handler (hot path)
      streaming.go             # SSE relay implementation
      provider.go              # Provider detection + format normalization
    compression/
      layer1.go                # Layer 1 orchestrator (14 sub-layers)
      json_compact.go          # 5.1: JSON minification
      comment_strip.go         # 5.2: Language-aware comment removal (10 languages)
      dedup.go                 # 5.3: SHA256 exact deduplication
      dedup_minhash.go         # 5.3: MinHash/LSH near-duplicate detection
      structure.go             # 5.4: Regex-based code structure extraction (10 languages)
      structure_patterns.go    # 5.4: Per-language regex pattern definitions
      delta.go                 # 5.5: Delta encoding for file revisions
      prompt_cache.go          # 5.6: Anthropic prompt cache optimization
      ansi_strip.go            # 5.7: ANSI escape + progress bar removal
      tool_classifier.go       # 5.8: Tool-result type classification
      tool_compressor.go       # 5.9: RTK-style tool-output compression
      success_shortcircuit.go  # 5.10: Success output detection + one-liner replacement
      image_replace.go         # 5.11: Base64 image replacement in old messages
      repeated_collapse.go     # 5.12: Identical tool call collapse
      graph_pruning.go         # 5.13: Conversation graph pruning (read-edit-read)
      prefiltered.go           # 5.14: Pre-filtered content tagging
    summarization/
      layer2.go                # Layer 2 orchestrator
      minimax.go               # MiniMax M2.7 API client
      anchor.go                # Anchor point detection
      validator.go             # Summary quality validation
      cache.go                 # Summary cache management
      progressive.go           # Progressive compression tiers
      adaptive_window.go       # 6.8: Adaptive sliding window
      priority.go              # 6.9: Tool result priority classification
    caching/
      response_cache.go        # Response cache (Layer 3)
      file_watcher.go          # fsnotify-based cache invalidation
    filter/                    # Layer 0: CLI filter engine
      engine.go                # Filter dispatch + subprocess execution
      classify.go              # Command classification (RegexSet)
      rewrite.go               # Command rewriting engine
      tokenizer.go             # Shell command tokenizer (quotes, pipes, redirects)
      rules.go                 # 60+ rewrite rules
      git.go                   # F01-F05: Git filters
      build.go                 # F07: Build output filter
      test.go                  # F08: Test output filter (multi-ecosystem)
      lint.go                  # F09: Lint output filter
      search.go                # F10: Search result filter (grep, find)
      filesystem.go            # F06, F11: File read + directory listing
      package_manager.go       # F12: Package manager output
      container.go             # F13: Docker/K8s output
      json_schema.go           # F14: JSON schema extraction
      log_dedup.go             # F15: Log deduplication
      aws.go                   # F16: AWS CLI output
      ansi.go                  # F17: Generic ANSI/progress strip
      github.go                # F18: GitHub CLI output
      misc.go                  # F19-F24: psql, dotnet, ruby, go test, mypy, format
      toml_dsl.go              # TOML filter DSL engine (8-stage pipeline)
      tee.go                   # Tee recovery system
      tracking.go              # SQLite filter tracking
    hooks/                     # Hook system
      install.go               # Hook installation per agent (10 agents)
      verify.go                # Hook integrity verification (SHA-256)
      status.go                # Hook status reporting
      scripts.go               # Embedded hook shell scripts
      permissions.go           # Allow/ask/deny permission model
    tokens/
      counter.go               # tiktoken-based token counting
      usage.go                 # Usage tracking
    resilience/
      retry.go                 # Auto-retry for 429 + context overflow
      health.go                # API health monitoring
      latency.go               # Per-provider latency tracking
    sessions/
      logger.go                # Session request/response logging (JSONL)
      export.go                # Session export
    security/
      secrets.go               # Secret detection + redaction
      patterns.go              # Built-in secret patterns
    analytics/
      collector.go             # Analytics event aggregation
      persistence.go           # Disk persistence (JSONL)
      gain.go                  # Token savings dashboard (like rtk gain)
    debug/                     # Debug & observability system
      decisions.go             # Decision chain recording + querying
      summary.go               # Aggregated pattern analysis
      tail.go                  # Streaming JSONL output
    tui/
      model.go                 # BubbleTea model
      views.go                 # View renderers (main, stats, debug)
      styles.go                # Lipgloss style definitions
      components.go            # Reusable TUI components
      keys.go                  # Keybinding definitions
    config/
      config.go                # Config struct + loading (TOML + env + flags)
      defaults.go              # Default configuration values
    types/
      types.go                 # Core message types, events, ring buffer
    util/
      safego.go                # Panic-recovering goroutine launcher
  docs/
    context.md                 # Active development worklog
    documentation.md           # Full technical documentation
    changelog.md               # Version history
    map.md                     # File index + architecture map
    todo.md                    # Master TODO with all pending work
  scripts/
  go.mod
  go.sum
  LICENSE
```

---

## 22. Build & Distribution

### Build

```bash
# Development build (pure Go; regex-based structure extraction; modernc.org/sqlite needs no CGO)
go build -o tokenproxy ./cmd/tokenproxy

# Release build (optimized, stripped)
go build -ldflags="-s -w -X main.version=2.0.0" -o tokenproxy ./cmd/tokenproxy

# macOS universal binary (Apple Silicon + Intel) — CGO not required for TokenProxy dependencies
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

## 23. Rollout Plan

**Sequencing note (normative for this repository):** Implementation order is **`handover.md` §4** — Layer 1 hardening on the existing proxy first, then Layer 0 (filter + hooks), then Layer 2 enhancements, then advanced Layer 1 sub-layers + debug. The phases below are a **feature grouping and completeness checklist** (milestones), not a calendar.

### Phase 1: Core Proxy + Layer 1 Original

Deliverables:
- HTTP reverse proxy with provider detection
- SSE streaming relay
- JSON minification
- Comment stripping (10 languages)
- Hash-based deduplication (SHA256 exact)
- Regex-based code structure extraction (10 languages)
- Delta encoding for file revisions
- ANSI escape + progress bar stripping (5.7)
- Basic token counting
- Config system (TOML + env + flags)
- CLI: start, config, test
- Unit tests for all Layer 1 components
- Integration test: full request/response cycle

Milestone: proxy works transparently with Claude Code and Codex.
Expected savings: 40-55% on long sessions.

### Phase 2: Layer 0 + Hook System

Deliverables:
- `tokenproxy filter <cmd>` subcommand with subprocess execution
- Top 10 built-in filters (F01-F10: git, build, test, lint, search, file read)
- Shell tokenizer + command rewriting engine
- Hook installation for Claude Code + Codex
- TOML filter DSL engine (8-stage pipeline)
- Filter tracking (SQLite)
- Tee recovery system
- Permission model (allow/ask/deny)
- Unit tests for all filters + rewriting engine
- `tokenproxy gain` analytics command

Milestone: Layer 0 + Layer 1 working together. Hook installed and active.
Expected savings: 70-80% on long sessions (Layer 0 + Layer 1 combined).

### Phase 3: Layer 2 + Analytics

Deliverables:
- MiniMax M2.7 integration
- Anchor point detection
- Async pre-compression pipeline
- Summary caching + validation
- Progressive compression tiers
- Adaptive sliding window (6.8)
- Tool result priority classification (6.9)
- Analytics collector
- Terminal dashboard (BubbleTea TUI)
- Persistent analytics (JSONL)
- Secret detection + redaction
- CLI: stats, cache commands

Milestone: full compression pipeline with real-time visibility.
Expected savings: 80-88% on long sessions.

### Phase 4: Advanced Layer 1 + Polish

Deliverables:
- Tool-result classifier (5.8) + tool-output compressor (5.9)
- Success short-circuit (5.10)
- Image base64 replacement (5.11)
- Repeated tool collapse (5.12)
- Conversation graph pruning (5.13)
- Pre-filtered content tagging (5.14)
- MinHash/LSH near-duplicate detection (5.3 enhancement)
- Remaining 14 built-in filters (F11-F24)
- Debug system (Section 12): debug last, summary, tail
- Prompt cache optimization (Anthropic)
- Response caching + file watcher
- Hook installation for additional agents (Cursor, Copilot, Gemini, etc.) — **post-v1 / optional** (v1 normative targets: Claude Code + Codex only; see §4.3)
- Benchmark tests
- Comprehensive integration tests
- Full TUI (all views)

Milestone: production-ready, all layers active, full observability, full filter coverage.
Expected savings: 85-90% on long sessions.

---

## Appendix A: Token Savings Breakdown (Expected)

| Technique | Savings (standalone) | Savings (combined) | Layer |
|---|---|---|---|
| **Pre-entry CLI filtering** | **60-80% on tool output** | **20-35% on total** | **0** |
| ANSI + progress strip | 2-5% | 2-3% | 1 |
| JSON minification | 10-25% | 5-10% | 1 |
| Comment stripping (10 langs) | 5-15% | 3-6% | 1 |
| Hash deduplication (SHA256+MinHash) | 10-20% | 5-10% | 1 |
| Regex code structure extraction | 15-30% | 5-10% | 1 |
| Delta encoding | 5-15% | 3-5% | 1 |
| Tool-result classification + compression | 15-30% | 8-15% | 1 |
| Success short-circuit | 3-5% | 2-3% | 1 |
| Image base64 replacement | 95%+ per image | 2-8% overall | 1 |
| Repeated tool collapse | 3-5% | 2-3% | 1 |
| Conversation graph pruning | 5-10% | 3-5% | 1 |
| MiniMax summarization | 40-60% | 15-25% | 2 |
| Adaptive sliding window | 3-8% | 2-4% | 2 |
| Prompt cache optimization | 50-90% cost | 20-40% cost | 3 |
| Response caching | 100% per hit | 3-8% overall | 3 |
| **Combined total (all layers)** | - | **85-90%** | **0+1+2+3** |

Note: "combined" accounts for overlap between techniques AND the multiplicative
cascade effect (Layer 0 output makes Layer 1 dedup/delta more effective).
The 85-90% figure is for long sessions (30+ messages). Short sessions achieve 70-85%.

## Appendix B: Usage Capacity Model (Subscription Plans)

### Proxy operating overhead

| Component | Cost/month | Resource Impact |
|---|---|---|
| MiniMax API calls | $0 | Included in existing Pro Plan (unlimited) |
| SQLite tracking DB | $0 | ~5MB after 90 days |
| Compute (local Mac) | $0 | <200MB RAM, negligible CPU |
| Storage (analytics) | $0 | ~1MB/day JSONL |

### Effective capacity multiplier

At 87% average compression (Layer 0 + Layers 1-3), subscription rate limits expand:

| Scenario | Messages/day (raw) | Messages/day (combined) | Multiplier |
|---|---|---|---|
| Light use | ~30 before limit | ~200+ before limit | **7x** |
| Medium use | ~80 before limit | ~530+ before limit | **7x** |
| Heavy use | ~150 before limit | ~1000+ before limit | **7x** |

### Additional tangible benefits

| Benefit | Proxy only (L1-3) | Combined (L0+L1-3) |
|---|---|---|
| Time-to-First-Token | 30-40% faster | 60-80% faster |
| /compact frequency | 3x less often | rarely needed |
| Session length | 3x more messages | 7x more messages |
| Response quality | Better (less noise) | Significantly better (clean context) |
| Debugging speed | Same | Faster (AI-optimized debug system) |

## Appendix C: Risk Assessment

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Summary loses critical context | Low | High | Anchor points + validation + conservative ratios |
| MiniMax API outage | Low | Low | Graceful degradation to Layer 1 only |
| Regex extraction failure | Medium | Low | Per-file fallback to uncompressed |
| API format change (Anthropic/OpenAI) | Low | Medium | Version-pinned parsing, passthrough on parse error |
| Token count inaccuracy | Medium | Low | tiktoken approximation is within 5%, stats are estimates anyway |
| Secret detection false positive | Medium | Low | Allowlist, warn mode, easy to disable per-pattern |
| Proxy crash | Very Low | Medium | Panic recovery, daemon auto-restart, stateless design |
| Filter misclassification | Medium | Low | Fallback to generic ANSI strip, never worse than raw |
| Hook script tampered | Very Low | Medium | SHA-256 integrity verification on every invocation |
| SQLite tracking DB corruption | Low | Low | Auto-recreate on open failure, analytics are non-critical |

## Appendix D: Drawback Analysis

### Layer 0 Drawbacks

| Drawback | Severity | Mitigation |
|---|---|---|
| Filter removes critical stacktrace | MODERATE | Failure-focus strategy: failed tests shown in FULL, only passed tests reduced. Tee system saves raw output for recovery. |
| Hook installs incorrectly | LOW | `tokenproxy hook verify` checks SHA-256. `tokenproxy hook status` shows active hooks. |
| Command rewriting changes semantics | LOW | Rewriting only adds `tokenproxy filter` prefix. Original command runs unchanged inside. If filter errors: raw output passthrough. |
| Unknown command not filtered | ZERO | Passthrough with generic ANSI strip. Never blocks execution. |
| Filter slows down command | NEGLIGIBLE | All filters are regex/string ops, <5ms overhead. Subprocess execution is the bottleneck (unchanged). |

### Layer 1 Drawbacks

| Drawback | Severity | Mitigation |
|---|---|---|
| SHA256 dedup misses near-duplicates | LOW | MinHash/LSH near-duplicate detection catches similarity >0.85. |
| Code structure extraction loses implementation details | LOW | Only on OLD messages. Model has already processed the full code. Can re-read via tool call if needed. |
| Comment stripping removes important TODO | MINIMAL | Only tool_result content in old messages, never user/assistant messages. |
| Success short-circuit false positive | LOW | "unless" conditions prevent short-circuit when errors/warnings present. |
| Image text extraction loses visual layout | LOW | Terminal screenshot text preserved. Non-terminal images reduced to dimensions only. Old images are rarely re-referenced. |
| Graph pruning removes referenced message | LOW | Safety check: scan all later messages for index references before pruning. Skip if any reference found. |

### Layer 2 Drawbacks

| Drawback | Severity | Mitigation |
|---|---|---|
| MiniMax summary loses a detail | MODERATE | 5-check validation (paths >90%, functions >80%, errors >50%, length bounds). On failure: discard summary, use Layer 1 only. |
| MiniMax API unavailable | ZERO IMPACT | Graceful degradation: Layer 2 skipped entirely. Other layers continue. |
| MiniMax costs money | LOW | ~10 RPM, M2.7 is inexpensive. Cost is orders of magnitude less than savings. |
| Summary not 100% deterministic | LOW | Temperature 0.1 produces near-identical summaries. Validation catches divergent outputs. |
| Adaptive window too aggressive | LOW | Minimum window: 3. Maximum shrink: -2 from base. Complex sessions get MORE protection, not less. |

### Provider Detection Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Provider detects proxy | PRACTICALLY ZERO | Only messages array changed. Identical to user using /compact. No headers, no fingerprint, no User-Agent change. |
| Account ban risk | ZERO | Shorter history is DESIRED by providers (less compute cost for them). Anthropic explicitly provides prompt caching for this purpose. /compact is a built-in feature. |
| API format changes break proxy | LOW | Version-pinned parsing. On parse error: passthrough unmodified. |

### Quality / Intelligence / Memory Impact

| Aspect | Impact | Explanation |
|---|---|---|
| Current context quality | UNCHANGED | Sliding window protects last 3-7 exchanges. Never compressed. |
| Long-term memory | MINIMAL DEGRADATION | Summaries preserve key facts. Anchor system protects edits/errors/decisions. Equivalent to what happens with /compact. |
| Code understanding | UNCHANGED for recent, REDUCED for old | Recent code: full fidelity. Old code: structural summary (signatures/types). Model can re-read if needed. |
| Reasoning quality | IMPROVED | Less noise = better focus. "Lost in the Middle" effect reduced. Clean context leads to better decisions. |
| Response speed | IMPROVED | 2-4x faster TTFT due to shorter prefill. |

## Appendix E: Synergy Cascade Effects

The combination of Layer 0 + Layers 1-3 creates multiplicative synergies where
each layer amplifies the effectiveness of subsequent layers.

### Cascade Chain

```
1. Layer 0 pre-filters tool output at entry time
   -> Tool results are smaller when they enter the conversation
   |
   v
2. Smaller tool results = MORE exact dedup hits in Layer 1
   -> ANSI codes already stripped by Layer 0 -> byte-identical outputs
   -> Same command produces same filtered output -> SHA256 exact match
   |
   v
3. Smaller tool results = more precise delta encoding
   -> Comment/whitespace noise already removed -> deltas show only real changes
   -> Diffs are shorter and more meaningful
   |
   v
4. Smaller tool results = better MiniMax summaries in Layer 2
   -> Less noise for the summarization model to process
   -> Summaries are more accurate (less irrelevant content to filter)
   -> MiniMax processes faster (less input tokens)
   -> MiniMax costs less per call
   |
   v
5. Deterministic filtered output = more stable cache keys
   -> Response cache hit rate increases
   -> Anthropic prompt cache prefix is LONGER (more stable content)
   -> More cached prefix = cheaper API calls (10% of normal price)
   |
   v
6. All layers combined: 85-90% savings vs 67% with proxy alone
   -> 7x message capacity vs 3x with proxy alone
   -> 2-4x faster TTFT vs 30-40% faster with proxy alone
```

### Quantified Synergy Gains

| Synergy | Mechanism | Additional savings |
|---|---|---|
| Dedup amplification | Layer 0 deterministic output -> more exact matches | +3-5% |
| Delta precision | Pre-stripped comments -> cleaner diffs | +1-2% |
| MiniMax quality | Less noise input -> better summaries | +2-3% quality, not tokens |
| Cache hit rate | Stable filtered output -> more cache hits | +2-4% |
| Prompt cache extension | Longer stable prefix -> more cached tokens | 10-30% cost reduction |
| **Total synergy bonus** | | **+8-14% additional savings** |

These synergy gains explain why the combined system achieves 85-90% savings
rather than the naive expectation of ~77% (1 - (1-0.48) * (1-0.67)) = 83%.
The synergies push it above the theoretical non-interactive combination.

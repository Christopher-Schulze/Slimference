# TASK 138: Session/turn ownership spine for AST and cross-tool state

Status: IN PROGRESS (core + hook-backed edit/read gates + T125 body recovery + T126 mini hot path landed; TUI/debug convergence still open)
Priority: P1
Scope: `internal/sessions/`, `internal/proxy/`, `internal/hooks/`, `internal/filter/`, `internal/readcache/`, `internal/codecompact/`, `internal/crosstool/`, `internal/toolarchive/`, `internal/quality/`, `internal/tui/`.

## Why

Several features are intentionally conservative because the code lacks one authoritative session/turn state owner:

- T125 AST compaction had `RecentlyEdited` and mode gates, but the hot path called it with static `Mode: "scan"` before the 2026-05-13 hook-backed context pass.
- T125 body-on-demand cannot be honest without request/session ownership or an archive-backed original body source.
- T126 crosstool dedup needs session/turn ownership; the standalone filter wrapper still lacks it, while Codex PostToolUse now has it through file-backed hook state.
- Read cache, repetition store, tool archive, quality signals, prompt cache, and Layer 2 session IDs all infer state independently.

The fix is not to wire global state into filters. The fix is a small, explicit session/turn spine that all these systems can consult.

## Target State

One `SessionTurnState` service owns:

- session id
- turn id
- request id
- client family
- current cwd/workspace
- tool calls seen this turn
- files read this turn
- files edited this turn
- archived tool outputs
- git path lists shown this turn
- last provider response id
- prompt-cache prefix hash
- quality events

It is request-scoped where possible and process-scoped only behind explicit session ids.

## Work Packages

### WP1 - Data model

- Add `internal/sessions/turn_state.go`.
- Types:
  - `SessionKey`
  - `TurnKey`
  - `ToolObservation`
  - `FileObservation`
  - `GitPathListObservation`
  - `EditObservation`
  - `PromptPrefixObservation`
  - `TurnSnapshot`
- Hard cap memory by sessions, turns, and age.
- No raw large content stored by default; use hashes and archive ids.

### WP2 - Boundary sources

- Proxy:
  - derive session from provider-specific ids and headers.
  - derive turn from request sequence if no hook turn_id.
- Hooks:
  - `SessionStart` resets/creates session.
  - `UserPromptSubmit` starts a new turn.
  - `PreToolUse` registers planned tool call.
  - `PostToolUse` registers output and result.
  - `Stop` closes turn.
- Fallback:
  - if no session id, use bounded anonymous request state only.

### WP3 - T125 integration

- `TryStripCommentsFileRead` / `compactSingleFileRead` receives `FileReadContext`.
- If file was edited in current or previous N turns, disable AST skeletoning.
- If tool intent is debug/edit, disable skeletoning.
- If request is orientation/scan, enable skeletoning.
- Body-on-demand:
  - AST output must include stable body ids.
  - A later explicit expand/read request can retrieve exact function body from archive or original file.
  - Never fake a body-on-demand protocol in plain text without retrieval support.

### WP4 - T126 integration

- Use `SessionTurnState` for git path-list observations.
- Only elide exact git name-only metadata when:
  - same turn
  - same session
  - same cwd
  - same exact sorted path fingerprint
  - output is pure metadata, not diff body.
- Reset automatically on new user turn.
- Track net saving and any model re-read/recovery penalty.

### WP5 - Read cache/tool archive/repetition convergence

- Replace ad hoc session id extraction where practical.
- Keep storage packages independent, but thread `SessionTurnState` keys through them.
- Ensure local-archive URIs map to session + turn for debug.

### WP6 - TUI/debug

- Add session/turn debug view:
  - current session
  - current turn
  - files read/edited
  - tools seen
  - crosstool state
  - AST compaction decisions
  - body-on-demand archive ids

### WP7 - Tests

- Pure unit tests for state lifecycle.
- Race tests for concurrent proxy requests.
- Hook event sequence tests.
- T125 edit-mode gating tests.
- T126 hot-path tests with turn reset.

## Acceptance

- [x] T125 edit-mode gating is backed by real hook session/turn state for Codex hook/PostToolUse paths.
- [x] T125 body-on-demand has a real retrieval path or stays disabled.
- [x] T126 hot-path integration is safe and per-turn only for Codex PostToolUse.
- [ ] Read cache, repetition, tool archive, quality, and proxy share compatible session/turn keys.
- [ ] TUI/debug can show why a file/tool output was compacted or left literal.
- [x] `go test -race ./...` passes.
- [x] `go run ./scripts/ci` passes.

## Notes

- This task intentionally avoids global magical state. If the session/turn id is unknown, features must degrade to passthrough.
- 2026-05-13 core implementation:
  - `internal/sessions/turn_state.go` adds the bounded `TurnStateStore` plus `SessionKey`, `TurnKey`, `ToolObservation`, `FileObservation`, `GitPathListObservation`, `PromptPrefixObservation`, and `TurnSnapshot`.
  - The store tracks current turn, tools, files read, files edited, git path-list fingerprints, prompt-prefix hashes, response ids, and quality event counts without storing raw large content.
  - Memory is capped by session count, turn count, and age. Empty session ids degrade to bounded `anonymous` state instead of global unbounded state.
  - `RecentlyEdited(session,path,N)` is now a real signal T125 can use for edit-mode gating once the hook/proxy paths thread session ids through it.
  - Git path-list fingerprints are exact, sorted, de-duplicated path hashes suitable for the T126 mini hot path.
  - Package tests include lifecycle, caps/eviction, duplicate suppression, concurrent observation safety, and fingerprint determinism at 100% statement coverage.
- 2026-05-13 hook-backed context implementation:
  - `internal/sessions/hook_state.go` adds a file-backed, lock-protected hook turn-state adapter under `~/.slimference/turn-state/` so separate Codex hook processes can share session/turn/read/edit observations.
  - `SessionStart`, `UserPromptSubmit`, `Stop`, `ReadHook`, and `PostToolUse` now best-effort record turn boundaries, file reads, tool events, and edit paths.
  - `internal/filter.FileReadContext` is wired through `CompactCapturedOutputWithContext`, `applyLayer0FiltersWithContext`, and `TryStripCommentsFileReadWithContext`.
  - Recently-edited, force-full, edit-mode, and debug-mode reads return literal output instead of AST skeletons, signature extraction, or comment stripping.
  - Hook state is intentionally file-backed rather than only in-memory because Codex invokes hooks as separate processes.
  - MiniMax/OpenAI-compatible Layer 2 provider knobs were hardened during the same stabilisation pass: direct `SLIMFERENCE_MINIMAX_API_KEY`, base URL/model/key-env overrides, honoured `temperature`/`top_p`, default MiniMax `reasoning_split`, and Rust summary examples.
- 2026-05-14 T126 mini hot-path implementation:
  - `HookTurnState` persists git path-list fingerprints per current turn under `~/.slimference/turn-state/`.
  - Codex `PostToolUse` observes raw `git status` path lists and elides only later `git diff --name-only` output with the same session, same turn, same CWD, and same exact sorted path fingerprint.
  - The marker is explicit (`[Slimference: N git paths already shown by previous ...]`) and no diff hunks, name-status tables, git ls-files output, non-git output, or standalone `slimference filter` runs are touched.
  - Tests cover file-backed repeat detection, CWD separation, cap enforcement, command detection, marker escaping, and full PostToolUse JSON output.
- 2026-05-14 T125 body-on-demand implementation:
  - Large AST-compacted reads are already archived by `PostToolUse` with the raw original output and the compacted preview.
  - `toolarchive.RenderContext` adds a concrete recovery command for AST previews: `slimference expand-body <archive-id> <symbol>`.
  - `expand-body` extracts Go function/method declarations from the archived original, supporting `Func`, `Type.Method`, and `(*Type).Method` symbols.
  - This is intentionally not wired through unsupported `updatedInput` mutation. The agent must run the explicit command when it needs an omitted body.
- Open boundaries:
  - T126 still needs live corpus proof before any broader command family or dedicated `gain --crosstool` report.
  - TUI/debug still needs a compact session/turn state view.
  - Read cache, repetition, tool archive, quality, and proxy still need a single visible session-key story.

# T39 - Smart Compaction via Progressive Checkpoints + Ranked Restore

Status: done
Priority: high
Scope: `internal/checkpoints`, `internal/hooks`, `internal/analytics`, `internal/proxy`, `internal/tui`, `cmd/slimference`
Source repo: `repos/token-optimizer`

---

## Problem

Slimference is strong at:

- request-time compression
- deterministic Layer 0 and Layer 1 reduction
- summarization
- read-hook blocking and delta feedback

But it still has no dedicated session-continuity layer for compaction events.

That leaves one real gap:

- a long session can still lose critical state when the upstream client compacts
- there is no Slimference-native checkpoint capture
- there is no ranked restore path that can recover the best saved state later

The second deep pass over `repos/token-optimizer` showed that this is the only
remaining high-value import from that repo beyond the already-landed `Read`
cache work.

---

## Reality-Checked Extraction from the Foreign Repo

Relevant upstream references:

- `repos/token-optimizer/openclaw/src/checkpoint-policy.ts`
- `repos/token-optimizer/openclaw/src/smart-compact.ts`
- `repos/token-optimizer/skills/token-optimizer/scripts/measure.py`

Ideas worth importing:

1. progressive checkpoint capture instead of "only when disaster already hit"
2. triggers on context-fill and quality thresholds
3. deterministic extraction, no LLM call in the checkpoint path
4. restore the best eligible checkpoint, not blindly the newest

Ideas explicitly not imported:

1. `dynamic_compact_instructions`
2. Claude-specific session glue from `measure.py`
3. broad heuristic coaching / nudge logic
4. OpenClaw-specific event wiring

---

## Why This Is Worth Doing

This is not primarily a "save 5% more per request" feature.

It matters because it preserves the value of all existing savings features:

- compressed sessions still need survivable state
- repeated debugging work is cheaper only if important decisions survive
- read-cache / delta / prompt-cache wins are weakened if compaction erases the
  thread's operational memory

So the actual benefit is:

- higher continuity across long sessions
- lower rebuild cost after compaction or crash
- more reliable continuation for Claude/Codex workflows

---

## Scope Boundaries

This task must stay disciplined.

In scope:

- deterministic local checkpoints
- progressive trigger policy
- ranked restore selection
- CLI/admin/TUI visibility
- hook integration only where the runtime really supports it

Out of scope:

- LLM-generated checkpoints
- auto-injection of large history blobs
- full transcript replay
- heuristic "smart advice" on top
- any coupling into the proxy fast path

---

## Desired End State

### Phase 1 - Checkpoint core

- Slimference can save deterministic checkpoints under its own storage path
- checkpoint content is compact and structured
- checkpoint metadata includes trigger, timestamps, and ranking inputs

### Phase 2 - Progressive trigger policy

- checkpoint capture can fire on context-fill thresholds
- checkpoint capture can fire on quality-threshold crossings
- the threshold state is one-shot and cooldown-protected

### Phase 3 - Ranked restore

- Slimference can list and restore checkpoints for a session/project
- restore selection prefers the richest eligible checkpoint, not just the latest
- restore output is bounded and attributable

### Phase 4 - Operator visibility

- admin status exposes checkpoint counters and recent trigger info
- TUI shows continuity health and recent checkpoint activity
- CLI has explicit inspection and restore commands

---

## Proposed Slimference Shape

### New package family

- `internal/checkpoints`
- `internal/checkpoints/store.go`
- `internal/checkpoints/policy.go`
- `internal/checkpoints/extract.go`
- `internal/checkpoints/restore.go`
- `internal/checkpoints/types.go`

Why a separate package:

- this is continuity infrastructure, not request compression
- it must stay isolated from `internal/proxy` hot-path logic

### Storage shape

Root:

- `~/.slimference/checkpoints/`

Per session or project:

- compact markdown or text artifact with stable sections
- manifest / metadata file
- small persisted policy state for threshold one-shots and cooldowns

### Capture input

The extractor should use only existing local evidence:

- recent request metrics already logged by Slimference
- session/debug traces already owned by Slimference
- known file-touch / read-cache / loop / overflow signals where available

The extractor must not call an LLM.

### Checkpoint body format

First pass should be deterministic and compact:

- user constraints / instructions
- key decisions
- notable errors
- touched files or working areas
- recent steps
- next-step breadcrumb

This should be closer to a structured operational checkpoint than a transcript
dump.

### Trigger policy

Initial imported trigger set:

- fill thresholds: `20`, `35`, `50`, `65`, `80`
- quality thresholds: `80`, `70`, `50`, `40`

First-pass policy should explicitly skip:

- pre-fanout milestones
- edit-burst milestones

Those can be added later if the base system proves useful.

### Restore ranking

Ranking should be deterministic:

1. trigger priority
2. semantic richness score
3. recency

This is the key imported idea.

Do not just "take newest".

### CLI surface

Initial commands:

- `slimference checkpoint capture`
- `slimference checkpoint list`
- `slimference checkpoint restore`
- `slimference checkpoint stats`

Hidden transport helpers if needed:

- `slimference checkpointhook capture`
- `slimference checkpointhook restore`

### Hook integration strategy

This part must respect reality.

Today Slimference already installs:

- Claude `PreToolUse` hooks for `Bash` and `Read`
- Codex `PreToolUse` / `PostToolUse` hooks for `Bash` and `Read`

It does **not** yet have a verified cross-client lifecycle hook surface for:

- `SessionStart`
- `SessionEnd`
- `Stop`
- `PreCompact`

So the correct integration plan is:

1. build the continuity core first
2. wire lifecycle hooks only where upstream support is confirmed
3. keep an explicit CLI/manual path as fallback

That keeps the implementation real instead of speculative.

---

## Implementation Plan

### WP1 - Checkpoint data model and local store

- define checkpoint record and manifest schema
- add deterministic storage layout under `~/.slimference/checkpoints/`
- add retention / pruning rules from day one

### WP2 - Deterministic extractor

- build a compact extractor from existing local Slimference evidence
- no transcript-wide raw dump
- no LLM summarization
- output stable sectioned text

### WP3 - Trigger policy

Completed implementation:

- `internal/checkpoints` now persists deterministic checkpoint artifacts under
  `~/.slimference/checkpoints/`
- auto-capture runs from the async analytics worker, not from the request hot
  path
- current real triggers are `overflow`, `pressure`, `fill`, `low_savings`,
  plus manual capture via CLI
- `slimference checkpoint capture|list|restore|stats` is implemented
- admin/TUI now expose checkpoint counts, last trigger, restore count, and
  storage size
- ranked restore prefers higher-value checkpoints instead of blindly taking the
  newest item

Verification:

- `go test ./internal/checkpoints ./internal/proxy ./cmd/slimference ./internal/tui`
- `go test ./...`

- add fill-threshold and quality-threshold bookkeeping
- add cooldown and one-shot guards
- persist policy state so restarts do not re-fire the same checkpoints

### WP4 - Ranked restore

- implement checkpoint enumeration and ranking
- restore bounded content only
- include trigger and timestamp attribution in the restored output

### WP5 - CLI surface

- add explicit user-facing checkpoint commands
- add hidden hook transport commands only if needed
- make the CLI usable even before lifecycle hooks are wired

### WP6 - Hook integration

- verify real lifecycle hook support per client before wiring anything
- if unsupported, do not fake the lifecycle through proxy request interception
- add install/verify/remove support only for the hook paths that are truly supported

### WP7 - Admin/TUI visibility

- checkpoint counters
- last trigger
- stored bytes
- recent restore info

### WP8 - Tests

- storage round-trips
- trigger one-shot logic
- cooldown behavior
- ranked restore selection
- retention cleanup
- CLI behavior
- admin/TUI snapshot surfaces

---

## Subtasks

- [x] Define checkpoint types, manifest schema, and retention model.
- [x] Add `internal/checkpoints` package skeleton with deterministic local storage.
- [x] Implement deterministic checkpoint extraction from existing Slimference evidence.
- [x] Implement threshold policy for fill and quality bands.
- [x] Implement ranked checkpoint restore selection.
- [x] Add `slimference checkpoint capture|list|restore|stats`.
- [x] Add admin snapshot fields for checkpoint health.
- [x] Add TUI visibility for checkpoint status and recent activity.
- [x] Verify lifecycle hook feasibility before wiring install/remove logic.
- [x] Add targeted tests and keep `go test ./...` green.

---

## Risks

- upstream clients may not expose the lifecycle hooks we want
- bad checkpoint ranking can restore the wrong state
- oversized checkpoints can defeat the point of compaction
- weak extraction can capture noise instead of durable state

Mitigations:

- CLI-first core before hook integration
- strict size caps
- deterministic extraction only
- ranking tests with explicit trigger precedence

---

## Acceptance Criteria

- [x] Slimference can save deterministic checkpoints locally without any LLM call.
- [x] Fill and quality thresholds can trigger one-shot checkpoint capture.
- [x] Restore selects the best eligible checkpoint, not just the newest file.
- [x] Checkpoint storage is bounded, inspectable, and pruneable.
- [x] CLI/admin/TUI surfaces expose checkpoint state clearly.
- [x] No checkpoint logic is added to the proxy hot path.

---

## Current Recommendation

This is the highest-value remaining import from `repos/token-optimizer`.

Implementation order should be:

1. storage + extractor
2. ranking + restore
3. CLI
4. visibility
5. hook integration only after real lifecycle-hook verification

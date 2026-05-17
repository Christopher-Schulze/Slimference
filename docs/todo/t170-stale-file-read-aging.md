# TASK 170: Stale-file-read aging (input-token reclamation)

Status: TODO (planning 2026-05-16)
Priority: P1
Scope: `internal/readcache/`, `internal/proxy/handler.go`, `internal/sessions/`, `internal/types/`

## Why

In long iterative sessions the model reads a file early on, then the file content sits in conversation history forever — even when the file has been modified since OR the model never references the read again. The full file content keeps inflating every request's input tokens for the rest of the session.

Track file-read freshness: if a read result is N turns old AND not referenced since AND the underlying file is unchanged → replace with a hash-marker. If the file changed → replace with `[file modified since this read; current state available via fresh read]`. Either way: tokens reclaimed.

**Why:** Long sessions accumulate up to 30-50% of input tokens in stale file-read results. Reclaiming them is pure win — the content is either unchanged (and thus retrievable from local cache) or stale (and thus wrong to keep).
**How to apply:** Walk conversation history, identify `tool_result` blocks from file-read tools (`Read`, `cat`, `head`, etc.), check last-reference + last-mtime, replace with marker.

## Target State

1. Per-session "read aging" tracker in `internal/readcache/`:
   - `RecordRead(sessionID, path, content) hash`
   - `LastReferenced(sessionID, hash) int` (turn index of last assistant reference)
   - `IsStale(sessionID, hash, currentTurn, mtime time.Time) bool`
2. In `internal/proxy/handler.go`, after extractMessages: walk history, find file-read tool_result blocks, decide:
   - **Not stale**: keep verbatim
   - **Stale, file unchanged**: replace with `[file unchanged since turn N: hash=<8 chars>]`. Hash lets readcache restore on demand.
   - **Stale, file changed**: replace with `[file modified after turn N; re-read for current state]`
3. Telemetry: tokens-saved-via-aging.
4. Configurable: `[compression.read_cache] aging_threshold_turns = 5` (default), `aging_enabled = true`.

## Acceptance

- Session with 10 turns, each turn does `Read src/main.go` (unchanged file): turns 1-5 are aged out, only turn 5+ retains full content; on turn 11 if a reference to that content is needed, readcache rehydrates.
- File modified mid-session: aged-out marker carries "modified" annotation so model knows to re-read.
- No false positives on `Bash` tool outputs (those aren't file reads).
- 100% coverage on aging logic.

## Sub-Tasks

- [ ] Identify all file-read tool families (Codex `Read`; Claude `Read`/`Bash(cat)`).
- [ ] Reference-tracker: when does an assistant turn "reference" an earlier read? (name-mention, content-quote)
- [ ] Aging policy: turn-distance + reference-recency.
- [ ] Marker format + rehydration path.
- [ ] Tests: aging-out, hash-rehydration, modification-detection.

## Notes

- This is **input-side** reclamation; combines with output-side reductions (t165-t168) for stacked wins.
- Marker must be small (~50 chars) so the saving is real.
- Risk: false aging when model relies on stale-marked content the next turn. Mitigation: store the hash in the marker; rehydration is cheap.

## Deviations

(none yet)

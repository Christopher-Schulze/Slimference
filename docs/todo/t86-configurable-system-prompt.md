# TASK 86: Configurable + versioned compression system prompt

Status: todo
Priority: P1
Scope: `internal/summarization/minimax.go`, `internal/config/`, `internal/debug/decisions.go`, `docs/`
Driver: The MiniMax system prompt is a hardcoded `const` (~800 tokens). Tweaking it requires a code change + redeploy. There is no A/B slot, no version field in telemetry, and every call pays the full prompt cost. This makes prompt iteration slow and savings/quality untrackable across versions.

---

## Problem

`internal/summarization/minimax.go::systemPrompt` is a code-baked string. Changing it loses git history of "which prompt was active when these summaries were made", forces a redeploy, and gives no way to compare two prompts on the same traffic. Every summarization call sends the full prompt as input even though MiniMax may not cache it server-side.

## Target State

- Prompt content lives in a file under `~/.slimference/prompts/<id>.txt` (or a configurable path), not in code.
- Config knob `[summarization.prompt] active_id = "...."` selects the active prompt; `template_paths = [...]` lists known prompt files.
- File-watcher hot-reloads on change; the running process picks up new prompts at the next call.
- Each prompt file carries a header comment with `version: <semver>`; the active version is recorded in `RequestSummary.prompt_version` and surfaced in analytics so savings/quality can be sliced by prompt version.
- The current hardcoded string ships as `prompts/v1.txt` so behaviour is unchanged on first run.
- A/B slot: `[summarization.prompt] ab_id = "..." + ab_traffic_share = 0.10` routes a configurable share of traffic to a candidate prompt for comparison.

## Implementation Plan

### WP1 - Prompt loader + watcher
- New `internal/summarization/promptstore.go` reads files, extracts `version:` header, watches for changes.

### WP2 - MiniMax client integration
- `MiniMaxClient` takes a `PromptStore` instead of the const.
- Active prompt picked per call (with A/B routing if configured).

### WP3 - Telemetry
- `RequestSummary.prompt_version`, `RequestSummary.prompt_id`.
- `/admin/status.summarization` exposes active id + ab id + traffic share.

### WP4 - First-run setup
- On first daemon start, the existing const is materialised to `~/.slimference/prompts/v1.txt` if no file exists.
- `slimference doctor` verifies the active prompt path exists and is readable.

### WP5 - Docs
- New section in `docs/documentation.md` "Compression prompt iteration" with the workflow for editing and versioning prompts.

## Acceptance Criteria

- [ ] Active prompt is loaded from file, not from code.
- [ ] Editing the active file is picked up at next call without restart.
- [ ] `RequestSummary` records the prompt version.
- [ ] A/B routing routes the configured share of traffic to the candidate prompt.
- [ ] Migration: first run materialises the legacy prompt verbatim.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Centralised prompt registry across machines.
- Auto-promotion of A/B candidates based on quality scores (T-future).

## Validation

```
slimference doctor
echo "version: v2" > ~/.slimference/prompts/v2.txt
# active prompt switch via config edit; daemon picks it up live
```

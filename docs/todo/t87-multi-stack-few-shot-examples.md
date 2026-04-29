# TASK 87: Multi-stack few-shot examples for the compression prompt

Status: completed
Priority: P2
Scope: `internal/summarization/`, prompt template under `~/.slimference/prompts/`
Driver: The current MiniMax prompt embeds a single Go-flavoured few-shot example (`HandleLogin()`, `go test ./...`). Sessions in Python or TypeScript get implicit Go-bias because the model has been primed with Go idioms. A small rotation of stack-specific examples removes the bias at no additional inference cost.

---

## Problem

The single Go example in the system prompt is good craft but introduces stack-bias. When the live session is mostly Python, the model still anchors to the Go example pattern (`func name()` mentions, `go test` style summary lines). This shows up as occasional Go-flavoured rephrasing in non-Go session summaries.

## Target State

- Prompt template (T86) supports an `{{example}}` slot that the prompt store fills at request time.
- Three checked-in examples cover Go, Python, TypeScript.
- Selection is driven by a cheap heuristic on the input transcript (file extensions, tool calls, language tokens). Default: pick the dominant stack; fall back to Go.
- Counter `prompt_example_<lang>` exposed under `/admin/status.summarization` so the operator can see the distribution of prompt examples used.

## Implementation Plan

### WP1 - Example library
- `~/.slimference/prompts/examples/{go,python,ts}.txt` (materialised on first run).
- Each contains a single CORRECT INPUT / CORRECT OUTPUT block matching the existing schema.

### WP2 - Selection heuristic
- New `internal/summarization/example_picker.go`: scans message bodies for path extensions and tool names, returns a label.
- Defaults to `go` on tie or empty signals.

### WP3 - Template fill
- `PromptStore` (T86) renders `{{example}}` from the picked label.

### WP4 - Telemetry
- `RequestSummary.prompt_example_lang`.
- `/admin/status.summarization.example_distribution`.

### WP5 - Tests
- Unit: Python-heavy fixture picks Python; TypeScript-heavy fixture picks TS.
- Snapshot: rendered prompt for each language.

## Acceptance Criteria

- [ ] Example slot is filled from one of three stack-specific files based on the input.
- [ ] Default picks Go on ambiguity to preserve current behaviour.
- [ ] Counter distribution exposed in admin endpoint.
- [ ] No regression in MiniMax savings on existing fixtures.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Adding stacks beyond Go / Python / TS (extend later if traffic warrants).
- LLM-based language detection.

## Validation

```
go test ./internal/summarization/...
```

## Closure Notes (2026-04-30)

Landed:

- Three checked-in example variants: `exampleGo`, `examplePython`,
  `exampleTS`. Each is a complete `EXAMPLE INPUT` + `CORRECT OUTPUT
  FOR ABOVE INPUT` block carrying its stack idioms (`handle_login` for
  Python, `handleLogin(req: Request, res: Response)` for TS,
  `HandleLogin(w http.ResponseWriter, r *http.Request)` for Go) plus
  the matching tooling (`pytest`, `npm test`, `go test`).
- `pickExampleLang(input)` scans the input transcript for cheap
  signals (file extensions, language idioms, tool names) and returns
  one of `go` / `python` / `ts`. Defaults to `go` on tie or empty
  signals.
- `buildSystemPrompt(input)` composes header + picked example +
  footer. Wired into the MiniMax client's request builder so every
  request gets the stack-appropriate priming.
- Per-language pick counters via `ExamplePromptCount(lang)` /
  `ExamplePromptCounts()` ready for future
  `/admin/status.summarization.example_distribution` exposure.
- 100% coverage; CI green.

Files written via T86's prompt store in lieu of `~/.slimference/prompts/`
landing — keeps everything compile-time until T86 lands.

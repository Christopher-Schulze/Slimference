# Slimference Data Policy

Last updated: 2026-06-05 (Layer 2 removed)

## Overview

Slimference processes LLM API requests through local deterministic reducers,
cache/accounting paths, and provider-bound upstream forwarding. It no longer
uses a model-facing Layer 2 summarization, OCRL/context-ledger replacement, or
external compression provider.

## Data Flow by Layer

### Layer 0: Pre-Entry Filtering (local only)

- **What happens**: Command output (stdout/stderr) from subprocess commands is filtered locally before reaching the LLM.
- **Data destination**: Local process only. No data leaves your machine.
- **Controls**: `slimference filter`, `.slimference/filters.toml`

### Layer 1: Deterministic Compression (local only)

- **What happens**: Message content in the conversation prefix is compressed through deterministic, reversible transformations (ANSI stripping, deduplication, structure extraction, etc.).
- **Data destination**: Local process only. No data leaves your machine.
- **Controls**: `[compression] layer1_enabled`

### Retired Layer 2: no model-facing summarization

- **What happens**: Nothing. The Layer 2 code path has been removed.
- **Data destination**: No data is sent to MiniMax, a local LLM, or any other
  side-channel summarization provider by Slimference.
- **Reason**: Any summary that replaces old context can remove details and create
  product drawdown: worse model memory, weaker context consistency, or wrong
  reconstruction. Slimference keeps default product savings on deterministic,
  recoverable, and fail-open mechanisms instead.
- **Controls**: There is no Layer 2 CLI surface or config surface.

### Layer 3: Response Cache (local only)

- **What happens**: Compressed responses are cached locally to avoid redundant compression.
- **Data destination**: Local memory and disk cache only. No data leaves your machine.
- **Controls**: `[compression] layer3_enabled`, `[cache]`

### Layer 4: Output and tool-surface reduction (local policy)

- **What happens**: Safe output discipline, repetition control, and tool-schema
  pruning reduce tokens that do not carry user-visible task state.
- **Data destination**: Local process policy plus the normal upstream provider
  request. No side-channel provider is involved.
- **Controls**: Output-reduce and tool-pruning settings in the local config.

## Provider Trust Labels

| Provider | Trust Class | Role |
|----------|------------|------|
| Anthropic | `upstream_provider` | The LLM you are talking to |
| OpenAI | `upstream_provider` | The LLM you are talking to |
| Codex (ChatGPT) | `upstream_provider` | The LLM you are talking to |

## Disabling Layers

Each layer can be individually disabled in config:

```toml
[compression]
layer1_enabled = true   # deterministic compression (safe, local)
layer3_enabled = true   # response cache (safe, local)
```

## Logging and Telemetry

- Slimference logs compression decisions locally (`~/.slimference/logs/`).
- No telemetry is sent to any external service.
- `slimference debug` commands inspect local logs only.

## Further Reading

- Spec: `spec+.md` (normative)

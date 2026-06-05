# Slimference Data Policy

Last updated: 2026-06-05 (OCRL local default direction + legacy external Layer 2 opt-in)

## Overview

Slimference processes LLM API requests through a multi-layer compression pipeline. This document describes what data flows where, and how to control it.

## Data Flow by Layer

### Layer 0: Pre-Entry Filtering (local only)

- **What happens**: Command output (stdout/stderr) from subprocess commands is filtered locally before reaching the LLM.
- **Data destination**: Local process only. No data leaves your machine.
- **Controls**: `slimference filter`, `.slimference/filters.toml`

### Layer 1: Deterministic Compression (local only)

- **What happens**: Message content in the conversation prefix is compressed through deterministic, reversible transformations (ANSI stripping, deduplication, structure extraction, etc.).
- **Data destination**: Local process only. No data leaves your machine.
- **Controls**: `[compression] layer1_enabled`

### Layer 2: OCRL Context Ledger (local) + legacy summarization (external opt-in)

- **Product direction**: OCRL, the Old Context Replacement Layer, is local and
  deterministic. It builds compact archive-backed capsules for old inactive
  context and can become model-facing only when route, session, archive
  recovery, active-context, quality-pressure, and positive-token-savings gates
  all pass.
- **Data destination for OCRL**: Local process and local archive only. OCRL does
  not call an external model.
- **Current Codex WSS state**: shadow/proof only. OCRL does not insert capsules
  into Codex WSS model-facing context.
- **Legacy external summarization**: When explicitly enabled, conversation
  prefixes exceeding the token threshold may be summarized by a configured
  OpenAI-compatible LLM endpoint.
- **Data destination for legacy summarization**: Compressed conversation content
  is sent to the configured summarization provider endpoint.
- **Default state**: **Disabled** for fresh configs. Existing configs with
  `layer2_enabled = true` stay enabled, but new installs must opt in explicitly.
  Model-facing legacy summary replacement remains blocked unless
  `[compression.summary].allow_model_facing_replacement = true` is also set.
- **Redaction**: Outbound redaction is **on by default** (T109). This strips:
  - HTTP authentication headers
  - Known credential/secret patterns (API keys, tokens, passwords)
  - File paths are normalised (`<HOME>`, `<TMP>`)
  - JSON credential keys are removed
- **Strict mode**: `[compression.summary] outbound_redaction = "strict"` additionally drops all `tool_input` bodies and runs a recursive JSON sweep.
- **Controls**:
  - Enable: `slimference layer2 enable --acknowledge-data-policy`
  - Disable: `slimference layer2 disable`
  - Status: `slimference layer2 status`
  - Doctor check: `slimference doctor`

### Layer 3: Response Cache (local only)

- **What happens**: Compressed responses are cached locally to avoid redundant compression.
- **Data destination**: Local memory and disk cache only. No data leaves your machine.
- **Controls**: `[compression] layer3_enabled`, `[cache]`

## Provider Trust Labels

| Provider | Trust Class | Role |
|----------|------------|------|
| Anthropic | `upstream_provider` | The LLM you are talking to |
| OpenAI | `upstream_provider` | The LLM you are talking to |
| Codex (ChatGPT) | `upstream_provider` | The LLM you are talking to |
| MiniMax | `external_third_party` | Side-channel summarization provider |

`slimference doctor` warns when any `external_third_party` provider is enabled.

## Alternative Summarization Endpoint

The `[compression.minimax]` section name is historical. The client sends non-streaming `/v1/chat/completions` requests and can point at any OpenAI-compatible endpoint. For MiniMax M2.x, `enable_reasoning_split = true` keeps thinking content out of `message.content`. Disable it when a non-MiniMax endpoint rejects extra fields.

```toml
[compression.minimax]
base_url = "http://localhost:11434/v1"      # e.g. local/self-hosted endpoint
model = "qwen2.5:7b"
api_key_env = "LOCAL_LLM_KEY"
enable_reasoning_split = false
trust_class = "upstream_provider"          # suppress external-provider warning for self-hosted endpoints
```

When `trust_class = "upstream_provider"` is set explicitly, `slimference doctor` no longer warns about the provider being external.

Environment overrides for fast model swaps:

```bash
SLIMFERENCE_MINIMAX_BASE_URL="https://integrate.api.nvidia.com/v1"
SLIMFERENCE_MINIMAX_MODEL="nvidia/nemotron-3-super-120b-a12b"
SLIMFERENCE_MINIMAX_API_KEY_ENV="NVIDIA_API_KEY"
SLIMFERENCE_MINIMAX_ENABLE_REASONING_SPLIT=false
```

## Disabling Layers

Each layer can be individually disabled in config:

```toml
[compression]
layer1_enabled = true   # deterministic compression (safe, local)
layer2_enabled = false  # legacy external summarization remains opt-in; OCRL is local/shadow unless promoted
layer3_enabled = true   # response cache (safe, local)
```

Or via CLI:

```bash
slimference layer2 disable   # writes layer2_enabled = false to config
```

## Logging and Telemetry

- Slimference logs compression decisions locally (`~/.slimference/logs/`).
- No telemetry is sent to any external service.
- `slimference debug` commands inspect local logs only.

## Further Reading

- Redaction design: `docs/todo/t109-l2-outbound-redaction.md`
- Trust model: `docs/todo/t121-l2-default-off-and-trust-labels.md`
- OCRL product spec: `docs/ocrl.md`
- Spec: `spec+.md` (normative)

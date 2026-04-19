# Compression Tuning Inventory

Date: 2026-04-19

Current centralized knobs under `[compression.tuning]`:

| Name | Default | Location | Purpose |
| --- | --- | --- | --- |
| `incremental_overlap_threshold` | `0.70` | `internal/config/{config,defaults}.go`, `internal/summarization/layer2.go` | Fallback overlap threshold for incremental Layer 2 updates when no staircase is configured. |
| `incremental_staircase[0]` | `<=60 -> 0.70` | same | Conversation-size keyed Layer 2 overlap threshold. |
| `incremental_staircase[1]` | `<=120 -> 0.55` | same | Mid-size conversation threshold. |
| `incremental_staircase[2]` | `<=1000000 -> 0.40` | same | Long-conversation threshold. |
| `overflow_sliding_window` | `2` | `internal/proxy/handler.go` via config | Aggressive sliding window used only during overflow recovery. |
| `overflow_target_ratio` | `0.10` | `internal/proxy/handler.go` via config | Aggressive summary target ratio used only during overflow recovery. |
| `structure_in_window` | `false` | `internal/compression/{layer1,structure_in_window}.go` | Enables conservative in-window structure extraction for large `tool_result` blocks. |
| `structure_in_window_min_tokens` | `1500` | same | Minimum estimated token count before in-window structure extraction can trigger. |
| `loop_detection` | `false` | `internal/compression/layer1.go` | Enables retry-loop detection and final-user-message nudge injection. |
| `structure_preview` | `false` | `internal/compression/preview_pass.go` | Enables shape-aware previews for large structured tool outputs. |

Notes:

- Some thresholds remain intentionally hardcoded when they are implementation
  details rather than operator-facing behaviour knobs.
- The main examples are the Layer 2 absolute input cap and internal fuzzy
  matching constants. Those should only move into config if operators really
  need to tune them.

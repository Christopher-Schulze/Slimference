# Compression Tuning Inventory

Date: 2026-04-19

Current centralized knobs under `[compression.tuning]`:

| Name | Default | Location | Purpose |
| --- | --- | --- | --- |
| `overflow_sliding_window` | `2` | `internal/proxy/handler.go` via config | Aggressive sliding window used only during overflow recovery. |
| `overflow_target_ratio` | `0.10` | `internal/proxy/handler.go` via config | Aggressive target ratio used only during overflow recovery. |
| `structure_in_window` | `false` | `internal/compression/{layer1,structure_in_window}.go` | Enables conservative in-window structure extraction for large `tool_result` blocks. |
| `structure_in_window_min_tokens` | `1500` | same | Minimum estimated token count before in-window structure extraction can trigger. |
| `loop_detection` | `false` | `internal/compression/layer1.go` | Enables retry-loop detection and final-user-message nudge injection. |
| `structure_preview` | `true` | `internal/compression/preview_pass.go` | Enables archive-backed shape-aware previews for large structured tool outputs. |

Notes:

- Some thresholds remain intentionally hardcoded when they are implementation
  details rather than operator-facing behaviour knobs.
- Removed Layer 2 tuning knobs must not be reintroduced as product defaults.

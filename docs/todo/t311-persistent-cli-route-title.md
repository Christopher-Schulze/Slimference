# T311 Persistent CLI route title indicator

Status: Done.

## Why

The scoped CLI session was correctly launched through Slimference, but Codex
rewrote the terminal tab/window title after startup. That made the route
indicator effectively invisible even though the transport was active. The
indicator must be visible without patching Codex UI, model metadata, prompts, or
provider state.

## Acceptance

- Proxied `slimference codex run` writes the `[SF] <cwd> | <model>` terminal
  title while the proxied process is active, for example
  `[SF] ~/CODE/Slimference | gpt-5.5 medium`.
- The title indicator is refreshed during the session so Codex title rewrites
  cannot permanently hide it.
- Direct mode and direct fallback do not write the Slimference title.
- Restore is idempotent and writes the neutral title only after the keepalive
  loop has stopped.
- Tests prove repeated active writes and a final reset write.
- Docs explain that the indicator is a terminal-title keepalive, not a Codex UI
  or model change.

## Sub-Tasks

- [x] Replace one-shot title write with a scoped keepalive loop.
- [x] Stop the keepalive before writing the reset title.
- [x] Keep the existing direct-mode no-indicator behavior.
- [x] Add regression coverage for repeated active title writes and reset order.
- [x] Update install and technical documentation.

## Notes

- This uses terminal OSC title sequences only.
- Codex can still render its own in-terminal prompt text unchanged; the visible
  Slimference marker is the macOS terminal tab/window title.
- The keepalive runs only inside proxied `slimference codex run` and stops when
  that process returns.
- The title model is resolved from explicit `--model`/`-m`, `CODEX_MODEL`, then
  top-level `~/.codex/config.toml` `model` plus reasoning effort when present.

## Deviations

None.

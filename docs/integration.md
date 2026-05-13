# Slimference Integration Guide

This is the operator reference for explicit client integration modes. The
default Codex CLI/App product path is transparent mode (`slimference proxy
install|enable`): local CA + daemon + macOS HTTPS proxy, with no Codex config
mutation. `slimference integrate` remains the legacy/config-patch path for
operators who explicitly want client config edits.

## What integration does

Running `slimference integrate install` performs explicit config-patch wiring
in a single idempotent operation:

| Client / Surface | Wire point                                  | Marker file                           |
|------------------|---------------------------------------------|---------------------------------------|
| Claude Code      | `export ANTHROPIC_BASE_URL=http://127.0.0.1:8990` | block in `~/.zshrc` / `.bashrc` / fish |
| Codex            | `openai_base_url` + `chatgpt_base_url`      | block in `~/.codex/config.toml`       |
| Hooks            | Optional Codex lifecycle + tool hooks       | `~/.codex/hooks.json` + `~/.slimference/hooks/*.sh` |

Every edit uses fenced marker comments so re-running install is a no-op and
`slimference integrate remove` removes exactly what was added. On first touch
of any file that already exists, a timestamped backup is saved next to it
(`.slim-backup-<ts>`).

## Quick start

```bash
# 1. default Codex App/CLI path: install daemon + local CA, no Codex config edit
slimference proxy install
slimference proxy enable

# 2. optional hook precision layer; enables only [features].codex_hooks=true
slimference hook install codex

# 3. legacy/config-patch mode only when explicitly desired
slimference integrate install --client codex

# 4. Claude shell-env path still needs a reload after integrate mode
exec $SHELL -l

# 5. verify
slimference proxy status
slimference hook verify codex
slimference integrate status
slimference doctor
```

## Verifying everything works

```bash
slimference integrate status
# Claude Code: fully_wired  <path/to/claude>
# Codex:       fully_wired  <path/to/codex>
# Daemon:      running (pid 12345)  health=ok
```

For Codex, `integrate status` reports config-patch mode, not transparent mode.
Hooks and transparent proxy readiness are separate dimensions; missing
config-patch is not a broken state when `proxy install|enable` is the chosen
product path. `hook install codex` does not write `openai_base_url` or
`chatgpt_base_url`; it only writes Codex hook artifacts and the required
`[features] codex_hooks = true` flag.

## Failure-mode matrix

### 1. Daemon process crashed

| | |
|---|---|
| Detection | launchd's KeepAlive fires, restart within ~2 s (T68). |
| Client impact | Single `ECONNREFUSED` on the in-flight request; SDK retries; succeeds. |
| User action | None. |

### 2. Daemon restart loop

| | |
|---|---|
| Detection | `slimference service status` shows climbing restart count. |
| Client impact | Some requests fail, some succeed. |
| User action | `slimference integrate remove` + `exec $SHELL -l` → direct to upstream while you investigate (`~/.slimference/logs/daemon.stderr.log`). |

### 3. Slimference binary moved / deleted

| | |
|---|---|
| Detection | launchd can't spawn; persistent ECONNREFUSED. |
| Client impact | Every request fails. |
| User action | If the binary is still somewhere: `slimference integrate remove` from that path. Otherwise: manual emergency-off (see below). |

### 4. You want to disable compression without uninstalling

| | |
|---|---|
| Approach | TUI `B` hotkey OR `slimference bypass on` (T67). |
| Effect | Proxy keeps accepting connections but forwards bytes unmodified. |
| Recovery | `B` again or `slimference bypass off`. Hot-reload, no shell restart. |

### 5. Panic button

`slimference integrate emergency-off` strips legacy/config-patch wiring (shell
block, codex config block, hooks, stops the daemon) and prints the reload
instruction. It does not replace transparent-mode recovery; use `slimference
proxy disable` when the macOS proxy is armed and you need direct upstream
immediately.

### Optional Codex hooks

`slimference hook install codex` installs the precision hook layer without
changing Codex's OpenAI endpoints. It writes executable scripts under
`~/.slimference/hooks/`, merges `~/.codex/hooks.json`, and enables the official
Codex hook feature flag in `~/.codex/config.toml`.

The installed hook set is:

| Event | Slimference entry point | Purpose |
|-------|--------------------------|---------|
| `SessionStart` | `slimference codexhook session-start` | attach local Slimference state summary and mark session start |
| `PreToolUse` | `slimference rewrite` / `slimference readhook` | deny/block where supported; no transparent command rewrite claim |
| `PermissionRequest` | `slimference codexhook permission-request` | map Layer-0 deny/ask policy into Codex allow/deny decisions |
| `PostToolUse` | `slimference posttool` | archive raw output and return compact replacement feedback |
| `UserPromptSubmit` | `slimference codexhook user-prompt-submit` | mark a new user turn for downstream state ownership |
| `Stop` | `slimference codexhook stop` | flush/checkpoint hook-side telemetry |

Unsupported fields stay disabled. In particular, `PreToolUse.updatedInput`
is treated as parsed-but-fail-open until live Codex proof shows the installed
Codex build actually honors it.

If even `slimference` is unreachable (binary gone / PATH broken), do the
cleanup by hand:

```bash
# shell rc
sed -i '' '/>>> slimference integration >>>/,/<<< slimference integration <<</d' ~/.zshrc  ~/.bashrc ~/.bash_profile 2>/dev/null

# codex config
sed -i '' '/>>> slimference integration >>>/,/<<< slimference integration <<</d' ~/.codex/config.toml

# hooks
rm -rf ~/.slimference/hooks/

# launchd
launchctl unload ~/Library/LaunchAgents/com.slimference.daemon.plist
rm -f ~/Library/LaunchAgents/com.slimference.daemon.plist

# reload
exec $SHELL -l
```

## Why legacy Codex config-patch mode works without MITM

Codex reads `openai_base_url` + `chatgpt_base_url` from its `config.toml`
directly - no binary patching, no certificate authority installation, no
TLS fingerprint forgery. The local proxy listens on plain HTTP on
`127.0.0.1:8990`; Codex talks to it in the clear; the proxy then speaks
real HTTPS to `https://chatgpt.com` upstream with the same bearer token
and user-agent Codex would have sent directly.

From Cloudflare's / OpenAI's perspective the request volume is unchanged;
the request path, query, authorization, cookies, and user-agent are forwarded
as Codex sent them, except for normal upstream authority handling. Slimference
does not add an identifying upstream header. Only the request *content* is
smaller after compression.

Transparent Codex mode is different: Codex keeps its own config, macOS routes
allowlisted HTTPS through the local Slimference daemon when the proxy is armed,
and the local CA lets Slimference terminate only the LLM HTTPS paths it is
allowed to process. Disabling the system proxy returns Codex to direct OpenAI
traffic without editing Codex files.

Codex request-body compression is code-ready without requiring live local
Codex wiring: `/v1/responses` and `/backend-api/codex/*` are accepted as
potential Codex compression paths, but only recognised conversation shapes
enter Layer 1-3. Unknown Codex backend bodies are forwarded byte-for-byte
instead of being rejected or rewritten.

To reproduce the checked-in Codex reporting smoke corpus without touching
your live Codex installation:

```bash
go run ./scripts/benchmarks session-report tests/fixtures/codex
go run ./scripts/benchmarks session-report --markdown tests/fixtures/codex
go run ./scripts/benchmarks codex-smoke-gate tests/fixtures/codex
```

`tests/fixtures/codex/codex-metadata.json` declares the corpus provenance
(scrubbing method, Codex version, hooks/layers exercised, scenarios) and the
regression baseline that `codex-smoke-gate` enforces. The same gate runs as
the final step of `go run ./scripts/ci`, so any drift in the smoke fixture
fails the local CI gate.

That smoke corpus proves the reporting and gating path on synthetic data. It
is not a real Codex production corpus; a real 10-20 session capture still
requires explicit permission to run live Codex.

## Bypass semantics - the three off-switches

| Layer            | How                                                   | Scope                     |
|------------------|-------------------------------------------------------|---------------------------|
| TUI bypass       | `B` hotkey OR `slimference bypass on`                 | all layers, all providers |
| Per-provider     | TUI toggle OR `/admin/provider` POST                  | one provider              |
| Per-layer        | TUI toggle OR `/admin/layer` POST                     | L1 / L2 / L3 individually |

The TUI bypass is a single atomic flag that short-circuits every downstream
toggle, so "proxy is off" has one canonical meaning.

## Uninstall

```bash
slimference proxy disable          # transparent mode off, direct upstream
slimference proxy uninstall        # remove local CA / launchd / proxy artifacts
slimference integrate remove       # remove legacy config-patch wiring + hooks
slimference service uninstall      # stop and remove legacy launchd service
exec $SHELL -l                     # reload to drop ANTHROPIC_BASE_URL
```

Data under `~/.slimference/` (analytics, read-cache, tool-archive) is kept
so re-enabling preserves history. `rm -rf ~/.slimference/` nukes it.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `Health probe: degraded` after install | launchd spawned but proxy crashed | `tail ~/.slimference/logs/daemon.stderr.log` |
| `ECONNREFUSED` inside Claude | Shell env not reloaded | `exec $SHELL -l` |
| `ECONNREFUSED` inside Codex | Codex running from before the config edit | quit and restart Codex |
| `403 cf-ray ...` from Codex | Cloudflare flagged the connection | that is an OpenAI-side issue, not Slimference; `integrate remove` + wait a few minutes |
| `401 Unauthorized` from upstream | Auth token expired | run Claude / Codex login flow separately |

# Slimference Integration Guide

This is the operator reference for wiring Slimference into Claude Code and
Codex, and for getting yourself back to a direct upstream in a hurry.

## What integration does

Running `slimference integrate install` performs three independent wire-ups
in a single idempotent operation:

| Client / Surface | Wire point                                  | Marker file                           |
|------------------|---------------------------------------------|---------------------------------------|
| Claude Code      | `export ANTHROPIC_BASE_URL=http://127.0.0.1:8990` | block in `~/.zshrc` / `.bashrc` / fish |
| Codex            | `openai_base_url` + `chatgpt_base_url`      | block in `~/.codex/config.toml`       |
| Hooks            | PreToolUse + PostToolUse scripts            | `~/.slimference/hooks/*.sh`           |

Every edit uses fenced marker comments so re-running install is a no-op and
`slimference integrate remove` removes exactly what was added. On first touch
of any file that already exists, a timestamped backup is saved next to it
(`.slim-backup-<ts>`).

## Quick start

```bash
# 1. one-shot wiring
slimference integrate install

# 2. launchd keeps the daemon running across logouts / crashes
slimference service install

# 3. reload the shell so ANTHROPIC_BASE_URL takes effect
exec $SHELL -l

# 4. verify
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

Any state other than `fully_wired` per client surfaces exactly which wire
point is missing (see `--json` for a machine-readable version).

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

`slimference integrate emergency-off` strips everything (shell block, codex
block, hooks, stops the daemon) and prints the reload instruction. Safe to
run from any state.

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

## Why Codex works without MITM

Codex reads `openai_base_url` + `chatgpt_base_url` from its `config.toml`
directly - no binary patching, no certificate authority installation, no
TLS fingerprint forgery. The local proxy listens on plain HTTP on
`127.0.0.1:8990`; Codex talks to it in the clear; the proxy then speaks
real HTTPS to `https://chatgpt.com` upstream with the same bearer token
and user-agent Codex would have sent directly.

From Cloudflare's / OpenAI's perspective the request volume is unchanged;
only the request *content* is smaller after compression.

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
slimference integrate remove       # unwire Claude + Codex + hooks
slimference service uninstall      # stop and remove launchd
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

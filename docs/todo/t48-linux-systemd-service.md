# T48 - Linux systemd Service Template + Install Docs

Status: todo
Priority: P1
Scope: `scripts/service/linux/`, `docs/deploy/`, `README.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`slimference service install` currently targets macOS launchd only. Linux
users have no supported daemon story - they can run `slimference --no-tui`
(after T44) but must author their own systemd unit file with no guidance
on paths, environment variables, restart policy, or log sinks.

Per `AGENTS.md` "macOS-only" scope was a 2026-04-18 decision made when the
headless mode did not exist. With T44 delivering a proper headless entry
point, a systemd template is cheap to ship and removes the single biggest
non-macOS UX gap.

## Current State

- `internal/daemon/` only implements launchd (macOS).
- No unit file template anywhere in the repo.
- No deploy docs.

## Target State

- `scripts/service/linux/slimference.service` - user-scoped systemd unit
  template (lives under `~/.config/systemd/user/`), does not require root.
- `scripts/service/linux/install.sh` - minimal bash installer (one of the
  **explicit exceptions** to TS-only tooling rule because systemd-install
  must run before Bun is guaranteed to be present; also the target audience
  is already on bash).
- `docs/deploy/linux-systemd.md` - step-by-step install, verify, logs,
  upgrade, uninstall.
- `slimference service install` on Linux generates + installs the unit file
  via `systemctl --user enable --now slimference.service`.

## Design

### Unit file template

```ini
[Unit]
Description=Slimference - Claude/Codex token-optimizing proxy
Documentation=https://github.com/slimference/slimference
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/slimference --no-tui --log-format=json
Restart=on-failure
RestartSec=5s
StartLimitInterval=60s
StartLimitBurst=5

# Hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=%h/.slimference %h/.config/slimference
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
LockPersonality=true
RestrictRealtime=true
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM

# Resource limits
LimitNOFILE=4096
MemoryMax=512M

# Environment
Environment=HOME=%h
EnvironmentFile=-%h/.config/slimference/env

[Install]
WantedBy=default.target
```

Two variants:
- `slimference.service` - user-scoped (recommended, no sudo).
- `slimference-system.service` - system-scoped alternative with
  `DynamicUser=yes`, for shared hosts.

### ENV file

`~/.config/slimference/env` (sourced if exists):

```
MINIMAX_API_KEY=...
SLIMFERENCE_LOG_LEVEL=info
SLIMFERENCE_LISTEN=127.0.0.1:8990
```

### Install script

`scripts/service/linux/install.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
mkdir -p "$UNIT_DIR"
install -m 0644 "$(dirname "$0")/slimference.service" "$UNIT_DIR/"

systemctl --user daemon-reload
systemctl --user enable --now slimference.service

echo "Slimference installed. Status:"
systemctl --user status slimference.service --no-pager -l
echo ""
echo "Logs: journalctl --user -u slimference -f"
```

### `slimference service install` on Linux

When run on Linux:

1. Writes unit file to `~/.config/systemd/user/slimference.service`.
2. Runs `systemctl --user daemon-reload && enable --now`.
3. Prints `journalctl --user -u slimference` pointer.

Subcommands that already exist on macOS (`service start/stop/status/logs`)
map to `systemctl --user [start|stop|status]` and `journalctl --user -u`.

### Docs

`docs/deploy/linux-systemd.md`:

1. Prerequisites (systemd ≥ 232 for user services, linger enabled for
   boot-time start: `loginctl enable-linger $USER`).
2. Install path (tar.gz from T47 release or `go install`).
3. Service install.
4. Configuration (ENV file, config.toml via XDG).
5. Logs (journalctl).
6. Upgrade (stop → replace binary → start).
7. Uninstall.

## Implementation Plan

### WP1 - Unit file template
- Write user-scoped unit with hardening.
- Write system-scoped variant with DynamicUser.

### WP2 - Install script
- Bash installer with idempotent `enable --now`.

### WP3 - `slimference service install` Linux arm
- Runtime detection (`runtime.GOOS == "linux"`).
- Parameterise `internal/daemon` to dispatch to platform-specific backend.

### WP4 - Logs/stop/start mapping
- `slimference service logs` → `journalctl --user -u slimference --since`.
- `slimference service status` → parse `systemctl show` output.
- `slimference service stop/start/restart` → systemctl.

### WP5 - Docs
- `docs/deploy/linux-systemd.md`.
- README `Install on Linux` section.

### WP6 - Tests
- Unit-file syntax test (`systemd-analyze verify` if available in CI).
- Go unit test for platform dispatch (mocked executor).

---

## Subtasks

- [ ] `scripts/service/linux/slimference.service` (user-scoped).
- [ ] `scripts/service/linux/slimference-system.service` (system-scoped).
- [ ] `scripts/service/linux/install.sh`.
- [ ] `internal/daemon/systemd.go` platform backend.
- [ ] Platform-dispatch in `service install/start/stop/status/logs`.
- [ ] `docs/deploy/linux-systemd.md`.
- [ ] README Linux install section.
- [ ] `systemd-analyze verify` step in CI on ubuntu-runner.
- [ ] Integration test in CI.

## Risks

- systemd version skew across distros: verify hardening directives on
  oldest supported (ubuntu 20.04 ships systemd 245).
- User without linger enabled will lose daemon on logout. Call out in
  docs + print warning in `service install`.
- `ProtectHome=read-only` blocks the default data path under `~/.slimference`.
  Worked around by explicit `ReadWritePaths=`.

## Acceptance Criteria

- [ ] `slimference service install` on Linux writes unit + enables.
- [ ] `systemctl --user status slimference` shows active after install.
- [ ] `slimference service logs` tails journal.
- [ ] Unit file passes `systemd-analyze verify`.
- [ ] Docs include the full lifecycle (install → verify → upgrade →
      uninstall).
- [ ] macOS behaviour unchanged.

## Out of Scope

- Distro packaging (deb/rpm/AUR).
- Windows service.
- OpenRC / runit / s6.

---

## Validation

```
./scripts/service/linux/install.sh
systemd-analyze verify scripts/service/linux/slimference.service
./slimference service status
./slimference service logs --since "5 min ago"
```

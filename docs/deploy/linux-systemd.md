# Slimference on Linux (systemd --user)

## Prerequisites

- systemd >= 245 (ships with Ubuntu 20.04+, Debian 11+, Fedora 34+).
- A writable `$XDG_CONFIG_HOME` (default `~/.config`).
- `loginctl enable-linger $USER` once if you want the daemon to keep
  running after logout.

## 1. Install the binary

From a release archive (see T47 / `scripts/release`):

```bash
curl -fsSL https://example.invalid/slimference_2.1.0_linux_amd64.tar.gz \
  | tar -xz -C /tmp
install -Dm755 /tmp/slimference_2.1.0_linux_amd64/slimference "$HOME/.local/bin/slimference"
```

Or from source inside a clone:

```bash
go build -o "$HOME/.local/bin/slimference" ./cmd/slimference
```

Verify:

```bash
slimference --version
slimference doctor
```

## 2. Install the service

```bash
./scripts/service/linux/install.sh
```

The installer copies `slimference.service` to
`$XDG_CONFIG_HOME/systemd/user/`, runs `systemctl --user daemon-reload`
and `enable --now`, then prints the canonical status/logs commands.

## 3. Configure

Optional environment overrides live in `~/.config/slimference/env`
(picked up via `EnvironmentFile=-`):

```env
MINIMAX_API_KEY=sk-...
SLIMFERENCE_LOG_LEVEL=info
SLIMFERENCE_LISTEN_PORT=8990
```

The service runs with:

- `ProtectHome=read-only` + `ReadWritePaths=~/.slimference ~/.config/slimference`
- `ProtectSystem=strict`, `NoNewPrivileges`, `PrivateTmp`
- `MemoryMax=512M`, `LimitNOFILE=4096`
- Restart on failure with a burst limit (5 in 60 s)

## 4. Observe

```bash
systemctl --user status slimference
journalctl --user -u slimference -f
curl -s 127.0.0.1:8990/admin/health
```

## 5. Upgrade

```bash
systemctl --user stop slimference
cp new-binary "$HOME/.local/bin/slimference"
systemctl --user start slimference
```

## 6. Uninstall

```bash
systemctl --user disable --now slimference
rm "$XDG_CONFIG_HOME/systemd/user/slimference.service"
systemctl --user daemon-reload
```

Data under `~/.slimference/` (analytics, read-cache, tool-archive) is
kept by design so re-enabling preserves history. Remove it manually if
you want a clean slate.

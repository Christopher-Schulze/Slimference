# T44 - Headless Foreground Mode (`--no-tui` / `--headless`)

Status: todo
Priority: P0
Scope: `cmd/slimference/main.go`, `internal/proxy/proxy.go`, `internal/slogutil/`, `docs/documentation.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`slimference` with no args unconditionally starts the TUI (BubbleTea). On
a non-TTY (Docker, systemd, CI, SSH without TTY, tmux with detached
session) this fails with:

```
TUI error: could not open a new TTY: open /dev/tty: device not configured
```

There is no supported path to run the proxy in foreground without the TUI.
`slimference service install` helps only on macOS (launchd). Users on
Linux, Docker, or simple setups cannot run the proxy as a long-lived
foreground process and stream logs to stdout/stderr - the canonical
deployment pattern for modern daemons.

This blocks: container deployments, systemd on Linux, k8s, GitHub Actions
dev-loops, tmux/screen sessions, anyone who wants `slimference | tee log`.

## Current State

- `main.go` → `startTUI()` for the empty-args path.
- `internal/proxy/proxy.go` has a `Run(ctx)` that works standalone; the TUI
  wraps it.
- No CLI flag exposes the headless path.

## Target State

- `slimference --no-tui` runs the proxy in foreground, logs structured JSON
  to stdout (or file via `--log-file`), traps SIGINT/SIGTERM for clean
  shutdown, exits 0 on clean or non-zero on fatal.
- `--headless` is a documented alias for `--no-tui`.
- On a non-TTY with no explicit flag, Slimference prints help and exits 2
  (matches T43), unless `SLIMFERENCE_HEADLESS=1` or `--no-tui` is set.
- Output is single-stream structured JSON line by default; `--log-format=text`
  for human-readable (colour respects `--color`).
- Supports `SIGHUP` for config reload (optional stretch goal).

## Design

### CLI surface

```
slimference --no-tui [--log-format=json|text] [--log-file=/var/log/slim.log]
                     [--log-level=info] [--color=auto|always|never]
```

### Code layout

1. `main.go`: early-dispatch after T43 help check:

```go
if flags.NoTUI || os.Getenv("SLIMFERENCE_HEADLESS") == "1" {
    return runHeadless(ctx, cfg, flags)
}
```

2. New function `runHeadless(ctx, cfg, flags)`:
   - configure slog handler (JSON or TextHandler)
   - set up signal handler (SIGINT/SIGTERM → cancel ctx)
   - `proxy.New(cfg)` → `p.Run(ctx)`
   - on ctx-done → `p.Shutdown(30*time.Second)` (uses T60 timeout guard)
   - return proxy exit code

### Logging

- Default: `slog.NewJSONHandler(os.Stdout, ...)`.
- `--log-file`: rotate through `internal/slogutil.RotatingWriter`.
- Respect `--log-level` (debug|info|warn|error).
- On shutdown log `event=shutdown reason=signal signal=SIGTERM`.

### Signal handling

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
go func() {
    sig := <-sigCh
    slog.Info("signal_received", "signal", sig)
    cancel()
}()
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | clean shutdown (SIGINT/SIGTERM) |
| 2 | bad flags / config |
| 3 | bind error (port in use) |
| 4 | upstream unreachable at boot (doctor-style fail-fast) |
| 5 | unexpected crash (recovered) |

### Health endpoint

Already present on `/admin/health`. Document it in help + docs as canonical
container healthcheck (`curl -f 127.0.0.1:8990/admin/health`).

## Implementation Plan

### WP1 - Flag parsing
- Add `--no-tui`, `--headless`, `--log-format`, `--log-file`, `--log-level`
  to flag layer introduced by T43.

### WP2 - runHeadless entrypoint
- Factor shared `proxy.New` setup out of TUI path into helper so both TUI
  and headless call the same constructor.

### WP3 - Signal handling + graceful shutdown
- Context cancellation + `p.Shutdown(timeout)` (requires T60 for hard cap).

### WP4 - Logging handler selection
- JSON handler default; Text handler with colour for `--log-format=text`.
- Rotating writer integration when `--log-file` set.

### WP5 - Exit code taxonomy
- Map boot failures to the table above.

### WP6 - Docker/systemd example configs
- Ship `scripts/service/linux/slimference.service` (ties into T48).
- Ship `scripts/service/docker/Dockerfile` as reference (minimal).

### WP7 - Tests
- Table test: start headless with random port, hit `/admin/health`, send
  SIGTERM, assert exit 0 within 1 s.
- Flag parse tests.

---

## Subtasks

- [ ] Add `--no-tui` / `--headless` flags + env fallback.
- [ ] Extract shared proxy-construction helper.
- [ ] Implement `runHeadless` with signal traps.
- [ ] JSON / Text log handler switching.
- [ ] Rotating file sink when `--log-file` set.
- [ ] Exit-code taxonomy wired to boot-failure paths.
- [ ] Dockerfile + systemd unit reference.
- [ ] Integration test (random port, SIGTERM, clean exit).
- [ ] Update README + `docs/documentation.md`.

## Risks

- Double-logging if TUI path also installs a JSON handler: ensure handler
  is selected in exactly one place.
- Signal race: if SIGTERM arrives mid-boot, shutdown must still proceed.
  Cover with unit test using fake proxy that sleeps 100 ms in Run.

## Acceptance Criteria

- [ ] `./slimference --no-tui` binds port, serves requests, exits 0 on
      SIGTERM within 1 s.
- [ ] JSON log lines on stdout; `--log-format=text` readable.
- [ ] `SLIMFERENCE_HEADLESS=1 ./slimference` works identically.
- [ ] `curl -f 127.0.0.1:8990/admin/health` returns 200 during runtime.
- [ ] Dockerfile example builds and runs the binary.
- [ ] Integration test green.

## Out of Scope

- Config reload on SIGHUP (stretch goal, separate TASK if needed).
- Windows service mode.

---

## Validation

```
./slimference --no-tui --log-level=debug &
SLIM_PID=$!
curl -sf 127.0.0.1:8990/admin/health
kill -TERM $SLIM_PID
wait $SLIM_PID   # expect 0

docker build -f scripts/service/docker/Dockerfile -t slimference:test .
docker run --rm -p 8990:8990 slimference:test --no-tui
```

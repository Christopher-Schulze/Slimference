# TASK 199: Daemon integration of Phase G packages

Status: PARTIAL 2026-05-17 (daemon seams wired; WSS mutation split to T208)
Priority: P0 (the seam that takes Phase G from "code in tree" to "code running")
Scope: `cmd/slimference/main.go`, `cmd/slimference/proxy_cmd.go`,
       `internal/proxy/proxy.go`, `internal/proxy/admin.go`,
       `internal/proxy/handler.go` - wire the 9 new packages into the
       existing daemon process

## Why

Phase G shipped 9 self-contained packages today. They compile, test
green, race-clean. They are NOT yet wired into `cmd/slimference`. To
finish the loop the daemon needs to:

- Construct an `apps.Manager` at startup, hot-reload on SIGHUP.
- Construct a `sniroute.Resolver` over that Manager.
- Construct a `tlsca` Signer + a `transparent.Engine` for the :443
  listener (only when the operator opts in via `slimference proxy
  enable`).
- Wire a `wsmitm.Session.FrameHandler` that calls into the existing
  Phase F mechanisms (outstop / streamcut / repdet / staleread /
  beterse).
- Aggregate counters from transparent.Engine + wsmitm.Session into
  the existing `/admin/state` surface.
- Build a `reversibility.Plan` from concrete Steps so the TUI / CLI
  install flow runs through it.
- Pass an `*apps.Manager`, `*reversibility.Plan`, and Probe-set into
  the TUI Model.

## Current reality 2026-05-17

Most daemon seams from this task have landed:

- `Proxy.SetAppsManager`, `AppsManager`, admin `/state`, admin `/apps`,
  `SavingsProbe`, and `NoopIndistProbe` are wired.
- `cmd/slimference` startup constructs the app manager, probe set, and
  SIGHUP reload path.
- `transparent.Engine` and `PhaseFDispatcher` run behind
  `Transparent.SNIPeekMode` / `SNIPeekPort`.
- The dispatcher has a safe byte bridge and a `wsmitm.Session` path.

The remaining missing product value is not routing, it is mutation:
`wsmitm.Session.FrameHandler` must map Codex WSS envelopes into the
existing Phase F mutators. That work is now tracked explicitly as
T208.

## Target state

### Startup sequence (`cmd/slimference/main.go`)

```go
func main() {
    // ... existing flag parsing ...

    cfg := config.Load()

    // Phase G plumbing.
    appsMgr, err := apps.NewManager(filepath.Join(
        os.Getenv("HOME"), ".config/slimference/apps.toml"))
    must(err)

    plan := reversibility.NewPlan(
        &steps.CAGenerate{Dir: slimDataDir},
        &steps.HostsPatch{
            Targets: []string{"chatgpt.com", "api.openai.com"},
            Address: "127.0.0.1",
            BackupDir: filepath.Join(slimDataDir, "backups"),
        },
        &steps.LaunchdInstall{
            BinaryPath: selfBinaryPath(),
        },
        // (codex config + hooks managed by existing integrate package;
        //  do NOT duplicate here)
    )

    p := proxy.New(cfg)
    p.SetAppsManager(appsMgr)         // new accessor

    // Transparent :443 listener constructed ONLY if proxy is "armed".
    var listener *transparent.Engine
    if config.IsTransparentArmed() {
        l, err := net.Listen("tcp", ":443")
        // ... fallback to pfctl rdr from 443 → 8990 if EACCES ...
        signer := tlsca.NewSigner(/* CA from slimDataDir */)
        listener = &transparent.Engine{
            Listener:   l,
            Resolver:   sniroute.New(appsMgr),
            Certs:      signer,
            Dispatcher: newPhaseFDispatcher(p),
        }
        go listener.Run(context.Background())
    }

    // TUI wiring.
    if tuiEnabled {
        m := tui.New(tui.Options{
            Proxy: p, AppsManager: appsMgr, Plan: plan,
            Probes: buildProbes(slimDataDir, p, appsMgr),
        })
        tea.NewProgram(m).Run()
    }

    // ... existing serve loop ...
}
```

### Phase-F dispatcher (`internal/proxy/wsmitm_dispatcher.go`, new)

```go
type PhaseFDispatcher struct {
    p *proxy.Proxy
}

func (d *PhaseFDispatcher) Handle(ctx context.Context, dec sniroute.Decision,
        req sniroute.Request, conn net.Conn) error {
    if dec == sniroute.PassthroughTLS {
        return passthroughBridge(ctx, req, conn) // dial upstream + bridge
    }
    // dec == MITMConversation: upgrade WebSocket on conn, dial upstream
    // wss://chatgpt.com/..., open a wsmitm.Session bridge.
    return runWSMITM(ctx, req, conn, d.p)
}
```

The wsmitm.FrameHandler reuses the existing Phase F handlers:

```go
handler := func(_ context.Context, dir wsmitm.Direction, env *wsmitm.Envelope) (bool, error) {
    if dir == wsmitm.DirClientToServer && env.Kind == wsmitm.FrameKindRequest {
        // Mutate env.Raw payload via outstop / beterse / staleread /
        // obsolete-prune by calling into the existing handler.go path.
        mutated := applyInputPipeline(env, p)
        return mutated, nil
    }
    if dir == wsmitm.DirServerToClient {
        // Run streamcut/repdet over text deltas + completion.
        return applyOutputPipeline(env, p)
    }
    return false, nil
}
```

### Admin endpoint extensions (`internal/proxy/admin.go`)

`/admin/state` returns the full `control.SetupState`. New endpoint
`/admin/apps` (POST) accepts `{"id":"codex_cli","enabled":false}` for
runtime toggle from CLI / TUI.

### Reload on SIGHUP

The existing signal handler must call `appsMgr.Reload()` so external
edits to `~/.config/slimference/apps.toml` take effect within seconds.

## Implementation plan

1. **Proxy accessor methods** in `internal/proxy/proxy.go`:
   - `(*Proxy).SetAppsManager(*apps.Manager)`
   - `(*Proxy).AppsManager() *apps.Manager`
   The handler.go path consults the manager when it has a UA on the
   request (already-implemented in T193 wiring).

2. **`buildProbes` helper** in `cmd/slimference/main.go`:
   ```go
   func buildProbes(dataDir string, p *proxy.Proxy, m *apps.Manager) control.Probes {
       return control.Probes{
           CA:        &control.FileCAProbe{Dir: dataDir},
           Daemon:    &control.HTTPDaemonProbe{BaseURL: "http://127.0.0.1:8990"},
           Listener:  &control.PortListenerProbe{Port443: 443, Port8990: 8990},
           NetworkRedir: &control.HostsFileNetworkProbe{},
           Apps:      &control.AppsManagerProbe{Manager: m, Counters: p.AppCounters()},
           Savings:   newSavingsProbe(p),
           Indist:    &control.NoopIndistProbe{}, // until T198 wires it
       }
   }
   ```

3. **`newSavingsProbe`**: maps `Proxy.OutputReduceCounters.Snapshot()`
   to `control.SavingsSummary`. One-to-one field copy + cost
   calculation using the existing `outputreduce` USD/M-tokens config.

4. **`/admin/state` handler** in `internal/proxy/admin.go`: assemble
   `control.SetupState` via `control.Build(ctx, p.probes)` and emit
   as JSON.

5. **`/admin/apps` POST handler**: parse body, call
   `p.AppsManager().SetEnabled(id, enabled)`, return new state.

6. **SIGHUP reload**: existing signal handler in `cmd/slimference/
   main.go` extended to call `appsMgr.Reload()`.

7. **wsmitm dispatcher**: new file `internal/proxy/wsmitm_dispatcher.
   go` implements the `transparent.Dispatcher` interface. For
   passthrough it dials the real upstream via DoH-resolved IP and
   bridges bytes. For MITM it upgrades the WS, dials real WSS upstream,
   constructs a `wsmitm.Session`, and runs Serve.

8. **Tests**:
   - `cmd/slimference/main_test.go`: verify `buildProbes` returns all
     7 probe slots populated.
   - `internal/proxy/admin_phase_g_test.go`: GET `/admin/state` returns
     a populated SetupState; POST `/admin/apps` flips a toggle.
   - `internal/proxy/wsmitm_dispatcher_test.go`: synthetic WSS handshake
     into the dispatcher; passthrough path bridges bytes byte-equal;
     MITM path applies stop_seq injection on a request frame.

## Acceptance

- `slimference --no-tui` starts the daemon with all new packages
  initialised; `/admin/state` returns a valid SetupState JSON.
- Toggling `apps.toml` and sending SIGHUP changes routing decisions
  within ≤ 2 s (next connection).
- A live conversation through the daemon (with CA trusted +
  `root-arm` + `enable` armed) increments `output_reduce_counters` AND
  `transparent.Telemetry` counters together.
- `slimference install` and `slimference uninstall` run the
  `reversibility.Plan` so the snapshot-diff test from T196 passes.
- Existing TUI still launches with `slimference` (no-arg).
- Full test suite green; race-clean.

## Sub-Tasks

- [ ] `Proxy.SetAppsManager` + `AppsManager()` accessors.
- [ ] `buildProbes` helper + `newSavingsProbe`.
- [ ] `NoopIndistProbe` placeholder until T198 wires real probe.
- [ ] `/admin/state` handler.
- [ ] `/admin/apps` POST handler.
- [ ] SIGHUP-reload wiring.
- [ ] `wsmitm_dispatcher.go` with passthrough + MITM paths.
- [ ] FrameHandler factory that calls existing handler.go pipeline.
- [ ] `cmd/slimference` startup sequence updates.
- [ ] Integration tests for the seams listed above.
- [ ] Documentation update in `docs/transparent-mode.md`.

## Notes

- Legacy `proxy enable` / system HTTPS proxy plumbing remains in tree
  only as advanced/manual support. The active Phase H default path is
  `cert-trust` + `root-arm` + `enable` feeding `transparent.Engine`.
- The `wsmitm_dispatcher` is the load-bearing missing link between
  Phase G packages and live traffic. Its correctness is the key
  measure of Phase G being "shipped".
- Frame-handler factory must NOT duplicate the Phase F logic; it
  invokes the existing `handler.go` path via shared helpers so a
  bug fixed in either path applies to both transports.

## Deviations

(none yet)

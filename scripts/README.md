# `scripts/` — Thematisches Go-Tooling (Slimference)

Alle **Werkzeuge** dieses Repos (Coverage-Gates, Benchmark-Helfer, Utils, …) liegen **hier** in **Unterordnern nach Thema** — nicht im Repository-Root.

## Unterordner

| Pfad | Zweck |
|------|--------|
| `build/` | Ein lokales, einzelnes Slimference-Binary mit Release-Flags bauen (`-trimpath -ldflags "-s -w"`); `--install` ersetzt die Ziel-Binary per temp-file + atomic rename |
| `coverage/` | Coverage auswerten, Schwellen (aktuell 95.0 % aggregate) prüfen, CI-lokal spiegeln |
| `benchmarks/` | Benchmarks bündeln, `go test -bench` auswerten |
| `release/` | Portable Release-Artefakte mit SHA256SUMS bauen; default Ziel ist macOS darwin/arm64 |
| `utils/` | Kleine Hilfs-CLIs, einmalige Tasks, Generatoren; `utils/indist_probe` ist das tshark-basierte Capture/Diff-Werkzeug für T224 |

Weitere Unterordner nur bei **klarem Thema** (z. B. `lint/`, `release/`).

## Regeln

- Implementierung: **Go** (`.go`), siehe **`AGENTS.md`** §3.
- **`rtk-master/`** gehört **nicht** hierher — Fremdreferenz, nicht verschieben.

## Ausführung

Vom Modulroot (`Slimference/`):

```bash
go run ./scripts/coverage/...    # sobald ein entrypoint existiert
```

Konkrete Kommandozeilen:

```bash
go run ./scripts/build --install                # Optimiertes Binary nach ~/.local/bin/slimference
go run ./scripts/build --restart                # Sicheres lokales Update: stop -> build -> atomic install -> start
go run ./scripts/build --out ./slimference      # Optimiertes lokales Binary
go run ./scripts/release --version v2.0.2       # Portable macOS-arm64 Release-Tarball + SHA256SUMS
go run ./scripts/release --version v2.0.2 --targets=all  # Alle aktuell unterstützten Targets
go run ./scripts/coverage -min=95.0              # Coverage-Gate (aggregate)
go run ./scripts/benchmarks                      # Hot-path Benchmarks (3s): compression/filter/proxy/readcache/archive/chunk/planner
go run ./scripts/benchmarks -- -benchtime=1s     # Schneller Durchlauf
go run ./scripts/benchmarks -- -count=3          # 3 Runden für Stabilität
go run ./scripts/benchmarks -- -pkg=compression  # Nur compression-Paket
go run ./scripts/benchmarks -- -pkg=proxy        # Nur Codex/WSS Layer-0 Hotpath
go run ./scripts/benchmarks session-report tests/fixtures/codex
go run ./scripts/benchmarks session-report --markdown tests/fixtures/codex
go run ./scripts/benchmarks codex-smoke-gate tests/fixtures/codex   # CI-enforced regression gate
go run ./scripts/utils session-report ~/.slimference/analytics/2026-04-17.jsonl
go run ./scripts/utils decision-report ~/.slimference/logs/decisions.jsonl --json
go run ./scripts/utils filter-report ~/.slimference/filter.db --csv
go run ./scripts/utils combined-report ~/.slimference/analytics/2026-04-17.jsonl \
  ~/.slimference/logs/decisions.jsonl \
  ~/.slimference/filter.db
go run ./scripts/utils aggregate-savings                                              # live admin/state honest aggregate
go run ./scripts/utils aggregate-savings --filter-db=~/.slimference/filter.db --period=today
go run ./scripts/utils aggregate-savings --admin-state-file=admin-state.json --json   # offline mode
go run ./scripts/utils workday-savings start                                         # baseline for real workday savings
go run ./scripts/utils workday-savings finish --filter-db=~/.slimference/filter.db   # flush-aware window delta
go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl --json         # content-free WSS route/session/re-read audit
go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl --since=2026-05-30T00:30:00Z --expect-distinct-sessions=2 --min-phasef=2  # fresh session-key gate
go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl --since=2026-05-30T00:30:00Z --min-phasef=2 --require-savings  # fresh savings gate
go run ./scripts/utils codex-capture-run --binary ~/.local/bin/slimference --capture ~/.slimference/captures/repeat.jsonl --matrix-row /tmp/proof-matrix.jsonl --id cli-repeat --workload-class repeat_full_read --expected-reducer read_delta --codex-timeout=180s --exit-marker CAPTURE_DONE --exit-marker-count=2 --quiet-codex-output -- "Run exactly two shell tool calls and do not modify files. First tool call cmd exactly: cat AGENTS.md Second tool call cmd exactly: cat AGENTS.md Then final message exactly CAPTURE_DONE"
go run ./scripts/utils wss-ab-replay captures/codex-wss-frames.jsonl --fail-on-lost # offline Phase-F comprehension A/B replay
go run ./scripts/utils wss-ab-replay captures/codex-wss-frames.jsonl --json          # machine-readable A/B report
go run ./scripts/utils wss-ab-replay captures/codex-wss-frames.jsonl --fail-on-lost --json # auto-policy WSS reducer replay, including T255 when safe
go run ./scripts/utils wss-proof-matrix captures/proof-matrix.jsonl --json           # T257 real-workload proof gate
go run ./scripts/utils tls-probe --profile=chromium_stable --json
go run ./scripts/utils/indist_probe capture --label codex-native-direct --out research/indist/codex-native-direct.json --iface en0 --host chatgpt.com --port 443
go run ./scripts/utils/indist_probe diff research/indist/codex-native-direct.json research/indist/slimference-scoped-wss.json
```

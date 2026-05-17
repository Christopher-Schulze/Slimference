# `scripts/` — Thematisches Go-Tooling (Slimference)

Alle **Werkzeuge** dieses Repos (Coverage-Gates, Benchmark-Helfer, Utils, …) liegen **hier** in **Unterordnern nach Thema** — nicht im Repository-Root.

## Unterordner

| Pfad | Zweck |
|------|--------|
| `build/` | Ein lokales, einzelnes Slimference-Binary mit Release-Flags bauen (`-trimpath -ldflags "-s -w"`) |
| `coverage/` | Coverage auswerten, Schwellen (aktuell 99.5 % aggregate) prüfen, CI-lokal spiegeln |
| `benchmarks/` | Benchmarks bündeln, `go test -bench` auswerten |
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
go run ./scripts/build --out ./slimference      # Optimiertes lokales Binary
go run ./scripts/coverage -min=99.5              # Coverage-Gate (aggregate)
go run ./scripts/benchmarks                      # Hot-path Benchmarks (3s)
go run ./scripts/benchmarks -- -benchtime=1s     # Schneller Durchlauf
go run ./scripts/benchmarks -- -count=3          # 3 Runden für Stabilität
go run ./scripts/benchmarks -- -pkg=compression  # Nur compression-Paket
go run ./scripts/benchmarks session-report tests/fixtures/codex
go run ./scripts/benchmarks session-report --markdown tests/fixtures/codex
go run ./scripts/benchmarks codex-smoke-gate tests/fixtures/codex   # CI-enforced regression gate
go run ./scripts/utils session-report ~/.slimference/analytics/2026-04-17.jsonl
go run ./scripts/utils decision-report ~/.slimference/logs/decisions.jsonl --json
go run ./scripts/utils filter-report ~/.slimference/filter.db --csv
go run ./scripts/utils combined-report ~/.slimference/analytics/2026-04-17.jsonl \
  ~/.slimference/logs/decisions.jsonl \
  ~/.slimference/filter.db
go run ./scripts/utils tls-probe --profile=chromium_stable --json
go run ./scripts/utils/indist_probe capture --label codex-native-direct --out research/indist/codex-native-direct.json --iface en0 --host chatgpt.com --port 443
go run ./scripts/utils/indist_probe diff research/indist/codex-native-direct.json research/indist/slimference-scoped-wss.json
```

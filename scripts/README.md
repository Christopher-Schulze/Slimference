# `scripts/` — Thematisches Go-Tooling (Slimference)

Alle **Werkzeuge** dieses Repos (Coverage-Gates, Benchmark-Helfer, Utils, …) liegen **hier** in **Unterordnern nach Thema** — nicht im Repository-Root.

## Unterordner

| Pfad | Zweck |
|------|--------|
| `coverage/` | Coverage auswerten, Schwellen (z. B. 100 %) prüfen, CI-lokal spiegeln |
| `benchmarks/` | Benchmarks bündeln, `go test -bench` auswerten |
| `utils/` | Kleine Hilfs-CLIs, einmalige Tasks, Generatoren |

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
go run ./scripts/coverage -min=100               # Coverage-Gate (100%)
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
```

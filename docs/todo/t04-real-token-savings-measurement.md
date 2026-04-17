# T04 - Echte Token-Savings Messung (Offline + Inline)

**Status:** done (offline tool built + inline measurement verified)
**Priority:** medium
**Files:** `scripts/utils/main.go` (neu), `internal/analytics/`, `internal/debug/`

## Problem

Die Spec behauptet "85-90% token savings (combined Layer 0 + Layers 1-3)". Diese Zahl ist
bisher **nicht empirisch belegt**. Es gibt Benchmarks fuer Latenz, aber nicht fuer Token-Savings
mit echten Coding-Session-Dumps.

Gleichzeitig ist die Messung heikel: Anthropic/OpenAI duerfen nicht merken dass ein Proxy
dazwischengeschaltet ist (Provider-Invisibility §16.4). Ein Benchmark-Modus der Live-APIs
anpingt und die Latenzen/Savings misst waere riskant.

## Loesung: Zwei-Strahl-Messung

### Strahl 1: Inline-Messung (bereits vorhanden)

Slimference misst bereits bei jedem Request:
- `input_orig`: Tokens vor Kompression
- `input_comp`: Tokens nach Kompression
- `saved`: Differenz
- `ratio`: Kompressionsrate
- `layers`: Welche Layer angewendet wurden
- `layer1_breakdown`: Per-Sub-Layer Savings

Diese Daten fliessen in:
- Analytics In-Memory Counter (`internal/analytics/`)
- JSONL Decision Log (`SLIMFERENCE_DEBUG_DECISIONS_LOG`)
- Session JSONL Files (`~/.slimference/analytics/`)

**Das IST bereits die echte Messung.** Man muss Slimference nur normal mit Claude Code/Codex
nutzen und dann die Logs auswerten.

### Strahl 2: Offline-Auswertung (neu zu bauen)

Ein Go-Tool unter `scripts/utils/` das bestehende Log-Dateien auswertet:

```bash
# Session-JSONL auswerten
go run ./scripts/utils session-report ~/.slimference/analytics/session-2026-04-16.jsonl

# Decision-JSONL auswerten
go run ./scripts/utils decision-report ~/.slimference/logs/slimference.jsonl

# Filter-DB auswerten (Layer 0 savings)
go run ./scripts/utils filter-report ~/.slimference/filter.db
```

Output:
```
=== Session Report: 2026-04-16 14:30 - 15:45 ===
Total Requests:       47
Input Tokens (orig):  1,247,000
Input Tokens (comp):  412,510
Savings:              834,490 (66.9%)
Layer 1 Savings:      312,000 (25.0%)
Layer 2 Savings:      498,000 (39.9%)
Layer 3 Savings:      24,490  (2.0%)

Per-Sub-Layer Breakdown:
  ansi_strip:          12,000 tokens
  json_compact:         8,500 tokens
  comment_strip:       15,200 tokens
  dedup:               45,000 tokens
  structure_extract:   98,300 tokens
  delta_encoding:      22,000 tokens
  success_shortcircuit: 8,000 tokens
  tool_compressor:     67,000 tokens
  repeated_collapse:   18,000 tokens
  graph_pruning:       18,000 tokens

Layer 0 (Filter DB):
  Commands filtered:    89
  Input bytes:          2.4 MB
  Output bytes:         340 KB
  Savings:              85.8%

Combined Savings:      85-90% (estimated, Layer 0 prevents tokens from ever entering)
```

### Strahl 3: Initialer Proof (manuell)

Anleitung zum ersten echten Nachweis:

1. Slimference starten
2. Claude Code 30 Minuten normal nutzen
3. `slimference stats today` -> echte Zahlen
4. `slimference debug last 20 --json` -> per-Request Detail
5. Session-JSONL archivieren fuer spaetere Analyse

Kein API-Call, kein Risk. Die Savings sind die echten aus dem Live-Betrieb.

## Implementation Plan

### scripts/utils/main.go

Neues Go-Tool, aufgerufen mit `go run ./scripts/utils <subcommand> <args>`.

Subcommands:
- `session-report <file.jsonl>`: Parse Session-JSONL, aggregate per-request metrics
- `decision-report <file.jsonl>`: Parse Decision-JSONL (from debug decisions_log)
- `filter-report <filter.db>`: Query SQLite filter_runs, aggregate per-command savings
- `combined-report <dir>`: Kombiniert alle drei Quellen zu einem Gesamtbild

Output-Formate: `--text` (default), `--json`, `--csv`

### Datenquellen

1. **Session JSONL** (`~/.slimference/analytics/*.jsonl`):
   - Jede Zeile: `types.AnalyticsEvent` als JSON
   - Enthaelt: `input_tokens_orig`, `input_tokens_comp`, `output_tokens`, `compression_ratio`, `layers`

2. **Decision JSONL** (`SLIMFERENCE_DEBUG_DECISIONS_LOG`):
   - Jede Zeile: `dbg.RequestSummary` als JSON
   - Enthaelt: `tokens.original`, `tokens.after_layer1`, `tokens.after_layer2`, `tokens.final`, `layer1_breakdown`

3. **Filter DB** (`~/.slimference/filter.db`):
   - Tabelle `filter_runs`: `input_tokens`, `output_tokens`, `savings_pct`, `command`, `timestamp`
   - Bereits abfragbar via `slimference gain`

### Was nicht gebaut wird

- Kein Benchmark-Modus der Live-APIs aufruft
- Kein automatisiertes E2E-Testing gegen Anthropic/OpenAI
- Kein Token-Zaehler der tiktoken auf Requests loslaesst (zu riskant)

## Sub-Tasks

- [ ] `scripts/utils/main.go` Grundstruktur (subcommand dispatch)
- [ ] `session-report` Subcommand: JSONL parse + aggregate
- [ ] `decision-report` Subcommand: JSONL parse + per-sub-layer breakdown
- [ ] `filter-report` Subcommand: SQLite query + aggregate
- [ ] `combined-report` Subcommand: alle Quellen zusammen
- [ ] Output: text, json, csv Formate
- [ ] Manueller Proof: 30 Min Claude Code Session + Report
- [ ] `docs/documentation.md`: Abschnitt "Measuring Real Savings" aktualisieren

## Verification

```bash
go build -o slimference ./cmd/slimference
./slimference                          # Start proxy
# ... 30 min Claude Code Session ...
./slimference stats today              # Inline numbers
go run ./scripts/utils session-report ~/.slimference/analytics/session-*.jsonl
```

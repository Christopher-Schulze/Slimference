# `tests/` — Integration, Fixtures, TypeScript-Testsuites

## Layout

| Pfad | Inhalt |
|------|--------|
| `integration/` | **Go**-Integrationstests (mehrere Pakete, ggf. `//go:build integration`) |
| `fixtures/` | Gemeinsame Testdaten für Go- und/oder TS-Tests |
| `ts/` | **TypeScript**-Testsuites (z. B. Vitest/Jest) — siehe **`AGENTS.md` §4.2 |

## Go vs. TypeScript

- **100 %-Coverage** auf `internal/` und `cmd/` wird über **`*_test.go`** neben dem Code erreicht (**Pflicht**).
- **`tests/ts/`** ist für **zusätzliche** TS-Tests (E2E, Contracts, …), **nicht** als Ersatz für Go-Pakettests.

Paketnahe kleine Dateien: weiterhin `testdata/` neben dem jeweiligen Paket.

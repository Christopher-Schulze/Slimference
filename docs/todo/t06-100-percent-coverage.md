# T06 - 100% Coverage herstellen

**Status:** done (100.0% on `cmd/...` + `internal/...`)
**Priority:** medium
**Files:** Alle `internal/` und `cmd/` Packages

## Problem

`docs/todo.md` behauptet "100% Coverage erreicht". Die Realitaet:

```
total: (statements) 98.2%
```

Zudem schlagen 2 Tests fehl (T01, T02) was die Coverage weiter drueckt.

### Aktuelle Coverage pro Package (Stand 2026-04-16)

| Package | Coverage | Status |
|---------|----------|--------|
| `cmd/slimference` | ~96% | Tests fallen wegen proxy-Abhaengigkeiten |
| `internal/analytics` | 100% | OK |
| `internal/caching` | 100% | OK |
| `internal/compression` | 100% | OK |
| `internal/config` | 100% | OK |
| `internal/debug` | 100% | OK |
| `internal/filter` | 100% | OK |
| `internal/hooks` | 100% | OK |
| `internal/proxy` | ~95% | **2 Tests fallen**, mehrere ungedeckte Branches |
| `internal/resilience` | 100% | OK |
| `internal/security` | 100% | OK |
| `internal/sessions` | 100% | OK |
| `internal/slogutil` | 95.1% | **Luecke** |
| `internal/summarization` | 100% | OK |
| `internal/tokens` | 100% | OK |
| `internal/tui` | 98.6% | **Luecke** |
| `internal/types` | 100% | OK |
| `internal/util` | 100% | OK |
| `scripts/coverage` | 33.3% | OK (Tool, nicht produktiv) |
| `scripts/ci` | 0% | OK (Tool, nicht produktiv) |

**Produktive Luecken:** `internal/proxy`, `internal/slogutil`, `internal/tui`

## Strategie

### Phase 1: T01+T02 fixen (Voraussetzung)

Die 2 Test-Failures in `internal/proxy` muessen zuerst gefixt werden.
Das bringt `internal/proxy` naeher an 100%.

### Phase 2: Coverage-Report analysieren

```bash
go test -coverprofile=coverage.out ./cmd/... ./internal/...
go tool cover -func=coverage.out | grep -v "100.0%"
```

Zeigt genau welche Funktionen/Zeilen nicht abgedeckt sind.

### Phase 3: Luecken schliessen

#### internal/proxy (nach T01+T02)

- [x] `TestHealthMonitor_degraded` fix -> erhoht Coverage
- [x] `TestStreamingRelay_contextCancelled` fix -> erhoht Coverage
- [x] `buildAggressiveCompressedBody` Error-Pfade
- [x] `parseRetryAfter` Edge-Cases (HTTP-date Format)
- [x] `isContextOverflow` alle Pattern-Varianten
- [x] Error-Pfade in `doUpstreamRequest` (429 retry exhaustion, etc.)

#### internal/slogutil (95.1%)

- [x] `RotatingWriter` Rotation-Trigger (max size reached)
- [x] Backup-File Rotation (.1 through .5)
- [x] Concurrent write + rotation race
- [x] Error-Pfade: permission denied, disk full

#### internal/tui (98.6%)

- [x] `renderMainView` Edge-Cases (zero requests, zero width)
- [x] `renderStatsView` mit leeren Per-Provider-Daten
- [x] Key-Handler Edge-Cases
- [x] Flash-Message Expiry
- [x] HookStatus Rendering (none installed, partial install)

### Phase 4: Verify

```bash
go test -coverprofile=coverage.out ./cmd/... ./internal/...
go tool cover -func=coverage.out | tail -1
# Must show: total: (statements) 100.0%

go test -race ./...
# Must pass clean
```

## Sub-Tasks

- [x] T01+T02 fixen (Voraussetzung)
- [x] Coverage-Report erstellen und Luecken identifizieren
- [x] `internal/proxy` fehlende Branches + Error-Pfade abdecken
- [x] `internal/slogutil` fehlende Branches abdecken
- [x] `internal/tui` fehlende Branches abdecken
- [x] `cmd/slimference` falls unter 100%
- [x] Final: 100% auf allen produktiven Packages
- [x] `go test -race ./...` clean
- [x] Coverage-Report archivieren

## Verification

```bash
go test -coverprofile=coverage.out ./cmd/... ./internal/...
go tool cover -func=coverage.out | grep -v "100.0%"
# No output = all 100%

go test -race ./...
# All pass
```

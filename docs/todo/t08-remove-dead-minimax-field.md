# T08: Layer2.minimax Feld entfernen (toter State)

**Date:** 2026-04-17
**Severity:** HIGH
**Status:** DONE
**Parent:** `docs/todo.md` -> Audit-Fixes

## Problem

`internal/summarization/layer2.go:23`:

```go
type Layer2 struct {
    cfg       *config.CompressionConfig
    minimax   *MiniMaxClient   // <-- TOT: wird nie gelesen
    chain     *FallbackChain
    ...
}
```

Das Feld `minimax` wird in `NewLayer2` (Zeile 38) gesetzt aber **nie gelesen**. Alle Zugriffe laufen ueber `l.chain`. Das Feld ist toter State der Speicher allokiert und Verwirrung stiftet.

## Verification

```bash
grep -rn 'l\.minimax' internal/summarization/*.go | grep -v '_test.go'
# Keine Treffer ausser layer2.go:23 (Deklaration) und layer2.go:38 (Zuweisung)
```

## Solution

1. `minimax *MiniMaxClient` aus Layer2 struct entfernen
2. `minimax: mm` aus NewLayer2 entfernen
3. Lokale Variable `mm` in NewLayer2 behalten (wird an chain weitergegeben)
4. Tests pruefen die direkt auf das Feld zugreifen (sollte keine geben)

## Acceptance Criteria

- [ ] Layer2 struct hat kein `minimax` Feld mehr
- [ ] `NewLayer2` erzeugt `mm` nur als lokale Variable fuer `NewFallbackChain(mm)`
- [ ] `go build ./...` kompiliert
- [ ] `go test ./internal/summarization/` gruen
- [ ] `go vet ./...` clean

## Affected Files

- `internal/summarization/layer2.go` - Struct + Constructor
- Ggf. `_test.go` die das Feld referenzieren

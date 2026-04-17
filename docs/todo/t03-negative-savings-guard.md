# T03 - Negative Savings Guard einbauen

**Status:** done
**Priority:** medium
**Files:** `internal/proxy/handler.go`, `internal/compression/layer1.go`

## Problem

Spec-Prinzip **"Zero-Downside Guarantee"** (spec+.md Section 1, Design Principle 1):
> "If compression would degrade quality, skip it. Uncompressed passthrough is always the fallback."

In der Praxis kann Layer-1 den Output vergroessern:

```
input_orig=9 input_comp=11 saved=-2 ratio=1.22 layers=[]
```

Ursachen:
- Structure-Extraction fuegt Header hinzu (`[Structural summary of ...]`)
- Dedup-Referenzen (`[Duplicate of message N ...]`) koennen laenger sein als sehr kurze Contents
- Delta-Encoding Header (`[Delta from message N to M for path]`) bei sehr kleinen Diffs

Der Code hat keinen Guard auf Gesamt-Ebene. Einzelne Sub-Layer haben Length-Checks, aber die
kombinierte Wirkung mehrerer Sub-Layer wird nicht geprueft.

## Fix-Strategie

### Guard 1: Handler-Ebene (global)

In `handleCompressibleRequest`, nach Layer-1 und Layer-2:

```go
// Zero-downside guarantee: if compression made things worse, use original messages.
if compressedTokens >= origTokens && origTokens > 0 {
    compressedMessages = messages  // revert to original
    compressedTokens = origTokens
    // Clear layer savings tracking
    appliedLayers = nil
}
```

Das ist der einfachste und sicherste Guard. Er greift nur wenn die Gesamtkompression
negativ ist - also wenn alle Sub-Layer zusammen den Output vergroessert haben.

### Guard 2: Sub-Layer-Ebene (optional, defensiv)

In `compressMessage`: Jede Transformation sollte bereits nur angewendet werden wenn das
Ergebnis kuerzer ist. Das ist teilweise implementiert (`len(compacted) < len(text)` Checks),
aber nicht konsistent.

**Prioritaet:** Guard 1 ist verpflichtend (Spec-konformitaet). Guard 2 ist nice-to-have.

## Implementation Plan

### handler.go

1. Nach Layer-1 Compress + Layer-2 ApplyToMessages + Prompt-Cache:
   ```go
   compressedTokens := tokens.CountMessages(compressedMessages)
   if compressedTokens >= origTokens && origTokens > 0 {
       slog.Debug("compression expanded output, reverting to original",
           "orig", origTokens, "comp", compressedTokens)
       compressedMessages = messages
       compressedTokens = origTokens
       appliedLayers = nil
   }
   ```

2. Token-Zaehlung **vor** diesem Guard durchfuehren (aktuell wird `compressedTokens`
   erst spaeter berechnet - muss frueher passieren oder der Guard kommt nach der
   existierenden Zaehlung).

### Neue Tests

1. `TestHandleCompressibleRequest_negativeSavingsReverted`:
   - Mock Layer-1 der Messages vergroessert
   - Verify: upstream bekommt original (uncompressed) Body
   - Verify: savings = 0, ratio = 1.0

2. `TestLayer1_neverExpandsOutput`:
   - Kuenstliche Messages die durch Dedup-Referenzen groesser werden wuerden
   - Verify: Gesamtergebnis ist nicht groesser als Input

## Sub-Tasks

- [ ] Guard in `handleCompressibleRequest` einfuegen (nach compressedTokens-Berechnung)
- [ ] Test: negative savings -> revert to original
- [ ] Test: positive savings -> compression applied normally
- [ ] Edge case: origTokens=0 -> kein Guard (avoid div-by-zero)
- [ ] `go test -race ./internal/proxy/...` clean
- [ ] `go test ./...` clean

## Verification

```bash
go test -run TestHandleCompressibleRequest -v ./internal/proxy/
go test -run TestLayer1 -v ./internal/compression/
go test ./...
```

Kein `saved=-2` mehr in Test-Logs.

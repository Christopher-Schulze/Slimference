# T02 - Streaming Relay: Context-Cancel Bug fixen

**Status:** done
**Priority:** high
**Files:** `internal/proxy/streaming.go`, `internal/proxy/streaming_test.go`

## Problem

`TestStreamingRelay_contextCancelled` schlaegt fehl mit 5s Timeout:

```
streaming_test.go:330: streamingRelay did not return within 5s after context cancel
```

Die `streamingRelay`-Funktion prueft `ctx.Done()` nur **zwischen** `scanner.Scan()`-Iterationen:

```go
for scanner.Scan() {
    select {
    case <-ctx.Done():
        return outputTokens
    default:
    }
    // ... write to client ...
}
```

Wenn der Upstream-Server nicht sendet (oder langsam), blockiert `scanner.Scan()` unbegrenzt.
Der Context-Check wird nie erreicht. Die Goroutine laeuft weiter bis der Upstream die
Connection schliesst.

## Root-Cause

`bufio.Scanner.Scan()` ist eine blockierende Operation die wartet bis ein `\n` gelesen wird
oder der Reader EOF zurueckgibt. Es gibt keine Moeglichkeit, den Scanner von aussen abzubrechen.
Der `select { case <-ctx.Done() }` ist wirkungslos solange `scanner.Scan()` blockiert.

## Fix-Strategie

### Option A: context-cancellable Reader Wrapper (empfohlen)

Einen `io.Reader`-Wrapper bauen der `ctx.Done()` beim Lesen beachtet:

```go
type ctxReader struct {
    ctx context.Context
    r   io.Reader
}

func (cr *ctxReader) Read(p []byte) (n int, err error) {
    // Non-blocking check first
    select {
    case <-cr.ctx.Done():
        return 0, cr.ctx.Err()
    default:
    }
    // Use a goroutine to make the actual read cancellable
    type result struct {
        n   int
        err error
    }
    ch := make(chan result, 1)
    go func() {
        n, err := cr.r.Read(p)
        ch <- result{n, err}
    }()
    select {
    case <-cr.ctx.Done():
        return 0, cr.ctx.Err()
    case r := <-ch:
        return r.n, r.err
    }
}
```

Dann in `streamingRelay`:

```go
cr := &ctxReader{ctx: ctx, r: upstreamResp.Body}
scanner := bufio.NewScanner(cr)
```

Wenn `ctx` cancelled wird, returned der Reader einen Error, `scanner.Scan()` returned `false`,
die Schleife beendet sich.

**Vorteil:** Minimal invasiv, aendert nur wie der Reader erstellt wird.
**Nachteil:** Eine Goroutine pro Read-Call. Aber SSE-Lines sind typischerweise klein und schnell,
das ist kein Performance-Problem.

### Option B: Scanner in separater Goroutine + Channel

Komplexer, erfordert groessere Refactoring.

**Empfehlung: Option A.**

## Implementation Plan

### streaming.go

1. `ctxReader` Typ und `Read`-Methode hinzufuegen (kann in streaming.go oder eigenem File sein)
2. In `streamingRelay`: `upstreamResp.Body` durch `&ctxReader{ctx, upstreamResp.Body}` ersetzen
3. Bestehenden `select { case <-ctx.Done() }` im Loop kann bleiben (doppelt-gemoppelt, aber unschaedlich)

### streaming_test.go

1. `TestStreamingRelay_contextCancelled` muss gruen werden:
   - Test-Server der eine Zeile sendet, dann 10s wartet (nie EOF)
   - Context nach Empfang der ersten Zeile cancellen
   - streamingRelay muss innerhalb von ~1s zurueckkehren
2. Existierende Tests duerfen nicht brechen

## Sub-Tasks

- [x] `ctxReader` implementieren
- [x] `streamingRelay` anpassen: ctxReader statt raw body
- [x] `TestStreamingRelay_contextCancelled` gruen
- [x] Alle existierenden Streaming-Tests gruen (kein Regression)
- [x] `go test -race ./internal/proxy/...` clean

## Verification

```bash
go test -run TestStreamingRelay -v ./internal/proxy/
go test -race ./internal/proxy/...
```

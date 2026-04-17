# T07: Rate-Limiter context.Background() blockt forever

**Date:** 2026-04-17
**Severity:** HIGH
**Status:** DONE
**Parent:** `docs/todo.md` -> Audit-Fixes

## Problem

`internal/summarization/minimax.go:167`:

```go
_ = c.limiter.Wait(context.Background())
```

`limiter.Wait()` blockiert bis ein Rate-Limit-Token verfuegbar ist. Mit `context.Background()` gibt es keinen Weg den Aufruf abzubrechen. Bei 10 RPM, Burst 1, koennen mehrere parallele Compression-Jobs bis zu 6 Sekunden warten - ohne dass ein Shutdown oder Cancel das abbrechen kann.

## Risk

- Proxy-Shutdown kann bis zu 6 Sekunden verzoegert werden wenn gerade ein `limiter.Wait()` aktiv ist
- Bei Fehlkonfiguration (sehr niedriges RPM) kann das System komplett blockieren
- Der `_ =` ignoriert den Fehler vom Wait (z.B. Context-Cancel)

## Solution

Option A (empfohlen): `Summarize` bekommt einen `ctx context.Context` Parameter der durchgereicht wird:
- `limiter.Wait(ctx)` statt `limiter.Wait(context.Background())`
- Caller (Layer2.RunCompressionJob) erzeugt einen Context mit Timeout (z.B. 30s)
- Bei Shutdown: Context canceln -> Wait bricht sofort ab

Option B: Interner Timeout-Context:
- `ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)`
- `defer cancel()`
- Simpler aber weniger kontrollierbar

## Acceptance Criteria

- [x] `Summarize` nutzt einen abbrechbaren Context fuer `limiter.Wait()`
- [x] Test: Rate-Limiter-Wait kann durch Context-Cancel abgebrochen werden
- [x] Keine Regression in bestehenden Retry-Tests
- [x] `go test -race ./internal/summarization/` clean

## Implementation

- `Summarizer` Interface: `Summarize(ctx context.Context, inputText string, ...)` 
- `MiniMaxClient.Summarize`: `c.limiter.Wait(ctx)` mit Error-Return bei Cancel
- `FallbackChain.Summarize`: reicht `ctx` an Provider durch
- Caller nutzen `context.Background()` als Default
- Test `TestMiniMaxClient_Summarize_rateLimiterCancelled`: pre-cancelled Context -> sofortiger Fehler

## Affected Files

- `internal/summarization/minimax.go` - Summarize Signatur aendern
- `internal/summarization/fallback.go` - Summarizer Interface aendern
- `internal/summarization/layer2.go` - Aufrufer anpassen
- `internal/summarization/progressive.go` - Aufrufer anpassen
- Alle zugehoerigen `_test.go` Dateien

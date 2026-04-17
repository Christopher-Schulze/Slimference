# T05 - Codex Hook-Integration: Rewrite auf hooks.json (v0.117.0+)

**Status:** done
**Priority:** high
**Files:** `internal/hooks/codex.go`, `internal/hooks/verify.go`, `cmd/slimference/main.go`

## Hintergrund

Codex CLI (OpenAI) hat **seit v0.117.0 (Maerz 2026)** ein echtes Hook-System mit PreToolUse-Events.
Die aktuelle Slimference-Implementation nutzt den veralteten AGENTS.md-Instruction-Ansatz
(~70-85% Adoption, suggestion-based). Der Hook-Ansatz bietet 100% Adoption wie bei Claude Code.

### Was sich geaendert hat

**Alt (aktueller Code):**
- `~/.codex/AGENTS.md` mit Instruction: "prefix shell commands with slimference filter"
- Suggestion-based, ~70-85% Adoption
- Keine echte Command-Interception

**Neu (Codex v0.117.0+):**
- `~/.codex/hooks.json` mit PreToolUse Bash-Matcher
- `~/.codex/config.toml` mit `[features] codex_hooks = true`
- Echt: Hook kriegt JSON auf stdin, kann Command rewrite auf stdout, exit 0/1/2
- 100% Adoption (wie Claude Code PreToolUse)

### Codex Hook Wire Format (offizielle Doku)

**PreToolUse Input (stdin JSON):**
```json
{
  "session_id": "...",
  "turn_id": "...",
  "tool_name": "Bash",
  "tool_use_id": "...",
  "tool_input": {
    "command": "git status"
  },
  "hook_event_name": "PreToolUse",
  "model": "gpt-5.4",
  "cwd": "/path/to/project"
}
```

**PreToolUse Output (stdout JSON):**
```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "Destructive command blocked"
  }
}
```

Exit Codes:
- 0 + kein Output: passthrough (Codex fuehrt Command unverandert aus)
- 0 + JSON mit `permissionDecision: "deny"`: Command blockiert
- 2 + stderr Text: auch "deny"
- `permissionDecision: "allow"` und `"ask"` werden geparsed aber **noch nicht unterstuetzt** (fail open)

**Wichtig:** `updatedInput` wird geparsed aber nicht unterstuetzt. Das heisst: **Command-Rewriting
ist aktuell NICHT moeglich ueber den Hook** (anders als bei Claude Code).

### Realitaetscheck: Was funktioniert heute?

1. **Deny:** Ja - Command wird blockiert
2. **Allow:** Geparst, nicht unterstuetzt (fail open = Command laeuft trotzdem)
3. **Command Rewrite:** Nicht unterstuetzt - es gibt kein `updatedInput` das Codex tatsaechlich anwendet
4. **PreToolUse firet nur fuer Bash-Tool**, nicht fuer Edit/Write/MCP/etc.

### Finale Umsetzung

Der finale Slimference-Modus fuer Codex ist ein **Hybrid aus PreToolUse und
PostToolUse**, angepasst an den realen Codex-Contract:

- **PreToolUse** ruft `slimference rewrite -- "$CMD"` auf.
- Wenn Slimference den Command nur durchlassen wuerde, returned der Hook nichts
  und Codex fuehrt normal aus.
- Wenn Slimference den Command blocken muss, returned der Hook einen
  `decision:"block"`-Payload mit Grund.
- Wenn Slimference einen Rewrite empfehlen wuerde, blockt der Hook den
  Original-Command und gibt eine klare Rerun-Anweisung fuer den umgeschriebenen
  Command zurueck. Das ist kein natives `updatedInput`, aber der sicherste
  verfuegbare Downside-Minimizer fuer die aktuelle Codex-Hook-Flaeche.
- **PostToolUse** ruft `slimference posttool` auf und liefert bei Nutzen
  `hookSpecificOutput.additionalContext`, also kompaktierte Bash-Resultate als
  zusaetzlichen Kontext statt eines riskanten Output-Replacements.

Damit erreicht Codex:
- sichere Command-Blocks fuer deny/ask/rewrite-Faelle
- Output-Kompaktierung nach der Tool-Ausfuehrung
- Proxy-Routing fuer Layer 1-3 ueber `openai_base_url`

Nicht erreicht wird weiterhin ein natives Codex-`updatedInput`-Rewrite. Diese
Grenze ist Codex-seitig, nicht Slimference-seitig.

## Implementation Plan

### 1. hooks/codex.go: Komplett-Umschreibung

```go
// InstallCodex installs the Slimference hooks for Codex CLI.
//
// Writes:
//   1. ~/.slimference/hooks/codex-pre-tool.sh
//   2. ~/.slimference/hooks/codex-post-tool.sh
//   3. ~/.codex/hooks.json - PreToolUse + PostToolUse Bash hook entries
//   4. ~/.codex/config.toml - openai_base_url + codex_hooks feature flag (merge, nicht overwrite)

func InstallCodex(home string, slimferenceCmd string) error
func RemoveCodex(home string) error
func CodexHookInstalled(home string) bool
```

### 2. Hook-Script

Der PreToolUse-Hook ruft `slimference rewrite`, der PostToolUse-Hook
`slimference posttool` auf:

```bash
#!/bin/bash
# ~/.slimference/hooks/codex-pre-tool.sh
slimference rewrite -- "$CMD"

# ~/.slimference/hooks/codex-post-tool.sh
slimference posttool
```

### 3. config.toml Patching

```go
// patchCodexConfig only adds missing keys and never overwrites an existing
// openai_base_url value.
```

### 4. slimference posttool

Dedizierter Subcommand fuer Codex PostToolUse:
- Input: PostToolUse JSON mit `tool_response` Feld
- Filter: `tool_response` durch `RunPipeline` schicken
- Output: `hookSpecificOutput.additionalContext` oder exit 0 (passthrough)

### 5. verify.go Aktualisierung

- Codex: Check `~/.codex/hooks.json` exists und enthaelt PreToolUse + PostToolUse
- Check `~/.codex/config.toml` hat `openai_base_url` und `codex_hooks = true`
- Check `~/.slimference/hooks/codex-pre-tool.sh` und `codex-post-tool.sh` existieren

### 6. Tests

- `TestInstallCodexHooks`: temp home dir, verify hooks.json + config.toml
- `TestRemoveCodexHooks`: verify cleanup
- `TestVerifyCodexHooks`: verify detection
- `TestPatchCodexConfig`: verify merge (bestehende Settings erhalten)
- `TestHandlePostToolCmd_*`: verify JSON-input -> `additionalContext` output

## Sub-Tasks

- [x] `internal/hooks/codex.go` umschreiben: AGENTS.md -> hooks.json + config.toml
- [x] `slimference posttool` Pfad fuer PostToolUse hinzufuegen
- [x] `internal/hooks/verify.go`: Codex-Verify aktualisieren
- [x] `cmd/slimference/main.go`: hook install/remove/verify fuer Codex aktualisieren
- [x] config.toml Patching: merge bestehender Config (nicht overwrite)
- [x] Tests: alle Codex-Hook-Tests umschreiben
- [x] `docs/documentation.md`: Codex-Setup-Sektion aktualisieren

## Resolved Questions

- `slimference hook install codex` setzt `openai_base_url` automatisch, aber nur
  wenn noch kein Eintrag vorhanden ist.
- Eine bestehende `openai_base_url` wird nicht ueberschrieben; Slimference
  merged nur fehlende Keys in `~/.codex/config.toml`.
- Codex bekommt zwei Hooks: PreToolUse fuer Rewrite/Block-Entscheidungen und
  PostToolUse fuer Output-Kompaktierung via `slimference posttool`.

## Deferred

Kein manueller Live-Codex-Lauf in diesem Pass. Der Nutzer wollte explizit
keine Live- oder Smoke-Tests aus diesem Projektlauf heraus.

## Files Affected

- `internal/hooks/codex.go` (major rewrite)
- `internal/hooks/verify.go` (Codex section update)
- `cmd/slimference/main.go` (rewrite subcommand extension, hook install/remove codex path)
- `internal/hooks/codex_test.go` (rewrite tests)
- `docs/documentation.md` (Codex setup section)

## Verification

```bash
# Unit tests
go test ./internal/hooks/... -v
go test ./cmd/slimference/... -v

# Manual integration
slimference hook install codex
cat ~/.codex/hooks.json    # Verify hooks.json
cat ~/.codex/config.toml   # Verify config patch
slimference hook verify codex  # SHA-256 check
slimference hook status        # Shows codex installed

# Cleanup
slimference hook remove codex
slimference hook status        # Shows codex not installed
```

## Open Questions

Keine offenen Codefragen mehr. Offene Live-Verifikation bleibt bewusst ausserhalb
dieses Passes.

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

### Fazit fuer Layer 0

Der **PreToolUse Hook kann Commands blocken (deny)**, aber er kann sie **nicht umschreiben**.
Das bedeutet fuer Layer 0:

**Option A (empfohlen): PostToolUse-Filterung**
- PreToolUse: pass through (exit 0, kein output)
- Codex fuehrt Command aus
- PostToolUse Hook: kriegt `tool_response` (den Command-Output)
- Hook kann den Output **ersetzen** indem er `continue: false` returned und
  den ersetzten Text als Feedback gibt
- Codex nimmt den Feedback-Text als "ersetzten Tool-Output"

**Option B: PreToolUse Deny + slimference filter als Ersatz**
- PreToolUse: deny + reason = "Run: slimference filter <command>"
- Codex zeigt dem User die deny-Nachricht
- Model muss dann selbst "slimference filter <command>" aufrufen
- ~70-85% Adoption (Model folgt dem Hinweis meistens)
- Schlechter als Option A weil das Model den Hinweis ignorieren kann

**Option C: Hybrid (best of both)**
- `slimference hook install codex` schreibt PostToolUse-Hook fuer Layer 0
- Proxy (Layer 1-3) via `openai_base_url` in config.toml
- Layer 0 = PostToolUse Output-Filterung
- Layer 1-3 = Proxy-Kompression

### Entscheidung: Option A/C (PostToolUse-basiert)

PostToolUse Hook kann den Bash-Output ersetzen:
```json
{
  "decision": "block",
  "reason": "[git status] 3 modified, 1 staged, 2 untracked",
  "hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "Output filtered by Slimference"
  }
}
```

Wenn `decision: "block"` + `continue: false` returned wird, ersetzt Codex den Tool-Output
mit dem reason-Text. Das Model bekommt den gefilterten Output.

**Einschraenkung:** `PostToolUse` kann Side-Effects nicht rueckgaengig machen (das Command
wurde bereits ausgefuehrt). Aber das ist bei Layer 0 kein Problem - wir wollen den Output
filtern, nicht die Ausfuehrung verhindern.

## Implementation Plan

### 1. hooks/codex.go: Komplett-Umschreibung

```go
// InstallCodexHooks installs the Slimference hooks for Codex CLI.
//
// Writes:
//   1. ~/.codex/hooks.json - PostToolUse Bash hook
//   2. ~/.codex/config.toml - openai_base_url + codex_hooks feature flag (merge, nicht overwrite)
//
// PostToolUse (nicht PreToolUse) weil:
// - PreToolUse kann Commands nur deny, nicht rewrite
// - PostToolUse kann tool_response ersetzen via decision:block + reason
// - Layer 0 filtert OUTPUT, nicht die Command-Ausfuehrung

func InstallCodexHooks(home string) error
func RemoveCodexHooks(home string) error
func VerifyCodexHooks(home string) (bool, string, error)
func CodexHookInstalled(home string) bool
```

### 2. Hook-Script

Der PostToolUse-Hook ruft `slimference filter-post` auf (neuer Subcommand oder erweiterter rewrite-Pfad):

```bash
#!/bin/bash
# ~/.slimference/hooks/codex-post-tool.sh
# Reads PostToolUse JSON from stdin, filters tool_response, outputs replacement
slimference rewrite --post-tool-use
```

Oder direkter: der Hook-Command ist `slimference rewrite --post-tool-use`.

### 3. config.toml Patching

```go
// PatchCodexConfig merges Slimference settings into ~/.codex/config.toml
// without overwriting existing user settings.
//
// Adds:
//   openai_base_url = "http://127.0.0.1:8990"
//   [features]
//   codex_hooks = true
//
// Preserves all existing config values.

func PatchCodexConfig(home string, proxyAddr string) error
func UnpatchCodexConfig(home string) error
```

### 4. slimference rewrite --post-tool-use

Erweiterter `rewrite` Subcommand der auch PostToolUse-JSON verarbeiten kann:
- Input: PostToolUse JSON mit `tool_response` Feld
- Filter: `tool_response` durch `RunPipeline` schicken
- Output: `{"decision":"block","reason":"<filtered_output>"}` oder exit 0 (passthrough)

### 5. verify.go Aktualisierung

- Codex: Check `~/.codex/hooks.json` exists + SHA-256
- Check `~/.codex/config.toml` hat `openai_base_url` und `codex_hooks = true`
- Check `~/.slimference/hooks/codex-post-tool.sh` exists

### 6. Tests

- `TestInstallCodexHooks`: temp home dir, verify hooks.json + config.toml
- `TestRemoveCodexHooks`: verify cleanup
- `TestVerifyCodexHooks`: verify detection
- `TestPatchCodexConfig`: verify merge (bestehende Settings erhalten)
- `TestPostToolUseFilter`: verify JSON-input -> filtered JSON-output

## Sub-Tasks

- [ ] `internal/hooks/codex.go` umschreiben: AGENTS.md -> hooks.json + config.toml
- [ ] `slimference rewrite --post-tool-use` Pfad hinzufuegen
- [ ] `internal/hooks/verify.go`: Codex-Verify aktualisieren
- [ ] `cmd/slimference/main.go`: hook install/remove/verify fuer Codex aktualisieren
- [ ] config.toml Patching: merge bestehender Config (nicht overwrite)
- [ ] Tests: alle Codex-Hook-Tests umschreiben
- [ ] `docs/documentation.md`: Codex-Setup-Sektion aktualisieren
- [ ] Manuelles Testing: `slimference hook install codex` + Codex nutzen

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

- [ ] Soll `slimference hook install codex` automatisch `openai_base_url` setzen,
      oder soll der User das manuell machen? (Empfehlung: automatisch, da Teil der Integration)
- [ ] Was passiert wenn `~/.codex/config.toml` bereits `openai_base_url` hat mit anderem Wert?
      (Empfehlung: warnen + nicht overwrite)
- [ ] PostToolUse `decision: "block"` ersetzt den Output wirklich? Manuelles Testing noetig.

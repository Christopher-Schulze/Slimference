package integrate

import (
	"fmt"
	"os"
	"strings"
)

// Install wires Claude Code and/or Codex. Idempotent. Returns a Report
// detailing every side effect (or would-be side effect in DryRun).
func Install(opts Options) Report {
	if opts.Client == "" {
		opts.Client = "all"
	}
	home, err := opts.resolveHome()
	if err != nil {
		return Report{Errors: []string{err.Error()}}
	}
	shell := os.Getenv("SHELL")
	rep := Report{}

	doClaude := opts.Client == "all" || opts.Client == "claude"
	doCodex := opts.Client == "all" || opts.Client == "codex"

	if doClaude {
		rc := DetectRCFile(home, shell)
		rep.Claude.Name = "claude"
		rep.Claude.ConfigPath = rc.Path
		if opts.DryRun {
			rep.Writes = append(rep.Writes, WriteEvent{Path: rc.Path,
				Action: "DRY_RUN_wrote_block"})
			rep.Claude.State = ClientFullyWired
			rep.Claude.Details = append(rep.Claude.Details,
				"dry-run: would export ANTHROPIC_BASE_URL="+opts.resolveProxyURL())
		} else {
			evt, err := WriteRCBlock(rc.Path, rc.Flavor, opts.resolveProxyURL())
			if err != nil {
				rep.Errors = append(rep.Errors,
					fmt.Sprintf("claude shell-rc: %v", err))
			} else {
				rep.Writes = append(rep.Writes, evt)
			}
			rep.Claude = DetectClaude(home, shell)
		}
	}

	if doCodex {
		rep.Codex.Name = "codex"
		rep.Codex.ConfigPath = CodexConfigPath(home)
		if opts.DryRun {
			rep.Writes = append(rep.Writes, WriteEvent{
				Path:   CodexConfigPath(home),
				Action: "DRY_RUN_wrote_block",
			})
			rep.Codex.State = ClientFullyWired
			rep.Codex.Details = append(rep.Codex.Details,
				"dry-run: would set openai_base_url + chatgpt_base_url to "+opts.resolveProxyURL())
		} else {
			evt, err := WriteCodexBlock(home, opts.resolveProxyURL())
			if err != nil {
				rep.Errors = append(rep.Errors,
					fmt.Sprintf("codex config: %v", err))
			} else {
				rep.Writes = append(rep.Writes, evt)
			}
			rep.Codex = DetectCodex(home)
		}
	}

	rep.Daemon = DetectDaemon(opts.resolveProxyURL())
	return rep
}

// Remove reverts every wiring side-effect.
func Remove(opts Options) Report {
	if opts.Client == "" {
		opts.Client = "all"
	}
	home, err := opts.resolveHome()
	if err != nil {
		return Report{Errors: []string{err.Error()}}
	}
	shell := os.Getenv("SHELL")
	rep := Report{}

	doClaude := opts.Client == "all" || opts.Client == "claude"
	doCodex := opts.Client == "all" || opts.Client == "codex"

	if doClaude {
		rc := DetectRCFile(home, shell)
		if opts.DryRun {
			rep.Writes = append(rep.Writes, WriteEvent{Path: rc.Path,
				Action: "DRY_RUN_removed_block"})
		} else {
			evt, err := RemoveRCBlock(rc.Path)
			if err != nil {
				rep.Errors = append(rep.Errors,
					fmt.Sprintf("claude shell-rc remove: %v", err))
			} else {
				rep.Writes = append(rep.Writes, evt)
			}
		}
		rep.Claude = DetectClaude(home, shell)
	}

	if doCodex {
		if opts.DryRun {
			rep.Writes = append(rep.Writes, WriteEvent{
				Path:   CodexConfigPath(home),
				Action: "DRY_RUN_removed_block",
			})
		} else {
			evt, err := RemoveCodexBlock(home)
			if err != nil {
				rep.Errors = append(rep.Errors,
					fmt.Sprintf("codex config remove: %v", err))
			} else {
				rep.Writes = append(rep.Writes, evt)
			}
		}
		rep.Codex = DetectCodex(home)
	}

	rep.Daemon = DetectDaemon(opts.resolveProxyURL())
	return rep
}

// DiffPreview returns a human-readable summary of the writes that Install or
// Remove would perform. Callers typically pair this with a `--dry-run` flag.
func DiffPreview(rep Report) string {
	var sb strings.Builder
	if len(rep.Writes) == 0 && len(rep.Errors) == 0 {
		sb.WriteString("(no changes)\n")
		return sb.String()
	}
	for _, w := range rep.Writes {
		sb.WriteString(fmt.Sprintf("  %s\t%s\n", w.Action, w.Path))
	}
	for _, e := range rep.Errors {
		sb.WriteString(fmt.Sprintf("  ERROR\t%s\n", e))
	}
	return sb.String()
}

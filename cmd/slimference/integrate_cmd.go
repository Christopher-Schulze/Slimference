package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/slimference/slimference/internal/hooks"
	"github.com/slimference/slimference/internal/integrate"
)

// handleIntegrateCmd dispatches `slimference integrate <status|install|remove>`.
// T65 one-shot installer for Claude Code + Codex wiring.
func handleIntegrateCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference integrate <status|install|remove> [--client all|claude|codex] [--dry-run] [--json]")
		exitFn(1)
		return
	}
	opts, extra, err := parseIntegrateFlags(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "integrate: %v\n", err)
		exitFn(1)
		return
	}
	switch args[0] {
	case "status":
		runIntegrateStatus(opts, extra)
	case "install":
		runIntegrateInstall(opts, extra)
	case "remove", "uninstall":
		runIntegrateRemove(opts, extra)
	case "emergency-off":
		runIntegrateEmergencyOff(opts, extra)
	default:
		fmt.Fprintf(os.Stderr, "integrate: unknown verb %q\n", args[0])
		exitFn(1)
	}
}

type integrateExtra struct {
	JSON        bool
	InstallHook bool // default true; --no-hook disables
}

func parseIntegrateFlags(args []string) (integrate.Options, integrateExtra, error) {
	opts := integrate.Options{Client: "all"}
	extra := integrateExtra{InstallHook: true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--dry-run":
			opts.DryRun = true
		case a == "--force":
			opts.Force = true
		case a == "--json":
			extra.JSON = true
		case a == "--no-hook":
			extra.InstallHook = false
		case a == "--client":
			if i+1 >= len(args) {
				return opts, extra, fmt.Errorf("--client requires a value")
			}
			i++
			v := args[i]
			if v != "all" && v != "claude" && v != "codex" && v != "daemon" {
				return opts, extra, fmt.Errorf("--client must be all|claude|codex|daemon, got %q", v)
			}
			opts.Client = v
		case strings.HasPrefix(a, "--client="):
			v := strings.TrimPrefix(a, "--client=")
			if v != "all" && v != "claude" && v != "codex" && v != "daemon" {
				return opts, extra, fmt.Errorf("--client must be all|claude|codex|daemon, got %q", v)
			}
			opts.Client = v
		case a == "--proxy-url":
			if i+1 >= len(args) {
				return opts, extra, fmt.Errorf("--proxy-url requires a value")
			}
			i++
			opts.ProxyURL = args[i]
		case strings.HasPrefix(a, "--proxy-url="):
			opts.ProxyURL = strings.TrimPrefix(a, "--proxy-url=")
		default:
			return opts, extra, fmt.Errorf("unknown flag %q", a)
		}
	}
	return opts, extra, nil
}

func runIntegrateStatus(opts integrate.Options, extra integrateExtra) {
	rep := integrate.Status(opts)
	if extra.JSON {
		emitJSON(rep)
		return
	}
	renderIntegrateReport("Status", rep)
}

func runIntegrateInstall(opts integrate.Options, extra integrateExtra) {
	rep := integrate.Install(opts)

	// Hooks: delegate to the existing internal/hooks package. We always
	// try both client hooks unless --client narrows it.
	if extra.InstallHook && !opts.DryRun {
		home, _ := osUserHomeDir()
		slimCmd := "slimference"
		if opts.Client == "all" || opts.Client == "claude" {
			if err := installClaudeHookFn(home, slimCmd); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("claude hook: %v", err))
			}
		}
		if opts.Client == "all" || opts.Client == "codex" {
			if err := installCodexHookFn(home, slimCmd); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("codex hook: %v", err))
			}
		}
	}

	if extra.JSON {
		emitJSON(rep)
		return
	}
	renderIntegrateReport("Install", rep)
	if !opts.DryRun {
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  1. `exec $SHELL -l`  (reload your shell so the env var takes effect)")
		fmt.Println("  2. `slimference service install` if the daemon is not yet running")
		fmt.Println("  3. Launch Claude Code / Codex - traffic now flows through Slimference.")
	}
}

func runIntegrateRemove(opts integrate.Options, extra integrateExtra) {
	rep := integrate.Remove(opts)
	if extra.InstallHook && !opts.DryRun {
		home, _ := osUserHomeDir()
		if opts.Client == "all" || opts.Client == "claude" {
			if err := removeClaudeHookFn(home); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("claude hook remove: %v", err))
			}
		}
		if opts.Client == "all" || opts.Client == "codex" {
			if err := removeCodexHookFn(home); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("codex hook remove: %v", err))
			}
		}
	}
	if extra.JSON {
		emitJSON(rep)
		return
	}
	renderIntegrateReport("Remove", rep)
	if !opts.DryRun {
		fmt.Println()
		fmt.Println("Reload your shell (`exec $SHELL -l`) so ANTHROPIC_BASE_URL is unset.")
	}
}

func runIntegrateEmergencyOff(opts integrate.Options, extra integrateExtra) {
	// Emergency-off = remove + attempt daemon stop. We do not fail loud on
	// the daemon stop - user may already have unloaded it.
	opts.Client = "all"
	rep := integrate.Remove(opts)
	if extra.InstallHook && !opts.DryRun {
		home, _ := osUserHomeDir()
		if err := removeClaudeHookFn(home); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("claude hook remove: %v", err))
		}
		if err := removeCodexHookFn(home); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("codex hook remove: %v", err))
		}
	}
	// Best-effort daemon stop; errors non-fatal.
	if !opts.DryRun {
		if err := daemonStopFn(); err != nil {
			rep.Errors = append(rep.Errors,
				fmt.Sprintf("daemon stop: %v (continue)", err))
		}
		_ = daemonUninstallFn
	}
	if extra.JSON {
		emitJSON(rep)
		return
	}
	renderIntegrateReport("Emergency-off", rep)
	fmt.Println()
	fmt.Println("All Slimference wiring removed. Reload your shell to continue.")
}

func renderIntegrateReport(title string, rep integrate.Report) {
	fmt.Printf("Slimference Integration - %s\n", title)
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Claude Code: %-15s  %s\n", rep.Claude.State, rep.Claude.BinaryPath)
	for _, d := range rep.Claude.Details {
		fmt.Printf("  - %s\n", d)
	}
	fmt.Printf("Codex:       %-15s  %s\n", rep.Codex.State, rep.Codex.BinaryPath)
	for _, d := range rep.Codex.Details {
		fmt.Printf("  - %s\n", d)
	}
	daemonLine := "offline"
	if rep.Daemon.Running {
		daemonLine = "running"
		if rep.Daemon.PID != 0 {
			daemonLine = fmt.Sprintf("running (pid %d)", rep.Daemon.PID)
		}
	}
	fmt.Printf("Daemon:      %s  health=%s\n", daemonLine, rep.Daemon.Health)
	for _, d := range rep.Daemon.Details {
		fmt.Printf("  - %s\n", d)
	}
	if len(rep.Writes) > 0 {
		fmt.Println()
		fmt.Println("Writes:")
		fmt.Print(integrate.DiffPreview(rep))
	}
	if len(rep.Errors) > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Errors:")
		for _, e := range rep.Errors {
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
	}
}

func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// Injectable shims so integrate tests can bypass the real installer paths.
var _ = hooks.InstalledStatus // prevent unused import when only aliases used

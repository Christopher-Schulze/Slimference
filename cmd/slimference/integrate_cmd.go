package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/slimference/slimference/internal/hooks"
	"github.com/slimference/slimference/internal/integrate"
)

// handleIntegrateCmd dispatches the legacy/config-patch integration surface.
// The scoped Codex CLI path uses `slimference install` plus
// `slimference codex run`.
func handleIntegrateCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference integrate <status|install|remove> [--client all|claude|codex] [--dry-run] [--json]  (legacy/config-patch mode)")
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
	var ok bool
	opts, ok = codexOnlyIntegrateOptions(opts)
	if !ok {
		fmt.Fprintln(os.Stderr, "integrate: Claude Code is parked; no files changed. Use RTK for Claude Code.")
		return
	}
	rep := integrate.Install(opts)

	// Hooks: delegate to the existing internal/hooks package. We always
	// install only Codex hooks while Claude Code is parked.
	if extra.InstallHook && !opts.DryRun {
		home, _ := osUserHomeDir()
		slimCmd := "slimference"
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
		fmt.Println("Legacy config-patch mode complete.")
		fmt.Println()
		fmt.Println("Preferred Phase H Codex path:")
		fmt.Println("  1. `slimference install`")
		fmt.Println("  2. `slimference status --preflight`")
		fmt.Println("  3. `slimference codex run -- <prompt>`")
		fmt.Println("  4. Optional shared CLI/App route: `slimference codex enable`")
		fmt.Println()
		fmt.Println("If you intentionally use legacy config-patch mode:")
		fmt.Println("  1. `exec $SHELL -l`  (reload your shell so env/config edits apply)")
		fmt.Println("  2. `slimference service install` if the daemon is not yet running")
	}
}

func runIntegrateRemove(opts integrate.Options, extra integrateExtra) {
	var ok bool
	opts, ok = codexOnlyIntegrateOptions(opts)
	if !ok {
		fmt.Fprintln(os.Stderr, "integrate: Claude Code is parked; no files changed. Use RTK for Claude Code.")
		return
	}
	rep := integrate.Remove(opts)
	if extra.InstallHook && !opts.DryRun {
		home, _ := osUserHomeDir()
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
		fmt.Println("Reload your shell (`exec $SHELL -l`) if legacy config-patch state was active.")
	}
}

func runIntegrateEmergencyOff(opts integrate.Options, extra integrateExtra) {
	// Emergency-off = remove + attempt daemon stop. We do not fail loud on
	// the daemon stop - user may already have unloaded it.
	opts.Client = "codex"
	rep := integrate.Remove(opts)
	if extra.InstallHook && !opts.DryRun {
		home, _ := osUserHomeDir()
		if err := removeCodexHookFn(home); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("codex hook remove: %v", err))
		}
	}
	// Best-effort daemon stop + launchd plist uninstall; errors non-fatal.
	// emergency-off is the panic button: we do everything we can to undo
	// side effects even if individual steps complain.
	if !opts.DryRun {
		if err := daemonStopFn(); err != nil {
			rep.Errors = append(rep.Errors,
				fmt.Sprintf("daemon stop: %v (continue)", err))
		}
		if err := daemonUninstallFn(); err != nil {
			rep.Errors = append(rep.Errors,
				fmt.Sprintf("launchd uninstall: %v (continue)", err))
		}
	}
	if extra.JSON {
		emitJSON(rep)
		return
	}
	renderIntegrateReport("Emergency-off", rep)
	fmt.Println()
	fmt.Println("Legacy/config-patch wiring removed. Reload your shell to continue.")
}

func codexOnlyIntegrateOptions(opts integrate.Options) (integrate.Options, bool) {
	switch opts.Client {
	case "claude":
		return opts, false
	case "all":
		opts.Client = "codex"
	}
	return opts, true
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

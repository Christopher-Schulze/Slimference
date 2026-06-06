// Command slimference is a transparent HTTP reverse proxy that applies multi-layer
// token compression to LLM API requests, extending effective usage limits by 2-3x.
//
// Usage:
//
//	slimference                    # Start TUI + proxy
//	slimference config init        # Generate default config file
//	slimference config show        # Print resolved config
//	slimference test anthropic     # Test Anthropic reachability
//	slimference test openai        # Test OpenAI reachability
//	slimference doctor             # Run all diagnostics
//	slimference stats today        # Print today's stats
//	slimference stats prompt-cache week --json # Prompt-cache report
//	slimference gain today         # Layer-0/filter/cache/output/proxy telemetry (--by-command, --by-parser, --cache, --output, --proxy)
//	slimference plan inspect       # Dry-run cross-layer planner decisions
//	slimference filter -- <cmd>    # Layer-0: subprocess + ANSI strip + DB log
//	slimference rewrite -- <cmd>   # Print command line; or pipe hook JSON (field "command") on stdin
//	slimference posttool          # Compact PostToolUse hook JSON from stdin for Codex
//	slimference codexhook <event> # Codex lifecycle hook entry points
//	slimference install            # Install Codex-only Phase H surface
//	slimference codex run -- <prompt> # Scoped Codex CLI with fail-open
//	slimference enable             # Advanced shared Codex CLI/App route
//	slimference lab enable         # Global lab: arm SNI-peek after explicit root-arm
//	slimference debug paths        # Show resolved config / filter.db / tee paths
//	slimference debug last         # Last Layer-0 row from filter.db (--json)
//	slimference debug summary week # Aggregate filter_runs for today|week|month|all
//	slimference debug tail 30      # Newest 30 rows (default 20, max 500, --json)
//	slimference debug replay f.jsonl # Replay session JSONL (per-request token breakdown)
//	slimference version            # Print version
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/buildinfo"
	"github.com/slimference/slimference/internal/codexroute"
	"github.com/slimference/slimference/internal/compactsignal"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/crosstool"
	"github.com/slimference/slimference/internal/daemon"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/hooks"
	"github.com/slimference/slimference/internal/integrate"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/readcache"
	"github.com/slimference/slimference/internal/repetition"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/slogutil"
	"github.com/slimference/slimference/internal/tlsca"
	"github.com/slimference/slimference/internal/tlsdial"
	"github.com/slimference/slimference/internal/tlsproof"
	"github.com/slimference/slimference/internal/toolarchive"
	"github.com/slimference/slimference/internal/transparent"
	"github.com/slimference/slimference/internal/tui"
	"github.com/slimference/slimference/internal/types"
	"golang.org/x/term"
)

var version = buildinfo.Version

// Injectable package-level vars for OS/driver boundaries - enables in-process error injection in tests.
var (
	runTUIFn                     = runTUI
	osGetwd                      = os.Getwd
	osExecutable                 = os.Executable
	osStartProcess               = os.StartProcess
	osOpenFile                   = os.OpenFile
	osMkdirAll                   = os.MkdirAll
	osUserHomeDir                = os.UserHomeDir
	termIsTerminalFn             = term.IsTerminal
	readStdinAll                 = func() ([]byte, error) { return io.ReadAll(os.Stdin) }
	osWriteFile                  = os.WriteFile
	testInterceptTimeout         = 60 * time.Second
	testInterceptShutdownTimeout = 5 * time.Second
	exitFn                       = os.Exit
	// recordHookFlightImpl is the testable indirection through which
	// failopen.go records degraded-session telemetry. Tests inject a
	// capture function; production points at recordHookFlight.
	recordHookFlightImpl     = recordHookFlight
	resolveFilterDBPathFn    = resolveFilterDBPath
	resolveTeeDirFn          = resolveTeeDir
	filterDefaultDataDirFn   = filter.DefaultDataDir
	writeGainByCommandCSV    = analytics.WriteGainByCommandCSV
	writeGainByParserCSV     = analytics.WriteGainByParserCSV
	writeGainSummaryCSV      = analytics.WriteGainSummaryCSV
	writeOutputReduceCSV     = analytics.WriteOutputReduceCSV
	writePromptCacheCSV      = analytics.WritePromptCacheCSV
	writeProxyFlightGainCSV  = analytics.WriteProxyFlightGainCSV
	replaySessionFn          = dbg.ReplaySession
	daemonIsRunningFn        = daemon.IsRunning
	daemonStopFn             = daemon.StopDaemon
	daemonInstallLaunchdFn   = daemon.InstallLaunchd
	daemonUninstallFn        = daemon.UninstallLaunchd
	daemonFormatStatusFn     = daemon.FormatStatus
	daemonRunFn              = runDaemonWithSlimferenceReload
	daemonRunWithReloadFn    = daemon.RunDaemonWithReload
	installClaudeHookFn      = hooks.InstallClaude
	installCodexHookFn       = hooks.InstallCodex
	removeClaudeHookFn       = hooks.RemoveClaude
	removeCodexHookFn        = hooks.RemoveCodex
	loadTUIStateFn           = tui.LoadPersistedState
	proxyRunFn               = proxyRun
	newTransparentNetworkFn  = func() proxyNetworkManager { return transparent.NewManager() }
	newTransparentKeychainFn = func() proxyKeychain { return transparent.NewKeychain() }
	newTransparentLaunchFn   = func() proxyLaunchAgent { return transparent.NewLaunchAgent() }
	transparentProxyHealthFn = defaultProxyHealthCheck

	// runTUI sub-components: injectable for test coverage of post-startup paths.
	configLoadFn = func() (*config.Config, error) {
		cfg, _, err := config.LoadWithOptions(config.LoadOptions{
			ExplicitPath: explicitConfigPath,
		})
		return cfg, err
	}
	runTUIAfterStartFn = runTUIAfterStart
	newRemoteProxyFn   = func(cfg *config.Config) tui.ProxyInterface { return newRemoteProxyAdapter(cfg) }
	newProxyFn         = proxy.New
	proxyStartRunnerFn = func(p *proxy.Proxy) error { return p.Start() }
	proxyHasListenerFn = func(p *proxy.Proxy) bool { return p.HasListener() }
	timeAfterFn        = time.After
	newTickerFn        = time.NewTicker
	startProxyFn       = func(cfg *config.Config) (func(ctx context.Context) error, error) {
		p := newProxyFn(cfg)
		ensureSlimDataDir()
		startProxyInstance = p
		startProxyConfig = cfg
		appsMgr := wirePhaseG(p, cfg)
		startProxyAppsManager = appsMgr
		startProxyHostsCleanup = applyHostsPatch(cfg)
		_, sniCancel := startSNIPeekEngineFn(p, cfg, appsMgr)
		startProxySNICancel = sniCancel
		// Only the managed daemon may own ~/.slimference/run/daemon.pid.
		// Foreground/TUI starts are short-lived and must not overwrite
		// the SIGHUP target used by `slimference enable/disable`.
		startProxyPIDCleanup = nil
		runner := proxyStartRunnerFn
		hasListener := proxyHasListenerFn
		after := timeAfterFn
		newTicker := newTickerFn
		errCh := make(chan error, 1)
		go func() {
			if err := runner(p); err != nil && !isServerClosed(err) {
				errCh <- err
			}
		}()

		deadline := after(proxyStartTimeout)
		ticker := newTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case err := <-errCh:
				return nil, err
			case <-ticker.C:
				if hasListener(p) {
					return p.Shutdown, nil
				}
			case <-deadline:
				if hasListener(p) {
					return p.Shutdown, nil
				}
				return nil, fmt.Errorf("proxy start timeout after %s", proxyStartTimeout)
			}
		}
	}
	proxyStartTimeout       = 200 * time.Millisecond
	daemonStartTimeout      = 3 * time.Second
	daemonStartPollInterval = 50 * time.Millisecond
	// runTeaProgramFn injects tea.Program.Run so tests can return immediately without a terminal.
	runTeaProgramFn = (*tea.Program).Run
	// tuiSendProxyEventFn injects tui.SendProxyEvent so progSender.send can be tested without a running program.
	tuiSendProxyEventFn   func(*tea.Program, types.RequestMetrics) = tui.SendProxyEvent
	startDetachedDaemonFn                                          = startDetachedDaemon
	osEnvironFn                                                    = os.Environ
	makeSignalChanFn                                               = func() chan os.Signal {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		return ch
	}
)

func main() {
	proxy.Version = version
	args := os.Args[1:]

	// Early dispatch: help and version must never try to open the TUI.
	if wantsHelp(args) {
		printHelp(args)
		return
	}
	if wantsVersion(args) {
		fmt.Printf("slimference v%s\n", version)
		return
	}

	// --config <path> is a global flag honoured by every code path that calls
	// configLoadFn. We parse it here, stash it in a package-level variable
	// that configLoadFn consults, and strip it from args so downstream
	// subcommand parsers do not see it.
	if p, rest := extractConfigFlag(args); p != "" {
		explicitConfigPath = p
		args = rest
		os.Args = append([]string{os.Args[0]}, rest...)
	}

	// Explicit headless / non-TTY foreground mode (T44).
	if wantsHeadless(args) {
		runHeadlessFn(args)
		return
	}

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		handleSubcommand(args)
		return
	}

	// No args: on a non-TTY we refuse to launch the TUI and emit help with exit 2
	// so Docker / systemd / CI paths surface a clear signal instead of a TTY error.
	if len(args) == 0 && !termIsTerminalFn(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "slimference: no TTY detected. Use --no-tui for headless mode or --help.")
		printHelp(nil)
		exitFn(2)
		return
	}

	runTUIFn()
}

// wantsHelp reports whether args contain a help flag or the bare 'help' subcommand.
func wantsHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "--help", "-h", "help":
		return true
	}
	return false
}

// wantsVersion reports whether args explicitly ask for the version banner.
// The 'version' subcommand is handled by handleSubcommand.
func wantsVersion(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "--version", "-V":
		return true
	}
	return false
}

// wantsHeadless reports whether args request the headless foreground proxy mode.
// Scans for --no-tui / --headless; stops at '--' argument terminator or at a
// known subcommand token so flags buried inside subcommand args do not flip the
// top-level mode. Env override SLIMFERENCE_HEADLESS=1 also counts.
func wantsHeadless(args []string) bool {
	if os.Getenv("SLIMFERENCE_HEADLESS") == "1" {
		return true
	}
	knownSubcommands := map[string]bool{
		"version": true, "config": true, "test": true, "doctor": true,
		"stats": true, "gain": true, "savings": true, "compress-preview": true, "watch": true, "filter": true, "rewrite": true,
		"readhook": true, "posttool": true, "codexhook": true, "checkpoint": true, "expand": true, "expand-body": true,
		"hook": true, "debug": true, "daemon": true, "start": true, "stop": true,
		"restart": true, "service": true, "integrate": true, "bypass": true,
		"completion": true, "trust": true,
		"app-server": true,
		"help":       true,
	}
	// Flags that consume the next token as a value; their value must not be
	// mistaken for a subcommand.
	flagWithValue := map[string]bool{
		"--port": true, "-port": true, "--sliding-window": true,
		"--log-level": true, "--log-format": true, "--log-file": true,
		"--color": true, "--config": true,
	}
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--" {
			return false
		}
		if a == "--no-tui" || a == "--headless" {
			return true
		}
		if flagWithValue[a] {
			skipNext = true
			continue
		}
		if knownSubcommands[a] {
			return false
		}
	}
	return false
}

// printHelp writes usage information to stdout. topic selects top-level or
// subcommand help. Nil / empty args prints the top-level banner.
func printHelp(args []string) {
	topic := ""
	if len(args) >= 2 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		topic = args[1]
	}
	if topic == "" {
		fmt.Fprint(os.Stdout, helpTopLevel())
		return
	}
	fmt.Fprint(os.Stdout, helpForSubcommand(topic))
}

// Injected for tests.
var runHeadlessFn = runHeadless

// explicitConfigPath mirrors the value of the top-level --config flag once
// main() has parsed it. Empty when the flag was not used. The default
// configLoadFn closure in main_vars consults this on each Load call so every
// subcommand, the TUI, and headless mode honour the same override.
var explicitConfigPath string

// extractConfigFlag scans args for "--config <path>" or "--config=<path>"
// and returns the path plus the argument slice with the flag removed.
// Unknown / absent flag yields ("", args).
func extractConfigFlag(args []string) (string, []string) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--config" {
			if i+1 < len(args) {
				p := args[i+1]
				out = append(out, args[i+2:]...)
				return p, out
			}
			// Trailing --config with no value: leave as-is so downstream
			// parsing can emit a proper error.
			out = append(out, args[i:]...)
			return "", out
		}
		if strings.HasPrefix(a, "--config=") {
			p := strings.TrimPrefix(a, "--config=")
			out = append(out, args[i+1:]...)
			return p, out
		}
		out = append(out, a)
	}
	return "", out
}

// progSender delivers proxy request events to the BubbleTea program via a buffered channel.
type progSender struct {
	ch chan *tea.Program
}

func (s *progSender) send(rm types.RequestMetrics) {
	select {
	case prog := <-s.ch:
		tuiSendProxyEventFn(prog, rm)
		s.ch <- prog // put it back so the next call can use it
	default:
	}
}

// runTUI starts the proxy and launches the BubbleTea TUI. Blocks until quit.
func runTUI() {
	cfg, err := configLoadFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		exitFn(1)
		return
	}

	// Apply CLI flag overrides (take priority over config file).
	applyTUIFlags(cfg, os.Args[1:])

	setupLogging(cfg)
	runTUIAfterStartFn(newRemoteProxyFn(cfg))
}

// runTUIAfterStart sets up OS signal handling and runs the BubbleTea TUI.
// Called by runTUI after the proxy has started successfully.
func runTUIAfterStart(p tui.ProxyInterface) {
	sigCh := makeSignalChanFn()
	done := make(chan struct{})
	defer func() {
		signal.Stop(sigCh)
		close(done)
	}()

	go func() {
		select {
		case <-sigCh:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = p.Shutdown(ctx)
			exitFn(0)
		case <-done:
		}
	}()

	model := tui.NewModel(p)
	model.SetServiceControl(&serviceControlAdapter{})
	if home, err := osUserHomeDir(); err == nil {
		claude, codex := hooks.InstalledStatus(home)
		model.SetHookStatus(tui.HookStatus{Claude: claude, Codex: codex})
	}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := runTeaProgramFn(program); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		exitFn(1)
	}

	// Graceful proxy shutdown on normal TUI quit (user pressed 'q' or similar).
	// Signal-triggered shutdowns are handled by the goroutine above; this covers the normal path.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = p.Shutdown(ctx)
}

// applyTUIFlags parses CLI flags from args and applies them as overrides to cfg.
// Flags have priority over config file and environment variables (spec §13.3).
// Supported flags:
//
//	--port <n>             Listen port override
//	--sliding-window <n>   Layer 1 sliding window size
//	--no-layer1            Disable Layer 1 (deterministic compression)
//	--no-layer2            Disable Layer 2 (response caching)
//	--log-level <level>    Log level: debug, info, warn, error
func applyTUIFlags(cfg *config.Config, args []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		nextArg := func() (string, bool) {
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		switch a {
		case "--port", "-port":
			if v, ok := nextArg(); ok {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					cfg.Proxy.ListenPort = n
				}
			}
		case "--sliding-window":
			if v, ok := nextArg(); ok {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					cfg.Compression.SlidingWindow = n
				}
			}
		case "--no-layer1":
			cfg.Compression.Layer1Enabled = false
		case "--no-layer2":
			cfg.Compression.Layer2Enabled = false
		case "--log-level":
			if v, ok := nextArg(); ok {
				cfg.Logging.Level = v
			}
		}
	}
}

// handleSubcommand dispatches non-TUI subcommands.
func handleSubcommand(args []string) {
	switch args[0] {
	case "version":
		fmt.Printf("slimference v%s\n", version)

	case "config":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: slimference config <init|show>")
			exitFn(1)
		}
		handleConfigCmd(args[1:])

	case "test":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: slimference test <anthropic|openai|intercept>")
			exitFn(1)
		}
		handleTestCmd(args[1:])

	case "doctor":
		handleDoctorCmd()

	case "stats":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: slimference stats <today|week|month|prompt-cache [today|week|month|all] [--json|--csv]>")
			exitFn(1)
		}
		handleStatsCmd(args[1:])

	case "gain":
		handleGainCmd(args[1:])

	case "savings":
		handleSavingsCmd(args[1:])

	case "quality":
		handleQualityCmd(args[1:])

	case "plan":
		handlePlanCmd(args[1:])

	case "soak":
		handleSoakCmd(args[1:])

	case "compress-preview":
		handleCompressPreviewCmd(args[1:])

	case "watch":
		handleWatchCmd(args[1:])

	case "filter":
		handleFilterCmd(args[1:])

	case "rewrite":
		handleRewriteCmd(args[1:])

	case "readhook":
		handleReadHookCmd(args[1:])

	case "posttool":
		handlePostToolCmd(args[1:])

	case "codexhook":
		handleCodexHookCmd(args[1:])

	case "checkpoint":
		handleCheckpointCmd(args[1:])

	case "expand":
		handleExpandCmd(args[1:])

	case "expand-body":
		handleExpandBodyCmd(args[1:])

	case "hook":
		handleHookCmd(args[1:])

	case "debug":
		handleDebugCmd(args[1:])

	case "daemon":
		handleDaemonCmd(args[1:])

	case "start":
		if handleNoArgLifecycleHelpOrError("start", args[1:]) {
			return
		}
		handleStartCmd()

	case "stop":
		if handleNoArgLifecycleHelpOrError("stop", args[1:]) {
			return
		}
		handleStopCmd()

	case "restart":
		if handleNoArgLifecycleHelpOrError("restart", args[1:]) {
			return
		}
		handleRestartCmd()

	case "service":
		handleServiceCmd(args[1:])

	case "integrate":
		handleIntegrateCmd(args[1:])

	case "bypass":
		handleBypassCmd(args[1:])

	case "output-reduce":
		handleOutputReduceCmd(args[1:])

	case "completion":
		handleCompletionCmd(args[1:])

	case "trust":
		handleTrustCmd(args[1:])

	case "capture-session":
		handleCaptureSessionCmd(args[1:])

	case "proxy":
		handleProxyCmd(args[1:])

	case "codex":
		handleCodexCmd(args[1:])

	case "app-server":
		handleCodexDesktopAppServerShim(args[1:])

	case "lab":
		handleLabCmd(args[1:])

	case "install":
		handleInstallCmd(args[1:])

	case "uninstall":
		handleUninstallCmd(args[1:])

	case "enable":
		handleEnableCmd(args[1:])

	case "disable":
		handleDisableCmd(args[1:])

	case "status":
		handleStatusCmd(args[1:])

	case "cert-trust":
		handleCertTrustCmd(args[1:])

	case "root-arm":
		handleRootArmCmd(args[1:])

	case "root-disarm":
		handleRootDisarmCmd(args[1:])

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Run 'slimference' to start the TUI, or use: config, test, doctor, stats, gain, plan, filter, rewrite, readhook, posttool, codexhook, checkpoint, expand, expand-body, hook, debug, daemon, start, stop, restart, service, output-reduce, completion, trust, capture-session, codex, lab, proxy, version")
		exitFn(1)
	}
}

func handleNoArgLifecycleHelpOrError(command string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		fmt.Fprint(os.Stdout, helpForSubcommand(command))
		return true
	}
	fmt.Fprintf(os.Stderr, "%s: unexpected argument %q\n", command, args[0])
	fmt.Fprintf(os.Stderr, "usage: slimference %s\n", command)
	exitFn(2)
	return true
}

// syncPermissionDeny merges global [filter] deny_patterns with project .slimference/filters.toml (cwd).
func syncPermissionDeny(wd string) {
	var global []string
	if cfg, err := config.Load(); err == nil {
		global = cfg.Filter.DenyPatterns
	}
	proj := filter.LoadMergedDenyPatterns(wd)
	out := append(append([]string{}, global...), proj...)
	filter.SetExtraDenyPatterns(out)
}

// layer0PermissionCheck implements spec+.md Layer-0 permission outcomes before running
// or rewriting a command: deny → exit 2, ask (sudo) → exit 3, else allow (0, "").
func layer0PermissionCheck(cmdLine string) (exitCode int, msg string) {
	wd, _ := os.Getwd()
	syncPermissionDeny(wd)
	if den, why := filter.DeniedShellCommand(cmdLine); den {
		return 2, why
	}
	if filter.AskRequired(cmdLine) {
		return 3, "slimference: sudo requires SLIMFERENCE_CONFIRM_SUDO=1"
	}
	return 0, ""
}

func resolveFilterDBPath() (string, error) {
	if p := os.Getenv("SLIMFERENCE_FILTER_DB"); p != "" {
		return p, nil
	}
	if cfg, err := config.Load(); err == nil {
		if p := strings.TrimSpace(cfg.Filter.FilterDB); p != "" {
			return filepath.Clean(config.ExpandHomePath(p)), nil
		}
	}
	return filter.DefaultFilterDBPath()
}

func resolveTeeDir() (string, error) {
	if p := os.Getenv("SLIMFERENCE_TEE_DIR"); p != "" {
		return p, nil
	}
	if cfg, err := config.Load(); err == nil {
		if p := strings.TrimSpace(cfg.Filter.TeeDir); p != "" {
			return filepath.Clean(config.ExpandHomePath(p)), nil
		}
	}
	return filter.DefaultTeeDir()
}

// mustOpenFilterDB opens the resolved filter.db. Returns (nil, false) if the file is missing.
// Exits on other failures.
func mustOpenFilterDB() (*sql.DB, bool) {
	path, err := resolveFilterDBPathFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "filter db path: %v\n", err)
		exitFn(1)
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		fmt.Fprintf(os.Stderr, "filter db: %v\n", err)
		exitFn(1)
	}
	db, err := filter.OpenDB(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open filter db: %v\n", err)
		exitFn(1)
	}
	return db, true
}

// handleFilterCmd runs Layer-0 pipeline: subprocess → ANSI strip on stdout → optional SQLite row.
func handleFilterCmd(args []string) {
	streamMode := false
	var argv []string
	for _, a := range args {
		if a == "--" {
			continue
		}
		if a == "--stream" {
			streamMode = true
			continue
		}
		argv = append(argv, a)
	}
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference filter [--stream] [--] <command> [args...]")
		exitFn(1)
	}
	if streamMode {
		// T94 streaming-aware Layer 0: tail-friendly pump that emits
		// compacted lines as they arrive instead of waiting for exit.
		opts := filter.StreamOptions{StripANSI: true, DedupConsecutive: true}
		code, err := filter.RunStreamingPipeline(context.Background(), argv, os.Stdout, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stream filter: %v\n", err)
			exitFn(1)
		}
		exitFn(code)
		return
	}
	cmdLine := strings.Join(argv, " ")
	if code, msg := layer0PermissionCheck(cmdLine); code != 0 {
		fmt.Fprintln(os.Stderr, msg)
		exitFn(code)
	}
	wd, err := osGetwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		exitFn(1)
	}
	maxOut := config.Defaults().Filter.PassthroughMaxChars
	if cfg, err := config.Load(); err == nil {
		maxOut = cfg.Filter.PassthroughMaxChars
	}
	pr := filter.RunPipeline(context.Background(), wd, argv, maxOut)
	_, _ = os.Stdout.Write(pr.Stdout)
	_, _ = os.Stderr.Write(pr.Stderr)

	if pr.Err != nil || pr.Code != 0 {
		teeDir, _ := resolveTeeDir()
		if teeDir != "" {
			if p, err := filter.WriteTeeRecovery(teeDir, pr.RawStdout, pr.RawStderr); err == nil {
				fmt.Fprintf(os.Stderr, "slimference: saved raw output to %s\n", p)
			}
		}
	}

	dbPath, _ := resolveFilterDBPath()
	if dbPath != "" && pr.Err == nil {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err == nil {
			if db, err := filter.OpenDB(dbPath); err == nil {
				defer db.Close()
				label := "[" + filter.ClassifyCommand(argv) + "] " + strings.Join(argv, " ")
				_ = filter.RecordFilterRun(db, label, wd,
					pr.InputTokens, pr.OutputTokens, pr.SavingsPct, time.Now())
			}
		}
	}

	if pr.Err != nil {
		fmt.Fprintf(os.Stderr, "filter: %v\n", pr.Err)
		exitFn(1)
	}
	exitFn(pr.Code)
}

func handleRewriteCmd(args []string) {
	// Panic-safety: any unexpected runtime error in the rewrite
	// pipeline degrades to silent passthrough so the user's shell never
	// breaks because slimference crashed. t164 fail-open contract.
	defer guardHook(FailOpenRewrite, nil)()
	// rewriteEmit applies the §4.2 rewrite pipeline to cmdLine and exits with
	// the appropriate hook exit code:
	//   0 = rewrite applied, stdout contains rewritten command
	//   1 = no filter matched, hook should passthrough original unchanged
	//   2 = deny rule matched, hook should block the command
	//   3 = ask (sudo) required, hook should prompt before running
	rewriteEmit := func(cmdLine string) {
		// Permission check runs first (deny/ask take priority over filter matching).
		if code, msg := layer0PermissionCheck(cmdLine); code != 0 {
			recordHookFlight("hook_pre", "", "Bash", msg, len(cmdLine), len(cmdLine), nil, nil)
			fmt.Fprintln(os.Stderr, msg)
			exitFn(code)
		}

		// Load excluded commands from config (commands never rewritten).
		var excluded []string
		if cfg, err := configLoadFn(); err == nil {
			excluded = cfg.Hooks.ExcludeCommands
		}

		// Apply the §4.2 compound-command rewrite engine.
		rewritten, hasFilter := filter.RewriteCommand(cmdLine, excluded)
		if !hasFilter {
			// No filter applies - exit 1 signals the hook to passthrough unchanged.
			recordHookFlight("hook_pre", "", "Bash", "passthrough", len(cmdLine), len(cmdLine), nil, nil)
			exitFn(1)
		}
		recordHookFlight("hook_pre", "", "Bash", "rewritten", len(cmdLine), len(rewritten), []int{0}, nil)
		fmt.Println(rewritten)
		exitFn(0)
	}

	var parts []string
	for _, a := range args {
		if a == "--" {
			continue
		}
		parts = append(parts, a)
	}
	if len(parts) == 0 {
		if termIsTerminalFn(int(os.Stdin.Fd())) {
			fmt.Fprintln(os.Stderr, "usage: slimference rewrite [--] <command words...>   (or pipe hook JSON on stdin)")
			exitFn(1)
		}
		b, err := readStdinAll()
		if err != nil {
			fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
			exitFn(1)
		}
		cmd, err := filter.ExtractCommandFromHookJSON(b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			exitFn(1)
		}
		rewriteEmit(cmd)
		return
	}
	rewriteEmit(strings.Join(parts, " "))
}

func handlePostToolCmd(args []string) {
	for _, arg := range args {
		if arg != "" && arg != "--" {
			fmt.Fprintln(os.Stderr, "usage: slimference posttool   (pipe PostToolUse hook JSON on stdin)")
			exitFn(1)
		}
	}
	if termIsTerminalFn(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "usage: slimference posttool   (pipe PostToolUse hook JSON on stdin)")
		exitFn(1)
	}

	payload, err := readStdinAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		exitFn(1)
	}
	// Panic-safety guard for the heavy posttool compaction pipeline.
	// Engages after payload is read so the recovery has bytes to emit.
	// t164 fail-open contract.
	defer guardHook(FailOpenPostTool, payload)()
	cfg := config.Defaults()
	if loaded, err := configLoadFn(); err == nil {
		cfg = loaded
	}
	details, err := filter.ExtractPostToolDetailsFromHookJSON(payload)
	if err != nil {
		failOpenCodexPostTool(payload, err)
		return
	}
	stopWatchdog := startCodexPostToolWatchdog(payload, postToolTimeoutDuration(cfg))
	defer stopWatchdog()
	if !details.HasToolResponse {
		recordHookFlight("hook_post", details.SessionID, postToolFlightToolName(details), "skip_no_response", len(payload), len(payload), []int{1}, nil)
		return
	}
	if minTokens := cfg.Hooks.CodexPostToolMinTokens; postToolBelowMinTokens(details.ToolResponse, minTokens) {
		recordHookFlight("hook_post", details.SessionID, postToolFlightToolName(details), "skip_tiny", len(details.ToolResponse), len(details.ToolResponse), []int{1}, nil)
		return
	}

	wd, err := osGetwd()
	if err != nil {
		wd = ""
	}
	maxOut := cfg.Filter.PassthroughMaxChars

	readCtx := hookFileReadContext(wd, details)
	compacted, changed := filter.CompactCapturedOutputWithContext(wd, details.CommandLine, details.ToolResponse, maxOut, readCtx)
	observePostToolTurnState(wd, details)
	if out, ok := applyPostToolCrossToolDedup(wd, details); ok {
		compacted = out
		changed = true
	}

	// T93 cross-session pattern mining: when the same (session, tool,
	// command, output) tuple has been observed multiple times, replace
	// the captured output with a marker pointing at the first message.
	// Best-effort: storage errors leave the original behaviour intact.
	if home, err := osUserHomeDir(); err == nil && details.SessionID != "" {
		if repDB, repErr := repetition.Open(repetition.DefaultPath(home)); repErr == nil {
			turnID := currentHookTurnID(sessions.DefaultHookStateDir(home), details.SessionID)
			count, firstMsg, _ := repetition.Record(repDB, repetition.Key{
				SessionID: details.SessionID,
				TurnID:    turnID,
				ToolName:  details.ToolName,
				Command:   details.CommandLine,
				Output:    details.ToolResponse,
			}, 0)
			_ = repDB.Close()
			if count >= 3 {
				compacted = []byte(repetition.Marker(details.ToolName, firstMsg, count))
				changed = true
			}
		}
	}
	emitReplacement := codexPostToolShouldEmitReplacement(changed, len(details.ToolResponse), len(compacted))

	if home, err := osUserHomeDir(); err == nil {
		turnID := currentHookTurnID(sessions.DefaultHookStateDir(home), details.SessionID)
		entry, archiveErr := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
			ToolName:  details.ToolName,
			ToolUseID: details.ToolUseID,
			SessionID: details.SessionID,
			TurnID:    turnID,
			Command:   details.CommandLine,
			Output:    details.ToolResponse,
			Preview:   string(compacted),
		})
		if archiveErr == nil && entry != nil {
			if !emitReplacement {
				recordCodexPostToolAccounting(details, changed, len(details.ToolResponse), len(details.ToolResponse), nil)
				return
			}
			context := codexPostToolArchiveContext(*entry)
			recordCodexPostToolAccounting(details, changed, len(details.ToolResponse), len(context), codexPostToolDecisionEntries(details, len(details.ToolResponse), len(compacted), len(context)))
			out := codexPostToolReplacement(context)
			if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
				fmt.Fprintf(os.Stderr, "encode posttool output: %v\n", err)
				exitFn(1)
			}
			return
		}
	}

	if !emitReplacement {
		recordCodexPostToolAccounting(details, changed, len(details.ToolResponse), len(details.ToolResponse), nil)
		return
	}

	context := "Recent Bash output was compacted locally."
	if details.CommandLine != "" {
		context = fmt.Sprintf("Bash output for %q was compacted locally.\n%s", details.CommandLine, compacted)
	} else {
		context = fmt.Sprintf("Bash output was compacted locally.\n%s", compacted)
	}

	recordCodexPostToolAccounting(details, changed, len(details.ToolResponse), len(context), codexPostToolDecisionEntries(details, len(details.ToolResponse), len(compacted), len(context)))
	out := codexPostToolReplacement(context)
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode posttool output: %v\n", err)
		exitFn(1)
	}
}

func handleClaudePostToolCmd(args []string) {
	for _, arg := range args {
		if arg != "" && arg != "--" {
			fmt.Fprintln(os.Stderr, "usage: slimference claudeposttool   (pipe Claude PostToolUse hook JSON on stdin)")
			exitFn(1)
		}
	}
	if !claudePostToolEnabled() {
		return
	}
	if termIsTerminalFn(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "usage: slimference claudeposttool   (pipe Claude PostToolUse hook JSON on stdin)")
		exitFn(1)
	}
	payload, err := readStdinAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		exitFn(1)
	}
	defer guardHook(FailOpenPostTool, payload)()
	cfg := config.Defaults()
	if loaded, err := configLoadFn(); err == nil {
		cfg = loaded
	}
	details, err := filter.ExtractPostToolDetailsFromHookJSON(payload)
	if err != nil {
		recordHookFlight("hook_post_claude", "", "PostToolUse", "parse_fail_open", len(payload), len(payload), []int{1}, err)
		return
	}
	if !details.HasToolResponse {
		recordHookFlight("hook_post_claude", details.SessionID, postToolFlightToolName(details), "skip_no_response", len(payload), len(payload), []int{1}, nil)
		return
	}
	if minTokens := cfg.Hooks.CodexPostToolMinTokens; postToolBelowMinTokens(details.ToolResponse, minTokens) {
		recordHookFlight("hook_post_claude", details.SessionID, postToolFlightToolName(details), "skip_tiny", len(details.ToolResponse), len(details.ToolResponse), []int{1}, nil)
		return
	}

	wd, err := osGetwd()
	if err != nil {
		wd = ""
	}
	compacted, changed := filter.CompactCapturedOutputWithContext(wd, details.CommandLine, details.ToolResponse, cfg.Filter.PassthroughMaxChars, hookFileReadContext(wd, details))
	if out, ok := applyPostToolCrossToolDedup(wd, details); ok {
		compacted = out
		changed = true
	}
	if !changed || len(compacted) >= len(details.ToolResponse) {
		recordHookFlight("hook_post_claude", details.SessionID, postToolFlightToolName(details), "passthrough", len(details.ToolResponse), len(details.ToolResponse), []int{1}, nil)
		return
	}

	recordHookFlightEntries(
		"hook_post_claude",
		details.SessionID,
		postToolFlightToolName(details),
		"compressed",
		len(details.ToolResponse),
		len(compacted),
		[]int{1},
		claudePostToolDecisionEntries(details, len(details.ToolResponse), len(compacted)),
		nil,
	)
	if err := json.NewEncoder(os.Stdout).Encode(claudePostToolReplacement(string(compacted))); err != nil {
		fmt.Fprintf(os.Stderr, "encode claudeposttool output: %v\n", err)
		exitFn(1)
	}
}

func recordCodexPostToolAccounting(details filter.PostToolPayload, changed bool, originalBytes int, finalBytes int, entries []dbg.DecisionEntry) {
	recordHookFlightEntries("hook_post", details.SessionID, postToolFlightToolName(details), hookDecision(changed), originalBytes, finalBytes, []int{1}, entries, nil)
}

func codexPostToolDecisionEntries(details filter.PostToolPayload, originalBytes int, compactedBytes int, contextBytes int) []dbg.DecisionEntry {
	originalTokens := filter.EstimateTokensFromBytes(originalBytes)
	compactedTokens := filter.EstimateTokensFromBytes(compactedBytes)
	contextTokens := filter.EstimateTokensFromBytes(contextBytes)
	entries := []dbg.DecisionEntry{{
		ContentType:  "tool_result",
		Layer:        1,
		SubLayer:     "codex_posttool_compaction",
		Action:       "compressed",
		Reason:       postToolFlightToolName(details),
		TokensBefore: originalTokens,
		TokensAfter:  compactedTokens,
		SavedTokens:  originalTokens - compactedTokens,
		Settings: map[string]string{
			"command": details.CommandLine,
		},
	}}
	contextAdded := contextTokens - compactedTokens
	if contextAdded > 0 {
		entries = append(entries, dbg.DecisionEntry{
			ContentType:  "tool_result",
			Layer:        1,
			SubLayer:     "codex_hook_replacement_context",
			Action:       "overhead",
			Reason:       "replacement metadata and archive pointer",
			TokensBefore: compactedTokens,
			TokensAfter:  contextTokens,
			SavedTokens:  -contextAdded,
		})
	} else if contextAdded < 0 {
		entries = append(entries, dbg.DecisionEntry{
			ContentType:  "tool_result",
			Layer:        1,
			SubLayer:     "codex_archive_replacement",
			Action:       "replacement",
			Reason:       "archive context shorter than compacted preview",
			TokensBefore: compactedTokens,
			TokensAfter:  contextTokens,
			SavedTokens:  -contextAdded,
		})
	}
	return entries
}

const codexPostToolContextPreviewChars = 600

func codexPostToolArchiveContext(entry toolarchive.Entry) string {
	base := "Bash output compacted by Slimference."
	if entry.Command != "" {
		base = fmt.Sprintf("Bash output for %q compacted by Slimference.", entry.Command)
	}
	context := base + "\nRaw archive: " + entry.URI + "\nArchive ID: " + entry.ID
	preview := strings.TrimSpace(entry.Preview)
	if preview == "" {
		return context
	}
	return context + "\nPreview:\n" + toolarchive.DefaultPreview(preview, codexPostToolContextPreviewChars)
}

func postToolBelowMinTokens(text string, minTokens int) bool {
	if minTokens <= 0 {
		return false
	}
	if len(text) < minTokens*2 {
		return true
	}
	return filter.EstimateTokensFromText(text) < minTokens
}

func postToolTimeoutDuration(cfg *config.Config) time.Duration {
	seconds := config.Defaults().Hooks.CodexPostToolTimeoutSeconds
	if cfg != nil && cfg.Hooks.CodexPostToolTimeoutSeconds > 0 {
		seconds = cfg.Hooks.CodexPostToolTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func startCodexPostToolWatchdog(payload []byte, timeout time.Duration) func() {
	if timeout <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-timer.C:
			recordCodexPostToolTimeout(payload, timeout)
			exitFn(0)
		}
	}()
	return func() { close(done) }
}

func postToolFlightToolName(details filter.PostToolPayload) string {
	if details.ToolName != "" {
		return details.ToolName
	}
	return "PostToolUse"
}

func codexPostToolReplacement(context string) map[string]interface{} {
	return map[string]interface{}{
		"continue":   false,
		"stopReason": "Slimference compacted Bash output.",
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "PostToolUse",
			"additionalContext": context,
		},
	}
}

func claudePostToolReplacement(output string) map[string]interface{} {
	return map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName": "PostToolUse",
			"updatedToolOutput": map[string]interface{}{
				"stdout":      output,
				"stderr":      "",
				"interrupted": false,
				"isImage":     false,
			},
		},
	}
}

func claudePostToolDecisionEntries(details filter.PostToolPayload, originalBytes int, compactedBytes int) []dbg.DecisionEntry {
	originalTokens := filter.EstimateTokensFromBytes(originalBytes)
	compactedTokens := filter.EstimateTokensFromBytes(compactedBytes)
	return []dbg.DecisionEntry{{
		ContentType:  "tool_result",
		Layer:        1,
		SubLayer:     "claude_posttool_updated_output",
		Action:       "compressed",
		Reason:       postToolFlightToolName(details),
		TokensBefore: originalTokens,
		TokensAfter:  compactedTokens,
		SavedTokens:  originalTokens - compactedTokens,
		Settings: map[string]string{
			"command": details.CommandLine,
			"mode":    claudeHookMode(),
		},
	}}
}

func claudePostToolEnabled() bool {
	switch claudeHookMode() {
	case "max", "compact", "aggressive", "auto":
		return true
	default:
		return false
	}
}

func claudeHookMode() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("SLIMFERENCE_CLAUDE_HOOK_MODE")))
}

const (
	codexPostToolAutoReplaceMinOriginalTokens = 600
	codexPostToolAutoReplaceMinSavedTokens    = 400
	codexPostToolAutoReplaceMinSavingsPct     = 45
)

func codexPostToolShouldEmitReplacement(changed bool, originalBytes int, finalBytes int) bool {
	if !changed || finalBytes <= 0 {
		return false
	}
	switch codexHookMode() {
	case "compact", "aggressive":
		return true
	case "auto":
		if finalBytes >= originalBytes {
			return false
		}
		return codexPostToolAutoReplacementWorthIt(originalBytes, finalBytes)
	default:
		return false
	}
}

func codexPostToolAutoReplacementWorthIt(originalBytes int, finalBytes int) bool {
	originalTokens := filter.EstimateTokensFromBytes(originalBytes)
	if originalTokens < codexPostToolAutoReplaceMinOriginalTokens {
		return false
	}
	savedTokens := filter.EstimateTokensFromBytes(originalBytes - finalBytes)
	if savedTokens < codexPostToolAutoReplaceMinSavedTokens {
		return false
	}
	return savedTokens*100/originalTokens >= codexPostToolAutoReplaceMinSavingsPct
}

func failOpenCodexPostTool(payload []byte, err error) {
	sessionID := extractJSONText(payload, "session_id", "conversation_id")
	toolName := extractJSONText(payload, "tool_name", "toolName")
	if toolName == "" {
		toolName = "PostToolUse"
	}
	// Preserve the legacy decision label ("fail_open" with no reason
	// suffix) for backward compatibility with existing telemetry
	// dashboards; new fail-open paths use failOpenPassthrough() with a
	// categorised reason.
	recordHookFlight("hook_post", sessionID, toolName, "fail_open", len(payload), len(payload), []int{1}, err)
}

func recordCodexPostToolTimeout(payload []byte, timeout time.Duration) {
	sessionID := extractJSONText(payload, "session_id", "conversation_id")
	toolName := extractJSONText(payload, "tool_name", "toolName")
	if toolName == "" {
		toolName = "PostToolUse"
	}
	// Preserve legacy "timeout_fail_open" decision label for the same
	// reason as failOpenCodexPostTool above.
	recordHookFlight("hook_post", sessionID, toolName, "timeout_fail_open", len(payload), len(payload), []int{1}, fmt.Errorf("posttool timeout after %s", timeout))
}

func handleCodexHookCmd(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: slimference codexhook <session-start|permission-request|user-prompt-submit|posttool-timeout|stop>   (pipe Codex hook JSON on stdin)")
		exitFn(1)
	}
	if termIsTerminalFn(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "usage: slimference codexhook <session-start|permission-request|user-prompt-submit|posttool-timeout|stop>   (pipe Codex hook JSON on stdin)")
		exitFn(1)
	}
	payload, err := readStdinAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		exitFn(1)
	}
	// Lifecycle hooks must never break the session: t164 fail-open
	// contract. Any panic in the lifecycle handler degrades silently.
	defer guardHook(FailOpenCodexLifecycle, payload)()
	switch args[0] {
	case "session-start":
		handleCodexSessionStartHook(payload)
	case "permission-request":
		handleCodexPermissionRequestHook(payload)
	case "user-prompt-submit":
		handleCodexUserPromptSubmitHook(payload)
	case "posttool-timeout":
		recordCodexPostToolTimeout(payload, postToolTimeoutDuration(config.Defaults()))
	case "stop":
		handleCodexStopHook(payload)
	case "pre-compact":
		handleCodexPreCompactHook(payload)
	case "post-compact":
		handleCodexPostCompactHook(payload)
	default:
		fmt.Fprintf(os.Stderr, "unknown codexhook event: %s\n", args[0])
		exitFn(1)
	}
}

func handleCodexSessionStartHook(payload []byte) {
	sessionID := extractJSONText(payload, "session_id", "conversation_id")
	observeSessionStartTurnState(sessionID)
	source := extractJSONText(payload, "source")
	if source == "" {
		source = "session"
	}
	recordHookFlight("codex_session_start", sessionID, "SessionStart", source, len(payload), len(payload), nil, nil)
	context := slimferenceAwarenessContext(codexHookMode())
	if context == "" {
		return
	}
	out := map[string]interface{}{
		"continue": true,
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "SessionStart",
			"additionalContext": context,
		},
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode codexhook output: %v\n", err)
		exitFn(1)
	}
}

// slimferenceAwarenessContext returns the SessionStart additionalContext
// text to inject into the model's session for the given hook mode. The
// text tells the model that tool outputs may be pre-compacted by
// Slimference so it does not retry "to see the full output" — a common
// failure mode that wastes tokens. Returns "" when the mode should not
// inject anything (silent / auto without aggressive flag).
//
// Mode semantics (mirrors the lifecycle hook script case statement):
//
//	silent      -> no injection
//	auto        -> short hint that compaction may occur
//	compact     -> explicit summary of what to expect
//	aggressive  -> fullest description of the contract
//	debug       -> debug-mode banner (legacy text preserved for fixtures)
func slimferenceAwarenessContext(mode string) string {
	switch mode {
	case "debug":
		return "Local hook debug mode is active. Compact tool-result feedback may replace oversized raw output."
	case "compact":
		return "Slimference is compacting tool outputs. Results from `git`, build/test runners, linters, and `grep`/`rg` may arrive as short structured summaries with markers like `[git status] 3 staged, 1 untracked` or `FAILED\\n<errors>`. Trust the summary; do not re-run a tool to see fuller output — the original was already captured and is recoverable via `slimference debug tail`."
	case "aggressive":
		return "Slimference is aggressively compacting tool outputs and prompt history. Tool results may appear as one-line summaries (`[tool] ok`, `[git status] 3 staged, 1 untracked`) or pre-extracted error blocks (`FAILED\\n<errors>`). Earlier file reads return delta-only context. Do not re-run a tool just to confirm output: the original is archived and surfaced via `slimference debug tail`. Trust archive markers like `[archived #ID preview …]` as proof the full result existed."
	case "auto":
		return "Slimference may compact some tool outputs in this session. Trust short structured summaries (`[tool] ok`, `FAILED\\n<errors>`); the full raw output is archived locally if needed."
	default:
		return ""
	}
}

func codexHookMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_HOOK_MODE")))
	switch mode {
	case "debug", "compact", "aggressive", "auto", "silent":
		return mode
	default:
		return "auto"
	}
}

func handleCodexPermissionRequestHook(payload []byte) {
	sessionID := extractJSONText(payload, "session_id", "conversation_id")
	toolName := extractJSONText(payload, "tool_name", "toolName")
	if toolName == "" {
		toolName = "PermissionRequest"
	}
	cmdLine, err := filter.ExtractCommandFromHookJSON(payload)
	if err != nil {
		recordHookFlight("codex_permission_request", sessionID, toolName, "no_command", len(payload), len(payload), nil, nil)
		return
	}
	if code, msg := layer0PermissionCheck(cmdLine); code != 0 {
		recordHookFlight("codex_permission_request", sessionID, toolName, "deny", len(payload), len(payload), nil, nil)
		out := map[string]interface{}{
			"hookSpecificOutput": map[string]interface{}{
				"hookEventName": "PermissionRequest",
				"decision": map[string]interface{}{
					"behavior": "deny",
					"message":  msg,
				},
			},
		}
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "encode codexhook output: %v\n", err)
			exitFn(1)
		}
		return
	}
	recordHookFlight("codex_permission_request", sessionID, toolName, "allow", len(payload), len(payload), nil, nil)
	out := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName": "PermissionRequest",
			"decision": map[string]interface{}{
				"behavior": "allow",
			},
		},
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode codexhook output: %v\n", err)
		exitFn(1)
	}
}

func handleCodexUserPromptSubmitHook(payload []byte) {
	sessionID := extractJSONText(payload, "session_id", "conversation_id")
	observeUserPromptTurnState(sessionID)
	recordHookFlight("codex_user_prompt_submit", sessionID, "UserPromptSubmit", "observed", len(payload), len(payload), nil, nil)
}

func handleCodexStopHook(payload []byte) {
	sessionID := extractJSONText(payload, "session_id", "conversation_id")
	observeStopTurnState(sessionID)
	recordHookFlight("codex_stop", sessionID, "Stop", "continue", len(payload), len(payload), nil, nil)
	out := map[string]interface{}{"continue": true}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode codexhook output: %v\n", err)
		exitFn(1)
	}
}

// handleCodexPreCompactHook handles Codex 0.130+ PreCompact events.
// PreCompact fires before Codex's own compaction runs (auto when
// context approaches auto_compact_token_limit, or manual on explicit
// request). Slimference records the boundary so analytics can show
// the "compaction signal -> next request" gap, and writes a marker
// file the daemon can poll to escalate proxy-side compaction
// aggressiveness on the next request from this session.
//
// Output: continue:true (default; we never block Codex's compaction).
// All work happens off the hot path so the user does not see latency.
//
// Schema reference: pre-compact.command.{input,output} in codex 0.130.
func handleCodexPreCompactHook(payload []byte) {
	sessionID := extractJSONText(payload, "session_id", "conversation_id")
	turnID := extractJSONText(payload, "turn_id")
	trigger := extractJSONText(payload, "trigger")
	if trigger == "" {
		trigger = "unknown"
	}
	writeCompactionMarker("pre", sessionID, turnID, trigger)
	recordHookFlight("codex_pre_compact", sessionID, "PreCompact", "trigger:"+trigger, len(payload), len(payload), nil, nil)
	out := map[string]interface{}{"continue": true}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode codexhook output: %v\n", err)
		exitFn(1)
	}
}

// handleCodexPostCompactHook handles Codex 0.130+ PostCompact events.
// PostCompact fires after Codex's compaction completes. Slimference
// records the boundary so analytics can correlate the before/after
// token state and surface compaction frequency in `slimference gain`.
// Output: continue:true.
func handleCodexPostCompactHook(payload []byte) {
	sessionID := extractJSONText(payload, "session_id", "conversation_id")
	turnID := extractJSONText(payload, "turn_id")
	trigger := extractJSONText(payload, "trigger")
	if trigger == "" {
		trigger = "unknown"
	}
	writeCompactionMarker("post", sessionID, turnID, trigger)
	recordHookFlight("codex_post_compact", sessionID, "PostCompact", "trigger:"+trigger, len(payload), len(payload), nil, nil)
	out := map[string]interface{}{"continue": true}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode codexhook output: %v\n", err)
		exitFn(1)
	}
}

// writeCompactionMarker writes a small JSON marker under
// ~/.slimference/run/compact/<phase>/<session_id>.json so the proxy
// daemon can poll for compaction-imminent signals on the next request.
// Best-effort: I/O failure is swallowed because the hook contract must
// never break on a bookkeeping side-effect.
func writeCompactionMarker(phase, sessionID, turnID, trigger string) {
	home, err := osUserHomeDir()
	if err != nil {
		return
	}
	_ = compactsignal.DefaultStore(home).WriteMarker(phase, sessionID, turnID, trigger)
}

func extractJSONText(payload []byte, keys ...string) string {
	var v interface{}
	if err := json.Unmarshal(payload, &v); err != nil {
		return ""
	}
	for _, key := range keys {
		if s, ok := findJSONText(v, key); ok {
			return s
		}
	}
	return ""
}

func findJSONText(v interface{}, key string) (string, bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		if s, ok := t[key].(string); ok && s != "" {
			return s, true
		}
		for _, child := range t {
			if s, ok := findJSONText(child, key); ok {
				return s, true
			}
		}
	case []interface{}:
		for _, child := range t {
			if s, ok := findJSONText(child, key); ok {
				return s, true
			}
		}
	}
	return "", false
}

func handleReadHookCmd(args []string) {
	mode := ""
	for _, arg := range args {
		switch arg {
		case "", "--":
		case "claude":
			fmt.Fprintln(os.Stderr, "readhook: Claude Code is parked; Slimference is Codex-only")
			exitFn(2)
		case "codex":
			mode = "codex"
		default:
			fmt.Fprintln(os.Stderr, "usage: slimference readhook codex   (pipe Codex Read hook JSON on stdin)")
			exitFn(1)
		}
	}
	if mode == "" {
		fmt.Fprintln(os.Stderr, "usage: slimference readhook codex   (pipe Codex Read hook JSON on stdin)")
		exitFn(1)
	}
	if termIsTerminalFn(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "usage: slimference readhook codex   (pipe Codex Read hook JSON on stdin)")
		exitFn(1)
	}

	payload, err := readStdinAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		exitFn(1)
	}
	// Panic-safety: readhook is on the Read tool hot path, runs many
	// times per session. A bug here would brick file reads, which is a
	// worst-case UX outcome. t164 fail-open contract.
	defer guardHook(FailOpenReadHook, payload)()
	req, err := readcache.ExtractRequest(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}

	home, err := osUserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "home: %v\n", err)
		exitFn(1)
	}
	if req.TurnID == "" {
		req.TurnID = currentHookTurnID(sessions.DefaultHookStateDir(home), req.SessionID)
	}
	decision, err := readcache.Evaluate(readcache.DefaultDir(home), req)
	if err != nil {
		recordHookFlight("readhook", req.SessionID, mode, "error", 0, 0, nil, err)
		fmt.Fprintf(os.Stderr, "readhook: %v\n", err)
		exitFn(1)
	}
	_ = sessions.ObserveHookFile(sessions.DefaultHookStateDir(home), req.SessionID, req.FilePath, "read")
	recordHookFlight("readhook", req.SessionID, mode, string(decision.Type), 0, 0, nil, nil)
	if decision.Type != readcache.DecisionBlock {
		return
	}

	var out map[string]interface{}
	if mode == "codex" {
		out = map[string]interface{}{
			"decision": "block",
			"reason":   decision.Reason,
		}
	} else {
		out = map[string]interface{}{
			"hookSpecificOutput": map[string]interface{}{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "deny",
				"permissionDecisionReason": decision.Reason,
			},
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode readhook output: %v\n", err)
		exitFn(1)
	}
}

func hookDecision(changed bool) string {
	if changed {
		return "compacted"
	}
	return "passthrough"
}

func hookFileReadContext(workDir string, details filter.PostToolPayload) filter.FileReadContext {
	ctx := filter.FileReadContext{Mode: "scan"}
	readPath := filter.ReadPathFromCommandLine(details.CommandLine)
	if readPath == "" || details.SessionID == "" {
		return ctx
	}
	home, err := osUserHomeDir()
	if err != nil {
		return ctx
	}
	dir := sessions.DefaultHookStateDir(home)
	for _, path := range hookPathCandidates(firstNonEmpty(details.CWD, workDir), readPath) {
		recent, err := sessions.RecentlyEditedHookFile(dir, details.SessionID, path, 2)
		if err == nil && recent {
			ctx.RecentlyEdited = true
			return ctx
		}
	}
	return ctx
}

func observeSessionStartTurnState(sessionID string) {
	if home, err := osUserHomeDir(); err == nil {
		_ = sessions.StartHookSession(sessions.DefaultHookStateDir(home), sessionID)
	}
}

func observeUserPromptTurnState(sessionID string) {
	if home, err := osUserHomeDir(); err == nil {
		_ = sessions.StartHookTurn(sessions.DefaultHookStateDir(home), sessionID)
	}
}

func observeStopTurnState(sessionID string) {
	if home, err := osUserHomeDir(); err == nil {
		_ = sessions.CloseHookTurn(sessions.DefaultHookStateDir(home), sessionID)
	}
}

func observePostToolTurnState(workDir string, details filter.PostToolPayload) {
	if details.SessionID == "" {
		return
	}
	home, err := osUserHomeDir()
	if err != nil {
		return
	}
	dir := sessions.DefaultHookStateDir(home)
	_ = sessions.ObserveHookTool(dir, details.SessionID, details.ToolName, details.CommandLine)
	if readPath := filter.ReadPathFromCommandLine(details.CommandLine); readPath != "" {
		for _, path := range hookPathCandidates(firstNonEmpty(details.CWD, workDir), readPath) {
			_ = sessions.ObserveHookFile(dir, details.SessionID, path, "read")
		}
	}
	if !postToolLooksLikeEdit(details) {
		return
	}
	for _, path := range details.FilePaths {
		for _, candidate := range hookPathCandidates(firstNonEmpty(details.CWD, workDir), path) {
			_ = sessions.ObserveHookFile(dir, details.SessionID, candidate, "edit")
		}
	}
}

func currentHookTurnID(dir, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	turnID, err := sessions.CurrentHookTurnID(dir, sessionID)
	if err != nil {
		return ""
	}
	return turnID
}

func applyPostToolCrossToolDedup(workDir string, details filter.PostToolPayload) ([]byte, bool) {
	if details.SessionID == "" {
		return nil, false
	}
	argv := filter.ArgvForCapturedOutput(details.CommandLine)
	if len(argv) == 0 {
		return nil, false
	}
	paths := gitPathListForPostTool(argv, details.ToolResponse)
	if len(paths) == 0 {
		return nil, false
	}
	home, err := osUserHomeDir()
	if err != nil {
		return nil, false
	}
	cwd := firstNonEmpty(details.CWD, workDir)
	dir := sessions.DefaultHookStateDir(home)
	source := strings.Join(argv, " ")
	previous, repeated, err := sessions.ObserveHookGitPathList(dir, details.SessionID, cwd, source, paths)
	if err != nil || !repeated || !crosstool.IsGitDiffNameOnlyArgv(argv) {
		return nil, false
	}
	return []byte(crosstool.Marker(previous.Count, previous.Source)), true
}

func gitPathListForPostTool(argv []string, output string) []string {
	if crosstool.IsGitStatusArgv(argv) {
		return crosstool.ExtractGitStatusPaths([]byte(output))
	}
	if crosstool.IsGitDiffNameOnlyArgv(argv) {
		return crosstool.ExtractGitNameOnlyPaths([]byte(output))
	}
	return nil
}

func postToolLooksLikeEdit(details filter.PostToolPayload) bool {
	s := strings.ToLower(details.ToolName + " " + details.CommandLine)
	return strings.Contains(s, "apply_patch") ||
		strings.Contains(s, "edit") ||
		strings.Contains(s, "write") ||
		strings.Contains(s, "*** update file:") ||
		strings.Contains(s, "*** add file:") ||
		strings.Contains(s, "*** delete file:")
}

func hookPathCandidates(workDir, path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	candidates := []string{filepath.Clean(path)}
	if workDir != "" && !filepath.IsAbs(path) {
		candidates = append(candidates, filepath.Clean(filepath.Join(workDir, path)))
	}
	return candidates
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func recordHookFlight(source, sessionID, toolName, decision string, originalBytes, finalBytes int, layers []int, hookErr error) {
	recordHookFlightEntries(source, sessionID, toolName, decision, originalBytes, finalBytes, layers, nil, hookErr)
}

func recordHookFlightEntries(source, sessionID, toolName, decision string, originalBytes, finalBytes int, layers []int, entries []dbg.DecisionEntry, hookErr error) {
	path := configuredDecisionsLogPath()
	if path == "" {
		return
	}
	originalTokens := filter.EstimateTokensFromBytes(originalBytes)
	finalTokens := filter.EstimateTokensFromBytes(finalBytes)
	if finalBytes == 0 && originalBytes > 0 {
		finalTokens = originalTokens
	}
	saved := originalTokens - finalTokens
	if saved < 0 {
		saved = 0
	}
	ratio := 1.0
	if originalTokens > 0 {
		ratio = float64(finalTokens) / float64(originalTokens)
	}
	var errorsOut []string
	if hookErr != nil {
		errorsOut = []string{hookErr.Error()}
	}
	rec := dbg.NewRecorder(1, path)
	requestID := fmt.Sprintf("%s-%d", source, time.Now().UnixNano())
	now := time.Now().UTC()
	for i := range entries {
		if entries[i].Timestamp.IsZero() {
			entries[i].Timestamp = now
		}
		if entries[i].RequestID == "" {
			entries[i].RequestID = requestID
		}
	}
	rec.Record(dbg.RequestSummary{
		RequestID:     requestID,
		Timestamp:     now,
		SessionID:     sessionID,
		Source:        source,
		Provider:      "local",
		ClientFamily:  toolName,
		RouteMode:     "hook",
		BypassReason:  decision,
		Model:         toolName,
		LayersApplied: append([]int(nil), layers...),
		Tokens: dbg.TokenCounts{
			Original: originalTokens,
			Final:    finalTokens,
			Saved:    saved,
			Ratio:    ratio,
		},
		Errors:  errorsOut,
		Entries: entries,
	})
}

func handleHookCmd(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: slimference hook <install|remove|verify|status|check-upstream> ...")
		exitFn(1)
	}
	home, err := osUserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "home: %v\n", err)
		exitFn(1)
	}
	tpCmd := ""
	if cfg, err := config.Load(); err == nil {
		tpCmd = strings.TrimSpace(cfg.Hooks.SlimferenceCommand)
	}
	switch args[0] {
	case "install":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: slimference hook install codex")
			exitFn(1)
		}
		switch args[1] {
		case "claude":
			fmt.Fprintln(os.Stderr, "Claude Code hooks are parked; Slimference installs Codex hooks only.")
			exitFn(2)
		case "codex":
			if err := installCodexHookFn(home, tpCmd); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				exitFn(1)
			}
			fmt.Println("Installed Codex hooks only (~/.codex/hooks.json + ~/.slimference/hooks).")
			fmt.Println("Enabled hooks feature flag only; Codex base URLs were not modified.")
			fmt.Println("Use `slimference integrate install --client codex` only for legacy config-patch mode.")
		default:
			fmt.Fprintf(os.Stderr, "unknown install target: %s (want codex; claude is parked)\n", args[1])
			exitFn(1)
		}
	case "remove":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: slimference hook remove codex")
			exitFn(1)
		}
		switch args[1] {
		case "claude":
			fmt.Fprintln(os.Stderr, "Claude Code hooks are parked; Slimference will not modify ~/.claude.")
			exitFn(2)
		case "codex":
			if err := removeCodexHookFn(home); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				exitFn(1)
			}
			fmt.Println("Removed Slimference hooks from Codex (hooks.json + ~/.slimference/hooks).")
			fmt.Println("Codex config was not modified. Use `slimference integrate remove --client codex` to remove config-patch mode.")
		default:
			fmt.Fprintf(os.Stderr, "unknown remove target: %s\n", args[1])
			exitFn(1)
		}
	case "verify":
		lines, ok := hooks.VerifyReport(home)
		if len(args) >= 2 && args[1] == "codex" {
			lines, ok = hooks.VerifyCodexReport(home)
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		if !ok {
			exitFn(1)
		}
	case "status":
		lines, _ := hooks.VerifyReport(home)
		if len(args) >= 2 && args[1] == "codex" {
			lines, _ = hooks.VerifyCodexReport(home)
		}
		for _, l := range lines {
			fmt.Println(l)
		}
	case "check-upstream":
		reports := hookDetectDriftFn(context.Background())
		fmt.Print(hooks.FormatDriftReports(reports))
		for _, r := range reports {
			if r.BinaryFound && r.Status != hooks.DriftOK {
				exitFn(1)
			}
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown hook subcommand: %s\n", args[0])
		exitFn(1)
	}
}

func handleConfigCmd(args []string) {
	switch args[0] {
	case "init":
		// Respect --config / SLIMFERENCE_CONFIG / XDG precedence for init too
		// so users initialising a profile at a custom path actually hit that
		// path. When no existing file is found we write to XDG by default.
		path := explicitConfigPath
		if path == "" {
			path = os.Getenv("SLIMFERENCE_CONFIG")
		}
		if path == "" {
			path = config.XDGConfigPath()
		}
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("Config already exists at %s\n", path)
			fmt.Println("Delete it first if you want to regenerate defaults.")
			exitFn(0)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "create dir: %v\n", err)
			exitFn(1)
		}
		if err := osWriteFile(path, []byte(config.DefaultTOML()), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "write config: %v\n", err)
			exitFn(1)
		}
		fmt.Printf("Config written to %s\n", path)
		fmt.Println("Next: run 'slimference doctor'")

	case "show":
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "load config: %v\n", err)
			exitFn(1)
		}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(data))

	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand: %s\n", args[0])
		exitFn(1)
	}
}

func handleTestCmd(args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
	}

	switch args[0] {
	case "anthropic":
		testUpstream("Anthropic", cfg.Upstream.Anthropic.BaseURL)
	case "openai":
		testUpstream("OpenAI", cfg.Upstream.OpenAI.BaseURL)
	case "intercept":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: slimference test intercept codex")
			exitFn(1)
		}
		testIntercept(cfg, args[1])
	default:
		fmt.Fprintf(os.Stderr, "unknown test subcommand: %s\n", args[0])
		exitFn(1)
	}
}

func testUpstream(name, baseURL string) {
	fmt.Printf("Testing %s connectivity to %s...\n", name, baseURL)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL)
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		exitFn(1)
	}
	defer resp.Body.Close()
	fmt.Printf("OK - HTTP %d\n", resp.StatusCode)
}

func testIntercept(cfg *config.Config, provider string) {
	fmt.Printf("Starting intercept test for %s...\n", provider)
	fmt.Printf("Listening on %s\n\n", cfg.ListenURL())

	switch provider {
	case "claude":
		fmt.Fprintln(os.Stderr, "test intercept: Claude Code is parked; use RTK for Claude Code")
		exitFn(2)
	case "codex":
		fmt.Println("Add to ~/.codex/config.toml:")
		fmt.Printf("  openai_base_url = \"%s\"\n", cfg.ListenURL())
		fmt.Println("Then run: codex 'say hi' in another terminal")
		fmt.Println()
	}

	mux := http.NewServeMux()
	received := make(chan struct{}, 1)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("\nRequest received from %s:\n", provider)
		fmt.Printf("  Method: %s %s\n", r.Method, r.URL.Path)
		fmt.Printf("  User-Agent: %s\n", r.Header.Get("User-Agent"))
		fmt.Printf("  Content-Type: %s\n", r.Header.Get("Content-Type"))
		if r.Header.Get("Authorization") != "" {
			fmt.Println("  Auth: Bearer *** (present)")
		}
		if r.Header.Get("x-api-key") != "" {
			fmt.Println("  x-api-key: *** (present)")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"type":"message","id":"test","content":[{"type":"text","text":"Slimference intercept test OK"}],"model":"test","role":"assistant","stop_reason":"end_turn"}`))
		fmt.Println("\nPASS - proxy intercept works!")
		notifyInterceptReceived(received)
	})

	srv := &http.Server{Addr: cfg.ListenAddr(), Handler: mux}
	serveErrCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErrCh <- err
		}
	}()

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), testInterceptShutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}

	select {
	case err := <-serveErrCh:
		fmt.Printf("FAIL - intercept server failed to start: %v\n", err)
		exitFn(1)
	case <-received:
		time.Sleep(100 * time.Millisecond)
		shutdown()
	case <-time.After(testInterceptTimeout):
		fmt.Println("FAIL - no request received within 60 seconds")
		fmt.Println("Troubleshooting:")
		fmt.Printf("  1. Is Codex configured to use %s?\n", cfg.ListenURL())
		fmt.Printf("  2. Is the CLI configured to use %s?\n", cfg.ListenURL())
		fmt.Println("  3. Try: curl " + cfg.ListenURL() + "/health")
		shutdown()
		exitFn(1)
	}
}

func notifyInterceptReceived(received chan struct{}) {
	select {
	case received <- struct{}{}:
	default:
	}
}

func handleDoctorCmd() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL config: %v\n", err)
		exitFn(1)
	}

	allOK := true
	check := func(name string, fn func() (string, bool)) {
		msg, ok := fn()
		symbol := "OK  "
		if !ok {
			symbol = "FAIL"
			allOK = false
		}
		fmt.Printf("[%s] %s: %s\n", symbol, name, msg)
	}
	warn := func(name string, fn func() string) {
		fmt.Printf("[WARN] %s: %s\n", name, fn())
	}

	fmt.Println("Slimference Doctor")
	fmt.Println(strings.Repeat("-", 50))

	check("Config file", func() (string, bool) {
		info := config.ResolveConfigPath(config.LoadOptions{ExplicitPath: explicitConfigPath})
		if info.ResolvedPath == "" {
			return fmt.Sprintf("no file found, using defaults (checked: %s)",
				strings.Join(info.Checked, ", ")), true
		}
		return fmt.Sprintf("%s (source=%s)", info.ResolvedPath, info.Source), true
	})

	check("Listen port", func() (string, bool) {
		return fmt.Sprintf("%s (port %d)", cfg.ListenAddr(), cfg.Proxy.ListenPort), true
	})

	check("Anthropic upstream", func() (string, bool) {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(cfg.Upstream.Anthropic.BaseURL)
		if err != nil {
			return fmt.Sprintf("unreachable: %v", err), false
		}
		defer resp.Body.Close()
		return fmt.Sprintf("reachable (HTTP %d)", resp.StatusCode), true
	})

	check("OpenAI upstream", func() (string, bool) {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(cfg.Upstream.OpenAI.BaseURL)
		if err != nil {
			return fmt.Sprintf("unreachable: %v", err), false
		}
		defer resp.Body.Close()
		return fmt.Sprintf("reachable (HTTP %d)", resp.StatusCode), true
	})

	check("Analytics log dir", func() (string, bool) {
		logDir := cfg.Analytics.ResolvedLogDir()
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Sprintf("cannot create %s: %v", logDir, err), false
		}
		return logDir, true
	})

	// T33: surface hook CLI drift as part of doctor output.
	reports := hookDetectDriftFn(context.Background())
	for _, r := range reports {
		ok := r.Status == hooks.DriftOK || !r.BinaryFound
		label := fmt.Sprintf("%s CLI drift", r.CLI)
		msg := fmt.Sprintf("status=%s", r.Status)
		if r.BinaryFound {
			msg += fmt.Sprintf(" version=%s supported=[%s, %s]", r.VersionParsed, r.MinSupported, r.MaxTested)
		}
		if r.Notes != "" {
			msg += " - " + r.Notes
		}
		check(label, func() (string, bool) { return msg, ok })
	}

	for _, prov := range []types.Provider{types.Anthropic, types.OpenAI, types.CodexChatGPT} {
		prov := prov
		check("Provider caps: "+prov.String(), func() (string, bool) {
			caps := types.CapabilitiesFor(prov)
			return fmt.Sprintf("seed=%v min_tokens=%v response_id=%v cached_prefix=%v",
				caps.SupportsSeed, caps.SupportsMinCompletionTokens,
				caps.SupportsResponseID, caps.SupportsCachedPrefix), true
		})
	}

	warn("TLS profile catalog", func() string {
		return formatTLSCatalogStatus(time.Now())
	})

	warn("TLS reflected proof", func() string {
		return formatTLSProofStatus(time.Now())
	})

	check("Content archive", func() (string, bool) {
		home, err := osUserHomeDir()
		if err != nil {
			return "home dir unavailable", false
		}
		dir := contentarchive.DefaultDir(home)
		stats, err := contentarchive.LoadStats(dir)
		if err != nil {
			return fmt.Sprintf("unreadable: %v", err), false
		}
		return fmt.Sprintf("%s - archived %d, expanded %d, re-injects %d",
			dir, stats.Archived, stats.Expanded, stats.ReInjectCount), true
	})

	// T69: integration fallback checks. These surface the common
	// "something is wired but the daemon is unreachable" states operators
	// need to triage before running real traffic.
	fmt.Println()
	fmt.Println("Integration / Fallbacks:")
	renderIntegrationChecks(check)

	fmt.Println(strings.Repeat("-", 50))
	if allOK {
		fmt.Println("All checks passed. Run 'slimference' to start.")
	} else {
		fmt.Println("Some checks failed. See above for details.")
	}
}

func formatTLSCatalogStatus(now time.Time) string {
	info := tlsdial.Catalog()
	ageDays := int(tlsdial.CatalogAge(now).Hours() / 24)
	state := "fresh"
	if tlsdial.CatalogStale(now) {
		state = "stale - review pinned uTLS/browser profiles"
	}
	return fmt.Sprintf("%s generated=%s age_days=%d max_age_days=%d state=%s",
		info.Version, info.Generated.Format("2006-01-02"), ageDays, info.MaxAgeDays, state)
}

func formatTLSProofStatus(now time.Time) string {
	home, err := osUserHomeDir()
	if err != nil {
		return "proof status unavailable: HOME lookup failed"
	}
	dir := tlsproof.DefaultDir(home)
	statuses, err := tlsproof.LatestByProfile(dir, now)
	if err != nil {
		return fmt.Sprintf("proof status unreadable: %v", err)
	}
	if len(statuses) == 0 {
		return fmt.Sprintf("no reflected provider-edge proof yet (run: go run ./scripts/utils tls-probe --profile=chromium_stable --reflector=https://... --save; dir=%s)", dir)
	}
	names := tlsproof.ProfilesWithProof(statuses)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		status := statuses[name]
		state := "failed"
		if status.Success {
			state = "ok"
		}
		hash := status.JA3Hash
		if hash == "" {
			hash = "no-ja3"
		}
		parts = append(parts, fmt.Sprintf("%s=%s age_days=%d ja3=%s", name, state, status.AgeDays, hash))
	}
	return strings.Join(parts, "; ")
}

// renderIntegrationChecks emits the T69 integration status block inside
// `doctor`. These are informational - every state is reported as OK with
// the actual wiring status in the message, because "not wired" is a valid
// operator choice, not a doctor failure. The only hard failure is "HOME
// unresolvable" which indicates a fundamentally broken environment.
func renderIntegrationChecks(check func(string, func() (string, bool))) {
	home, err := osUserHomeDir()
	if err != nil {
		check("integrate", func() (string, bool) {
			return fmt.Sprintf("cannot resolve HOME: %v", err), false
		})
		return
	}
	rep := integrateStatusFn(integrateOptions{HomeDir: home})
	check("Claude Code integration", func() (string, bool) {
		return fmt.Sprintf("%s (%s)", rep.Claude.State, rep.Claude.BinaryPath), true
	})
	check("Codex integration", func() (string, bool) {
		return fmt.Sprintf("%s (%s)", rep.Codex.State, rep.Codex.BinaryPath), true
	})
	check("Daemon reachable", func() (string, bool) {
		if rep.Daemon.Running {
			return fmt.Sprintf("running (pid %d)", rep.Daemon.PID), true
		}
		return "not running - start via `slimference service install` or `slimference --no-tui`", true
	})
	check("launchd plist", func() (string, bool) {
		path := daemonPlistPathFn()
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
		return "not installed (ok if you prefer manual start)", true
	})
}

// Injectable shims so doctor tests can bypass the real detect probes.
var (
	integrateStatusFn     = defaultIntegrateStatus
	daemonPlistPathFn     = daemon.LaunchdPlistPath
	integrateNotInstalled = "not_installed"
	integrateFullyWired   = "fully_wired"
)

type integrateOptions struct {
	HomeDir string
}

type integrateReportClient struct {
	State      string
	BinaryPath string
}

type integrateReportDaemon struct {
	Running bool
	PID     int
}

type integrateReport struct {
	Claude integrateReportClient
	Codex  integrateReportClient
	Daemon integrateReportDaemon
}

func defaultIntegrateStatus(opts integrateOptions) integrateReport {
	r := integrate.Status(integrate.Options{HomeDir: opts.HomeDir})
	return integrateReport{
		Claude: integrateReportClient{State: r.Claude.State.String(), BinaryPath: r.Claude.BinaryPath},
		Codex:  integrateReportClient{State: r.Codex.State.String(), BinaryPath: r.Codex.BinaryPath},
		Daemon: integrateReportDaemon{Running: r.Daemon.Running, PID: r.Daemon.PID},
	}
}

// hookDetectDriftFn is overridable in tests so doctor can be exercised
// without probing real CLIs.
var hookDetectDriftFn = hooks.DetectDrift

func handleStatsCmd(args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
	}

	logDir := cfg.Analytics.ResolvedLogDir()

	if args[0] == "prompt-cache" {
		handlePromptCacheStatsCmd(logDir, args[1:])
		return
	}

	switch args[0] {
	case "today":
		snapshots, err := analytics.ReadDailyStats(logDir, time.Now())
		if err != nil || len(snapshots) == 0 {
			fmt.Println("No stats for today yet.")
			return
		}
		printStatsTable(snapshots)
	case "week":
		snapshots, err := analytics.ReadWeeklyStats(logDir)
		if err != nil || len(snapshots) == 0 {
			fmt.Println("No stats for this week.")
			return
		}
		printStatsTable(snapshots)
	case "month":
		var allSnapshots []analytics.AnalyticsSnapshot
		for i := 0; i < 30; i++ {
			day := time.Now().AddDate(0, 0, -i)
			snaps, err := analytics.ReadDailyStats(logDir, day)
			if err == nil {
				allSnapshots = append(allSnapshots, snaps...)
			}
		}
		if len(allSnapshots) == 0 {
			fmt.Println("No stats for this month.")
			return
		}
		printStatsTable(allSnapshots)
	default:
		fmt.Fprintf(os.Stderr, "unknown stats subcommand: %s\n", args[0])
		exitFn(1)
	}
}

type gainCLIFlags struct {
	json      bool
	byCommand bool
	byParser  bool
	cache     bool
	output    bool
	proxy     bool
	csv       bool
	project   string
}

func parseGainArgs(args []string) (period string, f gainCLIFlags, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--json", "-json":
			f.json = true
		case "--by-command":
			f.byCommand = true
		case "--by-parser":
			f.byParser = true
		case "--cache":
			f.cache = true
		case "--output":
			f.output = true
		case "--proxy":
			f.proxy = true
		case "--csv":
			f.csv = true
		case "--project":
			i++
			if i >= len(args) || args[i] == "" {
				return "", f, fmt.Errorf("--project requires a path")
			}
			f.project = args[i]
		default:
			if a == "" {
				continue
			}
			if strings.HasPrefix(a, "-") {
				return "", f, fmt.Errorf("unknown flag: %s", a)
			}
			if period == "" {
				period = a
			} else {
				return "", f, fmt.Errorf("unexpected extra argument: %s", a)
			}
		}
	}
	if period == "" {
		period = "today"
	}
	if f.cache && (f.byCommand || f.byParser || f.output || f.project != "") {
		return "", f, fmt.Errorf("--cache cannot be combined with --by-command, --by-parser, --output, or --project")
	}
	if f.output && (f.byCommand || f.byParser || f.proxy || f.project != "") {
		return "", f, fmt.Errorf("--output cannot be combined with --by-command, --by-parser, --proxy, or --project")
	}
	if f.proxy && (f.byCommand || f.byParser || f.cache || f.project != "") {
		return "", f, fmt.Errorf("--proxy cannot be combined with --by-command, --by-parser, --cache, or --project")
	}
	if f.byCommand && f.byParser {
		return "", f, fmt.Errorf("--by-command and --by-parser are mutually exclusive")
	}
	return period, f, nil
}

// handleGainCmd prints aggregates from Layer-0 filter SQLite (filter_runs).
func handleGainCmd(args []string) {
	period, flags, err := parseGainArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}
	switch period {
	case "today", "week", "month", "all":
	default:
		fmt.Fprintln(os.Stderr, "usage: slimference gain [today|week|month|all] [--json] [--by-command|--by-parser|--cache|--output|--proxy] [--csv] [--project <path>]  (USD: [analytics] gain_usd_per_million_tokens or SLIMFERENCE_GAIN_USD_PER_MILLION)")
		exitFn(1)
	}
	if flags.cache {
		handleGainCache(period, flags)
		return
	}
	if flags.output {
		handleGainOutput(period, flags)
		return
	}
	if flags.proxy {
		handleGainProxy(period, flags)
		return
	}
	path, err := resolveFilterDBPathFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "filter db path: %v\n", err)
		exitFn(1)
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No Layer-0 filter runs recorded yet (no filter.db).")
			return
		}
		fmt.Fprintf(os.Stderr, "filter db: %v\n", err)
		exitFn(1)
	}
	usdRate := 0.0
	if cfg, err := config.Load(); err == nil {
		usdRate = cfg.Analytics.GainUSDPerMillionTokens
	}
	rep, err := analytics.QueryFilterGainReportWithOptions(path, period, time.Now(), flags.byCommand, flags.byParser, flags.project, usdRate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gain: %v\n", err)
		exitFn(1)
	}
	s := rep.FilterGainSummary
	if flags.csv {
		if flags.byCommand {
			if err := writeGainByCommandCSV(os.Stdout, rep.ByCommand); err != nil {
				fmt.Fprintf(os.Stderr, "gain: %v\n", err)
				exitFn(1)
			}
			return
		}
		if flags.byParser {
			if err := writeGainByParserCSV(os.Stdout, rep.ByParser); err != nil {
				fmt.Fprintf(os.Stderr, "gain: %v\n", err)
				exitFn(1)
			}
			return
		}
		if err := writeGainSummaryCSV(os.Stdout, s); err != nil {
			fmt.Fprintf(os.Stderr, "gain: %v\n", err)
			exitFn(1)
		}
		return
	}
	if flags.json {
		b, _ := analytics.FormatFilterGainReportJSON(rep)
		fmt.Println(string(b))
		return
	}
	if s.Runs == 0 {
		fmt.Println("No Layer-0 filter runs in this window.")
		return
	}
	title := "Layer 0 filter gain (" + period + ")"
	if s.ProjectPathFilter != "" {
		title += " — project " + s.ProjectPathFilter
	}
	fmt.Println(title)
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Filter runs:            %d\n", s.Runs)
	fmt.Printf("Input tokens (est):     %s\n", formatTokensPlain64(s.InputTokens))
	fmt.Printf("Output tokens (est):    %s\n", formatTokensPlain64(s.OutputTokens))
	fmt.Printf("Estimated tokens saved: %s\n", formatTokensPlain64(s.TokensSavedEst))
	if s.USDPerMillionTokens > 0 {
		fmt.Printf("Est. value saved (at $%.2f/M est. tokens): ~$%.4f\n", s.USDPerMillionTokens, s.SavingsUsdEst)
	}
	fmt.Println(strings.Repeat("-", 50))
	if flags.byCommand && len(rep.ByCommand) > 0 {
		fmt.Println("By command (sorted by est. saved):")
		for _, row := range rep.ByCommand {
			fmt.Printf("  %s\n", row.Command)
			extra := ""
			if row.SavingsUsdEst > 0 {
				extra = fmt.Sprintf("  (~$%.4f)", row.SavingsUsdEst)
			}
			fmt.Printf("    runs %d  in %s  out %s  saved ~%s%s\n",
				row.Runs,
				formatTokensPlain64(row.InputTokens),
				formatTokensPlain64(row.OutputTokens),
				formatTokensPlain64(row.TokensSavedEst),
				extra)
		}
	}
	if flags.byParser && len(rep.ByParser) > 0 {
		fmt.Println("By parser/tool family (sorted by est. saved):")
		for _, row := range rep.ByParser {
			extra := ""
			if row.SavingsUsdEst > 0 {
				extra = fmt.Sprintf("  (~$%.4f)", row.SavingsUsdEst)
			}
			fmt.Printf("  %-14s runs %d  in %s  out %s  saved ~%s%s\n",
				row.Parser,
				row.Runs,
				formatTokensPlain64(row.InputTokens),
				formatTokensPlain64(row.OutputTokens),
				formatTokensPlain64(row.TokensSavedEst),
				extra)
		}
	}
}

func handleGainProxy(period string, flags gainCLIFlags) {
	path := configuredDecisionsLogPath()
	if path == "" {
		fmt.Println("No decisions_log configured. Set [debug].decisions_log or SLIMFERENCE_DEBUG_DECISIONS_LOG.")
		return
	}
	summaries, err := replaySessionFn(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gain --proxy: %v\n", err)
		exitFn(1)
		return
	}
	report, err := analytics.SummarizeProxyFlights(summaries, period, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gain --proxy: %v\n", err)
		exitFn(1)
		return
	}
	if flags.csv {
		if err := writeProxyFlightGainCSV(os.Stdout, report); err != nil {
			fmt.Fprintf(os.Stderr, "gain --proxy: %v\n", err)
			exitFn(1)
		}
		return
	}
	if flags.json {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
		return
	}
	if report.Requests == 0 {
		fmt.Println("No proxy flight records in this window.")
		return
	}
	fmt.Printf("Proxy flight gain (%s)\n", period)
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Requests:                       %d\n", report.Requests)
	fmt.Printf("Provider-reported requests:      %d\n", report.ProviderReportedRequests)
	fmt.Printf("Provider input tokens:           %s\n", formatTokensPlain(report.ProviderInputTokens))
	fmt.Printf("Provider cached tokens:          %s\n", formatTokensPlain(report.ProviderCachedTokens))
	fmt.Printf("Provider output tokens:          %s\n", formatTokensPlain(report.ProviderOutputTokens))
	fmt.Printf("Billable input savings estimate: %s\n", formatTokensPlain(report.BillableInputSavingsEstimate))
	if report.ToolPruneSavedTokens > 0 || report.ToolPruneMisses > 0 || report.ToolPruneRetries > 0 {
		fmt.Printf("Tool-prune saved tokens:        %s\n", formatTokensPlain(report.ToolPruneSavedTokens))
		fmt.Printf("Tool-prune pruned tools:         %d\n", report.ToolPrunePrunedTools)
		fmt.Printf("Tool-prune reattached/miss/retry: %d/%d/%d\n", report.ToolPruneReattached, report.ToolPruneMisses, report.ToolPruneRetries)
	}
	fmt.Printf("Cache-read discount equivalent:  %s\n", formatTokensPlain(report.CacheReadDiscountTokenEquivalent))
	fmt.Printf("Net billable-equivalent estimate: %s\n", formatTokensPlain(report.NetBillableEquivalentEstimate))
	if report.OutputReduceInputOverheadTokens > 0 {
		fmt.Printf("Output-reduce input overhead:    %s\n", formatTokensPlain(report.OutputReduceInputOverheadTokens))
	}
	if len(report.PromptCacheHeat) > 0 {
		fmt.Println("Prompt-cache heat:")
		for i, row := range report.PromptCacheHeat {
			if i >= 5 {
				break
			}
			fmt.Printf("  %s requests=%d applied/skipped=%d/%d cached=%s read/create=%s/%s stable_max=%s\n",
				row.StablePrefixHash,
				row.Requests,
				row.HintsApplied,
				row.HintsSkipped,
				formatTokensPlain(row.ProviderCachedTokens),
				formatTokensPlain(row.CacheReadTokens),
				formatTokensPlain(row.CacheCreateTokens),
				formatTokensPlain(row.StablePrefixTokensMax),
			)
		}
	}
	fmt.Println("Provider cache credits are observed billing-equivalent credits, not claimed Slimference-caused token deletion.")
}

func handleGainOutput(period string, flags gainCLIFlags) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	report, err := analytics.ReadOutputReduceReport(cfg.Analytics.ResolvedLogDir(), period, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gain --output: %v\n", err)
		exitFn(1)
		return
	}
	if flags.csv {
		if err := writeOutputReduceCSV(os.Stdout, report); err != nil {
			fmt.Fprintf(os.Stderr, "gain --output: %v\n", err)
			exitFn(1)
		}
		return
	}
	if flags.json {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
		return
	}
	if report.TotalRequests == 0 {
		fmt.Println("No output-reduce telemetry in this window.")
		return
	}
	fmt.Printf("Output-reduce telemetry (%s)\n", period)
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Requests:                    %d\n", report.TotalRequests)
	fmt.Printf("Applied requests:            %d\n", report.AppliedRequests)
	fmt.Printf("Skipped requests:            %d\n", report.SkippedRequests)
	fmt.Printf("Directive input overhead:    %s\n", formatTokensPlain(report.InputOverheadTokens))
	fmt.Printf("Observed output tokens:      %s\n", formatTokensPlain(report.OutputTokensObserved))
	fmt.Printf("Applied-turn output tokens:  %s\n", formatTokensPlain(report.AppliedOutputTokens))
	fmt.Printf("Avg output tokens/request:   %.2f\n", report.AvgOutputTokens)
	fmt.Printf("Avg directive overhead/apply: %.2f\n", report.AvgInputOverheadPerApply)
	if len(report.Profiles) > 0 {
		fmt.Println("Profiles:")
		for _, key := range sortedStringIntKeys(report.Profiles) {
			fmt.Printf("  %s: %d\n", key, report.Profiles[key])
		}
	}
	if len(report.TaskShapes) > 0 {
		fmt.Println("Task shapes:")
		for _, key := range sortedStringIntKeys(report.TaskShapes) {
			fmt.Printf("  %s: %d\n", key, report.TaskShapes[key])
		}
	}
	if len(report.Reasons) > 0 {
		fmt.Println("Reasons:")
		for _, key := range sortedStringIntKeys(report.Reasons) {
			fmt.Printf("  %s: %d\n", key, report.Reasons[key])
		}
	}
	if len(report.ProfileRows) > 0 {
		fmt.Println("Profile rows:")
		for _, row := range report.ProfileRows {
			fmt.Printf(
				"  %s/%s %s/%s: requests=%d applied=%d skipped=%d output=%s overhead=%s avg_overhead/apply=%.2f\n",
				row.Provider,
				row.Model,
				row.Profile,
				row.TaskShape,
				row.Requests,
				row.AppliedRequests,
				row.SkippedRequests,
				formatTokensPlain(row.OutputTokensObserved),
				formatTokensPlain(row.InputOverheadTokens),
				row.AvgInputOverheadPerApply,
			)
		}
	}
	fmt.Println("Savings need a live baseline; this report intentionally does not invent one.")
}

func sortedStringIntKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func handleGainCache(period string, flags gainCLIFlags) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	report, err := analytics.ReadPromptCacheReport(cfg.Analytics.ResolvedLogDir(), period, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gain --cache: %v\n", err)
		exitFn(1)
		return
	}
	if flags.csv {
		if err := writePromptCacheCSV(os.Stdout, report); err != nil {
			fmt.Fprintf(os.Stderr, "gain --cache: %v\n", err)
			exitFn(1)
		}
		return
	}
	if flags.json {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
		return
	}
	if report.TotalRequests == 0 {
		fmt.Println("No prompt-cache gain in this window.")
		return
	}
	fmt.Printf("Prompt-cache gain (%s)\n", period)
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Requests:                  %d\n", report.TotalRequests)
	fmt.Printf("Cache-read requests:       %d (%.2f%%)\n", report.CacheReadRequests, report.HitRate*100)
	fmt.Printf("Cache read tokens:         %s\n", formatTokensPlain(report.CacheReadTokens))
	fmt.Printf("Cache create tokens:       %s\n", formatTokensPlain(report.CacheCreateTokens))
	fmt.Printf("Estimated read-token save: %s\n", formatTokensPlain(report.EstimatedSavedRead))
}

func handleDebugCmd(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: slimference debug <paths|last|summary|tail|replay|flight|bundle>")
		fmt.Fprintln(os.Stderr, "  paths — show resolved config file, analytics log, filter.db, tee dir")
		fmt.Fprintln(os.Stderr, "  last    — print last filter_runs row (optional --json)")
		fmt.Fprintln(os.Stderr, "  summary — aggregate for today|week|month|all (default today, --json)")
		fmt.Fprintln(os.Stderr, "  tail    — newest N rows (default 20, max 500, --json)")
		fmt.Fprintln(os.Stderr, "  replay  — replay session JSONL (RequestSummary per-request breakdown)")
		fmt.Fprintln(os.Stderr, "  flight  — normalized request flight recorder view")
		fmt.Fprintln(os.Stderr, "  bundle  — bounded content-free diagnostics bundle for later analysis")
		exitFn(1)
	}
	switch args[0] {
	case "paths":
		handleDebugPaths()
	case "last":
		handleDebugLast(args[1:])
	case "summary":
		handleDebugSummary(args[1:])
	case "tail":
		handleDebugTail(args[1:])
	case "replay":
		handleDebugReplay(args[1:])
	case "flight":
		handleDebugFlight(args[1:])
	case "bundle":
		handleDebugBundle(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown debug subcommand: %s\n", args[0])
		exitFn(1)
	}
}

func handleDebugFlight(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference debug flight <last|tail|replay|export> ...")
		exitFn(1)
	}
	switch args[0] {
	case "last":
		handleDebugFlightLast(args[1:])
	case "tail":
		handleDebugFlightTail(args[1:])
	case "replay":
		handleDebugFlightReplay(args[1:])
	case "export":
		handleDebugFlightExport(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown debug flight subcommand: %s\n", args[0])
		exitFn(1)
	}
}

func parseDebugFlightArgs(args []string, defaultLimit int) (limit int, jsonOut bool, err error) {
	limit = defaultLimit
	var gotLimit bool
	for _, a := range args {
		switch a {
		case "--json", "-json":
			jsonOut = true
		default:
			if a == "" {
				continue
			}
			if strings.HasPrefix(a, "-") {
				return 0, false, fmt.Errorf("unknown flag: %s", a)
			}
			if gotLimit {
				return 0, false, fmt.Errorf("unexpected extra argument: %s", a)
			}
			n, convErr := strconv.Atoi(a)
			if convErr != nil || n < 1 {
				return 0, false, fmt.Errorf("limit must be a positive integer")
			}
			limit = n
			gotLimit = true
		}
	}
	if limit > 500 {
		limit = 500
	}
	return limit, jsonOut, nil
}

func configuredDecisionsLogPath() string {
	cfg, _ := config.Load()
	path := strings.TrimSpace(cfg.Debug.DecisionsLog)
	if path == "" {
		return ""
	}
	return filepath.Clean(config.ExpandHomePath(path))
}

func handleDebugFlightLast(args []string) {
	limit, jsonOut, err := parseDebugFlightArgs(args, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}
	path := configuredDecisionsLogPath()
	if path == "" {
		fmt.Println("No decisions_log configured. Set [debug].decisions_log or SLIMFERENCE_DEBUG_DECISIONS_LOG.")
		return
	}
	summaries := readLastDecisionSummaries(path, limit)
	printFlightSummaries(summaries, jsonOut)
}

func handleDebugFlightTail(args []string) {
	limit, jsonOut, err := parseDebugFlightArgs(args, 20)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}
	path := configuredDecisionsLogPath()
	if path == "" {
		fmt.Println("No decisions_log configured. Set [debug].decisions_log or SLIMFERENCE_DEBUG_DECISIONS_LOG.")
		return
	}
	summaries := readLastDecisionSummaries(path, limit)
	printFlightSummaries(summaries, jsonOut)
}

func handleDebugFlightReplay(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: slimference debug flight replay <decisions.jsonl> [--json]")
		exitFn(1)
	}
	jsonOut := false
	for _, a := range args[1:] {
		if a == "--json" || a == "-json" {
			jsonOut = true
			continue
		}
		fmt.Fprintf(os.Stderr, "unknown argument: %s\n", a)
		exitFn(1)
	}
	summaries, err := replaySessionFn(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "flight replay: %v\n", err)
		exitFn(1)
	}
	printFlightSummaries(summaries, jsonOut)
}

func handleDebugFlightExport(args []string) {
	outPath, csvOut, err := parseDebugFlightExportArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}
	path := configuredDecisionsLogPath()
	if path == "" {
		fmt.Println("No decisions_log configured. Set [debug].decisions_log or SLIMFERENCE_DEBUG_DECISIONS_LOG.")
		return
	}
	summaries, err := replaySessionFn(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flight export: %v\n", err)
		exitFn(1)
	}
	if csvOut {
		err = writeFlightCSV(outPath, summaries)
	} else {
		err = writeFlightJSONL(outPath, summaries)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "flight export: %v\n", err)
		exitFn(1)
	}
	fmt.Printf("Exported %d flight record(s) to %s\n", len(summaries), outPath)
}

func parseDebugFlightExportArgs(args []string) (path string, csvOut bool, err error) {
	for _, arg := range args {
		switch arg {
		case "--csv", "-csv":
			csvOut = true
		case "":
			continue
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, fmt.Errorf("unknown flag: %s", arg)
			}
			if path != "" {
				return "", false, fmt.Errorf("unexpected extra argument: %s", arg)
			}
			path = arg
		}
	}
	if path == "" {
		return "", false, fmt.Errorf("usage: slimference debug flight export <out.jsonl|out.csv> [--csv]")
	}
	if strings.HasSuffix(strings.ToLower(path), ".csv") {
		csvOut = true
	}
	return path, csvOut, nil
}

func printFlightSummaries(summaries []dbg.RequestSummary, jsonOut bool) {
	flights := make([]dbg.FlightRequestSummary, 0, len(summaries))
	for _, summary := range summaries {
		summary.EnsureFlight()
		if summary.Flight != nil {
			flights = append(flights, *summary.Flight)
		}
	}
	if jsonOut {
		b, _ := json.MarshalIndent(flights, "", "  ")
		fmt.Println(string(b))
		return
	}
	if len(flights) == 0 {
		fmt.Println("No flight records found.")
		return
	}
	fmt.Printf("Flight recorder (%d request(s))\n", len(flights))
	fmt.Println(strings.Repeat("-", 50))
	for _, f := range flights {
		fmt.Printf("req_id:    %s\n", f.RequestID)
		fmt.Printf("source:    %s  route: %s  provider: %s\n", f.Source, f.RouteMode, f.Provider)
		fmt.Printf("tokens:    est_in %d -> %d  billable_saved_est=%d  provider_cached=%d\n",
			f.TokenAccounting.EstimatedOriginalInputTokens,
			f.TokenAccounting.EstimatedFinalInputTokens,
			f.TokenAccounting.BillableSavingsEstimate,
			f.TokenAccounting.ProviderCachedTokens)
		fmt.Printf("cache:     local=%v read=%d create=%d prev_response_id=%v billable_prev_id=%v\n",
			f.CacheAccounting.LocalResponseCacheHit,
			f.CacheAccounting.ProviderCacheReadTokens,
			f.CacheAccounting.ProviderCacheCreateTokens,
			f.CacheAccounting.PreviousResponseIDUsed,
			f.CacheAccounting.PreviousResponseIDBillable)
		if f.OutputReduce.Applied || f.OutputReduce.Reason != "" {
			fmt.Printf("output:    reduce=%v profile=%s shape=%s reason=%s added_tokens=%d\n",
				f.OutputReduce.Applied, f.OutputReduce.Profile, f.OutputReduce.TaskShape, f.OutputReduce.Reason, f.OutputReduce.AddedTokens)
		}
		fmt.Printf("privacy:   redacted=%v confidence=%s\n", f.PrivacyRedacted, f.Confidence)
		fmt.Println(strings.Repeat("-", 50))
	}
}

func writeFlightJSONL(path string, summaries []dbg.RequestSummary) error {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	for _, summary := range summaries {
		summary.EnsureFlight()
		_ = enc.Encode(summary.Flight)
	}
	return os.WriteFile(path, []byte(buf.String()), 0o600)
}

func writeFlightCSV(path string, summaries []dbg.RequestSummary) error {
	var buf strings.Builder
	encodeFlightCSV(&buf, summaries)
	return os.WriteFile(path, []byte(buf.String()), 0o600)
}

func encodeFlightCSV(out io.Writer, summaries []dbg.RequestSummary) {
	w := csv.NewWriter(out)
	_ = w.Write([]string{
		"request_id", "source", "route_mode", "provider", "host", "path",
		"estimated_original_input_tokens", "estimated_final_input_tokens",
		"provider_input_tokens", "provider_cached_tokens", "provider_output_tokens",
		"billable_savings_estimate", "wire_savings_estimate",
		"local_cache_hit", "previous_response_id_used", "output_reduce_applied",
		"output_reduce_task_shape", "confidence", "total_proxy_overhead_ms",
	})
	for _, summary := range summaries {
		summary.EnsureFlight()
		f := summary.Flight
		_ = w.Write([]string{
			f.RequestID,
			f.Source,
			f.RouteMode,
			f.Provider,
			f.Host,
			f.Path,
			strconv.Itoa(f.TokenAccounting.EstimatedOriginalInputTokens),
			strconv.Itoa(f.TokenAccounting.EstimatedFinalInputTokens),
			strconv.Itoa(f.TokenAccounting.ProviderInputTokens),
			strconv.Itoa(f.TokenAccounting.ProviderCachedTokens),
			strconv.Itoa(f.TokenAccounting.ProviderOutputTokens),
			strconv.Itoa(f.TokenAccounting.BillableSavingsEstimate),
			strconv.Itoa(f.TokenAccounting.WireSavingsEstimate),
			strconv.FormatBool(f.CacheAccounting.LocalResponseCacheHit),
			strconv.FormatBool(f.CacheAccounting.PreviousResponseIDUsed),
			strconv.FormatBool(f.OutputReduce.Applied),
			f.OutputReduce.TaskShape,
			f.Confidence,
			strconv.FormatFloat(f.TotalProxyOverheadMs, 'f', 2, 64),
		})
	}
	w.Flush()
}

func parseDebugPeriodArgs(args []string) (period string, jsonOut bool, err error) {
	for _, a := range args {
		switch a {
		case "--json", "-json":
			jsonOut = true
		default:
			if a == "" {
				continue
			}
			if strings.HasPrefix(a, "-") {
				return "", false, fmt.Errorf("unknown flag: %s", a)
			}
			if period == "" {
				period = a
			} else {
				return "", false, fmt.Errorf("unexpected extra argument: %s", a)
			}
		}
	}
	if period == "" {
		period = "today"
	}
	return period, jsonOut, nil
}

func handleDebugSummary(extra []string) {
	period, jsonOut, err := parseDebugPeriodArgs(extra)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}
	switch period {
	case "today", "week", "month", "all":
	default:
		fmt.Fprintln(os.Stderr, "usage: slimference debug summary [today|week|month|all] [--json]")
		exitFn(1)
	}
	db, ok := mustOpenFilterDB()
	if !ok {
		fmt.Println("No filter.db — no Layer-0 runs recorded yet.")
		return
	}
	defer db.Close()
	start, end, _ := analytics.FilterGainWindow(period, time.Now())
	agg, err := filter.QueryFilterRunsAggregate(db, start, end)
	if err != nil {
		fmt.Fprintf(os.Stderr, "filter_runs aggregate: %v\n", err)
		exitFn(1)
	}
	agg.Period = period
	if jsonOut {
		b, _ := json.MarshalIndent(agg, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Printf("Layer-0 filter_runs summary (%s)\n", period)
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("runs:              %d\n", agg.Runs)
	fmt.Printf("input_tokens:      %s\n", formatTokensPlain64(agg.InputTokens))
	fmt.Printf("output_tokens:     %s\n", formatTokensPlain64(agg.OutputTokens))
	fmt.Printf("est. tokens saved: %s\n", formatTokensPlain64(agg.TokensSavedEst))
	fmt.Println(strings.Repeat("-", 50))
}

func handleDebugTail(extra []string) {
	limit := 20
	jsonOut := false
	var gotLimit bool
	for _, a := range extra {
		switch a {
		case "--json", "-json":
			jsonOut = true
		default:
			if a == "" {
				continue
			}
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "unknown flag: %s\n", a)
				exitFn(1)
			}
			if gotLimit {
				fmt.Fprintln(os.Stderr, "usage: slimference debug tail [N] [--json]   (default N=20, max 500)")
				exitFn(1)
			}
			n, err := strconv.Atoi(a)
			if err != nil || n < 1 {
				fmt.Fprintln(os.Stderr, "usage: slimference debug tail [N] [--json]   (default N=20, max 500)")
				exitFn(1)
			}
			limit = n
			gotLimit = true
		}
	}
	if limit > 500 {
		limit = 500
	}
	db, ok := mustOpenFilterDB()
	if !ok {
		fmt.Println("No filter.db — no Layer-0 runs recorded yet.")
		return
	}
	defer db.Close()
	runs, err := filter.RecentFilterRuns(db, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "filter_runs: %v\n", err)
		exitFn(1)
	}
	if jsonOut {
		b, _ := json.MarshalIndent(runs, "", "  ")
		fmt.Println(string(b))
		return
	}
	if len(runs) == 0 {
		fmt.Println("No rows in filter_runs.")
		return
	}
	fmt.Printf("Layer-0 filter_runs (newest %d)\n", len(runs))
	fmt.Println(strings.Repeat("-", 50))
	for _, r := range runs {
		fmt.Printf("%d  %s  in=%d out=%d  %s\n",
			r.ID, r.CreatedAt.Format(time.RFC3339), r.InputTokens, r.OutputTokens, r.Command)
	}
	fmt.Println(strings.Repeat("-", 50))
}

func handleDebugReplay(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: slimference debug replay <session.jsonl>")
		exitFn(1)
	}
	path := args[0]
	lines, size, err := dbg.SessionFileStats(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		exitFn(1)
	}
	fmt.Println("Session replay (preview)")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("file:            %s\n", path)
	fmt.Printf("size:            %d bytes\n", size)
	fmt.Printf("non-empty lines: %d\n", lines)
	fmt.Println(strings.Repeat("-", 50))

	summaries, err := replaySessionFn(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay parse: %v\n", err)
		exitFn(1)
	}
	if len(summaries) == 0 {
		fmt.Println("No decodable request summaries found.")
		return
	}

	var totalSaved int
	for i, s := range summaries {
		totalSaved += s.Tokens.Saved
		fmt.Printf("\n#%d  %s  %s/%s\n", i+1, s.Timestamp.Format(time.RFC3339), s.Provider, s.Model)
		fmt.Printf("    tokens:  %d -> %d  saved: %d  ratio: %.2f\n",
			s.Tokens.Original, s.Tokens.Final, s.Tokens.Saved, s.Tokens.Ratio)
		if len(s.LayersApplied) > 0 {
			fmt.Printf("    layers:  %v\n", s.LayersApplied)
		}
		if len(s.Layer1Breakdown) > 0 {
			fmt.Println("    layer1:")
			for name, bd := range s.Layer1Breakdown {
				fmt.Printf("      %-22s blocks=%d  saved=%d\n", name, bd.Blocks, bd.Saved)
			}
		}
	}
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("TOTAL: %d request(s)  %d tokens saved\n", len(summaries), totalSaved)
}

func handleDebugLast(extra []string) {
	jsonOut := false
	n := 1
	for _, a := range extra {
		switch a {
		case "--json", "-json":
			jsonOut = true
		default:
			if v, err := strconv.Atoi(a); err == nil && v > 0 {
				n = v
			}
		}
	}

	// Try proxy decision log first (Layer 1-3 summaries from JSONL).
	cfg, _ := config.Load()
	if decisionsPath := strings.TrimSpace(cfg.Debug.DecisionsLog); decisionsPath != "" {
		decisionsPath = filepath.Clean(config.ExpandHomePath(decisionsPath))
		if summaries := readLastDecisionSummaries(decisionsPath, n); len(summaries) > 0 {
			if jsonOut {
				b, err := json.MarshalIndent(summaries, "", "  ")
				if err == nil {
					fmt.Println(string(b))
					return
				}
			}
			fmt.Printf("Last %d proxy request(s) from decision log\n", len(summaries))
			fmt.Println(strings.Repeat("-", 50))
			for _, s := range summaries {
				fmt.Printf("req_id:    %s\n", s.RequestID)
				fmt.Printf("time:      %s\n", s.Timestamp.Format(time.RFC3339))
				fmt.Printf("provider:  %s  model: %s\n", s.Provider, s.Model)
				fmt.Printf("tokens:    %d -> %d  saved: %d  ratio: %.2f\n",
					s.Tokens.Original, s.Tokens.Final, s.Tokens.Saved, s.Tokens.Ratio)
				fmt.Printf("layers:    %v\n", s.LayersApplied)
				if len(s.Layer1Breakdown) > 0 {
					fmt.Println("  layer1:")
					for name, bd := range s.Layer1Breakdown {
						fmt.Printf("    %-22s saved=%d\n", name, bd.Saved)
					}
				}
				fmt.Println(strings.Repeat("-", 50))
			}
			return
		}
	}

	// Fall back to Layer-0 filter_runs in SQLite.
	db, ok := mustOpenFilterDB()
	if !ok {
		fmt.Println("No filter.db and no decisions_log configured. No data available.")
		return
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query filter_runs: %v\n", err)
		exitFn(1)
	}
	if !ok {
		fmt.Println("No rows in filter_runs.")
		return
	}
	if jsonOut {
		b, _ := json.MarshalIndent(run, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Println("Last Layer-0 filter run")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("id:            %d\n", run.ID)
	fmt.Printf("command:       %s\n", run.Command)
	fmt.Printf("project_path:  %s\n", run.ProjectPath)
	fmt.Printf("input_tokens:  %d\n", run.InputTokens)
	fmt.Printf("output_tokens: %d\n", run.OutputTokens)
	fmt.Printf("savings_pct:   %.2f\n", run.SavingsPct)
	fmt.Printf("created_at:    %s\n", run.CreatedAt.Format(time.RFC3339))
	fmt.Println(strings.Repeat("-", 50))
}

// readLastDecisionSummaries reads the last n RequestSummaries from a decisions JSONL file.
// Returns newest first. Returns nil if the file doesn't exist or can't be read.
func readLastDecisionSummaries(path string, n int) []dbg.RequestSummary {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Collect all lines then return last n.
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil
	}
	if len(lines) == 0 {
		return nil
	}
	// Take last n lines.
	start := len(lines) - n
	if start < 0 {
		start = 0
	}
	tail := lines[start:]

	// Parse newest first.
	out := make([]dbg.RequestSummary, 0, len(tail))
	for i := len(tail) - 1; i >= 0; i-- {
		var s dbg.RequestSummary
		if err := json.Unmarshal([]byte(tail[i]), &s); err == nil && s.RequestID != "" {
			s.EnsureFlight()
			out = append(out, s)
		}
	}
	return out
}

func handleDebugPaths() {
	cfg, info, err := config.LoadWithOptions(config.LoadOptions{ExplicitPath: explicitConfigPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
	}
	configPath := "(built-in defaults)"
	if info.ResolvedPath != "" {
		configPath = info.ResolvedPath
	}
	configNote := debugConfigSourceLabel(info.Source)
	var filterLine, teeLine string
	if filterDB, ferr := resolveFilterDBPathFn(); ferr != nil {
		filterLine = fmt.Sprintf("(error: %v)", ferr)
	} else {
		fnote := "default"
		if os.Getenv("SLIMFERENCE_FILTER_DB") != "" {
			fnote = "SLIMFERENCE_FILTER_DB"
		} else if strings.TrimSpace(cfg.Filter.FilterDB) != "" {
			fnote = "[filter] filter_db"
		}
		filterLine = fmt.Sprintf("%s [%s]", filterDB, fnote)
	}
	if teeDir, terr := resolveTeeDirFn(); terr != nil {
		teeLine = fmt.Sprintf("(error: %v)", terr)
	} else {
		tnote := "default"
		if os.Getenv("SLIMFERENCE_TEE_DIR") != "" {
			tnote = "SLIMFERENCE_TEE_DIR"
		} else if strings.TrimSpace(cfg.Filter.TeeDir) != "" {
			tnote = "[filter] tee_dir"
		}
		teeLine = fmt.Sprintf("%s [%s]", teeDir, tnote)
	}
	dataDir, derr := filterDefaultDataDirFn()
	if derr != nil {
		dataDir = "(error: " + derr.Error() + ")"
	}
	dnote := "unset"
	if os.Getenv("SLIMFERENCE_DEBUG_DECISIONS_LOG") != "" {
		dnote = "SLIMFERENCE_DEBUG_DECISIONS_LOG"
	} else if strings.TrimSpace(cfg.Debug.DecisionsLog) != "" {
		dnote = "[debug] decisions_log"
	}
	decisionsLine := "(not configured)"
	if p := strings.TrimSpace(cfg.Debug.DecisionsLog); p != "" {
		decisionsLine = filepath.Clean(config.ExpandHomePath(p))
	}
	wd, wdErr := os.Getwd()
	projectFiltersLine := fmt.Sprintf("(getwd: %v)", wdErr)
	if wdErr == nil {
		pf := filter.ProjectFiltersPath(wd)
		if _, err := os.Stat(pf); err == nil {
			projectFiltersLine = pf + " [present]"
		} else {
			projectFiltersLine = pf + " [absent]"
		}
	}
	fmt.Println("Slimference debug paths")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("config file:      %s [%s]\n", configPath, configNote)
	fmt.Printf("analytics log:    %s\n", cfg.Analytics.ResolvedLogDir())
	fmt.Printf("data directory:   %s\n", dataDir)
	fmt.Printf("filter.db:        %s\n", filterLine)
	fmt.Printf("tee directory:    %s\n", teeLine)
	fmt.Printf("decisions log:    %s [%s]\n", decisionsLine, dnote)
	fmt.Printf("project filters:  %s\n", projectFiltersLine)
	fmt.Println(strings.Repeat("-", 50))
}

func debugConfigSourceLabel(source string) string {
	switch source {
	case "flag":
		return "--config"
	case "env":
		return "SLIMFERENCE_CONFIG"
	case "":
		return "defaults"
	default:
		return source
	}
}

func formatTokensPlain64(n int64) string {
	if n < 0 {
		n = 0
	}
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func printStatsTable(snapshots []analytics.AnalyticsSnapshot) {
	if len(snapshots) == 0 {
		return
	}
	// Aggregate.
	var total analytics.AnalyticsSnapshot
	total.SessionStart = snapshots[0].SessionStart
	for _, s := range snapshots {
		total.TotalRequests += s.TotalRequests
		total.TotalInputTokens += s.TotalInputTokens
		total.SavedInputTokens += s.SavedInputTokens
		total.TotalOutputTokens += s.TotalOutputTokens
		total.CacheHits += s.CacheHits
		total.SecretsRedacted += s.SecretsRedacted
	}

	ratio := 0
	if total.TotalInputTokens > 0 {
		ratio = int(float64(total.SavedInputTokens) / float64(total.TotalInputTokens) * 100)
	}

	fmt.Println("Slimference Stats")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Messages sent:       %d\n", total.TotalRequests)
	fmt.Printf("Input tokens (orig): %s\n", formatTokensPlain(total.TotalInputTokens))
	fmt.Printf("Input tokens saved:  %s (%d%%)\n", formatTokensPlain(total.SavedInputTokens), ratio)
	fmt.Printf("Output tokens:       %s\n", formatTokensPlain(total.TotalOutputTokens))
	fmt.Printf("Cache hits:          %d\n", total.CacheHits)
	fmt.Printf("Secrets redacted:    %d\n", total.SecretsRedacted)
	fmt.Println(strings.Repeat("-", 50))
}

func formatTokensPlain(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// setupLogging configures the global slog handler based on config.
func setupLogging(cfg *config.Config) {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.Logging.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}

	// When a log file is configured, write structured JSONL there via a rotating
	// writer (10 MB per file, keep 5 copies). This is the primary AI-readable
	// output; all slog.Debug/Info/Warn/Error calls end up here automatically.
	if logPath := config.ExpandHomePath(cfg.Logging.File); logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err == nil {
			if rw, err := slogutil.New(logPath, 0, 0); err == nil {
				slog.SetDefault(slog.New(slog.NewJSONHandler(rw, opts)))
				return
			}
		}
	}

	// Fallback: stderr with the user-selected format.
	var handler slog.Handler
	if cfg.Logging.Format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// isServerClosed returns true for the expected http.ErrServerClosed error.
func isServerClosed(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Server closed")
}

// serviceControlAdapter implements tui.ServiceControlInterface by calling daemon package functions
// and spawning subprocesses for hook install/remove.
type serviceControlAdapter struct{}

var (
	tuiInstallCmdFn            = runInstallCmd
	tuiEnableCmdFn             = runLabEnableCmd
	tuiDisableCmdFn            = runLabDisableCmd
	tuiUninstallCmdFn          = runUninstallCmd
	tuiCodexRouteEnableCmdFn   = runCodexEnableCmd
	tuiCodexRouteDisableCmdFn  = runCodexDisableCmd
	tuiCodexRouteHealthCheckFn = codexRouteHealthFn
	tuiCodexDesktopDirectFn    = func(dir string) error {
		args := []string{"-a", "Codex"}
		if dir != "" {
			args = append(args, dir)
		}
		cmd := exec.Command("open", args...)
		cmd.Env = codexDesktopDirectOpenEnv(os.Environ(), dir)
		return cmd.Run()
	}
	tuiLaunchCommandFn = func(name string, args ...string) error {
		return exec.Command(name, args...).Run()
	}
)

func (sca *serviceControlAdapter) StartDaemon() error {
	running, existing, err := daemonIsRunningFn()
	if err != nil {
		return fmt.Errorf("check daemon: %w", err)
	}
	if running {
		if existing == nil || existing.PID <= 0 {
			return fmt.Errorf("daemon state says running but PID metadata is invalid; run `slimference service status` and remove stale PID state if needed")
		}
		return fmt.Errorf("already running (PID %d, port %d)", existing.PID, existing.Port)
	}
	binary, err := resolveDaemonLifecycleBinary("start")
	if err != nil {
		return err
	}
	if err := startDetachedDaemonFn(binary); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	if _, err := waitForDaemonStarted(daemonStartTimeout, daemonStartPollInterval); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	return nil
}

func (sca *serviceControlAdapter) StopDaemon() error {
	return daemonStopFn()
}

func (sca *serviceControlAdapter) RestartDaemon() error {
	running, _, err := daemonIsRunningFn()
	if err != nil {
		return fmt.Errorf("check daemon: %w", err)
	}
	if running {
		if err := daemonStopFn(); err != nil {
			return err
		}
	}
	return sca.StartDaemon()
}

func (sca *serviceControlAdapter) InstallService() error {
	binary, err := resolveDaemonLifecycleBinary("service install")
	if err != nil {
		return err
	}
	return daemonInstallLaunchdFn(binary)
}

func (sca *serviceControlAdapter) UninstallService() error {
	return daemonUninstallFn()
}

func (sca *serviceControlAdapter) DaemonStatus() (bool, int, int) {
	running, pf, _ := daemonIsRunningFn()
	if !running || pf == nil {
		return false, 0, 0
	}
	return true, pf.PID, pf.Port
}

func (sca *serviceControlAdapter) DaemonNotice() string {
	return staleSlimferenceProcessNoticeFn()
}

func (sca *serviceControlAdapter) TransparentStatus() tui.TransparentStatus {
	home := os.Getenv("HOME")
	status := tui.TransparentStatus{}
	if home == "" {
		status.Detail = "HOME unresolved"
		return status
	}

	certPath := filepath.Join(home, ".slimference", "ca", "root.crt")
	if _, err := os.Stat(certPath); err == nil {
		status.CAExists = true
		trusted, trustErr := newTransparentKeychainFn().IsTrusted(certPath)
		status.CATrusted = trusted
		if trustErr != nil && status.Detail == "" {
			status.Detail = trustErr.Error()
		}
	}

	plistPath := transparent.DefaultPlistPath(home)
	status.AutoStartInstalled = newTransparentLaunchFn().IsInstalled(plistPath)

	snap := newTransparentNetworkFn().Status()
	if snap.UnreachableErr != nil {
		status.NetworkUnavailable = true
		if status.Detail == "" {
			status.Detail = snap.UnreachableErr.Error()
		}
		return status
	}
	for _, svc := range snap.Services {
		if !svc.HTTPSEnabled || !isSlimferenceProxyTarget(svc.HTTPSProxy, svc.HTTPSPort) {
			continue
		}
		status.ProxyArmed = true
		status.ActiveServices++
		if status.DaemonReachable {
			continue
		}
		if err := transparentProxyHealthFn(svc.HTTPSProxy, svc.HTTPSPort); err == nil {
			status.DaemonReachable = true
		} else if status.Detail == "" {
			status.Detail = err.Error()
		}
	}
	return status
}

func (sca *serviceControlAdapter) InstallTransparent() error {
	return sca.runInstallLifecycleCommand(tuiInstallCmdFn, nil)
}

func (sca *serviceControlAdapter) EnableTransparent() error {
	return sca.runInstallLifecycleCommand(tuiEnableCmdFn, nil)
}

func (sca *serviceControlAdapter) DisableTransparent() error {
	return sca.runInstallLifecycleCommand(tuiDisableCmdFn, nil)
}

func (sca *serviceControlAdapter) UninstallTransparent() error {
	return sca.runInstallLifecycleCommand(tuiUninstallCmdFn, nil)
}

func (sca *serviceControlAdapter) CodexRouteStatus() tui.CodexRouteStatus {
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		return tui.CodexRouteStatus{Detail: "HOME unresolved"}
	}
	proxyURL := codexroute.ProxyURL("127.0.0.1", "8990")
	status, err := codexRouteInspectFn(home, proxyURL, codexroute.Options{})
	out := tui.CodexRouteStatus{
		Exists:            status.Exists,
		Enabled:           status.Enabled,
		Complete:          status.Complete,
		Conflict:          status.Conflict,
		LegacyKeys:        status.LegacyKeys,
		Transport:         status.Transport,
		CertificationPath: codexroute.CertificationPath(home),
	}
	if err != nil {
		out.Detail = err.Error()
		return out
	}
	auto := codexAutoFn(home)
	out.AutoTransport = string(auto.Transport)
	out.AutoMode = string(auto.Mode)
	out.WSSCertified = auto.WSSCertified
	out.WSSBridgeAvailable = auto.WSSBridgeAvailable
	out.NeedsRecert = auto.NeedsRecert
	out.FallbackReason = auto.FallbackReason
	out.BridgeProofPath = auto.BridgeProofPath
	out.RecertStatePath = auto.RecertStatePath
	out.RecertLogPath = auto.RecertLogPath
	out.RecertStatus = auto.RecertStatus
	out.RecertAttemptID = auto.RecertAttemptID
	out.RecertStartedAt = auto.RecertStartedAt
	out.RecertFinishedAt = auto.RecertFinishedAt
	out.RecertLastSuccessAt = auto.RecertLastSuccessAt
	out.RecertRetryAfter = auto.RecertRetryAfter
	out.RecertLastError = auto.RecertLastError
	out.RecertCommand = auto.RecertCommand
	out.Detail = auto.LastWSSError
	if err := tuiCodexRouteHealthCheckFn("127.0.0.1", "8990"); err != nil {
		out.Detail = err.Error()
		return out
	}
	out.DaemonReachable = true
	if auto.NeedsRecert {
		codexAutoRecertFn(home, "127.0.0.1", "8990", auto)
	}
	return out
}

func (sca *serviceControlAdapter) CodexDesktopStatus() tui.CodexDesktopStatus {
	status := buildCodexDesktopStatus(codexDesktopStatusFlags{host: "127.0.0.1", port: "8990"})
	detail := status.DaemonError
	if detail == "" && len(status.Notes) > 0 {
		detail = status.Notes[0]
	}
	return tui.CodexDesktopStatus{
		Mode:                 status.Mode,
		FailureClass:         status.FailureClass,
		DaemonReachable:      status.DaemonReachable,
		CATrusted:            status.CATrust.Trusted,
		CAExists:             status.CATrust.Exists,
		ConversationObserved: status.ConversationObserved,
		Detail:               detail,
	}
}

func (sca *serviceControlAdapter) LaunchCodexCLI() (string, error) {
	binary, err := osExecutable()
	if err != nil {
		return "", fmt.Errorf("executable: %w", err)
	}
	dir, err := tuiLaunchDirectory()
	if err != nil {
		return "", err
	}
	inner := "for k in ${!CODEX_@}; do unset \"$k\"; done; cd " + shellQuote(dir) + " && " + shellQuote(binary) + " codex run --transport=auto --"
	cmdLine := "/bin/bash -lc " + shellQuote(inner)
	script := "tell application \"Terminal\" to do script " + strconv.Quote(cmdLine)
	if err := tuiLaunchCommandFn("osascript", "-e", script); err != nil {
		return "", fmt.Errorf("open Terminal: %w", err)
	}
	return "Codex CLI launched via Slimference transport=auto in " + dir, nil
}

func (sca *serviceControlAdapter) LaunchCodexApp() (string, error) {
	dir, err := tuiLaunchDirectory()
	if err != nil {
		return "", err
	}
	status := buildCodexDesktopStatus(codexDesktopStatusFlags{host: "127.0.0.1", port: "8990"})
	launchable := status.Mode == "desktop_app_server_phasef_proven" ||
		status.Mode == "desktop_app_server_proven" ||
		status.Mode == "desktop_app_server_route_ready"
	if !launchable || !status.ConversationObserved || status.FailureClass != "" {
		reason := status.FailureClass
		if reason == "" {
			reason = status.Mode
		}
		return "", fmt.Errorf("Desktop Slimference proof is not green (%s). Start Codex.app normally outside Slimference for direct mode, or run `slimference codex desktop prove --manual --json` to prove the app-server shim route", reason)
	}
	var out, errBuf strings.Builder
	rc := runCodexLaunchDesktopCmd(
		[]string{"--transport=app-server", "--replace-existing", "--env=PWD=" + dir},
		installPrinter{Out: &out, Err: &errBuf},
	)
	if rc != 0 {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = fmt.Sprintf("exit %d", rc)
		}
		return "", fmt.Errorf("launch Codex.app via Slimference: %s", msg)
	}
	msg := strings.TrimSpace(out.String())
	if msg == "" {
		msg = "Codex App launched via Slimference app-server shim in " + dir
	}
	return msg, nil
}

func tuiLaunchDirectory() (string, error) {
	dir, err := osGetwd()
	if err != nil {
		return "", fmt.Errorf("resolve launch directory: %w", err)
	}
	if dir == "" {
		return "", fmt.Errorf("resolve launch directory: empty working directory")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("launch directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("launch directory %q is not a directory", dir)
	}
	return dir, nil
}

func (sca *serviceControlAdapter) RepairCodexWSS() (string, error) {
	var stdout strings.Builder
	var stderr strings.Builder
	rc := runCodexRecertifyCmd([]string{"wss", "--force", "--operator=tui", "--notes=TUI Repair CLI WSS"}, installPrinter{Out: &stdout, Err: &stderr})
	if rc != 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = fmt.Sprintf("codex recertify failed with exit %d", rc)
		}
		return "", fmt.Errorf("%s", msg)
	}
	msg := strings.TrimSpace(stdout.String())
	if msg == "" {
		msg = "Codex CLI WSS repaired"
	}
	return msg, nil
}

func (sca *serviceControlAdapter) EnableCodexRoute() error {
	return sca.runInstallLifecycleCommand(tuiCodexRouteEnableCmdFn, nil)
}

func (sca *serviceControlAdapter) DisableCodexRoute() error {
	return sca.runInstallLifecycleCommand(tuiCodexRouteDisableCmdFn, nil)
}

func (sca *serviceControlAdapter) runInstallLifecycleCommand(fn func([]string, installPrinter) int, args []string) error {
	var stdout strings.Builder
	var stderr strings.Builder
	rc := fn(args, installPrinter{Out: &stdout, Err: &stderr})
	if rc == 0 {
		return nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = strings.TrimSpace(stdout.String())
	}
	if msg == "" {
		msg = fmt.Sprintf("install lifecycle command failed with exit %d", rc)
	}
	return fmt.Errorf("%s", msg)
}

func (sca *serviceControlAdapter) runTransparentProxyCommand(args ...string) error {
	var stdout strings.Builder
	var stderr strings.Builder
	rc := proxyRunFn(args, proxyCommandEnv(&stdout, &stderr, os.Stdin))
	if rc == 0 {
		return nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = strings.TrimSpace(stdout.String())
	}
	if msg == "" {
		msg = fmt.Sprintf("proxy %s failed with exit %d", strings.Join(args, " "), rc)
	}
	return fmt.Errorf("%s", msg)
}

func proxyCommandEnv(stdout io.Writer, stderr io.Writer, stdin io.Reader) proxyEnv {
	home := os.Getenv("HOME")
	return proxyEnv{
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  stdin,
		Home:   home,
		CADirFn: func() string {
			return filepath.Join(home, ".slimference")
		},
		Network:     newTransparentNetworkFn(),
		Keychain:    newTransparentKeychainFn(),
		Launch:      newTransparentLaunchFn(),
		LoadCA:      tlsca.LoadOrGenerateCA,
		HealthCheck: transparentProxyHealthFn,
	}
}

func (sca *serviceControlAdapter) InstallHook(target string) error {
	if target == "claude" {
		return fmt.Errorf("Claude Code hooks are parked; Slimference installs Codex hooks only")
	}
	home, err := osUserHomeDir()
	if err != nil {
		return fmt.Errorf("home: %w", err)
	}
	var tpCmd string
	if cfg, err := configLoadFn(); err == nil {
		tpCmd = strings.TrimSpace(cfg.Hooks.SlimferenceCommand)
	}
	switch target {
	case "codex":
		return installCodexHookFn(home, tpCmd)
	default:
		return fmt.Errorf("unknown hook target: %s", target)
	}
}

func (sca *serviceControlAdapter) RemoveHook(target string) error {
	if target == "claude" {
		return fmt.Errorf("Claude Code hooks are parked; Slimference will not modify ~/.claude")
	}
	home, err := osUserHomeDir()
	if err != nil {
		return fmt.Errorf("home: %w", err)
	}
	switch target {
	case "codex":
		return removeCodexHookFn(home)
	default:
		return fmt.Errorf("unknown hook target: %s", target)
	}
}

func startDetachedDaemon(binary string) error {
	logDir := filepath.Dir(daemonStdoutLogPathFn())
	if err := osMkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	stdinFile, err := osOpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open stdin: %w", err)
	}
	defer stdinFile.Close()

	stdoutFile, err := osOpenFile(daemonStdoutLogPathFn(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open stdout log: %w", err)
	}
	defer stdoutFile.Close()

	stderrFile, err := osOpenFile(daemonStderrLogPathFn(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open stderr log: %w", err)
	}
	defer stderrFile.Close()

	attrs := &os.ProcAttr{
		Files: []*os.File{stdinFile, stdoutFile, stderrFile},
		Env:   osEnvironFn(),
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}
	proc, err := osStartProcess(binary, []string{binary, "daemon"}, attrs)
	if err != nil {
		return err
	}
	return proc.Release()
}

// --- Daemon / Service commands ---

// startProxyForDaemon creates and starts a proxy, returning port and shutdown.
// Shared by handleDaemonCmd and handleStartCmd.
func startProxyForDaemon() (port int, shutdown func(ctx context.Context) error, err error) {
	cfg, err := configLoadFn()
	if err != nil {
		return 0, nil, fmt.Errorf("config load: %w", err)
	}
	setupLogging(cfg)
	p := newProxyFn(cfg)
	ensureSlimDataDir()
	startProxyInstance = p
	startProxyConfig = cfg
	startProxyAppsManager = wirePhaseG(p, cfg)
	startProxyHostsCleanup = applyHostsPatch(cfg)
	_, sniCancel := startSNIPeekEngineFn(p, cfg, startProxyAppsManager)
	startProxySNICancel = sniCancel
	applyPersistedRuntimeState(p)

	runner := proxyStartRunnerFn
	hasListener := proxyHasListenerFn
	after := timeAfterFn
	newTicker := newTickerFn
	errCh := make(chan error, 1)
	go func() {
		if err := runner(p); err != nil && !isServerClosed(err) {
			errCh <- err
		}
	}()

	deadline := after(proxyStartTimeout)
	ticker := newTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-errCh:
			return 0, nil, fmt.Errorf("proxy start: %w", err)
		case <-ticker.C:
			if hasListener(p) {
				startProxyPIDCleanup = writePIDFileFn()
				return cfg.Proxy.ListenPort, p.Shutdown, nil
			}
		case <-deadline:
			if hasListener(p) {
				startProxyPIDCleanup = writePIDFileFn()
				return cfg.Proxy.ListenPort, p.Shutdown, nil
			}
			return 0, nil, fmt.Errorf("proxy start: timeout after %s", proxyStartTimeout)
		}
	}
}

func applyPersistedRuntimeState(p *proxy.Proxy) {
	state, err := loadTUIStateFn()
	if err != nil || state == nil {
		return
	}
	p.SetProviderEnabled(types.Anthropic, state.ClaudeEnabled)
	p.SetProviderEnabled(types.OpenAI, state.CodexEnabled)
	p.SetLayerEnabled(1, state.Layer1Enabled)
	p.SetLayerEnabled(2, state.Layer2Enabled)
}

func handleDaemonCmd(args []string) {
	if len(args) == 0 {
		if err := daemonRunFn(startProxyForDaemon); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			exitFn(1)
		}
		return
	}
	switch args[0] {
	case "logs":
		handleDaemonLogsCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown daemon subcommand: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Usage: slimference daemon [logs [--path | --stream=stdout|stderr | --since=<dur> | --lines=<N>]]")
		exitFn(1)
	}
}

func runDaemonWithSlimferenceReload(start func() (int, func(context.Context) error, error)) error {
	return daemonRunWithReloadFn(start, func() {
		slog.Info("SIGHUP: reloading apps policy + SNIPeekMode")
		reloadAppsManager(startProxyAppsManager)
		reloadSNIPeekModeFromDisk(startProxyConfig)
	})
}

// handleDaemonLogsCmd implements `slimference daemon logs [flags]`.
// T30: exposes the launchd-managed stdout/stderr logs without the user
// needing to remember the file paths.
//
// Flags:
//
//	--path                    print the log file paths and exit
//	--stream=stdout|stderr|both  default: both
//	--lines=<N>               default: 200
//	--since=<duration>        e.g. 10m, 2h - drops older lines
func handleDaemonLogsCmd(args []string) {
	flags := parseDaemonLogsFlags(args)
	if flags.err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", flags.err)
		exitFn(1)
		return
	}
	stdoutPath := daemonStdoutLogPathFn()
	stderrPath := daemonStderrLogPathFn()

	if flags.showPath {
		fmt.Printf("stdout: %s\nstderr: %s\n", stdoutPath, stderrPath)
		return
	}

	var cutoff time.Time
	if flags.since > 0 {
		cutoff = time.Now().Add(-flags.since)
	}

	print := func(label, path string) {
		lines, err := daemonReadRecentLogLinesFn(path, flags.lines, cutoff)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s log: %v\n", label, err)
			return
		}
		if len(lines) == 0 {
			fmt.Fprintf(os.Stderr, "no %s lines (path=%s)\n", label, path)
			return
		}
		fmt.Printf("=== %s: %s (%d lines) ===\n", label, path, len(lines))
		for _, line := range lines {
			fmt.Println(line)
		}
	}

	switch flags.stream {
	case "stdout":
		print("stdout", stdoutPath)
	case "stderr":
		print("stderr", stderrPath)
	default:
		print("stdout", stdoutPath)
		print("stderr", stderrPath)
	}
}

type daemonLogsFlags struct {
	showPath bool
	stream   string
	lines    int
	since    time.Duration
	err      error
}

func parseDaemonLogsFlags(args []string) daemonLogsFlags {
	f := daemonLogsFlags{stream: "both", lines: 200}
	for _, a := range args {
		switch {
		case a == "":
			continue
		case a == "--path":
			f.showPath = true
		case strings.HasPrefix(a, "--stream="):
			v := strings.TrimPrefix(a, "--stream=")
			if v != "stdout" && v != "stderr" && v != "both" {
				f.err = fmt.Errorf("--stream must be stdout|stderr|both, got %q", v)
				return f
			}
			f.stream = v
		case strings.HasPrefix(a, "--lines="):
			v := strings.TrimPrefix(a, "--lines=")
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				f.err = fmt.Errorf("--lines must be a positive integer, got %q", v)
				return f
			}
			f.lines = n
		case strings.HasPrefix(a, "--since="):
			v := strings.TrimPrefix(a, "--since=")
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				f.err = fmt.Errorf("--since must be a positive duration, got %q", v)
				return f
			}
			f.since = d
		default:
			f.err = fmt.Errorf("unknown flag: %s", a)
			return f
		}
	}
	return f
}

// daemonStdoutLogPathFn / daemonStderrLogPathFn / daemonReadRecentLogLinesFn
// are overridable in tests so the CLI can be exercised without touching the
// real launchd log files.
var (
	daemonStdoutLogPathFn      = daemon.LaunchdStdoutLogPath
	daemonStderrLogPathFn      = daemon.LaunchdStderrLogPath
	daemonReadRecentLogLinesFn = daemon.ReadRecentLogLines
)

func resolveDaemonLifecycleBinary(verb string) (string, error) {
	binary, err := osExecutable()
	if err != nil {
		return "", fmt.Errorf("executable: %w", err)
	}
	if isTemporaryGoBuildExecutable(binary) {
		return "", fmt.Errorf("%s: executable path %q looks like a temporary Go build artifact; run `go run ./scripts/build --install` first, then use `~/.local/bin/slimference %s`", verb, binary, verb)
	}
	return binary, nil
}

func isTemporaryGoBuildExecutable(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	if !strings.Contains(clean, "/go-build") {
		return false
	}
	tmp := filepath.ToSlash(filepath.Clean(os.TempDir()))
	if tmp != "." && strings.HasPrefix(clean, tmp+"/") {
		return true
	}
	return strings.Contains(clean, "/T/go-build")
}

func handleStartCmd() {
	running, existing, err := daemonIsRunningFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "check daemon: %v\n", err)
		exitFn(1)
	}
	if running {
		if existing == nil || existing.PID <= 0 {
			fmt.Fprintln(os.Stderr, "daemon state says running but PID metadata is invalid; run `slimference service status` and remove stale PID state if needed")
			exitFn(1)
		}
		fmt.Fprintf(os.Stderr, "already running (PID %d, port %d)\n", existing.PID, existing.Port)
		exitFn(1)
	}
	// Fork into background via exec of self with "daemon" subcommand.
	binary, err := resolveDaemonLifecycleBinary("start")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}
	if err := startDetachedDaemonFn(binary); err != nil {
		fmt.Fprintf(os.Stderr, "start daemon: %v\n", err)
		exitFn(1)
	}
	started, err := waitForDaemonStarted(daemonStartTimeout, daemonStartPollInterval)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start daemon: %v\n", err)
		fmt.Fprintln(os.Stderr, "  check `slimference service logs --stream=stderr` for daemon startup errors")
		exitFn(1)
	}
	if started != nil && started.PID > 0 {
		fmt.Printf("Slimference daemon started. PID %d, port %d.\n", started.PID, started.Port)
		return
	}
	fmt.Println("Slimference daemon started.")
}

func handleStopCmd() {
	if err := daemonStopFn(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}
}

func handleRestartCmd() {
	running, _, err := daemonIsRunningFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "check daemon: %v\n", err)
		exitFn(1)
	}
	if running {
		if err := daemonStopFn(); err != nil {
			fmt.Fprintf(os.Stderr, "stop: %v\n", err)
			exitFn(1)
		}
	}
	handleStartCmd()
}

func waitForDaemonStarted(timeout, interval time.Duration) (*daemon.PIDFile, error) {
	deadline := timeAfterFn(timeout)
	ticker := newTickerFn(interval)
	defer ticker.Stop()

	for {
		running, pf, err := daemonIsRunningFn()
		if err != nil {
			return nil, fmt.Errorf("check daemon: %w", err)
		}
		if running {
			return pf, nil
		}
		select {
		case <-ticker.C:
		case <-deadline:
			return nil, fmt.Errorf("timeout after %s", timeout)
		}
	}
}

// postInstallHealthProbeTimeout bounds the health probe in seconds. Test-
// injectable.
var postInstallHealthProbeTimeout = 10 * time.Second

// healthProbeFn is the injectable probe used by post-install. Production
// uses net/http against the proxy's /admin/health endpoint. Tests replace
// this to short-circuit.
var healthProbeFn = defaultHealthProbe

func defaultHealthProbe(url string, timeout time.Duration) (ok bool, status string) {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true, strings.TrimSpace(string(body))
			}
			return false, fmt.Sprintf("http %d", resp.StatusCode)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false, "timeout waiting for daemon"
}

// runPostInstallHealthProbe polls the daemon health endpoint after a
// `service install` so the user gets immediate feedback on whether the
// daemon actually started or is looping under launchd KeepAlive.
func runPostInstallHealthProbe() {
	url := "http://127.0.0.1:8990/admin/health"
	ok, status := healthProbeFn(url, postInstallHealthProbeTimeout)
	if ok {
		fmt.Printf("Health probe: ok (%s)\n", status)
		return
	}
	fmt.Printf("Health probe: degraded (%s)\n", status)
	fmt.Println("  → check `slimference service status` and the log file")
	fmt.Println("    at ~/.slimference/logs/daemon.stderr.log")
}

func handleServiceCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference service <install|uninstall|start|stop|restart|status|logs>")
		exitFn(1)
	}
	switch args[0] {
	case "install":
		binary, err := resolveDaemonLifecycleBinary("service install")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			exitFn(1)
		}
		if err := daemonInstallLaunchdFn(binary); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			exitFn(1)
		}
		fmt.Println("Service installed. Slimference will start at login.")
		// T68: post-install health probe so the user sees whether launchd
		// successfully started the binary without having to check manually.
		runPostInstallHealthProbe()
	case "uninstall":
		if err := daemonUninstallFn(); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			exitFn(1)
		}
		fmt.Println("Service uninstalled.")
	case "start":
		handleStartCmd()
	case "stop":
		handleStopCmd()
	case "restart":
		handleRestartCmd()
	case "status":
		data, err := daemonFormatStatusFn()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			exitFn(1)
		}
		fmt.Println(string(data))
	case "logs":
		handleDaemonLogsCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown service command: %s\n", args[0])
		exitFn(1)
	}
}

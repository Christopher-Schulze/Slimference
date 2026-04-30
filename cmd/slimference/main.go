// Command slimference is a transparent HTTP reverse proxy that applies multi-layer
// token compression to LLM API requests, extending effective usage limits by 2-3x.
//
// Usage:
//
//	slimference                    # Start TUI + proxy
//	slimference config init        # Generate default config file
//	slimference config show        # Print resolved config
//	slimference test minimax       # Test MiniMax API connectivity
//	slimference test anthropic     # Test Anthropic reachability
//	slimference test openai        # Test OpenAI reachability
//	slimference doctor             # Run all diagnostics
//	slimference stats today        # Print today's stats
//	slimference stats prompt-cache week --json # Prompt-cache report
//	slimference gain today         # Layer-0 filter.db savings (--by-command, --csv, --project; optional USD/M rate in config)
//	slimference filter -- <cmd>    # Layer-0: subprocess + ANSI strip + DB log
//	slimference rewrite -- <cmd>   # Print command line; or pipe hook JSON (field "command") on stdin
//	slimference posttool          # Compact PostToolUse hook JSON from stdin for Codex
//	slimference hook install claude # Install Claude Code / Codex hooks (v1)
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
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/buildinfo"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/daemon"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/hooks"
	"github.com/slimference/slimference/internal/integrate"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/readcache"
	"github.com/slimference/slimference/internal/repetition"
	"github.com/slimference/slimference/internal/slogutil"
	"github.com/slimference/slimference/internal/summarization"
	"github.com/slimference/slimference/internal/toolarchive"
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
	resolveFilterDBPathFn        = resolveFilterDBPath
	resolveTeeDirFn              = resolveTeeDir
	filterDefaultDataDirFn       = filter.DefaultDataDir
	writeGainByCommandCSV        = analytics.WriteGainByCommandCSV
	writeGainSummaryCSV          = analytics.WriteGainSummaryCSV
	replaySessionFn              = dbg.ReplaySession
	daemonIsRunningFn            = daemon.IsRunning
	daemonStopFn                 = daemon.StopDaemon
	daemonInstallLaunchdFn       = daemon.InstallLaunchd
	daemonUninstallFn            = daemon.UninstallLaunchd
	daemonFormatStatusFn         = daemon.FormatStatus
	daemonRunFn                  = daemon.RunDaemon
	installClaudeHookFn          = hooks.InstallClaude
	installCodexHookFn           = installCodexIntegrationHook
	removeClaudeHookFn           = hooks.RemoveClaude
	removeCodexHookFn            = removeCodexIntegrationHook
	loadTUIStateFn               = tui.LoadPersistedState

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
	proxyStartTimeout = 200 * time.Millisecond
	// runTeaProgramFn injects tea.Program.Run so tests can return immediately without a terminal.
	runTeaProgramFn = (*tea.Program).Run
	// tuiSendProxyEventFn injects tui.SendProxyEvent so progSender.send can be tested without a running program.
	tuiSendProxyEventFn   func(*tea.Program, types.RequestMetrics) = tui.SendProxyEvent
	startDetachedDaemonFn                                          = startDetachedDaemon
	makeSignalChanFn                                               = func() chan os.Signal {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		return ch
	}
)

func installCodexIntegrationHook(home string, slimferenceCmd string) error {
	if err := hooks.InstallCodex(home, slimferenceCmd); err != nil {
		return err
	}
	if _, err := integrate.WriteCodexBlock(home, integrate.ProxyURL); err != nil {
		return fmt.Errorf("codex config: %w", err)
	}
	return nil
}

func removeCodexIntegrationHook(home string) error {
	if err := hooks.RemoveCodex(home); err != nil {
		return err
	}
	if _, err := integrate.RemoveCodexBlock(home); err != nil {
		return fmt.Errorf("codex config remove: %w", err)
	}
	return nil
}

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
		"readhook": true, "posttool": true, "checkpoint": true, "expand": true,
		"hook": true, "debug": true, "daemon": true, "start": true, "stop": true,
		"restart": true, "service": true, "integrate": true, "bypass": true,
		"completion": true, "trust": true,
		"help": true,
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
//	--no-layer2            Disable Layer 2 (MiniMax summarization)
//	--no-layer3            Disable Layer 3 (response caching)
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
		case "--no-layer3":
			cfg.Compression.Layer3Enabled = false
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
			fmt.Fprintln(os.Stderr, "usage: slimference test <minimax|anthropic|openai|intercept>")
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

	case "checkpoint":
		handleCheckpointCmd(args[1:])

	case "expand":
		handleExpandCmd(args[1:])

	case "hook":
		handleHookCmd(args[1:])

	case "debug":
		handleDebugCmd(args[1:])

	case "daemon":
		handleDaemonCmd(args[1:])

	case "start":
		handleStartCmd()

	case "stop":
		handleStopCmd()

	case "restart":
		handleRestartCmd()

	case "service":
		handleServiceCmd(args[1:])

	case "integrate":
		handleIntegrateCmd(args[1:])

	case "bypass":
		handleBypassCmd(args[1:])

	case "completion":
		handleCompletionCmd(args[1:])

	case "trust":
		handleTrustCmd(args[1:])

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Run 'slimference' to start the TUI, or use: config, test, doctor, stats, gain, filter, rewrite, readhook, posttool, checkpoint, expand, hook, debug, daemon, start, stop, restart, service, completion, trust, version")
		exitFn(1)
	}
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
	// rewriteEmit applies the §4.2 rewrite pipeline to cmdLine and exits with
	// the appropriate hook exit code:
	//   0 = rewrite applied, stdout contains rewritten command
	//   1 = no filter matched, hook should passthrough original unchanged
	//   2 = deny rule matched, hook should block the command
	//   3 = ask (sudo) required, hook should prompt before running
	rewriteEmit := func(cmdLine string) {
		// Permission check runs first (deny/ask take priority over filter matching).
		if code, msg := layer0PermissionCheck(cmdLine); code != 0 {
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
			exitFn(1)
		}
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
	details, err := filter.ExtractPostToolDetailsFromHookJSON(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
	}

	wd, err := osGetwd()
	if err != nil {
		wd = ""
	}
	maxOut := config.Defaults().Filter.PassthroughMaxChars
	if cfg, err := configLoadFn(); err == nil {
		maxOut = cfg.Filter.PassthroughMaxChars
	}

	compacted, changed := filter.CompactCapturedOutput(wd, details.CommandLine, details.ToolResponse, maxOut)

	// T93 cross-session pattern mining: when the same (session, tool,
	// command, output) tuple has been observed multiple times, replace
	// the captured output with a marker pointing at the first message.
	// Best-effort: storage errors leave the original behaviour intact.
	if home, err := osUserHomeDir(); err == nil && details.SessionID != "" {
		if repDB, repErr := repetition.Open(repetition.DefaultPath(home)); repErr == nil {
			count, firstMsg, _ := repetition.Record(repDB, repetition.Key{
				SessionID: details.SessionID,
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

	if home, err := osUserHomeDir(); err == nil {
		entry, archiveErr := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
			ToolName:  details.ToolName,
			ToolUseID: details.ToolUseID,
			SessionID: details.SessionID,
			Command:   details.CommandLine,
			Output:    details.ToolResponse,
			Preview:   string(compacted),
		})
		if archiveErr == nil && entry != nil {
			out := map[string]interface{}{
				"hookSpecificOutput": map[string]interface{}{
					"hookEventName":     "PostToolUse",
					"additionalContext": toolarchive.RenderContext(*entry),
				},
			}
			if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
				fmt.Fprintf(os.Stderr, "encode posttool output: %v\n", err)
				exitFn(1)
			}
			return
		}
	}

	if !changed || len(compacted) == 0 {
		return
	}

	context := "Recent Bash output was compacted locally."
	if details.CommandLine != "" {
		context = fmt.Sprintf("Bash output for %q was compacted locally.\n%s", details.CommandLine, compacted)
	} else {
		context = fmt.Sprintf("Bash output was compacted locally.\n%s", compacted)
	}

	out := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "PostToolUse",
			"additionalContext": context,
		},
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode posttool output: %v\n", err)
		exitFn(1)
	}
}

func handleReadHookCmd(args []string) {
	mode := "claude"
	for _, arg := range args {
		switch arg {
		case "", "--":
		case "claude":
			mode = "claude"
		case "codex":
			mode = "codex"
		default:
			fmt.Fprintln(os.Stderr, "usage: slimference readhook [claude|codex]   (pipe Read hook JSON on stdin)")
			exitFn(1)
		}
	}
	if termIsTerminalFn(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "usage: slimference readhook [claude|codex]   (pipe Read hook JSON on stdin)")
		exitFn(1)
	}

	payload, err := readStdinAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		exitFn(1)
	}
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
	decision, err := readcache.Evaluate(readcache.DefaultDir(home), req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "readhook: %v\n", err)
		exitFn(1)
	}
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
			fmt.Fprintln(os.Stderr, "usage: slimference hook install <claude|codex>")
			exitFn(1)
		}
		switch args[1] {
		case "claude":
			if err := hooks.InstallClaude(home, tpCmd); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				exitFn(1)
			}
			fmt.Println("Installed Claude Code hook (~/.claude/hooks/slimference-rewrite.sh).")
		case "codex":
			if err := installCodexHookFn(home, tpCmd); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				exitFn(1)
			}
			fmt.Println("Installed Codex hooks and config (~/.codex/hooks.json + config.toml + AGENTS.md).")
		default:
			fmt.Fprintf(os.Stderr, "unknown install target: %s (want claude|codex)\n", args[1])
			exitFn(1)
		}
	case "remove":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: slimference hook remove <claude|codex>")
			exitFn(1)
		}
		switch args[1] {
		case "claude":
			if err := hooks.RemoveClaude(home); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				exitFn(1)
			}
			fmt.Println("Removed Claude Code Slimference hook files.")
		case "codex":
			if err := removeCodexHookFn(home); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				exitFn(1)
			}
			fmt.Println("Removed Slimference hooks from Codex (hooks.json + config.toml + AGENTS.md).")
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
		fmt.Println("Next: set MINIMAX_API_KEY and run 'slimference doctor'")

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
	case "minimax":
		testMiniMax(cfg)
	case "anthropic":
		testUpstream("Anthropic", cfg.Upstream.Anthropic.BaseURL)
	case "openai":
		testUpstream("OpenAI", cfg.Upstream.OpenAI.BaseURL)
	case "intercept":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: slimference test intercept <claude|codex>")
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

func testMiniMax(cfg *config.Config) {
	apiKey := cfg.Compression.MiniMax.APIKey()
	if apiKey == "" {
		fmt.Printf("FAIL: %s env var not set\n", cfg.Compression.MiniMax.APIKeyEnv)
		exitFn(1)
	}
	fmt.Printf("Testing MiniMax connectivity (%s)...\n", cfg.Compression.MiniMax.BaseURL)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(strings.TrimSuffix(cfg.Compression.MiniMax.BaseURL, "/v1"))
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		exitFn(1)
	}
	defer resp.Body.Close()
	fmt.Printf("OK - HTTP %d (API key present)\n", resp.StatusCode)
}

func testIntercept(cfg *config.Config, provider string) {
	fmt.Printf("Starting intercept test for %s...\n", provider)
	fmt.Printf("Listening on %s\n\n", cfg.ListenURL())

	switch provider {
	case "claude":
		fmt.Println("In another terminal run:")
		fmt.Printf("  ANTHROPIC_BASE_URL=%s claude 'say hi'\n\n", cfg.ListenURL())
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
		select {
		case received <- struct{}{}:
		default:
		}
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
		fmt.Printf("  1. Is ANTHROPIC_BASE_URL set to %s?\n", cfg.ListenURL())
		fmt.Printf("  2. Is the CLI configured to use %s?\n", cfg.ListenURL())
		fmt.Println("  3. Try: curl " + cfg.ListenURL() + "/health")
		shutdown()
		exitFn(1)
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

	check("MiniMax API key", func() (string, bool) {
		if cfg.Compression.MiniMax.APIKey() == "" {
			return fmt.Sprintf("not set (%s env var missing) - Layer 2 disabled", cfg.Compression.MiniMax.APIKeyEnv), false
		}
		return "present", true
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

	check("Determinism gate", func() (string, bool) {
		if !cfg.Compression.Summary.RequireDeterministic {
			return "off (no strict-determinism check)", true
		}
		// Strict mode: MiniMax is the only deterministic provider in
		// the chain today. Warn loudly when EnableSeed is off because
		// the MiniMax client will not emit `seed` and a future
		// fallback would silently break reproducibility.
		if !cfg.Compression.MiniMax.EnableSeed {
			return "require_deterministic=on but [compression.minimax] enable_seed=false - MiniMax will be skipped", false
		}
		return "on (MiniMax: temperature=0 + seed)", true
	})

	check("Prompt override", func() (string, bool) {
		if cfg.Compression.PromptOverridePath == "" {
			return "default (no override path configured)", true
		}
		return fmt.Sprintf("%s (active version: %s)",
			cfg.Compression.PromptOverridePath,
			summarization.PromptVersion()), true
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
		fmt.Fprintln(os.Stderr, "usage: slimference gain [today|week|month|all] [--json] [--by-command] [--csv] [--project <path>]  (USD: [analytics] gain_usd_per_million_tokens or SLIMFERENCE_GAIN_USD_PER_MILLION)")
		exitFn(1)
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
	rep, err := analytics.QueryFilterGainReport(path, period, time.Now(), flags.byCommand, flags.project, usdRate)
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
}

func handleDebugCmd(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: slimference debug <paths|last|summary|tail|replay>")
		fmt.Fprintln(os.Stderr, "  paths — show resolved config file, analytics log, filter.db, tee dir")
		fmt.Fprintln(os.Stderr, "  last    — print last filter_runs row (optional --json)")
		fmt.Fprintln(os.Stderr, "  summary — aggregate for today|week|month|all (default today, --json)")
		fmt.Fprintln(os.Stderr, "  tail    — newest N rows (default 20, max 500, --json)")
		fmt.Fprintln(os.Stderr, "  replay  — replay session JSONL (RequestSummary per-request breakdown)")
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
	default:
		fmt.Fprintf(os.Stderr, "unknown debug subcommand: %s\n", args[0])
		exitFn(1)
	}
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
		if s.Layer2.Applied {
			fmt.Printf("    layer2:  ratio=%.2f  anchors=%d\n", s.Layer2.CompressionRatio, s.Layer2.AnchorCount)
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
			out = append(out, s)
		}
	}
	return out
}

func handleDebugPaths() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
	}
	configPath := config.DefaultConfigPath()
	configNote := "default"
	if p := os.Getenv("SLIMFERENCE_CONFIG"); p != "" {
		configPath = p
		configNote = "SLIMFERENCE_CONFIG"
	}
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
		total.MiniMaxCalls += s.MiniMaxCalls
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
	fmt.Printf("MiniMax calls:       %d\n", total.MiniMaxCalls)
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

// proxyAdapter adapts proxy.Proxy to tui.ProxyInterface to avoid import cycle.
type proxyAdapter struct {
	p *proxy.Proxy
}

func newProxyAdapter(p *proxy.Proxy) tui.ProxyInterface {
	return &proxyAdapter{p: p}
}

func (a *proxyAdapter) SetProviderEnabled(prov types.Provider, enabled bool) {
	a.p.SetProviderEnabled(prov, enabled)
}
func (a *proxyAdapter) SetLayerEnabled(layer int, enabled bool) {
	a.p.SetLayerEnabled(layer, enabled)
}
func (a *proxyAdapter) IsProviderEnabled(prov types.Provider) bool {
	return a.p.IsProviderEnabled(prov)
}
func (a *proxyAdapter) IsLayerEnabled(layer int) bool {
	return a.p.IsLayerEnabled(layer)
}
func (a *proxyAdapter) FlushCaches() {
	a.p.FlushCaches()
}
func (a *proxyAdapter) GetAnalytics() analytics.AnalyticsSnapshot {
	return a.p.GetAnalytics()
}
func (a *proxyAdapter) GetRecentRequests(n int) []types.RequestMetrics {
	return a.p.GetRecentRequests(n)
}
func (a *proxyAdapter) GetLayer2Status() tui.Layer2Status {
	cache := a.p.GetLayer2Cache()
	if cache == nil {
		return tui.Layer2Status{}
	}
	cs := cache.Get()
	status := tui.Layer2Status{
		HasCache:    cs != nil,
		Compressing: cache.Compressing.Load(),
		QueueDepth:  len(a.p.CompressQueue()),
	}
	if cs != nil {
		status.LastRun = cs.CreatedAt
	}
	return status
}
func (a *proxyAdapter) GetQualityStatus() tui.QualityStatus {
	q := a.p.QualitySnapshot()
	return tui.QualityStatus{
		ReReadSessions:    q.ReRead.Sessions,
		ReReadTotalChecks: q.ReRead.TotalChecks,
		ReReadTotalHits:   q.ReRead.TotalHits,
		ReReadRate:        q.ReRead.Rate,
		BaselineHitRate:   q.CacheMissSpike.BaselineRate,
		SpikeActive:       q.CacheMissSpike.Active,
		LastSpikeUnix:     q.CacheMissSpike.LastSpikeUnix,
		TotalSpikeCount:   q.CacheMissSpike.TotalSpikeCount,
		TotalSaved:        q.NetSavings.TotalSaved,
		TotalInvalidation: q.NetSavings.TotalInvalidation,
		NetSaved:          q.NetSavings.NetSaved,
	}
}
func (a *proxyAdapter) GetReadCacheStatus() tui.ReadCacheStatus {
	status := a.p.AdminStatusSnapshot().ReadCache
	return tui.ReadCacheStatus{
		Evaluations:     status.Evaluations,
		Allows:          status.Allows,
		Blocks:          status.Blocks,
		UnchangedBlocks: status.UnchangedBlocks,
		DeltaBlocks:     status.DeltaBlocks,
		Sessions:        status.Sessions,
		TrackedFiles:    status.TrackedFiles,
	}
}
func (a *proxyAdapter) GetProviderHealth(prov types.Provider) types.ProviderHealthInfo {
	return a.p.GetProviderHealth(prov)
}
func (a *proxyAdapter) SessionLogger() tui.SessionLoggerInterface {
	return a.p.SessionLogger() // *sessions.SessionLogger implements tui.SessionLoggerInterface
}
func (a *proxyAdapter) Shutdown(ctx context.Context) error {
	return a.p.Shutdown(ctx)
}
func (a *proxyAdapter) Config() tui.ProxyConfigInterface {
	return &configAdapter{cfg: a.p.Config()}
}
func (a *proxyAdapter) Bypass() bool           { return a.p.Bypass() }
func (a *proxyAdapter) SetBypass(enabled bool) { a.p.SetBypass(enabled) }

// configAdapter adapts config.Config to tui.ProxyConfigInterface.
type configAdapter struct {
	cfg *config.Config
}

func (ca *configAdapter) GetListenPort() int   { return ca.cfg.Proxy.ListenPort }
func (ca *configAdapter) GetPrefillSpeed() int { return ca.cfg.Usage.EstimatedPrefillSpeed }

// serviceControlAdapter implements tui.ServiceControlInterface by calling daemon package functions
// and spawning subprocesses for hook install/remove.
type serviceControlAdapter struct{}

func (sca *serviceControlAdapter) StartDaemon() error {
	running, existing, _ := daemonIsRunningFn()
	if running {
		return fmt.Errorf("already running (PID %d, port %d)", existing.PID, existing.Port)
	}
	binary, err := osExecutable()
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	if err := startDetachedDaemonFn(binary); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	return nil
}

func (sca *serviceControlAdapter) StopDaemon() error {
	return daemonStopFn()
}

func (sca *serviceControlAdapter) RestartDaemon() error {
	running, _, _ := daemonIsRunningFn()
	if running {
		if err := daemonStopFn(); err != nil {
			return err
		}
	}
	return sca.StartDaemon()
}

func (sca *serviceControlAdapter) InstallService() error {
	binary, err := osExecutable()
	if err != nil {
		return fmt.Errorf("executable: %w", err)
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

func (sca *serviceControlAdapter) InstallHook(target string) error {
	home, err := osUserHomeDir()
	if err != nil {
		return fmt.Errorf("home: %w", err)
	}
	var tpCmd string
	if cfg, err := configLoadFn(); err == nil {
		tpCmd = strings.TrimSpace(cfg.Hooks.SlimferenceCommand)
	}
	switch target {
	case "claude":
		return installClaudeHookFn(home, tpCmd)
	case "codex":
		return installCodexHookFn(home, tpCmd)
	default:
		return fmt.Errorf("unknown hook target: %s", target)
	}
}

func (sca *serviceControlAdapter) RemoveHook(target string) error {
	home, err := osUserHomeDir()
	if err != nil {
		return fmt.Errorf("home: %w", err)
	}
	switch target {
	case "claude":
		return removeClaudeHookFn(home)
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
				return cfg.Proxy.ListenPort, p.Shutdown, nil
			}
		case <-deadline:
			if hasListener(p) {
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
	p.SetLayerEnabled(3, state.Layer3Enabled)
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

func handleStartCmd() {
	running, existing, err := daemonIsRunningFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "check daemon: %v\n", err)
		exitFn(1)
	}
	if running {
		fmt.Fprintf(os.Stderr, "already running (PID %d, port %d)\n", existing.PID, existing.Port)
		exitFn(1)
	}
	// Fork into background via exec of self with "daemon" subcommand.
	binary, err := osExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "executable: %v\n", err)
		exitFn(1)
	}
	if err := startDetachedDaemonFn(binary); err != nil {
		fmt.Fprintf(os.Stderr, "start daemon: %v\n", err)
		exitFn(1)
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
	running, _, _ := daemonIsRunningFn()
	if running {
		if err := daemonStopFn(); err != nil {
			fmt.Fprintf(os.Stderr, "stop: %v\n", err)
			exitFn(1)
		}
	}
	handleStartCmd()
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
		fmt.Fprintln(os.Stderr, "usage: slimference service <install|uninstall|status>")
		exitFn(1)
	}
	switch args[0] {
	case "install":
		binary, err := osExecutable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "executable: %v\n", err)
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
	case "status":
		data, err := daemonFormatStatusFn()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			exitFn(1)
		}
		fmt.Println(string(data))
	default:
		fmt.Fprintf(os.Stderr, "unknown service command: %s\n", args[0])
		exitFn(1)
	}
}

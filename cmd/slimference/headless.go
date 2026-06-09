package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/proxy"
)

// Injected for tests.
var (
	signalNotifyFn    = signal.Notify
	signalStopFn      = signal.Stop
	shutdownTimeoutHL = 30 * time.Second
)

// runHeadless runs the proxy in foreground without the TUI. It traps SIGINT
// and SIGTERM for graceful shutdown. Exit codes:
//
//	0 clean shutdown on signal or normal exit
//	1 config load / proxy boot error
//	2 flag parse error (handled by caller)
//	6 shutdown exceeded shutdownTimeoutHL (T60)
//
// The returned function from startProxyFn IS the proxy's Shutdown method
// (see startProxyFn in main.go). runHeadless treats it accordingly: it waits
// on a signal, then calls that function with a deadline context.
func runHeadless(args []string) {
	cfg, err := configLoadFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		exitFn(1)
		return
	}
	applyTUIFlags(cfg, args)
	setupLogging(cfg)

	slog.Info("slimference headless start",
		"version", version,
		"listen", fmt.Sprintf(":%d", cfg.Proxy.ListenPort),
		"pid", os.Getpid(),
	)

	shutdownFn, err := startProxyFn(cfg)
	if err != nil {
		slog.Error("proxy start failed", "err", err)
		exitFn(1)
		return
	}

	sigCh := make(chan os.Signal, 1)
	signalNotifyFn(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signalStopFn(sigCh)

	hupCh := make(chan os.Signal, 1)
	signalNotifyFn(hupCh, syscall.SIGHUP)
	defer signalStopFn(hupCh)

	var sig os.Signal
hupLoop:
	for {
		select {
		case s := <-sigCh:
			sig = s
			break hupLoop
		case <-hupCh:
			slog.Info("SIGHUP: reloading apps policy + SNIPeekMode")
			reloadAppsManager(startProxyAppsManager)
			reloadSNIPeekModeFromDisk(cfg)
		}
	}
	slog.Info("signal received", "signal", sig.String())

	// Revert /etc/hosts BEFORE shutdown so a clean exit always leaves
	// the system reachable. Order matters: hosts → SNI listener → proxy.
	if startProxyHostsCleanup != nil {
		startProxyHostsCleanup()
	}
	if startProxySNICancel != nil {
		startProxySNICancel()
	}
	if startProxyPIDCleanup != nil {
		startProxyPIDCleanup()
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeoutHL)
	defer cancel()
	if err := shutdownFn(ctx); err != nil {
		if errors.Is(err, proxy.ErrShutdownTimeout) {
			slog.Error("shutdown timeout exceeded",
				"timeout", shutdownTimeoutHL.String(),
			)
			exitFn(6)
			return
		}
		slog.Error("shutdown error", "err", err)
		exitFn(1)
		return
	}
	slog.Info("shutdown clean")
	exitFn(0)
}

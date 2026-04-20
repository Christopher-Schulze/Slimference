package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
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
//	6 shutdown exceeded shutdownTimeoutHL
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

	run, err := startProxyFn(cfg)
	if err != nil {
		slog.Error("proxy start failed", "err", err)
		exitFn(1)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signalNotifyFn(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signalStopFn(sigCh)

	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx) }()

	select {
	case sig := <-sigCh:
		slog.Info("signal received", "signal", sig.String())
		cancel()
	case err := <-errCh:
		if err != nil {
			slog.Error("proxy exited", "err", err)
			exitFn(1)
			return
		}
	}

	shutdownDone := make(chan struct{})
	go func() {
		// Drain the runner; it must return when ctx is cancelled.
		<-errCh
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		slog.Info("shutdown clean")
		exitFn(0)
	case <-time.After(shutdownTimeoutHL):
		slog.Error("shutdown timeout exceeded",
			"timeout", shutdownTimeoutHL.String(),
		)
		exitFn(6)
	}
}

// Helper for tests: exposes a minimal config so tests can assemble a proxy
// without the full startProxyFn plumbing.
var _ = (*config.Config)(nil)
var _ = proxy.Version

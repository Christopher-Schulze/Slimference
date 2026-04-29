package main

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
)

// feedSignalAsync arranges for the next signalNotifyFn call to deliver a
// SIGTERM after the given delay, simulating the user hitting Ctrl+C without
// actually sending a signal to the test process.
func feedSignalAsync(t *testing.T, delay time.Duration) {
	t.Helper()
	origNotify := signalNotifyFn
	origStop := signalStopFn
	signalStopFn = func(c chan<- os.Signal) {} // no-op in tests
	signalNotifyFn = func(c chan<- os.Signal, sig ...os.Signal) {
		go func() {
			time.Sleep(delay)
			c <- syscall.SIGTERM
		}()
	}
	t.Cleanup(func() {
		signalNotifyFn = origNotify
		signalStopFn = origStop
	})
}

// stubRuntimeForHeadless replaces configLoadFn, startProxyFn, setupLoggingFn,
// exitFn with controllable doubles. Returns the captured exit code pointer.
func stubRuntimeForHeadless(t *testing.T, shutdown func(context.Context) error) *int {
	t.Helper()
	code := new(int)
	*code = -1

	origExit := exitFn
	exitFn = func(c int) { *code = c; panic(exitSentinel{}) }
	t.Cleanup(func() { exitFn = origExit })

	origCfg := configLoadFn
	configLoadFn = func() (*config.Config, error) {
		cfg, _, err := config.LoadWithOptions(config.LoadOptions{})
		return cfg, err
	}
	t.Cleanup(func() { configLoadFn = origCfg })

	origStart := startProxyFn
	startProxyFn = func(cfg *config.Config) (func(ctx context.Context) error, error) {
		return shutdown, nil
	}
	t.Cleanup(func() { startProxyFn = origStart })

	return code
}

// exitSentinel is panicked by the stub exitFn so runHeadless unwinds past its
// post-exit code paths without actually terminating the test process.
type exitSentinel struct{}

func recoverExit(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		if _, ok := r.(exitSentinel); ok {
			return
		}
		panic(r)
	}
}

func TestRunHeadless_CleanShutdownExit0(t *testing.T) {
	feedSignalAsync(t, 10*time.Millisecond)
	code := stubRuntimeForHeadless(t, func(ctx context.Context) error { return nil })

	defer recoverExit(t)
	runHeadless(nil)
	if *code != 0 {
		t.Fatalf("exit code = %d, want 0", *code)
	}
}

func TestRunHeadless_TimeoutMapsToExit6(t *testing.T) {
	feedSignalAsync(t, 10*time.Millisecond)
	code := stubRuntimeForHeadless(t, func(ctx context.Context) error {
		return proxy.ErrShutdownTimeout
	})

	defer recoverExit(t)
	runHeadless(nil)
	if *code != 6 {
		t.Fatalf("exit code = %d, want 6", *code)
	}
}

func TestRunHeadless_GenericShutdownErrorExit1(t *testing.T) {
	feedSignalAsync(t, 10*time.Millisecond)
	code := stubRuntimeForHeadless(t, func(ctx context.Context) error {
		return errors.New("disk full")
	})

	defer recoverExit(t)
	runHeadless(nil)
	if *code != 1 {
		t.Fatalf("exit code = %d, want 1", *code)
	}
}

func TestRunHeadless_StartFailureExit1(t *testing.T) {
	code := new(int)
	*code = -1

	origExit := exitFn
	exitFn = func(c int) { *code = c; panic(exitSentinel{}) }
	t.Cleanup(func() { exitFn = origExit })

	origCfg := configLoadFn
	configLoadFn = func() (*config.Config, error) {
		cfg, _, err := config.LoadWithOptions(config.LoadOptions{})
		return cfg, err
	}
	t.Cleanup(func() { configLoadFn = origCfg })

	origStart := startProxyFn
	startProxyFn = func(cfg *config.Config) (func(ctx context.Context) error, error) {
		return nil, errors.New("bind refused")
	}
	t.Cleanup(func() { startProxyFn = origStart })

	defer recoverExit(t)
	runHeadless(nil)
	if *code != 1 {
		t.Fatalf("exit code = %d, want 1", *code)
	}
}

func TestRunHeadless_ConfigLoadFailureExit1(t *testing.T) {
	code := new(int)
	*code = -1

	origExit := exitFn
	exitFn = func(c int) { *code = c; panic(exitSentinel{}) }
	t.Cleanup(func() { exitFn = origExit })

	origCfg := configLoadFn
	configLoadFn = func() (*config.Config, error) {
		return nil, errors.New("bad config")
	}
	t.Cleanup(func() { configLoadFn = origCfg })

	defer recoverExit(t)
	runHeadless(nil)
	if *code != 1 {
		t.Fatalf("exit code = %d, want 1", *code)
	}
}

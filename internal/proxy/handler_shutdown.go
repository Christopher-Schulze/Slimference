package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/readcache"
)

// ErrShutdownTimeout is returned by Shutdown when ctx was cancelled before
// every worker goroutine finished. When this happens a goroutine pprof dump
// is written to ~/.slimference/shutdown-hang-<ts>.pprof (best-effort) and
// callers may translate the error to a dedicated process exit code.
var ErrShutdownTimeout = errors.New("shutdown timeout exceeded")

// shutdownDumpWriterFn is overridden in tests to capture the pprof dump
// without touching the user filesystem.
var shutdownDumpWriterFn = defaultShutdownDumpWriter

// applyDrainTimeout wraps ctx with a deadline drawn from
// `[proxy] drain_timeout_seconds` when the caller's context has no
// deadline. Returns the (possibly wrapped) context and a cancel func.
// T85: turns the operator-config drain knob into a hard ceiling so a
// hung request cannot block exit indefinitely even when the caller
// passed context.Background().
func (p *Proxy) applyDrainTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.config == nil || p.config.Proxy.DrainTimeoutSeconds <= 0 {
		return ctx, func() {}
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(p.config.Proxy.DrainTimeoutSeconds)*time.Second)
}

// Shutdown performs a graceful shutdown of the proxy. Safe to call multiple
// times - only the first call does work; subsequent calls return nil. On
// timeout Shutdown returns ErrShutdownTimeout so process-level callers can
// translate the outcome into a distinct exit code (T60).
// Nil ctx is tolerated and replaced with context.Background so operator-
// scripts that call Shutdown(nil) never crash. T85 caps no-deadline calls
// at `[proxy] drain_timeout_seconds` when set so a stuck connection
// cannot block exit indefinitely.
func (p *Proxy) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	wrapped, cancel := p.applyDrainTimeout(ctx)
	defer cancel()
	var result error
	p.shutdownOnce.Do(func() {
		result = p.doShutdown(wrapped)
	})
	return result
}

func (p *Proxy) doShutdown(ctx context.Context) error {
	slog.Info("proxy shutdown initiated")
	if p.workerCancel != nil {
		p.workerCancel()
	}

	// server may be nil when Shutdown is called on a freshly New'd Proxy
	// that never Start()ed. Tolerate that so unit tests and the integrate
	// adapter-smoke path do not panic.
	if p.server != nil {
		if err := p.server.Shutdown(ctx); err != nil {
			slog.Warn("server shutdown error", "error", err)
		}
	}

	if p.shutdownCh != nil {
		close(p.shutdownCh)
	}

	workersDone := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(workersDone)
	}()

	var result error
	select {
	case <-workersDone:
		slog.Info("all workers stopped")
	case <-ctx.Done():
		dumpPath, dumpErr := shutdownDumpWriterFn()
		slog.Warn("shutdown timeout, some workers may still be running",
			"goroutines", runtime.NumGoroutine(),
			"dump_path", dumpPath,
			"dump_err", dumpErr,
		)
		result = ErrShutdownTimeout
	}

	// Final analytics flush.
	if p.persister != nil {
		snap := p.analytics.Snapshot()
		if err := p.persister.WriteSnapshot(snap); err != nil {
			slog.Warn("final analytics flush failed", "error", err)
		}
		p.persister.Close()
	}
	if home, err := os.UserHomeDir(); err == nil {
		if err := readcache.FlushDir(readcache.DefaultDir(home)); err != nil {
			slog.Warn("final readcache flush failed", "error", err)
		}
	}

	if p.fileWatcher != nil {
		p.fileWatcher.Close()
	}
	if p.debugRecorder != nil {
		p.debugRecorder.Close()
	}
	return result
}

// defaultShutdownDumpWriter writes a goroutine pprof dump to a stable path
// under the user's state dir. Best-effort: any filesystem error is reported
// back and logged by the caller, never propagated as a shutdown failure.
func defaultShutdownDumpWriter() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".slimference")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("shutdown-hang-%s.pprof",
		time.Now().UTC().Format("20060102T150405"))
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_ = pprof.Lookup("goroutine").WriteTo(f, 1)
	return path, nil
}

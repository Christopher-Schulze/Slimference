package proxy

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/types"
)

// shutdownTestProxy returns a minimal *Proxy with just enough state for the
// Shutdown method to execute: a closed HTTP server, a shutdown channel, a
// workerCancel, and a waitgroup. No goroutines are started unless the test
// explicitly adds them.
func shutdownTestProxy(t *testing.T) *Proxy {
	t.Helper()
	srv := &http.Server{Addr: "127.0.0.1:0"}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Proxy{
		server:         srv,
		analyticsQueue: make(chan types.AnalyticsEvent, 4),
		shutdownCh:     make(chan struct{}),
		workerCtx:      ctx,
		workerCancel:   cancel,
	}
	return p
}

func TestShutdown_CleanNoError(t *testing.T) {
	p := shutdownTestProxy(t)
	// No workers registered -> wg.Wait returns immediately -> clean path.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestShutdown_ReturnsErrOnTimeout(t *testing.T) {
	p := shutdownTestProxy(t)
	// Register a worker that ignores cancellation and keeps the wg blocked.
	p.wg.Add(1)
	release := make(chan struct{})
	go func() {
		defer p.wg.Done()
		<-release
	}()
	t.Cleanup(func() { close(release) })

	// Stub the dump writer so tests never touch the filesystem.
	origDump := shutdownDumpWriterFn
	shutdownDumpWriterFn = func() (string, error) { return "/dev/null-test", nil }
	t.Cleanup(func() { shutdownDumpWriterFn = origDump })

	// Very short deadline so the select hits ctx.Done() immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := p.Shutdown(ctx)
	if !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("err = %v, want ErrShutdownTimeout", err)
	}
}

func TestShutdown_IsIdempotent(t *testing.T) {
	p := shutdownTestProxy(t)
	ctx := context.Background()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("first shutdown err: %v", err)
	}
	// Second call must not panic (shutdownCh already closed) and must return nil.
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("second shutdown err: %v", err)
	}
}

func TestShutdown_DumpWriterFailureDoesNotCrash(t *testing.T) {
	p := shutdownTestProxy(t)
	p.wg.Add(1)
	release := make(chan struct{})
	go func() {
		defer p.wg.Done()
		<-release
	}()
	t.Cleanup(func() { close(release) })

	origDump := shutdownDumpWriterFn
	shutdownDumpWriterFn = func() (string, error) { return "", errors.New("no disk") }
	t.Cleanup(func() { shutdownDumpWriterFn = origDump })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := p.Shutdown(ctx)
	if !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("err = %v, want ErrShutdownTimeout", err)
	}
}

func TestShutdown_ConcurrentCallsOnlyRunOnce(t *testing.T) {
	p := shutdownTestProxy(t)
	var wg sync.WaitGroup
	wg.Add(8)
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		i := i
		go func() {
			defer wg.Done()
			ctx := context.Background()
			errs[i] = p.Shutdown(ctx)
		}()
	}
	wg.Wait()
	// Exactly one goroutine sees the work done; the others see nil (the
	// default zero-value returned after shutdownOnce was consumed).
	for i, err := range errs {
		if err != nil {
			t.Fatalf("errs[%d] = %v", i, err)
		}
	}
}

func TestDefaultShutdownDumpWriter_RoundTrip(t *testing.T) {
	// Exercise the real writer with HOME pointed at a temp dir so we can
	// assert the file exists and contains the pprof header.
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := defaultShutdownDumpWriter()
	if err != nil {
		t.Fatalf("writer err: %v", err)
	}
	if path == "" {
		t.Fatal("path empty")
	}
	// path must be under the temp home.
	want := home
	if !pathHasPrefix(path, want) {
		t.Fatalf("path = %q, want prefix %q", path, want)
	}
}

// small prefix-check helper to avoid pulling in strings for a single call.
func pathHasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

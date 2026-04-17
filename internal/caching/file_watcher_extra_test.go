package caching

import (
	"testing"
	"time"
)

func TestFileWatcher_run_eventsChannelClose(t *testing.T) {
	w := newMockFsWatcher()
	fw := newFileWatcherWithWatcher(w, func(string) {
		t.Fatal("onChange should not run when events channel closes")
	})
	_ = fw
	if err := w.Close(); err != nil {
		t.Fatalf("mock watcher close: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
}

func TestFileWatcher_run_pruneTicker(t *testing.T) {
	origTicker := newTickerFn
	newTickerFn = func(time.Duration) *time.Ticker {
		return time.NewTicker(5 * time.Millisecond)
	}
	defer func() {
		newTickerFn = origTicker
	}()

	w := newMockFsWatcher()
	fw := newFileWatcherWithWatcher(w, func(string) {})
	fw.mu.Lock()
	fw.trackedDirs["/tmp/stale"] = time.Now().Add(-pruneMaxAge - time.Second)
	fw.mu.Unlock()

	time.Sleep(20 * time.Millisecond)

	fw.mu.RLock()
	_, ok := fw.trackedDirs["/tmp/stale"]
	fw.mu.RUnlock()
	if ok {
		t.Fatal("expected prune ticker to remove stale directory")
	}
	_ = w.Close()
}

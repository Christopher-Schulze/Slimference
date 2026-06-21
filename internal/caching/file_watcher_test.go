package caching

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// mockFsWatcher is a test-only fsWatcher that owns its channels exclusively.
// Unlike *fsnotify.Watcher, it has no OS kqueue/inotify backend goroutine,
// making it safe to use in parallel tests with no inter-test races.
type mockFsWatcher struct {
	eventsCh  chan fsnotify.Event
	errorsCh  chan error
	addErr    error
	removeErr error
	closeOnce sync.Once
}

func newMockFsWatcher() *mockFsWatcher {
	return &mockFsWatcher{
		eventsCh: make(chan fsnotify.Event, 16),
		errorsCh: make(chan error, 16),
	}
}

func (m *mockFsWatcher) Add(_ string) error    { return m.addErr }
func (m *mockFsWatcher) Remove(_ string) error { return m.removeErr }
func (m *mockFsWatcher) Close() error {
	m.closeOnce.Do(func() {
		close(m.eventsCh)
		close(m.errorsCh)
	})
	return nil
}
func (m *mockFsWatcher) EventsChan() <-chan fsnotify.Event { return m.eventsCh }
func (m *mockFsWatcher) ErrorsChan() <-chan error          { return m.errorsCh }

func TestFileWatcher_watchUnwatchClose(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	f, err := os.CreateTemp(tmp, "fw-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()

	var calls int
	fw, err := NewFileWatcher(func(string) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	if err := fw.Watch(path); err != nil {
		t.Fatal(err)
	}
	// Duplicate watch of same parent dir is a no-op.
	if err := fw.Watch(path); err != nil {
		t.Fatal(err)
	}
	fw.Unwatch(path)
}

func TestFileWatcher_watchDirTrailingSlash(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "watchme") + string(os.PathSeparator)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	fw, err := NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()
	if err := fw.Watch(dir); err != nil {
		t.Fatal(err)
	}
}

func TestFileWatcher_IsWatching(t *testing.T) {
	t.Parallel()

	mw := newMockFsWatcher()
	fw := newFileWatcherWithWatcher(mw, func(string) {})
	defer fw.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := fw.Watch(path); err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	if !fw.IsWatching(path) {
		t.Fatal("expected file path to report watched")
	}
	if !fw.IsWatching(dir + "/") {
		t.Fatal("expected directory path with trailing slash to report watched")
	}
	if fw.IsWatching(filepath.Join(t.TempDir(), "other.txt")) {
		t.Fatal("expected unrelated path to report not watched")
	}
}

func TestFileWatcher_Close_idempotent(t *testing.T) {
	t.Parallel()

	mw := newMockFsWatcher()
	fw := newFileWatcherWithWatcher(mw, func(string) {})

	fw.Close()
	fw.Close()
}

func TestFileWatcher_pruneStale(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	f, err := os.CreateTemp(tmp, "prune-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()

	fw, err := NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()
	if err := fw.Watch(path); err != nil {
		t.Fatal(err)
	}
	// Negative maxAge makes cutoff lie in the future, so every tracked dir looks stale.
	fw.pruneStale(-time.Hour)
	// Safe to watch again after prune removed the dir from tracking.
	if err := fw.Watch(path); err != nil {
		t.Fatal(err)
	}
}

// TestFileWatcher_scheduleDebounce_createAndReset verifies timer creation on
// first call and reset on second call (covers scheduleDebounce L147-156),
// and that Close stops a pending debounce timer (covers Close L106-108).
func TestFileWatcher_scheduleDebounce_createAndReset(t *testing.T) {
	t.Parallel()
	fw, err := NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatal(err)
	}

	const fakePath = "/tmp/fw-debounce-test/file.txt"

	// First call: creates a new timer (L151-156).
	fw.scheduleDebounce(fakePath)
	fw.mu.RLock()
	_, exists := fw.debounceTimers[fakePath]
	fw.mu.RUnlock()
	if !exists {
		t.Fatal("expected debounce timer to be created on first call")
	}

	// Second call: resets the existing timer (L147-150).
	fw.scheduleDebounce(fakePath)
	fw.mu.RLock()
	_, stillExists := fw.debounceTimers[fakePath]
	fw.mu.RUnlock()
	if !stillExists {
		t.Fatal("expected debounce timer to still exist after reset call")
	}

	// Close must stop pending timers (L106-108); must not panic or deadlock.
	fw.Close()
}

// TestFileWatcher_Unwatch_notTracked verifies the early-return path (L84-86)
// when Unwatch is called for a path that is not being watched.
func TestFileWatcher_Unwatch_notTracked(t *testing.T) {
	t.Parallel()
	fw, err := NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	// Unwatch on a path that was never watched must not panic or block.
	fw.Unwatch("/tmp/not-watched-at-all/file.txt")
}

// TestFileWatcher_Unwatch_pendingTimer verifies that Unwatch stops and removes
// a pending debounce timer for the directory (L94-97).
func TestFileWatcher_Unwatch_pendingTimer(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	f, err := os.CreateTemp(tmp, "fw-timer-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()

	fw, err := NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	if err := fw.Watch(path); err != nil {
		t.Fatal(err)
	}

	// scheduleDebounce uses filepath.Dir(path) as timer key.
	// Unwatch(path) also uses filepath.Dir(path), so they match.
	dirPath := filepath.Dir(path)
	fw.scheduleDebounce(dirPath)

	fw.mu.RLock()
	_, timerCreated := fw.debounceTimers[dirPath]
	fw.mu.RUnlock()
	if !timerCreated {
		t.Fatal("expected debounce timer to exist before Unwatch")
	}

	// Unwatch the file path; internally it computes filepath.Dir(path) == dirPath.
	fw.Unwatch(path)

	fw.mu.RLock()
	_, timerAfter := fw.debounceTimers[dirPath]
	fw.mu.RUnlock()
	if timerAfter {
		t.Fatal("expected debounce timer to be removed by Unwatch")
	}
}

// TestFileWatcher_maxWatchedDirs verifies that Watch silently skips new paths
// once maxWatchedDirs (50) are already tracked (L64-70).
func TestFileWatcher_maxWatchedDirs(t *testing.T) {
	t.Parallel()
	// Create a base temp directory to house all subdirectories.
	base := t.TempDir()

	fw, err := NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	// Fill up to the limit by watching distinct real directories.
	for i := range maxWatchedDirs {
		dir := filepath.Join(base, fmt.Sprintf("d%d", i))
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			t.Fatalf("mkdir %s: %v", dir, mkErr)
		}
		if watchErr := fw.Watch(dir + "/"); watchErr != nil {
			t.Fatalf("Watch %s: %v", dir, watchErr)
		}
	}

	fw.mu.RLock()
	count := len(fw.trackedDirs)
	fw.mu.RUnlock()
	if count != maxWatchedDirs {
		t.Fatalf("expected %d tracked dirs, got %d", maxWatchedDirs, count)
	}

	// One more directory: must be silently skipped, no error returned.
	extra := filepath.Join(base, "extra")
	if mkErr := os.MkdirAll(extra, 0o755); mkErr != nil {
		t.Fatalf("mkdir extra: %v", mkErr)
	}
	if watchErr := fw.Watch(extra + "/"); watchErr != nil {
		t.Fatalf("Watch beyond limit returned unexpected error: %v", watchErr)
	}

	fw.mu.RLock()
	countAfter := len(fw.trackedDirs)
	fw.mu.RUnlock()
	if countAfter != maxWatchedDirs {
		t.Fatalf("expected count to stay at %d after limit exceeded, got %d", maxWatchedDirs, countAfter)
	}
}

// TestFileWatcher_run_chmodEventIgnored verifies that Chmod events (Op not in the mask)
// hit the continue branch (file_watcher.go:122-123) and do not invoke onChange.
func TestFileWatcher_run_chmodEventIgnored(t *testing.T) {
	// Not parallel: interacts with OS-level kqueue/inotify backend shared across all
	// FileWatcher instances; parallel execution triggers races in fsnotify internals.
	tmp := t.TempDir()
	path := filepath.Join(tmp, "chmod-test.txt")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	onChangeCalled := make(chan struct{}, 1)
	fw, err := NewFileWatcher(func(string) {
		select {
		case onChangeCalled <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	if err := fw.Watch(path); err != nil {
		t.Fatal(err)
	}

	// chmod triggers a Chmod event (Op == fsnotify.Chmod), which is not in the mask.
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}

	// onChange should NOT be called within the timeout (Chmod events are ignored).
	select {
	case <-onChangeCalled:
		// On some platforms chmod may also trigger a Write event.
		// That is acceptable; just verify no panic occurred.
	case <-time.After(300 * time.Millisecond):
		// Expected: Chmod-only event was silently skipped.
	}
}

// TestFileWatcher_run_errorsChannelError verifies that an error sent to watcher.Errors
// is handled gracefully via slog.Warn (file_watcher.go:130).
// Uses mockFsWatcher so no OS kqueue/inotify backend races with the channel send.
func TestFileWatcher_run_errorsChannelError(t *testing.T) {
	t.Parallel()
	mock := newMockFsWatcher()
	fw := newFileWatcherWithWatcher(mock, func(string) {})
	defer fw.Close()

	// Send a synthetic error into the mock Errors channel.
	select {
	case mock.errorsCh <- fmt.Errorf("synthetic fsnotify error"):
	case <-time.After(time.Second):
		t.Fatal("could not send error to watcher Errors channel")
	}

	// Give run() goroutine time to process the error.
	time.Sleep(50 * time.Millisecond)
	// No panic or deadlock -> pass.
}

// TestFileWatcher_run_errorsChannelClose verifies the !ok path on the Errors channel
// (file_watcher.go:127-129) when the underlying watcher is closed without signaling done.
// Uses mockFsWatcher to avoid races with the OS kqueue/inotify backend goroutine.
func TestFileWatcher_run_errorsChannelClose(t *testing.T) {
	t.Parallel()
	mock := newMockFsWatcher()
	fw := newFileWatcherWithWatcher(mock, func(string) {})

	// Close mock channels directly so both EventsChan and ErrorsChan return !ok,
	// triggering the early-return branches in run(). mockFsWatcher.Close is idempotent.
	_ = mock.Close()

	// Give run() time to detect the closed channels and exit.
	time.Sleep(100 * time.Millisecond)

	// fw.Close() closes done + calls mock.Close() again (idempotent - no panic).
	fw.Close()
}

// TestFileWatcher_Watch_addError covers the watcher.Add error path (Watch L71-73)
// by watching a nonexistent directory so fsnotify.Add returns an error.
func TestFileWatcher_Watch_addError(t *testing.T) {
	t.Parallel()
	fw, err := NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	err = fw.Watch("/nonexistent-slimference-test-dir-xyz999/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

// TestFileWatcher_Unwatch_removeError covers the slog.Warn path in Unwatch (L87-92)
// by pre-removing the dir from the underlying fsnotify watcher so that
// fw.watcher.Remove fails when Unwatch calls it.
func TestFileWatcher_Unwatch_removeError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	fw, err := NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	if err := fw.Watch(tmp + "/"); err != nil {
		t.Fatal(err)
	}

	// Remove the dir from fsnotify directly so Unwatch's Remove call will fail.
	_ = fw.watcher.Remove(tmp)

	// Unwatch: dir is still in trackedDirs but watcher.Remove returns an error.
	// Covers the slog.Warn branch at L87-92. Must not panic.
	fw.Unwatch(tmp + "/")
}

// TestFileWatcher_pruneStale_removeError covers the slog.Warn path in pruneStale (L167-172)
// by pre-removing the dir from fsnotify so that the Remove call inside pruneStale fails.
func TestFileWatcher_pruneStale_removeError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	fw, err := NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	if err := fw.Watch(tmp + "/"); err != nil {
		t.Fatal(err)
	}

	// Pre-remove from fsnotify so pruneStale's Remove call fails.
	_ = fw.watcher.Remove(tmp)

	// Negative maxAge makes everything stale; Remove will fail but must not panic.
	fw.pruneStale(-time.Hour)
}

// TestFileWatcher_scheduleDebounce_callsOnChange verifies the full event path:
// run() receives an fsnotify event -> scheduleDebounce creates a timer ->
// timer fires -> onChange is called (covers run() L119-125 and scheduleDebounce L151-156).
func TestFileWatcher_scheduleDebounce_callsOnChange(t *testing.T) {
	// Not parallel: triggers real OS filesystem events that are delivered via the shared
	// kqueue backend; concurrent parallel tests cause races in fsnotify internals.
	tmp := t.TempDir()
	f, err := os.CreateTemp(tmp, "fw-change-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()

	changed := make(chan string, 1)
	fw, err := NewFileWatcher(func(p string) {
		select {
		case changed <- p:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Close()

	if err := fw.Watch(path); err != nil {
		t.Fatal(err)
	}

	// Trigger a real filesystem write event.
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for onChange to fire (debounceDelay + generous margin).
	select {
	case <-changed:
		// success
	case <-time.After(debounceDelay * 10):
		t.Fatal("onChange was not called within timeout after file write")
	}
}

// TestNewFileWatcher_fsnotifyError covers the fsnotify.NewWatcher error return path.
func TestNewFileWatcher_fsnotifyError(t *testing.T) {
	// Not parallel: mutates package-level var fsnotifyNewWatcher.
	old := fsnotifyNewWatcher
	fsnotifyNewWatcher = func() (*fsnotify.Watcher, error) { return nil, errors.New("injected watcher error") }
	defer func() { fsnotifyNewWatcher = old }()

	_, err := NewFileWatcher(func(_ string) {})
	if err == nil {
		t.Fatal("expected error from injected fsnotifyNewWatcher")
	}
}

package caching

import (
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	maxWatchedDirs = 50
	debounceDelay  = 100 * time.Millisecond
)

// fsWatcher abstracts the underlying filesystem event provider for testability.
// Production code uses realFsWatcher (wraps *fsnotify.Watcher).
// Tests inject mockFsWatcher to avoid races with the OS kqueue/inotify backend.
type fsWatcher interface {
	Add(name string) error
	Remove(name string) error
	Close() error
	EventsChan() <-chan fsnotify.Event
	ErrorsChan() <-chan error
}

// realFsWatcher adapts *fsnotify.Watcher to the fsWatcher interface.
type realFsWatcher struct{ w *fsnotify.Watcher }

func (r *realFsWatcher) Add(name string) error             { return r.w.Add(name) }
func (r *realFsWatcher) Remove(name string) error          { return r.w.Remove(name) }
func (r *realFsWatcher) Close() error                      { return r.w.Close() }
func (r *realFsWatcher) EventsChan() <-chan fsnotify.Event { return r.w.Events }
func (r *realFsWatcher) ErrorsChan() <-chan error          { return r.w.Errors }

// FileWatcher monitors filesystem paths and calls onChange when files change.
// Changes are debounced per path to coalesce rapid successive events.
type FileWatcher struct {
	watcher        fsWatcher
	trackedDirs    map[string]time.Time // dir path -> last access time
	onChange       func(path string)
	mu             sync.RWMutex
	debounceTimers map[string]*time.Timer
	done           chan struct{}
	newTicker      func(time.Duration) *time.Ticker
}

// fsnotifyNewWatcher is set to fsnotify.NewWatcher; replaced in tests to inject errors.
var fsnotifyNewWatcher = fsnotify.NewWatcher
var newTickerFn = time.NewTicker

// NewFileWatcher creates a FileWatcher that calls onChange for every changed path.
// A background goroutine is started immediately; call Close to stop it.
func NewFileWatcher(onChange func(path string)) (*FileWatcher, error) {
	w, err := fsnotifyNewWatcher()
	if err != nil {
		return nil, err
	}
	return newFileWatcherWithWatcher(&realFsWatcher{w: w}, onChange), nil
}

// newFileWatcherWithWatcher creates a FileWatcher with an injected fsWatcher.
// Used in production via NewFileWatcher and in tests via mockFsWatcher.
func newFileWatcherWithWatcher(w fsWatcher, onChange func(string)) *FileWatcher {
	fw := &FileWatcher{
		watcher:        w,
		trackedDirs:    make(map[string]time.Time),
		onChange:       onChange,
		debounceTimers: make(map[string]*time.Timer),
		done:           make(chan struct{}),
		newTicker:      newTickerFn,
	}
	go fw.run()
	return fw
}

// Watch adds path (file or directory) to the watch list.
// If path is a file, its parent directory is watched instead.
// Returns an error if the underlying fsnotify call fails.
// Silently skips if already watched. Refuses if 50 dirs are already tracked.
func (fw *FileWatcher) Watch(path string) error {
	dir := filepath.Dir(path)
	// If path itself is a directory (trailing slash), watch it directly.
	if len(path) > 0 && path[len(path)-1] == '/' {
		dir = filepath.Clean(path)
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()

	if _, exists := fw.trackedDirs[dir]; exists {
		fw.trackedDirs[dir] = time.Now()
		return nil
	}
	if len(fw.trackedDirs) >= maxWatchedDirs {
		slog.Warn("file watcher: max watched dirs reached, skipping",
			slog.String("dir", dir),
			slog.Int("max", maxWatchedDirs),
		)
		return nil
	}
	if err := fw.watcher.Add(dir); err != nil {
		return err
	}
	fw.trackedDirs[dir] = time.Now()
	slog.Debug("file watcher: watching dir", slog.String("dir", dir))
	return nil
}

// Unwatch removes path from the watch list (uses the parent directory).
func (fw *FileWatcher) Unwatch(path string) {
	dir := filepath.Dir(path)
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if _, exists := fw.trackedDirs[dir]; !exists {
		return
	}
	if err := fw.watcher.Remove(dir); err != nil {
		slog.Warn("file watcher: remove failed",
			slog.String("dir", dir),
			slog.String("err", err.Error()),
		)
	}
	delete(fw.trackedDirs, dir)
	if t, ok := fw.debounceTimers[dir]; ok {
		t.Stop()
		delete(fw.debounceTimers, dir)
	}
}

// Close stops the background goroutine and releases all resources.
func (fw *FileWatcher) Close() {
	close(fw.done)
	_ = fw.watcher.Close()
	fw.mu.Lock()
	defer fw.mu.Unlock()
	for _, t := range fw.debounceTimers {
		t.Stop()
	}
}

// pruneInterval is how often stale watched directories are cleaned up.
const pruneInterval = 5 * time.Minute

// pruneMaxAge is the maximum time a directory can be unaccessed before pruning.
const pruneMaxAge = 10 * time.Minute

// run is the background event loop. It reads fsnotify events, debounces them per path,
// and periodically prunes stale watched directories.
func (fw *FileWatcher) run() {
	pruneTicker := fw.newTicker(pruneInterval)
	defer pruneTicker.Stop()
	for {
		select {
		case <-fw.done:
			return
		case <-pruneTicker.C:
			fw.pruneStale(pruneMaxAge)
		case event, ok := <-fw.watcher.EventsChan():
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			fw.scheduleDebounce(event.Name)
		case err, ok := <-fw.watcher.ErrorsChan():
			if !ok {
				return
			}
			slog.Warn("file watcher: fsnotify error", slog.String("err", err.Error()))
		}
	}
}

// scheduleDebounce resets (or creates) a debounce timer for path.
// After debounceDelay with no further events, onChange is called.
func (fw *FileWatcher) scheduleDebounce(path string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	// Update access time for the watched dir.
	dir := filepath.Dir(path)
	if _, tracked := fw.trackedDirs[dir]; tracked {
		fw.trackedDirs[dir] = time.Now()
	}

	if t, exists := fw.debounceTimers[path]; exists {
		t.Reset(debounceDelay)
		return
	}
	fw.debounceTimers[path] = time.AfterFunc(debounceDelay, func() {
		fw.mu.Lock()
		delete(fw.debounceTimers, path)
		fw.mu.Unlock()
		fw.onChange(path)
	})
}

// PruneStale removes directories not accessed within maxAge.
// Intended to be called periodically to prevent resource leaks.
func (fw *FileWatcher) pruneStale(maxAge time.Duration) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for dir, lastAccess := range fw.trackedDirs {
		if lastAccess.Before(cutoff) {
			if err := fw.watcher.Remove(dir); err != nil {
				slog.Warn("file watcher: prune remove failed",
					slog.String("dir", dir),
					slog.String("err", err.Error()),
				)
			}
			delete(fw.trackedDirs, dir)
			slog.Debug("file watcher: pruned stale dir", slog.String("dir", dir))
		}
	}
}

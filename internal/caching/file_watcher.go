package caching

import (
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	maxWatchedDirs  = 50
	debounceDelay   = 100 * time.Millisecond
)

// FileWatcher monitors filesystem paths and calls onChange when files change.
// Changes are debounced per path to coalesce rapid successive events.
type FileWatcher struct {
	watcher        *fsnotify.Watcher
	trackedDirs    map[string]time.Time // dir path -> last access time
	onChange       func(path string)
	mu             sync.RWMutex
	debounceTimers map[string]*time.Timer
	done           chan struct{}
}

// fsnotifyNewWatcher is set to fsnotify.NewWatcher; replaced in tests to inject errors.
var fsnotifyNewWatcher = fsnotify.NewWatcher

// NewFileWatcher creates a FileWatcher that calls onChange for every changed path.
// A background goroutine is started immediately; call Close to stop it.
func NewFileWatcher(onChange func(path string)) (*FileWatcher, error) {
	w, err := fsnotifyNewWatcher()
	if err != nil {
		return nil, err
	}
	fw := &FileWatcher{
		watcher:        w,
		trackedDirs:    make(map[string]time.Time),
		onChange:       onChange,
		debounceTimers: make(map[string]*time.Timer),
		done:           make(chan struct{}),
	}
	go fw.run()
	return fw, nil
}

// Watch adds path (file or directory) to the watch list.
// If path is a file, its parent directory is watched instead.
// Returns an error if the underlying fsnotify call fails.
// Silently skips if already watched. Refuses if 50 dirs are already tracked.
func (fw *FileWatcher) Watch(path string) error {
	dir := filepath.Dir(path)
	// If path itself is a directory, watch it directly.
	if path[len(path)-1] == '/' {
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

// run is the background event loop. It reads fsnotify events and debounces them
// per path before forwarding to onChange.
func (fw *FileWatcher) run() {
	for {
		select {
		case <-fw.done:
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			fw.scheduleDebounce(event.Name)
		case err, ok := <-fw.watcher.Errors:
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

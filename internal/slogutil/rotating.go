// Package slogutil provides a size-based rotating file writer for use with slog handlers.
package slogutil

import (
	"fmt"
	"os"
	"sync"
)

const (
	defaultMaxBytes int64 = 10 * 1024 * 1024 // 10 MB per file
	defaultMaxFiles int   = 5                 // keep 5 rotated copies
)

// RotatingWriter is an io.Writer backed by a file that rotates when it exceeds
// maxBytes. Up to maxFiles rotated copies are kept (path.1, path.2, ...).
// All methods are goroutine-safe.
type RotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	maxFiles int
	file     *os.File
	size     int64
}

// New opens (or creates) path and returns a RotatingWriter ready for writing.
// Pass 0 for maxBytes or maxFiles to use the package defaults (10 MB / 5 files).
func New(path string, maxBytes int64, maxFiles int) (*RotatingWriter, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if maxFiles <= 0 {
		maxFiles = defaultMaxFiles
	}
	rw := &RotatingWriter{path: path, maxBytes: maxBytes, maxFiles: maxFiles}
	if err := rw.openOrCreate(); err != nil {
		return nil, err
	}
	return rw, nil
}

// Write implements io.Writer. Rotates the file if adding p would exceed maxBytes.
func (rw *RotatingWriter) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.size+int64(len(p)) > rw.maxBytes {
		// Best-effort rotation; on failure keep writing to the current file.
		_ = rw.rotate()
	}
	n, err := rw.file.Write(p)
	rw.size += int64(n)
	return n, err
}

// Close closes the underlying file. Subsequent writes will fail.
func (rw *RotatingWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.file != nil {
		err := rw.file.Close()
		rw.file = nil
		return err
	}
	return nil
}

// openOrCreate opens path for append-writing, creating it if absent.
func (rw *RotatingWriter) openOrCreate() error {
	f, err := os.OpenFile(rw.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("slogutil: open %s: %w", rw.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("slogutil: stat %s: %w", rw.path, err)
	}
	rw.file = f
	rw.size = info.Size()
	return nil
}

// rotate closes the current file, shifts .1-.N back by one (dropping the oldest),
// renames the current file to .1, then opens a fresh file at path.
func (rw *RotatingWriter) rotate() error {
	if rw.file != nil {
		_ = rw.file.Close()
		rw.file = nil
	}
	// Shift: path.5 deleted implicitly by rename, path.4 -> path.5, ..., path.1 -> path.2.
	for i := rw.maxFiles - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", rw.path, i)
		dst := fmt.Sprintf("%s.%d", rw.path, i+1)
		_ = os.Rename(src, dst) // file may not exist - that is fine
	}
	_ = os.Rename(rw.path, rw.path+".1")
	return rw.openOrCreate()
}

package slogutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriter_BasicWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	rw, err := New(path, 0, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rw.Close()

	msg := []byte(`{"level":"INFO","msg":"hello"}` + "\n")
	n, err := rw.Write(msg)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(msg) {
		t.Fatalf("Write: wrote %d, want %d", n, len(msg))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != string(msg) {
		t.Fatalf("file content: got %q, want %q", data, msg)
	}
}

func TestRotatingWriter_Rotation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	// maxBytes = 50: first batch fits, second triggers rotation.
	rw, err := New(path, 50, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rw.Close()

	line := strings.Repeat("x", 30) + "\n" // 31 bytes

	// First write: 31 bytes, fits under 50.
	if _, err := rw.Write([]byte(line)); err != nil {
		t.Fatalf("write1: %v", err)
	}
	// Second write: 31+31=62 > 50, triggers rotation.
	if _, err := rw.Write([]byte(line)); err != nil {
		t.Fatalf("write2: %v", err)
	}

	// After rotation: path is the fresh file, path.1 is the old one.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("fresh file missing after rotation: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("rotated file .1 missing: %v", err)
	}
}

func TestRotatingWriter_MaxFilesOldestDropped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	// maxFiles=2: path, path.1, path.2 allowed; path.3 is beyond the limit.
	rw, err := New(path, 30, 2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rw.Close()

	line := strings.Repeat("a", 31) + "\n" // guaranteed to trigger rotation each write

	for i := range 4 {
		if _, err := rw.Write([]byte(line)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// path.3 must not exist (maxFiles=2 means only .1 and .2 are kept).
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Error("path.3 should not exist with maxFiles=2")
	}
}

func TestRotatingWriter_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "conc.jsonl")

	rw, err := New(path, 0, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rw.Close()

	done := make(chan struct{})
	for i := range 10 {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			line := fmt.Appendf(nil, `{"id":%d}`+"\n", id)
			if _, err := rw.Write(line); err != nil {
				t.Errorf("goroutine %d write: %v", id, err)
			}
		}(i)
	}
	for range 10 {
		<-done
	}
}

func TestNew_InvalidPath(t *testing.T) {
	t.Parallel()
	// Directory that cannot exist as a file path.
	_, err := New("/no/such/dir/test.jsonl", 0, 0)
	if err == nil {
		t.Fatal("want error for non-existent parent directory")
	}
}

func TestRotatingWriter_Close(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rw, err := New(filepath.Join(dir, "x.jsonl"), 0, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := rw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Double-close must not panic or error.
	if err := rw.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

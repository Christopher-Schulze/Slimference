package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// mu serializes tests that modify expandHomeFn (package-level var).
var mu sync.Mutex
var testSeq int

func withTempDir(t *testing.T, fn func()) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	testSeq++
	base := filepath.Join(os.TempDir(), "slm")
	os.MkdirAll(base, 0755)
	tmp := filepath.Join(base, fmt.Sprintf("d%d_%d", os.Getpid(), testSeq))
	os.RemoveAll(tmp)
	os.MkdirAll(tmp, 0755)
	t.Cleanup(func() { os.RemoveAll(tmp) })

	origFn := expandHomeFn
	expandHomeFn = func(path string) string {
		return filepath.Join(tmp, ".slm")
	}
	defer func() { expandHomeFn = origFn }()
	fn()
}

func TestTryAcquireLock_success(t *testing.T) {
	withTempDir(t, func() {
		closer, err := TryAcquireLock()
		if err != nil {
			t.Fatalf("first acquire should succeed: %v", err)
		}
		closer()
	})
}

func TestTryAcquireLock_doubleLock(t *testing.T) {
	withTempDir(t, func() {
		closer, err := TryAcquireLock()
		if err != nil {
			t.Fatalf("first acquire should succeed: %v", err)
		}
		defer closer()

		_, err = TryAcquireLock()
		if err == nil {
			t.Fatal("second acquire should fail (already locked)")
		}
	})
}

func TestIsLockHeld(t *testing.T) {
	withTempDir(t, func() {
		if IsLockHeld() {
			t.Error("no lock held yet, should be false")
		}

		closer, err := TryAcquireLock()
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}

		if !IsLockHeld() {
			t.Error("lock should be held after acquire")
		}
		closer()

		if IsLockHeld() {
			t.Error("lock should be released after closer()")
		}
	})
}

func TestWriteAndReadPID(t *testing.T) {
	withTempDir(t, func() {
		if err := WritePID(8990, "/tmp/test.toml"); err != nil {
			t.Fatalf("WritePID: %v", err)
		}

		pf, err := ReadPID()
		if err != nil {
			t.Fatalf("ReadPID: %v", err)
		}
		if pf == nil {
			t.Fatal("PID file should exist")
		}
		if pf.Port != 8990 {
			t.Errorf("want port 8990, got %d", pf.Port)
		}
		if pf.PID != os.Getpid() {
			t.Errorf("want PID %d, got %d", os.Getpid(), pf.PID)
		}
	})
}

func TestIsRunning_noPIDFile(t *testing.T) {
	withTempDir(t, func() {
		running, pf, err := IsRunning()
		if err != nil {
			t.Fatalf("IsRunning: %v", err)
		}
		if running {
			t.Error("should not be running without PID file")
		}
		if pf != nil {
			t.Error("PIDFile should be nil")
		}
	})
}

func TestFormatStatus(t *testing.T) {
	withTempDir(t, func() {
		data, err := FormatStatus()
		if err != nil {
			t.Fatalf("FormatStatus: %v", err)
		}
		if len(data) == 0 {
			t.Error("FormatStatus should return JSON")
		}
	})
}

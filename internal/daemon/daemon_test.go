package daemon

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
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

func TestFormatStatus_WithPIDAndError(t *testing.T) {
	mu.Lock()
	orig := isRunningFn
	isRunningFn = func() (bool, *PIDFile, error) {
		return true, &PIDFile{PID: 42, Port: 8990, StartedAt: time.Unix(1700000000, 0).UTC()}, nil
	}
	defer func() {
		isRunningFn = orig
		mu.Unlock()
	}()

	data, err := FormatStatus()
	if err != nil {
		t.Fatalf("FormatStatus: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"running": true`) || !strings.Contains(text, `"pid": 42`) {
		t.Fatalf("unexpected status json: %s", text)
	}

	isRunningFn = func() (bool, *PIDFile, error) { return false, nil, fmt.Errorf("boom") }
	if _, err := FormatStatus(); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected propagated error, got %v", err)
	}
}

func TestExpandHomeImpl(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := expandHomeImpl("~/Library/Test")
	want := filepath.Join(home, "Library", "Test")
	if got != want {
		t.Fatalf("expandHomeImpl: got %q want %q", got, want)
	}
	if expandHomeImpl("/tmp/plain") != "/tmp/plain" {
		t.Fatal("plain path should stay unchanged")
	}
}

func TestWritePID_CreateDirError(t *testing.T) {
	withTempDir(t, func() {
		blocker := filepath.Join(os.TempDir(), fmt.Sprintf("slm-blocker-%d", os.Getpid()))
		if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(blocker)

		origFn := expandHomeFn
		expandHomeFn = func(path string) string {
			return filepath.Join(blocker, "nested")
		}
		defer func() { expandHomeFn = origFn }()

		if err := WritePID(8990, "/tmp/test.toml"); err == nil || !strings.Contains(err.Error(), "create pid dir") {
			t.Fatalf("expected create dir error, got %v", err)
		}
	})
}

func TestRemovePID(t *testing.T) {
	withTempDir(t, func() {
		if err := WritePID(8990, "/tmp/test.toml"); err != nil {
			t.Fatal(err)
		}
		if err := RemovePID(); err != nil {
			t.Fatalf("RemovePID: %v", err)
		}
		if _, err := os.Stat(PIDPath()); !os.IsNotExist(err) {
			t.Fatalf("pid file should be removed, stat err=%v", err)
		}
	})
}

func TestReadPID_InvalidJSON(t *testing.T) {
	withTempDir(t, func() {
		if err := os.MkdirAll(filepath.Dir(PIDPath()), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(PIDPath(), []byte("{"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPID(); err == nil || !strings.Contains(err.Error(), "parse pid file") {
			t.Fatalf("expected parse error, got %v", err)
		}
	})
}

func TestIsRunning_LiveCurrentProcess(t *testing.T) {
	withTempDir(t, func() {
		if err := WritePID(8990, "/tmp/test.toml"); err != nil {
			t.Fatal(err)
		}
		running, pf, err := IsRunning()
		if err != nil {
			t.Fatalf("IsRunning: %v", err)
		}
		if !running || pf == nil || pf.PID != os.Getpid() {
			t.Fatalf("expected current process to be running, running=%v pf=%#v", running, pf)
		}
	})
}

func TestIsRunning_StalePIDRemovesFile(t *testing.T) {
	withTempDir(t, func() {
		if err := os.MkdirAll(filepath.Dir(PIDPath()), 0755); err != nil {
			t.Fatal(err)
		}
		data := `{"pid":999999,"port":8990,"started_at":"2026-01-01T00:00:00Z","config_path":""}`
		if err := os.WriteFile(PIDPath(), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		running, pf, err := IsRunning()
		if err != nil {
			t.Fatalf("IsRunning: %v", err)
		}
		if running || pf == nil {
			t.Fatalf("expected stale pid result, running=%v pf=%#v", running, pf)
		}
		if _, err := os.Stat(PIDPath()); !os.IsNotExist(err) {
			t.Fatalf("stale pid file should be removed, stat err=%v", err)
		}
	})
}

func TestSendSignalImpl_SignalZeroCurrentProcess(t *testing.T) {
	if err := sendSignalImpl(os.Getpid(), syscall.Signal(0)); err != nil {
		t.Fatalf("sendSignalImpl(signal 0): %v", err)
	}
}

func TestStatus_NotRunning(t *testing.T) {
	mu.Lock()
	orig := isRunningFn
	isRunningFn = func() (bool, *PIDFile, error) { return false, nil, nil }
	defer func() {
		isRunningFn = orig
		mu.Unlock()
	}()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := Status()
	_ = w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if got := buf.String(); got != "Slimference is not running.\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestStatus_Running(t *testing.T) {
	mu.Lock()
	orig := isRunningFn
	isRunningFn = func() (bool, *PIDFile, error) {
		return true, &PIDFile{PID: 42, Port: 8990, StartedAt: time.Unix(1700000000, 0).UTC()}, nil
	}
	defer func() {
		isRunningFn = orig
		mu.Unlock()
	}()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := Status()
	_ = w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "PID 42") || !strings.Contains(out, "port 8990") {
		t.Fatalf("unexpected stdout: %q", out)
	}
}

func TestStopDaemon_NotRunning(t *testing.T) {
	mu.Lock()
	origIsRunningFn := isRunningFn
	origSendSignalFn := sendSignalFn
	isRunningFn = func() (bool, *PIDFile, error) { return false, nil, nil }
	sendSignalFn = func(pid int, sig syscall.Signal) error {
		t.Fatalf("sendSignalFn should not be called, pid=%d sig=%v", pid, sig)
		return nil
	}
	defer func() {
		isRunningFn = origIsRunningFn
		sendSignalFn = origSendSignalFn
		mu.Unlock()
	}()

	if err := StopDaemon(); err != nil {
		t.Fatalf("StopDaemon: %v", err)
	}
}

func TestStopDaemon_SendSIGTERMError(t *testing.T) {
	mu.Lock()
	origIsRunningFn := isRunningFn
	origSendSignalFn := sendSignalFn
	isRunningFn = func() (bool, *PIDFile, error) {
		return true, &PIDFile{PID: 42, Port: 8990}, nil
	}
	sendSignalFn = func(pid int, sig syscall.Signal) error {
		if pid != 42 || sig != syscall.SIGTERM {
			t.Fatalf("unexpected signal call pid=%d sig=%v", pid, sig)
		}
		return fmt.Errorf("boom")
	}
	defer func() {
		isRunningFn = origIsRunningFn
		sendSignalFn = origSendSignalFn
		mu.Unlock()
	}()

	if err := StopDaemon(); err == nil || !strings.Contains(err.Error(), "send SIGTERM") {
		t.Fatalf("expected SIGTERM error, got %v", err)
	}
}

func TestStopDaemon_Graceful(t *testing.T) {
	mu.Lock()
	origIsRunningFn := isRunningFn
	origSendSignalFn := sendSignalFn
	origSleepFn := sleepFn
	var checks int
	isRunningFn = func() (bool, *PIDFile, error) {
		checks++
		if checks == 1 {
			return true, &PIDFile{PID: 42, Port: 8990}, nil
		}
		return false, &PIDFile{PID: 42, Port: 8990}, nil
	}
	var signals []syscall.Signal
	sendSignalFn = func(pid int, sig syscall.Signal) error {
		signals = append(signals, sig)
		return nil
	}
	sleepFn = func(time.Duration) {}
	defer func() {
		isRunningFn = origIsRunningFn
		sendSignalFn = origSendSignalFn
		sleepFn = origSleepFn
		mu.Unlock()
	}()

	if err := StopDaemon(); err != nil {
		t.Fatalf("StopDaemon: %v", err)
	}
	if len(signals) != 1 || signals[0] != syscall.SIGTERM {
		t.Fatalf("unexpected signals: %v", signals)
	}
}

func TestStopDaemon_ForceKill(t *testing.T) {
	mu.Lock()
	origIsRunningFn := isRunningFn
	origSendSignalFn := sendSignalFn
	origSleepFn := sleepFn
	origNow := timeNow
	isRunningFn = func() (bool, *PIDFile, error) {
		return true, &PIDFile{PID: 42, Port: 8990}, nil
	}
	var signals []syscall.Signal
	sendSignalFn = func(pid int, sig syscall.Signal) error {
		signals = append(signals, sig)
		return nil
	}
	sleepFn = func(time.Duration) {}
	nowSeq := []time.Time{
		time.Unix(100, 0),
		time.Unix(111, 0),
	}
	timeNow = func() time.Time {
		v := nowSeq[0]
		if len(nowSeq) > 1 {
			nowSeq = nowSeq[1:]
		}
		return v
	}
	defer func() {
		isRunningFn = origIsRunningFn
		sendSignalFn = origSendSignalFn
		sleepFn = origSleepFn
		timeNow = origNow
		mu.Unlock()
	}()

	if err := StopDaemon(); err != nil {
		t.Fatalf("StopDaemon: %v", err)
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("unexpected signals: %v", signals)
	}
}

func TestRunDaemon_AlreadyRunning(t *testing.T) {
	mu.Lock()
	origIsRunningFn := isRunningFn
	isRunningFn = func() (bool, *PIDFile, error) {
		return true, &PIDFile{PID: 42, Port: 8990}, nil
	}
	defer func() {
		isRunningFn = origIsRunningFn
		mu.Unlock()
	}()

	if err := RunDaemon(func() (int, func(context.Context) error, error) {
		t.Fatal("startProxy should not be called when already running")
		return 0, nil, nil
	}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected already running error, got %v", err)
	}
}

func TestRunDaemon_LockError(t *testing.T) {
	withTempDir(t, func() {
		origIsRunningFn := isRunningFn
		origTryAcquireLock := TryAcquireLock
		isRunningFn = func() (bool, *PIDFile, error) { return false, nil, nil }
		TryAcquireLock = func() (func(), error) { return nil, fmt.Errorf("lock fail") }
		defer func() {
			isRunningFn = origIsRunningFn
			TryAcquireLock = origTryAcquireLock
		}()

		if err := RunDaemon(func() (int, func(context.Context) error, error) {
			t.Fatal("startProxy should not be called when lock acquisition fails")
			return 0, nil, nil
		}); err == nil || !strings.Contains(err.Error(), "lock fail") {
			t.Fatalf("expected lock error, got %v", err)
		}
	})
}

func TestRunDaemon_StartProxyError(t *testing.T) {
	withTempDir(t, func() {
		origIsRunningFn := isRunningFn
		origTryAcquireLock := TryAcquireLock
		isRunningFn = func() (bool, *PIDFile, error) { return false, nil, nil }
		released := false
		TryAcquireLock = func() (func(), error) {
			return func() { released = true }, nil
		}
		defer func() {
			isRunningFn = origIsRunningFn
			TryAcquireLock = origTryAcquireLock
		}()

		err := RunDaemon(func() (int, func(context.Context) error, error) {
			return 0, nil, fmt.Errorf("proxy fail")
		})
		if err == nil || !strings.Contains(err.Error(), "start proxy") {
			t.Fatalf("expected start proxy error, got %v", err)
		}
		if !released {
			t.Fatal("lock closer should run on startProxy error")
		}
	})
}

func TestRunDaemon_Success(t *testing.T) {
	withTempDir(t, func() {
		origIsRunningFn := isRunningFn
		origTryAcquireLock := TryAcquireLock
		origSignalNotifyFn := signalNotifyFn
		origSignalStopFn := signalStopFn
		isRunningFn = func() (bool, *PIDFile, error) { return false, nil, nil }
		released := false
		TryAcquireLock = func() (func(), error) {
			return func() { released = true }, nil
		}
		signalNotifyFn = func(c chan<- os.Signal, _ ...os.Signal) {
			c <- syscall.SIGTERM
		}
		signalStopFn = func(chan<- os.Signal) {}
		defer func() {
			isRunningFn = origIsRunningFn
			TryAcquireLock = origTryAcquireLock
			signalNotifyFn = origSignalNotifyFn
			signalStopFn = origSignalStopFn
		}()

		shutdownCalled := false
		err := RunDaemon(func() (int, func(context.Context) error, error) {
			return 8990, func(context.Context) error {
				shutdownCalled = true
				return nil
			}, nil
		})
		if err != nil {
			t.Fatalf("RunDaemon: %v", err)
		}
		if !shutdownCalled {
			t.Fatal("shutdown should be called on signal")
		}
		if !released {
			t.Fatal("lock closer should run")
		}
		if _, err := os.Stat(PIDPath()); !os.IsNotExist(err) {
			t.Fatalf("pid file should be removed, stat err=%v", err)
		}
	})
}

func TestRunDaemonWithReload_SIGHUPReloadsAndKeepsRunning(t *testing.T) {
	withTempDir(t, func() {
		origIsRunningFn := isRunningFn
		origTryAcquireLock := TryAcquireLock
		origSignalNotifyFn := signalNotifyFn
		origSignalStopFn := signalStopFn
		isRunningFn = func() (bool, *PIDFile, error) { return false, nil, nil }
		released := false
		TryAcquireLock = func() (func(), error) {
			return func() { released = true }, nil
		}
		signalNotifyFn = func(c chan<- os.Signal, _ ...os.Signal) {
			go func() {
				c <- syscall.SIGHUP
				c <- syscall.SIGTERM
			}()
		}
		signalStopFn = func(chan<- os.Signal) {}
		defer func() {
			isRunningFn = origIsRunningFn
			TryAcquireLock = origTryAcquireLock
			signalNotifyFn = origSignalNotifyFn
			signalStopFn = origSignalStopFn
		}()

		reloads := 0
		shutdownCalled := false
		err := RunDaemonWithReload(func() (int, func(context.Context) error, error) {
			return 8990, func(context.Context) error {
				shutdownCalled = true
				return nil
			}, nil
		}, func() {
			reloads++
		})
		if err != nil {
			t.Fatalf("RunDaemonWithReload: %v", err)
		}
		if reloads != 1 {
			t.Fatalf("reloads=%d, want 1", reloads)
		}
		if !shutdownCalled {
			t.Fatal("shutdown should be called after SIGTERM")
		}
		if !released {
			t.Fatal("lock closer should run")
		}
	})
}

func TestTryAcquireLock_StaleSocketRecovery(t *testing.T) {
	withTempDir(t, func() {
		path := LockPath()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		addr, err := net.ResolveUnixAddr("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		ln, err := net.ListenUnix("unix", addr)
		if err != nil {
			t.Fatal(err)
		}
		if err := ln.Close(); err != nil {
			t.Fatal(err)
		}

		closer, err := TryAcquireLock()
		if err != nil {
			t.Fatalf("TryAcquireLock stale recovery: %v", err)
		}
		closer()
	})
}

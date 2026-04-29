package daemon

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWritePID_WriteFileError(t *testing.T) {
	withTempDir(t, func() {
		if err := os.MkdirAll(PIDPath(), 0o755); err != nil {
			t.Fatalf("mkdir pid path: %v", err)
		}
		err := WritePID(8990, "")
		if err == nil {
			t.Fatal("expected write pid error")
		}
	})
}

func TestWritePID_MarshalError(t *testing.T) {
	withTempDir(t, func() {
		orig := marshalPIDFn
		marshalPIDFn = func(any, string, string) ([]byte, error) {
			return nil, errors.New("marshal boom")
		}
		defer func() { marshalPIDFn = orig }()

		err := WritePID(8990, "")
		if err == nil || !strings.Contains(err.Error(), "marshal pid file") {
			t.Fatalf("expected marshal pid file error, got %v", err)
		}
	})
}

func TestReadPID_ReadFileError(t *testing.T) {
	withTempDir(t, func() {
		if err := os.MkdirAll(PIDPath(), 0o755); err != nil {
			t.Fatalf("mkdir pid path: %v", err)
		}
		_, err := ReadPID()
		if err == nil || !strings.Contains(err.Error(), "read pid file") {
			t.Fatalf("expected read pid file error, got %v", err)
		}
	})
}

func TestIsRunning_ReadPIDError(t *testing.T) {
	withTempDir(t, func() {
		if err := os.MkdirAll(PIDPath(), 0o755); err != nil {
			t.Fatalf("mkdir pid path: %v", err)
		}
		_, _, err := IsRunning()
		if err == nil || !strings.Contains(err.Error(), "read pid file") {
			t.Fatalf("expected read pid error, got %v", err)
		}
	})
}

func TestIsRunning_FindProcessError(t *testing.T) {
	withTempDir(t, func() {
		if err := WritePID(8990, ""); err != nil {
			t.Fatalf("WritePID: %v", err)
		}
		orig := findProcessFn
		findProcessFn = func(int) (*os.Process, error) {
			return nil, errors.New("find boom")
		}
		defer func() { findProcessFn = orig }()

		running, pf, err := IsRunning()
		if err != nil {
			t.Fatalf("IsRunning: %v", err)
		}
		if running || pf == nil {
			t.Fatalf("expected false with pid file preserved, got running=%v pf=%#v", running, pf)
		}
	})
}

func TestSendSignalImpl_FindProcessError(t *testing.T) {
	mu.Lock()
	orig := findProcessFn
	findProcessFn = func(int) (*os.Process, error) {
		return nil, errors.New("find boom")
	}
	defer func() {
		findProcessFn = orig
		mu.Unlock()
	}()

	err := sendSignalImpl(42, syscall.SIGTERM)
	if err == nil || !strings.Contains(err.Error(), "find boom") {
		t.Fatalf("expected find process error, got %v", err)
	}
}

func TestStopDaemon_CheckError(t *testing.T) {
	mu.Lock()
	orig := isRunningFn
	isRunningFn = func() (bool, *PIDFile, error) {
		return false, nil, errors.New("status boom")
	}
	defer func() {
		isRunningFn = orig
		mu.Unlock()
	}()

	err := StopDaemon()
	if err == nil || !strings.Contains(err.Error(), "check daemon") {
		t.Fatalf("expected check daemon error, got %v", err)
	}
}

func TestStatus_Error(t *testing.T) {
	mu.Lock()
	orig := isRunningFn
	isRunningFn = func() (bool, *PIDFile, error) {
		return false, nil, errors.New("status boom")
	}
	defer func() {
		isRunningFn = orig
		mu.Unlock()
	}()

	err := Status()
	if err == nil || !strings.Contains(err.Error(), "status boom") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestRunDaemon_WritePIDWarning(t *testing.T) {
	withTempDir(t, func() {
		origIsRunningFn := isRunningFn
		origTryAcquireLock := TryAcquireLock
		origSignalNotifyFn := signalNotifyFn
		origSignalStopFn := signalStopFn
		isRunningFn = func() (bool, *PIDFile, error) { return false, nil, nil }
		TryAcquireLock = func() (func(), error) { return func() {}, nil }
		signalNotifyFn = func(c chan<- os.Signal, _ ...os.Signal) { c <- syscall.SIGTERM }
		signalStopFn = func(chan<- os.Signal) {}
		defer func() {
			isRunningFn = origIsRunningFn
			TryAcquireLock = origTryAcquireLock
			signalNotifyFn = origSignalNotifyFn
			signalStopFn = origSignalStopFn
		}()

		if err := os.MkdirAll(PIDPath(), 0o755); err != nil {
			t.Fatalf("mkdir pid path: %v", err)
		}

		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		err := RunDaemon(func() (int, func(context.Context) error, error) {
			return 8990, func(context.Context) error { return nil }, nil
		})
		_ = w.Close()
		os.Stderr = oldStderr
		if err != nil {
			t.Fatalf("RunDaemon: %v", err)
		}

		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		if !strings.Contains(buf.String(), "warning: could not write PID file") {
			t.Fatalf("expected pid warning, got %q", buf.String())
		}
	})
}

func TestLaunchctlExecImpl_EmptyStdErr(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "launchctl")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write launchctl stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := launchctlExecImpl("fail")
	if err == nil || !strings.Contains(err.Error(), "launchctl fail:") {
		t.Fatalf("expected launchctl empty stderr error, got %v", err)
	}
}

func TestLaunchctlInspectImpl_UsesLaunchctlOutput(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "launchctl")
	body := `#!/bin/sh
cat <<'EOF'
{
    "PID" = 4321;
    "LastExitStatus" = 7;
}
EOF
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write launchctl stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	snap, err := launchctlInspectImpl("com.slimference.daemon")
	if err != nil {
		t.Fatalf("launchctlInspectImpl: %v", err)
	}
	if snap.PID != 4321 || snap.LastExitStatus != 7 {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestWriteLaunchdEnvFile_CreateDirError(t *testing.T) {
	withLaunchdTempDir(t, func(tmp string, _ *[]string) {
		blocker := filepath.Join(tmp, "env-blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatalf("write blocker: %v", err)
		}
		orig := LaunchdEnvPathFn
		LaunchdEnvPathFn = func() string { return filepath.Join(blocker, "child.env") }
		defer func() { LaunchdEnvPathFn = orig }()

		err := writeLaunchdEnvFile()
		if err == nil || !strings.Contains(err.Error(), "create env dir") {
			t.Fatalf("expected create env dir error, got %v", err)
		}
	})
}

func TestWriteLaunchdEnvFile_WriteError(t *testing.T) {
	withLaunchdTempDir(t, func(_ string, _ *[]string) {
		if err := os.MkdirAll(LaunchdEnvPath(), 0o755); err != nil {
			t.Fatalf("mkdir env path: %v", err)
		}
		if err := os.WriteFile(filepath.Join(LaunchdEnvPath(), "child"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write child: %v", err)
		}
		err := writeLaunchdEnvFile()
		if err == nil || !strings.Contains(err.Error(), "write env file") {
			t.Fatalf("expected write env file error, got %v", err)
		}
	})
}

func TestInstallLaunchd_DirAndLaunchctlErrors(t *testing.T) {
	withLaunchdTempDir(t, func(tmp string, _ *[]string) {
		blocker := filepath.Join(tmp, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatalf("write blocker: %v", err)
		}

		t.Run("plist dir", func(t *testing.T) {
			orig := LaunchdPlistPathFn
			LaunchdPlistPathFn = func() string { return filepath.Join(blocker, "plist", "svc.plist") }
			defer func() { LaunchdPlistPathFn = orig }()
			err := InstallLaunchd(filepath.Join(tmp, "slimference"))
			if err == nil || !strings.Contains(err.Error(), "create LaunchAgents dir") {
				t.Fatalf("expected plist dir error, got %v", err)
			}
		})

		t.Run("state dir", func(t *testing.T) {
			orig := expandHomeFn
			expandHomeFn = func(path string) string {
				if path == DefaultPIDDir {
					return filepath.Join(blocker, "state")
				}
				if strings.HasPrefix(path, "~/") {
					return filepath.Join(tmp, path[2:])
				}
				return path
			}
			defer func() { expandHomeFn = orig }()
			err := InstallLaunchd(filepath.Join(tmp, "slimference"))
			if err == nil || !strings.Contains(err.Error(), "create state dir") {
				t.Fatalf("expected state dir error, got %v", err)
			}
		})

		t.Run("log dir", func(t *testing.T) {
			orig := expandHomeFn
			expandHomeFn = func(path string) string {
				if path == "~/.slimference/logs" {
					return filepath.Join(blocker, "logs")
				}
				if path == DefaultPIDDir {
					return filepath.Join(tmp, ".slimference")
				}
				if strings.HasPrefix(path, "~/") {
					return filepath.Join(tmp, path[2:])
				}
				return path
			}
			defer func() { expandHomeFn = orig }()
			err := InstallLaunchd(filepath.Join(tmp, "slimference"))
			if err == nil || !strings.Contains(err.Error(), "create log dir") {
				t.Fatalf("expected log dir error, got %v", err)
			}
		})

		t.Run("write env propagate", func(t *testing.T) {
			orig := LaunchdEnvPathFn
			LaunchdEnvPathFn = func() string { return filepath.Join(blocker, "env", "launchd.env") }
			defer func() { LaunchdEnvPathFn = orig }()
			err := InstallLaunchd(filepath.Join(tmp, "slimference"))
			if err == nil || !strings.Contains(err.Error(), "create env dir") {
				t.Fatalf("expected env write error, got %v", err)
			}
		})

		t.Run("write plist", func(t *testing.T) {
			orig := LaunchdPlistPathFn
			LaunchdPlistPathFn = func() string { return filepath.Join(tmp, "LaunchAgents", "plist-dir") }
			defer func() { LaunchdPlistPathFn = orig }()
			if err := os.MkdirAll(LaunchdPlistPath(), 0o755); err != nil {
				t.Fatalf("mkdir plist path: %v", err)
			}
			if err := os.WriteFile(filepath.Join(LaunchdPlistPath(), "child"), []byte("x"), 0o644); err != nil {
				t.Fatalf("write child: %v", err)
			}
			err := InstallLaunchd(filepath.Join(tmp, "slimference"))
			if err == nil || !strings.Contains(err.Error(), "write plist") {
				t.Fatalf("expected write plist error, got %v", err)
			}
		})

		t.Run("enable", func(t *testing.T) {
			orig := launchctlExec
			launchctlExec = func(args ...string) error {
				if len(args) > 0 && args[0] == "enable" {
					return errors.New("enable boom")
				}
				return nil
			}
			defer func() { launchctlExec = orig }()
			err := InstallLaunchd(filepath.Join(tmp, "slimference"))
			if err == nil || !strings.Contains(err.Error(), "enable boom") {
				t.Fatalf("expected enable error, got %v", err)
			}
		})

		t.Run("kickstart", func(t *testing.T) {
			orig := launchctlExec
			launchctlExec = func(args ...string) error {
				if len(args) > 0 && args[0] == "kickstart" {
					return errors.New("kickstart boom")
				}
				return nil
			}
			defer func() { launchctlExec = orig }()
			err := InstallLaunchd(filepath.Join(tmp, "slimference"))
			if err == nil || !strings.Contains(err.Error(), "kickstart boom") {
				t.Fatalf("expected kickstart error, got %v", err)
			}
		})
	})
}

func TestUninstallLaunchd_RemovePlistError(t *testing.T) {
	withLaunchdTempDir(t, func(_ string, _ *[]string) {
		if err := os.MkdirAll(LaunchdPlistPath(), 0o755); err != nil {
			t.Fatalf("mkdir plist path: %v", err)
		}
		if err := os.WriteFile(filepath.Join(LaunchdPlistPath(), "child"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write child: %v", err)
		}
		err := UninstallLaunchd()
		if err == nil || !strings.Contains(err.Error(), "remove plist") {
			t.Fatalf("expected remove plist error, got %v", err)
		}
	})
}

func TestTryAcquireLock_SeamErrors(t *testing.T) {
	withTempDir(t, func() {
		t.Run("mkdir", func(t *testing.T) {
			orig := expandHomeFn
			blocker := filepath.Join(os.TempDir(), "daemon-lock-blocker")
			_ = os.Remove(blocker)
			if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
				t.Fatalf("write blocker: %v", err)
			}
			defer os.Remove(blocker)
			expandHomeFn = func(string) string { return filepath.Join(blocker, "nested") }
			defer func() { expandHomeFn = orig }()
			_, err := tryAcquireLockImpl()
			if err == nil || !strings.Contains(err.Error(), "create lock dir") {
				t.Fatalf("expected create lock dir error, got %v", err)
			}
		})

		t.Run("resolve", func(t *testing.T) {
			orig := resolveUnixFn
			resolveUnixFn = func(string, string) (*net.UnixAddr, error) {
				return nil, errors.New("resolve boom")
			}
			defer func() { resolveUnixFn = orig }()
			_, err := tryAcquireLockImpl()
			if err == nil || !strings.Contains(err.Error(), "resolve lock address") {
				t.Fatalf("expected resolve error, got %v", err)
			}
		})

		t.Run("listen retry still fails", func(t *testing.T) {
			origResolve := resolveUnixFn
			origListen := listenUnixFn
			origDial := dialUnixFn
			resolveUnixFn = net.ResolveUnixAddr
			listenCalls := 0
			listenUnixFn = func(network string, laddr *net.UnixAddr) (*net.UnixListener, error) {
				listenCalls++
				return nil, errors.New("listen boom")
			}
			dialUnixFn = func(string, *net.UnixAddr, *net.UnixAddr) (*net.UnixConn, error) {
				return nil, errors.New("dial boom")
			}
			defer func() {
				resolveUnixFn = origResolve
				listenUnixFn = origListen
				dialUnixFn = origDial
			}()
			_, err := tryAcquireLockImpl()
			if err == nil || !strings.Contains(err.Error(), "already running") || listenCalls != 2 {
				t.Fatalf("expected retry failure, got err=%v listenCalls=%d", err, listenCalls)
			}
		})
	})
}

func TestIsLockHeld_ResolveError(t *testing.T) {
	mu.Lock()
	orig := resolveUnixFn
	resolveUnixFn = func(string, string) (*net.UnixAddr, error) {
		return nil, errors.New("resolve boom")
	}
	defer func() {
		resolveUnixFn = orig
		mu.Unlock()
	}()

	if IsLockHeld() {
		t.Fatal("expected false on resolve error")
	}
}

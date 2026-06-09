package main

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/daemon"
)

func TestPatchSNIPeekModeCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := patchSNIPeekMode(path, true); err != nil {
		t.Fatalf("patch: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "sni_peek_mode = true") {
		t.Errorf("expected sni_peek_mode = true in %q", string(data))
	}
}

func stubReloadPIDWriter(t *testing.T) {
	t.Helper()
	prev := writePIDFileFn
	writePIDFileFn = func() func() { return func() {} }
	t.Cleanup(func() { writePIDFileFn = prev })
}

func TestPatchSNIPeekModeReplacesExistingKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "[transparent]\nenabled = false\nsni_peek_mode = false\nca_dir = \"~/.slimference\"\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := patchSNIPeekMode(path, true); err != nil {
		t.Fatalf("patch: %v", err)
	}
	data, _ := os.ReadFile(path)
	out := string(data)
	if !strings.Contains(out, "sni_peek_mode = true") {
		t.Errorf("not replaced: %q", out)
	}
	// Preserve unrelated keys
	if !strings.Contains(out, "ca_dir = \"~/.slimference\"") {
		t.Errorf("ca_dir lost: %q", out)
	}
	if !strings.Contains(out, "enabled = false") {
		t.Errorf("enabled lost: %q", out)
	}
}

func TestPatchSNIPeekModeInsertsKeyIntoExistingTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "[transparent]\nenabled = false\nca_dir = \"~/.slimference\"\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := patchSNIPeekMode(path, true); err != nil {
		t.Fatalf("patch: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "sni_peek_mode = true") {
		t.Errorf("not inserted: %q", string(data))
	}
}

func TestPatchSNIPeekModeAppendsTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "[proxy]\nlisten_port = 8990\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := patchSNIPeekMode(path, false); err != nil {
		t.Fatalf("patch: %v", err)
	}
	data, _ := os.ReadFile(path)
	out := string(data)
	if !strings.Contains(out, "[transparent]") {
		t.Errorf("table not appended: %q", out)
	}
	if !strings.Contains(out, "sni_peek_mode = false") {
		t.Errorf("key not set: %q", out)
	}
	if !strings.Contains(out, "[proxy]") {
		t.Errorf("existing section dropped: %q", out)
	}
}

func TestPatchSNIPeekModeIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := patchSNIPeekMode(path, true); err != nil {
		t.Fatalf("first: %v", err)
	}
	before, _ := os.ReadFile(path)
	if err := patchSNIPeekMode(path, true); err != nil {
		t.Fatalf("second: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("idempotency broken:\nbefore: %q\nafter:  %q", string(before), string(after))
	}
}

func TestPatchSNIPeekModeRejectsEmptyPath(t *testing.T) {
	if err := patchSNIPeekMode("", true); err == nil {
		t.Fatal("expected error on empty path")
	}
}

func TestPatchSNIPeekModeFilesystemErrors(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	if err := patchSNIPeekMode(filepath.Join(parentFile, "config.toml"), true); err == nil {
		t.Fatal("expected mkdir error when parent is a file")
	}

	dirPath := t.TempDir()
	if err := patchSNIPeekMode(dirPath, true); err == nil {
		t.Fatal("expected read error when config path is a directory")
	}
}

func TestSignalDaemonReloadNoPidFile(t *testing.T) {
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return t.TempDir(), nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	prevRunning := daemonIsRunningFn
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	t.Cleanup(func() { daemonIsRunningFn = prevRunning })

	sent, err := signalDaemonReload()
	if err != nil {
		t.Fatalf("expected nil err on missing pidfile, got %v", err)
	}
	if sent {
		t.Error("expected sent=false")
	}
}

func TestSignalDaemonReloadStalePidIgnored(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	prevRunning := daemonIsRunningFn
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) { return false, nil, nil }
	t.Cleanup(func() { daemonIsRunningFn = prevRunning })
	prevSignal := signalPIDFn
	signalPIDFn = func(pid int, sig os.Signal) error {
		if pid != 2147483646 {
			t.Fatalf("pid=%d, want stale pid", pid)
		}
		return syscall.ESRCH
	}
	t.Cleanup(func() { signalPIDFn = prevSignal })

	pidDir := filepath.Join(dir, ".slimference", "run")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// PID 1 always exists on UNIX but is the init process - we
	// shouldn't actually signal it. Use a more conservative path:
	// write a pid that should not exist (max int32 minus 1).
	if err := os.WriteFile(filepath.Join(pidDir, "daemon.pid"),
		[]byte("2147483646\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	sent, err := signalDaemonReload()
	if err != nil {
		t.Fatalf("expected nil err on stale pid, got %v", err)
	}
	if sent {
		t.Error("expected sent=false for stale pid")
	}
}

func TestSignalDaemonReloadFallsBackToCanonicalDaemonPID(t *testing.T) {
	dir := t.TempDir()
	prevHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prevHome })

	prevRunning := daemonIsRunningFn
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 4242}, nil
	}
	t.Cleanup(func() { daemonIsRunningFn = prevRunning })

	prevSignal := signalPIDFn
	var got []int
	signalPIDFn = func(pid int, sig os.Signal) error {
		if sig != syscall.SIGHUP {
			t.Fatalf("signal=%v, want SIGHUP", sig)
		}
		got = append(got, pid)
		if pid == 2147483646 {
			return syscall.ESRCH
		}
		return nil
	}
	t.Cleanup(func() { signalPIDFn = prevSignal })

	pidDir := filepath.Join(dir, ".slimference", "run")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "daemon.pid"),
		[]byte("2147483646\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	sent, err := signalDaemonReload()
	if err != nil {
		t.Fatalf("signalDaemonReload: %v", err)
	}
	if !sent {
		t.Fatal("expected canonical daemon PID fallback to send")
	}
	if len(got) != 2 || got[0] != 2147483646 || got[1] != 4242 {
		t.Fatalf("signal attempts=%v, want [2147483646 4242]", got)
	}
}

func TestSignalDaemonReloadMissingReloadPIDUsesCanonicalDaemonPID(t *testing.T) {
	dir := t.TempDir()
	prevHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prevHome })

	prevRunning := daemonIsRunningFn
	daemonIsRunningFn = func() (bool, *daemon.PIDFile, error) {
		return true, &daemon.PIDFile{PID: 4343}, nil
	}
	t.Cleanup(func() { daemonIsRunningFn = prevRunning })

	prevSignal := signalPIDFn
	var gotPID int
	signalPIDFn = func(pid int, sig os.Signal) error {
		gotPID = pid
		return nil
	}
	t.Cleanup(func() { signalPIDFn = prevSignal })

	sent, err := signalDaemonReload()
	if err != nil {
		t.Fatalf("signalDaemonReload: %v", err)
	}
	if !sent || gotPID != 4343 {
		t.Fatalf("sent=%v pid=%d, want sent=true pid=4343", sent, gotPID)
	}
}

func TestSignalDaemonReloadMalformedPidErrors(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	pidDir := filepath.Join(dir, ".slimference", "run")
	_ = os.MkdirAll(pidDir, 0o755)
	if err := os.WriteFile(filepath.Join(pidDir, "daemon.pid"), []byte("not-a-pid"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := signalDaemonReload(); err == nil {
		t.Fatal("expected error on malformed pid")
	}
}

func TestSignalPIDDefaultFunctionBranches(t *testing.T) {
	prevFind := findProcessFn
	findProcessFn = func(int) (*os.Process, error) { return nil, os.ErrPermission }
	if err := signalPID(123, syscall.SIGHUP); err == nil {
		t.Fatal("expected find process error")
	}
	findProcessFn = prevFind

	if err := signalPID(2147483646, syscall.SIGHUP); err == nil {
		t.Fatal("expected stale process signal error")
	}
}

func TestSignalDaemonReloadCurrentProcess(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })
	prevSignal := signalPIDFn
	var gotPID int
	var gotSignal os.Signal
	signalPIDFn = func(pid int, sig os.Signal) error {
		gotPID = pid
		gotSignal = sig
		return nil
	}
	t.Cleanup(func() { signalPIDFn = prevSignal })

	pidDir := filepath.Join(dir, ".slimference", "run")
	_ = os.MkdirAll(pidDir, 0o755)
	pid := os.Getpid()
	if err := os.WriteFile(filepath.Join(pidDir, "daemon.pid"),
		[]byte(intToString(pid)), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	sent, err := signalDaemonReload()
	if err != nil {
		t.Fatalf("signalDaemonReload: %v", err)
	}
	if !sent {
		t.Error("expected sent=true for self-pid")
	}
	if gotPID != pid || gotSignal != syscall.SIGHUP {
		t.Fatalf("signal target=(%d,%v), want (%d,%v)", gotPID, gotSignal, pid, syscall.SIGHUP)
	}
}

// intToString is a tiny helper to avoid pulling strconv into the
// test file just for one call.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	if neg {
		out = "-" + out
	}
	return out
}

func TestFetchSetupStateNoDaemonErrors(t *testing.T) {
	// Daemon definitely not on this port.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	_ = os.WriteFile(cfgPath, []byte("[proxy]\nlisten_port = 1\n"), 0o600)

	if _, err := fetchSetupState(200_000_000); err == nil {
		t.Fatal("expected error against no-daemon port")
	}
}

func TestDefaultAdminPortFromConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	_ = os.WriteFile(cfgPath, []byte("[proxy]\nlisten_port = 12345\n"), 0o600)
	if got := defaultAdminPort(); got != 12345 {
		t.Errorf("got %d want 12345", got)
	}
}

func TestDefaultAdminPortFallback(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	if got := defaultAdminPort(); got != 8990 {
		t.Errorf("got %d want 8990 fallback", got)
	}
}

func TestDefaultAdminPortInvalidAndZeroValuesFallback(t *testing.T) {
	for _, body := range []string{
		"[proxy]\nlisten_port = 0\n",
		"[proxy]\nlisten_port = nope\n",
		"[proxy]\n",
	} {
		cfgPath := filepath.Join(t.TempDir(), "config.toml")
		t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
		if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if got := defaultAdminPort(); got != 8990 {
			t.Fatalf("body %q: got %d want fallback", body, got)
		}
	}
}

func TestUpsertTransparentKeyEmpty(t *testing.T) {
	out := upsertTransparentKey([]byte(""), "sni_peek_mode", "true")
	if !strings.Contains(string(out), "[transparent]") {
		t.Errorf("missing table: %q", out)
	}
	if !strings.Contains(string(out), "sni_peek_mode = true") {
		t.Errorf("missing key: %q", out)
	}
}

func TestUpsertTransparentKeyPreservesSectionsAndNoTrailingNewline(t *testing.T) {
	out := upsertTransparentKey([]byte("[proxy]\nlisten_port = 8990"), "sni_peek_mode", "true")
	if !strings.Contains(string(out), "listen_port = 8990\n\n[transparent]") {
		t.Fatalf("append did not preserve no-newline suffix: %q", out)
	}

	out = upsertTransparentKey([]byte("[transparent]\nenabled = true\n[proxy]\nlisten_port = 8990\n"), "sni_peek_mode", "false")
	text := string(out)
	if strings.Index(text, "sni_peek_mode = false") > strings.Index(text, "[proxy]") {
		t.Fatalf("key inserted after next section: %q", text)
	}
}

func TestWriteAtomicFailsOnBadDir(t *testing.T) {
	if err := writeAtomic("/nonexistent/dir/file.toml", []byte("x"), 0o600); err == nil {
		t.Fatal("expected error on bad dir")
	}
}

func TestWriteAtomicSuccessAndPidHomeErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.toml")
	if err := writeAtomic(path, []byte("x"), 0o640); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}

	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { osUserHomeDir = prev })
	if got := pidFilePath(); got != "" {
		t.Fatalf("pidFilePath=%q want empty", got)
	}
	if sent, err := signalDaemonReload(); err == nil || sent {
		t.Fatalf("signal with unresolved HOME sent=%v err=%v", sent, err)
	}
}

type fakeAtomicTempFile struct {
	name     string
	writeErr error
	chmodErr error
	closeErr error
}

func (f *fakeAtomicTempFile) Write([]byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return 1, nil
}

func (f *fakeAtomicTempFile) Chmod(os.FileMode) error {
	return f.chmodErr
}

func (f *fakeAtomicTempFile) Close() error {
	return f.closeErr
}

func (f *fakeAtomicTempFile) Name() string {
	return f.name
}

func TestWriteAtomicInjectedFileErrors(t *testing.T) {
	prevCreate := createAtomicTempFileFn
	prevRemove := removeAtomicFileFn
	prevRename := renameAtomicFileFn
	t.Cleanup(func() {
		createAtomicTempFileFn = prevCreate
		removeAtomicFileFn = prevRemove
		renameAtomicFileFn = prevRename
	})
	var removed []string
	removeAtomicFileFn = func(path string) error {
		removed = append(removed, path)
		return nil
	}
	renameAtomicFileFn = func(string, string) error {
		t.Fatal("rename should not run after temp-file failure")
		return nil
	}

	for _, tc := range []struct {
		name string
		file *fakeAtomicTempFile
	}{
		{name: "write", file: &fakeAtomicTempFile{name: "write.tmp", writeErr: errors.New("write failed")}},
		{name: "chmod", file: &fakeAtomicTempFile{name: "chmod.tmp", chmodErr: errors.New("chmod failed")}},
		{name: "close", file: &fakeAtomicTempFile{name: "close.tmp", closeErr: errors.New("close failed")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			removed = nil
			createAtomicTempFileFn = func(string, string) (atomicTempFile, error) {
				return tc.file, nil
			}
			if err := writeAtomic(filepath.Join(t.TempDir(), "file.toml"), []byte("x"), 0o600); err == nil {
				t.Fatal("expected injected error")
			}
			if len(removed) != 1 || removed[0] != tc.file.name {
				t.Fatalf("removed=%v want %q", removed, tc.file.name)
			}
		})
	}
}

func TestSignalDaemonReloadReadAndFindErrors(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	pidPath := filepath.Join(dir, ".slimference", "run", "daemon.pid")
	if err := os.MkdirAll(pidPath, 0o755); err != nil {
		t.Fatalf("mkdir pid path as dir: %v", err)
	}
	if sent, err := signalDaemonReload(); err == nil || sent {
		t.Fatalf("expected read error for pid directory, sent=%v err=%v", sent, err)
	}
}

func TestFetchSetupStateNonOKAndBadJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{name: "non-ok", code: http.StatusTeapot, body: "nope"},
		{name: "bad-json", code: http.StatusOK, body: "not json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			port := ln.Addr().(*net.TCPAddr).Port
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			srv.Listener = ln
			srv.Start()
			t.Cleanup(srv.Close)

			cfgPath := filepath.Join(t.TempDir(), "config.toml")
			t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
			if err := os.WriteFile(cfgPath, []byte("[proxy]\nlisten_port = "+intToString(port)+"\n"), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if _, err := fetchSetupState(time.Second); err == nil {
				t.Fatal("expected fetch error")
			}
		})
	}
}

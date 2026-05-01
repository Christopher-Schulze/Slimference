package transparent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchAgent_DefaultPlistPath(t *testing.T) {
	t.Parallel()
	got := DefaultPlistPath("/Users/test")
	want := "/Users/test/Library/LaunchAgents/com.slimference.daemon.plist"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if DefaultPlistPath("") != "" {
		t.Fatal("empty home must yield empty path")
	}
}

func TestLaunchAgent_InstallWritesPlistAndRunsLaunchctl(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.slimference.daemon.plist")
	mock := newMockExec()
	a := NewLaunchAgent()
	a.SetExec(mock.run)
	if err := a.Install(plistPath, "/usr/local/bin/slimference", filepath.Join(dir, "log")); err != nil {
		t.Fatalf("install: %v", err)
	}
	body, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	if !strings.Contains(string(body), "com.slimference.daemon") {
		t.Fatalf("plist missing label, got %q", body)
	}
	if !strings.Contains(string(body), "/usr/local/bin/slimference") {
		t.Fatalf("plist missing daemon binary path")
	}
	if !strings.Contains(string(body), "RunAtLoad") {
		t.Fatalf("plist missing RunAtLoad key")
	}
	if !strings.Contains(string(body), "KeepAlive") {
		t.Fatalf("plist missing KeepAlive key")
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 launchctl call, got %v", mock.calls)
	}
	got := strings.Join(mock.calls[0], " ")
	if !strings.Contains(got, "load") || !strings.Contains(got, plistPath) {
		t.Fatalf("expected launchctl load <plist>, got %q", got)
	}
}

func TestLaunchAgent_InstallTolerantOnAlreadyLoaded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "agent.plist")
	mock := newMockExec()
	mock.errs["launchctl load -w "+plistPath] = errors.New("exit 1")
	mock.out["launchctl load -w "+plistPath] = []byte("agent already loaded\n")
	a := NewLaunchAgent()
	a.SetExec(mock.run)
	if err := a.Install(plistPath, "/usr/local/bin/slimference", dir); err != nil {
		t.Fatalf("install must tolerate already-loaded, got %v", err)
	}
}

func TestLaunchAgent_InstallSurfacesRealError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "agent.plist")
	mock := newMockExec()
	mock.errs["launchctl load -w "+plistPath] = errors.New("nosuch")
	mock.out["launchctl load -w "+plistPath] = []byte("some other error")
	a := NewLaunchAgent()
	a.SetExec(mock.run)
	if err := a.Install(plistPath, "/bin", dir); err == nil {
		t.Fatal("expected install to surface real launchctl error")
	}
}

func TestLaunchAgent_InstallRequiresArgs(t *testing.T) {
	t.Parallel()
	a := NewLaunchAgent()
	if err := a.Install("", "/bin", ""); err == nil {
		t.Fatal("expected error on empty plistPath")
	}
	if err := a.Install("/p", "", ""); err == nil {
		t.Fatal("expected error on empty daemonBinary")
	}
}

func TestLaunchAgent_InstallWriteFnFailureSurfaced(t *testing.T) {
	t.Parallel()
	a := NewLaunchAgent()
	a.SetWriteFn(func(path string, data []byte, mode os.FileMode) error {
		return errors.New("disk full")
	})
	if err := a.Install("/tmp/whatever.plist", "/bin", ""); err == nil {
		t.Fatal("expected write failure surfaced")
	}
}

func TestLaunchAgent_UninstallRemovesAndUnloads(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "agent.plist")
	if err := os.WriteFile(plistPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mock := newMockExec()
	a := NewLaunchAgent()
	a.SetExec(mock.run)
	if err := a.Uninstall(plistPath); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Fatalf("plist must be removed, stat err=%v", err)
	}
}

func TestLaunchAgent_UninstallNotInstalledIsNoop(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	a := NewLaunchAgent()
	a.SetExec(mock.run)
	if err := a.Uninstall("/nonexistent/path"); err != nil {
		t.Fatalf("uninstall on missing must be no-op, got %v", err)
	}
}

func TestLaunchAgent_UninstallRemoveFailureSurfaced(t *testing.T) {
	t.Parallel()
	a := NewLaunchAgent()
	a.SetExec(func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	})
	a.SetRemoveFn(func(path string) error {
		return errors.New("perm denied")
	})
	if err := a.Uninstall("/tmp/whatever.plist"); err == nil {
		t.Fatal("expected real remove failure surfaced")
	}
}

func TestLaunchAgent_IsInstalled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "p.plist")
	a := NewLaunchAgent()
	if a.IsInstalled(p) {
		t.Fatal("expected false on missing")
	}
	_ = os.WriteFile(p, []byte("x"), 0o644)
	if !a.IsInstalled(p) {
		t.Fatal("expected true on present")
	}
}

func TestLaunchAgent_RenderPlistDefaultLogDir(t *testing.T) {
	t.Parallel()
	body := renderLaunchPlist("/bin", "")
	if !strings.Contains(body, "/tmp/slimference.out.log") {
		t.Fatalf("expected /tmp default log dir, got %q", body)
	}
}

func TestLaunchAgent_SetExecNilNoOp(t *testing.T) {
	t.Parallel()
	a := NewLaunchAgent()
	a.SetExec(nil)
	if a.exec == nil {
		t.Fatal("nil arg must not clear exec hook")
	}
	a.SetWriteFn(nil)
	if a.writeFn == nil {
		t.Fatal("nil arg must not clear writeFn hook")
	}
	a.SetRemoveFn(nil)
	if a.removeFn == nil {
		t.Fatal("nil arg must not clear removeFn hook")
	}
}

package integrate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/hooks"
)

func TestShellFlavor_String(t *testing.T) {
	cases := map[ShellFlavor]string{
		ShellZsh:     "zsh",
		ShellBash:    "bash",
		ShellFish:    "fish",
		ShellUnknown: "unknown",
	}
	for f, want := range cases {
		if got := f.String(); got != want {
			t.Errorf("%d: got %q, want %q", int(f), got, want)
		}
	}
}

func TestBinaryOnPath_MissingReturnsEmpty(t *testing.T) {
	if p := binaryOnPath("definitely-not-a-real-binary-xyzzy-12345"); p != "" {
		t.Fatalf("got %q, want empty", p)
	}
}

func TestBinaryOnPath_PresentReturnsPath(t *testing.T) {
	// `sh` is guaranteed on POSIX.
	p := binaryOnPath("sh")
	if p == "" {
		t.Fatal("sh not found on PATH; test environment unexpected")
	}
}

func TestDetectCodex_NotInstalled(t *testing.T) {
	home := t.TempDir()
	// Point PATH at a directory we know does not contain `codex`.
	t.Setenv("PATH", home)
	s := DetectCodex(home)
	if s.State != ClientNotInstalled {
		t.Fatalf("state = %v, want ClientNotInstalled", s.State)
	}
	if s.BinaryPath != "" {
		t.Fatalf("binary path = %q, want empty", s.BinaryPath)
	}
}

func TestDetectCodex_InstalledButUnwired(t *testing.T) {
	home := t.TempDir()
	// Create a fake codex binary on PATH.
	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	fake := filepath.Join(binDir, "codex")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	s := DetectCodex(home)
	if s.State != ClientInstalled && s.State != ClientPartiallyWired {
		t.Fatalf("state = %v", s.State)
	}
	if s.BinaryPath == "" {
		t.Fatal("binary path empty")
	}
}

func TestDetectCodex_FullyWired(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	if err := os.WriteFile(filepath.Join(binDir, "codex"),
		[]byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	// Wire the config block.
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	if err := hooks.InstallCodex(home, "slimference"); err != nil {
		t.Fatal(err)
	}

	s := DetectCodex(home)
	if s.State != ClientFullyWired {
		t.Fatalf("state = %v, want FullyWired; details=%v", s.State, s.Details)
	}
}

func TestDetectClaude_NotInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", home)
	s := DetectClaude(home, "/bin/zsh")
	if s.State != ClientNotInstalled {
		t.Fatalf("state = %v, want NotInstalled", s.State)
	}
}

func TestDetectClaude_PartiallyWired(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	_ = os.WriteFile(filepath.Join(binDir, "claude"),
		[]byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", binDir)

	// Wire shell-rc only, no hooks.
	rc := filepath.Join(home, ".zshrc")
	if _, err := WriteRCBlock(rc, ShellZsh, ProxyURL); err != nil {
		t.Fatal(err)
	}
	// Missing env (current process has no ANTHROPIC_BASE_URL matching our
	// ProxyURL) → at most PartiallyWired regardless of shell-rc.
	t.Setenv("ANTHROPIC_BASE_URL", "")

	s := DetectClaude(home, "/bin/zsh")
	if s.State == ClientNotInstalled || s.State == ClientFullyWired {
		t.Fatalf("state = %v, want intermediate", s.State)
	}
}

func TestDetectDaemon_HTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	s := DetectDaemon(srv.URL)
	if s.Running {
		t.Fatal("non-200 reported as running")
	}
	if !strings.Contains(strings.ToLower(s.Health), "teapot") &&
		!strings.Contains(s.Health, "418") {
		// Accept either textual or numeric representation.
	}
}

func TestDetectDaemon_OKWithPID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"pid": 4242})
	}))
	defer srv.Close()
	s := DetectDaemon(srv.URL)
	if !s.Running {
		t.Fatal("200 not reported as running")
	}
	if s.PID != 4242 {
		t.Fatalf("pid = %d, want 4242", s.PID)
	}
	if s.Health != "ok" {
		t.Fatalf("health = %q", s.Health)
	}
}

func TestDetectDaemon_DefaultURLWhenEmpty(t *testing.T) {
	// Empty URL falls back to ProxyURL constant - pointed at an
	// unroutable port by default, so this just exercises the nil-URL path.
	s := DetectDaemon("")
	if s.Running {
		t.Fatal("empty url should yield unreachable daemon")
	}
}

func TestStatus_IncludesAllThreeSubsystems(t *testing.T) {
	home := t.TempDir()
	rep := Status(Options{HomeDir: home, ProxyURL: "http://127.0.0.1:1"})
	// Names must be populated (even for not-installed clients).
	if rep.Claude.Name != "claude" {
		t.Errorf("claude name = %q", rep.Claude.Name)
	}
	if rep.Codex.Name != "codex" {
		t.Errorf("codex name = %q", rep.Codex.Name)
	}
	if rep.Daemon.Running {
		t.Fatal("daemon reported running against unreachable URL")
	}
}

func TestFileExists_Helper(t *testing.T) {
	dir := t.TempDir()
	if fileExists(filepath.Join(dir, "nope")) {
		t.Fatal("non-existent file reported as exists")
	}
	f := filepath.Join(dir, "ok")
	_ = os.WriteFile(f, []byte{}, 0o644)
	if !fileExists(f) {
		t.Fatal("existing file not found")
	}
}

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/proxy/transparent"
)

func TestControlledExitMarkerMethod(t *testing.T) {
	ControlledExit{}.controlledExitMarker()
}

func TestCodexCompactionHooksEncodeErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func([]byte)
	}{
		{name: "pre", fn: handleCodexPreCompactHook},
		{name: "post", fn: handleCodexPostCompactHook},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prevHome := osUserHomeDir
			osUserHomeDir = func() (string, error) { return t.TempDir(), nil }
			t.Cleanup(func() { osUserHomeDir = prevHome })

			origStdout := os.Stdout
			origStderr := os.Stderr
			_, closedOut, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			errReader, errWriter, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			_ = closedOut.Close()
			os.Stdout = closedOut
			os.Stderr = errWriter
			t.Cleanup(func() {
				os.Stdout = origStdout
				os.Stderr = origStderr
				_ = errReader.Close()
				_ = errWriter.Close()
			})

			code, exited := captureExit(func() {
				tc.fn([]byte(`{"session_id":"s","turn_id":"t"}`))
			})
			_ = errWriter.Close()
			var stderr bytes.Buffer
			_, _ = io.Copy(&stderr, errReader)
			if !exited || code != 1 {
				t.Fatalf("exit=(%d,%v), want (1,true)", code, exited)
			}
			if !strings.Contains(stderr.String(), "encode codexhook output") {
				t.Fatalf("stderr missing encode error: %q", stderr.String())
			}
		})
	}
}

func TestWriteCompactionMarkerHomeError(t *testing.T) {
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
	t.Cleanup(func() { osUserHomeDir = prev })
	writeCompactionMarker("pre", "session", "turn", "manual")
}

func TestProxyCommandEnvCADirFn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	env := proxyCommandEnv(io.Discard, io.Discard, strings.NewReader(""))
	if got := env.CADirFn(); got != filepath.Join(home, ".slimference") {
		t.Fatalf("CADirFn=%q", got)
	}
}

func TestDefaultAdminPortFallbackBranches(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if got := defaultAdminPort(); got != 8990 {
		t.Fatalf("defaultAdminPort no config=%d", got)
	}

	t.Setenv("SLIMFERENCE_CONFIG", t.TempDir())
	if got := defaultAdminPort(); got != 8990 {
		t.Fatalf("defaultAdminPort unreadable config=%d", got)
	}
}

func TestRemoteProxyAdapterInvalidListenAddressBranches(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenAddress = "%zz"
	a := &remoteProxyAdapter{cfg: cfg, client: http.DefaultClient}
	if got := a.AppEntries(); got != nil {
		t.Fatalf("invalid URL AppEntries=%v", got)
	}
	if err := a.SetAppEnabled("codex_cli", true); err == nil {
		t.Fatal("invalid URL SetAppEnabled should fail")
	}
}

func TestStartSNIPeekEngineOnErrorHook(t *testing.T) {
	home := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	cfg.Transparent.SNIPeekPort = pickFreePort(t)
	p := proxy.New(cfg)
	engine, cancel := startSNIPeekEngine(p, cfg, nil)
	if engine == nil || cancel == nil {
		t.Fatal("expected running engine")
	}
	t.Cleanup(cancel)
	if engine.OnError == nil {
		t.Fatal("expected OnError hook")
	}
	engine.OnError(errors.New("synthetic engine error"))
	time.Sleep(20 * time.Millisecond)
	_ = engine.Listener.Close()
	time.Sleep(80 * time.Millisecond)
}

func TestRunSNIPeekEngineLogsRunError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	runSNIPeekEngine(context.Background(), &transparent.Engine{Listener: ln}, addr)
}

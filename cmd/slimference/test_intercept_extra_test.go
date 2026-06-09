package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
)

func TestTestIntercept_listenErrorExits1(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	cfg := config.Defaults()
	cfg.Proxy.ListenAddress = "127.0.0.1"
	cfg.Proxy.ListenPort = port

	oldStdout := os.Stdout
	rp, wp, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wp
	defer func() { os.Stdout = oldStdout }()

	code, exited := captureExit(func() {
		testIntercept(cfg, "codex")
	})

	_ = wp.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "failed to start") {
		t.Fatalf("stdout: %q", buf.String())
	}
}

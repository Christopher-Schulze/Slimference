package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestT68_PostInstallHealthProbe_OK(t *testing.T) {
	orig := healthProbeFn
	defer func() { healthProbeFn = orig }()
	healthProbeFn = func(url string, to time.Duration) (bool, string) {
		return true, "pid 12345 uptime 0s"
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	runPostInstallHealthProbe()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if !strings.Contains(buf.String(), "Health probe: ok") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestT68_PostInstallHealthProbe_Degraded(t *testing.T) {
	orig := healthProbeFn
	defer func() { healthProbeFn = orig }()
	healthProbeFn = func(url string, to time.Duration) (bool, string) {
		return false, "timeout"
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	runPostInstallHealthProbe()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	out := buf.String()
	if !strings.Contains(out, "Health probe: degraded") {
		t.Fatalf("expected degraded, got %q", out)
	}
	if !strings.Contains(out, "daemon.stderr.log") {
		t.Fatalf("expected troubleshooting hint, got %q", out)
	}
}

func TestT68_DefaultHealthProbe_TimeoutReturnsStatus(t *testing.T) {
	// Hit an unroutable port to force the timeout path. Keep the timeout
	// tiny so the test stays fast.
	ok, status := defaultHealthProbe("http://127.0.0.1:1/health", 50*time.Millisecond)
	if ok {
		t.Fatal("expected timeout, got ok")
	}
	if status == "" {
		t.Fatal("status must be non-empty on failure")
	}
}

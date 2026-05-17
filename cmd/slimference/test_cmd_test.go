package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
)

func TestTestUpstream_ok(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	old := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw
	testUpstream("Anthropic", srv.URL)
	testUpstream("OpenAI", srv.URL)
	_ = pw.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, pr)
	out := buf.String()
	if strings.Count(out, "OK - HTTP 200") != 2 {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleTestCmd_upstreamAndMinimax(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	for _, sub := range []string{"anthropic", "openai"} {
		old := os.Stdout
		pr, pw, _ := os.Pipe()
		os.Stdout = pw
		handleTestCmd([]string{sub})
		_ = pw.Close()
		os.Stdout = old
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, pr)
		if !strings.Contains(buf.String(), "OK - HTTP 200") {
			t.Fatalf("%s: %q", sub, buf.String())
		}
	}

}

func TestTestIntercept_claudeParked(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { testIntercept(cfg, "claude") })
	cleanup()
	var stderr bytes.Buffer
	_, _ = io.Copy(&stderr, rp)
	if !exited || code != 2 || !strings.Contains(stderr.String(), "Claude Code is parked") {
		t.Fatalf("exit=(%d,%v) stderr=%q", code, exited, stderr.String())
	}
}

func TestTestIntercept_codex(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		testIntercept(cfg, "codex")
		close(done)
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	ok := false
	for range 100 {
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", "slimference-test-intercept")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "sk-test")
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ok = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !ok {
		t.Fatal("intercept server did not respond with 200")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("testIntercept did not finish")
	}
}

func TestHandleSubcommand_testUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_TEST_USAGE") == "1" {
		handleSubcommand([]string{"test"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_testUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_TEST_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_testUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_TEST_BAD") == "1" {
		handleSubcommand([]string{"test", "not-a-subcommand"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_testUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_TEST_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_testInterceptUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_TEST_ICPT") == "1" {
		handleSubcommand([]string{"test", "intercept"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_testInterceptUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_TEST_ICPT=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestTestUpstream_connRefusedExits1 covers testUpstream error path (main.go:499-502).
func TestTestUpstream_connRefusedExits1(t *testing.T) {
	if os.Getenv("TP_UPSTREAM_FAIL") == "1" {
		testUpstream("Test", "http://127.0.0.1:1")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestTestUpstream_connRefusedExits1")
	cmd.Env = append(os.Environ(), "TP_UPSTREAM_FAIL=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestTestMiniMax_connRefusedExits1 covers testMiniMax error path (main.go:516-519).

// TestHandleTestCmd_configLoadErrorExits1 covers handleTestCmd config load error (main.go:471-474).
func TestHandleTestCmd_configLoadErrorExits1(t *testing.T) {
	if os.Getenv("TP_TESTCMD_CFG_BAD") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", os.Getenv("TP_BAD_CFG_FILE"))
		handleTestCmd([]string{"anthropic"})
		return
	}
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badPath, []byte("not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleTestCmd_configLoadErrorExits1")
	cmd.Env = append(os.Environ(), "TP_TESTCMD_CFG_BAD=1", "TP_BAD_CFG_FILE="+badPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleTestCmd_interceptCallsTestIntercept covers the testIntercept call from handleTestCmd
// (main.go:488) via the intercept subcommand path. Uses an ephemeral listen port.
func TestHandleTestCmd_interceptCallsTestIntercept(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	done := make(chan struct{})
	go func() {
		handleTestCmd([]string{"intercept", "codex"})
		close(done)
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	ok := false
	for range 100 {
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ok = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !ok {
		t.Fatal("handleTestCmd intercept: server did not respond with 200")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handleTestCmd intercept did not finish")
	}
}

// TestTestIntercept_codexProvider covers the "codex" provider branch in testIntercept.
func TestTestIntercept_codexProvider(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	done := make(chan struct{})
	go func() {
		handleTestCmd([]string{"intercept", "codex"})
		close(done)
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	var ok bool
	for range 100 {
		req, reqErr := http.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := client.Do(req)
		if doErr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ok = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !ok {
		t.Fatal("codex intercept: server did not respond with 200")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("codex intercept did not finish")
	}
}

// TestTestIntercept_timeout covers the timeout exit in testIntercept() (main.go:583-590).
// testInterceptTimeout is set to 1ms so the case fires immediately.
func TestTestIntercept_timeout(t *testing.T) {
	origTimeout := testInterceptTimeout
	defer func() { testInterceptTimeout = origTimeout }()
	testInterceptTimeout = time.Millisecond

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("SLIMFERENCE_LISTEN_PORT", strconv.Itoa(port))

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	defer func() { os.Stdout = old }()

	code, exited := captureExit(func() {
		testIntercept(cfg, "codex")
	})
	_ = wp.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "FAIL") {
		t.Fatalf("stdout: %q", buf.String())
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
)

type bypassRTFunc func(*http.Request) (*http.Response, error)

func (f bypassRTFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubBypassAdminServer(t *testing.T, state *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(proxy.AdminBypassResponse{Enabled: *state})
		case http.MethodPost:
			var req proxy.AdminBypassRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			*state = req.Enabled
			_ = json.NewEncoder(w).Encode(proxy.AdminBypassResponse{Enabled: *state})
		}
	}))
}

func captureStdoutBypass(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestHandleBypassCmd_MissingVerbExits1(t *testing.T) {
	origExit := exitFn
	defer func() { exitFn = origExit }()
	var code int
	exitFn = func(c int) { code = c; panic(exitSentinel{}) }
	defer func() { _ = recover() }()
	handleBypassCmd(nil)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestHandleBypassCmd_UnknownVerbExits1(t *testing.T) {
	origExit := exitFn
	defer func() { exitFn = origExit }()
	var code int
	exitFn = func(c int) { code = c; panic(exitSentinel{}) }
	defer func() { _ = recover() }()
	handleBypassCmd([]string{"weird"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestHandleBypassCmd_OnOffStatusRoundTrip(t *testing.T) {
	state := false
	srv := stubBypassAdminServer(t, &state)
	defer srv.Close()
	origURL := bypassProxyURL
	bypassProxyURL = srv.URL
	defer func() { bypassProxyURL = origURL }()

	out := captureStdoutBypass(t, func() { handleBypassCmd([]string{"on"}) })
	if !strings.Contains(out, "bypass: on") {
		t.Fatalf("on output: %q", out)
	}
	if !state {
		t.Fatal("server state did not update to true")
	}
	out = captureStdoutBypass(t, func() { handleBypassCmd([]string{"status"}) })
	if !strings.Contains(out, "bypass: on") {
		t.Fatalf("status output: %q", out)
	}
	out = captureStdoutBypass(t, func() { handleBypassCmd([]string{"off"}) })
	if !strings.Contains(out, "bypass: off") {
		t.Fatalf("off output: %q", out)
	}
	if state {
		t.Fatal("server state did not update to false")
	}
}

func TestHandleBypassCmd_StatusUnreachableExits1(t *testing.T) {
	origURL := bypassProxyURL
	bypassProxyURL = "http://127.0.0.1:1"
	defer func() { bypassProxyURL = origURL }()
	origExit := exitFn
	defer func() { exitFn = origExit }()
	var code int
	exitFn = func(c int) { code = c; panic(exitSentinel{}) }
	defer func() { _ = recover() }()
	handleBypassCmd([]string{"status"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 on unreachable daemon", code)
	}
}

func TestHandleBypassCmd_OnUnreachableExits1(t *testing.T) {
	origURL := bypassProxyURL
	bypassProxyURL = "http://127.0.0.1:1"
	defer func() { bypassProxyURL = origURL }()
	origExit := exitFn
	defer func() { exitFn = origExit }()
	var code int
	exitFn = func(c int) { code = c; panic(exitSentinel{}) }
	defer func() { _ = recover() }()
	handleBypassCmd([]string{"on"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 on unreachable daemon", code)
	}
}

func TestPostBypass_ErrorPath(t *testing.T) {
	origClient := bypassHTTPClient
	defer func() { bypassHTTPClient = origClient }()
	bypassHTTPClient = &http.Client{
		Transport: bypassRTFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		}),
	}
	if postBypass(true) {
		t.Fatal("postBypass should return false on transport error")
	}
}

func TestPostBypass_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	origURL := bypassProxyURL
	bypassProxyURL = srv.URL
	defer func() { bypassProxyURL = origURL }()
	if postBypass(true) {
		t.Fatal("non-200 should yield false")
	}
}

func TestGetBypass_ErrorPath(t *testing.T) {
	origClient := bypassHTTPClient
	defer func() { bypassHTTPClient = origClient }()
	bypassHTTPClient = &http.Client{
		Transport: bypassRTFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		}),
	}
	if _, ok := getBypass(); ok {
		t.Fatal("expected ok=false on transport error")
	}
}

func TestGetBypass_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	origURL := bypassProxyURL
	bypassProxyURL = srv.URL
	defer func() { bypassProxyURL = origURL }()
	if _, ok := getBypass(); ok {
		t.Fatal("expected ok=false on non-200")
	}
}

func TestProxyAdapter_BypassRoundTrip(t *testing.T) {
	// Covers proxyAdapter.Bypass + SetBypass wrappers via a real Proxy.
	// We intentionally do NOT call p.Shutdown here - the proxy has not been
	// started (no listener, no workers) so there is nothing to stop.
	cfg := config.Defaults()
	p := proxy.New(cfg)
	a := &proxyAdapter{p: p}
	if a.Bypass() {
		t.Fatal("fresh proxy should not be bypassing")
	}
	a.SetBypass(true)
	if !a.Bypass() {
		t.Fatal("SetBypass(true) not reflected")
	}
	a.SetBypass(false)
	if a.Bypass() {
		t.Fatal("SetBypass(false) not reflected")
	}
}

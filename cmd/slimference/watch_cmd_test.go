package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
)

func TestParseWatchArgs_Defaults(t *testing.T) {

	f, err := parseWatchArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.intervalSeconds != 2 || f.once {
		t.Fatalf("default flags: %+v", f)
	}
}

func TestParseWatchArgs_AllFlags(t *testing.T) {

	f, err := parseWatchArgs([]string{"--once", "--interval", "5", "--endpoint", "http://1.2.3.4:8990"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.once || f.intervalSeconds != 5 || f.endpoint == "" {
		t.Fatalf("flags: %+v", f)
	}
}

func TestParseWatchArgs_Errors(t *testing.T) {

	cases := [][]string{
		{"--unknown"},
		{"--interval"},
		{"--interval", "not-a-number"},
		{"--interval", "0"},
		{"--endpoint"},
		{"unexpected-positional"},
	}
	for _, c := range cases {
		if _, err := parseWatchArgs(c); err == nil {
			t.Fatalf("expected error on %v", c)
		}
	}
}

func TestParseWatchArgs_EmptySkipped(t *testing.T) {

	f, err := parseWatchArgs([]string{"", "--once", ""})
	if err != nil {
		t.Fatal(err)
	}
	if !f.once {
		t.Fatal("once must register")
	}
}

func TestParsePositiveSeconds(t *testing.T) {

	if n, err := parsePositiveSeconds("3"); err != nil || n != 3 {
		t.Fatalf("3 -> %d %v", n, err)
	}
	if _, err := parsePositiveSeconds("-1"); err == nil {
		t.Fatal("negative must error")
	}
	if _, err := parsePositiveSeconds("abc"); err == nil {
		t.Fatal("non-numeric must error")
	}
}

func TestWatchAdminEndpoint_DefaultPort(t *testing.T) {

	url := watchAdminEndpoint(nil, "")
	if !strings.Contains(url, "127.0.0.1:8990") {
		t.Fatalf("default endpoint missing port: %s", url)
	}
}

func TestWatchAdminEndpoint_ConfigPort(t *testing.T) {

	cfg := config.Defaults()
	cfg.Proxy.ListenPort = 9999
	url := watchAdminEndpoint(cfg, "")
	if !strings.Contains(url, ":9999") {
		t.Fatalf("config port missing: %s", url)
	}
}

func TestWatchAdminEndpoint_OverrideTrimsTrailingSlash(t *testing.T) {

	url := watchAdminEndpoint(nil, "http://example/")
	if strings.Contains(url, "//_") {
		t.Fatalf("double slash not handled: %s", url)
	}
	if !strings.Contains(url, "/_slimference/admin/status") {
		t.Fatalf("missing path: %s", url)
	}
}

func TestFetchAdminStatus_Success(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"bypass":false,"layers":{"1":true,"2":true,"3":true}}`))
	}))
	defer srv.Close()

	body, err := fetchAdminStatus(context.Background(), &http.Client{}, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "bypass") {
		t.Fatalf("body: %s", string(body))
	}
}

func TestFetchAdminStatus_NonOK(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := fetchAdminStatus(context.Background(), &http.Client{}, srv.URL); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestFetchAdminStatus_NetworkError(t *testing.T) {

	if _, err := fetchAdminStatus(context.Background(), &http.Client{Timeout: time.Millisecond}, "http://127.0.0.1:1/x"); err == nil {
		t.Fatal("expected network error")
	}
}

func TestFetchAdminStatus_BadURL(t *testing.T) {

	if _, err := fetchAdminStatus(context.Background(), &http.Client{}, "::not a url"); err == nil {
		t.Fatal("expected request creation error")
	}
}

func TestRenderWatchTick_HappyPath(t *testing.T) {

	body := []byte(`{
		"bypass": false,
		"any_provider_degraded": true,
		"layers": {"1": true, "2": false, "3": true},
		"cache_entries": 7,
		"analytics_queue": {"depth": 3},
		"recent_requests": [{"provider": "anthropic", "tokens_saved": 50, "compression_ratio": 0.5}]
	}`)
	got := renderWatchTick(time.Now(), body)
	for _, want := range []string{"PROVIDER DEGRADED", "L1,L2", "cache=7", "queue=3", "anthropic"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderWatchTick_Bypass(t *testing.T) {

	body := []byte(`{"bypass": true, "layers": {}}`)
	got := renderWatchTick(time.Now(), body)
	if !strings.Contains(got, "BYPASS ON") {
		t.Fatalf("bypass missing: %s", got)
	}
}

func TestRenderWatchTick_BadJSON(t *testing.T) {

	got := renderWatchTick(time.Now(), []byte("not json"))
	if !strings.Contains(got, "parse error") {
		t.Fatalf("expected parse error: %s", got)
	}
}

func TestHandleWatchCmd_Once(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"bypass":false,"layers":{"1":true}}`))
	}))
	defer srv.Close()

	origStdout := os.Stdout
	origCfg := configLoadFn
	defer func() {
		os.Stdout = origStdout
		configLoadFn = origCfg
	}()
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleWatchCmd([]string{"--once", "--endpoint", srv.URL})
	_ = w.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "layers=L1") {
		t.Fatalf("output: %q", buf.String())
	}
}

func TestHandleWatchCmd_BadFlag(t *testing.T) {
	origExit := exitFn
	origStderr := os.Stderr
	defer func() {
		exitFn = origExit
		os.Stderr = origStderr
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		_ = w.Close()
		_, _ = io.Copy(io.Discard, r)
	}()
	handleWatchCmd([]string{"--bogus"})
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

func TestHandleWatchCmd_ConfigError(t *testing.T) {
	origExit := exitFn
	origCfg := configLoadFn
	defer func() {
		exitFn = origExit
		configLoadFn = origCfg
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	configLoadFn = func() (*config.Config, error) { return nil, io.ErrUnexpectedEOF }
	handleWatchCmd([]string{"--once"})
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

func TestRunWatchLoop_TicksUntilCancel(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"bypass":false,"layers":{"1":true}}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	var stdout, stderr bytes.Buffer
	// Use 1-second interval so the loop ticks at least once before ctx
	// times out.
	runWatchLoop(ctx, &http.Client{}, srv.URL, 1, false, &stdout, &stderr)
	if !strings.Contains(stdout.String(), "layers=L1") {
		t.Fatalf("loop output: %s", stdout.String())
	}
}

func TestHandleSubcommand_WatchDispatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"bypass":false,"layers":{"1":true}}`))
	}))
	defer srv.Close()

	origStdout := os.Stdout
	origCfg := configLoadFn
	defer func() {
		os.Stdout = origStdout
		configLoadFn = origCfg
	}()
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"watch", "--once", "--endpoint", srv.URL})
	_ = w.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "layers=L1") {
		t.Fatalf("dispatch output: %q", buf.String())
	}
}

func TestHandleWatchCmd_FetchError(t *testing.T) {
	origStdout := os.Stdout
	origCfg := configLoadFn
	defer func() {
		os.Stdout = origStdout
		configLoadFn = origCfg
	}()
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = origStderr
		_ = w.Close()
		_, _ = io.Copy(io.Discard, r)
	}()
	handleWatchCmd([]string{"--once", "--endpoint", "http://127.0.0.1:1"})
	// fetch error logged to stderr; command does NOT exit non-zero (fetch
	// errors are recoverable in the polling loop), so just verify the
	// call returns without panic.
}

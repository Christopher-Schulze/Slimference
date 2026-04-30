package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParseBypassFlags_Defaults(t *testing.T) {
	d, n, err := parseBypassFlags(nil)
	if err != nil || d != 0 || n != 0 {
		t.Fatalf("defaults: %d %d %v", d, n, err)
	}
}

func TestParseBypassFlags_DurationFormats(t *testing.T) {
	cases := map[string]int{
		"--duration=30":  30,
		"--duration=2m":  120,
		"--duration=1h":  3600,
		"--duration=45s": 45,
	}
	for in, want := range cases {
		d, _, err := parseBypassFlags([]string{in})
		if err != nil || d != want {
			t.Fatalf("%s -> %d %v want %d", in, d, err, want)
		}
	}
}

func TestParseBypassFlags_NextRequest(t *testing.T) {
	_, n, err := parseBypassFlags([]string{"--next-request"})
	if err != nil || n != 1 {
		t.Fatalf("--next-request: %d %v", n, err)
	}
	_, n, err = parseBypassFlags([]string{"--next-request=4"})
	if err != nil || n != 4 {
		t.Fatalf("--next-request=4: %d %v", n, err)
	}
}

func TestParseBypassFlags_Errors(t *testing.T) {
	cases := [][]string{
		{"--duration=0"},
		{"--duration="},
		{"--duration=abc"},
		{"--next-request=0"},
		{"--next-request=-2"},
		{"--bogus"},
		{"--duration=10", "--next-request=5"},
	}
	for _, in := range cases {
		if _, _, err := parseBypassFlags(in); err == nil {
			t.Fatalf("expected error for %v", in)
		}
	}
}

func TestParseBypassFlags_EmptyArgsSkipped(t *testing.T) {
	d, _, err := parseBypassFlags([]string{"", "--duration=5"})
	if err != nil || d != 5 {
		t.Fatalf("empty skip: %d %v", d, err)
	}
}

func TestHandleBypassCmd_OnWithDuration(t *testing.T) {
	captured := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case captured <- body:
		default:
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"enabled":true,"expires_at_unix":12345}`))
	}))
	defer srv.Close()
	prevURL := bypassProxyURL
	prevClient := bypassHTTPClient
	t.Cleanup(func() {
		bypassProxyURL = prevURL
		bypassHTTPClient = prevClient
	})
	bypassProxyURL = srv.URL
	bypassHTTPClient = srv.Client()

	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleBypassCmd([]string{"on", "--duration=10s"})
	_ = w.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "reverts after 10s") {
		t.Fatalf("output: %q", buf.String())
	}
	body := <-captured
	if !strings.Contains(string(body), `"duration_seconds":10`) {
		t.Fatalf("payload missing duration: %s", string(body))
	}
}

func TestHandleBypassCmd_OnWithNextRequest(t *testing.T) {
	captured := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case captured <- body:
		default:
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"enabled":true,"next_request_budget":3}`))
	}))
	defer srv.Close()
	prevURL := bypassProxyURL
	prevClient := bypassHTTPClient
	t.Cleanup(func() {
		bypassProxyURL = prevURL
		bypassHTTPClient = prevClient
	})
	bypassProxyURL = srv.URL
	bypassHTTPClient = srv.Client()

	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleBypassCmd([]string{"on", "--next-request=3"})
	_ = w.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "reverts after 3 request") {
		t.Fatalf("output: %q", buf.String())
	}
}

func TestParseDurationSeconds_EmptyError(t *testing.T) {
	if _, err := parseDurationSeconds(""); err == nil {
		t.Fatal("empty must error")
	}
}

func TestHandleBypassCmd_BadFlag(t *testing.T) {
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
	handleBypassCmd([]string{"on", "--bogus"})
	_ = w.Close()
	os.Stderr = origStderr
	_, _ = io.Copy(io.Discard, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

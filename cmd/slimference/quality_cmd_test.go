package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
)

func TestParseQualityArgs_Defaults(t *testing.T) {
	t.Parallel()
	f, err := parseQualityArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.json || f.url != "" {
		t.Fatalf("defaults wrong: %+v", f)
	}
	if f.timeout <= 0 {
		t.Fatalf("timeout default zero: %v", f.timeout)
	}
}

func TestParseQualityArgs_FlagsRoundTrip(t *testing.T) {
	t.Parallel()
	f, err := parseQualityArgs([]string{"--json", "--url", "http://x:1"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.json || f.url != "http://x:1" {
		t.Fatalf("flags not parsed: %+v", f)
	}
}

func TestParseQualityArgs_UrlMissingValue(t *testing.T) {
	t.Parallel()
	if _, err := parseQualityArgs([]string{"--url"}); err == nil {
		t.Fatal("expected error for --url without value")
	}
	if _, err := parseQualityArgs([]string{"--url", ""}); err == nil {
		t.Fatal("expected error for --url with empty value")
	}
}

func TestParseQualityArgs_UnknownFlag(t *testing.T) {
	t.Parallel()
	if _, err := parseQualityArgs([]string{"--bogus"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestParseQualityArgs_UnexpectedPositional(t *testing.T) {
	t.Parallel()
	if _, err := parseQualityArgs([]string{"today"}); err == nil {
		t.Fatal("expected error for positional arg")
	}
}

func TestResolveQualityURL(t *testing.T) {
	t.Parallel()
	if got := resolveQualityURL(nil, ""); got != "http://127.0.0.1:8990/_slimference/admin/status" {
		t.Fatalf("nil cfg fallback: %s", got)
	}
	cfg := &config.Config{Proxy: config.ProxyConfig{ListenPort: 9999}}
	if got := resolveQualityURL(cfg, ""); !strings.Contains(got, "9999") {
		t.Fatalf("port from config: %s", got)
	}
	if got := resolveQualityURL(nil, "http://example.test:5/"); got != "http://example.test:5/_slimference/admin/status" {
		t.Fatalf("override: %s", got)
	}
}

func TestFetchQualityBlock_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"quality":{"reread":{"sessions":2,"reread_count":3,"reread_rate":0.15},"cache_miss_spike":{"window_hit_rate":0.6,"spike_active":false},"net_savings":{"net_saved_tokens":2000,"net_savings_ratio":0.42}}}`)
	}))
	defer srv.Close()
	q, err := fetchQualityBlock(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if q == nil || q["reread"] == nil {
		t.Fatalf("expected reread sub-block, got %+v", q)
	}
}

func TestFetchQualityBlock_Non2xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := fetchQualityBlock(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected error on 5xx")
	}
}

func TestFetchQualityBlock_BadJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{not json`)
	}))
	defer srv.Close()
	if _, err := fetchQualityBlock(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestFetchQualityBlock_BadURL(t *testing.T) {
	t.Parallel()
	if _, err := fetchQualityBlock(context.Background(), http.DefaultClient, ":://bad"); err == nil {
		t.Fatal("expected URL error")
	}
}

func TestFetchQualityBlock_DialFail(t *testing.T) {
	t.Parallel()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	if _, err := fetchQualityBlock(context.Background(), client, "http://127.0.0.1:1"); err == nil {
		t.Fatal("expected dial error")
	}
}

// stubBodyReader simulates a network read failure mid-body.
type stubBodyReader struct{}

func (stubBodyReader) Read(_ []byte) (int, error) { return 0, errors.New("boom") }
func (stubBodyReader) Close() error               { return nil }

type stubRT struct{}

func (stubRT) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       stubBodyReader{},
		Header:     make(http.Header),
	}, nil
}

func TestFetchQualityBlock_BodyReadError(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: stubRT{}}
	if _, err := fetchQualityBlock(context.Background(), client, "http://test/"); err == nil {
		t.Fatal("expected body read error")
	}
}

func TestRenderQualityText_Nil(t *testing.T) {
	t.Parallel()
	out := renderQualityText(nil)
	if !strings.Contains(out, "no quality data") {
		t.Fatalf("nil output: %s", out)
	}
}

func TestRenderQualityText_Populated(t *testing.T) {
	t.Parallel()
	q := map[string]any{
		"reread": map[string]any{
			"sessions":     float64(3),
			"reread_count": float64(5),
			"reread_rate":  0.4,
		},
		"cache_miss_spike": map[string]any{
			"window_hit_rate": 0.7,
			"spike_active":    true,
			"last_spike_unix": float64(1234),
		},
		"net_savings": map[string]any{
			"net_saved_tokens":  float64(900),
			"net_savings_ratio": 0.55,
		},
	}
	out := renderQualityText(q)
	for _, want := range []string{"Re-read sessions", "Cache hit rate", "Net savings", "0.4000", "0.5500"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output: %s", want, out)
		}
	}
}

func TestFormatFloat(t *testing.T) {
	t.Parallel()
	if got := formatFloat(0.5); got != "0.5000" {
		t.Fatalf("float: %s", got)
	}
	if got := formatFloat("oops"); got != "oops" {
		t.Fatalf("string passthrough: %s", got)
	}
}

func TestHandleQualityCmd_BadFlag(t *testing.T) {
	origExit := exitFn
	t.Cleanup(func() { exitFn = origExit })
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }

	stderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	handleQualityCmd([]string{"--bogus"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit(1), got %v", exits)
	}
	if !strings.Contains(buf.String(), "unknown flag") {
		t.Fatalf("stderr: %s", buf.String())
	}
}

func TestHandleQualityCmd_FetchFails(t *testing.T) {
	origExit := exitFn
	origLoad := configLoadFn
	t.Cleanup(func() {
		exitFn = origExit
		configLoadFn = origLoad
	})
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }

	stderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	// Point to an unbound port so the fetch fails fast.
	handleQualityCmd([]string{"--url", "http://127.0.0.1:1"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit(1), got %v", exits)
	}
	if !strings.Contains(buf.String(), "fetch quality") {
		t.Fatalf("stderr missing fetch error: %s", buf.String())
	}
}

func TestHandleQualityCmd_TextOutput(t *testing.T) {
	origLoad := configLoadFn
	t.Cleanup(func() { configLoadFn = origLoad })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"quality":{"reread":{"sessions":1,"reread_count":2,"reread_rate":0.5}}}`)
	}))
	defer srv.Close()

	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }

	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	handleQualityCmd([]string{"--url", srv.URL})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Re-read sessions") {
		t.Fatalf("text output: %s", buf.String())
	}
}

// TestHandleSubcommand_qualityDispatch covers the case "quality" branch
// in main.go::handleSubcommand.
func TestHandleSubcommand_qualityDispatch(t *testing.T) {
	origExit := exitFn
	origLoad := configLoadFn
	t.Cleanup(func() {
		exitFn = origExit
		configLoadFn = origLoad
	})
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	exitFn = func(int) {}

	stderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	handleSubcommand([]string{"quality", "--url", "http://127.0.0.1:1"})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "fetch quality") {
		t.Fatalf("dispatched but did not run fetch path: %s", buf.String())
	}
}

func TestHandleQualityCmd_JSONOutput(t *testing.T) {
	origLoad := configLoadFn
	t.Cleanup(func() { configLoadFn = origLoad })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"quality":{"reread":{"sessions":4}}}`)
	}))
	defer srv.Close()

	configLoadFn = func() (*config.Config, error) { return nil, errors.New("ignored") }

	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	handleQualityCmd([]string{"--json", "--url", srv.URL})
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"sessions"`) {
		t.Fatalf("json output: %s", buf.String())
	}
}

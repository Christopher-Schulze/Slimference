//go:build integration

package integration_test

// Fail-open integration tests for t164. These exercise the
// slimference-sidecar binary with a real engine socket (or no engine
// at all) to prove that the wiring promise survives daemon death.
//
// Setup pattern:
//
//	1. Build cmd/slimference-sidecar into a tempdir.
//	2. Spin a httptest upstream that records request bodies + headers.
//	3. Either spin a fake engine (Unix socket speaking hookproto) or
//	   point the sidecar at a path that doesn't exist (engine-down).
//	4. Launch sidecar with --addr 127.0.0.1:<random> + --engine-socket
//	   set to the fake/missing socket.
//	5. Send Anthropic-shape and Codex-shape requests through the sidecar.
//	6. Assert: upstream observed exactly what we expected (mutated when
//	   engine alive, verbatim when engine dead). Auth header passed
//	   through in both cases.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/daemon/hookproto"
)

func TestFailOpen_EngineDown_PassthroughAnthropic(t *testing.T) {
	upstream, upstreamGotBody, upstreamGotAuth := newRecordingUpstream(t, http.StatusOK, `{"ok":true}`)
	defer upstream.Close()

	sidecarBin := buildSidecar(t)
	addr := freeLoopbackAddr(t)
	missingSock := filepath.Join(shortTmpDir(t), "absent.sock")

	cmd := exec.Command(sidecarBin,
		"--addr", addr,
		"--engine-socket", missingSock,
		"--probe-interval", "1h",
		"--rpc-timeout", "30ms",
	)
	cmd.Env = appendUpstreamOverride(os.Environ(), upstream.URL)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sidecar: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	waitForListener(t, addr, 2*time.Second)

	body := `{"model":"claude-3-5-sonnet","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	resp := postJSON(t, "http://"+addr+"/v1/messages", body, http.Header{"Authorization": []string{"Bearer test"}})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if got := <-upstreamGotBody; got != body {
		t.Fatalf("upstream body drift on engine-down path: got %q want %q", got, body)
	}
	if got := <-upstreamGotAuth; got != "Bearer test" {
		t.Fatalf("auth header dropped: got %q", got)
	}
}

func TestFailOpen_EngineDown_PassthroughCodex(t *testing.T) {
	upstream, upstreamGotBody, upstreamGotAuth := newRecordingUpstream(t, http.StatusOK, `{}`)
	defer upstream.Close()

	sidecarBin := buildSidecar(t)
	addr := freeLoopbackAddr(t)
	missingSock := filepath.Join(shortTmpDir(t), "absent.sock")
	cmd := exec.Command(sidecarBin,
		"--addr", addr,
		"--engine-socket", missingSock,
		"--probe-interval", "1h",
		"--rpc-timeout", "30ms",
	)
	cmd.Env = appendUpstreamOverride(os.Environ(), upstream.URL)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sidecar: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	waitForListener(t, addr, 2*time.Second)

	body := `{"model":"o4","input":[{"type":"message","role":"user","content":"hi"}]}`
	resp := postJSON(t, "http://"+addr+"/backend-api/codex/responses", body, http.Header{
		"Authorization": []string{"Bearer codex-token"},
		"User-Agent":    []string{"codex-cli/0.5.2"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if got := <-upstreamGotBody; got != body {
		t.Fatalf("upstream body drift on engine-down codex path: got %q want %q", got, body)
	}
	if got := <-upstreamGotAuth; got != "Bearer codex-token" {
		t.Fatalf("auth dropped: got %q", got)
	}
}

func TestFailOpen_EngineKilledMidSession_NextRequestDegrades(t *testing.T) {
	upstream, upstreamGotBody, _ := newRecordingUpstream(t, http.StatusOK, `{"ok":true}`)
	defer upstream.Close()

	enginePath, engineStop := startFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		if env.Op == hookproto.OpPing {
			resp.Response = &hookproto.Response{Ping: &hookproto.PingResponse{Healthy: true}}
			return resp
		}
		// Engine mutates the body to a known marker.
		resp.Response = &hookproto.Response{ForwardRequest: &hookproto.ForwardRequestResponse{
			Body: []byte(`{"compacted":true}`),
		}}
		return resp
	})

	sidecarBin := buildSidecar(t)
	addr := freeLoopbackAddr(t)
	cmd := exec.Command(sidecarBin,
		"--addr", addr,
		"--engine-socket", enginePath,
		"--probe-interval", "100ms",
		"--rpc-timeout", "100ms",
	)
	cmd.Env = appendUpstreamOverride(os.Environ(), upstream.URL)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sidecar: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	waitForListener(t, addr, 2*time.Second)
	// Wait for first probe to flip engineHealthy=true.
	time.Sleep(200 * time.Millisecond)

	// First request: engine alive, body mutated.
	orig := `{"original":true}`
	resp1 := postJSON(t, "http://"+addr+"/v1/messages", orig, http.Header{"Authorization": []string{"Bearer x"}})
	resp1.Body.Close()
	if got := <-upstreamGotBody; got != `{"compacted":true}` {
		t.Fatalf("engine mutation lost first time: got %q", got)
	}

	// Kill engine mid-session.
	engineStop()

	// Wait one probe cycle so the sidecar marks engine unhealthy.
	time.Sleep(300 * time.Millisecond)

	// Second request: engine dead, sidecar must passthrough verbatim.
	resp2 := postJSON(t, "http://"+addr+"/v1/messages", orig, http.Header{"Authorization": []string{"Bearer x"}})
	resp2.Body.Close()
	if got := <-upstreamGotBody; got != orig {
		t.Fatalf("engine-kill degradation failed: got %q want %q", got, orig)
	}
}

// ---------------- helpers ----------------

// newRecordingUpstream returns an httptest.Server that captures every
// request's body + Authorization header into channels. Cap = 8 so a
// short test does not block.
func newRecordingUpstream(t *testing.T, status int, respBody string) (*httptest.Server, <-chan string, <-chan string) {
	t.Helper()
	bodies := make(chan string, 8)
	auths := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		bodies <- string(buf)
		auths <- r.Header.Get("Authorization")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	return srv, bodies, auths
}

// buildSidecar compiles the slimference-sidecar binary into a tempdir
// once per test run and returns the path. The build is cached across
// subtests within the same `go test -run` invocation via t.TempDir's
// parent process scope (each test gets its own dir, but go test caches
// build output via the module cache).
func buildSidecar(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "slsc")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	bin := filepath.Join(dir, "slimference-sidecar")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/Christopher-Schulze/Slimference/cmd/slimference-sidecar")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build sidecar: %v", err)
	}
	return bin
}

// shortTmpDir is a flat /tmp-rooted directory whose path stays under the
// macOS sun_path 104-byte limit. Test names embedded in t.TempDir paths
// can overflow when combined with the Unix-socket filename.
func shortTmpDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "fo")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// freeLoopbackAddr picks a random free loopback port and returns
// "127.0.0.1:PORT". The port may race with another listener before the
// sidecar binds; tests tolerate this via waitForListener.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// waitForListener polls TCP connect against addr until success or
// timeout. The sidecar binds asynchronously after launch.
func waitForListener(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("sidecar did not bind %s within %s", addr, timeout)
}

// postJSON sends a POST with body+headers and returns the response.
func postJSON(t *testing.T, url, body string, headers http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

// startFakeEngine spins a Unix-socket server that responds to hookproto
// envelopes via the provided function. Returns the socket path and a
// stop function. The stop function closes the listener AND any active
// connections so a kill-mid-session scenario is reproducible.
func startFakeEngine(t *testing.T, respond func(env hookproto.Envelope) hookproto.Envelope) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "fe")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sock := filepath.Join(dir, fmt.Sprintf("e%d.sock", time.Now().UnixNano()%1000000))
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	var conns sync.Map // active conns for forceful close on stop
	closed := atomic.Bool{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Store(conn, struct{}{})
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					env, err := hookproto.Decode(r)
					if err != nil {
						conns.Delete(c)
						return
					}
					if closed.Load() {
						return
					}
					if err := hookproto.Encode(c, respond(env)); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	stop := func() {
		closed.Store(true)
		_ = ln.Close()
		conns.Range(func(key, _ any) bool {
			_ = key.(net.Conn).Close()
			return true
		})
		_ = os.RemoveAll(dir)
	}
	t.Cleanup(stop)
	return sock, stop
}

// appendUpstreamOverride lets the sidecar route every provider to a
// test-controlled upstream by passing well-known SLIMFERENCE_UPSTREAM_*
// env vars. Today the sidecar binary does not honour these (default
// bases are hardcoded); this helper documents the future hook for when
// the sidecar gains config support. Until then, tests rely on the
// reverse-proxy receiving the upstream URL via the full request URL.
func appendUpstreamOverride(env []string, upstreamURL string) []string {
	return append(env,
		"SLIMFERENCE_UPSTREAM_ANTHROPIC="+upstreamURL,
		"SLIMFERENCE_UPSTREAM_OPENAI="+upstreamURL,
		"SLIMFERENCE_UPSTREAM_CODEX="+upstreamURL,
	)
}

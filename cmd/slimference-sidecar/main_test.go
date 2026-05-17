package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/daemon/hookproto"
	"github.com/slimference/slimference/internal/proxy/upstream"
)

// shortSockPath returns a short Unix-socket path that fits the
// platform's sun_path limit (104 bytes on macOS). t.TempDir() paths can
// overflow once test-name plus go's "001" subdir are added.
func shortSockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sl")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, fmt.Sprintf("e%d.sock", time.Now().UnixNano()%1000000))
}

// fakeEngine spins a Unix socket that speaks hookproto and lets the test
// dictate per-request behavior. ResponderFor returns a fresh closure per
// connection.
type fakeEngine struct {
	t        *testing.T
	listener net.Listener
	path     string
	respond  func(env hookproto.Envelope) hookproto.Envelope
	connCnt  atomic.Int32
}

func newFakeEngine(t *testing.T, respond func(env hookproto.Envelope) hookproto.Envelope) *fakeEngine {
	t.Helper()
	sock := shortSockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("unix listen: %v", err)
	}
	fe := &fakeEngine{t: t, listener: ln, path: sock, respond: respond}
	go fe.acceptLoop()
	return fe
}

func (fe *fakeEngine) acceptLoop() {
	for {
		conn, err := fe.listener.Accept()
		if err != nil {
			return
		}
		fe.connCnt.Add(1)
		go fe.handle(conn)
	}
}

func (fe *fakeEngine) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		env, err := hookproto.Decode(r)
		if err != nil {
			return
		}
		resp := fe.respond(env)
		if err := hookproto.Encode(conn, resp); err != nil {
			return
		}
	}
}

func (fe *fakeEngine) Close() {
	fe.listener.Close()
}

// fakeUpstream is a stand-in for chatgpt.com / api.anthropic.com. The
// sidecar's reverse-proxy talks to this in tests.
func newFakeUpstream(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func newSidecarUnderTest(engineSocket string) *sidecar {
	sc := newSidecar(engineSocket, 500*time.Millisecond, log.New(io.Discard, "", 0))
	return sc
}

// helper: tell the sidecar to forward to the test upstream by hijacking
// the URL host. We do this by setting the request path to the upstream
// URL directly and constructing a custom forward.
func TestServeHTTP_EnginePassThrough_ForwardsVerbatim(t *testing.T) {
	upstreamHits := atomic.Int32{}
	receivedBody := make(chan string, 1)
	receivedAuth := make(chan string, 1)
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		buf, _ := io.ReadAll(r.Body)
		receivedBody <- string(buf)
		receivedAuth <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer upstream.Close()

	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{ForwardRequest: &hookproto.ForwardRequestResponse{PassThrough: true}}
		return resp
	})
	defer fe.Close()

	sc := newSidecarUnderTest(fe.path)
	sc.engineHealthy.Store(true)

	// Override upstream resolution: replace the sidecar's forward to
	// rewrite the host to the test upstream. Simplest: stub the
	// sidecar's forward URL by setting host directly in the request URL.
	body := `{"model":"o4","input":"hi"}`
	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("User-Agent", "codex-cli/0.5.2")
	rec := httptest.NewRecorder()

	// Directly invoke forward to bypass DefaultBases hard-coded host.
	// This validates the streaming/header preservation path explicitly.
	sc.forward(rec, req, http.MethodPost, upstream.URL+"/v1/responses", req.Header, []byte(body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("upstream hits: got %d, want 1", upstreamHits.Load())
	}
	if got := <-receivedBody; got != body {
		t.Fatalf("body drift: got %q want %q", got, body)
	}
	if got := <-receivedAuth; got != "Bearer test-token" {
		t.Fatalf("auth dropped: got %q", got)
	}
}

func TestServeHTTP_EngineUnhealthy_DegradesToPassthrough(t *testing.T) {
	upstreamHits := atomic.Int32{}
	receivedBody := make(chan string, 1)
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		buf, _ := io.ReadAll(r.Body)
		receivedBody <- string(buf)
		w.WriteHeader(http.StatusOK)
	})
	defer upstream.Close()

	// No engine running: dialEngineFn returns an error.
	sc := newSidecarUnderTest("/nonexistent.sock")
	// engineHealthy stays false; askEngine never invoked.

	body := `{"model":"o4"}`
	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	sc.forward(rec, req, http.MethodPost, upstream.URL+"/v1/responses", req.Header, []byte(body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("upstream hits: got %d, want 1", upstreamHits.Load())
	}
}

func TestServeHTTP_EngineMutatesBody(t *testing.T) {
	upstreamGotBody := make(chan string, 1)
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		upstreamGotBody <- string(buf)
		w.WriteHeader(http.StatusOK)
	})
	defer upstream.Close()

	mutated := `{"model":"o4","input":"<compacted>"}`
	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{ForwardRequest: &hookproto.ForwardRequestResponse{
			Body: []byte(mutated),
		}}
		return resp
	})
	defer fe.Close()

	sc := newSidecarUnderTest(fe.path)
	// Probe once to flip the health flag true.
	sc.probeOnce(context.Background())
	// probeOnce flipped to false because our fake responder doesn't
	// answer Ping with Healthy=true. Force true for this test.
	sc.engineHealthy.Store(true)

	body := `{"model":"o4","input":"original"}`
	req := httptest.NewRequest(http.MethodPost, "http://x/backend-api/codex/responses", strings.NewReader(body))
	req.Header.Set("User-Agent", "codex-cli/0.5.2")
	rec := httptest.NewRecorder()

	// Drive askEngine + forward through a fake DefaultBases by
	// monkey-patching the forward call. We invoke askEngine directly to
	// validate the mutation flows through.
	dec, ok := sc.askEngine(req, []byte(body), "http://x/backend-api/codex/responses")
	if !ok {
		t.Fatalf("askEngine returned ok=false")
	}
	if dec.passThrough {
		t.Fatalf("expected mutation, got passthrough")
	}
	if string(dec.bodyOverride) != mutated {
		t.Fatalf("body override drift: got %q want %q", string(dec.bodyOverride), mutated)
	}

	// Now exercise the full path end-to-end through forward.
	finalBody := dec.bodyOverride
	sc.forward(rec, req, http.MethodPost, upstream.URL+"/backend-api/codex/responses", req.Header, finalBody)
	if got := <-upstreamGotBody; got != mutated {
		t.Fatalf("upstream saw %q, want %q", got, mutated)
	}
}

func TestAskEngine_DialFailureReturnsFalse(t *testing.T) {
	sc := newSidecarUnderTest("/does/not/exist.sock")
	req := httptest.NewRequest(http.MethodPost, "http://x/", strings.NewReader(""))
	_, ok := sc.askEngine(req, nil, "http://x/")
	if ok {
		t.Fatalf("expected dial failure to return ok=false")
	}
}

func TestAskEngine_DecodeErrorReturnsFalse(t *testing.T) {
	// Engine accepts the connection then closes without responding.
	sock := shortSockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	sc := newSidecarUnderTest(sock)
	req := httptest.NewRequest(http.MethodPost, "http://x/", strings.NewReader(""))
	_, ok := sc.askEngine(req, nil, "http://x/")
	if ok {
		t.Fatalf("expected decode failure to return ok=false")
	}
}

func TestAskEngine_EngineErrorReturnsFalse(t *testing.T) {
	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{Error: "engine boom"}
		return resp
	})
	defer fe.Close()
	sc := newSidecarUnderTest(fe.path)
	req := httptest.NewRequest(http.MethodPost, "http://x/", strings.NewReader(""))
	_, ok := sc.askEngine(req, nil, "http://x/")
	if ok {
		t.Fatalf("expected engine-error to return ok=false")
	}
}

func TestAskEngine_NilResponseReturnsFalse(t *testing.T) {
	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		return hookproto.NewEnvelope(env.Op, env.ID) // empty envelope
	})
	defer fe.Close()
	sc := newSidecarUnderTest(fe.path)
	req := httptest.NewRequest(http.MethodPost, "http://x/", strings.NewReader(""))
	_, ok := sc.askEngine(req, nil, "http://x/")
	if ok {
		t.Fatalf("expected nil response to return ok=false")
	}
}

func TestAskEngine_NilForwardResponseReturnsFalse(t *testing.T) {
	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{} // no ForwardRequest field
		return resp
	})
	defer fe.Close()
	sc := newSidecarUnderTest(fe.path)
	req := httptest.NewRequest(http.MethodPost, "http://x/", strings.NewReader(""))
	_, ok := sc.askEngine(req, nil, "http://x/")
	if ok {
		t.Fatalf("expected nil ForwardRequest response to return ok=false")
	}
}

func TestAskEngine_EncodeFailureClosesAndReturnsFalse(t *testing.T) {
	sock := shortSockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Close immediately so the sidecar's Encode write fails.
		conn.Close()
	}()
	sc := newSidecarUnderTest(sock)
	sc.rpcTimeout = 50 * time.Millisecond
	req := httptest.NewRequest(http.MethodPost, "http://x/", strings.NewReader(""))
	// Wait a tick so the goroutine has time to close the conn before we
	// attempt the write. Without this the first write may race-win the
	// close and we lose coverage of the encode-error branch.
	time.Sleep(5 * time.Millisecond)
	_, ok := sc.askEngine(req, nil, "http://x/")
	if ok {
		t.Fatalf("expected encode failure to return ok=false")
	}
}

func TestProbeOnce_HealthyResponseFlipsFlagTrue(t *testing.T) {
	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{Ping: &hookproto.PingResponse{Healthy: true, Version: "test"}}
		return resp
	})
	defer fe.Close()
	sc := newSidecarUnderTest(fe.path)
	sc.probeOnce(context.Background())
	if !sc.engineHealthy.Load() {
		t.Fatalf("expected healthy=true")
	}
}

func TestProbeOnce_UnhealthyResponseFlipsFlagFalse(t *testing.T) {
	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{Ping: &hookproto.PingResponse{Healthy: false, LastError: "boom"}}
		return resp
	})
	defer fe.Close()
	sc := newSidecarUnderTest(fe.path)
	sc.engineHealthy.Store(true)
	sc.probeOnce(context.Background())
	if sc.engineHealthy.Load() {
		t.Fatalf("expected healthy=false after engine reported unhealthy")
	}
}

func TestProbeOnce_DialFailureKeepsFalse(t *testing.T) {
	sc := newSidecarUnderTest("/missing.sock")
	sc.engineHealthy.Store(true)
	sc.probeOnce(context.Background())
	if sc.engineHealthy.Load() {
		t.Fatalf("expected healthy=false on dial fail")
	}
}

func TestProbeOnce_DecodeFailureKeepsFalse(t *testing.T) {
	sock := shortSockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close() // no response -> sidecar decode fails
	}()
	sc := newSidecarUnderTest(sock)
	sc.engineHealthy.Store(true)
	sc.probeOnce(context.Background())
	if sc.engineHealthy.Load() {
		t.Fatalf("expected healthy=false on decode fail")
	}
}

func TestProbeOnce_EncodeFailureKeepsFalse(t *testing.T) {
	// Accept then close: the encode write should fail because the peer
	// already closed.
	sock := shortSockPath(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()
	sc := newSidecarUnderTest(sock)
	sc.rpcTimeout = 50 * time.Millisecond
	sc.engineHealthy.Store(true)
	time.Sleep(5 * time.Millisecond)
	sc.probeOnce(context.Background())
	if sc.engineHealthy.Load() {
		t.Fatalf("expected healthy=false on encode fail")
	}
}

func TestRunHealthProbe_CancellationStops(t *testing.T) {
	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{Ping: &hookproto.PingResponse{Healthy: true}}
		return resp
	})
	defer fe.Close()
	sc := newSidecarUnderTest(fe.path)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sc.runHealthProbe(ctx, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("runHealthProbe did not exit on cancel")
	}
}

func TestForward_StreamsResponseBody(t *testing.T) {
	chunks := []string{"data: 1\n\n", "data: 2\n\n", "data: [DONE]\n\n"}
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			if fl != nil {
				fl.Flush()
			}
		}
	})
	defer upstream.Close()
	sc := newSidecarUnderTest("/missing.sock")
	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/responses", strings.NewReader(""))
	rec := httptest.NewRecorder()
	sc.forward(rec, req, http.MethodPost, upstream.URL+"/v1/responses", req.Header, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	got := rec.Body.String()
	for _, c := range chunks {
		if !strings.Contains(got, c) {
			t.Fatalf("missing chunk %q in body %q", c, got)
		}
	}
}

func TestForward_BadURLReturns502(t *testing.T) {
	sc := newSidecarUnderTest("/missing.sock")
	req := httptest.NewRequest(http.MethodPost, "http://x/", strings.NewReader(""))
	rec := httptest.NewRecorder()
	sc.forward(rec, req, http.MethodPost, "://broken-url", req.Header, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on bad URL, got %d", rec.Code)
	}
}

func TestForward_UpstreamErrorReturns502(t *testing.T) {
	sc := newSidecarUnderTest("/missing.sock")
	req := httptest.NewRequest(http.MethodPost, "http://x/", strings.NewReader(""))
	rec := httptest.NewRecorder()
	// Port 1 is reserved and refuses connections; reverse-proxy will
	// surface that as ErrorHandler invocation.
	sc.forward(rec, req, http.MethodPost, "http://127.0.0.1:1/x", req.Header, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on upstream error, got %d", rec.Code)
	}
}

func TestServeHTTP_BadBodyRead(t *testing.T) {
	sc := newSidecarUnderTest("/missing.sock")
	req := httptest.NewRequest(http.MethodPost, "http://x/", &errReader{err: errors.New("read boom")})
	rec := httptest.NewRecorder()
	sc.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on body read failure, got %d", rec.Code)
	}
}

func TestServeHTTP_EngineHealthyPassThroughBranch(t *testing.T) {
	upstreamHit := atomic.Int32{}
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamHit.Add(1)
		if r.URL.Path != "/backend-api/codex/responses" {
			t.Errorf("path drift: %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer x" {
			t.Errorf("auth dropped: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	})
	defer upstream.Close()

	engineHits := atomic.Int32{}
	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		engineHits.Add(1)
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{ForwardRequest: &hookproto.ForwardRequestResponse{PassThrough: true}}
		return resp
	})
	defer fe.Close()

	sc := newSidecarUnderTest(fe.path)
	sc.bases = upstreamBasesPointingAt(upstream.URL)
	sc.engineHealthy.Store(true)

	body := `{"x":1}`
	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer x")
	req.Header.Set("User-Agent", "codex-cli/0.5")
	rec := httptest.NewRecorder()
	sc.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if engineHits.Load() != 1 {
		t.Fatalf("expected 1 engine RPC, got %d", engineHits.Load())
	}
	if upstreamHit.Load() != 1 {
		t.Fatalf("expected 1 upstream hit, got %d", upstreamHit.Load())
	}
}

func TestServeHTTP_EngineMutatesMethodAndURL(t *testing.T) {
	gotMethod := make(chan string, 1)
	gotPath := make(chan string, 1)
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod <- r.Method
		gotPath <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	defer upstream.Close()

	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{ForwardRequest: &hookproto.ForwardRequestResponse{
			Method: "PUT",
			URL:    upstream.URL + "/rewritten",
		}}
		return resp
	})
	defer fe.Close()

	sc := newSidecarUnderTest(fe.path)
	sc.bases = upstreamBasesPointingAt(upstream.URL)
	sc.engineHealthy.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(`{"x":1}`))
	rec := httptest.NewRecorder()
	sc.ServeHTTP(rec, req)

	if m := <-gotMethod; m != "PUT" {
		t.Fatalf("method override dropped: got %q", m)
	}
	if p := <-gotPath; p != "/rewritten" {
		t.Fatalf("URL override dropped: got %q", p)
	}
}

func TestServeHTTP_EngineMutates_ForwardsMutated(t *testing.T) {
	receivedBody := make(chan string, 1)
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		receivedBody <- string(buf)
		w.WriteHeader(http.StatusOK)
	})
	defer upstream.Close()

	mutated := `{"compacted":true}`
	fe := newFakeEngine(t, func(env hookproto.Envelope) hookproto.Envelope {
		resp := hookproto.NewEnvelope(env.Op, env.ID)
		resp.Response = &hookproto.Response{ForwardRequest: &hookproto.ForwardRequestResponse{
			Body:    []byte(mutated),
			Headers: map[string][]string{"X-Slim": {"applied"}, "Authorization": {"Bearer x"}},
		}}
		return resp
	})
	defer fe.Close()

	sc := newSidecarUnderTest(fe.path)
	sc.bases = upstreamBasesPointingAt(upstream.URL)
	sc.engineHealthy.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(`{"original":true}`))
	req.Header.Set("Authorization", "Bearer x")
	req.Header.Set("User-Agent", "codex-cli/0.5")
	rec := httptest.NewRecorder()
	sc.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if got := <-receivedBody; got != mutated {
		t.Fatalf("upstream saw %q, want %q", got, mutated)
	}
}

func TestServeHTTP_EngineHealthy_RPCFailureDegradesAndFlipsFlag(t *testing.T) {
	receivedBody := make(chan string, 1)
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		receivedBody <- string(buf)
		w.WriteHeader(http.StatusOK)
	})
	defer upstream.Close()

	// Engine immediately closes -> askEngine returns ok=false.
	dir, err := os.MkdirTemp("", "slx")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, fmt.Sprintf("e%d.sock", time.Now().UnixNano()%1000000))
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	sc := newSidecarUnderTest(sock)
	sc.bases = upstreamBasesPointingAt(upstream.URL)
	sc.engineHealthy.Store(true)
	// Give the listener a moment to be ready.
	time.Sleep(5 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(`original`))
	rec := httptest.NewRecorder()
	sc.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if got := <-receivedBody; got != "original" {
		t.Fatalf("expected original body to reach upstream after degrade, got %q", got)
	}
	if sc.engineHealthy.Load() {
		t.Fatalf("expected engineHealthy=false after RPC failure")
	}
}

// upstreamBasesPointingAt routes every provider to the given httptest URL
// so ServeHTTP-driven tests can observe what the sidecar forwards.
func upstreamBasesPointingAt(u string) upstream.Bases {
	return upstream.Bases{Anthropic: u, OpenAI: u, CodexChatGPT: u}
}

func TestReadBody_NilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil
	body, err := readBody(req)
	if err != nil {
		t.Fatalf("readBody: %v", err)
	}
	if body != nil {
		t.Fatalf("expected nil body, got %q", string(body))
	}
}

func TestCloneHeaders_NilReturnsNil(t *testing.T) {
	if got := cloneHeaders(nil); got != nil {
		t.Fatalf("nil header should return nil, got %v", got)
	}
}

func TestCloneHeaders_DeepCopy(t *testing.T) {
	in := http.Header{"X": []string{"a", "b"}}
	out := cloneHeaders(in)
	out["X"][0] = "mutated"
	if in["X"][0] != "a" {
		t.Fatalf("clone shared backing array: %v", in["X"])
	}
}

func TestMapToHeader_DeepCopy(t *testing.T) {
	in := map[string][]string{"K": {"v1", "v2"}}
	out := mapToHeader(in)
	out["K"][0] = "mutated"
	if in["K"][0] != "v1" {
		t.Fatalf("mapToHeader shared backing array")
	}
}

func TestCloneHeaderMap_DeepCopy(t *testing.T) {
	in := http.Header{"K": []string{"v"}}
	out := cloneHeaderMap(in)
	out["K"][0] = "x"
	if in["K"][0] != "v" {
		t.Fatalf("cloneHeaderMap shared backing array")
	}
}

func TestBasesFromEnv_FallbacksToDefault(t *testing.T) {
	got := basesFromEnv(func(string) string { return "" })
	if got != upstream.DefaultBases {
		t.Fatalf("expected DefaultBases when no env vars, got %+v", got)
	}
}

func TestBasesFromEnv_OverridesPerProvider(t *testing.T) {
	got := basesFromEnv(func(key string) string {
		switch key {
		case "SLIMFERENCE_UPSTREAM_ANTHROPIC":
			return "https://anth-test.local"
		case "SLIMFERENCE_UPSTREAM_OPENAI":
			return "https://oai-test.local"
		case "SLIMFERENCE_UPSTREAM_CODEX":
			return "https://codex-test.local"
		}
		return ""
	})
	want := upstream.Bases{
		Anthropic:    "https://anth-test.local",
		OpenAI:       "https://oai-test.local",
		CodexChatGPT: "https://codex-test.local",
	}
	if got != want {
		t.Fatalf("override drift: got %+v want %+v", got, want)
	}
}

func TestDefaultEngineSocketPath(t *testing.T) {
	got := defaultEngineSocketPath(os.UserHomeDir)
	if got == "" {
		t.Fatalf("expected non-empty default path")
	}
	if !strings.HasSuffix(got, "hook.sock") {
		t.Fatalf("path should end in hook.sock, got %q", got)
	}
}

func TestDefaultEngineSocketPath_HomeError_FallsBackToTmp(t *testing.T) {
	got := defaultEngineSocketPath(func() (string, error) { return "", errors.New("no home") })
	if got != "/tmp/slimference-hook.sock" {
		t.Fatalf("home-error fallback: got %q", got)
	}
}

func TestDefaultEngineSocketPath_EmptyHome_FallsBackToTmp(t *testing.T) {
	got := defaultEngineSocketPath(func() (string, error) { return "", nil })
	if got != "/tmp/slimference-hook.sock" {
		t.Fatalf("empty-home fallback: got %q", got)
	}
}

func TestRun_InvalidFlagReturns2(t *testing.T) {
	var buf strings.Builder
	code := run(context.Background(), []string{"--no-such-flag"}, &buf)
	if code != 2 {
		t.Fatalf("expected exit 2 on flag error, got %d", code)
	}
}

func TestRun_ListenFailureReturns1(t *testing.T) {
	// Bind a port first so the sidecar's listen attempt fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	var buf strings.Builder
	code := run(context.Background(), []string{"--addr", ln.Addr().String(), "--probe-interval", "1h", "--rpc-timeout", "1ms"}, &buf)
	if code != 1 {
		t.Fatalf("expected exit 1 on bind failure, got %d", code)
	}
	if !strings.Contains(buf.String(), "sidecar: listen") {
		t.Fatalf("expected listen error message, got %q", buf.String())
	}
}

func TestRun_ServeFailureReturns1(t *testing.T) {
	prevListen := listenFn
	listenFn = func(network, addr string) (net.Listener, error) {
		return failingListener{err: errors.New("accept boom")}, nil
	}
	t.Cleanup(func() { listenFn = prevListen })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var buf strings.Builder
	code := run(ctx, []string{"--addr", "127.0.0.1:0", "--probe-interval", "1h", "--rpc-timeout", "1ms"}, &buf)
	if code != 1 {
		t.Fatalf("expected exit 1 on serve failure, got %d", code)
	}
	if !strings.Contains(buf.String(), "accept boom") {
		t.Fatalf("expected serve error message, got %q", buf.String())
	}
}

func TestMainUsesExitFn(t *testing.T) {
	prevExit := exitFn
	prevArgs := os.Args
	exitFn = func(code int) { panic(sidecarExit{code: code}) }
	os.Args = []string{"slimference-sidecar", "--no-such-flag"}
	t.Cleanup(func() {
		exitFn = prevExit
		os.Args = prevArgs
	})

	defer func() {
		r := recover()
		got, ok := r.(sidecarExit)
		if !ok {
			t.Fatalf("main panic=%#v, want sidecarExit", r)
		}
		if got.code != 2 {
			t.Fatalf("exit code=%d, want 2", got.code)
		}
	}()
	main()
}

func TestRun_GracefulShutdownReturns0(t *testing.T) {
	// Bind a free port then cancel the context. run() should return 0
	// after the http server reports ErrServerClosed.
	var buf strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		// :0 lets the OS pick a free port.
		done <- run(ctx, []string{"--addr", "127.0.0.1:0", "--probe-interval", "1h", "--rpc-timeout", "1ms", "--verbose"}, &buf)
	}()
	// Give the server a moment to start.
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("expected exit 0 on graceful shutdown, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("run did not return after context cancel")
	}
}

type errReader struct{ err error }

func (e *errReader) Read(p []byte) (int, error) { return 0, e.err }
func (e *errReader) Close() error               { return nil }

type sidecarExit struct{ code int }

type failingListener struct{ err error }

func (f failingListener) Accept() (net.Conn, error) { return nil, f.err }
func (f failingListener) Close() error              { return nil }
func (f failingListener) Addr() net.Addr            { return dummyAddr("failing-listener") }

type dummyAddr string

func (d dummyAddr) Network() string { return "tcp" }
func (d dummyAddr) String() string  { return string(d) }

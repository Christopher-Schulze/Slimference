// Command slimference-sidecar is the fail-open HTTP listener that holds
// 127.0.0.1:8990 (the address all Slimference-wired clients point at).
//
// Architecture (t164):
//
//	┌──────────────────────────────────────────────────────────┐
//	│ slimference-sidecar  (this binary)                       │
//	│                                                          │
//	│  for every request:                                      │
//	│    1. probe engine over Unix socket (cached health flag) │
//	│    2a. engine healthy → ask engine to mutate the request │
//	│         then forward to upstream                         │
//	│    2b. engine unhealthy / timeout / error                │
//	│         → forward request 1:1 to real upstream           │
//	│  auth header passes through unchanged in both modes      │
//	└──────────────────────────────────────────────────────────┘
//
// Dependency surface is intentionally minimal: net/http, net/http/httputil,
// encoding/json, internal/types, internal/proxy/upstream,
// internal/daemon/hookproto. No imports of internal/compression,
// internal/sessions, internal/tui, internal/tlsca
// so the sidecar cannot inherit those packages' crash risks.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/daemon/hookproto"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/upstream"
)

var (
	exitFn   = os.Exit
	listenFn = net.Listen
)

const (
	defaultAddr           = "127.0.0.1:8990"
	defaultEngineProbeInt = 5 * time.Second
	defaultEngineDial     = 50 * time.Millisecond
	defaultEngineRPC      = 50 * time.Millisecond
)

func main() {
	exitFn(run(context.Background(), os.Args[1:], os.Stderr))
}

// run is the testable entrypoint. Returns the exit code so main() stays
// a one-liner and the rest is covered by unit tests. The ctx is used to
// trigger graceful shutdown — when it cancels, the server stops and
// run returns 0.
func run(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("slimference-sidecar", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", defaultAddr, "bind address")
	engineSock := fs.String("engine-socket", defaultEngineSocketPath(homeDir), "Unix socket of the Slimference engine")
	probeInterval := fs.Duration("probe-interval", defaultEngineProbeInt, "engine health probe interval")
	rpcTimeout := fs.Duration("rpc-timeout", defaultEngineRPC, "per-request engine RPC timeout")
	verbose := fs.Bool("verbose", false, "log every request decision")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	logger := log.New(stderr, "slimference-sidecar ", log.LstdFlags|log.Lmicroseconds)
	if !*verbose {
		logger.SetOutput(io.Discard)
	}

	sc := newSidecar(*engineSock, *rpcTimeout, logger)

	probeCtx, probeCancel := context.WithCancel(ctx)
	defer probeCancel()
	go sc.runHealthProbe(probeCtx, *probeInterval)

	ln, err := listenFn("tcp", *addr)
	if err != nil {
		fmt.Fprintf(stderr, "sidecar: listen: %v\n", err)
		return 1
	}
	srv := &http.Server{
		Handler:           sc,
		ReadHeaderTimeout: 30 * time.Second,
	}
	logger.Printf("listening on %s, engine socket %s", *addr, *engineSock)

	shutdownDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		close(shutdownDone)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "sidecar: serve: %v\n", err)
		return 1
	}
	<-shutdownDone
	return 0
}

func newSidecar(engineSocket string, rpcTimeout time.Duration, logger *log.Logger) *sidecar {
	return &sidecar{
		engineSocket: engineSocket,
		rpcTimeout:   rpcTimeout,
		logger:       logger,
		client:       &http.Client{Timeout: 0},
		now:          time.Now,
		dialEngineFn: dialUnix,
		bases:        basesFromEnv(os.Getenv),
	}
}

// basesFromEnv loads the upstream bases, falling back to DefaultBases
// per provider when the env var is unset. The env-var override exists
// for two reasons: (1) integration tests point the sidecar at a fake
// upstream, and (2) staging/canary deployments may route to a different
// API host without modifying the binary.
func basesFromEnv(getenv func(string) string) upstream.Bases {
	pick := func(key, fallback string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return fallback
	}
	return upstream.Bases{
		Anthropic:    pick("SLIMFERENCE_UPSTREAM_ANTHROPIC", upstream.DefaultBases.Anthropic),
		OpenAI:       pick("SLIMFERENCE_UPSTREAM_OPENAI", upstream.DefaultBases.OpenAI),
		CodexChatGPT: pick("SLIMFERENCE_UPSTREAM_CODEX", upstream.DefaultBases.CodexChatGPT),
	}
}

// homeDir is the injectable home-dir resolver; defaults to os.UserHomeDir.
var homeDir = os.UserHomeDir

// defaultEngineSocketPath resolves the engine socket path, with a
// /tmp fallback when the home directory cannot be determined.
func defaultEngineSocketPath(home func() (string, error)) string {
	h, err := home()
	if err != nil || h == "" {
		return "/tmp/slimference-hook.sock"
	}
	return filepath.Join(h, ".slimference", "run", "hook.sock")
}

// sidecar holds the per-process state. Fields except engineHealthy are
// set once at startup; engineHealthy is atomic so the hot path is
// lock-free.
type sidecar struct {
	engineSocket string
	rpcTimeout   time.Duration
	logger       *log.Logger
	client       *http.Client
	now          func() time.Time

	// bases is injected so tests can route to a fake upstream without
	// monkey-patching package-level state. Production callers use
	// upstream.DefaultBases or load from config.
	bases upstream.Bases

	engineHealthy atomic.Bool

	// dialEngineFn is injectable for tests.
	dialEngineFn func(socket string, timeout time.Duration) (net.Conn, error)
}

func (s *sidecar) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	provider := upstream.Detect(r.URL.Path, body, r.Header.Get("User-Agent"))
	upstreamURL := upstream.URL(provider, r.URL.Path, r.URL.RawQuery, s.bases)

	// Engine fast-path: if the cached health flag says healthy, attempt
	// one RPC with a tight timeout. Any error path degrades to direct
	// passthrough — the user must keep working.
	if s.engineHealthy.Load() {
		dec, ok := s.askEngine(r, body, upstreamURL)
		if ok {
			method := r.Method
			if dec.method != "" {
				method = dec.method
			}
			finalURL := upstreamURL
			if dec.urlOverride != "" {
				finalURL = dec.urlOverride
			}
			headers := r.Header
			if dec.headerOverride != nil {
				headers = mapToHeader(dec.headerOverride)
			}
			finalBody := body
			if dec.bodyOverride != nil {
				finalBody = dec.bodyOverride
			}
			s.forward(w, r, method, finalURL, headers, finalBody)
			return
		}
		// Engine refused / timed out / errored. Mark unhealthy so the
		// next probe re-attempts; serve this request via passthrough.
		s.engineHealthy.Store(false)
		s.logger.Printf("engine rpc failed for %s %s: falling back to passthrough", r.Method, r.URL.Path)
	}
	s.forward(w, r, r.Method, upstreamURL, r.Header, body)
}

// engineDecision is the normalised result of one ForwardRequest RPC.
// Empty/nil override fields mean "keep the original request value".
type engineDecision struct {
	passThrough    bool
	method         string
	urlOverride    string
	headerOverride map[string][]string
	bodyOverride   []byte
}

func mapToHeader(m map[string][]string) http.Header {
	out := http.Header{}
	for k, vs := range m {
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}

// askEngine performs a single ForwardRequest RPC. Returns ok=false on
// any error so the caller degrades. The engine indicates "no change" by
// setting PassThrough=true, which we honour without mutation.
func (s *sidecar) askEngine(r *http.Request, body []byte, upstreamURL string) (engineDecision, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), s.rpcTimeout)
	defer cancel()

	conn, err := s.dialEngineFn(s.engineSocket, s.rpcTimeout)
	if err != nil {
		return engineDecision{}, false
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if ok {
		_ = conn.SetDeadline(deadline)
	}

	env := hookproto.NewEnvelope(hookproto.OpForwardRequest, "")
	env.Request = &hookproto.Request{ForwardRequest: &hookproto.ForwardRequest{
		Method:   r.Method,
		URL:      upstreamURL,
		Headers:  cloneHeaders(r.Header),
		Body:     body,
		SourceUA: r.Header.Get("User-Agent"),
	}}
	if err := hookproto.Encode(conn, env); err != nil {
		return engineDecision{}, false
	}
	resp, err := hookproto.Decode(bufio.NewReader(conn))
	if err != nil {
		return engineDecision{}, false
	}
	if resp.Response == nil {
		return engineDecision{}, false
	}
	if resp.Response.Error != "" {
		return engineDecision{}, false
	}
	if resp.Response.ForwardRequest == nil {
		return engineDecision{}, false
	}
	fr := resp.Response.ForwardRequest
	if fr.PassThrough {
		return engineDecision{passThrough: true}, true
	}
	return engineDecision{
		method:         fr.Method,
		urlOverride:    fr.URL,
		headerOverride: fr.Headers,
		bodyOverride:   fr.Body,
	}, true
}

// forward is the actual reverse-proxy step. It builds a new request to
// the upstream and streams the response back. Streaming (SSE / chunked
// transfer) is preserved because httputil.ReverseProxy flushes by
// default and FlushInterval=-1 disables buffering.
func (s *sidecar) forward(w http.ResponseWriter, orig *http.Request, method, fullURL string, headers http.Header, body []byte) {
	u, err := url.Parse(fullURL)
	if err != nil {
		http.Error(w, "bad upstream url: "+err.Error(), http.StatusBadGateway)
		return
	}
	target := &url.URL{Scheme: u.Scheme, Host: u.Host}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1
	rp.Director = func(req *http.Request) {
		req.URL.Scheme = u.Scheme
		req.URL.Host = u.Host
		req.URL.Path = u.Path
		req.URL.RawQuery = u.RawQuery
		req.Host = u.Host
		req.Method = method
		req.Header = cloneHeaderMap(headers)
		req.Body = io.NopCloser(strings.NewReader(string(body)))
		req.ContentLength = int64(len(body))
	}
	rp.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, err error) {
		s.logger.Printf("upstream error %s: %v", fullURL, err)
		http.Error(rw, "upstream error: "+err.Error(), http.StatusBadGateway)
	}
	rp.ServeHTTP(w, orig)
}

// runHealthProbe is a long-running goroutine that probes the engine on
// a fixed interval. Healthy -> sets atomic flag true. Any failure ->
// false. Used to gate the hot-path RPC attempt.
func (s *sidecar) runHealthProbe(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	s.probeOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.probeOnce(ctx)
		}
	}
}

func (s *sidecar) probeOnce(ctx context.Context) {
	conn, err := s.dialEngineFn(s.engineSocket, s.rpcTimeout)
	if err != nil {
		s.engineHealthy.Store(false)
		return
	}
	defer conn.Close()
	deadline := s.now().Add(s.rpcTimeout)
	_ = conn.SetDeadline(deadline)
	env := hookproto.NewEnvelope(hookproto.OpPing, "")
	env.Request = &hookproto.Request{Ping: &hookproto.PingRequest{}}
	if err := hookproto.Encode(conn, env); err != nil {
		s.engineHealthy.Store(false)
		return
	}
	resp, err := hookproto.Decode(bufio.NewReader(conn))
	if err != nil {
		s.engineHealthy.Store(false)
		return
	}
	healthy := resp.Response != nil && resp.Response.Ping != nil && resp.Response.Ping.Healthy
	s.engineHealthy.Store(healthy)
	_ = ctx // reserved for future cancellation-aware dials
}

func dialUnix(socket string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", socket, timeout)
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func cloneHeaders(h http.Header) map[string][]string {
	if h == nil {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, vs := range h {
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}

func cloneHeaderMap(h http.Header) http.Header {
	out := http.Header{}
	for k, vs := range h {
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}

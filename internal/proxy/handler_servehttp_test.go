package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/types"
	"github.com/Christopher-Schulze/Slimference/internal/wscompact"
	"github.com/klauspost/compress/zstd"
)

var errReadBodyTest = errors.New("read body failed")

type alwaysFailBody struct{}

func (alwaysFailBody) Read([]byte) (int, error) { return 0, errReadBodyTest }

type hijackErrorRecorder struct {
	http.ResponseWriter
}

func (h hijackErrorRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack failed")
}

func TestServeHTTP_passthroughGET(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"upstream":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	p := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, rec.Body.String())
	}
}

func TestServeHTTP_DirectCodexWebSocketUpgradeTunnels(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)
	seenPath := make(chan string, 1)
	p.webSocketTunnel = &WebSocketTunnel{
		Dialer: func(string, string) (net.Conn, error) {
			client, upstream := net.Pipe()
			go func() {
				defer upstream.Close()
				req, err := http.ReadRequest(bufio.NewReader(upstream))
				if err != nil {
					seenPath <- "read-error:" + err.Error()
					return
				}
				seenPath <- req.URL.Path
				_, _ = io.WriteString(upstream, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			}()
			return client, nil
		},
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET /backend-api/codex/responses HTTP/1.1\r\nHost: 127.0.0.1:8990\r\nUser-Agent: codex/0.130.0\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := <-seenPath; got != "/backend-api/codex/responses" {
		t.Fatalf("upstream path = %q", got)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].RouteMode != "websocket_tunnel" {
		t.Fatalf("missing websocket flight summary: %#v", summaries)
	}
}

func TestServeHTTP_DirectCodexWebSocketUpgradeUsesPhaseFBridge(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)
	seenPayload := make(chan string, 1)
	p.webSocketTunnel.Dialer = func(string, string) (net.Conn, error) {
		client, upstream := net.Pipe()
		go func() {
			defer upstream.Close()
			br := bufio.NewReader(upstream)
			if _, err := http.ReadRequest(br); err != nil {
				seenPayload <- "read-error:" + err.Error()
				return
			}
			_, _ = io.WriteString(upstream, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			frame, err := wscompact.ReadFrame(br)
			if err != nil {
				seenPayload <- "frame-error:" + err.Error()
				return
			}
			seenPayload <- string(frame.Payload)
		}()
		return client, nil
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET /backend-api/codex/responses HTTP/1.1\r\nHost: 127.0.0.1:8990\r\nUser-Agent: codex/0.130.0\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if _, err := wscompact.WriteFrame(conn, true, wscompact.OpcodeText, nil, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	select {
	case got := <-seenPayload:
		if got != `{"type":"ping"}` {
			t.Fatalf("upstream payload = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not receive WSS frame")
	}
	_ = conn.Close()
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].RouteMode != "websocket_phasef" {
		t.Fatalf("missing websocket phasef flight summary: %#v", summaries)
	}
}

func TestServeHTTP_DirectCodexWebSocketForceHTTPSFallback(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Proxy.DirectCodexWebSocketPolicy = "force_https_fallback"
	p := New(cfg)
	p.webSocketTunnel = &WebSocketTunnel{
		Dialer: func(string, string) (net.Conn, error) {
			t.Fatal("force_https_fallback must not dial upstream websocket")
			return nil, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/backend-api/codex/responses", nil)
	req.Host = "127.0.0.1:8990"
	req.Header.Set("User-Agent", "codex/0.130.0")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", res.StatusCode, rec.Body.String())
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].RouteMode != "websocket_force_https_fallback" {
		t.Fatalf("missing fallback flight summary: %#v", summaries)
	}
}

func TestServeHTTP_CodexMalformedBodyPassthroughPreservesContentType(t *testing.T) {
	t.Parallel()
	seenContentType := make(chan string, 1)
	seenBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		seenContentType <- r.Header.Get("Content-Type")
		seenBody <- string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	p := New(cfg)

	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader("(not-json)"))
	req.Header.Set("User-Agent", "codex/0.130.0")
	req.Header.Set("Content-Type", "application/cbor")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, rec.Body.String())
	}
	if got := <-seenContentType; got != "application/cbor" {
		t.Fatalf("content-type = %q", got)
	}
	if got := <-seenBody; got != "(not-json)" {
		t.Fatalf("body = %q", got)
	}
}

func TestServeHTTP_CodexZstdBodyRunsPipelineAndReencodes(t *testing.T) {
	t.Parallel()
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = encoder.Close() })
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(decoder.Close)

	seenContentEncoding := make(chan string, 1)
	seenDecodedBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		decoded, err := decoder.DecodeAll(body, nil)
		if err != nil {
			t.Errorf("decode upstream zstd body: %v", err)
		}
		seenContentEncoding <- r.Header.Get("Content-Encoding")
		seenDecodedBody <- string(decoded)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_1","output":[]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	p := New(cfg)

	body := []byte(`{"model":"codex-test","input":"please inspect the repo","stream":false}`)
	wireBody := encoder.EncodeAll(body, nil)
	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", bytes.NewReader(wireBody))
	req.Header.Set("User-Agent", "codex/0.130.0")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, rec.Body.String())
	}
	if got := <-seenContentEncoding; got != "zstd" {
		t.Fatalf("content-encoding = %q", got)
	}
	if got := <-seenDecodedBody; !strings.Contains(got, "please inspect the repo") {
		t.Fatalf("decoded body = %q", got)
	}
}

func TestServeHTTP_UnsupportedContentEncodingPassthrough(t *testing.T) {
	t.Parallel()
	seenContentEncoding := make(chan string, 1)
	seenBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		seenContentEncoding <- r.Header.Get("Content-Encoding")
		seenBody <- string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	p := New(cfg)

	body := `{"model":"codex-test","input":"please inspect the repo","stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(body))
	req.Header.Set("User-Agent", "codex/0.130.0")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "br")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, rec.Body.String())
	}
	if got := <-seenContentEncoding; got != "br" {
		t.Fatalf("content-encoding = %q", got)
	}
	if got := <-seenBody; got != body {
		t.Fatalf("body = %q", got)
	}
}

func TestRequestBodyEncodingUnsupportedBranches(t *testing.T) {
	t.Parallel()
	if decoded, enc, err := decodeRequestBodyForPipeline([]byte("body"), "identity"); err != nil || string(decoded) != "body" || enc != requestBodyEncodingIdentity {
		t.Fatalf("identity decode body=%q enc=%q err=%v", decoded, enc, err)
	}
	if _, _, err := decodeRequestBodyForPipeline([]byte("body"), "br"); err == nil {
		t.Fatal("expected unsupported decode encoding error")
	}
	if _, _, err := decodeRequestBodyForPipeline([]byte("not-zstd"), "zstd"); err == nil {
		t.Fatal("expected malformed zstd decode error")
	}
	if encoded, err := encodeRequestBodyForPipeline([]byte("body"), requestBodyEncodingIdentity); err != nil || string(encoded) != "body" {
		t.Fatalf("identity encode body=%q err=%v", encoded, err)
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = encoder.Close() })
	if encoded, err := encodeRequestBodyForPipeline([]byte("body"), requestBodyEncodingZstd); err != nil || len(encoded) == 0 || string(encoded) == "body" {
		t.Fatalf("zstd encode len=%d err=%v", len(encoded), err)
	}
	if _, err := encodeRequestBodyForPipeline([]byte("body"), requestBodyEncoding("br")); err == nil {
		t.Fatal("expected unsupported encode encoding error")
	}
}

func TestRequestBodyEncodingZstdConstructorErrors(t *testing.T) {
	origReader := newZstdReaderFn
	defer func() { newZstdReaderFn = origReader }()
	newZstdReaderFn = func(io.Reader, ...zstd.DOption) (*zstd.Decoder, error) {
		return nil, errReadBodyTest
	}
	if _, _, err := decodeRequestBodyForPipeline([]byte("body"), "zstd"); err == nil {
		t.Fatal("expected zstd reader constructor error")
	}

	origWriter := newZstdWriterFn
	defer func() { newZstdWriterFn = origWriter }()
	newZstdWriterFn = func(io.Writer, ...zstd.EOption) (*zstd.Encoder, error) {
		return nil, errReadBodyTest
	}
	if _, err := encodeRequestBodyForPipeline([]byte("body"), requestBodyEncodingZstd); err == nil {
		t.Fatal("expected zstd writer constructor error")
	}
}

func TestDoUpstreamRequest_UnsupportedBodyEncodingBuildError(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestBodyEncodingKey{}, requestBodyEncoding("br")))
	if _, err := p.doUpstreamRequest(req, types.OpenAI, []byte(`{"messages":[]}`)); err == nil {
		t.Fatal("expected unsupported request body encoding error")
	}
}

func TestDirectWebSocketUpgrade_errorBranches(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/backend-api/codex/responses", nil)
	req.Header.Set("Upgrade", "websocket")

	cfg := config.Defaults()
	p := New(cfg)
	p.webSocketTunnel = nil
	rec := httptest.NewRecorder()
	p.handleDirectWebSocketUpgrade(rec, req, types.CodexChatGPT)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("nil tunnel status = %d", rec.Code)
	}

	p.webSocketTunnel = &WebSocketTunnel{}
	rec = httptest.NewRecorder()
	p.handleDirectWebSocketUpgrade(rec, req, types.CodexChatGPT)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("non-hijacker status = %d", rec.Code)
	}

	badCfg := config.Defaults()
	badCfg.Upstream.CodexChatGPT.BaseURL = "://bad"
	bad := New(badCfg)
	bad.webSocketTunnel = &WebSocketTunnel{}
	rec = httptest.NewRecorder()
	bad.handleDirectWebSocketUpgrade(rec, req, types.CodexChatGPT)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("bad upstream status = %d", rec.Code)
	}

	emptyHostCfg := config.Defaults()
	emptyHostCfg.Upstream.CodexChatGPT.BaseURL = "https:///"
	emptyHost := New(emptyHostCfg)
	if host, ok := emptyHost.upstreamHost(types.CodexChatGPT); ok || host != "" {
		t.Fatalf("empty upstream host = (%q,%v)", host, ok)
	}

	okCfg := config.Defaults()
	okProxy := New(okCfg)
	okProxy.webSocketTunnel = &WebSocketTunnel{}
	okProxy.handleDirectWebSocketUpgrade(hijackErrorRecorder{ResponseWriter: httptest.NewRecorder()}, req, types.CodexChatGPT)
}

func TestServeHTTP_passthroughProviderDisabled(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	p := New(cfg)
	p.SetProviderEnabled(types.Anthropic, false)

	body := `{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
}

func TestServeHTTP_UnknownAnthropicVersionDowngradesPipeline(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Proxy.AnthropicVersions = []string{"2023-06-01"}
	cfg.Proxy.AnthropicUnknownBehavior = "passthrough"
	p := New(cfg)

	body := `{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2099-01-01")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if AnthropicUnknownVersionCount() == 0 {
		t.Fatal("unknown version counter did not increment")
	}
}

func TestServeHTTP_readBodyFailedCompressible(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Body = io.NopCloser(alwaysFailBody{})
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read body") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_readBodyFailedPassthrough(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	req.Body = io.NopCloser(alwaysFailBody{})
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read body") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_readBodyTooLargeCompressible(t *testing.T) {
	t.Parallel()

	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Body = io.NopCloser(&repeatingBody{remaining: maxRequestBodySize + 1})
	req.ContentLength = maxRequestBodySize + 1
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestServeHTTP_readBodyTooLargePassthrough(t *testing.T) {
	t.Parallel()

	p := New(config.Defaults())
	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	req.Body = io.NopCloser(&repeatingBody{remaining: maxRequestBodySize + 1})
	req.ContentLength = maxRequestBodySize + 1
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

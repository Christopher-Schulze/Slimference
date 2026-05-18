package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
	"github.com/slimference/slimference/internal/types"
)

func TestWSSProbeNilAndEmptyDispatcherBranches(t *testing.T) {
	if got := (WSSProbe{}).ProbeWSS(context.Background()); got.EngineActive {
		t.Fatalf("nil proxy should be inactive: %+v", got)
	}

	p := New(config.Defaults())
	if got := (WSSProbe{Proxy: p}).ProbeWSS(context.Background()); got.EngineActive {
		t.Fatalf("nil dispatcher should be inactive: %+v", got)
	}

	d := &PhaseFDispatcher{}
	d.counters.wsmitmForwarded.Add(1)
	p.SetWSSDispatcher(d)
	got := (WSSProbe{Proxy: p}).ProbeWSS(context.Background())
	if !got.EngineActive || !got.ByteBridgeOnly || got.MutationActive {
		t.Fatalf("dispatcher telemetry wrong: %+v", got)
	}
}

func TestWSPhaseFRemainingRepdetAndRequestBodyBranches(t *testing.T) {
	p := New(config.Defaults())
	p.config.Compression.OutputReduce.RepetitionDetectionEnabled = true
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	if adapter.applyRepdetResponse(&wsmitm.Envelope{
		Kind:     wsmitm.FrameKindResponseCompleted,
		Response: []byte(`{"output":[{"content":[{"text":"unchanged"}]}]}`),
	}) {
		t.Fatal("nil repdet index must not rewrite terminal response")
	}

	env := &wsmitm.Envelope{
		Raw: json.RawMessage(`{"model":"gpt-5-codex","stream":true}`),
		Fields: map[string]json.RawMessage{
			"model":  json.RawMessage(`"gpt-5-codex"`),
			"stream": json.RawMessage(`true`),
		},
	}
	if !wsEnvelopeLooksLikeRequestBody(env) {
		t.Fatal("model+stream envelope should be treated as a request body candidate")
	}
}

func TestPassthroughWithOptionalRepdetUnknownProvider(t *testing.T) {
	p := New(config.Defaults())
	p.config.Compression.OutputReduce.RepetitionDetectionEnabled = true
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("plain upstream body")),
	}
	rec := httptest.NewRecorder()
	body := p.passthroughWithOptionalRepdet(rec, resp, types.Provider(99), nil, nil)
	if string(body) != "plain upstream body" || rec.Body.String() != "plain upstream body" {
		t.Fatalf("unknown provider passthrough body=%q rec=%q", body, rec.Body.String())
	}
}

func TestDoHAndDefaultUpstreamDialRemainingBranches(t *testing.T) {
	if _, err := dohResolveA(context.Background(), strings.Repeat("a", 64)+".example"); err == nil {
		t.Fatal("oversized label should fail before transport")
	}
	canceledCtx, cancelDefaultDial := context.WithCancel(context.Background())
	cancelDefaultDial()
	_, _ = upstreamTCPDialContextFn(canceledCtx, "tcp", "127.0.0.1:1")

	prevDoHDial := dohDialTLSContextFn
	dohDialTLSContextFn = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("doh dial refused")
	}
	if _, err := dohResolveA(context.Background(), "example.com"); err == nil || !strings.Contains(err.Error(), "doh dial refused") {
		t.Fatalf("DoH dial error not propagated: %v", err)
	}
	dohDialTLSContextFn = prevDoHDial

	prevResolve := dohResolveAFn
	prevDial := upstreamTCPDialContextFn
	prevWrap := wrapTLSConnFn
	t.Cleanup(func() {
		dohResolveAFn = prevResolve
		upstreamTCPDialContextFn = prevDial
		wrapTLSConnFn = prevWrap
		upstreamIPCache = &ipCache{}
	})
	upstreamIPCache = &ipCache{}

	dohResolveAFn = func(context.Context, string) (string, error) {
		return "198.51.100.10", nil
	}
	var dialed []string
	upstreamTCPDialContextFn = func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		if strings.HasPrefix(address, "198.51.100.10:") {
			return nil, errors.New("direct IP refused")
		}
		return newScriptedConn(""), nil
	}
	wrapTLSConnFn = func(_ context.Context, raw net.Conn, host string) (net.Conn, error) {
		if host != "chatgpt.com" {
			t.Fatalf("host=%q", host)
		}
		return raw, nil
	}
	conn, err := DefaultUpstreamDial()(context.Background(), "chatgpt.com:443")
	if err != nil {
		t.Fatalf("fallback dial: %v", err)
	}
	_ = conn.Close()
	if len(dialed) != 2 || dialed[0] != "198.51.100.10:443" || dialed[1] != "chatgpt.com:443" {
		t.Fatalf("dial sequence=%v", dialed)
	}

	dohResolveAFn = func(context.Context, string) (string, error) {
		return "", errors.New("doh unavailable")
	}
	upstreamIPCache = &ipCache{}
	dialed = nil
	conn, err = DefaultUpstreamDial()(context.Background(), "chatgpt.com:443")
	if err != nil {
		t.Fatalf("system fallback after DoH error: %v", err)
	}
	_ = conn.Close()
	if len(dialed) != 1 || dialed[0] != "chatgpt.com:443" {
		t.Fatalf("fallback dial sequence=%v", dialed)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	upstreamTCPDialContextFn = func(context.Context, string, string) (net.Conn, error) {
		return nil, context.Canceled
	}
	if _, err := DefaultUpstreamDial()(ctx, "bad-host-port"); err == nil {
		t.Fatal("invalid hostPort should fail")
	}

	client, server := newPipe()
	_ = server.Close()
	if _, err := wrapTLS(ctx, client, "example.com"); err == nil {
		t.Fatal("canceled/closed wrapTLS should fail")
	}
}

func TestBridgeContextCancellationBranch(t *testing.T) {
	client, upstream := newPipe()
	defer client.Close()
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &PhaseFDispatcher{BridgeTimeout: time.Second}
	if err := d.bridge(ctx, client, upstream); !errors.Is(err, context.Canceled) {
		t.Fatalf("bridge cancellation err=%v", err)
	}
}

func TestProviderCodexRemainingBranches(t *testing.T) {
	msgs, extra, err := extractCodexMessages(map[string]json.RawMessage{"model": json.RawMessage(`"gpt-5"`)})
	if err != nil || msgs != nil || string(extra["model"]) != `"gpt-5"` {
		t.Fatalf("missing codex input branch msgs=%v extra=%v err=%v", msgs, extra, err)
	}

	if _, _, err := recoverCodexInputRawFromOriginal(
		types.Message{Index: 0},
		[]json.RawMessage{json.RawMessage(`{bad-json`)},
	); err == nil {
		t.Fatal("expected malformed original raw item error")
	}
}

package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/proxy/sniroute"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/transparent"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/wscompact"
)

// PhaseFDispatcher implements transparent.Dispatcher. It receives a
// post-handshake TLS connection plus the sniroute decision and, for
// every decision but Reject, bridges bytes between the client and a
// freshly-dialled TLS connection to the real upstream.
//
// PassthroughTLS is byte-equal. MITMConversation routes WebSocket
// frames through wsmitm.Session and applies the same Phase F reducers
// the HTTP path uses where the Codex WSS envelope carries a request
// or streamed response body.
//
// IMPORTANT: PassthroughTLS in this engine does NOT mean "transparent
// on the wire". The engine terminated the client's TLS using the
// Slimference CA, so the client sees our leaf certificate. The
// "passthrough" wording means "no Phase F mutation"; the connection
// is still a re-TLSed bridge.
type PhaseFDispatcher struct {
	// Proxy gives the MITMConversation path access to the established
	// Phase F mutators and counters. nil keeps WSS in byte-equal
	// bridge mode, which is the fail-open behaviour for tests and
	// partial deployments.
	Proxy *Proxy
	// UpstreamDial is called to open a TLS connection to the real
	// upstream. The string passed in is "host:port". Tests inject a
	// fake dialer that returns an in-memory pipe.
	UpstreamDial func(ctx context.Context, hostPort string) (net.Conn, error)
	// Resolver optionally re-runs the SNI route after the first HTTP
	// request line and headers are available. The transparent.Engine's
	// first decision only has SNI; live Codex WSS routing needs path,
	// User-Agent, and Sec-WebSocket-Protocol to choose MITM safely.
	Resolver interface {
		Resolve(sniroute.Request) sniroute.Decision
	}
	// UpstreamPort overrides the destination port. Zero defaults to
	// 443.
	UpstreamPort int
	// BridgeTimeout caps the bridging goroutine when both directions
	// stall. Zero defaults to no timeout (the bridge runs until either
	// side closes).
	BridgeTimeout time.Duration

	// Counters surface per-dispatcher activity to /admin/state.
	counters       DispatcherCounters
	activeMu       sync.Mutex
	activeNext     atomic.Uint64
	activeSessions map[uint64]activeWSMITMSession
	recentSockets  []WSSSocketLifecycleTelemetry
}

type activeWSMITMSession struct {
	session *wsmitm.Session
	adapter *wsPhaseFAdapter
}

// DispatcherCounters captures the observable state of the dispatcher.
// All fields are monotonic and read via Snapshot.
type DispatcherCounters struct {
	passthroughBridged atomic.Int64
	mitmBridged        atomic.Int64
	// phasefBridged counts raw-scoped Codex WSS sessions that entered the
	// Phase-F frame path (FrameBridge). It increments once per phasef
	// conversation at upgrade time, so it is a reliable, lag-free signal that a
	// Desktop/CLI conversation reached the savings route - unlike the byte/frame
	// counters which accumulate during the turn and lag the sampled snapshot.
	phasefBridged    atomic.Int64
	rejected         atomic.Int64
	upstreamDialFail atomic.Int64
	bytesC2S         atomic.Int64
	bytesS2C         atomic.Int64
	// WSS-layer counters (T199-C2). Aggregated across all MITM sessions
	// because individual Session.Snapshot is per-conversation and
	// disappears when the conversation ends. These tick whenever a
	// frame parses/passes through.
	wsmitmC2SFrames           atomic.Int64
	wsmitmS2CFrames           atomic.Int64
	wsmitmParseFailures       atomic.Int64
	wsmitmDegraded            atomic.Int64
	wsmitmReencoded           atomic.Int64
	wsmitmForwarded           atomic.Int64
	wsmitmCompressedInspected atomic.Int64
	wsmitmCompressedMutated   atomic.Int64
	wsmitmCompressedBypassed  atomic.Int64
	wsmitmCompressionErrors   atomic.Int64
	wsmitmPhaseFRequests      atomic.Int64
	wsmitmPhaseFRequestBodies atomic.Int64
	wsmitmPhaseFIndexed       atomic.Int64
	wsmitmPhaseFTextDeltas    atomic.Int64
	wsmitmPhaseFTerminals     atomic.Int64
	wsmitmPhaseFMutations     atomic.Int64
	wsmitmSocketsClosed       atomic.Int64
	wsmitmClientEOF           atomic.Int64
	wsmitmUpstreamEOF         atomic.Int64
	wsmitmClientErrors        atomic.Int64
	wsmitmUpstreamErrors      atomic.Int64
	wsmitmOurErrors           atomic.Int64
	wsmitmContextCancels      atomic.Int64
}

// DispatcherTelemetry is the snapshot type for /admin/state.
type DispatcherTelemetry struct {
	PassthroughBridged        int64                         `json:"passthrough_bridged"`
	MITMBridged               int64                         `json:"mitm_bridged"`
	PhasefBridged             int64                         `json:"phasef_bridged"`
	Rejected                  int64                         `json:"rejected"`
	UpstreamDialFail          int64                         `json:"upstream_dial_failures"`
	BytesC2S                  int64                         `json:"bytes_c2s"`
	BytesS2C                  int64                         `json:"bytes_s2c"`
	WSMITMC2SFrames           int64                         `json:"wsmitm_c2s_frames"`
	WSMITMS2CFrames           int64                         `json:"wsmitm_s2c_frames"`
	WSMITMParseFailures       int64                         `json:"wsmitm_parse_failures"`
	WSMITMDegraded            int64                         `json:"wsmitm_degraded_sessions"`
	WSMITMReencoded           int64                         `json:"wsmitm_reencoded_frames"`
	WSMITMForwarded           int64                         `json:"wsmitm_forwarded_frames"`
	WSMITMCompressedInspected int64                         `json:"wsmitm_compressed_messages_inspected"`
	WSMITMCompressedMutated   int64                         `json:"wsmitm_compressed_messages_mutated"`
	WSMITMCompressedBypassed  int64                         `json:"wsmitm_compressed_messages_bypassed"`
	WSMITMCompressionErrors   int64                         `json:"wsmitm_compression_errors"`
	WSMITMPhaseFRequests      int64                         `json:"wsmitm_phasef_requests"`
	WSMITMPhaseFRequestBodies int64                         `json:"wsmitm_phasef_request_bodies"`
	WSMITMPhaseFIndexed       int64                         `json:"wsmitm_phasef_request_messages_indexed"`
	WSMITMPhaseFTextDeltas    int64                         `json:"wsmitm_phasef_text_deltas"`
	WSMITMPhaseFTerminals     int64                         `json:"wsmitm_phasef_terminal_responses"`
	WSMITMPhaseFMutations     int64                         `json:"wsmitm_phasef_mutations"`
	WSMITMSocketsClosed       int64                         `json:"wsmitm_sockets_closed"`
	WSMITMClientEOF           int64                         `json:"wsmitm_client_eof"`
	WSMITMUpstreamEOF         int64                         `json:"wsmitm_upstream_eof"`
	WSMITMClientErrors        int64                         `json:"wsmitm_client_errors"`
	WSMITMUpstreamErrors      int64                         `json:"wsmitm_upstream_errors"`
	WSMITMOurErrors           int64                         `json:"wsmitm_our_errors"`
	WSMITMContextCancels      int64                         `json:"wsmitm_context_cancels"`
	RecentSockets             []WSSSocketLifecycleTelemetry `json:"recent_sockets,omitempty"`
}

type WSSSocketLifecycleTelemetry struct {
	SocketSeq        uint64 `json:"socket_seq"`
	OpenedAtUnixNano int64  `json:"opened_at_unix_nano"`
	ClosedAtUnixNano int64  `json:"closed_at_unix_nano"`
	AgeMillis        int64  `json:"age_millis"`
	CloseInitiator   string `json:"close_initiator,omitempty"`
	CloseError       string `json:"close_error,omitempty"`
	C2SFrames        int64  `json:"c2s_frames"`
	S2CFrames        int64  `json:"s2c_frames"`
	C2SBytes         int64  `json:"c2s_bytes"`
	S2CBytes         int64  `json:"s2c_bytes"`
	TurnsCompleted   int64  `json:"turns_completed"`
	Active           bool   `json:"active"`
}

const wssRecentSocketLifecycleLimit = 16

// Snapshot returns a value-copy of the counters.
func (d *PhaseFDispatcher) Snapshot() DispatcherTelemetry {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()

	out := DispatcherTelemetry{
		PassthroughBridged:        d.counters.passthroughBridged.Load(),
		MITMBridged:               d.counters.mitmBridged.Load(),
		PhasefBridged:             d.counters.phasefBridged.Load(),
		Rejected:                  d.counters.rejected.Load(),
		UpstreamDialFail:          d.counters.upstreamDialFail.Load(),
		BytesC2S:                  d.counters.bytesC2S.Load(),
		BytesS2C:                  d.counters.bytesS2C.Load(),
		WSMITMC2SFrames:           d.counters.wsmitmC2SFrames.Load(),
		WSMITMS2CFrames:           d.counters.wsmitmS2CFrames.Load(),
		WSMITMParseFailures:       d.counters.wsmitmParseFailures.Load(),
		WSMITMDegraded:            d.counters.wsmitmDegraded.Load(),
		WSMITMReencoded:           d.counters.wsmitmReencoded.Load(),
		WSMITMForwarded:           d.counters.wsmitmForwarded.Load(),
		WSMITMCompressedInspected: d.counters.wsmitmCompressedInspected.Load(),
		WSMITMCompressedMutated:   d.counters.wsmitmCompressedMutated.Load(),
		WSMITMCompressedBypassed:  d.counters.wsmitmCompressedBypassed.Load(),
		WSMITMCompressionErrors:   d.counters.wsmitmCompressionErrors.Load(),
		WSMITMPhaseFRequests:      d.counters.wsmitmPhaseFRequests.Load(),
		WSMITMPhaseFRequestBodies: d.counters.wsmitmPhaseFRequestBodies.Load(),
		WSMITMPhaseFIndexed:       d.counters.wsmitmPhaseFIndexed.Load(),
		WSMITMPhaseFTextDeltas:    d.counters.wsmitmPhaseFTextDeltas.Load(),
		WSMITMPhaseFTerminals:     d.counters.wsmitmPhaseFTerminals.Load(),
		WSMITMPhaseFMutations:     d.counters.wsmitmPhaseFMutations.Load(),
		WSMITMSocketsClosed:       d.counters.wsmitmSocketsClosed.Load(),
		WSMITMClientEOF:           d.counters.wsmitmClientEOF.Load(),
		WSMITMUpstreamEOF:         d.counters.wsmitmUpstreamEOF.Load(),
		WSMITMClientErrors:        d.counters.wsmitmClientErrors.Load(),
		WSMITMUpstreamErrors:      d.counters.wsmitmUpstreamErrors.Load(),
		WSMITMOurErrors:           d.counters.wsmitmOurErrors.Load(),
		WSMITMContextCancels:      d.counters.wsmitmContextCancels.Load(),
	}
	for id, active := range d.activeSessions {
		var phaseF wsPhaseFTelemetry
		if active.adapter != nil {
			phaseF = active.adapter.snapshot()
		}
		if active.session != nil {
			snap := active.session.Snapshot()
			addSessionTelemetryToDispatcher(&out, snap)
			out.RecentSockets = append(out.RecentSockets, wssSocketLifecycleTelemetry(id, snap, phaseF, true))
		}
		if active.adapter != nil {
			addPhaseFTelemetryToDispatcher(&out, phaseF)
		}
	}
	out.RecentSockets = append(out.RecentSockets, d.recentSockets...)
	return out
}

// Handle implements transparent.Dispatcher.
func (d *PhaseFDispatcher) Handle(ctx context.Context, dec sniroute.Decision,
	req sniroute.Request, conn net.Conn) error {

	switch dec {
	case sniroute.Reject:
		d.counters.rejected.Add(1)
		return nil // closing conn is the caller's job

	case sniroute.PassthroughTLS:
		if d.UpstreamDial == nil {
			return errors.New("wsmitm_dispatcher: UpstreamDial not configured")
		}
		hostPort := upstreamHostPort(req.SNI, d.UpstreamPort)
		upstream, err := d.UpstreamDial(ctx, hostPort)
		if err != nil {
			d.counters.upstreamDialFail.Add(1)
			return err
		}
		defer upstream.Close()
		if d.Resolver == nil {
			d.counters.passthroughBridged.Add(1)
			return d.bridge(ctx, conn, upstream)
		}
		return d.routeInitialHTTP(ctx, dec, req, conn, upstream)

	case sniroute.MITMConversation:
		if d.UpstreamDial == nil {
			return errors.New("wsmitm_dispatcher: UpstreamDial not configured")
		}
		hostPort := upstreamHostPort(req.SNI, d.UpstreamPort)
		upstream, err := d.UpstreamDial(ctx, hostPort)
		if err != nil {
			d.counters.upstreamDialFail.Add(1)
			return err
		}
		defer upstream.Close()
		if d.Resolver == nil {
			d.counters.mitmBridged.Add(1)
			return d.runWSMITM(ctx, conn, upstream, WebSocketBridgeOptions{})
		}
		return d.routeInitialHTTP(ctx, dec, req, conn, upstream)
	default:
		return nil
	}
}

const initialHTTPHeaderLimit = 64 << 10

// routeInitialHTTP preserves the original request bytes while still giving the
// router the post-TLS facts it needs for Codex WSS: path, method, User-Agent,
// and Sec-WebSocket-Protocol. If the first bytes are not an HTTP/1.x request
// header (for example HTTP/2 preface or arbitrary test bytes), it writes the
// bytes it already consumed and falls back to the byte bridge.
func (d *PhaseFDispatcher) routeInitialHTTP(ctx context.Context, initial sniroute.Decision,
	req sniroute.Request, client, upstream net.Conn) error {

	header, err := readHTTPHeader(client, initialHTTPHeaderLimit)
	if err != nil {
		if len(header) > 0 {
			if _, werr := upstream.Write(header); werr != nil {
				return werr
			}
		}
		d.counters.passthroughBridged.Add(1)
		return d.bridge(ctx, client, upstream)
	}
	parsed, ok := parseHTTPRequestHeader(header)
	if !ok {
		if _, err := upstream.Write(header); err != nil {
			return err
		}
		d.counters.passthroughBridged.Add(1)
		return d.bridge(ctx, client, upstream)
	}

	req.Method = parsed.method
	req.Path = parsed.path
	req.UserAgent = parsed.userAgent
	req.Subprotocol = parsed.subprotocol
	req.IsWebSocket = parsed.websocket

	decision := initial
	if d.Resolver != nil {
		decision = d.Resolver.Resolve(req)
	}
	if decision == sniroute.Reject {
		d.counters.rejected.Add(1)
		return nil
	}
	if _, err := upstream.Write(header); err != nil {
		return err
	}

	if decision == sniroute.MITMConversation && parsed.websocket {
		respHeader, err := readHTTPHeader(upstream, initialHTTPHeaderLimit)
		if err != nil {
			if len(respHeader) > 0 {
				_, _ = client.Write(respHeader)
			}
			return err
		}
		if _, err := client.Write(respHeader); err != nil {
			return err
		}
		opts := WebSocketBridgeOptions{
			UserAgent:    parsed.userAgent,
			ClientFamily: normalizeCodexClientFamily(parsed.userAgent),
			Extensions: wscompact.NegotiatePermessageDeflate(
				strings.Join(rawHTTPHeaderValues(header, "Sec-WebSocket-Extensions"), ", "),
				strings.Join(rawHTTPHeaderValues(respHeader, "Sec-WebSocket-Extensions"), ", "),
			),
		}
		d.counters.mitmBridged.Add(1)
		return d.runWSMITM(ctx, client, upstream, opts)
	}

	// Non-WebSocket HTTP conversation mutation is intentionally not
	// implemented in the transparent dispatcher. Preserve bytes and
	// let the existing loopback HTTP proxy path cover explicit API-key
	// testing.
	d.counters.passthroughBridged.Add(1)
	return d.bridge(ctx, client, upstream)
}

func readHTTPHeader(c net.Conn, limit int) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 1)
	for buf.Len() < limit {
		n, err := c.Read(tmp)
		if n > 0 {
			_ = buf.WriteByte(tmp[0])
			b := buf.Bytes()
			if bytes.Contains(b, []byte("\r\n\r\n")) || bytes.Contains(b, []byte("\n\n")) {
				return append([]byte(nil), b...), nil
			}
		}
		if err != nil {
			return append([]byte(nil), buf.Bytes()...), err
		}
	}
	return append([]byte(nil), buf.Bytes()...), errors.New("wsmitm_dispatcher: initial HTTP header too large")
}

type parsedHTTPRequestHeader struct {
	method      string
	path        string
	userAgent   string
	subprotocol string
	websocket   bool
}

func parseHTTPRequestHeader(header []byte) (parsedHTTPRequestHeader, bool) {
	text := strings.ReplaceAll(string(header), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	parts := strings.Fields(lines[0])
	if len(parts) < 3 || !strings.HasPrefix(parts[2], "HTTP/1.") {
		return parsedHTTPRequestHeader{}, false
	}
	out := parsedHTTPRequestHeader{method: parts[0], path: normaliseHTTPRequestTarget(parts[1])}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		value := strings.TrimSpace(v)
		switch key {
		case "user-agent":
			out.userAgent = value
		case "sec-websocket-protocol":
			out.subprotocol = value
		case "upgrade":
			out.websocket = strings.EqualFold(value, "websocket")
		}
	}
	return out, true
}

// runWSMITM routes the MITMConversation path through wsmitm.Session.
// The session reads frames from both directions, lets a small Phase F
// adapter mutate known Codex request/response envelopes, and forwards
// everything unknown byte-equal.
//
// Counters surface to /admin/state via Snapshot() so an operator can
// watch frame throughput live.
func (d *PhaseFDispatcher) runWSMITM(ctx context.Context, client, upstream net.Conn, opts WebSocketBridgeOptions) error {
	if d.BridgeTimeout > 0 {
		dl := time.Now().Add(d.BridgeTimeout)
		_ = client.SetDeadline(dl)
		_ = upstream.SetDeadline(dl)
	}
	adapter := d.newWSPhaseFAdapter()
	adapter.setBridgeClientFamily(opts.ClientFamily)
	adapter.setHandshakeUserAgent(opts.UserAgent)
	upstream = newLockedWriteConn(upstream)
	adapter.setRecoveryWriter(func(payload []byte) error {
		return writeMaskedWSTextFrame(upstream, payload)
	})
	capture := newWSSABReplayCaptureFromEnv()
	defer capture.Close()
	sess := &wsmitm.Session{
		Client:          client,
		Upstream:        upstream,
		ClientHandler:   capture.Wrap(adapter.handle),
		UpstreamHandler: capture.Wrap(adapter.handle),
		Extensions:      opts.Extensions,
	}
	activeID := d.registerActiveWSMITMSession(sess, adapter)
	err := sess.Serve(ctx)
	snap := sess.Snapshot()
	phaseF := adapter.snapshot()
	d.finishActiveWSMITMSession(activeID, snap, phaseF)
	return err
}

// runWSBridge keeps Codex on the native WSS path while disabling all
// Phase-F mutation handlers. It still parses WebSocket framing and
// permessage-deflate enough to surface bridge health counters.
func (d *PhaseFDispatcher) runWSBridge(ctx context.Context, client, upstream net.Conn, opts WebSocketBridgeOptions) error {
	if d.BridgeTimeout > 0 {
		dl := time.Now().Add(d.BridgeTimeout)
		_ = client.SetDeadline(dl)
		_ = upstream.SetDeadline(dl)
	}
	sess := &wsmitm.Session{
		Client:     client,
		Upstream:   upstream,
		Extensions: opts.Extensions,
	}
	activeID := d.registerActiveWSMITMSession(sess, nil)
	err := sess.Serve(ctx)
	snap := sess.Snapshot()
	d.finishActiveWSMITMSession(activeID, snap, wsPhaseFTelemetry{})
	return err
}

func (d *PhaseFDispatcher) registerActiveWSMITMSession(session *wsmitm.Session, adapter *wsPhaseFAdapter) uint64 {
	if d == nil || session == nil {
		return 0
	}
	id := d.activeNext.Add(1)
	if adapter != nil {
		adapter.setSocketSeq(id)
	}
	d.activeMu.Lock()
	defer d.activeMu.Unlock()
	if d.activeSessions == nil {
		d.activeSessions = make(map[uint64]activeWSMITMSession)
	}
	d.activeSessions[id] = activeWSMITMSession{session: session, adapter: adapter}
	return id
}

func (d *PhaseFDispatcher) finishActiveWSMITMSession(id uint64, snap wsmitm.SessionTelemetry, phaseF wsPhaseFTelemetry) {
	if d == nil {
		return
	}
	var adapter *wsPhaseFAdapter
	d.activeMu.Lock()
	if id != 0 && d.activeSessions != nil {
		adapter = d.activeSessions[id].adapter
		delete(d.activeSessions, id)
	}
	d.activeMu.Unlock()
	d.counters.wsmitmC2SFrames.Add(snap.C2SFrames)
	d.counters.wsmitmS2CFrames.Add(snap.S2CFrames)
	d.counters.wsmitmParseFailures.Add(snap.ParseFailures)
	d.counters.wsmitmReencoded.Add(snap.FramesReencoded)
	d.counters.wsmitmForwarded.Add(snap.FramesForwarded)
	d.counters.wsmitmCompressedInspected.Add(snap.CompressedMessagesInspected)
	d.counters.wsmitmCompressedMutated.Add(snap.CompressedMessagesMutated)
	d.counters.wsmitmCompressedBypassed.Add(snap.CompressedMessagesBypassed)
	d.counters.wsmitmCompressionErrors.Add(snap.CompressionErrors)
	d.counters.bytesC2S.Add(snap.C2SBytes)
	d.counters.bytesS2C.Add(snap.S2CBytes)
	if snap.Degraded {
		d.counters.wsmitmDegraded.Add(1)
	}
	if adapter != nil {
		adapter.attachWSSSocketLifecycle(snap, phaseF)
	}
	d.activeMu.Lock()
	d.recordSocketLifecycleLocked(id, snap, phaseF)
	d.activeMu.Unlock()
	addPhaseFTelemetryToCounters(&d.counters, phaseF)
}

func (d *PhaseFDispatcher) recordSocketLifecycleLocked(id uint64, snap wsmitm.SessionTelemetry, phaseF wsPhaseFTelemetry) {
	if snap.CloseInitiator == "" {
		return
	}
	d.counters.wsmitmSocketsClosed.Add(1)
	switch snap.CloseInitiator {
	case "client_eof":
		d.counters.wsmitmClientEOF.Add(1)
	case "upstream_eof":
		d.counters.wsmitmUpstreamEOF.Add(1)
	case "client_error":
		d.counters.wsmitmClientErrors.Add(1)
	case "upstream_error":
		d.counters.wsmitmUpstreamErrors.Add(1)
	case "our_error":
		d.counters.wsmitmOurErrors.Add(1)
	case "context_cancel":
		d.counters.wsmitmContextCancels.Add(1)
	}
	lifecycle := wssSocketLifecycleTelemetry(id, snap, phaseF, false)
	d.recentSockets = append([]WSSSocketLifecycleTelemetry{lifecycle}, d.recentSockets...)
	if len(d.recentSockets) > wssRecentSocketLifecycleLimit {
		d.recentSockets = d.recentSockets[:wssRecentSocketLifecycleLimit]
	}
}

func wssSocketLifecycleTelemetry(id uint64, snap wsmitm.SessionTelemetry, phaseF wsPhaseFTelemetry, active bool) WSSSocketLifecycleTelemetry {
	return WSSSocketLifecycleTelemetry{
		SocketSeq:        id,
		OpenedAtUnixNano: snap.OpenedAtUnixNano,
		ClosedAtUnixNano: snap.ClosedAtUnixNano,
		AgeMillis:        snap.AgeMillis,
		CloseInitiator:   snap.CloseInitiator,
		CloseError:       snap.CloseError,
		C2SFrames:        snap.C2SFrames,
		S2CFrames:        snap.S2CFrames,
		C2SBytes:         snap.C2SBytes,
		S2CBytes:         snap.S2CBytes,
		TurnsCompleted:   phaseF.TerminalResponsesSeen,
		Active:           active,
	}
}

func addSessionTelemetryToDispatcher(out *DispatcherTelemetry, snap wsmitm.SessionTelemetry) {
	if out == nil {
		return
	}
	out.WSMITMC2SFrames += snap.C2SFrames
	out.WSMITMS2CFrames += snap.S2CFrames
	out.WSMITMParseFailures += snap.ParseFailures
	out.WSMITMReencoded += snap.FramesReencoded
	out.WSMITMForwarded += snap.FramesForwarded
	out.WSMITMCompressedInspected += snap.CompressedMessagesInspected
	out.WSMITMCompressedMutated += snap.CompressedMessagesMutated
	out.WSMITMCompressedBypassed += snap.CompressedMessagesBypassed
	out.WSMITMCompressionErrors += snap.CompressionErrors
	out.BytesC2S += snap.C2SBytes
	out.BytesS2C += snap.S2CBytes
	if snap.Degraded {
		out.WSMITMDegraded++
	}
}

func addPhaseFTelemetryToDispatcher(out *DispatcherTelemetry, phaseF wsPhaseFTelemetry) {
	if out == nil {
		return
	}
	out.WSMITMPhaseFRequests += phaseF.RequestsSeen
	out.WSMITMPhaseFRequestBodies += phaseF.RequestBodiesSeen
	out.WSMITMPhaseFIndexed += phaseF.RequestMessagesIndexed
	out.WSMITMPhaseFTextDeltas += phaseF.ResponseTextDeltasSeen
	out.WSMITMPhaseFTerminals += phaseF.TerminalResponsesSeen
	out.WSMITMPhaseFMutations += phaseF.Mutations
}

func addPhaseFTelemetryToCounters(counters *DispatcherCounters, phaseF wsPhaseFTelemetry) {
	if counters == nil {
		return
	}
	counters.wsmitmPhaseFRequests.Add(phaseF.RequestsSeen)
	counters.wsmitmPhaseFRequestBodies.Add(phaseF.RequestBodiesSeen)
	counters.wsmitmPhaseFIndexed.Add(phaseF.RequestMessagesIndexed)
	counters.wsmitmPhaseFTextDeltas.Add(phaseF.ResponseTextDeltasSeen)
	counters.wsmitmPhaseFTerminals.Add(phaseF.TerminalResponsesSeen)
	counters.wsmitmPhaseFMutations.Add(phaseF.Mutations)
}

// bridge copies bytes both directions between client and upstream
// until either side closes. The first goroutine to finish closes the
// other half to unblock its peer.
func (d *PhaseFDispatcher) bridge(ctx context.Context, client, upstream net.Conn) error {
	if d.BridgeTimeout > 0 {
		dl := time.Now().Add(d.BridgeTimeout)
		_ = client.SetDeadline(dl)
		_ = upstream.SetDeadline(dl)
	}
	errC := make(chan error, 2)
	go func() {
		n, err := io.Copy(upstream, client)
		d.counters.bytesC2S.Add(n)
		_ = closeWrite(upstream)
		errC <- err
	}()
	go func() {
		n, err := io.Copy(client, upstream)
		d.counters.bytesS2C.Add(n)
		_ = closeWrite(client)
		errC <- err
	}()
	// Wait for both directions OR ctx cancellation.
	var first error
	for i := 0; i < 2; i++ {
		select {
		case err := <-errC:
			if first == nil && err != nil && !errors.Is(err, io.EOF) {
				first = err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return first
}

// closeWrite half-closes the write side of a connection so the peer
// sees EOF. For TLS / generic conns that don't support CloseWrite, we
// fall back to a full Close.
func closeWrite(c net.Conn) error {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Close()
}

// upstreamHostPort returns "sni:port", defaulting port to 443 when
// the dispatcher's UpstreamPort field is zero.
func upstreamHostPort(sni string, port int) string {
	if port == 0 {
		port = 443
	}
	return net.JoinHostPort(sni, itoa(port))
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [16]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// DefaultUpstreamDial returns a real TLS dialer that connects to the
// upstream host on the supplied port. ServerName is set to the SNI in
// hostPort so the upstream answers with its real certificate.
//
// CRITICAL for transparent MITM: when /etc/hosts redirects chatgpt.com
// → 127.0.0.1 the daemon would dial ITSELF if it used the system
// resolver. We therefore resolve via a public DNS-over-HTTPS endpoint
// that ignores /etc/hosts entirely. Falls back to the system resolver
// only when DoH fails.
func DefaultUpstreamDial() func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, hostPort string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(hostPort)
		if err != nil {
			return nil, err
		}
		// Bypass /etc/hosts: resolve via DoH and connect by IP.
		ip, derr := resolveUpstreamIP(ctx, host)
		if derr == nil {
			ipHostPort := net.JoinHostPort(ip, port)
			raw, err := upstreamTCPDialContextFn(ctx, "tcp", ipHostPort)
			if err == nil {
				return wrapTLSConnFn(ctx, raw, host)
			}
			// Fall through to system resolver if direct IP fails.
		}
		raw, err := upstreamTCPDialContextFn(ctx, "tcp", hostPort)
		if err != nil {
			return nil, err
		}
		return wrapTLSConnFn(ctx, raw, host)
	}
}

var (
	upstreamTCPDialContextFn = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: 8 * time.Second}
		return dialer.DialContext(ctx, network, address)
	}
	wrapTLSConnFn = wrapTLS
)

// Compile-time check that PhaseFDispatcher implements the interface.
var _ transparent.Dispatcher = (*PhaseFDispatcher)(nil)

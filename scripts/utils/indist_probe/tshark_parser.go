package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/indist"
)

// tsharkPacket is the shape of one element in tshark's `-T json`
// output. tshark wraps every dissected layer in an `_ws.col` /
// `_source.layers` tree; this struct only declares the fields we
// care about. Unknown fields are silently ignored.
type tsharkPacket struct {
	Source struct {
		Layers tsharkLayers `json:"layers"`
	} `json:"_source"`
}

type tsharkLayers struct {
	TLS       *tsharkTLS       `json:"tls,omitempty"`
	HTTP2     *tsharkHTTP2     `json:"http2,omitempty"`
	WebSocket *tsharkWebSocket `json:"websocket,omitempty"`
}

// tsharkTLS holds the tls dissection output. The structure tshark
// produces is deeply nested under `tls.record.handshake.extension`.
// We use json.RawMessage to grab the inner blob and lazily parse it.
type tsharkTLS struct {
	Record tsharkTLSRecord `json:"tls.record"`
}

type tsharkTLSRecord struct {
	Handshake json.RawMessage `json:"tls.handshake,omitempty"`
}

// ClientHelloFields is the extracted ClientHello data from tshark.
type ClientHelloFields struct {
	SNI        string
	ALPN       []string
	Ciphers    []uint16
	Extensions []uint16
	Curves     []uint16
	HasGREASE  bool
	JA3        string
	JA3Hash    string
	JA4        string
}

// HandshakeExtensions parses the handshake JSON for ClientHello data.
// tshark's field paths within tls.handshake are:
//
//	tls.handshake.extensions_server_name → SNI
//	tls.handshake.extensions_alpn_str    → ALPN protocols
//	tls.handshake.ciphersuite            → cipher list
//	tls.handshake.extension.type         → extension IDs
//	tls.handshake.extensions_supported_group → curve IDs
//	tls.handshake.ja3, .ja3_full, .ja4   → fingerprint strings
//
// We accept either string-typed or array-typed fields because tshark
// varies presentation based on count.
func (r tsharkTLSRecord) HandshakeExtensions() *ClientHelloFields {
	if len(r.Handshake) == 0 {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(r.Handshake, &raw); err != nil {
		// tshark emits an array of handshakes when the record contains
		// multiple; try as array.
		var arr []map[string]json.RawMessage
		if err2 := json.Unmarshal(r.Handshake, &arr); err2 != nil {
			return nil
		}
		// Find the ClientHello (type=1).
		for _, h := range arr {
			if t, _ := stringField(h["tls.handshake.type"]); t == "1" {
				raw = h
				break
			}
		}
		if raw == nil {
			return nil
		}
	}

	// Restrict to ClientHello (type=1).
	if t, _ := stringField(raw["tls.handshake.type"]); t != "" && t != "1" {
		return nil
	}

	out := &ClientHelloFields{}
	out.SNI, _ = stringField(raw["tls.handshake.extensions_server_name"])
	out.ALPN, _ = stringSliceField(raw["tls.handshake.extensions_alpn_str"])
	out.Ciphers, _ = uint16SliceField(raw["tls.handshake.ciphersuite"])
	out.Extensions, _ = uint16SliceField(raw["tls.handshake.extension.type"])
	out.Curves, _ = uint16SliceField(raw["tls.handshake.extensions_supported_group"])
	if s, _ := stringField(raw["tls.handshake.ja3"]); s != "" {
		out.JA3 = s
	}
	if s, _ := stringField(raw["tls.handshake.ja3_hash"]); s != "" {
		out.JA3Hash = s
	}
	if s, _ := stringField(raw["tls.handshake.ja4"]); s != "" {
		out.JA4 = s
	}
	// GREASE detection: any cipher / extension / curve whose low byte
	// matches 0x0A and high nibble matches 0xF is GREASE.
	for _, list := range [][]uint16{out.Ciphers, out.Extensions, out.Curves} {
		for _, v := range list {
			if (v&0x0f0f) == 0x0a0a && (v&0xf000) >= 0xa000 {
				out.HasGREASE = true
				break
			}
		}
	}
	return out
}

// tsharkHTTP2 holds the http2 dissection output for SETTINGS and
// header order extraction. tshark places SETTINGS under
// `http2.settings.parameter` and headers under `http2.header`.
type tsharkHTTP2 struct {
	Stream []tsharkHTTP2Stream `json:"http2.stream,omitempty"`
}

type tsharkHTTP2Stream struct {
	Headers []tsharkHTTP2Header `json:"http2.header,omitempty"`
}

type tsharkHTTP2Header struct {
	Name  string `json:"http2.header.name,omitempty"`
	Value string `json:"http2.header.value,omitempty"`
}

// Settings returns the parsed HTTP/2 SETTINGS list. tshark's
// settings dissection is complex; this implementation returns nil
// when settings can't be extracted (operator falls back to JA3/JA4
// alone for fingerprint).
func (h tsharkHTTP2) Settings() []indist.H2Setting {
	// Best-effort: tshark splits SETTINGS into per-parameter dict
	// entries that don't fit a simple Go decode. Leaving extraction
	// to a future pass once we have real captures to template on.
	return nil
}

// PseudoHeaderOrder returns the sequence of HTTP/2 pseudo-headers
// (those starting with ":"). Order matters for fingerprinting.
func (h tsharkHTTP2) PseudoHeaderOrder() []string {
	var out []string
	for _, stream := range h.Stream {
		for _, hdr := range stream.Headers {
			if strings.HasPrefix(hdr.Name, ":") {
				out = append(out, hdr.Name)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// HeaderOrder returns the order of regular (non-pseudo) headers.
func (h tsharkHTTP2) HeaderOrder() []string {
	var out []string
	for _, stream := range h.Stream {
		for _, hdr := range stream.Headers {
			if !strings.HasPrefix(hdr.Name, ":") {
				out = append(out, hdr.Name)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// tsharkWebSocket holds the websocket dissection for Upgrade-header
// extraction. tshark's websocket layer has the post-upgrade frames,
// but the Upgrade GET is in an http layer. We accept either source.
type tsharkWebSocket struct {
	Extensions  string `json:"websocket.extensions,omitempty"`
	Subprotocol string `json:"websocket.subprotocol,omitempty"`
	Version     string `json:"websocket.version,omitempty"`
}

// stringField extracts a single string from tshark's RawMessage,
// handling both string-typed and singleton-array-typed values.
func stringField(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return arr[0], true
	}
	return "", false
}

// stringSliceField extracts a comma- or space-separated list.
func stringSliceField(raw json.RawMessage) ([]string, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		// tshark uses ", " as separator in stringified lists.
		parts := strings.Split(s, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts, true
	}
	return nil, false
}

// uint16SliceField parses tshark's hex-coded uint16 list. tshark
// formats cipher / extension / curve IDs as either decimal strings
// or 0x-prefixed hex.
func uint16SliceField(raw json.RawMessage) ([]uint16, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	parse := func(s string) (uint16, bool) {
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, false
		}
		base := 10
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			s = s[2:]
			base = 16
		}
		n, err := strconv.ParseUint(s, base, 16)
		if err != nil {
			return 0, false
		}
		return uint16(n), true
	}

	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		out := make([]uint16, 0, len(arr))
		for _, s := range arr {
			if v, ok := parse(s); ok {
				out = append(out, v)
			}
		}
		return out, len(out) > 0
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		parts := strings.Split(s, ",")
		out := make([]uint16, 0, len(parts))
		for _, p := range parts {
			if v, ok := parse(p); ok {
				out = append(out, v)
			}
		}
		return out, len(out) > 0
	}
	return nil, false
}

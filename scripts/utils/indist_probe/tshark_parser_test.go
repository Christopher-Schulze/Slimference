package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStringFieldString(t *testing.T) {
	got, ok := stringField(json.RawMessage(`"chatgpt.com"`))
	if !ok || got != "chatgpt.com" {
		t.Errorf("got %q ok=%v", got, ok)
	}
}

func TestStringFieldSingletonArray(t *testing.T) {
	got, ok := stringField(json.RawMessage(`["chatgpt.com"]`))
	if !ok || got != "chatgpt.com" {
		t.Errorf("got %q ok=%v", got, ok)
	}
}

func TestStringFieldEmpty(t *testing.T) {
	_, ok := stringField(json.RawMessage(``))
	if ok {
		t.Error("empty should be false")
	}
}

func TestStringSliceFieldArray(t *testing.T) {
	got, ok := stringSliceField(json.RawMessage(`["h2","http/1.1"]`))
	if !ok || len(got) != 2 {
		t.Errorf("got %v ok=%v", got, ok)
	}
}

func TestStringSliceFieldCommaString(t *testing.T) {
	got, ok := stringSliceField(json.RawMessage(`"h2, http/1.1"`))
	if !ok || len(got) != 2 || got[1] != "http/1.1" {
		t.Errorf("got %v ok=%v", got, ok)
	}
}

func TestUint16SliceFieldHex(t *testing.T) {
	got, ok := uint16SliceField(json.RawMessage(`["0x1301","0x1302"]`))
	if !ok || len(got) != 2 || got[0] != 0x1301 || got[1] != 0x1302 {
		t.Errorf("got %v ok=%v", got, ok)
	}
}

func TestUint16SliceFieldDecimal(t *testing.T) {
	got, ok := uint16SliceField(json.RawMessage(`["4865","4866"]`))
	if !ok || got[0] != 4865 || got[1] != 4866 {
		t.Errorf("got %v ok=%v", got, ok)
	}
}

func TestUint16SliceFieldCommaString(t *testing.T) {
	got, ok := uint16SliceField(json.RawMessage(`"0x1301, 0x1302, 0x1303"`))
	if !ok || len(got) != 3 {
		t.Errorf("got %v ok=%v", got, ok)
	}
}

func TestHandshakeExtensionsClientHello(t *testing.T) {
	body := json.RawMessage(`{
		"tls.handshake.type": "1",
		"tls.handshake.extensions_server_name": "chatgpt.com",
		"tls.handshake.extensions_alpn_str": ["h2", "http/1.1"],
		"tls.handshake.ciphersuite": ["0x1301", "0x1302", "0x1303"],
		"tls.handshake.extension.type": ["0", "16", "10"],
		"tls.handshake.extensions_supported_group": ["0x001d", "0x0017"],
		"tls.handshake.ja3": "771,4865-4866-4867,0-23-65281-10,29-23,0",
		"tls.handshake.ja3_hash": "deadbeefdeadbeefdeadbeefdeadbeef",
		"tls.handshake.ja4": "t13d1516h2_8daaf6152771_b186095e22b6"
	}`)
	rec := tsharkTLSRecord{Handshake: body}
	got := rec.HandshakeExtensions()
	if got == nil {
		t.Fatal("nil result")
	}
	if got.SNI != "chatgpt.com" {
		t.Errorf("SNI=%q", got.SNI)
	}
	if len(got.ALPN) != 2 {
		t.Errorf("ALPN=%v", got.ALPN)
	}
	if len(got.Ciphers) != 3 {
		t.Errorf("Ciphers=%v", got.Ciphers)
	}
	if got.JA3 == "" || got.JA4 == "" {
		t.Errorf("fingerprints missing: JA3=%q JA4=%q", got.JA3, got.JA4)
	}
}

func TestHandshakeExtensionsNotClientHello(t *testing.T) {
	body := json.RawMessage(`{"tls.handshake.type":"2"}`)
	rec := tsharkTLSRecord{Handshake: body}
	if got := rec.HandshakeExtensions(); got != nil {
		t.Errorf("ServerHello should yield nil, got %+v", got)
	}
}

func TestHandshakeExtensionsArrayOfHandshakes(t *testing.T) {
	body := json.RawMessage(`[
		{"tls.handshake.type":"2"},
		{"tls.handshake.type":"1","tls.handshake.extensions_server_name":"foo.com"}
	]`)
	rec := tsharkTLSRecord{Handshake: body}
	got := rec.HandshakeExtensions()
	if got == nil {
		t.Fatal("nil")
	}
	if got.SNI != "foo.com" {
		t.Errorf("SNI=%q", got.SNI)
	}
}

func TestHandshakeExtensionsGREASEDetection(t *testing.T) {
	// GREASE values follow pattern 0x?A0A, where ? matches A-F.
	body := json.RawMessage(`{
		"tls.handshake.type": "1",
		"tls.handshake.ciphersuite": ["0xCA0A", "0x1301"]
	}`)
	rec := tsharkTLSRecord{Handshake: body}
	got := rec.HandshakeExtensions()
	if got == nil || !got.HasGREASE {
		t.Errorf("GREASE not detected: %+v", got)
	}
}

func TestParseTSharkJSONEmpty(t *testing.T) {
	_, err := parseTSharkJSON([]byte("[]"), "label")
	if err == nil || !strings.Contains(err.Error(), "empty array") {
		t.Errorf("expected empty-array error, got %v", err)
	}
}

func TestParseTSharkJSONMissingHandshake(t *testing.T) {
	pkts := `[{"_source":{"layers":{}}}]`
	_, err := parseTSharkJSON([]byte(pkts), "label")
	if err == nil {
		t.Error("expected error on no clienthello")
	}
}

func TestParseTSharkJSONHappyPath(t *testing.T) {
	pkts := `[{"_source":{"layers":{
		"tls": {"tls.record": {"tls.handshake": {
			"tls.handshake.type": "1",
			"tls.handshake.extensions_server_name": "chatgpt.com",
			"tls.handshake.ciphersuite": ["0x1301"],
			"tls.handshake.extension.type": ["0"],
			"tls.handshake.ja3": "abc"
		}}}
	}}}]`
	cap, err := parseTSharkJSON([]byte(pkts), "test-label")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cap.Label != "test-label" {
		t.Errorf("label=%q", cap.Label)
	}
	if cap.SNI != "chatgpt.com" {
		t.Errorf("SNI=%q", cap.SNI)
	}
	if cap.JA3 != "abc" {
		t.Errorf("JA3=%q", cap.JA3)
	}
}

func TestHTTP2PseudoHeaderOrder(t *testing.T) {
	h := tsharkHTTP2{
		Stream: []tsharkHTTP2Stream{
			{Headers: []tsharkHTTP2Header{
				{Name: ":method"},
				{Name: ":path"},
				{Name: "user-agent"},
			}},
		},
	}
	got := h.PseudoHeaderOrder()
	if len(got) != 2 || got[0] != ":method" {
		t.Errorf("got %v", got)
	}
}

func TestHTTP2HeaderOrderSkipsPseudo(t *testing.T) {
	h := tsharkHTTP2{
		Stream: []tsharkHTTP2Stream{
			{Headers: []tsharkHTTP2Header{
				{Name: ":method"},
				{Name: "user-agent"},
				{Name: "accept"},
			}},
		},
	}
	got := h.HeaderOrder()
	if len(got) != 2 || got[0] != "user-agent" {
		t.Errorf("got %v", got)
	}
}

package wscompact

import "testing"

func TestParseExtensionsHeader(t *testing.T) {
	tokens := ParseExtensionsHeader(`permessage-deflate; client_no_context_takeover; server_max_window_bits=15, x-test; q="a,b;c"`)
	if len(tokens) != 2 {
		t.Fatalf("tokens=%d", len(tokens))
	}
	if tokens[0].Name != "permessage-deflate" {
		t.Fatalf("first token=%+v", tokens[0])
	}
	if _, ok := tokens[0].Params["client_no_context_takeover"]; !ok {
		t.Fatalf("missing no-context param: %+v", tokens[0].Params)
	}
	if tokens[0].Params["server_max_window_bits"] != "15" {
		t.Fatalf("window bits=%q", tokens[0].Params["server_max_window_bits"])
	}
	if tokens[1].Name != "x-test" || tokens[1].Params["q"] != "a,b;c" {
		t.Fatalf("quoted token=%+v", tokens[1])
	}
}

func TestParseExtensionsHeaderSkipsEmptyAndKeepsEscapedQuotes(t *testing.T) {
	tokens := ParseExtensionsHeader(`, ; ignored, permessage-deflate; note="a\"b"; flag, broken; =value`)
	if len(tokens) != 2 {
		t.Fatalf("tokens=%d: %+v", len(tokens), tokens)
	}
	if tokens[0].Name != "permessage-deflate" {
		t.Fatalf("unexpected first parsed token: %+v", tokens[0])
	}
	if got := tokens[0].Params["note"]; got != `a\"b` {
		t.Fatalf("escaped quote value=%q", got)
	}
	if _, ok := tokens[0].Params["flag"]; !ok {
		t.Fatalf("bare flag missing: %+v", tokens[0].Params)
	}
	if _, ok := tokens[1].Params[""]; ok {
		t.Fatalf("empty parameter key should be skipped: %+v", tokens[1].Params)
	}
}

func TestNegotiatePermessageDeflateAccepted(t *testing.T) {
	profile := NegotiatePermessageDeflate(
		`permessage-deflate; client_max_window_bits`,
		`permessage-deflate; client_no_context_takeover; server_no_context_takeover; client_max_window_bits=15; server_max_window_bits=15`,
	)
	if !profile.Supported || !profile.PermessageDeflate {
		t.Fatalf("profile not supported: %+v", profile)
	}
	if !profile.ClientNoContextTakeover || !profile.ServerNoContextTakeover {
		t.Fatalf("takeover flags not parsed: %+v", profile)
	}
	if profile.ClientMaxWindowBits != 15 || profile.ServerMaxWindowBits != 15 {
		t.Fatalf("window bits not parsed: %+v", profile)
	}
}

func TestNegotiatePermessageDeflateDefaultWindowBits(t *testing.T) {
	profile := NegotiatePermessageDeflate(
		`permessage-deflate`,
		`permessage-deflate; client_max_window_bits; server_max_window_bits`,
	)
	if !profile.Supported {
		t.Fatalf("profile unsupported: %+v", profile)
	}
	if profile.ClientMaxWindowBits != 15 || profile.ServerMaxWindowBits != 15 {
		t.Fatalf("default window bits not normalized to 15: %+v", profile)
	}
}

func TestNegotiatePermessageDeflateUnsupported(t *testing.T) {
	tests := []struct {
		name   string
		accept string
	}{
		{name: "missing", accept: ""},
		{name: "unknown extension", accept: "x-unknown"},
		{name: "bad window", accept: "permessage-deflate; server_max_window_bits=12"},
		{name: "invalid window", accept: "permessage-deflate; client_max_window_bits=bogus"},
		{name: "unknown parameter", accept: "permessage-deflate; zed=1"},
		{name: "invalid no context", accept: "permessage-deflate; client_no_context_takeover=true"},
		{name: "invalid server no context", accept: "permessage-deflate; server_no_context_takeover=true"},
		{name: "duplicate", accept: "permessage-deflate, permessage-deflate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			profile := NegotiatePermessageDeflate("", tc.accept)
			if profile.Supported || profile.UnsupportedReason == "" {
				t.Fatalf("expected unsupported reason, got %+v", profile)
			}
		})
	}
}

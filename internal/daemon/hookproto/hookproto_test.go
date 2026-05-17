package hookproto

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNewEnvelopeStampsVersion(t *testing.T) {
	env := NewEnvelope(OpPing, "abc")
	if env.Version != CurrentVersion {
		t.Fatalf("version: got %d, want %d", env.Version, CurrentVersion)
	}
	if env.Op != OpPing {
		t.Fatalf("op: got %v", env.Op)
	}
	if env.ID != "abc" {
		t.Fatalf("id: got %q", env.ID)
	}
}

func TestEncodeDecodeRoundtrip_Ping(t *testing.T) {
	var buf bytes.Buffer
	want := NewEnvelope(OpPing, "id-1")
	want.Request = &Request{Ping: &PingRequest{}}
	if err := Encode(&buf, want); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Op != OpPing || got.ID != "id-1" || got.Version != CurrentVersion {
		t.Fatalf("envelope drift: %+v", got)
	}
	if got.Request == nil || got.Request.Ping == nil {
		t.Fatalf("expected ping request payload")
	}
}

func TestEncodeDecodeRoundtrip_ForwardRequest(t *testing.T) {
	var buf bytes.Buffer
	want := NewEnvelope(OpForwardRequest, "id-2")
	want.Request = &Request{ForwardRequest: &ForwardRequest{
		Method: "POST",
		URL:    "https://chatgpt.com/backend-api/codex/responses",
		Headers: map[string][]string{
			"Authorization": {"Bearer redacted"},
			"Content-Type":  {"application/json"},
		},
		Body:     []byte(`{"model":"o4"}`),
		SourceUA: "codex-cli/0.5.2",
	}}
	if err := Encode(&buf, want); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Request == nil || got.Request.ForwardRequest == nil {
		t.Fatalf("expected forward_request payload")
	}
	fr := got.Request.ForwardRequest
	if fr.Method != "POST" || fr.URL != want.Request.ForwardRequest.URL {
		t.Fatalf("method/url drift: %+v", fr)
	}
	if fr.SourceUA != "codex-cli/0.5.2" {
		t.Fatalf("source UA drift: %q", fr.SourceUA)
	}
	if string(fr.Body) != `{"model":"o4"}` {
		t.Fatalf("body drift: %q", string(fr.Body))
	}
	if got := fr.Headers["Authorization"]; len(got) != 1 || got[0] != "Bearer redacted" {
		t.Fatalf("auth header drift: %v", got)
	}
}

func TestDecodeRejectsMissingVersion(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(`{"op":"ping"}` + "\n"))
	_, err := Decode(r)
	if err == nil || !strings.Contains(err.Error(), "missing version") {
		t.Fatalf("want missing-version error, got %v", err)
	}
}

func TestDecodeFlagsFutureVersion(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(`{"version":999,"op":"ping"}` + "\n"))
	_, err := Decode(r)
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("want ErrUnsupportedVersion, got %v", err)
	}
}

func TestDecodeEOFOnEmptyStream(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(""))
	_, err := Decode(r)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want io.EOF, got %v", err)
	}
}

func TestDecodeLastLineWithoutNewline(t *testing.T) {
	// Some writers omit the trailing newline. We must still parse the line.
	r := bufio.NewReader(strings.NewReader(`{"version":1,"op":"ping","id":"tail"}`))
	got, err := Decode(r)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "tail" {
		t.Fatalf("id: got %q", got.ID)
	}
}

func TestDecodeReportsUnmarshalError(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("not-json\n"))
	_, err := Decode(r)
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("want unmarshal error, got %v", err)
	}
}

func TestDecodeReportsReadError(t *testing.T) {
	r := bufio.NewReader(&errReader{err: errors.New("boom")})
	_, err := Decode(r)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want read error, got %v", err)
	}
}

func TestEncodeReportsWriteError(t *testing.T) {
	err := Encode(&errWriter{err: errors.New("disk")}, NewEnvelope(OpPing, ""))
	if err == nil || !strings.Contains(err.Error(), "disk") {
		t.Fatalf("want write error, got %v", err)
	}
}

func TestTrimNewlineHandlesCRLF(t *testing.T) {
	got := trimNewline([]byte("hello\r\n"))
	if string(got) != "hello" {
		t.Fatalf("trim CRLF failed: %q", string(got))
	}
	if string(trimNewline([]byte{})) != "" {
		t.Fatalf("trim empty failed")
	}
	if string(trimNewline([]byte("no-newline"))) != "no-newline" {
		t.Fatalf("preserve content failed")
	}
}

type errReader struct{ err error }

func (e *errReader) Read(p []byte) (int, error) { return 0, e.err }

type errWriter struct{ err error }

func (e *errWriter) Write(p []byte) (int, error) { return 0, e.err }

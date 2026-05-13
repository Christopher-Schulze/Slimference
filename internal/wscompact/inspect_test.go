package wscompact

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func makeFrame(opcode byte, fin bool, masked bool, payload []byte) []byte {
	first := opcode
	if fin {
		first |= 0x80
	}
	var out []byte
	out = append(out, first)
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	switch {
	case len(payload) < 126:
		out = append(out, maskBit|byte(len(payload)))
	case len(payload) <= 0xffff:
		out = append(out, maskBit|126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(len(payload)))
		out = append(out, ext[:]...)
	default:
		out = append(out, maskBit|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(len(payload)))
		out = append(out, ext[:]...)
	}
	if masked {
		key := []byte{1, 2, 3, 4}
		out = append(out, key...)
		maskedPayload := append([]byte(nil), payload...)
		for i := range maskedPayload {
			maskedPayload[i] ^= key[i%4]
		}
		payload = maskedPayload
	}
	return append(out, payload...)
}

func TestReadFrame_UnmaskedText(t *testing.T) {
	t.Parallel()
	raw := makeFrame(1, true, false, []byte(`{"type":"hello"}`))
	frame, err := ReadFrame(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame.Raw, raw) || string(frame.Payload) != `{"type":"hello"}` || !frame.Fin || frame.Masked {
		t.Fatalf("frame=%+v payload=%q", frame, frame.Payload)
	}
}

func TestReadFrame_MaskedExtendedPayload(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("x"), 130)
	raw := makeFrame(1, true, true, payload)
	frame, err := ReadFrame(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !frame.Masked || !bytes.Equal(frame.Payload, payload) || !bytes.Equal(frame.Raw, raw) {
		t.Fatalf("masked frame mismatch")
	}
}

func TestReadFrame_Extended64Payload(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("y"), 66000)
	raw := makeFrame(2, true, false, payload)
	frame, err := ReadFrame(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if frame.Opcode != 2 || !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("64-bit extended frame mismatch")
	}
}

func TestReadFrame_HugeLengthRejected(t *testing.T) {
	t.Parallel()
	raw := []byte{0x82, 127}
	var ext [8]byte
	binary.BigEndian.PutUint64(ext[:], uint64(int(^uint(0)>>1))+1)
	raw = append(raw, ext[:]...)
	_, err := ReadFrame(bytes.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too-large error, got %v", err)
	}
}

func TestReadFrame_TruncatedHeaderAndPayload(t *testing.T) {
	t.Parallel()
	if _, err := ReadFrame(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("header err=%v", err)
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0x81, 126, 0x00})); err == nil {
		t.Fatal("expected extended length error")
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0x81, 127, 0x00})); err == nil {
		t.Fatal("expected 64-bit extended length error")
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0x81, 0x82, 1, 2, 3})); err == nil {
		t.Fatal("expected mask key error")
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0x81, 0x02, 'x'})); err == nil {
		t.Fatal("expected payload error")
	}
}

func TestInspectStream_ByteForByteAndJSONShape(t *testing.T) {
	t.Parallel()
	raw := makeFrame(1, true, false, []byte(`{"method":"responses.create","z":1,"a":2}`))
	var dst bytes.Buffer
	var summaries []FrameSummary
	written, err := InspectStream(&dst, bytes.NewReader(raw), DirectionClientToServer, InspectorFunc(func(s FrameSummary) {
		summaries = append(summaries, s)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(len(raw)) || !bytes.Equal(dst.Bytes(), raw) {
		t.Fatalf("not byte equal")
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries=%d", len(summaries))
	}
	got := summaries[0]
	if !got.JSON || got.JSONTopLevel != "object" || got.MessageType != "responses.create" || strings.Join(got.JSONKeys, ",") != "a,method,z" {
		t.Fatalf("summary=%+v", got)
	}
}

func TestInspectStream_FragmentedText(t *testing.T) {
	t.Parallel()
	raw := append(makeFrame(1, false, false, []byte(`{"type":"`)), makeFrame(0, true, false, []byte(`done"}`))...)
	var dst bytes.Buffer
	var summaries []FrameSummary
	_, err := InspectStream(&dst, bytes.NewReader(raw), DirectionServerToClient, InspectorFunc(func(s FrameSummary) {
		summaries = append(summaries, s)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.Bytes(), raw) || len(summaries) != 2 {
		t.Fatalf("dst/summaries mismatch")
	}
	if summaries[0].JSON || !summaries[0].Fragmented || !summaries[1].JSON || summaries[1].MessageType != "done" {
		t.Fatalf("fragment summaries=%+v", summaries)
	}
}

func TestInspectStream_ThreePartFragmentedText(t *testing.T) {
	t.Parallel()
	raw := append(makeFrame(1, false, false, []byte(`{"type"`)), makeFrame(0, false, false, []byte(`:"multi"`))...)
	raw = append(raw, makeFrame(0, true, false, []byte(`}`))...)
	var summaries []FrameSummary
	_, err := InspectStream(io.Discard, bytes.NewReader(raw), DirectionServerToClient, InspectorFunc(func(s FrameSummary) {
		summaries = append(summaries, s)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 3 || summaries[1].JSON || !summaries[2].JSON || summaries[2].MessageType != "multi" {
		t.Fatalf("three-part summaries=%+v", summaries)
	}
}

func TestInspectStream_ControlBinaryNonJSONAndRSV(t *testing.T) {
	t.Parallel()
	frames := [][]byte{
		makeFrame(2, true, false, []byte{1, 2}),
		makeFrame(9, true, false, []byte("p")),
		makeFrame(10, true, false, []byte("p")),
		makeFrame(8, true, false, nil),
		makeFrame(1, true, false, []byte("not-json")),
		{0xc1, 0x02, '{', '}'},
		makeFrame(3, true, false, nil),
		makeFrame(1, true, false, []byte(`[1,2]`)),
		makeFrame(1, true, false, []byte(`true`)),
	}
	raw := bytes.Join(frames, nil)
	var summaries []FrameSummary
	_, err := InspectStream(io.Discard, bytes.NewReader(raw), DirectionClientToServer, InspectorFunc(func(s FrameSummary) {
		summaries = append(summaries, s)
	}))
	if err != nil {
		t.Fatal(err)
	}
	wantOps := []string{"binary", "ping", "pong", "close", "text", "text", "unknown_3", "text", "text"}
	if len(summaries) != len(wantOps) {
		t.Fatalf("summary count=%d", len(summaries))
	}
	for i, want := range wantOps {
		if summaries[i].Opcode != want {
			t.Fatalf("summary[%d]=%+v want opcode %s", i, summaries[i], want)
		}
	}
	if summaries[4].InspectNote != "non_json_text" || summaries[5].InspectNote != "reserved_bits_or_compressed_extension" || summaries[7].JSONTopLevel != "array" || summaries[8].JSONTopLevel != "scalar" {
		t.Fatalf("unexpected summaries=%+v", summaries)
	}
}

func TestSummarizeFrame_ObjectWithoutStringType(t *testing.T) {
	t.Parallel()
	frame := Frame{Payload: []byte(`{"event":7}`), Fin: true, Opcode: 1}
	summary := SummarizeFrame(frame, DirectionClientToServer, frame.Payload, false)
	if !summary.JSON || summary.JSONTopLevel != "object" || summary.MessageType != "" || strings.Join(summary.JSONKeys, ",") != "event" {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestInspectStream_NilInspectorAndErrors(t *testing.T) {
	t.Parallel()
	raw := makeFrame(1, true, false, []byte("{}"))
	var dst bytes.Buffer
	if _, err := InspectStream(&dst, bytes.NewReader(raw), DirectionClientToServer, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.Bytes(), raw) {
		t.Fatal("nil inspector path changed bytes")
	}
	var short shortWriter
	if _, err := InspectStream(short, bytes.NewReader(raw), DirectionClientToServer, nil); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write err=%v", err)
	}
	if _, err := InspectStream(errWriter{}, bytes.NewReader(raw), DirectionClientToServer, nil); err == nil {
		t.Fatal("expected writer error")
	}
	if _, err := InspectStream(io.Discard, bytes.NewReader([]byte{0x81, 0x02, 'x'}), DirectionClientToServer, nil); err == nil {
		t.Fatal("expected read error")
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestInspectorFuncNilAndOpcodeNames(t *testing.T) {
	t.Parallel()
	InspectorFunc(nil).Observe(FrameSummary{})
	for op, want := range map[byte]string{0: "continuation", 1: "text", 2: "binary", 8: "close", 9: "ping", 10: "pong", 15: "unknown_15"} {
		if got := OpcodeName(op); got != want {
			t.Fatalf("OpcodeName(%d)=%s", op, got)
		}
	}
}

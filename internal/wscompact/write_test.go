package wscompact

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// roundTrip encodes via WriteFrame, decodes via ReadFrame, returns
// the decoded frame for assertions.
func roundTrip(t *testing.T, fin bool, op WSOpcode, maskKey []byte, payload []byte) Frame {
	t.Helper()
	var buf bytes.Buffer
	n, err := WriteFrame(&buf, fin, op, maskKey, payload)
	if err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if n != buf.Len() {
		t.Errorf("WriteFrame returned %d, buffer has %d", n, buf.Len())
	}
	frame, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	return frame
}

func TestWriteFrameTinyPayload(t *testing.T) {
	frame := roundTrip(t, true, OpcodeText, nil, []byte("hi"))
	if string(frame.Payload) != "hi" {
		t.Errorf("payload=%q", frame.Payload)
	}
	if frame.Opcode != byte(OpcodeText) || !frame.Fin {
		t.Errorf("got %+v", frame)
	}
	if frame.Masked {
		t.Errorf("unmasked frame should round-trip unmasked")
	}
}

func TestWriteFrameMaskedClientFrame(t *testing.T) {
	mask := []byte{0xde, 0xad, 0xbe, 0xef}
	frame := roundTrip(t, true, OpcodeText, mask, []byte("hello world"))
	if string(frame.Payload) != "hello world" {
		t.Errorf("masked decode wrong: %q", frame.Payload)
	}
	if !frame.Masked {
		t.Errorf("masked flag lost")
	}
}

func TestWriteFrameWithOptionsRSV1(t *testing.T) {
	var buf bytes.Buffer
	n, err := WriteFrameWithOptions(&buf, WriteFrameOptions{
		Fin:     true,
		Opcode:  OpcodeText,
		Payload: []byte("compressed"),
		RSV1:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != buf.Len() {
		t.Fatalf("n=%d len=%d", n, buf.Len())
	}
	frame, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !frame.RSV || !frame.RSV1 || frame.RSV2 || frame.RSV3 {
		t.Fatalf("RSV bits not preserved: %+v", frame)
	}
	if string(frame.Payload) != "compressed" {
		t.Fatalf("payload=%q", frame.Payload)
	}
}

func TestWriteFrameMediumPayload126Boundary(t *testing.T) {
	// Exactly 126 bytes forces the 2-byte extended length encoding.
	payload := make([]byte, 126)
	for i := range payload {
		payload[i] = byte('A' + (i % 26))
	}
	frame := roundTrip(t, true, OpcodeText, nil, payload)
	if !bytes.Equal(frame.Payload, payload) {
		t.Errorf("medium frame round-trip lost bytes")
	}
}

func TestWriteFrameLargePayload64KBoundary(t *testing.T) {
	// 70000 bytes forces 8-byte extended length encoding.
	payload := make([]byte, 70000)
	for i := range payload {
		payload[i] = byte(i)
	}
	frame := roundTrip(t, true, OpcodeBinary, nil, payload)
	if len(frame.Payload) != len(payload) {
		t.Errorf("large frame length lost: got %d want %d", len(frame.Payload), len(payload))
	}
}

func TestWriteFrameFragmented(t *testing.T) {
	// First fragment: fin=false, opcode=text.
	var buf bytes.Buffer
	if _, err := WriteFrame(&buf, false, OpcodeText, nil, []byte("part1")); err != nil {
		t.Fatal(err)
	}
	// Continuation: fin=true, opcode=0.
	if _, err := WriteFrame(&buf, true, OpcodeContinuation, nil, []byte("part2")); err != nil {
		t.Fatal(err)
	}
	first, _ := ReadFrame(&buf)
	second, _ := ReadFrame(&buf)
	if first.Fin || first.Opcode != byte(OpcodeText) {
		t.Errorf("first frame wrong: %+v", first)
	}
	if !second.Fin || second.Opcode != byte(OpcodeContinuation) {
		t.Errorf("continuation wrong: %+v", second)
	}
}

func TestWriteFrameZeroLengthPayload(t *testing.T) {
	frame := roundTrip(t, true, OpcodePing, nil, nil)
	if len(frame.Payload) != 0 {
		t.Errorf("zero payload not preserved")
	}
	if frame.Opcode != byte(OpcodePing) {
		t.Errorf("opcode=%d", frame.Opcode)
	}
}

func TestWriteFrameMaskKeyWrongLength(t *testing.T) {
	_, err := WriteFrame(io.Discard, true, OpcodeText, []byte{1, 2}, []byte("x"))
	if err == nil {
		t.Errorf("expected error for short mask key")
	}
}

func TestWriteFrameAllOpcodes(t *testing.T) {
	for _, op := range []WSOpcode{
		OpcodeContinuation, OpcodeText, OpcodeBinary,
		OpcodeClose, OpcodePing, OpcodePong,
	} {
		frame := roundTrip(t, true, op, nil, []byte("x"))
		if frame.Opcode != byte(op) {
			t.Errorf("opcode round-trip lost: want %d got %d", op, frame.Opcode)
		}
	}
}

func TestWriteFrameWriterErrorOnHeader(t *testing.T) {
	w := &failingWriter{failAfter: 0}
	_, err := WriteFrame(w, true, OpcodeText, nil, []byte("x"))
	if err == nil {
		t.Errorf("expected error from header write")
	}
}

func TestWriteFrameWriterErrorOnPayload(t *testing.T) {
	w := &failingWriter{failAfter: 2} // header writes, payload fails
	_, err := WriteFrame(w, true, OpcodeText, nil, []byte("x"))
	if err == nil {
		t.Errorf("expected error from payload write")
	}
}

func TestWriteFrameDoesNotMutateCallerPayload(t *testing.T) {
	mask := []byte{1, 2, 3, 4}
	payload := []byte("test")
	original := append([]byte{}, payload...)
	if _, err := WriteFrame(io.Discard, true, OpcodeText, mask, payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, original) {
		t.Errorf("WriteFrame mutated caller's payload buffer")
	}
}

// failingWriter writes up to failAfter bytes then returns io.ErrShortWrite.
type failingWriter struct {
	written   int
	failAfter int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	remaining := w.failAfter - w.written
	if remaining <= 0 {
		return 0, errors.New("injected write failure")
	}
	if len(p) > remaining {
		w.written += remaining
		return remaining, errors.New("injected write failure")
	}
	w.written += len(p)
	return len(p), nil
}

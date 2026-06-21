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
	return makeFrameWithFirst(opcode, fin, masked, payload)
}

func makeFrameWithFirst(opcode byte, fin bool, masked bool, payload []byte) []byte {
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
	raw := makeFrame(1, true, false, []byte("{\n  \"method\": \"responses.create\",\n  \"z\": 1,\n  \"a\": 2\n}"))
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
	if !got.JSON || got.JSONTopLevel != "object" || got.MessageType != "responses.create" || strings.Join(got.JSONKeys, ",") != "a,method,z" || strings.Join(got.JSONTypes, ",") != "a:number,method:string,z:number" || got.ShapeHash == "" {
		t.Fatalf("summary=%+v", got)
	}
	if got.Shadow == nil || !got.Shadow.Eligible || got.Shadow.SavedBytes <= 0 || strings.Join(got.Shadow.AppliedLayers, ",") != "json_compact" {
		t.Fatalf("shadow=%+v", got.Shadow)
	}
}

func TestRouteInspector_AttachesRoute(t *testing.T) {
	t.Parallel()
	var got FrameSummary
	before := ContentFreeShapeHash(FrameSummary{JSONTopLevel: "object"})
	inspector := RouteInspector("/backend-api/codex/responses?x=1", InspectorFunc(func(summary FrameSummary) {
		got = summary
	}))
	inspector.Observe(FrameSummary{JSON: true, JSONTopLevel: "object"})
	if got.Route != "/backend-api/codex/responses?x=1" {
		t.Fatalf("route=%q", got.Route)
	}
	if got.ShapeHash == "" || got.ShapeHash == before {
		t.Fatalf("route-bound shape hash=%q before=%q", got.ShapeHash, before)
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
	if summaries[4].Shadow == nil || summaries[4].Shadow.Blocker != "non_json_text" {
		t.Fatalf("non-json shadow=%+v", summaries[4].Shadow)
	}
	if summaries[5].Shadow == nil || summaries[5].Shadow.Blocker != "reserved_bits_or_compressed_extension" {
		t.Fatalf("rsv shadow=%+v", summaries[5].Shadow)
	}
	if summaries[8].Shadow == nil || summaries[8].Shadow.Blocker != "no_savings" {
		t.Fatalf("scalar shadow=%+v", summaries[8].Shadow)
	}
}

func TestReadFrame_RSVBitsSplit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		first      byte
		wantRSV1   bool
		wantRSV2   bool
		wantRSV3   bool
		wantAnyRSV bool
	}{
		{name: "rsv1", first: 0xc1, wantRSV1: true, wantAnyRSV: true},
		{name: "rsv2", first: 0xa1, wantRSV2: true, wantAnyRSV: true},
		{name: "rsv3", first: 0x91, wantRSV3: true, wantAnyRSV: true},
		{name: "all", first: 0xf1, wantRSV1: true, wantRSV2: true, wantRSV3: true, wantAnyRSV: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte{tc.first, 0x02, 'x', 'y'}
			frame, err := ReadFrame(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if frame.RSV1 != tc.wantRSV1 || frame.RSV2 != tc.wantRSV2 ||
				frame.RSV3 != tc.wantRSV3 || frame.RSV != tc.wantAnyRSV {
				t.Fatalf("RSV split mismatch: %+v", frame)
			}
		})
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

func TestSummarizeFrame_ShadowEstimateTokensBranches(t *testing.T) {
	t.Parallel()
	if got := shadowEstimateTokens(0); got != 0 {
		t.Fatalf("zero tokens=%d", got)
	}
	if got := shadowEstimateTokens(1); got != 1 {
		t.Fatalf("one-byte tokens=%d", got)
	}
	frame := Frame{Payload: []byte("{\n  \"a\": 1,\n  \"b\": 2\n}"), Fin: true, Opcode: 1}
	summary := SummarizeFrame(frame, DirectionClientToServer, frame.Payload, false)
	if summary.Shadow == nil || !summary.Shadow.Eligible || summary.Shadow.SavedTokens <= 0 {
		t.Fatalf("shadow summary=%+v", summary.Shadow)
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

func TestApplyShadowEstimateEdges(t *testing.T) {
	t.Parallel()

	nonJSON := FrameSummary{JSON: false}
	applyShadowEstimate(&nonJSON, []byte("plain"))
	if nonJSON.Shadow != nil {
		t.Fatalf("non-json without inspect note should not get shadow: %+v", nonJSON.Shadow)
	}

	badJSON := FrameSummary{JSON: true}
	applyShadowEstimate(&badJSON, []byte(`{not-json`))
	if badJSON.Shadow == nil || badJSON.Shadow.Blocker != "json_compact_failed" {
		t.Fatalf("bad-json blocker = %+v", badJSON.Shadow)
	}

	noSavings := FrameSummary{JSON: true}
	applyShadowEstimate(&noSavings, []byte(`{"x":1}`))
	if noSavings.Shadow == nil || noSavings.Shadow.Blocker != "no_savings" {
		t.Fatalf("no-savings blocker = %+v", noSavings.Shadow)
	}
}

func TestJsonShapeType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"nil", nil, "null"},
		{"bool_true", true, "bool"},
		{"bool_false", false, "bool"},
		{"string", "hello", "string"},
		{"empty_string", "", "string"},
		{"float64", 42.5, "number"},
		{"float64_zero", 0.0, "number"},
		{"array", []any{1, 2, 3}, "array"},
		{"empty_array", []any{}, "array"},
		{"object", map[string]any{"a": 1}, "object"},
		{"empty_object", map[string]any{}, "object"},
		{"int_unknown", 42, "unknown"},
		{"int32_unknown", int32(42), "unknown"},
		{"struct_unknown", struct{ X int }{X: 1}, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := jsonShapeType(tc.value); got != tc.want {
				t.Fatalf("jsonShapeType(%T %v) = %q, want %q", tc.value, tc.value, got, tc.want)
			}
		})
	}
}

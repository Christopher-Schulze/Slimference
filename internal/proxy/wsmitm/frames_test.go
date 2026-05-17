package wsmitm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvelopeMarshalPreservesUnknownFieldsAfterMutation(t *testing.T) {
	env, err := Parse([]byte(`{"type":"request","body":{"input":"x"},"future_field":{"nested":true},"delta":""}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	env.ItemID = "rewritten"
	out, err := env.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, `"future_field":{"nested":true}`) {
		t.Fatalf("unknown field lost: %s", text)
	}
	if !strings.Contains(text, `"item_id":"rewritten"`) {
		t.Fatalf("mutated typed field missing: %s", text)
	}
	if !strings.Contains(text, `"delta":""`) {
		t.Fatalf("explicit empty delta must be preserved for streamcut blanking: %s", text)
	}
}

func TestEnvelopeMarshalUnknownKindKeepsOriginalType(t *testing.T) {
	env, err := Parse([]byte(`{"type":"future.event","payload":1}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if env.Kind != FrameKindUnknown {
		t.Fatalf("Kind=%q, want unknown", env.Kind)
	}
	out, err := env.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("remarshal JSON: %v", err)
	}
	if string(raw["type"]) != `"future.event"` {
		t.Fatalf("unknown type not preserved: %s", out)
	}
}

func TestFrameKindClassifiers(t *testing.T) {
	for _, k := range []FrameKind{
		FrameKindResponseOutputTextDelta,
		FrameKindResponseReasoningTextDelta,
		FrameKindResponseReasoningSummaryTextDelta,
		FrameKindResponseFunctionCallArgsDelta,
		FrameKindResponseCustomToolCallInputDelta,
	} {
		if !k.IsTextDelta() {
			t.Fatalf("%q should be text-delta", k)
		}
	}
	for _, k := range []FrameKind{FrameKindResponseCompleted, FrameKindResponseIncomplete, FrameKindResponseFailed} {
		if !k.IsTerminal() {
			t.Fatalf("%q should be terminal", k)
		}
	}
	for _, k := range []FrameKind{FrameKindPing, FrameKindPong} {
		if !k.IsControl() {
			t.Fatalf("%q should be control", k)
		}
	}
	if FrameKindUnknown.IsKnown() || FrameKind("future").IsKnown() {
		t.Fatal("unknown/future kinds must not report known")
	}
	if FrameKindRequest.IsTextDelta() || FrameKindRequest.IsTerminal() || FrameKindRequest.IsControl() {
		t.Fatal("request should not be classified as text/terminal/control")
	}
}

func TestParseRejectsEmptyMalformedAndNonObject(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
		want error
	}{
		{name: "empty", raw: nil, want: ErrEmpty},
		{name: "whitespace", raw: []byte(" \n\t "), want: ErrMalformed},
		{name: "array", raw: []byte(`[]`), want: ErrMalformed},
		{name: "bad json", raw: []byte(`{"type":`), want: ErrMalformed},
		{name: "typed field mismatch", raw: []byte(`{"type":"request","sequence_number":"bad"}`), want: ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.raw)
			if err == nil || !strings.Contains(err.Error(), tc.want.Error()) {
				t.Fatalf("err=%v want containing %v", err, tc.want)
			}
		})
	}
}

func TestEnvelopeSequenceStringAndMarshalOptionalFields(t *testing.T) {
	seq := int64(42)
	summary := int64(3)
	content := int64(7)
	env := Envelope{
		Kind:           FrameKindResponseOutputTextDelta,
		SequenceNumber: &seq,
		SummaryIndex:   &summary,
		ContentIndex:   &content,
		ItemID:         "item-1",
		CallID:         "call-1",
		Delta:          "x",
		Headers:        json.RawMessage(`{"h":true}`),
		Metadata:       json.RawMessage(`{"m":true}`),
		Request:        json.RawMessage(`{"req":true}`),
		Response:       json.RawMessage(`{"resp":true}`),
		Item:           json.RawMessage(`{"item":true}`),
		Body:           json.RawMessage(`{"body":true}`),
	}
	if got := env.Sequence(); got != seq {
		t.Fatalf("Sequence=%d want %d", got, seq)
	}
	text := env.String()
	for _, want := range []string{"response.output_text.delta", "seq=42", "item=item-1", "call=call-1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("String missing %q: %q", want, text)
		}
	}
	out, err := env.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		`"sequence_number":42`,
		`"summary_index":3`,
		`"content_index":7`,
		`"headers":{"h":true}`,
		`"metadata":{"m":true}`,
		`"request":{"req":true}`,
		`"response":{"resp":true}`,
		`"item":{"item":true}`,
		`"body":{"body":true}`,
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("Marshal missing %q: %s", want, out)
		}
	}

	env.SequenceNumber = nil
	env.SummaryIndex = nil
	env.ContentIndex = nil
	env.Headers = nil
	env.Metadata = nil
	env.Request = nil
	env.Response = nil
	env.Item = nil
	env.Body = nil
	out, err = env.Marshal()
	if err != nil {
		t.Fatalf("Marshal without optional fields: %v", err)
	}
	var cleared map[string]json.RawMessage
	if err := json.Unmarshal(out, &cleared); err != nil {
		t.Fatalf("cleared marshal JSON: %v", err)
	}
	for _, absent := range []string{
		"sequence_number", "summary_index", "content_index",
		"headers", "metadata", "request", "response", "item", "body",
	} {
		if _, ok := cleared[absent]; ok {
			t.Fatalf("optional field %q should be absent after clearing: %s", absent, out)
		}
	}
	if got := env.Sequence(); got != 0 {
		t.Fatalf("nil Sequence=%d want 0", got)
	}
}

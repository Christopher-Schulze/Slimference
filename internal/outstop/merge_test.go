package outstop

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func unmarshalStop(t *testing.T, body []byte, field string) []string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal merged body: %v", err)
	}
	r, ok := raw[field]
	if !ok {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(r, &arr); err != nil {
		t.Fatalf("decode %s: %v body=%s", field, err, string(r))
	}
	return arr
}

func TestMergeIntoBodyAnthropicEmpty(t *testing.T) {
	body := []byte(`{"model":"claude","messages":[]}`)
	out, res := MergeIntoBody(types.Anthropic, body)
	if !res.OK {
		t.Fatalf("res.OK=false want true")
	}
	if res.AddedCount != 4 {
		t.Fatalf("AddedCount=%d want 4", res.AddedCount)
	}
	if res.FieldUsed != "stop_sequences" {
		t.Fatalf("FieldUsed=%q", res.FieldUsed)
	}
	got := unmarshalStop(t, out, "stop_sequences")
	want := PhrasesTopN(4)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stop_sequences=%v want %v", got, want)
	}
}

func TestMergeIntoBodyOpenAIEmpty(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[]}`)
	out, res := MergeIntoBody(types.OpenAI, body)
	if !res.OK || res.AddedCount != 4 || res.FieldUsed != "stop" {
		t.Fatalf("res=%+v", res)
	}
	got := unmarshalStop(t, out, "stop")
	if len(got) != 4 {
		t.Errorf("len(stop)=%d want 4", len(got))
	}
}

func TestMergeIntoBodyCodex(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","messages":[]}`)
	out, res := MergeIntoBody(types.CodexChatGPT, body)
	if res.FieldUsed != "stop" {
		t.Fatalf("FieldUsed=%q want stop", res.FieldUsed)
	}
	if res.AddedCount != 4 {
		t.Fatalf("AddedCount=%d want 4", res.AddedCount)
	}
	if got := unmarshalStop(t, out, "stop"); len(got) != 4 {
		t.Fatalf("len(stop)=%d want 4", len(got))
	}
}

func TestMergeIntoBodyOpenAIResponsesShapeSkipped(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":"hi"}`)
	out, res := MergeIntoBody(types.OpenAI, body)
	if !res.OK {
		t.Fatal("Responses-shape body should be a safe no-op")
	}
	if res.AddedCount != 0 {
		t.Fatalf("AddedCount=%d want 0", res.AddedCount)
	}
	if !reflect.DeepEqual(out, body) {
		t.Fatalf("Responses-shape body mutated: got %s want %s", out, body)
	}
}

func TestMergeIntoBodyCodexResponsesShapeSkipped(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","input":[{"type":"message","role":"user","content":"hi"}],"stream":true}`)
	out, res := MergeIntoBody(types.CodexChatGPT, body)
	if !res.OK {
		t.Fatal("Codex Responses-shape body should be a safe no-op")
	}
	if res.AddedCount != 0 {
		t.Fatalf("AddedCount=%d want 0", res.AddedCount)
	}
	if !reflect.DeepEqual(out, body) {
		t.Fatalf("Codex Responses-shape body mutated: got %s want %s", out, body)
	}
}

func TestMergeIntoBodyNoMessagesShapeSkipped(t *testing.T) {
	body := []byte(`{"model":"gpt-5","stop":"END"}`)
	out, res := MergeIntoBody(types.OpenAI, body)
	if !res.OK {
		t.Fatal("no-messages body should be a safe no-op")
	}
	if res.AddedCount != 0 {
		t.Fatalf("AddedCount=%d want 0", res.AddedCount)
	}
	if !reflect.DeepEqual(out, body) {
		t.Fatalf("no-messages body mutated: got %s want %s", out, body)
	}
}

func TestMergeIntoBodyPreservesUserStringForm(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[],"stop":"END"}`)
	out, res := MergeIntoBody(types.OpenAI, body)
	if !res.OK {
		t.Fatalf("res.OK=false")
	}
	got := unmarshalStop(t, out, "stop")
	if len(got) == 0 || got[0] != "END" {
		t.Fatalf("user 'END' not preserved as first entry, got=%v", got)
	}
	if len(got) != 4 {
		t.Errorf("expected cap=4, got %d", len(got))
	}
	if res.AddedCount != 3 {
		t.Errorf("AddedCount=%d want 3 (4 cap - 1 user)", res.AddedCount)
	}
}

func TestMergeIntoBodyPreservesUserArrayForm(t *testing.T) {
	body := []byte(`{"messages":[],"stop":["FOO","BAR"]}`)
	out, res := MergeIntoBody(types.OpenAI, body)
	if !res.OK {
		t.Fatalf("res.OK=false")
	}
	got := unmarshalStop(t, out, "stop")
	if got[0] != "FOO" || got[1] != "BAR" {
		t.Errorf("user entries not preserved first: %v", got)
	}
	if len(got) != 4 {
		t.Errorf("cap=4 violated, got %d", len(got))
	}
	if res.AddedCount != 2 {
		t.Errorf("AddedCount=%d want 2", res.AddedCount)
	}
}

func TestMergeIntoBodyUserFullCapNoAdds(t *testing.T) {
	body := []byte(`{"messages":[],"stop":["A","B","C","D"]}`)
	out, res := MergeIntoBody(types.OpenAI, body)
	if !res.OK {
		t.Fatalf("res.OK=false")
	}
	if res.AddedCount != 0 {
		t.Errorf("AddedCount=%d want 0 when user already at cap", res.AddedCount)
	}
	got := unmarshalStop(t, out, "stop")
	if !reflect.DeepEqual(got, []string{"A", "B", "C", "D"}) {
		t.Errorf("user list mutated: %v", got)
	}
}

func TestMergeIntoBodyUserShadowsOurPhrase(t *testing.T) {
	// User already lists one of our curated phrases. We must skip it
	// in the addition loop (dup branch) and add only the remaining
	// distinct phrases up to cap.
	ours := PhrasesTopN(4)
	body := []byte(`{"messages":[],"stop":` + mustJSON(t, []string{ours[1]}) + `}`)
	out, res := MergeIntoBody(types.OpenAI, body)
	got := unmarshalStop(t, out, "stop")
	if len(got) != 4 {
		t.Fatalf("len=%d want 4 got=%v", len(got), got)
	}
	if got[0] != ours[1] {
		t.Fatalf("user entry not preserved as first: %v", got)
	}
	if res.AddedCount != 3 {
		t.Errorf("AddedCount=%d want 3", res.AddedCount)
	}
	seen := map[string]int{}
	for _, s := range got {
		seen[s]++
	}
	if seen[ours[1]] != 1 {
		t.Errorf("duplicate of shadowed phrase: %v", got)
	}
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestMergeIntoBodyIdempotent(t *testing.T) {
	body := []byte(`{"model":"claude","messages":[]}`)
	first, _ := MergeIntoBody(types.Anthropic, body)
	second, res2 := MergeIntoBody(types.Anthropic, first)
	if res2.AddedCount != 0 {
		t.Errorf("second pass AddedCount=%d want 0 (idempotent)", res2.AddedCount)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second pass mutated body")
	}
}

func TestMergeIntoBodyDedupExistingUserEntries(t *testing.T) {
	body := []byte(`{"messages":[],"stop":["FOO","FOO","BAR"]}`)
	out, _ := MergeIntoBody(types.OpenAI, body)
	got := unmarshalStop(t, out, "stop")
	seen := map[string]int{}
	for _, s := range got {
		seen[s]++
	}
	for s, n := range seen {
		if n > 1 {
			t.Errorf("duplicate entry %q count=%d", s, n)
		}
	}
}

func TestMergeIntoBodyUserEmptyString(t *testing.T) {
	body := []byte(`{"messages":[],"stop":""}`)
	out, res := MergeIntoBody(types.OpenAI, body)
	if !res.OK {
		t.Fatalf("res.OK=false")
	}
	got := unmarshalStop(t, out, "stop")
	if len(got) != 4 {
		t.Errorf("len(stop)=%d want 4, empty user string should not occupy a slot", len(got))
	}
}

func TestMergeIntoBodyMalformedJSONPassthrough(t *testing.T) {
	body := []byte(`{not json`)
	out, res := MergeIntoBody(types.Anthropic, body)
	if res.OK {
		t.Errorf("res.OK=true on malformed JSON; want false")
	}
	if !reflect.DeepEqual(out, body) {
		t.Errorf("body mutated on malformed JSON")
	}
}

func TestMergeIntoBodyMalformedStopField(t *testing.T) {
	// stop is neither string nor array - decode fails for the field
	// and we treat it as "user supplied something unrecognised" -> no
	// new entries injected to avoid stomping the operator's intent.
	body := []byte(`{"messages":[],"stop":12345}`)
	out, res := MergeIntoBody(types.OpenAI, body)
	if !res.OK {
		t.Errorf("res.OK=false on unknown stop shape")
	}
	got := unmarshalStop(t, out, "stop")
	if len(got) == 0 {
		t.Errorf("expected our default phrases when user shape is unparseable, got empty")
	}
}

func TestMergeIntoBodyEmpty(t *testing.T) {
	out, res := MergeIntoBody(types.Anthropic, nil)
	if res.OK {
		t.Errorf("OK=true on nil body; want false")
	}
	if out != nil {
		t.Errorf("body mutated from nil")
	}
}

func TestMergeIntoBodyUnknownProviderPassthrough(t *testing.T) {
	body := []byte(`{"foo":1}`)
	out, res := MergeIntoBody(types.Provider(99), body)
	if !res.OK {
		t.Errorf("unknown provider should return OK=true (no-op)")
	}
	if res.AddedCount != 0 {
		t.Errorf("AddedCount=%d want 0", res.AddedCount)
	}
	if !reflect.DeepEqual(out, body) {
		t.Errorf("body mutated for unknown provider")
	}
}

func TestDecodeExistingStopVariants(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantArr []string
		wantHad bool
	}{
		{"absent", "", nil, false},
		{"whitespace", "   ", nil, false},
		{"empty string", `""`, nil, true},
		{"single", `"X"`, []string{"X"}, true},
		{"array", `["A","B"]`, []string{"A", "B"}, true},
		{"unparseable", `12345`, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			arr, had := decodeExistingStop(json.RawMessage(c.raw))
			if had != c.wantHad {
				t.Errorf("had=%v want %v", had, c.wantHad)
			}
			if !reflect.DeepEqual(arr, c.wantArr) {
				t.Errorf("arr=%v want %v", arr, c.wantArr)
			}
		})
	}
}

func TestEncodeStopFieldEmpty(t *testing.T) {
	out := encodeStopField(nil)
	if string(out) != "[]" {
		t.Errorf("got %s want []", out)
	}
}

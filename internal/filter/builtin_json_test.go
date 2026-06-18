package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactJSONMinify(t *testing.T) {
	t.Parallel()
	pretty := []byte("{\n  \"a\": 1,\n  \"b\": \"x\"\n}\n")
	out, ok := TryCompactJSONMinify(pretty)
	if !ok {
		t.Fatal("expected minify")
	}
	if string(out) != `{"a":1,"b":"x"}` {
		t.Fatalf("got %q", out)
	}
	oneLine := []byte(`{"x":true}`)
	if _, ok := TryCompactJSONMinify(oneLine); ok {
		t.Fatal("already minimal, skip")
	}
	if _, ok := TryCompactJSONMinify([]byte("not json")); ok {
		t.Fatal("invalid json")
	}
}

func TestTryCompactJQJSONExact(t *testing.T) {
	t.Parallel()

	pretty := []byte("{\n  \"a\": 1,\n  \"b\": \"x\"\n}\n")
	out, ok := TryCompactJQJSONExact([]string{"jq", ".", "package.json"}, pretty)
	if !ok {
		t.Fatal("jq JSON should be handled to block later lossy reducers")
	}
	if string(out) != `{"a":1,"b":"x"}` {
		t.Fatalf("unexpected compact jq JSON: %q", out)
	}

	oneLine := []byte(`{"x":true}`)
	out, ok = TryCompactJQJSONExact([]string{"jq", "-c", "."}, oneLine)
	if !ok {
		t.Fatal("already compact jq JSON should still be handled")
	}
	if string(out) != string(oneLine) {
		t.Fatalf("already compact jq JSON must full-pass, got %q", out)
	}

	text := []byte("plain\nplain\n")
	out, ok = TryCompactJQJSONExact([]string{"jq", "-r", ".name"}, text)
	if !ok {
		t.Fatal("jq non-JSON output should be handled to block TOML truncation")
	}
	if string(out) != string(text) {
		t.Fatalf("jq non-JSON output must full-pass, got %q", out)
	}

	if _, ok := TryCompactJQJSONExact([]string{"cat", "package.json"}, pretty); ok {
		t.Fatal("non-jq command must not enter jq exact gate")
	}
}

func TestTryCompactJQJSONExact_LargeJSONNeverSchemaSummarized(t *testing.T) {
	t.Parallel()

	body := "{\n  \"items\": [\n    " +
		strings.Repeat("{\"id\":1,\"name\":\"same\",\"value\":\"abcdef\"},\n    ", 80) +
		"{\"id\":2,\"name\":\"last\",\"value\":\"uvwxyz\"}\n  ]\n}\n"
	out, ok := TryCompactJQJSONExact([]string{"jq", "."}, []byte(body))
	if !ok {
		t.Fatal("large jq JSON should be handled to block generic schema extraction")
	}
	got := string(out)
	if strings.Contains(got, "{object,") || strings.Contains(got, "[array,") {
		t.Fatalf("jq JSON must not be schema-summarized: %q", got[:min(len(got), 120)])
	}
	if !strings.Contains(got, `"last"`) || !strings.Contains(got, `"uvwxyz"`) {
		t.Fatalf("jq JSON lost scalar evidence: %q", got[:min(len(got), 120)])
	}
}

func TestTryCompactJSONMinify_emptyInput(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactJSONMinify([]byte{}); ok {
		t.Fatal("empty input should return false")
	}
}

func TestTryCompactJSONMinify_schemaExtraction(t *testing.T) {
	t.Parallel()
	// Build a large JSON object that exceeds jsonSchemaThreshold.
	// This simulates a typical API response with many fields.
	var sb strings.Builder
	sb.WriteString(`{"status":"ok","data":{"id":12345,"name":"Example Response","description":"`)
	// Pad description to exceed threshold.
	sb.WriteString(strings.Repeat("This is a long description field that takes up lots of space. ", 30))
	sb.WriteString(`","items":[{"id":1,"value":"a"},{"id":2,"value":"b"}],"metadata":{"created_at":"2025-01-01","updated_at":"2025-12-31","tags":["api","test","example"]},"count":42,"active":true}}`)
	input := []byte(sb.String())

	out, ok := TryCompactJSONMinify(input)
	if !ok {
		t.Fatal("expected schema extraction or minification for large JSON")
	}
	s := string(out)
	if len(s) >= len(input) {
		t.Errorf("output should be shorter: got %d vs input %d", len(s), len(input))
	}
	// Schema output should mention the top-level keys.
	if !strings.Contains(s, "status") && !strings.Contains(s, "data") {
		t.Errorf("schema should mention top-level keys, got: %q", s[:min(len(s), 300)])
	}
}

func TestTryCompactJSONMinify_schemaArray(t *testing.T) {
	t.Parallel()
	// Large JSON array.
	var sb strings.Builder
	sb.WriteString(`[`)
	for i := 0; i < 50; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"id":`)
		sb.WriteString(strings.Repeat("1", 5))
		sb.WriteString(`,"name":"item_`)
		sb.WriteString(strings.Repeat("x", 20))
		sb.WriteString(`","value":`)
		sb.WriteString(strings.Repeat("9", 10))
		sb.WriteByte('}')
	}
	sb.WriteByte(']')
	input := []byte(sb.String())

	if len(input) < jsonSchemaThreshold {
		t.Skip("input too small to trigger schema extraction")
	}
	out, ok := TryCompactJSONMinify(input)
	if !ok {
		t.Fatal("expected extraction for large JSON array")
	}
	s := string(out)
	if len(s) >= len(input) {
		t.Errorf("output should be shorter: got %d vs input %d", len(s), len(input))
	}
	if !strings.Contains(s, "array") {
		t.Errorf("schema should indicate array type, got: %q", s[:min(len(s), 200)])
	}
}

func TestTryCompactJSONMinify_diagnosticKeysPreferValuePreservation(t *testing.T) {
	t.Parallel()
	input := []byte("{\n  \"status\": \"failed\",\n  \"error\": \"E42 exact failure message\",\n  \"data\": \"" + strings.Repeat("x", jsonSchemaThreshold) + "\"\n}\n")
	out, ok := TryCompactJSONMinify(input)
	if !ok {
		t.Fatal("expected minification for diagnostic JSON")
	}
	s := string(out)
	if !strings.Contains(s, "E42 exact failure message") {
		t.Fatalf("diagnostic scalar should be preserved exactly, got %q", s[:min(len(s), 200)])
	}
	if strings.Contains(s, "{object,") {
		t.Fatalf("diagnostic JSON should not be schema-only: %q", s[:min(len(s), 200)])
	}
}

// TestJsonTypeName exercises all branches of jsonTypeName.
func TestJsonTypeName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v    interface{}
		want string
	}{
		{map[string]interface{}{"key": "val"}, "object"},
		{[]interface{}{1.0, 2.0}, "array"},
		{"hello", "string"},
		{3.14, "number"},
		{true, "bool"},
		{nil, "null"},
		// unknown type (struct is not a JSON primitive)
		{struct{}{}, "unknown"},
	}
	for _, c := range cases {
		got := jsonTypeName(c.v)
		if got != c.want {
			t.Errorf("jsonTypeName(%T) = %q, want %q", c.v, got, c.want)
		}
	}
}

// TestExtractJSONSchema_primitiveTypes covers schemaOf at depth=0 for scalar JSON types.
func TestExtractJSONSchema_primitiveTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input []byte
		want  string // substring expected in output
	}{
		{[]byte(`"hello world"`), "[string,"},
		{[]byte(`3.14`), "[number:"},
		{[]byte(`true`), "[bool:"},
		{[]byte(`null`), "[null]"},
	}
	for _, c := range cases {
		got, ok := extractJSONSchema(c.input)
		if !ok {
			t.Errorf("extractJSONSchema(%s): expected ok, got false", c.input)
			continue
		}
		if !strings.Contains(string(got), c.want) {
			t.Errorf("extractJSONSchema(%s): want %q in output, got %q", c.input, c.want, string(got))
		}
	}
}

// TestExtractJSONSchema_invalidJSON covers the unmarshal error path.
func TestExtractJSONSchema_invalidJSON(t *testing.T) {
	t.Parallel()
	_, ok := extractJSONSchema([]byte("{invalid json"))
	if ok {
		t.Error("invalid JSON: want false, got true")
	}
}

// TestSchemaOf_manyKeys covers the i>=limit branch (line 78-80) in schemaOf:
// an object with more than 40 keys triggers the "[+N more keys]" truncation.
func TestSchemaOf_manyKeys(t *testing.T) {
	t.Parallel()
	// Build a JSON object with 45 keys.
	var sb strings.Builder
	sb.WriteByte('{')
	for i := 0; i < 45; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf("%q:%d", strings.Repeat("k", i+1), i))
	}
	sb.WriteByte('}')
	got, ok := extractJSONSchema([]byte(sb.String()))
	if !ok {
		t.Fatal("45-key object: expected schema, got false")
	}
	s := string(got)
	if !strings.Contains(s, "[+5 more keys]") {
		t.Errorf("want '[+5 more keys]' truncation marker, got: %q", s[:min(len(s), 300)])
	}
}

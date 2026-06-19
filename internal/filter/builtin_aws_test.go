package filter

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestTryCompactAwsJSON(t *testing.T) {
	t.Parallel()
	in := []byte(`{
  "UserId": "x",
  "ResultMetadata": {"foo": 1},
  "ResponseMetadata": {
    "RequestId": "rid",
    "HTTPStatusCode": 200
  }
}`)
	out, ok := TryCompactAwsJSON([]string{"aws", "sts", "get-caller-identity"}, in)
	if !ok {
		t.Fatal("expected strip")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["ResponseMetadata"]; ok {
		t.Fatal("ResponseMetadata should be removed")
	}
	if _, ok := m["ResultMetadata"]; ok {
		t.Fatal("ResultMetadata should be removed")
	}
	if m["UserId"] != "x" {
		t.Fatalf("UserId: %v", m["UserId"])
	}
	if _, ok := TryCompactAwsJSON([]string{"curl", "x"}, in); ok {
		t.Fatal("not aws")
	}
}

func TestTryCompactAwsJSON_nestedSliceAndExe(t *testing.T) {
	t.Parallel()
	in := []byte(`[{"UserId":"u","ResponseMetadata":{"RequestId":"r"}},{"k":2}]`)
	out, ok := TryCompactAwsJSON([]string{"aws.exe", "s3", "ls"}, in)
	if !ok {
		t.Fatal("expected strip")
	}
	if len(out) >= len(in) {
		t.Fatal("output should shrink")
	}
	var v []interface{}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	m := v[0].(map[string]interface{})
	if _, ok := m["ResponseMetadata"]; ok {
		t.Fatal("nested metadata should be stripped")
	}
}

func TestTryCompactAwsJSON_noShrinkReturnsFalse(t *testing.T) {
	t.Parallel()
	// Valid JSON with no strip keys — marshal may not shorten vs pretty-printed input.
	in := []byte(`{"a":1}`)
	if _, ok := TryCompactAwsJSON([]string{"aws", "x"}, in); ok {
		t.Fatal("nothing to strip")
	}
}

func TestTryCompactAwsJSON_rejectsNonJSON(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactAwsJSON([]string{"aws", "x"}, []byte(`not json`)); ok {
		t.Fatal("invalid json")
	}
}

func TestTryCompactAwsJSON_emptyArgv(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactAwsJSON([]string{}, []byte(`{"a":1}`)); ok {
		t.Fatal("empty argv should return false")
	}
}

func TestTryCompactAwsJSONExactPreservesMetadata(t *testing.T) {
	t.Parallel()
	in := []byte(`{
  "UserId": "x",
  "ResponseMetadata": {
    "RequestId": "rid",
    "HTTPStatusCode": 200
  }
}`)
	out, ok := TryCompactAwsJSONExact([]string{"aws", "sts", "get-caller-identity"}, in)
	if !ok {
		t.Fatal("expected exact AWS JSON match")
	}
	if len(out) >= len(in) {
		t.Fatal("exact AWS JSON should shrink pretty JSON")
	}
	if !bytes.Contains(out, []byte(`"ResponseMetadata"`)) || !bytes.Contains(out, []byte(`"RequestId":"rid"`)) {
		t.Fatalf("exact AWS JSON lost metadata: %s", out)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["ResponseMetadata"]; !ok {
		t.Fatal("ResponseMetadata must be preserved by exact AWS JSON")
	}
}

func TestTryCompactAwsJSONExactBoundaries(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactAwsJSONExact([]string{"curl", "https://example.com"}, []byte(`{"a":1}`)); ok {
		t.Fatal("non-AWS argv should not match")
	}
	out, ok := TryCompactAwsJSONExact([]string{"aws", "s3", "ls"}, []byte("not json\n"))
	if !ok {
		t.Fatal("AWS non-JSON should be handled as a full-pass boundary")
	}
	if string(out) != "not json\n" {
		t.Fatalf("AWS non-JSON should full-pass unchanged, got %q", out)
	}
}

package readcache

import "testing"

func TestExtractRequest(t *testing.T) {
	t.Parallel()

	req, err := ExtractRequest([]byte(`{
		"session_id": "sess-1",
		"tool_input": {"file_path": "main.go", "offset": 10, "limit": 25}
	}`))
	if err != nil {
		t.Fatalf("ExtractRequest: %v", err)
	}
	if req.SessionID != "sess-1" || req.FilePath != "main.go" || req.Offset != 10 || req.Limit != 25 {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestExtractRequest_MissingPath(t *testing.T) {
	t.Parallel()

	_, err := ExtractRequest([]byte(`{"session_id":"s"}`))
	if err == nil {
		t.Fatal("expected missing file_path error")
	}
}

func TestExtractRequest_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := ExtractRequest([]byte(`{`))
	if err == nil {
		t.Fatal("expected JSON error")
	}
}

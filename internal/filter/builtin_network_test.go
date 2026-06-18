package filter

import (
	"strings"
	"testing"
)

func TestTryCompactNetworkResponse_ExactJSONMinify(t *testing.T) {
	t.Parallel()

	out, ok := TryCompactNetworkResponse(
		[]string{"curl", "https://api.example.com/data"},
		[]byte("{\n  \"status\": \"ok\",\n  \"count\": 42\n}\n"),
	)
	if !ok {
		t.Fatal("curl JSON should be handled to block later lossy reducers")
	}
	if got := string(out); got != `{"status":"ok","count":42}` {
		t.Fatalf("unexpected compact JSON: %q", got)
	}

	httpieOut, ok := TryCompactNetworkResponse(
		[]string{"http", "GET", "https://api.example.com/data"},
		[]byte("{\n  \"status\": \"ok\",\n  \"client\": \"httpie\"\n}\n"),
	)
	if !ok {
		t.Fatal("HTTPie JSON should be handled to block later lossy reducers")
	}
	if got := string(httpieOut); got != `{"status":"ok","client":"httpie"}` {
		t.Fatalf("unexpected HTTPie compact JSON: %q", got)
	}
}

func TestTryCompactNetworkResponse_LargeJSONNeverSchemaSummarized(t *testing.T) {
	t.Parallel()

	body := `{"items":[` + strings.Repeat(`{"id":1,"name":"same","value":"abcdef"},`, 80) + `{"id":2,"name":"last","value":"uvwxyz"}]}`
	out, ok := TryCompactNetworkResponse([]string{"wget", "-qO-", "https://api.example.com/data"}, []byte(body))
	if !ok {
		t.Fatal("wget JSON should be handled to block generic schema extraction")
	}
	got := string(out)
	if strings.Contains(got, "{object,") || strings.Contains(got, "[array,") {
		t.Fatalf("network JSON must not be schema-summarized: %q", got[:min(len(got), 120)])
	}
	if !strings.Contains(got, `"last"`) || !strings.Contains(got, `"uvwxyz"`) {
		t.Fatalf("network JSON lost scalar evidence: %q", got[:min(len(got), 120)])
	}
}

func TestTryCompactAPIJSONExact_ExactJSONMinify(t *testing.T) {
	t.Parallel()

	out, ok := TryCompactAPIJSONExact(
		[]string{"gh", "api", "/repos/acme/project"},
		[]byte("{\n  \"status\": \"ok\",\n  \"count\": 42\n}\n"),
	)
	if !ok {
		t.Fatal("gh api JSON should be handled to block later lossy reducers")
	}
	if got := string(out); got != `{"status":"ok","count":42}` {
		t.Fatalf("unexpected gh api compact JSON: %q", got)
	}

	glabOut, ok := TryCompactAPIJSONExact(
		[]string{"glab", "api", "projects/1"},
		[]byte("{\n  \"status\": \"ok\",\n  \"client\": \"glab\"\n}\n"),
	)
	if !ok {
		t.Fatal("glab api JSON should be handled to block later lossy reducers")
	}
	if got := string(glabOut); got != `{"status":"ok","client":"glab"}` {
		t.Fatalf("unexpected glab api compact JSON: %q", got)
	}
}

func TestTryCompactAPIJSONExact_LargeJSONNeverSchemaSummarized(t *testing.T) {
	t.Parallel()

	body := "{\n  \"items\": [\n    " +
		strings.Repeat("{\"id\":1,\"name\":\"same\",\"value\":\"abcdef\"},\n    ", 80) +
		"{\"id\":2,\"name\":\"last\",\"value\":\"uvwxyz\"}\n  ]\n}\n"
	out, ok := TryCompactAPIJSONExact([]string{"gh", "api", "/repos/acme/project/releases"}, []byte(body))
	if !ok {
		t.Fatal("large gh api JSON should be handled to block generic schema extraction")
	}
	got := string(out)
	if strings.Contains(got, "{object,") || strings.Contains(got, "[array,") {
		t.Fatalf("API JSON must not be schema-summarized: %q", got[:min(len(got), 120)])
	}
	if !strings.Contains(got, `"last"`) || !strings.Contains(got, `"uvwxyz"`) {
		t.Fatalf("API JSON lost scalar evidence: %q", got[:min(len(got), 120)])
	}
}

func TestTryCompactAPIJSONExact_NonJSONFullPass(t *testing.T) {
	t.Parallel()

	body := []byte("plain response\nplain response\n")
	out, ok := TryCompactAPIJSONExact([]string{"gh", "api", "/markdown", "--input", "-"}, body)
	if !ok {
		t.Fatal("gh api non-JSON should be handled to block later log reduction")
	}
	if string(out) != string(body) {
		t.Fatalf("gh api non-JSON output must full-pass, got %q", out)
	}
}

func TestTryCompactAPIJSONExact_UnrelatedCommand(t *testing.T) {
	t.Parallel()

	if _, ok := TryCompactAPIJSONExact([]string{"gh", "pr", "list"}, []byte(`{"ok":true}`)); ok {
		t.Fatal("non-api gh command must not match API exact gate")
	}
	if _, ok := TryCompactAPIJSONExact([]string{"curl", "https://api.example.com/data"}, []byte(`{"ok":true}`)); ok {
		t.Fatal("network client command must stay on network exact gate")
	}
}

func TestTryCompactNetworkResponse_NonJSONFullPass(t *testing.T) {
	t.Parallel()

	body := []byte("INFO boot\nINFO boot\nINFO boot\n")
	out, ok := TryCompactNetworkResponse([]string{"curl", "https://api.example.com/logs"}, body)
	if !ok {
		t.Fatal("curl non-JSON should be handled to block later log reduction")
	}
	if string(out) != string(body) {
		t.Fatalf("non-JSON network output must full-pass, got %q", out)
	}

	out, ok = TryCompactNetworkResponse([]string{"https", "api.example.com/logs"}, body)
	if !ok {
		t.Fatal("HTTPie https non-JSON should be handled to block later log reduction")
	}
	if string(out) != string(body) {
		t.Fatalf("HTTPie non-JSON network output must full-pass, got %q", out)
	}
}

func TestTryCompactNetworkResponse_UnrelatedCommand(t *testing.T) {
	t.Parallel()

	if _, ok := TryCompactNetworkResponse([]string{"gh", "api", "/repos/x/y"}, []byte(`{"ok":true}`)); ok {
		t.Fatal("non-network command must not match network guard")
	}
}

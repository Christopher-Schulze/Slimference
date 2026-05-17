package filter

import (
	"strings"
	"testing"
)

func TestTryCompactSARIFRejectsNoSchemaOrVersion(t *testing.T) {
	in := `{"runs":[{"tool":{"driver":{"name":"tool"}},"results":[{"level":"warning"}]}]}`
	if _, ok := TryCompactSARIF(nil, []byte(in)); ok {
		t.Fatal("SARIF without schema/version must not match")
	}
}

func TestTryCompactSARIFEmptyRunsUsesGenericToolLabel(t *testing.T) {
	body := []byte(`{"version":"2.1.0","runs":[{}]}`)
	out, ok := TryCompactSARIF([]string{"tool", "--format", "sarif"}, body)
	if !ok {
		t.Fatal("valid empty SARIF should compact")
	}
	if !strings.Contains(string(out), "sarif") {
		t.Fatalf("generic label missing: %s", out)
	}
}

func TestTryCompactTestOutputStartsWithGoJSON(t *testing.T) {
	in := strings.Join([]string{
		`{"Time":"2026-05-17T00:00:00Z","Action":"run","Package":"pkg","Test":"TestX"}`,
		`{"Time":"2026-05-17T00:00:00Z","Action":"pass","Package":"pkg","Test":"TestX","Elapsed":0.01}`,
		`{"Time":"2026-05-17T00:00:00Z","Action":"pass","Package":"pkg","Elapsed":0.01}`,
	}, "\n")
	out, ok := TryCompactTestOutput([]string{"go", "test", "-json", "./..."}, []byte(in))
	if !ok {
		t.Fatal("expected go test json compaction")
	}
	if !strings.Contains(string(out), "go test") && !strings.Contains(string(out), "passed") {
		t.Fatalf("unexpected compacted output: %q", out)
	}
}

func TestTryCompactCargoTestJSONSkipsMalformedJSONLine(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"suite","event":"started","test_count":1}`,
		`{malformed`,
		`{"type":"suite","event":"ok","passed":1,"failed":0}`,
	}, "\n")
	out, ok := TryCompactCargoTestJSON([]string{"cargo", "test", "--", "--format", "json"}, []byte(in))
	if !ok {
		t.Fatal("expected malformed line to be skipped after valid events")
	}
	if !strings.Contains(string(out), "1 passed") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestVitestLabelFallback(t *testing.T) {
	if got := vitestLabelFromArgv([]string{"node", "runner", "--json"}); got != "test json" {
		t.Fatalf("label=%q", got)
	}
}

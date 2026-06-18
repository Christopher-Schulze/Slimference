package proxy

import (
	"context"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSSafeExactJQJSONBoundary(t *testing.T) {
	prettyJSON := "{\n  \"status\": \"ok\",\n  \"count\": 42\n}\n"
	if !wssSafeStatefulStatusCommandOutput("jq . package.json", prettyJSON) {
		t.Fatal("exact jq JSON whitespace minify should be stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("jq -r .name package.json", "plain response\nplain response\n") {
		t.Fatal("jq non-JSON output must not become stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("cat package.json", prettyJSON) {
		t.Fatal("non-jq JSON must not enter the jq exact-minify gate")
	}
}

func TestWSSStatefulSafeJQJSONExactMinifyCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: jq-json-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssNetworkJSONFixture(90)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-jq-json", "call_jq_json", "jq . package.json", envelope, "stateful-jq-json-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle jq JSON request: %v", err)
	}
	if !replace {
		t.Fatal("full-history exact jq JSON output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, `\"final\":\"kept\"`) ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "{object,") ||
		strings.Contains(body, `\"items\": [`) {
		t.Fatalf("jq JSON output was not exact-minified and archive-backed: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe jq JSON should save without structured guard: %+v", summary)
	}
}

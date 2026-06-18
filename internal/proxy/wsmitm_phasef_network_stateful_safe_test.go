package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSSafeExactNetworkResponseBoundary(t *testing.T) {
	prettyJSON := "{\n  \"status\": \"ok\",\n  \"count\": 42\n}\n"
	if !wssSafeStatefulStatusCommandOutput("curl https://api.example.com/data", prettyJSON) {
		t.Fatal("exact curl JSON whitespace minify should be stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("curl https://api.example.com/logs", "INFO boot\nINFO ready\n") {
		t.Fatal("curl non-JSON logs must not become stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("gh api /repos/x/y", prettyJSON) {
		t.Fatal("non-network JSON must not enter the network exact-minify gate")
	}
}

func TestWSSStatefulSafeNetworkJSONExactMinifyCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: network-json-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssNetworkJSONFixture(90)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-network-json", "call_network_json", "curl https://api.example.com/data", envelope, "stateful-network-json-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle network JSON request: %v", err)
	}
	if !replace {
		t.Fatal("full-history exact network JSON output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, `\"final\":\"kept\"`) ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "{object,") ||
		strings.Contains(body, `\"items\": [`) {
		t.Fatalf("network JSON output was not exact-minified and archive-backed: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe network JSON should save without structured guard: %+v", summary)
	}
}

func wssNetworkJSONFixture(count int) string {
	var out strings.Builder
	out.WriteString("{\n  \"items\": [\n")
	for i := 0; i < count; i++ {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "    {\"id\": %d, \"name\": \"item-%03d\", \"value\": \"payload-%03d\"}", i, i, i)
	}
	out.WriteString("\n  ],\n  \"final\": \"kept\"\n}\n")
	return out.String()
}

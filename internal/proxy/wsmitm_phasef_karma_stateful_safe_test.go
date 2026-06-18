package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeKarmaAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: karma-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssKarmaAllPassFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-karma-all-pass", "call_karma_all_pass", "karma start --single-run", envelope, "stateful-karma-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle karma all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history karma all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[karma] ok (TOTAL: 120 SUCCESS)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "Chrome Headless 126.0.0.0") {
		t.Fatalf("karma all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe karma all-pass should save without structured guard: %+v", summary)
	}
}

func wssKarmaAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("18 06 2026 12:00:00.000:INFO [karma-server]: Karma v6.4.0 server started at http://localhost:9876/\n")
	out.WriteString("18 06 2026 12:00:00.010:INFO [launcher]: Launching browsers ChromeHeadless with concurrency unlimited\n")
	out.WriteString("18 06 2026 12:00:00.020:INFO [launcher]: Starting browser ChromeHeadless\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "Chrome Headless 126.0.0.0 (Mac OS 10.15.7): Executed %d of %d SUCCESS (0.%03d secs / 0.%03d secs)\n", count, count, i%1000, i%500)
	}
	fmt.Fprintf(&out, "TOTAL: %d SUCCESS\n", count)
	return out.String()
}

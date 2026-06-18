package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeMinitestAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: minitest-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssMinitestAllPassFixture(5000)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-minitest-all-pass", "call_minitest_all_pass", "bundle exec ruby -Itest test/models/user_test.rb", envelope, "stateful-minitest-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle minitest all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history minitest all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[minitest] ok - 5000 runs, 10000 assertions") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, strings.Repeat(".", 40)) {
		t.Fatalf("minitest all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe minitest all-pass should save without structured guard: %+v", summary)
	}
}

func wssMinitestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("Run options: --seed 43125\n\n# Running:\n\n")
	out.WriteString(strings.Repeat(".", count))
	out.WriteString("\n\nFinished in 0.421998s, 11848.3973 runs/s, 23696.7946 assertions/s.\n")
	fmt.Fprintf(&out, "%d runs, %d assertions, 0 failures, 0 errors, 0 skips\n", count, count*2)
	return out.String()
}

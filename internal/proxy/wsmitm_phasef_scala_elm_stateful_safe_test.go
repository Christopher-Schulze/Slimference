package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeScalaElmAllPassCompactsFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		output    string
		want      string
		forbidden string
	}{
		{
			name:      "sbt",
			command:   "sbt -batch test",
			output:    wssScalaStyleAllPassFixture(120),
			want:      "[sbt test] ok (120 succeeded)",
			forbidden: "generated case 119",
		},
		{
			name:      "mill",
			command:   "./mill foo.test",
			output:    wssMillScalaStyleAllPassFixture(120),
			want:      "[mill test] ok (120 succeeded)",
			forbidden: "generated case 119",
		},
		{
			name:      "elm",
			command:   "elm-test",
			output:    wssElmTestAllPassFixture(120),
			want:      "[elm-test] ok (Passed: 120; Failed: 0)",
			forbidden: "generated case 119",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Compression.OutputReduce.StopSequencesEnabled = false
			cfg.Compression.OutputReduce.BeTerseHintEnabled = false
			cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
			cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
			p := New(cfg)
			adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
			envelope := "Chunk ID: " + tt.name + "-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 12000\nOutput:\n" + tt.output

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name+"-all-pass", "call_"+tt.name+"_all_pass", tt.command, envelope, "stateful-"+tt.name+"-safe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle %s all-pass request: %v", tt.name, err)
			}
			if !replace {
				t.Fatalf("full-history %s all-pass output should compact", tt.name)
			}
			body := string(env.Body)
			if !strings.Contains(body, tt.want) ||
				!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				strings.Contains(body, tt.forbidden) {
				t.Fatalf("%s all-pass output was not archive-backed compacted: %s", tt.name, body)
			}
			summary := p.DebugRecorder().Last(1, false)[0]
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("stateful-safe %s all-pass should save without structured guard: %+v", tt.name, summary)
			}
		})
	}
}

func TestWSSStatefulUnsafeScalaElmSignalsDoNotBecomeAllPass(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		output      string
		mustContain string
		forbidden   string
	}{
		{
			name:        "sbt pending",
			command:     "sbt -batch test",
			output:      wssScalaElmUnsafeEnvelope("sbt", strings.Replace(wssScalaStyleAllPassFixture(40), "pending 0", "pending 1", 1)),
			mustContain: "pending 1",
			forbidden:   "[sbt test] ok",
		},
		{
			name:        "sbt aborted",
			command:     "sbt -batch test",
			output:      wssScalaElmUnsafeEnvelope("sbt-aborted", strings.Replace(wssScalaStyleAllPassFixture(40), "aborted 0", "aborted 1", 1)),
			mustContain: "aborted 1",
			forbidden:   "[sbt test] ok",
		},
		{
			name:        "mill warning",
			command:     "./mill foo.test",
			output:      wssScalaElmUnsafeEnvelope("mill", wssMillScalaStyleAllPassFixture(40)+"[warn] flaky dependency resolution\n"),
			mustContain: "[warn] flaky",
			forbidden:   "[mill test] ok",
		},
		{
			name:        "elm failed",
			command:     "elm-test",
			output:      wssScalaElmUnsafeEnvelope("elm", strings.Replace(wssElmTestAllPassFixture(40), "Failed:   0", "Failed:   1", 1)),
			mustContain: "Failed:   1",
			forbidden:   "[elm-test] ok",
		},
		{
			name:        "elm warning",
			command:     "npx -y elm-test",
			output:      wssScalaElmUnsafeEnvelope("elm-warning", wssElmTestAllPassFixture(40)+"Warning: generated diagnostics\n"),
			mustContain: "Warning: generated diagnostics",
			forbidden:   "[elm-test] ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Compression.OutputReduce.StopSequencesEnabled = false
			cfg.Compression.OutputReduce.BeTerseHintEnabled = false
			cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
			cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
			p := New(cfg)
			adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+strings.ReplaceAll(tt.name, " ", "-")+"-unsafe", "call_"+strings.ReplaceAll(tt.name, " ", "_")+"_unsafe", tt.command, tt.output, "stateful-"+strings.ReplaceAll(tt.name, " ", "-")+"-unsafe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe %s request: %v", tt.name, err)
			}
			body := string(env.Body)
			archiveBacked := strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://")
			if strings.Contains(body, tt.forbidden) || (!strings.Contains(body, tt.mustContain) && !archiveBacked) {
				t.Fatalf("unsafe %s output changed unexpectedly: %s", tt.name, body)
			}
			if replace && !archiveBacked {
				t.Fatalf("unsafe %s mutation must keep archive recovery: %s", tt.name, body)
			}
		})
	}
}

func TestWSSCompactedTestOutputOKEdges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		compacted string
		want      bool
	}{
		{name: "explicit ok", compacted: "[sbt test] ok (2 succeeded)\n", want: true},
		{name: "passed prose", compacted: "[elm-test] 2 tests passed\n", want: true},
		{name: "missing bracket", compacted: "elm-test ok\n", want: false},
		{name: "failed", compacted: "[sbt test] failed\n", want: false},
		{name: "warning", compacted: "[elm-test] warning emitted\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wssCompactedTestOutputOK([]byte(tt.compacted)); got != tt.want {
				t.Fatalf("wssCompactedTestOutputOK(%q)=%v want %v", tt.compacted, got, tt.want)
			}
		})
	}
}

func wssScalaStyleAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("[info] Compiling 1 Scala source to /repo/target...\n")
	out.WriteString("[info] ExampleSuite:\n")
	for i := range count {
		fmt.Fprintf(&out, "[info] - generated case %03d\n", i)
	}
	fmt.Fprintf(&out, "[info] Total number of tests run: %d\n", count)
	out.WriteString("[info] Suites: completed 1, aborted 0\n")
	fmt.Fprintf(&out, "[info] Tests: succeeded %d, failed 0, canceled 0, ignored 0, pending 0\n", count)
	out.WriteString("[info] All tests passed.\n")
	out.WriteString("[success] Total time: 2 s, completed Jun 18, 2026\n")
	return out.String()
}

func wssMillScalaStyleAllPassFixture(count int) string {
	return "[12/12] foo.test\n" + wssScalaStyleAllPassFixture(count)
}

func wssElmTestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("elm-test 0.19.1-revision6\n")
	out.WriteString("-------------------------\n\n")
	fmt.Fprintf(&out, "Running %d tests. To reproduce these results, run: elm-test --fuzz 100 --seed 148067075282531\n", count)
	for i := range count {
		fmt.Fprintf(&out, "generated case %03d passed\n", i)
	}
	out.WriteString("\nTEST RUN PASSED\n\n")
	out.WriteString("Duration: 121 ms\n")
	fmt.Fprintf(&out, "Passed:   %d\n", count)
	out.WriteString("Failed:   0\n")
	return out.String()
}

func wssScalaElmUnsafeEnvelope(label, tail string) string {
	var out strings.Builder
	out.WriteString("Chunk ID: " + label + "-unsafe\n")
	out.WriteString("Wall time: 0.0010 seconds\n")
	out.WriteString("Process exited with code 0\n")
	out.WriteString("Original token count: 12000\n")
	out.WriteString("Output:\n")
	for i := range 80 {
		fmt.Fprintf(&out, "unsafe %s prelude %03d\n", label, i)
	}
	out.WriteString(tail)
	return out.String()
}

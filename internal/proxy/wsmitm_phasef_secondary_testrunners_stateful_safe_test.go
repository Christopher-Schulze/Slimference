package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeSecondaryTestRunnersAllPassCompactFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		output    string
		want      string
		forbidden string
	}{
		{
			name:      "phpunit",
			command:   "phpunit tests/",
			output:    wssPhpunitAllPassFixture(120),
			want:      "[phpunit] ok (120 tests, 360 assertions)",
			forbidden: "120 / 120 (100%)",
		},
		{
			name:      "gradle",
			command:   "./gradlew test",
			output:    wssGradleTestAllPassFixture(120),
			want:      "[gradle test] ok (BUILD SUCCESSFUL in 2s)",
			forbidden: "> Task :module119:test",
		},
		{
			name:      "dart",
			command:   "dart test",
			output:    wssDartTestAllPassFixture(120),
			want:      "[dart test] ok (00:02 +120: All tests passed!)",
			forbidden: "test/generated_119_test.dart",
		},
		{
			name:      "flutter",
			command:   "flutter test",
			output:    wssFlutterTestAllPassFixture(120),
			want:      "[flutter test] ok (00:03 +120: All tests passed!)",
			forbidden: "test/widget_119_test.dart",
		},
		{
			name:      "deno",
			command:   "deno test --allow-all",
			output:    wssDenoTestAllPassFixture(120),
			want:      "[deno test] ok (ok | 120 passed | 0 failed (123ms))",
			forbidden: "generated_119 ... ok",
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
			envelope := "Chunk ID: " + tt.name + "-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" + tt.output

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

func TestWSSStatefulUnsafeSecondaryTestRunnerSignalsDoNotBecomeAllPass(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		output      string
		mustContain string
		forbidden   string
	}{
		{
			name:        "phpunit warnings",
			command:     "phpunit tests/",
			output:      wssSecondaryTestUnsafeEnvelope("phpunit", "OK, but there were issues!\nTests: 120, Assertions: 360, Warnings: 1.\n"),
			mustContain: "Warnings: 1.",
			forbidden:   "[phpunit] ok",
		},
		{
			name:        "gradle deprecation",
			command:     "./gradlew test",
			output:      wssSecondaryTestUnsafeEnvelope("gradle", "BUILD SUCCESSFUL in 1s\nDeprecated Gradle features were used in this build.\n"),
			mustContain: "Deprecated Gradle features",
			forbidden:   "[gradle test] ok",
		},
		{
			name:        "dart failure",
			command:     "dart test",
			output:      wssSecondaryTestUnsafeEnvelope("dart", "00:01 +0 -1: Some tests failed.\n"),
			mustContain: "Some tests failed.",
			forbidden:   "[dart test] ok",
		},
		{
			name:        "flutter warning",
			command:     "flutter test",
			output:      wssSecondaryTestUnsafeEnvelope("flutter", "00:01 +1: All tests passed!\nWarning: golden images changed\n"),
			mustContain: "Warning: golden images changed",
			forbidden:   "[flutter test] ok",
		},
		{
			name:        "deno warning",
			command:     "deno test --allow-all",
			output:      wssSecondaryTestUnsafeEnvelope("deno", "ok | 119 passed | 0 failed (123ms)\nWarning experimental API\n"),
			mustContain: "Warning experimental API",
			forbidden:   "[deno test] ok",
		},
		{
			name:        "karma browser log",
			command:     "karma start --single-run",
			output:      wssSecondaryTestUnsafeEnvelope("karma", "Chrome Headless LOG: useful console evidence\nTOTAL: 120 SUCCESS\n"),
			mustContain: "LOG: useful console evidence",
			forbidden:   "[karma] ok",
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name+"-unsafe", "call_"+strings.ReplaceAll(tt.name, " ", "_")+"_unsafe", tt.command, tt.output, "stateful-"+strings.ReplaceAll(tt.name, " ", "-")+"-unsafe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe %s request: %v", tt.name, err)
			}
			body := string(env.Body)
			if strings.Contains(body, tt.forbidden) || !strings.Contains(body, tt.mustContain) {
				t.Fatalf("unsafe %s output changed unexpectedly: %s", tt.name, body)
			}
			if replace && !strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") {
				t.Fatalf("unsafe %s mutation must keep archive recovery: %s", tt.name, body)
			}
		})
	}
}

func wssPhpunitAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("PHPUnit 10.5.0 by Sebastian Bergmann and contributors.\n\n")
	for i := 0; i < count; i++ {
		if i > 0 && i%40 == 0 {
			fmt.Fprintf(&out, " %d / %d (%d%%)\n", i, count, i*100/count)
		}
		out.WriteByte('.')
	}
	fmt.Fprintf(&out, " %d / %d (100%%)\n\n", count, count)
	out.WriteString("Time: 00:00.123, Memory: 12.00 MB\n\n")
	fmt.Fprintf(&out, "OK (%d tests, %d assertions)\n", count, count*3)
	return out.String()
}

func wssGradleTestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("> Task :compileJava\n> Task :testClasses\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "> Task :module%03d:test\n", i)
	}
	out.WriteString("BUILD SUCCESSFUL in 2s\n")
	fmt.Fprintf(&out, "%d actionable tasks: %d executed\n", count+2, count+2)
	return out.String()
}

func wssDartTestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("00:00 +0: loading test/generated_000_test.dart\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "00:01 +%d: test/generated_%03d_test.dart: renders generated case %03d\n", i+1, i, i)
	}
	fmt.Fprintf(&out, "00:02 +%d: All tests passed!\n", count)
	return out.String()
}

func wssFlutterTestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("00:00 +0: loading test/widget_000_test.dart\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "00:02 +%d: test/widget_%03d_test.dart: paints generated widget %03d\n", i+1, i, i)
	}
	fmt.Fprintf(&out, "00:03 +%d: All tests passed!\n", count)
	return out.String()
}

func wssDenoTestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("Check file:///repo/generated_test.ts\n")
	fmt.Fprintf(&out, "running %d tests from ./generated_test.ts\n", count)
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "generated_%03d ... ok (1ms)\n", i)
	}
	fmt.Fprintf(&out, "ok | %d passed | 0 failed (123ms)\n", count)
	return out.String()
}

func wssSecondaryTestUnsafeEnvelope(label, tail string) string {
	var out strings.Builder
	out.WriteString("Chunk ID: " + label + "-unsafe\n")
	out.WriteString("Wall time: 0.0010 seconds\n")
	out.WriteString("Process exited with code 0\n")
	out.WriteString("Original token count: 10000\n")
	out.WriteString("Output:\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&out, "unsafe %s prelude %03d\n", label, i)
	}
	out.WriteString(tail)
	return out.String()
}

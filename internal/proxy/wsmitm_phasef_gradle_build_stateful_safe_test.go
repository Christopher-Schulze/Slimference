package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeGradleBuildCleanSuccessCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	output := wssGradleBuildEnvelope("gradle-build-safe", wssGradleBuildCleanSuccessFixture(80))
	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-gradle-build-clean", "call_gradle_build_clean", "gradle build --parallel", output, "stateful-gradle-build-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle gradle build clean request: %v", err)
	}
	if !replace {
		t.Fatal("full-history gradle build clean success should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[gradle build] ok (80 actionable tasks: 80 executed)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, ":module79:compileJava") {
		t.Fatalf("gradle build clean success was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe gradle build should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeGradleBuildUnsafeSuccessRejectsFalseOK(t *testing.T) {
	tests := []struct {
		name         string
		output       string
		mustPreserve string
	}{
		{
			name:         "application log",
			output:       wssGradleBuildEnvelope("gradle-app-log", "> Task :runGenerator\nGenerated production config\nBUILD SUCCESSFUL in 1s\n1 actionable task: 1 executed\n"),
			mustPreserve: "Generated production config",
		},
		{
			name:         "deprecation",
			output:       wssGradleBuildEnvelope("gradle-deprecated", wssGradleBuildCleanSuccessFixture(8)+"Deprecated Gradle features were used in this build.\n"),
			mustPreserve: "Deprecated Gradle features",
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name, "call_"+strings.ReplaceAll(tt.name, " ", "_"), "gradle build", tt.output, "stateful-gradle-build-unsafe-session"))
			_, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe gradle build request: %v", err)
			}
			body := string(env.Body)
			if strings.Contains(body, "[gradle build] ok") || !strings.Contains(body, tt.mustPreserve) {
				t.Fatalf("unsafe gradle build success hid detail as OK: %s", body)
			}
		})
	}
}

func wssGradleBuildEnvelope(id, output string) string {
	return "Chunk ID: " + id + "\n" +
		"Wall time: 0.0010 seconds\n" +
		"Process exited with code 0\n" +
		"Original token count: 10000\n" +
		"Output:\n" +
		output
}

func wssGradleBuildCleanSuccessFixture(tasks int) string {
	var b strings.Builder
	b.WriteString("Starting a Gradle Daemon, 1 busy Daemon could not be reused, use --status for details\n")
	for i := 0; i < tasks; i++ {
		fmt.Fprintf(&b, "> Task :module%d:compileJava\n", i)
	}
	b.WriteString("BUILD SUCCESSFUL in 4s\n")
	fmt.Fprintf(&b, "%d actionable tasks: %d executed\n", tasks, tasks)
	return b.String()
}

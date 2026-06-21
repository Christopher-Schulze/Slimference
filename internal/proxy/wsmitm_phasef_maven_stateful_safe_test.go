package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeMavenCleanSuccessCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, wssCommandOutputRequestBody(
		"resp-maven-safe",
		"call_maven_safe",
		"mvn test",
		webBuildCleanEnvelope("maven-safe", wssMavenCleanSuccessFixture(48)),
		"stateful-maven-safe-session",
	))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle maven clean success request: %v", err)
	}
	if !replace {
		t.Fatal("full-history maven clean success should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[mvn] ok (Tests run: 42, Failures: 0, Errors: 0, Skipped: 0)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "maven-resources-plugin:3.3.1:resources (default-resources-47)") {
		t.Fatalf("maven clean success was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe maven clean success should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeMavenUnsafeOutputRejectsFalseOK(t *testing.T) {
	tests := []struct {
		name         string
		output       string
		mustPreserve string
	}{
		{
			name:         "warning",
			output:       webBuildCleanEnvelope("maven-warning", wssMavenCleanSuccessFixture(8)+"[WARNING] Using platform encoding UTF-8 to copy filtered resources.\n"),
			mustPreserve: "[WARNING] Using platform encoding",
		},
		{
			name:         "source context",
			output:       webBuildCleanEnvelope("maven-source-context", wssMavenCleanSuccessFixture(8)+"[INFO] public class App {}\n"),
			mustPreserve: "public class App {}",
		},
		{
			name:         "arbitrary info log",
			output:       webBuildCleanEnvelope("maven-app-log", wssMavenCleanSuccessFixture(8)+"[INFO] application bootstrap token refreshed\n"),
			mustPreserve: "application bootstrap token refreshed",
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

			env := parseWSJSON(t, wssCommandOutputRequestBody(
				"resp-maven-"+tt.name,
				"call_maven_"+strings.ReplaceAll(tt.name, " ", "_"),
				"mvn test",
				tt.output,
				"stateful-maven-unsafe-session",
			))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe maven request: %v", err)
			}
			body := string(env.Body)
			if strings.Contains(body, "[mvn] ok") ||
				strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				!strings.Contains(body, tt.mustPreserve) {
				t.Fatalf("unsafe maven output hid detail as OK: replace=%v body=%s", replace, body)
			}
		})
	}
}

func wssMavenCleanSuccessFixture(modules int) string {
	var b strings.Builder
	b.WriteString("[INFO] Scanning for projects...\n")
	b.WriteString("[INFO] \n")
	b.WriteString("[INFO] -----------------------< com.example:demo >------------------------\n")
	b.WriteString("[INFO] Building demo 1.0.0\n")
	b.WriteString("[INFO] --------------------------------[ jar ]---------------------------------\n")
	for i := range modules {
		fmt.Fprintf(&b, "[INFO] --- maven-resources-plugin:3.3.1:resources (default-resources-%02d) @ demo ---\n", i)
		fmt.Fprintf(&b, "[INFO] Copying %d resources from src/main/resources to target/classes\n", i+1)
	}
	b.WriteString("[INFO] --- maven-compiler-plugin:3.13.0:compile (default-compile) @ demo ---\n")
	b.WriteString("[INFO] Changes detected - recompiling the module!\n")
	b.WriteString("[INFO] Compiling 3 source files with javac [debug target 21] to target/classes\n")
	b.WriteString("[INFO] --- maven-surefire-plugin:3.2.5:test (default-test) @ demo ---\n")
	b.WriteString("[INFO] Running com.example.DemoTest\n")
	b.WriteString("[INFO] Tests run: 42, Failures: 0, Errors: 0, Skipped: 0\n")
	b.WriteString("[INFO] --- maven-jar-plugin:3.4.1:jar (default-jar) @ demo ---\n")
	b.WriteString("[INFO] Building jar: /repo/target/demo.jar\n")
	b.WriteString("[INFO] ------------------------------------------------------------------------\n")
	b.WriteString("[INFO] BUILD SUCCESS\n")
	b.WriteString("[INFO] ------------------------------------------------------------------------\n")
	b.WriteString("[INFO] Total time:  4.123 s\n")
	b.WriteString("[INFO] Finished at: 2026-06-19T01:02:03Z\n")
	b.WriteString("[INFO] ------------------------------------------------------------------------\n")
	return b.String()
}

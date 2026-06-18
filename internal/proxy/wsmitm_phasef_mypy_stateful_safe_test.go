package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeMypyDiagnosticsCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := wssMypyDiagnosticEnvelope(false, false)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-mypy-diagnostics", "call_mypy_diagnostics", "mypy src", envelope, "stateful-mypy-diagnostics-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle mypy diagnostics request: %v", err)
	}
	if !replace {
		t.Fatal("full-history mypy diagnostics should compact")
	}
	body := string(env.Body)
	for _, want := range []string{
		"[mypy] FAILED (81 diagnostics)",
		"(repeated 80 times)",
		"src/app.py:10: note: expected str",
		"Found 80 errors in 1 file",
		"[context-archive kind=tool-output uri=local-archive://",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("mypy diagnostics missing %q in body=%s", want, body)
		}
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe mypy diagnostics should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafePackageManagerMypyDiagnosticsCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: package-mypy-diagnostics-safe\nWall time: 0.0010 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" +
		"> api@1.0.0 typecheck /repo\n> mypy src\n" + wssMypyDiagnosticPayload(false, false)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-mypy-diagnostics", "call_package_mypy_diagnostics", "pnpm run typecheck", envelope, "stateful-package-mypy-diagnostics-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle package mypy diagnostics request: %v", err)
	}
	if !replace {
		t.Fatal("full-history package mypy diagnostics should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[mypy] FAILED (81 diagnostics)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "api@1.0.0 typecheck") {
		t.Fatalf("package mypy diagnostics were not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe package mypy diagnostics should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulUnsafeMypyDiagnosticsStayGuarded(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{name: "stub notice", output: wssMypyDiagnosticEnvelope(true, false)},
		{name: "source context", output: wssMypyDiagnosticEnvelope(false, true)},
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-mypy-unsafe", "call_mypy_unsafe", "mypy src", tt.output, "stateful-mypy-unsafe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe mypy diagnostics request: %v", err)
			}
			body := string(env.Body)
			if replace ||
				strings.Contains(body, "[mypy] FAILED (") ||
				strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") {
				t.Fatalf("unsafe mypy diagnostics should stay byte-identical: replace=%v body=%s", replace, body)
			}
		})
	}
}

func wssMypyDiagnosticEnvelope(includeNotice bool, includeSource bool) string {
	var output strings.Builder
	output.WriteString("Chunk ID: mypy-diagnostics-safe\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 1\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	output.WriteString(wssMypyDiagnosticPayload(includeNotice, includeSource))
	return output.String()
}

func wssMypyDiagnosticPayload(includeNotice bool, includeSource bool) string {
	var output strings.Builder
	if includeNotice {
		output.WriteString("Skipping analyzing 'requests': module is installed, but missing library stubs\n")
	}
	for i := 0; i < 80; i++ {
		fmt.Fprintln(&output, "src/app.py:10: error: Incompatible return value type")
	}
	output.WriteString("src/app.py:10: note: expected str\n")
	if includeSource {
		output.WriteString("if value:\n")
	}
	output.WriteString("Found 80 errors in 1 file (checked 48 source files)\n")
	return output.String()
}

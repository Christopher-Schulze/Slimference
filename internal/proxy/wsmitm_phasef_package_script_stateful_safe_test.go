package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafePackageManagerLintScriptCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var output strings.Builder
	output.WriteString("Chunk ID: package-script-lint-safe\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 0\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	for i := range 120 {
		fmt.Fprintf(&output, "> workspace lint prelude %03d\n", i)
	}
	output.WriteString("> web@1.0.0 lint /repo\n")
	output.WriteString("> eslint .\n")

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-script-lint", "call_package_script_lint", "pnpm run lint", output.String(), "stateful-package-script-lint-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle package-manager lint script request: %v", err)
	}
	if !replace {
		t.Fatal("full-history package-manager lint script output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[eslint] ok") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "workspace lint prelude 119") {
		t.Fatalf("package-manager lint script output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe package-manager lint script should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulUnsafePackageManagerScriptStaysGuarded(t *testing.T) {
	tests := []struct {
		name    string
		command string
		output  string
	}{
		{
			name:    "unsafe script name",
			command: "npm run deploy",
			output:  packageManagerScriptUnsafeEnvelope("deploy", "> eslint .\n"),
		},
		{
			name:    "inner shell pipeline",
			command: "npm run lint",
			output:  packageManagerScriptUnsafeEnvelope("lint", "> eslint . | tee lint.log\n"),
		},
		{
			name:    "warning payload",
			command: "npm run lint",
			output:  packageManagerScriptUnsafeEnvelope("lint", "> eslint .\nwarning: generated config is deprecated\n"),
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-script-unsafe", "call_package_script_unsafe", tt.command, tt.output, "stateful-package-script-unsafe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe package-manager script request: %v", err)
			}
			if replace {
				t.Fatalf("unsafe package-manager script should stay byte-identical, body=%s", env.Body)
			}
			if !strings.Contains(string(env.Body), "workspace unsafe prelude 079") ||
				strings.Contains(string(env.Body), "[context-archive kind=tool-output uri=local-archive://") {
				t.Fatalf("unsafe package-manager script output changed unexpectedly: %s", env.Body)
			}
		})
	}
}

func TestWSSStatefulSafePackageManagerLintScriptFailureCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var output strings.Builder
	output.WriteString("Chunk ID: package-script-lint-failure\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 1\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	for i := range 120 {
		fmt.Fprintf(&output, "> workspace lint failure prelude %03d\n", i)
	}
	output.WriteString("> web@1.0.0 lint /repo\n")
	output.WriteString("> errcheck ./...\n")
	for range 90 {
		output.WriteString("internal/proxy/handler.go:164:15: Close() error return value is not checked\n")
	}

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-script-lint-failure", "call_package_script_lint_failure", "pnpm run lint", output.String(), "stateful-package-script-lint-failure-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle package-manager lint failure request: %v", err)
	}
	if !replace {
		t.Fatal("full-history package-manager lint failure output should compact")
	}
	body := string(env.Body)
	for _, want := range []string{
		"[errcheck] FAILED (90 diagnostics)",
		"(repeated 90 times)",
		"[context-archive kind=tool-output uri=local-archive://",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("package-manager lint failure output missing %q in body=%s", want, body)
		}
	}
	if strings.Contains(body, "workspace lint failure prelude 119") {
		t.Fatalf("package-manager lint failure prelude was not compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe package-manager lint failure should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulUnsafePackageManagerLintFailureStaysGuarded(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	output := packageManagerScriptUnsafeEnvelope("lint", strings.Join([]string{
		"> errcheck ./...",
		"internal/proxy/handler.go:164:15: Close() error return value is not checked",
		"if err != nil {",
		"",
	}, "\n"))

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-script-lint-failure-unsafe", "call_package_script_lint_failure_unsafe", "pnpm run lint", output, "stateful-package-script-lint-failure-unsafe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle unsafe package-manager lint failure request: %v", err)
	}
	body := string(env.Body)
	if replace ||
		strings.Contains(body, "[errcheck] FAILED (") ||
		strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "if err != nil {") {
		t.Fatalf("unsafe package-manager lint failure should stay byte-identical: replace=%v body=%s", replace, body)
	}
}

func packageManagerScriptUnsafeEnvelope(script, tail string) string {
	var output strings.Builder
	output.WriteString("Chunk ID: package-script-unsafe\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 0\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	for i := range 80 {
		fmt.Fprintf(&output, "> workspace unsafe prelude %03d\n", i)
	}
	fmt.Fprintf(&output, "> web@1.0.0 %s /repo\n", script)
	output.WriteString(tail)
	return output.String()
}

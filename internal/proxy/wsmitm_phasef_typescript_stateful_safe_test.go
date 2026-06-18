package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeTypeScriptDiagnosticsCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := wssTypeScriptDiagnosticEnvelope(false, true)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-tsc-diagnostics", "call_tsc_diagnostics", "pnpm exec tsc --noEmit", envelope, "stateful-tsc-diagnostics-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle TypeScript diagnostics request: %v", err)
	}
	if !replace {
		t.Fatal("full-history TypeScript diagnostics should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[typescript] FAILED") ||
		!strings.Contains(body, "TS2322") ||
		!strings.Contains(body, "TS2304") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "tsc progress 119") {
		t.Fatalf("TypeScript diagnostics were not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe TypeScript diagnostics should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafePackageManagerTypeScriptDiagnosticsCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: package-tsc-diagnostics-safe\nWall time: 0.0010 seconds\nProcess exited with code 2\nOriginal token count: 10000\nOutput:\n" +
		"> web@1.0.0 typecheck /repo\n> tsc --noEmit\n" + wssTypeScriptDiagnosticPayload(false, true)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-tsc-diagnostics", "call_package_tsc_diagnostics", "pnpm run typecheck", envelope, "stateful-package-tsc-diagnostics-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle package TypeScript diagnostics request: %v", err)
	}
	if !replace {
		t.Fatal("full-history package TypeScript diagnostics should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[typescript] FAILED") ||
		!strings.Contains(body, "TS2322") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "tsc progress 119") {
		t.Fatalf("package TypeScript diagnostics were not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe package TypeScript diagnostics should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulUnsafeTypeScriptDiagnosticsStayGuarded(t *testing.T) {
	tests := []struct {
		name    string
		command string
		output  string
	}{
		{
			name:    "summary only",
			command: "tsc --noEmit",
			output:  wssTypeScriptSummaryOnlyEnvelope(),
		},
		{
			name:    "source context",
			command: "tsc --noEmit",
			output:  wssTypeScriptDiagnosticEnvelope(true, true),
		},
		{
			name:    "package script source context",
			command: "pnpm run typecheck",
			output: "Chunk ID: package-tsc-unsafe\nWall time: 0.0010 seconds\nProcess exited with code 2\nOriginal token count: 10000\nOutput:\n" +
				"> web@1.0.0 typecheck /repo\n> tsc --noEmit\n" + wssTypeScriptDiagnosticPayload(true, true),
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-tsc-unsafe", "call_tsc_unsafe", tt.command, tt.output, "stateful-tsc-unsafe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe TypeScript diagnostics request: %v", err)
			}
			if replace {
				t.Fatalf("unsafe TypeScript diagnostics should stay byte-identical, body=%s", env.Body)
			}
			body := string(env.Body)
			if !strings.Contains(body, "tsc progress 079") ||
				strings.Contains(body, "[typescript] FAILED") ||
				strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") {
				t.Fatalf("unsafe TypeScript diagnostics output changed unexpectedly: %s", body)
			}
		})
	}
}

func TestWSSStatefulSafeTypeScriptDiagnosticsDeltaStillGuarded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := wssTypeScriptDiagnosticEnvelope(false, true)

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-tsc-delta",
			"prompt_cache_key":     "stateful-tsc-delta-session",
			"input": []map[string]any{
				{"type": "function_call_output", "call_id": "call_tsc_delta", "output": envelope},
			},
			"stream": true,
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle TypeScript diagnostics delta request: %v", err)
	}
	body := string(env.Body)
	if replace ||
		strings.Contains(body, "[typescript] FAILED") ||
		strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("delta TypeScript diagnostics must stay byte-identical under the delta proof gate: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "delta" {
		t.Fatalf("delta TypeScript diagnostics should stay byte-identical: %+v", summary.DebugFacts)
	}
	if summary.DebugFacts["wss.effective_mutation_guard"] != "wss_stateful_delta_mutation_proof_gate" &&
		summary.DebugFacts["wss.bypass_reason"] != "wss_previous_response_tool_output_full_pass" {
		t.Fatalf("delta TypeScript diagnostics should keep a byte-equal guard path: %+v", summary.DebugFacts)
	}
}

func wssTypeScriptDiagnosticEnvelope(includeSource bool, longPrelude bool) string {
	var output strings.Builder
	output.WriteString("Chunk ID: tsc-diagnostics-safe\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 2\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	output.WriteString(wssTypeScriptDiagnosticPayload(includeSource, longPrelude))
	return output.String()
}

func wssTypeScriptDiagnosticPayload(includeSource bool, longPrelude bool) string {
	var output strings.Builder
	limit := 8
	if longPrelude {
		limit = 120
	}
	for i := 0; i < limit; i++ {
		fmt.Fprintf(&output, "tsc progress %03d\n", i)
	}
	output.WriteString("src/app.ts(7,3): error TS2322: Type 'string' is not assignable to type 'number'.\n")
	if includeSource {
		output.WriteString("import { missingName } from './missing';\n")
	}
	output.WriteString("src/routes/+page.ts:3:11 - error TS2304: Cannot find name 'loadData'.\n")
	output.WriteString("Found 2 errors in 2 files.\n")
	return output.String()
}

func wssTypeScriptSummaryOnlyEnvelope() string {
	var output strings.Builder
	output.WriteString("Chunk ID: tsc-diagnostics-weak\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 2\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&output, "tsc progress %03d\n", i)
	}
	output.WriteString("Found 2 errors in 2 files.\n")
	return output.String()
}

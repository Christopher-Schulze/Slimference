package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeEslintStylishFindingsCompactFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name    string
		command string
		payload string
	}{
		{
			name:    "direct",
			command: "eslint src --format stylish",
			payload: wssEslintStylishEnvelope("eslint-stylish-direct", "", 90, true),
		},
		{
			name:    "package script",
			command: "pnpm run lint",
			payload: wssEslintStylishEnvelope("eslint-stylish-package", "> web@1.0.0 lint /repo\n> eslint src --format stylish\n", 90, true),
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name, "call_"+strings.ReplaceAll(tt.name, " ", "_"), tt.command, tt.payload, "stateful-eslint-stylish-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle ESLint stylish request: %v", err)
			}
			if !replace {
				t.Fatal("full-history ESLint stylish output should compact")
			}
			body := string(env.Body)
			for _, want := range []string{
				"[eslint] FINDINGS (180 problems: 90 errors, 90 warnings in 1 file)",
				"src/app.js",
				"2:1 warning [no-console] Unexpected console statement",
				"2:20 error [eqeqeq] Expected '===' and instead saw '=='",
				"[context-archive kind=tool-output uri=local-archive://",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("ESLint stylish compaction missing %q in body=%s", want, body)
				}
			}
			if strings.Contains(body, "eslint stylish prelude 089") {
				t.Fatalf("ESLint stylish prelude was not compacted: %s", body)
			}
			summary := p.DebugRecorder().Last(1, false)[0]
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("stateful-safe ESLint stylish should save without structured guard: %+v", summary)
			}
		})
	}
}

func TestWSSStatefulUnsafeEslintStylishCodeframeStaysGuarded(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	payload := strings.Join([]string{
		"Chunk ID: eslint-stylish-unsafe",
		"Wall time: 0.0010 seconds",
		"Process exited with code 1",
		"Original token count: 10000",
		"Output:",
		"src/app.js",
		"  1:1  error  Unexpected console statement  no-console",
		"  1 | console.log('x')",
		"    | ^^^^^^^^^^^",
		"✖ 1 problem (1 error, 0 warnings)",
		"",
	}, "\n")

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-eslint-stylish-unsafe", "call_eslint_stylish_unsafe", "eslint src", payload, "stateful-eslint-stylish-unsafe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle unsafe ESLint stylish request: %v", err)
	}
	body := string(env.Body)
	if replace ||
		strings.Contains(body, "[eslint] FINDINGS (") ||
		strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "console.log('x')") {
		t.Fatalf("unsafe ESLint codeframe should stay byte-identical: replace=%v body=%s", replace, body)
	}
}

func TestWSSEslintStylishCompactedDiagnosticPredicate(t *testing.T) {
	valid := strings.Join([]string{
		"[eslint] FINDINGS (2 problems: 1 error, 1 warning in 1 file)",
		"src/app.tsx",
		"  2:1 warning [no-console] Unexpected console statement",
		"  2:20 error [eqeqeq] Expected '===' and instead saw '=='",
		"1 error and 0 warnings potentially fixable with the `--fix` option.",
	}, "\n")
	if !wssCompactedEslintStylishDiagnostic([]byte(valid)) {
		t.Fatal("valid compacted ESLint stylish diagnostics should be WSS-safe")
	}

	invalids := []string{
		"[eslint] ok",
		"[eslint] FINDINGS (1 problem: 1 error, 0 warnings in 1 file)\n  2:1 error [rule] missing file",
		"[eslint] FINDINGS (1 problem: 1 error, 0 warnings in 1 file)\nsrc/app.tsx\n  two:1 error [rule] bad location",
		"[eslint] FINDINGS (1 problem: 1 error, 0 warnings in 1 file)\nsrc/app.tsx\n  2:1 info [rule] bad severity",
		"[eslint] FINDINGS (1 problem: 1 error, 0 warnings in 1 file)\nsrc/app.tsx\n  2:1 error rule missing brackets",
		"[eslint] FINDINGS (1 problem: 1 error, 0 warnings in 1 file)\nREADME.md\n  2:1 error [rule] unsupported file",
	}
	for _, invalid := range invalids {
		if wssCompactedEslintStylishDiagnostic([]byte(invalid)) {
			t.Fatalf("invalid compacted ESLint stylish diagnostics accepted: %q", invalid)
		}
	}
}

func wssEslintStylishEnvelope(chunkID, scriptHeader string, repeats int, fixable bool) string {
	var out strings.Builder
	out.WriteString("Chunk ID: ")
	out.WriteString(chunkID)
	out.WriteString("\nWall time: 0.0010 seconds\n")
	out.WriteString("Process exited with code 1\n")
	out.WriteString("Original token count: 10000\n")
	out.WriteString("Output:\n")
	if scriptHeader != "" {
		for i := 0; i < repeats; i++ {
			fmt.Fprintf(&out, "eslint stylish prelude %03d\n", i)
		}
	}
	out.WriteString(scriptHeader)
	for i := 0; i < repeats; i++ {
		out.WriteString("\nsrc/app.js\n")
		out.WriteString("  2:1   warning  Unexpected console statement         no-console\n")
		out.WriteString("  2:20  error    Expected '===' and instead saw '=='  eqeqeq\n")
	}
	fmt.Fprintf(&out, "\n✖ %d problems (%d errors, %d warnings)\n", repeats*2, repeats, repeats)
	if fixable {
		out.WriteString("  1 error and 0 warnings potentially fixable with the `--fix` option.\n")
	}
	return out.String()
}

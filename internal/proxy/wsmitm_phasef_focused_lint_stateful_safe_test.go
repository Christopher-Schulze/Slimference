package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeFocusedLintDiagnosticsCompactFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	payload.WriteString("Chunk ID: focused-lint-diagnostics\n")
	payload.WriteString("Wall time: 0.0010 seconds\n")
	payload.WriteString("Process exited with code 1\n")
	payload.WriteString("Original token count: 10000\n")
	payload.WriteString("Output:\n")
	for range 90 {
		fmt.Fprintln(&payload, "internal/proxy/handler.go:164:15: Close() error return value is not checked")
	}

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-focused-lint", "call_focused_lint", "errcheck ./...", payload.String(), "stateful-focused-lint-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle focused lint diagnostics request: %v", err)
	}
	if !replace {
		t.Fatal("full-history focused lint diagnostics should compact")
	}
	body := string(env.Body)
	for _, want := range []string{
		"[errcheck] FAILED (90 diagnostics)",
		"(repeated 90 times)",
		"[context-archive kind=tool-output uri=local-archive://",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("focused lint diagnostics missing %q in body=%s", want, body)
		}
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe focused lint diagnostics should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeGolangciLintDiagnosticsCompactFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	payload.WriteString("Chunk ID: golangci-lint-diagnostics\n")
	payload.WriteString("Wall time: 0.0010 seconds\n")
	payload.WriteString("Process exited with code 1\n")
	payload.WriteString("Original token count: 10000\n")
	payload.WriteString("Output:\n")
	for range 90 {
		fmt.Fprintln(&payload, "internal/app/app.go:10:2: unused-parameter: parameter ctx seems to be unused, consider removing or renaming it as _ (revive)")
	}

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-golangci-lint", "call_golangci_lint", "golangci-lint run ./...", payload.String(), "stateful-golangci-lint-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle golangci-lint diagnostics request: %v", err)
	}
	if !replace {
		t.Fatal("full-history golangci-lint diagnostics should compact")
	}
	body := string(env.Body)
	for _, want := range []string{
		"[golangci-lint] FAILED (90 diagnostics)",
		"(repeated 90 times)",
		"[context-archive kind=tool-output uri=local-archive://",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("golangci-lint diagnostics missing %q in body=%s", want, body)
		}
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe golangci-lint diagnostics should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeStaticcheckDiagnosticsCompactFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	payload.WriteString("Chunk ID: staticcheck-diagnostics\n")
	payload.WriteString("Wall time: 0.0010 seconds\n")
	payload.WriteString("Process exited with code 1\n")
	payload.WriteString("Original token count: 10000\n")
	payload.WriteString("Output:\n")
	for range 90 {
		fmt.Fprintln(&payload, "internal/app/app.go:22:7: this value of err is never used (SA4006)")
	}

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-staticcheck", "call_staticcheck", "staticcheck ./...", payload.String(), "stateful-staticcheck-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle staticcheck diagnostics request: %v", err)
	}
	if !replace {
		t.Fatal("full-history staticcheck diagnostics should compact")
	}
	body := string(env.Body)
	for _, want := range []string{
		"[staticcheck] FAILED (90 diagnostics)",
		"(repeated 90 times)",
		"[context-archive kind=tool-output uri=local-archive://",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("staticcheck diagnostics missing %q in body=%s", want, body)
		}
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe staticcheck diagnostics should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeReviveDiagnosticsCompactFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var payload strings.Builder
	payload.WriteString("Chunk ID: revive-diagnostics\n")
	payload.WriteString("Wall time: 0.0010 seconds\n")
	payload.WriteString("Process exited with code 1\n")
	payload.WriteString("Original token count: 10000\n")
	payload.WriteString("Output:\n")
	for range 90 {
		fmt.Fprintln(&payload, "internal/app/app.go:10:2: unused-parameter: parameter ctx seems to be unused, consider removing or renaming it as _")
	}

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-revive", "call_revive", "revive ./...", payload.String(), "stateful-revive-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle revive diagnostics request: %v", err)
	}
	if !replace {
		t.Fatal("full-history revive diagnostics should compact")
	}
	body := string(env.Body)
	for _, want := range []string{
		"[revive] FAILED (90 diagnostics)",
		"(repeated 90 times)",
		"[context-archive kind=tool-output uri=local-archive://",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("revive diagnostics missing %q in body=%s", want, body)
		}
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe revive diagnostics should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulUnsafeFocusedLintDiagnosticsStayGuarded(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	output := strings.Join([]string{
		"Chunk ID: focused-lint-unsafe",
		"Wall time: 0.0010 seconds",
		"Process exited with code 1",
		"Original token count: 10000",
		"Output:",
		"internal/proxy/handler.go:164:15: Close() error return value is not checked",
		"if err != nil {",
		"",
	}, "\n")
	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-focused-lint-unsafe", "call_focused_lint_unsafe", "errcheck ./...", output, "stateful-focused-lint-unsafe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle unsafe focused lint diagnostics request: %v", err)
	}
	body := string(env.Body)
	if replace ||
		strings.Contains(body, "[errcheck] FAILED (") ||
		strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "if err != nil {") {
		t.Fatalf("unsafe focused lint diagnostics should stay byte-identical: replace=%v body=%s", replace, body)
	}
}

func TestWSSCompactedFocusedLintDiagnosticClassifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "golangci-lint failure",
			in:   "[golangci-lint] FAILED (2 diagnostics)\ninternal/app/app.go:10:2: unused-parameter: bad (revive)\n",
			want: true,
		},
		{
			name: "staticcheck failure",
			in:   "[staticcheck] FAILED (2 diagnostics)\ninternal/app/app.go:22:7: this value of err is never used (SA4006)\n",
			want: true,
		},
		{
			name: "revive failure",
			in:   "[revive] FAILED (2 diagnostics)\ninternal/app/app.go:10:2: unused-parameter: bad\n",
			want: true,
		},
		{
			name: "errcheck failure",
			in:   "[errcheck] FAILED (2 diagnostics)\ninternal/app/app.go:10:2: unchecked error\n",
			want: true,
		},
		{
			name: "misspell failure",
			in:   "[misspell] FAILED (2 diagnostics)\ndocs/readme.md:9:22 found typo\n",
			want: true,
		},
		{
			name: "wrong label",
			in:   "[customlint] FAILED (2 diagnostics)\nfile.go:1:1: bad\n",
			want: false,
		},
		{
			name: "ok status",
			in:   "[errcheck] ok\n",
			want: false,
		},
		{
			name: "missing bracket",
			in:   "errcheck] FAILED (2 diagnostics)\nfile.go:1:1: bad\n",
			want: false,
		},
		{
			name: "missing diagnostic word",
			in:   "[errcheck] FAILED (2 issues)\nfile.go:1:1: bad\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wssCompactedFocusedLintDiagnostic([]byte(tt.in)); got != tt.want {
				t.Fatalf("wssCompactedFocusedLintDiagnostic=%v want %v for %q", got, tt.want, tt.in)
			}
		})
	}
}

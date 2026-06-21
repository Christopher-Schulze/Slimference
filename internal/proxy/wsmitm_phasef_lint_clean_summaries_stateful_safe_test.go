package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeLintCleanSummariesCompactFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		output    string
		want      string
		forbidden string
	}{
		{
			name:      "ruff package script",
			command:   "pnpm run lint",
			output:    lintCleanSummaryEnvelope("ruff", "> ruff check .\nAll checks passed!\n"),
			want:      "[ruff check] ok",
			forbidden: "lint prelude 119",
		},
		{
			name:      "biome package script",
			command:   "bun run lint",
			output:    lintCleanSummaryEnvelope("biome", "$ biome check .\nChecked 196 files in 24ms. No fixes applied.\n"),
			want:      "[biome check] ok (196 files checked)",
			forbidden: "lint prelude 119",
		},
		{
			name:      "pre-commit run",
			command:   "pre-commit run --all-files",
			output:    preCommitCleanSummaryEnvelope(90),
			want:      "[pre-commit] ok (90 hooks passed)",
			forbidden: "Hook 089",
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name, "call_"+strings.ReplaceAll(tt.name, " ", "_"), tt.command, tt.output, "stateful-lint-clean-summary-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle lint clean-summary request: %v", err)
			}
			if !replace {
				t.Fatal("full-history lint clean-summary output should compact")
			}
			body := string(env.Body)
			if !strings.Contains(body, tt.want) ||
				!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				strings.Contains(body, tt.forbidden) {
				t.Fatalf("lint clean-summary output was not archive-backed compacted: %s", body)
			}
			summary := p.DebugRecorder().Last(1, false)[0]
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("stateful-safe lint clean-summary should save without structured guard: %+v", summary)
			}
		})
	}
}

func TestWSSStatefulUnsafeLintCleanSummarySignalsDoNotBecomeOK(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		output      string
		forbidden   string
		mustContain string
	}{
		{
			name:        "ruff warning after clean line",
			command:     "pnpm run lint",
			output:      lintCleanSummaryEnvelope("ruff", "> ruff check .\nAll checks passed!\nwarning: no files included\n"),
			forbidden:   "[ruff check] ok",
			mustContain: "warning: no files included",
		},
		{
			name:        "biome warning line",
			command:     "bun run lint",
			output:      lintCleanSummaryEnvelope("biome", "$ biome check .\nChecked 2 files in 245ms. No fixes applied.\nFound 1 warning.\n"),
			forbidden:   "[biome check] ok",
			mustContain: "Found 1 warning",
		},
		{
			name:        "biome zero files",
			command:     "biome check .",
			output:      lintCleanSummaryEnvelope("biome", "> biome check .\nChecked 0 files in 65ms. No fixes applied.\n"),
			forbidden:   "[biome check] ok",
			mustContain: "Checked 0 files",
		},
		{
			name:        "pre-commit failed hook",
			command:     "pre-commit run --all-files",
			output:      preCommitUnsafeSummaryEnvelope("Check Yaml...............................................................Failed\n- hook id: check-yaml\n- exit code: 1\n"),
			forbidden:   "[pre-commit] ok",
			mustContain: "Check Yaml",
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name, "call_"+strings.ReplaceAll(tt.name, " ", "_"), tt.command, tt.output, "stateful-lint-unsafe-summary-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe lint clean-summary request: %v", err)
			}
			body := string(env.Body)
			archiveBacked := strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://")
			if strings.Contains(body, tt.forbidden) || (!strings.Contains(body, tt.mustContain) && !archiveBacked) {
				t.Fatalf("unsafe lint clean-summary output was incorrectly compacted: replace=%v body=%s", replace, body)
			}
			if replace && !archiveBacked {
				t.Fatalf("unsafe replacement must stay recovery-backed, body=%s", body)
			}
		})
	}
}

func lintCleanSummaryEnvelope(name, tail string) string {
	var output strings.Builder
	output.WriteString("Chunk ID: lint-clean-summary-")
	output.WriteString(name)
	output.WriteString("\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 0\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	for i := range 120 {
		fmt.Fprintf(&output, "> workspace lint prelude %03d\n", i)
	}
	output.WriteString("> web@1.0.0 lint /repo\n")
	output.WriteString(tail)
	return output.String()
}

func preCommitCleanSummaryEnvelope(hooks int) string {
	var output strings.Builder
	output.WriteString("Chunk ID: lint-clean-summary-precommit\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 0\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	output.WriteString("[INFO] Installing environment for https://github.com/psf/black.\n")
	output.WriteString("[INFO] Initializing environment for https://github.com/PyCQA/isort.\n")
	output.WriteString("[INFO] Once installed this environment will be reused.\n")
	output.WriteString("[INFO] This may take a few minutes...\n")
	for i := range hooks {
		fmt.Fprintf(&output, "Hook %03d.................................................................Passed\n", i)
	}
	return output.String()
}

func preCommitUnsafeSummaryEnvelope(tail string) string {
	var output strings.Builder
	output.WriteString("Chunk ID: lint-clean-summary-precommit-unsafe\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 1\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	output.WriteString("[INFO] Installing environment for https://github.com/pre-commit/mirrors-isort.\n")
	output.WriteString("[INFO] Once installed this environment will be reused.\n")
	output.WriteString("Trim Trailing Whitespace.................................................Passed\n")
	output.WriteString(tail)
	return output.String()
}

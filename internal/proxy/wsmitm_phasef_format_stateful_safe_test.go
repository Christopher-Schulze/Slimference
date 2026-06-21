package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafePrettierCleanCheckPredicate(t *testing.T) {
	clean := "Checking formatting...\nAll matched files use Prettier code style!\n"
	if !wssSafeStatefulStatusCommandOutput("prettier --check .", clean) {
		t.Fatal("exact prettier clean-check transcript should be stateful-safe")
	}
	issue := "Checking formatting...\n[warn] src/app.ts\n[warn] Code style issues found in the above file. Run Prettier with --write to fix.\n"
	if wssSafeStatefulStatusCommandOutput("prettier --check .", issue) {
		t.Fatal("prettier issue transcript must not be stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("prettier --write .", clean) {
		t.Fatal("prettier write mode must not inherit clean-check stateful-safe status")
	}
}

func TestWSSStatefulSafePackageManagerPrettierCleanCheckCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var output strings.Builder
	output.WriteString("Chunk ID: package-script-prettier-clean-check\n")
	output.WriteString("Wall time: 0.0010 seconds\n")
	output.WriteString("Process exited with code 0\n")
	output.WriteString("Original token count: 10000\n")
	output.WriteString("Output:\n")
	for i := range 80 {
		fmt.Fprintf(&output, "> workspace format prelude %03d\n", i)
	}
	output.WriteString("> web@1.0.0 format:check /repo\n")
	output.WriteString("> prettier --check .\n")
	output.WriteString("Checking formatting...\n")
	output.WriteString("All matched files use Prettier code style!\n")

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-prettier-clean-check", "call_package_prettier_clean_check", "pnpm run format:check", output.String(), "stateful-package-prettier-clean-check-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle package-manager prettier clean-check request: %v", err)
	}
	if !replace {
		t.Fatal("full-history package-manager prettier clean-check output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[prettier] ok") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "workspace format prelude 079") ||
		strings.Contains(body, "All matched files use Prettier code style!") {
		t.Fatalf("package-manager prettier clean-check output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe package-manager prettier clean-check should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulUnsafePrettierIssueOutputStaysGuarded(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	envelope := "Chunk ID: prettier-issue-check\nWall time: 0.0010 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" +
		"Checking formatting...\n[warn] src/app.ts\n[warn] Code style issues found in the above file. Run Prettier with --write to fix.\n"
	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-prettier-issue-check", "call_prettier_issue_check", "prettier --check .", envelope, "stateful-prettier-issue-check-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle prettier issue request: %v", err)
	}
	body := string(env.Body)
	if replace ||
		strings.Contains(body, "[prettier] ok") ||
		strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "Code style issues found") {
		t.Fatalf("unsafe prettier issue output should stay byte-identical: replace=%v body=%s", replace, body)
	}
}

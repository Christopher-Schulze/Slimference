package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutputReduceABReportPassesPositiveNetPair(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath,
		outputReduceABRow("ab-1-baseline", "ab-1", "baseline", &codexCaptureLiveDelta{
			ProviderOutputTokens:    1000,
			HostBudgetStatus:        "ok",
			HostBudgetCompressionOK: true,
			HostBudgetDegradationOK: true,
		}),
		outputReduceABRow("ab-1-directive", "ab-1", "directive", &codexCaptureLiveDelta{
			OutputReduceInjected:            1,
			OutputReduceInputOverheadTokens: 80,
			ProviderOutputTokens:            700,
			HostBudgetStatus:                "ok",
			HostBudgetCompressionOK:         true,
			HostBudgetDegradationOK:         true,
		}),
	)

	report, err := loadOutputReduceABReport(outputReduceABFlags{path: matrixPath, minNetTokens: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !report.GatePassed || report.PairCount != 1 {
		t.Fatalf("expected passing pair: %+v", report)
	}
	pair := report.Pairs[0]
	if pair.OutputTokensSaved != 300 || pair.NetTokensSaved != 220 || pair.OutputSavingsPct != 30 {
		t.Fatalf("bad pair economics: %+v", pair)
	}
}

func TestOutputReduceABReportFailsWithoutPairs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath, wssProofMatrixRecord{
		ID:            "ordinary",
		Client:        "cli",
		WorkloadClass: "repeat_full_read",
		LiveDelta:     &codexCaptureLiveDelta{BillableInputTokensSaved: 1},
	})

	report, err := loadOutputReduceABReport(outputReduceABFlags{path: matrixPath, minNetTokens: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.GatePassed || !strings.Contains(strings.Join(report.GateFailures, "\n"), "no output-reduce A/B pairs found") {
		t.Fatalf("expected no-pair failure: %+v", report)
	}
}

func TestOutputReduceABReportFailsUnsafeOrNegativePairs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath,
		outputReduceABRow("bad-baseline", "bad", "baseline", &codexCaptureLiveDelta{
			OutputReduceInjected:             1,
			OutputReduceOutputTokensObserved: 100,
			HostBudgetStatus:                 "ok",
			HostBudgetCompressionOK:          true,
			HostBudgetDegradationOK:          true,
		}),
		outputReduceABRow("bad-directive", "bad", "directive", &codexCaptureLiveDelta{
			OutputReduceInjected:             1,
			OutputReduceInputOverheadTokens:  20,
			OutputReduceOutputTokensObserved: 110,
			OutputReduceDowngrades:           1,
			HostBudgetStatus:                 "attention",
			HostBudgetExceeded:               true,
			HostBudgetCompressionOK:          true,
			HostBudgetDegradationOK:          true,
		}),
	)

	report, err := loadOutputReduceABReport(outputReduceABFlags{path: matrixPath, minNetTokens: 1})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(report.GateFailures, "\n")
	for _, want := range []string{
		"baseline unexpectedly has output_reduce_injected",
		"directive has output-reduce downgrade/repair signal",
		"directive host budget not ok",
		"output_tokens_saved=-10 <= 0",
		"net_tokens_saved=-30 < min=1",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in failures:\n%s", want, joined)
		}
	}
}

func TestRunOutputReduceABReportJSONExitCodes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	writeJSONLFile(t, matrixPath,
		outputReduceABRow("ab-baseline", "ab", "baseline", &codexCaptureLiveDelta{OutputReduceOutputTokensObserved: 500}),
		outputReduceABRow("ab-directive", "ab", "directive", &codexCaptureLiveDelta{
			OutputReduceInjected:             1,
			OutputReduceInputOverheadTokens:  10,
			OutputReduceOutputTokensObserved: 300,
		}),
	)
	var stdout, stderr bytes.Buffer
	code := runOutputReduceABReport([]string{matrixPath, "--json", "--min-net-tokens=50", "--min-output-savings-pct=10"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run failed code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"net_tokens_saved": 190`) {
		t.Fatalf("missing JSON economics: %s", stdout.String())
	}
}

func outputReduceABRow(id, pairID, variant string, live *codexCaptureLiveDelta) wssProofMatrixRecord {
	return wssProofMatrixRecord{
		ID:            id,
		Client:        "cli",
		WorkloadClass: "output_reduce_aggressive",
		ABPairID:      pairID,
		ABVariant:     variant,
		LiveDelta:     live,
	}
}

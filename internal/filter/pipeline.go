package filter

import (
	"context"
	"log/slog"

	"github.com/slimference/slimference/internal/compression"
)

// PipelineResult is stdout/stderr after Layer-0 processing plus token estimates for analytics.
type PipelineResult struct {
	Stdout []byte
	Stderr []byte
	Code   int
	Err    error // only start failures; exit codes are in Code

	// RawStdout/RawStderr are copies before ANSI strip (for tee recovery on failure).
	RawStdout []byte
	RawStderr []byte

	InputTokens  int
	OutputTokens int
	SavingsPct   float64
}

// RunPipeline executes the subprocess, strips ANSI from stdout/stderr, applies
// Layer-0 compaction to both streams, and computes rough savings.
// passthroughMaxRunes caps final stdout (0 = unlimited); applied after built-in/TOML (spec+.md §4.6).
func RunPipeline(ctx context.Context, workDir string, argv []string, passthroughMaxRunes int) PipelineResult {
	argv0 := ""
	if len(argv) > 0 {
		argv0 = argv[0]
	}

	out, errOut, code, runErr := RunCommand(ctx, workDir, argv)
	slog.Debug("layer0 exec", "argv0", argv0, "exit_code", code, "stdout_bytes", len(out), "stderr_bytes", len(errOut))

	rawOut := append([]byte(nil), out...)
	rawErr := append([]byte(nil), errOut...)
	pr := PipelineResult{
		Stdout:    rawOut,
		Stderr:    rawErr,
		RawStdout: rawOut,
		RawStderr: rawErr,
		Code:      code,
		Err:       runErr,
	}
	if runErr != nil {
		slog.Debug("layer0 exec failed", "argv0", argv0, "error", runErr)
		return pr
	}

	stripped := compression.StripANSICodes(string(out))
	pr.Stdout = applyLayer0AfterANSI(workDir, argv, []byte(stripped))
	pr.Stdout = TruncateStdoutWithHint(pr.Stdout, passthroughMaxRunes)
	strippedErr := compression.StripANSICodes(string(errOut))
	pr.Stderr = applyLayer0AfterANSI(workDir, argv, []byte(strippedErr))
	pr.Stderr = TruncateStdoutWithHint(pr.Stderr, passthroughMaxRunes)

	pr.InputTokens = estimateTokensFromByteSlices(out, errOut)
	pr.OutputTokens = estimateTokensFromByteSlices(pr.Stdout, pr.Stderr)
	if pr.InputTokens > 0 {
		pr.SavingsPct = float64(pr.InputTokens-pr.OutputTokens) / float64(pr.InputTokens) * 100.0
	}
	slog.Debug("layer0 result", "argv0", argv0, "in_tokens", pr.InputTokens, "out_tokens", pr.OutputTokens, "savings_pct", pr.SavingsPct)
	return pr
}

// applyLayer0AfterANSI applies built-in filters first, then TOML if no built-in handled
// the output (spec+.md §4.6: built-in > TOML > generic cleanup).
// Logs which filter matched (or passthrough) at debug level.
func applyLayer0AfterANSI(workDir string, argv []string, stdout []byte) []byte {
	out, filterName := applyLayer0FiltersWithContext(workDir, argv, stdout, FileReadContext{Mode: "scan"})
	if filterName != "" {
		slog.Debug("layer0 filter applied", "filter", filterName, "in_bytes", len(stdout), "out_bytes", len(out))
	} else {
		slog.Debug("layer0 passthrough", "in_bytes", len(stdout))
	}
	return out
}

func applyLayer0AfterANSIWithContext(workDir string, argv []string, stdout []byte, ctx FileReadContext) []byte {
	out, filterName := applyLayer0FiltersWithContext(workDir, argv, stdout, ctx)
	if filterName != "" {
		slog.Debug("layer0 filter applied", "filter", filterName, "in_bytes", len(stdout), "out_bytes", len(out))
	} else {
		slog.Debug("layer0 passthrough", "in_bytes", len(stdout))
	}
	return out
}

// applyLayer0Filters dispatches stdout through each built-in filter in priority order
// and returns the transformed output plus the name of the matched filter (empty = no match).
func applyLayer0Filters(workDir string, argv []string, stdout []byte) ([]byte, string) {
	return applyLayer0FiltersWithContext(workDir, argv, stdout, FileReadContext{Mode: "scan"})
}

func applyLayer0FiltersWithContext(workDir string, argv []string, stdout []byte, ctx FileReadContext) ([]byte, string) {
	if productDefaultFileReadMustFullPass(argv) {
		return stdout, ""
	}

	type filterEntry struct {
		name string
		fn   func() ([]byte, bool)
	}
	filters := []filterEntry{
		// Tier-1: strict structured JSON parsers. Parse the wire
		// schema directly; refuse to match on anything else. Beats
		// every regex-based Tier-2 below on accuracy and savings, so
		// they sit at the top. RTK's catalog has no Tier-1 — this is
		// where we surpass it.
		{"tier1_sarif", func() ([]byte, bool) { return TryCompactSARIF(argv, stdout) }},
		{"tier1_go_test_json", func() ([]byte, bool) { return TryCompactGoTestJSON(argv, stdout) }},
		{"tier1_vitest_jest_json", func() ([]byte, bool) { return TryCompactVitestJSON(argv, stdout) }},
		{"tier1_pytest_json", func() ([]byte, bool) { return TryCompactPytestJSON(argv, stdout) }},
		{"tier1_cargo_test_json", func() ([]byte, bool) { return TryCompactCargoTestJSON(argv, stdout) }},
		{"tier1_eslint_json", func() ([]byte, bool) { return TryCompactEslintJSON(argv, stdout) }},
		{"tier1_tsc_diagnostics", func() ([]byte, bool) { return TryCompactTscDiagnostics(argv, stdout) }},
		{"tier1_kubectl_json", func() ([]byte, bool) { return TryCompactKubectlJSON(argv, stdout) }},
		{"tier1_cargo_metadata_json", func() ([]byte, bool) { return TryCompactCargoMetadataJSON(argv, stdout) }},
		{"tier1_terraform_show_json", func() ([]byte, bool) { return TryCompactTerraformShowJSON(argv, stdout) }},
		// Tier-2: hand-written Go compactors (regex/heuristic-based).
		{"git_status", func() ([]byte, bool) { return TryCompactGitStatus(argv, stdout) }},
		{"git_diff", func() ([]byte, bool) { return TryCompactGitDiff(argv, stdout) }},
		{"git_log", func() ([]byte, bool) { return TryCompactGitLog(argv, stdout) }},
		{"git_show", func() ([]byte, bool) { return TryCompactGitShow(argv, stdout) }},
		{"git_f05", func() ([]byte, bool) { return TryCompactGitF05(argv, stdout) }},
		{"build_output", func() ([]byte, bool) { return TryCompactBuildOutput(argv, stdout) }},
		{"test_output", func() ([]byte, bool) { return TryCompactTestOutput(argv, stdout) }},
		{"dotnet", func() ([]byte, bool) { return TryCompactDotnet(argv, stdout) }},
		{"ruby_output", func() ([]byte, bool) { return TryCompactRubyOutput(argv, stdout) }},
		{"search_output", func() ([]byte, bool) { return TryCompactSearchOutput(argv, stdout) }},
		{"ls", func() ([]byte, bool) { return TryCompactLs(argv, stdout) }},
		{"tree", func() ([]byte, bool) { return TryCompactTree(argv, stdout) }},
		{"lint_output", func() ([]byte, bool) { return TryCompactLintOutput(argv, stdout) }},
		{"log_output", func() ([]byte, bool) { return TryCompactLogOutput(argv, stdout) }},
		{"format_output", func() ([]byte, bool) { return TryCompactFormatOutput(argv, stdout) }},
		{"psql", func() ([]byte, bool) { return TryCompactPsql(argv, stdout) }},
		{"package_output", func() ([]byte, bool) { return TryCompactPackageOutput(argv, stdout) }},
		{"container_output", func() ([]byte, bool) { return TryCompactContainerOutput(argv, stdout) }},
		{"gh_list", func() ([]byte, bool) { return TryCompactGhList(argv, stdout) }},
		{"glab_list", func() ([]byte, bool) { return TryCompactGlabList(argv, stdout) }},
		{"aws_json", func() ([]byte, bool) { return TryCompactAwsJSON(argv, stdout) }},
		{"python_traceback", func() ([]byte, bool) { return TryCompactPythonTraceback(stdout) }},
		{"terraform_plan", func() ([]byte, bool) { return TryCompactTerraformPlan(argv, stdout) }},
		{"terraform_init", func() ([]byte, bool) { return TryCompactTerraformInit(argv, stdout) }},
		{"terraform_validate", func() ([]byte, bool) { return TryCompactTerraformValidate(argv, stdout) }},
		{"terraform_state_list", func() ([]byte, bool) { return TryCompactTerraformStateList(argv, stdout) }},
		{"terraform_output", func() ([]byte, bool) { return TryCompactTerraformOutput(argv, stdout) }},
		{"terraform_show", func() ([]byte, bool) { return TryCompactTerraformShow(argv, stdout) }},
		{"json_minify", func() ([]byte, bool) { return TryCompactJSONMinify(stdout) }},
	}

	inBytes := len(stdout)
	for _, f := range filters {
		out, ok, stats := runFilter(f.name, f.fn)
		stats.InBytes = inBytes
		stats.OutBytes = inBytes
		if ok {
			stats.OutBytes = len(out)
			globalObservability.Record(stats)
			return out, f.name
		}
		globalObservability.Record(stats)
	}

	if rule := FirstMatchingTOMLRule(workDir, argv); rule != nil {
		out := ApplyTOMLRule(stdout, rule)
		globalObservability.Record(FilterStats{
			Name:     "toml_rule",
			Matched:  true,
			InBytes:  inBytes,
			OutBytes: len(out),
		})
		return out, "toml_rule"
	}
	// Embedded RTK-derived filter catalog (MIT, see NOTICE.md). Loaded
	// once via //go:embed. Sits BELOW the user/project TOML so explicit
	// user overrides always win, and BELOW the Go built-ins so curated
	// hand-written compactors (git-status etc.) win over generic
	// catalog filters.
	if name, rule := FirstMatchingBuiltinTOMLRule(argv); rule != nil {
		out := ApplyTOMLRule(stdout, rule)
		globalObservability.Record(FilterStats{
			Name:     "builtin_toml:" + name,
			Matched:  true,
			InBytes:  inBytes,
			OutBytes: len(out),
		})
		return out, "builtin_toml:" + name
	}
	return stdout, ""
}

func productDefaultFileReadMustFullPass(argv []string) bool {
	_, ok := readRequestFromArgv(argv)
	return ok
}

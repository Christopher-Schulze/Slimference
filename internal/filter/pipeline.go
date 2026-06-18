package filter

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/compression"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

// Layer0ReducerSafetyClass describes how much information a default reducer is
// allowed to remove from a tool output.
type Layer0ReducerSafetyClass string

const (
	Layer0ReducerSafetyExact              Layer0ReducerSafetyClass = "exact"
	Layer0ReducerSafetyStructuredEvidence Layer0ReducerSafetyClass = "structured_evidence"
	Layer0ReducerSafetyDiagnosticPriority Layer0ReducerSafetyClass = "diagnostic_priority"
	Layer0ReducerSafetyEmptyEvidence      Layer0ReducerSafetyClass = "empty_evidence"
)

// Layer0ReducerInfo is metadata only. It is safe for admin/TUI/reporting paths:
// no raw command output, no function pointers, no model-facing text.
type Layer0ReducerInfo struct {
	ID                string
	Family            string
	SafetyClass       Layer0ReducerSafetyClass
	DefaultEligible   bool
	RequiredFields    []string
	PreservedEvidence []string
	RecoveryPath      string
}

type layer0ReducerFunc func(argv []string, stdout []byte, ctx FileReadContext) ([]byte, bool)

type layer0ReducerSpec struct {
	Layer0ReducerInfo
	fn layer0ReducerFunc
}

// Layer0ReducerRegistry returns the product reducer contract in dispatch order.
func Layer0ReducerRegistry() []Layer0ReducerInfo {
	specs := layer0ReducerSpecs()
	out := make([]Layer0ReducerInfo, 0, len(specs))
	for _, spec := range specs {
		info := spec.Layer0ReducerInfo
		info.RequiredFields = append([]string(nil), info.RequiredFields...)
		info.PreservedEvidence = append([]string(nil), info.PreservedEvidence...)
		out = append(out, info)
	}
	return out
}

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
// passthroughMaxRunes caps final stdout (0 = unlimited); applied after built-in/TOML (docs/spec.md §4.6).
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
	strippedErr := compression.StripANSICodes(string(errOut))
	stdoutEmpty := strings.TrimSpace(stripped) == ""
	stderrEmpty := strings.TrimSpace(strippedErr) == ""
	if stdoutEmpty && (code != 0 || !stderrEmpty) {
		pr.Stdout = []byte(stripped)
	} else {
		pr.Stdout = applyLayer0AfterANSI(workDir, argv, []byte(stripped))
	}
	pr.Stdout = TruncateStdoutWithHint(pr.Stdout, passthroughMaxRunes)
	if stderrEmpty {
		pr.Stderr = []byte(strippedErr)
	} else {
		pr.Stderr = applyLayer0AfterANSI(workDir, argv, []byte(strippedErr))
	}
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
// the output (docs/spec.md §4.6: built-in > TOML > generic cleanup).
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

	inBytes := len(stdout)
	analysis := evidence.Analyze(argv, stdout)
	for _, reducer := range layer0ReducerSpecs() {
		out, ok, stats := runFilter(reducer.ID, func() ([]byte, bool) {
			return reducer.fn(argv, stdout, ctx)
		})
		stats.InBytes = inBytes
		stats.OutBytes = inBytes
		stats.ContentClass = string(analysis.ContentClass)
		stats.SafetyClass = string(layer0EvidenceSafety(reducer.SafetyClass))
		stats.Signals = evidenceSignalStrings(analysis.Signals)
		stats.PreservedEvidence = append([]string(nil), reducer.PreservedEvidence...)
		stats.Action = string(evidence.ActionSkipped)
		stats.Reason = "not_applicable"
		if stats.Panicked {
			stats.Action = string(evidence.ActionFailedOpen)
			stats.Reason = "panic_fail_open_original"
		}
		if ok {
			stats.OutBytes = len(out)
			stats.Action = string(evidence.ActionApplied)
			stats.Reason = "matched"
			if len(out) >= inBytes {
				stats.Reason = "matched_no_positive_byte_savings"
			}
			globalObservability.Record(stats)
			return out, reducer.ID
		}
		globalObservability.Record(stats)
	}

	if rule := FirstMatchingTOMLRule(workDir, argv); rule != nil {
		out := ApplyTOMLRule(stdout, rule)
		globalObservability.Record(FilterStats{
			Name:         "toml_rule",
			Matched:      true,
			InBytes:      inBytes,
			OutBytes:     len(out),
			ContentClass: string(analysis.ContentClass),
			SafetyClass:  string(evidence.SafetyStructuredEvidence),
			Action:       string(evidence.ActionApplied),
			Reason:       "matched_project_toml_rule",
			Signals:      evidenceSignalStrings(analysis.Signals),
		})
		return out, "toml_rule"
	}
	// Embedded TOML filter catalog. Loaded once via //go:embed.
	// Sits BELOW the user/project TOML so explicit
	// user overrides always win, and BELOW the Go built-ins so curated
	// hand-written compactors (git-status etc.) win over generic
	// catalog filters.
	if name, rule := FirstMatchingBuiltinTOMLRule(argv); rule != nil {
		out := ApplyBuiltinTOMLRule(stdout, rule)
		globalObservability.Record(FilterStats{
			Name:         "builtin_toml:" + name,
			Matched:      true,
			InBytes:      inBytes,
			OutBytes:     len(out),
			ContentClass: string(analysis.ContentClass),
			SafetyClass:  string(evidence.SafetyStructuredEvidence),
			Action:       string(evidence.ActionApplied),
			Reason:       "matched_builtin_toml_rule",
			Signals:      evidenceSignalStrings(analysis.Signals),
		})
		return out, "builtin_toml:" + name
	}
	return stdout, ""
}

func layer0EvidenceSafety(class Layer0ReducerSafetyClass) evidence.SafetyClass {
	switch class {
	case Layer0ReducerSafetyExact:
		return evidence.SafetyExact
	case Layer0ReducerSafetyStructuredEvidence:
		return evidence.SafetyStructuredEvidence
	case Layer0ReducerSafetyDiagnosticPriority:
		return evidence.SafetyDiagnosticPriority
	case Layer0ReducerSafetyEmptyEvidence:
		return evidence.SafetyStructuredEvidence
	default:
		return evidence.SafetyUnknown
	}
}

func evidenceSignalStrings(signals []evidence.Signal) []string {
	if len(signals) == 0 {
		return nil
	}
	out := make([]string, len(signals))
	for i, signal := range signals {
		out[i] = string(signal)
	}
	return out
}

func productDefaultFileReadMustFullPass(argv []string) bool {
	_, ok := readRequestFromArgv(argv)
	return ok
}

func layer0ReducerSpecs() []layer0ReducerSpec {
	return []layer0ReducerSpec{
		// Tier-1: strict structured parsers. They parse the wire schema directly
		// and refuse to match on anything else, so they run before heuristic reducers.
		structuredReducer("tier1_sarif", "security", []string{"rule id", "severity", "path", "line", "message"}, TryCompactSARIF),
		structuredReducer("tier1_go_test_json", "test", []string{"package", "test name", "fail action", "output line"}, TryCompactGoTestJSON),
		structuredReducer("tier1_vitest_jest_json", "test", []string{"suite", "test name", "failure message", "stack frame"}, TryCompactVitestJSON),
		structuredReducer("tier1_pytest_json", "test", []string{"node id", "outcome", "failure message", "traceback frame"}, TryCompactPytestJSON),
		structuredReducer("tier1_cargo_test_json", "test", []string{"package", "test name", "failure message", "stdout"}, TryCompactCargoTestJSON),
		structuredReducer("tier1_eslint_json", "lint", []string{"rule id", "severity", "path", "line", "column", "message"}, TryCompactEslintJSON),
		structuredReducer("tier1_tsc_diagnostics", "lint", []string{"path", "line", "column", "diagnostic code", "message"}, TryCompactTscDiagnostics),
		structuredReducer("tier1_kubectl_json", "container", []string{"resource kind", "name", "namespace", "status", "reason"}, TryCompactKubectlJSON),
		structuredReducer("tier1_cargo_metadata_json", "package", []string{"package", "target", "dependency edge"}, TryCompactCargoMetadataJSON),
		structuredReducer("tier1_terraform_show_json", "terraform", []string{"resource address", "action", "attribute path", "sensitive marker"}, TryCompactTerraformShowJSON),

		evidenceReducer("git_status", "git", []string{"staged count", "worktree count", "untracked count", "renames", "conflicts"}, TryCompactGitStatus),
		evidenceReducer("git_ls_files", "listing", []string{"file path", "directory grouping", "path count"}, TryCompactGitLsFiles),
		evidenceReducer("git_diff", "git", []string{"file path", "hunk header", "added line", "removed line"}, TryCompactGitDiff),
		evidenceReducer("git_log", "git", []string{"commit hash", "subject", "file count", "insertions", "deletions"}, TryCompactGitLog),
		evidenceReducer("git_show", "git", []string{"commit hash", "subject", "file path", "hunk header", "added line", "removed line"}, TryCompactGitShow),
		evidenceReducer("git_f05", "git", []string{"ref update", "changed count", "success marker", "failure line"}, TryCompactGitF05),
		diagnosticReducer("test_output", "test", []string{"tool", "failed test", "failure line", "summary", "file", "line"}, TryCompactTestOutput),
		diagnosticReducer("build_output", "build", []string{"tool", "exit evidence", "error line", "file", "line", "column"}, TryCompactBuildOutput),
		diagnosticReducer("dotnet", "build", []string{"tool", "error code", "file", "line", "message"}, TryCompactDotnet),
		diagnosticReducer("ruby_output", "test", []string{"tool", "failed example", "file", "line", "message"}, TryCompactRubyOutput),
		evidenceReducer("path_list_output", "listing", []string{"file path", "directory grouping", "path count"}, TryCompactPathListOutput),
		searchOutputReducer("search_output", "search", []string{"file", "line", "match text", "match count", "omitted count"}),
		emptyEvidenceReducer("ls", "listing", []string{"empty marker", "non-empty listings full-pass"}, TryCompactLs),
		emptyEvidenceReducer("tree", "listing", []string{"empty marker", "non-empty hierarchy full-pass"}, TryCompactTree),
		evidenceReducer("wc", "listing", []string{"count values", "requested count units", "file path", "total row"}, TryCompactWc),
		argvReducer("api_json_exact", "network", Layer0ReducerSafetyExact, []string{"all response bytes", "valid JSON fields", "valid JSON scalar values", "array order"}, TryCompactAPIJSONExact),
		argvReducer("network_response_exact", "network", Layer0ReducerSafetyExact, []string{"all response bytes", "valid JSON fields", "valid JSON scalar values", "array order"}, TryCompactNetworkResponse),
		diagnosticReducer("lint_output", "lint", []string{"tool", "rule id", "severity", "file", "line", "message"}, TryCompactLintOutput),
		diagnosticReducer("log_output", "log", []string{"severity", "timestamp", "error line", "count marker"}, TryCompactLogOutput),
		evidenceReducer("format_output", "format", []string{"tool", "changed file", "failure line", "success marker"}, TryCompactFormatOutput),
		evidenceReducer("psql", "database", []string{"row count", "column header", "error line"}, TryCompactPsql),
		evidenceReducer("package_output", "package", []string{"package manager", "changed package count", "error line", "success marker"}, TryCompactPackageOutput),
		evidenceReducer("container_output", "container", []string{"resource name", "status", "reason", "attention row"}, TryCompactContainerOutput),
		evidenceReducer("gh_list", "vcs_host", []string{"item number", "title", "state", "author"}, TryCompactGhList),
		evidenceReducer("glab_list", "vcs_host", []string{"item number", "title", "state", "author"}, TryCompactGlabList),
		evidenceReducer("aws_json", "cloud", []string{"service payload", "resource id", "error field"}, TryCompactAwsJSON),
		stdoutReducer("python_traceback", "runtime", Layer0ReducerSafetyDiagnosticPriority, []string{"exception type", "message", "file", "line", "stack frame"}, TryCompactPythonTraceback),
		evidenceReducer("terraform_plan", "terraform", []string{"resource address", "action", "change summary", "warning", "error"}, TryCompactTerraformPlan),
		evidenceReducer("terraform_init", "terraform", []string{"backend status", "provider status", "warning", "error"}, TryCompactTerraformInit),
		evidenceReducer("terraform_validate", "terraform", []string{"valid marker", "diagnostic severity", "message", "range"}, TryCompactTerraformValidate),
		evidenceReducer("terraform_show", "terraform", []string{"resource address", "attribute", "value summary", "sensitive marker"}, TryCompactTerraformShow),
		stdoutReducer("json_minify", "json", Layer0ReducerSafetyExact, []string{"all JSON fields", "all scalar values", "array order"}, TryCompactJSONMinify),
	}
}

func structuredReducer(id, family string, preserved []string, fn func([]string, []byte) ([]byte, bool)) layer0ReducerSpec {
	return argvReducer(id, family, Layer0ReducerSafetyStructuredEvidence, preserved, fn)
}

func diagnosticReducer(id, family string, preserved []string, fn func([]string, []byte) ([]byte, bool)) layer0ReducerSpec {
	return argvReducer(id, family, Layer0ReducerSafetyDiagnosticPriority, preserved, fn)
}

func evidenceReducer(id, family string, preserved []string, fn func([]string, []byte) ([]byte, bool)) layer0ReducerSpec {
	return argvReducer(id, family, Layer0ReducerSafetyStructuredEvidence, preserved, fn)
}

func emptyEvidenceReducer(id, family string, preserved []string, fn func([]string, []byte) ([]byte, bool)) layer0ReducerSpec {
	return argvReducer(id, family, Layer0ReducerSafetyEmptyEvidence, preserved, fn)
}

func argvReducer(id, family string, safety Layer0ReducerSafetyClass, preserved []string, fn func([]string, []byte) ([]byte, bool)) layer0ReducerSpec {
	return layer0ReducerSpec{
		Layer0ReducerInfo: Layer0ReducerInfo{
			ID:                id,
			Family:            family,
			SafetyClass:       safety,
			DefaultEligible:   true,
			RequiredFields:    append([]string(nil), preserved...),
			PreservedEvidence: append([]string(nil), preserved...),
			RecoveryPath:      "parser fail-open to original output",
		},
		fn: func(argv []string, stdout []byte, _ FileReadContext) ([]byte, bool) {
			return fn(argv, stdout)
		},
	}
}

func searchOutputReducer(id, family string, preserved []string) layer0ReducerSpec {
	return layer0ReducerSpec{
		Layer0ReducerInfo: Layer0ReducerInfo{
			ID:                id,
			Family:            family,
			SafetyClass:       Layer0ReducerSafetyStructuredEvidence,
			DefaultEligible:   true,
			RequiredFields:    append([]string(nil), preserved...),
			PreservedEvidence: append([]string(nil), preserved...),
			RecoveryPath:      "parser fail-open to original output",
		},
		fn: func(argv []string, stdout []byte, ctx FileReadContext) ([]byte, bool) {
			return TryCompactSearchOutputWithOptions(argv, stdout, ctx.SearchCompactOptions)
		},
	}
}

func stdoutReducer(id, family string, safety Layer0ReducerSafetyClass, preserved []string, fn func([]byte) ([]byte, bool)) layer0ReducerSpec {
	return layer0ReducerSpec{
		Layer0ReducerInfo: Layer0ReducerInfo{
			ID:                id,
			Family:            family,
			SafetyClass:       safety,
			DefaultEligible:   true,
			RequiredFields:    append([]string(nil), preserved...),
			PreservedEvidence: append([]string(nil), preserved...),
			RecoveryPath:      "parser fail-open to original output",
		},
		fn: func(_ []string, stdout []byte, _ FileReadContext) ([]byte, bool) {
			return fn(stdout)
		},
	}
}

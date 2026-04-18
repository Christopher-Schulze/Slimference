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

// RunPipeline executes the subprocess, strips ANSI from stdout (stderr unchanged), and computes rough savings.
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

	rawLen := len(out)
	stripped := compression.StripANSICodes(string(out))
	pr.Stdout = applyLayer0AfterANSI(workDir, argv, []byte(stripped))
	pr.Stdout = TruncateStdoutWithHint(pr.Stdout, passthroughMaxRunes)

	inBytes := rawLen + len(errOut)
	outBytes := len(pr.Stdout) + len(errOut)
	pr.InputTokens = EstimateTokensFromBytes(inBytes)
	pr.OutputTokens = EstimateTokensFromBytes(outBytes)
	if inBytes > 0 {
		pr.SavingsPct = float64(inBytes-outBytes) / float64(inBytes) * 100.0
	}
	slog.Debug("layer0 result", "argv0", argv0, "in_tokens", pr.InputTokens, "out_tokens", pr.OutputTokens, "savings_pct", pr.SavingsPct)
	return pr
}

// applyLayer0AfterANSI applies built-in filters first, then TOML if no built-in handled
// the output (spec+.md §4.6: built-in > TOML > generic cleanup).
// Logs which filter matched (or passthrough) at debug level.
func applyLayer0AfterANSI(workDir string, argv []string, stdout []byte) []byte {
	out, filterName := applyLayer0Filters(workDir, argv, stdout)
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
	if out, ok := TryCompactGitStatus(argv, stdout); ok {
		return out, "git_status"
	}
	if out, ok := TryCompactGitDiff(argv, stdout); ok {
		return out, "git_diff"
	}
	if out, ok := TryCompactGitLog(argv, stdout); ok {
		return out, "git_log"
	}
	if out, ok := TryCompactGitShow(argv, stdout); ok {
		return out, "git_show"
	}
	if out, ok := TryCompactGitF05(argv, stdout); ok {
		return out, "git_f05"
	}
	if out, ok := TryCompactBuildOutput(argv, stdout); ok {
		return out, "build_output"
	}
	if out, ok := TryCompactTestOutput(argv, stdout); ok {
		return out, "test_output"
	}
	if out, ok := TryCompactDotnet(argv, stdout); ok {
		return out, "dotnet"
	}
	if out, ok := TryCompactRubyOutput(argv, stdout); ok {
		return out, "ruby_output"
	}
	if out, ok := TryCompactSearchOutput(argv, stdout); ok {
		return out, "search_output"
	}
	if out, ok := TryCompactLs(argv, stdout); ok {
		return out, "ls"
	}
	if out, ok := TryCompactTree(argv, stdout); ok {
		return out, "tree"
	}
	if out, ok := TryStripCommentsFileRead(argv, stdout); ok {
		return out, "strip_comments_file_read"
	}
	if out, ok := TryCompactLintOutput(argv, stdout); ok {
		return out, "lint_output"
	}
	if out, ok := TryCompactFormatOutput(argv, stdout); ok {
		return out, "format_output"
	}
	if out, ok := TryCompactPsql(argv, stdout); ok {
		return out, "psql"
	}
	if out, ok := TryCompactPackageOutput(argv, stdout); ok {
		return out, "package_output"
	}
	if out, ok := TryCompactContainerOutput(argv, stdout); ok {
		return out, "container_output"
	}
	if out, ok := TryCompactGhList(argv, stdout); ok {
		return out, "gh_list"
	}
	if out, ok := TryCompactGlabList(argv, stdout); ok {
		return out, "glab_list"
	}
	if out, ok := TryCompactLogDedup(argv, stdout); ok {
		return out, "log_dedup"
	}
	if out, ok := TryCompactAwsJSON(argv, stdout); ok {
		return out, "aws_json"
	}
	if out, ok := TryCompactPythonTraceback(stdout); ok {
		return out, "python_traceback"
	}
	if out, ok := TryCompactJSONMinify(stdout); ok {
		return out, "json_minify"
	}
	if rule := FirstMatchingTOMLRule(workDir, argv); rule != nil {
		return ApplyTOMLRule(stdout, rule), "toml_rule"
	}
	return stdout, ""
}

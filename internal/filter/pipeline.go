package filter

import (
	"context"

	"github.com/tokenproxy/tokenproxy/internal/compression"
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
	out, errOut, code, runErr := RunCommand(ctx, workDir, argv)
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
	return pr
}

// applyLayer0AfterANSI applies built-in filters first, then TOML if no built-in handled
// the output (spec+.md §4.6: built-in > TOML > generic cleanup).
func applyLayer0AfterANSI(workDir string, argv []string, stdout []byte) []byte {
	if out, ok := TryCompactGitStatus(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactGitDiff(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactGitLog(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactGitShow(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactGitF05(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactBuildOutput(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactTestOutput(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactDotnet(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactRubyOutput(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactSearchOutput(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactLs(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactTree(argv, stdout); ok {
		return out
	}
	if out, ok := TryStripCommentsFileRead(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactLintOutput(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactFormatOutput(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactPsql(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactPackageOutput(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactContainerOutput(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactGhList(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactGlabList(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactLogDedup(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactAwsJSON(argv, stdout); ok {
		return out
	}
	if out, ok := TryCompactJSONMinify(stdout); ok {
		return out
	}
	if rule := FirstMatchingTOMLRule(workDir, argv); rule != nil {
		return ApplyTOMLRule(stdout, rule)
	}
	return stdout
}

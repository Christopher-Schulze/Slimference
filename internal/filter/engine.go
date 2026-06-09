package filter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/Christopher-Schulze/Slimference/internal/tokens"
)

// RunCommand runs argv[0] with argv[1:] in workDir, capturing combined stdout/stderr.
// exitCode is the process exit code; if the process could not start, runErr is non-nil and exitCode is -1.
func RunCommand(ctx context.Context, workDir string, argv []string) (stdout, stderr []byte, exitCode int, runErr error) {
	if len(argv) == 0 {
		return nil, nil, -1, fmt.Errorf("filter: empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	stdout, stderr = outb.Bytes(), errb.Bytes()
	if err == nil {
		return stdout, stderr, 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return stdout, stderr, ee.ExitCode(), nil
	}
	return stdout, stderr, -1, err
}

// EstimateTokensFromBytes is a rough byte/4 heuristic (matches proxy streaming style).
func EstimateTokensFromBytes(n int) int {
	if n <= 0 {
		return 0
	}
	t := n / 4
	if t == 0 {
		return 1
	}
	return t
}

// EstimateTokensFromText counts tokens with the local tokenizer and falls back
// to the historical byte/4 heuristic only if the tokenizer is unavailable.
func EstimateTokensFromText(text string) int {
	return estimateTokensFromText(text, tokens.CountString)
}

func estimateTokensFromText(text string, countString func(string) int) int {
	if text == "" {
		return 0
	}
	if counted := countString(text); counted > 0 {
		return counted
	}
	return EstimateTokensFromBytes(len(text))
}

func estimateTokensFromByteSlices(parts ...[]byte) int {
	total := 0
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		total += EstimateTokensFromText(string(part))
	}
	return total
}

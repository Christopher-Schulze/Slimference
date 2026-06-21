package filter

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func isDotnetBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "dotnet" || b == "dotnet.exe"
}

func tryCompactDotnetOutput(argv []string) ([]byte, bool) {
	if len(argv) < 2 || !isDotnetBin(argv[0]) {
		return nil, false
	}
	return compactDotnetSubcommandOK(argv[1])
}

func compactDotnetSubcommandOK(sub string) ([]byte, bool) {
	switch sub {
	case "build":
		return []byte("[dotnet build] ok\n"), true
	case "test":
		return []byte("[dotnet test] ok\n"), true
	case "publish", "pack":
		return fmt.Appendf(nil, "[dotnet %s] ok\n", sub), true
	default:
		return nil, false
	}
}

func dotnetSubcommand(argv []string) (string, bool) {
	if len(argv) < 2 {
		return "", false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok {
			return "", false
		}
		return dotnetSubcommand(rest)
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return dotnetSubcommand(argv[2:])
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && argv[1] == "run" {
		return dotnetSubcommand(argv[2:])
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return dotnetSubcommand(argv[1:])
	}
	if !isDotnetBin(argv[0]) {
		return "", false
	}
	return argv[1], true
}

// TryCompactDotnet summarizes `dotnet build/test/publish/pack` output (F20).
// Empty stdout → "[dotnet X] ok"; non-empty → errors-only if succeeded, error extract if failed.
func TryCompactDotnet(argv []string, stdout []byte) ([]byte, bool) {
	s := strings.TrimSpace(string(stdout))
	sub, isDotnet := dotnetSubcommand(argv)
	if !isDotnet {
		return stdout, false
	}

	// Empty: direct ok.
	if s == "" {
		if out, ok := compactDotnetSubcommandOK(sub); ok {
			return out, true
		}
		return stdout, false
	}

	if sub != "build" && sub != "test" && sub != "publish" && sub != "pack" {
		return stdout, false
	}

	// Extract errors-only from non-empty dotnet output.
	compact := extractDotnetErrors(s, sub)
	if compact == "" || len(compact) >= len(s) {
		return stdout, false
	}
	return []byte(compact), true
}

// extractDotnetErrors extracts only the relevant lines from dotnet output.
// On success, returns a one-liner; on failure, returns only error/warning lines.
func extractDotnetErrors(s, sub string) string {
	low := strings.ToLower(s)

	if sub == "test" {
		if compact := compactDotnetTestAllPass(s); compact != "" {
			return compact
		}
	}

	// Success detection.
	if strings.Contains(low, "build succeeded") || strings.Contains(low, "test run successful") {
		// Count warnings/errors from summary.
		warnings, errors := 0, 0
		for line := range strings.SplitSeq(s, "\n") {
			t := strings.TrimSpace(line)
			if strings.HasSuffix(t, "Warning(s)") {
				fmt.Sscanf(t, "%d", &warnings)
			}
			if strings.HasSuffix(t, "Error(s)") {
				fmt.Sscanf(t, "%d", &errors)
			}
		}
		if errors == 0 {
			if warnings > 0 {
				return fmt.Sprintf("[dotnet %s] ok (%d warning(s))\n", sub, warnings)
			}
			return fmt.Sprintf("[dotnet %s] ok\n", sub)
		}
	}

	// Failure: extract error and warning lines.
	var errLines []string
	for line := range strings.SplitSeq(s, "\n") {
		t := strings.TrimSpace(line)
		tLow := strings.ToLower(t)
		if strings.Contains(tLow, "error") || strings.Contains(tLow, "failed") ||
			strings.Contains(tLow, "warning") || strings.HasPrefix(tLow, "failed") {
			if t != "" {
				errLines = append(errLines, t)
			}
		}
	}
	if len(errLines) == 0 {
		return ""
	}
	return fmt.Sprintf("[dotnet %s] FAILED\n%s\n", sub, strings.Join(errLines, "\n"))
}

func compactDotnetTestAllPass(s string) string {
	type row struct {
		line    string
		failed  int
		passed  int
		skipped int
		total   int
	}
	var rows []row
	for line := range strings.SplitSeq(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Passed!") {
			low := strings.ToLower(trimmed)
			if strings.Contains(low, "warning") || strings.Contains(low, "error") || strings.Contains(low, "failed") {
				return ""
			}
			continue
		}
		failed, okFailed := dotnetSummaryInt(trimmed, "Failed:")
		passed, okPassed := dotnetSummaryInt(trimmed, "Passed:")
		skipped, okSkipped := dotnetSummaryInt(trimmed, "Skipped:")
		total, okTotal := dotnetSummaryInt(trimmed, "Total:")
		if !okFailed || !okPassed || !okSkipped || !okTotal {
			continue
		}
		rows = append(rows, row{
			line:    strings.TrimRight(line, "\r"),
			failed:  failed,
			passed:  passed,
			skipped: skipped,
			total:   total,
		})
	}
	if len(rows) == 0 {
		return ""
	}
	passed, skipped, total := 0, 0, 0
	for _, row := range rows {
		if row.failed != 0 || row.total <= 0 {
			return ""
		}
		passed += row.passed
		skipped += row.skipped
		total += row.total
	}
	var out strings.Builder
	fmt.Fprintf(&out, "[dotnet test] ok (%d passed, %d skipped, %d total across %d assembly(s))\n", passed, skipped, total, len(rows))
	for _, row := range rows {
		out.WriteString(row.line)
		out.WriteByte('\n')
	}
	return out.String()
}

func dotnetSummaryInt(line, label string) (int, bool) {
	_, after, ok := strings.Cut(line, label)
	if !ok {
		return 0, false
	}
	rest := strings.TrimSpace(after)
	if rest == "" {
		return 0, false
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	value, err := strconv.Atoi(rest[:end])
	return value, err == nil
}

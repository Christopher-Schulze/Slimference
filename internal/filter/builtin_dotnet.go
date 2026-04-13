package filter

import (
	"fmt"
	"path/filepath"
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
	switch argv[1] {
	case "build":
		return []byte("[dotnet build] ok\n"), true
	case "test":
		return []byte("[dotnet test] ok\n"), true
	case "publish", "pack":
		return []byte(fmt.Sprintf("[dotnet %s] ok\n", argv[1])), true
	default:
		return nil, false
	}
}

// TryCompactDotnet summarizes `dotnet build/test/publish/pack` output (F20).
// Empty stdout → "[dotnet X] ok"; non-empty → errors-only if succeeded, error extract if failed.
func TryCompactDotnet(argv []string, stdout []byte) ([]byte, bool) {
	s := strings.TrimSpace(string(stdout))

	// Empty: direct ok.
	if s == "" {
		if out, ok := tryCompactDotnetOutput(argv); ok {
			return out, true
		}
		if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 {
			if out, ok2 := tryCompactDotnetOutput(rest); ok2 {
				return out, true
			}
		}
		b0 := strings.ToLower(filepath.Base(argv[0]))
		if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
			if out, ok := tryCompactDotnetOutput(argv[2:]); ok {
				return out, true
			}
		}
		if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
			if out, ok := tryCompactDotnetOutput(argv[1:]); ok {
				return out, true
			}
		}
		return stdout, false
	}

	// Non-empty: determine if this is a dotnet command we handle.
	isDotnet := false
	sub := ""
	if len(argv) >= 2 && isDotnetBin(argv[0]) {
		isDotnet = true
		sub = argv[1]
	} else if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isDotnetBin(rest[0]) {
		isDotnet = true
		sub = rest[1]
	}
	if !isDotnet {
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

	// Success detection.
	if strings.Contains(low, "build succeeded") || strings.Contains(low, "test run successful") ||
		strings.Contains(low, "passed!") {
		// Count warnings/errors from summary.
		warnings, errors := 0, 0
		for _, line := range strings.Split(s, "\n") {
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
	for _, line := range strings.Split(s, "\n") {
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

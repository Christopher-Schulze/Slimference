package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

const logMaxLines = 100

// TryCompactLogDedup collapses consecutive duplicate lines and truncates large logs (F15).
func TryCompactLogDedup(argv []string, stdout []byte) ([]byte, bool) {
	if !isDockerLogsArgv(argv) && !isKubectlLogsArgv(argv) {
		return stdout, false
	}
	s := string(stdout)

	// Step 1: collapse consecutive duplicates.
	deduped := collapseConsecutiveDuplicateLines(s)

	// Step 2: if result is still very long, apply log-level filtering or truncation.
	if len(deduped) > 4000 {
		filtered := filterLogOutput(deduped)
		if len(filtered) < len(deduped) {
			deduped = filtered
		}
	}

	if len(deduped) >= len(s) {
		return stdout, false
	}
	return []byte(deduped), true
}

// filterLogOutput removes DEBUG/TRACE lines and truncates to logMaxLines if still large.
func filterLogOutput(s string) string {
	lines := strings.Split(s, "\n")
	var kept []string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		tl := strings.ToLower(t)
		// Drop DEBUG and TRACE level lines (common in verbose logs).
		if strings.Contains(tl, " debug ") || strings.Contains(tl, "[debug]") ||
			strings.HasPrefix(tl, "debug ") || strings.HasPrefix(tl, "debug:") ||
			strings.Contains(tl, " trace ") || strings.Contains(tl, "[trace]") ||
			strings.HasPrefix(tl, "trace ") || strings.HasPrefix(tl, "trace:") {
			continue
		}
		kept = append(kept, line)
	}

	if len(kept) == 0 {
		return s // nothing filtered, don't replace
	}

	result := strings.Join(kept, "\n")
	if strings.HasSuffix(s, "\n") {
		result += "\n"
	}

	// If still too long, truncate and add notice.
	if len(kept) > logMaxLines {
		more := len(kept) - logMaxLines
		result = strings.Join(kept[:logMaxLines], "\n") +
			fmt.Sprintf("\n... +%d more log line(s)\n", more)
	}

	return result
}

func isDockerLogsArgv(argv []string) bool {
	if len(argv) < 2 || argv[1] != "logs" {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	return b == "docker" || b == "podman" || b == "podman.exe"
}

func isKubectlLogsArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := filepath.Base(argv[0])
	return (b == "kubectl" || b == "kubectl.exe") && argv[1] == "logs"
}

func collapseConsecutiveDuplicateLines(s string) string {
	trailingNL := strings.HasSuffix(s, "\n")
	body := s
	if trailingNL {
		body = strings.TrimSuffix(s, "\n")
	}
	if body == "" {
		if trailingNL {
			return "\n"
		}
		return ""
	}
	raw := strings.Split(body, "\n")
	lines := make([]string, len(raw))
	for i, ln := range raw {
		lines[i] = strings.TrimSuffix(ln, "\r")
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(lines) {
		line := lines[i]
		j := i + 1
		for j < len(lines) && lines[j] == line {
			j++
		}
		n := j - i
		b.WriteString(line)
		if n > 1 {
			fmt.Fprintf(&b, " [×%d]", n)
		}
		if j < len(lines) {
			b.WriteByte('\n')
		}
		i = j
	}
	out := b.String()
	if trailingNL {
		out += "\n"
	}
	return out
}

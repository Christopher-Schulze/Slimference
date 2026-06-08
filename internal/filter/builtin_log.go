package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

const logMaxLines = 100

func TryCompactLogDedup(argv []string, stdout []byte) ([]byte, bool) {
	return TryCompactLogOutput(argv, stdout)
}

func TryCompactLogOutput(argv []string, stdout []byte) ([]byte, bool) {
	argvMatch := isLogReadingArgv(argv)
	if !argvMatch {
		shape, conf := DetectLogShape(stdout)
		if conf < 0.7 {
			return stdout, false
		}
		_ = shape
	}
	if len(stdout) == 0 {
		return stdout, false
	}
	s := string(stdout)

	deduped := collapseConsecutiveDuplicateLines(s)

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

	// If still too long, truncate. Preserve error/warning lines even when they
	// sit past the positional budget so the failing line a verbose log was run
	// to surface is never silently dropped.
	if len(kept) > logMaxLines {
		result = truncateLogPreservingImportant(kept, logMaxLines)
		if strings.HasSuffix(s, "\n") && !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
	}

	return result
}

// importantLogLine reports whether a log line carries a signal the model likely
// needs (error / failure / warning / panic / fatal / exception / traceback), so
// it survives truncation even when it sits past the positional head budget.
func importantLogLine(line string) bool {
	tl := strings.ToLower(line)
	for _, tok := range []string{
		"error", "fatal", "panic", "fail", "exception", "traceback", "warn",
		"critical", "severe", "denied", "forbidden", "unauthorized",
		"permission denied", "refused", "rejected", "timeout", "timed out", "unhealthy",
		"crashloop", "oom", "out of memory", "segfault", "abort",
	} {
		if strings.Contains(tl, tok) {
			return true
		}
	}
	return false
}

// truncateLogPreservingImportant keeps up to maxLines of kept, prioritising
// error/warning lines (in original order) over plain head lines, then emits the
// selection in original order with a dropped-count notice. This replaces a blunt
// head-N cut that could drop the failing line a verbose log was run to surface.
func truncateLogPreservingImportant(kept []string, maxLines int) string {
	if len(kept) <= maxLines || maxLines <= 0 {
		return strings.Join(kept, "\n")
	}
	selected := make(map[int]struct{}, maxLines)
	// 1. error/warning lines first, in original order, up to the budget.
	for i, line := range kept {
		if len(selected) >= maxLines {
			break
		}
		if importantLogLine(line) {
			selected[i] = struct{}{}
		}
	}
	// 2. fill the remaining budget with head lines for context.
	for i := 0; i < len(kept) && len(selected) < maxLines; i++ {
		selected[i] = struct{}{}
	}
	// 3. emit in original order.
	ordered := make([]string, 0, len(selected))
	for i, line := range kept {
		if _, ok := selected[i]; ok {
			ordered = append(ordered, line)
		}
	}
	dropped := len(kept) - len(ordered)
	out := strings.Join(ordered, "\n")
	if dropped > 0 {
		out += fmt.Sprintf("\n... +%d more log line(s) (kept errors/warnings)\n", dropped)
	}
	return out
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

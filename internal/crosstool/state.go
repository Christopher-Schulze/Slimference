package crosstool

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

func IsGitStatusArgv(argv []string) bool {
	if !isGitArgv(argv) {
		return false
	}
	return slices.Contains(argv[1:], "status")
}

func IsGitDiffNameOnlyArgv(argv []string) bool {
	if !isGitArgv(argv) {
		return false
	}
	hasDiff := false
	hasNameOnly := false
	for _, arg := range argv[1:] {
		switch arg {
		case "diff":
			hasDiff = true
		case "--name-only":
			hasNameOnly = true
		}
	}
	return hasDiff && hasNameOnly
}

func Marker(count int, source string) string {
	source = strings.TrimSpace(strings.ReplaceAll(source, "`", "'"))
	if source == "" {
		source = "earlier git command"
	}
	if len(source) > 120 {
		source = source[:120] + "..."
	}
	return "[Slimference: " + intString(count) + " git paths already shown by previous `" + source + "`]\n"
}

func isGitArgv(argv []string) bool {
	return len(argv) >= 2 && filepath.Base(argv[0]) == "git"
}

func ExtractGitStatusPaths(output []byte) []string {
	lines := strings.Split(string(output), "\n")
	paths := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "##") || strings.HasPrefix(trimmed, "!!") {
			continue
		}
		if len(line) < 4 {
			return nil
		}
		status := line[:2]
		if !isGitStatusCode(status) || line[2] != ' ' {
			return nil
		}
		path := strings.TrimSpace(line[3:])
		if path == "" {
			return nil
		}
		if arrow := strings.LastIndex(path, " -> "); arrow >= 0 {
			path = path[arrow+4:]
		}
		paths = append(paths, normalizeGitPath(path))
	}
	return sortedUnique(paths)
}

func ExtractGitNameOnlyPaths(output []byte) []string {
	lines := strings.Split(string(output), "\n")
	paths := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, "\t\r") || strings.Contains(line, " ") {
			return nil
		}
		paths = append(paths, normalizeGitPath(line))
	}
	return sortedUnique(paths)
}

func isGitStatusCode(code string) bool {
	for _, r := range code {
		if !strings.ContainsRune(" MADRCU?!", r) {
			return false
		}
	}
	return code != "  "
}

func normalizeGitPath(path string) string {
	path = strings.TrimPrefix(path, "./")
	path = filepath.ToSlash(path)
	return path
}

func sortedUnique(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	out := paths[:0]
	for _, path := range paths {
		if path == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == path {
			continue
		}
		out = append(out, path)
	}
	return out
}

func intString(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

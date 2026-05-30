package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ---- F10: search results grouping ----

const (
	maxMatchesPerFile  = 20 // lines shown per file before "[+N more]"
	maxFilesShown      = 30 // files shown before "[+N more files]"
	minLinesForGrouped = 4  // only group if output is at least this many lines
)

// groupSearchResults groups grep/rg/fd style "file:line:content" output by file.
// Returns (grouped, ok). ok=false means unchanged passthrough.
func groupSearchResults(stdout []byte, toolName string) ([]byte, bool) {
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return stdout, false
	}
	lines := strings.Split(s, "\n")
	if len(lines) < minLinesForGrouped {
		return stdout, false
	}

	// Try to parse as "file:line:content" or "file:content".
	type matchLine struct {
		lineNum string
		content string
	}
	fileOrder := []string{}
	fileMatches := map[string][]matchLine{}

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		// Try "file:linenum:content" (rg/grep -n style)
		firstColon := strings.IndexByte(line, ':')
		if firstColon <= 0 {
			// Not parseable as grep output; fall through.
			return stdout, false
		}
		filePart := line[:firstColon]
		rest := line[firstColon+1:]

		// Check if rest starts with a line number (digits + colon).
		secColon := strings.IndexByte(rest, ':')
		lineNum := ""
		content := rest
		if secColon > 0 {
			potentialNum := rest[:secColon]
			allDigits := true
			for _, c := range potentialNum {
				if c < '0' || c > '9' {
					allDigits = false
					break
				}
			}
			if allDigits && len(potentialNum) > 0 {
				lineNum = potentialNum
				content = rest[secColon+1:]
			}
		}

		if _, seen := fileMatches[filePart]; !seen {
			fileOrder = append(fileOrder, filePart)
		}
		fileMatches[filePart] = append(fileMatches[filePart], matchLine{lineNum: lineNum, content: content})
	}

	// Count total matches.
	totalMatches := 0
	for _, ms := range fileMatches {
		totalMatches += len(ms)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] %d match(es) in %d file(s)\n", toolName, totalMatches, len(fileOrder)))

	selectedFiles := cappedSearchIndexes(len(fileOrder), maxFilesShown, 6)
	previousFile := -1
	for _, fileIdx := range selectedFiles {
		if previousFile >= 0 && fileIdx > previousFile+1 {
			sb.WriteString(fmt.Sprintf("  [+%d more files]\n", fileIdx-previousFile-1))
		}
		f := fileOrder[fileIdx]
		ms := fileMatches[f]
		sb.WriteString(fmt.Sprintf("  %s (%d match(es))\n", f, len(ms)))
		selectedMatches := cappedSearchIndexes(len(ms), maxMatchesPerFile, 6)
		previousMatch := -1
		for _, matchIdx := range selectedMatches {
			if previousMatch >= 0 && matchIdx > previousMatch+1 {
				sb.WriteString(fmt.Sprintf("    [+%d more]\n", matchIdx-previousMatch-1))
			}
			m := ms[matchIdx]
			if m.lineNum != "" {
				sb.WriteString(fmt.Sprintf("    %s: %s\n", m.lineNum, strings.TrimSpace(m.content)))
			} else {
				sb.WriteString(fmt.Sprintf("    %s\n", strings.TrimSpace(m.content)))
			}
			previousMatch = matchIdx
		}
		previousFile = fileIdx
	}
	if len(selectedFiles) > 0 && selectedFiles[len(selectedFiles)-1] < len(fileOrder)-1 {
		sb.WriteString(fmt.Sprintf("  [+%d more files]\n", len(fileOrder)-selectedFiles[len(selectedFiles)-1]-1))
	}

	result := sb.String()
	if len(result) >= len(s) {
		return stdout, false // no benefit
	}
	return []byte(result), true
}

func cappedSearchIndexes(total, budget, tail int) []int {
	if total <= 0 || budget <= 0 {
		return nil
	}
	if total <= budget {
		out := make([]int, total)
		for i := range out {
			out[i] = i
		}
		return out
	}
	if tail < 0 {
		tail = 0
	}
	if tail > budget/2 {
		tail = budget / 2
	}
	head := budget - tail
	out := make([]int, 0, budget)
	for i := 0; i < head; i++ {
		out = append(out, i)
	}
	for i := total - tail; i < total; i++ {
		if i >= head {
			out = append(out, i)
		}
	}
	return out
}

// searchToolName extracts a short display name for the search tool.
func searchToolName(argv []string) string {
	if len(argv) == 0 {
		return "search"
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	base = strings.TrimSuffix(base, ".exe")
	if base == "git" && len(argv) >= 2 && argv[1] == "grep" {
		return "git grep"
	}
	return base
}

// SearchOutputKeyFromCommandLine returns a stable key for grep-style search
// commands whose output can be safely compared across turns.
func SearchOutputKeyFromCommandLine(commandLine string) string {
	argv := primaryArgvForCapturedOutput(commandLine)
	if !isGrepStyleTool(argv) {
		return ""
	}
	return strings.Join(argv, "\t")
}

// TryCompactRipgrep summarizes empty stdout from ripgrep (F10 partial).
func TryCompactRipgrep(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	if strings.ToLower(filepath.Base(argv[0])) != "rg" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[rg] no matches\n"), true
}

// TryCompactGrep summarizes empty stdout from grep (F10 partial).
func TryCompactGrep(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "grep" && b != "ggrep" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[grep] no matches\n"), true
}

// TryCompactFd summarizes empty stdout from fd (F10 partial).
func TryCompactFd(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "fd" && b != "fd.exe" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[fd] no matches\n"), true
}

// TryCompactFind summarizes empty stdout from `find` (F10 partial).
func TryCompactFind(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	if strings.ToLower(filepath.Base(argv[0])) != "find" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[find] no matches\n"), true
}

// TryCompactAg summarizes empty stdout from the Silver Searcher `ag` (F10 partial).
func TryCompactAg(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "ag" && b != "ag.exe" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[ag] no matches\n"), true
}

// TryCompactAck summarizes empty stdout from ack (F10 partial).
func TryCompactAck(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "ack" && b != "ack.pl" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[ack] no matches\n"), true
}

// TryCompactUgrep summarizes empty stdout from ugrep (`ug` / `ugrep`) (F10 partial).
func TryCompactUgrep(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "ug" && b != "ug.exe" && b != "ugrep" && b != "ugrep.exe" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[ug] no matches\n"), true
}

// TryCompactSift summarizes empty stdout from the `sift` search tool (F10 partial).
func TryCompactSift(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "sift" && b != "sift.exe" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[sift] no matches\n"), true
}

// TryCompactPlocate summarizes empty stdout from `plocate` (F10 partial).
func TryCompactPlocate(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "plocate" && b != "plocate.exe" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[plocate] no matches\n"), true
}

// TryCompactLocate summarizes empty stdout from `locate` (F10 partial).
func TryCompactLocate(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "locate" && b != "locate.exe" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[locate] no matches\n"), true
}

// TryCompactSk summarizes empty stdout from `sk` (skim fuzzy finder; F10 partial).
func TryCompactSk(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "sk" && b != "sk.exe" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[sk] no matches\n"), true
}

// TryCompactGitGrep summarizes empty stdout from `git grep` (F10 partial).
func TryCompactGitGrep(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	if strings.ToLower(filepath.Base(argv[0])) != "git" {
		return stdout, false
	}
	if argv[1] != "grep" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[git grep] no matches\n"), true
}

// TryCompactSearchOutput chains empty-result detection and then non-empty result grouping (F10).
func TryCompactSearchOutput(argv []string, stdout []byte) ([]byte, bool) {
	// Empty-result detection for all tools.
	for _, fn := range []func([]string, []byte) ([]byte, bool){
		TryCompactGitGrep, TryCompactRipgrep, TryCompactGrep,
		TryCompactFd, TryCompactFind, TryCompactAg, TryCompactAck,
		TryCompactUgrep, TryCompactSift, TryCompactPlocate,
		TryCompactLocate, TryCompactSk,
	} {
		if out, ok := fn(argv, stdout); ok {
			return out, true
		}
	}

	// Non-empty: try grouped output for grep-style tools (file:line:content format).
	if isGrepStyleTool(argv) {
		tool := searchToolName(argv)
		if out, ok := groupSearchResults(stdout, tool); ok {
			return out, true
		}
	}

	return stdout, false
}

// isGrepStyleTool returns true for tools that emit "file:line:content" style output.
func isGrepStyleTool(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	b = strings.TrimSuffix(b, ".exe")
	switch b {
	case "rg", "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift":
		return true
	case "git":
		return len(argv) >= 2 && argv[1] == "grep"
	}
	return false
}

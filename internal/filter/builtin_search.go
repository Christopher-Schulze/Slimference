package filter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
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
	skipped, nonEmpty := 0, 0

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		nonEmpty++
		if isSearchEnvelopeNoiseLine(line) {
			skipped++
			continue
		}
		// Try "file:linenum:content" (rg/grep -n style).
		firstColon := strings.IndexByte(line, ':')
		if firstColon <= 0 {
			// A colon-less line is noise: a header line (e.g. rg's
			// "Total output lines: N"), a context separator, or a line cut off
			// by Codex's output truncation. Skip it rather than abandoning the
			// whole grouping - a single such line must not defeat compaction.
			skipped++
			continue
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

	// Robustness guard: only group when this really looks like grep output -
	// at least one parsed match and noise (colon-less lines) does not dominate.
	// This keeps a few truncation/header lines from defeating compaction while
	// refusing to summarize an output that is not actually file:line:content.
	if totalMatches == 0 || skipped*2 > nonEmpty {
		return stdout, false
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

func isSearchEnvelopeNoiseLine(line string) bool {
	line = strings.TrimSpace(line)
	switch {
	case line == "Output:":
		return true
	case strings.HasPrefix(line, "Total output lines:"):
		return true
	case strings.HasPrefix(line, "Chunk ID:"):
		return true
	case strings.HasPrefix(line, "Wall time:"):
		return true
	case strings.HasPrefix(line, "Process exited with code "):
		return true
	case strings.HasPrefix(line, "Original token count:"):
		return true
	default:
		return false
	}
}

// CanonicalSearchMatchSet returns a stable, order-insensitive representation of
// grep-style file:line:content output. It is an identity primitive for search
// caches, not a model-facing replacement by itself.
func CanonicalSearchMatchSet(stdout []byte) (string, bool) {
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return "", false
	}
	if strings.HasPrefix(s, "[") {
		return "", false
	}
	lines := strings.Split(s, "\n")
	type matchLine struct {
		file    string
		lineNum string
		content string
	}
	matches := make([]matchLine, 0, len(lines))
	skipped, nonEmpty := 0, 0
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		nonEmpty++
		if isSearchEnvelopeNoiseLine(line) {
			skipped++
			continue
		}
		firstColon := strings.IndexByte(line, ':')
		if firstColon <= 0 {
			skipped++
			continue
		}
		filePart := line[:firstColon]
		rest := line[firstColon+1:]
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
		matches = append(matches, matchLine{
			file:    filePart,
			lineNum: lineNum,
			content: strings.TrimSpace(content),
		})
	}
	if len(matches) == 0 || skipped*2 > nonEmpty {
		return "", false
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.lineNum != b.lineNum {
			ai, aerr := strconv.Atoi(a.lineNum)
			bi, berr := strconv.Atoi(b.lineNum)
			if aerr == nil && berr == nil {
				return ai < bi
			}
			return a.lineNum < b.lineNum
		}
		return a.content < b.content
	})
	var sb strings.Builder
	for _, m := range matches {
		sb.WriteString(m.file)
		sb.WriteByte(':')
		if m.lineNum != "" {
			sb.WriteString(m.lineNum)
			sb.WriteByte(':')
		}
		sb.WriteString(m.content)
		sb.WriteByte('\n')
	}
	return sb.String(), true
}

func cappedSearchIndexes(total, budget, tail int) []int {
	return cappedEvidenceIndexes(total, budget, tail)
}

// searchToolName extracts a short display name for the search tool.
func searchToolName(argv []string) string {
	if len(argv) == 0 {
		return "search"
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	base = strings.TrimSuffix(base, ".exe")
	if base == "git" && gitGrepIndex(argv) >= 0 {
		return "git grep"
	}
	return base
}

// SearchOutputKeyFromCommandLine returns a stable key for grep-style search
// commands whose output can be safely compared across turns.
func SearchOutputKeyFromCommandLine(commandLine string) string {
	if normalized := NormalizeSearchCommandLine(commandLine, ""); normalized != "" {
		argv := primaryArgvForCapturedOutput(normalized)
		if isGrepStyleTool(argv) {
			return strings.Join(argv, "\t")
		}
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	if !isGrepStyleTool(argv) {
		return ""
	}
	return strings.Join(argv, "\t")
}

// RepoScopedSearchOutputKeyFromCommandLine returns a stable search key only
// when the command carries repository scope in the command line itself. This is
// stricter than SearchOutputKeyFromCommandLine and is intended for cross-turn
// cache/delta identity, where an implicit cwd would be unsafe to reuse.
func RepoScopedSearchOutputKeyFromCommandLine(commandLine string) string {
	if normalized := NormalizeSearchCommandLine(commandLine, ""); normalized != "" {
		argv := primaryArgvForCapturedOutput(normalized)
		if isGrepStyleTool(argv) && searchArgvHasRepoScope(argv) {
			return strings.Join(argv, "\t")
		}
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	if !isGrepStyleTool(argv) || !searchArgvHasRepoScope(argv) {
		return ""
	}
	return strings.Join(argv, "\t")
}

// NormalizeSearchCommandLine returns a canonical search command line that keeps
// repository scope in the argv itself. It is used only for compaction/keying,
// never to execute a user command.
func NormalizeSearchCommandLine(commandLine, workdir string) string {
	commandLine = strings.TrimSpace(commandLine)
	workdir = cleanSearchWorkdir(workdir)
	if cdWorkdir, inner, ok := splitLeadingCDSearch(commandLine); ok {
		workdir = cdWorkdir
		commandLine = inner
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	if !isGrepStyleTool(argv) {
		return ""
	}
	if workdir == "" {
		return ""
	}
	if gitGrepIndex(argv) >= 0 {
		return joinSearchArgs(ensureGitCSearchArgv(argv, workdir))
	}
	return joinSearchArgs(applySearchWorkdir(argv, workdir))
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
	if gitGrepIndex(argv) < 0 {
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

	// Non-empty: try grouped output only for grep-style tools that are expected
	// to emit file:line:content rows. Flags such as --json, --files, -l, or -c
	// intentionally full-pass because grouping those formats would change the
	// meaning of the output instead of just removing repeated file prefixes.
	if isGrepStyleTool(argv) && searchProducesMatchLineOutput(argv) {
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
		return gitGrepIndex(argv) >= 0
	}
	return false
}

func searchProducesMatchLineOutput(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			return true
		}
		switch {
		case arg == "--json" || arg == "--files" || arg == "--files-with-matches" ||
			arg == "--files-without-match" || arg == "--count" || arg == "--count-matches" ||
			arg == "--only-matching" || arg == "--vimgrep" || arg == "--type-list":
			return false
		case arg == "-l" || arg == "-L" || arg == "-c" || arg == "-o":
			return false
		case strings.HasPrefix(arg, "--json="), strings.HasPrefix(arg, "--files="),
			strings.HasPrefix(arg, "--files-with-matches="), strings.HasPrefix(arg, "--files-without-match="),
			strings.HasPrefix(arg, "--count="), strings.HasPrefix(arg, "--count-matches="),
			strings.HasPrefix(arg, "--only-matching="), strings.HasPrefix(arg, "--vimgrep="):
			return false
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--"):
			if shortSearchOutputFlagDisablesGrouping(arg) {
				return false
			}
		}
	}
	return true
}

func shortSearchOutputFlagDisablesGrouping(arg string) bool {
	if len(arg) < 2 || !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return false
	}
	for _, r := range arg[1:] {
		switch r {
		case 'l', 'L', 'c', 'o':
			return true
		}
	}
	return false
}

func gitGrepIndex(argv []string) int {
	if len(argv) < 2 || strings.ToLower(filepath.Base(argv[0])) != "git" {
		return -1
	}
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "grep":
			return i
		case arg == "-C", arg == "--git-dir", arg == "--work-tree", arg == "-c":
			if i+1 < len(argv) {
				i++
			}
		case strings.HasPrefix(arg, "--git-dir="), strings.HasPrefix(arg, "--work-tree="), strings.HasPrefix(arg, "-c"):
			continue
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return -1
		}
	}
	return -1
}

func ensureGitCSearchArgv(argv []string, workdir string) []string {
	out := append([]string(nil), argv...)
	for i := 1; i < len(out); i++ {
		if out[i] == "-C" {
			if i+1 < len(out) {
				out[i+1] = cleanSearchPath(out[i+1], workdir)
			}
			return out
		}
	}
	withC := make([]string, 0, len(out)+2)
	withC = append(withC, out[0], "-C", workdir)
	withC = append(withC, out[1:]...)
	return withC
}

func searchArgvHasRepoScope(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	if gitGrepIndex(argv) >= 0 {
		for i := 1; i < len(argv); i++ {
			arg := argv[i]
			switch {
			case arg == "-C" || arg == "--work-tree":
				if i+1 < len(argv) && filepath.IsAbs(argv[i+1]) {
					return true
				}
				i++
			case strings.HasPrefix(arg, "--work-tree="):
				if filepath.IsAbs(strings.TrimPrefix(arg, "--work-tree=")) {
					return true
				}
			}
		}
		return false
	}
	for _, idx := range searchPathArgIndexes(argv) {
		if idx >= 0 && idx < len(argv) && filepath.IsAbs(argv[idx]) {
			return true
		}
	}
	return false
}

func applySearchWorkdir(argv []string, workdir string) []string {
	out := append([]string(nil), argv...)
	indexes := searchPathArgIndexes(out)
	if len(indexes) == 0 {
		return append(out, workdir)
	}
	for _, idx := range indexes {
		out[idx] = cleanSearchPath(out[idx], workdir)
	}
	return out
}

func searchPathArgIndexes(argv []string) []int {
	if len(argv) == 0 || gitGrepIndex(argv) >= 0 {
		return nil
	}
	patternSeen := false
	stopOptions := false
	var indexes []int
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		if !stopOptions && arg == "--" {
			stopOptions = true
			continue
		}
		if !stopOptions && strings.HasPrefix(arg, "-") {
			kind := searchOptionKind(arg)
			if kind.consumesValue && i+1 < len(argv) {
				if kind.patternValue {
					patternSeen = true
				}
				i++
			} else if kind.patternValue {
				patternSeen = true
			}
			continue
		}
		if !patternSeen {
			patternSeen = true
			continue
		}
		indexes = append(indexes, i)
	}
	return indexes
}

type searchOptionInfo struct {
	consumesValue bool
	patternValue  bool
}

func searchOptionKind(arg string) searchOptionInfo {
	switch {
	case arg == "-e" || arg == "--regexp":
		return searchOptionInfo{consumesValue: true, patternValue: true}
	case strings.HasPrefix(arg, "-e") && len(arg) > 2:
		return searchOptionInfo{patternValue: true}
	case strings.HasPrefix(arg, "--regexp="):
		return searchOptionInfo{patternValue: true}
	case arg == "-f" || arg == "--file":
		return searchOptionInfo{consumesValue: true, patternValue: true}
	case strings.HasPrefix(arg, "--file="):
		return searchOptionInfo{patternValue: true}
	case arg == "-g" || arg == "--glob" || arg == "--iglob" ||
		arg == "-t" || arg == "--type" || arg == "-T" || arg == "--type-not" ||
		arg == "--type-add" || arg == "-A" || arg == "-B" || arg == "-C" ||
		arg == "--context" || arg == "--after-context" || arg == "--before-context" ||
		arg == "-m" || arg == "--max-count" || arg == "-j" || arg == "--threads" ||
		arg == "--include" || arg == "--exclude" || arg == "--exclude-dir" ||
		arg == "--include-dir" || arg == "--exclude-from" || arg == "--include-from" ||
		arg == "--path-separator" || arg == "--field-context-separator" ||
		arg == "--field-match-separator" || arg == "--sort" || arg == "--sortr" ||
		arg == "--engine" || arg == "--pre" || arg == "--pre-glob" ||
		arg == "--replace" || arg == "-r" || arg == "--max-filesize" ||
		arg == "--dfa-size-limit" || arg == "--regex-size-limit" || arg == "--hostname-bin":
		return searchOptionInfo{consumesValue: true}
	case strings.HasPrefix(arg, "--glob="), strings.HasPrefix(arg, "--type="),
		strings.HasPrefix(arg, "--iglob="), strings.HasPrefix(arg, "--type-not="),
		strings.HasPrefix(arg, "--type-add="), strings.HasPrefix(arg, "--context="),
		strings.HasPrefix(arg, "--after-context="), strings.HasPrefix(arg, "--before-context="),
		strings.HasPrefix(arg, "--max-count="), strings.HasPrefix(arg, "--threads="),
		strings.HasPrefix(arg, "--include="), strings.HasPrefix(arg, "--exclude="),
		strings.HasPrefix(arg, "--exclude-dir="), strings.HasPrefix(arg, "--include-dir="),
		strings.HasPrefix(arg, "--exclude-from="), strings.HasPrefix(arg, "--include-from="),
		strings.HasPrefix(arg, "--path-separator="),
		strings.HasPrefix(arg, "--field-context-separator="),
		strings.HasPrefix(arg, "--field-match-separator="), strings.HasPrefix(arg, "--sort="),
		strings.HasPrefix(arg, "--sortr="), strings.HasPrefix(arg, "--engine="),
		strings.HasPrefix(arg, "--pre="), strings.HasPrefix(arg, "--pre-glob="),
		strings.HasPrefix(arg, "--replace="), strings.HasPrefix(arg, "--max-filesize="),
		strings.HasPrefix(arg, "--dfa-size-limit="), strings.HasPrefix(arg, "--regex-size-limit="),
		strings.HasPrefix(arg, "--hostname-bin="):
		return searchOptionInfo{}
	default:
		return searchOptionInfo{}
	}
}

func splitLeadingCDSearch(commandLine string) (workdir, inner string, ok bool) {
	idx := strings.Index(commandLine, "&&")
	if idx < 0 {
		return "", "", false
	}
	prefix := strings.TrimSpace(commandLine[:idx])
	rest := strings.TrimSpace(commandLine[idx+len("&&"):])
	argv := primaryArgvForCapturedOutput(prefix)
	if len(argv) != 2 || strings.ToLower(filepath.Base(argv[0])) != "cd" {
		return "", "", false
	}
	workdir = cleanSearchWorkdir(argv[1])
	if workdir == "" || rest == "" {
		return "", "", false
	}
	return workdir, rest, true
}

func cleanSearchWorkdir(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" || !filepath.IsAbs(workdir) {
		return ""
	}
	return filepath.Clean(workdir)
}

func cleanSearchPath(path, workdir string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if path == "." {
		return workdir
	}
	return filepath.Clean(filepath.Join(workdir, path))
}

func joinSearchArgs(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, quoteSearchArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteSearchArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if strings.IndexFunc(arg, func(r rune) bool {
		return r == '"' || r == '\\' || r <= ' ' || r == '\'' || r == '$' || r == '`' ||
			r == '|' || r == '&' || r == ';' || r == '*' || r == '?'
	}) < 0 {
		return arg
	}
	return strconv.Quote(arg)
}

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

type searchMatchLine struct {
	lineNum string
	content string
	score   int
}

type parsedSearchLine struct {
	file    string
	lineNum string
	content string
}

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

	fileOrder := []string{}
	fileMatches := map[string][]searchMatchLine{}
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
		parsed, ok := parseSearchMatchLine(line)
		if !ok {
			// An unparsable line is noise: a header line (e.g. rg's
			// "Total output lines: N"), a context separator, or a line cut off
			// by Codex's output truncation. Skip it rather than abandoning the
			// whole grouping - a single such line must not defeat compaction.
			skipped++
			continue
		}

		if _, seen := fileMatches[parsed.file]; !seen {
			fileOrder = append(fileOrder, parsed.file)
		}
		fileMatches[parsed.file] = append(fileMatches[parsed.file], searchMatchLine{
			lineNum: parsed.lineNum,
			content: parsed.content,
			score:   searchEvidenceScore(parsed.content),
		})
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

	selectedFiles := selectSearchFileIndexes(fileOrder, fileMatches, maxFilesShown)
	previousFile := -1
	for _, fileIdx := range selectedFiles {
		if previousFile >= 0 && fileIdx > previousFile+1 {
			sb.WriteString(fmt.Sprintf("  [+%d more files]\n", fileIdx-previousFile-1))
		}
		f := fileOrder[fileIdx]
		ms := fileMatches[f]
		sb.WriteString(fmt.Sprintf("  %s (%d match(es))\n", f, len(ms)))
		selectedMatches := selectSearchMatchIndexes(ms, maxMatchesPerFile)
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

func parseSearchMatchLine(line string) (parsedSearchLine, bool) {
	if file, lineNum, content, ok := parseNumberedSearchMatchLine(line); ok {
		return parsedSearchLine{file: file, lineNum: lineNum, content: content}, true
	}
	if file, content, ok := parseUnnumberedSearchMatchLine(line); ok {
		return parsedSearchLine{file: file, content: content}, true
	}
	return parsedSearchLine{}, false
}

func parseNumberedSearchMatchLine(line string) (file string, lineNum string, content string, ok bool) {
	scanStart := 0
	if hasWindowsDrivePrefix(line) {
		scanStart = 2
	}
	for i := scanStart; i < len(line); i++ {
		sep := line[i]
		if sep != ':' && sep != '-' {
			continue
		}
		if i == 0 || i+2 >= len(line) || line[i+1] < '0' || line[i+1] > '9' {
			continue
		}
		if line[i-1] == ':' || line[i-1] == '-' {
			continue
		}
		j := i + 1
		for j < len(line) && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		if j == len(line) || (line[j] != ':' && line[j] != '-') {
			continue
		}
		if j+1 > len(line) {
			continue
		}
		file = line[:i]
		if !looksLikeSearchFile(file) {
			continue
		}
		return file, line[i+1 : j], line[j+1:], true
	}
	return "", "", "", false
}

func parseUnnumberedSearchMatchLine(line string) (file string, content string, ok bool) {
	scanStart := 0
	if hasWindowsDrivePrefix(line) {
		scanStart = 2
	}
	idx := strings.IndexByte(line[scanStart:], ':')
	if idx < 0 {
		return "", "", false
	}
	idx += scanStart
	if idx <= 0 || idx == len(line)-1 {
		return "", "", false
	}
	file = line[:idx]
	if !looksLikeSearchFile(file) {
		return "", "", false
	}
	return file, line[idx+1:], true
}

func hasWindowsDrivePrefix(line string) bool {
	return len(line) >= 3 &&
		((line[0] >= 'A' && line[0] <= 'Z') || (line[0] >= 'a' && line[0] <= 'z')) &&
		line[1] == ':' &&
		(line[2] == '\\' || line[2] == '/')
}

func looksLikeSearchFile(file string) bool {
	file = strings.TrimSpace(file)
	return file != "" &&
		(strings.Contains(file, "/") || strings.Contains(file, "\\") || strings.Contains(file, "."))
}

func selectSearchFileIndexes(fileOrder []string, fileMatches map[string][]searchMatchLine, budget int) []int {
	total := len(fileOrder)
	selected := map[int]struct{}{}
	seedFirstLastSearchIndexes(selected, total, budget)
	for len(selected) < min(budget, total) {
		bestIdx, bestScore := -1, 0
		for i, file := range fileOrder {
			if _, ok := selected[i]; ok {
				continue
			}
			score := 0
			for _, match := range fileMatches[file] {
				score += match.score
			}
			if score > bestScore {
				bestIdx, bestScore = i, score
			}
		}
		if bestIdx < 0 || bestScore <= 0 {
			break
		}
		selected[bestIdx] = struct{}{}
	}
	for _, idx := range cappedSearchIndexes(total, budget, 6) {
		if len(selected) >= min(budget, total) {
			break
		}
		selected[idx] = struct{}{}
	}
	return sortedSearchIndexes(selected)
}

func selectSearchMatchIndexes(matches []searchMatchLine, budget int) []int {
	total := len(matches)
	selected := map[int]struct{}{}
	seedFirstLastSearchIndexes(selected, total, budget)
	for len(selected) < min(budget, total) {
		bestIdx, bestScore := -1, 0
		for i, match := range matches {
			if _, ok := selected[i]; ok {
				continue
			}
			if match.score > bestScore {
				bestIdx, bestScore = i, match.score
			}
		}
		if bestIdx < 0 || bestScore <= 0 {
			break
		}
		selected[bestIdx] = struct{}{}
	}
	for _, idx := range cappedSearchIndexes(total, budget, 6) {
		if len(selected) >= min(budget, total) {
			break
		}
		selected[idx] = struct{}{}
	}
	return sortedSearchIndexes(selected)
}

func seedFirstLastSearchIndexes(selected map[int]struct{}, total, budget int) {
	if total <= 0 || budget <= 0 {
		return
	}
	selected[0] = struct{}{}
	if budget > 1 && total > 1 {
		selected[total-1] = struct{}{}
	}
}

func sortedSearchIndexes(selected map[int]struct{}) []int {
	out := make([]int, 0, len(selected))
	for idx := range selected {
		out = append(out, idx)
	}
	sort.Ints(out)
	return out
}

func searchEvidenceScore(content string) int {
	lower := strings.ToLower(content)
	score := 0
	for _, word := range []string{"panic", "fatal", "critical", "exception", "traceback", "crash", "error", "failed", "failure", "abort", "timeout", "timed out", "rejected", "denied", "invalid"} {
		if strings.Contains(lower, word) {
			score += 100
			break
		}
	}
	for _, word := range []string{"warning", "warn "} {
		if strings.Contains(lower, word) {
			score += 60
			break
		}
	}
	for _, word := range []string{"todo", "fixme", "important", "bug", "security", "secret", "password", "auth"} {
		if strings.Contains(lower, word) {
			score += 30
			break
		}
	}
	return score
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
		parsed, ok := parseSearchMatchLine(line)
		if !ok {
			skipped++
			continue
		}
		matches = append(matches, matchLine{
			file:    parsed.file,
			lineNum: parsed.lineNum,
			content: strings.TrimSpace(parsed.content),
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
		if isGrepStyleTool(argv) && searchProducesMatchLineOutput(argv) {
			return strings.Join(argv, "\t")
		}
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	if !isGrepStyleTool(argv) || !searchProducesMatchLineOutput(argv) {
		return ""
	}
	return strings.Join(argv, "\t")
}

// SearchOutputReducerEligibleFromCommandLine reports whether commandLine can
// route through the search_output reducer. It is intentionally broader than
// SearchOutputKeyFromCommandLine because some search reducers are useful for
// one-shot output compaction but unsafe as cross-turn identity keys.
func SearchOutputReducerEligibleFromCommandLine(commandLine, workDir string) bool {
	for _, candidate := range []string{commandLine, NormalizeSearchCommandLine(commandLine, workDir)} {
		if candidate == "" {
			continue
		}
		if searchOutputReducerEligibleArgv(primaryArgvForCapturedOutput(candidate)) {
			return true
		}
	}
	return false
}

func searchOutputReducerEligibleArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	if isPathListTool(argv) {
		return true
	}
	if isGrepStyleTool(argv) && searchProducesMatchLineOutput(argv) {
		return true
	}
	return isSearchEmptyResultTool(argv)
}

func isSearchEmptyResultTool(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	b = strings.TrimSuffix(b, ".exe")
	switch b {
	case "rg", "grep", "ggrep", "fd", "find", "ag", "ack", "ack.pl", "ug", "ugrep", "sift", "plocate", "locate", "sk":
		return true
	case "git":
		return gitGrepIndex(argv) >= 0
	default:
		return false
	}
}

// RepoScopedSearchOutputKeyFromCommandLine returns a stable search key only
// when the command carries repository scope in the command line itself. This is
// stricter than SearchOutputKeyFromCommandLine and is intended for cross-turn
// cache/delta identity, where an implicit cwd would be unsafe to reuse.
func RepoScopedSearchOutputKeyFromCommandLine(commandLine string) string {
	if normalized := NormalizeSearchCommandLine(commandLine, ""); normalized != "" {
		argv := primaryArgvForCapturedOutput(normalized)
		if isGrepStyleTool(argv) && searchProducesMatchLineOutput(argv) && searchArgvHasRepoScope(argv) {
			return strings.Join(argv, "\t")
		}
	}
	argv := primaryArgvForCapturedOutput(commandLine)
	if !isGrepStyleTool(argv) || !searchProducesMatchLineOutput(argv) || !searchArgvHasRepoScope(argv) {
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

	if isPathListTool(argv) {
		if out, ok := groupPathListResults(stdout, searchToolName(argv)); ok {
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

func isPathListTool(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	b = strings.TrimSuffix(b, ".exe")
	return b == "fd" || b == "find"
}

func groupPathListResults(stdout []byte, toolName string) ([]byte, bool) {
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return stdout, false
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 8 {
		return stdout, false
	}

	var sb strings.Builder
	sb.WriteString("[")
	sb.WriteString(toolName)
	sb.WriteString(" paths]\n")
	currentDir := ""
	grouped := 0
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if !safePathListLine(line) {
			return stdout, false
		}
		idx := strings.LastIndex(line, "/")
		if idx <= 0 || idx == len(line)-1 {
			return stdout, false
		}
		dir := line[:idx+1]
		base := line[idx+1:]
		if dir != currentDir {
			sb.WriteString(dir)
			sb.WriteByte('\n')
			currentDir = dir
			grouped++
		}
		sb.WriteString("  ")
		sb.WriteString(base)
		sb.WriteByte('\n')
	}
	out := sb.String()
	if grouped == 0 || len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func safePathListLine(line string) bool {
	if line == "" || strings.TrimSpace(line) != line {
		return false
	}
	for _, r := range line {
		if r == 0 || r == '\n' || r == '\r' || r == '\t' {
			return false
		}
		if r < 0x20 {
			return false
		}
	}
	return true
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
	args := searchOutputModeArgs(argv)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return true
		}
		switch {
		case searchOutputFlagDisablesGrouping(arg):
			return false
		case arg == "-l" || arg == "-L" || arg == "-c" || arg == "-o":
			return false
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--"):
			if shortSearchOutputFlagDisablesGrouping(arg) {
				return false
			}
			if kind := searchOptionKind(arg); kind.consumesValue && i+1 < len(args) {
				i++
			}
		case strings.HasPrefix(arg, "--"):
			if kind := searchOptionKind(arg); kind.consumesValue && i+1 < len(args) {
				i++
			}
		}
	}
	return true
}

func searchOutputModeArgs(argv []string) []string {
	if idx := gitGrepIndex(argv); idx >= 0 {
		return argv[idx+1:]
	}
	if len(argv) <= 1 {
		return nil
	}
	return argv[1:]
}

func searchOutputFlagDisablesGrouping(arg string) bool {
	switch arg {
	case "--json", "--files", "--files-with-matches", "--files-without-match",
		"--count", "--count-matches", "--only-matching", "--vimgrep", "--type-list",
		"--heading", "--pretty", "--context", "--after-context", "--before-context",
		"--passthru", "--multiline", "--multiline-dotall",
		"--field-context-separator", "--field-match-separator",
		"--null", "--null-data", "--path-separator":
		return true
	default:
		return strings.HasPrefix(arg, "--json=") ||
			strings.HasPrefix(arg, "--files=") ||
			strings.HasPrefix(arg, "--files-with-matches=") ||
			strings.HasPrefix(arg, "--files-without-match=") ||
			strings.HasPrefix(arg, "--count=") ||
			strings.HasPrefix(arg, "--count-matches=") ||
			strings.HasPrefix(arg, "--only-matching=") ||
			strings.HasPrefix(arg, "--vimgrep=") ||
			strings.HasPrefix(arg, "--heading=") ||
			strings.HasPrefix(arg, "--pretty=") ||
			strings.HasPrefix(arg, "--context=") ||
			strings.HasPrefix(arg, "--after-context=") ||
			strings.HasPrefix(arg, "--before-context=") ||
			strings.HasPrefix(arg, "--passthru=") ||
			strings.HasPrefix(arg, "--multiline=") ||
			strings.HasPrefix(arg, "--multiline-dotall=") ||
			strings.HasPrefix(arg, "--field-context-separator=") ||
			strings.HasPrefix(arg, "--field-match-separator=") ||
			strings.HasPrefix(arg, "--null=") ||
			strings.HasPrefix(arg, "--null-data=") ||
			strings.HasPrefix(arg, "--path-separator=")
	}
}

func shortSearchOutputFlagDisablesGrouping(arg string) bool {
	if len(arg) < 2 || !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return false
	}
	if strings.HasPrefix(arg, "-A") || strings.HasPrefix(arg, "-B") || strings.HasPrefix(arg, "-C") ||
		strings.HasPrefix(arg, "-U") || strings.HasPrefix(arg, "-p") {
		return true
	}
	for _, r := range arg[1:] {
		switch r {
		case 'l', 'L', 'c', 'o', 'A', 'B', 'C', 'U', 'p', '0', 'Z':
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

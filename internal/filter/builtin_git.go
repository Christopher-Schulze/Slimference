package filter

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ---- F02: git log compact (hash + subject + file stats) ----

// TryCompactGitLog handles `git log` stdout: empty → one-liner; non-empty → condensed.
func TryCompactGitLog(argv []string, stdout []byte) ([]byte, bool) {
	if !isGitLogArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[git log] empty\n"), true
	}
	compact := compactGitLog(s)
	if compact == "" || len(compact) >= len(s) {
		return stdout, false
	}
	return []byte(compact), true
}

// reGitLogCommitHeader matches "commit <hash>" lines (with optional refs like (HEAD -> main)).
var reGitLogCommitHeader = regexp.MustCompile(`^commit ([0-9a-f]{7,40})`)

// compactGitLog condenses a multi-commit log to one line per commit.
// Format: "<hash7> <subject> [<+N/-M files> N files]"
func compactGitLog(s string) string {
	type entry struct {
		hash    string
		subject string
		stat    string // e.g. "3 files, +12/-5"
	}
	var entries []entry
	var cur *entry

	// Track stat section parsing state.
	var inBody, inStat bool

	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimRight(raw, "\r")

		if m := reGitLogCommitHeader.FindStringSubmatch(line); m != nil {
			if cur != nil {
				entries = append(entries, *cur)
			}
			cur = &entry{hash: m[1][:7]}
			inBody = false
			inStat = false
			continue
		}
		if cur == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)

		// Blank line separates header from body/stats.
		if trimmed == "" {
			if cur.subject != "" {
				inStat = true
			}
			inBody = cur.subject == ""
			continue
		}

		if strings.HasPrefix(line, "Author:") || strings.HasPrefix(line, "Date:") ||
			strings.HasPrefix(line, "Merge:") || strings.HasPrefix(line, "Commit:") {
			continue
		}

		if inBody && cur.subject == "" {
			cur.subject = trimmed
			inBody = false
			inStat = false
			continue
		}

		if inStat {
			// stat lines: " file.go | 5 +++++" or "3 files changed, 12 insertions(+), 5 deletions(-)"
			if reGitStatSummary.MatchString(trimmed) {
				cur.stat = parseGitStatSummary(trimmed)
			}
		}
	}
	if cur != nil {
		entries = append(entries, *cur)
	}

	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[git log] %d commit(s)\n", len(entries)))
	for _, e := range entries {
		if e.stat != "" {
			sb.WriteString(fmt.Sprintf("  %s %s [%s]\n", e.hash, e.subject, e.stat))
		} else {
			sb.WriteString(fmt.Sprintf("  %s %s\n", e.hash, e.subject))
		}
	}
	return sb.String()
}

// reGitStatSummary matches the "N files changed, M insertions" summary line.
var reGitStatSummary = regexp.MustCompile(`\d+ file.* changed`)

// parseGitStatSummary extracts a compact "+M/-N" string from the git stat summary line.
func parseGitStatSummary(line string) string {
	var ins, del, files int
	fmt.Sscanf(line, "%d file", &files)
	if i := strings.Index(line, "insertion"); i >= 0 {
		part := line[:i]
		fields := strings.Fields(part)
		if len(fields) >= 2 {
			fmt.Sscanf(fields[len(fields)-1], "%d", &ins)
		}
	}
	if i := strings.Index(line, "deletion"); i >= 0 {
		part := line[:i]
		fields := strings.Fields(part)
		if len(fields) >= 2 {
			fmt.Sscanf(fields[len(fields)-1], "%d", &del)
		}
	}
	if files == 0 && ins == 0 && del == 0 {
		return ""
	}
	return fmt.Sprintf("%d file(s), +%d/-%d", files, ins, del)
}

// ---- F03: git diff compact (stats + compacted hunks) ----

// TryCompactGitDiff collapses `git diff` stdout: empty → one-liner; non-empty → stats + hunks.
func TryCompactGitDiff(argv []string, stdout []byte) ([]byte, bool) {
	if !isGitDiffArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[git diff] empty\n"), true
	}
	if isGitDiffNameOnlyPathListArgv(argv) {
		return groupPathListResults(stdout, "git diff --name-only")
	}
	if isGitDiffNameStatusPathListArgv(argv) {
		return groupNameStatusPathListResults(stdout, "git diff --name-status")
	}
	if isGitDiffStatArgv(argv) {
		compact := compactGitDiffStat(s)
		if compact == "" || len(compact) >= len(s) {
			return stdout, false
		}
		return []byte(compact), true
	}
	compact := compactGitDiff(s)
	if compact == "" || len(compact) >= len(s) {
		return stdout, false
	}
	return []byte(compact), true
}

// compactGitDiff strips context lines from a unified diff, keeping +/- lines and hunk headers.
func compactGitDiff(s string) string {
	type fileDiff struct {
		path    string // "x"
		added   int
		removed int
		meta    []string
		hunks   []string // @@ + changed lines only
	}

	var files []fileDiff
	var cur fileDiff
	var hasCur bool
	var inHunk bool

	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimRight(raw, "\r")

		if strings.HasPrefix(line, "diff --git ") {
			if hasCur {
				files = append(files, cur)
			}
			// Extract path: "diff --git a/foo/bar.go b/foo/bar.go" → "foo/bar.go"
			cur = fileDiff{path: extractDiffPath(line)}
			hasCur = true
			inHunk = false
			continue
		}

		if !hasCur {
			continue
		}

		if strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") {
			continue
		}
		if isGitDiffMetadataLine(line) {
			cur.meta = append(cur.meta, line)
			continue
		}

		if strings.HasPrefix(line, "@@") {
			inHunk = true
			// Keep only the hunk header, strip trailing function context.
			hdr := line
			if at := strings.LastIndex(line, "@@"); at > 0 && at < len(line)-2 {
				hdr = line[:at+2]
			}
			cur.hunks = append(cur.hunks, hdr)
			continue
		}

		if !inHunk {
			continue
		}

		if strings.HasPrefix(line, "+") {
			cur.added++
			cur.hunks = append(cur.hunks, line)
		} else if strings.HasPrefix(line, "-") {
			cur.removed++
			cur.hunks = append(cur.hunks, line)
		}
		// skip context lines
	}
	if hasCur {
		files = append(files, cur)
	}

	if len(files) == 0 {
		return ""
	}

	// Build compact output.
	var sb strings.Builder
	var totalAdded, totalRemoved int
	for _, f := range files {
		totalAdded += f.added
		totalRemoved += f.removed
	}
	sb.WriteString(fmt.Sprintf("[git diff] %d file(s) +%d/-%d\n", len(files), totalAdded, totalRemoved))
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("  %s (+%d/-%d)\n", f.path, f.added, f.removed))
		for _, m := range f.meta {
			sb.WriteString("    ")
			sb.WriteString(m)
			sb.WriteByte('\n')
		}
		for _, h := range f.hunks {
			sb.WriteString("    ")
			sb.WriteString(h)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

type gitDiffStatRow struct {
	Path string
	Stat string
}

func compactGitDiffStat(s string) string {
	lines := strings.Split(s, "\n")
	rows := make([]gitDiffStatRow, 0, len(lines))
	summary := ""
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if reGitStatSummary.MatchString(line) {
			summary = line
			continue
		}
		path, stat, ok := splitGitDiffStatLine(line)
		if !ok {
			return ""
		}
		rows = append(rows, gitDiffStatRow{Path: path, Stat: stat})
	}
	if len(rows) == 0 {
		return ""
	}
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		paths = append(paths, row.Path)
	}
	prefix := commonDirectoryPrefix(paths)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[git diff --stat] %d file(s)\n", len(rows)))
	if prefix != "" {
		sb.WriteString("[prefix=")
		sb.WriteString(prefix)
		sb.WriteString("]\n")
	}
	for _, row := range rows {
		path := strings.TrimPrefix(row.Path, prefix)
		if path == "" {
			path = row.Path
		}
		sb.WriteString(path)
		sb.WriteString(" | ")
		sb.WriteString(row.Stat)
		sb.WriteByte('\n')
	}
	if summary != "" {
		sb.WriteString("summary: ")
		sb.WriteString(summary)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func splitGitDiffStatLine(line string) (string, string, bool) {
	pipe := strings.LastIndex(line, "|")
	if pipe <= 0 || pipe == len(line)-1 {
		return "", "", false
	}
	path := strings.TrimSpace(line[:pipe])
	stat := strings.TrimSpace(line[pipe+1:])
	if path == "" || stat == "" {
		return "", "", false
	}
	return path, stat, true
}

func groupNameStatusPathListResults(stdout []byte, toolName string) ([]byte, bool) {
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
		status, path, ok := splitGitNameStatusPathLine(line)
		if !ok {
			return stdout, false
		}
		idx := strings.LastIndex(path, "/")
		if idx <= 0 || idx == len(path)-1 {
			return stdout, false
		}
		dir := path[:idx+1]
		base := path[idx+1:]
		if dir != currentDir {
			sb.WriteString(dir)
			sb.WriteByte('\n')
			currentDir = dir
			grouped++
		}
		sb.WriteString("  ")
		sb.WriteString(status)
		sb.WriteByte(' ')
		sb.WriteString(base)
		sb.WriteByte('\n')
	}
	out := sb.String()
	if grouped == 0 || len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func splitGitNameStatusPathLine(line string) (string, string, bool) {
	status, path, ok := strings.Cut(line, "\t")
	if !ok || strings.Contains(path, "\t") {
		return "", "", false
	}
	status = strings.TrimSpace(status)
	if len(status) != 1 || !strings.Contains("AMDTUXB", status) {
		return "", "", false
	}
	if !safePathListLine(path) {
		return "", "", false
	}
	return status, path, true
}

func isGitDiffMetadataLine(line string) bool {
	for _, prefix := range []string{
		"Binary ",
		"new file",
		"deleted file",
		"old mode ",
		"new mode ",
		"rename ",
		"copy ",
		"similarity ",
		"dissimilarity ",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// extractDiffPath extracts the b-path from "diff --git a/foo b/foo".
func extractDiffPath(line string) string {
	// "diff --git a/<path> b/<path>"
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return ""
	}
	bpath := parts[len(parts)-1]
	return strings.TrimPrefix(bpath, "b/")
}

// ---- F04: git show compact (commit header + stat + compacted diff) ----

// TryCompactGitShow compacts `git show` stdout (F04).
func TryCompactGitShow(argv []string, stdout []byte) ([]byte, bool) {
	if !isGitShowArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[git show] empty\n"), true
	}
	if isGitShowNameOnlyPathListArgv(argv) {
		return compactGitShowPathList(stdout, "git show --name-only", false)
	}
	if isGitShowNameStatusPathListArgv(argv) {
		return compactGitShowPathList(stdout, "git show --name-status", true)
	}
	if gitShowMetadataPathListArgPresent(argv) {
		return stdout, false
	}
	compact := compactGitShow(s)
	if compact == "" || len(compact) >= len(s) {
		return stdout, false
	}
	return []byte(compact), true
}

// reGitShowHash matches a standalone commit hash line.
var reGitShowHash = regexp.MustCompile(`^commit ([0-9a-f]{7,40})`)

// compactGitShow extracts the commit header + stat + calls compactGitDiff on the diff section.
func compactGitShow(s string) string {
	var hash, subject, statSummary string
	var statLines []string
	var diffStart int
	lines := strings.Split(s, "\n")
	var subjectFound bool
	var pastHeader bool

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if m := reGitShowHash.FindStringSubmatch(line); m != nil {
			hash = m[1][:7]
			continue
		}
		if strings.HasPrefix(line, "Author:") || strings.HasPrefix(line, "Date:") ||
			strings.HasPrefix(line, "Commit:") || strings.HasPrefix(line, "Merge:") {
			continue
		}
		if trimmed == "" {
			pastHeader = true
			continue
		}
		if pastHeader && !subjectFound && !strings.HasPrefix(line, "diff ") {
			subject = trimmed
			subjectFound = true
			continue
		}
		if reGitStatSummary.MatchString(trimmed) {
			statSummary = parseGitStatSummary(trimmed)
			statLines = append(statLines, trimmed)
			continue
		}
		if subjectFound {
			if _, _, ok := splitGitDiffStatLine(trimmed); ok {
				statLines = append(statLines, trimmed)
				continue
			}
		}
		if strings.HasPrefix(line, "diff --git ") {
			diffStart = i
			break
		}
	}

	if hash == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[git show] %s", hash))
	if subject != "" {
		sb.WriteString(fmt.Sprintf(" %s", subject))
	}
	if statSummary != "" {
		sb.WriteString(fmt.Sprintf(" [%s]", statSummary))
	}
	sb.WriteByte('\n')

	if len(statLines) > 0 {
		if statCompact := compactGitDiffStat(strings.Join(statLines, "\n")); statCompact != "" {
			statCompact = strings.Replace(statCompact, "[git diff --stat]", "[git show --stat]", 1)
			sb.WriteString(statCompact)
		}
	}

	// Compact the diff portion if present.
	if diffStart > 0 && diffStart < len(lines) {
		diffSection := strings.Join(lines[diffStart:], "\n")
		if diffCompact := compactGitDiff(diffSection); diffCompact != "" {
			sb.WriteString(diffCompact)
		}
	}

	return sb.String()
}

func compactGitShowPathList(stdout []byte, toolName string, nameStatus bool) ([]byte, bool) {
	header, paths, ok := splitGitShowHeaderAndPaths(string(stdout), nameStatus)
	if !ok {
		return stdout, false
	}
	var compactedPaths []byte
	if nameStatus {
		compactedPaths, ok = groupNameStatusPathListResults([]byte(strings.Join(paths, "\n")), toolName)
	} else {
		compactedPaths, ok = groupPathListResults([]byte(strings.Join(paths, "\n")), toolName)
	}
	if !ok {
		return stdout, false
	}
	out := header + string(compactedPaths)
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func splitGitShowHeaderAndPaths(stdout string, nameStatus bool) (string, []string, bool) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var hash, subject string
	var pastHeader, subjectFound bool
	var paths []string
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if subjectFound {
				continue
			}
			pastHeader = true
			continue
		}
		if m := reGitShowHash.FindStringSubmatch(line); m != nil {
			hash = m[1][:7]
			continue
		}
		if strings.HasPrefix(line, "Author:") || strings.HasPrefix(line, "Date:") ||
			strings.HasPrefix(line, "Commit:") || strings.HasPrefix(line, "Merge:") {
			continue
		}
		if pastHeader && !subjectFound {
			subject = trimmed
			subjectFound = true
			continue
		}
		if !subjectFound {
			continue
		}
		if nameStatus {
			if _, _, ok := splitGitNameStatusPathLine(line); !ok {
				return "", nil, false
			}
		} else if !safePathListLine(line) {
			return "", nil, false
		}
		paths = append(paths, line)
	}
	if hash == "" || len(paths) == 0 {
		return "", nil, false
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[git show] %s", hash))
	if subject != "" {
		sb.WriteByte(' ')
		sb.WriteString(subject)
	}
	sb.WriteByte('\n')
	return sb.String(), paths, true
}

// Porcelain v1: two status columns (see git-status short) or untracked ??.
var porcelainLine = regexp.MustCompile(`^(?:\?\?|[MADRCU?! ][MADRCU?! ])\s+\S+`)

// TryCompactGitStatus compresses porcelain-style `git status` output to one summary line.
func TryCompactGitStatus(argv []string, stdout []byte) ([]byte, bool) {
	if !isGitStatusArgv(argv) {
		return stdout, false
	}
	// Trim only trailing newlines — leading spaces are part of porcelain column 1.
	s := strings.TrimRight(string(stdout), "\r\n")
	if strings.TrimSpace(s) == "" {
		return []byte("[git status] clean\n"), true
	}
	lines := strings.Split(s, "\n")
	var pathLines []string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "##") {
			continue // branch / upstream line from git status -sb
		}
		if strings.HasPrefix(line, "!!") {
			continue // ignored paths — skip (still porcelain-safe)
		}
		if !porcelainLine.MatchString(line) {
			return stdout, false
		}
		pathLines = append(pathLines, line)
	}
	if len(pathLines) == 0 {
		return []byte("[git status] clean\n"), true
	}
	var untracked, staged, worktree, renamed, conflicts int
	for _, line := range pathLines {
		if strings.HasPrefix(line, "??") {
			untracked++
			continue
		}
		// Conflict codes: DD, AU, UD, UA, DU, AA, UU
		if len(line) >= 2 && (line[0] == 'U' || line[1] == 'U' ||
			(line[0] == 'A' && line[1] == 'A') || (line[0] == 'D' && line[1] == 'D')) {
			conflicts++
			continue
		}
		if line[0] == 'R' || line[0] == 'C' {
			renamed++
		}
		if line[0] != ' ' {
			staged++
		}
		if line[1] != ' ' {
			worktree++
		}
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("staged:%d", staged))
	parts = append(parts, fmt.Sprintf("worktree:%d", worktree))
	parts = append(parts, fmt.Sprintf("untracked:%d", untracked))
	if renamed > 0 {
		parts = append(parts, fmt.Sprintf("renamed:%d", renamed))
	}
	if conflicts > 0 {
		parts = append(parts, fmt.Sprintf("conflicts:%d", conflicts))
	}
	out := fmt.Sprintf("[git status] %d paths (%s)\n",
		len(pathLines), strings.Join(parts, " "))
	return []byte(out), true
}

// TryCompactGitLsFiles summarizes plain path-list output from `git ls-files`.
func TryCompactGitLsFiles(argv []string, stdout []byte) ([]byte, bool) {
	if !isGitLsFilesPathListArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[git ls-files] no paths\n"), true
	}
	return groupPathListResults(stdout, "git ls-files")
}

func isGitStatusArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	if filepath.Base(argv[0]) != "git" {
		return false
	}
	for _, a := range argv[1:] {
		if a == "status" {
			return true
		}
	}
	return false
}

func isGitLsFilesPathListArgv(argv []string) bool {
	idx := gitSubcommandIndex(argv, "ls-files")
	if idx < 0 {
		return false
	}
	for i := idx + 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == "" {
			return false
		}
		if arg == "--" {
			for _, rest := range argv[i+1:] {
				if strings.TrimSpace(rest) == "" {
					return false
				}
			}
			return true
		}
		if strings.HasPrefix(arg, "--") {
			switch arg {
			case "--cached", "--deleted", "--modified", "--others", "--exclude-standard",
				"--deduplicate", "--full-name", "--directory", "--no-empty-directory",
				"--recurse-submodules":
				continue
			default:
				return false
			}
		}
		if strings.HasPrefix(arg, "-") {
			if !gitLsFilesShortFlagsSafe(arg) {
				return false
			}
			continue
		}
	}
	return true
}

func gitLsFilesShortFlagsSafe(arg string) bool {
	if len(arg) < 2 || !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return false
	}
	for _, r := range arg[1:] {
		switch r {
		case 'c', 'd', 'm', 'o':
			continue
		default:
			return false
		}
	}
	return true
}

func isGitArgv(argv []string) bool {
	return len(argv) >= 2 && filepath.Base(argv[0]) == "git"
}

func gitSubcommandIndex(argv []string, subcommand string) int {
	if !isGitArgv(argv) {
		return -1
	}
	for i := 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == subcommand {
			return i
		}
		switch {
		case arg == "-C" || arg == "-c" || arg == "--git-dir" || arg == "--work-tree" ||
			arg == "--namespace" || arg == "--exec-path":
			i++
			if i >= len(argv) {
				return -1
			}
		case strings.HasPrefix(arg, "--git-dir="), strings.HasPrefix(arg, "--work-tree="),
			strings.HasPrefix(arg, "--namespace="), strings.HasPrefix(arg, "--exec-path="),
			strings.HasPrefix(arg, "-c"):
			continue
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return -1
		}
	}
	return -1
}

func isGitLogArgv(argv []string) bool {
	if !isGitArgv(argv) {
		return false
	}
	for _, a := range argv[1:] {
		if a == "log" {
			return true
		}
	}
	return false
}

func isGitDiffArgv(argv []string) bool {
	if !isGitArgv(argv) {
		return false
	}
	for _, a := range argv[1:] {
		if a == "diff" {
			return true
		}
	}
	return false
}

func isGitDiffStatArgv(argv []string) bool {
	if !isGitDiffArgv(argv) {
		return false
	}
	for _, arg := range argv[1:] {
		if arg == "--stat" || strings.HasPrefix(arg, "--stat=") {
			return true
		}
	}
	return false
}

func isGitDiffNameOnlyPathListArgv(argv []string) bool {
	idx := gitSubcommandIndex(argv, "diff")
	if idx < 0 {
		return false
	}
	hasNameOnly := false
	for i := idx + 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == "" {
			return false
		}
		if arg == "--" {
			for _, rest := range argv[i+1:] {
				if strings.TrimSpace(rest) == "" {
					return false
				}
			}
			return hasNameOnly
		}
		if strings.HasPrefix(arg, "--") {
			switch {
			case arg == "--name-only":
				hasNameOnly = true
			case arg == "--cached" || arg == "--staged" || arg == "--no-renames" ||
				arg == "--relative" || strings.HasPrefix(arg, "--relative=") ||
				strings.HasPrefix(arg, "--find-renames") || strings.HasPrefix(arg, "--find-copies") ||
				strings.HasPrefix(arg, "--diff-filter="):
			case arg == "--diff-filter":
				i++
				if i >= len(argv) || strings.TrimSpace(argv[i]) == "" {
					return false
				}
			default:
				return false
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			switch {
			case arg == "-M" || arg == "-C":
				continue
			case strings.HasPrefix(arg, "-M") || strings.HasPrefix(arg, "-C"):
				continue
			default:
				return false
			}
		}
	}
	return hasNameOnly
}

func isGitDiffNameStatusPathListArgv(argv []string) bool {
	idx := gitSubcommandIndex(argv, "diff")
	if idx < 0 {
		return false
	}
	hasNameStatus := false
	for i := idx + 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == "" {
			return false
		}
		if arg == "--" {
			for _, rest := range argv[i+1:] {
				if strings.TrimSpace(rest) == "" {
					return false
				}
			}
			return hasNameStatus
		}
		if strings.HasPrefix(arg, "--") {
			switch {
			case arg == "--name-status":
				hasNameStatus = true
			case arg == "--cached" || arg == "--staged" || arg == "--no-renames" ||
				arg == "--relative" || strings.HasPrefix(arg, "--relative=") ||
				strings.HasPrefix(arg, "--find-renames") || strings.HasPrefix(arg, "--find-copies") ||
				strings.HasPrefix(arg, "--diff-filter="):
			case arg == "--diff-filter":
				i++
				if i >= len(argv) || strings.TrimSpace(argv[i]) == "" {
					return false
				}
			default:
				return false
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			switch {
			case arg == "-M" || arg == "-C":
				continue
			case strings.HasPrefix(arg, "-M") || strings.HasPrefix(arg, "-C"):
				continue
			default:
				return false
			}
		}
	}
	return hasNameStatus
}

func isGitShowNameOnlyPathListArgv(argv []string) bool {
	return isGitShowSingleMetadataPathListArgv(argv, "--name-only")
}

func isGitShowNameStatusPathListArgv(argv []string) bool {
	return isGitShowSingleMetadataPathListArgv(argv, "--name-status")
}

func gitShowMetadataPathListArgPresent(argv []string) bool {
	idx := gitSubcommandIndex(argv, "show")
	if idx < 0 {
		return false
	}
	for _, arg := range argv[idx+1:] {
		if arg == "--name-only" || arg == "--name-status" {
			return true
		}
	}
	return false
}

func isGitShowSingleMetadataPathListArgv(argv []string, mode string) bool {
	idx := gitSubcommandIndex(argv, "show")
	if idx < 0 {
		return false
	}
	hasMode := false
	for i := idx + 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == "" {
			return false
		}
		if arg == "--" {
			for _, rest := range argv[i+1:] {
				if strings.TrimSpace(rest) == "" {
					return false
				}
			}
			return hasMode
		}
		if strings.HasPrefix(arg, "--") {
			switch {
			case arg == mode:
				hasMode = true
			case arg == "--name-only" || arg == "--name-status" || arg == "--stat" ||
				strings.HasPrefix(arg, "--stat=") || arg == "--numstat" || arg == "--raw" ||
				arg == "--shortstat" || arg == "--patch" || arg == "--patch-with-stat" ||
				arg == "--word-diff" || strings.HasPrefix(arg, "--word-diff="):
				return false
			case strings.HasPrefix(arg, "--format="), strings.HasPrefix(arg, "--pretty="),
				strings.HasPrefix(arg, "--find-renames"), strings.HasPrefix(arg, "--find-copies"),
				strings.HasPrefix(arg, "--diff-filter="):
				continue
			case arg == "--format" || arg == "--pretty" || arg == "--diff-filter":
				i++
				if i >= len(argv) || strings.TrimSpace(argv[i]) == "" {
					return false
				}
			default:
				continue
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			switch {
			case arg == "-M" || arg == "-C":
				continue
			case strings.HasPrefix(arg, "-M") || strings.HasPrefix(arg, "-C"):
				continue
			case arg == "-p" || arg == "-z" || strings.HasPrefix(arg, "-U"):
				return false
			default:
				continue
			}
		}
	}
	return hasMode
}

func isGitShowArgv(argv []string) bool {
	if !isGitArgv(argv) {
		return false
	}
	for _, a := range argv[1:] {
		if a == "show" {
			return true
		}
	}
	return false
}

var gitF05Subcommands = map[string]struct{}{
	"add": {}, "commit": {}, "push": {}, "pull": {}, "fetch": {}, "branch": {},
	"merge": {}, "rebase": {},
}

// gitF05Subcommand returns the first F05 subcommand token in argv (e.g. add, push).
func gitF05Subcommand(argv []string) string {
	if !isGitArgv(argv) {
		return ""
	}
	for _, a := range argv[1:] {
		if _, ok := gitF05Subcommands[a]; ok {
			return a
		}
	}
	return ""
}

// rePushRefUpdate matches "abc1234..def5678  branch -> remote/branch" lines in git push output.
var rePushRefUpdate = regexp.MustCompile(`[0-9a-f]{4,40}\.\.[0-9a-f]{4,40}\s+\S+\s*->\s*\S+`)

// rePushNewBranch matches "* [new branch]   branch -> origin/branch" lines.
var rePushNewBranch = regexp.MustCompile(`\*\s+\[new branch\]`)

// reFetchUpdate matches "   abc1234..def5678  branch -> origin/branch" fetch output.
var reFetchUpdate = regexp.MustCompile(`\s+[0-9a-f]{4,40}\.\.[0-9a-f]{4,40}\s+\S+\s*->\s*\S+`)

// reFetchNew matches "* [new branch]" or "* [new tag]" lines in fetch output.
var reFetchNew = regexp.MustCompile(`\*\s+\[(new branch|new tag)\]`)

// TryCompactGitF05 summarizes noisy-but-successful git add/commit/push/pull/fetch/branch stdout (F05).
func TryCompactGitF05(argv []string, stdout []byte) ([]byte, bool) {
	sub := gitF05Subcommand(argv)
	if sub == "" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[git %s] ok\n", sub)), true
	}
	low := strings.ToLower(s)
	switch sub {
	case "push":
		if strings.Contains(low, "everything up-to-date") {
			return []byte("[git push] up to date\n"), true
		}
		// Successful push: extract ref update lines
		if out := compactGitPushOutput(s); out != "" {
			return []byte(out), true
		}
	case "pull", "fetch":
		if strings.Contains(low, "already up to date") {
			return []byte(fmt.Sprintf("[git %s] up to date\n", sub)), true
		}
		// Successful fetch/pull: count updates and new branches/tags
		if out := compactGitFetchOutput(s, sub); out != "" {
			return []byte(out), true
		}
	case "merge":
		if strings.Contains(low, "already up to date") {
			return []byte("[git merge] up to date\n"), true
		}
		// Fast-forward merge
		if strings.Contains(low, "fast-forward") || strings.Contains(low, "fast forward") {
			if out := extractMergeStatLine(s); out != "" {
				return []byte(fmt.Sprintf("[git merge] fast-forward (%s)\n", out)), true
			}
			return []byte("[git merge] fast-forward\n"), true
		}
	case "rebase":
		if strings.Contains(low, "current branch") && strings.Contains(low, "up to date") {
			return []byte("[git rebase] up to date\n"), true
		}
		if strings.Contains(low, "successfully rebased") {
			return []byte("[git rebase] ok\n"), true
		}
	}
	return stdout, false
}

// compactGitPushOutput extracts ref update lines from git push output.
// Returns "" if nothing useful found or compact is not shorter.
func compactGitPushOutput(s string) string {
	var updates []string
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if rePushRefUpdate.MatchString(t) {
			updates = append(updates, t)
		} else if rePushNewBranch.MatchString(t) {
			updates = append(updates, t)
		}
	}
	if len(updates) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[git push] %d ref(s) updated\n", len(updates)))
	for _, u := range updates {
		sb.WriteString("  " + u + "\n")
	}
	result := sb.String()
	if len(result) >= len(s) {
		return ""
	}
	return result
}

// compactGitFetchOutput summarizes fetch/pull output with count of updates and new refs.
func compactGitFetchOutput(s, sub string) string {
	var updates, newRefs int
	for _, line := range strings.Split(s, "\n") {
		if reFetchUpdate.MatchString(line) {
			updates++
		} else if reFetchNew.MatchString(line) {
			newRefs++
		}
	}
	if updates == 0 && newRefs == 0 {
		return ""
	}
	parts := []string{}
	if updates > 0 {
		parts = append(parts, fmt.Sprintf("%d updated", updates))
	}
	if newRefs > 0 {
		parts = append(parts, fmt.Sprintf("%d new", newRefs))
	}
	return fmt.Sprintf("[git %s] %s\n", sub, strings.Join(parts, ", "))
}

// extractMergeStatLine finds the "N files changed" summary from merge output.
func extractMergeStatLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if reGitStatSummary.MatchString(t) {
			return parseGitStatSummary(t)
		}
	}
	return ""
}

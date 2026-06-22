package filter

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// TryCompactLs summarizes empty stdout from `ls`. Non-empty directory listings
// full-pass because file names are the evidence the model asked for.
func TryCompactLs(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "ls" && b != "ls.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[ls] empty\n"), true
	}
	if lsOnlyTotalLines(s) {
		return []byte("[ls] empty\n"), true
	}
	return stdout, false
}

type lsLongRow struct {
	Perms string
	Links string
	Owner string
	Group string
	Size  string
	Month string
	Day   string
	Time  string
	Name  string
}

// TryCompactLsLong compacts parser-proven `ls -l` style output while keeping
// every visible entry name and metadata row. It is stricter than TryCompactLs
// and is intended for archive-backed command-output-first use, not WSS
// downstream mutation.
func TryCompactLsLong(argv []string, stdout []byte) ([]byte, bool) {
	if !LsLongOutputEligibleArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return stdout, false
	}
	total, rows, ok := parseLsLongRows(s)
	if !ok || len(rows) < 8 {
		return stdout, false
	}
	out := formatLsLongRows(total, rows)
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

// TryCompactTree compacts parser-proven, bounded `tree` listings while keeping
// every visible entry as a path. Unknown shapes fail open because tree output is
// often used as repository reality.
func TryCompactTree(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "tree" && b != "tree.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[tree] empty\n"), true
	}
	if !treeOutputEligibleArgv(argv) {
		return stdout, false
	}
	listing, ok := parseTreeListing(s)
	if !ok || len(listing.Paths) < 8 {
		return stdout, false
	}
	out := formatTreeListing(listing)
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

type treeListing struct {
	Root  string
	Dirs  int
	Files int
	Paths []string
}

func treeOutputEligibleArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.TrimSpace(argv[0])), ".exe"))
	if base != "tree" {
		return false
	}
	sawDepth := false
	for i := 1; i < len(argv); i++ {
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
			return sawDepth
		}
		if strings.HasPrefix(arg, "--") {
			switch {
			case arg == "--dirsfirst":
				continue
			case arg == "--charset":
				i++
				if i >= len(argv) || strings.TrimSpace(argv[i]) == "" {
					return false
				}
				continue
			case strings.HasPrefix(arg, "--charset="):
				if strings.TrimSpace(strings.TrimPrefix(arg, "--charset=")) == "" {
					return false
				}
				continue
			default:
				return false
			}
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			for j := 1; j < len(arg); j++ {
				switch arg[j] {
				case 'a', 'd':
					continue
				case 'L':
					depth := strings.TrimSpace(arg[j+1:])
					if depth == "" {
						i++
						if i >= len(argv) {
							return false
						}
						depth = strings.TrimSpace(argv[i])
					}
					if !treeBoundedDepthArg(depth) {
						return false
					}
					sawDepth = true
					j = len(arg)
				default:
					return false
				}
			}
		}
	}
	return sawDepth
}

func treeBoundedDepthArg(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(raw)
	return err == nil && n > 0 && n <= 6
}

func parseTreeListing(s string) (treeListing, bool) {
	lines := strings.Split(s, "\n")
	var listing treeListing
	stack := make([]string, 0, 8)
	sawSummary := false

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.ContainsRune(line, '\x00') || strings.ContainsRune(line, '\x1b') || containsControl(line) {
			return treeListing{}, false
		}
		if dirs, files, ok := parseTreeSummaryLine(line); ok {
			if sawSummary {
				return treeListing{}, false
			}
			listing.Dirs = dirs
			listing.Files = files
			sawSummary = true
			continue
		}
		if sawSummary {
			return treeListing{}, false
		}
		if listing.Root == "" {
			if _, _, ok := parseTreeEntryLine(line); ok || strings.TrimSpace(line) != line {
				return treeListing{}, false
			}
			listing.Root = line
			continue
		}
		level, name, ok := parseTreeEntryLine(line)
		if !ok || name == "." || name == ".." || strings.TrimSpace(name) != name {
			return treeListing{}, false
		}
		if level > len(stack)+1 {
			return treeListing{}, false
		}
		stack = stack[:level-1]
		path := treeEntryPath(listing.Root, stack, name)
		if path == "" || containsControl(path) {
			return treeListing{}, false
		}
		listing.Paths = append(listing.Paths, path)
		stack = append(stack, name)
	}

	if listing.Root == "" || !sawSummary || len(listing.Paths) == 0 {
		return treeListing{}, false
	}
	total := listing.Dirs + listing.Files
	if total <= 0 || (len(listing.Paths) != total && len(listing.Paths) != total-1) {
		return treeListing{}, false
	}
	return listing, true
}

func parseTreeSummaryLine(line string) (int, int, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) != 4 {
		return 0, 0, false
	}
	if fields[1] != "directory," && fields[1] != "directories," {
		return 0, 0, false
	}
	if fields[3] != "file" && fields[3] != "files" {
		return 0, 0, false
	}
	dirs, err := strconv.Atoi(fields[0])
	if err != nil || dirs < 0 {
		return 0, 0, false
	}
	files, err := strconv.Atoi(fields[2])
	if err != nil || files < 0 {
		return 0, 0, false
	}
	return dirs, files, true
}

func parseTreeEntryLine(line string) (int, string, bool) {
	connectors := []string{"├── ", "└── ", "|-- ", "`-- ", "\\-- "}
	for _, connector := range connectors {
		before, after, ok := strings.Cut(line, connector)
		if !ok {
			continue
		}
		prefix := before
		depth, ok := treePrefixDepth(prefix)
		if !ok {
			return 0, "", false
		}
		name := after
		if name == "" {
			return 0, "", false
		}
		return depth + 1, name, true
	}
	return 0, "", false
}

func treePrefixDepth(prefix string) (int, bool) {
	depth := 0
	for prefix != "" {
		switch {
		case strings.HasPrefix(prefix, "│   "):
			prefix = strings.TrimPrefix(prefix, "│   ")
		case strings.HasPrefix(prefix, "│\u00a0\u00a0 "):
			prefix = strings.TrimPrefix(prefix, "│\u00a0\u00a0 ")
		case strings.HasPrefix(prefix, "|   "):
			prefix = strings.TrimPrefix(prefix, "|   ")
		case strings.HasPrefix(prefix, "|\u00a0\u00a0 "):
			prefix = strings.TrimPrefix(prefix, "|\u00a0\u00a0 ")
		case strings.HasPrefix(prefix, "    "):
			prefix = strings.TrimPrefix(prefix, "    ")
		default:
			return 0, false
		}
		depth++
	}
	return depth, true
}

func treeEntryPath(root string, stack []string, name string) string {
	parts := make([]string, 0, len(stack)+1)
	for _, part := range stack {
		parts = append(parts, strings.TrimSuffix(part, "/"))
	}
	parts = append(parts, name)
	joined := strings.Join(parts, "/")
	switch root {
	case ".", "./":
		return joined
	default:
		return strings.TrimRight(root, "/") + "/" + joined
	}
}

func formatTreeListing(listing treeListing) string {
	var sb strings.Builder
	sb.WriteString("[tree paths] ")
	sb.WriteString(strconv.Itoa(len(listing.Paths)))
	sb.WriteString(" entries ")
	sb.WriteString(strconv.Itoa(listing.Dirs))
	sb.WriteString(" directories ")
	sb.WriteString(strconv.Itoa(listing.Files))
	sb.WriteString(" files root=")
	sb.WriteString(listing.Root)
	sb.WriteByte('\n')
	currentDir := ""
	for _, path := range listing.Paths {
		idx := strings.LastIndex(path, "/")
		dir := "./"
		base := path
		if idx >= 0 {
			dir = path[:idx+1]
			base = path[idx+1:]
		}
		if dir != currentDir {
			sb.WriteString(dir)
			sb.WriteByte('\n')
			currentDir = dir
		}
		sb.WriteString("  ")
		sb.WriteString(base)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// LsLongOutputEligibleArgv reports whether argv is a safe `ls -l` style shape
// for parser-proven long-listing compaction.
func LsLongOutputEligibleArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.TrimSpace(argv[0])), ".exe"))
	if base != "ls" {
		return false
	}
	hasLong := false
	optionParsing := true
	for i := 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == "" {
			return false
		}
		if optionParsing && arg == "--" {
			optionParsing = false
			continue
		}
		if !optionParsing || !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		switch {
		case arg == "--all" || arg == "--almost-all" || arg == "--human-readable" ||
			arg == "--reverse" || arg == "--directory" || arg == "--group-directories-first":
			continue
		case arg == "--color=never" || strings.HasPrefix(arg, "--sort=") ||
			strings.HasPrefix(arg, "--time-style="):
			continue
		case strings.HasPrefix(arg, "--"):
			return false
		}
		for _, ch := range arg[1:] {
			switch ch {
			case 'l':
				hasLong = true
			case 'a', 'A', 'h', 't', 'r', 'S', '1', 'd':
			default:
				return false
			}
		}
	}
	return hasLong
}

func parseLsLongRows(s string) (string, []lsLongRow, bool) {
	lines := strings.Split(s, "\n")
	rows := make([]lsLongRow, 0, len(lines))
	total := ""
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.ContainsRune(line, '\x1b') || strings.ContainsRune(line, '\x00') {
			return "", nil, false
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "total ") {
			if total != "" && total != trimmed {
				return "", nil, false
			}
			total = trimmed
			continue
		}
		fields, rest, ok := splitLeadingFields(trimmed, 8)
		if !ok || rest == "" || strings.TrimSpace(rest) != rest {
			return "", nil, false
		}
		if !lsPermsField(fields[0]) || !allDigits(fields[1]) || !lsSizeField(fields[4]) ||
			containsControl(rest) {
			return "", nil, false
		}
		rows = append(rows, lsLongRow{
			Perms: fields[0],
			Links: fields[1],
			Owner: fields[2],
			Group: fields[3],
			Size:  fields[4],
			Month: fields[5],
			Day:   fields[6],
			Time:  fields[7],
			Name:  rest,
		})
	}
	return total, rows, len(rows) > 0
}

func splitLeadingFields(line string, n int) ([]string, string, bool) {
	fields := make([]string, 0, n)
	i := 0
	for len(fields) < n {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		start := i
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		if start == i {
			return nil, "", false
		}
		fields = append(fields, line[start:i])
	}
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) {
		return nil, "", false
	}
	return fields, line[i:], true
}

func formatLsLongRows(total string, rows []lsLongRow) string {
	owner, group := commonLsOwnerGroup(rows)
	var sb strings.Builder
	sb.WriteString("[ls -l] ")
	sb.WriteString(strconv.Itoa(len(rows)))
	sb.WriteString(" entr")
	if len(rows) == 1 {
		sb.WriteByte('y')
	} else {
		sb.WriteString("ies")
	}
	if total != "" {
		sb.WriteByte(' ')
		sb.WriteString(total)
	}
	if owner != "" && group != "" {
		sb.WriteString(" owner=")
		sb.WriteString(owner)
		sb.WriteString(" group=")
		sb.WriteString(group)
	}
	sb.WriteByte('\n')
	for _, row := range rows {
		sb.WriteString(row.Perms)
		sb.WriteByte(' ')
		sb.WriteString(row.Links)
		sb.WriteByte(' ')
		if owner == "" || group == "" {
			sb.WriteString(row.Owner)
			sb.WriteByte(':')
			sb.WriteString(row.Group)
			sb.WriteByte(' ')
		}
		sb.WriteString(row.Size)
		sb.WriteByte(' ')
		sb.WriteString(row.Month)
		sb.WriteByte(' ')
		sb.WriteString(row.Day)
		sb.WriteByte(' ')
		sb.WriteString(row.Time)
		sb.WriteByte(' ')
		sb.WriteString(row.Name)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func commonLsOwnerGroup(rows []lsLongRow) (string, string) {
	if len(rows) == 0 {
		return "", ""
	}
	owner := rows[0].Owner
	group := rows[0].Group
	if owner == "" || group == "" {
		return "", ""
	}
	for _, row := range rows[1:] {
		if row.Owner != owner || row.Group != group {
			return "", ""
		}
	}
	return owner, group
}

func lsPermsField(field string) bool {
	if len(field) < 10 || len(field) > 11 {
		return false
	}
	switch field[0] {
	case '-', 'd', 'l', 'c', 'b', 'p', 's':
	default:
		return false
	}
	for _, r := range field {
		if r < 32 || r == 127 || r == '\x1b' {
			return false
		}
	}
	return true
}

func lsSizeField(field string) bool {
	if field == "" {
		return false
	}
	for _, r := range field {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' || r == 'K' || r == 'M' || r == 'G' ||
			r == 'T' || r == 'P' || r == 'E' || r == 'B' {
			continue
		}
		return false
	}
	return true
}

func containsControl(s string) bool {
	for _, r := range s {
		if r < 32 || r == 127 {
			return true
		}
	}
	return false
}

// TryCompactPathListOutput compacts commands whose stdout is a deterministic
// newline-delimited file list, not search match output.
func TryCompactPathListOutput(argv []string, stdout []byte) ([]byte, bool) {
	if !pathListOutputEligibleArgv(argv) {
		return stdout, false
	}
	return groupPathListResults(stdout, pathListOutputLabel(argv))
}

// TryCompactPlainPathListOutput compacts metadata-less newline-delimited path
// lists with a neutral label. It is deliberately stricter than command-bound
// path-list reducers because no tool command is available to disambiguate
// search output, diagnostics, or prose.
func TryCompactPlainPathListOutput(stdout []byte) ([]byte, bool) {
	if !plainPathListPayloadSafe(stdout) {
		return stdout, false
	}
	return groupPathListResults(stdout, "plain")
}

// PathListOutputReducerEligibleFromCommandLine reports whether commandLine can
// use the path-list reducer without being treated as grep/search-match output.
func PathListOutputReducerEligibleFromCommandLine(commandLine string) bool {
	for _, candidate := range []string{commandLine, NormalizePathListCommandLine(commandLine, "")} {
		if candidate == "" {
			continue
		}
		if pathListOutputEligibleArgv(primaryArgvForCapturedOutput(candidate)) {
			return true
		}
	}
	return false
}

// PathListOutputReducerEligibleArgv reports whether argv can use the path-list
// reducer without being treated as grep/search-match output.
func PathListOutputReducerEligibleArgv(argv []string) bool {
	return pathListOutputEligibleArgv(argv)
}

// NormalizePathListCommandLine returns the inner command for supported
// `cd <abs> && <path-list command>` wrappers. Path-list reducers only need the
// command shape; stdout still carries the actual paths.
func NormalizePathListCommandLine(commandLine, workdir string) string {
	_ = workdir
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return ""
	}
	if _, inner, ok := splitLeadingCDSearch(commandLine); ok {
		commandLine = inner
	}
	if !pathListOutputEligibleArgv(primaryArgvForCapturedOutput(commandLine)) {
		return ""
	}
	return commandLine
}

func pathListOutputEligibleArgv(argv []string) bool {
	return ripgrepFilesArgv(argv) || fdPathListArgv(argv) || findPathListArgv(argv)
}

func pathListOutputLabel(argv []string) string {
	if ripgrepFilesArgv(argv) {
		return "rg --files"
	}
	if fdPathListArgv(argv) {
		return "fd"
	}
	if findPathListArgv(argv) {
		return "find"
	}
	return "paths"
}

func plainPathListPayloadSafe(stdout []byte) bool {
	const (
		maxBytes     = 128 * 1024
		maxEntries   = 2500
		maxLineBytes = 512
	)
	payload := string(stdout)
	if len(stdout) == 0 || len(stdout) > maxBytes || strings.ContainsRune(payload, '\x00') {
		return false
	}
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return false
	}
	entries := 0
	for raw := range strings.SplitSeq(strings.TrimRight(payload, "\n"), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || len(line) > maxLineBytes || strings.ContainsAny(line, " \t:;|<>\"'`$\\") ||
			strings.Contains(line, "://") || strings.HasPrefix(line, "-") || strings.TrimSpace(line) != line {
			return false
		}
		for _, r := range line {
			if r < 32 || r == 127 {
				return false
			}
		}
		entries++
		if entries > maxEntries {
			return false
		}
	}
	return entries > 0
}

func ripgrepFilesArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.TrimSpace(argv[0])), ".exe"))
	if base != "rg" && base != "ripgrep" {
		return false
	}
	sawFiles := false
	for i := 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == "" {
			return false
		}
		if arg == "--" {
			return sawFiles
		}
		switch {
		case arg == "--files":
			sawFiles = true
		case ripgrepFilesBoolFlag(arg):
		case ripgrepFilesValueFlag(arg):
			i++
			if i >= len(argv) || strings.TrimSpace(argv[i]) == "" {
				return false
			}
		case ripgrepFilesInlineValueFlag(arg):
		case strings.HasPrefix(arg, "-"):
			return false
		default:
			// Explicit search roots/path args keep stdout a path list.
		}
	}
	return sawFiles
}

func ripgrepFilesBoolFlag(arg string) bool {
	switch arg {
	case "--hidden", "--no-hidden", "--follow", "-L",
		"--no-ignore", "--no-ignore-vcs", "--no-ignore-dot",
		"--no-ignore-exclude", "--no-ignore-files",
		"--one-file-system", "-u", "-uu", "-uuu":
		return true
	default:
		return false
	}
}

func ripgrepFilesValueFlag(arg string) bool {
	switch arg {
	case "-g", "--glob", "--iglob", "-t", "--type", "-T", "--type-not",
		"--max-depth", "--ignore-file", "--sort", "--sortr":
		return true
	default:
		return false
	}
}

func ripgrepFilesInlineValueFlag(arg string) bool {
	switch {
	case strings.HasPrefix(arg, "--glob="),
		strings.HasPrefix(arg, "--iglob="),
		strings.HasPrefix(arg, "--type="),
		strings.HasPrefix(arg, "--type-not="),
		strings.HasPrefix(arg, "--max-depth="),
		strings.HasPrefix(arg, "--ignore-file="),
		strings.HasPrefix(arg, "--sort="),
		strings.HasPrefix(arg, "--sortr="):
		_, value, _ := strings.Cut(arg, "=")
		return strings.TrimSpace(value) != ""
	case strings.HasPrefix(arg, "-g"), strings.HasPrefix(arg, "-t"), strings.HasPrefix(arg, "-T"):
		return len(arg) > 2 && strings.TrimSpace(arg[2:]) != ""
	default:
		return false
	}
}

func fdPathListArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.TrimSpace(argv[0])), ".exe"))
	if base != "fd" && base != "fdfind" {
		return false
	}
	optionParsing := true
	for i := 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == "" {
			return false
		}
		if optionParsing && arg == "--" {
			optionParsing = false
			continue
		}
		if !optionParsing || !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		switch {
		case fdPathListBoolFlag(arg):
		case fdPathListValueFlag(arg):
			i++
			if i >= len(argv) || strings.TrimSpace(argv[i]) == "" {
				return false
			}
		case fdPathListInlineValueFlag(arg):
		case fdPathListCombinedBoolShortFlag(arg):
		case strings.HasPrefix(arg, "-"):
			return false
		}
	}
	return true
}

func fdPathListBoolFlag(arg string) bool {
	switch arg {
	case "--hidden", "-H",
		"--no-hidden",
		"--follow", "-L",
		"--unrestricted", "-u", "-uu", "-uuu",
		"--no-ignore", "-I", "--no-ignore-vcs", "--no-ignore-parent",
		"--absolute-path", "-a",
		"--full-path", "-p",
		"--case-sensitive", "-s",
		"--ignore-case", "-i",
		"--smart-case",
		"--glob", "-g",
		"--fixed-strings", "-F",
		"--one-file-system",
		"--prune":
		return true
	default:
		return false
	}
}

func fdPathListValueFlag(arg string) bool {
	switch arg {
	case "-e", "--extension",
		"-t", "--type",
		"-E", "--exclude",
		"-d", "--max-depth",
		"--min-depth",
		"--base-directory",
		"--search-path",
		"--color",
		"-c",
		"-j", "--threads",
		"--max-results",
		"--size",
		"--owner":
		return true
	default:
		return false
	}
}

func fdPathListInlineValueFlag(arg string) bool {
	switch {
	case strings.HasPrefix(arg, "--extension="),
		strings.HasPrefix(arg, "--type="),
		strings.HasPrefix(arg, "--exclude="),
		strings.HasPrefix(arg, "--max-depth="),
		strings.HasPrefix(arg, "--min-depth="),
		strings.HasPrefix(arg, "--base-directory="),
		strings.HasPrefix(arg, "--search-path="),
		strings.HasPrefix(arg, "--color="),
		strings.HasPrefix(arg, "--threads="),
		strings.HasPrefix(arg, "--max-results="),
		strings.HasPrefix(arg, "--size="),
		strings.HasPrefix(arg, "--owner="):
		_, value, _ := strings.Cut(arg, "=")
		return strings.TrimSpace(value) != ""
	case strings.HasPrefix(arg, "-e"),
		strings.HasPrefix(arg, "-t"),
		strings.HasPrefix(arg, "-E"),
		strings.HasPrefix(arg, "-d"),
		strings.HasPrefix(arg, "-c"),
		strings.HasPrefix(arg, "-j"):
		return len(arg) > 2 && strings.TrimSpace(arg[2:]) != ""
	default:
		return false
	}
}

func fdPathListCombinedBoolShortFlag(arg string) bool {
	if len(arg) <= 2 || !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return false
	}
	for _, flag := range arg[1:] {
		switch flag {
		case 'H', 'L', 'u', 'I', 'a', 'p', 's', 'i', 'S', 'g', 'F':
		default:
			return false
		}
	}
	return true
}

func findPathListArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.TrimSpace(argv[0])), ".exe"))
	if base != "find" {
		return false
	}
	sawMaxDepth := false
	for i := 1; i < len(argv); i++ {
		arg := strings.TrimSpace(argv[i])
		if arg == "" {
			return false
		}
		switch arg {
		case "-exec", "-execdir", "-ok", "-okdir", "-delete", "-printf", "-fprintf",
			"-ls", "-fls", "-print0", "-fprint", "-fprint0":
			return false
		case "-maxdepth", "-mindepth":
			isMaxDepth := arg == "-maxdepth"
			i++
			if i >= len(argv) {
				return false
			}
			if !findPathListBoundedDepthArg(argv[i]) {
				return false
			}
			if isMaxDepth {
				sawMaxDepth = true
			}
		case "-name", "-iname", "-path", "-ipath", "-regex", "-iregex", "-type":
			i++
			if i >= len(argv) || strings.TrimSpace(argv[i]) == "" {
				return false
			}
		case "-print", "-empty", "-not", "!", "-a", "-and", "-o", "-or", "(", ")":
		default:
			if strings.HasPrefix(arg, "-") {
				return false
			}
		}
	}
	return sawMaxDepth
}

func findPathListBoundedDepthArg(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "0" {
		return true
	}
	if raw == "" {
		return false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(raw)
	return err == nil && n > 0 && n <= 6
}

func lsOnlyTotalLines(s string) bool {
	lines := strings.Split(s, "\n")
	sawLine := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		sawLine = true
		if trimmed != "total 0" && !strings.HasPrefix(trimmed, "total ") {
			return false
		}
	}
	return sawLine
}

type wcColumn string

const (
	wcColumnLines         wcColumn = "L"
	wcColumnWords         wcColumn = "W"
	wcColumnChars         wcColumn = "Ch"
	wcColumnBytes         wcColumn = "B"
	wcColumnMaxLineLength wcColumn = "Max"
)

type wcRow struct {
	Counts []string
	Path   string
	Total  bool
}

// TryCompactWc compacts deterministic `wc` output while preserving every count,
// the requested units, file names, and total rows. Unknown flags or ambiguous
// rows fail open to the original output.
func TryCompactWc(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "wc" && b != "wc.exe" {
		return stdout, false
	}
	columns, ok := wcColumnsFromArgv(argv[1:])
	if !ok {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return stdout, false
	}
	rows, ok := parseWcRows(s, columns)
	if !ok {
		return stdout, false
	}
	out := formatWcRows(rows, columns)
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func wcColumnsFromArgv(args []string) ([]wcColumn, bool) {
	selected := map[wcColumn]bool{}
	for i := range args {
		arg := args[i]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			switch {
			case arg == "--lines":
				selected[wcColumnLines] = true
			case arg == "--words":
				selected[wcColumnWords] = true
			case arg == "--chars":
				selected[wcColumnChars] = true
			case arg == "--bytes":
				selected[wcColumnBytes] = true
			case arg == "--max-line-length":
				selected[wcColumnMaxLineLength] = true
			default:
				return nil, false
			}
			continue
		}
		for _, ch := range arg[1:] {
			switch ch {
			case 'l':
				selected[wcColumnLines] = true
			case 'w':
				selected[wcColumnWords] = true
			case 'm':
				selected[wcColumnChars] = true
			case 'c':
				selected[wcColumnBytes] = true
			case 'L':
				selected[wcColumnMaxLineLength] = true
			default:
				return nil, false
			}
		}
	}
	if len(selected) == 0 {
		return []wcColumn{wcColumnLines, wcColumnWords, wcColumnBytes}, true
	}
	order := []wcColumn{wcColumnLines, wcColumnWords, wcColumnChars, wcColumnBytes, wcColumnMaxLineLength}
	columns := make([]wcColumn, 0, len(selected))
	for _, column := range order {
		if selected[column] {
			columns = append(columns, column)
		}
	}
	return columns, len(columns) > 0
}

func parseWcRows(s string, columns []wcColumn) ([]wcRow, bool) {
	lines := strings.Split(s, "\n")
	rows := make([]wcRow, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		counts, rest, ok := parseLeadingWcCounts(line, len(columns))
		if !ok {
			return nil, false
		}
		rows = append(rows, wcRow{
			Counts: counts,
			Path:   rest,
			Total:  rest == "total",
		})
	}
	if len(rows) == 0 {
		return nil, false
	}
	if len(rows) > 1 {
		for _, row := range rows {
			if row.Path == "" {
				return nil, false
			}
		}
	}
	return rows, true
}

func parseLeadingWcCounts(line string, n int) ([]string, string, bool) {
	counts := make([]string, 0, n)
	i := 0
	for len(counts) < n {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		start := i
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
		if start == i {
			return nil, "", false
		}
		if i < len(line) && line[i] != ' ' && line[i] != '\t' {
			return nil, "", false
		}
		counts = append(counts, line[start:i])
	}
	return counts, strings.TrimSpace(line[i:]), true
}

func formatWcRows(rows []wcRow, columns []wcColumn) string {
	if len(rows) == 1 {
		return formatWcRow(rows[0], columns, "") + "\n"
	}
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Path != "" && !row.Total {
			paths = append(paths, row.Path)
		}
	}
	prefix := commonDirectoryPrefix(paths)
	var sb strings.Builder
	if prefix != "" {
		sb.WriteString("[wc prefix=")
		sb.WriteString(prefix)
		sb.WriteString("]\n")
	}
	for _, row := range rows {
		sb.WriteString(formatWcRow(row, columns, prefix))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func formatWcRow(row wcRow, columns []wcColumn, prefix string) string {
	counts := make([]string, 0, len(row.Counts))
	for i, count := range row.Counts {
		counts = append(counts, count+string(columns[i]))
	}
	countPart := strings.Join(counts, " ")
	if row.Path == "" {
		return countPart
	}
	if row.Total {
		return "total: " + countPart
	}
	path := strings.TrimPrefix(row.Path, prefix)
	if path == "" {
		path = row.Path
	}
	return fmt.Sprintf("%s: %s", path, countPart)
}

func commonDirectoryPrefix(paths []string) string {
	if len(paths) <= 1 {
		return ""
	}
	first := paths[0]
	pos := strings.LastIndex(first, "/")
	if pos < 0 {
		return ""
	}
	candidate := first[:pos+1]
	for candidate != "" {
		allMatch := true
		for _, path := range paths[1:] {
			if !strings.HasPrefix(path, candidate) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return candidate
		}
		trimmed := strings.TrimSuffix(candidate, "/")
		next := strings.LastIndex(trimmed, "/")
		if next < 0 {
			return ""
		}
		candidate = trimmed[:next+1]
	}
	return ""
}

// TryCompactDu compacts `du` output by capping the number of lines and
// truncating long paths. du output is lines of "size\tpath" — for large
// directory trees, this can be thousands of lines.
//
// Drawdown vector: the model loses individual file/dir sizes for entries
// beyond the cap. The total (last line) is always preserved. Fail-open
// on non-du output or small output.
func TryCompactDu(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "du" && b != "du.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[du] empty\n"), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 50 {
		return stdout, false
	}

	// Cap at 40 lines, preserving the last line (total).
	const maxLines = 40
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[du] %d entries (showing first %d + total)\n", len(lines), maxLines-1))

	lastLine := lines[len(lines)-1]
	shown := 0
	for i, line := range lines {
		if i >= maxLines-1 && i < len(lines)-1 {
			sb.WriteString(fmt.Sprintf("  [+%d more entries]\n", len(lines)-maxLines))
			break
		}
		if i == len(lines)-1 && shown > 0 {
			// Always include the last line (total) even if beyond cap.
		}
		// Truncate long lines at 120 chars.
		if len(line) > 120 {
			line = line[:117] + "..."
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
		shown++
	}
	// Ensure the last line (total) is always included.
	if !strings.HasSuffix(sb.String(), lastLine+"\n") {
		if len(lastLine) > 120 {
			lastLine = lastLine[:117] + "..."
		}
		sb.WriteString(lastLine)
		sb.WriteByte('\n')
	}

	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

func isPrettierArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "prettier" || b == "prettier.cmd" {
		return true
	}
	if npxMatches(argv, "prettier") {
		return true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "prettier" {
		return true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "prettier" {
		return true
	}
	return false
}

// TryCompactPrettier summarizes empty and exact clean-check stdout from Prettier (F24 partial).
func TryCompactPrettier(argv []string, stdout []byte) ([]byte, bool) {
	if !isPrettierArgv(argv) {
		return stdout, false
	}
	trimmed := strings.TrimSpace(string(stdout))
	if trimmed == "" {
		return []byte("[prettier] ok\n"), true
	}
	if !prettierArgvHasCheck(argv) || !prettierCleanCheckOutput(trimmed) {
		return stdout, false
	}
	return []byte("[prettier] ok\n"), true
}

func prettierArgvHasCheck(argv []string) bool {
	for _, arg := range argv {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--check", "-c":
			return true
		}
	}
	return false
}

func prettierCleanCheckOutput(trimmed string) bool {
	seenChecking := false
	seenClean := false
	for raw := range strings.SplitSeq(trimmed, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		switch line {
		case "Checking formatting...":
			if seenChecking || seenClean {
				return false
			}
			seenChecking = true
		case "All matched files use Prettier code style!":
			if !seenChecking || seenClean {
				return false
			}
			seenClean = true
		default:
			return false
		}
	}
	return seenChecking && seenClean
}

// TryCompactDprint summarizes empty stdout from `dprint fmt` / `npx|pnpm exec|yarn … dprint fmt` (F24 partial).
func TryCompactDprint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !execArgvSubcommand(argv, "dprint", "fmt") {
		return stdout, false
	}
	return []byte("[dprint fmt] ok\n"), true
}

// TryCompactBiomeFormat summarizes empty stdout from `biome format` / `npx|pnpm exec|yarn … biome format` (F24 partial).
func TryCompactBiomeFormat(argv []string, stdout []byte) ([]byte, bool) {
	if !argvContainsToken(argv, "format") {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if (b0 == "biome" || b0 == "biome.exe" || b0 == "biome.cmd") && len(argv) >= 2 && argv[1] == "format" {
		return []byte("[biome format] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && strings.EqualFold(filepath.Base(rest[0]), "biome") && rest[1] == "format" {
		return []byte("[biome format] ok\n"), true
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "biome") && argv[3] == "format" {
		return []byte("[biome format] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "biome") && argv[2] == "format" {
		return []byte("[biome format] ok\n"), true
	}
	return stdout, false
}

// TryCompactBufFormat summarizes empty stdout from `buf format` / `npx|pnpm exec|yarn … buf format` (F24 partial).
func TryCompactBufFormat(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !execArgvSubcommand(argv, "buf", "format") {
		return stdout, false
	}
	return []byte("[buf format] ok\n"), true
}

// TryCompactTerraformFmt summarizes empty stdout from `terraform fmt` / `tofu fmt` / `npx|pnpm exec|yarn … terraform|tofu fmt` (OpenTofu; F24 partial).
func TryCompactTerraformFmt(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if execArgvSubcommand(argv, "terraform", "fmt") || execArgvSubcommand(argv, "tofu", "fmt") {
		return []byte("[terraform fmt] ok\n"), true
	}
	return stdout, false
}

// isPyPkgToolArgv matches `tool` / `python -m tool` / `npx|pnpm exec|yarn …` for the same (F24).
func isPyPkgToolArgv(argv []string, tool string) bool {
	if len(argv) < 1 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 1 {
			return false
		}
		return isPyPkgToolArgv(rest, tool)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isPyPkgToolArgv(argv[2:], tool)
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isPyPkgToolArgv(argv[1:], tool)
	}
	b := b0
	tl := strings.ToLower(tool)
	if b == tl || b == tl+".exe" {
		return true
	}
	if b != "python" && b != "python3" && b != "python.exe" && b != "python3.exe" {
		return false
	}
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-m" && argv[i+1] == tool {
			return true
		}
	}
	return false
}

// TryCompactBlack summarizes empty stdout from `black` / `python -m black` / `npx|pnpm exec|yarn … black` / `pnpm exec|yarn … python … -m black` (F24 partial).
func TryCompactBlack(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isPyPkgToolArgv(argv, "black") {
		return stdout, false
	}
	return []byte("[black] ok\n"), true
}

// TryCompactRuffFormat summarizes empty stdout from `ruff format` / `python -m ruff format` / `npx|pnpm exec|yarn … ruff format` / `pnpm exec|yarn … python … -m ruff format` (F24 partial).
func TryCompactRuffFormat(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 || !argvContainsToken(argv, "format") {
		return stdout, false
	}
	if !isRuffArgv(argv) {
		return stdout, false
	}
	return []byte("[ruff format] ok\n"), true
}

// TryCompactTaploFormat summarizes empty stdout from `taplo format` / `npx|pnpm exec|yarn … taplo format` (F24 partial).
func TryCompactTaploFormat(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !execArgvSubcommand(argv, "taplo", "format") {
		return stdout, false
	}
	return []byte("[taplo format] ok\n"), true
}

// shfmtTailArgs returns argv slice after the shfmt binary for direct `shfmt` or `npx|pnpm exec|yarn … shfmt`.
func shfmtTailArgs(argv []string) ([]string, bool) {
	if len(argv) < 1 {
		return nil, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	switch {
	case b == "shfmt" || b == "shfmt.exe":
		return argv[1:], true
	case len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "shfmt":
		return argv[3:], true
	case len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "shfmt":
		return argv[2:], true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && strings.EqualFold(filepath.Base(rest[0]), "shfmt") {
		return rest[1:], true
	}
	return nil, false
}

// TryCompactShfmt summarizes empty stdout from `shfmt` / `npx|pnpm exec|yarn … shfmt` outside list/diff mode (F24 partial).
func TryCompactShfmt(argv []string, stdout []byte) ([]byte, bool) {
	tail, ok := shfmtTailArgs(argv)
	if !ok {
		return stdout, false
	}
	for _, a := range tail {
		switch a {
		case "-l", "--list", "-d", "--diff":
			return stdout, false
		}
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[shfmt] ok\n"), true
}

// TryCompactSqlfmt summarizes empty stdout from `sqlfmt` / `python -m sqlfmt` / `npx|pnpm exec|yarn … sqlfmt` / `pnpm exec|yarn … python … -m sqlfmt` (F24 partial).
func TryCompactSqlfmt(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isPyPkgToolArgv(argv, "sqlfmt") {
		return stdout, false
	}
	return []byte("[sqlfmt] ok\n"), true
}

// TryCompactIsort summarizes empty stdout from `isort` / `python -m isort` / `npx|pnpm exec|yarn … isort` / `pnpm exec|yarn … python … -m isort` (F24 partial).
func TryCompactIsort(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isPyPkgToolArgv(argv, "isort") {
		return stdout, false
	}
	return []byte("[isort] ok\n"), true
}

// TryCompactAutopep8 summarizes empty stdout from `autopep8` / `python -m autopep8` / `npx|pnpm exec|yarn … autopep8` / `pnpm exec|yarn … python … -m autopep8` (F24 partial).
func TryCompactAutopep8(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isPyPkgToolArgv(argv, "autopep8") {
		return stdout, false
	}
	return []byte("[autopep8] ok\n"), true
}

func goFmtCompactOutput(argv []string) ([]byte, bool) {
	if len(argv) < 1 {
		return nil, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "gofmt" || b == "gofmt.exe" {
		return []byte("[gofmt] ok\n"), true
	}
	if isGoBinary(argv[0]) && len(argv) >= 2 && argv[1] == "fmt" {
		return []byte("[go fmt] ok\n"), true
	}
	return nil, false
}

// TryCompactGofmt summarizes empty stdout from `gofmt` / `go fmt` / `npx|pnpm exec|yarn … gofmt|go fmt` (F24 partial).
func TryCompactGofmt(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if out, ok := goFmtCompactOutput(argv); ok {
		return out, true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 {
		if out, ok2 := goFmtCompactOutput(rest); ok2 {
			return out, true
		}
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		if out, ok := goFmtCompactOutput(argv[2:]); ok {
			return out, true
		}
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		if out, ok := goFmtCompactOutput(argv[1:]); ok {
			return out, true
		}
	}
	return stdout, false
}

// TryCompactRustfmt summarizes empty stdout from `rustfmt` / `npx|pnpm exec|yarn … rustfmt` (F24 partial).
func TryCompactRustfmt(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "rustfmt" || b == "rustfmt.exe" {
		return []byte("[rustfmt] ok\n"), true
	}
	if npxMatches(argv, "rustfmt") {
		return []byte("[rustfmt] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "rustfmt" {
		return []byte("[rustfmt] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "rustfmt" {
		return []byte("[rustfmt] ok\n"), true
	}
	return stdout, false
}

func isClangFormatBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "clang-format" || b == "clang-format.exe" || strings.HasPrefix(b, "clang-format-")
}

// TryCompactClangFormat summarizes empty stdout from `clang-format` / `clang-format-N` / `npx|pnpm exec|yarn … clang-format*` (F24 partial).
func TryCompactClangFormat(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isClangFormatBin(argv[0]) {
		return []byte("[clang-format] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && isClangFormatBin(rest[0]) {
		return []byte("[clang-format] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isClangFormatBin(argv[2]) {
		return []byte("[clang-format] ok\n"), true
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isClangFormatBin(argv[1]) {
		return []byte("[clang-format] ok\n"), true
	}
	return stdout, false
}

// TryCompactZigFmt summarizes empty stdout from `zig fmt` / `npx|pnpm exec|yarn … zig fmt` (F24 partial).
func TryCompactZigFmt(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "zig" || b == "zig.exe") && argv[1] == "fmt" {
		return []byte("[zig fmt] ok\n"), true
	}
	if npxMatches(argv, "zig", "fmt") {
		return []byte("[zig fmt] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "zig" && argv[3] == "fmt" {
		return []byte("[zig fmt] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "zig" && argv[2] == "fmt" {
		return []byte("[zig fmt] ok\n"), true
	}
	return stdout, false
}

// formatToolLabel returns a human-readable label for the format tool invoked by argv, or "" if not detected.
func formatToolLabel(argv []string) string {
	if len(argv) < 1 {
		return ""
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isPrettierArgv(argv) {
		return "prettier"
	}
	if execArgvSubcommand(argv, "dprint", "fmt") {
		return "dprint"
	}
	// biome format (mirrors TryCompactBiomeFormat detection)
	if argvContainsToken(argv, "format") {
		if (b0 == "biome" || b0 == "biome.exe" || b0 == "biome.cmd") && len(argv) >= 2 && argv[1] == "format" {
			return "biome"
		}
		if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && strings.EqualFold(filepath.Base(rest[0]), "biome") && rest[1] == "format" {
			return "biome"
		}
		if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "biome") && argv[3] == "format" {
			return "biome"
		}
		if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "biome") && argv[2] == "format" {
			return "biome"
		}
	}
	if execArgvSubcommand(argv, "buf", "format") {
		return "buf"
	}
	if execArgvSubcommand(argv, "terraform", "fmt") || execArgvSubcommand(argv, "tofu", "fmt") {
		return "terraform"
	}
	if isPyPkgToolArgv(argv, "black") {
		return "black"
	}
	if isRuffArgv(argv) && argvContainsToken(argv, "format") {
		return "ruff"
	}
	if execArgvSubcommand(argv, "taplo", "format") {
		return "taplo"
	}
	if tail, ok := shfmtTailArgs(argv); ok {
		listMode := false
		for _, a := range tail {
			if a == "-l" || a == "--list" || a == "-d" || a == "--diff" {
				listMode = true
				break
			}
		}
		if !listMode {
			return "shfmt"
		}
	}
	if isPyPkgToolArgv(argv, "sqlfmt") {
		return "sqlfmt"
	}
	if isPyPkgToolArgv(argv, "isort") {
		return "isort"
	}
	if isPyPkgToolArgv(argv, "autopep8") {
		return "autopep8"
	}
	// gofmt / go fmt (direct, npx, pnpm, yarn)
	if out, ok := goFmtCompactOutput(argv); ok {
		return strings.TrimSuffix(strings.TrimPrefix(string(out), "["), "] ok\n")
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 {
		if out, ok2 := goFmtCompactOutput(rest); ok2 {
			return strings.TrimSuffix(strings.TrimPrefix(string(out), "["), "] ok\n")
		}
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		if out, ok := goFmtCompactOutput(argv[2:]); ok {
			return strings.TrimSuffix(strings.TrimPrefix(string(out), "["), "] ok\n")
		}
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		if out, ok := goFmtCompactOutput(argv[1:]); ok {
			return strings.TrimSuffix(strings.TrimPrefix(string(out), "["), "] ok\n")
		}
	}
	// rustfmt
	if b0 == "rustfmt" || b0 == "rustfmt.exe" || npxMatches(argv, "rustfmt") ||
		(len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "rustfmt") ||
		(len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && argv[1] == "rustfmt") {
		return "rustfmt"
	}
	// clang-format
	if isClangFormatBin(argv[0]) {
		return "clang-format"
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && isClangFormatBin(rest[0]) {
		return "clang-format"
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isClangFormatBin(argv[2]) {
		return "clang-format"
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isClangFormatBin(argv[1]) {
		return "clang-format"
	}
	// zig fmt
	if (b0 == "zig" || b0 == "zig.exe") && len(argv) >= 2 && argv[1] == "fmt" {
		return "zig fmt"
	}
	if npxMatches(argv, "zig", "fmt") {
		return "zig fmt"
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "zig" && argv[3] == "fmt" {
		return "zig fmt"
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && argv[1] == "zig" && argv[2] == "fmt" {
		return "zig fmt"
	}
	return ""
}

const formatFileListMax = 10

// compactFormatFilelist summarizes format tool output that lists formatted files (one per line).
// Returns ("", false) if the output is short enough to keep as-is.
func compactFormatFilelist(s, label string) (string, bool) {
	var nonEmpty []string
	for l := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, strings.TrimSpace(l))
		}
	}
	if len(nonEmpty) <= formatFileListMax {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] %d file(s) formatted\n", label, len(nonEmpty)))
	selected := cappedSearchIndexes(len(nonEmpty), formatFileListMax, 3)
	previous := -1
	for _, idx := range selected {
		if previous >= 0 && idx > previous+1 {
			sb.WriteString(fmt.Sprintf("  [+%d more files]\n", idx-previous-1))
		}
		sb.WriteString("  ")
		sb.WriteString(nonEmpty[idx])
		sb.WriteByte('\n')
		previous = idx
	}
	if len(selected) > 0 && selected[len(selected)-1] < len(nonEmpty)-1 {
		sb.WriteString(fmt.Sprintf("  [+%d more files]\n", len(nonEmpty)-selected[len(selected)-1]-1))
	}
	out := sb.String()
	if len(out) >= len(s) {
		return "", false
	}
	return out, true
}

// TryCompactFormatOutput chains all format-tool compactors (F24).
// Empty stdout yields per-tool "ok". Non-empty stdout with many formatted files yields sampled changed paths.
func TryCompactFormatOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := TryCompactPrettier(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDprint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBiomeFormat(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBufFormat(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactTerraformFmt(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBlack(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactRuffFormat(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactTaploFormat(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactShfmt(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactSqlfmt(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactIsort(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactAutopep8(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGofmt(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactRustfmt(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactClangFormat(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactZigFmt(argv, stdout); ok {
		return out, true
	}
	if out, ok := compactPackageManagerFormatScriptOutput(argv, stdout); ok {
		return out, true
	}
	// Non-empty fallback: compact long file lists.
	if label := formatToolLabel(argv); label != "" {
		s := strings.TrimSpace(string(stdout))
		if s != "" {
			if out, ok := compactFormatFilelist(s, label); ok {
				return []byte(out), true
			}
		}
	}
	return stdout, false
}

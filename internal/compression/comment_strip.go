package compression

import (
	"path/filepath"
	"regexp"
	"strings"
)

// commentWhitelistPatterns marks comment lines that must survive the
// stripper because they carry semantic weight (safety invariants, license
// headers, critical TODOs). T98. Patterns are case-sensitive where the
// convention is conventional all-caps, case-insensitive where the
// surrounding word can vary.
var commentWhitelistPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bSAFETY:`),
	regexp.MustCompile(`\bINVARIANT:`),
	regexp.MustCompile(`\bTODO\(critical\):`),
	regexp.MustCompile(`\bFIXME\(critical\):`),
	regexp.MustCompile(`\bHACK\(critical\):`),
	regexp.MustCompile(`\bCopyright\b`),
	regexp.MustCompile(`\bSPDX-License-Identifier\b`),
	regexp.MustCompile(`\bAll rights reserved\b`),
	regexp.MustCompile(`\bLicensed under\b`),
}

// isWhitelistedComment reports whether a raw line carries content the
// whitelist mandates be preserved verbatim by the comment stripper.
// T98. Used by every per-language strip function as the first check
// before standard stripping logic runs.
func isWhitelistedComment(line string) bool {
	for _, re := range commentWhitelistPatterns {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// StripComments removes comments and normalizes whitespace from code.
// Conservative: lines that look ambiguous (complex strings, heredocs) pass through unchanged.
// lang is a tag such as from LanguageFromPath ("go", "python", …); empty lang returns code unchanged.
func StripComments(code, lang string) string {
	if lang == "" {
		return code
	}
	return stripCommentsByLang(code, lang)
}

func stripCommentsByLang(code, lang string) string {
	switch lang {
	case "go", "typescript", "javascript", "rust", "c", "cpp", "cxx", "java",
		"zig", "swift", "kotlin", "php", "dart", "lua", "scala", "graphql",
		"protobuf", "hcl", "powershell", "perl", "ocaml", "haskell",
		"erlang", "elixir", "solidity", "json5", "jsonnet":
		return stripCStyleComments(code)
	case "ruby", "shell", "bash", "sh", "zsh", "make", "dockerfile":
		return stripHashComments(code)
	case "python":
		return stripPythonComments(code)
	case "html":
		return stripHTMLComments(code)
	case "css":
		return stripCSSComments(code)
	case "yaml", "toml":
		return stripHashComments(code)
	case "svelte":
		return stripSvelteComments(code)
	case "markdown":
		return stripMarkdownComments(code)
	case "sql":
		return stripSQLComments(code)
	default:
		return code
	}
}

// stripCStyleComments removes // line comments and /* */ block comments
// while preserving comment-like content inside string literals.
func stripCStyleComments(code string) string {
	lines := strings.Split(code, "\n")
	result := make([]string, 0, len(lines))

	inBlockComment := false
	consecutiveBlanks := 0

	for _, line := range lines {
		if inBlockComment {
			// T98: a multi-line block comment containing a whitelisted
			// marker (e.g. license header) is preserved verbatim until
			// its terminator on the current line.
			if isWhitelistedComment(line) {
				if end := strings.Index(line, "*/"); end != -1 {
					inBlockComment = false
				}
				result = append(result, line)
				consecutiveBlanks = 0
				continue
			}
			end := strings.Index(line, "*/")
			if end == -1 {
				// Still inside block comment - skip line.
				continue
			}
			// Block comment ends on this line - keep remainder.
			inBlockComment = false
			line = line[end+2:]
		}

		// T98: preserve the original line whenever it carries a
		// semantic-comment marker (SAFETY: / Copyright / etc.).
		if isWhitelistedComment(line) {
			result = append(result, line)
			consecutiveBlanks = 0
			continue
		}

		stripped := stripCStyleLine(line, &inBlockComment)
		trimmed := strings.TrimSpace(stripped)

		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks <= 1 {
				result = append(result, "")
			}
			continue
		}
		consecutiveBlanks = 0
		result = append(result, stripped)
	}

	return strings.Join(result, "\n")
}

// stripCStyleLine processes a single line, removing // comments and starting /* */ tracking.
// inBlock is updated if a block comment starts and does not end on the same line.
func stripCStyleLine(line string, inBlock *bool) string {
	var b strings.Builder
	i := 0
	inString := false
	stringChar := byte(0)

	for i < len(line) {
		ch := line[i]

		if inString {
			b.WriteByte(ch)
			if ch == '\\' && i+1 < len(line) {
				// Escape sequence - consume next char too.
				i++
				b.WriteByte(line[i])
			} else if ch == stringChar {
				inString = false
			}
			i++
			continue
		}

		// Detect string start.
		if ch == '"' || ch == '\'' || ch == '`' {
			inString = true
			stringChar = ch
			b.WriteByte(ch)
			i++
			continue
		}

		// Detect // comment.
		if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			break // drop rest of line
		}

		// Detect /* comment start.
		if ch == '/' && i+1 < len(line) && line[i+1] == '*' {
			// Look for closing */ on the same line.
			end := strings.Index(line[i+2:], "*/")
			if end != -1 {
				// Block comment starts and ends on same line - skip it.
				i = i + 2 + end + 2
				continue
			}
			// Block comment runs past end of line.
			*inBlock = true
			break
		}

		b.WriteByte(ch)
		i++
	}

	return b.String()
}

// stripPythonComments removes # comments and triple-quoted docstrings.
func stripPythonComments(code string) string {
	// Remove triple-quoted strings used as docstrings.
	code = stripTripleQuotes(code, `"""`)
	code = stripTripleQuotes(code, "'''")

	lines := strings.Split(code, "\n")
	result := make([]string, 0, len(lines))
	consecutiveBlanks := 0

	for _, line := range lines {
		// T98: whitelist runs before stripping so semantic Python /
		// shell comments (license, SAFETY, …) survive intact.
		if isWhitelistedComment(line) {
			result = append(result, line)
			consecutiveBlanks = 0
			continue
		}
		stripped := stripHashLine(line)
		trimmed := strings.TrimSpace(stripped)
		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks <= 1 {
				result = append(result, "")
			}
			continue
		}
		consecutiveBlanks = 0
		result = append(result, stripped)
	}

	return strings.Join(result, "\n")
}

// stripTripleQuotes removes occurrences of triple-quoted strings from code.
// Conservative: if an odd number of delimiters are found, returns code unchanged.
func stripTripleQuotes(code, delim string) string {
	count := strings.Count(code, delim)
	if count < 2 || count%2 != 0 {
		return code
	}

	var b strings.Builder
	remaining := code
	for {
		start := strings.Index(remaining, delim)
		if start == -1 {
			b.WriteString(remaining)
			break
		}
		b.WriteString(remaining[:start])
		after := remaining[start+len(delim):]
		end := strings.Index(after, delim)
		remaining = after[end+len(delim):]
	}

	return b.String()
}

// stripHashLine removes a # comment from a line, preserving # inside strings.
func stripHashLine(line string) string {
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inString {
			if ch == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if ch == stringChar {
				inString = false
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = true
			stringChar = ch
			continue
		}
		if ch == '#' {
			return line[:i]
		}
	}
	return line
}

// stripHTMLComments removes <!-- --> HTML comments.
func stripHTMLComments(code string) string {
	var b strings.Builder
	remaining := code
	for {
		start := strings.Index(remaining, "<!--")
		if start == -1 {
			b.WriteString(remaining)
			break
		}
		b.WriteString(remaining[:start])
		after := remaining[start+4:]
		end := strings.Index(after, "-->")
		if end == -1 {
			b.WriteString(remaining[start:])
			break
		}
		remaining = after[end+3:]
	}
	result := normalizeBlankLines(b.String())
	return result
}

// stripCSSComments removes /* */ CSS comments.
func stripCSSComments(code string) string {
	var b strings.Builder
	remaining := code
	for {
		start := strings.Index(remaining, "/*")
		if start == -1 {
			b.WriteString(remaining)
			break
		}
		b.WriteString(remaining[:start])
		after := remaining[start+2:]
		end := strings.Index(after, "*/")
		if end == -1 {
			b.WriteString(remaining[start:])
			break
		}
		remaining = after[end+2:]
	}
	return normalizeBlankLines(b.String())
}

func stripCSSLine(line string, inBlock *bool) string {
	var b strings.Builder
	i := 0
	for i < len(line) {
		if *inBlock {
			end := strings.Index(line[i:], "*/")
			if end == -1 {
				return b.String()
			}
			i += end + 2
			*inBlock = false
			continue
		}
		if i+1 < len(line) && line[i] == '/' && line[i+1] == '*' {
			end := strings.Index(line[i+2:], "*/")
			if end == -1 {
				*inBlock = true
				return b.String()
			}
			i = i + 2 + end + 2
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

func stripSvelteComments(code string) string {
	lines := strings.Split(code, "\n")
	result := make([]string, 0, len(lines))
	inScript := false
	inStyle := false
	inScriptBlock := false
	inStyleBlock := false
	consecutiveBlanks := 0

	for _, line := range lines {
		lower := strings.ToLower(line)
		if isWhitelistedComment(line) {
			result = append(result, line)
			consecutiveBlanks = 0
			continue
		}

		stripped := line
		switch {
		case inScript:
			if strings.Contains(lower, "</script>") {
				inScript = false
				inScriptBlock = false
			} else {
				stripped = stripCStyleLine(line, &inScriptBlock)
			}
		case inStyle:
			if strings.Contains(lower, "</style>") {
				inStyle = false
				inStyleBlock = false
			} else {
				stripped = stripCSSLine(line, &inStyleBlock)
			}
		default:
			stripped = stripHTMLComments(line)
			if strings.Contains(lower, "<script") && !strings.Contains(lower, "</script>") {
				inScript = true
			} else if strings.Contains(lower, "<style") && !strings.Contains(lower, "</style>") {
				inStyle = true
			}
		}

		trimmed := strings.TrimSpace(stripped)
		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks <= 1 {
				result = append(result, "")
			}
			continue
		}
		consecutiveBlanks = 0
		result = append(result, stripped)
	}

	return strings.Join(result, "\n")
}

func stripMarkdownComments(code string) string {
	lines := strings.Split(code, "\n")
	result := make([]string, 0, len(lines))
	inFence := false
	fenceLang := ""
	inCBlock := false
	inSQLBlock := false
	consecutiveBlanks := 0

	for _, line := range lines {
		if lang, fence := markdownFenceLanguage(line); fence {
			inFence = !inFence
			if inFence {
				fenceLang = lang
				inCBlock = false
				inSQLBlock = false
			} else {
				fenceLang = ""
			}
			result = append(result, line)
			consecutiveBlanks = 0
			continue
		}

		stripped := line
		if inFence {
			switch {
			case isCStyleCommentLanguage(fenceLang):
				stripped = stripCStyleLine(line, &inCBlock)
			case isHashCommentLanguage(fenceLang):
				stripped = stripHashLine(line)
			case fenceLang == "sql":
				stripped = stripSQLLine(line, &inSQLBlock)
			case fenceLang == "html":
				stripped = stripHTMLComments(line)
			case fenceLang == "css":
				stripped = stripCSSComments(line)
			}
		} else {
			stripped = stripHTMLComments(line)
		}

		trimmed := strings.TrimSpace(stripped)
		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks <= 1 {
				result = append(result, "")
			}
			continue
		}
		consecutiveBlanks = 0
		result = append(result, stripped)
	}

	return strings.Join(result, "\n")
}

func markdownFenceLanguage(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "```") && !strings.HasPrefix(trimmed, "~~~") {
		return "", false
	}
	raw := strings.TrimSpace(trimmed[3:])
	if raw == "" {
		return "", true
	}
	fields := strings.Fields(raw)
	return canonicalFenceLanguage(fields[0]), true
}

func canonicalFenceLanguage(raw string) string {
	lang := strings.ToLower(strings.Trim(raw, "{}[](),"))
	lang = strings.TrimPrefix(lang, "language-")
	switch lang {
	case "ts", "tsx":
		return "typescript"
	case "js", "jsx", "mjs", "cjs":
		return "javascript"
	case "rs":
		return "rust"
	case "py", "python3":
		return "python"
	case "sh", "bash", "zsh":
		return "shell"
	case "c++", "cc", "cxx", "hpp":
		return "cpp"
	case "htm":
		return "html"
	case "scss", "sass":
		return "css"
	case "postgres", "postgresql", "mysql", "sqlite":
		return "sql"
	default:
		return lang
	}
}

func isCStyleCommentLanguage(lang string) bool {
	switch lang {
	case "go", "typescript", "javascript", "rust", "c", "cpp", "cxx", "java",
		"zig", "swift", "kotlin", "php", "dart", "lua", "scala", "graphql",
		"protobuf", "hcl", "powershell", "perl", "ocaml", "haskell",
		"erlang", "elixir", "solidity", "json5", "jsonnet":
		return true
	default:
		return false
	}
}

func isHashCommentLanguage(lang string) bool {
	switch lang {
	case "ruby", "shell", "bash", "sh", "zsh", "make", "dockerfile", "python", "yaml", "toml":
		return true
	default:
		return false
	}
}

func stripSQLComments(code string) string {
	lines := strings.Split(code, "\n")
	result := make([]string, 0, len(lines))
	inBlock := false
	consecutiveBlanks := 0

	for _, line := range lines {
		if isWhitelistedComment(line) {
			result = append(result, line)
			consecutiveBlanks = 0
			continue
		}
		stripped := stripSQLLine(line, &inBlock)
		trimmed := strings.TrimSpace(stripped)
		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks <= 1 {
				result = append(result, "")
			}
			continue
		}
		consecutiveBlanks = 0
		result = append(result, stripped)
	}

	return strings.Join(result, "\n")
}

func stripSQLLine(line string, inBlock *bool) string {
	if !*inBlock && strings.Contains(line, "$$") {
		return line
	}

	var b strings.Builder
	i := 0
	inString := false
	stringChar := byte(0)

	for i < len(line) {
		ch := line[i]

		if *inBlock {
			end := strings.Index(line[i:], "*/")
			if end == -1 {
				return b.String()
			}
			i += end + 2
			*inBlock = false
			continue
		}

		if inString {
			b.WriteByte(ch)
			if ch == stringChar {
				if stringChar == '\'' && i+1 < len(line) && line[i+1] == '\'' {
					i++
					b.WriteByte(line[i])
					i++
					continue
				}
				inString = false
			}
			i++
			continue
		}

		if ch == '\'' || ch == '"' || ch == '`' {
			inString = true
			stringChar = ch
			b.WriteByte(ch)
			i++
			continue
		}

		if ch == '-' && i+1 < len(line) && line[i+1] == '-' {
			break
		}

		if ch == '/' && i+1 < len(line) && line[i+1] == '*' {
			end := strings.Index(line[i+2:], "*/")
			if end == -1 {
				*inBlock = true
				break
			}
			i = i + 2 + end + 2
			continue
		}

		b.WriteByte(ch)
		i++
	}

	return b.String()
}

// stripHashComments removes # comments for YAML and TOML.
func stripHashComments(code string) string {
	lines := strings.Split(code, "\n")
	result := make([]string, 0, len(lines))
	consecutiveBlanks := 0

	for _, line := range lines {
		// T98: preserve semantic shell / Ruby / yaml / toml comments.
		if isWhitelistedComment(line) {
			result = append(result, line)
			consecutiveBlanks = 0
			continue
		}
		stripped := stripHashLine(line)
		trimmed := strings.TrimSpace(stripped)
		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks <= 1 {
				result = append(result, "")
			}
			continue
		}
		consecutiveBlanks = 0
		result = append(result, stripped)
	}

	return strings.Join(result, "\n")
}

// normalizeBlankLines collapses runs of more than 2 consecutive blank lines to 1.
func normalizeBlankLines(code string) string {
	lines := strings.Split(code, "\n")
	result := make([]string, 0, len(lines))
	consecutiveBlanks := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			consecutiveBlanks++
			if consecutiveBlanks <= 1 {
				result = append(result, "")
			}
			continue
		}
		consecutiveBlanks = 0
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// LanguageFromPath infers a language identifier from a file path extension (for StripComments).
func LanguageFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp", ".hxx":
		return "cpp"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".py":
		return "python"
	case ".css", ".scss", ".sass":
		return "css"
	case ".html", ".htm":
		return "html"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".zig":
		return "zig"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".php":
		return "php"
	case ".dart":
		return "dart"
	case ".lua":
		return "lua"
	case ".scala", ".sc":
		return "scala"
	case ".graphql", ".gql":
		return "graphql"
	case ".proto":
		return "protobuf"
	case ".tf", ".tfvars", ".hcl":
		return "hcl"
	case ".ps1", ".psm1", ".psd1":
		return "powershell"
	case ".pl", ".pm":
		return "perl"
	case ".ml", ".mli":
		return "ocaml"
	case ".hs", ".lhs":
		return "haskell"
	case ".erl", ".hrl":
		return "erlang"
	case ".ex", ".exs":
		return "elixir"
	case ".sol":
		return "solidity"
	case ".json5":
		return "json5"
	case ".jsonnet", ".libsonnet":
		return "jsonnet"
	case ".svelte":
		return "svelte"
	case ".md", ".markdown", ".mdx":
		return "markdown"
	case ".sql":
		return "sql"
	default:
		base := strings.ToLower(filepath.Base(path))
		switch base {
		case "dockerfile", "containerfile":
			return "dockerfile"
		case "makefile", "gnumakefile":
			return "make"
		default:
			return ""
		}
	}
}

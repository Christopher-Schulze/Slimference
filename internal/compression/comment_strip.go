package compression

import (
	"path/filepath"
	"strings"
)

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
	case "go", "typescript", "javascript", "rust", "c", "cpp", "cxx", "java":
		return stripCStyleComments(code)
	case "ruby", "shell", "bash", "sh", "zsh":
		return stripHashComments(code)
	case "python":
		return stripPythonComments(code)
	case "html":
		return stripHTMLComments(code)
	case "css":
		return stripCSSComments(code)
	case "yaml", "toml":
		return stripHashComments(code)
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
			end := strings.Index(line, "*/")
			if end == -1 {
				// Still inside block comment - skip line.
				continue
			}
			// Block comment ends on this line - keep remainder.
			inBlockComment = false
			line = line[end+2:]
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

// stripHashComments removes # comments for YAML and TOML.
func stripHashComments(code string) string {
	lines := strings.Split(code, "\n")
	result := make([]string, 0, len(lines))
	consecutiveBlanks := 0

	for _, line := range lines {
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
	default:
		return ""
	}
}

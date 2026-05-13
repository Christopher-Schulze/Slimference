package compression

import (
	"fmt"
	"regexp"
	"strings"
)

// StructureExtractor extracts structural signatures from code using regex patterns.
// It is intentionally conservative: if extraction would produce output larger than the
// original, the original is returned unchanged.
type StructureExtractor struct {
	// Language-specific compiled patterns are built lazily via helpers.
}

// NewStructureExtractor returns a ready-to-use StructureExtractor.
func NewStructureExtractor() *StructureExtractor {
	return &StructureExtractor{}
}

// Extract returns a structural summary of code for supported languages.
// Returns (code, false) if the language is unsupported or if structural summary would
// exceed the original size.
func (e *StructureExtractor) Extract(code, language string) (string, bool) {
	var summary string
	switch language {
	case "go":
		summary = extractGoStructure(code)
	case "typescript", "javascript":
		summary = extractTSStructure(code)
	case "rust":
		summary = extractRustStructure(code)
	case "python":
		summary = extractPythonStructure(code)
	case "c", "cpp":
		summary = extractCeeStructure(code)
	case "java":
		summary = extractJavaStructure(code)
	case "ruby":
		summary = extractRubyStructure(code)
	case "shell":
		summary = extractShellStructure(code)
	case "zig":
		summary = extractZigStructure(code)
	case "swift":
		summary = extractSwiftStructure(code)
	case "kotlin":
		summary = extractKotlinStructure(code)
	case "php":
		summary = extractPHPStructure(code)
	case "dart":
		summary = extractDartStructure(code)
	case "scala":
		summary = extractScalaStructure(code)
	case "elixir":
		summary = extractElixirStructure(code)
	case "solidity":
		summary = extractSolidityStructure(code)
	case "svelte":
		summary = extractSvelteStructure(code)
	default:
		return code, false
	}

	if summary == "" || len(summary) >= len(code) {
		return code, false
	}

	return summary, true
}

// ExtractStructure is a package-level convenience wrapper around StructureExtractor.Extract.
// It returns (extracted, true) when the extraction produces a shorter result, (original, false) otherwise.
func ExtractStructure(code, language string) (string, bool) {
	return defaultStructureExtractor.Extract(code, language)
}

// defaultStructureExtractor is a package-level singleton for stateless extraction calls.
var defaultStructureExtractor = NewStructureExtractor()

// extractGoStructure extracts func/type/import/const/var declarations, dropping bodies.
func extractGoStructure(code string) string {
	lines := strings.Split(code, "\n")
	var kept []string
	extracted := 0

	// Patterns for declaration lines.
	funcRe := regexp.MustCompile(`^func\s`)
	typeRe := regexp.MustCompile(`^type\s`)
	importRe := regexp.MustCompile(`^import\s*`)
	constRe := regexp.MustCompile(`^const\s`)
	varRe := regexp.MustCompile(`^var\s`)
	packageRe := regexp.MustCompile(`^package\s`)

	braceDepth := 0
	inBody := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if packageRe.MatchString(trimmed) {
			kept = append(kept, line)
			continue
		}

		if !inBody {
			// Count brace opens on declaration lines.
			opens := strings.Count(line, "{")
			closes := strings.Count(line, "}")

			isDecl := funcRe.MatchString(trimmed) ||
				typeRe.MatchString(trimmed) ||
				importRe.MatchString(trimmed) ||
				constRe.MatchString(trimmed) ||
				varRe.MatchString(trimmed)

			if isDecl {
				extracted++
				if opens > closes {
					// Declaration opens a body block.
					inBody = true
					braceDepth = opens - closes
					// Keep only the signature line (everything before the first {).
					sig := signatureOnly(line)
					kept = append(kept, sig)
					continue
				}
				kept = append(kept, line)
				continue
			}

			// Non-declaration line outside a body (blank line, comment, etc.).
			kept = append(kept, line)
		} else {
			// Inside a function/type body - track brace depth.
			braceDepth += strings.Count(line, "{")
			braceDepth -= strings.Count(line, "}")
			if braceDepth <= 0 {
				inBody = false
				braceDepth = 0
				// Skip the closing brace line entirely.
			}
			// Skip body lines.
		}
	}

	if extracted == 0 {
		return ""
	}

	header := fmt.Sprintf("// [Structural summary - %d functions/types extracted]\n", extracted)
	return header + strings.Join(kept, "\n")
}

// signatureOnly returns the part of a declaration line up to (not including) the opening brace.
func signatureOnly(line string) string {
	idx := strings.LastIndex(line, "{")
	if idx == -1 {
		return line
	}
	sig := strings.TrimRight(line[:idx], " \t")
	return sig + " {}"
}

// extractTSStructure extracts TypeScript/JavaScript structural declarations.
func extractTSStructure(code string) string {
	lines := strings.Split(code, "\n")
	var kept []string
	extracted := 0

	funcRe := regexp.MustCompile(`^(export\s+)?(async\s+)?function\s`)
	constFnRe := regexp.MustCompile(`^(export\s+)?const\s+\w+\s*=\s*(async\s*)?\(`)
	arrowFnRe := regexp.MustCompile(`^(export\s+)?const\s+\w+\s*=\s*(async\s+)?\w+\s*=>`)
	interfaceRe := regexp.MustCompile(`^(export\s+)?interface\s`)
	typeRe := regexp.MustCompile(`^(export\s+)?type\s+\w+`)
	classRe := regexp.MustCompile(`^(export\s+)?(abstract\s+)?class\s`)
	importRe := regexp.MustCompile(`^import\s`)

	braceDepth := 0
	inBody := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if importRe.MatchString(trimmed) {
			kept = append(kept, line)
			extracted++
			continue
		}

		if !inBody {
			opens := strings.Count(line, "{")
			closes := strings.Count(line, "}")

			isDecl := funcRe.MatchString(trimmed) ||
				constFnRe.MatchString(trimmed) ||
				arrowFnRe.MatchString(trimmed) ||
				interfaceRe.MatchString(trimmed) ||
				typeRe.MatchString(trimmed) ||
				classRe.MatchString(trimmed)

			if isDecl {
				extracted++
				if opens > closes {
					inBody = true
					braceDepth = opens - closes
					kept = append(kept, signatureOnly(line))
					continue
				}
				kept = append(kept, line)
				continue
			}

			kept = append(kept, line)
		} else {
			braceDepth += strings.Count(line, "{")
			braceDepth -= strings.Count(line, "}")
			if braceDepth <= 0 {
				inBody = false
				braceDepth = 0
			}
		}
	}

	if extracted == 0 {
		return ""
	}

	header := fmt.Sprintf("// [Structural summary - %d functions/types extracted]\n", extracted)
	return header + strings.Join(kept, "\n")
}

// extractRustStructure extracts Rust structural declarations.
func extractRustStructure(code string) string {
	lines := strings.Split(code, "\n")
	var kept []string
	extracted := 0

	fnRe := regexp.MustCompile(`^(pub(\([\w:]+\))?\s+)?(async\s+)?fn\s`)
	structRe := regexp.MustCompile(`^(pub(\([\w:]+\))?\s+)?struct\s`)
	enumRe := regexp.MustCompile(`^(pub(\([\w:]+\))?\s+)?enum\s`)
	traitRe := regexp.MustCompile(`^(pub(\([\w:]+\))?\s+)?trait\s`)
	implRe := regexp.MustCompile(`^impl(\s*<[^>]*>)?\s`)
	useRe := regexp.MustCompile(`^(pub(\([\w:]+\))?\s+)?use\s`)
	constRe := regexp.MustCompile(`^(pub(\([\w:]+\))?\s+)?const\s`)
	attrRe := regexp.MustCompile(`^#\[`)

	braceDepth := 0
	inBody := false
	pendingAttr := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if attrRe.MatchString(trimmed) {
			pendingAttr = line
			continue
		}

		if !inBody {
			opens := strings.Count(line, "{")
			closes := strings.Count(line, "}")

			isDecl := fnRe.MatchString(trimmed) ||
				structRe.MatchString(trimmed) ||
				enumRe.MatchString(trimmed) ||
				traitRe.MatchString(trimmed) ||
				implRe.MatchString(trimmed) ||
				useRe.MatchString(trimmed) ||
				constRe.MatchString(trimmed)

			if isDecl {
				extracted++
				if pendingAttr != "" {
					kept = append(kept, pendingAttr)
					pendingAttr = ""
				}
				if opens > closes {
					inBody = true
					braceDepth = opens - closes
					kept = append(kept, signatureOnly(line))
					continue
				}
				kept = append(kept, line)
				continue
			}

			pendingAttr = ""
			kept = append(kept, line)
		} else {
			pendingAttr = ""
			braceDepth += strings.Count(line, "{")
			braceDepth -= strings.Count(line, "}")
			if braceDepth <= 0 {
				inBody = false
				braceDepth = 0
			}
		}
	}

	if extracted == 0 {
		return ""
	}

	header := fmt.Sprintf("// [Structural summary - %d functions/types extracted]\n", extracted)
	return header + strings.Join(kept, "\n")
}

// extractPythonStructure extracts Python def/class/import declarations.
func extractPythonStructure(code string) string {
	lines := strings.Split(code, "\n")
	var kept []string
	extracted := 0

	defRe := regexp.MustCompile(`^(async\s+)?def\s`)
	classRe := regexp.MustCompile(`^class\s`)
	importRe := regexp.MustCompile(`^(import|from)\s`)
	decoratorRe := regexp.MustCompile(`^@`)

	inBody := false
	bodyIndent := ""
	pendingDecorator := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			if !inBody {
				kept = append(kept, line)
			}
			continue
		}

		if decoratorRe.MatchString(trimmed) {
			pendingDecorator = line
			continue
		}

		if inBody {
			// Detect end of body by indent returning to base level.
			currentIndent := leadingWhitespace(line)
			if len(currentIndent) <= len(bodyIndent) && !strings.HasPrefix(line, bodyIndent+" ") {
				inBody = false
				bodyIndent = ""
				// Fall through to process this line normally.
			} else {
				pendingDecorator = ""
				continue
			}
		}

		isDecl := defRe.MatchString(trimmed) || classRe.MatchString(trimmed)
		isImport := importRe.MatchString(trimmed)

		if isImport {
			extracted++
			pendingDecorator = ""
			kept = append(kept, line)
			continue
		}

		if isDecl {
			extracted++
			if pendingDecorator != "" {
				kept = append(kept, pendingDecorator)
				pendingDecorator = ""
			}
			// Keep only the signature (first line ending with :).
			sig := strings.TrimRight(line, " \t")
			if !strings.HasSuffix(sig, ":") {
				sig += ":"
			}
			kept = append(kept, sig)
			inBody = true
			bodyIndent = leadingWhitespace(line)
			continue
		}

		pendingDecorator = ""
		kept = append(kept, line)
	}

	if extracted == 0 {
		return ""
	}

	header := fmt.Sprintf("# [Structural summary - %d functions/types extracted]\n", extracted)
	return header + strings.Join(kept, "\n")
}

// leadingWhitespace returns the leading whitespace characters of a line.
func leadingWhitespace(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	return line[:len(line)-len(trimmed)]
}

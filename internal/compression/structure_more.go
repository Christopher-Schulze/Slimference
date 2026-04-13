package compression

import (
	"fmt"
	"regexp"
	"strings"
)

// extractCeeStructure extracts #includes, typedef/struct/enum, and C-style function signatures.
func extractCeeStructure(code string) string {
	lines := strings.Split(code, "\n")
	var kept []string
	extracted := 0

	includeRe := regexp.MustCompile(`^\s*#include`)
	typeRe := regexp.MustCompile(`^\s*(typedef|struct|enum|union)\b`)
	funcRe := regexp.MustCompile(`^\s*(?:static|extern|inline|const|volatile\s+)*[a-zA-Z_][\w*\s]*\s+\**[\w]+\s*\([^)]*\)\s*\{?`)

	braceDepth := 0
	inBody := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if includeRe.MatchString(trimmed) {
			kept = append(kept, line)
			extracted++
			continue
		}

		if !inBody {
			opens := strings.Count(line, "{")
			closes := strings.Count(line, "}")

			if typeRe.MatchString(trimmed) {
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

			isDecl := funcRe.MatchString(trimmed)
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

	header := fmt.Sprintf("// [Structural summary - %d declarations extracted]\n", extracted)
	return header + strings.Join(kept, "\n")
}

// extractJavaStructure extracts package/import and type declarations (class, interface, enum, record).
func extractJavaStructure(code string) string {
	lines := strings.Split(code, "\n")
	var kept []string
	extracted := 0

	packageRe := regexp.MustCompile(`^package\s`)
	importRe := regexp.MustCompile(`^import\s`)
	classRe := regexp.MustCompile(`^(public|private|protected)?\s*(static\s+)?(abstract\s+)?(final\s+)?(sealed\s+)?(non-sealed\s+)?(class|interface|enum|record)\s`)
	annotationRe := regexp.MustCompile(`^@`)

	braceDepth := 0
	inBody := false
	pendingAnn := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if annotationRe.MatchString(trimmed) {
			pendingAnn = line
			continue
		}

		if packageRe.MatchString(trimmed) || importRe.MatchString(trimmed) {
			extracted++
			kept = append(kept, line)
			pendingAnn = ""
			continue
		}

		if !inBody {
			opens := strings.Count(line, "{")
			closes := strings.Count(line, "}")

			isDecl := classRe.MatchString(trimmed)

			if isDecl {
				extracted++
				if pendingAnn != "" {
					kept = append(kept, pendingAnn)
					pendingAnn = ""
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

			pendingAnn = ""
			kept = append(kept, line)
		} else {
			pendingAnn = ""
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

	header := fmt.Sprintf("// [Structural summary - %d declarations extracted]\n", extracted)
	return header + strings.Join(kept, "\n")
}

// extractRubyStructure keeps require/load and def/class/module lines (no bodies).
func extractRubyStructure(code string) string {
	lines := strings.Split(code, "\n")
	var kept []string
	extracted := 0

	requireRe := regexp.MustCompile(`^\s*(require|require_relative|load)\s`)
	defRe := regexp.MustCompile(`^\s*(def|class|module)\s`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if requireRe.MatchString(line) || defRe.MatchString(trimmed) {
			extracted++
			kept = append(kept, line)
		}
	}

	if extracted == 0 {
		return ""
	}

	body := strings.Join(kept, "\n")
	if len(body) >= len(code) {
		return ""
	}
	header := fmt.Sprintf("# [Structural summary - %d declarations extracted]\n", extracted)
	out := header + body
	if len(out) >= len(code) {
		return body
	}
	return out
}

// extractShellStructure keeps shebang and function declarations.
func extractShellStructure(code string) string {
	lines := strings.Split(code, "\n")
	var kept []string
	extracted := 0

	shebang := regexp.MustCompile(`^#!`)
	funcRe := regexp.MustCompile(`^(?:function\s+\w+|[a-zA-Z_][a-zA-Z0-9_]*)\s*\(\s*\)\s*\{`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if shebang.MatchString(trimmed) || funcRe.MatchString(trimmed) {
			extracted++
			kept = append(kept, line)
		}
	}

	if extracted == 0 {
		return ""
	}

	body := strings.Join(kept, "\n")
	if len(body) >= len(code) {
		return ""
	}
	header := fmt.Sprintf("# [Structural summary - %d declarations extracted]\n", extracted)
	out := header + body
	if len(out) >= len(code) {
		return body
	}
	return out
}

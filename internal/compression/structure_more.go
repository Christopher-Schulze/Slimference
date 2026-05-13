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

func extractZigStructure(code string) string {
	return extractBracePatternStructure(code, "//",
		[]string{
			`^(pub\s+)?fn\s`,
			`^(pub\s+)?(const|var)\s+\w+`,
			`^test\s`,
		},
		[]string{
			`^(pub\s+)?const\s+\w+\s*=\s*@import\(`,
		},
		nil,
	)
}

func extractSwiftStructure(code string) string {
	return extractBracePatternStructure(code, "//",
		[]string{
			`^(public|private|internal|fileprivate|open)?\s*(final\s+)?(class|struct|enum|protocol|extension|actor)\b`,
			`^(public|private|internal|fileprivate|open)?\s*(static\s+|class\s+|mutating\s+)?func\s`,
			`^(public|private|internal|fileprivate|open)?\s*(static\s+|class\s+)?(var|let)\s+\w+`,
		},
		[]string{`^import\s`},
		[]string{`^@`},
	)
}

func extractKotlinStructure(code string) string {
	return extractBracePatternStructure(code, "//",
		[]string{
			`^(public\s+|private\s+|internal\s+|protected\s+|data\s+|sealed\s+|open\s+|abstract\s+|enum\s+|annotation\s+|value\s+|inline\s+)*((class|object|interface|fun|typealias)\b)`,
			`^(public\s+|private\s+|internal\s+|protected\s+|const\s+|lateinit\s+)*((val|var)\s+\w+)`,
		},
		[]string{`^package\s`, `^import\s`},
		[]string{`^@`},
	)
}

func extractPHPStructure(code string) string {
	return extractBracePatternStructure(code, "//",
		[]string{
			`^(final\s+|abstract\s+)?(class|interface|trait|enum)\s`,
			`^(public\s+|private\s+|protected\s+|static\s+)*function\s+\w+`,
		},
		[]string{`^namespace\s`, `^use\s`},
		nil,
	)
}

func extractDartStructure(code string) string {
	return extractBracePatternStructure(code, "//",
		[]string{
			`^(abstract\s+|base\s+|final\s+|sealed\s+|interface\s+|mixin\s+)*((class|enum|extension|mixin)\b)`,
			`^(Future(<[^>]+>)?|Stream(<[^>]+>)?|void|int|String|bool|double|num|var|final|const)\s+\w+\s*\(`,
		},
		[]string{`^(import|export|part)\s`},
		[]string{`^@`},
	)
}

func extractScalaStructure(code string) string {
	return extractBracePatternStructure(code, "//",
		[]string{
			`^(case\s+)?(class|object|trait|enum)\b`,
			`^(def|val|var|given)\s`,
		},
		[]string{`^package\s`, `^import\s`},
		[]string{`^@`},
	)
}

func extractSolidityStructure(code string) string {
	return extractBracePatternStructure(code, "//",
		[]string{
			`^(abstract\s+)?(contract|interface|library)\b`,
			`^(function|constructor|modifier|event|error|struct|enum)\b`,
		},
		[]string{`^(pragma|import)\b`},
		nil,
	)
}

func extractElixirStructure(code string) string {
	lines := strings.Split(code, "\n")
	var kept []string
	extracted := 0

	passthroughRe := regexp.MustCompile(`^\s*(use|import|alias|require)\s`)
	declRe := regexp.MustCompile(`^\s*(defmodule|defprotocol|defimpl|defmacro|defmacrop|defguard|defguardp|defp?|test)\b`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if passthroughRe.MatchString(trimmed) || declRe.MatchString(trimmed) {
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

func extractSvelteStructure(code string) string {
	lines := strings.Split(code, "\n")
	var scriptLines []string
	var markupLines []string
	inScript := false
	seenStyle := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "<script"):
			inScript = true
			continue
		case inScript && strings.HasPrefix(lower, "</script"):
			inScript = false
			continue
		case inScript:
			scriptLines = append(scriptLines, line)
			continue
		case strings.HasPrefix(lower, "<style"):
			seenStyle = true
			continue
		case isSvelteStructuralMarkup(trimmed):
			markupLines = append(markupLines, line)
		}
	}

	var kept []string
	extracted := 0
	if len(scriptLines) > 0 {
		if scriptSummary, ok := ExtractStructure(strings.Join(scriptLines, "\n"), "typescript"); ok {
			kept = append(kept, "<script>")
			kept = append(kept, scriptSummary)
			kept = append(kept, "</script>")
			extracted++
		}
	}
	if len(markupLines) > 0 {
		kept = append(kept, markupLines...)
		extracted += len(markupLines)
	}
	if seenStyle {
		kept = append(kept, "<style>...</style>")
		extracted++
	}
	if extracted == 0 {
		return ""
	}

	body := strings.Join(kept, "\n")
	if len(body) >= len(code) {
		return ""
	}
	header := fmt.Sprintf("<!-- [Structural summary - %d declarations extracted] -->\n", extracted)
	out := header + body
	if len(out) >= len(code) {
		return body
	}
	return out
}

func isSvelteStructuralMarkup(trimmed string) bool {
	if strings.HasPrefix(trimmed, "{#") || strings.HasPrefix(trimmed, "{:") || strings.HasPrefix(trimmed, "{/") {
		return true
	}
	if !strings.HasPrefix(trimmed, "<") || strings.HasPrefix(trimmed, "</") || strings.HasPrefix(trimmed, "<!--") {
		return false
	}
	tag := strings.TrimLeft(trimmed[1:], " \t")
	if tag == "" {
		return false
	}
	first := tag[0]
	if first >= 'A' && first <= 'Z' {
		return true
	}
	for _, prefix := range []string{"svelte:", "slot", "main", "section", "article", "form", "table"} {
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	return false
}

func extractBracePatternStructure(code, commentPrefix string, declPatterns, passthroughPatterns, attrPatterns []string) string {
	lines := strings.Split(code, "\n")
	decls := compileRegexps(declPatterns)
	passthrough := compileRegexps(passthroughPatterns)
	attrs := compileRegexps(attrPatterns)

	var kept []string
	var pendingAttrs []string
	extracted := 0
	braceDepth := 0
	inBody := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inBody && matchesAny(attrs, trimmed) {
			pendingAttrs = append(pendingAttrs, line)
			continue
		}

		if !inBody {
			if matchesAny(passthrough, trimmed) {
				extracted++
				pendingAttrs = nil
				kept = append(kept, line)
				continue
			}

			opens := strings.Count(line, "{")
			closes := strings.Count(line, "}")
			if matchesAny(decls, trimmed) {
				extracted++
				if len(pendingAttrs) > 0 {
					kept = append(kept, pendingAttrs...)
					pendingAttrs = nil
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

			pendingAttrs = nil
			kept = append(kept, line)
			continue
		}

		pendingAttrs = nil
		braceDepth += strings.Count(line, "{")
		braceDepth -= strings.Count(line, "}")
		if braceDepth <= 0 {
			inBody = false
			braceDepth = 0
		}
	}

	if extracted == 0 {
		return ""
	}

	header := fmt.Sprintf("%s [Structural summary - %d declarations extracted]\n", commentPrefix, extracted)
	return header + strings.Join(kept, "\n")
}

func compileRegexps(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		out = append(out, regexp.MustCompile(pattern))
	}
	return out
}

func matchesAny(patterns []*regexp.Regexp, line string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

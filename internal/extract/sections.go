// Package extract is the deterministic semantic compactor that
// replaces MiniMax-backed Layer-2 summarization. It compacts long
// natural-language content (system prompts, assistant replies,
// accumulated history) using TF-IDF-ranked extractive summarization
// while preserving structural elements (code blocks, headers, lists)
// verbatim.
//
// Properties:
//   - Deterministic: identical input → identical output.
//   - Stateless: no model calls, no API keys, no network.
//   - Low overhead: pure Go, O(n) tokenisation + O(n*log n) ranking.
//   - Sub-millisecond on typical prompt sizes (<100 kB).
//
// Pipeline:
//
//	text → parseSections (code-block-aware lexer)
//	     → score prose sentences (tf-idf + position + length)
//	     → pick top-K sentences per prose section
//	     → reassemble, keeping code/headers/lists verbatim
package extract

import "strings"

// SectionKind tags a chunk of the input by its structural role. The
// compactor uses this tag to decide whether to summarise, preserve, or
// drop the chunk.
type SectionKind int

const (
	// SectionProse is a natural-language paragraph. Subject to TF-IDF
	// summarisation.
	SectionProse SectionKind = iota
	// SectionCode is a fenced code block (``` or ~~~) or an
	// indented-by-4-spaces code block. Preserved verbatim — code is
	// load-bearing for the agent.
	SectionCode
	// SectionHeader is a markdown header line (^#{1,6}\s).
	SectionHeader
	// SectionList is a markdown list (bulleted or numbered). Preserved
	// verbatim because list items are typically load-bearing
	// enumerations.
	SectionList
	// SectionBlank is one or more blank lines. Preserved to keep
	// vertical rhythm; never counted as content.
	SectionBlank
)

// Section is one parsed chunk. Content keeps the original newline
// boundaries so reassembly is byte-precise for non-prose kinds.
type Section struct {
	Kind    SectionKind
	Content string
}

// parseSections splits text into Sections. The split is lossless: the
// concatenation of all Section.Content fields equals the input (plus
// canonicalised line endings — \r\n is converted to \n on read; the
// reassembler emits \n exclusively).
//
// State-machine rules:
//   - Lines fenced by ``` or ~~~ form a SectionCode that includes the
//     fences. The fences themselves count as part of the code block.
//   - Lines indented by ≥4 spaces or a tab, with no surrounding
//     fences, form a SectionCode (indented block).
//   - Lines starting with `#{1,6}\s` are SectionHeader (one section
//     per header line).
//   - Lines starting with `-`, `*`, `+`, or `\d+.` are SectionList. A
//     consecutive run forms one Section.
//   - Empty lines (or whitespace-only) form a SectionBlank.
//   - Everything else is SectionProse. A consecutive run of prose
//     lines forms one Section.
func parseSections(text string) []Section {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	// Drop the spurious empty trailing element that strings.Split
	// emits for a string ending in '\n'. Otherwise it manifests as a
	// phantom 1-byte SectionBlank that doubles the final newline.
	hadTrailingNewline := strings.HasSuffix(text, "\n")
	if hadTrailingNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var out []Section
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Fenced code block.
		if fence := codeFence(trimmed); fence != "" {
			start := i
			i++
			for i < len(lines) {
				if codeFence(strings.TrimSpace(lines[i])) == fence {
					i++ // include the closing fence
					break
				}
				i++
			}
			out = append(out, Section{
				Kind:    SectionCode,
				Content: strings.Join(lines[start:i], "\n") + "\n",
			})
			continue
		}

		// Indented code block: ≥4 leading spaces or a tab, on a
		// non-blank line. We only enter this branch when there is no
		// preceding list to claim the indented line.
		if isIndentedCodeLine(line) && !hasOpenList(out) {
			start := i
			i++
			for i < len(lines) && isIndentedCodeLine(lines[i]) {
				i++
			}
			out = append(out, Section{
				Kind:    SectionCode,
				Content: strings.Join(lines[start:i], "\n") + "\n",
			})
			continue
		}

		// Header.
		if isHeaderLine(trimmed) {
			out = append(out, Section{
				Kind:    SectionHeader,
				Content: line + "\n",
			})
			i++
			continue
		}

		// Blank.
		if trimmed == "" {
			start := i
			for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
				i++
			}
			out = append(out, Section{
				Kind:    SectionBlank,
				Content: strings.Repeat("\n", i-start),
			})
			continue
		}

		// List.
		if isListLine(line) {
			start := i
			for i < len(lines) && (isListLine(lines[i]) || isListContinuation(lines[i])) {
				i++
				if i < len(lines) && strings.TrimSpace(lines[i]) == "" {
					break
				}
			}
			out = append(out, Section{
				Kind:    SectionList,
				Content: strings.Join(lines[start:i], "\n") + "\n",
			})
			continue
		}

		// Prose: a run of non-blank, non-special lines.
		start := i
		for i < len(lines) {
			l := lines[i]
			tl := strings.TrimSpace(l)
			if tl == "" || codeFence(tl) != "" || isHeaderLine(tl) || isListLine(l) {
				break
			}
			i++
		}
		out = append(out, Section{
			Kind:    SectionProse,
			Content: strings.Join(lines[start:i], "\n") + "\n",
		})
	}
	// Final newline normalisation: if the original text did not end
	// with a newline, strip the trailing newline added by the joiner
	// on the last section. This keeps the reassembly byte-precise for
	// inputs that lack a trailing \n.
	if !hadTrailingNewline && len(out) > 0 {
		last := out[len(out)-1]
		last.Content = strings.TrimSuffix(last.Content, "\n")
		out[len(out)-1] = last
	}
	return out
}

func codeFence(trimmed string) string {
	if strings.HasPrefix(trimmed, "```") {
		return "```"
	}
	if strings.HasPrefix(trimmed, "~~~") {
		return "~~~"
	}
	return ""
}

func isIndentedCodeLine(line string) bool {
	if strings.HasPrefix(line, "\t") {
		return true
	}
	if strings.HasPrefix(line, "    ") {
		return true
	}
	return false
}

func isHeaderLine(trimmed string) bool {
	if trimmed == "" || trimmed[0] != '#' {
		return false
	}
	// Count leading '#'. trimmed[0] == '#' guarantees i ≥ 1 after the
	// loop, so no zero-iteration guard needed.
	i := 0
	for i < len(trimmed) && i < 6 && trimmed[i] == '#' {
		i++
	}
	// Must be followed by space (or end of line) per CommonMark.
	if i >= len(trimmed) {
		return false
	}
	return trimmed[i] == ' ' || trimmed[i] == '\t'
}

func isListLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return false
	}
	// Bullets: -, *, + followed by space.
	if (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') &&
		len(trimmed) > 1 && (trimmed[1] == ' ' || trimmed[1] == '\t') {
		return true
	}
	// Numbered: digits + '.' + space.
	end := 0
	for end < len(trimmed) && trimmed[end] >= '0' && trimmed[end] <= '9' {
		end++
	}
	if end > 0 && end < len(trimmed)-1 &&
		trimmed[end] == '.' && (trimmed[end+1] == ' ' || trimmed[end+1] == '\t') {
		return true
	}
	return false
}

// isListContinuation: indented line right after a list bullet. We
// treat it as part of the list to avoid splitting wrapped bullet
// items into separate sections.
func isListContinuation(line string) bool {
	if line == "" || strings.TrimSpace(line) == "" {
		return false
	}
	return strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")
}

func hasOpenList(out []Section) bool {
	if len(out) == 0 {
		return false
	}
	return out[len(out)-1].Kind == SectionList
}

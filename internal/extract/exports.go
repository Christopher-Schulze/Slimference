package extract

// This file exposes the section parser and sentence splitter to
// in-tree callers (e.g. internal/summarization) that need to reuse
// the deterministic structure detection without re-implementing it.
// The internal lowercase functions remain the implementation site;
// these wrappers are purely API surface.

// ParseSections returns the section breakdown of text. See
// parseSections for the rules. The Section slice is byte-lossless on
// newline-terminated inputs (see TestParseSections_LossyConcatPreservesBytes
// in sections_test.go).
func ParseSections(text string) []Section {
	return parseSections(text)
}

// SplitSentences returns the sentence breakdown of a piece of prose.
// See splitSentences for the abbreviation / decimal / ellipsis rules.
func SplitSentences(prose string) []string {
	return splitSentences(prose)
}

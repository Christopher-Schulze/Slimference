package extract

import (
	"strings"
	"unicode"
)

// splitSentences is a deterministic sentence splitter. Splits on `.`,
// `!`, `?`, `\n\n` boundaries while avoiding common pitfalls:
//
//   - abbreviations: "etc.", "i.e.", "e.g.", "vs.", "Mr.", "Dr.",
//     "Fig.", "No.", "Inc.", "Ltd.", "Co.", "U.S.", etc. do not end a
//     sentence;
//   - numeric decimals: "3.14" does not split;
//   - elipses: "..." treated as a single boundary token.
//
// Goal is sensible-good, not perfect. Determinism + speed > 100%
// accuracy. The downstream ranker tolerates over-split sentences
// (each becomes its own ranked unit) much better than missed splits
// (which fuse multiple ideas into one unit).
func splitSentences(prose string) []string {
	if strings.TrimSpace(prose) == "" {
		return nil
	}
	prose = strings.ReplaceAll(prose, "\r\n", "\n")

	var out []string
	var cur strings.Builder
	runes := []rune(prose)
	i := 0
	for i < len(runes) {
		r := runes[i]
		cur.WriteRune(r)
		if r == '\n' && i+1 < len(runes) && runes[i+1] == '\n' {
			// double newline = paragraph break = sentence boundary.
			i++
			cur.WriteRune('\n')
			emit(&out, cur.String())
			cur.Reset()
			i++
			continue
		}
		if r == '.' || r == '!' || r == '?' {
			// Look-ahead: is the next non-space character a lowercase
			// letter? If so, NOT a boundary (probably an abbrev).
			if isLikelySentenceEnd(runes, i, &cur) {
				emit(&out, cur.String())
				cur.Reset()
			}
		}
		i++
	}
	if cur.Len() > 0 {
		emit(&out, cur.String())
	}
	return out
}

// emit appends s to out if it has any non-whitespace content.
func emit(out *[]string, s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	*out = append(*out, s)
}

// commonAbbreviations are the abbreviations that end in '.' but do not
// end a sentence. Lowercased on comparison so "Etc." matches.
var commonAbbreviations = map[string]bool{
	"etc": true, "ie": true, "eg": true, "vs": true,
	"mr": true, "mrs": true, "ms": true, "dr": true, "prof": true, "sr": true, "jr": true,
	"fig": true, "no": true, "vol": true, "ch": true, "p": true, "pp": true,
	"inc": true, "ltd": true, "co": true, "corp": true,
	"u.s": true, "u.k": true, "e.u": true,
	"jan": true, "feb": true, "mar": true, "apr": true, "jun": true, "jul": true,
	"aug": true, "sep": true, "sept": true, "oct": true, "nov": true, "dec": true,
}

// isLikelySentenceEnd inspects the rune before and after position i to
// decide whether the '.', '!', or '?' at i ends a sentence. Sets
// `cur` is unused here directly; we use the buffer to look at the
// preceding word.
func isLikelySentenceEnd(runes []rune, i int, cur *strings.Builder) bool {
	r := runes[i]

	// '!' and '?' are nearly always boundaries — preserve.
	if r == '!' || r == '?' {
		return true
	}

	// '.' analysis.
	// 1. If the character before the '.' is a digit, and the character
	//    after is also a digit, it is a decimal — not a boundary.
	prev := lookBehind(runes, i-1)
	next := lookAhead(runes, i+1)
	if isDigit(prev) && isDigit(next) {
		return false
	}

	// 2. Ellipsis: "..." — the third '.' is the boundary, the first
	//    two are not.
	if next == '.' {
		return false
	}
	prevPrev := lookBehind(runes, i-2)
	if prev == '.' && prevPrev == '.' {
		// We're the third dot. Boundary, but consume next-non-space
		// check below as normal.
	}

	// 3. Abbreviation: walk back from i-1 to find the start of the
	//    preceding token. Lowercase it, strip internal dots so "e.g"
	//    and "eg" both match the same entry in commonAbbreviations,
	//    look up. If hit → not a boundary.
	word := previousWord(cur.String())
	if word != "" {
		key := strings.ToLower(word)
		if commonAbbreviations[key] || commonAbbreviations[strings.ReplaceAll(key, ".", "")] {
			return false
		}
	}

	// 4. Look-ahead: if the next non-space rune is lower-case, treat
	//    the '.' as inline (mid-sentence). If uppercase, end of line,
	//    or absent, treat as boundary.
	skipped := i + 1
	for skipped < len(runes) && unicode.IsSpace(runes[skipped]) {
		skipped++
	}
	if skipped >= len(runes) {
		return true
	}
	nextChar := runes[skipped]
	if unicode.IsLower(nextChar) {
		return false
	}
	return true
}

func lookAhead(runes []rune, i int) rune {
	if i >= len(runes) || i < 0 {
		return 0
	}
	return runes[i]
}

func lookBehind(runes []rune, i int) rune {
	if i < 0 || i >= len(runes) {
		return 0
	}
	return runes[i]
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// previousWord returns the trailing alphanumeric word from the buffer,
// stripped of the trailing '.'. Used for abbreviation matching.
func previousWord(buf string) string {
	// Drop the trailing '.'/'!'/'?' just appended.
	buf = strings.TrimRight(buf, " \t.!?")
	end := len(buf)
	start := end
	for start > 0 {
		r := rune(buf[start-1])
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.') {
			break
		}
		start--
	}
	if start == end {
		return ""
	}
	return buf[start:end]
}

// tokenizeWords lower-cases and word-splits a string for TF-IDF.
// Returns alphanumeric tokens only.
func tokenizeWords(s string) []string {
	s = strings.ToLower(s)
	var out []string
	var cur strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

package evidence

import (
	"strings"
	"unicode"
)

type keywordSignal struct {
	words  []string
	signal Signal
}

var keywordSignalRegistry = []keywordSignal{
	{
		signal: SignalErrorKeyword,
		words: []string{
			"abort", "aborted", "critical", "crash", "crashed", "denied",
			"error", "exception", "fail", "failed", "failure", "fatal",
			"invalid", "panic", "rejected", "timeout", "timed out",
		},
	},
	{
		signal: SignalWarning,
		words:  []string{"warn", "warning"},
	},
	{
		signal: SignalImportant,
		words:  []string{"bug", "fix", "fixme", "hack", "important", "note", "todo", "xxx"},
	},
	{
		signal: SignalSecurity,
		words:  []string{"auth", "password", "secret", "security"},
	},
}

func detectKeywordSignals(text string) []Signal {
	lower := strings.ToLower(text)
	var out []Signal
	for _, group := range keywordSignalRegistry {
		for _, word := range group.words {
			if containsKeyword(lower, word) {
				out = appendSignal(out, group.signal)
				break
			}
		}
	}
	return out
}

func containsKeyword(text, keyword string) bool {
	if keyword == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(text[start:], keyword)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isKeywordRune(rune(text[idx-1]))
		after := idx + len(keyword)
		afterOK := after >= len(text) || !isKeywordRune(rune(text[after]))
		if beforeOK && afterOK {
			return true
		}
		start = idx + 1
	}
}

func isKeywordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

package compression

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	pathDictionaryMaxEntries      = 8
	pathDictionaryMinOccurrences  = 3
	pathDictionaryMinPathLen      = 24
	pathDictionaryMinNetSavedByte = 16
)

var absolutePathPattern = regexp.MustCompile(`(?:/[A-Za-z0-9._%+@=-]+){3,}`)

type pathDictionaryCandidate struct {
	path  string
	count int
	first int
}

type pathDictionaryAlias struct {
	token string
	path  string
}

func applySemanticDictionary(text string) (string, int, bool) {
	candidates := semanticDictionaryCandidates(text)
	if len(candidates) == 0 {
		return text, 0, false
	}
	if len(candidates) > pathDictionaryMaxEntries {
		candidates = candidates[:pathDictionaryMaxEntries]
	}
	aliases := make([]pathDictionaryAlias, 0, len(candidates))
	for i, candidate := range candidates {
		aliases = append(aliases, pathDictionaryAlias{
			token: "[P" + strconv.Itoa(i+1) + "]",
			path:  candidate.path,
		})
	}
	rewritten := text
	for _, alias := range aliases {
		rewritten = strings.ReplaceAll(rewritten, alias.path, alias.token)
	}
	out := pathDictionaryLegend(aliases) + rewritten
	saved := len(text) - len(out)
	if saved < pathDictionaryMinNetSavedByte {
		return text, 0, false
	}
	return out, saved, true
}

func semanticDictionaryCandidates(text string) []pathDictionaryCandidate {
	indices := absolutePathPattern.FindAllStringIndex(text, -1)
	if len(indices) == 0 {
		return nil
	}
	byPath := make(map[string]*pathDictionaryCandidate)
	for _, pair := range indices {
		if pair[0] > 0 && text[pair[0]-1] == ':' {
			continue
		}
		path := text[pair[0]:pair[1]]
		if !isDictionaryPath(path) {
			continue
		}
		current := byPath[path]
		if current == nil {
			byPath[path] = &pathDictionaryCandidate{path: path, count: 1, first: pair[0]}
			continue
		}
		current.count++
	}
	out := make([]pathDictionaryCandidate, 0, len(byPath))
	for _, candidate := range byPath {
		if candidate.count >= pathDictionaryMinOccurrences && len(candidate.path) >= pathDictionaryMinPathLen {
			out = append(out, *candidate)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].first < out[j].first
	})
	return out
}

func isDictionaryPath(path string) bool {
	for _, prefix := range []string{"/Users/", "/private/", "/var/", "/tmp/", "/opt/", "/usr/", "/home/", "/root/", "/Volumes/", "/Applications/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func pathDictionaryLegend(aliases []pathDictionaryAlias) string {
	var b strings.Builder
	b.WriteString("[path dictionary]\n")
	for _, alias := range aliases {
		b.WriteString(alias.token)
		b.WriteString("=")
		b.WriteString(alias.path)
		b.WriteString("\n")
	}
	b.WriteString("[/path dictionary]\n")
	return b.String()
}

package toolprune

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/slimference/slimference/internal/types"
)

// ExtractToolNames parses the request body and returns the names of all
// tool definitions found. Provider-specific shapes are handled:
//   - Anthropic: tools[].name
//   - OpenAI: tools[].function.name
//   - CodexChatGPT: same as OpenAI when tools[] is present
//
// Returns nil (not empty) when no tools field exists or the body is
// not valid JSON.
func ExtractToolNames(body []byte, provider types.Provider) []string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	toolsRaw, ok := raw["tools"]
	if !ok {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &entries); err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := extractToolName(entry, provider)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ExtractToolNamesForPruning is the strict variant used by the product hot path.
// It returns safe=false when a tools[] entry cannot be named for the provider.
// Pruning then full-passes the whole tool schema instead of modifying a request
// shape we cannot prove we understand.
func ExtractToolNamesForPruning(body []byte, provider types.Provider) ([]string, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, false
	}
	toolsRaw, ok := raw["tools"]
	if !ok {
		return nil, true
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &entries); err != nil {
		return nil, false
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := extractToolName(entry, provider)
		if name == "" {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

// extractToolName picks the tool name from a single tool definition entry
// according to the provider's wire shape.
func extractToolName(entry json.RawMessage, provider types.Provider) string {
	switch provider {
	case types.Anthropic:
		var v struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(entry, &v); err == nil {
			return v.Name
		}
	default:
		// OpenAI / CodexChatGPT / unknown
		var v struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(entry, &v); err == nil {
			if v.Function.Name != "" {
				return v.Function.Name
			}
			if v.Name != "" {
				return v.Name
			}
			if provider == types.CodexChatGPT {
				return codexSpecialToolName(v.Type)
			}
		}
	}
	return ""
}

func codexSpecialToolName(toolType string) string {
	switch strings.TrimSpace(toolType) {
	case "tool_search", "web_search", "image_generation":
		return toolType
	default:
		return ""
	}
}

// PruneToolDefinitions removes tool definitions whose names appear in
// toPrune from the request body. Returns the modified body and a map
// of pruned tool name -> original definition (for archiving). The
// original body is returned unchanged when no tools match or on any
// parse error (fail-open).
func PruneToolDefinitions(body []byte, provider types.Provider, toPrune map[string]bool) ([]byte, map[string]json.RawMessage, error) {
	if len(toPrune) == 0 {
		return body, nil, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, nil, nil
	}
	toolsRaw, ok := raw["tools"]
	if !ok {
		return body, nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &entries); err != nil {
		return body, nil, nil
	}

	kept := make([]json.RawMessage, 0, len(entries))
	pruned := make(map[string]json.RawMessage)
	for _, entry := range entries {
		name := extractToolName(entry, provider)
		if name != "" && toPrune[name] {
			pruned[name] = entry
		} else {
			kept = append(kept, entry)
		}
	}
	if len(pruned) == 0 {
		return body, nil, nil
	}

	keptJSON, _ := json.Marshal(kept)
	raw["tools"] = keptJSON
	out, _ := json.Marshal(raw)
	return out, pruned, nil
}

// MentionedTools returns the subset of `candidates` whose name or safe alias
// appears in `text`. Pure function used by T103b reattach to decide which
// previously-pruned tools should be restored on the next turn. It prefers
// reattaching over capability loss: false positives cost schema tokens, false
// negatives can remove a needed tool.
func MentionedTools(text string, candidates []string) []string {
	if text == "" || len(candidates) == 0 {
		return nil
	}
	out := make([]string, 0, len(candidates))
	lowerText := strings.ToLower(text)
	normalizedText := normalizeToolMention(text)
	for _, name := range candidates {
		if name == "" {
			continue
		}
		if containsToolAlias(lowerText, normalizedText, name) {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func containsToolAlias(lowerText, normalizedText, name string) bool {
	for _, alias := range toolNameAliases(name) {
		if containsToolName(lowerText, alias) {
			return true
		}
		normalizedAlias := normalizeToolMention(alias)
		if len(normalizedAlias) >= 6 && strings.Contains(normalizedText, normalizedAlias) {
			return true
		}
	}
	return false
}

func toolNameAliases(name string) []string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil
	}
	aliases := []string{strings.ToLower(trimmed)}
	words := splitToolNameWords(trimmed)
	if len(words) > 0 {
		aliases = append(aliases, strings.Join(words, " "))
		aliases = append(aliases, strings.Join(words, "_"))
		if tail := words[len(words)-1]; len(tail) >= 4 {
			aliases = append(aliases, tail)
		}
	}
	aliases = append(aliases, commandFamilyAliases(trimmed)...)
	return uniqueNonEmpty(aliases)
}

func splitToolNameWords(name string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, strings.ToLower(string(current)))
		current = current[:0]
	}
	var prev rune
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			prev = 0
			continue
		}
		if len(current) > 0 && unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
			flush()
		}
		current = append(current, unicode.ToLower(r))
		prev = r
	}
	flush()
	return words
}

func commandFamilyAliases(name string) []string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "bash") || strings.Contains(lower, "shell") || strings.Contains(lower, "exec") || strings.Contains(lower, "terminal"):
		return []string{"bash", "shell", "exec", "terminal", "command"}
	case strings.Contains(lower, "grep") || strings.Contains(lower, "search") || strings.Contains(lower, "rg"):
		return []string{"grep", "rg", "search"}
	case strings.Contains(lower, "read") || strings.Contains(lower, "open") || strings.Contains(lower, "view"):
		return []string{"read", "open", "view"}
	case strings.Contains(lower, "write") || strings.Contains(lower, "edit") || strings.Contains(lower, "patch"):
		return []string{"write", "edit", "patch", "apply_patch"}
	default:
		return nil
	}
}

func uniqueNonEmpty(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.ToLower(item))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeToolMention(s string) string {
	var out strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

// containsToolName tests whether `name` appears in `text` as a word
// (delimited by non-letter / non-digit characters or string ends).
func containsToolName(text, name string) bool {
	idx := 0
	for {
		hit := strings.Index(text[idx:], name)
		if hit == -1 {
			return false
		}
		start := idx + hit
		end := start + len(name)
		if isWordBoundary(text, start-1) && isWordBoundary(text, end) {
			return true
		}
		idx = end
	}
}

func isWordBoundary(s string, pos int) bool {
	if pos < 0 || pos >= len(s) {
		return true
	}
	c := s[pos]
	switch {
	case c >= 'a' && c <= 'z':
	case c >= 'A' && c <= 'Z':
	case c >= '0' && c <= '9':
	case c == '_':
	default:
		return true
	}
	return false
}

// ReattachToolDefinitions adds the supplied definitions back into the
// request body's `tools[]` array. Definitions whose tool name already
// exists in `tools[]` are skipped so reattaching twice in the same
// request is a no-op. Returns the modified body and the number of
// definitions that were actually re-attached. T103b.
func ReattachToolDefinitions(body []byte, provider types.Provider, defs map[string]json.RawMessage) ([]byte, int, error) {
	if len(defs) == 0 || len(body) == 0 {
		return body, 0, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, 0, nil
	}
	var entries []json.RawMessage
	if rawTools, ok := raw["tools"]; ok {
		if err := json.Unmarshal(rawTools, &entries); err != nil {
			return body, 0, nil
		}
	}
	existing := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		name := extractToolName(e, provider)
		if name == "" {
			return body, 0, nil
		}
		existing[name] = struct{}{}
	}
	added := 0
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		def := defs[name]
		if _, dup := existing[name]; dup {
			continue
		}
		entries = append(entries, def)
		existing[name] = struct{}{}
		added++
	}
	if added == 0 {
		return body, 0, nil
	}
	raw["tools"], _ = json.Marshal(entries)
	out, _ := json.Marshal(raw)
	return out, added, nil
}

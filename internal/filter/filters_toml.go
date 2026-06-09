package filter

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/Christopher-Schulze/Slimference/internal/compression"
)

// FiltersFile is the root document for .slimference/filters.toml (project or user-global).
type FiltersFile struct {
	SchemaVersion int                   `toml:"schema_version"`
	DenyPatterns  []string              `toml:"deny_patterns"`
	Filters       map[string]FilterRule `toml:"filters"`
}

// ReplacePair is one entry in `replace` (regex on each line, rules chained in order).
type ReplacePair struct {
	Pattern     string `toml:"pattern"`
	Replacement string `toml:"replacement"`
}

// MatchOutputRule is one entry in `match_output` (whole-output short-circuit).
type MatchOutputRule struct {
	Pattern string `toml:"pattern"`
	Message string `toml:"message"`
	Unless  string `toml:"unless"`
}

// FilterRule is one [filters.NAME] block (docs/spec.md §4.5 eight-stage pipeline).
type FilterRule struct {
	Description        string            `toml:"description"`
	MatchCommand       string            `toml:"match_command"`
	StripANSI          bool              `toml:"strip_ansi"`
	Replace            []ReplacePair     `toml:"replace"`
	MatchOutput        []MatchOutputRule `toml:"match_output"`
	StripLinesMatching []string          `toml:"strip_lines_matching"`
	KeepLinesMatching  []string          `toml:"keep_lines_matching"`
	TruncateLinesAt    int               `toml:"truncate_lines_at"`
	HeadLines          int               `toml:"head_lines"`
	TailLines          int               `toml:"tail_lines"`
	MaxLines           int               `toml:"max_lines"`
	OnEmpty            string            `toml:"on_empty"`
}

// UserFiltersPath returns ~/.slimference/filters.toml.
func UserFiltersPath() string {
	h, err := userHomeDirFunc()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".slimference", "filters.toml")
}

// LoadFiltersFile parses one filters.toml; missing file returns (nil, nil).
func LoadFiltersFile(path string) (*FiltersFile, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f FiltersFile
	if _, err := toml.Decode(string(b), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func uniqueFilterPaths(wd string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	// Project-local filters are gated by the trust model to prevent
	// repository-committed filters.toml from acting as a prompt-injection
	// vector against Claude Code or Codex. User-scoped filters under
	// ~/.slimference/ are trusted by definition (operator-owned).
	if p := ProjectFiltersPath(wd); p != "" {
		if projectFilterAllowed(p) {
			add(p)
		}
	}
	add(UserFiltersPath())
	return out
}

// projectFilterAllowed reports whether the project filter file at path is
// permitted to contribute to the merged filter set. Missing files are
// "allowed" (there's nothing to load). Existing files must either have a
// matching entry in the trust store or be overridden via the
// SLIMFERENCE_TRUST_PROJECT_FILTERS=1 env var.
func projectFilterAllowed(path string) bool {
	status, _, err := evaluateTrustFn(path)
	if err != nil {
		return false
	}
	switch status {
	case TrustStatusTrusted, TrustStatusEnvOverride, TrustStatusMissing:
		return true
	default:
		return false
	}
}

// evaluateTrustFn is overridable in tests so filter loading can be
// exercised without reading the on-disk trust store.
var evaluateTrustFn = EvaluateTrust

// LoadMergedDenyPatterns returns deny_patterns from project and user filters.toml (deduped paths).
func LoadMergedDenyPatterns(wd string) []string {
	var out []string
	for _, p := range uniqueFilterPaths(wd) {
		f, err := LoadFiltersFile(p)
		if err != nil || f == nil {
			continue
		}
		out = append(out, f.DenyPatterns...)
	}
	return out
}

// FirstMatchingTOMLRule finds the first filter rule whose match_command regex matches argv joined (project file before user).
func FirstMatchingTOMLRule(wd string, argv []string) *FilterRule {
	cmd := strings.Join(argv, " ")
	for _, path := range uniqueFilterPaths(wd) {
		f, err := LoadFiltersFile(path)
		if err != nil || f == nil || len(f.Filters) == 0 {
			continue
		}
		names := make([]string, 0, len(f.Filters))
		for n := range f.Filters {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			r := f.Filters[name]
			if strings.TrimSpace(r.MatchCommand) == "" {
				continue
			}
			re, err := regexp.Compile(r.MatchCommand)
			if err != nil {
				continue
			}
			if re.MatchString(cmd) {
				cp := r
				return &cp
			}
		}
	}
	return nil
}

// ApplyTOMLRule applies the §4.5 pipeline: strip_ansi → replace → match_output →
// strip/keep lines → truncate_lines_at → head/tail → max_lines → on_empty.
func ApplyTOMLRule(stdout []byte, rule *FilterRule) []byte {
	return applyTOMLRule(stdout, rule, tomlRuleApplyOptions{})
}

// ApplyBuiltinTOMLRule applies an embedded product-default TOML rule. It keeps
// the public/user TOML DSL semantics intact but makes the bundled catalog obey
// Layer-0's evidence-first contract: line caps preserve late diagnostic lines
// instead of blindly keeping only the first rows.
func ApplyBuiltinTOMLRule(stdout []byte, rule *FilterRule) []byte {
	return applyTOMLRule(stdout, rule, tomlRuleApplyOptions{preserveImportantLineCaps: true})
}

type tomlRuleApplyOptions struct {
	preserveImportantLineCaps bool
}

func applyTOMLRule(stdout []byte, rule *FilterRule, opts tomlRuleApplyOptions) []byte {
	if rule == nil {
		return stdout
	}
	s := string(stdout)
	if rule.StripANSI {
		s = compression.StripANSICodes(s)
	}
	lines := strings.Split(s, "\n")
	lines = applyReplacePerLine(lines, rule.Replace)

	blob := strings.Join(lines, "\n")
	if out, ok := applyMatchOutput(blob, rule.MatchOutput); ok {
		return out
	}

	lines = strings.Split(blob, "\n")
	lines = filterStripLines(lines, rule.StripLinesMatching)
	lines = filterKeepLines(lines, rule.KeepLinesMatching)
	if rule.TruncateLinesAt > 0 {
		lines = truncateEachLine(lines, rule.TruncateLinesAt)
	}
	if rule.HeadLines > 0 && len(lines) > rule.HeadLines {
		if opts.preserveImportantLineCaps {
			lines = truncateTOMLLinesPreservingEvidence(lines, rule.HeadLines, "head")
		} else {
			lines = lines[:rule.HeadLines]
		}
	}
	if rule.TailLines > 0 && len(lines) > rule.TailLines {
		if opts.preserveImportantLineCaps {
			lines = truncateTOMLLinesPreservingEvidence(lines, rule.TailLines, "tail")
		} else {
			lines = lines[len(lines)-rule.TailLines:]
		}
	}
	if rule.MaxLines > 0 && len(lines) > rule.MaxLines {
		if opts.preserveImportantLineCaps {
			lines = truncateTOMLLinesPreservingEvidence(lines, rule.MaxLines, "head_tail")
		} else {
			lines = lines[:rule.MaxLines]
		}
	}
	out := strings.Join(lines, "\n")
	if strings.TrimSpace(out) == "" && rule.OnEmpty != "" {
		return []byte(rule.OnEmpty)
	}
	return []byte(out)
}

func applyReplacePerLine(lines []string, rules []ReplacePair) []string {
	if len(rules) == 0 {
		return lines
	}
	var res []*regexp.Regexp
	var repl []string
	for _, r := range rules {
		pat := strings.TrimSpace(r.Pattern)
		if pat == "" {
			continue
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			continue
		}
		res = append(res, re)
		repl = append(repl, r.Replacement)
	}
	if len(res) == 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		s := line
		for j := range res {
			s = res[j].ReplaceAllString(s, repl[j])
		}
		out[i] = s
	}
	return out
}

func applyMatchOutput(blob string, rules []MatchOutputRule) ([]byte, bool) {
	for _, m := range rules {
		pat := strings.TrimSpace(m.Pattern)
		if pat == "" {
			continue
		}
		re, err := regexp.Compile(pat)
		if err != nil || !re.MatchString(blob) {
			continue
		}
		unless := strings.TrimSpace(m.Unless)
		if unless != "" {
			u, err := regexp.Compile(unless)
			if err == nil && u.MatchString(blob) {
				continue
			}
		}
		return []byte(m.Message), true
	}
	return nil, false
}

func compileLineRegexes(patterns []string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			continue
		}
		out = append(out, re)
	}
	return out
}

func filterStripLines(lines []string, patterns []string) []string {
	res := compileLineRegexes(patterns)
	if len(res) == 0 {
		return lines
	}
	var kept []string
line:
	for _, line := range lines {
		for _, re := range res {
			if re.MatchString(line) {
				continue line
			}
		}
		kept = append(kept, line)
	}
	return kept
}

func filterKeepLines(lines []string, patterns []string) []string {
	res := compileLineRegexes(patterns)
	if len(res) == 0 {
		return lines
	}
	var kept []string
line:
	for _, line := range lines {
		for _, re := range res {
			if re.MatchString(line) {
				kept = append(kept, line)
				continue line
			}
		}
	}
	return kept
}

func truncateEachLine(lines []string, maxRunes int) []string {
	if maxRunes <= 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if utf8.RuneCountInString(line) <= maxRunes {
			out[i] = line
			continue
		}
		runes := []rune(line)
		out[i] = string(runes[:maxRunes])
	}
	return out
}

func truncateTOMLLinesPreservingEvidence(lines []string, maxLines int, mode string) []string {
	if maxLines <= 0 || len(lines) <= maxLines {
		return lines
	}
	budget := maxLines
	includeMarker := maxLines > 1
	if includeMarker {
		budget--
	}
	if budget <= 0 {
		return lines[:maxLines]
	}
	selected := make(map[int]struct{}, budget)
	for i, line := range lines {
		if len(selected) >= budget {
			break
		}
		if importantTOMLLine(line) {
			selected[i] = struct{}{}
		}
	}
	for _, idx := range preferredTOMLIndexes(len(lines), budget, mode) {
		if len(selected) >= budget {
			break
		}
		selected[idx] = struct{}{}
	}
	out := make([]string, 0, maxLines)
	for i, line := range lines {
		if _, ok := selected[i]; ok {
			out = append(out, line)
		}
	}
	if includeMarker {
		marker := fmt.Sprintf("... +%d omitted line(s) (evidence-first cap)", len(lines)-len(out))
		out = append(out, marker)
	}
	return out
}

func preferredTOMLIndexes(total, budget int, mode string) []int {
	if total <= 0 || budget <= 0 {
		return nil
	}
	if total <= budget {
		out := make([]int, total)
		for i := range out {
			out[i] = i
		}
		return out
	}
	switch mode {
	case "tail":
		out := make([]int, 0, budget)
		for i := total - budget; i < total; i++ {
			out = append(out, i)
		}
		return out
	case "head_tail":
		return cappedEvidenceIndexes(total, budget, min(6, budget/2))
	default:
		out := make([]int, budget)
		for i := range out {
			out[i] = i
		}
		return out
	}
}

func importantTOMLLine(line string) bool {
	if importantLogLine(line) {
		return true
	}
	tl := strings.ToLower(line)
	for _, tok := range []string{
		"cannot", "undefined", "unresolved", "invalid", "denied", "timeout",
		"timed out", "not found", "no such file", "crash", "segfault", "oom",
		"out of memory", "abort", "diagnostic", "violation", "problem", "issue",
		"destroy", "destroyed", "delete", "deleted", "deleting", "replacement",
		"replace", "tainted", "deposed", "drift", "crashloop", "unhealthy",
		"forbidden", "unauthorized", "permission denied", "connection refused",
	} {
		if strings.Contains(tl, tok) {
			return true
		}
	}
	return false
}

package filter

import (
	"embed"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// builtinsTOMLFS embeds the built-in filter catalog under
// internal/filter/builtins_toml/. Each file is one TOML document with a single
// [filters.NAME] section plus optional [[tests.NAME]] blocks that are ignored
// by FilterRule decoding and used as live snapshot fixtures.
//
//go:embed builtins_toml/*.toml
var builtinsTOMLFS embed.FS

// compiledBuiltinTOML is a parsed embedded filter with its regex
// pre-compiled. Stored once at package init, accessed lock-free on the
// hot path.
type compiledBuiltinTOML struct {
	name       string
	rule       FilterRule
	re         *regexp.Regexp
	sourceTOML string // for diagnostics
}

var (
	builtinsTOMLOnce sync.Once
	builtinsTOMLAll  []compiledBuiltinTOML
	builtinsTOMLFSys fs.FS = builtinsTOMLFS
)

// loadedBuiltinTOMLs returns every embedded TOML filter, parsed and ready for
// matching. The slice order is deterministic (alphabetical by filename) so two
// binary builds match the same command in the same way.
func loadedBuiltinTOMLs() []compiledBuiltinTOML {
	builtinsTOMLOnce.Do(func() {
		entries, err := fs.ReadDir(builtinsTOMLFSys, "builtins_toml")
		if err != nil {
			return
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, fname := range names {
			data, err := fs.ReadFile(builtinsTOMLFSys, "builtins_toml/"+fname)
			if err != nil {
				continue
			}
			var f FiltersFile
			if _, err := toml.Decode(string(data), &f); err != nil {
				continue
			}
			ruleNames := make([]string, 0, len(f.Filters))
			for n := range f.Filters {
				ruleNames = append(ruleNames, n)
			}
			sort.Strings(ruleNames)
			for _, n := range ruleNames {
				r := f.Filters[n]
				if strings.TrimSpace(r.MatchCommand) == "" {
					continue
				}
				re, err := regexp.Compile(r.MatchCommand)
				if err != nil {
					continue
				}
				builtinsTOMLAll = append(builtinsTOMLAll, compiledBuiltinTOML{
					name:       n,
					rule:       r,
					re:         re,
					sourceTOML: fname,
				})
			}
		}
	})
	return builtinsTOMLAll
}

// FirstMatchingBuiltinTOMLRule returns the first embedded TOML filter whose
// match_command regex matches the joined argv. Used by the Layer-0 pipeline
// between Go built-ins and user TOML so the catalog ships out-of-box, no
// config required.
func FirstMatchingBuiltinTOMLRule(argv []string) (string, *FilterRule) {
	cmd := strings.Join(argv, " ")
	for _, b := range loadedBuiltinTOMLs() {
		if b.re.MatchString(cmd) {
			cp := b.rule
			return b.name, &cp
		}
	}
	return "", nil
}

// BuiltinTOMLCount reports how many embedded filter rules were loaded.
// Used by diagnostics (slimference doctor) and tests.
func BuiltinTOMLCount() int {
	return len(loadedBuiltinTOMLs())
}

// BuiltinTOMLNames returns the sorted list of embedded filter names.
// For diagnostics / `slimference debug parsers`.
func BuiltinTOMLNames() []string {
	all := loadedBuiltinTOMLs()
	names := make([]string, 0, len(all))
	for _, b := range all {
		names = append(names, b.name)
	}
	return names
}

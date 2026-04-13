package filter

import (
	"path/filepath"
)

// ProjectFiltersPath returns <wd>/.tokenproxy/filters.toml.
func ProjectFiltersPath(wd string) string {
	if wd == "" {
		return ""
	}
	return filepath.Join(wd, ".tokenproxy", "filters.toml")
}

// LoadProjectDenyPatterns reads deny_patterns from the project filters file only (see LoadMergedDenyPatterns for project+user).
func LoadProjectDenyPatterns(wd string) []string {
	p := ProjectFiltersPath(wd)
	f, err := LoadFiltersFile(p)
	if err != nil || f == nil {
		return nil
	}
	return f.DenyPatterns
}

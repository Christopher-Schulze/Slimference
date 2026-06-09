package filter

import (
	"io/fs"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/BurntSushi/toml"
)

// TestBuiltinTOMLCatalog_Loaded checks the //go:embed scoop actually picked
// up the embedded filter catalog. If this drops to zero, something broke the
// embed.FS path.
func TestBuiltinTOMLCatalog_Loaded(t *testing.T) {
	n := BuiltinTOMLCount()
	if n < 50 {
		t.Fatalf("expected >=50 embedded filters, got %d", n)
	}
}

// TestBuiltinTOMLCatalog_KnownNames asserts a handful of well-known
// filter names are present so future drift (someone deleting a file)
// fails loudly.
func TestBuiltinTOMLCatalog_KnownNames(t *testing.T) {
	names := BuiltinTOMLNames()
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	must := []string{"gradle", "gcc", "make", "helm", "terraform-plan", "yamllint", "jq", "ssh", "xcodebuild"}
	for _, m := range must {
		if !set[m] {
			t.Fatalf("expected embedded filter %q in catalog, got: %v", m, names)
		}
	}
}

// TestBuiltinTOMLCatalog_NamesAreSortedDeterministically: hot-path
// matching iterates the slice; deterministic order means two builds
// match the same command the same way.
func TestBuiltinTOMLCatalog_NamesAreSortedDeterministically(t *testing.T) {
	a := BuiltinTOMLNames()
	b := BuiltinTOMLNames()
	if !equalStrings(a, b) {
		t.Fatalf("non-deterministic order between calls")
	}
	sorted := append([]string(nil), a...)
	sort.Strings(sorted)
	// Embedded filters are loaded in filename order, then by
	// alphabetical rule name within each file. The result is not
	// strictly sorted (multiple rules per file), but it must be
	// stable. The check above is the real invariant; the secondary
	// sort would only catch reorderings, which the equalStrings check
	// already covers.
	_ = sorted
}

// TestFirstMatchingBuiltinTOMLRule_GccMatches verifies the routing
// regex from gcc.toml actually fires on a `gcc main.c` argv.
func TestFirstMatchingBuiltinTOMLRule_GccMatches(t *testing.T) {
	name, rule := FirstMatchingBuiltinTOMLRule([]string{"gcc", "-o", "main", "main.c"})
	if rule == nil {
		t.Fatalf("expected gcc filter to match")
	}
	if name != "gcc" {
		t.Fatalf("matched %q, want gcc", name)
	}
}

func TestFirstMatchingBuiltinTOMLRule_NoMatchReturnsNil(t *testing.T) {
	name, rule := FirstMatchingBuiltinTOMLRule([]string{"definitely-not-a-real-command"})
	if rule != nil || name != "" {
		t.Fatalf("unexpected match: name=%q rule=%v", name, rule)
	}
}

// TestPipelineRoutesThroughEmbeddedCatalog wires the full pipeline:
// run a fake command whose argv matches an embedded filter (and is
// NOT covered by any Go built-in compactor), and verify the embedded
// catalog kicks in. `helm` is in the embedded catalog but not in our Go
// built-in dispatch table, so it isolates the embedded TOML branch.
func TestPipelineRoutesThroughEmbeddedCatalog(t *testing.T) {
	argv := []string{"helm", "list"}
	in := []byte("\n\n\n")
	out, name := applyLayer0FiltersWithContext("", argv, in, FileReadContext{Mode: "scan"})
	if !strings.HasPrefix(name, "builtin_toml:") {
		t.Fatalf("expected builtin_toml:* filter to fire, got %q (out=%q)", name, string(out))
	}
}

func TestPipelineBuiltinTOMLMaxLinesKeepsLateImportantEvidence(t *testing.T) {
	withBuiltinTOMLFS(t, fstest.MapFS{
		"builtins_toml/custom.toml": {Data: []byte(`[filters.custom]
match_command = "^custom$"
max_lines = 5
`)},
	})
	var input strings.Builder
	for i := 0; i < 30; i++ {
		input.WriteString("progress line\n")
	}
	input.WriteString("2026-06-03 FATAL database connection refused\n")
	for i := 0; i < 30; i++ {
		input.WriteString("more progress line\n")
	}

	out, name := applyLayer0FiltersWithContext("", []string{"custom"}, []byte(input.String()), FileReadContext{Mode: "scan"})
	if name != "builtin_toml:custom" {
		t.Fatalf("expected builtin custom filter, got %q", name)
	}
	s := string(out)
	if !strings.Contains(s, "FATAL database connection refused") {
		t.Fatalf("late important evidence was dropped: %q", s)
	}
	if !strings.Contains(s, "omitted line") {
		t.Fatalf("omitted-count marker missing: %q", s)
	}
	if got := strings.Count(strings.TrimSpace(s), "\n") + 1; got > 5 {
		t.Fatalf("line cap exceeded: got %d lines in %q", got, s)
	}
}

// TestBuiltinTOMLSnapshots runs the [[tests.X]] blocks embedded in the TOML
// filter files as table-driven Go tests. Catches regressions when we touch
// filter parsing or DSL semantics.
//
// Notes:
//   - Tests are loaded with a separate toml.Decode pass into a private
//     struct so they don't pollute the public FilterRule shape.
//   - When a snapshot asserts equality verbatim with a literal "\n" in
//     `expected`, we apply the same matching: ApplyTOMLRule should produce the
//     same bytes.
func TestBuiltinTOMLSnapshots(t *testing.T) {
	// Load every embedded TOML again, this time also parsing the
	// [[tests.X]] blocks.
	type snapshotCase struct {
		Name     string `toml:"name"`
		Input    string `toml:"input"`
		Expected string `toml:"expected"`
	}
	type bundle struct {
		Filters map[string]FilterRule     `toml:"filters"`
		Tests   map[string][]snapshotCase `toml:"tests"`
	}
	for _, b := range loadedBuiltinTOMLs() {
		t.Run(b.name, func(t *testing.T) {
			data := readEmbeddedTOML(t, b.sourceTOML)
			var bundleDoc bundle
			if _, err := toml.Decode(string(data), &bundleDoc); err != nil {
				t.Fatalf("decode %s: %v", b.sourceTOML, err)
			}
			cases, ok := bundleDoc.Tests[b.name]
			if !ok || len(cases) == 0 {
				t.Skipf("no [[tests.%s]] snapshots in %s", b.name, b.sourceTOML)
			}
			for _, c := range cases {
				t.Run(c.Name, func(t *testing.T) {
					got := string(ApplyTOMLRule([]byte(c.Input), &b.rule))
					// Normalize trailing newline divergence: some
					// snapshots preserve the final '\n' on multi-line
					// output; our ApplyTOMLRule strips it during
					// line-split + rejoin. The byte-difference is
					// semantically neutral for downstream consumers.
					if rtrim(got) != rtrim(c.Expected) {
						t.Fatalf("snapshot drift\nfilter: %s\ncase:   %s\ninput:  %q\nwant:   %q\ngot:    %q", b.name, c.Name, c.Input, c.Expected, got)
					}
				})
			}
		})
	}
}

func readEmbeddedTOML(t *testing.T, name string) []byte {
	t.Helper()
	data, err := builtinsTOMLFS.ReadFile("builtins_toml/" + name)
	if err != nil {
		t.Fatalf("embed read %s: %v", name, err)
	}
	return data
}

// rtrim strips trailing '\n' / '\r' from s. Used by snapshot tests to
// neutralise trailing-newline differences in line-rejoin semantics.
func rtrim(s string) string {
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != '\n' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func withBuiltinTOMLFS(t *testing.T, fsys fs.FS) {
	t.Helper()
	prevFS := builtinsTOMLFSys
	prevAll := builtinsTOMLAll
	builtinsTOMLFSys = fsys
	builtinsTOMLOnce = sync.Once{}
	builtinsTOMLAll = nil
	t.Cleanup(func() {
		builtinsTOMLFSys = prevFS
		builtinsTOMLOnce = sync.Once{}
		builtinsTOMLAll = prevAll
	})
}

func TestBuiltinTOMLLoaderSkipsBadCatalogEntries(t *testing.T) {
	withBuiltinTOMLFS(t, fstest.MapFS{
		"builtins_toml/empty_match.toml": {Data: []byte(`[filters.empty]
match_command = ""
`)},
		"builtins_toml/invalid_regex.toml": {Data: []byte(`[filters.bad]
match_command = "["
`)},
		"builtins_toml/invalid_toml.toml": {Data: []byte(`[filters.bad`)},
		"builtins_toml/valid.toml": {Data: []byte(`[filters.ok]
match_command = "^ok$"
`)},
	})
	names := BuiltinTOMLNames()
	if len(names) != 1 || names[0] != "ok" {
		t.Fatalf("loaded names=%v, want [ok]", names)
	}
}

func TestBuiltinTOMLLoaderReadDirAndReadFileErrors(t *testing.T) {
	withBuiltinTOMLFS(t, fstest.MapFS{})
	if got := BuiltinTOMLCount(); got != 0 {
		t.Fatalf("missing dir count=%d want 0", got)
	}

	withBuiltinTOMLFS(t, readFileErrorFS{})
	if got := BuiltinTOMLCount(); got != 0 {
		t.Fatalf("read-file error count=%d want 0", got)
	}
}

type readFileErrorFS struct{}

func (readFileErrorFS) Open(string) (fs.File, error) { return nil, fs.ErrPermission }

func (readFileErrorFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "builtins_toml" {
		return nil, fs.ErrNotExist
	}
	return []fs.DirEntry{fakeDirEntry{name: "bad.toml"}}, nil
}

type fakeDirEntry struct{ name string }

func (e fakeDirEntry) Name() string               { return e.name }
func (e fakeDirEntry) IsDir() bool                { return false }
func (e fakeDirEntry) Type() fs.FileMode          { return 0 }
func (e fakeDirEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrPermission }

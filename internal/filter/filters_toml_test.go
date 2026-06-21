package filter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMergedDenyPatterns_dedupe(t *testing.T) {
	t.Setenv("SLIMFERENCE_TRUST_PROJECT_FILTERS", "1")
	tmp := t.TempDir()
	// same path if wd is home — use subdir for project
	proj := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(proj, ".slimference"), 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(proj, ".slimference", "filters.toml")
	if err := os.WriteFile(p, []byte("deny_patterns = ['^x']\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := LoadMergedDenyPatterns(proj)
	if len(got) != 1 {
		t.Fatalf("%#v", got)
	}
}

func TestFirstMatchingTOMLRule_andApply(t *testing.T) {
	t.Setenv("SLIMFERENCE_TRUST_PROJECT_FILTERS", "1")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".slimference"), 0755); err != nil {
		t.Fatal(err)
	}
	content := `
schema_version = 1

[filters.testecho]
match_command = "^echo\\s+hi"
max_lines = 2
on_empty = "[empty]"
`
	if err := os.WriteFile(filepath.Join(dir, ".slimference", "filters.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	rule := FirstMatchingTOMLRule(dir, []string{"echo", "hi", "a", "b", "c"})
	if rule == nil || rule.MaxLines != 2 {
		t.Fatalf("rule=%v", rule)
	}
	out := ApplyTOMLRule([]byte("L1\nL2\nL3\n"), rule)
	if string(out) != "L1\nL2" {
		t.Fatalf("%q", out)
	}
	out2 := ApplyTOMLRule([]byte("  \n"), &FilterRule{OnEmpty: "E"})
	if string(out2) != "E" {
		t.Fatalf("%q", out2)
	}
}

func TestApplyTOMLRule_stripLines(t *testing.T) {
	t.Parallel()
	r := &FilterRule{StripLinesMatching: []string{`^DROP`}}
	out := ApplyTOMLRule([]byte("KEEP\nDROP\n"), r)
	if string(out) != "KEEP\n" {
		t.Fatalf("%q", out)
	}
}

func TestApplyTOMLRule_replaceAndMatchOutput(t *testing.T) {
	t.Parallel()
	r := &FilterRule{
		Replace: []ReplacePair{
			{Pattern: `^PREFIX\s*`, Replacement: ""},
		},
		MatchOutput: []MatchOutputRule{
			{Pattern: `^BUILD SUCCESS$`, Message: "[ok]"},
			{Pattern: `NOPE`, Message: "bad", Unless: `ALWAYS`},
		},
	}
	out := ApplyTOMLRule([]byte("PREFIXBUILD SUCCESS"), r)
	if string(out) != "[ok]" {
		t.Fatalf("short-circuit: %q", out)
	}
	out2 := ApplyTOMLRule([]byte("PREFIXx\ny"), r)
	if string(out2) != "x\ny" {
		t.Fatalf("replace only: %q", out2)
	}
}

func TestApplyTOMLRule_keepLinesTruncateHeadTail(t *testing.T) {
	t.Parallel()
	r := &FilterRule{
		KeepLinesMatching: []string{`ERR|^keep`},
		TruncateLinesAt:   4,
		HeadLines:         2,
		MaxLines:          1,
	}
	out := ApplyTOMLRule([]byte("ERR longline\nnoise\nkeep short"), r)
	// keep: ERR longline, keep short; head 2; truncate ERR l -> ERR ; max_lines 1
	if string(out) != "ERR " {
		t.Fatalf("%q", out)
	}
	r2 := &FilterRule{TailLines: 2, MaxLines: 1}
	out2 := ApplyTOMLRule([]byte("a\nb\nc\nd"), r2)
	if string(out2) != "c" {
		t.Fatalf("tail then max: %q", out2)
	}
}

func TestApplyBuiltinTOMLRulePreservesLateInfraEvidence(t *testing.T) {
	t.Parallel()
	r := &FilterRule{MaxLines: 5}
	var input strings.Builder
	for range 30 {
		input.WriteString("info neutral line\n")
	}
	input.WriteString("module.db.aws_instance.main will be destroyed and replaced\n")
	input.WriteString("module.cache.aws_elasticache_replication_group.primary is tainted\n")
	input.WriteString("connection refused while checking provider plugin\n")
	out := string(ApplyBuiltinTOMLRule([]byte(input.String()), r))
	for _, want := range []string{"destroyed", "tainted", "connection refused", "evidence-first cap"} {
		if !strings.Contains(out, want) {
			t.Fatalf("late infra evidence %q was not preserved: %q", want, out)
		}
	}
	if len(strings.Split(out, "\n")) > 5 {
		t.Fatalf("builtin cap exceeded max lines: %q", out)
	}
}

// TestLoadFiltersFile_emptyPath covers the path=="" early return.
func TestLoadFiltersFile_emptyPath(t *testing.T) {
	t.Parallel()
	f, err := LoadFiltersFile("")
	if err != nil || f != nil {
		t.Fatalf("empty path: f=%v err=%v", f, err)
	}
}

// TestLoadFiltersFile_invalidTOML covers the toml.Decode error return.
func TestLoadFiltersFile_invalidTOML(t *testing.T) {
	tmp := t.TempDir()
	p := tmp + "/bad.toml"
	if err := os.WriteFile(p, []byte("[proxy\nbad = "), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFiltersFile(p)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

// TestApplyTOMLRule_stripANSI covers the rule.StripANSI branch.
func TestApplyTOMLRule_stripANSI(t *testing.T) {
	t.Parallel()
	r := &FilterRule{StripANSI: true}
	input := "\x1b[31mred\x1b[0m plain"
	out := ApplyTOMLRule([]byte(input), r)
	if string(out) != "red plain" {
		t.Fatalf("StripANSI: %q", out)
	}
}

// TestApplyTOMLRule_nilRule covers the rule==nil early return.
func TestApplyTOMLRule_nilRule(t *testing.T) {
	t.Parallel()
	in := []byte("data")
	out := ApplyTOMLRule(in, nil)
	if string(out) != "data" {
		t.Fatalf("nil rule: %q", out)
	}
}

// TestTruncateEachLine_zeroMax covers the maxRunes<=0 early return.
func TestTruncateEachLine_zeroMax(t *testing.T) {
	t.Parallel()
	lines := []string{"hello", "world"}
	out := truncateEachLine(lines, 0)
	if len(out) != 2 || out[0] != "hello" {
		t.Fatalf("zero max: %v", out)
	}
}

// TestCompileLineRegexes_badPattern covers the err!=nil skip.
func TestCompileLineRegexes_badPattern(t *testing.T) {
	t.Parallel()
	res := compileLineRegexes([]string{"valid.*", "[invalid", "also valid"})
	if len(res) != 2 {
		t.Fatalf("expected 2 valid regexes, got %d", len(res))
	}
}

// TestFirstMatchingTOMLRule_emptyMatchCommand covers the TrimSpace=="" continue.
func TestFirstMatchingTOMLRule_emptyMatchCommand(t *testing.T) {
	t.Setenv("SLIMFERENCE_TRUST_PROJECT_FILTERS", "1")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".slimference"), 0755); err != nil {
		t.Fatal(err)
	}
	content := `
schema_version = 1

[filters.empty_cmd]
match_command = "   "
on_empty = "SHOULD_NOT_MATCH"

[filters.real_cmd]
match_command = "^echo"
on_empty = "MATCHED"
`
	if err := os.WriteFile(filepath.Join(dir, ".slimference", "filters.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	rule := FirstMatchingTOMLRule(dir, []string{"echo", "hi"})
	if rule == nil || rule.OnEmpty != "MATCHED" {
		t.Fatalf("expected real_cmd rule, got %v", rule)
	}
}

// TestApplyReplacePerLine_badRegex covers the regexp.Compile error skip.
func TestApplyReplacePerLine_badRegex(t *testing.T) {
	t.Parallel()
	r := &FilterRule{
		Replace: []ReplacePair{
			{Pattern: "[invalid", Replacement: "x"},
			{Pattern: `^hello`, Replacement: "bye"},
		},
	}
	out := ApplyTOMLRule([]byte("hello world"), r)
	if string(out) != "bye world" {
		t.Fatalf("bad regex skipped: %q", out)
	}
}

// TestApplyMatchOutput_badUnlessRegex covers the Unless compile error fallthrough.
func TestApplyMatchOutput_badUnlessRegex(t *testing.T) {
	t.Parallel()
	r := &FilterRule{
		MatchOutput: []MatchOutputRule{
			{Pattern: `ok`, Message: "[ok]", Unless: "[invalid"},
		},
	}
	// Unless has bad regex → compile error → unless check skipped → message returned.
	out := ApplyTOMLRule([]byte("ok"), r)
	if string(out) != "[ok]" {
		t.Fatalf("bad unless: %q", out)
	}
}

// TestUniqueFilterPaths_dedup covers the seen[p] duplicate skip.
func TestUniqueFilterPaths_dedup(t *testing.T) {
	t.Parallel()
	// Both project and user paths resolve to same dir only if we set both to same temp dir.
	// We can test the dedup by verifying that uniqueFilterPaths never returns duplicates.
	paths := uniqueFilterPaths(t.TempDir())
	seen := make(map[string]bool)
	for _, p := range paths {
		if seen[p] {
			t.Fatalf("duplicate path: %q", p)
		}
		seen[p] = true
	}
}

// TestUniqueFilterPaths_deduplication triggers the seen[p] duplicate path when project == user dir.
func TestUniqueFilterPaths_deduplication(t *testing.T) {
	// Not parallel — sets HOME env var
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// ProjectFiltersPath(dir) and UserFiltersPath() both resolve to dir/.slimference/filters.toml
	paths := uniqueFilterPaths(dir)
	if len(paths) != 1 {
		t.Fatalf("expected 1 unique path, got %d: %v", len(paths), paths)
	}
}

// TestFirstMatchingTOMLRule_invalidRegex covers the regexp.Compile error path (skip and continue).
func TestFirstMatchingTOMLRule_invalidRegex(t *testing.T) {
	t.Setenv("SLIMFERENCE_TRUST_PROJECT_FILTERS", "1")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".slimference"), 0755); err != nil {
		t.Fatal(err)
	}
	content := `
schema_version = 1

[filters.aaa_bad]
match_command = "["
on_empty = "BAD"

[filters.zzz_good]
match_command = "^echo"
on_empty = "GOOD"
`
	if err := os.WriteFile(filepath.Join(dir, ".slimference", "filters.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// aaa_bad sorts before zzz_good; bad regex compile fails → skip → zzz_good matches
	rule := FirstMatchingTOMLRule(dir, []string{"echo", "hi"})
	if rule == nil || rule.OnEmpty != "GOOD" {
		t.Fatalf("expected zzz_good rule, got %v", rule)
	}
}

// TestApplyTOMLRule_headLinesExecute covers the lines[:HeadLines] truncation path.
func TestApplyTOMLRule_headLinesExecute(t *testing.T) {
	t.Parallel()
	r := &FilterRule{HeadLines: 1}
	out := ApplyTOMLRule([]byte("a\nb\nc"), r)
	if string(out) != "a" {
		t.Fatalf("HeadLines=1 with 3 lines: %q", out)
	}
}

// TestTruncateEachLine_withinLimit covers the continue path (line <= maxRunes).
func TestTruncateEachLine_withinLimit(t *testing.T) {
	t.Parallel()
	lines := []string{"hi", "toolongstring"}
	out := truncateEachLine(lines, 4)
	if out[0] != "hi" {
		t.Fatalf("short line should be unchanged: %q", out[0])
	}
	if out[1] != "tool" {
		t.Fatalf("long line should be truncated: %q", out[1])
	}
}

// TestApplyReplacePerLine_emptyPattern covers the empty pattern skip and len(res)==0 return.
func TestApplyReplacePerLine_emptyPattern(t *testing.T) {
	t.Parallel()
	// All patterns empty → all skipped → len(res)==0 → return lines unchanged
	r := &FilterRule{
		Replace: []ReplacePair{
			{Pattern: "", Replacement: "x"},
		},
	}
	out := ApplyTOMLRule([]byte("hello"), r)
	if string(out) != "hello" {
		t.Fatalf("empty pattern: %q", out)
	}
}

// TestApplyMatchOutput_emptyPattern covers the empty pattern skip inside applyMatchOutput.
func TestApplyMatchOutput_emptyPattern(t *testing.T) {
	t.Parallel()
	r := &FilterRule{
		MatchOutput: []MatchOutputRule{
			{Pattern: "", Message: "SHOULD_NOT_MATCH"},
			{Pattern: `ok`, Message: "[ok]"},
		},
	}
	out := ApplyTOMLRule([]byte("ok"), r)
	if string(out) != "[ok]" {
		t.Fatalf("empty pattern must be skipped: %q", out)
	}
}

// TestApplyMatchOutput_unlessMatch covers the Unless condition where Unless matches the blob.
func TestApplyMatchOutput_unlessMatch(t *testing.T) {
	t.Parallel()
	r := &FilterRule{
		MatchOutput: []MatchOutputRule{
			{Pattern: `err`, Message: "ERROR", Unless: `safe`},
		},
	}
	// blob contains "err" (pattern matches) but also "safe" (unless matches) → message skipped
	out := ApplyTOMLRule([]byte("safe err"), r)
	if string(out) == "ERROR" {
		t.Fatal("unless match should skip the message")
	}
}

// TestCompileLineRegexes_emptyPattern covers the empty string skip inside compileLineRegexes.
func TestCompileLineRegexes_emptyPattern(t *testing.T) {
	t.Parallel()
	res := compileLineRegexes([]string{"", "valid.*", ""})
	if len(res) != 1 {
		t.Fatalf("expected 1 valid regex (2 empty skipped), got %d", len(res))
	}
}

// TestLoadFiltersFile_readError covers the non-NotExist ReadFile error path.
func TestLoadFiltersFile_readError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Create a directory at the file path — ReadFile returns an error that is NOT IsNotExist
	dirPath := filepath.Join(dir, "is-a-dir")
	if err := os.Mkdir(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFiltersFile(dirPath)
	if err == nil {
		t.Fatal("expected error reading a directory as a file")
	}
}

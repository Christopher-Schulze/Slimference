package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseFnForTest(t *testing.T, src string) (*ast.FuncDecl, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "stub.go", "package x\n"+src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			return fn, fset
		}
	}
	t.Fatalf("no func found in %q", src)
	return nil, nil
}

func TestClassifyTryCompactFunc_EmptyOnly(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(argv []string, stdout []byte) ([]byte, bool) {
		return tryCompactEmptyStdoutSingleBinary(argv, stdout, "x", "[x]")
	}`)
	cat, _, _ := classifyTryCompactFunc(fn, fset)
	if cat != LeafEmptyOnly {
		t.Fatalf("expected empty_only_stub, got %s", cat)
	}
}

func TestClassifyTryCompactFunc_RealParser_HelperPrefix(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(stdout []byte) ([]byte, bool) {
		s := string(stdout)
		out, ok := extractBuildErrors(s, "x")
		_ = ok
		return []byte(out), ok
	}`)
	cat, _, helpers := classifyTryCompactFunc(fn, fset)
	if cat != LeafRealParser {
		t.Fatalf("expected real_parser, got %s", cat)
	}
	if len(helpers) == 0 || helpers[0] != "extractBuildErrors" {
		t.Fatalf("expected extractBuildErrors helper, got %v", helpers)
	}
}

func TestClassifyTryCompactFunc_Mixed(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(argv []string, stdout []byte) ([]byte, bool) {
		if out, ok := tryCompactEmptyStdoutSingleBinary(argv, stdout, "x", "[x]"); ok {
			return out, true
		}
		out, ok := extractBuildErrors(string(stdout), "x")
		return []byte(out), ok
	}`)
	cat, _, _ := classifyTryCompactFunc(fn, fset)
	if cat != LeafMixed {
		t.Fatalf("expected mixed, got %s", cat)
	}
}

func TestClassifyTryCompactFunc_Fallback_TinyBody(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(argv []string, stdout []byte) ([]byte, bool) {
		return stdout, false
	}`)
	cat, _, _ := classifyTryCompactFunc(fn, fset)
	if cat != LeafFallback {
		t.Fatalf("expected fallback for trivial body, got %s", cat)
	}
}

func TestClassifyTryCompactFunc_Fallback_NoSignal(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(argv []string, stdout []byte) ([]byte, bool) {
		_ = argv
		_ = stdout
		x := 1
		y := 2
		_ = x + y
		_ = x * y
		_ = y / x
		return stdout, false
	}`)
	cat, _, _ := classifyTryCompactFunc(fn, fset)
	if cat != LeafFallback {
		t.Fatalf("expected fallback when no parser signal, got %s", cat)
	}
}

func TestClassifyTryCompactFunc_RealParser_InlineSignal(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(stdout []byte) ([]byte, bool) {
		s := strings.TrimSpace(string(stdout))
		_ = s
		parts := strings.Split(s, "\n")
		_ = parts
		x := 1
		_ = x
		return stdout, false
	}`)
	cat, _, _ := classifyTryCompactFunc(fn, fset)
	if cat != LeafRealParser {
		t.Fatalf("expected real_parser via inline signal, got %s", cat)
	}
}

func TestClassifyTryCompactFunc_NoBody(t *testing.T) {
	t.Parallel()
	fn := &ast.FuncDecl{Body: nil, Name: ast.NewIdent("TryCompactX")}
	cat, _, _ := classifyTryCompactFunc(fn, token.NewFileSet())
	if cat != LeafFallback {
		t.Fatalf("expected fallback when body is nil, got %s", cat)
	}
}

func TestClassifyTryCompactFunc_ExtractPrefix(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(stdout []byte) ([]byte, bool) {
		out := extractWhatever(string(stdout))
		return []byte(out), true
	}`)
	cat, _, helpers := classifyTryCompactFunc(fn, fset)
	if cat != LeafRealParser {
		t.Fatalf("expected real_parser via extract* prefix, got %s", cat)
	}
	if len(helpers) == 0 {
		t.Fatalf("expected helper to be recorded")
	}
}

func TestClassifyTryCompactFunc_CompressPrefix(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(stdout []byte) ([]byte, bool) {
		out := compressTraceback(stdout)
		return out, true
	}`)
	cat, _, _ := classifyTryCompactFunc(fn, fset)
	if cat != LeafRealParser {
		t.Fatalf("expected real_parser via compress* prefix, got %s", cat)
	}
}

func TestClassifyTryCompactFunc_CompactPrefix(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(stdout []byte) ([]byte, bool) {
		out := compactRows(stdout)
		return out, true
	}`)
	cat, _, _ := classifyTryCompactFunc(fn, fset)
	if cat != LeafRealParser {
		t.Fatalf("expected real_parser via compact* prefix, got %s", cat)
	}
}

func TestClassifyTryCompactFunc_RegexMatch(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(stdout []byte) ([]byte, bool) {
		if !someRegex.Match(stdout) {
			return stdout, false
		}
		_ = stdout
		x := 1
		_ = x
		return stdout, true
	}`)
	cat, _, _ := classifyTryCompactFunc(fn, fset)
	if cat != LeafRealParser {
		t.Fatalf("expected real_parser via Match call, got %s", cat)
	}
}

func TestClassifyTryCompactFunc_DetectBuildSuccessIsParser(t *testing.T) {
	t.Parallel()
	fn, fset := parseFnForTest(t, `func TryCompactX(stdout []byte) ([]byte, bool) {
		if detectBuildSuccess(string(stdout)) {
			return []byte("[x] ok\n"), true
		}
		x := 1
		_ = x
		return stdout, false
	}`)
	cat, _, _ := classifyTryCompactFunc(fn, fset)
	if cat != LeafRealParser {
		t.Fatalf("expected real_parser via detectBuildSuccess, got %s", cat)
	}
}

func TestAuditFilterPackage_RealRepo(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	report, err := AuditFilterPackage(root)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if report.Total == 0 {
		t.Fatalf("expected non-zero total leaves in real repo")
	}
	if report.EmptyOnlyPct > 30 {
		t.Fatalf("regression: empty-only ratio %.1f%% exceeds 30%% threshold", report.EmptyOnlyPct)
	}
}

func TestAuditFilterPackage_BadRoot(t *testing.T) {
	t.Parallel()
	_, err := AuditFilterPackage(filepath.Join(t.TempDir(), "no-such"))
	if err == nil {
		t.Fatalf("expected error for missing root")
	}
}

func TestAuditFilterPackage_ParseError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "filter")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "builtin_broken.go"), []byte("not go code at all"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := AuditFilterPackage(root); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestAuditFilterPackage_EmptyDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "filter")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	report, err := AuditFilterPackage(root)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if report.Total != 0 {
		t.Fatalf("expected zero leaves in empty dir, got %d", report.Total)
	}
	if report.EmptyOnlyPct != 0 {
		t.Fatalf("expected zero pct on empty dir, got %.1f", report.EmptyOnlyPct)
	}
}

func TestAuditFilterPackage_SkipsTestFilesAndNonBuiltin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "filter")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	src := `package filter
func TryCompactReal(stdout []byte) ([]byte, bool) {
	out := extractStuff(string(stdout))
	return []byte(out), true
}
`
	if err := os.WriteFile(filepath.Join(dir, "builtin_real.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "builtin_real_test.go"), []byte("package filter\nfunc TryCompactTestOnly() {}\n"), 0o644); err != nil {
		t.Fatalf("write test: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helpers.go"), []byte("package filter\nfunc TryCompactInHelpers() {}\n"), 0o644); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "builtin_subdir"), []byte("ignored content"), 0o644); err != nil {
		t.Fatalf("write subdir-named file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "actually_subdir"), 0o755); err != nil {
		t.Fatalf("mkdir actual subdir: %v", err)
	}
	report, err := AuditFilterPackage(root)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if report.Total != 1 || report.Entries[0].FuncName != "TryCompactReal" {
		t.Fatalf("expected single non-test builtin entry, got %+v", report.Entries)
	}
}

func TestFormatLeafAuditMarkdown_Renders(t *testing.T) {
	t.Parallel()
	report := LeafAuditReport{
		Root:          "/x",
		Total:         3,
		EmptyOnly:     1,
		RealParser:    2,
		EmptyOnlyPct:  33.33,
		PerFileCounts: map[string]int{"builtin_a.go": 2, "builtin_b.go": 1},
		Entries: []LeafAuditEntry{
			{File: "builtin_a.go", FuncName: "TryCompactA", Category: LeafEmptyOnly, Lines: 4, Notes: "empty"},
			{File: "builtin_a.go", FuncName: "TryCompactB", Category: LeafRealParser, Lines: 25, Notes: "extract|with|pipes"},
			{File: "builtin_b.go", FuncName: "TryCompactC", Category: LeafRealParser, Lines: 12, Notes: "ok"},
		},
	}
	md := FormatLeafAuditMarkdown(report)
	if !strings.Contains(md, "Total `TryCompact*` functions: **3**") {
		t.Fatalf("missing total line, got %q", md)
	}
	if !strings.Contains(md, "Empty-only stubs: **1** (33.3%)") {
		t.Fatalf("missing empty-only summary, got %q", md)
	}
	if !strings.Contains(md, "extract\\|with\\|pipes") {
		t.Fatalf("expected pipe escaping in notes, got %q", md)
	}
}

func TestLeafAuditGate_Pass(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	var stdout, stderr bytes.Buffer
	rc := LeafAuditGate(root, 30, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected pass under 30%% threshold, got %d (stderr=%q)", rc, stderr.String())
	}
}

func TestLeafAuditGate_FailUnderTightThreshold(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	var stdout, stderr bytes.Buffer
	rc := LeafAuditGate(root, 0, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected fail at 0%% threshold")
	}
	if !strings.Contains(stdout.String(), "FAIL") {
		t.Fatalf("expected FAIL marker, got %q", stdout.String())
	}
}

func TestLeafAuditGate_BadRoot(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rc := LeafAuditGate(filepath.Join(t.TempDir(), "no-such"), 30, &stdout, &stderr)
	if rc == 0 {
		t.Fatalf("expected fail on bad root")
	}
}

func TestRunLeafAudit_DefaultText(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	var stdout, stderr bytes.Buffer
	rc := runLeafAudit([]string{"--root=" + root}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected success, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "Layer 0 leaf audit") {
		t.Fatalf("expected markdown header, got %q", stdout.String())
	}
}

func TestRunLeafAudit_JSONFlag(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	var stdout, stderr bytes.Buffer
	rc := runLeafAudit([]string{"--root=" + root, "--json"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected success, got %d", rc)
	}
	var report LeafAuditReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json decode: %v (stdout=%q)", err, stdout.String())
	}
	if report.Total == 0 {
		t.Fatalf("expected non-zero total")
	}
}

func TestRunLeafAudit_WriteMarkdown(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	out := filepath.Join(t.TempDir(), "audit.md")
	var stdout, stderr bytes.Buffer
	rc := runLeafAudit([]string{"--root=" + root, "--write-markdown=" + out}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected success, got %d", rc)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read written: %v", err)
	}
	if !strings.Contains(string(data), "Layer 0 leaf audit") {
		t.Fatalf("expected header in written file")
	}
}

func TestRunLeafAudit_WriteMarkdownBadPath(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	var stdout, stderr bytes.Buffer
	rc := runLeafAudit([]string{"--root=" + root, "--write-markdown=" + filepath.Join(t.TempDir(), "no", "such", "dir", "audit.md")}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("expected 1 on bad write path, got %d", rc)
	}
}

func TestRunLeafAudit_BadFlags(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--bogus"},
		{"--max-empty-only-pct=not-a-number"},
		{"unexpected_positional"},
	} {
		var stdout, stderr bytes.Buffer
		rc := runLeafAudit(args, &stdout, &stderr)
		if rc != 2 {
			t.Fatalf("expected exit 2 for %v, got %d", args, rc)
		}
	}
}

func TestRunLeafAudit_CheckPasses(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	var stdout, stderr bytes.Buffer
	rc := runLeafAudit([]string{"--root=" + root, "--check", "--max-empty-only-pct=30"}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("expected pass, got %d (stderr=%q)", rc, stderr.String())
	}
}

func TestRunLeafAudit_BadRootInReport(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rc := runLeafAudit([]string{"--root=" + filepath.Join(t.TempDir(), "no-such")}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("expected 1 on bad root, got %d", rc)
	}
}

func TestRunLeafAudit_JSONEncodeFailureNotPossible(t *testing.T) {
	t.Parallel()
	// Sanity: marshalling LeafAuditReport never fails on the populated
	// version we ship. Test with empty input.
	var stdout, stderr bytes.Buffer
	rc := runLeafAudit([]string{"--root=" + t.TempDir(), "--json"}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("expected 1 on bad root, got %d", rc)
	}
}

func TestParseFloat_Bad(t *testing.T) {
	t.Parallel()
	if _, err := parseFloat("abc"); err == nil {
		t.Fatalf("expected error for non-numeric input")
	}
}

func TestParseFloat_Good(t *testing.T) {
	t.Parallel()
	v, err := parseFloat("12.5")
	if err != nil || v != 12.5 {
		t.Fatalf("expected 12.5, got %v err=%v", v, err)
	}
}

func TestAppendUnique(t *testing.T) {
	t.Parallel()
	got := appendUnique([]string{"a", "b"}, "a")
	if len(got) != 2 {
		t.Fatalf("expected dedup, got %v", got)
	}
	got = appendUnique([]string{"a"}, "b")
	if len(got) != 2 {
		t.Fatalf("expected append, got %v", got)
	}
}

// findRepoRoot walks up from cwd until it finds go.mod; the leaf-audit
// tests need the real repo to verify the production filter package.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LeafCategory classifies a single TryCompact* function by what kind of
// compaction it actually performs. The audit tool reads each function's
// AST and produces one record per function so a maintainer can see the
// distribution at a glance and CI can guard the empty-only-stub ratio.
type LeafCategory string

const (
	// LeafEmptyOnly: the function only fires on empty stdout. Examples
	// today: most `aws`, `terraform`, `dotnet`, `mvn`, `gradle` wrappers.
	// These produce zero savings on the typical successful case.
	LeafEmptyOnly LeafCategory = "empty_only_stub"
	// LeafRealParser: the function does semantic parsing of non-empty
	// output (table compaction, list dedup, structured failure
	// extraction, JSON metadata strip, etc.).
	LeafRealParser LeafCategory = "real_parser"
	// LeafMixed: handles both empty and non-empty output cases.
	LeafMixed LeafCategory = "mixed"
	// LeafFallback: only used as the last-resort dispatch fallback for a
	// family (passthrough, simple truncate, etc.).
	LeafFallback LeafCategory = "fallback"
)

// LeafAuditEntry is one row of the audit table.
type LeafAuditEntry struct {
	File         string       `json:"file"`
	FuncName     string       `json:"func_name"`
	Category     LeafCategory `json:"category"`
	Lines        int          `json:"lines"`
	Notes        string       `json:"notes"`
	CallsHelpers []string     `json:"calls_helpers,omitempty"`
}

// LeafAuditReport is the aggregate view across all builtin_*.go files.
type LeafAuditReport struct {
	Root          string           `json:"root"`
	Total         int              `json:"total"`
	EmptyOnly     int              `json:"empty_only"`
	RealParser    int              `json:"real_parser"`
	Mixed         int              `json:"mixed"`
	Fallback      int              `json:"fallback"`
	EmptyOnlyPct  float64          `json:"empty_only_pct"`
	Entries       []LeafAuditEntry `json:"entries"`
	PerFileCounts map[string]int   `json:"per_file_counts"`
}

// classifyTryCompactFunc inspects a function declaration and returns its
// category plus a short note explaining the verdict. The classification
// is heuristic but tuned to the patterns actually used in builtin_*.go:
//
//   - tryCompactEmptyStdoutSingleBinary call -> empty-only-stub.
//   - extractBuildErrors / extractTestFailures / detectBuildSuccess /
//     bytes.* on stdout -> real-parser (does semantic work on non-empty
//     output).
//   - both signals present -> mixed.
//   - func body is essentially `return stdout, false` -> fallback.
func classifyTryCompactFunc(decl *ast.FuncDecl, fset *token.FileSet) (LeafCategory, string, []string) {
	body := decl.Body
	if body == nil {
		return LeafFallback, "no body", nil
	}
	calls := collectCallNames(body)
	hasEmptyOnly := false
	hasParserHelper := false
	parserHelpers := []string{}
	for _, c := range calls {
		switch {
		case c == "tryCompactEmptyStdoutSingleBinary":
			hasEmptyOnly = true
		case c == "tryCompactEmptyStdoutSubcmd":
			hasEmptyOnly = true
		case c == "extractBuildErrors":
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case c == "extractTestFailures":
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case c == "detectBuildSuccess":
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case strings.HasPrefix(c, "extract"):
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case strings.HasPrefix(c, "compact"):
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case strings.HasPrefix(c, "compress"):
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case strings.HasPrefix(c, "collapse"):
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case strings.HasPrefix(c, "summarize") || strings.HasPrefix(c, "Summarize"):
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case strings.HasPrefix(c, "ParseFailures"):
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case strings.HasPrefix(c, "compactTable") || strings.HasPrefix(c, "compactList") || strings.HasPrefix(c, "compactRows"):
			hasParserHelper = true
			parserHelpers = appendUnique(parserHelpers, c)
		case c == "Match" || c == "MatchString" || c == "FindStringIndex":
			// regex-based parsing on the function's input
			hasParserHelper = true
		}
	}
	startLine := fset.Position(body.Lbrace).Line
	endLine := fset.Position(body.Rbrace).Line
	bodyLen := endLine - startLine
	switch {
	case hasEmptyOnly && hasParserHelper:
		return LeafMixed, "uses both empty-only fallback and a parser helper", parserHelpers
	case hasEmptyOnly:
		return LeafEmptyOnly, "calls tryCompactEmptyStdoutSingleBinary as the sole compaction path", parserHelpers
	case hasParserHelper:
		return LeafRealParser, "delegates to a parser helper for semantic compaction", parserHelpers
	case bodyLen <= 4:
		return LeafFallback, "tiny body; treated as fallback", parserHelpers
	default:
		// Functions that do their own parsing (e.g. inline regex / JSON
		// parsing) without delegating to a shared helper still count as
		// real parsers. Heuristic: body has any of the markers below.
		if hasInlineParserSignal(body) {
			return LeafRealParser, "inline parsing of stdout (no shared helper)", parserHelpers
		}
		return LeafFallback, "no recognised parser or empty-only marker", parserHelpers
	}
}

func collectCallNames(body *ast.BlockStmt) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			out = append(out, fn.Name)
		case *ast.SelectorExpr:
			out = append(out, fn.Sel.Name)
		}
		return true
	})
	return out
}

// hasInlineParserSignal looks for direct evidence that the function does
// semantic work on non-empty stdout (json.Unmarshal, bytes.Split, regex
// matching against stdout, line-by-line scanning, etc.).
func hasInlineParserSignal(body *ast.BlockStmt) bool {
	signals := map[string]struct{}{
		"Unmarshal": {}, "Split": {}, "Replace": {}, "FindAll": {},
		"FindAllString": {}, "FindAllSubmatch": {}, "Scanner": {},
		"NewScanner": {}, "TrimSpace": {}, "Fields": {},
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if _, in := signals[sel.Sel.Name]; in {
			found = true
			return false
		}
		return true
	})
	return found
}

func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// AuditFilterPackage walks <root>/internal/filter/builtin_*.go,
// classifies every exported `TryCompact*` function and produces an
// aggregate report. Files ending in `_test.go` are skipped.
func AuditFilterPackage(root string) (LeafAuditReport, error) {
	dir := filepath.Join(root, "internal", "filter")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return LeafAuditReport{}, fmt.Errorf("read filter dir: %w", err)
	}
	report := LeafAuditReport{Root: dir, PerFileCounts: map[string]int{}}
	fset := token.NewFileSet()
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), "builtin_") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return report, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name == nil || !strings.HasPrefix(fn.Name.Name, "TryCompact") {
				continue
			}
			cat, note, helpers := classifyTryCompactFunc(fn, fset)
			startLine := fset.Position(fn.Pos()).Line
			endLine := fset.Position(fn.End()).Line
			entry := LeafAuditEntry{
				File:         name,
				FuncName:     fn.Name.Name,
				Category:     cat,
				Lines:        endLine - startLine + 1,
				Notes:        note,
				CallsHelpers: helpers,
			}
			report.Entries = append(report.Entries, entry)
			report.Total++
			report.PerFileCounts[name]++
			switch cat {
			case LeafEmptyOnly:
				report.EmptyOnly++
			case LeafRealParser:
				report.RealParser++
			case LeafMixed:
				report.Mixed++
			case LeafFallback:
				report.Fallback++
			}
		}
	}
	if report.Total > 0 {
		report.EmptyOnlyPct = float64(report.EmptyOnly) / float64(report.Total) * 100
	}
	return report, nil
}

// FormatLeafAuditMarkdown renders the report as a Markdown document
// suitable for committing under docs/.
func FormatLeafAuditMarkdown(report LeafAuditReport) string {
	var sb strings.Builder
	sb.WriteString("# Layer 0 leaf audit\n\n")
	sb.WriteString("Generated by `go run ./scripts/utils leaf-audit`. ")
	sb.WriteString("This file documents the current distribution of `TryCompact*` ")
	sb.WriteString("functions in `internal/filter/`. The single most useful number ")
	sb.WriteString("is the empty-only-stub ratio: every entry in that bucket fires ")
	sb.WriteString("only on empty stdout and produces zero savings on the typical ")
	sb.WriteString("successful case. T119 tracks lifting this ratio from the ")
	sb.WriteString("starting state down to <=30%.\n\n")
	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("- Total `TryCompact*` functions: **%d**\n", report.Total))
	sb.WriteString(fmt.Sprintf("- Empty-only stubs: **%d** (%.1f%%)\n", report.EmptyOnly, report.EmptyOnlyPct))
	sb.WriteString(fmt.Sprintf("- Real parsers: **%d**\n", report.RealParser))
	sb.WriteString(fmt.Sprintf("- Mixed: **%d**\n", report.Mixed))
	sb.WriteString(fmt.Sprintf("- Fallback: **%d**\n\n", report.Fallback))
	sb.WriteString("## Per-file counts\n\n")
	sb.WriteString("| File | Functions |\n| --- | ---: |\n")
	files := make([]string, 0, len(report.PerFileCounts))
	for f := range report.PerFileCounts {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("| %s | %d |\n", f, report.PerFileCounts[f]))
	}
	sb.WriteString("\n## Per-function classification\n\n")
	sb.WriteString("| File | Function | Category | Lines | Notes |\n| --- | --- | --- | ---: | --- |\n")
	rows := make([]LeafAuditEntry, len(report.Entries))
	copy(rows, report.Entries)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].File != rows[j].File {
			return rows[i].File < rows[j].File
		}
		return rows[i].FuncName < rows[j].FuncName
	})
	for _, e := range rows {
		notes := strings.ReplaceAll(e.Notes, "|", "\\|")
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %d | %s |\n",
			e.File, e.FuncName, e.Category, e.Lines, notes))
	}
	return sb.String()
}

// LeafAuditGate compares the empty-only ratio against a threshold and
// returns a non-zero exit code on regression. Used as the CI step.
func LeafAuditGate(root string, maxEmptyOnlyPct float64, stdout, stderr io.Writer) int {
	report, err := AuditFilterPackage(root)
	if err != nil {
		fmt.Fprintf(stderr, "leaf-audit: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "leaf-audit: total=%d empty_only=%d (%.1f%%) real=%d mixed=%d fallback=%d\n",
		report.Total, report.EmptyOnly, report.EmptyOnlyPct,
		report.RealParser, report.Mixed, report.Fallback)
	if report.EmptyOnlyPct > maxEmptyOnlyPct+1e-9 {
		fmt.Fprintf(stdout, "leaf-audit: FAIL empty_only_pct=%.1f > max=%.1f\n",
			report.EmptyOnlyPct, maxEmptyOnlyPct)
		return 1
	}
	fmt.Fprintf(stdout, "leaf-audit: PASS\n")
	return 0
}

// runLeafAudit is the CLI dispatcher hooked from main.go.
func runLeafAudit(args []string, stdout, stderr io.Writer) int {
	root := "."
	maxPct := 100.0
	check := false
	jsonOut := false
	mdOut := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--check":
			check = true
		case a == "--json":
			jsonOut = true
		case strings.HasPrefix(a, "--max-empty-only-pct="):
			v := strings.TrimPrefix(a, "--max-empty-only-pct=")
			f, err := parseFloat(v)
			if err != nil {
				fmt.Fprintf(stderr, "leaf-audit: invalid --max-empty-only-pct=%s\n", v)
				return 2
			}
			maxPct = f
		case strings.HasPrefix(a, "--write-markdown="):
			mdOut = strings.TrimPrefix(a, "--write-markdown=")
		case strings.HasPrefix(a, "--root="):
			root = strings.TrimPrefix(a, "--root=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "leaf-audit: unknown flag %q\n", a)
			return 2
		default:
			fmt.Fprintf(stderr, "leaf-audit: unexpected argument %q\n", a)
			return 2
		}
	}
	if check {
		return LeafAuditGate(root, maxPct, stdout, stderr)
	}
	report, err := AuditFilterPackage(root)
	if err != nil {
		fmt.Fprintf(stderr, "leaf-audit: %v\n", err)
		return 1
	}
	if mdOut != "" {
		if err := os.WriteFile(mdOut, []byte(FormatLeafAuditMarkdown(report)), 0o644); err != nil {
			fmt.Fprintf(stderr, "leaf-audit: write %s: %v\n", mdOut, err)
			return 1
		}
		fmt.Fprintf(stdout, "leaf-audit: wrote %s\n", mdOut)
	}
	if jsonOut {
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "leaf-audit: json: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(out))
		return 0
	}
	if mdOut == "" {
		fmt.Fprint(stdout, FormatLeafAuditMarkdown(report))
	}
	return 0
}

func parseFloat(s string) (float64, error) {
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0, err
	}
	return f, nil
}

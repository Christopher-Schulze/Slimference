package codecompact

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"strings"
)

const defaultMinBytes = 3000

var ErrUnsupported = errors.New("unsupported language")

type Options struct {
	MinBytes             int
	MaxIncludedBodyLines int
	Mode                 string
	RecentlyEdited       bool
	ForceFull            bool
	RelevantSymbols      []string
}

type Stats struct {
	Language       string
	OriginalBytes  int
	CompactedBytes int
	Functions      int
	OmittedBodies  int
	IncludedBodies int
	Mode           string
}

func Compact(path string, content []byte, opts Options) ([]byte, Stats, bool, error) {
	stats := Stats{
		Language:      languageFromPath(path),
		OriginalBytes: len(content),
		Mode:          opts.Mode,
	}
	if !allowCompact(content, opts) {
		return content, stats, false, nil
	}
	if stats.Language != "go" {
		return content, stats, false, ErrUnsupported
	}
	out, goStats, err := compactGo(path, content, opts)
	stats.Functions = goStats.Functions
	stats.OmittedBodies = goStats.OmittedBodies
	stats.IncludedBodies = goStats.IncludedBodies
	if err != nil {
		return content, stats, false, err
	}
	stats.CompactedBytes = len(out)
	if len(out) >= len(content) {
		return content, stats, false, nil
	}
	return out, stats, true, nil
}

func ExtractGoSymbolBody(path string, content []byte, symbol string) ([]byte, bool, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil, false, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return nil, false, err
	}
	var matched *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if !goSymbolMatches(fn, symbol) {
			continue
		}
		if matched != nil {
			return nil, false, nil
		}
		matched = fn
	}
	if matched == nil {
		return nil, false, nil
	}
	var out strings.Builder
	renderDecl(&out, fset, matched)
	out.WriteString("\n")
	return []byte(out.String()), true, nil
}

func allowCompact(content []byte, opts Options) bool {
	if opts.ForceFull || opts.RecentlyEdited {
		return false
	}
	switch opts.Mode {
	case "", "scan", "orientation":
	default:
		return false
	}
	minBytes := opts.MinBytes
	if minBytes <= 0 {
		minBytes = defaultMinBytes
	}
	return len(content) >= minBytes
}

func languageFromPath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".go") {
		return "go"
	}
	return ""
}

type goCompactStats struct {
	Functions      int
	OmittedBodies  int
	IncludedBodies int
}

func compactGo(path string, content []byte, opts Options) ([]byte, goCompactStats, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return nil, goCompactStats{}, err
	}

	maxBodyLines := opts.MaxIncludedBodyLines
	if maxBodyLines <= 0 {
		maxBodyLines = 8
	}
	relevant := relevantSymbolSet(opts.RelevantSymbols)

	var out strings.Builder
	out.WriteString("package ")
	out.WriteString(file.Name.Name)
	out.WriteString("\n\n")

	stats := goCompactStats{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			renderDecl(&out, fset, d)
			out.WriteString("\n\n")
		case *ast.FuncDecl:
			stats.Functions++
			bodyLines := goBodyLines(fset, d)
			includeBody := shouldIncludeGoBody(d, bodyLines, maxBodyLines, relevant)
			if includeBody {
				renderDecl(&out, fset, d)
				stats.IncludedBodies++
			} else {
				renderGoSignature(&out, fset, d)
				out.WriteString(" { /* body omitted: ")
				out.WriteString(intString(bodyLines))
				out.WriteString(" lines */ }")
				stats.OmittedBodies++
			}
			out.WriteString("\n\n")
		}
	}
	out.WriteString("/* AST-compacted by Slimference. Re-read the file for full bodies when editing/debugging. */\n")
	return []byte(out.String()), stats, nil
}

func renderDecl(out *strings.Builder, fset *token.FileSet, decl ast.Decl) {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, fset, decl)
	out.WriteString(strings.TrimSpace(buf.String()))
}

func renderGoSignature(out *strings.Builder, fset *token.FileSet, fn *ast.FuncDecl) {
	copyFn := *fn
	copyFn.Body = nil
	renderDecl(out, fset, &copyFn)
}

func goBodyLines(fset *token.FileSet, fn *ast.FuncDecl) int {
	if fset == nil || fn == nil || fn.Body == nil {
		return 0
	}
	start := fset.Position(fn.Body.Pos()).Line
	end := fset.Position(fn.Body.End()).Line
	return end - start + 1
}

func shouldIncludeGoBody(fn *ast.FuncDecl, bodyLines, maxBodyLines int, relevant map[string]struct{}) bool {
	name := fn.Name.Name
	if bodyLines <= maxBodyLines || name == "main" || name == "init" {
		return true
	}
	_, ok := relevant[name]
	return ok
}

func goSymbolMatches(fn *ast.FuncDecl, symbol string) bool {
	if fn == nil || fn.Name == nil {
		return false
	}
	if symbol == fn.Name.Name {
		return true
	}
	recv := goReceiverName(fn)
	if recv == "" {
		return false
	}
	return symbol == recv+"."+fn.Name.Name || symbol == "(*"+recv+")."+fn.Name.Name
}

func goReceiverName(fn *ast.FuncDecl) string {
	if fn == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch t := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func relevantSymbolSet(symbols []string) map[string]struct{} {
	out := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol != "" {
			out[symbol] = struct{}{}
		}
	}
	return out
}

func intString(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	n := v
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

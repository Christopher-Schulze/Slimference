package codecompact

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// ExtractGoSymbolBody extracts the full body of a single Go function matching
// symbol. It is used by the checkpoint command to show one function's body
// without re-reading the whole file.
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

func renderDecl(out *strings.Builder, fset *token.FileSet, decl ast.Decl) {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, fset, decl)
	out.WriteString(strings.TrimSpace(buf.String()))
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

package codecompact

import (
	"go/ast"
	"strings"
	"testing"
)

func TestExtractGoSymbolBody(t *testing.T) {
	t.Parallel()
	src := []byte(`package demo

type Service struct{}

func Plain() int {
	return 1
}

func (s *Service) Run() int {
	return Plain()
}
`)
	for _, symbol := range []string{"Plain", "Service.Run", "(*Service).Run", "Run"} {
		body, ok, err := ExtractGoSymbolBody("service.go", src, symbol)
		if err != nil {
			t.Fatalf("symbol %q: %v", symbol, err)
		}
		if !ok || !strings.Contains(string(body), "return") {
			t.Fatalf("symbol %q body ok=%v body=%q", symbol, ok, body)
		}
	}
}

func TestExtractGoSymbolBodyMissesAndAmbiguous(t *testing.T) {
	t.Parallel()
	src := []byte(`package demo

type A struct{}
type B struct{}

func (a A) Run() {}
func (b B) Run() {}
`)
	for _, symbol := range []string{"", "Missing", "Run"} {
		body, ok, err := ExtractGoSymbolBody("service.go", src, symbol)
		if err != nil {
			t.Fatalf("symbol %q: %v", symbol, err)
		}
		if ok || len(body) != 0 {
			t.Fatalf("symbol %q should miss, ok=%v body=%q", symbol, ok, body)
		}
	}
	if _, ok, err := ExtractGoSymbolBody("broken.go", []byte("package demo\nfunc {"), "Run"); err == nil || ok {
		t.Fatalf("broken input should error, ok=%v err=%v", ok, err)
	}
}

func TestGoSymbolHelperBranches(t *testing.T) {
	t.Parallel()
	if goSymbolMatches(nil, "Run") {
		t.Fatal("nil func must not match")
	}
	if goSymbolMatches(&ast.FuncDecl{}, "Run") {
		t.Fatal("nameless func must not match")
	}
	if got := goReceiverName(&ast.FuncDecl{Recv: &ast.FieldList{List: []*ast.Field{{Type: &ast.SelectorExpr{}}}}}); got != "" {
		t.Fatalf("unsupported receiver=%q", got)
	}
	if got := goReceiverName(&ast.FuncDecl{Recv: &ast.FieldList{List: []*ast.Field{{Type: &ast.StarExpr{X: &ast.SelectorExpr{}}}}}}); got != "" {
		t.Fatalf("unsupported star receiver=%q", got)
	}
}

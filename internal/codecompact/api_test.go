package codecompact

import (
	"strings"
	"testing"
)

func TestCompactGo_OmitsLargeBodiesAndKeepsShortBodies(t *testing.T) {
	t.Parallel()
	src := []byte(goFixture(20))
	out, stats, ok, err := Compact("service.go", src, Options{Mode: "scan", MinBytes: 100})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !ok {
		t.Fatal("expected compaction")
	}
	got := string(out)
	for _, want := range []string{"package demo", "import (", "type Service struct", "func Tiny() int", "func Huge() int { /* body omitted:", "AST-compacted by Slimference"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if stats.Functions != 2 || stats.OmittedBodies != 1 || stats.IncludedBodies != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestCompactGo_RelevantSymbolIncludesBody(t *testing.T) {
	t.Parallel()
	src := []byte(goFixture(20))
	out, stats, err := compactGo("service.go", src, Options{RelevantSymbols: []string{"Huge"}})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "func Huge() int { /* body omitted:") {
		t.Fatalf("relevant body should be included:\n%s", got)
	}
	if stats.OmittedBodies != 0 || stats.IncludedBodies != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestCompact_ModeGate(t *testing.T) {
	t.Parallel()
	src := []byte(goFixture(20))
	for _, opts := range []Options{
		{Mode: "edit", MinBytes: 100},
		{Mode: "debug", MinBytes: 100},
		{Mode: "scan", MinBytes: 100, ForceFull: true},
		{Mode: "scan", MinBytes: 100, RecentlyEdited: true},
		{Mode: "scan", MinBytes: len(src) + 1},
	} {
		out, _, ok, err := Compact("service.go", src, opts)
		if err != nil {
			t.Fatalf("compact: %v", err)
		}
		if ok {
			t.Fatalf("expected gate to deny compaction for %+v", opts)
		}
		if string(out) != string(src) {
			t.Fatal("denied compaction must return original content")
		}
	}
}

func TestCompact_UnsupportedAndInvalid(t *testing.T) {
	t.Parallel()
	if _, _, ok, err := Compact("service.ts", []byte(strings.Repeat("x", 200)), Options{Mode: "scan", MinBytes: 10}); err != ErrUnsupported || ok {
		t.Fatalf("unsupported: ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := Compact("broken.go", []byte("package main\nfunc {"), Options{Mode: "scan", MinBytes: 10}); err == nil || ok {
		t.Fatalf("invalid Go should error without ok: ok=%v err=%v", ok, err)
	}
}

func TestCompact_DefaultMinAndUnsupportedSmall(t *testing.T) {
	t.Parallel()
	src := []byte(strings.Repeat("x", 200))
	out, stats, ok, err := Compact("service.ts", src, Options{Mode: "scan"})
	if err != nil || ok || string(out) != string(src) {
		t.Fatalf("small unsupported file should be gated before language error: stats=%+v ok=%v err=%v", stats, ok, err)
	}
}

func TestCompact_NotShorterReturnsOriginal(t *testing.T) {
	t.Parallel()
	src := []byte(`package demo

func Tiny() int {
	return 1
}
`)
	out, _, ok, err := Compact("tiny.go", src, Options{Mode: "scan", MinBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("compaction that is not shorter must be rejected")
	}
	if string(out) != string(src) {
		t.Fatal("not-shorter compaction must return original content")
	}
}

func TestCompactGo_MainAndInitBodiesIncluded(t *testing.T) {
	t.Parallel()
	src := []byte(`package main

func init() {
	println("one")
	println("two")
	println("three")
	println("four")
	println("five")
	println("six")
	println("seven")
	println("eight")
	println("nine")
}

func main() {
	println("hello")
}
` + strings.Repeat("// pad\n", 80))
	out, stats, ok, err := Compact("main.go", src, Options{Mode: "scan", MinBytes: 100, MaxIncludedBodyLines: 1})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !ok {
		t.Fatal("expected compaction")
	}
	got := string(out)
	if strings.Contains(got, "func init() { /* body omitted:") || strings.Contains(got, "func main() { /* body omitted:") {
		t.Fatalf("main/init should be included:\n%s", got)
	}
	if stats.IncludedBodies != 2 {
		t.Fatalf("included bodies=%d want 2", stats.IncludedBodies)
	}
}

func TestIntString(t *testing.T) {
	t.Parallel()
	if intString(0) != "0" || intString(12345) != "12345" {
		t.Fatal("bad intString")
	}
}

func TestGoBodyLines_NilBody(t *testing.T) {
	t.Parallel()
	if got := goBodyLines(nil, nil); got != 0 {
		t.Fatalf("nil body lines=%d want 0", got)
	}
}

func goFixture(hugeLines int) string {
	var sb strings.Builder
	sb.WriteString(`package demo

import (
	"context"
	"fmt"
)

type Service struct {
	ctx context.Context
}

func Tiny() int {
	return 1
}

func Huge() int {
`)
	for i := 0; i < hugeLines; i++ {
		sb.WriteString(`	fmt.Println("line")
`)
	}
	sb.WriteString(`	return 2
}
`)
	return sb.String()
}

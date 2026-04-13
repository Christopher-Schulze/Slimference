package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTotalPercent(t *testing.T) {
	t.Parallel()
	in := `
github.com/foo/bar/a.go:10:	Foo		50.0%
github.com/foo/bar/b.go:20:	Bar		100.0%
total:						(statements)	87.3%
`
	v, ok := parseTotalPercent(in)
	if !ok || v < 87.2 || v > 87.4 {
		t.Fatalf("got %v ok=%v", v, ok)
	}
}

func TestParseTotalPercent_LastLine(t *testing.T) {
	t.Parallel()
	in := "total:\t\t\t(statements)\t100.0%\n"
	v, ok := parseTotalPercent(in)
	if !ok || v != 100 {
		t.Fatalf("got %v ok=%v", v, ok)
	}
}

func TestParseTotalPercent_noMatch(t *testing.T) {
	t.Parallel()
	if _, ok := parseTotalPercent("github.com/x/a.go:1:\tFoo\t50.0%\n"); ok {
		t.Fatal("expected no total line")
	}
}

func TestParseTotalPercent_totalNotLastLine(t *testing.T) {
	t.Parallel()
	in := `total:						(statements)	10.0%
github.com/foo/bar/a.go:10:	Foo		50.0%
`
	v, ok := parseTotalPercent(in)
	if !ok || v < 9.9 || v > 10.1 {
		t.Fatalf("got %v ok=%v", v, ok)
	}
}

func TestFindModuleRoot(t *testing.T) {
	t.Parallel()
	root, err := findModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go.mod in %s: %v", root, err)
	}
}

func TestFindModuleRoot_noGoMod(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := findModuleRoot(); err == nil {
		t.Fatal("expected error when no go.mod in ancestors")
	}
}

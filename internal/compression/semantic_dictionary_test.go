package compression

import (
	"fmt"
	"strings"
	"testing"
)

func TestApplySemanticDictionary_RepeatedPaths(t *testing.T) {
	t.Parallel()
	path := "/Users/example/workspace/slimference/internal/proxy/handler.go"
	input := strings.Join([]string{
		"panic: failed",
		path + ":10",
		path + ":20",
		path + ":30",
		path + ":40",
		path + ":50",
		path + ":60",
	}, "\n")
	out, saved, ok := applySemanticDictionary(input)
	if !ok || saved <= 0 {
		t.Fatalf("dictionary not applied: saved=%d ok=%v", saved, ok)
	}
	if !strings.Contains(out, "[path dictionary]") || strings.Contains(out, "Slimference path dictionary") || !strings.Contains(out, "[P1]="+path) {
		t.Fatalf("legend missing: %s", out)
	}
	if strings.Count(out, path) != 1 {
		t.Fatalf("path should only remain in legend, got %d occurrences in %s", strings.Count(out, path), out)
	}
	if strings.Count(out, "[P1]") <= 3 {
		t.Fatalf("aliases not applied enough: %s", out)
	}
}

func TestApplySemanticDictionary_SkipsUnsafeOrUnprofitableInputs(t *testing.T) {
	t.Parallel()
	unprofitable := "/Users/example/a/b/c/d.go"
	samples := []string{
		"https://api.example.com/v1/really/long/path/repeated https://api.example.com/v1/really/long/path/repeated https://api.example.com/v1/really/long/path/repeated",
		"file:" + unprofitable + "\nfile:" + unprofitable + "\nfile:" + unprofitable + "\n",
		unprofitable + "\n" + unprofitable + "\n" + unprofitable + "\n",
		"/Users/example/a.go\n/Users/example/a.go\n/Users/example/a.go\n",
		"/not/a/known/root/because/prefix/is/custom\n/not/a/known/root/because/prefix/is/custom\n/not/a/known/root/because/prefix/is/custom\n",
	}
	for _, sample := range samples {
		out, saved, ok := applySemanticDictionary(sample)
		if ok || saved != 0 || out != sample {
			t.Fatalf("unexpected dictionary application for %q: out=%q saved=%d ok=%v", sample, out, saved, ok)
		}
	}
}

func TestSemanticDictionaryCandidates_CapsEntries(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < pathDictionaryMaxEntries+2; i++ {
		path := fmt.Sprintf("/Users/example/project/pkg/%02d/deep/file.go", i)
		b.WriteString(path + "\n" + path + "\n" + path + "\n")
	}
	candidates := semanticDictionaryCandidates(b.String())
	if len(candidates) != pathDictionaryMaxEntries+2 {
		t.Fatalf("candidate collection should not cap before apply, got %d", len(candidates))
	}
	out, _, ok := applySemanticDictionary(b.String())
	if !ok {
		t.Fatal("dictionary should apply")
	}
	if strings.Contains(out, "[P9]=") {
		t.Fatalf("dictionary should cap aliases to P1-P8: %s", out)
	}
	if !isDictionaryPath("/Applications/Foo.app/Contents/MacOS/Foo") || isDictionaryPath("/srv/custom/path") {
		t.Fatal("dictionary path root filter mismatch")
	}
}

func TestPathDictionaryLegend_Empty(t *testing.T) {
	t.Parallel()
	if got := pathDictionaryLegend(nil); got != "[path dictionary]\n[/path dictionary]\n" {
		t.Fatalf("empty legend=%q", got)
	}
}

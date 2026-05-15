package compression

import (
	"fmt"
	"strings"
	"testing"
)

func TestCompactSemanticTestFailureGoPanic(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("=== RUN   TestExplodes\n")
	sb.WriteString("--- FAIL: TestExplodes (0.01s)\n")
	sb.WriteString("panic: expected cached value, got stale value\n")
	sb.WriteString("goroutine 19 [running]:\n")
	sb.WriteString("github.com/acme/project/internal/cache.TestExplodes(0x140001)\n")
	sb.WriteString("\t/Users/christopher/CODE/project/internal/cache/cache_test.go:42 +0x20\n")
	sb.WriteString("testing.tRunner(0x140001)\n")
	sb.WriteString("\t/usr/local/go/src/testing/testing.go:1689 +0x120\n")
	sb.WriteString("runtime.gopanic()\n")
	sb.WriteString("\t/usr/local/go/src/runtime/panic.go:770 +0x124\n")
	for i := 0; i < 40; i++ {
		sb.WriteString("\t/usr/local/go/src/testing/testing.go:1689 +0x120\n")
	}
	sb.WriteString("ok  \tgithub.com/acme/project/internal/other\t0.01s\n")
	sb.WriteString("FAIL\tgithub.com/acme/project/internal/cache\t0.01s\n")
	content := sb.String()

	got := compactSemanticTestFailure(content, true)
	if got == content {
		t.Fatal("expected semantic test compaction")
	}
	for _, want := range []string{
		"TestExplodes",
		"panic: expected cached value",
		"/Users/christopher/CODE/project/internal/cache/cache_test.go:42",
		"FAIL\tgithub.com/acme/project/internal/cache",
		"framework/vendor stack frame",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compacted output missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "testing.go:1689") > 1 {
		t.Fatalf("framework frames should be collapsed:\n%s", got)
	}
}

func TestCompactSemanticTestFailureJavaScriptStack(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("FAIL src/button.test.tsx > Button > renders disabled state\n")
	sb.WriteString("AssertionError: expected false to be true\n")
	sb.WriteString("- Expected\n")
	sb.WriteString("+ Received\n")
	sb.WriteString("    at Button.test (src/button.test.tsx:17:12)\n")
	sb.WriteString("    at renderWithProviders (src/test/render.tsx:9:3)\n")
	for i := 0; i < 30; i++ {
		sb.WriteString("    at runTest (node_modules/vitest/dist/chunk-runtime.js:123:45)\n")
	}
	sb.WriteString("1 failed, 22 passed\n")
	content := sb.String()

	got := compactSemanticTestFailure(content, false)
	if got == content {
		t.Fatal("expected JavaScript stack compaction")
	}
	for _, want := range []string{
		"Button > renders disabled state",
		"AssertionError",
		"src/button.test.tsx:17:12",
		"src/test/render.tsx:9:3",
		"1 failed, 22 passed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compacted output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "node_modules/vitest/dist/chunk-runtime.js") {
		t.Fatalf("vendor frames should be omitted:\n%s", got)
	}
}

func TestCompactSemanticTestFailureShortOutputPassthrough(t *testing.T) {
	t.Parallel()
	content := "--- FAIL: TestShort (0.01s)\n    x_test.go:7: got 1 want 2\nFAIL\n"
	if got := compactSemanticTestFailure(content, false); got != content {
		t.Fatalf("short output should pass through, got %q", got)
	}
}

func TestCompactSemanticTestFailureOverflowMarkersAndBranches(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("thread 'worker' panicked at src/lib.rs:7:3\n")
	for i := 0; i < 20; i++ {
		sb.WriteString("- expected line\n")
		sb.WriteString("+ actual line\n")
	}
	for i := 0; i < 12; i++ {
		sb.WriteString("at crate::module::case(src/lib.rs:12:3)\n")
	}
	sb.WriteString("at helper (/Users/christopher/project/node_modules/pkg/index.js:1:1)\n")
	for i := 0; i < 25; i++ {
		sb.WriteString("    repeated diagnostic context\n")
	}
	content := sb.String()

	got := compactSemanticTestFailure(content, true)
	for _, want := range []string{
		"additional application frame",
		"additional assertion/diff",
		"framework/vendor stack frame",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compacted output missing %q:\n%s", want, got)
		}
	}
	if isApplicationFrame("at helper (/Users/christopher/project/node_modules/pkg/index.js:1:1)") {
		t.Fatal("framework frame must not be treated as application frame")
	}
}

func TestFilterTestCompactUsesSemanticStacktraceCompaction(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("--- FAIL: TestProxy (0.01s)\n")
	sb.WriteString("AssertionError: expected route to be cached\n")
	sb.WriteString("    at TestProxy (src/proxy.test.ts:10:2)\n")
	for i := 0; i < 30; i++ {
		sb.WriteString("    at runTest (node_modules/vitest/dist/runtime.js:100:1)\n")
	}
	content := sb.String()

	got := filterTestCompact(content, true)
	if !strings.Contains(got, "[semantic-test-compact]") {
		t.Fatalf("filterTestCompact should use semantic stacktrace compaction, got:\n%s", got)
	}
}

func TestCompactSemanticTestFailureNoKeptAndNoSavings(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("    at anonymous callback\n", 30)
	if got := compactSemanticTestFailure(content, false); got != content {
		t.Fatalf("stack-shaped but unactionable output should pass through, got %q", got)
	}

	var shortButStack strings.Builder
	shortButStack.WriteString("    at x\n")
	for i := 0; i < 30; i++ {
		shortButStack.WriteString(fmt.Sprintf("ERROR case %d\n", i))
	}
	if got := compactSemanticTestFailure(shortButStack.String(), false); got != shortButStack.String() {
		t.Fatalf("non-saving compaction should pass through, got %q", got)
	}
}

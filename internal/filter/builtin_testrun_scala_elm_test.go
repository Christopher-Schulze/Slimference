package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactScalaAndElmTestAllPass(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		argv   []string
		stdout string
		parse  func([]string, []byte) ([]byte, bool)
		want   string
	}{
		{
			name:   "sbt",
			argv:   []string{"sbt", "-batch", "test"},
			stdout: filterScalaStyleAllPassFixture(64),
			parse:  TryCompactSbtTest,
			want:   "[sbt test] ok (64 succeeded)\n",
		},
		{
			name:   "mill dotted task",
			argv:   []string{"./mill", "foo.test"},
			stdout: filterMillScalaStyleAllPassFixture(64),
			parse:  TryCompactMillTest,
			want:   "[mill test] ok (64 succeeded)\n",
		},
		{
			name:   "elm-test",
			argv:   []string{"elm-test"},
			stdout: filterElmTestAllPassFixture(64),
			parse:  TryCompactElmTest,
			want:   "[elm-test] ok (Passed: 64; Failed: 0)\n",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := tt.parse(tt.argv, []byte(tt.stdout))
			if !ok || string(out) != tt.want {
				t.Fatalf("%s parser = ok %v out %q, want %q", tt.name, ok, out, tt.want)
			}
			if len(out) >= len(tt.stdout) {
				t.Fatalf("%s parser did not save bytes: original=%d compacted=%d", tt.name, len(tt.stdout), len(out))
			}
		})
	}
}

func TestTryCompactScalaAndElmTestUnsafeSignalsFailOpen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		argv   []string
		stdout string
		parse  func([]string, []byte) ([]byte, bool)
	}{
		{
			name:   "sbt pending",
			argv:   []string{"sbt", "-batch", "test"},
			stdout: strings.Replace(filterScalaStyleAllPassFixture(8), "pending 0", "pending 1", 1),
			parse:  TryCompactSbtTest,
		},
		{
			name:   "sbt aborted suite",
			argv:   []string{"sbt", "-batch", "test"},
			stdout: strings.Replace(filterScalaStyleAllPassFixture(8), "aborted 0", "aborted 1", 1),
			parse:  TryCompactSbtTest,
		},
		{
			name:   "mill warning",
			argv:   []string{"mill", "test"},
			stdout: filterMillScalaStyleAllPassFixture(8) + "[warn] flaky dependency resolution\n",
			parse:  TryCompactMillTest,
		},
		{
			name:   "elm-test failed",
			argv:   []string{"elm-test"},
			stdout: strings.Replace(filterElmTestAllPassFixture(8), "Failed:   0", "Failed:   1", 1),
			parse:  TryCompactElmTest,
		},
		{
			name:   "elm-test warning",
			argv:   []string{"npx", "-y", "elm-test"},
			stdout: filterElmTestAllPassFixture(8) + "Warning: generated diagnostics\n",
			parse:  TryCompactElmTest,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := tt.parse(tt.argv, []byte(tt.stdout))
			if ok || string(out) != tt.stdout {
				t.Fatalf("%s parser must fail open, got ok %v out %q", tt.name, ok, out)
			}
		})
	}
}

func TestTryCompactTestOutputIncludesScalaAndElmAllPass(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		argv   []string
		stdout string
		want   string
	}{
		{
			name:   "sbt",
			argv:   []string{"pnpm", "exec", "sbt", "test"},
			stdout: filterScalaStyleAllPassFixture(16),
			want:   "[sbt test] ok (16 succeeded)\n",
		},
		{
			name:   "mill dotted task",
			argv:   []string{"npx", "mill", "foo.test"},
			stdout: filterMillScalaStyleAllPassFixture(16),
			want:   "[mill test] ok (16 succeeded)\n",
		},
		{
			name:   "elm-test",
			argv:   []string{"pnpm", "exec", "elm-test"},
			stdout: filterElmTestAllPassFixture(16),
			want:   "[elm-test] ok (Passed: 16; Failed: 0)\n",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactTestOutput(tt.argv, []byte(tt.stdout))
			if !ok || string(out) != tt.want {
				t.Fatalf("TryCompactTestOutput(%s) = ok %v out %q, want %q", tt.name, ok, out, tt.want)
			}
		})
	}
}

func TestScalaElmParserFailOpenEdges(t *testing.T) {
	t.Parallel()
	if out, ok := compactElmTestAllPass([]byte("TEST RUN PASSED\nPassed: 1\nFailed: 0\n")); ok || string(out) != "TEST RUN PASSED\nPassed: 1\nFailed: 0\n" {
		t.Fatalf("short elm-test transcript must fail open, ok=%v out=%q", ok, out)
	}
	if _, ok := parseCountAfterColon("Passed: 7"); !ok {
		t.Fatalf("parseCountAfterColon should accept numeric count")
	}
	for _, line := range []string{"Passed 7", "Passed:"} {
		line := line
		t.Run("elm count "+line, func(t *testing.T) {
			t.Parallel()
			if _, ok := parseCountAfterColon(line); ok {
				t.Fatalf("parseCountAfterColon(%q) should reject", line)
			}
		})
	}

	if !isMillTestArgv([]string{"yarn", "mill", "'foo.test'"}) {
		t.Fatalf("quoted mill dotted test task should be accepted")
	}
	for _, argv := range [][]string{
		{"mill", "--help"},
		{"mill", "compile"},
		{"npx", "mill", "compile"},
	} {
		argv := argv
		t.Run("mill argv reject "+strings.Join(argv, " "), func(t *testing.T) {
			t.Parallel()
			if isMillTestArgv(argv) {
				t.Fatalf("isMillTestArgv(%v) should reject non-test task", argv)
			}
		})
	}
	if millArgsContainTestTask([]string{"--watch", "compile"}) {
		t.Fatalf("millArgsContainTestTask should reject flag-only/non-test args")
	}

	totalTimeOnly := "[info] Tests: succeeded 2, failed 0, canceled 0, ignored 0, pending 0\n[info] All tests passed.\nTotal time: 1 s\n"
	out, ok := compactScalaStyleTestAllPass([]byte(totalTimeOnly), "sbt test")
	if !ok || string(out) != "[sbt test] ok (2 succeeded)\n" {
		t.Fatalf("scala-style total-time summary failed: ok=%v out=%q", ok, out)
	}
	for _, stdout := range []string{
		"[info] All tests passed.\n[success] Total time: 1 s\n",
		"[info] Tests: succeeded nope, failed 0, canceled 0, ignored 0, pending 0\n[info] All tests passed.\n[success] Total time: 1 s\n",
		"[info] Tests: succeeded 1, failed 0\n[info] All tests passed.\n[success] Total time: 1 s\n",
		"[info] Tests: succeeded 1, failed 0, canceled 0, ignored 0, pending 0\n[error] boom\n",
	} {
		stdout := stdout
		t.Run("scala fail-open", func(t *testing.T) {
			t.Parallel()
			out, ok := compactScalaStyleTestAllPass([]byte(stdout), "sbt test")
			if ok || string(out) != stdout {
				t.Fatalf("scala-style parser must fail open, ok=%v out=%q", ok, out)
			}
		})
	}

	if aborted, ok := parseScalaStyleSuitesAborted("Suites: completed 1, aborted 0"); !ok || aborted != 0 {
		t.Fatalf("expected aborted=0, got aborted=%d ok=%v", aborted, ok)
	}
	for _, line := range []string{"no suite summary", "Suites: completed 1, aborted nope"} {
		line := line
		t.Run("suite parse reject", func(t *testing.T) {
			t.Parallel()
			if _, ok := parseScalaStyleSuitesAborted(line); ok {
				t.Fatalf("parseScalaStyleSuitesAborted(%q) should reject", line)
			}
		})
	}
	if counts, ok := parseScalaStyleTestsSummary("Tests: succeeded 3, failed 0, canceled 0, ignored 0, pending 0"); !ok || counts["succeeded"] != 3 {
		t.Fatalf("expected succeeded=3, got counts=%v ok=%v", counts, ok)
	}
	if scalaStyleTestLineHasUnsafeMarker("all green") {
		t.Fatalf("scalaStyleTestLineHasUnsafeMarker should not flag clean text")
	}
}

func filterScalaStyleAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("[info] Compiling 1 Scala source to /repo/target...\n")
	out.WriteString("[info] ExampleSuite:\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "[info] - generated case %03d\n", i)
	}
	fmt.Fprintf(&out, "[info] Total number of tests run: %d\n", count)
	out.WriteString("[info] Suites: completed 1, aborted 0\n")
	fmt.Fprintf(&out, "[info] Tests: succeeded %d, failed 0, canceled 0, ignored 0, pending 0\n", count)
	out.WriteString("[info] All tests passed.\n")
	out.WriteString("[success] Total time: 2 s, completed Jun 18, 2026\n")
	return out.String()
}

func filterMillScalaStyleAllPassFixture(count int) string {
	return "[12/12] foo.test\n" + filterScalaStyleAllPassFixture(count)
}

func filterElmTestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("elm-test 0.19.1-revision6\n")
	out.WriteString("-------------------------\n\n")
	fmt.Fprintf(&out, "Running %d tests. To reproduce these results, run: elm-test --fuzz 100 --seed 148067075282531\n", count)
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "generated case %03d passed\n", i)
	}
	out.WriteString("\nTEST RUN PASSED\n\n")
	out.WriteString("Duration: 121 ms\n")
	fmt.Fprintf(&out, "Passed:   %d\n", count)
	out.WriteString("Failed:   0\n")
	return out.String()
}

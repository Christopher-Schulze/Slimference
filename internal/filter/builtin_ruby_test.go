package filter

import (
	"strings"
	"testing"
)

func TestTryCompactRubyOutput(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactRubyOutput([]string{"rake", "spec"}, []byte("\n"))
	if !ok || string(out) != "[rake] ok\n" {
		t.Fatalf("rake: ok=%v %q", ok, out)
	}
	out2, ok := TryCompactRubyOutput([]string{"rspec"}, []byte(""))
	if !ok || string(out2) != "[rspec] ok\n" {
		t.Fatalf("rspec: %q", out2)
	}
	out3, ok := TryCompactRubyOutput([]string{"bundle", "exec", "rspec"}, []byte(""))
	if !ok || string(out3) != "[rspec] ok\n" {
		t.Fatalf("bundle exec rspec: %q", out3)
	}
	outNpx, ok := TryCompactRubyOutput([]string{"npx", "rake", "db:migrate"}, []byte(""))
	if !ok || string(outNpx) != "[rake] ok\n" {
		t.Fatalf("npx rake: %q", outNpx)
	}
	outRspecYarn, ok := TryCompactRubyOutput([]string{"yarn", "rspec"}, []byte("\n"))
	if !ok || string(outRspecYarn) != "[rspec] ok\n" {
		t.Fatalf("yarn rspec: %q", outRspecYarn)
	}
	if _, ok := TryCompactRubyOutput([]string{"ruby", "-e", "1"}, []byte("")); ok {
		t.Fatal("ruby not handled")
	}
}

func TestTryCompactRubyOutput_missingBranches(t *testing.T) {
	t.Parallel()
	// isRakeArgv len<1
	if isRakeArgv([]string{}) {
		t.Fatal("isRakeArgv: len<1 should return false")
	}
	// pnpm exec rake
	out, ok := TryCompactRubyOutput([]string{"pnpm", "exec", "rake", "db:migrate"}, []byte(""))
	if !ok || string(out) != "[rake] ok\n" {
		t.Fatalf("pnpm exec rake: ok=%v %q", ok, out)
	}
	// yarn rake
	out2, ok := TryCompactRubyOutput([]string{"yarn", "rake"}, []byte("\n"))
	if !ok || string(out2) != "[rake] ok\n" {
		t.Fatalf("yarn rake: ok=%v %q", ok, out2)
	}
	// isRspecDirectOrBundleExec len<1
	if isRspecDirectOrBundleExec([]string{}) {
		t.Fatal("isRspecDirectOrBundleExec: len<1 should return false")
	}
	// npx rspec
	out3, ok := TryCompactRubyOutput([]string{"npx", "rspec"}, []byte(""))
	if !ok || string(out3) != "[rspec] ok\n" {
		t.Fatalf("npx rspec: ok=%v %q", ok, out3)
	}
	// pnpm exec rspec
	out4, ok := TryCompactRubyOutput([]string{"pnpm", "exec", "rspec"}, []byte(""))
	if !ok || string(out4) != "[rspec] ok\n" {
		t.Fatalf("pnpm exec rspec: ok=%v %q", ok, out4)
	}
}

func TestTryCompactRspec_allPassing(t *testing.T) {
	t.Parallel()
	// Typical rspec output when all tests pass - "0 failures" contains "failure"
	// which is why the success detection bug existed.
	input := `....

Finished in 0.12345 seconds (files took 1.234 seconds to load)
5 examples, 0 failures
`
	out, ok := TryCompactRspec([]string{"rspec"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact ok output, got pass-through; input=%q", input)
	}
	s := string(out)
	if !strings.Contains(s, "[rspec] ok") {
		t.Errorf("want [rspec] ok prefix, got: %q", s)
	}
	if !strings.Contains(s, "5 examples, 0 failures") {
		t.Errorf("want summary in ok line, got: %q", s)
	}
}

func TestTryCompactRspec_withFailures(t *testing.T) {
	t.Parallel()
	// Rspec output with a failure - should extract Failures: section and summary.
	input := `....F

Failures:

  1) MyClass#my_method raises an error
     Failure/Error: expect(result).to eq(42)

       expected: 42
            got: 0

     # ./spec/my_class_spec.rb:12:in 'block (3 levels) in <top (required)>'

Finished in 0.05432 seconds (files took 1.234 seconds to load)
5 examples, 1 failure
`
	out, ok := TryCompactRspec([]string{"rspec"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact failure output, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "Failures:") {
		t.Errorf("want Failures: section, got: %q", s)
	}
	if !strings.Contains(s, "5 examples, 1 failure") {
		t.Errorf("want summary line, got: %q", s)
	}
	if strings.Contains(s, "Finished in") {
		t.Errorf("Finished in line should be stripped, got: %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact output should be shorter: got %d vs %d", len(s), len(input))
	}
}

func TestTryCompactRspec_nonRspec(t *testing.T) {
	t.Parallel()
	_, ok := TryCompactRspec([]string{"ruby", "-e", "1"}, []byte(""))
	if ok {
		t.Fatal("ruby -e not rspec")
	}
}

// TestTryCompactRspec_noSummaryNoFailures covers the compact=="" guard (line 80-82):
// rspec output with only dots (no summary line, no Failures section) → compactRspecOutput returns "".
func TestTryCompactRspec_noSummaryNoFailures(t *testing.T) {
	t.Parallel()
	// Dots-only output: no "X examples" summary, no "Failures:" section.
	_, ok := TryCompactRspec([]string{"rspec"}, []byte(".....\n"))
	if ok {
		t.Error("no-summary no-failures output: want pass-through (false), got true")
	}
}

func TestTryCompactMinitestAllPass(t *testing.T) {
	t.Parallel()
	var input strings.Builder
	input.WriteString("Run options: --seed 43125\n\n# Running:\n\n")
	input.WriteString(strings.Repeat(".", 120))
	input.WriteString("\n\nFinished in 0.421998s, 284.3612 runs/s, 568.7224 assertions/s.\n")
	input.WriteString("120 runs, 240 assertions, 0 failures, 0 errors, 0 skips\n")
	out, ok := TryCompactMinitest([]string{"ruby", "-Itest", "test/models/user_test.rb"}, []byte(input.String()))
	if !ok {
		t.Fatal("minitest all-pass output should compact")
	}
	s := string(out)
	if !strings.Contains(s, "[minitest] ok - 120 runs, 240 assertions") ||
		!strings.Contains(s, "120 runs, 240 assertions, 0 failures, 0 errors, 0 skips") {
		t.Fatalf("minitest compact summary missing counts: %q", s)
	}
	if strings.Contains(s, strings.Repeat(".", 20)) {
		t.Fatalf("minitest compact output leaked progress dots: %q", s)
	}
	if len(s) >= len(input.String()) {
		t.Fatalf("minitest compact output should be shorter: got %d vs %d", len(s), len(input.String()))
	}
}

func TestTryCompactMinitestBundleExecAllPass(t *testing.T) {
	t.Parallel()
	var input strings.Builder
	input.WriteString("Run options: --seed 9\n\n# Running:\n\n")
	input.WriteString(strings.Repeat(".", 80))
	input.WriteString("\n\nFinished in 0.123456s, 648.0000 runs/s, 1296.0000 assertions/s.\n")
	input.WriteString("80 runs, 160 assertions, 0 failures, 0 errors, 0 skips\n")
	out, ok := TryCompactRubyOutput([]string{"bundle", "exec", "ruby", "-I", "test", "test/controllers/widgets_test.rb"}, []byte(input.String()))
	if !ok || !strings.Contains(string(out), "[minitest] ok - 80 runs, 160 assertions") {
		t.Fatalf("bundle exec ruby minitest all-pass should compact: ok=%v out=%q", ok, out)
	}
}

func TestTryCompactMinitestRejectsRiskyOutput(t *testing.T) {
	t.Parallel()
	base := strings.Join([]string{
		"Run options: --seed 1",
		"",
		"# Running:",
		"",
		".....",
		"",
		"Finished in 0.100000s, 50.0000 runs/s, 100.0000 assertions/s.",
		"5 runs, 10 assertions, 0 failures, 0 errors, 0 skips",
		"",
	}, "\n")
	tests := []struct {
		name   string
		argv   []string
		output string
	}{
		{name: "non-test ruby", argv: []string{"ruby", "-e", "puts 1"}, output: base},
		{name: "failure count", argv: []string{"ruby", "-Itest", "test/widget_test.rb"}, output: strings.Replace(base, "0 failures", "1 failure", 1)},
		{name: "error count", argv: []string{"ruby", "-Itest", "test/widget_test.rb"}, output: strings.Replace(base, "0 errors", "1 error", 1)},
		{name: "skip count", argv: []string{"ruby", "-Itest", "test/widget_test.rb"}, output: strings.Replace(base, "0 skips", "1 skip", 1)},
		{name: "warning text", argv: []string{"ruby", "-Itest", "test/widget_test.rb"}, output: strings.Replace(base, "# Running:", "Warning: deprecated helper\n# Running:", 1)},
		{name: "progress mismatch", argv: []string{"ruby", "-Itest", "test/widget_test.rb"}, output: strings.Replace(base, ".....", "....", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := TryCompactMinitest(tt.argv, []byte(tt.output)); ok {
				t.Fatalf("risky minitest output must stay unmodified")
			}
		})
	}
}

// TestCompactRspecOutput_noSummaryNoFailures covers the summaryLine=="" && len(failures)==0 return (line 122-124).
func TestCompactRspecOutput_noSummaryNoFailures(t *testing.T) {
	t.Parallel()
	// Input with no "X examples" line and no "Failures:" section.
	got := compactRspecOutput(".....\n")
	if got != "" {
		t.Errorf("dots-only output: want empty string, got %q", got)
	}
}

package filter

import (
	"strings"
	"testing"
)

func TestParseFailuresFocusedLintDiagnostics(t *testing.T) {
	t.Parallel()

	var repeated strings.Builder
	for i := 0; i < 80; i++ {
		repeated.WriteString("internal/proxy/handler.go:164:15: Close() error return value is not checked\n")
	}
	got, ok := ParseFailures([]string{"errcheck", "./..."}, repeated.String())
	if !ok {
		t.Fatal("expected errcheck diagnostics to compact")
	}
	for _, want := range []string{
		"[errcheck] FAILED (80 diagnostics)",
		"internal/proxy/handler.go:164:15: Close() error return value is not checked",
		"(repeated 80 times)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused lint diagnostic missing %q in %q", want, got)
		}
	}

	misspellLine := `docs/readme.md:9:22 found "langauge" a misspelling of "language"`
	misspell := strings.Repeat(misspellLine+"\n", 40)
	got, ok = ParseFailures([]string{"misspell", "."}, misspell)
	if !ok {
		t.Fatal("expected misspell diagnostics to compact")
	}
	if !strings.Contains(got, "[misspell] FAILED (40 diagnostics)") ||
		!strings.Contains(got, `(repeated 40 times)`) {
		t.Fatalf("unexpected misspell compact output: %q", got)
	}

	csv := "file,line,column,typo,corrected\n" + strings.Repeat(`"README.md",9,22,langauge,language`+"\n", 40)
	got, ok = ParseFailures([]string{"misspell", "-f", "csv", "."}, csv)
	if !ok {
		t.Fatal("expected misspell CSV diagnostics to compact")
	}
	if !strings.Contains(got, "[misspell] FAILED (41 diagnostics)") ||
		!strings.Contains(got, `"README.md",9,22,langauge,language (repeated 40 times)`) {
		t.Fatalf("unexpected misspell CSV compact output: %q", got)
	}
}

func TestParseFailuresFocusedLintDiagnosticShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		line   string
		want   string
		prefix string
	}{
		{
			name:   "golangci-lint colon",
			argv:   []string{"golangci-lint", "run", "./..."},
			line:   "internal/app/app.go:10:2: unused-parameter: parameter ctx seems to be unused, consider removing or renaming it as _ (revive)",
			want:   "[golangci-lint] FAILED (35 diagnostics)",
			prefix: "running golangci-lint run ./...\n",
		},
		{
			name:   "ineffassign colon",
			argv:   []string{"ineffassign", "./..."},
			line:   "internal/app/app.go:10:2: ineffectual assignment to err",
			want:   "[ineffassign] FAILED (35 diagnostics)",
			prefix: "checking package internal/app\n",
		},
		{
			name:   "nilaway colon",
			argv:   []string{"nilaway", "./..."},
			line:   "internal/app/app.go:22:11: potential nil panic: field may be nil",
			want:   "[nilaway] FAILED (35 diagnostics)",
			prefix: "analyzing package internal/app\n",
		},
		{
			name:   "unparam colon",
			argv:   []string{"unparam", "./..."},
			line:   "internal/app/app.go:31:6: parameter ctx is unused",
			want:   "[unparam] FAILED (35 diagnostics)",
			prefix: "running unparam ./...\n",
		},
		{
			name:   "gocyclo score prefix",
			argv:   []string{"gocyclo", "-over", "10", "."},
			line:   "17 internal/proxy/wsmitm_phasef.go:2012:1: wssSafeReducerOKSummaryOutput has cyclomatic complexity 17 (> 10)",
			want:   "[gocyclo] FAILED (35 diagnostics)",
			prefix: "gocyclo -over 10 .\n",
		},
		{
			name:   "forbidigo colon",
			argv:   []string{"forbidigo", "./..."},
			line:   "internal/app/app.go:15:2: use of fmt.Print forbidden by pattern",
			want:   "[forbidigo] FAILED (35 diagnostics)",
			prefix: "$ forbidigo ./...\n",
		},
		{
			name:   "prealloc line only",
			argv:   []string{"prealloc", "./..."},
			line:   "internal/app/app.go:45: Consider preallocating values",
			want:   "[prealloc] FAILED (35 diagnostics)",
			prefix: "> prealloc ./...\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := tt.prefix + strings.Repeat(tt.line+"\n", 35)
			got, ok := ParseFailures(tt.argv, stdout)
			if !ok {
				t.Fatalf("expected %s diagnostics to compact", tt.name)
			}
			if !strings.Contains(got, tt.want) ||
				!strings.Contains(got, tt.line+" (repeated 35 times)") {
				t.Fatalf("unexpected compact output: %q", got)
			}
		})
	}
}

func TestParseFailuresFocusedLintDiagnosticsFailOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{
			name: "source context after diagnostic",
			argv: []string{"errcheck", "./..."},
			stdout: strings.Join([]string{
				"internal/proxy/handler.go:164:15: Close() error return value is not checked",
				"if err != nil {",
			}, "\n") + "\n",
		},
		{
			name:   "unknown command",
			argv:   []string{"customlint", "./..."},
			stdout: "internal/proxy/handler.go:164:15: Close() error return value is not checked\n",
		},
		{
			name:   "non shrinking single diagnostic",
			argv:   []string{"ineffassign", "./..."},
			stdout: "internal/app/app.go:10:2: ineffectual assignment to err\n",
		},
		{
			name:   "short misspell diagnostic",
			argv:   []string{"misspell", "."},
			stdout: `docs/readme.md:9:22 found "langauge" a misspelling of "language"` + "\n",
		},
		{
			name:   "success summary is not a diagnostic",
			argv:   []string{"nilaway", "./..."},
			stdout: "Success: no issues found\n",
		},
		{
			name: "golangci-lint source context",
			argv: []string{"golangci-lint", "run", "./..."},
			stdout: strings.Join([]string{
				"internal/app/app.go:10:2: unused-parameter: bad (revive)",
				"func run(ctx context.Context) error {",
			}, "\n") + "\n",
		},
		{
			name:   "golangci-lint unknown info line",
			argv:   []string{"golangci-lint", "run", "./..."},
			stdout: "level=info msg=\"golangci-lint has version 2.1.0\"\ninternal/app/app.go:10:2: unused-parameter: bad (revive)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := ParseFailures(tt.argv, tt.stdout); ok {
				t.Fatalf("expected fail-open, got %q", got)
			}
		})
	}
}

func TestTryCompactFocusedLintDiagnosticParsers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		line string
		try  func([]string, []byte) ([]byte, bool)
		want string
	}{
		{
			name: "golangci-lint",
			argv: []string{"golangci-lint", "run", "./..."},
			line: "internal/app/app.go:10:2: unused-parameter: parameter ctx seems to be unused, consider removing or renaming it as _ (revive)",
			try:  TryCompactGolangciLint,
			want: "[golangci-lint] FAILED (40 diagnostics)",
		},
		{
			name: "errcheck",
			argv: []string{"errcheck", "./..."},
			line: "internal/proxy/handler.go:164:15: Close() error return value is not checked",
			try:  TryCompactErrcheck,
			want: "[errcheck] FAILED (40 diagnostics)",
		},
		{
			name: "ineffassign",
			argv: []string{"ineffassign", "./..."},
			line: "internal/app/app.go:10:2: ineffectual assignment to err",
			try:  TryCompactIneffassign,
			want: "[ineffassign] FAILED (40 diagnostics)",
		},
		{
			name: "nilaway",
			argv: []string{"nilaway", "./..."},
			line: "internal/app/app.go:22:11: potential nil panic: field may be nil",
			try:  TryCompactNilaway,
			want: "[nilaway] FAILED (40 diagnostics)",
		},
		{
			name: "unparam",
			argv: []string{"unparam", "./..."},
			line: "internal/app/app.go:31:6: parameter ctx is unused",
			try:  TryCompactUnparam,
			want: "[unparam] FAILED (40 diagnostics)",
		},
		{
			name: "misspell",
			argv: []string{"misspell", "."},
			line: `docs/readme.md:9:22 found "langauge" a misspelling of "language"`,
			try:  TryCompactMisspell,
			want: "[misspell] FAILED (40 diagnostics)",
		},
		{
			name: "gocyclo",
			argv: []string{"gocyclo", "-over", "10", "."},
			line: "17 internal/proxy/wsmitm_phasef.go:2012:1: wssSafeReducerOKSummaryOutput has cyclomatic complexity 17 (> 10)",
			try:  TryCompactGocyclo,
			want: "[gocyclo] FAILED (40 diagnostics)",
		},
		{
			name: "forbidigo",
			argv: []string{"forbidigo", "./..."},
			line: "internal/app/app.go:15:2: use of fmt.Print forbidden by pattern",
			try:  TryCompactForbidigo,
			want: "[forbidigo] FAILED (40 diagnostics)",
		},
		{
			name: "prealloc",
			argv: []string{"prealloc", "./..."},
			line: "internal/app/app.go:45: Consider preallocating values",
			try:  TryCompactPrealloc,
			want: "[prealloc] FAILED (40 diagnostics)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := strings.Repeat(tt.line+"\n", 40)
			out, ok := tt.try(tt.argv, []byte(stdout))
			if !ok {
				t.Fatalf("expected %s direct parser to compact diagnostics", tt.name)
			}
			got := string(out)
			if !strings.Contains(got, tt.want) ||
				!strings.Contains(got, tt.line+" (repeated 40 times)") {
				t.Fatalf("unexpected %s direct compact output: %q", tt.name, got)
			}
		})
	}
}

func TestTryCompactFocusedLintDiagnosticParsersFailOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
		try    func([]string, []byte) ([]byte, bool)
	}{
		{
			name:   "wrong command",
			argv:   []string{"customlint", "./..."},
			stdout: strings.Repeat("internal/proxy/handler.go:164:15: Close() error return value is not checked\n", 40),
			try:    TryCompactErrcheck,
		},
		{
			name:   "golangci-lint unknown info line",
			argv:   []string{"golangci-lint", "run", "./..."},
			stdout: "level=info msg=\"golangci-lint has version 2.1.0\"\ninternal/app/app.go:10:2: unused-parameter: bad (revive)\n",
			try:    TryCompactGolangciLint,
		},
		{
			name: "source context",
			argv: []string{"errcheck", "./..."},
			stdout: strings.Join([]string{
				"internal/proxy/handler.go:164:15: Close() error return value is not checked",
				"if err != nil {",
				"",
			}, "\n"),
			try: TryCompactErrcheck,
		},
		{
			name:   "single non shrinking",
			argv:   []string{"ineffassign", "./..."},
			stdout: "internal/app/app.go:10:2: ineffectual assignment to err\n",
			try:    TryCompactIneffassign,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out, ok := tt.try(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("expected fail-open, got %q", out)
			}
		})
	}
}

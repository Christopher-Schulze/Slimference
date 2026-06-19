package filter

import "testing"

func TestTryCompactStylelintJSONClean(t *testing.T) {
	t.Parallel()
	in := `[{"source":"/src/a.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[]},` +
		`{"source":"/src/b.scss","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[],"ignored":false,"autofixed":false}]`
	out, ok := TryCompactStylelintJSON([]string{"stylelint", "--formatter", "json", "**/*.css"}, []byte(in))
	if !ok {
		t.Fatalf("expected compaction")
	}
	if got := string(out); got != "[stylelint] clean (2 file(s))\n" {
		t.Fatalf("clean summary = %q", got)
	}
}

func TestTryCompactStylelintJSONWrappedArgv(t *testing.T) {
	t.Parallel()
	in := `[{"source":"src/a.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[]}]`
	tests := [][]string{
		{`C:\repo\node_modules\.bin\stylelint.cmd`, "-f=json", "src/**/*.css"},
		{"npx", "-y", "stylelint", "-f", "json", "src/**/*.css"},
		{"pnpm", "exec", "stylelint", "--formatter=json", "src/**/*.css"},
		{"yarn", "stylelint", "--formatter", "json", "src/**/*.css"},
	}
	for _, argv := range tests {
		out, ok := TryCompactStylelintJSON(argv, []byte(in))
		if !ok {
			t.Fatalf("expected compaction for argv %#v", argv)
		}
		if got := string(out); got != "[stylelint] clean (1 file(s))\n" {
			t.Fatalf("clean summary for argv %#v = %q", argv, got)
		}
	}
}

func TestTryCompactStylelintJSONFailOpen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{
			name:   "not stylelint",
			argv:   []string{"eslint", "--format", "json"},
			stdout: `[]`,
		},
		{
			name:   "no json formatter",
			argv:   []string{"stylelint", "src/**/*.css"},
			stdout: `[]`,
		},
		{
			name:   "custom formatter",
			argv:   []string{"stylelint", "--custom-formatter", "./formatter.js", "--formatter", "json"},
			stdout: `[]`,
		},
		{
			name:   "custom formatter equals",
			argv:   []string{"stylelint", "--custom-formatter=./formatter.js", "--formatter", "json"},
			stdout: `[]`,
		},
		{
			name:   "npx without command",
			argv:   []string{"npx", "-y"},
			stdout: `[]`,
		},
		{
			name:   "pnpm exec non-stylelint",
			argv:   []string{"pnpm", "exec", "eslint", "-f", "json"},
			stdout: `[]`,
		},
		{
			name:   "yarn non-stylelint",
			argv:   []string{"yarn", "eslint", "-f", "json"},
			stdout: `[]`,
		},
		{
			name:   "zero files",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[]`,
		},
		{
			name:   "warning finding",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":true,"warnings":[{"line":3,"column":1,"rule":"block-no-empty","severity":"error","text":"Unexpected empty block"}],"deprecations":[],"invalidOptionWarnings":[]}]`,
		},
		{
			name:   "deprecation",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"warnings":[],"deprecations":[{"text":"deprecated"}],"invalidOptionWarnings":[]}]`,
		},
		{
			name:   "invalid option warning",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[{"text":"bad option"}]}]`,
		},
		{
			name:   "ignored",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[],"ignored":true}]`,
		},
		{
			name:   "autofixed",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[],"autofixed":true}]`,
		},
		{
			name:   "unknown schema field",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[],"parseErrors":[]}]`,
		},
		{
			name:   "missing source",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[]}]`,
		},
		{
			name:   "empty source",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":" ","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[]}]`,
		},
		{
			name:   "missing warnings",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"deprecations":[],"invalidOptionWarnings":[]}]`,
		},
		{
			name:   "warnings wrong type",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"warnings":{},"deprecations":[],"invalidOptionWarnings":[]}]`,
		},
		{
			name:   "errored wrong type",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":"false","warnings":[],"deprecations":[],"invalidOptionWarnings":[]}]`,
		},
		{
			name:   "ignored wrong type",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[],"ignored":"false"}]`,
		},
		{
			name:   "trailing non-json",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[]}]` + "\nMax warnings exceeded",
		},
		{
			name:   "second json value",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{"source":"src/a.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[]}] []`,
		},
		{
			name:   "malformed json",
			argv:   []string{"stylelint", "-f", "json"},
			stdout: `[{bad`,
		},
	}

	for _, tt := range tests {
		if _, ok := TryCompactStylelintJSON(tt.argv, []byte(tt.stdout)); ok {
			t.Fatalf("%s should fail open", tt.name)
		}
	}
}

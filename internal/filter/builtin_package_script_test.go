package filter

import (
	"strings"
	"testing"
)

func TestCompactPackageManagerBuildScriptOutput(t *testing.T) {
	t.Parallel()

	typecheck := "> web@1.0.0 typecheck\n> tsc --noEmit\n"
	out, ok := TryCompactBuildOutput([]string{"npm", "run", "typecheck"}, []byte(typecheck))
	if !ok || string(out) != "[tsc] ok\n" {
		t.Fatalf("npm run typecheck: ok=%v out=%q", ok, out)
	}

	var vite strings.Builder
	vite.WriteString("> web@1.0.0 build /repo\n")
	vite.WriteString("> vite build\n")
	out, ok = TryCompactBuildOutput([]string{"pnpm", "run", "build"}, []byte(vite.String()))
	if !ok || string(out) != "[vite build] ok\n" {
		t.Fatalf("pnpm run build: ok=%v out=%q", ok, out)
	}
}

func TestCompactPackageManagerBuildScriptTypeScriptFailureOutput(t *testing.T) {
	t.Parallel()

	var stdout strings.Builder
	stdout.WriteString("> web@1.0.0 typecheck /repo\n")
	stdout.WriteString("> tsc --noEmit\n")
	for i := 0; i < 80; i++ {
		stdout.WriteString("tsc progress line\n")
	}
	stdout.WriteString("src/app.ts(7,3): error TS2322: Type 'string' is not assignable to type 'number'.\n")
	stdout.WriteString("src/routes/+page.ts:3:11 - error TS2304: Cannot find name 'loadData'.\n")
	stdout.WriteString("Found 2 errors in 2 files.\n")

	out, ok := TryCompactBuildOutput([]string{"pnpm", "run", "typecheck"}, []byte(stdout.String()))
	if !ok {
		t.Fatal("expected package-script TypeScript failure diagnostics to compact")
	}
	for _, want := range []string{
		"[typescript] FAILED",
		"TS2322",
		"TS2304",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("compact package-script TypeScript failure missing %q in %q", want, out)
		}
	}
	if strings.Contains(string(out), "tsc progress line") {
		t.Fatalf("neutral TypeScript progress should be compacted out: %q", out)
	}
}

func TestCompactPackageManagerBuildScriptMypyOutput(t *testing.T) {
	t.Parallel()

	var stdout strings.Builder
	stdout.WriteString("> api@1.0.0 typecheck /repo\n")
	stdout.WriteString("> mypy src\n")
	for i := 0; i < 70; i++ {
		stdout.WriteString("src/app.py:10: error: Incompatible return value type\n")
	}
	stdout.WriteString("src/app.py:10: note: expected str\n")
	stdout.WriteString("Found 70 errors in 1 file (checked 48 source files)\n")

	out, ok := TryCompactBuildOutput([]string{"pnpm", "run", "typecheck"}, []byte(stdout.String()))
	if !ok {
		t.Fatal("expected package-script mypy diagnostics to compact")
	}
	for _, want := range []string{
		"[mypy] FAILED (71 diagnostics)",
		"(repeated 70 times)",
		"src/app.py:10: note: expected str",
		"Found 70 errors in 1 file",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("compact package-script mypy diagnostics missing %q in %q", want, out)
		}
	}

	success := "> api@1.0.0 typecheck /repo\n> mypy src\nSuccess: no issues found in 188 source files\n"
	out, ok = TryCompactBuildOutput([]string{"npm", "run", "typecheck"}, []byte(success))
	if !ok || !strings.Contains(string(out), "[mypy] ok (Success: no issues found in 188 source files)") {
		t.Fatalf("expected package-script mypy success to compact: ok=%v out=%q", ok, out)
	}
}

func TestCompactPackageManagerLintScriptOutput(t *testing.T) {
	t.Parallel()

	eslint := "> web@1.0.0 lint /repo\n> eslint .\n"
	out, ok := TryCompactLintOutput([]string{"pnpm", "run", "lint"}, []byte(eslint))
	if !ok || string(out) != "[eslint] ok\n" {
		t.Fatalf("pnpm run lint: ok=%v out=%q", ok, out)
	}

	biome := "bun run lint\n$ biome check .\n"
	out, ok = TryCompactLintOutput([]string{"bun", "run", "lint"}, []byte(biome))
	if !ok || string(out) != "[biome check] ok\n" {
		t.Fatalf("bun run lint: ok=%v out=%q", ok, out)
	}

	pnpmExec := "> app@1.0.0 lint\n> pnpm exec eslint .\n"
	out, ok = TryCompactLintOutput([]string{"npm", "run", "lint"}, []byte(pnpmExec))
	if !ok || string(out) != "[eslint] ok\n" {
		t.Fatalf("npm run lint with pnpm exec eslint: ok=%v out=%q", ok, out)
	}
}

func TestCompactPackageManagerFormatScriptOutput(t *testing.T) {
	t.Parallel()

	output := strings.Join([]string{
		"yarn run v1.22.22",
		"$ prettier --check .",
		"Done in 0.42s.",
		"",
	}, "\n")
	out, ok := TryCompactFormatOutput([]string{"yarn", "format:check"}, []byte(output))
	if !ok || string(out) != "[prettier] ok\n" {
		t.Fatalf("yarn format:check: ok=%v out=%q", ok, out)
	}
}

func TestCompactPackageManagerScriptOutputFailsOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
		try    func([]string, []byte) ([]byte, bool)
	}{
		{
			name:   "unsafe script name",
			argv:   []string{"npm", "run", "deploy"},
			stdout: "> app@1.0.0 deploy\n> eslint .\n",
			try:    TryCompactLintOutput,
		},
		{
			name:   "fix script name",
			argv:   []string{"npm", "run", "lint:fix"},
			stdout: "> app@1.0.0 lint:fix\n> eslint .\n",
			try:    TryCompactLintOutput,
		},
		{
			name:   "no inner command",
			argv:   []string{"pnpm", "run", "lint"},
			stdout: "Lint finished cleanly\n",
			try:    TryCompactLintOutput,
		},
		{
			name:   "inner shell pipeline",
			argv:   []string{"npm", "run", "lint"},
			stdout: "> app@1.0.0 lint\n> eslint . | tee lint.log\n",
			try:    TryCompactLintOutput,
		},
		{
			name:   "nested run script",
			argv:   []string{"npm", "run", "build"},
			stdout: "> app@1.0.0 build\n> npm run build\n",
			try:    TryCompactBuildOutput,
		},
		{
			name: "typecheck source context",
			argv: []string{"pnpm", "run", "typecheck"},
			stdout: strings.Join([]string{
				"> app@1.0.0 typecheck",
				"> tsc --noEmit",
				"src/App.tsx(12,7): error TS2322: Type 'string' is not assignable to type 'number'.",
				"import { missingName } from './missing';",
				"Found 1 error in 1 file.",
				"",
			}, "\n"),
			try: TryCompactBuildOutput,
		},
		{
			name:   "typecheck shell pipeline",
			argv:   []string{"pnpm", "run", "typecheck"},
			stdout: "> app@1.0.0 typecheck\n> tsc --noEmit | tee tsc.log\nsrc/app.ts(7,3): error TS2322: bad\n",
			try:    TryCompactBuildOutput,
		},
		{
			name:   "warning payload",
			argv:   []string{"npm", "run", "lint"},
			stdout: "> app@1.0.0 lint\n> eslint .\nwarning: generated config is deprecated\n",
			try:    TryCompactLintOutput,
		},
		{
			name:   "mypy stub notice rejected",
			argv:   []string{"npm", "run", "typecheck"},
			stdout: "> app@1.0.0 typecheck\n> mypy src\nSkipping analyzing 'requests': module is installed, but missing library stubs\nsrc/app.py:1: error: bad\nFound 1 error in 1 file\n",
			try:    TryCompactBuildOutput,
		},
		{
			name:   "mypy source context rejected",
			argv:   []string{"npm", "run", "typecheck"},
			stdout: "> app@1.0.0 typecheck\n> mypy src\nsrc/app.py:1: error: bad\nif value:\nFound 1 error in 1 file\n",
			try:    TryCompactBuildOutput,
		},
		{
			name:   "non shrinking",
			argv:   []string{"npm", "run", "lint"},
			stdout: "> eslint .\n",
			try:    TryCompactLintOutput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out, ok := tt.try(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("expected fail-open, got ok with %q", out)
			}
		})
	}
}

func TestCompactPackageManagerLintScriptFailureOutput(t *testing.T) {
	t.Parallel()

	var stdout strings.Builder
	stdout.WriteString("> web@1.0.0 lint /repo\n")
	stdout.WriteString("> errcheck ./...\n")
	for i := 0; i < 80; i++ {
		stdout.WriteString("internal/proxy/handler.go:164:15: Close() error return value is not checked\n")
	}

	out, ok := TryCompactLintOutput([]string{"pnpm", "run", "lint"}, []byte(stdout.String()))
	if !ok {
		t.Fatal("expected package-script lint failure diagnostics to compact")
	}
	for _, want := range []string{
		"[errcheck] FAILED (80 diagnostics)",
		"(repeated 80 times)",
		"internal/proxy/handler.go:164:15: Close() error return value is not checked",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("compact package-script lint failure missing %q in %q", want, out)
		}
	}

	stdout.Reset()
	stdout.WriteString("> api@1.0.0 lint /repo\n")
	stdout.WriteString("> golangci-lint run ./...\n")
	for i := 0; i < 80; i++ {
		stdout.WriteString("internal/app/app.go:10:2: unused-parameter: parameter ctx seems to be unused, consider removing or renaming it as _ (revive)\n")
	}
	out, ok = TryCompactLintOutput([]string{"npm", "run", "lint"}, []byte(stdout.String()))
	if !ok {
		t.Fatal("expected package-script golangci-lint diagnostics to compact")
	}
	for _, want := range []string{
		"[golangci-lint] FAILED (80 diagnostics)",
		"(repeated 80 times)",
		"internal/app/app.go:10:2: unused-parameter",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("compact package-script golangci-lint failure missing %q in %q", want, out)
		}
	}

	stdout.Reset()
	stdout.WriteString("> api@1.0.0 lint /repo\n")
	stdout.WriteString("> staticcheck ./...\n")
	for i := 0; i < 80; i++ {
		stdout.WriteString("internal/app/app.go:22:7: this value of err is never used (SA4006)\n")
	}
	out, ok = TryCompactLintOutput([]string{"pnpm", "run", "lint"}, []byte(stdout.String()))
	if !ok {
		t.Fatal("expected package-script staticcheck diagnostics to compact")
	}
	for _, want := range []string{
		"[staticcheck] FAILED (80 diagnostics)",
		"(repeated 80 times)",
		"internal/app/app.go:22:7: this value of err is never used",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("compact package-script staticcheck failure missing %q in %q", want, out)
		}
	}
}

func TestCompactPackageManagerLintScriptFailureOutputFailsOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{
			name:   "source context",
			argv:   []string{"pnpm", "run", "lint"},
			stdout: "> web@1.0.0 lint /repo\n> errcheck ./...\ninternal/proxy/handler.go:164:15: Close() error return value is not checked\nif err != nil {\n",
		},
		{
			name:   "shell pipeline",
			argv:   []string{"npm", "run", "lint"},
			stdout: "> web@1.0.0 lint\n> errcheck ./... | tee lint.log\ninternal/proxy/handler.go:164:15: Close() error return value is not checked\n",
		},
		{
			name:   "unsafe script",
			argv:   []string{"npm", "run", "lint:fix"},
			stdout: "> web@1.0.0 lint:fix\n> errcheck ./...\ninternal/proxy/handler.go:164:15: Close() error return value is not checked\n",
		},
		{
			name:   "short non shrinking",
			argv:   []string{"pnpm", "run", "lint"},
			stdout: "> web@1.0.0 lint /repo\n> errcheck ./...\ninternal/proxy/handler.go:164:15: Close() error return value is not checked\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out, ok := TryCompactLintOutput(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("expected fail-open, got %q", out)
			}
		})
	}
}

package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactPrettier(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactPrettier([]string{"prettier", "--check", "."}, []byte(""))
	if !ok || string(out) != "[prettier] ok\n" {
		t.Fatalf("prettier: ok=%v %q", ok, out)
	}
	out2, ok := TryCompactPrettier([]string{"npx", "prettier", "--write", "a.ts"}, []byte("\n"))
	if !ok || string(out2) != "[prettier] ok\n" {
		t.Fatalf("npx: %q", out2)
	}
	prPnpm, ok := TryCompactPrettier([]string{"pnpm", "exec", "prettier", "--check", "."}, []byte(""))
	if !ok || string(prPnpm) != "[prettier] ok\n" {
		t.Fatalf("pnpm prettier: %q", prPnpm)
	}
	prYarn, ok := TryCompactPrettier([]string{"yarn", "prettier", "src/"}, []byte(""))
	if !ok || string(prYarn) != "[prettier] ok\n" {
		t.Fatalf("yarn prettier: %q", prYarn)
	}
	if _, ok := TryCompactPrettier([]string{"eslint", "."}, []byte("")); ok {
		t.Fatal("eslint not prettier")
	}
	dp, ok := TryCompactDprint([]string{"dprint", "fmt"}, []byte(""))
	if !ok || string(dp) != "[dprint fmt] ok\n" {
		t.Fatalf("dprint: %q", dp)
	}
	dpNpx, ok := TryCompactDprint([]string{"npx", "dprint", "fmt"}, []byte("\n"))
	if !ok || string(dpNpx) != "[dprint fmt] ok\n" {
		t.Fatalf("npx dprint fmt: %q", dpNpx)
	}
	dpYarn, ok := TryCompactDprint([]string{"yarn", "dprint", "fmt", "."}, []byte(""))
	if !ok || string(dpYarn) != "[dprint fmt] ok\n" {
		t.Fatalf("yarn dprint fmt: %q", dpYarn)
	}
	dpPnpm, ok := TryCompactDprint([]string{"pnpm", "exec", "dprint", "fmt", "."}, []byte(""))
	if !ok || string(dpPnpm) != "[dprint fmt] ok\n" {
		t.Fatalf("pnpm exec dprint fmt: %q", dpPnpm)
	}
	if _, ok := TryCompactDprint([]string{"dprint", "check"}, []byte("")); ok {
		t.Fatal("dprint check not fmt")
	}
	bf, ok := TryCompactBiomeFormat([]string{"biome", "format", "--write", "."}, []byte(""))
	if !ok || string(bf) != "[biome format] ok\n" {
		t.Fatalf("biome format: %q", bf)
	}
	bfNpx, ok := TryCompactBiomeFormat([]string{"npx", "biome", "format", "."}, []byte("\n"))
	if !ok || string(bfNpx) != "[biome format] ok\n" {
		t.Fatalf("npx biome format: %q", bfNpx)
	}
	bfPnpm, ok := TryCompactBiomeFormat([]string{"pnpm", "exec", "biome", "format", "src/"}, []byte(""))
	if !ok || string(bfPnpm) != "[biome format] ok\n" {
		t.Fatalf("pnpm biome format: %q", bfPnpm)
	}
	if _, ok := TryCompactBiomeFormat([]string{"biome", "check", "."}, []byte("")); ok {
		t.Fatal("biome check is not format")
	}
	bufFmt, ok := TryCompactBufFormat([]string{"buf", "format", "-w"}, []byte(""))
	if !ok || string(bufFmt) != "[buf format] ok\n" {
		t.Fatalf("buf format: %q", bufFmt)
	}
	bufNpx, ok := TryCompactBufFormat([]string{"npx", "buf", "format", "."}, []byte(""))
	if !ok || string(bufNpx) != "[buf format] ok\n" {
		t.Fatalf("npx buf format: %q", bufNpx)
	}
	bufPnpm, ok := TryCompactBufFormat([]string{"pnpm", "exec", "buf", "format", "-w"}, []byte("\n"))
	if !ok || string(bufPnpm) != "[buf format] ok\n" {
		t.Fatalf("pnpm buf format: %q", bufPnpm)
	}
	if _, ok := TryCompactBufFormat([]string{"buf", "lint"}, []byte("")); ok {
		t.Fatal("buf lint not format")
	}
	tf, ok := TryCompactTerraformFmt([]string{"terraform", "fmt", "-check"}, []byte(""))
	if !ok || string(tf) != "[terraform fmt] ok\n" {
		t.Fatalf("terraform fmt: %q", tf)
	}
	tofuF, ok := TryCompactTerraformFmt([]string{"tofu", "fmt", "-check"}, []byte("\n"))
	if !ok || string(tofuF) != "[terraform fmt] ok\n" {
		t.Fatalf("tofu fmt: %q", tofuF)
	}
	tfNpx, ok := TryCompactTerraformFmt([]string{"npx", "terraform", "fmt", "."}, []byte(""))
	if !ok || string(tfNpx) != "[terraform fmt] ok\n" {
		t.Fatalf("npx terraform fmt: %q", tfNpx)
	}
	tfNpxY, ok := TryCompactTerraformFmt([]string{"npx", "-y", "terraform", "fmt"}, []byte(""))
	if !ok || string(tfNpxY) != "[terraform fmt] ok\n" {
		t.Fatalf("npx -y terraform fmt: %q", tfNpxY)
	}
	tfPnpm, ok := TryCompactTerraformFmt([]string{"pnpm", "exec", "tofu", "fmt", "-check"}, []byte("\n"))
	if !ok || string(tfPnpm) != "[terraform fmt] ok\n" {
		t.Fatalf("pnpm tofu fmt: %q", tfPnpm)
	}
	tfYarn, ok := TryCompactTerraformFmt([]string{"yarn", "terraform", "fmt"}, []byte(""))
	if !ok || string(tfYarn) != "[terraform fmt] ok\n" {
		t.Fatalf("yarn terraform fmt: %q", tfYarn)
	}
	if _, ok := TryCompactTerraformFmt([]string{"terraform", "validate"}, []byte("")); ok {
		t.Fatal("terraform validate not fmt")
	}
	bl, ok := TryCompactBlack([]string{"black", "--check", "src/"}, []byte(""))
	if !ok || string(bl) != "[black] ok\n" {
		t.Fatalf("black: %q", bl)
	}
	blNpx, ok := TryCompactBlack([]string{"npx", "black", "--check", "."}, []byte(""))
	if !ok || string(blNpx) != "[black] ok\n" {
		t.Fatalf("npx black: %q", blNpx)
	}
	blPnpm, ok := TryCompactBlack([]string{"pnpm", "exec", "black", "src/"}, []byte("\n"))
	if !ok || string(blPnpm) != "[black] ok\n" {
		t.Fatalf("pnpm black: %q", blPnpm)
	}
	blPy, ok := TryCompactBlack([]string{"python3", "-m", "black", "."}, []byte(""))
	if !ok || string(blPy) != "[black] ok\n" {
		t.Fatalf("python -m black: %q", blPy)
	}
	blPnpmPy, ok := TryCompactBlack([]string{"pnpm", "exec", "python", "-m", "black", "src/"}, []byte(""))
	if !ok || string(blPnpmPy) != "[black] ok\n" {
		t.Fatalf("pnpm python -m black: %q", blPnpmPy)
	}
	rf, ok := TryCompactRuffFormat([]string{"ruff", "format", "--check", "."}, []byte(""))
	if !ok || string(rf) != "[ruff format] ok\n" {
		t.Fatalf("ruff format: %q", rf)
	}
	rfNpx, ok := TryCompactRuffFormat([]string{"npx", "ruff", "format", "."}, []byte(""))
	if !ok || string(rfNpx) != "[ruff format] ok\n" {
		t.Fatalf("npx ruff format: %q", rfNpx)
	}
	rfPnpm, ok := TryCompactRuffFormat([]string{"pnpm", "exec", "ruff", "format", "src/"}, []byte(""))
	if !ok || string(rfPnpm) != "[ruff format] ok\n" {
		t.Fatalf("pnpm ruff format: %q", rfPnpm)
	}
	rfPy, ok := TryCompactRuffFormat([]string{"python3", "-m", "ruff", "format", "."}, []byte(""))
	if !ok || string(rfPy) != "[ruff format] ok\n" {
		t.Fatalf("python -m ruff format: %q", rfPy)
	}
	rfPnpmPy, ok := TryCompactRuffFormat([]string{"pnpm", "exec", "python", "-m", "ruff", "format", "src/"}, []byte("\n"))
	if !ok || string(rfPnpmPy) != "[ruff format] ok\n" {
		t.Fatalf("pnpm python -m ruff format: %q", rfPnpmPy)
	}
	tpf, ok := TryCompactTaploFormat([]string{"taplo", "format", "x.toml"}, []byte(""))
	if !ok || string(tpf) != "[taplo format] ok\n" {
		t.Fatalf("taplo format: %q", tpf)
	}
	tpfNpx, ok := TryCompactTaploFormat([]string{"npx", "taplo", "format", "a.toml"}, []byte(""))
	if !ok || string(tpfNpx) != "[taplo format] ok\n" {
		t.Fatalf("npx taplo format: %q", tpfNpx)
	}
	if _, ok := TryCompactTaploFormat([]string{"taplo", "check", "x.toml"}, []byte("")); ok {
		t.Fatal("taplo check not format")
	}
	sh, ok := TryCompactShfmt([]string{"shfmt", "-w", "script.sh"}, []byte(""))
	if !ok || string(sh) != "[shfmt] ok\n" {
		t.Fatalf("shfmt: %q", sh)
	}
	shNpx, ok := TryCompactShfmt([]string{"npx", "shfmt", "-w", "."}, []byte("\n"))
	if !ok || string(shNpx) != "[shfmt] ok\n" {
		t.Fatalf("npx shfmt: %q", shNpx)
	}
	shNpxY, ok := TryCompactShfmt([]string{"npx", "--yes", "shfmt", "-w", "."}, []byte(""))
	if !ok || string(shNpxY) != "[shfmt] ok\n" {
		t.Fatalf("npx --yes shfmt: %q", shNpxY)
	}
	if _, ok := TryCompactShfmt([]string{"npx", "shfmt", "-l", "."}, []byte("")); ok {
		t.Fatal("npx shfmt -l")
	}
	if _, ok := TryCompactShfmt([]string{"shfmt", "-l", "."}, []byte("")); ok {
		t.Fatal("shfmt -l")
	}
	sqfmt, ok := TryCompactSqlfmt([]string{"sqlfmt", "q.sql"}, []byte("\n"))
	if !ok || string(sqfmt) != "[sqlfmt] ok\n" {
		t.Fatalf("sqlfmt: %q", sqfmt)
	}
	sqfmtNpx, ok := TryCompactSqlfmt([]string{"npx", "sqlfmt", "q.sql"}, []byte(""))
	if !ok || string(sqfmtNpx) != "[sqlfmt] ok\n" {
		t.Fatalf("npx sqlfmt: %q", sqfmtNpx)
	}
	sqfmtPy, ok := TryCompactSqlfmt([]string{"python3", "-m", "sqlfmt", "q.sql"}, []byte("\n"))
	if !ok || string(sqfmtPy) != "[sqlfmt] ok\n" {
		t.Fatalf("python -m sqlfmt: %q", sqfmtPy)
	}
	sqfmtPnpmPy, ok := TryCompactSqlfmt([]string{"pnpm", "exec", "python", "-m", "sqlfmt", "."}, []byte(""))
	if !ok || string(sqfmtPnpmPy) != "[sqlfmt] ok\n" {
		t.Fatalf("pnpm python -m sqlfmt: %q", sqfmtPnpmPy)
	}
	is, ok := TryCompactIsort([]string{"isort", "--check-only", "."}, []byte(""))
	if !ok || string(is) != "[isort] ok\n" {
		t.Fatalf("isort: %q", is)
	}
	isPnpm, ok := TryCompactIsort([]string{"pnpm", "exec", "isort", "--check-only", "."}, []byte(""))
	if !ok || string(isPnpm) != "[isort] ok\n" {
		t.Fatalf("pnpm isort: %q", isPnpm)
	}
	isPy, ok := TryCompactIsort([]string{"python", "-m", "isort", "."}, []byte("\n"))
	if !ok || string(isPy) != "[isort] ok\n" {
		t.Fatalf("python -m isort: %q", isPy)
	}
	ap8, ok := TryCompactAutopep8([]string{"autopep8", "--diff", "a.py"}, []byte(""))
	if !ok || string(ap8) != "[autopep8] ok\n" {
		t.Fatalf("autopep8: %q", ap8)
	}
	ap8Npx, ok := TryCompactAutopep8([]string{"npx", "autopep8", "a.py"}, []byte("\n"))
	if !ok || string(ap8Npx) != "[autopep8] ok\n" {
		t.Fatalf("npx autopep8: %q", ap8Npx)
	}
	ap8YarnPy, ok := TryCompactAutopep8([]string{"yarn", "python3", "-m", "autopep8", "a.py"}, []byte(""))
	if !ok || string(ap8YarnPy) != "[autopep8] ok\n" {
		t.Fatalf("yarn python -m autopep8: %q", ap8YarnPy)
	}
	if _, ok := TryCompactRuffFormat([]string{"ruff", "check", "."}, []byte("")); ok {
		t.Fatal("ruff check not format")
	}
	gofmtOut, ok := TryCompactGofmt([]string{"gofmt", "-l", "."}, []byte(""))
	if !ok || string(gofmtOut) != "[gofmt] ok\n" {
		t.Fatalf("gofmt: %q", gofmtOut)
	}
	gofmtNpx, ok := TryCompactGofmt([]string{"npx", "gofmt", "-w", "."}, []byte(""))
	if !ok || string(gofmtNpx) != "[gofmt] ok\n" {
		t.Fatalf("npx gofmt: %q", gofmtNpx)
	}
	goFmtOut, ok := TryCompactGofmt([]string{"go", "fmt", "./..."}, []byte(""))
	if !ok || string(goFmtOut) != "[go fmt] ok\n" {
		t.Fatalf("go fmt: %q", goFmtOut)
	}
	goFmtNpx, ok := TryCompactGofmt([]string{"npx", "go", "fmt", "./..."}, []byte("\n"))
	if !ok || string(goFmtNpx) != "[go fmt] ok\n" {
		t.Fatalf("npx go fmt: %q", goFmtNpx)
	}
	goFmtPnpm, ok := TryCompactGofmt([]string{"pnpm", "exec", "go", "fmt"}, []byte(""))
	if !ok || string(goFmtPnpm) != "[go fmt] ok\n" {
		t.Fatalf("pnpm go fmt: %q", goFmtPnpm)
	}
	rustF, ok := TryCompactRustfmt([]string{"rustfmt", "--check", "src/lib.rs"}, []byte(""))
	if !ok || string(rustF) != "[rustfmt] ok\n" {
		t.Fatalf("rustfmt: %q", rustF)
	}
	rustNpx, ok := TryCompactRustfmt([]string{"yarn", "rustfmt", "--check", "a.rs"}, []byte("\n"))
	if !ok || string(rustNpx) != "[rustfmt] ok\n" {
		t.Fatalf("yarn rustfmt: %q", rustNpx)
	}
	cf, ok := TryCompactClangFormat([]string{"clang-format-18", "--dry-run", "a.cc"}, []byte(""))
	if !ok || string(cf) != "[clang-format] ok\n" {
		t.Fatalf("clang-format: %q", cf)
	}
	cfNpx, ok := TryCompactClangFormat([]string{"npx", "clang-format-18", "--dry-run", "a.cc"}, []byte(""))
	if !ok || string(cfNpx) != "[clang-format] ok\n" {
		t.Fatalf("npx clang-format: %q", cfNpx)
	}
	zf, ok := TryCompactZigFmt([]string{"zig", "fmt", "src/"}, []byte(""))
	if !ok || string(zf) != "[zig fmt] ok\n" {
		t.Fatalf("zig fmt: %q", zf)
	}
	zfNpx, ok := TryCompactZigFmt([]string{"npx", "zig", "fmt", "."}, []byte(""))
	if !ok || string(zfNpx) != "[zig fmt] ok\n" {
		t.Fatalf("npx zig fmt: %q", zfNpx)
	}
}

// TestTryCompactFormat_missingBranches covers guard and wrapper branches missed above.
func TestTryCompactFormat_missingBranches(t *testing.T) {
	t.Parallel()

	// --- isPrettierArgv: len < 1 (L9) ---
	if _, ok := TryCompactPrettier([]string{}, []byte("")); ok {
		t.Fatal("prettier empty argv")
	}

	// --- TryCompactPrettier: non-empty stdout (L33) ---
	if _, ok := TryCompactPrettier([]string{"prettier", "."}, []byte("output\n")); ok {
		t.Fatal("prettier non-empty stdout")
	}

	// --- TryCompactBiomeFormat: non-empty stdout (L55) ---
	if _, ok := TryCompactBiomeFormat([]string{"biome", "format", "."}, []byte("output\n")); ok {
		t.Fatal("biome format non-empty stdout")
	}

	// --- TryCompactBiomeFormat: yarn biome format (L71) ---
	bfYarn, ok := TryCompactBiomeFormat([]string{"yarn", "biome", "format", "."}, []byte(""))
	if !ok || string(bfYarn) != "[biome format] ok\n" {
		t.Fatalf("yarn biome format: ok=%v %q", ok, bfYarn)
	}

	// --- isPyPkgToolArgv: len < 1 (L101) ---
	if _, ok := TryCompactBlack([]string{}, []byte("")); ok {
		t.Fatal("black empty argv")
	}
	// --- isPyPkgToolArgv: npx failure (L107) ---
	if _, ok := TryCompactBlack([]string{"npx", "-y"}, []byte("")); ok {
		t.Fatal("npx -y: no black command")
	}
	// --- isPyPkgToolArgv: python without -m tool (L131) ---
	if _, ok := TryCompactBlack([]string{"python3", "setup.py"}, []byte("")); ok {
		t.Fatal("python3 setup.py: no -m black")
	}

	// --- shfmtTailArgs: len < 1 (L172) ---
	if _, ok := TryCompactShfmt([]string{}, []byte("")); ok {
		t.Fatal("shfmt empty argv")
	}
	// --- shfmtTailArgs: pnpm exec shfmt (L179) ---
	shPnpm, ok := TryCompactShfmt([]string{"pnpm", "exec", "shfmt", "-w", "."}, []byte(""))
	if !ok || string(shPnpm) != "[shfmt] ok\n" {
		t.Fatalf("pnpm exec shfmt: ok=%v %q", ok, shPnpm)
	}
	// --- shfmtTailArgs: yarn shfmt (L181) ---
	shYarn, ok := TryCompactShfmt([]string{"yarn", "shfmt", "-w", "."}, []byte(""))
	if !ok || string(shYarn) != "[shfmt] ok\n" {
		t.Fatalf("yarn shfmt: ok=%v %q", ok, shYarn)
	}
	// --- TryCompactShfmt: non-empty stdout (L202) ---
	if _, ok := TryCompactShfmt([]string{"shfmt", "-w", "."}, []byte("output\n")); ok {
		t.Fatal("shfmt non-empty stdout")
	}

	// --- goFmtCompactOutput: len < 1 (L242) ---
	if _, ok := goFmtCompactOutput([]string{}); ok {
		t.Fatal("goFmtCompactOutput empty argv")
	}

	// --- TryCompactGofmt: yarn branch calling goFmtCompactOutput (L275) ---
	if _, ok := TryCompactGofmt([]string{"yarn", "not-gofmt"}, []byte("")); ok {
		t.Fatal("yarn not-gofmt: should not match")
	}

	// --- TryCompactRustfmt: len < 1 (L287) ---
	if _, ok := TryCompactRustfmt([]string{}, []byte("")); ok {
		t.Fatal("rustfmt empty argv")
	}
	// --- TryCompactRustfmt: npxMatches (L294) ---
	rustNpx, ok := TryCompactRustfmt([]string{"npx", "rustfmt", "--check", "a.rs"}, []byte(""))
	if !ok || string(rustNpx) != "[rustfmt] ok\n" {
		t.Fatalf("npx rustfmt: ok=%v %q", ok, rustNpx)
	}
	// --- TryCompactRustfmt: pnpm exec (L297) ---
	rustPnpm, ok := TryCompactRustfmt([]string{"pnpm", "exec", "rustfmt", "a.rs"}, []byte(""))
	if !ok || string(rustPnpm) != "[rustfmt] ok\n" {
		t.Fatalf("pnpm exec rustfmt: ok=%v %q", ok, rustPnpm)
	}

	// --- TryCompactClangFormat: len < 1 (L316) ---
	if _, ok := TryCompactClangFormat([]string{}, []byte("")); ok {
		t.Fatal("clang-format empty argv")
	}
	// --- TryCompactClangFormat: pnpm exec (L326) ---
	cfPnpm, ok := TryCompactClangFormat([]string{"pnpm", "exec", "clang-format-18", "a.cc"}, []byte(""))
	if !ok || string(cfPnpm) != "[clang-format] ok\n" {
		t.Fatalf("pnpm exec clang-format: ok=%v %q", ok, cfPnpm)
	}
	// --- TryCompactClangFormat: yarn (L329) ---
	cfYarn, ok := TryCompactClangFormat([]string{"yarn", "clang-format", "a.cc"}, []byte(""))
	if !ok || string(cfYarn) != "[clang-format] ok\n" {
		t.Fatalf("yarn clang-format: ok=%v %q", ok, cfYarn)
	}

	// --- TryCompactZigFmt: len < 2 (L340) ---
	if _, ok := TryCompactZigFmt([]string{"zig"}, []byte("")); ok {
		t.Fatal("zig (no subcommand): len<2")
	}
	// --- TryCompactZigFmt: pnpm exec zig fmt (L350) ---
	zfPnpm, ok := TryCompactZigFmt([]string{"pnpm", "exec", "zig", "fmt", "."}, []byte(""))
	if !ok || string(zfPnpm) != "[zig fmt] ok\n" {
		t.Fatalf("pnpm exec zig fmt: ok=%v %q", ok, zfPnpm)
	}
	// --- TryCompactZigFmt: yarn zig fmt (L353) ---
	zfYarn, ok := TryCompactZigFmt([]string{"yarn", "zig", "fmt", "."}, []byte(""))
	if !ok || string(zfYarn) != "[zig fmt] ok\n" {
		t.Fatalf("yarn zig fmt: ok=%v %q", ok, zfYarn)
	}
	// --- TryCompactGofmt: yarn gofmt success (L275-277) ---
	gfYarn, ok := TryCompactGofmt([]string{"yarn", "gofmt", "."}, []byte(""))
	if !ok || string(gfYarn) != "[gofmt] ok\n" {
		t.Fatalf("yarn gofmt: ok=%v %q", ok, gfYarn)
	}
}

func TestTryCompactPrettierCleanCheckOutput(t *testing.T) {
	t.Parallel()

	clean := []byte("Checking formatting...\nAll matched files use Prettier code style!\n")
	out, ok := TryCompactPrettier([]string{"prettier", "--check", "."}, clean)
	if !ok || string(out) != "[prettier] ok\n" {
		t.Fatalf("prettier clean check: ok=%v out=%q", ok, out)
	}

	out, ok = TryCompactFormatOutput([]string{"pnpm", "run", "format:check"}, []byte(strings.Join([]string{
		"> app@1.0.0 format:check /repo",
		"> prettier --check .",
		"Checking formatting...",
		"All matched files use Prettier code style!",
		"",
	}, "\n")))
	if !ok || string(out) != "[prettier] ok\n" {
		t.Fatalf("package-script prettier clean check: ok=%v out=%q", ok, out)
	}
}

func TestTryCompactPrettierCleanCheckOutputFailsOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{
			name:   "no check arg",
			argv:   []string{"prettier", "."},
			stdout: "Checking formatting...\nAll matched files use Prettier code style!\n",
		},
		{
			name:   "warning issue",
			argv:   []string{"prettier", "--check", "."},
			stdout: "Checking formatting...\n[warn] src/app.ts\n[warn] Code style issues found in the above file. Run Prettier with --write to fix.\n",
		},
		{
			name:   "unknown line",
			argv:   []string{"prettier", "--check", "."},
			stdout: "Checking formatting...\nAll matched files use Prettier code style!\nwarning: generated config is deprecated\n",
		},
		{
			name:   "missing clean verdict",
			argv:   []string{"prettier", "--check", "."},
			stdout: "Checking formatting...\n",
		},
		{
			name:   "verdict before checking",
			argv:   []string{"prettier", "--check", "."},
			stdout: "All matched files use Prettier code style!\nChecking formatting...\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out, ok := TryCompactPrettier(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("expected fail-open, got ok with %q", out)
			}
		})
	}
}

func TestTryCompactFormatOutput_manyFiles(t *testing.T) {
	t.Parallel()
	// prettier --write outputting 15 formatted filenames (>formatFileListMax=10) → compact
	var sb strings.Builder
	for i := 0; i < 15; i++ {
		sb.WriteString(fmt.Sprintf("src/components/Component%d.tsx\n", i))
	}
	input := []byte(sb.String())
	out, ok := TryCompactFormatOutput([]string{"prettier", "--write", "src/"}, input)
	if !ok {
		t.Fatalf("expected compact for 15 files, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "[prettier] 15 file(s) formatted") {
		t.Errorf("want file count, got: %q", s)
	}
	if !strings.Contains(s, "src/components/Component0.tsx") || !strings.Contains(s, "src/components/Component14.tsx") {
		t.Errorf("want sampled first and last formatted files, got: %q", s)
	}
	if !strings.Contains(s, "[+5 more files]") {
		t.Errorf("want omitted-file marker, got: %q", s)
	}
	if strings.Contains(s, "src/components/Component8.tsx") {
		t.Errorf("want middle noise omitted, got: %q", s)
	}
}

func TestTryCompactFormatOutput_fewFiles(t *testing.T) {
	t.Parallel()
	// 3 files <= formatFileListMax=10 → pass-through
	input := []byte("src/a.ts\nsrc/b.ts\nsrc/c.ts\n")
	_, ok := TryCompactFormatOutput([]string{"prettier", "--write", "src/"}, input)
	if ok {
		t.Fatal("expected pass-through for 3 files, got compact")
	}
}

func TestTryCompactFormatOutput_gofmtManyFiles(t *testing.T) {
	t.Parallel()
	// gofmt -l with 12 files → compact
	var sb strings.Builder
	for i := 0; i < 12; i++ {
		sb.WriteString(fmt.Sprintf("pkg/very/deep/generated/service/component/file%d_with_long_name.go\n", i))
	}
	out, ok := TryCompactFormatOutput([]string{"gofmt", "-l", "."}, []byte(sb.String()))
	if !ok {
		t.Fatalf("expected compact for gofmt 12 files")
	}
	if !strings.Contains(string(out), "[gofmt] 12 file(s) formatted") {
		t.Errorf("want gofmt count, got: %q", out)
	}
	if !strings.Contains(string(out), "pkg/very/deep/generated/service/component/file0_with_long_name.go") ||
		!strings.Contains(string(out), "pkg/very/deep/generated/service/component/file11_with_long_name.go") {
		t.Errorf("want sampled gofmt changed files, got: %q", out)
	}
}

func TestTryCompactFormatOutput_emptyStdout(t *testing.T) {
	t.Parallel()
	// empty stdout → per-tool ok summary
	out, ok := TryCompactFormatOutput([]string{"prettier", "--write", "src/"}, []byte(""))
	if !ok || string(out) != "[prettier] ok\n" {
		t.Fatalf("empty stdout: ok=%v %q", ok, out)
	}
}

func TestTryCompactFormatOutput_unknownTool(t *testing.T) {
	t.Parallel()
	// unknown tool → pass-through even with many lines
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		sb.WriteString(fmt.Sprintf("file%d.txt\n", i))
	}
	_, ok := TryCompactFormatOutput([]string{"unknown-tool", "--write", "."}, []byte(sb.String()))
	if ok {
		t.Fatal("unknown tool should not compact")
	}
}

// TestTryCompactBiomeFormat_shortArgv covers the len(argv) < 2 guard (line 59-61):
// argv = ["format"] passes argvContainsToken but has length 1 → return false.
func TestTryCompactBiomeFormat_shortArgv(t *testing.T) {
	t.Parallel()
	// 1-element argv passes argvContainsToken("format") but len(argv)<2 fires.
	_, ok := TryCompactBiomeFormat([]string{"format"}, []byte(""))
	if ok {
		t.Error("len(argv)<2: want false, got true")
	}
}

// TestFormatToolLabel_biomePaths covers the npx/pnpm/yarn biome format branches (lines 377-385)
// and the ruff format branch (line 396-398) and npx clang-format (line 452-454).
func TestFormatToolLabel_biomePaths(t *testing.T) {
	t.Parallel()
	// npx biome format → "biome" (line 377-379)
	if got := formatToolLabel([]string{"npx", "biome", "format", "."}); got != "biome" {
		t.Errorf("npx biome format: want 'biome', got %q", got)
	}
	// pnpm exec biome format → "biome" (line 380-382)
	if got := formatToolLabel([]string{"pnpm", "exec", "biome", "format", "src/"}); got != "biome" {
		t.Errorf("pnpm exec biome format: want 'biome', got %q", got)
	}
	// yarn biome format → "biome" (line 383-385)
	if got := formatToolLabel([]string{"yarn", "biome", "format", "."}); got != "biome" {
		t.Errorf("yarn biome format: want 'biome', got %q", got)
	}
	// ruff format → "ruff" (line 396-398)
	if got := formatToolLabel([]string{"ruff", "format", "."}); got != "ruff" {
		t.Errorf("ruff format: want 'ruff', got %q", got)
	}
	// npx clang-format → "clang-format" (line 452-454)
	if got := formatToolLabel([]string{"npx", "clang-format", "--style=google"}); got != "clang-format" {
		t.Errorf("npx clang-format: want 'clang-format', got %q", got)
	}
}

// TestCompactFormatFilelist_compactNotShorter covers the len(out) >= len(s) guard (line 492-494):
// 11 very short file lines where the compact "[label] 11 file(s) formatted\n" is longer.
func TestCompactFormatFilelist_compactNotShorter(t *testing.T) {
	t.Parallel()
	// 11 one-char lines "x\n" = 22 chars; compact "[fmt] 11 file(s) formatted\n" = 27 chars > 22.
	input := strings.Repeat("x\n", 11)
	_, ok := compactFormatFilelist(input, "fmt")
	if ok {
		t.Error("compact >= original for short lines: want false, got true")
	}
}

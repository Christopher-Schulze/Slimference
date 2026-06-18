package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactLintOutput(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactCargoClippy([]string{"cargo", "clippy", "--", "-D", "warnings"}, []byte(""))
	if !ok || string(out) != "[cargo clippy] ok\n" {
		t.Fatalf("clippy: ok=%v %q", ok, out)
	}
	clNpx, ok := TryCompactCargoClippy([]string{"npx", "cargo", "clippy"}, []byte("\n"))
	if !ok || string(clNpx) != "[cargo clippy] ok\n" {
		t.Fatalf("npx cargo clippy: %q", clNpx)
	}
	out2, ok := TryCompactGolangciLint([]string{"golangci-lint", "run", "./..."}, []byte("\n"))
	if !ok || string(out2) != "[golangci-lint] ok\n" {
		t.Fatalf("golangci-lint: %q", out2)
	}
	glNpx, ok := TryCompactGolangciLint([]string{"npx", "golangci-lint", "run"}, []byte(""))
	if !ok || string(glNpx) != "[golangci-lint] ok\n" {
		t.Fatalf("npx golangci-lint: %q", glNpx)
	}
	st, ok := TryCompactStaticcheck([]string{"staticcheck", "./..."}, []byte(""))
	if !ok || string(st) != "[staticcheck] ok\n" {
		t.Fatalf("staticcheck: %q", st)
	}
	stNpx, ok := TryCompactStaticcheck([]string{"pnpm", "exec", "staticcheck", "./..."}, []byte("\n"))
	if !ok || string(stNpx) != "[staticcheck] ok\n" {
		t.Fatalf("pnpm staticcheck: %q", stNpx)
	}
	gc, ok := TryCompactGocritic([]string{"gocritic", "check", "./..."}, []byte("\n"))
	if !ok || string(gc) != "[gocritic] ok\n" {
		t.Fatalf("gocritic: %q", gc)
	}
	gcNpx, ok := TryCompactGocritic([]string{"npx", "gocritic", "check", "."}, []byte(""))
	if !ok || string(gcNpx) != "[gocritic] ok\n" {
		t.Fatalf("npx gocritic check: %q", gcNpx)
	}
	gs, ok := TryCompactGosec([]string{"gosec", "./..."}, []byte(""))
	if !ok || string(gs) != "[gosec] ok\n" {
		t.Fatalf("gosec: %q", gs)
	}
	gsYarn, ok := TryCompactGosec([]string{"yarn", "gosec", "./..."}, []byte(""))
	if !ok || string(gsYarn) != "[gosec] ok\n" {
		t.Fatalf("yarn gosec: %q", gsYarn)
	}
	bl, ok := TryCompactBufLint([]string{"buf", "lint"}, []byte(""))
	if !ok || string(bl) != "[buf lint] ok\n" {
		t.Fatalf("buf lint: %q", bl)
	}
	blNpx, ok := TryCompactBufLint([]string{"npx", "buf", "lint"}, []byte(""))
	if !ok || string(blNpx) != "[buf lint] ok\n" {
		t.Fatalf("npx buf lint: %q", blNpx)
	}
	blPnpm, ok := TryCompactBufLint([]string{"pnpm", "exec", "buf", "lint", "."}, []byte("\n"))
	if !ok || string(blPnpm) != "[buf lint] ok\n" {
		t.Fatalf("pnpm exec buf lint: %q", blPnpm)
	}
	pl, ok := TryCompactProtolint([]string{"protolint", "api/"}, []byte("\n"))
	if !ok || string(pl) != "[protolint] ok\n" {
		t.Fatalf("protolint: %q", pl)
	}
	plNpx, ok := TryCompactProtolint([]string{"npx", "protolint", "api/"}, []byte(""))
	if !ok || string(plNpx) != "[protolint] ok\n" {
		t.Fatalf("npx protolint: %q", plNpx)
	}
	sg, ok := TryCompactSemgrep([]string{"semgrep", "--config", "auto"}, []byte(""))
	if !ok || string(sg) != "[semgrep] ok\n" {
		t.Fatalf("semgrep: %q", sg)
	}
	sgNpx, ok := TryCompactSemgrep([]string{"npx", "semgrep", "--config", "auto"}, []byte("\n"))
	if !ok || string(sgNpx) != "[semgrep] ok\n" {
		t.Fatalf("npx semgrep: %q", sgNpx)
	}
	sgPy, ok := TryCompactSemgrep([]string{"python3", "-m", "semgrep", "--config", "auto"}, []byte(""))
	if !ok || string(sgPy) != "[semgrep] ok\n" {
		t.Fatalf("python -m semgrep: %q", sgPy)
	}
	jsc, ok := TryCompactJscpd([]string{"jscpd", "src/"}, []byte(""))
	if !ok || string(jsc) != "[jscpd] ok\n" {
		t.Fatalf("jscpd: %q", jsc)
	}
	jscNpx, ok := TryCompactJscpd([]string{"npx", "jscpd", "src/"}, []byte(""))
	if !ok || string(jscNpx) != "[jscpd] ok\n" {
		t.Fatalf("npx jscpd: %q", jscNpx)
	}
	spc, ok := TryCompactSpectralLint([]string{"spectral", "lint", "api.yaml"}, []byte(""))
	if !ok || string(spc) != "[spectral lint] ok\n" {
		t.Fatalf("spectral lint: %q", spc)
	}
	spcYarn, ok := TryCompactSpectralLint([]string{"yarn", "spectral", "lint", "openapi/"}, []byte(""))
	if !ok || string(spcYarn) != "[spectral lint] ok\n" {
		t.Fatalf("yarn spectral lint: %q", spcYarn)
	}
	spcPnpm, ok := TryCompactSpectralLint([]string{"pnpm", "exec", "spectral", "lint", "api.yaml"}, []byte(""))
	if !ok || string(spcPnpm) != "[spectral lint] ok\n" {
		t.Fatalf("pnpm exec spectral lint: %q", spcPnpm)
	}
	if _, ok := TryCompactSpectralLint([]string{"spectral", "version"}, []byte("")); ok {
		t.Fatal("spectral without lint")
	}
	dj, ok := TryCompactDjlint([]string{"djlint", "templates/"}, []byte(""))
	if !ok || string(dj) != "[djlint] ok\n" {
		t.Fatalf("djlint: %q", dj)
	}
	djNpx, ok := TryCompactDjlint([]string{"pnpm", "exec", "djlint", "templates/"}, []byte(""))
	if !ok || string(djNpx) != "[djlint] ok\n" {
		t.Fatalf("pnpm djlint: %q", djNpx)
	}
	djPy, ok := TryCompactDjlint([]string{"python", "-m", "djlint", "templates/"}, []byte("\n"))
	if !ok || string(djPy) != "[djlint] ok\n" {
		t.Fatalf("python -m djlint: %q", djPy)
	}
	ty, ok := TryCompactTyCheck([]string{"ty", "check"}, []byte(""))
	if !ok || string(ty) != "[ty check] ok\n" {
		t.Fatalf("ty check: %q", ty)
	}
	tyNpx, ok := TryCompactTyCheck([]string{"npx", "ty", "check"}, []byte("\n"))
	if !ok || string(tyNpx) != "[ty check] ok\n" {
		t.Fatalf("npx ty check: %q", tyNpx)
	}
	tyPnpm, ok := TryCompactTyCheck([]string{"pnpm", "exec", "ty", "check"}, []byte(""))
	if !ok || string(tyPnpm) != "[ty check] ok\n" {
		t.Fatalf("pnpm exec ty check: %q", tyPnpm)
	}
	zz, ok := TryCompactZizmor([]string{"zizmor", ".github/workflows/ci.yml"}, []byte(""))
	if !ok || string(zz) != "[zizmor] ok\n" {
		t.Fatalf("zizmor: %q", zz)
	}
	zzNpx, ok := TryCompactZizmor([]string{"npx", "zizmor", "."}, []byte(""))
	if !ok || string(zzNpx) != "[zizmor] ok\n" {
		t.Fatalf("npx zizmor: %q", zzNpx)
	}
	kl, ok := TryCompactKubeLinter([]string{"kube-linter", "lint", "chart/"}, []byte("\n"))
	if !ok || string(kl) != "[kube-linter] ok\n" {
		t.Fatalf("kube-linter: %q", kl)
	}
	klNpx, ok := TryCompactKubeLinter([]string{"npx", "kube-linter", "chart/"}, []byte(""))
	if !ok || string(klNpx) != "[kube-linter] ok\n" {
		t.Fatalf("npx kube-linter: %q", klNpx)
	}
	pyr, ok := TryCompactPyright([]string{"pyright", "src/"}, []byte(""))
	if !ok || string(pyr) != "[pyright] ok\n" {
		t.Fatalf("pyright: %q", pyr)
	}
	pyr2, ok := TryCompactPyright([]string{"basedpyright", "."}, []byte("\n"))
	if !ok || string(pyr2) != "[pyright] ok\n" {
		t.Fatalf("basedpyright: %q", pyr2)
	}
	pyrNpx, ok := TryCompactPyright([]string{"npx", "pyright", "."}, []byte(""))
	if !ok || string(pyrNpx) != "[pyright] ok\n" {
		t.Fatalf("npx pyright: %q", pyrNpx)
	}
	pyrPnpm, ok := TryCompactPyright([]string{"pnpm", "exec", "basedpyright", "src/"}, []byte(""))
	if !ok || string(pyrPnpm) != "[pyright] ok\n" {
		t.Fatalf("pnpm basedpyright: %q", pyrPnpm)
	}
	ans, ok := TryCompactAnsibleLint([]string{"ansible-lint", "playbooks/"}, []byte(""))
	if !ok || string(ans) != "[ansible-lint] ok\n" {
		t.Fatalf("ansible-lint: %q", ans)
	}
	ansNpx, ok := TryCompactAnsibleLint([]string{"npx", "ansible-lint", "."}, []byte("\n"))
	if !ok || string(ansNpx) != "[ansible-lint] ok\n" {
		t.Fatalf("npx ansible-lint: %q", ansNpx)
	}
	cueV, ok := TryCompactCueVet([]string{"cue", "vet", "./schema"}, []byte(""))
	if !ok || string(cueV) != "[cue vet] ok\n" {
		t.Fatalf("cue vet: %q", cueV)
	}
	cueNpx, ok := TryCompactCueVet([]string{"npx", "cue", "vet", "."}, []byte(""))
	if !ok || string(cueNpx) != "[cue vet] ok\n" {
		t.Fatalf("npx cue vet: %q", cueNpx)
	}
	cuePnpm, ok := TryCompactCueVet([]string{"pnpm", "exec", "cue", "vet", "./schema"}, []byte("\n"))
	if !ok || string(cuePnpm) != "[cue vet] ok\n" {
		t.Fatalf("pnpm exec cue vet: %q", cuePnpm)
	}
	if _, ok := TryCompactCueVet([]string{"cue", "version"}, []byte("")); ok {
		t.Fatal("cue version not vet")
	}
	tfL, ok := TryCompactTflint([]string{"tflint", "--chdir=infra"}, []byte(""))
	if !ok || string(tfL) != "[tflint] ok\n" {
		t.Fatalf("tflint: %q", tfL)
	}
	tfLNpx, ok := TryCompactTflint([]string{"pnpm", "exec", "tflint", "."}, []byte("\n"))
	if !ok || string(tfLNpx) != "[tflint] ok\n" {
		t.Fatalf("pnpm tflint: %q", tfLNpx)
	}
	pnt, ok := TryCompactPint([]string{"pint", "--test"}, []byte("\n"))
	if !ok || string(pnt) != "[pint] ok\n" {
		t.Fatalf("pint: %q", pnt)
	}
	pntNpx, ok := TryCompactPint([]string{"npx", "pint"}, []byte(""))
	if !ok || string(pntNpx) != "[pint] ok\n" {
		t.Fatalf("npx pint: %q", pntNpx)
	}
	ph, ok := TryCompactPhpcs([]string{"phpcs", "--standard=PSR12", "src/"}, []byte(""))
	if !ok || string(ph) != "[phpcs] ok\n" {
		t.Fatalf("phpcs: %q", ph)
	}
	phPnpm, ok := TryCompactPhpcs([]string{"pnpm", "exec", "phpcs", "src/"}, []byte("\n"))
	if !ok || string(phPnpm) != "[phpcs] ok\n" {
		t.Fatalf("pnpm phpcs: %q", phPnpm)
	}
	pst, ok := TryCompactPhpstan([]string{"phpstan", "analyse"}, []byte(""))
	if !ok || string(pst) != "[phpstan] ok\n" {
		t.Fatalf("phpstan: %q", pst)
	}
	pstNpx, ok := TryCompactPhpstan([]string{"npx", "phpstan", "analyse"}, []byte(""))
	if !ok || string(pstNpx) != "[phpstan] ok\n" {
		t.Fatalf("npx phpstan: %q", pstNpx)
	}
	psm, ok := TryCompactPsalm([]string{"psalm"}, []byte("\n"))
	if !ok || string(psm) != "[psalm] ok\n" {
		t.Fatalf("psalm: %q", psm)
	}
	psmYarn, ok := TryCompactPsalm([]string{"yarn", "psalm"}, []byte(""))
	if !ok || string(psmYarn) != "[psalm] ok\n" {
		t.Fatalf("yarn psalm: %q", psmYarn)
	}
	phn, ok := TryCompactPhan([]string{"phan", "-k", ".phan/config.php"}, []byte(""))
	if !ok || string(phn) != "[phan] ok\n" {
		t.Fatalf("phan: %q", phn)
	}
	phnNpx, ok := TryCompactPhan([]string{"npx", "phan"}, []byte(""))
	if !ok || string(phnNpx) != "[phan] ok\n" {
		t.Fatalf("npx phan: %q", phnNpx)
	}
	cfn, ok := TryCompactCfnLint([]string{"cfn-lint", "template.yaml"}, []byte("\n"))
	if !ok || string(cfn) != "[cfn-lint] ok\n" {
		t.Fatalf("cfn-lint: %q", cfn)
	}
	cfnYarn, ok := TryCompactCfnLint([]string{"yarn", "cfn-lint", "t.yaml"}, []byte(""))
	if !ok || string(cfnYarn) != "[cfn-lint] ok\n" {
		t.Fatalf("yarn cfn-lint: %q", cfnYarn)
	}
	dvl, ok := TryCompactDotenvLinter([]string{"dotenv-linter", ".env"}, []byte(""))
	if !ok || string(dvl) != "[dotenv-linter] ok\n" {
		t.Fatalf("dotenv-linter: %q", dvl)
	}
	dvlPnpm, ok := TryCompactDotenvLinter([]string{"pnpm", "exec", "dotenv-linter", ".env"}, []byte("\n"))
	if !ok || string(dvlPnpm) != "[dotenv-linter] ok\n" {
		t.Fatalf("pnpm dotenv-linter: %q", dvlPnpm)
	}
	gf, ok := TryCompactGofumpt([]string{"gofumpt", "-l", "."}, []byte(""))
	if !ok || string(gf) != "[gofumpt] ok\n" {
		t.Fatalf("gofumpt: %q", gf)
	}
	gfNpx, ok := TryCompactGofumpt([]string{"npx", "gofumpt", "."}, []byte(""))
	if !ok || string(gfNpx) != "[gofumpt] ok\n" {
		t.Fatalf("npx gofumpt: %q", gfNpx)
	}
	rv, ok := TryCompactRevive([]string{"revive", "./..."}, []byte(""))
	if !ok || string(rv) != "[revive] ok\n" {
		t.Fatalf("revive: %q", rv)
	}
	rvPnpm, ok := TryCompactRevive([]string{"pnpm", "exec", "revive", "-config", "c.toml", "./..."}, []byte("\n"))
	if !ok || string(rvPnpm) != "[revive] ok\n" {
		t.Fatalf("pnpm revive: %q", rvPnpm)
	}
	erc, ok := TryCompactErrcheck([]string{"errcheck", "./..."}, []byte("\n"))
	if !ok || string(erc) != "[errcheck] ok\n" {
		t.Fatalf("errcheck: %q", erc)
	}
	ercNpx, ok := TryCompactErrcheck([]string{"npx", "errcheck", "./..."}, []byte(""))
	if !ok || string(ercNpx) != "[errcheck] ok\n" {
		t.Fatalf("npx errcheck: %q", ercNpx)
	}
	ercNpxY, ok := TryCompactErrcheck([]string{"npx", "-y", "errcheck", "./..."}, []byte(""))
	if !ok || string(ercNpxY) != "[errcheck] ok\n" {
		t.Fatalf("npx -y errcheck: %q", ercNpxY)
	}
	ia, ok := TryCompactIneffassign([]string{"ineffassign", "./..."}, []byte(""))
	if !ok || string(ia) != "[ineffassign] ok\n" {
		t.Fatalf("ineffassign: %q", ia)
	}
	nw, ok := TryCompactNilaway([]string{"nilaway", "./..."}, []byte(""))
	if !ok || string(nw) != "[nilaway] ok\n" {
		t.Fatalf("nilaway: %q", nw)
	}
	gv, ok := TryCompactGoVet([]string{"go", "vet", "./..."}, []byte(""))
	if !ok || string(gv) != "[go vet] ok\n" {
		t.Fatalf("go vet: %q", gv)
	}
	gvNpx, ok := TryCompactGoVet([]string{"npx", "-y", "go", "vet", "./..."}, []byte("\n"))
	if !ok || string(gvNpx) != "[go vet] ok\n" {
		t.Fatalf("npx go vet: %q", gvNpx)
	}
	gvPnpm, ok := TryCompactGoVet([]string{"pnpm", "exec", "go", "vet", "./..."}, []byte(""))
	if !ok || string(gvPnpm) != "[go vet] ok\n" {
		t.Fatalf("pnpm go vet: %q", gvPnpm)
	}
	unp, ok := TryCompactUnparam([]string{"unparam", "./..."}, []byte("\n"))
	if !ok || string(unp) != "[unparam] ok\n" {
		t.Fatalf("unparam: %q", unp)
	}
	ms, ok := TryCompactMisspell([]string{"misspell", "-error", "."}, []byte(""))
	if !ok || string(ms) != "[misspell] ok\n" {
		t.Fatalf("misspell: %q", ms)
	}
	gcy, ok := TryCompactGocyclo([]string{"gocyclo", "."}, []byte(""))
	if !ok || string(gcy) != "[gocyclo] ok\n" {
		t.Fatalf("gocyclo: %q", gcy)
	}
	fbd, ok := TryCompactForbidigo([]string{"forbidigo", "./..."}, []byte(""))
	if !ok || string(fbd) != "[forbidigo] ok\n" {
		t.Fatalf("forbidigo: %q", fbd)
	}
	pre, ok := TryCompactPrealloc([]string{"prealloc", "./..."}, []byte(""))
	if !ok || string(pre) != "[prealloc] ok\n" {
		t.Fatalf("prealloc: %q", pre)
	}
	prePnpm, ok := TryCompactPrealloc([]string{"pnpm", "exec", "prealloc", "."}, []byte("\n"))
	if !ok || string(prePnpm) != "[prealloc] ok\n" {
		t.Fatalf("pnpm prealloc: %q", prePnpm)
	}
	out3, ok := TryCompactRuffCheck([]string{"ruff", "check", "."}, []byte(""))
	if !ok || string(out3) != "[ruff check] ok\n" {
		t.Fatalf("ruff: %q", out3)
	}
	ruffPnpm, ok := TryCompactRuffCheck([]string{"pnpm", "exec", "ruff", "check", "src/"}, []byte(""))
	if !ok || string(ruffPnpm) != "[ruff check] ok\n" {
		t.Fatalf("pnpm ruff check: %q", ruffPnpm)
	}
	ruffNpx, ok := TryCompactRuffCheck([]string{"npx", "ruff", "check", "."}, []byte("\n"))
	if !ok || string(ruffNpx) != "[ruff check] ok\n" {
		t.Fatalf("npx ruff check: %q", ruffNpx)
	}
	ruffPy, ok := TryCompactRuffCheck([]string{"python", "-m", "ruff", "check", "src/"}, []byte(""))
	if !ok || string(ruffPy) != "[ruff check] ok\n" {
		t.Fatalf("python -m ruff check: %q", ruffPy)
	}
	ruffYarnPy, ok := TryCompactRuffCheck([]string{"yarn", "python3", "-m", "ruff", "check", "."}, []byte("\n"))
	if !ok || string(ruffYarnPy) != "[ruff check] ok\n" {
		t.Fatalf("yarn python -m ruff check: %q", ruffYarnPy)
	}
	cargoAud, ok := TryCompactCargoAudit([]string{"cargo", "audit"}, []byte(""))
	if !ok || string(cargoAud) != "[cargo audit] ok\n" {
		t.Fatalf("cargo audit: %q", cargoAud)
	}
	cargoAudPnpm, ok := TryCompactCargoAudit([]string{"pnpm", "exec", "cargo", "audit"}, []byte(""))
	if !ok || string(cargoAudPnpm) != "[cargo audit] ok\n" {
		t.Fatalf("pnpm cargo audit: %q", cargoAudPnpm)
	}
	if _, ok := TryCompactCargoAudit([]string{"cargo", "tree"}, []byte("")); ok {
		t.Fatal("cargo tree not audit")
	}
	pyl, ok := TryCompactPylint([]string{"pylint", "pkg"}, []byte(""))
	if !ok || string(pyl) != "[pylint] ok\n" {
		t.Fatalf("pylint: %q", pyl)
	}
	pyl2, ok := TryCompactPylint([]string{"python3", "-m", "pylint", "."}, []byte("\n"))
	if !ok || string(pyl2) != "[pylint] ok\n" {
		t.Fatalf("python -m pylint: %q", pyl2)
	}
	pylNpx, ok := TryCompactPylint([]string{"npx", "pylint", "pkg"}, []byte(""))
	if !ok || string(pylNpx) != "[pylint] ok\n" {
		t.Fatalf("npx pylint: %q", pylNpx)
	}
	pylPnpmPy, ok := TryCompactPylint([]string{"pnpm", "exec", "python3", "-m", "pylint", "."}, []byte(""))
	if !ok || string(pylPnpmPy) != "[pylint] ok\n" {
		t.Fatalf("pnpm python -m pylint: %q", pylPnpmPy)
	}
	flk, ok := TryCompactFlake8([]string{"flake8", "src"}, []byte(""))
	if !ok || string(flk) != "[flake8] ok\n" {
		t.Fatalf("flake8: %q", flk)
	}
	flk2, ok := TryCompactFlake8([]string{"python", "-m", "flake8"}, []byte(""))
	if !ok || string(flk2) != "[flake8] ok\n" {
		t.Fatalf("python -m flake8: %q", flk2)
	}
	flkPnpm, ok := TryCompactFlake8([]string{"pnpm", "exec", "flake8", "."}, []byte("\n"))
	if !ok || string(flkPnpm) != "[flake8] ok\n" {
		t.Fatalf("pnpm flake8: %q", flkPnpm)
	}
	flkYarnPy, ok := TryCompactFlake8([]string{"yarn", "python", "-m", "flake8"}, []byte(""))
	if !ok || string(flkYarnPy) != "[flake8] ok\n" {
		t.Fatalf("yarn python -m flake8: %q", flkYarnPy)
	}
	band, ok := TryCompactBandit([]string{"bandit", "-r", "."}, []byte(""))
	if !ok || string(band) != "[bandit] ok\n" {
		t.Fatalf("bandit: %q", band)
	}
	bandPy, ok := TryCompactBandit([]string{"python3", "-m", "bandit", "src"}, []byte("\n"))
	if !ok || string(bandPy) != "[bandit] ok\n" {
		t.Fatalf("python -m bandit: %q", bandPy)
	}
	bandYarn, ok := TryCompactBandit([]string{"yarn", "bandit", "-r", "."}, []byte(""))
	if !ok || string(bandYarn) != "[bandit] ok\n" {
		t.Fatalf("yarn bandit: %q", bandYarn)
	}
	bandPnpmPy, ok := TryCompactBandit([]string{"pnpm", "exec", "python3", "-m", "bandit", "src"}, []byte(""))
	if !ok || string(bandPnpmPy) != "[bandit] ok\n" {
		t.Fatalf("pnpm python -m bandit: %q", bandPnpmPy)
	}
	sh, ok := TryCompactShellcheck([]string{"shellcheck", "script.sh"}, []byte(""))
	if !ok || string(sh) != "[shellcheck] ok\n" {
		t.Fatalf("shellcheck: %q", sh)
	}
	shNpx, ok := TryCompactShellcheck([]string{"npx", "shellcheck", "a.sh"}, []byte(""))
	if !ok || string(shNpx) != "[shellcheck] ok\n" {
		t.Fatalf("npx shellcheck: %q", shNpx)
	}
	had, ok := TryCompactHadolint([]string{"hadolint", "Dockerfile"}, []byte(""))
	if !ok || string(had) != "[hadolint] ok\n" {
		t.Fatalf("hadolint: %q", had)
	}
	hadNpx, ok := TryCompactHadolint([]string{"npx", "hadolint", "Dockerfile"}, []byte(""))
	if !ok || string(hadNpx) != "[hadolint] ok\n" {
		t.Fatalf("npx hadolint: %q", hadNpx)
	}
	mdl, ok := TryCompactMarkdownlint([]string{"markdownlint", "**/*.md"}, []byte(""))
	if !ok || string(mdl) != "[markdownlint] ok\n" {
		t.Fatalf("markdownlint: %q", mdl)
	}
	mdlPnpm, ok := TryCompactMarkdownlint([]string{"pnpm", "exec", "markdownlint", "."}, []byte(""))
	if !ok || string(mdlPnpm) != "[markdownlint] ok\n" {
		t.Fatalf("pnpm markdownlint: %q", mdlPnpm)
	}
	yml, ok := TryCompactYamllint([]string{"yamllint", ".github/"}, []byte(""))
	if !ok || string(yml) != "[yamllint] ok\n" {
		t.Fatalf("yamllint: %q", yml)
	}
	ymlYarn, ok := TryCompactYamllint([]string{"yarn", "yamllint", "*.yml"}, []byte(""))
	if !ok || string(ymlYarn) != "[yamllint] ok\n" {
		t.Fatalf("yarn yamllint: %q", ymlYarn)
	}
	ymlPy, ok := TryCompactYamllint([]string{"pnpm", "exec", "python3", "-m", "yamllint", ".github/"}, []byte(""))
	if !ok || string(ymlPy) != "[yamllint] ok\n" {
		t.Fatalf("pnpm python -m yamllint: %q", ymlPy)
	}
	al, ok := TryCompactActionlint([]string{"actionlint", ".github/workflows/ci.yml"}, []byte(""))
	if !ok || string(al) != "[actionlint] ok\n" {
		t.Fatalf("actionlint: %q", al)
	}
	alPnpm, ok := TryCompactActionlint([]string{"pnpm", "exec", "actionlint"}, []byte(""))
	if !ok || string(alPnpm) != "[actionlint] ok\n" {
		t.Fatalf("pnpm actionlint: %q", alPnpm)
	}
	vale, ok := TryCompactVale([]string{"vale", "docs/"}, []byte(""))
	if !ok || string(vale) != "[vale] ok\n" {
		t.Fatalf("vale: %q", vale)
	}
	valeNpx, ok := TryCompactVale([]string{"npx", "vale", "."}, []byte("\n"))
	if !ok || string(valeNpx) != "[vale] ok\n" {
		t.Fatalf("npx vale: %q", valeNpx)
	}
	out4, ok := TryCompactBiomeCheck([]string{"biome", "check", "src"}, []byte(""))
	if !ok || string(out4) != "[biome check] ok\n" {
		t.Fatalf("biome: %q", out4)
	}
	bioNpx, ok := TryCompactBiomeCheck([]string{"npx", "biome", "check", "."}, []byte("\n"))
	if !ok || string(bioNpx) != "[biome check] ok\n" {
		t.Fatalf("npx biome check: %q", bioNpx)
	}
	bioPnpm, ok := TryCompactBiomeCheck([]string{"pnpm", "exec", "biome", "check", "src"}, []byte(""))
	if !ok || string(bioPnpm) != "[biome check] ok\n" {
		t.Fatalf("pnpm exec biome: %q", bioPnpm)
	}
	bioYarn, ok := TryCompactBiomeCheck([]string{"yarn", "biome", "check"}, []byte(""))
	if !ok || string(bioYarn) != "[biome check] ok\n" {
		t.Fatalf("yarn biome check: %q", bioYarn)
	}
	if _, ok := TryCompactBiomeCheck([]string{"npx", "eslint", "check"}, []byte("")); ok {
		t.Fatal("npx eslint check not biome")
	}
	sqf, ok := TryCompactSqlfluffLint([]string{"sqlfluff", "lint", "q.sql"}, []byte(""))
	if !ok || string(sqf) != "[sqlfluff lint] ok\n" {
		t.Fatalf("sqlfluff: %q", sqf)
	}
	sqfNpx, ok := TryCompactSqlfluffLint([]string{"npx", "sqlfluff", "lint", "."}, []byte(""))
	if !ok || string(sqfNpx) != "[sqlfluff lint] ok\n" {
		t.Fatalf("npx sqlfluff lint: %q", sqfNpx)
	}
	sqfPy, ok := TryCompactSqlfluffLint([]string{"python3", "-m", "sqlfluff", "lint", "q.sql"}, []byte("\n"))
	if !ok || string(sqfPy) != "[sqlfluff lint] ok\n" {
		t.Fatalf("python -m sqlfluff lint: %q", sqfPy)
	}
	sqfPnpmPy, ok := TryCompactSqlfluffLint([]string{"pnpm", "exec", "python", "-m", "sqlfluff", "lint", "."}, []byte(""))
	if !ok || string(sqfPnpmPy) != "[sqlfluff lint] ok\n" {
		t.Fatalf("pnpm python -m sqlfluff lint: %q", sqfPnpmPy)
	}
	tap, ok := TryCompactTaploCheck([]string{"taplo", "check", "x.toml"}, []byte(""))
	if !ok || string(tap) != "[taplo check] ok\n" {
		t.Fatalf("taplo: %q", tap)
	}
	tapNpx, ok := TryCompactTaploCheck([]string{"yarn", "taplo", "check"}, []byte("\n"))
	if !ok || string(tapNpx) != "[taplo check] ok\n" {
		t.Fatalf("yarn taplo check: %q", tapNpx)
	}
	tapPnpm, ok := TryCompactTaploCheck([]string{"pnpm", "exec", "taplo", "check", "x.toml"}, []byte(""))
	if !ok || string(tapPnpm) != "[taplo check] ok\n" {
		t.Fatalf("pnpm exec taplo check: %q", tapPnpm)
	}
	ox, ok := TryCompactOxlint([]string{"oxlint", "."}, []byte(""))
	if !ok || string(ox) != "[oxlint] ok\n" {
		t.Fatalf("oxlint: %q", ox)
	}
	oxNpx, ok := TryCompactOxlint([]string{"npx", "oxlint", "src/"}, []byte(""))
	if !ok || string(oxNpx) != "[oxlint] ok\n" {
		t.Fatalf("npx oxlint: %q", oxNpx)
	}
	denoL, ok := TryCompactDenoLint([]string{"deno", "lint"}, []byte(""))
	if !ok || string(denoL) != "[deno lint] ok\n" {
		t.Fatalf("deno lint: %q", denoL)
	}
	denoNpx, ok := TryCompactDenoLint([]string{"npx", "deno", "lint"}, []byte("\n"))
	if !ok || string(denoNpx) != "[deno lint] ok\n" {
		t.Fatalf("npx deno lint: %q", denoNpx)
	}
	denoPnpm, ok := TryCompactDenoLint([]string{"pnpm", "exec", "deno", "lint"}, []byte(""))
	if !ok || string(denoPnpm) != "[deno lint] ok\n" {
		t.Fatalf("pnpm deno lint: %q", denoPnpm)
	}
	denoYarn, ok := TryCompactDenoLint([]string{"yarn", "deno", "lint", "."}, []byte(""))
	if !ok || string(denoYarn) != "[deno lint] ok\n" {
		t.Fatalf("yarn deno lint: %q", denoYarn)
	}
	denoNpxY, ok := TryCompactDenoLint([]string{"npx", "-y", "deno", "lint"}, []byte(""))
	if !ok || string(denoNpxY) != "[deno lint] ok\n" {
		t.Fatalf("npx -y deno lint: %q", denoNpxY)
	}
	da, ok := TryCompactDartAnalyze([]string{"dart", "analyze"}, []byte(""))
	if !ok || string(da) != "[dart analyze] ok\n" {
		t.Fatalf("dart analyze: %q", da)
	}
	daFvm, ok := TryCompactDartAnalyze([]string{"fvm", "dart", "analyze"}, []byte("\n"))
	if !ok || string(daFvm) != "[dart analyze] ok\n" {
		t.Fatalf("fvm dart analyze: %q", daFvm)
	}
	daNpx, ok := TryCompactDartAnalyze([]string{"npx", "dart", "analyze"}, []byte(""))
	if !ok || string(daNpx) != "[dart analyze] ok\n" {
		t.Fatalf("npx dart analyze: %q", daNpx)
	}
	daPnpmFvm, ok := TryCompactDartAnalyze([]string{"pnpm", "exec", "fvm", "dart", "analyze"}, []byte("\n"))
	if !ok || string(daPnpmFvm) != "[dart analyze] ok\n" {
		t.Fatalf("pnpm fvm dart analyze: %q", daPnpmFvm)
	}
	if _, ok := TryCompactDartAnalyze([]string{"dart", "test"}, []byte("")); ok {
		t.Fatal("dart test not analyze")
	}
	fa, ok := TryCompactFlutterAnalyze([]string{"flutter", "analyze"}, []byte(""))
	if !ok || string(fa) != "[flutter analyze] ok\n" {
		t.Fatalf("flutter analyze: %q", fa)
	}
	faFvm, ok := TryCompactFlutterAnalyze([]string{"fvm", "flutter", "analyze"}, []byte("\n"))
	if !ok || string(faFvm) != "[flutter analyze] ok\n" {
		t.Fatalf("fvm flutter analyze: %q", faFvm)
	}
	faYarn, ok := TryCompactFlutterAnalyze([]string{"yarn", "flutter", "analyze"}, []byte(""))
	if !ok || string(faYarn) != "[flutter analyze] ok\n" {
		t.Fatalf("yarn flutter analyze: %q", faYarn)
	}
	daYarnFvm, ok := TryCompactDartAnalyze([]string{"yarn", "fvm", "dart", "analyze"}, []byte("\n"))
	if !ok || string(daYarnFvm) != "[dart analyze] ok\n" {
		t.Fatalf("yarn fvm dart analyze: %q", daYarnFvm)
	}
	sw, ok := TryCompactSwiftlint([]string{"swiftlint", "lint", "--strict"}, []byte("\n"))
	if !ok || string(sw) != "[swiftlint] ok\n" {
		t.Fatalf("swiftlint: %q", sw)
	}
	swNpx, ok := TryCompactSwiftlint([]string{"npx", "swiftlint", "lint"}, []byte(""))
	if !ok || string(swNpx) != "[swiftlint] ok\n" {
		t.Fatalf("npx swiftlint: %q", swNpx)
	}
	kt, ok := TryCompactKtlint([]string{"ktlint", "src/"}, []byte(""))
	if !ok || string(kt) != "[ktlint] ok\n" {
		t.Fatalf("ktlint: %q", kt)
	}
	ktYarn, ok := TryCompactKtlint([]string{"yarn", "ktlint", "."}, []byte("\n"))
	if !ok || string(ktYarn) != "[ktlint] ok\n" {
		t.Fatalf("yarn ktlint: %q", ktYarn)
	}
	dk, ok := TryCompactDetekt([]string{"detekt", "--input", "src"}, []byte(""))
	if !ok || string(dk) != "[detekt] ok\n" {
		t.Fatalf("detekt: %q", dk)
	}
	dkPnpm, ok := TryCompactDetekt([]string{"pnpm", "exec", "detekt", "-i", "src"}, []byte(""))
	if !ok || string(dkPnpm) != "[detekt] ok\n" {
		t.Fatalf("pnpm detekt: %q", dkPnpm)
	}
	if _, ok := TryCompactBufLint([]string{"buf", "format", "-w"}, []byte("")); ok {
		t.Fatal("buf format not lint")
	}
	if _, ok := TryCompactGocritic([]string{"gocritic", "version"}, []byte("")); ok {
		t.Fatal("gocritic without check")
	}
	if _, ok := TryCompactRuffCheck([]string{"ruff", "format", "."}, []byte("")); ok {
		t.Fatal("ruff format should not match check")
	}
	out5, ok := TryCompactLintOutput([]string{"cargo", "clippy"}, []byte(""))
	if !ok || string(out5) != "[cargo clippy] ok\n" {
		t.Fatalf("chain: %q", out5)
	}
	rb, ok := TryCompactRubocop([]string{"rubocop"}, []byte(""))
	if !ok || string(rb) != "[rubocop] ok\n" {
		t.Fatalf("rubocop: %q", rb)
	}
	rbNpx, ok := TryCompactRubocop([]string{"npx", "rubocop", "."}, []byte("\n"))
	if !ok || string(rbNpx) != "[rubocop] ok\n" {
		t.Fatalf("npx rubocop: %q", rbNpx)
	}
	es, ok := TryCompactEslint([]string{"eslint", "src/"}, []byte(""))
	if !ok || string(es) != "[eslint] ok\n" {
		t.Fatalf("eslint: %q", es)
	}
	es2, ok := TryCompactEslint([]string{"npx", "eslint", "."}, []byte(""))
	if !ok || string(es2) != "[eslint] ok\n" {
		t.Fatalf("npx eslint: %q", es2)
	}
	esPnpm, ok := TryCompactEslint([]string{"pnpm", "exec", "eslint", "src/"}, []byte(""))
	if !ok || string(esPnpm) != "[eslint] ok\n" {
		t.Fatalf("pnpm exec eslint: %q", esPnpm)
	}
	esYarn, ok := TryCompactEslint([]string{"yarn", "eslint", "."}, []byte("\n"))
	if !ok || string(esYarn) != "[eslint] ok\n" {
		t.Fatalf("yarn eslint: %q", esYarn)
	}
	stl, ok := TryCompactStylelint([]string{"stylelint", "**/*.css"}, []byte(""))
	if !ok || string(stl) != "[stylelint] ok\n" {
		t.Fatalf("stylelint: %q", stl)
	}
	stlNpx, ok := TryCompactStylelint([]string{"npx", "stylelint", "*.css"}, []byte(""))
	if !ok || string(stlNpx) != "[stylelint] ok\n" {
		t.Fatalf("npx stylelint: %q", stlNpx)
	}
	stlPnpm, ok := TryCompactStylelint([]string{"pnpm", "exec", "stylelint", "a.css"}, []byte(""))
	if !ok || string(stlPnpm) != "[stylelint] ok\n" {
		t.Fatalf("pnpm stylelint: %q", stlPnpm)
	}
	my, ok := TryCompactMypy([]string{"mypy", "."}, []byte(""))
	if !ok || string(my) != "[mypy] ok\n" {
		t.Fatalf("mypy: %q", my)
	}
	my2, ok := TryCompactMypy([]string{"python3", "-m", "mypy", "src"}, []byte("\n"))
	if !ok || string(my2) != "[mypy] ok\n" {
		t.Fatalf("python -m mypy: %q", my2)
	}
	myNpx, ok := TryCompactMypy([]string{"npx", "mypy", "."}, []byte(""))
	if !ok || string(myNpx) != "[mypy] ok\n" {
		t.Fatalf("npx mypy: %q", myNpx)
	}
	myPnpmPy, ok := TryCompactMypy([]string{"pnpm", "exec", "python3", "-m", "mypy", "src"}, []byte("\n"))
	if !ok || string(myPnpmPy) != "[mypy] ok\n" {
		t.Fatalf("pnpm python -m mypy: %q", myPnpmPy)
	}
}

// TestTryCompactLintOutput_missingWrappers covers pnpm exec / yarn / npx and
// guard branches not exercised in the primary test function.
func TestTryCompactLintOutput_missingWrappers(t *testing.T) {
	t.Parallel()

	// --- golangci-lint: pnpm exec + yarn ---
	glPnpm, ok := TryCompactGolangciLint([]string{"pnpm", "exec", "golangci-lint", "run"}, []byte(""))
	if !ok || string(glPnpm) != "[golangci-lint] ok\n" {
		t.Fatalf("pnpm golangci-lint: %q", glPnpm)
	}
	glYarn, ok := TryCompactGolangciLint([]string{"yarn", "golangci-lint", "run"}, []byte(""))
	if !ok || string(glYarn) != "[golangci-lint] ok\n" {
		t.Fatalf("yarn golangci-lint: %q", glYarn)
	}
	if _, ok := TryCompactGolangciLint([]string{}, []byte("")); ok {
		t.Fatal("golangci-lint empty argv")
	}

	// --- staticcheck: npx + yarn ---
	stNpx2, ok := TryCompactStaticcheck([]string{"npx", "staticcheck", "./..."}, []byte(""))
	if !ok || string(stNpx2) != "[staticcheck] ok\n" {
		t.Fatalf("npx staticcheck: %q", stNpx2)
	}
	stYarn, ok := TryCompactStaticcheck([]string{"yarn", "staticcheck", "./..."}, []byte(""))
	if !ok || string(stYarn) != "[staticcheck] ok\n" {
		t.Fatalf("yarn staticcheck: %q", stYarn)
	}
	if _, ok := TryCompactStaticcheck([]string{}, []byte("")); ok {
		t.Fatal("staticcheck empty argv")
	}

	// --- gocritic: pnpm exec + yarn ---
	gcPnpm, ok := TryCompactGocritic([]string{"pnpm", "exec", "gocritic", "check", "./..."}, []byte(""))
	if !ok || string(gcPnpm) != "[gocritic] ok\n" {
		t.Fatalf("pnpm gocritic: %q", gcPnpm)
	}
	gcYarn, ok := TryCompactGocritic([]string{"yarn", "gocritic", "check", "."}, []byte(""))
	if !ok || string(gcYarn) != "[gocritic] ok\n" {
		t.Fatalf("yarn gocritic: %q", gcYarn)
	}

	// --- gosec: npx + pnpm ---
	gsNpx, ok := TryCompactGosec([]string{"npx", "gosec", "./..."}, []byte(""))
	if !ok || string(gsNpx) != "[gosec] ok\n" {
		t.Fatalf("npx gosec: %q", gsNpx)
	}
	gsPnpm, ok := TryCompactGosec([]string{"pnpm", "exec", "gosec", "./..."}, []byte(""))
	if !ok || string(gsPnpm) != "[gosec] ok\n" {
		t.Fatalf("pnpm gosec: %q", gsPnpm)
	}
	if _, ok := TryCompactGosec([]string{}, []byte("")); ok {
		t.Fatal("gosec empty argv")
	}

	// --- protolint: pnpm + yarn ---
	plPnpm, ok := TryCompactProtolint([]string{"pnpm", "exec", "protolint", "api/"}, []byte(""))
	if !ok || string(plPnpm) != "[protolint] ok\n" {
		t.Fatalf("pnpm protolint: %q", plPnpm)
	}
	plYarn, ok := TryCompactProtolint([]string{"yarn", "protolint", "api/"}, []byte(""))
	if !ok || string(plYarn) != "[protolint] ok\n" {
		t.Fatalf("yarn protolint: %q", plYarn)
	}
	if _, ok := TryCompactProtolint([]string{}, []byte("")); ok {
		t.Fatal("protolint empty argv")
	}

	// --- zizmor: pnpm + yarn ---
	zzPnpm, ok := TryCompactZizmor([]string{"pnpm", "exec", "zizmor", ".github/"}, []byte(""))
	if !ok || string(zzPnpm) != "[zizmor] ok\n" {
		t.Fatalf("pnpm zizmor: %q", zzPnpm)
	}
	zzYarn, ok := TryCompactZizmor([]string{"yarn", "zizmor", ".github/"}, []byte(""))
	if !ok || string(zzYarn) != "[zizmor] ok\n" {
		t.Fatalf("yarn zizmor: %q", zzYarn)
	}
	if _, ok := TryCompactZizmor([]string{}, []byte("")); ok {
		t.Fatal("zizmor empty argv")
	}

	// --- kube-linter: pnpm + yarn ---
	klPnpm, ok := TryCompactKubeLinter([]string{"pnpm", "exec", "kube-linter", "lint", "chart/"}, []byte(""))
	if !ok || string(klPnpm) != "[kube-linter] ok\n" {
		t.Fatalf("pnpm kube-linter: %q", klPnpm)
	}
	klYarn, ok := TryCompactKubeLinter([]string{"yarn", "kube-linter", "chart/"}, []byte(""))
	if !ok || string(klYarn) != "[kube-linter] ok\n" {
		t.Fatalf("yarn kube-linter: %q", klYarn)
	}
	if _, ok := TryCompactKubeLinter([]string{}, []byte("")); ok {
		t.Fatal("kube-linter empty argv")
	}

	// --- pyright: yarn ---
	pyrYarn, ok := TryCompactPyright([]string{"yarn", "basedpyright", "src/"}, []byte(""))
	if !ok || string(pyrYarn) != "[pyright] ok\n" {
		t.Fatalf("yarn basedpyright: %q", pyrYarn)
	}
	if _, ok := TryCompactPyright([]string{}, []byte("")); ok {
		t.Fatal("pyright empty argv")
	}

	// --- ansible-lint: pnpm + yarn ---
	ansNpx, ok := TryCompactAnsibleLint([]string{"npx", "ansible-lint", "playbooks/"}, []byte(""))
	if !ok || string(ansNpx) != "[ansible-lint] ok\n" {
		t.Fatalf("npx ansible-lint: %q", ansNpx)
	}
	ansPnpm, ok := TryCompactAnsibleLint([]string{"pnpm", "exec", "ansible-lint", "."}, []byte(""))
	if !ok || string(ansPnpm) != "[ansible-lint] ok\n" {
		t.Fatalf("pnpm ansible-lint: %q", ansPnpm)
	}
	ansYarn, ok := TryCompactAnsibleLint([]string{"yarn", "ansible-lint", "playbooks/"}, []byte(""))
	if !ok || string(ansYarn) != "[ansible-lint] ok\n" {
		t.Fatalf("yarn ansible-lint: %q", ansYarn)
	}
	if _, ok := TryCompactAnsibleLint([]string{}, []byte("")); ok {
		t.Fatal("ansible-lint empty argv")
	}

	// --- tflint: npx + yarn ---
	tfNpx2, ok := TryCompactTflint([]string{"npx", "tflint", "--chdir=infra"}, []byte(""))
	if !ok || string(tfNpx2) != "[tflint] ok\n" {
		t.Fatalf("npx tflint: %q", tfNpx2)
	}
	tfYarn, ok := TryCompactTflint([]string{"yarn", "tflint", "."}, []byte(""))
	if !ok || string(tfYarn) != "[tflint] ok\n" {
		t.Fatalf("yarn tflint: %q", tfYarn)
	}
	if _, ok := TryCompactTflint([]string{}, []byte("")); ok {
		t.Fatal("tflint empty argv")
	}

	// --- pint: pnpm + yarn ---
	pntPnpm, ok := TryCompactPint([]string{"pnpm", "exec", "pint", "--test"}, []byte(""))
	if !ok || string(pntPnpm) != "[pint] ok\n" {
		t.Fatalf("pnpm pint: %q", pntPnpm)
	}
	pntYarn, ok := TryCompactPint([]string{"yarn", "pint"}, []byte(""))
	if !ok || string(pntYarn) != "[pint] ok\n" {
		t.Fatalf("yarn pint: %q", pntYarn)
	}
	if _, ok := TryCompactPint([]string{}, []byte("")); ok {
		t.Fatal("pint empty argv")
	}

	// --- phpcs: npx + yarn ---
	phNpx, ok := TryCompactPhpcs([]string{"npx", "phpcs", "src/"}, []byte(""))
	if !ok || string(phNpx) != "[phpcs] ok\n" {
		t.Fatalf("npx phpcs: %q", phNpx)
	}
	phYarn, ok := TryCompactPhpcs([]string{"yarn", "phpcs", "src/"}, []byte(""))
	if !ok || string(phYarn) != "[phpcs] ok\n" {
		t.Fatalf("yarn phpcs: %q", phYarn)
	}
	if _, ok := TryCompactPhpcs([]string{}, []byte("")); ok {
		t.Fatal("phpcs empty argv")
	}

	// --- cfn-lint: npx + pnpm ---
	cfnNpx, ok := TryCompactCfnLint([]string{"npx", "cfn-lint", "template.yaml"}, []byte(""))
	if !ok || string(cfnNpx) != "[cfn-lint] ok\n" {
		t.Fatalf("npx cfn-lint: %q", cfnNpx)
	}
	cfnPnpm, ok := TryCompactCfnLint([]string{"pnpm", "exec", "cfn-lint", "t.yaml"}, []byte(""))
	if !ok || string(cfnPnpm) != "[cfn-lint] ok\n" {
		t.Fatalf("pnpm cfn-lint: %q", cfnPnpm)
	}
	if _, ok := TryCompactCfnLint([]string{}, []byte("")); ok {
		t.Fatal("cfn-lint empty argv")
	}

	// --- dotenv-linter: npx + yarn ---
	dvlNpx, ok := TryCompactDotenvLinter([]string{"npx", "dotenv-linter", ".env"}, []byte(""))
	if !ok || string(dvlNpx) != "[dotenv-linter] ok\n" {
		t.Fatalf("npx dotenv-linter: %q", dvlNpx)
	}
	dvlYarn, ok := TryCompactDotenvLinter([]string{"yarn", "dotenv-linter", ".env"}, []byte(""))
	if !ok || string(dvlYarn) != "[dotenv-linter] ok\n" {
		t.Fatalf("yarn dotenv-linter: %q", dvlYarn)
	}
	if _, ok := TryCompactDotenvLinter([]string{}, []byte("")); ok {
		t.Fatal("dotenv-linter empty argv")
	}

	// --- phpstan: pnpm + yarn ---
	pstPnpm, ok := TryCompactPhpstan([]string{"pnpm", "exec", "phpstan", "analyse"}, []byte(""))
	if !ok || string(pstPnpm) != "[phpstan] ok\n" {
		t.Fatalf("pnpm phpstan: %q", pstPnpm)
	}
	pstYarn, ok := TryCompactPhpstan([]string{"yarn", "phpstan", "analyse"}, []byte(""))
	if !ok || string(pstYarn) != "[phpstan] ok\n" {
		t.Fatalf("yarn phpstan: %q", pstYarn)
	}
	if _, ok := TryCompactPhpstan([]string{}, []byte("")); ok {
		t.Fatal("phpstan empty argv")
	}

	// --- psalm: npx + pnpm ---
	psmNpx, ok := TryCompactPsalm([]string{"npx", "psalm"}, []byte(""))
	if !ok || string(psmNpx) != "[psalm] ok\n" {
		t.Fatalf("npx psalm: %q", psmNpx)
	}
	psmPnpm, ok := TryCompactPsalm([]string{"pnpm", "exec", "psalm"}, []byte(""))
	if !ok || string(psmPnpm) != "[psalm] ok\n" {
		t.Fatalf("pnpm psalm: %q", psmPnpm)
	}
	if _, ok := TryCompactPsalm([]string{}, []byte("")); ok {
		t.Fatal("psalm empty argv")
	}

	// --- phan: pnpm + yarn ---
	phnPnpm, ok := TryCompactPhan([]string{"pnpm", "exec", "phan"}, []byte(""))
	if !ok || string(phnPnpm) != "[phan] ok\n" {
		t.Fatalf("pnpm phan: %q", phnPnpm)
	}
	phnYarn, ok := TryCompactPhan([]string{"yarn", "phan"}, []byte(""))
	if !ok || string(phnYarn) != "[phan] ok\n" {
		t.Fatalf("yarn phan: %q", phnYarn)
	}
	if _, ok := TryCompactPhan([]string{}, []byte("")); ok {
		t.Fatal("phan empty argv")
	}

	// --- jscpd: pnpm + yarn ---
	jscPnpm, ok := TryCompactJscpd([]string{"pnpm", "exec", "jscpd", "src/"}, []byte(""))
	if !ok || string(jscPnpm) != "[jscpd] ok\n" {
		t.Fatalf("pnpm jscpd: %q", jscPnpm)
	}
	jscYarn, ok := TryCompactJscpd([]string{"yarn", "jscpd", "src/"}, []byte(""))
	if !ok || string(jscYarn) != "[jscpd] ok\n" {
		t.Fatalf("yarn jscpd: %q", jscYarn)
	}
	if _, ok := TryCompactJscpd([]string{}, []byte("")); ok {
		t.Fatal("jscpd empty argv")
	}

	// --- gofumpt: pnpm + yarn ---
	gfPnpm, ok := TryCompactGofumpt([]string{"pnpm", "exec", "gofumpt", "."}, []byte(""))
	if !ok || string(gfPnpm) != "[gofumpt] ok\n" {
		t.Fatalf("pnpm gofumpt: %q", gfPnpm)
	}
	gfYarn, ok := TryCompactGofumpt([]string{"yarn", "gofumpt", "."}, []byte(""))
	if !ok || string(gfYarn) != "[gofumpt] ok\n" {
		t.Fatalf("yarn gofumpt: %q", gfYarn)
	}
	if _, ok := TryCompactGofumpt([]string{}, []byte("")); ok {
		t.Fatal("gofumpt empty argv")
	}

	// --- revive: npx + yarn ---
	rvNpx, ok := TryCompactRevive([]string{"npx", "revive", "./..."}, []byte(""))
	if !ok || string(rvNpx) != "[revive] ok\n" {
		t.Fatalf("npx revive: %q", rvNpx)
	}
	rvYarn, ok := TryCompactRevive([]string{"yarn", "revive", "./..."}, []byte(""))
	if !ok || string(rvYarn) != "[revive] ok\n" {
		t.Fatalf("yarn revive: %q", rvYarn)
	}
	if _, ok := TryCompactRevive([]string{}, []byte("")); ok {
		t.Fatal("revive empty argv")
	}

	// --- shellcheck: pnpm + yarn ---
	shPnpm, ok := TryCompactShellcheck([]string{"pnpm", "exec", "shellcheck", "a.sh"}, []byte(""))
	if !ok || string(shPnpm) != "[shellcheck] ok\n" {
		t.Fatalf("pnpm shellcheck: %q", shPnpm)
	}
	shYarn, ok := TryCompactShellcheck([]string{"yarn", "shellcheck", "a.sh"}, []byte(""))
	if !ok || string(shYarn) != "[shellcheck] ok\n" {
		t.Fatalf("yarn shellcheck: %q", shYarn)
	}
	if _, ok := TryCompactShellcheck([]string{}, []byte("")); ok {
		t.Fatal("shellcheck empty argv")
	}

	// --- hadolint: pnpm + yarn ---
	hadPnpm, ok := TryCompactHadolint([]string{"pnpm", "exec", "hadolint", "Dockerfile"}, []byte(""))
	if !ok || string(hadPnpm) != "[hadolint] ok\n" {
		t.Fatalf("pnpm hadolint: %q", hadPnpm)
	}
	hadYarn, ok := TryCompactHadolint([]string{"yarn", "hadolint", "Dockerfile"}, []byte(""))
	if !ok || string(hadYarn) != "[hadolint] ok\n" {
		t.Fatalf("yarn hadolint: %q", hadYarn)
	}
	if _, ok := TryCompactHadolint([]string{}, []byte("")); ok {
		t.Fatal("hadolint empty argv")
	}

	// --- markdownlint: npx + yarn ---
	mdlNpx, ok := TryCompactMarkdownlint([]string{"npx", "markdownlint", "docs/"}, []byte(""))
	if !ok || string(mdlNpx) != "[markdownlint] ok\n" {
		t.Fatalf("npx markdownlint: %q", mdlNpx)
	}
	mdlYarn, ok := TryCompactMarkdownlint([]string{"yarn", "markdownlint", "docs/"}, []byte(""))
	if !ok || string(mdlYarn) != "[markdownlint] ok\n" {
		t.Fatalf("yarn markdownlint: %q", mdlYarn)
	}
	if _, ok := TryCompactMarkdownlint([]string{}, []byte("")); ok {
		t.Fatal("markdownlint empty argv")
	}

	// --- actionlint: npx + yarn ---
	alNpx, ok := TryCompactActionlint([]string{"npx", "actionlint"}, []byte(""))
	if !ok || string(alNpx) != "[actionlint] ok\n" {
		t.Fatalf("npx actionlint: %q", alNpx)
	}
	alYarn, ok := TryCompactActionlint([]string{"yarn", "actionlint"}, []byte(""))
	if !ok || string(alYarn) != "[actionlint] ok\n" {
		t.Fatalf("yarn actionlint: %q", alYarn)
	}
	if _, ok := TryCompactActionlint([]string{}, []byte("")); ok {
		t.Fatal("actionlint empty argv")
	}

	// --- vale: pnpm + yarn ---
	valePnpm, ok := TryCompactVale([]string{"pnpm", "exec", "vale", "docs/"}, []byte(""))
	if !ok || string(valePnpm) != "[vale] ok\n" {
		t.Fatalf("pnpm vale: %q", valePnpm)
	}
	valeYarn, ok := TryCompactVale([]string{"yarn", "vale", "docs/"}, []byte(""))
	if !ok || string(valeYarn) != "[vale] ok\n" {
		t.Fatalf("yarn vale: %q", valeYarn)
	}
	if _, ok := TryCompactVale([]string{}, []byte("")); ok {
		t.Fatal("vale empty argv")
	}

	// --- eslint: len < 1 ---
	if _, ok := TryCompactEslint([]string{}, []byte("")); ok {
		t.Fatal("eslint empty argv")
	}

	// --- stylelint: yarn ---
	stlYarn, ok := TryCompactStylelint([]string{"yarn", "stylelint", "**/*.css"}, []byte(""))
	if !ok || string(stlYarn) != "[stylelint] ok\n" {
		t.Fatalf("yarn stylelint: %q", stlYarn)
	}
	if _, ok := TryCompactStylelint([]string{}, []byte("")); ok {
		t.Fatal("stylelint empty argv")
	}

	// --- oxlint: pnpm + yarn ---
	oxPnpm, ok := TryCompactOxlint([]string{"pnpm", "exec", "oxlint", "."}, []byte(""))
	if !ok || string(oxPnpm) != "[oxlint] ok\n" {
		t.Fatalf("pnpm oxlint: %q", oxPnpm)
	}
	oxYarn, ok := TryCompactOxlint([]string{"yarn", "oxlint", "src/"}, []byte(""))
	if !ok || string(oxYarn) != "[oxlint] ok\n" {
		t.Fatalf("yarn oxlint: %q", oxYarn)
	}
	if _, ok := TryCompactOxlint([]string{}, []byte("")); ok {
		t.Fatal("oxlint empty argv")
	}

	// --- rubocop: pnpm + yarn ---
	rbPnpm, ok := TryCompactRubocop([]string{"pnpm", "exec", "rubocop"}, []byte(""))
	if !ok || string(rbPnpm) != "[rubocop] ok\n" {
		t.Fatalf("pnpm rubocop: %q", rbPnpm)
	}
	rbYarn, ok := TryCompactRubocop([]string{"yarn", "rubocop", "."}, []byte(""))
	if !ok || string(rbYarn) != "[rubocop] ok\n" {
		t.Fatalf("yarn rubocop: %q", rbYarn)
	}
	if _, ok := TryCompactRubocop([]string{}, []byte("")); ok {
		t.Fatal("rubocop empty argv")
	}

	// --- ktlint: npx + pnpm ---
	ktNpx, ok := TryCompactKtlint([]string{"npx", "ktlint"}, []byte(""))
	if !ok || string(ktNpx) != "[ktlint] ok\n" {
		t.Fatalf("npx ktlint: %q", ktNpx)
	}
	ktPnpm, ok := TryCompactKtlint([]string{"pnpm", "exec", "ktlint", "."}, []byte(""))
	if !ok || string(ktPnpm) != "[ktlint] ok\n" {
		t.Fatalf("pnpm ktlint: %q", ktPnpm)
	}
	if _, ok := TryCompactKtlint([]string{}, []byte("")); ok {
		t.Fatal("ktlint empty argv")
	}

	// --- swiftlint: pnpm + yarn ---
	swPnpm, ok := TryCompactSwiftlint([]string{"pnpm", "exec", "swiftlint", "lint"}, []byte(""))
	if !ok || string(swPnpm) != "[swiftlint] ok\n" {
		t.Fatalf("pnpm swiftlint: %q", swPnpm)
	}
	swYarn, ok := TryCompactSwiftlint([]string{"yarn", "swiftlint", "lint"}, []byte(""))
	if !ok || string(swYarn) != "[swiftlint] ok\n" {
		t.Fatalf("yarn swiftlint: %q", swYarn)
	}
	if _, ok := TryCompactSwiftlint([]string{}, []byte("")); ok {
		t.Fatal("swiftlint empty argv")
	}

	// --- detekt: npx + yarn ---
	dkNpx, ok := TryCompactDetekt([]string{"npx", "detekt"}, []byte(""))
	if !ok || string(dkNpx) != "[detekt] ok\n" {
		t.Fatalf("npx detekt: %q", dkNpx)
	}
	dkYarn, ok := TryCompactDetekt([]string{"yarn", "detekt", "--input", "src"}, []byte(""))
	if !ok || string(dkYarn) != "[detekt] ok\n" {
		t.Fatalf("yarn detekt: %q", dkYarn)
	}
	if _, ok := TryCompactDetekt([]string{}, []byte("")); ok {
		t.Fatal("detekt empty argv")
	}

	// --- non-empty stdout guards (covers the stdout!="" early-return branches) ---
	if _, ok := TryCompactRuffCheck([]string{"ruff", "check", "."}, []byte("E123 error\n")); ok {
		t.Fatal("ruff check non-empty stdout")
	}
	if _, ok := TryCompactPylint([]string{"pylint", "pkg"}, []byte("E0001 error\n")); ok {
		t.Fatal("pylint non-empty stdout")
	}
	if _, ok := TryCompactFlake8([]string{"flake8", "."}, []byte("E123 error\n")); ok {
		t.Fatal("flake8 non-empty stdout")
	}
	if _, ok := TryCompactBandit([]string{"bandit", "-r", "."}, []byte("Issue found\n")); ok {
		t.Fatal("bandit non-empty stdout")
	}
	if _, ok := TryCompactBiomeCheck([]string{"biome", "check", "."}, []byte("error\n")); ok {
		t.Fatal("biome check non-empty stdout")
	}
	if _, ok := TryCompactMypy([]string{"mypy", "."}, []byte("error\n")); ok {
		t.Fatal("mypy non-empty stdout")
	}

	// --- is*Argv helpers via TryCompact* wrappers ---

	// isCargoClippyArgv: pnpm exec (L23)
	ccPnpm, ok := TryCompactCargoClippy([]string{"pnpm", "exec", "cargo", "clippy"}, []byte(""))
	if !ok || string(ccPnpm) != "[cargo clippy] ok\n" {
		t.Fatalf("pnpm cargo clippy: %q", ccPnpm)
	}
	// isCargoClippyArgv: npx failure - rest has only 1 element (L18)
	if _, ok := TryCompactCargoClippy([]string{"npx", "cargo"}, []byte("")); ok {
		t.Fatal("npx cargo: too short for clippy")
	}

	// isCargoAuditArgv: npx success (L51/56)
	caaNpx, ok := TryCompactCargoAudit([]string{"npx", "cargo", "audit"}, []byte(""))
	if !ok || string(caaNpx) != "[cargo audit] ok\n" {
		t.Fatalf("npx cargo audit: %q", caaNpx)
	}
	// isCargoAuditArgv: npx failure (L53)
	if _, ok := TryCompactCargoAudit([]string{"npx", "cargo"}, []byte("")); ok {
		t.Fatal("npx cargo: too short for audit")
	}

	// isBanditArgv: npx success (L981/986)
	bandNpx, ok := TryCompactBandit([]string{"npx", "bandit", "-r", "."}, []byte(""))
	if !ok || string(bandNpx) != "[bandit] ok\n" {
		t.Fatalf("npx bandit: %q", bandNpx)
	}
	// isBanditArgv: npx failure (L983) - npx with no command after flags
	if _, ok := TryCompactBandit([]string{"npx", "-y"}, []byte("")); ok {
		t.Fatal("npx -y: no bandit command")
	}
	// isBanditArgv: return false (L1006) - non-matching binary
	if _, ok := TryCompactBandit([]string{"curl", "http://x"}, []byte("")); ok {
		t.Fatal("curl not bandit")
	}

	// isFlake8Argv: npx success (L806/811)
	flkNpx, ok := TryCompactFlake8([]string{"npx", "flake8", "."}, []byte(""))
	if !ok || string(flkNpx) != "[flake8] ok\n" {
		t.Fatalf("npx flake8: %q", flkNpx)
	}
	// isFlake8Argv: npx failure (L808)
	if _, ok := TryCompactFlake8([]string{"npx", "-y"}, []byte("")); ok {
		t.Fatal("npx -y: no flake8 command")
	}
	// isFlake8Argv: len < 1 (L802)
	if _, ok := TryCompactFlake8([]string{}, []byte("")); ok {
		t.Fatal("flake8 empty argv")
	}
	// isFlake8Argv: return false (L831) - non-matching binary
	if _, ok := TryCompactFlake8([]string{"curl", "url"}, []byte("")); ok {
		t.Fatal("curl not flake8")
	}

	// isMypyArgv: npx success (L1100 covered via recursive return)
	myNpx2, ok := TryCompactMypy([]string{"npx", "mypy", "."}, []byte(""))
	if !ok || string(myNpx2) != "[mypy] ok\n" {
		t.Fatalf("npx mypy: %q", myNpx2)
	}
	// isMypyArgv: len < 1 (L1094)
	if _, ok := TryCompactMypy([]string{}, []byte("")); ok {
		t.Fatal("mypy empty argv")
	}
	// isMypyArgv: return false (L1123) - non-matching binary
	if _, ok := TryCompactMypy([]string{"curl", "url"}, []byte("")); ok {
		t.Fatal("curl not mypy")
	}
	// isMypyArgv: npx failure (L1100)
	if _, ok := TryCompactMypy([]string{"npx", "-y"}, []byte("")); ok {
		t.Fatal("npx -y: no mypy command")
	}

	// isPylintArgv: npx success (L764 covered via recursive return)
	pylNpx2, ok := TryCompactPylint([]string{"npx", "pylint", "src/"}, []byte(""))
	if !ok || string(pylNpx2) != "[pylint] ok\n" {
		t.Fatalf("npx pylint: %q", pylNpx2)
	}
	// isPylintArgv: len < 1 (L758)
	if _, ok := TryCompactPylint([]string{}, []byte("")); ok {
		t.Fatal("pylint empty argv")
	}
	// isPylintArgv: return false (L787) - non-matching binary
	if _, ok := TryCompactPylint([]string{"curl", "url"}, []byte("")); ok {
		t.Fatal("curl not pylint")
	}
	// isPylintArgv: npx failure (L764)
	if _, ok := TryCompactPylint([]string{"npx", "-y"}, []byte("")); ok {
		t.Fatal("npx -y: no pylint command")
	}
}

// TestTryCompactLintOutput_remainingBranches covers the remaining uncovered branches
// in builtin_lint.go that were missed by earlier test functions.
func TestTryCompactLintOutput_remainingBranches(t *testing.T) {
	t.Parallel()

	// --- tryCompactEmptyStdoutSingleBinary (L634) ---

	// L638: len(argv) < 1
	if _, ok := TryCompactErrcheck([]string{}, []byte("")); ok {
		t.Fatal("errcheck empty argv should return false")
	}
	// L651: yarn branch
	yarnNilaway, ok := TryCompactNilaway([]string{"yarn", "nilaway"}, []byte(""))
	if !ok || string(yarnNilaway) != "[nilaway] ok\n" {
		t.Fatalf("yarn nilaway: ok=%v %q", ok, yarnNilaway)
	}

	// --- isGoVetArgv (L672) ---

	// L682: npx with only 1 token after flags (len(rest) < 2)
	if _, ok := TryCompactGoVet([]string{"npx", "go"}, []byte("")); ok {
		t.Fatal("npx go (no subcommand): should not match go vet")
	}

	// --- isPylintArgv (L757): python without -m pylint reaches L787 ---
	if _, ok := TryCompactPylint([]string{"python3", "setup.py"}, []byte("")); ok {
		t.Fatal("python3 setup.py: no -m pylint, should return false")
	}

	// --- isFlake8Argv (L801): python without -m flake8 reaches L831 ---
	if _, ok := TryCompactFlake8([]string{"python3", "setup.py"}, []byte("")); ok {
		t.Fatal("python3 setup.py: no -m flake8, should return false")
	}

	// --- isMypyArgv (L1093): python without -m mypy reaches L1123 ---
	if _, ok := TryCompactMypy([]string{"python3", "setup.py"}, []byte("")); ok {
		t.Fatal("python3 setup.py: no -m mypy, should return false")
	}

	// --- isBanditArgv (L976) ---

	// L977: len(argv) < 1
	if _, ok := TryCompactBandit([]string{}, []byte("")); ok {
		t.Fatal("bandit empty argv should return false")
	}
	// L1006: python without -m bandit
	if _, ok := TryCompactBandit([]string{"python3", "setup.py"}, []byte("")); ok {
		t.Fatal("python3 setup.py: no -m bandit, should return false")
	}

	// --- isDartAnalyzeArgv (L1222): missing branches ---

	// L1236/1238: npx + fvm dart analyze
	daNpxFvm, ok := TryCompactDartAnalyze([]string{"npx", "fvm", "dart", "analyze"}, []byte(""))
	if !ok || string(daNpxFvm) != "[dart analyze] ok\n" {
		t.Fatalf("npx fvm dart analyze: ok=%v %q", ok, daNpxFvm)
	}
	// L1242: pnpm exec dart analyze (direct, not fvm)
	daPnpm, ok := TryCompactDartAnalyze([]string{"pnpm", "exec", "dart", "analyze"}, []byte(""))
	if !ok || string(daPnpm) != "[dart analyze] ok\n" {
		t.Fatalf("pnpm exec dart analyze: ok=%v %q", ok, daPnpm)
	}
	// L1245: yarn dart analyze (direct, not fvm)
	daYarn, ok := TryCompactDartAnalyze([]string{"yarn", "dart", "analyze"}, []byte(""))
	if !ok || string(daYarn) != "[dart analyze] ok\n" {
		t.Fatalf("yarn dart analyze: ok=%v %q", ok, daYarn)
	}

	// --- isFlutterAnalyzeArgv (L1274): missing branches ---

	// L1285: npx flutter analyze (npxMatches hit)
	faNpx, ok := TryCompactFlutterAnalyze([]string{"npx", "flutter", "analyze"}, []byte(""))
	if !ok || string(faNpx) != "[flutter analyze] ok\n" {
		t.Fatalf("npx flutter analyze: ok=%v %q", ok, faNpx)
	}
	// L1288/1290: npx + fvm flutter analyze
	faNpxFvm, ok := TryCompactFlutterAnalyze([]string{"npx", "fvm", "flutter", "analyze"}, []byte(""))
	if !ok || string(faNpxFvm) != "[flutter analyze] ok\n" {
		t.Fatalf("npx fvm flutter analyze: ok=%v %q", ok, faNpxFvm)
	}
	// L1294: pnpm exec flutter analyze (direct)
	faPnpm, ok := TryCompactFlutterAnalyze([]string{"pnpm", "exec", "flutter", "analyze"}, []byte(""))
	if !ok || string(faPnpm) != "[flutter analyze] ok\n" {
		t.Fatalf("pnpm exec flutter analyze: ok=%v %q", ok, faPnpm)
	}
	// L1300/1302: pnpm exec fvm flutter analyze
	faPnpmFvm, ok := TryCompactFlutterAnalyze([]string{"pnpm", "exec", "fvm", "flutter", "analyze"}, []byte(""))
	if !ok || string(faPnpmFvm) != "[flutter analyze] ok\n" {
		t.Fatalf("pnpm exec fvm flutter analyze: ok=%v %q", ok, faPnpmFvm)
	}
	// L1306/1308: yarn fvm flutter analyze
	faYarnFvm, ok := TryCompactFlutterAnalyze([]string{"yarn", "fvm", "flutter", "analyze"}, []byte(""))
	if !ok || string(faYarnFvm) != "[flutter analyze] ok\n" {
		t.Fatalf("yarn fvm flutter analyze: ok=%v %q", ok, faYarnFvm)
	}

	// BiomeCheck len(argv) < 2 branch (line 1017-1019):
	// argv has 1 element containing "check" — passes argvContainsToken but fails the len check.
	if _, ok := TryCompactBiomeCheck([]string{"check"}, []byte("")); ok {
		t.Fatal("single-element argv should return false")
	}
}

func TestTryCompactMypy_errors(t *testing.T) {
	t.Parallel()
	// Realistic mypy output with loading/progress noise before errors.
	input := `Skipping analyzing 'requests': module is installed, but missing library stubs
No stubs found for module 'boto3'.  (Stub-only packages are from typeshed)
src/main.py:10: error: Argument 1 to "func" has incompatible type "str"; expected "int"
src/config.py:25: error: Item "None" of "Optional[str]" has no attribute "split"
src/main.py:10: note: See https://mypy.readthedocs.io/en/stable/running_mypy.html
Found 2 errors in 2 files (checked 5 source files)
`
	out, ok := TryCompactMypy([]string{"mypy", "src/"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact mypy error output, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "Found 2 errors") {
		t.Errorf("want summary line, got: %q", s)
	}
	if !strings.Contains(s, "error:") {
		t.Errorf("want error lines, got: %q", s)
	}
	// Should be shorter because "Skipping analyzing" and "No stubs found" lines are dropped.
	if len(s) >= len(input) {
		t.Errorf("compact should be shorter: %d vs %d", len(s), len(input))
	}
}

func TestTryCompactMypyDiagnosticsStrict(t *testing.T) {
	t.Parallel()

	var input strings.Builder
	for i := 0; i < 80; i++ {
		input.WriteString("src/app.py:10: error: Incompatible return value type\n")
	}
	input.WriteString("src/app.py:10: note: expected str\n")
	input.WriteString("Found 80 errors in 1 file (checked 48 source files)\n")

	out, ok := TryCompactMypyDiagnostics([]string{"mypy", "src"}, []byte(input.String()))
	if !ok {
		t.Fatal("expected strict mypy diagnostics to compact")
	}
	for _, want := range []string{
		"[mypy] FAILED (81 diagnostics)",
		"src/app.py:10: error: Incompatible return value type (repeated 80 times)",
		"src/app.py:10: note: expected str",
		"Found 80 errors in 1 file",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("strict mypy diagnostics missing %q in %q", want, out)
		}
	}

	if _, ok := TryCompactMypyDiagnostics([]string{"mypy", "src"}, []byte("Skipping analyzing 'requests': module is installed, but missing library stubs\nsrc/app.py:10: error: bad\nFound 1 error in 1 file\n")); ok {
		t.Fatal("strict mypy diagnostics must fail open on stub notices")
	}
	if _, ok := TryCompactMypyDiagnostics([]string{"mypy", "src"}, []byte("src/app.py:10: error: bad\nif value:\nFound 1 error in 1 file\n")); ok {
		t.Fatal("strict mypy diagnostics must fail open on source context")
	}
	if _, ok := TryCompactMypyDiagnostics([]string{"mypy", "src"}, []byte("src/app.py:10: error: bad\nFound 2 errors in 1 file\n")); ok {
		t.Fatal("strict mypy diagnostics must fail open on mismatched summary count")
	}
	if _, ok := TryCompactMypyDiagnostics([]string{"python", "script.py"}, []byte(input.String())); ok {
		t.Fatal("non-mypy command must fail open")
	}
}

func TestTryCompactMypy_success(t *testing.T) {
	t.Parallel()
	// mypy success with non-empty output (Success: line)
	input := "Success: no issues found in 5 source files\n"
	out, ok := TryCompactMypy([]string{"mypy", "src/"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact success, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "[mypy] ok") {
		t.Errorf("want [mypy] ok prefix, got: %q", s)
	}
}

func TestTryCompactCargoClippyCleanOutput(t *testing.T) {
	t.Parallel()

	input := cargoClippyCleanFixture(40)
	out, ok := TryCompactCargoClippy([]string{"cargo", "clippy", "--all-targets", "--all-features"}, []byte(input))
	if !ok || string(out) != "[cargo clippy] ok\n" {
		t.Fatalf("cargo clippy clean: ok=%v out=%q", ok, out)
	}
	chainOut, ok := TryCompactLintOutput([]string{"pnpm", "exec", "cargo", "clippy", "--workspace"}, []byte(input))
	if !ok || string(chainOut) != "[cargo clippy] ok\n" {
		t.Fatalf("cargo clippy lint chain: ok=%v out=%q", ok, chainOut)
	}
	if len(out) >= len(input) {
		t.Fatalf("cargo clippy clean summary should be shorter: %d >= %d", len(out), len(input))
	}
}

func TestTryCompactCargoClippyCleanOutputGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "warning", input: cargoClippyCleanFixture(4) + "warning: generated binding is deprecated\n"},
		{name: "error", input: "    Checking slimtest v0.1.0 (/repo/slimtest)\nerror: unnecessary clone\n"},
		{name: "note", input: cargoClippyCleanFixture(4) + "note: run with `RUST_BACKTRACE=1`\n"},
		{name: "help", input: cargoClippyCleanFixture(4) + "help: remove this binding\n"},
		{name: "unknown progress", input: "    Updating crates.io index\n    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.12s\n"},
		{name: "missing finished", input: "    Checking slimtest v0.1.0 (/repo/slimtest)\n"},
		{name: "finished only", input: "    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.12s\n"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := TryCompactCargoClippy([]string{"cargo", "clippy"}, []byte(tt.input)); ok {
				t.Fatalf("unsafe cargo clippy output compacted: %q", tt.input)
			}
		})
	}
	if _, ok := TryCompactCargoClippy([]string{"cargo", "check"}, []byte(cargoClippyCleanFixture(4))); ok {
		t.Fatal("cargo check must not use cargo clippy parser")
	}
}

func cargoClippyCleanFixture(packages int) string {
	var out strings.Builder
	for i := 0; i < packages; i++ {
		fmt.Fprintf(&out, "    Checking slimtest_%03d v0.1.0 (/repo/crates/slimtest_%03d)\n", i, i)
	}
	out.WriteString("    Finished `dev` profile [unoptimized + debuginfo] target(s) in 1.23s\n")
	return out.String()
}

func TestTryCompactPyrightCleanOutput(t *testing.T) {
	t.Parallel()

	jsonInput := `{
  "version": "1.1.400",
  "time": "2026-06-18T12:00:00.000Z",
  "generalDiagnostics": [],
  "summary": {
    "filesAnalyzed": 188,
    "errorCount": 0,
    "warningCount": 0,
    "informationCount": 0,
    "timeInSec": 1.23
  }
}
`
	out, ok := TryCompactPyright([]string{"pyright", "--outputjson", "src"}, []byte(jsonInput))
	if !ok || string(out) != "[pyright --outputjson] ok (188 files analyzed)\n" {
		t.Fatalf("pyright json clean: ok=%v out=%q", ok, out)
	}
	chainOut, ok := TryCompactLintOutput([]string{"basedpyright", "--outputjson", "."}, []byte(jsonInput))
	if !ok || string(chainOut) != "[pyright --outputjson] ok (188 files analyzed)\n" {
		t.Fatalf("pyright lint chain json clean: ok=%v out=%q", ok, chainOut)
	}

	textInput := "Found 188 source files\n0 errors, 0 warnings, 0 informations\n"
	textOut, ok := TryCompactPyright([]string{"basedpyright", "."}, []byte(textInput))
	if !ok || string(textOut) != "[pyright] ok (188 files analyzed)\n" {
		t.Fatalf("pyright text clean: ok=%v out=%q", ok, textOut)
	}

	notesOut, ok := TryCompactPyright([]string{"pyright", "."}, []byte("0 errors, 0 warnings, 0 notes\n"))
	if !ok || string(notesOut) != "[pyright] ok\n" {
		t.Fatalf("basedpyright notes clean: ok=%v out=%q", ok, notesOut)
	}
}

func TestTryCompactPyrightCleanOutputGuards(t *testing.T) {
	t.Parallel()

	withDiagnostic := `{"version":"1.1.400","time":"t","generalDiagnostics":[{"message":"bad"}],"summary":{"filesAnalyzed":1,"errorCount":0,"warningCount":0,"informationCount":0,"timeInSec":0.1}}`
	if _, ok := TryCompactPyright([]string{"pyright", "--outputjson"}, []byte(withDiagnostic)); ok {
		t.Fatal("pyright JSON diagnostics must fail open")
	}
	withWarning := `{"version":"1.1.400","time":"t","generalDiagnostics":[],"summary":{"filesAnalyzed":1,"errorCount":0,"warningCount":1,"informationCount":0,"timeInSec":0.1}}`
	if _, ok := TryCompactPyright([]string{"pyright", "--outputjson"}, []byte(withWarning)); ok {
		t.Fatal("pyright JSON warnings must fail open")
	}
	withUnknownField := `{"version":"1.1.400","time":"t","generalDiagnostics":[],"extra":"keep me","summary":{"filesAnalyzed":1,"errorCount":0,"warningCount":0,"informationCount":0,"timeInSec":0.1}}`
	if _, ok := TryCompactPyright([]string{"pyright", "--outputjson"}, []byte(withUnknownField)); ok {
		t.Fatal("pyright JSON unknown fields must fail open")
	}
	withInfo := "0 errors, 0 warnings, 1 information\n"
	if _, ok := TryCompactPyright([]string{"pyright", "."}, []byte(withInfo)); ok {
		t.Fatal("pyright text information diagnostics must fail open")
	}
	withConfigNoise := "No configuration file found.\n0 errors, 0 warnings, 0 informations\n"
	if _, ok := TryCompactPyright([]string{"pyright", "."}, []byte(withConfigNoise)); ok {
		t.Fatal("pyright text config noise must fail open")
	}
}

func TestTryCompactLintOutput_truncatesLargeOutput(t *testing.T) {
	t.Parallel()
	// Build a large lint output that is not parser-proven (>60 non-empty lines)
	// so the legacy non-WSS truncation fallback remains covered.
	var sb strings.Builder
	for i := 1; i <= 80; i++ {
		sb.WriteString("src/handler.go:123:45: error: unused variable 'x' (deadcode)\n")
	}
	input := sb.String()
	out, ok := TryCompactLintOutput([]string{"gocritic", "check", "./..."}, []byte(input))
	if !ok {
		t.Fatalf("expected truncation, got pass-through (input %d bytes)", len(input))
	}
	s := string(out)
	if !strings.Contains(s, "+20 more violation(s)") {
		t.Errorf("want truncation notice, got: %s", s[:min(len(s), 200)])
	}
	if len(s) >= len(input) {
		t.Errorf("truncated output should be shorter: %d vs %d", len(s), len(input))
	}
}

func TestTryCompactLintOutput_shortPassthrough(t *testing.T) {
	t.Parallel()
	// Short lint output (< 60 lines) should pass through unchanged
	input := "src/main.go:10:1: error: unused import\nsrc/main.go:20:1: error: missing return\n"
	_, ok := TryCompactLintOutput([]string{"gocritic", "check", "./..."}, []byte(input))
	if ok {
		t.Fatal("short lint output should not be truncated")
	}
}

// TestExtractMypyErrors_blankLine covers the t=="" continue branch (line 1108-1109):
// an empty line in the middle of mypy output is silently skipped.
func TestExtractMypyErrors_blankLine(t *testing.T) {
	t.Parallel()
	// Blank line between error line and summary; the blank should be skipped.
	input := "src/main.py:10: error: Incompatible type\n\nFound 1 error in 1 file\n"
	out, ok := TryCompactMypy([]string{"mypy", "src/"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact output for error with blank line, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "error:") {
		t.Errorf("want error line in output, got %q", s)
	}
	if !strings.Contains(s, "Found 1 error") {
		t.Errorf("want summary line in output, got %q", s)
	}
}

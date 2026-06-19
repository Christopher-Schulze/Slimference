package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactPackageOutput(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactNpmInstall([]string{"npm", "ci"}, []byte(""))
	if !ok || string(out) != "[npm ci] ok\n" {
		t.Fatalf("npm ci: ok=%v %q", ok, out)
	}
	out2, ok := TryCompactPnpmInstall([]string{"pnpm", "install"}, []byte("\n"))
	if !ok || string(out2) != "[pnpm install] ok\n" {
		t.Fatalf("pnpm: %q", out2)
	}
	pnpmCI, ok := TryCompactPnpmInstall([]string{"pnpm", "ci"}, []byte(""))
	if !ok || string(pnpmCI) != "[pnpm ci] ok\n" {
		t.Fatalf("pnpm ci: %q", pnpmCI)
	}
	out3, ok := TryCompactYarnInstall([]string{"yarn", "install"}, []byte(""))
	if !ok || string(out3) != "[yarn install] ok\n" {
		t.Fatalf("yarn: %q", out3)
	}
	npmUpdate, ok := TryCompactNpmInstall([]string{"npm", "update"}, []byte(""))
	if !ok || string(npmUpdate) != "[npm update] ok\n" {
		t.Fatalf("npm update: %q", npmUpdate)
	}
	pnpmUpdate, ok := TryCompactPnpmInstall([]string{"pnpm", "update"}, []byte(""))
	if !ok || string(pnpmUpdate) != "[pnpm update] ok\n" {
		t.Fatalf("pnpm update: %q", pnpmUpdate)
	}
	yarnUpgrade, ok := TryCompactYarnInstall([]string{"yarn", "upgrade"}, []byte(""))
	if !ok || string(yarnUpgrade) != "[yarn upgrade] ok\n" {
		t.Fatalf("yarn upgrade: %q", yarnUpgrade)
	}
	poet, ok := TryCompactPoetryInstall([]string{"poetry", "install"}, []byte(""))
	if !ok || string(poet) != "[poetry install] ok\n" {
		t.Fatalf("poetry: %q", poet)
	}
	poetNpx, ok := TryCompactPoetryInstall([]string{"npx", "poetry", "install"}, []byte("\n"))
	if !ok || string(poetNpx) != "[poetry install] ok\n" {
		t.Fatalf("npx poetry install: %q", poetNpx)
	}
	penv, ok := TryCompactPipenvInstall([]string{"pipenv", "install"}, []byte(""))
	if !ok || string(penv) != "[pipenv install] ok\n" {
		t.Fatalf("pipenv: %q", penv)
	}
	penvPnpm, ok := TryCompactPipenvInstall([]string{"pnpm", "exec", "pipenv", "install"}, []byte(""))
	if !ok || string(penvPnpm) != "[pipenv install] ok\n" {
		t.Fatalf("pnpm pipenv install: %q", penvPnpm)
	}
	comp, ok := TryCompactComposerInstall([]string{"composer", "install"}, []byte(""))
	if !ok || string(comp) != "[composer install] ok\n" {
		t.Fatalf("composer: %q", comp)
	}
	mix, ok := TryCompactMixDepsGet([]string{"mix", "deps.get"}, []byte(""))
	if !ok || string(mix) != "[mix deps.get] ok\n" {
		t.Fatalf("mix deps.get: %q", mix)
	}
	mixYarn, ok := TryCompactMixDepsGet([]string{"yarn", "mix", "deps.get"}, []byte(""))
	if !ok || string(mixYarn) != "[mix deps.get] ok\n" {
		t.Fatalf("yarn mix deps.get: %q", mixYarn)
	}
	bundleOut, ok := TryCompactBundleInstall([]string{"bundle", "install"}, []byte(""))
	if !ok || string(bundleOut) != "[bundle install] ok\n" {
		t.Fatalf("bundle install: %q", bundleOut)
	}
	gemOut, ok := TryCompactGemInstall([]string{"gem", "install", "rake"}, []byte(""))
	if !ok || string(gemOut) != "[gem install] ok\n" {
		t.Fatalf("gem install: %q", gemOut)
	}
	out4, ok := TryCompactPipInstall([]string{"pip3", "install", "-r", "req.txt"}, []byte(""))
	if !ok || string(out4) != "[pip install] ok\n" {
		t.Fatalf("pip: %q", out4)
	}
	pipNpx, ok := TryCompactPipInstall([]string{"npx", "pip", "install", "x"}, []byte(""))
	if !ok || string(pipNpx) != "[pip install] ok\n" {
		t.Fatalf("npx pip install: %q", pipNpx)
	}
	out5, ok := TryCompactGoMod([]string{"go", "mod", "tidy"}, []byte(""))
	if !ok || string(out5) != "[go mod tidy] ok\n" {
		t.Fatalf("go mod: %q", out5)
	}
	goModNpx, ok := TryCompactGoMod([]string{"npx", "go", "mod", "download"}, []byte("\n"))
	if !ok || string(goModNpx) != "[go mod download] ok\n" {
		t.Fatalf("npx go mod download: %q", goModNpx)
	}
	goModPnpm, ok := TryCompactGoMod([]string{"pnpm", "exec", "go", "mod", "verify"}, []byte(""))
	if !ok || string(goModPnpm) != "[go mod verify] ok\n" {
		t.Fatalf("pnpm go mod verify: %q", goModPnpm)
	}
	bun, ok := TryCompactBunInstall([]string{"bun", "install"}, []byte(""))
	if !ok || string(bun) != "[bun install] ok\n" {
		t.Fatalf("bun: %q", bun)
	}
	bunYarn, ok := TryCompactBunInstall([]string{"yarn", "bun", "install"}, []byte("\n"))
	if !ok || string(bunYarn) != "[bun install] ok\n" {
		t.Fatalf("yarn bun install: %q", bunYarn)
	}
	uvp, ok := TryCompactUvPipInstall([]string{"uv", "pip", "install", "x"}, []byte(""))
	if !ok || string(uvp) != "[uv pip install] ok\n" {
		t.Fatalf("uv pip: %q", uvp)
	}
	uvpNpx, ok := TryCompactUvPipInstall([]string{"npx", "uv", "pip", "install", "x"}, []byte(""))
	if !ok || string(uvpNpx) != "[uv pip install] ok\n" {
		t.Fatalf("npx uv pip install: %q", uvpNpx)
	}
	uvpPnpm, ok := TryCompactUvPipInstall([]string{"pnpm", "exec", "uv", "pip", "install"}, []byte(""))
	if !ok || string(uvpPnpm) != "[uv pip install] ok\n" {
		t.Fatalf("pnpm uv pip install: %q", uvpPnpm)
	}
	uvs, ok := TryCompactUvSync([]string{"uv", "sync"}, []byte("\n"))
	if !ok || string(uvs) != "[uv sync] ok\n" {
		t.Fatalf("uv sync: %q", uvs)
	}
	uvsYarn, ok := TryCompactUvSync([]string{"yarn", "uv", "sync"}, []byte(""))
	if !ok || string(uvsYarn) != "[uv sync] ok\n" {
		t.Fatalf("yarn uv sync: %q", uvsYarn)
	}
	if _, ok := TryCompactNpmInstall([]string{"npm", "run", "build"}, []byte("")); ok {
		t.Fatal("npm run build is not install")
	}
	out6, ok := TryCompactPackageOutput([]string{"go", "mod", "verify"}, []byte(""))
	if !ok || string(out6) != "[go mod verify] ok\n" {
		t.Fatalf("chain: %q", out6)
	}
	cf, ok := TryCompactCargoFetch([]string{"cargo", "fetch"}, []byte(""))
	if !ok || string(cf) != "[cargo fetch] ok\n" {
		t.Fatalf("cargo fetch: %q", cf)
	}
	cfYarn, ok := TryCompactCargoFetch([]string{"yarn", "cargo", "fetch"}, []byte("\n"))
	if !ok || string(cfYarn) != "[cargo fetch] ok\n" {
		t.Fatalf("yarn cargo fetch: %q", cfYarn)
	}
	cu, ok := TryCompactCargoUpdate([]string{"cargo", "update"}, []byte("\n"))
	if !ok || string(cu) != "[cargo update] ok\n" {
		t.Fatalf("cargo update: %q", cu)
	}
	if _, ok := TryCompactCargoUpdate([]string{"cargo", "build"}, []byte("")); ok {
		t.Fatal("cargo build not update")
	}
	cuNpx, ok := TryCompactCargoUpdate([]string{"npx", "-y", "cargo", "update"}, []byte(""))
	if !ok || string(cuNpx) != "[cargo update] ok\n" {
		t.Fatalf("npx cargo update: %q", cuNpx)
	}
	cuChain, ok := TryCompactPackageOutput([]string{"cargo", "update"}, []byte(""))
	if !ok || string(cuChain) != "[cargo update] ok\n" {
		t.Fatalf("chain cargo update: %q", cuChain)
	}
	swR, ok := TryCompactSwiftPackageResolve([]string{"swift", "package", "resolve"}, []byte(""))
	if !ok || string(swR) != "[swift package resolve] ok\n" {
		t.Fatalf("swift package resolve: %q", swR)
	}
	swNpx, ok := TryCompactSwiftPackageResolve([]string{"npx", "-y", "swift", "package", "resolve"}, []byte("\n"))
	if !ok || string(swNpx) != "[swift package resolve] ok\n" {
		t.Fatalf("npx swift package resolve: %q", swNpx)
	}
	swChain, ok := TryCompactPackageOutput([]string{"pnpm", "exec", "swift", "package", "resolve"}, []byte(""))
	if !ok || string(swChain) != "[swift package resolve] ok\n" {
		t.Fatalf("chain swift resolve: %q", swChain)
	}
}

// TestTryCompactPackageOutput_missingBranches covers guard and wrapper branches missed above.
func TestTryCompactPackageOutput_missingBranches(t *testing.T) {
	t.Parallel()

	// --- compactEmptyStdoutWithNpxPnpmYarn: len < 1 (L11) ---
	if _, ok := TryCompactPoetryInstall([]string{}, []byte("")); ok {
		t.Fatal("compactEmptyStdout: empty argv")
	}

	// --- is*Argv: len < 2 (via single-element argv that passes L11) ---
	// Each is*Argv returns false when called with 1-element argv
	for _, tc := range []struct {
		name string
		fn   func([]string, []byte) ([]byte, bool)
		bin  string
	}{
		{"poetry", TryCompactPoetryInstall, "poetry"},
		{"pipenv", TryCompactPipenvInstall, "pipenv"},
		{"composer", TryCompactComposerInstall, "composer"},
		{"mix", TryCompactMixDepsGet, "mix"},
		{"bundle", TryCompactBundleInstall, "bundle"},
		{"gem", TryCompactGemInstall, "gem"},
		{"pip", TryCompactPipInstall, "pip"},
		{"bun", TryCompactBunInstall, "bun"},
	} {
		if _, ok := tc.fn([]string{tc.bin}, []byte("")); ok {
			t.Fatalf("%s: single-element argv should return false", tc.name)
		}
	}

	// --- TryCompactNpmInstall: len < 2 (L127) ---
	if _, ok := TryCompactNpmInstall([]string{"npm"}, []byte("")); ok {
		t.Fatal("npm len<2")
	}
	// --- TryCompactNpmInstall: non-empty stdout (L138) ---
	if _, ok := TryCompactNpmInstall([]string{"npm", "ci"}, []byte("output\n")); ok {
		t.Fatal("npm ci non-empty stdout")
	}

	// --- TryCompactPnpmInstall: len < 2 (L146) ---
	if _, ok := TryCompactPnpmInstall([]string{"pnpm"}, []byte("")); ok {
		t.Fatal("pnpm len<2")
	}
	// --- TryCompactPnpmInstall: non-empty stdout (L157) ---
	if _, ok := TryCompactPnpmInstall([]string{"pnpm", "install"}, []byte("output\n")); ok {
		t.Fatal("pnpm install non-empty stdout")
	}

	// --- TryCompactYarnInstall: len < 2 (L165) ---
	if _, ok := TryCompactYarnInstall([]string{"yarn"}, []byte("")); ok {
		t.Fatal("yarn len<2")
	}
	// --- TryCompactYarnInstall: non-empty stdout (L171) ---
	if _, ok := TryCompactYarnInstall([]string{"yarn", "install"}, []byte("output\n")); ok {
		t.Fatal("yarn install non-empty stdout")
	}

	// --- TryCompactUvPipInstall: yarn uv pip install (L245) ---
	uvpYarn, ok := TryCompactUvPipInstall([]string{"yarn", "uv", "pip", "install", "x"}, []byte(""))
	if !ok || string(uvpYarn) != "[uv pip install] ok\n" {
		t.Fatalf("yarn uv pip install: ok=%v %q", ok, uvpYarn)
	}

	// --- isUvSyncArgv: len < 2 (L254) ---
	if _, ok := TryCompactUvSync([]string{"uv"}, []byte("")); ok {
		t.Fatal("uv len<2 in isUvSyncArgv")
	}
	// --- TryCompactUvSync: npx uv sync (L272) ---
	uvsNpx, ok := TryCompactUvSync([]string{"npx", "uv", "sync"}, []byte(""))
	if !ok || string(uvsNpx) != "[uv sync] ok\n" {
		t.Fatalf("npx uv sync: ok=%v %q", ok, uvsNpx)
	}
	// --- TryCompactUvSync: pnpm exec uv sync (L277) ---
	uvsPnpm, ok := TryCompactUvSync([]string{"pnpm", "exec", "uv", "sync"}, []byte(""))
	if !ok || string(uvsPnpm) != "[uv sync] ok\n" {
		t.Fatalf("pnpm exec uv sync: ok=%v %q", ok, uvsPnpm)
	}

	// --- isGoModCompactArgv: len < 3 (L290) ---
	if _, ok := TryCompactGoMod([]string{"go", "mod"}, []byte("")); ok {
		t.Fatal("go mod (no subcommand): len<3")
	}
	// --- isGoModCompactArgv: unsupported subcommand (L299) ---
	if _, ok := TryCompactGoMod([]string{"go", "mod", "vendor"}, []byte("")); ok {
		t.Fatal("go mod vendor: not supported")
	}
	// --- TryCompactGoMod: yarn go mod (L322/324) ---
	goModYarn, ok := TryCompactGoMod([]string{"yarn", "go", "mod", "tidy"}, []byte(""))
	if !ok || string(goModYarn) != "[go mod tidy] ok\n" {
		t.Fatalf("yarn go mod tidy: ok=%v %q", ok, goModYarn)
	}

	// --- isCargoFetchArgv: len < 2 (L332) ---
	if _, ok := TryCompactCargoFetch([]string{}, []byte("")); ok {
		t.Fatal("cargo fetch: empty argv")
	}
	// --- isCargoFetchArgv: npx check (L339) and npx failure len<2 (L341) ---
	if _, ok := TryCompactCargoFetch([]string{"npx", "cargo"}, []byte("")); ok {
		t.Fatal("npx cargo: len<2 rest")
	}
	// --- isCargoFetchArgv: npx cargo fetch (L344) ---
	cfNpx, ok := TryCompactCargoFetch([]string{"npx", "cargo", "fetch"}, []byte(""))
	if !ok || string(cfNpx) != "[cargo fetch] ok\n" {
		t.Fatalf("npx cargo fetch: ok=%v %q", ok, cfNpx)
	}
	// --- isCargoFetchArgv: pnpm exec (L346) ---
	cfPnpm, ok := TryCompactCargoFetch([]string{"pnpm", "exec", "cargo", "fetch"}, []byte(""))
	if !ok || string(cfPnpm) != "[cargo fetch] ok\n" {
		t.Fatalf("pnpm exec cargo fetch: ok=%v %q", ok, cfPnpm)
	}

	// --- isCargoUpdateArgv: len < 2 (L367) ---
	if _, ok := TryCompactCargoUpdate([]string{}, []byte("")); ok {
		t.Fatal("cargo update: empty argv")
	}
	// --- isCargoUpdateArgv: npx failure len<2 (L376) ---
	if _, ok := TryCompactCargoUpdate([]string{"npx", "cargo"}, []byte("")); ok {
		t.Fatal("npx cargo: no update subcommand")
	}
	// --- isCargoUpdateArgv: pnpm exec (L384) ---
	cuPnpm, ok := TryCompactCargoUpdate([]string{"pnpm", "exec", "cargo", "update"}, []byte(""))
	if !ok || string(cuPnpm) != "[cargo update] ok\n" {
		t.Fatalf("pnpm exec cargo update: ok=%v %q", ok, cuPnpm)
	}
	// --- isCargoUpdateArgv: yarn (L384-386) ---
	cuYarn, ok := TryCompactCargoUpdate([]string{"yarn", "cargo", "update"}, []byte(""))
	if !ok || string(cuYarn) != "[cargo update] ok\n" {
		t.Fatalf("yarn cargo update: ok=%v %q", ok, cuYarn)
	}

	// --- isSwiftPackageResolveArgv: len < 3 (L402) ---
	if _, ok := TryCompactSwiftPackageResolve([]string{"swift", "package"}, []byte("")); ok {
		t.Fatal("swift package: len<3")
	}
	// --- TryCompactSwiftPackageResolve: yarn branch (L426) ---
	swYarn, ok := TryCompactSwiftPackageResolve([]string{"yarn", "swift", "package", "resolve"}, []byte(""))
	if !ok || string(swYarn) != "[swift package resolve] ok\n" {
		t.Fatalf("yarn swift package resolve: ok=%v %q", ok, swYarn)
	}
}

func TestTryCompactPoetryInstallNonEmptySuccess(t *testing.T) {
	t.Parallel()

	input := poetryInstallCleanFixture(80)
	out, ok := TryCompactPoetryInstall([]string{"poetry", "install"}, []byte(input))
	if !ok {
		t.Fatal("expected poetry install clean success to compact")
	}
	got := string(out)
	if got != "[poetry install] ok (80 installs, 0 updates, 0 removals; lock file written)\n" {
		t.Fatalf("unexpected poetry summary: %q", got)
	}
	if len(out) >= len(input) || strings.Contains(got, "package-079") {
		t.Fatalf("poetry summary did not shrink or leaked package rows: %q", got)
	}

	wrapped, ok := TryCompactPoetryInstall([]string{"pnpm", "exec", "poetry", "install"}, []byte(input))
	if !ok || string(wrapped) != got {
		t.Fatalf("pnpm exec poetry install: ok=%v out=%q", ok, wrapped)
	}
	chain, ok := TryCompactPackageOutput([]string{"yarn", "poetry", "install"}, []byte(input))
	if !ok || string(chain) != got {
		t.Fatalf("package chain poetry install: ok=%v out=%q", ok, chain)
	}

	upToDate := "Installing dependencies from lock file\n\nNo dependencies to install or update\nInstalling the current project: slimtest (0.1.0)\n"
	upToDateOut, ok := TryCompactPoetryInstall([]string{"poetry", "install"}, []byte(upToDate))
	if !ok || string(upToDateOut) != "[poetry install] ok (up to date; current project installed)\n" {
		t.Fatalf("poetry up-to-date: ok=%v out=%q", ok, upToDateOut)
	}
}

func TestTryCompactPoetryInstallNonEmptyGuards(t *testing.T) {
	t.Parallel()

	clean := poetryInstallCleanFixture(3)
	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{name: "wrong command", argv: []string{"poetry", "update"}, stdout: clean},
		{name: "warning", argv: []string{"poetry", "install"}, stdout: "Warning: lock file is not consistent\n" + clean},
		{name: "error", argv: []string{"poetry", "install"}, stdout: "Installing dependencies from lock file\n\nError: package resolution failed\n"},
		{name: "unknown line", argv: []string{"poetry", "install"}, stdout: "Installing dependencies from lock file\nResolving dependencies...\nNo dependencies to install or update\n"},
		{name: "count mismatch", argv: []string{"poetry", "install"}, stdout: "Installing dependencies from lock file\nPackage operations: 2 installs, 0 updates, 0 removals\n  - Installing package-000 (1.0.0)\n"},
		{name: "non-shrinking", argv: []string{"poetry", "install"}, stdout: "No changes.\n"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactPoetryInstall(tt.argv, []byte(tt.stdout))
			if ok || string(out) != tt.stdout {
				t.Fatalf("unsafe poetry output compacted: ok=%v out=%q", ok, out)
			}
		})
	}
}

func TestTryCompactUvInstallNonEmptySuccess(t *testing.T) {
	t.Parallel()

	syncInput := uvPackageCleanFixture(80, true)
	syncOut, ok := TryCompactUvSync([]string{"uv", "sync"}, []byte(syncInput))
	if !ok {
		t.Fatal("expected uv sync clean success to compact")
	}
	wantSync := "[uv sync] ok (resolved 80 packages; prepared 80 packages; installed 80 packages; audited 80 packages)\n"
	if string(syncOut) != wantSync {
		t.Fatalf("unexpected uv sync summary: %q", syncOut)
	}
	if len(syncOut) >= len(syncInput) || strings.Contains(string(syncOut), "uv-package-079") {
		t.Fatalf("uv sync summary did not shrink or leaked package rows: %q", syncOut)
	}

	wrapped, ok := TryCompactUvSync([]string{"pnpm", "exec", "uv", "sync"}, []byte(syncInput))
	if !ok || string(wrapped) != wantSync {
		t.Fatalf("pnpm exec uv sync: ok=%v out=%q", ok, wrapped)
	}
	chain, ok := TryCompactPackageOutput([]string{"yarn", "uv", "sync"}, []byte(syncInput))
	if !ok || string(chain) != wantSync {
		t.Fatalf("package chain uv sync: ok=%v out=%q", ok, chain)
	}

	pipInput := uvPackageCleanFixture(40, false)
	pipOut, ok := TryCompactUvPipInstall([]string{"uv", "pip", "install", "requests"}, []byte(pipInput))
	if !ok {
		t.Fatal("expected uv pip install clean success to compact")
	}
	wantPip := "[uv pip install] ok (resolved 40 packages; prepared 40 packages; installed 40 packages)\n"
	if string(pipOut) != wantPip {
		t.Fatalf("unexpected uv pip summary: %q", pipOut)
	}
}

func TestTryCompactUvInstallNonEmptyGuards(t *testing.T) {
	t.Parallel()

	clean := uvPackageCleanFixture(3, true)
	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{name: "wrong command", argv: []string{"uv", "run", "pip", "install"}, stdout: clean},
		{name: "warning", argv: []string{"uv", "sync"}, stdout: "warning: package is yanked\n" + clean},
		{name: "error", argv: []string{"uv", "sync"}, stdout: "Resolved 3 packages in 10ms\nerror: No solution found when resolving dependencies\n"},
		{name: "unknown line", argv: []string{"uv", "sync"}, stdout: "Resolved 3 packages in 10ms\nDownloading package metadata\nAudited 3 packages in 1ms\n"},
		{name: "count mismatch", argv: []string{"uv", "sync"}, stdout: "Resolved 2 packages in 1ms\nInstalled 2 packages in 1ms\n + uv-package-000==1.0.0\n"},
		{name: "non-shrinking", argv: []string{"uv", "sync"}, stdout: "Audited 1 package in 1ms\n"},
		{name: "empty argv", argv: nil, stdout: ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactUvSync(tt.argv, []byte(tt.stdout))
			if ok || string(out) != tt.stdout {
				t.Fatalf("unsafe uv sync output compacted: ok=%v out=%q", ok, out)
			}
		})
	}
}

func TestPackageInstallParserHelpers(t *testing.T) {
	t.Parallel()

	installs, updates, removals, ok := parsePoetryPackageOperations("Package operations: 1 install, 2 updates, 1 removal")
	if !ok || installs != 1 || updates != 2 || removals != 1 {
		t.Fatalf("poetry package operations parse failed: installs=%d updates=%d removals=%d ok=%v", installs, updates, removals, ok)
	}

	badPoetryOperations := []string{
		"Package operations: 1 installs, 0 updates, 0 removals",
		"Package operations: one install, 0 updates, 0 removals",
		"Package operations: 1 install, 0 updates",
		"Package operations: 1 install, 0 updates, -1 removals",
	}
	for _, input := range badPoetryOperations {
		input := input
		t.Run("bad poetry operations "+input, func(t *testing.T) {
			t.Parallel()
			if _, _, _, ok := parsePoetryPackageOperations(input); ok {
				t.Fatalf("invalid poetry operations parsed: %q", input)
			}
		})
	}

	if !poetryPackageBulletLineOK("- Updating package-a (1.0.0 -> 1.1.0)") {
		t.Fatal("valid poetry update bullet rejected")
	}
	if poetryPackageBulletLineOK("- Installing package-a") {
		t.Fatal("poetry bullet without version/detail parens accepted")
	}

	count, ok := parseUvPackageCountLine("Installed 1 package in 4ms", "Installed")
	if !ok || count != 1 {
		t.Fatalf("uv singular package count parse failed: count=%d ok=%v", count, ok)
	}
	badUvCounts := []string{
		"Installed one package in 4ms",
		"Installed 1 packages in 4ms",
		"Installed 2 package in 4ms",
		"Installed 2 packages",
		"Prepared 2 packages after 4ms",
	}
	for _, input := range badUvCounts {
		input := input
		t.Run("bad uv count "+input, func(t *testing.T) {
			t.Parallel()
			if _, ok := parseUvPackageCountLine(input, strings.Fields(input)[0]); ok {
				t.Fatalf("invalid uv package count parsed: %q", input)
			}
		})
	}

	if !npmInstallArgvUnsafe([]string{"--loglevel", "verbose"}) ||
		!npmInstallArgvUnsafe([]string{"--loglevel=silly"}) ||
		!npmInstallArgvUnsafe([]string{"--loglevel"}) ||
		!npmInstallArgvUnsafe([]string{"--package-lock-only"}) {
		t.Fatal("unsafe npm install argv modes were not rejected")
	}
	if npmInstallArgvUnsafe([]string{"--ignore-scripts", "--loglevel", "notice"}) ||
		npmInstallLogLevelUnsafe("http") {
		t.Fatal("safe npm install argv/loglevel modes were rejected")
	}

	if pnpmInstallArgvUnsafe([]string{"--reporter", "append-only", "--prod"}) ||
		pnpmInstallArgvUnsafe([]string{"--reporter=default", "--dev"}) {
		t.Fatal("safe pnpm install argv modes were rejected")
	}
	if !pnpmInstallArgvUnsafe([]string{"--reporter"}) ||
		!pnpmInstallArgvUnsafe([]string{"--reporter", "ndjson"}) ||
		!pnpmInstallArgvUnsafe([]string{"--stream"}) ||
		!pnpmInstallArgvUnsafe([]string{"left-pad"}) {
		t.Fatal("unsafe pnpm install argv modes were not rejected")
	}

	if yarnInstallArgvUnsafe([]string{"--non-interactive", "--frozen-lockfile", "--no-progress"}) {
		t.Fatal("safe yarn install argv modes were rejected")
	}
	if !yarnInstallArgvUnsafe([]string{"--ignore-scripts"}) ||
		!yarnInstallArgvUnsafe([]string{"--json"}) ||
		!yarnInstallArgvUnsafe([]string{"left-pad"}) {
		t.Fatal("unsafe yarn install argv modes were not rejected")
	}

	pnpmAdded, pnpmRemoved, ok := parsePnpmPackagesLine("Packages: +3 -1")
	if !ok || pnpmAdded != 3 || pnpmRemoved != 1 {
		t.Fatalf("pnpm packages parse failed: added=%d removed=%d ok=%v", pnpmAdded, pnpmRemoved, ok)
	}
	badPnpmPackages := []string{"Packages:", "Packages: +0", "Packages: +x", "Packages: 3", "Packages: +2 ~1"}
	for _, input := range badPnpmPackages {
		input := input
		t.Run("bad pnpm packages "+input, func(t *testing.T) {
			t.Parallel()
			if _, _, ok := parsePnpmPackagesLine(input); ok {
				t.Fatalf("invalid pnpm packages line parsed: %q", input)
			}
		})
	}
	if !pnpmProgressLineOK("Progress: resolved 2, reused 1, downloaded 0, added 2, done") {
		t.Fatal("valid pnpm progress line rejected")
	}
	badPnpmProgress := []string{
		"Progress:",
		"Progress: resolving 2",
		"Progress: resolved two",
		"Progress: resolved -1",
		"Progress: resolved 1, done now",
	}
	for _, input := range badPnpmProgress {
		input := input
		t.Run("bad pnpm progress "+input, func(t *testing.T) {
			t.Parallel()
			if pnpmProgressLineOK(input) {
				t.Fatalf("invalid pnpm progress line accepted: %q", input)
			}
		})
	}
	if !pnpmProgressGlyphLineOK("++--") || pnpmProgressGlyphLineOK("") ||
		pnpmProgressGlyphLineOK(strings.Repeat("+", 201)) ||
		pnpmProgressGlyphLineOK("++x") {
		t.Fatal("pnpm glyph line validation failed")
	}
	if !pnpmDependencySectionLineOK("devDependencies:") || pnpmDependencySectionLineOK("scripts:") {
		t.Fatal("pnpm dependency section validation failed")
	}
	if !pnpmDependencyRowOK("+ @scope/pkg 1.2.3") ||
		pnpmDependencyRowOK("+ ") ||
		pnpmDependencyRowOK("+ pkg 1.0.0 extra") ||
		pnpmDependencyRowOK("+ pkg\t1.0.0") {
		t.Fatal("pnpm dependency row validation failed")
	}
	if !pnpmDoneLineOK("Done in 256ms using pnpm v10.13.1") ||
		pnpmDoneLineOK("Done after 256ms using pnpm v10.13.1") ||
		pnpmDoneLineOK("Done in 256ms using yarn v1.22.22") {
		t.Fatal("pnpm done line validation failed")
	}

	if !yarnClassicHeaderLineOK("yarn install v1.22.22", "install") ||
		yarnClassicHeaderLineOK("yarn install v4.0.0", "install") ||
		yarnClassicHeaderLineOK("yarn add v1.22.22", "install") {
		t.Fatal("yarn classic header validation failed")
	}
	if !yarnClassicStepLineOK("[4/4] Rebuilding all packages...") {
		t.Fatal("valid yarn classic step rejected")
	}
	badYarnSteps := []string{
		"1/4] Resolving packages...",
		"[5/4] Resolving packages...",
		"[x/4] Resolving packages...",
		"[1/4] Running custom hook...",
	}
	for _, input := range badYarnSteps {
		input := input
		t.Run("bad yarn step "+input, func(t *testing.T) {
			t.Parallel()
			if yarnClassicStepLineOK(input) {
				t.Fatalf("invalid yarn step accepted: %q", input)
			}
		})
	}
	if count, ok := parseYarnClassicSavedDependencyLine("success Saved 1 new dependency."); !ok || count != 1 {
		t.Fatalf("yarn saved dependency singular parse failed: count=%d ok=%v", count, ok)
	}
	if count, ok := parseYarnClassicSavedDependencyLine("success Saved 2 new dependencies."); !ok || count != 2 {
		t.Fatalf("yarn saved dependency plural parse failed: count=%d ok=%v", count, ok)
	}
	badYarnSaved := []string{
		"success Saved two new dependencies.",
		"success Saved 1 new dependencies.",
		"success Saved 0 new dependencies.",
		"success Saved 1 dependency.",
	}
	for _, input := range badYarnSaved {
		input := input
		t.Run("bad yarn saved "+input, func(t *testing.T) {
			t.Parallel()
			if _, ok := parseYarnClassicSavedDependencyLine(input); ok {
				t.Fatalf("invalid yarn saved dependency line parsed: %q", input)
			}
		})
	}
	if !yarnClassicDependencyRowOK("└─ @scope/pkg@1.2.3") ||
		yarnClassicDependencyRowOK("└─ ") ||
		yarnClassicDependencyRowOK("└─ pkg\t1.2.3") {
		t.Fatal("yarn dependency row validation failed")
	}
	if !yarnClassicDoneLineOK("Done in 0.04s.") ||
		yarnClassicDoneLineOK("Done after 0.04s.") ||
		yarnClassicDoneLineOK("Done in .") {
		t.Fatal("yarn done line validation failed")
	}

	if count, ok := parseNpmFundingLine("1 package are looking for funding"); !ok || count != 1 {
		t.Fatalf("npm funding singular parse failed: count=%d ok=%v", count, ok)
	}
	badFunding := []string{
		"1 package is looking for funding",
		"two packages are looking for funding",
		"-1 packages are looking for funding",
		"2 package are looking for funding",
		"2 packages are looking for funding now",
	}
	for _, input := range badFunding {
		input := input
		t.Run("bad npm funding "+input, func(t *testing.T) {
			t.Parallel()
			if _, ok := parseNpmFundingLine(input); ok {
				t.Fatalf("invalid npm funding line parsed: %q", input)
			}
		})
	}

	if count, ok := parseNpmAuditedTail("2 packages in 4s"); !ok || count != 2 {
		t.Fatalf("npm audited tail parse failed: count=%d ok=%v", count, ok)
	}
	badAudited := []string{
		"2 packages",
		"two packages in 4s",
		"-1 packages in 4s",
		"1 packages in 4s",
		"2 packages after 4s",
		"2 packages in 4s extra",
	}
	for _, input := range badAudited {
		input := input
		t.Run("bad npm audited "+input, func(t *testing.T) {
			t.Parallel()
			if _, ok := parseNpmAuditedTail(input); ok {
				t.Fatalf("invalid npm audited tail parsed: %q", input)
			}
		})
	}

	if part, ok := parseNpmInstallOperationPart("removed 1 package"); !ok || part != "removed 1 package" {
		t.Fatalf("npm operation parse failed: part=%q ok=%v", part, ok)
	}
	badOperations := []string{
		"installed 1 package",
		"added one package",
		"added -1 packages",
		"added 1 packages",
		"added 1 package quickly",
	}
	for _, input := range badOperations {
		input := input
		t.Run("bad npm operation "+input, func(t *testing.T) {
			t.Parallel()
			if _, ok := parseNpmInstallOperationPart(input); ok {
				t.Fatalf("invalid npm operation parsed: %q", input)
			}
		})
	}

	badSummaries := []string{
		"added 1 package in 1s",
		"added 1 package, and audited one package in 1s",
		"installed 1 package, and audited 2 packages in 1s",
		"up to date, audited 1 packages in 1s",
	}
	for _, input := range badSummaries {
		input := input
		t.Run("bad npm summary "+input, func(t *testing.T) {
			t.Parallel()
			if _, ok := parseNpmInstallAuditSummaryLine(input); ok {
				t.Fatalf("invalid npm install summary parsed: %q", input)
			}
		})
	}
}

func TestTryCompactPackageInstallNonEmptyEdgeCases(t *testing.T) {
	t.Parallel()

	poetryMixed := `Installing dependencies from lock file
Package operations: 1 install, 1 update, 1 removal
  - Installing alpha (1.0.0)
  - Updating beta (1.0.0 -> 1.1.0)
  - Removing oldpkg (0.9.0)
`
	poetryOut, ok := TryCompactPoetryInstall([]string{"poetry", "install"}, []byte(poetryMixed))
	if !ok || string(poetryOut) != "[poetry install] ok (1 install, 1 update, 1 removal)\n" {
		t.Fatalf("poetry mixed operations: ok=%v out=%q", ok, poetryOut)
	}

	poetryBadCurrentProject := "Installing dependencies from lock file\nInstalling the current project: \n"
	if out, ok := TryCompactPoetryInstall([]string{"poetry", "install"}, []byte(poetryBadCurrentProject)); ok || string(out) != poetryBadCurrentProject {
		t.Fatalf("empty current project compacted: ok=%v out=%q", ok, out)
	}

	uvUpdate := `Using Python 3.12.4
Resolved 2 packages in 2ms
Updated 2 packages in 1ms
 ~ alpha==1.1.0
 ~ beta==2.0.0
`
	uvOut, ok := TryCompactUvSync([]string{"uv", "sync"}, []byte(uvUpdate))
	if !ok || string(uvOut) != "[uv sync] ok (resolved 2 packages; updated 2 packages)\n" {
		t.Fatalf("uv update rows: ok=%v out=%q", ok, uvOut)
	}

	uvRowsWithoutChangedCount := "Resolved 1 package in 1ms\nAudited 1 package in 1ms\n + alpha==1.0.0\n"
	if out, ok := TryCompactUvSync([]string{"uv", "sync"}, []byte(uvRowsWithoutChangedCount)); ok || string(out) != uvRowsWithoutChangedCount {
		t.Fatalf("uv row without changed count compacted: ok=%v out=%q", ok, out)
	}

	uvWarnColon := "Resolved 1 package in 1ms\nwarn: alpha is yanked\nInstalled 1 package in 1ms\n + alpha==1.0.0\n"
	if out, ok := TryCompactUvSync([]string{"uv", "sync"}, []byte(uvWarnColon)); ok || string(out) != uvWarnColon {
		t.Fatalf("uv warn: output compacted: ok=%v out=%q", ok, out)
	}
}

func TestTryCompactNpmInstallNonEmptyCleanSuccess(t *testing.T) {
	t.Parallel()

	input := `npm warn deprecated lodash@3.10.1: maintenance mode
npm warn deprecated uuid@3.4.0: Please upgrade  to v9
npm verb lock using: npm@10.2.4
npm http fetch GET 200 https://registry.npmjs.org/lodash 1234ms
npm timing npm:load:preflight Completed in 5ms

added 342 packages, and audited 343 packages in 12s

45 packages are looking for funding
  run 'npm fund' for details

found 0 vulnerabilities
`
	out, ok := TryCompactPackageOutput([]string{"npm", "install"}, []byte(input))
	if ok || string(out) != input {
		t.Fatalf("npm install warning output must fail open: ok=%v out=%q", ok, out)
	}

	clean := npmInstallCleanFixture(342)
	cleanOut, ok := TryCompactNpmInstall([]string{"npm", "install"}, []byte(clean))
	if !ok {
		t.Fatalf("expected compact npm install clean output, got pass-through")
	}
	want := "[npm install] added 342 packages; audited 343 packages; funding 45 packages; 0 vulnerabilities\n"
	if string(cleanOut) != want {
		t.Fatalf("unexpected npm clean summary: %q", cleanOut)
	}
	if len(cleanOut) >= len(clean) || strings.Contains(string(cleanOut), "package_341") {
		t.Fatalf("npm clean summary did not shrink or leaked package rows: %q", cleanOut)
	}

	chainOut, ok := TryCompactPackageOutput([]string{"npm", "install"}, []byte(clean))
	if !ok || string(chainOut) != want {
		t.Fatalf("package chain npm clean output: ok=%v out=%q", ok, chainOut)
	}

	upToDate := "\nup to date, audited 1 package in 292ms\n\nfound 0 vulnerabilities\n"
	upToDateOut, ok := TryCompactNpmInstall([]string{"npm", "ci"}, []byte(upToDate))
	if !ok || string(upToDateOut) != "[npm ci] up to date; audited 1 package; 0 vulnerabilities\n" {
		t.Fatalf("npm ci up-to-date summary: ok=%v out=%q", ok, upToDateOut)
	}

	updated := "\nchanged 2 packages, and audited 9 packages in 1s\n\nfound 0 vulnerabilities\n"
	updatedOut, ok := TryCompactNpmInstall([]string{"npm", "update"}, []byte(updated))
	if !ok || string(updatedOut) != "[npm update] changed 2 packages; audited 9 packages; 0 vulnerabilities\n" {
		t.Fatalf("npm update summary: ok=%v out=%q", ok, updatedOut)
	}
}

func TestTryCompactNpmInstallNonEmptyGuards(t *testing.T) {
	t.Parallel()

	clean := npmInstallCleanFixture(3)
	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{name: "warning", argv: []string{"npm", "install"}, stdout: "npm warn deprecated left-pad@1.3.0: use String.prototype.padStart()\n" + clean},
		{name: "nonzero vulnerabilities", argv: []string{"npm", "install"}, stdout: strings.Replace(clean, "found 0 vulnerabilities", "3 vulnerabilities (1 moderate, 2 high)", 1)},
		{name: "unknown lifecycle line", argv: []string{"npm", "install"}, stdout: "postinstall generated src/config.ts\n" + clean},
		{name: "missing funding prompt", argv: []string{"npm", "install"}, stdout: strings.Replace(clean, "  run `npm fund` for details\n", "", 1)},
		{name: "dry run", argv: []string{"npm", "install", "--dry-run"}, stdout: clean},
		{name: "json mode", argv: []string{"npm", "ci", "--json"}, stdout: clean},
		{name: "bad plural", argv: []string{"npm", "install"}, stdout: "added 1 packages, and audited 2 packages in 1s\nfound 0 vulnerabilities\n"},
		{name: "non-shrinking", argv: []string{"npm", "install"}, stdout: "up to date, audited 1 package in 1s\nfound 0 vulnerabilities\n"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactNpmInstall(tt.argv, []byte(tt.stdout))
			if ok || string(out) != tt.stdout {
				t.Fatalf("unsafe npm install output compacted: ok=%v out=%q", ok, out)
			}
			chainOut, chainOK := TryCompactPackageOutput(tt.argv, []byte(tt.stdout))
			if chainOK || string(chainOut) != tt.stdout {
				t.Fatalf("unsafe npm install chain compacted: ok=%v out=%q", chainOK, chainOut)
			}
		})
	}
}

func TestTryCompactPnpmInstallNonEmptyCleanSuccess(t *testing.T) {
	t.Parallel()

	clean := pnpmInstallCleanFixture(80, true)
	out, ok := TryCompactPnpmInstall([]string{"pnpm", "install", "--ignore-scripts"}, []byte(clean))
	if !ok {
		t.Fatal("expected pnpm install clean success to compact")
	}
	want := "[pnpm install] ok (added 80 packages)\n"
	if string(out) != want {
		t.Fatalf("unexpected pnpm summary: %q", out)
	}
	if len(out) >= len(clean) || strings.Contains(string(out), "slimference-pnpm-package-079") {
		t.Fatalf("pnpm summary did not shrink or leaked package rows: %q", out)
	}

	chainOut, ok := TryCompactPackageOutput([]string{"pnpm", "install", "--reporter=append-only"}, []byte(clean))
	if !ok || string(chainOut) != want {
		t.Fatalf("package chain pnpm clean output: ok=%v out=%q", ok, chainOut)
	}

	lockfileState := "Lockfile is up to date, resolution step is skipped\n" + pnpmInstallCleanFixture(2, false)
	lockfileOut, ok := TryCompactPnpmInstall([]string{"pnpm", "ci", "--frozen-lockfile"}, []byte(lockfileState))
	if !ok || string(lockfileOut) != "[pnpm ci] ok (lockfile up to date; added 2 packages)\n" {
		t.Fatalf("pnpm ci lockfile summary: ok=%v out=%q", ok, lockfileOut)
	}

	upToDate := "Already up to date\n\nDone in 192ms using pnpm v10.13.1\n"
	upToDateOut, ok := TryCompactPnpmInstall([]string{"pnpm", "update", "--offline"}, []byte(upToDate))
	if !ok || string(upToDateOut) != "[pnpm update] ok (up to date)\n" {
		t.Fatalf("pnpm update up-to-date summary: ok=%v out=%q", ok, upToDateOut)
	}
}

func TestTryCompactPnpmInstallNonEmptyGuards(t *testing.T) {
	t.Parallel()

	clean := pnpmInstallCleanFixture(3, true)
	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{name: "wrong command", argv: []string{"pnpm", "add", "x"}, stdout: clean},
		{name: "warning", argv: []string{"pnpm", "install", "--ignore-scripts"}, stdout: " WARN  deprecated left-pad@1.3.0\n" + clean},
		{name: "lockfile only", argv: []string{"pnpm", "install", "--lockfile-only"}, stdout: clean},
		{name: "ndjson reporter", argv: []string{"pnpm", "install", "--reporter=ndjson"}, stdout: clean},
		{name: "positional package", argv: []string{"pnpm", "update", "left-pad"}, stdout: clean},
		{name: "unknown flag", argv: []string{"pnpm", "install", "--workspace-concurrency=4"}, stdout: clean},
		{name: "bad progress", argv: []string{"pnpm", "install"}, stdout: strings.Replace(clean, "Progress: resolved", "Progress: resolving", 1)},
		{name: "bad package delta", argv: []string{"pnpm", "install"}, stdout: strings.Replace(clean, "Packages: +3", "Packages: three", 1)},
		{name: "update notifier", argv: []string{"pnpm", "install"}, stdout: clean + "╭────────────────╮\n│ Update available │\n╰────────────────╯\n"},
		{name: "missing done", argv: []string{"pnpm", "install"}, stdout: strings.Replace(clean, "Done in 256ms using pnpm v10.13.1\n", "", 1)},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactPnpmInstall(tt.argv, []byte(tt.stdout))
			if ok || string(out) != tt.stdout {
				t.Fatalf("unsafe pnpm install output compacted: ok=%v out=%q", ok, out)
			}
			chainOut, chainOK := TryCompactPackageOutput(tt.argv, []byte(tt.stdout))
			if chainOK || string(chainOut) != tt.stdout {
				t.Fatalf("unsafe pnpm install chain compacted: ok=%v out=%q", chainOK, chainOut)
			}
		})
	}
}

func TestTryCompactYarnInstallNonEmptyCleanSuccess(t *testing.T) {
	t.Parallel()

	clean := yarnClassicInstallCleanFixture()
	out, ok := TryCompactYarnInstall([]string{"yarn", "install", "--non-interactive"}, []byte(clean))
	if !ok {
		t.Fatal("expected yarn install clean success to compact")
	}
	want := "[yarn install] ok (lockfile saved)\n"
	if string(out) != want {
		t.Fatalf("unexpected yarn summary: %q", out)
	}
	if len(out) >= len(clean) || strings.Contains(string(out), "Resolving packages") {
		t.Fatalf("yarn summary did not shrink or leaked step rows: %q", out)
	}

	upToDate := "yarn install v1.22.22\n[1/4] Resolving packages...\nsuccess Already up-to-date.\nDone in 0.03s.\n"
	upToDateOut, ok := TryCompactYarnInstall([]string{"yarn", "install", "--frozen-lockfile"}, []byte(upToDate))
	if !ok || string(upToDateOut) != "[yarn install] ok (up to date)\n" {
		t.Fatalf("yarn up-to-date summary: ok=%v out=%q", ok, upToDateOut)
	}

	upgrade := yarnClassicUpgradeCleanFixture(3)
	upgradeOut, ok := TryCompactPackageOutput([]string{"yarn", "upgrade", "--non-interactive"}, []byte(upgrade))
	if !ok || string(upgradeOut) != "[yarn upgrade] ok (saved 3 dependencies; lockfile saved)\n" {
		t.Fatalf("yarn upgrade summary: ok=%v out=%q", ok, upgradeOut)
	}
}

func TestTryCompactYarnInstallNonEmptyGuards(t *testing.T) {
	t.Parallel()

	clean := yarnClassicInstallCleanFixture()
	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{name: "wrong command", argv: []string{"yarn", "add", "left-pad"}, stdout: clean},
		{name: "ignore scripts warning", argv: []string{"yarn", "install", "--ignore-scripts"}, stdout: strings.Replace(clean, "success Saved lockfile.", "warning Ignored scripts due to flag.\nsuccess Saved lockfile.", 1)},
		{name: "json mode", argv: []string{"yarn", "install", "--json"}, stdout: clean},
		{name: "silent mode", argv: []string{"yarn", "install", "--silent"}, stdout: clean},
		{name: "positional package", argv: []string{"yarn", "upgrade", "left-pad"}, stdout: yarnClassicUpgradeCleanFixture(1)},
		{name: "berry output", argv: []string{"yarn", "install"}, stdout: "➤ YN0000: · Yarn 4.9.2\n➤ YN0000: ┌ Resolution step\n➤ YN0000: └ Completed\n"},
		{name: "missing header", argv: []string{"yarn", "install"}, stdout: strings.TrimPrefix(clean, "yarn install v1.22.22\n")},
		{name: "bad step", argv: []string{"yarn", "install"}, stdout: strings.Replace(clean, "[1/4] Resolving packages...", "[1/4] Running custom hook...", 1)},
		{name: "missing done", argv: []string{"yarn", "install"}, stdout: strings.Replace(clean, "Done in 0.04s.\n", "", 1)},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactYarnInstall(tt.argv, []byte(tt.stdout))
			if ok || string(out) != tt.stdout {
				t.Fatalf("unsafe yarn install output compacted: ok=%v out=%q", ok, out)
			}
			chainOut, chainOK := TryCompactPackageOutput(tt.argv, []byte(tt.stdout))
			if chainOK || string(chainOut) != tt.stdout {
				t.Fatalf("unsafe yarn install chain compacted: ok=%v out=%q", chainOK, chainOut)
			}
		})
	}
}

func TestTryCompactBunInstallNonEmptyCleanSuccess(t *testing.T) {
	t.Parallel()

	clean := bunInstallCleanFixture(80, true)
	out, ok := TryCompactBunInstall([]string{"bun", "install", "--ignore-scripts"}, []byte(clean))
	if !ok {
		t.Fatal("expected bun install clean success to compact")
	}
	want := "[bun install] ok (installed 80 packages; lockfile saved)\n"
	if string(out) != want {
		t.Fatalf("unexpected bun summary: %q", out)
	}
	if len(out) >= len(clean) || strings.Contains(string(out), "bun-package-079") {
		t.Fatalf("bun summary did not shrink or leaked package rows: %q", out)
	}

	wrapped, ok := TryCompactPackageOutput([]string{"pnpm", "exec", "bun", "install", "--ignore-scripts"}, []byte(clean))
	if !ok || string(wrapped) != want {
		t.Fatalf("pnpm exec bun install: ok=%v out=%q", ok, wrapped)
	}

	noPackages := "bun install v1.3.14 (0d9b296a)\nNo packages! Deleted empty lockfile\n\n[1.00ms] done\n"
	noPackagesOut, ok := TryCompactBunInstall([]string{"bun", "install", "--ignore-scripts"}, []byte(noPackages))
	if !ok || string(noPackagesOut) != "[bun install] ok (no packages; empty lockfile deleted)\n" {
		t.Fatalf("bun no-packages summary: ok=%v out=%q", ok, noPackagesOut)
	}
}

func TestTryCompactBunInstallNonEmptyGuards(t *testing.T) {
	t.Parallel()

	clean := bunInstallCleanFixture(3, true)
	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{name: "wrong command", argv: []string{"bun", "add", "x"}, stdout: clean},
		{name: "warning", argv: []string{"bun", "install", "--ignore-scripts"}, stdout: "warning: package has a deprecated postinstall\n" + clean},
		{name: "dry run", argv: []string{"bun", "install", "--dry-run"}, stdout: clean},
		{name: "lockfile only", argv: []string{"bun", "install", "--lockfile-only"}, stdout: clean},
		{name: "unknown flag", argv: []string{"bun", "install", "--backend=copyfile"}, stdout: clean},
		{name: "positional package", argv: []string{"bun", "install", "left-pad@1.3.0"}, stdout: clean},
		{name: "missing header", argv: []string{"bun", "install", "--ignore-scripts"}, stdout: strings.TrimPrefix(clean, "bun install v1.3.14 (0d9b296a)\n")},
		{name: "bad row", argv: []string{"bun", "install", "--ignore-scripts"}, stdout: strings.Replace(clean, "+ bun-package-000@1.0.0", "+ bun package 000", 1)},
		{name: "count mismatch", argv: []string{"bun", "install", "--ignore-scripts"}, stdout: strings.Replace(clean, "3 packages installed", "2 packages installed", 1)},
		{name: "missing terminal", argv: []string{"bun", "install", "--ignore-scripts"}, stdout: strings.Replace(clean, "3 packages installed [9.00ms]\n", "", 1)},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactBunInstall(tt.argv, []byte(tt.stdout))
			if ok || string(out) != tt.stdout {
				t.Fatalf("unsafe bun install output compacted: ok=%v out=%q", ok, out)
			}
			chainOut, chainOK := TryCompactPackageOutput(tt.argv, []byte(tt.stdout))
			if chainOK || string(chainOut) != tt.stdout {
				t.Fatalf("unsafe bun install chain compacted: ok=%v out=%q", chainOK, chainOut)
			}
		})
	}
}

func TestTryCompactPackageOutput_pipInstall(t *testing.T) {
	t.Parallel()
	// pip install with verbose output
	input := `Collecting requests
  Downloading requests-2.31.0-py3-none-any.whl (62 kB)
     ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 62.6/62.6 kB 1.2 MB/s eta 0:00:00
Collecting charset-normalizer<4,>=2
  Downloading charset_normalizer-3.3.2-cp311-cp311-linux_x86_64.whl (482.6 kB)
Successfully installed charset-normalizer-3.3.2 idna-3.6 requests-2.31.0 urllib3-2.1.0
`
	out, ok := TryCompactPackageOutput([]string{"pip", "install", "requests"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact pip output, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "Successfully installed") {
		t.Errorf("want summary, got: %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact should be shorter: %d vs %d", len(s), len(input))
	}
}

func TestTryCompactPackageOutput_installResolverErrors(t *testing.T) {
	t.Parallel()
	input := strings.Repeat("npm timing idealTree Completed in 12ms\n", 20) + `npm ERR! code ERESOLVE
npm ERR! ERESOLVE unable to resolve dependency tree
npm ERR! peer react@"^18" from @example/widget@1.0.0
`
	out, ok := TryCompactPackageOutput([]string{"npm", "install"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact npm resolver error, got pass-through")
	}
	s := string(out)
	for _, want := range []string{"[npm install]", "ERESOLVE", "peer react"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %q", want, s)
		}
	}
	if strings.Contains(s, "idealTree") || len(s) >= len(input) {
		t.Fatalf("resolver compaction kept noise or did not shrink: %q", s)
	}

	pnpmInput := strings.Repeat("Progress: resolved 100, reused 99\n", 20) + `ERR_PNPM_NO_MATCHING_VERSION No matching version found for left-pad@99.0.0
Failed with errors
`
	pnpmOut, ok := TryCompactPackageOutput([]string{"pnpm", "update"}, []byte(pnpmInput))
	if !ok {
		t.Fatalf("expected compact pnpm resolver error, got pass-through")
	}
	if !strings.Contains(string(pnpmOut), "ERR_PNPM_NO_MATCHING_VERSION") || strings.Contains(string(pnpmOut), "Progress:") {
		t.Fatalf("unexpected pnpm resolver compact output: %q", pnpmOut)
	}

	pipInput := strings.Repeat("Downloading dependency metadata\n", 20) + `ERROR: Could not find a version that satisfies the requirement does-not-exist==99
ERROR: ResolutionImpossible: for help visit https://pip.pypa.io
`
	pipOut, ok := TryCompactPackageOutput([]string{"uv", "pip", "install", "does-not-exist==99"}, []byte(pipInput))
	if !ok {
		t.Fatalf("expected compact uv pip resolver error, got pass-through")
	}
	if !strings.Contains(string(pipOut), "ResolutionImpossible") || strings.Contains(string(pipOut), "Downloading") {
		t.Fatalf("unexpected uv pip resolver compact output: %q", pipOut)
	}

	uvSyncInput := strings.Repeat("Resolved 120 packages\n", 20) + "error: No solution found when resolving dependencies\n"
	uvSyncOut, ok := TryCompactPackageOutput([]string{"uv", "sync"}, []byte(uvSyncInput))
	if !ok {
		t.Fatalf("expected compact uv sync resolver error, got pass-through")
	}
	if !strings.Contains(string(uvSyncOut), "No solution found") || strings.Contains(string(uvSyncOut), "Resolved 120") {
		t.Fatalf("unexpected uv sync resolver compact output: %q", uvSyncOut)
	}

	yarnInput := strings.Repeat("YN0000: Resolving packages\n", 20) + `➤ YN0001: │ Error: @example/missing isn't supported by any available resolver
➤ YN0000: Failed with errors in 1s 20ms
`
	yarnOut, ok := TryCompactPackageOutput([]string{"yarn", "install"}, []byte(yarnInput))
	if !ok {
		t.Fatalf("expected compact yarn resolver error, got pass-through")
	}
	if !strings.Contains(string(yarnOut), "YN0001") || strings.Contains(string(yarnOut), "Resolving packages") {
		t.Fatalf("unexpected yarn resolver compact output: %q", yarnOut)
	}

	bunInput := strings.Repeat("bun install v1.2.0\n", 20) + "error: No matching version found for package totally-missing@99\n"
	bunOut, ok := TryCompactPackageOutput([]string{"bun", "install"}, []byte(bunInput))
	if !ok {
		t.Fatalf("expected compact bun resolver error, got pass-through")
	}
	if !strings.Contains(string(bunOut), "No matching version") || strings.Contains(string(bunOut), "bun install v") {
		t.Fatalf("unexpected bun resolver compact output: %q", bunOut)
	}
}

func TestTryCompactPackageAuditJSONZeroVulnerabilities(t *testing.T) {
	t.Parallel()

	npmJSON := `{
  "auditReportVersion": 2,
  "vulnerabilities": {},
  "metadata": {
    "vulnerabilities": {
      "info": 0,
      "low": 0,
      "moderate": 0,
      "high": 0,
      "critical": 0,
      "total": 0
    },
    "dependencies": {
      "prod": 140,
      "dev": 87,
      "optional": 3,
      "peer": 12,
      "peerOptional": 0,
      "total": 242
    }
  }
}`
	out, ok := TryCompactPackageAuditJSON([]string{"npm", "audit", "--json"}, []byte(npmJSON))
	if !ok || string(out) != "[npm audit] 0 vulnerabilities\n" {
		t.Fatalf("npm audit --json zero vulnerabilities: ok=%v out=%q", ok, out)
	}
	chainOut, ok := TryCompactPackageOutput([]string{"npm", "audit", "--audit-level=high", "--json=true"}, []byte(npmJSON))
	if !ok || string(chainOut) != "[npm audit] 0 vulnerabilities\n" {
		t.Fatalf("npm audit chain: ok=%v out=%q", ok, chainOut)
	}

	pnpmJSON := `{
  "advisories": {},
  "actions": [],
  "muted": [],
  "metadata": {
    "vulnerabilities": {
      "info": 0,
      "low": 0,
      "moderate": 0,
      "high": 0,
      "critical": 0,
      "total": 0
    },
    "dependencies": {
      "prod": 12,
      "dev": 34,
      "optional": 0,
      "total": 46
    }
  }
}`
	pnpmOut, ok := TryCompactPackageAuditJSON([]string{"pnpm", "audit", "--json=1"}, []byte(pnpmJSON))
	if !ok || string(pnpmOut) != "[pnpm audit] 0 vulnerabilities\n" {
		t.Fatalf("pnpm audit --json zero vulnerabilities: ok=%v out=%q", ok, pnpmOut)
	}
}

func TestTryCompactPackageAuditJSONRejectsUnsafeOrAmbiguousReports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
	}{
		{
			name: "text output is not enough",
			argv: []string{"npm", "audit"},
			stdout: strings.Repeat("auditing package tree\n", 20) +
				"found 0 vulnerabilities\n",
		},
		{
			name:   "json false flag overrides json true",
			argv:   []string{"npm", "audit", "--json", "--json=false"},
			stdout: `{"vulnerabilities":{},"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0}}}`,
		},
		{
			name:   "nonzero total",
			argv:   []string{"npm", "audit", "--json"},
			stdout: `{"vulnerabilities":{},"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":1}}}`,
		},
		{
			name:   "nonzero high",
			argv:   []string{"npm", "audit", "--json"},
			stdout: `{"vulnerabilities":{},"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":1,"critical":0,"total":1}}}`,
		},
		{
			name:   "missing severity key",
			argv:   []string{"pnpm", "audit", "--json"},
			stdout: `{"advisories":{},"actions":[],"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"total":0}}}`,
		},
		{
			name:   "extra severity key",
			argv:   []string{"pnpm", "audit", "--json"},
			stdout: `{"advisories":{},"actions":[],"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0,"unknown":0}}}`,
		},
		{
			name:   "nonempty npm vulnerabilities object",
			argv:   []string{"npm", "audit", "--json"},
			stdout: `{"vulnerabilities":{"lodash":{"severity":"high"}},"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0}}}`,
		},
		{
			name:   "nonempty pnpm advisories object",
			argv:   []string{"pnpm", "audit", "--json"},
			stdout: `{"advisories":{"1":{"severity":"low"}},"actions":[],"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0}}}`,
		},
		{
			name:   "nonempty pnpm actions array",
			argv:   []string{"pnpm", "audit", "--json"},
			stdout: `{"advisories":{},"actions":[{"action":"install"}],"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0}}}`,
		},
		{
			name:   "invalid json",
			argv:   []string{"npm", "audit", "--json"},
			stdout: `{"metadata":`,
		},
		{
			name:   "unsupported command",
			argv:   []string{"yarn", "audit", "--json"},
			stdout: `{"vulnerabilities":{},"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0}}}`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactPackageAuditJSON(tt.argv, []byte(tt.stdout))
			if ok || string(out) != tt.stdout {
				t.Fatalf("unsafe audit report compacted: ok=%v out=%q", ok, out)
			}
		})
	}
}

// TestExtractPkgSummary exercises all branches of extractPkgSummary directly.
func TestExtractPkgSummary(t *testing.T) {
	t.Parallel()

	// yarn "Done in Xs." branch
	yarnOutput := strings.Repeat("info resolving packages...\n", 30) + "Done in 3.5s.\n"
	out, ok := extractPkgSummary(yarnOutput, "yarn install")
	if !ok {
		t.Fatalf("yarn Done in: expected compact, got false")
	}
	if !strings.Contains(out, "Done in 3.5s.") {
		t.Errorf("yarn: want Done in summary, got %q", out)
	}

	// bundler "Bundle complete!" branch
	bundlerOutput := strings.Repeat("Fetching gem activesupport 7.0.4\n", 20) + "Bundle complete! 10 Gemfile dependencies, 42 gems now installed.\n"
	out2, ok2 := extractPkgSummary(bundlerOutput, "bundle install")
	if !ok2 {
		t.Fatalf("bundler: expected compact, got false")
	}
	if !strings.Contains(out2, "Bundle complete") {
		t.Errorf("bundler: want Bundle complete, got %q", out2)
	}

	// No summary lines found → false
	out3, ok3 := extractPkgSummary("resolving packages...\nsome generic output\n", "npm install")
	if ok3 {
		t.Errorf("no summary: want false, got true with %q", out3)
	}

	// Summary not shorter than input → false (very short input)
	shortInput := "added 1 packages\n"
	out4, ok4 := extractPkgSummary(shortInput, "npm install")
	if ok4 {
		t.Errorf("short input: want false when summary not shorter, got true with %q", out4)
	}

	// Error line cap keeps pathological resolver output bounded.
	var capped strings.Builder
	for i := 0; i < 20; i++ {
		capped.WriteString("npm ERR! repeated resolver error\n")
	}
	out5, ok5 := extractPkgSummary(capped.String(), "npm install")
	if !ok5 {
		t.Fatal("expected capped resolver output to compact")
	}
	if strings.Count(out5, "npm ERR!") != 12 {
		t.Fatalf("expected 12 capped error lines, got %q", out5)
	}

	warningSuccess := "npm warn deprecated left-pad@1.3.0\nadded 1 package, and audited 2 packages in 1s\nfound 0 vulnerabilities\n"
	if out6, ok6 := extractPkgSummary(warningSuccess, "npm install"); ok6 {
		t.Fatalf("warning success fallback compacted: %q", out6)
	}
}

func npmInstallCleanFixture(packages int) string {
	var out strings.Builder
	for i := 0; i < packages; i++ {
		fmt.Fprintf(&out, "npm http fetch GET 200 https://registry.npmjs.org/package_%03d 12%dms\n", i, i%10)
		fmt.Fprintf(&out, "npm timing idealTree:node_modules/package_%03d Completed in %dms\n", i, i%20+1)
	}
	fmt.Fprintf(&out, "\nadded %d %s, and audited %d %s in 12s\n\n", packages, pluralWord(packages, "package", "packages"), packages+1, pluralWord(packages+1, "package", "packages"))
	out.WriteString("45 packages are looking for funding\n")
	out.WriteString("  run `npm fund` for details\n\n")
	out.WriteString("found 0 vulnerabilities\n")
	return out.String()
}

func pnpmInstallCleanFixture(packages int, includeDependencies bool) string {
	var out strings.Builder
	out.WriteString("Progress: resolved 1, reused 0, downloaded 0, added 0\n")
	fmt.Fprintf(&out, "Packages: +%d\n", packages)
	out.WriteString(strings.Repeat("+", packages))
	out.WriteString("\n")
	fmt.Fprintf(&out, "Progress: resolved %d, reused %d, downloaded 0, added %d, done\n\n", packages, packages, packages)
	if includeDependencies {
		out.WriteString("dependencies:\n")
		for i := 0; i < packages; i++ {
			fmt.Fprintf(&out, "+ slimference-pnpm-package-%03d 1.0.%d\n", i, i)
		}
		out.WriteString("\n")
	}
	out.WriteString("Done in 256ms using pnpm v10.13.1\n")
	return out.String()
}

func yarnClassicInstallCleanFixture() string {
	return strings.Join([]string{
		"yarn install v1.22.22",
		"info No lockfile found.",
		"[1/4] Resolving packages...",
		"[2/4] Fetching packages...",
		"[3/4] Linking dependencies...",
		"[4/4] Building fresh packages...",
		"success Saved lockfile.",
		"Done in 0.04s.",
		"",
	}, "\n")
}

func yarnClassicUpgradeCleanFixture(packages int) string {
	var out strings.Builder
	out.WriteString("yarn upgrade v1.22.22\n")
	out.WriteString("[1/4] Resolving packages...\n")
	out.WriteString("[2/4] Fetching packages...\n")
	out.WriteString("[3/4] Linking dependencies...\n")
	out.WriteString("[4/4] Rebuilding all packages...\n")
	out.WriteString("success Saved lockfile.\n")
	fmt.Fprintf(&out, "success Saved %d new %s.\n", packages, pluralWord(packages, "dependency", "dependencies"))
	out.WriteString("info Direct dependencies\n")
	for i := 0; i < packages; i++ {
		prefix := "├─"
		if i == packages-1 {
			prefix = "└─"
		}
		fmt.Fprintf(&out, "%s slimference-yarn-package-%03d@1.0.%d\n", prefix, i, i)
	}
	out.WriteString("info All dependencies\n")
	for i := 0; i < packages; i++ {
		prefix := "├─"
		if i == packages-1 {
			prefix = "└─"
		}
		fmt.Fprintf(&out, "%s slimference-yarn-package-%03d@1.0.%d\n", prefix, i, i)
	}
	out.WriteString("Done in 0.04s.\n")
	return out.String()
}

func bunInstallCleanFixture(packages int, savedLockfile bool) string {
	var out strings.Builder
	out.WriteString("bun install v1.3.14 (0d9b296a)\n")
	if savedLockfile {
		out.WriteString("Saved lockfile\n")
	}
	out.WriteString("\n")
	for i := 0; i < packages; i++ {
		fmt.Fprintf(&out, "+ bun-package-%03d@1.0.%d\n", i, i)
	}
	out.WriteString("\n")
	fmt.Fprintf(&out, "%d %s installed [9.00ms]\n", packages, pluralWord(packages, "package", "packages"))
	return out.String()
}

func poetryInstallCleanFixture(packages int) string {
	var out strings.Builder
	out.WriteString("Installing dependencies from lock file\n\n")
	fmt.Fprintf(&out, "Package operations: %d %s, 0 updates, 0 removals\n\n", packages, pluralWord(packages, "install", "installs"))
	for i := 0; i < packages; i++ {
		fmt.Fprintf(&out, "  - Installing package-%03d (1.0.%d)\n", i, i)
	}
	out.WriteString("\nWriting lock file\n")
	return out.String()
}

func uvPackageCleanFixture(packages int, audit bool) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Using CPython 3.12.4 interpreter at: /usr/bin/python3\n")
	fmt.Fprintf(&out, "Resolved %d %s in 23ms\n", packages, pluralWord(packages, "package", "packages"))
	fmt.Fprintf(&out, "Prepared %d %s in 42ms\n", packages, pluralWord(packages, "package", "packages"))
	fmt.Fprintf(&out, "Installed %d %s in 5ms\n", packages, pluralWord(packages, "package", "packages"))
	for i := 0; i < packages; i++ {
		fmt.Fprintf(&out, " + uv-package-%03d==1.0.%d\n", i, i)
	}
	if audit {
		fmt.Fprintf(&out, "Audited %d %s in 1ms\n", packages, pluralWord(packages, "package", "packages"))
	}
	return out.String()
}

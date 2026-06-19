package filter

import (
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

func TestTryCompactPackageOutput_npmProgress(t *testing.T) {
	t.Parallel()
	// Typical npm install output with progress noise and a summary line
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
	if !ok {
		t.Fatalf("expected compact npm install output, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "added 342 packages") {
		t.Errorf("want summary, got: %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact should be shorter: %d vs %d", len(s), len(input))
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
}

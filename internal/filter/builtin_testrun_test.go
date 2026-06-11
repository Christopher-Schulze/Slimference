package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactGoTestJSON(t *testing.T) {
	t.Parallel()
	okStream := "{\"Action\":\"start\",\"Package\":\"p\"}\n{\"Action\":\"run\",\"Package\":\"p\",\"Test\":\"TestX\"}\n{\"Action\":\"pass\",\"Package\":\"p\",\"Test\":\"TestX\"}\n{\"Action\":\"pass\",\"Package\":\"p\"}\n"
	out, ok := TryCompactGoTestJSON([]string{"go", "test", "-json", "./..."}, []byte(okStream))
	if !ok || string(out) != "[go test -json] ok\n" {
		t.Fatalf("compact ok stream: ok=%v %q", ok, out)
	}
	// Fail stream: should compact to failure summary (new behavior: failures ARE compacted).
	failStream := strings.Replace(okStream, `"Action":"pass","Package":"p","Test":"TestX"`, `"Action":"fail","Package":"p","Test":"TestX"`, 1)
	failOut, ok := TryCompactGoTestJSON([]string{"go", "test", "-json", "./..."}, []byte(failStream))
	if !ok {
		t.Fatal("fail stream should be compacted to failure summary")
	}
	if !strings.Contains(string(failOut), "failed") {
		t.Fatalf("failure summary should mention 'failed': %q", failOut)
	}
	if _, ok := TryCompactGoTestJSON([]string{"go", "test", "./..."}, []byte(okStream)); ok {
		t.Fatal("needs -json")
	}
	if _, ok := TryCompactGoTestJSON([]string{"go", "test", "-json"}, []byte("{\"Action\":\"pass\"}\n")); ok {
		t.Fatal("needs at least 2 lines")
	}
	outNpx, ok := TryCompactGoTestJSON([]string{"npx", "-y", "go", "test", "-json", "./..."}, []byte(okStream))
	if !ok || string(outNpx) != "[go test -json] ok\n" {
		t.Fatalf("npx go test -json: ok=%v %q", ok, outNpx)
	}
}

func TestTryCompactGinkgoCtest(t *testing.T) {
	t.Parallel()
	gk, ok := TryCompactGinkgo([]string{"ginkgo", "-r", "."}, []byte(""))
	if !ok || string(gk) != "[ginkgo] ok\n" {
		t.Fatalf("ginkgo: %q", gk)
	}
	ct, ok := TryCompactCtest([]string{"ctest", "--output-on-failure"}, []byte("\n"))
	if !ok || string(ct) != "[ctest] ok\n" {
		t.Fatalf("ctest: %q", ct)
	}
	gkNpx, ok := TryCompactGinkgo([]string{"npx", "-y", "ginkgo", "-r", "."}, []byte(""))
	if !ok || string(gkNpx) != "[ginkgo] ok\n" {
		t.Fatalf("npx -y ginkgo: %q", gkNpx)
	}
	ctNpx, ok := TryCompactCtest([]string{"npx", "ctest", "-N"}, []byte(""))
	if !ok || string(ctNpx) != "[ctest] ok\n" {
		t.Fatalf("npx ctest: %q", ctNpx)
	}
	if _, ok := TryCompactGinkgo([]string{"go", "test"}, []byte("")); ok {
		t.Fatal("go test not ginkgo")
	}
}

func TestTryCompactCargoNextest(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactCargoNextest([]string{"cargo", "nextest", "run"}, []byte(""))
	if !ok || string(out) != "[cargo nextest run] ok\n" {
		t.Fatalf("nextest: ok=%v %q", ok, out)
	}
	var verbose strings.Builder
	verbose.WriteString("    Finished `test` profile [unoptimized + debuginfo] target(s) in 0.01s\n")
	verbose.WriteString("────────────\n")
	verbose.WriteString("    Starting 3 tests across 1 binary\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&verbose, "        PASS [   0.%03ds] slimference::test_%03d\n", i%100, i)
	}
	verbose.WriteString("────────────\n")
	verbose.WriteString("     Summary [   0.088s] 80 tests run: 80 passed, 0 skipped\n")
	out, ok = TryCompactCargoNextest([]string{"cargo", "nextest", "run"}, []byte(verbose.String()))
	if !ok {
		t.Fatal("nextest verbose all-pass should compact")
	}
	nextestOut := string(out)
	if !strings.Contains(nextestOut, "[cargo nextest run] ok - 80 passed") ||
		!strings.Contains(nextestOut, "Summary [   0.088s] 80 tests run: 80 passed") ||
		strings.Contains(nextestOut, "slimference::test_079") {
		t.Fatalf("nextest compaction lost summary or kept roll-call: %q", nextestOut)
	}
	failed := strings.Replace(verbose.String(), "0 skipped", "1 failed, 0 skipped", 1)
	if _, ok := TryCompactCargoNextest([]string{"cargo", "nextest", "run"}, []byte(failed)); ok {
		t.Fatal("nextest failure summary must fail open")
	}
	if _, ok := TryCompactCargoNextest([]string{"cargo", "test"}, []byte("")); ok {
		t.Fatal("not nextest")
	}
	llv, ok := TryCompactCargoLlvmCov([]string{"cargo", "llvm-cov"}, []byte(""))
	if !ok || string(llv) != "[cargo llvm-cov] ok\n" {
		t.Fatalf("llvm-cov: ok=%v %q", ok, llv)
	}
	llv2, ok := TryCompactCargoLlvmCov([]string{"cargo", "llvm-cov", "nextest"}, []byte("\n"))
	if !ok || string(llv2) != "[cargo llvm-cov] ok\n" {
		t.Fatalf("llvm-cov nextest: %q", llv2)
	}
	if _, ok := TryCompactCargoLlvmCov([]string{"cargo", "test"}, []byte("")); ok {
		t.Fatal("cargo test not llvm-cov")
	}
	nxNpx, ok := TryCompactCargoNextest([]string{"npx", "cargo", "nextest", "run"}, []byte("\n"))
	if !ok || string(nxNpx) != "[cargo nextest run] ok\n" {
		t.Fatalf("npx cargo nextest run: %q", nxNpx)
	}
	llvPnpm, ok := TryCompactCargoLlvmCov([]string{"pnpm", "exec", "cargo", "llvm-cov"}, []byte(""))
	if !ok || string(llvPnpm) != "[cargo llvm-cov] ok\n" {
		t.Fatalf("pnpm cargo llvm-cov: %q", llvPnpm)
	}
}

func TestTryCompactGoTest_verboseAllPass(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "=== RUN   TestAlpha%03d\n--- PASS: TestAlpha%03d (0.00s)\n", i, i)
	}
	b.WriteString("=== RUN   TestSkipped\n--- SKIP: TestSkipped (0.00s)\n")
	b.WriteString("    helper_test.go:12: kept log line\n")
	b.WriteString("PASS\nok  \tslimtest/lib\t0.006s\n")

	out, ok := TryCompactGoTest([]string{"go", "test", "./...", "-v"}, []byte(b.String()))
	if !ok {
		t.Fatalf("verbose all-pass go test must compact")
	}
	s := string(out)
	if !strings.Contains(s, "[go test] ok - 120 passed") ||
		!strings.Contains(s, "ok  \tslimtest/lib\t0.006s") ||
		!strings.Contains(s, "--- SKIP: TestSkipped") ||
		!strings.Contains(s, "kept log line") {
		t.Fatalf("compacted output lost evidence: %q", s)
	}
	if strings.Contains(s, "--- PASS: TestAlpha000") || strings.Contains(s, "=== RUN") {
		t.Fatalf("pass roll-call must be elided: %q", s)
	}
	if len(out)*4 > len(b.String()) {
		t.Fatalf("compaction too weak: %d of %d bytes", len(out), len(b.String()))
	}

	failure := "=== RUN   TestX\n--- FAIL: TestX (0.00s)\nFAIL\tslimtest/lib\t0.01s\n"
	if _, ok := TryCompactGoTest([]string{"go", "test", "-v"}, []byte(failure)); ok {
		t.Fatal("failure output must fail open to the full transcript")
	}
	racy := "=== RUN   TestY\nWARNING: DATA RACE\n--- PASS: TestY (0.00s)\nPASS\nok  \tx\t0.01s\n"
	if _, ok := TryCompactGoTest([]string{"go", "test", "-v"}, []byte(racy)); ok {
		t.Fatal("data-race output must fail open")
	}
}

func TestTryCompactTestOutput_goCargo(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactGoTest([]string{"go", "test", "./..."}, []byte("\n\t "))
	if !ok || string(out) != "[go test] ok\n" {
		t.Fatalf("go test: ok=%v %q", ok, out)
	}
	if _, ok := TryCompactGoTest([]string{"go", "build"}, []byte("")); ok {
		t.Fatal("go build not test")
	}
	out2, ok := TryCompactCargoTest([]string{"cargo", "test"}, []byte(""))
	if !ok || string(out2) != "[cargo test] ok\n" {
		t.Fatalf("cargo test: %q", out2)
	}
	out2Yarn, ok := TryCompactCargoTest([]string{"yarn", "cargo", "test"}, []byte("\n"))
	if !ok || string(out2Yarn) != "[cargo test] ok\n" {
		t.Fatalf("yarn cargo test: %q", out2Yarn)
	}
	goNpx, ok := TryCompactGoTest([]string{"npx", "go", "test", "./..."}, []byte(""))
	if !ok || string(goNpx) != "[go test] ok\n" {
		t.Fatalf("npx go test: %q", goNpx)
	}
	out3, ok := TryCompactTestOutput([]string{"go", "test"}, []byte(""))
	if !ok || string(out3) != "[go test] ok\n" {
		t.Fatalf("chain: %q", out3)
	}
	py, ok := TryCompactPytest([]string{"pytest", "-q"}, []byte(""))
	if !ok || string(py) != "[pytest] ok\n" {
		t.Fatalf("pytest: %q", py)
	}
	pyNpx, ok := TryCompactPytest([]string{"npx", "pytest", "-q"}, []byte("\n"))
	if !ok || string(pyNpx) != "[pytest] ok\n" {
		t.Fatalf("npx pytest: %q", pyNpx)
	}
	pyNpxY, ok := TryCompactPytest([]string{"npx", "-y", "pytest"}, []byte(""))
	if !ok || string(pyNpxY) != "[pytest] ok\n" {
		t.Fatalf("npx -y pytest: %q", pyNpxY)
	}
	pyPnpm, ok := TryCompactPytest([]string{"pnpm", "exec", "pytest", "."}, []byte(""))
	if !ok || string(pyPnpm) != "[pytest] ok\n" {
		t.Fatalf("pnpm pytest: %q", pyPnpm)
	}
	pyMod, ok := TryCompactPytest([]string{"python3", "-m", "pytest"}, []byte(""))
	if !ok || string(pyMod) != "[pytest] ok\n" {
		t.Fatalf("python3 -m pytest: %q", pyMod)
	}
	uvPy, ok := TryCompactUvRunPytest([]string{"uv", "run", "pytest", "-q"}, []byte(""))
	if !ok || string(uvPy) != "[uv run pytest] ok\n" {
		t.Fatalf("uv run pytest: %q", uvPy)
	}
	uvPyMod, ok := TryCompactUvRunPytest([]string{"uv", "run", "python", "-m", "pytest"}, []byte(""))
	if !ok || string(uvPyMod) != "[uv run pytest] ok\n" {
		t.Fatalf("uv run python -m pytest: %q", uvPyMod)
	}
	poPy, ok := TryCompactPoetryRunPytest([]string{"poetry", "run", "pytest"}, []byte(""))
	if !ok || string(poPy) != "[poetry run pytest] ok\n" {
		t.Fatalf("poetry run pytest: %q", poPy)
	}
	poPyMod, ok := TryCompactPoetryRunPytest([]string{"poetry", "run", "python3", "-m", "pytest"}, []byte("\n"))
	if !ok || string(poPyMod) != "[poetry run pytest] ok\n" {
		t.Fatalf("poetry run python -m pytest: %q", poPyMod)
	}
	uvPyNpx, ok := TryCompactUvRunPytest([]string{"npx", "uv", "run", "pytest"}, []byte(""))
	if !ok || string(uvPyNpx) != "[uv run pytest] ok\n" {
		t.Fatalf("npx uv run pytest: %q", uvPyNpx)
	}
	uvPyPnpm, ok := TryCompactUvRunPytest([]string{"pnpm", "exec", "uv", "run", "python", "-m", "pytest"}, []byte(""))
	if !ok || string(uvPyPnpm) != "[uv run pytest] ok\n" {
		t.Fatalf("pnpm uv run python -m pytest: %q", uvPyPnpm)
	}
	poPyYarn, ok := TryCompactPoetryRunPytest([]string{"yarn", "poetry", "run", "pytest"}, []byte("\n"))
	if !ok || string(poPyYarn) != "[poetry run pytest] ok\n" {
		t.Fatalf("yarn poetry run pytest: %q", poPyYarn)
	}
	ht, ok := TryCompactHatchTest([]string{"hatch", "test"}, []byte(""))
	if !ok || string(ht) != "[hatch test] ok\n" {
		t.Fatalf("hatch test: %q", ht)
	}
	htNpx, ok := TryCompactHatchTest([]string{"npx", "-y", "hatch", "test"}, []byte(""))
	if !ok || string(htNpx) != "[hatch test] ok\n" {
		t.Fatalf("npx -y hatch test: %q", htNpx)
	}
	noxT, ok := TryCompactNoxTest([]string{"nox", "-s", "test"}, []byte(""))
	if !ok || string(noxT) != "[nox test] ok\n" {
		t.Fatalf("nox -s test: %q", noxT)
	}
	noxT2, ok := TryCompactNoxTest([]string{"nox", "--session=test"}, []byte("\n"))
	if !ok || string(noxT2) != "[nox test] ok\n" {
		t.Fatalf("nox --session=test: %q", noxT2)
	}
	noxNpx, ok := TryCompactNoxTest([]string{"npx", "-y", "nox", "-s", "test"}, []byte(""))
	if !ok || string(noxNpx) != "[nox test] ok\n" {
		t.Fatalf("npx -y nox -s test: %q", noxNpx)
	}
	noxPnpm, ok := TryCompactNoxTest([]string{"pnpm", "exec", "nox", "-s", "test"}, []byte(""))
	if !ok || string(noxPnpm) != "[nox test] ok\n" {
		t.Fatalf("pnpm exec nox -s test: %q", noxPnpm)
	}
	noxYarn, ok := TryCompactNoxTest([]string{"yarn", "nox", "-s", "test"}, []byte(""))
	if !ok || string(noxYarn) != "[nox test] ok\n" {
		t.Fatalf("yarn nox -s test: %q", noxYarn)
	}
	if _, ok := TryCompactNoxTest([]string{"nox", "-s", "lint"}, []byte("")); ok {
		t.Fatal("nox -s lint not test")
	}
	pu2, ok := TryCompactPythonUnittest([]string{"python3", "-m", "unittest", "discover"}, []byte(""))
	if !ok || string(pu2) != "[python -m unittest] ok\n" {
		t.Fatalf("python -m unittest: %q", pu2)
	}
	puUniNpx, ok := TryCompactPythonUnittest([]string{"npx", "-y", "python3", "-m", "unittest"}, []byte(""))
	if !ok || string(puUniNpx) != "[python -m unittest] ok\n" {
		t.Fatalf("npx python -m unittest: %q", puUniNpx)
	}
	puUniPnpm, ok := TryCompactPythonUnittest([]string{"pnpm", "exec", "python3", "-m", "unittest", "discover"}, []byte("\n"))
	if !ok || string(puUniPnpm) != "[python -m unittest] ok\n" {
		t.Fatalf("pnpm exec python -m unittest: %q", puUniPnpm)
	}
	puUniYarn, ok := TryCompactPythonUnittest([]string{"yarn", "python3", "-m", "unittest"}, []byte(""))
	if !ok || string(puUniYarn) != "[python -m unittest] ok\n" {
		t.Fatalf("yarn python -m unittest: %q", puUniYarn)
	}
	pu, ok := TryCompactPhpunit([]string{"phpunit", "tests/"}, []byte(""))
	if !ok || string(pu) != "[phpunit] ok\n" {
		t.Fatalf("phpunit: %q", pu)
	}
	puNpx, ok := TryCompactPhpunit([]string{"npx", "-y", "phpunit"}, []byte("\n"))
	if !ok || string(puNpx) != "[phpunit] ok\n" {
		t.Fatalf("npx phpunit: %q", puNpx)
	}
	rt, ok := TryCompactRailsTest([]string{"rails", "test"}, []byte(""))
	if !ok || string(rt) != "[rails test] ok\n" {
		t.Fatalf("rails test: %q", rt)
	}
	rtb, ok := TryCompactRailsTest([]string{"bundle", "exec", "rails", "test"}, []byte("\n"))
	if !ok || string(rtb) != "[rails test] ok\n" {
		t.Fatalf("bundle exec rails test: %q", rtb)
	}
	rtNpx, ok := TryCompactRailsTest([]string{"npx", "-y", "rails", "test"}, []byte(""))
	if !ok || string(rtNpx) != "[rails test] ok\n" {
		t.Fatalf("npx -y rails test: %q", rtNpx)
	}
	rtPnpm, ok := TryCompactRailsTest([]string{"pnpm", "exec", "rails", "test"}, []byte(""))
	if !ok || string(rtPnpm) != "[rails test] ok\n" {
		t.Fatalf("pnpm exec rails test: %q", rtPnpm)
	}
	rtNpxBundle, ok := TryCompactRailsTest([]string{"npx", "bundle", "exec", "rails", "test"}, []byte(""))
	if !ok || string(rtNpxBundle) != "[rails test] ok\n" {
		t.Fatalf("npx bundle exec rails test: %q", rtNpxBundle)
	}
	rtYarnBundle, ok := TryCompactRailsTest([]string{"yarn", "bundle", "exec", "rails", "test"}, []byte("\n"))
	if !ok || string(rtYarnBundle) != "[rails test] ok\n" {
		t.Fatalf("yarn bundle exec rails test: %q", rtYarnBundle)
	}
	gt, ok := TryCompactGradleTest([]string{"gradlew", "test"}, []byte(""))
	if !ok || string(gt) != "[gradle test] ok\n" {
		t.Fatalf("gradle test: %q", gt)
	}
	gtNpx, ok := TryCompactGradleTest([]string{"npx", "-y", "gradlew", "test"}, []byte(""))
	if !ok || string(gtNpx) != "[gradle test] ok\n" {
		t.Fatalf("npx -y gradlew test: %q", gtNpx)
	}
	gtPnpm, ok := TryCompactGradleTest([]string{"pnpm", "exec", "gradle", "test"}, []byte("\n"))
	if !ok || string(gtPnpm) != "[gradle test] ok\n" {
		t.Fatalf("pnpm exec gradle test: %q", gtPnpm)
	}
	st, ok := TryCompactSbtTest([]string{"sbt", "-batch", "test"}, []byte(""))
	if !ok || string(st) != "[sbt test] ok\n" {
		t.Fatalf("sbt test: %q", st)
	}
	stNpx, ok := TryCompactSbtTest([]string{"npx", "sbt", "test"}, []byte(""))
	if !ok || string(stNpx) != "[sbt test] ok\n" {
		t.Fatalf("npx sbt test: %q", stNpx)
	}
	mt, ok := TryCompactMillTest([]string{"mill", "test"}, []byte(""))
	if !ok || string(mt) != "[mill test] ok\n" {
		t.Fatalf("mill test: %q", mt)
	}
	mtNpx, ok := TryCompactMillTest([]string{"npx", "mill", "test"}, []byte("\n"))
	if !ok || string(mtNpx) != "[mill test] ok\n" {
		t.Fatalf("npx mill test: %q", mtNpx)
	}
	vi, ok := TryCompactVitest([]string{"vitest", "run"}, []byte(""))
	if !ok || string(vi) != "[vitest] ok\n" {
		t.Fatalf("vitest: %q", vi)
	}
	viNpx, ok := TryCompactVitest([]string{"npx", "vitest", "run"}, []byte(""))
	if !ok || string(viNpx) != "[vitest] ok\n" {
		t.Fatalf("npx vitest: %q", viNpx)
	}
	viPnpm, ok := TryCompactVitest([]string{"pnpm", "exec", "vitest"}, []byte("\n"))
	if !ok || string(viPnpm) != "[vitest] ok\n" {
		t.Fatalf("pnpm exec vitest: %q", viPnpm)
	}
	viYarn, ok := TryCompactVitest([]string{"yarn", "vitest"}, []byte(""))
	if !ok || string(viYarn) != "[vitest] ok\n" {
		t.Fatalf("yarn vitest: %q", viYarn)
	}
	if _, ok := TryCompactVitest([]string{"yarn", "test"}, []byte("")); ok {
		t.Fatal("yarn test not vitest")
	}
	llvChain, ok := TryCompactTestOutput([]string{"cargo", "llvm-cov"}, []byte(""))
	if !ok || string(llvChain) != "[cargo llvm-cov] ok\n" {
		t.Fatalf("chain llvm-cov: %q", llvChain)
	}
	kar, ok := TryCompactKarma([]string{"karma", "start"}, []byte(""))
	if !ok || string(kar) != "[karma] ok\n" {
		t.Fatalf("karma: %q", kar)
	}
	karNpx, ok := TryCompactKarma([]string{"npx", "karma", "start"}, []byte(""))
	if !ok || string(karNpx) != "[karma] ok\n" {
		t.Fatalf("npx karma start: %q", karNpx)
	}
	karPnpm, ok := TryCompactKarma([]string{"pnpm", "exec", "karma", "start"}, []byte("\n"))
	if !ok || string(karPnpm) != "[karma] ok\n" {
		t.Fatalf("pnpm karma start: %q", karPnpm)
	}
	jest, ok := TryCompactJest([]string{"jest", "--passWithNoTests"}, []byte(""))
	if !ok || string(jest) != "[jest] ok\n" {
		t.Fatalf("jest: %q", jest)
	}
	jestNpx, ok := TryCompactJest([]string{"npx", "jest"}, []byte("\n"))
	if !ok || string(jestNpx) != "[jest] ok\n" {
		t.Fatalf("npx jest: %q", jestNpx)
	}
	jestPnpm, ok := TryCompactJest([]string{"pnpm", "exec", "jest"}, []byte(""))
	if !ok || string(jestPnpm) != "[jest] ok\n" {
		t.Fatalf("pnpm jest: %q", jestPnpm)
	}
	jestYarn, ok := TryCompactJest([]string{"yarn", "jest"}, []byte(""))
	if !ok || string(jestYarn) != "[jest] ok\n" {
		t.Fatalf("yarn jest: %q", jestYarn)
	}
	mocha, ok := TryCompactMocha([]string{"mocha", "test/"}, []byte(""))
	if !ok || string(mocha) != "[mocha] ok\n" {
		t.Fatalf("mocha: %q", mocha)
	}
	mochaNpx, ok := TryCompactMocha([]string{"npx", "mocha"}, []byte(""))
	if !ok || string(mochaNpx) != "[mocha] ok\n" {
		t.Fatalf("npx mocha: %q", mochaNpx)
	}
	mochaPnpm, ok := TryCompactMocha([]string{"pnpm", "exec", "mocha"}, []byte("\n"))
	if !ok || string(mochaPnpm) != "[mocha] ok\n" {
		t.Fatalf("pnpm mocha: %q", mochaPnpm)
	}
	ava, ok := TryCompactAva([]string{"ava"}, []byte(""))
	if !ok || string(ava) != "[ava] ok\n" {
		t.Fatalf("ava: %q", ava)
	}
	avaNpx, ok := TryCompactAva([]string{"npx", "ava"}, []byte("\n"))
	if !ok || string(avaNpx) != "[ava] ok\n" {
		t.Fatalf("npx ava: %q", avaNpx)
	}
	avaYarn, ok := TryCompactAva([]string{"yarn", "ava"}, []byte(""))
	if !ok || string(avaYarn) != "[ava] ok\n" {
		t.Fatalf("yarn ava: %q", avaYarn)
	}
	tapOut, ok := TryCompactTap([]string{"tap", "test/*.js"}, []byte(""))
	if !ok || string(tapOut) != "[tap] ok\n" {
		t.Fatalf("tap: %q", tapOut)
	}
	tapNpx, ok := TryCompactTap([]string{"npx", "tap"}, []byte(""))
	if !ok || string(tapNpx) != "[tap] ok\n" {
		t.Fatalf("npx tap: %q", tapNpx)
	}
	tapPnpm, ok := TryCompactTap([]string{"pnpm", "exec", "tap"}, []byte(""))
	if !ok || string(tapPnpm) != "[tap] ok\n" {
		t.Fatalf("pnpm tap: %q", tapPnpm)
	}
	pw, ok := TryCompactPlaywrightTest([]string{"playwright", "test"}, []byte(""))
	if !ok || string(pw) != "[playwright test] ok\n" {
		t.Fatalf("playwright: %q", pw)
	}
	pwNpx, ok := TryCompactPlaywrightTest([]string{"npx", "playwright", "test"}, []byte("\n"))
	if !ok || string(pwNpx) != "[playwright test] ok\n" {
		t.Fatalf("npx playwright test: %q", pwNpx)
	}
	pwPnpm, ok := TryCompactPlaywrightTest([]string{"pnpm", "exec", "playwright", "test"}, []byte(""))
	if !ok || string(pwPnpm) != "[playwright test] ok\n" {
		t.Fatalf("pnpm playwright: %q", pwPnpm)
	}
	dt, ok := TryCompactDartTest([]string{"dart", "test"}, []byte(""))
	if !ok || string(dt) != "[dart test] ok\n" {
		t.Fatalf("dart test: %q", dt)
	}
	dtFvm, ok := TryCompactDartTest([]string{"fvm", "dart", "test"}, []byte("\n"))
	if !ok || string(dtFvm) != "[dart test] ok\n" {
		t.Fatalf("fvm dart test: %q", dtFvm)
	}
	dtNpx, ok := TryCompactDartTest([]string{"npx", "-y", "dart", "test"}, []byte(""))
	if !ok || string(dtNpx) != "[dart test] ok\n" {
		t.Fatalf("npx -y dart test: %q", dtNpx)
	}
	dtNpxFvm, ok := TryCompactDartTest([]string{"npx", "fvm", "dart", "test"}, []byte(""))
	if !ok || string(dtNpxFvm) != "[dart test] ok\n" {
		t.Fatalf("npx fvm dart test: %q", dtNpxFvm)
	}
	dtPnpm, ok := TryCompactDartTest([]string{"pnpm", "exec", "dart", "test"}, []byte(""))
	if !ok || string(dtPnpm) != "[dart test] ok\n" {
		t.Fatalf("pnpm exec dart test: %q", dtPnpm)
	}
	dtYarn, ok := TryCompactDartTest([]string{"yarn", "dart", "test"}, []byte("\n"))
	if !ok || string(dtYarn) != "[dart test] ok\n" {
		t.Fatalf("yarn dart test: %q", dtYarn)
	}
	ft, ok := TryCompactFlutterTest([]string{"flutter", "test"}, []byte(""))
	if !ok || string(ft) != "[flutter test] ok\n" {
		t.Fatalf("flutter test: %q", ft)
	}
	ftFvm, ok := TryCompactFlutterTest([]string{"fvm", "flutter", "test"}, []byte("\n"))
	if !ok || string(ftFvm) != "[flutter test] ok\n" {
		t.Fatalf("fvm flutter test: %q", ftFvm)
	}
	ftNpx, ok := TryCompactFlutterTest([]string{"npx", "flutter", "test"}, []byte(""))
	if !ok || string(ftNpx) != "[flutter test] ok\n" {
		t.Fatalf("npx flutter test: %q", ftNpx)
	}
	ftNpxFvm, ok := TryCompactFlutterTest([]string{"npx", "-y", "fvm", "flutter", "test"}, []byte(""))
	if !ok || string(ftNpxFvm) != "[flutter test] ok\n" {
		t.Fatalf("npx -y fvm flutter test: %q", ftNpxFvm)
	}
	ftPnpm, ok := TryCompactFlutterTest([]string{"pnpm", "exec", "flutter", "test"}, []byte(""))
	if !ok || string(ftPnpm) != "[flutter test] ok\n" {
		t.Fatalf("pnpm exec flutter test: %q", ftPnpm)
	}
	ftYarn, ok := TryCompactFlutterTest([]string{"yarn", "flutter", "test"}, []byte("\n"))
	if !ok || string(ftYarn) != "[flutter test] ok\n" {
		t.Fatalf("yarn flutter test: %q", ftYarn)
	}
	elm, ok := TryCompactElmTest([]string{"elm-test"}, []byte(""))
	if !ok || string(elm) != "[elm-test] ok\n" {
		t.Fatalf("elm-test: %q", elm)
	}
	elmNpx, ok := TryCompactElmTest([]string{"npx", "elm-test"}, []byte("\n"))
	if !ok || string(elmNpx) != "[elm-test] ok\n" {
		t.Fatalf("npx elm-test: %q", elmNpx)
	}
	elmNpxY, ok := TryCompactElmTest([]string{"npx", "-y", "elm-test"}, []byte(""))
	if !ok || string(elmNpxY) != "[elm-test] ok\n" {
		t.Fatalf("npx -y elm-test: %q", elmNpxY)
	}
	elmPnpm, ok := TryCompactElmTest([]string{"pnpm", "exec", "elm-test"}, []byte(""))
	if !ok || string(elmPnpm) != "[elm-test] ok\n" {
		t.Fatalf("pnpm elm-test: %q", elmPnpm)
	}
	denoT, ok := TryCompactDenoTest([]string{"deno", "test"}, []byte(""))
	if !ok || string(denoT) != "[deno test] ok\n" {
		t.Fatalf("deno test: %q", denoT)
	}
	denoTNpxY, ok := TryCompactDenoTest([]string{"npx", "-y", "deno", "test"}, []byte(""))
	if !ok || string(denoTNpxY) != "[deno test] ok\n" {
		t.Fatalf("npx -y deno test: %q", denoTNpxY)
	}
	denoPnpm, ok := TryCompactDenoTest([]string{"pnpm", "exec", "deno", "test"}, []byte(""))
	if !ok || string(denoPnpm) != "[deno test] ok\n" {
		t.Fatalf("pnpm exec deno test: %q", denoPnpm)
	}
	denoYarn, ok := TryCompactDenoTest([]string{"yarn", "deno", "test"}, []byte("\n"))
	if !ok || string(denoYarn) != "[deno test] ok\n" {
		t.Fatalf("yarn deno test: %q", denoYarn)
	}
	cy, ok := TryCompactCypressRun([]string{"cypress", "run"}, []byte(""))
	if !ok || string(cy) != "[cypress run] ok\n" {
		t.Fatalf("cypress: %q", cy)
	}
	cyNpx, ok := TryCompactCypressRun([]string{"npx", "cypress", "run"}, []byte("\n"))
	if !ok || string(cyNpx) != "[cypress run] ok\n" {
		t.Fatalf("npx cypress: %q", cyNpx)
	}
	cyPnpm, ok := TryCompactCypressRun([]string{"pnpm", "exec", "cypress", "run"}, []byte(""))
	if !ok || string(cyPnpm) != "[cypress run] ok\n" {
		t.Fatalf("pnpm cypress: %q", cyPnpm)
	}
	wdio, ok := TryCompactWdioRun([]string{"wdio", "run", "wdio.conf.ts"}, []byte(""))
	if !ok || string(wdio) != "[wdio run] ok\n" {
		t.Fatalf("wdio run: %q", wdio)
	}
	wdioNpx, ok := TryCompactWdioRun([]string{"npx", "wdio", "run", "wdio.conf.js"}, []byte("\n"))
	if !ok || string(wdioNpx) != "[wdio run] ok\n" {
		t.Fatalf("npx wdio run: %q", wdioNpx)
	}
	nxT, ok := TryCompactNxTest([]string{"nx", "test", "app"}, []byte(""))
	if !ok || string(nxT) != "[nx test] ok\n" {
		t.Fatalf("nx test: %q", nxT)
	}
	nxTNpx, ok := TryCompactNxTest([]string{"npx", "nx", "test", "app"}, []byte(""))
	if !ok || string(nxTNpx) != "[nx test] ok\n" {
		t.Fatalf("npx nx test: %q", nxTNpx)
	}
	nxTNpxY, ok := TryCompactNxTest([]string{"npx", "-y", "nx", "test", "app"}, []byte(""))
	if !ok || string(nxTNpxY) != "[nx test] ok\n" {
		t.Fatalf("npx -y nx test: %q", nxTNpxY)
	}
	turboT, ok := TryCompactTurboTest([]string{"turbo", "run", "test"}, []byte(""))
	if !ok || string(turboT) != "[turbo test] ok\n" {
		t.Fatalf("turbo run test: %q", turboT)
	}
	turboT2, ok := TryCompactTurboTest([]string{"turbo", "test"}, []byte("\n"))
	if !ok || string(turboT2) != "[turbo test] ok\n" {
		t.Fatalf("turbo test: %q", turboT2)
	}
	turboNpx, ok := TryCompactTurboTest([]string{"npx", "turbo", "run", "test"}, []byte(""))
	if !ok || string(turboNpx) != "[turbo test] ok\n" {
		t.Fatalf("npx turbo run test: %q", turboNpx)
	}
	npmT, ok := TryCompactNpmRunTest([]string{"npm", "run", "test"}, []byte(""))
	if !ok || string(npmT) != "[npm run test] ok\n" {
		t.Fatalf("npm test: %q", npmT)
	}
	pnpmT, ok := TryCompactPnpmTest([]string{"pnpm", "test"}, []byte(""))
	if !ok || string(pnpmT) != "[pnpm test] ok\n" {
		t.Fatalf("pnpm: %q", pnpmT)
	}
	yarnT, ok := TryCompactYarnTest([]string{"yarn", "test"}, []byte(""))
	if !ok || string(yarnT) != "[yarn test] ok\n" {
		t.Fatalf("yarn: %q", yarnT)
	}
	bunT, ok := TryCompactBunTest([]string{"bun", "test"}, []byte(""))
	if !ok || string(bunT) != "[bun test] ok\n" {
		t.Fatalf("bun test: %q", bunT)
	}
	bunNpx, ok := TryCompactBunTest([]string{"npx", "-y", "bun", "test"}, []byte("\n"))
	if !ok || string(bunNpx) != "[bun test] ok\n" {
		t.Fatalf("npx -y bun test: %q", bunNpx)
	}
	bunPnpm, ok := TryCompactBunTest([]string{"pnpm", "exec", "bun", "test"}, []byte(""))
	if !ok || string(bunPnpm) != "[bun test] ok\n" {
		t.Fatalf("pnpm exec bun test: %q", bunPnpm)
	}
	bunYarn, ok := TryCompactBunTest([]string{"yarn", "bun", "test"}, []byte(""))
	if !ok || string(bunYarn) != "[bun test] ok\n" {
		t.Fatalf("yarn bun test: %q", bunYarn)
	}
	nxTPnpm, ok := TryCompactNxTest([]string{"pnpm", "exec", "nx", "test", "app"}, []byte(""))
	if !ok || string(nxTPnpm) != "[nx test] ok\n" {
		t.Fatalf("pnpm exec nx test: %q", nxTPnpm)
	}
	nxTYarn, ok := TryCompactNxTest([]string{"yarn", "nx", "test", "lib"}, []byte(""))
	if !ok || string(nxTYarn) != "[nx test] ok\n" {
		t.Fatalf("yarn nx test: %q", nxTYarn)
	}
	turboTPnpm, ok := TryCompactTurboTest([]string{"pnpm", "exec", "turbo", "run", "test"}, []byte(""))
	if !ok || string(turboTPnpm) != "[turbo test] ok\n" {
		t.Fatalf("pnpm exec turbo run test: %q", turboTPnpm)
	}
	turboTPnpm2, ok := TryCompactTurboTest([]string{"pnpm", "exec", "turbo", "test"}, []byte(""))
	if !ok || string(turboTPnpm2) != "[turbo test] ok\n" {
		t.Fatalf("pnpm exec turbo test: %q", turboTPnpm2)
	}
	turboTYarn, ok := TryCompactTurboTest([]string{"yarn", "turbo", "run", "test"}, []byte(""))
	if !ok || string(turboTYarn) != "[turbo test] ok\n" {
		t.Fatalf("yarn turbo run test: %q", turboTYarn)
	}
	turboTYarn2, ok := TryCompactTurboTest([]string{"yarnpkg", "turbo", "test"}, []byte(""))
	if !ok || string(turboTYarn2) != "[turbo test] ok\n" {
		t.Fatalf("yarnpkg turbo test: %q", turboTYarn2)
	}
}

// TestTryCompactTestOutput_missingWrappers covers pnpm exec / yarn / npx and
// guard branches not exercised in the primary test function.
func TestTryCompactTestOutput_missingWrappers(t *testing.T) {
	t.Parallel()

	// --- non-empty stdout guards ---
	if _, ok := TryCompactGoTest([]string{"go", "test", "./..."}, []byte("FAIL\n")); ok {
		t.Fatal("go test non-empty stdout")
	}
	if _, ok := TryCompactCargoTest([]string{"cargo", "test"}, []byte("FAILED\n")); ok {
		t.Fatal("cargo test non-empty stdout")
	}
	if _, ok := TryCompactCargoNextest([]string{"cargo", "nextest", "run"}, []byte("FAILED\n")); ok {
		t.Fatal("cargo nextest non-empty stdout")
	}
	if _, ok := TryCompactCargoLlvmCov([]string{"cargo", "llvm-cov"}, []byte("FAILED\n")); ok {
		t.Fatal("cargo llvm-cov non-empty stdout")
	}
	if _, ok := TryCompactPythonUnittest([]string{"python3", "-m", "unittest"}, []byte("FAILED\n")); ok {
		t.Fatal("python -m unittest non-empty stdout")
	}
	if _, ok := TryCompactNpmRunTest([]string{"npm", "run", "test"}, []byte("FAILED\n")); ok {
		t.Fatal("npm run test non-empty stdout")
	}
	if _, ok := TryCompactPnpmTest([]string{"pnpm", "test"}, []byte("FAILED\n")); ok {
		t.Fatal("pnpm test non-empty stdout")
	}
	if _, ok := TryCompactYarnTest([]string{"yarn", "test"}, []byte("FAILED\n")); ok {
		t.Fatal("yarn test non-empty stdout")
	}
	if _, ok := TryCompactNoxTest([]string{"nox", "-s", "test"}, []byte("FAILED\n")); ok {
		t.Fatal("nox non-empty stdout")
	}

	// --- GoTestJSON edge cases ---
	// empty s="" branch (L15): TryCompactGoTestJSON with first line empty
	if _, ok := TryCompactGoTestJSON([]string{"go", "test", "-json", "./..."}, []byte("\n{\"Action\":\"pass\"}\n")); ok {
		t.Fatal("go test -json: empty first line should fail or not match")
	}
	// invalid JSON line (L28)
	if _, ok := TryCompactGoTestJSON([]string{"go", "test", "-json", "./..."}, []byte("{\"Action\":\"pass\"}\nnot-json\n")); ok {
		t.Fatal("go test -json: invalid JSON line should not compact")
	}

	// --- NpmRunTest: non-run argv check (L497) ---
	if _, ok := TryCompactNpmRunTest([]string{"npm", "build"}, []byte("")); ok {
		t.Fatal("npm build not npm run test")
	}

	// --- isGoTestArgv: npx failure (L85) + pnpm (L90) ---
	if _, ok := TryCompactGoTest([]string{"npx", "go"}, []byte("")); ok {
		t.Fatal("npx go: too short for go test")
	}
	goTestPnpm, ok := TryCompactGoTest([]string{"pnpm", "exec", "go", "test", "./..."}, []byte(""))
	if !ok || string(goTestPnpm) != "[go test] ok\n" {
		t.Fatalf("pnpm go test: %q", goTestPnpm)
	}

	// --- isCargoTestArgv: npx full block (L112/117) + pnpm (L119) ---
	ctNpx, ok := TryCompactCargoTest([]string{"npx", "cargo", "test"}, []byte(""))
	if !ok || string(ctNpx) != "[cargo test] ok\n" {
		t.Fatalf("npx cargo test: %q", ctNpx)
	}
	if _, ok := TryCompactCargoTest([]string{"npx", "cargo"}, []byte("")); ok {
		t.Fatal("npx cargo: too short for test")
	}
	ctPnpm, ok := TryCompactCargoTest([]string{"pnpm", "exec", "cargo", "test"}, []byte(""))
	if !ok || string(ctPnpm) != "[cargo test] ok\n" {
		t.Fatalf("pnpm cargo test: %q", ctPnpm)
	}

	// --- isCargoNextestRunArgv: npx failure (L149) + pnpm (L154) + yarn (L157) ---
	if _, ok := TryCompactCargoNextest([]string{"npx", "cargo", "nextest"}, []byte("")); ok {
		t.Fatal("npx cargo nextest: too short (needs run)")
	}
	nxPnpm, ok := TryCompactCargoNextest([]string{"pnpm", "exec", "cargo", "nextest", "run"}, []byte(""))
	if !ok || string(nxPnpm) != "[cargo nextest run] ok\n" {
		t.Fatalf("pnpm cargo nextest run: %q", nxPnpm)
	}
	nxYarn, ok := TryCompactCargoNextest([]string{"yarn", "cargo", "nextest", "run"}, []byte(""))
	if !ok || string(nxYarn) != "[cargo nextest run] ok\n" {
		t.Fatalf("yarn cargo nextest run: %q", nxYarn)
	}

	// --- isCargoLlvmCovArgv: npx block (L182/187) ---
	llvNpx, ok := TryCompactCargoLlvmCov([]string{"npx", "cargo", "llvm-cov"}, []byte(""))
	if !ok || string(llvNpx) != "[cargo llvm-cov] ok\n" {
		t.Fatalf("npx cargo llvm-cov: %q", llvNpx)
	}
	if _, ok := TryCompactCargoLlvmCov([]string{"npx", "cargo"}, []byte("")); ok {
		t.Fatal("npx cargo: too short for llvm-cov")
	}

	// --- Pytest: yarn + len<1 ---
	pyYarn, ok := TryCompactPytest([]string{"yarn", "pytest", "tests/"}, []byte(""))
	if !ok || string(pyYarn) != "[pytest] ok\n" {
		t.Fatalf("yarn pytest: %q", pyYarn)
	}
	if _, ok := TryCompactPytest([]string{}, []byte("")); ok {
		t.Fatal("pytest empty argv")
	}

	// --- Phpunit: pnpm + yarn ---
	phPnpm, ok := TryCompactPhpunit([]string{"pnpm", "exec", "phpunit"}, []byte(""))
	if !ok || string(phPnpm) != "[phpunit] ok\n" {
		t.Fatalf("pnpm phpunit: %q", phPnpm)
	}
	phYarn, ok := TryCompactPhpunit([]string{"yarn", "phpunit"}, []byte(""))
	if !ok || string(phYarn) != "[phpunit] ok\n" {
		t.Fatalf("yarn phpunit: %q", phYarn)
	}
	if _, ok := TryCompactPhpunit([]string{}, []byte("")); ok {
		t.Fatal("phpunit empty argv")
	}

	// --- Vitest: len<1 ---
	if _, ok := TryCompactVitest([]string{}, []byte("")); ok {
		t.Fatal("vitest empty argv")
	}

	// --- Karma: yarn ---
	karmaYarn, ok := TryCompactKarma([]string{"yarn", "karma", "start"}, []byte(""))
	if !ok || string(karmaYarn) != "[karma] ok\n" {
		t.Fatalf("yarn karma: %q", karmaYarn)
	}

	// --- Jest: len<1 ---
	if _, ok := TryCompactJest([]string{}, []byte("")); ok {
		t.Fatal("jest empty argv")
	}

	// --- Mocha: yarn + len<1 ---
	mochaYarn, ok := TryCompactMocha([]string{"yarn", "mocha"}, []byte(""))
	if !ok || string(mochaYarn) != "[mocha] ok\n" {
		t.Fatalf("yarn mocha: %q", mochaYarn)
	}
	if _, ok := TryCompactMocha([]string{}, []byte("")); ok {
		t.Fatal("mocha empty argv")
	}

	// --- Ava: pnpm + len<1 ---
	avaPnpm, ok := TryCompactAva([]string{"pnpm", "exec", "ava"}, []byte(""))
	if !ok || string(avaPnpm) != "[ava] ok\n" {
		t.Fatalf("pnpm ava: %q", avaPnpm)
	}
	if _, ok := TryCompactAva([]string{}, []byte("")); ok {
		t.Fatal("ava empty argv")
	}

	// --- Tap: yarn + len<1 ---
	tapYarn, ok := TryCompactTap([]string{"yarn", "tap"}, []byte(""))
	if !ok || string(tapYarn) != "[tap] ok\n" {
		t.Fatalf("yarn tap: %q", tapYarn)
	}
	if _, ok := TryCompactTap([]string{}, []byte("")); ok {
		t.Fatal("tap empty argv")
	}

	// --- PlaywrightTest: yarn ---
	pwYarn, ok := TryCompactPlaywrightTest([]string{"yarn", "playwright", "test"}, []byte(""))
	if !ok || string(pwYarn) != "[playwright test] ok\n" {
		t.Fatalf("yarn playwright test: %q", pwYarn)
	}

	// --- CypressRun: yarn ---
	cyYarn, ok := TryCompactCypressRun([]string{"yarn", "cypress", "run"}, []byte(""))
	if !ok || string(cyYarn) != "[cypress run] ok\n" {
		t.Fatalf("yarn cypress run: %q", cyYarn)
	}

	// --- WdioRun: pnpm + yarn ---
	wdPnpm, ok := TryCompactWdioRun([]string{"pnpm", "exec", "wdio", "run", "wdio.conf.ts"}, []byte(""))
	if !ok || string(wdPnpm) != "[wdio run] ok\n" {
		t.Fatalf("pnpm wdio run: %q", wdPnpm)
	}
	wdYarn, ok := TryCompactWdioRun([]string{"yarn", "wdio", "run", "wdio.conf.ts"}, []byte(""))
	if !ok || string(wdYarn) != "[wdio run] ok\n" {
		t.Fatalf("yarn wdio run: %q", wdYarn)
	}

	// --- ElmTest: yarn + len<1 ---
	elmYarn, ok := TryCompactElmTest([]string{"yarn", "elm-test"}, []byte(""))
	if !ok || string(elmYarn) != "[elm-test] ok\n" {
		t.Fatalf("yarn elm-test: %q", elmYarn)
	}
	if _, ok := TryCompactElmTest([]string{}, []byte("")); ok {
		t.Fatal("elm-test empty argv")
	}

	// --- GradleTest: yarn ---
	grTestYarn, ok := TryCompactGradleTest([]string{"yarn", "gradlew", "test"}, []byte(""))
	if !ok || string(grTestYarn) != "[gradle test] ok\n" {
		t.Fatalf("yarn gradlew test: %q", grTestYarn)
	}

	// --- SbtTest: pnpm + yarn ---
	sbtPnpm, ok := TryCompactSbtTest([]string{"pnpm", "exec", "sbt", "test"}, []byte(""))
	if !ok || string(sbtPnpm) != "[sbt test] ok\n" {
		t.Fatalf("pnpm sbt test: %q", sbtPnpm)
	}
	sbtYarn, ok := TryCompactSbtTest([]string{"yarn", "sbt", "test"}, []byte(""))
	if !ok || string(sbtYarn) != "[sbt test] ok\n" {
		t.Fatalf("yarn sbt test: %q", sbtYarn)
	}

	// --- MillTest: pnpm + yarn ---
	millPnpm, ok := TryCompactMillTest([]string{"pnpm", "exec", "mill", "test"}, []byte(""))
	if !ok || string(millPnpm) != "[mill test] ok\n" {
		t.Fatalf("pnpm mill test: %q", millPnpm)
	}
	millYarn, ok := TryCompactMillTest([]string{"yarn", "mill", "test"}, []byte(""))
	if !ok || string(millYarn) != "[mill test] ok\n" {
		t.Fatalf("yarn mill test: %q", millYarn)
	}

	// --- HatchTest: pnpm + yarn ---
	hatchPnpm, ok := TryCompactHatchTest([]string{"pnpm", "exec", "hatch", "test"}, []byte(""))
	if !ok || string(hatchPnpm) != "[hatch test] ok\n" {
		t.Fatalf("pnpm hatch test: %q", hatchPnpm)
	}
	hatchYarn, ok := TryCompactHatchTest([]string{"yarn", "hatch", "test"}, []byte(""))
	if !ok || string(hatchYarn) != "[hatch test] ok\n" {
		t.Fatalf("yarn hatch test: %q", hatchYarn)
	}

	// --- TurboTest: npx -> no test in run (L602/605) ---
	// TurboTest with npx build/run but "test" not in rest
	if _, ok := TryCompactTurboTest([]string{"npx", "turbo", "run", "build"}, []byte("")); ok {
		t.Fatal("npx turbo run build should not match turbo test")
	}

	// --- isRailsTestArgv: npx failure (L681) ---
	if _, ok := TryCompactRailsTest([]string{"npx", "rails"}, []byte("")); ok {
		t.Fatal("npx rails: too short for test")
	}

	// --- isPythonUnittestArgv: npx failure (L649) ---
	if _, ok := TryCompactPythonUnittest([]string{"npx", "python3"}, []byte("")); ok {
		t.Fatal("npx python3: too short for unittest")
	}
	// isPythonUnittestArgv: return false (L671)
	if _, ok := TryCompactPythonUnittest([]string{"curl", "url"}, []byte("")); ok {
		t.Fatal("curl not python unittest")
	}

	// --- UvRunPytest: yarn (L1029/1030) ---
	uvYarn, ok := TryCompactUvRunPytest([]string{"yarn", "uv", "run", "pytest"}, []byte(""))
	if !ok || string(uvYarn) != "[uv run pytest] ok\n" {
		t.Fatalf("yarn uv run pytest: %q", uvYarn)
	}
	// isUvRunPytestArgv: return false (L1009) - non-matching
	if _, ok := TryCompactUvRunPytest([]string{"curl", "url"}, []byte("")); ok {
		t.Fatal("curl not uv run pytest")
	}

	// --- PoetryRunPytest: npx (L1066) + pnpm (L1070) ---
	poNpx, ok := TryCompactPoetryRunPytest([]string{"npx", "poetry", "run", "pytest"}, []byte(""))
	if !ok || string(poNpx) != "[poetry run pytest] ok\n" {
		t.Fatalf("npx poetry run pytest: %q", poNpx)
	}
	poPnpm, ok := TryCompactPoetryRunPytest([]string{"pnpm", "exec", "poetry", "run", "pytest"}, []byte(""))
	if !ok || string(poPnpm) != "[poetry run pytest] ok\n" {
		t.Fatalf("pnpm poetry run pytest: %q", poPnpm)
	}
	// isPoetryRunPytestArgv: wrong sub (L1045)
	if _, ok := TryCompactPoetryRunPytest([]string{"poetry", "build"}, []byte("")); ok {
		t.Fatal("poetry build not run pytest")
	}
	// isPoetryRunPytestArgv: return false (L1055)
	if _, ok := TryCompactPoetryRunPytest([]string{"poetry", "run", "notpytest"}, []byte("")); ok {
		t.Fatal("poetry run notpytest")
	}

	// --- isNoxTestSessionArgv: npx failure (L1118) + bad tool (L1121) ---
	if _, ok := TryCompactNoxTest([]string{"npx", "-y"}, []byte("")); ok {
		t.Fatal("npx -y: no nox command")
	}
	// wrong tool after npx
	if _, ok := TryCompactNoxTest([]string{"npx", "rake", "-s", "test"}, []byte("")); ok {
		t.Fatal("npx rake not nox")
	}

	// --- isDartTestArgv: pnpm+fvm + yarn+fvm ---
	dtPnpmFvm, ok := TryCompactDartTest([]string{"pnpm", "exec", "fvm", "dart", "test"}, []byte(""))
	if !ok || string(dtPnpmFvm) != "[dart test] ok\n" {
		t.Fatalf("pnpm fvm dart test: %q", dtPnpmFvm)
	}
	dtYarnFvm, ok := TryCompactDartTest([]string{"yarn", "fvm", "dart", "test"}, []byte(""))
	if !ok || string(dtYarnFvm) != "[dart test] ok\n" {
		t.Fatalf("yarn fvm dart test: %q", dtYarnFvm)
	}

	// --- isFlutterTestArgv: pnpm+fvm + yarn+fvm ---
	ftPnpmFvm, ok := TryCompactFlutterTest([]string{"pnpm", "exec", "fvm", "flutter", "test"}, []byte(""))
	if !ok || string(ftPnpmFvm) != "[flutter test] ok\n" {
		t.Fatalf("pnpm fvm flutter test: %q", ftPnpmFvm)
	}
	ftYarnFvm, ok := TryCompactFlutterTest([]string{"yarn", "fvm", "flutter", "test"}, []byte(""))
	if !ok || string(ftYarnFvm) != "[flutter test] ok\n" {
		t.Fatalf("yarn fvm flutter test: %q", ftYarnFvm)
	}
}

// TestTryCompactTestrun_missingBranches covers remaining uncovered branches.
func TestTryCompactTestrun_missingBranches(t *testing.T) {
	t.Parallel()

	// TryCompactGoTestJSON: empty stdout (L15-17)
	if _, ok := TryCompactGoTestJSON([]string{"go", "test", "-json", "./..."}, []byte("")); ok {
		t.Fatal("go test -json: empty stdout should return false")
	}

	// TryCompactGoTestJSON: empty line within stream and failed==0 no-shrink guard.
	// Short stream "{}\n\n{}" (7 bytes) → compact "[go test -json] ok\n" (19 bytes) >= 7 → returns false.
	tinyWithEmpty := []byte("{}\n\n{}")
	if _, ok := TryCompactGoTestJSON([]string{"go", "test", "-json", "./..."}, tinyWithEmpty); ok {
		t.Fatal("go test -json: tiny stream should not compact (output >= input)")
	}

	// TryCompactNpmRunTest: wrong argv[1] or argv[2] (L497-499)
	if _, ok := TryCompactNpmRunTest([]string{"npm", "exec", "test"}, []byte("")); ok {
		t.Fatal("npm exec test: should not match (not 'run test')")
	}

	// TryCompactTurboTest: turbo binary with non-test subcommand (L602)
	if _, ok := TryCompactTurboTest([]string{"turbo", "build"}, []byte("")); ok {
		t.Fatal("turbo build: should not match as test")
	}

	// TryCompactTurboTest: npx turbo test (L605-607)
	out, ok := TryCompactTurboTest([]string{"npx", "turbo", "test"}, []byte(""))
	if !ok || string(out) != "[turbo test] ok\n" {
		t.Fatalf("npx turbo test: ok=%v %q", ok, out)
	}

	// isPythonUnittestArgv: npx with rest<3 (L649-651)
	if isPythonUnittestArgv([]string{"npx", "-y", "python3"}) {
		t.Fatal("npx rest<3: should return false")
	}

	// isPythonUnittestArgv: python without -m unittest (L671) — needs len>=3 to reach end
	if isPythonUnittestArgv([]string{"python3", "script.py", "arg"}) {
		t.Fatal("python3 script.py arg: no -m unittest, should return false")
	}

	// isUvRunPytestArgv: uv run <not pytest> (L1009)
	if isUvRunPytestArgv([]string{"uv", "run", "something_else"}) {
		t.Fatal("uv run something_else: should return false")
	}

	// isPoetryRunPytestArgv: poetry with wrong subcommand (L1045-1047)
	if isPoetryRunPytestArgv([]string{"poetry", "build", "pytest"}) {
		t.Fatal("poetry build: should return false (not 'run')")
	}
}

func TestTryCompactGoTestJSON_failureSummary(t *testing.T) {
	t.Parallel()
	// Realistic go test -json stream with 1 pass and 1 fail
	stream := `{"Action":"start","Package":"github.com/myapp/pkg"}
{"Action":"run","Package":"github.com/myapp/pkg","Test":"TestFoo"}
{"Action":"pass","Package":"github.com/myapp/pkg","Test":"TestFoo"}
{"Action":"run","Package":"github.com/myapp/pkg","Test":"TestBar"}
{"Action":"output","Package":"github.com/myapp/pkg","Test":"TestBar","Output":"--- FAIL: TestBar (0.00s)\n"}
{"Action":"output","Package":"github.com/myapp/pkg","Test":"TestBar","Output":"    expected 42 got 0\n"}
{"Action":"fail","Package":"github.com/myapp/pkg","Test":"TestBar"}
{"Action":"fail","Package":"github.com/myapp/pkg"}
`
	out, ok := TryCompactGoTestJSON([]string{"go", "test", "-json", "./..."}, []byte(stream))
	if !ok {
		t.Fatalf("expected compact failure output, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "1 passed, 1 failed") {
		t.Errorf("want pass/fail counts, got: %q", s)
	}
	if !strings.Contains(s, "TestBar") {
		t.Errorf("want failed test name, got: %q", s)
	}
	if len(s) >= len(stream) {
		t.Errorf("compact should be shorter: %d vs %d", len(s), len(stream))
	}
}

func TestTryCompactTestOutput_nonEmptyGoTestPass(t *testing.T) {
	t.Parallel()
	// go test with verbose passing output
	input := "=== RUN   TestFoo\n--- PASS: TestFoo (0.00s)\n=== RUN   TestBar\n--- PASS: TestBar (0.01s)\nPASS\nok  \tgithub.com/myapp/pkg\t0.024s\n"
	out, ok := TryCompactTestOutput([]string{"go", "test", "./..."}, []byte(input))
	if !ok {
		t.Fatalf("expected compact pass output, got pass-through; input=%q", input)
	}
	s := string(out)
	if !strings.Contains(s, "[go test] ok") {
		t.Errorf("want [go test] ok, got: %q", s)
	}
}

func TestTryCompactTestOutput_nonEmptySecondaryAllPass(t *testing.T) {
	t.Parallel()
	ctest := strings.Join([]string{
		"Test project /repo/build",
		"      Start  1: alpha",
		" 1/12 Test  #1: alpha ...........................   Passed    0.01 sec",
		strings.Repeat(" 2/12 Test  #2: beta ............................   Passed    0.01 sec\n", 20),
		"100% tests passed, 0 tests failed out of 12",
	}, "\n")
	out, ok := TryCompactTestOutput([]string{"ctest", "--output-on-failure"}, []byte(ctest))
	if !ok || string(out) != "[ctest] ok (100% tests passed, 0 tests failed out of 12)\n" {
		t.Fatalf("ctest all-pass compaction failed: ok=%v out=%q", ok, out)
	}

	phpunit := strings.Join([]string{
		"PHPUnit 10.5.0 by Sebastian Bergmann and contributors.",
		"",
		strings.Repeat(".", 80) + " 80 / 80 (100%)",
		"",
		"Time: 00:00.123, Memory: 12.00 MB",
		"",
		"OK (80 tests, 240 assertions)",
	}, "\n")
	out, ok = TryCompactTestOutput([]string{"phpunit", "tests/"}, []byte(phpunit))
	if !ok || string(out) != "[phpunit] ok (80 tests, 240 assertions)\n" {
		t.Fatalf("phpunit all-pass compaction failed: ok=%v out=%q", ok, out)
	}

	gradle := strings.Join([]string{
		"> Task :compileJava",
		"> Task :test",
		strings.Repeat("> Task :subproject:test\n", 40),
		"BUILD SUCCESSFUL in 2s",
		"12 actionable tasks: 12 executed",
	}, "\n")
	out, ok = TryCompactTestOutput([]string{"./gradlew", "test"}, []byte(gradle))
	if !ok || string(out) != "[gradle test] ok (BUILD SUCCESSFUL in 2s)\n" {
		t.Fatalf("gradle all-pass compaction failed: ok=%v out=%q", ok, out)
	}

	dart := strings.Join([]string{
		"00:00 +0: loading test/widget_test.dart",
		strings.Repeat("00:01 +1: widget renders frame\n", 40),
		"00:02 +40: All tests passed!",
	}, "\n")
	out, ok = TryCompactTestOutput([]string{"dart", "test"}, []byte(dart))
	if !ok || string(out) != "[dart test] ok (00:02 +40: All tests passed!)\n" {
		t.Fatalf("dart all-pass compaction failed: ok=%v out=%q", ok, out)
	}

	flutter := strings.Join([]string{
		"00:00 +0: loading test/app_test.dart",
		strings.Repeat("00:01 +1: renders home screen\n", 40),
		"00:03 +40: All tests passed!",
	}, "\n")
	out, ok = TryCompactTestOutput([]string{"flutter", "test"}, []byte(flutter))
	if !ok || string(out) != "[flutter test] ok (00:03 +40: All tests passed!)\n" {
		t.Fatalf("flutter all-pass compaction failed: ok=%v out=%q", ok, out)
	}

	deno := strings.Join([]string{
		"Check file:///repo/foo_test.ts",
		"running 40 tests from ./foo_test.ts",
		strings.Repeat("test case ... ok (1ms)\n", 40),
		"ok | 40 passed | 0 failed (123ms)",
	}, "\n")
	out, ok = TryCompactTestOutput([]string{"deno", "test", "--allow-all"}, []byte(deno))
	if !ok || string(out) != "[deno test] ok (ok | 40 passed | 0 failed (123ms))\n" {
		t.Fatalf("deno all-pass compaction failed: ok=%v out=%q", ok, out)
	}
}

func TestTryCompactTestOutput_secondaryAllPassFailOpenOnSignals(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactCtest([]string{"ctest"}, []byte("90% tests passed, 1 tests failed out of 10\nThe following tests FAILED:\n")); ok {
		t.Fatal("ctest failure output must fail open")
	}
	if _, ok := TryCompactPhpunit([]string{"phpunit"}, []byte("OK, but there were issues!\nTests: 10, Assertions: 20, Warnings: 1.\n")); ok {
		t.Fatal("phpunit warning output must fail open")
	}
	if _, ok := TryCompactGradleTest([]string{"gradle", "test"}, []byte("BUILD SUCCESSFUL in 1s\nDeprecated Gradle features were used in this build.\n")); ok {
		t.Fatal("gradle deprecation output must fail open")
	}
	if _, ok := TryCompactDartTest([]string{"dart", "test"}, []byte("00:01 +0 -1: Some tests failed.\n")); ok {
		t.Fatal("dart failure output must fail open")
	}
	if _, ok := TryCompactFlutterTest([]string{"flutter", "test"}, []byte("00:01 +1: All tests passed!\nWarning: golden images changed\n")); ok {
		t.Fatal("flutter warning output must fail open")
	}
	if _, ok := TryCompactDenoTest([]string{"deno", "test"}, []byte("ok | 9 passed | 0 failed (10ms)\nWarning experimental API\n")); ok {
		t.Fatal("deno warning output must fail open")
	}
}

func TestTryCompactTestOutput_nonEmptyGoTestFail(t *testing.T) {
	t.Parallel()
	// go test with failing output
	input := "=== RUN   TestFoo\n--- FAIL: TestFoo (0.00s)\n    expected: 42, got: 0\n=== RUN   TestBar\n--- PASS: TestBar (0.00s)\nFAIL\tgithub.com/myapp/pkg\t0.001s\n"
	out, ok := TryCompactTestOutput([]string{"go", "test", "./..."}, []byte(input))
	if !ok {
		t.Fatalf("expected compact fail output, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "[go test] FAILED") {
		t.Errorf("want FAILED header, got: %q", s)
	}
	if !strings.Contains(s, "FAIL: TestFoo") {
		t.Errorf("want FAIL: TestFoo line, got: %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact should be shorter: %d vs %d", len(s), len(input))
	}
}

func TestTestToolLabel_packageManagerRunTest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv  []string
		label string
	}{
		{[]string{"npm", "run", "test"}, "npm run test"},
		{[]string{"pnpm", "run", "test"}, "pnpm run test"},
		{[]string{"yarn", "run", "test"}, "yarn run test"},
	}
	for _, c := range cases {
		got := testToolLabel(c.argv)
		if got != c.label {
			t.Errorf("testToolLabel(%v) = %q, want %q", c.argv, got, c.label)
		}
	}
}

func TestTestToolLabel_nxAndTurbo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv  []string
		label string
	}{
		{[]string{"nx", "test"}, "nx test"},
		{[]string{"npx", "nx", "test"}, "nx test"},
		{[]string{"turbo", "test"}, "turbo test"},
		{[]string{"pnpm", "exec", "turbo", "test"}, "turbo test"},
	}
	for _, c := range cases {
		got := testToolLabel(c.argv)
		if got != c.label {
			t.Errorf("testToolLabel(%v) = %q, want %q", c.argv, got, c.label)
		}
	}
}

func TestTestToolLabel_binSubTools(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv  []string
		label string
	}{
		{[]string{"mocha", "--reporter", "spec"}, "mocha"},
		{[]string{"ava", "test.js"}, "ava"},
		{[]string{"tap", "test.js"}, "tap"},
		{[]string{"playwright", "test", "e2e/"}, "playwright test"},
		{[]string{"cypress", "run", "--browser", "chrome"}, "cypress run"},
		{[]string{"wdio", "run", "wdio.conf.js"}, "wdio run"},
		{[]string{"elm-test", "--seed", "123"}, "elm-test"},
		{[]string{"karma", "start", "karma.conf.js"}, "karma"},
		{[]string{"bun", "test"}, "bun test"},
	}
	for _, c := range cases {
		got := testToolLabel(c.argv)
		if got != c.label {
			t.Errorf("testToolLabel(%v) = %q, want %q", c.argv, got, c.label)
		}
	}
}

func TestTestToolLabel_unknownReturnsEmpty(t *testing.T) {
	t.Parallel()
	got := testToolLabel([]string{"unknown-tool", "arg"})
	if got != "" {
		t.Errorf("unknown tool should return empty, got %q", got)
	}
}

// TestTestToolLabel_switchCases covers all switch-case branches in testToolLabel.
func TestTestToolLabel_switchCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv  []string
		label string
	}{
		// go test (isGoTestArgv)
		{[]string{"go", "test", "./..."}, "go test"},
		// go test -json (isGoTestJSONArgv)
		{[]string{"go", "test", "-json", "./..."}, "go test"},
		// cargo test (isCargoTestArgv)
		{[]string{"cargo", "test"}, "cargo test"},
		// cargo nextest run (isCargoNextestRunArgv)
		{[]string{"cargo", "nextest", "run"}, "cargo nextest"},
		// cargo llvm-cov (isCargoLlvmCovArgv)
		{[]string{"cargo", "llvm-cov", "--all-features"}, "cargo llvm-cov"},
		// python -m unittest (isPythonUnittestArgv)
		{[]string{"python3", "-m", "unittest", "discover"}, "python unittest"},
		// rails test (isRailsTestArgv)
		{[]string{"rails", "test"}, "rails test"},
		// dart test (isDartTestArgv)
		{[]string{"dart", "test"}, "dart test"},
		// flutter test (isFlutterTestArgv)
		{[]string{"flutter", "test"}, "flutter test"},
		// deno test (isDenoTestArgv)
		{[]string{"deno", "test"}, "deno test"},
		// nox -s test (isNoxTestSessionArgv)
		{[]string{"nox", "-s", "test"}, "nox test"},
		// uv run pytest (isUvRunPytestArgv)
		{[]string{"uv", "run", "pytest"}, "uv run pytest"},
		// poetry run pytest (isPoetryRunPytestArgv)
		{[]string{"poetry", "run", "pytest"}, "poetry run pytest"},
		// ginkgo (binSub slice)
		{[]string{"ginkgo", "--randomize-all"}, "ginkgo"},
		// ctest
		{[]string{"ctest", "--output-on-failure"}, "ctest"},
		// pytest
		{[]string{"pytest", "-v"}, "pytest"},
		// py.test alias
		{[]string{"py.test", "tests/"}, "pytest"},
		// phpunit
		{[]string{"phpunit", "--testdox"}, "phpunit"},
		// phpunit.phar
		{[]string{"phpunit.phar", "tests/"}, "phpunit"},
		// vitest
		{[]string{"vitest", "run"}, "vitest"},
		// jest
		{[]string{"jest", "--coverage"}, "jest"},
	}
	for _, c := range cases {
		got := testToolLabel(c.argv)
		if got != c.label {
			t.Errorf("testToolLabel(%v) = %q, want %q", c.argv, got, c.label)
		}
	}
}

func TestTryCompactTestRunners_verboseAllPass(t *testing.T) {
	t.Parallel()

	var cargo strings.Builder
	cargo.WriteString("   Compiling slimtest v0.1.0\n    Finished test profile\n     Running unittests src/lib.rs\n\nrunning 80 tests\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&cargo, "test alpha::op_%03d ... ok\n", i)
	}
	cargo.WriteString("\ntest result: ok. 80 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s\n")
	out, ok := TryCompactCargoTest([]string{"cargo", "test"}, []byte(cargo.String()))
	if !ok || !strings.Contains(string(out), "[cargo test] ok - 80 passed") ||
		!strings.Contains(string(out), "test result: ok. 80 passed; 0 failed") ||
		strings.Contains(string(out), "alpha::op_000") {
		t.Fatalf("cargo verbose all-pass: ok=%v %q", ok, out)
	}
	if _, ok := TryCompactCargoTest([]string{"cargo", "test"}, []byte("running 2 tests\ntest a ... ok\ntest b ... FAILED\n\ntest result: FAILED. 1 passed; 1 failed\n")); ok {
		t.Fatal("cargo failure must fail open")
	}

	var py strings.Builder
	py.WriteString("============================= test session starts ==============================\n")
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&py, "tests/test_alpha.py::test_op_%03d PASSED                                  [ %2d%%]\n", i, i)
	}
	py.WriteString("============================== 90 passed in 0.42s ===============================\n")
	out, ok = TryCompactPytest([]string{"pytest", "-v"}, []byte(py.String()))
	if !ok || !strings.Contains(string(out), "[pytest] ok - 90 passed") ||
		!strings.Contains(string(out), "90 passed in 0.42s") ||
		strings.Contains(string(out), "test_op_000") {
		t.Fatalf("pytest verbose all-pass: ok=%v %q", ok, out)
	}
	if _, ok := TryCompactPytest([]string{"pytest", "-v"}, []byte("tests/test_a.py::test_x FAILED\n=== 1 failed in 0.1s ===\n")); ok {
		t.Fatal("pytest failure must fail open")
	}

	var js strings.Builder
	js.WriteString("PASS src/alpha.test.ts\n")
	for i := 0; i < 70; i++ {
		fmt.Fprintf(&js, "  ✓ renders op %03d (2 ms)\n", i)
	}
	js.WriteString("\nTests: 70 passed, 70 total\nTime: 1.2 s\n")
	out, ok = TryCompactJest([]string{"jest"}, []byte(js.String()))
	if !ok || !strings.Contains(string(out), "[jest] ok - 70 passed") ||
		!strings.Contains(string(out), "Tests: 70 passed, 70 total") ||
		strings.Contains(string(out), "renders op 000") {
		t.Fatalf("jest verbose all-pass: ok=%v %q", ok, out)
	}
	if _, ok := TryCompactJest([]string{"jest"}, []byte("FAIL src/a.test.ts\n  ✕ broken (3 ms)\nTests: 1 failed, 1 total\n")); ok {
		t.Fatal("jest failure must fail open")
	}

	vit := strings.ReplaceAll(js.String(), "PASS src/alpha.test.ts", " ✓ src/alpha.test.ts (70 tests)")
	out, ok = TryCompactVitest([]string{"vitest", "run"}, []byte(vit))
	if !ok || !strings.Contains(string(out), "[vitest] ok") || strings.Contains(string(out), "renders op 000") {
		t.Fatalf("vitest verbose all-pass: ok=%v %q", ok, out)
	}
}

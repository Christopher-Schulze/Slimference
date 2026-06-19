package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactBuildOutput_goCargo(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactGoBuild([]string{"go", "build", "./..."}, []byte(" \n"))
	if !ok || string(out) != "[go build] ok\n" {
		t.Fatalf("go build: ok=%v %q", ok, out)
	}
	goNpx, ok := TryCompactGoBuild([]string{"npx", "go", "build", "."}, []byte(""))
	if !ok || string(goNpx) != "[go build] ok\n" {
		t.Fatalf("npx go build: %q", goNpx)
	}
	if _, ok := TryCompactGoBuild([]string{"go", "test"}, []byte("")); ok {
		t.Fatal("go test not build")
	}
	out2, ok := TryCompactCargoBuild([]string{"cargo", "build"}, []byte(""))
	if !ok || string(out2) != "[cargo build] ok\n" {
		t.Fatalf("cargo: %q", out2)
	}
	cbYarn, ok := TryCompactCargoBuild([]string{"yarn", "cargo", "build"}, []byte("\n"))
	if !ok || string(cbYarn) != "[cargo build] ok\n" {
		t.Fatalf("yarn cargo build: %q", cbYarn)
	}
	if _, ok := TryCompactCargoBuild([]string{"cargo", "test"}, []byte("")); ok {
		t.Fatal("cargo test not build")
	}
	chk, ok := TryCompactCargoCheck([]string{"cargo", "check"}, []byte(""))
	if !ok || string(chk) != "[cargo check] ok\n" {
		t.Fatalf("cargo check: %q", chk)
	}
	chkNpx, ok := TryCompactCargoCheck([]string{"npx", "-y", "cargo", "check"}, []byte(""))
	if !ok || string(chkNpx) != "[cargo check] ok\n" {
		t.Fatalf("npx cargo check: %q", chkNpx)
	}
	doc, ok := TryCompactCargoDoc([]string{"cargo", "doc", "--no-deps"}, []byte(""))
	if !ok || string(doc) != "[cargo doc] ok\n" {
		t.Fatalf("cargo doc: %q", doc)
	}
	docPnpm, ok := TryCompactCargoDoc([]string{"pnpm", "exec", "cargo", "doc"}, []byte("\n"))
	if !ok || string(docPnpm) != "[cargo doc] ok\n" {
		t.Fatalf("pnpm cargo doc: %q", docPnpm)
	}
	mk, ok := TryCompactMake([]string{"make", "all"}, []byte("\n"))
	if !ok || string(mk) != "[make] ok\n" {
		t.Fatalf("make: %q", mk)
	}
	mkNpx, ok := TryCompactMake([]string{"npx", "make", "all"}, []byte(""))
	if !ok || string(mkNpx) != "[make] ok\n" {
		t.Fatalf("npx make: %q", mkNpx)
	}
	if _, ok := TryCompactMake([]string{"make", "-n"}, []byte("")); ok {
		t.Fatal("make -n")
	}
	if _, ok := TryCompactMake([]string{"npx", "make", "-n"}, []byte("")); ok {
		t.Fatal("npx make -n")
	}
	nj, ok := TryCompactNinja([]string{"ninja", "-C", "build"}, []byte(""))
	if !ok || string(nj) != "[ninja] ok\n" {
		t.Fatalf("ninja: %q", nj)
	}
	njPnpm, ok := TryCompactNinja([]string{"pnpm", "exec", "ninja", "-C", "out"}, []byte("\n"))
	if !ok || string(njPnpm) != "[ninja] ok\n" {
		t.Fatalf("pnpm ninja: %q", njPnpm)
	}
	cm, ok := TryCompactCmakeBuild([]string{"cmake", "--build", "build", "--target", "all"}, []byte(""))
	if !ok || string(cm) != "[cmake --build] ok\n" {
		t.Fatalf("cmake --build: %q", cm)
	}
	cmYarn, ok := TryCompactCmakeBuild([]string{"yarn", "cmake", "--build", "dbg"}, []byte(""))
	if !ok || string(cmYarn) != "[cmake --build] ok\n" {
		t.Fatalf("yarn cmake --build: %q", cmYarn)
	}
	out3, ok := TryCompactBuildOutput([]string{"go", "build"}, []byte(""))
	if !ok || string(out3) != "[go build] ok\n" {
		t.Fatalf("chain: %q", out3)
	}
}

func TestTryCompactBuildOutput_jsToolchain(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactTsc([]string{"/x/node_modules/.bin/tsc", "--noEmit"}, []byte(""))
	if !ok || string(out) != "[tsc] ok\n" {
		t.Fatalf("tsc: ok=%v %q", ok, out)
	}
	tscNpx, ok := TryCompactTsc([]string{"npx", "tsc", "--noEmit"}, []byte(""))
	if !ok || string(tscNpx) != "[tsc] ok\n" {
		t.Fatalf("npx tsc: %q", tscNpx)
	}
	out2, ok := TryCompactNextBuild([]string{"next", "build"}, []byte("\n"))
	if !ok || string(out2) != "[next build] ok\n" {
		t.Fatalf("next: %q", out2)
	}
	nextNpx, ok := TryCompactNextBuild([]string{"npx", "next", "build"}, []byte(""))
	if !ok || string(nextNpx) != "[next build] ok\n" {
		t.Fatalf("npx next build: %q", nextNpx)
	}
	out3, ok := TryCompactNpmRunBuild([]string{"npm", "run", "build"}, []byte(""))
	if !ok || string(out3) != "[npm run build] ok\n" {
		t.Fatalf("npm: %q", out3)
	}
	vb, ok := TryCompactViteBuild([]string{"vite", "build", "--emptyOutDir"}, []byte(""))
	if !ok || string(vb) != "[vite build] ok\n" {
		t.Fatalf("vite build: %q", vb)
	}
	viteNpx, ok := TryCompactViteBuild([]string{"npx", "vite", "build"}, []byte(""))
	if !ok || string(viteNpx) != "[vite build] ok\n" {
		t.Fatalf("npx vite build: %q", viteNpx)
	}
	wp, ok := TryCompactWebpack([]string{"webpack", "--mode", "production"}, []byte(""))
	if !ok || string(wp) != "[webpack] ok\n" {
		t.Fatalf("webpack: %q", wp)
	}
	wp2, ok := TryCompactWebpack([]string{"webpack-cli", "--config", "w.js"}, []byte("\n"))
	if !ok || string(wp2) != "[webpack] ok\n" {
		t.Fatalf("webpack-cli: %q", wp2)
	}
	wpNpx, ok := TryCompactWebpack([]string{"npx", "webpack", "--mode", "production"}, []byte(""))
	if !ok || string(wpNpx) != "[webpack] ok\n" {
		t.Fatalf("npx webpack: %q", wpNpx)
	}
	rb, ok := TryCompactRspackBuild([]string{"rspack", "build"}, []byte(""))
	if !ok || string(rb) != "[rspack build] ok\n" {
		t.Fatalf("rspack build: %q", rb)
	}
	parcelB, ok := TryCompactParcelBuild([]string{"parcel", "build"}, []byte(""))
	if !ok || string(parcelB) != "[parcel build] ok\n" {
		t.Fatalf("parcel build: %q", parcelB)
	}
	rspackNpx, ok := TryCompactRspackBuild([]string{"npx", "rspack", "build"}, []byte("\n"))
	if !ok || string(rspackNpx) != "[rspack build] ok\n" {
		t.Fatalf("npx rspack build: %q", rspackNpx)
	}
	parcelNpx, ok := TryCompactParcelBuild([]string{"npx", "parcel", "build"}, []byte(""))
	if !ok || string(parcelNpx) != "[parcel build] ok\n" {
		t.Fatalf("npx parcel build: %q", parcelNpx)
	}
	rollup, ok := TryCompactRollupConfig([]string{"rollup", "-c"}, []byte(""))
	if !ok || string(rollup) != "[rollup] ok\n" {
		t.Fatalf("rollup -c: %q", rollup)
	}
	rollupCfg, ok := TryCompactRollupConfig([]string{"rollup", "--config", "rollup.config.mjs"}, []byte("\n"))
	if !ok || string(rollupCfg) != "[rollup] ok\n" {
		t.Fatalf("rollup --config: %q", rollupCfg)
	}
	if _, ok := TryCompactRollupConfig([]string{"rollup", "entry.js"}, []byte("")); ok {
		t.Fatal("rollup without -c/--config")
	}
	rollupNpx, ok := TryCompactRollupConfig([]string{"npx", "rollup", "-c"}, []byte(""))
	if !ok || string(rollupNpx) != "[rollup] ok\n" {
		t.Fatalf("npx rollup -c: %q", rollupNpx)
	}
	nxb, ok := TryCompactNxBuild([]string{"nx", "build", "app"}, []byte(""))
	if !ok || string(nxb) != "[nx build] ok\n" {
		t.Fatalf("nx build: %q", nxb)
	}
	nxbNpx, ok := TryCompactNxBuild([]string{"npx", "nx", "build", "app"}, []byte(""))
	if !ok || string(nxbNpx) != "[nx build] ok\n" {
		t.Fatalf("npx nx build: %q", nxbNpx)
	}
	tb, ok := TryCompactTurboBuild([]string{"turbo", "run", "build"}, []byte(""))
	if !ok || string(tb) != "[turbo build] ok\n" {
		t.Fatalf("turbo run build: %q", tb)
	}
	tb2, ok := TryCompactTurboBuild([]string{"turbo", "build"}, []byte("\n"))
	if !ok || string(tb2) != "[turbo build] ok\n" {
		t.Fatalf("turbo build: %q", tb2)
	}
	tbNpx, ok := TryCompactTurboBuild([]string{"npx", "turbo", "run", "build"}, []byte(""))
	if !ok || string(tbNpx) != "[turbo build] ok\n" {
		t.Fatalf("npx turbo run build: %q", tbNpx)
	}
	tbNpxY, ok := TryCompactTurboBuild([]string{"npx", "-y", "turbo", "run", "build"}, []byte(""))
	if !ok || string(tbNpxY) != "[turbo build] ok\n" {
		t.Fatalf("npx -y turbo run build: %q", tbNpxY)
	}
	if _, ok := TryCompactTurboBuild([]string{"turbo", "run", "lint"}, []byte("")); ok {
		t.Fatal("turbo run lint not build")
	}
	esb, ok := TryCompactEsbuildBundle([]string{"esbuild", "in.ts", "--bundle", "--outfile=out.js"}, []byte(""))
	if !ok || string(esb) != "[esbuild] ok\n" {
		t.Fatalf("esbuild: %q", esb)
	}
	esbNpx, ok := TryCompactEsbuildBundle([]string{"npx", "esbuild", "in.ts", "--bundle", "--outfile=out.js"}, []byte(""))
	if !ok || string(esbNpx) != "[esbuild] ok\n" {
		t.Fatalf("npx esbuild: %q", esbNpx)
	}
	if _, ok := TryCompactEsbuildBundle([]string{"esbuild", "in.ts"}, []byte("")); ok {
		t.Fatal("esbuild without --bundle")
	}
	if _, ok := TryCompactEsbuildBundle([]string{"npx", "esbuild", "in.ts"}, []byte("")); ok {
		t.Fatal("npx esbuild without --bundle")
	}
	if _, ok := TryCompactNpmRunBuild([]string{"npm", "run", "test"}, []byte("")); ok {
		t.Fatal("npm run test not build")
	}
	pnpmB, ok := TryCompactPnpmRunBuild([]string{"pnpm", "run", "build"}, []byte(""))
	if !ok || string(pnpmB) != "[pnpm run build] ok\n" {
		t.Fatalf("pnpm run build: %q", pnpmB)
	}
	yarnB, ok := TryCompactYarnRunBuild([]string{"yarn", "run", "build"}, []byte(""))
	if !ok || string(yarnB) != "[yarn run build] ok\n" {
		t.Fatalf("yarn run build: %q", yarnB)
	}
	mv, ok := TryCompactMvn([]string{"mvnw", "-q", "test"}, []byte(""))
	if !ok || string(mv) != "[mvn] ok\n" {
		t.Fatalf("mvn: %q", mv)
	}
	mvNpx, ok := TryCompactMvn([]string{"npx", "mvnw", "-q", "test"}, []byte("\n"))
	if !ok || string(mvNpx) != "[mvn] ok\n" {
		t.Fatalf("npx mvnw: %q", mvNpx)
	}
	mvPnpm, ok := TryCompactMvn([]string{"pnpm", "exec", "mvn", "-q", "verify"}, []byte(""))
	if !ok || string(mvPnpm) != "[mvn] ok\n" {
		t.Fatalf("pnpm mvn: %q", mvPnpm)
	}
	if _, ok := TryCompactMvn([]string{"mvn", "-version"}, []byte("")); ok {
		t.Fatal("mvn -version")
	}
	if _, ok := TryCompactMvn([]string{"mvn", "-q"}, []byte("")); ok {
		t.Fatal("mvn -q only")
	}
	gr, ok := TryCompactGradle([]string{"gradlew", "build", "-q"}, []byte("\n"))
	if !ok || string(gr) != "[gradle build] ok\n" {
		t.Fatalf("gradle: %q", gr)
	}
	grNpx, ok := TryCompactGradle([]string{"npx", "-y", "gradlew", "build"}, []byte(""))
	if !ok || string(grNpx) != "[gradle build] ok\n" {
		t.Fatalf("npx gradlew build: %q", grNpx)
	}
	grYarn, ok := TryCompactGradle([]string{"yarn", "gradle", "build", "-q"}, []byte(""))
	if !ok || string(grYarn) != "[gradle build] ok\n" {
		t.Fatalf("yarn gradle build: %q", grYarn)
	}
	if _, ok := TryCompactGradle([]string{"gradle", "tasks"}, []byte("")); ok {
		t.Fatal("gradle tasks without build")
	}
	zb, ok := TryCompactZigBuild([]string{"zig", "build"}, []byte(""))
	if !ok || string(zb) != "[zig build] ok\n" {
		t.Fatalf("zig: %q", zb)
	}
	zbNpx, ok := TryCompactZigBuild([]string{"npx", "-y", "zig", "build"}, []byte("\n"))
	if !ok || string(zbNpx) != "[zig build] ok\n" {
		t.Fatalf("npx zig build: %q", zbNpx)
	}
	zbPnpm, ok := TryCompactZigBuild([]string{"pnpm", "exec", "zig", "build"}, []byte(""))
	if !ok || string(zbPnpm) != "[zig build] ok\n" {
		t.Fatalf("pnpm zig build: %q", zbPnpm)
	}
	j, ok := TryCompactJust([]string{"just", "ci"}, []byte("\n"))
	if !ok || string(j) != "[just] ok\n" {
		t.Fatalf("just: %q", j)
	}
	jNpx, ok := TryCompactJust([]string{"npx", "-y", "just", "ci"}, []byte(""))
	if !ok || string(jNpx) != "[just] ok\n" {
		t.Fatalf("npx just: %q", jNpx)
	}
	jYarn, ok := TryCompactJust([]string{"yarn", "just", "fmt"}, []byte("\n"))
	if !ok || string(jYarn) != "[just] ok\n" {
		t.Fatalf("yarn just: %q", jYarn)
	}
	wpack, ok := TryCompactWasmPackBuild([]string{"wasm-pack", "build"}, []byte(""))
	if !ok || string(wpack) != "[wasm-pack build] ok\n" {
		t.Fatalf("wasm-pack: %q", wpack)
	}
	wpackNpx, ok := TryCompactWasmPackBuild([]string{"npx", "-y", "wasm-pack", "build"}, []byte("\n"))
	if !ok || string(wpackNpx) != "[wasm-pack build] ok\n" {
		t.Fatalf("npx wasm-pack build: %q", wpackNpx)
	}
	wpackPnpm, ok := TryCompactWasmPackBuild([]string{"pnpm", "exec", "wasm-pack", "build"}, []byte(""))
	if !ok || string(wpackPnpm) != "[wasm-pack build] ok\n" {
		t.Fatalf("pnpm wasm-pack build: %q", wpackPnpm)
	}
	wpackYarn, ok := TryCompactWasmPackBuild([]string{"yarn", "wasm-pack", "build"}, []byte(""))
	if !ok || string(wpackYarn) != "[wasm-pack build] ok\n" {
		t.Fatalf("yarn wasm-pack build: %q", wpackYarn)
	}
	bz, ok := TryCompactBazelBuild([]string{"bazelisk", "build", "//pkg:all"}, []byte(""))
	if !ok || string(bz) != "[bazel build] ok\n" {
		t.Fatalf("bazel build: %q", bz)
	}
	bzNpx, ok := TryCompactBazelBuild([]string{"npx", "bazel", "build", "//x"}, []byte(""))
	if !ok || string(bzNpx) != "[bazel build] ok\n" {
		t.Fatalf("npx bazel build: %q", bzNpx)
	}
	bzPnpm, ok := TryCompactBazelBuild([]string{"pnpm", "exec", "bazelisk", "build", "//y"}, []byte("\n"))
	if !ok || string(bzPnpm) != "[bazel build] ok\n" {
		t.Fatalf("pnpm bazelisk build: %q", bzPnpm)
	}
	sw, ok := TryCompactSwiftBuild([]string{"swift", "build", "-c", "release"}, []byte("\n"))
	if !ok || string(sw) != "[swift build] ok\n" {
		t.Fatalf("swift build: %q", sw)
	}
	swYarn, ok := TryCompactSwiftBuild([]string{"yarn", "swift", "build"}, []byte(""))
	if !ok || string(swYarn) != "[swift build] ok\n" {
		t.Fatalf("yarn swift build: %q", swYarn)
	}
	bufB, ok := TryCompactBufBuild([]string{"buf", "build"}, []byte(""))
	if !ok || string(bufB) != "[buf build] ok\n" {
		t.Fatalf("buf build: %q", bufB)
	}
	bufBNpx, ok := TryCompactBufBuild([]string{"npx", "buf", "build"}, []byte("\n"))
	if !ok || string(bufBNpx) != "[buf build] ok\n" {
		t.Fatalf("npx buf build: %q", bufBNpx)
	}
	koB, ok := TryCompactKoBuild([]string{"ko", "build", "./cmd/foo"}, []byte(""))
	if !ok || string(koB) != "[ko build] ok\n" {
		t.Fatalf("ko build: %q", koB)
	}
	koNpx, ok := TryCompactKoBuild([]string{"npx", "ko", "build", "."}, []byte("\n"))
	if !ok || string(koNpx) != "[ko build] ok\n" {
		t.Fatalf("npx ko build: %q", koNpx)
	}
	mes, ok := TryCompactMesonCompile([]string{"meson", "compile", "-C", "build"}, []byte(""))
	if !ok || string(mes) != "[meson compile] ok\n" {
		t.Fatalf("meson compile: %q", mes)
	}
	mesPnpm, ok := TryCompactMesonCompile([]string{"pnpm", "exec", "meson", "compile", "-C", "out"}, []byte(""))
	if !ok || string(mesPnpm) != "[meson compile] ok\n" {
		t.Fatalf("pnpm meson compile: %q", mesPnpm)
	}
	moonB, ok := TryCompactMoonRunBuild([]string{"moon", "run", "app:build"}, []byte(""))
	if !ok || string(moonB) != "[moon run build] ok\n" {
		t.Fatalf("moon run build: %q", moonB)
	}
	moonB2, ok := TryCompactMoonRunBuild([]string{"moon", "run", "build"}, []byte("\n"))
	if !ok || string(moonB2) != "[moon run build] ok\n" {
		t.Fatalf("moon run build literal: %q", moonB2)
	}
	moonNpx, ok := TryCompactMoonRunBuild([]string{"npx", "-y", "moon", "run", "build"}, []byte(""))
	if !ok || string(moonNpx) != "[moon run build] ok\n" {
		t.Fatalf("npx -y moon run build: %q", moonNpx)
	}
	moonPnpm, ok := TryCompactMoonRunBuild([]string{"pnpm", "exec", "moon", "run", "app:build"}, []byte("\n"))
	if !ok || string(moonPnpm) != "[moon run build] ok\n" {
		t.Fatalf("pnpm exec moon run app:build: %q", moonPnpm)
	}
	moonYarn, ok := TryCompactMoonRunBuild([]string{"yarn", "moon", "run", "build"}, []byte(""))
	if !ok || string(moonYarn) != "[moon run build] ok\n" {
		t.Fatalf("yarn moon run build: %q", moonYarn)
	}
	if _, ok := TryCompactBufBuild([]string{"buf", "lint"}, []byte("")); ok {
		t.Fatal("buf lint not build")
	}
	if _, ok := TryCompactMoonRunBuild([]string{"moon", "run", "test"}, []byte("")); ok {
		t.Fatal("moon run test not build")
	}
	pk, ok := TryCompactPackBuild([]string{"pack", "build", "myimg:latest"}, []byte(""))
	if !ok || string(pk) != "[pack build] ok\n" {
		t.Fatalf("pack build: %q", pk)
	}
	pkNpx, ok := TryCompactPackBuild([]string{"npx", "-y", "pack", "build", "img"}, []byte("\n"))
	if !ok || string(pkNpx) != "[pack build] ok\n" {
		t.Fatalf("npx pack build: %q", pkNpx)
	}
	if _, ok := TryCompactPackBuild([]string{"pack", "inspect"}, []byte("")); ok {
		t.Fatal("pack inspect not build")
	}
}

func TestTryCompactNextViteBuildCleanOutput(t *testing.T) {
	t.Parallel()

	nextOut, ok := TryCompactNextBuild([]string{"next", "build"}, []byte(nextBuildCleanFixture()))
	if !ok || string(nextOut) != "[next build] ok\n" {
		t.Fatalf("next clean build: ok=%v out=%q", ok, nextOut)
	}

	nextNpx, ok := TryCompactBuildOutput([]string{"npx", "-y", "next", "build"}, []byte(nextBuildCleanFixture()))
	if !ok || string(nextNpx) != "[next build] ok\n" {
		t.Fatalf("npx next clean build: ok=%v out=%q", ok, nextNpx)
	}

	viteOut, ok := TryCompactViteBuild([]string{"vite", "build", "--emptyOutDir"}, []byte(viteBuildCleanFixture()))
	if !ok || string(viteOut) != "[vite build] ok\n" {
		t.Fatalf("vite clean build: ok=%v out=%q", ok, viteOut)
	}

	vitePnpm, ok := TryCompactBuildOutput([]string{"pnpm", "exec", "vite", "build"}, []byte(viteBuildCleanFixture()))
	if !ok || string(vitePnpm) != "[vite build] ok\n" {
		t.Fatalf("pnpm vite clean build: ok=%v out=%q", ok, vitePnpm)
	}
}

func TestTryCompactNextViteBuildCleanOutputFailOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
		try    func([]string, []byte) ([]byte, bool)
	}{
		{
			name:   "next warning",
			argv:   []string{"next", "build"},
			stdout: nextBuildCleanFixture() + "warning: viewport metadata is deprecated\n",
			try:    TryCompactNextBuild,
		},
		{
			name:   "next failed",
			argv:   []string{"next", "build"},
			stdout: "Next.js 15.3.0\nCreating an optimized production build ...\nFailed to compile.\napp/page.tsx:1:1 error Missing export\n",
			try:    TryCompactNextBuild,
		},
		{
			name:   "next weak generic success",
			argv:   []string{"next", "build"},
			stdout: "Compiled successfully\n",
			try:    TryCompactNextBuild,
		},
		{
			name:   "vite chunk warning",
			argv:   []string{"vite", "build"},
			stdout: viteBuildCleanFixture() + "(!) Some chunks are larger than 500 kB after minification.\n",
			try:    TryCompactViteBuild,
		},
		{
			name:   "vite error",
			argv:   []string{"vite", "build"},
			stdout: "vite v6.3.5 building for production...\n1 modules transformed.\nerror during build: Could not resolve ./missing\n",
			try:    TryCompactViteBuild,
		},
		{
			name:   "vite weak generic success",
			argv:   []string{"vite", "build"},
			stdout: "built in 10ms\n",
			try:    TryCompactViteBuild,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out, ok := tt.try(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("unsafe or weak web build output compacted: %q", out)
			}
		})
	}
}

func TestTryCompactMakeCmakeCleanProgressOutput(t *testing.T) {
	t.Parallel()

	makeOut, ok := TryCompactMake([]string{"make", "-j8"}, []byte(makeCmakeStyleCleanFixture(24)))
	if !ok || string(makeOut) != "[make] ok\n" {
		t.Fatalf("make CMake-style clean progress: ok=%v out=%q", ok, makeOut)
	}

	wrappedMake, ok := TryCompactBuildOutput([]string{"pnpm", "exec", "make", "all"}, []byte(makeCmakeStyleCleanFixture(24)))
	if !ok || string(wrappedMake) != "[make] ok\n" {
		t.Fatalf("wrapped make CMake-style clean progress: ok=%v out=%q", ok, wrappedMake)
	}

	cmakeOut, ok := TryCompactCmakeBuild([]string{"cmake", "--build", "build", "--parallel"}, []byte(cmakeBuildCleanFixture(24)))
	if !ok || string(cmakeOut) != "[cmake --build] ok\n" {
		t.Fatalf("cmake --build clean progress: ok=%v out=%q", ok, cmakeOut)
	}

	cmakeNinja, ok := TryCompactBuildOutput([]string{"yarn", "cmake", "--build", "build"}, []byte(cmakeNinjaCleanFixture(24)))
	if !ok || string(cmakeNinja) != "[cmake --build] ok\n" {
		t.Fatalf("wrapped cmake ninja clean progress: ok=%v out=%q", ok, cmakeNinja)
	}
}

func TestTryCompactMakeCmakeCleanProgressFailOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
		try    func([]string, []byte) ([]byte, bool)
	}{
		{
			name:   "make warning",
			argv:   []string{"make"},
			stdout: makeCmakeStyleCleanFixture(8) + "warning: generated header is stale\n",
			try:    TryCompactMake,
		},
		{
			name:   "make arbitrary recipe",
			argv:   []string{"make"},
			stdout: "printf 'deploying production'\ncc -O2 main.c -o app\n",
			try:    TryCompactMake,
		},
		{
			name:   "make dry run",
			argv:   []string{"make", "-n"},
			stdout: makeCmakeStyleCleanFixture(8),
			try:    TryCompactMake,
		},
		{
			name:   "cmake compile error",
			argv:   []string{"cmake", "--build", "build"},
			stdout: "[ 50%] Building CXX object src/CMakeFiles/app.dir/main.cpp.o\nerror: missing semicolon\n",
			try:    TryCompactCmakeBuild,
		},
		{
			name:   "cmake no terminal success",
			argv:   []string{"cmake", "--build", "build"},
			stdout: "[ 10%] Building CXX object src/CMakeFiles/app.dir/main.cpp.o\n[ 20%] Building CXX object src/CMakeFiles/app.dir/lib.cpp.o\n",
			try:    TryCompactCmakeBuild,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if out, ok := tt.try(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("unsafe make/cmake progress compacted: %q", out)
			}
		})
	}
}

func TestTryCompactMakeCmakeCleanProgressWrapperAndNoWorkBranches(t *testing.T) {
	t.Parallel()

	progress := cmakeBuildCleanFixture(6)
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "npx make", argv: []string{"npx", "make", "all"}, want: "[make] ok\n"},
		{name: "yarn make", argv: []string{"yarn", "make", "all"}, want: "[make] ok\n"},
		{name: "npx cmake build", argv: []string{"npx", "cmake", "--build", "build"}, want: "[cmake --build] ok\n"},
		{name: "pnpm cmake build", argv: []string{"pnpm", "exec", "cmake", "--build", "build"}, want: "[cmake --build] ok\n"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactBuildOutput(tt.argv, []byte(progress))
			if !ok || string(out) != tt.want {
				t.Fatalf("%s: ok=%v out=%q", tt.name, ok, out)
			}
		})
	}

	noWork, ok := TryCompactCmakeBuild([]string{"cmake", "--build", "build"}, []byte("ninja: no work to do.\n"))
	if !ok || string(noWork) != "[cmake --build] ok\n" {
		t.Fatalf("ninja no work: ok=%v out=%q", ok, noWork)
	}
	if out, ok := TryCompactMake(nil, []byte(progress)); ok || string(out) != progress {
		t.Fatalf("empty argv make must fail open: ok=%v out=%q", ok, out)
	}
	if out, ok := TryCompactCmakeBuild(nil, []byte(progress)); ok || string(out) != progress {
		t.Fatalf("empty argv cmake must fail open: ok=%v out=%q", ok, out)
	}
}

func TestTryCompactMvnCleanSuccessOutput(t *testing.T) {
	t.Parallel()

	input := mavenCleanSuccessFixture(32)
	out, ok := TryCompactMvn([]string{"mvn", "test"}, []byte(input))
	if !ok || string(out) != "[mvn] ok (Tests run: 42, Failures: 0, Errors: 0, Skipped: 0)\n" {
		t.Fatalf("maven clean success: ok=%v out=%q", ok, out)
	}
	if len(out) >= len(input) {
		t.Fatal("maven clean success summary must be shorter than original output")
	}

	wrapped, ok := TryCompactBuildOutput([]string{"pnpm", "exec", "mvnw", "-q", "verify"}, []byte(input))
	if !ok || string(wrapped) != "[mvn] ok (Tests run: 42, Failures: 0, Errors: 0, Skipped: 0)\n" {
		t.Fatalf("wrapped maven clean success through build chain: ok=%v out=%q", ok, wrapped)
	}
}

func TestTryCompactMvnCleanSuccessFailOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "warning", input: mavenCleanSuccessFixture(4) + "[WARNING] Using platform encoding UTF-8 to copy filtered resources.\n"},
		{name: "build failure", input: strings.Replace(mavenCleanSuccessFixture(4), "[INFO] BUILD SUCCESS", "[INFO] BUILD FAILURE", 1)},
		{name: "test failure", input: strings.Replace(mavenCleanSuccessFixture(4), "Failures: 0", "Failures: 1", 1)},
		{name: "skipped tests", input: strings.Replace(mavenCleanSuccessFixture(4), "Skipped: 0", "Skipped: 2", 1)},
		{name: "source context", input: mavenCleanSuccessFixture(4) + "[INFO] public class App {}\n"},
		{name: "arbitrary info log", input: mavenCleanSuccessFixture(4) + "[INFO] application bootstrap token refreshed\n"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if out, ok := TryCompactMvn([]string{"mvn", "test"}, []byte(tt.input)); ok {
				t.Fatalf("unsafe maven output compacted: %q", out)
			}
			if strings.Contains(tt.input, "[INFO] BUILD SUCCESS") {
				if out, ok := TryCompactBuildOutput([]string{"mvn", "test"}, []byte(tt.input)); ok {
					t.Fatalf("unsafe maven success compacted by build chain: %q", out)
				}
				return
			}
			if out, ok := TryCompactBuildOutput([]string{"mvn", "test"}, []byte(tt.input)); ok && strings.Contains(string(out), "[mvn] ok") {
				t.Fatalf("unsafe maven success compacted by build chain: %q", out)
			}
		})
	}
	if out, ok := compactMvnCleanSuccessOutput("[INFO] BUILD SUCCESS\n[INFO] Total time: 1 s\n", len("[mvn] ok\n")); ok || out != nil {
		t.Fatalf("non-shrinking maven summary must fail closed: ok=%v out=%q", ok, out)
	}
}

func TestCmakeStyleCleanBuildProgressLineBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line         string
		wantOK       bool
		wantTerminal bool
	}{
		{line: "make[1]: Entering directory '/repo/build'", wantOK: true},
		{line: "gmake[2]: Leaving directory '/repo/build'", wantOK: true},
		{line: "Scanning dependencies of target app", wantOK: true},
		{line: "Built target app", wantOK: true, wantTerminal: true},
		{line: "ninja: no work to do.", wantOK: true, wantTerminal: true},
		{line: "[ 25%] Automatic MOC for target app", wantOK: true},
		{line: "[ 26%] Automatic RCC for target app", wantOK: true},
		{line: "[ 27%] Automatic UIC for target app", wantOK: true},
		{line: "[ 30%] Building CUDA object src/CMakeFiles/app.dir/kernel.cu.o", wantOK: true},
		{line: "[ 35%] Building Fortran object src/CMakeFiles/app.dir/main.f90.o", wantOK: true},
		{line: "[ 40%] Generating generated/version.hpp", wantOK: true},
		{line: "[ 60%] Copying assets", wantOK: true},
		{line: "[100%] Linking C shared library libapp.dylib", wantOK: true, wantTerminal: true},
		{line: "[100%] Linking C static library libapp.a", wantOK: true, wantTerminal: true},
		{line: "[100%] Linking CXX static library libapp.a", wantOK: true, wantTerminal: true},
		{line: "[100%] Linking CUDA executable cuda-app", wantOK: true, wantTerminal: true},
		{line: "[100%] Linking Fortran executable solver", wantOK: true, wantTerminal: true},
		{line: "[100%] Linking CXX shared library libapp.dylib", wantOK: true, wantTerminal: true},
		{line: "[1/2] Generating generated/version.hpp", wantOK: true},
		{line: "[2/2] Linking CXX executable app", wantOK: true, wantTerminal: true},
		{line: "["},
		{line: "[] Building CXX object app.o"},
		{line: "[ /2] Building CXX object app.o"},
		{line: "[1/ ] Building CXX object app.o"},
		{line: "[x/2] Building CXX object app.o"},
		{line: "[1/2] Running custom command"},
		{line: "[ 50%]"},
		{line: "[ 50%] Running custom command"},
		{line: "[100] Building CXX object app.o"},
		{line: "cc -O2 main.c -o app"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			gotOK, gotTerminal := cmakeStyleCleanBuildProgressLine(tt.line)
			if gotOK != tt.wantOK || gotTerminal != tt.wantTerminal {
				t.Fatalf("line classification mismatch: got ok=%v terminal=%v", gotOK, gotTerminal)
			}
		})
	}
}

func TestCmakeStyleBuildOutputUnsafeAndBoundaryBranches(t *testing.T) {
	t.Parallel()

	tooSmall, ok := compactCmakeStyleCleanBuildOutput("Built target app", len("[make] ok\n"), "make")
	if ok || tooSmall != nil {
		t.Fatalf("non-shrinking make/cmake compaction must fail closed: ok=%v out=%q", ok, tooSmall)
	}

	emptyOut, ok := compactCmakeStyleCleanBuildOutput("\n \n", 100, "cmake --build")
	if ok || emptyOut != nil {
		t.Fatalf("empty non-direct cmake-style progress must fail closed: ok=%v out=%q", ok, emptyOut)
	}

	unsafeMarkers := []string{
		"make[1]: *** [all] Error 2",
		"undefined reference to `main'",
		"make: *** No rule to make target 'all'. Stop.",
		"recipe for target 'app' failed",
		"make[1]: Leaving directory with error '/repo/build'",
	}
	for _, marker := range unsafeMarkers {
		marker := marker
		t.Run(marker, func(t *testing.T) {
			t.Parallel()
			if !cmakeStyleBuildOutputHasUnsafeSignal(marker) {
				t.Fatalf("unsafe marker not detected: %q", marker)
			}
		})
	}

	safeOut := "\n" + cmakeNinjaCleanFixture(2) + "\n"
	if cmakeStyleBuildOutputHasUnsafeSignal(safeOut) {
		t.Fatalf("clean ninja-style output should not have unsafe signal: %q", safeOut)
	}
}

func TestTryCompactBuildOutputPackageScriptWebBuildCleanOutput(t *testing.T) {
	t.Parallel()

	var vite strings.Builder
	vite.WriteString("> web@1.0.0 build /repo\n")
	vite.WriteString("> vite build\n")
	vite.WriteString(viteBuildCleanFixture())
	out, ok := TryCompactBuildOutput([]string{"pnpm", "run", "build"}, []byte(vite.String()))
	if !ok || string(out) != "[vite build] ok\n" {
		t.Fatalf("pnpm run build vite clean output: ok=%v out=%q", ok, out)
	}

	var next strings.Builder
	next.WriteString("> app@1.0.0 build /repo\n")
	next.WriteString("> next build\n")
	next.WriteString(nextBuildCleanFixture())
	out, ok = TryCompactBuildOutput([]string{"npm", "run", "build"}, []byte(next.String()))
	if !ok || string(out) != "[next build] ok\n" {
		t.Fatalf("npm run build next clean output: ok=%v out=%q", ok, out)
	}
}

func makeCmakeStyleCleanFixture(files int) string {
	var b strings.Builder
	b.WriteString("make[1]: Entering directory '/repo/build'\n")
	b.WriteString("Consolidate compiler generated dependencies of target app\n")
	for i := 0; i < files; i++ {
		fmt.Fprintf(&b, "[%3d%%] Building CXX object src/CMakeFiles/app.dir/generated/object_%02d.cpp.o\n", i+1, i)
	}
	b.WriteString("[100%] Linking CXX executable app\n")
	b.WriteString("[100%] Built target app\n")
	b.WriteString("make[1]: Leaving directory '/repo/build'\n")
	return b.String()
}

func cmakeBuildCleanFixture(files int) string {
	var b strings.Builder
	b.WriteString("Consolidate compiler generated dependencies of target slimference\n")
	for i := 0; i < files; i++ {
		fmt.Fprintf(&b, "[%3d%%] Building C object src/CMakeFiles/slimference.dir/generated/object_%02d.c.o\n", i+1, i)
	}
	b.WriteString("[100%] Linking C executable slimference\n")
	b.WriteString("[100%] Built target slimference\n")
	return b.String()
}

func cmakeNinjaCleanFixture(files int) string {
	var b strings.Builder
	for i := 1; i <= files; i++ {
		fmt.Fprintf(&b, "[%d/%d] Building CXX object src/CMakeFiles/app.dir/generated/object_%02d.cpp.o\n", i, files+1, i)
	}
	fmt.Fprintf(&b, "[%d/%d] Linking CXX executable app\n", files+1, files+1)
	return b.String()
}

func mavenCleanSuccessFixture(modules int) string {
	var b strings.Builder
	b.WriteString("[INFO] Scanning for projects...\n")
	b.WriteString("[INFO] \n")
	b.WriteString("[INFO] -----------------------< com.example:demo >------------------------\n")
	b.WriteString("[INFO] Building demo 1.0.0\n")
	b.WriteString("[INFO] --------------------------------[ jar ]---------------------------------\n")
	for i := 0; i < modules; i++ {
		fmt.Fprintf(&b, "[INFO] --- maven-resources-plugin:3.3.1:resources (default-resources-%02d) @ demo ---\n", i)
		fmt.Fprintf(&b, "[INFO] Copying %d resources from src/main/resources to target/classes\n", i+1)
	}
	b.WriteString("[INFO] --- maven-compiler-plugin:3.13.0:compile (default-compile) @ demo ---\n")
	b.WriteString("[INFO] Changes detected - recompiling the module!\n")
	b.WriteString("[INFO] Compiling 3 source files with javac [debug target 21] to target/classes\n")
	b.WriteString("[INFO] --- maven-surefire-plugin:3.2.5:test (default-test) @ demo ---\n")
	b.WriteString("[INFO] Running com.example.DemoTest\n")
	b.WriteString("[INFO] Tests run: 42, Failures: 0, Errors: 0, Skipped: 0\n")
	b.WriteString("[INFO] --- maven-jar-plugin:3.4.1:jar (default-jar) @ demo ---\n")
	b.WriteString("[INFO] Building jar: /repo/target/demo.jar\n")
	b.WriteString("[INFO] ------------------------------------------------------------------------\n")
	b.WriteString("[INFO] BUILD SUCCESS\n")
	b.WriteString("[INFO] ------------------------------------------------------------------------\n")
	b.WriteString("[INFO] Total time:  4.123 s\n")
	b.WriteString("[INFO] Finished at: 2026-06-19T01:02:03Z\n")
	b.WriteString("[INFO] ------------------------------------------------------------------------\n")
	return b.String()
}

func nextBuildCleanFixture() string {
	var b strings.Builder
	b.WriteString("Next.js 15.3.0\n")
	b.WriteString("Creating an optimized production build ...\n")
	b.WriteString("Compiled successfully in 2.8s\n")
	b.WriteString("Linting and checking validity of types ...\n")
	b.WriteString("Collecting page data ...\n")
	b.WriteString("Generating static pages (0/8) ...\n")
	b.WriteString("Generating static pages (4/8) ...\n")
	b.WriteString("Generating static pages (8/8) ...\n")
	b.WriteString("Finalizing page optimization ...\n")
	b.WriteString("Collecting build traces ...\n")
	b.WriteString("Route (app)                              Size     First Load JS\n")
	for i := 0; i < 24; i++ {
		fmt.Fprintf(&b, "/dashboard/section-%02d                  2.%02d kB        110 kB\n", i, i)
	}
	return b.String()
}

func viteBuildCleanFixture() string {
	var b strings.Builder
	b.WriteString("vite v6.3.5 building for production...\n")
	b.WriteString("transforming...\n")
	b.WriteString("240 modules transformed.\n")
	b.WriteString("rendering chunks...\n")
	b.WriteString("computing gzip size...\n")
	for i := 0; i < 24; i++ {
		fmt.Fprintf(&b, "dist/assets/chunk-%02d.js                 %0.2f kB | gzip: %0.2f kB\n", i, float64(i)+12.4, float64(i)+3.1)
	}
	b.WriteString("built in 2.31s\n")
	return b.String()
}

// Exercises pnpm exec / yarn … branches that are distinct from direct-binary and npx paths.
func TestTryCompactBuildOutput_pnpmYarnExecVariants(t *testing.T) {
	t.Parallel()
	empty := []byte("")

	njYarn, ok := TryCompactNinja([]string{"yarn", "ninja", "-C", "out"}, empty)
	if !ok || string(njYarn) != "[ninja] ok\n" {
		t.Fatalf("yarn ninja: %q", njYarn)
	}
	tscPnpm, ok := TryCompactTsc([]string{"pnpm", "exec", "tsc", "--noEmit"}, empty)
	if !ok || string(tscPnpm) != "[tsc] ok\n" {
		t.Fatalf("pnpm exec tsc: %q", tscPnpm)
	}
	tscYarn, ok := TryCompactTsc([]string{"yarn", "tsc", "--noEmit"}, empty)
	if !ok || string(tscYarn) != "[tsc] ok\n" {
		t.Fatalf("yarn tsc: %q", tscYarn)
	}
	nextPnpm, ok := TryCompactNextBuild([]string{"pnpm", "exec", "next", "build"}, empty)
	if !ok || string(nextPnpm) != "[next build] ok\n" {
		t.Fatalf("pnpm exec next build: %q", nextPnpm)
	}
	nextYarn, ok := TryCompactNextBuild([]string{"yarnpkg", "next", "build"}, empty)
	if !ok || string(nextYarn) != "[next build] ok\n" {
		t.Fatalf("yarnpkg next build: %q", nextYarn)
	}
	vitePnpm, ok := TryCompactViteBuild([]string{"pnpm.cmd", "exec", "vite", "build"}, empty)
	if !ok || string(vitePnpm) != "[vite build] ok\n" {
		t.Fatalf("pnpm.cmd exec vite build: %q", vitePnpm)
	}
	viteYarn, ok := TryCompactViteBuild([]string{"yarn.cmd", "vite", "build"}, empty)
	if !ok || string(viteYarn) != "[vite build] ok\n" {
		t.Fatalf("yarn.cmd vite build: %q", viteYarn)
	}
	wpPnpm, ok := TryCompactWebpack([]string{"pnpm", "exec", "webpack-cli"}, empty)
	if !ok || string(wpPnpm) != "[webpack] ok\n" {
		t.Fatalf("pnpm exec webpack-cli: %q", wpPnpm)
	}
	wpYarn, ok := TryCompactWebpack([]string{"yarn", "webpack"}, empty)
	if !ok || string(wpYarn) != "[webpack] ok\n" {
		t.Fatalf("yarn webpack: %q", wpYarn)
	}
	rspPnpm, ok := TryCompactRspackBuild([]string{"pnpm", "exec", "rspack", "build"}, empty)
	if !ok || string(rspPnpm) != "[rspack build] ok\n" {
		t.Fatalf("pnpm exec rspack build: %q", rspPnpm)
	}
	rspYarn, ok := TryCompactRspackBuild([]string{"yarn", "rspack", "build"}, empty)
	if !ok || string(rspYarn) != "[rspack build] ok\n" {
		t.Fatalf("yarn rspack build: %q", rspYarn)
	}
	parPnpm, ok := TryCompactParcelBuild([]string{"pnpm", "exec", "parcel", "build"}, empty)
	if !ok || string(parPnpm) != "[parcel build] ok\n" {
		t.Fatalf("pnpm exec parcel build: %q", parPnpm)
	}
	parYarn, ok := TryCompactParcelBuild([]string{"yarn", "parcel", "build"}, empty)
	if !ok || string(parYarn) != "[parcel build] ok\n" {
		t.Fatalf("yarn parcel build: %q", parYarn)
	}
	rollupPnpm, ok := TryCompactRollupConfig([]string{"pnpm", "exec", "rollup", "-c"}, empty)
	if !ok || string(rollupPnpm) != "[rollup] ok\n" {
		t.Fatalf("pnpm exec rollup -c: %q", rollupPnpm)
	}
	rollupYarn, ok := TryCompactRollupConfig([]string{"yarn", "rollup", "--config", "r.mjs"}, empty)
	if !ok || string(rollupYarn) != "[rollup] ok\n" {
		t.Fatalf("yarn rollup --config: %q", rollupYarn)
	}
	esbPnpm, ok := TryCompactEsbuildBundle([]string{"pnpm", "exec", "esbuild", "in.ts", "--bundle", "--outfile=o.js"}, empty)
	if !ok || string(esbPnpm) != "[esbuild] ok\n" {
		t.Fatalf("pnpm exec esbuild --bundle: %q", esbPnpm)
	}
	esbYarn, ok := TryCompactEsbuildBundle([]string{"yarn", "esbuild", "x.ts", "--bundle"}, empty)
	if !ok || string(esbYarn) != "[esbuild] ok\n" {
		t.Fatalf("yarn esbuild --bundle: %q", esbYarn)
	}
	nxPnpm, ok := TryCompactNxBuild([]string{"pnpm", "exec", "nx", "build", "app"}, empty)
	if !ok || string(nxPnpm) != "[nx build] ok\n" {
		t.Fatalf("pnpm exec nx build: %q", nxPnpm)
	}
	nxYarn, ok := TryCompactNxBuild([]string{"yarn", "nx", "build", "lib"}, empty)
	if !ok || string(nxYarn) != "[nx build] ok\n" {
		t.Fatalf("yarn nx build: %q", nxYarn)
	}
	tbPnpm, ok := TryCompactTurboBuild([]string{"pnpm", "exec", "turbo", "run", "build"}, empty)
	if !ok || string(tbPnpm) != "[turbo build] ok\n" {
		t.Fatalf("pnpm exec turbo run build: %q", tbPnpm)
	}
	tbPnpm2, ok := TryCompactTurboBuild([]string{"pnpm", "exec", "turbo", "build"}, empty)
	if !ok || string(tbPnpm2) != "[turbo build] ok\n" {
		t.Fatalf("pnpm exec turbo build: %q", tbPnpm2)
	}
	tbYarn, ok := TryCompactTurboBuild([]string{"yarn", "turbo", "run", "build"}, empty)
	if !ok || string(tbYarn) != "[turbo build] ok\n" {
		t.Fatalf("yarn turbo run build: %q", tbYarn)
	}
	tbYarn2, ok := TryCompactTurboBuild([]string{"yarn", "turbo", "build"}, empty)
	if !ok || string(tbYarn2) != "[turbo build] ok\n" {
		t.Fatalf("yarn turbo build: %q", tbYarn2)
	}
	koPnpm, ok := TryCompactKoBuild([]string{"pnpm", "exec", "ko", "build", "."}, empty)
	if !ok || string(koPnpm) != "[ko build] ok\n" {
		t.Fatalf("pnpm exec ko build: %q", koPnpm)
	}
	koYarn, ok := TryCompactKoBuild([]string{"yarn", "ko", "build"}, empty)
	if !ok || string(koYarn) != "[ko build] ok\n" {
		t.Fatalf("yarn ko build: %q", koYarn)
	}
	mesYarn, ok := TryCompactMesonCompile([]string{"yarn", "meson", "compile", "-C", "b"}, empty)
	if !ok || string(mesYarn) != "[meson compile] ok\n" {
		t.Fatalf("yarn meson compile: %q", mesYarn)
	}
}

// TestTryCompactBuildOutput_missingBranches covers non-empty stdout guards, npx/pnpm/yarn
// wrapper variants, and early-return branches not exercised by the prior test functions.
func TestTryCompactBuildOutput_missingBranches(t *testing.T) {
	t.Parallel()
	empty := []byte("")
	nonempty := []byte("output\n")

	// --- TryCompactGoBuild: non-empty stdout ---
	if _, ok := TryCompactGoBuild([]string{"go", "build"}, nonempty); ok {
		t.Fatal("go build non-empty stdout")
	}
	// --- isGoBuildArgv: npx failure (len(rest)<2) ---
	if _, ok := TryCompactGoBuild([]string{"npx", "go"}, empty); ok {
		t.Fatal("npx go: no subcommand")
	}
	// --- isGoBuildArgv: pnpm exec ---
	goNpxPnpm, ok := TryCompactGoBuild([]string{"pnpm", "exec", "go", "build"}, empty)
	if !ok || string(goNpxPnpm) != "[go build] ok\n" {
		t.Fatalf("pnpm exec go build: ok=%v %q", ok, goNpxPnpm)
	}

	// --- TryCompactCargoBuild: non-empty stdout ---
	if _, ok := TryCompactCargoBuild([]string{"cargo", "build"}, nonempty); ok {
		t.Fatal("cargo build non-empty stdout")
	}
	// --- isCargoBuildArgv: npx success ---
	cbNpx, ok := TryCompactCargoBuild([]string{"npx", "cargo", "build"}, empty)
	if !ok || string(cbNpx) != "[cargo build] ok\n" {
		t.Fatalf("npx cargo build: ok=%v %q", ok, cbNpx)
	}
	// --- isCargoBuildArgv: npx failure (len<2) ---
	if _, ok := TryCompactCargoBuild([]string{"npx", "cargo"}, empty); ok {
		t.Fatal("npx cargo: no build subcommand")
	}
	// --- isCargoBuildArgv: pnpm exec ---
	cbPnpm, ok := TryCompactCargoBuild([]string{"pnpm", "exec", "cargo", "build"}, empty)
	if !ok || string(cbPnpm) != "[cargo build] ok\n" {
		t.Fatalf("pnpm exec cargo build: ok=%v %q", ok, cbPnpm)
	}

	// --- TryCompactCargoCheck: non-empty stdout ---
	if _, ok := TryCompactCargoCheck([]string{"cargo", "check"}, nonempty); ok {
		t.Fatal("cargo check non-empty stdout")
	}
	// --- isCargoCheckArgv: npx failure (len<2) ---
	if _, ok := TryCompactCargoCheck([]string{"npx", "cargo"}, empty); ok {
		t.Fatal("npx cargo: no check subcommand")
	}
	// --- isCargoCheckArgv: pnpm exec ---
	ccPnpm, ok := TryCompactCargoCheck([]string{"pnpm", "exec", "cargo", "check"}, empty)
	if !ok || string(ccPnpm) != "[cargo check] ok\n" {
		t.Fatalf("pnpm exec cargo check: ok=%v %q", ok, ccPnpm)
	}

	// --- TryCompactCargoDoc: non-empty stdout ---
	if _, ok := TryCompactCargoDoc([]string{"cargo", "doc"}, nonempty); ok {
		t.Fatal("cargo doc non-empty stdout")
	}
	// --- isCargoDocArgv: npx success ---
	cdNpx, ok := TryCompactCargoDoc([]string{"npx", "cargo", "doc"}, empty)
	if !ok || string(cdNpx) != "[cargo doc] ok\n" {
		t.Fatalf("npx cargo doc: ok=%v %q", ok, cdNpx)
	}
	// --- isCargoDocArgv: npx failure (len<2) ---
	if _, ok := TryCompactCargoDoc([]string{"npx", "cargo"}, empty); ok {
		t.Fatal("npx cargo: no doc subcommand")
	}

	// --- TryCompactMesonCompile: npx ---
	mesNpx, ok := TryCompactMesonCompile([]string{"npx", "meson", "compile", "-C", "build"}, empty)
	if !ok || string(mesNpx) != "[meson compile] ok\n" {
		t.Fatalf("npx meson compile: ok=%v %q", ok, mesNpx)
	}

	// --- isMakeCompactArgv: len < 1 (direct call) ---
	if isMakeCompactArgv([]string{}) {
		t.Fatal("isMakeCompactArgv: empty argv")
	}
	// --- TryCompactMake: pnpm exec make ---
	mkPnpm, ok := TryCompactMake([]string{"pnpm", "exec", "make", "all"}, empty)
	if !ok || string(mkPnpm) != "[make] ok\n" {
		t.Fatalf("pnpm exec make: ok=%v %q", ok, mkPnpm)
	}
	// --- TryCompactMake: yarn make ---
	mkYarn, ok := TryCompactMake([]string{"yarn", "make", "all"}, empty)
	if !ok || string(mkYarn) != "[make] ok\n" {
		t.Fatalf("yarn make: ok=%v %q", ok, mkYarn)
	}

	// --- TryCompactNinja: non-empty stdout ---
	if _, ok := TryCompactNinja([]string{"ninja"}, nonempty); ok {
		t.Fatal("ninja non-empty stdout")
	}
	// --- TryCompactNinja: npx ---
	njNpx, ok := TryCompactNinja([]string{"npx", "ninja", "-C", "out"}, empty)
	if !ok || string(njNpx) != "[ninja] ok\n" {
		t.Fatalf("npx ninja: ok=%v %q", ok, njNpx)
	}

	// --- TryCompactCmakeBuild: npx ---
	cmNpx, ok := TryCompactCmakeBuild([]string{"npx", "cmake", "--build", "."}, empty)
	if !ok || string(cmNpx) != "[cmake --build] ok\n" {
		t.Fatalf("npx cmake --build: ok=%v %q", ok, cmNpx)
	}
	// --- TryCompactCmakeBuild: pnpm exec ---
	cmPnpm, ok := TryCompactCmakeBuild([]string{"pnpm", "exec", "cmake", "--build", "."}, empty)
	if !ok || string(cmPnpm) != "[cmake --build] ok\n" {
		t.Fatalf("pnpm exec cmake --build: ok=%v %q", ok, cmPnpm)
	}

	// --- TryCompactTsc: len < 1 ---
	if _, ok := TryCompactTsc([]string{}, empty); ok {
		t.Fatal("tsc: empty argv")
	}

	// --- TryCompactWebpack: len < 1 ---
	if _, ok := TryCompactWebpack([]string{}, empty); ok {
		t.Fatal("webpack: empty argv")
	}

	// --- TryCompactEsbuildBundle: non-esbuild binary with --bundle (final return) ---
	if _, ok := TryCompactEsbuildBundle([]string{"not-esbuild", "file.ts", "--bundle"}, empty); ok {
		t.Fatal("not-esbuild --bundle: should return false")
	}

	// --- TryCompactTurboBuild: npx turbo build (without run) ---
	tbNpxDirect, ok := TryCompactTurboBuild([]string{"npx", "turbo", "build"}, empty)
	if !ok || string(tbNpxDirect) != "[turbo build] ok\n" {
		t.Fatalf("npx turbo build: ok=%v %q", ok, tbNpxDirect)
	}

	// --- TryCompactNpmRunBuild: non-empty stdout ---
	if _, ok := TryCompactNpmRunBuild([]string{"npm", "run", "build"}, nonempty); ok {
		t.Fatal("npm run build non-empty stdout")
	}

	// --- TryCompactPnpmRunBuild: wrong subcommand (L630) and non-empty stdout (L633) ---
	if _, ok := TryCompactPnpmRunBuild([]string{"pnpm", "run", "test"}, empty); ok {
		t.Fatal("pnpm run test not build")
	}
	if _, ok := TryCompactPnpmRunBuild([]string{"pnpm", "run", "build"}, nonempty); ok {
		t.Fatal("pnpm run build non-empty stdout")
	}

	// --- TryCompactYarnRunBuild: non-empty stdout ---
	if _, ok := TryCompactYarnRunBuild([]string{"yarn", "run", "build"}, nonempty); ok {
		t.Fatal("yarn run build non-empty stdout")
	}

	// --- TryCompactMvn: yarn ---
	mvYarn, ok := TryCompactMvn([]string{"yarn", "mvnw", "-q", "package"}, empty)
	if !ok || string(mvYarn) != "[mvn] ok\n" {
		t.Fatalf("yarn mvnw: ok=%v %q", ok, mvYarn)
	}

	// --- TryCompactGradle: pnpm ---
	grPnpm, ok := TryCompactGradle([]string{"pnpm", "exec", "gradle", "build"}, empty)
	if !ok || string(grPnpm) != "[gradle build] ok\n" {
		t.Fatalf("pnpm exec gradle build: ok=%v %q", ok, grPnpm)
	}

	// --- TryCompactZigBuild: yarn ---
	zbYarn, ok := TryCompactZigBuild([]string{"yarn", "zig", "build"}, empty)
	if !ok || string(zbYarn) != "[zig build] ok\n" {
		t.Fatalf("yarn zig build: ok=%v %q", ok, zbYarn)
	}

	// --- TryCompactJust: len < 1 ---
	if _, ok := TryCompactJust([]string{}, empty); ok {
		t.Fatal("just: empty argv")
	}
	// --- TryCompactJust: pnpm exec ---
	jPnpm, ok := TryCompactJust([]string{"pnpm", "exec", "just", "ci"}, empty)
	if !ok || string(jPnpm) != "[just] ok\n" {
		t.Fatalf("pnpm exec just ci: ok=%v %q", ok, jPnpm)
	}

	// --- TryCompactBazelBuild: yarn ---
	bzYarn, ok := TryCompactBazelBuild([]string{"yarn", "bazel", "build", "//x"}, empty)
	if !ok || string(bzYarn) != "[bazel build] ok\n" {
		t.Fatalf("yarn bazel build: ok=%v %q", ok, bzYarn)
	}

	// --- TryCompactSwiftBuild: npx ---
	swNpx, ok := TryCompactSwiftBuild([]string{"npx", "swift", "build"}, empty)
	if !ok || string(swNpx) != "[swift build] ok\n" {
		t.Fatalf("npx swift build: ok=%v %q", ok, swNpx)
	}
	// --- TryCompactSwiftBuild: pnpm exec ---
	swPnpm, ok := TryCompactSwiftBuild([]string{"pnpm", "exec", "swift", "build"}, empty)
	if !ok || string(swPnpm) != "[swift build] ok\n" {
		t.Fatalf("pnpm exec swift build: ok=%v %q", ok, swPnpm)
	}

	// --- TryCompactMoonRunBuild: moon argv[1] != "run" ---
	if _, ok := TryCompactMoonRunBuild([]string{"moon", "build", "app"}, empty); ok {
		t.Fatal("moon build: argv[1] not run")
	}
	// --- TryCompactMoonRunBuild: moon run task != build ---
	if _, ok := TryCompactMoonRunBuild([]string{"moon", "run", "lint"}, empty); ok {
		t.Fatal("moon run lint: not build")
	}
	// --- TryCompactMoonRunBuild: npx moon, rest[1] != "run" ---
	if _, ok := TryCompactMoonRunBuild([]string{"npx", "moon", "build", "app"}, empty); ok {
		t.Fatal("npx moon build: not run")
	}
	// --- TryCompactMoonRunBuild: npx moon run, task != build ---
	if _, ok := TryCompactMoonRunBuild([]string{"npx", "moon", "run", "test"}, empty); ok {
		t.Fatal("npx moon run test: not build")
	}
	// --- TryCompactMoonRunBuild: pnpm moon, argv[3] != "run" ---
	if _, ok := TryCompactMoonRunBuild([]string{"pnpm", "exec", "moon", "build", "app"}, empty); ok {
		t.Fatal("pnpm moon build: not run")
	}
	// --- TryCompactMoonRunBuild: pnpm moon run, task != build ---
	if _, ok := TryCompactMoonRunBuild([]string{"pnpm", "exec", "moon", "run", "test"}, empty); ok {
		t.Fatal("pnpm moon run test: not build")
	}
	// --- TryCompactMoonRunBuild: yarn moon, argv[2] != "run" ---
	if _, ok := TryCompactMoonRunBuild([]string{"yarn", "moon", "build", "app"}, empty); ok {
		t.Fatal("yarn moon build: not run")
	}
	// --- TryCompactMoonRunBuild: yarn moon run, task != build ---
	if _, ok := TryCompactMoonRunBuild([]string{"yarn", "moon", "run", "test"}, empty); ok {
		t.Fatal("yarn moon run test: not build")
	}

	// --- TryCompactPackBuild: pnpm exec ---
	pkPnpm, ok := TryCompactPackBuild([]string{"pnpm", "exec", "pack", "build"}, empty)
	if !ok || string(pkPnpm) != "[pack build] ok\n" {
		t.Fatalf("pnpm exec pack build: ok=%v %q", ok, pkPnpm)
	}
	// --- TryCompactPackBuild: yarn ---
	pkYarn, ok := TryCompactPackBuild([]string{"yarn", "pack", "build"}, empty)
	if !ok || string(pkYarn) != "[pack build] ok\n" {
		t.Fatalf("yarn pack build: ok=%v %q", ok, pkYarn)
	}
	// --- TryCompactNinja: len<1 ---
	if _, ok := TryCompactNinja([]string{}, empty); ok {
		t.Fatal("ninja: len<1 should return false")
	}
}

func TestTryCompactBuildOutput_nonEmptySuccess(t *testing.T) {
	t.Parallel()
	// go build success with verbose output
	input := `# github.com/myapp/cmd
# Compiled successfully
Build succeeded with 0 errors.
`
	out, ok := TryCompactBuildOutput([]string{"go", "build", "./..."}, []byte(input))
	if !ok {
		t.Fatalf("expected compact success, got pass-through; input=%q", input)
	}
	s := string(out)
	if s != "[go build] ok\n" {
		t.Errorf("want [go build] ok, got: %q", s)
	}
}

func TestTryCompactCargoBuildCleanProgressOutput(t *testing.T) {
	t.Parallel()

	buildInput := cargoBuildCleanProgressFixture("Compiling", 40)
	out, ok := TryCompactCargoBuild([]string{"cargo", "build", "--workspace"}, []byte(buildInput))
	if !ok || string(out) != "[cargo build] ok\n" {
		t.Fatalf("cargo build clean: ok=%v out=%q", ok, out)
	}
	checkInput := cargoBuildCleanProgressFixture("Checking", 40)
	checkOut, ok := TryCompactBuildOutput([]string{"pnpm", "exec", "cargo", "check", "--all-targets"}, []byte(checkInput))
	if !ok || string(checkOut) != "[cargo check] ok\n" {
		t.Fatalf("cargo check clean through build chain: ok=%v out=%q", ok, checkOut)
	}
	docInput := cargoBuildCleanProgressFixture("Documenting", 40)
	docOut, ok := TryCompactCargoDoc([]string{"yarn", "cargo", "doc", "--no-deps"}, []byte(docInput))
	if !ok || string(docOut) != "[cargo doc] ok\n" {
		t.Fatalf("cargo doc clean: ok=%v out=%q", ok, docOut)
	}
	if len(out) >= len(buildInput) || len(checkOut) >= len(checkInput) || len(docOut) >= len(docInput) {
		t.Fatal("cargo clean summaries must be shorter than original progress output")
	}
}

func TestTryCompactCargoBuildCleanProgressOutputGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "warning", input: cargoBuildCleanProgressFixture("Compiling", 4) + "warning: generated binding is deprecated\n"},
		{name: "error", input: "   Compiling slimtest v0.1.0 (/repo/slimtest)\nerror[E0308]: mismatched types\n"},
		{name: "note", input: cargoBuildCleanProgressFixture("Checking", 4) + "note: run with `RUST_BACKTRACE=1`\n"},
		{name: "help", input: cargoBuildCleanProgressFixture("Checking", 4) + "help: remove this binding\n"},
		{name: "unknown progress", input: "    Updating crates.io index\n    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.12s\n"},
		{name: "missing finished", input: "    Checking slimtest v0.1.0 (/repo/slimtest)\n"},
		{name: "finished only", input: "    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.12s\n"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := TryCompactCargoBuild([]string{"cargo", "build"}, []byte(tt.input)); ok {
				t.Fatalf("unsafe cargo build output compacted: %q", tt.input)
			}
		})
	}
	if _, ok := TryCompactCargoBuild([]string{"cargo", "test"}, []byte(cargoBuildCleanProgressFixture("Compiling", 4))); ok {
		t.Fatal("cargo test must not use cargo build parser")
	}
}

func cargoBuildCleanProgressFixture(verb string, packages int) string {
	var out strings.Builder
	for i := 0; i < packages; i++ {
		fmt.Fprintf(&out, "    %s slimtest_%03d v0.1.0 (/repo/crates/slimtest_%03d)\n", verb, i, i)
	}
	out.WriteString("    Finished `dev` profile [unoptimized + debuginfo] target(s) in 1.23s\n")
	return out.String()
}

func TestTryCompactBuildOutput_successWithWarningFailsOpen(t *testing.T) {
	t.Parallel()
	input := `# github.com/myapp/cmd
# Compiled successfully
warning: generated binding is deprecated
Build succeeded with 0 errors and 1 warning.
`
	if out, ok := TryCompactBuildOutput([]string{"go", "build", "./..."}, []byte(input)); ok {
		t.Fatalf("build success with warning must fail open, got %q", out)
	}
}

func TestTryCompactBuildOutput_DoesNotEatMypySuccess(t *testing.T) {
	t.Parallel()
	input := "Using mypy cache metadata for 188 modules\nSuccess: no issues found in 188 source files\n"
	if out, ok := TryCompactBuildOutput([]string{"mypy", "src"}, []byte(input)); ok {
		t.Fatalf("build reducer must not preempt mypy success: %q", out)
	}
	out, ok := TryCompactLintOutput([]string{"mypy", "src"}, []byte(input))
	if !ok || !strings.Contains(string(out), "[mypy] ok (Success: no issues found in 188 source files)") {
		t.Fatalf("mypy lint reducer did not compact success precisely: ok=%v out=%q", ok, out)
	}
}

func TestTryCompactBuildOutput_nonEmptyFailure(t *testing.T) {
	t.Parallel()
	// cargo build failure output
	input := `   Compiling myapp v0.1.0 (/home/user/myapp)
error[E0308]: mismatched types
  --> src/main.rs:12:5
   |
12 |     return "hello";
   |            ^^^^^^^ expected (), found &str

error: aborting due to 1 previous error

For more information about this error, try 'rustc --explain E0308'.
error: could not compile 'myapp' due to 1 previous error
`
	out, ok := TryCompactBuildOutput([]string{"cargo", "build"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact failure output, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "[cargo build] FAILED") {
		t.Errorf("want FAILED header, got: %q", s)
	}
	if !strings.Contains(s, "E0308") {
		t.Errorf("want error code, got: %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact should be shorter: %d vs %d", len(s), len(input))
	}
}

func TestTryCompactBuildOutput_FallbackExtractsBuildErrors(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"step 1 completed",
		"step 2 completed",
		"step 3 completed",
		"fatal: linker could not resolve symbol",
		"step 4 completed",
		"step 5 completed",
		"step 6 completed",
	}, "\n")
	out, ok := TryCompactBuildOutput([]string{"zig", "build"}, []byte(input))
	if !ok {
		t.Fatal("expected fallback build-error compaction")
	}
	got := string(out)
	if !strings.Contains(got, "[zig build] FAILED") || !strings.Contains(got, "fatal: linker") {
		t.Fatalf("unexpected fallback summary: %q", got)
	}
}

func TestTryCompactBuildOutput_TypeScriptFallbackFailsOpenWhenParserDeclines(t *testing.T) {
	t.Parallel()
	weakSummary := strings.Repeat("tsc progress\n", 40) + "Found 2 errors in 2 files.\n"
	if out, ok := TryCompactBuildOutput([]string{"tsc", "--noEmit"}, []byte(weakSummary)); ok {
		t.Fatalf("summary-only TypeScript failure must fail open, got %q", out)
	}
	withSource := strings.Repeat("tsc progress\n", 40) +
		"src/App.tsx(12,7): error TS2322: Type 'string' is not assignable to type 'number'.\n" +
		"import { missingName } from './missing';\n" +
		"Found 1 error in 1 file.\n"
	if out, ok := TryCompactBuildOutput([]string{"tsc", "--noEmit"}, []byte(withSource)); ok {
		t.Fatalf("TypeScript failure with source context must fail open, got %q", out)
	}
}

// TestBuildToolLabel_pnpmYarnNinjaBazelZig covers the four uncovered branches in buildToolLabel:
// pnpm exec ninja, yarn ninja, npx zig build, npx bazel build.
func TestBuildToolLabel_pnpmYarnNinjaBazelZig(t *testing.T) {
	t.Parallel()
	// pnpm exec ninja → "ninja" (line 1103-1105)
	if got := buildToolLabel([]string{"pnpm", "exec", "ninja", "-C", "out"}); got != "ninja" {
		t.Errorf("pnpm exec ninja: want 'ninja', got %q", got)
	}
	// yarn ninja → "ninja" (line 1106-1108)
	if got := buildToolLabel([]string{"yarn", "ninja", "-j8"}); got != "ninja" {
		t.Errorf("yarn ninja: want 'ninja', got %q", got)
	}
	// npx zig build → "zig build" (line 1113-1115)
	if got := buildToolLabel([]string{"npx", "zig", "build"}); got != "zig build" {
		t.Errorf("npx zig build: want 'zig build', got %q", got)
	}
	// npx bazel build → "bazel build" (line 1120-1122)
	if got := buildToolLabel([]string{"npx", "bazel", "build", "//..."}); got != "bazel build" {
		t.Errorf("npx bazel build: want 'bazel build', got %q", got)
	}
}

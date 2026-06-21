package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactJSBundlerCleanOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
		want   string
	}{
		{
			name:   "webpack",
			argv:   []string{"webpack", "--mode", "production"},
			stdout: jsBundlerWebpackCleanFixture(18),
			want:   "[webpack] ok\n",
		},
		{
			name:   "rspack",
			argv:   []string{"pnpm", "exec", "rspack", "build"},
			stdout: jsBundlerRspackCleanFixture(18),
			want:   "[rspack build] ok\n",
		},
		{
			name:   "parcel",
			argv:   []string{"parcel", "build", "src/index.html"},
			stdout: jsBundlerParcelCleanFixture(18),
			want:   "[parcel build] ok\n",
		},
		{
			name:   "rollup",
			argv:   []string{"npx", "-y", "rollup", "-c"},
			stdout: jsBundlerRollupCleanFixture(18),
			want:   "[rollup] ok\n",
		},
		{
			name:   "esbuild",
			argv:   []string{"yarn", "esbuild", "src/index.ts", "--bundle", "--outfile=dist/index.js"},
			stdout: jsBundlerEsbuildCleanFixture(18),
			want:   "[esbuild] ok\n",
		},
		{
			name:   "tsup",
			argv:   []string{"npx", "-y", "tsup", "src/index.ts", "--format", "cjs,esm", "--dts"},
			stdout: jsBundlerTsupCleanFixture(18),
			want:   "[tsup] ok\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ok := TryCompactBuildOutput(tt.argv, []byte(tt.stdout))
			if !ok || string(out) != tt.want {
				t.Fatalf("%s clean output: ok=%v out=%q", tt.name, ok, out)
			}
		})
	}
}

func TestTryCompactJSBundlerPackageScriptCleanOutput(t *testing.T) {
	t.Parallel()

	var stdout strings.Builder
	stdout.WriteString("> web@1.0.0 build /repo\n")
	stdout.WriteString("> webpack --mode production\n")
	stdout.WriteString(jsBundlerWebpackCleanFixture(12))
	out, ok := TryCompactBuildOutput([]string{"pnpm", "run", "build"}, []byte(stdout.String()))
	if !ok || string(out) != "[webpack] ok\n" {
		t.Fatalf("package script webpack clean output: ok=%v out=%q", ok, out)
	}

	stdout.Reset()
	stdout.WriteString("> web@1.0.0 build /repo\n")
	stdout.WriteString("> tsup src/index.ts --format cjs,esm --dts\n")
	stdout.WriteString(jsBundlerTsupCleanFixture(10))
	out, ok = TryCompactBuildOutput([]string{"npm", "run", "build"}, []byte(stdout.String()))
	if !ok || string(out) != "[tsup] ok\n" {
		t.Fatalf("package script tsup clean output: ok=%v out=%q", ok, out)
	}
}

func TestTryCompactJSBundlerCleanOutputFailOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
		try    func([]string, []byte) ([]byte, bool)
	}{
		{
			name:   "webpack warning",
			argv:   []string{"webpack", "--mode", "production"},
			stdout: jsBundlerWebpackCleanFixture(8) + "WARNING in asset size limit: The following asset exceeds the recommended size limit\n",
			try:    TryCompactWebpack,
		},
		{
			name:   "rspack failed",
			argv:   []string{"rspack", "build"},
			stdout: "Rspack compiled with 1 error in 120 ms\nERROR in ./src/index.ts\nModule not found\n",
			try:    TryCompactRspackBuild,
		},
		{
			name:   "parcel weak built",
			argv:   []string{"parcel", "build", "src/index.html"},
			stdout: "Built in 120ms\n",
			try:    TryCompactParcelBuild,
		},
		{
			name:   "rollup no config",
			argv:   []string{"rollup", "src/index.ts"},
			stdout: jsBundlerRollupCleanFixture(8),
			try:    TryCompactRollupConfig,
		},
		{
			name:   "rollup warning",
			argv:   []string{"rollup", "-c"},
			stdout: jsBundlerRollupCleanFixture(8) + "(!) Circular dependency\n",
			try:    TryCompactRollupConfig,
		},
		{
			name:   "esbuild no bundle",
			argv:   []string{"esbuild", "src/index.ts", "--outfile=dist/index.js"},
			stdout: jsBundlerEsbuildCleanFixture(8),
			try:    TryCompactEsbuildBundle,
		},
		{
			name:   "esbuild error",
			argv:   []string{"esbuild", "src/index.ts", "--bundle", "--outfile=dist/index.js"},
			stdout: "dist/index.js 1.2kb\nerror: Could not resolve \"missing\"\n",
			try:    TryCompactEsbuildBundle,
		},
		{
			name:   "tsup dts error after esm success",
			argv:   []string{"tsup", "src/index.ts", "--dts"},
			stdout: jsBundlerTsupCleanFixture(8) + "DTS Build start\nError parsing: src/index.ts:1:0\nDTS Build error\n",
			try:    TryCompactTsupBuild,
		},
		{
			name:   "tsup esbuild bracket error",
			argv:   []string{"tsup", "src/index.ts"},
			stdout: "CLI Building entry: src/index.ts\nCLI tsup v8.5.0\nESM Build start\nX [ERROR] Could not resolve \"missing\"\nESM Build failed\n",
			try:    TryCompactTsupBuild,
		},
		{
			name:   "tsup warning-like unused import",
			argv:   []string{"tsup", "src/index.ts"},
			stdout: jsBundlerTsupCleanFixture(8) + "\"useCallback\" is imported from external module \"react\" but never used in \"dist/chunk.js\".\n",
			try:    TryCompactTsupBuild,
		},
		{
			name: "tsup started phase without success",
			argv: []string{"tsup", "src/index.ts", "--dts"},
			stdout: strings.Join([]string{
				"CLI Building entry: src/index.ts",
				"CLI tsup v8.5.0",
				"ESM Build start",
				"ESM dist/index.mjs 182.00 B",
				"ESM \u26a1\ufe0f Build success in 7ms",
				"DTS Build start",
				"DTS dist/index.d.ts 4.00 KB",
				"",
			}, "\n"),
			try: TryCompactTsupBuild,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out, ok := tt.try(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("unsafe or weak bundler output compacted: %q", out)
			}
		})
	}
}

func TestTryCompactJSBundlerCleanOutputStrictBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
		try    func([]string, []byte) ([]byte, bool)
	}{
		{
			name:   "webpack too small",
			argv:   []string{"webpack", "--mode", "production"},
			stdout: "webpack",
			try:    TryCompactWebpack,
		},
		{
			name:   "webpack missing success",
			argv:   []string{"webpack", "--mode", "production"},
			stdout: strings.Repeat("asset chunk.js 20 KiB\n", 4),
			try:    TryCompactWebpack,
		},
		{
			name:   "rspack missing artifact",
			argv:   []string{"rspack", "build"},
			stdout: strings.Repeat("Rspack compiled successfully in 820 ms\n", 4),
			try:    TryCompactRspackBuild,
		},
		{
			name:   "parcel missing built marker",
			argv:   []string{"parcel", "build", "src/index.html"},
			stdout: strings.Repeat("dist/chunk.js 10 KB\n", 4),
			try:    TryCompactParcelBuild,
		},
		{
			name:   "rollup missing artifact",
			argv:   []string{"rollup", "--config"},
			stdout: strings.Repeat("created bundle in 420ms\n", 4),
			try:    TryCompactRollupConfig,
		},
		{
			name:   "esbuild missing done",
			argv:   []string{"esbuild", "src/index.ts", "--bundle", "--outfile=dist/index.js"},
			stdout: strings.Repeat("dist/index.js  12 kb\n", 4),
			try:    TryCompactEsbuildBundle,
		},
		{
			name:   "tsup missing cli marker",
			argv:   []string{"tsup", "src/index.ts"},
			stdout: strings.Repeat("ESM dist/index.mjs 182.00 B\nESM Build success in 7ms\n", 3),
			try:    TryCompactTsupBuild,
		},
		{
			name:   "tsup missing artifact",
			argv:   []string{"tsup", "src/index.ts"},
			stdout: strings.Repeat("CLI Building entry: src/index.ts\nCLI tsup v8.5.0\nESM Build success in 7ms\n", 3),
			try:    TryCompactTsupBuild,
		},
		{
			name:   "tsup missing success",
			argv:   []string{"tsup", "src/index.ts"},
			stdout: strings.Repeat("CLI Building entry: src/index.ts\nCLI tsup v8.5.0\nESM dist/index.mjs 182.00 B\n", 3),
			try:    TryCompactTsupBuild,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out, ok := tt.try(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("strict branch compacted unsafe or weak bundler output: %q", out)
			}
		})
	}
}

func TestWebBuildSignalHelpers(t *testing.T) {
	t.Parallel()

	errorCases := []struct {
		line string
		want bool
	}{
		{"0 errors", false},
		{"error: failed", true},
		{"module error detected", true},
		{"src/index.ts: error TS1005", true},
		{"errors: 1", true},
		{"1 errors", true},
		{"x [error] could not resolve", true},
		{"compiled successfully", false},
	}
	for _, tc := range errorCases {
		if got := webBuildLineHasErrorSignal(tc.line); got != tc.want {
			t.Fatalf("webBuildLineHasErrorSignal(%q)=%v want %v", tc.line, got, tc.want)
		}
	}

	artifactCases := []struct {
		name string
		text string
		want bool
	}{
		{name: "empty lines ignored", text: "\n\n", want: false},
		{name: "path and size", text: "dist/app.js 12 kb\n", want: true},
		{name: "asset line", text: "asset app.js 12 KiB [emitted]\n", want: true},
		{name: "created line", text: "created dist/index.js in 420ms\n", want: true},
		{name: "path without size", text: "dist/app.js generated\n", want: false},
		{name: "size without path", text: "bundle 12 kb\n", want: false},
	}
	for _, tc := range artifactCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := webBuildHasArtifactSignal(tc.text); got != tc.want {
				t.Fatalf("webBuildHasArtifactSignal(%q)=%v want %v", tc.text, got, tc.want)
			}
		})
	}
}

func jsBundlerWebpackCleanFixture(entries int) string {
	var b strings.Builder
	for i := range entries {
		fmt.Fprintf(&b, "asset chunk-%02d.js %d KiB [emitted] [minimized] (name: chunk-%02d)\n", i, 20+i, i)
	}
	b.WriteString("./src/index.ts 128 bytes [built] [code generated]\n")
	b.WriteString("webpack 5.97.1 compiled successfully in 1234 ms\n")
	return b.String()
}

func jsBundlerRspackCleanFixture(entries int) string {
	var b strings.Builder
	for i := range entries {
		fmt.Fprintf(&b, "asset chunk-%02d.js %d KiB [emitted] (name: chunk-%02d)\n", i, 18+i, i)
	}
	b.WriteString("Rspack compiled successfully in 820 ms\n")
	return b.String()
}

func jsBundlerParcelCleanFixture(entries int) string {
	var b strings.Builder
	for i := range entries {
		fmt.Fprintf(&b, "dist/chunk-%02d.js    %d KB    20ms\n", i, 10+i)
	}
	b.WriteString("Built in 1.23s\n")
	return b.String()
}

func jsBundlerRollupCleanFixture(entries int) string {
	var b strings.Builder
	for i := range entries {
		fmt.Fprintf(&b, "dist/chunk-%02d.js -> dist/chunk-%02d.min.js\n", i, i)
	}
	b.WriteString("created dist/index.js in 420ms\n")
	return b.String()
}

func jsBundlerEsbuildCleanFixture(entries int) string {
	var b strings.Builder
	for i := range entries {
		fmt.Fprintf(&b, "dist/chunk-%02d.js  %d kb\n", i, 12+i)
	}
	b.WriteString("Done in 45ms\n")
	return b.String()
}

func jsBundlerTsupCleanFixture(entries int) string {
	var b strings.Builder
	b.WriteString("CLI Building entry: src/index.ts\n")
	b.WriteString("CLI Using tsconfig: tsconfig.json\n")
	b.WriteString("CLI tsup v8.5.0\n")
	b.WriteString("CLI Target: node18\n")
	b.WriteString("CLI Cleaning output folder\n")
	b.WriteString("ESM Build start\n")
	b.WriteString("CJS Build start\n")
	for i := range entries {
		fmt.Fprintf(&b, "ESM dist/chunk-%02d.mjs     %d.00 KB\n", i, 10+i)
		fmt.Fprintf(&b, "CJS dist/chunk-%02d.js      %d.00 KB\n", i, 11+i)
	}
	b.WriteString("ESM \u26a1\ufe0f Build success in 42ms\n")
	b.WriteString("CJS \u26a1\ufe0f Build success in 44ms\n")
	b.WriteString("DTS Build start\n")
	b.WriteString("DTS \u26a1\ufe0f Build success in 320ms\n")
	b.WriteString("DTS dist/index.d.ts 4.00 KB\n")
	return b.String()
}

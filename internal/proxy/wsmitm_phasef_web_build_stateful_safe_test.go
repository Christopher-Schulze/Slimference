package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeWebBuildCleanOutputCompactsFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		output    string
		want      string
		forbidden string
	}{
		{
			name:      "next build",
			command:   "next build",
			output:    webBuildCleanEnvelope("next-safe", wssNextBuildCleanFixture()),
			want:      "[next build] ok",
			forbidden: "/dashboard/section-39",
		},
		{
			name:      "vite build",
			command:   "vite build",
			output:    webBuildCleanEnvelope("vite-safe", wssViteBuildCleanFixture()),
			want:      "[vite build] ok",
			forbidden: "dist/assets/chunk-39.js",
		},
		{
			name:      "webpack",
			command:   "webpack --mode production",
			output:    webBuildCleanEnvelope("webpack-safe", wssWebpackCleanFixture()),
			want:      "[webpack] ok",
			forbidden: "asset chunk-39.js",
		},
		{
			name:      "rspack build",
			command:   "rspack build",
			output:    webBuildCleanEnvelope("rspack-safe", wssRspackCleanFixture()),
			want:      "[rspack build] ok",
			forbidden: "asset chunk-39.js",
		},
		{
			name:      "parcel build",
			command:   "parcel build src/index.html",
			output:    webBuildCleanEnvelope("parcel-safe", wssParcelCleanFixture()),
			want:      "[parcel build] ok",
			forbidden: "dist/chunk-39.js",
		},
		{
			name:      "rollup",
			command:   "rollup -c",
			output:    webBuildCleanEnvelope("rollup-safe", wssRollupCleanFixture()),
			want:      "[rollup] ok",
			forbidden: "dist/chunk-39.min.js",
		},
		{
			name:      "esbuild",
			command:   "esbuild src/index.ts --bundle --outfile=dist/index.js",
			output:    webBuildCleanEnvelope("esbuild-safe", wssEsbuildCleanFixture()),
			want:      "[esbuild] ok",
			forbidden: "dist/chunk-39.js",
		},
		{
			name:      "tsup",
			command:   "tsup src/index.ts --format cjs,esm --dts",
			output:    webBuildCleanEnvelope("tsup-safe", wssTsupCleanFixture()),
			want:      "[tsup] ok",
			forbidden: "dist/chunk-39.mjs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Compression.OutputReduce.StopSequencesEnabled = false
			cfg.Compression.OutputReduce.BeTerseHintEnabled = false
			cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
			cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
			p := New(cfg)
			adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name, "call_"+strings.ReplaceAll(tt.name, " ", "_"), tt.command, tt.output, "stateful-web-build-safe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle web build request: %v", err)
			}
			if !replace {
				t.Fatal("full-history web build clean output should compact")
			}
			body := string(env.Body)
			if !strings.Contains(body, tt.want) ||
				!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				strings.Contains(body, tt.forbidden) {
				t.Fatalf("web build output was not archive-backed compacted: %s", body)
			}
			summary := p.DebugRecorder().Last(1, false)[0]
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("stateful-safe web build should save without structured guard: %+v", summary)
			}
		})
	}
}

func TestWSSStatefulUnsafeGenericBuildSuccessSourceContextStaysGuarded(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	payload := strings.Repeat("BUILD SUCCESSFUL in 1s\n", 120) +
		"src/main.go:12: func main() {}\n" +
		"13 | return nil\n"
	env := parseWSJSON(t, wssCommandOutputRequestBody(
		"resp-gradle-source-context",
		"call_gradle_source_context",
		"gradle build",
		webBuildCleanEnvelope("gradle-source-context", payload),
		"stateful-web-build-source-context-session",
	))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle generic build source-context request: %v", err)
	}
	body := string(env.Body)
	if replace ||
		strings.Contains(body, "[gradle build] ok") ||
		strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "func main() {}") ||
		!strings.Contains(body, "13 | return nil") {
		t.Fatalf("generic build success with source context should stay byte-identical: replace=%v body=%s", replace, body)
	}
}

func webBuildCleanEnvelope(id, output string) string {
	return "Chunk ID: " + id + "\n" +
		"Wall time: 0.0010 seconds\n" +
		"Process exited with code 0\n" +
		"Original token count: 10000\n" +
		"Output:\n" +
		output
}

func wssNextBuildCleanFixture() string {
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
	for i := range 40 {
		fmt.Fprintf(&b, "/dashboard/section-%02d                  2.%02d kB        110 kB\n", i, i)
	}
	return b.String()
}

func wssViteBuildCleanFixture() string {
	var b strings.Builder
	b.WriteString("vite v6.3.5 building for production...\n")
	b.WriteString("transforming...\n")
	b.WriteString("240 modules transformed.\n")
	b.WriteString("rendering chunks...\n")
	b.WriteString("computing gzip size...\n")
	for i := range 40 {
		fmt.Fprintf(&b, "dist/assets/chunk-%02d.js                 %0.2f kB | gzip: %0.2f kB\n", i, float64(i)+12.4, float64(i)+3.1)
	}
	b.WriteString("built in 2.31s\n")
	return b.String()
}

func wssWebpackCleanFixture() string {
	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "asset chunk-%02d.js %d KiB [emitted] [minimized] (name: chunk-%02d)\n", i, 20+i, i)
	}
	b.WriteString("./src/index.ts 128 bytes [built] [code generated]\n")
	b.WriteString("webpack 5.97.1 compiled successfully in 1234 ms\n")
	return b.String()
}

func wssRspackCleanFixture() string {
	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "asset chunk-%02d.js %d KiB [emitted] (name: chunk-%02d)\n", i, 18+i, i)
	}
	b.WriteString("Rspack compiled successfully in 820 ms\n")
	return b.String()
}

func wssParcelCleanFixture() string {
	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "dist/chunk-%02d.js    %d KB    20ms\n", i, 10+i)
	}
	b.WriteString("Built in 1.23s\n")
	return b.String()
}

func wssRollupCleanFixture() string {
	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "dist/chunk-%02d.js -> dist/chunk-%02d.min.js\n", i, i)
	}
	b.WriteString("created dist/index.js in 420ms\n")
	return b.String()
}

func wssEsbuildCleanFixture() string {
	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "dist/chunk-%02d.js  %d kb\n", i, 12+i)
	}
	b.WriteString("Done in 45ms\n")
	return b.String()
}

func wssTsupCleanFixture() string {
	var b strings.Builder
	b.WriteString("CLI Building entry: src/index.ts\n")
	b.WriteString("CLI Using tsconfig: tsconfig.json\n")
	b.WriteString("CLI tsup v8.5.0\n")
	b.WriteString("CLI Target: node18\n")
	b.WriteString("CLI Cleaning output folder\n")
	b.WriteString("ESM Build start\n")
	b.WriteString("CJS Build start\n")
	for i := range 40 {
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

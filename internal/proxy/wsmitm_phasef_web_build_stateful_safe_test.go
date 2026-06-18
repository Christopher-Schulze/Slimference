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
	for i := 0; i < 40; i++ {
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
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "dist/assets/chunk-%02d.js                 %0.2f kB | gzip: %0.2f kB\n", i, float64(i)+12.4, float64(i)+3.1)
	}
	b.WriteString("built in 2.31s\n")
	return b.String()
}

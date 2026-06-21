package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeMakeCmakeCleanProgressCompactsFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		output    string
		want      string
		forbidden string
	}{
		{
			name:      "make cmake progress",
			command:   "make -j8",
			output:    wssMakeCmakeCleanEnvelope("make-cmake-safe", wssMakeCmakeCleanProgressFixture(48)),
			want:      "[make] ok",
			forbidden: "object_47.cpp.o",
		},
		{
			name:      "cmake build progress",
			command:   "cmake --build build --parallel",
			output:    wssMakeCmakeCleanEnvelope("cmake-build-safe", wssCmakeBuildCleanProgressFixture(48)),
			want:      "[cmake --build] ok",
			forbidden: "object_47.c.o",
		},
		{
			name:      "ninja progress",
			command:   "ninja -C build",
			output:    wssMakeCmakeCleanEnvelope("ninja-safe", wssNinjaCleanProgressFixture(48)),
			want:      "[ninja] ok",
			forbidden: "object_47.cpp.o",
		},
		{
			name:      "meson compile progress",
			command:   "meson compile -C build",
			output:    wssMakeCmakeCleanEnvelope("meson-safe", wssMesonCompileCleanProgressFixture(48)),
			want:      "[meson compile] ok",
			forbidden: "object_47.c.o",
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name, "call_"+strings.ReplaceAll(tt.name, " ", "_"), tt.command, tt.output, "stateful-make-cmake-safe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle make/cmake clean build request: %v", err)
			}
			if !replace {
				t.Fatal("full-history make/cmake clean progress should compact")
			}
			body := string(env.Body)
			if !strings.Contains(body, tt.want) ||
				!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				strings.Contains(body, tt.forbidden) {
				t.Fatalf("make/cmake clean progress was not archive-backed compacted: %s", body)
			}
			summary := p.DebugRecorder().Last(1, false)[0]
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("stateful-safe make/cmake clean progress should save without structured guard: %+v", summary)
			}
		})
	}
}

func TestWSSStatefulSafeMakeCmakeUnsafeProgressRejectsFalseOK(t *testing.T) {
	tests := []struct {
		name          string
		command       string
		output        string
		mustPreserve  string
		wantUnchanged bool
	}{
		{
			name:         "make warning",
			command:      "make -j8",
			output:       wssMakeCmakeCleanEnvelope("make-warning", wssMakeCmakeCleanProgressFixture(12)+"warning: generated header is stale\n"),
			mustPreserve: "warning: generated header is stale",
		},
		{
			name:          "make arbitrary recipe",
			command:       "make",
			output:        wssMakeCmakeCleanEnvelope("make-recipe", "printf 'deploying production'\ncc -O2 main.c -o app\n"),
			mustPreserve:  "printf 'deploying production'",
			wantUnchanged: true,
		},
		{
			name:         "cmake error",
			command:      "cmake --build build",
			output:       wssMakeCmakeCleanEnvelope("cmake-error", "[ 50%] Building CXX object src/CMakeFiles/app.dir/main.cpp.o\nerror: missing semicolon\n"),
			mustPreserve: "error: missing semicolon",
		},
		{
			name:         "ninja warning",
			command:      "ninja -C build",
			output:       wssMakeCmakeCleanEnvelope("ninja-warning", wssNinjaCleanProgressFixture(12)+"warning: generated header is stale\n"),
			mustPreserve: "warning: generated header is stale",
		},
		{
			name:          "ninja arbitrary tool mode",
			command:       "ninja -t graph",
			output:        wssMakeCmakeCleanEnvelope("ninja-graph", "digraph ninja {\n  \"app\" -> \"main.o\"\n}\n"),
			mustPreserve:  "digraph ninja",
			wantUnchanged: true,
		},
		{
			name:         "meson warning",
			command:      "meson compile -C build",
			output:       wssMakeCmakeCleanEnvelope("meson-warning", wssMesonCompileCleanProgressFixture(12)+"warning: generated header is stale\n"),
			mustPreserve: "warning: generated header is stale",
		},
		{
			name:         "meson custom success",
			command:      "meson compile -C build",
			output:       wssMakeCmakeCleanEnvelope("meson-custom-success", wssMesonCompileCleanProgressFixture(8)+"Custom command says build succeeded after refreshing production config\n"),
			mustPreserve: "Custom command says build succeeded",
		},
		{
			name:          "meson verbose mode",
			command:       "meson compile -v -C build",
			output:        wssMakeCmakeCleanEnvelope("meson-verbose", wssMesonCompileCleanProgressFixture(8)),
			mustPreserve:  "object_07.c.o",
			wantUnchanged: true,
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

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name, "call_"+strings.ReplaceAll(tt.name, " ", "_"), tt.command, tt.output, "stateful-make-cmake-unsafe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe make/cmake request: %v", err)
			}
			body := string(env.Body)
			if strings.Contains(body, "[make] ok") ||
				strings.Contains(body, "[cmake --build] ok") ||
				strings.Contains(body, "[ninja] ok") ||
				strings.Contains(body, "[meson compile] ok") ||
				!strings.Contains(body, tt.mustPreserve) {
				t.Fatalf("unsafe make/cmake progress hid a diagnostic as OK: replace=%v body=%s", replace, body)
			}
			if tt.wantUnchanged && replace {
				t.Fatalf("arbitrary make recipe should stay byte-identical: body=%s", body)
			}
		})
	}
}

func wssMakeCmakeCleanEnvelope(id, output string) string {
	return "Chunk ID: " + id + "\n" +
		"Wall time: 0.0010 seconds\n" +
		"Process exited with code 0\n" +
		"Original token count: 10000\n" +
		"Output:\n" +
		output
}

func wssMakeCmakeCleanProgressFixture(files int) string {
	var b strings.Builder
	b.WriteString("make[1]: Entering directory '/repo/build'\n")
	b.WriteString("Consolidate compiler generated dependencies of target app\n")
	for i := range files {
		fmt.Fprintf(&b, "[%3d%%] Building CXX object src/CMakeFiles/app.dir/generated/object_%02d.cpp.o\n", i+1, i)
	}
	b.WriteString("[100%] Linking CXX executable app\n")
	b.WriteString("[100%] Built target app\n")
	b.WriteString("make[1]: Leaving directory '/repo/build'\n")
	return b.String()
}

func wssCmakeBuildCleanProgressFixture(files int) string {
	var b strings.Builder
	b.WriteString("Consolidate compiler generated dependencies of target slimference\n")
	for i := range files {
		fmt.Fprintf(&b, "[%3d%%] Building C object src/CMakeFiles/slimference.dir/generated/object_%02d.c.o\n", i+1, i)
	}
	b.WriteString("[100%] Linking C executable slimference\n")
	b.WriteString("[100%] Built target slimference\n")
	return b.String()
}

func wssNinjaCleanProgressFixture(files int) string {
	var b strings.Builder
	b.WriteString("ninja: Entering directory `build'\n")
	for i := range files {
		fmt.Fprintf(&b, "[%d/%d] Building CXX object src/CMakeFiles/app.dir/generated/object_%02d.cpp.o\n", i+1, files+1, i)
	}
	fmt.Fprintf(&b, "[%d/%d] Linking CXX executable app\n", files+1, files+1)
	return b.String()
}

func wssMesonCompileCleanProgressFixture(files int) string {
	var b strings.Builder
	b.WriteString("ninja: Entering directory `/repo/build'\n")
	for i := range files {
		fmt.Fprintf(&b, "[%d/%d] Compiling C object app.p/generated/object_%02d.c.o\n", i+1, files+1, i)
	}
	fmt.Fprintf(&b, "[%d/%d] Linking target app\n", files+1, files+1)
	b.WriteString("INFO: autodetecting backend as ninja\n")
	b.WriteString("INFO: calculating backend command to run: /opt/homebrew/bin/ninja -C /repo/build\n")
	return b.String()
}

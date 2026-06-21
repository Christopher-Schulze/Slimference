package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactMesonCompileCleanProgressOutput(t *testing.T) {
	t.Parallel()

	progress := mesonCompileCleanProgressFixture(24)
	out, ok := TryCompactMesonCompile([]string{"meson", "compile", "-C", "build"}, []byte(progress))
	if !ok || string(out) != "[meson compile] ok\n" {
		t.Fatalf("meson compile clean progress: ok=%v out=%q", ok, out)
	}

	wrapped, ok := TryCompactBuildOutput([]string{"pnpm", "exec", "meson", "compile", "-C", "out"}, []byte(progress))
	if !ok || string(wrapped) != "[meson compile] ok\n" {
		t.Fatalf("wrapped meson compile clean progress through build chain: ok=%v out=%q", ok, wrapped)
	}

	noWork, ok := TryCompactMesonCompile([]string{"yarn", "meson", "compile", "-C", "out"}, []byte(mesonCompileNoWorkFixture()))
	if !ok || string(noWork) != "[meson compile] ok\n" {
		t.Fatalf("meson compile no-work: ok=%v out=%q", ok, noWork)
	}
}

func TestTryCompactMesonCompileCleanProgressFailOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                     string
		argv                     []string
		stdout                   string
		allowBuildFailureSummary bool
	}{
		{
			name:   "warning",
			argv:   []string{"meson", "compile", "-C", "build"},
			stdout: mesonCompileCleanProgressFixture(8) + "warning: generated header is stale\n",
		},
		{
			name:                     "error",
			argv:                     []string{"meson", "compile", "-C", "build"},
			stdout:                   "ninja: Entering directory `/repo/build'\n[1/2] Compiling C object app.p/main.c.o\nerror: missing semicolon\n",
			allowBuildFailureSummary: true,
		},
		{
			name:   "custom command",
			argv:   []string{"meson", "compile", "-C", "build"},
			stdout: "ninja: Entering directory `/repo/build'\n[1/1] Generating deployment manifest\n",
		},
		{
			name:   "missing terminal",
			argv:   []string{"meson", "compile", "-C", "build"},
			stdout: "ninja: Entering directory `/repo/build'\n[1/2] Compiling C object app.p/main.c.o\n",
		},
		{
			name:   "verbose mode",
			argv:   []string{"meson", "compile", "-v", "-C", "build"},
			stdout: mesonCompileCleanProgressFixture(8),
		},
		{
			name:   "clean mode",
			argv:   []string{"meson", "compile", "--clean", "-C", "build"},
			stdout: "",
		},
		{
			name:   "backend args",
			argv:   []string{"meson", "compile", "--ninja-args=-v", "-C", "build"},
			stdout: mesonCompileCleanProgressFixture(8),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if out, ok := TryCompactMesonCompile(tt.argv, []byte(tt.stdout)); ok {
				t.Fatalf("unsafe meson compile output compacted by strict parser: %q", out)
			}
			out, ok := TryCompactBuildOutput(tt.argv, []byte(tt.stdout))
			if tt.allowBuildFailureSummary {
				if !ok || !strings.Contains(string(out), "[meson compile] FAILED") || !strings.Contains(string(out), "error: missing semicolon") {
					t.Fatalf("meson failure summary should preserve the diagnostic: ok=%v out=%q", ok, out)
				}
				return
			}
			if ok {
				t.Fatalf("unsafe meson compile output escaped through build chain: %q", out)
			}
		})
	}
}

func TestTryCompactMesonCompileBlocksGenericSuccessFallback(t *testing.T) {
	t.Parallel()

	input := mesonCompileCleanProgressFixture(4) + "Custom command says build succeeded after refreshing production config\n"
	if out, ok := TryCompactMesonCompile([]string{"meson", "compile", "-C", "build"}, []byte(input)); ok {
		t.Fatalf("custom meson success compacted by strict parser: %q", out)
	}
	if out, ok := TryCompactBuildOutput([]string{"meson", "compile", "-C", "build"}, []byte(input)); ok {
		t.Fatalf("custom meson success escaped through generic fallback: %q", out)
	}
}

func TestMesonCompileHelperBranches(t *testing.T) {
	t.Parallel()

	if suffix, ok := mesonCompileArgvSuffix(nil); ok || suffix != nil {
		t.Fatalf("nil meson argv should miss: ok=%v suffix=%v", ok, suffix)
	}
	if _, ok := mesonCompileArgvSuffix([]string{"npx", "meson"}); ok {
		t.Fatal("npx meson without compile should miss")
	}
	if _, ok := mesonCompileArgvSuffix([]string{"pnpm", "exec", "meson", "compile", "--vs-args=/m"}); ok {
		t.Fatal("meson compile backend args must miss")
	}
	if !mesonCompileBackendInfoLine("INFO: autodetecting backend as ninja") {
		t.Fatal("expected Meson ninja backend autodetect line")
	}
	if !mesonCompileBackendInfoLine("INFO: calculating backend command to run: /opt/homebrew/bin/ninja -C /repo/build") {
		t.Fatal("expected Meson ninja backend command line")
	}
	if mesonCompileBackendInfoLine("INFO: calculating backend command to run: /usr/bin/xcodebuild -project app.xcodeproj") {
		t.Fatal("non-ninja Meson backend command must miss")
	}
	if ok, terminal := mesonNinjaCleanProgressLine("[1/2] Compiling C object app.p/main.c.o"); !ok || terminal {
		t.Fatalf("bad Meson compile progress classification: ok=%v terminal=%v", ok, terminal)
	}
	if ok, terminal := mesonNinjaCleanProgressLine("[2/2] Linking target app"); !ok || !terminal {
		t.Fatalf("bad Meson link progress classification: ok=%v terminal=%v", ok, terminal)
	}
	if ok, _ := mesonNinjaCleanProgressLine("[1/1] Generating deployment manifest"); ok {
		t.Fatal("custom Meson command line must miss")
	}
}

func mesonCompileCleanProgressFixture(files int) string {
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

func mesonCompileNoWorkFixture() string {
	return "ninja: Entering directory `/repo/build'\n" +
		"ninja: no work to do.\n" +
		"INFO: autodetecting backend as ninja\n" +
		"INFO: calculating backend command to run: /opt/homebrew/bin/ninja -C /repo/build\n"
}

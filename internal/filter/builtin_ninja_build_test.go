package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactNinjaCleanProgressOutput(t *testing.T) {
	t.Parallel()

	progress := ninjaCleanProgressFixture(24)
	out, ok := TryCompactNinja([]string{"ninja", "-C", "build"}, []byte(progress))
	if !ok || string(out) != "[ninja] ok\n" {
		t.Fatalf("ninja clean progress: ok=%v out=%q", ok, out)
	}

	wrapped, ok := TryCompactBuildOutput([]string{"pnpm", "exec", "ninja", "-C", "out"}, []byte(progress))
	if !ok || string(wrapped) != "[ninja] ok\n" {
		t.Fatalf("wrapped ninja clean progress through build chain: ok=%v out=%q", ok, wrapped)
	}

	noWork, ok := TryCompactNinja([]string{"yarn", "ninja", "-C", "out"}, []byte("ninja: no work to do.\n"))
	if !ok || string(noWork) != "[ninja] ok\n" {
		t.Fatalf("ninja no work: ok=%v out=%q", ok, noWork)
	}
}

func TestTryCompactNinjaCleanProgressFailOpen(t *testing.T) {
	t.Parallel()

	tests := []string{
		ninjaCleanProgressFixture(8) + "warning: generated header is stale\n",
		"[1/2] Building CXX object src/CMakeFiles/app.dir/main.cpp.o\n[2/2] Linker warning: unused object\n",
		"[1/2] Building CXX object src/CMakeFiles/app.dir/main.cpp.o\n[2/2] FAILED: app\n",
		"[1/2] printf 'deploying production'\n[2/2] cc -O2 main.c -o app\n",
		"[1/2] Building CXX object src/CMakeFiles/app.dir/main.cpp.o\n",
		"digraph ninja {\n  \"app\" -> \"main.o\"\n}\n",
	}
	for _, input := range tests {
		input := input
		t.Run(strings.SplitN(input, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			if out, ok := TryCompactNinja([]string{"ninja", "-C", "build"}, []byte(input)); ok {
				t.Fatalf("unsafe ninja output compacted: %q", out)
			}
		})
	}
}

func ninjaCleanProgressFixture(files int) string {
	var b strings.Builder
	b.WriteString("ninja: Entering directory `build'\n")
	for i := 0; i < files; i++ {
		fmt.Fprintf(&b, "[%d/%d] Building CXX object src/CMakeFiles/app.dir/generated/object_%02d.cpp.o\n", i+1, files+1, i)
	}
	fmt.Fprintf(&b, "[%d/%d] Linking CXX executable app\n", files+1, files+1)
	return b.String()
}

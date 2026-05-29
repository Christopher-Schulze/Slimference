package filter

import (
	"strings"
	"testing"
)

func TestApplyLayer0Filters_prefersDedicatedBuildBeforeGenericLog(t *testing.T) {
	t.Parallel()
	var in strings.Builder
	for i := 0; i < 80; i++ {
		in.WriteString("2026-05-29T00:00:00Z INFO compiling package\n")
	}
	in.WriteString("2026-05-29T00:00:01Z ERROR ./main.go:12: undefined: value\n")
	out, name := applyLayer0Filters("", []string{"go", "build", "./..."}, []byte(in.String()))
	if name != "build_output" {
		t.Fatalf("expected dedicated build filter before generic log filter, got %q: %s", name, string(out))
	}
}

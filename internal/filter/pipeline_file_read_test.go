package filter

import (
	"fmt"
	"strings"
	"testing"
)

// TestLayer0PipelineDoesNotElideFirstFileRead locks the product invariant:
// generic captured-output filtering must not remove comments, bodies, or any
// other file content from a first file read. Repeat savings belong in readcache.
func TestLayer0PipelineDoesNotElideFirstFileRead(t *testing.T) {
	t.Parallel()
	var body strings.Builder
	body.WriteString("package x\n\n")
	body.WriteString("// this comment is real context and must remain\n")
	for i := range 40 {
		body.WriteString(fmt.Sprintf("func F%d(a int) int {\n", i))
		for j := range 15 {
			body.WriteString(fmt.Sprintf("\ta += %d // body detail %d\n", j, j))
		}
		body.WriteString("\treturn a\n}\n\n")
	}
	content := body.String()
	out, changed := CompactCapturedOutputWithContext("", "cat /tmp/x.go", content, 0, FileReadContext{Mode: "scan"})
	if changed {
		t.Fatalf("first file read must full-pass: changed output len=%d input=%d", len(out), len(content))
	}
	if string(out) != content {
		t.Fatalf("first file read output changed without changed=true")
	}
	out, changed = CompactCapturedOutputWithContext("", "awk 'NR>=1 && NR<=80 {print}' /tmp/x.go", content, 0, FileReadContext{Mode: "scan"})
	if changed || string(out) != content {
		t.Fatalf("first awk range read must full-pass: changed=%v out len=%d input len=%d", changed, len(out), len(content))
	}
}

package filter

import (
	"fmt"
	"strings"
	"testing"
)

// TestSedPartialGoReadScansWithRecovery proves scan-mode now fires on Codex's
// dominant read pattern - `sed -n '1,Np' file.go` partial reads - eliding bodies
// to signatures with a recovery note, while edit/recently-edited reads full-pass.
func TestSedPartialGoReadScansWithRecovery(t *testing.T) {
	var body strings.Builder
	body.WriteString("package big\n\n")
	for i := 0; i < 40; i++ {
		body.WriteString(fmt.Sprintf("func F%d(a int) int {\n", i))
		for j := 0; j < 15; j++ {
			body.WriteString(fmt.Sprintf("\ta += %d\n", j))
		}
		body.WriteString("\treturn a\n}\n\n")
	}
	content := []byte(body.String())
	if len(content) < signatureOnlyThreshold {
		t.Fatalf("fixture too small: %d", len(content))
	}
	argv := []string{"sed", "-n", "1,400p", "/tmp/big.go"}

	out, changed := TryStripCommentsFileReadWithContext(argv, content, FileReadContext{Mode: "scan"})
	if !changed {
		t.Fatalf("sed partial Go read must scan-compact; got no change (len=%d)", len(content))
	}
	if len(out) >= len(content) {
		t.Fatalf("scan output must be smaller: in=%d out=%d", len(content), len(out))
	}
	if !strings.Contains(string(out), "re-run the read to see the full file") {
		t.Fatalf("scan output must carry the recovery note: %q", string(out)[:min(len(out), 160)])
	}

	// Risk-scope: an edit/recently-edited read must full-pass (no elision).
	for _, ctx := range []FileReadContext{{Mode: "scan", RecentlyEdited: true}, {Mode: "edit"}, {ForceFull: true, Mode: "scan"}} {
		if _, changed := TryStripCommentsFileReadWithContext(argv, content, ctx); changed {
			t.Fatalf("edit/recent/force read must full-pass, ctx=%+v", ctx)
		}
	}
}

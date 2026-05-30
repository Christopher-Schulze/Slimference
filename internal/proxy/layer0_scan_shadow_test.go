package proxy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

// TestRecordScanModeShadow_GatedTelemetryOnly proves the scan-mode read shadow is
// env-gated, measures a positive would-save on a large Go read, and only returns
// a number (it never mutates the read; the caller full-passes it).
func TestRecordScanModeShadow_GatedTelemetryOnly(t *testing.T) {
	tok := tokens.ForProvider(types.CodexChatGPT)
	var body strings.Builder
	body.WriteString("Process exited with code 0\nOutput:\n")
	body.WriteString("package x\n\n")
	for i := 0; i < 40; i++ {
		body.WriteString(fmt.Sprintf("func F%d(a int) int {\n", i))
		for j := 0; j < 15; j++ {
			body.WriteString(fmt.Sprintf("\ta += %d\n", j))
		}
		body.WriteString("\treturn a\n}\n\n")
	}
	text := body.String()
	cmd := "cat /tmp/x.go"
	ctx := filter.FileReadContext{Mode: "scan"}
	before := tok.CountString(text)

	t.Setenv(scanShadowEnv, "")
	if got := recordScanModeShadow(cmd, text, ctx, before, tok); got != 0 {
		t.Fatalf("env unset must be a no-op, got %d", got)
	}
	t.Setenv(scanShadowEnv, "1")
	if got := recordScanModeShadow(cmd, text, ctx, before, tok); got <= 0 {
		t.Fatalf("scan shadow should measure positive would-save on a large Go read, got %d", got)
	}
}

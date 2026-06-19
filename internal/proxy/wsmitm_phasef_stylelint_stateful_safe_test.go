package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeStylelintJSONCleanCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: stylelint-json-clean\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssStylelintJSONCleanFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-stylelint-json-clean", "call_stylelint_json_clean", "stylelint --formatter json 'src/**/*.css'", envelope, "stateful-stylelint-json-clean-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle stylelint JSON clean request: %v", err)
	}
	if !replace {
		t.Fatal("full-history stylelint JSON clean output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[stylelint] clean (120 file(s))") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "src/generated/widget_119.css") {
		t.Fatalf("stylelint JSON clean output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe stylelint JSON clean should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulUnsafeStylelintJSONFindingsStayGuarded(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: stylelint-json-finding\nWall time: 0.0010 seconds\nProcess exited with code 2\nOriginal token count: 10000\nOutput:\n" +
		`[{"source":"src/generated/widget_119.css","errored":true,"warnings":[{"line":3,"column":1,"rule":"block-no-empty","severity":"error","text":"Unexpected empty block"}],"deprecations":[],"invalidOptionWarnings":[]}]`

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-stylelint-json-finding", "call_stylelint_json_finding", "stylelint --formatter json 'src/**/*.css'", envelope, "stateful-stylelint-json-finding-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle stylelint JSON finding request: %v", err)
	}
	body := string(env.Body)
	archiveBacked := strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://")
	if strings.Contains(body, "[stylelint] clean") || (!strings.Contains(body, "Unexpected empty block") && !archiveBacked) {
		t.Fatalf("stylelint JSON finding was incorrectly compacted: replace=%v body=%s", replace, body)
	}
	if replace && !archiveBacked {
		t.Fatalf("unsafe replacement must stay recovery-backed, body=%s", body)
	}
}

func wssStylelintJSONCleanFixture(files int) string {
	var out strings.Builder
	out.WriteByte('[')
	for i := 0; i < files; i++ {
		if i > 0 {
			out.WriteByte(',')
		}
		fmt.Fprintf(&out, `{"source":"src/generated/widget_%03d.css","errored":false,"warnings":[],"deprecations":[],"invalidOptionWarnings":[]}`, i)
	}
	out.WriteString("]\n")
	return out.String()
}

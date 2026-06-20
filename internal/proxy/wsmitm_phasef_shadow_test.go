package proxy

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/servermirror"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func shadowMsg(text string) []types.Message {
	return []types.Message{{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: text}}}}
}

func TestRecordShadowMirror_PredictsRepeatAfterObserve(t *testing.T) {
	old := wssShadowMirror
	t.Cleanup(func() { wssShadowMirror = old })
	wssShadowMirror = servermirror.New()

	content := strings.Repeat("repeated tool output line\n", 40)
	frame := shadowMsg(content)

	// First frame: nothing referenceable yet, content recorded.
	if rep := recordShadowMirror("sess-x", frame, frame); rep.ReferenceableBlocks != 0 {
		t.Fatalf("first frame should have nothing referenceable: %+v", rep)
	}
	// Second frame with the same content: now the server already holds it.
	rep := recordShadowMirror("sess-x", frame, frame)
	if rep.ReferenceableBlocks != 1 || rep.PotentialSavedBytes != len(content) {
		t.Fatalf("repeated frame should be referenceable in shadow: %+v", rep)
	}
	// Empty session is a no-op (never panics, never references).
	if rep := recordShadowMirror("", frame, frame); rep.Blocks != 0 || rep.ReferenceableBlocks != 0 {
		t.Fatalf("empty session must be a no-op: %+v", rep)
	}
}

func TestRecordShadowMirror_NormalizedDebugFacts(t *testing.T) {
	old := wssShadowMirror
	t.Cleanup(func() { wssShadowMirror = old })
	wssShadowMirror = servermirror.New()

	payload := strings.Repeat("stable payload line\n", 30)
	first := "Chunk ID: first\nWall time: 0.0001 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + payload
	second := "Chunk ID: second\nWall time: 9.9999 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload
	recordShadowMirror("sess-normalized", shadowMsg(first), shadowMsg(first))
	rep := recordShadowMirror("sess-normalized", shadowMsg(second), shadowMsg(second))
	if rep.ReferenceableBlocks != 0 || rep.NormalizedReferenceableSegments != 1 {
		t.Fatalf("volatile envelope should be normalized-only referenceable: %+v", rep)
	}

	var meta wssRequestMeta
	attachShadowMirrorDebugFacts(&meta, rep)
	if meta.DebugFacts["wss.shadow_mirror_bytes"] != strconv.Itoa(len(second)) ||
		meta.DebugFacts["wss.shadow_mirror_referenceable_bytes"] != "0" ||
		meta.DebugFacts["wss.shadow_mirror_normalized_bytes"] != strconv.Itoa(len(payload)) ||
		meta.DebugFacts["wss.shadow_mirror_normalized_referenceable_bytes"] != strconv.Itoa(len(payload)) ||
		meta.DebugFacts["wss.shadow_mirror_normalized_by_kind"] != "codex_exec_payload="+strconv.Itoa(len(payload))+"/1/1" ||
		meta.DebugFacts["wss.shadow_mirror_normalized_density_by_kind"] != "codex_exec_payload="+strconv.Itoa(len(payload))+"/"+strconv.Itoa(len(payload))+"/1/1" {
		t.Fatalf("bad normalized shadow facts: %+v", meta.DebugFacts)
	}
}

func TestWSSShadowMirrorObservesRawFallbackMessages(t *testing.T) {
	old := wssShadowMirror
	t.Cleanup(func() { wssShadowMirror = old })
	wssShadowMirror = servermirror.New()

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	output := "Chunk ID: raw-shadow\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		strings.Repeat("stable mirror payload\n", 60)
	for i := 0; i < 2; i++ {
		env := parseWSJSON(t, map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-raw-shadow",
			"prompt_cache_key":     "raw-shadow-session",
			"input": []any{
				map[string]any{
					"type":    "function_call_output",
					"call_id": "call_raw_shadow",
					"output":  output,
				},
				"future-codex-item-shape",
			},
			"stream": true,
		})
		original := append([]byte(nil), env.Raw...)
		replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
		if err != nil {
			t.Fatalf("raw shadow handle %d: %v", i, err)
		}
		if replace || string(env.Raw) != string(original) {
			t.Fatalf("raw shadow fallback must stay byte-equal, replace=%v raw=%s", replace, env.Raw)
		}
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.raw_partial_messages"] != "1" ||
		summary.DebugFacts["wss.shadow_mirror_blocks"] != "1" ||
		summary.DebugFacts["wss.shadow_mirror_referenceable_blocks"] != "1" ||
		summary.DebugFacts["wss.shadow_mirror_referenceable_bytes"] == "0" ||
		summary.DebugFacts["wss.changed"] != "false" ||
		summary.Tokens.Saved != 0 {
		t.Fatalf("raw fallback mirror should be observe-only but referenceable: %+v summary=%+v", summary.DebugFacts, summary)
	}
}

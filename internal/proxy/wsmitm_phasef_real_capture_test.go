package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
)

// TestWSPhaseFRealCodexMultiReadProducesDeltaMarker locks in the behaviour
// proven on a real Codex 0.133.0 CLI multi-read capture (T247, 2026-05-23):
// when the same file is read repeatedly under the exec_command shell tool,
// the Phase-F reducer must turn each subsequent function_call_output into a
// readcache delta marker. The fixture replays the captured wire shape
// (Codex exec envelope around the payload, exec_command tool with bash -lc
// wrapper, OpenAI Responses-API JSON-encoded arguments string,
// prompt_cache_key based session id) and asserts that the second and third
// reads mutate, shrink, and carry the "Slimference delta for <path>"
// marker. The readcache is isolated to t.TempDir() so the test does not
// touch ~/.slimference.
func TestWSPhaseFRealCodexMultiReadProducesDeltaMarker(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	var fileBody strings.Builder
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&fileBody, "Synthetic markdown line %03d for the WSS Phase-F multi-read delta reducer regression test.\n", i)
	}
	rawFile := fileBody.String()

	codexEnvelope := func(chunkID string) string {
		return "Chunk ID: " + chunkID +
			"\nWall time: 0.001 seconds\nProcess exited with code 0\nOriginal token count: 1024\nOutput:\n" + rawFile
	}

	const (
		path           = "/tmp/t247-synthetic.md"
		promptCacheKey = "019e5220-deadbeef-0000-0000-000000000000"
	)

	// Real Codex 0.133.0 exec_command argument shape (captured 2026-05-23,
	// see /tmp/t247-dump-evidence.tgz resp-response.output_item.done):
	// the `arguments` field is a JSON-encoded STRING with an object that
	// uses `cmd` as a single shell command string (NOT a command array
	// wrapped in bash). Other observed keys: workdir, yield_time_ms,
	// max_output_tokens.
	argsObj := map[string]any{
		"cmd":               "cat " + path,
		"workdir":           "/tmp",
		"yield_time_ms":     1000,
		"max_output_tokens": 20000,
	}
	argsJSON, err := json.Marshal(argsObj)
	if err != nil {
		t.Fatalf("marshal exec arguments: %v", err)
	}

	seedToolCall := func(callID string) {
		env := parseWSJSON(t, map[string]any{
			"type": string(wsmitm.FrameKindResponseOutputItemDone),
			"item": map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      "exec_command",
				"arguments": string(argsJSON),
			},
		})
		if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &env); err != nil || replace {
			t.Fatalf("server function_call seed should not replace, replace=%v err=%v", replace, err)
		}
	}

	runRead := func(callID, chunkID string) (preLen, postLen int, replaced bool, rawAfter []byte) {
		body := mustMarshal(map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp_test",
			"prompt_cache_key":     promptCacheKey,
			"input": []map[string]any{
				{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  codexEnvelope(chunkID),
				},
			},
			"stream": true,
		})
		env, err := wsmitm.Parse(body)
		if err != nil {
			t.Fatalf("parse synthetic request body: %v", err)
		}
		preLen = len(env.Raw)
		replaced = adapter.handleRequest(&env)
		postLen = len(env.Raw)
		return preLen, postLen, replaced, []byte(env.Raw)
	}

	seedToolCall("call_aaa")
	pre1, post1, replaced1, _ := runRead("call_aaa", "5bab73")
	if pre1 == 0 {
		t.Fatalf("read #1 raw body unexpectedly empty")
	}
	_ = replaced1
	_ = post1

	seedToolCall("call_bbb")
	pre2, post2, replaced2, raw2 := runRead("call_bbb", "12030b")
	if !replaced2 {
		t.Fatalf("read #2 expected mutation; replaced=false pre=%d post=%d", pre2, post2)
	}
	if post2 >= pre2 {
		t.Fatalf("read #2 expected size shrinkage; pre=%d post=%d", pre2, post2)
	}
	wantMarker := []byte("Slimference delta for " + path)
	if !bytes.Contains(raw2, wantMarker) {
		t.Fatalf("read #2 missing delta marker %q; first 600 bytes: %s", wantMarker, raw2[:min(600, len(raw2))])
	}

	seedToolCall("call_ccc")
	pre3, post3, replaced3, raw3 := runRead("call_ccc", "87688d")
	if !replaced3 {
		t.Fatalf("read #3 expected mutation; replaced=false pre=%d post=%d", pre3, post3)
	}
	if post3 >= pre3 {
		t.Fatalf("read #3 expected size shrinkage; pre=%d post=%d", pre3, post3)
	}
	if !bytes.Contains(raw3, wantMarker) {
		t.Fatalf("read #3 missing delta marker %q; first 600 bytes: %s", wantMarker, raw3[:min(600, len(raw3))])
	}

	telemetry := adapter.snapshot()
	if telemetry.RequestsSeen != 3 {
		t.Fatalf("expected 3 c2s requests; got %d", telemetry.RequestsSeen)
	}
	if telemetry.Mutations < 2 {
		t.Fatalf("expected >=2 mutations (reads #2 and #3); got %d", telemetry.Mutations)
	}

	snap := p.OutputReduceCountersSnapshot()
	if snap.ProxyLayer0RequestsModified < 2 {
		t.Fatalf("expected >=2 Layer-0 modified requests; got %d", snap.ProxyLayer0RequestsModified)
	}
	if snap.ProxyLayer0TokensSaved == 0 {
		t.Fatalf("expected non-zero L0 token savings; got snapshot=%+v", snap)
	}

	if got := wsCodexSessionID([]byte(`{"prompt_cache_key":"` + promptCacheKey + `"}`)); got != "codex-wss:"+promptCacheKey {
		t.Fatalf("session-key prerequisite regression: got %q want %q", got, "codex-wss:"+promptCacheKey)
	}
}

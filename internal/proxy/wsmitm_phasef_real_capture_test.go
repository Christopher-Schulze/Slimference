package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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

	runReadWithOutput := func(callID string, output any) (preLen, postLen int, replaced bool, rawAfter []byte) {
		body := mustMarshal(map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp_test",
			"prompt_cache_key":     promptCacheKey,
			"input": []map[string]any{
				{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  output,
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
	runRead := func(callID, chunkID string) (preLen, postLen int, replaced bool, rawAfter []byte) {
		return runReadWithOutput(callID, codexEnvelope(chunkID))
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

	seedToolCall("call_ddd")
	pre4, post4, replaced4, raw4 := runReadWithOutput("call_ddd", []map[string]any{
		{"type": "output_text", "text": codexEnvelope("a1f00d")},
		{"type": "image", "id": "preserve-shape"},
	})
	if !replaced4 {
		t.Fatalf("read #4 expected nested output_text-array mutation; replaced=false pre=%d post=%d", pre4, post4)
	}
	if post4 >= pre4 {
		t.Fatalf("read #4 expected size shrinkage; pre=%d post=%d", pre4, post4)
	}
	if !bytes.Contains(raw4, wantMarker) || !bytes.Contains(raw4, []byte(`"type":"image"`)) {
		t.Fatalf("read #4 should mutate only nested output_text and preserve sibling output items; first 700 bytes: %s", raw4[:min(700, len(raw4))])
	}

	telemetry := adapter.snapshot()
	if telemetry.RequestsSeen != 4 {
		t.Fatalf("expected 4 c2s requests; got %d", telemetry.RequestsSeen)
	}
	if telemetry.Mutations < 3 {
		t.Fatalf("expected >=3 mutations (reads #2, #3, and #4); got %d", telemetry.Mutations)
	}

	snap := p.OutputReduceCountersSnapshot()
	if snap.ProxyLayer0RequestsModified < 3 {
		t.Fatalf("expected >=3 Layer-0 modified requests; got %d", snap.ProxyLayer0RequestsModified)
	}
	if snap.ProxyLayer0TokensSaved == 0 {
		t.Fatalf("expected non-zero L0 token savings; got snapshot=%+v", snap)
	}

	if got := wsCodexSessionID([]byte(`{"prompt_cache_key":"` + promptCacheKey + `"}`)); got != "codex-wss:"+promptCacheKey {
		t.Fatalf("session-key prerequisite regression: got %q want %q", got, "codex-wss:"+promptCacheKey)
	}
}

func TestWSPhaseFAdditionalCodexToolShapesProduceDeltaMarkers(t *testing.T) {
	type toolShapeFixture struct {
		name        string
		fileName    string
		seedItem    func(callID string, fileName string, workdir string) map[string]any
		outputItem  func(callID string, text string) map[string]any
		preserveRaw string
	}

	fixtures := []toolShapeFixture{
		{
			name:     "local_shell_call_action_command_array_aggregated_output",
			fileName: "local-shell-read.md",
			seedItem: func(callID string, fileName string, workdir string) map[string]any {
				return map[string]any{
					"type":    "local_shell_call",
					"call_id": callID,
					"action": map[string]any{
						"command": []string{"/bin/bash", "-lc", "cat " + fileName},
						"cwd":     workdir,
					},
				}
			},
			outputItem: func(callID string, text string) map[string]any {
				return map[string]any{
					"type":              "local_shell_call_output",
					"call_id":           callID,
					"aggregated_output": text,
				}
			},
		},
		{
			name:     "shell_call_stdout_object",
			fileName: "shell-read.md",
			seedItem: func(callID string, fileName string, workdir string) map[string]any {
				return map[string]any{
					"type":    "shell_call",
					"call_id": callID,
					"action": map[string]any{
						"command": []string{"sh", "-c", "cat " + fileName},
						"cwd":     workdir,
					},
				}
			},
			outputItem: func(callID string, text string) map[string]any {
				return map[string]any{
					"type":    "shell_call_output",
					"call_id": callID,
					"stdout": map[string]any{
						"text":      text,
						"exit_code": 0,
					},
				}
			},
			preserveRaw: `"exit_code":0`,
		},
		{
			name:     "direct_read_file_tool_path_arguments",
			fileName: "direct-read.md",
			seedItem: func(callID string, fileName string, workdir string) map[string]any {
				return map[string]any{
					"type":    "function_call",
					"call_id": callID,
					"name":    "read_file",
					"arguments": map[string]any{
						"path":    fileName,
						"workdir": workdir,
					},
				}
			},
			outputItem: func(callID string, text string) map[string]any {
				return map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  text,
				}
			},
		},
		{
			name:     "mcp_read_file_result_content_text",
			fileName: "mcp-read.md",
			seedItem: func(callID string, fileName string, workdir string) map[string]any {
				return map[string]any{
					"type":    "mcp_call",
					"call_id": callID,
					"name":    "mcp.read_file",
					"arguments": map[string]any{
						"target":                    fileName,
						"current_working_directory": workdir,
					},
				}
			},
			outputItem: func(callID string, text string) map[string]any {
				return map[string]any{
					"type":    "mcp_call_output",
					"call_id": callID,
					"result": map[string]any{
						"content": []map[string]any{{"type": "text", "text": text}},
						"isError": false,
					},
				}
			},
			preserveRaw: `"isError":false`,
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
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

			workdir := filepath.Join(tmp, "repo")
			expectedPath := filepath.Join(workdir, fixture.fileName)
			promptCacheKey := "019e5220-t248-" + strings.ReplaceAll(fixture.name, "_", "-")
			var fileBody strings.Builder
			for i := 0; i < 260; i++ {
				fmt.Fprintf(&fileBody, "%s baseline line %03d for Codex WSS shape fixture coverage.\n", fixture.name, i)
			}
			before := fileBody.String()
			after := before + "T248 shape fixture appended line for " + fixture.name + ".\n"

			seedToolCall := func(callID string) {
				env := parseWSJSON(t, map[string]any{
					"type": string(wsmitm.FrameKindResponseOutputItemDone),
					"item": fixture.seedItem(callID, fixture.fileName, workdir),
				})
				if replace, err := adapter.handle(context.Background(), wsmitm.DirServerToClient, &env); err != nil || replace {
					t.Fatalf("server tool-call seed should not replace, replace=%v err=%v", replace, err)
				}
			}
			runOutput := func(callID string, text string) (int, int, bool, []byte) {
				body := mustMarshal(map[string]any{
					"model":                "gpt-5-codex",
					"previous_response_id": "resp_" + fixture.name,
					"prompt_cache_key":     promptCacheKey,
					"input":                []map[string]any{fixture.outputItem(callID, text)},
					"stream":               true,
				})
				env, err := wsmitm.Parse(body)
				if err != nil {
					t.Fatalf("parse fixture request body: %v", err)
				}
				preLen := len(env.Raw)
				replaced := adapter.handleRequest(&env)
				return preLen, len(env.Raw), replaced, []byte(env.Raw)
			}

			seedToolCall("call_first")
			_, _, _, _ = runOutput("call_first", before)

			seedToolCall("call_second")
			pre, post, replaced, raw := runOutput("call_second", after)
			if !replaced {
				t.Fatalf("expected repeated %s output to mutate; replaced=false pre=%d post=%d", fixture.name, pre, post)
			}
			if post >= pre {
				t.Fatalf("expected repeated %s output to shrink; pre=%d post=%d", fixture.name, pre, post)
			}
			wantMarker := []byte("Slimference delta for " + expectedPath)
			if !bytes.Contains(raw, wantMarker) {
				t.Fatalf("missing delta marker %q; first 700 bytes: %s", wantMarker, raw[:min(700, len(raw))])
			}
			if fixture.preserveRaw != "" && !bytes.Contains(raw, []byte(fixture.preserveRaw)) {
				t.Fatalf("mutated %s output did not preserve metadata %q; first 700 bytes: %s", fixture.name, fixture.preserveRaw, raw[:min(700, len(raw))])
			}

			telemetry := adapter.snapshot()
			if telemetry.Mutations == 0 {
				t.Fatalf("expected Phase-F mutation telemetry for %s; got %+v", fixture.name, telemetry)
			}
			snap := p.OutputReduceCountersSnapshot()
			if snap.ProxyLayer0TokensSaved == 0 || snap.ProxyLayer0ReadDeltaBlocks == 0 {
				t.Fatalf("expected Layer-0 read-delta savings for %s; got snapshot=%+v", fixture.name, snap)
			}
		})
	}
}

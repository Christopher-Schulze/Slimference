package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestExtractMessages_CodexResponsesInput(t *testing.T) {
	t.Parallel()
	body := readCodexFixture(t, "tests/fixtures/codex/v1-responses-input.json")

	msgs, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 10 {
		t.Fatalf("len=%d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].TextContent() == "" {
		t.Fatalf("first message: %#v", msgs[0])
	}
	if msgs[3].Role != "tool" || msgs[3].Content[0].Type != "tool_result" || msgs[3].Content[0].ToolResultID != "call_1" {
		t.Fatalf("tool output mapping: %#v", msgs[3])
	}

	msgs[3].Content[0].Text = `{"status":"compact"}`
	out, err := reconstructBody(types.CodexChatGPT, body, msgs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"metadata"`) || !strings.Contains(s, `"conversation_id"`) {
		t.Fatalf("body metadata not preserved: %s", s)
	}
	if !strings.Contains(s, `{\"status\":\"compact\"}`) {
		t.Fatalf("tool output rewrite missing: %s", s)
	}
}

func TestExtractMessages_CodexInputSkipsUnsupportedItemsLosslessly(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"model":"codex-test",
		"input":[
			{"type":"unknown","opaque":{"keep":true}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"original text"}]},
			{"type":"custom","content":[{"type":"image","url":"local"}]}
		],
		"stream":false
	}`)

	msgs, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Index != 1 || msgs[0].TextContent() != "original text" {
		t.Fatalf("unsupported items should be skipped without aborting parse: %#v", msgs)
	}

	msgs[0].Content[0].Text = "compact text"
	out, err := reconstructBody(types.CodexChatGPT, body, msgs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"opaque":{"keep":true}`) || !strings.Contains(s, `"type":"custom"`) {
		t.Fatalf("unsupported items were not preserved: %s", s)
	}
	if !strings.Contains(s, "compact text") || strings.Contains(s, "original text") {
		t.Fatalf("known item rewrite failed: %s", s)
	}
}

func TestExtractMessages_CodexResponseItemPayloadRoundTrip(t *testing.T) {
	t.Parallel()
	var status strings.Builder
	for i := 0; i < 80; i++ {
		status.WriteString(" M internal/proxy/wrapped_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	bodyMap := map[string]any{
		"model": "gpt-5-codex",
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "check git status"},
				},
			},
			map[string]any{
				"type": "response_item",
				"payload": map[string]any{
					"type":      "function_call",
					"call_id":   "call_status",
					"name":      "exec_command",
					"arguments": map[string]any{"cmd": "git status --short"},
				},
			},
			map[string]any{
				"type": "response_item",
				"payload": map[string]any{
					"type":    "function_call_output",
					"call_id": "call_status",
					"output":  status.String(),
				},
			},
		},
		"stream": true,
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		t.Fatal(err)
	}

	msgs, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 || msgs[1].Content[0].ToolName != "exec_command" || msgs[2].Content[0].Type != "tool_result" {
		t.Fatalf("unexpected wrapper extraction: %#v", msgs)
	}
	msgs[2].Content[0].Text = "[git status]\n M internal/proxy/wrapped_0.go\n[... 79 files omitted ...]\n"
	out, err := reconstructBody(types.CodexChatGPT, body, msgs)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Input []struct {
			Type    string `json:"type"`
			Payload struct {
				Type   string `json:"type"`
				Output string `json:"output"`
			} `json:"payload"`
			Output string `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Input[2].Type != "response_item" || decoded.Input[2].Payload.Type != "function_call_output" {
		t.Fatalf("wrapper identity was not preserved: %s", out)
	}
	if !strings.Contains(decoded.Input[2].Payload.Output, "[git status]") || strings.Contains(decoded.Input[2].Payload.Output, "wrapped_79.go") {
		t.Fatalf("payload output was not rewritten compactly: %q", decoded.Input[2].Payload.Output)
	}
	if decoded.Input[2].Output != "" {
		t.Fatalf("rewrite leaked to wrapper top-level output: %s", out)
	}
}

func TestExtractMessages_CodexMessagesShapeUsesOpenAIParser(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"codex-test","messages":[{"role":"user","content":"hi"},{"role":"tool","tool_call_id":"call_x","content":"tool out"}],"store":true}`)
	msgs, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Content[0].Type != "tool_result" {
		t.Fatalf("unexpected messages: %#v", msgs)
	}
	msgs[1].Content[0].Text = "compact out"
	out, err := reconstructBody(types.CodexChatGPT, body, msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "compact out") || !strings.Contains(string(out), `"store"`) {
		t.Fatalf("reconstruct: %s", out)
	}
}

func TestExtractMessages_CodexInputStringRoundTrip(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"codex-test","input":"please inspect the repository","stream":false}`)

	msgs, _, err := extractMessages(types.CodexChatGPT, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].TextContent() != "please inspect the repository" {
		t.Fatalf("unexpected string input messages: %#v", msgs)
	}

	msgs[0].Content[0].Text = "inspect repo"
	out, err := reconstructBody(types.CodexChatGPT, body, msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"input":"inspect repo"`) || !strings.Contains(string(out), `"stream":false`) {
		t.Fatalf("string input reconstruct: %s", out)
	}
}

func TestExtractCodexInputMessages_ErrorAndEmptyBranches(t *testing.T) {
	t.Parallel()
	extra := map[string]json.RawMessage{"model": json.RawMessage(`"codex-test"`)}

	msgs, returned, err := extractCodexInputMessages(nil, extra)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 || returned["model"] == nil {
		t.Fatalf("empty input returned messages=%#v extra=%#v", msgs, returned)
	}

	if _, _, err := extractCodexInputMessages(json.RawMessage(`"unterminated`), extra); err == nil {
		t.Fatal("expected invalid input string error")
	}
	if _, _, err := extractCodexInputMessages(json.RawMessage(`{}`), extra); err == nil {
		t.Fatal("expected invalid input items error")
	}
	if msgs, _, err := extractCodexInputMessages(json.RawMessage(`[{"type":"unknown","content":"ignored"},{"type":"message"}]`), extra); err != nil || len(msgs) != 0 {
		t.Fatalf("unknown/no-content items should be ignored, messages=%#v err=%v", msgs, err)
	}
	if msgs, _, err := extractCodexInputMessages(json.RawMessage(`[]`), extra); err != nil || len(msgs) != 0 {
		t.Fatalf("empty input array should produce no messages, messages=%#v err=%v", msgs, err)
	}
	if _, _, err := extractCodexInputMessages(json.RawMessage(`["not-an-object"]`), extra); err == nil {
		t.Fatal("expected non-object input item error")
	}
	if _, _, err := extractCodexInputMessages(json.RawMessage(`[{]`), extra); err == nil {
		t.Fatal("expected malformed input item error")
	}
}

func TestCodexInputItemToMessage_Branches(t *testing.T) {
	t.Parallel()

	msg, ok, err := codexInputItemToMessage(7, json.RawMessage(`{"role":"system","content":"policy text"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Index != 7 || msg.Role != "system" || msg.TextContent() != "policy text" {
		t.Fatalf("explicit role content: ok=%v msg=%#v", ok, msg)
	}

	msg, ok, err = codexInputItemToMessage(8, json.RawMessage(`{"type":"function_call","id":"call_fallback","name":"read_file","arguments":{"path":"README.md"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Role != "assistant" || msg.Content[0].ToolUseID != "call_fallback" || !strings.Contains(msg.Content[0].ToolInput, "README.md") {
		t.Fatalf("function call fallback mapping: ok=%v msg=%#v", ok, msg)
	}

	msg, ok, err = codexInputItemToMessage(9, json.RawMessage(`{"type":"function_call_output","id":"call_out","output":{"ok":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Role != "tool" || msg.Content[0].ToolResultID != "call_out" || msg.Content[0].Text != `{"ok":true}` {
		t.Fatalf("function output fallback mapping: ok=%v msg=%#v", ok, msg)
	}

	msg, ok, err = codexInputItemToMessage(9, json.RawMessage(`{"type":"function_call_output","id":"call_wrapped","output":{"stdout":"ok  github.com/slimference/slimference/internal/proxy  0.041s\nPASS\n","exit_code":0}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Content[0].Text != "ok  github.com/slimference/slimference/internal/proxy  0.041s\nPASS\n" {
		t.Fatalf("wrapped stdout should be extracted as tool text: ok=%v msg=%#v", ok, msg)
	}
	msg.Content[0].Text = "ok\n"
	raw, ok := msg.Content[0].RawBlock.(codexInputItemRaw)
	if !ok {
		t.Fatal("expected codex raw block")
	}
	out, err := codexMessageToInputItem(msg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"stdout":"ok\n"`) || !strings.Contains(string(out), `"exit_code":0`) {
		t.Fatalf("wrapped stdout rewrite should preserve output object metadata: %s", out)
	}

	msg, ok, err = codexInputItemToMessage(9, json.RawMessage(`{"type":"local_shell_call","call_id":"call_shell","action":{"command":"/opt/homebrew/bin/bash -lc 'git status --short .'"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Role != "assistant" || msg.Content[0].ToolName != "shell" || !strings.Contains(msg.Content[0].ToolInput, "git status") {
		t.Fatalf("local shell call mapping: ok=%v msg=%#v", ok, msg)
	}

	msg, ok, err = codexInputItemToMessage(9, json.RawMessage(`{"type":"local_shell_call_output","call_id":"call_shell","command":["/opt/homebrew/bin/bash","-lc","git status --short ."],"aggregated_output":"Chunk ID: abc\nProcess exited with code 0\nOutput:\n M internal/proxy/provider.go\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Role != "tool" || msg.Content[0].ToolResultID != "call_shell" || msg.Content[0].ToolName != "shell" || !strings.Contains(msg.Content[0].ToolInput, "git status") || !strings.Contains(msg.Content[0].Text, "Chunk ID") {
		t.Fatalf("local shell output mapping: ok=%v msg=%#v", ok, msg)
	}
	msg.Content[0].Text = "compact\n"
	raw, ok = msg.Content[0].RawBlock.(codexInputItemRaw)
	if !ok {
		t.Fatal("expected local shell raw block")
	}
	out, err = codexMessageToInputItem(msg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"aggregated_output":"compact\n"`) || !strings.Contains(string(out), `"command":["/opt/homebrew/bin/bash","-lc","git status --short ."]`) {
		t.Fatalf("aggregated_output rewrite should preserve command metadata: %s", out)
	}

	msg, ok, err = codexInputItemToMessage(9, json.RawMessage(`{"type":"tool_output","call_id":"call_obj","stdout":{"text":"out\n","exit_code":0}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Content[0].Text != "out\n" {
		t.Fatalf("direct stdout object mapping: ok=%v msg=%#v", ok, msg)
	}
	msg.Content[0].Text = "compact out\n"
	raw, ok = msg.Content[0].RawBlock.(codexInputItemRaw)
	if !ok {
		t.Fatal("expected stdout raw block")
	}
	out, err = codexMessageToInputItem(msg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"stdout":{"exit_code":0,"text":"compact out\n"}`) {
		t.Fatalf("direct stdout object rewrite should preserve metadata: %s", out)
	}

	msg, ok, err = codexInputItemToMessage(9, json.RawMessage(`{"type":"tool_result","call_id":"call_content","content":"tool content\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Content[0].Text != "tool content\n" {
		t.Fatalf("top-level content tool result mapping: ok=%v msg=%#v", ok, msg)
	}
	msg.Content[0].Text = "compact content\n"
	raw, ok = msg.Content[0].RawBlock.(codexInputItemRaw)
	if !ok {
		t.Fatal("expected content raw block")
	}
	out, err = codexMessageToInputItem(msg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"content":"compact content\n"`) {
		t.Fatalf("content rewrite should preserve field name: %s", out)
	}

	msg, ok, err = codexInputItemToMessage(9, json.RawMessage(`{"type":"function_call_output","call_id":"call_parts","output":[{"type":"output_text","text":"nested output\n"},{"type":"image","id":"preserve"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg.Content[0].Text != "nested output\n" {
		t.Fatalf("single text-part output array should be extracted: ok=%v msg=%#v", ok, msg)
	}
	msg.Content[0].Text = "compact nested output\n"
	raw, ok = msg.Content[0].RawBlock.(codexInputItemRaw)
	if !ok {
		t.Fatal("expected text-part raw block")
	}
	out, err = codexMessageToInputItem(msg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"text":"compact nested output\n"`) || !strings.Contains(string(out), `"type":"image"`) {
		t.Fatalf("output text-part rewrite should preserve output array shape: %s", out)
	}

	for _, rawOutput := range []json.RawMessage{
		json.RawMessage(`{`),
		json.RawMessage(`{"stdout":"out","stderr":"err"}`),
		json.RawMessage(`{"stdout":"   "}`),
		json.RawMessage(`{"exit_code":0}`),
	} {
		if _, ok := singleCodexOutputTextField(rawOutput); ok {
			t.Fatalf("singleCodexOutputTextField(%s) unexpectedly matched", rawOutput)
		}
	}
	_, err = codexMessageToInputItem(types.Message{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: "replacement"}}}, codexInputItemRaw{
		Fields: map[string]json.RawMessage{
			"type":   json.RawMessage(`"function_call_output"`),
			"output": json.RawMessage(`"not-object"`),
		},
		TextPath: "output_field:stdout",
	})
	if err == nil {
		t.Fatal("expected invalid output object error")
	}
	_, err = codexMessageToInputItem(types.Message{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: "replacement"}}}, codexInputItemRaw{
		Fields: map[string]json.RawMessage{
			"type":   json.RawMessage(`"tool_output"`),
			"stdout": json.RawMessage(`"not-object"`),
		},
		TextPath: "field_object:stdout:text",
	})
	if err == nil {
		t.Fatal("expected invalid field object error")
	}

	if msg, ok, err = codexInputItemToMessage(10, json.RawMessage(`{"type":"unknown","content":"ignored"}`)); err != nil || ok || msg.Role != "" {
		t.Fatalf("unknown item should be ignored, ok=%v msg=%#v err=%v", ok, msg, err)
	}
	if msg, ok, err = codexInputItemToMessage(10, json.RawMessage(`{"type":"message","content":[{"type":"input_text","text":"one"},{"type":"output_text","text":"two"}]}`)); err != nil || ok || msg.Role != "" {
		t.Fatalf("multi-text content should be unsupported, ok=%v msg=%#v err=%v", ok, msg, err)
	}
	if msg, ok, err = codexInputItemToMessage(10, json.RawMessage(`{"type":"message","content":{"unexpected":true}}`)); err != nil || ok || msg.Role != "" {
		t.Fatalf("object content should be unsupported, ok=%v msg=%#v err=%v", ok, msg, err)
	}
	if msg, ok, err = codexInputItemToMessage(10, json.RawMessage(`{"type":"message","role":"assistant"}`)); err != nil || ok || msg.Role != "" {
		t.Fatalf("content-free message should be unsupported, ok=%v msg=%#v err=%v", ok, msg, err)
	}
	if _, _, err = codexInputItemToMessage(11, json.RawMessage(`{]`)); err == nil {
		t.Fatal("expected malformed item error")
	}

	if codexRoleForInputType("unrecognized") != "" || rawJSONString(json.RawMessage(`{"not":"string"}`)) != "" || rawJSONText(nil) != "" || firstNonEmpty("", "") != "" {
		t.Fatal("expected empty helper fallbacks")
	}
}

func TestCodexToolShapeHelpers(t *testing.T) {
	t.Parallel()

	if codexLooksLikeToolCall("", map[string]json.RawMessage{"id": json.RawMessage(`"x"`)}) {
		t.Fatal("id alone should not become a tool call")
	}
	if !codexLooksLikeToolCall("", map[string]json.RawMessage{"id": json.RawMessage(`"x"`), "input": json.RawMessage(`{"cmd":"pwd"}`)}) {
		t.Fatal("id plus input should become a tool call")
	}
	if !codexLooksLikeToolCall("", map[string]json.RawMessage{"id": json.RawMessage(`"x"`), "command_line": json.RawMessage(`"git status"`)}) {
		t.Fatal("id plus command_line should become a tool call")
	}
	if codexLooksLikeToolOutput("custom_output", nil) {
		t.Fatal("output suffix without output text should not match")
	}
	if !codexLooksLikeToolOutput("output", map[string]json.RawMessage{"text": json.RawMessage(`"done"`)}) {
		t.Fatal("output item with direct text should match")
	}
	if codexToolName(map[string]json.RawMessage{"name": json.RawMessage(`"exec_command"`), "cmd": json.RawMessage(`"pwd"`)}) != "exec_command" {
		t.Fatal("explicit tool name should win")
	}
	if codexToolName(nil) != "" || codexToolInput(nil) != "" {
		t.Fatal("empty tool metadata should stay empty")
	}
	if input := codexToolInput(map[string]json.RawMessage{"cmd": json.RawMessage(`"cat docs/todo.md"`), "workdir": json.RawMessage(`"/repo/project"`)}); !strings.Contains(input, `"workdir":"/repo/project"`) {
		t.Fatalf("codexToolInput should preserve workdir for relative read-cache keys: %s", input)
	}

	commandCases := []struct {
		fields map[string]json.RawMessage
		want   string
	}{
		{map[string]json.RawMessage{"cmd": json.RawMessage(`"go test ./..."`)}, "go test ./..."},
		{map[string]json.RawMessage{"command_line": json.RawMessage(`"cargo test"`)}, "cargo test"},
		{map[string]json.RawMessage{"cmdline": json.RawMessage(`"go vet ./..."`)}, "go vet ./..."},
		{map[string]json.RawMessage{"shell_command": json.RawMessage(`"git status --short"`)}, "git status --short"},
		{map[string]json.RawMessage{"command": json.RawMessage(`"git diff"`)}, "git diff"},
		{map[string]json.RawMessage{"argv": json.RawMessage(`["go","test","./pkg with space"]`)}, `go test "./pkg with space"`},
		{map[string]json.RawMessage{"args": json.RawMessage(`["rg","needle","path with space"]`)}, `rg needle "path with space"`},
		{map[string]json.RawMessage{"input": json.RawMessage(`{"command":["/bin/sh","-c","git status --short"]}`)}, `/bin/sh -c "git status --short"`},
		{map[string]json.RawMessage{"action": json.RawMessage(`{"command_line":"make test"}`)}, "make test"},
		{map[string]json.RawMessage{"action": json.RawMessage(`{"args":["go","test","./..."]}`)}, "go test ./..."},
		{map[string]json.RawMessage{"parameters": json.RawMessage(`{"cmd":"git diff --stat"}`)}, "git diff --stat"},
	}
	for _, tc := range commandCases {
		if got := codexCommandLineFromFields(tc.fields); got != tc.want {
			t.Fatalf("codexCommandLineFromFields=%q want %q for %#v", got, tc.want, tc.fields)
		}
	}
	if got := rawJSONStringArray(json.RawMessage(`{"bad":true}`)); got != nil {
		t.Fatalf("invalid string array=%#v", got)
	}
	if got := rawJSONStringArray(nil); got != nil {
		t.Fatalf("nil string array=%#v", got)
	}

	outputCases := []struct {
		fields   map[string]json.RawMessage
		wantText string
		wantPath string
	}{
		{map[string]json.RawMessage{"stderr": json.RawMessage(`"err\n"`)}, "err\n", "field:stderr"},
		{map[string]json.RawMessage{"text": json.RawMessage(`"text\n"`)}, "text\n", "field:text"},
		{map[string]json.RawMessage{"aggregated_output": json.RawMessage(`{"stdout":"wrapped\n","exit_code":0}`)}, "wrapped\n", "field_object:aggregated_output:stdout"},
		{map[string]json.RawMessage{"stdout": json.RawMessage(`{"unexpected":true}`)}, `{"unexpected":true}`, "field:stdout"},
		{map[string]json.RawMessage{"content": json.RawMessage(`"content\n"`)}, "content\n", "field:content"},
		{map[string]json.RawMessage{"result": json.RawMessage(`{"tool_response":"result text\n"}`)}, "result text\n", "field_object:result:tool_response"},
		{map[string]json.RawMessage{"tool_response": json.RawMessage(`"direct response\n"`)}, "direct response\n", "field:tool_response"},
		{map[string]json.RawMessage{"output": json.RawMessage(`[{"type":"output_text","text":"array output\n"},{"type":"image","id":"keep"}]`)}, "array output\n", "field_part_text:output:0"},
		{map[string]json.RawMessage{"content": json.RawMessage(`[{"type":"text","text":"array content\n"},{"type":"image","id":"keep"}]`)}, "array content\n", "field_part_text:content:0"},
	}
	for _, tc := range outputCases {
		text, path := codexToolOutputText(tc.fields)
		if text != tc.wantText || path != tc.wantPath {
			t.Fatalf("codexToolOutputText text=%q path=%q want %q/%q", text, path, tc.wantText, tc.wantPath)
		}
	}
	if text, path := codexToolOutputText(nil); text != "" || path != "" {
		t.Fatalf("empty output text=%q path=%q", text, path)
	}
	if text, path := codexToolOutputText(map[string]json.RawMessage{"output": json.RawMessage(`[{"type":"output_text","text":"one"},{"type":"text","text":"two"}]`)}); text != "" || path != "" {
		t.Fatalf("multi-text output arrays must fail open without rewrite path, text=%q path=%q", text, path)
	}
}

func TestCodexTextFromContent_Branches(t *testing.T) {
	t.Parallel()

	text, path, index := codexTextFromContent(nil)
	if text != "" || path != "" || index != -1 {
		t.Fatalf("empty content text=%q path=%q index=%d", text, path, index)
	}

	text, path, index = codexTextFromContent(json.RawMessage(`{"unexpected":true}`))
	if text != `{"unexpected":true}` || path != "" || index != -1 {
		t.Fatalf("object fallback text=%q path=%q index=%d", text, path, index)
	}

	text, path, index = codexTextFromContent(json.RawMessage(`[{"type":"input_text","text":"first"},{"type":"output_text","text":"second"},{"type":"image","url":"local"}]`))
	if text != "first\nsecond" || path != "" || index != -1 {
		t.Fatalf("multi text fallback text=%q path=%q index=%d", text, path, index)
	}

	text, path, index = codexTextFromContent(json.RawMessage(`[{"type":"image","url":"local"}]`))
	if text != "" || path != "" || index != -1 {
		t.Fatalf("non-text parts text=%q path=%q index=%d", text, path, index)
	}
}

func TestMessagesToCodexInputJSON_Fallbacks(t *testing.T) {
	t.Parallel()

	originalString := json.RawMessage(`"unchanged"`)
	out, err := messagesToCodexInputJSON(originalString, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(originalString) {
		t.Fatalf("empty messages should preserve string input: %s", out)
	}

	originalItems := json.RawMessage(`[{"type":"message","role":"user","content":"unchanged"}]`)
	out, err = messagesToCodexInputJSON(originalItems, []types.Message{{Index: 0, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "no raw"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"content":"no raw"`) || strings.Contains(string(out), "unchanged") {
		t.Fatalf("missing raw item should recover the original input slot and rewrite: %s", out)
	}

	out, err = messagesToCodexInputJSON(originalItems, []types.Message{{Index: 99, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "orphan rawless"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(originalItems) {
		t.Fatalf("unrecoverable rawless item should preserve original input: %s", out)
	}

	_, err = messagesToCodexInputJSON(json.RawMessage(`{}`), []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{{
			Type: "text",
			Text: "replacement",
			RawBlock: codexInputItemRaw{
				Fields:    map[string]json.RawMessage{"content": json.RawMessage(`"old"`)},
				TextPath:  "content_string",
				TextIndex: -1,
			},
		}},
	}})
	if err == nil {
		t.Fatal("expected invalid original input array error")
	}

	out, err = messagesToCodexInputJSON(originalItems, []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{{
			Type: "text",
			Text: "replacement",
			RawBlock: codexInputItemRaw{
				Fields:    map[string]json.RawMessage{"content": json.RawMessage(`"old"`)},
				ItemIndex: 99,
				TextPath:  "content_string",
				TextIndex: -1,
			},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(originalItems) {
		t.Fatalf("out-of-range raw item should preserve original input: %s", out)
	}

	_, err = messagesToCodexInputJSON(originalItems, []types.Message{{
		Role: "user",
		Content: []types.ContentBlock{{
			Type: "text",
			Text: "replacement",
			RawBlock: codexInputItemRaw{
				Fields: map[string]json.RawMessage{
					"type":    json.RawMessage(`"message"`),
					"role":    json.RawMessage(`"user"`),
					"content": json.RawMessage(`"not-an-array"`),
				},
				TextPath:  "content_part_text",
				TextIndex: 0,
			},
		}},
	}})
	if err == nil {
		t.Fatal("expected invalid content_part_text raw error")
	}

	raw := codexInputItemRaw{
		Fields: map[string]json.RawMessage{
			"type":    json.RawMessage(`"message"`),
			"role":    json.RawMessage(`"user"`),
			"content": json.RawMessage(`[{"type":"input_text","text":"same"}]`),
		},
		TextPath:  "content_part_text",
		TextIndex: 99,
	}
	item, err := codexMessageToInputItem(types.Message{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "replacement"}}}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(item), "replacement") {
		t.Fatalf("out-of-range text index should leave content unchanged: %s", item)
	}
}

func TestServeHTTP_CodexResponsesCompressionAndHeaders(t *testing.T) {
	t.Parallel()
	body := readCodexFixture(t, "tests/fixtures/codex/v1-responses-input.json")
	var capturedBody []byte
	var capturedAuth, capturedUA, capturedSession string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		capturedAuth = r.Header.Get("Authorization")
		capturedUA = r.Header.Get("User-Agent")
		capturedSession = r.Header.Get("x-codex-session-id")
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_test","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = true
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.SlidingWindow = 1
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer codex-oauth-token")
	req.Header.Set("User-Agent", "codex/0.125.0 (rust)")
	req.Header.Set("x-codex-session-id", "sess-redacted")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if capturedAuth != "Bearer codex-oauth-token" || capturedUA == "" || capturedSession != "sess-redacted" {
		t.Fatalf("headers auth=%q ua=%q session=%q", capturedAuth, capturedUA, capturedSession)
	}
	if len(capturedBody) == 0 || len(capturedBody) >= len(body) {
		t.Fatalf("expected shorter upstream body, original=%d captured=%d body=%s", len(body), len(capturedBody), capturedBody)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].Provider != "codex_chatgpt" || summaries[0].Tokens.Saved <= 0 {
		t.Fatalf("debug summary missing codex savings: %#v", summaries)
	}
}

func TestServeHTTP_CodexResponsesProxyLayer0CompactsToolOutput(t *testing.T) {
	t.Parallel()
	var status strings.Builder
	for i := 0; i < 120; i++ {
		status.WriteString(" M internal/proxy/file_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	bodyMap := map[string]interface{}{
		"model": "codex-test",
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "check git status"},
				},
			},
			map[string]interface{}{
				"type":      "function_call",
				"call_id":   "call_status",
				"name":      "shell",
				"arguments": map[string]interface{}{"command": "git status --short"},
			},
			map[string]interface{}{
				"type":    "function_call_output",
				"call_id": "call_status",
				"output":  status.String(),
			},
		},
		"stream": false,
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		t.Fatal(err)
	}
	var capturedBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/responses" {
			http.NotFound(w, r)
			return
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_layer0","usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":0},"output_tokens":5}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codex/0.130.0")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if len(capturedBody) == 0 || len(capturedBody) >= len(body) {
		t.Fatalf("expected Layer 0 to shorten upstream body, original=%d captured=%d body=%s", len(body), len(capturedBody), capturedBody)
	}
	msgs, _, err := extractMessages(types.CodexChatGPT, capturedBody)
	if err != nil {
		t.Fatal(err)
	}
	var toolText string
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				toolText = block.Text
			}
		}
	}
	if !strings.Contains(toolText, "[git status]") || strings.Contains(toolText, "file_119.go") {
		t.Fatalf("tool output was not Layer-0 compacted: %q", toolText)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || !slices.Contains(summaries[0].LayersApplied, 0) {
		t.Fatalf("summary missing Layer 0: %#v", summaries)
	}
	if summaries[0].Tokens.AfterLayer0 >= summaries[0].Tokens.Original {
		t.Fatalf("Layer 0 accounting did not save tokens: %#v", summaries[0].Tokens)
	}
}

func TestServeHTTP_CodexResponsesProxyLayer0CompactsLocalShellEnvelope(t *testing.T) {
	t.Parallel()
	var status strings.Builder
	for i := 0; i < 120; i++ {
		status.WriteString(" M internal/proxy/local_")
		status.WriteString(strconv.Itoa(i))
		status.WriteString(".go\n")
	}
	bodyMap := map[string]interface{}{
		"model": "codex-test",
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "check git status"},
				},
			},
			map[string]interface{}{
				"type":    "local_shell_call",
				"call_id": "call_status",
				"action":  map[string]interface{}{"command": "/opt/homebrew/bin/bash -lc 'git status --short .'"},
			},
			map[string]interface{}{
				"type":              "local_shell_call_output",
				"call_id":           "call_status",
				"command":           []string{"/opt/homebrew/bin/bash", "-lc", "git status --short ."},
				"aggregated_output": "Chunk ID: abc\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + status.String(),
			},
		},
		"stream": false,
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		t.Fatal(err)
	}
	var capturedBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_layer0","usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":0},"output_tokens":5}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Secrets.Mode = "off"
	p := New(cfg)

	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codex/0.130.0")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	msgs, _, err := extractMessages(types.CodexChatGPT, capturedBody)
	if err != nil {
		t.Fatal(err)
	}
	var toolText string
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				toolText = block.Text
			}
		}
	}
	if !strings.Contains(toolText, "Process exited with code 0") || !strings.Contains(toolText, "Output:\n[git status]") || strings.Contains(toolText, "local_119.go") {
		t.Fatalf("local shell envelope was not Layer-0 compacted: %q", toolText)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || !slices.Contains(summaries[0].LayersApplied, 0) {
		t.Fatalf("summary missing Layer 0: %#v", summaries)
	}
}

func TestServeHTTP_GenericOpenAIResponsesPassthrough(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"gpt-test","input":"generic responses request"}`)
	var capturedBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_openai","output":[]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "openai-python/2.0")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if string(capturedBody) != string(body) {
		t.Fatalf("generic OpenAI /v1/responses must pass through byte-equal: got %s want %s", capturedBody, body)
	}
	if summaries := p.DebugRecorder().Last(1, false); len(summaries) != 0 {
		t.Fatalf("passthrough request should not record compression summary: %#v", summaries)
	}
}

func TestServeHTTP_CodexUnknownShapePassthrough(t *testing.T) {
	t.Parallel()
	body := []byte(`{"conversation_id":"conv-redacted","metadata":{"route":"unknown"}}`)
	var capturedBody []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.CodexChatGPT.BaseURL = upstream.URL
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	req := httptest.NewRequest(http.MethodPost, "/backend-api/codex/conversations", strings.NewReader(string(body)))
	req.Header.Set("User-Agent", "codex/0.125.0 (rust)")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if string(capturedBody) != string(body) {
		t.Fatalf("unknown shape must pass through byte-equal: got %s want %s", capturedBody, body)
	}
}

func readCodexFixture(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

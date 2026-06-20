package servermirror

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func msg(texts ...string) []types.Message {
	blocks := make([]types.ContentBlock, len(texts))
	for i, t := range texts {
		blocks[i] = types.ContentBlock{Type: "tool_result", Text: t}
	}
	return []types.Message{{Role: "tool", Content: blocks}}
}

func TestMirror_ObserveThenPredictReferenceable(t *testing.T) {
	t.Parallel()
	m := New()
	content := strings.Repeat("server has this\n", 50)
	m.Observe("s1", msg(content))
	rep := m.Predict("s1", msg(content))
	if rep.Blocks != 1 || rep.ReferenceableBlocks != 1 || rep.PotentialSavedBytes != len(content) {
		t.Fatalf("forwarded content should be referenceable: %+v", rep)
	}
	if !rep.Predictions[0].AlreadyForwarded {
		t.Fatal("block should be AlreadyForwarded")
	}
}

func TestMirror_NovelContentNotReferenceable(t *testing.T) {
	t.Parallel()
	m := New()
	m.Observe("s", msg("alpha content here"))
	rep := m.Predict("s", msg("totally different beta content"))
	if rep.ReferenceableBlocks != 0 || rep.PotentialSavedBytes != 0 {
		t.Fatalf("novel content must not be referenceable: %+v", rep)
	}
	if rep.Predictions[0].AlreadyForwarded {
		t.Fatal("novel block must not be AlreadyForwarded")
	}
}

func TestMirror_NormalizedCodexExecPayloadPredictsThroughVolatileHeader(t *testing.T) {
	t.Parallel()
	m := New()
	payload := strings.Repeat("stable command payload\n", 20)
	first := "Chunk ID: first\nWall time: 0.0001 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + payload
	second := "Chunk ID: second\nWall time: 9.9999 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload
	m.Observe("s", msg(first))

	rep := m.Predict("s", msg(second))
	if rep.BlockBytes != len(second) || rep.ReferenceableBlocks != 0 || rep.PotentialSavedBytes != 0 {
		t.Fatalf("exact mirror must not match volatile envelopes: %+v", rep)
	}
	if rep.NormalizedSegments != 1 ||
		rep.NormalizedBytes != len(payload) ||
		rep.NormalizedReferenceableSegments != 1 ||
		rep.NormalizedPotentialSavedBytes != len(payload) {
		t.Fatalf("normalized payload should be referenceable in shadow: %+v", rep)
	}
	kind := rep.NormalizedPotentialSavedBytesByKind["codex_exec_payload"]
	if kind.Segments != 1 || kind.Bytes != len(payload) || kind.ReferenceableSegments != 1 || kind.PotentialSavedBytes != len(payload) {
		t.Fatalf("normalized kind accounting wrong: %+v", rep.NormalizedPotentialSavedBytesByKind)
	}
	if got := rep.NormalizedPredictions[0]; got.Kind != "codex_exec_payload" || !got.AlreadyForwarded || got.Bytes != len(payload) {
		t.Fatalf("normalized prediction wrong: %+v", got)
	}
}

func TestMirror_NormalizedCodexExecPayloadClassifiesCommandFamily(t *testing.T) {
	t.Parallel()
	m := New()
	payload := strings.Repeat("stable npm install output\n", 20)
	first := "Chunk ID: first\nProcess exited with code 0\nOutput:\n" + payload
	second := "Chunk ID: second\nProcess exited with code 0\nOutput:\n" + payload
	firstMsg := []types.Message{{Role: "tool", Content: []types.ContentBlock{{
		Type:      "tool_result",
		Text:      first,
		ToolInput: `{"cmd":"npm install"}`,
	}}}}
	secondMsg := []types.Message{{Role: "tool", Content: []types.ContentBlock{{
		Type:      "tool_result",
		Text:      second,
		ToolInput: `{"command":["/usr/local/bin/npm","install"]}`,
	}}}}
	m.Observe("s", firstMsg)

	rep := m.Predict("s", secondMsg)
	kind := rep.NormalizedPotentialSavedBytesByKind["codex_exec_payload_command_npm"]
	if kind.Segments != 1 || kind.ReferenceableSegments != 1 || kind.PotentialSavedBytes != len(payload) {
		t.Fatalf("command-family kind accounting wrong: %+v", rep.NormalizedPotentialSavedBytesByKind)
	}
	if got := rep.NormalizedPredictions[0]; got.Kind != "codex_exec_payload_command_npm" || !got.AlreadyForwarded {
		t.Fatalf("command-family prediction wrong: %+v", got)
	}
}

func TestMirror_NormalizedToolResultClassifiesResolvedCommandFamily(t *testing.T) {
	t.Parallel()
	m := New()
	payload := strings.Repeat("stable git status output\n", 20)
	firstMsg := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{
			Type:      "tool_use",
			ToolUseID: "call_git",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"git status --short"}`,
		}}},
		{Role: "tool", Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_git",
			Text:         payload,
		}}},
	}
	secondMsg := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{
			Type:      "tool_use",
			ToolUseID: "call_git_again",
			ToolName:  "exec_command",
			ToolInput: `{"command":["/usr/bin/git","status","--short"]}`,
		}}},
		{Role: "tool", Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_git_again",
			Text:         payload,
		}}},
	}
	m.Observe("s", firstMsg)

	rep := m.Predict("s", secondMsg)
	kind := rep.NormalizedPotentialSavedBytesByKind["tool_result_command_git"]
	if kind.Segments != 1 || kind.ReferenceableSegments != 1 || kind.PotentialSavedBytes != len(payload) {
		t.Fatalf("tool-result command-family accounting wrong: %+v", rep.NormalizedPotentialSavedBytesByKind)
	}
	if got := rep.NormalizedPredictions[0]; got.Kind != "tool_result_command_git" || !got.AlreadyForwarded {
		t.Fatalf("tool-result command-family prediction wrong: %+v", got)
	}
}

func TestMirror_NormalizedToolResultFallsBackWithoutResolvedCommand(t *testing.T) {
	t.Parallel()
	m := New()
	payload := strings.Repeat("stable opaque tool output\n", 20)
	firstMsg := []types.Message{{Role: "tool", Content: []types.ContentBlock{{
		Type:         "tool_result",
		ToolResultID: "missing_use",
		ToolName:     "Read_File",
		Text:         payload,
	}}}}
	secondMsg := []types.Message{{Role: "tool", Content: []types.ContentBlock{{
		Type:         "tool_result",
		ToolResultID: "still_missing",
		ToolName:     "Read_File",
		Text:         payload,
	}}}}
	m.Observe("s", firstMsg)

	rep := m.Predict("s", secondMsg)
	if _, ok := rep.NormalizedPotentialSavedBytesByKind["tool_result_command_read_file"]; ok {
		t.Fatalf("tool name must not be misclassified as command: %+v", rep.NormalizedPotentialSavedBytesByKind)
	}
	kind := rep.NormalizedPotentialSavedBytesByKind["tool_result_tool_read_file"]
	if kind.Segments != 1 || kind.ReferenceableSegments != 1 || kind.PotentialSavedBytes != len(payload) {
		t.Fatalf("tool-result tool fallback accounting wrong: %+v", rep.NormalizedPotentialSavedBytesByKind)
	}
}

func TestMirror_NormalizedNovelPayloadNotReferenceable(t *testing.T) {
	t.Parallel()
	m := New()
	firstPayload := strings.Repeat("first payload\n", 20)
	secondPayload := strings.Repeat("second payload\n", 20)
	first := "Chunk ID: first\nProcess exited with code 0\nOutput:\n" + firstPayload
	second := "Chunk ID: second\nProcess exited with code 0\nOutput:\n" + secondPayload
	m.Observe("s", msg(first))

	rep := m.Predict("s", msg(second))
	if rep.NormalizedReferenceableSegments != 0 || rep.NormalizedPotentialSavedBytes != 0 {
		t.Fatalf("novel normalized payload must not be referenceable: %+v", rep)
	}
}

func TestMirror_NormalizedHelpersCoverFallbacksAndMalformedEnvelopes(t *testing.T) {
	t.Parallel()

	if _, _, ok := splitCodexExecEnvelope("plain output without exit marker"); ok {
		t.Fatal("plain output must not parse as a Codex exec envelope")
	}
	if _, _, ok := splitCodexExecEnvelope("Process exited with code 0\nno output marker"); ok {
		t.Fatal("exec envelope without output marker must not parse")
	}
	if _, _, ok := splitCodexExecEnvelope("Process exited with code 0\nOutput:\n"); ok {
		t.Fatal("empty exec payload must not parse as referenceable")
	}
	_, payload, ok := splitCodexExecEnvelope("Process exited with code 0\r\nOutput:\r\nstable\r\n")
	if !ok || payload != "stable\r\n" {
		t.Fatalf("CRLF exec envelope parsed incorrectly: ok=%v payload=%q", ok, payload)
	}

	roleOnly := normalizedSegmentKind(types.Message{Role: "assistant"}, types.ContentBlock{})
	if roleOnly != "assistant" {
		t.Fatalf("role fallback kind wrong: %q", roleOnly)
	}
	textFallback := normalizedSegmentKind(types.Message{}, types.ContentBlock{})
	if textFallback != "text" {
		t.Fatalf("text fallback kind wrong: %q", textFallback)
	}
	if kind := normalizedSegmentKind(types.Message{Role: "tool"}, types.ContentBlock{Type: "tool_result", ToolName: "Read_File"}); kind != "tool_result_tool_read_file" {
		t.Fatalf("tool-result tool kind wrong: %q", kind)
	}
	if kind := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: `{"cmd":"git status --short"}`}); kind != "codex_exec_payload_command_git" {
		t.Fatalf("codex exec command kind wrong: %q", kind)
	}
	if kind := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: `{"command":["/opt/homebrew/bin/bash","-lc","go test ./..."]}`}); kind != "codex_exec_payload_command_go" {
		t.Fatalf("codex exec array command kind wrong: %q", kind)
	}
	if kind := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: `{"cmd":"env GIT_OPTIONAL_LOCKS=0 git status --short"}`}); kind != "codex_exec_payload_command_git" {
		t.Fatalf("codex exec env-wrapped command kind wrong: %q", kind)
	}
	if kind := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: `{"cmd":"!!!"}`}); kind != "codex_exec_payload" {
		t.Fatalf("unsafe command kind should fall back, got %q", kind)
	}
	commandInputs := map[string]string{
		`"cargo test --workspace"`:                         "codex_exec_payload_command_cargo",
		`{"command_line":"go test ./..."}`:                 "codex_exec_payload_command_go",
		`{"shellCommand":"pnpm build"}`:                    "codex_exec_payload_command_pnpm",
		`{"argv":["/usr/bin/python3","-m","pytest"]}`:      "codex_exec_payload_command_python3",
		`{"args":["/bin/zsh","-c","terraform plan"]}`:      "codex_exec_payload_command_terraform",
		`not-json-command --flag`:                          "codex_exec_payload_command_not_json_command",
		`{"command":["env","-i","PATH=/bin","git","log"]}`: "codex_exec_payload_command_git",
		`{"command":123}`:                                  "codex_exec_payload",
		`{"argv":[1,2]}`:                                   "codex_exec_payload",
		`{"unknown":"value"}`:                              "codex_exec_payload",
		``:                                                 "codex_exec_payload",
	}
	for input, want := range commandInputs {
		if got := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: input}); got != want {
			t.Fatalf("command input %q classified as %q, want %q", input, got, want)
		}
	}

	m := New()
	m.Observe("", msg("must not attach to an empty session"))
	if rep := m.Predict("non-empty", msg("must not attach to an empty session")); rep.ReferenceableBlocks != 0 {
		t.Fatalf("empty-session observe must not seed another session: %+v", rep)
	}
}

// TestMirror_NoFalseElisionProperty is the catastrophic-bug guard: content the
// mirror NEVER observed must NEVER be predicted referenceable, even after
// observing many other blocks.
func TestMirror_NoFalseElisionProperty(t *testing.T) {
	t.Parallel()
	m := New()
	for i := 0; i < 200; i++ {
		m.Observe("s", msg(fmt.Sprintf("observed block number %d with filler", i)))
	}
	for i := 0; i < 200; i++ {
		never := fmt.Sprintf("NEVER-FORWARDED unique content %d", i)
		rep := m.Predict("s", msg(never))
		if rep.ReferenceableBlocks != 0 || rep.Predictions[0].AlreadyForwarded {
			t.Fatalf("false elision: un-observed content predicted referenceable: %q", never)
		}
	}
}

func TestMirror_SessionIsolation(t *testing.T) {
	t.Parallel()
	m := New()
	content := "session A private content xyz"
	m.Observe("a", msg(content))
	if rep := m.Predict("b", msg(content)); rep.ReferenceableBlocks != 0 {
		t.Fatalf("session b must not reference session a content: %+v", rep)
	}
	if rep := m.Predict("a", msg(content)); rep.ReferenceableBlocks != 1 {
		t.Fatal("session a should reference its own content")
	}
}

func TestMirror_ResetAndNilNoOps(t *testing.T) {
	t.Parallel()
	var nilM *Mirror
	nilM.Observe("s", msg("x")) // must not panic
	if rep := nilM.Predict("s", msg("x")); rep.ReferenceableBlocks != 0 {
		t.Fatal("nil mirror predicts nothing referenceable")
	}
	nilM.Reset("s")

	m := New()
	c := "reset me content"
	m.Observe("s", msg(c))
	m.Reset("s")
	if rep := m.Predict("s", msg(c)); rep.ReferenceableBlocks != 0 {
		t.Fatal("after reset, content must not be referenceable")
	}
	if rep := m.Predict("", msg(c)); rep.ReferenceableBlocks != 0 {
		t.Fatal("empty session predicts nothing referenceable")
	}
	if rep := m.Predict("s", msg("")); rep.Blocks != 0 {
		t.Fatal("empty text blocks are not counted")
	}
}

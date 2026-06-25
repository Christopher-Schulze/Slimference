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

func TestSameRequestExactCountsOnlyLaterVisibleDuplicates(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("same visible block\n", 10)
	report := SameRequestExact([]types.Message{
		{
			Role:    "tool",
			Content: []types.ContentBlock{{Type: "tool_result", Text: content}},
		},
		{
			Role:    "tool",
			Content: []types.ContentBlock{{Type: "tool_result", Text: content}},
		},
		{
			Role:    "tool",
			Content: []types.ContentBlock{{Type: "tool_result", Text: "different"}},
		},
	})
	if report.Blocks != 3 || report.Bytes != len(content)*2+len("different") {
		t.Fatalf("bad same-request block accounting: %+v", report)
	}
	if report.ReferenceableBlocks != 1 || report.PotentialSavedBytes != len(content) {
		t.Fatalf("only the second exact copy should be referenceable: %+v", report)
	}
	kind := report.PotentialSavedBytesByKind["tool_result"]
	if kind.Segments != 3 || kind.ReferenceableSegments != 1 || kind.PotentialSavedBytes != len(content) {
		t.Fatalf("bad same-request kind report: %+v", report.PotentialSavedBytesByKind)
	}
}

func TestSameRequestExactDoesNotCountVolatileCodexExecEnvelope(t *testing.T) {
	t.Parallel()
	payload := strings.Repeat("ok  example.com/pkg 0.010s\n", 8)
	first := "Chunk ID: first\nProcess exited with code 0\nOutput:\n" + payload
	second := "Chunk ID: second\nProcess exited with code 0\nOutput:\n" + payload
	report := SameRequestExact([]types.Message{
		{
			Role: "tool",
			Content: []types.ContentBlock{{
				Type:      "tool_result",
				Text:      first,
				ToolInput: `{"cmd":"go test ./..."}`,
			}},
		},
		{
			Role: "tool",
			Content: []types.ContentBlock{{
				Type:      "tool_result",
				Text:      second,
				ToolInput: `{"cmd":"go test ./..."}`,
			}},
		},
	})
	if report.ReferenceableBlocks != 0 || report.PotentialSavedBytes != 0 {
		t.Fatalf("volatile full envelopes must not exact-match: %+v", report)
	}
	if got := report.PotentialSavedBytesByKind["codex_exec_payload_command_go"]; got.Segments != 2 || got.ReferenceableSegments != 0 {
		t.Fatalf("same-request exact kind accounting changed: %+v", report.PotentialSavedBytesByKind)
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

// TestMirror_FullHistoryResendSizing is the proof-gate-step-1 sizing for the
// server-state mutation lever (new-servermirror-mutation-ultra.md): on a
// FULL-HISTORY resend (the lever's only real surface — delta turns resend
// ~nothing via native previous_response_id continuation), how much of the
// resent history is normalized-referenceable, i.e. the validated upper bound the
// mutation step could substitute? It also confirms WHY whole-block mirroring
// predicts ~0 (volatile per-call exec envelopes), so the normalized-segment path
// is the only viable one. No mutation here (that is hard-blocked on the
// stateless-detach keystone apply); this sizes the opportunity.
func TestMirror_FullHistoryResendSizing(t *testing.T) {
	t.Parallel()
	m := New()
	const k = 8
	body := func(i int) string {
		return strings.Repeat("stable tool output line for command "+string(rune('A'+i))+"\n", 30)
	}
	envelope := func(chunkID, b string) string {
		// Volatile per-call exec header (changes every turn) + stable body.
		return "Chunk ID: " + chunkID + "\nWall time: 0.1234 seconds\nProcess exited with code 0\nOutput:\n" + b
	}
	// K prior tool outputs were each forwarded to the server on earlier turns.
	var stableBytes int
	for i := 0; i < k; i++ {
		m.Observe("s", msg(envelope("obs-"+string(rune('A'+i)), body(i))))
		stableBytes += len(body(i))
	}
	// Full-history resend turn: all K prior outputs reappear with NEW volatile
	// headers (so whole-block hashing cannot match them) plus one genuinely new
	// output the server has never seen.
	resent := make([]string, 0, k+1)
	for i := 0; i < k; i++ {
		resent = append(resent, envelope("resend-"+string(rune('A'+i)), body(i)))
	}
	newBody := strings.Repeat("brand new never-forwarded output\n", 30)
	resent = append(resent, envelope("resend-NEW", newBody))

	rep := m.Predict("s", msg(resent...))

	// Whole-block predicts ~0: the volatile envelopes make every block non-identical.
	if rep.PotentialSavedBytes != 0 {
		t.Fatalf("whole-block mirroring must predict 0 through volatile envelopes, got %d", rep.PotentialSavedBytes)
	}
	// Normalized predicts exactly the K stable bodies (the new one is not referenceable).
	if rep.NormalizedReferenceableSegments != k {
		t.Fatalf("normalized must reference exactly the %d resent stable bodies, got %d", k, rep.NormalizedReferenceableSegments)
	}
	if rep.NormalizedPotentialSavedBytes != stableBytes {
		t.Fatalf("normalized referenceable bytes = %d, want %d (sum of K stable bodies)", rep.NormalizedPotentialSavedBytes, stableBytes)
	}
	if rep.NormalizedPotentialSavedBytes <= rep.PotentialSavedBytes {
		t.Fatalf("normalized path must beat whole-block on a full-history resend")
	}
	ratio := float64(rep.NormalizedPotentialSavedBytes) / float64(rep.NormalizedBytes)
	t.Logf("full-history resend sizing: normalized referenceable %d / %d bytes (%.1f%%) across %d/%d segments; whole-block %d",
		rep.NormalizedPotentialSavedBytes, rep.NormalizedBytes, ratio*100, rep.NormalizedReferenceableSegments, rep.NormalizedSegments, rep.PotentialSavedBytes)
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
	if kind := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: `{"cmd":"git status --short"}`}, ""); kind != "codex_exec_payload_command_git" {
		t.Fatalf("codex exec command kind wrong: %q", kind)
	}
	if kind := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: `{"command":["/opt/homebrew/bin/bash","-lc","go test ./..."]}`}, ""); kind != "codex_exec_payload_command_go" {
		t.Fatalf("codex exec array command kind wrong: %q", kind)
	}
	if kind := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: `{"cmd":"env GIT_OPTIONAL_LOCKS=0 git status --short"}`}, ""); kind != "codex_exec_payload_command_git" {
		t.Fatalf("codex exec env-wrapped command kind wrong: %q", kind)
	}
	if kind := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: `{"cmd":"!!!"}`}, ""); kind != "codex_exec_payload" {
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
		if got := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: input}, ""); got != want {
			t.Fatalf("command input %q classified as %q, want %q", input, got, want)
		}
	}
	payloadInputs := map[string]string{
		"ok  github.com/Christopher-Schulze/Slimference/internal/proxy 0.123s\nok  github.com/Christopher-Schulze/Slimference/internal/servermirror 0.045s\n":                                                                                                                                            "codex_exec_payload_command_go",
		"internal/proxy/layer0_proxy.go:2048:\tif proxyLooksLikeGoTestOutput(payload) {\ninternal/proxy/layer0_proxy.go:2049:\t\treturn \"go test\"\ninternal/servermirror/mirror.go:305:func payloadLooksLikeGoTestOutput(payload string) bool {\n":                                                     "codex_exec_payload_command_rg",
		" M internal/servermirror/mirror.go\n?? docs/todo/t418.md\n M internal/proxy/layer0_proxy.go\n":                                                                                                                                                                                                  "codex_exec_payload_command_git",
		"internal/servermirror/mirror.go      | 42 +++++++++++++++++++++\ninternal/servermirror/mirror_test.go | 31 +++++++++++++++\n2 files changed, 73 insertions(+)\n":                                                                                                                                "codex_exec_payload_command_git",
		"commit abcdef1234567890abcdef1234567890abcdef12\nAuthor: Test <test@example.com>\n\n    sample\n\ninternal/servermirror/mirror.go | 10 +++++-----\n1 file changed, 5 insertions(+), 5 deletions(-)\n":                                                                                           "codex_exec_payload_command_git",
		"M\tinternal/servermirror/mirror.go\nA\tinternal/servermirror/mirror_test.go\nD\tdocs/old.md\n":                                                                                                                                                                                                  "codex_exec_payload_command_git",
		"abcdef1 TASK T418: rank WSS shadow opportunities\n1234567 TASK T417: server state continuation\nfedcba9 TASK T419: archive recovery gate\n":                                                                                                                                                     "codex_exec_payload_command_git",
		"  120 internal/servermirror/mirror.go\n   87 internal/servermirror/mirror_test.go\n":                                                                                                                                                                                                            "codex_exec_payload_command_wc",
		"internal/servermirror/mirror.go\ninternal/servermirror/mirror_test.go\ninternal/proxy/layer0_proxy.go\ndocs/todo/t418.md\ncmd/slimference/gain.go\n":                                                                                                                                            "codex_exec_payload_command_find",
		`{"$schema":"https://json.schemastore.org/sarif-2.1.0.json","version":"2.1.0","runs":[{"tool":{"driver":{"name":"eslint"}},"results":[]}]}` + "\n":                                                                                                                                               "codex_exec_payload_command_sarif",
		"added 120 packages, and audited 121 packages in 2s\n45 packages are looking for funding\nfound 0 vulnerabilities\n":                                                                                                                                                                             "codex_exec_payload_command_npm",
		"Terraform will perform the following actions:\n\n  # aws_s3_bucket.example will be created\n\nPlan: 12 to add, 0 to change, 0 to destroy.\n":                                                                                                                                                    "codex_exec_payload_command_terraform",
		"NAME      READY   STATUS             RESTARTS   AGE\napi-0     1/1     Running            0          2m\nworker-0  0/1     CrashLoopBackOff   3          1m\n":                                                                                                                                  "codex_exec_payload_command_kubectl",
		"total 320\n-rw-r--r--  1 user staff 1200 Jan 01 00:00 file_00.go\n-rw-r--r--  1 user staff 1201 Jan 01 00:00 file_01.go\n-rw-r--r--  1 user staff 1202 Jan 01 00:00 file_02.go\n-rw-r--r--  1 user staff 1203 Jan 01 00:00 file_03.go\n-rw-r--r--  1 user staff 1204 Jan 01 00:00 file_04.go\n": "codex_exec_payload_command_ls",
		".\n├── cmd\n│   └── slimference\n└── internal\n    └── servermirror\n\n4 directories, 0 files\n":                                                                                                                                                                                                "codex_exec_payload_command_tree",
		"warning:12:looks like line syntax but no file path\ntrace:34:also not a grep path\nplain:56:still ambiguous\n":                                                                                                                                                                                  "codex_exec_payload",
		"this is just ambiguous prose\nwith multiple stable lines\nbut no command shape\n":                                                                                                                                                                                                               "codex_exec_payload",
	}
	for payload, want := range payloadInputs {
		if got := normalizedCodexExecPayloadKind(types.ContentBlock{}, payload); got != want {
			t.Fatalf("payload %q classified as %q, want %q", payload, got, want)
		}
	}
	if got := normalizedCodexExecPayloadKind(types.ContentBlock{ToolInput: `{"cmd":"npm install"}`}, "ok  github.com/example/project 0.123s\nok  github.com/example/other 0.123s\n"); got != "codex_exec_payload_command_npm" {
		t.Fatalf("tool input must win over payload inference, got %q", got)
	}

	m := New()
	m.Observe("", msg("must not attach to an empty session"))
	if rep := m.Predict("non-empty", msg("must not attach to an empty session")); rep.ReferenceableBlocks != 0 {
		t.Fatalf("empty-session observe must not seed another session: %+v", rep)
	}
}

func TestMirror_PayloadInferenceBoundaries(t *testing.T) {
	t.Parallel()

	if !payloadLooksLikeSARIFOutput(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"sarif-runner"}}}]}`) {
		t.Fatal("SARIF marker fallback should classify")
	}
	if payloadLooksLikeSARIFOutput("not json") {
		t.Fatal("non-JSON SARIF payload must not classify")
	}
	if payloadLooksLikeSARIFOutput(`{"$schema":"https://json.schemastore.org/sarif-2.1.0.json","version":"2.0.0","runs":[]}`) {
		t.Fatal("wrong SARIF version must not classify")
	}
	if payloadLooksLikeSARIFOutput(`{"$schema":"https://json.schemastore.org/sarif-2.1.0.json","version":"2.1.0","runs":{}}`) {
		t.Fatal("SARIF runs object must not classify")
	}
	if payloadLooksLikeSARIFOutput(`{"version":"2.1.0","runs":[{}]}`) {
		t.Fatal("SARIF run without tool/results must not classify")
	}
	if !payloadLooksLikePackageInstallOutput("added 12 packages, and audited 13 packages in 1s\nfound 0 vulnerabilities\n") {
		t.Fatal("npm install success should classify")
	}
	if !payloadLooksLikeTerraformPlanOutput("Terraform will perform the following actions:\n\nPlan: 0 to add, 1 to change, 0 to destroy.\n") {
		t.Fatal("terraform plan should classify")
	}
	if payloadLooksLikeTerraformPlanOutput("Terraform will perform the following actions:\n\nNo changes. Your infrastructure matches the configuration.\n") {
		t.Fatal("terraform no-op output without Plan counts must not classify")
	}
	if !payloadLooksLikeKubectlGetOutput("NAME READY STATUS RESTARTS AGE\npod-a 1/1 Running 0 1m\npod-b 1/1 Running 0 2m\n") {
		t.Fatal("kubectl get table should classify")
	}
	if !payloadLooksLikeLsLongOutput("total 5\ndrwxr-xr-x 2 user staff 64 Jan 01 00:00 dir\nlrwxr-xr-x 1 user staff 3 Jan 01 00:00 link -> dst\n-rw-r--r-- 1 user staff 1 Jan 01 00:00 a\n-rw-r--r-- 1 user staff 2 Jan 01 00:00 b\n-rw-r--r-- 1 user staff 3 Jan 01 00:00 c\n") {
		t.Fatal("ls long output should classify")
	}
	if looksLikeLsMode("xrwxrwxrwx") || looksLikeLsMode("-rwxbad") || !looksLikeLsMode("-rw-r--r--") {
		t.Fatal("ls mode guard misclassified")
	}
	if !payloadLooksLikeTreeOutput(".\n|-- cmd\n`-- internal\n\n2 directories, 0 files\n") {
		t.Fatal("ASCII tree output should classify")
	}
	if got := inferCommandLineFromCodexExecPayload(" \n\t "); got != "" {
		t.Fatalf("empty payload inferred as %q", got)
	}
	if got := inferCommandLineFromCodexExecPayload("=== RUN   TestThing\n--- PASS: TestThing (0.00s)\nPASS\n"); got != "go test" {
		t.Fatalf("verbose go test payload inferred as %q", got)
	}
	showStat := "commit abcdef1234567890abcdef1234567890abcdef12\nAuthor: Test <test@example.com>\n\n    sample\n\ninternal/servermirror/mirror.go | 10 +++++-----\n1 file changed, 5 insertions(+), 5 deletions(-)\n"
	if got := inferCommandLineFromCodexExecPayload(showStat); got != "git show --stat" {
		t.Fatalf("git show stat payload inferred as %q", got)
	}
	if got := commandBaseFromFields(nil); got != "" {
		t.Fatalf("empty command fields returned %q", got)
	}
	if payloadLooksLikeGoTestOutput("ok  github.com/example/one 0.1s\nplain line\n") {
		t.Fatal("single go-test-like package line plus prose must not classify")
	}
	if payloadLooksLikeGoTestOutput("ok  github.com/example/one 0.1s\n\nplain line\n") {
		t.Fatal("go-test sparse output with prose must not classify")
	}
	if payloadLooksLikeSARIFOutput(`{"version":"2.1.0","runs":[]}`) {
		t.Fatal("SARIF without schema or sarif marker must not classify")
	}
	if payloadLooksLikePackageInstallOutput("added two ideas and audited the document; found 0 vulnerabilities in prose") {
		t.Fatal("package prose must not classify as install output")
	}
	if payloadLooksLikeTerraformPlanOutput("Plan: write the code and add tests") {
		t.Fatal("plain plan prose must not classify as terraform")
	}
	if payloadLooksLikeKubectlGetOutput("NAME VALUE\nthing 1\nother 2\n") {
		t.Fatal("short two-column table must not classify as kubectl get")
	}
	if payloadLooksLikeKubectlGetOutput("\nPOD READY STATUS\npod-a 1/1 Running\npod-b 1/1 Running\n") {
		t.Fatal("kubectl-like output without NAME header must not classify")
	}
	if payloadLooksLikeKubectlGetOutput("NAME READY STATUS RESTARTS AGE\npod-a 1/1\npod-b 1/1\n") {
		t.Fatal("kubectl rows with too few columns must not classify")
	}
	if payloadLooksLikeLsLongOutput("-rw-r--r-- one malformed row\n-rw-r--r-- another malformed row\n") {
		t.Fatal("malformed ls rows must not classify")
	}
	if looksLikeLsMode("-rw-r--q--") || !looksLikeLsMode("drwxr-xr-x") || !looksLikeLsMode("lrwxrwxrwx") {
		t.Fatal("ls mode branch coverage misclassified")
	}
	if payloadLooksLikeLsLongOutput("total 2\n-rw-r--r-- 1 user staff 1 Jan 01 00:00 a\nplain prose\n") {
		t.Fatal("mixed sparse ls output must not classify")
	}
	if payloadLooksLikeTreeOutput(".\ncmd\ninternal\n\n2 directories, 0 files\n") {
		t.Fatal("plain summary without tree drawing must not classify")
	}
	if payloadLooksLikeSearchOutput("Total output lines: 2\nplain:12:not/a/path\nother:34:still/no/path\n") {
		t.Fatal("colon prose without path must not classify as search")
	}
	for _, line := range []string{"no-colon", "file.go:x:needle", "file.go:12:", "file.go::content", ":12:content", "file.go:12:content"} {
		if line == "file.go:12:content" {
			if !payloadLooksLikeSearchResultLine(line) {
				t.Fatalf("valid search line not classified: %q", line)
			}
			continue
		}
		if payloadLooksLikeSearchResultLine(line) {
			t.Fatalf("invalid search line classified: %q", line)
		}
	}
	if gitStatusXY("M") || gitStatusXY("ZZ") {
		t.Fatal("invalid git status XY classified")
	}
	if payloadLooksLikeGitDiffNameStatusOutput("M\tone.go\nplain\n") {
		t.Fatal("sparse name-status-like output must not classify")
	}
	if payloadLooksLikeGitDiffNameStatusOutput("M\tone.go\n\nplain\n") {
		t.Fatal("name-status-like output with blank/prose must not classify")
	}
	if payloadLooksLikeGitLogOnelineOutput("nothex1 first\nnothex2 second\nnothex3 third\n") {
		t.Fatal("non-hex log-like output must not classify")
	}
	if payloadLooksLikeGitLogOnelineOutput("abcdef1 first\n\nplain\n") {
		t.Fatal("sparse log-like output with prose must not classify")
	}
	if payloadLooksLikeWcOutput("abc file.go\n123\n") {
		t.Fatal("mixed wc-like output must not classify")
	}
	if payloadLooksLikeWcOutput("123 file.go\n\nplain\n") {
		t.Fatal("sparse wc-like output with prose must not classify")
	}
	if allDecimal("") || allDecimal("12x") {
		t.Fatal("invalid decimal classified")
	}
	if payloadLooksLikePlainPathListOutput("key:value\nother:value\nthird:value\nfourth:value\nfifth:value\n") {
		t.Fatal("colon-heavy output must not classify as plain path list")
	}
	if got := sanitizeKindSuffix("\u00dcber Cmd!*"); got != "bercmd" {
		t.Fatalf("unicode unsafe suffix sanitized as %q", got)
	}
}

func TestMirror_StateBoundaryBranches(t *testing.T) {
	t.Parallel()

	m := New()
	full := make(map[string]struct{}, maxBlocksPerSession)
	for i := range maxBlocksPerSession {
		full[fmt.Sprintf("h-%d", i)] = struct{}{}
	}
	m.sessions["full"] = full
	m.Observe("full", msg("new content must not fit"))
	if _, ok := m.sessions["full"][hashContent("new content must not fit")]; ok {
		t.Fatal("full exact mirror must not accept new content")
	}

	fullNormalized := make(map[string]struct{}, maxBlocksPerSession)
	for i := range maxBlocksPerSession {
		fullNormalized[fmt.Sprintf("nh-%d", i)] = struct{}{}
	}
	m.normalizedSessions["normalized-full"] = fullNormalized
	m.Observe("normalized-full", msg("Process exited with code 0\nOutput:\nstable payload\n"))
	if _, ok := m.normalizedSessions["normalized-full"][hashContent("stable payload\n")]; ok {
		t.Fatal("full normalized mirror must not accept new content")
	}

	var nilM *Mirror
	rep := nilM.Predict("s", msg("", "counted"))
	if rep.Blocks != 1 {
		t.Fatalf("nil predict should count only non-empty text blocks, got %+v", rep)
	}

	uses := toolUseIndexFromMessages([]types.Message{{Content: []types.ContentBlock{
		{Type: "assistant", ToolUseID: "ignored"},
		{Type: "tool_use"},
	}}})
	if len(uses) != 0 {
		t.Fatalf("invalid tool uses indexed: %+v", uses)
	}
	block := blockWithResolvedToolUse(types.ContentBlock{ToolResultID: "missing", ToolInput: "keep"}, map[string]types.ContentBlock{
		"other": {ToolInput: "replace"},
	})
	if block.ToolInput != "keep" {
		t.Fatalf("missing tool use must leave block unchanged: %+v", block)
	}
}

// TestMirror_NoFalseElisionProperty is the catastrophic-bug guard: content the
// mirror NEVER observed must NEVER be predicted referenceable, even after
// observing many other blocks.
func TestMirror_NoFalseElisionProperty(t *testing.T) {
	t.Parallel()
	m := New()
	for i := range 200 {
		m.Observe("s", msg(fmt.Sprintf("observed block number %d with filler", i)))
	}
	for i := range 200 {
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

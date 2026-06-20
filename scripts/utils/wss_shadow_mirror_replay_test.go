package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSSShadowMirrorReplayCommandReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	payload := strings.Repeat("ok  github.com/Christopher-Schulze/Slimference/internal/proxy 0.010s\n", 20)
	writeJSONLFile(t, path,
		wssABReplayTestRecord("client_to_server", wssShadowMirrorReplayTestBody("shadow-cli", "", "call-1", "go test ./...", "Chunk ID: first\nProcess exited with code 0\nOutput:\n"+payload)),
		wssABReplayTestRecord("client_to_server", wssShadowMirrorReplayTestBody("shadow-cli", "resp-1", "call-2", "go test ./...", "Chunk ID: second\nProcess exited with code 0\nOutput:\n"+payload)),
	)

	report, err := loadWSSShadowMirrorReplayReport(wssShadowMirrorReplayFlags{path: path})
	if err != nil {
		t.Fatal(err)
	}
	if report.RequestTurns != 2 || report.Normalized.ReferenceableBytes != len(payload) || len(report.Rows) != 1 {
		t.Fatalf("bad report: %+v", report)
	}
	if report.Rows[0].Kind != "codex_exec_payload_command_go" || report.Rows[0].CandidateTokensEstimate <= 0 {
		t.Fatalf("bad row: %+v", report.Rows)
	}

	var stdout, stderr bytes.Buffer
	code := runWSSShadowMirrorReplay([]string{path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runWSSShadowMirrorReplay code=%d stderr=%s", code, stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{
		"WSS shadow mirror replay",
		"kind=codex_exec_payload_command_go",
		"candidate_tokens=",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, payload) {
		t.Fatalf("text output leaked payload:\n%s", text)
	}
}

func TestWSSShadowMirrorReplayParsesSocketSeqFlag(t *testing.T) {
	flags, err := parseWSSShadowMirrorReplayFlags([]string{"frames.jsonl", "--json", "--socket-seq=8"})
	if err != nil {
		t.Fatal(err)
	}
	if flags.path != "frames.jsonl" || flags.outputFormat != outputJSON || flags.socketSeq != 8 {
		t.Fatalf("flags=%+v", flags)
	}
}

func TestWSSShadowMirrorReplayDirectoryAggregatesAndSkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	firstDir := filepath.Join(dir, "first")
	secondDir := filepath.Join(dir, "second")
	if err := os.Mkdir(firstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(secondDir, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(firstDir, "frames.jsonl")
	second := filepath.Join(secondDir, "frames.jsonl")
	empty := filepath.Join(dir, "empty.frames.jsonl")
	payload := strings.Repeat("ok  example.com/pkg 0.010s\n", 16)
	writeJSONLFile(t, first,
		wssABReplayTestRecord("client_to_server", wssShadowMirrorReplayTestBody("shadow-dir-a", "", "call-a1", "go test ./...", "Chunk ID: first-a\nProcess exited with code 0\nOutput:\n"+payload)),
		wssABReplayTestRecord("client_to_server", wssShadowMirrorReplayTestBody("shadow-dir-a", "resp-a", "call-a2", "go test ./...", "Chunk ID: second-a\nProcess exited with code 0\nOutput:\n"+payload)),
	)
	writeJSONLFile(t, second,
		wssABReplayTestRecord("client_to_server", wssShadowMirrorReplayTestBody("shadow-dir-b", "", "call-b1", "go test ./...", "Chunk ID: first-b\nProcess exited with code 0\nOutput:\n"+payload)),
		wssABReplayTestRecord("client_to_server", wssShadowMirrorReplayTestBody("shadow-dir-b", "resp-b", "call-b2", "go test ./...", "Chunk ID: second-b\nProcess exited with code 0\nOutput:\n"+payload)),
	)
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := loadWSSShadowMirrorReplayReport(wssShadowMirrorReplayFlags{path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 3 || report.SkippedFiles != 1 || report.RequestTurns != 4 || len(report.TopFiles) != 2 {
		t.Fatalf("bad directory report: %+v", report)
	}
	if report.Normalized.ReferenceableBytes != len(payload)*2 || report.Rows[0].Kind != "codex_exec_payload_command_go" {
		t.Fatalf("bad aggregate rows: %+v", report)
	}
	var stdout, stderr bytes.Buffer
	code := runWSSShadowMirrorReplay([]string{dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("directory run code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "top_files:") || !strings.Contains(stdout.String(), "skipped_files:      1") {
		t.Fatalf("directory text missing aggregate details:\n%s", stdout.String())
	}
}

func TestWSSShadowMirrorReplayErrors(t *testing.T) {
	if _, err := collectWSSShadowMirrorReplayFiles(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing replay path should error")
	}
	emptyDir := t.TempDir()
	if _, err := collectWSSShadowMirrorReplayFiles(emptyDir); err == nil {
		t.Fatal("empty replay dir should error")
	}
	if code := runWSSShadowMirrorReplay(nil, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("missing args exit=%d, want 2", code)
	}
	if code := runWSSShadowMirrorReplay([]string{"--bad"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("bad flag exit=%d, want 2", code)
	}
}

func wssShadowMirrorReplayTestBody(session, previousResponseID, callID, command, output string) map[string]any {
	body := map[string]any{
		"model":            "gpt-5-codex",
		"prompt_cache_key": session,
		"input": []map[string]any{
			{
				"type":    "local_shell_call",
				"call_id": callID,
				"command": []string{"bash", "-lc", command},
			},
			{
				"type":              "local_shell_call_output",
				"call_id":           callID,
				"command":           []string{"bash", "-lc", command},
				"aggregated_output": output,
			},
		},
		"stream": true,
	}
	if previousResponseID != "" {
		body["previous_response_id"] = previousResponseID
	}
	return body
}

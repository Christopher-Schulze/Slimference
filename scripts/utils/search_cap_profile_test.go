package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSearchCapProfileJSONAndText(t *testing.T) {
	t.Parallel()

	path := writeSearchCapProfileFixture(t)
	var stdout, stderr bytes.Buffer
	code := runSearchCapProfile([]string{
		"--command", "rg -n function",
		"--input", path,
		"--json",
		"--require-applicable",
		"--require-aggressive-savings",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runSearchCapProfile json code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProfileReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if !report.GatePassed || len(report.Profiles) != 2 {
		t.Fatalf("bad report gate/profile count: %+v", report)
	}
	defaultRow := report.Profiles[0]
	aggressiveRow := report.Profiles[1]
	if defaultRow.Name != "default" ||
		defaultRow.OriginalFiles != 35 ||
		defaultRow.OriginalMatches != 875 ||
		defaultRow.ShownFiles != 30 ||
		defaultRow.ShownMatches != 600 {
		t.Fatalf("bad default row: %+v", defaultRow)
	}
	if aggressiveRow.Name != "aggressive" ||
		aggressiveRow.ShownFiles != 10 ||
		aggressiveRow.ShownMatches != 50 ||
		aggressiveRow.SavedBytesVsDefault <= 0 ||
		aggressiveRow.OmittedMatchesVsDefault <= 0 {
		t.Fatalf("bad aggressive row: %+v", aggressiveRow)
	}
	if strings.Contains(stdout.String(), "fatal timeout rejected") {
		t.Fatalf("profile report must stay content-free, got raw match text:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runSearchCapProfile([]string{"--command=rg -n function", "--input=" + path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runSearchCapProfile text code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Search Cap Profile") ||
		!strings.Contains(stdout.String(), "delta vs default") ||
		strings.Contains(stdout.String(), "fatal timeout rejected") {
		t.Fatalf("unexpected text report:\n%s", stdout.String())
	}
}

func TestRunSearchCapProfileFrames(t *testing.T) {
	t.Parallel()

	outputPath := writeSearchCapProfileFixture(t)
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	framesPath := writeSearchCapProfileFramesFixture(t, string(output))
	var stdout, stderr bytes.Buffer
	code := runSearchCapProfile([]string{
		"--frames", framesPath,
		"--json",
		"--require-applicable",
		"--require-aggressive-savings",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runSearchCapProfile frames code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProfileReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.Source != "frames" || report.Frames != 2 || report.SearchOutputs != 1 {
		t.Fatalf("bad frame report shape: %+v", report)
	}
	if !report.GatePassed || len(report.Profiles) != 2 || !report.Profiles[0].Applied {
		t.Fatalf("bad frame report gate/profile count: %+v", report)
	}
	if report.Profiles[1].SavedBytesVsDefault <= 0 || report.Profiles[1].OmittedMatchesVsDefault <= 0 {
		t.Fatalf("aggressive frame profile did not tighten caps: %+v", report.Profiles[1])
	}
	if strings.Contains(stdout.String(), "fatal timeout rejected") {
		t.Fatalf("frame profile report must stay content-free, got raw match text:\n%s", stdout.String())
	}
}

func TestRunSearchCapProfileFramesStripsCodexExecEnvelope(t *testing.T) {
	t.Parallel()

	outputPath := writeSearchCapProfileFixture(t)
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	enveloped := "Chunk ID: search-proof\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 9000\nOutput:\n" + string(output)
	framesPath := writeSearchCapProfileFramesFixture(t, enveloped)

	var stdout, stderr bytes.Buffer
	code := runSearchCapProfile([]string{
		"--frames", framesPath,
		"--json",
		"--require-applicable",
		"--require-aggressive-savings",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runSearchCapProfile enveloped frames code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report searchCapProfileReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if report.SearchOutputs != 1 || !report.Profiles[0].Applied ||
		report.Profiles[0].InputBytes != len(output) {
		t.Fatalf("enveloped search output was not profiled as payload-only search stdout: %+v", report)
	}
	if strings.Contains(stdout.String(), "fatal timeout rejected") {
		t.Fatalf("enveloped frame profile report must stay content-free, got raw match text:\n%s", stdout.String())
	}
}

func TestRunSearchCapProfileCandidatesAndRetentionGate(t *testing.T) {
	t.Parallel()

	path := writeSearchCapProfileFixture(t)
	var stdout, stderr bytes.Buffer
	code := runSearchCapProfile([]string{
		"--command", "rg -n function",
		"--input", path,
		"--candidate", "25:15",
		"--candidate=20:10",
		"--require-aggressive-savings",
		"--min-candidate-retained-pct", "40",
		"--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("candidate retention floor code=%d want 0 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report searchCapProfileReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json output did not parse: %v\n%s", err, stdout.String())
	}
	if !report.GatePassed || len(report.Profiles) != 3 {
		t.Fatalf("bad candidate floor/report shape: %+v", report)
	}
	if report.SelectedCandidate == nil ||
		report.SelectedCandidate.Name != "candidate_20x10" ||
		report.SelectedCandidate.SavedBytesVsDefault != report.Profiles[2].SavedBytesVsDefault ||
		report.SelectedCandidate.MinRetainedPct != 40 {
		t.Fatalf("bad selected candidate: %+v report=%+v", report.SelectedCandidate, report)
	}
	if report.Profiles[1].Name != "candidate_25x15" ||
		report.Profiles[1].ShownFiles != 25 ||
		report.Profiles[1].ShownMatches != 375 ||
		report.Profiles[1].SavedBytesVsDefault <= 0 {
		t.Fatalf("bad first candidate row: %+v", report.Profiles[1])
	}
	if report.Profiles[2].Name != "candidate_20x10" ||
		report.Profiles[2].ShownFiles != 25 ||
		report.Profiles[2].ShownMatches != 350 ||
		report.Profiles[2].MatchRetentionPct != 40 ||
		report.Profiles[2].SavedBytesVsDefault <= report.Profiles[1].SavedBytesVsDefault {
		t.Fatalf("bad second candidate row: %+v", report.Profiles[2])
	}
	if strings.Contains(stdout.String(), "fatal timeout rejected") {
		t.Fatalf("candidate profile report must stay content-free, got raw match text:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runSearchCapProfile([]string{
		"--command", "rg -n function",
		"--input", path,
		"--candidate", "25:15",
		"--min-candidate-retained-pct", "40",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("candidate text code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "selected candidate: candidate_25x15") ||
		strings.Contains(stdout.String(), "fatal timeout rejected") {
		t.Fatalf("unexpected candidate text report:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runSearchCapProfile([]string{
		"--command", "rg -n function",
		"--input", path,
		"--candidate", "25:15",
		"--require-aggressive-savings",
		"--min-candidate-retained-pct", "100",
		"--json",
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("over-retained candidate code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "expected candidate_25x15 profile to save more bytes than default") {
		t.Fatalf("missing high-retention savings failure:\n%s", stdout.String())
	}
}

func TestRunSearchCapProfileRejectsBadCandidates(t *testing.T) {
	t.Parallel()

	path := writeSearchCapProfileFixture(t)
	for _, args := range [][]string{
		{"--command", "rg -n function", "--input", path, "--candidate", "25"},
		{"--command", "rg -n function", "--input", path, "--candidate", "25:x"},
		{"--command", "rg -n function", "--input", path, "--candidate", "0:10"},
		{"--command", "rg -n function", "--input", path, "--candidate", "25:0"},
		{"--command", "rg -n function", "--input", path, "--aggressive-files", "0"},
		{"--command", "rg -n function", "--input", path, "--aggressive-matches", "-1"},
		{"--command", "rg -n function", "--input", path, "--min-aggressive-retained-pct", "-0.1"},
		{"--command", "rg -n function", "--input", path, "--min-aggressive-retained-pct", "100.1"},
		{"--command", "rg -n function", "--input", path, "--min-candidate-retained-pct=-1"},
		{"--command", "rg -n function", "--input", path, "--min-candidate-retained-pct=101"},
	} {
		var stdout, stderr bytes.Buffer
		code := runSearchCapProfile(args, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("candidate args %v code=%d want 2 stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestRunSearchCapProfileGateFailures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(path, []byte("not search output\nstill not search output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runSearchCapProfile([]string{
		"--command", "rg -n function",
		"--input", path,
		"--require-applicable",
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("gate code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "expected compactable search output") {
		t.Fatalf("missing gate failure:\n%s", stdout.String())
	}
}

func TestRunSearchCapProfileFramesRequireResolvedSearchOutput(t *testing.T) {
	t.Parallel()

	outputPath := writeSearchCapProfileFixture(t)
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "unresolved.jsonl")
	writeSearchCapProfileFrameRecords(t, framesPath, []map[string]any{
		{
			"direction": "client_to_server",
			"payload": map[string]any{
				"input": []map[string]any{
					{
						"type":    "function_call_output",
						"call_id": "unknown-search",
						"output":  string(output),
					},
				},
			},
		},
	})

	var stdout, stderr bytes.Buffer
	code := runSearchCapProfile([]string{
		"--frames", framesPath,
		"--require-applicable",
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("gate code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "expected compactable search output") ||
		!strings.Contains(stdout.String(), "search outputs: 0") {
		t.Fatalf("missing unresolved-output gate failure:\n%s", stdout.String())
	}
}

func TestSearchCapProfileToolUseFromFunctionCallIncludesWorkdir(t *testing.T) {
	t.Parallel()

	item := map[string]json.RawMessage{
		"type":      json.RawMessage(`"function_call"`),
		"call_id":   json.RawMessage(`"search-1"`),
		"arguments": json.RawMessage(`"{\"cmd\":\"rg -n function src\",\"workdir\":\"/repo\"}"`),
	}
	toolUse := searchCapProfileToolUseFromFunctionCall(item)
	if toolUse.command != "rg -n function src" || toolUse.workdir != "/repo" {
		t.Fatalf("bad tool use extraction: %+v", toolUse)
	}
	if normalized := normalizedSearchCapCommand(toolUse.command, toolUse.workdir); normalized != "rg -n function /repo/src" {
		t.Fatalf("workdir was not applied to normalized search command: %q", normalized)
	}
}

func writeSearchCapProfileFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "search.txt")
	var sb strings.Builder
	for f := 0; f < 35; f++ {
		for m := 1; m <= 25; m++ {
			msg := "ordinary function body content here with enough length"
			if f == 17 && m == 13 {
				msg = "fatal timeout rejected request with enough payload"
			}
			fmt.Fprintf(&sb, "pkg/internal/module/sub/file_%02d.go:%d:%s\n", f, m, msg)
		}
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeSearchCapProfileFramesFixture(t *testing.T, output string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "frames.jsonl")
	writeSearchCapProfileFrameRecords(t, path, []map[string]any{
		{
			"direction": "server_to_client",
			"payload": map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"output": []map[string]any{
						{
							"type":      "function_call",
							"call_id":   "search-1",
							"name":      "exec_command",
							"arguments": map[string]any{"cmd": "rg -n function pkg"},
						},
					},
				},
			},
		},
		{
			"direction": "client_to_server",
			"payload": map[string]any{
				"input": []map[string]any{
					{
						"type":    "function_call_output",
						"call_id": "search-1",
						"output":  output,
					},
				},
			},
		},
	})
	return path
}

func writeSearchCapProfileFrameRecords(t *testing.T, path string, records []map[string]any) {
	t.Helper()
	var sb strings.Builder
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(data)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

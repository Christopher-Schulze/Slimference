package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/planner"
)

func TestPlanInspect_TextAndJSON(t *testing.T) {
	dir := t.TempDir()
	requestPath := filepath.Join(dir, "request.txt")
	if err := os.WriteFile(requestPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	text := captureStdoutPlan(t, func() {
		handlePlanInspect([]string{
			"--provider", "codex_chatgpt",
			"--model", "codex",
			"--route", "websocket_tunnel",
			"--input-tokens", "9000",
			"--output-tokens", "500",
			"--task-shape", "code_edit",
			"--class", "tool_output",
			"--disable", "l0,4,ws",
			"--recent-edit",
			"--provider-cache",
			"--previous-response",
			"--output-cooldown",
			"--negative-savings",
			"--ws-known",
			"--ws-mutate",
			"--confidence", "high",
			requestPath,
		})
	})
	for _, want := range []string{"Slimference plan inspect", "Provider:       codex_chatgpt", "websocket", "operator_disabled"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text output missing %q: %s", want, text)
		}
	}

	jsonOut := captureStdoutPlan(t, func() {
		handlePlanInspect([]string{"--json", "--provider", "openai", "--input-tokens", "1000"})
	})
	var plan planner.CompressionPlan
	if err := json.Unmarshal([]byte(jsonOut), &plan); err != nil {
		t.Fatalf("json output invalid: %v %s", err, jsonOut)
	}
	if plan.Provider != "openai" || len(plan.Decisions) == 0 {
		t.Fatalf("bad json plan: %+v", plan)
	}
}

func TestHandlePlanCmdDispatch(t *testing.T) {
	code, exited := captureExit(func() { handlePlanCmd(nil) })
	if !exited || code != 1 {
		t.Fatalf("nil plan cmd code=%d exited=%v", code, exited)
	}
	code, exited = captureExit(func() { handlePlanCmd([]string{"bad"}) })
	if !exited || code != 1 {
		t.Fatalf("bad plan cmd code=%d exited=%v", code, exited)
	}
	out := captureStdoutPlan(t, func() {
		handlePlanCmd([]string{"inspect", "--input-tokens", "250"})
	})
	if !strings.Contains(out, "Slimference plan inspect") {
		t.Fatalf("inspect dispatch output: %s", out)
	}
	out = captureStdoutPlan(t, func() {
		handleSubcommand([]string{"plan", "inspect", "--input-tokens", "250"})
	})
	if !strings.Contains(out, "Slimference plan inspect") {
		t.Fatalf("subcommand plan output: %s", out)
	}
}

func TestPlanInspectStdinAndFileError(t *testing.T) {
	orig := readStdinAll
	readStdinAll = func() ([]byte, error) { return []byte("function x() {}"), nil }
	out := captureStdoutPlan(t, func() {
		handlePlanInspect([]string{"-", "--json"})
	})
	readStdinAll = orig
	if !strings.Contains(out, `"provider": "openai"`) {
		t.Fatalf("stdin json output: %s", out)
	}

	code, exited := captureExit(func() {
		handlePlanInspect([]string{filepath.Join(t.TempDir(), "missing.json")})
	})
	if !exited || code != 1 {
		t.Fatalf("file error code=%d exited=%v", code, exited)
	}
}

func TestPlanInspectParseAndBuildErrors(t *testing.T) {
	errorArgs := [][]string{
		{"--provider"},
		{"--model"},
		{"--route"},
		{"--input-tokens"},
		{"--input-tokens", "nope"},
		{"--output-tokens"},
		{"--output-tokens", "nope"},
		{"--task-shape"},
		{"--class"},
		{"--disable"},
		{"--confidence"},
		{"--unknown"},
		{"a", "b"},
	}
	for _, args := range errorArgs {
		if _, err := parsePlanInspectArgs(args); err == nil {
			t.Fatalf("expected parse error for %v", args)
		}
	}
	code, exited := captureExit(func() {
		handlePlanInspect([]string{"--provider"})
	})
	if !exited || code != 1 {
		t.Fatalf("parse error code=%d exited=%v", code, exited)
	}
	code, exited = captureExit(func() {
		handlePlanInspect([]string{"--disable", "bad"})
	})
	if !exited || code != 1 {
		t.Fatalf("build error code=%d exited=%v", code, exited)
	}
}

func TestPlanInspectHelpers(t *testing.T) {
	value, next, err := parsePlanIntFlag([]string{"--x", "12"}, 0, "--x")
	if err != nil || value != 12 || next != 1 {
		t.Fatalf("parse int value=%d next=%d err=%v", value, next, err)
	}
	if n, err := planInputTokenEstimate(""); err != nil || n != 0 {
		t.Fatalf("empty input estimate n=%d err=%v", n, err)
	}
	layers, err := parsePlanDisabledLayers([]string{"0", "1", "3", "l4", "output", "output-reduce", "websocket", "ws", ""})
	if err != nil {
		t.Fatal(err)
	}
	for _, layer := range []planner.Layer{planner.Layer0, planner.Layer1, planner.Layer3, planner.Layer4, planner.LayerWebSocket} {
		if !layers[layer] {
			t.Fatalf("missing disabled layer %s in %+v", layer, layers)
		}
	}
	if _, err := buildInspectablePlan(planInspectFlags{disabledLayers: []string{"bad"}}); err == nil {
		t.Fatal("expected build error for bad layer")
	}
}

func TestHelpIncludesPlan(t *testing.T) {
	if out := helpForSubcommand("plan"); !strings.Contains(out, "slimference plan inspect") {
		t.Fatalf("plan help: %s", out)
	}
}

func captureStdoutPlan(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	rp, wp, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wp
	fn()
	_ = wp.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	_ = rp.Close()
	return buf.String()
}

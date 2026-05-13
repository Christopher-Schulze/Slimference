package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestHandleCompletionCmd_bashPrintsScript prints the bash script to stdout.
func TestHandleCompletionCmd_bashPrintsScript(t *testing.T) {
	origOut := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	handleCompletionCmd([]string{"bash"})
	_ = wp.Close()
	os.Stdout = origOut

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	out := buf.String()
	for _, need := range []string{
		"_slimference()",
		"complete -F _slimference slimference",
		"config test doctor stats gain",
		"service proxy integrate",
		"install remove verify status check-upstream",
		"claude codex",
		"install enable disable status uninstall env",
		"--direct --proxied --host= --port= --",
		"prompt-cache",
		"paths last summary tail replay",
		"--path --stream=stdout",
	} {
		if !strings.Contains(out, need) {
			t.Fatalf("missing expected token %q in emitted script", need)
		}
	}
}

// TestHandleCompletionCmd_noArgs exits non-zero when no shell is specified.
func TestHandleCompletionCmd_noArgs(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleCompletionCmd(nil) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "completion bash") {
		t.Fatalf("no-args must exit 1 with usage hint, got code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

// TestHandleCompletionCmd_unknownShell rejects non-bash requests.
func TestHandleCompletionCmd_unknownShell(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleCompletionCmd([]string{"zsh"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "unsupported shell") {
		t.Fatalf("expected zsh to be rejected, got code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

// TestHandleSubcommand_completionDispatch covers the `completion` case in the
// top-level dispatcher.
func TestHandleSubcommand_completionDispatch(t *testing.T) {
	origOut := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	handleSubcommand([]string{"completion", "bash"})
	_ = wp.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !strings.Contains(buf.String(), "complete -F _slimference slimference") {
		t.Fatalf("completion dispatch did not emit the script: %q", buf.String())
	}
}

// TestBashCompletionScript_sourceable runs the emitted script through bash
// to prove it has no syntax errors.
func TestBashCompletionScript_sourceable(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available on this system")
	}
	cmd := exec.Command(bash, "-n", "-c", bashCompletionScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n refused the script: %v\n%s", err, out)
	}
}

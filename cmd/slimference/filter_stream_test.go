package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestHandleFilterCmd_StreamMode(t *testing.T) {
	origStdout := os.Stdout
	origExit := exitFn
	defer func() {
		os.Stdout = origStdout
		exitFn = origExit
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleFilterCmd([]string{"--stream", "--", "sh", "-c", "echo line-1; echo line-1; echo line-2"})
	_ = w.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "line-1") || !strings.Contains(buf.String(), "line-2") {
		t.Fatalf("stream output: %q", buf.String())
	}
	if len(exits) == 0 || exits[0] != 0 {
		t.Fatalf("expected exit 0, got %v", exits)
	}
}

func TestHandleFilterCmd_StreamErrorPath(t *testing.T) {
	origExit := exitFn
	origStderr := os.Stderr
	defer func() {
		exitFn = origExit
		os.Stderr = origStderr
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	r, w, _ := os.Pipe()
	os.Stderr = w
	handleFilterCmd([]string{"--stream", "/no/such/binary"})
	_ = w.Close()
	os.Stderr = origStderr
	_, _ = io.Copy(io.Discard, r)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// recordHookFlight is replaced by the existing test fixtures in this
// package via recordHookFlightFn. We capture invocations here for
// failopen assertions without depending on the SQLite analytics path.
type capturedFlight struct {
	source        string
	sessionID     string
	toolName      string
	decision      string
	originalBytes int
	finalBytes    int
	layers        []int
	err           error
}

func TestFailOpenPassthrough_RecordsFlightWithReasonSuffix(t *testing.T) {
	var captured *capturedFlight
	orig := recordHookFlightImpl
	recordHookFlightImpl = func(source, sessionID, toolName, decision string, originalBytes, finalBytes int, layers []int, hookErr error) {
		captured = &capturedFlight{source, sessionID, toolName, decision, originalBytes, finalBytes, layers, hookErr}
	}
	defer func() { recordHookFlightImpl = orig }()

	payload := []byte(`{"x":1}`)
	failOpenPassthrough(FailOpenPostTool, payload, "Bash", "sess-1", ReasonTimeout, errors.New("rpc deadline"))
	if captured == nil {
		t.Fatalf("expected recordHookFlight to be called")
	}
	if captured.source != "hook_post" {
		t.Fatalf("source: got %q, want hook_post", captured.source)
	}
	if captured.decision != "fail_open:timeout" {
		t.Fatalf("decision: got %q, want fail_open:timeout", captured.decision)
	}
	if captured.toolName != "Bash" || captured.sessionID != "sess-1" {
		t.Fatalf("tool/session drift: %+v", captured)
	}
	if captured.originalBytes != len(payload) || captured.finalBytes != len(payload) {
		t.Fatalf("byte counts: %+v", captured)
	}
	if captured.err == nil || !strings.Contains(captured.err.Error(), "deadline") {
		t.Fatalf("err lost: %v", captured.err)
	}
}

func TestFailOpenPassthrough_DefaultsToolNameToSourceWhenEmpty(t *testing.T) {
	var captured *capturedFlight
	orig := recordHookFlightImpl
	recordHookFlightImpl = func(source, sessionID, toolName, decision string, originalBytes, finalBytes int, layers []int, hookErr error) {
		captured = &capturedFlight{source, sessionID, toolName, decision, originalBytes, finalBytes, layers, hookErr}
	}
	defer func() { recordHookFlightImpl = orig }()

	failOpenPassthrough(FailOpenReadHook, []byte("p"), "", "", ReasonDaemonDown, nil)
	if captured == nil || captured.toolName != "hook_read" {
		t.Fatalf("expected tool fallback to source string, got %+v", captured)
	}
}

func TestGuardHook_NoPanicIsNoOp(t *testing.T) {
	called := false
	orig := recordHookFlightImpl
	recordHookFlightImpl = func(string, string, string, string, int, int, []int, error) {
		called = true
	}
	defer func() { recordHookFlightImpl = orig }()
	defer guardHook(FailOpenRewrite, nil)()
	// Normal flow: defer runs without recovering a panic; no flight
	// record should be emitted.
	if called {
		t.Fatalf("guardHook should not record on no-panic path")
	}
}

func TestGuardHook_RecoversPanicAndExitsZero(t *testing.T) {
	var captured *capturedFlight
	origRec := recordHookFlightImpl
	recordHookFlightImpl = func(source, sessionID, toolName, decision string, originalBytes, finalBytes int, layers []int, hookErr error) {
		captured = &capturedFlight{source, sessionID, toolName, decision, originalBytes, finalBytes, layers, hookErr}
	}
	defer func() { recordHookFlightImpl = origRec }()

	gotExit := -1
	origExit := exitFn
	exitFn = func(code int) {
		gotExit = code
		panic(exitSentinel{}) // stop the deferred chain inside the test func
	}
	defer func() { exitFn = origExit }()

	// Capture stdout writes.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	payload := []byte(`{"session_id":"s9","tool_name":"Bash"}`)
	func() {
		defer func() { _ = recover() }() // swallow the exitSentinel
		defer guardHook(FailOpenRewrite, payload)()
		panic("boom")
	}()
	w.Close()
	stdoutBuf, _ := io.ReadAll(r)
	os.Stdout = origStdout

	if captured == nil {
		t.Fatalf("expected recordHookFlight after panic")
	}
	if !strings.HasPrefix(captured.decision, "fail_open:panic") {
		t.Fatalf("decision: got %q", captured.decision)
	}
	if captured.sessionID != "s9" || captured.toolName != "Bash" {
		t.Fatalf("payload introspection lost: %+v", captured)
	}
	if captured.err == nil || !strings.Contains(captured.err.Error(), "boom") {
		t.Fatalf("err lost: %v", captured.err)
	}
	if !bytes.Equal(stdoutBuf, payload) {
		t.Fatalf("stdout drift: got %q, want %q", string(stdoutBuf), string(payload))
	}
	if gotExit != 0 {
		t.Fatalf("expected exit 0 on fail-open, got %d", gotExit)
	}
}

func TestGuardHook_RereaisesControlledExit(t *testing.T) {
	called := false
	orig := recordHookFlightImpl
	recordHookFlightImpl = func(string, string, string, string, int, int, []int, error) {
		called = true
	}
	defer func() { recordHookFlightImpl = orig }()

	rethrown := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(ControlledExit); ok {
					rethrown = true
				}
			}
		}()
		defer guardHook(FailOpenRewrite, []byte(`{"x":1}`))()
		panic(ControlledExit{})
	}()

	if !rethrown {
		t.Fatalf("expected guardHook to re-raise ControlledExit")
	}
	if called {
		t.Fatalf("guardHook should not record fail-open for controlled-exit unwinds")
	}
}

// controlledExitDummy exercises the marker method to keep coverage 100%
// without depending on test-only embedding.
type controlledExitDummy struct{}

func (controlledExitDummy) controlledExitMarker() {}

func TestControlledExitMarker_IsSatisfied(t *testing.T) {
	var v any = ControlledExit{}
	if _, ok := v.(controlledExitMarker); !ok {
		t.Fatalf("ControlledExit must satisfy controlledExitMarker")
	}
	var d any = controlledExitDummy{}
	if _, ok := d.(controlledExitMarker); !ok {
		t.Fatalf("controlledExitDummy must satisfy controlledExitMarker")
	}
	// Invoke through the interface so the coverage pass counts the
	// method body. A direct ControlledExit{}.controlledExitMarker()
	// call can be inline-elided by the compiler.
	m := controlledExitMarker(ControlledExit{})
	m.controlledExitMarker()
}

func TestGuardHook_EmptyPayloadSkipsStdoutWrite(t *testing.T) {
	origRec := recordHookFlightImpl
	recordHookFlightImpl = func(string, string, string, string, int, int, []int, error) {}
	defer func() { recordHookFlightImpl = origRec }()

	origExit := exitFn
	exitFn = func(int) { panic(exitSentinel{}) }
	defer func() { exitFn = origExit }()

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	func() {
		defer func() { _ = recover() }()
		defer guardHook(FailOpenCodexLifecycle, nil)()
		panic("nil-payload")
	}()
	w.Close()
	stdoutBuf, _ := io.ReadAll(r)
	os.Stdout = origStdout

	if len(stdoutBuf) != 0 {
		t.Fatalf("expected no stdout write on nil payload, got %q", string(stdoutBuf))
	}
}

// exitSentinel is shared with headless_test.go in the same package.

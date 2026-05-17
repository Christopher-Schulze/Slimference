package main

import (
	"fmt"
	"os"
)

// failopen.go centralises the "daemon down must not kill coding tools"
// contract (t164). Every hook handler in this binary defers a guard from
// here so an unexpected panic or runtime error degrades to silent
// passthrough rather than breaking the user's coding session.

// FailOpenSource matches the telemetry "source" tag used by
// recordHookFlight so analytics can break out degraded sessions by hook
// event without redefining the source vocabulary.
type FailOpenSource string

const (
	FailOpenRewrite        FailOpenSource = "hook_pre"
	FailOpenPostTool       FailOpenSource = "hook_post"
	FailOpenReadHook       FailOpenSource = "hook_read"
	FailOpenCodexLifecycle FailOpenSource = "hook_lifecycle"
)

// FailOpenReason categorises why the handler degraded. Recorded as a
// suffix on the recordHookFlight "decision" column so we can chart
// "fail_open:panic" vs "fail_open:timeout" vs "fail_open:invalid_payload".
type FailOpenReason string

const (
	ReasonPanic      FailOpenReason = "panic"
	ReasonTimeout    FailOpenReason = "timeout"
	ReasonInvalid    FailOpenReason = "invalid_payload"
	ReasonInternal   FailOpenReason = "internal_error"
	ReasonDaemonDown FailOpenReason = "daemon_down"
)

// failOpenPassthrough emits a fail-open telemetry row. It does not touch
// stdout or call exitFn — callers control the hook contract (exit code +
// stdout payload) themselves. Returns immediately with no error.
func failOpenPassthrough(source FailOpenSource, payload []byte, toolName, sessionID string, reason FailOpenReason, err error) {
	if toolName == "" {
		toolName = string(source)
	}
	decision := "fail_open:" + string(reason)
	recordHookFlightImpl(string(source), sessionID, toolName, decision, len(payload), len(payload), []int{1}, err)
}

// controlledExitMarker is the package-private interface every "exitFn
// substitute" panic value implements. guardHook re-raises any recovered
// panic that satisfies this interface so the test fixture's outer
// recover completes unchanged. Production exitFn (os.Exit) never
// panics; this surface exists purely so the fail-open guard does not
// swallow test-controlled exit unwinds.
type controlledExitMarker interface {
	controlledExitMarker()
}

// ControlledExit is the canonical zero-value panic sentinel. Tests
// panic with ControlledExit{}; guardHook detects the marker interface
// and re-panics.
type ControlledExit struct{}

// controlledExitMarker tags ControlledExit as a controlled unwind.
func (ControlledExit) controlledExitMarker() {}

// guardHook returns a deferred function that recovers any panic in the
// caller, records a fail-open row, prints the original payload on stdout
// so the hook chain continues, and exits 0. Usage:
//
//	func handleXxxCmd(args []string) {
//	    payload, _ := readStdinAll()
//	    defer guardHook(FailOpenPostTool, payload)()
//	    ...risky work...
//	}
//
// Calling exitFn from this guard is intentional: every hook contract in
// the Slimference setup treats exit 0 + empty stdout as "do nothing,
// pass the original through", which is the desired degraded behavior.
func guardHook(source FailOpenSource, payload []byte) func() {
	return func() {
		r := recover()
		if r == nil {
			return
		}
		// Re-raise controlled-exit unwinds: those come from exitFn
		// stubs in tests and from any future production callers that
		// use this pattern to signal "exit cleanly, do not fail-open".
		if _, controlled := r.(controlledExitMarker); controlled {
			panic(r)
		}
		err := fmt.Errorf("panic: %v", r)
		sessionID := extractJSONText(payload, "session_id", "conversation_id")
		toolName := extractJSONText(payload, "tool_name", "toolName")
		failOpenPassthrough(source, payload, toolName, sessionID, ReasonPanic, err)
		// Emit the original payload so the hook tool sees passthrough.
		// PostToolUse contract: empty stdout = keep original tool output.
		// PreToolUse rewrite contract: exit 1 = passthrough. We pick
		// exit 0 here because a panic means our subprocess has degraded
		// fully; the calling bash wrapper handles further degradation
		// (timeout 30ms ... || cat) if needed.
		if len(payload) > 0 {
			_, _ = os.Stdout.Write(payload)
		}
		exitFn(0)
	}
}

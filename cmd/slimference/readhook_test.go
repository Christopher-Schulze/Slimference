package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleReadHookCmd(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origHome := osUserHomeDir
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		osUserHomeDir = origHome
	}()

	t.Run("usage_when_terminal", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return true }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handleReadHookCmd(nil) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 1 || !strings.Contains(buf.String(), "usage: slimference readhook codex") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
	})

	t.Run("allow_emits_nothing", func(t *testing.T) {
		home := t.TempDir()
		file := filepath.Join(home, "main.go")
		if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		termIsTerminalFn = func(int) bool { return false }
		osUserHomeDir = func() (string, error) { return home, nil }
		readStdinAll = func() ([]byte, error) {
			return []byte(`{"session_id":"s1","tool_input":{"file_path":"` + file + `"}}`), nil
		}

		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleReadHookCmd([]string{"codex"})
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if buf.Len() != 0 {
			t.Fatalf("expected no stdout, got %q", buf.String())
		}
	})

	t.Run("claude_arg_rejected", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		rp, cleanup := redirectStderr()
		code, exited := captureExit(func() { handleReadHookCmd([]string{"claude"}) })
		cleanup()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rp)
		if !exited || code != 2 || !strings.Contains(buf.String(), "Claude Code is parked") {
			t.Fatalf("exited=%v code=%d stderr=%q", exited, code, buf.String())
		}
	})

	t.Run("blocked_read_emits_codex_json", func(t *testing.T) {
		home := t.TempDir()
		file := filepath.Join(home, "main.go")
		if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		termIsTerminalFn = func(int) bool { return false }
		osUserHomeDir = func() (string, error) { return home, nil }
		payload := []byte(`{"session_id":"s1","tool_input":{"file_path":"` + file + `"}}`)
		readStdinAll = func() ([]byte, error) { return payload, nil }
		handleReadHookCmd([]string{"codex"})
		readStdinAll = func() ([]byte, error) { return payload, nil }

		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleReadHookCmd([]string{"codex"})
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, `"reason":"`) {
			t.Fatalf("unexpected stdout: %q", out)
		}
	})

	t.Run("invalid_arg_exits", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		code, exited := captureExit(func() { handleReadHookCmd([]string{"bad"}) })
		if !exited || code != 1 {
			t.Fatalf("exited=%v code=%d", exited, code)
		}
	})

	t.Run("read_stdin_error_exits", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return nil, os.ErrInvalid }
		code, exited := captureExit(func() { handleReadHookCmd([]string{"codex"}) })
		if !exited || code != 1 {
			t.Fatalf("exited=%v code=%d", exited, code)
		}
	})

	t.Run("bad_payload_exits", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return []byte(`{`), nil }
		code, exited := captureExit(func() { handleReadHookCmd([]string{"codex"}) })
		if !exited || code != 1 {
			t.Fatalf("exited=%v code=%d", exited, code)
		}
	})

	t.Run("home_error_exits", func(t *testing.T) {
		termIsTerminalFn = func(int) bool { return false }
		readStdinAll = func() ([]byte, error) { return []byte(`{"session_id":"s1","tool_input":{"file_path":"main.go"}}`), nil }
		osUserHomeDir = func() (string, error) { return "", os.ErrInvalid }
		code, exited := captureExit(func() { handleReadHookCmd([]string{"codex"}) })
		if !exited || code != 1 {
			t.Fatalf("exited=%v code=%d", exited, code)
		}
	})
}

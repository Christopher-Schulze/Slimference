package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRootArmScriptCodexOnlyIPv4(t *testing.T) {
	script := buildRootArmScript("/tmp/root.crt")
	if !strings.Contains(script, "127.0.0.1 chatgpt.com api.openai.com") {
		t.Fatalf("root-arm script missing Codex hosts:\n%s", script)
	}
	for _, bad := range []string{"api.anthropic.com", "::1 chatgpt.com", "::1 api.openai.com"} {
		if strings.Contains(script, bad) {
			t.Fatalf("root-arm script must not contain %q:\n%s", bad, script)
		}
	}
	if !strings.Contains(script, "dscacheutil -flushcache") ||
		!strings.Contains(script, "killall -HUP mDNSResponder") {
		t.Fatalf("root-arm script must flush macOS resolver caches:\n%s", script)
	}
}

func TestBuildRootDisarmScriptStripsMarkerAndFlushesAnchor(t *testing.T) {
	script := buildRootDisarmScript()
	for _, want := range []string{
		"/# slimference:start/",
		"/# slimference:end/",
		"pfctl -a slimference -F all",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("root-disarm script missing %q:\n%s", want, script)
		}
	}
}

func captureRootCommandOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldOut := os.Stdout
	oldErr := os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW
	defer func() {
		os.Stdout = oldOut
		os.Stderr = oldErr
	}()
	fn()
	_ = outW.Close()
	_ = errW.Close()
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, outR)
	_, _ = io.Copy(&errBuf, errR)
	return outBuf.String(), errBuf.String()
}

func withTempHomeForRootCommand(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	prevHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prevHome })
	return home
}

func TestHandleRootArmHelpDoesNotExit(t *testing.T) {
	out, _ := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleRootArmCmd([]string{"--help"}) })
		if exited {
			t.Fatalf("unexpected exit %d", code)
		}
	})
	if !strings.Contains(out, "slimference root-arm") {
		t.Fatalf("help output missing command name: %q", out)
	}
}

func TestHandleRootArmMissingHomeExits1(t *testing.T) {
	prevHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = prevHome })

	_, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleRootArmCmd(nil) })
		if !exited || code != 1 {
			t.Fatalf("exit=(%d,%v), want (1,true)", code, exited)
		}
	})
	if !strings.Contains(stderr, "HOME unresolved") {
		t.Fatalf("stderr missing HOME error: %q", stderr)
	}
}

func TestHandleRootArmMissingCertExits1(t *testing.T) {
	home := withTempHomeForRootCommand(t)
	_, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleRootArmCmd(nil) })
		if !exited || code != 1 {
			t.Fatalf("exit=(%d,%v), want (1,true)", code, exited)
		}
	})
	if !strings.Contains(stderr, filepath.Join(home, ".slimference", "ca", "root.crt")) {
		t.Fatalf("stderr missing cert path: %q", stderr)
	}
}

func TestHandleRootArmRunsAdminScriptThroughInjectedRunner(t *testing.T) {
	home := withTempHomeForRootCommand(t)
	cert := filepath.Join(home, ".slimference", "ca", "root.crt")
	if err := os.MkdirAll(filepath.Dir(cert), 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	if err := os.WriteFile(cert, []byte("cert"), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	prevRun := runWithAdminPrivilegesFn
	var gotScript, gotPrompt string
	runWithAdminPrivilegesFn = func(script, prompt string) error {
		gotScript = script
		gotPrompt = prompt
		return nil
	}
	t.Cleanup(func() { runWithAdminPrivilegesFn = prevRun })

	out, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleRootArmCmd(nil) })
		if exited {
			t.Fatalf("unexpected exit %d", code)
		}
	})
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if !strings.Contains(out, "Hosts + pfctl armed") {
		t.Fatalf("success output missing arm confirmation: %q", out)
	}
	if !strings.Contains(gotPrompt, "arm transparent MITM") {
		t.Fatalf("prompt=%q", gotPrompt)
	}
	if !strings.Contains(gotScript, "127.0.0.1 chatgpt.com api.openai.com") ||
		strings.Contains(gotScript, "api.anthropic.com") {
		t.Fatalf("root-arm script not Codex-only:\n%s", gotScript)
	}
}

func TestHandleRootArmAdminFailureExits1(t *testing.T) {
	home := withTempHomeForRootCommand(t)
	cert := filepath.Join(home, ".slimference", "ca", "root.crt")
	if err := os.MkdirAll(filepath.Dir(cert), 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	if err := os.WriteFile(cert, []byte("cert"), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	prevRun := runWithAdminPrivilegesFn
	runWithAdminPrivilegesFn = func(_, _ string) error { return errors.New("denied") }
	t.Cleanup(func() { runWithAdminPrivilegesFn = prevRun })

	_, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleRootArmCmd(nil) })
		if !exited || code != 1 {
			t.Fatalf("exit=(%d,%v), want (1,true)", code, exited)
		}
	})
	if !strings.Contains(stderr, "denied") {
		t.Fatalf("stderr missing admin failure: %q", stderr)
	}
}

func TestHandleRootDisarmHelpDoesNotExit(t *testing.T) {
	out, _ := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleRootDisarmCmd([]string{"-h"}) })
		if exited {
			t.Fatalf("unexpected exit %d", code)
		}
	})
	if !strings.Contains(out, "slimference root-disarm") {
		t.Fatalf("help output missing command name: %q", out)
	}
}

func TestHandleRootDisarmRunsAdminScriptThroughInjectedRunner(t *testing.T) {
	withTempHomeForRootCommand(t)
	prevRun := runWithAdminPrivilegesFn
	var gotScript, gotPrompt string
	runWithAdminPrivilegesFn = func(script, prompt string) error {
		gotScript = script
		gotPrompt = prompt
		return nil
	}
	t.Cleanup(func() { runWithAdminPrivilegesFn = prevRun })

	out, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleRootDisarmCmd(nil) })
		if exited {
			t.Fatalf("unexpected exit %d", code)
		}
	})
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if !strings.Contains(out, "Codex talks direct again") {
		t.Fatalf("success output missing disarm confirmation: %q", out)
	}
	if !strings.Contains(gotPrompt, "disarm transparent MITM") {
		t.Fatalf("prompt=%q", gotPrompt)
	}
	if !strings.Contains(gotScript, "pfctl -a slimference -F all") {
		t.Fatalf("disarm script missing pfctl flush:\n%s", gotScript)
	}
}

func TestHandleRootDisarmMissingHomeExits1(t *testing.T) {
	prevHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = prevHome })

	_, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleRootDisarmCmd(nil) })
		if !exited || code != 1 {
			t.Fatalf("exit=(%d,%v), want (1,true)", code, exited)
		}
	})
	if !strings.Contains(stderr, "HOME unresolved") {
		t.Fatalf("stderr missing HOME error: %q", stderr)
	}
}

func TestHandleRootDisarmAdminFailureExits1(t *testing.T) {
	withTempHomeForRootCommand(t)
	prevRun := runWithAdminPrivilegesFn
	runWithAdminPrivilegesFn = func(_, _ string) error { return errors.New("denied") }
	t.Cleanup(func() { runWithAdminPrivilegesFn = prevRun })

	_, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleRootDisarmCmd(nil) })
		if !exited || code != 1 {
			t.Fatalf("exit=(%d,%v), want (1,true)", code, exited)
		}
	})
	if !strings.Contains(stderr, "denied") {
		t.Fatalf("stderr missing admin failure: %q", stderr)
	}
}

func TestRunWithAdminPrivilegesTempFileErrors(t *testing.T) {
	prevCreate := createRootScriptTempFn
	prevRemove := removeRootScriptFn
	prevExec := execOsascriptFn
	t.Cleanup(func() {
		createRootScriptTempFn = prevCreate
		removeRootScriptFn = prevRemove
		execOsascriptFn = prevExec
	})
	removeRootScriptFn = func(string) error { return nil }
	execOsascriptFn = func(string) ([]byte, error) { return nil, nil }

	createRootScriptTempFn = func(string, string) (atomicTempFile, error) {
		return nil, errors.New("no tmp")
	}
	if err := runWithAdminPrivileges("echo ok", "prompt"); err == nil || !strings.Contains(err.Error(), "temp file") {
		t.Fatalf("create error=%v, want temp file error", err)
	}

	for _, tc := range []struct {
		name string
		file *fakeAtomicTempFile
		want string
	}{
		{name: "write", file: &fakeAtomicTempFile{name: "tmp", writeErr: errors.New("disk full")}, want: "write script"},
		{name: "chmod", file: &fakeAtomicTempFile{name: "tmp", chmodErr: errors.New("no chmod")}, want: "chmod"},
		{name: "close", file: &fakeAtomicTempFile{name: "tmp", closeErr: errors.New("close fail")}, want: "close"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			createRootScriptTempFn = func(string, string) (atomicTempFile, error) {
				return tc.file, nil
			}
			err := runWithAdminPrivileges("echo ok", "prompt")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunWithAdminPrivilegesOsascriptBranches(t *testing.T) {
	prevCreate := createRootScriptTempFn
	prevRemove := removeRootScriptFn
	prevExec := execOsascriptFn
	t.Cleanup(func() {
		createRootScriptTempFn = prevCreate
		removeRootScriptFn = prevRemove
		execOsascriptFn = prevExec
	})
	createRootScriptTempFn = func(string, string) (atomicTempFile, error) {
		return &fakeAtomicTempFile{name: "/tmp/slim-root-test.sh"}, nil
	}
	removeRootScriptFn = func(string) error { return nil }

	execOsascriptFn = func(script string) ([]byte, error) {
		if !strings.Contains(script, "/bin/sh /tmp/slim-root-test.sh") ||
			!strings.Contains(script, "with administrator privileges") {
			t.Fatalf("bad AppleScript: %s", script)
		}
		return []byte("execution error: User cancelled. (-128)"), errors.New("exit 1")
	}
	if err := runWithAdminPrivileges("echo ok", "prompt"); err == nil || !strings.Contains(err.Error(), "user cancelled") {
		t.Fatalf("cancel err=%v", err)
	}

	execOsascriptFn = func(string) ([]byte, error) {
		return []byte("boom"), errors.New("exit 1")
	}
	if err := runWithAdminPrivileges("echo ok", "prompt"); err == nil || !strings.Contains(err.Error(), "osascript") {
		t.Fatalf("osascript err=%v", err)
	}

	execOsascriptFn = func(string) ([]byte, error) {
		return []byte("root output\n"), nil
	}
	_, stderr := captureRootCommandOutput(t, func() {
		if err := runWithAdminPrivileges("echo ok", "prompt"); err != nil {
			t.Fatalf("success err=%v", err)
		}
	})
	if !strings.Contains(stderr, "root output") {
		t.Fatalf("stderr missing script output: %q", stderr)
	}
}

func TestRunWithAdminPrivilegesUsesTempScriptAndReportsOutput(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "osascript")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho fake-admin-output\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake osascript: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, stderr := captureRootCommandOutput(t, func() {
		if err := runWithAdminPrivileges("echo ok", "prompt"); err != nil {
			t.Fatalf("runWithAdminPrivileges: %v", err)
		}
	})
	if !strings.Contains(stderr, "fake-admin-output") {
		t.Fatalf("stderr missing script output: %q", stderr)
	}
}

func TestRunWithAdminPrivilegesMapsUserCancelled(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "osascript")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'execution error: User cancelled. (-128)'\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake osascript: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runWithAdminPrivileges("echo ok", "prompt")
	if err == nil || !strings.Contains(err.Error(), "user cancelled") {
		t.Fatalf("err=%v want user cancelled", err)
	}
}

func TestRunWithAdminPrivilegesReportsOsascriptFailure(t *testing.T) {
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "osascript")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho boom\nexit 2\n"), 0o755); err != nil {
		t.Fatalf("write fake osascript: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runWithAdminPrivileges("echo ok", "prompt")
	if err == nil || !strings.Contains(err.Error(), "osascript") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v want osascript boom", err)
	}
}

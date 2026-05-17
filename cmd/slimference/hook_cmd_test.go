package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHandleSubcommand_hook_status(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "status"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if out == "" {
		t.Fatal("expected hook status lines")
	}
}

func TestHandleSubcommand_hook_installRemove_codexOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "install", "codex"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Installed Codex hooks") {
		t.Fatalf("install codex: %q", buf.String())
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"hook", "remove", "codex"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	if !strings.Contains(buf.String(), "Removed Slimference hooks from Codex") {
		t.Fatalf("remove codex: %q", buf.String())
	}

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleSubcommand([]string{"hook", "install", "claude"}) })
	cleanup()
	var stderr bytes.Buffer
	_, _ = io.Copy(&stderr, rp)
	if !exited || code != 2 || !strings.Contains(stderr.String(), "Claude Code hooks are parked") {
		t.Fatalf("install claude parked exit=(%d,%v) stderr=%q", code, exited, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("hook install claude must not create ~/.claude, stat err=%v", err)
	}
}

func TestHandleSubcommand_hook_verify_afterInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r0, w0, _ := os.Pipe()
	os.Stdout = w0
	handleSubcommand([]string{"hook", "install", "codex"})
	_ = w0.Close()
	os.Stdout = old
	_, _ = io.Copy(io.Discard, r0)

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "verify"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "sha256=") {
		t.Fatalf("verify stdout: %q", out)
	}
}

func TestHandleSubcommand_hookUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_USAGE") == "1" {
		handleSubcommand([]string{"hook"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_BAD") == "1" {
		handleSubcommand([]string{"hook", "not-a-subcommand"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookInstallUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_IN_USAGE") == "1" {
		handleSubcommand([]string{"hook", "install"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookInstallUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_IN_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookInstallUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_IN_BAD") == "1" {
		handleSubcommand([]string{"hook", "install", "emacs"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookInstallUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_IN_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookRemoveUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_RM_USAGE") == "1" {
		handleSubcommand([]string{"hook", "remove"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookRemoveUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_RM_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_hookRemoveUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_HOOK_RM_BAD") == "1" {
		handleSubcommand([]string{"hook", "remove", "vim"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_hookRemoveUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_HOOK_RM_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleHookCmd_verifyNotOkExits1 covers the verify !ok branch (main.go:420-422) - hooks not installed.
func TestHandleHookCmd_verifyNotOkExits1(t *testing.T) {
	if os.Getenv("TP_HOOK_VFY_FAIL") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_VFY_HOME"))
		handleSubcommand([]string{"hook", "verify"})
		return
	}
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_verifyNotOkExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_VFY_FAIL=1", "TP_HOOK_VFY_HOME="+home)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 (hooks missing), got err=%v", err)
	}
}

// TestHandleHookCmd_installClaude_parked ensures the public CLI never writes
// Claude Code hooks while Slimference is in Codex-only product mode.
func TestHandleHookCmd_installClaude_parked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleSubcommand([]string{"hook", "install", "claude"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 2 || !strings.Contains(buf.String(), "Claude Code hooks are parked") {
		t.Fatalf("exit=(%d,%v) stderr=%q", code, exited, buf.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("Claude hook install must not create ~/.claude, stat err=%v", err)
	}
}

// TestHandleHookCmd_installCodex_success covers hooks.InstallCodex success path (main.go:388).
func TestHandleHookCmd_installCodex_success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "install", "codex"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Installed Codex hooks") {
		t.Fatalf("expected codex install message, got: %q", buf.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "hooks.json")); err != nil {
		t.Fatalf("hooks.json missing after hook install: %v", err)
	}
	configData, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("config.toml missing after hook install: %v", err)
	}
	if !strings.Contains(string(configData), "hooks = true") || strings.Contains(string(configData), "openai_base_url") {
		t.Fatalf("hook install must enable hooks only, got config.toml: %s", configData)
	}
}

// TestHandleHookCmd_removeClaude_parked ensures the public CLI does not
// modify existing Claude Code files while parked.
func TestHandleHookCmd_removeClaude_parked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	if err := os.MkdirAll(filepath.Join(home, ".claude", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleSubcommand([]string{"hook", "remove", "claude"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 2 || !strings.Contains(buf.String(), "Claude Code hooks are parked") {
		t.Fatalf("exit=(%d,%v) stderr=%q", code, exited, buf.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh")); err != nil {
		t.Fatalf("parked remove must not delete Claude hook file: %v", err)
	}
}

// TestHandleHookCmd_removeCodex_success covers hooks.RemoveCodex success path (main.go:410).
func TestHandleHookCmd_removeCodex_success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "remove", "codex"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Removed Slimference hooks from Codex") {
		t.Fatalf("expected remove message, got: %q", buf.String())
	}
}

func TestHandleHookCmd_installCodexDoesNotValidateConfigPatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("openai_base_url = \"http://example.com\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "install", "codex"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Enabled hooks feature flag only") {
		t.Fatalf("expected feature-flag-only message, got %q", buf.String())
	}
	configData, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configData), "openai_base_url = \"http://example.com\"") ||
		!strings.Contains(string(configData), "hooks = true") {
		t.Fatalf("hook install changed config.toml: %s", configData)
	}
}

// TestHandleHookCmd_installClaude_parkedExits2 covers the parked Claude branch.
func TestHandleHookCmd_installClaude_parkedExits2(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on windows")
	}
	if os.Getenv("TP_HOOK_ICLAUDE_ERR") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_ICLAUDE_HOME"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"hook", "install", "claude"})
		return
	}

	tmp := t.TempDir()
	roHome := filepath.Join(tmp, "ro-home")
	if err := os.Mkdir(roHome, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roHome, 0o755) })
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_installClaude_parkedExits2")
	cmd.Env = append(os.Environ(), "TP_HOOK_ICLAUDE_ERR=1", "TP_HOOK_ICLAUDE_HOME="+roHome)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("want exit 2 from parked Claude install, got err=%v", err)
	}
}

// TestHandleHookCmd_installCodex_errorExits1 covers hooks.InstallCodex error path (main.go:384-387).
func TestHandleHookCmd_installCodex_errorExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on windows")
	}
	if os.Getenv("TP_HOOK_ICODEX_ERR") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_ICODEX_HOME"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"hook", "install", "codex"})
		return
	}
	tmp := t.TempDir()
	roHome := filepath.Join(tmp, "ro-home")
	if err := os.Mkdir(roHome, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roHome, 0o755) })
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_installCodex_errorExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_ICODEX_ERR=1", "TP_HOOK_ICODEX_HOME="+roHome)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 from InstallCodex error, got err=%v", err)
	}
}

// TestHandleHookCmd_removeClaude_parkedExits2 covers the parked Claude remove branch.
func TestHandleHookCmd_removeClaude_parkedExits2(t *testing.T) {
	if os.Getenv("TP_HOOK_RCLAUDE_ERR") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_RCLAUDE_HOME"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"hook", "remove", "claude"})
		return
	}
	home := t.TempDir()

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("not json {{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_removeClaude_parkedExits2")
	cmd.Env = append(os.Environ(), "TP_HOOK_RCLAUDE_ERR=1", "TP_HOOK_RCLAUDE_HOME="+home)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("want exit 2 from parked Claude remove, got err=%v", err)
	}
}

// TestHandleHookCmd_removeCodex_errorExits1 covers hooks.RemoveCodex error path (main.go:406-409).
func TestHandleHookCmd_removeCodex_ignoresAgentsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on windows")
	}
	if os.Getenv("TP_HOOK_RCODEX_ERR") == "1" {
		t.Setenv("HOME", os.Getenv("TP_HOOK_RCODEX_HOME"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"hook", "remove", "codex"})
		return
	}
	home := t.TempDir()

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(codexDir, "AGENTS.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_removeCodex_ignoresAgentsDirectory")
	cmd.Env = append(os.Environ(), "TP_HOOK_RCODEX_ERR=1", "TP_HOOK_RCODEX_HOME="+home)
	if err := cmd.Run(); err != nil {
		t.Fatalf("RemoveCodex must ignore AGENTS.md directory, got err=%v", err)
	}
}

func TestHandleHookCmd_removeCodex_errorExits(t *testing.T) {
	origHome := osUserHomeDir
	origRemove := removeCodexHookFn
	defer func() {
		osUserHomeDir = origHome
		removeCodexHookFn = origRemove
	}()
	osUserHomeDir = func() (string, error) { return t.TempDir(), nil }
	removeCodexHookFn = func(string) error { return errors.New("remove codex failed") }

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleHookCmd([]string{"remove", "codex"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "remove codex failed") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleHookCmd_homeDirError covers the os.UserHomeDir error exit in handleHookCmd (main.go:375-379).
func TestHandleHookCmd_homeDirError(t *testing.T) {
	orig := osUserHomeDir
	defer func() { osUserHomeDir = orig }()
	osUserHomeDir = func() (string, error) { return "", errors.New("no home dir") }

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"hook", "install", "claude"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "home") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleSubcommand_hook_verify_codex covers the verify codex branch (main.go:1002-1004).
func TestHandleSubcommand_hook_verify_codex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	// Pre-install codex hooks so verify returns ok=true and does not exit.
	handleSubcommand([]string{"hook", "install", "codex"})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "verify", "codex"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if out == "" {
		t.Fatal("expected hook verify codex lines")
	}
}

// TestHandleSubcommand_hook_status_codex covers the status codex branch (main.go:1013-1015).
func TestHandleSubcommand_hook_status_codex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "status", "codex"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if out == "" {
		t.Fatal("expected hook status codex lines")
	}
}

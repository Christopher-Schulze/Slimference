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

func TestHandleSubcommand_hook_installRemove_claude_and_codex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "install", "claude"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Installed Claude") {
		t.Fatalf("install claude: %q", buf.String())
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	handleSubcommand([]string{"hook", "install", "codex"})
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	if !strings.Contains(buf.String(), "Installed Codex hooks") {
		t.Fatalf("install codex: %q", buf.String())
	}

	r3, w3, _ := os.Pipe()
	os.Stdout = w3
	handleSubcommand([]string{"hook", "remove", "claude"})
	_ = w3.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r3)
	if !strings.Contains(buf.String(), "Removed Claude Code") {
		t.Fatalf("remove claude: %q", buf.String())
	}

	r4, w4, _ := os.Pipe()
	os.Stdout = w4
	handleSubcommand([]string{"hook", "remove", "codex"})
	_ = w4.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r4)
	if !strings.Contains(buf.String(), "Removed Slimference hooks from Codex") {
		t.Fatalf("remove codex: %q", buf.String())
	}
}

func TestHandleSubcommand_hook_verify_afterInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r0, w0, _ := os.Pipe()
	os.Stdout = w0
	handleSubcommand([]string{"hook", "install", "claude"})
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

// TestHandleHookCmd_installClaude_success covers hooks.InstallClaude success path (main.go:382).
// Uses a temp HOME so the install writes to a temp dir.
func TestHandleHookCmd_installClaude_success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "install", "claude"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Installed Claude") {
		t.Fatalf("expected install message, got: %q", buf.String())
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
	configData, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("read codex config: %v", err)
	}
	configText := string(configData)
	if !strings.Contains(configText, "openai_base_url") || !strings.Contains(configText, "chatgpt_base_url") {
		t.Fatalf("hook install codex should write complete codex config block: %s", configText)
	}
}

// TestHandleHookCmd_removeClaude_success covers hooks.RemoveClaude success path (main.go:404).
func TestHandleHookCmd_removeClaude_success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	if err := os.MkdirAll(filepath.Join(home, ".claude", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "hooks", "slimference-rewrite.sh"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"hook", "remove", "claude"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Removed Claude Code") {
		t.Fatalf("expected remove message, got: %q", buf.String())
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

func TestInstallCodexIntegrationHook_ConfigError(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(codexDir, "config.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := installCodexIntegrationHook(home, "slimference")
	if err == nil || !strings.Contains(err.Error(), "codex config") {
		t.Fatalf("expected codex config error, got %v", err)
	}
}

func TestRemoveCodexIntegrationHook_ConfigError(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(codexDir, "config.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := removeCodexIntegrationHook(home)
	if err == nil || !strings.Contains(err.Error(), "codex config remove") {
		t.Fatalf("expected codex config remove error, got %v", err)
	}
}

// TestHandleHookCmd_installClaude_errorExits1 covers hooks.InstallClaude error path (main.go:378-381).
// Makes HOME an unwritable dir so MkdirAll fails inside InstallClaude.
func TestHandleHookCmd_installClaude_errorExits1(t *testing.T) {
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
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_installClaude_errorExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_ICLAUDE_ERR=1", "TP_HOOK_ICLAUDE_HOME="+roHome)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 from InstallClaude error, got err=%v", err)
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

// TestHandleHookCmd_removeClaude_errorExits1 covers hooks.RemoveClaude error path (main.go:400-403).
// Makes settings.json contain invalid JSON so stripClaudePreToolUse fails.
func TestHandleHookCmd_removeClaude_errorExits1(t *testing.T) {
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
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_removeClaude_errorExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_RCLAUDE_ERR=1", "TP_HOOK_RCLAUDE_HOME="+home)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 from RemoveClaude error, got err=%v", err)
	}
}

// TestHandleHookCmd_removeCodex_errorExits1 covers hooks.RemoveCodex error path (main.go:406-409).
func TestHandleHookCmd_removeCodex_errorExits1(t *testing.T) {
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

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHookCmd_removeCodex_errorExits1")
	cmd.Env = append(os.Environ(), "TP_HOOK_RCODEX_ERR=1", "TP_HOOK_RCODEX_HOME="+home)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 from RemoveCodex error, got err=%v", err)
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

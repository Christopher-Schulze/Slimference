package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

func TestLayer0PermissionCheck(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIRM_SUDO", "")
	if code, msg := layer0PermissionCheck("echo ok"); code != 0 || msg != "" {
		t.Fatalf("allowed: got code=%d msg=%q", code, msg)
	}
	if code, msg := layer0PermissionCheck("rm -rf /"); code != 2 || msg == "" {
		t.Fatalf("deny: want code 2 and message, got %d %q", code, msg)
	}
	if code, msg := layer0PermissionCheck("sudo apt update"); code != 3 || msg == "" {
		t.Fatalf("ask: want code 3, got %d %q", code, msg)
	}
	t.Setenv("SLIMFERENCE_CONFIRM_SUDO", "1")
	if code, msg := layer0PermissionCheck("sudo apt update"); code != 0 || msg != "" {
		t.Fatalf("sudo allowed with SLIMFERENCE_CONFIRM_SUDO=1: got %d %q", code, msg)
	}
}

func TestResolveFilterDBPath_TeeDir_env(t *testing.T) {
	t.Setenv("SLIMFERENCE_FILTER_DB", "/tmp/slimference-filter-unit.db")
	p, err := resolveFilterDBPath()
	if err != nil || p != "/tmp/slimference-filter-unit.db" {
		t.Fatalf("filter db: err=%v p=%q", err, p)
	}
	t.Setenv("SLIMFERENCE_TEE_DIR", "/tmp/slimference-tee-unit")
	d, err := resolveTeeDir()
	if err != nil || d != "/tmp/slimference-tee-unit" {
		t.Fatalf("tee: err=%v d=%q", err, d)
	}
}

func TestResolveFilterDBPath_TeeDir_fromConfigFile(t *testing.T) {
	tmp := t.TempDir()
	filterPath := filepath.Join(tmp, "my-filter.db")
	teePath := filepath.Join(tmp, "my-tee")
	cfgPath := filepath.Join(tmp, "config.toml")
	content := fmt.Sprintf(`[filter]
filter_db = %q
tee_dir = %q
`, filterPath, teePath)
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	t.Setenv("SLIMFERENCE_FILTER_DB", "")
	t.Setenv("SLIMFERENCE_TEE_DIR", "")
	p, err := resolveFilterDBPath()
	if err != nil {
		t.Fatalf("resolveFilterDBPath: %v", err)
	}
	if p != filepath.Clean(filterPath) {
		t.Fatalf("filter db: got %q want %q", p, filepath.Clean(filterPath))
	}
	d, err := resolveTeeDir()
	if err != nil {
		t.Fatalf("resolveTeeDir: %v", err)
	}
	if d != filepath.Clean(teePath) {
		t.Fatalf("tee dir: got %q want %q", d, filepath.Clean(teePath))
	}
}

func TestHandleSubcommand_filter_echo_recordsRun(t *testing.T) {
	if os.Getenv("TP_FILTER_ECHO") == "1" {
		t.Setenv("SLIMFERENCE_FILTER_DB", os.Getenv("TP_FILTER_DB"))
		handleSubcommand([]string{"filter", "--", "echo", "hello"})
		return
	}
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "filter.db")
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_filter_echo_recordsRun")
	cmd.Env = append(os.Environ(), "TP_FILTER_ECHO=1", "TP_FILTER_DB="+dbPath)
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("err=%v out=%s", err, out)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM filter_runs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("filter_runs rows: %d", n)
	}
}

// TestHandleSubcommand_filter_nonZeroExit_teeRecovery covers tee recovery when the child exits non-zero.
func TestHandleSubcommand_filter_nonZeroExit_teeRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	if os.Getenv("TP_FILTER_TEE_FAIL") == "1" {
		t.Setenv("SLIMFERENCE_TEE_DIR", os.Getenv("TP_FILTER_TEE_DIR"))
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleSubcommand([]string{"filter", "--", "sh", "-c", "exit 7"})
		return
	}
	teeDir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_filter_nonZeroExit_teeRecovery")
	cmd.Env = append(os.Environ(),
		"TP_FILTER_TEE_FAIL=1",
		"TP_FILTER_TEE_DIR="+teeDir,
	)
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("want exit 7, got err=%v out=%s", err, out)
	}
	if !strings.Contains(string(out), "saved raw output") {
		t.Fatalf("expected tee message: %q", out)
	}
}

func TestHandleSubcommand_rewrite_stdinHookJSON(t *testing.T) {

	if os.Getenv("TP_REWRITE_STDIN") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_stdinHookJSON")
	cmd.Env = append(os.Environ(), "TP_REWRITE_STDIN=1")
	cmd.Stdin = strings.NewReader(`{"command":"git status"}`)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("err=%v stderr may be lost", err)
	}
	if strings.TrimSpace(string(out)) != "slimference filter git status" {
		t.Fatalf("got %q", out)
	}
}

// TestHandleSubcommand_rewrite_stdinHookJSON_NoFilter covers the exit-1 path when
// the command read from hook JSON has no matching filter (docs/spec.md §4.2 exit code 1 = passthrough).
func TestHandleSubcommand_rewrite_stdinHookJSON_NoFilter(t *testing.T) {
	if os.Getenv("TP_REWRITE_NOFIL") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_stdinHookJSON_NoFilter")
	cmd.Env = append(os.Environ(), "TP_REWRITE_NOFIL=1")
	cmd.Stdin = strings.NewReader(`{"command":"echo rewrite-ok"}`)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 (no filter match), got err=%v", err)
	}
}

func TestHandleSubcommand_rewrite_stdinNoCommandExits1(t *testing.T) {
	if os.Getenv("TP_REWRITE_NO_CMD") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_stdinNoCommandExits1")
	cmd.Env = append(os.Environ(), "TP_REWRITE_NO_CMD=1")
	cmd.Stdin = strings.NewReader(`{}`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), `"command"`) {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestHandleSubcommand_rewrite_stdinInvalidJSONExits1(t *testing.T) {
	if os.Getenv("TP_REWRITE_BAD_JSON") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_stdinInvalidJSONExits1")
	cmd.Env = append(os.Environ(), "TP_REWRITE_BAD_JSON=1")
	cmd.Stdin = strings.NewReader(`not-json`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "JSON") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

// TestHandleSubcommand_rewrite_usageTTYExits1 exercises handleRewriteCmd when there are no
// args and stdin is a TTY (usage on stderr, exit 1). Uses /dev/tty when available.
func TestHandleSubcommand_rewrite_usageTTYExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/tty")
	}
	if os.Getenv("TP_REWRITE_TTY_USAGE") == "1" {
		handleSubcommand([]string{"rewrite"})
		return
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		t.Skipf("open /dev/tty: %v", err)
	}
	defer tty.Close()
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewrite_usageTTYExits1")
	cmd.Env = append(os.Environ(), "TP_REWRITE_TTY_USAGE=1")
	cmd.Stdin = tty
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage: slimference rewrite") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestMustOpenFilterDB_invalidSQLiteExits1(t *testing.T) {
	if os.Getenv("TP_MUSTOPEN_BAD") == "1" {
		mustOpenFilterDB()
		return
	}
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "filter.db")
	if err := os.WriteFile(dbPath, []byte("not-a-sqlite-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMustOpenFilterDB_invalidSQLiteExits1")
	cmd.Env = append(os.Environ(), "TP_MUSTOPEN_BAD=1", "SLIMFERENCE_FILTER_DB="+dbPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestMustOpenFilterDB_statNotExistVsOther covers mustOpenFilterDB when os.Stat fails with
// an error other than ErrNotExist (e.g. permission denied traversing a mode-000 directory).
func TestMustOpenFilterDB_statNotExistVsOther(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode not applicable")
	}
	if os.Getenv("TP_MUSTOPEN_STAT") == "1" {
		mustOpenFilterDB()
		return
	}
	tmp := t.TempDir()
	blocked := filepath.Join(tmp, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(blocked, "filter.db")
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })

	cmd := exec.Command(os.Args[0], "-test.run=TestMustOpenFilterDB_statNotExistVsOther")
	cmd.Env = append(os.Environ(), "TP_MUSTOPEN_STAT=1", "SLIMFERENCE_FILTER_DB="+dbPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_filterUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_FILTER_USAGE") == "1" {
		handleSubcommand([]string{"filter"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_filterUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_FILTER_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_rewriteLayer0DenyExits2(t *testing.T) {
	if os.Getenv("TP_SUB_RW_DENY") == "1" {
		handleSubcommand([]string{"rewrite", "rm", "-rf", "/"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewriteLayer0DenyExits2")
	cmd.Env = append(os.Environ(), "TP_SUB_RW_DENY=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("want exit 2, got err=%v", err)
	}
}

func TestHandleSubcommand_rewriteSudoExits3(t *testing.T) {
	if os.Getenv("TP_SUB_RW_SUDO") == "1" {
		t.Setenv("SLIMFERENCE_CONFIRM_SUDO", "")
		handleSubcommand([]string{"rewrite", "sudo", "apt", "update"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_rewriteSudoExits3")
	cmd.Env = append(os.Environ(), "TP_SUB_RW_SUDO=1", "SLIMFERENCE_CONFIRM_SUDO=")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("want exit 3, got err=%v", err)
	}
}

// TestHandleFilterCmd_deniedExits2 covers handleFilterCmd layer0 permission deny (main.go:274-277).
func TestHandleFilterCmd_deniedExits2(t *testing.T) {
	if os.Getenv("TP_FILTER_DENY") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleFilterCmd([]string{"rm", "-rf", "/"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleFilterCmd_deniedExits2")
	cmd.Env = append(os.Environ(), "TP_FILTER_DENY=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatalf("want exit 2 (deny), got err=%v", err)
	}
}

// TestHandleFilterCmd_sudoExits3 covers handleFilterCmd layer0 sudo ask path (main.go:274-277 exit 3).
func TestHandleFilterCmd_sudoExits3(t *testing.T) {
	if os.Getenv("TP_FILTER_SUDO") == "1" {
		t.Setenv("SLIMFERENCE_CONFIRM_SUDO", "")
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleFilterCmd([]string{"sudo", "apt", "update"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleFilterCmd_sudoExits3")
	cmd.Env = append(os.Environ(), "TP_FILTER_SUDO=1", "SLIMFERENCE_CONFIRM_SUDO=")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("want exit 3 (sudo ask), got err=%v", err)
	}
}

// TestHandleRewriteCmd_dashDashSkip covers the `if a == "--" { continue }` branch.
// Uses a filterable command so exit 0 with rewritten output is expected (docs/spec.md §4.2).
func TestHandleRewriteCmd_dashDashSkip(t *testing.T) {
	if os.Getenv("TP_RW_DASHDASH") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleRewriteCmd([]string{"--", "git", "status"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleRewriteCmd_dashDashSkip")
	cmd.Env = append(os.Environ(), "TP_RW_DASHDASH=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("want exit 0, got err=%v out=%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "slimference filter git status" {
		t.Fatalf("expected 'slimference filter git status', got %q", out)
	}
}

// TestHandleRewriteCmd_dashDashSkip_NoFilter covers the exit-1 path when "--" skips
// the separator but the resulting command has no matching filter (docs/spec.md §4.2 exit 1 = passthrough).
func TestHandleRewriteCmd_dashDashSkip_NoFilter(t *testing.T) {
	if os.Getenv("TP_RW_DASHDASH_NF") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleRewriteCmd([]string{"--", "echo", "hi"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleRewriteCmd_dashDashSkip_NoFilter")
	cmd.Env = append(os.Environ(), "TP_RW_DASHDASH_NF=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1 (no filter), got err=%v", err)
	}
}

// TestHandleFilterCmd_prErrExits1 covers the pr.Err != nil branch (main.go:312-315) when
// the command cannot be found (exec failure).
func TestHandleFilterCmd_prErrExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	if os.Getenv("TP_FILTER_PRERR") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
		handleFilterCmd([]string{"nonexistent-command-xyz-abc-1234"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleFilterCmd_prErrExits1")
	cmd.Env = append(os.Environ(), "TP_FILTER_PRERR=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleFilterCmd_getwdError covers the os.Getwd error exit in handleFilterCmd (main.go:293-297).
func TestHandleFilterCmd_getwdError(t *testing.T) {
	orig := osGetwd
	defer func() { osGetwd = orig }()
	osGetwd = func() (string, error) { return "", errors.New("getwd failed") }
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"filter", "--", "echo", "hi"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "getwd") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleRewriteCmd_terminalTrue covers the TTY-detected exit in handleRewriteCmd (main.go:351-354).
func TestHandleRewriteCmd_terminalTrue(t *testing.T) {
	orig := termIsTerminalFn
	defer func() { termIsTerminalFn = orig }()
	termIsTerminalFn = func(fd int) bool { return true }
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"rewrite"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "usage: slimference rewrite") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestHandleRewriteCmd_stdinReadError covers the io.ReadAll error exit in handleRewriteCmd (main.go:355-359).
func TestHandleRewriteCmd_stdinReadError(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
	}()
	termIsTerminalFn = func(fd int) bool { return false }
	readStdinAll = func() ([]byte, error) { return nil, errors.New("read failed") }
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"rewrite"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "read stdin") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestMustOpenFilterDB_resolvePathError covers the resolveFilterDBPathFn error exit (main.go:254-257).
func TestMustOpenFilterDB_resolvePathError(t *testing.T) {
	orig := resolveFilterDBPathFn
	defer func() { resolveFilterDBPathFn = orig }()
	resolveFilterDBPathFn = func() (string, error) { return "", errors.New("path error") }

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { mustOpenFilterDB() })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "filter db path") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestMustOpenFilterDB_invalidSQLite_inProcess is the in-process equivalent of
// TestMustOpenFilterDB_invalidSQLiteExits1, contributing to coverage.
func TestMustOpenFilterDB_invalidSQLite_inProcess(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "corrupt.db")
	if err := os.WriteFile(dbPath, []byte("not-a-sqlite-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { mustOpenFilterDB() })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "open filter db") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

// TestMustOpenFilterDB_statError_inProcess is the in-process equivalent of
// TestMustOpenFilterDB_statNotExistVsOther, contributing to coverage.
func TestMustOpenFilterDB_statError_inProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode not applicable on Windows")
	}
	tmp := t.TempDir()
	blocked := filepath.Join(tmp, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(blocked, "filter.db")
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })
	t.Setenv("SLIMFERENCE_FILTER_DB", dbPath)

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { mustOpenFilterDB() })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "filter db") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

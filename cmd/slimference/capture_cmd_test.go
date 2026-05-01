package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureEnvForTest(t *testing.T, home string) (env captureSessionEnv, stdout, stderr *bytes.Buffer) {
	t.Helper()
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	env = captureSessionEnv{
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
		},
		Stdout: stdout,
		Stderr: stderr,
		Home:   home,
	}
	return env, stdout, stderr
}

func TestCaptureSession_StartGeneratesNameAndPolicyHint(t *testing.T) {
	t.Parallel()
	env, stdout, _ := captureEnvForTest(t, t.TempDir())
	rc := captureSessionRun(nil, env)
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	out := stdout.String()
	if !strings.Contains(out, "session_20260501_120000") {
		t.Fatalf("expected timestamped fallback name, got %q", out)
	}
	if !strings.Contains(out, "live-corpus-policy.md") {
		t.Fatalf("expected privacy hint pointing at policy doc, got %q", out)
	}
	if !strings.Contains(out, "SLIMFERENCE_DEBUG_DECISIONS_LOG=") {
		t.Fatalf("expected env-var instruction, got %q", out)
	}
}

func TestCaptureSession_StartHonoursName(t *testing.T) {
	t.Parallel()
	env, stdout, _ := captureEnvForTest(t, t.TempDir())
	rc := captureSessionRun([]string{"--name=feature_long_001"}, env)
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "feature_long_001.jsonl") {
		t.Fatalf("expected named jsonl in output, got %q", stdout.String())
	}
}

func TestCaptureSession_StopMessage(t *testing.T) {
	t.Parallel()
	env, stdout, _ := captureEnvForTest(t, t.TempDir())
	rc := captureSessionRun([]string{"--stop"}, env)
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "captures") {
		t.Fatalf("expected stop guidance, got %q", stdout.String())
	}
}

func TestCaptureSession_StatusEmpty(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env, stdout, _ := captureEnvForTest(t, home)
	rc := captureSessionRun([]string{"--status"}, env)
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "No captures yet") {
		t.Fatalf("expected guidance for missing dir, got %q", stdout.String())
	}
}

func TestCaptureSession_StatusListsFiles(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	captureDir := filepath.Join(home, ".slimference", "captures")
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(captureDir, "alpha.jsonl"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(captureDir, "ignore.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(captureDir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	env, stdout, _ := captureEnvForTest(t, home)
	rc := captureSessionRun([]string{"--status"}, env)
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	out := stdout.String()
	if !strings.Contains(out, "alpha.jsonl") {
		t.Fatalf("expected alpha.jsonl listed, got %q", out)
	}
	if strings.Contains(out, "ignore.txt") {
		t.Fatalf("non-jsonl file leaked into status output: %q", out)
	}
}

func TestCaptureSession_StatusEmptyDirShowsNone(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	captureDir := filepath.Join(home, ".slimference", "captures")
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	env, stdout, _ := captureEnvForTest(t, home)
	rc := captureSessionRun([]string{"--status"}, env)
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "(none)") {
		t.Fatalf("expected (none) marker, got %q", stdout.String())
	}
}

func TestCaptureSession_UnknownFlag(t *testing.T) {
	t.Parallel()
	env, _, stderr := captureEnvForTest(t, t.TempDir())
	rc := captureSessionRun([]string{"--bogus"}, env)
	if rc != 2 {
		t.Fatalf("expected exit 2, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Fatalf("expected unknown flag note, got %q", stderr.String())
	}
}

func TestCaptureSession_UnexpectedPositional(t *testing.T) {
	t.Parallel()
	env, _, stderr := captureEnvForTest(t, t.TempDir())
	rc := captureSessionRun([]string{"oops"}, env)
	if rc != 2 {
		t.Fatalf("expected exit 2, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "unexpected") {
		t.Fatalf("expected unexpected-arg note, got %q", stderr.String())
	}
}

func TestCaptureSession_NoHomeFails(t *testing.T) {
	t.Parallel()
	env, _, stderr := captureEnvForTest(t, "")
	rc := captureSessionRun(nil, env)
	if rc != 1 {
		t.Fatalf("expected exit 1, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "HOME not set") {
		t.Fatalf("expected HOME guidance, got %q", stderr.String())
	}
}

func TestCaptureSession_StatusReadFail(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Create captures as a regular file so ReadDir fails for a reason
	// other than ErrNotExist, exercising the error branch.
	captureDir := filepath.Join(home, ".slimference", "captures")
	if err := os.MkdirAll(filepath.Dir(captureDir), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(captureDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	env, _, stderr := captureEnvForTest(t, home)
	rc := captureSessionRun([]string{"--status"}, env)
	if rc != 1 {
		t.Fatalf("expected exit 1 on unreadable captures path, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "read") {
		t.Fatalf("expected read-failure note, got %q", stderr.String())
	}
}

func TestCaptureSession_PublicEntrypointInvokesExit(t *testing.T) {
	originalExit := exitFn
	defer func() { exitFn = originalExit }()
	captured := -1
	exitFn = func(code int) { captured = code }
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", "")
	handleCaptureSessionCmd(nil)
	if captured != 1 {
		t.Fatalf("expected exitFn(1), got %d", captured)
	}
}

func TestCaptureSession_PublicEntrypointSuccessNoExit(t *testing.T) {
	originalExit := exitFn
	defer func() { exitFn = originalExit }()
	exitFn = func(code int) { t.Fatalf("exitFn called with %d on success", code) }
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", t.TempDir())
	handleCaptureSessionCmd([]string{"--status"})
}

func TestCaptureSession_StartFlagIsAccepted(t *testing.T) {
	t.Parallel()
	env, stdout, _ := captureEnvForTest(t, t.TempDir())
	rc := captureSessionRun([]string{"--start", "--name=explicit"}, env)
	if rc != 0 {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "explicit.jsonl") {
		t.Fatalf("expected named output, got %q", stdout.String())
	}
}

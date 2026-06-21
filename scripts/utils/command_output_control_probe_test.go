package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCommandOutputControlProbe_processLocalShellAndPathShim(t *testing.T) {
	originalShell := os.Getenv("SHELL")
	originalBashEnv := os.Getenv("BASH_ENV")
	originalPath := os.Getenv("PATH")
	temp := t.TempDir()
	fakeBin := filepath.Join(temp, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	realProbeTarget := filepath.Join(fakeBin, "probe-target")
	if err := os.WriteFile(realProbeTarget, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+originalPath)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("BASH_ENV", originalBashEnv)

	var childOut bytes.Buffer
	var childErr bytes.Buffer
	result, err := commandOutputControlProbe(commandOutputProbeFlags{
		keepDir:      true,
		shimCommands: []string{"probe-target"},
		childArgs:    []string{"sh", "-c", `probe-target; "$SHELL" -c 'exit 0'`},
	}, &childOut, &childErr)
	if err != nil {
		t.Fatalf("probe failed: %v stderr=%s", err, childErr.String())
	}
	if result.ChildExitCode != 0 {
		t.Fatalf("child exit=%d stderr=%s", result.ChildExitCode, childErr.String())
	}
	if !result.RouteSafety.ProcessLocalOnly ||
		result.RouteSafety.WritesShellRC ||
		result.RouteSafety.WritesCodexConfig ||
		result.RouteSafety.WritesGlobalProxy ||
		result.RouteSafety.TouchesHostsOrPFCTL ||
		result.RouteSafety.TouchesNormalCodexApps {
		t.Fatalf("unexpected route safety: %#v", result.RouteSafety)
	}
	if !result.Observed.ShellWrapper {
		t.Fatalf("shell wrapper was not observed; findings=%v log=%s", result.Findings, readProbeLogForTest(t, result.LogPath))
	}
	if !result.Observed.PathShims["probe-target"] {
		t.Fatalf("path shim was not observed; findings=%v log=%s", result.Findings, readProbeLogForTest(t, result.LogPath))
	}
	assertProbeSeamForTest(t, result, "shell", "observed", false)
	assertProbeSeamForTest(t, result, "path_shim", "observed", true)
	assertProbeSeamForTest(t, result, "hook_replacement", "not_tested", false)
	assertProbeSeamForTest(t, result, "pty_wrapper", "not_tested", false)
	assertProbeSeamForTest(t, result, "app_server_mcp", "not_tested", false)
	if !commandOutputProbeTestContainsString(result.Findings, "product_primary_seam:bash_env_path_shim") {
		t.Fatalf("missing primary seam finding: %v", result.Findings)
	}
	if got := os.Getenv("SHELL"); got != "/bin/sh" {
		t.Fatalf("parent SHELL changed to %q", got)
	}
	if got := os.Getenv("BASH_ENV"); got != originalBashEnv {
		t.Fatalf("parent BASH_ENV changed to %q", got)
	}
	if got := os.Getenv("PATH"); !strings.HasPrefix(got, fakeBin) {
		t.Fatalf("test PATH setup lost: %q", got)
	}
	_ = originalShell
}

func TestCommandOutputControlProbe_bashEnvOptional(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell probe is POSIX-only")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash unavailable")
	}
	t.Setenv("SHELL", "/bin/sh")

	var childOut bytes.Buffer
	var childErr bytes.Buffer
	result, err := commandOutputControlProbe(commandOutputProbeFlags{
		realShell: "/bin/sh",
		childArgs: []string{"/bin/bash", "-c", "true"},
	}, &childOut, &childErr)
	if err != nil {
		t.Fatalf("probe failed: %v stderr=%s", err, childErr.String())
	}
	if result.ChildExitCode != 0 {
		t.Fatalf("child exit=%d stderr=%s", result.ChildExitCode, childErr.String())
	}
	if !result.Observed.BashEnv {
		t.Fatalf("BASH_ENV was not observed; findings=%v log=%s", result.Findings, readProbeLogForTest(t, result.LogPath))
	}
	assertProbeSeamForTest(t, result, "bash_env", "observed", true)
}

func TestParseCommandOutputProbeFlags_requiresChildCommand(t *testing.T) {
	t.Parallel()

	if _, err := parseCommandOutputProbeFlags([]string{"--json"}); err == nil {
		t.Fatal("expected missing child command error")
	}
}

func TestCommandOutputControlProbe_timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell probe is POSIX-only")
	}

	var childOut bytes.Buffer
	var childErr bytes.Buffer
	result, err := commandOutputControlProbe(commandOutputProbeFlags{
		timeout:   10 * time.Millisecond,
		childArgs: []string{"/bin/sh", "-c", "sleep 1"},
	}, &childOut, &childErr)
	if err != nil {
		t.Fatalf("probe returned hard error: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("expected timeout result: %#v", result)
	}
	if !commandOutputProbeTestContainsString(result.Findings, "child_timeout") {
		t.Fatalf("missing child_timeout finding: %v", result.Findings)
	}
}

func commandOutputProbeTestContainsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func TestCommandOutputControlProbe_textIncludesSeamVerdicts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell probe is POSIX-only")
	}

	temp := t.TempDir()
	fakeBin := filepath.Join(temp, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	realProbeTarget := filepath.Join(fakeBin, "probe-target")
	if err := os.WriteFile(realProbeTarget, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SHELL", "/bin/sh")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runCommandOutputControlProbe([]string{"--shim-command=probe-target", "--", "sh", "-c", "probe-target"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("probe exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	text := stdout.String()
	for _, want := range []string{
		"seam[path_shim]=observed primary=true",
		"seam[pty_wrapper]=not_tested primary=false",
		"seam[app_server_mcp]=not_tested primary=false",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in text output:\n%s", want, text)
		}
	}
}

func assertProbeSeamForTest(t *testing.T, result commandOutputProbeResult, seam, status string, primary bool) {
	t.Helper()
	verdict, ok := result.SeamVerdicts[seam]
	if !ok {
		t.Fatalf("missing seam verdict %q in %#v", seam, result.SeamVerdicts)
	}
	if verdict.Status != status || verdict.PrimaryProductEligible != primary {
		t.Fatalf("seam %s got status=%s primary=%v want status=%s primary=%v reason=%s", seam, verdict.Status, verdict.PrimaryProductEligible, status, primary, verdict.Reason)
	}
	if strings.TrimSpace(verdict.Reason) == "" {
		t.Fatalf("seam %s has empty reason", seam)
	}
}

func TestNormalizeProbeShimCommands(t *testing.T) {
	t.Parallel()

	got := normalizeProbeShimCommands([]string{" git ", "rg", "git", "/tmp/nope", "", "."})
	want := []string{"git", "rg"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func readProbeLogForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(data)
}

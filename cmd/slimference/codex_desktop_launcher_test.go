package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCodexLaunchDesktopFlagsDefaults(t *testing.T) {
	f, err := parseCodexLaunchDesktopFlags(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.host != "127.0.0.1" || f.port != "8990" {
		t.Fatalf("defaults host=%q port=%q", f.host, f.port)
	}
	if f.probe || f.help || f.appPath != "" || len(f.extra) != 0 {
		t.Fatalf("unexpected non-zero: %+v", f)
	}
}

func TestParseCodexLaunchDesktopFlagsAll(t *testing.T) {
	args := []string{
		"--probe",
		"--host=10.0.0.5",
		"--port=9000",
		"--app=/opt/Codex.app",
		"--env=FOO=bar",
		"--env=BAZ=qux",
	}
	f, err := parseCodexLaunchDesktopFlags(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !f.probe || f.host != "10.0.0.5" || f.port != "9000" || f.appPath != "/opt/Codex.app" {
		t.Fatalf("flags=%+v", f)
	}
	if len(f.extra) != 2 || f.extra[0] != "FOO=bar" || f.extra[1] != "BAZ=qux" {
		t.Fatalf("extra=%v", f.extra)
	}
}

func TestParseCodexLaunchDesktopFlagsHelp(t *testing.T) {
	for _, a := range []string{"--help", "-h"} {
		f, err := parseCodexLaunchDesktopFlags([]string{a})
		if err != nil || !f.help {
			t.Fatalf("%q: help=%v err=%v", a, f.help, err)
		}
	}
}

func TestParseCodexLaunchDesktopFlagsRejectsBadEnv(t *testing.T) {
	if _, err := parseCodexLaunchDesktopFlags([]string{"--env=NO_EQUAL"}); err == nil {
		t.Fatal("expected error on --env without '='")
	}
	if _, err := parseCodexLaunchDesktopFlags([]string{"--bogus"}); err == nil {
		t.Fatal("expected error on unknown flag")
	}
}

func TestBuildCodexDesktopLaunchEnvOverridesAndDeduplicates(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HOME=/Users/x",
		"OPENAI_BASE_URL=https://api.openai.com/v1",  // must be dropped
		"CHATGPT_CODEX_BASE_URL=https://chatgpt.com", // must be dropped
		"UNRELATED=keep",
		"NOEQUAL", // no '=' — preserved verbatim
	}
	want := "http://127.0.0.1:8990/backend-api/codex"
	got := buildCodexDesktopLaunchEnv(want, base, nil)

	preserved := map[string]bool{
		"PATH=/usr/bin":  false,
		"HOME=/Users/x":  false,
		"UNRELATED=keep": false,
		"NOEQUAL":        false,
	}
	for _, kv := range got {
		if _, ok := preserved[kv]; ok {
			preserved[kv] = true
		}
	}
	for k, v := range preserved {
		if !v {
			t.Errorf("missing preserved env entry %q", k)
		}
	}

	overrides := map[string]int{}
	for _, k := range codexDesktopEnvOverrideKeys {
		overrides[k] = 0
	}
	for _, kv := range got {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key := kv[:eq]
		if _, isOverride := overrides[key]; isOverride {
			overrides[key]++
			if kv != key+"="+want {
				t.Errorf("override %q got %q, want %q", key, kv, key+"="+want)
			}
		}
	}
	for k, n := range overrides {
		if n != 1 {
			t.Errorf("override %q appears %d times, want 1", k, n)
		}
	}
}

func TestBuildCodexDesktopLaunchEnvExtraAppliesLast(t *testing.T) {
	base := []string{"OPENAI_API_BASE=https://api.openai.com/v1"}
	got := buildCodexDesktopLaunchEnv("http://x", base, []string{"OPENAI_API_BASE=http://custom"})
	// expect exactly one OPENAI_API_BASE entry equal to the operator extra
	var seen []string
	for _, kv := range got {
		if strings.HasPrefix(kv, "OPENAI_API_BASE=") {
			seen = append(seen, kv)
		}
	}
	if len(seen) != 2 {
		// override appended first, then operator extra appended at the end.
		// dedup is by removing base entries only; both override+extra are kept.
		t.Fatalf("OPENAI_API_BASE entries=%v (want 2: override + extra)", seen)
	}
	if seen[0] != "OPENAI_API_BASE=http://x" {
		t.Errorf("first OPENAI_API_BASE=%q want override URL", seen[0])
	}
	if seen[1] != "OPENAI_API_BASE=http://custom" {
		t.Errorf("second (extra) OPENAI_API_BASE=%q want operator value", seen[1])
	}
	// last wins in spawn semantics because cmd.Env is consumed by the OS
	// in order; many libc loaders pick the last occurrence.
}

func TestFilterCodexDesktopOverrideEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"CHATGPT_CODEX_BASE_URL=http://x",
		"FOO=bar",
		"OPENAI_BASE_URL=http://x",
	}
	got := filterCodexDesktopOverrideEnv(env)
	want := []string{"CHATGPT_CODEX_BASE_URL=http://x", "OPENAI_BASE_URL=http://x"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filter[%d] = %q want %q", i, got[i], want[i])
		}
	}
}

func TestRunCodexLaunchDesktopRejectsMissingBinary(t *testing.T) {
	prevStat := codexDesktopStatFn
	prevAppFn := codexDesktopAppPathFn
	t.Cleanup(func() {
		codexDesktopStatFn = prevStat
		codexDesktopAppPathFn = prevAppFn
	})
	codexDesktopAppPathFn = func() string { return "/nonexistent/Codex.app" }
	codexDesktopStatFn = func(name string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}

	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd(nil, installPrinter{Out: &out, Err: &errBuf})
	if rc != 1 {
		t.Fatalf("rc=%d want 1", rc)
	}
	if !strings.Contains(errBuf.String(), "Codex.app binary not found") {
		t.Errorf("stderr missing error message: %q", errBuf.String())
	}
}

func TestRunCodexLaunchDesktopProbeEmitsJSON(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Codex.app")
	bin := filepath.Join(app, defaultCodexDesktopExecRelPath)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	prevStart := codexDesktopStartFn
	t.Cleanup(func() { codexDesktopStartFn = prevStart })
	startCalled := false
	codexDesktopStartFn = func(p installPrinter, binary string, env []string) int {
		startCalled = true
		return 0
	}

	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd(
		[]string{"--probe", "--app=" + app, "--host=192.0.2.1", "--port=4444"},
		installPrinter{Out: &out, Err: &errBuf},
	)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	if startCalled {
		t.Fatal("probe must not spawn Codex.app")
	}
	var probe codexLaunchDesktopProbe
	if err := json.Unmarshal(out.Bytes(), &probe); err != nil {
		t.Fatalf("json: %v\nraw: %s", err, out.String())
	}
	wantURL := "http://192.0.2.1:4444/backend-api/codex"
	if probe.OverrideURL != wantURL {
		t.Errorf("OverrideURL=%q want %q", probe.OverrideURL, wantURL)
	}
	if probe.Binary != bin {
		t.Errorf("Binary=%q want %q", probe.Binary, bin)
	}
	if len(probe.EnvOverride) != len(codexDesktopEnvOverrideKeys) {
		t.Errorf("EnvOverride entries=%d want %d", len(probe.EnvOverride), len(codexDesktopEnvOverrideKeys))
	}
	for _, kv := range probe.EnvOverride {
		if !strings.HasSuffix(kv, "="+wantURL) {
			t.Errorf("env entry %q does not end with override URL", kv)
		}
	}
}

func TestRunCodexLaunchDesktopSpawnsViaInjectedStartFn(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Codex.app")
	bin := filepath.Join(app, defaultCodexDesktopExecRelPath)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	prevStart := codexDesktopStartFn
	prevBaseEnv := codexDesktopBaseEnvFn
	t.Cleanup(func() {
		codexDesktopStartFn = prevStart
		codexDesktopBaseEnvFn = prevBaseEnv
	})
	codexDesktopBaseEnvFn = func() []string { return []string{"PATH=/usr/bin"} }

	var (
		seenBinary string
		seenEnv    []string
	)
	codexDesktopStartFn = func(p installPrinter, binary string, env []string) int {
		seenBinary = binary
		seenEnv = env
		_, _ = p.Out.Write([]byte("stub-launched\n"))
		return 0
	}

	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd(
		[]string{"--app=" + app, "--env=FOO=bar"},
		installPrinter{Out: &out, Err: &errBuf},
	)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	if seenBinary != bin {
		t.Errorf("binary=%q want %q", seenBinary, bin)
	}
	hasPath := false
	hasFoo := false
	overrides := 0
	for _, kv := range seenEnv {
		if kv == "PATH=/usr/bin" {
			hasPath = true
		}
		if kv == "FOO=bar" {
			hasFoo = true
		}
		for _, k := range codexDesktopEnvOverrideKeys {
			if strings.HasPrefix(kv, k+"=") {
				overrides++
			}
		}
	}
	if !hasPath {
		t.Error("base env PATH must be preserved")
	}
	if !hasFoo {
		t.Error("extra env FOO=bar must be appended")
	}
	if overrides != len(codexDesktopEnvOverrideKeys) {
		t.Errorf("override count=%d want %d", overrides, len(codexDesktopEnvOverrideKeys))
	}
}

func TestRunCodexLaunchDesktopHelp(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd([]string{"--help"}, installPrinter{Out: &out, Err: &errBuf})
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out.String(), "usage: slimference codex launch-desktop") {
		t.Errorf("help text missing usage line: %s", out.String())
	}
}

func TestRunCodexLaunchDesktopBadFlag(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd([]string{"--unknown"}, installPrinter{Out: &out, Err: &errBuf})
	if rc != 2 {
		t.Fatalf("rc=%d want 2", rc)
	}
	if !strings.Contains(errBuf.String(), "unknown flag") {
		t.Errorf("stderr missing 'unknown flag': %q", errBuf.String())
	}
}

func TestStartCodexDesktopProcessSpawnsRealBinary(t *testing.T) {
	// Use /bin/echo as a benign stand-in for Codex.app. It exits quickly
	// and never produces interactive UI, so the test stays hermetic.
	if _, err := os.Stat("/bin/echo"); err != nil {
		t.Skip("/bin/echo not present")
	}
	var out, errBuf bytes.Buffer
	rc := startCodexDesktopProcess(installPrinter{Out: &out, Err: &errBuf},
		"/bin/echo", []string{"PATH=/usr/bin", "CHATGPT_CODEX_BASE_URL=http://x"})
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "Codex.app launched") {
		t.Errorf("stdout missing success line: %q", out.String())
	}
	// /bin/echo exits immediately; wait a beat so the process reaper
	// doesn't leave a zombie for parallel tests.
	time.Sleep(50 * time.Millisecond)
}

// Sanity: probe path on real Codex.app (if present) does not invoke
// the spawn function. Skipped when /Applications/Codex.app missing so
// CI on machines without the app stays green.
func TestRunCodexLaunchDesktopProbeRealAppPresent(t *testing.T) {
	if _, err := os.Stat(defaultCodexDesktopAppPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("Codex.app not installed; skipping real-path probe")
		}
		t.Fatal(err)
	}
	prevStart := codexDesktopStartFn
	t.Cleanup(func() { codexDesktopStartFn = prevStart })
	codexDesktopStartFn = func(installPrinter, string, []string) int {
		t.Fatal("probe must not spawn")
		return 0
	}

	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd([]string{"--probe"}, installPrinter{Out: &out, Err: &errBuf})
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	var probe codexLaunchDesktopProbe
	if err := json.Unmarshal(out.Bytes(), &probe); err != nil {
		t.Fatalf("json: %v\nraw: %s", err, out.String())
	}
	if !strings.HasPrefix(probe.OverrideURL, "http://127.0.0.1:8990/") {
		t.Errorf("default override URL host=%q", probe.OverrideURL)
	}
}

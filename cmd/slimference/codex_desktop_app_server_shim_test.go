package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCodexDesktopAppServerShimExec(t *testing.T) {
	codexBin := writeFakeExecutable(t, "codex")
	env := []string{
		"PATH=/usr/bin",
		"CODEX_CLI_PATH=/tmp/slimference",
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=" + codexBin,
		"SLIMFERENCE_CODEX_DESKTOP_BASE_URL=http://127.0.0.1:8990/backend-api/codex/",
		"FOO=bar",
	}
	argv0, argv, childEnv, err := buildCodexDesktopAppServerShimExec([]string{"--analytics-default-enabled"}, env)
	if err != nil {
		t.Fatalf("build exec: %v", err)
	}
	if argv0 != codexBin {
		t.Fatalf("argv0=%q", argv0)
	}
	joinedArgs := strings.Join(argv, "\n")
	for _, want := range []string{
		codexBin,
		"app-server",
		"model_provider=\"slimference-codex\"",
		"model_providers.slimference-codex.base_url=\"http://127.0.0.1:8990/backend-api/codex\"",
		"model_providers.slimference-codex.requires_openai_auth=true",
		"model_providers.slimference-codex.supports_websockets=true",
		"model_providers.slimference-codex.wire_api=\"responses\"",
		"--analytics-default-enabled",
	} {
		if !strings.Contains(joinedArgs, want) {
			t.Fatalf("argv missing %q in %v", want, argv)
		}
	}
	joinedEnv := strings.Join(childEnv, "\n")
	for _, forbidden := range []string{"CODEX_CLI_PATH=", "SLIMFERENCE_CODEX_DESKTOP_ACTIVE=", "SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=", "SLIMFERENCE_CODEX_DESKTOP_BASE_URL="} {
		if strings.Contains(joinedEnv, forbidden) {
			t.Fatalf("child env leaked %s in %v", forbidden, childEnv)
		}
	}
	if !strings.Contains(joinedEnv, "PATH=/usr/bin") || !strings.Contains(joinedEnv, "FOO=bar") {
		t.Fatalf("child env lost ordinary entries: %v", childEnv)
	}
}

func TestBuildCodexDesktopAppServerShimExecRejectsMissingScope(t *testing.T) {
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=/tmp/codex"}); err == nil {
		t.Fatal("expected inactive scope rejection")
	}
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1"}); err == nil {
		t.Fatal("expected missing upstream rejection")
	}
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=/tmp/codex",
	}); err == nil {
		t.Fatal("expected inaccessible upstream rejection before base-url")
	}
	codexBin := writeFakeExecutable(t, "codex")
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=" + codexBin,
	}); err == nil {
		t.Fatal("expected missing base-url rejection")
	}
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=" + codexBin,
		"SLIMFERENCE_CODEX_DESKTOP_BASE_URL=https://chatgpt.com/backend-api/codex",
	}); err == nil {
		t.Fatal("expected non-local base-url rejection")
	}
}

func TestRunCodexDesktopAppServerShimExecsRealCodex(t *testing.T) {
	prevExec := codexDesktopAppServerExecFn
	t.Cleanup(func() { codexDesktopAppServerExecFn = prevExec })
	codexBin := writeFakeExecutable(t, "codex")
	var gotArgv0 string
	var gotArgv []string
	var gotEnv []string
	codexDesktopAppServerExecFn = func(argv0 string, argv []string, envv []string) error {
		gotArgv0 = argv0
		gotArgv = append([]string(nil), argv...)
		gotEnv = append([]string(nil), envv...)
		return nil
	}
	env := []string{
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=" + codexBin,
		"SLIMFERENCE_CODEX_DESKTOP_BASE_URL=http://127.0.0.1:8990/backend-api/codex",
	}
	for _, kv := range env {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		t.Setenv(key, value)
	}

	var out, errBuf bytes.Buffer
	rc := runCodexDesktopAppServerShim([]string{"--analytics-default-enabled"}, installPrinter{Out: &out, Err: &errBuf})
	if rc != 0 {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	if gotArgv0 != codexBin || !strings.Contains(strings.Join(gotArgv, "\n"), "app-server") {
		t.Fatalf("exec argv0=%q argv=%v", gotArgv0, gotArgv)
	}
	if strings.Contains(strings.Join(gotEnv, "\n"), "SLIMFERENCE_CODEX_DESKTOP_ACTIVE=") {
		t.Fatalf("exec env leaked shim state: %v", gotEnv)
	}

	codexDesktopAppServerExecFn = func(string, []string, []string) error { return errors.New("exec denied") }
	rc = runCodexDesktopAppServerShim([]string{"--analytics-default-enabled"}, installPrinter{Out: &out, Err: &errBuf})
	if rc != 1 || !strings.Contains(errBuf.String(), "exec denied") {
		t.Fatalf("exec failure rc=%d err=%q", rc, errBuf.String())
	}
}

func writeFakeExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

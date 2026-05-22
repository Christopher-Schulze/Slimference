package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	if f.transport != codexDesktopTransportProxy {
		t.Fatalf("default transport=%q want proxy", f.transport)
	}
	if f.probe || f.help || f.appPath != "" || f.insecureSkipTrustCheck || len(f.extra) != 0 {
		t.Fatalf("unexpected non-zero: %+v", f)
	}
}

func TestParseCodexLaunchDesktopFlagsAll(t *testing.T) {
	args := []string{
		"--probe",
		"--host=10.0.0.5",
		"--port=9000",
		"--transport=proxy",
		"--app=/opt/Codex.app",
		"--with-ca-env",
		"--env=FOO=bar",
		"--env=BAZ=qux",
		"--insecure-skip-cert-trust-check",
	}
	f, err := parseCodexLaunchDesktopFlags(args)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !f.probe || !f.withCAEnv || !f.insecureSkipTrustCheck || f.host != "10.0.0.5" || f.port != "9000" || f.transport != codexDesktopTransportProxy || f.appPath != "/opt/Codex.app" {
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
	if _, err := parseCodexLaunchDesktopFlags([]string{"--transport=bad"}); err == nil {
		t.Fatal("expected error on invalid transport")
	}
	if _, err := parseCodexLaunchDesktopFlags([]string{"--transport=base-url", "--with-ca-env"}); err == nil {
		t.Fatal("expected error when --with-ca-env is used outside proxy mode")
	}
}

func TestBuildCodexDesktopLaunchEnvOverridesAndDeduplicates(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HOME=/Users/x",
		"OPENAI_BASE_URL=https://api.openai.com/v1",  // must be dropped
		"CHATGPT_CODEX_BASE_URL=https://chatgpt.com", // must be dropped
		"CODEX_THREAD_ID=old-thread",                 // inherited session state must be dropped
		"CODEX_CI=1",                                 // inherited Codex runtime state must be dropped
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
		if strings.HasPrefix(kv, "CODEX_THREAD_ID=") || strings.HasPrefix(kv, "CODEX_CI=") {
			t.Fatalf("base-url launch must not inherit Codex session env: %v", got)
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

func TestBuildCodexDesktopProxyEnvScopedAndNoBaseURLOverrides(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HTTPS_PROXY=http://old",
		"OPENAI_BASE_URL=http://old-base",
		"CODEX_THREAD_ID=old-thread",
		"CODEX_CI=1",
		"UNRELATED=keep",
		"NOEQUAL",
	}
	got := buildCodexDesktopProxyEnv("http://127.0.0.1:8990", base, []string{"HTTPS_PROXY=http://operator"})
	wantPresent := map[string]bool{
		"PATH=/usr/bin":                     false,
		"UNRELATED=keep":                    false,
		"NOEQUAL":                           false,
		"HTTP_PROXY=http://127.0.0.1:8990":  false,
		"HTTPS_PROXY=http://127.0.0.1:8990": false,
		"WSS_PROXY=http://127.0.0.1:8990":   false,
		"ALL_PROXY=http://127.0.0.1:8990":   false,
		"NO_PROXY=127.0.0.1,localhost,::1":  false,
		"CODEX_NETWORK_PROXY_ACTIVE=1":      false,
		"HTTPS_PROXY=http://operator":       false,
	}
	for _, kv := range got {
		if _, ok := wantPresent[kv]; ok {
			wantPresent[kv] = true
		}
		if strings.HasPrefix(kv, "OPENAI_BASE_URL=") {
			t.Fatalf("proxy mode must not leak base-url override: %v", got)
		}
		if strings.HasPrefix(kv, "CODEX_THREAD_ID=") || strings.HasPrefix(kv, "CODEX_CI=") {
			t.Fatalf("desktop launch must not inherit Codex session env: %v", got)
		}
	}
	for kv, seen := range wantPresent {
		if !seen {
			t.Errorf("missing env entry %q in %v", kv, got)
		}
	}
}

func TestSanitizeCodexDesktopBaseEnvDropsInheritedSessionState(t *testing.T) {
	got := sanitizeCodexDesktopBaseEnv([]string{
		"PATH=/usr/bin",
		"CODEX_THREAD_ID=old-thread",
		"CODEX_HOME=/tmp/codex-home",
		"CODEX_MANAGED_BY_NPM=1",
		"UNRELATED=keep",
		"NOEQUAL",
	})
	joined := strings.Join(got, "\n")
	for _, forbidden := range []string{"CODEX_THREAD_ID=", "CODEX_HOME=", "CODEX_MANAGED_BY_NPM="} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("sanitized env still contains %s in %v", forbidden, got)
		}
	}
	for _, want := range []string{"PATH=/usr/bin", "UNRELATED=keep", "NOEQUAL"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("sanitized env missing %s in %v", want, got)
		}
	}
}

func TestCodexDesktopDirectOpenEnvSetsLaunchPWDAndDropsThreadState(t *testing.T) {
	got := codexDesktopDirectOpenEnv([]string{
		"PATH=/usr/bin",
		"PWD=/Users/christopher/CODE/ClankWork-main",
		"OLDPWD=/tmp",
		"CODEX_THREAD_ID=old-thread",
		"CODEX_CI=1",
		"HOME=/Users/christopher",
	}, "/Users/christopher/CODE/Slimference")
	joined := strings.Join(got, "\n")
	for _, forbidden := range []string{"CODEX_THREAD_ID=", "CODEX_CI=", "OLDPWD=", "PWD=/Users/christopher/CODE/ClankWork-main"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("direct open env leaked %s in %v", forbidden, got)
		}
	}
	if !strings.Contains(joined, "PWD=/Users/christopher/CODE/Slimference") {
		t.Fatalf("direct open env did not pin launch PWD: %v", got)
	}
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "HOME=/Users/christopher") {
		t.Fatalf("direct open env lost ordinary environment: %v", got)
	}
}

func TestAppendCodexDesktopCAEnvIsExplicitAndExtraWins(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"CODEX_CA_CERTIFICATE=/old/codex-root.crt",
		"SSL_CERT_FILE=/old/root.crt",
		"HTTPS_PROXY=http://127.0.0.1:8990",
		"NOEQUAL",
	}
	got := appendCodexDesktopCAEnv(base, "/tmp/slimference-root.crt", []string{"SSL_CERT_FILE=/operator/root.crt"})
	wantPresent := map[string]bool{
		"PATH=/usr/bin":                     false,
		"HTTPS_PROXY=http://127.0.0.1:8990": false,
		"NOEQUAL":                           false,
		"CODEX_CA_CERTIFICATE=/tmp/slimference-root.crt": false,
		"SSL_CERT_FILE=/tmp/slimference-root.crt":        false,
		"CURL_CA_BUNDLE=/tmp/slimference-root.crt":       false,
		"REQUESTS_CA_BUNDLE=/tmp/slimference-root.crt":   false,
		"NODE_EXTRA_CA_CERTS=/tmp/slimference-root.crt":  false,
		"SSL_CERT_FILE=/operator/root.crt":               false,
	}
	for _, kv := range got {
		if _, ok := wantPresent[kv]; ok {
			wantPresent[kv] = true
		}
		if kv == "SSL_CERT_FILE=/old/root.crt" {
			t.Fatalf("old CA env must be removed: %v", got)
		}
		if kv == "CODEX_CA_CERTIFICATE=/old/codex-root.crt" {
			t.Fatalf("old Codex CA env must be removed: %v", got)
		}
	}
	for kv, seen := range wantPresent {
		if !seen {
			t.Errorf("missing %q in %v", kv, got)
		}
	}
	if got[len(got)-1] != "SSL_CERT_FILE=/operator/root.crt" {
		t.Fatalf("operator extra must be last, got %v", got)
	}
}

func TestFilterCodexDesktopOverrideEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"NOEQUAL",
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

func TestFilterCodexDesktopProxyEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"NOEQUAL",
		"HTTPS_PROXY=http://x",
		"CODEX_CA_CERTIFICATE=/tmp/root.crt",
		"SSL_CERT_FILE=/tmp/root.crt",
		"FOO=bar",
		"NO_PROXY=127.0.0.1",
	}
	got := filterCodexDesktopProxyEnv(env)
	want := []string{"HTTPS_PROXY=http://x", "CODEX_CA_CERTIFICATE=/tmp/root.crt", "SSL_CERT_FILE=/tmp/root.crt", "NO_PROXY=127.0.0.1"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filter[%d] = %q want %q", i, got[i], want[i])
		}
	}
}

func TestRunCodexLaunchDesktopProbeWithCAEnvEmitsRootHints(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Codex.app")
	bin := filepath.Join(app, defaultCodexDesktopExecRelPath)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	prevCA := codexDesktopCATrustFn
	t.Cleanup(func() { codexDesktopCATrustFn = prevCA })
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: true}
	}

	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd(
		[]string{"--probe", "--with-ca-env", "--app=" + app},
		installPrinter{Out: &out, Err: &errBuf},
	)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	var probe codexLaunchDesktopProbe
	if err := json.Unmarshal(out.Bytes(), &probe); err != nil {
		t.Fatalf("json: %v\nraw: %s", err, out.String())
	}
	joined := strings.Join(probe.EnvOverride, "\n")
	for _, want := range []string{
		"CODEX_CA_CERTIFICATE=/tmp/root.crt",
		"SSL_CERT_FILE=/tmp/root.crt",
		"CURL_CA_BUNDLE=/tmp/root.crt",
		"REQUESTS_CA_BUNDLE=/tmp/root.crt",
		"NODE_EXTRA_CA_CERTS=/tmp/root.crt",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("probe missing %q in %s", want, joined)
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
	prevCA := codexDesktopCATrustFn
	t.Cleanup(func() {
		codexDesktopStartFn = prevStart
		codexDesktopCATrustFn = prevCA
	})
	startCalled := false
	codexDesktopStartFn = func(p installPrinter, binary string, env []string) int {
		startCalled = true
		return 0
	}
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: true}
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
	wantProxy := "http://192.0.2.1:4444"
	if probe.Transport != codexDesktopTransportProxy {
		t.Errorf("Transport=%q want proxy", probe.Transport)
	}
	if probe.Binary != bin {
		t.Errorf("Binary=%q want %q", probe.Binary, bin)
	}
	if probe.ProxyURL != wantProxy {
		t.Errorf("ProxyURL=%q want %q", probe.ProxyURL, wantProxy)
	}
	if !probe.CATrust.Trusted {
		t.Errorf("CA trust not propagated: %+v", probe.CATrust)
	}
	if len(probe.EnvOverride) != len(codexDesktopProxyEnvKeys) {
		t.Errorf("EnvOverride entries=%d want %d", len(probe.EnvOverride), len(codexDesktopProxyEnvKeys))
	}
	for _, kv := range probe.EnvOverride {
		if strings.HasPrefix(kv, "NO_PROXY=") || strings.HasPrefix(kv, "no_proxy=") || strings.HasPrefix(kv, "CODEX_NETWORK_PROXY_ACTIVE=") {
			continue
		}
		if !strings.HasSuffix(kv, "="+wantProxy) {
			t.Errorf("env entry %q does not end with proxy URL", kv)
		}
	}
}

func TestRunCodexLaunchDesktopBaseURLProbeEmitsDiagnosticEnv(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Codex.app")
	bin := filepath.Join(app, defaultCodexDesktopExecRelPath)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd(
		[]string{"--transport=base-url", "--probe", "--app=" + app, "--host=192.0.2.9", "--port=4999"},
		installPrinter{Out: &out, Err: &errBuf},
	)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	var probe codexLaunchDesktopProbe
	if err := json.Unmarshal(out.Bytes(), &probe); err != nil {
		t.Fatalf("json: %v\nraw: %s", err, out.String())
	}
	wantURL := "http://192.0.2.9:4999/backend-api/codex"
	if probe.Transport != codexDesktopTransportBaseURL || probe.OverrideURL != wantURL || probe.ProxyURL != "" {
		t.Fatalf("probe=%+v", probe)
	}
	if len(probe.EnvOverride) != len(codexDesktopEnvOverrideKeys) {
		t.Fatalf("base-url env count=%d want %d", len(probe.EnvOverride), len(codexDesktopEnvOverrideKeys))
	}
	if probe.CATrust.Exists || probe.CATrust.Trusted || probe.CATrust.Path != "" {
		t.Fatalf("base-url diagnostic mode must not report CA gate: %+v", probe.CATrust)
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
	prevCA := codexDesktopCATrustFn
	t.Cleanup(func() {
		codexDesktopStartFn = prevStart
		codexDesktopBaseEnvFn = prevBaseEnv
		codexDesktopCATrustFn = prevCA
	})
	codexDesktopBaseEnvFn = func() []string { return []string{"PATH=/usr/bin"} }
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: true}
	}

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
		for _, k := range codexDesktopProxyEnvKeys {
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
	if overrides != len(codexDesktopProxyEnvKeys) {
		t.Errorf("override count=%d want %d", overrides, len(codexDesktopProxyEnvKeys))
	}
}

func TestRunCodexLaunchDesktopRefusesProxyWithoutTrustedCA(t *testing.T) {
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
	prevCA := codexDesktopCATrustFn
	t.Cleanup(func() {
		codexDesktopStartFn = prevStart
		codexDesktopCATrustFn = prevCA
	})
	startCalled := false
	codexDesktopStartFn = func(p installPrinter, binary string, env []string) int {
		startCalled = true
		return 0
	}
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: false}
	}

	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd([]string{"--app=" + app}, installPrinter{Out: &out, Err: &errBuf})
	if rc != 1 {
		t.Fatalf("rc=%d want 1", rc)
	}
	if startCalled {
		t.Fatal("launcher must not spawn when CA trust gate fails")
	}
	if !strings.Contains(errBuf.String(), "cert-trust") {
		t.Fatalf("stderr missing cert-trust remediation: %q", errBuf.String())
	}
}

func TestRunCodexLaunchDesktopWithCAEnvAllowsUntrustedKeychain(t *testing.T) {
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
	prevCA := codexDesktopCATrustFn
	t.Cleanup(func() {
		codexDesktopStartFn = prevStart
		codexDesktopCATrustFn = prevCA
	})
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: false, Error: "keychain untrusted"}
	}
	var seenEnv []string
	codexDesktopStartFn = func(p installPrinter, binary string, env []string) int {
		seenEnv = env
		return 0
	}

	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd(
		[]string{"--app=" + app, "--with-ca-env"},
		installPrinter{Out: &out, Err: &errBuf},
	)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	joined := strings.Join(seenEnv, "\n")
	if !strings.Contains(joined, "CODEX_CA_CERTIFICATE=/tmp/root.crt") {
		t.Fatalf("spawn env missing Codex CA hook: %s", joined)
	}
}

func TestRunCodexLaunchDesktopRefusesExistingMainInstance(t *testing.T) {
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
	prevCA := codexDesktopCATrustFn
	prevRunning := codexDesktopRunningFn
	t.Cleanup(func() {
		codexDesktopStartFn = prevStart
		codexDesktopCATrustFn = prevCA
		codexDesktopRunningFn = prevRunning
	})
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: true}
	}
	codexDesktopRunningFn = func(binary string) ([]int, error) {
		if binary != bin {
			t.Fatalf("running probe binary=%q want %q", binary, bin)
		}
		return []int{101, 202}, nil
	}
	startCalled := false
	codexDesktopStartFn = func(p installPrinter, binary string, env []string) int {
		startCalled = true
		return 0
	}

	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd(
		[]string{"--app=" + app, "--with-ca-env"},
		installPrinter{Out: &out, Err: &errBuf},
	)
	if rc != 1 {
		t.Fatalf("rc=%d want 1", rc)
	}
	if startCalled {
		t.Fatal("launcher must not spawn when Codex.app is already running")
	}
	if !strings.Contains(errBuf.String(), "already running") || !strings.Contains(errBuf.String(), "101,202") {
		t.Fatalf("stderr missing running-process detail: %q", errBuf.String())
	}
}

func TestRunCodexLaunchDesktopRefusesProxyWithMissingCA(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Codex.app")
	bin := filepath.Join(app, defaultCodexDesktopExecRelPath)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	prevCA := codexDesktopCATrustFn
	t.Cleanup(func() { codexDesktopCATrustFn = prevCA })
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/missing.crt", Exists: false, Trusted: false, Error: "probe failed"}
	}
	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd([]string{"--app=" + app}, installPrinter{Out: &out, Err: &errBuf})
	if rc != 1 {
		t.Fatalf("rc=%d want 1", rc)
	}
	if !strings.Contains(errBuf.String(), "install") {
		t.Fatalf("stderr missing install remediation: %q", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "probe failed") {
		t.Fatalf("stderr missing probe error: %q", errBuf.String())
	}
}

func TestRunCodexLaunchDesktopInsecureSkipTrustCheckSpawns(t *testing.T) {
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
	prevCA := codexDesktopCATrustFn
	t.Cleanup(func() {
		codexDesktopStartFn = prevStart
		codexDesktopCATrustFn = prevCA
	})
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: false}
	}
	spawned := false
	codexDesktopStartFn = func(p installPrinter, binary string, env []string) int {
		spawned = true
		return 0
	}
	var out, errBuf bytes.Buffer
	rc := runCodexLaunchDesktopCmd(
		[]string{"--app=" + app, "--insecure-skip-cert-trust-check"},
		installPrinter{Out: &out, Err: &errBuf},
	)
	if rc != 0 || !spawned {
		t.Fatalf("rc=%d spawned=%v stderr=%q", rc, spawned, errBuf.String())
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

func TestHandleCodexLaunchDesktopCmdHelp(t *testing.T) {
	oldExit := exitFn
	t.Cleanup(func() { exitFn = oldExit })
	got := -1
	exitFn = func(code int) { got = code }

	handleCodexLaunchDesktopCmd([]string{"--help"})
	if got != 0 {
		t.Fatalf("exit=%d want 0", got)
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

func TestEmitCodexDesktopProbeWriterError(t *testing.T) {
	var errBuf bytes.Buffer
	rc := emitCodexDesktopProbe(
		installPrinter{Out: failingCodexDesktopWriter{}, Err: &errBuf},
		"/Applications/Codex.app/Contents/MacOS/Codex",
		codexDesktopTransportProxy,
		"http://127.0.0.1:8990/backend-api/codex",
		"http://127.0.0.1:8990",
		[]string{"HTTPS_PROXY=http://127.0.0.1:8990"},
		codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: true},
	)
	if rc != 1 || !strings.Contains(errBuf.String(), "probe encode") {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
}

type failingCodexDesktopWriter struct{}

func (failingCodexDesktopWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestCodexDesktopCATrustStateBranches(t *testing.T) {
	prevHome := osUserHomeDir
	prevKeychain := newTransparentKeychainFn
	t.Cleanup(func() {
		osUserHomeDir = prevHome
		newTransparentKeychainFn = prevKeychain
	})

	osUserHomeDir = func() (string, error) { return "", errors.New("home") }
	if got := codexDesktopCATrustState(); !strings.Contains(got.Error, "home") {
		t.Fatalf("home error state=%+v", got)
	}

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	missing := codexDesktopCATrustState()
	if missing.Exists || missing.Trusted || !strings.Contains(missing.Error, "no such file") {
		t.Fatalf("missing state=%+v", missing)
	}

	cert := filepath.Join(home, ".slimference", "ca", "root.crt")
	if err := os.MkdirAll(filepath.Dir(cert), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cert, []byte("cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	newTransparentKeychainFn = func() proxyKeychain {
		return &fakeKeychain{trusted: true}
	}
	trusted := codexDesktopCATrustState()
	if !trusted.Exists || !trusted.Trusted || trusted.Path != cert {
		t.Fatalf("trusted state=%+v", trusted)
	}
	newTransparentKeychainFn = func() proxyKeychain {
		return &fakeKeychain{trusted: false, verifyErr: errors.New("verify failed")}
	}
	untrusted := codexDesktopCATrustState()
	if !untrusted.Exists || untrusted.Trusted || !strings.Contains(untrusted.Error, "verify failed") {
		t.Fatalf("untrusted state=%+v", untrusted)
	}
}

func TestNewCodexDesktopCommandDetached(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "Codex.app", defaultCodexDesktopExecRelPath)
	cmd := newCodexDesktopCommand(binary, []string{"PATH=/usr/bin", "FOO=bar"})

	if cmd.Path != binary {
		t.Fatalf("Path=%q want %q", cmd.Path, binary)
	}
	if cmd.Dir != filepath.Dir(binary) {
		t.Fatalf("Dir=%q want %q", cmd.Dir, filepath.Dir(binary))
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Fatalf("Codex Desktop launcher must detach into a new session, SysProcAttr=%+v", cmd.SysProcAttr)
	}
	if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
		t.Fatalf("stdio should stay nil so exec connects it to %s", os.DevNull)
	}
	if strings.Join(cmd.Env, "\n") != "PATH=/usr/bin\nFOO=bar" {
		t.Fatalf("env=%v", cmd.Env)
	}
}

func TestStartCodexDesktopProcessSpawnsDetachedBinary(t *testing.T) {
	script := writeCodexDesktopTestScript(t, "#!/bin/sh\nsleep 30\n")
	oldDelay := codexDesktopStartProbeDelay
	t.Cleanup(func() { codexDesktopStartProbeDelay = oldDelay })
	codexDesktopStartProbeDelay = 25 * time.Millisecond

	var out, errBuf bytes.Buffer
	rc := startCodexDesktopProcess(installPrinter{Out: &out, Err: &errBuf},
		script, []string{"PATH=/bin:/usr/bin", "CHATGPT_CODEX_BASE_URL=http://x"})
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "Codex.app launched") {
		t.Errorf("stdout missing success line: %q", out.String())
	}
	var pid int
	if _, err := fmt.Sscanf(out.String(), "Codex.app launched (PID %d)", &pid); err != nil {
		t.Fatalf("parse launch pid from %q: %v", out.String(), err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find process %d: %v", pid, err)
	}
	_ = proc.Kill()
	var status syscall.WaitStatus
	_, _ = syscall.Wait4(pid, &status, 0, nil)
}

func TestStartCodexDesktopProcessRejectsImmediateExit(t *testing.T) {
	script := writeCodexDesktopTestScript(t, "#!/bin/sh\nexit 0\n")
	oldDelay := codexDesktopStartProbeDelay
	t.Cleanup(func() { codexDesktopStartProbeDelay = oldDelay })
	codexDesktopStartProbeDelay = 25 * time.Millisecond

	var out, errBuf bytes.Buffer
	rc := startCodexDesktopProcess(installPrinter{Out: &out, Err: &errBuf},
		script, []string{"PATH=/usr/bin"})
	if rc != 1 {
		t.Fatalf("rc=%d want 1; stdout=%q stderr=%q", rc, out.String(), errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "process exited during startup") || !strings.Contains(errBuf.String(), "exit=0") {
		t.Fatalf("stderr missing early-exit proof: %q", errBuf.String())
	}
}

func TestStartCodexDesktopProcessSpawnFailure(t *testing.T) {
	var out, errBuf bytes.Buffer
	rc := startCodexDesktopProcess(installPrinter{Out: &out, Err: &errBuf},
		filepath.Join(t.TempDir(), "missing-binary"), []string{"PATH=/usr/bin"})
	if rc != 1 {
		t.Fatalf("rc=%d want 1", rc)
	}
	if !strings.Contains(errBuf.String(), "spawn failed") {
		t.Fatalf("stderr missing spawn failure: %q", errBuf.String())
	}
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

func writeCodexDesktopTestScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-desktop-test")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

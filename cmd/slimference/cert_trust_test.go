package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleCertTrustHelpDoesNotExit(t *testing.T) {
	out, _ := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleCertTrustCmd([]string{"--help"}) })
		if exited {
			t.Fatalf("unexpected exit %d", code)
		}
	})
	if !strings.Contains(out, "slimference cert-trust") {
		t.Fatalf("help output missing command name: %q", out)
	}
}

func TestHandleCertTrustMissingHomeExits1(t *testing.T) {
	prevHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = prevHome })

	_, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleCertTrustCmd(nil) })
		if !exited || code != 1 {
			t.Fatalf("exit=(%d,%v), want (1,true)", code, exited)
		}
	})
	if !strings.Contains(stderr, "HOME unresolved") {
		t.Fatalf("stderr missing HOME error: %q", stderr)
	}
}

func TestHandleCertTrustMissingCertExits1(t *testing.T) {
	home := withTempHomeForRootCommand(t)
	_, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleCertTrustCmd(nil) })
		if !exited || code != 1 {
			t.Fatalf("exit=(%d,%v), want (1,true)", code, exited)
		}
	})
	if !strings.Contains(stderr, filepath.Join(home, ".slimference", "ca", "root.crt")) {
		t.Fatalf("stderr missing cert path: %q", stderr)
	}
}

func TestHandleCertTrustOpensKeychainThroughInjectedRunner(t *testing.T) {
	home := withTempHomeForRootCommand(t)
	cert := filepath.Join(home, ".slimference", "ca", "root.crt")
	if err := os.MkdirAll(filepath.Dir(cert), 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	if err := os.WriteFile(cert, []byte("cert"), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	prevOpen := openKeychainAccessFn
	var gotCert string
	openKeychainAccessFn = func(path string) error {
		gotCert = path
		return nil
	}
	t.Cleanup(func() { openKeychainAccessFn = prevOpen })

	out, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleCertTrustCmd(nil) })
		if exited {
			t.Fatalf("unexpected exit %d", code)
		}
	})
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if gotCert != cert {
		t.Fatalf("opened cert=%q want %q", gotCert, cert)
	}
	for _, want := range []string{"Keychain Access opened", "sudo security add-trusted-cert", cert} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestHandleCertTrustReportsOpenFailureButDoesNotExit(t *testing.T) {
	home := withTempHomeForRootCommand(t)
	cert := filepath.Join(home, ".slimference", "ca", "root.crt")
	if err := os.MkdirAll(filepath.Dir(cert), 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	if err := os.WriteFile(cert, []byte("cert"), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	prevOpen := openKeychainAccessFn
	openKeychainAccessFn = func(string) error { return errors.New("no gui") }
	t.Cleanup(func() { openKeychainAccessFn = prevOpen })

	out, stderr := captureRootCommandOutput(t, func() {
		code, exited := captureExit(func() { handleCertTrustCmd(nil) })
		if exited {
			t.Fatalf("unexpected exit %d", code)
		}
	})
	if !strings.Contains(stderr, "could not auto-open Keychain Access") {
		t.Fatalf("stderr missing open failure: %q", stderr)
	}
	if !strings.Contains(out, "sudo security add-trusted-cert") {
		t.Fatalf("fallback command missing: %q", out)
	}
}

func TestOpenKeychainAccessUsesOpenCommand(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "open.log")
	openPath := filepath.Join(dir, "open")
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" > " + shellQuoteForTest(logPath) + "\n"
	if err := os.WriteFile(openPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake open: %v", err)
	}
	t.Setenv("PATH", dir)

	cert := filepath.Join(dir, "root.crt")
	if err := openKeychainAccess(cert); err != nil {
		t.Fatalf("openKeychainAccess: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake open log: %v", err)
	}
	if strings.TrimSpace(string(data)) != cert {
		t.Fatalf("open arg=%q want %q", strings.TrimSpace(string(data)), cert)
	}
}

func shellQuoteForTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

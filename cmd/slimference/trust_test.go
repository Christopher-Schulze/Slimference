package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/filter"
)

// writeTrustFile is a small helper to create a filter file for trust tests.
func writeTrustFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "filters.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// redirectStdoutForTrust pipes stdout through a reader while the test runs.
func redirectStdoutForTrust(t *testing.T) (r *os.File, cleanup func()) {
	t.Helper()
	orig := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	return rp, func() {
		_ = wp.Close()
		os.Stdout = orig
	}
}

// redirectTrustStore points the trust store at a tempdir for the duration
// of a test.
func redirectTrustStore(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	filter.SetTrustStorePathForTest(filepath.Join(dir, "trust.json"))
	t.Cleanup(func() { filter.ResetTrustStorePathForTest() })
}

func TestHandleTrustCmd_noArgsFails(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleTrustCmd(nil) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "trust [add|list|remove|status]") {
		t.Fatalf("want usage hint on exit 1, got code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

func TestHandleTrustCmd_unknownSub(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleTrustCmd([]string{"nope"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "unknown trust subcommand") {
		t.Fatalf("unknown sub: code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

func TestHandleTrustCmd_addListRemoveRoundtrip(t *testing.T) {
	redirectTrustStore(t)
	path := writeTrustFile(t, "schema_version = 1\n")

	// add
	rp, cleanup := redirectStdoutForTrust(t)
	handleTrustCmd([]string{"add", path})
	cleanup()
	var addOut bytes.Buffer
	_, _ = io.Copy(&addOut, rp)
	if !strings.Contains(addOut.String(), "Trusted") || !strings.Contains(addOut.String(), "sha256:") {
		t.Fatalf("add output: %s", addOut.String())
	}

	// status
	rp, cleanup = redirectStdoutForTrust(t)
	handleTrustCmd([]string{"status", path})
	cleanup()
	var statusOut bytes.Buffer
	_, _ = io.Copy(&statusOut, rp)
	if !strings.Contains(statusOut.String(), "trusted") {
		t.Fatalf("status output: %s", statusOut.String())
	}

	// list
	rp, cleanup = redirectStdoutForTrust(t)
	handleTrustCmd([]string{"list"})
	cleanup()
	var listOut bytes.Buffer
	_, _ = io.Copy(&listOut, rp)
	if !strings.Contains(listOut.String(), "Trusted filters:") || !strings.Contains(listOut.String(), path) {
		t.Fatalf("list output: %s", listOut.String())
	}

	// remove
	rp, cleanup = redirectStdoutForTrust(t)
	handleTrustCmd([]string{"remove", path})
	cleanup()
	var rmOut bytes.Buffer
	_, _ = io.Copy(&rmOut, rp)
	if !strings.Contains(rmOut.String(), "Removed trust") {
		t.Fatalf("remove output: %s", rmOut.String())
	}

	// remove again -> no record
	rp, cleanup = redirectStdoutForTrust(t)
	handleTrustCmd([]string{"remove", path})
	cleanup()
	var rm2 bytes.Buffer
	_, _ = io.Copy(&rm2, rp)
	if !strings.Contains(rm2.String(), "No trust record") {
		t.Fatalf("second remove: %s", rm2.String())
	}

	// list empty
	rp, cleanup = redirectStdoutForTrust(t)
	handleTrustCmd([]string{"list"})
	cleanup()
	var emptyList bytes.Buffer
	_, _ = io.Copy(&emptyList, rp)
	if !strings.Contains(emptyList.String(), "No trusted filters") {
		t.Fatalf("empty list: %s", emptyList.String())
	}
}

func TestHandleTrustCmd_addMissingArg(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleTrustCmd([]string{"add"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "add <path>") {
		t.Fatalf("add missing arg: code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

func TestHandleTrustCmd_removeMissingArg(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleTrustCmd([]string{"remove"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "remove <path>") {
		t.Fatalf("remove missing arg: code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

func TestHandleTrustCmd_statusMissingArg(t *testing.T) {
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleTrustCmd([]string{"status"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "status <path>") {
		t.Fatalf("status missing arg: code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

func TestHandleTrustCmd_addFailure(t *testing.T) {
	redirectTrustStore(t)
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleTrustCmd([]string{"add", filepath.Join(t.TempDir(), "nope.toml")}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "trust add:") {
		t.Fatalf("add fail: code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

func TestHandleTrustCmd_statusFailure(t *testing.T) {
	// Force an error by pointing the trust store at a dir so load fails.
	dir := t.TempDir()
	filter.SetTrustStorePathForTest(dir)
	t.Cleanup(func() { filter.ResetTrustStorePathForTest() })

	file := writeTrustFile(t, "x")
	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleTrustCmd([]string{"status", file}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "trust status:") {
		t.Fatalf("status fail: code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

func TestHandleTrustCmd_listFailure(t *testing.T) {
	// Trust store at a directory -> load error.
	dir := t.TempDir()
	filter.SetTrustStorePathForTest(dir)
	t.Cleanup(func() { filter.ResetTrustStorePathForTest() })

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleTrustCmd([]string{"list"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "trust list:") {
		t.Fatalf("list fail: code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

func TestHandleTrustCmd_removeFailure(t *testing.T) {
	// Trust store at a directory -> load error.
	dir := t.TempDir()
	filter.SetTrustStorePathForTest(dir)
	t.Cleanup(func() { filter.ResetTrustStorePathForTest() })

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() { handleTrustCmd([]string{"remove", "/whatever"}) })
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !exited || code != 1 || !strings.Contains(buf.String(), "trust remove:") {
		t.Fatalf("remove fail: code=%d exited=%v err=%q", code, exited, buf.String())
	}
}

// TestHandleSubcommand_trustDispatch covers the top-level dispatcher branch.
func TestHandleSubcommand_trustDispatch(t *testing.T) {
	redirectTrustStore(t)
	path := writeTrustFile(t, "x")

	origOut := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp
	handleSubcommand([]string{"trust", "add", path})
	_ = wp.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	if !strings.Contains(buf.String(), "Trusted") {
		t.Fatalf("trust dispatch: %s", buf.String())
	}
}

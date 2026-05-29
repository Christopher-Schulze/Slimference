package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildCommandArgsDefault(t *testing.T) {
	got := buildCommandArgs("./slimference", "")
	want := []string{
		"build",
		"-trimpath",
		"-ldflags", "-s -w",
		"-o", "./slimference",
		"./cmd/slimference",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildCommandArgsVersionInjection(t *testing.T) {
	got := strings.Join(buildCommandArgs("bin/slimference", " v2.3.0 "), " ")
	if !strings.Contains(got, "-trimpath") || !strings.Contains(got, "-s -w") {
		t.Fatalf("missing release flags: %s", got)
	}
	if !strings.Contains(got, "github.com/slimference/slimference/internal/buildinfo.Version=v2.3.0") {
		t.Fatalf("missing buildinfo version injection: %s", got)
	}
}

func TestRunDryRun(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := run([]string{"--dry-run", "--install", "--out", "/tmp/slimference"}, &out, &errOut); err != nil {
		t.Fatalf("run dry-run: %v stderr=%s", err, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "go build -trimpath -ldflags -s -w -o /tmp/slimference ./cmd/slimference") {
		t.Fatalf("dry-run missing build command: %q", text)
	}
	if !strings.Contains(text, "install /tmp/slimference ->") {
		t.Fatalf("dry-run missing install command: %q", text)
	}
}

func TestRunDryRunRestartCeremony(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := run([]string{"--dry-run", "--restart", "--out", "/tmp/slimference"}, &out, &errOut); err != nil {
		t.Fatalf("run dry-run restart: %v stderr=%s", err, errOut.String())
	}
	text := out.String()
	stopIdx := strings.Index(text, " stop\n")
	buildIdx := strings.Index(text, "go build ")
	installIdx := strings.Index(text, "install /tmp/slimference ->")
	startIdx := strings.LastIndex(text, " start\n")
	if stopIdx < 0 || buildIdx < 0 || installIdx < 0 || startIdx < 0 {
		t.Fatalf("dry-run restart missing ceremony steps: %q", text)
	}
	if !(stopIdx < buildIdx && buildIdx < installIdx && installIdx < startIdx) {
		t.Fatalf("restart ceremony order wrong: %q", text)
	}
}

func TestRunRejectsUnexpectedArg(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := run([]string{"extra"}, &out, &errOut); err == nil {
		t.Fatal("expected unexpected argument error")
	}
}

func TestRunInstalledLifecycleSkipsMissingStop(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	missing := filepath.Join(t.TempDir(), "slimference")
	if err := runInstalledLifecycle(missing, "stop", &out, &errOut, false); err != nil {
		t.Fatalf("missing stop should be a no-op: %v", err)
	}
	if !strings.Contains(out.String(), "skip stop") {
		t.Fatalf("missing stop output=%q", out.String())
	}
}

func TestCopyFileInstallsAtomicallyExecutable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "new binary" {
		t.Fatalf("dst=%q", got)
	}
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("mode=%#o want 0755", st.Mode().Perm())
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".dst.tmp-*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary install files left behind: %v", leftovers)
	}
}

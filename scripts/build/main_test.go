package main

import (
	"bytes"
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

func TestRunRejectsUnexpectedArg(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := run([]string{"extra"}, &out, &errOut); err == nil {
		t.Fatal("expected unexpected argument error")
	}
}

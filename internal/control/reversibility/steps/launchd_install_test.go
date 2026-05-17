package steps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/control/reversibility"
)

// makeStubLaunchctl creates a tiny shell script that exits 0 - safe
// substitute for /bin/launchctl in tests.
func makeStubLaunchctl(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "launchctl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLaunchdInstallApplyWritesPlist(t *testing.T) {
	dir := t.TempDir()
	step := &LaunchdInstall{
		PlistDir:      dir,
		BinaryPath:    "/usr/local/bin/slimference",
		LaunchctlPath: makeStubLaunchctl(t),
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	plistPath := filepath.Join(dir, "com.slimference.proxy.plist")
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "<key>Label</key>") {
		t.Errorf("plist missing Label key: %q", s)
	}
	if !strings.Contains(s, "/usr/local/bin/slimference") {
		t.Errorf("plist missing binary path: %q", s)
	}
	if !strings.Contains(s, "--no-tui") {
		t.Errorf("plist missing default arg: %q", s)
	}
}

func TestLaunchdInstallApplyCustomLabelAndArgs(t *testing.T) {
	dir := t.TempDir()
	step := &LaunchdInstall{
		Label:         "com.example.custom",
		PlistDir:      dir,
		BinaryPath:    "/opt/slimference",
		Args:          []string{"daemon", "--port=8990"},
		LaunchctlPath: makeStubLaunchctl(t),
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "com.example.custom.plist"))
	s := string(data)
	if !strings.Contains(s, "<string>com.example.custom</string>") {
		t.Errorf("plist label missing: %q", s)
	}
	if !strings.Contains(s, "daemon") || !strings.Contains(s, "--port=8990") {
		t.Errorf("custom args missing: %q", s)
	}
}

func TestLaunchdInstallApplyValidateError(t *testing.T) {
	step := &LaunchdInstall{}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected validation error on empty BinaryPath")
	}
}

func TestLaunchdInstallApplyContextCancelled(t *testing.T) {
	dir := t.TempDir()
	step := &LaunchdInstall{
		PlistDir:   dir,
		BinaryPath: "/x",
		SkipLoad:   true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := step.Apply(ctx); err == nil {
		t.Errorf("expected ctx error")
	}
}

func TestLaunchdInstallApplyMkdirError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	step := &LaunchdInstall{
		PlistDir:      filepath.Join(parent, "no-write"),
		BinaryPath:    "/x",
		LaunchctlPath: makeStubLaunchctl(t),
	}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected mkdir error")
	}
}

func TestLaunchdInstallApplyLaunchctlFailureCleansUpPlist(t *testing.T) {
	dir := t.TempDir()
	// stub that always exits 1 → simulated launchctl load failure
	failStub := filepath.Join(t.TempDir(), "launchctl-fail")
	if err := os.WriteFile(failStub, []byte("#!/bin/sh\necho boom; exit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	step := &LaunchdInstall{
		PlistDir:      dir,
		BinaryPath:    "/x",
		LaunchctlPath: failStub,
	}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected error from failing launchctl")
	}
	plistPath := filepath.Join(dir, "com.slimference.proxy.plist")
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Errorf("failed Apply did not clean up plist (err=%v)", err)
	}
}

func TestLaunchdInstallApplyWriteFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	step := &LaunchdInstall{
		PlistDir:      dir,
		BinaryPath:    "/x",
		LaunchctlPath: makeStubLaunchctl(t),
	}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected WriteFile error")
	}
}

func TestLaunchdInstallSkipLoad(t *testing.T) {
	dir := t.TempDir()
	step := &LaunchdInstall{
		PlistDir:   dir,
		BinaryPath: "/x",
		SkipLoad:   true,
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "com.slimference.proxy.plist")); err != nil {
		t.Errorf("plist not written")
	}
}

func TestLaunchdInstallReverseRemovesPlist(t *testing.T) {
	dir := t.TempDir()
	step := &LaunchdInstall{
		PlistDir:      dir,
		BinaryPath:    "/x",
		LaunchctlPath: makeStubLaunchctl(t),
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "com.slimference.proxy.plist")); !os.IsNotExist(err) {
		t.Errorf("plist still present after Reverse: err=%v", err)
	}
}

func TestLaunchdInstallReverseOnMissingPlistIsNoop(t *testing.T) {
	step := &LaunchdInstall{
		PlistDir:   t.TempDir(),
		BinaryPath: "/x",
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Errorf("missing plist Reverse should no-op, got %v", err)
	}
}

func TestLaunchdInstallReverseRejectsNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	// Create a directory where the plist file should be.
	if err := os.MkdirAll(filepath.Join(dir, "com.slimference.proxy.plist"), 0o755); err != nil {
		t.Fatal(err)
	}
	step := &LaunchdInstall{PlistDir: dir, BinaryPath: "/x"}
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected error for non-regular plist")
	}
}

func TestLaunchdInstallReverseStatErrorPropagates(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	inner := filepath.Join(dir, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "com.slimference.proxy.plist"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(inner, 0o000); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(inner, 0o755) })
	step := &LaunchdInstall{PlistDir: inner, BinaryPath: "/x"}
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected stat error")
	}
}

func TestLaunchdInstallReverseValidateError(t *testing.T) {
	step := &LaunchdInstall{}
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected validation error on empty BinaryPath")
	}
}

func TestLaunchdInstallReverseRemoveError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	plist := filepath.Join(dir, "com.slimference.proxy.plist")
	if err := os.WriteFile(plist, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	step := &LaunchdInstall{
		PlistDir: dir, BinaryPath: "/x", SkipLoad: true,
	}
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected remove error on locked dir")
	}
}

func TestLaunchdInstallInspectStates(t *testing.T) {
	dir := t.TempDir()
	step := &LaunchdInstall{PlistDir: dir, BinaryPath: "/x", SkipLoad: true}
	if s := step.Inspect(context.Background()); s != reversibility.StateAbsent {
		t.Errorf("pre-apply: got %s want absent", s)
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := step.Inspect(context.Background()); s != reversibility.StatePresent {
		t.Errorf("post-apply: got %s want present", s)
	}
}

func TestLaunchdInstallInspectNonRegularPartial(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "com.slimference.proxy.plist"), 0o755); err != nil {
		t.Fatal(err)
	}
	step := &LaunchdInstall{PlistDir: dir, BinaryPath: "/x"}
	if s := step.Inspect(context.Background()); s != reversibility.StatePartial {
		t.Errorf("dir-where-file-should-be: got %s want partial", s)
	}
}

func TestLaunchdInstallInspectStatErrorUnknown(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	inner := filepath.Join(dir, "x")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "com.slimference.proxy.plist"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(inner, 0o000); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(inner, 0o755) })
	step := &LaunchdInstall{PlistDir: inner, BinaryPath: "/x"}
	if s := step.Inspect(context.Background()); s != reversibility.StateUnknown {
		t.Errorf("locked dir: got %s want unknown", s)
	}
}

func TestLaunchdInstallDefaultPlistDir(t *testing.T) {
	step := &LaunchdInstall{BinaryPath: "/x"}
	path, err := step.plistPath()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "Library", "LaunchAgents", "com.slimference.proxy.plist")
	if path != want {
		t.Errorf("got %s want %s", path, want)
	}
}

func TestLaunchdInstallDefaultHomeErrors(t *testing.T) {
	prevHome := launchdUserHomeDirFn
	launchdUserHomeDirFn = func() (string, error) { return "", errors.New("home boom") }
	t.Cleanup(func() { launchdUserHomeDirFn = prevHome })

	step := &LaunchdInstall{BinaryPath: "/x"}
	if err := step.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "home boom") {
		t.Fatalf("Apply err=%v, want home boom", err)
	}
	if err := step.Reverse(context.Background()); err == nil || !strings.Contains(err.Error(), "home boom") {
		t.Fatalf("Reverse err=%v, want home boom", err)
	}
	if got := step.Inspect(context.Background()); got != reversibility.StateUnknown {
		t.Fatalf("Inspect=%v want unknown", got)
	}
	if _, err := step.plistPath(); err == nil || !strings.Contains(err.Error(), "home boom") {
		t.Fatalf("plistPath err=%v, want home boom", err)
	}
}

func TestLaunchdInstallDefaultLaunchctl(t *testing.T) {
	step := &LaunchdInstall{BinaryPath: "/x"}
	if step.launchctl() != "/bin/launchctl" {
		t.Errorf("default launchctl: %s", step.launchctl())
	}
}

func TestLaunchdInstallReverseUnloadFailureStillRemovesPlist(t *testing.T) {
	dir := t.TempDir()
	failStub := filepath.Join(t.TempDir(), "launchctl-failunload")
	// Stub fails on unload but Reverse must still remove the file.
	if err := os.WriteFile(failStub, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	step := &LaunchdInstall{
		PlistDir: dir, BinaryPath: "/x", LaunchctlPath: failStub,
		SkipLoad: false,
	}
	// Stage the plist by hand to avoid the failed-load cleanup path.
	plistPath := filepath.Join(dir, "com.slimference.proxy.plist")
	if err := os.WriteFile(plistPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Errorf("plist still present after Reverse with failing unload: %v", err)
	}
}

func TestRenderPlistEscapesXMLSpecialChars(t *testing.T) {
	step := &LaunchdInstall{
		BinaryPath: `/path/with "quote" & <tag>`,
		Args:       []string{`--key=val "x"`},
	}
	plist := step.renderPlist()
	if strings.Contains(plist, `"quote"`) {
		t.Errorf("quotes not escaped: %s", plist)
	}
	if strings.Contains(plist, "<tag>") && strings.Count(plist, "<tag>") > 0 {
		// The literal "<tag>" must not survive as XML angle brackets.
		// Our escape replaces < and >, so the output should contain &lt;tag&gt;.
		if !strings.Contains(plist, "&lt;tag&gt;") {
			t.Errorf("tag not escaped: %s", plist)
		}
	}
}

func TestXMLEscapeRoundTrip(t *testing.T) {
	in := `& < > "`
	got := xmlEscape(in)
	for _, want := range []string{"&amp;", "&lt;", "&gt;", "&quot;"} {
		if !strings.Contains(got, want) {
			t.Errorf("xmlEscape missing %q: %q", want, got)
		}
	}
}

func TestLaunchdInstallNameStable(t *testing.T) {
	step := &LaunchdInstall{}
	if !strings.Contains(step.Name(), "launchd") {
		t.Errorf("name: %s", step.Name())
	}
}

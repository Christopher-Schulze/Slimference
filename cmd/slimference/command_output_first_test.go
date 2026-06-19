package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

func TestCommandOutputFirstShimGitStatusCleanFullPassesEmptyOutput(t *testing.T) {
	realGit := writeFakeGit(t, `#!/bin/sh
if [ "$1" = "status" ]; then
  exit 0
fi
echo "unexpected $*" >&2
exit 2
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "status", "--short"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout=%q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestCommandOutputFirstShimGitStatusDirtyFullPasses(t *testing.T) {
	realGit := writeFakeGit(t, `#!/bin/sh
if [ "$1" = "status" ]; then
  printf ' M file.go\n?? new.txt\n'
  exit 0
fi
exit 2
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "status", "--short"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != " M file.go\n?? new.txt\n" {
		t.Fatalf("stdout=%q", got)
	}
}

func TestCommandOutputFirstShimGitDiffStatCompacts(t *testing.T) {
	withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString(" internal/proxy/file")
		b.WriteString(string(rune('a' + (i % 20))))
		b.WriteString(".go | 10 +++++-----\n")
	}
	b.WriteString(" 40 files changed, 200 insertions(+), 200 deletions(-)\n")
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "diff", "--stat"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[git diff --stat] 40 file(s)") || !strings.Contains(got, "[prefix=internal/proxy/]") {
		t.Fatalf("unexpected compacted stdout=%q", got)
	}
}

func TestCommandOutputFirstShimGitLsFilesCompacts(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 0; i < 12; i++ {
		b.WriteString("internal/proxy/path/file")
		b.WriteString(string(rune('a' + i)))
		b.WriteString(".go\n")
	}
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "ls-files", "--cached"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[git ls-files paths]") || !strings.Contains(got, "internal/proxy/path/") {
		t.Fatalf("unexpected compacted stdout=%q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:git] git ls-files --cached") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.ProjectPath != "/repo" {
		t.Fatalf("project path=%q", run.ProjectPath)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimStderrFullPasses(t *testing.T) {
	realGit := writeFakeGit(t, `#!/bin/sh
printf ' file.go | 1 +\n'
printf 'warning: side channel\n' >&2
exit 0
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "diff", "--stat"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if got := stdout.String(); got != " file.go | 1 +\n" {
		t.Fatalf("stdout=%q", got)
	}
	if got := stderr.String(); got != "warning: side channel\n" {
		t.Fatalf("stderr=%q", got)
	}
}

func TestCommandOutputFirstAllowCaptureKeepsPlainDiffOut(t *testing.T) {
	if commandOutputFirstAllowCapture("git", []string{"diff"}) {
		t.Fatal("plain git diff must not be captured by the command-output-first shim")
	}
	if !commandOutputFirstAllowCapture("git", []string{"diff", "--stat"}) {
		t.Fatal("git diff --stat should be captured")
	}
	if !commandOutputFirstAllowCapture("git", []string{"-C", "/repo", "diff", "--name-only"}) {
		t.Fatal("git -C repo diff --name-only should be captured")
	}
	if commandOutputFirstAllowCapture("git", []string{"show", "HEAD"}) {
		t.Fatal("git show must not be captured")
	}
	if !commandOutputFirstAllowCapture("rg", []string{"TODO"}) {
		t.Fatal("rg should be captured by the command-output-first shim")
	}
	if commandOutputFirstAllowCapture("grep", []string{"TODO"}) {
		t.Fatal("grep is not part of the first command-output-first command set")
	}
}

func TestCommandOutputFirstShimRgCompactsWithFullRetentionAndAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 1; i <= 36; i++ {
		b.WriteString("internal/proxy/long/path/handler.go:")
		b.WriteString(strings.Repeat("1", len("36")-len("1")))
		b.WriteString("1")
		b.WriteString(":func handleRequest() { return nil } // repeated match payload\n")
	}
	realRg := writeFakeCommand(t, "rg", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=rg", "--real-bin=" + realRg, "--", "handleRequest", "internal/proxy"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[rg] 36 match(es) in 1 file(s)") || !strings.Contains(got, "internal/proxy/long/path/handler.go") {
		t.Fatalf("unexpected compacted rg stdout=%q", got)
	}
	if strings.Contains(got, "[+") {
		t.Fatalf("rg command-output-first must retain every match in first product slice: %q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:rg] rg handleRequest internal/proxy") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimRgEmptyFullPasses(t *testing.T) {
	realRg := writeFakeCommand(t, "rg", "#!/bin/sh\nexit 0\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=rg", "--real-bin=" + realRg, "--", "missing"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommandOutputFirstEnvInjectedOnlyForScopedProxiedRun(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() { osExecutable = oldExecutable })
	binDir := t.TempDir()
	self := filepath.Join(binDir, "slimference")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	gitBin := filepath.Join(binDir, "git")
	if err := os.WriteFile(gitBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	osExecutable = func() (string, error) { return self, nil }
	t.Setenv("PATH", binDir)

	command := codexEnvCommand("proxied-wss", "127.0.0.1", "8990", []string{"exec", "hi"})
	got, cleanup := maybeApplyCommandOutputFirstEnv("proxied-wss", command)
	defer cleanup()
	joined := strings.Join(got, "\x00")
	if !strings.Contains(joined, "\x00"+commandOutputFirstActiveEnv+"=1\x00") {
		t.Fatalf("missing active env in %#v", got)
	}
	if !strings.Contains(joined, "\x00BASH_ENV=") {
		t.Fatalf("missing BASH_ENV in %#v", got)
	}
	pathValue := envValueInCommand(t, got, "PATH")
	if !strings.Contains(pathValue, string(os.PathListSeparator)+binDir) {
		t.Fatalf("PATH did not preserve original path: %q", pathValue)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pathValue, string(os.PathListSeparator))[0], "git")); err != nil {
		t.Fatalf("git shim missing: %v", err)
	}
	bashEnv := envValueInCommand(t, got, "BASH_ENV")
	bashEnvContent, err := os.ReadFile(bashEnv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bashEnvContent), "export PATH=") || !strings.Contains(string(bashEnvContent), "${PATH:+:$PATH}") {
		t.Fatalf("BASH_ENV does not re-prepend shim PATH: %q", bashEnvContent)
	}
}

func TestProxyRunCodexAppliesCommandOutputFirstEnvForScopedRun(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() { osExecutable = oldExecutable })
	binDir := t.TempDir()
	self := filepath.Join(binDir, "slimference")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	gitBin := filepath.Join(binDir, "git")
	if err := os.WriteFile(gitBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	osExecutable = func() (string, error) { return self, nil }
	t.Setenv("PATH", binDir)

	env, _, stderr, _, _, _ := newProxyEnv(t)
	var gotName string
	var gotArgs []string
	env.RunCommand = func(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}
	if rc := proxyRun([]string{"run", "codex", "--proxied-wss", "--", "exec", "hi"}, env); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if gotName != "env" {
		t.Fatalf("runner name=%q", gotName)
	}
	joined := strings.Join(gotArgs, "\x00")
	if !strings.Contains(joined, commandOutputFirstActiveEnv+"=1") || !strings.Contains(joined, "BASH_ENV=") {
		t.Fatalf("scoped run missing command-output-first env: %#v", gotArgs)
	}
	if !strings.Contains(joined, "\x00codex\x00") {
		t.Fatalf("codex command missing: %#v", gotArgs)
	}
}

func TestCommandOutputFirstEnvNotInjectedForDirectRun(t *testing.T) {
	command := codexEnvCommand("direct", "127.0.0.1", "8990", []string{"exec", "hi"})
	got, cleanup := maybeApplyCommandOutputFirstEnv("direct", command)
	defer cleanup()
	joined := strings.Join(got, "\x00")
	if strings.Contains(joined, commandOutputFirstActiveEnv+"=1") || strings.Contains(joined, "BASH_ENV=") {
		t.Fatalf("direct mode must not get command-output-first env: %#v", got)
	}
}

func TestCommandOutputFirstEnvDisabledAndPrepareFailure(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() { osExecutable = oldExecutable })
	command := codexEnvCommand("proxied-wss", "127.0.0.1", "8990", []string{"exec", "hi"})

	t.Setenv(commandOutputFirstDisableEnv, "1")
	got, cleanup := maybeApplyCommandOutputFirstEnv("proxied-wss", command)
	cleanup()
	if strings.Contains(strings.Join(got, "\x00"), commandOutputFirstActiveEnv+"=1") {
		t.Fatalf("disabled env still injected: %#v", got)
	}

	t.Setenv(commandOutputFirstDisableEnv, "0")
	osExecutable = func() (string, error) { return "", errors.New("missing executable") }
	got, cleanup = maybeApplyCommandOutputFirstEnv("proxied-wss", command)
	cleanup()
	if strings.Contains(strings.Join(got, "\x00"), commandOutputFirstActiveEnv+"=1") {
		t.Fatalf("prepare failure still injected: %#v", got)
	}
}

func TestPrepareCommandOutputFirstEnvNoGit(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() { osExecutable = oldExecutable })
	self := filepath.Join(t.TempDir(), "slimference")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	osExecutable = func() (string, error) { return self, nil }
	t.Setenv("PATH", t.TempDir())
	env, cleanup, ok := prepareCommandOutputFirstEnv()
	defer cleanup()
	if ok || len(env) != 0 {
		t.Fatalf("expected no env without git, got ok=%v env=%#v", ok, env)
	}
}

func TestWriteCommandOutputFirstShimWriteError(t *testing.T) {
	err := writeCommandOutputFirstShim(filepath.Join(t.TempDir(), "missing", "git"), "/bin/slimference", "/usr/bin/git", "git")
	if err == nil {
		t.Fatal("expected write error for missing parent directory")
	}
}

func TestInsertEnvAssignmentsBeforeUtilityEdges(t *testing.T) {
	empty := insertEnvAssignmentsBeforeUtility(nil, "A=1")
	if len(empty) != 0 {
		t.Fatalf("empty command changed to %#v", empty)
	}
	unchanged := insertEnvAssignmentsBeforeUtility([]string{"env", "codex"})
	if strings.Join(unchanged, " ") != "env codex" {
		t.Fatalf("unexpected unchanged command %#v", unchanged)
	}
	got := insertEnvAssignmentsBeforeUtility([]string{"env", "-u", "HTTP_PROXY", "NO_PROXY=*", "codex"}, "A=1")
	if strings.Join(got, " ") != "env -u HTTP_PROXY NO_PROXY=* A=1 codex" {
		t.Fatalf("unexpected insertion %#v", got)
	}
}

func TestHandleCommandOutputFirstShimUsesExitFn(t *testing.T) {
	oldExit := exitFn
	defer func() { exitFn = oldExit }()
	var gotCode int
	exitFn = func(code int) { gotCode = code }
	handleCommandOutputFirstShim([]string{"--bad"})
	if gotCode != 2 {
		t.Fatalf("exit code=%d", gotCode)
	}
}

func writeFakeGit(t *testing.T, script string) string {
	t.Helper()
	return writeFakeCommand(t, "git", script)
}

func writeFakeCommand(t *testing.T, name string, script string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func envValueInCommand(t *testing.T, command []string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, arg := range command {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	t.Fatalf("%s not found in %#v", key, command)
	return ""
}

func TestCommandOutputFirstShimRejectsBadArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 2 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestCommandOutputFirstShimRejectsUnknownArgAndMissingRequired(t *testing.T) {
	for _, args := range [][]string{
		{"--wat"},
		{"--command=git", "--"},
		{"--real-bin=/bin/git", "--"},
	} {
		var stdout, stderr bytes.Buffer
		rc := runCommandOutputFirstShim(args, &bytes.Buffer{}, &stdout, &stderr)
		if rc != 2 {
			t.Fatalf("args=%v rc=%d stderr=%q", args, rc, stderr.String())
		}
	}
}

func TestCommandOutputFirstShimNonZeroFullPassesWithoutRecoveryNoise(t *testing.T) {
	realGit := writeFakeGit(t, `#!/bin/sh
printf 'fatal: not a git repository\n' >&2
exit 128
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "status", "--short"}, io.Reader(&bytes.Buffer{}), &stdout, &stderr)
	if rc != 128 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if got := stderr.String(); got != "fatal: not a git repository\n" {
		t.Fatalf("stderr=%q", got)
	}
}

func TestCommandOutputFirstCompactRejectsWrongCommandAndUnknownSubcommand(t *testing.T) {
	if out, ok := compactCommandOutputFirst("rg", "/usr/bin/rg", []string{"TODO"}, nil, nil, 0); ok || out != nil {
		t.Fatalf("non-git compacted: out=%q ok=%v", out, ok)
	}
	if out, ok := compactCommandOutputFirst("git", "/usr/bin/git", []string{"show", "HEAD"}, []byte("commit x\n"), nil, 0); ok || out != nil {
		t.Fatalf("git show compacted: out=%q ok=%v", out, ok)
	}
}

func TestCommandOutputFirstGitSubcommandGlobalOptionEdges(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"-C"}, want: ""},
		{args: []string{"-C", "/repo", "status"}, want: "status"},
		{args: []string{"--git-dir"}, want: ""},
		{args: []string{"--git-dir=/repo/.git", "diff", "--stat"}, want: "diff"},
		{args: []string{"-c", "core.quotePath=false", "ls-files"}, want: "ls-files"},
		{args: []string{"-cfoo=bar", "status"}, want: "status"},
		{args: []string{"--no-pager", "status"}, want: "status"},
		{args: []string{""}, want: ""},
	}
	for _, tc := range cases {
		if got := commandOutputFirstGitSubcommand(tc.args); got != tc.want {
			t.Fatalf("args=%v got=%q want=%q", tc.args, got, tc.want)
		}
	}
}

func TestCommandExitCodeStartError(t *testing.T) {
	if got := commandExitCode(errors.New("start failed")); got != 127 {
		t.Fatalf("exit code=%d", got)
	}
}

func TestCommandOutputFirstPassthroughMissingBinaryReturns127(t *testing.T) {
	if rc := execCommandOutputFirstPassthrough(filepath.Join(t.TempDir(), "missing-git"), []string{"show"}); rc != 127 {
		t.Fatalf("rc=%d", rc)
	}
}

func withCommandOutputFirstRecordingDB(t *testing.T) string {
	t.Helper()
	oldPath := resolveFilterDBPathFn
	oldGetwd := osGetwd
	dbPath := filepath.Join(t.TempDir(), "filter.db")
	resolveFilterDBPathFn = func() (string, error) { return dbPath, nil }
	osGetwd = func() (string, error) { return "/repo", nil }
	t.Cleanup(func() {
		resolveFilterDBPathFn = oldPath
		osGetwd = oldGetwd
	})
	return dbPath
}

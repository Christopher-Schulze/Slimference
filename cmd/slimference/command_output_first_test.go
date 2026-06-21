package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/readcache"
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

func TestCommandOutputFirstShimGitStatusTinyDirtyFullPasses(t *testing.T) {
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

func TestCommandOutputFirstShimGitStatusLargeDirtyCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, " M internal/generated/very/deep/path/file_%03d.go\n", i)
	}
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "?? internal/generated/very/deep/path/new_%03d.go\n", i)
	}
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "status", "--short"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[git status] 200 paths") ||
		!strings.Contains(got, "worktree:120") ||
		!strings.Contains(got, "untracked:80") {
		t.Fatalf("unexpected compacted stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand git status archive: %v", err)
	}
	if !bytes.Contains(raw, []byte(" M internal/generated/very/deep/path/file_119.go")) ||
		!bytes.Contains(raw, []byte("?? internal/generated/very/deep/path/new_079.go")) {
		t.Fatalf("archive did not preserve raw git status output: %q", raw)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	run, ok, err := filter.LastFilterRun(db)
	if err != nil || !ok {
		t.Fatalf("missing git status accounting row: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(run.Command, "[command-output-first:git] git status --short") || run.SavingsPct <= 0 {
		t.Fatalf("bad git status accounting row: %+v", run)
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

func TestCommandOutputFirstShimGitShowMetadataCompacts(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	b.WriteString("commit a1b2c3d4e5f6a7b8\n")
	b.WriteString("Author: Alice <alice@example.com>\n")
	b.WriteString("Date:   Mon Apr 7 10:30:00 2025 +0000\n\n")
	b.WriteString("    Metadata-only show\n\n")
	for i := 0; i < 40; i++ {
		b.WriteString(" internal/proxy/generated/very/deep/path/file_")
		b.WriteString(fmt.Sprintf("%02d.go | %d +++++-----\n", i, i+1))
	}
	b.WriteString(" 40 files changed, 820 insertions(+), 410 deletions(-)\n")
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "show", "--stat", "HEAD"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[git show] a1b2c3d Metadata-only show") ||
		!strings.Contains(got, "[git show --stat] 40 file(s)") ||
		!strings.Contains(got, "[prefix=internal/proxy/generated/very/deep/path/]") {
		t.Fatalf("unexpected compacted stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand git show archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("file_39.go | 40 +++++-----")) {
		t.Fatalf("archive did not preserve raw git show output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:git] git show --stat HEAD") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimGitLogStatCompacts(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for commit := 0; commit < 16; commit++ {
		b.WriteString("commit ")
		b.WriteString(fmt.Sprintf("%040x", commit+1))
		b.WriteString("\n")
		b.WriteString("Author: Dev <dev@example.com>\n")
		b.WriteString("Date:   Mon Apr 7 10:30:00 2025 +0000\n\n")
		b.WriteString("    Tighten command-output-first metadata path ")
		b.WriteString(strconv.Itoa(commit))
		b.WriteString("\n\n")
		for i := 0; i < 8; i++ {
			b.WriteString(" internal/proxy/generated/history/deep/path/commit_")
			b.WriteString(fmt.Sprintf("%02d", commit))
			b.WriteString("_file_")
			b.WriteString(fmt.Sprintf("%02d.go | %d +++++-----\n", i, i+1))
		}
		b.WriteString(" 8 files changed, 64 insertions(+), 32 deletions(-)\n\n")
	}
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "-C", "/repo", "log", "--stat", "--max-count=16", "--", "internal/proxy"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[git log] 16 commit(s)") ||
		!strings.Contains(got, "Tighten command-output-first metadata path 15") ||
		!strings.Contains(got, "[8 file(s), +64/-32]") {
		t.Fatalf("unexpected compacted stdout=%q", got)
	}
	if strings.Contains(got, "internal/proxy/generated/history/deep/path/commit_15_file_07.go") {
		t.Fatalf("git log --stat command-output-first should elide repeated stat path rows: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand git log archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("internal/proxy/generated/history/deep/path/commit_15_file_07.go")) {
		t.Fatalf("archive did not preserve raw git log output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:git] git -C /repo log --stat --max-count=16 -- internal/proxy") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimGitLogNameOnlyCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	b.WriteString("commit a1b2c3d4e5f6a7b8\n")
	b.WriteString("Author: Alice <alice@example.com>\n")
	b.WriteString("Date:   Mon Apr 7 10:30:00 2025 +0000\n\n")
	b.WriteString("    Feature branch sweep\n\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "internal/proxy/generated/very/deep/path/file_%02d.go\n", i)
	}
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "log", "--name-only", "--max-count=1"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[git log --name-only] 1 commit(s)") ||
		!strings.Contains(got, "a1b2c3d Feature branch sweep") ||
		!strings.Contains(got, "internal/proxy/generated/very/deep/path/") ||
		strings.Contains(got, "internal/proxy/generated/very/deep/path/file_79.go") {
		t.Fatalf("unexpected compacted stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand git log name-only archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("internal/proxy/generated/very/deep/path/file_79.go")) {
		t.Fatalf("archive did not preserve raw git log name-only output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:git] git log --name-only --max-count=1") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
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

func TestCommandOutputFirstShimLocatePathListCompacts(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 0; i < 48; i++ {
		b.WriteString("/Users/christopher/CODE/Slimference/internal/proxy/generated/deep/path/file_")
		b.WriteString(fmt.Sprintf("%02d.go\n", i))
	}
	realPlocate := writeFakeCommand(t, "plocate", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=plocate", "--real-bin=" + realPlocate, "--", "generated"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[plocate paths]") ||
		!strings.Contains(got, "/Users/christopher/CODE/Slimference/internal/proxy/generated/deep/path/") ||
		!strings.Contains(got, "file_47.go") {
		t.Fatalf("unexpected compacted stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand plocate archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("file_47.go")) {
		t.Fatalf("archive did not preserve raw plocate output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:plocate] plocate generated") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimLsLongCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var raw strings.Builder
	raw.WriteString("total 320\n")
	for i := 0; i < 48; i++ {
		fmt.Fprintf(&raw, "-rw-r--r--  1 user staff 4096 Jan 01 00:%02d generated_file_%02d.go\n", i%60, i)
	}
	realLs := writeFakeCommand(t, "ls", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=ls", "--real-bin=" + realLs, "--", "-lah", "generated"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[ls -l] 48 entries total 320 owner=user group=staff") ||
		!strings.Contains(got, "generated_file_47.go") ||
		strings.Contains(got, "user staff 4096") {
		t.Fatalf("unexpected ls command-output-first output: %q", got[:min(len(got), 260)])
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("generated_file_47.go")) || !bytes.Contains(archived, []byte("user staff 4096")) {
		t.Fatalf("archive did not preserve raw ls output: %q", archived[:min(len(archived), 220)])
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
	if !strings.Contains(run.Command, "[command-output-first:ls] ls -lah generated") {
		t.Fatalf("command label=%q", run.Command)
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
	if !commandOutputFirstAllowCapture("git", []string{"show", "--stat", "HEAD"}) {
		t.Fatal("git show --stat should be captured")
	}
	if !commandOutputFirstAllowCapture("git", []string{"-C", "/repo", "show", "--name-only", "HEAD"}) {
		t.Fatal("git -C repo show --name-only should be captured")
	}
	if !commandOutputFirstAllowCapture("git", []string{"show", "--name-status", "HEAD"}) {
		t.Fatal("git show --name-status should be captured")
	}
	if commandOutputFirstAllowCapture("git", []string{"show", "--stat", "--patch", "HEAD"}) {
		t.Fatal("git show --stat --patch must not be captured")
	}
	if commandOutputFirstAllowCapture("git", []string{"show", "--name-only", "--name-status", "HEAD"}) {
		t.Fatal("git show with multiple metadata modes must not be captured")
	}
	if commandOutputFirstAllowCapture("git", []string{"log"}) {
		t.Fatal("plain git log must not be captured")
	}
	if !commandOutputFirstAllowCapture("git", []string{"log", "--stat", "--max-count=20"}) {
		t.Fatal("git log --stat should be captured")
	}
	if !commandOutputFirstAllowCapture("git", []string{"-C", "/repo", "log", "--stat=80", "--", "internal"}) {
		t.Fatal("git -C repo log --stat pathspec should be captured")
	}
	if commandOutputFirstAllowCapture("git", []string{"log", "--stat", "--patch"}) {
		t.Fatal("git log --stat --patch must not be captured")
	}
	if !commandOutputFirstAllowCapture("git", []string{"log", "--name-only", "--max-count=20"}) {
		t.Fatal("git log --name-only should be captured")
	}
	if !commandOutputFirstAllowCapture("git", []string{"log", "--name-status", "--diff-filter=M", "--", "internal"}) {
		t.Fatal("git log --name-status should be captured")
	}
	if commandOutputFirstAllowCapture("git", []string{"log", "--name-only", "--name-status"}) {
		t.Fatal("git log with multiple metadata path-list modes must not be captured")
	}
	if commandOutputFirstAllowCapture("git", []string{"log", "--stat", "--format=oneline"}) {
		t.Fatal("git log custom format must not be captured by the default-header reducer")
	}
	if !commandOutputFirstAllowCapture("git", []string{"grep", "-n", "TODO", "--", "internal"}) {
		t.Fatal("git grep should be captured by the command-output-first shim")
	}
	if !commandOutputFirstAllowCapture("rg", []string{"TODO"}) {
		t.Fatal("rg should be captured by the command-output-first shim")
	}
	if !commandOutputFirstAllowCapture("rg", []string{"--files", "internal"}) {
		t.Fatal("rg --files should be captured by the command-output-first shim")
	}
	if !commandOutputFirstAllowCapture("ls", []string{"-lah", "internal"}) {
		t.Fatal("ls -lah should be captured by the command-output-first shim")
	}
	if commandOutputFirstAllowCapture("ls", []string{"internal"}) {
		t.Fatal("plain ls must not be captured by the command-output-first shim")
	}
	if commandOutputFirstAllowCapture("ls", []string{"-lR", "internal"}) {
		t.Fatal("recursive ls must not be captured by the command-output-first shim")
	}
	if commandOutputFirstAllowCapture("ls", []string{"-la@", "internal"}) {
		t.Fatal("extended-attribute ls must not be captured by the command-output-first shim")
	}
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{command: "grep", args: []string{"-R", "-n", "TODO", "internal"}},
		{command: "ggrep", args: []string{"-R", "-n", "TODO", "internal"}},
		{command: "ag", args: []string{"-n", "TODO", "internal"}},
		{command: "ack", args: []string{"-n", "TODO", "internal"}},
		{command: "ugrep", args: []string{"-n", "TODO", "internal"}},
		{command: "sift", args: []string{"-n", "TODO", "internal"}},
	} {
		if !commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v should be captured by the command-output-first shim", tc.command, tc.args)
		}
	}
	if !commandOutputFirstAllowCapture("go", []string{"test", "-v", "./..."}) {
		t.Fatal("go test should be captured by the command-output-first shim")
	}
	if !commandOutputFirstAllowCapture("go", []string{"-C", "/repo", "build", "./cmd/slimference"}) {
		t.Fatal("go -C repo build should be captured by the command-output-first shim")
	}
	if commandOutputFirstAllowCapture("go", []string{"env"}) {
		t.Fatal("go env must not be captured by the command-output-first shim")
	}
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{command: "npm", args: []string{"test", "--", "--runInBand"}},
		{command: "npm", args: []string{"run", "test"}},
		{command: "npm", args: []string{"install"}},
		{command: "npm", args: []string{"ci", "--loglevel", "warn"}},
		{command: "npm", args: []string{"audit", "--json"}},
		{command: "pnpm", args: []string{"run", "test"}},
		{command: "pnpm", args: []string{"install", "--reporter=append-only"}},
		{command: "pnpm", args: []string{"audit", "--json=1"}},
		{command: "yarn", args: []string{"run", "test"}},
		{command: "yarn", args: []string{"install", "--non-interactive"}},
		{command: "bun", args: []string{"test"}},
		{command: "bun", args: []string{"install", "--ignore-scripts"}},
		{command: "npm", args: []string{"run", "build"}},
		{command: "pnpm", args: []string{"run", "build"}},
		{command: "yarn", args: []string{"run", "build"}},
		{command: "cargo", args: []string{"test", "--", "--nocapture"}},
		{command: "cargo", args: []string{"+nightly", "nextest", "run"}},
		{command: "cargo", args: []string{"check", "--workspace"}},
		{command: "cargo", args: []string{"clippy", "--all-targets"}},
		{command: "cargo", args: []string{"fetch"}},
		{command: "cargo", args: []string{"update"}},
		{command: "pytest", args: []string{"-vv"}},
		{command: "python3", args: []string{"-m", "pytest", "-vv"}},
		{command: "python", args: []string{"-u", "-m", "unittest"}},
		{command: "uv", args: []string{"run", "pytest", "-vv"}},
		{command: "uv", args: []string{"sync"}},
		{command: "uv", args: []string{"pip", "install", "pytest"}},
		{command: "poetry", args: []string{"install"}},
		{command: "poetry", args: []string{"run", "python", "-m", "pytest"}},
		{command: "pip", args: []string{"install", "requests"}},
		{command: "pip3", args: []string{"install", "requests"}},
		{command: "fd", args: []string{"--extension", "go", "internal"}},
		{command: "fdfind", args: []string{"-e", "go", "internal"}},
		{command: "find", args: []string{"internal", "-maxdepth", "4", "-type", "f"}},
		{command: "wc", args: []string{"-l", "cmd/slimference/command_output_first.go"}},
		{command: "npx", args: []string{"-y", "next", "build"}},
		{command: "make", args: []string{"-j8"}},
		{command: "cmake", args: []string{"--build", "build", "--parallel"}},
		{command: "tsc", args: []string{"--noEmit"}},
		{command: "next", args: []string{"build"}},
		{command: "vite", args: []string{"build"}},
		{command: "webpack", args: []string{"--mode", "production"}},
		{command: "webpack-cli", args: []string{"--mode", "production"}},
		{command: "pre-commit", args: []string{"run", "--all-files"}},
		{command: "staticcheck", args: []string{"./..."}},
		{command: "errcheck", args: []string{"./..."}},
		{command: "gocyclo", args: []string{"-over", "12", "."}},
		{command: "ruff", args: []string{"check", "."}},
		{command: "pyright", args: []string{"--outputjson", "src"}},
		{command: "stylelint", args: []string{"--formatter", "json", "**/*.css"}},
		{command: "eslint", args: []string{"src", "--format", "stylish"}},
		{command: "biome", args: []string{"check", "."}},
		{command: "deno", args: []string{"lint", "."}},
		{command: "shellcheck", args: []string{"scripts/build.sh"}},
		{command: "markdownlint", args: []string{"docs"}},
		{command: "python3", args: []string{"-m", "pylint", "src"}},
		{command: "python", args: []string{"-u", "-m", "flake8", "src"}},
		{command: "buf", args: []string{"lint"}},
		{command: "gocritic", args: []string{"check", "./..."}},
		{command: "prettier", args: []string{"--check", "."}},
		{command: "gofmt", args: []string{"-l", "."}},
		{command: "go", args: []string{"fmt", "./..."}},
		{command: "go", args: []string{"vet", "./..."}},
		{command: "rustfmt", args: []string{"--check", "src/lib.rs"}},
		{command: "black", args: []string{"--check", "src/"}},
		{command: "isort", args: []string{"--check-only", "src/"}},
		{command: "clang-format", args: []string{"--dry-run", "src/app.cc"}},
		{command: "npm", args: []string{"run", "lint"}},
		{command: "pnpm", args: []string{"run", "typecheck"}},
		{command: "yarn", args: []string{"run", "format:check"}},
		{command: "bun", args: []string{"run", "build"}},
		{command: "tsup", args: []string{"src/index.ts"}},
		{command: "rspack", args: []string{"build"}},
		{command: "parcel", args: []string{"build", "src/index.html"}},
		{command: "rollup", args: []string{"-c"}},
		{command: "esbuild", args: []string{"src/index.ts", "--bundle"}},
		{command: "nx", args: []string{"build", "web"}},
		{command: "turbo", args: []string{"run", "build"}},
		{command: "mvn", args: []string{"test"}},
		{command: "mvnw", args: []string{"-q", "verify"}},
		{command: "docker", args: []string{"ps"}},
		{command: "docker", args: []string{"images", "--quiet"}},
		{command: "podman", args: []string{"ps", "-q"}},
		{command: "nerdctl", args: []string{"images"}},
		{command: "docker", args: []string{"compose", "ps", "-q"}},
		{command: "docker-compose", args: []string{"ps", "-q"}},
		{command: "kubectl", args: []string{"get", "pods", "-n", "default"}},
		{command: "kubectl", args: []string{"get", "pods", "-o", "json"}},
		{command: "oc", args: []string{"get", "pods"}},
		{command: "helm", args: []string{"list", "-q"}},
		{command: "helm", args: []string{"search", "repo", "slimference"}},
		{command: "terraform", args: []string{"plan", "-no-color"}},
		{command: "tofu", args: []string{"init"}},
		{command: "tf", args: []string{"show", "-json"}},
		{command: "gh", args: []string{"api", "/repos/acme/project"}},
		{command: "gh", args: []string{"pr", "list", "--json", "number,title"}},
		{command: "glab", args: []string{"pipeline", "list"}},
		{command: "aws", args: []string{"sts", "get-caller-identity"}},
		{command: "jq", args: []string{".", "package.json"}},
		{command: "curl", args: []string{"-sS", "https://api.example.com/data"}},
		{command: "wget", args: []string{"-qO-", "https://api.example.com/data"}},
		{command: "http", args: []string{"GET", "https://api.example.com/data"}},
		{command: "https", args: []string{"api.example.com/data"}},
		{command: "psql", args: []string{"-c", "select * from users"}},
		{command: "psql", args: []string{"--command=select 1"}},
		{command: "mysql", args: []string{"-e", "select * from users"}},
		{command: "mariadb", args: []string{"--execute=select 1"}},
		{command: "sqlite3", args: []string{"db.sqlite", "select * from users"}},
		{command: "sqlite", args: []string{"-readonly", "db.sqlite", "select 1"}},
		{command: "duckdb", args: []string{"-c", "select * from users"}},
		{command: "duckdb", args: []string{"db.duckdb", "select 1"}},
		{command: "gradle", args: []string{"build"}},
		{command: "gradlew", args: []string{"build", "--parallel"}},
		{command: "meson", args: []string{"compile", "-C", "build"}},
		{command: "zig", args: []string{"build"}},
		{command: "wasm-pack", args: []string{"build"}},
		{command: "bazel", args: []string{"build", "//..."}},
		{command: "swift", args: []string{"build"}},
		{command: "buf", args: []string{"build"}},
		{command: "ko", args: []string{"build", "./cmd/app"}},
		{command: "moon", args: []string{"run", "web:build"}},
		{command: "pack", args: []string{"build", "app"}},
		{command: "vitest", args: []string{"run"}},
		{command: "jest", args: []string{"--runInBand"}},
		{command: "mocha", args: []string{"test/**/*.spec.js"}},
		{command: "playwright", args: []string{"test"}},
		{command: "cypress", args: []string{"run", "--headless"}},
		{command: "wdio", args: []string{"run", "wdio.conf.ts"}},
		{command: "nx", args: []string{"test", "web"}},
		{command: "turbo", args: []string{"run", "test"}},
		{command: "deno", args: []string{"test", "--allow-all"}},
		{command: "npx", args: []string{"-y", "vitest", "run"}},
		{command: "nox", args: []string{"-s", "test"}},
		{command: "tox", args: []string{"-e", "test"}},
		{command: "ruby", args: []string{"-I", "test", "test/models/user_test.rb"}},
		{command: "bundle", args: []string{"exec", "rspec", "spec"}},
		{command: "bundle", args: []string{"exec", "ruby", "-I", "test", "test/models/user_test.rb"}},
		{command: "bundle", args: []string{"install", "--jobs", "4", "--retry=2"}},
		{command: "bundle", args: []string{"update", "rails"}},
		{command: "pipenv", args: []string{"install", "--dev"}},
		{command: "composer", args: []string{"install", "--no-interaction"}},
		{command: "mix", args: []string{"deps.get", "--only", "test"}},
		{command: "gem", args: []string{"install", "rake", "--no-document"}},
		{command: "rake", args: []string{"spec"}},
		{command: "rails", args: []string{"test"}},
		{command: "gradle", args: []string{"test"}},
		{command: "sbt", args: []string{"test"}},
		{command: "mill", args: []string{"foo.test"}},
	} {
		if !commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v should be captured", tc.command, tc.args)
		}
	}
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{command: "npm", args: []string{"run", "dev"}},
		{command: "npm", args: []string{"install", "--json"}},
		{command: "npm", args: []string{"ci", "--loglevel"}},
		{command: "npm", args: []string{"ci", "--loglevel=verbose"}},
		{command: "npm", args: []string{"update", "-d"}},
		{command: "npm", args: []string{"audit"}},
		{command: "npm", args: []string{"audit", "--json=false"}},
		{command: "pnpm", args: []string{"install", "--reporter"}},
		{command: "pnpm", args: []string{"install", "--reporter", "ndjson"}},
		{command: "pnpm", args: []string{"update", "--json"}},
		{command: "pnpm", args: []string{"exec", "vitest"}},
		{command: "yarn", args: []string{"start"}},
		{command: "yarn", args: []string{"install", "--json"}},
		{command: "bun", args: []string{"install", "--dry-run"}},
		{command: "cargo", args: []string{"install", "ripgrep"}},
		{command: "cargo", args: []string{"nextest", "list"}},
		{command: "python3", args: []string{"script.py"}},
		{command: "python3", args: []string{"-m", "http.server"}},
		{command: "uv", args: []string{"run", "python", "script.py"}},
		{command: "ruby", args: []string{"-e", "puts 1"}},
		{command: "ruby", args: []string{"script.rb"}},
		{command: "bundle", args: []string{"exec", "ruby", "script.rb"}},
		{command: "bundle", args: []string{"exec", "rake", "db:migrate"}},
		{command: "bundle", args: []string{"exec", "rails", "server"}},
		{command: "bundle", args: []string{"install", "--verbose"}},
		{command: "pipenv", args: []string{"run", "pytest"}},
		{command: "pipenv", args: []string{"install", "--verbose"}},
		{command: "composer", args: []string{"update"}},
		{command: "composer", args: []string{"install", "-vvv"}},
		{command: "mix", args: []string{"test"}},
		{command: "mix", args: []string{"deps.get", "--debug"}},
		{command: "gem", args: []string{"update", "rake"}},
		{command: "gem", args: []string{"install", "rake", "--debug"}},
		{command: "find", args: []string{"internal", "-type", "f"}},
		{command: "find", args: []string{"internal", "-maxdepth", "4", "-delete"}},
		{command: "fd", args: []string{"--exec", "rm", "{}"}},
		{command: "wc", args: []string{"-l"}},
		{command: "wc", args: []string{"--files0-from=list"}},
		{command: "npx", args: []string{"-c", "next build"}},
		{command: "npx", args: []string{"cowsay", "hello"}},
		{command: "make", args: []string{"-n"}},
		{command: "cmake", args: []string{"-S", ".", "-B", "build"}},
		{command: "tsc", args: []string{"--watch"}},
		{command: "next", args: []string{"dev"}},
		{command: "vite", args: []string{"--host", "127.0.0.1"}},
		{command: "webpack", args: []string{"serve"}},
		{command: "prettier", args: []string{"--write", "."}},
		{command: "ruff", args: []string{"format", "."}},
		{command: "gofmt", args: []string{"-w", "."}},
		{command: "gofmt", args: []string{"."}},
		{command: "rustfmt", args: []string{"src/lib.rs"}},
		{command: "black", args: []string{"src/"}},
		{command: "isort", args: []string{"src/"}},
		{command: "clang-format", args: []string{"src/app.cc"}},
		{command: "biome", args: []string{"ci", "."}},
		{command: "buf", args: []string{"format", "-w"}},
		{command: "gocritic", args: []string{"doc"}},
		{command: "python3", args: []string{"-m", "pip", "list"}},
		{command: "npm", args: []string{"run", "dev"}},
		{command: "yarn", args: []string{"run", "format"}},
		{command: "playwright", args: []string{"codegen"}},
		{command: "cypress", args: []string{"open"}},
		{command: "deno", args: []string{"run", "script.ts"}},
		{command: "nox", args: []string{"-s", "lint"}},
		{command: "tox", args: []string{"-e", "lint"}},
		{command: "rake", args: []string{"db:migrate"}},
		{command: "rails", args: []string{"server"}},
		{command: "gradle", args: []string{"assemble"}},
		{command: "sbt", args: []string{"compile"}},
		{command: "mill", args: []string{"foo.compile"}},
		{command: "tsup", args: []string{"--watch"}},
		{command: "rspack", args: []string{"serve"}},
		{command: "parcel", args: []string{"watch", "src/index.html"}},
		{command: "rollup", args: []string{"--watch", "-c"}},
		{command: "esbuild", args: []string{"src/index.ts"}},
		{command: "mvn", args: []string{"deploy"}},
		{command: "mvn", args: []string{"site"}},
		{command: "docker", args: []string{"run", "nginx"}},
		{command: "docker", args: []string{"compose", "logs"}},
		{command: "docker", args: []string{"compose", "up"}},
		{command: "kubectl", args: []string{"describe", "pod", "web"}},
		{command: "kubectl", args: []string{"get", "pods", "--watch"}},
		{command: "kubectl", args: []string{"get", "pods", "-w"}},
		{command: "oc", args: []string{"get", "pods", "--watch-only"}},
		{command: "helm", args: []string{"install", "web", "repo/web"}},
		{command: "helm", args: []string{"upgrade", "web", "repo/web"}},
		{command: "terraform", args: []string{"apply", "-auto-approve"}},
		{command: "terraform", args: []string{"destroy"}},
		{command: "terraform", args: []string{"import", "aws_s3_bucket.main", "id"}},
		{command: "gh", args: []string{"pr", "view", "1"}},
		{command: "glab", args: []string{"mr", "view", "1"}},
		{command: "aws", args: []string{"configure"}},
		{command: "aws", args: []string{"sso", "login"}},
		{command: "aws", args: []string{"ecr", "get-login-password"}},
		{command: "curl", args: []string{"-o", "body.json", "https://api.example.com/data"}},
		{command: "curl", args: []string{"--no-buffer", "https://api.example.com/events"}},
		{command: "wget", args: []string{"https://api.example.com/data"}},
		{command: "http", args: []string{"--download", "https://api.example.com/data"}},
		{command: "psql", args: nil},
		{command: "psql", args: []string{"-c"}},
		{command: "psql", args: []string{"db"}},
		{command: "mysql", args: nil},
		{command: "mysql", args: []string{"-e"}},
		{command: "mariadb", args: []string{"--execute="}},
		{command: "sqlite3", args: nil},
		{command: "sqlite3", args: []string{"db.sqlite"}},
		{command: "sqlite3", args: []string{"-cmd", "select 1", "db.sqlite"}},
		{command: "duckdb", args: nil},
		{command: "duckdb", args: []string{"db.duckdb"}},
		{command: "duckdb", args: []string{"-c"}},
		{command: "gradle", args: []string{"assemble"}},
		{command: "meson", args: []string{"setup", "build"}},
		{command: "moon", args: []string{"run", "web:test"}},
	} {
		if commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v must not be captured", tc.command, tc.args)
		}
	}
}

func TestCommandOutputFirstStructuredDiagnosticAllowed(t *testing.T) {
	allowed := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "go build", command: "go", args: []string{"build", "./..."}},
		{name: "go vet", command: "go", args: []string{"vet", "./..."}},
		{name: "cargo check", command: "cargo", args: []string{"check", "--workspace"}},
		{name: "python module lint", command: "python3", args: []string{"-m", "pylint", "src"}},
		{name: "python sqlfluff lint", command: "python", args: []string{"-m", "sqlfluff", "lint", "q.sql"}},
		{name: "package script", command: "pnpm", args: []string{"run", "build"}},
		{name: "npx direct build", command: "npx", args: []string{"-y", "tsc", "--noEmit"}},
		{name: "direct build", command: "tsc", args: []string{"--noEmit"}},
		{name: "direct lint", command: "shellcheck", args: []string{"scripts/build.sh"}},
		{name: "direct test", command: "vitest", args: []string{"run"}},
		{name: "direct format check", command: "prettier", args: []string{"--check", "."}},
	}
	for _, tc := range allowed {
		t.Run("allow "+tc.name, func(t *testing.T) {
			if !commandOutputFirstStructuredDiagnosticAllowed(tc.command, tc.args) {
				t.Fatalf("%s %v should allow structured diagnostics", tc.command, tc.args)
			}
		})
	}

	denied := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "search no match", command: "rg", args: []string{"needle"}},
		{name: "go env", command: "go", args: []string{"env", "-json"}},
		{name: "cargo test", command: "cargo", args: []string{"test"}},
		{name: "python non lint", command: "python3", args: []string{"-m", "pip", "list"}},
		{name: "unknown npx", command: "npx", args: []string{"cowsay", "hello"}},
		{name: "network", command: "curl", args: []string{"https://example.com"}},
		{name: "unknown direct", command: "custom-tool", args: []string{"run"}},
	}
	for _, tc := range denied {
		t.Run("deny "+tc.name, func(t *testing.T) {
			if commandOutputFirstStructuredDiagnosticAllowed(tc.command, tc.args) {
				t.Fatalf("%s %v must not allow structured diagnostics", tc.command, tc.args)
			}
		})
	}
}

func TestCommandOutputFirstShimSARIFNonzeroCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var results strings.Builder
	for i := 0; i < 80; i++ {
		if i > 0 {
			results.WriteString(",")
		}
		fmt.Fprintf(&results, `{"ruleId":"no-generated-%02d","level":"warning","message":{"text":"generated diagnostic number %02d with repeated context payload repeated context payload repeated context payload"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"src/generated/deep/path/file_%02d.ts"},"region":{"startLine":%d,"startColumn":7}}}]}`, i, i, i, i+10)
	}
	sarif := `{"$schema":"https://json.schemastore.org/sarif-2.1.0.json","version":"2.1.0","runs":[{"tool":{"driver":{"name":"eslint","version":"9.0.0"}},"results":[` + results.String() + `]}]}`
	realEslint := writeFakeCommand(t, "eslint", "#!/bin/sh\ncat <<'EOF'\n"+sarif+"\nEOF\nexit 1\n")

	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=eslint", "--real-bin=" + realEslint, "--", "--format", "sarif", "src"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should stay empty: %q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[sarif: eslint] 80 result(s)") ||
		!strings.Contains(got, "src/generated/deep/path/file_00.ts:10:7 warning [no-generated-00]") {
		t.Fatalf("unexpected SARIF command-output-first stdout=%q", got)
	}
	if strings.Contains(got, `"runs"`) || strings.Contains(got, `"ruleId"`) {
		t.Fatalf("visible SARIF output should be compacted, not raw JSON payload: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand SARIF command-output-first archive: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"ruleId":"no-generated-79"`)) {
		t.Fatalf("archive did not preserve raw SARIF tail result: %q", raw[:min(len(raw), 260)])
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
	if !strings.Contains(run.Command, "[command-output-first:eslint] eslint --format sarif src") ||
		run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("bad SARIF accounting row: %+v", run)
	}
}

func TestCommandOutputFirstPythonModuleLintAllowed(t *testing.T) {
	allowed := [][]string{
		{"-m", "pylint", "src"},
		{"-u", "-m", "flake8", "src"},
		{"-m", "bandit", "-r", "src"},
		{"-m", "semgrep", "--config", "auto"},
		{"-m", "djlint", "templates"},
		{"-m", "yamllint", "."},
		{"-m", "sqlfluff", "lint", "q.sql"},
	}
	for _, args := range allowed {
		if !commandOutputFirstPythonModuleLintAllowed("python3", args) {
			t.Fatalf("python3 %v should allow module lint", args)
		}
	}
	denied := [][]string{
		{"-m", "sqlfluff", "fix", "q.sql"},
		{"-m", "pip", "list"},
		{"script.py"},
	}
	for _, args := range denied {
		if commandOutputFirstPythonModuleLintAllowed("python3", args) {
			t.Fatalf("python3 %v must not allow module lint", args)
		}
	}
	if commandOutputFirstPythonModuleLintAllowed("uv", []string{"-m", "pylint", "src"}) {
		t.Fatal("non-python command must not allow python module lint")
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

func TestCommandOutputFirstShimGrepStyleSearchCompactsWithFullRetentionAndAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 1; i <= 45; i++ {
		b.WriteString("internal/proxy/long/path/handler.go:")
		b.WriteString(strconv.Itoa(100 + i))
		b.WriteString(":func handleGrepStyleResult() { return nil } // repeated grep payload ")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	realGrep := writeFakeCommand(t, "grep", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=grep", "--real-bin=" + realGrep, "--", "-R", "-n", "handleGrepStyleResult", "internal/proxy"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[grep] 45 match(es) in 1 file(s)") ||
		!strings.Contains(got, "internal/proxy/long/path/handler.go") ||
		!strings.Contains(got, "145: func handleGrepStyleResult() { return nil } // repeated grep payload 45") {
		t.Fatalf("unexpected compacted grep stdout=%q", got)
	}
	if strings.Contains(got, "[+") {
		t.Fatalf("grep command-output-first must retain every match in product slice: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing grep archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand grep archive: %v", err)
	}
	if bytes.Count(raw, []byte("handleGrepStyleResult")) != 45 {
		t.Fatalf("archive did not preserve grep raw output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:grep] grep -R -n handleGrepStyleResult internal/proxy") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimGrepStyleContextModeFullPasses(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	raw := "internal/app/app.go-10-before\ninternal/app/app.go:11:match\ninternal/app/app.go-12-after\n"
	realGrep := writeFakeCommand(t, "grep", "#!/bin/sh\ncat <<'EOF'\n"+raw+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=grep", "--real-bin=" + realGrep, "--", "-R", "-n", "-C2", "match", "internal/app"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != raw {
		t.Fatalf("context-mode grep must full-pass raw stdout\ngot=%q\nwant=%q", got, raw)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("context-mode full-pass must not archive: %q", stdout.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if run, ok, err := filter.LastFilterRun(db); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("context-mode full-pass must not record accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimRgArchivedCompactsLongMatchContent(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	// Single-file search output (line:content format, no file prefix).
	// The standard grouping with 100% retention full-passes because the
	// grouped header overhead exceeds the savings from stripping the file
	// prefix. The archived variant truncates content and caps matches,
	// producing a smaller summary that is recoverable via the archive.
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		b.WriteString(strconv.Itoa(i))
		b.WriteString(":func handler")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("() { return someVeryLongPayloadThatExceedsTheTruncationLimitAndShouldBeCutOffSoThatTheArchivedSummaryIsSmallerThanTheOriginal() } // extra padding ")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	rawOutput := b.String()
	realRg := writeFakeCommand(t, "rg", "#!/bin/sh\ncat <<'EOF'\n"+rawOutput+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=rg", "--real-bin=" + realRg, "--", "-n", "func", "src/handler.go"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[rg] 30 match(es) in 1 file(s)") {
		t.Fatalf("expected archived rg summary: %q", got)
	}
	if !strings.Contains(got, "src/handler.go") {
		t.Fatalf("expected file path in archived summary: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("expected truncation marker in archived summary: %q", got)
	}
	// Verify archive marker is present and raw output is recoverable.
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing rg archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand rg archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("handler30")) {
		t.Fatalf("archive did not preserve rg raw output: %q", raw)
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
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimGrepArchivedCompactsLongMatchContent(t *testing.T) {
	// Single-file search output (line:content format, no file prefix).
	// The standard grouping full-passes; the archived variant truncates.
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		b.WriteString(strconv.Itoa(i))
		b.WriteString(":func handler")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("() { return someVeryLongPayloadThatExceedsTheTruncationLimitAndShouldBeCutOffSoThatTheArchivedSummaryIsSmallerThanTheOriginal() }\n")
	}
	rawOutput := b.String()
	realGrep := writeFakeCommand(t, "grep", "#!/bin/sh\ncat <<'EOF'\n"+rawOutput+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=grep", "--real-bin=" + realGrep, "--", "-n", "func", "src/handler.go"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[grep] 30 match(es) in 1 file(s)") {
		t.Fatalf("expected archived grep summary: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("expected truncation marker in archived summary: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing grep archive marker in %q", stdout.String())
	}
}

func TestCommandOutputFirstShimRgFilesCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 0; i < 48; i++ {
		b.WriteString("internal/filter/generated/deep/file_")
		if i < 10 {
			b.WriteByte('0')
		}
		b.WriteString(strings.TrimSpace(string(rune('0' + i/10))))
		b.WriteString(strings.TrimSpace(string(rune('0' + i%10))))
		b.WriteString(".go\n")
	}
	realRg := writeFakeCommand(t, "rg", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=rg", "--real-bin=" + realRg, "--", "--files", "-g", "*.go", "internal/filter"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[rg --files paths]") || !strings.Contains(got, "internal/filter/generated/deep/") || !strings.Contains(got, "file_47.go") {
		t.Fatalf("unexpected compacted rg --files stdout=%q", got)
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
	if !strings.Contains(run.Command, "[command-output-first:rg] rg --files -g *.go internal/filter") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFindAndWcCompact(t *testing.T) {
	var paths strings.Builder
	for i := 0; i < 44; i++ {
		paths.WriteString("docs/todo/generated/task_")
		if i < 10 {
			paths.WriteByte('0')
		}
		paths.WriteString(strings.TrimSpace(string(rune('0' + i/10))))
		paths.WriteString(strings.TrimSpace(string(rune('0' + i%10))))
		paths.WriteString(".md\n")
	}
	realFind := writeFakeCommand(t, "find", "#!/bin/sh\ncat <<'EOF'\n"+paths.String()+"EOF\n")
	var findStdout, findStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=find", "--real-bin=" + realFind, "--", "docs/todo", "-maxdepth", "4", "-type", "f"}, &bytes.Buffer{}, &findStdout, &findStderr)
	if rc != 0 {
		t.Fatalf("find rc=%d stderr=%q", rc, findStderr.String())
	}
	findOut := findStdout.String()
	if !strings.Contains(findOut, "[find paths]") || !strings.Contains(findOut, "docs/todo/generated/") || !strings.Contains(findOut, "task_43.md") {
		t.Fatalf("unexpected compacted find stdout=%q", findOut)
	}

	var wcRows strings.Builder
	totalLines := 0
	totalWords := 0
	for i := 0; i < 40; i++ {
		lines := 30 + i
		words := 90 + i
		totalLines += lines
		totalWords += words
		wcRows.WriteString("      ")
		wcRows.WriteString(strconv.Itoa(lines))
		wcRows.WriteString("      ")
		wcRows.WriteString(strconv.Itoa(words))
		wcRows.WriteString(" src/file")
		wcRows.WriteString(strconv.Itoa(i))
		wcRows.WriteString(".go\n")
	}
	wcRows.WriteString("      ")
	wcRows.WriteString(strconv.Itoa(totalLines))
	wcRows.WriteString("      ")
	wcRows.WriteString(strconv.Itoa(totalWords))
	wcRows.WriteString(" total\n")
	realWc := writeFakeCommand(t, "wc", "#!/bin/sh\ncat <<'EOF'\n"+wcRows.String()+"EOF\n")
	var wcStdout, wcStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=wc", "--real-bin=" + realWc, "--", "-lw", "src/main.go", "src/lib.go"}, &bytes.Buffer{}, &wcStdout, &wcStderr)
	if rc != 0 {
		t.Fatalf("wc rc=%d stderr=%q", rc, wcStderr.String())
	}
	gotWc := commandOutputFirstVisibleOutput(wcStdout.String())
	if !strings.Contains(gotWc, "[wc prefix=src/]") || !strings.Contains(gotWc, "file39.go: 69L 129W") || !strings.Contains(gotWc, "total: ") {
		t.Fatalf("unexpected compacted wc stdout=%q", gotWc)
	}
}

func TestCommandOutputFirstShimGitGrepCompactsWithFullRetentionAndAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 1; i <= 34; i++ {
		b.WriteString("internal/proxy/long/path/handler.go:")
		b.WriteString("42")
		b.WriteString(":func handleGitGrepResult() { return nil } // repeated search payload\n")
	}
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "grep", "-n", "handleGitGrepResult", "--", "internal/proxy"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[git grep] 34 match(es) in 1 file(s)") || !strings.Contains(got, "internal/proxy/long/path/handler.go") {
		t.Fatalf("unexpected compacted git grep stdout=%q", got)
	}
	if strings.Contains(got, "[+") {
		t.Fatalf("git grep command-output-first must retain every match in first product slice: %q", got)
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
	if !strings.Contains(run.Command, "[command-output-first:git] git grep -n handleGitGrepResult -- internal/proxy") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimGitGrepNoMatchFullPasses(t *testing.T) {
	realGit := writeFakeGit(t, "#!/bin/sh\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "grep", "-n", "missing"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommandOutputFirstShimGoTestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		b.WriteString("=== RUN   TestFeature")
		b.WriteString(strings.Repeat("0", len("60")-len("1")))
		b.WriteString("1\n")
		b.WriteString("--- PASS: TestFeature")
		b.WriteString(strings.Repeat("0", len("60")-len("1")))
		b.WriteString("1 (0.00s)\n")
	}
	b.WriteString("PASS\nok  \tgithub.com/example/project/internal/feature\t0.123s\n")
	realGo := writeFakeCommand(t, "go", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=go", "--real-bin=" + realGo, "--", "test", "-v", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[go test] ok - 60 passed") || !strings.Contains(got, "github.com/example/project/internal/feature") {
		t.Fatalf("unexpected compacted go test stdout=%q", got)
	}
	if strings.Contains(got, "=== RUN") || strings.Contains(got, "--- PASS:") {
		t.Fatalf("go test command-output-first should elide redundant pass roll-call: %q", got)
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
	if !strings.Contains(run.Command, "[command-output-first:go] go test -v ./...") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimGoBuildEmptyFullPasses(t *testing.T) {
	realGo := writeFakeCommand(t, "go", "#!/bin/sh\nexit 0\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=go", "--real-bin=" + realGo, "--", "build", "./cmd/slimference"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommandOutputFirstShimNpmRunTestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	b.WriteString("bun test v1.3.14 (0d9b296a)\n\nsession.test.ts:\n")
	for i := 1; i <= 42; i++ {
		b.WriteString("(pass) sample_session.jsonl > case ")
		if i < 10 {
			b.WriteString("00")
		} else if i < 100 {
			b.WriteString("0")
		}
		b.WriteString(strings.TrimSpace(string(rune('0' + i/10))))
		b.WriteString(strings.TrimSpace(string(rune('0' + i%10))))
		b.WriteString(" [0.01ms]\n")
	}
	b.WriteString("\n 42 pass\n 0 fail\n 50 expect() calls\nRan 42 tests across 2 files. [3.01s]\n")
	realNpm := writeFakeCommand(t, "npm", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npm", "--real-bin=" + realNpm, "--", "--silent", "run", "test"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[bun test] ok - 42 passed") || !strings.Contains(got, "Ran 42 tests across 2 files.") {
		t.Fatalf("unexpected compacted npm test stdout=%q", got)
	}
	if strings.Contains(got, "case 001") {
		t.Fatalf("npm test command-output-first should elide redundant pass roll-call: %q", got)
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
	if !strings.Contains(run.Command, "[command-output-first:npm] npm --silent run test") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimPackageBuildEmptyFullPasses(t *testing.T) {
	realPnpm := writeFakeCommand(t, "pnpm", "#!/bin/sh\nexit 0\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=pnpm", "--real-bin=" + realPnpm, "--", "run", "build"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommandOutputFirstShimPackageInstallAndAuditCompactWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	auditJSON := `{"auditReportVersion":2,"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0}},"vulnerabilities":{},"advisories":{},"actions":[],"scanNoise":"` + strings.Repeat("x", 512) + `"}`
	realNpm := writeFakeCommand(t, "npm", `#!/bin/sh
if [ "$1" = "install" ]; then
  cat <<'EOF'
`+commandOutputFirstNpmInstallFixture(90)+`EOF
  exit 0
fi
if [ "$1" = "audit" ]; then
  cat <<'EOF'
`+auditJSON+`
EOF
  exit 0
fi
echo "unexpected $*" >&2
exit 2
`)

	var installStdout, installStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npm", "--real-bin=" + realNpm, "--", "install"}, &bytes.Buffer{}, &installStdout, &installStderr)
	if rc != 0 {
		t.Fatalf("install rc=%d stderr=%q", rc, installStderr.String())
	}
	installVisible := commandOutputFirstVisibleOutput(installStdout.String())
	for _, want := range []string{
		"[npm install] added 90 packages",
		"audited 91 packages",
		"funding 45 packages",
		"0 vulnerabilities",
	} {
		if !strings.Contains(installVisible, want) {
			t.Fatalf("npm install compact output missing %q in %q", want, installVisible)
		}
	}
	if strings.Contains(installVisible, "package_089") {
		t.Fatalf("npm install command-output-first should elide fetch/timing roll-call: %q", installVisible)
	}
	installURI := commandOutputFirstArchiveURI(installStdout.String())
	if installURI == "" {
		t.Fatalf("missing npm install archive marker in %q", installStdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, installRaw, err := contentarchive.Get(contentarchive.DefaultDir(home), installURI)
	if err != nil {
		t.Fatalf("expand npm install archive: %v", err)
	}
	if !bytes.Contains(installRaw, []byte("package_089")) || !bytes.Contains(installRaw, []byte("found 0 vulnerabilities")) {
		t.Fatalf("archive did not preserve npm install raw output: %q", installRaw)
	}

	var auditStdout, auditStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=npm", "--real-bin=" + realNpm, "--", "audit", "--json"}, &bytes.Buffer{}, &auditStdout, &auditStderr)
	if rc != 0 {
		t.Fatalf("audit rc=%d stderr=%q", rc, auditStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(auditStdout.String()); got != "[npm audit] 0 vulnerabilities\n" {
		t.Fatalf("unexpected npm audit compact output=%q", got)
	}
	auditURI := commandOutputFirstArchiveURI(auditStdout.String())
	if auditURI == "" {
		t.Fatalf("missing npm audit archive marker in %q", auditStdout.String())
	}
	_, auditRaw, err := contentarchive.Get(contentarchive.DefaultDir(home), auditURI)
	if err != nil {
		t.Fatalf("expand npm audit archive: %v", err)
	}
	if !bytes.Contains(auditRaw, []byte(`"critical":0`)) {
		t.Fatalf("archive did not preserve npm audit raw output: %q", auditRaw)
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
	if !strings.Contains(run.Command, "[command-output-first:npm] npm audit --json") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimPackageInstallWarningFullPasses(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	raw := "npm WARN deprecated left-pad@1.3.0: use String.prototype.padStart()\n" + commandOutputFirstNpmInstallFixture(3)
	realNpm := writeFakeCommand(t, "npm", "#!/bin/sh\ncat <<'EOF'\n"+raw+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npm", "--real-bin=" + realNpm, "--", "install"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != raw {
		t.Fatalf("warning install must full-pass raw stdout\ngot=%q\nwant=%q", got, raw)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("warning full-pass must not archive: %q", stdout.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if run, ok, err := filter.LastFilterRun(db); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("warning full-pass must not record accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimNpxNextBuildCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realNpx := writeFakeCommand(t, "npx", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstNextBuildFixture()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npx", "--real-bin=" + realNpx, "--", "-y", "next", "build"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[next build] ok\n" {
		t.Fatalf("unexpected compacted npx next stdout=%q", got)
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
	if !strings.Contains(run.Command, "[command-output-first:npx] npx -y next build") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimDirectVitestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realVitest := writeFakeCommand(t, "vitest", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstJSTestFixture(36)+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=vitest", "--real-bin=" + realVitest, "--", "run"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[vitest] ok - 36 passed") || strings.Contains(got, "renders op 035") {
		t.Fatalf("unexpected compacted vitest stdout=%q", got)
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
	if !strings.Contains(run.Command, "[command-output-first:vitest] vitest run") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimMavenBuildCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realMvn := writeFakeCommand(t, "mvn", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstMavenFixture(24)+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=mvn", "--real-bin=" + realMvn, "--", "test"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[mvn] ok (Tests run: 42, Failures: 0, Errors: 0, Skipped: 0)\n" {
		t.Fatalf("unexpected compacted maven stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("[INFO] BUILD SUCCESS")) || !bytes.Contains(raw, []byte("Tests run: 42")) {
		t.Fatalf("archive did not preserve Maven raw output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:mvn] mvn test") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstArchiveMarkerIsCompactAndRecoverable(t *testing.T) {
	uri := "local-archive://0123456789abcdef0123456789abcdef"
	stdoutMarker := commandOutputFirstArchiveMarker(uri, "stdout")
	if !strings.Contains(stdoutMarker, uri) || !strings.Contains(stdoutMarker, "recover: slimference expand URI") {
		t.Fatalf("stdout marker lost recovery affordance: %q", stdoutMarker)
	}
	if strings.Count(stdoutMarker, uri) != 1 {
		t.Fatalf("stdout marker must not duplicate archive URI: %q", stdoutMarker)
	}
	if strings.Contains(stdoutMarker, "context-archive") || strings.Contains(stdoutMarker, "kind=tool-output") {
		t.Fatalf("stdout marker kept old high-overhead wording: %q", stdoutMarker)
	}
	if got := commandOutputFirstArchiveURI("compact\n" + stdoutMarker); got != uri {
		t.Fatalf("stdout marker archive URI got %q want %q", got, uri)
	}
	stderrMarker := commandOutputFirstArchiveMarker(uri, "stderr")
	if !strings.Contains(stderrMarker, "stream=stderr") || !strings.Contains(stderrMarker, "recover: slimference expand URI") {
		t.Fatalf("stderr marker lost stream or recovery affordance: %q", stderrMarker)
	}
	if strings.Count(stderrMarker, uri) != 1 {
		t.Fatalf("stderr marker must not duplicate archive URI: %q", stderrMarker)
	}
	if got := commandOutputFirstArchiveURI("compact\n" + stderrMarker); got != uri {
		t.Fatalf("stderr marker archive URI got %q want %q", got, uri)
	}
}

func TestCommandOutputFirstShimArchiveUnavailableFullPasses(t *testing.T) {
	oldHome := osUserHomeDir
	oldPath := resolveFilterDBPathFn
	oldGetwd := osGetwd
	dbPath := filepath.Join(t.TempDir(), "filter.db")
	osUserHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
	resolveFilterDBPathFn = func() (string, error) { return dbPath, nil }
	osGetwd = func() (string, error) { return "/repo", nil }
	t.Cleanup(func() {
		osUserHomeDir = oldHome
		resolveFilterDBPathFn = oldPath
		osGetwd = oldGetwd
	})

	dir := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	realMvn := filepath.Join(dir, "mvn")
	raw := commandOutputFirstMavenFixture(24)
	if err := os.WriteFile(realMvn, []byte("#!/bin/sh\ncat <<'EOF'\n"+raw+"EOF\n"), 0755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=mvn", "--real-bin=" + realMvn, "--", "test"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != raw {
		t.Fatalf("archive-unavailable path must full-pass raw stdout\ngot=%q\nwant=%q", got, raw)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := filter.QueryFilterObservationByCommand(db, commandOutputFirstObservationScope, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Outcome != commandOutputFirstObservationArchiveFail || !strings.Contains(rows[0].Command, "mvn test") {
		t.Fatalf("archive unavailable observation rows=%+v", rows)
	}
}

func TestCommandOutputFirstShimFullPassRecordsOpportunity(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	dir := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	realLs := filepath.Join(dir, "ls")
	raw := strings.Repeat("not-a-long-ls-row with enough bytes to survive the observation threshold\n", 80)
	if err := os.WriteFile(realLs, []byte("#!/bin/sh\ncat <<'EOF'\n"+raw+"EOF\n"), 0755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=ls", "--real-bin=" + realLs, "--", "-lah", "generated"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.String() != raw {
		t.Fatalf("full-pass stdout changed\ngot=%q\nwant=%q", stdout.String(), raw)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := filter.QueryFilterObservationByCommand(db, commandOutputFirstObservationScope, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Outcome != commandOutputFirstObservationFullPass || !strings.Contains(rows[0].Command, "ls -lah generated") {
		t.Fatalf("full-pass observation rows=%+v", rows)
	}
	if rows[0].InputTokens < commandOutputFirstObservationMinTokens {
		t.Fatalf("observation below threshold: %+v", rows[0])
	}
}

func TestCommandOutputFirstShimGradleBuildCompacts(t *testing.T) {
	realGradle := writeFakeCommand(t, "gradle", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstGradleBuildFixture(18)+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=gradle", "--real-bin=" + realGradle, "--", "build", "--parallel"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[gradle build] ok (18 actionable tasks: 18 executed)\n" {
		t.Fatalf("unexpected compacted gradle stdout=%q", got)
	}
}

func TestCommandOutputFirstShimDotnetBuildCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	raw := strings.Join([]string{
		"Microsoft (R) Build Engine version 17.8.0",
		"  Determining projects to restore...",
		"  All projects are up-to-date for restore.",
		"  App -> /repo/App/bin/Debug/net8.0/App.dll",
		"",
		"Build succeeded.",
		"    0 Warning(s)",
		"    0 Error(s)",
		"",
		"Time Elapsed 00:00:03.21",
	}, "\n") + "\n"
	realDotnet := writeFakeCommand(t, "dotnet", "#!/bin/sh\ncat <<'EOF'\n"+raw+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=dotnet", "--real-bin=" + realDotnet, "--", "build", "--configuration", "Release"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[dotnet build] ok\n" {
		t.Fatalf("unexpected compacted dotnet stdout=%q", got)
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
	if !strings.Contains(run.Command, "[command-output-first:dotnet] dotnet build --configuration Release") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimDotnetTestCompactsWithArchive(t *testing.T) {
	var raw strings.Builder
	raw.WriteString("Test run for /repo/App.Tests/bin/Debug/net8.0/App.Tests.dll (.NETCoreApp,Version=v8.0)\n")
	raw.WriteString("VSTest version 17.10.0 (arm64)\n\n")
	raw.WriteString("Starting test execution, please wait...\n")
	raw.WriteString("A total of 1 test files matched the specified pattern.\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&raw, "  Passed App.Tests.WidgetTests.Case%03d [1 ms]\n", i)
	}
	raw.WriteString("Passed!  - Failed:     0, Passed:    80, Skipped:     0, Total:    80, Duration: 1 s - App.Tests.dll (net8.0)\n")
	realDotnet := writeFakeCommand(t, "dotnet", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=dotnet", "--real-bin=" + realDotnet, "--", "test", "--logger", "console;verbosity=detailed"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[dotnet test] ok (80 passed, 0 skipped, 80 total across 1 assembly(s))") ||
		strings.Contains(got, "WidgetTests.Case000") {
		t.Fatalf("unexpected compacted dotnet test stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("WidgetTests.Case079")) ||
		!bytes.Contains(archived, []byte("Passed!  - Failed:     0, Passed:    80")) {
		t.Fatalf("archive did not preserve raw dotnet output: %q", archived)
	}
}

func TestCommandOutputFirstShimRspecCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var raw strings.Builder
	raw.WriteString("Randomized with seed 12345\n\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&raw, "Widget feature example %03d emits a very noisy success line that should not enter model context\n", i)
	}
	raw.WriteString("\nFinished in 2.3 seconds (files took 1.1 seconds to load)\n")
	raw.WriteString("120 examples, 0 failures\n")
	realRspec := writeFakeCommand(t, "rspec", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=rspec", "--real-bin=" + realRspec, "--", "spec"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if got != "[rspec] ok (120 examples, 0 failures)\n" {
		t.Fatalf("unexpected compacted rspec stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("Widget feature example 119 emits")) ||
		!bytes.Contains(archived, []byte("120 examples, 0 failures")) {
		t.Fatalf("archive did not preserve raw rspec output: %q", archived)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil || !ok {
		t.Fatalf("missing rspec accounting row: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(run.Command, "[command-output-first:rspec] rspec spec") ||
		run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("bad rspec accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimBundleExecRspecNonzeroCompactsWithArchive(t *testing.T) {
	var raw strings.Builder
	raw.WriteString("Randomized with seed 999\n")
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&raw, "intermediate success noise line %03d that should be archived instead of forwarded\n", i)
	}
	raw.WriteString("\nFailures:\n\n")
	raw.WriteString("  1) Widget does the important thing\n")
	raw.WriteString("     Failure/Error: expect(result).to eq(:ok)\n")
	raw.WriteString("       expected: :ok\n")
	raw.WriteString("            got: :bad\n")
	raw.WriteString("     # ./spec/widget_spec.rb:42:in `block (2 levels) in <top (required)>'\n\n")
	raw.WriteString("Finished in 1.2 seconds (files took 0.8 seconds to load)\n")
	raw.WriteString("91 examples, 1 failure\n")
	realBundle := writeFakeCommand(t, "bundle", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=bundle", "--real-bin=" + realBundle, "--", "exec", "rspec", "spec/widget_spec.rb"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "Failures:") ||
		!strings.Contains(got, "Widget does the important thing") ||
		!strings.Contains(got, "91 examples, 1 failure") ||
		strings.Contains(got, "intermediate success noise line 089") {
		t.Fatalf("unexpected compacted bundle rspec stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("intermediate success noise line 089")) ||
		!bytes.Contains(archived, []byte("91 examples, 1 failure")) {
		t.Fatalf("archive did not preserve raw bundle rspec output: %q", archived)
	}
}

func TestCommandOutputFirstShimRubyMinitestCompactsWithArchive(t *testing.T) {
	var raw strings.Builder
	raw.WriteString("Run options: --seed 4242\n\n")
	raw.WriteString("# Running:\n\n")
	for i := 0; i < 8; i++ {
		raw.WriteString(strings.Repeat(".", 80))
		raw.WriteByte('\n')
	}
	raw.WriteString("\nFinished in 0.123456s, 5184.0 runs/s, 10368.0 assertions/s.\n")
	raw.WriteString("640 runs, 1280 assertions, 0 failures, 0 errors, 0 skips\n")
	realRuby := writeFakeCommand(t, "ruby", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=ruby", "--real-bin=" + realRuby, "--", "-I", "test", "test/models/widget_test.rb"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[minitest] ok - 640 runs, 1280 assertions, progress dots elided") ||
		strings.Contains(got, strings.Repeat(".", 80)) {
		t.Fatalf("unexpected compacted minitest stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(archived, []byte(strings.Repeat(".", 80))) ||
		!bytes.Contains(archived, []byte("640 runs, 1280 assertions")) {
		t.Fatalf("archive did not preserve raw minitest output: %q", archived)
	}
}

func TestCommandOutputFirstShimBundleInstallCompactsWithArchive(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&raw, "Fetching gem noisy_dependency_%03d 1.2.%d\n", i, i)
	}
	raw.WriteString("Bundle complete! 12 Gemfile dependencies, 101 gems now installed.\n")
	realBundle := writeFakeCommand(t, "bundle", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=bundle", "--real-bin=" + realBundle, "--", "install", "--jobs", "4"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "Bundle complete! 12 Gemfile dependencies") ||
		strings.Contains(got, "noisy_dependency_099") {
		t.Fatalf("unexpected compacted bundle install stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("noisy_dependency_099")) ||
		!bytes.Contains(archived, []byte("Bundle complete! 12 Gemfile dependencies")) {
		t.Fatalf("archive did not preserve raw bundle install output: %q", archived)
	}
}

func TestCommandOutputFirstShimPackageManagerFrontierCompactsWithArchive(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		args        []string
		raw         string
		wantVisible string
		wantArchive string
	}{
		{
			name:    "pipenv",
			command: "pipenv",
			args:    []string{"install", "--dev"},
			raw: strings.Repeat("Installing dependencies from Pipfile.lock (abc123)...\n", 80) +
				"All dependencies are now up-to-date!\n",
			wantVisible: "[pipenv install] ok (up to date)",
			wantArchive: "Pipfile.lock (abc123)",
		},
		{
			name:    "composer",
			command: "composer",
			args:    []string{"install", "--no-interaction"},
			raw: strings.Repeat("- Downloading symfony/console (v6.4.0)\n- Installing symfony/console (v6.4.0): Extracting archive\n", 40) +
				"Installing dependencies from lock file (including require-dev)\n" +
				"Verifying lock file contents can be installed on current platform.\n" +
				"Package operations: 40 installs, 0 updates, 0 removals\n" +
				"Generating autoload files\n" +
				"9 packages you are using are looking for funding.\n" +
				"Use the `composer fund` command to find out more!\n",
			wantVisible: "[composer install] ok (40 installs, 0 updates, 0 removals; autoload generated; funding 9 packages)",
			wantArchive: "Extracting archive",
		},
		{
			name:    "mix",
			command: "mix",
			args:    []string{"deps.get", "--only", "test"},
			raw: "Resolving Hex dependencies...\n" +
				"Resolution completed in 0.123s\n" +
				"Unchanged:\n" +
				func() string {
					var rows strings.Builder
					for i := 0; i < 80; i++ {
						fmt.Fprintf(&rows, "plug_%03d 1.14.%d\n", i, i%10)
					}
					return rows.String()
				}(),
			wantVisible: "[mix deps.get] ok (80 dependencies listed)",
			wantArchive: "plug_079 1.14.9",
		},
		{
			name:    "gem",
			command: "gem",
			args:    []string{"install", "rake", "--no-document"},
			raw: func() string {
				var rows strings.Builder
				for i := 0; i < 40; i++ {
					fmt.Fprintf(&rows, "Successfully installed package_%03d-1.0.%d\n", i, i%10)
					fmt.Fprintf(&rows, "Parsing documentation for package_%03d-1.0.%d\n", i, i%10)
				}
				rows.WriteString("Done installing documentation for rake after 0 seconds\n")
				rows.WriteString("40 gems installed\n")
				return rows.String()
			}(),
			wantVisible: "[gem install] ok (installed 40 gems; documentation installed)",
			wantArchive: "package_039-1.0.9",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := withCommandOutputFirstRecordingDB(t)
			realBin := writeFakeCommand(t, tc.command, "#!/bin/sh\ncat <<'EOF'\n"+tc.raw+"EOF\n")
			var stdout, stderr bytes.Buffer
			shimArgs := append([]string{"--command=" + tc.command, "--real-bin=" + realBin, "--"}, tc.args...)
			rc := runCommandOutputFirstShim(shimArgs, &bytes.Buffer{}, &stdout, &stderr)
			if rc != 0 {
				t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
			}
			got := commandOutputFirstVisibleOutput(stdout.String())
			if !strings.Contains(got, tc.wantVisible) || strings.Contains(got, tc.wantArchive) {
				t.Fatalf("unexpected compacted stdout=%q", got)
			}
			uri := commandOutputFirstArchiveURI(stdout.String())
			if uri == "" {
				t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
			}
			home, err := osUserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
			if err != nil {
				t.Fatalf("expand command-output-first archive: %v", err)
			}
			if !bytes.Contains(archived, []byte(tc.wantArchive)) {
				t.Fatalf("archive did not preserve raw %s output: %q", tc.name, archived)
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
			if !strings.Contains(run.Command, "[command-output-first:"+tc.command+"] "+tc.command) ||
				run.InputTokens <= run.OutputTokens ||
				run.SavingsPct <= 0 {
				t.Fatalf("bad %s accounting row: %+v", tc.name, run)
			}
		})
	}
}

func TestCommandOutputFirstDotnetEdges(t *testing.T) {
	for _, args := range [][]string{
		{"build"},
		{"publish", "--configuration", "Release"},
		{"pack", "--no-build"},
	} {
		if !commandOutputFirstDirectBuildAllowed("dotnet", args) {
			t.Fatalf("dotnet %v should be build-allowed", args)
		}
	}
	if !commandOutputFirstDirectTestAllowed("dotnet", []string{"test", "--logger", "console;verbosity=detailed"}) {
		t.Fatal("dotnet test should be test-allowed")
	}
	for _, args := range [][]string{
		{"restore"},
		{"run"},
		{"watch", "test"},
		{"build", "--watch"},
	} {
		if commandOutputFirstDirectBuildAllowed("dotnet", args) || commandOutputFirstDirectTestAllowed("dotnet", args) {
			t.Fatalf("dotnet %v must not be command-output-first allowed", args)
		}
	}
}

func TestCommandOutputFirstRubyBundleEdges(t *testing.T) {
	rubyAllowed := [][]string{
		{"test/models/user_test.rb"},
		{"./test/models/user_test.rb"},
		{"-w", "-Itest", "-rminitest/autorun", "test/models/user_test.rb"},
		{"--", "test/models/user_test.rb"},
	}
	for _, args := range rubyAllowed {
		if !commandOutputFirstRubyMinitestAllowed(args) {
			t.Fatalf("ruby %v should be minitest-allowed", args)
		}
	}

	rubyDenied := [][]string{
		nil,
		{""},
		{"-e", "puts 1"},
		{"--eval", "puts 1"},
		{"-"},
		{"--"},
		{"-I"},
		{"-C", ""},
		{"script.rb"},
		{"spec/models/user_spec.rb"},
	}
	for _, args := range rubyDenied {
		if commandOutputFirstRubyMinitestAllowed(args) {
			t.Fatalf("ruby %v must not be minitest-allowed", args)
		}
	}

	bundleAllowed := [][]string{
		{"exec", "rspec", "spec"},
		{"exec", "rake", "test"},
		{"exec", "rake", "spec"},
		{"exec", "rails", "test"},
		{"exec", "ruby", "-I", "test", "test/models/user_test.rb"},
	}
	for _, args := range bundleAllowed {
		if !commandOutputFirstBundleExecRubyTestAllowed(args) {
			t.Fatalf("bundle %v should be test-allowed", args)
		}
	}

	bundleDenied := [][]string{
		nil,
		{"exec"},
		{"rspec"},
		{"exec", "rake", "db:migrate"},
		{"exec", "rails", "server"},
		{"exec", "ruby", "script.rb"},
		{"exec", "unknown", "test"},
	}
	for _, args := range bundleDenied {
		if commandOutputFirstBundleExecRubyTestAllowed(args) {
			t.Fatalf("bundle %v must not be test-allowed", args)
		}
	}

	installAllowed := [][]string{
		nil,
		{"--jobs=4", "--retry", "2", "--path", "vendor/bundle"},
		{"--with", "development", "--without=production", "--gemfile", "Gemfile"},
	}
	for _, args := range installAllowed {
		if !commandOutputFirstBundleInstallArgsAllowed(args) {
			t.Fatalf("bundle install args %v should be allowed", args)
		}
	}

	installDenied := [][]string{
		{""},
		{"--verbose"},
		{"-v"},
		{"--jobs"},
		{"--gemfile", ""},
		{"--unknown"},
	}
	for _, args := range installDenied {
		if commandOutputFirstBundleInstallArgsAllowed(args) {
			t.Fatalf("bundle install args %v must not be allowed", args)
		}
	}

	diagnosticAllowed := []struct {
		command string
		args    []string
	}{
		{command: "rspec", args: []string{"spec"}},
		{command: "rake", args: []string{"spec"}},
		{command: "ruby", args: []string{"test/models/user_test.rb"}},
		{command: "bundle", args: []string{"exec", "rspec", "spec"}},
	}
	for _, tc := range diagnosticAllowed {
		if !commandOutputFirstRubyDiagnosticAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v should allow ruby diagnostics", tc.command, tc.args)
		}
	}
	diagnosticDenied := []struct {
		command string
		args    []string
	}{
		{command: "rake", args: []string{"db:migrate"}},
		{command: "ruby", args: []string{"script.rb"}},
		{command: "bundle", args: []string{"exec", "rails", "server"}},
		{command: "unknown", args: []string{"spec"}},
	}
	for _, tc := range diagnosticDenied {
		if commandOutputFirstRubyDiagnosticAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v must not allow ruby diagnostics", tc.command, tc.args)
		}
	}
}

func TestCommandOutputFirstShimContainerStatusCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var raw strings.Builder
	raw.WriteString("NAME                    READY   STATUS             RESTARTS   AGE\n")
	for i := 0; i < 36; i++ {
		status := "Running"
		if i == 7 {
			status = "CrashLoopBackOff"
		}
		if i == 23 {
			status = "ImagePullBackOff"
		}
		fmt.Fprintf(&raw, "api-%05d              1/1     %-18s 0          5d\n", i, status)
	}
	realKubectl := writeFakeCommand(t, "kubectl", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=kubectl", "--real-bin=" + realKubectl, "--", "get", "pods", "-n", "default"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	for _, want := range []string{
		"[kubectl get pods -n default] 36 item(s), 2 attention row(s)",
		"api-00007",
		"CrashLoopBackOff",
		"api-00023",
		"ImagePullBackOff",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("container compact output missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "api-00035") {
		t.Fatalf("healthy container rows should stay in archive, not visible compact output: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("api-00035")) ||
		!bytes.Contains(archived, []byte("ImagePullBackOff")) {
		t.Fatalf("archive did not preserve raw kubectl output: %q", archived)
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
	if !strings.Contains(run.Command, "[command-output-first:kubectl] kubectl get pods -n default") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimContainerQuietNonemptyFullPasses(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	raw := "abc123\n"
	realDocker := writeFakeCommand(t, "docker", "#!/bin/sh\nprintf 'abc123\\n'\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=docker", "--real-bin=" + realDocker, "--", "ps", "-q"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != raw {
		t.Fatalf("non-empty quiet output must full-pass\ngot=%q\nwant=%q", got, raw)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("non-compacted quiet output must not archive: %q", stdout.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if run, ok, err := filter.LastFilterRun(db); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("full-pass quiet output must not record accounting row: %+v", run)
	}
}

func TestCommandOutputFirstContainerStatusEdges(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{command: "docker", args: []string{"ps"}},
		{command: "docker", args: []string{"images", "-q"}},
		{command: "docker", args: []string{"compose", "ps", "--quiet"}},
		{command: "docker", args: []string{"compose", "ls"}},
		{command: "docker-compose", args: []string{"ps", "-q"}},
		{command: "podman", args: []string{"ps"}},
		{command: "nerdctl", args: []string{"images"}},
		{command: "kubectl", args: []string{"get", "pods"}},
		{command: "oc", args: []string{"get", "routes"}},
		{command: "helm", args: []string{"list", "--short"}},
		{command: "helm", args: []string{"search", "repo", "app"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstContainerStatusAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v should be container-status allowed", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "docker", args: []string{"logs", "web"}},
		{command: "docker", args: []string{"run", "nginx"}},
		{command: "docker", args: []string{"compose", "logs"}},
		{command: "docker", args: []string{"compose", "up"}},
		{command: "docker-compose", args: []string{"logs"}},
		{command: "kubectl", args: []string{"describe", "pod", "web"}},
		{command: "kubectl", args: []string{"get", "pods", "-w"}},
		{command: "oc", args: []string{"get", "pods", "--watch-only"}},
		{command: "helm", args: []string{"install", "web", "repo/web"}},
		{command: "helm", args: []string{"upgrade", "web", "repo/web"}},
		{command: "unknown", args: []string{"get", "pods"}},
		{command: "kubectl", args: nil},
	}
	for _, tc := range denied {
		if commandOutputFirstContainerStatusAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v must not be container-status allowed", tc.command, tc.args)
		}
	}
}

func TestCommandOutputFirstShimLogDuplicateCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realDocker := writeFakeCommand(t, "docker", `#!/bin/sh
if [ "$1" = "logs" ]; then
  for i in $(seq 1 70); do
    echo "2026-06-20T10:00:00Z INFO worker heartbeat id=alpha"
  done
  for i in $(seq 1 20); do
    echo "2026-06-20T10:00:01Z ERROR upstream refused request id=beta"
  done
  exit 0
fi
exit 2
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=docker", "--real-bin=" + realDocker, "--", "logs", "web"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	for _, want := range []string{"INFO worker heartbeat id=alpha [×70]", "ERROR upstream refused request id=beta [×20]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log duplicate compaction missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "more log line(s)") || strings.Contains(got, "local-archive://") {
		t.Fatalf("visible duplicate-only output should not truncate or expose marker in visible helper: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand docker logs archive: %v", err)
	}
	if bytes.Count(raw, []byte("INFO worker heartbeat")) != 70 ||
		bytes.Count(raw, []byte("ERROR upstream refused")) != 20 {
		t.Fatalf("archive did not preserve raw duplicate log output: %q", raw)
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
	if !ok || !strings.Contains(run.Command, "[command-output-first:docker] docker logs web") {
		t.Fatalf("expected docker logs accounting row, ok=%v run=%+v", ok, run)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimLogUniqueFullPasses(t *testing.T) {
	realKubectl := writeFakeCommand(t, "kubectl", `#!/bin/sh
if [ "$1" = "logs" ]; then
  echo "2026-06-20T10:00:00Z INFO first unique event"
  echo "2026-06-20T10:00:01Z ERROR second unique event"
  echo "2026-06-20T10:00:02Z WARN third unique event"
  exit 0
fi
exit 2
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=kubectl", "--real-bin=" + realKubectl, "--", "logs", "pod/web"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	want := "2026-06-20T10:00:00Z INFO first unique event\n" +
		"2026-06-20T10:00:01Z ERROR second unique event\n" +
		"2026-06-20T10:00:02Z WARN third unique event\n"
	if stdout.String() != want {
		t.Fatalf("unique logs must full-pass byte-identically:\nwant=%q\ngot=%q", want, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestCommandOutputFirstLogDuplicateAllowlistEdges(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{command: "docker", args: []string{"logs", "web"}},
		{command: "podman", args: []string{"logs", "--tail", "200", "web"}},
		{command: "nerdctl", args: []string{"logs", "web"}},
		{command: "kubectl", args: []string{"logs", "-n", "default", "pod/web"}},
		{command: "oc", args: []string{"logs", "pod/web"}},
		{command: "journalctl", args: []string{"-u", "slimference.service", "-n", "200"}},
		{command: "tail", args: []string{"-n", "200", "app.log"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v should be log-duplicate allowed", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "docker", args: []string{"logs", "-f", "web"}},
		{command: "docker", args: []string{"compose", "logs"}},
		{command: "docker-compose", args: []string{"logs", "web"}},
		{command: "kubectl", args: []string{"logs", "--follow", "pod/web"}},
		{command: "journalctl", args: []string{"--follow", "-u", "slimference.service"}},
		{command: "tail", args: []string{"-f", "app.log"}},
		{command: "tail", args: []string{"-n", "200", "notes.txt"}},
		{command: "cat", args: []string{"app.log"}},
	}
	for _, tc := range denied {
		if commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v must not be log-duplicate allowed", tc.command, tc.args)
		}
	}
	if !commandOutputFirstLogArgsFinite([]string{"--follow=false", "--", "web"}) {
		t.Fatal("--follow=false should be finite")
	}
	if !commandOutputFirstLogArgsFinite([]string{"--follow=0", "web"}) {
		t.Fatal("--follow=0 should be finite")
	}
	if commandOutputFirstLogArgsFinite([]string{"--follow=true", "web"}) {
		t.Fatal("--follow=true must not be finite")
	}
	if commandOutputFirstLogArgsFinite([]string{"web", ""}) {
		t.Fatal("empty log arg must fail closed")
	}
	if commandOutputFirstTailLogAllowed([]string{"-n", "20", "-v"}) {
		t.Fatal("tail target that is still an option must fail closed")
	}
}

func TestCommandOutputFirstShimTerraformPlanCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var raw strings.Builder
	raw.WriteString("Terraform will perform the following actions:\n\n")
	for i := 0; i < 36; i++ {
		fmt.Fprintf(&raw, "  # aws_s3_bucket.generated_%03d will be created\n", i)
		fmt.Fprintf(&raw, "  + resource \"aws_s3_bucket\" \"generated_%03d\" {\n", i)
		raw.WriteString("      + acl    = \"private\"\n")
		raw.WriteString("      + bucket = \"bucket-generated-name\"\n")
		raw.WriteString("      + tags   = {\n")
		raw.WriteString("          + \"Environment\" = \"prod\"\n")
		raw.WriteString("        }\n")
		raw.WriteString("    }\n\n")
	}
	raw.WriteString("Plan: 36 to add, 0 to change, 0 to destroy.\n")
	realTerraform := writeFakeCommand(t, "terraform", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=terraform", "--real-bin=" + realTerraform, "--", "plan", "-no-color"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	for _, want := range []string{
		"# aws_s3_bucket.generated_000 will be created",
		"resource \"aws_s3_bucket\" \"generated_000\"",
		"Plan: 36 to add, 0 to change, 0 to destroy.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("terraform compact output missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "bucket-generated-name") || strings.Contains(got, "Environment") {
		t.Fatalf("terraform attribute body should be archived, not visible compact output: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("bucket-generated-name")) ||
		!bytes.Contains(archived, []byte("aws_s3_bucket.generated_035")) {
		t.Fatalf("archive did not preserve raw terraform output: %q", archived)
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
	if !strings.Contains(run.Command, "[command-output-first:terraform] terraform plan -no-color") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimVCSHostJSONExactCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var raw strings.Builder
	raw.WriteString("{\n  \"items\": [\n")
	for i := 0; i < 80; i++ {
		comma := ","
		if i == 79 {
			comma = ""
		}
		fmt.Fprintf(&raw, "    {\"id\": %d, \"name\": \"release-%03d\", \"value\": \"%s\"}%s\n", i, i, strings.Repeat("abcdef", 4), comma)
	}
	raw.WriteString("  ]\n}\n")
	realGh := writeFakeCommand(t, "gh", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=gh", "--real-bin=" + realGh, "--", "api", "/repos/acme/project/releases"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if strings.Contains(got, "\n  ") {
		t.Fatalf("gh api JSON should be minified, got %q", got[:min(len(got), 160)])
	}
	for _, want := range []string{`"release-079"`, `"value":"abcdefabcdefabcdefabcdef"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("exact JSON compact output lost %q in %q", want, got[:min(len(got), 220)])
		}
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
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
	if !strings.Contains(run.Command, "[command-output-first:gh] gh api /repos/acme/project/releases") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimJQNonJSONFullPasses(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	raw := "plain\nplain\n"
	realJQ := writeFakeCommand(t, "jq", "#!/bin/sh\nprintf 'plain\\nplain\\n'\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=jq", "--real-bin=" + realJQ, "--", "-r", ".name", "package.json"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != raw {
		t.Fatalf("jq non-json must full-pass\ngot=%q\nwant=%q", got, raw)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("non-json full-pass must not archive: %q", stdout.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if run, ok, err := filter.LastFilterRun(db); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("jq non-json full-pass must not record accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimNetworkJSONExactCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var raw strings.Builder
	raw.WriteString("{\n  \"items\": [\n")
	for i := 0; i < 72; i++ {
		comma := ","
		if i == 71 {
			comma = ""
		}
		fmt.Fprintf(&raw, "    {\"id\": %d, \"name\": \"object-%03d\", \"value\": \"%s\"}%s\n", i, i, strings.Repeat("xyz", 8), comma)
	}
	raw.WriteString("  ]\n}\n")
	realCurl := writeFakeCommand(t, "curl", "#!/bin/sh\ncat <<'EOF'\n"+raw.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=curl", "--real-bin=" + realCurl, "--", "-sS", "https://api.example.com/data"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if strings.Contains(got, "\n  ") {
		t.Fatalf("curl JSON should be minified, got %q", got[:min(len(got), 160)])
	}
	for _, want := range []string{`"object-071"`, `"value":"xyzxyzxyzxyzxyzxyzxyzxyz"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("exact network JSON compact output lost %q in %q", want, got[:min(len(got), 220)])
		}
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("\n  \"items\"")) || !bytes.Contains(archived, []byte("object-071")) {
		t.Fatalf("archive did not preserve raw network response: %q", archived[:min(len(archived), 220)])
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
	if !strings.Contains(run.Command, "[command-output-first:curl] curl -sS https://api.example.com/data") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimNetworkNonJSONFullPasses(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	raw := "INFO boot\nINFO boot\nINFO boot\n"
	realHTTP := writeFakeCommand(t, "http", "#!/bin/sh\nprintf 'INFO boot\\nINFO boot\\nINFO boot\\n'\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=http", "--real-bin=" + realHTTP, "--", "GET", "https://api.example.com/logs"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != raw {
		t.Fatalf("http non-json must full-pass\ngot=%q\nwant=%q", got, raw)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("non-json full-pass must not archive: %q", stdout.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if run, ok, err := filter.LastFilterRun(db); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("network non-json full-pass must not record accounting row: %+v", run)
	}
}

func TestCommandOutputFirstNetworkResponseHelperBoundaries(t *testing.T) {
	allowed := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "curl url flag", command: "curl", args: []string{"--url", "https://api.example.com/data"}},
		{name: "curl request with data", command: "curl", args: []string{"-X", "POST", "-H", "content-type: application/json", "-d", "{\"ok\":true}", "https://api.example.com/data"}},
		{name: "wget split stdout", command: "wget", args: []string{"-q", "-O", "-", "https://api.example.com/data"}},
		{name: "wget compact stdout", command: "wget", args: []string{"-qO-", "https://api.example.com/data"}},
		{name: "wget long stdout", command: "wget", args: []string{"--output-document=-", "https://api.example.com/data"}},
		{name: "httpie localhost", command: "http", args: []string{"localhost:8990/status"}},
		{name: "httpie relative local", command: "http", args: []string{":8990/status"}},
		{name: "httpie dotted host", command: "https", args: []string{"api.example.com/data"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstNetworkResponseAllowed(tc.command, tc.args) {
			t.Fatalf("%s: %s %v should be network-response allowed", tc.name, tc.command, tc.args)
		}
	}

	denied := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "unknown command", command: "ftp", args: []string{"https://api.example.com/data"}},
		{name: "empty args", command: "curl", args: nil},
		{name: "curl blank arg", command: "curl", args: []string{""}},
		{name: "curl no url", command: "curl", args: []string{"-sS"}},
		{name: "curl missing header value", command: "curl", args: []string{"-H"}},
		{name: "curl output file", command: "curl", args: []string{"--output=body.json", "https://api.example.com/data"}},
		{name: "curl upload file", command: "curl", args: []string{"--upload-file=body.json", "https://api.example.com/data"}},
		{name: "curl include headers", command: "curl", args: []string{"-i", "https://api.example.com/data"}},
		{name: "wget blank arg", command: "wget", args: []string{""}},
		{name: "wget no stdout", command: "wget", args: []string{"https://api.example.com/data"}},
		{name: "wget missing output value", command: "wget", args: []string{"-O"}},
		{name: "wget output file", command: "wget", args: []string{"-O", "body.json", "https://api.example.com/data"}},
		{name: "wget recursive", command: "wget", args: []string{"-r", "-qO-", "https://api.example.com/data"}},
		{name: "httpie blank arg", command: "http", args: []string{""}},
		{name: "httpie no target", command: "http", args: []string{"GET"}},
		{name: "httpie missing auth value", command: "http", args: []string{"--auth"}},
		{name: "httpie download", command: "http", args: []string{"--download", "https://api.example.com/data"}},
		{name: "httpie headers", command: "http", args: []string{"--headers", "https://api.example.com/data"}},
		{name: "httpie print headers", command: "https", args: []string{"-pH", "api.example.com/data"}},
	}
	for _, tc := range denied {
		if commandOutputFirstNetworkResponseAllowed(tc.command, tc.args) {
			t.Fatalf("%s: %s %v must not be network-response allowed", tc.name, tc.command, tc.args)
		}
	}
}

func TestCommandOutputFirstInfraJSONEdges(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{command: "terraform", args: []string{"plan"}},
		{command: "terraform", args: []string{"-chdir=infra", "init"}},
		{command: "tofu", args: []string{"validate"}},
		{command: "tf", args: []string{"show", "-json"}},
		{command: "gh", args: []string{"api", "/repos/acme/project"}},
		{command: "gh", args: []string{"--repo", "acme/project", "pr", "list", "--json", "number,title"}},
		{command: "glab", args: []string{"pipeline", "list"}},
		{command: "aws", args: []string{"sts", "get-caller-identity"}},
		{command: "aws", args: []string{"ec2", "describe-instances", "--output", "json"}},
		{command: "jq", args: []string{".", "package.json"}},
		{command: "cargo", args: []string{"metadata", "--format-version", "1"}},
		{command: "go", args: []string{"env", "-json"}},
		{command: "npm", args: []string{"view", "react", "--json"}},
		{command: "curl", args: []string{"--url", "https://api.example.com/data"}},
		{command: "wget", args: []string{"-q", "-O", "-", "https://api.example.com/data"}},
		{command: "http", args: []string{"localhost:8990/status"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v should be infra/json allowed", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "terraform", args: []string{"apply", "-auto-approve"}},
		{command: "terraform", args: []string{"destroy"}},
		{command: "terraform", args: []string{"refresh"}},
		{command: "terraform", args: []string{"import", "aws_s3_bucket.main", "id"}},
		{command: "gh", args: []string{"pr", "view", "1"}},
		{command: "glab", args: []string{"mr", "view", "1"}},
		{command: "aws", args: []string{"configure"}},
		{command: "aws", args: []string{"sso", "login"}},
		{command: "aws", args: []string{"ecr", "get-login-password"}},
		{command: "go", args: []string{"env"}},
		{command: "npm", args: []string{"install", "--json"}},
		{command: "cargo", args: []string{"install", "ripgrep", "--json"}},
		{command: "curl", args: []string{"-I", "https://api.example.com/data"}},
		{command: "curl", args: []string{"--output=body.json", "https://api.example.com/data"}},
		{command: "wget", args: []string{"-r", "-qO-", "https://api.example.com/data"}},
		{command: "http", args: []string{"--headers", "https://api.example.com/data"}},
	}
	for _, tc := range denied {
		if commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v must not be infra/json allowed", tc.command, tc.args)
		}
	}
}

func TestCommandOutputFirstInfraJSONHelperBoundaries(t *testing.T) {
	allowed := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "terraform chdir split option", command: "terraform", args: []string{"-chdir", "infra", "plan"}},
		{name: "terraform var-file split option", command: "terraform", args: []string{"-var-file", "prod.tfvars", "show"}},
		{name: "docker root inspect", command: "docker", args: []string{"inspect", "web"}},
		{name: "docker object inspect", command: "docker", args: []string{"container", "inspect", "web"}},
		{name: "docker compose json", command: "docker", args: []string{"compose", "config", "--format=json"}},
		{name: "docker-compose json", command: "docker-compose", args: []string{"config", "--format", "json"}},
		{name: "yarn npm json", command: "yarn", args: []string{"npm", "info", "react", "--json"}},
		{name: "bun pm json", command: "bun", args: []string{"pm", "view", "react", "--json=1"}},
		{name: "gh repo list", command: "gh", args: []string{"--repo", "acme/project", "pr", "list"}},
		{name: "gh json flag", command: "gh", args: []string{"pr", "view", "1", "--json=number"}},
		{name: "aws region before command", command: "aws", args: []string{"--region", "eu-central-1", "sts", "get-caller-identity"}},
		{name: "aws output equals json", command: "aws", args: []string{"ec2", "describe-instances", "--output=json"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s: %s %v should be command-output-first allowed", tc.name, tc.command, tc.args)
		}
	}

	denied := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "terraform missing chdir value", command: "terraform", args: []string{"-chdir"}},
		{name: "terraform blank var value", command: "terraform", args: []string{"-var", " ", "plan"}},
		{name: "terraform blank first arg", command: "terraform", args: []string{" ", "plan"}},
		{name: "docker no args", command: "docker", args: nil},
		{name: "docker compose no json", command: "docker", args: []string{"compose", "config"}},
		{name: "docker-compose no json", command: "docker-compose", args: []string{"config"}},
		{name: "npm json false", command: "npm", args: []string{"view", "react", "--json=false"}},
		{name: "npm no script", command: "npm", args: []string{"--json"}},
		{name: "npm unknown json verb", command: "npm", args: []string{"install", "--json"}},
		{name: "bun direct json", command: "bun", args: []string{"view", "react", "--json"}},
		{name: "gh no args", command: "gh", args: nil},
		{name: "gh missing repo value", command: "gh", args: []string{"--repo"}},
		{name: "gh blank repo value", command: "gh", args: []string{"--repo", " ", "pr", "list"}},
		{name: "aws no args", command: "aws", args: nil},
		{name: "aws only option", command: "aws", args: []string{"--profile", "prod"}},
		{name: "aws missing option value", command: "aws", args: []string{"--region"}},
		{name: "aws output missing value", command: "aws", args: []string{"sts", "get-caller-identity", "--output"}},
		{name: "aws text output", command: "aws", args: []string{"sts", "get-caller-identity", "--output", "text"}},
		{name: "aws output equals text", command: "aws", args: []string{"sts", "get-caller-identity", "--output=text"}},
	}
	for _, tc := range denied {
		if commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s: %s %v must not be command-output-first allowed", tc.name, tc.command, tc.args)
		}
	}
}

func TestCommandOutputFirstInfraJSONCompactionBranches(t *testing.T) {
	var goVet strings.Builder
	for i := 0; i < 80; i++ {
		goVet.WriteString("internal/app/app.go:10:5: fmt.Printf call needs 1 arg but has 2 args\n")
	}
	var terraformFmt strings.Builder
	for i := 0; i < 32; i++ {
		fmt.Fprintf(&terraformFmt, "modules/app_%02d/main.tf\n", i)
	}
	var ghRuns strings.Builder
	for i := 1; i <= 25; i++ {
		state := "SUCCESS"
		if i == 23 {
			state = "FAILURE"
		}
		fmt.Fprintf(&ghRuns, "%d\tci run %d\t%s\t2024-01-01\n", i, i, state)
	}
	var glabPipelines strings.Builder
	for i := 1; i <= 25; i++ {
		status := "success"
		if i == 22 {
			status = "failed"
		}
		fmt.Fprintf(&glabPipelines, "pipeline-%02d  branch-%02d  %s  2024-01-01\n", i, i, status)
	}
	cases := []struct {
		name    string
		command string
		args    []string
		stdout  []byte
		want    string
	}{
		{
			name:    "go env json exact",
			command: "go",
			args:    []string{"env", "-json"},
			stdout:  []byte("{\n  \"GOOS\": \"darwin\",\n  \"GOARCH\": \"arm64\"\n}\n"),
			want:    `{"GOOS":"darwin","GOARCH":"arm64"}`,
		},
		{
			name:    "go vet diagnostic summary",
			command: "go",
			args:    []string{"vet", "./..."},
			stdout:  []byte(goVet.String()),
			want:    "fmt.Printf call needs 1 arg but has 2 args [x80]",
		},
		{
			name:    "npm view json exact",
			command: "npm",
			args:    []string{"view", "react", "--json"},
			stdout:  []byte("{\n  \"name\": \"react\",\n  \"version\": \"19.0.0\"\n}\n"),
			want:    `{"name":"react","version":"19.0.0"}`,
		},
		{
			name:    "cargo metadata summary",
			command: "cargo",
			args:    []string{"metadata", "--format-version", "1"},
			stdout:  []byte(`{"packages":[{"name":"app","version":"0.1.0","id":"path+file:///app#0.1.0"},{"name":"lib","version":"0.2.0","id":"path+file:///lib#0.2.0"},{"name":"serde","version":"1.0.0","id":"registry+serde#1.0.0"}],"workspace_members":["path+file:///app#0.1.0","path+file:///lib#0.2.0"],"resolve":{"nodes":[{"id":"path+file:///app#0.1.0","dependencies":["registry+serde#1.0.0"]}]}}`),
			want:    "[cargo metadata]",
		},
		{
			name:    "docker inspect json exact",
			command: "docker",
			args:    []string{"container", "inspect", "web"},
			stdout:  []byte("[\n  {\"Id\": \"abc\", \"State\": {\"Status\": \"running\"}}\n]\n"),
			want:    `"Status":"running"`,
		},
		{
			name:    "kubectl healthy json exact fallback",
			command: "kubectl",
			args:    []string{"get", "pods", "-o", "json"},
			stdout:  []byte("{\n  \"kind\": \"List\",\n  \"items\": []\n}\n"),
			want:    `{"kind":"List","items":[]}`,
		},
		{
			name:    "terraform init summary",
			command: "terraform",
			args:    []string{"init"},
			stdout: []byte(`Initializing provider plugins...
- Finding hashicorp/aws versions matching "~> 5.0"...
- Finding hashicorp/random versions matching "~> 3.5"...
- Installing hashicorp/aws v5.31.0...
- Installed hashicorp/aws v5.31.0 (signed by HashiCorp)
- Installing hashicorp/random v3.5.1...
- Installed hashicorp/random v3.5.1 (signed by HashiCorp)

Terraform has been successfully initialized!
`),
			want: "provider(s) installed",
		},
		{
			name:    "terraform validate summary",
			command: "terraform",
			args:    []string{"validate"},
			stdout: []byte(`Initializing modules...

Success! The configuration is valid.

`),
			want: "Success!",
		},
		{
			name:    "terraform show text summary",
			command: "terraform",
			args:    []string{"show"},
			stdout: []byte(`  # aws_s3_bucket.main was created
  + resource "aws_s3_bucket" "main" {
      + acl = "private"
    }

Plan: 1 to add, 0 to change, 0 to destroy.
`),
			want: "aws_s3_bucket.main",
		},
		{
			name:    "terraform show json summary",
			command: "terraform",
			args:    []string{"show", "-json", "plan.out"},
			stdout:  []byte(`{"format_version":"1.2","resource_changes":[{"address":"aws_s3_bucket.app","change":{"actions":["create"]}},{"address":"aws_iam_role.old","change":{"actions":["delete"]}}]}`),
			want:    "aws_iam_role.old actions=delete",
		},
		{
			name:    "terraform fmt summary",
			command: "terraform",
			args:    []string{"fmt", "-recursive"},
			stdout:  []byte(terraformFmt.String()),
			want:    "[terraform] 32 file(s) formatted",
		},
		{
			name:    "gh list summary",
			command: "gh",
			args:    []string{"run", "list"},
			stdout:  []byte(ghRuns.String()),
			want:    "ci run 23",
		},
		{
			name:    "glab list summary",
			command: "glab",
			args:    []string{"pipeline", "list"},
			stdout:  []byte(glabPipelines.String()),
			want:    "pipeline-22",
		},
		{
			name:    "aws json exact",
			command: "aws",
			args:    []string{"sts", "get-caller-identity"},
			stdout:  []byte("{\n  \"UserId\": \"AIDAEXAMPLE\",\n  \"Account\": \"123456789012\"\n}\n"),
			want:    `"Account":"123456789012"`,
		},
	}
	for _, tc := range cases {
		out, ok := compactCommandOutputFirstStdout(tc.command, tc.command, tc.args, tc.stdout, 0)
		if !ok {
			t.Fatalf("%s: expected compaction", tc.name)
		}
		if !strings.Contains(string(out), tc.want) {
			t.Fatalf("%s: compacted output missing %q in %q", tc.name, tc.want, out)
		}
		if len(out) >= len(tc.stdout) {
			t.Fatalf("%s: compacted output must shrink, in=%d out=%d", tc.name, len(tc.stdout), len(out))
		}
	}
}

func TestCommandOutputFirstShimNpxEsbuildCompacts(t *testing.T) {
	var build strings.Builder
	for i := 0; i < 40; i++ {
		build.WriteString("dist/chunk")
		build.WriteString(strconv.Itoa(i))
		build.WriteString(".js 12.3 kb\n")
	}
	build.WriteString("Done in 10ms\n")
	realNpx := writeFakeCommand(t, "npx", "#!/bin/sh\ncat <<'EOF'\n"+build.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npx", "--real-bin=" + realNpx, "--", "-y", "esbuild", "src/index.ts", "--bundle"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[esbuild] ok\n" {
		t.Fatalf("unexpected compacted esbuild stdout=%q", got)
	}
}

func TestCommandOutputFirstShimNpxVitestCompacts(t *testing.T) {
	realNpx := writeFakeCommand(t, "npx", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstJSTestFixture(24)+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npx", "--real-bin=" + realNpx, "--", "-y", "vitest", "run"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[vitest] ok - 24 passed") || strings.Contains(got, "renders op 023") {
		t.Fatalf("unexpected compacted npx vitest stdout=%q", got)
	}
}

func TestCommandOutputFirstShimDirectTestRunnerNonzeroFullPasses(t *testing.T) {
	realJest := writeFakeCommand(t, "jest", `#!/bin/sh
printf 'FAIL src/app.test.ts\nTests: 1 failed, 1 total\n'
printf 'runner diagnostic\n' >&2
exit 1
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=jest", "--real-bin=" + realJest, "--", "--runInBand"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if got := stdout.String(); got != "FAIL src/app.test.ts\nTests: 1 failed, 1 total\n" {
		t.Fatalf("stdout=%q", got)
	}
	if got := stderr.String(); got != "runner diagnostic\n" {
		t.Fatalf("stderr=%q", got)
	}
}

func TestCommandOutputFirstShimMakeCompacts(t *testing.T) {
	var build strings.Builder
	build.WriteString("make[1]: Entering directory '/repo/build'\n")
	build.WriteString("Consolidate compiler generated dependencies of target app\n")
	for i := 0; i < 24; i++ {
		build.WriteString("[ 50%] Building CXX object src/CMakeFiles/app.dir/generated/object.cpp.o\n")
	}
	build.WriteString("[100%] Linking CXX executable app\n")
	build.WriteString("[100%] Built target app\n")
	build.WriteString("make[1]: Leaving directory '/repo/build'\n")
	realMake := writeFakeCommand(t, "make", "#!/bin/sh\ncat <<'EOF'\n"+build.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=make", "--real-bin=" + realMake, "--", "-j8"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("make rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[make] ok\n" {
		t.Fatalf("unexpected make compacted stdout=%q", got)
	}
}

func commandOutputFirstJSTestFixture(count int) string {
	var out strings.Builder
	out.WriteString("PASS src/app.test.ts\n")
	for i := 0; i < count; i++ {
		out.WriteString("  \u2713 renders op ")
		out.WriteString(strings.Repeat("x", 3))
		out.WriteString(" (2 ms)\n")
	}
	out.WriteString("\nTests: ")
	for _, ch := range strconv.Itoa(count) {
		out.WriteRune(ch)
	}
	out.WriteString(" passed, ")
	for _, ch := range strconv.Itoa(count) {
		out.WriteRune(ch)
	}
	out.WriteString(" total\nTime: 1.2 s\n")
	return out.String()
}

func commandOutputFirstNpmInstallFixture(count int) string {
	var out strings.Builder
	for i := 0; i < count; i++ {
		out.WriteString("npm http fetch GET 200 https://registry.npmjs.org/package_")
		if i < 10 {
			out.WriteString("00")
		} else if i < 100 {
			out.WriteString("0")
		}
		out.WriteString(strconv.Itoa(i))
		out.WriteString(" 12ms\n")
		out.WriteString("npm timing idealTree:node_modules/package_")
		if i < 10 {
			out.WriteString("00")
		} else if i < 100 {
			out.WriteString("0")
		}
		out.WriteString(strconv.Itoa(i))
		out.WriteString(" Completed in 5ms\n")
	}
	out.WriteString("\nadded ")
	out.WriteString(strconv.Itoa(count))
	out.WriteString(" packages, and audited ")
	out.WriteString(strconv.Itoa(count + 1))
	out.WriteString(" packages in 12s\n\n")
	out.WriteString("45 packages are looking for funding\n")
	out.WriteString("  run `npm fund` for details\n\n")
	out.WriteString("found 0 vulnerabilities\n")
	return out.String()
}

func commandOutputFirstMavenFixture(modules int) string {
	var out strings.Builder
	out.WriteString("[INFO] Scanning for projects...\n")
	out.WriteString("[INFO] \n")
	out.WriteString("[INFO] -----------------------< com.example:demo >------------------------\n")
	out.WriteString("[INFO] Building demo 1.0.0\n")
	out.WriteString("[INFO] --------------------------------[ jar ]---------------------------------\n")
	for i := 0; i < modules; i++ {
		out.WriteString("[INFO] --- maven-resources-plugin:3.3.1:resources (default-resources-")
		out.WriteString(strconv.Itoa(i))
		out.WriteString(") @ demo ---\n")
		out.WriteString("[INFO] Copying 1 resources from src/main/resources to target/classes\n")
	}
	out.WriteString("[INFO] --- maven-compiler-plugin:3.13.0:compile (default-compile) @ demo ---\n")
	out.WriteString("[INFO] Changes detected - recompiling the module!\n")
	out.WriteString("[INFO] Compiling 3 source files with javac [debug target 21] to target/classes\n")
	out.WriteString("[INFO] --- maven-surefire-plugin:3.2.5:test (default-test) @ demo ---\n")
	out.WriteString("[INFO] Running com.example.DemoTest\n")
	out.WriteString("[INFO] Tests run: 42, Failures: 0, Errors: 0, Skipped: 0\n")
	out.WriteString("[INFO] --- maven-jar-plugin:3.4.1:jar (default-jar) @ demo ---\n")
	out.WriteString("[INFO] Building jar: /repo/target/demo.jar\n")
	out.WriteString("[INFO] ------------------------------------------------------------------------\n")
	out.WriteString("[INFO] BUILD SUCCESS\n")
	out.WriteString("[INFO] ------------------------------------------------------------------------\n")
	out.WriteString("[INFO] Total time:  4.123 s\n")
	out.WriteString("[INFO] Finished at: 2026-06-20T01:02:03Z\n")
	out.WriteString("[INFO] ------------------------------------------------------------------------\n")
	return out.String()
}

func commandOutputFirstGradleBuildFixture(tasks int) string {
	var out strings.Builder
	out.WriteString("Starting a Gradle Daemon, 1 busy Daemon could not be reused, use --status for details\n")
	for i := 0; i < tasks; i++ {
		out.WriteString("> Task :module")
		out.WriteString(strconv.Itoa(i))
		out.WriteString(":compileJava\n")
	}
	out.WriteString("BUILD SUCCESSFUL in 4s\n")
	out.WriteString(strconv.Itoa(tasks))
	out.WriteString(" actionable tasks: ")
	out.WriteString(strconv.Itoa(tasks))
	out.WriteString(" executed\n")
	return out.String()
}

func TestCommandOutputFirstNpxToolEdges(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantTool string
		wantArgs []string
		wantOK   bool
	}{
		{name: "yes next", args: []string{"--yes", "next", "build"}, wantTool: "next", wantArgs: []string{"build"}, wantOK: true},
		{name: "package value", args: []string{"--package", "next", "next", "build"}, wantTool: "next", wantArgs: []string{"build"}, wantOK: true},
		{name: "package equals", args: []string{"--package=next", "--", "next", "build"}, wantTool: "next", wantArgs: []string{"build"}, wantOK: true},
		{name: "missing package value", args: []string{"--package"}, wantOK: false},
		{name: "unknown option", args: []string{"--call", "next build"}, wantOK: false},
		{name: "empty", args: []string{""}, wantOK: false},
		{name: "separator without tool", args: []string{"--"}, wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool, toolArgs, ok := commandOutputFirstNpxTool(tc.args)
			if ok != tc.wantOK || tool != tc.wantTool || strings.Join(toolArgs, "\x00") != strings.Join(tc.wantArgs, "\x00") {
				t.Fatalf("npx tool args=%v got tool=%q args=%v ok=%v want tool=%q args=%v ok=%v", tc.args, tool, toolArgs, ok, tc.wantTool, tc.wantArgs, tc.wantOK)
			}
		})
	}
}

func TestCommandOutputFirstShimPreCommitAndPrettierCompact(t *testing.T) {
	var hooks strings.Builder
	for i := 0; i < 20; i++ {
		hooks.WriteString("Hook check...............................................................Passed\n")
	}
	realPreCommit := writeFakeCommand(t, "pre-commit", "#!/bin/sh\ncat <<'EOF'\n"+hooks.String()+"EOF\n")
	var lintStdout, lintStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=pre-commit", "--real-bin=" + realPreCommit, "--", "run", "--all-files"}, &bytes.Buffer{}, &lintStdout, &lintStderr)
	if rc != 0 {
		t.Fatalf("pre-commit rc=%d stderr=%q", rc, lintStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(lintStdout.String()); got != "[pre-commit] ok (20 hooks passed)\n" {
		t.Fatalf("unexpected pre-commit compacted stdout=%q", got)
	}

	realPrettier := writeFakeCommand(t, "prettier", "#!/bin/sh\ncat <<'EOF'\nChecking formatting...\nAll matched files use Prettier code style!\nEOF\n")
	var fmtStdout, fmtStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=prettier", "--real-bin=" + realPrettier, "--", "--check", "."}, &bytes.Buffer{}, &fmtStdout, &fmtStderr)
	if rc != 0 {
		t.Fatalf("prettier rc=%d stderr=%q", rc, fmtStderr.String())
	}
	if got := fmtStdout.String(); got != "Checking formatting...\nAll matched files use Prettier code style!\n" {
		t.Fatalf("small prettier output should full-pass after archive overhead, got %q", got)
	}
	if uri := commandOutputFirstArchiveURI(fmtStdout.String()); uri != "" {
		t.Fatalf("small prettier full-pass must not archive: %q", fmtStdout.String())
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroDiagnosticsCompactWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var diagnostics strings.Builder
	for i := 0; i < 60; i++ {
		diagnostics.WriteString("internal/app/app.go:22:7: this value of err is never used (SA4006)\n")
	}
	realStaticcheck := writeFakeCommand(t, "staticcheck", "#!/bin/sh\ncat <<'EOF'\n"+diagnostics.String()+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=staticcheck", "--real-bin=" + realStaticcheck, "--", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	for _, want := range []string{
		"[staticcheck] FAILED (60 diagnostics)",
		"internal/app/app.go:22:7: this value of err is never used (SA4006) (repeated 60 times)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused lint compact output missing %q in %q", want, got)
		}
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand focused lint archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("SA4006")) || bytes.Count(raw, []byte("this value of err is never used")) != 60 {
		t.Fatalf("archive did not preserve focused lint raw output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:staticcheck] staticcheck ./...") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroStderrDiagnosticsCompactWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var diagnostics strings.Builder
	for i := 0; i < 70; i++ {
		diagnostics.WriteString("internal/app/app.go:10:2: unused-parameter: parameter ctx seems to be unused, consider removing or renaming it as _ (revive)\n")
	}
	realGolangci := writeFakeCommand(t, "golangci-lint", "#!/bin/sh\ncat >&2 <<'EOF'\n"+diagnostics.String()+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=golangci-lint", "--real-bin=" + realGolangci, "--", "run", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	got := commandOutputFirstVisibleOutput(stderr.String())
	for _, want := range []string{
		"[golangci-lint] FAILED (70 diagnostics)",
		"internal/app/app.go:10:2: unused-parameter: parameter ctx seems to be unused, consider removing or renaming it as _ (revive) (repeated 70 times)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused lint stderr compact output missing %q in %q", want, got)
		}
	}
	if !strings.Contains(stderr.String(), "stream=stderr") {
		t.Fatalf("stderr archive marker must preserve stream distinction: %q", stderr.String())
	}
	uri := commandOutputFirstArchiveURI(stderr.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stderr.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand focused lint stderr archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("unused-parameter")) || bytes.Count(raw, []byte("parameter ctx seems to be unused")) != 70 {
		t.Fatalf("archive did not preserve focused lint raw stderr: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:golangci-lint] golangci-lint run ./...") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroMixedStdoutStderrCompactsStdout(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var diagnostics strings.Builder
	for i := 0; i < 72; i++ {
		diagnostics.WriteString("internal/app/app.go:22:7: this value of err is never used (SA4006)\n")
	}
	realStaticcheck := writeFakeCommand(t, "staticcheck", "#!/bin/sh\ncat <<'EOF'\n"+diagnostics.String()+"EOF\nprintf 'warning: matched no packages\\n' >&2\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=staticcheck", "--real-bin=" + realStaticcheck, "--", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	for _, want := range []string{
		"[staticcheck] FAILED (72 diagnostics)",
		"internal/app/app.go:22:7: this value of err is never used (SA4006) (repeated 72 times)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused lint mixed stdout compact output missing %q in %q", want, got)
		}
	}
	if got := stderr.String(); got != "warning: matched no packages\n" {
		t.Fatalf("stderr must stay byte-identical, got %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing mixed stdout archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand mixed stdout archive: %v", err)
	}
	if bytes.Count(raw, []byte("this value of err is never used")) != 72 {
		t.Fatalf("archive did not preserve mixed stdout diagnostics: %q", raw)
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
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroMixedStdoutStderrCompactsStderr(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var diagnostics strings.Builder
	for i := 0; i < 72; i++ {
		diagnostics.WriteString("internal/app/app.go:10:2: unused-parameter: bad (revive)\n")
	}
	realGolangci := writeFakeCommand(t, "golangci-lint", "#!/bin/sh\nprintf 'lint runner note\\n'\ncat >&2 <<'EOF'\n"+diagnostics.String()+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=golangci-lint", "--real-bin=" + realGolangci, "--", "run", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if got := stdout.String(); got != "lint runner note\n" {
		t.Fatalf("stdout must stay byte-identical, got %q", got)
	}
	got := commandOutputFirstVisibleOutput(stderr.String())
	if !strings.Contains(got, "[golangci-lint] FAILED (72 diagnostics)") ||
		!strings.Contains(got, "internal/app/app.go:10:2: unused-parameter: bad (revive) (repeated 72 times)") {
		t.Fatalf("focused lint mixed stderr compact output=%q", got)
	}
	if !strings.Contains(stderr.String(), "stream=stderr") {
		t.Fatalf("stderr archive marker must preserve stream distinction: %q", stderr.String())
	}
	uri := commandOutputFirstArchiveURI(stderr.String())
	if uri == "" {
		t.Fatalf("missing mixed stderr archive marker in %q", stderr.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand mixed stderr archive: %v", err)
	}
	if bytes.Count(raw, []byte("unused-parameter")) != 72 {
		t.Fatalf("archive did not preserve mixed stderr diagnostics: %q", raw)
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
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimStructuredNonzeroDiagnosticsCompactWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var diagnostics strings.Builder
	for i := 0; i < 80; i++ {
		diagnostics.WriteString("src/app.ts:10:5 - error TS2322: Type 'string' is not assignable to type 'number'.\n")
	}
	realTSC := writeFakeCommand(t, "tsc", "#!/bin/sh\ncat <<'EOF'\n"+diagnostics.String()+"EOF\nexit 2\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=tsc", "--real-bin=" + realTSC, "--", "--noEmit"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 2 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[typescript] FAILED") ||
		!strings.Contains(got, "src/app.ts:10:5 - error TS2322: Type 'string' is not assignable to type 'number'.") {
		t.Fatalf("structured diagnostics compact output=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing structured diagnostic archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand structured diagnostic archive: %v", err)
	}
	if bytes.Count(raw, []byte("TS2322")) != 80 {
		t.Fatalf("archive did not preserve structured diagnostic raw output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:tsc] tsc --noEmit") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimGoVetNonzeroDiagnosticsCompactWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var diagnostics strings.Builder
	for i := 0; i < 80; i++ {
		diagnostics.WriteString("internal/app/app.go:10:5: fmt.Printf call needs 1 arg but has 2 args\n")
	}
	realGo := writeFakeCommand(t, "go", "#!/bin/sh\ncat <<'EOF'\n"+diagnostics.String()+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=go", "--real-bin=" + realGo, "--", "vet", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[go vet] FAILED") ||
		!strings.Contains(got, "internal/app/app.go:10:5: fmt.Printf call needs 1 arg but has 2 args [x80]") {
		t.Fatalf("go vet diagnostics compact output=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing go vet diagnostic archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand go vet archive: %v", err)
	}
	if bytes.Count(raw, []byte("fmt.Printf call needs 1 arg")) != 80 {
		t.Fatalf("archive did not preserve go vet raw output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:go] go vet ./...") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimStructuredNonzeroDiagnosticsFailOpen(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	raw := "build failed\ninspect full logs manually\n"
	realTSC := writeFakeCommand(t, "tsc", "#!/bin/sh\ncat <<'EOF'\n"+raw+"EOF\nexit 2\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=tsc", "--real-bin=" + realTSC, "--", "--noEmit"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 2 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.String() != raw {
		t.Fatalf("unstructured diagnostics must full-pass, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("unstructured full-pass must not archive: %q", stdout.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if run, ok, err := filter.LastFilterRun(db); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("unstructured full-pass must not record accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroMixedTinyFullPasses(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realStaticcheck := writeFakeCommand(t, "staticcheck", `#!/bin/sh
printf 'internal/app/app.go:22:7: this value of err is never used (SA4006)\n'
printf 'warning: matched no packages\n' >&2
exit 1
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=staticcheck", "--real-bin=" + realStaticcheck, "--", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if got := stdout.String(); got != "internal/app/app.go:22:7: this value of err is never used (SA4006)\n" {
		t.Fatalf("stdout=%q", got)
	}
	if got := stderr.String(); got != "warning: matched no packages\n" {
		t.Fatalf("stderr=%q", got)
	}
	if uri := commandOutputFirstArchiveURI(stdout.String() + stderr.String()); uri != "" {
		t.Fatalf("tiny mixed full-pass must not archive: %q %q", stdout.String(), stderr.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if run, ok, err := filter.LastFilterRun(db); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("mixed stdout/stderr full-pass must not record accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroUnknownLineFullPasses(t *testing.T) {
	realStaticcheck := writeFakeCommand(t, "staticcheck", `#!/bin/sh
cat <<'EOF'
warning: matched no packages
internal/app/app.go:22:7: this value of err is never used (SA4006)
EOF
exit 1
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=staticcheck", "--real-bin=" + realStaticcheck, "--", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	want := "warning: matched no packages\ninternal/app/app.go:22:7: this value of err is never used (SA4006)\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout=%q want=%q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("unknown-line full-pass must not archive: %q", stdout.String())
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroStderrUnknownLineFullPasses(t *testing.T) {
	realGolangci := writeFakeCommand(t, "golangci-lint", `#!/bin/sh
cat >&2 <<'EOF'
level=info msg="golangci-lint has version 2.1.0"
internal/app/app.go:10:2: unused-parameter: bad (revive)
EOF
exit 1
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=golangci-lint", "--real-bin=" + realGolangci, "--", "run", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	want := "level=info msg=\"golangci-lint has version 2.1.0\"\ninternal/app/app.go:10:2: unused-parameter: bad (revive)\n"
	if got := stderr.String(); got != want {
		t.Fatalf("stderr=%q want=%q", got, want)
	}
	if uri := commandOutputFirstArchiveURI(stderr.String()); uri != "" {
		t.Fatalf("unknown-line stderr full-pass must not archive: %q", stderr.String())
	}
}

func TestCommandOutputFirstShimMypyNonzeroStdoutCompactWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var diagnostics strings.Builder
	for i := 0; i < 75; i++ {
		diagnostics.WriteString("src/app.py:10: error: Incompatible return value type\n")
	}
	diagnostics.WriteString("src/app.py:10: note: expected str\n")
	diagnostics.WriteString("Found 75 errors in 1 file (checked 48 source files)\n")
	realMypy := writeFakeCommand(t, "mypy", "#!/bin/sh\ncat <<'EOF'\n"+diagnostics.String()+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=mypy", "--real-bin=" + realMypy, "--", "src"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	for _, want := range []string{
		"[mypy] FAILED (76 diagnostics)",
		"src/app.py:10: error: Incompatible return value type (repeated 75 times)",
		"src/app.py:10: note: expected str",
		"Found 75 errors in 1 file",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("mypy compact output missing %q in %q", want, got)
		}
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing mypy archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand mypy archive: %v", err)
	}
	if bytes.Count(raw, []byte("Incompatible return value type")) != 75 ||
		!bytes.Contains(raw, []byte("Found 75 errors in 1 file")) {
		t.Fatalf("archive did not preserve mypy diagnostics: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:mypy] mypy src") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimPythonMypyNonzeroStderrCompactWithArchive(t *testing.T) {
	var diagnostics strings.Builder
	for i := 0; i < 65; i++ {
		diagnostics.WriteString("pkg/model.pyi:7: error: Missing return statement\n")
	}
	diagnostics.WriteString("Found 65 errors in 1 file\n")
	realPython := writeFakeCommand(t, "python3", "#!/bin/sh\ncat >&2 <<'EOF'\n"+diagnostics.String()+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=python3", "--real-bin=" + realPython, "--", "-m", "mypy", "pkg"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	got := commandOutputFirstVisibleOutput(stderr.String())
	if !strings.Contains(got, "[mypy] FAILED (65 diagnostics)") ||
		!strings.Contains(got, "pkg/model.pyi:7: error: Missing return statement (repeated 65 times)") {
		t.Fatalf("python -m mypy compact output=%q", got)
	}
	if !strings.Contains(stderr.String(), "stream=stderr") {
		t.Fatalf("stderr archive marker must preserve stream distinction: %q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stderr.String()); uri == "" {
		t.Fatalf("missing python -m mypy archive marker in %q", stderr.String())
	}
}

func TestCommandOutputFirstShimMypyRiskyDiagnosticsFullPass(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realMypy := writeFakeCommand(t, "mypy", `#!/bin/sh
cat <<'EOF'
Skipping analyzing 'requests': module is installed, but missing library stubs
src/app.py:10: error: bad
Found 1 error in 1 file
EOF
exit 1
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=mypy", "--real-bin=" + realMypy, "--", "src"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	want := "Skipping analyzing 'requests': module is installed, but missing library stubs\nsrc/app.py:10: error: bad\nFound 1 error in 1 file\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout=%q want=%q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("risky mypy full-pass must not archive: %q", stdout.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if run, ok, err := filter.LastFilterRun(db); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("risky mypy full-pass must not record accounting row: %+v", run)
	}
}

func TestCommandOutputFirstPythonMypyAllowCaptureIsNarrow(t *testing.T) {
	if !commandOutputFirstAllowCapture("python3", []string{"-m", "mypy", "src"}) {
		t.Fatal("python -m mypy should be captured")
	}
	if commandOutputFirstAllowCapture("python3", []string{"script.py"}) {
		t.Fatal("plain python script must not be captured as mypy")
	}
}

func TestCommandOutputFirstShimEslintStylishNonzeroStdoutCompactWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realEslint := writeFakeCommand(t, "eslint", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstEslintStylishFixture("src/app.js", 45)+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=eslint", "--real-bin=" + realEslint, "--", "src", "--format", "stylish"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	for _, want := range []string{
		"[eslint] FINDINGS (90 problems: 45 errors, 45 warnings in 1 file)",
		"src/app.js",
		"2:1 warning [no-console] Unexpected console statement",
		"2:20 error [eqeqeq] Expected '===' and instead saw '=='",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("eslint compact output missing %q in %q", want, got)
		}
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing eslint archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand eslint archive: %v", err)
	}
	if bytes.Count(raw, []byte("Unexpected console statement")) != 45 ||
		bytes.Count(raw, []byte("Expected '===' and instead saw '=='")) != 45 {
		t.Fatalf("archive did not preserve eslint raw output: %q", raw)
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
	if !strings.Contains(run.Command, "[command-output-first:eslint] eslint src --format stylish") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimEslintStylishNonzeroStderrCompactWithArchive(t *testing.T) {
	realEslint := writeFakeCommand(t, "eslint", "#!/bin/sh\ncat >&2 <<'EOF'\n"+commandOutputFirstEslintStylishFixture("src/app.js", 40)+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=eslint", "--real-bin=" + realEslint, "--", "src"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if !strings.Contains(commandOutputFirstVisibleOutput(stderr.String()), "[eslint] FINDINGS (80 problems: 40 errors, 40 warnings in 1 file)") {
		t.Fatalf("eslint stderr compact output=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "stream=stderr") {
		t.Fatalf("eslint stderr archive marker must preserve stream: %q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stderr.String()); uri == "" {
		t.Fatalf("missing eslint stderr archive marker in %q", stderr.String())
	}
}

func TestCommandOutputFirstShimEslintStylishMixedStdoutStderrCompactsStdout(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realEslint := writeFakeCommand(t, "eslint", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstEslintStylishFixture("src/app.js", 20)+"EOF\nprintf 'warning: config ignored\\n' >&2\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=eslint", "--real-bin=" + realEslint, "--", "src"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[eslint] FINDINGS (40 problems: 20 errors, 20 warnings in 1 file)") ||
		!strings.Contains(got, "2:1 warning [no-console] Unexpected console statement") {
		t.Fatalf("eslint mixed compact output=%q", got)
	}
	if got := stderr.String(); got != "warning: config ignored\n" {
		t.Fatalf("stderr must stay byte-identical, got %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing eslint mixed archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand mixed eslint archive: %v", err)
	}
	if bytes.Count(raw, []byte("Unexpected console statement")) != 20 ||
		bytes.Count(raw, []byte("Expected '===' and instead saw '=='")) != 20 {
		t.Fatalf("archive did not preserve mixed eslint raw output: %q", raw)
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
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimEslintStylishAnsiFullPasses(t *testing.T) {
	realEslint := writeFakeCommand(t, "eslint", "#!/bin/sh\nprintf '\\033[31msrc/app.js\\033[0m\\n  1:1  error  bad  no-console\\n\\n\\342\\234\\226 1 problem (1 error, 0 warnings)\\n'\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=eslint", "--real-bin=" + realEslint, "--", "src"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "\x1b[31msrc/app.js\x1b[0m") {
		t.Fatalf("ansi eslint output should full-pass, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("ansi eslint full-pass must not archive: %q", stdout.String())
	}
}

func commandOutputFirstEslintStylishFixture(file string, count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		b.WriteString("\n")
		b.WriteString(file)
		b.WriteString("\n")
		b.WriteString("  2:1   warning  Unexpected console statement         no-console\n")
		b.WriteString("  2:20  error    Expected '===' and instead saw '=='  eqeqeq\n")
	}
	total := count * 2
	b.WriteString("\n\u2716 ")
	b.WriteString(strconv.Itoa(total))
	b.WriteString(" problems (")
	b.WriteString(strconv.Itoa(count))
	b.WriteString(" errors, ")
	b.WriteString(strconv.Itoa(count))
	b.WriteString(" warnings)\n")
	return b.String()
}

func TestCommandOutputFirstShimPackageLintAndFormatCompact(t *testing.T) {
	realNpm := writeFakeCommand(t, "npm", "#!/bin/sh\ncat <<'EOF'\n> app@1.0.0 lint /repo\n> pre-commit run --all-files\nHook one................................................................Passed\nHook two................................................................Passed\nHook three..............................................................Passed\nHook four...............................................................Passed\nHook five...............................................................Passed\nHook six................................................................Passed\nHook seven..............................................................Passed\nHook eight..............................................................Passed\nHook nine...............................................................Passed\nHook ten................................................................Passed\nHook eleven.............................................................Passed\nHook twelve.............................................................Passed\nEOF\n")
	var lintStdout, lintStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npm", "--real-bin=" + realNpm, "--", "run", "lint"}, &bytes.Buffer{}, &lintStdout, &lintStderr)
	if rc != 0 {
		t.Fatalf("npm lint rc=%d stderr=%q", rc, lintStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(lintStdout.String()); got != "[pre-commit] ok (12 hooks passed)\n" {
		t.Fatalf("unexpected npm lint compacted stdout=%q", got)
	}

	realYarn := writeFakeCommand(t, "yarn", "#!/bin/sh\ncat <<'EOF'\n> app@1.0.0 format:check /repo\n> prettier --check .\nChecking formatting...\nAll matched files use Prettier code style!\nEOF\n")
	var fmtStdout, fmtStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=yarn", "--real-bin=" + realYarn, "--", "run", "format:check"}, &bytes.Buffer{}, &fmtStdout, &fmtStderr)
	if rc != 0 {
		t.Fatalf("yarn format rc=%d stderr=%q", rc, fmtStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(fmtStdout.String()); got != "[prettier] ok\n" {
		t.Fatalf("yarn format output should compact after short archive marker, got %q", got)
	}
	uri := commandOutputFirstArchiveURI(fmtStdout.String())
	if uri == "" {
		t.Fatalf("missing yarn format archive marker in %q", fmtStdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand yarn format archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("> app@1.0.0 format:check /repo")) ||
		!bytes.Contains(raw, []byte("All matched files use Prettier code style!")) {
		t.Fatalf("archive did not preserve yarn format raw output: %q", raw)
	}
}

func TestCommandOutputFirstShimDirectFormatListCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var files strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&files, "internal/generated/pkg_%03d/file_%03d.go\n", i, i)
	}
	realGofmt := writeFakeCommand(t, "gofmt", "#!/bin/sh\ncat <<'EOF'\n"+files.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=gofmt", "--real-bin=" + realGofmt, "--", "-l", "internal/generated"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("gofmt rc=%d stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[gofmt] 80 file(s) formatted") ||
		!strings.Contains(got, "internal/generated/pkg_000/file_000.go") ||
		!strings.Contains(got, "[+") {
		t.Fatalf("unexpected gofmt compact output: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing gofmt archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand gofmt archive: %v", err)
	}
	if bytes.Count(raw, []byte("internal/generated/pkg_")) != 80 {
		t.Fatalf("archive did not preserve all gofmt file rows: %q", raw)
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
	if !ok || !strings.Contains(run.Command, "[command-output-first:gofmt] gofmt -l internal/generated") {
		t.Fatalf("missing gofmt accounting row: ok=%v run=%+v", ok, run)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive gofmt accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimSQLTableCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var table strings.Builder
	table.WriteString(" id | name        | email\n")
	table.WriteString("----+-------------+-----------------------------\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&table, " %3d | user_%03d    | user_%03d@example.com\n", i, i, i)
	}
	table.WriteString("(120 rows)\n")
	realPsql := writeFakeCommand(t, "psql", "#!/bin/sh\ncat <<'EOF'\n"+table.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=psql", "--real-bin=" + realPsql, "--", "-c", "select * from users"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("psql rc=%d stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if strings.Contains(got, "----+----") ||
		!strings.Contains(got, "id | name | email") ||
		!strings.Contains(got, "user_119@example.com") ||
		!strings.Contains(got, "(120 rows)") {
		t.Fatalf("unexpected psql compact output: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing psql archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand psql archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("----+-------------+")) ||
		bytes.Count(raw, []byte("user_")) != 240 {
		t.Fatalf("archive did not preserve raw psql table: %q", raw)
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
	if !ok || !strings.Contains(run.Command, "[command-output-first:psql] psql -c select * from users") {
		t.Fatalf("missing psql accounting row: ok=%v run=%+v", ok, run)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive psql accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimSQLiteTableCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var table strings.Builder
	table.WriteString("id  | name        | email\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&table, "%-3d | user_%03d    | user_%03d@example.com\n", i, i, i)
	}
	realSQLite := writeFakeCommand(t, "sqlite3", "#!/bin/sh\ncat <<'EOF'\n"+table.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=sqlite3", "--real-bin=" + realSQLite, "--", "db.sqlite", "select * from users"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("sqlite3 rc=%d stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "id | name | email") ||
		!strings.Contains(got, "user_119@example.com") {
		t.Fatalf("unexpected sqlite compact output: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing sqlite archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand sqlite archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("id  | name        | email")) ||
		bytes.Count(raw, []byte("user_")) != 240 {
		t.Fatalf("archive did not preserve raw sqlite table: %q", raw)
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
	if !ok || !strings.Contains(run.Command, "[command-output-first:sqlite3] sqlite3 db.sqlite select * from users") {
		t.Fatalf("missing sqlite accounting row: ok=%v run=%+v", ok, run)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive sqlite accounting row: %+v", run)
	}
}

func commandOutputFirstNextBuildFixture() string {
	var b strings.Builder
	b.WriteString("Next.js 15.3.0\n")
	b.WriteString("Creating an optimized production build ...\n")
	b.WriteString("Compiled successfully in 2.8s\n")
	b.WriteString("Linting and checking validity of types ...\n")
	b.WriteString("Collecting page data ...\n")
	b.WriteString("Generating static pages (0/8) ...\n")
	b.WriteString("Generating static pages (4/8) ...\n")
	b.WriteString("Generating static pages (8/8) ...\n")
	b.WriteString("Finalizing page optimization ...\n")
	b.WriteString("Collecting build traces ...\n")
	b.WriteString("Route (app)                              Size     First Load JS\n")
	for i := 0; i < 24; i++ {
		b.WriteString("/dashboard/section                      2.00 kB        110 kB\n")
	}
	return b.String()
}

func TestCommandOutputFirstShimCargoTestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	b.WriteString("running 42 tests\n")
	for i := 1; i <= 42; i++ {
		b.WriteString("test suite::case_")
		if i < 10 {
			b.WriteString("0")
		}
		b.WriteString(strings.TrimSpace(string(rune('0' + i/10))))
		b.WriteString(strings.TrimSpace(string(rune('0' + i%10))))
		b.WriteString(" ... ok\n")
	}
	b.WriteString("\ntest result: ok. 42 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s\n")
	realCargo := writeFakeCommand(t, "cargo", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=cargo", "--real-bin=" + realCargo, "--", "test", "--", "--nocapture"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[cargo test] ok - 42 passed") || !strings.Contains(got, "test result: ok. 42 passed") {
		t.Fatalf("unexpected compacted cargo test stdout=%q", got)
	}
	if strings.Contains(got, "suite::case_01") {
		t.Fatalf("cargo test command-output-first should elide redundant pass roll-call: %q", got)
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
	if !strings.Contains(run.Command, "[command-output-first:cargo] cargo test -- --nocapture") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimCargoBuildAndClippyCompact(t *testing.T) {
	var build strings.Builder
	for i := 0; i < 40; i++ {
		build.WriteString("    Compiling slimtest v0.1.0 (/repo/crates/slimtest)\n")
	}
	build.WriteString("    Finished `dev` profile [unoptimized + debuginfo] target(s) in 1.23s\n")
	realCargoBuild := writeFakeCommand(t, "cargo", "#!/bin/sh\ncat <<'EOF'\n"+build.String()+"EOF\n")
	var buildStdout, buildStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=cargo", "--real-bin=" + realCargoBuild, "--", "build", "--workspace"}, &bytes.Buffer{}, &buildStdout, &buildStderr)
	if rc != 0 {
		t.Fatalf("cargo build rc=%d stderr=%q", rc, buildStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(buildStdout.String()); got != "[cargo build] ok\n" {
		t.Fatalf("cargo build compacted stdout=%q", got)
	}

	var clippy strings.Builder
	for i := 0; i < 40; i++ {
		clippy.WriteString("    Checking slimtest v0.1.0 (/repo/crates/slimtest)\n")
	}
	clippy.WriteString("    Finished `dev` profile [unoptimized + debuginfo] target(s) in 1.23s\n")
	realCargoClippy := writeFakeCommand(t, "cargo", "#!/bin/sh\ncat <<'EOF'\n"+clippy.String()+"EOF\n")
	var clippyStdout, clippyStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=cargo", "--real-bin=" + realCargoClippy, "--", "clippy", "--all-targets"}, &bytes.Buffer{}, &clippyStdout, &clippyStderr)
	if rc != 0 {
		t.Fatalf("cargo clippy rc=%d stderr=%q", rc, clippyStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(clippyStdout.String()); got != "[cargo clippy] ok\n" {
		t.Fatalf("cargo clippy compacted stdout=%q", got)
	}
}

func TestCommandOutputFirstShimCargoStderrFullPasses(t *testing.T) {
	realCargo := writeFakeCommand(t, "cargo", "#!/bin/sh\nprintf '    Checking slimtest v0.1.0\\n'\nprintf 'warning: diagnostic\\n' >&2\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=cargo", "--real-bin=" + realCargo, "--", "check"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != "    Checking slimtest v0.1.0\n" {
		t.Fatalf("stdout passthrough=%q", got)
	}
	if got := stderr.String(); got != "warning: diagnostic\n" {
		t.Fatalf("stderr passthrough=%q", got)
	}
}

func TestCommandOutputFirstShimPythonUnittestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 1; i <= 400; i++ {
		b.WriteByte('.')
		if i%40 == 0 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n----------------------------------------------------------------------\n")
	b.WriteString("Ran 400 tests in 0.321s\n\nOK\n")
	realPython := writeFakeCommand(t, "python3", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=python3", "--real-bin=" + realPython, "--", "-u", "-m", "unittest"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if got = commandOutputFirstVisibleOutput(got); got != "[python -m unittest] ok (Ran 400 tests in 0.321s; OK)\n" {
		t.Fatalf("unexpected compacted unittest stdout=%q", got)
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
	if !strings.Contains(run.Command, "[command-output-first:python3] python3 -u -m unittest") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimPytestCompacts(t *testing.T) {
	var b strings.Builder
	b.WriteString("============================= test session starts ==============================\n")
	for i := 0; i < 80; i++ {
		b.WriteString("tests/test_alpha.py::test_op PASSED                                  [ 10%]\n")
	}
	b.WriteString("============================== 80 passed in 0.42s ===============================\n")
	realPytest := writeFakeCommand(t, "pytest", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=pytest", "--real-bin=" + realPytest, "--", "-v"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[pytest] ok - 80 passed") || strings.Contains(got, "test_alpha.py::test_op") {
		t.Fatalf("unexpected compacted pytest stdout=%q", got)
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
	dotnetBin := filepath.Join(binDir, "dotnet")
	if err := os.WriteFile(dotnetBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	kubectlBin := filepath.Join(binDir, "kubectl")
	if err := os.WriteFile(kubectlBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	terraformBin := filepath.Join(binDir, "terraform")
	if err := os.WriteFile(terraformBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ghBin := filepath.Join(binDir, "gh")
	if err := os.WriteFile(ghBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	plocateBin := filepath.Join(binDir, "plocate")
	if err := os.WriteFile(plocateBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	treeBin := filepath.Join(binDir, "tree")
	if err := os.WriteFile(treeBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	osExecutable = func() (string, error) { return self, nil }
	t.Setenv("PATH", binDir)

	for _, mode := range []string{"proxied", "proxied-wss", "proxied-wss-bridge", "transparent-proxied"} {
		command := codexEnvCommand(mode, "127.0.0.1", "8990", []string{"exec", "hi"})
		got, cleanup := maybeApplyCommandOutputFirstEnv(mode, command)
		defer cleanup()
		joined := strings.Join(got, "\x00")
		if !strings.Contains(joined, "\x00"+commandOutputFirstActiveEnv+"=1\x00") {
			t.Fatalf("%s missing active env in %#v", mode, got)
		}
		if !strings.Contains(joined, "\x00"+commandOutputFirstSessionEnv+"=cof-") {
			t.Fatalf("%s missing command-output-first session env in %#v", mode, got)
		}
		if !strings.Contains(joined, "\x00BASH_ENV=") {
			t.Fatalf("%s missing BASH_ENV in %#v", mode, got)
		}
	}

	command := codexEnvCommand("transparent-proxied", "127.0.0.1", "8990", []string{"exec", "hi"})
	got, cleanup := maybeApplyCommandOutputFirstEnv("transparent-proxied", command)
	defer cleanup()
	pathValue := envValueInCommand(t, got, "PATH")
	if !strings.Contains(pathValue, string(os.PathListSeparator)+binDir) {
		t.Fatalf("PATH did not preserve original path: %q", pathValue)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pathValue, string(os.PathListSeparator))[0], "git")); err != nil {
		t.Fatalf("git shim missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pathValue, string(os.PathListSeparator))[0], "dotnet")); err != nil {
		t.Fatalf("dotnet shim missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pathValue, string(os.PathListSeparator))[0], "kubectl")); err != nil {
		t.Fatalf("kubectl shim missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pathValue, string(os.PathListSeparator))[0], "terraform")); err != nil {
		t.Fatalf("terraform shim missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pathValue, string(os.PathListSeparator))[0], "gh")); err != nil {
		t.Fatalf("gh shim missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pathValue, string(os.PathListSeparator))[0], "plocate")); err != nil {
		t.Fatalf("plocate shim missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pathValue, string(os.PathListSeparator))[0], "tree")); err != nil {
		t.Fatalf("tree shim missing: %v", err)
	}
	bashEnv := envValueInCommand(t, got, "BASH_ENV")
	bashEnvContent, err := os.ReadFile(bashEnv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bashEnvContent), "export PATH=") || !strings.Contains(string(bashEnvContent), "${PATH:+:$PATH}") {
		t.Fatalf("BASH_ENV does not re-prepend shim PATH: %q", bashEnvContent)
	}
	if !strings.Contains(string(bashEnvContent), "export "+commandOutputFirstSessionEnv+"=") {
		t.Fatalf("BASH_ENV missing command-output-first session export: %q", bashEnvContent)
	}
}

func TestPrepareCommandOutputFirstEnvFailOpenBoundaries(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() { osExecutable = oldExecutable })

	osExecutable = func() (string, error) { return "", errors.New("missing self") }
	if env, cleanup, ok := prepareCommandOutputFirstEnv(); ok || env != nil {
		cleanup()
		t.Fatalf("osExecutable error must fail open without env, ok=%v env=%v", ok, env)
	}

	osExecutable = func() (string, error) { return "   ", nil }
	if env, cleanup, ok := prepareCommandOutputFirstEnv(); ok || env != nil {
		cleanup()
		t.Fatalf("blank self path must fail open without env, ok=%v env=%v", ok, env)
	}

	binDir := t.TempDir()
	self := filepath.Join(binDir, "slimference")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	osExecutable = func() (string, error) { return self, nil }
	t.Setenv("PATH", t.TempDir())
	if env, cleanup, ok := prepareCommandOutputFirstEnv(); ok || env != nil {
		cleanup()
		t.Fatalf("no real command binaries must fail open without env, ok=%v env=%v", ok, env)
	}
}

func TestRecordCommandOutputFirstRunFailOpenAndMissingCWD(t *testing.T) {
	oldPath := resolveFilterDBPathFn
	oldMkdirAll := osMkdirAll
	oldGetwd := osGetwd
	t.Cleanup(func() {
		resolveFilterDBPathFn = oldPath
		osMkdirAll = oldMkdirAll
		osGetwd = oldGetwd
	})

	raw := []byte(strings.Repeat("raw-output-line\n", 80))
	compacted := []byte("[terraform] compact\n")

	resolveFilterDBPathFn = func() (string, error) { return "", errors.New("db path unavailable") }
	recordCommandOutputFirstRun("terraform", []string{"plan"}, raw, compacted)

	resolveFilterDBPathFn = func() (string, error) { return " ", nil }
	recordCommandOutputFirstRun("terraform", []string{"plan"}, raw, compacted)

	dbPath := filepath.Join(t.TempDir(), "filter.db")
	resolveFilterDBPathFn = func() (string, error) { return dbPath, nil }
	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir failed") }
	recordCommandOutputFirstRun("terraform", []string{"plan"}, raw, compacted)

	osMkdirAll = oldMkdirAll
	osGetwd = func() (string, error) { return "", errors.New("cwd unavailable") }
	recordCommandOutputFirstRun("terraform", []string{"plan"}, raw, compacted)

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
		t.Fatal("expected accounting row despite missing cwd")
	}
	if run.ProjectPath != "" {
		t.Fatalf("missing cwd must record blank project path, got %q", run.ProjectPath)
	}
	if !strings.Contains(run.Command, "[command-output-first:terraform] terraform plan") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestRecordCommandOutputFirstObservationFailOpenAndMissingCWD(t *testing.T) {
	oldPath := resolveFilterDBPathFn
	oldMkdirAll := osMkdirAll
	oldGetwd := osGetwd
	t.Cleanup(func() {
		resolveFilterDBPathFn = oldPath
		osMkdirAll = oldMkdirAll
		osGetwd = oldGetwd
	})

	raw := []byte(strings.Repeat("raw-output-line\n", 80))
	recordCommandOutputFirstObservation("ls", []string{"-lah"}, []byte("tiny"), nil, commandOutputFirstObservationFullPass)

	resolveFilterDBPathFn = func() (string, error) { return "", errors.New("db path unavailable") }
	recordCommandOutputFirstObservation("ls", []string{"-lah"}, raw, nil, commandOutputFirstObservationFullPass)

	resolveFilterDBPathFn = func() (string, error) { return " ", nil }
	recordCommandOutputFirstObservation("ls", []string{"-lah"}, raw, nil, commandOutputFirstObservationFullPass)

	dbPath := filepath.Join(t.TempDir(), "filter.db")
	resolveFilterDBPathFn = func() (string, error) { return dbPath, nil }
	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir failed") }
	recordCommandOutputFirstObservation("ls", []string{"-lah"}, raw, nil, commandOutputFirstObservationFullPass)

	osMkdirAll = oldMkdirAll
	osGetwd = func() (string, error) { return "", errors.New("cwd unavailable") }
	recordCommandOutputFirstObservation("ls", []string{"-lah"}, raw, nil, commandOutputFirstObservationFullPass)

	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := filter.QueryFilterObservationByCommand(db, commandOutputFirstObservationScope, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Command != "[command-output-first:ls] ls -lah" || rows[0].Outcome != commandOutputFirstObservationFullPass {
		t.Fatalf("observation rows=%+v", rows)
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
	for _, flag := range []string{"--proxied", "--proxied-wss", "--proxied-wss-bridge", "--transparent-proxied"} {
		gotName = ""
		gotArgs = nil
		if rc := proxyRun([]string{"run", "codex", flag, "--", "exec", "hi"}, env); rc != 0 {
			t.Fatalf("%s rc=%d stderr=%q", flag, rc, stderr.String())
		}
		if gotName != "env" {
			t.Fatalf("%s runner name=%q", flag, gotName)
		}
		joined := strings.Join(gotArgs, "\x00")
		if !strings.Contains(joined, commandOutputFirstActiveEnv+"=1") || !strings.Contains(joined, "BASH_ENV=") {
			t.Fatalf("%s scoped run missing command-output-first env: %#v", flag, gotArgs)
		}
		if !strings.Contains(joined, "\x00codex\x00") {
			t.Fatalf("%s codex command missing: %#v", flag, gotArgs)
		}
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
	withCommandOutputFirstArchiveHome(t)
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
	var showStat strings.Builder
	showStat.WriteString("commit a1b2c3d4e5f6a7b8\n\n    Subject\n\n")
	for i := 0; i < 40; i++ {
		showStat.WriteString(fmt.Sprintf(" internal/proxy/generated/very/deep/path/file_%02d.go | 10 +++++-----\n", i))
	}
	showStat.WriteString(" 40 files changed, 200 insertions(+), 200 deletions(-)\n")
	if out, ok := compactCommandOutputFirst("git", "/usr/bin/git", []string{"show", "--stat", "HEAD"}, []byte(showStat.String()), nil, 0); !ok || !strings.Contains(string(out), "[git show --stat]") {
		t.Fatalf("git show --stat did not compact: out=%q ok=%v", out, ok)
	}
	var showNameOnly strings.Builder
	showNameOnly.WriteString("commit a1b2c3d4e5f6a7b8\n\n    Subject\n\n")
	for i := 0; i < 40; i++ {
		showNameOnly.WriteString(fmt.Sprintf("internal/proxy/generated/very/deep/path/file_%02d.go\n", i))
	}
	if out, ok := compactCommandOutputFirst("git", "/usr/bin/git", []string{"show", "--name-only", "HEAD"}, []byte(showNameOnly.String()), nil, 0); !ok || !strings.Contains(string(out), "[git show --name-only paths]") {
		t.Fatalf("git show --name-only did not compact: out=%q ok=%v", out, ok)
	}
	var showNameStatus strings.Builder
	showNameStatus.WriteString("commit a1b2c3d4e5f6a7b8\n\n    Subject\n\n")
	for i := 0; i < 40; i++ {
		showNameStatus.WriteString(fmt.Sprintf("M\tinternal/proxy/generated/very/deep/path/file_%02d.go\n", i))
	}
	if out, ok := compactCommandOutputFirst("git", "/usr/bin/git", []string{"show", "--name-status", "HEAD"}, []byte(showNameStatus.String()), nil, 0); !ok || !strings.Contains(string(out), "[git show --name-status paths]") {
		t.Fatalf("git show --name-status did not compact: out=%q ok=%v", out, ok)
	}
	if out, ok := compactCommandOutputFirst("npm", "/usr/bin/npm", []string{"run", "dev"}, []byte("ready\n"), nil, 0); ok || out != nil {
		t.Fatalf("npm run dev compacted: out=%q ok=%v", out, ok)
	}
}

func TestCommandOutputFirstShimTreeCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var listing strings.Builder
	listing.WriteString(".\n")
	listing.WriteString("├── src\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&listing, "│   ├── generated_file_%02d.go\n", i)
	}
	listing.WriteString("│   └── service.go\n")
	listing.WriteString("└── docs\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&listing, "    ├── guide_%02d.md\n", i)
	}
	listing.WriteString("    └── README.md\n\n")
	listing.WriteString("2 directories, 122 files\n")
	realTree := writeFakeCommand(t, "tree", "#!/bin/sh\ncat <<'EOF'\n"+listing.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=tree", "--real-bin=" + realTree, "--", "-L", "2", "."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("tree rc=%d stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	if !strings.Contains(got, "[tree paths] 124 entries 2 directories 122 files root=.") ||
		!strings.Contains(got, "src/\n") ||
		!strings.Contains(got, "generated_file_79.go") ||
		!strings.Contains(got, "docs/\n") ||
		!strings.Contains(got, "guide_39.md") ||
		strings.Contains(got, "├──") {
		t.Fatalf("unexpected tree compact output: %q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing tree archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand tree archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("├── src")) ||
		bytes.Count(raw, []byte("generated_file_")) != 80 ||
		bytes.Count(raw, []byte("guide_")) != 40 {
		t.Fatalf("archive did not preserve raw tree listing: %q", raw)
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
	if !ok || !strings.Contains(run.Command, "[command-output-first:tree] tree -L 2 .") {
		t.Fatalf("missing tree accounting row: ok=%v run=%+v", ok, run)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive tree accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimRepeatedSedReadCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	t.Setenv(commandOutputFirstSessionEnv, "cof-read-session")
	var body strings.Builder
	for i := 1; i <= 220; i++ {
		fmt.Fprintf(&body, "func sourceLine%03d() string { return \"line-%03d\" }\n", i, i)
	}
	raw := body.String()
	realSed := writeFakeCommand(t, "sed", "#!/bin/sh\ncat <<'EOF'\n"+raw+"EOF\n")

	var firstStdout, firstStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=sed", "--real-bin=" + realSed, "--", "-n", "1,220p", "internal/proxy/wsmitm_phasef.go"}, &bytes.Buffer{}, &firstStdout, &firstStderr)
	if rc != 0 || firstStderr.Len() != 0 {
		t.Fatalf("first sed rc=%d stderr=%q", rc, firstStderr.String())
	}
	if firstStdout.String() != raw {
		t.Fatalf("first sed read must full-pass, got len=%d want=%d", firstStdout.Len(), len(raw))
	}
	if uri := commandOutputFirstArchiveURI(firstStdout.String()); uri != "" {
		t.Fatalf("first read must not emit archive marker, got %q", firstStdout.String())
	}

	var secondStdout, secondStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=sed", "--real-bin=" + realSed, "--", "-n", "1,220p", "internal/proxy/wsmitm_phasef.go"}, &bytes.Buffer{}, &secondStdout, &secondStderr)
	if rc != 0 || secondStderr.Len() != 0 {
		t.Fatalf("second sed rc=%d stderr=%q", rc, secondStderr.String())
	}
	visible := commandOutputFirstVisibleOutput(secondStdout.String())
	if !strings.Contains(visible, `[context-elided kind=file-read status=unchanged path="internal/proxy/wsmitm_phasef.go" archive=local-archive://`) ||
		strings.Contains(visible, "sourceLine220") {
		t.Fatalf("second sed read not compacted as unchanged archive reference: %q", secondStdout.String())
	}
	uri := commandOutputFirstArchiveURI(secondStdout.String())
	if uri == "" {
		t.Fatalf("missing repeated read archive URI in %q", secondStdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand repeated read archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("sourceLine220")) {
		t.Fatalf("archive did not preserve raw repeated read: %q", archived)
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
	if !ok || !strings.Contains(run.Command, "[command-output-first:sed] sed -n 1,220p internal/proxy/wsmitm_phasef.go") {
		t.Fatalf("missing repeated read accounting row: ok=%v run=%+v", ok, run)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive repeated read accounting row: %+v", run)
	}
}

func TestCommandOutputFirstReadDeltaEdges(t *testing.T) {
	if !commandOutputFirstReadAllowed("cat", []string{"internal/proxy/wsmitm_phasef.go"}) {
		t.Fatal("cat file read was not allowed")
	}
	if !commandOutputFirstReadAllowed("head", []string{"-n", "20", "internal/proxy/wsmitm_phasef.go"}) {
		t.Fatal("head range read was not allowed")
	}
	if !commandOutputFirstReadAllowed("awk", []string{"NR>=1&&NR<=5{print}", "internal/proxy/wsmitm_phasef.go"}) {
		t.Fatal("awk range read was not allowed")
	}
	if commandOutputFirstReadAllowed("cat", []string{"app.log"}) {
		t.Fatal("cat .log read was allowed")
	}
	if commandOutputFirstReadAllowed("cat", []string{"-"}) {
		t.Fatal("stdin read was allowed")
	}
	if commandOutputFirstReadAllowed("sed", []string{"s/foo/bar/g", "internal/proxy/wsmitm_phasef.go"}) {
		t.Fatal("non-read sed expression was allowed")
	}

	t.Setenv(commandOutputFirstSessionEnv, "")
	if out, ok := compactCommandOutputFirstReadDelta("sed", []string{"-n", "1,2p", "internal/proxy/wsmitm_phasef.go"}, []byte("line1\nline2\n")); ok || out != nil {
		t.Fatalf("read delta compacted without session: out=%q ok=%v", out, ok)
	}

	t.Setenv(commandOutputFirstSessionEnv, "cof-read-edge")
	if out, ok := compactCommandOutputFirstReadDelta("cat", []string{"app.log"}, []byte("line\n")); ok || out != nil {
		t.Fatalf("log read delta compacted: out=%q ok=%v", out, ok)
	}
	prevHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
	if out, ok := compactCommandOutputFirstReadDelta("cat", []string{"internal/proxy/wsmitm_phasef.go"}, []byte("line\n")); ok || out != nil {
		osUserHomeDir = prevHome
		t.Fatalf("read delta compacted with unavailable home: out=%q ok=%v", out, ok)
	}
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prevHome })
	if out, ok := compactCommandOutputFirstReadDelta("cat", []string{"internal/proxy/wsmitm_phasef.go"}, []byte("first read\n")); ok || out != nil {
		t.Fatalf("first read compacted: out=%q ok=%v", out, ok)
	}
	state, err := readcache.LoadSession(readcache.DefaultDir(home), "cof-read-edge")
	if err != nil {
		t.Fatalf("load flushed read session: %v", err)
	}
	if len(state.Files) == 0 {
		t.Fatalf("first read did not flush seeded file state: %+v", state)
	}
}

func TestCommandOutputFirstShimRepeatedGenericOutputCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	t.Setenv(commandOutputFirstSessionEnv, "cof-output-session")
	var body strings.Builder
	for i := 1; i <= 90; i++ {
		fmt.Fprintf(&body, "plain repeated diagnostic block %03d with enough detail to matter\n", i)
	}
	raw := body.String()
	realJQ := writeFakeCommand(t, "jq", "#!/bin/sh\ncat <<'EOF'\n"+raw+"EOF\n")

	var firstStdout, firstStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=jq", "--real-bin=" + realJQ, "--", "."}, &bytes.Buffer{}, &firstStdout, &firstStderr)
	if rc != 0 || firstStderr.Len() != 0 {
		t.Fatalf("first jq rc=%d stderr=%q", rc, firstStderr.String())
	}
	if firstStdout.String() != raw {
		t.Fatalf("first repeated output seed must full-pass, got len=%d want=%d", firstStdout.Len(), len(raw))
	}

	var secondStdout, secondStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=jq", "--real-bin=" + realJQ, "--", "."}, &bytes.Buffer{}, &secondStdout, &secondStderr)
	if rc != 0 || secondStderr.Len() != 0 {
		t.Fatalf("second jq rc=%d stderr=%q", rc, secondStderr.String())
	}
	visible := commandOutputFirstVisibleOutput(secondStdout.String())
	if !strings.Contains(visible, `[context-elided kind=tool-output status=unchanged command="jq ." archive=local-archive://`) ||
		strings.Contains(visible, "diagnostic block 090") {
		t.Fatalf("second generic output not compacted as unchanged archive reference: %q", secondStdout.String())
	}
	uri := commandOutputFirstArchiveURI(secondStdout.String())
	if uri == "" {
		t.Fatalf("missing repeated output archive URI in %q", secondStdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand repeated output archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("diagnostic block 090")) {
		t.Fatalf("archive did not preserve raw repeated output: %q", archived)
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
	if !ok || !strings.Contains(run.Command, "[command-output-first:jq] jq .") {
		t.Fatalf("missing repeated output accounting row: ok=%v run=%+v", ok, run)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive repeated output accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimRepeatedSearchSameMatchSetCompactsWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	t.Setenv(commandOutputFirstSessionEnv, "cof-search-output-session")
	var beforeLines []string
	for i := 1; i <= 45; i++ {
		beforeLines = append(beforeLines, fmt.Sprintf("src/b.go:%d:needle beta context %s", i+100, strings.Repeat("detail ", 18)))
		beforeLines = append(beforeLines, fmt.Sprintf("src/a.go:%d:needle alpha context %s", i, strings.Repeat("detail ", 18)))
	}
	before := strings.Join(beforeLines, "\n") + "\n"
	var afterLines []string
	for i := 45; i >= 1; i-- {
		afterLines = append(afterLines, fmt.Sprintf("src/a.go:%d:needle alpha context %s", i, strings.Repeat("detail ", 18)))
		afterLines = append(afterLines, fmt.Sprintf("src/b.go:%d:needle beta context %s", i+100, strings.Repeat("detail ", 18)))
	}
	after := strings.Join(afterLines, "\n") + "\n"
	marker := filepath.Join(t.TempDir(), "rg-repeat-seen")
	realRG := writeFakeCommand(t, "rg", "#!/bin/sh\nif [ -f "+shellQuote(marker)+" ]; then\ncat <<'EOF'\n"+after+"EOF\nelse\n: > "+shellQuote(marker)+"\ncat <<'EOF'\n"+before+"EOF\nfi\n")

	var firstStdout, firstStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=rg", "--real-bin=" + realRG, "--", "-n", "needle", "src"}, &bytes.Buffer{}, &firstStdout, &firstStderr)
	if rc != 0 || firstStderr.Len() != 0 {
		t.Fatalf("first rg rc=%d stderr=%q", rc, firstStderr.String())
	}
	if !strings.Contains(firstStdout.String(), "needle alpha context") {
		t.Fatalf("first rg did not expose search evidence: %q", firstStdout.String())
	}

	var secondStdout, secondStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=rg", "--real-bin=" + realRG, "--", "-n", "needle", "src"}, &bytes.Buffer{}, &secondStdout, &secondStderr)
	if rc != 0 || secondStderr.Len() != 0 {
		t.Fatalf("second rg rc=%d stderr=%q", rc, secondStderr.String())
	}
	visible := commandOutputFirstVisibleOutput(secondStdout.String())
	if !strings.Contains(visible, `[context-elided kind=search-output status=same-match-set command="rg -n needle src" archive=local-archive://`) ||
		strings.Contains(visible, "needle beta context") {
		t.Fatalf("second rg same-match-set not compacted: %q", secondStdout.String())
	}
	uri := commandOutputFirstArchiveURI(secondStdout.String())
	if uri == "" {
		t.Fatalf("missing repeated search archive URI in %q", secondStdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand repeated search archive: %v", err)
	}
	if !bytes.Contains(archived, []byte("src/b.go:145:needle beta context")) {
		t.Fatalf("archive did not preserve raw repeated search output: %q", archived)
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
	if !ok || !strings.Contains(run.Command, "[command-output-first:rg] rg -n needle src") {
		t.Fatalf("missing repeated search accounting row: ok=%v run=%+v", ok, run)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive repeated search accounting row: %+v", run)
	}
}

func TestCommandOutputFirstRepeatedOutputEdges(t *testing.T) {
	if !commandOutputFirstReadCommand("sed") {
		t.Fatal("sed must be treated as read command")
	}
	if commandOutputFirstReadCommand("rg") {
		t.Fatal("rg must not be treated as read command")
	}
	if got := commandOutputFirstCommandLine("rg", []string{"-n", "needle", "src"}); got != "rg -n needle src" {
		t.Fatalf("command line=%q", got)
	}
	if got := commandOutputFirstCommandLine("jq", nil); got != "jq" {
		t.Fatalf("command line without args=%q", got)
	}
	if got := commandOutputFirstOutputKey("rg", []string{"-n", "needle", "src"}); !strings.HasPrefix(got, "search:rg\t") {
		t.Fatalf("rg key=%q", got)
	}
	if got := commandOutputFirstOutputKey("git", []string{"grep", "-n", "needle"}); !strings.HasPrefix(got, "search:git\t") {
		t.Fatalf("git grep key=%q", got)
	}
	if got := commandOutputFirstOutputKey("jq", []string{"."}); got != "command:jq ." {
		t.Fatalf("jq key=%q", got)
	}
	if !commandOutputFirstSearchCommand("grep", []string{"-R", "needle", "."}) {
		t.Fatal("grep should be search command")
	}
	if commandOutputFirstSearchCommand("git", []string{"status"}) {
		t.Fatal("git status must not be search command")
	}

	t.Setenv(commandOutputFirstSessionEnv, "")
	if out, ok := compactCommandOutputFirstRepeatedOutput("jq", []string{"."}, []byte(strings.Repeat("line\n", 200))); ok || out != nil {
		t.Fatalf("repeated output compacted without session: out=%q ok=%v", out, ok)
	}

	t.Setenv(commandOutputFirstSessionEnv, "cof-output-edge")
	if out, ok := compactCommandOutputFirstRepeatedOutput("sed", []string{"-n", "1,5p", "file.go"}, []byte(strings.Repeat("line\n", 200))); ok || out != nil {
		t.Fatalf("read command used repeated output fallback: out=%q ok=%v", out, ok)
	}
	prevHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
	if out, ok := compactCommandOutputFirstRepeatedOutput("jq", []string{"."}, []byte(strings.Repeat("line\n", 200))); ok || out != nil {
		osUserHomeDir = prevHome
		t.Fatalf("repeated output compacted with unavailable home: out=%q ok=%v", out, ok)
	}
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prevHome })
	if out, ok := compactCommandOutputFirstRepeatedOutput("jq", []string{"."}, []byte("short\n")); ok || out != nil {
		t.Fatalf("short repeated output compacted: out=%q ok=%v", out, ok)
	}
	if out, ok := compactCommandOutputFirstRepeatedOutput("jq", []string{"."}, []byte(strings.Repeat("first seed\n", 80))); ok || out != nil {
		t.Fatalf("first output seed compacted: out=%q ok=%v", out, ok)
	}
	state, err := readcache.LoadSession(readcache.DefaultDir(home), "cof-output-edge")
	if err != nil {
		t.Fatalf("load flushed output session: %v", err)
	}
	if len(state.Outputs) == 0 {
		t.Fatalf("first output did not flush seeded output state: %+v", state)
	}
}

func TestCommandOutputFirstMixedCompactionRejectsNonPositiveAndUnknownStream(t *testing.T) {
	if got, ok := commandOutputFirstMixedCompaction("stdout", []byte("short\n"), []byte("warn\n"), []byte("short\n")); ok {
		t.Fatalf("non-positive mixed stdout compacted: %+v", got)
	}
	if got, ok := commandOutputFirstMixedCompaction("stderr", []byte("note\n"), []byte("short\n"), []byte("short\n")); ok {
		t.Fatalf("non-positive mixed stderr compacted: %+v", got)
	}
	if got, ok := commandOutputFirstMixedCompaction("weird", []byte("note\n"), []byte("diag\n"), []byte("compact\n")); ok {
		t.Fatalf("unknown mixed stream compacted: %+v", got)
	}
}

func TestCommandOutputFirstDiagnosticPredicatesNpxAndDeny(t *testing.T) {
	if !commandOutputFirstFocusedLintDiagnosticAllowed("npx", []string{"--yes", "staticcheck", "./..."}) {
		t.Fatal("npx staticcheck diagnostic should be allowed")
	}
	if commandOutputFirstFocusedLintDiagnosticAllowed("npx", []string{"--yes", "eslint", "src"}) {
		t.Fatal("npx eslint must not use focused Go lint diagnostic path")
	}
	if !commandOutputFirstEslintStylishDiagnosticAllowed("npx", []string{"--yes", "eslint", "src"}) {
		t.Fatal("npx eslint stylish diagnostic should be allowed")
	}
	if commandOutputFirstEslintStylishDiagnosticAllowed("pnpm", []string{"exec", "eslint", "src"}) {
		t.Fatal("only direct eslint or strict npx eslint should be allowed in command-output-first")
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

func TestCommandOutputFirstGoSubcommandGlobalOptionEdges(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"test", "./..."}, want: "test"},
		{args: []string{"-C", "/repo", "build", "./cmd/slimference"}, want: "build"},
		{args: []string{"-C=/repo", "vet", "./..."}, want: "vet"},
		{args: []string{"-C"}, want: ""},
		{args: []string{"-C=/repo", "test"}, want: "test"},
		{args: []string{"-mod=mod", "test"}, want: "test"},
		{args: []string{"env"}, want: "env"},
		{args: []string{""}, want: ""},
	}
	for _, tc := range cases {
		if got := commandOutputFirstGoSubcommand(tc.args); got != tc.want {
			t.Fatalf("args=%v got=%q want=%q", tc.args, got, tc.want)
		}
	}
}

func TestCommandOutputFirstPathListAndWcEdges(t *testing.T) {
	pathListAllowed := []struct {
		command string
		args    []string
	}{
		{command: "rg", args: []string{"--files", "--hidden", "-g", "*.go", "internal"}},
		{command: "fd", args: []string{"--extension", "go", "internal"}},
		{command: "fdfind", args: []string{"-e", "go", "internal"}},
		{command: "find", args: []string{"internal", "-maxdepth", "4", "-type", "f"}},
		{command: "plocate", args: []string{"generated"}},
		{command: "locate", args: []string{"-i", "--limit=100", "generated"}},
		{command: "locate", args: []string{"-d", "/var/db/locate.database", "-l50", "--", "generated"}},
		{command: "tree", args: []string{"-L", "2", "."}},
		{command: "tree", args: []string{"--dirsfirst", "--charset=ascii", "-L2", "--", "internal/proxy"}},
	}
	for _, tc := range pathListAllowed {
		if !commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v should be captured", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "fd", args: []string{"--exec", "rm", "{}"}},
		{command: "find", args: []string{"internal", "-type", "f"}},
		{command: "find", args: []string{"internal", "-maxdepth", "9", "-type", "f"}},
		{command: "find", args: []string{"internal", "-maxdepth", "4", "-printf", "%p\n"}},
		{command: "plocate", args: []string{}},
		{command: "plocate", args: []string{"--null", "generated"}},
		{command: "locate", args: []string{"--count", "generated"}},
		{command: "locate", args: []string{"--statistics"}},
		{command: "locate", args: []string{"--help"}},
		{command: "locate", args: []string{"--database"}},
		{command: "locate", args: []string{"--database="}},
		{command: "locate", args: []string{"-l"}},
		{command: "locate", args: []string{"-l", ""}},
		{command: "locate", args: []string{"--", ""}},
		{command: "locate", args: []string{"--unknown", "generated"}},
		{command: "tree", args: nil},
		{command: "tree", args: []string{"."}},
		{command: "tree", args: []string{"-L", "9", "."}},
		{command: "tree", args: []string{"-L", "2", "--du", "."}},
		{command: "tree", args: []string{"-f", "-L", "2", "."}},
		{command: "wc", args: []string{"-l"}},
		{command: "wc", args: []string{"--files0-from=list"}},
		{command: "wc", args: []string{"-q", "file.go"}},
	}
	for _, tc := range denied {
		if commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v must not be captured", tc.command, tc.args)
		}
	}
	for _, args := range [][]string{
		{"file.go"},
		{"-l", "file.go"},
		{"--lines", "--words", "file.go"},
		{"-lw", "--", "-leading-name.txt"},
	} {
		if !commandOutputFirstWcAllowed(args) {
			t.Fatalf("wc %v should be allowed", args)
		}
	}
}

func TestCommandOutputFirstCargoSubcommandEdges(t *testing.T) {
	cases := []struct {
		args    []string
		wantSub string
		wantOK  bool
	}{
		{args: []string{"test", "--workspace"}, wantSub: "test", wantOK: true},
		{args: []string{"+nightly", "nextest", "run"}, wantSub: "nextest", wantOK: true},
		{args: []string{"+nightly", "nextest", "list"}, wantSub: "nextest", wantOK: false},
		{args: []string{"--config", "build.jobs=1", "check"}, wantSub: "check", wantOK: true},
		{args: []string{"--config=build.jobs=1", "doc"}, wantSub: "doc", wantOK: true},
		{args: []string{"-Z", "unstable-options", "llvm-cov"}, wantSub: "llvm-cov", wantOK: true},
		{args: []string{"-Zunstable-options", "-v", "audit"}, wantSub: "audit", wantOK: true},
		{args: []string{"--config"}, wantSub: "", wantOK: false},
		{args: []string{"+stable"}, wantSub: "", wantOK: false},
		{args: []string{"install", "ripgrep"}, wantSub: "install", wantOK: false},
		{args: []string{""}, wantSub: "", wantOK: false},
	}
	for _, tc := range cases {
		if got := commandOutputFirstCargoSubcommand(tc.args); got != tc.wantSub {
			t.Fatalf("cargo args=%v sub=%q want %q", tc.args, got, tc.wantSub)
		}
		if got := commandOutputFirstCargoAllowed(tc.args); got != tc.wantOK {
			t.Fatalf("cargo args=%v allowed=%v want %v", tc.args, got, tc.wantOK)
		}
	}
}

func TestCommandOutputFirstPythonTestEdges(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{command: "pytest", args: []string{"-vv"}},
		{command: "py.test", args: []string{"tests/"}},
		{command: "python3", args: []string{"-u", "-m", "pytest"}},
		{command: "python", args: []string{"-W", "ignore", "-m", "unittest"}},
		{command: "python3", args: []string{"-X", "dev", "-m", "unittest"}},
		{command: "uv", args: []string{"--project", "app", "run", "pytest"}},
		{command: "uv", args: []string{"run", "python3", "-m", "pytest"}},
		{command: "poetry", args: []string{"run", "pytest"}},
		{command: "poetry", args: []string{"run", "python", "-m", "pytest"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstPythonTestAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v should be allowed", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "python3", args: []string{"script.py"}},
		{command: "python3", args: []string{"-m", "http.server"}},
		{command: "python3", args: []string{"-W"}},
		{command: "python3", args: []string{"-X", ""}},
		{command: "uv", args: []string{"run", "python", "script.py"}},
		{command: "uv", args: []string{"run"}},
		{command: "uv", args: []string{"pip", "install", "pytest"}},
		{command: "poetry", args: []string{"install"}},
		{command: "poetry", args: []string{"run", "python", "script.py"}},
		{command: "ruby", args: []string{"-e", "puts 1"}},
	}
	for _, tc := range denied {
		if commandOutputFirstPythonTestAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v must not be allowed", tc.command, tc.args)
		}
	}
	if got := commandOutputFirstPythonModule([]string{"-u", "-m", "pytest"}); got != "pytest" {
		t.Fatalf("python module=%q", got)
	}
}

func TestCommandOutputFirstDirectTestEdges(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{command: "karma", args: []string{"--config", "karma.conf.js", "start"}},
		{command: "playwright", args: []string{"--config", "playwright.config.ts", "test"}},
		{command: "cypress", args: []string{"--config", "video=false", "run"}},
		{command: "wdio", args: []string{"--config", "wdio.conf.ts", "run"}},
		{command: "turbo", args: []string{"test"}},
		{command: "turbo", args: []string{"run", "test", "--filter", "web"}},
		{command: "nox", args: []string{"--session=test"}},
		{command: "tox", args: []string{"--env=test"}},
		{command: "tox", args: []string{"-e=test"}},
		{command: "mill", args: []string{"test"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstDirectTestAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v should be allowed", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "karma", args: []string{"--config"}},
		{command: "playwright", args: []string{"--project", "", "test"}},
		{command: "cypress", args: []string{"--", "open"}},
		{command: "wdio", args: []string{"config"}},
		{command: "turbo", args: []string{"run", "--filter", "web", "build"}},
		{command: "turbo", args: []string{"prune"}},
		{command: "nox", args: []string{"--session"}},
		{command: "nox", args: []string{"--session=lint"}},
		{command: "tox", args: []string{"--env"}},
		{command: "tox", args: []string{"--env=lint"}},
		{command: "mill", args: []string{""}},
		{command: "mill", args: []string{"foo.compile"}},
		{command: "unknown", args: []string{"test"}},
	}
	for _, tc := range denied {
		if commandOutputFirstDirectTestAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v must not be allowed", tc.command, tc.args)
		}
	}

	if got := commandOutputFirstFirstNonOption([]string{"--config", "cfg", "--", "test"}); got != "test" {
		t.Fatalf("first non-option=%q", got)
	}
	if got := commandOutputFirstFirstNonOption(nil); got != "" {
		t.Fatalf("empty args first command=%q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{""}); got != "" {
		t.Fatalf("blank arg first command=%q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--"}); got != "" {
		t.Fatalf("bare separator first command=%q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--", "test"}); got != "test" {
		t.Fatalf("separator first command=%q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--config"}); got != "" {
		t.Fatalf("missing option value should return empty first command, got %q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--config", "", "test"}); got != "" {
		t.Fatalf("blank option value should return empty first command, got %q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--verbose", "test"}); got != "test" {
		t.Fatalf("flag before test first command=%q", got)
	}
}

func TestCommandOutputFirstDirectBuildEdges(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{command: "make", args: []string{"-j8"}},
		{command: "ninja", args: []string{"-C", "build"}},
		{command: "cmake", args: []string{"--build", "build"}},
		{command: "tsc", args: []string{"--noEmit"}},
		{command: "next", args: []string{"--turbo", "build"}},
		{command: "vite", args: []string{"--config", "vite.config.ts", "build"}},
		{command: "webpack", args: []string{"--mode", "production"}},
		{command: "webpack-cli", args: []string{"--mode", "production"}},
		{command: "tsup", args: nil},
		{command: "rollup", args: []string{"--config", "rollup.config.mjs"}},
		{command: "esbuild", args: []string{"src/index.ts", "--bundle=true"}},
		{command: "nx", args: []string{"--project", "web", "build"}},
		{command: "turbo", args: []string{"build"}},
		{command: "turbo", args: []string{"run", "build", "--filter", "web"}},
		{command: "mvn", args: []string{"--batch-mode", "package"}},
		{command: "mvnw", args: []string{"-q", "install"}},
		{command: "gradlew", args: []string{"build", "--parallel"}},
		{command: "moon", args: []string{"run", "build"}},
		{command: "moon", args: []string{"run", "web:build"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstDirectBuildAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v should be allowed", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "make", args: []string{"dev"}},
		{command: "ninja", args: []string{"-t", "commands"}},
		{command: "cmake", args: []string{"--build", "build", "--target", "serve"}},
		{command: "tsc", args: []string{"-w"}},
		{command: "next", args: []string{"dev"}},
		{command: "next", args: nil},
		{command: "vite", args: []string{"--host", "127.0.0.1"}},
		{command: "webpack", args: []string{"serve"}},
		{command: "tsup", args: []string{"--watch"}},
		{command: "rspack", args: []string{"serve"}},
		{command: "parcel", args: []string{"watch", "src/index.html"}},
		{command: "rollup", args: []string{"--watch", "--config"}},
		{command: "rollup", args: nil},
		{command: "esbuild", args: []string{"src/index.ts"}},
		{command: "nx", args: []string{"build", "--watch"}},
		{command: "turbo", args: []string{"run", "--filter", "web", "dev"}},
		{command: "turbo", args: []string{"prune"}},
		{command: "mvn", args: nil},
		{command: "mvn", args: []string{"--batch-mode"}},
		{command: "mvn", args: []string{""}},
		{command: "mvn", args: []string{"release"}},
		{command: "gradle", args: []string{"build", "--continuous"}},
		{command: "meson", args: []string{"setup", "build"}},
		{command: "moon", args: []string{"run"}},
		{command: "moon", args: []string{"run", "web:test"}},
		{command: "unknown", args: []string{"build"}},
	}
	for _, tc := range denied {
		if commandOutputFirstDirectBuildAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v must not be allowed", tc.command, tc.args)
		}
	}
}

func TestCommandOutputFirstPackageScriptEdges(t *testing.T) {
	allowed := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "npm global flag test", command: "npm", args: []string{"--silent", "test"}},
		{name: "pnpm cwd run test", command: "pnpm", args: []string{"--dir", "app", "run", "test"}},
		{name: "yarn run option build", command: "yarn", args: []string{"run", "--silent", "build"}},
		{name: "npm inline prefix test", command: "npm", args: []string{"--prefix=app", "test"}},
		{name: "bun direct test", command: "bun", args: []string{"--cwd", "app", "test"}},
		{name: "bun run build", command: "bun", args: []string{"run", "build"}},
		{name: "npm run lint", command: "npm", args: []string{"run", "lint"}},
		{name: "pnpm run typecheck", command: "pnpm", args: []string{"run", "typecheck"}},
		{name: "yarn run format check", command: "yarn", args: []string{"run", "format:check"}},
	}
	for _, tc := range allowed {
		t.Run("allow "+tc.name, func(t *testing.T) {
			if !commandOutputFirstPackageScriptAllowed(tc.command, tc.args) {
				t.Fatalf("%s %v should be allowed", tc.command, tc.args)
			}
		})
	}

	denied := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "unknown command", command: "deno", args: []string{"test"}},
		{name: "npm run without script", command: "npm", args: []string{"run"}},
		{name: "npm empty args", command: "npm", args: []string{""}},
		{name: "pnpm missing option value", command: "pnpm", args: []string{"--dir"}},
		{name: "bun run test", command: "bun", args: []string{"run", "test"}},
		{name: "yarn run write format", command: "yarn", args: []string{"run", "format"}},
	}
	for _, tc := range denied {
		t.Run("deny "+tc.name, func(t *testing.T) {
			if commandOutputFirstPackageScriptAllowed(tc.command, tc.args) {
				t.Fatalf("%s %v must not be allowed", tc.command, tc.args)
			}
		})
	}
	if commandOutputFirstPackageScriptIsBuild("npm", []string{"test"}) {
		t.Fatal("npm test must not be classified as build")
	}
	if commandOutputFirstPackageScriptIsBuild("deno", []string{"run", "build"}) {
		t.Fatal("unknown package manager must not be classified as build")
	}
	if commandOutputFirstPackageScriptIsTest("bun", []string{"run", "test"}) {
		t.Fatal("bun run test must stay out until package-script semantics are explicitly supported")
	}
}

func TestCommandOutputFirstPackageScriptFilterArgs(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		want    []string
	}{
		{command: "npm", args: []string{"--silent", "run", "test"}, want: []string{"run", "test"}},
		{command: "pnpm", args: []string{"--dir", "app", "run", "test"}, want: []string{"run", "test"}},
		{command: "yarn", args: []string{"run", "--silent", "build"}, want: []string{"run", "build"}},
		{command: "bun", args: []string{"--cwd", "app", "test"}, want: []string{"test"}},
		{command: "deno", args: []string{"--quiet", "test"}, want: []string{"--quiet", "test"}},
		{command: "npm", args: []string{"--dir"}, want: []string{"--dir"}},
	}
	for _, tc := range cases {
		got := commandOutputFirstPackageScriptFilterArgs(tc.command, tc.args)
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Fatalf("%s %v got %v want %v", tc.command, tc.args, got, tc.want)
		}
	}
}

func TestPackageScriptParserEdges(t *testing.T) {
	firstCommandCases := []struct {
		args      []string
		wantVerb  string
		wantIndex int
	}{
		{args: []string{"--"}, wantVerb: "", wantIndex: -1},
		{args: []string{"--", ""}, wantVerb: "", wantIndex: -1},
		{args: []string{"--", "test"}, wantVerb: "test", wantIndex: 1},
		{args: []string{"--dir"}, wantVerb: "", wantIndex: -1},
		{args: []string{"--dir", ""}, wantVerb: "", wantIndex: -1},
		{args: []string{""}, wantVerb: "", wantIndex: -1},
		{args: []string{"--prefix=app", "test"}, wantVerb: "test", wantIndex: 1},
	}
	for _, tc := range firstCommandCases {
		gotVerb, gotIndex := packageScriptFirstCommand(tc.args)
		if gotVerb != tc.wantVerb || gotIndex != tc.wantIndex {
			t.Fatalf("first command args=%v got=(%q,%d) want=(%q,%d)", tc.args, gotVerb, gotIndex, tc.wantVerb, tc.wantIndex)
		}
	}

	runNameCases := []struct {
		args []string
		want string
	}{
		{args: []string{"test"}, want: ""},
		{args: []string{"run", "", "test"}, want: ""},
		{args: []string{"run", "--silent", "build"}, want: "build"},
		{args: []string{"run", "--if-present", "test"}, want: "test"},
	}
	for _, tc := range runNameCases {
		if got := packageRunScriptName(tc.args); got != tc.want {
			t.Fatalf("run script args=%v got=%q want=%q", tc.args, got, tc.want)
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
	withCommandOutputFirstArchiveHome(t)
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

func withCommandOutputFirstArchiveHome(t *testing.T) string {
	t.Helper()
	oldHome := osUserHomeDir
	home := filepath.Join(t.TempDir(), "home")
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = oldHome })
	return home
}

func commandOutputFirstVisibleOutput(output string) string {
	idx := strings.Index(output, "\n[archive local-archive://")
	if idx < 0 {
		idx = strings.Index(output, "\n[context-archive kind=tool-output uri=local-archive://")
	}
	if idx < 0 {
		return output
	}
	return strings.TrimRight(output[:idx], "\n") + "\n"
}

func commandOutputFirstArchiveURI(output string) string {
	archiveIdx := strings.Index(output, "local-archive://")
	if archiveIdx >= 0 {
		rest := output[archiveIdx:]
		end := strings.IndexAny(rest, " ;]\n\t\"")
		if end < 0 {
			return strings.TrimSpace(rest)
		}
		return strings.TrimSpace(rest[:end])
	}
	marker := "uri="
	idx := strings.Index(output, marker)
	if idx < 0 {
		return ""
	}
	rest := output[idx+len(marker):]
	end := strings.IndexAny(rest, " ]")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

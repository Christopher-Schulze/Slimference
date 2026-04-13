package filter

import (
	"strings"
	"testing"
)

func TestTryCompactGitStatus_porcelainCounts(t *testing.T) {
	t.Parallel()
	argv := []string{"git", "status"}
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "staged only",
			in:   "M  cmd/foo.go\n",
			want: "[git status] 1 paths (staged:1 worktree:0 untracked:0)\n",
			ok:   true,
		},
		{
			name: "worktree only",
			in:   " M README.md\n",
			want: "[git status] 1 paths (staged:0 worktree:1 untracked:0)\n",
			ok:   true,
		},
		{
			name: "staged and worktree",
			in:   "MM both.go\n",
			want: "[git status] 1 paths (staged:1 worktree:1 untracked:0)\n",
			ok:   true,
		},
		{
			name: "untracked",
			in:   "?? new.txt\n",
			want: "[git status] 1 paths (staged:0 worktree:0 untracked:1)\n",
			ok:   true,
		},
		{
			name: "branch and mixed",
			in:   "## main...origin/main [ahead 1]\n M a.go\n?? b\n",
			want: "[git status] 2 paths (staged:0 worktree:1 untracked:1)\n",
			ok:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactGitStatus(argv, []byte(tt.in))
			if ok != tt.ok {
				t.Fatalf("ok=%v", ok)
			}
			if string(out) != tt.want {
				t.Fatalf("got %q want %q", out, tt.want)
			}
		})
	}
}

func TestTryCompactGitF05(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactGitF05([]string{"git", "add", "."}, []byte("  \n"))
	if !ok || string(out) != "[git add] ok\n" {
		t.Fatalf("empty add: ok=%v %q", ok, out)
	}
	if _, ok := TryCompactGitF05([]string{"git", "status"}, []byte("")); ok {
		t.Fatal("status not F05")
	}
	out2, ok := TryCompactGitF05([]string{"git", "push"}, []byte("Everything up-to-date\n"))
	if !ok || string(out2) != "[git push] up to date\n" {
		t.Fatalf("push: %q", out2)
	}
	out3, ok := TryCompactGitF05([]string{"git", "pull"}, []byte("Already up to date.\n"))
	if !ok || string(out3) != "[git pull] up to date\n" {
		t.Fatalf("pull: %q", out3)
	}
	out4, ok := TryCompactGitF05([]string{"git", "merge", "main"}, []byte("Already up to date.\n"))
	if !ok || string(out4) != "[git merge] up to date\n" {
		t.Fatalf("merge: %q", out4)
	}
	out5, ok := TryCompactGitF05([]string{"git", "rebase", "origin/main"}, []byte("Current branch foo is up to date.\n"))
	if !ok || string(out5) != "[git rebase] up to date\n" {
		t.Fatalf("rebase: %q", out5)
	}
}

func TestTryCompactGitLogShow_empty(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactGitLog([]string{"git", "log", "-1"}, []byte("\n  "))
	if !ok || string(out) != "[git log] empty\n" {
		t.Fatalf("log: ok=%v %q", ok, out)
	}
	if _, ok := TryCompactGitLog([]string{"git", "status"}, []byte("")); ok {
		t.Fatal("not log")
	}
	out2, ok := TryCompactGitShow([]string{"git", "show", "HEAD"}, []byte(""))
	if !ok || string(out2) != "[git show] empty\n" {
		t.Fatalf("show: ok=%v %q", ok, out2)
	}
	if _, ok := TryCompactGitShow([]string{"git", "diff"}, []byte("")); ok {
		t.Fatal("not show")
	}
}

func TestTryCompactGitDiff_empty(t *testing.T) {
	t.Parallel()
	argv := []string{"git", "diff"}
	out, ok := TryCompactGitDiff(argv, []byte("  \n\r\n"))
	if !ok || string(out) != "[git diff] empty\n" {
		t.Fatalf("ok=%v out=%q", ok, out)
	}
	if _, ok := TryCompactGitDiff([]string{"git", "status"}, []byte("")); ok {
		t.Fatal("not diff")
	}
	out2, ok := TryCompactGitDiff(argv, []byte("diff --git a/x b/x\n"))
	if ok || string(out2) == "[git diff] empty\n" {
		t.Fatalf("non-empty should pass through: ok=%v %q", ok, out2)
	}
}

func TestTryCompactGitStatus_emptyAndEmptyLine(t *testing.T) {
	t.Parallel()
	argv := []string{"git", "status"}
	// Empty stdout → clean at the TrimSpace check
	out, ok := TryCompactGitStatus(argv, []byte(""))
	if !ok || string(out) != "[git status] clean\n" {
		t.Fatalf("empty stdout: ok=%v %q", ok, out)
	}
	// Contains an empty line within porcelain output (empty line continue branch)
	out2, ok := TryCompactGitStatus(argv, []byte("M  file.go\n\n?? new.txt\n"))
	if !ok {
		t.Fatalf("with empty line: ok=%v %q", ok, out2)
	}
}

func TestTryCompactGitLog_nonEmpty(t *testing.T) {
	t.Parallel()
	// Non-empty stdout → pass through (final return false)
	if _, ok := TryCompactGitLog([]string{"git", "log"}, []byte("commit abc\n")); ok {
		t.Fatal("non-empty git log should pass through")
	}
}

func TestTryCompactGitShow_nonEmpty(t *testing.T) {
	t.Parallel()
	// Non-empty stdout → pass through
	if _, ok := TryCompactGitShow([]string{"git", "show", "HEAD"}, []byte("commit info\n")); ok {
		t.Fatal("non-empty git show should pass through")
	}
}

func TestTryCompactGitF05_noMatch(t *testing.T) {
	t.Parallel()
	// Non-matching push output — final return false
	if _, ok := TryCompactGitF05([]string{"git", "push"}, []byte("remote: some output\n")); ok {
		t.Fatal("non-matching push output should pass through")
	}
	// fetch up-to-date
	out, ok := TryCompactGitF05([]string{"git", "fetch"}, []byte("Already up to date.\n"))
	if !ok || string(out) != "[git fetch] up to date\n" {
		t.Fatalf("fetch up to date: ok=%v %q", ok, out)
	}
}

func TestTryCompactGitF05_pushSuccess(t *testing.T) {
	t.Parallel()
	// Successful push with ref update
	pushOutput := `To https://github.com/user/repo.git
   abc1234..def5678  main -> main
`
	out, ok := TryCompactGitF05([]string{"git", "push"}, []byte(pushOutput))
	if !ok {
		t.Fatalf("push success: expected compact, got false")
	}
	s := string(out)
	if !strings.Contains(s, "[git push]") {
		t.Errorf("push: want [git push] in %q", s)
	}
	if !strings.Contains(s, "1 ref(s) updated") {
		t.Errorf("push: want '1 ref(s) updated' in %q", s)
	}

	// New branch push
	newBranchOutput := `To https://github.com/user/repo.git
 * [new branch]      feature/x -> origin/feature/x
`
	out2, ok2 := TryCompactGitF05([]string{"git", "push"}, []byte(newBranchOutput))
	if !ok2 {
		t.Fatalf("push new branch: expected compact, got false")
	}
	if !strings.Contains(string(out2), "[git push]") {
		t.Errorf("push new branch: want [git push] in %q", out2)
	}

	// Push with no recognizable refs - passthrough
	_, ok3 := TryCompactGitF05([]string{"git", "push"}, []byte("remote: some other output\n"))
	if ok3 {
		t.Error("push no refs: expected passthrough")
	}
}

func TestTryCompactGitF05_fetchSuccess(t *testing.T) {
	t.Parallel()
	fetchOutput := `From https://github.com/user/repo.git
   abc1234..def5678  main     -> origin/main
 * [new branch]      feature  -> origin/feature
`
	out, ok := TryCompactGitF05([]string{"git", "fetch"}, []byte(fetchOutput))
	if !ok {
		t.Fatalf("fetch success: expected compact, got false")
	}
	s := string(out)
	if !strings.Contains(s, "[git fetch]") {
		t.Errorf("fetch: want [git fetch] in %q", s)
	}
	if !strings.Contains(s, "updated") {
		t.Errorf("fetch: want 'updated' in %q", s)
	}
	if !strings.Contains(s, "new") {
		t.Errorf("fetch: want 'new' in %q", s)
	}

	// Fetch with no updates - passthrough (no updates/new refs)
	_, ok2 := TryCompactGitF05([]string{"git", "fetch"}, []byte("From github.com/user/repo\n"))
	if ok2 {
		t.Error("fetch no updates: expected passthrough")
	}
}

func TestTryCompactGitF05_mergeSuccess(t *testing.T) {
	t.Parallel()
	// Fast-forward merge
	mergeFF := `Updating abc1234..def5678
Fast-forward
 file.go | 5 +++++
 1 file changed, 5 insertions(+)
`
	out, ok := TryCompactGitF05([]string{"git", "merge"}, []byte(mergeFF))
	if !ok {
		t.Fatalf("merge ff: expected compact, got false")
	}
	if !strings.Contains(string(out), "fast-forward") {
		t.Errorf("merge ff: want 'fast-forward' in %q", out)
	}

	// Fast-forward without stat line
	mergeFFNoStat := `Updating abc1234..def5678
Fast-forward
`
	out2, ok2 := TryCompactGitF05([]string{"git", "merge"}, []byte(mergeFFNoStat))
	if !ok2 {
		t.Fatalf("merge ff no stat: expected compact, got false")
	}
	if !strings.Contains(string(out2), "fast-forward") {
		t.Errorf("merge ff no stat: want 'fast-forward' in %q", out2)
	}

	// Non-ff merge - passthrough
	_, ok3 := TryCompactGitF05([]string{"git", "merge"}, []byte("Merge made by the 'recursive' strategy.\n"))
	if ok3 {
		t.Error("merge non-ff: expected passthrough")
	}
}

func TestTryCompactGitF05_rebaseSuccess(t *testing.T) {
	t.Parallel()
	// Successful rebase
	out, ok := TryCompactGitF05([]string{"git", "rebase"}, []byte("Successfully rebased and updated refs/heads/main.\n"))
	if !ok {
		t.Fatalf("rebase success: expected compact")
	}
	if !strings.Contains(string(out), "[git rebase] ok") {
		t.Errorf("rebase: want '[git rebase] ok' in %q", out)
	}
}

// TestCompactGitPushOutput_notShorter covers the len(result) >= len(s) guard.
func TestCompactGitPushOutput_notShorter(t *testing.T) {
	t.Parallel()
	// Single very short ref line — result won't be shorter
	short := "abc123..def456  x -> x\n"
	got := compactGitPushOutput(short)
	// May or may not be shorter depending on overhead; just ensure no panic
	_ = got
}

// TestCompactGitFetchOutput_noUpdates covers the updates==0 && newRefs==0 return "".
func TestCompactGitFetchOutput_noUpdates(t *testing.T) {
	t.Parallel()
	got := compactGitFetchOutput("From github.com/x\n", "fetch")
	if got != "" {
		t.Errorf("no updates: want empty, got %q", got)
	}
}

// TestExtractMergeStatLine_noMatch covers the no-match return "".
func TestExtractMergeStatLine_noMatch(t *testing.T) {
	t.Parallel()
	got := extractMergeStatLine("Fast-forward\nUpdating abc..def\n")
	if got != "" {
		t.Errorf("no stat line: want empty, got %q", got)
	}
}

func TestTryCompactGitLog_fullCompact(t *testing.T) {
	t.Parallel()
	input := `commit a1b2c3d4e5f6a7b8 (HEAD -> main)
Author: Alice <alice@example.com>
Date:   Mon Apr 7 10:30:00 2025 +0000

    Add feature X

 internal/foo.go | 5 +++++
 internal/bar.go | 3 ---
 2 files changed, 5 insertions(+), 3 deletions(-)

commit 9e8d7c6b5a4f3e2d
Author: Bob <bob@example.com>
Date:   Sun Apr 6 08:00:00 2025 +0000

    Fix critical bug

 cmd/main.go | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)
`
	out, ok := TryCompactGitLog([]string{"git", "log"}, []byte(input))
	if !ok {
		t.Fatal("expected compact")
	}
	s := string(out)
	if !strings.HasPrefix(s, "[git log] 2 commit(s)") {
		t.Fatalf("want '[git log] 2 commit(s)' prefix, got %q", s)
	}
	if !strings.Contains(s, "a1b2c3d") {
		t.Errorf("want first hash in output, got %q", s)
	}
	if !strings.Contains(s, "Add feature X") {
		t.Errorf("want first subject in output, got %q", s)
	}
	if !strings.Contains(s, "Fix critical bug") {
		t.Errorf("want second subject in output, got %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("expected shorter output: got %d vs input %d", len(s), len(input))
	}
}

func TestTryCompactGitDiff_fullCompact(t *testing.T) {
	t.Parallel()
	input := `diff --git a/internal/proxy/handler.go b/internal/proxy/handler.go
index abc123..def456 100644
--- a/internal/proxy/handler.go
+++ b/internal/proxy/handler.go
@@ -10,7 +10,8 @@ package proxy
 import "fmt"

-func oldFunc() {
+func newFunc() {
+	// new implementation
 }

 context here
diff --git a/cmd/main.go b/cmd/main.go
index 111222..333444 100644
--- a/cmd/main.go
+++ b/cmd/main.go
@@ -1,5 +1,4 @@ package main
 import "os"
-import "fmt"

 func main() {}
`
	out, ok := TryCompactGitDiff([]string{"git", "diff"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact: %q", out)
	}
	s := string(out)
	if !strings.HasPrefix(s, "[git diff] 2 file(s)") {
		t.Fatalf("want diff header, got %q", s)
	}
	if !strings.Contains(s, "handler.go") {
		t.Errorf("want handler.go in output, got %q", s)
	}
	if !strings.Contains(s, "cmd/main.go") || !strings.Contains(s, "main.go") {
		t.Errorf("want main.go in output, got %q", s)
	}
	// Context lines should NOT appear.
	if strings.Contains(s, "context here") {
		t.Errorf("context lines should be stripped, got %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("expected shorter output: got %d vs input %d", len(s), len(input))
	}
}

func TestTryCompactGitShow_fullCompact(t *testing.T) {
	t.Parallel()
	input := `commit a1b2c3d4e5f6a7b8
Author: Alice <alice@example.com>
Date:   Mon Apr 7 10:30:00 2025 +0000

    Refactor handler

 internal/proxy/handler.go | 10 +++++++---
 1 file changed, 7 insertions(+), 3 deletions(-)

diff --git a/internal/proxy/handler.go b/internal/proxy/handler.go
index 111..222 100644
--- a/internal/proxy/handler.go
+++ b/internal/proxy/handler.go
@@ -5,3 +5,10 @@ package proxy
 existing code
-old line
+new line
`
	out, ok := TryCompactGitShow([]string{"git", "show", "HEAD"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact: %q", out)
	}
	s := string(out)
	if !strings.HasPrefix(s, "[git show] a1b2c3d") {
		t.Fatalf("want '[git show] a1b2c3d' prefix, got %q", s)
	}
	if !strings.Contains(s, "Refactor handler") {
		t.Errorf("want subject in output, got %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("expected shorter output: got %d vs input %d", len(s), len(input))
	}
}

func TestTryCompactGitStatus_ignoredSkipped(t *testing.T) {
	t.Parallel()
	argv := []string{"git", "status"}
	// Ignored-only paths are omitted; with only ## + !! we treat as clean summary.
	in := "## x\n!! ignored.o\n"
	out, ok := TryCompactGitStatus(argv, []byte(in))
	if !ok {
		t.Fatal("expected compact")
	}
	if string(out) != "[git status] clean\n" {
		t.Fatalf("%q", out)
	}
}

// TestExtractDiffPath covers both branches of extractDiffPath.
func TestExtractDiffPath(t *testing.T) {
	t.Parallel()
	// Valid diff header line
	got := extractDiffPath("diff --git a/internal/foo.go b/internal/foo.go")
	if got != "internal/foo.go" {
		t.Errorf("extractDiffPath: want internal/foo.go, got %q", got)
	}
	// Too few parts → empty
	got2 := extractDiffPath("short line")
	if got2 != "" {
		t.Errorf("short line: want empty, got %q", got2)
	}
	// Empty string → empty
	got3 := extractDiffPath("")
	if got3 != "" {
		t.Errorf("empty: want empty, got %q", got3)
	}
}

// TestCompactGitLog_noStat covers the e.stat=="" branch (line 103-105) in compactGitLog:
// a commit with no files-changed summary line produces a plain "hash subject" line.
func TestCompactGitLog_noStat(t *testing.T) {
	t.Parallel()
	input := `commit a1b2c3d4e5f6a7b8
Author: Alice <alice@example.com>
Date:   Mon Apr 7 10:30:00 2025 +0000

    Fix small typo

commit b2c3d4e5f6a7b8c9
Author: Bob <bob@example.com>
Date:   Sun Apr 6 08:00:00 2025 +0000

    Update README
`
	got := compactGitLog(input)
	if got == "" {
		t.Fatal("expected non-empty compact output")
	}
	if !strings.Contains(got, "Fix small typo") {
		t.Errorf("want first subject, got: %q", got)
	}
	if !strings.Contains(got, "Update README") {
		t.Errorf("want second subject, got: %q", got)
	}
	// No stat should appear (no "[N file(s)...]" brackets)
	if strings.Contains(got, "file(s)") {
		t.Errorf("no-stat commit should not have file count, got: %q", got)
	}
}

// TestParseGitStatSummary_allZero covers the files==0&&ins==0&&del==0 guard (line 131-133).
func TestParseGitStatSummary_allZero(t *testing.T) {
	t.Parallel()
	got := parseGitStatSummary("nothing matching here")
	if got != "" {
		t.Errorf("no-number line: want empty, got %q", got)
	}
}

// TestCompactGitDiff_noHasCur covers the !hasCur continue (line 183-184):
// content before the first "diff --git" line is silently skipped.
func TestCompactGitDiff_noHasCur(t *testing.T) {
	t.Parallel()
	input := "warning: LF will be replaced by CRLF\ndiff --git a/x b/x\n@@ -1,2 +1,3 @@\n+added line\n"
	got := compactGitDiff(input)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(got, "added") {
		t.Errorf("want added line mention, got: %q", got)
	}
}

// TestCompactGitDiff_notInHunk covers the !inHunk continue (line 205-206):
// mode-change lines between the file header and the @@ marker are silently skipped.
func TestCompactGitDiff_notInHunk(t *testing.T) {
	t.Parallel()
	input := "diff --git a/script.sh b/script.sh\nold mode 100644\nnew mode 100755\n@@ -1,2 +1,3 @@\n+echo hi\n"
	got := compactGitDiff(input)
	if got == "" {
		t.Fatal("expected non-empty output")
	}
}

// TestCompactGitDiff_noFiles covers the len(files)==0 return "" (line 222-224):
// content with no "diff --git" headers yields no files and an empty result.
func TestCompactGitDiff_noFiles(t *testing.T) {
	t.Parallel()
	got := compactGitDiff("plain text\nno diff headers here\n")
	if got != "" {
		t.Errorf("no diff headers: want empty, got %q", got)
	}
}

// TestTryCompactGitStatus_renameAndConflict covers the renamed/conflict branches.
func TestTryCompactGitStatus_renameAndConflict(t *testing.T) {
	t.Parallel()
	argv := []string{"git", "status"}

	// Renamed file (R in column 1 = staged rename)
	out, ok := TryCompactGitStatus(argv, []byte("R  old.go -> new.go\n M other.go\n"))
	if !ok {
		t.Fatalf("rename: expected compact, got false")
	}
	s := string(out)
	if !strings.Contains(s, "renamed:1") {
		t.Errorf("rename: want renamed:1 in %q", s)
	}
	if !strings.Contains(s, "staged:1") {
		t.Errorf("rename: want staged:1 in %q", s)
	}

	// Copy in column 1
	out2, ok2 := TryCompactGitStatus(argv, []byte("C  orig.go -> copy.go\n"))
	if !ok2 {
		t.Fatalf("copy: expected compact, got false")
	}
	if !strings.Contains(string(out2), "renamed:1") {
		t.Errorf("copy: want renamed:1 in %q", out2)
	}

	// Conflict (UU = both modified)
	out3, ok3 := TryCompactGitStatus(argv, []byte("UU conflict.go\n M other.go\n"))
	if !ok3 {
		t.Fatalf("conflict UU: expected compact, got false")
	}
	s3 := string(out3)
	if !strings.Contains(s3, "conflicts:1") {
		t.Errorf("conflict UU: want conflicts:1 in %q", s3)
	}

	// AA conflict
	out4, ok4 := TryCompactGitStatus(argv, []byte("AA added.go\n"))
	if !ok4 {
		t.Fatalf("conflict AA: expected compact, got false")
	}
	if !strings.Contains(string(out4), "conflicts:1") {
		t.Errorf("conflict AA: want conflicts:1 in %q", out4)
	}

	// AU conflict (line[1]=='U')
	out5, ok5 := TryCompactGitStatus(argv, []byte("AU file.go\n"))
	if !ok5 {
		t.Fatalf("conflict AU: expected compact, got false")
	}
	if !strings.Contains(string(out5), "conflicts:1") {
		t.Errorf("conflict AU: want conflicts:1 in %q", out5)
	}

	// DD conflict (both deleted)
	out6, ok6 := TryCompactGitStatus(argv, []byte("DD deleted.go\n"))
	if !ok6 {
		t.Fatalf("conflict DD: expected compact, got false")
	}
	if !strings.Contains(string(out6), "conflicts:1") {
		t.Errorf("conflict DD: want conflicts:1 in %q", out6)
	}

	// No conflicts → no "conflicts:" in output
	out7, ok7 := TryCompactGitStatus(argv, []byte("M  clean.go\n"))
	if !ok7 {
		t.Fatalf("no conflict: expected compact")
	}
	if strings.Contains(string(out7), "conflicts:") {
		t.Errorf("no conflict: unexpected conflicts: in %q", out7)
	}
	// No renames → no "renamed:" in output
	if strings.Contains(string(out7), "renamed:") {
		t.Errorf("no rename: unexpected renamed: in %q", out7)
	}
}

// TestTryCompactGitStatus_ignoredAndMalformed covers three uncovered TryCompactGitStatus guards:
// - "!!" prefix lines (line 381-382): ignored files are silently skipped.
// - len(line) < 3 (line 384-386): malformed short line → return stdout, false.
// - line[2] not space/tab (line 387-389): invalid separator → return stdout, false.
func TestTryCompactGitStatus_ignoredAndMalformed(t *testing.T) {
	t.Parallel()
	argv := []string{"git", "status"}

	// "!!" prefix → ignored file, still produces valid compact output.
	out, ok := TryCompactGitStatus(argv, []byte("M  staged.go\n!! .env\n"))
	if !ok {
		t.Fatalf("!! line: expected compact output, got false")
	}
	if !strings.Contains(string(out), "staged:1") {
		t.Errorf("!! line: want staged:1, got %q", out)
	}

	// Line < 3 chars → malformed, return stdout, false.
	_, ok2 := TryCompactGitStatus(argv, []byte("AB\n"))
	if ok2 {
		t.Error("len<3 line: want false, got true")
	}

	// line[2] not space/tab → malformed separator, return stdout, false.
	_, ok3 := TryCompactGitStatus(argv, []byte("ABx file.go\n"))
	if ok3 {
		t.Error("line[2] not space/tab: want false, got true")
	}
}

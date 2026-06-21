package crosstool

import (
	"strings"
	"testing"
)

func TestExtractGitStatusPaths(t *testing.T) {
	t.Parallel()
	got := ExtractGitStatusPaths([]byte("## main\n M ./a.go\nR  old.go -> b.go\n?? c/d.txt\n!! ignored.o\n"))
	want := []string{"a.go", "b.go", "c/d.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("paths=%v want %v", got, want)
	}
}

func TestExtractGitStatusPaths_RejectsNonStatusShape(t *testing.T) {
	t.Parallel()
	if got := ExtractGitStatusPaths([]byte("plain output\n M ok.go\n")); got != nil {
		t.Fatalf("expected reject, got %v", got)
	}
	if got := ExtractGitStatusPaths([]byte("   \n")); got != nil {
		t.Fatalf("expected empty reject, got %v", got)
	}
	for _, input := range []string{"M\n", "ZZ bad.go\n", "M\tbad.go\n", "M    \n"} {
		if got := ExtractGitStatusPaths([]byte(input)); got != nil {
			t.Fatalf("expected reject for %q, got %v", input, got)
		}
	}
}

func TestExtractGitNameOnlyPaths(t *testing.T) {
	t.Parallel()
	got := ExtractGitNameOnlyPaths([]byte("./b.go\na.go\na.go\n"))
	want := []string{"a.go", "b.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("paths=%v want %v", got, want)
	}
	if got := ExtractGitNameOnlyPaths([]byte("not a path with spaces\n")); got != nil {
		t.Fatalf("expected reject, got %v", got)
	}
}

func TestGitArgvDetectionAndMarker(t *testing.T) {
	t.Parallel()
	if !IsGitStatusArgv([]string{"/usr/bin/git", "status", "--short"}) {
		t.Fatal("expected git status detection")
	}
	if IsGitStatusArgv([]string{"status"}) {
		t.Fatal("non-git argv must not be status")
	}
	if IsGitStatusArgv([]string{"git", "diff", "--name-only"}) {
		t.Fatal("diff must not be status")
	}
	if !IsGitDiffNameOnlyArgv([]string{"git", "-C", "/repo", "diff", "--name-only"}) {
		t.Fatal("expected git diff --name-only detection")
	}
	if IsGitDiffNameOnlyArgv([]string{"diff", "--name-only"}) {
		t.Fatal("non-git argv must not be git diff")
	}
	if IsGitDiffNameOnlyArgv([]string{"git", "diff", "--name-status"}) {
		t.Fatal("name-status must not be name-only")
	}
	if IsGitDiffNameOnlyArgv([]string{"git", "status", "--name-only"}) {
		t.Fatal("name-only without diff must not match")
	}
	marker := Marker(2, "git `status` with a very long suffix "+strings.Repeat("x", 160))
	if !strings.Contains(marker, "2 git paths") || strings.Contains(marker, "`status`") || !strings.Contains(marker, "...`") {
		t.Fatalf("bad marker: %q", marker)
	}
	if !strings.Contains(Marker(1, ""), "earlier git command") {
		t.Fatal("empty marker source should use fallback")
	}
}

func TestSortedUniqueDropsEmpty(t *testing.T) {
	t.Parallel()
	got := sortedUnique([]string{"b.go", "", "a.go", "a.go"})
	want := []string{"a.go", "b.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sortedUnique=%v want %v", got, want)
	}
	if got := sortedUnique(nil); got != nil {
		t.Fatalf("nil sortedUnique=%v", got)
	}
}

func TestIntString(t *testing.T) {
	t.Parallel()
	if intString(0) != "0" || intString(42) != "42" {
		t.Fatal("bad intString")
	}
}

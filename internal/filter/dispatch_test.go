package filter

import "testing"

func TestClassifyCommand(t *testing.T) {
	t.Parallel()
	if g := ClassifyCommand([]string{"git", "status"}); g != "git" {
		t.Fatal(g)
	}
	if g := ClassifyCommand([]string{"/usr/bin/docker", "ps"}); g != "docker" {
		t.Fatal(g)
	}
	if g := ClassifyCommand([]string{"foo"}); g != "generic" {
		t.Fatal(g)
	}
}

func TestClassifyCommand_empty(t *testing.T) {
	t.Parallel()
	if g := ClassifyCommand([]string{}); g != "empty" {
		t.Fatalf("empty argv: got %q", g)
	}
}

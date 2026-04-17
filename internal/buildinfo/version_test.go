package buildinfo

import "testing"

func TestVersion(t *testing.T) {
	t.Parallel()

	if Version == "" {
		t.Fatal("Version must not be empty")
	}
	if Version != "2.0.2" {
		t.Fatalf("Version = %q, want %q", Version, "2.0.2")
	}
}

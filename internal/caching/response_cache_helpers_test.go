package caching

import "testing"

func TestCanonicalizeJSON_InvalidInputFallsBackToTrimmedText(t *testing.T) {
	t.Parallel()

	got := string(canonicalizeJSON([]byte("  not-json  ")))
	if got != "not-json" {
		t.Fatalf("canonicalizeJSON fallback: got %q", got)
	}
}

func TestExtractDependencyHelpers(t *testing.T) {
	t.Parallel()

	paths := extractDependencyPathsFromString("edit src/main.go then src/main.go and ./pkg/handler_test.go")
	if len(paths) != 2 {
		t.Fatalf("expected deduped dependency paths, got %#v", paths)
	}

	if normalizeDependencyPath("   ") != "" {
		t.Fatal("empty path should normalize to empty string")
	}
	if normalizeDependencyPath("/") != "" {
		t.Fatal("root path should normalize to empty string")
	}

	entry := &CacheEntry{DependencyPaths: []string{"src/main.go", "/Users/me/repo/internal/app.go"}}
	if !cacheEntryDependsOnPath(entry, "/Users/me/repo/src/main.go") {
		t.Fatal("absolute changed path should match stored relative dependency")
	}
	if !cacheEntryDependsOnPath(entry, "internal/app.go") {
		t.Fatal("relative changed path should match stored absolute dependency suffix")
	}
	if cacheEntryDependsOnPath(entry, "docs/readme.md") {
		t.Fatal("unrelated path should not match dependencies")
	}
}

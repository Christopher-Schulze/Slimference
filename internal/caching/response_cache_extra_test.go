package caching

import (
	"errors"
	"regexp"
	"testing"
)

func TestCanonicalizeJSON_ValidJSONCompacts(t *testing.T) {
	got := string(canonicalizeJSON([]byte("{\n  \"b\": 2,\n  \"a\": 1\n}")))
	if got != "{\"a\":1,\"b\":2}" && got != "{\"b\":2,\"a\":1}" {
		t.Fatalf("canonicalizeJSON compacted unexpected payload: %q", got)
	}
}

func TestCanonicalizeJSON_MarshalErrorFallsBackToTrimmedText(t *testing.T) {
	orig := jsonMarshalFn
	jsonMarshalFn = func(any) ([]byte, error) {
		return nil, errors.New("marshal boom")
	}
	defer func() {
		jsonMarshalFn = orig
	}()

	got := string(canonicalizeJSON([]byte(" {\"a\":1} ")))
	if got != "{\"a\":1}" {
		t.Fatalf("expected trimmed fallback, got %q", got)
	}
}

func TestExtractDependencyPaths_FallbackAndDedupe(t *testing.T) {
	paths := ExtractDependencyPaths([]byte("read src/main.go and ./src/main.go plus /tmp/app/config.toml"))
	if len(paths) != 2 {
		t.Fatalf("expected 2 deduped paths, got %v", paths)
	}
	if paths[0] != "/tmp/app/config.toml" || paths[1] != "src/main.go" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestExtractDependencyPathsFromString_NoMatches(t *testing.T) {
	if got := extractDependencyPathsFromString("no file paths here"); got != nil {
		t.Fatalf("expected nil for no matches, got %v", got)
	}
}

func TestExtractDependencyPathsFromString_SkipsEmptyNormalizedPath(t *testing.T) {
	orig := dependencyPathRegex
	dependencyPathRegex = regexp.MustCompile(`/`)
	defer func() {
		dependencyPathRegex = orig
	}()

	if got := extractDependencyPathsFromString("/"); len(got) != 0 {
		t.Fatalf("expected empty slice after empty normalized path skip, got %v", got)
	}
}

func TestCacheEntryDependsOnPath_MatchModes(t *testing.T) {
	entry := &CacheEntry{
		DependencyPaths: []string{
			"",
			"src/main.go",
			"/Users/christopher/CODE/Slimference/src/lib/util.go",
		},
	}

	if !cacheEntryDependsOnPath(entry, "src/main.go") {
		t.Fatal("expected exact relative match")
	}
	if !cacheEntryDependsOnPath(entry, "/Users/christopher/CODE/Slimference/src/main.go") {
		t.Fatal("expected absolute changed path to match relative dependency")
	}
	if !cacheEntryDependsOnPath(entry, "src/lib/util.go") {
		t.Fatal("expected relative changed path to match absolute dependency")
	}
	if cacheEntryDependsOnPath(entry, "src/other.go") {
		t.Fatal("unexpected dependency match")
	}
}

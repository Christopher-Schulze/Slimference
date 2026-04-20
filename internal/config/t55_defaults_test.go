package config

import "testing"

// TestT55_StructurePreviewDefaultOn pins the T55 contract: the built-in
// config defaults enable the structure-aware tool-result preview so new
// installations get the savings without having to opt in. Flipping this
// back to false is a deliberate breaking change and must be done together
// with a changelog entry.
func TestT55_StructurePreviewDefaultOn(t *testing.T) {
	cfg := defaultsRaw()
	if !cfg.Compression.Tuning.StructurePreview {
		t.Fatal("T55 contract broken: compression.tuning.structure_preview default must be true")
	}
}

// TestT55_DefaultTOMLMirrorsStructOn ensures the DefaultTOML template string
// (used by `slimference config init`) stays in sync with the Go struct
// default. A mismatch means new users writing a config file would silently
// opt themselves out of the T55 default.
func TestT55_DefaultTOMLMirrorsStructOn(t *testing.T) {
	tmpl := DefaultTOML()
	// A crude but effective check: the line must set structure_preview to
	// true, not false.
	if !contains(tmpl, "structure_preview = true") {
		t.Fatalf("DefaultTOML missing 'structure_preview = true'")
	}
	if contains(tmpl, "structure_preview = false") {
		t.Fatalf("DefaultTOML still opts out of T55 default")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

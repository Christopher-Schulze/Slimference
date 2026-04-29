package config

import "testing"

// TestT76_StructurePreviewDefaultOn pins the T76 contract: with the
// content-archive recorder ensuring reversibility, the structure-aware
// tool-result preview ships default-on. Switching it back to false would
// regress the savings T76 unlocks.
func TestT76_StructurePreviewDefaultOn(t *testing.T) {
	cfg := defaultsRaw()
	if !cfg.Compression.Tuning.StructurePreview {
		t.Fatal("T76 contract broken: compression.tuning.structure_preview default must be true")
	}
}

// TestT76_DefaultTOMLMirrorsStructOn ensures the DefaultTOML template
// (used by `slimference config init`) stays in sync with the Go struct
// default. New configs must opt-in to T76's reversible default-on.
func TestT76_DefaultTOMLMirrorsStructOn(t *testing.T) {
	tmpl := DefaultTOML()
	if !contains(tmpl, "structure_preview = true") {
		t.Fatalf("DefaultTOML missing 'structure_preview = true'")
	}
	if contains(tmpl, "structure_preview = false") {
		t.Fatalf("DefaultTOML still ships preview off")
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

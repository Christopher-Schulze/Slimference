package config

import "testing"

// TestT74_StructurePreviewDefaultOff pins the T74 safety contract: the
// structure-aware tool-result preview remains implemented, but is opt-in until
// each preview has local archive recovery. Default-on lossy preview is not
// production-safe.
func TestT74_StructurePreviewDefaultOff(t *testing.T) {
	cfg := defaultsRaw()
	if cfg.Compression.Tuning.StructurePreview {
		t.Fatal("T74 contract broken: compression.tuning.structure_preview default must be false")
	}
}

// TestT74_DefaultTOMLMirrorsStructOff ensures the DefaultTOML template string
// (used by `slimference config init`) stays in sync with the Go struct
// default. A mismatch means new users writing a config file could silently
// opt themselves into a lossy preview.
func TestT74_DefaultTOMLMirrorsStructOff(t *testing.T) {
	tmpl := DefaultTOML()
	// A crude but effective check: the line must set structure_preview to
	// false, not true.
	if !contains(tmpl, "structure_preview = false") {
		t.Fatalf("DefaultTOML missing 'structure_preview = false'")
	}
	if contains(tmpl, "structure_preview = true") {
		t.Fatalf("DefaultTOML still opts into lossy structure preview")
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

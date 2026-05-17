package extract

import "testing"

func TestExportedWrappersDelegateToParsers(t *testing.T) {
	sections := ParseSections("# Heading\n\nBody sentence.")
	if len(sections) == 0 {
		t.Fatal("ParseSections returned no sections")
	}
	if sections[0].Content == "" {
		t.Fatalf("ParseSections returned empty first section: %#v", sections)
	}

	sentences := SplitSentences("Dr. Smith shipped v1.2. It passed.")
	if len(sentences) != 2 {
		t.Fatalf("SplitSentences len=%d, want 2: %#v", len(sentences), sentences)
	}
}

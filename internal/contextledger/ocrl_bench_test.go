package contextledger

import "testing"

func BenchmarkBuildOCRLReplacement(b *testing.B) {
	capsules := make([]Capsule, 0, 512)
	archiveIDs := make([]string, 0, 512)
	for i := 0; i < 512; i++ {
		id := "arch-" + intFact(i)
		archiveIDs = append(archiveIDs, id)
		capsule := testCapsule(CapsuleFile, "bench-session", "old-"+intFact(i), id)
		capsule.Facts["path"] = "/repo/file" + intFact(i) + ".go"
		capsules = append(capsules, capsule)
	}
	policy := OCRLPolicy{
		Mode:           OCRLModeMax,
		Route:          OCRLRouteFullHistoryHTTP,
		Selection:      SelectionPolicy{SessionID: "bench-session", MaxCapsules: 512},
		ArchiveLoader:  fixedArchiveLoader(archiveIDs...),
		CountTokens:    func(text string) int { return len(text) / 4 },
		OriginalTokens: 200000,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := BuildOCRLReplacement(capsules, policy)
		if !result.Applied {
			b.Fatalf("OCRL did not apply: %+v", result)
		}
	}
}

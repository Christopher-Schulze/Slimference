package contextledger

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

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

func BenchmarkDeriveOCRLMessageTargets(b *testing.B) {
	messages, capsules, load := ocrlDerivationBenchFixture(512)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		derivation := DeriveOCRLMessageTargets(messages, capsules, load)
		if derivation.Matched != len(capsules) || len(derivation.Targets) != len(capsules) {
			b.Fatalf("bad derivation: %+v", derivation)
		}
	}
}

func BenchmarkApplyOCRLToMessagesByArchiveMatch(b *testing.B) {
	messages, capsules, load := ocrlDerivationBenchFixture(512)
	policy := OCRLPolicy{
		Mode:          OCRLModeMax,
		Route:         OCRLRouteFullHistoryHTTP,
		Selection:     SelectionPolicy{SessionID: "bench-session", MaxCapsules: 512},
		ArchiveLoader: load,
		CountTokens:   func(text string) int { return len(text) / 4 },
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, derivation := ApplyOCRLToMessagesByArchiveMatch(messages, capsules, policy)
		if !result.OCRL.Applied || derivation.Matched != len(capsules) {
			b.Fatalf("bad OCRL apply result=%+v derivation=%+v", result.OCRL, derivation)
		}
	}
}

func ocrlDerivationBenchFixture(n int) ([]types.Message, []Capsule, ArchiveLoader) {
	messages := make([]types.Message, n)
	capsules := make([]Capsule, 0, n)
	archiveBodies := make(map[string][]byte, n)
	for i := 0; i < n; i++ {
		id := "arch-" + intFact(i)
		body := strings.Repeat("bench old context "+intFact(i)+" ", 80)
		messages[i] = types.Message{
			Index: i,
			Role:  "tool",
			Content: []types.ContentBlock{{
				Type: "tool_result",
				Text: body,
			}},
		}
		archiveBodies[id] = []byte(body)
		capsule, err := BuildFileCapsule(FileObservation{
			SessionID:    "bench-session",
			TurnID:       "old-" + intFact(i),
			Path:         "/repo/file" + intFact(i) + ".go",
			RepoRoot:     "/repo",
			Range:        "1:80",
			Content:      []byte(body),
			ArchiveID:    id,
			FullPassTurn: "full-" + intFact(i),
		})
		if err != nil {
			panic(err)
		}
		capsules = append(capsules, capsule)
	}
	return messages, capsules, func(id string) ([]byte, error) {
		return archiveBodies[id], nil
	}
}

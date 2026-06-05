package contextledger

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildOCRLReplacementAppliesOnlyOnFullHistoryPositiveSavings(t *testing.T) {
	t.Parallel()
	capsules := []Capsule{
		testCapsule(CapsuleFile, "s", "old-file", "file-arch"),
		testCapsule(CapsuleCommand, "s", "old-command", "cmd-arch"),
	}
	result := BuildOCRLReplacement(capsules, OCRLPolicy{
		Mode:           OCRLModeAuto,
		Route:          OCRLRouteFullHistoryHTTP,
		Selection:      SelectionPolicy{SessionID: "s"},
		ArchiveLoader:  fixedArchiveLoader("file-arch", "cmd-arch"),
		CountTokens:    fixedTokenCounter(20),
		OriginalTokens: 250,
	})
	if !result.Applied || result.ShadowOnly || result.Reason != OCRLReasonApplied {
		t.Fatalf("result=%+v want applied", result)
	}
	if result.NetSavedTokens != 230 || result.ArchiveExpansions != 2 {
		t.Fatalf("bad accounting: %+v", result)
	}
	if !strings.Contains(result.Text, "[ocrl:v1 selected=2 archive_recoverable=true]") {
		t.Fatalf("missing OCRL header: %q", result.Text)
	}
}

func TestBuildOCRLReplacementKeepsCodexWSSShadowOnly(t *testing.T) {
	t.Parallel()
	result := BuildOCRLReplacement([]Capsule{
		testCapsule(CapsuleSearch, "s", "old-search", "search-arch"),
	}, OCRLPolicy{
		Mode:           OCRLModeMax,
		Route:          OCRLRouteCodexWSS,
		Selection:      SelectionPolicy{SessionID: "s"},
		ArchiveLoader:  fixedArchiveLoader("search-arch"),
		CountTokens:    fixedTokenCounter(10),
		OriginalTokens: 100,
	})
	if result.Applied || !result.ShadowOnly || result.Reason != OCRLReasonRouteNotEligible {
		t.Fatalf("result=%+v want route shadow only", result)
	}
	if result.NetSavedTokens != 90 {
		t.Fatalf("shadow accounting should still be computed: %+v", result)
	}
}

func TestBuildOCRLReplacementCanCountOriginalTokensFromArchives(t *testing.T) {
	t.Parallel()
	result := BuildOCRLReplacement([]Capsule{
		testCapsule(CapsuleFile, "s", "old-file", "file-arch"),
	}, OCRLPolicy{
		Mode:                     OCRLModeMax,
		Route:                    OCRLRouteCodexWSS,
		Selection:                SelectionPolicy{SessionID: "s"},
		ArchiveLoader:            func(string) ([]byte, error) { return []byte("one two three four five"), nil },
		CountTokens:              func(text string) int { return len(strings.Fields(text)) },
		UseArchiveOriginalTokens: true,
	})
	if result.Applied || !result.ShadowOnly || result.Reason != OCRLReasonRouteNotEligible {
		t.Fatalf("result=%+v want WSS shadow-only", result)
	}
	if result.OriginalTokens != 5 || result.ReplacementTokens == 0 || result.NetSavedTokens == 0 {
		t.Fatalf("archive token accounting missing: %+v", result)
	}
}

func TestBuildOCRLReplacementShadowModeNeverApplies(t *testing.T) {
	t.Parallel()
	result := BuildOCRLReplacement([]Capsule{
		testCapsule(CapsuleCommand, "s", "old-command", "cmd-arch"),
	}, OCRLPolicy{
		Mode:           OCRLModeShadow,
		Route:          OCRLRouteFullHistoryHTTP,
		Selection:      SelectionPolicy{SessionID: "s"},
		ArchiveLoader:  fixedArchiveLoader("cmd-arch"),
		CountTokens:    fixedTokenCounter(10),
		OriginalTokens: 100,
	})
	if result.Applied || !result.ShadowOnly || result.Reason != OCRLReasonShadowOnly {
		t.Fatalf("result=%+v want shadow only", result)
	}
}

func TestBuildOCRLReplacementFailsClosedOnArchiveAndSavingsGates(t *testing.T) {
	t.Parallel()
	capsule := testCapsule(CapsuleFile, "s", "old", "file-arch")
	missingArchive := BuildOCRLReplacement([]Capsule{capsule}, OCRLPolicy{
		Mode:          OCRLModeAuto,
		Route:         OCRLRouteFullHistoryHTTP,
		Selection:     SelectionPolicy{SessionID: "s"},
		ArchiveLoader: func(string) ([]byte, error) { return nil, errors.New("missing") },
		CountTokens:   fixedTokenCounter(1),
	})
	if missingArchive.Applied || missingArchive.Reason != OCRLReasonArchiveUnavailable {
		t.Fatalf("missingArchive=%+v want archive failure", missingArchive)
	}

	missingCounter := BuildOCRLReplacement([]Capsule{capsule}, OCRLPolicy{
		Mode:           OCRLModeAuto,
		Route:          OCRLRouteFullHistoryHTTP,
		Selection:      SelectionPolicy{SessionID: "s"},
		ArchiveLoader:  fixedArchiveLoader("file-arch"),
		OriginalTokens: 100,
	})
	if missingCounter.Applied || missingCounter.Reason != OCRLReasonMissingTokenCounter {
		t.Fatalf("missingCounter=%+v want token-counter failure", missingCounter)
	}

	noSavings := BuildOCRLReplacement([]Capsule{capsule}, OCRLPolicy{
		Mode:              OCRLModeAuto,
		Route:             OCRLRouteFullHistoryHTTP,
		Selection:         SelectionPolicy{SessionID: "s"},
		ArchiveLoader:     fixedArchiveLoader("file-arch"),
		CountTokens:       fixedTokenCounter(99),
		OriginalTokens:    100,
		MinNetSavedTokens: 2,
	})
	if noSavings.Applied || noSavings.Reason != OCRLReasonNetSavingsTooSmall {
		t.Fatalf("noSavings=%+v want net-savings failure", noSavings)
	}
}

func TestBuildOCRLReplacementDelegatesSafetyToSelector(t *testing.T) {
	t.Parallel()
	result := BuildOCRLReplacement([]Capsule{
		testCapsule(CapsuleFailure, "s", "old-failure", "failure-arch"),
		testCapsule(CapsuleFile, "s", "active-file", "file-arch"),
	}, OCRLPolicy{
		Mode:           OCRLModeAuto,
		Route:          OCRLRouteFullHistoryHTTP,
		Selection:      SelectionPolicy{SessionID: "s", ActivePaths: []string{"/repo/a.go"}},
		ArchiveLoader:  fixedArchiveLoader("failure-arch", "file-arch"),
		CountTokens:    fixedTokenCounter(1),
		OriginalTokens: 100,
	})
	if result.Applied || result.Reason != OCRLReasonNoCapsules {
		t.Fatalf("result=%+v want no selected capsules", result)
	}
	if result.Selection.Verbatim != 2 {
		t.Fatalf("selector should preserve risky context verbatim: %+v", result.Selection)
	}
}

func TestRenderOCRLCapsulesIsStable(t *testing.T) {
	t.Parallel()
	capsule := testCapsule(CapsuleCommand, "s", "old", "b", "a")
	capsule.Facts["z"] = "last"
	capsule.Facts["a"] = "first"
	capsule.Hashes = map[string]string{"z_hash": "z", "a_hash": "a"}
	got := RenderOCRLCapsules([]Capsule{capsule})
	want := "[ocrl:v1 selected=1 archive_recoverable=true]\n" +
		"- kind=command session=\"s\" turn=\"old\" source=\"test\" archives=[\"a\",\"b\"] facts={\"a\":\"first\",\"command\":\"go test ./...\",\"exit_code\":\"0\",\"z\":\"last\"} hashes={\"a_hash\":\"a\",\"z_hash\":\"z\"}\n"
	if got != want {
		t.Fatalf("render mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func fixedArchiveLoader(ids ...string) ArchiveLoader {
	allowed := map[string][]byte{}
	for _, id := range ids {
		allowed[id] = []byte("body:" + id)
	}
	return func(id string) ([]byte, error) {
		body, ok := allowed[id]
		if !ok {
			return nil, errors.New("missing archive")
		}
		return body, nil
	}
}

func fixedTokenCounter(tokens int) TokenCounter {
	return func(string) int { return tokens }
}

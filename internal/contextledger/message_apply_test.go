package contextledger

import (
	"errors"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestApplyOCRLToMessagesReplacesExplicitArchiveMatchedBlocks(t *testing.T) {
	t.Parallel()
	fileText := strings.Repeat("old file body ", 30)
	searchText := strings.Repeat("old search body ", 25)
	messages := []types.Message{
		{
			Index: 1,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: fileText, ArchiveID: "old-file-archive", RawBlock: map[string]string{"raw": "file"}},
				{Type: "text", Text: "fresh active instruction"},
			},
		},
		{
			Index: 2,
			Role:  "tool",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: searchText, ArchiveID: "old-search-archive", RawBlock: map[string]string{"raw": "search"}},
			},
		},
	}
	fileCapsule := testFileCapsule(t, "session-1", "turn-old-file", "/repo/a.go", "file-archive")
	searchCapsule := testSearchCapsule(t, "session-1", "turn-old-search", "search-archive")
	result := ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{
		OCRLPolicy: OCRLPolicy{
			Mode:          OCRLModeAuto,
			Route:         OCRLRouteFullHistoryHTTP,
			Selection:     SelectionPolicy{SessionID: "session-1"},
			ArchiveLoader: mapArchiveLoader(map[string]string{"file-archive": fileText, "search-archive": searchText}),
			CountTokens:   wordTokenCounter,
		},
		Targets: []OCRLMessageTarget{
			{MessageIndex: 0, BlockIndex: 0, Capsule: fileCapsule},
			{MessageIndex: 1, BlockIndex: 0, Capsule: searchCapsule},
		},
	})
	if !result.OCRL.Applied || result.OCRL.Reason != OCRLReasonApplied {
		t.Fatalf("result=%+v want applied", result)
	}
	if result.AppliedTargets != 2 || result.CoveredMarkers != 1 {
		t.Fatalf("target accounting=%+v want two targets and one marker", result)
	}
	replaced := result.Messages[0].Content[0]
	if !strings.HasPrefix(replaced.Text, "[ocrl:v1 selected=2 archive_recoverable=true]") {
		t.Fatalf("missing OCRL replacement block: %q", replaced.Text)
	}
	if strings.Contains(replaced.Text, fileText) || strings.Contains(replaced.Text, searchText) {
		t.Fatalf("replacement leaked original payload: %q", replaced.Text)
	}
	if replaced.ArchiveID != "" || replaced.RawBlock != nil {
		t.Fatalf("replacement must force remarshal and clear archive stamp: %+v", replaced)
	}
	if got := result.Messages[0].Content[1].Text; got != "fresh active instruction" {
		t.Fatalf("active sibling block changed: %q", got)
	}
	if got := result.Messages[1].Content[0].Text; got != "[ocrl:v1 covered_by=message:0:block:0]" {
		t.Fatalf("single-block covered target should keep marker, got %q", got)
	}
	if result.Messages[1].Content[0].ArchiveID != "" || result.Messages[1].Content[0].RawBlock != nil {
		t.Fatalf("covered marker must clear archive/raw metadata: %+v", result.Messages[1].Content[0])
	}
	if messages[0].Content[0].Text != fileText || messages[0].Content[0].ArchiveID == "" || messages[0].Content[0].RawBlock == nil {
		t.Fatalf("input messages were mutated: %+v", messages[0].Content[0])
	}
}

func TestApplyOCRLToMessagesDeletesCoveredBlocksInMultiBlockMessages(t *testing.T) {
	t.Parallel()
	firstText := strings.Repeat("first old block ", 20)
	secondText := strings.Repeat("second old block ", 20)
	messages := []types.Message{
		{
			Index: 1,
			Role:  "tool",
			Content: []types.ContentBlock{
				{Type: "tool_result", Text: firstText, ArchiveID: "first-arch"},
				{Type: "tool_result", Text: secondText, ArchiveID: "second-arch"},
				{Type: "text", Text: "still here"},
			},
		},
	}
	firstCapsule := testSearchCapsule(t, "session-1", "turn-first", "first-archive")
	secondCapsule := testSearchCapsule(t, "session-1", "turn-second", "second-archive")
	result := ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{
		OCRLPolicy: OCRLPolicy{
			Mode:          OCRLModeMax,
			Route:         OCRLRouteFullHistoryHTTP,
			Selection:     SelectionPolicy{SessionID: "session-1"},
			ArchiveLoader: mapArchiveLoader(map[string]string{"first-archive": firstText, "second-archive": secondText}),
			CountTokens:   wordTokenCounter,
		},
		Targets: []OCRLMessageTarget{
			{MessageIndex: 0, BlockIndex: 0, Capsule: firstCapsule},
			{MessageIndex: 0, BlockIndex: 1, Capsule: secondCapsule},
		},
	})
	if !result.OCRL.Applied || result.CoveredMarkers != 0 {
		t.Fatalf("result=%+v want applied without markers", result)
	}
	if len(result.Messages[0].Content) != 2 {
		t.Fatalf("content length=%d want replacement plus untouched block", len(result.Messages[0].Content))
	}
	if got := result.Messages[0].Content[1].Text; got != "still here" {
		t.Fatalf("wrong remaining block: %q", got)
	}
}

func TestApplyOCRLToMessagesFullPassesOnArchiveMismatch(t *testing.T) {
	t.Parallel()
	messages := []types.Message{{
		Index:   1,
		Role:    "tool",
		Content: []types.ContentBlock{{Type: "tool_result", Text: "real payload", ArchiveID: "arch"}},
	}}
	result := ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{
		OCRLPolicy: OCRLPolicy{
			Mode:          OCRLModeAuto,
			Route:         OCRLRouteFullHistoryHTTP,
			Selection:     SelectionPolicy{SessionID: "session-1"},
			ArchiveLoader: mapArchiveLoader(map[string]string{"archive": "different payload"}),
			CountTokens:   wordTokenCounter,
		},
		Targets: []OCRLMessageTarget{
			{MessageIndex: 0, BlockIndex: 0, Capsule: testSearchCapsule(t, "session-1", "turn-old", "archive")},
		},
	})
	if result.OCRL.Applied || result.OCRL.Reason != OCRLReasonTargetArchiveMismatch {
		t.Fatalf("result=%+v want archive mismatch full-pass", result)
	}
	if result.Messages[0].Content[0].Text != "real payload" || result.Messages[0].Content[0].ArchiveID != "arch" {
		t.Fatalf("message changed despite mismatch: %+v", result.Messages[0].Content[0])
	}
}

func TestApplyOCRLToMessagesNormalizesSingleArchiveID(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("trimmed archive target ", 20)
	capsule := testSearchCapsule(t, "session-1", "turn-old", " archive ")
	messages := []types.Message{{
		Index:   1,
		Role:    "tool",
		Content: []types.ContentBlock{{Type: "tool_result", Text: text}},
	}}
	result := ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{
		OCRLPolicy: OCRLPolicy{
			Mode:          OCRLModeAuto,
			Route:         OCRLRouteFullHistoryHTTP,
			Selection:     SelectionPolicy{SessionID: "session-1"},
			ArchiveLoader: mapArchiveLoader(map[string]string{"archive": text}),
			CountTokens:   wordTokenCounter,
		},
		Targets: []OCRLMessageTarget{{MessageIndex: 0, BlockIndex: 0, Capsule: capsule}},
	})
	if !result.OCRL.Applied || result.OCRL.Reason != OCRLReasonApplied {
		t.Fatalf("result=%+v want archive id trimmed and applied", result)
	}
}

func TestApplyOCRLToMessagesRejectsMultipleArchiveIDs(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("multi archive target ", 20)
	capsule := testSearchCapsule(t, "session-1", "turn-old", "archive")
	capsule.Archives = append(capsule.Archives, "other")
	messages := []types.Message{{
		Index:   1,
		Role:    "tool",
		Content: []types.ContentBlock{{Type: "tool_result", Text: text}},
	}}
	result := ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{
		OCRLPolicy: OCRLPolicy{
			Mode:          OCRLModeAuto,
			Route:         OCRLRouteFullHistoryHTTP,
			Selection:     SelectionPolicy{SessionID: "session-1"},
			ArchiveLoader: mapArchiveLoader(map[string]string{"archive": text, "other": text}),
			CountTokens:   wordTokenCounter,
		},
		Targets: []OCRLMessageTarget{{MessageIndex: 0, BlockIndex: 0, Capsule: capsule}},
	})
	if result.OCRL.Applied || result.OCRL.Reason != OCRLReasonTargetArchiveMismatch {
		t.Fatalf("result=%+v want multi-archive target rejected", result)
	}
	if result.Messages[0].Content[0].Text != text {
		t.Fatalf("multi-archive mismatch mutated messages: %+v", result.Messages)
	}
}

func TestApplyOCRLToMessagesHonorsShadowAndRouteGates(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("old route gated body ", 20)
	messages := []types.Message{{Index: 1, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: text}}}}
	target := OCRLMessageTarget{MessageIndex: 0, BlockIndex: 0, Capsule: testSearchCapsule(t, "session-1", "turn-old", "archive")}
	base := OCRLPolicy{
		Selection:     SelectionPolicy{SessionID: "session-1"},
		ArchiveLoader: mapArchiveLoader(map[string]string{"archive": text}),
		CountTokens:   wordTokenCounter,
	}
	for _, tc := range []struct {
		name   string
		mode   OCRLMode
		route  OCRLRoute
		reason OCRLReason
	}{
		{name: "shadow", mode: OCRLModeShadow, route: OCRLRouteFullHistoryHTTP, reason: OCRLReasonShadowOnly},
		{name: "codex-wss", mode: OCRLModeMax, route: OCRLRouteCodexWSS, reason: OCRLReasonRouteNotEligible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			policy := base
			policy.Mode = tc.mode
			policy.Route = tc.route
			result := ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{OCRLPolicy: policy, Targets: []OCRLMessageTarget{target}})
			if result.OCRL.Applied || result.OCRL.Reason != tc.reason {
				t.Fatalf("result=%+v want gated reason %q", result, tc.reason)
			}
			if result.Messages[0].Content[0].Text != text {
				t.Fatalf("gated route mutated context: %+v", result.Messages[0].Content[0])
			}
		})
	}
}

func TestApplyOCRLToMessagesCountsCoveredMarkerOverhead(t *testing.T) {
	t.Parallel()
	firstText := strings.Repeat("alpha ", 12)
	secondText := strings.Repeat("beta ", 12)
	messages := []types.Message{
		{Index: 1, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: firstText}}},
		{Index: 2, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: secondText}}},
	}
	countTokens := func(text string) int {
		if strings.HasPrefix(text, ocrlCoveredMarkerPrefix) {
			return 20
		}
		if strings.HasPrefix(text, "[ocrl:v1 selected=") {
			return 5
		}
		return len(strings.Fields(text))
	}
	result := ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{
		OCRLPolicy: OCRLPolicy{
			Mode:              OCRLModeAuto,
			Route:             OCRLRouteFullHistoryHTTP,
			Selection:         SelectionPolicy{SessionID: "session-1"},
			ArchiveLoader:     mapArchiveLoader(map[string]string{"first-archive": firstText, "second-archive": secondText}),
			CountTokens:       countTokens,
			MinNetSavedTokens: 1,
		},
		Targets: []OCRLMessageTarget{
			{MessageIndex: 0, BlockIndex: 0, Capsule: testSearchCapsule(t, "session-1", "turn-first", "first-archive")},
			{MessageIndex: 1, BlockIndex: 0, Capsule: testSearchCapsule(t, "session-1", "turn-second", "second-archive")},
		},
	})
	if result.OCRL.Applied || result.OCRL.Reason != OCRLReasonNetSavingsTooSmall {
		t.Fatalf("result=%+v want marker overhead to block mutation", result)
	}
	if result.OCRL.NetSavedTokens != -1 {
		t.Fatalf("net savings should include marker overhead, got %+v", result.OCRL)
	}
	if result.Messages[0].Content[0].Text != firstText || result.Messages[1].Content[0].Text != secondText {
		t.Fatalf("failed net-savings gate mutated messages: %+v", result.Messages)
	}
}

func TestApplyOCRLToMessagesCountsOnlySelectedTargetsForFinalSavings(t *testing.T) {
	t.Parallel()
	fileText := strings.Repeat("excluded active file ", 60)
	searchText := "small selected search"
	messages := []types.Message{
		{Index: 1, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: fileText}}},
		{Index: 2, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: searchText}}},
	}
	countTokens := func(text string) int {
		if strings.HasPrefix(text, "[ocrl:v1 selected=") {
			return 5
		}
		return len(strings.Fields(text))
	}
	searchCapsule, err := BuildSearchCapsule(SearchObservation{
		SessionID:    "session-1",
		TurnID:       "turn-search",
		CommandLine:  "rg -n TODO b.go",
		RepoRoot:     "/repo",
		PatternHash:  "pattern-sha",
		FilesMatched: []string{"b.go"},
		OmittedCount: 1,
		Output:       []byte("b.go:1:TODO"),
		ArchiveID:    "search-archive",
	})
	if err != nil {
		t.Fatalf("BuildSearchCapsule error: %v", err)
	}
	result := ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{
		OCRLPolicy: OCRLPolicy{
			Mode:  OCRLModeAuto,
			Route: OCRLRouteFullHistoryHTTP,
			Selection: SelectionPolicy{
				SessionID:   "session-1",
				ActivePaths: []string{"/repo/a.go"},
			},
			ArchiveLoader: mapArchiveLoader(map[string]string{"file-archive": fileText, "search-archive": searchText}),
			CountTokens:   countTokens,
		},
		Targets: []OCRLMessageTarget{
			{MessageIndex: 0, BlockIndex: 0, Capsule: testFileCapsule(t, "session-1", "turn-file", "/repo/a.go", "file-archive")},
			{MessageIndex: 1, BlockIndex: 0, Capsule: searchCapsule},
		},
	})
	if result.OCRL.Applied || result.OCRL.Reason != OCRLReasonNetSavingsTooSmall {
		t.Fatalf("result=%+v want selected-only savings gate to block", result)
	}
	if result.OCRL.NetSavedTokens != -2 {
		t.Fatalf("net savings should count only the selected search target, got %+v", result.OCRL)
	}
	if result.Messages[0].Content[0].Text != fileText || result.Messages[1].Content[0].Text != searchText {
		t.Fatalf("failed selected-only gate mutated messages: %+v", result.Messages)
	}
}

func TestApplyOCRLToMessagesRejectsDuplicateTargets(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("duplicate target body ", 10)
	capsule := testSearchCapsule(t, "session-1", "turn-old", "archive")
	messages := []types.Message{{Index: 1, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: text}}}}
	result := ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{
		OCRLPolicy: OCRLPolicy{
			Mode:          OCRLModeAuto,
			Route:         OCRLRouteFullHistoryHTTP,
			Selection:     SelectionPolicy{SessionID: "session-1"},
			ArchiveLoader: mapArchiveLoader(map[string]string{"archive": text}),
			CountTokens:   wordTokenCounter,
		},
		Targets: []OCRLMessageTarget{
			{MessageIndex: 0, BlockIndex: 0, Capsule: capsule},
			{MessageIndex: 0, BlockIndex: 0, Capsule: capsule},
		},
	})
	if result.OCRL.Applied || result.OCRL.Reason != OCRLReasonTargetInvalid {
		t.Fatalf("result=%+v want duplicate target rejected", result)
	}
}

func TestApplyOCRLToMessagesByArchiveMatchDerivesExactTargets(t *testing.T) {
	t.Parallel()
	fileText := strings.Repeat("derived old file ", 30)
	searchText := strings.Repeat("derived old search ", 30)
	messages := []types.Message{
		{Index: 1, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: fileText}}},
		{Index: 2, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: searchText}}},
		{Index: 3, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "keep active request"}}},
	}
	result, derivation := ApplyOCRLToMessagesByArchiveMatch(messages, []Capsule{
		testFileCapsule(t, "session-1", "turn-file", "/repo/a.go", "file-archive"),
		testSearchCapsule(t, "session-1", "turn-search", "search-archive"),
	}, OCRLPolicy{
		Mode:          OCRLModeAuto,
		Route:         OCRLRouteFullHistoryHTTP,
		Selection:     SelectionPolicy{SessionID: "session-1"},
		ArchiveLoader: mapArchiveLoader(map[string]string{"file-archive": fileText, "search-archive": searchText}),
		CountTokens:   wordTokenCounter,
	})
	if derivation.Matched != 2 || len(derivation.Targets) != 2 ||
		derivation.Ambiguous != 0 || derivation.Unmatched != 0 ||
		derivation.MissingArchive != 0 || derivation.ArchiveErrors != 0 ||
		derivation.DuplicateTarget != 0 {
		t.Fatalf("unexpected derivation: %+v", derivation)
	}
	if !result.OCRL.Applied || result.AppliedTargets != 2 {
		t.Fatalf("result=%+v want archive-derived OCRL apply", result)
	}
	if strings.Contains(result.Messages[0].Content[0].Text, fileText) ||
		strings.Contains(result.Messages[0].Content[0].Text, searchText) {
		t.Fatalf("derived replacement leaked raw context: %q", result.Messages[0].Content[0].Text)
	}
	if got := result.Messages[2].Content[0].Text; got != "keep active request" {
		t.Fatalf("untargeted active request changed: %q", got)
	}
}

func TestDeriveOCRLMessageTargetsFailsClosedOnAmbiguousOrMissingEvidence(t *testing.T) {
	t.Parallel()
	shared := strings.Repeat("same old context ", 20)
	unique := strings.Repeat("unique old context ", 20)
	messages := []types.Message{
		{Index: 1, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: shared}}},
		{Index: 2, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: shared}}},
		{Index: 3, Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: unique}}},
	}
	duplicateA := testSearchCapsule(t, "session-1", "turn-dup-a", "unique-a")
	duplicateB := testSearchCapsule(t, "session-1", "turn-dup-b", "unique-b")
	missingArchive := testCapsule(CapsuleSearch, "session-1", "turn-missing")
	multiArchive := testCapsule(CapsuleSearch, "session-1", "turn-multi", "a", "b")
	derivation := DeriveOCRLMessageTargets(messages, []Capsule{
		testSearchCapsule(t, "session-1", "turn-ambiguous", "ambiguous"),
		testSearchCapsule(t, "session-1", "turn-unmatched", "unmatched"),
		testSearchCapsule(t, "session-1", "turn-error", "error"),
		missingArchive,
		multiArchive,
		duplicateA,
		duplicateB,
	}, func(id string) ([]byte, error) {
		switch id {
		case "ambiguous":
			return []byte(shared), nil
		case "unmatched":
			return []byte("not in messages"), nil
		case "error":
			return nil, errors.New("archive missing")
		case "unique-a", "unique-b":
			return []byte(unique), nil
		default:
			return nil, errors.New("unexpected archive")
		}
	})
	if derivation.Matched != 1 || len(derivation.Targets) != 1 {
		t.Fatalf("expected one unambiguous target, got %+v", derivation)
	}
	if derivation.Ambiguous != 1 || derivation.Unmatched != 1 ||
		derivation.ArchiveErrors != 1 || derivation.MissingArchive != 2 ||
		derivation.DuplicateTarget != 1 {
		t.Fatalf("bad fail-closed counters: %+v", derivation)
	}
	if derivation.Targets[0].MessageIndex != 2 || derivation.Targets[0].BlockIndex != 0 {
		t.Fatalf("wrong derived target: %+v", derivation.Targets[0])
	}
}

func testFileCapsule(t *testing.T, sessionID, turnID, path, archiveID string) Capsule {
	t.Helper()
	capsule, err := BuildFileCapsule(FileObservation{
		SessionID:    sessionID,
		TurnID:       turnID,
		Path:         path,
		RepoRoot:     "/repo",
		Range:        "1:120",
		Content:      []byte("archived content"),
		ArchiveID:    archiveID,
		FullPassTurn: "turn-full",
	})
	if err != nil {
		t.Fatalf("BuildFileCapsule error: %v", err)
	}
	return capsule
}

func testSearchCapsule(t *testing.T, sessionID, turnID, archiveID string) Capsule {
	t.Helper()
	capsule, err := BuildSearchCapsule(SearchObservation{
		SessionID:    sessionID,
		TurnID:       turnID,
		CommandLine:  "rg -n TODO .",
		RepoRoot:     "/repo",
		PatternHash:  "pattern-sha",
		FilesMatched: []string{"a.go"},
		OmittedCount: 1,
		Output:       []byte("a.go:1:TODO"),
		ArchiveID:    archiveID,
	})
	if err != nil {
		t.Fatalf("BuildSearchCapsule error: %v", err)
	}
	return capsule
}

func mapArchiveLoader(values map[string]string) ArchiveLoader {
	return func(id string) ([]byte, error) {
		value, ok := values[id]
		if !ok {
			return nil, errors.New("missing archive")
		}
		return []byte(value), nil
	}
}

func wordTokenCounter(text string) int {
	return len(strings.Fields(text))
}

package summarization

import (
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func buildAnchorMessages(t *testing.T) ([]types.Message, []int) {
	t.Helper()
	msgs := make([]types.Message, 20)
	for i := range msgs {
		msgs[i] = msg(t, i, "user", "normal message "+string(rune('a'+i)))
	}
	msgs[3] = toolUseMsg(t, 3, "edit_file")
	msgs[4] = toolResultMsg(t, 4, "edit applied to handler.go")
	msgs[7] = msg(t, 7, "assistant", "panic: runtime error: nil pointer dereference")
	msgs[11] = msg(t, 11, "user", "yes")
	msgs[15] = toolResultMsg(t, 15, "wrote config.toml with new settings")
	msgs[17] = msg(t, 17, "assistant", "architecture\n- A\n- B\n- C\n- D")
	anchorIndices := []int{3, 4, 7, 11, 15, 17}
	return msgs, anchorIndices
}

func TestApplyToMessagesSession_AnchorVerbatim(t *testing.T) {
	t.Parallel()
	l := NewLayer2(testCompressionConfig())
	l.cfg.Summary.AllowModelFacingReplacement = true

	msgs, anchorIndices := buildAnchorMessages(t)
	anchorMsgs := make([]types.Message, len(anchorIndices))
	for i, idx := range anchorIndices {
		anchorMsgs[i] = deepCopyMessage(msgs[idx])
	}

	l.sessions.Store("s1", &CachedSummary{
		Summary:          "test summary",
		CoveredRange:     [2]int{0, 18},
		AnchorsInlined:   anchorIndices,
		AnchorMessages:   anchorMsgs,
		OriginalTokens:   1000,
		CompressedTokens: 100,
		CreatedAt:        time.Now(),
	})

	result, saved, applied := l.ApplyToMessagesSession("s1", msgs)
	if !applied {
		t.Fatal("expected applied")
	}
	if saved != 900 {
		t.Fatalf("saved=%d want 900", saved)
	}

	syntheticText := result[0].Content[0].Text
	if !strings.Contains(syntheticText, "excluding anchors at") {
		t.Fatalf("summary text should mention excluded anchors: %s", syntheticText)
	}

	anchoredCount := 0
	for _, idx := range anchorIndices {
		origText := fullText(msgs[idx])
		if origText == "" {
			hasContent := false
			for _, blk := range msgs[idx].Content {
				if blk.ToolName != "" || blk.Type == "tool_use" {
					hasContent = true
					break
				}
			}
			if hasContent {
				for _, r := range result {
					for _, blk := range r.Content {
						if blk.ToolName != "" || blk.Type == "tool_use" {
							anchoredCount++
							break
						}
					}
				}
			} else {
				anchoredCount++
			}
			continue
		}
		for _, r := range result {
			if strings.Contains(fullText(r), origText) {
				anchoredCount++
				break
			}
		}
	}
	if anchoredCount < len(anchorIndices) {
		t.Fatalf("found %d/%d anchor messages in result", anchoredCount, len(anchorIndices))
	}

	for i, m := range result {
		if m.Index != i {
			t.Fatalf("index mismatch at %d: got %d", i, m.Index)
		}
	}
}

func TestApplyToMessagesSession_BudgetOverflow(t *testing.T) {
	t.Parallel()
	cfg := testCompressionConfig()
	cfg.Summary.MaxAnchorsInlined = 3
	cfg.Summary.AllowModelFacingReplacement = true
	l := NewLayer2(cfg)

	msgs := make([]types.Message, 20)
	for i := range msgs {
		msgs[i] = msg(t, i, "user", "message with long enough text to test truncation path that exceeds eighty characters total "+string(rune('a'+i)))
	}

	anchorIndices := []int{0, 2, 4, 6, 8, 10, 12, 14, 16, 18}
	anchorMsgs := make([]types.Message, len(anchorIndices))
	for i, idx := range anchorIndices {
		anchorMsgs[i] = deepCopyMessage(msgs[idx])
	}

	l.sessions.Store("s2", &CachedSummary{
		Summary:          "budget test",
		CoveredRange:     [2]int{0, 18},
		AnchorsInlined:   anchorIndices,
		AnchorMessages:   anchorMsgs,
		OriginalTokens:   2000,
		CompressedTokens: 200,
		CreatedAt:        time.Now(),
	})

	result, _, applied := l.ApplyToMessagesSession("s2", msgs)
	if !applied {
		t.Fatal("expected applied")
	}

	verbatimCount := 0
	digestCount := 0
	for i := 1; i <= len(anchorMsgs); i++ {
		if i < len(result) {
			text := fullText(result[i])
			if strings.HasPrefix(text, "[anchor:") {
				digestCount++
			} else {
				verbatimCount++
			}
		}
	}
	if verbatimCount != 3 {
		t.Fatalf("verbatim=%d want 3", verbatimCount)
	}
	if digestCount != 7 {
		t.Fatalf("digest=%d want 7", digestCount)
	}

	stats := l.CacheStats()
	if stats.AnchorsTotal != 10 {
		t.Fatalf("AnchorsTotal=%d want 10", stats.AnchorsTotal)
	}
	if stats.AnchorsVerbatim != 3 {
		t.Fatalf("AnchorsVerbatim=%d want 3", stats.AnchorsVerbatim)
	}
	if stats.AnchorsDemoted != 7 {
		t.Fatalf("AnchorsDemoted=%d want 7", stats.AnchorsDemoted)
	}
}

func TestApplyToMessagesSession_ValidatorFallback(t *testing.T) {
	t.Parallel()
	l := NewLayer2(testCompressionConfig())
	l.cfg.Summary.AllowModelFacingReplacement = true

	msgs := make([]types.Message, 10)
	for i := range msgs {
		msgs[i] = msg(t, i, "user", "msg "+string(rune('a'+i)))
	}
	msgs[5] = msg(t, 5, "assistant", "critical edit applied to handler.go")

	anchorIndices := []int{5}
	emptyAnchorMsgs := []types.Message{}

	l.sessions.Store("s3", &CachedSummary{
		Summary:          "validator test",
		CoveredRange:     [2]int{0, 8},
		AnchorsInlined:   anchorIndices,
		AnchorMessages:   emptyAnchorMsgs,
		OriginalTokens:   500,
		CompressedTokens: 50,
		CreatedAt:        time.Now(),
	})

	result, saved, applied := l.ApplyToMessagesSession("s3", msgs)
	if applied {
		t.Fatal("should fallback when anchor missing")
	}
	if saved != 0 {
		t.Fatalf("saved=%d want 0 on fallback", saved)
	}
	if len(result) != len(msgs) {
		t.Fatalf("result len=%d want original %d", len(result), len(msgs))
	}
}

func TestApplyToMessagesSession_NoAnchors(t *testing.T) {
	t.Parallel()
	l := NewLayer2(testCompressionConfig())
	l.cfg.Summary.AllowModelFacingReplacement = true

	msgs := makeTestMessages(10)
	l.sessions.Store("s4", &CachedSummary{
		Summary:          "no anchors",
		CoveredRange:     [2]int{0, 5},
		OriginalTokens:   100,
		CompressedTokens: 20,
		CreatedAt:        time.Now(),
	})

	result, saved, applied := l.ApplyToMessagesSession("s4", msgs)
	if !applied {
		t.Fatal("expected applied")
	}
	if saved != 80 {
		t.Fatalf("saved=%d", saved)
	}
	syntheticText := result[0].Content[0].Text
	if strings.Contains(syntheticText, "excluding anchors") {
		t.Fatal("no anchors should not mention excluding anchors")
	}
}

func TestValidateApply_AllPresent(t *testing.T) {
	t.Parallel()
	v := NewCompressionValidator()
	original := []types.Message{
		msg(t, 0, "user", "hello"),
		msg(t, 1, "assistant", "world"),
		msg(t, 2, "user", "important error occurred"),
	}
	post := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "summary"}}},
		{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "important error occurred"}}},
	}
	result := v.ValidateApply(original, post, []int{2}, 8)
	if !result.Valid {
		t.Fatalf("should be valid: %s", result.FailReason)
	}
}

func TestValidateApply_MissingAnchor(t *testing.T) {
	t.Parallel()
	v := NewCompressionValidator()
	original := []types.Message{
		msg(t, 0, "user", "hello"),
		msg(t, 1, "assistant", "critical edit applied"),
	}
	post := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "summary"}}},
	}
	result := v.ValidateApply(original, post, []int{1}, 8)
	if result.Valid {
		t.Fatal("should detect missing anchor")
	}
	if !strings.Contains(result.FailReason, "anchor lost") {
		t.Fatalf("unexpected reason: %s", result.FailReason)
	}
}

func TestValidateApply_EmptyIndices(t *testing.T) {
	t.Parallel()
	v := NewCompressionValidator()
	result := v.ValidateApply(nil, nil, nil, 8)
	if !result.Valid {
		t.Fatal("empty anchors should be valid")
	}
}

func TestValidateApply_OutOfBoundsIndex(t *testing.T) {
	t.Parallel()
	v := NewCompressionValidator()
	original := []types.Message{
		msg(t, 0, "user", "hello"),
	}
	post := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "summary"}}},
	}
	result := v.ValidateApply(original, post, []int{5, 10}, 8)
	if !result.Valid {
		t.Fatalf("out-of-bounds anchor should be skipped: %s", result.FailReason)
	}
}

func TestBuildSummaryText(t *testing.T) {
	text := buildSummaryText(10, []int{3, 7}, "the summary")
	if !strings.Contains(text, "excluding anchors at 3, 7") {
		t.Fatalf("expected excluded anchor indices: %s", text)
	}

	textNoAnchors := buildSummaryText(10, nil, "the summary")
	if strings.Contains(textNoAnchors, "excluding") {
		t.Fatalf("should not mention excluding when no anchors: %s", textNoAnchors)
	}
}

func TestDeepCopyMessage(t *testing.T) {
	original := msg(t, 0, "user", "hello")
	cp := deepCopyMessage(original)
	cp.Content[0].Text = "modified"
	if original.Content[0].Text != "hello" {
		t.Fatal("deep copy should not share content slices")
	}
}

func TestMaxAnchorsInlined_Default(t *testing.T) {
	cfg := testCompressionConfig()
	l := NewLayer2(cfg)
	if l.maxAnchorsInlined() != defaultMaxAnchorsInlined {
		t.Fatalf("default=%d want %d", l.maxAnchorsInlined(), defaultMaxAnchorsInlined)
	}
	cfg.Summary.MaxAnchorsInlined = 20
	l2 := NewLayer2(cfg)
	if l2.maxAnchorsInlined() != 20 {
		t.Fatalf("configured=%d want 20", l2.maxAnchorsInlined())
	}
}

func TestSelectAnchors_Empty(t *testing.T) {
	result := selectAnchors(nil, nil, 8)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestPrioritizeAnchors(t *testing.T) {
	t.Parallel()
	d := NewAnchorDetector()
	msgs := []types.Message{
		msg(t, 0, "user", "normal"),
		msg(t, 1, "assistant", "error: something failed"),
		msg(t, 2, "user", "yes"),
		msg(t, 3, "assistant", "architecture\n- a\n- b\n- c\n- d"),
	}
	indices := []int{1, 2, 3}

	pa := prioritizeAnchors(d, indices, msgs)
	if len(pa) != 3 {
		t.Fatalf("got %d items", len(pa))
	}
	if pa[0].category != anchorError {
		t.Fatalf("first should be error, got %d", pa[0].category)
	}
	if pa[1].category != anchorDecision {
		t.Fatalf("second should be decision, got %d", pa[1].category)
	}
	if pa[2].category != anchorArchitect {
		t.Fatalf("third should be architect, got %d", pa[2].category)
	}

	paOOB := prioritizeAnchors(d, []int{1, 99, 100}, msgs)
	if len(paOOB) != 1 {
		t.Fatalf("OOB: expected 1 item, got %d", len(paOOB))
	}
}

func TestClassifyAnchor(t *testing.T) {
	t.Parallel()
	d := NewAnchorDetector()
	editMsg := toolUseMsg(t, 0, "write_file")
	if d.classifyAnchor(editMsg) != anchorEdit {
		t.Fatal("expected anchorEdit")
	}
	errorMsg := msg(t, 1, "assistant", "panic: nil pointer")
	if d.classifyAnchor(errorMsg) != anchorError {
		t.Fatal("expected anchorError")
	}
	decisionMsg := msg(t, 2, "user", "yes")
	if d.classifyAnchor(decisionMsg) != anchorDecision {
		t.Fatal("expected anchorDecision")
	}
	configMsg := toolResultMsg(t, 3, "wrote config.toml")
	if d.classifyAnchor(configMsg) != anchorConfig {
		t.Fatal("expected anchorConfig")
	}
	archMsg := msg(t, 4, "assistant", "architecture\n- a\n- b\n- c\n- d")
	if d.classifyAnchor(archMsg) != anchorArchitect {
		t.Fatal("expected anchorArchitect")
	}
	normalMsg := msg(t, 5, "user", "hello")
	if d.classifyAnchor(normalMsg) != anchorUnknown {
		t.Fatal("expected anchorUnknown")
	}
}

func TestClassifyStoredAnchor(t *testing.T) {
	t.Parallel()
	editMsg := toolUseMsg(t, 0, "edit_file")
	if classifyStoredAnchor(editMsg) != anchorEdit {
		t.Fatal("expected anchorEdit")
	}

	errorMsg := msg(t, 1, "assistant", "error: something failed")
	if classifyStoredAnchor(errorMsg) != anchorError {
		t.Fatal("expected anchorError")
	}

	decisionMsg := msg(t, 2, "user", "yes")
	if classifyStoredAnchor(decisionMsg) != anchorDecision {
		t.Fatal("expected anchorDecision")
	}

	normalMsg := msg(t, 3, "user", "hello world")
	if classifyStoredAnchor(normalMsg) != anchorUnknown {
		t.Fatalf("expected anchorUnknown, got %d", classifyStoredAnchor(normalMsg))
	}
}

func TestAnchorCategoryString(t *testing.T) {
	cases := map[anchorCategory]string{
		anchorError:     "error",
		anchorEdit:      "edit",
		anchorDecision:  "decision",
		anchorConfig:    "config",
		anchorArchitect: "architect",
		anchorUnknown:   "generic",
	}
	for cat, want := range cases {
		if got := anchorCategoryString(cat); got != want {
			t.Errorf("category %d: got %q want %q", cat, got, want)
		}
	}
}

func TestApplyToMessages_LegacyWithAnchors(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults().Compression
	cfg.Summary.AllowModelFacingReplacement = true
	l := NewLayer2(&cfg)

	msgs := []types.Message{
		msg(t, 0, "user", "hello"),
		msg(t, 1, "assistant", "world"),
		msg(t, 2, "user", "question"),
		msg(t, 3, "assistant", "answer"),
		msg(t, 4, "user", "tail"),
	}

	anchorMsgs := []types.Message{deepCopyMessage(msgs[1])}
	l.cache.Store(&CachedSummary{
		Summary:          "legacy test",
		CoveredRange:     [2]int{0, 2},
		AnchorsInlined:   []int{1},
		AnchorMessages:   anchorMsgs,
		OriginalTokens:   400,
		CompressedTokens: 40,
		CreatedAt:        time.Now(),
	})

	out, saved, ok := l.ApplyToMessages(msgs)
	if !ok || saved != 360 {
		t.Fatalf("ok=%v saved=%d", ok, saved)
	}

	found := false
	for _, m := range out {
		if strings.Contains(fullText(m), "world") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("anchor message 'world' should be in output")
	}
}

func TestContainsVerbatimAnchor(t *testing.T) {
	post := types.Message{
		Content: []types.ContentBlock{{Type: "text", Text: "the error occurred at line 42"}},
	}
	orig := types.Message{
		Content: []types.ContentBlock{{Type: "text", Text: "error occurred"}},
	}
	if !containsVerbatimAnchor(post, orig) {
		t.Fatal("should find verbatim anchor")
	}

	origEmpty := types.Message{Content: []types.ContentBlock{{Type: "text", Text: ""}}}
	if !containsVerbatimAnchor(post, origEmpty) {
		t.Fatal("empty orig text should match")
	}
}

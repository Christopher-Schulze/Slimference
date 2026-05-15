package summarization

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/types"
)

func TestBuildContextCapsules_microArchiveBackedAndAnchorSafe(t *testing.T) {
	dir := t.TempDir()
	large := strings.Repeat("large tool output line\n", 80)
	editLarge := strings.Repeat("edit output must remain verbatim\n", 80)
	msgs := []types.Message{
		{Index: 0, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-1", Text: large}}},
		{Index: 1, Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolName: "apply_patch"}}},
		{Index: 2, Role: "user", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "edit-1", Text: editLarge}}},
		{Index: 3, Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "live tail"}}},
	}

	capsules, err := BuildContextCapsules(msgs, CapsuleBuildOptions{
		SessionID:                "sess-1",
		ArchiveDir:               dir,
		ActiveTailMessages:       1,
		MicroToolResultMinTokens: 10,
		PhaseMinMessages:         10,
		SessionMinPhaseCapsules:  2,
		SessionMinOriginalTokens: 10,
		Now:                      time.Unix(1, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	micro := CapsulesByTier(capsules, CapsuleTierMicro)
	if len(micro) != 1 {
		t.Fatalf("micro capsules = %d, want 1: %+v", len(micro), capsules)
	}
	if micro[0].SourceRange != [2]int{0, 0} || len(micro[0].ArchiveURIs) != 1 {
		t.Fatalf("bad micro capsule: %+v", micro[0])
	}
	_, body, err := contentarchive.Get(dir, micro[0].ArchiveURIs[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != large {
		t.Fatal("archive expansion did not return original tool output")
	}
}

func TestBuildContextCapsules_phaseSessionAndTierSelection(t *testing.T) {
	dir := t.TempDir()
	msgs := []types.Message{
		textMsg(0, "user", "task alpha"),
		textMsg(1, "assistant", strings.Repeat("alpha work ", 80)),
		textMsg(2, "assistant", "alpha result"),
		textMsg(3, "user", "next task beta"),
		textMsg(4, "assistant", strings.Repeat("beta work ", 80)),
		textMsg(5, "assistant", "beta result"),
		textMsg(6, "user", "live tail"),
	}

	capsules, err := BuildContextCapsules(msgs, CapsuleBuildOptions{
		SessionID:                "sess-2",
		ArchiveDir:               dir,
		ActiveTailMessages:       1,
		MicroToolResultMinTokens: 500,
		PhaseMinMessages:         2,
		SessionMinPhaseCapsules:  2,
		SessionMinOriginalTokens: 10,
		Now:                      time.Unix(2, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	phases := CapsulesByTier(capsules, CapsuleTierPhase)
	if len(phases) != 2 {
		t.Fatalf("phase capsules = %d, want 2: %+v", len(phases), capsules)
	}
	sessions := CapsulesByTier(capsules, CapsuleTierSession)
	if len(sessions) != 1 {
		t.Fatalf("session capsules = %d, want 1: %+v", len(sessions), capsules)
	}
	if got := CapsulesByTier(capsules, CapsuleTierMicro); len(got) != 0 {
		t.Fatalf("micro selector = %d, want 0", len(got))
	}
	if sessions[0].ProjectedSavingsTokens <= 0 || len(sessions[0].ArchiveURIs) < 3 {
		t.Fatalf("weak session capsule: %+v", sessions[0])
	}
	_, body, err := contentarchive.Get(dir, sessions[0].ArchiveURIs[len(sessions[0].ArchiveURIs)-1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "task alpha") || !strings.Contains(string(body), "next task beta") {
		t.Fatalf("session archive missing source content: %s", body)
	}
}

func TestContextCapsuleOptionAndBranchCoverage(t *testing.T) {
	dir := t.TempDir()
	defaults := DefaultCapsuleBuildOptions("sess-default", dir)
	if defaults.SessionID != "sess-default" || defaults.ArchiveDir != dir || defaults.ActiveTailMessages == 0 || defaults.Now.IsZero() {
		t.Fatalf("bad defaults: %+v", defaults)
	}
	normalized := normalizeCapsuleOptions(CapsuleBuildOptions{})
	if normalized.ActiveTailMessages != 6 ||
		normalized.MicroToolResultMinTokens != 800 ||
		normalized.PhaseMinMessages != 4 ||
		normalized.SessionMinPhaseCapsules != 2 ||
		normalized.SessionMinOriginalTokens != 4000 ||
		normalized.Now.IsZero() {
		t.Fatalf("bad normalized options: %+v", normalized)
	}
	if capsules, err := BuildContextCapsules(nil, CapsuleBuildOptions{ArchiveDir: dir}); err != nil || capsules != nil {
		t.Fatalf("empty message build should no-op: capsules=%+v err=%v", capsules, err)
	}
	if capsules, err := BuildContextCapsules([]types.Message{textMsg(0, "user", "x")}, CapsuleBuildOptions{}); err != nil || capsules != nil {
		t.Fatalf("empty archive build should no-op: capsules=%+v err=%v", capsules, err)
	}
	original := []ContextCapsule{{Tier: CapsuleTierMicro}, {Tier: CapsuleTierPhase}}
	copyAll := CapsulesByTier(original)
	copyAll[0].Tier = CapsuleTierSession
	if original[0].Tier != CapsuleTierMicro {
		t.Fatal("CapsulesByTier without filters must return a copy")
	}
	if got := CapsulesByTier(original, CapsuleTierPhase); len(got) != 1 || got[0].Tier != CapsuleTierPhase {
		t.Fatalf("phase filter mismatch: %+v", got)
	}
}

func TestContextCapsuleFailOpenAndHelperBranches(t *testing.T) {
	dir := t.TempDir()
	shortTool := []types.Message{{
		Index: 0,
		Role:  "assistant",
		Content: []types.ContentBlock{{
			Type: "tool_result",
			Text: "one two three four five six seven",
		}},
	}}
	capsules, err := BuildContextCapsules(shortTool, CapsuleBuildOptions{
		SessionID:                "sess-short",
		ArchiveDir:               dir,
		ActiveTailMessages:       1,
		MicroToolResultMinTokens: 3,
		PhaseMinMessages:         10,
		Now:                      time.Unix(3, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(capsules) != 0 {
		t.Fatalf("ineligible short archive should skip capsule: %+v", capsules)
	}

	badArchive := tempFilePath(t)
	longTool := []types.Message{{
		Index: 0,
		Role:  "assistant",
		Content: []types.ContentBlock{{
			Type: "tool_result",
			Text: strings.Repeat("large output ", 80),
		}},
	}}
	if _, err := BuildContextCapsules(longTool, CapsuleBuildOptions{
		SessionID:                "sess-bad",
		ArchiveDir:               badArchive,
		ActiveTailMessages:       1,
		MicroToolResultMinTokens: 10,
		PhaseMinMessages:         10,
		Now:                      time.Unix(4, 0),
	}); err == nil {
		t.Fatal("bad archive path should return archive error")
	}

	if got := phaseRanges(makeTestMessages(2), 5, 1); got != nil {
		t.Fatalf("active tail beyond length should yield no ranges: %+v", got)
	}
	ranges := phaseRanges([]types.Message{textMsg(0, "assistant", "a"), textMsg(1, "assistant", "b"), textMsg(2, "assistant", "c")}, 1, 2)
	if len(ranges) != 1 || ranges[0] != [2]int{0, 1} {
		t.Fatalf("final phase range mismatch: %+v", ranges)
	}
	if isPhaseBoundary(textMsg(0, "assistant", "task")) ||
		isPhaseBoundary(textMsg(0, "user", "   ")) ||
		!isPhaseBoundary(textMsg(0, "user", "› continue")) {
		t.Fatal("phase boundary helper mismatch")
	}
	anchors := anchorsInRange(map[int]bool{1: true, 4: true}, [2]int{0, 2})
	if len(anchors) != 1 || anchors[0] != 1 {
		t.Fatalf("anchors in range mismatch: %+v", anchors)
	}
	phaseSummary := summarizePhase([]types.Message{{
		Index: 0,
		Role:  "assistant",
		Content: []types.ContentBlock{{
			Type:     "tool_use",
			ToolName: "bash",
		}},
	}}, [2]int{0, 0})
	if !strings.Contains(phaseSummary, "tools: bash") {
		t.Fatalf("phase summary missing tools: %q", phaseSummary)
	}
	rendered := renderCapsuleMessages([]types.Message{{
		Index: 7,
		Role:  "assistant",
		Content: []types.ContentBlock{
			{Type: "text", Text: "plain"},
			{Type: "tool_use", ToolName: "bash", ToolInput: `{"command":"pwd"}`},
			{Type: "tool_result", ToolResultID: "r1", Text: "out"},
		},
	}})
	if !strings.Contains(rendered, "<tool_use") || !strings.Contains(rendered, "<tool_result") {
		t.Fatalf("rendered capsule messages missing tool blocks: %s", rendered)
	}
	if trimForCapsule("short", 10) != "short" || !strings.HasSuffix(trimForCapsule(strings.Repeat("x", 20), 10), "...") {
		t.Fatal("trim helper mismatch")
	}
	if keys := sortedKeys(nil); len(keys) != 0 {
		t.Fatalf("nil sorted keys mismatch: %+v", keys)
	}
}

func TestContextCapsuleInternalErrorAndSkipBranches(t *testing.T) {
	dir := t.TempDir()
	badArchive := tempFilePath(t)

	if capsules, err := buildMicroCapsules([]types.Message{{
		Index: 0,
		Role:  "assistant",
		Content: []types.ContentBlock{{
			Type: "tool_result",
			Text: "tiny",
		}},
	}}, CapsuleBuildOptions{ArchiveDir: dir, MicroToolResultMinTokens: 100, Now: time.Unix(5, 0)}, nil); err != nil || len(capsules) != 0 {
		t.Fatalf("below-threshold micro should skip, capsules=%+v err=%v", capsules, err)
	}

	phaseMessages := []types.Message{
		textMsg(0, "user", strings.Repeat("phase a ", 20)),
		textMsg(1, "assistant", strings.Repeat("phase b ", 20)),
		textMsg(2, "assistant", strings.Repeat("phase c ", 20)),
	}
	if capsules, err := buildPhaseCapsules(phaseMessages, CapsuleBuildOptions{ArchiveDir: dir, ActiveTailMessages: 0, PhaseMinMessages: 1, Now: time.Unix(6, 0)}, map[int]bool{1: true}); err != nil || len(capsules) != 0 {
		t.Fatalf("anchor-bearing phase should skip, capsules=%+v err=%v", capsules, err)
	}
	if _, err := buildPhaseCapsules(phaseMessages, CapsuleBuildOptions{ArchiveDir: badArchive, ActiveTailMessages: 0, PhaseMinMessages: 1, Now: time.Unix(7, 0)}, nil); err == nil {
		t.Fatal("bad archive path should fail phase capsule build")
	}
	if capsules, err := buildPhaseCapsules([]types.Message{textMsg(0, "user", "")}, CapsuleBuildOptions{ArchiveDir: dir, ActiveTailMessages: 0, PhaseMinMessages: 1, Now: time.Unix(8, 0)}, nil); err != nil || len(capsules) != 0 {
		t.Fatalf("ineligible phase archive should skip, capsules=%+v err=%v", capsules, err)
	}

	phases := []ContextCapsule{
		{SourceRange: [2]int{0, 0}, Summary: "p0", ArchiveURIs: []string{"local-archive://p0"}},
		{SourceRange: [2]int{1, 1}, Summary: "p1", ArchiveURIs: []string{"local-archive://p1"}},
	}
	if capsule, err := buildSessionCapsule(phaseMessages, CapsuleBuildOptions{ArchiveDir: dir, SessionMinPhaseCapsules: 2, SessionMinOriginalTokens: 1, Now: time.Unix(9, 0)}, phases, map[int]bool{1: true}); err != nil || capsule != nil {
		t.Fatalf("anchor-bearing session should skip, capsule=%+v err=%v", capsule, err)
	}
	if capsule, err := buildSessionCapsule(phaseMessages, CapsuleBuildOptions{ArchiveDir: dir, SessionMinPhaseCapsules: 2, SessionMinOriginalTokens: 100000, Now: time.Unix(10, 0)}, phases, nil); err != nil || capsule != nil {
		t.Fatalf("below-token session should skip, capsule=%+v err=%v", capsule, err)
	}
	if _, err := buildSessionCapsule(phaseMessages, CapsuleBuildOptions{ArchiveDir: badArchive, SessionMinPhaseCapsules: 2, SessionMinOriginalTokens: 1, Now: time.Unix(11, 0)}, phases, nil); err == nil {
		t.Fatal("bad archive path should fail session capsule build")
	}
	tinyPhases := []ContextCapsule{
		{SourceRange: [2]int{0, 0}, Summary: "p0"},
		{SourceRange: [2]int{0, 0}, Summary: "p1"},
	}
	if capsule, err := buildSessionCapsule([]types.Message{textMsg(0, "user", "")}, CapsuleBuildOptions{ArchiveDir: dir, SessionMinPhaseCapsules: 2, SessionMinOriginalTokens: 1, Now: time.Unix(12, 0)}, tinyPhases, nil); err != nil || capsule != nil {
		t.Fatalf("ineligible session archive should skip, capsule=%+v err=%v", capsule, err)
	}
}

func TestBuildContextCapsulesArchiveErrorPropagation(t *testing.T) {
	dir := t.TempDir()
	messages := []types.Message{
		textMsg(0, "user", "task alpha"),
		textMsg(1, "assistant", strings.Repeat("phase a ", 20)),
		textMsg(2, "assistant", strings.Repeat("phase b ", 20)),
		textMsg(3, "user", "next beta"),
		textMsg(4, "assistant", strings.Repeat("phase c ", 20)),
		textMsg(5, "assistant", strings.Repeat("phase d ", 20)),
		textMsg(6, "user", "live tail"),
	}
	origPut := capsuleArchivePut
	defer func() { capsuleArchivePut = origPut }()

	capsuleArchivePut = func(_ string, input contentarchive.Input, _ contentarchive.Limits) (*contentarchive.Entry, error) {
		if input.SubLayer == "capsule_phase" {
			return nil, errors.New("phase archive")
		}
		return origPut(dir, input, contentarchive.Limits{})
	}
	if _, err := BuildContextCapsules(messages, CapsuleBuildOptions{
		SessionID:                "sess-phase-error",
		ArchiveDir:               dir,
		ActiveTailMessages:       1,
		MicroToolResultMinTokens: 500,
		PhaseMinMessages:         2,
		SessionMinPhaseCapsules:  2,
		SessionMinOriginalTokens: 1,
		Now:                      time.Unix(13, 0),
	}); err == nil {
		t.Fatal("expected phase archive error")
	}

	capsuleArchivePut = func(_ string, input contentarchive.Input, _ contentarchive.Limits) (*contentarchive.Entry, error) {
		if input.SubLayer == "capsule_session" {
			return nil, errors.New("session archive")
		}
		return origPut(dir, input, contentarchive.Limits{})
	}
	if _, err := BuildContextCapsules(messages, CapsuleBuildOptions{
		SessionID:                "sess-session-error",
		ArchiveDir:               dir,
		ActiveTailMessages:       1,
		MicroToolResultMinTokens: 500,
		PhaseMinMessages:         2,
		SessionMinPhaseCapsules:  2,
		SessionMinOriginalTokens: 1,
		Now:                      time.Unix(14, 0),
	}); err == nil {
		t.Fatal("expected session archive error")
	}
}

func tempFilePath(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/archive-file"
	if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func textMsg(index int, role string, text string) types.Message {
	return types.Message{
		Index: index,
		Role:  role,
		Content: []types.ContentBlock{{
			Type: "text",
			Text: text,
		}},
	}
}

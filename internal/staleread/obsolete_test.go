package staleread

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func writeUse(id, path, tool string) types.ContentBlock {
	return types.ContentBlock{
		Type:      "tool_use",
		ToolUseID: id,
		ToolName:  tool,
		ToolInput: `{"path":"` + path + `"}`,
	}
}

func TestPruneObsoleteEmpty(t *testing.T) {
	out, stats := PruneObsoleteReads(nil, ObsoleteOptions{})
	if out != nil {
		t.Errorf("nil input should return nil")
	}
	if stats.BlocksReplaced != 0 {
		t.Errorf("stats=%+v", stats)
	}
}

func TestPruneReadThenMutate(t *testing.T) {
	bigBody := strings.Repeat("file content. ", 60)
	msgs := []types.Message{
		// turn 0: Read use
		{Content: []types.ContentBlock{readUse("r1", "src/x.go")}},
		// turn 1: tool_result with big content
		{Content: []types.ContentBlock{readResult("r1", bigBody)}},
		// turn 2: text
		{Content: []types.ContentBlock{{Type: "text", Text: "think"}}},
		// turn 3: apply_patch mutates the file
		{Content: []types.ContentBlock{writeUse("p1", "src/x.go", "apply_patch")}},
	}
	out, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 1 {
		t.Fatalf("expected 1 read pruned, got %d", stats.BlocksReplaced)
	}
	if stats.PathsPruned != 1 {
		t.Errorf("paths=%d want 1", stats.PathsPruned)
	}
	if stats.BytesReplaced <= 0 {
		t.Errorf("bytes_replaced=%d", stats.BytesReplaced)
	}
	if out[1].Content[0].Text == bigBody {
		t.Errorf("obsolete read not pruned")
	}
	if !strings.Contains(out[1].Content[0].Text, "kind=obsolete-read") {
		t.Errorf("marker missing: %q", out[1].Content[0].Text)
	}
	if !strings.Contains(out[1].Content[0].Text, "src/x.go") {
		t.Errorf("path missing in marker: %q", out[1].Content[0].Text)
	}
	// Original slice unchanged.
	if msgs[1].Content[0].Text != bigBody {
		t.Errorf("input mutated")
	}
}

func TestPruneReadAfterMutateNotPruned(t *testing.T) {
	// Mutation at turn 0, read at turn 2 - read sees post-mutation
	// state and must NOT be pruned.
	msgs := []types.Message{
		{Content: []types.ContentBlock{writeUse("p1", "src/x.go", "Write")}},
		{Content: []types.ContentBlock{readUse("r1", "src/x.go")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("after ", 100))}},
	}
	out, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("read after mutation should not be pruned, got %d", stats.BlocksReplaced)
	}
	if !strings.HasPrefix(out[2].Content[0].Text, "after ") {
		t.Errorf("read mutated: %q", out[2].Content[0].Text)
	}
}

func TestPruneOnlyEarliestMutationCounts(t *testing.T) {
	// Earliest mutation determines pruning. Reads BEFORE the
	// earliest mutation are obsolete; reads BETWEEN mutations are
	// also obsolete (the later mutation post-dates them).
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("r1", "x.go")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("v1 ", 60))}},
		{Content: []types.ContentBlock{writeUse("p1", "x.go", "Edit")}},
		{Content: []types.ContentBlock{readUse("r2", "x.go")}},
		{Content: []types.ContentBlock{readResult("r2", strings.Repeat("v2 ", 60))}},
		{Content: []types.ContentBlock{writeUse("p2", "x.go", "Edit")}},
	}
	out, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	// r1 is pruned (first mutation at turn 2 > read turn 1).
	// r2 is NOT pruned (first mutation at turn 2 < read turn 4, but
	// we only track the FIRST mutation; a more elaborate version
	// would prune r2 too via turn-5 mutation. v1 simplification:
	// only first mutation is used; pruning r2 needs a follow-up.)
	if stats.BlocksReplaced != 1 {
		t.Errorf("expected 1 prune (r1), got %d", stats.BlocksReplaced)
	}
	if !strings.Contains(out[1].Content[0].Text, "kind=obsolete-read") {
		t.Errorf("r1 not pruned: %q", out[1].Content[0].Text)
	}
}

func TestPruneMultiplePaths(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("ra", "a.go")}},
		{Content: []types.ContentBlock{readResult("ra", strings.Repeat("a ", 50))}},
		{Content: []types.ContentBlock{readUse("rb", "b.go")}},
		{Content: []types.ContentBlock{readResult("rb", strings.Repeat("b ", 50))}},
		// Only a.go gets mutated.
		{Content: []types.ContentBlock{writeUse("pa", "a.go", "apply_patch")}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 1 {
		t.Errorf("expected 1 prune (a.go only), got %d", stats.BlocksReplaced)
	}
	if stats.PathsPruned != 1 {
		t.Errorf("paths=%d want 1", stats.PathsPruned)
	}
}

func TestPruneNoMutationsNoPruning(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("ra", "a.go")}},
		{Content: []types.ContentBlock{readResult("ra", strings.Repeat("body ", 100))}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("no mutations should mean no prunes")
	}
}

func TestPruneNoReadsNoPruning(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{writeUse("p1", "a.go", "Edit")}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("no reads should mean no prunes")
	}
}

func TestPruneCustomMutateToolNames(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("r1", "x.go")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("body ", 100))}},
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolUseID: "p1", ToolName: "my_custom_writer",
			ToolInput: `{"path":"x.go"}`,
		}}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{
		MutateToolNames: []string{"my_custom_writer"},
	})
	if stats.BlocksReplaced != 1 {
		t.Errorf("custom mutator should trigger prune, got %d", stats.BlocksReplaced)
	}
}

func TestPruneShellReadThenShellApplyPatch(t *testing.T) {
	patch := `apply_patch <<'PATCH'
*** Begin Patch
*** Update File: src/x.go
@@
-old
+new
*** End Patch
PATCH`
	msgs := []types.Message{
		{Content: []types.ContentBlock{shellReadUse("r1", "cat src/x.go", "/repo")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("old shell body ", 80))}},
		{Content: []types.ContentBlock{{Type: "text", Text: "think"}}},
		{Content: []types.ContentBlock{shellReadUse("p1", patch, "/repo")}},
	}
	out, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 1 {
		t.Fatalf("shell apply_patch should prune prior shell read, got %+v", stats)
	}
	if !strings.Contains(out[1].Content[0].Text, `/repo/src/x.go`) {
		t.Fatalf("marker missing shell mutation path: %q", out[1].Content[0].Text)
	}
}

func TestPruneShellMutationWithoutPatchPathFullPass(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{shellReadUse("r1", "cat src/x.go", "/repo")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("old shell body ", 80))}},
		{Content: []types.ContentBlock{shellReadUse("p1", "apply_patch <<'PATCH'\nno path here\nPATCH", "/repo")}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Fatalf("shell mutation without explicit patch path must full-pass, got %+v", stats)
	}
}

func TestPruneMutationWithoutPath(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("r1", "x.go")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("b ", 100))}},
		// Mutation with no parseable path.
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolUseID: "p1", ToolName: "Edit",
			ToolInput: `{}`,
		}}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("mutation without path should not prune anything")
	}
}

func TestPruneMutationWithoutToolUseIDStillTracksPath(t *testing.T) {
	// A mutation without ToolUseID still mutates the file; the
	// pruner uses path+turn, not the use_id. Confirm prune fires.
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("r1", "x.go")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("b ", 100))}},
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolName: "Edit",
			ToolInput: `{"path":"x.go"}`,
		}}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 1 {
		t.Errorf("mutation w/o ToolUseID should still prune by path, got %d", stats.BlocksReplaced)
	}
}

func TestPruneReadWithoutMatchingUseUntouched(t *testing.T) {
	msgs := []types.Message{
		{Content: []types.ContentBlock{readResult("orphan", strings.Repeat("body ", 100))}},
		{Content: []types.ContentBlock{writeUse("p1", "x.go", "Edit")}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("orphan tool_result should not be pruned")
	}
}

func TestToolResultRefIDFallbackToToolUseID(t *testing.T) {
	// Some Codex shapes populate ToolUseID directly on the
	// tool_result block instead of ToolResultID.
	bigBody := strings.Repeat("body ", 100)
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("r1", "x.go")}},
		{Content: []types.ContentBlock{{
			Type:      "tool_result",
			ToolUseID: "r1",
			Text:      bigBody,
		}}},
		{Content: []types.ContentBlock{writeUse("p1", "x.go", "apply_patch")}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 1 {
		t.Errorf("tool_result with ToolUseID fallback should be pruned, got %d", stats.BlocksReplaced)
	}
}

func TestPruneMultipleMutationsOnlyFirstTracked(t *testing.T) {
	// Two mutations of the same path; first wins for the
	// pruning-threshold check. Confirms the dedup branch in pass 1.
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("r1", "x.go")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("v1 ", 60))}},
		{Content: []types.ContentBlock{writeUse("p1", "x.go", "Edit")}},
		{Content: []types.ContentBlock{writeUse("p2", "x.go", "Write")}}, // second mutation, must not overwrite firstMutationTurn
	}
	out, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 1 {
		t.Errorf("expected 1 prune, got %d", stats.BlocksReplaced)
	}
	if !strings.Contains(out[1].Content[0].Text, "edited_turn=2") {
		t.Errorf("marker should reference earliest mutation turn 2: %q", out[1].Content[0].Text)
	}
}

func TestPruneOrphanToolResultAmongValidReads(t *testing.T) {
	// idToPath populated; a stray tool_result with no matching
	// tool_use should be skipped in pass 2 without affecting other
	// pruning.
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("r1", "x.go")}},
		{Content: []types.ContentBlock{readResult("r1", strings.Repeat("body ", 100))}},
		// Orphan tool_result with refID unknown to idToPath
		{Content: []types.ContentBlock{readResult("orphan-id-not-in-map", "stray")}},
		{Content: []types.ContentBlock{writeUse("p1", "x.go", "Edit")}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 1 {
		t.Errorf("expected 1 prune (orphan ignored), got %d", stats.BlocksReplaced)
	}
}

func TestAgeAndPruneCompose(t *testing.T) {
	// Realistic combined scenario: read x.go twice with a mutation
	// in between, plus a third read of y.go that's older than its
	// later read. Aging should hit the first y.go read; obsolete-
	// pruning should hit the first x.go read.
	old1 := strings.Repeat("x v1 body. ", 50)
	old2 := strings.Repeat("y old body. ", 50)
	msgs := []types.Message{
		// turn 0: read x.go
		{Content: []types.ContentBlock{readUse("rx1", "x.go")}},
		// turn 1: tool_result (x v1)
		{Content: []types.ContentBlock{readResult("rx1", old1)}},
		// turn 2: read y.go
		{Content: []types.ContentBlock{readUse("ry1", "y.go")}},
		// turn 3: tool_result (y old)
		{Content: []types.ContentBlock{readResult("ry1", old2)}},
		// turn 4: edit x.go
		{Content: []types.ContentBlock{writeUse("p1", "x.go", "apply_patch")}},
		// turn 5: filler
		{Content: []types.ContentBlock{{Type: "text", Text: "f"}}},
		// turn 6: re-read y.go (fresh)
		{Content: []types.ContentBlock{readUse("ry2", "y.go")}},
		// turn 7: tool_result (y fresh)
		{Content: []types.ContentBlock{readResult("ry2", "y fresh")}},
	}

	// Step 1: aging - y.go's first read is superseded by ry2.
	aged, ageStats := AgeMessages(msgs, Options{MinTurnGap: 2})
	if ageStats.BlocksReplaced != 1 {
		t.Fatalf("aging: expected 1 replacement, got %d", ageStats.BlocksReplaced)
	}
	if !strings.Contains(aged[3].Content[0].Text, "kind=stale-read") || !strings.Contains(aged[3].Content[0].Text, "y.go") {
		t.Errorf("y.go old read not aged: %q", aged[3].Content[0].Text)
	}
	// x.go first read should be untouched by aging (no newer read).
	if !strings.HasPrefix(aged[1].Content[0].Text, "x v1") {
		t.Errorf("aging touched x.go first read incorrectly: %q", aged[1].Content[0].Text)
	}

	// Step 2: obsolete-prune - x.go's first read is pre-edit.
	pruned, pruneStats := PruneObsoleteReads(aged, ObsoleteOptions{})
	if pruneStats.BlocksReplaced != 1 {
		t.Fatalf("prune: expected 1 replacement, got %d", pruneStats.BlocksReplaced)
	}
	if !strings.Contains(pruned[1].Content[0].Text, "kind=obsolete-read") || !strings.Contains(pruned[1].Content[0].Text, "x.go") {
		t.Errorf("x.go pre-edit read not pruned: %q", pruned[1].Content[0].Text)
	}
	// y.go aged-out marker should still be present (no mutation of y.go).
	if !strings.Contains(pruned[3].Content[0].Text, "kind=stale-read") || !strings.Contains(pruned[3].Content[0].Text, "y.go") {
		t.Errorf("aging marker lost during pruning: %q", pruned[3].Content[0].Text)
	}
	// Fresh y.go read survives both passes.
	if pruned[7].Content[0].Text != "y fresh" {
		t.Errorf("fresh y.go read corrupted: %q", pruned[7].Content[0].Text)
	}
}

func TestObsoletePreservesCacheControlAndMetadata(t *testing.T) {
	longBody := strings.Repeat("body ", 100)
	preEditRead := types.ContentBlock{
		Type:         "tool_result",
		ToolResultID: "r1",
		Text:         longBody,
		CacheControl: &types.CacheControl{Type: "ephemeral"},
		ArchiveID:    "arch-pre",
	}
	msgs := []types.Message{
		{Content: []types.ContentBlock{readUse("r1", "x.go")}},
		{Content: []types.ContentBlock{preEditRead}},
		{Content: []types.ContentBlock{writeUse("p1", "x.go", "apply_patch")}},
	}
	out, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 1 {
		t.Fatalf("expected 1, got %d", stats.BlocksReplaced)
	}
	pruned := out[1].Content[0]
	if pruned.CacheControl == nil || pruned.CacheControl.Type != "ephemeral" {
		t.Errorf("CacheControl lost: %+v", pruned.CacheControl)
	}
	if pruned.ArchiveID != "arch-pre" {
		t.Errorf("ArchiveID lost: %q", pruned.ArchiveID)
	}
	if pruned.ToolResultID != "r1" {
		t.Errorf("ToolResultID lost: %q", pruned.ToolResultID)
	}
}

func TestPruneToolUseWithoutToolUseIDNoRead(t *testing.T) {
	// Read tool_use without ToolUseID can't be linked - read is not
	// trackable, so no pruning.
	msgs := []types.Message{
		{Content: []types.ContentBlock{{
			Type: "tool_use", ToolName: "Read",
			ToolInput: `{"path":"x.go"}`,
		}}},
		{Content: []types.ContentBlock{writeUse("p1", "x.go", "Edit")}},
	}
	_, stats := PruneObsoleteReads(msgs, ObsoleteOptions{})
	if stats.BlocksReplaced != 0 {
		t.Errorf("untrackable read should not prune anything")
	}
}

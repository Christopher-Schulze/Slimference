package savingspolicy

import "testing"

func TestDecideCodexToolOutputAutoEnablesRecoverableChunkDedup(t *testing.T) {
	t.Parallel()
	got := DecideCodexToolOutput(CodexToolOutputInput{
		Mode:                     "auto",
		ArchiveRecoveryAvailable: true,
		OutputBytes:              9000,
		ChunkMinBytes:            8192,
	})
	if !got.ReadDelta || !got.RepeatedOutput || !got.ChunkDedup || !got.NeedsRecoveryNote || got.Loosened {
		t.Fatalf("auto policy should enable safe reducers plus recoverable chunk dedup: %+v", got)
	}
	if got.Reason != "auto_recoverable_chunk_dedup" {
		t.Fatalf("reason=%q", got.Reason)
	}
}

func TestDecideCodexToolOutputLoosensForContextRisk(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   CodexToolOutputInput
	}{
		{name: "recent edit", in: CodexToolOutputInput{Mode: "auto", ArchiveRecoveryAvailable: true, RecentlyEdited: true, OutputBytes: 9000, ChunkMinBytes: 1}},
		{name: "post-collapse reread", in: CodexToolOutputInput{Mode: "auto", ArchiveRecoveryAvailable: true, PostCollapseReRead: true, OutputBytes: 9000, ChunkMinBytes: 1}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DecideCodexToolOutput(tc.in)
			if !got.Loosened || got.ChunkDedup || !got.ReadDelta || !got.RepeatedOutput {
				t.Fatalf("context-risk signal should loosen only aggressive reducers: %+v", got)
			}
		})
	}
}

func TestDecideCodexToolOutputModes(t *testing.T) {
	t.Parallel()
	off := DecideCodexToolOutput(CodexToolOutputInput{Mode: "off", ArchiveRecoveryAvailable: true, OutputBytes: 9000})
	if off.ReadDelta || off.RepeatedOutput || off.ChunkDedup {
		t.Fatalf("off mode must disable policy-managed reducers: %+v", off)
	}
	conservative := DecideCodexToolOutput(CodexToolOutputInput{
		Mode:                     "conservative",
		ArchiveRecoveryAvailable: true,
		OutputBytes:              9000,
		ChunkMinBytes:            1,
	})
	if !conservative.ReadDelta || !conservative.RepeatedOutput || conservative.ChunkDedup {
		t.Fatalf("conservative mode should keep lossless reducers and skip auto chunk dedup: %+v", conservative)
	}
	forced := DecideCodexToolOutput(CodexToolOutputInput{
		Mode:                     "conservative",
		ArchiveRecoveryAvailable: true,
		ExplicitChunkDedup:       true,
		OutputBytes:              9000,
		ChunkMinBytes:            1,
	})
	if !forced.ChunkDedup || !forced.NeedsRecoveryNote {
		t.Fatalf("explicit chunk toggle should still work in conservative mode: %+v", forced)
	}
}

func TestValidCodexMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"", "off", "conservative", "safe", "auto", "max", "aggressive"} {
		if !ValidCodexMode(mode) {
			t.Fatalf("mode %q should be valid", mode)
		}
	}
	if ValidCodexMode("reckless") {
		t.Fatal("unknown mode must be invalid")
	}
}

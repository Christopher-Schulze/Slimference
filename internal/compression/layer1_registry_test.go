package compression

import "testing"

func TestLayer1SubLayerRegistryContracts(t *testing.T) {
	t.Parallel()
	registry := Layer1SubLayerRegistry()
	if len(registry) == 0 {
		t.Fatal("registry is empty")
	}

	seen := make(map[string]bool, len(registry))
	for _, info := range registry {
		if info.ID == "" {
			t.Fatalf("empty id: %+v", info)
		}
		if seen[info.ID] {
			t.Fatalf("duplicate id %q", info.ID)
		}
		seen[info.ID] = true
		if info.Tier == "" {
			t.Fatalf("%s has empty tier", info.ID)
		}
		if info.ModelRisk == "" {
			t.Fatalf("%s has empty model risk", info.ID)
		}
		if info.Recovery == "" {
			t.Fatalf("%s has empty recovery", info.ID)
		}
		if info.RequiresArchive && info.Tier != Layer1SafetyRecoverableWithArchive && info.Tier != Layer1SafetyTaskPreservingSummary {
			t.Fatalf("%s requires archive with incompatible tier %q", info.ID, info.Tier)
		}
	}

	for _, id := range []string{
		"ansi_strip",
		"json_compact",
		"semantic_dictionary",
		"dedup",
		"dedup_near",
		"delta",
		"comment_strip",
		"structure_extract",
		"tool_compressor",
		"success_short_circuit",
		"image_replace",
		"repeated_collapse",
		"graph_pruning",
		"preview_pass",
		"tool_output_in_window",
		"loop_nudge",
	} {
		if !seen[id] {
			t.Fatalf("registry missing %s", id)
		}
	}
}

func TestLayer1SubLayerRegistryReturnsCopy(t *testing.T) {
	t.Parallel()
	first := Layer1SubLayerRegistry()
	if len(first) == 0 {
		t.Fatal("registry is empty")
	}
	first[0].ID = "mutated"

	second := Layer1SubLayerRegistry()
	if second[0].ID == "mutated" {
		t.Fatal("registry mutated through returned slice")
	}
}

func TestLayer1SubLayerByID(t *testing.T) {
	t.Parallel()
	info, ok := layer1SubLayerByID("structure_extract")
	if !ok {
		t.Fatal("structure_extract not found")
	}
	if info.Tier != Layer1SafetyRecoverableWithArchive || !info.RequiresArchive {
		t.Fatalf("structure_extract safety mismatch: %+v", info)
	}
	if _, ok := layer1SubLayerByID("missing"); ok {
		t.Fatal("missing sub-layer must not resolve")
	}
}

func TestLayer1MutationRequiresArchiveFailsClosedForUnknownSubLayer(t *testing.T) {
	t.Parallel()
	if !layer1MutationRequiresArchive(nil) {
		t.Fatal("unattributed non-ANSI mutation must require archive until classified")
	}
	if !layer1MutationRequiresArchive([]string{"new_lossy_candidate"}) {
		t.Fatal("unknown sub-layer must require archive until classified")
	}
	if layer1MutationRequiresArchive([]string{"json_compact", "ansi_strip"}) {
		t.Fatal("known exact sub-layers should not require archive")
	}
	if !layer1MutationRequiresArchive([]string{"json_compact", "structure_extract"}) {
		t.Fatal("archive-required sub-layer in chain must require archive")
	}
	if !layer1MutationRequiresArchive([]string{"dedup_near"}) {
		t.Fatal("near-dedup must require archive because similar text is not identical")
	}
}

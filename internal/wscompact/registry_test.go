package wscompact

import "testing"

func TestShapeRegistry_ObserveKnownAndCount(t *testing.T) {
	t.Parallel()
	registry := NewShapeRegistry()
	if registry.Known() || registry.Count() != 0 {
		t.Fatalf("fresh registry known=%t count=%d", registry.Known(), registry.Count())
	}
	registry.Observe(FrameSummary{
		Route:        "/backend-api/dev",
		Direction:    DirectionClientToServer,
		JSON:         true,
		JSONTopLevel: "object",
		JSONKeys:     []string{"type"},
		JSONTypes:    []string{"type:string"},
		MessageType:  "hello",
		Opcode:       "text",
	})
	if registry.Known() || registry.MutationCapable() || registry.Count() != 1 {
		t.Fatalf("registry known=%t count=%d", registry.Known(), registry.Count())
	}
	registry.Observe(FrameSummary{
		Route:        "/backend-api/dev",
		Direction:    DirectionClientToServer,
		JSON:         true,
		JSONTopLevel: "object",
		JSONKeys:     []string{"type"},
		JSONTypes:    []string{"type:string"},
		MessageType:  "hello",
		Opcode:       "text",
	})
	if registry.Count() != 1 {
		t.Fatalf("duplicate shape count=%d, want 1", registry.Count())
	}
	entries := registry.Entries()
	if len(entries) != 1 || entries[0].Count != 2 || entries[0].MutationEligibility != "inspect_only" || entries[0].Fallback != "byte_equal_bridge" || entries[0].Hash == "" {
		t.Fatalf("inspect-only entries=%+v", entries)
	}
	registry.Observe(FrameSummary{
		Route:        "/backend-api/codex/responses",
		Direction:    DirectionClientToServer,
		JSON:         true,
		JSONTopLevel: "object",
		JSONKeys:     []string{"request", "type"},
		JSONTypes:    []string{"request:object", "type:string"},
		MessageType:  "request",
		Opcode:       "text",
	})
	if !registry.Known() || !registry.MutationCapable() || registry.Count() != 2 {
		t.Fatalf("registered request shape known=%t count=%d", registry.Known(), registry.Count())
	}
	registry.Observe(FrameSummary{JSON: true, InspectNote: "reserved_bits_or_compressed_extension"})
	if registry.Count() != 2 {
		t.Fatalf("blocked shape count=%d, want 2", registry.Count())
	}
}

func TestShapeRegistry_NilAndEmptyShape(t *testing.T) {
	t.Parallel()
	var registry *ShapeRegistry
	registry.Observe(FrameSummary{JSON: true, JSONTopLevel: "object"})
	if registry.Known() || registry.Count() != 0 {
		t.Fatal("nil registry must stay empty")
	}
	registry = &ShapeRegistry{}
	registry.Observe(FrameSummary{JSON: true})
	if registry.Known() || registry.Count() != 0 {
		t.Fatalf("empty shape should not register: known=%t count=%d", registry.Known(), registry.Count())
	}
	registry.Observe(FrameSummary{JSON: true, JSONTopLevel: "object", Direction: DirectionServerToClient})
	if registry.Known() || registry.Count() != 1 {
		t.Fatalf("zero-value registry known=%t count=%d", registry.Known(), registry.Count())
	}
	if key := shapeRegistryKey(FrameSummary{}); key != "" {
		t.Fatalf("empty shape key = %q", key)
	}
}

func TestContentFreeShapeHash_StableAndValueFree(t *testing.T) {
	t.Parallel()
	first := FrameSummary{
		Route:        "/backend-api/codex/responses",
		Direction:    DirectionClientToServer,
		Opcode:       "text",
		JSONTopLevel: "object",
		JSONKeys:     []string{"request", "type"},
		JSONTypes:    []string{"request:object", "type:string"},
		MessageType:  "request",
	}
	second := first
	if got, want := ContentFreeShapeHash(first), ContentFreeShapeHash(second); got == "" || got != want {
		t.Fatalf("hash got=%q want=%q", got, want)
	}
	changed := first
	changed.JSONTypes = []string{"request:string", "type:string"}
	if ContentFreeShapeHash(first) == ContentFreeShapeHash(changed) {
		t.Fatal("shape hash must change when content-free field types change")
	}
}

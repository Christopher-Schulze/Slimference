package wscompact

import "testing"

func TestShapeRegistry_ObserveKnownAndCount(t *testing.T) {
	t.Parallel()
	registry := NewShapeRegistry()
	if registry.Known() || registry.Count() != 0 {
		t.Fatalf("fresh registry known=%t count=%d", registry.Known(), registry.Count())
	}
	registry.Observe(FrameSummary{
		Direction:    DirectionClientToServer,
		JSON:         true,
		JSONTopLevel: "object",
		JSONKeys:     []string{"type"},
		MessageType:  "hello",
	})
	if !registry.Known() || registry.Count() != 1 {
		t.Fatalf("registry known=%t count=%d", registry.Known(), registry.Count())
	}
	registry.Observe(FrameSummary{
		Direction:    DirectionClientToServer,
		JSON:         true,
		JSONTopLevel: "object",
		JSONKeys:     []string{"type"},
		MessageType:  "hello",
	})
	if registry.Count() != 1 {
		t.Fatalf("duplicate shape count=%d, want 1", registry.Count())
	}
	registry.Observe(FrameSummary{JSON: true, InspectNote: "reserved_bits_or_compressed_extension"})
	if registry.Count() != 1 {
		t.Fatalf("blocked shape count=%d, want 1", registry.Count())
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
	if !registry.Known() || registry.Count() != 1 {
		t.Fatalf("zero-value registry known=%t count=%d", registry.Known(), registry.Count())
	}
	if key := shapeRegistryKey(FrameSummary{}); key != "" {
		t.Fatalf("empty shape key = %q", key)
	}
}

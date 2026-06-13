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
	if got := ContentFreeShapeHash(FrameSummary{}); got != "" {
		t.Fatalf("empty shape hash = %q", got)
	}
}

func TestShapeRegistry_EntriesSortedAndCloned(t *testing.T) {
	t.Parallel()
	registry := NewShapeRegistry()
	for _, summary := range []FrameSummary{
		{
			ShapeHash:    "c",
			Route:        "/backend-api/dev",
			Direction:    DirectionClientToServer,
			Opcode:       "text",
			JSON:         true,
			JSONTopLevel: "object",
			JSONKeys:     []string{"c"},
			JSONTypes:    []string{"c:string"},
			MessageType:  "c",
		},
		{
			ShapeHash:    "a",
			Route:        "/backend-api/dev",
			Direction:    DirectionClientToServer,
			Opcode:       "text",
			JSON:         true,
			JSONTopLevel: "object",
			JSONKeys:     []string{"a"},
			JSONTypes:    []string{"a:string"},
			MessageType:  "a",
		},
		{
			ShapeHash:    "b",
			Route:        "/backend-api/dev",
			Direction:    DirectionClientToServer,
			Opcode:       "text",
			JSON:         true,
			JSONTopLevel: "object",
			JSONKeys:     []string{"b"},
			JSONTypes:    []string{"b:string"},
			MessageType:  "b",
		},
	} {
		registry.Observe(summary)
	}

	entries := registry.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries=%+v", entries)
	}
	if got := entries[0].Hash + entries[1].Hash + entries[2].Hash; got != "abc" {
		t.Fatalf("entry order=%s", got)
	}
	entries[0].JSONKeys[0] = "mutated"
	entries[0].JSONTypes[0] = "mutated:string"
	again := registry.Entries()
	if again[0].JSONKeys[0] != "a" || again[0].JSONTypes[0] != "a:string" {
		t.Fatalf("entries must be defensive clones: %+v", again[0])
	}
}

func TestShapeRegistry_CodexMutationEligibility(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		summary FrameSummary
		want    string
	}{
		{
			name: "client request route with query and trailing slash",
			summary: FrameSummary{
				Route:        "/backend-api/codex-bridge/responses/?v=1",
				Direction:    DirectionClientToServer,
				Opcode:       "text",
				JSONTopLevel: "object",
				JSONKeys:     []string{"request", "type"},
				MessageType:  "request",
			},
			want: "phasef_request",
		},
		{
			name: "client request without request key",
			summary: FrameSummary{
				Route:        "/backend-api/codex/responses",
				Direction:    DirectionClientToServer,
				Opcode:       "text",
				JSONTopLevel: "object",
				JSONKeys:     []string{"type"},
				MessageType:  "request",
			},
			want: "inspect_only",
		},
		{
			name: "server delta response",
			summary: FrameSummary{
				Route:        "/backend-api/codex/responses",
				Direction:    DirectionServerToClient,
				Opcode:       "text",
				JSONTopLevel: "object",
				MessageType:  "response.output_text.delta",
			},
			want: "phasef_observe_or_stream_delta",
		},
		{
			name: "server failed response",
			summary: FrameSummary{
				Route:        "/backend-api/codex/responses",
				Direction:    DirectionServerToClient,
				Opcode:       "text",
				JSONTopLevel: "object",
				MessageType:  "response.failed",
			},
			want: "phasef_observe_or_stream_delta",
		},
		{
			name: "server unknown response",
			summary: FrameSummary{
				Route:        "/backend-api/codex/responses",
				Direction:    DirectionServerToClient,
				Opcode:       "text",
				JSONTopLevel: "object",
				MessageType:  "response.unknown",
			},
			want: "inspect_only",
		},
		{
			name: "non codex route",
			summary: FrameSummary{
				Route:        "/backend-api/dev",
				Direction:    DirectionServerToClient,
				Opcode:       "text",
				JSONTopLevel: "object",
				MessageType:  "response.failed",
			},
			want: "inspect_only",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mutationEligibility(tc.summary); got != tc.want {
				t.Fatalf("mutationEligibility()=%q want %q", got, tc.want)
			}
			if got := fallbackBehavior(tc.summary); tc.want == "inspect_only" && got != "byte_equal_bridge" {
				t.Fatalf("inspect-only fallback=%q", got)
			} else if tc.want != "inspect_only" && got != "mutate_only_after_phasef_and_live_cert" {
				t.Fatalf("mutation-capable fallback=%q", got)
			}
		})
	}
}

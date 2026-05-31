package wscompact

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
)

type ShapeRegistry struct {
	mu     sync.RWMutex
	shapes map[string]FrameShape
}

type FrameShape struct {
	Hash                string    `json:"hash"`
	Route               string    `json:"route,omitempty"`
	Direction           Direction `json:"direction"`
	Opcode              string    `json:"opcode"`
	JSONTopLevel        string    `json:"json_top_level"`
	JSONKeys            []string  `json:"json_keys,omitempty"`
	JSONTypes           []string  `json:"json_types,omitempty"`
	MessageType         string    `json:"message_type,omitempty"`
	Count               int       `json:"count"`
	MutationEligibility string    `json:"mutation_eligibility"`
	Fallback            string    `json:"fallback"`
}

func NewShapeRegistry() *ShapeRegistry {
	return &ShapeRegistry{shapes: make(map[string]FrameShape)}
}

func (r *ShapeRegistry) Observe(summary FrameSummary) {
	if r == nil || !summary.JSON || summary.InspectNote != "" {
		return
	}
	key := shapeRegistryKey(summary)
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shapes == nil {
		r.shapes = make(map[string]FrameShape)
	}
	shape := r.shapes[key]
	if shape.Count == 0 {
		shape = frameShapeFromSummary(key, summary)
	}
	shape.Count++
	r.shapes[key] = shape
}

func (r *ShapeRegistry) Known() bool {
	return r.MutationCapable()
}

func (r *ShapeRegistry) MutationCapable() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, shape := range r.shapes {
		if shape.MutationEligibility != "inspect_only" {
			return true
		}
	}
	return false
}

func (r *ShapeRegistry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.shapes)
}

func (r *ShapeRegistry) Entries() []FrameShape {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]FrameShape, 0, len(r.shapes))
	for _, shape := range r.shapes {
		out = append(out, cloneFrameShape(shape))
	}
	sortFrameShapes(out)
	return out
}

func shapeRegistryKey(summary FrameSummary) string {
	if summary.JSONTopLevel == "" {
		return ""
	}
	parts := []string{
		normalizeShapeRoute(summary.Route),
		string(summary.Direction),
		summary.Opcode,
		summary.JSONTopLevel,
		summary.MessageType,
		strings.Join(summary.JSONKeys, ","),
		strings.Join(summary.JSONTypes, ","),
	}
	return strings.Join(parts, "|")
}

func frameShapeFromSummary(key string, summary FrameSummary) FrameShape {
	hash := summary.ShapeHash
	if hash == "" {
		hash = contentFreeShapeHashKey(key)
	}
	return FrameShape{
		Hash:                hash,
		Route:               normalizeShapeRoute(summary.Route),
		Direction:           summary.Direction,
		Opcode:              summary.Opcode,
		JSONTopLevel:        summary.JSONTopLevel,
		JSONKeys:            append([]string(nil), summary.JSONKeys...),
		JSONTypes:           append([]string(nil), summary.JSONTypes...),
		MessageType:         summary.MessageType,
		MutationEligibility: mutationEligibility(summary),
		Fallback:            fallbackBehavior(summary),
	}
}

func mutationEligibility(summary FrameSummary) string {
	if !isCodexResponsesShapeRoute(summary.Route) {
		return "inspect_only"
	}
	if summary.Direction == DirectionClientToServer && summary.Opcode == "text" && summary.JSONTopLevel == "object" && summary.MessageType == "request" && hasJSONKey(summary, "request") {
		return "phasef_request"
	}
	if summary.Direction == DirectionServerToClient && summary.Opcode == "text" && summary.JSONTopLevel == "object" {
		switch summary.MessageType {
		case "response.output_item.added", "response.output_item.done", "response.output_text.delta", "response.completed", "response.incomplete", "response.failed":
			return "phasef_observe_or_stream_delta"
		}
	}
	return "inspect_only"
}

func fallbackBehavior(summary FrameSummary) string {
	if mutationEligibility(summary) == "inspect_only" {
		return "byte_equal_bridge"
	}
	return "mutate_only_after_phasef_and_live_cert"
}

func hasJSONKey(summary FrameSummary, want string) bool {
	for _, key := range summary.JSONKeys {
		if key == want {
			return true
		}
	}
	return false
}

func isCodexResponsesShapeRoute(route string) bool {
	route = normalizeShapeRoute(route)
	return route == "/backend-api/codex/responses" || route == "/backend-api/codex-bridge/responses"
}

func normalizeShapeRoute(route string) string {
	if idx := strings.Index(route, "?"); idx >= 0 {
		route = route[:idx]
	}
	return strings.TrimSuffix(route, "/")
}

func ContentFreeShapeHash(summary FrameSummary) string {
	return contentFreeShapeHashKey(shapeRegistryKey(summary))
}

func contentFreeShapeHashKey(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

func cloneFrameShape(shape FrameShape) FrameShape {
	shape.JSONKeys = append([]string(nil), shape.JSONKeys...)
	shape.JSONTypes = append([]string(nil), shape.JSONTypes...)
	return shape
}

func sortFrameShapes(shapes []FrameShape) {
	for i := 1; i < len(shapes); i++ {
		for j := i; j > 0 && shapes[j-1].Hash > shapes[j].Hash; j-- {
			shapes[j-1], shapes[j] = shapes[j], shapes[j-1]
		}
	}
}

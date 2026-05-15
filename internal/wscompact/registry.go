package wscompact

import (
	"strings"
	"sync"
)

type ShapeRegistry struct {
	mu     sync.RWMutex
	shapes map[string]int
}

func NewShapeRegistry() *ShapeRegistry {
	return &ShapeRegistry{shapes: make(map[string]int)}
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
		r.shapes = make(map[string]int)
	}
	r.shapes[key]++
}

func (r *ShapeRegistry) Known() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.shapes) > 0
}

func (r *ShapeRegistry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.shapes)
}

func shapeRegistryKey(summary FrameSummary) string {
	if summary.JSONTopLevel == "" {
		return ""
	}
	parts := []string{
		string(summary.Direction),
		summary.JSONTopLevel,
		summary.MessageType,
		strings.Join(summary.JSONKeys, ","),
	}
	return strings.Join(parts, "|")
}

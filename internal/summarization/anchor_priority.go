package summarization

import "github.com/slimference/slimference/internal/types"

type anchorCategory int

const (
	anchorUnknown anchorCategory = iota
	anchorError
	anchorEdit
	anchorDecision
	anchorConfig
	anchorArchitect
)

func (d *AnchorDetector) classifyAnchor(msg types.Message) anchorCategory {
	if d.isAnchorError(msg) {
		return anchorError
	}
	if d.isAnchorEdit(msg) {
		return anchorEdit
	}
	if d.isAnchorDecision(msg) {
		return anchorDecision
	}
	if d.isAnchorConfig(msg) {
		return anchorConfig
	}
	if d.isAnchorArchitect(msg) {
		return anchorArchitect
	}
	return anchorUnknown
}

type prioritizedAnchor struct {
	index    int
	message  types.Message
	category anchorCategory
}

func prioritizeAnchors(detector *AnchorDetector, indices []int, messages []types.Message) []prioritizedAnchor {
	pa := make([]prioritizedAnchor, 0, len(indices))
	for _, idx := range indices {
		if idx >= len(messages) {
			continue
		}
		pa = append(pa, prioritizedAnchor{
			index:    idx,
			message:  messages[idx],
			category: detector.classifyAnchor(messages[idx]),
		})
	}
	return pa
}

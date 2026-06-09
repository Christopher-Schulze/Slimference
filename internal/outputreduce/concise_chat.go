package outputreduce

import "github.com/Christopher-Schulze/Slimference/internal/types"

const ProfileConciseChat Profile = "concise_chat"

func ConciseChatEligibility(provider types.Provider, body []byte, shape TaskShape) (TaskShape, string) {
	if shape == "" {
		shape = DetectTaskShape(provider, body)
	}
	switch shape {
	case ShapeDirectAnswer, ShapeExplanation:
		return shape, ""
	case ShapeExactReply:
		return shape, "exact_reply"
	case ShapeRepairFollowup:
		return shape, "repair_followup_low_roi"
	case ShapeCommandRelay:
		return shape, "command_output_relay_exact_output"
	case ShapeCodeEdit, ShapeNewFile, ShapeReadOnly, ShapeReview, ShapeDebugging, ShapeToolReasoning, ShapePlanning, ShapeFinalSummary:
		return shape, "non_chat_shape_full_pass"
	case ShapeUnknown:
		return shape, "unknown_shape_full_pass"
	default:
		return shape, "non_chat_shape_full_pass"
	}
}

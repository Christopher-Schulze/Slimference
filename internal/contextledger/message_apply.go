package contextledger

import (
	"errors"
	"sort"
	"strconv"

	"github.com/slimference/slimference/internal/types"
)

const ocrlCoveredMarkerPrefix = "[ocrl:v1 covered_by="

const (
	OCRLReasonTargetInvalid         OCRLReason = "target_invalid"
	OCRLReasonTargetArchiveMismatch OCRLReason = "target_archive_mismatch"
)

type OCRLMessageTarget struct {
	MessageIndex int
	BlockIndex   int
	Capsule      Capsule
}

type OCRLMessageApplyPolicy struct {
	OCRLPolicy
	Targets []OCRLMessageTarget
}

type OCRLMessageApplyResult struct {
	Messages       []types.Message `json:"-"`
	OCRL           OCRLResult      `json:"ocrl"`
	AppliedTargets int             `json:"applied_targets"`
	CoveredMarkers int             `json:"covered_markers"`
}

// ApplyOCRLToMessages replaces explicitly targeted old message blocks with one
// deterministic OCRL block. It is intentionally stricter than BuildOCRLReplacement:
// every selected target must point at a live message block whose current text is
// byte-equal to the target capsule's single archive payload. Callers that cannot
// prove that exact mapping get a full-pass result.
func ApplyOCRLToMessages(messages []types.Message, policy OCRLMessageApplyPolicy) OCRLMessageApplyResult {
	result := OCRLMessageApplyResult{Messages: messages}
	if len(policy.Targets) == 0 {
		result.OCRL.Reason = OCRLReasonNoCapsules
		return result
	}
	capsules := make([]Capsule, 0, len(policy.Targets))
	for _, target := range policy.Targets {
		capsules = append(capsules, target.Capsule)
	}
	targetTokens, originalTokens, err := verifyMessageTargets(messages, policy.Targets, policy.ArchiveLoader, policy.CountTokens)
	if err != nil {
		result.OCRL.Reason = OCRLReasonTargetInvalid
		if errors.Is(err, errOCRLTargetArchiveMismatch) {
			result.OCRL.Reason = OCRLReasonTargetArchiveMismatch
		}
		return result
	}
	buildPolicy := policy.OCRLPolicy
	buildPolicy.OriginalTokens = originalTokens
	buildPolicy.UseArchiveOriginalTokens = false
	ocrl := BuildOCRLReplacement(capsules, buildPolicy)
	result.OCRL = ocrl
	if !ocrl.Applied {
		return result
	}
	selected := selectedCapsuleKeys(ocrl.Selection)
	selectedTargets := selectMessageTargets(policy.Targets, selected)
	if len(selectedTargets) == 0 {
		result.OCRL.Applied = false
		result.OCRL.Reason = OCRLReasonNoCapsules
		return result
	}
	selectedTargets = sortMessageTargets(selectedTargets)
	mutated, markers := applyOCRLMessageTargets(messages, selectedTargets, ocrl.Text)
	selectedOriginalTokens := sumMessageTargetTokens(selectedTargets, targetTokens)
	replacementTokens := ocrl.ReplacementTokens + countCoveredMarkers(selectedTargets, markers, policy.CountTokens)
	netSaved := selectedOriginalTokens - replacementTokens - policy.RecoveryOverheadTokens
	minSaved := policy.MinNetSavedTokens
	if minSaved <= 0 {
		minSaved = 1
	}
	if netSaved < minSaved {
		result.OCRL.Applied = false
		result.OCRL.Reason = OCRLReasonNetSavingsTooSmall
		result.OCRL.ReplacementTokens = replacementTokens
		result.OCRL.NetSavedTokens = netSaved
		return result
	}
	result.Messages = mutated
	result.OCRL.ReplacementTokens = replacementTokens
	result.OCRL.NetSavedTokens = netSaved
	result.AppliedTargets = len(selectedTargets)
	result.CoveredMarkers = markers
	return result
}

var errOCRLTargetArchiveMismatch = errors.New("ocrl target archive mismatch")

func verifyMessageTargets(messages []types.Message, targets []OCRLMessageTarget, load ArchiveLoader, count TokenCounter) (map[string]int, int, error) {
	if load == nil || count == nil {
		return nil, 0, errors.New("archive loader and token counter are required")
	}
	seen := make(map[string]struct{}, len(targets))
	tokensByTarget := make(map[string]int, len(targets))
	total := 0
	for _, target := range targets {
		if target.MessageIndex < 0 || target.MessageIndex >= len(messages) {
			return nil, 0, errors.New("message index out of range")
		}
		msg := messages[target.MessageIndex]
		if target.BlockIndex < 0 || target.BlockIndex >= len(msg.Content) {
			return nil, 0, errors.New("block index out of range")
		}
		targetKey := messageTargetKey(target)
		if _, ok := seen[targetKey]; ok {
			return nil, 0, errors.New("duplicate message target")
		}
		seen[targetKey] = struct{}{}
		if len(target.Capsule.Archives) != 1 {
			return nil, 0, errOCRLTargetArchiveMismatch
		}
		body, err := load(target.Capsule.Archives[0])
		if err != nil {
			return nil, 0, errOCRLTargetArchiveMismatch
		}
		text := msg.Content[target.BlockIndex].Text
		if string(body) != text {
			return nil, 0, errOCRLTargetArchiveMismatch
		}
		tokens := count(text)
		if tokens <= 0 {
			return nil, 0, errors.New("target token counter returned non-positive value")
		}
		tokensByTarget[targetKey] = tokens
		total += tokens
	}
	return tokensByTarget, total, nil
}

func sumMessageTargetTokens(targets []OCRLMessageTarget, tokens map[string]int) int {
	total := 0
	for _, target := range targets {
		total += tokens[messageTargetKey(target)]
	}
	return total
}

func messageTargetKey(target OCRLMessageTarget) string {
	return strconv.Itoa(target.MessageIndex) + ":" + strconv.Itoa(target.BlockIndex)
}

func selectedCapsuleKeys(report SelectionReport) map[string]struct{} {
	out := make(map[string]struct{}, report.Capsules)
	for _, decision := range report.Decisions {
		if decision.Action == SelectionCapsule {
			out[renderCapsuleKey(decision.Capsule)] = struct{}{}
		}
	}
	return out
}

func selectMessageTargets(targets []OCRLMessageTarget, selected map[string]struct{}) []OCRLMessageTarget {
	out := make([]OCRLMessageTarget, 0, len(targets))
	for _, target := range targets {
		if _, ok := selected[renderCapsuleKey(target.Capsule)]; ok {
			out = append(out, target)
		}
	}
	return out
}

func sortMessageTargets(targets []OCRLMessageTarget) []OCRLMessageTarget {
	out := append([]OCRLMessageTarget(nil), targets...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].MessageIndex != out[j].MessageIndex {
			return out[i].MessageIndex < out[j].MessageIndex
		}
		return out[i].BlockIndex < out[j].BlockIndex
	})
	return out
}

func applyOCRLMessageTargets(messages []types.Message, targets []OCRLMessageTarget, text string) ([]types.Message, int) {
	out := cloneMessages(messages)
	first := targets[0]
	firstBlock := out[first.MessageIndex].Content[first.BlockIndex]
	firstBlock.Text = text
	firstBlock.ArchiveID = ""
	firstBlock.RawBlock = nil
	out[first.MessageIndex].Content[first.BlockIndex] = firstBlock

	markers := 0
	for i := len(targets) - 1; i >= 1; i-- {
		target := targets[i]
		blocks := out[target.MessageIndex].Content
		if len(blocks) > 1 {
			out[target.MessageIndex].Content = append(blocks[:target.BlockIndex], blocks[target.BlockIndex+1:]...)
			continue
		}
		block := blocks[target.BlockIndex]
		block.Text = ocrlCoveredMarker(first.MessageIndex, first.BlockIndex)
		block.ArchiveID = ""
		block.RawBlock = nil
		out[target.MessageIndex].Content[target.BlockIndex] = block
		markers++
	}
	return out, markers
}

func countCoveredMarkers(targets []OCRLMessageTarget, markers int, count TokenCounter) int {
	if markers == 0 || count == nil || len(targets) == 0 {
		return 0
	}
	first := targets[0]
	return markers * count(ocrlCoveredMarker(first.MessageIndex, first.BlockIndex))
}

func cloneMessages(messages []types.Message) []types.Message {
	out := make([]types.Message, len(messages))
	for i, msg := range messages {
		out[i] = msg
		if len(msg.Content) > 0 {
			out[i].Content = append([]types.ContentBlock(nil), msg.Content...)
		}
	}
	return out
}

func ocrlCoveredMarker(messageIndex, blockIndex int) string {
	return ocrlCoveredMarkerPrefix + "message:" + strconv.Itoa(messageIndex) + ":block:" + strconv.Itoa(blockIndex) + "]"
}

func renderCapsuleKey(capsule Capsule) string {
	return RenderOCRLCapsules([]Capsule{capsule})
}

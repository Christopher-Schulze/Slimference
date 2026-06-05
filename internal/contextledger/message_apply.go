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

type OCRLMessageTargetDerivation struct {
	Targets         []OCRLMessageTarget `json:"-"`
	Matched         int                 `json:"matched"`
	Unmatched       int                 `json:"unmatched"`
	Ambiguous       int                 `json:"ambiguous"`
	MissingArchive  int                 `json:"missing_archive"`
	ArchiveErrors   int                 `json:"archive_errors"`
	DuplicateTarget int                 `json:"duplicate_target"`
}

type ocrlTargetKey struct {
	MessageIndex int
	BlockIndex   int
}

// ApplyOCRLToMessagesByArchiveMatch is the safe full-history convenience path:
// it derives explicit targets only when a capsule's single archive payload is
// byte-equal to exactly one current message block. Ambiguous, missing, or
// archive-error candidates are omitted instead of guessed.
func ApplyOCRLToMessagesByArchiveMatch(messages []types.Message, capsules []Capsule, policy OCRLPolicy) (OCRLMessageApplyResult, OCRLMessageTargetDerivation) {
	derivation := DeriveOCRLMessageTargets(messages, capsules, policy.ArchiveLoader)
	return ApplyOCRLToMessages(messages, OCRLMessageApplyPolicy{
		OCRLPolicy: policy,
		Targets:    derivation.Targets,
	}), derivation
}

func DeriveOCRLMessageTargets(messages []types.Message, capsules []Capsule, load ArchiveLoader) OCRLMessageTargetDerivation {
	derivation := OCRLMessageTargetDerivation{Targets: make([]OCRLMessageTarget, 0, len(capsules))}
	usedTargets := make(map[ocrlTargetKey]struct{}, len(capsules))
	blockIndex := indexOCRLMessageBlocks(messages)
	for _, capsule := range capsules {
		target, reason := deriveOCRLMessageTarget(blockIndex, capsule, load)
		switch reason {
		case "matched":
			key := messageTargetKey(target)
			if _, ok := usedTargets[key]; ok {
				derivation.DuplicateTarget++
				continue
			}
			usedTargets[key] = struct{}{}
			derivation.Targets = append(derivation.Targets, target)
			derivation.Matched++
		case "missing_archive":
			derivation.MissingArchive++
		case "archive_error":
			derivation.ArchiveErrors++
		case "ambiguous":
			derivation.Ambiguous++
		default:
			derivation.Unmatched++
		}
	}
	return derivation
}

type ocrlMessageBlockMatch struct {
	MessageIndex int
	BlockIndex   int
	Count        int
}

func indexOCRLMessageBlocks(messages []types.Message) map[string]ocrlMessageBlockMatch {
	index := make(map[string]ocrlMessageBlockMatch)
	for msgIdx, msg := range messages {
		for blockIdx, block := range msg.Content {
			if block.Text == "" {
				continue
			}
			match := index[block.Text]
			if match.Count == 0 {
				match.MessageIndex = msgIdx
				match.BlockIndex = blockIdx
			}
			match.Count++
			index[block.Text] = match
		}
	}
	return index
}

func deriveOCRLMessageTarget(index map[string]ocrlMessageBlockMatch, capsule Capsule, load ArchiveLoader) (OCRLMessageTarget, string) {
	if load == nil || len(capsule.Archives) != 1 {
		return OCRLMessageTarget{}, "missing_archive"
	}
	id := sortedArchiveIDs(capsule.Archives)
	if len(id) != 1 {
		return OCRLMessageTarget{}, "missing_archive"
	}
	body, err := load(id[0])
	if err != nil || len(body) == 0 {
		return OCRLMessageTarget{}, "archive_error"
	}
	payload := string(body)
	match, ok := index[payload]
	if !ok {
		return OCRLMessageTarget{}, "unmatched"
	}
	if match.Count > 1 {
		return OCRLMessageTarget{}, "ambiguous"
	}
	return OCRLMessageTarget{MessageIndex: match.MessageIndex, BlockIndex: match.BlockIndex, Capsule: capsule}, "matched"
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
	selectedTargets := selectMessageTargetsByDecision(policy.Targets, ocrl.Selection)
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

func verifyMessageTargets(messages []types.Message, targets []OCRLMessageTarget, load ArchiveLoader, count TokenCounter) (map[ocrlTargetKey]int, int, error) {
	if load == nil || count == nil {
		return nil, 0, errors.New("archive loader and token counter are required")
	}
	seen := make(map[ocrlTargetKey]struct{}, len(targets))
	tokensByTarget := make(map[ocrlTargetKey]int, len(targets))
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
		id := sortedArchiveIDs(target.Capsule.Archives)
		if len(id) != 1 {
			return nil, 0, errOCRLTargetArchiveMismatch
		}
		body, err := load(id[0])
		if err != nil {
			return nil, 0, errOCRLTargetArchiveMismatch
		}
		text := msg.Content[target.BlockIndex].Text
		if !bytesEqualString(body, text) {
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

func sumMessageTargetTokens(targets []OCRLMessageTarget, tokens map[ocrlTargetKey]int) int {
	total := 0
	for _, target := range targets {
		total += tokens[messageTargetKey(target)]
	}
	return total
}

func messageTargetKey(target OCRLMessageTarget) ocrlTargetKey {
	return ocrlTargetKey{MessageIndex: target.MessageIndex, BlockIndex: target.BlockIndex}
}

func bytesEqualString(value []byte, text string) bool {
	if len(value) != len(text) {
		return false
	}
	for i, b := range value {
		if b != text[i] {
			return false
		}
	}
	return true
}

func selectMessageTargetsByDecision(targets []OCRLMessageTarget, report SelectionReport) []OCRLMessageTarget {
	if len(report.Decisions) != len(targets) {
		return nil
	}
	out := make([]OCRLMessageTarget, 0, len(targets))
	for i, decision := range report.Decisions {
		if decision.Action == SelectionCapsule {
			out = append(out, targets[i])
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

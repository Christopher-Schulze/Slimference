package summarization

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

var capsuleArchivePut = contentarchive.Put

type CapsuleTier string

const (
	CapsuleTierMicro   CapsuleTier = "micro"
	CapsuleTierPhase   CapsuleTier = "phase"
	CapsuleTierSession CapsuleTier = "session"
)

type CapsuleValidationState string

const (
	CapsuleValid    CapsuleValidationState = "valid"
	CapsuleRejected CapsuleValidationState = "rejected"
)

type ContextCapsule struct {
	ID                     string                 `json:"id"`
	Tier                   CapsuleTier            `json:"tier"`
	SourceRange            [2]int                 `json:"source_range"`
	MessageIndex           int                    `json:"message_index,omitempty"`
	BlockIndex             int                    `json:"block_index,omitempty"`
	OriginalTokens         int                    `json:"original_tokens"`
	CapsuleTokens          int                    `json:"capsule_tokens"`
	ProjectedSavingsTokens int                    `json:"projected_savings_tokens"`
	AnchorIndices          []int                  `json:"anchor_indices,omitempty"`
	ArchiveURIs            []string               `json:"archive_uris"`
	Summary                string                 `json:"summary"`
	Validation             CapsuleValidationState `json:"validation"`
	CreatedAt              time.Time              `json:"created_at"`
}

type CapsuleBuildOptions struct {
	SessionID                string
	ArchiveDir               string
	ActiveTailMessages       int
	MicroToolResultMinTokens int
	PhaseMinMessages         int
	SessionMinPhaseCapsules  int
	SessionMinOriginalTokens int
	Now                      time.Time
}

func DefaultCapsuleBuildOptions(sessionID string, archiveDir string) CapsuleBuildOptions {
	return CapsuleBuildOptions{
		SessionID:                sessionID,
		ArchiveDir:               archiveDir,
		ActiveTailMessages:       6,
		MicroToolResultMinTokens: 800,
		PhaseMinMessages:         4,
		SessionMinPhaseCapsules:  2,
		SessionMinOriginalTokens: 4000,
		Now:                      time.Now().UTC(),
	}
}

func BuildContextCapsules(messages []types.Message, opts CapsuleBuildOptions) ([]ContextCapsule, error) {
	opts = normalizeCapsuleOptions(opts)
	if len(messages) == 0 || opts.ArchiveDir == "" {
		return nil, nil
	}
	anchors := anchorSet(NewAnchorDetector().Detect(messages))

	capsules := make([]ContextCapsule, 0)
	micro, err := buildMicroCapsules(messages, opts, anchors)
	if err != nil {
		return nil, err
	}
	capsules = append(capsules, micro...)

	phases, err := buildPhaseCapsules(messages, opts, anchors)
	if err != nil {
		return nil, err
	}
	capsules = append(capsules, phases...)

	if session, err := buildSessionCapsule(messages, opts, phases, anchors); err != nil {
		return nil, err
	} else if session != nil {
		capsules = append(capsules, *session)
	}
	return capsules, nil
}

func CapsulesByTier(capsules []ContextCapsule, tiers ...CapsuleTier) []ContextCapsule {
	if len(tiers) == 0 {
		return append([]ContextCapsule(nil), capsules...)
	}
	allowed := make(map[CapsuleTier]bool, len(tiers))
	for _, tier := range tiers {
		allowed[tier] = true
	}
	out := make([]ContextCapsule, 0, len(capsules))
	for _, capsule := range capsules {
		if allowed[capsule.Tier] {
			out = append(out, capsule)
		}
	}
	return out
}

func normalizeCapsuleOptions(opts CapsuleBuildOptions) CapsuleBuildOptions {
	if opts.ActiveTailMessages <= 0 {
		opts.ActiveTailMessages = 6
	}
	if opts.MicroToolResultMinTokens <= 0 {
		opts.MicroToolResultMinTokens = 800
	}
	if opts.PhaseMinMessages <= 0 {
		opts.PhaseMinMessages = 4
	}
	if opts.SessionMinPhaseCapsules <= 0 {
		opts.SessionMinPhaseCapsules = 2
	}
	if opts.SessionMinOriginalTokens <= 0 {
		opts.SessionMinOriginalTokens = 4000
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	return opts
}

func buildMicroCapsules(messages []types.Message, opts CapsuleBuildOptions, anchors map[int]bool) ([]ContextCapsule, error) {
	capsules := make([]ContextCapsule, 0)
	for i, msg := range messages {
		if anchors[i] {
			continue
		}
		for j, block := range msg.Content {
			if block.Type != "tool_result" {
				continue
			}
			originalTokens := tokens.CountString(block.Text)
			if originalTokens < opts.MicroToolResultMinTokens {
				continue
			}
			summary := summarizeToolResult(block.Text)
			entry, err := capsuleArchivePut(opts.ArchiveDir, contentarchive.Input{
				SessionID:    opts.SessionID,
				MessageIndex: i,
				BlockIndex:   j,
				SubLayer:     "capsule_micro",
				Original:     block.Text,
				Preview:      summary,
			}, contentarchive.Limits{})
			if err != nil {
				return nil, err
			}
			if entry == nil {
				continue
			}
			capsules = append(capsules, newCapsule(CapsuleTierMicro, [2]int{i, i}, i, j, originalTokens, summary, nil, []string{entry.URI}, opts.Now))
		}
	}
	return capsules, nil
}

func buildPhaseCapsules(messages []types.Message, opts CapsuleBuildOptions, anchors map[int]bool) ([]ContextCapsule, error) {
	ranges := phaseRanges(messages, opts.ActiveTailMessages, opts.PhaseMinMessages)
	capsules := make([]ContextCapsule, 0, len(ranges))
	for _, r := range ranges {
		anchorIndices := anchorsInRange(anchors, r)
		if len(anchorIndices) > 0 {
			continue
		}
		original := renderCapsuleMessages(messages[r[0] : r[1]+1])
		summary := summarizePhase(messages, r)
		entry, err := capsuleArchivePut(opts.ArchiveDir, contentarchive.Input{
			SessionID:    opts.SessionID,
			MessageIndex: r[0],
			BlockIndex:   -1,
			SubLayer:     "capsule_phase",
			Original:     original,
			Preview:      summary,
		}, contentarchive.Limits{})
		if err != nil {
			return nil, err
		}
		if entry == nil {
			continue
		}
		capsules = append(capsules, newCapsule(CapsuleTierPhase, r, r[0], -1, tokens.CountString(original), summary, anchorIndices, []string{entry.URI}, opts.Now))
	}
	return capsules, nil
}

func buildSessionCapsule(messages []types.Message, opts CapsuleBuildOptions, phases []ContextCapsule, anchors map[int]bool) (*ContextCapsule, error) {
	if len(phases) < opts.SessionMinPhaseCapsules {
		return nil, nil
	}
	end := phases[len(phases)-1].SourceRange[1]
	sourceRange := [2]int{0, end}
	if len(anchorsInRange(anchors, sourceRange)) > 0 {
		return nil, nil
	}
	original := renderCapsuleMessages(messages[:end+1])
	originalTokens := tokens.CountString(original)
	if originalTokens < opts.SessionMinOriginalTokens {
		return nil, nil
	}
	phaseLines := make([]string, 0, len(phases))
	archiveURIs := make([]string, 0, len(phases)+1)
	for _, phase := range phases {
		phaseLines = append(phaseLines, phase.Summary)
		archiveURIs = append(archiveURIs, phase.ArchiveURIs...)
	}
	summary := fmt.Sprintf("Session capsule covering messages 0-%d; phases: %s", end, strings.Join(phaseLines, " | "))
	entry, err := capsuleArchivePut(opts.ArchiveDir, contentarchive.Input{
		SessionID:    opts.SessionID,
		MessageIndex: 0,
		BlockIndex:   -1,
		SubLayer:     "capsule_session",
		Original:     original,
		Preview:      summary,
	}, contentarchive.Limits{})
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	archiveURIs = append(archiveURIs, entry.URI)
	capsule := newCapsule(CapsuleTierSession, sourceRange, 0, -1, originalTokens, summary, nil, archiveURIs, opts.Now)
	return &capsule, nil
}

func newCapsule(tier CapsuleTier, sourceRange [2]int, msgIdx int, blockIdx int, originalTokens int, summary string, anchors []int, archiveURIs []string, now time.Time) ContextCapsule {
	capsuleTokens := tokens.CountString(summary)
	return ContextCapsule{
		ID:                     capsuleID(tier, sourceRange, archiveURIs),
		Tier:                   tier,
		SourceRange:            sourceRange,
		MessageIndex:           msgIdx,
		BlockIndex:             blockIdx,
		OriginalTokens:         originalTokens,
		CapsuleTokens:          capsuleTokens,
		ProjectedSavingsTokens: max(0, originalTokens-capsuleTokens),
		AnchorIndices:          append([]int(nil), anchors...),
		ArchiveURIs:            append([]string(nil), archiveURIs...),
		Summary:                summary,
		Validation:             CapsuleValid,
		CreatedAt:              now,
	}
}

func capsuleID(tier CapsuleTier, sourceRange [2]int, archiveURIs []string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", tier, sourceRange[0], sourceRange[1], strings.Join(archiveURIs, ","))))
	return "cap_" + string(tier) + "_" + hex.EncodeToString(sum[:])[:12]
}

func anchorSet(indices []int) map[int]bool {
	out := make(map[int]bool, len(indices))
	for _, idx := range indices {
		out[idx] = true
	}
	return out
}

func anchorsInRange(anchors map[int]bool, r [2]int) []int {
	out := make([]int, 0)
	for i := r[0]; i <= r[1]; i++ {
		if anchors[i] {
			out = append(out, i)
		}
	}
	return out
}

func phaseRanges(messages []types.Message, activeTail int, minMessages int) [][2]int {
	limit := len(messages) - activeTail
	if limit <= 0 {
		return nil
	}
	start := 0
	ranges := make([][2]int, 0)
	for i := 1; i < limit; i++ {
		if isPhaseBoundary(messages[i]) && i-start >= minMessages {
			ranges = append(ranges, [2]int{start, i - 1})
			start = i
		}
	}
	if limit-start >= minMessages {
		ranges = append(ranges, [2]int{start, limit - 1})
	}
	return ranges
}

func isPhaseBoundary(msg types.Message) bool {
	if strings.ToLower(msg.Role) != "user" {
		return false
	}
	text := strings.ToLower(fullText(msg))
	if strings.TrimSpace(text) == "" {
		return false
	}
	for _, marker := range []string{"task", "todo", "next", "weiter", "fix", "bug", "plan", "go", "mach"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return strings.HasPrefix(strings.TrimSpace(text), "›")
}

func summarizeToolResult(text string) string {
	lines := nonEmptyLines(text)
	first := ""
	if len(lines) > 0 {
		first = trimForCapsule(lines[0], 120)
	}
	return fmt.Sprintf("Tool result capsule: %d lines, %d tokens; first: %s", len(lines), tokens.CountString(text), first)
}

func summarizePhase(messages []types.Message, r [2]int) string {
	roles := make([]string, 0, r[1]-r[0]+1)
	tools := make(map[string]bool)
	for _, msg := range messages[r[0] : r[1]+1] {
		roles = append(roles, msg.Role)
		for _, block := range msg.Content {
			if block.ToolName != "" {
				tools[block.ToolName] = true
			}
		}
	}
	toolList := sortedKeys(tools)
	if len(toolList) == 0 {
		return fmt.Sprintf("Phase capsule messages %d-%d; roles: %s", r[0], r[1], strings.Join(roles, ","))
	}
	return fmt.Sprintf("Phase capsule messages %d-%d; roles: %s; tools: %s", r[0], r[1], strings.Join(roles, ","), strings.Join(toolList, ","))
}

func renderCapsuleMessages(messages []types.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s msg %d]\n", strings.ToUpper(msg.Role), msg.Index))
		for _, block := range msg.Content {
			switch block.Type {
			case "text":
				sb.WriteString(block.Text)
			case "tool_use":
				sb.WriteString(fmt.Sprintf("<tool_use name=%q input=%s>", block.ToolName, block.ToolInput))
			case "tool_result":
				sb.WriteString(fmt.Sprintf("<tool_result id=%q>\n%s\n</tool_result>", block.ToolResultID, block.Text))
			}
			sb.WriteByte('\n')
		}
		sb.WriteString("---\n")
	}
	return sb.String()
}

func nonEmptyLines(text string) []string {
	raw := strings.Split(text, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func trimForCapsule(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit-3] + "..."
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

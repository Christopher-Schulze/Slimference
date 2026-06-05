package contextledger

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

type SelectionAction string

const (
	SelectionCapsule  SelectionAction = "capsule"
	SelectionVerbatim SelectionAction = "verbatim"
	SelectionReject   SelectionAction = "reject"
)

type SelectionReason string

const (
	SelectionReasonArchiveBackedOldContext SelectionReason = "archive_backed_old_context"
	SelectionReasonActiveTurn              SelectionReason = "active_turn"
	SelectionReasonRecentTurn              SelectionReason = "recent_turn"
	SelectionReasonHighRiskFailure         SelectionReason = "high_risk_failure"
	SelectionReasonMissingArchive          SelectionReason = "missing_archive"
	SelectionReasonMissingPolicySession    SelectionReason = "missing_policy_session"
	SelectionReasonMissingProvenance       SelectionReason = "missing_provenance"
	SelectionReasonMissingFacts            SelectionReason = "missing_facts"
	SelectionReasonWrongSession            SelectionReason = "wrong_session"
	SelectionReasonActivePath              SelectionReason = "active_path"
	SelectionReasonQualityPressure         SelectionReason = "quality_pressure"
	SelectionReasonBudgetExhausted         SelectionReason = "budget_exhausted"
	SelectionReasonUnknownKind             SelectionReason = "unknown_kind"
)

type SelectionPolicy struct {
	SessionID       string
	ActiveTurnID    string
	RecentTurnIDs   []string
	ActivePaths     []string
	QualityPressure bool
	MaxCapsules     int
}

type CapsuleDecision struct {
	Capsule Capsule         `json:"capsule"`
	Action  SelectionAction `json:"action"`
	Reason  SelectionReason `json:"reason"`
}

type SelectionReport struct {
	Decisions []CapsuleDecision `json:"decisions"`
	Capsules  int               `json:"capsules"`
	Verbatim  int               `json:"verbatim"`
	Rejected  int               `json:"rejected"`
}

func SelectCapsules(capsules []Capsule, policy SelectionPolicy) SelectionReport {
	recentTurns := stringSet(policy.RecentTurnIDs)
	activePaths := pathSet(policy.ActivePaths)
	report := SelectionReport{Decisions: make([]CapsuleDecision, 0, len(capsules))}
	for _, capsule := range capsules {
		action, reason := selectCapsule(capsule, policy, recentTurns, activePaths, report.Capsules)
		decision := CapsuleDecision{Capsule: capsule, Action: action, Reason: reason}
		report.Decisions = append(report.Decisions, decision)
		switch action {
		case SelectionCapsule:
			report.Capsules++
		case SelectionVerbatim:
			report.Verbatim++
		case SelectionReject:
			report.Rejected++
		}
	}
	return report
}

func selectCapsule(capsule Capsule, policy SelectionPolicy, recentTurns, activePaths map[string]struct{}, selected int) (SelectionAction, SelectionReason) {
	if !knownKind(capsule.Kind) {
		return SelectionReject, SelectionReasonUnknownKind
	}
	if policy.QualityPressure {
		return SelectionVerbatim, SelectionReasonQualityPressure
	}
	wantSession := strings.TrimSpace(policy.SessionID)
	if wantSession == "" {
		return SelectionVerbatim, SelectionReasonMissingPolicySession
	}
	sessionID := strings.TrimSpace(capsule.Provenance.SessionID)
	turnID := strings.TrimSpace(capsule.Provenance.TurnID)
	source := strings.TrimSpace(capsule.Provenance.Source)
	if sessionID == "" || turnID == "" || source == "" {
		return SelectionVerbatim, SelectionReasonMissingProvenance
	}
	if sessionID != wantSession {
		return SelectionReject, SelectionReasonWrongSession
	}
	if active := strings.TrimSpace(policy.ActiveTurnID); active != "" && turnID == active {
		return SelectionVerbatim, SelectionReasonActiveTurn
	}
	if _, ok := recentTurns[turnID]; ok {
		return SelectionVerbatim, SelectionReasonRecentTurn
	}
	if !capsuleHasRequiredFacts(capsule) {
		return SelectionVerbatim, SelectionReasonMissingFacts
	}
	if capsuleTouchesActivePath(capsule, activePaths) {
		return SelectionVerbatim, SelectionReasonActivePath
	}
	if capsule.Kind == CapsuleFailure {
		return SelectionVerbatim, SelectionReasonHighRiskFailure
	}
	if len(capsule.Archives) == 0 {
		return SelectionVerbatim, SelectionReasonMissingArchive
	}
	if policy.MaxCapsules > 0 && selected >= policy.MaxCapsules {
		return SelectionVerbatim, SelectionReasonBudgetExhausted
	}
	return SelectionCapsule, SelectionReasonArchiveBackedOldContext
}

func knownKind(kind CapsuleKind) bool {
	switch kind {
	case CapsuleCommand, CapsuleFile, CapsuleSearch, CapsuleFailure, CapsuleDecisionContext, CapsuleRecoveryContext:
		return true
	default:
		return false
	}
}

func capsuleHasRequiredFacts(capsule Capsule) bool {
	switch capsule.Kind {
	case CapsuleCommand:
		return hasFact(capsule, "command") && hasFact(capsule, "exit_code")
	case CapsuleFile:
		return hasFact(capsule, "path") && hasFact(capsule, "repo_root") && hasFact(capsule, "full_pass_turn")
	case CapsuleSearch:
		return hasFact(capsule, "command") && hasFact(capsule, "repo_root") && hasFact(capsule, "pattern_hash")
	case CapsuleFailure:
		return hasFact(capsule, "message") && hasFact(capsule, "exit_code")
	case CapsuleDecisionContext:
		return hasAnyFact(capsule, "goal", "accepted_plan")
	case CapsuleRecoveryContext:
		return hasFact(capsule, "archive_ids") && hasFact(capsule, "status")
	default:
		return false
	}
}

func hasAnyFact(capsule Capsule, keys ...string) bool {
	for _, key := range keys {
		if hasFact(capsule, key) {
			return true
		}
	}
	return false
}

func hasFact(capsule Capsule, key string) bool {
	return strings.TrimSpace(capsule.Facts[key]) != ""
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func pathSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = cleanPathFact(value)
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func capsuleTouchesActivePath(capsule Capsule, activePaths map[string]struct{}) bool {
	if len(activePaths) == 0 {
		return false
	}
	switch capsule.Kind {
	case CapsuleFile:
		return pathMatchesActive(capsule.Facts["path"], capsule.Facts["repo_root"], activePaths)
	case CapsuleSearch:
		repoRoot := capsule.Facts["repo_root"]
		for _, file := range strings.Split(capsule.Facts["files_matched"], ",") {
			if pathMatchesActive(file, repoRoot, activePaths) {
				return true
			}
		}
	case CapsuleDecisionContext:
		for _, file := range strings.Split(capsule.Facts["active_files"], ",") {
			if _, ok := activePaths[cleanPathFact(file)]; ok {
				return true
			}
		}
	}
	return false
}

func pathMatchesActive(path, repoRoot string, activePaths map[string]struct{}) bool {
	path = cleanPathFact(path)
	if path == "" {
		return false
	}
	if _, ok := activePaths[path]; ok {
		return true
	}
	repoRoot = cleanPathFact(repoRoot)
	if repoRoot == "" || filepath.IsAbs(path) {
		return false
	}
	_, ok := activePaths[cleanPathFact(filepath.Join(repoRoot, path))]
	return ok
}

type ArchiveLoader func(id string) ([]byte, error)

type ArchiveExpansion struct {
	ID    string `json:"id"`
	Bytes []byte `json:"-"`
}

func ExpandCapsuleArchives(capsule Capsule, load ArchiveLoader) ([]ArchiveExpansion, error) {
	if load == nil {
		return nil, errors.New("archive loader is required")
	}
	ids := sortedStrings(capsule.Archives)
	if len(ids) == 0 {
		return nil, errors.New("capsule has no archive ids")
	}
	out := make([]ArchiveExpansion, 0, len(ids))
	for _, id := range ids {
		body, err := load(id)
		if err != nil {
			return nil, err
		}
		out = append(out, ArchiveExpansion{ID: id, Bytes: append([]byte(nil), body...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

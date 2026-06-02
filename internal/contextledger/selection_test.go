package contextledger

import (
	"errors"
	"testing"
)

func TestSelectCapsulesKeepsActiveRecentAndHighRiskVerbatim(t *testing.T) {
	t.Parallel()
	capsules := []Capsule{
		testCapsule(CapsuleFile, "s", "active", "arch-active"),
		testCapsule(CapsuleSearch, "s", "recent", "arch-recent"),
		testCapsule(CapsuleFailure, "s", "old", "arch-failure"),
		testCapsule(CapsuleCommand, "s", "old", "arch-command"),
	}
	report := SelectCapsules(capsules, SelectionPolicy{
		SessionID:     "s",
		ActiveTurnID:  "active",
		RecentTurnIDs: []string{"recent"},
	})
	if report.Capsules != 1 || report.Verbatim != 3 || report.Rejected != 0 {
		t.Fatalf("summary mismatch: %+v", report)
	}
	assertDecision(t, report.Decisions[0], SelectionVerbatim, SelectionReasonActiveTurn)
	assertDecision(t, report.Decisions[1], SelectionVerbatim, SelectionReasonRecentTurn)
	assertDecision(t, report.Decisions[2], SelectionVerbatim, SelectionReasonHighRiskFailure)
	assertDecision(t, report.Decisions[3], SelectionCapsule, SelectionReasonArchiveBackedOldContext)
}

func TestSelectCapsulesFailsClosedOnMissingArchiveAndProvenance(t *testing.T) {
	t.Parallel()
	capsules := []Capsule{
		testCapsule(CapsuleFile, "s", "old", ""),
		testCapsule(CapsuleSearch, "", "old", "arch"),
		testCapsule(CapsuleCommand, "other", "old", "arch"),
		{Kind: CapsuleKind("alien"), Provenance: Provenance{SessionID: "s", TurnID: "old", Source: "test"}, Archives: []string{"arch"}},
	}
	report := SelectCapsules(capsules, SelectionPolicy{SessionID: "s"})
	if report.Capsules != 0 || report.Verbatim != 2 || report.Rejected != 2 {
		t.Fatalf("summary mismatch: %+v", report)
	}
	assertDecision(t, report.Decisions[0], SelectionVerbatim, SelectionReasonMissingArchive)
	assertDecision(t, report.Decisions[1], SelectionVerbatim, SelectionReasonMissingProvenance)
	assertDecision(t, report.Decisions[2], SelectionReject, SelectionReasonWrongSession)
	assertDecision(t, report.Decisions[3], SelectionReject, SelectionReasonUnknownKind)
}

func TestSelectCapsulesRequiresPolicySession(t *testing.T) {
	t.Parallel()
	report := SelectCapsules([]Capsule{
		testCapsule(CapsuleCommand, "s", "old", "arch-command"),
	}, SelectionPolicy{})
	if report.Capsules != 0 || report.Verbatim != 1 || report.Rejected != 0 {
		t.Fatalf("summary mismatch: %+v", report)
	}
	assertDecision(t, report.Decisions[0], SelectionVerbatim, SelectionReasonMissingPolicySession)
}

func TestSelectCapsulesKeepsIncompleteCapsulesVerbatim(t *testing.T) {
	t.Parallel()
	capsules := []Capsule{
		{
			Kind:       CapsuleCommand,
			Provenance: Provenance{SessionID: "s", TurnID: "old", Source: "test"},
			Facts:      map[string]string{"exit_code": "0"},
			Archives:   []string{"arch-command"},
		},
		{
			Kind:       CapsuleFile,
			Provenance: Provenance{SessionID: "s", TurnID: "old", Source: "test"},
			Facts:      map[string]string{"path": "/repo/a.go"},
			Archives:   []string{"arch-file"},
		},
		{
			Kind:       CapsuleSearch,
			Provenance: Provenance{SessionID: "s", TurnID: "old", Source: "test"},
			Facts:      map[string]string{"command": "rg needle ."},
			Archives:   []string{"arch-search"},
		},
	}
	report := SelectCapsules(capsules, SelectionPolicy{SessionID: "s"})
	if report.Capsules != 0 || report.Verbatim != 3 || report.Rejected != 0 {
		t.Fatalf("summary mismatch: %+v", report)
	}
	for i, decision := range report.Decisions {
		if decision.Action != SelectionVerbatim || decision.Reason != SelectionReasonMissingFacts {
			t.Fatalf("decision[%d]=%+v want missing-facts verbatim", i, decision)
		}
	}
}

func TestSelectCapsulesHonorsBudget(t *testing.T) {
	t.Parallel()
	report := SelectCapsules([]Capsule{
		testCapsule(CapsuleFile, "s", "old-1", "a"),
		testCapsule(CapsuleSearch, "s", "old-2", "b"),
	}, SelectionPolicy{SessionID: "s", MaxCapsules: 1})
	if report.Capsules != 1 || report.Verbatim != 1 {
		t.Fatalf("summary mismatch: %+v", report)
	}
	assertDecision(t, report.Decisions[0], SelectionCapsule, SelectionReasonArchiveBackedOldContext)
	assertDecision(t, report.Decisions[1], SelectionVerbatim, SelectionReasonBudgetExhausted)
}

func TestExpandCapsuleArchivesCopiesAndFailsClosed(t *testing.T) {
	t.Parallel()
	capsule := testCapsule(CapsuleFile, "s", "old", "b", "a", "a")
	loaded := map[string][]byte{
		"a": []byte("alpha"),
		"b": []byte("beta"),
	}
	expansions, err := ExpandCapsuleArchives(capsule, func(id string) ([]byte, error) {
		body, ok := loaded[id]
		if !ok {
			return nil, errors.New("missing")
		}
		return body, nil
	})
	if err != nil {
		t.Fatalf("ExpandCapsuleArchives error: %v", err)
	}
	if len(expansions) != 2 || expansions[0].ID != "a" || string(expansions[0].Bytes) != "alpha" || expansions[1].ID != "b" {
		t.Fatalf("bad expansions: %+v", expansions)
	}
	expansions[0].Bytes[0] = 'X'
	if string(loaded["a"]) != "alpha" {
		t.Fatal("expansion body aliases loader storage")
	}
	if _, err := ExpandCapsuleArchives(Capsule{}, nil); err == nil {
		t.Fatal("expected nil loader error")
	}
	if _, err := ExpandCapsuleArchives(testCapsule(CapsuleFile, "s", "old", ""), func(string) ([]byte, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("expected missing archive error")
	}
}

func testCapsule(kind CapsuleKind, sessionID, turnID string, archives ...string) Capsule {
	return Capsule{
		Kind:       kind,
		Provenance: Provenance{SessionID: sessionID, TurnID: turnID, Source: "test"},
		Facts:      testCapsuleFacts(kind),
		Archives:   sortedStrings(archives),
	}
}

func testCapsuleFacts(kind CapsuleKind) map[string]string {
	switch kind {
	case CapsuleCommand:
		return map[string]string{"command": "go test ./...", "exit_code": "0"}
	case CapsuleFile:
		return map[string]string{"path": "/repo/a.go", "full_pass_turn": "turn-1"}
	case CapsuleSearch:
		return map[string]string{"command": "rg needle .", "pattern_hash": "hash"}
	case CapsuleFailure:
		return map[string]string{"message": "failed", "exit_code": "1"}
	default:
		return map[string]string{"k": "v"}
	}
}

func assertDecision(t *testing.T, decision CapsuleDecision, action SelectionAction, reason SelectionReason) {
	t.Helper()
	if decision.Action != action || decision.Reason != reason {
		t.Fatalf("decision=%+v want action=%s reason=%s", decision, action, reason)
	}
}

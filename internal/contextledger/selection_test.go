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
			Kind:       CapsuleFile,
			Provenance: Provenance{SessionID: "s", TurnID: "old", Source: "test"},
			Facts:      map[string]string{"path": "/repo/a.go", "full_pass_turn": "turn-1"},
			Archives:   []string{"arch-file"},
		},
		{
			Kind:       CapsuleSearch,
			Provenance: Provenance{SessionID: "s", TurnID: "old", Source: "test"},
			Facts:      map[string]string{"command": "rg needle ."},
			Archives:   []string{"arch-search"},
		},
		{
			Kind:       CapsuleSearch,
			Provenance: Provenance{SessionID: "s", TurnID: "old", Source: "test"},
			Facts:      map[string]string{"command": "rg needle .", "pattern_hash": "hash"},
			Archives:   []string{"arch-search"},
		},
		{
			Kind:       CapsuleDecisionContext,
			Provenance: Provenance{SessionID: "s", TurnID: "old", Source: "test"},
			Facts:      map[string]string{"blocked_reason": "needs proof"},
			Archives:   []string{"arch-decision"},
		},
		{
			Kind:       CapsuleRecoveryContext,
			Provenance: Provenance{SessionID: "s", TurnID: "old", Source: "test"},
			Facts:      map[string]string{"archive_ids": "arch-recovery"},
			Archives:   []string{"arch-recovery"},
		},
	}
	report := SelectCapsules(capsules, SelectionPolicy{SessionID: "s"})
	if report.Capsules != 0 || report.Verbatim != 7 || report.Rejected != 0 {
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

func TestSelectCapsulesKeepsActivePathsVerbatim(t *testing.T) {
	t.Parallel()
	capsules := []Capsule{
		testCapsule(CapsuleFile, "s", "old-file", "arch-file"),
		testCapsule(CapsuleSearch, "s", "old-search", "arch-search"),
		testCapsule(CapsuleDecisionContext, "s", "old-decision", "arch-decision"),
		testCapsule(CapsuleCommand, "s", "old-command", "arch-command"),
	}
	capsules[1].Facts["files_matched"] = "b.go,a.go"
	capsules[2].Facts["active_files"] = "/repo/a.go"
	report := SelectCapsules(capsules, SelectionPolicy{
		SessionID:   "s",
		ActivePaths: []string{"/repo/./a.go"},
	})
	if report.Capsules != 1 || report.Verbatim != 3 || report.Rejected != 0 {
		t.Fatalf("summary mismatch: %+v", report)
	}
	assertDecision(t, report.Decisions[0], SelectionVerbatim, SelectionReasonActivePath)
	assertDecision(t, report.Decisions[1], SelectionVerbatim, SelectionReasonActivePath)
	assertDecision(t, report.Decisions[2], SelectionVerbatim, SelectionReasonActivePath)
	assertDecision(t, report.Decisions[3], SelectionCapsule, SelectionReasonArchiveBackedOldContext)
}

func TestSelectCapsulesQualityPressureFullPasses(t *testing.T) {
	t.Parallel()
	report := SelectCapsules([]Capsule{
		testCapsule(CapsuleFile, "s", "old-file", "arch-file"),
		testCapsule(CapsuleCommand, "s", "old-command", "arch-command"),
	}, SelectionPolicy{
		SessionID:       "s",
		QualityPressure: true,
	})
	if report.Capsules != 0 || report.Verbatim != 2 || report.Rejected != 0 {
		t.Fatalf("summary mismatch: %+v", report)
	}
	assertDecision(t, report.Decisions[0], SelectionVerbatim, SelectionReasonQualityPressure)
	assertDecision(t, report.Decisions[1], SelectionVerbatim, SelectionReasonQualityPressure)
}

func TestSelectCapsulesAcceptsArchiveBackedDecisionAndRecoveryCapsules(t *testing.T) {
	t.Parallel()
	report := SelectCapsules([]Capsule{
		testCapsule(CapsuleDecisionContext, "s", "old-decision", "decision-arch"),
		testCapsule(CapsuleRecoveryContext, "s", "old-recovery", "recovery-arch"),
	}, SelectionPolicy{SessionID: "s"})
	if report.Capsules != 2 || report.Verbatim != 0 || report.Rejected != 0 {
		t.Fatalf("summary mismatch: %+v", report)
	}
	assertDecision(t, report.Decisions[0], SelectionCapsule, SelectionReasonArchiveBackedOldContext)
	assertDecision(t, report.Decisions[1], SelectionCapsule, SelectionReasonArchiveBackedOldContext)
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

func TestVerifyCapsuleArchivesAvoidsCopiesAndFailsClosed(t *testing.T) {
	t.Parallel()
	calls := 0
	count, err := VerifyCapsuleArchives(testCapsule(CapsuleFile, "s", "old", "b", "a", "a"), func(id string) ([]byte, error) {
		calls++
		if id == "a" || id == "b" {
			return []byte("body"), nil
		}
		return nil, errors.New("missing")
	})
	if err != nil {
		t.Fatalf("VerifyCapsuleArchives error: %v", err)
	}
	if count != 2 || calls != 2 {
		t.Fatalf("count=%d calls=%d want 2", count, calls)
	}
	if _, err := VerifyCapsuleArchives(Capsule{}, nil); err == nil {
		t.Fatal("expected nil loader error")
	}
	if _, err := VerifyCapsuleArchives(testCapsule(CapsuleFile, "s", "old", ""), func(string) ([]byte, error) {
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
		return map[string]string{"path": "/repo/a.go", "repo_root": "/repo", "full_pass_turn": "turn-1"}
	case CapsuleSearch:
		return map[string]string{"command": "rg needle .", "repo_root": "/repo", "pattern_hash": "hash"}
	case CapsuleFailure:
		return map[string]string{"message": "failed", "exit_code": "1"}
	case CapsuleDecisionContext:
		return map[string]string{"goal": "preserve context", "accepted_plan": "archive-backed ledger"}
	case CapsuleRecoveryContext:
		return map[string]string{"archive_ids": "arch", "status": "success"}
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

package contextledger

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestBuildCommandCapsuleDeterministic(t *testing.T) {
	capsule, err := BuildCommandCapsule(CommandObservation{
		SessionID:   "s",
		TurnID:      "t",
		CommandLine: "  go test ./...  ",
		CWD:         "/repo/./app",
		ExitCode:    1,
		Stdout:      []byte("ok"),
		Stderr:      []byte("fail"),
		ArchiveIDs:  []string{"b", "a", "a"},
		Mechanisms:  []string{"search", "read_delta", "search"},
	})
	if err != nil {
		t.Fatalf("BuildCommandCapsule error: %v", err)
	}
	if capsule.Kind != CapsuleCommand || capsule.Provenance.SessionID != "s" || capsule.Provenance.TurnID != "t" {
		t.Fatalf("provenance mismatch: %+v", capsule)
	}
	if capsule.Facts["command"] != "go test ./..." || capsule.Facts["cwd"] != "/repo/app" || capsule.Facts["exit_code"] != "1" {
		t.Fatalf("facts mismatch: %+v", capsule.Facts)
	}
	if capsule.Facts["mechanisms"] != "read_delta,search" {
		t.Fatalf("mechanisms not stable: %+v", capsule.Facts)
	}
	if got, want := capsule.Hashes["stdout_sha256"], sha256Hex("ok"); got != want {
		t.Fatalf("stdout hash=%q want %q", got, want)
	}
	if len(capsule.Archives) != 2 || capsule.Archives[0] != "a" || capsule.Archives[1] != "b" {
		t.Fatalf("archives not stable: %+v", capsule.Archives)
	}
}

func TestBuildFileCapsuleRequiresArchiveForOmittedContent(t *testing.T) {
	if _, err := BuildFileCapsule(FileObservation{Path: "/repo/a.go", Content: []byte("package main")}); err == nil {
		t.Fatal("expected archive requirement for omitted file content")
	}
	if _, err := BuildFileCapsule(FileObservation{Path: "/repo/a.go", ArchiveID: "arch"}); err == nil {
		t.Fatal("expected full-pass turn requirement for archived file content")
	}
	capsule, err := BuildFileCapsule(FileObservation{
		Path:         "/repo/./a.go",
		RepoRoot:     "/repo",
		Range:        "1:20",
		Content:      []byte("package main"),
		ArchiveID:    "arch",
		FullPassTurn: "turn-1",
	})
	if err != nil {
		t.Fatalf("BuildFileCapsule error: %v", err)
	}
	if capsule.Facts["path"] != "/repo/a.go" || capsule.Facts["range"] != "1:20" {
		t.Fatalf("file facts mismatch: %+v", capsule.Facts)
	}
	if got, want := capsule.Hashes["content_sha256"], sha256Hex("package main"); got != want {
		t.Fatalf("content hash=%q want %q", got, want)
	}
	if len(capsule.Archives) != 1 || capsule.Archives[0] != "arch" {
		t.Fatalf("archive mismatch: %+v", capsule.Archives)
	}
}

func TestBuildSearchCapsuleSortsEvidence(t *testing.T) {
	if _, err := BuildSearchCapsule(SearchObservation{CommandLine: "rg -n TODO .", PatternHash: "pat"}); err == nil {
		t.Fatal("expected search scope requirement")
	}
	capsule, err := BuildSearchCapsule(SearchObservation{
		CommandLine:  "rg -n TODO .",
		RepoRoot:     "/repo",
		PatternHash:  "pat",
		FilesMatched: []string{"b.go", "a.go", "b.go"},
		OmittedCount: 3,
		Output:       []byte("a.go:1:TODO"),
		ArchiveID:    "search-arch",
	})
	if err != nil {
		t.Fatalf("BuildSearchCapsule error: %v", err)
	}
	if capsule.Facts["files_matched"] != "a.go,b.go" || capsule.Facts["omitted_count"] != "3" {
		t.Fatalf("search facts mismatch: %+v", capsule.Facts)
	}
	if got, want := capsule.Hashes["output_sha256"], sha256Hex("a.go:1:TODO"); got != want {
		t.Fatalf("output hash=%q want %q", got, want)
	}
}

func TestBuildFailureCapsuleRequiresMessage(t *testing.T) {
	if _, err := BuildFailureCapsule(FailureObservation{}); err == nil {
		t.Fatal("expected message requirement")
	}
	capsule, err := BuildFailureCapsule(FailureObservation{
		Tool:     "go test",
		File:     "/repo/pkg/a_test.go",
		Line:     "42",
		Column:   "7",
		Message:  "expected true",
		ExitCode: 1,
	})
	if err != nil {
		t.Fatalf("BuildFailureCapsule error: %v", err)
	}
	if capsule.Kind != CapsuleFailure || capsule.Facts["message"] != "expected true" || capsule.Facts["exit_code"] != "1" {
		t.Fatalf("failure capsule mismatch: %+v", capsule)
	}
}

func TestBuildDecisionCapsulePreservesConstraintsAndSortsFiles(t *testing.T) {
	if _, err := BuildDecisionCapsule(DecisionObservation{}); err == nil {
		t.Fatal("expected decision goal or plan requirement")
	}
	capsule, err := BuildDecisionCapsule(DecisionObservation{
		SessionID:     "s",
		TurnID:        "t",
		Goal:          "ship safe savings",
		Constraints:   []string{"zero drawdown", "archive backed", "zero drawdown"},
		AcceptedPlan:  "keep ledger telemetry only",
		BlockedReason: "live proof pending",
		ActiveFiles:   []string{"/repo/b.go", "/repo/./a.go", "/repo/b.go"},
		ArchiveID:     "decision-arch",
	})
	if err != nil {
		t.Fatalf("BuildDecisionCapsule error: %v", err)
	}
	if capsule.Kind != CapsuleDecisionContext || capsule.Facts["goal"] != "ship safe savings" {
		t.Fatalf("decision capsule mismatch: %+v", capsule)
	}
	if capsule.Facts["constraints"] != "zero drawdown\narchive backed" {
		t.Fatalf("constraints should preserve first-seen order: %+v", capsule.Facts)
	}
	if capsule.Facts["active_files"] != "/repo/a.go,/repo/b.go" {
		t.Fatalf("active files should be stable path facts: %+v", capsule.Facts)
	}
	if len(capsule.Archives) != 1 || capsule.Archives[0] != "decision-arch" {
		t.Fatalf("archive mismatch: %+v", capsule.Archives)
	}
}

func TestBuildRecoveryCapsuleRequiresArchiveAndStatus(t *testing.T) {
	if _, err := BuildRecoveryCapsule(RecoveryObservation{AttemptStatus: "success"}); err == nil {
		t.Fatal("expected archive requirement")
	}
	if _, err := BuildRecoveryCapsule(RecoveryObservation{ArchiveIDs: []string{"a"}}); err == nil {
		t.Fatal("expected status requirement")
	}
	capsule, err := BuildRecoveryCapsule(RecoveryObservation{
		SessionID:     "s",
		TurnID:        "t",
		ArchiveIDs:    []string{"b", "a", "a"},
		RequestedURI:  "local-archive://a",
		AttemptStatus: "success",
		LastError:     " ",
	})
	if err != nil {
		t.Fatalf("BuildRecoveryCapsule error: %v", err)
	}
	if capsule.Kind != CapsuleRecoveryContext || capsule.Facts["archive_ids"] != "a,b" || capsule.Facts["status"] != "success" {
		t.Fatalf("recovery capsule mismatch: %+v", capsule)
	}
	if len(capsule.Archives) != 2 || capsule.Archives[0] != "a" || capsule.Archives[1] != "b" {
		t.Fatalf("archives not stable: %+v", capsule.Archives)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

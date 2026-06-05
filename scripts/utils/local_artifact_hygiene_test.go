package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanLocalGeneratedArtifactsFindsOnlyKnownCandidates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFileForLocalArtifactTest(t, filepath.Join(root, "proxy.test"), "binary")
	writeFileForLocalArtifactTest(t, filepath.Join(root, "readcache.test"), "binary")
	writeFileForLocalArtifactTest(t, filepath.Join(root, "benchmarks"), "bench")
	writeFileForLocalArtifactTest(t, filepath.Join(root, "dist", "slimference.tar.gz"), "release")
	writeFileForLocalArtifactTest(t, filepath.Join(root, "cmd", "slimference", "~", "backup"), "backup")
	writeFileForLocalArtifactTest(t, filepath.Join(root, "coverage.out"), "not part of this guard")

	report, err := scanLocalGeneratedArtifacts(root, func(_, relPath string) (localArtifactGitState, error) {
		return localArtifactGitState{Ignored: relPath != "dist"}, nil
	})
	if err != nil {
		t.Fatalf("scanLocalGeneratedArtifacts() error = %v", err)
	}
	if report.Clean {
		t.Fatalf("report should not be clean")
	}
	if len(report.Findings) != len(localGeneratedArtifactCandidates) {
		t.Fatalf("findings = %d, want %d: %+v", len(report.Findings), len(localGeneratedArtifactCandidates), report.Findings)
	}
	if report.TotalBytes == 0 {
		t.Fatalf("expected non-zero total bytes")
	}
	for _, finding := range report.Findings {
		if finding.Path == "coverage.out" {
			t.Fatalf("guard should not sweep unrelated ignored files")
		}
	}
}

func TestCleanLocalGeneratedArtifactsSkipsTrackedCandidate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	trackedPath := filepath.Join(root, "proxy.test")
	untrackedPath := filepath.Join(root, "readcache.test")
	writeFileForLocalArtifactTest(t, trackedPath, "tracked")
	writeFileForLocalArtifactTest(t, untrackedPath, "untracked")

	report, err := scanLocalGeneratedArtifacts(root, func(_, relPath string) (localArtifactGitState, error) {
		return localArtifactGitState{Tracked: relPath == "proxy.test", Ignored: true}, nil
	})
	if err != nil {
		t.Fatalf("scanLocalGeneratedArtifacts() error = %v", err)
	}
	cleaned, err := cleanLocalGeneratedArtifacts(root, report, func(_, relPath string) (localArtifactGitState, error) {
		return localArtifactGitState{Tracked: relPath == "proxy.test", Ignored: true}, nil
	})
	if err != nil {
		t.Fatalf("cleanLocalGeneratedArtifacts() error = %v", err)
	}
	if cleaned.Clean {
		t.Fatalf("tracked candidate must keep report non-clean")
	}
	if _, err := os.Stat(trackedPath); err != nil {
		t.Fatalf("tracked candidate should not be removed: %v", err)
	}
	if _, err := os.Stat(untrackedPath); !os.IsNotExist(err) {
		t.Fatalf("untracked candidate should be removed, stat err=%v", err)
	}
}

func TestRunLocalArtifactHygieneJSONCheckReportsFindings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	initGitRepoForLocalArtifactTest(t, root)
	writeFileForLocalArtifactTest(t, filepath.Join(root, "proxy.test"), "binary")

	var stdout, stderr bytes.Buffer
	code := runLocalArtifactHygiene([]string{"--root", root, "--json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var report localArtifactHygieneReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, stdout.String())
	}
	if report.Clean || len(report.Findings) != 1 || report.Findings[0].Path != "proxy.test" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunLocalArtifactHygieneClean(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	initGitRepoForLocalArtifactTest(t, root)
	writeFileForLocalArtifactTest(t, filepath.Join(root, "proxy.test"), "binary")

	var stdout, stderr bytes.Buffer
	code := runLocalArtifactHygiene([]string{"--root", root, "--clean"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "proxy.test")); !os.IsNotExist(err) {
		t.Fatalf("proxy.test should be removed, stat err=%v", err)
	}
	if !strings.Contains(stdout.String(), "removed") {
		t.Fatalf("clean output should mention removal: %s", stdout.String())
	}
}

func writeFileForLocalArtifactTest(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func initGitRepoForLocalArtifactTest(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, string(out))
	}
}

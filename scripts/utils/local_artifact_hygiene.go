package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type localArtifactCandidate struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type localArtifactGitState struct {
	Tracked bool `json:"tracked"`
	Ignored bool `json:"ignored"`
}

type localArtifactFinding struct {
	Path         string `json:"path"`
	Reason       string `json:"reason"`
	SizeBytes    int64  `json:"size_bytes"`
	Tracked      bool   `json:"tracked"`
	Ignored      bool   `json:"ignored"`
	Removed      bool   `json:"removed"`
	UnsafeReason string `json:"unsafe_reason,omitempty"`
}

type localArtifactHygieneReport struct {
	Root         string                   `json:"root"`
	Clean        bool                     `json:"clean"`
	Candidates   []localArtifactCandidate `json:"candidates"`
	Findings     []localArtifactFinding   `json:"findings,omitempty"`
	TotalBytes   int64                    `json:"total_bytes"`
	RemovedBytes int64                    `json:"removed_bytes"`
}

type localArtifactGitStatusFunc func(root, relPath string) (localArtifactGitState, error)

var localGeneratedArtifactCandidates = []localArtifactCandidate{
	{Path: "proxy.test", Reason: "Go test binary left in repository root"},
	{Path: "readcache.test", Reason: "Go test binary left in repository root"},
	{Path: "benchmarks", Reason: "Local benchmark binary/output left in repository root"},
	{Path: "dist", Reason: "Local release scratch output"},
	{Path: filepath.Join("cmd", "slimference", "~"), Reason: "Editor backup directory"},
}

func runLocalArtifactHygiene(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("local-artifact-hygiene", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "write machine-readable JSON")
	clean := fs.Bool("clean", false, "remove safe untracked generated artifacts")
	rootFlag := fs.String("root", "", "repository root override")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "local-artifact-hygiene: unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}

	root := *rootFlag
	var err error
	if root == "" {
		root, err = findLocalArtifactRepoRoot()
		if err != nil {
			fmt.Fprintf(stderr, "local-artifact-hygiene: %v\n", err)
			return 2
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(stderr, "local-artifact-hygiene: resolve root: %v\n", err)
		return 2
	}

	report, err := scanLocalGeneratedArtifacts(root, defaultLocalArtifactGitStatus)
	if err != nil {
		fmt.Fprintf(stderr, "local-artifact-hygiene: %v\n", err)
		return 2
	}
	if *clean {
		report, err = cleanLocalGeneratedArtifacts(root, report, defaultLocalArtifactGitStatus)
		if err != nil {
			fmt.Fprintf(stderr, "local-artifact-hygiene: %v\n", err)
			return 1
		}
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(stderr, "local-artifact-hygiene: encode report: %v\n", err)
			return 2
		}
	} else {
		writeLocalArtifactHygieneText(stdout, report, *clean)
	}

	if report.Clean {
		return 0
	}
	return 1
}

func findLocalArtifactRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}

func scanLocalGeneratedArtifacts(root string, gitStatus localArtifactGitStatusFunc) (localArtifactHygieneReport, error) {
	report := localArtifactHygieneReport{
		Root:       root,
		Clean:      true,
		Candidates: append([]localArtifactCandidate(nil), localGeneratedArtifactCandidates...),
	}
	for _, candidate := range localGeneratedArtifactCandidates {
		absPath, err := safeLocalArtifactPath(root, candidate.Path)
		if err != nil {
			return report, err
		}
		info, err := os.Lstat(absPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return report, fmt.Errorf("stat %s: %w", candidate.Path, err)
		}
		state, err := gitStatus(root, candidate.Path)
		if err != nil {
			return report, err
		}
		size, err := localArtifactSize(absPath, info)
		if err != nil {
			return report, fmt.Errorf("size %s: %w", candidate.Path, err)
		}
		finding := localArtifactFinding{
			Path:      candidate.Path,
			Reason:    candidate.Reason,
			SizeBytes: size,
			Tracked:   state.Tracked,
			Ignored:   state.Ignored,
		}
		if state.Tracked {
			finding.UnsafeReason = "candidate is tracked by git"
		}
		report.Findings = append(report.Findings, finding)
		report.TotalBytes += size
		report.Clean = false
	}
	return report, nil
}

func cleanLocalGeneratedArtifacts(root string, report localArtifactHygieneReport, gitStatus localArtifactGitStatusFunc) (localArtifactHygieneReport, error) {
	for i := range report.Findings {
		finding := &report.Findings[i]
		if finding.Tracked {
			continue
		}
		absPath, err := safeLocalArtifactPath(root, finding.Path)
		if err != nil {
			return report, err
		}
		if err := os.RemoveAll(absPath); err != nil {
			return report, fmt.Errorf("remove %s: %w", finding.Path, err)
		}
		finding.Removed = true
		report.RemovedBytes += finding.SizeBytes
	}

	after, err := scanLocalGeneratedArtifacts(root, gitStatus)
	if err != nil {
		return report, err
	}
	if len(after.Findings) == 0 {
		report.Clean = true
		return report, nil
	}
	report.Clean = false
	return report, nil
}

func defaultLocalArtifactGitStatus(root, relPath string) (localArtifactGitState, error) {
	tracked, err := gitCommandExitsZero(root, "ls-files", "--error-unmatch", "--", relPath)
	if err != nil {
		return localArtifactGitState{}, err
	}
	ignored, err := gitCommandExitsZero(root, "check-ignore", "-q", "--", relPath)
	if err != nil {
		return localArtifactGitState{}, err
	}
	return localArtifactGitState{Tracked: tracked, Ignored: ignored}, nil
}

func gitCommandExitsZero(root string, args ...string) (bool, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}

func safeLocalArtifactPath(root, relPath string) (string, error) {
	cleanRel := filepath.Clean(relPath)
	if filepath.IsAbs(cleanRel) || cleanRel == "." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) || cleanRel == ".." {
		return "", fmt.Errorf("unsafe cleanup path %q", relPath)
	}
	absPath := filepath.Join(root, cleanRel)
	relToRoot, err := filepath.Rel(root, absPath)
	if err != nil {
		return "", fmt.Errorf("rel %s: %w", relPath, err)
	}
	if strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || relToRoot == ".." {
		return "", fmt.Errorf("cleanup path escapes root: %s", relPath)
	}
	return absPath, nil
}

func localArtifactSize(path string, info os.FileInfo) (int64, error) {
	if !info.IsDir() {
		return info.Size(), nil
	}
	var size int64
	err := filepath.WalkDir(path, func(child string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func writeLocalArtifactHygieneText(w io.Writer, report localArtifactHygieneReport, cleanRequested bool) {
	if len(report.Findings) == 0 {
		fmt.Fprintf(w, "local generated-artifact hygiene: clean (%s)\n", report.Root)
		return
	}
	if cleanRequested {
		fmt.Fprintf(w, "local generated-artifact hygiene: removed %s across %d candidates\n", formatBytes(report.RemovedBytes), removedLocalArtifactCount(report.Findings))
	} else {
		fmt.Fprintf(w, "local generated-artifact hygiene: found %s across %d candidates\n", formatBytes(report.TotalBytes), len(report.Findings))
	}
	for _, finding := range report.Findings {
		status := "stale"
		if finding.Removed {
			status = "removed"
		}
		if finding.Tracked {
			status = "unsafe"
		}
		fmt.Fprintf(w, "  %s %s %s", status, finding.Path, formatBytes(finding.SizeBytes))
		if finding.Ignored {
			fmt.Fprint(w, " ignored")
		}
		if finding.UnsafeReason != "" {
			fmt.Fprintf(w, " %s", finding.UnsafeReason)
		}
		fmt.Fprintln(w)
	}
}

func removedLocalArtifactCount(findings []localArtifactFinding) int {
	count := 0
	for _, finding := range findings {
		if finding.Removed {
			count++
		}
	}
	return count
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f%s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1fPiB", value/unit)
}

package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

// TryCompactDockerPsQuiet summarizes empty stdout from `docker`/`nerdctl`/`podman` `ps -q` / `--quiet` when no containers run (F13 partial).
func TryCompactDockerPsQuiet(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	if argv[1] != "ps" {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	var client string
	switch b {
	case "docker":
		client = "docker"
	case "nerdctl", "nerdctl.exe":
		client = "nerdctl"
	case "podman", "podman.exe":
		client = "podman"
	default:
		return stdout, false
	}
	if !argvContainsToken(argv, "-q") && !argvContainsToken(argv, "--quiet") {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte(fmt.Sprintf("[%s ps] no containers\n", client)), true
}

// TryCompactDockerImagesQuiet summarizes empty stdout from `docker`/`nerdctl`/`podman images -q` / `--quiet` when no images match (F13 partial).
func TryCompactDockerImagesQuiet(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	if argv[1] != "images" {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	var client string
	switch b {
	case "docker":
		client = "docker"
	case "nerdctl", "nerdctl.exe":
		client = "nerdctl"
	case "podman", "podman.exe":
		client = "podman"
	default:
		return stdout, false
	}
	if !argvContainsToken(argv, "-q") && !argvContainsToken(argv, "--quiet") {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte(fmt.Sprintf("[%s images] empty\n", client)), true
}

// TryCompactDockerComposePsQuiet summarizes empty stdout from `docker compose ps -q` / `docker-compose ps -q` (F13 partial).
func TryCompactDockerComposePsQuiet(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !argvContainsToken(argv, "-q") && !argvContainsToken(argv, "--quiet") {
		return stdout, false
	}
	b := filepath.Base(argv[0])
	if b == "docker" && len(argv) >= 4 && argv[1] == "compose" && argv[2] == "ps" {
		return []byte("[docker compose ps] no containers\n"), true
	}
	if (b == "docker-compose" || b == "docker-compose.exe") && len(argv) >= 3 && argv[1] == "ps" {
		return []byte("[docker-compose ps] no containers\n"), true
	}
	return stdout, false
}

// TryCompactDockerComposeLsQuiet summarizes empty stdout from `docker compose ls -q` / `--quiet` (F13 partial).
func TryCompactDockerComposeLsQuiet(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !argvContainsToken(argv, "-q") && !argvContainsToken(argv, "--quiet") {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) < 4 || (b != "docker" && b != "docker.exe") || argv[1] != "compose" || argv[2] != "ls" {
		return stdout, false
	}
	return []byte("[docker compose ls] empty\n"), true
}

// TryCompactHelmListQuiet summarizes empty stdout from `helm list -q` (F13 partial).
func TryCompactHelmListQuiet(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	b := filepath.Base(argv[0])
	if b != "helm" && b != "helm.exe" {
		return stdout, false
	}
	if argv[1] != "list" {
		return stdout, false
	}
	if !argvContainsToken(argv, "-q") && !argvContainsToken(argv, "--short") {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[helm list] empty\n"), true
}

// TryCompactHelmSearch summarizes empty stdout from `helm search …` when there are no results (F13 partial).
func TryCompactHelmSearch(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	b := filepath.Base(argv[0])
	if b != "helm" && b != "helm.exe" {
		return stdout, false
	}
	if argv[1] != "search" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[helm search] empty\n"), true
}

// TryCompactKubectlGet summarizes empty stdout from `kubectl get …` / `oc get …` when nothing matches (F13 partial).
func TryCompactKubectlGet(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 3 {
		return stdout, false
	}
	b := filepath.Base(argv[0])
	var client string
	switch b {
	case "kubectl", "kubectl.exe":
		client = "kubectl"
	case "oc", "oc.exe":
		client = "oc"
	default:
		return stdout, false
	}
	if argv[1] != "get" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte(fmt.Sprintf("[%s get] empty\n", client)), true
}

// TryCompactContainerOutput chains docker + kubectl empty-result summaries.
func TryCompactContainerOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := TryCompactDockerPsQuiet(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDockerImagesQuiet(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDockerComposePsQuiet(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDockerComposeLsQuiet(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactHelmListQuiet(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactHelmSearch(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactKubectlGet(argv, stdout); ok {
		return out, true
	}
	// Fallback: non-empty docker ps / kubectl get table → count rows.
	if label, rows := containerTableRows(argv, stdout); rows > 0 {
		s := strings.TrimSpace(string(stdout))
		if compact := compactContainerTable(s, label, rows); compact != "" && len(compact) < len(s) {
			return []byte(compact), true
		}
	}
	return stdout, false
}

// containerTableRows detects docker ps / kubectl get table output and returns (label, rowCount).
func containerTableRows(argv []string, stdout []byte) (string, int) {
	if len(argv) < 2 {
		return "", 0
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return "", 0
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	lines := strings.Split(s, "\n")
	var nonEmpty []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	if len(nonEmpty) < 2 {
		return "", 0 // header only or empty
	}

	switch b0 {
	case "docker", "nerdctl", "podman", "podman.exe", "nerdctl.exe":
		switch argv[1] {
		case "ps":
			if strings.HasPrefix(nonEmpty[0], "CONTAINER ID") {
				return fmt.Sprintf("%s ps", b0), len(nonEmpty) - 1
			}
		case "images":
			if strings.HasPrefix(nonEmpty[0], "REPOSITORY") {
				return fmt.Sprintf("%s images", b0), len(nonEmpty) - 1
			}
		}
	case "kubectl", "kubectl.exe":
		if argv[1] == "get" && len(nonEmpty) >= 2 {
			// kubectl get table: first line is header (NAME READY STATUS ...)
			return fmt.Sprintf("kubectl get %s", strings.Join(argv[2:], " ")), len(nonEmpty) - 1
		}
	}
	return "", 0
}

// compactContainerTable returns a summary of table output if it's shorter.
func compactContainerTable(s, label string, rows int) string {
	if rows <= 5 {
		return "" // short tables are fine as-is
	}
	return fmt.Sprintf("[%s] %d item(s)\n", label, rows)
}

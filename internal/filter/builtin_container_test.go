package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactContainerOutput(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactDockerPsQuiet([]string{"docker", "ps", "-q"}, []byte(""))
	if !ok || string(out) != "[docker ps] no containers\n" {
		t.Fatalf("docker ps -q: ok=%v %q", ok, out)
	}
	nerd, ok := TryCompactDockerPsQuiet([]string{"nerdctl", "ps", "--quiet"}, []byte(""))
	if !ok || string(nerd) != "[nerdctl ps] no containers\n" {
		t.Fatalf("nerdctl: %q", nerd)
	}
	pod, ok := TryCompactDockerPsQuiet([]string{"podman", "ps", "-q"}, []byte(""))
	if !ok || string(pod) != "[podman ps] no containers\n" {
		t.Fatalf("podman: %q", pod)
	}
	img, ok := TryCompactDockerImagesQuiet([]string{"docker", "images", "-q"}, []byte(""))
	if !ok || string(img) != "[docker images] empty\n" {
		t.Fatalf("docker images: %q", img)
	}
	imgN, ok := TryCompactDockerImagesQuiet([]string{"nerdctl", "images", "--quiet"}, []byte(""))
	if !ok || string(imgN) != "[nerdctl images] empty\n" {
		t.Fatalf("nerdctl images: %q", imgN)
	}
	if _, ok := TryCompactDockerPsQuiet([]string{"docker", "ps"}, []byte("")); ok {
		t.Fatal("docker ps without -q")
	}
	out2, ok := TryCompactKubectlGet([]string{"kubectl", "get", "pods", "-n", "x"}, []byte("\n"))
	if !ok || string(out2) != "[kubectl get] empty\n" {
		t.Fatalf("kubectl: %q", out2)
	}
	ocg, ok := TryCompactKubectlGet([]string{"oc", "get", "pods"}, []byte(""))
	if !ok || string(ocg) != "[oc get] empty\n" {
		t.Fatalf("oc: %q", ocg)
	}
	out3, ok := TryCompactContainerOutput([]string{"docker", "ps", "--quiet"}, []byte(""))
	if !ok || string(out3) != "[docker ps] no containers\n" {
		t.Fatalf("chain: %q", out3)
	}
	outImg, ok := TryCompactContainerOutput([]string{"docker", "images", "-q"}, []byte(""))
	if !ok || string(outImg) != "[docker images] empty\n" {
		t.Fatalf("chain images: %q", outImg)
	}
	dc, ok := TryCompactDockerComposePsQuiet([]string{"docker", "compose", "ps", "-q"}, []byte(""))
	if !ok || string(dc) != "[docker compose ps] no containers\n" {
		t.Fatalf("compose: %q", dc)
	}
	dcls, ok := TryCompactDockerComposeLsQuiet([]string{"docker", "compose", "ls", "-q"}, []byte(""))
	if !ok || string(dcls) != "[docker compose ls] empty\n" {
		t.Fatalf("compose ls: %q", dcls)
	}
	dcy, ok := TryCompactDockerComposePsQuiet([]string{"docker-compose", "ps", "-q"}, []byte(""))
	if !ok || string(dcy) != "[docker-compose ps] no containers\n" {
		t.Fatalf("docker-compose: %q", dcy)
	}
	hl, ok := TryCompactHelmListQuiet([]string{"helm", "list", "-q", "-n", "x"}, []byte(""))
	if !ok || string(hl) != "[helm list] empty\n" {
		t.Fatalf("helm: %q", hl)
	}
	hs, ok := TryCompactHelmSearch([]string{"helm", "search", "repo", "nonexistent-xyz"}, []byte(""))
	if !ok || string(hs) != "[helm search] empty\n" {
		t.Fatalf("helm search: %q", hs)
	}
	outHS, ok := TryCompactContainerOutput([]string{"helm", "search", "hub", "foo"}, []byte("\n"))
	if !ok || string(outHS) != "[helm search] empty\n" {
		t.Fatalf("chain helm search: %q", outHS)
	}
}

// TestTryCompactContainerOutput_missingBranches covers guard and alternative-binary branches.
func TestTryCompactContainerOutput_missingBranches(t *testing.T) {
	t.Parallel()

	// --- TryCompactDockerPsQuiet ---
	// len < 2
	if _, ok := TryCompactDockerPsQuiet([]string{"docker"}, []byte("")); ok {
		t.Fatal("docker: len<2")
	}
	// unknown binary (default case)
	if _, ok := TryCompactDockerPsQuiet([]string{"kubectl", "ps", "-q"}, []byte("")); ok {
		t.Fatal("kubectl: not docker/nerdctl/podman")
	}
	// non-empty stdout
	if _, ok := TryCompactDockerPsQuiet([]string{"docker", "ps", "-q"}, []byte("abc123\n")); ok {
		t.Fatal("docker ps -q: non-empty stdout")
	}

	// --- TryCompactDockerImagesQuiet ---
	// len < 2
	if _, ok := TryCompactDockerImagesQuiet([]string{"docker"}, []byte("")); ok {
		t.Fatal("docker images: len<2")
	}
	// podman images
	podImg, ok := TryCompactDockerImagesQuiet([]string{"podman", "images", "-q"}, []byte(""))
	if !ok || string(podImg) != "[podman images] empty\n" {
		t.Fatalf("podman images: ok=%v %q", ok, podImg)
	}
	// unknown binary (default case)
	if _, ok := TryCompactDockerImagesQuiet([]string{"kubectl", "images", "-q"}, []byte("")); ok {
		t.Fatal("kubectl: not docker/nerdctl/podman")
	}
	// no -q flag
	if _, ok := TryCompactDockerImagesQuiet([]string{"docker", "images", "--no-trunc"}, []byte("")); ok {
		t.Fatal("docker images without -q")
	}
	// non-empty stdout
	if _, ok := TryCompactDockerImagesQuiet([]string{"docker", "images", "-q"}, []byte("abc123\n")); ok {
		t.Fatal("docker images -q: non-empty stdout")
	}

	// --- TryCompactHelmListQuiet ---
	// len < 2
	if _, ok := TryCompactHelmListQuiet([]string{"helm"}, []byte("")); ok {
		t.Fatal("helm: len<2")
	}
	// missing -q/--short
	if _, ok := TryCompactHelmListQuiet([]string{"helm", "list", "-n", "default"}, []byte("")); ok {
		t.Fatal("helm list without -q/--short")
	}
	// non-empty stdout
	if _, ok := TryCompactHelmListQuiet([]string{"helm", "list", "-q"}, []byte("my-release\n")); ok {
		t.Fatal("helm list -q: non-empty stdout")
	}
	// --short flag
	hlShort, ok := TryCompactHelmListQuiet([]string{"helm", "list", "--short"}, []byte(""))
	if !ok || string(hlShort) != "[helm list] empty\n" {
		t.Fatalf("helm list --short: ok=%v %q", ok, hlShort)
	}

	// --- TryCompactHelmSearch ---
	// len < 2
	if _, ok := TryCompactHelmSearch([]string{"helm"}, []byte("")); ok {
		t.Fatal("helm search: len<2")
	}
	// wrong subcommand
	if _, ok := TryCompactHelmSearch([]string{"helm", "list"}, []byte("")); ok {
		t.Fatal("helm list: not search")
	}
	// non-empty stdout
	if _, ok := TryCompactHelmSearch([]string{"helm", "search", "repo", "x"}, []byte("NAME\n")); ok {
		t.Fatal("helm search: non-empty stdout")
	}

	// --- TryCompactKubectlGet ---
	if _, ok := TryCompactKubectlGet([]string{"kubectl", "describe", "pods"}, []byte("")); ok {
		t.Fatal("kubectl describe: not get")
	}
	// non-empty stdout
	if _, ok := TryCompactKubectlGet([]string{"kubectl", "get", "pods"}, []byte("pod/my-pod\n")); ok {
		t.Fatal("kubectl get pods: non-empty stdout")
	}
}

func TestTryCompactContainerOutput_dockerPsTable(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("CONTAINER ID   IMAGE     COMMAND   CREATED   STATUS    PORTS     NAMES\n")
	for i := 0; i < 10; i++ {
		sb.WriteString(fmt.Sprintf("abc%010d   nginx     /nginx    5m ago    Up 5m     80/tcp    web%d\n", i, i))
	}
	input := sb.String()
	out, ok := TryCompactContainerOutput([]string{"docker", "ps"}, []byte(input))
	if ok || string(out) != input {
		t.Fatalf("healthy docker ps table must full-pass: ok=%v out=%q", ok, out)
	}
}

func TestTryCompactContainerOutput_kubectlGetMany(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("NAME                    READY   STATUS    RESTARTS   AGE\n")
	for i := 0; i < 10; i++ {
		sb.WriteString(fmt.Sprintf("nginx-deployment-%05d   1/1     Running   0          5d\n", i))
	}
	input := sb.String()
	out, ok := TryCompactContainerOutput([]string{"kubectl", "get", "pods"}, []byte(input))
	if ok || string(out) != input {
		t.Fatalf("healthy kubectl get table must full-pass: ok=%v out=%q", ok, out)
	}
}

func TestTryCompactContainerOutput_keepsAttentionRows(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("NAME                    READY   STATUS             RESTARTS   AGE\n")
	for i := 0; i < 10; i++ {
		status := "Running"
		if i == 7 {
			status = "CrashLoopBackOff"
		}
		sb.WriteString(fmt.Sprintf("api-%05d              1/1     %-18s 0          5d\n", i, status))
	}
	out, ok := TryCompactContainerOutput([]string{"kubectl", "get", "pods"}, []byte(sb.String()))
	if !ok {
		t.Fatalf("expected compact kubectl get table, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "attention row") || !strings.Contains(s, "api-00007") || !strings.Contains(s, "CrashLoopBackOff") {
		t.Fatalf("attention row was not preserved: %q", s)
	}
}

// TestContainerTableRows_dockerImages covers the "docker images" REPOSITORY header branch.
func TestContainerTableRows_dockerImages(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("REPOSITORY          TAG       IMAGE ID       CREATED        SIZE\n")
	for i := 0; i < 8; i++ {
		sb.WriteString(fmt.Sprintf("nginx               latest    abc%05d       5 days ago     133MB\n", i))
	}
	label, rows := containerTableRows([]string{"docker", "images"}, []byte(sb.String()))
	if label != "docker images" {
		t.Errorf("docker images: want label 'docker images', got %q", label)
	}
	if rows != 8 {
		t.Errorf("docker images: want 8 rows, got %d", rows)
	}
}

// TestContainerTableRows_unknownSubcommand covers the default return "", 0 branch.
func TestContainerTableRows_unknownSubcommand(t *testing.T) {
	t.Parallel()
	// docker run is not a table command
	label, rows := containerTableRows([]string{"docker", "run", "nginx"}, []byte("some output\nmore output\n"))
	if label != "" || rows != 0 {
		t.Errorf("docker run: want empty label and 0 rows, got %q %d", label, rows)
	}
}

// TestContainerTableRows_headerOnly covers the len(nonEmpty) < 2 branch.
func TestContainerTableRows_headerOnly(t *testing.T) {
	t.Parallel()
	// Only header line, no data rows
	input := "CONTAINER ID   IMAGE   COMMAND   CREATED   STATUS   PORTS   NAMES\n"
	label, rows := containerTableRows([]string{"docker", "ps"}, []byte(input))
	if label != "" || rows != 0 {
		t.Errorf("header-only: want empty, got %q %d", label, rows)
	}
}

// TestContainerTableRows_shortArgv covers the len(argv) < 2 guard (line 198-200).
func TestContainerTableRows_shortArgv(t *testing.T) {
	t.Parallel()
	label, rows := containerTableRows([]string{"docker"}, []byte("CONTAINER ID\nsome-id\n"))
	if label != "" || rows != 0 {
		t.Errorf("argv<2: want empty, got %q %d", label, rows)
	}
}

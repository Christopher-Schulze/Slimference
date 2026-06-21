// Command ci runs the full CI pipeline locally.
// Usage (from module root): go run ./scripts/ci
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

type step struct {
	label string
	cmd   string
	args  []string
}

func defaultSteps() []step {
	return []step{
		{
			label: "gofmt",
			cmd:   "internal:gofmt-check",
		},
		{
			label: "go vet",
			cmd:   "go",
			args:  []string{"vet", "./..."},
		},
		{
			label: "go build",
			cmd:   "go",
			args:  []string{"build", "./cmd/..."},
		},
		{
			label: "go test",
			cmd:   "go",
			args:  []string{"test", "./cmd/...", "./internal/...", "-timeout", "120s"},
		},
		{
			label: "coverage gate",
			cmd:   "go",
			args:  []string{"run", "./scripts/coverage", "-min=94.5"},
		},
		{
			label: "codex smoke gate",
			cmd:   "go",
			args:  []string{"run", "./scripts/benchmarks", "codex-smoke-gate", "tests/fixtures/codex"},
		},
		{
			label: "live corpus gate",
			cmd:   "go",
			args: []string{
				"run", "./scripts/benchmarks", "benchmark-corpus", "tests/fixtures/live_corpus",
				"--check",
				"--promotion-check",
				"--maxx-check",
				"--real-local-min-ratio=0.0597",
				"--real-local-min-saved=331802",
			},
		},
		{
			label: "leaf audit gate",
			cmd:   "go",
			args:  []string{"run", "./scripts/utils", "leaf-audit", "--check", "--max-empty-only-pct=20", "--root=."},
		},
	}
}

func main() {
	raiseFDLimit()
	os.Exit(run(defaultSteps(), os.Stdout, os.Stderr))
}

// raiseFDLimit raises the process file-descriptor limit so parallel test
// packages that open many sockets/files do not hit "too many open files"
// on platforms with a low default soft limit. The Go runtime already
// raises the limit for the current process, but `go test` spawns child
// test binaries that may inherit the original shell limit.
func raiseFDLimit() {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return
	}
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		return
	}
	const want = 65536
	if rlim.Cur >= want {
		return
	}
	rlim.Cur = want
	if rlim.Max > 0 && rlim.Cur > rlim.Max {
		rlim.Cur = rlim.Max
	}
	_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlim)
}

// run executes the given steps with the supplied IO streams and returns the
// process exit code. Split out of main so unit tests can drive the runner
// without actually calling os.Exit. The caller provides the steps so tests
// can run with mocked cheap commands.
func run(steps []step, stdout, stderr *os.File) int {
	root, err := findModuleRoot()
	if err != nil {
		fmt.Fprintf(stderr, "ci: %v\n", err)
		return 2
	}

	total := len(steps)
	for i, s := range steps {
		fmt.Fprintf(stdout, "[%d/%d] %s\n", i+1, total, s.label)
		// Internal steps run inline so we don't fork an extra
		// `gofmt`-check binary just to keep the CI pipeline gating
		// consistent. New internal steps register here.
		if s.cmd == "internal:gofmt-check" {
			if err := runGofmtCheck(root, stdout); err != nil {
				fmt.Fprintf(stdout, "\nFAIL: step %d/%d (%s): %v\n", i+1, total, s.label, err)
				return 1
			}
			continue
		}
		c := exec.Command(s.cmd, s.args...)
		c.Dir = root
		c.Stdout = stdout
		c.Stderr = stderr
		if err := c.Run(); err != nil {
			fmt.Fprintf(stdout, "\nFAIL: step %d/%d (%s)\n", i+1, total, s.label)
			return 1
		}
	}

	fmt.Fprintf(stdout, "\nPASS: all %d steps completed\n", total)
	return 0
}

func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from cwd")
		}
		dir = parent
	}
}

// runGofmtCheck runs `gofmt -l` against the supervised dirs (cmd,
// internal, scripts) and fails when any file would be reformatted.
// Only repository-owned Go tooling is checked here; removed reference trees are
// intentionally outside the supervised script layout.
func runGofmtCheck(root string, stdout *os.File) error {
	c := exec.Command("gofmt", "-l", "./cmd", "./internal", "./scripts")
	c.Dir = root
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	if err := c.Run(); err != nil {
		return fmt.Errorf("gofmt run: %w (output: %s)", err, out.String())
	}
	drift := strings.TrimSpace(out.String())
	if drift == "" {
		fmt.Fprintln(stdout, "gofmt: clean")
		return nil
	}
	fmt.Fprintln(stdout, "gofmt drift:")
	for _, line := range strings.Split(drift, "\n") {
		fmt.Fprintln(stdout, "  "+line)
	}
	return fmt.Errorf("gofmt drift in %d file(s); run `gofmt -w`", len(strings.Split(drift, "\n")))
}
